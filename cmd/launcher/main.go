// Command launcher (unreagent) startet und überwacht den Unreal-Editor und
// einen Agenten (z.B. Claude Code) und bietet dem Agenten einen MCP-Server, mit
// dem er den Editor steuern (start/stop/restart), Befehle ausführen (compile,
// package), Logs lesen und Python-/Node-Code in vorbereiteten Umgebungen
// ausführen kann.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	cfgPath := flag.String("config", "", "Pfad zur config.json (Standard: neben der ausführbaren Datei)")
	flag.Parse()

	path := *cfgPath
	if path == "" {
		path = defaultConfigPath()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Config nicht lesbar (%s): %w", path, err)
	}
	cfg, err := config.Parse(raw)
	if err != nil {
		return err
	}

	logger := func(line string) {
		fmt.Printf("%s  %s\n", time.Now().Format("15:04:05"), line)
	}
	logger(fmt.Sprintf("unreagent %s — Config: %s", version, path))

	// Pfade prüfen (nur Warnung, damit der Start auch bei Tippfehlern erklärt).
	warnIfMissing(logger, "unreal.editor", cfg.Unreal.Editor)
	if cfg.Unreal.Project != "" {
		warnIfMissing(logger, "unreal.project", cfg.Unreal.Project)
	}

	sup := supervisor.New(logger)

	// --- Unreal-Service ---
	ueArgs := append([]string{}, cfg.Unreal.Args...)
	if cfg.Unreal.Project != "" {
		ueArgs = append([]string{cfg.Unreal.Project}, ueArgs...)
	}
	sup.AddService(supervisor.ServiceSpec{
		Name:         "ue",
		Command:      cfg.Unreal.Editor,
		Args:         ueArgs,
		Autostart:    !cfg.Unreal.ManualStart,
		Restart:      cfg.Unreal.Restart,
		MaxRestarts:  cfg.Unreal.MaxRestarts,
		RestartDelay: secs(cfg.Unreal.RestartDelaySeconds),
	})

	// --- Agent-Service ---
	agentWorkdir := cfg.Agent.Workdir
	if agentWorkdir == "" && cfg.Unreal.Project != "" {
		agentWorkdir = filepath.Dir(cfg.Unreal.Project)
	}
	mcpURL := "http://" + cfg.MCP.Address + "/mcp"
	if cfg.Agent.Enabled {
		agentArgs := append([]string{}, cfg.Agent.Args...)
		var agentEnv []string
		if cfg.MCP.Enabled && cfg.Agent.ClaudeIntegration {
			inline := map[string]interface{}{
				"mcpServers": map[string]interface{}{
					config.MCPServerName: map[string]interface{}{"type": "http", "url": mcpURL},
				},
			}
			b, _ := json.Marshal(inline)
			agentArgs = append(agentArgs, "--mcp-config", string(b))
			if cfg.Permissions.Enabled {
				agentArgs = append(agentArgs, "--permission-prompt-tool", "mcp__"+config.MCPServerName+"__approve")
			}
			agentEnv = append(agentEnv, "UNREAGENT_MCP_URL="+mcpURL)
			logger("Agent: Claude-Integration aktiv (--mcp-config" + perm(cfg.Permissions.Enabled) + ")")
		}
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
		})
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

	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	prepareRuntimes(sup, cfg, agentWorkdir, logger)
	go commandLoop(ctx, stop, sup, logger)

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

// commandLoop liest Steuerbefehle von stdin (für manuelle Bedienung).
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

func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(exe), "config.json")
}

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

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
		return " + --permission-prompt-tool"
	}
	return ""
}
