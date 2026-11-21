package supervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func requireBin(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH — test skipped", name)
	}
}

// A service that exits on its own and is not restarted must fire OnExit — that
// is the signal the launcher needs so it does not silently hang around with a
// running editor.
func TestOnExitFiresOnNaturalExit(t *testing.T) {
	requireBin(t, "true")
	fired := make(chan bool, 1)
	sup := New(nil)
	sup.AddService(ServiceSpec{
		Name:      "x",
		Command:   "true", // exits immediately with exit 0
		Autostart: true,
		Restart:   "on-failure", // exit 0 -> no restart
		OnExit:    func(e Exit) { fired <- e.Success },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)

	select {
	case success := <-fired:
		if !success {
			t.Fatalf("expected success=true (exit 0), got false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnExit did not fire")
	}
	cancel()
	wg.Wait()
}

// A manually stopped service must NOT fire OnExit — the stop was intended, so
// the launcher must not shut down.
func TestOnExitNotFiredOnManualStop(t *testing.T) {
	requireBin(t, "sleep")
	fired := make(chan bool, 1)
	sup := New(nil)
	sup.AddService(ServiceSpec{
		Name:      "x",
		Command:   "sleep",
		Args:      []string{"60"},
		Autostart: true,
		Restart:   "never",
		OnExit:    func(e Exit) { fired <- e.Success },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)

	time.Sleep(300 * time.Millisecond) // let it come up
	if _, err := sup.StopService("x"); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	select {
	case <-fired:
		t.Fatal("OnExit must not fire on a manual stop")
	case <-time.After(700 * time.Millisecond):
		// good — nothing fired
	}
	cancel()
	wg.Wait()
}

// A start that fails terminally must fire OnExit as well: the service was
// desired and is not coming back. Without it, an agent.command that is not on
// PATH leaves the user with a dead console while the editor keeps running.
func TestOnExitFiresWhenStartFails(t *testing.T) {
	fired := make(chan bool, 2)
	sup := New(nil)
	sup.AddService(ServiceSpec{
		Name:      "x",
		Command:   filepath.Join(t.TempDir(), "does-not-exist"),
		Autostart: true,
		Restart:   "never",
		OnExit:    func(e Exit) { fired <- e.Success },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)

	select {
	case success := <-fired:
		if success {
			t.Fatal("a failed start must report success=false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnExit did not fire for a start that failed")
	}
	cancel()
	wg.Wait()
}

// A program that exists but cannot be executed goes through the restart policy
// first: OnExit must fire once the retries are used up, and only once.
func TestOnExitFiresAfterStartRetriesExhausted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bits work differently on Windows")
	}
	bin := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fired := make(chan bool, 4)
	sup := New(nil)
	sup.AddService(ServiceSpec{
		Name:        "x",
		Command:     bin,
		Autostart:   true,
		Restart:     "on-failure",
		MaxRestarts: 1,
		OnExit:      func(e Exit) { fired <- e.Success },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)

	select {
	case success := <-fired:
		if success {
			t.Fatal("a failed start must report success=false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnExit did not fire after the start retries were exhausted")
	}
	select {
	case <-fired:
		t.Fatal("OnExit fired more than once for one exhausted start")
	case <-time.After(300 * time.Millisecond):
	}
	cancel()
	wg.Wait()
}

// preStarts records the automatic flag of every PreStart call.
type preStarts struct {
	mu    sync.Mutex
	calls []bool
}

func (p *preStarts) record(automatic bool) {
	p.mu.Lock()
	p.calls = append(p.calls, automatic)
	p.mu.Unlock()
}

func (p *preStarts) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return fmt.Sprint(p.calls)
}

// k77: PreStart could not tell the automatic restart after a crash from a
// start somebody asked for, so cleanOnRestart wiped the crash dump the agent
// needs on exactly the restart that follows the crash. Only a start of the
// restart policy — after an exit, or retrying a failed start — is automatic;
// the first start (delayed or not), StartService and RestartService are not.
func TestPreStartSaysWhetherTheStartIsAutomatic(t *testing.T) {
	t.Run("policy restart after an exit, then StartService and RestartService", func(t *testing.T) {
		var pre preStarts
		sup := New(nil)
		// Run 1 fails and is restarted by the policy; every later run exits 0,
		// which on-failure does not restart.
		spec, _ := runsSpec(t, "fo", 0)
		spec.PreStart = pre.record
		exits := startService(t, sup, spec)
		waitExit(t, exits, 10*time.Second, "the restarted run did not end")
		if _, err := sup.StartService("svc"); err != nil {
			t.Fatalf("StartService: %v", err)
		}
		waitExit(t, exits, 10*time.Second, "the run StartService began did not end")
		if _, err := sup.RestartService("svc"); err != nil {
			t.Fatalf("RestartService: %v", err)
		}
		waitExit(t, exits, 10*time.Second, "the run RestartService began did not end")
		if got, want := pre.String(), "[false true false false]"; got != want {
			t.Fatalf("PreStart(automatic) calls = %s, want %s (first start, policy restart, StartService, RestartService)", got, want)
		}
	})
	t.Run("delayed first start", func(t *testing.T) {
		var pre preStarts
		spec, _ := runsSpec(t, "o", 0)
		spec.StartDelay = 10 * time.Millisecond
		spec.PreStart = pre.record
		waitExit(t, startService(t, New(nil), spec), 10*time.Second, "the delayed first run did not end")
		if got, want := pre.String(), "[false]"; got != want {
			t.Fatalf("PreStart(automatic) calls = %s, want %s: a start delay is not a restart", got, want)
		}
	})
	t.Run("policy retry of a failed start", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("executable bits work differently on Windows")
		}
		bin := filepath.Join(t.TempDir(), "not-executable")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		var pre preStarts
		spec := ServiceSpec{
			Name:         "svc",
			Command:      bin,
			Autostart:    true,
			Restart:      "on-failure",
			MaxRestarts:  1,
			RestartDelay: 10 * time.Millisecond,
			PreStart:     pre.record,
		}
		waitExit(t, startService(t, New(nil), spec), 10*time.Second, "the start retries were not given up")
		if got, want := pre.String(), "[false true]"; got != want {
			t.Fatalf("PreStart(automatic) calls = %s, want %s: the policy's retry of a failed start is automatic", got, want)
		}
	})
}
