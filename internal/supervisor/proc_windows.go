//go:build windows

package supervisor

import (
	"os/exec"
	"syscall"
)

// createNewConsole = CREATE_NEW_CONSOLE: der Kindprozess bekommt eine eigene
// Konsole (eigenes Fenster mit echtem TTY).
const createNewConsole = 0x00000010

// setNewConsole lässt den Prozess in einem eigenen Konsolenfenster starten.
func setNewConsole(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
}
