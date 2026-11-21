package config

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// hermesConfig is an unreagent.yaml with Hermes enabled and, unless name is
// nil, serverName set to *name (quoted, so that YAML takes it as written).
func hermesConfig(name *string) string {
	if name == nil {
		return "agent: {hermes: {enabled: true}}\n"
	}
	return fmt.Sprintf("agent: {hermes: {enabled: true, serverName: %s}}\n", strconv.Quote(*name))
}

// k131: agent.hermes.serverName becomes a key under mcp_servers in the Hermes
// config. Every name an MCP server sensibly has loads as written; unset or
// empty means the default.
func TestHermesServerNameAccepted(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	str := func(s string) *string { return &s }
	for _, tc := range []struct {
		name *string
		want string
	}{
		{nil, MCPServerName},
		{str(""), MCPServerName},
		{str("unreagent"), "unreagent"},
		{str("Unreagent2"), "Unreagent2"},
		{str("my-game"), "my-game"},
		{str("my_game"), "my_game"},
		{str("ue5-dev_box"), "ue5-dev_box"},
		{str("x"), "x"},
		{str("X1"), "X1"},
	} {
		yaml := hermesConfig(tc.name)
		c, _, err := Load(writeConfigFiles(t, t.TempDir(), yaml, ""))
		if err != nil {
			t.Errorf("%q: %v", yaml, err)
			continue
		}
		if got := c.Agent.Hermes.ServerName; got != tc.want {
			t.Errorf("%q: serverName = %q; want %q", yaml, got, tc.want)
		}
	}
}

// k131: a serverName that is not a plain YAML key used to reach the merge in
// hermes-setup ("<<" was written as a merge key, and only the late edit check
// refused it, without naming the option). It is a load error now, naming the
// option and the value.
func TestHermesServerNameRefused(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	const (
		chars = `agent.hermes.serverName invalid: %q (allowed: ASCII letters, digits, "_" and "-", starting with a letter)`
		typed = `agent.hermes.serverName invalid: %q (YAML may read it as a boolean or null rather than a name)`
	)
	for _, tc := range []struct{ name, format string }{
		// Characters YAML gives a meaning, or a key would need quoting for.
		{"<<", chars},
		{"&anchor", chars},
		{"*alias", chars},
		{"!tag", chars},
		{"-dash", chars},
		{"?key", chars},
		{"~", chars},
		{"a: b", chars},
		{"a:b", chars},
		{"a #b", chars},
		{"#a", chars},
		{"a\nb", chars},
		{" unreagent", chars},
		{"unreagent ", chars},
		{"my game", chars},
		{"{a}", chars},
		{"a,b", chars},
		{"'a'", chars},
		{"${PROJECT_NAME}", chars},
		// A dot is plain to YAML, but not a tool-name character.
		{"ue5.4", chars},
		{"1.5", chars},
		// Plain characters, but not at the start.
		{".hidden", chars},
		{"_private", chars},
		{"2game", chars},
		// Outside ASCII.
		{"ünreagent", chars},
		// YAML numbers, dates and timestamps start with a digit, so the leading
		// letter refuses them all — including those yaml.v3 writes unquoted
		// and PyYAML misreads or cannot load: integers past 64 bits, "0x_" and
		// "0b_", an impossible date.
		{"123", chars},
		{"1_000", chars},
		{"0x1F", chars},
		{"1e3", chars},
		{"2001-12-14", chars},
		{"0x" + strings.Repeat("F", 17), chars},
		{"0b1" + strings.Repeat("0", 64), chars},
		{"1" + strings.Repeat("0", 309), chars},
		{"0x_", chars},
		{"0b_", chars},
		{"2001-02-30", chars},
		// Letter-first names YAML resolves to something other than a string.
		// The YAML 1.1 booleans (yes, on, ...) are what a reader such as
		// PyYAML takes them for.
		{"true", typed},
		{"False", typed},
		{"null", typed},
		{"NULL", typed},
		{"yes", typed},
		{"No", typed},
		{"on", typed},
		{"OFF", typed},
		{"y", typed},
		{"n", typed},
	} {
		_, _, err := Load(writeConfigFiles(t, t.TempDir(), hermesConfig(&tc.name), ""))
		want := fmt.Sprintf(tc.format, tc.name)
		if err == nil {
			t.Errorf("serverName %q loaded; want the error %s", tc.name, want)
		} else if err.Error() != want {
			t.Errorf("serverName %q:\n got: %s\nwant: %s", tc.name, err, want)
		}
	}
}

// k131: the check runs only with agent.hermes.enabled, like the other checks
// of an optional section: without it hermes-setup does nothing with the name.
// The default name is refused by nothing, and the rule refuses an empty name
// even though Load turns one into the default before it gets there.
func TestHermesServerNameCheckedOnlyWhenEnabled(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	yaml := "agent: {hermes: {serverName: \"<<\"}}\n"
	c, _, err := Load(writeConfigFiles(t, t.TempDir(), yaml, ""))
	if err != nil {
		t.Fatalf("hermes disabled: %v", err)
	}
	if c.Agent.Hermes.ServerName != "<<" {
		t.Errorf("serverName = %q; want it kept as written", c.Agent.Hermes.ServerName)
	}
	if err := validHermesServerName(MCPServerName); err != nil {
		t.Errorf("default name %q refused: %v", MCPServerName, err)
	}
	if err := validHermesServerName(""); err == nil {
		t.Error("empty name accepted")
	}
}

// k131: the check runs on the overlaid config, so unreagent.local.yaml can
// enable Hermes, set the name, or both, and the name that ends up in force is
// the one checked.
func TestHermesServerNameFromTheLocalOverlay(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	const chars = `agent.hermes.serverName invalid: %q (allowed: ASCII letters, digits, "_" and "-", starting with a letter)`
	for _, tc := range []struct {
		name, base, local, wantErr, wantName string
	}{
		{
			name:    "local enables Hermes, base name checked",
			base:    "agent: {hermes: {serverName: \"<<\"}}\n",
			local:   "agent: {hermes: {enabled: true}}\n",
			wantErr: fmt.Sprintf(chars, "<<"),
		},
		{
			name:    "bad local name refused",
			base:    "agent: {hermes: {enabled: true, serverName: unreagent}}\n",
			local:   "agent: {hermes: {serverName: \"ue5.4\"}}\n",
			wantErr: fmt.Sprintf(chars, "ue5.4"),
		},
		{
			name:     "good local name replaces a bad base name",
			base:     "agent: {hermes: {enabled: true, serverName: \"<<\"}}\n",
			local:    "agent: {hermes: {serverName: unreagent-dev}}\n",
			wantName: "unreagent-dev",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, err := Load(writeConfigFiles(t, t.TempDir(), tc.base, tc.local))
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("loaded with serverName %q; want the error %s", c.Agent.Hermes.ServerName, tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Errorf("\n got: %s\nwant: %s", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := c.Agent.Hermes.ServerName; got != tc.wantName {
				t.Errorf("serverName = %q; want %q", got, tc.wantName)
			}
		})
	}
}
