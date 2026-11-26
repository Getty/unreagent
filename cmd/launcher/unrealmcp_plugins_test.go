package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
)

// k267: with unreal.mcp on, the .uproject must enable the engine plugin
// ModelContextProtocol and at least one toolset plugin (AllToolsets, or a
// plugin of the engine's Plugins/Experimental/Toolsets); all of them are off
// by default in the engine, so only a .uproject entry enables them.
// unrealMCPPluginCheck names each one missing or switched off, says nothing
// where it cannot tell, and reports a check it cannot make.
func TestUnrealMCPPluginCheck(t *testing.T) {
	const (
		mcp      = `{"Name": "ModelContextProtocol", "Enabled": true}`
		all      = `{"Name": "AllToolsets", "Enabled": true}`
		mcpHead  = "Unreal MCP: the .uproject does not enable the plugin ModelContextProtocol"
		offHead  = "Unreal MCP: the .uproject switches the plugin ModelContextProtocol off"
		toolHead = "Unreal MCP: the .uproject enables no toolset plugin"
	)
	plugins := func(entries ...string) string {
		return `{"FileVersion": 3, "Plugins": [` + strings.Join(entries, ", ") + `]}`
	}
	engineToolsets := []string{"AllToolsets", "DataflowAgent", "EditorToolset"}
	for _, tc := range []struct {
		name       string
		uproject   string
		toolsets   []string // directories under the engine's Plugins/Experimental/Toolsets; nil = no engine directory, empty = an engine without that directory
		args       []string
		want       [][]string // one entry per warning: its head first, then strings its line holds
		notChecked []string   // one entry per check not made: a string its report holds
	}{
		{name: "neither entry", uproject: `{"FileVersion": 3}`, toolsets: engineToolsets,
			want: [][]string{
				{mcpHead, "no Plugins entry", "Unreal MCP server", "5 minutes", `Fix: add {"Name": "ModelContextProtocol", "Enabled": true}`, "unreal.mcp.enabled: false"},
				{toolHead, "AllToolsets", "Toolsets", `Fix: add {"Name": "AllToolsets", "Enabled": true}`},
			}},
		{name: "both enabled, no engine needed", uproject: plugins(mcp, all)},
		{name: "names in another case", uproject: plugins(`{"Name": "modelcontextprotocol", "Enabled": true}`, `{"Name": "alltoolsets", "Enabled": true}`)},
		{name: "ModelContextProtocol switched off", uproject: plugins(`{"Name": "ModelContextProtocol", "Enabled": false}`, all),
			want: [][]string{{offHead, `"Enabled": false`, "5 minutes", "Fix: set that entry's Enabled to true"}}},
		{name: "the first entry counts", uproject: plugins(`{"Name": "ModelContextProtocol", "Enabled": false}`, mcp, all),
			want: [][]string{{offHead}}},
		{name: "AllToolsets switched off", uproject: plugins(mcp, `{"Name": "AllToolsets", "Enabled": false}`), toolsets: engineToolsets,
			want: [][]string{{toolHead, `AllToolsets is listed with "Enabled": false`}}},
		{name: "a single engine toolset instead of AllToolsets", uproject: plugins(mcp, `{"Name": "EditorToolset", "Enabled": true}`), toolsets: engineToolsets},
		{name: "an engine toolset without Toolset in its name", uproject: plugins(mcp, `{"Name": "DataflowAgent", "Enabled": true}`), toolsets: engineToolsets},
		{name: "ToolsetRegistry and the modeling plugins are no toolsets", toolsets: engineToolsets,
			uproject: plugins(mcp, `{"Name": "ToolsetRegistry", "Enabled": true}`, `{"Name": "MeshModelingToolsetExp", "Enabled": true}`, `{"Name": "HairModelingToolset", "Enabled": true}`),
			want:     [][]string{{toolHead}}},
		{name: "a switched-off toolset does not count", uproject: plugins(mcp, `{"Name": "EditorToolset", "Enabled": false}`), toolsets: engineToolsets,
			want: [][]string{{toolHead}}},
		{name: "a single toolset, no engine directory: toolsets not checked", uproject: plugins(mcp, `{"Name": "EditorToolset", "Enabled": true}`),
			notChecked: []string{"Unreal MCP toolsets: not checked"}},
		{name: "an engine without the Toolsets directory: toolsets not checked", uproject: plugins(mcp), toolsets: []string{},
			notChecked: []string{"Unreal MCP toolsets: not checked"}},
		{name: "toolsets not checked, ModelContextProtocol still is", uproject: `{}`,
			want: [][]string{{mcpHead}}, notChecked: []string{"Unreal MCP toolsets: not checked"}},
		{name: "a limited entry cannot be told: quiet", uproject: plugins(`{"Name": "ModelContextProtocol", "Enabled": true, "Optional": true}`, `{"Name": "AllToolsets", "Enabled": true, "PlatformAllowList": ["Win64"]}`)},
		{name: "an Enabled that is no boolean cannot be told: quiet", uproject: plugins(`{"Name": "ModelContextProtocol", "Enabled": "true"}`, all)},
		{name: "-EnablePlugins= is not read", uproject: `{}`, args: []string{"-EnablePlugins=ModelContextProtocol"}, notChecked: []string{"-EnablePlugins="}},
		{name: "-enableplugins= in any case", uproject: `{}`, args: []string{"/enableplugins=AllToolsets"}, notChecked: []string{"-EnablePlugins="}},
		{name: "-EnableAllPlugins", uproject: `{}`, args: []string{"-EnableAllPlugins"}, notChecked: []string{"-EnableAllPlugins"}},
		{name: "-NoEnginePlugins", uproject: plugins(mcp, all), args: []string{"-NoEnginePlugins"}, notChecked: []string{"skips the .uproject's Plugins list"}},
		{name: "-DisablePlugins= can only switch off: still checked", uproject: `{}`, args: []string{"-DisablePlugins=Foo"}, toolsets: engineToolsets,
			want: [][]string{{mcpHead}, {toolHead}}},
		{name: "unreadable .uproject", uproject: `not json`, notChecked: []string{"cannot be read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			project := filepath.Join(dir, "Game.uproject")
			mustWriteFile(t, project, tc.uproject)
			engine := ""
			if tc.toolsets != nil {
				engine = filepath.Join(t.TempDir(), "Engine")
				mustWriteFile(t, filepath.Join(engine, "Build", "Build.version"), buildVersion584)
				for _, name := range tc.toolsets {
					mustWriteFile(t, filepath.Join(engine, "Plugins", "Experimental", "Toolsets", name, name+".uplugin"), "{}")
				}
			}
			warnings, notChecked := unrealMCPPluginCheck(project, engine, append([]string{project}, tc.args...))
			if len(notChecked) != len(tc.notChecked) {
				t.Errorf("not checked: got %q, want %d holding %q", notChecked, len(tc.notChecked), tc.notChecked)
			}
			for i := range notChecked {
				if i < len(tc.notChecked) && !strings.Contains(notChecked[i], tc.notChecked[i]) {
					t.Errorf("not checked %d is %q, want it to hold %q", i, notChecked[i], tc.notChecked[i])
				}
			}
			if len(warnings) != len(tc.want) {
				t.Fatalf("got %d warnings, want %d: %+v", len(warnings), len(tc.want), warnings)
			}
			for i, wants := range tc.want {
				line := warnings[i].line()
				if warnings[i].head != wants[0] {
					t.Errorf("warning %d head %q, want %q", i, warnings[i].head, wants[0])
				}
				for _, w := range wants[1:] {
					if !strings.Contains(line, w) {
						t.Errorf("warning %d does not hold %q:\n%s", i, w, line)
					}
				}
				first, rest, more := strings.Cut(line, "\n")
				if first != warnings[i].head || !more || !strings.Contains(rest, project) {
					t.Errorf("warning %d: want the head alone on the first line, the .uproject in the rest:\n%s", i, line)
				}
				if n := len("unreagent: WARN " + first); n > 100 {
					t.Errorf("warning %d: first line is %d characters with the console prefix", i, n)
				}
				if strings.Contains(line, "5m0s") {
					t.Errorf("warning %d shows a Go duration: %s", i, line)
				}
			}
		})
	}
}

// k267 in the init preflight: with unreal.mcp on, a WARN finding per
// missing plugin, an ok line when nothing is missing; nothing at all with
// the switch off.
func TestInitPreflightNamesMissingUnrealMCPPlugins(t *testing.T) {
	tr := newInitTree(t, buildVersion584, "node", "npm", "claude")
	project := filepath.Join(tr.dir, "Game.uproject")
	mustWriteFile(t, filepath.Join(tr.dir, "unreagent.local.yaml"), "unreal:\n  mcp: {enabled: true}\nmcp:\n  enabled: true\n")
	find := func() []finding {
		cfg, info, err := config.LoadForPreflight("", tr.dir)
		if err != nil {
			t.Fatal(err)
		}
		var got []finding
		for _, f := range preflight(cfg, info) {
			if strings.HasPrefix(f.text, "Unreal MCP") {
				got = append(got, f)
			}
		}
		return got
	}

	// The engine of the tree has no Toolsets directory: only
	// ModelContextProtocol is checked then.
	fs := find()
	if len(fs) != 2 || fs[0].level != levelWarn || !strings.Contains(fs[0].text, "does not enable the plugin ModelContextProtocol") ||
		!strings.HasPrefix(fs[0].fix, `add {"Name": "ModelContextProtocol"`) ||
		fs[1].level != levelInfo || !strings.Contains(fs[1].text, "toolsets: not checked") {
		t.Errorf("nothing enabled, no Toolsets directory: findings %+v", fs)
	}
	mustWriteFile(t, filepath.Join(tr.engine, "Engine", "Plugins", "Experimental", "Toolsets", "AllToolsets", "AllToolsets.uplugin"), "{}")
	fs = find()
	if len(fs) != 2 || fs[0].level != levelWarn || fs[1].level != levelWarn || !strings.Contains(fs[1].text, "enables no toolset plugin") {
		t.Errorf("nothing enabled: findings %+v", fs)
	}

	mustWriteFile(t, project, `{"FileVersion": 3, "Modules": [{"Name": "Game", "Type": "Runtime"}], "Plugins": [{"Name": "ModelContextProtocol", "Enabled": true}, {"Name": "AllToolsets", "Enabled": true}]}`)
	if fs := find(); len(fs) != 1 || fs[0].level != levelOK || strings.Contains(fs[0].text, "neither") {
		t.Errorf("both enabled: findings %+v", fs)
	}

	mustWriteFile(t, filepath.Join(tr.dir, "unreagent.local.yaml"), "unreal:\n  mcp: {enabled: true}\n  args: [\"-EnableAllPlugins\"]\nmcp:\n  enabled: true\n")
	if fs := find(); len(fs) != 1 || fs[0].level != levelInfo || !strings.Contains(fs[0].text, "not checked") {
		t.Errorf("-EnableAllPlugins: findings %+v", fs)
	}

	mustWriteFile(t, filepath.Join(tr.dir, "unreagent.local.yaml"), "mcp:\n  enabled: true\n")
	mustWriteFile(t, project, `{"FileVersion": 3}`)
	if fs := find(); len(fs) != 0 {
		t.Errorf("unreal.mcp off: findings %+v", fs)
	}
}

// k267 through run(): with unreal.mcp on and a .uproject that enables
// neither plugin, both WARNs come before the editor starts, in the log and,
// in window mode, once each on stderr; the editor and the agent start all
// the same. With the switch off there is no such line.
func TestRunWarnsWhenUnrealMCPPluginsAreMissing(t *testing.T) {
	const (
		mcpWarn  = "WARN Unreal MCP: the .uproject does not enable the plugin ModelContextProtocol"
		toolWarn = "WARN Unreal MCP: the .uproject enables no toolset plugin"
	)
	for _, tc := range []struct {
		name   string
		window bool
		on     bool
	}{
		{"window mode, log is a file", true, true},
		{"log on stdout", false, true},
		{"switch off", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// The editor in the <root>/Engine/Binaries/<platform> shape, so
			// the engine's Toolsets directory is found from it.
			engine := filepath.Join(dir, "UE", "Engine")
			mustWriteFile(t, filepath.Join(engine, "Plugins", "Experimental", "Toolsets", "AllToolsets", "AllToolsets.uplugin"), "{}")
			editor := filepath.Join(engine, "Binaries", "Linux", "editor.sh")
			if err := os.MkdirAll(filepath.Dir(editor), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(editor, []byte(markerScript), 0o755); err != nil {
				t.Fatal(err)
			}
			agent := filepath.Join(dir, "agent.sh")
			if err := os.WriteFile(agent, []byte(markerScript), 0o755); err != nil {
				t.Fatal(err)
			}
			project := filepath.Join(dir, "Demo.uproject")
			mustWriteFile(t, project, "{}")
			yaml := `unreal:
  editor: ` + editor + `
  project: ` + project + `
  args: ["20"]
  unattended: false
  mcp: {enabled: ` + fmt.Sprint(tc.on) + `, port: 1}
mcp:
  enabled: false
permissions:
  enabled: false
runtimes:
  python: { enabled: false }
  node: { enabled: false }
agent:
  enabled: true
  command: ` + agent + `
  window: ` + fmt.Sprint(tc.window) + `
  claudeIntegration: false
  startDelaySeconds: 1
`
			addr := freeLoopbackAddr(t)
			l := startLauncherIn(t, dir, withMCP(yaml, addr))
			waitForFile(t, agent+".ran", 15*time.Second)
			waitForFile(t, editor+".ran", 15*time.Second)
			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			log := l.stdout.String()
			if tc.window {
				log = mustReadFile(t, filepath.Join(dir, "unreagent.log"))
			}
			stderr := l.stderr.String()
			for _, w := range []string{mcpWarn, toolWarn} {
				switch {
				case !tc.on && strings.Contains(log, w):
					t.Errorf("switch off, yet the log holds %q:\n%s", w, log)
				case tc.on && !strings.Contains(log, w):
					t.Errorf("the log does not hold %q:\n%s", w, log)
				case tc.window && strings.Count(stderr, w+" (the rest is in ") != 1:
					t.Errorf("stderr holds %q with the pointer to the log %d times, want once:\n%s", w, strings.Count(stderr, w+" (the rest is in "), stderr)
				case !tc.window && strings.Contains(stderr, w):
					t.Errorf("stderr repeats %q, which the log on stdout shows:\n%s", w, stderr)
				}
			}
		})
	}
}
