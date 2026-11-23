package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeBridge creates the toolkit's bridge entry point below dir/Plugins/plugin
// and returns its path with forward slashes.
func writeBridge(t *testing.T, dir, plugin string) string {
	t.Helper()
	p := filepath.Join(dir, "Plugins", filepath.FromSlash(plugin), "Resources", "mcp-bridge", "index.js")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("// bridge\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(p)
}

// toolkitArgs returns the args of the ue-llm-toolkit extra server, nil if
// there is no such server.
func toolkitArgs(c *Config) []interface{} {
	args, _ := c.MCP.ExtraServers[ToolkitServerName]["args"].([]interface{})
	return args
}

// k227: no config file next to the executable, exactly one .uproject there —
// the launcher runs on the built-in defaults instead of failing.
func TestMissingConfigRunsOnDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UE_ROOT", filepath.Join(dir, "engine"))
	project := writeUProject(t, dir, "Game.uproject")

	c, info, err := load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Defaults {
		t.Error("Info.Defaults is false on a start without a config file")
	}
	if want := filepath.Join(dir, "unreagent.yaml"); info.ConfigPath != want {
		t.Errorf("ConfigPath = %q, want %q (where the config would be)", info.ConfigPath, want)
	}
	if info.Project != project || info.ProjectName != "Game" || c.Unreal.Project != project {
		t.Errorf("project = %q (%q), unreal.project = %q; want %q", info.Project, info.ProjectName, c.Unreal.Project, project)
	}
	if !c.Agent.Enabled || !c.Agent.ClaudeIntegration || !c.MCP.Enabled {
		t.Errorf("agent.enabled=%v agent.claudeIntegration=%v mcp.enabled=%v; want all true",
			c.Agent.Enabled, c.Agent.ClaudeIntegration, c.MCP.Enabled)
	}
	if c.Permissions.Enabled {
		t.Error("permissions.enabled is true on the defaults path; want the built-in default, off")
	}
	if len(c.MCP.ExtraServers) != 0 {
		t.Errorf("extraServers = %v without a toolkit plugin; want none", c.MCP.ExtraServers)
	}
	// The rest went through applyDefaults and substitute as for a config file.
	if c.Agent.Command != "claude" || c.MCP.Address != DefaultMCPAddress {
		t.Errorf("agent.command = %q, mcp.address = %q; want the defaults", c.Agent.Command, c.MCP.Address)
	}
	if want := filepath.ToSlash(filepath.Join(dir, "engine")) + "/Engine/Binaries/Win64/UnrealEditor.exe"; c.Unreal.Editor != want {
		t.Errorf("unreal.editor = %q, want %q", c.Unreal.Editor, want)
	}
	if _, ok := c.Commands["compile"]; !ok {
		t.Error("built-in compile command is missing")
	}
}

func TestDefaultsNeedExactlyOneProject(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		noEngine(t)
		dir := t.TempDir()
		_, _, err := load("", dir)
		errContains(t, err, "no unreagent.yaml", "no .uproject", dir, "next to your .uproject")
	})
	t.Run("several", func(t *testing.T) {
		noEngine(t)
		dir := t.TempDir()
		writeUProject(t, dir, "B.uproject")
		writeUProject(t, dir, "A.uproject")
		_, _, err := load("", dir)
		errContains(t, err, "no unreagent.yaml", "A.uproject, B.uproject", "unreal.project")
		// k253: the launcher already lies next to them; that advice does not fit.
		if strings.Contains(err.Error(), "next to your .uproject") {
			t.Errorf("error tells the user to put the launcher next to the .uproject it already lies next to: %v", err)
		}
	})
}

// A start with a config file keeps its behaviour: several .uproject files
// silently give the first, none gives an empty project, and neither sets
// Info.Defaults nor adds a toolkit server.
func TestConfigFileStartIsUnchanged(t *testing.T) {
	t.Setenv("UE_ROOT", t.TempDir())
	const base = "agent: {enabled: false}\n"
	for _, explicit := range []bool{true, false} {
		dir := t.TempDir()
		writeUProject(t, dir, "B.uproject")
		first := writeUProject(t, dir, "A.uproject")
		writeBridge(t, dir, "UELLMToolkit")
		path := writeConfigFiles(t, dir, base, "")
		if !explicit {
			path = ""
		}
		c, info, err := load(path, dir)
		if err != nil {
			t.Fatalf("explicit=%v: %v", explicit, err)
		}
		if info.Defaults || info.Project != first || len(info.Warnings) != 0 {
			t.Errorf("explicit=%v: Defaults=%v Project=%q Warnings=%v; want false, %q, none", explicit, info.Defaults, info.Project, info.Warnings, first)
		}
		if c.Agent.Enabled || c.MCP.Enabled || len(c.MCP.ExtraServers) != 0 {
			t.Errorf("explicit=%v: agent.enabled=%v mcp.enabled=%v extraServers=%v; want the file's values", explicit, c.Agent.Enabled, c.MCP.Enabled, c.MCP.ExtraServers)
		}
	}

	// An empty config file is a config file: nothing is switched on.
	dir := t.TempDir()
	writeConfigFiles(t, dir, "", "")
	c, info, err := load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Defaults || info.Project != "" || c.Agent.Enabled || c.MCP.Enabled {
		t.Errorf("empty config: Defaults=%v Project=%q agent.enabled=%v mcp.enabled=%v; want all zero", info.Defaults, info.Project, c.Agent.Enabled, c.MCP.Enabled)
	}
}

// An explicit -config that does not exist stays an error, whatever lies next
// to the executable.
func TestExplicitMissingConfigStillFails(t *testing.T) {
	dir := t.TempDir()
	writeUProject(t, dir, "Game.uproject")
	_, _, err := load(filepath.Join(dir, "other.yaml"), dir)
	errContains(t, err, "config not readable", "other.yaml")
}

// A broken unreagent.yaml link is a load error, not a missing config.
func TestBrokenConfigLinkIsNotDefaults(t *testing.T) {
	dir := t.TempDir()
	writeUProject(t, dir, "Game.uproject")
	symlink(t, filepath.Join(dir, "missing.yaml"), filepath.Join(dir, "unreagent.yaml"))
	_, _, err := load("", dir)
	errContains(t, err, "config not readable", "symlink")
}

func TestDefaultsToolkitDetection(t *testing.T) {
	const flat = "UELLMToolkit"
	const nested = "ue-llm-toolkit/Plugin/UELLMToolkit"
	for _, tc := range []struct {
		name    string
		plugins []string
		want    string // plugin directory used, "" = no server
		warn    bool
	}{
		{name: "absent"},
		{name: "flat", plugins: []string{flat}, want: flat},
		{name: "nested", plugins: []string{nested}, want: nested},
		{name: "several take the shallowest", plugins: []string{nested, flat}, want: flat, warn: true},
		{name: "several at one depth take the first by name", plugins: []string{"B", "A"}, want: "A", warn: true},
		{name: "at the depth limit", plugins: []string{"a/b/c/d"}, want: "a/b/c/d"},
		{name: "too deep", plugins: []string{"a/b/c/d/e"}},
		{name: "not below node_modules", plugins: []string{"X/node_modules/y"}},
		{name: "not below Content", plugins: []string{"X/Content/y"}},
		{name: "not below Source", plugins: []string{"X/Source/y"}},
		{name: "not below Saved", plugins: []string{"X/Saved/y"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("UE_ROOT", filepath.Join(dir, "engine"))
			writeUProject(t, dir, "Game.uproject")
			if len(tc.plugins) == 0 {
				// A Plugins directory without a bridge.
				if err := os.MkdirAll(filepath.Join(dir, "Plugins", "Other", "Resources"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			bridges := map[string]string{}
			for _, p := range tc.plugins {
				bridges[p] = writeBridge(t, dir, p)
			}
			c, info, err := load("", dir)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(c.MCP.ExtraServers) != 0 || len(info.Warnings) != 0 {
					t.Fatalf("extraServers = %v, warnings = %v; want none", c.MCP.ExtraServers, info.Warnings)
				}
				return
			}
			def := c.MCP.ExtraServers[ToolkitServerName]
			if def["command"] != "node" {
				t.Errorf("command = %v, want node", def["command"])
			}
			if got, want := toolkitArgs(c), []interface{}{bridges[tc.want]}; !reflect.DeepEqual(got, want) {
				t.Errorf("args = %v, want %v", got, want)
			}
			if env, _ := def["env"].(map[string]interface{}); env["UNREAL_MCP_URL"] != "http://127.0.0.1:3000" {
				t.Errorf("env = %v, want UNREAL_MCP_URL http://127.0.0.1:3000", def["env"])
			}
			if !c.hasNodeExtraServer() {
				t.Error("the toolkit entry is not seen as a node-started server (bridge preparation would skip it)")
			}
			if tc.warn != (len(info.Warnings) == 1) {
				t.Fatalf("warnings = %v, want one: %v", info.Warnings, tc.warn)
			}
			if tc.warn {
				for _, p := range tc.plugins {
					if !strings.Contains(info.Warnings[0], "Plugins/"+p+"/Resources/mcp-bridge/index.js") {
						t.Errorf("warning %q does not name the bridge in %s", info.Warnings[0], p)
					}
				}
			}
		})
	}
}

// k238: a plugin directory that is a link (a toolkit checkout kept outside
// the project) is searched like a directory, and the bridge path stays the
// one below Plugins/. Links that lead to a file or nowhere are passed over.
// Windows junctions take the same branch (linksToDir) but are unverified.
func TestDefaultsToolkitBehindSymlink(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UE_ROOT", filepath.Join(dir, "engine"))
	writeUProject(t, dir, "Game.uproject")
	checkout := t.TempDir()
	writeBridge(t, checkout, "UELLMToolkit") // checkout/Plugins/UELLMToolkit
	plugins := filepath.Join(dir, "Plugins")
	if err := os.Mkdir(plugins, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugins, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(checkout, "Plugins", "UELLMToolkit"), filepath.Join(plugins, "Toolkit"))
	symlink(t, filepath.Join(plugins, "README.md"), filepath.Join(plugins, "ToFile"))
	symlink(t, filepath.Join(plugins, "missing"), filepath.Join(plugins, "Dangling"))

	c, info, err := load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.ToSlash(filepath.Join(plugins, "Toolkit", "Resources", "mcp-bridge", "index.js"))
	if got := toolkitArgs(c); !reflect.DeepEqual(got, []interface{}{want}) {
		t.Errorf("args = %v, want [%s]", got, want)
	}
	if len(info.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", info.Warnings)
	}
}

// k228: a link that leads back up (Plugins/Loop -> Plugins), or a second link
// to the same plugin, shows the one bridge again under another path. It is
// one bridge: no "several bridges" warning, and the path is the first found.
func TestDefaultsToolkitFoundOncePerDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UE_ROOT", filepath.Join(dir, "engine"))
	writeUProject(t, dir, "Game.uproject")
	want := writeBridge(t, dir, "Sub/UELLMToolkit")
	plugins := filepath.Join(dir, "Plugins")
	symlink(t, plugins, filepath.Join(plugins, "Loop"))
	symlink(t, filepath.Join(plugins, "Sub", "UELLMToolkit"), filepath.Join(plugins, "Sub", "Again"))
	symlink(t, filepath.Join(plugins, "Sub", "UELLMToolkit"), filepath.Join(plugins, "Zzz"))

	c, info, err := load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Warnings) != 0 {
		t.Errorf("warnings = %v, want none: it is one plugin", info.Warnings)
	}
	// Plugins/Zzz is the shallowest path to it, and so the one used.
	want = strings.Replace(want, "Plugins/Sub/UELLMToolkit", "Plugins/Zzz", 1)
	if got := toolkitArgs(c); !reflect.DeepEqual(got, []interface{}{want}) {
		t.Errorf("args = %v, want [%s]", got, want)
	}
}

// k238: the toolkit is optional, so a Plugins the search cannot look into
// warns and the launcher starts without the bridge — it does not stop.
func TestDefaultsUnsearchablePluginsWarns(t *testing.T) {
	setup := func(t *testing.T) string {
		dir := t.TempDir()
		t.Setenv("UE_ROOT", filepath.Join(dir, "engine"))
		writeUProject(t, dir, "Game.uproject")
		return dir
	}
	check := func(t *testing.T, dir string, wantBridge bool, wants ...string) {
		t.Helper()
		c, info, err := load("", dir)
		if err != nil {
			t.Fatalf("load failed, want a warning: %v", err)
		}
		if !info.Defaults || !c.Agent.Enabled || !c.MCP.Enabled {
			t.Errorf("Defaults=%v agent.enabled=%v mcp.enabled=%v; want the defaults", info.Defaults, c.Agent.Enabled, c.MCP.Enabled)
		}
		if got := len(c.MCP.ExtraServers) == 1; got != wantBridge {
			t.Errorf("extraServers = %v, want a bridge: %v", c.MCP.ExtraServers, wantBridge)
		}
		if len(info.Warnings) != 1 {
			t.Fatalf("warnings = %v, want one", info.Warnings)
		}
		for _, w := range wants {
			if !strings.Contains(info.Warnings[0], w) {
				t.Errorf("warning %q does not mention %q", info.Warnings[0], w)
			}
		}
	}
	t.Run("Plugins is a file", func(t *testing.T) {
		dir := setup(t)
		if err := os.WriteFile(filepath.Join(dir, "Plugins"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		check(t, dir, false, "Plugins next to the .uproject is not a directory", "UE LLM Toolkit")
	})
	// unreadable makes path a directory that cannot be listed, and skips the
	// test where that cannot be arranged (root, or a file system without modes).
	unreadable := func(t *testing.T, path string) {
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
		if _, err := os.ReadDir(path); err == nil {
			t.Skip("cannot make a directory unreadable here")
		}
	}
	t.Run("Plugins unreadable", func(t *testing.T) {
		dir := setup(t)
		writeBridge(t, dir, "UELLMToolkit")
		unreadable(t, filepath.Join(dir, "Plugins"))
		check(t, dir, false, "cannot read Plugins:", "UE LLM Toolkit")
	})
	// One unreadable plugin directory does not hide the bridge in another.
	t.Run("a plugin directory unreadable", func(t *testing.T) {
		dir := setup(t)
		writeBridge(t, dir, "UELLMToolkit")
		writeBridge(t, dir, "Locked")
		unreadable(t, filepath.Join(dir, "Plugins", "Locked"))
		check(t, dir, true, "cannot read Plugins/Locked:")
	})
}

// The document DefaultYAML returns is what `unreagent init` writes: portable
// (placeholders, no machine path), and loading it as a file gives the config
// a start without a file runs on.
func TestDefaultYAMLRoundTrips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UE_ROOT", filepath.Join(dir, "engine"))
	writeUProject(t, dir, "Game.uproject")
	writeBridge(t, dir, "ue-llm-toolkit/Plugin/UELLMToolkit")

	doc, bridges, warnings, err := DefaultYAML(dir)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err = %v, warnings = %v", err, warnings)
	}
	const want = `agent:
  enabled: true
  claudeIntegration: true
mcp:
  enabled: true
  extraServers:
    ue-llm-toolkit:
      command: node
      args:
        - ${PROJECT_DIR}/Plugins/ue-llm-toolkit/Plugin/UELLMToolkit/Resources/mcp-bridge/index.js
      env:
        UNREAL_MCP_URL: http://127.0.0.1:3000
`
	if string(doc) != want {
		t.Errorf("DefaultYAML =\n%s\nwant\n%s", doc, want)
	}
	if want := []string{"Plugins/ue-llm-toolkit/Plugin/UELLMToolkit/Resources/mcp-bridge/index.js"}; !reflect.DeepEqual(bridges, want) {
		t.Errorf("bridges = %v, want %v", bridges, want)
	}

	fromDefaults, _, err := load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	fromFile, info, err := load("", filepath.Dir(writeConfigFiles(t, dir, string(doc), "")))
	if err != nil {
		t.Fatal(err)
	}
	if info.Defaults {
		t.Error("Info.Defaults is true with the written file in place")
	}
	if !reflect.DeepEqual(fromDefaults, fromFile) {
		t.Errorf("config from the defaults differs from the config loaded from the written document:\n%+v\n%+v", fromDefaults, fromFile)
	}

	plain, bridges, _, err := DefaultYAML(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "extraServers") || len(bridges) != 0 {
		t.Errorf("without a plugin: bridges = %v, document =\n%s", bridges, plain)
	}
}

// unreagent.local.yaml overlays the defaults as it overlays a config file, so
// engineRoot there works without one. unreal.project does not: without a
// config file the project is the .uproject next to the executable.
func TestDefaultsTakeLocalOverlay(t *testing.T) {
	noEngine(t)
	dir := t.TempDir()
	writeUProject(t, dir, "Game.uproject")
	engine := filepath.ToSlash(filepath.Join(dir, "UE"))
	local := filepath.Join(dir, "unreagent.local.yaml")
	if err := os.WriteFile(local, []byte("engineRoot: "+engine+"\nagent: {command: my-claude}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, info, err := load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Defaults || info.LocalPath != local || info.EngineRoot != engine {
		t.Errorf("Defaults=%v LocalPath=%q EngineRoot=%q; want true, %q, %q", info.Defaults, info.LocalPath, info.EngineRoot, local, engine)
	}
	if !c.Agent.Enabled || c.Agent.Command != "my-claude" {
		t.Errorf("agent.enabled=%v agent.command=%q; want the default kept and the overlay's command", c.Agent.Enabled, c.Agent.Command)
	}

	writeUProject(t, dir, "Other/Other.uproject")
	if err := os.WriteFile(local, []byte("unreal: {project: Other/Other.uproject}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = load("", dir)
	errContains(t, err, "unreal.project", "unreagent.local.yaml", "unreagent.yaml")
}

// k246: LoadForPreflight is Load for `unreagent init`. A missing engine is the
// one thing it lets through: the configuration loads with ${ENGINE} left in
// place and the engine-bound built-in commands left out, where Load — the
// launcher's start — keeps refusing with the error that names the fix.
func TestLoadForPreflightLoadsWithoutAnEngine(t *testing.T) {
	noEngine(t)
	dir := t.TempDir()
	project := writeUProject(t, dir, "Game.uproject")

	_, _, err := load("", dir)
	errContains(t, err, "${ENGINE} is unresolved (engine not found — set UE_ROOT or engineRoot in unreagent.local.yaml) but used by unreal.editor")

	c, info, err := LoadForPreflight("", dir)
	if err != nil {
		t.Fatalf("LoadForPreflight without an engine: %v", err)
	}
	if !info.Defaults || info.EngineRoot != "" || info.Project != project {
		t.Errorf("info = %+v; want the defaults, no engine and project %s", info, project)
	}
	if want := "${ENGINE}/Engine/Binaries/Win64/UnrealEditor.exe"; c.Unreal.Editor != want {
		t.Errorf("unreal.editor = %q; want the placeholder kept: %q", c.Unreal.Editor, want)
	}
	if want := []string{"compile", "package"}; !reflect.DeepEqual(info.SkippedCommands, want) || len(c.Commands) != 0 {
		t.Errorf("SkippedCommands = %v, commands = %v; want %v left out", info.SkippedCommands, c.Commands, want)
	}
	if !c.Agent.Enabled || !c.MCP.Enabled {
		t.Errorf("agent.enabled = %v, mcp.enabled = %v; want the built-in defaults", c.Agent.Enabled, c.MCP.Enabled)
	}

	// With an engine it is Load.
	t.Setenv("UE_ROOT", filepath.Join(dir, "engine"))
	strict, strictInfo, err := load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	lenient, lenientInfo, err := LoadForPreflight("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(strict, lenient) || !reflect.DeepEqual(strictInfo, lenientInfo) {
		t.Errorf("with an engine LoadForPreflight differs from Load:\n%+v\n%+v", lenient, strict)
	}
}

// Only the engine is optional there: every other load error stays one, and a
// check that fails on the placeholder left in place says so.
func TestLoadForPreflightStillFails(t *testing.T) {
	noEngine(t)
	t.Run("no project", func(t *testing.T) {
		_, _, err := LoadForPreflight("", t.TempDir())
		errContains(t, err, "no unreagent.yaml and no .uproject")
	})
	t.Run("unresolved project placeholder", func(t *testing.T) {
		path := writeConfigFiles(t, t.TempDir(), "unreal: {editor: \"${PROJECT_DIR}/ue\"}\n", "")
		_, _, err := LoadForPreflight(path, "")
		errContains(t, err, "${PROJECT_DIR} is unresolved", "unreal.editor")
		if err != nil && strings.Contains(err.Error(), "${ENGINE}") {
			t.Errorf("err = %v; the engine is not an error here", err)
		}
	})
	t.Run("rule on the engine", func(t *testing.T) {
		dir := t.TempDir()
		writeUProject(t, dir, "Game.uproject")
		path := writeConfigFiles(t, dir, "mcp: {enabled: true}\npermissions:\n  enabled: true\n  allow: [\"Read(${ENGINE}/*)\"]\n", "")
		_, _, err := LoadForPreflight(path, "")
		errContains(t, err, "permissions.allow[0]", "checked with ${ENGINE} left in place: engine not found — set UE_ROOT or engineRoot")
	})
	t.Run("error that has nothing to do with the engine", func(t *testing.T) {
		dir := t.TempDir()
		writeUProject(t, dir, "Game.uproject")
		path := writeConfigFiles(t, dir, "agent: {enabled: true, onExit: sometimes}\n", "")
		_, _, err := LoadForPreflight(path, "")
		errContains(t, err, "agent.onExit invalid")
		if err != nil && strings.Contains(err.Error(), "${ENGINE}") {
			t.Errorf("err = %v; it names the engine, which this error is not about", err)
		}
	})
}
