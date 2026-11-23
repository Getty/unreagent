package main

import (
	"bytes"
	"encoding/json"
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

// modulesToFix returns the modules of project that look unbuilt (see
// unbuiltModules), none where the guess does not apply: no project, a
// .uproject that declares no module or cannot be read, and an editor that is
// not the engine's Development editor for Windows (developmentEditor) — a
// wrapper script, the demo's ping, another platform's or configuration's
// binary.
func modulesToFix(project, editor string) []string {
	if project == "" || !developmentEditor(editor) {
		return nil
	}
	missing, _ := unbuiltModules(project)
	return missing
}

// unbuiltDetail is the part of the startup WARN and of the give-up pointer
// that names the modules, the heuristic and the fix.
func unbuiltDetail(missing []string, compile bool, r editorRoutes, atGiveUp bool) string {
	why := "An editor that misses a module's binary exits at startup, and the launcher gives up on it after a few tries. "
	if atGiveUp {
		why = "An editor that misses a module's binary exits at startup, which fits this failure. "
	}
	return strings.Join(missing, ", ") + " — no Binaries/Win64/UnrealEditor-<Name>.dll for them next to the .uproject" + moduleHeuristic +
		". " + why + moduleFix(compile, r, atGiveUp)
}

// checkProjectModules logs one WARN when modules of project look unbuilt
// (modulesToFix), before the editor starts: such an editor exits at startup
// and is restarted until the launcher gives up on it (k229). The line names
// the fix (moduleFix) and builds nothing itself.
func checkProjectModules(project, editor string, compile bool, r editorRoutes, logger func(string)) {
	if missing := modulesToFix(project, editor); len(missing) > 0 {
		logger("WARN project modules look unbuilt: " + unbuiltDetail(missing, compile, r, false))
	}
}

// giveUpModulesPointer is the sentence the editor's give-up lines end with
// when the project's modules look unbuilt now (k247); "" otherwise. It checks
// again at the give-up, not at startup: the modules may have been built
// since. ends is a give-up that ends the session: no route is named then.
func giveUpModulesPointer(project, editor string, compile bool, r editorRoutes, ends bool) string {
	if ends {
		r = editorRoutes{}
	}
	missing := modulesToFix(project, editor)
	if len(missing) == 0 {
		return ""
	}
	return " The project's modules still look unbuilt: " + unbuiltDetail(missing, compile, r, true)
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
