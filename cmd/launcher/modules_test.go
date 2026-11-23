package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectModules(t *testing.T) {
	dir := t.TempDir()
	for content, want := range map[string]string{
		`{"Modules": [{"Name": "A"}, {"name": "B", "Type": "Editor"}, {"Type": "Runtime"}, 7,]}`: "A,B",
		`{"Modules": []}`:  "",
		`{}`:               "",
		`{"Modules": "x"}`: "",
		`not json`:         "",
	} {
		path := filepath.Join(dir, "P.uproject")
		mustWriteFile(t, path, content)
		if got := strings.Join(projectModules(path), ","); got != want {
			t.Errorf("projectModules(%s) = %q, want %q", content, got, want)
		}
	}
}

// k229: one WARN when a module the .uproject declares has no editor binary,
// naming the modules, the heuristic and the compile command; nothing where
// the guess does not apply.
func TestCheckProjectModules(t *testing.T) {
	const engineEditor = "C:/UE_5.8/Engine/Binaries/Win64/UnrealEditor.exe"
	tree := func(t *testing.T, uproject string, built ...string) string {
		t.Helper()
		dir := t.TempDir()
		project := filepath.Join(dir, "Game.uproject")
		mustWriteFile(t, project, uproject)
		for _, m := range built {
			mustWriteFile(t, filepath.Join(dir, "Binaries", "Win64", "UnrealEditor-"+m+".dll"), "")
		}
		return project
	}
	check := func(project, editor string, compile bool) []string {
		var lines []string
		checkProjectModules(project, editor, compile, editorRoutes{console: true, agent: true}, func(line string) { lines = append(lines, line) })
		return lines
	}
	const two = `{"Modules": [{"Name": "Game"}, {"Name": "GameEditor", "Type": "Editor"}]}`

	t.Run("unbuilt", func(t *testing.T) {
		lines := check(tree(t, two, "GameEditor"), engineEditor, true)
		if len(lines) != 1 {
			t.Fatalf("lines = %q, want one WARN", lines)
		}
		for _, want := range []string{"WARN project modules look unbuilt: Game — no Binaries/Win64/UnrealEditor-<Name>.dll",
			"a heuristic, not verified against a real project", "exits at startup",
			"'c compile' in the launcher console", "run_command compile", "'stop ue'", "'start ue'"} {
			if !strings.Contains(lines[0], want) {
				t.Errorf("WARN does not mention %q:\n%s", want, lines[0])
			}
		}
		if strings.Contains(lines[0], "\n") {
			t.Errorf("WARN has more than one line, and stderr would get only the first:\n%s", lines[0])
		}
	})
	t.Run("all unbuilt, no compile command", func(t *testing.T) {
		lines := check(tree(t, two), `C:\UE\Engine\Binaries\Win64\UnrealEditor-Cmd.exe`, false)
		if len(lines) != 1 || !strings.Contains(lines[0], "look unbuilt: Game, GameEditor — ") || !strings.Contains(lines[0], "in your IDE") || !strings.Contains(lines[0], "offers no compile command") {
			t.Errorf("lines = %q, want one WARN naming both modules and the IDE", lines)
		}
		if len(lines) == 1 && strings.Contains(lines[0], "c compile") {
			t.Errorf("WARN names a compile command the launcher does not offer:\n%s", lines[0])
		}
	})
	for name, tc := range map[string]struct{ project, editor string }{
		"built":                 {tree(t, two, "Game", "GameEditor"), engineEditor},
		"no project":            {"", engineEditor},
		"no modules":            {tree(t, `{"Modules": []}`), engineEditor},
		"unreadable .uproject":  {tree(t, `not json`), engineEditor},
		"missing .uproject":     {filepath.Join(t.TempDir(), "Gone.uproject"), engineEditor},
		"the demo's ping":       {tree(t, two), "ping"},
		"a wrapper script":      {tree(t, two), "C:/tools/start-editor.bat"},
		"another platform":      {tree(t, two), "/opt/UE/Engine/Binaries/Linux/UnrealEditor"},
		"another configuration": {tree(t, two), "C:/UE/Engine/Binaries/Win64/UnrealEditor-Win64-DebugGame.exe"},
		"a bare name on PATH":   {tree(t, two), "UnrealEditor.exe"},
	} {
		t.Run("quiet: "+name, func(t *testing.T) {
			if lines := check(tc.project, tc.editor, true); len(lines) != 0 {
				t.Errorf("lines = %q, want none", lines)
			}
		})
	}
}

// k247: the fix text names only the routes the running configuration has —
// the launcher console unless an interactive agent holds the window, the
// agent tools only with mcp.enabled. At the give-up the editor is stopped
// already, so the fix starts at the build step.
func TestModuleFixNamesOnlyExistingRoutes(t *testing.T) {
	for _, tc := range []struct {
		compile  bool
		r        editorRoutes
		atGiveUp bool
		want     []string
		not      []string
	}{
		{true, editorRoutes{true, true}, false, []string{"'c compile' in the launcher console", "run_command compile", "'stop ue'", "ue_stop", "'start ue'"}, nil},
		{true, editorRoutes{true, true}, true, []string{"'c compile' in the launcher console", "run_command compile", "'start ue'"}, []string{"'stop ue'", "ue_stop"}},
		{true, editorRoutes{true, false}, false, []string{"'c compile' in the launcher console", "'stop ue'", "'start ue'"}, []string{"run_command", "ue_stop", "an agent"}},
		{true, editorRoutes{true, false}, true, []string{"'c compile' in the launcher console", "'start ue'"}, []string{"run_command", "stop", "an agent"}},
		{true, editorRoutes{false, true}, false, []string{"Have the agent run ue_stop, run_command compile, then ue_start."}, []string{"console", "'c compile'", "'start ue'"}},
		{true, editorRoutes{false, true}, true, []string{"Have the agent run run_command compile, then ue_start."}, []string{"console", "ue_stop", "'start ue'"}},
		{true, editorRoutes{false, false}, false, []string{"IDE", "start the launcher again"}, []string{"'c compile'", "run_command", "'start ue'", "ue_start"}},
		{true, editorRoutes{false, false}, true, []string{"IDE", "start the launcher again"}, []string{"'c compile'", "run_command", "'start ue'", "ue_start"}},
		{false, editorRoutes{true, true}, false, []string{"IDE", "'start ue' in the launcher console", "ue_start", "no compile command"}, nil},
		{false, editorRoutes{true, false}, false, []string{"IDE", "'start ue' in the launcher console"}, []string{"ue_start", "an agent"}},
		{false, editorRoutes{false, true}, false, []string{"IDE", "have the agent run ue_start"}, []string{"console", "'start ue'"}},
		{false, editorRoutes{false, false}, false, []string{"IDE", "start the launcher again"}, []string{"console", "ue_start", "'start ue'"}},
	} {
		got := moduleFix(tc.compile, tc.r, tc.atGiveUp)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("moduleFix(%v, %+v, %v) = %q, want it to mention %q", tc.compile, tc.r, tc.atGiveUp, got, w)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(got, n) {
				t.Errorf("moduleFix(%v, %+v, %v) = %q, must not mention %q", tc.compile, tc.r, tc.atGiveUp, got, n)
			}
		}
	}
}
