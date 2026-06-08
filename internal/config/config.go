// Package config lädt und validiert die Launcher-Konfiguration (config.json).
//
// Das Format ist JSON, erlaubt aber ganze Kommentarzeilen (beginnend mit //
// oder #), damit die Datei von Hand bequem editierbar bleibt. Unter Windows
// dürfen Pfade Forward-Slashes verwenden ("C:/Program Files/..."), das spart
// die lästigen doppelten Backslashes.
package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Config ist die komplette Launcher-Konfiguration.
type Config struct {
	Unreal      UnrealConfig           `json:"unreal"`
	Agent       AgentConfig            `json:"agent"`
	Commands    map[string]CommandSpec `json:"commands"`
	MCP         MCPConfig              `json:"mcp"`
	Permissions Permissions            `json:"permissions"`
	Runtimes    Runtimes               `json:"runtimes"`
}

// Runtimes stellt dem Agenten saubere Ausführungsumgebungen bereit, sodass er
// Python-/Node-Code direkt ausführen kann, ohne selbst venv/Deps aufzusetzen.
type Runtimes struct {
	Python PythonRuntime `json:"python"`
	Node   NodeRuntime   `json:"node"`
}

// PythonRuntime nutzt uv: `uv run` baut/synct das venv automatisch aus
// pyproject.toml/requirements und installiert bei Bedarf die richtige
// Python-Version.
type PythonRuntime struct {
	Enabled        bool   `json:"enabled"`
	UV             string `json:"uv"`             // Pfad zur uv-Binary (default "uv")
	Project        string `json:"project"`        // Projektverzeichnis (default: Agent-Workdir)
	PrepareOnStart bool   `json:"prepareOnStart"` // beim Start `uv sync` ausführen
}

// NodeRuntime führt Node-Code im Projektkontext aus (node_modules werden
// aufgelöst).
type NodeRuntime struct {
	Enabled        bool   `json:"enabled"`
	Node           string `json:"node"`           // Pfad zur node-Binary (default "node")
	Npm            string `json:"npm"`            // Pfad zu npm (default "npm")
	Project        string `json:"project"`        // Projektverzeichnis (default: Agent-Workdir)
	PrepareOnStart bool   `json:"prepareOnStart"` // beim Start `npm install` ausführen
}

// UnrealConfig beschreibt den Unreal-Editor-Prozess.
type UnrealConfig struct {
	Editor              string   `json:"editor"`  // Pfad zu UnrealEditor.exe
	Project             string   `json:"project"` // Pfad zum .uproject (optional)
	Args                []string `json:"args"`    // zusätzliche Editor-Argumente
	ManualStart         bool     `json:"manualStart"`
	Restart             string   `json:"restart"`             // never | on-failure | always
	MaxRestarts         int      `json:"maxRestarts"`         // 0 = unbegrenzt
	RestartDelaySeconds int      `json:"restartDelaySeconds"` // Backoff
}

// AgentConfig beschreibt den Agenten-Prozess (z.B. Claude Code).
type AgentConfig struct {
	Enabled             bool     `json:"enabled"`
	Command             string   `json:"command"`
	Args                []string `json:"args"`
	Workdir             string   `json:"workdir"` // leer = Verzeichnis des .uproject
	StartDelaySeconds   int      `json:"startDelaySeconds"`
	Restart             string   `json:"restart"`
	MaxRestarts         int      `json:"maxRestarts"`
	RestartDelaySeconds int      `json:"restartDelaySeconds"`
	// ClaudeIntegration hängt automatisch --mcp-config und (falls Permissions
	// aktiv) --permission-prompt-tool an die Agent-Argumente an. Nur für die
	// Claude-Code-CLI sinnvoll; für generische Agenten false lassen.
	ClaudeIntegration bool `json:"claudeIntegration"`
}

// CommandSpec ist ein benannter Einmal-Befehl (z.B. "compile"), den der Agent
// über das MCP-Tool run_command auslösen kann.
type CommandSpec struct {
	Description string   `json:"description"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	Dir         string   `json:"dir"`
}

// MCPConfig steuert den eingebauten MCP-Server.
type MCPConfig struct {
	Enabled bool   `json:"enabled"`
	Address string `json:"address"` // z.B. "127.0.0.1:8765"
}

// Permissions steuert das Permission-Prompt-Tool für den Agenten.
//
// Mode:
//   - "allow_all"  – jede Anfrage wird genehmigt (außer deny-Treffer)
//   - "allowlist"  – nur Anfragen, die auf eine allow-Regel passen
//   - "deny_all"   – alles wird abgelehnt
type Permissions struct {
	Enabled bool     `json:"enabled"`
	Mode    string   `json:"mode"`
	Allow   []string `json:"allow"`
	Deny    []string `json:"deny"`
}

const (
	RestartNever     = "never"
	RestartOnFailure = "on-failure"
	RestartAlways    = "always"

	ModeAllowAll  = "allow_all"
	ModeAllowlist = "allowlist"
	ModeDenyAll   = "deny_all"

	DefaultMCPAddress = "127.0.0.1:8765"
	// MCPServerName ist der Name, unter dem der Server beim Agenten registriert
	// wird. Daraus ergibt sich der Tool-Name mcp__unreagent__approve.
	MCPServerName = "unreagent"
)

// Parse liest die Konfiguration aus rohem JSON (inkl. Kommentarzeilen),
// setzt Defaults und validiert sie.
func Parse(raw []byte) (*Config, error) {
	cleaned := stripComments(raw)
	var c Config
	dec := json.NewDecoder(strings.NewReader(cleaned))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config konnte nicht geparst werden: %w", err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Unreal.Restart == "" {
		c.Unreal.Restart = RestartOnFailure
	}
	if c.Unreal.RestartDelaySeconds == 0 {
		c.Unreal.RestartDelaySeconds = 3
	}
	if c.Agent.Restart == "" {
		c.Agent.Restart = RestartOnFailure
	}
	if c.Agent.RestartDelaySeconds == 0 {
		c.Agent.RestartDelaySeconds = 3
	}
	if c.MCP.Address == "" {
		c.MCP.Address = DefaultMCPAddress
	}
	if c.Permissions.Mode == "" {
		c.Permissions.Mode = ModeAllowlist
	}
	if c.Runtimes.Python.UV == "" {
		c.Runtimes.Python.UV = "uv"
	}
	if c.Runtimes.Node.Node == "" {
		c.Runtimes.Node.Node = "node"
	}
	if c.Runtimes.Node.Npm == "" {
		c.Runtimes.Node.Npm = "npm"
	}
}

func (c *Config) validate() error {
	if c.Unreal.Editor == "" {
		return fmt.Errorf("unreal.editor fehlt (Pfad zu UnrealEditor.exe)")
	}
	if err := validRestart(c.Unreal.Restart); err != nil {
		return fmt.Errorf("unreal.restart: %w", err)
	}
	if c.Agent.Enabled && c.Agent.Command == "" {
		return fmt.Errorf("agent.command fehlt (oder agent.enabled=false setzen)")
	}
	if c.Agent.Enabled {
		if err := validRestart(c.Agent.Restart); err != nil {
			return fmt.Errorf("agent.restart: %w", err)
		}
	}
	switch c.Permissions.Mode {
	case ModeAllowAll, ModeAllowlist, ModeDenyAll:
	default:
		return fmt.Errorf("permissions.mode ungültig: %q (erlaubt: allow_all, allowlist, deny_all)", c.Permissions.Mode)
	}
	for name, cmd := range c.Commands {
		if cmd.Command == "" {
			return fmt.Errorf("commands.%s.command fehlt", name)
		}
	}
	if c.Permissions.Enabled && !c.MCP.Enabled {
		return fmt.Errorf("permissions.enabled=true erfordert mcp.enabled=true (das approve-Tool läuft über den MCP-Server)")
	}
	return nil
}

func validRestart(p string) error {
	switch p {
	case RestartNever, RestartOnFailure, RestartAlways:
		return nil
	default:
		return fmt.Errorf("ungültige Policy %q (erlaubt: never, on-failure, always)", p)
	}
}

// stripComments entfernt ganze Zeilen, die (nach Whitespace) mit // oder #
// beginnen, sodass der Rest gültiges JSON ist.
func stripComments(b []byte) string {
	var sb strings.Builder
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") {
			continue
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}
