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
	"regexp"
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
	agent := recordingAgent(t, dir)
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
	// The file is removed once the launcher ends (k181).
	agentConfig := mustReadFile(t, mcpConfigArg(t, recordedArgs(t, agent)))
	l.waitLine(t, "MCP server listening on ")
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
	if got := urlOf("--mcp-config", agentConfig); got != want {
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

// k60: the MCP server's Origin allowlist could be replaced only in code
// (SetAllowedOrigins), which run() never called, so a browser-based MCP client
// served from a non-loopback origin was refused with no way to allow it. A
// non-empty mcp.allowedOrigins replaces the built-in loopback allowlist and is
// named in the log; absent or empty keeps the built-in one, silently.
func TestRunAppliesMCPAllowedOrigins(t *testing.T) {
	loopback := []string{"http://localhost:3000", "http://127.0.0.1:9"}
	for _, tc := range []struct {
		name, origins    string // the mcp.allowedOrigins line, "" for none
		allowed, refused []string
		logged           string // the allowlist line, "" for none
	}{
		{"absent", "", loopback, []string{"https://ide.example.com"}, ""},
		{"empty", "  allowedOrigins: []\n", loopback, []string{"https://ide.example.com"}, ""},
		{
			"set", "  allowedOrigins: [\"https://ide.example.com\", \"http://192.168.1.20:8080\"]\n",
			[]string{"https://ide.example.com", "HTTPS://IDE.EXAMPLE.COM", "http://192.168.1.20:8080"},
			append([]string{"https://evil.example"}, loopback...),
			"MCP server: browser Origins allowed: https://ide.example.com, http://192.168.1.20:8080 (mcp.allowedOrigins, replacing the built-in loopback origins)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr := freeLoopbackAddr(t)
			yaml := withMCP(launcherYAML("sleep", "  enabled: false\n"), addr)
			yaml = strings.Replace(yaml, "  address: \""+addr+"\"\n", "  address: \""+addr+"\"\n"+tc.origins, 1)
			l := startLauncher(t, yaml)
			l.waitLine(t, "MCP server listening on "+addr+" ")

			status := func(origin string) int {
				req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/mcp",
					strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", origin)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("POST with Origin %s: %v\n%s", origin, err, l.stdout)
				}
				_ = resp.Body.Close()
				return resp.StatusCode
			}
			for _, o := range tc.allowed {
				if got := status(o); got != http.StatusOK {
					t.Errorf("Origin %s: status %d; want 200", o, got)
				}
			}
			for _, o := range tc.refused {
				if got := status(o); got != http.StatusForbidden {
					t.Errorf("Origin %s: status %d; want 403", o, got)
				}
			}
			out := l.stdout.String()
			if line, ok := afterMarker(out, "MCP server: browser Origins"); tc.logged == "" && ok {
				t.Errorf("logged an allowlist with none configured: %q", line)
			} else if tc.logged != "" && !strings.Contains(out, tc.logged) {
				t.Errorf("log does not name the allowlist, want the line %q:\n%s", tc.logged, out)
			}

			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
		})
	}
}

// k88: an EngineAssociation that names no usable engine fell back to the
// highest installed one silently — the editor then opens the project with
// an engine it is not associated with. run() now warns, naming the
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
		if !serveMCP(&http.Server{Handler: http.NotFoundHandler()}, brokenListener{}, &serving, log.log, w) {
			t.Errorf("alert %v: serveMCP reports a close; want it to report that it stopped serving, which run() ends a waiting editor give-up for (k155)", withAlert)
		}

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
	onEditorExit := makeEditorExitHandler(sess, log.log, false, &serving, new(editorGiveUp), config.UnrealConfig{}, "")

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
	var stopped bool // read after served is closed
	go func() { stopped = serveMCP(srv, ln, &serving, log.log, &alert); close(served) }()

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
	if stopped {
		t.Error("serveMCP reports a normal close as stopped serving; run() would end a waiting editor give-up on the launcher's own shutdown (k155)")
	}
	if got := log.String(); got != "" || alert.Len() != 0 {
		t.Errorf("logged %q, alerted %q on a normal close; want nothing", got, alert.String())
	}
}

// k238: a start on the built-in defaults says plainly that unreagent applies
// no permission policy there. An unreagent.local.yaml that enables permissions
// makes that untrue, and the line goes.
func TestDefaultsNoticeSaysNoPermissionPolicy(t *testing.T) {
	off := strings.Join(defaultsNotice(&config.Config{}), "\n")
	for _, want := range []string{"built-in defaults", "applies no permission policy", "Claude Code's own permission prompts"} {
		if !strings.Contains(off, want) {
			t.Errorf("notice does not mention %q:\n%s", want, off)
		}
	}
	var cfg config.Config
	cfg.Permissions.Enabled = true
	on := defaultsNotice(&cfg)
	if len(on) != 1 || strings.Contains(on[0], "permission") {
		t.Errorf("notice with permissions enabled by the overlay = %q; want the defaults line alone", on)
	}
}

// k38, k46 (maintainer's decision: warn, do not refuse): an enabled
// permissions policy that the launcher cannot hand to the agent it starts —
// an interactive agent, or one without claudeIntegration — is named in one
// warning: that it is not enforced, what is passed instead, and how to get it
// enforced. No policy, an external agent, a headless Claude Code with the
// integration and a headless agent with a prompt tool wired by hand are
// silent. An interactive agent always warns, a hand-wired prompt tool or not
// (main agent's decision on the k38 review): that flag is a headless feature,
// and the warning's fix is to run headless.
func TestPermissionsInertReason(t *testing.T) {
	headless := []string{"-p", "work through the tasks"}
	handWired := []string{"--permission-prompt-tool", "mcp__unreagent__approve"}
	const (
		notEnforced = "permissions are not enforced: "
		skipFlag    = "--dangerously-skip-permissions"
		headlessFix = `put "-p" (or "--print") and its task in agent.args, and leave agent.window unset or false`
		windowMode  = "runs interactively in window mode (agent.window: true, or agent.window unset and no -p/--print in agent.args)"
		launcherOff = "the launcher passes --permission-prompt-tool only to a headless agent"
		wiredOff    = "the --permission-prompt-tool wired into agent.args by hand takes effect only for a headless agent"
		noClaude    = "agent.claudeIntegration is false"
		claudeFix   = "set agent.claudeIntegration: true"
		askWindow   = "asks in its window"
		otherTool   = `names "mcp__other__ask", not mcp__unreagent__approve`
		setApprove  = "set it to mcp__unreagent__approve"
		noSecond    = "The launcher passes no --permission-prompt-tool of its own next to it."
		orRemove    = "or remove it from agent.args"
	)
	modes := []string{config.ModeAllowAll, config.ModeAllowlist, config.ModeDenyAll}
	type row struct {
		name        string
		enabled     bool // permissions.enabled
		mode        string
		agent       bool // agent.enabled
		claude      bool // agent.claudeIntegration
		args        []string
		interactive bool     // as run() derives it from agent.window and agent.args
		want        []string // nil: no warning
		absent      []string
	}
	rows := []row{
		{name: "permissions disabled, interactive allow_all", mode: config.ModeAllowAll, agent: true, claude: true, interactive: true},
		{name: "permissions disabled, no claudeIntegration", mode: config.ModeDenyAll, agent: true},
		// run() never sees an agent it does not start as interactive.
		{name: "external agent, claudeIntegration", enabled: true, mode: config.ModeAllowAll, claude: true},
		{name: "external agent, no claudeIntegration", enabled: true, mode: config.ModeDenyAll},
		{name: "headless --print", enabled: true, mode: config.ModeAllowlist, agent: true, claude: true, args: []string{"--print", "task"}},
		{name: "agent.window: false without -p", enabled: true, mode: config.ModeAllowAll, agent: true, claude: true},
		{name: "no claudeIntegration, headless, prompt tool wired by hand", enabled: true, mode: config.ModeAllowlist, agent: true,
			args: append(slices.Clone(headless), handWired...)},
		{name: "no claudeIntegration, headless, prompt tool wired by hand as -flag=value", enabled: true, mode: config.ModeAllowlist, agent: true,
			args: append(slices.Clone(headless), "--permission-prompt-tool=mcp__unreagent__approve")},
		{name: "interactive, agent.window: true with -p", enabled: true, mode: config.ModeAllowlist, agent: true, claude: true, args: headless, interactive: true,
			want: []string{windowMode, launcherOff, headlessFix}, absent: []string{skipFlag, noClaude, wiredOff}},
		{name: "no claudeIntegration and interactive: one warning, both fixes", enabled: true, mode: config.ModeAllowAll, agent: true, interactive: true,
			want: []string{noClaude, claudeFix, headlessFix}, absent: []string{skipFlag, windowMode}},
		{name: "no claudeIntegration, interactive, prompt tool wired by hand as -flag=value", enabled: true, mode: config.ModeAllowlist, agent: true,
			args: []string{"--permission-prompt-tool=mcp__unreagent__approve"}, interactive: true,
			want: []string{windowMode, wiredOff, headlessFix}, absent: []string{noClaude, claudeFix, launcherOff, skipFlag, setApprove}},
		// k172: the launcher passes no prompt tool of its own next to a
		// hand-wired one, so one naming another tool leaves the policy
		// unconsulted, with claudeIntegration or without.
		{name: "headless, another prompt tool wired by hand", enabled: true, mode: config.ModeAllowlist, agent: true, claude: true,
			args: append(slices.Clone(headless), "--permission-prompt-tool", "mcp__other__ask"),
			want: []string{otherTool, setApprove, noSecond, orRemove}, absent: []string{windowMode, headlessFix, noClaude, skipFlag}},
		{name: "headless, another prompt tool wired by hand as -flag=value", enabled: true, mode: config.ModeAllowAll, agent: true, claude: true,
			args: append(slices.Clone(headless), "--permission-prompt-tool=mcp__other__ask"),
			want: []string{otherTool, setApprove, noSecond, orRemove}, absent: []string{windowMode, headlessFix, skipFlag}},
		{name: "no claudeIntegration, headless, another prompt tool wired by hand", enabled: true, mode: config.ModeDenyAll, agent: true,
			args: append(slices.Clone(headless), "--permission-prompt-tool", "mcp__other__ask"),
			want: []string{otherTool, setApprove}, absent: []string{windowMode, headlessFix, noClaude, noSecond, orRemove}},
		{name: "headless, approve and another tool wired by hand", enabled: true, mode: config.ModeAllowlist, agent: true, claude: true,
			args: append(slices.Clone(headless), "--permission-prompt-tool", "mcp__unreagent__approve", "--permission-prompt-tool=mcp__other__ask"),
			want: []string{otherTool, setApprove}, absent: []string{windowMode, headlessFix}},
		{name: "interactive, another prompt tool wired by hand", enabled: true, mode: config.ModeAllowlist, agent: true, claude: true,
			args: []string{"--permission-prompt-tool", "mcp__other__ask"}, interactive: true,
			want: []string{windowMode, wiredOff, headlessFix, setApprove}, absent: []string{launcherOff, noClaude}},
	}
	for _, mode := range modes {
		rows = append(rows, row{name: "headless -p, " + mode, enabled: true, mode: mode, agent: true, claude: true, args: headless},
			row{name: "headless -p, prompt tool wired by hand, " + mode, enabled: true, mode: mode, agent: true, claude: true,
				args: append(slices.Clone(headless), handWired...)})
		interactive := row{name: "interactive, " + mode, enabled: true, mode: mode, agent: true, claude: true, interactive: true,
			want: []string{windowMode, launcherOff, headlessFix}, absent: []string{noClaude, wiredOff}}
		// A hand-wired prompt tool on an interactive agent: the launcher's own
		// flags as above, but the text names the hand-wired flag and no
		// longer claims Claude Code asks as usual.
		wired := row{name: "interactive, prompt tool wired by hand, " + mode, enabled: true, mode: mode, agent: true, claude: true,
			args: handWired, interactive: true,
			want: []string{windowMode, wiredOff, headlessFix}, absent: []string{noClaude, claudeFix, launcherOff, askWindow, setApprove}}
		if mode == config.ModeAllowAll {
			interactive.want = append(interactive.want, skipFlag, "including one a deny rule names")
			wired.want = append(wired.want, "The launcher also passes --dangerously-skip-permissions (mode allow_all).")
		} else {
			interactive.want = append(interactive.want, "passes no permission flag instead", askWindow)
			interactive.absent = append(interactive.absent, skipFlag)
			wired.absent = append(wired.absent, skipFlag)
		}
		rows = append(rows, interactive, wired,
			row{name: "no claudeIntegration, interactive, prompt tool wired by hand, " + mode, enabled: true, mode: mode, agent: true,
				args: handWired, interactive: true,
				want: []string{windowMode, wiredOff, headlessFix}, absent: []string{noClaude, claudeFix, launcherOff, skipFlag}},
			row{name: "no claudeIntegration, headless, " + mode, enabled: true, mode: mode, agent: true, args: headless,
				want: []string{noClaude, claudeFix}, absent: []string{headlessFix, skipFlag, windowMode}})
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Agent:       config.AgentConfig{Enabled: tc.agent, ClaudeIntegration: tc.claude, Args: tc.args},
				MCP:         config.MCPConfig{Enabled: true}, // validate requires it for permissions
				Permissions: config.Permissions{Enabled: tc.enabled, Mode: tc.mode, Deny: []string{"Bash(rm -rf *)"}},
			}
			got := permissionsInertReason(cfg, tc.interactive)
			if tc.want == nil {
				if got != "" {
					t.Fatalf("warned although the policy takes effect or there is nothing to enforce: %s", got)
				}
				return
			}
			if !strings.HasPrefix(got, notEnforced) {
				t.Fatalf("got %q, want a warning starting %q", got, notEnforced)
			}
			for _, want := range append(tc.want, "permissions.mode ("+tc.mode+"), allow and deny are never consulted") {
				if !strings.Contains(got, want) {
					t.Errorf("warning does not say %q: %s", want, got)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Errorf("warning says %q, which does not apply here: %s", absent, got)
				}
			}
		})
	}
}

// k38, k46 through run(): the warning about an inert permissions policy is
// logged before the agent command line. In window mode, where the log is a
// file and the console the agent's, it also goes to stderr, once; with the
// log on stdout stderr stays clean. A headless Claude Code with the
// integration gets the prompt tool and no warning.
func TestRunWarnsWhenThePermissionsPolicyIsInert(t *testing.T) {
	for _, tc := range []struct {
		name   string
		agent  string // agent settings after enabled and command
		window bool
		want   string // in the WARN line; "" = no warning
	}{
		{"window mode, allow_all (k38)", "  window: true\n  claudeIntegration: true\n", true, "--dangerously-skip-permissions"},
		{"no claudeIntegration, log on stdout (k46)", "  window: false\n  claudeIntegration: false\n", false, "agent.claudeIntegration is false"},
		{"headless with the integration", "  args: [\"-p\", \"task\"]\n  claudeIntegration: true\n", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			agent := filepath.Join(dir, "agent.sh") // ignores the flags the launcher adds
			if err := os.WriteFile(agent, []byte(markerScript), 0o755); err != nil {
				t.Fatalf("write agent: %v", err)
			}
			yaml := withMCP(launcherYAML("sleep", "  enabled: true\n  command: "+agent+"\n"+tc.agent+"  startDelaySeconds: 1\n"), freeLoopbackAddr(t))
			yaml = strings.Replace(yaml, "permissions:\n  enabled: false\n",
				"permissions:\n  enabled: true\n  mode: allow_all\n  deny: [\"Bash(rm -rf *)\"]\n", 1)
			l := startLauncherIn(t, dir, yaml)
			// Logged after the agent command line, and once the signal
			// handler is installed (k141): SIGTERM ends the run cleanly.
			const listening = "MCP server listening on "
			logPath := filepath.Join(dir, "unreagent.log")
			if tc.window {
				waitFileLine(t, logPath, listening)
			} else {
				l.waitLine(t, listening)
			}
			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			log := l.stdout.String()
			if tc.window {
				b, err := os.ReadFile(logPath)
				if err != nil {
					t.Fatalf("read launcher log: %v", err)
				}
				log = string(b)
			}
			const warn = "WARN permissions are not enforced: "
			var line string
			for _, ln := range strings.Split(log, "\n") {
				if strings.Contains(ln, warn) {
					line = ln
				}
			}
			stderr := l.stderr.String()
			if tc.want == "" {
				if line != "" || strings.Contains(stderr, warn) {
					t.Fatalf("warned although the prompt tool is passed:\nlog: %s\nstderr: %s", line, stderr)
				}
				if !strings.Contains(log, "--permission-prompt-tool mcp__unreagent__approve") {
					t.Errorf("the agent command has no prompt tool:\n%s", log)
				}
				return
			}
			if !strings.Contains(line, tc.want) {
				t.Fatalf("launcher log has no warning saying %q:\n%s", tc.want, log)
			}
			if before, _, _ := strings.Cut(log, "Agent command: "); !strings.Contains(before, warn) {
				t.Errorf("the warning does not come before the agent command line:\n%s", log)
			}
			alerts := strings.Count(stderr, "unreagent: "+warn)
			if tc.window && alerts != 1 {
				t.Errorf("stderr has the warning %d times; want once:\n%s", alerts, stderr)
			}
			if !tc.window && alerts != 0 {
				t.Errorf("stderr repeats the warning that the log on stdout already shows:\n%s", stderr)
			}
		})
	}
}

// k172: with claudeIntegration, a headless agent whose agent.args wires
// --permission-prompt-tool by hand got the launcher's own as well: the flag
// twice, and which one Claude Code takes is unverified. The launcher now
// adds none next to a hand-wired one, in either form, so the agent gets the
// flag once. One naming mcp__unreagent__approve gets an INFO line that it is
// set already, one naming another tool the WARN that the policy is not
// enforced; with none wired by hand, the launcher passes its own, silently.
func TestRunPassesNoSecondPermissionPromptTool(t *testing.T) {
	const (
		approve = "mcp__unreagent__approve"
		info    = "Agent: --permission-prompt-tool " + approve + " is set in agent.args already"
		warn    = "WARN permissions are not enforced: "
	)
	for _, tc := range []struct {
		name  string
		wired []string // agent.args after "-p", "task"
		want  []string // the agent's --permission-prompt-tool arguments
		info  bool
		warn  string // in the WARN line; "" = no warning
	}{
		{name: "none by hand", want: []string{"--permission-prompt-tool", approve}},
		{name: "approve by hand", wired: []string{"--permission-prompt-tool", approve},
			want: []string{"--permission-prompt-tool", approve}, info: true},
		{name: "approve by hand as -flag=value", wired: []string{"--permission-prompt-tool=" + approve},
			want: []string{"--permission-prompt-tool=" + approve}, info: true},
		{name: "another tool by hand", wired: []string{"--permission-prompt-tool", "mcp__other__ask"},
			want: []string{"--permission-prompt-tool", "mcp__other__ask"}, warn: `names "mcp__other__ask"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			agent := recordingAgent(t, dir)
			args := `"-p", "task"`
			for _, a := range tc.wired {
				args += fmt.Sprintf(", %q", a)
			}
			yaml := withMCP(launcherYAML("sleep", "  enabled: true\n  command: "+agent+"\n  args: ["+args+"]\n  claudeIntegration: true\n  startDelaySeconds: 1\n"), freeLoopbackAddr(t))
			yaml = strings.Replace(yaml, "permissions:\n  enabled: false\n",
				"permissions:\n  enabled: true\n  mode: allow_all\n  deny: [\"Bash(rm -rf *)\"]\n", 1)
			l := startLauncherIn(t, dir, yaml)

			var got []string
			recorded := recordedArgs(t, agent)
			for i, a := range recorded {
				switch {
				case a == "--permission-prompt-tool" && i+1 < len(recorded):
					got = append(got, a, recorded[i+1])
				case a == "--permission-prompt-tool" || strings.HasPrefix(a, "--permission-prompt-tool="):
					got = append(got, a)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("the agent's prompt-tool arguments are %q; want %q\nall: %q", got, tc.want, recorded)
			}
			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			log := l.stdout.String()
			wantInfo := 0
			if tc.info {
				wantInfo = 1
			}
			if got := strings.Count(log, info); got != wantInfo {
				t.Errorf("the log has %q %d times; want %d:\n%s", info, got, wantInfo, log)
			}
			var line string
			for _, ln := range strings.Split(log, "\n") {
				if strings.Contains(ln, warn) {
					line = ln
				}
			}
			if tc.warn == "" && line != "" {
				t.Errorf("warned although the policy takes effect: %s", line)
			}
			if tc.warn != "" && !strings.Contains(line, tc.warn) {
				t.Errorf("the log has no warning saying %q:\n%s", tc.warn, log)
			}
		})
	}
}

// k56, k79 through run(): the warnings of the bridge and the runtime
// preparation went to the launcher log alone, which window mode puts in a
// file: the agent complained about a broken MCP server while the explanation
// sat where the user never looked. In window mode they also go to stderr,
// once each, and the preparation's other lines stay in the log; with the log
// on stdout stderr stays clean. stderr gets a WARN's first line only, with a
// pointer to the log for an output tail, which the log keeps whole (main
// agent's decision on the review).
func TestRunAlertsPreparationWarnings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		window bool
	}{
		{"window mode, log is a file", true},
		{"log on stdout", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr := freeLoopbackAddr(t)
			dir := t.TempDir()
			missing := filepath.Join(dir, "missing") // the bridge is not there
			uv := filepath.Join(dir, "uv")           // fails with two lines of output
			newScripts(t).write(uv, "echo 'error: no solution found'\necho 'hint: pin a version'\nexit 1\n")
			if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			yaml := withMCP(launcherYAML("sleep", fmt.Sprintf(`  enabled: true
  command: sleep
  args: ["20"]
  window: %v
  claudeIntegration: false
  startDelaySeconds: 1
`, tc.window)), addr)
			yaml = strings.Replace(yaml, "  address: \""+addr+"\"\n",
				"  address: \""+addr+"\"\n  extraServers:\n    toolkit:\n      command: \""+missing+"\"\n", 1)
			yaml = strings.Replace(yaml, "  python: { enabled: false }\n",
				"  python: { enabled: true, uv: \""+uv+"\", project: \""+dir+"\", prepareOnStart: true }\n", 1)
			l := startLauncherIn(t, dir, yaml)
			const (
				bridgeWarn  = "WARN MCP server 'toolkit': command '"
				runtimeWarn = "WARN Runtime: preparing Python failed — "
				runtimeInfo = "Runtime: preparing Python (uv sync)"
				tail        = "error: no solution found"
			)
			// No .uproject: the log is unreagent.log in the working directory.
			runtimeAlert := "unreagent: " + runtimeWarn + uv + " sync in " + dir + ": exit 1 (the rest is in unreagent.log)\n"
			// The bridge is checked before the services start, the runtime
			// prepared after: the bridge's warning comes first. Each copy is
			// waited for before the signal, so neither can be cut off.
			if tc.window {
				log := filepath.Join(dir, "unreagent.log")
				waitFileLine(t, log, bridgeWarn)
				waitFileLine(t, log, runtimeWarn)
				waitFileLine(t, log, tail)
				l.waitStderr(t, "unreagent: "+bridgeWarn)
				l.waitStderr(t, runtimeAlert)
			} else {
				l.waitLine(t, bridgeWarn)
				l.waitLine(t, runtimeWarn)
				l.waitLine(t, tail)
			}
			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			stderr := l.stderr.String()
			for _, alert := range []string{"unreagent: " + bridgeWarn, runtimeAlert} {
				alerts := strings.Count(stderr, alert)
				if tc.window && alerts != 1 {
					t.Errorf("stderr has %q %d times; want once:\n%s", alert, alerts, stderr)
				}
				if !tc.window && strings.Contains(stderr, "unreagent: WARN") {
					t.Errorf("stderr repeats a warning that the log on stdout already shows:\n%s", stderr)
				}
			}
			if strings.Contains(stderr, tail) {
				t.Errorf("stderr carries the output tail, which belongs in the log alone:\n%s", stderr)
			}
			if strings.Contains(stderr, runtimeInfo) {
				t.Errorf("stderr carries the preparation's info line, which belongs in the log alone:\n%s", stderr)
			}
		})
	}
}

// waitStderr waits until the launcher's stderr holds want.
func (l *launcherRun) waitStderr(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(l.stderr.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("stderr has no %q within 15s:\n%s", want, l.stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Main agent's decision on the k56/k79 review: alertWarnings gives alert
// the first line of a WARN and, when there are more, points to the log for
// them; the log keeps every line whole, and a line that is no WARN stays in
// the log alone. Without alert it is the log alone.
func TestAlertWarnings(t *testing.T) {
	lines := []string{
		"WARN MCP server 'toolkit': command 'node' not found (PATH)",
		"WARN Runtime: preparing Node failed — npm install in proj: exit 1\nnpm ERR! code E404\nnpm ERR! 404 Not Found",
		"Runtime: preparing Node (npm install) …",
		"[prepare:node] WARN job assign failed",
	}
	var log syncLog
	var alert bytes.Buffer
	logger := alertWarnings(log.log, &alert, filepath.Join("proj", "unreagent.log"))
	for _, line := range lines {
		logger(line)
	}
	if !slices.Equal(log.lines, lines) {
		t.Errorf("logged %q; want every line whole: %q", log.lines, lines)
	}
	want := "unreagent: WARN MCP server 'toolkit': command 'node' not found (PATH)\n" +
		"unreagent: WARN Runtime: preparing Node failed — npm install in proj: exit 1 (the rest is in " + filepath.Join("proj", "unreagent.log") + ")\n"
	if got := alert.String(); got != want {
		t.Errorf("alert got\n%s\nwant\n%s", got, want)
	}

	var only syncLog
	alertWarnings(only.log, nil, "")(lines[1])
	if !slices.Equal(only.lines, lines[1:2]) {
		t.Errorf("without alert, logged %q; want %q", only.lines, lines[1:2])
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
		// the word: a usage error since k159.
		{"bool flag before", []string{"-no-agent", "hermes-setup"}, 2, "Usage of hermes-setup"},
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

// runCommandLine runs the launcher with args after the harness's -config, on
// a config whose editor is a markerScript, for a command line that ends on
// its own. It fails the test if the editor started or anything in the project
// changed, and returns the exit code, stdout and stderr.
func runCommandLine(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte(markerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := launcherYAML(editor, "  enabled: false\n")
	before := dirState(t, dir)
	before["unreagent.yaml"] = yaml // startLauncherIn writes it
	l := startLauncherIn(t, dir, yaml, args...)
	code = l.exitCode(t)
	stdout, stderr = l.stdout.String(), l.stderr.String()
	if _, err := os.Stat(editor + ".ran"); err == nil {
		t.Errorf("the editor was started:\n%s%s", stdout, stderr)
	}
	if diff := stateDiff(before, dirState(t, dir)); len(diff) > 0 {
		t.Errorf("wrote into the project: %s\n%s%s", strings.Join(diff, ", "), stdout, stderr)
	}
	return code, stdout, stderr
}

// k159: a bad flag for hermes-setup, or a word among its arguments, is a
// usage error like a bad launcher flag or an unknown sub-command: exit 2, the
// message on stderr once, and nothing read or written. A bad flag used to be
// printed by hermes-setup's flag set and again by main, with exit 1; a word
// was ignored and hermes-setup ran. The harness's -config comes first.
func TestRunHermesSetupUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		once  string // on stderr exactly once
		usage bool   // hermes-setup's usage follows it, as after a bad launcher flag
	}{
		{"unknown flag after the word", []string{"hermes-setup", "-nosuch"}, "flag provided but not defined: -nosuch", true},
		{"launcher flag before the word", []string{"-no-agent", "hermes-setup"}, "flag provided but not defined: -no-agent", true},
		{"flag without its value", []string{"hermes-setup", "-config"}, "flag needs an argument: -config", true},
		{"a word after its flags", []string{"hermes-setup", "extra"}, `hermes-setup takes flags only, not "extra" (unreagent hermes-setup -h lists them)`, false},
		{"a word before a flag", []string{"hermes-setup", "extra", "-config", "x"}, `hermes-setup takes flags only, not "extra"`, false},
		{"a flag after --", []string{"hermes-setup", "--", "-config", "x"}, `hermes-setup takes flags only, not "-config"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runCommandLine(t, tc.args...)
			if code != 2 {
				t.Errorf("exit code %d, want 2:\n%s%s", code, stdout, stderr)
			}
			if n := strings.Count(stderr, tc.once); n != 1 {
				t.Errorf("stderr holds %q %d times, want once:\n%s", tc.once, n, stderr)
			}
			if got := strings.Contains(stderr, "Usage of hermes-setup:"); got != tc.usage {
				t.Errorf("hermes-setup's usage on stderr: %v, want %v:\n%s", got, tc.usage, stderr)
			}
			if stdout != "" {
				t.Errorf("hermes-setup ran, stdout:\n%s", stdout)
			}
		})
	}
}

// k159: -h asks for the usage; it is no error. The launcher's and
// hermes-setup's print it on stderr and exit 0; hermes-setup's used to exit 1
// with "Error: flag: help requested". The launcher's names the sub-commands.
func TestRunHelpExitsZero(t *testing.T) {
	sub := regexp.MustCompile(`(?m)^  hermes-setup +` + regexp.QuoteMeta(subcommands["hermes-setup"].summary) + `$`)
	for _, tc := range []struct {
		name string
		args []string
		want string // on stderr
	}{
		{"launcher -h", []string{"-h"}, "\nFlags:\n"},
		{"launcher --help after a flag", []string{"-no-agent", "--help"}, "\nFlags:\n"},
		{"hermes-setup -h", []string{"hermes-setup", "-h"}, "Usage of hermes-setup:\n  -config string\n"},
		{"hermes-setup --help", []string{"hermes-setup", "--help"}, "Usage of hermes-setup:\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runCommandLine(t, tc.args...)
			if code != 0 {
				t.Errorf("exit code %d, want 0:\n%s%s", code, stdout, stderr)
			}
			if !strings.Contains(stderr, tc.want) || strings.Contains(stderr, "Error:") {
				t.Errorf("stderr does not hold %q, or holds an error:\n%s", tc.want, stderr)
			}
			if launcher := tc.args[0] != "hermes-setup"; launcher != sub.MatchString(stderr) {
				t.Errorf("hermes-setup's line in the launcher's usage: %v, want %v:\n%s", !launcher, launcher, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout:\n%s", stdout)
			}
		})
	}
}

// k159: the launcher's -h names every sub-command on a line of its own with
// its one-line summary, after the call forms and before the flags.
func TestPrintUsageNamesTheSubcommands(t *testing.T) {
	set := flag.NewFlagSet("unreagent", flag.ContinueOnError)
	var buf bytes.Buffer
	set.SetOutput(&buf)
	set.Bool("no-agent", false, "Don't start the agent")
	printUsage(set)
	out := buf.String()
	subs, flags := strings.Index(out, "\nSub-commands"), strings.Index(out, "\nFlags:\n")
	if !strings.HasPrefix(out, "Usage:\n") || subs < 0 || flags < subs || !strings.Contains(out[flags:], "  -no-agent\n") {
		t.Fatalf("want the call forms, the sub-commands, then the flags:\n%s", out)
	}
	for name, sc := range subcommands {
		if sc.summary == "" || strings.Contains(sc.summary, "\n") {
			t.Errorf("%s: summary %q, want one line", name, sc.summary)
		}
		line := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + ` +` + regexp.QuoteMeta(sc.summary) + `$`)
		if !line.MatchString(out[subs:flags]) {
			t.Errorf("no line %q among the sub-commands:\n%s", "  "+name+"  "+sc.summary, out)
		}
	}
}
