package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// fakeNpmEnv makes this test binary act as npm (fakeNpm) instead of running
// the tests. prepareNodeBridge starts runtimes.node.npm with npm's own
// arguments, which leave no room for a -test.run, and the shell-script npm of
// bridge_test.go does not start on Windows; the test binary starts everywhere,
// through the same RunOnceContext and job object as the real npm.
const fakeNpmEnv = "UNREAGENT_TEST_FAKE_NPM"

// TestMain answers as npm only when started the way prepareNodeBridge starts
// npm — the env var set and "install" first — so the variable left set in a
// shell does not turn go test into a run of no test at all.
func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeNpmEnv); dir != "" && len(os.Args) > 1 && os.Args[1] == "install" {
		os.Exit(fakeNpm(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeNpm appends its arguments as one line to dir/calls and answers the n-th
// call with dir/reply-<n>: the exit code on its first line, optionally
// followed by a delay before exiting ("1 1500ms"); "sleep" sleeps a minute
// instead, far past any install timeout a test sets. The output npm prints
// comes after that line. No reply file: exit 0 without output.
func fakeNpm(dir string, args []string) int {
	calls := filepath.Join(dir, "calls")
	f, err := os.OpenFile(calls, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake npm:", err)
		return 99
	}
	_, err = fmt.Fprintln(f, strings.Join(args, " "))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	b, rerr := os.ReadFile(calls)
	if err != nil || rerr != nil {
		fmt.Fprintln(os.Stderr, "fake npm:", err, rerr)
		return 99
	}
	n := strings.Count(string(b), "\n")
	reply, err := os.ReadFile(filepath.Join(dir, "reply-"+strconv.Itoa(n)))
	if err != nil {
		return 0
	}
	first, out, _ := strings.Cut(string(reply), "\n")
	fmt.Fprint(os.Stderr, out)
	if first == "sleep" {
		time.Sleep(time.Minute)
		return 0
	}
	exit, delay, _ := strings.Cut(first, " ")
	code, err := strconv.Atoi(exit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake npm: bad reply", first)
		return 99
	}
	if delay != "" {
		d, err := time.ParseDuration(delay)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fake npm: bad delay", delay)
			return 99
		}
		time.Sleep(d)
	}
	return code
}

// npm output as npm 10.9.2 printed it on Windows (k215), and the npm 9 form
// of a code line.
const (
	npmEdgesOut = "npm error Cannot read properties of null (reading 'edgesOut')\r\n" +
		"npm error A complete log of this run can be found in: C:\\Users\\u\\AppData\\Local\\npm-cache\\_logs\\2026-11-22T08_30_15_661Z-debug-0.log\r\n"
	npmEresolve = "npm error code ERESOLVE\r\n" +
		"npm error ERESOLVE unable to resolve dependency tree\r\n" +
		"npm error\r\n" +
		"npm error While resolving: eresolve-probe@1.0.0\r\n" +
		"npm error Found: react@17.0.2\r\n"
	npmRefused = "npm error code ECONNREFUSED\r\n" +
		"npm error syscall connect\r\n" +
		"npm error errno ECONNREFUSED\r\n" +
		"npm error FetchError: request to http://127.0.0.1:18898/@vitest%2fcoverage-v8 failed, reason: connect ECONNREFUSED 127.0.0.1:18898\r\n"
	npmScriptFailed = "\r\n> script-probe@1.0.0 postinstall\r\n> node -e \"process.exit(3)\"\r\n\r\n" +
		"npm error code 3\r\n" +
		"npm error path C:\\probe\r\n" +
		"npm error command failed\r\n"
	npm9Eresolve = "npm ERR! code ERESOLVE\nnpm ERR! ERESOLVE could not resolve\n"
)

func TestNpmErrorCode(t *testing.T) {
	for _, tc := range []struct{ name, output, want string }{
		{"crash inside npm", npmEdgesOut, ""},
		{"peer conflict", npmEresolve, "ERESOLVE"},
		{"registry unreachable", npmRefused, "ECONNREFUSED"},
		{"install script", npmScriptFailed, "3"},
		{"npm 9", npm9Eresolve, "ERESOLVE"},
		{"success", "\nadded 89 packages, and audited 90 packages in 4s\n", ""},
		{"no output", "", ""},
	} {
		if got := npmErrorCode(tc.output); got != tc.want {
			t.Errorf("%s: npmErrorCode = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// npmFixture is a node bridge (bridge/index.js with a package.json, no
// node_modules) whose npm is fakeNpm, answering from dir.
type npmFixture struct {
	dir string
	cfg *config.Config
}

func newNpmFixture(t *testing.T, replies ...string) *npmFixture {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	f := &npmFixture{dir: t.TempDir(), cfg: &config.Config{}}
	f.cfg.Runtimes.Node.Npm = exe
	t.Setenv(fakeNpmEnv, f.dir)
	if err := os.MkdirAll(filepath.Join(f.dir, "bridge"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.js", "package.json"} {
		if err := os.WriteFile(filepath.Join(f.dir, "bridge", name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i, r := range replies {
		if err := os.WriteFile(filepath.Join(f.dir, "reply-"+strconv.Itoa(i+1)), []byte(r), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// prepare runs prepareNodeBridge and returns its result, the log and the
// argument lines npm was started with.
func (f *npmFixture) prepare(t *testing.T) (ok bool, log string, calls []string) {
	t.Helper()
	var l syncLog
	_, ok = prepareNodeBridge(context.Background(), supervisor.New(l.log), f.cfg, "toolkit", []string{"bridge/index.js"}, f.dir, l.log)
	return ok, l.String(), f.calls(t)
}

// calls returns the argument lines npm was started with so far.
func (f *npmFixture) calls(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "calls"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if s := strings.TrimSpace(strings.ReplaceAll(string(b), "\r", "")); s != "" {
		return strings.Split(s, "\n")
	}
	return nil
}

// k215: npm 10.9.2 crashes inside its tree building on the UE LLM Toolkit
// bridge (no error code) and installs it with --legacy-peer-deps; a peer
// conflict (ERESOLVE) is what that flag is for. Either gets one retry with it.
func TestPrepareNodeBridgeRetriesWithLegacyPeerDeps(t *testing.T) {
	for _, tc := range []struct{ name, output, named string }{
		{"crash", npmEdgesOut, "(no npm error code)"},
		{"ERESOLVE", npmEresolve, "(ERESOLVE)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNpmFixture(t, "1\n"+tc.output, "0\nadded 89 packages\n")
			ok, log, calls := f.prepare(t)
			if want := []string{"install --no-save", "install --no-save --legacy-peer-deps"}; strings.Join(calls, "|") != strings.Join(want, "|") {
				t.Fatalf("npm calls = %q, want %q:\n%s", calls, want, log)
			}
			if !ok || strings.Contains(log, "WARN") {
				t.Fatalf("the retry installed the bridge, yet it is not usable or got a WARN (ok %v):\n%s", ok, log)
			}
			notice := "npm install --no-save exit 1 " + tc.named + " — trying once more with --legacy-peer-deps"
			if !strings.Contains(log, notice) || !strings.Contains(log, strings.TrimSpace(strings.SplitN(tc.output, "\n", 2)[0])) {
				t.Fatalf("the log does not say why the retry ran (%q, with npm's output):\n%s", notice, log)
			}
			if !strings.Contains(log, "dependencies installed (npm install --no-save --legacy-peer-deps)") {
				t.Fatalf("the log does not name the install that worked:\n%s", log)
			}
		})
	}
}

// k215: a plain install that works is the only one — the flag stays out, so
// the peers npm installs on its own are kept.
func TestPrepareNodeBridgePlainInstallKeepsPeers(t *testing.T) {
	f := newNpmFixture(t, "0\nadded 90 packages\n")
	ok, log, calls := f.prepare(t)
	if !ok || len(calls) != 1 || calls[0] != "install --no-save" {
		t.Fatalf("want one plain npm install, got %q (ok %v):\n%s", calls, ok, log)
	}
	if !strings.Contains(log, "dependencies installed (npm install --no-save)") {
		t.Fatalf("the log does not name the install:\n%s", log)
	}
}

// k215: a failure that names another npm error code is not about peers — no
// retry, which would only repeat it (an unreachable registry costs npm
// 10.9.2 141 s per try).
func TestPrepareNodeBridgeNoRetryForOtherErrors(t *testing.T) {
	for _, tc := range []struct{ name, output string }{
		{"registry unreachable", npmRefused},
		{"install script", npmScriptFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNpmFixture(t, "1\n"+tc.output, "0\n")
			ok, log, calls := f.prepare(t)
			if len(calls) != 1 {
				t.Fatalf("npm calls = %q, want the plain install only:\n%s", calls, log)
			}
			if ok || !strings.Contains(log, "WARN MCP server 'toolkit': npm install --no-save exit 1 — bridge will not start.") {
				t.Fatalf("want the failed install reported and the bridge left out (ok %v):\n%s", ok, log)
			}
		})
	}
}

// k215: when the retry fails too, the WARN names the retry and carries its
// output; the first attempt's output is in the line before it.
func TestPrepareNodeBridgeRetryFailsToo(t *testing.T) {
	f := newNpmFixture(t, "1\n"+npmEdgesOut, "1\n"+npmRefused)
	ok, log, calls := f.prepare(t)
	if len(calls) != 2 || ok {
		t.Fatalf("want two npm calls and an unusable bridge, got %q (ok %v):\n%s", calls, ok, log)
	}
	warn := "WARN MCP server 'toolkit': npm install --no-save --legacy-peer-deps exit 1 — bridge will not start.\n"
	if !strings.Contains(log, warn+"npm error code ECONNREFUSED") {
		t.Fatalf("the WARN does not name the retry with its output (%q):\n%s", warn, log)
	}
	if strings.Count(log, "edgesOut") != 1 {
		t.Fatalf("the first attempt's output belongs in the retry notice, once:\n%s", log)
	}
}

// k215: the retry runs within the install's one bridgeInstallTimeout, and a
// timed-out install gets no retry.
func TestPrepareNodeBridgeRetrySharesTheTimeout(t *testing.T) {
	old := bridgeInstallTimeout
	bridgeInstallTimeout = 2 * time.Second
	defer func() { bridgeInstallTimeout = old }()
	dir := func(f *npmFixture) string { return filepath.Join(f.dir, "bridge") }
	for _, tc := range []struct {
		name    string
		replies []string
		calls   int
		warn    func(f *npmFixture) string
	}{
		{"plain install times out", []string{"sleep\n"}, 1, func(f *npmFixture) string {
			return "WARN MCP server 'toolkit': npm install --no-save timed out after 2s — bridge will not start. Registry or proxy unreachable? Run npm install --no-save in " + dir(f) + " by hand."
		}},
		// The plain install uses 1.5 s of the 2: a retry with a timeout of
		// its own would end at 3.5 s, past the bound below. The WARN names
		// the retry, not the plain install that crashes.
		{"retry times out", []string{"1 1500ms\n" + npmEdgesOut, "sleep\n"}, 2, func(f *npmFixture) string {
			return "WARN MCP server 'toolkit': npm install --no-save --legacy-peer-deps timed out: the plain install and it had 2s together — bridge will not start. Registry or proxy unreachable? Run npm install --no-save --legacy-peer-deps in " + dir(f) + " by hand."
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNpmFixture(t, tc.replies...)
			start := time.Now()
			ok, log, calls := f.prepare(t)
			// The job object (Windows) or the kill ends npm at the deadline.
			if d := time.Since(start); d > bridgeInstallTimeout+time.Second {
				t.Fatalf("the install took %s with a timeout of %s:\n%s", d, bridgeInstallTimeout, log)
			}
			if len(calls) != tc.calls || ok || !strings.Contains(log, tc.warn(f)) {
				t.Fatalf("want %d npm calls and %q, got %q (ok %v):\n%s", tc.calls, tc.warn(f), calls, ok, log)
			}
		})
	}
}

// k215 review: a plain install that the timeout or Ctrl-C ends gets no
// retry, even when the output it left names no npm error code — the retry
// is for an npm that exited on its own. Only err tells the two apart on
// Linux, where the killed npm's exit code is -1. On Windows, closing the job
// object ends it with exit code 0, so there the exit code alone already
// keeps the retry out.
func TestPrepareNodeBridgeNoRetryAfterTimeoutOrCancel(t *testing.T) {
	old := bridgeInstallTimeout
	defer func() { bridgeInstallTimeout = old }()
	// npm prints the crash, then hangs: only err tells this from a crash.
	hang := "sleep\n" + npmEdgesOut
	t.Run("timeout", func(t *testing.T) {
		bridgeInstallTimeout = time.Second
		f := newNpmFixture(t, hang)
		ok, log, calls := f.prepare(t)
		if ok || len(calls) != 1 || strings.Contains(log, "trying once more") || !strings.Contains(log, "npm install --no-save timed out after 1s") {
			t.Fatalf("a timed-out install must be reported as such, with no retry (calls %q, ok %v):\n%s", calls, ok, log)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		bridgeInstallTimeout = time.Minute
		f := newNpmFixture(t, hang)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var l syncLog
		done := make(chan bool)
		go func() {
			_, ok := prepareNodeBridge(ctx, supervisor.New(l.log), f.cfg, "toolkit", []string{"bridge/index.js"}, f.dir, l.log)
			done <- ok
		}()
		waitForFile(t, filepath.Join(f.dir, "calls"), 10*time.Second)
		time.Sleep(200 * time.Millisecond) // let npm print its output
		cancel()
		var ok bool
		select {
		case ok = <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("prepareNodeBridge still running 5s after the cancel:\n%s", l.String())
		}
		log := l.String()
		if ok || len(f.calls(t)) != 1 || strings.Contains(log, "trying once more") || !strings.Contains(log, "npm install aborted") {
			t.Fatalf("a cancelled install must be logged as an abort, with no retry (calls %q, ok %v):\n%s", f.calls(t), ok, log)
		}
	})
}
