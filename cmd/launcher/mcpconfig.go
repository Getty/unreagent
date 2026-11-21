package main

import (
	"encoding/json"
	"os"
	"path/filepath"

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

// writeMCPConfigs writes the MCP config to the configured file targets.
func writeMCPConfigs(outputs []config.MCPOutput, servers map[string]interface{}, baseDir string, logger func(string)) {
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
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			logger("WARN MCP config (" + path + "): " + err.Error())
			continue
		}
		format := out.Format
		if format == "" {
			format = "mcp_json"
		}
		logger("MCP config written: " + path + " (" + format + ")")
	}
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
