package supervisor

import (
	"context"
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
			PreStart: func() { <-release },
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
