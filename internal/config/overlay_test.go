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
	// The default unreal.editor needs ${ENGINE}; an unresolved one is a load
	// error.
	t.Setenv("UE_ROOT", "/ue")
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
	t.Setenv("UE_ROOT", "/ue") // see TestLocalOverlaySemantics
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

// k15: skills is a struct section, so the local overlay decodes it field by
// field — each key it sets replaces only that key. mcp: false in the local
// file must not drop the base's materialize/dir (a whole-struct replace would
// also reset them), and the enabled the overlay never mentions keeps its
// default. The dir comes out of the base, substituted.
func TestLocalOverlaySkillsFieldByField(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	dir := t.TempDir()
	writeUProject(t, dir, "Game.uproject")
	base := "skills: {materialize: true, dir: \"${PROJECT_DIR}/Skills\"}\n"
	c, info, err := Load(writeConfigFiles(t, dir, base, "skills: {mcp: false}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if info.LocalPath == "" {
		t.Fatal("local overlay was not picked up")
	}
	s := c.Skills
	if boolVal(s.MCP) {
		t.Error("skills.mcp = true; want the local false")
	}
	if !s.Materialize {
		t.Error("skills.materialize: base true lost although local does not set it")
	}
	if want := filepath.ToSlash(dir) + "/Skills"; s.Dir != want {
		t.Errorf("skills.dir = %q; want the base value substituted, %q", s.Dir, want)
	}
	if !boolVal(s.Enabled) {
		t.Error("skills.enabled = false; want the default true")
	}
	if s.ViaMCP() || !s.ViaFiles() {
		t.Errorf("ViaMCP/ViaFiles = %v/%v; want false/true", s.ViaMCP(), s.ViaFiles())
	}
}

// k15: an overlay's skills.enabled: false turns both paths off over a base
// that has them on; the both-paths-off load error applies only while skills
// are enabled, so mcp: false from the base is no error here.
func TestLocalOverlaySkillsDisabled(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	dir := t.TempDir()
	writeUProject(t, dir, "Game.uproject")
	base := "skills: {mcp: false, materialize: false}\n"
	if _, _, err := Load(writeConfigFiles(t, t.TempDir(), base, "")); err == nil {
		t.Fatal("base alone: want the both-paths-off error")
	}
	c, _, err := Load(writeConfigFiles(t, dir, base, "skills: {enabled: false}\n"))
	if err != nil {
		t.Fatalf("overlay enabled: false: %v", err)
	}
	if boolVal(c.Skills.Enabled) || c.Skills.ViaMCP() || c.Skills.ViaFiles() {
		t.Errorf("enabled/ViaMCP/ViaFiles = %v/%v/%v; want false/false/false", boolVal(c.Skills.Enabled), c.Skills.ViaMCP(), c.Skills.ViaFiles())
	}
}

// k264: unreal.mcp is a struct inside the unreal section, so the overlay
// decodes it field by field like any other: a local port leaves the base's
// enabled alone, and a local enabled: false turns the switch off over a base
// that has it on. Unlike a list or a map, a struct is not reset by null:
// yaml.v3 leaves it as the base set it, so enabled: false is the only way an
// overlay turns the switch off.
func TestLocalOverlayUnrealMCP(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	base := "unreal:\n  mcp: {enabled: true, port: 8100}\nmcp: {enabled: true}\n"
	for _, tc := range []struct {
		local   string
		enabled bool
		port    int
	}{
		{"unreal:\n  mcp: {port: 8200}\n", true, 8200},
		{"unreal:\n  mcp: {enabled: false}\n", false, 8100},
		{"unreal:\n  maxRestarts: 2\n", true, 8100},
		{"unreal:\n  mcp: ~\n", true, 8100},
	} {
		c, info, err := Load(writeConfigFiles(t, t.TempDir(), base, tc.local))
		if err != nil {
			t.Fatalf("local %q: %v", tc.local, err)
		}
		if info.LocalPath == "" {
			t.Fatalf("local %q: overlay was not picked up", tc.local)
		}
		if c.Unreal.MCP.Enabled != tc.enabled || c.Unreal.MCP.Port != tc.port {
			t.Errorf("local %q: unreal.mcp = %+v; want enabled %v, port %d", tc.local, c.Unreal.MCP, tc.enabled, tc.port)
		}
	}
}
