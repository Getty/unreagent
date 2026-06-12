//go:build !windows

// On non-Windows platforms there is no taskbar to flash, so the operation is a
// no-op. This stub keeps callers in cmd/launcher platform-agnostic — the
// cross-compile from Linux stays green without any Windows-specific code.
package supervisor

// FlashConsoleWindow is a no-op on non-Windows. It exists so MCP tool handlers
// and the "unreagent flash" sub-command can call it unconditionally.
func FlashConsoleWindow() error {
	return nil
}
