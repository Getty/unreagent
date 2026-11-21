package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// writeConfigFiles writes unreagent.yaml (and unreagent.local.yaml, if local
// is not empty) into dir and returns the path of unreagent.yaml.
func writeConfigFiles(t *testing.T, dir, base, local string) string {
	t.Helper()
	path := filepath.Join(dir, "unreagent.yaml")
	if err := os.WriteFile(path, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	if local != "" {
		if err := os.WriteFile(filepath.Join(dir, "unreagent.local.yaml"), []byte(local), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// writeUProject creates an (empty-JSON) .uproject at dir/rel.
func writeUProject(t *testing.T, dir, rel string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(p)
}

// noEngine makes ${ENGINE} unresolvable whatever the host has installed:
// UE_ROOT is cleared and auto-detection finds nothing.
func noEngine(t *testing.T) {
	t.Helper()
	t.Setenv("UE_ROOT", "")
	saved := autodetect
	autodetect = func(string) (string, string) { return "", "" }
	t.Cleanup(func() { autodetect = saved })
}

func errContains(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Errorf("Load succeeded; want an error mentioning %q", wants)
		return
	}
	for _, w := range wants {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not mention %q", err, w)
		}
	}
}

// k41: ${PROJECT_DIR} and ${PROJECT_NAME} are derived from unreal.project, and
// ${ENGINE} auto-detection reads the project's EngineAssociation, so a
// placeholder in unreal.project cannot be resolved. It used to fall through to
// globbing next to the config and leave the project empty; it must be a
// load-time error instead.
func TestProjectPlaceholderIsRejected(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, p := range []string{
		"${PROJECT_DIR}/Game/Game.uproject",
		"${PROJECT}",
		"Game/${PROJECT_NAME}.uproject",
		"${ENGINE}/Samples/Game/Game.uproject",
	} {
		dir := t.TempDir()
		writeUProject(t, dir, "Game/Game.uproject")
		_, _, err := Load(writeConfigFiles(t, dir, "unreal:\n  project: \""+p+"\"\n", ""))
		errContains(t, err, "unreal.project")
	}
}

// k41 control: the supported way to name a .uproject in a subdirectory, a
// path relative to the config file, resolves and feeds the placeholders.
func TestExplicitRelativeProjectResolves(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	dir := t.TempDir()
	want := writeUProject(t, dir, "Game/Game.uproject")
	c, info, err := Load(writeConfigFiles(t, dir, "unreal:\n  project: Game/Game.uproject\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	if c.Unreal.Project != want || info.Project != want || info.ProjectName != "Game" {
		t.Errorf("project = %q / %q / %q; want %q / %q / Game", c.Unreal.Project, info.Project, info.ProjectName, want, want)
	}
	if wantRoot := filepath.ToSlash(filepath.Join(dir, "Game")); c.Files.Root != wantRoot {
		t.Errorf("files.root = %q; want %q", c.Files.Root, wantRoot)
	}
	if args := c.Commands["compile"].Args; !hasString(args, "-Project="+want) {
		t.Errorf("compile args = %v; want -Project=%s", args, want)
	}
}

// k47 item 7: an explicit unreal.project is checked for existence at load
// time; a stale path used to load cleanly and fail later as a UE startup
// error. A directory is not a project file either.
func TestExplicitProjectMustExist(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Game"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"Missing.uproject", "Game"} {
		_, _, err := Load(writeConfigFiles(t, dir, "unreal:\n  project: "+p+"\n", ""))
		errContains(t, err, "unreal.project", p)
	}
}

// k42: every field that holds a path, a command, an argument, an environment
// value or a permission pattern gets the placeholders. Names, addresses, the
// token and descriptions deliberately stay literal.
func TestSubstitutionCoversPathAndArgumentFields(t *testing.T) {
	ue := filepath.ToSlash(t.TempDir())
	t.Setenv("UE_ROOT", ue)
	dir := t.TempDir()
	writeUProject(t, dir, "Game.uproject")
	pd := filepath.ToSlash(dir)
	base := `
agent:
  enabled: true
  command: ${PROJECT_DIR}/bin/claude
  args: ["--add-dir", "${ENGINE}/Engine/Source"]
  env: {GAME_HOME: "${PROJECT_DIR}/.home", "${PROJECT_NAME}": "key"}
  hermes: {configPath: "${PROJECT_DIR}/hermes.yaml", serverName: "${PROJECT_NAME}"}
mcp:
  enabled: true
  token: "${PROJECT_NAME}"
permissions:
  enabled: true
  allow: ["Edit(${PROJECT_DIR}/Source/*)"]
  deny: ["Bash(rm -rf ${PROJECT_DIR}*)"]
runtimes:
  python: {uv: "${PROJECT_DIR}/tools/uv"}
  node: {node: "${ENGINE}/node", npm: "${ENGINE}/npm"}
skills: {materialize: true, dir: "${PROJECT_DIR}/Skills"}
commands:
  hello: {description: "builds ${PROJECT_NAME}", command: echo}
`
	c, _, err := Load(writeConfigFiles(t, dir, base, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ field, got, want string }{
		{"agent.command", c.Agent.Command, pd + "/bin/claude"},
		{"agent.args[1]", c.Agent.Args[1], ue + "/Engine/Source"},
		{"agent.env.GAME_HOME", c.Agent.Env["GAME_HOME"], pd + "/.home"},
		{"agent.hermes.configPath", c.Agent.Hermes.ConfigPath, pd + "/hermes.yaml"},
		{"permissions.allow[0]", c.Permissions.Allow[0], "Edit(" + pd + "/Source/*)"},
		{"permissions.deny[0]", c.Permissions.Deny[0], "Bash(rm -rf " + pd + "*)"},
		{"runtimes.python.uv", c.Runtimes.Python.UV, pd + "/tools/uv"},
		{"runtimes.node.node", c.Runtimes.Node.Node, ue + "/node"},
		{"runtimes.node.npm", c.Runtimes.Node.Npm, ue + "/npm"},
		{"skills.dir", c.Skills.Dir, pd + "/Skills"},
		// Deliberately literal.
		{"agent.env key", c.Agent.Env["${PROJECT_NAME}"], "key"},
		{"agent.hermes.serverName", c.Agent.Hermes.ServerName, "${PROJECT_NAME}"},
		{"mcp.token", c.MCP.Token, "${PROJECT_NAME}"},
		{"commands.hello.description", c.Commands["hello"].Description, "builds ${PROJECT_NAME}"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q; want %q", tc.field, tc.got, tc.want)
		}
	}

	// The substituted rules decide as written: a permission rule with
	// ${PROJECT_DIR} used to stay literal and never match.
	if d := c.Permissions.Decide("Edit", map[string]any{"file_path": pd + "/Source/a.cpp"}); !d.Allow {
		t.Errorf("Edit inside Source denied: %s", d.Message)
	}
	if d := c.Permissions.Decide("Edit", map[string]any{"file_path": pd + "/Config/a.ini"}); d.Allow {
		t.Errorf("Edit outside Source allowed: %s", d.Message)
	}
	if d := c.Permissions.Decide("Bash", map[string]any{"command": "rm -rf " + pd + "/Saved"}); d.Allow {
		t.Errorf("deny rule with ${PROJECT_DIR} did not block: %s", d.Message)
	}

	// mcp.address stays literal too. A placeholder there is no host since
	// k162, so it is refused as written; substituted, it would load as
	// "Game:1".
	_, _, err = Load(writeConfigFiles(t, dir, "mcp: {enabled: true, address: \"${PROJECT_NAME}:1\"}\n", ""))
	errContains(t, err, `mcp.address invalid: "${PROJECT_NAME}:1"`)
}

// k43: a placeholder that resolved to "" in a field the launcher will use is a
// load-time error naming the field and the placeholder. It used to yield
// garbage like /Engine/Binaries/Win64/UnrealEditor.exe — or, in an allow rule,
// "Edit(/*)", which grants far more than written.
func TestUnresolvedPlaceholderInUsedFieldFails(t *testing.T) {
	noEngine(t)
	for _, tc := range []struct {
		name, yaml, field, placeholder string
	}{
		{"default editor, no engine", "", "unreal.editor", "${ENGINE}"},
		{"editor args, no project", "unreal: {editor: ue, args: [\"${PROJECT}\"]}\n", "unreal.args[0]", "${PROJECT}"},
		{"user command, no project", "unreal: {editor: ue}\ncommands:\n  hello: {command: echo, args: [\"${PROJECT_DIR}\"]}\n", "commands.hello.args[0]", "${PROJECT_DIR}"},
		{"user override of compile, no engine", "unreal: {editor: ue}\ncommands:\n  compile: {command: \"${ENGINE}/Build.bat\"}\n", "commands.compile.command", "${ENGINE}"},
		{"file tools, no project", "unreal: {editor: ue}\nmcp: {enabled: true}\nfiles: {enabled: true}\n", "files.root", "${PROJECT_DIR}"},
		{"agent args, no engine", "unreal: {editor: ue}\nagent: {enabled: true, args: [\"--add-dir\", \"${ENGINE}/Engine/Source\"]}\n", "agent.args[1]", "${ENGINE}"},
		{"agent workdir, no project", "unreal: {editor: ue}\nagent: {workdir: \"${PROJECT_DIR}\"}\n", "agent.workdir", "${PROJECT_DIR}"},
		{"allow rule, no project", "unreal: {editor: ue}\nmcp: {enabled: true}\npermissions: {enabled: true, allow: [\"Edit(${PROJECT_DIR}/*)\"]}\n", "permissions.allow[0]", "${PROJECT_DIR}"},
		{"python runtime, no project", "unreal: {editor: ue}\nruntimes: {python: {enabled: true, project: \"${PROJECT_DIR}/py\"}}\n", "runtimes.python.project", "${PROJECT_DIR}"},
		{"extra server, no project", "unreal: {editor: ue}\nmcp: {enabled: true, extraServers: {x: {command: \"${PROJECT_DIR}/srv\"}}}\n", "mcp.extraServers.x", "${PROJECT_DIR}"},
		{"hermes config, no project", "unreal: {editor: ue}\nagent: {hermes: {enabled: true, configPath: \"${PROJECT_DIR}/h.yaml\"}}\n", "agent.hermes.configPath", "${PROJECT_DIR}"},
		{"materialized skills, no project", "unreal: {editor: ue}\nskills: {materialize: true}\n", "skills.dir", "${PROJECT_DIR}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Load(writeConfigFiles(t, t.TempDir(), tc.yaml, ""))
			errContains(t, err, tc.field, tc.placeholder)
		})
	}
}

// k43: the same placeholders in fields the launcher will not use are not an
// error. The built-in compile and package commands, which need the engine and
// the project, are left out (and reported) instead of being offered as
// garbage, and files.root is cleared so that turning the file tools on later
// (-files) finds no root rather than a wrong one.
func TestUnresolvedPlaceholderInUnusedFieldLoads(t *testing.T) {
	noEngine(t)
	base := `
unreal: {editor: ue}
agent:
  enabled: false
  args: ["--add-dir", "${ENGINE}/Engine/Source"]
runtimes:
  python: {enabled: false, project: "${PROJECT_DIR}/py"}
commands:
  hello: {command: echo}
`
	c, info, err := Load(writeConfigFiles(t, t.TempDir(), base, ""))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range c.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"hello"}) {
		t.Errorf("commands = %v; want only hello", names)
	}
	if want := []string{"compile", "package"}; !reflect.DeepEqual(info.SkippedCommands, want) {
		t.Errorf("info.SkippedCommands = %v; want %v", info.SkippedCommands, want)
	}
	if c.Files.Root != "" {
		t.Errorf("files.root = %q; want empty while ${PROJECT_DIR} is unresolved", c.Files.Root)
	}
}

// example/unreagent.yaml ships as the demo config of every release: no engine,
// no .uproject, ping as the "editor". It has to keep loading.
func TestExampleDemoConfigLoads(t *testing.T) {
	noEngine(t)
	c, _, err := Load(filepath.Join("..", "..", "example", "unreagent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Unreal.Editor != "ping" || c.Commands["hello"].Command != "cmd" {
		t.Errorf("demo config: editor %q, hello %q", c.Unreal.Editor, c.Commands["hello"].Command)
	}
}

// k47 item 2, documented limitation: 0 (or unset) means the default delay —
// the restart delay is a floor against an unthrottled restart loop. A
// negative value used to slip past that floor as a zero delay; it is rejected.
func TestDelays(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	zero := "unreal: {restartDelaySeconds: 0}\nagent: {enabled: true, startDelaySeconds: 0, restartDelaySeconds: 0}\n"
	c, _, err := Load(writeConfigFiles(t, t.TempDir(), zero, ""))
	if err != nil {
		t.Fatal(err)
	}
	if c.Unreal.RestartDelaySeconds != 3 || c.Agent.RestartDelaySeconds != 3 || c.Agent.StartDelaySeconds != 5 {
		t.Errorf("delays = %d/%d/%d; want the defaults 3/3/5",
			c.Unreal.RestartDelaySeconds, c.Agent.RestartDelaySeconds, c.Agent.StartDelaySeconds)
	}
	for field, yaml := range map[string]string{
		"unreal.restartDelaySeconds": "unreal: {restartDelaySeconds: -1}\n",
		"agent.startDelaySeconds":    "agent: {enabled: true, startDelaySeconds: -1}\n",
		"agent.restartDelaySeconds":  "agent: {enabled: true, restartDelaySeconds: -1}\n",
	} {
		_, _, err := Load(writeConfigFiles(t, t.TempDir(), yaml, ""))
		errContains(t, err, field)
	}
}

// k47 item 3, documented limitation: null in the overlay is "not set", so
// unreal.args falls back to the built-in default; an empty list clears it.
func TestUnrealArgsNullVersusEmptyInOverlay(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	base := "unreal:\n  args: [\"-base\"]\n"
	for local, want := range map[string][]string{
		"unreal:\n  args: ~\n":  {"-stdout", "-FullStdOutLogOutput"},
		"unreal:\n  args: []\n": {},
	} {
		c, _, err := Load(writeConfigFiles(t, t.TempDir(), base, local))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c.Unreal.Args, want) {
			t.Errorf("local %q: unreal.args = %#v; want %#v", local, c.Unreal.Args, want)
		}
	}
}

// k47 item 4: mcp.writeConfig format is an enum; a typo used to fall through
// to mcp_json and was then logged as the format.
func TestWriteConfigFormat(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, f := range []string{"", "mcp_json", "vscode"} {
		yaml := "mcp:\n  enabled: true\n  writeConfig: [{path: a.json, format: \"" + f + "\"}]\n"
		if _, _, err := Load(writeConfigFiles(t, t.TempDir(), yaml, "")); err != nil {
			t.Errorf("format %q: %v", f, err)
		}
	}
	yaml := "mcp:\n  enabled: true\n  writeConfig: [{path: a.json, format: vscod}]\n"
	_, _, err := Load(writeConfigFiles(t, t.TempDir(), yaml, ""))
	errContains(t, err, "mcp.writeConfig[0].format", "vscod")
}

// k42 review F1: Load makes the config path absolute. With a relative -config
// path ${PROJECT_DIR} used to become "." or "sub": "Write(./*)" is cleaned to
// "*" by the matcher and allowed writing anywhere, and a relative deny rule
// never matched the absolute paths Claude Code sends.
func TestRelativeConfigPathYieldsAbsoluteRules(t *testing.T) {
	t.Setenv("UE_ROOT", filepath.ToSlash(t.TempDir()))
	base := `
mcp: {enabled: true}
permissions:
  enabled: true
  mode: allowlist
  allow: ["Write(${PROJECT_DIR}/*)"]
  deny: ["Write(${PROJECT_DIR}/Config/*)"]
`
	outside := filepath.ToSlash(t.TempDir()) + "/outside.txt"
	for _, rel := range []string{"unreagent.yaml", "sub/unreagent.yaml"} {
		t.Run(rel, func(t *testing.T) {
			root := t.TempDir()
			cfgDir := filepath.Join(root, filepath.FromSlash(filepath.Dir(rel)))
			writeUProject(t, cfgDir, "Game.uproject")
			writeConfigFiles(t, cfgDir, base, "")
			t.Chdir(root)
			pd, err := filepath.Abs(filepath.Dir(rel))
			if err != nil {
				t.Fatal(err)
			}
			pd = filepath.ToSlash(pd)

			c, info, err := Load(rel)
			if err != nil {
				t.Fatal(err)
			}
			if !filepath.IsAbs(info.ConfigPath) {
				t.Errorf("info.ConfigPath = %q; want absolute", info.ConfigPath)
			}
			if c.Unreal.Project != pd+"/Game.uproject" || c.Files.Root != pd {
				t.Errorf("project %q, files.root %q; want under %q", c.Unreal.Project, c.Files.Root, pd)
			}
			if want := "Write(" + pd + "/*)"; c.Permissions.Allow[0] != want {
				t.Errorf("allow[0] = %q; want %q", c.Permissions.Allow[0], want)
			}
			if want := "Write(" + pd + "/Config/*)"; c.Permissions.Deny[0] != want {
				t.Errorf("deny[0] = %q; want %q", c.Permissions.Deny[0], want)
			}
			for path, allow := range map[string]bool{
				outside:                 false, // the allow rule must not widen to "*"
				pd + "/Source/a.cpp":    true,
				pd + "/Config/Game.ini": false, // the deny rule must not be inert
			} {
				if d := c.Permissions.Decide("Write", map[string]any{"file_path": path}); d.Allow != allow {
					t.Errorf("Write %s: allow = %v (%s); want %v", path, d.Allow, d.Message, allow)
				}
			}
		})
	}
}

// k42 review F1, same hole via ${ENGINE}: a relative UE_ROOT or engineRoot
// is made absolute (against the working directory, where it pointed before)
// instead of being substituted into rules as typed.
func TestRelativeEngineRootIsAbsolute(t *testing.T) {
	for _, tc := range []struct{ name, env, yaml string }{
		{"UE_ROOT", "eng", "unreal: {}\n"},
		{"engineRoot", "", "engineRoot: eng\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("UE_ROOT", tc.env)
			root := t.TempDir()
			writeConfigFiles(t, root, tc.yaml, "")
			t.Chdir(root)
			want, err := filepath.Abs("eng")
			if err != nil {
				t.Fatal(err)
			}
			want = filepath.ToSlash(want)
			c, info, err := Load("unreagent.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if info.EngineRoot != want || c.Unreal.Editor != want+"/Engine/Binaries/Win64/UnrealEditor.exe" {
				t.Errorf("engine %q, editor %q; want under %q", info.EngineRoot, c.Unreal.Editor, want)
			}
		})
	}
}

// k88: the fallback warning belongs to auto-detection. An engine given by
// UE_ROOT or engineRoot is the user's choice; auto-detection is not even
// consulted then, so its warning cannot reach Info.Warnings. An auto-detected
// engine's warning does, once.
func TestEngineWarningOnlyFromAutodetection(t *testing.T) {
	ue := filepath.ToSlash(t.TempDir())
	const autoRoot, autoWarning = "/auto/UE_5.9", "auto-detection's warning"
	for _, tc := range []struct {
		name, env, yaml string
		wantRoot        string
		autodetected    bool
	}{
		{"auto-detected", "", "unreal: {}\n", autoRoot, true},
		{"UE_ROOT", ue, "unreal: {}\n", ue, false},
		{"engineRoot", "", "engineRoot: " + ue + "\n", ue, false},
		{"UE_ROOT and engineRoot", ue, "engineRoot: /other\n", ue, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("UE_ROOT", tc.env)
			var calls []string
			saved := autodetect
			autodetect = func(project string) (string, string) {
				calls = append(calls, project)
				return autoRoot, autoWarning
			}
			t.Cleanup(func() { autodetect = saved })
			dir := t.TempDir()
			project := writeUProject(t, dir, "Game.uproject")

			_, info, err := Load(writeConfigFiles(t, dir, tc.yaml, ""))
			if err != nil {
				t.Fatal(err)
			}
			if info.EngineRoot != tc.wantRoot {
				t.Errorf("engine root %q; want %q", info.EngineRoot, tc.wantRoot)
			}
			if !tc.autodetected {
				if len(calls) != 0 {
					t.Errorf("auto-detection consulted (for %q) although the engine is given", calls)
				}
				if len(info.Warnings) != 0 {
					t.Errorf("warnings %q; want none for an engine that is given", info.Warnings)
				}
				return
			}
			if !reflect.DeepEqual(calls, []string{project}) {
				t.Errorf("auto-detection called for %q; want once, for %q", calls, project)
			}
			if !reflect.DeepEqual(info.Warnings, []string{autoWarning}) {
				t.Errorf("warnings %q; want auto-detection's, once", info.Warnings)
			}
		})
	}
}

// k43 review F3: runtimes.node.npm is not only the node runtime's: the
// launcher runs it for every extra server started with node while mcp.enabled
// is set (prepareNodeBridge, npm install). An unresolved placeholder in it is
// then a load error even with the node runtime off.
func TestNpmIsUsedByNodeExtraServers(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	npm := "runtimes: {node: {enabled: false, npm: \"${PROJECT_DIR}/npm\"}}\nunreal: {editor: ue}\n"
	for _, tc := range []struct {
		name, mcp string
		used      bool
	}{
		{"node server", "mcp: {enabled: true, extraServers: {b: {command: node, args: [b.js]}}}\n", true},
		{"node.exe by path", "mcp: {enabled: true, extraServers: {b: {command: \"C:/Program Files/nodejs/NODE.EXE\"}}}\n", true},
		{"http server only", "mcp: {enabled: true, extraServers: {b: {type: http, url: \"http://127.0.0.1:1/mcp\"}}}\n", false},
		{"mcp disabled", "mcp: {enabled: false, extraServers: {b: {command: node}}}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Load(writeConfigFiles(t, t.TempDir(), npm+tc.mcp, ""))
			if tc.used {
				errContains(t, err, "runtimes.node.npm", "${PROJECT_DIR}")
			} else if err != nil {
				t.Errorf("npm is not used here, yet: %v", err)
			}
		})
	}
}

// k124: yaml.v3 decodes one YAML document at a time, and Load read only the
// first one of unreagent.yaml and of unreagent.local.yaml: whatever followed a
// second "---" was dropped without a word. A file that holds more than one is
// a load error naming the file and the line the next document starts on. A
// document that holds nothing but a comment counts, as in hermes-setup (k118).
func TestLoadRefusesMultipleDocuments(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for name, c := range map[string]struct {
		yaml string
		line int
	}{
		"two documents":                    {"mcp:\n  address: 127.0.0.1:1111\n---\nmcp:\n  address: 127.0.0.1:2222\n", 3},
		"after an explicit end":            {"mcp:\n  address: 127.0.0.1:1111\n...\n---\nmcp:\n  address: 127.0.0.1:2222\n", 4},
		"content after an empty first one": {"---\n---\nmcp:\n  address: 127.0.0.1:2222\n", 2},
		"a comment-only second document":   {"mcp:\n  address: 127.0.0.1:1111\n--- # kept for later\n", 3},
	} {
		for _, file := range []string{"unreagent.yaml", "unreagent.local.yaml"} {
			t.Run(name+"/"+file, func(t *testing.T) {
				base, local := c.yaml, ""
				if file == "unreagent.local.yaml" {
					base, local = "", c.yaml
				}
				_, _, err := Load(writeConfigFiles(t, t.TempDir(), base, local))
				want := fmt.Sprintf("%s: it holds more than one YAML document (another one starts at line %d); refusing to drop all but the first", file, c.line)
				if err == nil {
					t.Fatal("Load succeeded; want an error for a multi-document file")
				}
				if err.Error() != want {
					t.Errorf("error %q,\nwant %q", err, want)
				}
			})
		}
	}
}

// k124 control: a bare "---" or "..." at the end starts or ends a document
// that holds nothing, so there is nothing to drop and the file loads, in
// either file, as in hermes-setup (k118). A comment after a trailing "---"
// belongs to the first document for yaml.v3.
func TestLoadAcceptsEmptyTrailingDocument(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	const doc = "mcp:\n  address: 127.0.0.1:2222\n"
	for name, yaml := range map[string]string{
		"trailing ---":             doc + "---\n",
		"trailing ...":             doc + "...\n",
		"trailing --- then ...":    doc + "---\n...\n",
		"two trailing ---":         doc + "---\n---\n",
		"comment after the ---":    doc + "---\n# the end\n",
		"leading and trailing ---": "---\n" + doc + "---\n",
		"comment before a ---":     "# machine overrides\n---\n" + doc,
		"CRLF line breaks":         strings.ReplaceAll(doc+"---\n", "\n", "\r\n"),
	} {
		for _, file := range []string{"unreagent.yaml", "unreagent.local.yaml"} {
			t.Run(name+"/"+file, func(t *testing.T) {
				base, local := yaml, ""
				if file == "unreagent.local.yaml" {
					base, local = "mcp:\n  address: 127.0.0.1:1111\n", yaml
				}
				c, _, err := Load(writeConfigFiles(t, t.TempDir(), base, local))
				if err != nil {
					t.Fatal(err)
				}
				if c.MCP.Address != "127.0.0.1:2222" {
					t.Errorf("mcp.address = %q; want 127.0.0.1:2222 from %s", c.MCP.Address, file)
				}
			})
		}
	}
}

// k124: Load looked for unreagent.local.yaml with a check that follows a
// symlink, so a dangling or looping one read as "no overlay", and the machine
// overrides in it were missing without a word. Only a path that does not
// exist is absent: a symlink there that leads to no file is a load error
// naming it, and the end of a chain of links that is missing. The check runs
// before anything else is read from the overlay.
func TestLoadRefusesBrokenLocalOverlayLink(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for name, link := range map[string]func(t *testing.T, local string) (want string, exact bool){
		"dangling": func(t *testing.T, local string) (string, bool) {
			missing := filepath.Join(t.TempDir(), "gone", "unreagent.local.yaml")
			symlink(t, missing, local)
			return "config not readable: " + local + " is a symlink to " + missing + ", which does not exist", true
		},
		"dangling chain": func(t *testing.T, local string) (string, bool) {
			shared, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			hop := filepath.Join(shared, "hop.yaml")
			symlink(t, hop, local)
			symlink(t, "gone.yaml", hop) // relative: next to hop
			return "config not readable: " + local + " is a symlink to " + hop + "; following the links leads to " +
				filepath.Join(shared, "gone.yaml") + ", which does not exist", true
		},
		"loop": func(t *testing.T, local string) (string, bool) {
			partner := local + ".partner"
			symlink(t, partner, local)
			symlink(t, local, partner)
			return "config not readable: " + local + " is a symlink that cannot be resolved: ", false
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeConfigFiles(t, dir, "mcp:\n  address: 127.0.0.1:1111\n", "")
			want, exact := link(t, filepath.Join(dir, "unreagent.local.yaml"))

			_, _, err := Load(path)
			if err == nil {
				t.Fatal("Load succeeded; want an error for an overlay symlink that leads to no file")
			}
			got, want := filepath.ToSlash(err.Error()), filepath.ToSlash(want)
			if exact && got != want || !exact && !strings.HasPrefix(got, want) {
				t.Errorf("error %q,\nwant %q", got, want)
			}
		})
	}
}

// k124 review: only a path that does not exist is an absent overlay; any other
// error from looking for it is a load error naming the overlay path. A config
// named with 250 bytes gets an overlay name of 256 bytes, one past NAME_MAX on
// Linux, so the lookup fails with "file name too long". No overlay can exist
// under that name, but Load does not sort errors into harmless ones: ce31def
// skipped the overlay here, as it did for a broken symlink.
func TestLoadReportsOverlayLookupError(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	dir := t.TempDir()
	stem := strings.Repeat("u", 245)
	path := filepath.Join(dir, stem+".yaml")
	if err := os.WriteFile(path, []byte("mcp:\n  address: 127.0.0.1:1111\n"), 0o644); err != nil {
		t.Skipf("cannot create a config with a 250-byte name here: %v", err)
	}
	local := filepath.Join(dir, stem+".local.yaml")
	_, lookupErr := os.Lstat(local)
	if lookupErr == nil || os.IsNotExist(lookupErr) {
		t.Skipf("this file system takes a 256-byte name (lstat: %v)", lookupErr)
	}

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded; want an error for an overlay that cannot be looked up")
	}
	if want := "config not readable (" + local + "): " + lookupErr.Error(); err.Error() != want {
		t.Errorf("error %q,\nwant %q", err, want)
	}
}

// k124 control: a symlinked unreagent.local.yaml that resolves is overlaid as
// before.
func TestLoadFollowsLocalOverlayLink(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	dir := t.TempDir()
	path := writeConfigFiles(t, dir, "mcp:\n  address: 127.0.0.1:1111\n", "")
	target := filepath.Join(t.TempDir(), "machine.yaml")
	if err := os.WriteFile(target, []byte("mcp:\n  address: 127.0.0.1:2222\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(dir, "unreagent.local.yaml")
	symlink(t, target, local)

	c, info, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.LocalPath != local || c.MCP.Address != "127.0.0.1:2222" {
		t.Errorf("LocalPath %q, mcp.address %q; want %q, 127.0.0.1:2222", info.LocalPath, c.MCP.Address, local)
	}
}

// k157: findConfig looked for unreagent.yaml and unreagent.yml with a check
// that follows a symlink, so a dangling or looping unreagent.yaml read as "not
// there", and an unreagent.yml beside it was loaded without a word although
// the link named the config the user meant. A candidate that is a symlink
// leading to no file stops the search with an error naming it, and no later
// candidate is tried; only one that does not exist falls through.
func TestFindConfigRefusesBrokenLink(t *testing.T) {
	for name, tc := range map[string]struct {
		broken string // the candidate that is a broken link
		yml    bool   // a real unreagent.yml beside it
		loop   bool   // a loop instead of a dangling link
	}{
		"dangling unreagent.yaml beside unreagent.yml": {broken: "unreagent.yaml", yml: true},
		"looping unreagent.yaml beside unreagent.yml":  {broken: "unreagent.yaml", yml: true, loop: true},
		"dangling unreagent.yml, no unreagent.yaml":    {broken: "unreagent.yml"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.yml {
				if err := os.WriteFile(filepath.Join(dir, "unreagent.yml"), []byte("mcp:\n  address: 127.0.0.1:1111\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(dir, tc.broken)
			want, exact := "", true
			if tc.loop {
				partner := filepath.Join(t.TempDir(), "partner.yaml")
				symlink(t, partner, link)
				symlink(t, link, partner)
				want, exact = "config not readable: "+link+" is a symlink that cannot be resolved: ", false
			} else {
				missing := filepath.Join(t.TempDir(), "gone", tc.broken)
				symlink(t, missing, link)
				want = "config not readable: " + link + " is a symlink to " + missing + ", which does not exist"
			}

			path, err := findConfig(dir)
			if err == nil {
				t.Fatalf("findConfig = %q; want an error for %s, a symlink that leads to no file", path, tc.broken)
			}
			got, want := filepath.ToSlash(err.Error()), filepath.ToSlash(want)
			if exact && got != want || !exact && !strings.HasPrefix(got, want) {
				t.Errorf("error %q,\nwant %q", got, want)
			}
		})
	}
}

// k157 control: where findConfig gives no error, it picks what it picked
// before — the first of unreagent.yaml and unreagent.yml that is there, else
// unreagent.yaml — and a symlinked candidate that resolves is used under its
// own name and loads.
func TestFindConfigPicksCandidate(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	write := func(t *testing.T, path, address string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("mcp:\n  address: "+address+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, tc := range map[string]struct {
		setup func(t *testing.T, dir string)
		want  string // the name findConfig picks
		addr  string // mcp.address Load reads from it; "" = not loaded
	}{
		"neither": {func(*testing.T, string) {}, "unreagent.yaml", ""},
		"only unreagent.yml": {func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "unreagent.yml"), "127.0.0.1:2222")
		}, "unreagent.yml", "127.0.0.1:2222"},
		"both": {func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "unreagent.yaml"), "127.0.0.1:1111")
			write(t, filepath.Join(dir, "unreagent.yml"), "127.0.0.1:2222")
		}, "unreagent.yaml", "127.0.0.1:1111"},
		"unreagent.yaml linked to a file": {func(t *testing.T, dir string) {
			target := filepath.Join(t.TempDir(), "shared.yaml")
			write(t, target, "127.0.0.1:3333")
			symlink(t, target, filepath.Join(dir, "unreagent.yaml"))
			write(t, filepath.Join(dir, "unreagent.yml"), "127.0.0.1:2222")
		}, "unreagent.yaml", "127.0.0.1:3333"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)

			path, err := findConfig(dir)
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(dir, tc.want); path != want {
				t.Fatalf("findConfig = %q; want %q", path, want)
			}
			if tc.addr == "" {
				return
			}
			c, _, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if c.MCP.Address != tc.addr {
				t.Errorf("mcp.address = %q; want %q", c.MCP.Address, tc.addr)
			}
		})
	}
}

// symlink creates the symlink link -> target, and skips the test where the
// system or the account cannot create one (Windows without the privilege or
// developer mode).
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
}

// k104: a path rule for a file tool has to name an absolute path below a file
// system root. Anything else is a load error naming the rule — not an allow
// rule that cleaning widens to "*" ("Write(./*)" granted Write on /etc/passwd)
// or a deny rule that never meets the absolute paths the tools send.
func TestPathRulesMustBeAbsolute(t *testing.T) {
	t.Setenv("UE_ROOT", filepath.ToSlash(t.TempDir()))
	load := func(t *testing.T, enabled bool, list, rule string) error {
		t.Helper()
		dir := t.TempDir()
		writeUProject(t, dir, "Game.uproject")
		yaml := fmt.Sprintf("mcp: {enabled: true}\npermissions:\n  enabled: %v\n  mode: allowlist\n  %s: ['%s']\n", enabled, list, rule)
		_, _, err := Load(writeConfigFiles(t, dir, yaml, ""))
		return err
	}

	climb := "${PROJECT_DIR}" + strings.Repeat("/..", 64) + "/*"
	for _, tc := range []struct{ name, list, rule, why string }{
		{"dot star", "allow", "Write(./*)", "is not an absolute path"},
		{"dot-dot star", "allow", "Write(x/../*)", "is not an absolute path"},
		{"bare star", "allow", "Write(*)", "is not an absolute path"},
		{"relative directory", "allow", "Read(Source/*)", "is not an absolute path"},
		{"empty pattern", "allow", "Edit()", "is not an absolute path"},
		{"drive-relative", "allow", `Edit(C:*)`, "is not an absolute path"},
		{"POSIX root", "allow", "Read(/*)", "names a file system root"},
		{"drive root", "allow", `Edit(C:\*)`, "names a file system root"},
		{"traversal to the root", "allow", "MultiEdit(/proj/../*)", "names a file system root"},
		{"placeholder climbed to the root", "allow", "Write(" + climb + ")", "names a file system root"},
		{"relative deny rule", "deny", "Edit(Config/*)", "is not an absolute path"},
		{"notebook deny rule", "deny", "NotebookEdit(*)", "is not an absolute path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The bare tool name is the way to cover every path, and the
			// message says so in the list's own words.
			every := `to allow every path, write the bare tool name`
			if tc.list == "deny" {
				every = `to deny every path, write the bare tool name`
			}
			errContains(t, load(t, true, tc.list, tc.rule), "permissions."+tc.list+"[0]", tc.why, every)
		})
	}

	for _, tc := range []struct{ name, list, rule string }{
		{"absolute POSIX rule", "allow", "Write(/proj/Source/*)"},
		{"absolute drive rule", "allow", `Edit(C:\Proj\Source\*)`},
		{"placeholder rule", "deny", "Write(${PROJECT_DIR}/Config/*)"},
		// A deny rule can only block: one anchored at a root blocks that
		// whole file system and is not refused.
		{"deny at the POSIX root", "deny", "Read(/*)"},
		{"deny at a drive root", "deny", `Write(C:\*)`},
		{"deny traversing to the root", "deny", "MultiEdit(/proj/../*)"},
		{"UNC rule", "allow", `Read(\\server\share\proj\*)`},
		{"bare tool name", "allow", "Write"},
		{"Bash rule with a relative path", "allow", "Bash(./build.sh *)"},
		{"MCP tool rule, not known to be a path", "allow", "mcp__foo__bar(secret*)"},
		{"Grep rule, pattern or path", "deny", "Grep(TODO*)"},
	} {
		t.Run("accepted: "+tc.name, func(t *testing.T) {
			if err := load(t, true, tc.list, tc.rule); err != nil {
				t.Errorf("%s %q rejected: %v", tc.list, tc.rule, err)
			}
		})
	}

	t.Run("not checked while permissions are disabled", func(t *testing.T) {
		if err := load(t, false, "allow", "Write(./*)"); err != nil {
			t.Errorf("disabled permissions rejected an unused rule: %v", err)
		}
	})
}

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
