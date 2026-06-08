//go:build !windows

// Auf Nicht-Windows-Plattformen (Linux-Entwicklung/-Tests) gibt es keine
// Job-Objects. Die OS-erzwungene Baum-Tötung ist damit ein No-Op; der
// Supervisor killt dort nur den direkten Kindprozess. Das ist akzeptabel, da
// das eigentliche Ziel Windows ist.
package supervisor

// Job ist auf Nicht-Windows ein No-Op-Platzhalter.
type Job struct{}

// NewJob liefert einen No-Op-Job.
func NewJob() (*Job, error) { return &Job{}, nil }

// Assign tut nichts.
func (j *Job) Assign(pid int) error { return nil }

// Close tut nichts.
func (j *Job) Close() error { return nil }
