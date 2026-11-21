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
// the engine named by the .uproject's EngineAssociation → the highest version
// installed (Epic Launcher registry entries and standard installation paths,
// compared numerically). See engine.go.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	Skills      SkillsConfig           `yaml:"skills"`
}

// SkillsConfig delivers the skill documents embedded in the launcher to the
// agent: through the skills MCP tool, as files in Dir, or both. Enabled and MCP
// default to true, so they are *bool; ViaMCP and ViaFiles read them with
// Enabled applied.
type SkillsConfig struct {
	Enabled     *bool  `yaml:"enabled"`     // default true; false turns both paths off
	MCP         *bool  `yaml:"mcp"`         // default true: serve them through the skills MCP tool
	Materialize bool   `yaml:"materialize"` // write them into Dir on every start
	Dir         string `yaml:"dir"`         // default: ${PROJECT_DIR}/.claude/skills
}

// ViaMCP reports whether the skills MCP tool is to be served.
func (s SkillsConfig) ViaMCP() bool { return boolVal(s.Enabled) && boolVal(s.MCP) }

// ViaFiles reports whether the skills are to be written into Dir.
func (s SkillsConfig) ViaFiles() bool { return boolVal(s.Enabled) && s.Materialize }

func boolVal(p *bool) bool { return p != nil && *p }

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
	Project             string   `yaml:"project"` // optional; default: auto-detection of the .uproject; no placeholders, must exist
	Args                []string `yaml:"args"`    // unset or null: -stdout -FullStdOutLogOutput; [] clears
	ManualStart         bool     `yaml:"manualStart"`
	Restart             string   `yaml:"restart"`
	MaxRestarts         int      `yaml:"maxRestarts"`
	RestartDelaySeconds int      `yaml:"restartDelaySeconds"` // 0 = default 3; at least 1
	// Unattended appends -unattended (default true): it suppresses the crash
	// reporter dialog AND both recovery prompts on restart.
	Unattended *bool `yaml:"unattended"`
	// KillCrashReporter kills CrashReportClientEditor.exe before every (re)start
	// except one that follows a crash, whose reporter may still be writing
	// (default true) — a safeguard in case a reporter window is stuck after all.
	KillCrashReporter *bool `yaml:"killCrashReporter"`
	// CleanOnRestart removes Saved/Autosaves/PackageRestoreData.json before
	// every start, and Saved/Crashes/* before every start except one that
	// follows a crash — an automatic restart, and the first start after the
	// editor failed and was not restarted — which keeps the crash dumps for
	// diagnosis (default false).
	CleanOnRestart bool `yaml:"cleanOnRestart"`
}

// AgentConfig describes the agent process (e.g. Claude Code).
type AgentConfig struct {
	Enabled             bool              `yaml:"enabled"`
	Command             string            `yaml:"command"`
	Args                []string          `yaml:"args"`
	Env                 map[string]string `yaml:"env"` // additional environment variables (e.g. HOME/USERPROFILE)
	Workdir             string            `yaml:"workdir"`
	StartDelaySeconds   int               `yaml:"startDelaySeconds"` // 0 = default 5; at least 1
	Restart             string            `yaml:"restart"`
	MaxRestarts         int               `yaml:"maxRestarts"`
	RestartDelaySeconds int               `yaml:"restartDelaySeconds"` // 0 = default 3; at least 1
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
	// "mcp_servers:"). Default: "unreagent". With Enabled it must be a plain
	// YAML key (see validHermesServerName).
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
	// SkippedCommands are the built-in commands (compile, package) left out
	// because a placeholder they need resolved to nothing — sorted.
	SkippedCommands []string
	// Warnings are what Load noticed without failing, each a complete message
	// for the launcher to log as a WARN line: today only an EngineAssociation
	// that names no usable engine (see chooseEngine).
	Warnings []string
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
	// Absolute, because every project path derives from the config directory
	// and lands in placeholders: a relative "-config unreagent.yaml" made
	// ${PROJECT_DIR} ".", and "Write(${PROJECT_DIR}/*)" then matched anything.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, info, fmt.Errorf("config path %s: %w", path, err)
	}
	path = abs
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

	builtins := c.applyDefaults()

	// Project first: engine auto-detection reads its EngineAssociation.
	project, projectDir, projectName, err := resolveProject(&c, baseDir)
	if err != nil {
		return nil, info, err
	}
	engine, warning, err := resolveEngine(&c, project)
	if err != nil {
		return nil, info, err
	}
	if warning != "" {
		info.Warnings = append(info.Warnings, warning)
	}
	info.EngineRoot = engine
	info.Project = project
	info.ProjectName = projectName

	// Write the resolved/auto-detected project path back, so the editor starts
	// with the project and project-related features can use it.
	c.Unreal.Project = project

	skipped, err := c.substitute(engine, project, projectDir, projectName, builtins)
	info.SkippedCommands = skipped
	if err != nil {
		return nil, info, err
	}

	if err := c.validate(); err != nil {
		return nil, info, err
	}
	return &c, info, nil
}

// applyDefaults fills unset values and returns the names of the built-in
// commands it added (the user defined none of that name).
//
// Zero is "not set" for the plain ints: a delay of 0 gets the default, so the
// smallest delay that can be configured is 1 second — for the restart delay
// that floor is what keeps a crashing process from restarting in a tight loop.
// A nil list is "not set" too, and null in unreagent.local.yaml decodes to nil:
// "unreal.args: ~" restores the default arguments, "unreal.args: []" clears
// them.
func (c *Config) applyDefaults() (builtins map[string]bool) {
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
	if c.Skills.Enabled == nil {
		t := true
		c.Skills.Enabled = &t
	}
	if c.Skills.MCP == nil {
		t := true
		c.Skills.MCP = &t
	}
	if c.Skills.Dir == "" {
		c.Skills.Dir = "${PROJECT_DIR}/.claude/skills"
	}

	// Built-in default commands (only if not defined by the user).
	if c.Commands == nil {
		c.Commands = map[string]CommandSpec{}
	}
	builtins = map[string]bool{}
	if _, ok := c.Commands["compile"]; !ok {
		builtins["compile"] = true
		c.Commands["compile"] = CommandSpec{
			Description: "Compiles the project's C++ modules (UnrealBuildTool).",
			Command:     "${ENGINE}/Engine/Build/BatchFiles/Build.bat",
			Args:        []string{"${PROJECT_NAME}Editor", "Win64", "Development", "-Project=${PROJECT}", "-waitmutex", "-FromMsBuild"},
		}
	}
	if _, ok := c.Commands["package"]; !ok {
		builtins["package"] = true
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
	return builtins
}

// substitute replaces the placeholders in every field that holds a path, a
// command, an argument, an environment value or a permission pattern:
// unreal.editor/args, agent.command/args/workdir, the agent.env values,
// agent.hermes.configPath, permissions.allow/deny, the runtimes' executables
// and project directories, files.root, skills.dir, commands (command, args,
// dir), every string in mcp.extraServers and the mcp.writeConfig paths. Everything
// else stays literal on purpose: names and keys (env names, hermes.serverName),
// enums, mcp.address and mcp.token (no placeholder forms part of either, and a
// token must never change under the user's hands), descriptions (prose), and
// engineRoot and unreal.project, from which the placeholders are derived.
//
// A placeholder that resolved to "" is an error in every field the launcher
// will use: the result would be a garbage path such as
// /Engine/Binaries/Win64/UnrealEditor.exe or, in an allow rule, "Edit(/*)".
// A field is used when its section is enabled; unreal.*, agent.workdir (it
// also roots the runtimes and the written MCP configs) and the user's commands
// always are, runtimes.node.npm also for a node-started extra server, and
// skills.dir while skills are materialized (ViaFiles). An unused field keeps
// whatever the replacement produces, with three exceptions: a built-in command
// (see builtins) that needs the placeholder is removed and its name returned
// in skipped, and files.root and skills.dir are cleared, so that -files and
// -no-skills-mcp, which turn their feature on after loading, find no directory
// rather than a wrong one.
func (c *Config) substitute(engine, project, projectDir, projectName string, builtins map[string]bool) (skipped []string, err error) {
	values := [][2]string{
		{"${ENGINE}", engine},
		{"${PROJECT_DIR}", projectDir},
		{"${PROJECT_NAME}", projectName},
		{"${PROJECT}", project},
	}
	var pairs, empty []string
	for _, v := range values {
		pairs = append(pairs, v[0], v[1])
		if v[1] == "" {
			empty = append(empty, v[0])
		}
	}
	rep := strings.NewReplacer(pairs...)
	unresolved := func(s string) []string {
		var out []string
		for _, p := range empty {
			if strings.Contains(s, p) {
				out = append(out, p)
			}
		}
		return out
	}
	usedBy := map[string]map[string]bool{} // placeholder -> fields
	sub := func(field string, used bool, s string) string {
		if used {
			for _, p := range unresolved(s) {
				if usedBy[p] == nil {
					usedBy[p] = map[string]bool{}
				}
				usedBy[p][field] = true
			}
		}
		return rep.Replace(s)
	}
	subList := func(field string, used bool, in []string) []string {
		out := make([]string, len(in))
		for i, s := range in {
			out[i] = sub(fmt.Sprintf("%s[%d]", field, i), used, s)
		}
		return out
	}

	c.Unreal.Editor = sub("unreal.editor", true, c.Unreal.Editor)
	c.Unreal.Args = subList("unreal.args", true, c.Unreal.Args)

	agent := c.Agent.Enabled
	c.Agent.Command = sub("agent.command", agent, c.Agent.Command)
	c.Agent.Args = subList("agent.args", agent, c.Agent.Args)
	for k, v := range c.Agent.Env {
		c.Agent.Env[k] = sub("agent.env."+k, agent, v)
	}
	c.Agent.Workdir = sub("agent.workdir", true, c.Agent.Workdir)
	c.Agent.Hermes.ConfigPath = sub("agent.hermes.configPath", c.Agent.Hermes.Enabled, c.Agent.Hermes.ConfigPath)

	// Safe in patterns only because every value is an absolute path: the
	// config path is made absolute in Load (the project values derive from
	// it) and resolveEngine makes the engine root absolute. Then the only glob
	// character is a trailing *, and the rule's own "(" comes before any
	// substituted path. A relative value would break this: matchPath cleans
	// "Write(./*)" to "*", which grants everything, and "Write(sub/*)" never
	// matches the absolute paths Claude Code sends, which leaves a deny rule
	// inert. An empty one is caught by the unresolved check ("Edit(/*)").
	c.Permissions.Allow = subList("permissions.allow", c.Permissions.Enabled, c.Permissions.Allow)
	c.Permissions.Deny = subList("permissions.deny", c.Permissions.Enabled, c.Permissions.Deny)

	py, node := c.Runtimes.Python.Enabled, c.Runtimes.Node.Enabled
	c.Runtimes.Python.UV = sub("runtimes.python.uv", py, c.Runtimes.Python.UV)
	c.Runtimes.Python.Project = sub("runtimes.python.project", py, c.Runtimes.Python.Project)
	c.Runtimes.Node.Node = sub("runtimes.node.node", node, c.Runtimes.Node.Node)
	// npm also installs the dependencies of node-started extra servers
	// (prepareNodeBridge in cmd/launcher/bridge.go).
	npmUsed := node || (c.MCP.Enabled && c.hasNodeExtraServer())
	c.Runtimes.Node.Npm = sub("runtimes.node.npm", npmUsed, c.Runtimes.Node.Npm)
	c.Runtimes.Node.Project = sub("runtimes.node.project", node, c.Runtimes.Node.Project)

	if !c.Files.Enabled && len(unresolved(c.Files.Root)) > 0 {
		c.Files.Root = ""
	} else {
		c.Files.Root = sub("files.root", c.Files.Enabled, c.Files.Root)
	}
	if skillFiles := c.Skills.ViaFiles(); !skillFiles && len(unresolved(c.Skills.Dir)) > 0 {
		c.Skills.Dir = ""
	} else {
		c.Skills.Dir = sub("skills.dir", skillFiles, c.Skills.Dir)
	}

	for name, cmd := range c.Commands {
		if builtins[name] && len(unresolved(strings.Join(append([]string{cmd.Command, cmd.Dir}, cmd.Args...), "\n"))) > 0 {
			delete(c.Commands, name)
			skipped = append(skipped, name)
			continue
		}
		cmd.Command = sub("commands."+name+".command", true, cmd.Command)
		cmd.Dir = sub("commands."+name+".dir", true, cmd.Dir)
		cmd.Args = subList("commands."+name+".args", true, cmd.Args)
		c.Commands[name] = cmd
	}
	sort.Strings(skipped)

	for name, def := range c.MCP.ExtraServers {
		field := "mcp.extraServers." + name
		replace := func(s string) string { return sub(field, c.MCP.Enabled, s) }
		if m, ok := substituteAny(replace, def).(map[string]interface{}); ok {
			c.MCP.ExtraServers[name] = m
		}
	}
	for i := range c.MCP.WriteConfig {
		c.MCP.WriteConfig[i].Path = sub(fmt.Sprintf("mcp.writeConfig[%d].path", i), c.MCP.Enabled, c.MCP.WriteConfig[i].Path)
	}

	var problems []string
	for _, p := range empty {
		if len(usedBy[p]) == 0 {
			continue
		}
		fields := make([]string, 0, len(usedBy[p]))
		for f := range usedBy[p] {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		problems = append(problems, fmt.Sprintf("%s is unresolved (%s) but used by %s", p, placeholderHint(p), strings.Join(fields, ", ")))
	}
	if len(problems) > 0 {
		return skipped, errors.New(strings.Join(problems, "; "))
	}
	return skipped, nil
}

// hasNodeExtraServer reports whether an extra server is started with node, by
// the same test as prepareMCPBridges in cmd/launcher/bridge.go — change both
// together.
func (c *Config) hasNodeExtraServer() bool {
	for _, def := range c.MCP.ExtraServers {
		command, _ := def["command"].(string)
		if strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe") == "node" {
			return true
		}
	}
	return false
}

// placeholderHint says why a placeholder resolved to nothing and what to set.
func placeholderHint(p string) string {
	if p == "${ENGINE}" {
		return "engine not found — set UE_ROOT or engineRoot in unreagent.local.yaml"
	}
	return "no .uproject found next to the config — set unreal.project"
}

// substituteAny replaces placeholders recursively in strings/maps/lists (for the
// extraServers definitions passed through raw).
func substituteAny(replace func(string) string, v interface{}) interface{} {
	switch t := v.(type) {
	case string:
		return replace(t)
	case map[string]interface{}:
		for k, val := range t {
			t[k] = substituteAny(replace, val)
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = substituteAny(replace, val)
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
	if err := validDelay("unreal.restartDelaySeconds", c.Unreal.RestartDelaySeconds); err != nil {
		return err
	}
	if c.Agent.Enabled {
		if err := validRestart(c.Agent.Restart); err != nil {
			return fmt.Errorf("agent.restart: %w", err)
		}
		if err := validDelay("agent.startDelaySeconds", c.Agent.StartDelaySeconds); err != nil {
			return err
		}
		if err := validDelay("agent.restartDelaySeconds", c.Agent.RestartDelaySeconds); err != nil {
			return err
		}
		switch c.Agent.OnExit {
		case OnExitAsk, OnExitShutdown, OnExitLeave:
		default:
			return fmt.Errorf("agent.onExit invalid: %q (allowed: ask, shutdown, leave)", c.Agent.OnExit)
		}
	}
	if c.Agent.Hermes.Enabled {
		if err := validHermesServerName(c.Agent.Hermes.ServerName); err != nil {
			return err
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
	for i, out := range c.MCP.WriteConfig {
		switch out.Format {
		case "", "mcp_json", "vscode":
		default:
			return fmt.Errorf("mcp.writeConfig[%d].format invalid: %q (allowed: mcp_json, vscode)", i, out.Format)
		}
	}
	if boolVal(c.Skills.Enabled) && !c.Skills.ViaMCP() && !c.Skills.ViaFiles() {
		return fmt.Errorf("skills.mcp and skills.materialize are both false, so no skill would reach the agent — set skills.mcp: true (the skills MCP tool), skills.materialize: true (files in skills.dir), or skills.enabled: false to turn skills off")
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

// hermesServerNameChars is what agent.hermes.serverName may consist of: ASCII
// letters, digits, "_" and "-", starting with a letter. None of these means
// anything to YAML, so no such name needs quoting as a key, and they are the
// characters model APIs commonly allow in tool names. The leading letter rules
// out every YAML number, date and timestamp, which start with a digit, a sign
// or a dot — among them some yaml.v3 writes unquoted and PyYAML misreads or
// cannot load (an integer past 64 bits, "0x_", "2001-02-30").
var hermesServerNameChars = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// validHermesServerName refuses a serverName that is not a plain YAML key:
// hermes-setup writes it as a key under mcp_servers in the Hermes config, and
// "<<" there became a merge key. A name of the allowed characters can still
// read as something other than a string: true, null, and the YAML 1.1
// booleans yes, on, n ..., which hermes-setup's node encoder writes unquoted
// and a YAML 1.1 reader takes as booleans. yaml.v3 quotes all of them when it
// marshals the bare string, so a name it would quote is refused.
func validHermesServerName(name string) error {
	if !hermesServerNameChars.MatchString(name) {
		return fmt.Errorf(`agent.hermes.serverName invalid: %q (allowed: ASCII letters, digits, "_" and "-", starting with a letter)`, name)
	}
	if out, err := yaml.Marshal(name); err != nil || string(out) != name+"\n" {
		return fmt.Errorf("agent.hermes.serverName invalid: %q (YAML may read it as a boolean or null rather than a name)", name)
	}
	return nil
}

// validDelay rejects a negative delay. A negative value used to act as a zero
// delay — the way around the restart delay's floor (see applyDefaults).
func validDelay(field string, seconds int) error {
	if seconds < 0 {
		return fmt.Errorf("%s invalid: %d (seconds, at least 1; 0 = default)", field, seconds)
	}
	return nil
}

// --- resolution ---

// autodetect is the auto-detection step of resolveEngine; tests replace it so
// that they do not depend on the engines installed on the host.
var autodetect = autodetectEngine

// resolveEngine determines the UE installation root; project (the resolved
// .uproject path, may be empty) feeds auto-detection its EngineAssociation.
// A relative UE_ROOT or engineRoot is made absolute against the working
// directory, which is where it pointed before: as typed, it would land in
// placeholders and permission rules relative ("Write(${ENGINE}/*)" with
// engineRoot "." would match anything). warning is auto-detection's (see
// chooseEngine); an engine given by UE_ROOT or engineRoot gets none.
func resolveEngine(c *Config, project string) (root, warning string, err error) {
	for _, v := range []string{strings.TrimSpace(os.Getenv("UE_ROOT")), c.EngineRoot} {
		if v == "" {
			continue
		}
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", "", fmt.Errorf("engine root %s: %w", v, err)
		}
		return filepath.ToSlash(abs), "", nil
	}
	root, warning = autodetect(project)
	return root, warning, nil
}

// resolveProject determines the .uproject: unreal.project if set (absolute or
// relative to the config directory; it must exist), otherwise the first
// *.uproject in the config directory, "" if there is none. It returns the full
// path, the directory and the name without the extension.
//
// unreal.project takes no placeholders: ${PROJECT_DIR}, ${PROJECT_NAME} and
// ${PROJECT} are derived from it, and ${ENGINE} auto-detection reads the
// project's EngineAssociation.
func resolveProject(c *Config, baseDir string) (project, dir, name string, err error) {
	p := strings.TrimSpace(c.Unreal.Project)
	switch {
	case strings.Contains(p, "${"):
		return "", "", "", fmt.Errorf("unreal.project %q: placeholders are not supported here, they are derived from this setting — give the path absolute or relative to %s", p, filepath.ToSlash(baseDir))
	case p != "":
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		st, statErr := os.Stat(p)
		if statErr != nil {
			return "", "", "", fmt.Errorf("unreal.project: %w", statErr)
		}
		if st.IsDir() {
			return "", "", "", fmt.Errorf("unreal.project: %s is a directory, not a .uproject file", filepath.ToSlash(p))
		}
	default:
		matches, _ := filepath.Glob(filepath.Join(baseDir, "*.uproject"))
		if len(matches) == 0 {
			return "", "", "", nil
		}
		sort.Strings(matches)
		p = matches[0]
	}
	p = filepath.ToSlash(p)
	dir = filepath.ToSlash(filepath.Dir(p))
	name = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	return p, dir, name, nil
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
