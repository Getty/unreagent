package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

const (
	// smokeTestTimeout bounds one bridge's smoke test (start + initialize).
	smokeTestTimeout = 20 * time.Second
	// smokeTestWaitDelay bounds the wait for a killed bridge's output pipes. A
	// grandchild that inherited them (a worker the bridge forked) can hold them
	// open long after; on Windows the job object ends it, elsewhere this bound
	// is what returns. Same bound as the supervisor's output drain.
	smokeTestWaitDelay = 2 * time.Second
)

// bridgeInstallTimeout caps the whole npm install of a node bridge, the retry
// with legacyPeerDeps included. It runs before the services start, so
// without a cap a stalled registry keeps the editor from ever starting. Five
// minutes is a choice made here, not a known requirement: npm's default
// fetch-timeout (five minutes, per request) was the reference point, which
// leaves a cold install over a slow link room while a wedged one still lets
// the launcher go on. A var so tests can shorten it.
var bridgeInstallTimeout = 5 * time.Minute

// legacyPeerDeps is the flag of the one retry of a failed bridge install
// (prepareNodeBridge): npm then resolves the tree the way npm 6 did and
// installs no package that only a peerDependencies entry asks for. A peer
// that is also a regular dependency of another package is installed anyway.
const legacyPeerDeps = "--legacy-peer-deps"

// noSave is on both installs of a node bridge (k242): the bridge is a
// plugin's own project, often a tracked submodule whose package-lock.json
// does not match its package.json, and a plain npm install rewrites that
// lock. With --no-save npm still installs what the lock pins and adds peers
// on its own, but writes neither the lock nor package.json (measured by k215
// on Windows with npm 10.9.2, not by this code's tests).
const noSave = "--no-save"

// bridgeInstallMarker is the file in a node bridge's node_modules that lets
// the next start skip npm install. Only a passing smoke test writes it
// (settleModules).
const bridgeInstallMarker = ".unreagent-installed"

// bridgeModules is the node_modules of a node bridge, as prepareNodeBridge
// hands it on to settleModules.
type bridgeModules struct {
	path    string // "" = no package.json: npm is not involved
	earlier bool   // node_modules was there before this start
	marked  bool   // it carries the marker
}

// prepareMCPBridges checks all stdio extraServers (e.g. the UE LLM Toolkit
// bridge) completely once BEFORE the services start and complains loudly about
// every link in the chain, instead of letting the agent run into a meaningless
// MCP connect error (-32000):
//  1. Binary findable (node etc. on PATH)?
//  2. Node bridges: script present? node_modules marked, i.e. the bridge
//     passed a smoke test on it? (otherwise npm install)
//  3. Smoke test: start the server, send MCP initialize, wait for the answer.
//  4. UNREAL_MCP_URL: once every bridge is prepared, report asynchronously as
//     soon as the in-editor server is reachable — or warn if it is not.
//
// An http/sse entry (no command) starts no process, so only the last check
// applies to it: its url is watched the same way, asynchronously — the agent
// is not held back for it (k87). Nor for Epic's Unreal MCP server
// (unreal.mcp), which is watched by a ping here unless the agent's first
// start waits for it (agentHeldForUnrealMCP, k266).
//
// ctx is the launcher's signal context: once it is cancelled, the install or
// smoke test in progress is stopped, the remaining bridges are skipped and no
// watcher starts; the watchers, once started, end with it.
func prepareMCPBridges(ctx context.Context, sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, logger func(string)) {
	type watch struct {
		name string
		ep   endpoint
		url  string
	}
	var watches []watch // started once the loop is done
	for name, def := range cfg.MCP.ExtraServers {
		if ctx.Err() != nil {
			return // shutting down — no further bridge to check
		}
		command, _ := def["command"].(string)
		if command == "" {
			u, _ := def["url"].(string)
			watches = append(watches, watch{name, remoteEndpoint, u})
			continue
		}
		var args []string
		if raw, ok := def["args"].([]interface{}); ok {
			for _, a := range raw {
				if s, ok := a.(string); ok {
					args = append(args, s)
				}
			}
		}
		var env []string
		envMap := map[string]string{}
		if raw, ok := def["env"].(map[string]interface{}); ok {
			for k, v := range raw {
				if s, ok := v.(string); ok {
					env = append(env, k+"="+s)
					envMap[k] = s
				}
			}
		}

		if _, err := exec.LookPath(command); err != nil {
			logger("WARN MCP server '" + name + "': command '" + command + "' not found (PATH) — bridge cannot start. Install it or put the full path in the config.")
			continue
		}

		var modules bridgeModules
		if strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe") == "node" {
			m, ok := prepareNodeBridge(ctx, sup, cfg, name, args, agentWorkdir, logger)
			if !ok {
				continue
			}
			modules = m
		}

		passed := smokeTestMCP(ctx, name, command, args, env, agentWorkdir, logger)
		settleModules(ctx, name, modules, passed, logger)

		if u := envMap["UNREAL_MCP_URL"]; u != "" {
			watches = append(watches, watch{name, editorEndpoint, u})
		}
	}
	// After the loop, so a later bridge's npm install does not use up a
	// window before the editor has even been started (k87, k163).
	if ctx.Err() != nil {
		return
	}
	// Epic's Unreal MCP server (unreal.mcp, k266) is watched the same way,
	// by a ping; while the agent's first start waits for it, the hold in
	// run() is that watch.
	if cfg.Unreal.MCP.Enabled && !agentHeldForUnrealMCP(cfg) {
		watches = append(watches, watch{config.UnrealMCPServerName, unrealMCPEndpoint, cfg.Unreal.MCP.URL()})
	}
	for _, w := range watches {
		go waitForEndpoint(ctx, w.name, w.ep, w.url, editorNotRunning(sup), logger)
	}
}

// editorNotRunning returns, for waitForEndpoint, what to say when the
// supervisor's "ue" service is not running at the time of the warning: "" while
// it runs or when its state is unknown. It makes no claim about a pending
// restart: the supervisor keeps its desired state on after a give-up and after
// an exit 0, so that state cannot tell them from a restart waiting out its
// delay.
func editorNotRunning(sup *supervisor.Supervisor) func() string {
	return func() string {
		for _, st := range sup.Status() {
			if st.Name == "ue" {
				return editorNotRunningWhy(st)
			}
		}
		return ""
	}
}

// editorNotRunningWhy is the sentence for one status of the editor service.
func editorNotRunningWhy(st supervisor.ServiceStatus) string {
	if st.Running || st.Unresponsive {
		return ""
	}
	why := "the launcher's editor is not running"
	switch {
	case st.LastError != "":
		why += " (its last start failed: " + st.LastError + ")"
	case st.LastExit != "":
		why += " (its last run ended: " + st.LastExit + ")"
	}
	if st.Waiting == "crash-reporter" {
		why += "; its start is held until the last run's crash reporter has finished, then comes on its own"
	}
	return why + "; the server cannot answer until the editor runs again — if the launcher is not restarting it, start it with ue_start or `start ue` in the console"
}

// prepareNodeBridge makes sure the script and node_modules of a Node bridge are
// present, with an npm install unless node_modules carries the marker. ok
// false = the bridge is unusable. A plain npm install that fails on peer
// resolution, or crashes, gets one retry with legacyPeerDeps within the same
// bridgeInstallTimeout. After a failed install over a node_modules
// that was there before, the bridge stays usable and the smoke test judges
// what that install left — npm may have changed it, and the earlier tree may
// itself be the leftover of a failed install.
func prepareNodeBridge(ctx context.Context, sup *supervisor.Supervisor, cfg *config.Config, name string, args []string, agentWorkdir string, logger func(string)) (m bridgeModules, ok bool) {
	script := nodeScript(args)
	if script == "" {
		return m, true // no script recognisable — the smoke test decides
	}
	if !filepath.IsAbs(script) {
		script = filepath.Join(agentWorkdir, script)
	}
	if !fileExists(script) {
		logger("WARN MCP server '" + name + "': script not found: " + script + " — is the plugin installed?")
		return m, false
	}
	dir := filepath.Dir(script)
	if !fileExists(filepath.Join(dir, "package.json")) {
		return m, true
	}
	m.path = filepath.Join(dir, "node_modules")
	m.earlier = fileExists(m.path)
	if m.marked = fileExists(filepath.Join(m.path, bridgeInstallMarker)); m.marked {
		return m, true
	}
	npm := cfg.Runtimes.Node.Npm
	if npm == "" {
		npm = "npm"
	}
	// node_modules without the marker is what an install cut short leaves,
	// one a smoke test failed on, or a complete one never marked (from
	// before the marker, or installed by hand). The launcher deletes
	// nothing; npm itself may remove extraneous packages.
	state := "node_modules missing"
	if m.earlier {
		state = "node_modules not marked as working"
	}
	logger(fmt.Sprintf("MCP server '%s': %s — installing dependencies (npm install %s, at most %s) in %s …", name, state, noSave, bridgeInstallTimeout, dir))
	ictx, cancel := context.WithTimeout(ctx, bridgeInstallTimeout)
	defer cancel()
	install := []string{"install", noSave}
	retried := false
	res, err := sup.RunOnceContext(ictx, npm, install, dir, nil, "prepare:mcp:"+name)
	// One retry with legacyPeerDeps (k215), for a failure that flag can
	// change: the peer conflict ERESOLVE, whose own message suggests it, or
	// no error code at all, which is how an exception inside npm's tree
	// building shows — npm 10.9.2 crashes that way on the UE LLM Toolkit
	// bridge ("Cannot read properties of null (reading 'edgesOut')") and
	// installs it with the flag. Any other code (network, registry, file
	// access, an install script) has nothing to do with peers, and a retry
	// would only repeat it: a refused registry connection took npm 10.9.2
	// 141 s to give up on an empty cache. A bridge npm installs without the
	// flag keeps the peers npm adds on its own.
	if code := npmErrorCode(res.Output); err == nil && res.ExitCode != 0 && (code == "" || code == "ERESOLVE") {
		if code == "" {
			code = "no npm error code"
		}
		logger(fmt.Sprintf("MCP server '%s': npm %s exit %d (%s) — trying once more with %s, which installs no package that only a peerDependencies entry asks for:\n%s",
			name, strings.Join(install, " "), res.ExitCode, code, legacyPeerDeps, tailLines(res.Output, 20)))
		retried = true
		install = []string{"install", noSave, legacyPeerDeps}
		res, err = sup.RunOnceContext(ictx, npm, install, dir, nil, "prepare:mcp:"+name)
	}
	command := "npm " + strings.Join(install, " ")
	var cause, hint string
	switch {
	case err != nil && ctx.Err() != nil:
		logger("MCP server '" + name + "': npm install aborted — the launcher is shutting down; the next start installs again.")
		return bridgeModules{}, false
	case errors.Is(err, context.DeadlineExceeded):
		cause = fmt.Sprintf("%s timed out after %s", command, bridgeInstallTimeout)
		if retried {
			cause = fmt.Sprintf("%s timed out: the plain install and it had %s together", command, bridgeInstallTimeout)
		}
		hint = " Registry or proxy unreachable? Run " + command + " in " + dir + " by hand."
	case err != nil:
		cause = command + " failed: " + err.Error()
	case res.ExitCode != 0:
		cause = fmt.Sprintf("%s exit %d", command, res.ExitCode)
	default:
		// Not marked yet: npm can call a package cut short mid-extraction up
		// to date, so only the smoke test can vouch for the result.
		logger("MCP server '" + name + "': dependencies installed (" + command + ").")
		return m, true
	}
	// Offline, a complete node_modules from before the marker must not cost
	// the bridge: the smoke test decides, and a pass writes the marker.
	verdict := "bridge will not start"
	if m.earlier {
		verdict = "the launcher keeps the node_modules there, and the smoke test decides whether it is complete"
	}
	msg := fmt.Sprintf("WARN MCP server '%s': %s — %s.%s", name, cause, verdict, hint)
	if out := tailLines(res.Output, 20); out != "" {
		msg += "\n" + out
	}
	logger(msg)
	if m.earlier {
		return m, true
	}
	return bridgeModules{}, false
}

// nodeScript is the script among a node bridge's args: the first that ends
// in .js, .mjs or .cjs, "" if there is none.
func nodeScript(args []string) string {
	for _, a := range args {
		if strings.HasSuffix(a, ".js") || strings.HasSuffix(a, ".mjs") || strings.HasSuffix(a, ".cjs") {
			return a
		}
	}
	return ""
}

// npmErrorCode returns the error code a failed npm names in its output
// ("npm error code ERESOLVE", in older npm "npm ERR! code ERESOLVE"), or ""
// when it names none.
func npmErrorCode(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"npm error code ", "npm ERR! code "} {
			if code, ok := strings.CutPrefix(line, prefix); ok {
				return strings.TrimSpace(code)
			}
		}
	}
	return ""
}

// settleModules acts on the smoke test's verdict for a node bridge's
// node_modules: a pass writes the marker; a fail on one that was there
// before this start says what to delete should the error be a missing
// module. The smoke test fails for reasons outside node_modules too, hence
// the condition in the hint rather than deleting anything automatically.
func settleModules(ctx context.Context, name string, m bridgeModules, passed bool, logger func(string)) {
	const missing = "': if the error above names a missing module or file, delete "
	switch {
	case m.path == "": // no package.json — nothing to mark or repair
	case passed && !m.marked:
		markInstalled(name, m.path, logger)
	case passed || !m.earlier || ctx.Err() != nil:
		// marked already, installed fresh this start, or shutting down
	case m.marked:
		// No npm install ran this start, so one may simply be due (a plugin
		// update that needs a new module): deleting the marker brings it.
		logger("WARN MCP server '" + name + missing + filepath.Join(m.path, bridgeInstallMarker) + " and start again — the next start runs npm install; if that does not help, delete " + m.path + ".")
	default:
		// npm install ran over it and can call a partial tree up to date, so
		// another install would not repair it.
		logger("WARN MCP server '" + name + missing + m.path + " and start again — that node_modules was there before this start, and npm install can report a partial one as up to date.")
	}
}

// markInstalled writes the marker into a bridge's node_modules. A marker
// that cannot be written costs another npm install on the next start,
// nothing more.
func markInstalled(name, modules string, logger func(string)) {
	// A package without dependencies can install without a node_modules.
	err := os.MkdirAll(modules, 0o755)
	if err == nil {
		err = os.WriteFile(filepath.Join(modules, bridgeInstallMarker),
			[]byte("Written by unreagent once the bridge passed its smoke test on this node_modules. Delete this file and the next start runs npm install again.\n"), 0o644)
	}
	if err != nil {
		logger("WARN MCP server '" + name + "': could not mark node_modules as installed: " + err.Error() + " — the next start runs npm install again")
	}
}

// smokeTestMCP starts the stdio server once as a trial, sends a real MCP
// initialize and waits for the answer. If the start fails, the real cause
// (stderr) is in the log instead of just a connect error at the agent. It
// reports whether the bridge answered with an initialize result
// (initResponse); any other response to it ends the wait at once and is
// named.
//
// The bridge runs in a job object, as every service and one-shot command
// does (only the short-lived taskkill/pkill, reg query and git check-ignore
// (unignoredTokenFile) helpers run without one), so ending the trial —
// answer, timeout or parent cancelled — ends what the bridge started as
// well, and a launcher crash leaves nothing behind.
func smokeTestMCP(parent context.Context, name, command string, args, env []string, dir string, logger func(string)) bool {
	if parent.Err() != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, smokeTestTimeout)
	defer cancel()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = smokeTestWaitDelay
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err1 := cmd.StdinPipe()
	stdout, err2 := cmd.StdoutPipe()
	if err1 != nil || err2 != nil {
		logger("WARN MCP server '" + name + "': smoke test could not open pipes")
		return false
	}
	job, err := supervisor.NewJob()
	if err != nil {
		logger("WARN MCP server '" + name + "': smoke test: job object could not be created: " + err.Error() + " — a launcher crash leaves the bridge behind")
	}
	if err := cmd.Start(); err != nil {
		job.Close()
		logger("WARN MCP server '" + name + "': smoke test failed to start: " + err.Error())
		return false
	}
	if err := job.Assign(cmd.Process.Pid); err != nil {
		logger("WARN MCP server '" + name + "': smoke test: job assign failed: " + err.Error() + " — the bridge runs unsupervised")
	}
	_, _ = io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"unreagent-smoketest","version":"`+version+`"}}}`+"\n")
	// Read in the background so the timeout and the parent can end the wait;
	// Wait below closes stdout, which ends this reader. It sends "" for the
	// initialize result, the failure for any other response to it, and "no
	// initialize response" once stdout ends without one.
	answer := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if ok, failure := initResponse(sc.Bytes()); ok || failure != "" {
				answer <- failure
				return
			}
		}
		answer <- "no initialize response"
	}()
	ok, reason := false, "" // reason stays "" when the timeout ends the wait
	select {
	case reason = <-answer:
		ok = reason == ""
	case <-ctx.Done():
	}
	// The job first (Windows: the bridge's whole tree), the direct child as the
	// fallback where there is no job.
	job.Close()
	_ = cmd.Process.Kill()
	_ = stdin.Close()
	_ = cmd.Wait()
	if ok {
		logger("MCP server '" + name + "': smoke test OK — bridge answers initialize.")
		return true
	}
	if parent.Err() != nil {
		logger("MCP server '" + name + "': smoke test aborted — the launcher is shutting down")
		return false
	}
	detail := strings.TrimSpace(stderr.String())
	if reason == "" {
		reason = fmt.Sprintf("timeout after %s", smokeTestTimeout)
	}
	if detail != "" {
		logger(fmt.Sprintf("WARN MCP server '%s': smoke test failed (%s):\n%s", name, reason, stderrTail(detail, 15)))
	} else {
		logger("WARN MCP server '" + name + "': smoke test failed (" + reason + ", no stderr)")
	}
	return false
}

// initResponse reads one line of a bridge's stdout as the answer to the
// smoke test's initialize, which goes out with id 1. A response to it, a
// JSON object with id 1, a result or an error and no method, ends the wait:
// ok only for a result whose serverInfo is an object; any other is the
// failure, an error named by initError. No second answer to id 1 would
// come. Any other line is neither, and the smoke test reads on: one that is
// not JSON (a bridge's debug output), a notification or request, a reply to
// another id.
func initResponse(line []byte) (ok bool, failure string) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method json.RawMessage `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(line, &msg) != nil || string(msg.ID) != "1" || msg.Method != nil {
		return false, ""
	}
	if msg.Error != nil && string(msg.Error) != "null" {
		return false, initError(msg.Error)
	}
	if msg.Result == nil {
		return false, "" // neither result nor error: no response
	}
	var result struct {
		ServerInfo json.RawMessage `json:"serverInfo"`
	}
	if json.Unmarshal(msg.Result, &result) != nil || !bytes.HasPrefix(bytes.TrimSpace(result.ServerInfo), []byte("{")) {
		return false, "initialize answered without serverInfo"
	}
	return true, ""
}

// initError names an error response to initialize by its code and message
// as the bridge sent them: a code that is no integer, or a message that is
// no string, shows as its JSON instead of hiding the rest.
func initError(raw json.RawMessage) string {
	var e struct {
		Code    json.RawMessage `json:"code"`
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal(raw, &e) != nil { // the error is no object
		return "initialize answered with error " + string(raw)
	}
	s := "initialize answered with error"
	if e.Code != nil {
		s += " " + string(e.Code)
	}
	var text string
	if json.Unmarshal(e.Message, &text) != nil {
		text = string(e.Message)
	}
	if text != "" {
		s += ": " + text
	}
	return s
}

// stderrTail returns the last n lines of a failed bridge's stderr, led by the
// line that names the error when that line is not among them: node prints it
// above a stack trace and the error's properties, which alone can fill n
// lines ("Cannot find module 'dep'" was cut that way). Node prints the
// uncaught error last, so the last such line is the one; "[...]" stands for
// what lies between it and the tail.
func stderrTail(stderr string, n int) string {
	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	cut := max(len(lines)-n, 0) // lines[cut:] are the tail
	tail := strings.Join(lines[cut:], "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		switch {
		case !jsErrorLine(lines[i]):
		case i >= cut:
			return tail
		case i == cut-1:
			return lines[i] + "\n" + tail
		default:
			return lines[i] + "\n[...]\n" + tail
		}
	}
	return tail
}

// jsErrorLine reports whether line opens a JavaScript error as node prints
// one: an unindented name ending in Error, an optional [CODE], a colon —
// "Error: …", "TypeError: …", "Error [ERR_MODULE_NOT_FOUND]: …". A cause
// nested inside ("  [cause]: Error: …") is indented and does not count.
// Node 24's bracketed form for an unhandled rejection ("[TypeError: fetch
// failed] {") counts: the leading [ is no space, so "[TypeError" passes the
// no-space test.
func jsErrorLine(line string) bool {
	name, _, ok := strings.Cut(line, ":")
	if !ok {
		return false
	}
	name, _, _ = strings.Cut(name, " [")
	return strings.HasSuffix(name, "Error") && !strings.ContainsAny(name, " \t")
}

// endpointWaitWindow is how long waitForEndpoint keeps trying a URL before it
// warns; endpointPollInterval is the pause between two tries;
// endpointStatusTimeout bounds one GET of the toolkit's status route. Vars so
// tests can shorten them.
var (
	endpointWaitWindow    = 5 * time.Minute
	endpointPollInterval  = 5 * time.Second
	endpointStatusTimeout = 3 * time.Second
)

// endpoint is the kind of URL waitForEndpoint watches, with the words its log
// lines use for it.
type endpoint struct {
	field string // config key the URL is under, named when it does not parse
	what  string // how the lines name the server ahead of its URL ("" = the URL alone)
	hint  string // what the give-up warning suggests
	probe probeKind
	then  string // appended to the give-up warning ("" = nothing)
}

// probeKind is what waitForEndpoint asks once a connect succeeded.
type probeKind int

const (
	probeNone   probeKind = iota // the connect is "up"
	probeStatus                  // GET <url>/mcp/status, the toolkit's route (k205)
	probePing                    // POST a JSON-RPC ping to <url> (k266)
)

var (
	// editorEndpoint is a stdio bridge's UNREAL_MCP_URL: the in-editor server
	// the bridge talks to.
	editorEndpoint = endpoint{
		field: "UNREAL_MCP_URL",
		what:  "in-editor server ",
		hint:  "is the editor running with the plugin? Firewall? Is the server listening on localhost only?",
		probe: probeStatus,
	}
	// remoteEndpoint is the url of an http/sse entry, which the agent connects
	// to itself — at its own start, possibly before the server is up.
	remoteEndpoint = endpoint{
		field: "url",
		hint:  "is the server running, and does the url match its port and path? The agent may have marked the server failed; once it answers, /mcp in Claude Code reconnects it.",
	}
	// unrealMCPEndpoint is Epic's Unreal MCP server in the editor, which
	// unreal.mcp switches on (k266).
	unrealMCPEndpoint = endpoint{
		field: "unreal.mcp.port",
		what:  "Unreal MCP server ",
		hint:  "is the editor running with the ModelContextProtocol plugin enabled, and is unreal.mcp.port free for it?",
		probe: probePing,
	}
	// heldAgentEndpoint is unrealMCPEndpoint while the agent's first start
	// waits for it (holdAgentForUnrealMCP): its warning says the agent starts.
	heldAgentEndpoint = func() endpoint {
		ep := unrealMCPEndpoint
		ep.then = "the agent starts now without it: a headless agent stays without Unreal MCP for its whole run, an interactive one gets it through /mcp in Claude Code once the server answers."
		return ep
	}()
)

// dialAddr is the host:port waitForEndpoint connects to for u: the URL's own
// port, or else the scheme's default — 443 for https, 80 for anything else.
func dialAddr(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if strings.EqualFold(u.Scheme, "https") {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// waitForEndpoint reports once as soon as rawURL is up — the editor needs
// time to start up, and so does a server in it. If it is not up within
// endpointWaitWindow, there is one warning naming the usual suspects. It
// returns silently once ctx ends. The result is true when the watch ended on
// an answer (a probe's line or, without a probe, a connect), false when the
// window or ctx ended it.
//
// What "up" means depends on ep.probe. For an http/sse entry (remoteEndpoint,
// probeNone) it is a bare TCP connect: no request, no MCP session, nothing
// for the server to act on. For a server in the editor a connect is not
// enough: the editor's listeners accept seconds before it serves HTTP —
// requests are handled on the game thread, which a modal dialog can block
// for good. Once the port is open, each try asks the server, bounded by
// endpointStatusTimeout:
//   - the toolkit (editorEndpoint, probeStatus, k205): GET <url>/mcp/status
//     (the path the bridge uses); only a 200 with a JSON object counts as
//     ready. A 404, a redirect or a 200 that is not JSON means another
//     listener or a toolkit without the route, so the connect is reported as
//     it was, tool count unknown;
//   - Epic's Unreal MCP server (unrealMCPEndpoint, probePing, k266): a
//     JSON-RPC ping POSTed to <url> without a session; only a 200 carrying
//     the ping's result counts (pingProbe).
//
// A timeout or any other answer is "not ready yet".
//
// The lines show scheme, host and path only: user info and query can carry a
// token, and headers are never read here.
//
// notRunning, when not nil, is asked at the warning for what the launcher
// knows about the editor (k253): a non-empty answer replaces the hint, which
// only guesses at a firewall, a localhost-only server or a blocked game
// thread. It applies to the servers in the editor (those with a probe) alone.
func waitForEndpoint(ctx context.Context, name string, ep endpoint, rawURL string, notRunning func() string, logger func(string)) bool {
	window, pause, statusTimeout := endpointWaitWindow, endpointPollInterval, endpointStatusTimeout
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		logger("WARN MCP server '" + name + "': " + ep.field + " is not a URL with a host — its reachability is not checked.")
		return false
	}
	addr := dialAddr(u)
	shown := ep.what + (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
	prefix := "MCP server '" + name + "': " + shown

	wctx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	dialer := net.Dialer{Timeout: 3 * time.Second}
	var probe interface {
		try(ctx context.Context, lastAnswer *string) (line string, done bool)
	}
	switch ep.probe {
	case probeStatus:
		probe = newStatusProbe(u, statusTimeout)
	case probePing:
		probe = newPingProbe(u, statusTimeout)
	}
	opened := false  // some try connected; the window can end inside a later dial
	lastAnswer := "" // the probe's last answer that did not end the watch, e.g. "HTTP 503"
	for {
		conn, err := dialer.DialContext(wctx, "tcp", addr)
		if err == nil {
			opened = true
			_ = conn.Close()
			if probe == nil {
				logger(prefix + " is reachable.")
				return true
			}
			if line, done := probe.try(wctx, &lastAnswer); done {
				logger(prefix + line)
				return true
			}
		}
		select {
		case <-time.After(pause):
		case <-wctx.Done():
			if ctx.Err() != nil { // the launcher ends, not the window
				return false
			}
			hint := ep.hint
			what := fmt.Sprintf(" not reachable after %s", window)
			if opened && probe != nil {
				what = fmt.Sprintf(": port open, but the toolkit did not answer %s within %s", statusPath, window)
				if ep.probe == probePing {
					what = fmt.Sprintf(": port open, but it did not answer a ping within %s", window)
				}
				if lastAnswer != "" {
					what += " (last answer: " + lastAnswer + ")"
				}
				hint = "is the editor's game thread blocked, e.g. by a modal dialog such as \"Restore Packages\" after a crash, or is it still loading? No editor tool can run until it answers."
			}
			if notRunning != nil && probe != nil {
				if why := notRunning(); why != "" {
					hint = why
				}
			}
			line := "WARN " + prefix + what + " — " + hint
			if ep.then != "" {
				line += " — " + ep.then
			}
			logger(line)
			return false
		}
	}
}

// agentHeldForUnrealMCP reports whether the agent's first start waits for
// Epic's Unreal MCP server (k266): with unreal.mcp on, an agent, and an
// editor the launcher starts itself. Claude Code marks an http server that
// does not answer at its start as failed for good after about 6 s, and a
// headless session never tries again (k221 item 5), while the editor's
// server first answered 44 s after the start (k221 item 2). An editor left
// to a manual start gets no hold: nothing says when it comes. Nor does an
// agent that gets no unreal-mcp entry: the launcher hands it one only in the
// MCP config it writes with mcp.enabled and agent.claudeIntegration (run(),
// buildMCPServers), so there is nothing for it to wait for.
func agentHeldForUnrealMCP(cfg *config.Config) bool {
	return cfg.Unreal.MCP.Enabled && cfg.Agent.Enabled && !cfg.Unreal.ManualStart &&
		cfg.MCP.Enabled && cfg.Agent.ClaudeIntegration
}

// holdAgentForUnrealMCP waits until the Unreal MCP server at rawURL answers a
// ping, then calls startAgent once (k266). At the end of endpointWaitWindow
// it calls startAgent anyway, after one warning that says so. When the
// editor ends and is not restarted (a value on editorEnded: the editor
// service's OnExit, the give-up of k69/k251 among them) the server cannot
// answer any more: the wait ends at once with one warning naming that, and
// the agent starts, so it can fix the editor as the give-up line says. When
// ctx ends first it starts nothing. Only the agent's first start is held: a
// later restart is the supervisor's, as before.
func holdAgentForUnrealMCP(ctx context.Context, rawURL string, editorEnded <-chan supervisor.Exit, notRunning func() string, logger func(string), startAgent func()) {
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var ended *supervisor.Exit
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case e := <-editorEnded:
			ended = &e
			cancel()
		case <-wctx.Done():
		}
	}()
	// waitForEndpoint's own warning, if any, is the hold's one warning.
	warned := false
	answered := waitForEndpoint(wctx, config.UnrealMCPServerName, heldAgentEndpoint, rawURL, notRunning, func(line string) {
		warned = warned || strings.HasPrefix(line, "WARN ")
		logger(line)
	})
	cancel()
	<-watchDone
	if ctx.Err() != nil {
		return
	}
	if ended != nil && !answered && !warned {
		how := "failed"
		if ended.Success {
			how = "exited"
		}
		logger("WARN MCP server '" + config.UnrealMCPServerName + "': " + heldAgentEndpoint.what + rawURL +
			" will not answer: the editor " + how + " and is not restarted — " + heldAgentEndpoint.then)
	}
	startAgent()
}

// statusPath is the toolkit's status route, relative to UNREAL_MCP_URL; the
// bridge appends the same path (lib.js checkUnrealConnection).
const statusPath = "/mcp/status"

// statusBodyLimit caps how much of a status answer is read: it lists every
// tool with its description, a few dozen kilobytes for the toolkit's set.
const statusBodyLimit = 4 << 20

// statusProbe GETs the toolkit's status route for waitForEndpoint.
type statusProbe struct {
	url    string
	client *http.Client
}

func newStatusProbe(u *url.URL, timeout time.Duration) *statusProbe {
	su := url.URL{Scheme: u.Scheme, User: u.User, Host: u.Host, Path: strings.TrimSuffix(u.Path, "/") + statusPath}
	return &statusProbe{url: su.String(), client: probeClient(timeout)}
}

// probeClient is the HTTP client of a probe of waitForEndpoint.
func probeClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		// Loopback as a rule: no proxy from the environment, and no
		// connection kept open in the editor between tries.
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		// A redirect is an answer from something that is not the server
		// asked; the probe reports it instead of following it.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// try GETs the status route once. done reports an answer that ends the watch,
// with the rest of its log line: a 200 with a JSON object (ready), or an
// answer that is not the toolkit's — a 404, a 3xx, a 200 that is not JSON.
// Anything else — a timeout, a reset, another status code — means not ready
// yet; another code is kept in *lastAnswer for the warning.
func (p *statusProbe) try(ctx context.Context, lastAnswer *string) (line string, done bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return "", false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		about, ok := describeStatus(io.LimitReader(resp.Body, statusBodyLimit))
		if !ok {
			return " is reachable and answers " + statusPath + " with 200, but not with the toolkit's JSON — probably another server on that port; tool count unknown.", true
		}
		return " is ready — " + about + ".", true
	case code == http.StatusNotFound:
		return " is reachable, but " + statusPath + " answers 404 — another server on that port, or a toolkit without the status route; tool count unknown.", true
	case code >= 300 && code < 400:
		return " is reachable, but " + statusPath + " redirects (HTTP " + strconv.Itoa(code) + ") — not the toolkit's answer; tool count unknown.", true
	default:
		*lastAnswer = "HTTP " + strconv.Itoa(resp.StatusCode)
		return "", false
	}
}

// describeStatus is what the log line says about a 200 status answer: the
// tool count, plus project and engine when present. ok is false when the body
// is not a JSON object at all (an HTML page, say): that is not the toolkit. A
// JSON object without a usable count still means ready; only the count is
// unknown then.
func describeStatus(body io.Reader) (string, bool) {
	var raw json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil || !bytes.HasPrefix(raw, []byte("{")) {
		return "", false
	}
	var st struct {
		ToolCount     *float64 `json:"toolCount"`
		ProjectName   string   `json:"projectName"`
		EngineVersion string   `json:"engineVersion"`
	}
	// A field of the wrong type fails the decode but the others still fill
	// in; the count is unknown then (the pointer may be set to a zero value).
	if err := json.Unmarshal(raw, &st); err != nil || st.ToolCount == nil || *st.ToolCount < 0 || *st.ToolCount != math.Trunc(*st.ToolCount) {
		return "the toolkit answers " + statusPath + ", tool count unknown", true
	}
	s := fmt.Sprintf("the toolkit lists %d tools", int64(*st.ToolCount))
	var about []string
	if v := logSafe(st.ProjectName); v != "" {
		about = append(about, "project "+v)
	}
	if v := logSafe(st.EngineVersion); v != "" {
		about = append(about, "engine "+v)
	}
	if len(about) > 0 {
		s += " (" + strings.Join(about, ", ") + ")"
	}
	return s, true
}

// logSafe is s for a log line, or "" when it is long or carries a rune that
// is not printable — control and format characters such as U+202E included:
// it comes from whatever listens on the port.
func logSafe(s string) string {
	if len(s) > 120 || strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		return ""
	}
	return s
}

// pingRequest is the JSON-RPC ping pingProbe POSTs, pingID its id. k221
// measured the editor's answer: 200 {"jsonrpc":"2.0","id":N,"result":{}},
// without a session; a GET is 405.
const (
	pingID      = 1
	pingRequest = `{"jsonrpc":"2.0","id":1,"method":"ping"}`
)

// pingProbe POSTs a JSON-RPC ping to Epic's Unreal MCP server for
// waitForEndpoint (k266).
type pingProbe struct {
	url    string
	client *http.Client
}

func newPingProbe(u *url.URL, timeout time.Duration) *pingProbe {
	return &pingProbe{url: u.String(), client: probeClient(timeout)}
}

// try POSTs the ping once, without a session. done is true only for a 200
// that carries the ping's result. Anything else — a timeout, a reset, another
// status code, a 200 with an error or without a result — is "not ready yet",
// kept in *lastAnswer for the warning. Unlike statusProbe, no answer ends the
// watch early, a 404 or a redirect neither: the editor's listener accepts
// long before it answers, what it says on the way is not known (k221 saw
// only timeouts), and an early end would start the agent too soon.
func (p *pingProbe) try(ctx context.Context, lastAnswer *string) (line string, done bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, strings.NewReader(pingRequest))
	if err != nil {
		return "", false
	}
	req.Header.Set("Content-Type", "application/json")
	// Both, as the streamable HTTP transport asks of a client: the server
	// may answer a POST as an event stream.
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		*lastAnswer = "HTTP " + strconv.Itoa(resp.StatusCode)
		return "", false
	}
	if !pingAnswered(resp.Header.Get("Content-Type"), io.LimitReader(resp.Body, statusBodyLimit)) {
		*lastAnswer = "HTTP 200 without a ping result"
		return "", false
	}
	return " is ready — it answers ping.", true
}

// pingAnswered reports whether body, of the given Content-Type, is the
// answer to pingRequest. An event stream counts when one of its data lines
// is.
func pingAnswered(contentType string, body io.Reader) bool {
	if media, _, _ := strings.Cut(contentType, ";"); strings.EqualFold(strings.TrimSpace(media), "text/event-stream") {
		sc := bufio.NewScanner(body)
		sc.Buffer(nil, statusBodyLimit)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data:"); ok && isPingResult([]byte(data)) {
				return true
			}
		}
		return false
	}
	raw, err := io.ReadAll(body)
	return err == nil && isPingResult(raw)
}

// isPingResult reports whether raw is a JSON-RPC 2.0 response to pingRequest
// (its id) that carries a result object and no error.
func isPingResult(raw []byte) bool {
	var r struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *float64        `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return false
	}
	return r.JSONRPC == "2.0" && r.ID != nil && *r.ID == pingID && r.Error == nil &&
		bytes.HasPrefix(bytes.TrimSpace(r.Result), []byte("{"))
}
