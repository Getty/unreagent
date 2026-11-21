package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

// bridgeInstallTimeout caps the whole npm install of a node bridge. It runs
// before the services start, so without a cap a stalled registry keeps the
// editor from ever starting. Five minutes is a choice made here, not a known
// requirement: npm's default fetch-timeout (five minutes, per request) was the
// reference point, which leaves a cold install over a slow link room while a
// wedged one still lets the launcher go on. A var so tests can shorten it.
var bridgeInstallTimeout = 5 * time.Minute

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
//  4. UNREAL_MCP_URL: report asynchronously as soon as the in-editor server is
//     reachable — or warn if it is not.
//
// ctx is the launcher's signal context: once it is cancelled, the install or
// smoke test in progress is stopped and the remaining bridges are skipped.
func prepareMCPBridges(ctx context.Context, sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, logger func(string)) {
	for name, def := range cfg.MCP.ExtraServers {
		if ctx.Err() != nil {
			return // shutting down — no further bridge to check
		}
		command, _ := def["command"].(string)
		if command == "" {
			continue // http/sse server — starts no process, nothing to check
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

		if u := envMap["UNREAL_MCP_URL"]; u != "" && ctx.Err() == nil {
			go waitForEndpoint(name, u, logger)
		}
	}
}

// prepareNodeBridge makes sure the script and node_modules of a Node bridge are
// present, with an npm install unless node_modules carries the marker. ok
// false = the bridge is unusable. After a failed install over a node_modules
// that was there before, the bridge stays usable and the smoke test judges
// what that install left — npm may have changed it, and the earlier tree may
// itself be the leftover of a failed install.
func prepareNodeBridge(ctx context.Context, sup *supervisor.Supervisor, cfg *config.Config, name string, args []string, agentWorkdir string, logger func(string)) (m bridgeModules, ok bool) {
	var script string
	for _, a := range args {
		if strings.HasSuffix(a, ".js") || strings.HasSuffix(a, ".mjs") || strings.HasSuffix(a, ".cjs") {
			script = a
			break
		}
	}
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
	logger(fmt.Sprintf("MCP server '%s': %s — installing dependencies (npm install, at most %s) in %s …", name, state, bridgeInstallTimeout, dir))
	ictx, cancel := context.WithTimeout(ctx, bridgeInstallTimeout)
	defer cancel()
	res, err := sup.RunOnceContext(ictx, npm, []string{"install"}, dir, nil, "prepare:mcp:"+name)
	var cause, hint string
	switch {
	case err != nil && ctx.Err() != nil:
		logger("MCP server '" + name + "': npm install aborted — the launcher is shutting down; the next start installs again.")
		return bridgeModules{}, false
	case errors.Is(err, context.DeadlineExceeded):
		cause = fmt.Sprintf("npm install timed out after %s", bridgeInstallTimeout)
		hint = " Registry or proxy unreachable? Run npm install in " + dir + " by hand."
	case err != nil:
		cause = "npm install failed: " + err.Error()
	case res.ExitCode != 0:
		cause = fmt.Sprintf("npm install exit %d", res.ExitCode)
	default:
		// Not marked yet: npm can call a package cut short mid-extraction up
		// to date, so only the smoke test can vouch for the result.
		logger("MCP server '" + name + "': dependencies installed.")
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
// reports whether the bridge answered.
//
// The bridge runs in a job object, as every service and one-shot command
// does (only the short-lived taskkill/pkill and reg query helpers run
// without one), so ending the trial — answer, timeout or parent cancelled —
// ends what the bridge started as well, and a launcher crash leaves nothing
// behind.
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
	// Wait below closes stdout, which ends this reader.
	answered := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if strings.Contains(line, `"serverInfo"`) || (strings.Contains(line, `"result"`) && strings.Contains(line, `"id":1`)) {
				answered <- true
				return
			}
		}
		answered <- false
	}()
	ok := false
	select {
	case ok = <-answered:
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
	reason := "no initialize response"
	if ctx.Err() == context.DeadlineExceeded {
		reason = fmt.Sprintf("timeout after %s", smokeTestTimeout)
	}
	if detail != "" {
		logger(fmt.Sprintf("WARN MCP server '%s': smoke test failed (%s):\n%s", name, reason, stderrTail(detail, 15)))
	} else {
		logger("WARN MCP server '" + name + "': smoke test failed (" + reason + ", no stderr)")
	}
	return false
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

// waitForEndpoint reports as soon as the in-editor HTTP server (UNREAL_MCP_URL)
// is reachable — the editor needs time to start up. If no connection is made
// within 5 minutes, there is a clear warning naming the usual suspects.
func waitForEndpoint(name, rawURL string, logger func(string)) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		logger("WARN MCP server '" + name + "': UNREAL_MCP_URL not parseable: " + rawURL)
		return
	}
	addr := u.Host
	if u.Port() == "" {
		addr += ":80"
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err == nil {
			_ = conn.Close()
			logger("MCP server '" + name + "': in-editor server " + rawURL + " is reachable.")
			return
		}
		time.Sleep(5 * time.Second)
	}
	logger("WARN MCP server '" + name + "': in-editor server " + rawURL + " not reachable after 5 minutes — is the editor running with the plugin? Firewall? Is the server listening on localhost only?")
}
