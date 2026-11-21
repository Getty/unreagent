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

// k106 fixtures. initAnswer is a bridge that answers MCP initialize;
// brokenBridge one that dies like node on a missing module. npmOK installs
// and succeeds; npmOffline fails like npm without a registry, after leaving
// a partial node_modules behind.
const (
	initAnswer = `read line
echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"fake","version":"0"}}}'
`
	brokenBridge = "echo \"Error: Cannot find module 'dep'\" >&2\nexit 1\n"
	npmOK        = "mkdir -p node_modules/dep\nexit 0\n"
	npmOffline   = "mkdir -p node_modules/half\necho 'npm ERR! network request failed' >&2\nexit 1\n"
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

// k106: node_modules with the marker is complete — no npm install.
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
	if !strings.Contains(log, "npm install exit 1") || !strings.Contains(log, "bridge will not start") {
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
