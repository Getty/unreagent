package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLocalOverlaySemantics pins how unreagent.local.yaml overlays
// unreagent.yaml (a second YAML decode into the same struct, yaml.v3
// semantics), as the unreagent-config skill documents it: a scalar wins, an
// absent key keeps the base value, a nested struct is decoded field by field,
// a list replaces, a map merges by key with each given entry replaced whole.
func TestLocalOverlaySemantics(t *testing.T) {
	dir := t.TempDir()
	base := `
unreal:
  args: ["-base"]
  restart: always
mcp:
  enabled: true
  address: 127.0.0.1:1111
  extraServers:
    a: {url: http://a}
    b: {url: http://b, headers: {X-Base: "1"}}
permissions:
  deny: ["Bash(rm *)"]
commands:
  hello: {description: base, command: base-cmd, args: ["base-arg"]}
`
	local := `
unreal:
  maxRestarts: 5
mcp:
  address: 127.0.0.1:2222
  extraServers:
    b: {url: http://b2}
    c: {url: http://c}
permissions:
  deny: ["Bash(dd *)"]
commands:
  hello: {command: local-cmd}
`
	path := filepath.Join(dir, "unreagent.yaml")
	if err := os.WriteFile(path, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unreagent.local.yaml"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	c, info, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.LocalPath == "" {
		t.Fatal("local overlay was not picked up")
	}

	// Scalar: local wins; absent key: base stays.
	if c.MCP.Address != "127.0.0.1:2222" {
		t.Errorf("mcp.address = %q; want the local value", c.MCP.Address)
	}
	if !c.MCP.Enabled {
		t.Error("mcp.enabled: base value lost although local does not set it")
	}

	// Nested struct: field by field.
	if !reflect.DeepEqual(c.Unreal.Args, []string{"-base"}) || c.Unreal.Restart != RestartAlways || c.Unreal.MaxRestarts != 5 {
		t.Errorf("unreal = args %v, restart %q, maxRestarts %d; want [-base], always, 5",
			c.Unreal.Args, c.Unreal.Restart, c.Unreal.MaxRestarts)
	}

	// List: replaced, not appended.
	if want := []string{"Bash(dd *)"}; !reflect.DeepEqual(c.Permissions.Deny, want) {
		t.Errorf("permissions.deny = %v; want %v", c.Permissions.Deny, want)
	}

	// Map: merged by key; a given entry's value is replaced whole (b loses
	// its headers).
	wantServers := map[string]map[string]interface{}{
		"a": {"url": "http://a"},
		"b": {"url": "http://b2"},
		"c": {"url": "http://c"},
	}
	if !reflect.DeepEqual(c.MCP.ExtraServers, wantServers) {
		t.Errorf("mcp.extraServers = %v; want %v", c.MCP.ExtraServers, wantServers)
	}

	// Map of structs: the entry is replaced whole too, not merged field by
	// field like a top-level section.
	// (substitute turns a nil Args into an empty slice, hence len.)
	if h := c.Commands["hello"]; h.Command != "local-cmd" || h.Description != "" || len(h.Args) != 0 {
		t.Errorf("commands.hello = %#v; want only command local-cmd", h)
	}
}

// TestLocalOverlayCannotClearMapWithEmptyMap pins the consequence of the
// merge: {} in the local file leaves the base map alone, null clears it.
func TestLocalOverlayCannotClearMapWithEmptyMap(t *testing.T) {
	base := "mcp:\n  extraServers:\n    a: {url: http://a}\n"
	for _, tc := range []struct {
		local string
		want  int
	}{
		{"mcp:\n  extraServers: {}\n", 1},
		{"mcp:\n  extraServers: ~\n", 0},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "unreagent.yaml")
		if err := os.WriteFile(path, []byte(base), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "unreagent.local.yaml"), []byte(tc.local), 0o644); err != nil {
			t.Fatal(err)
		}
		c, _, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(c.MCP.ExtraServers); got != tc.want {
			t.Errorf("local %q: %d extraServers left; want %d", tc.local, got, tc.want)
		}
	}
}
