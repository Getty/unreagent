package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	managedOnly := hermesConfigMarker + "\nmcp_servers:\n    unreagent:\n        url: http://127.0.0.1:8765/mcp\n"
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
func TestHermesSetupRefusesUnmergeableServers(t *testing.T) {
	for name, hermesConfig := range map[string]string{
		"list":           "model: hermes-3\nmcp_servers:\n  - github\n",
		"non-string key": "model: hermes-3\nmcp_servers:\n  8080:\n    url: http://127.0.0.1:8080/mcp\n  github:\n    command: npx\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "user-token-0123456789", hermesConfig)

			if _, err := f.run(t); err == nil {
				t.Error("expected an error for an unmergeable mcp_servers, got nil")
			}
			if got := readFile(t, f.hermesPath); got != hermesConfig {
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
