// Regression tests for karr ticket #29: run_python/run_node used to write
// their scratch script into the system temp directory (os.CreateTemp("", …)),
// which kept Node from resolving bare specifiers via the project's
// node_modules and made Python's sys.path[0] a directory with nothing
// importable in it. The fix moves the script into the runtime's own working
// directory (runtimeDir), with no fallback to system temp on a create
// failure — that fallback is exactly the bug this ticket closes.
//
// isStaleScriptName is the sharp edge here: it decides what sweepStaleScripts
// deletes from a user's project directory on launcher startup. A false
// positive there destroys a file that isn't ours.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// --- test scaffolding -------------------------------------------------

// newLogCollector returns a Logger-shaped func that records every line it is
// given, plus a pointer to the recorded lines for later assertions.
func newLogCollector() (func(string), *[]string) {
	var lines []string
	return func(s string) { lines = append(lines, s) }, &lines
}

func mustTouch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

// staleNamedEntries lists the entries of dir whose name carries scriptPrefix,
// used to prove scriptAction never writes into the system temp dir on a
// create failure (no leftover unreagent-* entries appear there).
func staleNamedEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), scriptPrefix) {
			names = append(names, e.Name())
		}
	}
	return names
}

// --- isStaleScriptName ------------------------------------------------

// TestIsStaleScriptName is table-driven in both directions: names that
// os.CreateTemp("unreagent-*.<ext>") actually produces, and names a real
// project could plausibly contain that must survive the startup sweep.
func TestIsStaleScriptName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		// --- must match: real os.CreateTemp output ---
		{"unreagent-1.py", true},
		{"unreagent-4242424242.mjs", true},
		{"unreagent-3902847561.py", true},           // realistic 10-digit suffix
		{"unreagent-5.mjs", true},                   // single digit, the other known extension
		{"unreagent-0.py", true},                    // minimum digit body: a single "0"
		{"unreagent-007.mjs", true},                 // leading zeros are still decimal digits
		{"unreagent-99999999999999999999.py", true}, // long digit run: string-scanned, not parsed as a number

		// --- must NOT match: real project files ---
		{"unreagent-helper.py", false},  // non-digit body
		{"unreagent-.py", false},        // no digits before the extension
		{"unreagent-123.txt", false},    // extension scriptAction never assigns
		{"unreagent-123.py.bak", false}, // last dot wins; extension is "bak"
		{"unreagent-1.2.py", false},     // dot inside the digit run
		{"unreagent-12a.py", false},     // non-digit inside the digit run
		{"helper.py", false},            // missing the prefix entirely
		{"my-unreagent-1.py", false},    // prefix appears mid-string, not at position 0
		{"unreagent-", false},           // prefix only, nothing after it
		{"", false},                     // empty string
		{"unreagent-123.PY", false},     // extension match is case-sensitive
		{"Unreagent-123.py", false},     // prefix match is case-sensitive
		{"unreagent-123", false},        // no dot/extension at all
		{"unreagent-123.", false},       // trailing dot, empty extension
		{"unreagent-1.py ", false},      // trailing space folds into the "extension"
		{"unreagent-1٢.py", false},      // U+0662 ARABIC-INDIC DIGIT TWO looks numeral-ish but isn't ASCII '0'-'9'
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStaleScriptName(tc.name); got != tc.want {
				t.Errorf("isStaleScriptName(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestIsStaleScriptNameRecognizesRealCreateTempOutput closes a gap the table
// above cannot: every "must match" row there is a hand-written literal, so
// the table and isStaleScriptName could both agree with each other while
// silently disagreeing with what os.CreateTemp actually produces. This test
// asks the real generator for real names — same scriptPrefix constant, same
// "prefix*.ext" pattern scriptAction itself passes to os.CreateTemp — and
// checks isStaleScriptName still accepts them. If the Go stdlib ever changes
// the alphabet '*' expands to, this is what would catch sweepStaleScripts
// silently losing the ability to recognise its own leftovers.
func TestIsStaleScriptNameRecognizesRealCreateTempOutput(t *testing.T) {
	dir := t.TempDir()
	const iterations = 20 // past a single lucky draw; the point is the alphabet, not statistics
	for _, ext := range scriptExts {
		for i := 0; i < iterations; i++ {
			pattern := scriptPrefix + "*." + ext
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				t.Fatalf("os.CreateTemp(%q, %q): %v", dir, pattern, err)
			}
			name := filepath.Base(f.Name())
			f.Close()
			if !isStaleScriptName(name) {
				t.Errorf("os.CreateTemp(%q) produced %q, which isStaleScriptName does not recognise as stale. "+
					"This means the Go stdlib changed what '*' expands to in CreateTemp's pattern, not that "+
					"this test or scriptAction is broken — isStaleScriptName (and the rule it encodes) needs "+
					"a matching update, or sweepStaleScripts will stop cleaning up after itself.", pattern, name)
			}
		}
	}
}

// --- runtimeDir ---------------------------------------------------------

// TestRuntimeDirPrefersProjectThenAgentWorkdirThenDot exercises all three
// branches, including that a set project wins over a set agent workdir —
// scriptAction and sweepStaleScripts both call runtimeDir and must agree, so
// this is the single place that decision is allowed to live.
func TestRuntimeDirPrefersProjectThenAgentWorkdirThenDot(t *testing.T) {
	cases := []struct {
		name         string
		project      string
		agentWorkdir string
		want         string
	}{
		{"project wins over a set agent workdir", "/configured/project", "/agent/workdir", "/configured/project"},
		{"project alone", "/configured/project", "", "/configured/project"},
		{"agent workdir when project is unset", "", "/agent/workdir", "/agent/workdir"},
		{"both unset falls back to the current directory", "", "", "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeDir(tc.project, tc.agentWorkdir); got != tc.want {
				t.Errorf("runtimeDir(%q, %q) = %q, want %q", tc.project, tc.agentWorkdir, got, tc.want)
			}
		})
	}
}

// --- sweepStaleScripts ----------------------------------------------------

// TestSweepStaleScriptsRemovesOnlyStaleFiles plants two stale files, a
// same-prefixed user file, and a directory whose name matches the stale
// pattern, and asserts exactly the stale files vanish.
func TestSweepStaleScriptsRemovesOnlyStaleFiles(t *testing.T) {
	dir := t.TempDir()
	stale1 := filepath.Join(dir, "unreagent-1.py")
	stale2 := filepath.Join(dir, "unreagent-42.mjs")
	userFile := filepath.Join(dir, "unreagent-helper.py")
	staleLookingDir := filepath.Join(dir, "unreagent-2.py")
	mustTouch(t, stale1)
	mustTouch(t, stale2)
	mustTouch(t, userFile)
	if err := os.Mkdir(staleLookingDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", staleLookingDir, err)
	}

	cfg := &config.Config{Runtimes: config.Runtimes{
		Python: config.PythonRuntime{Enabled: true, Project: dir},
	}}
	logger, logs := newLogCollector()
	sweepStaleScripts(cfg, "", logger)

	if exists(t, stale1) {
		t.Errorf("stale file %s survived the sweep", stale1)
	}
	if exists(t, stale2) {
		t.Errorf("stale file %s survived the sweep", stale2)
	}
	if !exists(t, userFile) {
		t.Errorf("user file %s was deleted by the sweep, must survive", userFile)
	}
	if !exists(t, staleLookingDir) {
		t.Errorf("directory %s (named like a stale script) was removed by the sweep, must survive", staleLookingDir)
	}
	for _, line := range *logs {
		if strings.Contains(line, "WARN") {
			t.Errorf("sweep logged a warning during a clean run: %q", line)
		}
	}
}

// TestSweepStaleScriptsSkipsDisabledRuntime plants a stale file in a disabled
// runtime's directory and asserts it survives — only enabled runtimes'
// directories get swept.
func TestSweepStaleScriptsSkipsDisabledRuntime(t *testing.T) {
	enabledDir := t.TempDir()
	disabledDir := t.TempDir()
	enabledStale := filepath.Join(enabledDir, "unreagent-1.py")
	disabledStale := filepath.Join(disabledDir, "unreagent-1.mjs")
	mustTouch(t, enabledStale)
	mustTouch(t, disabledStale)

	cfg := &config.Config{Runtimes: config.Runtimes{
		Python: config.PythonRuntime{Enabled: true, Project: enabledDir},
		Node:   config.NodeRuntime{Enabled: false, Project: disabledDir},
	}}
	logger, _ := newLogCollector()
	sweepStaleScripts(cfg, "", logger)

	if exists(t, enabledStale) {
		t.Errorf("stale file in the enabled runtime's directory survived: %s", enabledStale)
	}
	if !exists(t, disabledStale) {
		t.Errorf("sweep touched the disabled runtime's directory: %s was removed", disabledStale)
	}
}

// TestSweepStaleScriptsDedupsSameDirectory has both runtimes point at the
// same directory and asserts the sweep neither double-processes it (which
// would try to remove the same file twice, logging a WARN on the second
// attempt) nor errors.
func TestSweepStaleScriptsDedupsSameDirectory(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "unreagent-7.py")
	mustTouch(t, stale)

	cfg := &config.Config{Runtimes: config.Runtimes{
		Python: config.PythonRuntime{Enabled: true, Project: dir},
		Node:   config.NodeRuntime{Enabled: true, Project: dir},
	}}
	logger, logs := newLogCollector()
	sweepStaleScripts(cfg, "", logger)

	if exists(t, stale) {
		t.Errorf("stale file survived: %s", stale)
	}
	var removals int
	for _, line := range *logs {
		if strings.Contains(line, "entfernt") {
			removals++
		}
		if strings.Contains(line, "WARN") {
			t.Errorf("both runtimes sharing a directory produced a warning (processed twice?): %q", line)
		}
	}
	if removals != 1 {
		t.Errorf("removal logged %d times, want exactly 1 — same directory from both runtimes must be deduped, not processed twice", removals)
	}
}

// TestSweepStaleScriptsSilentOnMissingDirectory points a runtime at a
// directory that doesn't exist. The code deliberately stays quiet there
// (comment at main.go: "scriptAction meldet das laut genug") — no panic, no
// log line of any kind.
func TestSweepStaleScriptsSilentOnMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	cfg := &config.Config{Runtimes: config.Runtimes{
		Python: config.PythonRuntime{Enabled: true, Project: missing},
	}}
	logger, logs := newLogCollector()

	sweepStaleScripts(cfg, "", logger) // must not panic

	if len(*logs) != 0 {
		t.Errorf("sweep over a missing directory logged %v, want no output at all", *logs)
	}
}

// --- scriptAction ---------------------------------------------------------

// TestScriptActionCreateFailureNamesDirectoryAndSkipsSystemTempFallback is
// the anti-regression for the ticket's core fix: pointing a runtime at a
// non-existent directory must produce an error naming that directory and the
// config key, and must NOT fall back to writing into the system temp dir —
// that silent fallback is the bug this ticket closes. The failure happens in
// os.CreateTemp before any command runs, so this needs no real "python"/
// "node" binary.
func TestScriptActionCreateFailureNamesDirectoryAndSkipsSystemTempFallback(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	sup := supervisor.New(nil)
	handler := scriptAction(sup, "unused-command", nil, missing, "py", "python")

	before := staleNamedEntries(t, os.TempDir())
	res := handler(map[string]interface{}{"code": "print(1)"})
	after := staleNamedEntries(t, os.TempDir())

	if !res.IsError {
		t.Fatalf("scriptAction with a non-existent directory: IsError = false, want true (text: %q)", res.Text)
	}
	if !strings.Contains(res.Text, missing) {
		t.Errorf("scriptAction error text = %q, want it to name the directory %q", res.Text, missing)
	}
	if !strings.Contains(res.Text, "runtimes.python.project") {
		t.Errorf("scriptAction error text = %q, want it to name the config key runtimes.python.project", res.Text)
	}
	if len(after) != len(before) {
		t.Errorf("scriptAction fell back to the system temp dir on create failure: before %v, after %v", before, after)
	}
}

// TestScriptActionWritesToConfiguredDirectoryAndCleansUpOnSuccess drives a
// real, successful scriptAction run without depending on uv or node: the
// configured "command" is plain POSIX sh. "${0%/*}" is shell parameter
// expansion (no external `dirname` needed) that strips the last path
// segment off $0, which `sh -c '...' <arg>` binds to the first positional
// argument — here, the script file scriptAction generated. Printing it
// proves the file was created inside dir, and re-reading dir afterward
// proves the deferred os.Remove cleaned it up.
func TestScriptActionWritesToConfiguredDirectoryAndCleansUpOnSuccess(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not in PATH — skipping")
	}
	dir := t.TempDir()
	sup := supervisor.New(nil)
	handler := scriptAction(sup, "sh", []string{"-c", `echo "${0%/*}"`}, dir, "txt", "test")

	res := handler(map[string]interface{}{"code": "irrelevant payload"})

	if res.IsError {
		t.Fatalf("scriptAction returned an error: %q", res.Text)
	}
	const prefix = "exit 0\n\n"
	if !strings.HasPrefix(res.Text, prefix) {
		t.Fatalf("scriptAction result = %q, want an %q prefix", res.Text, prefix)
	}
	if gotDir := strings.TrimSpace(strings.TrimPrefix(res.Text, prefix)); gotDir != dir {
		t.Errorf("script ran from directory %q, want %q", gotDir, dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("scriptAction left %v behind in %s after a successful run, want none (defer os.Remove should have cleaned up)", names, dir)
	}
}
