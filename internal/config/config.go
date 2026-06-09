// Package config lädt und validiert die Launcher-Konfiguration (YAML).
//
// Modell:
//   - unreagent.yaml        – committbar, portabel, KEINE Maschinenpfade
//   - unreagent.local.yaml  – git-ignoriert, Maschinen-Overrides (drübergelegt)
//
// Maschinenspezifische Pfade werden zur Laufzeit aufgelöst und über Platzhalter
// in die Config eingesetzt:
//
//	${ENGINE}        – Wurzel der UE-Installation
//	${PROJECT}       – voller Pfad zur .uproject
//	${PROJECT_DIR}   – Verzeichnis der .uproject
//	${PROJECT_NAME}  – Dateiname der .uproject ohne Endung
//
// Engine-Auflösung (Priorität): Env UE_ROOT → engineRoot (meist local.yaml) →
// Auto-Detect der Standard-Epic-Installationspfade.
package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config ist die komplette Launcher-Konfiguration.
type Config struct {
	// EngineRoot wird normalerweise nur in unreagent.local.yaml gesetzt, falls
	// Auto-Detect/Env die Engine nicht finden.
	EngineRoot  string                 `yaml:"engineRoot"`
	Unreal      UnrealConfig           `yaml:"unreal"`
	Agent       AgentConfig            `yaml:"agent"`
	Commands    map[string]CommandSpec `yaml:"commands"`
	MCP         MCPConfig              `yaml:"mcp"`
	Permissions Permissions            `yaml:"permissions"`
	Runtimes    Runtimes               `yaml:"runtimes"`
	Files       FilesConfig            `yaml:"files"`
}

// FilesConfig exponiert Datei-Tools (read/write/list/edit) über den MCP-Server,
// damit ein (auch externer/headless) Agent am UE-Projekt arbeiten kann, ohne
// dass jemand lokal anwesend ist. Alle Pfade sind auf Root beschränkt.
type FilesConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Root     string `yaml:"root"`     // default: ${PROJECT_DIR}
	ReadOnly bool   `yaml:"readOnly"` // true = nur lesen, kein Schreiben/Editieren
}

// UnrealConfig beschreibt den Unreal-Editor-Prozess.
type UnrealConfig struct {
	Editor              string   `yaml:"editor"`  // default: ${ENGINE}/Engine/Binaries/Win64/UnrealEditor.exe
	Project             string   `yaml:"project"` // optional; default: Auto-Detect der .uproject
	Args                []string `yaml:"args"`
	ManualStart         bool     `yaml:"manualStart"`
	Restart             string   `yaml:"restart"`
	MaxRestarts         int      `yaml:"maxRestarts"`
	RestartDelaySeconds int      `yaml:"restartDelaySeconds"`
	// Unattended hängt -unattended an (default true): unterdrückt den
	// Crash-Reporter-Dialog UND beide Recovery-Prompts beim Neustart.
	Unattended *bool `yaml:"unattended"`
	// KillCrashReporter killt CrashReportClientEditor.exe vor jedem (Neu-)Start
	// (default true) — Absicherung, falls doch ein Reporter-Fenster hängt.
	KillCrashReporter *bool `yaml:"killCrashReporter"`
	// CleanOnRestart räumt vor jedem Start Saved/Autosaves/PackageRestoreData.json
	// und Saved/Crashes/* weg (default false) — garantiert sauberer Start.
	CleanOnRestart bool `yaml:"cleanOnRestart"`
}

// AgentConfig beschreibt den Agenten-Prozess (z.B. Claude Code).
type AgentConfig struct {
	Enabled             bool     `yaml:"enabled"`
	Command             string   `yaml:"command"`
	Args                []string `yaml:"args"`
	Workdir             string   `yaml:"workdir"`
	StartDelaySeconds   int      `yaml:"startDelaySeconds"`
	Restart             string   `yaml:"restart"`
	MaxRestarts         int      `yaml:"maxRestarts"`
	RestartDelaySeconds int      `yaml:"restartDelaySeconds"`
	ClaudeIntegration   bool     `yaml:"claudeIntegration"`
}

// CommandSpec ist ein benannter Einmal-Befehl (z.B. "compile").
type CommandSpec struct {
	Description string   `yaml:"description"`
	Command     string   `yaml:"command"`
	Args        []string `yaml:"args"`
	Dir         string   `yaml:"dir"`
}

// MCPConfig steuert den eingebauten MCP-Server sowie zusätzliche MCP-Server, die
// dem Agenten mitgegeben werden (z.B. ein In-Editor-Plugin wie UE LLM Toolkit,
// über das Claude Code IN der Engine arbeitet).
type MCPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Address string `yaml:"address"`
	// Strict gibt --strict-mcp-config an den Agenten weiter: nur die hier/vom
	// Launcher definierten Server werden genutzt (projekt-eigene .mcp.json wird
	// ignoriert). Default false = additiv.
	Strict bool `yaml:"strict"`
	// ExtraServers sind roh durchgereichte MCP-Server-Definitionen im Format der
	// Claude-Code-.mcp.json (Felder type/url/command/args/env/headers …).
	// Platzhalter wie ${PROJECT_DIR} werden in allen String-Werten ersetzt.
	ExtraServers map[string]map[string]interface{} `yaml:"extraServers"`
	// WriteConfig schreibt die zusammengebaute MCP-Config zusätzlich als Datei(en)
	// auf die Platte, damit externe Clients (eigene Claude-Sitzung, Cursor, VS
	// Code) sie nutzen können.
	WriteConfig []MCPOutput `yaml:"writeConfig"`
}

// MCPOutput ist ein Datei-Ziel für die geschriebene MCP-Config.
type MCPOutput struct {
	Path   string `yaml:"path"`   // relativ zum Projekt oder absolut
	Format string `yaml:"format"` // mcp_json (default) | vscode
}

// Permissions steuert das Permission-Prompt-Tool für den Agenten.
type Permissions struct {
	Enabled bool     `yaml:"enabled"`
	Mode    string   `yaml:"mode"`
	Allow   []string `yaml:"allow"`
	Deny    []string `yaml:"deny"`
}

// Runtimes stellt dem Agenten saubere Ausführungsumgebungen bereit.
type Runtimes struct {
	Python PythonRuntime `yaml:"python"`
	Node   NodeRuntime   `yaml:"node"`
}

// PythonRuntime nutzt uv (`uv run` baut/synct das venv automatisch).
type PythonRuntime struct {
	Enabled        bool   `yaml:"enabled"`
	UV             string `yaml:"uv"`
	Project        string `yaml:"project"`
	PrepareOnStart bool   `yaml:"prepareOnStart"`
}

// NodeRuntime führt Node-Code im Projektkontext aus.
type NodeRuntime struct {
	Enabled        bool   `yaml:"enabled"`
	Node           string `yaml:"node"`
	Npm            string `yaml:"npm"`
	Project        string `yaml:"project"`
	PrepareOnStart bool   `yaml:"prepareOnStart"`
}

const (
	RestartNever     = "never"
	RestartOnFailure = "on-failure"
	RestartAlways    = "always"

	ModeAllowAll  = "allow_all"
	ModeAllowlist = "allowlist"
	ModeDenyAll   = "deny_all"

	DefaultMCPAddress = "127.0.0.1:8765"
	MCPServerName     = "unreagent"
)

// Info beschreibt die aufgelösten Pfade (für Logging/Diagnose).
type Info struct {
	ConfigPath  string
	LocalPath   string
	EngineRoot  string
	Project     string
	ProjectName string
}

// Load liest die Konfiguration. explicitPath ist optional; ist er leer, wird
// neben der ausführbaren Datei nach unreagent.yaml gesucht. Eine danebenliegende
// unreagent.local.yaml wird als Overlay drübergelegt.
func Load(explicitPath string) (*Config, Info, error) {
	var info Info

	path := explicitPath
	if path == "" {
		path = findConfig()
	}
	info.ConfigPath = path
	baseDir := filepath.Dir(path)

	var c Config
	if err := decodeYAML(path, &c); err != nil {
		return nil, info, err
	}
	if local := localPathFor(path); fileExists(local) {
		info.LocalPath = local
		if err := decodeYAML(local, &c); err != nil {
			return nil, info, err
		}
	}

	c.applyDefaults()

	engine := resolveEngine(&c)
	project, projectDir, projectName := resolveProject(&c, baseDir)
	info.EngineRoot = engine
	info.Project = project
	info.ProjectName = projectName

	// Aufgelösten/auto-erkannten Projektpfad zurückschreiben, damit der Editor
	// mit dem Projekt startet und projektbezogene Features ihn nutzen können.
	c.Unreal.Project = project

	c.substitute(engine, project, projectDir, projectName)

	if err := c.validate(); err != nil {
		return nil, info, err
	}
	return &c, info, nil
}

func (c *Config) applyDefaults() {
	if c.Unreal.Editor == "" {
		c.Unreal.Editor = "${ENGINE}/Engine/Binaries/Win64/UnrealEditor.exe"
	}
	if c.Unreal.Args == nil {
		c.Unreal.Args = []string{"-stdout", "-FullStdOutLogOutput"}
	}
	if c.Unreal.Restart == "" {
		c.Unreal.Restart = RestartOnFailure
	}
	if c.Unreal.RestartDelaySeconds == 0 {
		c.Unreal.RestartDelaySeconds = 3
	}
	if c.Unreal.Unattended == nil {
		t := true
		c.Unreal.Unattended = &t
	}
	if c.Unreal.KillCrashReporter == nil {
		t := true
		c.Unreal.KillCrashReporter = &t
	}

	if c.Agent.Command == "" {
		c.Agent.Command = "claude"
	}
	if c.Agent.Restart == "" {
		c.Agent.Restart = RestartOnFailure
	}
	if c.Agent.RestartDelaySeconds == 0 {
		c.Agent.RestartDelaySeconds = 3
	}
	if c.Agent.StartDelaySeconds == 0 {
		c.Agent.StartDelaySeconds = 5
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
	if c.Files.Root == "" {
		c.Files.Root = "${PROJECT_DIR}"
	}

	// Eingebaute Default-Befehle (nur, wenn nicht selbst definiert).
	if c.Commands == nil {
		c.Commands = map[string]CommandSpec{}
	}
	if _, ok := c.Commands["compile"]; !ok {
		c.Commands["compile"] = CommandSpec{
			Description: "Kompiliert die C++-Module des Projekts (UnrealBuildTool).",
			Command:     "${ENGINE}/Engine/Build/BatchFiles/Build.bat",
			Args:        []string{"${PROJECT_NAME}Editor", "Win64", "Development", "-Project=${PROJECT}", "-waitmutex", "-FromMsBuild"},
		}
	}
	if _, ok := c.Commands["package"]; !ok {
		c.Commands["package"] = CommandSpec{
			Description: "Erstellt einen verteilbaren Windows-Build (cook + stage + pak).",
			Command:     "${ENGINE}/Engine/Build/BatchFiles/RunUAT.bat",
			Args: []string{
				"BuildCookRun", "-project=${PROJECT}", "-noP4", "-platform=Win64",
				"-clientconfig=Development", "-build", "-cook", "-stage", "-pak",
				"-archive", "-archivedirectory=${PROJECT_DIR}/Packaged",
			},
		}
	}
}

// substitute ersetzt die Platzhalter in allen Pfad-/Argument-Feldern.
func (c *Config) substitute(engine, project, projectDir, projectName string) {
	rep := strings.NewReplacer(
		"${ENGINE}", engine,
		"${PROJECT_DIR}", projectDir,
		"${PROJECT_NAME}", projectName,
		"${PROJECT}", project,
	)
	c.Unreal.Editor = rep.Replace(c.Unreal.Editor)
	c.Unreal.Project = rep.Replace(c.Unreal.Project)
	c.Unreal.Args = replaceAll(rep, c.Unreal.Args)
	c.Agent.Workdir = rep.Replace(c.Agent.Workdir)
	c.Runtimes.Python.Project = rep.Replace(c.Runtimes.Python.Project)
	c.Runtimes.Node.Project = rep.Replace(c.Runtimes.Node.Project)
	c.Files.Root = rep.Replace(c.Files.Root)
	for name, cmd := range c.Commands {
		cmd.Command = rep.Replace(cmd.Command)
		cmd.Dir = rep.Replace(cmd.Dir)
		cmd.Args = replaceAll(rep, cmd.Args)
		c.Commands[name] = cmd
	}
	for name, def := range c.MCP.ExtraServers {
		if m, ok := substituteAny(rep, def).(map[string]interface{}); ok {
			c.MCP.ExtraServers[name] = m
		}
	}
	for i := range c.MCP.WriteConfig {
		c.MCP.WriteConfig[i].Path = rep.Replace(c.MCP.WriteConfig[i].Path)
	}
}

// substituteAny ersetzt Platzhalter rekursiv in Strings/Maps/Listen (für die
// roh durchgereichten extraServers-Definitionen).
func substituteAny(rep *strings.Replacer, v interface{}) interface{} {
	switch t := v.(type) {
	case string:
		return rep.Replace(t)
	case map[string]interface{}:
		for k, val := range t {
			t[k] = substituteAny(rep, val)
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = substituteAny(rep, val)
		}
		return t
	default:
		return v
	}
}

func (c *Config) validate() error {
	if err := validRestart(c.Unreal.Restart); err != nil {
		return fmt.Errorf("unreal.restart: %w", err)
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
	if c.Permissions.Enabled && !c.MCP.Enabled {
		return fmt.Errorf("permissions.enabled erfordert mcp.enabled (das approve-Tool läuft über den MCP-Server)")
	}
	for name, cmd := range c.Commands {
		if cmd.Command == "" {
			return fmt.Errorf("commands.%s.command fehlt", name)
		}
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

// --- Auflösung ---

// resolveEngine ermittelt die UE-Installationswurzel.
func resolveEngine(c *Config) string {
	if v := strings.TrimSpace(os.Getenv("UE_ROOT")); v != "" {
		return filepath.ToSlash(v)
	}
	if c.EngineRoot != "" {
		return filepath.ToSlash(c.EngineRoot)
	}
	return autodetectEngine()
}

// autodetectEngine durchsucht Standard-Epic-Installationspfade und nimmt die
// höchste gefundene Version mit einer UnrealEditor-Executable.
func autodetectEngine() string {
	patterns := []string{
		`C:/Program Files/Epic Games/UE_*`,
		`D:/Program Files/Epic Games/UE_*`,
		`C:/Epic Games/UE_*`,
	}
	var candidates []string
	for _, p := range patterns {
		matches, _ := filepath.Glob(p)
		candidates = append(candidates, matches...)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(candidates))) // höchste Version zuerst
	for _, dir := range candidates {
		dir = filepath.ToSlash(dir)
		if fileExists(dir + "/Engine/Binaries/Win64/UnrealEditor.exe") {
			return dir
		}
	}
	return ""
}

// resolveProject ermittelt die .uproject (explizit gesetzt oder per Auto-Detect
// im Konfig-Verzeichnis). Liefert vollen Pfad, Verzeichnis und Name-ohne-Endung.
func resolveProject(c *Config, baseDir string) (project, dir, name string) {
	p := strings.TrimSpace(c.Unreal.Project)
	if p != "" && !strings.Contains(p, "${") {
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
	} else {
		matches, _ := filepath.Glob(filepath.Join(baseDir, "*.uproject"))
		if len(matches) > 0 {
			sort.Strings(matches)
			p = matches[0]
		} else {
			return "", "", ""
		}
	}
	p = filepath.ToSlash(p)
	dir = filepath.ToSlash(filepath.Dir(p))
	name = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	return p, dir, name
}

// --- Datei-Helfer ---

func decodeYAML(path string, c *Config) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Config nicht lesbar (%s): %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true) // unbekannte Felder = Fehler (Tippfehler-Schutz)
	if err := dec.Decode(c); err != nil {
		if err == io.EOF {
			return nil // leere Datei ist ok
		}
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

func findConfig() string {
	dir := exeDir()
	for _, name := range []string{"unreagent.yaml", "unreagent.yml"} {
		p := filepath.Join(dir, name)
		if fileExists(p) {
			return p
		}
	}
	return filepath.Join(dir, "unreagent.yaml")
}

func localPathFor(path string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + ".local" + ext
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func replaceAll(rep *strings.Replacer, in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = rep.Replace(s)
	}
	return out
}
