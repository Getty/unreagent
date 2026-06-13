//go:build windows

// FlashConsoleWindow flashes the current process's console window in the
// Windows taskbar. We use the Win32 API directly via syscall.NewLazyDLL to
// avoid pulling in golang.org/x/sys/windows — the cross-compile from Linux
// stays offline and we mirror the pattern in job_windows.go.
//
// The flash keeps going (FLASHW_TIMERNOFG) until the user activates the
// window, so a single call is enough for the whole "waiting for input" window.
package supervisor

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procFlashWindowEx = user32.NewProc("FlashWindowEx")

	// kernel32 is already lazily loaded by job_windows.go, but we re-bind
	// GetConsoleWindow + AllocConsole here so this file stays self-contained
	// and build-order independent (the lazy handles from job_windows.go would
	// work too, but referencing them across files would force an awkward
	// coupling).
	kernel32Flash        = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWindow = kernel32Flash.NewProc("GetConsoleWindow")
	procAllocConsole     = kernel32Flash.NewProc("AllocConsole")
)

const (
	flashwStop    = 0
	flashwCaption = 0x00000001
	flashwTray    = 0x00000002
	// FLASHW_TIMERNOFG: flash continuously until the window comes to the
	// foreground. Combined with FLASHW_TRAY we get a taskbar-only flash that
	// self-stops when the user clicks the taskbar entry.
	flashwTimerNoFG = 0x0000000C
)

// flashWInfo is the FLASHWINFO struct passed to FlashWindowEx. Layout must
// match the Win32 declaration exactly.
type flashWInfo struct {
	cbSize    uint32
	hwnd      uintptr
	dwFlags   uint32
	uCount    uint32
	dwTimeout uint32
}

// FlashConsoleWindow flashes the console window (the one attached to this
// process) in the taskbar until the user activates it.
//
// If the process has no console attached (e.g. launched detached, via
// Task-Scheduler, or from the Explorer with `CreateProcess` flags that hide
// the console), the function tries to allocate one with AllocConsole as a
// fallback — useful for the "unreagent started in background, agent needs
// to flash the taskbar" workflow. If AllocConsole also fails (e.g. the
// process is a Windows service with no interactive session), a non-nil
// error is returned so the caller can surface the failure instead of
// silently doing nothing — the previous behavior reported "ok" to the MCP
// tool even when nothing was flashed, which made diagnosis painful.
func FlashConsoleWindow() error {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		// No console attached. Try AllocConsole as a fallback — it creates a
		// new console for this process if the parent didn't pass one down
		// (common when unreagent is started from Task-Scheduler, Service, or
		// Explorer). Returns non-zero on success. We deliberately ignore the
		// underlying errno: AllocConsole failure modes (no station, no
		// desktop, etc.) are surfaced via the final GetConsoleWindow == 0
		// check below.
		ok, _, _ := procAllocConsole.Call()
		if ok != 0 {
			hwnd, _, _ = procGetConsoleWindow.Call()
		}
	}
	if hwnd == 0 {
		// Still no console after AllocConsole attempt. Return a clear error
		// so the MCP tool reports "flash failed: ..." instead of silently
		// claiming success. Operators see this in the unreagent log and
		// know the launcher needs a real terminal session.
		return fmt.Errorf("no console window attached to unreagent process " +
			"(GetConsoleWindow returned 0, AllocConsole fallback failed) — " +
			"start unreagent from a visible terminal to enable taskbar flash")
	}
	info := flashWInfo{
		cbSize:    uint32(unsafe.Sizeof(flashWInfo{})),
		hwnd:      hwnd,
		dwFlags:   flashwTray | flashwTimerNoFG,
		uCount:    0, // 0 = flash until window comes to foreground
		dwTimeout: 0, // use the OS default cursor blink rate
	}
	ret, _, callErr := procFlashWindowEx.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		// FlashWindowEx returned 0 (the window was not previously in the
		// foreground-flash state). syscall.Proc.Call captures GetLastError for
		// us on the same OS thread immediately after the call — a separate
		// GetLastError syscall would race the Go runtime, which may overwrite
		// the thread-local error before we read it. So use callErr directly.
		// The hex form is convenient for cross-referencing against WINERROR.H
		// (e.g. 0x0578 = 1400 = ERROR_INVALID_WINDOW_HANDLE → window lives on
		// another desktop/session-station; 0x0005 = ERROR_ACCESS_DENIED →
		// UAC/IL boundary).
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return fmt.Errorf("FlashWindowEx returned 0: %w (GetLastError=%d, 0x%x)",
				callErr, uint32(errno), uint32(errno))
		}
		return errors.New("FlashWindowEx returned 0 with no Win32 error set " +
			"(window may already be flashing, or lives on another desktop/session)")
	}
	return nil
}
