//go:build !windows

package main

// soleConsoleClient is false outside Windows: a terminal there outlives the
// programs it runs, so an error exit never waits (k160).
func soleConsoleClient() bool { return false }
