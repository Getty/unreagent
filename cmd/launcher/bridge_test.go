package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// syncLog is a logger that may be called from several goroutines (the
// supervisor and the bridge preparation both log).
type syncLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *syncLog) log(s string) {
	l.mu.Lock()
	l.lines = append(l.lines, s)
	l.mu.Unlock()
}

func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// scripts writes the fake npm and bridges of one test. The scripts sleep for a
// minute, far past any bound a test waits for; each appends its PID to a file
// so the test's cleanup kills whatever it left running — a failing test, or a
// grandchild that no job object ends on Linux.
type scripts struct {
	t    *testing.T
	pids string
}

func newScripts(t *testing.T) *scripts {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake npm / bridge are shell scripts")
	}
	s := &scripts{t: t, pids: filepath.Join(t.TempDir(), "pids")}
	t.Cleanup(func() {
		b, err := os.ReadFile(s.pids)
		if err != nil {
			return
		}
		for _, field := range strings.Fields(string(b)) {
			if pid, err := strconv.Atoi(field); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	})
	return s
}

// write writes an executable shell script: body, then a sleep that keeps the
// script's PID (exec). In body, "$PIDS" is the file to append further PIDs to.
func (s *scripts) write(path, body string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.t.Fatalf("mkdir: %v", err)
	}
	script := "#!/bin/sh\nPIDS='" + s.pids + "'\necho $$ >> \"$PIDS\"\n" + body + "exec sleep 60\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		s.t.Fatalf("write %s: %v", path, err)
	}
}

// runBridges runs prepareMCPBridges in the background and returns a channel
// that is closed when it is done.
func runBridges(ctx context.Context, cfg *config.Config, dir string, log *syncLog) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		prepareMCPBridges(ctx, supervisor.New(log.log), cfg, dir, log.log)
	}()
	return done
}

func waitForFile(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not appear within %s", path, within)
}

func bridgeConfig(name string, def map[string]interface{}) *config.Config {
	cfg := &config.Config{}
	cfg.MCP.Enabled = true
	cfg.MCP.ExtraServers = map[string]map[string]interface{}{name: def}
	return cfg
}

// nodeBridge sets up a node bridge whose node_modules is missing, so its
// preparation runs npm install first: bin/node (the base name "node" is what
// marks a node bridge) touches ran if it is ever started, bin/npm runs npmBody.
func nodeBridge(t *testing.T, s *scripts, dir, ran, npmBody string) *config.Config {
	t.Helper()
	s.write(filepath.Join(dir, "bin", "npm"), npmBody)
	s.write(filepath.Join(dir, "bin", "node"), "touch '"+ran+"'\n")
	if err := os.MkdirAll(filepath.Join(dir, "bridge"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.js", "package.json"} {
		if err := os.WriteFile(filepath.Join(dir, "bridge", f), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := bridgeConfig("toolkit", map[string]interface{}{
		"command": filepath.Join(dir, "bin", "node"),
		"args":    []interface{}{"bridge/index.js"},
	})
	cfg.Runtimes.Node.Npm = filepath.Join(dir, "bin", "npm")
	return cfg
}

// k54: a node bridge whose node_modules is missing gets an npm install before
// the services start, and that install had no deadline: on a machine with a
// stalled registry the editor was never started and no MCP server listened.
// It must give up after bridgeInstallTimeout, say so, and leave the bridge out
// — its smoke test would only fail on the missing modules.
func TestPrepareMCPBridgesNpmInstallTimesOut(t *testing.T) {
	s := newScripts(t)
	dir := t.TempDir()
	ran := filepath.Join(dir, "node-ran")
	cfg := nodeBridge(t, s, dir, ran, "")

	old := bridgeInstallTimeout
	bridgeInstallTimeout = 300 * time.Millisecond
	defer func() { bridgeInstallTimeout = old }()

	var log syncLog
	select {
	case <-runBridges(context.Background(), cfg, dir, &log):
	case <-time.After(5 * time.Second):
		t.Fatalf("bridge preparation still running 5s in, with an install timeout of %s — npm (sleeps 60s) was not stopped:\n%s",
			bridgeInstallTimeout, log.String())
	}
	if !strings.Contains(log.String(), "timed out") {
		t.Fatalf("the install timeout was not reported:\n%s", log.String())
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("the bridge was smoke-tested although its npm install did not finish")
	}
}

// k68: Ctrl-C during the npm install of a node bridge must stop the install
// and say the launcher is shutting down — not warn that npm failed, which
// sends the user hunting for a registry problem that is not there.
func TestPrepareMCPBridgesNpmInstallStopsOnCancel(t *testing.T) {
	s := newScripts(t)
	dir := t.TempDir()
	ready := filepath.Join(dir, "npm-ready")
	cfg := nodeBridge(t, s, dir, filepath.Join(dir, "node-ran"), "touch '"+ready+"'\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncLog
	done := runBridges(ctx, cfg, dir, &log)
	waitForFile(t, ready, 10*time.Second)

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("bridge preparation still running 3s after the cancel — npm install ignores the signal context:\n%s", log.String())
	}
	if strings.Contains(log.String(), "WARN") || !strings.Contains(log.String(), "npm install aborted") {
		t.Fatalf("a cancelled install must be logged as an abort, not as a failing npm:\n%s", log.String())
	}
}

// k68: Ctrl-C during the bridge preparation did nothing until the 20s smoke
// test ran out — prepareMCPBridges never saw the signal context. It must stop
// the running bridge and return as soon as the context is cancelled, and not
// go on to watch for the in-editor server of a bridge it gave up on.
func TestPrepareMCPBridgesStopsOnCancel(t *testing.T) {
	s := newScripts(t)
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	bridge := filepath.Join(dir, "bridge.sh")
	s.write(bridge, "touch \"$READY\"\n")
	cfg := bridgeConfig("hung", map[string]interface{}{
		"command": bridge,
		"env": map[string]interface{}{
			"READY": ready,
			// Not a URL: waitForEndpoint warns about it the moment it starts.
			"UNREAL_MCP_URL": "not a url",
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncLog
	done := runBridges(ctx, cfg, dir, &log)
	waitForFile(t, ready, 10*time.Second)

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("bridge preparation still running 3s after the cancel — the smoke test ignores the signal context:\n%s", log.String())
	}
	// waitForEndpoint runs in its own goroutine; started, it logs at once.
	time.Sleep(200 * time.Millisecond)
	// The user stopped the launcher; the bridge did nothing wrong.
	if strings.Contains(log.String(), "WARN") || !strings.Contains(log.String(), "smoke test aborted") {
		t.Fatalf("a cancel must be logged as an abort, not as a failing bridge:\n%s", log.String())
	}
}

// k68: a context that is already cancelled — the signal came during an
// earlier bridge — must not start the next bridge at all. Started anyway, it
// is killed at once by the cancel path, so only the log can tell: it must
// stay empty.
func TestPrepareMCPBridgesCancelledStartsNothing(t *testing.T) {
	s := newScripts(t)
	dir := t.TempDir()
	bridge := filepath.Join(dir, "bridge.sh")
	s.write(bridge, "")
	cfg := bridgeConfig("next", map[string]interface{}{"command": bridge})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var log syncLog
	select {
	case <-runBridges(ctx, cfg, dir, &log):
	case <-time.After(3 * time.Second):
		t.Fatalf("bridge preparation ran on with a cancelled context:\n%s", log.String())
	}
	if out := log.String(); out != "" {
		t.Fatalf("a bridge was started although the context was already cancelled:\n%s", out)
	}
}

// k55, the part Linux can show: the smoke test used to kill only the direct
// child and then wait for its output pipes, which a grandchild still held — a
// bridge that forks a worker kept the launcher in the smoke test for the
// worker's whole lifetime. On Windows the job object now ends that grandchild
// (unverified from Linux); everywhere the wait for the pipes is bounded.
func TestSmokeTestMCPReturnsWhileGrandchildHoldsOutput(t *testing.T) {
	s := newScripts(t)
	dir := t.TempDir()
	bridge := filepath.Join(dir, "bridge.sh")
	s.write(bridge, `sleep 60 &
echo $! >> "$PIDS"
read line
echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"fake","version":"0"}}}'
`)

	var log syncLog
	done := make(chan struct{})
	go func() {
		defer close(done)
		smokeTestMCP(context.Background(), "forking", bridge, nil, nil, dir, log.log)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("smoke test still waiting 10s in — it waits for the grandchild (sleeps 60s) holding its output:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "smoke test OK") {
		t.Fatalf("expected 'smoke test OK', got:\n%s", log.String())
	}
}
