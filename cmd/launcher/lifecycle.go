package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// lineReader is the session's one reader of the console input. It reads a
// byte at a time and never past the line it returns: a bufio.Scanner buffers
// ahead, and with piped input the lines it took past its own were lost to the
// next reader — the next exit prompt, the launcher console, or the foreground
// agent, which reads the same stdin itself after a handover. One read call
// per byte is nothing for typed input. It is not safe for concurrent reads;
// the k71 handover keeps the readers to one at a time.
type lineReader struct {
	in io.Reader
}

// readLine returns the next line without its line end (CRLF included), and
// false at EOF or on a read error — after a last line without a line end.
func (r *lineReader) readLine() (string, bool) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := r.in.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				return strings.TrimSuffix(string(line), "\r"), true
			}
			line = append(line, b[0])
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
// crash dumps are in projectDir ("" for none): the next start keeps them
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
// editor's exit, not when the handler is built.
func makeEditorExitHandler(sess *session, logger func(string), agent bool, mcpServing *atomic.Bool, u config.UnrealConfig, projectDir string) func(supervisor.Exit) {
	return func(e supervisor.Exit) {
		if e.Success {
			return
		}
		serving := mcpServing.Load()
		ends := !agent && e.StartFailed && !serving
		// dumps says where the failed runs' crash dumps are, and what empties
		// that folder: a retry from this launcher does not (followsCrash), the
		// first start of a new launcher run does with cleanOnRestart.
		dumps := ""
		if projectDir != "" {
			dumps = " Any crash dumps are in " + filepath.Join(projectDir, "Saved", "Crashes")
			switch {
			case u.CleanOnRestart && !ends:
				dumps += " — a retry keeps them, a new launcher run empties them (unreal.cleanOnRestart)"
			case u.CleanOnRestart:
				dumps += " — the next launcher run empties them (unreal.cleanOnRestart)"
			}
			dumps += "."
		}
		if agent || !e.StartFailed {
			if dumps != "" {
				logger("The editor failed and is not restarted." + dumps)
			}
			return
		}
		failed := fmt.Sprintf("failed to start, or failed within %s of starting", supervisor.FastFailWindow)
		if serving {
			logger("The editor " + failed + ", and is not restarted — the launcher keeps serving MCP: read the logs, fix the cause, retry with ue_start (Ctrl-C stops the launcher)." + dumps)
			return
		}
		logger("The editor " + failed + ", and is not restarted; no agent is configured and MCP is not serving — stopping everything." + dumps)
		sess.fail(fmt.Errorf("the editor %s, and with no agent and MCP not serving nothing can retry it (see the launcher log)", failed))
	}
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

		switch cfg.Agent.OnExit {
		case config.OnExitLeave:
			logger("agent.onExit=leave — editor/MCP keep running. Ctrl-C stops the launcher.")
		case config.OnExitShutdown:
			logger("agent.onExit=shutdown — stopping everything.")
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
	act := func(fn func(string) (supervisor.ServiceStatus, error), name string, starts bool) {
		st, err := fn(name)
		switch {
		case err != nil:
			reply("Error: " + err.Error())
		case starts && !st.Running:
			reply("Error: " + startFailure(name, st))
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
				act(sup.RestartService, fields[1], true)
			} else {
				for _, n := range sup.ServiceNames() {
					if n != foreground {
						act(sup.RestartService, n, true)
					}
				}
				if foreground != "" {
					return true
				}
			}
		case "stop":
			if len(fields) > 1 {
				act(sup.StopService, fields[1], false)
			}
		case "start":
			if len(fields) > 1 {
				if fields[1] == foreground {
					return true
				}
				act(sup.StartService, fields[1], true)
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

// editorPreStart is the editor's PreStart: the crash-reporter kill
// (unreal.killCrashReporter) and the cleanup of unreal.cleanOnRestart in
// projectDir ("" for none). A start that follows a crash (followsCrash)
// spares that crash's evidence: Saved/Crashes stays, and so does a crash
// reporter, which may still be writing the report — the restart policy
// starts the editor again about one restart delay after it ended. Skipping
// the kill cannot hold the restart up, where waiting for a reporter to finish
// would need a bound and a way to tell it is done; and every other start
// still kills it, so a stuck reporter window is gone by the second start
// somebody asks for after a crash. kill is killCrashReporter; tests pass
// their own.
func editorPreStart(u config.UnrealConfig, projectDir string, kill func(), logger func(string)) func(supervisor.Start) {
	return func(st supervisor.Start) {
		if boolVal(u.KillCrashReporter) && !followsCrash(st) {
			kill()
		}
		if u.CleanOnRestart && projectDir != "" {
			cleanRecovery(projectDir, st, logger)
		}
	}
}

// followsCrash reports whether st is a start that keeps the last crash's
// evidence for diagnosis: the restart policy's, and the first start after the
// editor failed and was not restarted — the one the give-up line asks for
// (makeEditorExitHandler), before anybody has read the dumps.
func followsCrash(st supervisor.Start) bool {
	return st.Automatic || st.AfterGiveUp
}

// killCrashReporter terminates a possibly stuck crash reporter window.
func killCrashReporter() {
	for _, c := range crashReporterKillers() {
		_ = exec.Command(c[0], c[1:]...).Run() // ignore errors (the process may not exist)
	}
}

// crashReporterKillers returns the commands killCrashReporter runs.
func crashReporterKillers() [][]string {
	if runtime.GOOS == "windows" {
		return [][]string{
			{"taskkill", "/F", "/IM", "CrashReportClientEditor.exe"},
			{"taskkill", "/F", "/IM", "CrashReportClient.exe"},
		}
	}
	// pkill -x compares the process name, which Linux cuts to 15 characters
	// ("CrashReportClie"), so neither full name would ever match. -f matches
	// the whole command line instead, and a bare -f CrashReportClient hit any
	// process with the string in an argument — an editor open on
	// CrashReportClient.cpp. So the pattern pins the name to argv[0]'s last
	// path component: anchored at the start of the command line, behind an
	// optional directory part without spaces (with .* it could reach into the
	// arguments, e.g. a .../Programs/CrashReportClient directory), and ended
	// by the space before the first argument or by the end of the line (else
	// a program inside such a directory would match). The price: a crash
	// reporter started from a path with a space in it is not matched; the
	// kill is the backup to -unattended, not the guard itself.
	return [][]string{{"pkill", "-f", "^([^ ]*/)?CrashReportClient(Editor)?( |$)"}}
}

// cleanRecovery removes recovery/crash artefacts for a clean start.
// PackageRestoreData.json goes on every start: its restore dialog would block
// an unattended restart. Saved/Crashes is emptied only on a start that does
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
			if !st.Automatic {
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
