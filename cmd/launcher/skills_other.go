//go:build !windows

package main

// renameInUseErrors is empty off Windows: a POSIX rename does not care about
// open files in the directory, so no rename failure there is a passing "in
// use" that waiting would clear.
var renameInUseErrors []error
