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

// prepareMCPBridges checks all stdio extraServers (e.g. the UE LLM Toolkit
// bridge) completely once BEFORE the services start and complains loudly about
// every link in the chain, instead of letting the agent run into a meaningless
// MCP connect error (-32000):
//  1. Binary findable (node etc. on PATH)?
//  2. Node bridges: script present? node_modules there? (otherwise npm install)
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

		if strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe") == "node" {
			if !prepareNodeBridge(ctx, sup, cfg, name, args, agentWorkdir, logger) {
				continue
			}
		}

		smokeTestMCP(ctx, name, command, args, env, agentWorkdir, logger)

		if u := envMap["UNREAL_MCP_URL"]; u != "" && ctx.Err() == nil {
			go waitForEndpoint(name, u, logger)
		}
	}
}

// prepareNodeBridge makes sure the script and node_modules of a Node bridge are
// present (npm install if needed). false = the bridge is unusable.
func prepareNodeBridge(ctx context.Context, sup *supervisor.Supervisor, cfg *config.Config, name string, args []string, agentWorkdir string, logger func(string)) bool {
	var script string
	for _, a := range args {
		if strings.HasSuffix(a, ".js") || strings.HasSuffix(a, ".mjs") || strings.HasSuffix(a, ".cjs") {
			script = a
			break
		}
	}
	if script == "" {
		return true // no script recognisable — the smoke test decides
	}
	if !filepath.IsAbs(script) {
		script = filepath.Join(agentWorkdir, script)
	}
	if !fileExists(script) {
		logger("WARN MCP server '" + name + "': script not found: " + script + " — is the plugin installed?")
		return false
	}
	dir := filepath.Dir(script)
	if !fileExists(filepath.Join(dir, "package.json")) || fileExists(filepath.Join(dir, "node_modules")) {
		return true
	}
	npm := cfg.Runtimes.Node.Npm
	if npm == "" {
		npm = "npm"
	}
	logger(fmt.Sprintf("MCP server '%s': node_modules missing — installing dependencies (npm install, at most %s) in %s …", name, bridgeInstallTimeout, dir))
	ictx, cancel := context.WithTimeout(ctx, bridgeInstallTimeout)
	defer cancel()
	res, err := sup.RunOnceContext(ictx, npm, []string{"install"}, dir, nil, "prepare:mcp:"+name)
	// An install cut short can leave a partial node_modules, and the check
	// above then skips the install on the next start.
	partial := "A partial node_modules left in " + dir + " makes the next start skip the install — remove it first."
	switch {
	case err != nil && ctx.Err() != nil:
		logger("MCP server '" + name + "': npm install aborted — the launcher is shutting down. " + partial)
		return false
	case errors.Is(err, context.DeadlineExceeded):
		logger(fmt.Sprintf("WARN MCP server '%s': npm install timed out after %s — bridge will not start. Registry or proxy unreachable? Run npm install in %s by hand. %s\n%s",
			name, bridgeInstallTimeout, dir, partial, tailLines(res.Output, 20)))
		return false
	case err != nil:
		logger("WARN MCP server '" + name + "': npm install failed: " + err.Error() + " — bridge will not start")
		return false
	}
	if res.ExitCode != 0 {
		logger(fmt.Sprintf("WARN MCP server '%s': npm install exit %d — bridge will not start\n%s",
			name, res.ExitCode, tailLines(res.Output, 20)))
		return false
	}
	logger("MCP server '" + name + "': dependencies installed.")
	return true
}

// smokeTestMCP starts the stdio server once as a trial, sends a real MCP
// initialize and waits for the answer. If the start fails, the real cause
// (stderr) is in the log instead of just a connect error at the agent.
//
// The bridge runs in a job object like every process the launcher spawns, so
// ending the trial — answer, timeout or parent cancelled — ends what the
// bridge started as well, and a launcher crash leaves nothing behind.
func smokeTestMCP(parent context.Context, name, command string, args, env []string, dir string, logger func(string)) {
	if parent.Err() != nil {
		return
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
		return
	}
	job, err := supervisor.NewJob()
	if err != nil {
		logger("WARN MCP server '" + name + "': smoke test: job object could not be created: " + err.Error() + " — a launcher crash leaves the bridge behind")
	}
	if err := cmd.Start(); err != nil {
		job.Close()
		logger("WARN MCP server '" + name + "': smoke test failed to start: " + err.Error())
		return
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
		return
	}
	if parent.Err() != nil {
		logger("MCP server '" + name + "': smoke test aborted — the launcher is shutting down")
		return
	}
	detail := strings.TrimSpace(stderr.String())
	reason := "no initialize response"
	if ctx.Err() == context.DeadlineExceeded {
		reason = fmt.Sprintf("timeout after %s", smokeTestTimeout)
	}
	if detail != "" {
		logger(fmt.Sprintf("WARN MCP server '%s': smoke test failed (%s):\n%s", name, reason, tailLines(detail, 15)))
	} else {
		logger("WARN MCP server '" + name + "': smoke test failed (" + reason + ", no stderr)")
	}
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
