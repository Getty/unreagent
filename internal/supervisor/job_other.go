//go:build !windows

// On non-Windows platforms (Linux development/tests) there are no job objects.
// The OS-enforced tree kill is a no-op there; the supervisor only kills the
// direct child process. That is acceptable, since the actual target is Windows.
package supervisor

import "fmt"

// Job is a no-op placeholder on non-Windows.
type Job struct{}

// NewJob returns a no-op job.
func NewJob() (*Job, error) { return &Job{}, nil }

// Assign does nothing — except refuse a nil job, mirroring the Windows
// implementation so callers cannot silently skip the assignment.
func (j *Job) Assign(pid int) error {
	if j == nil {
		return fmt.Errorf("no job object for pid %d", pid)
	}
	return nil
}

// Close does nothing.
func (j *Job) Close() error { return nil }
