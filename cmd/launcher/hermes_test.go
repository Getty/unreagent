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

// k57: an mcp.address on every interface (":8765") went into the Hermes entry
// and the printed url as http://:8765/mcp. Both carry the client URL now.
func TestHermesSetupWritesTheClientURL(t *testing.T) {
	const token = "user-token-0123456789"
	f := newHermesFixture(t, token, userHermesConfig)
	cfg := readFile(t, f.cfgPath)
	writeFile(t, f.cfgPath, strings.Replace(cfg, "address: 127.0.0.1:8765", `address: ":8765"`, 1))

	out, err := f.run(t)
	if err != nil {
		t.Fatal(err)
	}
	assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
	if !strings.Contains(out, "  url:    http://127.0.0.1:8765/mcp\n") {
		t.Errorf("hermes-setup does not print the client URL http://127.0.0.1:8765/mcp:\n%s", out)
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

// k118: yaml.v3 reads the first YAML document of a file, so a rewrite would
// drop the others. A Hermes config that holds more than one is refused with
// nothing written; a document that holds nothing but a comment counts, since
// the comment would be lost.
func TestHermesSetupRefusesMultipleDocuments(t *testing.T) {
	for name, c := range map[string]struct {
		config string
		line   int
	}{
		"two documents":                    {"model: hermes-3\n---\nmcp_servers:\n  github:\n    command: npx\n", 2},
		"after an explicit end":            {"model: hermes-3\n...\n---\nmodel: other\n", 3},
		"content after an empty first one": {"---\n---\nmodel: hermes-3\n", 2},
		"after an empty second one":        {"model: hermes-3\n---\n---\nmodel: other\n", 3},
		"a comment-only second document":   {"model: hermes-3\n--- # kept for later\n", 2},
		"a comment after a blank line":     {"model: x\n---\n\n# c\n", 2},
		"a null second document":           {"model: hermes-3\n--- ~\n", 2},
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "user-token-0123456789", c.config)

			want := fmt.Sprintf("Hermes config %s not parseable: it holds more than one YAML document (another one starts at line %d); refusing to drop all but the first", filepath.ToSlash(f.hermesPath), c.line)
			if _, err := f.run(t); err == nil {
				t.Error("expected an error for a multi-document config, got nil")
			} else if err.Error() != want {
				t.Errorf("error %q,\nwant %q", err, want)
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

// k118: a bare "---" or "..." at the end of the file starts or ends a document
// that holds nothing, so there is nothing to drop: the config is edited as a
// single document. A comment after a trailing "---" belongs to the first
// document for yaml.v3, and stays.
func TestHermesSetupAcceptsEmptyTrailingDocument(t *testing.T) {
	for name, hermesConfig := range map[string]string{
		"trailing ---":             "model: hermes-3\n---\n",
		"trailing ...":             "model: hermes-3\n...\n",
		"trailing --- then ...":    "model: hermes-3\n---\n...\n",
		"two trailing ---":         "model: hermes-3\n---\n---\n",
		"comment after the ---":    "model: hermes-3\n---\n# the end\n",
		"leading and trailing ---": "---\nmodel: hermes-3\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			const token = "user-token-0123456789"
			f := newHermesFixture(t, token, hermesConfig)

			if _, err := f.run(t); err != nil {
				t.Fatal(err)
			}
			got := readFile(t, f.hermesPath)
			assertCommentsKept(t, "Hermes config", hermesConfig, got)
			doc := readYAMLMap(t, f.hermesPath)
			assertUnreagentEntry(t, doc, token)
			if got := lookup(doc, "model"); got != "hermes-3" {
				t.Errorf("model = %#v, want \"hermes-3\"", got)
			}
		})
	}
}

// k118: a generated token is not written into an unreagent.local.yaml that
// holds more than one YAML document either; the rewrite would drop the others.
// Since k124 config.Load refuses such a file first, so hermes-setup stops
// before the token edit; the edit's own refusal stays, checked directly.
func TestHermesSetupRefusesMultiDocumentLocalConfig(t *testing.T) {
	f := newHermesFixture(t, "", userHermesConfig)
	localPath := localOverridePath(f.cfgPath)
	const local = "agent:\n  hermes:\n    serverName: unreagent\n---\n# second document\nmcp:\n  address: 127.0.0.1:9999\n"
	writeFile(t, localPath, local)

	const want = "it holds more than one YAML document (another one starts at line 4); refusing to drop all but the first"
	_, err := f.run(t)
	if err == nil {
		t.Fatal("expected an error for a multi-document unreagent.local.yaml, got nil")
	}
	if want := "unreagent.local.yaml: " + want; err.Error() != want {
		t.Errorf("error %q,\nwant %q", err, want)
	}
	if _, err := localConfigWithToken(localPath, "user-token-0123456789"); err == nil {
		t.Error("the token edit accepted a multi-document unreagent.local.yaml")
	} else if want := "not parseable: " + want; err.Error() != want {
		t.Errorf("token edit error %q,\nwant %q", err, want)
	}
	if got := readFile(t, localPath); got != local {
		t.Errorf("unreagent.local.yaml was changed:\n%s", got)
	}
	if got := readFile(t, f.hermesPath); got != userHermesConfig {
		t.Errorf("Hermes config was changed:\n%s", got)
	}
}

// k134: a generated token is not written into an unreagent.local.yaml whose
// mcp is an anchored null either. The token would go into a new mapping in
// the null's place, without the anchor, and the alias would be left undefined.
func TestHermesSetupRefusesAnchoredNullLocalMCP(t *testing.T) {
	f := newHermesFixture(t, "", userHermesConfig)
	localPath := localOverridePath(f.cfgPath)
	const local = "mcp: &m\nagent: *m\n"
	writeFile(t, localPath, local)

	_, err := f.run(t)
	if err == nil {
		t.Fatalf("hermes-setup accepted an anchored null mcp and wrote:\n%s", readFile(t, localPath))
	}
	if want := "add the token to " + localPath + ": mcp carries the anchor &m; adding to it would change its aliases too (nothing written)"; err.Error() != want {
		t.Errorf("error %q, want %q", err, want)
	}
	if got := readFile(t, localPath); got != local {
		t.Errorf("unreagent.local.yaml was changed:\n%s", got)
	}
	if got := readFile(t, f.hermesPath); got != userHermesConfig {
		t.Errorf("Hermes config was changed:\n%s", got)
	}
}

// mcp_servers that is not a mapping with string keys cannot be merged into
// without dropping what is there, so the command refuses and touches nothing.
// Neither can an mcp_servers that is an alias, carries an anchor, or comes in
// through a merge key (k96): adding to it would change the anchored mapping or
// its aliases, or hide the merged servers. An unreagent entry that carries an
// anchor, on itself or below, is not replaced either: the aliases of that
// anchor would be left undefined. A duplicate key is refused as unparsable, as
// before k96. k116: so is an entry that is a stdio server, or whose headers
// spell Authorization more than one way. Each refusal names its reason:
// checkEdit would refuse most of these inputs, too, but with a message that
// does not say why.
func TestHermesSetupRefusesUnmergeableServers(t *testing.T) {
	for name, c := range map[string]struct{ config, wantErr string }{
		"list":                 {"model: hermes-3\nmcp_servers:\n  - github\n", "mcp_servers is not a mapping of server names"},
		"non-string key":       {"model: hermes-3\nmcp_servers:\n  8080:\n    url: http://127.0.0.1:8080/mcp\n  github:\n    command: npx\n", "mcp_servers is not a mapping of server names"},
		"alias":                {"shared: &servers\n  github:\n    command: npx\nmcp_servers: *servers\n", "mcp_servers is an alias (*servers); adding to it would change the anchored mapping too; refusing to change it"},
		"anchored mcp_servers": {"mcp_servers: &srv\n  github:\n    command: npx\nbackup_servers: *srv\n", "mcp_servers carries the anchor &srv; adding to it would change its aliases too; refusing to change it"},
		"merge key":            {"base: &base\n  mcp_servers:\n    github:\n      command: npx\n<<: *base\nmodel: hermes-3\n", "mcp_servers comes in through a merge key (<<); refusing to override it"},
		"duplicate key":        {"mcp_servers:\n  github:\n    command: npx\nmcp_servers:\n  other:\n    command: x\n", `mapping key "mcp_servers" already defined at line 1`},
		// k134: a null is replaced by a new mapping, which would drop the
		// anchor and leave the alias undefined.
		"anchored null mcp_servers": {"mcp_servers: &m\nother: *m\n", "mcp_servers carries the anchor &m; adding to it would change its aliases too; refusing to change it"},
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
`, "mcp_servers.unreagent carries the anchor &unreagent, which other parts of the config may use; refusing to change it"},
		"anchor inside the entry": {`mcp_servers:
  unreagent:
    url: http://old.invalid/mcp
    headers: &h
      Authorization: Bearer old
  other:
    url: http://127.0.0.1:9000/mcp
    headers: *h
`, "mcp_servers.unreagent carries the anchor &h, which other parts of the config may use; refusing to change it"},
		// k116: the entry is edited in place, so it and its headers have to be
		// mappings of the entry's own.
		"entry is a scalar":           {"mcp_servers:\n  unreagent: http://127.0.0.1:8765/mcp\n", "mcp_servers.unreagent is not a mapping with string keys (a list, a single value, or a key that is not a string); refusing to change it"},
		"entry is a list":             {"mcp_servers:\n  unreagent: [http://127.0.0.1:8765/mcp]\n", "mcp_servers.unreagent is not a mapping with string keys"},
		"entry has a non-string key":  {"mcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    8765: port\n", "mcp_servers.unreagent is not a mapping with string keys"},
		"headers is a scalar":         {"mcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    timeout: 120\n    headers: Bearer old-token\n", "mcp_servers.unreagent.headers is not a mapping with string keys (a list, a single value, or a key that is not a string); refusing to change it"},
		"headers is a list":           {"mcp_servers:\n  unreagent:\n    headers: [Authorization]\n", "mcp_servers.unreagent.headers is not a mapping with string keys"},
		"entry is an alias":           {"shared: &s\n  url: http://old.invalid/mcp\nmcp_servers:\n  unreagent: *s\n", "mcp_servers.unreagent is an alias (*s); adding to it would change the anchored mapping too; refusing to change it"},
		"headers is an alias":         {"auth: &h\n  Authorization: Bearer old\nmcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    headers: *h\n", "mcp_servers.unreagent.headers is an alias (*h); adding to it would change the anchored mapping too; refusing to change it"},
		"entry through a merge key":   {"base: &base\n  unreagent:\n    url: http://old.invalid/mcp\n    timeout: 120\nmcp_servers:\n  <<: *base\n  github:\n    command: npx\n", "mcp_servers.unreagent comes in through a merge key (<<); refusing to override it"},
		"headers through a merge key": {"defaults: &d\n  headers:\n    X-Trace: \"on\"\nmcp_servers:\n  unreagent:\n    <<: *d\n    url: http://old.invalid/mcp\n", "mcp_servers.unreagent.headers comes in through a merge key (<<); refusing to override it"},
		// k116: an entry that runs unreagent as a stdio server is the user's
		// own; setting url and headers on it would leave Hermes a server with
		// both. So is one that gets its command through a merge key.
		"stdio entry":            {"mcp_servers:\n  unreagent:\n    command: unreagent\n    args: [mcp]\n  github:\n    command: npx\n", "mcp_servers.unreagent is a stdio server (it sets command), and hermes-setup only manages an HTTP entry (url and headers); remove command and args from the entry, or rename or delete the entry, then re-run hermes-setup"},
		"stdio entry with a url": {"mcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    command: unreagent\n", "mcp_servers.unreagent is a stdio server (it sets command)"},
		// A null or empty command is still a command that is set: the entry has the key.
		"stdio entry with a null command":   {"mcp_servers:\n  unreagent:\n    command: ~\n    url: http://old.invalid/mcp\n", "mcp_servers.unreagent is a stdio server (it sets command)"},
		"stdio entry with an empty command": {"mcp_servers:\n  unreagent:\n    command: \"\"\n    args: [mcp]\n", "mcp_servers.unreagent is a stdio server (it sets command)"},
		"command through a merge key":       {"stdio: &s\n  command: unreagent\nmcp_servers:\n  unreagent:\n    <<: *s\n    timeout: 120\n", "mcp_servers.unreagent is a stdio server (it sets command)"},
		// k116: header names ignore case, so two spellings of Authorization
		// leave it open which token counts.
		"Authorization twice":           {"mcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    headers:\n      authorization: Bearer stale\n      Authorization: Bearer old\n", "mcp_servers.unreagent.headers sets the Authorization header more than once (Authorization, authorization); header names ignore case, so which token counts is ambiguous; keep one, then re-run hermes-setup"},
		"Authorization three times":     {"mcp_servers:\n  unreagent:\n    headers: {AUTHORIZATION: a, authorization: b, Authorization: c}\n", "more than once (AUTHORIZATION, Authorization, authorization)"},
		"Authorization twice via merge": {"auth: &a\n  authorization: Bearer stale\nmcp_servers:\n  unreagent:\n    headers:\n      <<: *a\n      Authorization: Bearer old\n", "more than once (Authorization, authorization)"},
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

// k116: a re-run refreshes only the keys unreagent owns in its entry, url and
// headers.Authorization. Every other key of the entry stays with its value,
// its place and its comments, other headers among them.
func TestHermesSetupKeepsUserKeysInEntry(t *testing.T) {
	const token = "user-token-0123456789"
	const hermesConfig = `model: hermes-3
mcp_servers:
  unreagent:
    # where unreagent listens
    url: http://old.invalid/mcp  # refreshed by hermes-setup
    timeout: 120  # the editor is slow to answer
    headers:
      X-Trace: "on"  # my proxy wants it
      Authorization: Bearer old-token
    tools:
      include: [ue_start, logs]
  github:
    command: npx
`
	f := newHermesFixture(t, token, hermesConfig)

	want := map[string]interface{}{
		"url":     "http://127.0.0.1:8765/mcp",
		"timeout": 120,
		"headers": map[string]interface{}{"X-Trace": "on", "Authorization": "Bearer " + token},
		"tools":   map[string]interface{}{"include": []interface{}{"ue_start", "logs"}},
	}
	for run := 1; run <= 2; run++ {
		if _, err := f.run(t); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		got := readFile(t, f.hermesPath)
		what := fmt.Sprintf("run %d: Hermes config", run)
		assertCommentsKept(t, what, hermesConfig, got)
		assertInOrder(t, what, got, "unreagent:", "url:", "timeout:", "headers:", "X-Trace:", "Authorization:", "tools:", "github:")
		doc := readYAMLMap(t, f.hermesPath)
		if entry := lookup(doc, "mcp_servers", "unreagent"); !reflect.DeepEqual(entry, want) {
			t.Errorf("%s: mcp_servers.unreagent = %#v,\nwant %#v", what, entry, want)
		}
		if got := lookup(doc, "mcp_servers", "github", "command"); got != "npx" {
			t.Errorf("%s: mcp_servers.github.command = %#v, want \"npx\"", what, got)
		}
	}
}

// k116: an entry hermes-setup creates, or one that is null, holds exactly the
// keys unreagent owns, url first.
func TestHermesSetupNewEntryHoldsOwnedKeys(t *testing.T) {
	const token = "user-token-0123456789"
	want := map[string]interface{}{
		"url":     "http://127.0.0.1:8765/mcp",
		"headers": map[string]interface{}{"Authorization": "Bearer " + token},
	}
	for name, hermesConfig := range map[string]string{
		"no mcp_servers":   "model: hermes-3\n",
		"no entry":         userHermesConfig,
		"null entry":       "mcp_servers:\n  unreagent:\n  github:\n    command: npx\n",
		"null headers":     "mcp_servers:\n  unreagent:\n    headers: ~\n",
		"no config at all": "",
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, token, hermesConfig)
			if _, err := f.run(t); err != nil {
				t.Fatal(err)
			}
			if entry := lookup(readYAMLMap(t, f.hermesPath), "mcp_servers", "unreagent"); !reflect.DeepEqual(entry, want) {
				t.Errorf("mcp_servers.unreagent = %#v,\nwant %#v", entry, want)
			}
			got := readFile(t, f.hermesPath)
			assertInOrder(t, "Hermes config", got, "unreagent:", "url:", "headers:", "Authorization:")
		})
	}
}

// k116: header names ignore case, so an Authorization header the entry spells
// another way is the one a re-run refreshes, under its own spelling; adding
// a second one would leave the stale token in the config next to the new one.
// That holds for one that comes in through a merge key, too: the entry's own
// key, in the same spelling, overrides it.
func TestHermesSetupAuthorizationIgnoresCase(t *testing.T) {
	const token = "user-token-0123456789"
	for name, c := range map[string]struct{ config, key string }{
		"lower case":                     {"mcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    headers:\n      authorization: Bearer old  # keep me\n      X-Trace: \"on\"\n", "authorization"},
		"upper case":                     {"mcp_servers:\n  unreagent:\n    headers: {X-Trace: \"on\", AUTHORIZATION: Bearer old}\n", "AUTHORIZATION"},
		"lower case through a merge key": {"auth: &a\n  authorization: Bearer old\nmcp_servers:\n  unreagent:\n    headers:\n      <<: *a\n      X-Trace: \"on\"\n", "authorization"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, token, c.config)
			want := map[string]interface{}{c.key: "Bearer " + token, "X-Trace": "on"}
			for run := 1; run <= 2; run++ {
				if _, err := f.run(t); err != nil {
					t.Fatalf("run %d: %v", run, err)
				}
				what := fmt.Sprintf("run %d: Hermes config", run)
				assertCommentsKept(t, what, c.config, readFile(t, f.hermesPath))
				doc := readYAMLMap(t, f.hermesPath)
				if got := lookup(doc, "mcp_servers", "unreagent", "headers"); !reflect.DeepEqual(got, want) {
					t.Errorf("%s: headers = %#v,\nwant %#v", what, got, want)
				}
				if got := lookup(doc, "mcp_servers", "unreagent", "url"); got != "http://127.0.0.1:8765/mcp" {
					t.Errorf("%s: url = %#v", what, got)
				}
			}
		})
	}
}

// k96: a comment the user put after the unreagent entry survives the run that
// refreshes the entry (yaml.v3 hangs such a comment on the entry's last line
// when the entry is the last server). k116: it stays after the entry, at the
// indentation of the servers, both on the run that adds headers to the entry
// and on the re-run that finds the entry complete.
func TestHermesSetupKeepsCommentAfterReplacedEntry(t *testing.T) {
	const token = "user-token-0123456789"
	const servers = `mcp_servers:
  github:
    command: npx
  unreagent:
    url: http://old.invalid/mcp
  # my-server:
  #   command: not-yet
`
	for name, hermesConfig := range map[string]string{
		"before a top-level key": servers + "model: x\n",
		"at the end of the file": servers,
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, token, hermesConfig)
			tail := "\n      Authorization: Bearer " + token + "\n  # my-server:\n  #   command: not-yet\n" + strings.TrimPrefix(hermesConfig, servers)
			for run := 1; run <= 2; run++ {
				if _, err := f.run(t); err != nil {
					t.Fatal(err)
				}
				got := readFile(t, f.hermesPath)
				what := fmt.Sprintf("run %d: Hermes config", run)
				assertCommentsKept(t, what, hermesConfig, got)
				assertInOrder(t, what, got, "github:", "unreagent:", "Authorization:", "# my-server:")
				if !strings.HasSuffix(got, tail) {
					t.Errorf("%s: the comment after the entry did not stay after it, want the file to end in %q:\n%s", what, tail, got)
				}
				assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
			}
		})
	}
}

// k116: with another server after the entry, a comment at the end of the
// entry stays inside it, right before that server, at least as deep as it was
// written, with no blank line coming in. yaml.v3 gives a comment at the
// servers' indentation to the next server, so what hangs on the entry's last
// line is the entry's own. (yaml.v3 writes it at the indentation of the
// entry's last key, as a plain round trip does: the one at the entry indent
// comes out at the indent of headers.)
func TestHermesSetupKeepsCommentInsideEntryBeforeNextServer(t *testing.T) {
	const token = "user-token-0123456789"
	for name, comment := range map[string]string{
		"inside headers":      `      # X-Debug: "1"`,
		"at the entry indent": "    # disabled: true",
	} {
		t.Run(name, func(t *testing.T) {
			hermesConfig := "mcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    headers:\n      Authorization: Bearer old\n" +
				comment + "\n  github:\n    command: npx\n"
			f := newHermesFixture(t, token, hermesConfig)
			for run := 1; run <= 2; run++ {
				if _, err := f.run(t); err != nil {
					t.Fatal(err)
				}
				got := readFile(t, f.hermesPath)
				what := fmt.Sprintf("run %d: Hermes config", run)
				assertCommentsKept(t, what, hermesConfig, got)
				if strings.Contains(got, "\n\n") {
					t.Errorf("%s gained a blank line:\n%s", what, got)
				}
				before, _, found := strings.Cut(got, "\n  github:\n")
				prev := before[strings.LastIndex(before, "\n")+1:] // the line before github:
				indent := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
				if !found || strings.TrimSpace(prev) != strings.TrimSpace(comment) || indent(prev) < indent(comment) {
					t.Errorf("%s: the comment %q is not right before the next server, indented %d or more:\n%s", what, strings.TrimSpace(comment), indent(comment), got)
				}
				assertUnreagentEntry(t, readYAMLMap(t, f.hermesPath), token)
			}
		})
	}
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
// file-wide marker an earlier release put on top. k116: it says which keys a
// re-run refreshes, and replaces the entry marker of the builds that still
// replaced the whole entry.
func TestHermesSetupMarksOnlyTheEntry(t *testing.T) {
	const legacy = "# unreagent-managed: do not edit — re-run `unreagent hermes-setup` to refresh"
	const wholeEntry = "# unreagent-managed: re-run `unreagent hermes-setup` to refresh this entry; edits to it are overwritten"
	const marker = "# unreagent-managed: re-running `unreagent hermes-setup` refreshes url and headers.Authorization; other keys in this entry are kept"
	for name, hermesConfig := range map[string]string{
		"user config":                        userHermesConfig,
		"written by an earlier release":      legacy + "\nmcp_servers:\n    unreagent:\n        url: http://127.0.0.1:8765/mcp\n",
		"comments only, with the old marker": legacy + "\n# my note\n",
		"entry marker that predates k116":    "mcp_servers:\n  # my note\n  " + wholeEntry + "\n  unreagent:\n    url: http://127.0.0.1:8765/mcp\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "user-token-0123456789", hermesConfig)
			for run := 1; run <= 2; run++ {
				if _, err := f.run(t); err != nil {
					t.Fatal(err)
				}
				got := readFile(t, f.hermesPath)
				assertCommentsKept(t, fmt.Sprintf("run %d", run), withoutLine(withoutLine(hermesConfig, legacy), wholeEntry), got)
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
				if entry < 1 || strings.TrimSpace(lines[entry-1]) != marker {
					t.Errorf("run %d: the marker %q is not right above the unreagent entry:\n%s", run, marker, got)
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

// assertMode checks the permission bits of the file at path.
func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s has mode %04o, want %04o", filepath.Base(path), got, want)
	}
}

// k96: a file that is replaced keeps its permission bits; a file that is
// created holds a token and gets 0600. k167 makes one exception, an
// unreagent.local.yaml open to group or others that gets the token (see
// TestHermesSetupTightensOpenLocalConfig); the Hermes config holds the token,
// too, but keeps its mode, and so does a local file closed to them.
func TestHermesSetupKeepsFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not kept on Windows")
	}

	t.Run("existing files", func(t *testing.T) {
		f := newHermesFixture(t, "", userHermesConfig)
		localPath := localOverridePath(f.cfgPath)
		writeFile(t, localPath, "# machine overrides\n")
		for p, mode := range map[string]os.FileMode{localPath: 0o400, f.hermesPath: 0o644} {
			if err := os.Chmod(p, mode); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.run(t); err != nil {
			t.Fatal(err)
		}
		assertMode(t, localPath, 0o400)
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

// k167: the generated token goes into an existing unreagent.local.yaml whose
// mode lets group or others at it (the usual 0644 of a hand-made file): the
// file comes out 0600, and the output names it with the mode it had. One
// that keeps them out keeps its mode, and nothing is said.
func TestHermesSetupTightensOpenLocalConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not govern who can read a file on Windows (it inherits the directory's ACL); nothing is tightened there")
	}
	const says = "tightened it to 0600"
	for _, c := range []struct {
		mode os.FileMode
		want string // the output line, "" for none
	}{
		{0o644, "unreagent.local.yaml was open to group or others (mode 0644); tightened it to 0600, since it now holds the bearer token."},
		{0o640, "unreagent.local.yaml was open to group or others (mode 0640); tightened it to 0600, since it now holds the bearer token."},
		{0o600, ""},
	} {
		t.Run(fmt.Sprintf("%04o", c.mode), func(t *testing.T) {
			f := newHermesFixture(t, "", userHermesConfig)
			localPath := localOverridePath(f.cfgPath)
			writeFile(t, localPath, "# machine overrides\n")
			if err := os.Chmod(localPath, c.mode); err != nil {
				t.Fatal(err)
			}

			out, err := f.run(t)
			if err != nil {
				t.Fatal(err)
			}
			assertMode(t, localPath, 0o600)
			if lookup(readYAMLMap(t, localPath), "mcp", "token") == nil {
				t.Errorf("unreagent.local.yaml holds no mcp.token:\n%s", readFile(t, localPath))
			}
			if c.want != "" && !strings.Contains(out, c.want+"\n") {
				t.Errorf("output does not say the file was tightened (%q):\n%s", c.want, out)
			}
			if c.want == "" && strings.Contains(out, says) {
				t.Errorf("output reports a tightening of a 0600 file:\n%s", out)
			}
		})
	}
}

// k96: the token write needs the project directory writable for its temp
// file. A read-only one is refused, with no in-place write as a fallback, and
// the error names the directory. k167: the 0644 file the token would have
// tightened keeps its mode, since the mode goes onto the temp file only.
func TestHermesSetupReadOnlyProjectDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to (POSIX permissions, not root)")
	}
	f := newHermesFixture(t, "", userHermesConfig)
	localPath := localOverridePath(f.cfgPath)
	const local = "# machine overrides\n"
	writeFile(t, localPath, local)
	if err := os.Chmod(localPath, 0o644); err != nil { // whatever the umask
		t.Fatal(err)
	}
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
	assertMode(t, localPath, 0o644)
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

	_, err := writeFileAtomic(filepath.Join(link, "unreagent.local.yaml"), []byte("a: 2\n"), false)
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

// k122: in a chain of links that ends at a missing file, the error names that
// file, not the first hop, which exists as a link. A relative target is read
// against the directory of the link that holds it. Since k124 config.Load
// refuses such an unreagent.local.yaml before hermes-setup writes anything,
// with the same diagnosis.
func TestHermesSetupNamesEndOfDanglingChain(t *testing.T) {
	for _, which := range []string{"Hermes config", "unreagent.local.yaml"} {
		t.Run(which, func(t *testing.T) {
			f := newHermesFixture(t, "", "")
			localPath := localOverridePath(f.cfgPath)
			link, other, otherContent := f.hermesPath, localPath, "# machine overrides\n"
			if which == "unreagent.local.yaml" {
				link, other, otherContent = localPath, f.hermesPath, userHermesConfig
			}
			writeFile(t, other, otherContent)
			shared := realTempDir(t)
			hop1, hop2 := filepath.Join(shared, "hop1.yaml"), filepath.Join(shared, "hop2.yaml")
			if err := os.Symlink(hop1, link); err != nil {
				t.Skipf("cannot create a symlink here: %v", err)
			}
			if err := os.Symlink("hop2.yaml", hop1); err != nil { // relative: next to hop1
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join("gone", "config.yaml"), hop2); err != nil {
				t.Fatal(err)
			}
			missing := filepath.Join(shared, "gone", "config.yaml")

			want := link + " is a symlink to " + hop1 + "; following the links leads to " + missing +
				", which does not exist; refusing to replace the link with a new file (nothing written)"
			if which == "unreagent.local.yaml" {
				want = "config not readable: " + link + " is a symlink to " + hop1 + "; following the links leads to " + missing +
					", which does not exist"
			}
			if _, err := f.run(t); err == nil {
				t.Error("expected an error for a dangling chain of symlinks, got nil")
			} else if filepath.ToSlash(err.Error()) != filepath.ToSlash(want) {
				t.Errorf("error %q,\nwant %q", err, want)
			}
			for l, dest := range map[string]string{link: hop1, hop1: "hop2.yaml", hop2: filepath.Join("gone", "config.yaml")} {
				if got, err := os.Readlink(l); err != nil || got != dest {
					t.Errorf("the link %s was changed: Readlink = %q, %v", l, got, err)
				}
			}
			if _, err := os.Lstat(missing); !os.IsNotExist(err) {
				t.Errorf("the end of the chain was created (lstat: %v)", err)
			}
			if got := readFile(t, other); got != otherContent {
				t.Errorf("%s was changed:\n%s", filepath.Base(other), got)
			}
			if _, err := os.Lstat(f.hermesPath + ".bak"); !os.IsNotExist(err) {
				t.Errorf("a .bak was created although nothing was written (lstat: %v)", err)
			}
		})
	}
}

// realTempDir is t.TempDir with its symlinks resolved: an error that follows
// a chain of links names the directories they really lead to.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// k122: a relative target is read against the directory its link really is
// in. Through a symlinked directory, a ".." leads out of the directory the
// link points to, not out of the one the path names: here ../shared is
// dotfiles/shared, not a shared directory next to the fixture's hermes link.
func TestHermesSetupNamesEndOfDanglingChainThroughLinkedDir(t *testing.T) {
	f := newHermesFixture(t, "user-token-0123456789", "")
	dotfiles := realTempDir(t)
	realDir, sharedDir := filepath.Join(dotfiles, "hermes"), filepath.Join(dotfiles, "shared")
	for _, d := range []string{realDir, sharedDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	linkDir := filepath.Dir(f.hermesPath) // <fixture>/hermes -> dotfiles/hermes
	if err := os.Remove(linkDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	hop := filepath.Join("..", "shared", "hop.yaml")
	if err := os.Symlink(hop, filepath.Join(realDir, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("gone.yaml", filepath.Join(sharedDir, "hop.yaml")); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(sharedDir, "gone.yaml")

	want := f.hermesPath + " is a symlink to " + hop + "; following the links leads to " + missing +
		", which does not exist; refusing to replace the link with a new file (nothing written)"
	if _, err := f.run(t); err == nil {
		t.Error("expected an error for a dangling chain of symlinks, got nil")
	} else if filepath.ToSlash(err.Error()) != filepath.ToSlash(want) {
		t.Errorf("error %q,\nwant %q", err, want)
	}
	for _, p := range []string{missing, filepath.Join(f.dir, "shared"), localOverridePath(f.cfgPath)} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s was created (lstat: %v)", p, err)
		}
	}
}

// k122: a symlink loop at either path is refused before anything is written,
// and the error names the path the user gave; filepath.EvalSymlinks alone
// says only "too many links". Since k124 config.Load refuses such an
// unreagent.local.yaml first, as a config it cannot read.
func TestHermesSetupRefusesSymlinkLoop(t *testing.T) {
	for _, which := range []string{"Hermes config", "unreagent.local.yaml"} {
		t.Run(which, func(t *testing.T) {
			f := newHermesFixture(t, "", "")
			localPath := localOverridePath(f.cfgPath)
			link, other, otherContent := f.hermesPath, localPath, "# machine overrides\n"
			if which == "unreagent.local.yaml" {
				link, other, otherContent = localPath, f.hermesPath, userHermesConfig
			}
			writeFile(t, other, otherContent)
			partner := link + ".partner"
			if err := os.Symlink(partner, link); err != nil {
				t.Skipf("cannot create a symlink here: %v", err)
			}
			if err := os.Symlink(link, partner); err != nil {
				t.Fatal(err)
			}

			_, err := f.run(t)
			if err == nil {
				t.Fatal("expected an error for a symlink loop, got nil")
			}
			msg := filepath.ToSlash(err.Error())
			// A looping unreagent.local.yaml stops config.Load before hermes-setup
			// starts, so its error has no "(nothing written)"; the file checks
			// below show that nothing was.
			want := filepath.ToSlash(link) + " is a symlink that cannot be resolved: "
			if which == "unreagent.local.yaml" {
				want = "config not readable: " + want
			}
			if !strings.HasPrefix(msg, want) {
				t.Errorf("error %q does not start with %q", err, want)
			}
			if which == "Hermes config" && !strings.HasSuffix(msg, "(nothing written)") {
				t.Errorf("error %q does not say that nothing was written", err)
			}
			for l, dest := range map[string]string{link: partner, partner: link} {
				if got, err := os.Readlink(l); err != nil || got != dest {
					t.Errorf("the link %s was changed: Readlink = %q, %v", l, got, err)
				}
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

	// k116: for the unreagent entry the edit is the old entry with url and
	// headers.Authorization set. An entry replaced whole loses the user's keys
	// and is refused, as is a change to a key or header unreagent does not own.
	var withEntry map[string]interface{}
	if err := yaml.Unmarshal([]byte("mcp_servers:\n  unreagent: {url: http://old, timeout: 120, headers: {X-Trace: on, Authorization: Bearer old}}\n"), &withEntry); err != nil {
		t.Fatal(err)
	}
	want := hermesEntryWant(lookup(withEntry, "mcp_servers", "unreagent"), "http://u", "Bearer t")
	for out, wantErr := range map[string]bool{
		"mcp_servers:\n  unreagent: {url: http://u, timeout: 120, headers: {X-Trace: on, Authorization: Bearer t}}\n":   false,
		"mcp_servers:\n  unreagent: {url: http://u, headers: {Authorization: Bearer t}}\n":                              true,
		"mcp_servers:\n  unreagent: {url: http://u, timeout: 120, headers: {Authorization: Bearer t}}\n":                true,
		"mcp_servers:\n  unreagent: {url: http://u, timeout: 60, headers: {X-Trace: on, Authorization: Bearer t}}\n":    true,
		"mcp_servers:\n  unreagent: {url: http://old, timeout: 120, headers: {X-Trace: on, Authorization: Bearer t}}\n": true,
	} {
		if err := checkEdit(withEntry, []byte(out), want, "mcp_servers", "unreagent"); (err != nil) != wantErr {
			t.Errorf("checkEdit(%q) = %v, want error: %v", out, err, wantErr)
		}
	}
	if url, auth := lookup(withEntry, "mcp_servers", "unreagent", "url"), lookup(withEntry, "mcp_servers", "unreagent", "headers", "Authorization"); url != "http://old" || auth != "Bearer old" {
		t.Errorf("hermesEntryWant changed the entry it started from: url = %#v, Authorization = %#v", url, auth)
	}

	// k116: header names ignore case, so the bearer goes under the spelling
	// the entry has; one added under another spelling, next to the old token
	// or in its place, is a change checkEdit refuses.
	var lower map[string]interface{}
	if err := yaml.Unmarshal([]byte("mcp_servers:\n  unreagent: {url: http://old, headers: {authorization: Bearer old}}\n"), &lower); err != nil {
		t.Fatal(err)
	}
	want = hermesEntryWant(lookup(lower, "mcp_servers", "unreagent"), "http://u", "Bearer t")
	for out, wantErr := range map[string]bool{
		"mcp_servers:\n  unreagent: {url: http://u, headers: {authorization: Bearer t}}\n":                            false,
		"mcp_servers:\n  unreagent: {url: http://u, headers: {Authorization: Bearer t}}\n":                            true,
		"mcp_servers:\n  unreagent: {url: http://u, headers: {authorization: Bearer old, Authorization: Bearer t}}\n": true,
	} {
		if err := checkEdit(lower, []byte(out), want, "mcp_servers", "unreagent"); (err != nil) != wantErr {
			t.Errorf("checkEdit(%q) = %v, want error: %v", out, err, wantErr)
		}
	}

	// A null or missing parent counts as empty, before and after.
	for _, b := range []map[string]interface{}{{"mcp": nil}, {}} {
		if err := checkEdit(b, []byte("mcp:\n  token: t\n"), "t", "mcp", "token"); err != nil {
			t.Errorf("checkEdit(%v) = %v, want nil", b, err)
		}
	}

	// k165: only a difference found below a mapping or list want is named. A
	// scalar want (mcp.token) and a difference changedPath cannot place (the
	// entry missing) keep the bare refusal, word for word.
	for _, c := range []struct {
		out  string
		want interface{}
		path []string
		msg  string
	}{
		{"model: x\nmcp_servers:\n  github: {command: npx}\nmcp: {token: other}\n", "t", []string{"mcp", "token"}, "the result would not hold the new mcp.token"},
		{"model: x\nmcp_servers:\n  github: {command: npx}\n", entry, []string{"mcp_servers", "unreagent"}, "the result would not hold the new mcp_servers.unreagent"},
	} {
		if err := checkEdit(before, []byte(c.out), c.want, c.path...); fmt.Sprint(err) != c.msg {
			t.Errorf("checkEdit(%q) = %v, want the bare %q", c.out, err, c.msg)
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

// k125: checkEdit has to stay wired into hermesSetup. This is one input that
// only checkEdit refuses: a folded block scalar with a more-indented line,
// outside the entry. The node edit leaves it alone, but the vendored yaml.v3
// (v3.0.1) writes it back with an extra blank line before the indented lines,
// so the result would change the prompt. A hermesSetup that no longer calls
// checkEdit writes that file. k134 took away the anchored null mcp_servers
// this test used before: childMapping now refuses it itself. This input does
// not rest on a gap in hermes.go but on the encoder: a yaml.v3 that writes the
// scalar back unchanged, or a workaround for it in the edit, needs another
// input here that only checkEdit refuses.
func TestHermesSetupChecksTheEditBeforeWriting(t *testing.T) {
	const hermesConfig = "model: hermes-3\nsystem_prompt: >\n  Rules:\n    - be brief\n    - be kind\n"
	f := newHermesFixture(t, "", hermesConfig)

	_, err := f.run(t)
	if err == nil {
		t.Fatalf("hermes-setup accepted a config yaml.v3 does not write back as read, and wrote:\n%s", readFile(t, f.hermesPath))
	}
	if want := "the result would change more than mcp_servers.unreagent"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q is not checkEdit's refusal (%q)", err, want)
	}
	if got := readFile(t, f.hermesPath); got != hermesConfig {
		t.Errorf("Hermes config was changed:\n%s", got)
	}
	if _, err := os.Stat(f.hermesPath + ".bak"); !os.IsNotExist(err) {
		t.Errorf("a .bak was created although nothing was written (stat: %v)", err)
	}
	if _, err := os.Stat(localOverridePath(f.cfgPath)); !os.IsNotExist(err) {
		t.Errorf("the generated token was saved although the Hermes edit was refused (stat: %v)", err)
	}
}

// assertNothingWritten checks that a refused run left the fixture as it was:
// the Hermes config holds hermesConfig, and neither directory holds anything
// new — no .bak, no temp file, no unreagent.local.yaml with a generated token.
func assertNothingWritten(t *testing.T, f hermesFixture, hermesConfig string) {
	t.Helper()
	if got := readFile(t, f.hermesPath); got != hermesConfig {
		t.Errorf("Hermes config was changed:\n%s", got)
	}
	for dir, want := range map[string]string{f.dir: "hermes unreagent.yaml", filepath.Dir(f.hermesPath): "config.yaml"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if got := strings.Join(names, " "); got != want {
			t.Errorf("%s holds %q after a refused run, want %q", dir, got, want)
		}
	}
}

// k145: a value yaml.v3 writes back differently is still refused, but the
// message names where it is and what the user can do about it. The vendored
// yaml.v3 (v3.0.1) adds a blank line to a folded block scalar with a
// more-indented line, and drops the leading blank line of a literal one.
func TestHermesSetupNamesTheValueTheWriterWouldChange(t *testing.T) {
	for name, c := range map[string]struct{ config, path string }{
		"folded with a more-indented line":   {"mcp_servers:\n  github:\n    command: npx\n    description: >\n      Tools:\n        - issues\n", "mcp_servers.github.description"},
		"literal starting with a blank line": {"model: hermes-3\nprompts:\n  - name: greet\n    text: |\n\n      Hello\n", "prompts[0].text"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "", c.config)

			_, err := f.run(t)
			if err == nil {
				t.Fatalf("hermes-setup accepted a config yaml.v3 does not write back as read, and wrote:\n%s", readFile(t, f.hermesPath))
			}
			for _, want := range []string{
				"the result would change more than mcp_servers.unreagent: the value at " + c.path + " would change (the YAML writer re-encodes it differently, e.g. a block scalar)",
				"rewrite that value as a quoted string, or as a block scalar without a leading blank line or extra-indented lines, then re-run hermes-setup",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
			assertNothingWritten(t, f, c.config)
		})
	}
}

// k165: a value of that kind inside the entry hermes-setup edits, one of the
// user's keys of mcp_servers.unreagent, is named the same way: the entry would
// not come out as wanted, and the message says where and what to do.
func TestHermesSetupNamesTheValueTheWriterWouldChangeInTheEntry(t *testing.T) {
	for name, c := range map[string]struct{ config, path string }{
		"folded with a more-indented line":   {"mcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    description: >\n      Tools:\n        - issues\n", "mcp_servers.unreagent.description"},
		"literal starting with a blank line": {"mcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    headers:\n      X-Note: |\n\n        hello\n", "mcp_servers.unreagent.headers.X-Note"},
		"in a list":                          {"mcp_servers:\n  unreagent:\n    notes:\n      - plain\n      - |\n\n        hello\n", "mcp_servers.unreagent.notes[1]"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "", c.config)

			_, err := f.run(t)
			if err == nil {
				t.Fatalf("hermes-setup accepted an entry yaml.v3 does not write back as read, and wrote:\n%s", readFile(t, f.hermesPath))
			}
			for _, want := range []string{
				"the result would not hold the new mcp_servers.unreagent: the value at " + c.path + " would change (the YAML writer re-encodes it differently, e.g. a block scalar)",
				"rewrite that value as a quoted string, or as a block scalar without a leading blank line or extra-indented lines, then re-run hermes-setup",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
			assertNothingWritten(t, f, c.config)
		})
	}
}

// k145: changedPath names the first value that differs, keys joined by dots,
// list indexes in brackets, keys that are not strings as they print.
func TestChangedPath(t *testing.T) {
	for _, c := range []struct{ a, b, want string }{
		{"a: 1\nb: 2\n", "a: 1\nb: 3\n", "b"},
		{"a: 1\nb: 2\n", "a: 1\nb: 2\n", ""},
		{"a: 1\n", "a: 1\nb: 2\n", "b"},
		{"a: {x: [1, {y: 2}]}\n", "a: {x: [1, {y: 3}]}\n", "a.x[1].y"},
		{"l: [[1, 2]]\n", "l: [[1, 3]]\n", "l[0][1]"},
		{"l: [1, 2]\n", "l: [1]\n", "l"},
		{"ports: {8080: a}\n", "ports: {8080: b}\n", "ports.8080"},
		{"a: {x: 1}\n", "a: [x]\n", "a"},
		{"a: 1\nb: 1\n", "a: 2\nb: 2\n", "a"},
	} {
		var a, b map[string]interface{}
		if err := yaml.Unmarshal([]byte(c.a), &a); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal([]byte(c.b), &b); err != nil {
			t.Fatal(err)
		}
		if got := changedPath(a, b); got != c.want {
			t.Errorf("changedPath(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

// k145: a merge key written quoted with the !!merge tag is a merge key to
// yaml.v3, but the encoder writes it back as a quoted "<<" without the tag,
// which every reader takes for a plain string key. It is refused before
// anything is edited, in the Hermes config and in unreagent.local.yaml.
func TestHermesSetupRefusesQuotedMergeKey(t *testing.T) {
	const want = `the merge key at line 4 is written quoted with the !!merge tag (!!merge "<<"); the YAML writer would turn it into a plain string key "<<" that merges nothing; write it unquoted as <<, then re-run hermes-setup`
	for name, hermesConfig := range map[string]string{
		"double-quoted": "defaults: &d\n  timeout: 120\nmcp_servers:\n  !!merge \"<<\": *d\n  github:\n    command: npx\n",
		"single-quoted": "defaults: &d\n  timeout: 120\nother:\n  !!merge '<<': *d\n  x: 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "", hermesConfig)

			_, err := f.run(t)
			if err == nil {
				t.Fatalf("hermes-setup accepted a quoted merge key, and wrote:\n%s", readFile(t, f.hermesPath))
			}
			if !strings.Contains(err.Error(), "left untouched: "+want) {
				t.Errorf("error %q does not give the reason %q", err, want)
			}
			assertNothingWritten(t, f, hermesConfig)
		})
	}

	t.Run("unreagent.local.yaml", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "unreagent.local.yaml")
		writeFile(t, path, "defaults: &d\n  address: 127.0.0.1:8765\nmcp:\n  !!merge \"<<\": *d\n")
		if _, err := localConfigWithToken(path, "t"); err == nil || err.Error() != want {
			t.Errorf("localConfigWithToken = %v, want the refusal %q", err, want)
		}
	})

	// Only the quoted form with the tag is refused. A plain !!merge << is
	// written back as << and still merges; a quoted "<<" without the tag is a
	// plain string key, and stays one.
	t.Run("plain !!merge and untagged quoted key kept", func(t *testing.T) {
		const hermesConfig = "defaults: &d\n  timeout: 120\ntagged:\n  !!merge <<: *d\n  x: 1\nquoted:\n  \"<<\": *d\n  x: 1\n"
		f := newHermesFixture(t, "user-token-0123456789", hermesConfig)

		if _, err := f.run(t); err != nil {
			t.Fatalf("hermes-setup refused a merge key it can write back: %v", err)
		}
		got := readFile(t, f.hermesPath)
		for _, want := range []string{"tagged:\n  <<: *d\n  x: 1\n", "quoted:\n  \"<<\": *d\n  x: 1\n"} {
			if !strings.Contains(got, want) {
				t.Errorf("Hermes config lacks %q:\n%s", want, got)
			}
		}
		doc := readYAMLMap(t, f.hermesPath)
		for _, c := range []struct {
			keys []string
			want interface{}
		}{
			{[]string{"tagged", "timeout"}, 120},
			{[]string{"quoted", "<<", "timeout"}, 120},
			{[]string{"quoted", "timeout"}, nil},
		} {
			if v := lookup(doc, c.keys...); v != c.want {
				t.Errorf("%s = %#v, want %#v", strings.Join(c.keys, "."), v, c.want)
			}
		}
		assertUnreagentEntry(t, doc, "user-token-0123456789")
	})
}

// k145: a key on the way to what hermes-setup edits that reads as mcp_servers,
// the entry's name, url, headers or Authorization but is not written as a
// plain string — an alias, or a tag such as !!binary — is refused early and
// named. The node edit finds keys by their plain spelling: it would add a
// second key beside it, which checkEdit refused without saying why (url), or
// which went through unnoticed (Authorization: the later key wins). A tagged
// key got a message that blamed a merge key (mcp_servers) or a key that is not
// a string (headers).
func TestHermesSetupRefusesKeysNotWrittenPlain(t *testing.T) {
	for name, c := range map[string]struct{ config, wantErr string }{
		"alias for url": {
			"names: [&u url]\nmcp_servers:\n  unreagent:\n    *u : http://old.invalid/mcp\n",
			"the key mcp_servers.unreagent.url is written as *u, not as a plain string; hermes-setup would add a second url instead of changing it; write the key as plain url, then re-run hermes-setup",
		},
		"alias for Authorization": {
			"names: [&a authorization]\nmcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    headers:\n      *a : Bearer old\n",
			"the key mcp_servers.unreagent.headers.authorization is written as *a, not as a plain string; hermes-setup would add a second authorization instead of changing it",
		},
		"!!binary for headers": {
			"mcp_servers:\n  unreagent:\n    url: http://old.invalid/mcp\n    !!binary aGVhZGVycw==: {X-Trace: \"on\"}\n",
			"the key mcp_servers.unreagent.headers is written as !!binary aGVhZGVycw==, not as a plain string",
		},
		"!!binary for mcp_servers": {
			"!!binary bWNwX3NlcnZlcnM=:\n  github:\n    command: npx\n",
			"the key mcp_servers is written as !!binary bWNwX3NlcnZlcnM=, not as a plain string",
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newHermesFixture(t, "", c.config)

			_, err := f.run(t)
			if err == nil {
				t.Fatalf("hermes-setup accepted a key not written as a plain string, and wrote:\n%s", readFile(t, f.hermesPath))
			}
			if !strings.Contains(err.Error(), "left untouched: "+c.wantErr) {
				t.Errorf("error %q does not give the reason %q", err, c.wantErr)
			}
			assertNothingWritten(t, f, c.config)
		})
	}

	// The token edit of unreagent.local.yaml looks for mcp and token the same
	// way. Without the check, checkEdit blamed the YAML writer for an alias;
	// a tagged token key, which makes mcp decode with a key that is not a
	// string, got "mcp is not a mapping" while that check came first.
	for name, c := range map[string]struct{ config, want string }{
		"local alias for mcp": {
			"names: [&m mcp]\n*m :\n  address: 127.0.0.1:9000\n",
			"the key mcp is written as *m, not as a plain string; hermes-setup would add a second mcp instead of changing it; write the key as plain mcp, then re-run hermes-setup",
		},
		"local !!binary for token": {
			"mcp:\n  address: 127.0.0.1:9000\n  !!binary dG9rZW4=: old\n",
			"the key mcp.token is written as !!binary dG9rZW4=, not as a plain string; hermes-setup would add a second token instead of changing it; write the key as plain token, then re-run hermes-setup",
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "unreagent.local.yaml")
			writeFile(t, path, c.config)
			if _, err := localConfigWithToken(path, "t"); err == nil || err.Error() != c.want {
				t.Errorf("localConfigWithToken = %v, want the refusal %q", err, c.want)
			}
		})
	}
}
