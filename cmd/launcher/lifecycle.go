package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// console is the launcher's own terminal, which the exit prompt and the
// launcher console read and write. run() makes one for the session
// (newConsole) and hands the same one to both, so they share its reader.
type console struct {
	in  *lineReader
	out io.Writer
	// logSeparate is set when the launcher log does not go to out (window
	// mode's unreagent.log): the launcher console then answers on out and
	// logs the answer as well.
	logSeparate bool
}

// newConsole is the console on in and out for a launcher that logs to log.
func newConsole(in io.Reader, out, log io.Writer) console {
	return console{in: &lineReader{in: in}, out: out, logSeparate: log != out}
}

// reply answers the user of the launcher console: on the console and in the
// launcher log, once when the log is the console itself.
func (c console) reply(logger func(string), line string) {
	if c.logSeparate {
		fmt.Fprintln(c.out, line)
	}
	logger(line)
}

// show puts line on the console when the launcher log is elsewhere, for what
// the log holds in a line of its own already: the supervisor's started,
// stopped and restarted lines (k151). With the log on the console, that line
// is the answer, and nothing is written beside it.
func (c console) show(line string) {
	if c.logSeparate {
		fmt.Fprintln(c.out, line)
	}
}

// lineReader is the session's one reader of the console input. It reads a
// byte at a time and never past the line it returns: a bufio.Scanner buffers
// ahead, and with piped input the lines it took past its own were lost to the
// next reader — the next exit prompt, the launcher console, or the foreground
// agent, which reads the same stdin itself after a handover. One read call
// per byte is nothing for typed input. It is not safe for concurrent reads;
// the k71 handover keeps the readers to one at a time.
type lineReader struct {
	in io.Reader
	// started is set by the first readLine, the only one that drops a byte
	// order mark (utf8BOM).
	started bool
}

// utf8BOM is the byte order mark in UTF-8. Windows PowerShell 5.1 writes it
// before what it pipes into a native program when its $OutputEncoding is
// UTF-8 ([Text.Encoding]::UTF8), so 'quit' | unreagent.exe arrived as an
// unknown command (k213). It comes once per stream, at its very start, however
// many lines follow: only the first line can carry it, and a U+FEFF anywhere
// later is input.
const utf8BOM = "\xef\xbb\xbf"

// readLine returns the next line without its line end (CRLF included), and
// false at EOF or on a read error — after a last line without a line end.
// The first line comes without a leading byte order mark.
func (r *lineReader) readLine() (string, bool) {
	var line []byte
	b := make([]byte, 1)
	bom := !r.started
	r.started = true
	for {
		n, err := r.in.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				return strings.TrimSuffix(string(line), "\r"), true
			}
			line = append(line, b[0])
			if bom && len(line) == len(utf8BOM) {
				bom = false
				if string(line) == utf8BOM {
					line = line[:0]
				}
			}
		}
		if err != nil {
			return string(line), len(line) > 0
		}
	}
}

// openLauncherLog creates the launcher log of window mode at path. The previous
// session's log is kept as path.1 — one generation, so the log of a session
// that crashed survives the relaunch. When it cannot be moved aside, this
// session is appended to it instead of truncating it. What fails is said on
// warn (the real stderr): the log is the very thing that is missing, and the
// agent is about to take the console. It returns nil when the log cannot be
// created; the launcher logs then go to the console.
func openLauncherLog(path string, warn io.Writer) *os.File {
	flag := os.O_RDWR | os.O_CREATE | os.O_TRUNC // as os.Create
	if err := os.Rename(path, path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(warn, "WARN could not keep the previous launcher log as %s.1: %v — this session is appended to %s\n", path, err, path)
		flag = os.O_RDWR | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(path, flag, 0o666)
	if err != nil {
		fmt.Fprintf(warn, "WARN could not create the launcher log: %v — the launcher logs go to this console and mix with the agent's screen\n", err)
		return nil
	}
	return f
}

// session is the launcher's run from start to shutdown and carries its outcome
// to run()'s return value, i.e. the exit code. Design choice for k69: a signal
// ends it with exit 0, as that is the user's or the service manager's
// decision; when the launcher ends it itself, the result is the agent's last
// outcome (end) or a failure of its own (fail) (see k69).
type session struct {
	ctx    context.Context // done once the session is ending, for whatever reason
	cancel context.CancelFunc

	mu       sync.Mutex
	agentErr error // how the agent's last run ended; nil = no run ended yet, or exit 0
	ended    bool  // the launcher ended the session itself, not a signal
	err      error // the result, fixed when the launcher ended the session
}

var errAgentFailed = errors.New("the agent failed: a non-zero exit, a failed start or a crash loop (see the launcher log)")

func newSession(ctx context.Context, cancel context.CancelFunc) *session {
	return &session{ctx: ctx, cancel: cancel}
}

// agentExited records how the agent's last run ended, for a later end().
func (s *session) agentExited(success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if success {
		s.agentErr = nil
	} else {
		s.agentErr = errAgentFailed
	}
}

// end ends the session from inside the launcher — the agent's exit handling,
// 'q' on a console — with the agent's last outcome as the result.
func (s *session) end() {
	s.mu.Lock()
	s.finishLocked(s.agentErr)
	s.mu.Unlock()
	s.cancel()
}

// fail ends the session as a failure, whatever the agent did.
func (s *session) fail(err error) {
	s.mu.Lock()
	s.finishLocked(err)
	s.mu.Unlock()
	s.cancel()
}

// finishLocked fixes the result unless the session is ending already: the
// first reason wins. That keeps a signal at exit 0 when the agent, hit by the
// same Ctrl-C, dies a moment later and its exit handling ends the session too.
// It narrows that race, it does not close it: when the agent's exit handling
// gets there before the signal reaches the launcher, the agent's outcome
// stands. How Ctrl-C reaches a foreground agent on Windows is unverified.
func (s *session) finishLocked(err error) {
	if s.ended || s.ctx.Err() != nil {
		return
	}
	s.ended, s.err = true, err
}

// endedByLauncher reports whether the launcher ended the session itself.
func (s *session) endedByLauncher() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

// result is what run() returns: nil after a signal, else how the launcher
// ended the session.
func (s *session) result() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// makeEditorExitHandler builds the editor's OnExit. On every failure the
// editor is not restarted after, with or without an agent, it says where the
// crash dumps are in projectDir, when that holds any ("" for none): the next start keeps them
// (followsCrash). Without an agent it also acts on an editor whose last run
// never got going (not found, cannot be executed, or failed within the
// fast-fail window of its start); one that ran and then ended is left to its
// restart policy, as before. Design choice for k69: with MCP serving, an
// external agent can still read the logs and retry ue_start, so the launcher
// keeps running and says so. With MCP not serving (off, or stopped serving
// since: see serveMCP) and no agent, nothing can retry on the user's behalf,
// so the session ends with exit 1 instead of idling until somebody presses
// Ctrl-C — also when the failed start was typed into the launcher console;
// that user can start the launcher again (see k69). mcpServing is read at the
// editor's exit, not when the handler is built; a give-up MCP still serves is
// left in giveUp, which ends the session should MCP stop serving before the
// editor is started again (k155). modulesPointer (nil for none) is asked at
// each failure, with whether the give-up ends the session, and returns the
// sentence the line ends with when the project's modules look unbuilt then,
// "" otherwise (k247).
func makeEditorExitHandler(sess *session, logger func(string), agent bool, mcpServing *atomic.Bool, giveUp *editorGiveUp, u config.UnrealConfig, projectDir string, modulesPointer func(ends bool) string) func(supervisor.Exit) {
	return func(e supervisor.Exit) {
		if e.Success {
			return
		}
		// The k69 give-up: with no agent, only MCP is left to retry an editor
		// whose last run never got going.
		gaveUp := !agent && e.StartFailed
		serving := gaveUp && giveUp.keepServing(mcpServing)
		ends := gaveUp && !serving
		// dumps says where the failed runs' crash dumps are, and what empties
		// that folder: a retry from this launcher does not (followsCrash), the
		// first start of a new launcher run does with cleanOnRestart.
		dumps := ""
		if crashDumpsExist(projectDir) {
			dumps = " Crash dumps are in " + filepath.Join(projectDir, "Saved", "Crashes")
			switch {
			case u.CleanOnRestart && !ends:
				dumps += " — a retry keeps them, a new launcher run empties them (unreal.cleanOnRestart)"
			case u.CleanOnRestart:
				dumps += " — the next launcher run empties them (unreal.cleanOnRestart)"
			}
			dumps += "."
		}
		// Where the modules still look unbuilt, the cause the startup WARN
		// named minutes ago (k247). Text only: the restart policy is unchanged.
		if modulesPointer != nil {
			dumps += modulesPointer(ends)
		}
		if !gaveUp {
			logger("The editor failed and is not restarted." + dumps)
			return
		}
		if serving {
			logger("The editor " + editorFailed + ", and is not restarted — the launcher keeps serving MCP: read the logs, fix the cause, retry with ue_start (Ctrl-C stops the launcher)." + dumps)
			return
		}
		logger("The editor " + editorFailed + ", and is not restarted; no agent is configured and MCP is not serving — stopping everything." + dumps)
		sess.fail(errEditorGaveUp)
	}
}

// crashDumpsExist reports whether Saved/Crashes under projectDir holds any
// entry: only then does a give-up line point there (k253). "" or a project
// without the folder has none, e.g. an editor that never started because a
// module was unbuilt.
func crashDumpsExist(projectDir string) bool {
	if projectDir == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(projectDir, "Saved", "Crashes"))
	return err == nil && len(entries) > 0
}

// editorFailed is how the k69 give-up lines say that the editor's last run
// never got going.
var editorFailed = fmt.Sprintf("failed to start, or failed within %s of starting", supervisor.FastFailWindow)

// errEditorGaveUp is run()'s error for the k69 give-up, in either order of
// its two events (see editorGiveUp).
var errEditorGaveUp = errors.New("the editor " + editorFailed + ", and with no agent and MCP not serving nothing can retry it (see the launcher log)")

// editorGiveUp carries a k69 give-up that MCP still serves to the moment MCP
// stops serving (k155). With no agent, the editor's give-up and MCP not
// serving end the session with exit 1, whichever of the two comes second.
// They come on goroutines of their own — the editor's OnExit and serveMCP's —
// so one lock orders them, and exactly one of them ends the session. A start
// of the editor takes the give-up back. One window is left open: a start
// that reaches the editor's control loop after the exit and before the exit
// handler takes the lock is taken back before the give-up is recorded, so a
// later serve failure ends the session with that editor running. The zero
// value is ready.
type editorGiveUp struct {
	mu      sync.Mutex
	waiting bool // the editor gave up while MCP served, and was not started since
}

// keepServing is the editor's give-up (makeEditorExitHandler). It reports
// whether MCP serves, and while it does, the give-up waits for mcpStopped.
// serving is read under the lock: serveMCP clears it before mcpStopped takes
// the lock, so of the two, the later one sees the other.
func (g *editorGiveUp) keepServing(serving *atomic.Bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.waiting = serving.Load()
	return g.waiting
}

// started takes a give-up back: the editor is starting again (its PreStart).
func (g *editorGiveUp) started() {
	g.mu.Lock()
	g.waiting = false
	g.mu.Unlock()
}

// mcpStopped is told that MCP stopped serving (serveMCP). After a give-up
// that waits for it, nothing is left that could retry the editor, and it ends
// sess with exit 1 as the k69 rule does with MCP off. Otherwise — an agent is
// configured, or the editor has not given up — the serve failure stays
// serveMCP's ERROR line (k45, k123).
func (g *editorGiveUp) mcpStopped(sess *session, logger func(string)) {
	g.mu.Lock()
	waiting := g.waiting
	g.waiting = false
	g.mu.Unlock()
	if !waiting {
		return
	}
	logger("MCP stopped serving, and the editor has not been started again since it " + editorFailed + "; no agent is configured and MCP is not serving — stopping everything.")
	sess.fail(errEditorGaveUp)
}

// makeAgentExitHandler builds the callback that fires when the agent ends and is
// not restarted on its own (see ServiceSpec.OnExit). In window mode the agent is
// the leading process — once it ends the session is over, and the launcher must
// not silently hang around with a running editor.
func makeAgentExitHandler(ctx context.Context, sess *session, sup *supervisor.Supervisor, cfg *config.Config, logger func(string), interactive bool, con console) func(success bool) {
	var mu sync.Mutex
	cmdLoopRunning := false // already switched to the launcher console?
	stop := sess.end
	// restartAgent is the one way the agent is started again from here: the
	// prompt's 'r', and the launcher console handing the window back. A start
	// that fails is named by the exit prompt (agentEnded), not here: the agent
	// comes back to this handler once its restart policy stops retrying it —
	// at once for a missing program — and a line printed here would race that
	// prompt for the console.
	restartAgent := func() {
		fmt.Fprintln(con.out, "Restarting agent …")
		if _, err := sup.StartService("agent"); err != nil {
			logger("Agent restart failed: " + err.Error() + " — stopping everything.")
			sess.fail(fmt.Errorf("agent restart failed: %w", err))
		}
	}
	return func(success bool) {
		sess.agentExited(success)
		if success {
			logger("Agent exited (exit 0).")
		} else {
			logger("Agent failed and will not be restarted automatically.")
		}

		// If the launcher console is already running (an earlier 'k' choice), a
		// second stdin prompt would compete with it for the input — so only log.
		mu.Lock()
		busy := cmdLoopRunning
		mu.Unlock()
		if busy {
			con.reply(logger, "Launcher console active — 'q' stops everything, 'start agent' restarts the agent.")
			return
		}

		// The onExit lines go to the console too (k151): in window mode the
		// agent has handed it back. There they are led by how the agent ended,
		// which the log has in the line above (k188).
		onExitLine := func(line string) {
			logger(line)
			con.show(withEnded(agentEnded(sup, success), line))
		}
		switch cfg.Agent.OnExit {
		case config.OnExitLeave:
			onExitLine("agent.onExit=leave — editor/MCP keep running. Ctrl-C stops the launcher.")
		case config.OnExitShutdown:
			onExitLine("agent.onExit=shutdown — stopping everything.")
			stop()
		default: // ask
			if !interactive {
				// Headless (-p): no TTY to ask on — the command loop reads stdin.
				// Agent end = job done -> shut down cleanly.
				stop()
				return
			}
			switch promptAgentExit(con, agentEnded(sup, success)) {
			case "k":
				fmt.Fprintln(con.out, "Editor keeps running. Launcher console — 'start agent' gives this window back to the agent, 'q' stops everything:")
				mu.Lock()
				cmdLoopRunning = true
				mu.Unlock()
				go func() {
					// The console reads stdin until a command would start the
					// agent, which needs stdin itself (k71). By the time the loop
					// returns it has stopped reading, and the flag is clear before
					// the agent can end again, so that exit gets the prompt.
					handover := commandLoop(ctx, con, stop, sup, logger, "agent")
					mu.Lock()
					cmdLoopRunning = false
					mu.Unlock()
					if handover {
						restartAgent()
					}
				}()
			case "r":
				restartAgent()
			default: // Enter / q / Timeout / EOF
				stop()
			}
		}
	}
}

// agentEnded is the exit prompt's first line: how the agent's last run ended.
// A start that failed (the program is missing, …) comes here as an exit too;
// it is named with its start error, as ue_start names it. The error stays in
// lastError only until a start succeeds, so after a run it is empty.
func agentEnded(sup *supervisor.Supervisor, success bool) string {
	if success {
		return "Agent exited."
	}
	for _, st := range sup.Status() {
		if st.Name == "agent" && st.LastError != "" {
			return "Agent did not start: " + st.LastError
		}
	}
	return "Agent crashed."
}

// agentRetryLine is the console's line for a failed agent start that the
// restart policy retries (k152), worded as agentEnded names the start the
// policy gives up on: one per retry, and none for that last one, which the
// exit prompt names instead.
func agentRetryLine(r supervisor.Retry) string {
	return fmt.Sprintf("Agent did not start: %s — retry %d%s in %s", r.Err, r.Restart, restartLimit(r.MaxRestarts), r.Delay)
}

// restartLimit is "/max" for a bounded restart count, "" for none, as the
// supervisor's restart lines have it.
func restartLimit(maxRestarts int) string {
	if maxRestarts > 0 {
		return fmt.Sprintf("/%d", maxRestarts)
	}
	return ""
}

// restartIn is the supervisor's "restart N[/M] in D" for a restart it armed.
func restartIn(restart, maxRestarts int, delay time.Duration) string {
	return fmt.Sprintf("restart %d%s in %s", restart, restartLimit(maxRestarts), delay)
}

// agentRestartLine is the console's line for an agent run that ended and
// that the restart policy restarts (k188), worded as agentRetryLine: one per
// restart, and none for a run that is not restarted, which the exit prompt
// or the onExit line names instead. Only for e.Restart > 0.
func agentRestartLine(e supervisor.RunEnd) string {
	return fmt.Sprintf("Agent exited (%s) — %s", e.Exit, restartIn(e.Restart, e.MaxRestarts, e.Delay))
}

// runEndLines are the supervisor's log lines for a run of service name that
// ended (k188): "[name] exited (…)", and "[name] restart N[/M] in D" when a
// restart follows. In window mode the console shows the editor's as the log
// has them.
func runEndLines(name string, e supervisor.RunEnd) []string {
	lines := []string{fmt.Sprintf("[%s] exited (%s)", name, e.Exit)}
	if e.Restart > 0 {
		lines = append(lines, fmt.Sprintf("[%s] %s", name, restartIn(e.Restart, e.MaxRestarts, e.Delay)))
	}
	return lines
}

// startRetryLines are the supervisor's log lines for a failed start of
// service name that the policy retries (k192): "[name] start failed: …" and
// its "[name] restart N[/M] in D".
func startRetryLines(name string, r supervisor.Retry) []string {
	return []string{
		fmt.Sprintf("[%s] start failed: %s", name, r.Err),
		fmt.Sprintf("[%s] %s", name, restartIn(r.Restart, r.MaxRestarts, r.Delay)),
	}
}

// alertLines writes lines to alert, each with "unreagent: " in front, the way
// run() mirrors a line the user must see in window mode (k152); nothing when
// alert is nil, where the log is the console already.
func alertLines(alert io.Writer, lines ...string) {
	if alert == nil {
		return
	}
	for _, line := range lines {
		fmt.Fprintln(alert, "unreagent: "+line)
	}
}

// alertHeldRestart is service name's OnStartHeld in window mode: it shows on
// alert the log's line (supervisor.HeldLine) for a restart the policy armed
// and that now waits for the last run's crash reporter, so the console's
// "restart N in D" before it is not left standing (k188). A start somebody
// asked for is answered where it was asked (startHeld), so it gets no line.
func alertHeldRestart(alert io.Writer, name string) func(supervisor.Start) {
	return func(st supervisor.Start) {
		if st.Automatic {
			alertLines(alert, supervisor.HeldLine(name))
		}
	}
}

// alertAll returns a logger that writes every line to logger and to alert
// (alertLines); logger itself when alert is nil.
func alertAll(logger func(string), alert io.Writer) func(string) {
	if alert == nil {
		return logger
	}
	return func(line string) {
		logger(line)
		alertLines(alert, line)
	}
}

// withEnded joins ended, how the agent ended (agentEnded), and line into one
// console line, with one full stop between them.
func withEnded(ended, line string) string {
	if !strings.HasSuffix(ended, ".") {
		ended += "."
	}
	return ended + " " + line
}

// promptAgentExit shows the selection menu under ended (agentEnded) on the
// real console (not the logger — in window mode the launcher logs go to the
// file) and returns the choice. After 30s without input, "stop everything"
// applies (e.g. agent crashed overnight).
func promptAgentExit(con console, ended string) string {
	fmt.Fprintln(con.out, "\n"+ended)
	fmt.Fprintln(con.out, "  [Enter] stop everything (UE + launcher)")
	fmt.Fprintln(con.out, "  [k]     keep the editor running, launcher console")
	fmt.Fprintln(con.out, "  [r]     restart the agent")
	fmt.Fprint(con.out, "> ")

	// The read outlives a timeout; harmless, as a timeout ends the session.
	ch := make(chan string, 1)
	go func() {
		line, _ := con.in.readLine() // EOF (e.g. stdin closed): "" -> stop everything
		ch <- strings.ToLower(strings.TrimSpace(line))
	}()
	select {
	case s := <-ch:
		return s
	case <-time.After(30 * time.Second):
		fmt.Fprintln(con.out, "\n(Timeout) — stopping everything.")
		return ""
	}
}

// commandLoop reads control commands from the console (for manual operation)
// until 'q', EOF or the end of ctx. foreground names a service that needs the
// console itself while it runs ("" for none). A command that would (re)start
// it is not run here: the loop returns true before it reads another line, and
// the caller hands the console over and starts the service — two readers on
// one stdin split the user's input between them (k71). Its answers go to the
// console and the launcher log (console.reply).
func commandLoop(ctx context.Context, con console, stop func(), sup *supervisor.Supervisor, logger func(string), foreground string) (handover bool) {
	reply := func(line string) { con.reply(logger, line) }
	// act runs a lifecycle command and names what went wrong, as serviceAction
	// does for ue_start / ue_stop / ue_restart: an error, or — for a command
	// that starts — a service not running afterwards, with the start error.
	// A start held for the last run's crash reporter is no error and says
	// so. One that went through says on the console (console.show) what the
	// service is now: done ("is running", "restarted", "stopped") and the pid
	// of a running one.
	act := func(fn func(string) (supervisor.ServiceStatus, error), name string, starts bool, done string) {
		st, err := fn(name)
		switch {
		case err != nil:
			reply("Error: " + err.Error())
		case starts && !st.Running && st.Waiting != "":
			con.show(startHeld(name))
		case starts && !st.Running:
			reply("Error: " + startFailure(name, st))
		case st.Running:
			con.show(fmt.Sprintf("%s %s (pid %d)", name, done, st.PID))
		default:
			con.show(name + " " + done)
		}
	}
	reply("Commands: 'status' | 'r' (restart all) | 'r <name>' | 'stop <name>' | 'start <name>' | 'c <name>' | 'q'")
	for {
		line, ok := con.in.readLine()
		if !ok {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		default:
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "q", "quit", "exit":
			reply("Stopping …")
			stop()
			return false
		case "status", "s":
			b, _ := json.MarshalIndent(sup.Status(), "", "  ")
			reply("\n" + string(b))
		case "r", "restart":
			if len(fields) > 1 {
				if fields[1] == foreground {
					return true
				}
				act(sup.RestartService, fields[1], true, "restarted")
			} else {
				for _, n := range sup.ServiceNames() {
					if n != foreground {
						act(sup.RestartService, n, true, "restarted")
					}
				}
				if foreground != "" {
					return true
				}
			}
		case "stop":
			if len(fields) > 1 {
				act(sup.StopService, fields[1], false, "stopped")
			}
		case "start":
			if len(fields) > 1 {
				if fields[1] == foreground {
					return true
				}
				act(sup.StartService, fields[1], true, "is running")
			}
		case "c", "cmd":
			if len(fields) > 1 {
				res, err := sup.RunCommand(fields[1])
				if err != nil {
					reply("Error: " + err.Error())
				} else {
					reply(fmt.Sprintf("exit %d\n%s", res.ExitCode, tailLines(res.Output, 100)))
				}
			}
		default:
			reply("Unknown command: " + fields[0])
		}
	}
}

// editorPreStart is the editor's PreStart: the cleanup of
// unreal.cleanOnRestart in projectDir ("" for none). A start that follows a
// crash (followsCrash) spares that crash's evidence: Saved/Crashes stays.
//
// It kills no crash reporter (k154). The editor starts CrashReportClientEditor
// as its crash monitor for its whole life (-MONITOR=<editor pid>), not after a
// crash, so the taskkill /IM it used to run ended the monitor of every other
// editor on the machine (k154, run 52). Every reporter of ours is in our
// editor's job (the engine starts it without breakaway): it ends when that job
// is closed, after crashReporterGrace at the latest, and the supervisor has
// the next start wait for that. What the kill still reached was a reporter of
// an editor the launcher did not start, which is not ours to end.
func editorPreStart(u config.UnrealConfig, projectDir string, logger func(string)) func(supervisor.Start) {
	return func(st supervisor.Start) {
		if u.CleanOnRestart && projectDir != "" {
			cleanRecovery(projectDir, st, logger)
		}
	}
}

// crashReporterGrace is how long the editor's job stays open after a failed
// run while its crash reporter is still in it (ServiceSpec.ReporterGrace,
// k154). The reporter needed about 5 s after the editor's exit to symbolicate
// the call stack, fill in CrashContext.runtime-xml and send (run 34); 30 s
// leaves room for a cold symbol load or a slow send. The bound is for a
// reporter that never ends on its own — one showing its window without
// -unattended waits for a human — and it delays only the editor's next start,
// which waits for the job: a launcher shutdown closes the job at once.
const crashReporterGrace = 30 * time.Second

// followsCrash reports whether st is a start that keeps the last crash's
// evidence for diagnosis: the restart policy's, a start somebody asks for in
// its place while it is pending (console start or r, ue_start, ue_restart
// within the restart delay), and the first start after the editor failed and
// was not restarted — the one the give-up line asks for
// (makeEditorExitHandler). Each comes before anybody has read the dumps.
func followsCrash(st supervisor.Start) bool {
	return st.Automatic || st.ReplacesRestart || st.AfterGiveUp
}

// editorArgs is the editor's argument list: the project, unreal.args, and
// -unattended unless unreal.unattended is off or the args already carry it.
func editorArgs(u config.UnrealConfig, logger func(string)) []string {
	args := append([]string{}, u.Args...)
	if u.Project != "" {
		args = append([]string{u.Project}, args...)
	}
	if boolVal(u.Unattended) && !hasEditorSwitch(args, "unattended") {
		args = append(args, "-unattended")
		logger("Unreal: -unattended active (no crash dialog, no recovery prompt)")
	}
	return args
}

// hasEditorSwitch reports whether args carry the editor switch name the way
// the engine reads its command line (FParse::Param): "-" or "/" in front, the
// name without regard to case, and nothing after it — "-name=1" is not the
// switch. Editor arguments only; an agent's flags are case-sensitive (hasArg).
func hasEditorSwitch(args []string, name string) bool {
	for _, a := range args {
		if a == "" || (a[0] != '-' && a[0] != '/') {
			continue
		}
		if strings.EqualFold(a[1:], name) {
			return true
		}
	}
	return false
}

// cleanRecovery removes recovery/crash artefacts for a clean start.
// PackageRestoreData.json goes on every start, so that a stale restore offer
// does not outlive the session it came from. Read from the UE 5.8.4 source,
// not run: only an editor started without -unattended shows the modal restore
// dialog and blocks on it; with -unattended
// FPackageAutoSaver::HasPackagesToRestore
// (Engine/Source/Editor/UnrealEd/Private/PackageAutoSaver.cpp) is false, so
// FUnrealEdMisc::OnInit (UnrealEdMisc.cpp, same directory) never calls
// OfferToRestorePackages — no dialog, nothing restored, and the file keeps
// its entries until a clean exit (UUnrealEdEngine::FinishDestroy) or the
// next restore-file update. Saved/Crashes is emptied only on a start that does
// not follow a crash (followsCrash): the crash dump is what the agent reads
// to find out why.
func cleanRecovery(projectDir string, st supervisor.Start, logger func(string)) {
	saved := filepath.Join(projectDir, "Saved")
	restore := filepath.Join(saved, "Autosaves", "PackageRestoreData.json")
	if err := os.Remove(restore); err == nil {
		logger("Recovery: PackageRestoreData.json removed")
	}
	crashes := filepath.Join(saved, "Crashes")
	if entries, err := os.ReadDir(crashes); err == nil && len(entries) > 0 {
		if followsCrash(st) {
			why := "automatic restart — the crash dump stays for diagnosis"
			switch {
			case st.ReplacesRestart:
				why = "start in place of the pending automatic restart — the crash dump stays for diagnosis"
			case !st.Automatic:
				why = "first start since the editor failed and was not restarted — the crash dumps stay for diagnosis"
			}
			logger("Recovery: Saved/Crashes kept (" + why + ")")
			return
		}
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(crashes, e.Name()))
		}
		logger("Recovery: Saved/Crashes emptied")
	}
}
