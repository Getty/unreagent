package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/skills"
)

// initHeader heads the unreagent.yaml `unreagent init` writes.
const initHeader = "# Written by `unreagent init`: the built-in defaults, which a start without\n" +
	"# this file runs on as well, plus skills.materialize: true, which only init\n" +
	"# turns on — every start then writes the shipped skills into .claude/skills,\n" +
	"# and the topic agents and the rules file next to them (skills.agents: false\n" +
	"# keeps those two out). Edit freely — unreagent.example.yaml documents every\n" +
	"# option. Machine-specific values (engineRoot, a token) belong in a\n" +
	"# git-ignored unreagent.local.yaml next to this file.\n\n"

// The command `unreagent init` prints when the UE LLM Toolkit is missing: the
// fork's branch for UE 5.8 as a submodule, the layout the bridge search's
// depth is cut for (Plugins/ue-llm-toolkit/Plugin/UELLMToolkit).
const (
	toolkitForkURL     = "https://github.com/Getty/ue-llm-toolkit"
	toolkitForkBranch  = "ue-5.8"
	toolkitUpstreamURL = "https://github.com/ColtonWilley/ue-llm-toolkit"
	toolkitSubmodule   = "Plugins/ue-llm-toolkit"
)

// initCommand is `unreagent init`: next to the one .uproject it writes
// config.InitYAML — the built-in defaults plus skills.materialize: true — as
// unreagent.yaml, the starter CLAUDE.md into the project when it has none,
// and, while the config materializes skills, what a start writes into the
// project (writeSkillFiles: skills, topic agents, rules file); then it prints
// a preflight of what a start needs, with findings on those files. It never
// overwrites: a config file there already (unreagent.yaml or unreagent.yml)
// is left as it is and used, a CLAUDE.md is the user's. -check writes
// nothing at all and prints the preflight, for the config init would write
// where there is none. -config names a config to look at: init then writes
// nothing either.
//
// An engine that is not found does not end it at the load, as it ends a start
// of the launcher (config.LoadForPreflight): the defaults are written all the
// same — the document does not depend on the engine, and the fix belongs in
// the environment or in an unreagent.local.yaml next to it — and the preflight
// is printed in full, with the engine as its blocking finding.
//
// args, out and the usage errors are as for hermesSetup. The returned error
// is non-nil when the configuration cannot be loaded (no .uproject, or
// several, among the causes) or the preflight has a blocking finding; either
// way it ends on what init wrote in this run (wroteList), or nothing.
func initCommand(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Print the preflight for this unreagent.yaml (default: the one next to the executable); with -config init writes nothing")
	check := fs.Bool("check", false, "Write nothing — no config, no CLAUDE.md, no skills or agents — only print the preflight")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		// The flag set has printed the error and the usage to stderr.
		return usageError{msg: err.Error(), printed: true}
	}
	if fs.NArg() > 0 {
		return usageError{msg: fmt.Sprintf("init takes flags only, not %q (unreagent init -h lists them)", fs.Arg(0))}
	}
	// -config: a config to look at, perhaps for another project; nothing is
	// written for it (k246, k256).
	return initRun(func() (*config.Config, config.Info, error) { return config.LoadForInit(*cfgPath, "") }, *check || *cfgPath != "", out)
}

// initRun is initCommand behind its flags: load is config.LoadForInit for
// the path or directory in question.
func initRun(load func() (*config.Config, config.Info, error), check bool, out io.Writer) error {
	cfg, info, err := load()
	if err != nil {
		return fmt.Errorf("%w (unreagent init wrote nothing)", err)
	}
	var wrote []string // what this run wrote, for the closing line
	wroteConfig := false
	switch {
	case !info.Defaults && check:
		fmt.Fprintf(out, "Config: %s exists and is left as it is. Nothing is written: preflight only.\n", info.ConfigPath)
	case !info.Defaults:
		fmt.Fprintf(out, "Config: %s exists and is left as it is.\n", info.ConfigPath)
	case check:
		fmt.Fprintf(out, "No config file at %s; -check, so nothing is written. The preflight is for the unreagent.yaml init would write (the built-in defaults plus skills.materialize: true); a start without one runs on the built-in defaults and writes nothing into the project.\n", info.ConfigPath)
	default:
		doc, err := writeDefaultConfig(info.ConfigPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Wrote %s:\n\n%s\n\n", info.ConfigPath, indent(string(doc), "    "))
		wrote, wroteConfig = append(wrote, info.ConfigPath), true
		// From here on init uses the config it wrote, loaded as a start loads
		// it: unreagent.local.yaml over it, if there is one.
		if cfg, info, err = load(); err != nil {
			return fmt.Errorf("%w (unreagent init wrote %s)", err, wroteList(wrote))
		}
	}

	findings := preflight(cfg, info)
	if info.Project != "" {
		said := false
		say := func(l string) { fmt.Fprintln(out, l); said = true }
		setup, written, err := projectSetup(cfg, info, check, wroteConfig, say)
		wrote = append(wrote, written...)
		if said {
			fmt.Fprintln(out)
		}
		if err != nil {
			return fmt.Errorf("%w (unreagent init wrote %s)", err, wroteList(wrote))
		}
		findings = append(findings, setup...)
	}
	fmt.Fprintln(out, "Preflight:")
	blocking, warnings := 0, 0
	for _, f := range findings {
		switch f.level {
		case levelBlocking:
			blocking++
		case levelWarn:
			warnings++
		}
		fmt.Fprintf(out, "  %-10s %s\n", "["+f.level+"]", f.text)
		if f.fix != "" {
			fmt.Fprintln(out, indent("Fix: "+f.fix, "             "))
		}
	}
	fmt.Fprintf(out, "%d blocking, %d warning(s). BLOCKING keeps the editor from starting; with a WARN the launcher starts, and the finding says what is missing then.\n", blocking, warnings)
	if blocking > 0 {
		return fmt.Errorf("preflight: %d blocking finding(s), see [BLOCKING] above (unreagent init wrote %s)", blocking, wroteList(wrote))
	}
	return nil
}

// wroteList is what init's closing line says it wrote: each item, joined as
// "a, b and c", or "nothing".
func wroteList(items []string) string {
	switch len(items) {
	case 0:
		return "nothing"
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// writeDefaultConfig writes config.InitYAML for the project directory of
// path to path, which must not exist (writeNewFile). It returns the document
// without the header comment.
func writeDefaultConfig(path string) ([]byte, error) {
	doc, _, _, err := config.InitYAML(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if err := writeNewFile(path, append([]byte(initHeader), doc...)); err != nil {
		return nil, err
	}
	return doc, nil
}

// writeNewFile writes data to path, which must not exist: the file is created
// exclusively, so nothing that appeared since it was looked for is
// overwritten either (the error then wraps fs.ErrExist).
func writeNewFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("unreagent init does not overwrite, and could not create %s: %w", path, err)
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// indent puts prefix before every line of text.
func indent(text, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n"+prefix)
}

const (
	levelOK       = "ok"
	levelInfo     = "info"
	levelWarn     = "WARN"
	levelBlocking = "BLOCKING"
)

// finding is one line of the preflight; fix says what to do about it.
type finding struct {
	level, text, fix string
}

// preflight checks what a start of the launcher on cfg needs, without
// starting anything. Blocking is what keeps the editor from starting at all:
// no project, no engine. cfg is config.LoadForPreflight's, so without an
// engine the fields that use ${ENGINE} still hold the placeholder. What needs
// the engine is not looked up then: the editor binary is part of the blocking
// engine line, the plugins' EngineVersion and every command or path that
// holds the placeholder (withoutEngine) are reported as not checked. With an
// engine, everything else — the editor binary, the agent command, node, npm,
// the toolkit, an unbuilt module — is a warning, as it is at a start of the
// launcher, which goes on without it.
func preflight(cfg *config.Config, info config.Info) []finding {
	var fs []finding
	add := func(level, text, fix string) { fs = append(fs, finding{level, text, fix}) }

	if info.Project == "" {
		add(levelBlocking, "project: no .uproject found next to the config", "put the launcher and unreagent.yaml next to the .uproject, or set unreal.project in unreagent.yaml")
	} else {
		add(levelOK, "project: "+info.Project, "")
	}

	engineDir := ""
	engineVersion, engineVersionErr := ueVersion{}, errNoEngine
	if info.EngineRoot == "" {
		text := "engine: not found"
		if strings.Contains(cfg.Unreal.Editor, "${ENGINE}") {
			text += " — the launcher does not start without it: unreal.editor is " + cfg.Unreal.Editor
		}
		add(levelBlocking, text, "set the environment variable UE_ROOT, or engineRoot in a git-ignored unreagent.local.yaml next to unreagent.yaml, to the directory that holds Engine/")
	} else {
		engineDir = engineDirOf(cfg.Unreal.Editor, info.EngineRoot)
		engineVersion, engineVersionErr = compatibleEngineVersion(engineDir)
		text := "engine: " + info.EngineRoot
		if engineVersionErr == nil {
			text += fmt.Sprintf(" (UE %d.%d)", engineVersion.major, engineVersion.minor)
		}
		add(levelOK, text, "")
		warnIfMissing(func(line string) {
			add(levelWarn, strings.TrimPrefix(line, "WARN "), "check that engineRoot names a complete engine install, or set unreal.editor to the editor binary")
		}, "unreal.editor", cfg.Unreal.Editor)
	}
	// The engine the .uproject names is not the one used, a Plugins directory
	// the bridge search could not read: complete messages with their fix.
	for _, w := range info.Warnings {
		add(levelWarn, w, "")
	}

	// A start binds mcp.address before anything else and ends if it cannot
	// (k252); with the MCP server off there is nothing to bind. The error is
	// listenMCP's and names its fix. Whether a running unreagent holds the port
	// is not told apart: nothing in the tree identifies one for this project.
	if cfg.MCP.Enabled {
		if ln, err := listenMCP(cfg.MCP.Address); err != nil {
			add(levelBlocking, err.Error(), "")
		} else {
			_ = ln.Close()
			add(levelOK, "mcp: "+cfg.MCP.Address+" can be bound", "")
		}
	}

	if !cfg.Agent.Enabled {
		add(levelInfo, "agent: off (agent.enabled is false) — the launcher starts no agent; an external one can connect to the MCP server", "")
	} else if withoutEngine(info, cfg.Agent.Command) {
		add(levelInfo, "agent: not checked — agent.command is "+cfg.Agent.Command+", which needs the engine", "")
	} else if _, err := exec.LookPath(cfg.Agent.Command); err != nil {
		add(levelWarn, "agent: command "+cfg.Agent.Command+" is not on PATH — the launcher starts the editor and the MCP server, and the agent fails to start",
			"install Claude Code (https://code.claude.com/docs/en/setup) so that claude is on PATH, or set agent.command to its full path in unreagent.local.yaml")
	} else {
		add(levelOK, "agent: "+cfg.Agent.Command+" is on PATH", "")
	}

	if info.Project != "" {
		fs = append(fs, toolkitFindings(cfg, info, engineVersion, engineVersionErr)...)
	}

	if info.Project != "" && engineDir != "" {
		quiet := true
		checkPluginEngineVersions(info.Project, engineDir, editorArgs(cfg.Unreal, func(string) {}), func(line string) {
			quiet = false
			if text, isWarn := strings.CutPrefix(line, "WARN "); isWarn {
				add(levelWarn, text, "") // the message names its fix
			} else {
				add(levelInfo, line, "")
			}
		})
		if quiet {
			add(levelOK, "plugin EngineVersion: no project plugin is predicted to be refused by this engine (the check says nothing where it cannot tell)", "")
		}
	} else if info.Project != "" {
		add(levelInfo, "plugin EngineVersion: not checked — without an engine there is no version to hold the project plugins' EngineVersion against", "")
	}

	if info.Project != "" {
		fs = append(fs, moduleFinding(cfg, info))
		fs = append(fs, pluginModuleFindings(cfg, info)...)
	}

	if cfg.Permissions.Enabled {
		add(levelInfo, "permissions: enabled, mode "+cfg.Permissions.Mode+" — the launcher warns at start if the policy cannot reach this agent", "")
	} else {
		add(levelInfo, "permissions: off — unreagent applies no permission policy: "+noPolicyMeaning+". To have unreagent decide, set permissions: as unreagent.example.yaml describes; a policy reaches Claude Code only when it runs headless", "")
	}
	return fs
}

// toolkitFindings reports on the UE LLM Toolkit for the project of info: the
// plugin's bridge below Plugins/ (config.DefaultYAML's search), the
// mcp.extraServers entry for it, node and npm, and the bridge's node_modules.
// engine is the engine's version when engineErr is nil; it decides how the
// command to add a missing toolkit is worded.
func toolkitFindings(cfg *config.Config, info config.Info, engine ueVersion, engineErr error) []finding {
	var fs []finding
	add := func(level, text, fix string) { fs = append(fs, finding{level, text, fix}) }
	projectDir := filepath.Dir(info.Project)
	doc, bridges, searchWarnings, err := config.InitYAML(projectDir) // the document shown is the one init writes
	if err != nil {
		add(levelWarn, "UE LLM Toolkit: not checked: "+err.Error(), "")
		return fs
	}
	// On the defaults path config.Load has reported both already (info.Warnings).
	if !info.Defaults {
		for _, w := range searchWarnings {
			add(levelWarn, w, "")
		}
		if len(bridges) > 1 {
			add(levelInfo, "several UE LLM Toolkit bridges found below Plugins/: "+strings.Join(bridges, ", "), "")
		}
	}

	entry, configured := cfg.MCP.ExtraServers[config.ToolkitServerName]
	command, _ := entry["command"].(string)
	var script string
	if raw, ok := entry["args"].([]interface{}); ok {
		var args []string
		for _, a := range raw {
			if s, ok := a.(string); ok {
				args = append(args, s)
			}
		}
		script = nodeScript(args)
	}
	scriptNeedsEngine := withoutEngine(info, script)
	if script != "" && !scriptNeedsEngine && !filepath.IsAbs(script) {
		script = filepath.Join(projectDir, script)
	}

	const without = "the agent has unreagent's own tools (builds, logs, scripts) but none that act inside the editor"
	switch {
	case configured && script == "":
		add(levelInfo, "UE LLM Toolkit: mcp.extraServers."+config.ToolkitServerName+" is configured without a script unreagent recognises; the launcher's smoke test at start judges it", "")
	case configured && scriptNeedsEngine:
		add(levelInfo, "UE LLM Toolkit: not checked — the bridge script the config names is "+script+", which needs the engine", "")
	case configured && !fileExists(script):
		add(levelWarn, "UE LLM Toolkit: the bridge script the config names is not there: "+script+" — "+without,
			"correct the path under mcp.extraServers."+config.ToolkitServerName+".args, or add the plugin:\n"+toolkitAddCommand(engine, engineErr))
	case configured:
		add(levelOK, "UE LLM Toolkit: bridge "+script, "")
	case len(bridges) > 0:
		add(levelWarn, "UE LLM Toolkit: the plugin is there ("+bridges[0]+") but the config has no mcp.extraServers."+config.ToolkitServerName+" entry — "+without,
			"add the entry to "+filepath.Base(info.ConfigPath)+"; `unreagent init` would write this document now:\n"+indent(string(doc), "    "))
	default:
		add(levelWarn, "UE LLM Toolkit: not found below Plugins/ — "+without, "add the plugin:\n"+toolkitAddCommand(engine, engineErr)+
			"\nThen run `unreagent init` again: with an unreagent.yaml in place it prints the mcp.extraServers entry to add to it; a start without a config file finds the plugin by itself.")
	}

	// node starts the bridge; npm installs its dependencies at the launcher's
	// first start. Without the toolkit neither is needed for it.
	level, need := levelWarn, ""
	if !configured && len(bridges) == 0 {
		level, need = levelInfo, " (needed only for the UE LLM Toolkit bridge)"
	}
	node := "node"
	if configured && command != "" {
		node = command
	}
	for _, tool := range []struct{ label, command, role string }{
		{"node", node, "the bridge cannot start"},
		{"npm", cfg.Runtimes.Node.Npm, "the bridge's dependencies cannot be installed"},
	} {
		if withoutEngine(info, tool.command) {
			add(levelInfo, tool.label+": not checked — "+tool.command+" needs the engine", "")
		} else if _, err := exec.LookPath(tool.command); err != nil {
			add(level, tool.label+": "+tool.command+" is not on PATH — "+tool.role+need, "install Node.js (https://nodejs.org), which brings both node and npm, and open a new console so that PATH is current")
		} else {
			add(levelOK, tool.label+": "+tool.command+" is on PATH", "")
		}
	}

	if configured && script != "" && fileExists(script) && fileExists(filepath.Join(filepath.Dir(script), "package.json")) {
		modules := filepath.Join(filepath.Dir(script), "node_modules")
		switch {
		case fileExists(filepath.Join(modules, bridgeInstallMarker)):
			add(levelOK, "bridge dependencies: installed, and the bridge passed the launcher's smoke test on them", "")
		case fileExists(modules):
			add(levelInfo, "bridge dependencies: node_modules is there but not marked as working — the launcher runs npm install and a smoke test at its next start", "")
		default:
			add(levelInfo, fmt.Sprintf("bridge dependencies: not installed — the launcher runs npm install at its first start (needs npm and the network, at most %s) and a smoke test after it", bridgeInstallTimeout), "")
		}
	}
	return fs
}

// withoutEngine reports whether s cannot be looked up because it holds
// ${ENGINE} and no engine was found (config.LoadForPreflight leaves the
// placeholder in place then).
func withoutEngine(info config.Info, s string) bool {
	return info.EngineRoot == "" && strings.Contains(s, "${ENGINE}")
}

// errNoEngine stands in for the engine's version where no engine was found.
var errNoEngine = errors.New("no engine")

// toolkitAddCommand is the command that adds the UE LLM Toolkit to a project,
// indented, with what is known about its fit: the fork's branch is made for
// UE 5.8, and for any other engine unreagent knows of no matching version.
func toolkitAddCommand(engine ueVersion, engineErr error) string {
	text := "    git submodule add -b " + toolkitForkBranch + " " + toolkitForkURL + " " + toolkitSubmodule + "\n" +
		"(in the project directory; for a project that is no git repository: git clone -b " + toolkitForkBranch + " " + toolkitForkURL + " " + toolkitSubmodule + ")\n" +
		"That is the " + toolkitForkBranch + " branch of a fork, made for UE 5.8; upstream is " + toolkitUpstreamURL + "."
	switch {
	case engineErr == errNoEngine:
		text += " No engine was found, so whether the branch fits yours is unknown."
	case engineErr != nil:
		text += " The version of this engine could not be read, so whether the branch fits it is unknown."
	case engine.major != 5 || engine.minor != 8:
		text += fmt.Sprintf(" This engine is UE %d.%d: that branch is not made for it, and unreagent does not know which version of the toolkit is — look upstream.", engine.major, engine.minor)
	}
	return text + "\nThe plugin has C++ modules: build the project's editor target after adding it."
}

// moduleFinding is the preflight's line on whether the project's C++ modules
// are built — the guess modules.go describes, with its label.
func moduleFinding(cfg *config.Config, info config.Info) finding {
	projectDir := filepath.Dir(info.Project)
	missing, declared := unbuiltModules(info.Project)
	if declared == 0 {
		if fi, err := os.Stat(filepath.Join(projectDir, "Source")); err == nil && fi.IsDir() {
			return finding{levelInfo, "project modules: not checked — there is a Source/ directory, but no Modules could be read from the .uproject", ""}
		}
		return finding{levelOK, "project modules: the .uproject declares none, so there is nothing to build", ""}
	}
	if len(missing) == 0 {
		return finding{levelOK, "project modules: built, as far as unreagent can tell" + moduleHeuristic, ""}
	}
	return finding{levelWarn, "project modules: " + strings.Join(missing, ", ") + " look unbuilt — no Binaries/Win64/UnrealEditor-<Name>.dll for them" + moduleHeuristic +
		". An editor that misses a module's binary exits at startup, and the launcher gives up on it after a few tries", moduleBuildFix(cfg, info)}
}

// moduleBuildFix is the preflight's fix for unbuilt modules: the project's
// editor target in the IDE, or the launcher's compile command where it can be
// named.
func moduleBuildFix(cfg *config.Config, info config.Info) string {
	fix := "build the project's editor target (" + info.ProjectName + "Editor, Win64, Development) in your IDE"
	if c, ok := cfg.Commands["compile"]; ok && c.Command != "" && !withoutEngine(info, strings.Join(append([]string{c.Command}, c.Args...), " ")) {
		fix += ", or run what the launcher's compile command runs:\n    " + strings.Join(append([]string{c.Command}, c.Args...), " ")
	}
	return fix
}

// pluginModuleFindings is the preflight's line on whether the modules of the
// project's plugins are built (k251) — the guess modules.go describes, with
// its label; none when no enabled project plugin with source declares a
// module the editor loads.
func pluginModuleFindings(cfg *config.Config, info config.Info) []finding {
	if pluginModulesUnchecked(editorArgs(cfg.Unreal, func(string) {})) {
		return []finding{{levelInfo, "plugin modules: not checked — the editor's arguments carry -NoEnginePlugins or -DisablePlugins=, which change the plugins it enables in a way unreagent does not read", ""}}
	}
	missing, checked := unbuiltPluginModules(info.Project, nil)
	switch {
	case checked == 0:
		return nil
	case len(missing) == 0:
		return []finding{{levelOK, "plugin modules: built, as far as unreagent can tell" + pluginModuleHeuristic, ""}}
	}
	return []finding{{levelWarn, "plugin modules: " + strings.Join(missing, ", ") + " look unbuilt — no Binaries/Win64/UnrealEditor-<Name>.dll for them in their plugin's directory" + pluginModuleHeuristic +
		". " + strings.ToUpper(pluginModuleEffect[:1]) + pluginModuleEffect[1:] + ", and the launcher gives up on it after a few tries", moduleBuildFix(cfg, info)}}
}

// projectSetup is init's part in the project's own files (k256). It writes
// the starter CLAUDE.md into the project directory when the project has none,
// and, while cfg materializes skills, runs what a start writes into the
// project (writeSkillFiles: the skills, the topic agents, the rules file), so
// that the project is complete before its first start. Under check it writes
// nothing and the findings say what init would do. say prints a line of
// init's output at once, before what follows is done; wrote names what was
// written for init's closing line; err is a file set that does not load.
// wroteConfig says that this run of init has just written the config cfg was
// loaded from.
//
// The materialization's sweep replaces or deletes every unreagent-* directory
// in skills.dir. Where nothing shows that the launcher has materialized there
// before (launcherWroteAgentsIn), init names in a WARN each one it cannot show
// to be its own copy (unprovenSkillDirs), as it may be the project's own
// skill, and writes no skills, agents or rules file. Where something does,
// one named like a shipped skill is the launcher's from an earlier version,
// and replacing it is the refresh every start does: it is not named. Init
// then names those the sweep deletes, the unreagent-* directories not named
// like a shipped skill, before it sweeps, and refreshes as a start does. A
// plain start keeps its rule: it replaces and deletes them.
func projectSetup(cfg *config.Config, info config.Info, check, wroteConfig bool, say func(string)) (fs []finding, wrote []string, err error) {
	add := func(level, text, fix string) { fs = append(fs, finding{level, text, fix}) }
	projectDir := filepath.Dir(info.Project)
	set, err := loadEmbeddedAgents()
	if err != nil {
		return nil, nil, err
	}

	// CLAUDE.md: the project's own, at the top or in .claude/ (Claude Code
	// reads either); the starter is written at the top.
	claudeMD, present := filepath.Join(projectDir, "CLAUDE.md"), false
	for _, p := range []string{claudeMD, filepath.Join(projectDir, ".claude", "CLAUDE.md")} {
		if _, err := os.Lstat(p); err == nil {
			claudeMD, present = p, true
			break
		}
	}
	starter := ""
	switch {
	case !present && check:
		add(levelInfo, "CLAUDE.md: none in the project — `unreagent init` writes the starter to "+claudeMD+": a kickstart block that has the agent's main session ask you about the project, and the sections the answers go into", "")
		starter = ", and the starter would be written there"
	case !present:
		err := writeNewFile(claudeMD, []byte(set.Starter))
		switch {
		case err == nil:
			say("Wrote " + claudeMD + ": the starter CLAUDE.md. Its kickstart block has the agent's main session ask you about the project, write the answers into the file and remove the block. The file is yours from now on: unreagent never writes to it again.")
			wrote = append(wrote, claudeMD)
			fs = append(fs, claudeMDFinding(claudeMD))
			starter = ", and the starter written there"
		case errors.Is(err, os.ErrExist):
			fs = append(fs, claudeMDFinding(claudeMD)) // appeared since: the user's
		default:
			add(levelWarn, "CLAUDE.md: the starter could not be written: "+err.Error(), "")
		}
	default:
		fs = append(fs, claudeMDFinding(claudeMD))
	}
	// Claude Code reads CLAUDE.md from its working directory upward: an agent
	// that works outside the project directory may never see the one looked
	// for here; one working in it or below it finds it on the way up.
	if w := cfg.Agent.Workdir; cfg.Agent.Enabled && w != "" && !strings.Contains(w, "${") && !inDir(w, projectDir) {
		fs[len(fs)-1].text += ". CLAUDE.md was looked for in the project directory " + projectDir + " (CLAUDE.md and .claude/CLAUDE.md)" + starter +
			"; agent.workdir is " + w + ", so the agent's working directory is elsewhere, and Claude Code reads CLAUDE.md from its working directory and the directories above it"
	}
	if cfg.Agent.Hermes.Enabled || (cfg.Agent.Enabled && !cfg.Agent.ClaudeIntegration) {
		why := "agent.claudeIntegration is false"
		if cfg.Agent.Hermes.Enabled {
			why = "agent.hermes.enabled is set"
		}
		add(levelInfo, "agent files: CLAUDE.md, the topic agents and the rules file are read by Claude Code only — "+why+", and an agent that is not Claude Code reads none of them", "")
	}

	s := cfg.Skills
	agentWorkdir := cfg.Agent.Workdir // as run() takes it
	if agentWorkdir == "" {
		agentWorkdir = projectDir
	}
	dir := skillsDirFor(s, agentWorkdir)
	if !s.ViaMCP() && !s.ViaFiles() {
		add(levelInfo, "skills: off (skills.enabled is false) — no skills, topic agents or rules file are written into the project, and the skills MCP tool is not served", "")
		return fs, wrote, nil
	}
	if dir != "" && strings.Contains(dir, "${") {
		add(levelInfo, "skills: not checked — skills.dir is "+dir+", which needs the engine; no skills, topic agents or rules file are written", "")
		return fs, wrote, nil
	}
	all, err := loadEmbeddedSkills()
	if err == nil {
		err = checkSkillDirs(all)
	}
	if err != nil {
		return fs, wrote, err
	}
	claude, underClaude := claudeDirOf(dir)
	earlier := dir != "" && underClaude && launcherWroteAgentsIn(claude)
	var replaced, removed []string
	var scanErr error
	if dir != "" {
		replaced, removed, scanErr = unprovenSkillDirs(dir, all)
	}
	// After an earlier materialization a directory named like a shipped skill
	// is the launcher's own copy from an earlier version: refreshing it is
	// what every start does, so only the ones the sweep deletes are named.
	own := " unreagent-* directories unreagent cannot tell are its own: "
	if earlier {
		replaced, own = nil, " unreagent-* directories not named like a shipped skill: "
	}
	unproven := describeUnproven(replaced, removed)

	if !s.ViaFiles() {
		add(levelInfo, "skills: not written into the project — skills.materialize is off, so no skills, topic agents or rules file are written there; the agent gets the skills through the skills MCP tool alone", materializeOffFix(info))
		what := "replaces those named like a shipped skill and deletes the others"
		if earlier {
			what = "deletes them"
		}
		if unproven != "" {
			add(levelWarn, "skills.dir "+dir+" holds"+own+unproven+" — once skills.materialize is on, the first start "+what+", as it owns every unreagent-* directory there",
				"move your own out of "+dir+", or rename them without the unreagent- prefix, before you turn skills.materialize on")
		}
		return fs, wrote, nil
	}
	if dir == "" {
		add(levelWarn, "skills: not written — skills.dir names a placeholder that is unresolved", "set unreal.project or skills.dir")
		return fs, wrote, nil
	}

	// Without an earlier materialization, init holds; with one it sweeps as a
	// start does, and names what the sweep takes all the same.
	hold := !earlier && (unproven != "" || scanErr != nil)
	if scanErr != nil {
		text := "skills.dir " + dir + " could not be read: " + scanErr.Error()
		if hold {
			text += " — init writes no skills, topic agents or rules file"
		}
		add(levelWarn, text, "")
	}
	switch {
	case unproven != "" && earlier && check:
		add(levelWarn, "skills.dir "+dir+" holds"+own+unproven+" — an agent or rules file the launcher wrote next to it shows an earlier materialization there, so `unreagent init` and every start delete them",
			"move them out of "+dir+", or rename them without the unreagent- prefix, before you run `unreagent init` or start the launcher")
	case unproven != "" && earlier:
		say("Deleting from " + dir + " the" + own + unproven + " — an earlier materialization there makes them the launcher's, and init sweeps as every start does.")
		add(levelWarn, "skills.dir "+dir+" held"+own+unproven+" — an agent or rules file the launcher wrote next to it shows an earlier materialization there, so init deleted them, as every start does",
			"if one of them was your own, get it back from version control or a backup, and keep your own skills under a name without the unreagent- prefix")
	case unproven != "":
		var what string
		switch {
		case check && info.Defaults:
			what = "`unreagent init` would leave them and write no skills, topic agents or rules file — but the unreagent.yaml it writes at " + info.ConfigPath +
				" turns skills.materialize on, so the first start after it replaces those named like a shipped skill and deletes the others, unless you move them first"
		case check:
			what = "`unreagent init` would leave them and write no skills, topic agents or rules file — but skills.materialize is on in " + materializeOnIn(info, false) +
				", so the next start of the launcher replaces those named like a shipped skill and deletes the others, unless you move them first"
		default:
			what = "init left them and wrote no skills, topic agents or rules file — but skills.materialize is on in " + materializeOnIn(info, wroteConfig) +
				", so the next start of the launcher replaces those named like a shipped skill and deletes the others, unless you move them first"
		}
		add(levelWarn, "skills.dir "+dir+" holds"+own+unproven+". "+what,
			"move your own out of "+dir+", or rename them without the unreagent- prefix, before the next start; then run `unreagent init` again. If they are only copies an earlier unreagent wrote, there is nothing to keep: start the launcher, and its sweep replaces and deletes them")
	}

	if check || hold {
		verb := "`unreagent init` writes"
		if hold {
			verb = "init writes none while the directories above are there; a start writes"
		}
		add(levelInfo, fmt.Sprintf("skills: %s %d shipped skills into %s, as every start does with skills.materialize on", verb, len(all), dir), "")
		fs = append(fs, agentPlanFindings(s, dir, set, hold)...)
		return fs, wrote, nil
	}

	var logged []string
	w := writeSkillFiles(s, all, agentWorkdir, func(l string) { logged = append(logged, l) })
	switch {
	case w.skillsDir != "" && w.skillsPartly:
		wrote = append(wrote, "part of the shipped skills into "+w.skillsDir)
	case w.skillsDir != "":
		wrote = append(wrote, fmt.Sprintf("the %d shipped skills into %s", len(all), w.skillsDir))
	}
	switch {
	case w.agents == 1:
		wrote = append(wrote, "1 file of the topic agents and the rules file into "+w.claude)
	case w.agents > 1:
		wrote = append(wrote, fmt.Sprintf("%d files of the topic agents and the rules file into %s", w.agents, w.claude))
	}
	what := "the shipped skills"
	if w.skillsPartly {
		what = "part of the shipped skills"
	}
	say("Wrote " + what + " into " + dir + " and what goes next to them, as every start does from now on; the preflight below says what.")
	for _, l := range logged {
		switch {
		case strings.HasPrefix(l, "WARN "):
			add(levelWarn, strings.TrimPrefix(l, "WARN "), "")
		case strings.HasPrefix(l, "Agents: not written"):
			add(levelInfo, lowerFirst(l), "")
		default:
			add(levelOK, lowerFirst(l), "")
		}
	}
	return fs, wrote, nil
}

// materializeOnIn names the file that turns skills.materialize on for init's
// findings: unreagent.local.yaml when it sets it, which wins as it is laid
// over the config; else the config, which wroteConfig says init has just
// written.
func materializeOnIn(info config.Info, wroteConfig bool) string {
	if info.LocalPath != "" {
		if on, set := config.MaterializeSetting(info.LocalPath); set && on {
			return info.LocalPath
		}
	}
	if wroteConfig {
		return "the unreagent.yaml init has just written (" + info.ConfigPath + ")"
	}
	return info.ConfigPath
}

// materializeOffFix is the fix for skills.materialize off: the file whose
// value decides and what to change in it. unreagent.local.yaml is laid over
// the config, so a value it sets wins; else the config's false is changed to
// true, or, when it sets none, the lines go into the config, never into
// unreagent.local.yaml (whether a project carries the skills and
// agents is the project's choice, the same for every checkout). The config
// turns it on when it sets it, or when it is the one init writes
// (info.Defaults: no file yet, under -check).
func materializeOffFix(info config.Info) string {
	cfgName := filepath.Base(info.ConfigPath)
	base := cfgName
	if info.Defaults {
		base = "the " + cfgName + " init writes"
	}
	baseOn, baseSets := config.MaterializeSetting(info.ConfigPath)
	baseOn = baseOn || info.Defaults
	// A config that sets it false already has the key: adding a second one
	// would break it, so the value is changed instead.
	baseOff := baseSets && !baseOn
	localSets := false
	if info.LocalPath != "" {
		_, localSets = config.MaterializeSetting(info.LocalPath)
	}
	localName := filepath.Base(info.LocalPath)
	lines := "(into its skills: section, if it has one):\n    skills:\n      materialize: true\n" +
		"There and not in unreagent.local.yaml: whether a project carries the skills and agents is the project's choice, the same for everyone who checks it out, and unreagent.local.yaml is for one machine's values."
	var fix string
	switch {
	case localSets && baseOn:
		fix = localName + " sets skills.materialize: false and is laid over " + base + ", which turns it on: remove skills.materialize from " + localName + " to have every start write them."
	case localSets && baseOff:
		fix = "to have every start write them, remove skills.materialize from " + localName + ", which is laid over " + cfgName + " and wins, and change skills.materialize from false to true in " + cfgName + "."
	case localSets:
		fix = "to have every start write them, remove skills.materialize from " + localName + ", which is laid over " + cfgName + " and wins, and add to " + cfgName + " " + lines
	case baseOff:
		fix = "to have every start write them, change skills.materialize from false to true in " + cfgName + "."
		if info.LocalPath != "" {
			fix += " " + localName + " is laid over it: a skills.materialize there wins."
		}
	default:
		fix = "to have every start write them, add to " + cfgName + " " + lines
		if info.LocalPath != "" {
			fix += " " + localName + " is laid over it: a skills.materialize there wins."
		}
	}
	return fix + " Then run `unreagent init` again."
}

// inDir reports whether path is dir or lies below it, as paths: both made
// absolute and clean, compared as text — links are not resolved, and case is
// not folded, so on Windows a path that differs from dir only in case counts
// as outside it (unverified there; it only adds an info note).
func inDir(path, dir string) bool {
	p, err1 := filepath.Abs(path)
	d, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(d, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// lowerFirst is s with its first letter lower case, as the findings start.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// describeUnproven names the directories unprovenSkillDirs found, each with
// what materializing does to it; "" when there are none.
func describeUnproven(replaced, removed []string) string {
	var parts []string
	for _, n := range replaced {
		parts = append(parts, n+" (replaced)")
	}
	for _, n := range removed {
		parts = append(parts, n+" (deleted)")
	}
	return strings.Join(parts, ", ")
}

// claudeMDFinding is the finding on the project's CLAUDE.md at path: there,
// and whether the starter's kickstart block is still in it (both marker lines
// found, each alone on its line).
func claudeMDFinding(path string) finding {
	data, err := os.ReadFile(path)
	if err != nil {
		return finding{levelWarn, "CLAUDE.md: " + path + " is there but cannot be read: " + err.Error(), ""}
	}
	begin, end := false, false
	for _, l := range strings.Split(string(data), "\n") {
		switch strings.TrimSpace(l) {
		case skills.KickstartBegin:
			begin = true
		case skills.KickstartEnd:
			end = true
		}
	}
	if begin && end {
		return finding{levelInfo, "CLAUDE.md: " + path + " still holds the kickstart block — the agent's main session asks you about the project, writes the answers into the file and removes the block", ""}
	}
	return finding{levelOK, "CLAUDE.md: " + path + " is there, without the kickstart block; unreagent leaves it as it is", ""}
}

// agentPlanFindings say what a materialization into dir does with the topic
// agents and the rules file, without doing it: not written, and why, where
// that applies; else how many files would be written, refreshed or left, and
// each file in the way by name. hold says init does not write them now.
func agentPlanFindings(s config.SkillsConfig, dir string, set skills.AgentSet, hold bool) []finding {
	claude, ok := claudeDirOf(dir)
	switch {
	case !s.AgentFiles():
		return []finding{{levelInfo, "agents: off (skills.agents is false) — the skills are written, no topic agents and no rules file", ""}}
	case !ok:
		return []finding{{levelInfo, "agents: not written — skills.dir " + dir + " is not <project>/.claude/skills, the only place the topic agents and the rules file are written next to; the skills alone are written", ""}}
	}
	p := planAgentFiles(claude, set)
	verb := "`unreagent init` writes"
	if hold {
		verb = "init writes none while the directories above are there; a start writes"
	}
	fs := []finding{{levelInfo, fmt.Sprintf("agents: %s %d topic agent files and the rules file in %s: %d new, %d refreshed, %d unchanged", verb, len(set.Agents), claude, p.absent, p.stale, p.same), ""}}
	if len(p.foreign) > 0 {
		fs = append(fs, finding{levelWarn, "agents: in the way and left alone, not written by unreagent or edited since: " + strings.Join(p.foreign, ", "), "delete one to have unreagent write it; keep yours under another name than unreagent-*"})
	}
	if p.err != nil {
		fs = append(fs, finding{levelWarn, "agents: could not be read: " + strings.ReplaceAll(p.err.Error(), "\n", "; "), ""})
	}
	return fs
}
