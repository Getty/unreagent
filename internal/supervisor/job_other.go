//go:build !windows

// Auf Nicht-Windows-Plattformen (Linux-Entwicklung/-Tests) gibt es keine
// Job-Objects. Die OS-erzwungene Baum-Tötung ist damit ein No-Op; der
// Supervisor killt dort nur den direkten Kindprozess. Das ist akzeptabel, da
// das eigentliche Ziel Windows ist.
package supervisor

import "fmt"

// Job ist auf Nicht-Windows ein No-Op-Platzhalter.
type Job struct{}

// NewJob liefert einen No-Op-Job.
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
