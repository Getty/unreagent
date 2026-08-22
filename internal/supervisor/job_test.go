package supervisor

import "testing"

// NewJob can fail on Windows (CreateJobObjectW / SetInformationJobObject), and
// its callers used to discard the error and hand the nil job to Assign, which
// dereferenced it. A nil job must report the failure — running a process with
// no job object voids the KILL_ON_JOB_CLOSE guarantee, so it may never happen
// silently.
//
// Only the guard itself is exercised here: on Linux NewJob never fails, so the
// Windows failure path stays untested.
func TestNilJobRefusesAssign(t *testing.T) {
	var j *Job
	if err := j.Assign(4711); err == nil {
		t.Fatal("Assign on a nil job must return an error, not succeed or panic")
	}
	if err := j.Close(); err != nil {
		t.Fatalf("Close on a nil job must be a no-op, got: %v", err)
	}
}
