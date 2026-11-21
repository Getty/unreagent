package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/skills"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// registerTools registers all MCP tools. The description texts are deliberately
// verbose — they are the "manual" the agent sees automatically. skillSet is the
// embedded skill set setupSkills loaded (nil while skills are disabled).
func registerTools(srv *mcp.Server, sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, skillSet []skills.Skill, logger func(string)) {
	noArgs := map[string]interface{}{"type": "object", "additionalProperties": false}

	srv.AddTool(mcp.Tool{
		Name:        "status",
		Description: "Returns the status of all managed processes (Unreal Editor, agent): running/stopped, PID, restart count, \"lastError\": why the last start failed (absent once a start succeeds), and \"lastExit\": how its last run that ended on its own ended, e.g. \"exit 1\" (absent until one ends; ue_start and ue_restart clear it). Also lists the one-off commands available to run_command. Use this to check whether the editor is running before you control it. A service may also come back with \"unresponsive\": true — its control loop did not answer within the internal timeout, so running/pid are unknown and sent as false/0. That is not the same as stopped: do not call ue_start on it, re-check status instead.",
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
		Description: "Starts the Unreal Editor if it is not running. No-op if it is already running. Fails, naming the start error, when the editor is not running afterwards; running only means it was spawned, so check status after a few seconds for one that died at once (lastExit).",
		InputSchema: noArgs,
		Handler:     serviceAction(sup, "ue", sup.StartService, true),
	})
	srv.AddTool(mcp.Tool{
		Name:        "ue_stop",
		Description: "Stops the Unreal Editor and prevents auto-restart until it is explicitly started again. Terminates the whole process tree (no orphan processes).",
		InputSchema: noArgs,
		Handler:     serviceAction(sup, "ue", sup.StopService, false),
	})
	srv.AddTool(mcp.Tool{
		Name:        "ue_restart",
		Description: "Restarts the Unreal Editor (stop + start); starts it if it is stopped. Use it when the editor hangs, or to load C++ modules built while it kept running. To compile, stop it first instead (see run_command). Fails, naming the start error, when the editor is not running afterwards; running only means it was spawned, so check status after a few seconds for one that died at once (lastExit).",
		InputSchema: noArgs,
		Handler:     serviceAction(sup, "ue", sup.RestartService, true),
	})

	srv.AddTool(mcp.Tool{
		Name:        "logs",
		Description: fmt.Sprintf("Returns the last output lines (stdout+stderr) of a supervised process, from a buffer that keeps only its last %[1]d lines. service='ue' holds the editor's own output: startup, errors, the last lines before a crash. service='agent' exists only when the launcher runs its own agent, and stays empty while that agent runs interactively in the console (the default); only a headless agent's output is captured. When you ask for more than it still holds and it has dropped older lines, the reply starts with [... N lines omitted; the log keeps only the last %[1]d]; a larger lines value does not bring those back. A reply holds at most %[2]d KiB: past that its oldest bytes go, of a single long line too, and a line [... N bytes omitted; %[3]s] takes their place.", supervisor.LogLines, toolReplyBytes>>10, replyCutReason),
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service": map[string]interface{}{"type": "string", "description": "Service name (ue, agent). Default: ue"},
				"lines":   map[string]interface{}{"type": "integer", "description": fmt.Sprintf("Number of lines (default 50; at most %d are kept)", supervisor.LogLines)},
			},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			service := getString(args, "service", "ue")
			n, err := getInt(args, "lines", 50)
			if err != nil {
				return errResult(err)
			}
			lines, dropped, err := sup.Logs(service, n)
			if err != nil {
				return errResult(err)
			}
			head := ""
			// n <= 0 asks for the whole buffer; n past what it holds reaches
			// back into the lines it dropped.
			if dropped > 0 && (n <= 0 || n > len(lines)) {
				head = omittedMarker(dropped, fmt.Sprintf("the log keeps only the last %d", supervisor.LogLines)) + "\n"
			}
			return mcp.ToolResult{Text: capReply(head, lines)}
		},
	})

	// cutNote tells the one-shot tools' callers how a cut shows: the line
	// marker of commandReply and capReply's byte marker below it. The
	// supervisor's own byte marker for a middle it dropped sits more than the
	// cap above the end of the output, so the cap nearly always cuts it away,
	// and the note names that drop itself. When the kept lines reach back
	// past it, neither count includes it (k175).
	cutNote := "A cut is marked by a line: [... N lines omitted] at the top, then [... N bytes omitted; " + replyCutReason + "] where the oldest bytes of the rest went, of a single long line too. " +
		"Past 2 MiB the middle of the output is dropped as well, and with long lines neither count includes it."

	cmdNames := sup.CommandNames()
	srv.AddTool(mcp.Tool{
		Name: "run_command",
		Description: fmt.Sprintf("Runs a preconfigured one-off command and returns once it exits, with its exit code and the last %d lines of its output (stdout+stderr merged). ", toolOutputLines) +
			cutNote + " What was cut, which can hold the first compile errors, is gone: that output is kept nowhere else, and logs does not have it. " +
			"Typical commands: compile (build the C++ modules), package (create a build). Available commands: " +
			strings.Join(cmdNames, ", ") + ". Stop the editor (ue_stop) before 'compile', since UnrealBuildTool refuses to build while Live Coding is active, and ue_start it afterwards to load the new modules.",
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
				Text:    commandReply(res, toolOutputLines),
				IsError: res.ExitCode != 0,
			}
		},
	})

	// --- skills tool ---
	if cfg.Skills.ViaMCP() {
		srv.AddTool(skillsTool(skillSet))
	}

	// --- runtime tools ---
	if cfg.Runtimes.Python.Enabled {
		dir := runtimeDir(cfg.Runtimes.Python.Project, agentWorkdir)
		uv := cfg.Runtimes.Python.UV
		srv.AddTool(mcp.Tool{
			Name:        "run_python",
			Description: "Runs Python code in a clean, uv-managed environment and returns the exit code and the last " + fmt.Sprint(toolOutputLines) + " lines of stdout+stderr. " + cutNote + " The environment (venv, dependencies from pyproject.toml/requirements, matching Python version) is provided automatically by uv — you do NOT need to create a venv, install anything or analyse the environment. Just pass the code. The script is stored in the project directory and executed there, so modules sitting next to the pyproject.toml are importable directly by name. For additional packages use dependencies declared in pyproject.toml; an ad-hoc 'import' only works for packages that are already present.",
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
			Description: "Runs Node.js code in the project context and returns the exit code and the last " + fmt.Sprint(toolOutputLines) + " lines of stdout+stderr. " + cutNote + " The script is stored in the project directory and executed there, so bare imports are resolved via the project's node_modules — you do not need to set up or analyse the environment yourself. The code ALWAYS runs as an ES module, regardless of package.json: import/export and top-level await work, require is not defined. Reach CommonJS packages via a dynamic import() or via createRequire from 'node:module'.",
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
				nameKeys := []string{"tool_name", "toolName", "name", "tool"}
				toolName := firstString(args, nameKeys...)
				toolInput := firstMap(args, "tool_input", "input", "arguments", "parameters")
				// A request we cannot attribute to a tool and its input fails
				// closed: Decide("", {}) would let allow_all (or any deny list
				// that cannot match "") allow it, with updatedInput {}.
				var dec config.Decision
				switch {
				case toolName == "":
					dec.Message = fmt.Sprintf("unreadable permission request: no tool name (looked for %s; got keys %q)", strings.Join(nameKeys, ", "), slices.Sorted(maps.Keys(args)))
					for _, k := range nameKeys {
						if _, isString := args[k].(string); args[k] != nil && !isString {
							dec.Message = fmt.Sprintf("unreadable permission request: tool name is not a string (%s is %T)", k, args[k])
							break
						}
					}
				case toolInput == nil:
					dec.Message = fmt.Sprintf("unreadable permission request: no tool input object (looked for tool_input, input, arguments, parameters; got keys %q)", slices.Sorted(maps.Keys(args)))
				default:
					dec = cfg.Permissions.Decide(toolName, toolInput)
				}
				var payload map[string]interface{}
				if dec.Allow {
					payload = map[string]interface{}{"behavior": "allow", "updatedInput": toolInput}
				} else {
					payload = map[string]interface{}{"behavior": "deny", "message": dec.Message}
				}
				b, _ := json.Marshal(payload)
				logName := toolName
				if logName == "" {
					logName = "<unknown>"
				}
				logger(fmt.Sprintf("[approve] %s → %s (%s)", logName, allowWord(dec.Allow), dec.Message))
				return mcp.ToolResult{Text: string(b)}
			},
		})
	}
}

// serviceAction builds a handler for a lifecycle action. starts marks an action
// meant to leave the service running (start, restart): when it is not running
// afterwards, the result is an error naming the start error. The supervisor
// answers only after the start attempt, so the snapshot does not race it.
func serviceAction(sup *supervisor.Supervisor, name string, fn func(string) (supervisor.ServiceStatus, error), starts bool) mcp.ToolHandler {
	return func(map[string]interface{}) mcp.ToolResult {
		st, err := fn(name)
		if err != nil {
			return errResult(err)
		}
		b, _ := json.Marshal(st)
		if starts && !st.Running {
			return mcp.ToolResult{Text: "Error: " + startFailure(name, st) + "\n" + string(b), IsError: true}
		}
		return mcp.ToolResult{Text: string(b)}
	}
}

// startFailure says that a start left the service not running, and why: the
// ue_start / ue_restart error and the launcher console's answer (commandLoop).
func startFailure(name string, st supervisor.ServiceStatus) string {
	return fmt.Sprintf("service %q did not start: %s", name, st.LastError)
}

func allowWord(b bool) string {
	if b {
		return "allow"
	}
	return "deny"
}
