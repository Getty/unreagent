//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The two refusals Windows gives a rename while a file in the directory is
// open count as "in use", wrapped as os.Rename returns them; a missing
// directory does not (k235).
func TestRenameInUseOnWindows(t *testing.T) {
	for _, c := range []struct {
		errno syscall.Errno
		want  bool
	}{
		{syscall.ERROR_ACCESS_DENIED, true},
		{syscall.Errno(32), true}, // ERROR_SHARING_VIOLATION
		{syscall.ERROR_FILE_NOT_FOUND, false},
	} {
		err := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: c.errno}
		if got := renameInUse(err); got != c.want {
			t.Errorf("renameInUse(%v) = %v; want %v", err, got, c.want)
		}
	}
}

// A directory with a file held open cannot be renamed on Windows, and the
// error is one renameInUse accepts: the k235 cause, against the real OS.
func TestRenameOfADirectoryWithAnOpenFileIsInUse(t *testing.T) {
	dir := t.TempDir()
	held := filepath.Join(dir, "held")
	if err := os.Mkdir(held, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(held, "file"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = os.Rename(held, filepath.Join(dir, "moved"))
	if err == nil {
		t.Fatal("rename succeeded with a file open inside; want Windows to refuse it")
	}
	if !renameInUse(err) {
		t.Errorf("rename error %v; want renameInUse to accept it", err)
	}
}
