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
	"syscall"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procFlashWindowEx = user32.NewProc("FlashWindowEx")

	// kernel32 is already lazily loaded by job_windows.go, but we re-bind
	// GetConsoleWindow here so this file stays self-contained and build-order
	// independent (the lazy handles from job_windows.go would work too, but
	// referencing them across files would force an awkward coupling).
	kernel32Flash        = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWindow = kernel32Flash.NewProc("GetConsoleWindow")
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
// process) in the taskbar until the user activates it. Returns nil if the
// process has no console (e.g. launched detached) — callers don't need to
// special-case that.
func FlashConsoleWindow() error {
	hwnd, _, err := procGetConsoleWindow.Call()
	if hwnd == 0 {
		// No console (e.g. running under `nohup` or as a Windows service).
		// Treat as a no-op so the MCP tool still returns success.
		if err != nil && !errors.Is(err, syscall.Errno(0)) {
			return nil
		}
		return nil
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
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return callErr
		}
		return errors.New("FlashWindowEx returned 0")
	}
	return nil
}
