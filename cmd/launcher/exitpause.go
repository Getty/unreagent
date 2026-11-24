package main

import (
	"fmt"
	"io"
)

// exitPauseHint is the line an error exit shows before it waits (k160).
const exitPauseHint = "unreagent stopped with the error above. Press Enter to close this window."

// pauseBeforeExit holds an error exit until the user presses Enter, when sole
// says the launcher is the only process on its console: started from Explorer
// (a double-click, or a file dropped onto the .exe), that console closes as the
// launcher exits and takes the error with it (k160). It writes exitPauseHint to
// out and reads in a byte at a time up to the first line end, never past it,
// or until a read fails (EOF, the console closed). With sole false it neither
// writes nor reads. main calls it after run() has returned, so the editor and
// the agent have ended by then and their job objects are closed; see
// soleConsoleClient for what sole means and when it is false.
func pauseBeforeExit(sole bool, in io.Reader, out io.Writer) {
	if !sole {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, exitPauseHint)
	b := make([]byte, 1)
	for {
		n, err := in.Read(b)
		if n == 1 && b[0] == '\n' {
			return
		}
		if err != nil {
			return
		}
	}
}
