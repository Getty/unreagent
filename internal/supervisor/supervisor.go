// Package supervisor manages long-lived processes (Unreal Editor, agent) with
// an auto-restart policy and runs named one-off commands (e.g. compile).
//
// Every running process instance lives in a Windows job object, so stopping,
// restarting or crashing the launcher leaves no orphan processes behind
// (see job_windows.go).
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

// Logger is a simple line sink (e.g. to stdout).
type Logger func(line string)

// ServiceSpec describes a supervised process.
type ServiceSpec struct {
	Name         string
	Command      string
	Args         []string
	Dir          string
	Env          []string // additional environment variables ("KEY=VAL")
	Autostart    bool
	StartDelay   time.Duration
	Restart      string // never | on-failure | always
	MaxRestarts  int    // 0 = unlimited
	RestartDelay time.Duration
	// PreStart is called before every (re)start of the process (e.g. to kill the
	// crash reporter or to clean up recovery files).
	PreStart func()
	// Foreground gives the process the launcher's real console (stdin/stdout/
	// stderr are inherited) — required for interactive TUIs like Claude Code that
	// need a TTY. stdout/stderr are then not mirrored into the log, and the caller
	// should refrain from using stdin itself (command loop).
	Foreground bool
	// OnExit fires when the process ends and is NOT restarted automatically
	// (policy or MaxRestarts exhausted) but was still "desired" — i.e. an
	// unexpected end, not a manual stop. success = exit 0. It is called in its own
	// goroutine, so it may block / control the supervisor.
	OnExit func(success bool)
}

// CommandSpec describes a one-off command.
type CommandSpec struct {
	Description string
	Command     string
	Args        []string
	Dir         string
}

// ServiceStatus is a snapshot of a service.
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

// CommandResult is the result of a one-off command.
type CommandResult struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
}

// defaultCtrlTimeout is how long a control message waits for the service's
// loop before the service counts as unresponsive.
const defaultCtrlTimeout = 10 * time.Second

// Supervisor holds all services and commands.
type Supervisor struct {
	log Logger
	// ctrlTimeout is a field so tests can shorten it; New sets the default.
	ctrlTimeout time.Duration
	mu          sync.Mutex
	services    map[string]*service
	order       []string
	commands    map[string]CommandSpec
}

// New creates a supervisor with the given log sink.
func New(log Logger) *Supervisor {
	if log == nil {
		log = func(string) {}
	}
	return &Supervisor{
		log:         log,
		ctrlTimeout: defaultCtrlTimeout,
		services:    map[string]*service{},
		commands:    map[string]CommandSpec{},
	}
}

func (s *Supervisor) logf(format string, a ...interface{}) {
	s.log(fmt.Sprintf(format, a...))
}

// AddService registers a supervised process (call before Start).
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

// AddCommand registers a one-off command.
func (s *Supervisor) AddCommand(name string, spec CommandSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands[name] = spec
}

// Start starts the supervision goroutines of all services.
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

// --- control (called by the MCP server / the keyboard) ---

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
		return ServiceStatus{}, fmt.Errorf("unknown service %q", name)
	}
	reply := make(chan ServiceStatus, 1)
	select {
	case svc.ctrl <- ctrlMsg{kind: kind, reply: reply}:
	case <-time.After(s.ctrlTimeout):
		return ServiceStatus{}, fmt.Errorf("service %q is not responding", name)
	}
	return <-reply, nil
}

// StartService starts a (stopped) service.
func (s *Supervisor) StartService(name string) (ServiceStatus, error) {
	return s.send(name, ctrlStart)
}

// StopService stops a service and prevents auto-restart.
func (s *Supervisor) StopService(name string) (ServiceStatus, error) {
	return s.send(name, ctrlStop)
}

// RestartService restarts a service.
func (s *Supervisor) RestartService(name string) (ServiceStatus, error) {
	return s.send(name, ctrlRestart)
}

// Status returns snapshots of all services in registration order.
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

// Logs returns the last n output lines of a service.
func (s *Supervisor) Logs(name string, n int) ([]string, error) {
	s.mu.Lock()
	svc := s.services[name]
	s.mu.Unlock()
	if svc == nil {
		return nil, fmt.Errorf("unknown service %q", name)
	}
	return svc.logbuf.tail(n), nil
}

// ServiceNames returns all registered service names.
func (s *Supervisor) ServiceNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.order...)
}

// CommandNames returns all registered command names.
func (s *Supervisor) CommandNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.commands))
	for name := range s.commands {
		out = append(out, name)
	}
	return out
}

// CommandDescription returns the description of a command.
func (s *Supervisor) CommandDescription(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spec, ok := s.commands[name]
	return spec.Description, ok
}

// RunCommand runs a named one-off command synchronously and returns its
// collected output. This process runs in a job object as well.
func (s *Supervisor) RunCommand(name string) (CommandResult, error) {
	s.mu.Lock()
	spec, ok := s.commands[name]
	s.mu.Unlock()
	if !ok {
		return CommandResult{}, fmt.Errorf("unknown command %q", name)
	}
	return s.runOnce(spec.Command, spec.Args, spec.Dir, nil, fmt.Sprintf("cmd:%s", name))
}

// RunOnce runs an arbitrary command synchronously (used by runtimes).
func (s *Supervisor) RunOnce(command string, args []string, dir string, env []string, label string) (CommandResult, error) {
	return s.runOnce(command, args, dir, env, label)
}

func (s *Supervisor) runOnce(command string, args []string, dir string, env []string, label string) (CommandResult, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	// Bounded on purpose: a full UE package log is tens to hundreds of MB and
	// the callers only ever show the tail of it.
	buf := newHeadTailBuffer(commandOutputLimit)
	cmd.Stdout = buf
	cmd.Stderr = buf

	job, err := NewJob()
	if err != nil {
		s.logf("[%s] WARN job object could not be created: %v — the process runs unsupervised (a launcher crash leaves it behind)", label, err)
	}
	s.logf("[%s] starting: %s %v", label, command, args)
	if err := cmd.Start(); err != nil {
		job.Close()
		return CommandResult{}, fmt.Errorf("start failed: %w", err)
	}
	if cmd.Process != nil {
		if err := job.Assign(cmd.Process.Pid); err != nil {
			s.logf("[%s] WARN job assign failed: %v — the process runs unsupervised", label, err)
		}
	}
	err = cmd.Wait()
	job.Close()
	res := CommandResult{Output: buf.String(), ExitCode: exitCodeOf(err)}
	s.logf("[%s] done (exit %d)", label, res.ExitCode)
	return res, nil
}

// --- internal service management ---

type service struct {
	spec   ServiceSpec
	ctrl   chan ctrlMsg
	logbuf *ringBuffer
}

// runService is the lifecycle loop of a service. A single goroutine owns all of
// the state; control goes through svc.ctrl.
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
			job.Close() // Windows: kills the whole tree
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill() // fallback (Linux / if the job is a no-op)
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
			// Inherit the real console -> a real TTY for interactive TUIs.
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
		s.logf("[%s] started (pid %d)", spec.Name, c.Process.Pid)
	}

	snapshot := func() ServiceStatus {
		st := ServiceStatus{Name: spec.Name, Desired: desired, Restarts: restarts}
		if cmd != nil && cmd.Process != nil {
			st.Running = true
			st.PID = cmd.Process.Pid
		}
		return st
	}

	// Initial start (with an optional delay).
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
			s.logf("[%s] stopping …", spec.Name)
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
			s.logf("[%s] exited (%s)", spec.Name, describeExit(err))
			if desired && shouldRestart(spec, restarts, success) {
				restarts++
				s.logf("[%s] restart %d%s in %s", spec.Name, restarts, maxStr(spec), spec.RestartDelay)
				backoff = time.After(spec.RestartDelay)
			} else if desired {
				s.logf("[%s] no automatic restart (policy=%s)", spec.Name, spec.Restart)
				if spec.OnExit != nil {
					// Own goroutine: the handler may block (TTY prompt) and steer us
					// via svc.ctrl (start/restart) — doing it synchronously would
					// deadlock, because we are exactly that loop.
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
				s.logf("[%s] stopped (manual)", spec.Name)
			case ctrlRestart:
				desired = true
				restarts = 0
				backoff = nil
				stop()
				start()
				s.logf("[%s] restarted (manual)", spec.Name)
			case ctrlStatus:
				// snapshot only
			}
			if msg.reply != nil {
				msg.reply <- snapshot()
			}
		}
	}
}

// waitChOrNil prevents a nil channel from firing immediately in a select.
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
		// always (up to MaxRestarts)
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

// isNotFound detects whether a start failed because the program does not exist
// (restarts are pointless then).
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
