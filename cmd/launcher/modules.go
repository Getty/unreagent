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

// checkProjectModules logs one WARN when modules of project look unbuilt
// (unbuiltModules), before the editor starts: such an editor exits at startup
// and is restarted until the launcher gives up on it (k229). The line names
// the fix — the compile command, if the launcher offers one (compile) — and
// builds nothing itself.
//
// It stays quiet where the guess does not apply: no project, a .uproject
// that declares no module or cannot be read, and an editor that is not the
// engine's Development editor for Windows (developmentEditor) — a wrapper
// script, the demo's ping, another platform's or configuration's binary.
func checkProjectModules(project, editor string, compile bool, logger func(string)) {
	if project == "" || !developmentEditor(editor) {
		return
	}
	missing, _ := unbuiltModules(project)
	if len(missing) == 0 {
		return
	}
	fix := "With the editor stopped ('stop ue'), build them with 'c compile' in the launcher console (an agent: ue_stop, then run_command compile), then 'start ue'."
	if !compile {
		fix = "Build the project's editor target (Win64, Development) in your IDE, then 'start ue' in the launcher console; the launcher offers no compile command here."
	}
	logger("WARN project modules look unbuilt: " + strings.Join(missing, ", ") + " — no Binaries/Win64/UnrealEditor-<Name>.dll for them next to the .uproject" + moduleHeuristic +
		". An editor that misses a module's binary exits at startup, and the launcher gives up on it after a few tries. " + fix)
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
