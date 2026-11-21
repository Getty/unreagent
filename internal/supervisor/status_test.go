package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// A service whose control loop is blocked used to disappear from Status()
// entirely, so the agent could not tell "no editor configured" from "the
// control loop is stuck" — and acted on it. It must be reported as
// unresponsive, and the services must be queried concurrently instead of
// paying one timeout after the other.
func TestStatusReportsUnresponsiveServicesConcurrently(t *testing.T) {
	requireBin(t, "sleep")
	const timeout = 500 * time.Millisecond
	release := make(chan struct{})
	blocked := []string{"stuck1", "stuck2", "stuck3"}

	sup := New(nil)
	sup.ctrlTimeout = timeout
	for _, name := range blocked {
		sup.AddService(ServiceSpec{
			Name:      name,
			Command:   "sleep",
			Args:      []string{"30"},
			Autostart: true,
			Restart:   "never",
			// PreStart runs inside the control loop, so the loop cannot answer
			// while it blocks — the same shape as a hung editor start.
			PreStart: func(bool) { <-release },
		})
	}
	sup.AddService(ServiceSpec{Name: "idle", Command: "sleep", Args: []string{"30"}, Restart: "never"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	time.Sleep(100 * time.Millisecond) // let the blocked loops reach PreStart

	began := time.Now()
	st := sup.Status()
	elapsed := time.Since(began)

	if len(st) != len(blocked)+1 {
		t.Fatalf("Status dropped services: got %d entries, want %d", len(st), len(blocked)+1)
	}
	for i, name := range blocked {
		if st[i].Name != name {
			t.Fatalf("entry %d is %q, want %q (registration order)", i, st[i].Name, name)
		}
		if !st[i].Unresponsive {
			t.Fatalf("service %q is blocked but not reported as unresponsive: %+v", name, st[i])
		}
	}
	last := st[len(st)-1]
	if last.Name != "idle" || last.Unresponsive {
		t.Fatalf("responsive service reported wrongly: %+v", last)
	}
	if want := time.Duration(len(blocked)) * timeout; elapsed >= want {
		t.Fatalf("Status queried sequentially: took %v, one timeout is %v", elapsed, timeout)
	}

	close(release)
	cancel()
	wg.Wait()
}

// k72: a start that failed was reported like any other snapshot — running
// false, nothing saying why — and the error reached only the launcher log, so
// ue_start on a missing editor looked like a success to the agent. The snapshot
// StartService and RestartService return, and Status, must carry the start
// error, and a later successful start must clear it again.
func TestStartErrorIsReportedAndClearedOnSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the editor stand-in is a shell script")
	}
	requireBin(t, "sh")
	requireBin(t, "sleep")
	bin := filepath.Join(t.TempDir(), "editor")
	var log lineLog
	sup := New(log.log)
	sup.AddService(ServiceSpec{Name: "ue", Command: bin, Restart: "never"})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })

	for _, call := range []struct {
		name string
		fn   func(string) (ServiceStatus, error)
	}{
		{"StartService", sup.StartService},
		{"RestartService", sup.RestartService},
	} {
		st, err := call.fn("ue")
		if err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
		if st.Running {
			t.Fatalf("%s on a missing program: running = true (%+v)", call.name, st)
		}
		if !strings.Contains(st.LastError, bin) {
			t.Fatalf("%s on a missing program: lastError = %q, want the start error naming %s", call.name, st.LastError, bin)
		}
	}
	if st := sup.Status(); len(st) != 1 || !strings.Contains(st[0].LastError, bin) {
		t.Fatalf("Status after a failed start = %+v, want the start error in lastError", st)
	}
	if strings.Contains(log.String(), "restarted (manual)") {
		t.Fatalf("a restart whose start failed is logged as done:\n%s", log.String())
	}

	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 20\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	st, err := sup.StartService("ue")
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	if !st.Running || st.LastError != "" {
		t.Fatalf("StartService once the program exists = %+v, want running with no lastError", st)
	}
	if st := sup.Status(); len(st) != 1 || st[0].LastError != "" {
		t.Fatalf("Status after a successful start = %+v, want the old start error cleared", st)
	}
}
