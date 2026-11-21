package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
)

// buildMCPServers assembles the mcpServers map: our launcher server (HTTP) plus
// all additional (in-editor) MCP servers from the config. If a bearer token is
// set, the unreagent entry gets the matching Authorization header, so the
// embedded agent (Claude Code) does not lock itself out.
func buildMCPServers(cfg *config.Config, mcpURL string) map[string]interface{} {
	unreagent := map[string]interface{}{"type": "http", "url": mcpURL}
	if cfg.MCP.Token != "" {
		unreagent["headers"] = map[string]interface{}{
			"Authorization": "Bearer " + cfg.MCP.Token,
		}
	}
	servers := map[string]interface{}{
		config.MCPServerName: unreagent,
	}
	for name, def := range cfg.MCP.ExtraServers {
		servers[name] = def
	}
	return servers
}

// writeMCPConfigs writes the MCP config to the configured file targets, each
// replaced atomically and private to its owner (writeFileAtomic, tightening
// an open file): it holds the bearer token and the extra servers' env
// values. With tokenSet, a target inside projectDir that git does not ignore
// gets a WARN (unignoredTokenFile). The WARN also goes to alert unless alert
// is nil, as run() passes it: stderr in window mode, where the log is a file
// and the console the agent's.
func writeMCPConfigs(outputs []config.MCPOutput, servers map[string]interface{}, baseDir, projectDir string, tokenSet bool, logger func(string), alert io.Writer) {
	for _, out := range outputs {
		path := out.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		var payload interface{}
		switch out.Format {
		case "vscode":
			payload = map[string]interface{}{"servers": toVSCode(servers), "inputs": []interface{}{}}
		default: // mcp_json
			payload = map[string]interface{}{"mcpServers": servers}
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			logger("WARN MCP config (" + path + "): " + err.Error())
			continue
		}
		was, err := writeFileAtomic(path, append(b, '\n'), true)
		if was != 0 {
			logger(fmt.Sprintf("MCP config %s: mode %04o tightened to 0600 — it can hold the bearer token and the extra servers' env values", path, was))
		}
		if err != nil {
			logger("WARN MCP config (" + path + "): " + err.Error())
			continue
		}
		format := out.Format
		if format == "" {
			format = "mcp_json"
		}
		logger("MCP config written: " + path + " (" + format + ")")
		if unignoredTokenFile(path, projectDir, tokenSet) {
			line := "WARN MCP config " + path + " holds the bearer token (mcp.token), and git does not ignore it — add it to .gitignore (and git rm --cached it if git already tracks it), or the token can end up in a commit"
			logger(line)
			if alert != nil {
				fmt.Fprintln(alert, "unreagent: "+line)
			}
		}
	}
}

// agentMCPConfigName is the name of the file writeAgentMCPConfig writes.
const agentMCPConfigName = "mcp-config.json"

// writeAgentMCPConfig writes servers as {"mcpServers": …} to
// mcp-config.json in a directory of its own in the temp directory and
// returns the file's path, for the agent's --mcp-config (k181). The config
// holds the bearer token and the extra servers' env values, so it goes into
// a file and never inline onto the agent's command line, which every local
// user can read (/proc/<pid>/cmdline, ps). os.MkdirTemp creates the
// directory 0700 and writeFileAtomic the file 0600; on Windows the mode
// bits do not apply, and the per-user %TEMP% keeps them private. The caller
// removes the directory (removeAgentMCPConfig); after an error nothing is
// left behind. The error is meant to end the launcher: falling back to the
// inline JSON would put the token back on the command line.
func writeAgentMCPConfig(servers map[string]interface{}) (string, error) {
	dir, err := os.MkdirTemp("", "unreagent-")
	if err != nil {
		return "", agentMCPConfigError(err)
	}
	path := filepath.Join(dir, agentMCPConfigName)
	b, _ := json.MarshalIndent(map[string]interface{}{"mcpServers": servers}, "", "  ")
	if _, err := writeFileAtomic(path, append(b, '\n'), true); err != nil {
		_ = os.RemoveAll(dir)
		return "", agentMCPConfigError(err)
	}
	return path, nil
}

// agentMCPConfigError is writeAgentMCPConfig's error for err, which names
// the path.
func agentMCPConfigError(err error) error {
	return fmt.Errorf("cannot write the agent's MCP config: %w — the launcher hands it to the agent as a private file in the temp directory, never inline on its command line, where other local users could read mcp.token; make %s writable, or set TMPDIR (TMP on Windows) to a directory that is", err, os.TempDir())
}

// removeAgentMCPConfig removes the directory writeAgentMCPConfig made for
// path, the file with it. A failure is a WARN naming the directory, to
// logger and, unless alert is nil, to alert: what is left behind can hold
// the token.
func removeAgentMCPConfig(path string, logger func(string), alert io.Writer) {
	dir := filepath.Dir(path)
	if err := os.RemoveAll(dir); err != nil {
		line := "WARN cannot remove " + dir + ", the agent's MCP config: " + err.Error() + " — what is left there can hold mcp.token and the extra servers' env values; delete it by hand"
		logger(line)
		if alert != nil {
			fmt.Fprintln(alert, "unreagent: "+line)
		}
	}
}

// redactedValue stands in a log line for a value the line must not show.
const redactedValue = "<redacted>"

// redactAgentArgs returns args as the "Agent command:" log line shows them
// (k44). A --mcp-config value, the next argument or what follows
// "--mcp-config=", is replaced whole: inline it holds the bearer token and
// the extra servers' env values and headers, and in window mode the log is
// unreagent.log in the project directory. The one exception is own, the
// launcher's config file (writeAgentMCPConfig) after its --mcp-config, whose
// path is no secret and helps debugging (k181); a value agent.args puts
// there by hand, a path too, is replaced. A token is replaced in every other
// argument too. The extra servers' env values reach the agent's arguments
// only inside --mcp-config; replaced elsewhere, a value such as "1" or
// "true" would garble the whole line. args itself is left alone: the agent
// is started with it.
func redactAgentArgs(args []string, token, own string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		switch {
		case own != "" && a == own && i > 0 && args[i-1] == "--mcp-config":
			// The launcher's own config file: shown as it is.
		case i > 0 && args[i-1] == "--mcp-config":
			a = redactedValue
		case strings.HasPrefix(a, "--mcp-config="):
			a = "--mcp-config=" + redactedValue
		case token != "":
			a = strings.ReplaceAll(a, token, redactedValue)
		}
		out[i] = a
	}
	return out
}

// gitCheckIgnoreTimeout bounds the git call of unignoredTokenFile; a var so
// tests can shorten it.
var gitCheckIgnoreTimeout = 5 * time.Second

// unignoredTokenFile reports whether path, an MCP config just written, holds
// the bearer token where a commit can take it along: tokenSet, path inside
// projectDir (relToRoot), and `git check-ignore -q <path>`, run in path's
// directory, exits 1, which says git does not ignore the file (a file git
// already tracks exits 1 too, whatever .gitignore says). The warning it
// drives speaks only when it is sure (main agent's decision for k44), so
// every other outcome is false: no token, no project directory, a path
// outside it, git not on PATH, exit 0 (ignored), exit 128 (no work tree, or
// another error) and a git that does not answer within
// gitCheckIgnoreTimeout. Like taskkill and reg query, git is a short helper
// run without a job object; its output goes nowhere.
func unignoredTokenFile(path, projectDir string, tokenSet bool) bool {
	if !tokenSet || projectDir == "" {
		return false
	}
	abs, err := filepath.Abs(path) // git runs in path's directory
	if err != nil {
		return false
	}
	if _, inside := relToRoot(projectDir, abs); !inside {
		return false
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitCheckIgnoreTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, "check-ignore", "-q", abs)
	cmd.Dir = filepath.Dir(abs)
	err = cmd.Run()
	// The timeout is ruled out first: on Windows a killed process exits 1 too.
	var exit *exec.ExitError
	return ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == 1
}

// toVSCode converts the mcpServers map into the VS Code format (stdio servers
// get type:stdio if it is not set).
func toVSCode(servers map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for name, def := range servers {
		m, ok := def.(map[string]interface{})
		if !ok {
			out[name] = def
			continue
		}
		cp := map[string]interface{}{}
		for k, v := range m {
			cp[k] = v
		}
		if _, hasType := cp["type"]; !hasType {
			if _, hasCmd := cp["command"]; hasCmd {
				cp["type"] = "stdio"
			}
		}
		out[name] = cp
	}
	return out
}
