package supervisor

import (
	"context"
	"strings"
	"time"
)

// reporterPoll is how often a job kept open for its crash reporter is looked
// into (ServiceSpec.ReporterGrace).
const reporterPoll = 250 * time.Millisecond

// isCrashReporterImage reports whether path, a process image path as Windows
// gives it, is Unreal's crash reporter: CrashReportClientEditor.exe (the
// editor's) or CrashReportClient.exe, in any case.
func isCrashReporterImage(path string) bool {
	name := path[strings.LastIndexAny(path, `\/`)+1:]
	return strings.EqualFold(name, "CrashReportClientEditor.exe") || strings.EqualFold(name, "CrashReportClient.exe")
}

// reporterIn reports whether a crash reporter is still in job, the job of
// service name's run that just ended with a failure, and logs that the job
// is kept for it. It runs on the service's loop, before the job is handed to
// closeAfterReporter, so the loop only ever waits for a job it has seen a
// reporter in. A probe that fails counts as no reporter: the job is closed at
// once, as before the grace existed, since a failed look cannot tell a
// finished report from a running one.
func (s *Supervisor) reporterIn(name string, job *Job, grace time.Duration) bool {
	running, err := s.reporterRunning(job)
	if err != nil {
		s.logf("[%s] WARN cannot look for a crash reporter in the job: %v — job closed", name, err)
		return false
	}
	if running {
		s.logf("[%s] crash reporter still running — the job stays open up to %s so it can finish the crash report", name, grace)
	}
	return running
}

// closeAfterReporter closes job, the job of a run that ended with a failure
// and still holds a crash reporter (reporterIn), once no crash reporter is
// left in it, once grace has passed, or once ctx is done, whichever comes
// first; then it closes done. It runs in its own goroutine and owns job from
// then on: the service's loop has let go of it.
//
// The job is only ever looked into, never left open beyond grace or the
// shutdown, so KILL_ON_JOB_CLOSE still ends whatever the run left behind. A
// probe that fails closes the job at once (see reporterIn).
func (s *Supervisor) closeAfterReporter(ctx context.Context, name string, job *Job, grace time.Duration, done chan<- struct{}) {
	defer close(done)
	defer s.closeJob(job)
	began := time.Now()
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	tick := time.NewTicker(s.reporterPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logf("[%s] crash reporter ended with the job: the launcher is shutting down", name)
			return
		case <-deadline.C:
			s.logf("[%s] WARN crash reporter still running after %s — ended with the job; the crash report may lack the call stack", name, grace)
			return
		case <-tick.C:
			running, err := s.reporterRunning(job)
			if err != nil {
				s.logf("[%s] WARN cannot look for a crash reporter in the job: %v — job closed", name, err)
				return
			}
			if !running {
				s.logf("[%s] crash reporter finished after %s — job closed", name, time.Since(began).Round(100*time.Millisecond))
				return
			}
		}
	}
}
