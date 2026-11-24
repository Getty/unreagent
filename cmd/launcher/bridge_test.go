package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// k106 fixtures. initAnswer is a bridge that answers MCP initialize;
// brokenBridge one that dies like node on a missing module. npmOK installs
// and succeeds; npmOffline fails like npm without a registry, after leaving
// a partial node_modules behind — with an error code, so it gets no retry
// with --legacy-peer-deps (k215).
const (
	initAnswer = `read line
echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"fake","version":"0"}}}'
`
	brokenBridge = "echo \"Error: Cannot find module 'dep'\" >&2\nexit 1\n"
	npmOK        = "mkdir -p node_modules/dep\nexit 0\n"
	npmOffline   = "mkdir -p node_modules/half\necho 'npm error code ENOTFOUND' >&2\necho 'npm error network request failed' >&2\nexit 1\n"
)

// installFixture is a node bridge (nodeBridge) whose npm touches npmRan and
// whose node touches nodeRan before running their bodies.
type installFixture struct {
	cfg                      *config.Config
	dir, modules             string
	npmRan, nodeRan, earlier string
}

func newInstallFixture(t *testing.T, npmBody, nodeBody string) *installFixture {
	t.Helper()
	s := newScripts(t)
	dir := t.TempDir()
	f := &installFixture{
		dir:     dir,
		modules: filepath.Join(dir, "bridge", "node_modules"),
		npmRan:  filepath.Join(dir, "npm-ran"),
		nodeRan: filepath.Join(dir, "node-ran"),
		earlier: filepath.Join(dir, "bridge", "node_modules", "dep", "index.js"),
	}
	f.cfg = nodeBridge(t, s, dir, f.nodeRan, "touch '"+f.npmRan+"'\n"+npmBody)
	s.write(filepath.Join(dir, "bin", "node"), "touch '"+f.nodeRan+"'\n"+nodeBody)
	return f
}

// withModules puts a node_modules from an earlier run in place, with the
// completion marker or without it.
func (f *installFixture) withModules(t *testing.T, marked bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.earlier), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.earlier, []byte("module.exports = {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if marked {
		if err := os.WriteFile(filepath.Join(f.modules, bridgeInstallMarker), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// run prepares the bridges and returns the log.
func (f *installFixture) run(t *testing.T) string {
	t.Helper()
	var log syncLog
	select {
	case <-runBridges(context.Background(), f.cfg, f.dir, &log):
	case <-time.After(15 * time.Second):
		t.Fatalf("bridge preparation still running 15s in:\n%s", log.String())
	}
	return log.String()
}

func (f *installFixture) marked() bool {
	return fileExists(filepath.Join(f.modules, bridgeInstallMarker))
}

// k106: an install cut short (timeout, Ctrl-C) leaves a partial node_modules,
// and the next start skipped the install because the directory existed. A
// successful install now leaves a marker, and node_modules without it is
// installed again — over what is there, nothing deleted.
func TestPrepareNodeBridgeReinstallsWithoutMarker(t *testing.T) {
	f := newInstallFixture(t, npmOK, initAnswer)
	f.withModules(t, false)
	log := f.run(t)
	if !fileExists(f.npmRan) {
		t.Fatalf("node_modules without the marker did not get an npm install:\n%s", log)
	}
	if !f.marked() {
		t.Fatalf("a successful npm install left no marker:\n%s", log)
	}
	if !fileExists(f.earlier) {
		t.Fatal("the node_modules already there lost a file — the install must not delete anything")
	}
	if !strings.Contains(log, "smoke test OK") {
		t.Fatalf("the bridge was not smoke-tested after the install:\n%s", log)
	}
}

// k106: the first install of a bridge leaves the marker, so the next start
// does not install again.
func TestPrepareNodeBridgeMarksFreshInstall(t *testing.T) {
	f := newInstallFixture(t, npmOK, initAnswer)
	log := f.run(t)
	if !fileExists(f.npmRan) || !f.marked() {
		t.Fatalf("a fresh install must run npm and leave the marker (npm ran: %v, marker: %v):\n%s",
			fileExists(f.npmRan), f.marked(), log)
	}
}

// k106: node_modules with the marker is complete — no npm install. k135: and
// a bridge that passes its smoke test on it gets no repair hint, nor any
// other WARN, on every start.
func TestPrepareNodeBridgeSkipsInstallWithMarker(t *testing.T) {
	f := newInstallFixture(t, npmOffline, initAnswer)
	f.withModules(t, true)
	log := f.run(t)
	if fileExists(f.npmRan) {
		t.Fatalf("npm install ran although node_modules carries the marker:\n%s", log)
	}
	if !strings.Contains(log, "smoke test OK") {
		t.Fatalf("expected 'smoke test OK', got:\n%s", log)
	}
	if strings.Contains(log, "WARN") {
		t.Fatalf("a marked node_modules whose bridge passed its smoke test got a WARN:\n%s", log)
	}
}

// k106: a complete node_modules from before the marker existed, and no
// registry. The failed re-install must not cost the bridge: the smoke test
// decides, and a bridge that answers gets the marker, so the next start
// leaves npm alone.
func TestPrepareNodeBridgeOfflineKeepsEarlierModules(t *testing.T) {
	f := newInstallFixture(t, npmOffline, initAnswer)
	f.withModules(t, false)
	log := f.run(t)
	if !fileExists(f.npmRan) {
		t.Fatalf("node_modules without the marker did not get an npm install:\n%s", log)
	}
	if !strings.Contains(log, "smoke test OK") {
		t.Fatalf("a failed re-install over an earlier node_modules must leave the decision to the smoke test:\n%s", log)
	}
	if !f.marked() {
		t.Fatalf("the smoke test passed, but node_modules got no marker:\n%s", log)
	}
	if !fileExists(f.earlier) {
		t.Fatal("the node_modules already there lost a file — the install must not delete anything")
	}
}

// k106: the same, but the earlier node_modules is the partial one: the smoke
// test fails, says why, and no marker is written — the next start installs
// again.
func TestPrepareNodeBridgeOfflinePartialModulesStayUnmarked(t *testing.T) {
	f := newInstallFixture(t, npmOffline, brokenBridge)
	f.withModules(t, false)
	log := f.run(t)
	if !fileExists(f.npmRan) {
		t.Fatalf("node_modules without the marker did not get an npm install:\n%s", log)
	}
	if !strings.Contains(log, "smoke test failed") || !strings.Contains(log, "Cannot find module") {
		t.Fatalf("expected the smoke test to fail with the bridge's stderr:\n%s", log)
	}
	if f.marked() {
		t.Fatalf("node_modules got the marker although the install failed and the smoke test too:\n%s", log)
	}
}

// k106: a first install that fails leaves no marker, even though npm left a
// node_modules behind, and the bridge is not smoke-tested (as before k106).
func TestPrepareNodeBridgeFailedFreshInstallStaysUnmarked(t *testing.T) {
	f := newInstallFixture(t, npmOffline, initAnswer)
	log := f.run(t)
	if !strings.Contains(log, "npm install --no-save exit 1") || !strings.Contains(log, "bridge will not start") {
		t.Fatalf("expected the failed install to be reported:\n%s", log)
	}
	if f.marked() {
		t.Fatalf("a failed install left the marker:\n%s", log)
	}
	if fileExists(f.nodeRan) {
		t.Fatalf("the bridge was smoke-tested although its first install failed:\n%s", log)
	}
}

// The two forms of the repair hint after a failed smoke test over a
// node_modules that was there before this start. Unmarked: npm install ran
// and can call a partial tree up to date, so node_modules itself has to go.
// Marked: no install ran, so deleting the marker brings one first.
func unmarkedHint(modules string) string {
	return "WARN MCP server 'toolkit': if the error above names a missing module or file, delete " + modules +
		" and start again — that node_modules was there before this start, and npm install can report a partial one as up to date."
}

func markedHint(modules string) string {
	return "WARN MCP server 'toolkit': if the error above names a missing module or file, delete " + filepath.Join(modules, bridgeInstallMarker) +
		" and start again — the next start runs npm install; if that does not help, delete " + modules + "."
}

// k106 review, finding 1: real npm calls a node_modules up to date whose
// package was cut short mid-extraction (package.json there, index.js not).
// Marked after that "successful" install, it was never installed again and
// the bridge failed on every start. Only a passing smoke test may write the
// marker, and a failing one over an earlier node_modules names the sure fix.
func TestPrepareNodeBridgeUpToDateButBrokenStaysUnmarked(t *testing.T) {
	f := newInstallFixture(t, "echo 'up to date, audited 2 packages in 150ms'\nexit 0\n", brokenBridge)
	f.withModules(t, false)
	log := f.run(t)
	if !fileExists(f.npmRan) || !strings.Contains(log, "smoke test failed") {
		t.Fatalf("expected an npm install and a failing smoke test:\n%s", log)
	}
	if f.marked() {
		t.Fatalf("npm said up to date but the bridge failed its smoke test — the marker locks that tree in:\n%s", log)
	}
	if !strings.Contains(log, unmarkedHint(f.modules)) {
		t.Fatalf("the log does not name the sure fix (%q):\n%s", unmarkedHint(f.modules), log)
	}
}

// k106: after a first install that npm calls a success, a failing smoke test
// leaves no marker either. No node_modules was there before this start, so
// there is nothing for the user to delete yet: no repair hint.
func TestPrepareNodeBridgeFreshInstallBrokenStaysUnmarked(t *testing.T) {
	f := newInstallFixture(t, npmOK, brokenBridge)
	log := f.run(t)
	if f.marked() {
		t.Fatalf("the bridge failed its smoke test but got the marker:\n%s", log)
	}
	if strings.Contains(log, "and start again") {
		t.Fatalf("repair hint for a node_modules this start installed:\n%s", log)
	}
}

// k106: a marked node_modules skips the install; when the bridge then fails
// (a plugin update that needs a new module, say), no npm install ran this
// start, so the hint names the marker first — deleting it brings the
// install — and node_modules only as the fallback.
func TestPrepareNodeBridgeMarkedButBrokenNamesRepair(t *testing.T) {
	f := newInstallFixture(t, npmOK, brokenBridge)
	f.withModules(t, true)
	log := f.run(t)
	if fileExists(f.npmRan) {
		t.Fatalf("npm install ran although node_modules carries the marker:\n%s", log)
	}
	if !strings.Contains(log, markedHint(f.modules)) {
		t.Fatalf("the log does not name the marker first (%q):\n%s", markedHint(f.modules), log)
	}
	if strings.Contains(log, "npm install can report a partial one") {
		t.Fatalf("no npm install ran this start, so it is not the reason:\n%s", log)
	}
}

// k106: Ctrl-C during the smoke test over an earlier node_modules is a
// shutdown, not a failing bridge: no repair hint, no WARN.
func TestPrepareNodeBridgeCancelledSmokeTestNamesNoRepair(t *testing.T) {
	f := newInstallFixture(t, npmOK, "") // the bridge never answers
	f.withModules(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncLog
	done := runBridges(ctx, f.cfg, f.dir, &log)
	waitForFile(t, f.nodeRan, 10*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("bridge preparation still running 3s after the cancel:\n%s", log.String())
	}
	if strings.Contains(log.String(), "WARN") || !strings.Contains(log.String(), "smoke test aborted") {
		t.Fatalf("a cancelled smoke test must be logged as an abort, without a repair hint:\n%s", log.String())
	}
}

// k106 review, finding 3: a timed-out install over an earlier node_modules is
// a failed install like any other — the folder is kept, the smoke test
// decides, a pass writes the marker.
func TestPrepareNodeBridgeTimeoutOverEarlierModules(t *testing.T) {
	f := newInstallFixture(t, "", initAnswer) // npm sleeps 60s
	f.withModules(t, false)
	old := bridgeInstallTimeout
	bridgeInstallTimeout = 300 * time.Millisecond
	defer func() { bridgeInstallTimeout = old }()
	log := f.run(t)
	if !strings.Contains(log, "timed out") || !strings.Contains(log, "smoke test OK") {
		t.Fatalf("expected the timeout, then a smoke test over the node_modules already there:\n%s", log)
	}
	if !f.marked() {
		t.Fatalf("the smoke test passed, but node_modules got no marker:\n%s", log)
	}
}

// k106 review, finding 3: a marker that cannot be written is said so — the
// next start installs again — and does not cost the bridge.
func TestPrepareNodeBridgeMarkerWriteFails(t *testing.T) {
	f := newInstallFixture(t, npmOK, initAnswer)
	f.withModules(t, false)
	if err := os.Chmod(f.modules, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.modules, 0o755) })
	if probe := filepath.Join(f.modules, "probe"); os.WriteFile(probe, nil, 0o644) == nil {
		t.Skip("a read-only directory is writable here (root?) — the marker write cannot be made to fail")
	}
	log := f.run(t)
	if !strings.Contains(log, "WARN MCP server 'toolkit': could not mark node_modules as installed") {
		t.Fatalf("a failed marker write was not reported:\n%s", log)
	}
	if !strings.Contains(log, "smoke test OK") || f.marked() {
		t.Fatalf("expected a passing smoke test and no marker:\n%s", log)
	}
}

// k106 review, finding 3: npm installs a package without dependencies
// without creating node_modules; the marker creates it, or every start would
// install again.
func TestPrepareNodeBridgeWithoutDependencies(t *testing.T) {
	f := newInstallFixture(t, "exit 0\n", initAnswer)
	log := f.run(t)
	if !strings.Contains(log, "smoke test OK") || !f.marked() {
		t.Fatalf("a package without dependencies must end up marked (marker: %v):\n%s", f.marked(), log)
	}
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

// k135: node 24's report of a missing module, as node 24.19.0 prints it on
// Linux for a bridge whose index.js requires 'dep' (captured, the path
// replaced). The line that names the error is the 5th of 24, so the last 15
// lines are the stack trace and the error's properties alone.
const nodeMissingModule = `node:internal/modules/cjs/loader:1520
  throw err;
  ^

Error: Cannot find module 'dep'
Require stack:
- /opt/toolkit/bridge/index.js
    at Module._resolveFilename (node:internal/modules/cjs/loader:1517:15)
    at wrapResolveFilename (node:internal/modules/cjs/loader:1071:27)
    at defaultResolveImplForCJSLoading (node:internal/modules/cjs/loader:1095:10)
    at resolveForCJSWithHooks (node:internal/modules/cjs/loader:1122:12)
    at Module._load (node:internal/modules/cjs/loader:1294:5)
    at wrapModuleLoad (node:internal/modules/cjs/loader:255:19)
    at Module.require (node:internal/modules/cjs/loader:1617:12)
    at require (node:internal/modules/helpers:153:16)
    at Object.<anonymous> (/opt/toolkit/bridge/index.js:1:13)
    at Module._compile (node:internal/modules/cjs/loader:1872:14) {
  code: 'MODULE_NOT_FOUND',
  requireStack: [
    '/opt/toolkit/bridge/index.js'
  ]
}

Node.js v24.19.0
`

// k135: the smoke test showed only the last 15 lines of a failing bridge's
// stderr. For node's missing-module error that cut "Error: Cannot find module
// 'dep'" itself, and the repair hint's "if the error above names a missing
// module" had only the code field left to go by. The line that names the
// error now leads the tail, which stays 15 lines.
func TestSmokeTestMCPKeepsNodeErrorLine(t *testing.T) {
	s := newScripts(t)
	dir := t.TempDir()
	trace := filepath.Join(dir, "stderr")
	if err := os.WriteFile(trace, []byte(nodeMissingModule), 0o644); err != nil {
		t.Fatal(err)
	}
	bridge := filepath.Join(dir, "bridge.sh")
	s.write(bridge, "cat '"+trace+"' >&2\nexit 1\n")

	var log syncLog
	if smokeTestMCP(context.Background(), "toolkit", bridge, nil, nil, dir, log.log) {
		t.Fatalf("a bridge that dies on a missing module passed its smoke test:\n%s", log.String())
	}
	lines := strings.Split(strings.TrimSpace(nodeMissingModule), "\n")
	want := "WARN MCP server 'toolkit': smoke test failed (no initialize response):\n" +
		"Error: Cannot find module 'dep'\n[...]\n" + strings.Join(lines[len(lines)-15:], "\n")
	if got := log.String(); got != want {
		t.Fatalf("smoke test log:\n%s\n\nwant:\n%s", got, want)
	}
}

// k135: which error line leads the tail. nodeThrowWithCause is node 24.19.0
// on Linux for a bridge that logs an "Error: …" line of its own and then
// throws a TypeError with a cause (captured, the path replaced): the crash's
// line is the last unindented one, the nested cause is indented.
const nodeThrowWithCause = `Error: config not found, using defaults
/opt/toolkit/bridge/index.js:3
throw new TypeError('outer', { cause: new Error('inner') });
^

TypeError: outer
    at Object.<anonymous> (/opt/toolkit/bridge/index.js:3:7)
    at Module._compile (node:internal/modules/cjs/loader:1872:14)
    ... 5 lines matching cause stack trace ...
    at node:internal/main/run_main_module:33:47 {
  [cause]: Error: inner
      at Object.<anonymous> (/opt/toolkit/bridge/index.js:3:39)
      at Module._compile (node:internal/modules/cjs/loader:1872:14)
      at Object..js (node:internal/modules/cjs/loader:2003:10)
      at Module.load (node:internal/modules/cjs/loader:1594:32)
      at Module._load (node:internal/modules/cjs/loader:1396:12)
      at wrapModuleLoad (node:internal/modules/cjs/loader:255:19)
      at Module.executeUserEntryPoint [as runMain] (node:internal/modules/run_main:154:5)
      at node:internal/main/run_main_module:33:47
}

Node.js v24.19.0
`

// k135: nodeFetchRejection is node 24.19.0 on Linux for a bridge whose fetch
// is rejected and never handled (captured, the path replaced): node prints
// that error in brackets.
const nodeFetchRejection = `node:internal/process/promises:394
    triggerUncaughtException(err, true /* fromPromise */);
    ^

[TypeError: fetch failed] {
  [cause]: Error: bad port
      at makeNetworkError (node:internal/deps/undici/undici:11806:35)
      at mainFetch (node:internal/deps/undici/undici:13189:22)
      at fetching (node:internal/deps/undici/undici:13176:7)
      at fetch (node:internal/deps/undici/undici:13040:20)
      at fetch (node:internal/deps/undici/undici:17708:10)
      at fetch (node:internal/bootstrap/web/exposed-window-or-worker:83:12)
      at Object.<anonymous> (/opt/toolkit/bridge/index.js:1:1)
      at Module._compile (node:internal/modules/cjs/loader:1872:14)
      at Object..js (node:internal/modules/cjs/loader:2003:10)
      at Module.load (node:internal/modules/cjs/loader:1594:32)
}

Node.js v24.19.0
`

func TestStderrTail(t *testing.T) {
	last := func(s string, n int) string {
		lines := strings.Split(strings.TrimSpace(s), "\n")
		return strings.Join(lines[len(lines)-n:], "\n")
	}
	for _, c := range []struct{ name, in, want string }{
		{"shorter than n: all of it", "Error: boom\n    at f", "Error: boom\n    at f"},
		{"error line in the tail: the tail alone",
			"log\nError: boom\n    at f\n    at g", "Error: boom\n    at f\n    at g"},
		{"error line right above the tail: no gap to mark",
			"Error: boom\n    at f\n    at g\n    at h", "Error: boom\n    at f\n    at g\n    at h"},
		{"error line further up: it, the marker, the tail",
			"Error: boom\n    at e\n    at f\n    at g\n    at h", "Error: boom\n[...]\n    at f\n    at g\n    at h"},
		{"node's [CODE] form",
			"Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'dep'\n    at e\n    at f\n    at g\n    at h",
			"Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'dep'\n[...]\n    at f\n    at g\n    at h"},
		{"no error line: the tail alone", "one\ntwo\nthree\nfour\nfive", "three\nfour\nfive"},
		{"a log line naming Error is no error line",
			"[bridge] Error: retrying\nWARN Error: x\none\ntwo\nthree", "one\ntwo\nthree"},
		{"the crash, not the bridge's earlier log line nor the nested cause",
			nodeThrowWithCause, "TypeError: outer\n[...]\n" + last(nodeThrowWithCause, 3)},
		{"the last error line in the tail: no earlier one put on top",
			"Error: early\nlog\nError: boom\n    at f\n    at g", "Error: boom\n    at f\n    at g"},
		{"node's source-location header is no error line",
			"node:internal/modules/cjs/loader:1520\n  throw err;\n  ^\none\ntwo\nthree", "one\ntwo\nthree"},
		{"a name that does not end in Error is no error line",
			"Errors: 3\nlog\none\ntwo\nthree", "one\ntwo\nthree"},
		{"a drive-letter colon in the message: the first colon ends the name",
			`Error: Cannot find module 'C:\bridge\node_modules\dep\index.js'` + "\n    at e\n    at f\n    at g\n    at h",
			`Error: Cannot find module 'C:\bridge\node_modules\dep\index.js'` + "\n[...]\n    at f\n    at g\n    at h"},
		{"node 24's bracketed form for an unhandled rejection",
			nodeFetchRejection, "[TypeError: fetch failed] {\n[...]\n" + last(nodeFetchRejection, 3)},
	} {
		if got := stderrTail(c.in, 3); got != c.want {
			t.Errorf("%s:\n%s\n\nwant:\n%s", c.name, got, c.want)
		}
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

// k56: the smoke test took any stdout line holding "serverInfo", or "result"
// next to "id":1, for the answer to initialize. A bridge's debug line naming
// serverInfo, or its reply to id 12, passed a bridge that never answered; an
// error response with id 1 left it waiting out the timeout. Each line is read
// as JSON-RPC now: only the result for id 1 whose serverInfo is an object
// passes, any other response to id 1 fails at once (no second one comes),
// every other line is read past.
func TestInitResponse(t *testing.T) {
	const (
		failed  = "initialize answered with error -32602: Unsupported protocol version"
		without = "initialize answered without serverInfo"
	)
	for _, c := range []struct {
		name, line string
		ok         bool
		failure    string
	}{
		{"the initialize result", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"toolkit","version":"1.0"}}}`, true, ""},
		{"an empty serverInfo is still one", `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{}}}`, true, ""},
		{"the error response", `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Unsupported protocol version"}}`, false, failed},
		{"a debug line naming serverInfo", `[bridge] waiting for "serverInfo" from the editor`, false, ""},
		{"a JSON debug line with id 1", `{"level":"debug","msg":"serverInfo pending","id":1}`, false, ""},
		{"the reply to id 12", `{"jsonrpc":"2.0","id":12,"result":{"serverInfo":{"name":"other"}}}`, false, ""},
		{"an error for another id", `{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"Method not found"}}`, false, ""},
		{"id 1 as a string", `{"jsonrpc":"2.0","id":"1","result":{"serverInfo":{"name":"x"}}}`, false, ""},
		{"space ahead of serverInfo's object", `{"jsonrpc":"2.0","id":1,"result":{"serverInfo": {"name":"x"}}}`, true, ""},
		{"a null error beside the result", `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"x"}},"error":null}`, true, ""},
		{"a result without serverInfo", `{"jsonrpc":"2.0","id":1,"result":{}}`, false, without},
		{"a null serverInfo", `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":null}}`, false, without},
		{"a null result", `{"jsonrpc":"2.0","id":1,"result":null}`, false, without},
		{"a result that is no object", `{"jsonrpc":"2.0","id":1,"result":"ok"}`, false, without},
		{"serverInfo as a string", `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":"toolkit 1.0"}}`, false, without},
		{"an error whose code is a string", `{"jsonrpc":"2.0","id":1,"error":{"code":"E_PROTO","message":"Unsupported protocol version"}}`,
			false, `initialize answered with error "E_PROTO": Unsupported protocol version`},
		{"an error whose code is a fraction", `{"jsonrpc":"2.0","id":1,"error":{"code":-32602.5,"message":"Unsupported protocol version"}}`,
			false, "initialize answered with error -32602.5: Unsupported protocol version"},
		{"an error without a code", `{"jsonrpc":"2.0","id":1,"error":{"message":"Unsupported protocol version"}}`,
			false, "initialize answered with error: Unsupported protocol version"},
		{"an error whose message is no string", `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":{"text":"bad"}}}`,
			false, `initialize answered with error -32602: {"text":"bad"}`},
		{"an error that is no object", `{"jsonrpc":"2.0","id":1,"error":"boom"}`, false, `initialize answered with error "boom"`},
		{"a request of the bridge's own with id 1", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, false, ""},
		{"a notification", `{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"serverInfo"}}`, false, ""},
		{"an empty line", ``, false, ""},
	} {
		ok, failure := initResponse([]byte(c.line))
		if ok != c.ok || failure != c.failure {
			t.Errorf("%s: initResponse(%s) = %v, %q; want %v, %q", c.name, c.line, ok, failure, c.ok, c.failure)
		}
	}
}

// k56 through smokeTestMCP: a bridge whose stdout carries lines the substring
// match took for the answer, and which then ends without answering, fails
// the smoke test; the same lines ahead of the answer are read past.
func TestSmokeTestMCPReadsOnlyTheInitializeAnswer(t *testing.T) {
	const noise = `read line
echo '[bridge] waiting for "serverInfo" from the editor'
echo '{"jsonrpc":"2.0","id":12,"result":{"serverInfo":{"name":"other"}}}'
`
	for _, c := range []struct {
		name, body, want string
		ok               bool
	}{
		{"noise, then the bridge ends", noise + "exit 0\n",
			"WARN MCP server 'toolkit': smoke test failed (no initialize response, no stderr)", false},
		{"noise, then the answer", noise + `echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"fake","version":"0"}}}'` + "\n",
			"MCP server 'toolkit': smoke test OK — bridge answers initialize.", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newScripts(t)
			dir := t.TempDir()
			bridge := filepath.Join(dir, "bridge.sh")
			s.write(bridge, c.body)
			var log syncLog
			if ok := smokeTestMCP(context.Background(), "toolkit", bridge, nil, nil, dir, log.log); ok != c.ok {
				t.Fatalf("smoke test passed = %v, want %v:\n%s", ok, c.ok, log.String())
			}
			if got := log.String(); got != c.want {
				t.Fatalf("smoke test log:\n%s\n\nwant:\n%s", got, c.want)
			}
		})
	}
}

// k56: an error response to initialize, or a result without serverInfo,
// failed the smoke test only once its timeout ran out, and the warning named
// the timeout instead of the answer. It fails at once now and names what
// came. The bridge keeps running after its answer, so nothing but that
// answer ends the wait before the timeout.
func TestSmokeTestMCPFailsOnAWrongAnswer(t *testing.T) {
	for _, c := range []struct{ name, answer, reason string }{
		{"an error response", `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Unsupported protocol version"}}`,
			"initialize answered with error -32602: Unsupported protocol version"},
		{"a result without serverInfo", `{"jsonrpc":"2.0","id":1,"result":{}}`,
			"initialize answered without serverInfo"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newScripts(t)
			dir := t.TempDir()
			bridge := filepath.Join(dir, "bridge.sh")
			s.write(bridge, "read line\necho '"+c.answer+"'\n")
			var log syncLog
			if smokeTestMCP(context.Background(), "toolkit", bridge, nil, nil, dir, log.log) {
				t.Fatalf("a bridge that answered %s passed its smoke test:\n%s", c.answer, log.String())
			}
			want := "WARN MCP server 'toolkit': smoke test failed (" + c.reason + ", no stderr)"
			if got := log.String(); got != want {
				t.Fatalf("smoke test log:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}

// shortenEndpointWait sets waitForEndpoint's window and pause for one test.
// waitForEndpoint reads both before it logs anything, so a test that has seen
// a watcher's line may restore them.
func shortenEndpointWait(t *testing.T, window, pause time.Duration) {
	t.Helper()
	oldWindow, oldPause := endpointWaitWindow, endpointPollInterval
	endpointWaitWindow, endpointPollInterval = window, pause
	t.Cleanup(func() { endpointWaitWindow, endpointPollInterval = oldWindow, oldPause })
}

// waitForLine waits until the log holds a line containing want.
func waitForLine(t *testing.T, log *syncLog, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if strings.Contains(log.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no line containing %q within %s:\n%s", want, within, log.String())
}

// prepared runs prepareMCPBridges to its return; the watchers it starts run on
// until ctx ends.
func prepared(t *testing.T, ctx context.Context, cfg *config.Config, log *syncLog) {
	t.Helper()
	select {
	case <-runBridges(ctx, cfg, t.TempDir(), log):
	case <-time.After(15 * time.Second):
		t.Fatalf("bridge preparation still running 15s in:\n%s", log.String())
	}
}

// k87: an http/sse entry (no command) was passed through without a look at
// its url, so nothing in the log said whether the server was up. Once the url
// answers, that is logged — once, with scheme, host and path only: the user
// info, query and headers of the entry can carry a token. The probe is a bare
// connect, so the server sees no request.
func TestPrepareMCPBridgesReportsRemoteAnswer(t *testing.T) {
	shortenEndpointWait(t, time.Minute, 10*time.Millisecond)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	cfg := bridgeConfig("unreal-mcp", map[string]interface{}{
		"type":    "http",
		"url":     "http://user:hunter2@" + host + "/mcp?key=s3cret",
		"headers": map[string]interface{}{"Authorization": "Bearer t0ken"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncLog
	prepared(t, ctx, cfg, &log)
	want := "MCP server 'unreal-mcp': " + srv.URL + "/mcp is reachable."
	waitForLine(t, &log, want, 5*time.Second)
	// Ten pauses: a watcher that went on polling would have logged again.
	time.Sleep(100 * time.Millisecond)

	out := log.String()
	if n := strings.Count(out, "is reachable"); n != 1 {
		t.Fatalf("the answer must be logged once, got %d times:\n%s", n, out)
	}
	for _, secret := range []string{"hunter2", "s3cret", "t0ken"} {
		if strings.Contains(out, secret) {
			t.Fatalf("the log carries %q from the entry:\n%s", secret, out)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("the probe sent %d HTTP request(s); it must only connect", n)
	}
}

// k87: a url that never answers gets one warning once the window is over,
// naming the entry and its url and saying how to get the agent to connect.
func TestPrepareMCPBridgesWarnsRemoteNeverAnswers(t *testing.T) {
	shortenEndpointWait(t, 300*time.Millisecond, 20*time.Millisecond)
	u := "http://" + freeLoopbackAddr(t) + "/mcp"
	cfg := bridgeConfig("unreal-mcp", map[string]interface{}{"type": "http", "url": u})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncLog
	prepared(t, ctx, cfg, &log)
	waitForLine(t, &log, "not reachable after 300ms", 5*time.Second)
	time.Sleep(100 * time.Millisecond)

	out := log.String()
	want := "WARN MCP server 'unreal-mcp': " + u + " not reachable after 300ms — "
	if strings.Contains(out, "\n") || !strings.HasPrefix(out, want) {
		t.Fatalf("want exactly one line starting %q, got:\n%s", want, out)
	}
	for _, hint := range []string{"may have marked the server failed", "/mcp in Claude Code reconnects it"} {
		if !strings.Contains(out, hint) {
			t.Fatalf("the warning lacks %q:\n%s", hint, out)
		}
	}
}

// k87: the watcher ends with the launcher's context, mid-window and without a
// warning — the user stopped the launcher, the server did nothing wrong.
func TestWaitForEndpointEndsWithContext(t *testing.T) {
	u := "http://" + freeLoopbackAddr(t) + "/mcp"
	ctx, cancel := context.WithCancel(context.Background())
	var log syncLog
	done := make(chan struct{})
	go func() {
		defer close(done)
		waitForEndpoint(ctx, "unreal-mcp", remoteEndpoint, u, nil, log.log)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("watcher still running 2s after the cancel (window %s):\n%s", endpointWaitWindow, log.String())
	}
	if out := log.String(); out != "" {
		t.Fatalf("a cancel must end the watcher silently, got:\n%s", out)
	}
}

// k87: the watch is for entries without a command. A stdio bridge next to an
// http entry still gets its smoke test, and no url watch of its own.
func TestPrepareMCPBridgesStdioEntryUnchanged(t *testing.T) {
	s := newScripts(t)
	shortenEndpointWait(t, 300*time.Millisecond, 20*time.Millisecond)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	bridge := filepath.Join(t.TempDir(), "bridge.sh")
	s.write(bridge, initAnswer)
	cfg := bridgeConfig("toolkit", map[string]interface{}{"command": bridge})
	cfg.MCP.ExtraServers["unreal-mcp"] = map[string]interface{}{"type": "http", "url": srv.URL + "/mcp"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncLog
	prepared(t, ctx, cfg, &log)
	waitForLine(t, &log, "MCP server 'unreal-mcp': "+srv.URL+"/mcp is reachable.", 5*time.Second)
	// Past the window: a watcher on the stdio entry would have warned by now.
	time.Sleep(400 * time.Millisecond)

	var toolkit []string
	for _, line := range strings.Split(log.String(), "\n") {
		if strings.Contains(line, "'toolkit'") {
			toolkit = append(toolkit, line)
		}
	}
	want := "MCP server 'toolkit': smoke test OK — bridge answers initialize."
	if len(toolkit) != 1 || toolkit[0] != want {
		t.Fatalf("the stdio entry must log only %q, got:\n%s", want, log.String())
	}
}

// k163: a stdio bridge's UNREAL_MCP_URL watch started inside the loop, right
// after that bridge's smoke test, so a later bridge's npm install (up to five
// minutes) used up the window before the editor had even been started, and
// the user got a false "not reachable". The watch now starts after the loop,
// as the http/sse one does (k87).
//
// Two node bridges, each with an UNREAL_MCP_URL that answers: the map gives no
// order, so whichever is prepared first is the one whose watch must wait for
// the other. Each npm install holds until the test releases it. A watch's
// first connect is the start of its window and is logged at once, so no
// "reachable" line may appear while the later install is held.
func TestPrepareMCPBridgesEditorWatchWaitsForLaterInstall(t *testing.T) {
	s := newScripts(t)
	shortenEndpointWait(t, time.Minute, 10*time.Millisecond)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	dir := t.TempDir()
	// npm runs in the bridge's directory.
	s.write(filepath.Join(dir, "bin", "npm"), "touch ready\nwhile [ ! -e release ]; do sleep 0.01; done\nexit 0\n")
	s.write(filepath.Join(dir, "bin", "node"), initAnswer)
	cfg := &config.Config{}
	cfg.MCP.Enabled = true
	cfg.MCP.ExtraServers = map[string]map[string]interface{}{}
	cfg.Runtimes.Node.Npm = filepath.Join(dir, "bin", "npm")
	names := []string{"alpha", "beta"}
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"index.js", "package.json"} {
			if err := os.WriteFile(filepath.Join(dir, name, f), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cfg.MCP.ExtraServers[name] = map[string]interface{}{
			"command": filepath.Join(dir, "bin", "node"),
			"args":    []interface{}{name + "/index.js"},
			"env":     map[string]interface{}{"UNREAL_MCP_URL": srv.URL + "/mcp"},
		}
	}
	file := func(name, f string) string { return filepath.Join(dir, name, f) }
	release := func(name string) {
		t.Helper()
		if err := os.WriteFile(file(name, "release"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncLog
	done := runBridges(ctx, cfg, dir, &log)

	first, later := "", ""
	for deadline := time.Now().Add(10 * time.Second); first == ""; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no npm install started within 10s:\n%s", log.String())
		}
		for i, name := range names {
			if fileExists(file(name, "ready")) {
				first, later = name, names[1-i]
			}
		}
	}
	release(first)
	waitForFile(t, file(later, "ready"), 10*time.Second)
	// The loop is on the later install now. Fixed code starts no watch here,
	// so there is nothing to wait for: the hold is a bound, and a watch
	// started in the loop was started before this install even began.
	time.Sleep(200 * time.Millisecond)
	if out := log.String(); strings.Contains(out, "is reachable") {
		t.Fatalf("the watch of '%s' started while the npm install of '%s' was still running:\n%s", first, later, out)
	}

	release(later)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("bridge preparation still running 10s after the last release:\n%s", log.String())
	}
	for _, name := range names {
		// The stub answers 404 on the status route: the bare-connect fallback (k205).
		waitForLine(t, &log, "MCP server '"+name+"': in-editor server "+srv.URL+"/mcp is reachable, but /mcp/status answers 404", 5*time.Second)
	}
}

// k87: a url without a port was probed on :80 whatever its scheme, so an
// https entry was watched on the wrong port. The scheme's default applies now,
// and an IPv6 host keeps its brackets. The urls are parsed as waitForEndpoint
// parses them; url.Parse lowercases the scheme, so the uppercase row pins
// what a user writes, not the EqualFold in dialAddr.
func TestDialAddr(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"http://127.0.0.1/mcp", "127.0.0.1:80"},
		{"https://mcp.example.com/mcp", "mcp.example.com:443"},
		{"http://127.0.0.1:8000/mcp", "127.0.0.1:8000"},
		{"https://mcp.example.com:8443/mcp", "mcp.example.com:8443"},
		{"http://[::1]:8000/mcp", "[::1]:8000"},
		{"https://[::1]/mcp", "[::1]:443"},
		{"HTTPS://mcp.example.com/mcp", "mcp.example.com:443"},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.raw, err)
		}
		if got := dialAddr(u); got != tc.want {
			t.Errorf("dialAddr(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// k253: the five-minute warning says that the editor is not running when the
// launcher knows it, in place of guessing at a firewall. One wording for every
// not-running state, with no claim about a pending restart: the supervisor
// keeps "desired" on after a give-up and after an exit 0 alike. A start held
// for the last run's crash reporter (status waiting, k154) is named, since
// the supervisor runs it on its own once the reporter is done.
func TestEditorNotRunningWhy(t *testing.T) {
	const tail = "start it with ue_start or `start ue` in the console"
	for _, c := range []struct {
		name string
		st   supervisor.ServiceStatus
		want string // "" = no statement, the guess stays
	}{
		{"running", supervisor.ServiceStatus{Name: "ue", Running: true, Desired: true}, ""},
		{"unknown", supervisor.ServiceStatus{Name: "ue", Unresponsive: true}, ""},
		{"never started", supervisor.ServiceStatus{Name: "ue"}, "the launcher's editor is not running;"},
		{"stopped", supervisor.ServiceStatus{Name: "ue", LastExit: ""}, tail},
		{"exit 0, still desired", supervisor.ServiceStatus{Name: "ue", Desired: true, LastExit: "exit 0"}, "last run ended: exit 0)"},
		{"exit 1, still desired", supervisor.ServiceStatus{Name: "ue", Desired: true, LastExit: "exit 1"}, "last run ended: exit 1)"},
		{"start failed", supervisor.ServiceStatus{Name: "ue", Desired: true, LastError: "no such file"}, "last start failed: no such file)"},
		{"start held for the crash reporter", supervisor.ServiceStatus{Name: "ue", Desired: true, LastExit: "exit 3", Waiting: "crash-reporter"}, "last run ended: exit 3); its start is held until the last run's crash reporter has finished"},
	} {
		got := editorNotRunningWhy(c.st)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want it to contain %q", c.name, got, c.want)
		}
		if got != "" {
			for _, must := range []string{"cannot answer until the editor runs again", tail} {
				if !strings.Contains(got, must) {
					t.Errorf("%s: %q lacks %q", c.name, got, must)
				}
			}
			if strings.Contains(got, "pending") || strings.Contains(got, "is not restarted") {
				t.Errorf("%s: %q claims something about a restart", c.name, got)
			}
		}
	}
}

// The hookup: prepareMCPBridges hands the watcher the supervisor's view, so the warning names the editor. Passing nil for the
// probe would leave the firewall guess.
func TestPrepareMCPBridgesWarnsThatTheEditorIsNotRunning(t *testing.T) {
	shortenEndpointWait(t, 100*time.Millisecond, 20*time.Millisecond)
	sup := supervisor.New(func(string) {})
	if err := sup.AddService(supervisor.ServiceSpec{Name: "ue", Command: "sleep", Args: []string{"1"}}); err != nil {
		t.Fatal(err)
	}
	// Never started: not running, not desired.
	sc := newScripts(t)
	bridge := filepath.Join(t.TempDir(), "bridge")
	sc.write(bridge, initAnswer)
	cfg := &config.Config{}
	cfg.MCP.ExtraServers = map[string]map[string]interface{}{"ue-llm-toolkit": {
		"command": bridge,
		"env":     map[string]interface{}{"UNREAL_MCP_URL": "http://" + freeLoopbackAddr(t)},
	}}
	var log syncLog
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg) // Status needs the control loops
	defer func() { cancel(); wg.Wait() }()
	prepareMCPBridges(ctx, sup, cfg, t.TempDir(), log.log)
	waitForLine(t, &log, "not reachable after", 5*time.Second)
	if out := log.String(); !strings.Contains(out, "editor is not running") || strings.Contains(out, "Firewall") {
		t.Fatalf("the warning does not say the editor is not running:\n%s", out)
	}
}

func TestWaitForEndpointNamesAnEditorThatIsNotRunning(t *testing.T) {
	shortenEndpointWait(t, 100*time.Millisecond, 20*time.Millisecond)
	u := "http://" + freeLoopbackAddr(t)
	for _, c := range []struct {
		name       string
		ep         endpoint
		notRunning func() string
		want, not  string
	}{
		{"editor down", editorEndpoint, func() string { return "the launcher's editor is not running" }, "the launcher's editor is not running", "Firewall"},
		{"editor up", editorEndpoint, func() string { return "" }, "Firewall", ""},
		{"no probe", editorEndpoint, nil, "Firewall", ""},
		{"remote server", remoteEndpoint, func() string { return "the launcher's editor is not running" }, "does the url match", "editor is not running"},
	} {
		var log syncLog
		waitForEndpoint(context.Background(), "x", c.ep, u, c.notRunning, log.log)
		out := log.String()
		if !strings.Contains(out, c.want) || (c.not != "" && strings.Contains(out, c.not)) {
			t.Errorf("%s: logged %q, want %q and not %q", c.name, out, c.want, c.not)
		}
	}
}
