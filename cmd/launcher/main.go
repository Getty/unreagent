// Command launcher (unreagent) starts and supervises the Unreal Editor and an
// agent (e.g. Claude Code) and offers the agent an MCP server through which it
// can control the editor (start/stop/restart), run commands (compile, package),
// read logs and execute Python/Node code in prepared environments.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// version is set in the release build via -ldflags "-X main.version=<tag>".
var version = "dev"

// now is the clock of the launcher log's timestamps; the run() tests step it.
var now = time.Now

func main() {
	err := run()
	if err == nil {
		return
	}
	var usage usageError
	isUsage := errors.As(err, &usage)
	if !usage.printed {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
	if isUsage {
		os.Exit(2)
	}
	os.Exit(1)
}

func run() error {
	cfgPath := flag.String("config", "", "Path to unreagent.yaml (default: next to the executable)")
	noAgent := flag.Bool("no-agent", false, "Don't start the agent (UE + MCP server only; an external agent can connect)")
	filesFlag := flag.Bool("files", false, "Expose file tools (read/write/list/edit) over the MCP server")
	writeMcp := flag.String("write-mcp-config", "", "Also write the MCP config as a .mcp.json to this path")
	noSkillsMCP := flag.Bool("no-skills-mcp", false, "Deliver the skills as files in skills.dir instead of through the skills MCP tool (skills.mcp: false, skills.materialize: true)")
	// The launcher's flag set parses up to the first positional argument
	// (a bad flag exits 2, -h exits 0); that word, if any, must name a
	// sub-command. Define every launcher flag above this call: one defined
	// below it is unknown here, and exits 2.
	flag.Usage = func() { printUsage(flag.CommandLine) }
	sub, subArgs, err := extractSubcommand(flag.CommandLine, os.Args[1:])
	if err != nil {
		return err
	}
	if sub != "" {
		return subcommands[sub].run(subArgs)
	}

	logOut := io.Writer(os.Stdout)
	// early keeps what logger writes until window mode's launcher log opens,
	// for run() to replay there (k150): config, engine and project lines and
	// their warnings come before the switch and went to the console alone.
	// nil from the switch on, and once it is clear there is no log file.
	early := new(bytes.Buffer)
	logger := func(line string) {
		text := fmt.Sprintf("%s  %s\n", now().Format("15:04:05"), line)
		_, _ = io.WriteString(logOut, text)
		if early != nil {
			early.WriteString(text)
		}
	}

	cfg, info, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	logger(startLine(info))
	if info.Defaults {
		for _, line := range defaultsNotice(cfg) {
			logger("INFO " + line)
		}
	}
	if info.LocalPath != "" {
		logger("Local overlay: " + info.LocalPath)
	}
	if info.EngineRoot != "" {
		logger("Engine: " + info.EngineRoot)
	} else {
		logger("WARN engine not found — set UE_ROOT or engineRoot in unreagent.local.yaml")
	}
	for _, w := range info.Warnings {
		logger("WARN " + w)
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
	if *noSkillsMCP {
		applyNoSkillsMCP(&cfg.Skills, logger)
	}

	// Bind the MCP port before anything has side effects (k45, k123): a port
	// that cannot be bound ends the launcher with exit 1 before the editor,
	// the bridges or the agent start. Running on, the agent would talk to
	// whoever holds the port, and an external agent would find nothing.
	// Usually that is another unreagent on the same project, whose launcher
	// log, script files and skill files this one would otherwise rotate,
	// sweep and rewrite.
	var mcpLn net.Listener
	if cfg.MCP.Enabled {
		mcpLn, err = listenMCP(cfg.MCP.Address)
		if err != nil {
			return err
		}
		defer mcpLn.Close() // for the returns before Serve; then httpSrv.Close closes it
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
	var logPath string // window mode's launcher log file; logOut once it opens
	if agentInteractive {
		dir := "."
		if info.Project != "" {
			dir = filepath.Dir(info.Project)
		}
		logPath = filepath.Join(dir, "unreagent.log")
		if f := openLauncherLog(logPath, os.Stderr); f != nil {
			// Stamped already: they keep the times they were logged at.
			_, _ = early.WriteTo(f)
			logOut = f
			defer f.Close()
			fmt.Printf("unreagent %s\n", version)
			fmt.Printf("Launcher logs -> %s   (Unreal runs in the background)\n", logPath)
			fmt.Println("The agent (Claude) takes over this window …")
			fmt.Println()
		}
	}
	// Before the goroutines that log start below: logger reads early unlocked.
	early = nil
	// In window mode the launcher log is a file and the console the agent's,
	// so a line the user must see is also written to alert, stderr; otherwise
	// the log is the console already and alert stays nil.
	var alert io.Writer
	if logOut != io.Writer(os.Stdout) {
		alert = os.Stderr
	}

	// In window mode these come after the switch to unreagent.log, so the
	// console gets them as well (k192).
	startupWarn := alertWarnings(logger, alert, logPath)
	warnIfMissing(startupWarn, "unreal.editor", cfg.Unreal.Editor)
	if cfg.Agent.Enabled {
		if _, lookErr := exec.LookPath(cfg.Agent.Command); lookErr != nil {
			startupWarn("WARN agent command not in PATH: " + cfg.Agent.Command + " (set the full path in unreagent.local.yaml)")
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
	ueArgs := editorArgs(cfg.Unreal, logger)
	// A plugin the editor refuses for its EngineVersion is dropped without
	// a word under -unattended (k214), so say it before the editor starts,
	// where the user looks. The editor's own arguments decide -unattended
	// and which plugins -DisablePlugins= switches off.
	checkPluginEngineVersions(cfg.Unreal.Project, engineDirOf(cfg.Unreal.Editor, info.EngineRoot),
		ueArgs, alertWarnings(logger, alert, logPath))
	// A module without its binary makes the editor exit at startup, again
	// after every restart, until the launcher gives up on it (k229): name it
	// and the compile command first. Nothing is built here.
	_, compile := cfg.Commands["compile"]
	// The routes the fix text may name: no launcher console with an
	// interactive agent in the window, no agent tools without MCP (k247).
	// Fixed at startup: after an interactive agent exits and the user keeps a
	// console with 'k', a later give-up still names no console.
	routes := editorRoutes{console: !agentInteractive, agent: cfg.MCP.Enabled}
	checkProjectModules(cfg.Unreal.Project, cfg.Unreal.Editor, compile, routes, alertWarnings(logger, alert, logPath))
	ueProjectDir := ""
	if cfg.Unreal.Project != "" {
		ueProjectDir = filepath.Dir(cfg.Unreal.Project)
	}
	// Like onAgentExit below: set once the session exists.
	var onEditorExit func(supervisor.Exit)
	// The editor's give-up while MCP serves, for a later serve failure to end
	// the session with (k155); every start of the editor takes it back.
	var giveUp editorGiveUp
	if cfg.Unreal.KillCrashReporter != nil {
		logger("WARN unreal.killCrashReporter has no effect any more and can be removed: the launcher kills no crash reporter before a start, as the kill ended other editors' crash monitors too; this editor's reporter ends with its job")
	}
	preStart := editorPreStart(cfg.Unreal, ueProjectDir, logger)
	if err := sup.AddService(supervisor.ServiceSpec{
		Name:         "ue",
		Command:      cfg.Unreal.Editor,
		Args:         ueArgs,
		Autostart:    !cfg.Unreal.ManualStart,
		Restart:      cfg.Unreal.Restart,
		MaxRestarts:  cfg.Unreal.MaxRestarts,
		RestartDelay: secs(cfg.Unreal.RestartDelaySeconds),
		// The editor's crash reporter finishes its report after the editor
		// has exited (k154).
		ReporterGrace: crashReporterGrace,
		PreStart: func(st supervisor.Start) {
			giveUp.started()
			preStart(st)
		},
		OnExit: func(e supervisor.Exit) {
			if onEditorExit != nil {
				onEditorExit(e)
			}
		},
		// In window mode the console shows the editor's automatic lines as
		// the log has them: its exits and the restarts that follow (k188),
		// a retried failed start (k192), and a restart held for the crash
		// reporter, which the restart line would otherwise misstate.
		OnRunEnd: func(e supervisor.RunEnd) {
			alertLines(alert, runEndLines("ue", e)...)
		},
		OnStartRetry: func(r supervisor.Retry) {
			alertLines(alert, startRetryLines("ue", r)...)
		},
		OnStartHeld: alertHeldRestart(alert, "ue"),
	}); err != nil {
		return err
	}

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
	// Before the agent starts, so that it finds the skill files complete.
	skillSet, err := setupSkills(cfg, agentWorkdir, logger)
	if err != nil {
		return err
	}
	mcpURL := cfg.MCP.ClientURL()
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

		var ownMCPConfig string // shown in the "Agent command:" line
		if cfg.MCP.Enabled && cfg.Agent.ClaudeIntegration {
			// A file, not the inline JSON (k181): the agent's command line is
			// readable to every local user.
			ownMCPConfig, err = writeAgentMCPConfig(mcpServers)
			if err != nil {
				return err
			}
			// For the whole run, so that a restarted agent finds it too.
			defer removeAgentMCPConfig(ownMCPConfig, logger, alert)
			agentArgs = append(agentArgs, "--mcp-config", ownMCPConfig)
			if cfg.MCP.Strict {
				agentArgs = append(agentArgs, "--strict-mcp-config")
			}
			if cfg.Permissions.Enabled {
				if agentInteractive {
					// Interactive: --permission-prompt-tool is a headless feature
					// (-p) and aborts otherwise. allow_all -> skip prompts; other
					// modes -> Claude asks in the window (manually). Either way
					// the policy is not applied: permissionsInertReason says so.
					if cfg.Permissions.Mode == config.ModeAllowAll {
						agentArgs = append(agentArgs, "--dangerously-skip-permissions")
					}
				} else if tools := promptToolsIn(cfg.Agent.Args); len(tools) == 0 {
					agentArgs = append(agentArgs, "--permission-prompt-tool", approveTool)
				} else if !slices.ContainsFunc(tools, isNotApprove) {
					// No second one next to the hand-wired flag (k172); another
					// tool there is permissionsInertReason's to warn about.
					logger("Agent: --permission-prompt-tool " + approveTool + " is set in agent.args already; the launcher does not pass it a second time")
				}
			}
			logger(fmt.Sprintf("Agent: Claude integration active (%d MCP server(s)%s)",
				len(mcpServers), strictWord(cfg.MCP.Strict)))
		}
		// Before the agent takes the console in window mode (k38, k46).
		if reason := permissionsInertReason(cfg, agentInteractive); reason != "" {
			line := "WARN " + reason
			logger(line)
			if alert != nil {
				fmt.Fprintln(alert, "unreagent: "+line)
			}
		}
		logger("Agent command: " + cfg.Agent.Command + " " + strings.Join(redactAgentArgs(agentArgs, cfg.MCP.Token, ownMCPConfig), " "))
		if err := sup.AddService(supervisor.ServiceSpec{
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
			OnExit: func(e supervisor.Exit) {
				if onAgentExit != nil {
					onAgentExit(e.Success)
				}
			},
			// The supervisor's lines name the retry in the log; in window
			// mode the console gets one line per retry as well (k152).
			OnStartRetry: func(r supervisor.Retry) {
				alertLines(alert, agentRetryLine(r))
			},
			// So does each restart after an exit (k188); the run that is not
			// restarted is the exit prompt's or the onExit line's to name.
			OnRunEnd: func(e supervisor.RunEnd) {
				if e.Restart > 0 {
					alertLines(alert, agentRestartLine(e))
				}
			},
		}); err != nil {
			return err
		}
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
		writeMCPConfigs(cfg.MCP.WriteConfig, mcpServers, base, ueProjectDir, cfg.MCP.Token != "", logger, alert)
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

	// Before the MCP server: whoever acts on its "listening" line, a client
	// or a Ctrl-C/SIGTERM, must find the signal handler installed (k141).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sess := newSession(ctx, stop)

	// --- MCP server ---
	var httpSrv *http.Server
	// True while the MCP server serves; serveMCP clears it from its own
	// goroutine when the server stops serving, the editor's exit handler reads
	// it from the supervisor's.
	var mcpServing atomic.Bool
	if cfg.MCP.Enabled {
		srv := mcp.NewServer(config.MCPServerName, version, logger)
		if cfg.MCP.Token != "" {
			srv.SetToken(cfg.MCP.Token)
			logger("MCP server: bearer auth enabled (mcp.token is set)")
		}
		// Empty keeps the server's built-in loopback allowlist.
		if len(cfg.MCP.AllowedOrigins) > 0 {
			srv.SetAllowedOrigins(cfg.MCP.AllowedOrigins)
			logger("MCP server: browser Origins allowed: " + strings.Join(cfg.MCP.AllowedOrigins, ", ") + " (mcp.allowedOrigins, replacing the built-in loopback origins)")
		}
		registerTools(srv, sup, cfg, agentWorkdir, skillSet, logger)
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
		mcpServing.Store(true)
		go func() {
			if serveMCP(httpSrv, mcpLn, &mcpServing, logger, alert) {
				giveUp.mcpStopped(sess, logger)
			}
		}()
		// The port is bound since listenMCP, so a client acting on this line
		// connects (k141). The address names the interfaces, the URL is what
		// clients got; they differ for a wildcard host (k57).
		logger("MCP server listening on " + cfg.MCP.Address + " — clients connect to " + mcpURL)
	}

	con := newConsole(os.Stdin, os.Stdout, logOut)
	// Its line, the editor's give-up, goes to the console as well (k192).
	onEditorExit = makeEditorExitHandler(sess, alertAll(logger, alert), cfg.Agent.Enabled, &mcpServing, &giveUp, cfg.Unreal, ueProjectDir,
		func(ends bool) string {
			return giveUpModulesPointer(cfg.Unreal.Project, cfg.Unreal.Editor, compile, routes, ends)
		})
	if cfg.Agent.Enabled {
		onAgentExit = makeAgentExitHandler(ctx, sess, sup, cfg, logger, agentInteractive, con)
	}

	// A broken bridge or runtime is what the agent trips over, so their
	// warnings reach the user's console in window mode too (k56, k79).
	prepLogger := alertWarnings(logger, alert, logPath)
	if cfg.MCP.Enabled && len(cfg.MCP.ExtraServers) > 0 {
		prepareMCPBridges(ctx, sup, cfg, agentWorkdir, prepLogger)
	}

	var wg sync.WaitGroup
	// A signal during the bridge preparation: start nothing only to stop it
	// again — the select below shuts down right away.
	if ctx.Err() == nil {
		sup.Start(ctx, &wg)
		go prepareRuntimes(sup, cfg, agentWorkdir, prepLogger)
		// In interactive mode stdin belongs to the agent — no command loop of our own.
		if !agentInteractive {
			go commandLoop(ctx, con, sess.end, sup, logger, "")
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
	switch {
	case sess.endedByLauncher():
		logger("Stopping all processes …")
	case ctx.Err() != nil:
		logger("Signal received — stopping all processes …")
	default:
		logger("All services stopped.")
	}
	stop()
	wg.Wait()
	if httpSrv != nil {
		_ = httpSrv.Close()
	}
	logger("Bye.")
	return sess.result()
}

// listen is net.Listen; the run() tests replace it to break the MCP server's
// port after the bind.
var listen = net.Listen

// listenMCP binds the MCP server's address. Its error names the address, the
// cause and the usual suspects, for run() to end the launcher with.
func listenMCP(addr string) (net.Listener, error) {
	ln, err := listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("MCP server cannot listen on %s: %w — another unreagent or another program may already hold that port, or mcp.address is not a host:port of this machine; stop the other program, or set mcp.address to a free host:port (for this machine only: in unreagent.local.yaml)", addr, err)
	}
	return ln, nil
}

// serveMCP serves srv on ln until srv is closed. An error before that, which
// means the bound port broke, clears serving and is logged as one ERROR line,
// also to alert unless alert is nil. Main agent's decision for k123: the
// session goes on and nothing rebinds. Everything else keeps running with
// what it holds, and the user decides when to restart, once the work in the
// editor is saved. Only a port that cannot be bound at the start is fatal
// (listenMCP); and with no agent, an editor that gave up is left with nothing
// to retry it, which the caller ends the session for (editorGiveUp, k155).
// It reports whether it stopped serving on such an error, rather than closed.
func serveMCP(srv *http.Server, ln net.Listener, serving *atomic.Bool, logger func(string), alert io.Writer) (stopped bool) {
	err := srv.Serve(ln)
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return false
	}
	serving.Store(false)
	// "Everything else", not "the editor and the agent": there may be no agent.
	line := fmt.Sprintf("ERROR MCP server on %s stopped serving: %v — the agent's unreagent tools (and permission prompts through approve) no longer work; everything else keeps running. Save your work in the editor, then restart unreagent.", ln.Addr(), err)
	logger(line)
	if alert != nil {
		fmt.Fprintln(alert, "unreagent: "+line)
	}
	return true
}

// alertWarnings returns a logger that writes every line to logger and a
// "WARN " line also to alert, the way run() mirrors its own warnings: alert
// is stderr in window mode and nil otherwise, which leaves logger as it is.
// alert gets the first line of a WARN only, and for one with more lines (an
// output tail) a pointer to logPath, the log the whole text goes to: such a
// warning can come while the agent's terminal UI owns the console, where
// twenty lines of npm output do more harm than good.
func alertWarnings(logger func(string), alert io.Writer, logPath string) func(string) {
	if alert == nil {
		return logger
	}
	return func(line string) {
		logger(line)
		if !strings.HasPrefix(line, "WARN ") {
			return
		}
		if first, _, more := strings.Cut(line, "\n"); more {
			line = first + " (the rest is in " + logPath + ")"
		}
		fmt.Fprintln(alert, "unreagent: "+line)
	}
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

// noPolicyMeaning says what permissions.enabled off means for the agent.
const noPolicyMeaning = "it neither allows nor denies a tool call, and Claude Code's own permission prompts and settings alone decide what the agent may do"

// defaultsNotice is what a start without a config file says about itself,
// without the INFO prefix: what the loaded configuration switches on — the
// built-in defaults with an unreagent.local.yaml over them, which is the only
// thing that can have switched agent or MCP server off —, that `unreagent
// init` writes the defaults out, and, unless the overlay enabled permissions,
// that unreagent applies no permission policy on this path.
func defaultsNotice(cfg *config.Config) []string {
	onOff := func(what string, on bool) string {
		if on {
			return what + " on"
		}
		return what + " off (unreagent.local.yaml)"
	}
	bridge := "no UE LLM Toolkit bridge (plugin not found below Plugins/)"
	if def, ok := cfg.MCP.ExtraServers[config.ToolkitServerName]; ok {
		bridge = "UE LLM Toolkit bridge configured"
		if script := bridgeScript(def); script != "" {
			bridge += " (" + script + ")"
		}
	}
	lines := []string{
		"no config file there — running on built-in defaults: " + onOff("agent", cfg.Agent.Enabled) + ", " + onOff("MCP server", cfg.MCP.Enabled) + ", " + bridge,
		"`unreagent init` writes these defaults out as an unreagent.yaml to edit, and checks the setup (engine, node, the agent command, the toolkit plugin)",
	}
	if !cfg.Permissions.Enabled {
		lines = append(lines, "unreagent applies no permission policy on built-in defaults (permissions.enabled is off): "+noPolicyMeaning)
	}
	return lines
}

// startLine is the launcher's first log line. Without a config file it says
// so (k253): it must not read as a loaded config.
func startLine(info config.Info) string {
	if info.Defaults {
		return fmt.Sprintf("unreagent %s — no config file (looked for %s)", version, info.ConfigPath)
	}
	return fmt.Sprintf("unreagent %s — Config: %s", version, info.ConfigPath)
}

// bridgeScript is the script a node bridge definition runs, "" when its args
// name none.
func bridgeScript(def map[string]interface{}) string {
	raw, _ := def["args"].([]interface{})
	args := make([]string, 0, len(raw))
	for _, a := range raw {
		if s, ok := a.(string); ok {
			args = append(args, s)
		}
	}
	return nodeScript(args)
}

// approveTool is the launcher's permission prompt tool, as
// --permission-prompt-tool names it.
const approveTool = "mcp__" + config.MCPServerName + "__approve"

func isNotApprove(tool string) bool { return tool != approveTool }

// promptToolsIn returns the tool each --permission-prompt-tool in args
// names, in either form ("--permission-prompt-tool X" and
// "--permission-prompt-tool=X"); "" for one with nothing after it.
func promptToolsIn(args []string) []string {
	var tools []string
	for i, a := range args {
		if a == "--permission-prompt-tool" {
			tool := ""
			if i+1 < len(args) {
				tool = args[i+1]
			}
			tools = append(tools, tool)
		} else if tool, ok := strings.CutPrefix(a, "--permission-prompt-tool="); ok {
			tools = append(tools, tool)
		}
	}
	return tools
}

// permissionsInertReason is the warning, without its WARN prefix, for a
// permissions policy that cannot take effect on the agent this launcher
// starts; "" when it can, or when there is no policy or no such agent. The
// policy reaches the agent only through --permission-prompt-tool naming
// approveTool, which the agent-start block in run() passes with
// claudeIntegration, only to an agent that is not interactive and only
// when agent.args wires no --permission-prompt-tool by hand (k172): keep
// the two in step. An external agent (agent disabled, -no-agent) can call
// approve through its own flag, and so can a headless agent whose args
// carry the flag by hand, naming approveTool. A hand-wired flag that names
// another tool, even next to one naming approveTool, warns: which one
// Claude Code takes is unverified. An interactive agent always warns,
// whether claudeIntegration is set and whether the flag is wired by hand:
// the flag is a headless feature, and the warning's fix is to run headless.
// Maintainer's decision for k38 and k46: warn and start anyway, do not
// refuse.
//
// claudeIntegration: false with an interactive agent and no hand-wired flag
// is one warning, not two: the launcher then passes no permission flag at
// all, so the interactive text's "passed instead" would be wrong, and the
// fix needs both steps. With a hand-wired flag the cause is the window, so
// that text names the flag and asks for headless, and to set the flag to
// approveTool when it names another tool.
func permissionsInertReason(cfg *config.Config, agentInteractive bool) string {
	if !cfg.Permissions.Enabled || !cfg.Agent.Enabled {
		return ""
	}
	tools := promptToolsIn(cfg.Agent.Args)
	handWired := len(tools) > 0
	other := slices.IndexFunc(tools, isNotApprove) // a hand-wired tool that is not approve, or -1
	if !agentInteractive && (cfg.Agent.ClaudeIntegration || handWired) && other < 0 {
		return ""
	}
	const notEnforced = "permissions are not enforced: "
	policy := "permissions.mode (" + cfg.Permissions.Mode + "), allow and deny are never consulted"
	headless := `run the agent headless: put "-p" (or "--print") and its task in agent.args, and leave agent.window unset or false`
	var names string
	if other >= 0 {
		names = fmt.Sprintf("names %q, not %s", tools[other], approveTool)
	}
	if !agentInteractive && other >= 0 {
		reason := notEnforced + "the --permission-prompt-tool wired into agent.args by hand " + names + ", so " + policy + "."
		fix := " To enforce the policy, set it to " + approveTool
		if cfg.Agent.ClaudeIntegration {
			reason += " The launcher passes no --permission-prompt-tool of its own next to it."
			fix += ", or remove it from agent.args: with agent.claudeIntegration the launcher passes its own"
		}
		return reason + fix + "."
	}
	if !cfg.Agent.ClaudeIntegration && !handWired {
		fix := "set agent.claudeIntegration: true (Claude Code only)"
		if agentInteractive {
			fix += " and " + headless
		}
		return notEnforced + "agent.claudeIntegration is false, so the launcher passes the agent no --permission-prompt-tool and " +
			policy + ". To enforce the policy, " + fix + "."
	}
	window := "the agent runs interactively in window mode (agent.window: true, or agent.window unset and no -p/--print in agent.args), and "
	fix := " To enforce the policy, " + headless + "."
	if handWired {
		also := ""
		if cfg.Agent.ClaudeIntegration && cfg.Permissions.Mode == config.ModeAllowAll {
			also = " The launcher also passes --dangerously-skip-permissions (mode allow_all)."
		}
		if other >= 0 {
			fix += " That flag " + names + ", so set it to " + approveTool + " as well."
		}
		return notEnforced + window + "the --permission-prompt-tool wired into agent.args by hand takes effect only for a headless agent, so " +
			policy + "." + also + fix
	}
	instead := "The launcher passes no permission flag instead: Claude Code asks in its window as usual, and you answer each prompt yourself."
	if cfg.Permissions.Mode == config.ModeAllowAll {
		instead = "The launcher passes --dangerously-skip-permissions instead (mode allow_all): every tool call runs without a prompt, including one a deny rule names."
	}
	return notEnforced + window + "the launcher passes --permission-prompt-tool only to a headless agent, so " + policy + ". " + instead + fix
}

func strictWord(strict bool) string {
	if strict {
		return ", strict"
	}
	return ""
}

// subcommand is a word the launcher takes as its first positional argument.
// run runs instead of a launch and gets the arguments around the word, see
// extractSubcommand; summary is the word's line in the launcher's -h.
type subcommand struct {
	run     func(args []string) error
	summary string
}

var subcommands = map[string]subcommand{
	"hermes-setup": {
		run:     func(args []string) error { return hermesSetup(args, os.Stdout) },
		summary: "merge the unreagent MCP server into Hermes Agent's config.yaml (needs agent.hermes.enabled)",
	},
	"init": {
		run:     func(args []string) error { return initCommand(args, os.Stdout) },
		summary: "write the built-in defaults as unreagent.yaml next to the .uproject (never over an existing config) and print a preflight of the setup",
	},
}

// printUsage is the launcher's -h, which the flag package also prints after
// a bad flag: how to call the launcher, one line per sub-command, then the
// flags of fs, the launcher's flag set. It writes to fs.Output().
func printUsage(fs *flag.FlagSet) {
	w := fs.Output()
	names := slices.Sorted(maps.Keys(subcommands))
	width := 0
	for _, name := range names {
		width = max(width, len(name))
	}
	fmt.Fprint(w, "Usage:\n"+
		"  unreagent [flags]                          start the editor and the agent\n"+
		"  unreagent [flags] <sub-command> [flags]    run the sub-command instead, then exit\n"+
		"\nSub-commands (unreagent <sub-command> -h lists its flags):\n")
	for _, name := range names {
		fmt.Fprintf(w, "  %-*s  %s\n", width, name, subcommands[name].summary)
	}
	fmt.Fprint(w, "\nFlags:\n")
	fs.PrintDefaults()
}

// usageError is a command line the launcher cannot run. main exits 2 for it,
// the code the flag package gives a bad flag, and prints msg unless printed
// is set: a sub-command's flag set has printed its error and its usage
// itself.
type usageError struct {
	msg     string
	printed bool
}

func (e usageError) Error() string { return e.msg }

// extractSubcommand parses args (os.Args[1:]) with fs, the launcher's flag
// set, and returns the sub-command the first positional argument names.
// Parsing stops at that word, and it does so as the flags are defined, so
// neither a bool flag (-no-agent hermes-setup) nor -flag=value takes it as a
// value. rest is args without the word, the flags on both sides of it as
// typed: the sub-command parses them with its own flag set, so
// `-config x hermes-setup` and `hermes-setup -config x` mean the same. A "--"
// that ended the launcher's flags is left out of rest; passed on, it would
// end the sub-command's flags before the first one after the word.
// name == "" with a nil error is a plain launch. A positional that is no
// sub-command is a usageError.
func extractSubcommand(fs *flag.FlagSet, args []string) (name string, rest []string, err error) {
	if err = fs.Parse(args); err != nil {
		return "", nil, err
	}
	if fs.NArg() == 0 {
		return "", nil, nil
	}
	i := len(args) - fs.NArg() // fs.Args() is the tail of args from the word on
	name = args[i]
	if _, ok := subcommands[name]; !ok {
		return "", nil, unknownSubcommand(name)
	}
	before := args[:i]
	if endsWithTerminator(fs, before) {
		before = before[:i-1]
	}
	return name, slices.Concat(before, args[i+1:]), nil
}

// endsWithTerminator reports whether flags, the arguments fs parsed before
// the first positional one, end with the "--" that stopped the parse, not
// with a "--" that is a flag's value (-config --). It walks them as the flag
// package does: a flag that is no bool and has no "=" takes the next argument
// as its value.
func endsWithTerminator(fs *flag.FlagSet, flags []string) bool {
	for j := 0; j < len(flags); j++ {
		if flags[j] == "--" {
			return j == len(flags)-1 // the parse stops at the first one
		}
		name, _, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(flags[j], "-"), "-"), "=")
		f := fs.Lookup(name)
		if f == nil || hasValue {
			continue
		}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !b.IsBoolFlag() {
			j++ // the next argument is this flag's value
		}
	}
	return false
}

// unknownSubcommand is the usage error for a positional that names no
// sub-command. One that is an existing path gets its own text: a file dropped
// onto the .exe arrives that way, and the launcher used to start with the
// file ignored.
func unknownSubcommand(word string) error {
	known := "sub-commands: " + strings.Join(slices.Sorted(maps.Keys(subcommands)), ", ")
	if _, err := os.Stat(word); err == nil {
		return usageError{msg: `"` + word + `" is not a sub-command: unreagent does not open files passed to it ` +
			`(such as one dropped onto the .exe); it reads unreagent.yaml next to the .exe, or the file ` +
			`given with -config <path> (` + known + `)`}
	}
	return usageError{msg: `unknown sub-command "` + word + `" (` + known + `)`}
}
