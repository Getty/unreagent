// Package supervisor verwaltet langlebige Prozesse (Unreal-Editor, Agent) mit
// Auto-Restart-Policy und führt benannte Einmal-Befehle (z.B. Compile) aus.
//
// Jede laufende Prozess-Instanz steckt in einem Windows-Job-Object, sodass beim
// Stoppen, Neustarten oder Absturz des Launchers keine Waisenprozesse
// zurückbleiben (siehe job_windows.go).
package supervisor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Logger ist eine einfache Zeilen-Senke (z.B. nach stdout).
type Logger func(line string)

// ServiceSpec beschreibt einen überwachten Prozess.
type ServiceSpec struct {
	Name         string
	Command      string
	Args         []string
	Dir          string
	Env          []string // zusätzliche Umgebungsvariablen ("KEY=VAL")
	Autostart    bool
	StartDelay   time.Duration
	Restart      string // never | on-failure | always
	MaxRestarts  int    // 0 = unbegrenzt
	RestartDelay time.Duration
	// PreStart wird vor jedem (Neu-)Start des Prozesses aufgerufen (z.B. um den
	// Crash-Reporter zu killen oder Recovery-Dateien aufzuräumen).
	PreStart func()
	// Foreground gibt dem Prozess die echte Konsole des Launchers (stdin/stdout/
	// stderr werden geerbt) — nötig für interaktive TUIs wie Claude Code, die ein
	// TTY brauchen. Dann werden stdout/stderr nicht ins Log gespiegelt, und der
	// Aufrufer sollte die eigene stdin-Nutzung (Command-Loop) unterlassen.
	Foreground bool
	// OnExit feuert, wenn der Prozess endet und NICHT automatisch neugestartet
	// wird (Policy oder MaxRestarts erschöpft), aber noch "gewünscht" war — also
	// ein unerwartetes Ende, kein manueller Stop. success = exit 0. Wird in einer
	// eigenen Goroutine aufgerufen, darf also blockieren / den Supervisor steuern.
	OnExit func(success bool)
}

// CommandSpec beschreibt einen Einmal-Befehl.
type CommandSpec struct {
	Description string
	Command     string
	Args        []string
	Dir         string
}

// ServiceStatus ist eine Momentaufnahme eines Service.
type ServiceStatus struct {
	Name     string `json:"name"`
	Running  bool   `json:"running"`
	PID      int    `json:"pid"`
	Restarts int    `json:"restarts"`
	Desired  bool   `json:"desired"`
}

// CommandResult ist das Ergebnis eines Einmal-Befehls.
type CommandResult struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
}

// Supervisor hält alle Services und Befehle.
type Supervisor struct {
	log      Logger
	mu       sync.Mutex
	services map[string]*service
	order    []string
	commands map[string]CommandSpec
}

// New erzeugt einen Supervisor mit der angegebenen Log-Senke.
func New(log Logger) *Supervisor {
	if log == nil {
		log = func(string) {}
	}
	return &Supervisor{
		log:      log,
		services: map[string]*service{},
		commands: map[string]CommandSpec{},
	}
}

func (s *Supervisor) logf(format string, a ...interface{}) {
	s.log(fmt.Sprintf(format, a...))
}

// AddService registriert einen überwachten Prozess (vor Start aufrufen).
func (s *Supervisor) AddService(spec ServiceSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.services[spec.Name] = &service{
		spec:   spec,
		ctrl:   make(chan ctrlMsg),
		logbuf: newRingBuffer(500),
	}
	s.order = append(s.order, spec.Name)
}

// AddCommand registriert einen Einmal-Befehl.
func (s *Supervisor) AddCommand(name string, spec CommandSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands[name] = spec
}

// Start startet die Überwachungs-Goroutinen aller Services.
func (s *Supervisor) Start(ctx context.Context, wg *sync.WaitGroup) {
	s.mu.Lock()
	svcs := make([]*service, 0, len(s.order))
	for _, name := range s.order {
		svcs = append(svcs, s.services[name])
	}
	s.mu.Unlock()
	for _, svc := range svcs {
		wg.Add(1)
		go func(svc *service) {
			defer wg.Done()
			s.runService(ctx, svc)
		}(svc)
	}
}

// --- Steuerung (vom MCP-Server / der Tastatur aufgerufen) ---

type ctrlKind int

const (
	ctrlStart ctrlKind = iota
	ctrlStop
	ctrlRestart
	ctrlStatus
)

type ctrlMsg struct {
	kind  ctrlKind
	reply chan ServiceStatus
}

func (s *Supervisor) send(name string, kind ctrlKind) (ServiceStatus, error) {
	s.mu.Lock()
	svc := s.services[name]
	s.mu.Unlock()
	if svc == nil {
		return ServiceStatus{}, fmt.Errorf("unbekannter Service %q", name)
	}
	reply := make(chan ServiceStatus, 1)
	select {
	case svc.ctrl <- ctrlMsg{kind: kind, reply: reply}:
	case <-time.After(10 * time.Second):
		return ServiceStatus{}, fmt.Errorf("Service %q reagiert nicht", name)
	}
	return <-reply, nil
}

// StartService startet einen (gestoppten) Service.
func (s *Supervisor) StartService(name string) (ServiceStatus, error) {
	return s.send(name, ctrlStart)
}

// StopService stoppt einen Service und verhindert Auto-Restart.
func (s *Supervisor) StopService(name string) (ServiceStatus, error) {
	return s.send(name, ctrlStop)
}

// RestartService startet einen Service neu.
func (s *Supervisor) RestartService(name string) (ServiceStatus, error) {
	return s.send(name, ctrlRestart)
}

// Status liefert Momentaufnahmen aller Services in Registrierungsreihenfolge.
func (s *Supervisor) Status() []ServiceStatus {
	s.mu.Lock()
	names := append([]string(nil), s.order...)
	s.mu.Unlock()
	out := make([]ServiceStatus, 0, len(names))
	for _, name := range names {
		if st, err := s.send(name, ctrlStatus); err == nil {
			out = append(out, st)
		}
	}
	return out
}

// Logs liefert die letzten n Ausgabezeilen eines Service.
func (s *Supervisor) Logs(name string, n int) ([]string, error) {
	s.mu.Lock()
	svc := s.services[name]
	s.mu.Unlock()
	if svc == nil {
		return nil, fmt.Errorf("unbekannter Service %q", name)
	}
	return svc.logbuf.tail(n), nil
}

// ServiceNames liefert alle registrierten Service-Namen.
func (s *Supervisor) ServiceNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.order...)
}

// CommandNames liefert alle registrierten Befehlsnamen.
func (s *Supervisor) CommandNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.commands))
	for name := range s.commands {
		out = append(out, name)
	}
	return out
}

// CommandDescription liefert die Beschreibung eines Befehls.
func (s *Supervisor) CommandDescription(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spec, ok := s.commands[name]
	return spec.Description, ok
}

// RunCommand führt einen benannten Einmal-Befehl synchron aus und gibt dessen
// gesammelte Ausgabe zurück. Auch dieser Prozess läuft in einem Job-Object.
func (s *Supervisor) RunCommand(name string) (CommandResult, error) {
	s.mu.Lock()
	spec, ok := s.commands[name]
	s.mu.Unlock()
	if !ok {
		return CommandResult{}, fmt.Errorf("unbekannter Befehl %q", name)
	}
	return s.runOnce(spec.Command, spec.Args, spec.Dir, nil, fmt.Sprintf("cmd:%s", name))
}

// RunOnce führt einen beliebigen Befehl synchron aus (genutzt von Runtimes).
func (s *Supervisor) RunOnce(command string, args []string, dir string, env []string, label string) (CommandResult, error) {
	return s.runOnce(command, args, dir, env, label)
}

func (s *Supervisor) runOnce(command string, args []string, dir string, env []string, label string) (CommandResult, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	job, _ := NewJob()
	s.logf("[%s] starte: %s %v", label, command, args)
	if err := cmd.Start(); err != nil {
		job.Close()
		return CommandResult{}, fmt.Errorf("Start fehlgeschlagen: %w", err)
	}
	if cmd.Process != nil {
		if err := job.Assign(cmd.Process.Pid); err != nil {
			s.logf("[%s] WARN Job-Assign: %v", label, err)
		}
	}
	err := cmd.Wait()
	job.Close()
	res := CommandResult{Output: buf.String(), ExitCode: exitCodeOf(err)}
	s.logf("[%s] fertig (exit %d)", label, res.ExitCode)
	return res, nil
}

// --- interne Service-Verwaltung ---

type service struct {
	spec   ServiceSpec
	ctrl   chan ctrlMsg
	logbuf *ringBuffer
}

// runService ist die Lebenszyklus-Schleife eines Service. Eine einzige
// Goroutine besitzt den gesamten Zustand; Steuerung läuft über svc.ctrl.
func (s *Supervisor) runService(ctx context.Context, svc *service) {
	spec := svc.spec

	var (
		cmd      *exec.Cmd
		job      *Job
		waitCh   chan error
		restarts int
		backoff  <-chan time.Time
		desired  = spec.Autostart
	)

	stop := func() {
		if cmd == nil {
			return
		}
		if job != nil {
			job.Close() // Windows: tötet den ganzen Baum
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill() // Fallback (Linux / falls Job No-Op)
		}
		if waitCh != nil {
			<-waitCh
		}
		cmd, job, waitCh = nil, nil, nil
	}

	start := func() {
		if spec.PreStart != nil {
			spec.PreStart()
		}
		c := exec.Command(spec.Command, spec.Args...)
		c.Dir = spec.Dir
		if len(spec.Env) > 0 {
			c.Env = append(os.Environ(), spec.Env...)
		}
		var stdout, stderr io.ReadCloser
		if spec.Foreground {
			// Echte Konsole erben -> echtes TTY für interaktive TUIs.
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		} else {
			var err error
			if stdout, err = c.StdoutPipe(); err != nil {
				s.logf("[%s] StdoutPipe: %v", spec.Name, err)
				return
			}
			if stderr, err = c.StderrPipe(); err != nil {
				s.logf("[%s] StderrPipe: %v", spec.Name, err)
				return
			}
		}
		if err := c.Start(); err != nil {
			s.logf("[%s] Start fehlgeschlagen: %v", spec.Name, err)
			if isNotFound(err) {
				// Programm existiert gar nicht — Neustarts wären sinnlos.
				s.logf("[%s] Programm nicht gefunden — kein weiterer Versuch (Pfad/Installation prüfen)", spec.Name)
				desired = false
				return
			}
			if desired && shouldRestart(spec, restarts, false) {
				restarts++
				backoff = time.After(spec.RestartDelay)
			}
			return
		}
		j, _ := NewJob()
		if c.Process != nil {
			if err := j.Assign(c.Process.Pid); err != nil {
				s.logf("[%s] WARN Job-Assign: %v", spec.Name, err)
			}
		}
		if !spec.Foreground {
			go s.stream(svc, stdout)
			go s.stream(svc, stderr)
		}
		wc := make(chan error, 1)
		go func() { wc <- c.Wait() }()
		cmd, job, waitCh = c, j, wc
		s.logf("[%s] gestartet (pid %d)", spec.Name, c.Process.Pid)
	}

	snapshot := func() ServiceStatus {
		st := ServiceStatus{Name: spec.Name, Desired: desired, Restarts: restarts}
		if cmd != nil && cmd.Process != nil {
			st.Running = true
			st.PID = cmd.Process.Pid
		}
		return st
	}

	// Initialer Start (mit optionaler Verzögerung).
	if desired {
		if spec.StartDelay > 0 {
			backoff = time.After(spec.StartDelay)
		} else {
			start()
		}
	}

	for {
		select {
		case <-ctx.Done():
			s.logf("[%s] stoppe …", spec.Name)
			stop()
			return

		case <-backoff:
			backoff = nil
			if desired && cmd == nil {
				start()
			}

		case err := <-waitChOrNil(waitCh):
			if job != nil {
				job.Close()
			}
			cmd, job, waitCh = nil, nil, nil
			success := err == nil
			s.logf("[%s] beendet (%s)", spec.Name, describeExit(err))
			if desired && shouldRestart(spec, restarts, success) {
				restarts++
				s.logf("[%s] Neustart %d%s in %s", spec.Name, restarts, maxStr(spec), spec.RestartDelay)
				backoff = time.After(spec.RestartDelay)
			} else if desired {
				s.logf("[%s] kein automatischer Neustart (policy=%s)", spec.Name, spec.Restart)
				if spec.OnExit != nil {
					// Eigene Goroutine: der Handler darf blockieren (TTY-Prompt) und
					// uns über svc.ctrl steuern (start/restart) — synchron wäre das
					// ein Deadlock, weil wir genau diese Schleife sind.
					go spec.OnExit(success)
				}
			}

		case msg := <-svc.ctrl:
			switch msg.kind {
			case ctrlStart:
				desired = true
				restarts = 0
				backoff = nil
				if cmd == nil {
					start()
				}
			case ctrlStop:
				desired = false
				backoff = nil
				stop()
				s.logf("[%s] gestoppt (manuell)", spec.Name)
			case ctrlRestart:
				desired = true
				restarts = 0
				backoff = nil
				stop()
				start()
				s.logf("[%s] neugestartet (manuell)", spec.Name)
			case ctrlStatus:
				// nur Snapshot
			}
			if msg.reply != nil {
				msg.reply <- snapshot()
			}
		}
	}
}

// waitChOrNil verhindert, dass ein nil-Channel im select sofort feuert.
func waitChOrNil(ch chan error) <-chan error {
	if ch == nil {
		return nil
	}
	return ch
}

func (s *Supervisor) stream(svc *service, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		svc.logbuf.add(line)
		s.log(fmt.Sprintf("[%s] %s", svc.spec.Name, line))
	}
}

func shouldRestart(spec ServiceSpec, restarts int, success bool) bool {
	switch spec.Restart {
	case "never":
		return false
	case "on-failure":
		if success {
			return false
		}
	case "always":
		// immer (bis MaxRestarts)
	default:
		return false
	}
	if spec.MaxRestarts > 0 && restarts >= spec.MaxRestarts {
		return false
	}
	return true
}

func maxStr(spec ServiceSpec) string {
	if spec.MaxRestarts > 0 {
		return fmt.Sprintf("/%d", spec.MaxRestarts)
	}
	return ""
}

func describeExit(err error) string {
	if err == nil {
		return "exit 0"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit %d", ee.ExitCode())
	}
	return err.Error()
}

// isNotFound erkennt, ob ein Start fehlschlug, weil das Programm nicht existiert
// (dann sind Neustarts sinnlos).
func isNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
