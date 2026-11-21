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

// preStarts records every PreStart call.
type preStarts struct {
	mu    sync.Mutex
	calls []Start
}

func (p *preStarts) record(st Start) {
	p.mu.Lock()
	p.calls = append(p.calls, st)
	p.mu.Unlock()
}

// String lists the Automatic flag of every call.
func (p *preStarts) String() string {
	return p.flags(func(st Start) bool { return st.Automatic })
}

// afterGiveUp lists the AfterGiveUp flag of every call.
func (p *preStarts) afterGiveUp() string {
	return p.flags(func(st Start) bool { return st.AfterGiveUp })
}

// replacesRestart lists the ReplacesRestart flag of every call.
func (p *preStarts) replacesRestart() string {
	return p.flags(func(st Start) bool { return st.ReplacesRestart })
}

func (p *preStarts) flags(flag func(Start) bool) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	got := make([]bool, len(p.calls))
	for i, st := range p.calls {
		got[i] = flag(st)
	}
	return fmt.Sprint(got)
}

// k128: after the fast-fail give-up the launcher asks for ue_start, and with
// cleanOnRestart that start emptied Saved/Crashes, because PreStart could not
// tell it from any other start somebody asks for. The first start after a
// failure the supervisor did not restart says so; later starts do not.
func TestPreStartSaysWhetherTheStartFollowsAGiveUp(t *testing.T) {
	t.Run("fast-fail give-up, then StartService twice", func(t *testing.T) {
		var pre preStarts
		sup := New(nil)
		// Runs 1-3 fail at once (the give-up); every later run exits 0.
		spec, _ := runsSpec(t, "fffo", 0)
		spec.PreStart = pre.record
		exits := startService(t, sup, spec)
		if e := waitExit(t, exits, 10*time.Second, "the failing runs were not given up"); e.Success {
			t.Fatal("the give-up must report success=false")
		}
		for i := 0; i < 2; i++ {
			if _, err := sup.StartService("svc"); err != nil {
				t.Fatalf("StartService: %v", err)
			}
			waitExit(t, exits, 10*time.Second, "the run StartService began did not end")
		}
		if got, want := pre.afterGiveUp(), "[false false false true false]"; got != want {
			t.Fatalf("PreStart AfterGiveUp = %s, want %s (first start, two policy restarts, StartService after the give-up, StartService after a clean exit)", got, want)
		}
		if got, want := pre.String(), "[false true true false false]"; got != want {
			t.Fatalf("PreStart Automatic = %s, want %s", got, want)
		}
	})
	t.Run("the start after the give-up's start", func(t *testing.T) {
		var pre preStarts
		sup := New(nil)
		// Runs 1-3 fail at once (the give-up); run 4 keeps running until
		// RestartService replaces it, so no exit of its own resets the flag.
		spec, _ := runsSpec(t, "fffs", time.Minute)
		spec.PreStart = pre.record
		exits := startService(t, sup, spec)
		waitExit(t, exits, 10*time.Second, "the failing runs were not given up")
		if _, err := sup.StartService("svc"); err != nil {
			t.Fatalf("StartService: %v", err)
		}
		if _, err := sup.RestartService("svc"); err != nil {
			t.Fatalf("RestartService: %v", err)
		}
		if got, want := pre.afterGiveUp(), "[false false false true false]"; got != want {
			t.Fatalf("PreStart AfterGiveUp = %s, want %s: only the first start after the give-up follows it", got, want)
		}
	})
	t.Run("restart never, then RestartService", func(t *testing.T) {
		var pre preStarts
		sup := New(nil)
		spec, _ := runsSpec(t, "fo", 0)
		spec.Restart = "never"
		spec.PreStart = pre.record
		exits := startService(t, sup, spec)
		waitExit(t, exits, 10*time.Second, "the failed run was not reported")
		if _, err := sup.RestartService("svc"); err != nil {
			t.Fatalf("RestartService: %v", err)
		}
		waitExit(t, exits, 10*time.Second, "the run RestartService began did not end")
		if got, want := pre.afterGiveUp(), "[false true]"; got != want {
			t.Fatalf("PreStart AfterGiveUp = %s, want %s: a failure the policy does not restart is given up on too", got, want)
		}
	})
	t.Run("a start that cannot be retried", func(t *testing.T) {
		var pre preStarts
		sup := New(nil)
		exits := startService(t, sup, ServiceSpec{
			Name:      "svc",
			Command:   filepath.Join(t.TempDir(), "does-not-exist"),
			Autostart: true,
			Restart:   "on-failure",
			PreStart:  pre.record,
		})
		waitExit(t, exits, 10*time.Second, "the failed start was not reported")
		for i := 0; i < 2; i++ {
			if _, err := sup.StartService("svc"); err != nil {
				t.Fatalf("StartService: %v", err)
			}
			waitExit(t, exits, 10*time.Second, "the failed StartService was not reported")
		}
		if got, want := pre.afterGiveUp(), "[false true true]"; got != want {
			t.Fatalf("PreStart AfterGiveUp = %s, want %s: each start after a start that was not retried follows a give-up", got, want)
		}
	})
}

// waitRestartPending waits until the policy's restart of svc is pending: the
// run has ended and Restarts has counted it. Both change in the loop turn that
// arms the restart delay, so a control call after this lands within it.
func waitRestartPending(t *testing.T, sup *Supervisor, restarts int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := sup.Status()
		if len(st) == 1 && !st[0].Running && st[0].Restarts == restarts {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart %d of svc did not become pending: %+v", restarts, st)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// k153: a StartService or RestartService during the restart delay takes the
// place of the restart policy's start, but PreStart saw a start like any other
// one somebody asks for, and cleanOnRestart emptied the crash dumps the
// automatic restart would have kept. Such a start says it replaces the
// pending restart; a start with none pending, and one during the delay of the
// first start, do not.
func TestPreStartSaysWhetherTheStartReplacesAPendingRestart(t *testing.T) {
	t.Run("StartService during the restart delay, then after a clean exit", func(t *testing.T) {
		var pre preStarts
		sup := New(nil)
		// Run 1 fails; the policy's restart waits out a delay longer than the
		// test. Every later run exits 0, which on-failure does not restart.
		spec, _ := runsSpec(t, "fo", 0)
		spec.RestartDelay = time.Hour
		spec.PreStart = pre.record
		exits := startService(t, sup, spec)
		waitRestartPending(t, sup, 1)
		for i := 0; i < 2; i++ {
			if _, err := sup.StartService("svc"); err != nil {
				t.Fatalf("StartService: %v", err)
			}
			waitExit(t, exits, 10*time.Second, "the run StartService began did not end")
		}
		if got, want := pre.replacesRestart(), "[false true false]"; got != want {
			t.Fatalf("PreStart ReplacesRestart = %s, want %s (first start, StartService during the restart delay, StartService after a clean exit)", got, want)
		}
		if got, want := pre.String(), "[false false false]"; got != want {
			t.Fatalf("PreStart Automatic = %s, want %s: a start somebody asks for is never automatic", got, want)
		}
	})
	t.Run("RestartService during the restart delay", func(t *testing.T) {
		var pre preStarts
		sup := New(nil)
		spec, _ := runsSpec(t, "fo", 0)
		spec.RestartDelay = time.Hour
		spec.PreStart = pre.record
		exits := startService(t, sup, spec)
		waitRestartPending(t, sup, 1)
		if _, err := sup.RestartService("svc"); err != nil {
			t.Fatalf("RestartService: %v", err)
		}
		waitExit(t, exits, 10*time.Second, "the run RestartService began did not end")
		if got, want := pre.replacesRestart(), "[false true]"; got != want {
			t.Fatalf("PreStart ReplacesRestart = %s, want %s", got, want)
		}
	})
	t.Run("StartService during the retry delay of a failed start", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("executable bits work differently on Windows")
		}
		bin := filepath.Join(t.TempDir(), "not-executable")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		var pre preStarts
		sup := New(nil)
		startService(t, sup, ServiceSpec{
			Name:         "svc",
			Command:      bin,
			Autostart:    true,
			Restart:      "on-failure",
			RestartDelay: time.Hour,
			PreStart:     pre.record,
		})
		waitRestartPending(t, sup, 1)
		// PreStart runs in the control loop before StartService gets its reply.
		if _, err := sup.StartService("svc"); err != nil {
			t.Fatalf("StartService: %v", err)
		}
		if got, want := pre.replacesRestart(), "[false true]"; got != want {
			t.Fatalf("PreStart ReplacesRestart = %s, want %s: the policy's retry of a failed start is a pending restart too", got, want)
		}
	})
	t.Run("StartService during the start delay", func(t *testing.T) {
		var pre preStarts
		sup := New(nil)
		spec, _ := runsSpec(t, "o", 0)
		spec.StartDelay = time.Hour
		spec.PreStart = pre.record
		exits := startService(t, sup, spec)
		if _, err := sup.StartService("svc"); err != nil {
			t.Fatalf("StartService: %v", err)
		}
		waitExit(t, exits, 10*time.Second, "the run StartService began did not end")
		if got, want := pre.replacesRestart(), "[false]"; got != want {
			t.Fatalf("PreStart ReplacesRestart = %s, want %s: a start delay is not a restart", got, want)
		}
	})
	t.Run("StartService after a StopService during the restart delay", func(t *testing.T) {
		var pre preStarts
		sup := New(nil)
		spec, _ := runsSpec(t, "fo", 0)
		spec.RestartDelay = time.Hour
		spec.PreStart = pre.record
		exits := startService(t, sup, spec)
		waitRestartPending(t, sup, 1)
		// The stop cancels the pending restart and ends the crash episode:
		// the start after it replaces nothing, so cleanOnRestart empties the
		// crash dumps like any manual start.
		if _, err := sup.StopService("svc"); err != nil {
			t.Fatalf("StopService: %v", err)
		}
		if _, err := sup.StartService("svc"); err != nil {
			t.Fatalf("StartService: %v", err)
		}
		waitExit(t, exits, 10*time.Second, "the run StartService began after the stop did not end")
		if got, want := pre.replacesRestart(), "[false false]"; got != want {
			t.Fatalf("PreStart ReplacesRestart = %s, want %s: a stop during the restart delay cancels the pending restart", got, want)
		}
	})
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
