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
	// Unresponsive is set when the service's control loop did not answer in
	// time. The other fields are then unknown — not "stopped".
	Unresponsive bool `json:"unresponsive,omitempty"`
}

// CommandResult ist das Ergebnis eines Einmal-Befehls.
type CommandResult struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
}

// ctrlTimeout is how long a control message waits for the service's loop.
const ctrlTimeout = 10 * time.Second

// Supervisor hält alle Services und Befehle.
type Supervisor struct {
	log Logger
	// ctrlTimeout is a field so tests can shorten it; New sets the default.
	ctrlTimeout time.Duration
	mu          sync.Mutex
	services    map[string]*service
	order       []string
	commands    map[string]CommandSpec
}

// New erzeugt einen Supervisor mit der angegebenen Log-Senke.
func New(log Logger) *Supervisor {
	if log == nil {
		log = func(string) {}
	}
	return &Supervisor{
		log:         log,
		ctrlTimeout: ctrlTimeout,
		services:    map[string]*service{},
		commands:    map[string]CommandSpec{},
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
	case <-time.After(s.ctrlTimeout):
		return ServiceStatus{}, fmt.Errorf("service %q is not responding", name)
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
//
// A service whose control loop does not answer is reported as unresponsive
// rather than dropped: the caller has to be able to tell "no such service" from
// "the loop is blocked". The services are queried concurrently, so one blocked
// loop does not add its timeout to every other one.
func (s *Supervisor) Status() []ServiceStatus {
	s.mu.Lock()
	names := append([]string(nil), s.order...)
	s.mu.Unlock()
	out := make([]ServiceStatus, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			st, err := s.send(name, ctrlStatus)
			if err != nil {
				s.logf("[%s] status: %v", name, err)
				st = ServiceStatus{Name: name, Unresponsive: true}
			}
			out[i] = st
		}(i, name)
	}
	wg.Wait()
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

	job, err := NewJob()
	if err != nil {
		s.logf("[%s] WARN job object could not be created: %v — the process runs unsupervised (a launcher crash leaves it behind)", label, err)
	}
	s.logf("[%s] starte: %s %v", label, command, args)
	if err := cmd.Start(); err != nil {
		job.Close()
		return CommandResult{}, fmt.Errorf("Start fehlgeschlagen: %w", err)
	}
	if cmd.Process != nil {
		if err := job.Assign(cmd.Process.Pid); err != nil {
			s.logf("[%s] WARN job assign failed: %v — the process runs unsupervised", label, err)
		}
	}
	err = cmd.Wait()
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
		streams  *streamSet
		restarts int
		backoff  <-chan time.Time
		desired  = spec.Autostart
	)

	// releaseStreams flushes what the finished process left in its pipes into
	// the log buffer before the exit is reported, then drops the read ends.
	releaseStreams := func() {
		streams.wait(streamDrainTimeout)
		streams.close()
		streams = nil
	}

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
		releaseStreams()
		cmd, job, waitCh = nil, nil, nil
	}

	// start is only ever called while the service is desired (initial start,
	// backoff, ctrlStart, ctrlRestart).
	start := func() {
		if spec.PreStart != nil {
			spec.PreStart()
		}
		// startFailed reports a start that will not be retried. The service was
		// desired and is not coming back, which is exactly what OnExit is for —
		// without it a missing agent.command leaves the user with a dead console
		// while the editor keeps running in the background.
		startFailed := func() {
			if spec.OnExit != nil {
				// Own goroutine, same reason as in the exit branch below: the
				// handler may block and steer us via svc.ctrl.
				go spec.OnExit(false)
			}
		}
		c := exec.Command(spec.Command, spec.Args...)
		c.Dir = spec.Dir
		if len(spec.Env) > 0 {
			c.Env = append(os.Environ(), spec.Env...)
		}
		var ss *streamSet
		if spec.Foreground {
			// Echte Konsole erben -> echtes TTY für interaktive TUIs.
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		} else {
			// Own pipes instead of c.StdoutPipe(): Wait() closes the pipes it
			// created itself as soon as the process is gone, throwing away
			// whatever is still buffered — which is exactly the crash output.
			var err error
			if ss, err = newStreamSet(c); err != nil {
				s.logf("[%s] output pipe: %v", spec.Name, err)
				startFailed()
				return
			}
		}
		err := c.Start()
		// The child holds its own copies of the write ends; ours have to go or
		// the readers never see EOF.
		ss.closeWriteEnds()
		if err != nil {
			ss.close()
			s.logf("[%s] start failed: %v", spec.Name, err)
			if isNotFound(err) {
				// The program does not exist at all — retries would be pointless.
				s.logf("[%s] program not found — no further attempt (check path / installation)", spec.Name)
				startFailed()
				desired = false
				return
			}
			if desired && shouldRestart(spec, restarts, false) {
				restarts++
				backoff = time.After(spec.RestartDelay)
				return
			}
			startFailed()
			return
		}
		j, jerr := NewJob()
		if jerr != nil {
			s.logf("[%s] WARN job object could not be created: %v — the process runs unsupervised (a launcher crash leaves it behind)", spec.Name, jerr)
		}
		if c.Process != nil {
			if err := j.Assign(c.Process.Pid); err != nil {
				s.logf("[%s] WARN job assign failed: %v — the process runs unsupervised", spec.Name, err)
			}
		}
		ss.startDrain(func(r io.Reader) { s.stream(svc, r) })
		wc := make(chan error, 1)
		go func() { wc <- c.Wait() }()
		cmd, job, waitCh, streams = c, j, wc, ss
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
			// Flush the trailing output first: the lines a crashing process
			// wrote last are the ones the user needs.
			releaseStreams()
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

const (
	// streamReadBuffer is the read chunk size for a process's output.
	streamReadBuffer = 64 * 1024
	// maxLogLine caps a single log line. Longer output is split across several
	// entries — dropping it (what bufio.Scanner did on a too-long token) ended
	// the whole stream, and the child then blocked forever on the full pipe.
	maxLogLine = 1 << 20
	// streamDrainTimeout bounds how long a finished process's pipes are drained
	// before its exit is reported. Grandchildren inherit the write end
	// (ShaderCompileWorker!) and can hold the pipe open long after the direct
	// child is gone, so this may never be an unbounded wait.
	streamDrainTimeout = 2 * time.Second
)

// streamSet owns the pipes of one process instance: the read ends this package
// drains and — until Start returned — the parent's copies of the write ends.
// Unlike cmd.StdoutPipe(), these are not known to exec.Cmd, so Wait() cannot
// close them under the readers and discard buffered output.
type streamSet struct {
	read  []*os.File
	write []*os.File
	done  chan struct{}
}

// newStreamSet points stdout and stderr of c at fresh pipes.
func newStreamSet(c *exec.Cmd) (*streamSet, error) {
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		_ = outR.Close()
		_ = outW.Close()
		return nil, err
	}
	c.Stdout, c.Stderr = outW, errW
	return &streamSet{
		read:  []*os.File{outR, errR},
		write: []*os.File{outW, errW},
		done:  make(chan struct{}),
	}, nil
}

// startDrain runs drain on every read end; done is closed once all of them
// returned, i.e. once the pipes are at EOF.
func (ss *streamSet) startDrain(drain func(io.Reader)) {
	if ss == nil {
		return
	}
	var wg sync.WaitGroup
	for _, f := range ss.read {
		wg.Add(1)
		go func(f *os.File) {
			defer wg.Done()
			defer f.Close()
			drain(f)
		}(f)
	}
	go func() {
		wg.Wait()
		close(ss.done)
	}()
}

// closeWriteEnds drops the parent's copies of the write ends right after Start;
// without that the readers would never see EOF.
func (ss *streamSet) closeWriteEnds() {
	if ss == nil {
		return
	}
	for _, f := range ss.write {
		_ = f.Close()
	}
	ss.write = nil
}

// wait blocks until the drain goroutines are done, at most d.
func (ss *streamSet) wait(d time.Duration) {
	if ss == nil {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ss.done:
	case <-t.C:
	}
}

// close releases the read ends. A goroutine still stuck in Read is woken by it
// (and reports os.ErrClosed, which stream treats as a normal end).
func (ss *streamSet) close() {
	if ss == nil {
		return
	}
	for _, f := range ss.read {
		_ = f.Close()
	}
}

// stream copies one output pipe into the service's log buffer, line by line,
// until EOF. It never gives up on the pipe: an over-long line is split, and a
// read error is reported instead of being swallowed.
func (s *Supervisor) stream(svc *service, r io.Reader) {
	br := bufio.NewReaderSize(r, streamReadBuffer)
	var pending []byte
	for {
		chunk, err := br.ReadSlice('\n')
		pending = append(pending, chunk...)
		switch {
		case err == nil:
			s.emit(svc, trimEOL(pending))
			pending = pending[:0]
		case errors.Is(err, bufio.ErrBufferFull):
			// No line break yet — emit in pieces rather than buffering without
			// bound or dropping the rest of the stream.
			if len(pending) >= maxLogLine {
				s.emit(svc, pending)
				pending = pending[:0]
			}
		default:
			if len(pending) > 0 {
				s.emit(svc, trimEOL(pending))
			}
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				s.logf("[%s] output stream ended: %v", svc.spec.Name, err)
			}
			return
		}
	}
}

func (s *Supervisor) emit(svc *service, line []byte) {
	text := string(line)
	svc.logbuf.add(text)
	s.log(fmt.Sprintf("[%s] %s", svc.spec.Name, text))
}

// trimEOL strips the line terminator, CRLF included (Windows).
func trimEOL(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte("\n"))
	return bytes.TrimSuffix(b, []byte("\r"))
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
