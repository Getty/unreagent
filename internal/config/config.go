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
// the engine the .uproject names → the highest version installed, compared
// numerically. The .uproject names it by its EngineAssociation: a version is
// looked up in the Epic Launcher's LauncherInstalled.dat, then its registry
// entries as the fallback, then the standard installation paths — the places
// the highest-version step searches as well; a source build's identifier only
// among the builds registered in the HKCU registry; a path (with / or \) is
// an engine root, relative to the .uproject's directory or, if absolute,
// taken as written; a blank association names the engine tree the .uproject
// lies in, if any. See engine.go.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	Agents      *bool  `yaml:"agents"`      // default true: with Materialize, also write the topic agents and the rules file next to Dir
}

// ViaMCP reports whether the skills MCP tool is to be served.
func (s SkillsConfig) ViaMCP() bool { return boolVal(s.Enabled) && boolVal(s.MCP) }

// ViaFiles reports whether the skills are to be written into Dir.
func (s SkillsConfig) ViaFiles() bool { return boolVal(s.Enabled) && s.Materialize }

// AgentFiles reports whether the shipped topic agents and the rules file are
// to be written next to Dir: only while the skills are written there, since
// an agent file names its skills by their materialized directories.
func (s SkillsConfig) AgentFiles() bool { return s.ViaFiles() && boolVal(s.Agents) }

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
	// KillCrashReporter has no effect (k154): the launcher no longer kills
	// crash reporters before a start, as the kill by image name ended the
	// crash monitor of every other editor on the machine, and the editor's own
	// reporter ends with its job. The key is still accepted, so a config
	// that sets it keeps loading; run() warns when it is set.
	KillCrashReporter *bool `yaml:"killCrashReporter"`
	// CleanOnRestart (default false) removes
	// Saved/Autosaves/PackageRestoreData.json before every start, and empties
	// Saved/Crashes before a start that does not follow a crash: the first
	// start of a launcher run and every other manual start (console start or
	// r, ue_start, ue_restart). Saved/Crashes is kept, so the crash dumps stay
	// readable, on an automatic restart (a retry of a failed start too), on a
	// start issued in place of a pending automatic restart (during the
	// restart delay, or the retry delay of a failed start), and on the first
	// start after the editor failed and was not restarted. A ue_stop or
	// console stop during that delay cancels the pending restart and ends the
	// crash episode, so a later start empties the folder.
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
	Enabled bool `yaml:"enabled"`
	// Address is the host:port the server listens on, a Go listen address:
	// an empty or wildcard host listens on every interface. Clients get
	// ClientURL, not the address as written.
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
	// AllowedOrigins, if non-empty, replaces the MCP server's built-in Origin
	// allowlist (http/https on localhost, 127.0.0.1 or ::1, any port): a POST
	// that carries an Origin header must carry one of these. Each entry is a
	// browser origin — scheme://host[:port] with http or https, or
	// scheme://host for a browser extension — compared with the whole header
	// case-insensitively, no wildcards (validOrigin). Empty
	// or absent keeps the built-in list. A request without an Origin header (a
	// non-browser MCP client) is accepted either way.
	AllowedOrigins []string `yaml:"allowedOrigins"`
	// ExtraServers are MCP server definitions passed through raw, in the format of
	// Claude Code's .mcp.json (fields type/url/command/args/env/headers …).
	// Placeholders like ${PROJECT_DIR} are substituted in all string values.
	ExtraServers map[string]map[string]interface{} `yaml:"extraServers"`
	// WriteConfig additionally writes the assembled MCP config to disk as file(s),
	// so external clients (a separate Claude session, Cursor, VS Code) can use it.
	WriteConfig []MCPOutput `yaml:"writeConfig"`
}

// ClientURL is the URL clients reach the MCP server at: the agent's
// --mcp-config, every mcp.writeConfig file and the Hermes entry. An empty or
// wildcard host (":8765", "0.0.0.0:8765", "[::]:8765") listens on every
// interface but is no address to connect to, so it becomes the loopback of
// its family, 127.0.0.1 or ::1. Any other host stays as written, an IPv6
// literal in brackets. Address is one validate accepted; one that does not
// split into host and port goes into the URL as written.
func (m MCPConfig) ClientURL() string {
	host, port, err := net.SplitHostPort(m.Address)
	if err != nil {
		return "http://" + m.Address + "/mcp"
	}
	if ip := net.ParseIP(host); host == "" || ip.IsUnspecified() {
		host = "127.0.0.1"
		if ip != nil && ip.To4() == nil {
			host = "::1"
		}
	}
	return "http://" + net.JoinHostPort(host, port) + "/mcp"
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
	// for the launcher to log as a WARN line: today that the engine the
	// .uproject names — by its EngineAssociation, or by lying in an engine
	// tree — is not usable (see chooseEngine), and on the defaults path a
	// toolkit bridge chosen among several and a directory the bridge search
	// could not read (loadDefaults).
	Warnings []string
	// Defaults is true when there was no config file and the configuration is
	// the built-in one (DefaultYAML; for LoadForInit the document init writes,
	// InitYAML). ConfigPath then names the file that was looked for and is not
	// there.
	Defaults bool
}

// Load reads the configuration. explicitPath is optional; if it is empty,
// unreagent.yaml, else unreagent.yml, is looked for next to the executable
// (findConfig). An unreagent.local.yaml sitting beside it is overlaid on top.
// A symlink in any of these places that leads to no file is an error, not a
// file that is not there. Each file holds one YAML document.
//
// With no explicitPath and neither file there, the launcher runs on the
// built-in defaults (DefaultYAML) for the one .uproject next to the
// executable, and Info.Defaults says so; no .uproject there, or several, is an
// error. An unreagent.local.yaml is overlaid on the defaults as on a file,
// except that it cannot set unreal.project. An explicitPath that does not
// exist stays an error.
func Load(explicitPath string) (*Config, Info, error) {
	return load(explicitPath, exeDir())
}

// LoadForPreflight is Load for `unreagent init`, which reports on a setup
// instead of starting it. It differs in one thing: an engine that is not found
// is not an error, so that the preflight can name it next to everything else.
// Info.EngineRoot is "" then, every field that uses ${ENGINE} keeps the
// placeholder as written (unreal.editor of the built-in defaults among them),
// and the built-in commands that need it are left out as usual
// (Info.SkippedCommands). Such a configuration is for looking at, never for
// starting anything. Everything else fails as in Load.
//
// dir is the directory the launcher's executable lies in; "" is the running
// executable's, as for Load. init's tests name a directory of their choice.
func LoadForPreflight(explicitPath, dir string) (*Config, Info, error) {
	if dir == "" {
		dir = exeDir()
	}
	return loadMode(explicitPath, dir, true, false)
}

// LoadForInit is LoadForPreflight as `unreagent init` sees the project: with
// no config file it loads InitYAML, the document init writes, in place of the
// built-in defaults — the configuration a start runs on once init has written
// it (Info.Defaults still says that no file is there). With a config file it
// is LoadForPreflight.
func LoadForInit(explicitPath, dir string) (*Config, Info, error) {
	if dir == "" {
		dir = exeDir()
	}
	return loadMode(explicitPath, dir, true, true)
}

// load is Load with the executable's directory as a parameter.
func load(explicitPath, dir string) (*Config, Info, error) {
	return loadMode(explicitPath, dir, false, false)
}

// loadMode is load; engineOptional makes it LoadForPreflight, forInit with it
// LoadForInit.
func loadMode(explicitPath, dir string, engineOptional, forInit bool) (*Config, Info, error) {
	var info Info

	path := explicitPath
	if path == "" {
		var err error
		if path, err = findConfig(dir); err != nil {
			return nil, info, err
		}
		present, err := configPresent(path)
		if err != nil {
			return nil, info, err
		}
		info.Defaults = !present
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
	if info.Defaults {
		warnings, err := loadDefaults(&c, path, forInit)
		if err != nil {
			return nil, info, err
		}
		info.Warnings = append(info.Warnings, warnings...)
	} else if err := decodeYAML(path, &c); err != nil {
		return nil, info, err
	}
	local := localPathFor(path)
	present, err := configPresent(local)
	if err != nil {
		return nil, info, err
	}
	if present {
		info.LocalPath = local
		if err := decodeYAML(local, &c); err != nil {
			return nil, info, err
		}
		if info.Defaults && strings.TrimSpace(c.Unreal.Project) != "" {
			return nil, info, fmt.Errorf("unreal.project in %s needs an unreagent.yaml beside it: without a config file the project is the .uproject next to the launcher — move the setting into an unreagent.yaml", filepath.Base(local))
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

	skipped, err := c.substitute(engine, project, projectDir, projectName, builtins, engineOptional)
	info.SkippedCommands = skipped
	if err != nil {
		return nil, info, err
	}

	if err := c.validate(); err != nil {
		if engineOptional && engine == "" && strings.Contains(err.Error(), "${ENGINE}") {
			// The value the error quotes still holds ${ENGINE}, and can fail
			// for that alone.
			return nil, info, fmt.Errorf("%w (checked with ${ENGINE} left in place: %s)", err, placeholderHint("${ENGINE}"))
		}
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
	if c.Skills.Agents == nil {
		t := true
		c.Skills.Agents = &t
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
// enums, mcp.address, mcp.allowedOrigins and mcp.token (no placeholder forms
// part of any of them, and a token must never change under the user's hands),
// descriptions (prose), and engineRoot and unreal.project, from which the
// placeholders are derived.
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
//
// engineOptional (LoadForPreflight) takes the error away from an empty
// ${ENGINE} and nothing else: the placeholder stays in the text instead of
// being replaced by nothing, and what is removed or cleared for it still is.
func (c *Config) substitute(engine, project, projectDir, projectName string, builtins map[string]bool, engineOptional bool) (skipped []string, err error) {
	values := [][2]string{
		{"${ENGINE}", engine},
		{"${PROJECT_DIR}", projectDir},
		{"${PROJECT_NAME}", projectName},
		{"${PROJECT}", project},
	}
	var pairs, empty []string
	engineKept := engineOptional && engine == ""
	for _, v := range values {
		if v[1] == "" {
			empty = append(empty, v[0])
			if engineKept && v[0] == "${ENGINE}" {
				continue // not replaced: the text keeps the placeholder
			}
		}
		pairs = append(pairs, v[0], v[1])
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
	// substituted path. A relative value would break this: "Write(./*)"
	// cleans to "*" (checkRules and matchValue refuse it rather than let it
	// grant everything, k104), and "Write(sub/*)" never matches the absolute
	// paths Claude Code sends, which leaves a deny rule inert. An empty one is
	// caught by the unresolved check ("Edit(/*)").
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
		if len(usedBy[p]) == 0 || engineKept && p == "${ENGINE}" {
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
	if c.Permissions.Enabled {
		if err := c.Permissions.checkRules(); err != nil {
			return err
		}
	}
	for name, cmd := range c.Commands {
		if cmd.Command == "" {
			return fmt.Errorf("commands.%s.command is missing", name)
		}
	}
	// hermes-setup writes the client URL into the Hermes entry whether the
	// MCP server is on or not.
	if c.MCP.Enabled || c.Agent.Hermes.Enabled {
		if err := validMCPAddress(c.MCP.Address); err != nil {
			return err
		}
	}
	if c.MCP.Enabled {
		for i, origin := range c.MCP.AllowedOrigins {
			if err := validOrigin(origin); err != nil {
				return fmt.Errorf("mcp.allowedOrigins[%d] invalid: %q (%v; want scheme://host[:port] (http or https) or scheme://host for a browser extension, e.g. https://ide.example.com)", i, origin, err)
			}
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

// mcpHostName is what an mcp.address host outside brackets may consist of:
// ASCII letters, digits, "-", "_" and "." — an IPv4 address or a host name.
// The "_" is for Windows computer names, which resolvers take; the URL
// Standard does not forbid it in a host. Empty is the wildcard.
var mcpHostName = regexp.MustCompile(`^[A-Za-z0-9._-]*$`)

// validMCPAddress refuses an mcp.address that is not host:port, whose port
// is not a number from 1 to 65535, or whose host is no host. net.Listen would
// take port 0 or a service name ("http"), but the client URL names the port
// as written: port 0 binds a random one no client is told, and a name is no
// URL port. The host goes into the client URL as written too, so it is empty
// (every interface), an IPv6 address in brackets without a zone, or
// mcpHostName. A zone ([fe80::1%eth0]) would need escaping as %25 there and a
// Node client takes none at all; user info, a path or a space ("user@host",
// "a/b") fails the bind, but hermes-setup writes the client URL with the MCP
// server off.
func validMCPAddress(address string) error {
	const want = "want host:port, e.g. 127.0.0.1:8765 or [::1]:8765"
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		var ae *net.AddrError
		if errors.As(err, &ae) {
			err = errors.New(ae.Err)
		}
		return fmt.Errorf("mcp.address invalid: %q (%v; %s)", address, err, want)
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return fmt.Errorf("mcp.address invalid: %q (port must be a number from 1 to 65535; %s)", address, want)
	}
	var cause string
	if strings.HasPrefix(address, "[") {
		addr, err := netip.ParseAddr(host)
		switch {
		case strings.Contains(host, "%"):
			cause = "no IPv6 zone allowed, a client URL takes none"
		case err != nil || !addr.Is6():
			cause = "brackets hold an IPv6 address only"
		}
	} else if !mcpHostName.MatchString(host) {
		cause = `host must be an IP address or a name of ASCII letters, digits, "-", "_" and "."`
	}
	if cause != "" {
		return fmt.Errorf("mcp.address invalid: %q (%s; %s)", address, cause, want)
	}
	return nil
}

// validOrigin refuses an mcp.allowedOrigins entry that no browser sends as an
// Origin header. The MCP server compares the header with each entry as a whole
// string (case-insensitively, nothing else), so such an entry would never
// match. A web page's origin is scheme://host[:port] with http or https:
// nothing after the host and port, no user info, the host in ASCII (an
// internationalized name as punycode, xn--…) and without a code point the URL
// Standard forbids in a host (forbiddenHostChars), an IP address in its one
// canonical form (canonicalIP), the port as a plain number and left out when
// it is the scheme's default — https://example.com, never
// https://example.com:443. A browser extension's page sends its own scheme and
// host, chrome-extension://<id> or moz-extension://<uuid>, with no port and
// nothing after the host. The other schemes a browser parses specially (file,
// ftp, ws, wss) never appear in an Origin header, nor does a scheme without a
// host; "null" does, from every sandboxed frame and local file, so allowing it
// would allow them all. The server knows no wildcards, so a "*" is refused
// rather than compared literally.
func validOrigin(origin string) error {
	if origin == "" {
		return errors.New("empty entry")
	}
	if strings.Contains(origin, "*") {
		return errors.New("wildcards are not supported, list each origin")
	}
	if strings.EqualFold(origin, "null") {
		return errors.New(`"null" is the Origin of every sandboxed frame and local file, it cannot be allowed`)
	}
	u, err := url.Parse(origin)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return err
	}
	web := u.Scheme == "http" || u.Scheme == "https"
	switch {
	case u.Scheme == "":
		return errors.New("missing scheme")
	case u.Scheme == "file" || u.Scheme == "ftp" || u.Scheme == "ws" || u.Scheme == "wss":
		return fmt.Errorf("a browser sends no Origin with scheme %s, only http, https or a browser extension's own", u.Scheme)
	case u.User != nil:
		return errors.New("no user info allowed")
	case u.Path != "":
		return errors.New("no path allowed, not even a trailing /")
	case u.RawQuery != "" || u.ForceQuery:
		return errors.New("no query allowed")
	case strings.Contains(origin, "#"):
		return errors.New("no fragment allowed")
	case u.Hostname() == "":
		return errors.New("missing host")
	}
	if port := u.Port(); port != "" || strings.HasSuffix(u.Host, ":") {
		if !web {
			return fmt.Errorf("no port allowed with scheme %s, only http and https take one", u.Scheme)
		}
		if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 || strconv.FormatUint(n, 10) != port {
			return errors.New("port must be a number from 1 to 65535, without leading zeros")
		}
		if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
			return fmt.Errorf("drop the default port :%s, a browser leaves it out of the Origin header", port)
		}
	}
	for i := 0; i < len(u.Host); i++ {
		if u.Host[i] >= 0x80 {
			return errors.New("non-ASCII host, write it as punycode (xn--…), the form a browser sends")
		}
	}
	if !strings.HasPrefix(u.Host, "[") {
		forbidden := forbiddenHostChars
		if web {
			forbidden += "%"
		}
		if i := strings.IndexAny(u.Hostname(), forbidden); i >= 0 {
			return fmt.Errorf("no %q allowed in a host, a browser refuses the URL", u.Hostname()[i:i+1])
		}
	}
	return canonicalIP(u.Hostname(), web)
}

// forbiddenHostChars are the URL Standard's forbidden host code points: a
// browser refuses a URL whose host holds one. url.Parse refuses most of them
// itself or reads them as a delimiter, but lets "<", ">" and "]" into a host.
// An http or https host is a domain, which takes no "%" either; validOrigin
// adds it there, the % of a %25 that url.Parse decoded. A bracketed host is
// an IPv6 address, which canonicalIP checks.
const forbiddenHostChars = "\x00\t\n\r #/:<>?@[\\]^|"

// canonicalIP refuses an IP host written in any form but the one a browser
// sends. A bracketed host (url.Parse has checked it is an IPv6 address) goes
// out compressed in lowercase as netip writes it — [::1], never
// [0:0:0:0:0:0:0:1] — except an IPv4-mapped one, which a browser writes in
// hex, [::ffff:7f00:1], not [::ffff:127.0.0.1]; it never carries a zone. For
// http and https a host that browserIPv4 reads as an IPv4 address goes out as
// a dotted quad: 127.1, 0x7f.0.0.1 and 2130706433 all as 127.0.0.1. Any other
// host is a name.
func canonicalIP(host string, web bool) error {
	if strings.Contains(host, ":") {
		addr, err := netip.ParseAddr(host)
		if err != nil {
			return err
		}
		if addr.Zone() != "" {
			return errors.New("no IPv6 zone allowed, a browser sends none")
		}
		want := addr.String()
		if addr.Is4In6() {
			b := addr.As16()
			want = fmt.Sprintf("::ffff:%x:%x", uint16(b[12])<<8|uint16(b[13]), uint16(b[14])<<8|uint16(b[15]))
		}
		if !strings.EqualFold(host, want) {
			return fmt.Errorf("non-canonical IPv6 host, write it as [%s], the form a browser sends", want)
		}
		return nil
	}
	if !web {
		return nil
	}
	addr, isIP, octal := browserIPv4(host)
	switch {
	case !isIP:
		return nil
	case !addr.IsValid():
		return errors.New("numeric host but no IPv4 address a browser accepts, write a dotted quad such as 192.168.1.20")
	case host != addr.String():
		msg := "non-canonical IPv4 host, write it as " + addr.String() + ", the form a browser sends"
		if octal {
			msg += " (a part with a leading 0 is octal)"
		}
		return errors.New(msg)
	}
	return nil
}

// browserIPv4 reads host as a browser reads the host of an http or https URL
// (the WHATWG URL Standard's IPv4 parser). A host whose last label, after one
// trailing dot, is all decimal digits or 0x and nothing but hex digits is an
// IPv4 address (isIP); any other host is a name. That goes by the characters,
// as in the URL Standard, not by what ipv4Number can parse: a hex number too
// large for it (http://a.0xffffffffffffffffffff) still makes the host an
// address, one the browser refuses. Such an address has one to four
// dot-separated parts, each decimal, octal with a leading 0 or hex with 0x;
// each part but the last is one byte, the last fills the remaining bytes. An
// address that breaks these rules, a URL the browser refuses, comes back
// invalid. octal reports a part read as octal.
func browserIPv4(host string) (addr netip.Addr, isIP, octal bool) {
	parts := strings.Split(host, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	last, digits := parts[len(parts)-1], "0123456789"
	if len(last) >= 2 && strings.EqualFold(last[:2], "0x") {
		last, digits = last[2:], "0123456789abcdefABCDEF"
	} else if last == "" {
		return addr, false, false
	}
	if strings.Trim(last, digits) != "" {
		return addr, false, false
	}
	if len(parts) > 4 {
		return addr, true, false
	}
	var v uint64
	for i, p := range parts {
		n, oct, err := ipv4Number(p)
		if err != nil {
			return netip.Addr{}, true, octal
		}
		octal = octal || oct
		if i < len(parts)-1 {
			if n > 255 {
				return netip.Addr{}, true, octal
			}
			v += n << (8 * (3 - i))
		} else {
			if n >= 1<<(8*(5-len(parts))) {
				return netip.Addr{}, true, octal
			}
			v += n
		}
	}
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), true, octal
}

// ipv4Number reads one part of an IPv4 host as a browser does: hex after 0x,
// octal after a leading 0, decimal otherwise; a bare "0x" or "0" is 0.
func ipv4Number(s string) (n uint64, octal bool, err error) {
	if s == "" {
		return 0, false, errors.New("empty part")
	}
	base := 10
	switch {
	case len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X"):
		base, s = 16, s[2:]
	case len(s) >= 2 && s[0] == '0':
		base, s = 8, s[1:]
	}
	if s == "" {
		return 0, base == 8, nil
	}
	n, err = strconv.ParseUint(s, base, 64)
	return n, base == 8, err
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

// MaterializeSetting reports whether the config file at path sets
// skills.materialize, and to what. A file that is not there, does not read or
// does not parse sets nothing. `unreagent init` uses it to name the file whose
// value decides — unreagent.local.yaml is decoded over unreagent.yaml, so a
// value it sets wins.
func MaterializeSetting(path string) (on, set bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	var doc struct {
		Skills struct {
			Materialize *bool `yaml:"materialize"`
		} `yaml:"skills"`
	}
	if yaml.Unmarshal(b, &doc) != nil || doc.Skills.Materialize == nil {
		return false, false
	}
	return *doc.Skills.Materialize, true
}

// decodeYAML decodes the config file at path into c. An empty file is fine; a
// second YAML document that holds anything, even just a comment, is an error,
// since only the first would be read. A bare "---" or "..." at the end holds
// nothing and goes through (EmptyDocument).
func decodeYAML(path string, c *Config) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config not readable (%s): %w", path, err)
	}
	return decodeDoc(filepath.Base(path), b, c)
}

// decodeDoc is decodeYAML for a document already read; name stands for it in
// errors.
func decodeDoc(name string, b []byte, c *Config) error {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true) // unknown fields = error (typo protection)
	if err := dec.Decode(c); err != nil {
		if err == io.EOF {
			return nil // an empty file is fine
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	for {
		var next yaml.Node
		err := dec.Decode(&next)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if !EmptyDocument(&next) {
			return fmt.Errorf("%s: it holds more than one YAML document (another one starts at line %d); refusing to drop all but the first", name, next.Line)
		}
	}
}

// EmptyDocument reports whether the document node d is what a bare "---"
// starts: a plain null with no text, no tag, no anchor and no comments. A
// later document like that holds nothing a reader of only the first one
// could miss.
func EmptyDocument(d *yaml.Node) bool {
	if len(d.Content) != 1 || d.HeadComment != "" || d.LineComment != "" || d.FootComment != "" {
		return false
	}
	n := d.Content[0]
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null" && n.Value == "" && n.Style == 0 &&
		n.Anchor == "" && n.HeadComment == "" && n.LineComment == "" && n.FootComment == ""
}

// configPresent reports whether the config file at path is there to be read:
// a candidate findConfig looks for, or the unreagent.local.yaml overlay. Only
// a path that does not exist is absent. The check does not follow a symlink,
// so one that leads to no file, dangling or looping, is an error naming it
// instead of a file skipped without a word.
func configPresent(path string) (bool, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("config not readable (%s): %w", path, err)
	}
	if _, err := ResolveLink(path); err != nil {
		return false, fmt.Errorf("config not readable: %w", err)
	}
	return true, nil
}

// ResolveLink returns the file a symlink at path leads to, or path itself when
// path is not a symlink or is not there. Only a symlinked file is resolved: a
// path through a symlinked directory stays as given. A symlink that leads to
// no file is an error naming path: a dangling one names its target and, at
// the end of a chain of links, the first one that is missing; its error
// matches os.ErrNotExist (errors.Is). A link that cannot be resolved at all,
// such as a loop, gets the error filepath.EvalSymlinks gives.
func ResolveLink(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return path, nil // not a symlink, or not there
	}
	target, err := filepath.EvalSymlinks(path)
	switch {
	case err == nil:
		return target, nil
	case os.IsNotExist(err):
		dest, _ := os.Readlink(path)
		if missing, chain := danglingEnd(path); chain {
			return "", &danglingLinkError{fmt.Sprintf("%s is a symlink to %s; following the links leads to %s, which does not exist", path, dest, missing), err}
		}
		return "", &danglingLinkError{fmt.Sprintf("%s is a symlink to %s, which does not exist", path, dest), err}
	default:
		return "", fmt.Errorf("%s is a symlink that cannot be resolved: %w", path, err)
	}
}

// danglingLinkError is ResolveLink's error for a symlink whose target does not
// exist. It wraps the error filepath.EvalSymlinks gave, so it matches
// os.ErrNotExist, but says which link leads where: that error names only a
// path along the way.
type danglingLinkError struct {
	msg string
	err error
}

func (e *danglingLinkError) Error() string { return e.msg }
func (e *danglingLinkError) Unwrap() error { return e.err }

// danglingEnd follows the symlink at path from link to link and returns the
// first target that does not exist. A relative target is read against the
// directory its link really is in, symlinks resolved, as the system does: a
// ".." in it leaves the directory a symlinked one points to. A target that
// starts at the root but names no drive (\x on Windows) is taken as it is, as
// filepath.EvalSymlinks takes it. chain is false when the missing target is
// path's own, or when none turns up.
func danglingEnd(path string) (missing string, chain bool) {
	cur := path
	for hops := 0; hops < 255; hops++ { // filepath.EvalSymlinks gives up after 255 links
		dest, err := os.Readlink(cur)
		if err != nil {
			return "", false
		}
		if rooted := filepath.IsAbs(dest) || dest != "" && os.IsPathSeparator(dest[0]); !rooted {
			dir, err := filepath.EvalSymlinks(filepath.Dir(cur))
			if err != nil {
				return "", false
			}
			dest = filepath.Join(dir, dest)
		}
		fi, err := os.Lstat(dest)
		if os.IsNotExist(err) {
			return dest, hops > 0
		}
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			return "", false
		}
		cur = dest
	}
	return "", false
}

// findConfig returns the config in dir, the executable's directory: the first
// of unreagent.yaml and unreagent.yml that is there, else unreagent.yaml. Only
// a name that does not exist is passed over (configPresent): a symlink that
// leads to no file, or any other error looking for one, ends the search with
// an error naming it, so an unreagent.yml is never loaded in place of a
// broken unreagent.yaml link.
func findConfig(dir string) (string, error) {
	for _, name := range []string{"unreagent.yaml", "unreagent.yml"} {
		p := filepath.Join(dir, name)
		present, err := configPresent(p)
		if err != nil {
			return "", err
		}
		if present {
			return p, nil
		}
	}
	return filepath.Join(dir, "unreagent.yaml"), nil
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
