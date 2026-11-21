package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
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

// k130: on Windows exec.Command adds a PATHEXT extension to a path that has
// none, so unreal.editor = C:/UE/Engine/Binaries/Win64/UnrealEditor starts
// UnrealEditor.exe. warnIfMissing checked the path with os.Stat alone, which
// knows no PATHEXT, and warned "path not found" for an editor that starts.
func TestWarnIfMissingAppliesPathExtOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PATHEXT is a Windows rule; TestWarnIfMissingAddsNoExtensionElsewhere covers this platform")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "UnrealEditor.exe")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	bare := filepath.Join(dir, "UnrealEditor")
	for _, tc := range []struct {
		name, path string
		warn       bool
	}{
		{"absolute path without .exe", bare, false},
		{"absolute path without .exe, forward slashes", filepath.ToSlash(bare), false},
		{"absolute path with .exe", exe, false},
		{"no file under any PATHEXT extension", filepath.Join(dir, "NoSuchEditor"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			warnIfMissing(func(s string) { got = append(got, s) }, "unreal.editor", tc.path)
			if tc.warn && len(got) == 0 {
				t.Fatalf("%q starts nothing but warnIfMissing stayed silent", tc.path)
			}
			if !tc.warn && len(got) != 0 {
				t.Fatalf("exec.Command starts %q, but warnIfMissing warned: %q", tc.path, got)
			}
		})
	}
}

// k130, the other platforms: exec adds no extension outside Windows, so a
// path without .exe names nothing exec can start even when its .exe sibling
// exists. No extension may be added here either: that path still warns, with
// the message it had before.
func TestWarnIfMissingAddsNoExtensionElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by TestWarnIfMissingAppliesPathExtOnWindows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "UnrealEditor.exe")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(dir, "UnrealEditor")

	var got []string
	warnIfMissing(func(s string) { got = append(got, s) }, "unreal.editor", bare)
	want := "WARN unreal.editor: path not found: " + bare
	if len(got) != 1 || got[0] != want {
		t.Fatalf("a path without .exe must still warn outside Windows\n got  %q\n want %q", got, []string{want})
	}

	got = nil
	warnIfMissing(func(s string) { got = append(got, s) }, "unreal.editor", exe)
	if len(got) != 0 {
		t.Fatalf("%q exists but warnIfMissing warned: %q", exe, got)
	}
}

// k147: outside Windows warnIfMissing checked a path with os.Stat alone, so a
// directory or a file without an execute bit passed as unreal.editor,
// although exec.Command then fails with permission denied. Each now warns,
// naming its cause; an executable file still passes. Starting each path with
// exec.Command checks the expectations against exec itself. A file that
// group and others may execute but its owner, this test, may not is refused
// by execve: the check goes by this user's permission, not by any execute
// bit.
func TestWarnIfMissingNamesWhyAPathDoesNotStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows whatever exec.LookPath refuses warns as not found; see TestWarnIfMissingAppliesPathExtOnWindows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "UnrealEditor")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "UnrealEditor.sh")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "Binaries") // its execute bits make it searchable, not startable
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	others := filepath.Join(dir, "Others.sh")
	if err := os.WriteFile(others, []byte("#!/bin/sh\n"), 0o611); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(others, 0o611); err != nil { // past the umask, which may clear group and other bits
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, path, want string
		notRoot          bool
	}{
		{"executable file", exe, "", false},
		{"file without an execute bit", plain, "WARN unreal.editor: path is not executable: " + plain, false},
		{"directory", sub, "WARN unreal.editor: path is a directory: " + sub, false},
		{"x for group and other, not owner", others, "WARN unreal.editor: path is not executable: " + others, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.notRoot && os.Geteuid() == 0 {
				t.Skip("root may execute any file with an execute bit")
			}
			if err := exec.Command(tc.path).Run(); (err != nil) != (tc.want != "") {
				t.Fatalf("the test's premise is wrong: exec.Command(%q).Run() = %v", tc.path, err)
			}
			var got []string
			warnIfMissing(func(s string) { got = append(got, s) }, "unreal.editor", tc.path)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("exec.Command starts %q, but warnIfMissing warned: %q", tc.path, got)
				}
				return
			}
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("exec.Command cannot start %q\n got  %q\n want %q", tc.path, got, []string{tc.want})
			}
		})
	}
}

// k133: getInt turned every JSON number into an int with int(v). 1.5 was
// truncated to 1 without a word, and 1e20, beyond any int, became MinInt64.
// A number that is not integral or does not fit an int is refused, and the
// error names the argument.
func TestGetIntRefusesNonIntegralAndOutOfRange(t *testing.T) {
	rangeErr := func(got string) string {
		return fmt.Sprintf("'offset' must be an integer between %d and %d, got %s", math.MinInt, math.MaxInt, got)
	}
	// -float64(math.MinInt) is 2^63 (2^31 where int has 32 bits): the first
	// value past math.MaxInt, and the value float64(math.MaxInt) rounds to,
	// so a "v > float64(math.MaxInt)" bound lets it through.
	pastMax := -float64(math.MinInt)
	for _, tc := range []struct {
		name string
		in   interface{}
		want string
	}{
		{"non-integral", 1.5, "'offset' must be an integer, got 1.5"},
		// Trunc rounds toward zero, so v-Trunc(v) is negative here: a check
		// for "v-Trunc(v) > 0" lets -1.5 through as -1.
		{"negative non-integral", -1.5, "'offset' must be an integer, got -1.5"},
		{"NaN", math.NaN(), "'offset' must be an integer, got NaN"},
		{"too large", 1e20, rangeErr("1e+20")},
		{"first value past MaxInt", pastMax, rangeErr(fmt.Sprint(pastMax))},
		{"too small", -1e20, rangeErr("-1e+20")},
		{"too large as a numeric string", "99999999999999999999", rangeErr(`"99999999999999999999"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := getInt(map[string]interface{}{"offset": tc.in}, "offset", 0)
			if err == nil {
				t.Fatalf("getInt(offset=%v) = %d, nil; want the error %q", tc.in, got, tc.want)
			}
			if err.Error() != tc.want {
				t.Fatalf("getInt(offset=%v) error\n got  %q\n want %q", tc.in, err, tc.want)
			}
		})
	}
}

// k133: the checks must not refuse a valid number. A negative offset is
// legal (read_file counts it from the end), and math.MinInt, which a float64
// holds exactly, is the lowest int there is.
func TestGetIntAcceptsIntegralEdges(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   float64
		want int
	}{
		{"negative offset", -65536, -65536},
		{"zero", 0, 0},
		{"MinInt", float64(math.MinInt), math.MinInt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := getInt(map[string]interface{}{"offset": tc.in}, "offset", 7)
			if err != nil || got != tc.want {
				t.Fatalf("getInt(offset=%v) = %d, %v; want %d, nil", tc.in, got, err, tc.want)
			}
		})
	}
}

// k133, end to end: read_file offset=1e20 read from the start of the file,
// because the offset arrived as MinInt64, a valid "count from the end"
// offset larger than any file. It must refuse instead.
func TestReadFileRefusesOffsetBeyondInt(t *testing.T) {
	res := readFileTool(t, "f.txt", "0123456789", map[string]interface{}{"offset": 1e20})
	want := fmt.Sprintf("Error: 'offset' must be an integer between %d and %d, got 1e+20", math.MinInt, math.MaxInt)
	if !res.IsError || res.Text != want {
		t.Fatalf("offset=1e20: IsError=%v\n got  %q\n want %q", res.IsError, res.Text, want)
	}
}
