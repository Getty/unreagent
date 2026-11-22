package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/conflict-industries/unreagent/internal/config"
)

// initTree is a project directory for `unreagent init`: one .uproject that
// declares the module Game, an engine (UE_ROOT) with a Build.version and an
// editor binary, and a PATH that holds exactly the tools named. All paths are
// built for the host: on Windows the layout is the same, but these tests
// have only run on Linux.
type initTree struct {
	t           *testing.T
	dir, engine string
}

const toolkitPlugin = "Plugins/ue-llm-toolkit/Plugin/UELLMToolkit"

func newInitTree(t *testing.T, buildVersion string, onPath ...string) initTree {
	t.Helper()
	dir := t.TempDir()
	tr := initTree{t: t, dir: dir, engine: filepath.Join(t.TempDir(), "UE")}
	mustWriteFile(t, filepath.Join(dir, "Game.uproject"), `{"FileVersion": 3, "Modules": [{"Name": "Game", "Type": "Runtime"}]}`)
	if buildVersion != "" {
		mustWriteFile(t, filepath.Join(tr.engine, "Engine", "Build", "Build.version"), buildVersion)
		tr.executable(filepath.Join(tr.engine, "Engine", "Binaries", "Win64", "UnrealEditor.exe"))
		t.Setenv("UE_ROOT", tr.engine)
	} else {
		t.Setenv("UE_ROOT", "")
	}
	bin := t.TempDir()
	for _, name := range onPath {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		tr.executable(filepath.Join(bin, name))
	}
	t.Setenv("PATH", bin)
	return tr
}

func (tr initTree) executable(path string) {
	tr.t.Helper()
	mustWriteFile(tr.t, path, "#!/bin/sh\n")
	if err := os.Chmod(path, 0o755); err != nil {
		tr.t.Fatal(err)
	}
}

// toolkit adds the UE LLM Toolkit in the submodule layout; uplugin is its
// descriptor, installed says whether its node_modules carry the marker.
func (tr initTree) toolkit(uplugin string, installed bool) {
	tr.t.Helper()
	plugin := filepath.Join(tr.dir, filepath.FromSlash(toolkitPlugin))
	mustWriteFile(tr.t, filepath.Join(plugin, "UELLMToolkit.uplugin"), uplugin)
	bridge := filepath.Join(plugin, "Resources", "mcp-bridge")
	mustWriteFile(tr.t, filepath.Join(bridge, "index.js"), "// bridge\n")
	mustWriteFile(tr.t, filepath.Join(bridge, "package.json"), "{}")
	if installed {
		mustWriteFile(tr.t, filepath.Join(bridge, "node_modules", bridgeInstallMarker), "")
	}
}

func (tr initTree) built() {
	tr.t.Helper()
	mustWriteFile(tr.t, filepath.Join(tr.dir, "Binaries", "Win64", "UnrealEditor-Game.dll"), "")
}

// run is `unreagent init` (with -check if check) for a launcher in the tree.
func (tr initTree) run(check bool) (string, error) {
	tr.t.Helper()
	var out bytes.Buffer
	err := initRun(func() (*config.Config, config.Info, error) { return config.LoadForPreflight("", tr.dir) }, check, &out)
	return out.String(), err
}

func (tr initTree) config() string { return filepath.Join(tr.dir, "unreagent.yaml") }

func wantAll(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output does not mention %q:\n%s", w, out)
		}
	}
}

func wantNone(t *testing.T, out string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(out, n) {
			t.Errorf("output mentions %q:\n%s", n, out)
		}
	}
}

// k228: next to a .uproject with no config, init writes the built-in defaults
// with placeholders and no machine path, the toolkit entry as found, and a
// preflight with nothing to fix when everything is there.
func TestInitWritesTheDefaultsAndACleanPreflight(t *testing.T) {
	tr := newInitTree(t, buildVersion584, "node", "npm", "claude")
	tr.toolkit(`{"FriendlyName": "UE LLM Toolkit"}`, true)
	tr.built()

	out, err := tr.run(false)
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}
	written := mustReadFile(t, tr.config())
	doc, _, _, err := config.DefaultYAML(tr.dir)
	if err != nil {
		t.Fatal(err)
	}
	if written != initHeader+string(doc) {
		t.Errorf("written config is not the header and config.DefaultYAML:\n%s", written)
	}
	wantAll(t, written, "${PROJECT_DIR}/"+toolkitPlugin+"/Resources/mcp-bridge/index.js", "enabled: true")
	if strings.Contains(written, tr.dir) || strings.Contains(written, filepath.ToSlash(tr.dir)) || strings.Contains(written, tr.engine) {
		t.Errorf("written config holds a machine path:\n%s", written)
	}
	// The written file is a config the launcher loads, no longer the defaults.
	if _, info, err := config.LoadForPreflight("", tr.dir); err != nil || info.Defaults {
		t.Errorf("loading the written config: Defaults=%v, err=%v", info.Defaults, err)
	}

	wantAll(t, out, "Wrote "+tr.config(),
		"[ok]       project: ", "[ok]       engine: ", "(UE 5.8)",
		"[ok]       agent: claude is on PATH",
		"[ok]       UE LLM Toolkit: bridge ", "[ok]       node: ", "[ok]       npm: ",
		"[ok]       bridge dependencies: installed",
		"[ok]       plugin EngineVersion: ",
		"[ok]       project modules: built", "not verified against a real project",
		"permissions: off — unreagent applies no permission policy", "Claude Code's own permission prompts",
		"0 blocking, 0 warning(s)")
	wantNone(t, out, "[WARN]", "[BLOCKING]", "Fix:")
}

// It never overwrites: an existing unreagent.yaml, or unreagent.yml, stays as
// it is, no second file appears, and the preflight is for that config.
func TestInitNeverOverwritesAConfig(t *testing.T) {
	const existing = "# mine\nagent: {enabled: false}\n"
	for _, name := range []string{"unreagent.yaml", "unreagent.yml"} {
		t.Run(name, func(t *testing.T) {
			tr := newInitTree(t, buildVersion584, "node", "npm")
			tr.toolkit(`{}`, false)
			tr.built()
			path := filepath.Join(tr.dir, name)
			mustWriteFile(t, path, existing)

			out, err := tr.run(false)
			if err != nil {
				t.Fatalf("init failed: %v\n%s", err, out)
			}
			if got := mustReadFile(t, path); got != existing {
				t.Errorf("%s was changed:\n%s", name, got)
			}
			entries, _ := os.ReadDir(tr.dir)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "unreagent") && e.Name() != name {
					t.Errorf("init created %s next to %s", e.Name(), name)
				}
			}
			wantAll(t, out, path+" exists and is left as it is. Preflight only.",
				"agent: off (agent.enabled is false)",
				// The plugin is there, this config does not name it: the entry is shown.
				"[WARN]     UE LLM Toolkit: the plugin is there ("+toolkitPlugin+"/Resources/mcp-bridge/index.js) but the config has no mcp.extraServers.ue-llm-toolkit entry",
				"Fix: add the entry to "+name, "${PROJECT_DIR}/"+toolkitPlugin+"/Resources/mcp-bridge/index.js")
			wantNone(t, out, "Wrote ")
		})
	}

	// writeDefaultConfig itself refuses a file that appeared after the load.
	tr := newInitTree(t, buildVersion584)
	mustWriteFile(t, tr.config(), existing)
	if _, err := writeDefaultConfig(tr.config()); err == nil || !strings.Contains(err.Error(), "does not overwrite") {
		t.Errorf("writeDefaultConfig over an existing file: err = %v", err)
	}
	if got := mustReadFile(t, tr.config()); got != existing {
		t.Errorf("unreagent.yaml was changed:\n%s", got)
	}
}

// -check prints the preflight for the built-in defaults and writes nothing.
func TestInitCheckWritesNothing(t *testing.T) {
	tr := newInitTree(t, buildVersion584, "node", "npm", "claude")
	out, err := tr.run(true)
	if err != nil {
		t.Fatalf("init -check failed: %v\n%s", err, out)
	}
	if fileExists(tr.config()) {
		t.Error("init -check wrote unreagent.yaml")
	}
	wantAll(t, out, "-check, so none is written", "Preflight:")
}

// No .uproject, or several: nothing to write a config for. The load error
// names the directory or the files; nothing is written.
func TestInitNeedsExactlyOneProject(t *testing.T) {
	for name, projects := range map[string][]string{"none": nil, "several": {"A.uproject", "B.uproject"}} {
		t.Run(name, func(t *testing.T) {
			tr := newInitTree(t, buildVersion584)
			if err := os.Remove(filepath.Join(tr.dir, "Game.uproject")); err != nil {
				t.Fatal(err)
			}
			for _, p := range projects {
				mustWriteFile(t, filepath.Join(tr.dir, p), "{}")
			}
			out, err := tr.run(false)
			if err == nil || !strings.Contains(err.Error(), ".uproject") || !strings.Contains(err.Error(), "unreagent init wrote nothing") {
				t.Errorf("err = %v; want the load error about the .uproject\n%s", err, out)
			}
			for _, p := range projects {
				if err != nil && !strings.Contains(err.Error(), p) {
					t.Errorf("err = %v does not name %s", err, p)
				}
			}
			if fileExists(tr.config()) {
				t.Error("init wrote unreagent.yaml")
			}
		})
	}
}

// k246: a missing engine does not end init at the load, as it ends a start of
// the launcher. The defaults are written — the document does not depend on
// the engine — and the preflight is printed in full, with the engine as its
// one blocking finding and a non-zero exit.
func TestInitWithoutEnginePrintsTheFullPreflight(t *testing.T) {
	tr := newInitTree(t, "", "node", "npm", "claude")
	tr.toolkit(`{"EngineVersion": "5.6.0"}`, true)
	out, err := tr.run(false)
	if strings.Contains(out, "[ok]       engine: ") {
		t.Skip("this host has an engine config.Load detects without UE_ROOT")
	}
	if err == nil || !strings.Contains(err.Error(), "1 blocking finding(s)") {
		t.Errorf("err = %v, want one blocking finding\n%s", err, out)
	}
	doc, _, _, derr := config.DefaultYAML(tr.dir)
	if derr != nil {
		t.Fatal(derr)
	}
	if !fileExists(tr.config()) {
		t.Fatalf("init wrote no unreagent.yaml:\n%s", out)
	}
	if written := mustReadFile(t, tr.config()); written != initHeader+string(doc) {
		t.Errorf("written config is not the header and config.DefaultYAML:\n%s", written)
	}
	wantAll(t, out, "Wrote "+tr.config(),
		"[ok]       project: ",
		"[BLOCKING] engine: not found — the launcher does not start without it: unreal.editor is ${ENGINE}/Engine/Binaries/Win64/UnrealEditor.exe",
		"Fix: set the environment variable UE_ROOT, or engineRoot in a git-ignored unreagent.local.yaml",
		"[ok]       agent: claude is on PATH",
		"[ok]       UE LLM Toolkit: bridge ", "[ok]       node: ", "[ok]       npm: ",
		"[ok]       bridge dependencies: installed",
		"[info]     plugin EngineVersion: not checked — without an engine",
		"[WARN]     project modules: Game look unbuilt",
		"permissions: off",
		"1 blocking, 1 warning(s)")
	// Nothing is said about an editor binary or a plugin that needs the engine to judge.
	wantNone(t, out, "unreal.editor not found", "does not match the engine", "or run what the launcher's compile command runs")

	// Again, now with the file there: the same finding, nothing rewritten.
	out, err = tr.run(false)
	if err == nil || !strings.Contains(err.Error(), "1 blocking finding(s)") {
		t.Errorf("second run: err = %v, want one blocking finding\n%s", err, out)
	}
	wantAll(t, out, "exists and is left as it is. Preflight only.", "[BLOCKING] engine: not found")

	t.Run("check writes nothing", func(t *testing.T) {
		tr := newInitTree(t, "")
		out, err := tr.run(true)
		if err == nil || fileExists(tr.config()) {
			t.Errorf("init -check without an engine: err = %v, config written = %v\n%s", err, fileExists(tr.config()), out)
		}
		wantAll(t, out, "[BLOCKING] engine: not found", "git submodule add -b ue-5.8", "No engine was found, so whether the branch fits yours is unknown.", "1 blocking, ")
		wantNone(t, out, "could not be read")
	})
	t.Run("config that does not use the engine", func(t *testing.T) {
		tr := newInitTree(t, "", "node", "npm", "claude")
		mustWriteFile(t, tr.config(), "unreal: {editor: claude}\n")
		out, err := tr.run(false)
		if err == nil || !strings.Contains(err.Error(), "1 blocking finding(s)") {
			t.Errorf("err = %v, want one blocking finding\n%s", err, out)
		}
		wantAll(t, out, "[BLOCKING] engine: not found\n", "1 blocking, ")
	})
}

// k246: init never writes to a -config path — not to a config that is there,
// and not where the path names no file, which is a load error.
func TestInitConfigFlagNeverWrites(t *testing.T) {
	const existing = "# mine\nunreal: {editor: claude}\n"
	tr := newInitTree(t, buildVersion584, "node", "npm", "claude")
	path := filepath.Join(tr.dir, "custom.yaml")
	mustWriteFile(t, path, existing)
	before := dirNames(t, tr.dir)

	var out bytes.Buffer
	if err := initCommand([]string{"-config", path}, &out); err != nil {
		t.Fatalf("init -config failed: %v\n%s", err, out.String())
	}
	wantAll(t, out.String(), path+" exists and is left as it is. Preflight only.", "Preflight:")
	wantNone(t, out.String(), "Wrote ")
	if got := mustReadFile(t, path); got != existing {
		t.Errorf("the -config file was changed:\n%s", got)
	}

	out.Reset()
	missing := filepath.Join(tr.dir, "missing.yaml")
	if err := initCommand([]string{"-config", missing}, &out); err == nil || !strings.Contains(err.Error(), "unreagent init wrote nothing") {
		t.Errorf("init -config on a missing file: err = %v\n%s", err, out.String())
	}
	wantNone(t, out.String(), "Wrote ", "Preflight:")
	if after := dirNames(t, tr.dir); after != before {
		t.Errorf("init -config changed the directory: %s, was %s", after, before)
	}
}

// dirNames lists the entries of dir, sorted.
func dirNames(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}

// k246: a symlink at the target is left untouched. One that leads to a config
// is that config: preflight only, nothing written through it. A broken one is
// a load error, and neither the link nor the file it names is created or
// replaced — writeDefaultConfig refuses it as well, were the load to miss it.
func TestInitLeavesASymlinkAtTheTargetAlone(t *testing.T) {
	symlink := func(t *testing.T, target, link string) {
		t.Helper()
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot create a symlink here: %v", err)
		}
	}
	isLinkTo := func(t *testing.T, link, target string) {
		t.Helper()
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Errorf("%s is no longer the link to %s: %q, %v", link, target, got, err)
		}
	}

	t.Run("to a config", func(t *testing.T) {
		const existing = "# mine\nagent: {enabled: false}\n"
		tr := newInitTree(t, buildVersion584)
		target := filepath.Join(t.TempDir(), "shared.yaml")
		mustWriteFile(t, target, existing)
		symlink(t, target, tr.config())

		out, err := tr.run(false)
		if err != nil {
			t.Fatalf("init failed: %v\n%s", err, out)
		}
		wantAll(t, out, "exists and is left as it is. Preflight only.", "agent: off (agent.enabled is false)")
		wantNone(t, out, "Wrote ")
		isLinkTo(t, tr.config(), target)
		if got := mustReadFile(t, target); got != existing {
			t.Errorf("the linked config was changed:\n%s", got)
		}
	})
	t.Run("broken", func(t *testing.T) {
		tr := newInitTree(t, buildVersion584)
		target := filepath.Join(t.TempDir(), "gone.yaml")
		symlink(t, target, tr.config())

		out, err := tr.run(false)
		if err == nil || !strings.Contains(err.Error(), "unreagent init wrote nothing") {
			t.Errorf("init on a broken link: err = %v\n%s", err, out)
		}
		wantNone(t, out, "Wrote ", "Preflight:")
		if _, err := writeDefaultConfig(tr.config()); err == nil || !strings.Contains(err.Error(), "does not overwrite") {
			t.Errorf("writeDefaultConfig on a broken link: err = %v", err)
		}
		isLinkTo(t, tr.config(), target)
		if fileExists(target) {
			t.Error("init wrote the file the broken link names")
		}
	})
}

// What the launcher starts without is a warning, each with its fix: the
// toolkit (the exact command to add it), the agent command, an unbuilt
// module, a plugin the engine will refuse. node and npm are only noted when
// there is no toolkit to run with them.
func TestInitFindingsNameTheFix(t *testing.T) {
	t.Run("nothing installed, UE 5.8", func(t *testing.T) {
		tr := newInitTree(t, buildVersion584)
		out, err := tr.run(false)
		if err != nil {
			t.Fatalf("init failed on warnings alone: %v\n%s", err, out)
		}
		wantAll(t, out,
			"[WARN]     agent: command claude is not on PATH", "set agent.command to its full path in unreagent.local.yaml",
			"[WARN]     UE LLM Toolkit: not found below Plugins/",
			"    git submodule add -b ue-5.8 https://github.com/Getty/ue-llm-toolkit Plugins/ue-llm-toolkit",
			"upstream is https://github.com/ColtonWilley/ue-llm-toolkit",
			"[info]     node: node is not on PATH", "[info]     npm: npm is not on PATH", "needed only for the UE LLM Toolkit bridge",
			"[WARN]     project modules: Game look unbuilt", "not verified against a real project",
			"GameEditor Win64 Development",
			"Then run `unreagent init` again",
			"0 blocking, 3 warning(s)")
		wantNone(t, out, "not made for it", "could not be read", "bridge dependencies")
		if written := mustReadFile(t, tr.config()); strings.Contains(written, "extraServers") {
			t.Errorf("config names a toolkit that is not there:\n%s", written)
		}
	})
	t.Run("toolkit command on another engine", func(t *testing.T) {
		tr := newInitTree(t, buildVersion574)
		out, _ := tr.run(true)
		wantAll(t, out, "git submodule add -b ue-5.8", "This engine is UE 5.7: that branch is not made for it")
	})
	t.Run("toolkit there, tools missing, plugin refused", func(t *testing.T) {
		tr := newInitTree(t, buildVersion584, "claude")
		tr.toolkit(`{"EngineVersion": "5.6.0"}`, false)
		tr.built()
		out, err := tr.run(false)
		if err != nil {
			t.Fatalf("init failed on warnings alone: %v\n%s", err, out)
		}
		wantAll(t, out,
			"[ok]       UE LLM Toolkit: bridge ",
			"[WARN]     node: node is not on PATH — the bridge cannot start", "[WARN]     npm: npm is not on PATH", "Fix: install Node.js",
			"[info]     bridge dependencies: not installed — the launcher runs npm install at its first start",
			"[WARN]     plugin 'UELLMToolkit': its EngineVersion \"5.6.0\" does not match", "under -unattended the answer is No",
			"[ok]       project modules: built",
			"0 blocking, 3 warning(s)")
		wantNone(t, out, "needed only for")
	})
}

// A word among the arguments and a bad flag are usage errors before anything
// is loaded or written, as for hermes-setup.
func TestInitUsageErrors(t *testing.T) {
	var out bytes.Buffer
	err := initCommand([]string{"extra"}, &out)
	if _, ok := err.(usageError); !ok || !strings.Contains(err.Error(), `init takes flags only, not "extra"`) {
		t.Errorf("err = %v, want a usageError about the word", err)
	}
	if out.Len() != 0 {
		t.Errorf("init ran, output:\n%s", out.String())
	}
}

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
