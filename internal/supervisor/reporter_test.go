package supervisor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// reporterProbe stands in for the look into a job for a crash reporter
// (Supervisor.reporterRunning): it says one is there until released, and
// counts how often it was asked. err fails every look, failLater every look
// once set. It also stands in for the close of a job
// (Supervisor.closeJob), counting the closes and then closing the job for
// real. Only the look and the close are replaced; the job is the real one of
// the platform.
type reporterProbe struct {
	released  atomic.Bool
	failLater atomic.Bool
	calls     atomic.Int32
	closes    atomic.Int32
	err       error
}

func (p *reporterProbe) running(*Job) (bool, error) {
	p.calls.Add(1)
	if p.err != nil {
		return false, p.err
	}
	if p.failLater.Load() {
		return false, errors.New("access denied")
	}
	return !p.released.Load(), nil
}

func (p *reporterProbe) close(j *Job) {
	p.closes.Add(1)
	_ = j.Close()
}

// reporterSup is a supervisor whose job probe and job close are p's and that
// looks every 10ms.
func reporterSup(log Logger, p *reporterProbe) *Supervisor {
	sup := New(log)
	sup.reporterPoll = 10 * time.Millisecond
	sup.reporterRunning = p.running
	sup.closeJob = p.close
	return sup
}

// closedOnce checks that exactly one job was closed, and that no second
// close follows.
func (p *reporterProbe) closedOnce(t *testing.T, when string) {
	t.Helper()
	time.Sleep(100 * time.Millisecond) // 10 looks: room for a second close
	if n := p.closes.Load(); n != 1 {
		t.Fatalf("%s: the job was closed %d times, want once", when, n)
	}
}

// startTimes records when each PreStart ran, and which start it was.
type startTimes struct {
	mu     sync.Mutex
	at     []time.Time
	starts []Start
}

func (s *startTimes) record(st Start) {
	s.mu.Lock()
	s.at = append(s.at, time.Now())
	s.starts = append(s.starts, st)
	s.mu.Unlock()
}

func (s *startTimes) n() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.at)
}

func (s *startTimes) get(i int) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.at[i]
}

func (s *startTimes) start(i int) Start {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starts[i]
}

// waitFor polls cond for up to d.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", d, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// k154: the editor exits right after its crash reporter has acknowledged the
// crash, and the reporter then still writes the symbolicated call stack. The
// job used to be closed at once, and KILL_ON_JOB_CLOSE cut the report short.
// After a failed run the job stays open while a reporter is in it, and the
// restart policy's start waits for that job: it does not come while the old
// run's job still holds the reporter, and it comes once the reporter is gone.
func TestRestartWaitsForTheCrashReporterOfTheLastRun(t *testing.T) {
	p := &reporterProbe{}
	sup := reporterSup(nil, p)
	spec, _ := runsSpec(t, "f", 0) // fails at once, restarted after 10ms
	spec.MaxRestarts = 1
	spec.ReporterGrace = time.Minute
	var starts startTimes
	spec.PreStart = starts.record
	startService(t, sup, spec)

	waitFor(t, 10*time.Second, "the job of the failed run to be probed", func() bool { return p.calls.Load() > 0 })
	time.Sleep(300 * time.Millisecond) // 30 restart delays
	if n := starts.n(); n != 1 {
		t.Fatalf("%d starts while the crash reporter of the failed run was still in its job, want 1", n)
	}
	released := time.Now()
	p.released.Store(true)
	waitFor(t, 10*time.Second, "the restart once the crash reporter ended", func() bool { return starts.n() == 2 })
	if at := starts.get(1); at.Before(released) {
		t.Fatalf("restart at %s, before the crash reporter ended at %s", at, released)
	}
}

// k154: the grace is a bound. A reporter that never ends (one waiting on its
// window, a stuck send) is ended with the job once the grace has passed, and
// the restart follows; it does not stall the service.
func TestCrashReporterGraceIsBounded(t *testing.T) {
	var log lineLog
	p := &reporterProbe{}
	sup := reporterSup(log.log, p)
	spec, _ := runsSpec(t, "f", 0)
	spec.MaxRestarts = 1
	spec.ReporterGrace = 300 * time.Millisecond
	var starts startTimes
	spec.PreStart = starts.record
	startService(t, sup, spec)

	waitFor(t, 10*time.Second, "the restart after the grace", func() bool { return starts.n() == 2 })
	if d := starts.get(1).Sub(starts.get(0)); d < spec.ReporterGrace {
		t.Fatalf("restart %s after the first start, before the grace of %s was over", d, spec.ReporterGrace)
	}
	if !strings.Contains(log.String(), "still running after 300ms") {
		t.Fatalf("no line about the reporter that outlived the grace; log:\n%s", log.String())
	}
}

// k154: only a failed run of a service with a grace keeps its job. A clean
// exit 0, and any exit of a service without ReporterGrace (the agent), close
// the job at once, as before: the probe is never asked.
func TestOnlyAFailedRunKeepsItsJobForTheCrashReporter(t *testing.T) {
	for _, c := range []struct {
		name    string
		pattern string
		grace   time.Duration
	}{
		{"exit 0 with a grace", "o", time.Minute},
		{"failure without a grace", "f", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &reporterProbe{}
			sup := reporterSup(nil, p)
			spec, _ := runsSpec(t, c.pattern, 0)
			spec.Restart = "never"
			spec.ReporterGrace = c.grace
			exits := startService(t, sup, spec)
			waitExit(t, exits, 10*time.Second, c.name)
			time.Sleep(100 * time.Millisecond)
			if n := p.calls.Load(); n != 0 {
				t.Fatalf("the job was probed %d times for a crash reporter, want 0", n)
			}
		})
	}
}

// k154: the crash reporter is told apart by its image name, in whichever
// case and directory form Windows reports it; nothing else matches.
func TestIsCrashReporterImage(t *testing.T) {
	for path, want := range map[string]bool{
		`C:\UE_5.8\Engine\Binaries\Win64\CrashReportClientEditor.exe`: true,
		`C:\UE_5.8\Engine\Binaries\Win64\CrashReportClient.exe`:       true,
		`c:\ue\engine\binaries\win64\crashreportclienteditor.EXE`:     true,
		`CrashReportClientEditor.exe`:                                 true,
		`C:\UE_5.8\Engine\Binaries\Win64\UnrealEditor.exe`:            false,
		`C:\UE_5.8\Engine\Binaries\Win64\zenserver.exe`:               false,
		`C:\Tools\CrashReportClientEditor.exe.bak`:                    false,
		`C:\CrashReportClient.exe\UnrealEditor.exe`:                   false,
		``: false,
	} {
		if got := isCrashReporterImage(path); got != want {
			t.Errorf("isCrashReporterImage(%q) = %v, want %v", path, got, want)
		}
	}
}

// k154: a launcher shutdown does not wait for the reporter: it closes the
// kept job at once (on Windows that ends the reporter, no zombie), once, and
// the service's loop returns promptly, after the job is closed.
func TestShutdownClosesAJobKeptForTheCrashReporter(t *testing.T) {
	p := &reporterProbe{}
	sup := reporterSup(nil, p)
	spec, _ := runsSpec(t, "f", 0)
	spec.Restart = "never"
	spec.ReporterGrace = time.Hour
	if err := sup.AddService(spec); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	waitFor(t, 10*time.Second, "the job of the failed run to be probed", func() bool { return p.calls.Load() > 0 })
	if n := p.closes.Load(); n != 0 {
		t.Fatalf("the job was closed %d times while the crash reporter ran, want it kept", n)
	}

	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown waited for the crash reporter")
	}
	if n := p.closes.Load(); n != 1 {
		t.Fatalf("shutdown returned with the kept job closed %d times, want once", n)
	}
	p.closedOnce(t, "after the shutdown")
}

// k154: the job kept for the crash reporter is the no-zombie guarantee of
// that run, so it is closed exactly once at every end of the grace: when the
// reporter has finished, when the grace has run out, and when a later look
// into the job fails (it cannot tell a finished report from a running one).
// Until then it stays open.
func TestKeptJobIsClosedOnceWhenTheGraceEnds(t *testing.T) {
	for _, c := range []struct {
		name  string
		grace time.Duration
		end   func(*reporterProbe)
		log   string
	}{
		{"the reporter finishes", time.Minute, func(p *reporterProbe) { p.released.Store(true) }, "crash reporter finished after"},
		{"the grace runs out", 300 * time.Millisecond, func(*reporterProbe) {}, "still running after 300ms"},
		{"a look into the job fails", time.Minute, func(p *reporterProbe) { p.failLater.Store(true) }, "WARN cannot look for a crash reporter"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var log lineLog
			p := &reporterProbe{}
			sup := reporterSup(log.log, p)
			spec, _ := runsSpec(t, "f", 0)
			spec.Restart = "never"
			spec.ReporterGrace = c.grace
			startService(t, sup, spec)

			waitFor(t, 10*time.Second, "the kept job to be looked into again", func() bool { return p.calls.Load() > 1 })
			if n := p.closes.Load(); n != 0 {
				t.Fatalf("the job was closed %d times while the crash reporter ran, want it kept", n)
			}
			c.end(p)
			waitFor(t, 10*time.Second, "the kept job to be closed", func() bool { return p.closes.Load() > 0 })
			p.closedOnce(t, c.name)
			if !strings.Contains(log.String(), c.log) {
				t.Fatalf("no line %q; log:\n%s", c.log, log.String())
			}
		})
	}
}

// k154: a failed run whose job holds no crash reporter any more, or whose
// job cannot be looked into, has its job closed at once, once, on the loop:
// before the exit is reported (OnExit), as before the grace existed. Nothing
// says it waits for a reporter, and only a failed look is logged.
func TestJobWithoutACrashReporterIsClosedAtOnce(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{
		{"no reporter left", nil},
		{"the look fails", errors.New("access denied")},
	} {
		t.Run(c.name, func(t *testing.T) {
			var log lineLog
			p := &reporterProbe{err: c.err}
			p.released.Store(true)
			sup := reporterSup(log.log, p)
			spec, _ := runsSpec(t, "f", 0)
			spec.Restart = "never"
			spec.ReporterGrace = time.Minute
			exits := startService(t, sup, spec)

			waitExit(t, exits, 10*time.Second, c.name)
			if n := p.closes.Load(); n != 1 {
				t.Fatalf("the job was closed %d times when the exit was reported, want once", n)
			}
			p.closedOnce(t, c.name)
			if n := p.calls.Load(); n != 1 {
				t.Fatalf("the job was looked into %d times, want once", n)
			}
			for _, claim := range []string{"start held until the last run's crash reporter has finished", "crash reporter still running"} {
				if strings.Contains(log.String(), claim) {
					t.Fatalf("log claims %q though no crash reporter was in the job:\n%s", claim, log.String())
				}
			}
			if failed := strings.Contains(log.String(), "WARN cannot look for a crash reporter"); failed != (c.err != nil) {
				t.Fatalf("WARN about the failed look logged = %v, want %v; log:\n%s", failed, c.err != nil, log.String())
			}
		})
	}
}

// heldSup runs a service that fails at once and keeps its job for its crash
// reporter, restarted once; its second run exits 0. It returns once the
// restart is held for that job (status says waiting).
func heldSup(t *testing.T) (*Supervisor, *reporterProbe, *startTimes) {
	t.Helper()
	p := &reporterProbe{}
	sup := reporterSup(nil, p)
	sup.ctrlTimeout = 500 * time.Millisecond
	spec, _ := runsSpec(t, "fo", 0)
	spec.MaxRestarts = 1
	spec.ReporterGrace = time.Minute
	starts := &startTimes{}
	spec.PreStart = starts.record
	startService(t, sup, spec)
	t.Cleanup(func() { p.released.Store(true) })
	waitFor(t, 10*time.Second, "the restart to be held for the crash reporter", func() bool {
		return statusJSON(t, sup)["waiting"] == "crash-reporter"
	})
	return sup, p, starts
}

// k154, review of the first round: the start that waited for the last run's
// job blocked the service's loop, so status and stop went unanswered for up
// to the grace. The loop now holds the start and keeps answering; status
// says what is true meanwhile: not running, wanted, a start held for the
// crash reporter (waiting), and no busy loop.
func TestStatusDuringTheWaitForTheCrashReporter(t *testing.T) {
	sup, p, starts := heldSup(t)
	st := statusJSON(t, sup)
	if st["unresponsive"] != nil || st["busy"] != nil {
		t.Fatalf("status while a start is held = %v, want an answering loop", st)
	}
	if st["running"] != false || st["desired"] != true || st["lastExit"] != "exit 1" {
		t.Fatalf("status while a start is held = %v, want running false, desired true, lastExit \"exit 1\"", st)
	}
	if n := starts.n(); n != 1 {
		t.Fatalf("%d starts while the start is held, want 1", n)
	}
	if n := p.closes.Load(); n != 0 {
		t.Fatalf("the kept job was closed %d times while the reporter ran, want it kept", n)
	}
}

// k154: a stop while a start is held for the crash reporter cancels that
// start, as a stop during the restart delay does: no run follows when the
// job is closed. It leaves the kept job alone, to be closed once when the
// reporter has finished (the report is what the grace is for).
func TestStopDuringTheWaitForTheCrashReporterCancelsTheStart(t *testing.T) {
	sup, p, starts := heldSup(t)
	st, err := sup.StopService("svc")
	if err != nil {
		t.Fatalf("StopService while a start is held: %v", err)
	}
	if st.Running || st.Desired || st.Waiting != "" {
		t.Fatalf("status after the stop = %+v, want not running, not desired, nothing held", st)
	}
	if n := p.closes.Load(); n != 0 {
		t.Fatalf("the stop closed the kept job %d times, want it left to the reporter", n)
	}
	p.released.Store(true)
	waitFor(t, 10*time.Second, "the kept job to be closed", func() bool { return p.closes.Load() > 0 })
	p.closedOnce(t, "after the reporter finished")
	time.Sleep(200 * time.Millisecond)
	if n := starts.n(); n != 1 {
		t.Fatalf("%d starts after a stop cancelled the held one, want 1", n)
	}
}

// k154: a start or restart while a start is held for the crash reporter is
// answered at once (the start is held, not failed) and runs once the job is
// closed, in place of the restart policy's start (ReplacesRestart), and only
// once.
func TestStartDuringTheWaitForTheCrashReporterIsHeld(t *testing.T) {
	for _, c := range []struct {
		name string
		call func(*Supervisor) (ServiceStatus, error)
	}{
		{"start", func(s *Supervisor) (ServiceStatus, error) { return s.StartService("svc") }},
		{"restart", func(s *Supervisor) (ServiceStatus, error) { return s.RestartService("svc") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			sup, p, starts := heldSup(t)
			st, err := c.call(sup)
			if err != nil {
				t.Fatalf("%s while a start is held: %v", c.name, err)
			}
			if st.Running || !st.Desired || st.Waiting != "crash-reporter" || st.LastError != "" {
				t.Fatalf("status after the %s = %+v, want a held start: not running, desired, waiting, no error", c.name, st)
			}
			time.Sleep(100 * time.Millisecond)
			if n := starts.n(); n != 1 {
				t.Fatalf("%d starts while the crash reporter is in the job, want 1", n)
			}
			released := time.Now()
			p.released.Store(true)
			waitFor(t, 10*time.Second, "the held start", func() bool { return starts.n() == 2 })
			if at := starts.get(1); at.Before(released) {
				t.Fatalf("start at %s, before the crash reporter ended at %s", at, released)
			}
			if got := starts.start(1); got != (Start{ReplacesRestart: true}) {
				t.Fatalf("held start = %+v, want ReplacesRestart only", got)
			}
			time.Sleep(200 * time.Millisecond)
			if n := starts.n(); n != 2 {
				t.Fatalf("%d starts, want 2: the held start ran more than once", n)
			}
		})
	}
}
