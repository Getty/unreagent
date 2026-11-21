// Command launcher (unreagent) starts and supervises the Unreal Editor and an
// agent (e.g. Claude Code) and offers the agent an MCP server through which it
// can control the editor (start/stop/restart), run commands (compile, package),
// read logs and execute Python/Node code in prepared environments.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
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
	for _, name := range info.SkippedCommands {
		logger("WARN built-in command " + name + " not offered: it needs the engine and the .uproject (see above)")
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
	// config.Load rejects an unresolved files.root only when files.enabled is
	// set in the config; -files turns the tools on afterwards and finds the
	// root cleared instead. Refuse rather than fall back to some other dir.
	if cfg.Files.Enabled && cfg.Files.Root == "" {
		logger("WARN file tools disabled: files.root names a placeholder that is unresolved — set unreal.project or files.root")
		cfg.Files.Enabled = false
	}
	if cfg.Files.Enabled && !cfg.MCP.Enabled {
		logger("WARN file tools not served: they need mcp.enabled")
	} else if cfg.Files.Enabled {
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
		prepareMCPBridges(ctx, sup, cfg, agentWorkdir, logger)
	}

	var wg sync.WaitGroup
	// A signal during the bridge preparation: start nothing only to stop it
	// again — the select below shuts down right away.
	if ctx.Err() == nil {
		sup.Start(ctx, &wg)
		prepareRuntimes(sup, cfg, agentWorkdir, logger)
		// In interactive mode stdin belongs to the agent — no command loop of our own.
		if !agentInteractive {
			go commandLoop(ctx, stop, sup, logger)
		}
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
	case <-done:
	}
	// Decided after the select, not by it: with nothing started, done is
	// ready as early as the cancelled ctx and the select would pick at random.
	if ctx.Err() != nil {
		logger("Signal received — stopping all processes …")
	} else {
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

// --- helper functions ---

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

func boolVal(p *bool) bool { return p != nil && *p }

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

func strictWord(strict bool) string {
	if strict {
		return ", strict"
	}
	return ""
}

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
