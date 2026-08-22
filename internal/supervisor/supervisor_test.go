package supervisor

import (
	"context"
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
		t.Skipf("%s nicht im PATH — Test übersprungen", name)
	}
}

// Ein Service, der von selbst endet und nicht neugestartet wird, muss OnExit
// feuern — das ist das Signal, das der Launcher braucht, um nicht stumm mit
// laufendem Editor hängenzubleiben.
func TestOnExitFiresOnNaturalExit(t *testing.T) {
	requireBin(t, "true")
	fired := make(chan bool, 1)
	sup := New(nil)
	sup.AddService(ServiceSpec{
		Name:      "x",
		Command:   "true", // beendet sich sofort mit exit 0
		Autostart: true,
		Restart:   "on-failure", // exit 0 -> kein Neustart
		OnExit:    func(success bool) { fired <- success },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)

	select {
	case success := <-fired:
		if !success {
			t.Fatalf("erwartet success=true (exit 0), bekam false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnExit wurde nicht gefeuert")
	}
	cancel()
	wg.Wait()
}

// Ein manuell gestoppter Service darf OnExit NICHT feuern — der Stop war
// gewollt, da soll der Launcher nicht herunterfahren.
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
		OnExit:    func(success bool) { fired <- success },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)

	time.Sleep(300 * time.Millisecond) // anlaufen lassen
	if _, err := sup.StopService("x"); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	select {
	case <-fired:
		t.Fatal("OnExit darf bei manuellem Stop nicht feuern")
	case <-time.After(700 * time.Millisecond):
		// gut — nichts gefeuert
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
		OnExit:    func(success bool) { fired <- success },
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
		OnExit:      func(success bool) { fired <- success },
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
