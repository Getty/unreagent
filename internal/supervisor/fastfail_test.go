package supervisor

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// helperRunsEnv names the file the "runs" helper appends one line to per
	// run; helperPatternEnv how each run ends: 'f' fails at once, 's' fails
	// after the duration in helperSlowEnv, 'o' exits 0 at once. Runs past the
	// end of the pattern repeat its last letter.
	helperRunsEnv    = "UNREAGENT_TEST_RUNS"
	helperPatternEnv = "UNREAGENT_TEST_PATTERN"
	helperSlowEnv    = "UNREAGENT_TEST_SLOW"
)

// helperRuns is the "runs" helper mode: it counts its own runs in a file, so
// one service can fail fast on some starts and slow on others.
func helperRuns(out *bufio.Writer) {
	path, pattern := os.Getenv(helperRunsEnv), os.Getenv(helperPatternEnv)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = f.WriteString("run\n")
		_ = f.Close()
	}
	b, _ := os.ReadFile(path)
	n := strings.Count(string(b), "\n")
	if pattern == "" || n == 0 {
		out.Flush()
		os.Exit(2)
	}
	if n > len(pattern) {
		n = len(pattern)
	}
	switch pattern[n-1] {
	case 'o':
		out.Flush()
		os.Exit(0)
	case 's':
		d, _ := time.ParseDuration(os.Getenv(helperSlowEnv))
		time.Sleep(d)
	}
	out.Flush()
	os.Exit(1)
}

// runsSpec is a service running the "runs" helper with the given pattern and
// slow-run duration, restarted on failure without a bound of its own. It
// returns the spec and a func that counts the runs so far.
func runsSpec(t *testing.T, pattern string, slow time.Duration) (ServiceSpec, func() int) {
	t.Helper()
	runs := filepath.Join(t.TempDir(), "runs")
	spec := helperSpec(t, "svc", "runs")
	spec.Env = append(spec.Env, helperRunsEnv+"="+runs, helperPatternEnv+"="+pattern, helperSlowEnv+"="+slow.String())
	spec.Restart = "on-failure"
	spec.RestartDelay = 10 * time.Millisecond
	count := func() int {
		b, _ := os.ReadFile(runs)
		return strings.Count(string(b), "\n")
	}
	return spec, count
}

// startService runs sup until the test ends and returns the OnExit events.
func startService(t *testing.T, sup *Supervisor, spec ServiceSpec) <-chan Exit {
	t.Helper()
	exits := make(chan Exit, 4)
	spec.OnExit = func(e Exit) { exits <- e }
	sup.AddService(spec)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })
	return exits
}

func waitExit(t *testing.T, exits <-chan Exit, within time.Duration, what string) Exit {
	t.Helper()
	select {
	case e := <-exits:
		return e
	case <-time.After(within):
		t.Fatalf("OnExit did not fire within %s: %s", within, what)
		return Exit{}
	}
}

func restartsOf(t *testing.T, sup *Supervisor) int {
	t.Helper()
	st := sup.Status()
	if len(st) != 1 || st[0].Unresponsive {
		t.Fatalf("unexpected status: %+v", st)
	}
	return st[0].Restarts
}

// k70: a service that keeps dying right after its start used to be restarted
// every RestartDelay forever (maxRestarts 0 = unlimited), so OnExit never
// fired: a headless run hung and the exit prompt was unreachable. After three
// such exits in a row the supervisor gives up and reports a failed start.
func TestFastFailGivesUp(t *testing.T) {
	var log lineLog
	sup := New(log.log)
	spec, runs := runsSpec(t, "f", 0)
	exits := startService(t, sup, spec)

	e := waitExit(t, exits, 10*time.Second, "a service failing at every start was restarted without bound")
	if e.Success || !e.StartFailed {
		t.Fatalf("give-up reported as %+v, want a failed start", e)
	}
	if got := runs(); got != fastFailLimit {
		t.Fatalf("service ran %d times, want %d before giving up", got, fastFailLimit)
	}
	if got := restartsOf(t, sup); got != fastFailLimit-1 {
		t.Fatalf("restarts = %d, want %d", got, fastFailLimit-1)
	}
	if !strings.Contains(log.String(), "giving up") {
		t.Fatalf("no give-up line in the log:\n%s", log.String())
	}
}

// The window is about starts, not about crashes: a service that dies only
// after it ran for a while is still restarted without bound.
func TestSlowFailKeepsRestarting(t *testing.T) {
	sup := New(nil)
	sup.fastFailWindow = 50 * time.Millisecond
	spec, _ := runsSpec(t, "s", 300*time.Millisecond)
	exits := startService(t, sup, spec)

	// fastFailLimit restarts: the third exit is where a fast service is given
	// up on, so getting past it proves the slow one was not.
	deadline := time.Now().Add(20 * time.Second)
	for restartsOf(t, sup) < fastFailLimit {
		select {
		case e := <-exits:
			t.Fatalf("gave up on a service that ran longer than the window each time: %+v", e)
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d restarts in 20s", restartsOf(t, sup))
		}
	}
}

// A run that outlasts the window resets the count: the rule is "N fast exits
// in a row", not "N fast exits in total".
func TestFastFailCountResetsAfterALongerRun(t *testing.T) {
	sup := New(nil)
	// Wide enough that an immediate exit is always inside it, also under -race
	// on a loaded box; the slow run outlasts it.
	sup.fastFailWindow = 500 * time.Millisecond
	spec, runs := runsSpec(t, "ffsfff", 800*time.Millisecond)
	exits := startService(t, sup, spec)

	e := waitExit(t, exits, 20*time.Second, "the service was never given up on")
	if !e.StartFailed {
		t.Fatalf("give-up reported as %+v, want a failed start", e)
	}
	if got := runs(); got != 6 {
		t.Fatalf("gave up after run %d, want 6 (fast, fast, slow, then three fast in a row)", got)
	}
}

// Only failures count: under restart: always a job that finishes at once with
// exit 0 is what was asked for, not a crash loop, and is restarted without
// bound. Asserted like TestSlowFailKeepsRestarting: getting past fastFailLimit
// restarts without an OnExit proves there was no give-up.
func TestQuickCleanExitsKeepRestarting(t *testing.T) {
	sup := New(nil)
	spec, _ := runsSpec(t, "o", 0)
	spec.Restart = "always"
	exits := startService(t, sup, spec)

	deadline := time.Now().Add(20 * time.Second)
	for restartsOf(t, sup) < 2*fastFailLimit {
		select {
		case e := <-exits:
			t.Fatalf("gave up on a service that exited 0 each time: %+v", e)
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d restarts in 20s", restartsOf(t, sup))
		}
	}
}

// A clean exit resets the count like a longer run does: two failures, a clean
// exit and two more failures are not three failures in a row. The pattern's
// last letter repeats, so the sixth run is the third failure in a row and the
// give-up must come exactly there — which also proves none came earlier.
func TestCleanExitResetsTheFailCount(t *testing.T) {
	sup := New(nil)
	spec, runs := runsSpec(t, "ffofff", 0)
	spec.Restart = "always"
	exits := startService(t, sup, spec)

	e := waitExit(t, exits, 20*time.Second, "the service was never given up on")
	if !e.StartFailed {
		t.Fatalf("give-up reported as %+v, want a failed start", e)
	}
	if got := runs(); got != 6 {
		t.Fatalf("gave up after run %d, want 6 (fail, fail, exit 0, then three fails in a row)", got)
	}
}

// A manual start or restart resets the count: after a give-up, a start from the
// console or ue_start gets the full fastFailLimit runs again, not a single one.
func TestManualStartResetsTheFailCount(t *testing.T) {
	for _, how := range []string{"start", "restart"} {
		t.Run(how, func(t *testing.T) {
			sup := New(nil)
			spec, runs := runsSpec(t, "f", 0)
			exits := startService(t, sup, spec)
			waitExit(t, exits, 10*time.Second, "the first give-up")

			var err error
			if how == "start" {
				_, err = sup.StartService("svc")
			} else {
				_, err = sup.RestartService("svc")
			}
			if err != nil {
				t.Fatalf("manual %s: %v", how, err)
			}
			e := waitExit(t, exits, 10*time.Second, "the second give-up")
			if !e.StartFailed {
				t.Fatalf("second give-up reported as %+v, want a failed start", e)
			}
			if got := runs(); got != 2*fastFailLimit {
				t.Fatalf("ran %d times in all, want %d: a manual %s must give %d fresh runs", got, 2*fastFailLimit, how, fastFailLimit)
			}
		})
	}
}

// Design choice for k70: StartFailed means "the last run never got going" — a
// failed start, or a failure within the window of its start. It does not
// depend on how many runs came before or on what ended the restarts:
// restart: never, a used-up maxRestarts and the give-up report it alike. A
// last run that failed after the window is a crash, not a failed start.
func TestStartFailedReportsTheLastRun(t *testing.T) {
	const slow = 300 * time.Millisecond
	cases := []struct {
		name        string
		pattern     string
		restart     string
		maxRestarts int
		window      time.Duration // 0 = the default
		wantRuns    int
		want        Exit
	}{
		{"restart never, immediate failure", "f", "never", 0, 0, 1, Exit{StartFailed: true}},
		{"restart never, failure after the window", "s", "never", 0, 50 * time.Millisecond, 1, Exit{}},
		{"maxRestarts 1, quick failures", "f", "on-failure", 1, 0, 2, Exit{StartFailed: true}},
		// The first run's failure is quick; only the last one decides.
		{"maxRestarts 1, last run failed after the window", "fs", "on-failure", 1, 50 * time.Millisecond, 2, Exit{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sup := New(nil)
			if c.window > 0 {
				sup.fastFailWindow = c.window
			}
			spec, runs := runsSpec(t, c.pattern, slow)
			spec.Restart = c.restart
			spec.MaxRestarts = c.maxRestarts
			exits := startService(t, sup, spec)
			if got := waitExit(t, exits, 10*time.Second, c.name); got != c.want {
				t.Fatalf("OnExit got %+v, want %+v", got, c.want)
			}
			if got := runs(); got != c.wantRuns {
				t.Fatalf("service ran %d times, want %d", got, c.wantRuns)
			}
		})
	}
}

// An explicit maxRestarts keeps its meaning: the fast-fail rule only bounds the
// unlimited default, it does not cut a configured bound short.
func TestExplicitMaxRestartsKeepsItsBound(t *testing.T) {
	sup := New(nil)
	spec, runs := runsSpec(t, "f", 0)
	spec.MaxRestarts = 5
	exits := startService(t, sup, spec)

	e := waitExit(t, exits, 20*time.Second, "maxRestarts was not honoured")
	if e.Success {
		t.Fatalf("exhausted restarts reported as %+v", e)
	}
	if got := runs(); got != 6 {
		t.Fatalf("service ran %d times, want 6 (start + maxRestarts 5)", got)
	}
}

// A program that exists but cannot be executed fails in Start, before there is
// a process to time. That is the fastest fail there is and it must count too:
// with the unlimited default it used to be retried forever.
func TestStartErrorGivesUpWithoutMaxRestarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bits work differently on Windows")
	}
	bin := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sup := New(nil)
	exits := startService(t, sup, ServiceSpec{
		Name:         "svc",
		Command:      bin,
		Autostart:    true,
		Restart:      "on-failure",
		RestartDelay: 10 * time.Millisecond,
	})

	e := waitExit(t, exits, 10*time.Second, "a start that fails every time was retried without bound")
	if e.Success || !e.StartFailed {
		t.Fatalf("give-up reported as %+v, want a failed start", e)
	}
	if got := restartsOf(t, sup); got != fastFailLimit-1 {
		t.Fatalf("restarts = %d, want %d", got, fastFailLimit-1)
	}
}

// k152: a failed start that the policy retries is reported to OnStartRetry,
// once per retry, with the start error and the retry's number. The start the
// policy gives up on is OnExit's, and a restart after an exit follows no
// failed start, so neither calls it. The hook runs on the loop, so every call
// is in by the time OnExit fires.
func TestOnStartRetryReportsEachRetriedStartError(t *testing.T) {
	t.Run("start error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("executable bits work differently on Windows")
		}
		bin := filepath.Join(t.TempDir(), "not-executable")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		retries := make(chan Retry, 8)
		sup := New(nil)
		exits := startService(t, sup, ServiceSpec{
			Name: "svc", Command: bin, Autostart: true, Restart: "on-failure",
			MaxRestarts: 2, RestartDelay: 10 * time.Millisecond,
			OnStartRetry: func(r Retry) { retries <- r },
		})
		waitExit(t, exits, 10*time.Second, "a start that fails every time was retried without bound")
		lastErr := sup.Status()[0].LastError
		if !strings.Contains(lastErr, bin) {
			t.Fatalf("lastError = %q, want the start error naming %s", lastErr, bin)
		}
		if got := len(retries); got != 2 {
			t.Fatalf("OnStartRetry fired %d times, want once per retry: 2 (maxRestarts)", got)
		}
		for i := 1; i <= 2; i++ {
			want := Retry{Err: lastErr, Restart: i, MaxRestarts: 2, Delay: 10 * time.Millisecond}
			if got := <-retries; got != want {
				t.Fatalf("retry %d reported as %+v, want %+v", i, got, want)
			}
		}
	})
	t.Run("exits", func(t *testing.T) {
		retries := make(chan Retry, 8)
		sup := New(nil)
		spec, _ := runsSpec(t, "f", 0) // exit 1 at once, until the fast-fail give-up
		spec.OnStartRetry = func(r Retry) { retries <- r }
		exits := startService(t, sup, spec)
		waitExit(t, exits, 10*time.Second, "a service failing at every start was restarted without bound")
		if got := restartsOf(t, sup); got != fastFailLimit-1 {
			t.Fatalf("restarts = %d, want %d: the premise is restarts after exits", got, fastFailLimit-1)
		}
		if got := len(retries); got != 0 {
			t.Fatalf("OnStartRetry fired %d times for restarts after an exit, want none: %+v", got, <-retries)
		}
	})
}

// StartFailed separates "never got going" from "ran and then ended": only the
// first means the service is not coming up at all. A start that fails in
// Start reports it whichever way the retries end. (A quick crash reports it
// too — TestStartFailedReportsTheLastRun.)
func TestExitReportsStartFailure(t *testing.T) {
	requireBin(t, "true")
	notExecutable := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cases := []struct {
		name        string
		command     string
		restart     string
		maxRestarts int
		want        Exit
	}{
		{"program not found", filepath.Join(t.TempDir(), "does-not-exist"), "never", 0, Exit{StartFailed: true}},
		{"start error, maxRestarts used up", notExecutable, "on-failure", 1, Exit{StartFailed: true}},
		{"exit 0", "true", "on-failure", 0, Exit{Success: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.command == notExecutable && runtime.GOOS == "windows" {
				t.Skip("executable bits work differently on Windows")
			}
			exits := startService(t, New(nil), ServiceSpec{
				Name: "svc", Command: c.command, Autostart: true, Restart: c.restart,
				MaxRestarts: c.maxRestarts, RestartDelay: 10 * time.Millisecond,
			})
			if got := waitExit(t, exits, 10*time.Second, c.name); got != c.want {
				t.Fatalf("OnExit got %+v, want %+v", got, c.want)
			}
		})
	}
}
