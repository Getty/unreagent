package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/skills"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// runConfigEnv selects TestLauncherRunProcess and names the config it runs;
// runArgsEnv carries further launcher arguments, one per line.
const (
	runConfigEnv = "UNREAGENT_TEST_RUN_CONFIG"
	runArgsEnv   = "UNREAGENT_TEST_RUN_ARGS"
)

// TestLauncherRunProcess is not a test: it is the launcher process for the
// run() tests, selected via runConfigEnv. It runs main(), so the parent sees
// the launcher's exit code.
func TestLauncherRunProcess(t *testing.T) {
	path := os.Getenv(runConfigEnv)
	if path == "" {
		return
	}
	if os.Getenv(loadFailEnv) != "" {
		loadEmbeddedSkills = func() ([]skills.Skill, error) { return nil, errors.New("skills: injected load failure") }
	}
	if os.Getenv(acceptFailEnv) != "" {
		listen = func(network, addr string) (net.Listener, error) {
			ln, err := net.Listen(network, addr)
			if err != nil {
				return nil, err
			}
			return acceptFails{ln}, nil
		}
	}
	if path := os.Getenv(mcpBreakEnv); path != "" {
		listen = func(network, addr string) (net.Listener, error) {
			ln, err := net.Listen(network, addr)
			if err != nil {
				return nil, err
			}
			return breaksOnFile{ln, path}, nil
		}
	}
	if os.Getenv(clockStepEnv) != "" {
		var ticks atomic.Int64
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
		now = func() time.Time { return start.Add(time.Duration(ticks.Add(1)) * time.Minute) }
	}
	os.Args = []string{os.Args[0], "-config", path}
	if extra := os.Getenv(runArgsEnv); extra != "" {
		os.Args = append(os.Args, strings.Split(extra, "\n")...)
	}
	main() // exits non-zero itself
	os.Exit(0)
}

// launcherRun is one launcher process started by startLauncher.
type launcherRun struct {
	cmd    *exec.Cmd
	lines  chan string // stdout, line by line; closed at EOF
	stdout *syncLog
	stderr syncBuffer     // read while the launcher runs, too (waitStderr)
	stdin  io.WriteCloser // the launcher's stdin; set only by startLauncherPiped
	done   chan struct{}
	err    error
}

// startLauncher runs the launcher on the given unreagent.yaml. The editor and
// the agent in these configs are plain programs (sleep, false): the tests are
// about how a session ends, not about Unreal.
func startLauncher(t *testing.T, yaml string) *launcherRun {
	t.Helper()
	return startLauncherIn(t, t.TempDir(), yaml)
}

// startLauncherIn is startLauncher in dir, which the test may have prepared
// (a .uproject, a Saved directory, an old log); the config is written there and
// the launcher runs with dir as its working directory, given args after
// -config.
func startLauncherIn(t *testing.T, dir, yaml string, args ...string) *launcherRun {
	t.Helper()
	return startLauncherCmd(t, dir, yaml, false, args)
}

// startLauncherPiped is startLauncherIn with a pipe as the launcher's stdin
// (l.stdin), so a test can type console commands. The agent script of such a
// test is a shell script, so sh must be on PATH too. The caller closes l.stdin.
func startLauncherPiped(t *testing.T, dir, yaml string, args ...string) *launcherRun {
	t.Helper()
	return startLauncherCmd(t, dir, yaml, true, args)
}

// startLauncherCmd is the one place a launcher test process is started; piped
// gives it a stdin pipe (l.stdin) instead of no stdin.
func startLauncherCmd(t *testing.T, dir, yaml string, piped bool, args []string) *launcherRun {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the configs use sleep/false and the tests send SIGTERM")
	}
	bins := []string{"sleep", "false"}
	if piped {
		bins = append(bins, "sh")
	}
	for _, bin := range bins {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	path := filepath.Join(dir, "unreagent.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	l := &launcherRun{lines: make(chan string, 256), stdout: &syncLog{}, done: make(chan struct{})}
	l.cmd = exec.Command(exe, "-test.run=^TestLauncherRunProcess$")
	l.cmd.Dir = dir
	l.cmd.Env = append(os.Environ(), runConfigEnv+"="+path, runArgsEnv+"="+strings.Join(args, "\n"))
	l.cmd.Stderr = &l.stderr
	if piped {
		if l.stdin, err = l.cmd.StdinPipe(); err != nil {
			t.Fatalf("stdin pipe: %v", err)
		}
	}
	out, err := l.cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := l.cmd.Start(); err != nil {
		t.Fatalf("start launcher: %v", err)
	}
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			l.stdout.log(sc.Text())
			l.lines <- sc.Text()
		}
		close(l.lines)
		l.err = l.cmd.Wait()
		close(l.done)
	}()
	t.Cleanup(func() {
		select {
		case <-l.done:
		default:
			_ = l.cmd.Process.Kill()
			<-l.done
		}
	})
	return l
}

// waitLine waits for a stdout line containing want.
func (l *launcherRun) waitLine(t *testing.T, want string) {
	t.Helper()
	l.waitLineWithin(t, want, 15*time.Second)
}

// waitLineWithin is waitLine with its own time limit, for a line that comes
// only after slow runs.
func (l *launcherRun) waitLineWithin(t *testing.T, want string, within time.Duration) {
	t.Helper()
	timeout := time.After(within)
	for {
		select {
		case line, ok := <-l.lines:
			if !ok {
				t.Fatalf("launcher ended before logging %q:\n%s\nstderr: %s", want, l.stdout, l.stderr.String())
			}
			if strings.Contains(line, want) {
				return
			}
		case <-timeout:
			t.Fatalf("launcher did not log %q within %s:\n%s", want, within, l.stdout)
		}
	}
}

// exitCode waits for the launcher to end and returns its exit code.
func (l *launcherRun) exitCode(t *testing.T) int {
	t.Helper()
	go func() {
		for range l.lines { // keep draining until EOF
		}
	}()
	select {
	case <-l.done:
	case <-time.After(20 * time.Second):
		t.Fatalf("launcher still running after 20s — the session never ended:\n%s", l.stdout)
	}
	var ee *exec.ExitError
	if errors.As(l.err, &ee) {
		return ee.ExitCode()
	}
	if l.err != nil {
		t.Fatalf("launcher: %v", l.err)
	}
	return 0
}

// launcherYAML is a launcher config with the given agent section; the editor is
// a sleep, MCP and everything that needs it is off.
func launcherYAML(editor, agent string) string {
	return `unreal:
  editor: ` + editor + `
  args: ["20"]
  unattended: false
mcp:
  enabled: false
permissions:
  enabled: false
runtimes:
  python: { enabled: false }
  node: { enabled: false }
agent:
` + agent
}

// k69/k70: a headless agent that fails at every start (claude not logged in,
// a bad flag) used to be restarted forever; with the fast-fail give-up the
// session ends — and it must end as a failure, so a script or CI job running
// the launcher sees exit 1, not 0.
func TestRunHeadlessAgentFailureExitsNonZero(t *testing.T) {
	l := startLauncher(t, launcherYAML("sleep", `  enabled: true
  command: "false"
  window: false
  claudeIntegration: false
  startDelaySeconds: 1
  restartDelaySeconds: 1
`))
	if code := l.exitCode(t); code != 1 {
		t.Fatalf("exit code %d, want 1 for a headless agent that failed:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	if !strings.Contains(l.stderr.String(), "agent") {
		t.Fatalf("stderr does not name the agent: %q", l.stderr.String())
	}
	if strings.Contains(l.stdout.String(), "Signal received") {
		t.Fatalf("a session the launcher ended is logged as a signal:\n%s", l.stdout)
	}
}

// The first reason to end the session wins. Ctrl-C reaches the agent too; when
// it dies of it a moment later, its exit handling ends the session once more —
// that must not turn the signal's exit 0 into the agent's failure.
func TestSessionSignalWinsOverALaterEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sess := newSession(ctx, cancel)
	cancel() // the signal
	sess.agentExited(false)
	sess.end()
	if err := sess.result(); err != nil {
		t.Fatalf("result = %v after a signal, want nil", err)
	}
	if sess.endedByLauncher() {
		t.Fatal("a session ended by a signal is reported as ended by the launcher")
	}
}

// Without an agent only an editor that never got going ends the session, and
// only with MCP off; one that ran and then ended is left to its restart
// policy, as before. With MCP on the give-up is logged, the session goes on.
func TestEditorExitHandlerEndsOnlyOnAFailedStartWithMCPOff(t *testing.T) {
	cases := []struct {
		exit       supervisor.Exit
		mcpServing bool
		wantErr    bool
		wantLog    string
	}{
		{supervisor.Exit{}, false, false, "The editor failed and is not restarted."},
		{supervisor.Exit{Success: true}, false, false, ""},
		{supervisor.Exit{StartFailed: true}, false, true, "stopping everything"},
		{supervisor.Exit{}, true, false, "The editor failed and is not restarted."},
		{supervisor.Exit{StartFailed: true}, true, false, "keeps serving MCP"},
	}
	for _, c := range cases {
		ctx, cancel := context.WithCancel(context.Background())
		sess := newSession(ctx, cancel)
		var log syncLog
		var serving atomic.Bool
		serving.Store(c.mcpServing)
		makeEditorExitHandler(sess, log.log, false, &serving, new(editorGiveUp), config.UnrealConfig{}, "", nil)(c.exit)
		if ended := ctx.Err() != nil; ended != c.wantErr {
			t.Fatalf("%+v, mcp %v: session ended = %v, want %v", c.exit, c.mcpServing, ended, c.wantErr)
		}
		if err := sess.result(); (err != nil) != c.wantErr {
			t.Fatalf("%+v, mcp %v: result = %v, want error: %v", c.exit, c.mcpServing, err, c.wantErr)
		}
		if got := log.String(); (c.wantLog == "") != (got == "") || !strings.Contains(got, c.wantLog) {
			t.Fatalf("%+v, mcp %v: logged %q, want a line with %q", c.exit, c.mcpServing, got, c.wantLog)
		}
		cancel()
	}
}

// k128: the give-up line said "read the logs" and nothing about the crash
// dumps the failed runs left. It names Saved/Crashes, and with cleanOnRestart
// what empties it: a retry keeps the dumps, a new launcher run does not. The
// hint comes on every failure the editor is not restarted after — also with
// the launcher's own agent, where there is no k69 give-up line to carry it,
// and after a run that failed late — and only the k69 case ends the session.
func TestEditorGiveUpLineSaysWhereTheDumpsAre(t *testing.T) {
	dir := t.TempDir()
	crashes := filepath.Join(dir, "Saved", "Crashes")
	// k253: the line points there only when a dump is there.
	if err := os.MkdirAll(filepath.Join(crashes, "UECC-Windows-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir() // a project whose editor never got going: no Saved/Crashes
	quick, late := supervisor.Exit{StartFailed: true}, supervisor.Exit{}
	const failedLine = "The editor failed and is not restarted."
	for _, c := range []struct {
		agent      bool
		exit       supervisor.Exit
		mcpServing bool
		clean      bool
		projectDir string
		ends       bool
		want       string // the end of the logged line; "" for no dump hint
	}{
		{false, quick, true, false, dir, false, "(Ctrl-C stops the launcher). Crash dumps are in " + crashes + "."},
		{false, quick, true, true, dir, false, " Crash dumps are in " + crashes + " — a retry keeps them, a new launcher run empties them (unreal.cleanOnRestart)."},
		{false, quick, false, false, dir, true, "stopping everything. Crash dumps are in " + crashes + "."},
		{false, quick, false, true, dir, true, " Crash dumps are in " + crashes + " — the next launcher run empties them (unreal.cleanOnRestart)."},
		{false, late, false, true, dir, false, failedLine + " Crash dumps are in " + crashes + " — a retry keeps them, a new launcher run empties them (unreal.cleanOnRestart)."},
		{true, quick, false, true, dir, false, failedLine + " Crash dumps are in " + crashes + " — a retry keeps them, a new launcher run empties them (unreal.cleanOnRestart)."},
		{true, quick, true, false, dir, false, failedLine + " Crash dumps are in " + crashes + "."},
		{true, late, false, false, dir, false, failedLine + " Crash dumps are in " + crashes + "."},
		{true, supervisor.Exit{Success: true}, false, true, dir, false, ""},
		{false, quick, true, true, empty, false, ""},
		{true, late, false, true, empty, false, failedLine},
		{false, quick, true, true, "", false, ""},
		{false, quick, false, true, "", true, ""},
		{true, quick, false, true, "", false, ""},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		var log syncLog
		var serving atomic.Bool
		serving.Store(c.mcpServing)
		makeEditorExitHandler(newSession(ctx, cancel), log.log, c.agent, &serving, new(editorGiveUp), config.UnrealConfig{CleanOnRestart: c.clean}, c.projectDir, nil)(c.exit)
		ended := ctx.Err() != nil
		cancel()
		got := strings.TrimSpace(log.String())
		if ended != c.ends {
			t.Errorf("agent %v, %+v, mcp %v: session ended = %v, want %v", c.agent, c.exit, c.mcpServing, ended, c.ends)
		}
		if c.want == "" {
			if strings.Contains(got, "rash dumps") {
				t.Errorf("agent %v, %+v, project %q: logged %q, want no dump hint", c.agent, c.exit, c.projectDir, got)
			}
			continue
		}
		if !strings.HasSuffix(got, c.want) {
			t.Errorf("agent %v, %+v, mcp %v, clean %v: logged %q, want it to end with %q", c.agent, c.exit, c.mcpServing, c.clean, got, c.want)
		}
	}
}

// A signal-driven shutdown is the user's (or the service manager's) decision
// and stays exit 0 — also after the agent failed and onExit=leave kept the
// launcher running, which is when the agent's outcome is already recorded.
func TestRunSignalShutdownExitsZero(t *testing.T) {
	cases := []struct {
		name, agent, wait string
	}{
		{"agent running", `  enabled: true
  command: sleep
  args: ["20"]
  window: false
  claudeIntegration: false
  startDelaySeconds: 1
`, "[agent] started (pid"},
		{"agent failed, onExit=leave", `  enabled: true
  command: "false"
  window: false
  claudeIntegration: false
  startDelaySeconds: 1
  restart: never
  onExit: leave
`, "agent.onExit=leave"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := startLauncher(t, launcherYAML("sleep", c.agent))
			l.waitLine(t, c.wait)
			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			if !strings.Contains(l.stdout.String(), "Signal received") {
				t.Fatalf("shutdown not reported as a signal:\n%s", l.stdout)
			}
		})
	}
}

// k69: without an agent and with MCP off there is nothing to fall back on — an
// editor that cannot start left the launcher idling forever with exit 0 at the
// next Ctrl-C. (launcherYAML has MCP off.)
func TestRunEditorStartFailureWithoutAgentExitsNonZero(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-editor")
	l := startLauncher(t, launcherYAML(missing, "  enabled: false\n"))
	if code := l.exitCode(t); code != 1 {
		t.Fatalf("exit code %d, want 1 for an editor that cannot start and no agent:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	if !strings.Contains(l.stderr.String(), "editor") {
		t.Fatalf("stderr does not name the editor: %q", l.stderr.String())
	}
}

// Design choice for k69: with MCP on (-no-agent, manualStart, an external
// agent) the editor's give-up must not take the MCP server down — an external
// agent can still read the logs and retry ue_start. The launcher says so,
// keeps answering MCP, and a signal still ends it with exit 0.
func TestRunEditorStartFailureWithMCPKeepsServing(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick a free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	missing := filepath.Join(t.TempDir(), "no-such-editor")
	yaml := strings.Replace(launcherYAML(missing, "  enabled: false\n"),
		"mcp:\n  enabled: false\n", "mcp:\n  enabled: true\n  address: "+addr+"\n", 1)
	l := startLauncher(t, yaml)
	l.waitLine(t, "keeps serving MCP")

	resp, err := http.Post("http://"+addr+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("MCP server gone after the editor gave up: %v\n%s", err, l.stdout)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list after the editor gave up: HTTP %d", resp.StatusCode)
	}

	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0 — the give-up must not decide the exit code here:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	if !strings.Contains(l.stdout.String(), "Signal received") {
		t.Fatalf("shutdown not reported as a signal:\n%s", l.stdout)
	}
}

// mcpBreakEnv makes TestLauncherRunProcess serve MCP as usual until the file
// it names exists; the next connection then breaks the port (breaksOnFile).
const mcpBreakEnv = "UNREAGENT_TEST_MCP_BREAK_FILE"

// breaksOnFile is a bound listener that a test breaks when it chooses to:
// once the file at path exists, Accept closes the next connection and fails
// with an error that is not temporary, so the MCP server stops serving.
type breaksOnFile struct {
	net.Listener
	path string
}

func (l breaksOnFile) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(l.path); statErr == nil {
		_ = c.Close()
		return nil, errors.New("accept: injected failure")
	}
	return c, nil
}

// breakMCP stops the MCP server of a launcher that runs with mcpBreakEnv set
// to trigger, and waits for its ERROR line.
func breakMCP(t *testing.T, l *launcherRun, addr, trigger string) {
	t.Helper()
	if err := os.WriteFile(trigger, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("connect to the MCP port to break it: %v", err)
	}
	_ = c.Close()
	l.waitLine(t, "ERROR MCP server on "+addr+" stopped serving")
}

// noAgentMCPYAML is a launcherYAML config without an agent, with the editor
// at editor, no restart for it, and MCP on at addr.
func noAgentMCPYAML(editor, addr string) string {
	return withMCP(strings.Replace(launcherYAML(editor, "  enabled: false\n"),
		"  unattended: false\n", "  unattended: false\n  restart: never\n", 1), addr)
}

// The k69 give-up line with MCP not serving, and the k155 line for MCP
// stopping after the give-up; each names both reasons.
const (
	giveUpNotServing   = "no agent is configured and MCP is not serving — stopping everything"
	stoppedAfterGiveUp = "MCP stopped serving, and the editor has not been started again since it failed to start"
)

// k155: without an agent, an editor give-up while MCP served left the
// launcher waiting for a retry through MCP (k69). When MCP then stopped
// serving, nothing checked again: the launcher idled with no editor, no agent
// and no MCP until somebody pressed Ctrl-C. The serve failure is the second
// event now, and it ends the session as the k69 rule does with MCP off: exit
// 1, with a line naming both reasons.
func TestRunEditorGiveUpThenServeFailureExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	trigger := filepath.Join(dir, "break-mcp")
	t.Setenv(mcpBreakEnv, trigger)
	addr := freeLoopbackAddr(t)
	l := startLauncherIn(t, dir, noAgentMCPYAML(filepath.Join(dir, "no-such-editor"), addr))
	l.waitLine(t, "keeps serving MCP")

	breakMCP(t, l, addr, trigger)
	if code := l.exitCode(t); code != 1 {
		t.Fatalf("exit code %d, want 1 once MCP stopped serving after the editor gave up:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	out := l.stdout.String()
	if !strings.Contains(out, stoppedAfterGiveUp) || !strings.Contains(out, giveUpNotServing) {
		t.Errorf("no line names both reasons (%q, %q):\n%s", stoppedAfterGiveUp, giveUpNotServing, out)
	}
	if strings.Contains(out, "Signal received") {
		t.Errorf("a session the launcher ended is logged as a signal:\n%s", out)
	}
	if !strings.Contains(l.stderr.String(), "editor") {
		t.Errorf("stderr does not name the editor: %q", l.stderr.String())
	}
}

// k155, the other order: MCP stops serving while the editor runs, which alone
// ends nothing (k45, k123), and the editor gives up later. The give-up reads
// MCP's state at the editor's exit, so it ends the session as with MCP off.
func TestRunServeFailureThenEditorGiveUpExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	trigger := filepath.Join(dir, "break-mcp")
	t.Setenv(mcpBreakEnv, trigger)
	addr := freeLoopbackAddr(t)
	// Fails once the test says so: within the fast-fail window of its start.
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nwhile [ ! -e \"$0.fail\" ]; do sleep 0.05; done\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := startLauncherIn(t, dir, noAgentMCPYAML(editor, addr))
	l.waitLine(t, "[ue] started (pid")

	breakMCP(t, l, addr, trigger)
	if err := os.WriteFile(editor+".fail", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := l.exitCode(t); code != 1 {
		t.Fatalf("exit code %d, want 1 when the editor gave up after MCP stopped serving:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	out := l.stdout.String()
	if !strings.Contains(out, "The editor "+editorFailed+", and is not restarted; "+giveUpNotServing) {
		t.Errorf("no give-up line for MCP not serving:\n%s", out)
	}
	if strings.Contains(out, stoppedAfterGiveUp) {
		t.Errorf("the serve failure ended the session while the editor ran:\n%s", out)
	}
}

// k155: a start of the editor after its give-up takes the give-up back. MCP
// stopping after that leaves a running editor, and the session goes on as for
// any serve failure (k45, k123); a signal still ends it with exit 0.
func TestRunServeFailureAfterTheEditorStartedAgainKeepsRunning(t *testing.T) {
	dir := t.TempDir()
	trigger := filepath.Join(dir, "break-mcp")
	t.Setenv(mcpBreakEnv, trigger)
	addr := freeLoopbackAddr(t)
	// The first run fails at once, the second keeps running.
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nif [ ! -e \"$0.ran\" ]; then : > \"$0.ran\"; exit 1; fi\nexec sleep 20\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := startLauncherIn(t, dir, noAgentMCPYAML(editor, addr))
	l.waitLine(t, "keeps serving MCP")

	resp, err := http.Post("http://"+addr+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ue_start","arguments":{}}}`))
	if err != nil {
		t.Fatalf("ue_start: %v\n%s", err, l.stdout)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.Contains(string(body), `"isError":true`) {
		t.Fatalf("ue_start: HTTP %d %s\n%s", resp.StatusCode, body, l.stdout)
	}

	breakMCP(t, l, addr, trigger)
	select {
	case <-l.done:
		t.Fatalf("the launcher ended after the serve failure, with the editor running:\n%s", l.stdout)
	case <-time.After(time.Second):
	}
	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
}

// crashingEditor is an editor stand-in for the cleanOnRestart test. Its first
// run records which recovery artefacts it finds in Saved, leaves a crash dump
// and a restore file behind as a crashing editor does, and fails; the second
// run — the restart policy's — records what the restart left of them and keeps
// running. $1 is the .uproject the launcher passes first.
const crashingEditor = `#!/bin/sh
d=$(dirname "$1")
found() {
	for p in Saved/Crashes/old Saved/Crashes/dump Saved/Autosaves/PackageRestoreData.json; do
		if [ -e "$d/$p" ]; then echo "$p"; fi
	done
}
if [ ! -e "$d/run1" ]; then
	found > "$d/run1"
	mkdir -p "$d/Saved/Crashes/dump" "$d/Saved/Autosaves"
	: > "$d/Saved/Autosaves/PackageRestoreData.json"
	exit 1
fi
found > "$d/run2.tmp" && mv "$d/run2.tmp" "$d/run2"
exec sleep 20
`

// k77: cleanOnRestart emptied Saved/Crashes before every start, also on the
// automatic restart right after a crash — the dump the agent needs to find out
// why went with it. The first start still cleans both; the automatic restart
// removes the restore file (its dialog would block the unattended restart)
// and keeps the crash dump.
func TestRunCleanOnRestartKeepsCrashesOnAnAutomaticRestart(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	dir := t.TempDir()
	for _, d := range []string{"Saved/Crashes/old", "Saved/Autosaves"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"Demo.uproject": "{}",
		"Saved/Autosaves/PackageRestoreData.json": "{}",
		"editor.sh": crashingEditor,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yaml := strings.Replace(launcherYAML(filepath.Join(dir, "editor.sh"), "  enabled: false\n"),
		"  unattended: false\n",
		"  unattended: false\n  cleanOnRestart: true\n  restart: on-failure\n  restartDelaySeconds: 1\n", 1)
	l := startLauncherIn(t, dir, yaml)

	var run2 []byte
	deadline := time.Now().Add(15 * time.Second)
	for {
		var err error
		if run2, err = os.ReadFile(filepath.Join(dir, "run2")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the editor was not restarted after its crash:\n%s\nstderr: %s", l.stdout, l.stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	run1, err := os.ReadFile(filepath.Join(dir, "run1"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(run1)); got != "" {
		t.Errorf("the first start left %q in Saved, want both the old crash and the restore file cleaned", got)
	}
	if got := strings.TrimSpace(string(run2)); got != "Saved/Crashes/dump" {
		t.Errorf("the automatic restart left %q in Saved, want only the crash dump (Saved/Crashes/dump)", got)
	}
	l.waitLine(t, "Saved/Crashes kept")

	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
}

// k153: a ue_start during the restart delay after a crash takes the place of
// the restart policy's restart, but with cleanOnRestart it emptied
// Saved/Crashes as any manual start does — the dump of the crash that just
// happened went before anybody read it. That start keeps it, as the
// automatic restart it replaces would (k77).
func TestRunCleanOnRestartKeepsCrashesOnAStartDuringTheRestartDelay(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick a free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"Demo.uproject": "{}",
		"editor.sh":     crashingEditor,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The restart delay outlasts the test: only ue_start can start run 2.
	yaml := strings.Replace(launcherYAML(filepath.Join(dir, "editor.sh"), "  enabled: false\n"),
		"  unattended: false\n",
		"  unattended: false\n  cleanOnRestart: true\n  restart: on-failure\n  restartDelaySeconds: 600\n", 1)
	yaml = strings.Replace(yaml, "mcp:\n  enabled: false\n", "mcp:\n  enabled: true\n  address: "+addr+"\n", 1)
	l := startLauncherIn(t, dir, yaml)
	l.waitLine(t, "[ue] restart 1 in ")

	resp, err := http.Post("http://"+addr+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ue_start","arguments":{}}}`))
	if err != nil {
		t.Fatalf("ue_start: %v\n%s", err, l.stdout)
	}
	_ = resp.Body.Close()
	var run2 []byte
	deadline := time.Now().Add(15 * time.Second)
	for {
		if run2, err = os.ReadFile(filepath.Join(dir, "run2")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the editor did not start again after ue_start:\n%s\nstderr: %s", l.stdout, l.stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := strings.TrimSpace(string(run2)); got != "Saved/Crashes/dump" {
		t.Errorf("ue_start during the restart delay left %q in Saved, want only the crash dump (Saved/Crashes/dump)", got)
	}
	l.waitLine(t, "Saved/Crashes kept (start in place of the pending automatic restart")

	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
}

// crashLoopEditor is an editor stand-in for the give-up test. Its first three
// runs each leave a crash dump in Saved/Crashes and fail at once, which the
// fast-fail rule gives up on; the fourth run records what Saved/Crashes holds
// when it starts and keeps running. $1 is the .uproject the launcher passes.
const crashLoopEditor = `#!/bin/sh
d=$(dirname "$1")
n=$(ls "$d" | grep -c '^run\.')
n=$((n+1))
: > "$d/run.$n"
if [ "$n" -le 3 ]; then
	mkdir -p "$d/Saved/Crashes/dump$n"
	exit 1
fi
ls "$d/Saved/Crashes" > "$d/seen.tmp" 2>/dev/null
mv "$d/seen.tmp" "$d/seen"
exec sleep 20
`

// k128: after the k70 give-up the launcher advises "retry with ue_start", and
// with cleanOnRestart that manual start emptied Saved/Crashes — the dumps of
// the crash loop went before anybody read them. The first start after a
// give-up keeps them, as the automatic restart does (k77).
func TestRunCleanOnRestartKeepsCrashesOnTheFirstStartAfterAGiveUp(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick a free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"Demo.uproject": "{}",
		"editor.sh":     crashLoopEditor,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yaml := strings.Replace(launcherYAML(filepath.Join(dir, "editor.sh"), "  enabled: false\n"),
		"  unattended: false\n",
		"  unattended: false\n  cleanOnRestart: true\n  restartDelaySeconds: 1\n", 1)
	yaml = strings.Replace(yaml, "mcp:\n  enabled: false\n", "mcp:\n  enabled: true\n  address: "+addr+"\n", 1)
	l := startLauncherIn(t, dir, yaml)
	l.waitLine(t, "keeps serving MCP")

	resp, err := http.Post("http://"+addr+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ue_start","arguments":{}}}`))
	if err != nil {
		t.Fatalf("ue_start: %v\n%s", err, l.stdout)
	}
	_ = resp.Body.Close()
	var seen []byte
	deadline := time.Now().Add(15 * time.Second)
	for {
		if seen, err = os.ReadFile(filepath.Join(dir, "seen")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the editor did not start again after ue_start:\n%s\nstderr: %s", l.stdout, l.stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got, want := strings.Fields(string(seen)), []string{"dump1", "dump2", "dump3"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the first start after the give-up found %q in Saved/Crashes, want the crash loop's dumps %q", got, want)
	}

	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
}

// k128: the dump hint came only with the k69 give-up line, which a launcher
// with its own agent — the default — never logs: there the editor's give-up
// was the supervisor's line alone. Every editor give-up names the folder.
func TestRunEditorGiveUpWithAnAgentSaysWhereTheDumpsAre(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	dir := t.TempDir()
	for name, content := range map[string]string{
		"Demo.uproject": "{}",
		// The run leaves a dump behind (k253: the line needs one).
		"editor.sh": "#!/bin/sh\nmkdir -p '" + filepath.Join(dir, "Saved", "Crashes", "UECC-1") + "'\nexit 1\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yaml := strings.Replace(launcherYAML(filepath.Join(dir, "editor.sh"), `  enabled: true
  command: sleep
  args: ["20"]
  window: false
  claudeIntegration: false
`), "  unattended: false\n", "  unattended: false\n  restartDelaySeconds: 1\n", 1)
	l := startLauncherIn(t, dir, yaml)
	l.waitLine(t, "Crash dumps are in "+filepath.Join(dir, "Saved", "Crashes")+".")

	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0 (an editor give-up does not end a session with an agent):\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
}

// sleepProcessEnv selects TestSleepProcess.
const sleepProcessEnv = "UNREAGENT_TEST_SLEEP"

// TestSleepProcess is not a test: it is a process that sleeps, for the
// crash-reporter kill test, selected via sleepProcessEnv.
func TestSleepProcess(t *testing.T) {
	if os.Getenv(sleepProcessEnv) == "" {
		return
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// k154: the pre-start ran taskkill /F /IM CrashReportClientEditor.exe (pkill
// off Windows), and the crash reporter is no leftover but each editor's
// monitor for its whole life: the kill ended the monitor of every other
// editor on the machine (run 52). Every reporter of ours is in our editor's
// job and ends with it, so no pre-start kills one, whatever the start and
// whatever unreal.killCrashReporter says. The reporters are the test binary
// under their names, children of this test; on Windows the test cannot
// stand in for an image name, so it runs off Windows only.
func TestEditorPreStartKillsNoCrashReporter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in reporters are symlinks to the test binary")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dir := t.TempDir()
	type proc struct {
		name string
		cmd  *exec.Cmd
		done chan struct{}
	}
	var reporters []*proc
	for _, name := range []string{"CrashReportClient", "CrashReportClientEditor"} {
		link := filepath.Join(dir, name)
		if err := os.Symlink(exe, link); err != nil {
			t.Skipf("symlink: %v", err)
		}
		p := &proc{name: name, done: make(chan struct{})}
		p.cmd = exec.Command(link, "-test.run=^TestSleepProcess$", "--", "-MONITOR=4711")
		p.cmd.Env = append(os.Environ(), sleepProcessEnv+"=1")
		if err := p.cmd.Start(); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		go func() { _ = p.cmd.Wait(); close(p.done) }()
		t.Cleanup(func() { _ = p.cmd.Process.Kill(); <-p.done })
		reporters = append(reporters, p)
	}
	time.Sleep(200 * time.Millisecond) // let them reach their sleep

	on := true
	pre := editorPreStart(config.UnrealConfig{KillCrashReporter: &on}, "", func(string) {})
	for _, st := range []supervisor.Start{{}, {Automatic: true}, {AfterGiveUp: true}, {ReplacesRestart: true}} {
		pre(st)
	}
	time.Sleep(200 * time.Millisecond)
	for _, p := range reporters {
		select {
		case <-p.done:
			t.Errorf("the editor pre-start ended %s (%v)", p.name, p.cmd.ProcessState)
		default:
		}
	}
}

// k154: unreal.killCrashReporter lost its effect with the kill. A config that
// sets it still loads, and the launcher says the key does nothing; one
// without it hears nothing about it.
func TestRunWarnsThatKillCrashReporterHasNoEffect(t *testing.T) {
	const warn = "unreal.killCrashReporter has no effect"
	with := strings.Replace(launcherYAML("sleep", "  enabled: false\n"), "  unattended: false\n", "  unattended: false\n  killCrashReporter: false\n", 1)
	l := startLauncher(t, with)
	l.waitLine(t, "WARN "+warn)

	l = startLauncher(t, launcherYAML("sleep", "  enabled: false\n"))
	l.waitLine(t, "[ue] started")
	if strings.Contains(l.stdout.String(), warn) {
		t.Fatalf("warned about unreal.killCrashReporter though the config does not set it:\n%s", l.stdout)
	}
}

// logContent returns the content of the log at path, "<missing>" when there is none.
func logContent(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "<missing>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// k80: unreagent.log was truncated at every start, so after a crash and a
// relaunch the log of the session that crashed was gone. The previous
// session's log is kept as unreagent.log.1 — one generation: the one before
// it is dropped.
func TestLauncherLogKeepsThePreviousSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unreagent.log")
	var warn bytes.Buffer
	session := func(text string) {
		t.Helper()
		f := openLauncherLog(path, &warn)
		if f == nil {
			t.Fatalf("openLauncherLog failed: %s", warn.String())
		}
		if _, err := f.WriteString(text); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	session("session 1")
	if got := logContent(t, path+".1"); got != "<missing>" {
		t.Fatalf("the first session ever made unreagent.log.1 = %q, want none", got)
	}
	session("session 2")
	if got, want := logContent(t, path+".1"), "session 1"; got != want {
		t.Fatalf("unreagent.log.1 = %q, want the previous session %q", got, want)
	}
	if got, want := logContent(t, path), "session 2"; got != want {
		t.Fatalf("unreagent.log = %q, want only this session %q", got, want)
	}
	session("session 3")
	if got, want := logContent(t, path+".1"), "session 2"; got != want {
		t.Fatalf("unreagent.log.1 = %q, want the previous session %q (one generation)", got, want)
	}
	if warn.Len() > 0 {
		t.Fatalf("warned without a reason: %s", warn.String())
	}
}

// k80: a log that could not be created was given up on silently, and the
// launcher logs went to the console the agent was about to take over. The
// user has to be told, and why.
func TestLauncherLogCreateFailureWarns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "unreagent.log")
	var warn bytes.Buffer
	if f := openLauncherLog(path, &warn); f != nil {
		_ = f.Close()
		t.Fatal("openLauncherLog returned a file in a directory that does not exist")
	}
	// The cause as the OS words it, e.g. "no such file or directory".
	var cause *fs.PathError
	if _, err := os.Create(path); !errors.As(err, &cause) {
		t.Fatalf("creating %s in a missing directory: %v, want a *fs.PathError", path, err)
	}
	got := warn.String()
	for _, want := range []string{"WARN", path, cause.Err.Error()} {
		if !strings.Contains(got, want) {
			t.Fatalf("warning %q does not contain %q", got, want)
		}
	}
}

// When the previous log cannot be moved aside, that is said too, and the new
// session still gets its log — appended to the previous one, which a
// truncating create would wipe right after the warning. A directory in the way
// of unreagent.log.1 makes the rename fail (on Windows a second launcher
// holding the file does too).
func TestLauncherLogAppendsWhenThePreviousCannotBeKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unreagent.log")
	if err := os.WriteFile(path, []byte("session 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path+".1", "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	f := openLauncherLog(path, &warn)
	if f == nil {
		t.Fatalf("no log for the new session: %s", warn.String())
	}
	if _, err := f.WriteString("session 2"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := logContent(t, path), "session 1session 2"; got != want {
		t.Fatalf("unreagent.log = %q, want the previous session followed by this one, %q", got, want)
	}
	got := warn.String()
	for _, want := range []string{"WARN", path + ".1", "append"} {
		if !strings.Contains(got, want) {
			t.Fatalf("warning %q does not contain %q", got, want)
		}
	}
}

// k80: the warning has to reach the user's terminal from run(), on stderr,
// before the agent takes the console — not the launcher log, which is the
// thing that failed. The log goes next to the .uproject, whose directory is
// read-only here.
func TestRunWarnsWhenTheLauncherLogCannotBeCreated(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "Demo.uproject"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(proj, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(proj, 0o755) }) // so TempDir can remove it
	yaml := strings.Replace(launcherYAML(`"true"`, `  enabled: true
  command: sleep
  args: ["20"]
  window: true
  claudeIntegration: false
  startDelaySeconds: 1
`), "unreal:\n", "unreal:\n  project: proj/Demo.uproject\n", 1)
	l := startLauncherIn(t, dir, yaml)
	l.waitLine(t, "[agent] started (pid")
	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	if got, want := l.stderr.String(), filepath.Join(proj, "unreagent.log"); !strings.Contains(got, "WARN") || !strings.Contains(got, want) {
		t.Fatalf("stderr %q does not warn that %s could not be created", got, want)
	}
}

// clockStepEnv makes TestLauncherRunProcess step the clock of the launcher's
// log lines by a minute per line, so that no two lines carry the same time.
const clockStepEnv = "UNREAGENT_TEST_CLOCK_STEP"

// stampedLine is a launcher log line: its time, two spaces, the text.
var stampedLine = regexp.MustCompile(`^\d\d:\d\d:\d\d  `)

// k150: in window mode the lines logged before the switch to unreagent.log —
// config, engine and project, and their warnings, such as k88's fallback
// engine — went to the console alone, and a user reading the log after a
// window-mode start could not tell why that engine was chosen. They still go
// to the console, and they open the log, byte for byte: with the time each was
// logged at, not the switch's. The clock steps a minute per line, so a line
// stamped again on its way into the log would show.
func TestRunReplaysTheStartupLinesIntoTheLauncherLog(t *testing.T) {
	skipEpicDefaultFixtureOnWindows(t)
	t.Setenv("UE_ROOT", "")
	t.Setenv(clockStepEnv, "1")
	dir := t.TempDir()
	// As in TestRunWarnsWhenTheEngineAssociationIsNotInstalled: off Windows
	// the Epic default path resolves under the launcher's working directory.
	engine := "C:/Program Files/Epic Games/UE_5.9"
	mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(engine), "Engine", "Binaries", "Win64", "UnrealEditor.exe"), "")
	mustWriteFile(t, filepath.Join(dir, "Demo.uproject"), `{"EngineAssociation": "5.7"}`)
	l := startLauncherIn(t, dir, launcherYAML("sleep", `  enabled: true
  command: sleep
  args: ["20"]
  window: true
  claudeIntegration: false
  startDelaySeconds: 1
`))
	l.waitLine(t, "takes over this window") // printed right after the switch
	logPath := filepath.Join(dir, "unreagent.log")
	waitFileLine(t, logPath, "[agent] started (pid")
	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}

	// After the switch the console gets the banner and no log line.
	var early []string
	for _, line := range strings.Split(l.stdout.String(), "\n") {
		if stampedLine.MatchString(line) {
			early = append(early, line)
		}
	}
	if len(early) < 2 || early[0][:8] == early[1][:8] {
		t.Fatalf("want startup lines with a time each of their own on the console:\n%s", l.stdout)
	}
	if !slices.ContainsFunc(early, func(l string) bool { return strings.Contains(l, "WARN") && strings.Contains(l, "EngineAssociation") }) {
		t.Fatalf("no EngineAssociation WARN among the startup lines:\n%s", strings.Join(early, "\n"))
	}
	logged := logContent(t, logPath)
	head := strings.Join(early, "\n") + "\n"
	if !strings.HasPrefix(logged, head) {
		t.Fatalf("unreagent.log does not open with the startup lines as the console showed them\nconsole:\n%s\nlog:\n%s", head, logged)
	}
	if !strings.Contains(logged[len(head):], "[agent] started (pid") {
		t.Fatalf("the lines logged after the switch do not follow the startup lines:\n%s", logged)
	}
}

// k152: a failed agent start that the restart policy retried said nothing on
// the console in window mode — the start error sat in unreagent.log — so the
// user saw nothing until the policy gave up and the exit prompt named the
// error (k126). Each retried start now leaves one line on the console
// (stderr, as the other alerts), naming the error and the retry. The start
// the policy gives up on leaves none: the prompt names it, as before. That
// holds whether maxRestarts ends the retries or, without one, the fast-fail
// give-up (k70): one line per retry, the log's restart lines, and no more.
func TestRunNamesEachRetriedAgentStartOnTheConsole(t *testing.T) {
	for _, tc := range []struct {
		name        string
		maxRestarts int
	}{
		{"maxRestarts 2", 2},
		{"maxRestarts 0, the fast-fail give-up", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// Not executable: Start fails, and the policy retries it.
			agent := filepath.Join(dir, "agent")
			if err := os.WriteFile(agent, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			l := startLauncherIn(t, dir, launcherYAML("sleep", fmt.Sprintf(`  enabled: true
  command: "%s"
  window: true
  claudeIntegration: false
  startDelaySeconds: 1
  restartDelaySeconds: 1
  maxRestarts: %d
`, agent, tc.maxRestarts)))
			// stdin is empty: the exit prompt reads EOF and stops everything.
			if code := l.exitCode(t); code != 1 {
				t.Fatalf("exit code %d, want 1 for an agent that never started:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			logged := logContent(t, filepath.Join(dir, "unreagent.log"))
			const failed = "[agent] start failed: "
			var startErr string
			attempts := 0
			for _, line := range strings.Split(logged, "\n") {
				if _, e, ok := strings.Cut(line, failed); ok {
					startErr = e
					attempts++
				}
			}
			if attempts < 2 {
				t.Fatalf("%d failed start(s) in the log, want a retry at least:\n%s", attempts, logged)
			}
			if restarts := strings.Count(logged, "[agent] restart "); restarts != attempts-1 {
				t.Fatalf("the log has %d restart lines for %d failed starts, want one for each but the last:\n%s", restarts, attempts, logged)
			}
			if tc.maxRestarts > 0 && attempts != tc.maxRestarts+1 {
				t.Fatalf("%d failed starts, want the start and maxRestarts %d retries:\n%s", attempts, tc.maxRestarts, logged)
			}
			stderr := l.stderr.String()
			const notice = "unreagent: Agent did not start: "
			if got := strings.Count(stderr, notice); got != attempts-1 {
				t.Fatalf("stderr has %d retry lines for %d failed starts, want one per retry:\n%s", got, attempts, stderr)
			}
			limit := ""
			if tc.maxRestarts > 0 {
				limit = fmt.Sprintf("/%d", tc.maxRestarts)
			}
			for i := 1; i < attempts; i++ {
				want := fmt.Sprintf("%s%s — retry %d%s in 1s\n", notice, startErr, i, limit)
				if !strings.Contains(stderr, want) {
					t.Fatalf("stderr does not have %q:\n%s", want, stderr)
				}
			}
			if got := strings.Count(l.stdout.String(), "Agent did not start: "+startErr); got != 1 {
				t.Fatalf("the exit prompt named the failed start %d times, want once:\n%s", got, l.stdout)
			}
		})
	}
}

// consoleAlerts returns the lines run() wrote to the console's alert path
// (stderr, "unreagent: " in front), without that prefix, that start with one
// of prefixes.
func consoleAlerts(stderr string, prefixes ...string) []string {
	var out []string
	for _, line := range strings.Split(stderr, "\n") {
		rest, ok := strings.CutPrefix(line, "unreagent: ")
		if ok && slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(rest, p) }) {
			out = append(out, rest)
		}
	}
	return out
}

// logLinesWith returns the lines of a launcher log, without their time, that
// start with one of prefixes.
func logLinesWith(logged string, prefixes ...string) []string {
	var out []string
	for _, line := range strings.Split(logged, "\n") {
		line = stampedLine.ReplaceAllString(line, "")
		if slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(line, p) }) {
			out = append(out, line)
		}
	}
	return out
}

// k188: in window mode an agent that exited and was restarted by its policy
// left no launcher line on the console — the restart sat in unreagent.log, and
// the user saw the agent's screen go and come back. Each automatic restart
// after an exit now leaves one console line, naming the exit and the restart;
// the run that is not restarted leaves none (the exit prompt names it). The
// log keeps its lines. Without window mode the log is the console, and no
// line is written beside it.
func TestRunNamesEachAgentRestartOnTheConsole(t *testing.T) {
	for _, window := range []bool{true, false} {
		t.Run(fmt.Sprintf("window %v", window), func(t *testing.T) {
			dir := t.TempDir()
			l := startLauncherIn(t, dir, launcherYAML("sleep", fmt.Sprintf(`  enabled: true
  command: "false"
  window: %v
  claudeIntegration: false
  startDelaySeconds: 1
  restart: on-failure
  restartDelaySeconds: 1
  maxRestarts: 2
`, window)))
			// Window mode: stdin is empty, the exit prompt reads EOF and stops
			// everything. Headless: the agent's end ends the session.
			if code := l.exitCode(t); code != 1 {
				t.Fatalf("exit code %d, want 1 for an agent that failed:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			logged := l.stdout.String()
			if window {
				logged = logContent(t, filepath.Join(dir, "unreagent.log"))
			}
			if got := logLinesWith(logged, "[agent] restart "); len(got) != 2 {
				t.Fatalf("the log has %d restart lines, want 2 (maxRestarts):\n%s", len(got), logged)
			}
			got := consoleAlerts(l.stderr.String(), "Agent exited")
			if !window {
				if len(got) != 0 {
					t.Fatalf("with the log on the console, restart lines were written beside it:\n%s", l.stderr.String())
				}
				return
			}
			want := []string{
				"Agent exited (exit 1) — restart 1/2 in 1s",
				"Agent exited (exit 1) — restart 2/2 in 1s",
			}
			if !slices.Equal(got, want) {
				t.Fatalf("console restart lines = %q, want %q — one per restart, none for the last run:\n%s", got, want, l.stderr.String())
			}
		})
	}
}

// k188, k192: in window mode the editor's automatic lines — its exits, the
// restarts that follow, a retried failed start, the give-up line — and the
// startup WARN about unreal.editor reached unreagent.log only, while the user
// watched the agent's window. They go to the console as well (stderr,
// "unreagent: " in front), each as the log has it, word for word: the test
// compares the two, so a change of wording in either breaks it. Not on the
// console: the last failed start before the give-up (no hook carries it; the
// give-up line follows it), and the supervisor's own give-up lines (the
// give-up line names that end). Without window mode the log is the console,
// and nothing is written beside it.
func TestRunShowsTheEditorsAutomaticLinesOnTheConsole(t *testing.T) {
	const giveUp = "The editor failed and is not restarted."
	for _, tc := range []struct {
		name string
		// editor returns the editor's command, made in dir.
		editor func(t *testing.T, dir string) string
		// mirrored are the prefixes of the editor's log lines the console
		// shows; dropLast drops the last of them, which it does not.
		mirrored []string
		dropLast bool
		warn     string // a startup WARN the console shows, "" for none
	}{
		{
			name:     "exits and restarts",
			editor:   func(*testing.T, string) string { return "false" },
			mirrored: []string{"[ue] exited (", "[ue] restart "},
		},
		{
			name: "failed starts",
			editor: func(t *testing.T, dir string) string {
				// Not executable: Start fails, and the policy retries it.
				p := filepath.Join(dir, "editor")
				if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return p
			},
			mirrored: []string{"[ue] start failed: ", "[ue] restart "},
			dropLast: true,
			warn:     "WARN unreal.editor: path is not executable: ",
		},
	} {
		for _, window := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, window %v", tc.name, window), func(t *testing.T) {
				dir := t.TempDir()
				l := startLauncherIn(t, dir, fmt.Sprintf(`unreal:
  editor: "%s"
  unattended: false
  restart: on-failure
  restartDelaySeconds: 1
  maxRestarts: 1
mcp:
  enabled: false
permissions:
  enabled: false
runtimes:
  python: { enabled: false }
  node: { enabled: false }
agent:
  enabled: true
  command: sleep
  args: ["20"]
  window: %v
  claudeIntegration: false
`, tc.editor(t, dir), window))
				logPath := filepath.Join(dir, "unreagent.log")
				if window {
					waitFileLine(t, logPath, giveUp)
				} else {
					l.waitLine(t, giveUp)
				}
				if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatalf("SIGTERM: %v", err)
				}
				if code := l.exitCode(t); code != 0 {
					t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
				}
				stderr := l.stderr.String()
				prefixes := append([]string{giveUp, "WARN unreal.editor"}, tc.mirrored...)
				if !window {
					if got := consoleAlerts(stderr, prefixes...); len(got) != 0 {
						t.Fatalf("with the log on the console, %q were written beside it:\n%s", got, stderr)
					}
					return
				}
				logged := logContent(t, logPath)
				want := logLinesWith(logged, tc.mirrored...)
				if len(want) < 3 {
					t.Fatalf("the premise is a run, a restart and a second run in the log; it has %q:\n%s", want, logged)
				}
				if tc.dropLast {
					want = want[:len(want)-1]
				}
				if got := consoleAlerts(stderr, tc.mirrored...); !slices.Equal(got, want) {
					t.Fatalf("console lines = %q, want the log's %q:\n%s", got, want, stderr)
				}
				for _, p := range []string{giveUp, tc.warn} {
					if p == "" {
						continue
					}
					got, want := consoleAlerts(stderr, p), logLinesWith(logged, p)
					if len(want) != 1 || !slices.Equal(got, want) {
						t.Fatalf("console has %q, want the log's one line %q:\n%s", got, want, stderr)
					}
				}
			})
		}
	}
}

// k192: in window mode the startup WARN that agent.command is not on PATH
// went to unreagent.log only, while the user watched the window for the
// agent that never came. It goes to the console as well, as the log has it;
// without window mode the log is the console.
func TestRunShowsTheAgentPathWarningOnTheConsole(t *testing.T) {
	for _, window := range []bool{true, false} {
		t.Run(fmt.Sprintf("window %v", window), func(t *testing.T) {
			dir := t.TempDir()
			l := startLauncherIn(t, dir, launcherYAML("sleep", fmt.Sprintf(`  enabled: true
  command: no-such-agent-k192
  window: %v
  claudeIntegration: false
`, window)))
			if code := l.exitCode(t); code != 1 {
				t.Fatalf("exit code %d, want 1 for an agent that never started:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			const warn = "WARN agent command not in PATH: no-such-agent-k192"
			got := consoleAlerts(l.stderr.String(), warn)
			if !window {
				if len(got) != 0 {
					t.Fatalf("with the log on the console, the WARN was written beside it:\n%s", l.stderr.String())
				}
				return
			}
			want := logLinesWith(logContent(t, filepath.Join(dir, "unreagent.log")), warn)
			if len(want) != 1 || !slices.Equal(got, want) {
				t.Fatalf("console has %q, want the log's one line %q", got, want)
			}
		})
	}
}

// --- the exit prompt and the launcher console, on a fake console ---

// fakeConsole is a console the test types into line by line. io.Pipe hands a
// line over only when a reader takes it, so a returned type() means somebody
// read the line — and the test can see who did.
type fakeConsole struct {
	w   *io.PipeWriter
	con console
	out *syncBuffer
}

func newFakeConsole(t *testing.T) *fakeConsole {
	r, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	out := &syncBuffer{}
	return &fakeConsole{w: w, con: console{in: &lineReader{in: r}, out: out, logSeparate: true}, out: out}
}

// typeLine types one line; it fails the test when nobody reads it.
func (f *fakeConsole) typeLine(t *testing.T, line string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := io.WriteString(f.w, line+"\n")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("type %q: %v", line, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("nobody read %q from the console within 5s; console so far:\n%s", line, f.out)
	}
}

// waitOutput waits until the console shows want n times.
func (f *fakeConsole) waitOutput(t *testing.T, want string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for strings.Count(f.out.String(), want) < n {
		if time.Now().After(deadline) {
			t.Fatalf("console did not show %q %d time(s) within 10s:\n%s", want, n, f.out)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// agentSession wires an "agent" service running command to the interactive
// exit handler on a fake console, the way run() does, and starts it.
func agentSession(t *testing.T, command string, args ...string) (*session, *fakeConsole, *supervisor.Supervisor) {
	t.Helper()
	if _, err := exec.LookPath(command); err != nil {
		t.Skipf("%s not on PATH", command)
	}
	fc := newFakeConsole(t)
	sess, sup, _ := agentSessionOn(t, fc.con, command, args...)
	return sess, fc, sup
}

// agentSessionOn is agentSession on the given console; it also returns the
// launcher log.
func agentSessionOn(t *testing.T, con console, command string, args ...string) (*session, *supervisor.Supervisor, *syncLog) {
	t.Helper()
	return agentSessionWith(t, con, config.OnExitAsk, command, args...)
}

// agentSessionWith is agentSessionOn with agent.onExit set to onExit.
func agentSessionWith(t *testing.T, con console, onExit string, command string, args ...string) (*session, *supervisor.Supervisor, *syncLog) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	sess := newSession(ctx, cancel)
	logs := &syncLog{}
	sup := supervisor.New(logs.log)
	var handler func(bool)
	sup.AddService(supervisor.ServiceSpec{
		Name:      "agent",
		Command:   command,
		Args:      args,
		Autostart: true,
		Restart:   "never",
		OnExit:    func(e supervisor.Exit) { handler(e.Success) },
	})
	cfg := &config.Config{}
	cfg.Agent.OnExit = onExit
	handler = makeAgentExitHandler(ctx, sess, sup, cfg, logs.log, true, con)
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })
	return sess, sup, logs
}

func waitEnded(t *testing.T, sess *session, fc *fakeConsole) {
	t.Helper()
	select {
	case <-sess.ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatalf("session did not end; console:\n%s", fc.out)
	}
}

// k69: in window mode the user ends the session at the exit prompt; the exit
// code then reflects how the agent's last run ended — quitting after a crash
// is still a failed session, quitting after /quit is not. Only the last run
// counts: a crash the user restarted past is not held against the session.
func TestPromptQuitReflectsTheAgentOutcome(t *testing.T) {
	// crashOnce fails on its first run and exits 0 on every later one.
	crashOnce := []string{"sh", "-c", `if [ -e "$0" ]; then exit 0; fi; : > "$0"; exit 1`, filepath.Join(t.TempDir(), "ran")}
	cases := []struct {
		name    string
		agent   []string
		input   []string
		wantErr bool
	}{
		{"q after a crash", []string{"false"}, []string{"q"}, true},
		{"Enter after a crash", []string{"false"}, []string{""}, true},
		{"k, then q in the console after a crash", []string{"false"}, []string{"k", "q"}, true},
		{"Enter after exit 0", []string{"true"}, []string{""}, false},
		// The second line is read by the prompt for the restarted run's exit 0:
		// io.Pipe hands it over only when that prompt reads.
		{"crash, r, exit 0, Enter", crashOnce, []string{"r", ""}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sess, fc, _ := agentSession(t, c.agent[0], c.agent[1:]...)
			fc.waitOutput(t, "> ", 1)
			for _, line := range c.input {
				fc.typeLine(t, line)
			}
			waitEnded(t, sess, fc)
			if err := sess.result(); (err != nil) != c.wantErr {
				t.Fatalf("result = %v, want error: %v", err, c.wantErr)
			}
			if c.input[0] == "r" { // the premise: a crash first, then a run that exited 0
				out := fc.out.String()
				if i, j := strings.Index(out, "Agent crashed."), strings.Index(out, "Agent exited."); i < 0 || j < i {
					t.Fatalf("want a crash prompt, then an exit-0 prompt; console:\n%s", out)
				}
			}
		})
	}
}

// k71: the launcher console and a foreground agent must never read stdin at
// the same time. A command that starts the foreground agent hands the console
// over: the loop returns (true) without reading another line and without
// starting the agent itself — the caller does, once the loop is gone. Other
// commands keep the loop reading.
func TestCommandLoopHandsTheConsoleToTheForegroundAgent(t *testing.T) {
	for _, bin := range []string{"sleep"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	for _, cmd := range []string{"start agent", "r agent", "restart agent", "r"} {
		t.Run(cmd, func(t *testing.T) {
			logs := &syncLog{}
			sup := supervisor.New(logs.log)
			sup.AddService(supervisor.ServiceSpec{Name: "ue", Command: "sleep", Args: []string{"20"}, Autostart: true, Restart: "never"})
			sup.AddService(supervisor.ServiceSpec{Name: "agent", Command: "sleep", Args: []string{"20"}, Restart: "never"})
			ctx, cancel := context.WithCancel(context.Background())
			var wg sync.WaitGroup
			sup.Start(ctx, &wg)
			defer func() { cancel(); wg.Wait() }()
			uePID := waitRunning(t, sup, "ue")

			fc := newFakeConsole(t)
			ended := false
			result := make(chan bool, 1)
			go func() { result <- commandLoop(ctx, fc.con, func() { ended = true }, sup, logs.log, "agent") }()

			fc.typeLine(t, "status") // an ordinary command: the loop keeps reading
			fc.typeLine(t, cmd)
			select {
			case handover := <-result:
				if !handover || ended {
					t.Fatalf("loop returned handover=%v, ended=%v; want a handover", handover, ended)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("the loop kept reading the console after %q — a foreground agent would now share stdin with it", cmd)
			}
			if st := statusOf(t, sup, "agent"); st.Running || st.Desired {
				t.Fatalf("the loop started the agent itself (%+v); the caller must, after the loop is gone", st)
			}
			if cmd == "r" {
				if st := statusOf(t, sup, "ue"); !st.Running || st.PID == uePID {
					t.Fatalf("'r' did not restart the other services: ue %+v (pid before %d)", st, uePID)
				}
			}
		})
	}
}

// k71: after 'k' and 'start agent' the exit prompt must take over again when
// that agent ends. While the console flag stayed set, the handler only logged
// "Launcher console active" and nobody offered the prompt — the user was left
// typing into a loop that no longer existed or into two readers at once.
func TestPromptReturnsAfterTheConsoleStartsTheAgent(t *testing.T) {
	sess, fc, _ := agentSession(t, "false")
	fc.waitOutput(t, "Agent crashed.", 1)
	fc.typeLine(t, "k")
	fc.typeLine(t, "start agent")
	fc.waitOutput(t, "Agent crashed.", 2) // the restarted agent ended: prompt again
	fc.typeLine(t, "q")
	waitEnded(t, sess, fc)
}

func statusOf(t *testing.T, sup *supervisor.Supervisor, name string) supervisor.ServiceStatus {
	t.Helper()
	for _, st := range sup.Status() {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("no service %q", name)
	return supervisor.ServiceStatus{}
}

func waitRunning(t *testing.T, sup *supervisor.Supervisor, name string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st := statusOf(t, sup, name); st.Running {
			return st.PID
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not come up", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The engine matches a switch without regard to case and takes "/" as well as
// "-" in front of it (FParse::Param, UE 5.8.4 Parse.cpp), so unreal.args that
// already carry the switch in any of those spellings get no second one (k207).
// "-unattended=1" is not that switch to the engine, so the default still goes
// on.
func TestEditorArgsAddsUnattendedOnlyWhenTheEngineWouldMissIt(t *testing.T) {
	on, off := true, false
	cases := []struct {
		name       string
		args       []string
		unattended *bool
		want       []string
	}{
		{"absent", []string{"-stdout"}, &on, []string{"-stdout", "-unattended"}},
		{"exact", []string{"-unattended", "-stdout"}, &on, []string{"-unattended", "-stdout"}},
		{"other case", []string{"-Unattended"}, &on, []string{"-Unattended"}},
		{"upper case", []string{"-UNATTENDED"}, &on, []string{"-UNATTENDED"}},
		{"slash form", []string{"/Unattended"}, &on, []string{"/Unattended"}},
		{"with a value", []string{"-unattended=1"}, &on, []string{"-unattended=1", "-unattended"}},
		{"longer switch", []string{"-unattendedfoo"}, &on, []string{"-unattendedfoo", "-unattended"}},
		{"no prefix", []string{"unattended"}, &on, []string{"unattended", "-unattended"}},
		{"switched off", []string{"-stdout"}, &off, []string{"-stdout"}},
		{"unset", []string{"-stdout"}, nil, []string{"-stdout"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logged []string
			got := editorArgs(config.UnrealConfig{Args: tc.args, Unattended: tc.unattended},
				func(s string) { logged = append(logged, s) })
			if !slices.Equal(got, tc.want) {
				t.Errorf("editorArgs(%q) = %q, want %q", tc.args, got, tc.want)
			}
			if added := len(got) > len(tc.args); added != (len(logged) == 1) {
				t.Errorf("added=%v but logged %q", added, logged)
			}
		})
	}
}

// The project goes in front of unreal.args, and the caller's slice is left
// alone.
func TestEditorArgsPutsTheProjectFirst(t *testing.T) {
	on := true
	args := make([]string, 1, 4)
	args[0] = "-stdout"
	got := editorArgs(config.UnrealConfig{Project: "/p/Game.uproject", Args: args, Unattended: &on}, func(string) {})
	want := []string{"/p/Game.uproject", "-stdout", "-unattended"}
	if !slices.Equal(got, want) {
		t.Errorf("editorArgs = %q, want %q", got, want)
	}
	if len(args) != 1 || args[0] != "-stdout" {
		t.Errorf("unreal.args changed: %q", args)
	}
}

// k247: where the project's modules still look unbuilt when the editor is not
// restarted, the give-up line repeats the compile pointer — asked again at the
// give-up, so built modules (or an editor the check does not apply to) leave
// the line as it was. Text only: the session ends exactly as without it.
func TestEditorGiveUpRepeatsTheModulePointer(t *testing.T) {
	const editor = "C:/UE_5.8/Engine/Binaries/Win64/UnrealEditor.exe"
	dir := t.TempDir()
	project := filepath.Join(dir, "Game.uproject")
	mustWriteFile(t, project, `{"Modules": [{"Name": "Game"}]}`)
	routes := editorRoutes{console: true}
	pointer := func(ends bool) string { return giveUpModulesPointer(project, editor, nil, true, routes, ends) }
	for _, c := range []struct {
		name  string
		exit  supervisor.Exit
		agent bool
		serve bool
		ends  bool
	}{
		{"late failure", supervisor.Exit{}, false, false, false},
		{"failed start, MCP serving", supervisor.Exit{StartFailed: true}, false, true, false},
		{"failed start, ends", supervisor.Exit{StartFailed: true}, false, false, true},
		{"with an agent", supervisor.Exit{StartFailed: true}, true, false, false},
	} {
		run := func() (string, bool) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var log syncLog
			var serving atomic.Bool
			serving.Store(c.serve)
			makeEditorExitHandler(newSession(ctx, cancel), log.log, c.agent, &serving, new(editorGiveUp), config.UnrealConfig{}, dir, pointer)(c.exit)
			return log.String(), ctx.Err() != nil
		}
		got, ended := run()
		if ended != c.ends {
			t.Errorf("%s: session ended = %v, want %v", c.name, ended, c.ends)
		}
		// The claim for the ending give-up changed (k247 review): the launcher
		// is stopping, so no console is named — only the IDE and a new start.
		fix, not := "Build them with 'c compile' in the launcher console, then 'start ue'.", "start the launcher again"
		if c.ends {
			fix, not = "Build the project's editor target (Win64, Development) in your IDE, then start the launcher again.", "launcher console"
		}
		for _, want := range []string{"The project's modules still look unbuilt: Game", "which fits this failure. " + fix} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: logged %q, want it to mention %q", c.name, got, want)
			}
		}
		for _, bad := range []string{not, "an agent:", "'stop ue'", "gives up on it after a few tries"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s: logged %q, must not mention %q", c.name, got, bad)
			}
		}
		// Built since the startup check: no pointer, same ending.
		mustWriteFile(t, filepath.Join(dir, "Binaries", "Win64", "UnrealEditor-Game.dll"), "")
		got, ended = run()
		if strings.Contains(got, "look unbuilt") {
			t.Errorf("%s, built: logged %q, want no module pointer", c.name, got)
		}
		if ended != c.ends {
			t.Errorf("%s, built: session ended = %v, want %v", c.name, ended, c.ends)
		}
		os.Remove(filepath.Join(dir, "Binaries", "Win64", "UnrealEditor-Game.dll"))
	}
}

// k188: after k154 the editor's restart can be held for the last run's crash
// reporter, and the log then follows "[ue] restart N in D" with the hold. The
// console, which shows that restart line in window mode, shows the hold as
// well, word for word the log's (supervisor.HeldLine). This tests only the
// helper, alertHeldRestart: the hold needs a job to look into and cannot run
// in a Linux test, so the wiring (OnStartHeld: alertHeldRestart(alert, "ue")
// in main.go) is not covered by any test. A start somebody
// asked for gets no line: it is answered where it was asked. Without window
// mode (alert nil) nothing is written.
func TestHeldRestartIsShownAsTheLogHasIt(t *testing.T) {
	var out bytes.Buffer
	alertHeldRestart(&out, "ue")(supervisor.Start{Automatic: true})
	if got, want := out.String(), "unreagent: "+supervisor.HeldLine("ue")+"\n"; got != want {
		t.Fatalf("console = %q, want %q", got, want)
	}
	out.Reset()
	alertHeldRestart(&out, "ue")(supervisor.Start{ReplacesRestart: true})
	alertHeldRestart(&out, "ue")(supervisor.Start{})
	if got := out.String(); got != "" {
		t.Fatalf("a start somebody asked for was shown as held: %q", got)
	}
	alertHeldRestart(nil, "ue")(supervisor.Start{Automatic: true}) // must not panic
}
