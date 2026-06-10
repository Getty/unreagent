package supervisor

import (
	"context"
	"os/exec"
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
