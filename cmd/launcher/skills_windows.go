//go:build windows

package main

import "syscall"

// renameInUseErrors are the errors Windows gives a rename of a directory in
// which another process holds a file or directory open (Go opens files without
// FILE_SHARE_DELETE): ERROR_ACCESS_DENIED and ERROR_SHARING_VIOLATION (32,
// which package syscall does not name). A second launcher writing its new
// copy, a sweep still removing what it claimed, a virus scanner on a file just
// written: each holds it only briefly.
var renameInUseErrors = []error{syscall.ERROR_ACCESS_DENIED, syscall.Errno(32)}
