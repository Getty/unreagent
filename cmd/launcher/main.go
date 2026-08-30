// Command launcher (unreagent) starts and supervises the Unreal Editor and an
// agent (e.g. Claude Code) and offers the agent an MCP server through which it
// can control the editor (start/stop/restart), run commands (compile, package),
// read logs and execute Python/Node code in prepared environments.
package main

import (
	"bufio"
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/supervisor"

	"gopkg.in/yaml.v3"
)

// version is set in the release build via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	// Sub-command dispatch must run before flag.Parse so a stray `-x` after a
	// sub-command doesn't trip the flag parser. Sub-commands are self-contained.
	//
	// extractSubcommand scans os.Args for the first positional argument,
	// skipping `-key value` pairs so both `unreagent -config foo hermes-setup`
	// and `unreagent hermes-setup -config foo` recognize the sub-command without
	// swallowing `-config` or `foo`. Each sub-command then re-parses its own
	// flags from the remaining argv via its own FlagSet.
	if sub, _ := extractSubcommand(); sub != "" {
		switch sub {
		case "hermes-setup":
			return runHermesSetup()
		}
	}

	cfgPath := flag.String("config", "", "Path to unreagent.yaml (default: next to the executable)")
	noAgent := flag.Bool("no-agent", false, "Don't start the agent (UE + MCP server only; an external agent can connect)")
	filesFlag := flag.Bool("files", false, "Expose file tools (read/write/list/edit) over the MCP server")
	writeMcp := flag.String("write-mcp-config", "", "Also write the MCP config as a .mcp.json to this path")
	flag.Parse()

	logOut := io.Writer(os.Stdout)
	logger := func(line string) {
		fmt.Fprintf(logOut, "%s  %s\n", time.Now().Format("15:04:05"), line)
	}

	cfg, info, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	logger(fmt.Sprintf("unreagent %s — Config: %s", version, info.ConfigPath))
	if info.LocalPath != "" {
		logger("Local overlay: " + info.LocalPath)
	}
	if info.EngineRoot != "" {
		logger("Engine: " + info.EngineRoot)
	} else {
		logger("WARN engine not found — set UE_ROOT or engineRoot in unreagent.local.yaml")
	}
	if info.Project != "" {
		logger("Project: " + info.Project)
	} else {
		logger("WARN no .uproject found next to the config — set unreal.project")
	}
	// CLI overrides.
	if *noAgent && cfg.Agent.Enabled {
		cfg.Agent.Enabled = false
		logger("Flag -no-agent: agent will not be started (MCP server stays available)")
	}
	if *filesFlag && !cfg.Files.Enabled {
		cfg.Files.Enabled = true
	}
	if *writeMcp != "" {
		cfg.MCP.WriteConfig = append(cfg.MCP.WriteConfig, config.MCPOutput{Path: *writeMcp, Format: "mcp_json"})
	}

	// Interactive agent: it inherits the real console (TTY). So that Claude has
	// the window to itself, we redirect the launcher logs into a file and skip
	// our own stdin command loop. Off in headless -p mode.
	agentInteractive := false
	if cfg.Agent.Enabled {
		agentInteractive = !hasPromptArg(cfg.Agent.Args)
		if cfg.Agent.Window != nil {
			agentInteractive = *cfg.Agent.Window
		}
	}
	if agentInteractive {
		dir := "."
		if info.Project != "" {
			dir = filepath.Dir(info.Project)
		}
		logPath := filepath.Join(dir, "unreagent.log")
		if f, ferr := os.Create(logPath); ferr == nil {
			logOut = f
			defer f.Close()
			fmt.Printf("unreagent %s\n", version)
			fmt.Printf("Launcher logs -> %s   (Unreal runs in the background)\n", logPath)
			fmt.Println("The agent (Claude) takes over this window …")
			fmt.Println()
		}
	}

	warnIfMissing(logger, "unreal.editor", cfg.Unreal.Editor)
	if cfg.Agent.Enabled {
		if _, lookErr := exec.LookPath(cfg.Agent.Command); lookErr != nil {
			logger("WARN agent command not in PATH: " + cfg.Agent.Command + " (set the full path in unreagent.local.yaml)")
		}
	}
	if cfg.Files.Enabled {
		mode := "read/write"
		if cfg.Files.ReadOnly {
			mode = "read-only"
		}
		logger(fmt.Sprintf("File tools active (%s) under: %s", mode, cfg.Files.Root))
	}

	sup := supervisor.New(logger)

	// --- unreal service ---
	ueArgs := append([]string{}, cfg.Unreal.Args...)
	if cfg.Unreal.Project != "" {
		ueArgs = append([]string{cfg.Unreal.Project}, ueArgs...)
	}
	if boolVal(cfg.Unreal.Unattended) && !hasArg(ueArgs, "-unattended") {
		ueArgs = append(ueArgs, "-unattended")
		logger("Unreal: -unattended active (no crash dialog, no recovery prompt)")
	}
	ueProjectDir := ""
	if cfg.Unreal.Project != "" {
		ueProjectDir = filepath.Dir(cfg.Unreal.Project)
	}
	uePreStart := func() {
		if boolVal(cfg.Unreal.KillCrashReporter) {
			killCrashReporter()
		}
		if cfg.Unreal.CleanOnRestart && ueProjectDir != "" {
			cleanRecovery(ueProjectDir, logger)
		}
	}
	sup.AddService(supervisor.ServiceSpec{
		Name:         "ue",
		Command:      cfg.Unreal.Editor,
		Args:         ueArgs,
		Autostart:    !cfg.Unreal.ManualStart,
		Restart:      cfg.Unreal.Restart,
		MaxRestarts:  cfg.Unreal.MaxRestarts,
		RestartDelay: secs(cfg.Unreal.RestartDelaySeconds),
		PreStart:     uePreStart,
	})

	// --- agent service ---
	// onAgentExit is only set once ctx/stop exist (see below); the service holds
	// nothing but an indirection to it.
	var onAgentExit func(success bool)
	agentWorkdir := cfg.Agent.Workdir
	if agentWorkdir == "" && cfg.Unreal.Project != "" {
		agentWorkdir = filepath.Dir(cfg.Unreal.Project)
	}
	// Remove leftover script files before the MCP server can write new ones
	// (see scriptAction).
	sweepStaleScripts(cfg, agentWorkdir, logger)
	mcpURL := "http://" + cfg.MCP.Address + "/mcp"
	var mcpServers map[string]interface{}
	if cfg.MCP.Enabled {
		mcpServers = buildMCPServers(cfg, mcpURL)
	}
	if cfg.Agent.Enabled {
		agentArgs := append([]string{}, cfg.Agent.Args...)
		var agentEnv []string
		for k, v := range cfg.Agent.Env {
			agentEnv = append(agentEnv, k+"="+v)
		}
		if runtime.GOOS == "windows" && cfg.Agent.ClaudeIntegration && (cfg.Agent.PowershellTool == nil || *cfg.Agent.PowershellTool) {
			if _, ok := cfg.Agent.Env["CLAUDE_CODE_USE_POWERSHELL_TOOL"]; !ok {
				agentEnv = append(agentEnv, "CLAUDE_CODE_USE_POWERSHELL_TOOL=1")
			}
		}

		if cfg.MCP.Enabled && cfg.Agent.ClaudeIntegration {
			b, _ := json.Marshal(map[string]interface{}{"mcpServers": mcpServers})
			agentArgs = append(agentArgs, "--mcp-config", string(b))
			if cfg.MCP.Strict {
				agentArgs = append(agentArgs, "--strict-mcp-config")
			}
			if cfg.Permissions.Enabled {
				if agentInteractive {
					// Interactive: --permission-prompt-tool is a headless feature
					// (-p) and aborts otherwise. allow_all -> skip prompts; other
					// modes -> Claude asks in the window (manually).
					if cfg.Permissions.Mode == config.ModeAllowAll {
						agentArgs = append(agentArgs, "--dangerously-skip-permissions")
					}
				} else {
					agentArgs = append(agentArgs, "--permission-prompt-tool", "mcp__"+config.MCPServerName+"__approve")
				}
			}
			logger(fmt.Sprintf("Agent: Claude integration active (%d MCP server(s)%s)",
				len(mcpServers), strictWord(cfg.MCP.Strict)))
		}
		logger("Agent command: " + cfg.Agent.Command + " " + strings.Join(agentArgs, " "))
		sup.AddService(supervisor.ServiceSpec{
			Name:         "agent",
			Command:      cfg.Agent.Command,
			Args:         agentArgs,
			Dir:          agentWorkdir,
			Env:          agentEnv,
			Autostart:    true,
			StartDelay:   secs(cfg.Agent.StartDelaySeconds),
			Restart:      cfg.Agent.Restart,
			MaxRestarts:  cfg.Agent.MaxRestarts,
			RestartDelay: secs(cfg.Agent.RestartDelaySeconds),
			Foreground:   agentInteractive,
			OnExit: func(success bool) {
				if onAgentExit != nil {
					onAgentExit(success)
				}
			},
		})
		if agentInteractive {
			logger("Agent: runs interactively in the foreground (inherits the console)")
		}
	}

	// --- write MCP config files (for external clients) ---
	if cfg.MCP.Enabled && len(cfg.MCP.WriteConfig) > 0 {
		base := agentWorkdir
		if base == "" {
			base = "."
		}
		writeMCPConfigs(cfg.MCP.WriteConfig, mcpServers, base, logger)
	}

	// --- one-off commands ---
	for name, c := range cfg.Commands {
		sup.AddCommand(name, supervisor.CommandSpec{
			Description: c.Description,
			Command:     c.Command,
			Args:        c.Args,
			Dir:         c.Dir,
		})
	}

	// --- MCP server ---
	var httpSrv *http.Server
	if cfg.MCP.Enabled {
		srv := mcp.NewServer(config.MCPServerName, version, logger)
		if cfg.MCP.Token != "" {
			srv.SetToken(cfg.MCP.Token)
			logger("MCP server: bearer auth enabled (mcp.token is set)")
		}
		registerTools(srv, sup, cfg, agentWorkdir, logger)
		mux := http.NewServeMux()
		mux.Handle("/mcp", srv)
		mux.Handle("/", srv)
		// Without timeouts a half-open connection pins a goroutine and a socket
		// forever — a trivial slowloris as soon as mcp.address is not loopback.
		// WriteTimeout covers the handler as well, so it must outlast the
		// slowest tool call: run_command can drive a full UE build for hours.
		// It is a backstop against a wedged connection, not a request budget.
		httpSrv = &http.Server{
			Addr:              cfg.MCP.Address,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      4 * time.Hour,
			IdleTimeout:       120 * time.Second,
		}
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger("MCP server error: " + err.Error())
			}
		}()
		logger("MCP server listening on " + mcpURL)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Agent.Enabled {
		onAgentExit = makeAgentExitHandler(ctx, stop, sup, cfg, logger, agentInteractive)
	}

	if cfg.MCP.Enabled && len(cfg.MCP.ExtraServers) > 0 {
		prepareMCPBridges(sup, cfg, agentWorkdir, logger)
	}

	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	prepareRuntimes(sup, cfg, agentWorkdir, logger)
	// In interactive mode stdin belongs to the agent — no command loop of our own.
	if !agentInteractive {
		go commandLoop(ctx, stop, sup, logger)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		logger("Signal received — stopping all processes …")
	case <-done:
		logger("All services stopped.")
	}
	stop()
	wg.Wait()
	if httpSrv != nil {
		_ = httpSrv.Close()
	}
	logger("Bye.")
	return nil
}

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

// scriptPrefix is the name prefix of the script files scriptAction creates.
const scriptPrefix = "unreagent-"

// scriptExts are the extensions scriptAction assigns (run_python, run_node).
var scriptExts = []string{"py", "mjs"}

// runtimeDir returns the working directory of a runtime: the configured project
// directory, otherwise the agent workdir, otherwise the current directory.
// Storing the script (scriptAction) and sweeping it up (sweepStaleScripts) must
// agree on the very same directory — which is why the decision is made here and
// nowhere else.
func runtimeDir(project, agentWorkdir string) string {
	if project != "" {
		return project
	}
	if agentWorkdir != "" {
		return agentWorkdir
	}
	return "."
}

// scriptAction builds a handler that writes code to a temporary file and runs it
// via command (pre-args + file).
//
// The file deliberately lands IN the working directory of the runtime and not in
// the system temp directory: only there does Node resolve bare specifiers via the
// project's node_modules, and only there is sys.path[0] the project directory for
// Python (so modules sitting next to it become importable). There is deliberately
// no fallback to the system temp directory — it would silently break exactly that
// resolution again.
func scriptAction(sup *supervisor.Supervisor, command string, pre []string, dir, ext, label string) mcp.ToolHandler {
	return func(args map[string]interface{}) mcp.ToolResult {
		code := getString(args, "code", "")
		if code == "" {
			return mcp.ToolResult{Text: "Error: 'code' missing", IsError: true}
		}
		f, err := os.CreateTemp(dir, scriptPrefix+"*."+ext)
		if err != nil {
			return mcp.ToolResult{
				Text: fmt.Sprintf("Error: could not create the script file in the working directory of the %s runtime (%s): %v"+
					"\nThe directory must exist and be writable — check runtimes.%s.project or agent.workdir.",
					label, dir, err, label),
				IsError: true,
			}
		}
		name := f.Name()
		defer os.Remove(name)
		if _, err := f.WriteString(code); err != nil {
			f.Close()
			return errResult(err)
		}
		f.Close()
		callArgs := append(append([]string{}, pre...), name)
		res, err := sup.RunOnce(command, callArgs, dir, nil, label)
		if err != nil {
			return errResult(err)
		}
		return mcp.ToolResult{
			Text:    fmt.Sprintf("exit %d\n\n%s", res.ExitCode, tailLines(res.Output, 300)),
			IsError: res.ExitCode != 0,
		}
	}
}

// registerFileTools registers read/list (+ write/edit, unless readOnly) on the
// MCP server, all paths strictly restricted to root.
func registerFileTools(srv *mcp.Server, root string, readOnly bool) {
	rootAbs, err := filepath.Abs(root)
	if err != nil || root == "" {
		rootAbs, _ = filepath.Abs(".")
	}
	resolve := func(rel string) (string, error) {
		if rel == "" {
			rel = "."
		}
		abs, err := filepath.Abs(filepath.Join(rootAbs, rel))
		if err != nil {
			return "", err
		}
		if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
			return "", fmt.Errorf("path outside the allowed root")
		}
		return abs, nil
	}

	srv.AddTool(mcp.Tool{
		Name:        "read_file",
		Description: "Reads a text file from the UE project (path relative to the project root). Lets an agent read source code/config/logs without being present locally. Large files are truncated to their head (first 256 KiB), marked with …[truncated]; the tail is unreachable.",
		InputSchema: requiredPathSchema("path", "Path to the file, relative to the project root"),
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			if path == "" {
				return mcp.ToolResult{Text: "Error: 'path' missing", IsError: true}
			}
			abs, err := resolve(path)
			if err != nil {
				return errResult(err)
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return errResult(err)
			}
			const max = 256 * 1024
			if len(b) > max {
				return mcp.ToolResult{Text: string(b[:max]) + "\n…[truncated]"}
			}
			return mcp.ToolResult{Text: string(b)}
		},
	})

	srv.AddTool(mcp.Tool{
		Name:        "list_dir",
		Description: "Lists the entries of a directory in the UE project (path relative to the root, empty = root). Directories end with /.",
		InputSchema: optionalPathSchema("path", "Directory relative to the root (empty = root)"),
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			abs, err := resolve(getString(args, "path", "."))
			if err != nil {
				return errResult(err)
			}
			entries, err := os.ReadDir(abs)
			if err != nil {
				return errResult(err)
			}
			var sb strings.Builder
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				sb.WriteString(name)
				sb.WriteByte('\n')
			}
			return mcp.ToolResult{Text: sb.String()}
		},
	})

	if readOnly {
		return
	}

	srv.AddTool(mcp.Tool{
		Name:        "write_file",
		Description: "Writes/overwrites a text file in the UE project (path relative to the root). Missing directories are created. Lets an agent create/modify files without being present locally.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "Path relative to the root"},
				"content": map[string]interface{}{"type": "string", "description": "New file content"},
			},
			"required": []string{"path", "content"},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			if path == "" {
				return mcp.ToolResult{Text: "Error: 'path' missing", IsError: true}
			}
			abs, err := resolve(path)
			if err != nil {
				return errResult(err)
			}
			content, ok := stringArg(args, "content")
			if !ok {
				return mcp.ToolResult{Text: "Error: 'content' missing", IsError: true}
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return errResult(err)
			}
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				return errResult(err)
			}
			return mcp.ToolResult{Text: fmt.Sprintf("written: %s (%d bytes)", path, len(content))}
		},
	})

	srv.AddTool(mcp.Tool{
		Name:        "edit_file",
		Description: "Replaces all occurrences of old_string with new_string in a text file (path relative to the root). Error if old_string does not occur.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":       map[string]interface{}{"type": "string", "description": "Path relative to the root"},
				"old_string": map[string]interface{}{"type": "string", "description": "Text to replace"},
				"new_string": map[string]interface{}{"type": "string", "description": "Replacement text"},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			if path == "" {
				return mcp.ToolResult{Text: "Error: 'path' missing", IsError: true}
			}
			abs, err := resolve(path)
			if err != nil {
				return errResult(err)
			}
			oldS := getString(args, "old_string", "")
			if oldS == "" {
				return mcp.ToolResult{Text: "Error: 'old_string' missing", IsError: true}
			}
			newS, ok := stringArg(args, "new_string")
			if !ok {
				return mcp.ToolResult{Text: "Error: 'new_string' missing", IsError: true}
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return errResult(err)
			}
			content := string(b)
			n := strings.Count(content, oldS)
			if n == 0 {
				return mcp.ToolResult{Text: "Error: 'old_string' not found", IsError: true}
			}
			content = strings.ReplaceAll(content, oldS, newS)
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				return errResult(err)
			}
			return mcp.ToolResult{Text: fmt.Sprintf("%d occurrence(s) replaced in %s", n, path)}
		},
	})
}

// requiredPathSchema describes a single string path argument that the tool
// refuses to run without. The "required" key lets a schema-validating client
// reject the call before it ever reaches the handler.
func requiredPathSchema(name, desc string) map[string]interface{} {
	schema := optionalPathSchema(name, desc)
	schema["required"] = []string{name}
	return schema
}

// optionalPathSchema describes a single string path argument the tool may be
// called without, because the handler substitutes a default. It deliberately
// carries no "required" key: omitting the argument must stay a legal call.
func optionalPathSchema(name, desc string) map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			name: map[string]interface{}{"type": "string", "description": desc},
		},
	}
}

// sweepStaleScripts removes script files that a hard kill of the launcher left
// behind in the working directories of the runtimes: the defer os.Remove in
// scriptAction no longer runs in that case, and ever since scripts are stored
// there, the file sits in the user's project instead of the system temp
// directory. Called at startup, before the MCP server can write new scripts.
func sweepStaleScripts(cfg *config.Config, agentWorkdir string, logger func(string)) {
	var dirs []string
	add := func(d string) {
		for _, seen := range dirs {
			if seen == d {
				return
			}
		}
		dirs = append(dirs, d)
	}
	if cfg.Runtimes.Python.Enabled {
		add(runtimeDir(cfg.Runtimes.Python.Project, agentWorkdir))
	}
	if cfg.Runtimes.Node.Enabled {
		add(runtimeDir(cfg.Runtimes.Node.Project, agentWorkdir))
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // missing/unreadable: scriptAction reports that loudly enough
		}
		for _, e := range entries {
			if e.IsDir() || !isStaleScriptName(e.Name()) {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if err := os.Remove(path); err != nil {
				logger("WARN could not remove stale script file " + path + ": " + err.Error())
				continue
			}
			logger("Stale script file removed: " + path)
		}
	}
}

// isStaleScriptName matches exactly the names os.CreateTemp generates from
// "unreagent-*.<ext>": the prefix, then decimal digits only (the replacement for
// '*'), then one of the script extensions. The user's own files such as
// "unreagent-helper.py" therefore deliberately do not match.
func isStaleScriptName(name string) bool {
	if !strings.HasPrefix(name, scriptPrefix) {
		return false
	}
	rest := name[len(scriptPrefix):]
	dot := strings.LastIndexByte(rest, '.')
	if dot < 1 { // at least one digit before the extension
		return false
	}
	var known bool
	for _, ext := range scriptExts {
		if rest[dot+1:] == ext {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	for i := 0; i < dot; i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	return true
}

// prepareRuntimes warms up the environments (uv sync / npm install) if they are
// configured and a manifest is present. Runs asynchronously.
func prepareRuntimes(sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, logger func(string)) {
	if cfg.Runtimes.Python.Enabled && cfg.Runtimes.Python.PrepareOnStart {
		dir := runtimeDir(cfg.Runtimes.Python.Project, agentWorkdir)
		if fileExists(filepath.Join(dir, "pyproject.toml")) {
			logger("Runtime: preparing Python (uv sync) …")
			go func() { _, _ = sup.RunOnce(cfg.Runtimes.Python.UV, []string{"sync"}, dir, nil, "prepare:python") }()
		}
	}
	if cfg.Runtimes.Node.Enabled && cfg.Runtimes.Node.PrepareOnStart {
		dir := runtimeDir(cfg.Runtimes.Node.Project, agentWorkdir)
		if fileExists(filepath.Join(dir, "package.json")) {
			logger("Runtime: preparing Node (npm install) …")
			go func() { _, _ = sup.RunOnce(cfg.Runtimes.Node.Npm, []string{"install"}, dir, nil, "prepare:node") }()
		}
	}
}

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

// commandLoop reads control commands from stdin (for manual operation).
// makeAgentExitHandler builds the callback that fires when the agent ends and is
// not restarted on its own (see ServiceSpec.OnExit). In window mode the agent is
// the leading process — once it ends the session is over, and the launcher must
// not silently hang around with a running editor.
func makeAgentExitHandler(ctx context.Context, stop func(), sup *supervisor.Supervisor, cfg *config.Config, logger func(string), interactive bool) func(success bool) {
	var mu sync.Mutex
	cmdLoopRunning := false // already switched to the launcher console?
	return func(success bool) {
		if success {
			logger("Agent exited (exit 0).")
		} else {
			logger("Agent crashed / restarts exhausted.")
		}

		// If the launcher console is already running (an earlier 'k' choice), a
		// second stdin prompt would compete with it for the input — so only log.
		mu.Lock()
		busy := cmdLoopRunning
		mu.Unlock()
		if busy {
			logger("Launcher console active — 'q' stops everything, 'start agent' restarts the agent.")
			return
		}

		switch cfg.Agent.OnExit {
		case config.OnExitLeave:
			logger("agent.onExit=leave — editor/MCP keep running. Ctrl-C stops the launcher.")
		case config.OnExitShutdown:
			logger("agent.onExit=shutdown — stopping everything.")
			stop()
		default: // ask
			if !interactive {
				// Headless (-p): no TTY to ask on — the command loop reads stdin.
				// Agent end = job done -> shut down cleanly.
				stop()
				return
			}
			switch promptAgentExit(success) {
			case "k":
				fmt.Fprintln(os.Stdout, "Editor keeps running. Launcher console:")
				mu.Lock()
				cmdLoopRunning = true
				mu.Unlock()
				go commandLoop(ctx, stop, sup, logger)
			case "r":
				fmt.Fprintln(os.Stdout, "Restarting agent …")
				if _, err := sup.StartService("agent"); err != nil {
					logger("Agent restart failed: " + err.Error() + " — stopping everything.")
					stop()
				}
			default: // Enter / q / Timeout / EOF
				stop()
			}
		}
	}
}

// promptAgentExit shows the selection menu on the real console (os.Stdout/Stdin —
// in window mode the launcher logs go to the file) and returns the choice. After
// 30s without input, "stop everything" applies (e.g. agent crashed overnight).
func promptAgentExit(success bool) string {
	if success {
		fmt.Fprintln(os.Stdout, "\nAgent exited.")
	} else {
		fmt.Fprintln(os.Stdout, "\nAgent crashed.")
	}
	fmt.Fprintln(os.Stdout, "  [Enter] stop everything (UE + launcher)")
	fmt.Fprintln(os.Stdout, "  [k]     keep the editor running, launcher console")
	fmt.Fprintln(os.Stdout, "  [r]     restart the agent")
	fmt.Fprint(os.Stdout, "> ")

	ch := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		if sc.Scan() {
			ch <- strings.ToLower(strings.TrimSpace(sc.Text()))
		} else {
			ch <- "" // EOF (e.g. stdin closed) -> stop everything
		}
	}()
	select {
	case s := <-ch:
		return s
	case <-time.After(30 * time.Second):
		fmt.Fprintln(os.Stdout, "\n(Timeout) — stopping everything.")
		return ""
	}
}

func commandLoop(ctx context.Context, stop func(), sup *supervisor.Supervisor, logger func(string)) {
	logger("Commands: 'status' | 'r' (restart all) | 'r <name>' | 'stop <name>' | 'start <name>' | 'c <name>' | 'q'")
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		fields := strings.Fields(strings.TrimSpace(sc.Text()))
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "q", "quit", "exit":
			logger("Stopping …")
			stop()
			return
		case "status", "s":
			b, _ := json.MarshalIndent(sup.Status(), "", "  ")
			logger("\n" + string(b))
		case "r", "restart":
			if len(fields) > 1 {
				sup.RestartService(fields[1])
			} else {
				for _, n := range sup.ServiceNames() {
					sup.RestartService(n)
				}
			}
		case "stop":
			if len(fields) > 1 {
				sup.StopService(fields[1])
			}
		case "start":
			if len(fields) > 1 {
				sup.StartService(fields[1])
			}
		case "c", "cmd":
			if len(fields) > 1 {
				res, err := sup.RunCommand(fields[1])
				if err != nil {
					logger("Error: " + err.Error())
				} else {
					logger(fmt.Sprintf("exit %d\n%s", res.ExitCode, tailLines(res.Output, 100)))
				}
			}
		default:
			logger("Unknown command: " + fields[0])
		}
	}
}

// --- helper functions ---

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

func boolVal(p *bool) bool { return p != nil && *p }

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

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func hasPromptArg(args []string) bool {
	return hasArg(args, "-p") || hasArg(args, "--print")
}

// killCrashReporter terminates a possibly stuck crash reporter window.
func killCrashReporter() {
	var cmds [][]string
	if runtime.GOOS == "windows" {
		cmds = [][]string{
			{"taskkill", "/F", "/IM", "CrashReportClientEditor.exe"},
			{"taskkill", "/F", "/IM", "CrashReportClient.exe"},
		}
	} else {
		cmds = [][]string{{"pkill", "-f", "CrashReportClient"}}
	}
	for _, c := range cmds {
		_ = exec.Command(c[0], c[1:]...).Run() // ignore errors (the process may not exist)
	}
}

// cleanRecovery removes recovery/crash artefacts for a clean restart.
func cleanRecovery(projectDir string, logger func(string)) {
	saved := filepath.Join(projectDir, "Saved")
	restore := filepath.Join(saved, "Autosaves", "PackageRestoreData.json")
	if err := os.Remove(restore); err == nil {
		logger("Recovery: PackageRestoreData.json removed")
	}
	crashes := filepath.Join(saved, "Crashes")
	if entries, err := os.ReadDir(crashes); err == nil && len(entries) > 0 {
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(crashes, e.Name()))
		}
		logger("Recovery: Saved/Crashes emptied")
	}
}

func warnIfMissing(logger func(string), label, path string) {
	if !fileExists(path) {
		logger(fmt.Sprintf("WARN %s: path not found: %s", label, path))
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func errResult(err error) mcp.ToolResult {
	return mcp.ToolResult{Text: "Error: " + err.Error(), IsError: true}
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func getString(args map[string]interface{}, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

// stringArg reads a string argument and reports whether the key was actually
// present and held a string. Unlike getString it tells a missing key apart from
// an empty value, which matters wherever "" is a legal payload but a typo in the
// key name would otherwise silently destroy data.
func stringArg(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key].(string)
	return v, ok
}

func getInt(args map[string]interface{}, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

func firstString(args map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func firstMap(args map[string]interface{}, keys ...string) map[string]interface{} {
	for _, k := range keys {
		if v, ok := args[k].(map[string]interface{}); ok {
			return v
		}
	}
	return nil
}

func allowWord(b bool) string {
	if b {
		return "allow"
	}
	return "deny"
}

func perm(enabled bool) string {
	if enabled {
		return " + permission-prompt-tool"
	}
	return ""
}

func strictWord(strict bool) string {
	if strict {
		return ", strict"
	}
	return ""
}

// hermesConfigMarker is the comment that tags the mcp_servers block we write.
// We prepend it to the file as a YAML comment so the user knows: this block
// was written by unreagent.
const hermesConfigMarker = "# unreagent-managed: do not edit — re-run `unreagent hermes-setup` to refresh"

// extractSubcommand returns the first positional value from os.Args[1:] plus
// its index into os.Args (used by callers that need to splice it out before
// re-parsing flags). Values of `-key value` flags are skipped so both
// `unreagent -config foo hermes-setup` and `unreagent hermes-setup -config foo`
// resolve to "hermes-setup" without swallowing "-config" or "foo". index == 0
// means no sub-command was found.
func extractSubcommand() (name string, index int) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			// "-key" followed by something that doesn't start with "-" is
			// treated as a key/value pair — consume the value (don't return it).
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		return a, i + 1 // +1 because os.Args[0] is the binary path
	}
	return "", 0
}

// runHermesSetup is the entry point for `unreagent hermes-setup`. It merges
// the unreagent MCP server entry into Hermes Agent's config.yaml so Hermes
// (Nous Research) can use the same toolset as Claude Code.
//
// Idempotent: a second run overwrites the previous entry instead of duplicating
// it. The unreagent config is **not** auto-merged on normal launcher runs —
// the user has to opt in by running this command once.
func runHermesSetup() error {
	// Use a private FlagSet so the sub-command is self-contained (the main
	// flags are parsed in run(), which we never reach here). os.Args[0] is the
	// binary path. To make `-config` work in either order, we splice ONLY the
	// sub-command name out of os.Args[1:] and hand the rest to fs.Parse. That
	// way the FlagSet sees exactly the flags the user typed — flag.Parse does
	// stop at the first non-flag arg, but since the sub-command word is gone,
	// the next value (if any) is already a flag-pair.
	_, subIndex := extractSubcommand()
	filtered := make([]string, 0, len(os.Args))
	filtered = append(filtered, os.Args[:subIndex]...)   // binary + everything before the sub-command
	filtered = append(filtered, os.Args[subIndex+1:]...) // everything after the sub-command
	fs := flag.NewFlagSet("hermes-setup", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Path to unreagent.yaml (default: next to the executable)")
	if err := fs.Parse(filtered[1:]); err != nil {
		return err
	}

	cfg, info, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	if !cfg.Agent.Hermes.Enabled {
		fmt.Println("agent.hermes.enabled is not set — nothing to do.")
		fmt.Println("Set it in unreagent.yaml and re-run this command to integrate Hermes.")
		fmt.Println("Hint: this file — " + info.ConfigPath)
		return nil
	}

	token := cfg.MCP.Token
	tokenGenerated := false
	if token == "" {
		t, err := generateBearerToken()
		if err != nil {
			return fmt.Errorf("generate token: %w", err)
		}
		token = t
		tokenGenerated = true
	}

	// Token persistence: if we just generated one, write it to
	// unreagent.local.yaml so the launcher reuses the same token on the next
	// start (otherwise Hermes' bearer header would no longer match after a
	// reload).
	if tokenGenerated {
		if err := persistGeneratedToken(info, token); err != nil {
			return fmt.Errorf("write token to %s: %w", localOverridePath(info.ConfigPath), err)
		}
		fmt.Println("New bearer token generated and written to unreagent.local.yaml.")
	}

	hermesPath, err := resolveHermesConfigPath(cfg.Agent.Hermes.ConfigPath)
	if err != nil {
		return err
	}
	fmt.Println("Hermes config: " + hermesPath)

	// Back up the existing file before we touch it. Empty file or not present
	// — no backup needed.
	if existing, statErr := os.Stat(hermesPath); statErr == nil && existing.Size() > 0 {
		bak := hermesPath + ".bak"
		if _, bakErr := os.Stat(bak); bakErr == nil {
			// A backup from a previous hermes-setup run is already there — do
			// not overwrite it; it is the user's value.
			fmt.Println("Backup kept: " + bak)
		} else {
			if err := os.Rename(hermesPath, bak); err != nil {
				return fmt.Errorf("create backup: %w", err)
			}
			fmt.Println("Backup: " + bak)
		}
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("stat Hermes config: %w", statErr)
	}

	doc, err := loadHermesYAML(hermesPath)
	if err != nil {
		return err
	}

	mergeHermesServer(doc, cfg.Agent.Hermes.ServerName, cfg.MCP.Address, token)

	if err := os.MkdirAll(filepath.Dir(hermesPath), 0o755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}
	if err := writeYAMLAtomic(hermesPath, doc); err != nil {
		return fmt.Errorf("write Hermes config: %w", err)
	}

	fmt.Println()
	fmt.Println("Hermes MCP server registered under mcp_servers." + cfg.Agent.Hermes.ServerName)
	fmt.Println("  url:    http://" + cfg.MCP.Address + "/mcp")
	fmt.Println("  bearer: " + token[:8] + "… (64 hex chars, in unreagent.local.yaml)")
	fmt.Println()
	fmt.Println("Restart Hermes so it re-reads the config. The unreagent tools")
	fmt.Println("(ue_start/stop/restart, logs, run_command, …) will then be available.")
	return nil
}

// resolveHermesConfigPath returns the absolute path to the Hermes config:
//   - explicitly set in agent.hermes.configPath
//   - $HERMES_HOME/config.yaml, if set
//   - otherwise ~/.hermes/config.yaml
func resolveHermesConfigPath(explicit string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("make configPath absolute: %w", err)
		}
		return filepath.ToSlash(abs), nil
	}
	if h := os.Getenv("HERMES_HOME"); h != "" {
		return filepath.ToSlash(filepath.Join(h, "config.yaml")), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.ToSlash(filepath.Join(home, ".hermes", "config.yaml")), nil
}

// generateBearerToken returns 32 random bytes as a hex string (64 chars).
// Enough entropy for a localhost bearer, small enough to stay manageable in
// YAML.
func generateBearerToken() (string, error) {
	b := make([]byte, 32)
	if _, err := cryptorand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// localOverridePath returns the path to unreagent.local.yaml next to a
// unreagent.yaml (same convention as config.localPathFor).
func localOverridePath(yamlPath string) string {
	ext := filepath.Ext(yamlPath)
	return strings.TrimSuffix(yamlPath, ext) + ".local" + ext
}

// persistGeneratedToken writes mcp.token into unreagent.local.yaml. Other
// fields in that file (e.g. engineRoot) are left alone — we only merge.
// If the file does not exist yet, it is created.
func persistGeneratedToken(info config.Info, token string) error {
	override := localOverridePath(info.ConfigPath)

	// Read any existing file so we don't clobber it.
	var existing map[string]interface{}
	if b, err := os.ReadFile(override); err == nil && len(bytes.TrimSpace(b)) > 0 {
		if uerr := yaml.Unmarshal(b, &existing); uerr != nil {
			return fmt.Errorf("existing %s not parseable: %w", filepath.Base(override), uerr)
		}
	}
	if existing == nil {
		existing = map[string]interface{}{}
	}

	mcpSection, _ := existing["mcp"].(map[string]interface{})
	if mcpSection == nil {
		mcpSection = map[string]interface{}{}
	}
	mcpSection["token"] = token
	existing["mcp"] = mcpSection

	out, err := yaml.Marshal(existing)
	if err != nil {
		return err
	}
	// YAML comment so the user knows this file was generated.
	header := "# Generated by `unreagent hermes-setup` — mcp.token was auto-\n" +
		"# generated so the unreagent MCP server accepts the bearer auth from\n" +
		"# Hermes' config. Keep the token in this (git-ignored) file.\n"
	return os.WriteFile(override, []byte(header+string(out)), 0o600)
}

// loadHermesYAML reads the Hermes config and returns a mutable map. A missing
// or empty file is OK (fresh Hermes install).
func loadHermesYAML(path string) (map[string]interface{}, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil
		}
		return nil, fmt.Errorf("read Hermes config %s: %w", path, err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]interface{}{}, nil
	}
	doc := map[string]interface{}{}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("Hermes config %s not parseable: %w", path, err)
	}
	return doc, nil
}

// mergeHermesServer sets mcp_servers.<name> to the unreagent entry. If a
// block with that name already exists (from a previous hermes-setup run), it
// is overwritten. Other servers under mcp_servers are left alone.
func mergeHermesServer(doc map[string]interface{}, name, address, token string) {
	servers, _ := doc["mcp_servers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	servers[name] = map[string]interface{}{
		"url": "http://" + address + "/mcp",
		"headers": map[string]interface{}{
			"Authorization": "Bearer " + token,
		},
	}
	doc["mcp_servers"] = servers
}

// writeYAMLAtomic writes the config safely: first to a temp file in the same
// directory, then rename. Avoids a half-written config if the process is
// killed mid-write.
//
// We prepend a marker comment so the user knows this file is managed by
// unreagent. On a re-run the marker persists (useful as a hint that the file
// is tool-managed).
func writeYAMLAtomic(path string, doc map[string]interface{}) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // in case the rename below fails

	if _, err := tmp.WriteString(hermesConfigMarker + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}
