package supervisor

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// k251: an editor that fails the same way on every run, but only after the
// fast-fail window (82 s per run, exit 0xC0000005, on the k226 walk-through),
// was restarted without end. A spec's CrashLoopWindow gives the unlimited
// default up on fastFailLimit failed runs in a row that each ended within it,
// with the give-up line naming that window. The runs here outlast the
// fast-fail window, so the k70 rule alone would never give up on them
// (TestSlowFailKeepsRestarting).
func TestCrashLoopWindowGivesUpOnSlowFailures(t *testing.T) {
	var log lineLog
	sup := New(log.log)
	sup.fastFailWindow = 50 * time.Millisecond
	spec, runs := runsSpec(t, "s", 200*time.Millisecond)
	spec.CrashLoopWindow = 2 * time.Second
	exits := startService(t, sup, spec)

	e := waitExit(t, exits, 20*time.Second, "a service failing after the fast-fail window on every run was restarted without bound")
	if e != (Exit{}) {
		t.Fatalf("give-up reported as %+v, want a failure that is no failed start (the runs outlasted the fast-fail window)", e)
	}
	if got := runs(); got != fastFailLimit {
		t.Fatalf("service ran %d times, want %d before giving up", got, fastFailLimit)
	}
	if got := restartsOf(t, sup); got != fastFailLimit-1 {
		t.Fatalf("restarts = %d, want %d", got, fastFailLimit-1)
	}
	want := fmt.Sprintf("[svc] failed %d times in a row, each within %s of its start — giving up (no automatic restart)", fastFailLimit, spec.CrashLoopWindow)
	if !strings.Contains(log.String(), want) {
		t.Fatalf("log lacks %q:\n%s", want, log.String())
	}
	if strings.Contains(log.String(), "within "+sup.fastFailWindow.String()) {
		t.Fatalf("the give-up names the fast-fail window, which no run failed within:\n%s", log.String())
	}
}

// A run that outlasts CrashLoopWindow is the healthy run that resets the
// count: two short failures, a long one and three short ones give up on the
// sixth run, not the third. The long run fails too, so only its length can
// have reset the count.
func TestCrashLoopCountResetsAfterALongRun(t *testing.T) {
	sup := New(nil)
	sup.fastFailWindow = 50 * time.Millisecond
	spec, runs := runsSpec(t, "sslsss", 200*time.Millisecond)
	spec.Env = append(spec.Env, helperLongEnv+"=1500ms")
	spec.CrashLoopWindow = time.Second
	exits := startService(t, sup, spec)

	waitExit(t, exits, 30*time.Second, "the service was never given up on")
	if got := runs(); got != 6 {
		t.Fatalf("gave up after run %d, want 6 (short, short, long, then three short in a row)", got)
	}
}

// An exit 0 resets the count as well (under restart: always).
func TestCrashLoopCountResetsAfterACleanExit(t *testing.T) {
	sup := New(nil)
	sup.fastFailWindow = 50 * time.Millisecond
	spec, runs := runsSpec(t, "ssosss", 200*time.Millisecond)
	spec.Restart = "always"
	spec.CrashLoopWindow = 2 * time.Second
	exits := startService(t, sup, spec)

	waitExit(t, exits, 30*time.Second, "the service was never given up on")
	if got := runs(); got != 6 {
		t.Fatalf("gave up after run %d, want 6 (fail, fail, exit 0, then three fails in a row)", got)
	}
}

// A manual start or restart gives the full count again, as for the k70 rule.
func TestCrashLoopManualStartResetsTheCount(t *testing.T) {
	for _, how := range []string{"start", "restart"} {
		t.Run(how, func(t *testing.T) {
			sup := New(nil)
			sup.fastFailWindow = 50 * time.Millisecond
			spec, runs := runsSpec(t, "s", 200*time.Millisecond)
			spec.CrashLoopWindow = 2 * time.Second
			exits := startService(t, sup, spec)
			waitExit(t, exits, 20*time.Second, "the first give-up")

			var err error
			if how == "start" {
				_, err = sup.StartService("svc")
			} else {
				_, err = sup.RestartService("svc")
			}
			if err != nil {
				t.Fatalf("manual %s: %v", how, err)
			}
			waitExit(t, exits, 20*time.Second, "the second give-up")
			if got := runs(); got != 2*fastFailLimit {
				t.Fatalf("ran %d times in all, want %d: a manual %s must give %d fresh runs", got, 2*fastFailLimit, how, fastFailLimit)
			}
		})
	}
}

// A configured maxRestarts keeps its exact meaning: the window, like the k70
// rule, only bounds the unlimited default.
func TestCrashLoopWindowKeepsAnExplicitMaxRestarts(t *testing.T) {
	var log lineLog
	sup := New(log.log)
	sup.fastFailWindow = 50 * time.Millisecond
	spec, runs := runsSpec(t, "s", 100*time.Millisecond)
	spec.CrashLoopWindow = 2 * time.Second
	spec.MaxRestarts = 4
	exits := startService(t, sup, spec)

	waitExit(t, exits, 30*time.Second, "maxRestarts was not honoured")
	if got := runs(); got != 5 {
		t.Fatalf("service ran %d times, want 5 (start + maxRestarts 4)", got)
	}
	if strings.Contains(log.String(), "giving up") {
		t.Fatalf("the window cut a configured maxRestarts short:\n%s", log.String())
	}
}
