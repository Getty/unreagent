package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type onceResult struct {
	res CommandResult
	err error
}

// lineLog is a Logger that keeps every line. The supervisor logs from more
// than one goroutine.
type lineLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *lineLog) log(s string) {
	l.mu.Lock()
	l.lines = append(l.lines, s)
	l.mu.Unlock()
}

func (l *lineLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// readyFile returns the path the "hang" helper writes its PID to and makes
// sure that helper dies with the test: a failing test leaves the process it
// started behind otherwise, sleeping for helperHang.
func readyFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ready")
	t.Cleanup(func() {
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	})
	return path
}

// runOnceAsync runs RunOnceContext in the background, so a test can bound how
// long it waits for it instead of hanging with it.
func runOnceAsync(ctx context.Context, sup *Supervisor, spec ServiceSpec, env []string) <-chan onceResult {
	done := make(chan onceResult, 1)
	go func() {
		res, err := sup.RunOnceContext(ctx, spec.Command, spec.Args, "", env, spec.Name)
		done <- onceResult{res, err}
	}()
	return done
}

// waitForReady waits until the helper created its ready file. A run that ends
// before that is reported with its result — it never got to the point the test
// wants to cancel at.
func waitForReady(t *testing.T, path string, done <-chan onceResult) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case r := <-done:
			t.Fatalf("command ended before it was up: err=%v exit=%d output=%q", r.err, r.res.ExitCode, r.res.Output)
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("helper did not come up within 10s")
}

// k54/k68: a one-shot command bounded by a context must end when the context
// does. The bridge npm install ran under plain RunOnce, so a stalled registry
// kept the launcher from ever starting the editor and Ctrl-C could not end it.
// The child is killed, not abandoned, and what it wrote before is kept — that
// output is what tells the user where it got stuck.
func TestRunOnceContextKillsChildOnCancel(t *testing.T) {
	spec := helperSpec(t, "hang", "hang")
	ready := readyFile(t)
	env := append(append([]string(nil), spec.Env...), helperReadyEnv+"="+ready)
	sup := New(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runOnceAsync(ctx, sup, spec, env)
	waitForReady(t, ready, done)

	cancel()
	start := time.Now()
	select {
	case r := <-done:
		if !errors.Is(r.err, context.Canceled) {
			t.Fatalf("err = %v, want one wrapping context.Canceled", r.err)
		}
		if !strings.Contains(r.res.Output, helperHeadMarker) {
			t.Fatalf("output written before the cancel was lost: %q", r.res.Output)
		}
		t.Logf("RunOnceContext returned %s after the cancel", time.Since(start).Round(time.Millisecond))
	case <-time.After(5 * time.Second):
		t.Fatalf("RunOnceContext still running 5s after the cancel — the child (sleeps %s) was not stopped", helperHang)
	}
}

// A context that is already done must not start the command at all: Ctrl-C
// during the preparation means the user wants out, not one more npm run. Only
// the log can tell: started anyway, the command is killed by the cancel path
// at once, long before it would show any other sign of life.
func TestRunOnceContextDoneStartsNothing(t *testing.T) {
	spec := helperSpec(t, "hang", "hang")
	env := append(append([]string(nil), spec.Env...), helperReadyEnv+"="+readyFile(t))
	var log lineLog
	sup := New(log.log)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	select {
	case r := <-runOnceAsync(ctx, sup, spec, env):
		if !errors.Is(r.err, context.Canceled) {
			t.Fatalf("err = %v, want one wrapping context.Canceled", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunOnceContext ran the command although its context was already done")
	}
	if strings.Contains(log.String(), "[hang] starting:") {
		t.Fatalf("the command was started although its context was already done:\n%s", log.String())
	}
}

// k68: Ctrl-C during the bridge preparation cancels the context before the
// services start, and Start still started the editor — only to kill it again
// on the next loop turn ("[ue] started", then "[ue] stopping …").
func TestStartWithDoneContextStartsNothing(t *testing.T) {
	spec := helperSpec(t, "x", "hang")
	spec.Env = append(spec.Env, helperReadyEnv+"="+readyFile(t))
	var log lineLog
	sup := New(log.log)
	sup.AddService(spec)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	wg.Wait()

	if strings.Contains(log.String(), "started (pid") {
		t.Fatalf("service started although the context was already done:\n%s", log.String())
	}
}

// k182: a due restart timer and the shutdown, ready together, reached the
// loop's select, which picks one at random. Picking the timer ran PreStart
// and spawned the editor once more, only for the next turn to kill it. Once
// the context is done, the timer starts nothing: no PreStart, no spawn. The
// delayed first start (a start delay) is the same timer.
//
// The seam: backoffTaken cancels the context once the loop has taken the
// timer. That is the state select leaves behind when it picks the timer over
// a shutdown that is ready with it, and it makes the case deterministic.
func TestShutdownAtADueTimerStartsNothing(t *testing.T) {
	cases := []struct {
		name       string
		startDelay bool   // the timer is the delayed first start
		automatic  string // PreStart Automatic flags of every start
		spawned    int
	}{
		{"restart", false, "[false]", 1},
		{"delayed first start", true, "[]", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var pre preStarts
			var log lineLog
			sup := New(log.log)
			spec, _ := runsSpec(t, "f", 0) // run 1 fails at once, restarted after 10ms
			if c.startDelay {
				spec.StartDelay = 10 * time.Millisecond
			}
			spec.PreStart = pre.record
			if err := sup.AddService(spec); err != nil {
				t.Fatalf("AddService: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sup.backoffTaken = cancel
			var wg sync.WaitGroup
			sup.Start(ctx, &wg)
			ended := make(chan struct{})
			go func() { wg.Wait(); close(ended) }()
			select {
			case <-ended:
			case <-time.After(10 * time.Second):
				t.Fatal("the loop did not end after the shutdown")
			}
			if got := pre.String(); got != c.automatic {
				t.Fatalf("PreStart Automatic = %s, want %s: the timer ran a start after the shutdown", got, c.automatic)
			}
			if got := strings.Count(log.String(), "started (pid"); got != c.spawned {
				t.Fatalf("%d processes spawned, want %d: the timer spawned one after the shutdown\n%s", got, c.spawned, log.String())
			}
		})
	}
}

// k182: a shutdown that came while PreStart ran still spawned the process
// once, only for the next turn to kill it. The editor's PreStart shells out
// (the crash reporter, Saved/Crashes), so that window can last seconds. A
// start whose context is done once PreStart returns spawns nothing.
//
// PreStart cancels the context itself, on the first start or on the restart
// after run 1, so the start always finds the shutdown once PreStart returns.
func TestShutdownDuringPreStartSpawnsNothing(t *testing.T) {
	cases := []struct {
		name    string
		cancel  int // the PreStart call that cancels
		spawned int
	}{
		{"first start", 1, 0},
		{"restart", 2, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var log lineLog
			sup := New(log.log)
			spec, _ := runsSpec(t, "f", 0) // run 1 fails at once, restarted after 10ms
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0 // PreStart runs on the loop goroutine only
			spec.PreStart = func(Start) {
				if calls++; calls == c.cancel {
					cancel()
				}
			}
			if err := sup.AddService(spec); err != nil {
				t.Fatalf("AddService: %v", err)
			}
			var wg sync.WaitGroup
			sup.Start(ctx, &wg)
			ended := make(chan struct{})
			go func() { wg.Wait(); close(ended) }()
			select {
			case <-ended:
			case <-time.After(10 * time.Second):
				t.Fatal("the loop did not end after the shutdown")
			}
			if calls != c.cancel {
				t.Fatalf("PreStart ran %d times, want %d", calls, c.cancel)
			}
			out := log.String()
			if got := strings.Count(out, "started (pid"); got != c.spawned {
				t.Fatalf("%d processes spawned, want %d: the start went on after a shutdown during its PreStart\n%s", got, c.spawned, out)
			}
			if !strings.Contains(out, "not started: shutting down") {
				t.Fatalf("no line says the start was dropped for the shutdown:\n%s", out)
			}
		})
	}
}

// k182: a start or restart taken in the same turn as the shutdown started the
// process after it, as the due timer did: one waiting when the process ended
// on its own or when its restart came due, both of which the loop takes
// ahead of the next turn (k179). It starts nothing and is answered with the
// shutdown as the reason.
//
// The call cancels the context before it is sent, so the loop finds the
// shutdown when it takes the call at the seam.
func TestControlCallTakenAtTheShutdownStartsNothing(t *testing.T) {
	cases := []struct {
		name, call string
		atExit     bool // the call waits at the exit, not at the due restart
	}{
		{"start at an exit", "start", true},
		{"restart at an exit", "restart", true},
		{"start at a due restart", "start", false},
		{"restart at a due restart", "restart", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var pre preStarts
			var log lineLog
			sup := New(log.log)
			spec, _ := runsSpec(t, "f", 0) // run 1 fails at once, restarted after 10ms
			spec.PreStart = pre.record
			seam := &sup.backoffTaken
			if c.atExit {
				spec.RestartDelay = time.Hour // the restart the exit arms never comes due
				seam = &sup.exitTaken
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			call := controlCall(sup, c.call)
			r, _ := runWithQueuedCall(t, ctx, sup, spec, seam, func() (ServiceStatus, error) {
				cancel()
				return call()
			})
			// The loop runs the start before it answers, so these need no wait.
			if got := pre.String(); got != "[false]" {
				t.Fatalf("PreStart Automatic = %s, want [false] (run 1 only): the %s ran a start after the shutdown", got, c.call)
			}
			if got := strings.Count(log.String(), "started (pid"); got != 1 {
				t.Fatalf("%d processes spawned, want 1 (run 1 only): the %s spawned one after the shutdown\n%s", got, c.call, log.String())
			}
			// ue_start, ue_restart and the console print "did not start: " and
			// LastError: it has to name the shutdown.
			if r.st.Running || r.st.LastError != "the launcher is shutting down" {
				t.Fatalf("%s answered %+v, want not running, LastError %q", c.call, r.st, "the launcher is shutting down")
			}
		})
	}
}
