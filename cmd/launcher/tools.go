package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// registerTools registers all MCP tools. The description texts are deliberately
// verbose — they are the "manual" the agent sees automatically.
func registerTools(srv *mcp.Server, sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, logger func(string)) {
	noArgs := map[string]interface{}{"type": "object", "additionalProperties": false}

	srv.AddTool(mcp.Tool{
		Name:        "status",
		Description: "Returns the status of all managed processes (Unreal Editor, agent): running/stopped, PID, restart count. Also lists the one-off commands available to run_command. Use this to check whether the editor is running before you control it. A service may also come back with \"unresponsive\": true — its control loop did not answer within the internal timeout, so running/pid are unknown and sent as false/0. That is not the same as stopped: do not call ue_start on it, re-check status instead.",
		InputSchema: noArgs,
		Handler: func(map[string]interface{}) mcp.ToolResult {
			payload := map[string]interface{}{
				"services": sup.Status(),
				"commands": sup.CommandNames(),
			}
			b, _ := json.MarshalIndent(payload, "", "  ")
			return mcp.ToolResult{Text: string(b)}
		},
	})

	srv.AddTool(mcp.Tool{
		Name:        "ue_start",
		Description: "Starts the Unreal Editor if it is not running. No-op if it is already running.",
		InputSchema: noArgs,
		Handler:     serviceAction(sup, "ue", sup.StartService),
	})
	srv.AddTool(mcp.Tool{
		Name:        "ue_stop",
		Description: "Stops the Unreal Editor and prevents auto-restart until it is explicitly started again. Terminates the whole process tree (no orphan processes).",
		InputSchema: noArgs,
		Handler:     serviceAction(sup, "ue", sup.StopService),
	})
	srv.AddTool(mcp.Tool{
		Name:        "ue_restart",
		Description: "Restarts the Unreal Editor (stop + start). Use this after a C++ build so the editor loads the new modules, or when the editor hangs.",
		InputSchema: noArgs,
		Handler:     serviceAction(sup, "ue", sup.RestartService),
	})

	srv.AddTool(mcp.Tool{
		Name:        "logs",
		Description: "Returns the last output lines of a service (stdout+stderr). Use service='ue' for editor/build output, service='agent' for the agent. Handy for reading compile errors or crash messages.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service": map[string]interface{}{"type": "string", "description": "Service name (ue, agent). Default: ue"},
				"lines":   map[string]interface{}{"type": "integer", "description": "Number of lines (default 50)"},
			},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			service := getString(args, "service", "ue")
			n := getInt(args, "lines", 50)
			lines, err := sup.Logs(service, n)
			if err != nil {
				return errResult(err)
			}
			return mcp.ToolResult{Text: strings.Join(lines, "\n")}
		},
	})

	cmdNames := sup.CommandNames()
	srv.AddTool(mcp.Tool{
		Name: "run_command",
		Description: "Runs a preconfigured one-off command synchronously and returns its output + exit code. Typical commands: compile (build the C++ modules), package (create a build). Available commands: " +
			strings.Join(cmdNames, ", ") + ". After 'compile', ue_restart is recommended.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Name of the command (see status.commands)"},
			},
			"required": []string{"name"},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			name := getString(args, "name", "")
			if name == "" {
				return mcp.ToolResult{Text: "Error: 'name' missing", IsError: true}
			}
			res, err := sup.RunCommand(name)
			if err != nil {
				return errResult(err)
			}
			return mcp.ToolResult{
				Text:    fmt.Sprintf("exit %d\n\n%s", res.ExitCode, tailLines(res.Output, 300)),
				IsError: res.ExitCode != 0,
			}
		},
	})

	// --- runtime tools ---
	if cfg.Runtimes.Python.Enabled {
		dir := runtimeDir(cfg.Runtimes.Python.Project, agentWorkdir)
		uv := cfg.Runtimes.Python.UV
		srv.AddTool(mcp.Tool{
			Name:        "run_python",
			Description: "Runs Python code in a clean, uv-managed environment and returns stdout/stderr + exit code. The environment (venv, dependencies from pyproject.toml/requirements, matching Python version) is provided automatically by uv — you do NOT need to create a venv, install anything or analyse the environment. Just pass the code. The script is stored in the project directory and executed there, so modules sitting next to the pyproject.toml are importable directly by name. For additional packages use dependencies declared in pyproject.toml; an ad-hoc 'import' only works for packages that are already present.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code": map[string]interface{}{"type": "string", "description": "Python code to execute"},
				},
				"required": []string{"code"},
			},
			Handler: scriptAction(sup, uv, []string{"run", "python"}, dir, "py", "python"),
		})
	}
	if cfg.Runtimes.Node.Enabled {
		dir := runtimeDir(cfg.Runtimes.Node.Project, agentWorkdir)
		node := cfg.Runtimes.Node.Node
		srv.AddTool(mcp.Tool{
			Name:        "run_node",
			Description: "Runs Node.js code in the project context and returns stdout/stderr + exit code. The script is stored in the project directory and executed there, so bare imports are resolved via the project's node_modules — you do not need to set up or analyse the environment yourself. The code ALWAYS runs as an ES module, regardless of package.json: import/export and top-level await work, require is not defined. Reach CommonJS packages via a dynamic import() or via createRequire from 'node:module'.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code": map[string]interface{}{"type": "string", "description": "JavaScript code to execute"},
				},
				"required": []string{"code"},
			},
			Handler: scriptAction(sup, node, nil, dir, "mjs", "node"),
		})
	}

	// --- file tools (restricted to cfg.Files.Root) ---
	if cfg.Files.Enabled {
		root := cfg.Files.Root
		if root == "" {
			root = agentWorkdir
		}
		registerFileTools(srv, root, cfg.Files.ReadOnly)
	}

	// --- permission tool ---
	if cfg.Permissions.Enabled {
		srv.AddTool(mcp.Tool{
			Name:        "approve",
			Description: "Permission prompt tool for the Claude Code CLI (--permission-prompt-tool). Called by the harness, not directly by the model. Decides based on the launcher policy whether a tool call is allowed.",
			InputSchema: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": true,
			},
			Handler: func(args map[string]interface{}) mcp.ToolResult {
				toolName := firstString(args, "tool_name", "toolName", "name", "tool")
				toolInput := firstMap(args, "tool_input", "input", "arguments", "parameters")
				if toolInput == nil {
					toolInput = map[string]interface{}{}
				}
				dec := cfg.Permissions.Decide(toolName, toolInput)
				var payload map[string]interface{}
				if dec.Allow {
					payload = map[string]interface{}{"behavior": "allow", "updatedInput": toolInput}
				} else {
					payload = map[string]interface{}{"behavior": "deny", "message": dec.Message}
				}
				b, _ := json.Marshal(payload)
				logger(fmt.Sprintf("[approve] %s → %s (%s)", toolName, allowWord(dec.Allow), dec.Message))
				return mcp.ToolResult{Text: string(b)}
			},
		})
	}
}

// serviceAction builds a handler for a lifecycle action.
func serviceAction(sup *supervisor.Supervisor, name string, fn func(string) (supervisor.ServiceStatus, error)) mcp.ToolHandler {
	return func(map[string]interface{}) mcp.ToolResult {
		st, err := fn(name)
		if err != nil {
			return errResult(err)
		}
		b, _ := json.Marshal(st)
		return mcp.ToolResult{Text: string(b)}
	}
}

func allowWord(b bool) string {
	if b {
		return "allow"
	}
	return "deny"
}
