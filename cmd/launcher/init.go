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
)

// initHeader heads the unreagent.yaml `unreagent init` writes.
const initHeader = "# Written by `unreagent init`: the built-in defaults, which a start without\n" +
	"# this file runs on as well. Edit freely — unreagent.example.yaml documents\n" +
	"# every option. Machine-specific values (engineRoot, a token) belong in a\n" +
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

// initCommand is `unreagent init`: next to the one .uproject it writes the
// built-in defaults (config.DefaultYAML) as unreagent.yaml, then prints a
// preflight of what a start needs. It never overwrites: with a config file
// there already (unreagent.yaml or unreagent.yml, or the one -config names)
// it writes nothing and prints the preflight for that config; -check prints
// the preflight without writing where there is none.
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
// way it ends on what init wrote, the file or nothing.
func initCommand(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Print the preflight for this unreagent.yaml (default: the one next to the executable); init never writes to a -config path")
	check := fs.Bool("check", false, "Write nothing, only print the preflight (automatic when a config file exists)")
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
	return initRun(func() (*config.Config, config.Info, error) { return config.LoadForPreflight(*cfgPath, "") }, *check, out)
}

// initRun is initCommand behind its flags: load is config.LoadForPreflight
// for the path or directory in question.
func initRun(load func() (*config.Config, config.Info, error), check bool, out io.Writer) error {
	cfg, info, err := load()
	if err != nil {
		return fmt.Errorf("%w (unreagent init wrote nothing)", err)
	}
	wrote := "nothing"
	switch {
	case !info.Defaults:
		fmt.Fprintf(out, "Config: %s exists and is left as it is. Preflight only.\n", info.ConfigPath)
	case check:
		fmt.Fprintf(out, "No config file at %s; -check, so none is written. A start runs on the built-in defaults.\n", info.ConfigPath)
	default:
		doc, err := writeDefaultConfig(info.ConfigPath)
		if err != nil {
			return err
		}
		// cfg stays the one loaded: the written document decodes to the same
		// configuration (config.TestDefaultYAMLRoundTrips).
		fmt.Fprintf(out, "Wrote %s:\n\n%s\n\n", info.ConfigPath, indent(string(doc), "    "))
		wrote = info.ConfigPath
	}

	findings := preflight(cfg, info)
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
		return fmt.Errorf("preflight: %d blocking finding(s), see [BLOCKING] above (unreagent init wrote %s)", blocking, wrote)
	}
	return nil
}

// writeDefaultConfig writes the built-in defaults for the project directory
// of path to path, which must not exist: the file is created exclusively, so
// nothing that appeared since the load is overwritten either. It returns the
// document without the header comment.
func writeDefaultConfig(path string) ([]byte, error) {
	doc, _, _, err := config.DefaultYAML(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("unreagent init does not overwrite, and could not create %s: %w", path, err)
	}
	_, err = f.Write(append([]byte(initHeader), doc...))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("writing %s: %w", path, err)
	}
	return doc, nil
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
	doc, bridges, searchWarnings, err := config.DefaultYAML(projectDir)
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
	fix := "build the project's editor target (" + info.ProjectName + "Editor, Win64, Development) in your IDE"
	if c, ok := cfg.Commands["compile"]; ok && c.Command != "" && !withoutEngine(info, strings.Join(append([]string{c.Command}, c.Args...), " ")) {
		fix += ", or run what the launcher's compile command runs:\n    " + strings.Join(append([]string{c.Command}, c.Args...), " ")
	}
	return finding{levelWarn, "project modules: " + strings.Join(missing, ", ") + " look unbuilt — no Binaries/Win64/UnrealEditor-<Name>.dll for them" + moduleHeuristic +
		". An editor that misses a module's binary exits at startup, and the launcher gives up on it after a few tries", fix}
}
