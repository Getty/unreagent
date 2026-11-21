package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// userHermesConfig is a Hermes config as a user has it before running
// hermes-setup: foreign top-level keys and another MCP server.
const userHermesConfig = `# my Hermes setup
model: hermes-3-405b
api_key: sk-keep-me
toolsets:
  - web
  - terminal
mcp_servers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
`

// hermesFixture is a temp directory holding an unreagent.yaml that enables
// Hermes and points agent.hermes.configPath at hermes/config.yaml beside it.
type hermesFixture struct {
	dir        string
	cfgPath    string
	hermesPath string
}

// newHermesFixture writes the fixture. token "" leaves mcp.token unset (the
// command then generates one); hermesConfig "" leaves the Hermes config absent.
func newHermesFixture(t *testing.T, token, hermesConfig string) hermesFixture {
	t.Helper()
	dir := t.TempDir()
	f := hermesFixture{
		dir:        dir,
		cfgPath:    filepath.Join(dir, "unreagent.yaml"),
		hermesPath: filepath.Join(dir, "hermes", "config.yaml"),
	}
	if err := os.MkdirAll(filepath.Dir(f.hermesPath), 0o755); err != nil {
		t.Fatal(err)
	}
	f.writeConfig(t, token)
	if hermesConfig != "" {
		writeFile(t, f.hermesPath, hermesConfig)
	}
	return f
}

// writeConfig (re)writes unreagent.yaml with the given mcp.token.
func (f hermesFixture) writeConfig(t *testing.T, token string) {
	t.Helper()
	cfg := fmt.Sprintf(`engineRoot: %q
unreal:
  editor: ping
agent:
  hermes:
    enabled: true
    configPath: %q
mcp:
  address: 127.0.0.1:8765
`, filepath.Join(f.dir, "no-engine"), f.hermesPath)
	if token != "" {
		cfg += fmt.Sprintf("  token: %q\n", token)
	}
	writeFile(t, f.cfgPath, cfg)
}

// run executes `hermes-setup -config <fixture>` and returns its output. A
// panic fails the test instead of killing the test binary.
func (f hermesFixture) run(t *testing.T) (string, error) {
	t.Helper()
	var out bytes.Buffer
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("hermes-setup panicked: %v\noutput so far:\n%s", r, out.String())
		}
	}()
	err := hermesSetup([]string{"-config", f.cfgPath}, &out)
	return out.String(), err
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readYAMLMap(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	doc := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatalf("%s is not valid YAML: %v", path, err)
	}
	return doc
}

// lookup walks nested string-keyed maps; nil if any step is missing.
func lookup(doc map[string]interface{}, keys ...string) interface{} {
	var cur interface{} = doc
	for _, k := range keys {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// assertUserConfigKept checks that every key of userHermesConfig survived.
func assertUserConfigKept(t *testing.T, doc map[string]interface{}) {
	t.Helper()
	for _, c := range []struct {
		keys []string
		want interface{}
	}{
		{[]string{"model"}, "hermes-3-405b"},
		{[]string{"api_key"}, "sk-keep-me"},
		{[]string{"toolsets"}, []interface{}{"web", "terminal"}},
		{[]string{"mcp_servers", "github", "command"}, "npx"},
		{[]string{"mcp_servers", "github", "args"}, []interface{}{"-y", "@modelcontextprotocol/server-github"}},
	} {
		if got := lookup(doc, c.keys...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %#v, want %#v (user's config not preserved)", strings.Join(c.keys, "."), got, c.want)
		}
	}
}

func assertUnreagentEntry(t *testing.T, doc map[string]interface{}, token string) {
	t.Helper()
	if got, want := lookup(doc, "mcp_servers", "unreagent", "url"), "http://127.0.0.1:8765/mcp"; got != want {
		t.Errorf("mcp_servers.unreagent.url = %#v, want %q", got, want)
	}
	if got, want := lookup(doc, "mcp_servers", "unreagent", "headers", "Authorization"), "Bearer "+token; got != want {
		t.Errorf("mcp_servers.unreagent.headers.Authorization = %#v, want %q", got, want)
	}
}

// k39: the existing Hermes config is merged into, not replaced — foreign keys
// and other MCP servers survive — and the original is backed up byte for byte.
func TestHermesSetupKeepsExistingConfig(t *testing.T) {
	const token = "user-token-0123456789"
	f := newHermesFixture(t, token, userHermesConfig)

	if _, err := f.run(t); err != nil {
		t.Fatal(err)
	}

	doc := readYAMLMap(t, f.hermesPath)
	assertUserConfigKept(t, doc)
	assertUnreagentEntry(t, doc, token)
	if got := readFile(t, f.hermesPath+".bak"); got != userHermesConfig {
		t.Errorf(".bak is not the original config:\n%s", got)
	}
}

// k39: a second run still starts from the user's config, refreshes the
// unreagent entry, and keeps the first run's backup (the untouched original)
// instead of overwriting it.
func TestHermesSetupSecondRunKeepsConfigAndFirstBackup(t *testing.T) {
	f := newHermesFixture(t, "first-token-0123456789", userHermesConfig)
	if _, err := f.run(t); err != nil {
		t.Fatal(err)
	}

	const second = "second-token-0123456789"
	f.writeConfig(t, second)
	out, err := f.run(t)
	if err != nil {
		t.Fatal(err)
	}

	doc := readYAMLMap(t, f.hermesPath)
	assertUserConfigKept(t, doc)
	assertUnreagentEntry(t, doc, second)
	if got := readFile(t, f.hermesPath+".bak"); got != userHermesConfig {
		t.Errorf(".bak was overwritten on the second run:\n%s", got)
	}
	if !strings.Contains(out, "Backup kept: ") {
		t.Errorf("second run does not report the kept backup:\n%s", out)
	}
}

// An existing .bak is never overwritten. The scenario is what releases up to
// 0.0.9 left behind: the user's original moved to .bak, the config holding
// only the unreagent entry. Re-running must not replace that only copy.
func TestHermesSetupNeverOverwritesExistingBackup(t *testing.T) {
	const token = "user-token-0123456789"
	managedOnly := hermesLegacyMarker + "\nmcp_servers:\n    unreagent:\n        url: http://127.0.0.1:8765/mcp\n"
	f := newHermesFixture(t, token, managedOnly)
	writeFile(t, f.hermesPath+".bak", userHermesConfig)

	out, err := f.run(t)
	if err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, f.hermesPath+".bak"); got != userHermesConfig {
		t.Errorf("existing .bak was overwritten:\n%s", got)
	}
	if !strings.Contains(out, "Backup kept: ") {
		t.Errorf("output does not report the kept backup:\n%s", out)
	}
	assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
}

// A missing or 0-byte config has nothing worth backing up, so no .bak is
// written — an empty one would count as "already backed up" and block the
// backup of every later run. The second run proves that: it backs up the file
// the first run wrote.
func TestHermesSetupNoBackupForMissingOrEmptyConfig(t *testing.T) {
	for name, create := range map[string]bool{"missing": false, "empty": true} {
		t.Run(name, func(t *testing.T) {
			const token = "user-token-0123456789"
			f := newHermesFixture(t, token, "")
			if create {
				writeFile(t, f.hermesPath, "")
			}

			out, err := f.run(t)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(f.hermesPath + ".bak"); !os.IsNotExist(err) {
				t.Errorf("first run created a .bak for a %s config (stat: %v)", name, err)
			}
			if strings.Contains(out, "Backup: ") || strings.Contains(out, "Backup kept: ") {
				t.Errorf("first run reports a backup for a %s config:\n%s", name, out)
			}

			written := readFile(t, f.hermesPath)
			out, err = f.run(t)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "Backup: ") {
				t.Errorf("second run did not create a backup:\n%s", out)
			}
			if got := readFile(t, f.hermesPath+".bak"); got != written {
				t.Errorf(".bak is not the config the first run wrote:\n%s", got)
			}
		})
	}
}

// A config without a mapping to merge into (a YAML null, or nothing but a
// document marker or comments) is treated as empty, not as a nil map.
func TestHermesSetupEmptyDocuments(t *testing.T) {
	for name, hermesConfig := range map[string]string{
		"null":            "null\n",
		"tilde":           "~\n",
		"document marker": "---\n",
		"comments only":   "# nothing configured yet\n",
	} {
		t.Run(name, func(t *testing.T) {
			const token = "user-token-0123456789"
			f := newHermesFixture(t, token, hermesConfig)

			if _, err := f.run(t); err != nil {
				t.Fatal(err)
			}
			assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
			if got := readFile(t, f.hermesPath+".bak"); got != hermesConfig {
				t.Errorf(".bak is not the original config:\n%s", got)
			}
		})
	}
}

// mcp_servers that is not a mapping with string keys cannot be merged into
// without dropping what is there, so the command refuses and touches nothing.
// Neither can an mcp_servers that is an alias, carries an anchor, or comes in
// through a merge key (k96): adding to it would change the anchored mapping or
// its aliases, or hide the merged servers. An unreagent entry that carries an
// anchor, on itself or below, is not replaced either: the aliases of that
// anchor would be left undefined. A duplicate key is refused as unparsable, as
// before k96. Each refusal names its reason: checkEdit would refuse most of
// these inputs, too, but with a message that does not say why.
func TestHermesSetupRefusesUnmergeableServers(t *testing.T) {
	for name, c := range map[string]struct{ config, wantErr string }{
		"list":                 {"model: hermes-3\nmcp_servers:\n  - github\n", "mcp_servers is not a mapping of server names"},
		"non-string key":       {"model: hermes-3\nmcp_servers:\n  8080:\n    url: http://127.0.0.1:8080/mcp\n  github:\n    command: npx\n", "mcp_servers is not a mapping of server names"},
		"alias":                {"shared: &servers\n  github:\n    command: npx\nmcp_servers: *servers\n", "mcp_servers is an alias (*servers); adding to it would change the anchored mapping too; refusing to change it"},
		"anchored mcp_servers": {"mcp_servers: &srv\n  github:\n    command: npx\nbackup_servers: *srv\n", "mcp_servers carries the anchor &srv; adding to it would change its aliases too; refusing to change it"},
		"merge key":            {"base: &base\n  mcp_servers:\n    github:\n      command: npx\n<<: *base\nmodel: hermes-3\n", "mcp_servers comes in through a merge key (<<); refusing to override it"},
		"duplicate key":        {"mcp_servers:\n  github:\n    command: npx\nmcp_servers:\n  other:\n    command: x\n", `mapping key "mcp_servers" already defined at line 1`},
		"anchor on the entry": {`model: hermes-3
mcp_servers:
  unreagent: &unreagent
    url: http://127.0.0.1:8765/mcp
    headers:
      Authorization: Bearer old-token
  unreagent-logs:
    <<: *unreagent
    tools:
      include: [logs, status]
`, "mcp_servers.unreagent carries the anchor &unreagent, which other parts of the config may use; refusing to replace it"},
		"anchor inside the entry": {`mcp_servers:
  unreagent:
    url: http://old.invalid/mcp
    headers: &h
      Authorization: Bearer old
  other:
    url: http://127.0.0.1:9000/mcp
    headers: *h
`, "mcp_servers.unreagent carries the anchor &h, which other parts of the config may use; refusing to replace it"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "user-token-0123456789", c.config)

			if _, err := f.run(t); err == nil {
				t.Error("expected an error for an unmergeable mcp_servers, got nil")
			} else if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error %q does not give the reason %q", err, c.wantErr)
			}
			if got := readFile(t, f.hermesPath); got != c.config {
				t.Errorf("config was changed:\n%s", got)
			}
			if _, err := os.Stat(f.hermesPath + ".bak"); !os.IsNotExist(err) {
				t.Errorf("a .bak was created although nothing was written (stat: %v)", err)
			}
		})
	}
}

// k53: the bearer summary line never panics on a short token, makes the
// "64 hex chars, in unreagent.local.yaml" claim only for a token this command
// generated, and does not echo a token the user set.
func TestHermesSetupBearerSummary(t *testing.T) {
	for _, c := range []struct {
		name, token, secret string
	}{
		{"user token shorter than eight characters", "tk5zq", "tk5zq"},
		{"user token longer than eight characters", "Zq9vK2mX-user-token-long", "Zq9vK2mX"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newHermesFixture(t, c.token, "")

			out, err := f.run(t)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "64 hex chars") {
				t.Errorf("claims a generated token for a user-set one:\n%s", out)
			}
			if strings.Contains(out, c.secret) {
				t.Errorf("echoes the user's token:\n%s", out)
			}
			assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), c.token)
		})
	}

	t.Run("generated token", func(t *testing.T) {
		f := newHermesFixture(t, "", "")

		out, err := f.run(t)
		if err != nil {
			t.Fatal(err)
		}
		token, _ := lookup(readYAMLMap(t, localOverridePath(f.cfgPath)), "mcp", "token").(string)
		if len(token) != 64 {
			t.Fatalf("unreagent.local.yaml mcp.token = %q, want 64 hex chars", token)
		}
		if want := token[:8] + "… (64 hex chars, in unreagent.local.yaml)"; !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
		assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
	})
}

// commentedHermesConfig has comments in every position a user puts them,
// top-level keys and MCP servers in no sorted order, and a flow sequence.
const commentedHermesConfig = `# my Hermes setup
zeta: first-in-the-file   # though last alphabetically
model: hermes-3-405b
# tools I use
toolsets:
  - web
  - terminal
mcp_servers:
  # the GitHub one
  zz-github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
  aa-local:
    url: http://127.0.0.1:9000/mcp  # local experiment
api_key: sk-keep-me
# end of file
`

// commentsIn returns every comment of a YAML text, from the # to the end of
// its line.
func commentsIn(text string) []string {
	var comments []string
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			comments = append(comments, line[i:])
		}
	}
	return comments
}

// assertCommentsKept checks that every comment of the original text is in got.
func assertCommentsKept(t *testing.T, what, original, got string) {
	t.Helper()
	for _, c := range commentsIn(original) {
		if !strings.Contains(got, c) {
			t.Errorf("%s lost the comment %q:\n%s", what, c, got)
		}
	}
}

// assertInOrder checks that the parts occur in text in the given order.
func assertInOrder(t *testing.T, what, text string, parts ...string) {
	t.Helper()
	at := -1
	for _, p := range parts {
		i := strings.Index(text, p)
		if i < 0 {
			t.Errorf("%s lacks %q:\n%s", what, p, text)
			return
		}
		if i < at {
			t.Errorf("%s: %q is out of order:\n%s", what, p, text)
			return
		}
		at = i
	}
}

// k96: the merge keeps the user's config as written — comments, key order,
// flow style and the other MCP servers — on the first run and on a re-run.
func TestHermesSetupKeepsCommentsAndKeyOrder(t *testing.T) {
	const token = "user-token-0123456789"
	f := newHermesFixture(t, token, commentedHermesConfig)

	for run := 1; run <= 2; run++ {
		if _, err := f.run(t); err != nil {
			t.Fatal(err)
		}
		got := readFile(t, f.hermesPath)
		what := fmt.Sprintf("run %d: Hermes config", run)
		assertCommentsKept(t, what, commentedHermesConfig, got)
		assertInOrder(t, what, got, "zeta:", "model:", "toolsets:", "mcp_servers:",
			"zz-github:", "aa-local:", "unreagent:", "api_key:")
		if want := `args: ["-y", "@modelcontextprotocol/server-github"]`; !strings.Contains(got, want) {
			t.Errorf("%s lost the flow style of %q:\n%s", what, want, got)
		}

		doc := readYAMLMap(t, f.hermesPath)
		assertUnreagentEntry(t, doc, token)
		for _, c := range []struct {
			keys []string
			want interface{}
		}{
			{[]string{"zeta"}, "first-in-the-file"},
			{[]string{"api_key"}, "sk-keep-me"},
			{[]string{"mcp_servers", "zz-github", "command"}, "npx"},
			{[]string{"mcp_servers", "aa-local", "url"}, "http://127.0.0.1:9000/mcp"},
		} {
			if got := lookup(doc, c.keys...); !reflect.DeepEqual(got, c.want) {
				t.Errorf("%s: %s = %#v, want %#v", what, strings.Join(c.keys, "."), got, c.want)
			}
		}
	}
}

// k96: a config with Windows line endings keeps its comments and gains no
// blank lines. The result has LF line endings, and the .bak keeps the CRLF original.
func TestHermesSetupCRLFConfig(t *testing.T) {
	const token = "user-token-0123456789"
	const hermesConfig = "# my Hermes setup\r\nmodel: x\r\n# the servers\r\nmcp_servers:\r\n  github:\r\n    command: npx  # via node\r\n"
	f := newHermesFixture(t, token, hermesConfig)

	if _, err := f.run(t); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, f.hermesPath)
	assertCommentsKept(t, "Hermes config", strings.ReplaceAll(hermesConfig, "\r\n", "\n"), got)
	if strings.Contains(got, "\n\n") || strings.Contains(got, "\r") {
		t.Errorf("Hermes config gained a blank line or kept a CR: %q", got)
	}
	assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
	if got := readFile(t, f.hermesPath+".bak"); got != hermesConfig {
		t.Errorf(".bak is not the original config: %q", got)
	}
}

// k96: a comment the user put after the unreagent entry survives the run that
// replaces the entry (yaml.v3 hangs such a comment on the entry's last line).
func TestHermesSetupKeepsCommentAfterReplacedEntry(t *testing.T) {
	const token = "user-token-0123456789"
	const hermesConfig = `mcp_servers:
  github:
    command: npx
  unreagent:
    url: http://old.invalid/mcp
  # my-server:
  #   command: not-yet
model: x
`
	f := newHermesFixture(t, token, hermesConfig)

	if _, err := f.run(t); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, f.hermesPath)
	assertCommentsKept(t, "Hermes config", hermesConfig, got)
	assertInOrder(t, "Hermes config", got, "github:", "unreagent:", "# my-server:", "model:")
	assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
}

// k96: a config that is nothing but comments, or a null with comments, keeps
// those comments; yaml.v3 alone drops a comment-only document.
func TestHermesSetupKeepsCommentsOfEmptyConfig(t *testing.T) {
	for name, hermesConfig := range map[string]string{
		"comments only":          "# nothing configured yet\n# model: hermes-3\n",
		"comment above null":     "# reset on purpose\nnull\n",
		"null mcp_servers":       "model: x\nmcp_servers: ~  # none yet\n",
		"commented-out mappings": "model: x\n# mcp_servers:\n#   old: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			const token = "user-token-0123456789"
			f := newHermesFixture(t, token, hermesConfig)

			if _, err := f.run(t); err != nil {
				t.Fatal(err)
			}
			assertCommentsKept(t, "Hermes config", hermesConfig, readFile(t, f.hermesPath))
			assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
		})
	}
}

// k96: the unreagent-managed marker heads the unreagent entry, not the whole
// file, appears once however often the command runs, and replaces the
// file-wide marker an earlier release put on top.
func TestHermesSetupMarksOnlyTheEntry(t *testing.T) {
	const legacy = "# unreagent-managed: do not edit — re-run `unreagent hermes-setup` to refresh"
	for name, hermesConfig := range map[string]string{
		"user config":                        userHermesConfig,
		"written by an earlier release":      legacy + "\nmcp_servers:\n    unreagent:\n        url: http://127.0.0.1:8765/mcp\n",
		"comments only, with the old marker": legacy + "\n# my note\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "user-token-0123456789", hermesConfig)
			for run := 1; run <= 2; run++ {
				if _, err := f.run(t); err != nil {
					t.Fatal(err)
				}
				got := readFile(t, f.hermesPath)
				assertCommentsKept(t, fmt.Sprintf("run %d", run), withoutLine(hermesConfig, legacy), got)
				lines := strings.Split(got, "\n")
				if strings.Contains(lines[0], "unreagent-managed") {
					t.Errorf("run %d: the marker is on top of the file:\n%s", run, got)
				}
				if n := strings.Count(got, "unreagent-managed"); n != 1 {
					t.Errorf("run %d: %d markers, want 1:\n%s", run, n, got)
				}
				entry := -1
				for i, l := range lines {
					if strings.TrimSpace(l) == "unreagent:" {
						entry = i
					}
				}
				if entry < 1 || !strings.Contains(lines[entry-1], "unreagent-managed") {
					t.Errorf("run %d: the marker is not right above the unreagent entry:\n%s", run, got)
				}
			}
		})
	}
}

// k96: a generated token is only saved once the Hermes merge has succeeded. A
// refusal leaves unreagent.local.yaml as it was — byte for byte, or absent.
func TestHermesSetupRefusalLeavesLocalConfigAlone(t *testing.T) {
	const localYAML = "# my machine\nagent:\n  hermes:\n    serverName: unreagent  # the default\n"
	for name, hermesConfig := range map[string]string{
		"unmergeable mcp_servers": "model: hermes-3\nmcp_servers:\n  - github\n",
		"unparsable config":       "model: hermes-3\nmcp_servers: [unclosed\n",
	} {
		for local, content := range map[string]string{"absent": "", "present": localYAML} {
			t.Run(name+", local.yaml "+local, func(t *testing.T) {
				f := newHermesFixture(t, "", hermesConfig)
				localPath := localOverridePath(f.cfgPath)
				if content != "" {
					writeFile(t, localPath, content)
				}

				if _, err := f.run(t); err == nil {
					t.Fatal("expected an error, got nil")
				}
				if content == "" {
					if _, err := os.Stat(localPath); !os.IsNotExist(err) {
						t.Errorf("unreagent.local.yaml was created (stat: %v)", err)
					}
				} else if got := readFile(t, localPath); got != content {
					t.Errorf("unreagent.local.yaml was changed:\n%s", got)
				}
				if got := readFile(t, f.hermesPath); got != hermesConfig {
					t.Errorf("Hermes config was changed:\n%s", got)
				}
			})
		}
	}
}

// k96: a generated token is saved before the Hermes config gets it, so the
// Hermes config never carries a token the launcher does not know. When the
// Hermes write fails after that, the error says the token is saved, and a
// re-run reuses it to finish.
func TestHermesSetupHermesWriteFailureKeepsSavedToken(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to (POSIX permissions, not root)")
	}
	f := newHermesFixture(t, "", userHermesConfig)
	writeFile(t, f.hermesPath+".bak", userHermesConfig) // kept, so the backup creates nothing
	dir := filepath.Dir(f.hermesPath)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) }) // before TempDir's cleanup

	_, err := f.run(t)
	if err == nil {
		t.Fatal("expected the Hermes write to fail, got nil")
	}
	localPath := localOverridePath(f.cfgPath)
	token, _ := lookup(readYAMLMap(t, localPath), "mcp", "token").(string)
	if len(token) != 64 {
		t.Fatalf("mcp.token = %q, want the generated token saved before the Hermes write", token)
	}
	if want := "already saved in " + localPath; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not say %q", err, want)
	}
	if got := readFile(t, f.hermesPath); got != userHermesConfig {
		t.Errorf("Hermes config was changed:\n%s", got)
	}

	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := f.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "New bearer token") {
		t.Errorf("re-run generated a new token instead of reusing the saved one:\n%s", out)
	}
	assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
}

// k96: writing a generated token into an existing unreagent.local.yaml keeps
// its comments, its other keys and their order, leaves no temp file behind,
// and the next run reuses the saved token.
func TestHermesSetupTokenKeepsLocalConfig(t *testing.T) {
	for name, c := range map[string]struct {
		local string
		order []string
	}{
		"mcp section first": {
			local: "# machine overrides\nmcp:\n  # the address stays\n  address: 127.0.0.1:8765\nagent:\n  hermes:\n    serverName: unreagent  # the default\n",
			order: []string{"mcp:", "address:", "token:", "agent:", "serverName:"},
		},
		"no mcp section": {
			local: "# machine overrides\nagent:\n  hermes:\n    serverName: unreagent  # the default\n",
			order: []string{"agent:", "serverName:", "mcp:", "token:"},
		},
		"comments only": {
			local: "# engineRoot: D:/UE/UE_5.7\n",
			order: []string{"# engineRoot", "mcp:", "token:"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "", "")
			localPath := localOverridePath(f.cfgPath)
			writeFile(t, localPath, c.local)

			if _, err := f.run(t); err != nil {
				t.Fatal(err)
			}
			got := readFile(t, localPath)
			assertCommentsKept(t, "unreagent.local.yaml", c.local, got)
			assertInOrder(t, "unreagent.local.yaml", got, c.order...)
			local := readYAMLMap(t, localPath)
			token, _ := lookup(local, "mcp", "token").(string)
			if len(token) != 64 {
				t.Fatalf("mcp.token = %q, want 64 hex chars:\n%s", token, got)
			}
			if strings.Contains(c.local, "serverName") {
				if got := lookup(local, "agent", "hermes", "serverName"); got != "unreagent" {
					t.Errorf("agent.hermes.serverName = %#v, want \"unreagent\"", got)
				}
			}
			assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
			if tmp, _ := filepath.Glob(filepath.Join(f.dir, "*.tmp-*")); len(tmp) > 0 {
				t.Errorf("temp files left behind: %v", tmp)
			}

			out, err := f.run(t)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "New bearer token") {
				t.Errorf("second run generated a new token instead of reusing the saved one:\n%s", out)
			}
			assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
		})
	}
}

// k96: anchors, aliases and merge keys outside the entry are written back as
// the user wrote them; yaml.v3 alone writes every merge key as "!!merge <<".
func TestHermesSetupKeepsMergeKeys(t *testing.T) {
	const token = "user-token-0123456789"
	const hermesConfig = `defaults: &defaults
  timeout: 30
providers:
  a:
    <<: *defaults
    url: http://a.invalid
mcp_servers:
  github:
    <<: *defaults
    command: npx
`
	f := newHermesFixture(t, token, hermesConfig)

	if _, err := f.run(t); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, f.hermesPath)
	if strings.Contains(got, "!!merge") || strings.Count(got, "<<: *defaults") != 2 || !strings.Contains(got, "defaults: &defaults") {
		t.Errorf("anchor or merge keys not written as before:\n%s", got)
	}
	doc := readYAMLMap(t, f.hermesPath)
	assertUnreagentEntry(t, doc, token)
	if got := lookup(doc, "mcp_servers", "github", "timeout"); got != 30 {
		t.Errorf("mcp_servers.github.timeout = %#v, want 30 from the merge key", got)
	}
}

// k96: a symlinked unreagent.local.yaml or Hermes config stays a symlink, and
// its target gets the token or the entry.
func TestHermesSetupFollowsSymlinks(t *testing.T) {
	f := newHermesFixture(t, "", "")
	shared := t.TempDir()
	localTarget := filepath.Join(shared, "local.yaml")
	hermesTarget := filepath.Join(shared, "hermes.yaml")
	writeFile(t, localTarget, "# shared machine overrides\nagent:\n  hermes:\n    serverName: unreagent\n")
	writeFile(t, hermesTarget, userHermesConfig)
	localPath := localOverridePath(f.cfgPath)
	if err := os.Symlink(localTarget, localPath); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	if err := os.Symlink(hermesTarget, f.hermesPath); err != nil {
		t.Fatal(err)
	}

	if _, err := f.run(t); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{localPath, f.hermesPath} {
		if fi, err := os.Lstat(p); err != nil {
			t.Error(err)
		} else if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is no longer a symlink (mode %v)", p, fi.Mode())
		}
	}
	token, _ := lookup(readYAMLMap(t, localTarget), "mcp", "token").(string)
	if len(token) != 64 {
		t.Fatalf("the symlink target lacks mcp.token:\n%s", readFile(t, localTarget))
	}
	doc := readYAMLMap(t, hermesTarget)
	assertUserConfigKept(t, doc)
	assertUnreagentEntry(t, doc, token)
	if tmp, _ := filepath.Glob(filepath.Join(shared, "*.tmp-*")); len(tmp) > 0 {
		t.Errorf("temp files left behind: %v", tmp)
	}
}

// k96: a file that is replaced keeps its permission bits; a file that is
// created holds a token and gets 0600.
func TestHermesSetupKeepsFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not kept on Windows")
	}
	assertMode := func(t *testing.T, path string, want os.FileMode) {
		t.Helper()
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s has mode %v, want %v", filepath.Base(path), got, want)
		}
	}

	t.Run("existing files", func(t *testing.T) {
		f := newHermesFixture(t, "", userHermesConfig)
		localPath := localOverridePath(f.cfgPath)
		writeFile(t, localPath, "# machine overrides\n")
		for _, p := range []string{localPath, f.hermesPath} {
			if err := os.Chmod(p, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.run(t); err != nil {
			t.Fatal(err)
		}
		assertMode(t, localPath, 0o644)
		assertMode(t, f.hermesPath, 0o644)
	})

	t.Run("new files", func(t *testing.T) {
		f := newHermesFixture(t, "", "")
		if _, err := f.run(t); err != nil {
			t.Fatal(err)
		}
		assertMode(t, localOverridePath(f.cfgPath), 0o600)
		assertMode(t, f.hermesPath, 0o600)
	})
}

// k96: the token write needs the project directory writable for its temp
// file. A read-only one is refused, with no in-place write as a fallback, and
// the error names the directory.
func TestHermesSetupReadOnlyProjectDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to (POSIX permissions, not root)")
	}
	f := newHermesFixture(t, "", userHermesConfig)
	localPath := localOverridePath(f.cfgPath)
	const local = "# machine overrides\n"
	writeFile(t, localPath, local)
	if err := os.Chmod(f.dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(f.dir, 0o755) }) // before TempDir's cleanup

	_, err := f.run(t)
	if err == nil {
		t.Fatal("expected an error for a read-only project directory, got nil")
	}
	if want := "in directory " + f.dir + ":"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the directory (%q)", err, want)
	}
	if got := readFile(t, localPath); got != local {
		t.Errorf("unreagent.local.yaml was changed:\n%s", got)
	}
	if got := readFile(t, f.hermesPath); got != userHermesConfig {
		t.Errorf("Hermes config was changed:\n%s", got)
	}
}

// k96: only a symlinked file is followed. A file reached through a symlinked
// directory keeps its path, so an error names the directory the user gave.
func TestWriteFileAtomicKeepsSymlinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to (POSIX permissions, not root)")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	writeFile(t, filepath.Join(target, "unreagent.local.yaml"), "a: 1\n")
	if err := os.Chmod(target, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(target, 0o755) }) // before TempDir's cleanup

	err := writeFileAtomic(filepath.Join(link, "unreagent.local.yaml"), []byte("a: 2\n"))
	if err == nil {
		t.Fatal("expected an error for a read-only directory, got nil")
	}
	if want := "in directory " + link + ":"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the directory as given (%q)", err, want)
	}
}

// k96: a dangling symlink at either path is refused before anything is
// written. The real file usually lives somewhere unavailable right now; a
// write would replace the link with a new file, and for the Hermes config
// with one that holds only the unreagent entry, without a .bak.
func TestHermesSetupRefusesDanglingSymlinks(t *testing.T) {
	for _, which := range []string{"Hermes config", "unreagent.local.yaml"} {
		t.Run(which, func(t *testing.T) {
			f := newHermesFixture(t, "", "")
			localPath := localOverridePath(f.cfgPath)
			missing := filepath.Join(t.TempDir(), "gone", "config.yaml")
			link, other, otherContent := f.hermesPath, localPath, "# machine overrides\n"
			if which == "unreagent.local.yaml" {
				link, other, otherContent = localPath, f.hermesPath, userHermesConfig
			}
			writeFile(t, other, otherContent)
			if err := os.Symlink(missing, link); err != nil {
				t.Skipf("cannot create a symlink here: %v", err)
			}

			want := link + " is a symlink to " + missing + ", which does not exist"
			if _, err := f.run(t); err == nil {
				t.Error("expected an error for a dangling symlink, got nil")
			} else if !strings.Contains(filepath.ToSlash(err.Error()), filepath.ToSlash(want)) {
				t.Errorf("error %q does not name the link and its target (%q)", err, want)
			}
			if dest, err := os.Readlink(link); err != nil || dest != missing {
				t.Errorf("the link was changed: Readlink = %q, %v", dest, err)
			}
			if _, err := os.Lstat(missing); !os.IsNotExist(err) {
				t.Errorf("the link target was created (lstat: %v)", err)
			}
			if got := readFile(t, other); got != otherContent {
				t.Errorf("%s was changed:\n%s", filepath.Base(other), got)
			}
			if _, err := os.Lstat(f.hermesPath + ".bak"); !os.IsNotExist(err) {
				t.Errorf("a .bak was created although nothing was written (lstat: %v)", err)
			}
			for _, dir := range []string{f.dir, filepath.Dir(f.hermesPath)} {
				if tmp, _ := filepath.Glob(filepath.Join(dir, "*.tmp-*")); len(tmp) > 0 {
					t.Errorf("temp files left behind: %v", tmp)
				}
			}
		})
	}
}

// k96: a .nan outside the entry goes through. NaN never equals itself as a
// float, but it is the same value before and after the edit.
func TestHermesSetupKeepsNaN(t *testing.T) {
	const hermesConfig = "model: hermes-3\nsampling:\n  temperature: .nan\nmcp_servers:\n  github:\n    command: npx\n    weights: [1, .NaN]\n"
	f := newHermesFixture(t, "user-token-0123456789", hermesConfig)

	if _, err := f.run(t); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, f.hermesPath)
	for _, want := range []string{"temperature: .nan\n", "weights: [1, .NaN]\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("Hermes config lacks %q:\n%s", want, got)
		}
	}
	assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), "user-token-0123456789")
}

// k96: checkEdit is the safety net before any write: the encoded result must
// parse, hold the edited value, and hold everything else as before.
func TestCheckEdit(t *testing.T) {
	before := map[string]interface{}{
		"model":       "x",
		"mcp_servers": map[string]interface{}{"github": map[string]interface{}{"command": "npx"}},
	}
	entry := map[string]interface{}{"url": "http://u"}
	for name, c := range map[string]struct {
		out     string
		wantErr bool
	}{
		"edit only":         {"model: x\nmcp_servers:\n  github: {command: npx}\n  unreagent: {url: http://u}\n", false},
		"does not parse":    {"model: x\nmcp_servers:\n  github: *gone\n  unreagent: {url: http://u}\n", true},
		"edit missing":      {"model: x\nmcp_servers:\n  github: {command: npx}\n", true},
		"edit wrong":        {"model: x\nmcp_servers:\n  github: {command: npx}\n  unreagent: {url: http://other}\n", true},
		"other key change":  {"model: y\nmcp_servers:\n  github: {command: npx}\n  unreagent: {url: http://u}\n", true},
		"other server lost": {"model: x\nmcp_servers:\n  unreagent: {url: http://u}\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkEdit(before, []byte(c.out), entry, "mcp_servers", "unreagent")
			if (err != nil) != c.wantErr {
				t.Errorf("checkEdit = %v, want error: %v", err, c.wantErr)
			}
		})
	}

	// A null or missing parent counts as empty, before and after.
	for _, b := range []map[string]interface{}{{"mcp": nil}, {}} {
		if err := checkEdit(b, []byte("mcp:\n  token: t\n"), "t", "mcp", "token"); err != nil {
			t.Errorf("checkEdit(%v) = %v, want nil", b, err)
		}
	}

	// NaN counts as equal to NaN, in a list and under a key that is not a
	// string, too; a NaN that turns into a number is still a change.
	var withNaN map[string]interface{}
	if err := yaml.Unmarshal([]byte("t: .nan\nl: [.nan]\nports: {8080: .nan}\n"), &withNaN); err != nil {
		t.Fatal(err)
	}
	for out, wantErr := range map[string]bool{
		"t: .nan\nl: [.nan]\nports: {8080: .nan}\nmcp: {token: t}\n": false,
		"t: 0\nl: [.nan]\nports: {8080: .nan}\nmcp: {token: t}\n":    true,
		"t: .nan\nl: [.nan]\nports: {8080: 0}\nmcp: {token: t}\n":    true,
	} {
		if err := checkEdit(withNaN, []byte(out), "t", "mcp", "token"); (err != nil) != wantErr {
			t.Errorf("checkEdit(%q) = %v, want error: %v", out, err, wantErr)
		}
	}
}
