//go:build !windows

package supervisor

import (
	"os"
	"os/exec"
)

// setNewConsole: Nicht-Windows-Plattformen haben kein CREATE_NEW_CONSOLE. Als
// brauchbarer Fallback (Linux-Entwicklung) erbt der Prozess die Konsole des
// Launchers, damit er ein TTY hat. (Auf dem Zielsystem Windows läuft die echte
// Variante in proc_windows.go.)
func setNewConsole(c *exec.Cmd) {
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
}
