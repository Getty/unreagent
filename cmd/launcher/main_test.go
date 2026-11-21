package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// freeLoopbackAddr returns a loopback address whose port was free a moment
// ago.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick a free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// withMCP turns the MCP server of a launcherYAML config on, at addr.
func withMCP(yaml, addr string) string {
	return strings.Replace(yaml, "mcp:\n  enabled: false\n", "mcp:\n  enabled: true\n  address: \""+addr+"\"\n", 1)
}

// markerScript is an editor or agent stand-in that leaves <script>.ran behind
// once it is started, and then keeps running.
const markerScript = "#!/bin/sh\n: > \"$0.ran\"\nexec sleep 20\n"

// dirState is everything under dir: each file's content by its slash path,
// each directory as its path with a trailing slash.
func dirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			state[rel+"/"] = ""
			return nil
		}
		state[rel] = mustReadFile(t, p)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return state
}

// stateDiff lists, sorted, what got changed, created or removed from before
// to after.
func stateDiff(before, after map[string]string) []string {
	var diff []string
	for p, was := range before {
		switch now, ok := after[p]; {
		case !ok:
			diff = append(diff, "removed "+p)
		case now != was:
			diff = append(diff, "changed "+p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			diff = append(diff, "created "+p)
		}
	}
	slices.Sort(diff)
	return diff
}

// k45/k123: an MCP address that cannot be bound — the port taken by another
// unreagent or another program, or no host:port at all — was only a log line,
// and the launcher went on: the agent then talked to the other instance, and
// an external agent found nothing to connect to. Now run() refuses before the
// editor and the agent start, exit 1, with an error that names the address,
// the cause and the way out.
//
// It refuses before it writes anything, too. The usual holder of the port is
// another unreagent on the same project, so the test plants what that one
// has on disk: its window-mode launcher log, a script file it is running, its
// materialized skills and its mcp.writeConfig file. Every one of them must
// stay byte for byte, and nothing may be created (the log rotation's .1, an
// editor's or agent's .ran marker) or removed.
func TestRunExitsWhenTheMCPAddressCannotBeBound(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy a port: %v", err)
	}
	defer taken.Close()

	for _, tc := range []struct {
		name, addr string
		cause      string
	}{
		{"port taken", taken.Addr().String(), "address already in use"},
		{"no port", "127.0.0.1", "missing port"}, // config.Load refuses it since k57
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			editor, agent := filepath.Join(dir, "editor.sh"), filepath.Join(dir, "agent.sh")
			for _, p := range []string{editor, agent} {
				if err := os.WriteFile(p, []byte(markerScript), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			// The running instance's state; the project dir is dir, so it is
			// also the agent's working directory and the log's directory.
			mustWriteFile(t, filepath.Join(dir, "Demo.uproject"), "{}")
			mustWriteFile(t, filepath.Join(dir, "unreagent.log"), "the running instance's launcher log\n")
			mustWriteFile(t, filepath.Join(dir, "unreagent-123456.py"), "print('a script the running instance runs')\n")
			mustWriteFile(t, filepath.Join(dir, ".claude", "skills", "unreagent-ue-context", "SKILL.md"), "the running instance's skill file\n")
			mustWriteFile(t, filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"the running instance": {}}}`+"\n")
			// Window mode for the log rotation, the python runtime for the
			// sweep, skills.materialize and mcp.writeConfig for the rest.
			yaml := `unreal:
  editor: ` + editor + `
  args: ["20"]
  unattended: false
  killCrashReporter: false
mcp:
  enabled: true
  address: "` + tc.addr + `"
  writeConfig:
    - path: .mcp.json
permissions:
  enabled: false
runtimes:
  python: { enabled: true }
  node: { enabled: false }
skills:
  materialize: true
agent:
  enabled: true
  command: ` + agent + `
  window: true
  claudeIntegration: false
  startDelaySeconds: 1
`
			before := dirState(t, dir)
			before["unreagent.yaml"] = yaml // startLauncherIn writes it
			l := startLauncherIn(t, dir, yaml)
			if code := l.exitCode(t); code != 1 {
				t.Fatalf("exit code %d, want 1 for an MCP address that cannot be bound:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			stderr := l.stderr.String()
			for _, want := range []string{tc.addr, tc.cause, "mcp.address"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr does not mention %q: %q", want, stderr)
				}
			}
			if strings.Contains(l.stdout.String(), "MCP server listening") {
				t.Errorf("logged the MCP server as listening although it cannot bind:\n%s", l.stdout)
			}
			if diff := stateDiff(before, dirState(t, dir)); len(diff) > 0 {
				t.Errorf("a launcher that cannot bind touched the project: %s\n%s", strings.Join(diff, ", "), l.stdout)
			}
		})
	}
}

// k141: "MCP server listening on …" was logged before the port was bound, so
// a client acting on the line could get connection refused. A connect right
// after the line must succeed — no retry.
func TestRunLogsMCPListeningOnlyOnceAConnectSucceeds(t *testing.T) {
	addr := freeLoopbackAddr(t)
	l := startLauncher(t, withMCP(launcherYAML("sleep", "  enabled: false\n"), addr))
	l.waitLine(t, "MCP server listening on "+addr+" ")

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("connect right after the listening line: %v\n%s", err, l.stdout)
	}
	_ = conn.Close()

	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
}

// afterMarker returns what follows marker on the first line of out that has
// it, and whether there was one.
func afterMarker(out, marker string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		if _, rest, ok := strings.Cut(line, marker); ok {
			return rest, true
		}
	}
	return "", false
}

// k57: with mcp.address on every interface (":<port>", an ordinary Go listen
// address), the agent's --mcp-config and the mcp.writeConfig file pointed at
// http://:<port>/mcp. Both carry the loopback now, which a client connects to,
// and the listening line names the address bound as well as that URL.
func TestRunHandsOutALoopbackURLForAWildcardMCPAddress(t *testing.T) {
	_, port, err := net.SplitHostPort(freeLoopbackAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	want := "http://127.0.0.1:" + port + "/mcp"
	dir := t.TempDir()
	agent := filepath.Join(dir, "agent.sh")
	// Each argument on a line of its own, which the launcher logs as
	// "[agent] arg: …".
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nfor a; do printf 'arg: %s\\n' \"$a\"; done\nexec sleep 20\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := startLauncherIn(t, dir, `unreal:
  editor: sleep
  args: ["20"]
  unattended: false
  killCrashReporter: false
mcp:
  enabled: true
  address: ":`+port+`"
  writeConfig:
    - path: .mcp.json
permissions:
  enabled: false
runtimes:
  python: { enabled: false }
  node: { enabled: false }
agent:
  enabled: true
  command: `+agent+`
  window: false
  claudeIntegration: true
  startDelaySeconds: 1
`)
	l.waitLine(t, "[agent] arg: {")
	out := l.stdout.String()

	type mcpServers struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	urlOf := func(what, js string) string {
		var v mcpServers
		if err := json.Unmarshal([]byte(js), &v); err != nil {
			t.Fatalf("%s is not an MCP config: %v\n%s", what, err, js)
		}
		return v.MCPServers[config.MCPServerName].URL
	}
	arg, _ := afterMarker(out, "[agent] arg: {")
	if got := urlOf("--mcp-config", "{"+arg); got != want {
		t.Errorf("--mcp-config hands the agent %q; want %q", got, want)
	}
	if got := urlOf(".mcp.json", mustReadFile(t, filepath.Join(dir, ".mcp.json"))); got != want {
		t.Errorf("mcp.writeConfig wrote %q; want %q", got, want)
	}
	if line, _ := afterMarker(out, "MCP server listening on "); line != ":"+port+" — clients connect to "+want {
		t.Errorf("listening line %q; want it to name %q and %q", line, ":"+port, want)
	}
	conn, err := net.Dial("tcp", strings.TrimSuffix(strings.TrimPrefix(want, "http://"), "/mcp"))
	if err != nil {
		t.Fatalf("connect to the client URL's host:port: %v\n%s", err, out)
	}
	_ = conn.Close()

	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
}

// k88: an EngineAssociation that names no usable engine fell back to the
// highest installed one silently — the editor then opens the project in a
// newer engine and offers to convert it. run() now warns, naming the
// association, the engine chosen and how to pin one.
//
// The engine is found for real: off Windows the Epic default paths
// ("C:/Program Files/Epic Games/UE_*") are relative paths, so they resolve
// under the launcher's working directory, where the test installs a 5.9.
func TestRunWarnsWhenTheEngineAssociationIsNotInstalled(t *testing.T) {
	for _, tc := range []struct {
		name, assoc string
		warn        bool
	}{
		{"association not installed", "5.7", true},
		{"association installed", "5.9", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("UE_ROOT", "")
			dir := t.TempDir()
			engine := "C:/Program Files/Epic Games/UE_5.9"
			mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(engine), "Engine", "Binaries", "Win64", "UnrealEditor.exe"), "")
			mustWriteFile(t, filepath.Join(dir, "Demo.uproject"), `{"EngineAssociation": "`+tc.assoc+`"}`)
			l := startLauncherIn(t, dir, launcherYAML("sleep", "  enabled: false\n"))
			// The warning comes with the engine line, before the project line.
			l.waitLine(t, "Project: ")

			out := l.stdout.String()
			if !strings.Contains(out, "Engine: "+engine) {
				t.Fatalf("auto-detection did not pick the test's engine %s:\n%s", engine, out)
			}
			var warning string
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "WARN") && strings.Contains(line, "EngineAssociation") {
					warning = line
				}
			}
			if !tc.warn {
				if warning != "" {
					t.Errorf("warned although the associated engine is installed: %s", warning)
				}
				return
			}
			if warning == "" {
				t.Fatalf("no WARN about the EngineAssociation:\n%s", out)
			}
			for _, want := range []string{`"5.7"`, "5.9", engine, "engineRoot", "UE_ROOT"} {
				if !strings.Contains(warning, want) {
					t.Errorf("warning does not mention %q: %s", want, warning)
				}
			}
		})
	}
}

// brokenListener is a bound port that breaks: Accept fails with an error that
// is not temporary, so http.Server.Serve gives up at once.
type brokenListener struct{}

func (brokenListener) Accept() (net.Conn, error) { return nil, errors.New("accept: listener broken") }
func (brokenListener) Close() error              { return nil }
func (brokenListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8765} }

// acceptFailEnv makes TestLauncherRunProcess bind the MCP port for real and
// then break it: every Accept fails with acceptFails' error.
const acceptFailEnv = "UNREAGENT_TEST_MCP_ACCEPT_FAIL"

// acceptFails is a bound listener whose Accept fails with an error that is
// not temporary, so http.Server.Serve gives up at once.
type acceptFails struct{ net.Listener }

func (acceptFails) Accept() (net.Conn, error) { return nil, errors.New("accept: injected failure") }

// waitFileLine waits until the file at path has a line containing want.
func waitFileLine(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		b, _ := os.ReadFile(path)
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, want) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s has no line with %q within 15s:\n%s", path, want, b)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// k123, decision 5, through run(): a port that breaks after the bind leaves
// the launcher running (the agent still starts after it) and is reported once
// on the console the user sees. In window mode the launcher log is a file and
// the console belongs to the agent, so the line also goes to stderr; with the
// log on stdout it would be the same line twice, so stderr stays clean.
func TestRunReportsAServeFailureOnTheUsersConsole(t *testing.T) {
	for _, tc := range []struct {
		name       string
		window     bool
		wantStderr bool
	}{
		{"window mode, log is a file", true, true},
		{"log on stdout", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(acceptFailEnv, "1")
			addr := freeLoopbackAddr(t)
			dir := t.TempDir()
			yaml := withMCP(launcherYAML("sleep", fmt.Sprintf(`  enabled: true
  command: sleep
  args: ["20"]
  window: %v
  claudeIntegration: false
  startDelaySeconds: 1
`, tc.window)), addr)
			l := startLauncherIn(t, dir, yaml)
			const errLine = "ERROR MCP server on "
			if tc.window {
				log := filepath.Join(dir, "unreagent.log")
				waitFileLine(t, log, errLine)
				waitFileLine(t, log, "[agent] started (pid")
			} else {
				l.waitLine(t, errLine)
				l.waitLine(t, "[agent] started (pid")
			}

			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			stderr := l.stderr.String()
			alerts := strings.Count(stderr, "unreagent: "+errLine+addr+" stopped serving")
			if tc.wantStderr && alerts != 1 {
				t.Errorf("stderr has the ERROR line %d times; want once:\n%s", alerts, stderr)
			}
			if !tc.wantStderr && strings.Contains(stderr, "ERROR") {
				t.Errorf("stderr repeats the ERROR line that the log on stdout already shows:\n%s", stderr)
			}
		})
	}
}

// Main agent's decision for k123: an MCP server that stops serving after a
// successful bind does not end the launcher. The editor may hold unsaved
// work, so serveMCP marks MCP as not serving and says so loudly, once, in the
// log and on the alert writer: what broke, that the agent's unreagent tools
// are gone, and what to do. A nil alert writer is the log alone.
func TestServeMCPFailureMarksMCPNotServingAndSaysSo(t *testing.T) {
	for _, withAlert := range []bool{true, false} {
		var serving atomic.Bool
		serving.Store(true)
		var log syncLog
		var alert bytes.Buffer
		var w io.Writer
		if withAlert {
			w = &alert
		}
		serveMCP(&http.Server{Handler: http.NotFoundHandler()}, brokenListener{}, &serving, log.log, w)

		if serving.Load() {
			t.Errorf("alert %v: MCP still marked as serving after it stopped", withAlert)
		}
		if len(log.lines) != 1 {
			t.Fatalf("alert %v: logged %q; want one line", withAlert, log.lines)
		}
		line := log.lines[0]
		for _, want := range []string{"ERROR", "MCP server on 127.0.0.1:8765 stopped serving", "listener broken", "unreagent tools", "no longer work", "everything else keeps running", "Save your work in the editor", "restart unreagent"} {
			if !strings.Contains(line, want) {
				t.Errorf("alert %v: line %q does not say %q", withAlert, line, want)
			}
		}
		if want := "unreagent: " + line + "\n"; withAlert && alert.String() != want {
			t.Errorf("alert writer got %q; want %q", alert.String(), want)
		}
	}
}

// k69 with k123: the give-up line reads MCP's state when the editor gives up,
// not when the handler was built. After a serve failure it must not claim the
// launcher keeps serving MCP; without an agent, nothing can retry then, so the
// session ends as with MCP off. The serve failure itself does not end it.
func TestEditorGiveUpAfterMCPStoppedServing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := newSession(ctx, cancel)
	var serving atomic.Bool
	serving.Store(true)
	var log syncLog
	onEditorExit := makeEditorExitHandler(sess, log.log, false, &serving, config.UnrealConfig{}, "")

	serveMCP(&http.Server{Handler: http.NotFoundHandler()}, brokenListener{}, &serving, log.log, nil)
	if ctx.Err() != nil || sess.result() != nil {
		t.Fatalf("the serve failure ended the session (result %v); the editor and the agent must keep running", sess.result())
	}

	onEditorExit(supervisor.Exit{StartFailed: true})
	got := log.String()
	if strings.Contains(got, "keeps serving MCP") {
		t.Errorf("the give-up claims MCP is served after it stopped:\n%s", got)
	}
	if !strings.Contains(got, "MCP is not serving — stopping everything") {
		t.Errorf("logged %q; want the give-up for MCP not serving", got)
	}
	if ctx.Err() == nil || sess.result() == nil {
		t.Errorf("session ended = %v, result %v; want ended with an error", ctx.Err() != nil, sess.result())
	}
}

// The launcher's own shutdown closes the server; that is not a failure.
func TestServeMCPClosedIsNotAFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var serving atomic.Bool
	serving.Store(true)
	var log syncLog
	var alert bytes.Buffer
	srv := &http.Server{Handler: http.NotFoundHandler()}
	served := make(chan struct{})
	go func() { serveMCP(srv, ln, &serving, log.log, &alert); close(served) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("server not serving: %v", err)
	}
	_ = resp.Body.Close()
	_ = srv.Close()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("serveMCP did not return after the server was closed")
	}
	if got := log.String(); got != "" || alert.Len() != 0 {
		t.Errorf("logged %q, alerted %q on a normal close; want nothing", got, alert.String())
	}
}

// k48: the sub-command after a bool flag or a -flag=value form was taken as
// that flag's value, so the launcher started the editor and the agent instead
// of the one-shot sub-command; an unknown word launched as well. Each line
// here must end without starting the editor or writing anything. The harness
// puts "-config <path>" in front of the arguments given; CONFIG stands for
// that path, OTHER for a second config in a subdirectory.
func TestRunFindsTheSubcommandAfterAnyFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string // in stdout + stderr
	}{
		// hermes-setup runs on the config and, with agent.hermes off, has
		// nothing to do.
		{"-flag=value before", []string{"-config=CONFIG", "hermes-setup"}, 0, "agent.hermes.enabled is not set"},
		// The -config after the word wins over the harness's, across the --
		// that ended the launcher's flags. hermes-setup names the file it read.
		{"-- before the word, -config after it", []string{"--", "hermes-setup", "-config", "OTHER"}, 0, "Hint: this file — OTHER"},
		// hermes-setup runs and refuses the launcher flag, as it does after
		// the word.
		{"bool flag before", []string{"-no-agent", "hermes-setup"}, 1, "Usage of hermes-setup"},
		{"unknown word after a bool flag", []string{"-no-agent", "bogus"}, 2, `unknown sub-command "bogus" (sub-commands: hermes-setup)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			editor := filepath.Join(dir, "editor.sh")
			if err := os.WriteFile(editor, []byte(markerScript), 0o755); err != nil {
				t.Fatal(err)
			}
			yaml := launcherYAML(editor, "  enabled: false\n")
			config, other := filepath.Join(dir, "unreagent.yaml"), filepath.Join(dir, "other", "unreagent.yaml")
			mustWriteFile(t, other, yaml)
			args := slices.Clone(tc.args)
			for i, a := range args {
				args[i] = strings.NewReplacer("CONFIG", config, "OTHER", other).Replace(a)
			}
			want := strings.ReplaceAll(tc.want, "OTHER", other)
			before := dirState(t, dir)
			before["unreagent.yaml"] = yaml // startLauncherIn writes it
			l := startLauncherIn(t, dir, yaml, args...)
			code := l.exitCode(t)
			out := l.stdout.String() + l.stderr.String()
			if code != tc.code {
				t.Errorf("exit code %d, want %d:\n%s", code, tc.code, out)
			}
			if !strings.Contains(out, want) {
				t.Errorf("output does not contain %q:\n%s", want, out)
			}
			if _, err := os.Stat(editor + ".ran"); err == nil {
				t.Errorf("the editor was started:\n%s", out)
			}
			if diff := stateDiff(before, dirState(t, dir)); len(diff) > 0 {
				t.Errorf("wrote into the project: %s\n%s", strings.Join(diff, ", "), out)
			}
		})
	}
}

// k48: extractSubcommand finds the sub-command with the flag set's own
// parsing. The set here stands in for the launcher's: a bool and a string
// flag, defined like -no-agent and -config.
func TestExtractSubcommand(t *testing.T) {
	file := filepath.Join(t.TempDir(), "unreagent.yaml")
	mustWriteFile(t, file, "")
	for _, tc := range []struct {
		name     string
		args     []string
		sub      string
		rest     []string
		config   string // the -config hermes-setup's flag set reads from rest; "" for no check
		usageErr string // a usageError containing this; "" for none
	}{
		{name: "no arguments"},
		{name: "flags only", args: []string{"-no-agent", "-config", "x"}},
		{name: "the word alone", args: []string{"hermes-setup"}, sub: "hermes-setup"},
		{name: "bool flag before", args: []string{"-no-agent", "hermes-setup"}, sub: "hermes-setup", rest: []string{"-no-agent"}},
		{name: "-flag=value before", args: []string{"-config=x", "hermes-setup"}, sub: "hermes-setup", rest: []string{"-config=x"}},
		{name: "--flag=value before", args: []string{"--config=x", "hermes-setup"}, sub: "hermes-setup", rest: []string{"--config=x"}},
		{name: "-flag value before", args: []string{"-config", "x", "hermes-setup"}, sub: "hermes-setup", rest: []string{"-config", "x"}},
		{name: "flags after", args: []string{"hermes-setup", "-config", "x"}, sub: "hermes-setup", rest: []string{"-config", "x"}},
		{name: "flags on both sides", args: []string{"-no-agent", "hermes-setup", "-config=x"}, sub: "hermes-setup", rest: []string{"-no-agent", "-config=x"}},
		// The word is -config's value here: a config file of that name.
		{name: "the word as a flag value", args: []string{"-config", "hermes-setup"}},
		// The -- that ended the launcher's flags is dropped: kept, it would end
		// the sub-command's flags before the first one, and hermes-setup would
		// run on the default config.
		{name: "-- before the word", args: []string{"--", "hermes-setup"}, sub: "hermes-setup"},
		{name: "-- before the word, a flag after", args: []string{"--", "hermes-setup", "-config", "B"}, sub: "hermes-setup", rest: []string{"-config", "B"}, config: "B"},
		{name: "-config on both sides of --", args: []string{"-config", "A", "--", "hermes-setup", "-config", "B"}, sub: "hermes-setup", rest: []string{"-config", "A", "-config", "B"}, config: "B"},
		{name: "a bool flag before --", args: []string{"-no-agent", "--", "hermes-setup", "-config=B"}, sub: "hermes-setup", rest: []string{"-no-agent", "-config=B"}},
		{name: "-flag=value before --", args: []string{"-config=A", "--", "hermes-setup"}, sub: "hermes-setup", rest: []string{"-config=A"}, config: "A"},
		// This -- is -config's value, not the end of the flags: it stays.
		{name: "-- as a flag value", args: []string{"-config", "--", "hermes-setup"}, sub: "hermes-setup", rest: []string{"-config", "--"}, config: "--"},
		{name: "-- as a flag value, then --", args: []string{"-config", "--", "--", "hermes-setup"}, sub: "hermes-setup", rest: []string{"-config", "--"}, config: "--"},
		{name: "unknown word", args: []string{"bogus"}, usageErr: `unknown sub-command "bogus" (sub-commands: hermes-setup)`},
		{name: "unknown word after a bool flag", args: []string{"-no-agent", "bogus"}, usageErr: `unknown sub-command "bogus"`},
		{name: "unknown word before the sub-command", args: []string{"bogus", "hermes-setup"}, usageErr: `unknown sub-command "bogus"`},
		// A file dropped onto the .exe arrives as its path.
		{name: "an existing file", args: []string{file}, usageErr: `"` + file + `" is not a sub-command: unreagent does not open files passed to it`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := flag.NewFlagSet("unreagent", flag.ContinueOnError)
			set.SetOutput(io.Discard)
			set.Bool("no-agent", false, "")
			set.String("config", "", "")
			sub, rest, err := extractSubcommand(set, tc.args)
			if tc.usageErr != "" {
				if !errors.As(err, new(usageError)) || !strings.Contains(err.Error(), tc.usageErr) {
					t.Fatalf("err = %v, want a usage error containing %q", err, tc.usageErr)
				}
				if sub != "" {
					t.Errorf("sub = %q with a usage error, want none", sub)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if sub != tc.sub || !slices.Equal(rest, tc.rest) {
				t.Errorf("got %q %q, want %q %q", sub, rest, tc.sub, tc.rest)
			}
			if tc.config != "" {
				hs := flag.NewFlagSet("hermes-setup", flag.ContinueOnError)
				hs.SetOutput(io.Discard)
				cfg := hs.String("config", "", "")
				if err := hs.Parse(rest); err != nil || *cfg != tc.config {
					t.Errorf("hermes-setup's flags read -config %q (err %v) from %q, want %q", *cfg, err, rest, tc.config)
				}
			}
		})
	}
}

// A bad flag is the flag package's error, not a usageError: the launcher's
// flag set exits 2 for it on its own.
func TestExtractSubcommandBadFlag(t *testing.T) {
	set := flag.NewFlagSet("unreagent", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	_, _, err := extractSubcommand(set, []string{"-nosuch", "hermes-setup"})
	if err == nil || errors.As(err, new(usageError)) {
		t.Fatalf("err = %v, want the flag package's error", err)
	}
}
