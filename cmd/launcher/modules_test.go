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
		checkProjectModules(project, editor, nil, compile, editorRoutes{console: true, agent: true}, func(line string) { lines = append(lines, line) })
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

// pluginTree is a project Game with one plugin Kit under Plugins/Kit: its
// .uplugin, a Source/ directory when source is set, and the editor binary of
// each module in built. uproject replaces the .uproject (no Modules by
// default, so only the plugin can warn).
type pluginTree struct {
	uproject, uplugin string
	source            bool
	built             []string
}

func (p pluginTree) make(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	project := filepath.Join(dir, "Game.uproject")
	uproject := p.uproject
	if uproject == "" {
		uproject = `{"FileVersion": 3}`
	}
	mustWriteFile(t, project, uproject)
	plugin := filepath.Join(dir, "Plugins", "Kit")
	mustWriteFile(t, filepath.Join(plugin, "Kit.uplugin"), p.uplugin)
	if p.source {
		mustWriteFile(t, filepath.Join(plugin, "Source", "Kit", "Kit.Build.cs"), "")
	}
	for _, m := range p.built {
		mustWriteFile(t, filepath.Join(plugin, "Binaries", "Win64", "UnrealEditor-"+m+".dll"), "")
	}
	return project
}

// k251: a module of an enabled project plugin with source is checked like a
// project module (its UnrealEditor-<Name>.dll, here in the plugin's
// Binaries/Win64); what the editor would not load, or what the launcher
// cannot build or cannot tell, is never warned about.
func TestUnbuiltPluginModules(t *testing.T) {
	mod := func(fields string) string {
		return `{"Modules": [{"Name": "Kit", "Type": "Runtime", "LoadingPhase": "Default"` + fields + `}]}`
	}
	typed := func(typ string) string { return `{"Modules": [{"Name": "Kit", "Type": "` + typ + `"}]}` }
	warn := []struct {
		name string
		tree pluginTree
		args []string
	}{
		{"enabled by default", pluginTree{uplugin: mod(""), source: true}, nil},
		{"EnabledByDefault false, the .uproject enables it",
			pluginTree{uplugin: `{"EnabledByDefault": false, "Modules": [{"Name": "Kit", "Type": "Runtime"}]}`, source: true,
				uproject: `{"Plugins": [{"Name": "kit", "Enabled": true}]}`}, nil},
		{"PlatformAllowList with Win64", pluginTree{uplugin: mod(`, "PlatformAllowList": ["Linux", "win64"]`), source: true}, nil},
		{"HasExplicitPlatforms with Win64 allowed", pluginTree{uplugin: mod(`, "HasExplicitPlatforms": true, "PlatformAllowList": ["Win64"]`), source: true}, nil},
		{"TargetAllowList with Editor", pluginTree{uplugin: mod(`, "TargetAllowList": ["Editor"]`), source: true}, nil},
		{"deny lists without the editor", pluginTree{uplugin: mod(`, "PlatformDenyList": ["Linux"], "TargetDenyList": ["Game"], "TargetConfigurationDenyList": ["Shipping"]`), source: true}, nil},
		{"TargetConfigurationAllowList with Development", pluginTree{uplugin: mod(`, "TargetConfigurationAllowList": ["Development"]`), source: true}, nil},
		{"one of two built", pluginTree{uplugin: `{"Modules": [{"Name": "Kit", "Type": "Runtime"}, {"Name": "KitEditor", "Type": "Editor"}]}`, source: true, built: []string{"KitEditor"}}, nil},
		{"unrelated editor args", pluginTree{uplugin: mod(""), source: true}, []string{"-unattended", "-EnablePlugins=Other"}},
	}
	// Spelled out, not editorModuleTypes: a type dropped from that list must
	// fail here.
	for _, typ := range []string{"Runtime", "RuntimeNoCommandlet", "RuntimeAndProgram", "Editor", "EditorNoCommandlet",
		"EditorAndProgram", "Developer", "DeveloperTool", "UncookedOnly", "editor"} {
		warn = append(warn, struct {
			name string
			tree pluginTree
			args []string
		}{"type " + typ, pluginTree{uplugin: typed(typ), source: true}, nil})
	}
	for _, tc := range warn {
		t.Run("warns: "+tc.name, func(t *testing.T) {
			missing, checked := unbuiltPluginModules(tc.tree.make(t), tc.args)
			if len(missing) != 1 || missing[0] != "Kit (plugin Kit)" || checked == 0 {
				t.Errorf("missing = %q, checked = %d; want [Kit (plugin Kit)]", missing, checked)
			}
		})
	}

	for name, tc := range map[string]struct {
		tree pluginTree
		args []string
	}{
		"built":                                             {pluginTree{uplugin: mod(""), source: true, built: []string{"Kit"}}, nil},
		"binary-only, no Source":                            {pluginTree{uplugin: mod("")}, nil},
		"content-only, no Modules":                          {pluginTree{uplugin: `{"FriendlyName": "Kit"}`, source: true}, nil},
		"Modules not a list":                                {pluginTree{uplugin: `{"Modules": "Kit"}`, source: true}, nil},
		"disabled by default":                               {pluginTree{uplugin: `{"EnabledByDefault": false, "Modules": [{"Name": "Kit", "Type": "Runtime"}]}`, source: true}, nil},
		"disabled by the .uproject":                         {pluginTree{uplugin: mod(""), source: true, uproject: `{"Plugins": [{"Name": "Kit", "Enabled": false}]}`}, nil},
		"limited by the .uproject":                          {pluginTree{uplugin: mod(""), source: true, uproject: `{"Plugins": [{"Name": "Kit", "Enabled": true, "Optional": true}]}`}, nil},
		"explicitly loaded":                                 {pluginTree{uplugin: `{"ExplicitlyLoaded": true, "Modules": [{"Name": "Kit", "Type": "Runtime"}]}`, source: true}, nil},
		"ExplicitlyLoaded no boolean":                       {pluginTree{uplugin: `{"ExplicitlyLoaded": "yes", "Modules": [{"Name": "Kit", "Type": "Runtime"}]}`, source: true}, nil},
		"a plugin extension":                                {pluginTree{uplugin: `{"bIsPluginExtension": true, "Modules": [{"Name": "Kit", "Type": "Runtime"}]}`, source: true}, nil},
		"unreadable .uplugin":                               {pluginTree{uplugin: `not json`, source: true}, nil},
		"no Type":                                           {pluginTree{uplugin: `{"Modules": [{"Name": "Kit"}]}`, source: true}, nil},
		"type Program":                                      {pluginTree{uplugin: typed("Program"), source: true}, nil},
		"type CookedOnly":                                   {pluginTree{uplugin: typed("CookedOnly"), source: true}, nil},
		"type ClientOnly":                                   {pluginTree{uplugin: typed("ClientOnly"), source: true}, nil},
		"type ServerOnly":                                   {pluginTree{uplugin: typed("ServerOnly"), source: true}, nil},
		"an unknown type":                                   {pluginTree{uplugin: typed("Sometimes"), source: true}, nil},
		"LoadingPhase None":                                 {pluginTree{uplugin: `{"Modules": [{"Name": "Kit", "Type": "Runtime", "LoadingPhase": "none"}]}`, source: true}, nil},
		"LoadingPhase no string":                            {pluginTree{uplugin: `{"Modules": [{"Name": "Kit", "Type": "Runtime", "LoadingPhase": 3}]}`, source: true}, nil},
		"no Name":                                           {pluginTree{uplugin: `{"Modules": [{"Type": "Runtime"}]}`, source: true}, nil},
		"PlatformAllowList without Win64":                   {pluginTree{uplugin: mod(`, "PlatformAllowList": ["Linux"]`), source: true}, nil},
		"WhitelistPlatforms without Win64":                  {pluginTree{uplugin: mod(`, "WhitelistPlatforms": ["Mac"]`), source: true}, nil},
		"PlatformDenyList with Win64":                       {pluginTree{uplugin: mod(`, "PlatformDenyList": ["Win64"]`), source: true}, nil},
		"BlacklistPlatforms with Win64":                     {pluginTree{uplugin: mod(`, "BlacklistPlatforms": ["Win64"]`), source: true}, nil},
		"PlatformAllowList not a list":                      {pluginTree{uplugin: mod(`, "PlatformAllowList": "Win64"`), source: true}, nil},
		"HasExplicitPlatforms without a list":               {pluginTree{uplugin: mod(`, "HasExplicitPlatforms": true`), source: true}, nil},
		"HasExplicitPlatforms no boolean":                   {pluginTree{uplugin: mod(`, "HasExplicitPlatforms": "true", "PlatformAllowList": ["Win64"]`), source: true}, nil},
		"TargetAllowList without Editor":                    {pluginTree{uplugin: mod(`, "TargetAllowList": ["Game"]`), source: true}, nil},
		"WhitelistTargets without Editor":                   {pluginTree{uplugin: mod(`, "WhitelistTargets": ["Client"]`), source: true}, nil},
		"TargetDenyList with Editor":                        {pluginTree{uplugin: mod(`, "TargetDenyList": ["Editor"]`), source: true}, nil},
		"BlacklistTargets with Editor":                      {pluginTree{uplugin: mod(`, "BlacklistTargets": ["Editor"]`), source: true}, nil},
		"TargetConfigurationAllowList without Development":  {pluginTree{uplugin: mod(`, "TargetConfigurationAllowList": ["Shipping"]`), source: true}, nil},
		"WhitelistTargetConfigurations without Development": {pluginTree{uplugin: mod(`, "WhitelistTargetConfigurations": ["Debug"]`), source: true}, nil},
		"TargetConfigurationDenyList with Development":      {pluginTree{uplugin: mod(`, "TargetConfigurationDenyList": ["Development"]`), source: true}, nil},
		"BlacklistTargetConfigurations with Development":    {pluginTree{uplugin: mod(`, "BlacklistTargetConfigurations": ["Development"]`), source: true}, nil},
		"HasExplicitPlatforms, only a target list":          {pluginTree{uplugin: mod(`, "HasExplicitPlatforms": true, "TargetAllowList": ["Editor"]`), source: true}, nil},
		"TargetDenyList not a list":                         {pluginTree{uplugin: mod(`, "TargetDenyList": "Game"`), source: true}, nil},
		"-NoEnginePlugins":                                  {pluginTree{uplugin: mod(""), source: true}, []string{"-NoEnginePlugins"}},
		"-DisablePlugins=":                                  {pluginTree{uplugin: mod(""), source: true}, []string{"-DisablePlugins=Other"}},
	} {
		t.Run("quiet: "+name, func(t *testing.T) {
			if missing, _ := unbuiltPluginModules(tc.tree.make(t), tc.args); len(missing) != 0 {
				t.Errorf("missing = %q, want none", missing)
			}
		})
	}

	t.Run("quiet: the same plugin name twice", func(t *testing.T) {
		project := pluginTree{uplugin: mod(""), source: true}.make(t)
		other := filepath.Join(filepath.Dir(project), "Plugins", "Vendor", "Kit")
		mustWriteFile(t, filepath.Join(other, "Kit.uplugin"), mod(""))
		mustWriteFile(t, filepath.Join(other, "Source", "Kit", "Kit.Build.cs"), "")
		if missing, _ := unbuiltPluginModules(project, nil); len(missing) != 0 {
			t.Errorf("missing = %q, want none: the engine keeps one of the two, the launcher cannot tell which", missing)
		}
	})
	t.Run("no project", func(t *testing.T) {
		if missing, checked := unbuiltPluginModules("", nil); len(missing) != 0 || checked != 0 {
			t.Errorf("missing = %q, checked = %d, want none", missing, checked)
		}
	})
}

// k251: the startup WARN and the give-up pointer name unbuilt plugin modules
// as k229 and k247 name project modules, with what the editor was seen to do
// then (k226) and the same fix; the k229 text for project modules alone is
// unchanged.
func TestCheckProjectModulesNamesPluginModules(t *testing.T) {
	const engineEditor = "C:/UE_5.8/Engine/Binaries/Win64/UnrealEditor.exe"
	const kit = `{"Modules": [{"Name": "Kit", "Type": "Runtime"}]}`
	check := func(project, editor string, args []string) []string {
		var lines []string
		checkProjectModules(project, editor, args, true, editorRoutes{console: true, agent: true}, func(line string) { lines = append(lines, line) })
		return lines
	}

	t.Run("a plugin module", func(t *testing.T) {
		lines := check(pluginTree{uplugin: kit, source: true}.make(t), engineEditor, nil)
		if len(lines) != 1 {
			t.Fatalf("lines = %q, want one WARN", lines)
		}
		wantAll(t, lines[0],
			"WARN plugin modules look unbuilt: Kit (plugin Kit) — no Binaries/Win64/UnrealEditor-<Name>.dll for them in their plugin's directory",
			"a heuristic, not verified against a real project: the Modules of each enabled project plugin's .uplugin",
			"An editor that misses an enabled plugin's module binary cannot load that plugin and shuts down (seen once, UE 5.8.4: exit 0xC0000005 about 80 s after its start), and the launcher gives up on it after a few tries.",
			"'c compile' in the launcher console", "'stop ue'", "'start ue'")
		wantNone(t, lines[0], "\n", "next to the .uproject", "exits at startup")
	})
	t.Run("project and plugin modules", func(t *testing.T) {
		project := pluginTree{uplugin: kit, source: true, uproject: `{"Modules": [{"Name": "Game", "Type": "Runtime"}]}`}.make(t)
		lines := check(project, engineEditor, nil)
		if len(lines) != 1 {
			t.Fatalf("lines = %q, want one WARN", lines)
		}
		wantAll(t, lines[0],
			"WARN project and plugin modules look unbuilt: Game — no Binaries/Win64/UnrealEditor-<Name>.dll for them next to the .uproject",
			"; Kit (plugin Kit) — no Binaries/Win64/UnrealEditor-<Name>.dll for them in their plugin's directory",
			"An editor that misses a module's binary exits at startup; an editor that misses an enabled plugin's module binary cannot load that plugin and shuts down",
			"'c compile' in the launcher console")
	})
	for name, tc := range map[string]struct {
		editor string
		args   []string
	}{
		"the demo's ping":  {"ping", nil},
		"a wrapper script": {"C:/tools/start-editor.bat", nil},
		"-NoEnginePlugins": {engineEditor, []string{"-NoEnginePlugins"}},
	} {
		t.Run("quiet: "+name, func(t *testing.T) {
			if lines := check(pluginTree{uplugin: kit, source: true}.make(t), tc.editor, tc.args); len(lines) != 0 {
				t.Errorf("lines = %q, want none", lines)
			}
		})
	}
	t.Run("give-up pointer", func(t *testing.T) {
		project := pluginTree{uplugin: kit, source: true}.make(t)
		got := giveUpModulesPointer(project, engineEditor, nil, true, editorRoutes{console: true}, false)
		wantAll(t, got,
			" The project's plugin modules still look unbuilt: Kit (plugin Kit) — ",
			"cannot load that plugin and shuts down, which fits this failure. Build them with 'c compile' in the launcher console, then 'start ue'.")
		// The pointer serves the quick give-up (runs under 10 s) as well, which
		// the one seen run's timing does not fit, so it names no timing.
		if strings.Contains(got, "80 s") || strings.Contains(got, "0xC0000005") {
			t.Errorf("give-up pointer %q names the seen run's timing, which does not fit the quick give-up", got)
		}
		if got := giveUpModulesPointer(project, engineEditor, []string{"-NoEnginePlugins"}, true, editorRoutes{console: true}, false); got != "" {
			t.Errorf("pointer with -NoEnginePlugins = %q, want none", got)
		}
	})
}
