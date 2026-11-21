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
