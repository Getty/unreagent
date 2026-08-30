// Package config loads and validates the launcher configuration (YAML).
//
// Model:
//   - unreagent.yaml        – committable, portable, NO machine paths
//   - unreagent.local.yaml  – git-ignored, machine overrides (overlaid on top)
//
// Machine-specific paths are resolved at runtime and substituted into the config
// through placeholders:
//
//	${ENGINE}        – root of the UE installation
//	${PROJECT}       – full path to the .uproject
//	${PROJECT_DIR}   – directory of the .uproject
//	${PROJECT_NAME}  – file name of the .uproject without the extension
//
// Engine resolution (priority): env UE_ROOT → engineRoot (usually local.yaml) →
// auto-detection of the standard Epic installation paths.
package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the complete launcher configuration.
type Config struct {
	// EngineRoot is normally only set in unreagent.local.yaml, in case
	// auto-detection/env cannot find the engine.
	EngineRoot  string                 `yaml:"engineRoot"`
	Unreal      UnrealConfig           `yaml:"unreal"`
	Agent       AgentConfig            `yaml:"agent"`
	Commands    map[string]CommandSpec `yaml:"commands"`
	MCP         MCPConfig              `yaml:"mcp"`
	Permissions Permissions            `yaml:"permissions"`
	Runtimes    Runtimes               `yaml:"runtimes"`
	Files       FilesConfig            `yaml:"files"`
}

// FilesConfig exposes file tools (read/write/list/edit) through the MCP server,
// so an agent (external/headless as well) can work on the UE project without
// anyone being present locally. All paths are restricted to root.
type FilesConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Root     string `yaml:"root"`     // default: ${PROJECT_DIR}
	ReadOnly bool   `yaml:"readOnly"` // true = read only, no writing/editing
}

// UnrealConfig describes the Unreal Editor process.
type UnrealConfig struct {
	Editor              string   `yaml:"editor"`  // default: ${ENGINE}/Engine/Binaries/Win64/UnrealEditor.exe
	Project             string   `yaml:"project"` // optional; default: auto-detection of the .uproject
	Args                []string `yaml:"args"`
	ManualStart         bool     `yaml:"manualStart"`
	Restart             string   `yaml:"restart"`
	MaxRestarts         int      `yaml:"maxRestarts"`
	RestartDelaySeconds int      `yaml:"restartDelaySeconds"`
	// Unattended appends -unattended (default true): it suppresses the crash
	// reporter dialog AND both recovery prompts on restart.
	Unattended *bool `yaml:"unattended"`
	// KillCrashReporter kills CrashReportClientEditor.exe before every (re)start
	// (default true) — a safeguard in case a reporter window is stuck after all.
	KillCrashReporter *bool `yaml:"killCrashReporter"`
	// CleanOnRestart removes Saved/Autosaves/PackageRestoreData.json and
	// Saved/Crashes/* before every start (default false) — guarantees a clean
	// start.
	CleanOnRestart bool `yaml:"cleanOnRestart"`
}

// AgentConfig describes the agent process (e.g. Claude Code).
type AgentConfig struct {
	Enabled             bool              `yaml:"enabled"`
	Command             string            `yaml:"command"`
	Args                []string          `yaml:"args"`
	Env                 map[string]string `yaml:"env"` // additional environment variables (e.g. HOME/USERPROFILE)
	Workdir             string            `yaml:"workdir"`
	StartDelaySeconds   int               `yaml:"startDelaySeconds"`
	Restart             string            `yaml:"restart"`
	MaxRestarts         int               `yaml:"maxRestarts"`
	RestartDelaySeconds int               `yaml:"restartDelaySeconds"`
	ClaudeIntegration   bool              `yaml:"claudeIntegration"`
	// PowershellTool sets CLAUDE_CODE_USE_POWERSHELL_TOOL=1 in the agent env on
	// Windows (with claudeIntegration). Default true; can be turned off with
	// powershellTool: false.
	PowershellTool *bool `yaml:"powershellTool"`
	// Window runs the agent interactively in the foreground: it inherits the
	// launcher's real console (TTY), the launcher logs go to unreagent.log.
	// Default true (except in headless -p mode); window: false turns it off.
	Window *bool `yaml:"window"`
	// OnExit controls what happens when the agent exits (e.g. /quit) and is NOT
	// restarted — in window mode the agent is the leading process, so the session
	// is over then:
	//   ask      (default) ask in window mode: shut everything down / keep the
	//            editor running (launcher console) / restart the agent.
	//            Timeout 30s -> shut everything down. Headless without a TTY =
	//            shutdown.
	//   shutdown always shut the whole stack down immediately (UE + MCP +
	//            launcher).
	//   leave    keep everything running (warning only) — manual Ctrl-C needed.
	OnExit string `yaml:"onExit"`
	// Hermes registers the unreagent MCP server with Hermes Agent (Nous
	// Research) as an alternative agent. It is NOT auto-merged on launcher
	// start — the user runs
	//   unreagent hermes-setup
	// once to merge the Hermes config (backup, token generation). Subsequent
	// runs are idempotent. See README "Hermes Agent".
	Hermes HermesConfig `yaml:"hermes"`
}

// HermesConfig controls the Hermes Agent (Nous Research) integration. We
// inject the unreagent MCP server into Hermes' YAML config so Hermes can use
// the same toolset as Claude Code.
type HermesConfig struct {
	// Enabled is a precondition for `unreagent hermes-setup` to do anything
	// (otherwise: "not configured, skipping" and exit 0). It does NOT enable
	// an auto-merge.
	Enabled bool `yaml:"enabled"`
	// ServerName is the name Hermes uses for the unreagent server (key under
	// "mcp_servers:"). Default: "unreagent".
	ServerName string `yaml:"serverName"`
	// ConfigPath is the Hermes config file. Default: $HERMES_HOME/config.yaml
	// (typically ~/.hermes/config.yaml). Empty = default.
	ConfigPath string `yaml:"configPath"`
}

// CommandSpec is a named one-off command (e.g. "compile").
type CommandSpec struct {
	Description string   `yaml:"description"`
	Command     string   `yaml:"command"`
	Args        []string `yaml:"args"`
	Dir         string   `yaml:"dir"`
}

// MCPConfig controls the built-in MCP server as well as additional MCP servers
// handed to the agent (e.g. an in-editor plugin like UE LLM Toolkit, through
// which Claude Code works INSIDE the engine).
type MCPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Address string `yaml:"address"`
	// Strict passes --strict-mcp-config to the agent: only the servers defined
	// here / by the launcher are used (the project's own .mcp.json is ignored).
	// Default false = additive.
	Strict bool `yaml:"strict"`
	// Token, if set, requires an "Authorization: Bearer <token>" header on
	// every MCP request. Empty = open (default — compatible with prior
	// behavior and local-only setups). Recommended as soon as the MCP server
	// listens on an address other than 127.0.0.1.
	Token string `yaml:"token"`
	// ExtraServers are MCP server definitions passed through raw, in the format of
	// Claude Code's .mcp.json (fields type/url/command/args/env/headers …).
	// Placeholders like ${PROJECT_DIR} are substituted in all string values.
	ExtraServers map[string]map[string]interface{} `yaml:"extraServers"`
	// WriteConfig additionally writes the assembled MCP config to disk as file(s),
	// so external clients (a separate Claude session, Cursor, VS Code) can use it.
	WriteConfig []MCPOutput `yaml:"writeConfig"`
}

// MCPOutput is a file target for the written MCP config.
type MCPOutput struct {
	Path   string `yaml:"path"`   // relative to the project or absolute
	Format string `yaml:"format"` // mcp_json (default) | vscode
}

// Permissions controls the permission prompt tool for the agent.
type Permissions struct {
	Enabled bool     `yaml:"enabled"`
	Mode    string   `yaml:"mode"`
	Allow   []string `yaml:"allow"`
	Deny    []string `yaml:"deny"`
}

// Runtimes provides the agent with clean execution environments.
type Runtimes struct {
	Python PythonRuntime `yaml:"python"`
	Node   NodeRuntime   `yaml:"node"`
}

// PythonRuntime uses uv (`uv run` builds/syncs the venv automatically).
type PythonRuntime struct {
	Enabled        bool   `yaml:"enabled"`
	UV             string `yaml:"uv"`
	Project        string `yaml:"project"`
	PrepareOnStart bool   `yaml:"prepareOnStart"`
}

// NodeRuntime runs Node code in the project context.
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

	OnExitAsk      = "ask"
	OnExitShutdown = "shutdown"
	OnExitLeave    = "leave"

	DefaultMCPAddress = "127.0.0.1:8765"
	MCPServerName     = "unreagent"
)

// Info describes the resolved paths (for logging/diagnostics).
type Info struct {
	ConfigPath  string
	LocalPath   string
	EngineRoot  string
	Project     string
	ProjectName string
}

// Load reads the configuration. explicitPath is optional; if it is empty,
// unreagent.yaml is looked for next to the executable. An unreagent.local.yaml
// sitting beside it is overlaid on top.
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

	// Write the resolved/auto-detected project path back, so the editor starts
	// with the project and project-related features can use it.
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
	if c.Agent.OnExit == "" {
		c.Agent.OnExit = OnExitAsk
	}

	if c.MCP.Address == "" {
		c.MCP.Address = DefaultMCPAddress
	}
	if c.Agent.Hermes.ServerName == "" {
		c.Agent.Hermes.ServerName = MCPServerName
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

	// Built-in default commands (only if not defined by the user).
	if c.Commands == nil {
		c.Commands = map[string]CommandSpec{}
	}
	if _, ok := c.Commands["compile"]; !ok {
		c.Commands["compile"] = CommandSpec{
			Description: "Compiles the project's C++ modules (UnrealBuildTool).",
			Command:     "${ENGINE}/Engine/Build/BatchFiles/Build.bat",
			Args:        []string{"${PROJECT_NAME}Editor", "Win64", "Development", "-Project=${PROJECT}", "-waitmutex", "-FromMsBuild"},
		}
	}
	if _, ok := c.Commands["package"]; !ok {
		c.Commands["package"] = CommandSpec{
			Description: "Builds a distributable Windows build (cook + stage + pak).",
			Command:     "${ENGINE}/Engine/Build/BatchFiles/RunUAT.bat",
			Args: []string{
				"BuildCookRun", "-project=${PROJECT}", "-noP4", "-platform=Win64",
				"-clientconfig=Development", "-build", "-cook", "-stage", "-pak",
				"-archive", "-archivedirectory=${PROJECT_DIR}/Packaged",
			},
		}
	}
}

// substitute replaces the placeholders in all path/argument fields.
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

// substituteAny replaces placeholders recursively in strings/maps/lists (for the
// extraServers definitions passed through raw).
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
		switch c.Agent.OnExit {
		case OnExitAsk, OnExitShutdown, OnExitLeave:
		default:
			return fmt.Errorf("agent.onExit invalid: %q (allowed: ask, shutdown, leave)", c.Agent.OnExit)
		}
	}
	switch c.Permissions.Mode {
	case ModeAllowAll, ModeAllowlist, ModeDenyAll:
	default:
		return fmt.Errorf("permissions.mode invalid: %q (allowed: allow_all, allowlist, deny_all)", c.Permissions.Mode)
	}
	if c.Permissions.Enabled && !c.MCP.Enabled {
		return fmt.Errorf("permissions.enabled requires mcp.enabled (the approve tool is served by the MCP server)")
	}
	for name, cmd := range c.Commands {
		if cmd.Command == "" {
			return fmt.Errorf("commands.%s.command is missing", name)
		}
	}
	return nil
}

func validRestart(p string) error {
	switch p {
	case RestartNever, RestartOnFailure, RestartAlways:
		return nil
	default:
		return fmt.Errorf("invalid policy %q (allowed: never, on-failure, always)", p)
	}
}

// --- resolution ---

// resolveEngine determines the UE installation root.
func resolveEngine(c *Config) string {
	if v := strings.TrimSpace(os.Getenv("UE_ROOT")); v != "" {
		return filepath.ToSlash(v)
	}
	if c.EngineRoot != "" {
		return filepath.ToSlash(c.EngineRoot)
	}
	return autodetectEngine()
}

// autodetectEngine looks for the UE installation: first in the Windows registry
// (Epic Launcher installs), then in the standard installation paths. It takes
// the highest version that has an UnrealEditor executable.
func autodetectEngine() string {
	if dir := registryEngine(); dir != "" {
		return dir
	}
	patterns := []string{
		`C:/Program Files/Epic Games/UE_*`,
		`D:/Program Files/Epic Games/UE_*`,
		`E:/Program Files/Epic Games/UE_*`,
		`C:/Epic Games/UE_*`,
		`D:/Epic Games/UE_*`,
	}
	var candidates []string
	for _, p := range patterns {
		// filepath.Glob uses the OS separator (\ on Windows) — forward-slash
		// patterns would not match there, hence FromSlash.
		matches, _ := filepath.Glob(filepath.FromSlash(p))
		candidates = append(candidates, matches...)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(candidates))) // highest version first
	for _, dir := range candidates {
		if fileExists(filepath.Join(dir, "Engine", "Binaries", "Win64", "UnrealEditor.exe")) {
			return filepath.ToSlash(dir)
		}
	}
	return ""
}

// registryEngine reads the UE installation path from the Windows registry
// (HKLM\SOFTWARE\EpicGames\Unreal Engine\<ver>\InstalledDirectory).
func registryEngine() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, ver := range []string{"5.7", "5.6", "5.5", "5.4"} {
		out, err := exec.Command("reg", "query",
			`HKLM\SOFTWARE\EpicGames\Unreal Engine\`+ver, "/v", "InstalledDirectory").Output()
		if err != nil {
			continue
		}
		dir := parseRegSZ(string(out))
		if dir != "" && fileExists(filepath.Join(dir, "Engine", "Binaries", "Win64", "UnrealEditor.exe")) {
			return filepath.ToSlash(dir)
		}
	}
	return ""
}

// parseRegSZ extracts the value behind "REG_SZ" from a `reg query` output.
func parseRegSZ(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "REG_SZ"); i >= 0 {
			return strings.TrimSpace(line[i+len("REG_SZ"):])
		}
	}
	return ""
}

// resolveProject determines the .uproject (set explicitly or auto-detected in
// the config directory). It returns the full path, the directory and the name
// without the extension.
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

// --- file helpers ---

func decodeYAML(path string, c *Config) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config not readable (%s): %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true) // unknown fields = error (typo protection)
	if err := dec.Decode(c); err != nil {
		if err == io.EOF {
			return nil // an empty file is fine
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
