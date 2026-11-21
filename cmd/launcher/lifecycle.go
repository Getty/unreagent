package main

import (
	"bufio"
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
// launcher console read and write. run() passes os.Stdin and os.Stdout.
type console struct {
	in  io.Reader
	out io.Writer
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

// makeEditorExitHandler builds the editor's OnExit for a launcher without an
// agent. It acts only on an editor whose last run never got going (not found,
// cannot be executed, or failed within the fast-fail window of its start); one
// that ran and then ended is left to its restart policy, as before. Design
// choice for k69: with MCP serving, an external agent can still read the logs
// and retry ue_start, so the launcher keeps running and says so. With MCP not
// serving (off, or stopped serving since: see serveMCP) and no agent, nothing
// can retry on the user's behalf, so the session ends with exit 1 instead of
// idling until somebody presses Ctrl-C — also when the failed start was typed
// into the launcher console; that user can start the launcher again (see k69).
// mcpServing is read at the editor's exit, not when the handler is built.
func makeEditorExitHandler(sess *session, logger func(string), mcpServing *atomic.Bool) func(supervisor.Exit) {
	return func(e supervisor.Exit) {
		if !e.StartFailed {
			return
		}
		failed := fmt.Sprintf("failed to start, or failed within %s of starting", supervisor.FastFailWindow)
		if mcpServing.Load() {
			logger("The editor " + failed + ", and is not restarted — the launcher keeps serving MCP: read the logs, fix the cause, retry with ue_start (Ctrl-C stops the launcher).")
			return
		}
		logger("The editor " + failed + ", and is not restarted; no agent is configured and MCP is not serving — stopping everything.")
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
	// prompt's 'r', and the launcher console handing the window back.
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
			logger("Launcher console active — 'q' stops everything, 'start agent' restarts the agent.")
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
			switch promptAgentExit(con, success) {
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
					handover := commandLoop(ctx, con.in, stop, sup, logger, "agent")
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

// promptAgentExit shows the selection menu on the real console (not the logger —
// in window mode the launcher logs go to the file) and returns the choice. After
// 30s without input, "stop everything" applies (e.g. agent crashed overnight).
func promptAgentExit(con console, success bool) string {
	if success {
		fmt.Fprintln(con.out, "\nAgent exited.")
	} else {
		fmt.Fprintln(con.out, "\nAgent crashed.")
	}
	fmt.Fprintln(con.out, "  [Enter] stop everything (UE + launcher)")
	fmt.Fprintln(con.out, "  [k]     keep the editor running, launcher console")
	fmt.Fprintln(con.out, "  [r]     restart the agent")
	fmt.Fprint(con.out, "> ")

	ch := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(con.in)
		if sc.Scan() {
			ch <- strings.ToLower(strings.TrimSpace(sc.Text()))
		} else {
			ch <- "" // EOF (e.g. stdin closed) -> stop everything
		}
	}()
	select {
	case s := <-ch:
		return s
	case <-time.After(30 * time.Second):
		fmt.Fprintln(con.out, "\n(Timeout) — stopping everything.")
		return ""
	}
}

// commandLoop reads control commands from in (for manual operation) until 'q',
// EOF or the end of ctx. foreground names a service that needs the console
// itself while it runs ("" for none). A command that would (re)start it is not
// run here: the loop returns true before it reads another line, and the caller
// hands the console over and starts the service — two readers on one stdin
// split the user's input between them (k71).
func commandLoop(ctx context.Context, in io.Reader, stop func(), sup *supervisor.Supervisor, logger func(string), foreground string) (handover bool) {
	logger("Commands: 'status' | 'r' (restart all) | 'r <name>' | 'stop <name>' | 'start <name>' | 'c <name>' | 'q'")
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		fields := strings.Fields(strings.TrimSpace(sc.Text()))
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "q", "quit", "exit":
			logger("Stopping …")
			stop()
			return false
		case "status", "s":
			b, _ := json.MarshalIndent(sup.Status(), "", "  ")
			logger("\n" + string(b))
		case "r", "restart":
			if len(fields) > 1 {
				if fields[1] == foreground {
					return true
				}
				sup.RestartService(fields[1])
			} else {
				for _, n := range sup.ServiceNames() {
					if n != foreground {
						sup.RestartService(n)
					}
				}
				if foreground != "" {
					return true
				}
			}
		case "stop":
			if len(fields) > 1 {
				sup.StopService(fields[1])
			}
		case "start":
			if len(fields) > 1 {
				if fields[1] == foreground {
					return true
				}
				sup.StartService(fields[1])
			}
		case "c", "cmd":
			if len(fields) > 1 {
				res, err := sup.RunCommand(fields[1])
				if err != nil {
					logger("Error: " + err.Error())
				} else {
					logger(fmt.Sprintf("exit %d\n%s", res.ExitCode, tailLines(res.Output, 100)))
				}
			}
		default:
			logger("Unknown command: " + fields[0])
		}
	}
	return false
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
// an unattended restart. Saved/Crashes is emptied only on a start somebody
// asked for; an automatic restart follows a crash, and its dump is what the
// agent reads to find out why.
func cleanRecovery(projectDir string, automatic bool, logger func(string)) {
	saved := filepath.Join(projectDir, "Saved")
	restore := filepath.Join(saved, "Autosaves", "PackageRestoreData.json")
	if err := os.Remove(restore); err == nil {
		logger("Recovery: PackageRestoreData.json removed")
	}
	crashes := filepath.Join(saved, "Crashes")
	if entries, err := os.ReadDir(crashes); err == nil && len(entries) > 0 {
		if automatic {
			logger("Recovery: Saved/Crashes kept (automatic restart — the crash dump stays for diagnosis)")
			return
		}
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(crashes, e.Name()))
		}
		logger("Recovery: Saved/Crashes emptied")
	}
}
