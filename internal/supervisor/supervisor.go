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
	"sync/atomic"
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
	MaxRestarts  int    // 0 = unlimited — but one that keeps failing at its start is given up on (giveUp in runService)
	RestartDelay time.Duration
	// PreStart is called before every (re)start of the process (e.g. to kill the
	// crash reporter or to clean up recovery files); Start says which start it
	// is.
	PreStart func(Start)
	// Foreground gives the process the launcher's real console (stdin/stdout/
	// stderr are inherited) — required for interactive TUIs like Claude Code that
	// need a TTY. stdout/stderr are then not mirrored into the log, and the caller
	// should refrain from using stdin itself (command loop).
	Foreground bool
	// OnExit fires when the process ends and is NOT restarted automatically
	// (policy, MaxRestarts exhausted, a start that cannot be retried, or the
	// fast-fail give-up) but was still "desired" — i.e. an unexpected end, not
	// a manual stop. A StopService that is already waiting when the process
	// ends on its own wins over the exit: OnExit does not fire for it. Nor
	// does it for a StartService or RestartService waiting then, which is
	// carried out instead. It is called in its own goroutine, so it may
	// block / control the supervisor.
	OnExit func(Exit)
	// OnStartRetry fires for a start that failed (the program cannot be
	// executed, …) when the restart policy retries it after RestartDelay;
	// not for one the policy gives up on, which OnExit reports. It is called
	// on the service's loop, before the loop answers anything else: it must
	// return quickly and must not call the supervisor.
	OnStartRetry func(Retry)
}

// Retry tells ServiceSpec.OnStartRetry about a failed start that is retried.
type Retry struct {
	// Err is why the start failed, as ServiceStatus.LastError says it.
	Err string
	// Restart is the retry's number among the restarts (ServiceStatus.Restarts),
	// MaxRestarts and Delay the spec's.
	Restart     int
	MaxRestarts int
	Delay       time.Duration
}

// Start tells ServiceSpec.PreStart which start is coming.
type Start struct {
	// Automatic is true when the restart policy starts the service — after an
	// exit, or retrying a failed start — and false for the first start (with
	// or without StartDelay) and for StartService / RestartService.
	Automatic bool
	// AfterGiveUp is true for the first start after the service failed and was
	// not restarted — the fast-fail give-up, a used-up MaxRestarts, a policy
	// that does not restart the failure, a start that cannot be retried (what
	// OnExit reports with Success false). Only StartService or RestartService
	// start it then, so it is never Automatic.
	AfterGiveUp bool
	// ReplacesRestart is true for a StartService or RestartService that comes
	// while the restart policy's start is pending — within RestartDelay of an
	// exit or of a failed start, including one waiting when that delay runs
	// out — and so takes its place: that start does not come any more. It is
	// never Automatic.
	ReplacesRestart bool
}

// Exit tells ServiceSpec.OnExit how the service ended.
type Exit struct {
	// Success is true for exit 0.
	Success bool
	// StartFailed is true when the last run never got going: it could not be
	// started, or it failed within FastFailWindow of its start.
	// Design choice for k70: it describes the last run only, so restart:
	// never, a used-up maxRestarts and the fast-fail give-up report a quick
	// failure alike, however many runs came before (see k70).
	StartFailed bool
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
	// Busy says, next to Unresponsive, what kept the loop from answering:
	// "pre-start" while it runs the service's PreStart (the editor's
	// crash-reporter kill and crash cleanup). It is empty otherwise.
	Busy string `json:"busy,omitempty"`
	// LastError is why the last start attempt failed (the program is missing,
	// cannot be executed, …). A successful start clears it.
	LastError string `json:"lastError,omitempty"`
	// LastExit is how the last run that ended on its own ended ("exit 1"), not
	// one StopService or RestartService ended. A process that dies right after
	// a successful start shows here, not in LastError. StartService and
	// RestartService clear it; the restart policy's restarts keep it.
	LastExit string `json:"lastExit,omitempty"`
}

// CommandResult is the result of a one-off command.
type CommandResult struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
	// lines, keptLines and dropped are set when the output buffer dropped the
	// middle of the output (see headTailBuffer.result, LinesBefore and Gap).
	lines, keptLines, dropped int
}

// LogLines is how many output lines a service's log buffer keeps. Older lines
// are dropped, and Logs says how many.
const LogLines = 500

// CtrlTimeout is how long a control call waits for the service's loop to take
// its message and answer it, both together, before the service counts as
// unresponsive. It is exported so the launcher's tool descriptions can name it.
const CtrlTimeout = 10 * time.Second

// FastFailWindow and fastFailLimit define a service that keeps failing right
// after it starts: fastFailLimit failed runs in a row (non-zero exit or failed
// start), each ending within FastFailWindow of its start. FastFailWindow is
// exported so the launcher's messages can name it.
const (
	FastFailWindow = 10 * time.Second
	fastFailLimit  = 3
)

// Supervisor holds all services and commands.
type Supervisor struct {
	log Logger
	// ctrlTimeout, drainTimeout and fastFailWindow are fields so tests can
	// shorten them; New sets the defaults.
	ctrlTimeout    time.Duration
	drainTimeout   time.Duration
	fastFailWindow time.Duration
	mu             sync.Mutex
	services       map[string]*service
	order          []string
	commands       map[string]CommandSpec
	// exitTaken, when set, is called by a service's loop once it has taken a
	// run's exit, right before it looks for a control message that came in the
	// meantime. A test seam: it lets a test make a stop wait at that point.
	exitTaken func()
	// backoffTaken is the same seam for a due restart timer (or the delayed
	// first start): called once the loop has taken the timer, right before it
	// looks for a waiting control message.
	backoffTaken func()
}

// New creates a supervisor with the given log sink.
func New(log Logger) *Supervisor {
	if log == nil {
		log = func(string) {}
	}
	return &Supervisor{
		log:            log,
		ctrlTimeout:    CtrlTimeout,
		drainTimeout:   streamDrainTimeout,
		fastFailWindow: FastFailWindow,
		services:       map[string]*service{},
		commands:       map[string]CommandSpec{},
	}
}

func (s *Supervisor) logf(format string, a ...interface{}) {
	s.log(fmt.Sprintf(format, a...))
}

// AddService registers a supervised process (call before Start). A name that
// is already registered is refused: two loops would share one service.
func (s *Supervisor) AddService(spec ServiceSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.services[spec.Name]; ok {
		return fmt.Errorf("service %q is already registered", spec.Name)
	}
	s.services[spec.Name] = &service{
		spec:   spec,
		ctrl:   make(chan ctrlMsg),
		logbuf: newRingBuffer(LogLines),
	}
	s.order = append(s.order, spec.Name)
	return nil
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
	// One deadline for the whole call: the loop has to take the message and
	// answer it within ctrlTimeout. reply is buffered, so a loop that answers
	// after the caller gave up does not block on it.
	reply := make(chan ServiceStatus, 1)
	deadline := time.NewTimer(s.ctrlTimeout)
	defer deadline.Stop()
	select {
	case svc.ctrl <- ctrlMsg{kind: kind, reply: reply}:
	case <-deadline.C:
		return ServiceStatus{}, svc.timeoutError(false, s.ctrlTimeout)
	}
	select {
	case st := <-reply:
		return st, nil
	case <-deadline.C:
		return ServiceStatus{}, svc.timeoutError(true, s.ctrlTimeout)
	}
}

// timeoutError says why a control call ran out of time after d. taken tells a
// message the loop took and is still carrying out from one it never took, for
// which nothing was done. A taken one goes on, so it must not read as failed:
// a repeated call would wait behind it and then act on what it did, a second
// restart restarting the editor the first just spawned. A loop inside
// PreStart is named as busy with it, not as not responding: PreStart runs on
// the loop, and the editor's shells out.
func (svc *service) timeoutError(taken bool, d time.Duration) error {
	name, busy := svc.spec.Name, svc.preStarting.Load()
	e := &ctrlTimeoutError{preStart: busy}
	switch {
	case !taken && busy:
		e.msg = fmt.Sprintf("service %q is busy with its pre-start step and did not take the request within %s; nothing was done, try again shortly", name, d)
	case !taken:
		e.msg = fmt.Sprintf("service %q is not responding: it did not take the request within %s; nothing was done", name, d)
	case busy:
		e.msg = fmt.Sprintf("service %q took the request and is still carrying it out after %s (busy with its pre-start step); do not repeat it, check status for the outcome", name, d)
	default:
		e.msg = fmt.Sprintf("service %q took the request and is still carrying it out after %s; do not repeat it, check status for the outcome", name, d)
	}
	return e
}

// ctrlTimeoutError is the error of a control call that ran out of time
// (timeoutError). preStart is the one reading of preStarting its text was
// built from, so Status reports the state the error names, not a second
// reading that PreStart may have ended in between.
type ctrlTimeoutError struct {
	msg      string
	preStart bool
}

func (e *ctrlTimeoutError) Error() string { return e.msg }

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
// "the loop is blocked". One busy with PreStart says so (Busy). The services
// are queried concurrently, so one blocked loop does not add its timeout to
// every other one.
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
				var te *ctrlTimeoutError
				if errors.As(err, &te) && te.preStart {
					st.Busy = "pre-start"
				}
			}
			out[i] = st
		}(i, name)
	}
	wg.Wait()
	return out
}

// Logs returns the last n output lines of a service (all it holds for n <= 0)
// and how many older lines its buffer has dropped since the service was
// added, restarts included: lines no call returns any more.
func (s *Supervisor) Logs(name string, n int) ([]string, int, error) {
	s.mu.Lock()
	svc := s.services[name]
	s.mu.Unlock()
	if svc == nil {
		return nil, 0, fmt.Errorf("unknown service %q", name)
	}
	lines, dropped := svc.logbuf.tail(n)
	return lines, dropped, nil
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
	return s.runOnce(context.Background(), spec.Command, spec.Args, spec.Dir, nil, fmt.Sprintf("cmd:%s", name))
}

// RunOnce runs an arbitrary command synchronously (used by runtimes).
func (s *Supervisor) RunOnce(command string, args []string, dir string, env []string, label string) (CommandResult, error) {
	return s.runOnce(context.Background(), command, args, dir, env, label)
}

// RunOnceContext is RunOnce bounded by ctx. When ctx is done before the command
// ends, its job object is closed (on Windows that ends the whole tree) and the
// process is killed; the output collected up to then comes back together with
// an error wrapping ctx.Err(). A ctx that is already done starts nothing.
func (s *Supervisor) RunOnceContext(ctx context.Context, command string, args []string, dir string, env []string, label string) (CommandResult, error) {
	return s.runOnce(ctx, command, args, dir, env, label)
}

func (s *Supervisor) runOnce(ctx context.Context, command string, args []string, dir string, env []string, label string) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{}, fmt.Errorf("not started: %w", err)
	}
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	// A parent-owned pipe instead of an io.Writer: exec.Cmd then only sees an
	// *os.File and Wait() returns when the command exits — not when the last
	// grandchild that inherited the write end (MSBuild node reuse) lets go of
	// it, which kept the caller waiting for that grandchild's whole lifetime.
	ss, err := newCombinedStreamSet(cmd)
	if err != nil {
		return CommandResult{}, fmt.Errorf("output pipe: %w", err)
	}
	// Bounded on purpose: a full UE package log is tens to hundreds of MB and
	// the callers only ever show the tail of it.
	buf := newHeadTailBuffer(commandOutputLimit)

	job, err := NewJob()
	if err != nil {
		s.logf("[%s] WARN job object could not be created: %v — the process runs unsupervised (a launcher crash leaves it behind)", label, err)
	}
	s.logf("[%s] starting: %s %v", label, command, args)
	err = cmd.Start()
	// The child holds its own copy of the write end; ours has to go or the
	// reader never sees EOF.
	ss.closeWriteEnds()
	if err != nil {
		ss.close()
		job.Close()
		return CommandResult{}, fmt.Errorf("start failed: %w", err)
	}
	if cmd.Process != nil {
		if err := job.Assign(cmd.Process.Pid); err != nil {
			s.logf("[%s] WARN job assign failed: %v — the process runs unsupervised", label, err)
		}
	}
	ss.startDrain(func(r io.Reader) {
		if _, err := io.Copy(buf, r); err != nil && !errors.Is(err, os.ErrClosed) {
			s.logf("[%s] output stream ended: %v", label, err)
		}
	})
	waitc := make(chan error, 1)
	go func() { waitc <- cmd.Wait() }()
	var aborted error
	select {
	case err = <-waitc:
	case <-ctx.Done():
		aborted = ctx.Err()
		// The order of stopping a service: the job first (Windows: the whole
		// tree), the direct child as the fallback where there is no job.
		job.Close()
		_ = cmd.Process.Kill()
		err = <-waitc
	}
	// Close the job before draining, not after: on Windows KILL_ON_JOB_CLOSE
	// ends whatever the command left behind, and with it their copies of the
	// pipe, so the drain reaches EOF right away. The timeout covers the rest —
	// a process outside the job, and platforms without job objects.
	job.Close()
	ss.wait(s.drainTimeout)
	ss.close()
	// close() woke a reader still blocked on a write end held by a grandchild;
	// join it, so nothing writes to buf any more once it is read.
	<-ss.done
	res := buf.result(exitCodeOf(err))
	if aborted != nil {
		s.logf("[%s] aborted (%v)", label, aborted)
		return res, fmt.Errorf("aborted: %w", aborted)
	}
	s.logf("[%s] done (exit %d)", label, res.ExitCode)
	return res, nil
}

// --- internal service management ---

type service struct {
	spec   ServiceSpec
	ctrl   chan ctrlMsg
	logbuf *ringBuffer
	// preStarting is set while the loop runs spec.PreStart, so a control call
	// that times out meanwhile can say so (timeoutError).
	preStarting atomic.Bool
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
		// backoffAuto says which start the pending backoff is for: the
		// restart policy's (automatic) or the delayed first start (not).
		backoffAuto bool
		desired     = spec.Autostart
		// startedAt is when the running process started; quickFails counts
		// the failed runs in a row that ended within fastFailWindow of their
		// start.
		startedAt  time.Time
		quickFails int
		// lastErr is why the last start attempt failed; a successful start
		// clears it (ServiceStatus.LastError).
		lastErr string
		// lastExit is how the last run that ended on its own ended
		// (ServiceStatus.LastExit).
		lastExit string
		// afterGiveUp is set while the service failed and was not restarted, until
		// the next start (Start.AfterGiveUp).
		afterGiveUp bool
	)

	// countRun is told whether the run that just ended failed early: a failed
	// start, or a non-zero exit within fastFailWindow of its start. Only such
	// runs count; a clean exit or a run that outlasts the window resets the
	// count — under restart: always a job that finishes quickly with exit 0 is
	// what was asked for.
	countRun := func(failedEarly bool) {
		if failedEarly {
			quickFails++
		} else {
			quickFails = 0
		}
	}
	// giveUp is asked when the policy would restart the service. It reports
	// (and logs) a service that keeps failing right after its start, which is
	// restarted no more. The rule only bounds the unlimited default
	// (MaxRestarts 0): a configured maxRestarts keeps its exact meaning, and a
	// long-lived service is still restarted without bound. Design choice for
	// k70: fastFailLimit failures in a row, each within the window, and only
	// failures count (see k70).
	giveUp := func() bool {
		if spec.MaxRestarts > 0 || quickFails < fastFailLimit {
			return false
		}
		s.logf("[%s] failed %d times in a row, each within %s of its start — giving up (no automatic restart)", spec.Name, quickFails, s.fastFailWindow)
		return true
	}

	// releaseStreams flushes what the finished process left in its pipes into
	// the log buffer before the exit is reported, then drops the read ends.
	releaseStreams := func() {
		streams.wait(s.drainTimeout)
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
	// backoff, ctrlStart, ctrlRestart); pre says which start it is, and is
	// passed on to PreStart with AfterGiveUp filled in.
	//
	// Once ctx is done it starts nothing, PreStart included (k182). A due timer
	// or a start call can be taken in the same turn as the shutdown: select
	// picks among ready cases at random, and a call waiting at an exit or at a
	// due timer is taken ahead of the next turn. The process would only be
	// killed again on that next turn, which then takes the shutdown. A
	// shutdown that comes while PreStart runs (the editor's shells out) stops
	// the start before the spawn, for the same reason.
	start := func(pre Start) {
		// shuttingDown reports a done ctx and records it as the reason the
		// start failed, so a start or restart call says why, not a stale error.
		shuttingDown := func() bool {
			if ctx.Err() == nil {
				return false
			}
			lastErr = "the launcher is shutting down"
			s.logf("[%s] not started: shutting down", spec.Name)
			return true
		}
		if shuttingDown() {
			return
		}
		pre.AfterGiveUp = afterGiveUp
		afterGiveUp = false
		if spec.PreStart != nil {
			svc.preStarting.Store(true)
			spec.PreStart(pre)
			svc.preStarting.Store(false)
			if shuttingDown() {
				return
			}
		}
		// startFailed reports a start that will not be retried. The service was
		// desired and is not coming back, which is exactly what OnExit is for —
		// without it a missing agent.command leaves the user with a dead console
		// while the editor keeps running in the background.
		startFailed := func() {
			afterGiveUp = true
			if spec.OnExit != nil {
				// Own goroutine, same reason as in the exit branch below: the
				// handler may block and steer us via svc.ctrl.
				go spec.OnExit(Exit{StartFailed: true})
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
				lastErr = "output pipe: " + err.Error()
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
			lastErr = err.Error()
			if isNotFound(err) {
				// The program does not exist at all — retries would be pointless.
				s.logf("[%s] program not found — no further attempt (check path / installation)", spec.Name)
				startFailed()
				desired = false
				return
			}
			// No process to time: a failed start is the earliest failure there is.
			countRun(true)
			if desired && shouldRestart(spec, restarts, false) && !giveUp() {
				restarts++
				// As after an exit: the log says that a retry comes (k152).
				s.logf("[%s] restart %d%s in %s", spec.Name, restarts, maxStr(spec), spec.RestartDelay)
				if spec.OnStartRetry != nil {
					spec.OnStartRetry(Retry{Err: lastErr, Restart: restarts, MaxRestarts: spec.MaxRestarts, Delay: spec.RestartDelay})
				}
				backoff, backoffAuto = time.After(spec.RestartDelay), true
				return
			}
			startFailed()
			return
		}
		startedAt = time.Now()
		lastErr = ""
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
		st := ServiceStatus{Name: spec.Name, Desired: desired, Restarts: restarts, LastError: lastErr, LastExit: lastExit}
		if cmd != nil && cmd.Process != nil {
			st.Running = true
			st.PID = cmd.Process.Pid
		}
		return st
	}

	// Initial start (with an optional delay) — not when the launcher is already
	// shutting down: the process would only be killed again on the next turn.
	if desired && ctx.Err() == nil {
		if spec.StartDelay > 0 {
			backoff, backoffAuto = time.After(spec.StartDelay), false
		} else {
			start(Start{})
		}
	}

	// replacesRestart reports whether a start somebody asks for now takes
	// the place of the restart policy's pending start (Start.ReplacesRestart);
	// the delayed first start is not one.
	replacesRestart := func() bool {
		return cmd == nil && backoff != nil && backoffAuto
	}

	// handle carries out a control message and answers it.
	handle := func(msg ctrlMsg) {
		switch msg.kind {
		case ctrlStart:
			desired = true
			restarts, quickFails, lastExit = 0, 0, ""
			pre := Start{ReplacesRestart: replacesRestart()}
			backoff = nil
			if cmd == nil {
				start(pre)
			}
		case ctrlStop:
			desired = false
			backoff = nil
			stop()
			s.logf("[%s] stopped (manual)", spec.Name)
		case ctrlRestart:
			desired = true
			restarts, quickFails, lastExit = 0, 0, ""
			pre := Start{ReplacesRestart: replacesRestart()}
			backoff = nil
			stop()
			start(pre)
			if cmd != nil { // a failed start has logged its own lines
				s.logf("[%s] restarted (manual)", spec.Name)
			}
		case ctrlStatus:
			// snapshot only
		}
		if msg.reply != nil {
			msg.reply <- snapshot()
		}
	}

	for {
		select {
		case <-ctx.Done():
			s.logf("[%s] stopping …", spec.Name)
			stop()
			return

		case <-backoff:
			// A control message that is ready together with the due timer —
			// select picks either at random — is taken before the timer's
			// start. A start, restart or stop is carried out instead of it:
			// a stop cancels the start, so nothing comes up just to be killed,
			// and a start or restart takes the place of a pending restart
			// (backoff is still set, so it says ReplacesRestart), as during
			// the delay. A status is answered after the timer's start, as on
			// the next turn.
			if s.backoffTaken != nil {
				s.backoffTaken()
			}
			var pending *ctrlMsg
			select {
			case msg := <-svc.ctrl:
				pending = &msg
			default:
			}
			if pending != nil && pending.kind != ctrlStatus {
				handle(*pending)
				break // out of the select: the message took the timer's place
			}
			backoff = nil
			if desired && cmd == nil {
				start(Start{Automatic: backoffAuto})
			}
			if pending != nil {
				handle(*pending)
			}

		case err := <-waitChOrNil(waitCh):
			// Timed before the drain, which may wait for a grandchild.
			failedEarly := err != nil && time.Since(startedAt) < s.fastFailWindow
			countRun(failedEarly)
			if job != nil {
				job.Close()
			}
			// Flush the trailing output first: the lines a crashing process
			// wrote last are the ones the user needs.
			releaseStreams()
			cmd, job, waitCh = nil, nil, nil
			success := err == nil
			exit := describeExit(err)
			s.logf("[%s] exited (%s)", spec.Name, exit)
			// A control message that came while this exit was handled (the
			// drain can take drainTimeout), or that was ready together with
			// it — select picks either at random — is taken before the exit is
			// acted on. A stop wins: the run ends as the stop's, with no
			// restart, no OnExit and lastExit kept, as when select picks the
			// stop first and stop() collects the exit. Any other message is
			// carried out after the exit, as on the next turn. A start or
			// restart is the user's action, not an end for OnExit to judge:
			// the exit counts as any exit does (lastExit, the fast-fail count,
			// a give-up, the restart it arms), but OnExit does not fire, and
			// the call then starts the service — in place of the restart the
			// exit armed (ReplacesRestart) or as the first start after a
			// failure that is not restarted (AfterGiveUp), and clearing
			// lastExit and the counts as it always does.
			if s.exitTaken != nil {
				s.exitTaken()
			}
			var pending *ctrlMsg
			select {
			case msg := <-svc.ctrl:
				pending = &msg
			default:
			}
			if pending != nil && pending.kind == ctrlStop {
				desired = false
			} else {
				lastExit = exit
			}
			takenOver := pending != nil && (pending.kind == ctrlStart || pending.kind == ctrlRestart)
			restart := desired && shouldRestart(spec, restarts, success)
			gaveUp := restart && giveUp()
			if restart && !gaveUp {
				restarts++
				s.logf("[%s] restart %d%s in %s", spec.Name, restarts, maxStr(spec), spec.RestartDelay)
				backoff, backoffAuto = time.After(spec.RestartDelay), true
			} else if desired {
				if !gaveUp { // the give-up has logged its own line
					s.logf("[%s] no automatic restart (policy=%s)", spec.Name, spec.Restart)
				}
				afterGiveUp = !success
				if spec.OnExit != nil && !takenOver {
					// Own goroutine: the handler may block (TTY prompt) and steer us
					// via svc.ctrl (start/restart) — doing it synchronously would
					// deadlock, because we are exactly that loop.
					go spec.OnExit(Exit{Success: success, StartFailed: failedEarly})
				}
			}
			if pending != nil {
				handle(*pending)
			}

		case msg := <-svc.ctrl:
			handle(msg)
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

// newCombinedStreamSet points stdout and stderr of c at one shared pipe, so
// the two stay interleaved the way the child wrote them — the order a build
// log is read in — and a single drain goroutine is the only writer.
func newCombinedStreamSet(c *exec.Cmd) (*streamSet, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	c.Stdout, c.Stderr = w, w
	return &streamSet{
		read:  []*os.File{r},
		write: []*os.File{w},
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
