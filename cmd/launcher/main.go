// Command launcher (unreagent) startet und überwacht den Unreal-Editor und
// einen Agenten (z.B. Claude Code) und bietet dem Agenten einen MCP-Server, mit
// dem er den Editor steuern (start/stop/restart), Befehle ausführen (compile,
// package), Logs lesen und Python-/Node-Code in vorbereiteten Umgebungen
// ausführen kann.
package main

import (
	"bufio"
	"bytes"
	"context"
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
)

// version wird beim Release-Build per -ldflags "-X main.version=<tag>" gesetzt.
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "", "Pfad zur unreagent.yaml (Standard: neben der ausführbaren Datei)")
	noAgent := flag.Bool("no-agent", false, "Agenten nicht starten (nur UE + MCP-Server; externer Agent kann sich verbinden)")
	filesFlag := flag.Bool("files", false, "Datei-Tools (read/write/list/edit) über den MCP-Server aktivieren")
	writeMcp := flag.String("write-mcp-config", "", "MCP-Config zusätzlich als .mcp.json an diesen Pfad schreiben")
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
		logger("Lokales Overlay: " + info.LocalPath)
	}
	if info.EngineRoot != "" {
		logger("Engine: " + info.EngineRoot)
	} else {
		logger("WARN Engine nicht gefunden — UE_ROOT oder engineRoot in unreagent.local.yaml setzen")
	}
	if info.Project != "" {
		logger("Projekt: " + info.Project)
	} else {
		logger("WARN keine .uproject neben der Config gefunden — unreal.project setzen")
	}
	// CLI-Overrides.
	if *noAgent && cfg.Agent.Enabled {
		cfg.Agent.Enabled = false
		logger("Flag -no-agent: Agent wird nicht gestartet (MCP-Server bleibt verfügbar)")
	}
	if *filesFlag && !cfg.Files.Enabled {
		cfg.Files.Enabled = true
	}
	if *writeMcp != "" {
		cfg.MCP.WriteConfig = append(cfg.MCP.WriteConfig, config.MCPOutput{Path: *writeMcp, Format: "mcp_json"})
	}

	// Interaktiver Agent: er erbt die echte Konsole (TTY). Damit Claude das
	// Fenster für sich hat, leiten wir die Launcher-Logs in eine Datei um und
	// lassen den eigenen stdin-Command-Loop weg. Im headless -p Modus aus.
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
			fmt.Printf("Launcher-Logs -> %s   (Unreal läuft im Hintergrund)\n", logPath)
			fmt.Println("Der Agent (Claude) übernimmt dieses Fenster …")
			fmt.Println()
		}
	}

	warnIfMissing(logger, "unreal.editor", cfg.Unreal.Editor)
	if cfg.Agent.Enabled {
		if _, lookErr := exec.LookPath(cfg.Agent.Command); lookErr != nil {
			logger("WARN Agent-Command nicht im PATH: " + cfg.Agent.Command + " (vollen Pfad in unreagent.local.yaml setzen)")
		}
	}
	if cfg.Files.Enabled {
		mode := "read/write"
		if cfg.Files.ReadOnly {
			mode = "read-only"
		}
		logger(fmt.Sprintf("Datei-Tools aktiv (%s) unter: %s", mode, cfg.Files.Root))
	}

	sup := supervisor.New(logger)

	// --- Unreal-Service ---
	ueArgs := append([]string{}, cfg.Unreal.Args...)
	if cfg.Unreal.Project != "" {
		ueArgs = append([]string{cfg.Unreal.Project}, ueArgs...)
	}
	if boolVal(cfg.Unreal.Unattended) && !hasArg(ueArgs, "-unattended") {
		ueArgs = append(ueArgs, "-unattended")
		logger("Unreal: -unattended aktiv (kein Crash-Dialog, kein Recovery-Prompt)")
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

	// --- Agent-Service ---
	// onAgentExit wird erst gesetzt, wenn ctx/stop existieren (s.u.); der Service
	// hält nur eine Indirektion darauf.
	var onAgentExit func(success bool)
	agentWorkdir := cfg.Agent.Workdir
	if agentWorkdir == "" && cfg.Unreal.Project != "" {
		agentWorkdir = filepath.Dir(cfg.Unreal.Project)
	}
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
					// Interaktiv: --permission-prompt-tool ist ein Headless-Feature
					// (-p) und bricht sonst ab. allow_all -> Prompts überspringen;
					// andere Modi -> Claude fragt im Fenster nach (manuell).
					if cfg.Permissions.Mode == config.ModeAllowAll {
						agentArgs = append(agentArgs, "--dangerously-skip-permissions")
					}
				} else {
					agentArgs = append(agentArgs, "--permission-prompt-tool", "mcp__"+config.MCPServerName+"__approve")
				}
			}
			agentEnv = append(agentEnv, "UNREAGENT_MCP_URL="+mcpURL)
			logger(fmt.Sprintf("Agent: Claude-Integration aktiv (%d MCP-Server%s)",
				len(mcpServers), strictWord(cfg.MCP.Strict)))
		}
		logger("Agent-Kommando: " + cfg.Agent.Command + " " + strings.Join(agentArgs, " "))
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
			logger("Agent: läuft interaktiv im Vordergrund (erbt die Konsole)")
		}
	}

	// --- MCP-Config-Dateien schreiben (für externe Clients) ---
	if cfg.MCP.Enabled && len(cfg.MCP.WriteConfig) > 0 {
		base := agentWorkdir
		if base == "" {
			base = "."
		}
		writeMCPConfigs(cfg.MCP.WriteConfig, mcpServers, base, logger)
	}

	// --- Einmal-Befehle ---
	for name, c := range cfg.Commands {
		sup.AddCommand(name, supervisor.CommandSpec{
			Description: c.Description,
			Command:     c.Command,
			Args:        c.Args,
			Dir:         c.Dir,
		})
	}

	// --- MCP-Server ---
	var httpSrv *http.Server
	if cfg.MCP.Enabled {
		srv := mcp.NewServer(config.MCPServerName, version, logger)
		registerTools(srv, sup, cfg, agentWorkdir, logger)
		mux := http.NewServeMux()
		mux.Handle("/mcp", srv)
		mux.Handle("/", srv)
		httpSrv = &http.Server{Addr: cfg.MCP.Address, Handler: mux}
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger("MCP-Server Fehler: " + err.Error())
			}
		}()
		logger("MCP-Server läuft auf " + mcpURL)
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
	// Im interaktiven Modus gehört stdin dem Agenten — kein eigener Command-Loop.
	if !agentInteractive {
		go commandLoop(ctx, stop, sup, logger)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		logger("Signal empfangen — beende alle Prozesse …")
	case <-done:
		logger("Alle Services beendet.")
	}
	stop()
	wg.Wait()
	if httpSrv != nil {
		_ = httpSrv.Close()
	}
	logger("Tschüss.")
	return nil
}

// registerTools registriert alle MCP-Tools. Die Description-Texte sind bewusst
// ausführlich — sie sind die "Anleitung", die der Agent automatisch sieht.
func registerTools(srv *mcp.Server, sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, logger func(string)) {
	noArgs := map[string]interface{}{"type": "object", "additionalProperties": false}

	srv.AddTool(mcp.Tool{
		Name:        "status",
		Description: "Liefert den Status aller verwalteten Prozesse (Unreal-Editor, Agent): laufend/gestoppt, PID, Anzahl Neustarts. Außerdem die Liste der verfügbaren Einmal-Befehle für run_command. Nutze dies, um zu prüfen, ob der Editor läuft, bevor du ihn steuerst.",
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
		Description: "Startet den Unreal-Editor, falls er nicht läuft. No-op, wenn er bereits läuft.",
		InputSchema: noArgs,
		Handler:     serviceAction(sup, "ue", sup.StartService),
	})
	srv.AddTool(mcp.Tool{
		Name:        "ue_stop",
		Description: "Stoppt den Unreal-Editor und verhindert Auto-Restart, bis er wieder explizit gestartet wird. Beendet den gesamten Prozessbaum (keine Waisenprozesse).",
		InputSchema: noArgs,
		Handler:     serviceAction(sup, "ue", sup.StopService),
	})
	srv.AddTool(mcp.Tool{
		Name:        "ue_restart",
		Description: "Startet den Unreal-Editor neu (stop + start). Nutze dies nach einem C++-Build, damit der Editor die neuen Module lädt, oder wenn der Editor hängt.",
		InputSchema: noArgs,
		Handler:     serviceAction(sup, "ue", sup.RestartService),
	})

	srv.AddTool(mcp.Tool{
		Name:        "logs",
		Description: "Liefert die letzten Ausgabezeilen eines Service (stdout+stderr). Nutze service='ue' für Editor-/Build-Ausgaben, service='agent' für den Agenten. Praktisch, um Compile-Fehler oder Crash-Meldungen zu lesen.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service": map[string]interface{}{"type": "string", "description": "Service-Name (ue, agent). Default: ue"},
				"lines":   map[string]interface{}{"type": "integer", "description": "Anzahl Zeilen (Default 50)"},
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
		Description: "Führt einen vorkonfigurierten Einmal-Befehl synchron aus und gibt dessen Ausgabe + Exit-Code zurück. Typische Befehle: compile (C++-Module bauen), package (Build erstellen). Verfügbare Befehle: " +
			strings.Join(cmdNames, ", ") + ". Nach 'compile' empfiehlt sich ue_restart.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Name des Befehls (siehe status.commands)"},
			},
			"required": []string{"name"},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			name := getString(args, "name", "")
			if name == "" {
				return mcp.ToolResult{Text: "Fehler: 'name' fehlt", IsError: true}
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

	// --- Runtime-Tools ---
	if cfg.Runtimes.Python.Enabled {
		dir := cfg.Runtimes.Python.Project
		if dir == "" {
			dir = agentWorkdir
		}
		uv := cfg.Runtimes.Python.UV
		srv.AddTool(mcp.Tool{
			Name:        "run_python",
			Description: "Führt Python-Code in einer sauberen, uv-verwalteten Umgebung aus und gibt stdout/stderr + Exit-Code zurück. Die Umgebung (venv, Abhängigkeiten aus pyproject.toml/requirements, passende Python-Version) wird automatisch von uv bereitgestellt — du musst KEIN venv anlegen, nichts installieren und die Umgebung nicht analysieren. Übergib einfach den Code. Für zusätzliche Pakete nutze in pyproject.toml deklarierte Deps; ad-hoc geht 'import' nur für bereits vorhandene.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code": map[string]interface{}{"type": "string", "description": "Auszuführender Python-Code"},
				},
				"required": []string{"code"},
			},
			Handler: scriptAction(sup, uv, []string{"run", "python"}, dir, "py", "python"),
		})
	}
	if cfg.Runtimes.Node.Enabled {
		dir := cfg.Runtimes.Node.Project
		if dir == "" {
			dir = agentWorkdir
		}
		node := cfg.Runtimes.Node.Node
		srv.AddTool(mcp.Tool{
			Name:        "run_node",
			Description: "Führt Node.js-Code im Projektkontext aus und gibt stdout/stderr + Exit-Code zurück. node_modules des Projekts werden aufgelöst — du musst die Umgebung nicht selbst einrichten oder analysieren. Übergib einfach den Code (CommonJS oder ESM je nach package.json).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code": map[string]interface{}{"type": "string", "description": "Auszuführender JavaScript-Code"},
				},
				"required": []string{"code"},
			},
			Handler: scriptAction(sup, node, nil, dir, "mjs", "node"),
		})
	}

	// --- Datei-Tools (auf cfg.Files.Root beschränkt) ---
	if cfg.Files.Enabled {
		root := cfg.Files.Root
		if root == "" {
			root = agentWorkdir
		}
		registerFileTools(srv, root, cfg.Files.ReadOnly)
	}

	// --- Permission-Tool ---
	if cfg.Permissions.Enabled {
		srv.AddTool(mcp.Tool{
			Name:        "approve",
			Description: "Permission-Prompt-Tool für die Claude-Code-CLI (--permission-prompt-tool). Wird vom Harness aufgerufen, nicht direkt vom Modell. Entscheidet anhand der Launcher-Policy, ob ein Tool-Aufruf erlaubt wird.",
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

// serviceAction baut einen Handler für eine Lifecycle-Aktion.
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

// scriptAction baut einen Handler, der Code in eine temporäre Datei schreibt und
// per command (pre-args + Datei) ausführt.
func scriptAction(sup *supervisor.Supervisor, command string, pre []string, dir, ext, label string) mcp.ToolHandler {
	return func(args map[string]interface{}) mcp.ToolResult {
		code := getString(args, "code", "")
		if code == "" {
			return mcp.ToolResult{Text: "Fehler: 'code' fehlt", IsError: true}
		}
		f, err := os.CreateTemp("", "unreagent-*."+ext)
		if err != nil {
			return errResult(err)
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

// registerFileTools registriert read/list (+ write/edit, falls nicht readOnly)
// auf dem MCP-Server, alle Pfade strikt auf root beschränkt.
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
			return "", fmt.Errorf("Pfad außerhalb des erlaubten Roots")
		}
		return abs, nil
	}

	srv.AddTool(mcp.Tool{
		Name:        "read_file",
		Description: "Liest eine Textdatei aus dem UE-Projekt (Pfad relativ zum Projekt-Root). Damit kann ein Agent Quellcode/Config/Logs lesen, ohne lokal anwesend zu sein. Große Dateien werden gekürzt.",
		InputSchema: pathSchema("path", "Pfad zur Datei, relativ zum Projekt-Root"),
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			if path == "" {
				return mcp.ToolResult{Text: "Fehler: 'path' fehlt", IsError: true}
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
				return mcp.ToolResult{Text: string(b[:max]) + "\n…[gekürzt]"}
			}
			return mcp.ToolResult{Text: string(b)}
		},
	})

	srv.AddTool(mcp.Tool{
		Name:        "list_dir",
		Description: "Listet Einträge eines Verzeichnisses im UE-Projekt (Pfad relativ zum Root, leer = Root). Verzeichnisse enden mit /.",
		InputSchema: pathSchema("path", "Verzeichnis relativ zum Root (leer = Root)"),
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
		Description: "Schreibt/überschreibt eine Textdatei im UE-Projekt (Pfad relativ zum Root). Fehlende Verzeichnisse werden angelegt. Damit kann ein Agent Dateien erstellen/ändern, ohne lokal anwesend zu sein.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "Pfad relativ zum Root"},
				"content": map[string]interface{}{"type": "string", "description": "Neuer Dateiinhalt"},
			},
			"required": []string{"path", "content"},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			if path == "" {
				return mcp.ToolResult{Text: "Fehler: 'path' fehlt", IsError: true}
			}
			abs, err := resolve(path)
			if err != nil {
				return errResult(err)
			}
			content := getString(args, "content", "")
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return errResult(err)
			}
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				return errResult(err)
			}
			return mcp.ToolResult{Text: fmt.Sprintf("geschrieben: %s (%d Bytes)", path, len(content))}
		},
	})

	srv.AddTool(mcp.Tool{
		Name:        "edit_file",
		Description: "Ersetzt in einer Textdatei alle Vorkommen von old_string durch new_string (Pfad relativ zum Root). Fehler, wenn old_string nicht vorkommt.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":       map[string]interface{}{"type": "string", "description": "Pfad relativ zum Root"},
				"old_string": map[string]interface{}{"type": "string", "description": "zu ersetzender Text"},
				"new_string": map[string]interface{}{"type": "string", "description": "Ersatztext"},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			abs, err := resolve(path)
			if err != nil {
				return errResult(err)
			}
			oldS := getString(args, "old_string", "")
			if oldS == "" {
				return mcp.ToolResult{Text: "Fehler: 'old_string' fehlt", IsError: true}
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return errResult(err)
			}
			content := string(b)
			n := strings.Count(content, oldS)
			if n == 0 {
				return mcp.ToolResult{Text: "Fehler: 'old_string' nicht gefunden", IsError: true}
			}
			content = strings.ReplaceAll(content, oldS, getString(args, "new_string", ""))
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				return errResult(err)
			}
			return mcp.ToolResult{Text: fmt.Sprintf("%d Vorkommen ersetzt in %s", n, path)}
		},
	})
}

func pathSchema(name, desc string) map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			name: map[string]interface{}{"type": "string", "description": desc},
		},
	}
}

// prepareRuntimes wärmt die Umgebungen vor (uv sync / npm install), falls
// konfiguriert und ein Manifest vorhanden ist. Läuft asynchron.
func prepareRuntimes(sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, logger func(string)) {
	if cfg.Runtimes.Python.Enabled && cfg.Runtimes.Python.PrepareOnStart {
		dir := cfg.Runtimes.Python.Project
		if dir == "" {
			dir = agentWorkdir
		}
		if fileExists(filepath.Join(dir, "pyproject.toml")) {
			logger("Runtime: bereite Python vor (uv sync) …")
			go func() { _, _ = sup.RunOnce(cfg.Runtimes.Python.UV, []string{"sync"}, dir, nil, "prepare:python") }()
		}
	}
	if cfg.Runtimes.Node.Enabled && cfg.Runtimes.Node.PrepareOnStart {
		dir := cfg.Runtimes.Node.Project
		if dir == "" {
			dir = agentWorkdir
		}
		if fileExists(filepath.Join(dir, "package.json")) {
			logger("Runtime: bereite Node vor (npm install) …")
			go func() { _, _ = sup.RunOnce(cfg.Runtimes.Node.Npm, []string{"install"}, dir, nil, "prepare:node") }()
		}
	}
}

// prepareMCPBridges prüft alle stdio-extraServers (z.B. die UE-LLM-Toolkit-
// Bridge) VOR dem Service-Start einmal komplett durch und beschwert sich bei
// jedem Glied der Kette laut, statt den Agenten in einen nichtssagenden
// MCP-Connect-Fehler (-32000) laufen zu lassen:
//  1. Binary auffindbar (node etc. im PATH)?
//  2. Node-Bridges: Skript vorhanden? node_modules da? (sonst npm install)
//  3. Smoke-Test: Server starten, MCP-initialize senden, Antwort abwarten.
//  4. UNREAL_MCP_URL: asynchron melden, sobald der In-Editor-Server erreichbar
//     ist — oder warnen, wenn nicht.
func prepareMCPBridges(sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, logger func(string)) {
	for name, def := range cfg.MCP.ExtraServers {
		command, _ := def["command"].(string)
		if command == "" {
			continue // http/sse-Server — startet kein Prozess, nichts zu prüfen
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
			logger("WARN MCP-Server '" + name + "': Befehl '" + command + "' nicht gefunden (PATH) — Bridge kann nicht starten. Installieren oder vollen Pfad in der Config eintragen.")
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

// prepareNodeBridge stellt sicher, dass Skript und node_modules einer Node-
// Bridge vorhanden sind (npm install bei Bedarf). false = Bridge unbrauchbar.
func prepareNodeBridge(sup *supervisor.Supervisor, cfg *config.Config, name string, args []string, agentWorkdir string, logger func(string)) bool {
	var script string
	for _, a := range args {
		if strings.HasSuffix(a, ".js") || strings.HasSuffix(a, ".mjs") || strings.HasSuffix(a, ".cjs") {
			script = a
			break
		}
	}
	if script == "" {
		return true // kein Skript erkennbar — Smoke-Test entscheidet
	}
	if !filepath.IsAbs(script) {
		script = filepath.Join(agentWorkdir, script)
	}
	if !fileExists(script) {
		logger("WARN MCP-Server '" + name + "': Skript nicht gefunden: " + script + " — ist das Plugin installiert?")
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
	logger("MCP-Server '" + name + "': node_modules fehlt — installiere Abhängigkeiten (npm install) in " + dir + " …")
	res, err := sup.RunOnce(npm, []string{"install"}, dir, nil, "prepare:mcp:"+name)
	if err != nil {
		logger("WARN MCP-Server '" + name + "': npm install fehlgeschlagen: " + err.Error() + " — Bridge wird nicht starten")
		return false
	}
	if res.ExitCode != 0 {
		logger(fmt.Sprintf("WARN MCP-Server '%s': npm install exit %d — Bridge wird nicht starten\n%s",
			name, res.ExitCode, tailLines(res.Output, 20)))
		return false
	}
	logger("MCP-Server '" + name + "': Abhängigkeiten installiert.")
	return true
}

// smokeTestMCP startet den stdio-Server einmal probeweise, schickt ein echtes
// MCP-initialize und wartet auf die Antwort. Scheitert der Start, steht die
// echte Ursache (stderr) im Log statt nur ein Connect-Fehler beim Agenten.
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
		logger("WARN MCP-Server '" + name + "': Smoke-Test konnte Pipes nicht öffnen")
		return
	}
	if err := cmd.Start(); err != nil {
		logger("WARN MCP-Server '" + name + "': Smoke-Test-Start fehlgeschlagen: " + err.Error())
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
		logger("MCP-Server '" + name + "': Smoke-Test OK — Bridge antwortet auf initialize.")
		return
	}
	detail := strings.TrimSpace(stderr.String())
	reason := "keine initialize-Antwort"
	if ctx.Err() == context.DeadlineExceeded {
		reason = "Timeout nach 20s"
	}
	if detail != "" {
		logger(fmt.Sprintf("WARN MCP-Server '%s': Smoke-Test fehlgeschlagen (%s):\n%s", name, reason, tailLines(detail, 15)))
	} else {
		logger("WARN MCP-Server '" + name + "': Smoke-Test fehlgeschlagen (" + reason + ", kein stderr)")
	}
}

// waitForEndpoint meldet, sobald der In-Editor-HTTP-Server (UNREAL_MCP_URL)
// erreichbar ist — der Editor braucht beim Start Zeit. Kommt binnen 5 Minuten
// keine Verbindung zustande, gibt es eine deutliche Warnung mit Verdächtigen.
func waitForEndpoint(name, rawURL string, logger func(string)) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		logger("WARN MCP-Server '" + name + "': UNREAL_MCP_URL unverständlich: " + rawURL)
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
			logger("MCP-Server '" + name + "': In-Editor-Server " + rawURL + " ist erreichbar.")
			return
		}
		time.Sleep(5 * time.Second)
	}
	logger("WARN MCP-Server '" + name + "': In-Editor-Server " + rawURL + " nach 5 Minuten nicht erreichbar — läuft der Editor mit dem Plugin? Firewall? Lauscht der Server nur auf localhost?")
}

// commandLoop liest Steuerbefehle von stdin (für manuelle Bedienung).
// makeAgentExitHandler baut den Callback, der feuert, wenn der Agent endet und
// nicht von selbst neugestartet wird (siehe ServiceSpec.OnExit). Der Agent ist
// im Fenster-Modus der Leitprozess — endet er, ist die Session vorbei und der
// Launcher darf nicht stumm mit laufendem Editor hängenbleiben.
func makeAgentExitHandler(ctx context.Context, stop func(), sup *supervisor.Supervisor, cfg *config.Config, logger func(string), interactive bool) func(success bool) {
	var mu sync.Mutex
	cmdLoopRunning := false // schon in die Launcher-Konsole gewechselt?
	return func(success bool) {
		if success {
			logger("Agent beendet (exit 0).")
		} else {
			logger("Agent abgestürzt / Neustarts erschöpft.")
		}

		// Läuft bereits die Launcher-Konsole (vorherige 'k'-Wahl), würde ein
		// zweiter stdin-Prompt mit ihr um die Eingabe konkurrieren — also nur loggen.
		mu.Lock()
		busy := cmdLoopRunning
		mu.Unlock()
		if busy {
			logger("Launcher-Konsole aktiv — 'q' beendet alles, 'start agent' startet den Agenten neu.")
			return
		}

		switch cfg.Agent.OnExit {
		case config.OnExitLeave:
			logger("agent.onExit=leave — Editor/MCP laufen weiter. Ctrl-C beendet den Launcher.")
		case config.OnExitShutdown:
			logger("agent.onExit=shutdown — beende alles.")
			stop()
		default: // ask
			if !interactive {
				// Headless (-p): kein TTY zum Nachfragen — der Command-Loop liest
				// stdin. Agent-Ende = Auftrag erledigt -> sauber herunterfahren.
				stop()
				return
			}
			switch promptAgentExit(success) {
			case "k":
				fmt.Fprintln(os.Stdout, "Editor läuft weiter. Launcher-Konsole:")
				mu.Lock()
				cmdLoopRunning = true
				mu.Unlock()
				go commandLoop(ctx, stop, sup, logger)
			case "r":
				fmt.Fprintln(os.Stdout, "Starte Agent neu …")
				if _, err := sup.StartService("agent"); err != nil {
					logger("Agent-Neustart fehlgeschlagen: " + err.Error() + " — beende alles.")
					stop()
				}
			default: // Enter / q / Timeout / EOF
				stop()
			}
		}
	}
}

// promptAgentExit zeigt das Auswahlmenü auf der echten Konsole (os.Stdout/Stdin —
// die Launcher-Logs gehen im Fenster-Modus in die Datei) und liefert die Wahl.
// Nach 30s ohne Eingabe gilt "alles beenden" (z.B. Agent über Nacht abgestürzt).
func promptAgentExit(success bool) string {
	if success {
		fmt.Fprintln(os.Stdout, "\nAgent beendet.")
	} else {
		fmt.Fprintln(os.Stdout, "\nAgent abgestürzt.")
	}
	fmt.Fprintln(os.Stdout, "  [Enter] alles beenden (UE + Launcher)")
	fmt.Fprintln(os.Stdout, "  [k]     Editor weiterlaufen lassen, Launcher-Konsole")
	fmt.Fprintln(os.Stdout, "  [r]     Agent neu starten")
	fmt.Fprint(os.Stdout, "> ")

	ch := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		if sc.Scan() {
			ch <- strings.ToLower(strings.TrimSpace(sc.Text()))
		} else {
			ch <- "" // EOF (z.B. stdin geschlossen) -> alles beenden
		}
	}()
	select {
	case s := <-ch:
		return s
	case <-time.After(30 * time.Second):
		fmt.Fprintln(os.Stdout, "\n(Timeout) — beende alles.")
		return ""
	}
}

func commandLoop(ctx context.Context, stop func(), sup *supervisor.Supervisor, logger func(string)) {
	logger("Befehle: 'status' | 'r' (alle neu) | 'r <name>' | 'stop <name>' | 'start <name>' | 'c <name>' | 'q'")
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
			logger("Beende …")
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
					logger("Fehler: " + err.Error())
				} else {
					logger(fmt.Sprintf("exit %d\n%s", res.ExitCode, tailLines(res.Output, 100)))
				}
			}
		default:
			logger("Unbekannter Befehl: " + fields[0])
		}
	}
}

// --- Hilfsfunktionen ---

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

func boolVal(p *bool) bool { return p != nil && *p }

// buildMCPServers stellt die mcpServers-Map zusammen: unser Launcher-Server (HTTP)
// plus alle zusätzlichen (In-Editor-)MCP-Server aus der Config.
func buildMCPServers(cfg *config.Config, mcpURL string) map[string]interface{} {
	servers := map[string]interface{}{
		config.MCPServerName: map[string]interface{}{"type": "http", "url": mcpURL},
	}
	for name, def := range cfg.MCP.ExtraServers {
		servers[name] = def
	}
	return servers
}

// writeMCPConfigs schreibt die MCP-Config in die konfigurierten Datei-Ziele.
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
			logger("WARN MCP-Config (" + path + "): " + err.Error())
			continue
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			logger("WARN MCP-Config (" + path + "): " + err.Error())
			continue
		}
		format := out.Format
		if format == "" {
			format = "mcp_json"
		}
		logger("MCP-Config geschrieben: " + path + " (" + format + ")")
	}
}

// toVSCode wandelt die mcpServers-Map ins VS-Code-Format (stdio-Server bekommen
// type:stdio, falls nicht gesetzt).
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

// killCrashReporter beendet ein evtl. hängendes Crash-Reporter-Fenster.
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
		_ = exec.Command(c[0], c[1:]...).Run() // Fehler ignorieren (Prozess evtl. nicht vorhanden)
	}
}

// cleanRecovery entfernt Recovery-/Crash-Artefakte für einen sauberen Neustart.
func cleanRecovery(projectDir string, logger func(string)) {
	saved := filepath.Join(projectDir, "Saved")
	restore := filepath.Join(saved, "Autosaves", "PackageRestoreData.json")
	if err := os.Remove(restore); err == nil {
		logger("Recovery: PackageRestoreData.json entfernt")
	}
	crashes := filepath.Join(saved, "Crashes")
	if entries, err := os.ReadDir(crashes); err == nil && len(entries) > 0 {
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(crashes, e.Name()))
		}
		logger("Recovery: Saved/Crashes geleert")
	}
}

func warnIfMissing(logger func(string), label, path string) {
	if !fileExists(path) {
		logger(fmt.Sprintf("WARN %s: Pfad nicht gefunden: %s", label, path))
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
	return mcp.ToolResult{Text: "Fehler: " + err.Error(), IsError: true}
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
