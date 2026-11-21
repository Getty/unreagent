package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// k119: warnIfMissing used os.Stat alone, so a bare command name — resolved
// via PATH when the service starts — warned on every start ("WARN
// unreal.editor: path not found: ping" on every demo run). A bare name must
// be looked up on PATH, and a path must still warn when the file is missing.
func TestWarnIfMissing(t *testing.T) {
	bin := t.TempDir()
	exe := "fake-editor"
	if runtime.GOOS == "windows" {
		exe += ".exe" // LookPath finds a bare name on Windows only by its PATHEXT extension
	}
	present := filepath.Join(bin, exe)
	if err := os.WriteFile(present, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	for _, tc := range []struct {
		name, path string
		warn       bool
	}{
		{"bare name on PATH", "fake-editor", false},
		{"bare name not on PATH", "no-such-editor", true},
		{"absolute path present", present, false},
		{"absolute path missing", filepath.Join(bin, "missing", exe), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			warnIfMissing(func(s string) { got = append(got, s) }, "unreal.editor", tc.path)
			if tc.warn && len(got) == 0 {
				t.Fatalf("%q is missing but warnIfMissing stayed silent", tc.path)
			}
			if !tc.warn && len(got) != 0 {
				t.Fatalf("%q is there but warnIfMissing warned: %q", tc.path, got)
			}
		})
	}
}

// k119 review: a bare name that LookPath finds only relative to the current
// directory (PATH holds "."; on Windows the implicit current-directory
// search) comes back with exec.ErrDot. The file is there, but exec refuses
// to start it — the warning must say that, not "not found on PATH".
func TestWarnIfMissingFoundOnlyRelative(t *testing.T) {
	dir := t.TempDir()
	exe := "fake-editor"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, exe), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("PATH", ".")

	var got []string
	warnIfMissing(func(s string) { got = append(got, s) }, "unreal.editor", "fake-editor")
	if len(got) != 1 {
		t.Fatalf("expected one warning — exec refuses a command found relative to the current directory — got %q", got)
	}
	if strings.Contains(got[0], "not found on PATH") || !strings.Contains(got[0], "relative to the current directory") {
		t.Fatalf("the warning must say the command was found only relative to the current directory: %q", got[0])
	}
}
