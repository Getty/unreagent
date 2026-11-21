package main

import (
	"bufio"
	"bytes"
	"context"
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

// prepareMCPBridges checks all stdio extraServers (e.g. the UE LLM Toolkit
// bridge) completely once BEFORE the services start and complains loudly about
// every link in the chain, instead of letting the agent run into a meaningless
// MCP connect error (-32000):
//  1. Binary findable (node etc. on PATH)?
//  2. Node bridges: script present? node_modules there? (otherwise npm install)
//  3. Smoke test: start the server, send MCP initialize, wait for the answer.
//  4. UNREAL_MCP_URL: report asynchronously as soon as the in-editor server is
//     reachable — or warn if it is not.
func prepareMCPBridges(sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, logger func(string)) {
	for name, def := range cfg.MCP.ExtraServers {
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
			if !prepareNodeBridge(sup, cfg, name, args, agentWorkdir, logger) {
				continue
			}
		}

		smokeTestMCP(name, command, args, env, agentWorkdir, logger)

		if u := envMap["UNREAL_MCP_URL"]; u != "" {
			go waitForEndpoint(name, u, logger)
		}
	}
}

// prepareNodeBridge makes sure the script and node_modules of a Node bridge are
// present (npm install if needed). false = the bridge is unusable.
func prepareNodeBridge(sup *supervisor.Supervisor, cfg *config.Config, name string, args []string, agentWorkdir string, logger func(string)) bool {
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
	logger("MCP server '" + name + "': node_modules missing — installing dependencies (npm install) in " + dir + " …")
	res, err := sup.RunOnce(npm, []string{"install"}, dir, nil, "prepare:mcp:"+name)
	if err != nil {
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
func smokeTestMCP(name, command string, args, env []string, dir string, logger func(string)) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err1 := cmd.StdinPipe()
	stdout, err2 := cmd.StdoutPipe()
	if err1 != nil || err2 != nil {
		logger("WARN MCP server '" + name + "': smoke test could not open pipes")
		return
	}
	if err := cmd.Start(); err != nil {
		logger("WARN MCP server '" + name + "': smoke test failed to start: " + err.Error())
		return
	}
	_, _ = io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"unreagent-smoketest","version":"`+version+`"}}}`+"\n")
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	ok := false
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, `"serverInfo"`) || (strings.Contains(line, `"result"`) && strings.Contains(line, `"id":1`)) {
			ok = true
			break
		}
	}
	_ = stdin.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if ok {
		logger("MCP server '" + name + "': smoke test OK — bridge answers initialize.")
		return
	}
	detail := strings.TrimSpace(stderr.String())
	reason := "no initialize response"
	if ctx.Err() == context.DeadlineExceeded {
		reason = "timeout after 20s"
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
