//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// Through syscall.NewLazyDLL, like the supervisor's job objects: no cgo, so
// the cross-compile from Linux keeps working.
var procGetConsoleProcessList = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// soleConsoleClient reports whether an error exit should wait for Enter
// (pauseBeforeExit, k160): stdin and stderr are both the console, and the
// launcher is the only process attached to it. Started from cmd.exe or
// PowerShell, the shell is attached too; a pipe or redirect fails the console
// check; without a console GetConsoleProcessList fails. All of these are false.
//
// One process on the console does not prove an Explorer start (research
// 2026-11-21, section 5): `start unreagent.exe`, or any parent that gives the
// launcher a new console, counts as well. That console closes with the
// launcher just the same, so it waits there too. A child the launcher started
// on its console and that still runs is attached and makes the count > 1.
func soleConsoleClient() bool {
	var mode uint32
	if syscall.GetConsoleMode(syscall.Handle(os.Stdin.Fd()), &mode) != nil {
		return false
	}
	if syscall.GetConsoleMode(syscall.Handle(os.Stderr.Fd()), &mode) != nil {
		return false
	}
	// A buffer of one: with more processes attached the call stores no ids and
	// returns the number it needs, > 1; 0 is a failure (no console).
	var ids [1]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), 1)
	return n == 1 && ids[0] == uint32(os.Getpid())
}
