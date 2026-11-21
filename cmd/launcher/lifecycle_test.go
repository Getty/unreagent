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
	"runtime"
	"strconv"
	"strings"
	"sync"
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
// run() tests, selected via runConfigEnv. It does what main() does, so the
// parent sees run()'s result as the exit code.
func TestLauncherRunProcess(t *testing.T) {
	path := os.Getenv(runConfigEnv)
	if path == "" {
		return
	}
	if os.Getenv(loadFailEnv) != "" {
		loadEmbeddedSkills = func() ([]skills.Skill, error) { return nil, errors.New("skills: injected load failure") }
	}
	os.Args = []string{os.Args[0], "-config", path}
	if extra := os.Getenv(runArgsEnv); extra != "" {
		os.Args = append(os.Args, strings.Split(extra, "\n")...)
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// launcherRun is one launcher process started by startLauncher.
type launcherRun struct {
	cmd    *exec.Cmd
	lines  chan string // stdout, line by line; closed at EOF
	stdout *syncLog
	stderr bytes.Buffer
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
	if runtime.GOOS == "windows" {
		t.Skip("the configs use sleep/false and the tests send SIGTERM")
	}
	for _, bin := range []string{"sleep", "false"} {
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
	timeout := time.After(15 * time.Second)
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
			t.Fatalf("launcher did not log %q within 15s:\n%s", want, l.stdout)
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
  killCrashReporter: false
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
		{supervisor.Exit{}, false, false, ""},
		{supervisor.Exit{Success: true}, false, false, ""},
		{supervisor.Exit{StartFailed: true}, false, true, "stopping everything"},
		{supervisor.Exit{}, true, false, ""},
		{supervisor.Exit{StartFailed: true}, true, false, "keeps serving MCP"},
	}
	for _, c := range cases {
		ctx, cancel := context.WithCancel(context.Background())
		sess := newSession(ctx, cancel)
		var log syncLog
		makeEditorExitHandler(sess, log.log, c.mcpServing)(c.exit)
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
		"  killCrashReporter: false\n",
		"  killCrashReporter: false\n  cleanOnRestart: true\n  restart: on-failure\n  restartDelaySeconds: 1\n", 1)
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

// k80: off Windows the crash-reporter kill ran pkill -f CrashReportClient
// before every editor start, and -f matches every command line that contains
// the string — an editor open on CrashReportClient.cpp, a tool run on the
// engine's Programs/CrashReportClient directory. It must hit the two crash
// reporter programs and nothing else. The test runs the real commands, limited
// to its own children (pkill -P), so it never touches a process of the user.
func TestCrashReporterKillMatchesOnlyTheProgram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("taskkill /IM matches image names; this tests the pkill pattern")
	}
	if _, err := exec.LookPath("pkill"); err != nil {
		t.Skip("pkill not on PATH")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dir := t.TempDir()
	src := "/src/UE/Engine/Source/Programs/CrashReportClient"
	type proc struct {
		name string
		cmd  *exec.Cmd
		done chan struct{}
	}
	start := func(name, program string, args ...string) *proc {
		p := &proc{name: name, done: make(chan struct{})}
		p.cmd = exec.Command(program, append([]string{"-test.run=^TestSleepProcess$"}, args...)...)
		p.cmd.Env = append(os.Environ(), sleepProcessEnv+"=1")
		if err := p.cmd.Start(); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		go func() { _ = p.cmd.Wait(); close(p.done) }()
		t.Cleanup(func() { _ = p.cmd.Process.Kill(); <-p.done })
		return p
	}
	// The crash reporters are the test binary under their names: argv[0] is
	// the path a symlink is started by.
	var targets []*proc
	for _, name := range []string{"CrashReportClient", "CrashReportClientEditor"} {
		link := filepath.Join(dir, name)
		if err := os.Symlink(exe, link); err != nil {
			t.Skipf("symlink: %v", err)
		}
		targets = append(targets, start(name, link))
	}
	inside := filepath.Join(dir, "Programs", "CrashReportClient")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(inside, "build")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	decoys := []*proc{
		start("an editor open on CrashReportClient.cpp", exe, src+"/Private/CrashReportClient.cpp"),
		start("a tool run on the CrashReportClient directory", exe, src),
		start("a program inside a CrashReportClient directory", filepath.Join(inside, "build")),
	}
	time.Sleep(200 * time.Millisecond) // let them all reach their sleep

	for _, c := range crashReporterKillers() {
		args := append([]string{c[0], "-P", strconv.Itoa(os.Getpid())}, c[1:]...)
		_ = exec.Command(args[0], args[1:]...).Run() // exit 1 = nothing matched
	}
	for _, p := range targets {
		select {
		case <-p.done:
			if ws, ok := p.cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() {
				t.Errorf("%s ended without being killed: %v", p.name, p.cmd.ProcessState)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s was not killed", p.name)
		}
	}
	time.Sleep(100 * time.Millisecond)
	for _, p := range decoys {
		select {
		case <-p.done:
			t.Errorf("killed %s (%v): the pattern matches more than the crash reporter", p.name, p.cmd.ProcessState)
		default:
		}
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
	return &fakeConsole{w: w, con: console{in: r, out: out}, out: out}
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
	ctx, cancel := context.WithCancel(context.Background())
	sess := newSession(ctx, cancel)
	fc := newFakeConsole(t)
	logs := &syncLog{}
	sup := supervisor.New(logs.log)
	var onExit func(bool)
	sup.AddService(supervisor.ServiceSpec{
		Name:      "agent",
		Command:   command,
		Args:      args,
		Autostart: true,
		Restart:   "never",
		OnExit:    func(e supervisor.Exit) { onExit(e.Success) },
	})
	cfg := &config.Config{}
	cfg.Agent.OnExit = config.OnExitAsk
	onExit = makeAgentExitHandler(ctx, sess, sup, cfg, logs.log, true, fc.con)
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })
	return sess, fc, sup
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
			go func() { result <- commandLoop(ctx, fc.con.in, func() { ended = true }, sup, logs.log, "agent") }()

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
