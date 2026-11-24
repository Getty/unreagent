package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Whether a project's C++ modules are built is guessed, by `unreagent init`
// (moduleFinding) and before the editor starts (checkProjectModules), from
// one thing: for each module the .uproject declares, the Development editor
// binary Binaries/Win64/UnrealEditor-<Name>.dll next to it. A heuristic, not
// verified against a real project: a module the editor target does not build,
// or another build configuration, would be reported wrongly. What is known
// (k208, UE 5.7.4 and 5.8.4): an editor started on a .uproject that names a
// module without a binary exits 1 at startup.

// moduleHeuristic labels a statement made on that guess.
const moduleHeuristic = " (a heuristic, not verified against a real project: the Modules of the .uproject against Binaries/Win64/UnrealEditor-<Name>.dll)"

// The same guess covers the modules of the project's plugins (k251): for
// each module an enabled plugin under <project>/Plugins declares, its
// Binaries/Win64/UnrealEditor-<Name>.dll in the plugin's directory. What
// the editor does then was seen once (k226, UE 5.8.4, the UE LLM Toolkit
// added and not built): it logs that the plugin failed to load because its
// module was not found, shuts down, and ends with exit 0xC0000005 about 80 s
// after its start. Which plugins and modules count (unbuiltPluginModules)
// leans to silence where the launcher cannot tell: a module is warned about
// only when the editor is sure to want it.

// pluginModuleHeuristic labels a statement made on the plugin guess.
const pluginModuleHeuristic = " (a heuristic, not verified against a real project: the Modules of each enabled project plugin's .uplugin against Binaries/Win64/UnrealEditor-<Name>.dll in the plugin's directory)"

// pluginModuleFailure is what an editor does on a plugin module without its
// binary; pluginModuleEffect adds the timing of the one time it was seen
// (k226). The give-up pointer names no timing: it also serves the quick
// give-up of runs under 10 s, which that timing does not fit.
const (
	pluginModuleFailure = "an editor that misses an enabled plugin's module binary cannot load that plugin and shuts down"
	pluginModuleEffect  = pluginModuleFailure + " (seen once, UE 5.8.4: exit 0xC0000005 about 80 s after its start)"
)

// unbuiltModules returns the modules project declares that have no editor
// binary, in the .uproject's order, and how many it declares; 0 when the file
// or its Modules cannot be read.
func unbuiltModules(project string) (missing []string, declared int) {
	modules := projectModules(project)
	for _, m := range modules {
		if !fileExists(filepath.Join(filepath.Dir(project), "Binaries", "Win64", "UnrealEditor-"+m+".dll")) {
			missing = append(missing, m)
		}
	}
	return missing, len(modules)
}

// editorRoutes are the ways, in the running configuration, to act on the
// editor after a module-fix text: the launcher console (none when an
// interactive agent holds the launcher's window, see commandLoop in run())
// and the agent tools (none when mcp.enabled is false).
type editorRoutes struct {
	console bool
	agent   bool
}

// moduleFix is the sentence that says how to fix unbuilt modules, naming only
// the routes that exist (k247). compile is whether the launcher offers a
// compile command. atGiveUp is the editor's give-up: the editor is stopped
// already, so the fix starts at the build step. A give-up that ends the
// session passes no routes, as nothing is left to use after it.
func moduleFix(compile bool, r editorRoutes, atGiveUp bool) string {
	if compile {
		switch {
		case r.console && r.agent && atGiveUp:
			return "Build them with 'c compile' in the launcher console (an agent: run_command compile), then 'start ue'."
		case r.console && r.agent:
			return "With the editor stopped ('stop ue'), build them with 'c compile' in the launcher console (an agent: ue_stop, then run_command compile), then 'start ue'."
		case r.console && atGiveUp:
			return "Build them with 'c compile' in the launcher console, then 'start ue'."
		case r.console:
			return "With the editor stopped ('stop ue'), build them with 'c compile' in the launcher console, then 'start ue'."
		case r.agent && atGiveUp:
			return "Have the agent run run_command compile, then ue_start."
		case r.agent:
			return "Have the agent run ue_stop, run_command compile, then ue_start."
		}
		return "Build the project's editor target (Win64, Development) in your IDE, then start the launcher again."
	}
	const ide = "Build the project's editor target (Win64, Development) in your IDE, then "
	const none = "; the launcher offers no compile command here."
	switch {
	case r.console && r.agent:
		return ide + "'start ue' in the launcher console (an agent: ue_start)" + none
	case r.console:
		return ide + "'start ue' in the launcher console" + none
	case r.agent:
		return ide + "have the agent run ue_start" + none
	}
	return ide + "start the launcher again" + none
}

// unbuiltSet is what looks unbuilt: modules of the .uproject by name, and
// modules of project plugins as "<Module> (plugin <Plugin>)".
type unbuiltSet struct {
	project, plugins []string
}

func (u unbuiltSet) empty() bool { return len(u.project) == 0 && len(u.plugins) == 0 }

// modulesToFix returns the modules of project, and of its plugins as the
// editor's args leave them (unbuiltPluginModules), that look unbuilt; none
// where the guess does not apply: no project, a .uproject that declares no
// module or cannot be read, and an editor that is not the engine's
// Development editor for Windows (developmentEditor) — a wrapper script, the
// demo's ping, another platform's or configuration's binary.
func modulesToFix(project, editor string, args []string) unbuiltSet {
	if project == "" || !developmentEditor(editor) {
		return unbuiltSet{}
	}
	missing, _ := unbuiltModules(project)
	plugins, _ := unbuiltPluginModules(project, args)
	return unbuiltSet{missing, plugins}
}

// unbuiltDetail is the part of the startup WARN and of the give-up pointer
// that names the modules, the heuristic and the fix.
func unbuiltDetail(u unbuiltSet, compile bool, r editorRoutes, atGiveUp bool) string {
	var what, why []string
	if len(u.project) > 0 {
		what = append(what, strings.Join(u.project, ", ")+" — no Binaries/Win64/UnrealEditor-<Name>.dll for them next to the .uproject"+moduleHeuristic)
		why = append(why, "An editor that misses a module's binary exits at startup")
	}
	if len(u.plugins) > 0 {
		what = append(what, strings.Join(u.plugins, ", ")+" — no Binaries/Win64/UnrealEditor-<Name>.dll for them in their plugin's directory"+pluginModuleHeuristic)
		effect := pluginModuleEffect
		if atGiveUp {
			effect = pluginModuleFailure
		}
		if len(why) == 0 {
			effect = "An" + strings.TrimPrefix(effect, "an")
		}
		why = append(why, effect)
	}
	tail := ", and the launcher gives up on it after a few tries. "
	if atGiveUp {
		tail = ", which fits this failure. "
	}
	return strings.Join(what, "; ") + ". " + strings.Join(why, "; ") + tail + moduleFix(compile, r, atGiveUp)
}

// checkProjectModules logs one WARN when modules of project or of its
// plugins look unbuilt (modulesToFix), before the editor starts: such an
// editor fails and is restarted until the launcher gives up on it (k229,
// k251). The line names the fix (moduleFix) and builds nothing itself.
func checkProjectModules(project, editor string, args []string, compile bool, r editorRoutes, logger func(string)) {
	u := modulesToFix(project, editor, args)
	if u.empty() {
		return
	}
	what := "project modules"
	switch {
	case len(u.project) > 0 && len(u.plugins) > 0:
		what = "project and plugin modules"
	case len(u.plugins) > 0:
		what = "plugin modules"
	}
	logger("WARN " + what + " look unbuilt: " + unbuiltDetail(u, compile, r, false))
}

// giveUpModulesPointer is the sentence the editor's give-up lines end with
// when the project's or its plugins' modules look unbuilt now (k247, k251);
// "" otherwise. It checks again at the give-up, not at startup: the modules
// may have been built since. ends is a give-up that ends the session: no
// route is named then.
func giveUpModulesPointer(project, editor string, args []string, compile bool, r editorRoutes, ends bool) string {
	if ends {
		r = editorRoutes{}
	}
	u := modulesToFix(project, editor, args)
	if u.empty() {
		return ""
	}
	what := "The project's modules"
	switch {
	case len(u.project) > 0 && len(u.plugins) > 0:
		what = "The project's modules and its plugin modules"
	case len(u.plugins) > 0:
		what = "The project's plugin modules"
	}
	return " " + what + " still look unbuilt: " + unbuiltDetail(u, compile, r, true)
}

// developmentEditor reports whether editor is named like the engine's
// Development editor for Windows, the one that loads
// Binaries/Win64/UnrealEditor-<Name>.dll: UnrealEditor.exe or
// UnrealEditor-Cmd.exe in a directory Win64, in any case. Only the path is
// looked at, so that the answer is the same on every host.
func developmentEditor(editor string) bool {
	slash := strings.ReplaceAll(editor, `\`, "/")
	dir, name := "", slash
	if i := strings.LastIndex(slash, "/"); i >= 0 {
		dir, name = slash[:i], slash[i+1:]
	}
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		dir = dir[i+1:]
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	return strings.EqualFold(dir, "Win64") && (name == "unrealeditor" || name == "unrealeditor-cmd")
}

// projectModules are the names of the modules a .uproject declares, in its
// order; none when the file or its Modules cannot be read.
func projectModules(project string) []string {
	data, err := readUEJSON(project)
	if err != nil {
		return nil
	}
	m, err := firstMembers(data, "Modules")
	if err != nil {
		return nil
	}
	var raws []json.RawMessage
	if json.Unmarshal(bytes.TrimSpace(m["Modules"]), &raws) != nil {
		return nil
	}
	var names []string
	for _, raw := range raws {
		mod, err := firstMembers(raw, "Name")
		if err != nil {
			continue
		}
		if name, ok := jsonString(mod["Name"]); ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

// pluginModulesUnchecked reports editor args that change which project
// plugins the editor enables in a way the launcher does not model:
// -NoEnginePlugins (no project plugin is enabled by default or through the
// .uproject) and any -DisablePlugins= (see checkPluginEngineVersions).
func pluginModulesUnchecked(args []string) bool {
	return hasEditorSwitch(args, "NoEnginePlugins") || disablesPlugins(args)
}

// unbuiltPluginModules returns the modules of project's plugins that the
// editor loads and that have no editor binary in their plugin's
// Binaries/Win64, as "<Module> (plugin <Plugin>)" in discovery order, and
// how many modules it checked. Which plugins: those
// checkPluginEngineVersions would check — found under <project>/Plugins,
// one name only once, no plugin extension, enabled by the .uproject or by
// default (pluginEnabled) — that also have a Source directory: a plugin
// without one ships its binaries and cannot be built here. Nothing when
// args carry what pluginModulesUnchecked names. Which modules:
// editorPluginModules.
func unbuiltPluginModules(project string, args []string) (missing []string, checked int) {
	if project == "" || pluginModulesUnchecked(args) {
		return nil, 0
	}
	plugins := findProjectPlugins(filepath.Join(filepath.Dir(project), "Plugins"))
	found := map[string]int{} // plugin names compare without regard to case
	for _, path := range plugins {
		found[strings.ToLower(pluginName(path))]++
	}
	switches := projectPluginSwitches(project)
	for _, path := range plugins {
		name := pluginName(path)
		if found[strings.ToLower(name)] > 1 {
			continue
		}
		_, enabledByDefault, ok := readPluginDescriptor(path)
		if !ok || !pluginEnabled(name, enabledByDefault, switches) {
			continue
		}
		dir := filepath.Dir(path)
		if fi, err := os.Stat(filepath.Join(dir, "Source")); err != nil || !fi.IsDir() {
			continue
		}
		for _, m := range editorPluginModules(path) {
			checked++
			if !fileExists(filepath.Join(dir, "Binaries", "Win64", "UnrealEditor-"+m+".dll")) {
				missing = append(missing, m+" (plugin "+name+")")
			}
		}
	}
	return missing, checked
}

// editorModuleTypes are the module Types the Win64 Development editor is
// taken to build and load. Checked against UE 5.8.4 ModuleDescriptor.cpp
// (lines 603-627 and 683-775; read, not run): the editor builds and loads all
// nine. ServerOnly, ClientOnly and ClientOnlyNoCommandlet are built and
// loaded by the editor too but are left out on purpose, as are CookedOnly,
// Program and any other type: a module of such a type is never warned about,
// so the list can only miss a warning, never give a false one.
var editorModuleTypes = []string{
	"Runtime", "RuntimeNoCommandlet", "RuntimeAndProgram",
	"Editor", "EditorNoCommandlet", "EditorAndProgram",
	"Developer", "DeveloperTool", "UncookedOnly",
}

// moduleListLimits are a module descriptor's allow and deny lists, each
// also under its deprecated Whitelist/Blacklist name, and the value that
// stands for the Win64 Development editor in them.
var moduleListLimits = []struct {
	allow, deny [2]string
	editor      string
}{
	{[2]string{"PlatformAllowList", "WhitelistPlatforms"}, [2]string{"PlatformDenyList", "BlacklistPlatforms"}, "Win64"},
	{[2]string{"TargetAllowList", "WhitelistTargets"}, [2]string{"TargetDenyList", "BlacklistTargets"}, "Editor"},
	{[2]string{"TargetConfigurationAllowList", "WhitelistTargetConfigurations"}, [2]string{"TargetConfigurationDenyList", "BlacklistTargetConfigurations"}, "Development"},
}

// editorPluginModules are the names of the modules the .uplugin at path
// declares that the Win64 Development editor is sure to load at startup;
// none for a plugin marked ExplicitlyLoaded (or with a non-boolean one), or
// a file that cannot be read. A module counts only with a Name, a Type among
// editorModuleTypes, a LoadingPhase other than None (absent is the
// default), and allow and deny lists that let the editor in: an allow list
// must name it, a deny list must not, and a list that is no list of strings
// keeps the module out; HasExplicitPlatforms, when true, needs a platform
// allow list. Names, types and values compare without regard to case. What
// the engine makes of anything else is not modelled, so it never warns.
func editorPluginModules(path string) []string {
	data, err := readUEJSON(path)
	if err != nil {
		return nil
	}
	m, err := firstMembers(data, "Modules", "ExplicitlyLoaded")
	if err != nil {
		return nil
	}
	if explicit, isBool := boolMember(m, "ExplicitlyLoaded", false); !isBool || explicit {
		return nil
	}
	var raws []json.RawMessage
	if json.Unmarshal(bytes.TrimSpace(m["Modules"]), &raws) != nil {
		return nil
	}
	fields := []string{"Name", "Type", "LoadingPhase", "HasExplicitPlatforms"}
	for _, l := range moduleListLimits {
		fields = append(fields, l.allow[0], l.allow[1], l.deny[0], l.deny[1])
	}
	var names []string
	for _, raw := range raws {
		mod, err := firstMembers(raw, fields...)
		if err != nil {
			continue
		}
		name, ok := jsonString(mod["Name"])
		if !ok || name == "" {
			continue
		}
		if typ, ok := jsonString(mod["Type"]); !ok || !containsFold(editorModuleTypes, typ) {
			continue
		}
		if raw, present := mod["LoadingPhase"]; present {
			if phase, ok := jsonString(raw); !ok || strings.EqualFold(phase, "None") {
				continue
			}
		}
		if moduleLeavesEditorOut(mod) {
			continue
		}
		names = append(names, name)
	}
	return names
}

// moduleLeavesEditorOut reports whether the lists of a module descriptor's
// members mod may leave the Win64 Development editor out (see
// editorPluginModules).
func moduleLeavesEditorOut(mod map[string]json.RawMessage) bool {
	explicit, isBool := boolMember(mod, "HasExplicitPlatforms", false)
	if !isBool {
		return true
	}
	platformAllow := false
	for i, l := range moduleListLimits {
		for _, field := range l.allow {
			raw, present := mod[field]
			if !present {
				continue
			}
			if i == 0 {
				platformAllow = true
			}
			if list, ok := jsonStrings(raw); !ok || !containsFold(list, l.editor) {
				return true
			}
		}
		for _, field := range l.deny {
			raw, present := mod[field]
			if !present {
				continue
			}
			if list, ok := jsonStrings(raw); !ok || containsFold(list, l.editor) {
				return true
			}
		}
	}
	return explicit && !platformAllow
}

// jsonStrings is raw as a list of strings if it is a JSON array of strings.
func jsonStrings(raw json.RawMessage) (list []string, ok bool) {
	return list, json.Unmarshal(raw, &list) == nil
}

// containsFold reports whether list holds s, compared without regard to case.
func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
