package config

import (
	"fmt"
	"strings"
	"testing"
)

// k264: unreal.mcp switches Epic's Unreal MCP server (UE 5.8) on. It is off
// by default, its port defaults to 8000 (the plugin's ServerPortNumber), and
// 0 means that default, as for the delays.
func TestUnrealMCPDefaults(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, base := range []string{"", "unreal: {mcp: {port: 0}}\n", "unreal: {mcp: {enabled: false}}\n"} {
		c, _, err := Load(writeConfigFiles(t, t.TempDir(), base, ""))
		if err != nil {
			t.Fatalf("%q: %v", base, err)
		}
		if c.Unreal.MCP.Enabled || c.Unreal.MCP.Port != DefaultUnrealMCPPort {
			t.Errorf("%q: unreal.mcp = %+v; want off, port %d", base, c.Unreal.MCP, DefaultUnrealMCPPort)
		}
	}
	if DefaultUnrealMCPPort != 8000 {
		t.Errorf("DefaultUnrealMCPPort = %d; want 8000, the plugin's default", DefaultUnrealMCPPort)
	}
	if UnrealMCPServerName != "unreal-mcp" {
		t.Errorf("UnrealMCPServerName = %q; want unreal-mcp, the name the plugin's GenerateClientConfig writes", UnrealMCPServerName)
	}
}

func TestUnrealMCPOverride(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	c, _, err := Load(writeConfigFiles(t, t.TempDir(), "unreal: {mcp: {enabled: true, port: 8123}}\nmcp: {enabled: true}\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Unreal.MCP.Enabled || c.Unreal.MCP.Port != 8123 {
		t.Errorf("unreal.mcp = %+v; want on, port 8123", c.Unreal.MCP)
	}
}

// The URL the agent gets: always the loopback, where the plugin listens.
func TestUnrealMCPURL(t *testing.T) {
	for port, want := range map[int]string{
		8000:  "http://127.0.0.1:8000/mcp",
		1:     "http://127.0.0.1:1/mcp",
		65535: "http://127.0.0.1:65535/mcp",
	} {
		if got := (UnrealMCPConfig{Enabled: true, Port: port}).URL(); got != want {
			t.Errorf("port %d: URL %q; want %q", port, got, want)
		}
	}
}

func TestUnrealMCPPortRefused(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, port := range []string{"-1", "65536", "70000"} {
		base := fmt.Sprintf("unreal: {mcp: {enabled: true, port: %s}}\n", port)
		_, _, err := Load(writeConfigFiles(t, t.TempDir(), base, ""))
		errContains(t, err, "unreal.mcp.port invalid: "+port, "from 1 to 65535")
	}
	// Checked only when the switch is on, like every optional section.
	if _, _, err := Load(writeConfigFiles(t, t.TempDir(), "unreal: {mcp: {port: 70000}}\n", "")); err != nil {
		t.Errorf("switch off, port 70000: %v", err)
	}
}

// The editor's server and the launcher's own cannot share a port. The hosts
// are not compared: a wildcard or LAN mcp.address takes the port as well.
func TestUnrealMCPPortCollidesWithMCPAddress(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	// validMCPAddress takes a leading zero ("08000" is port 8000).
	for _, address := range []string{"127.0.0.1:8000", ":8000", "192.168.1.20:8000", "127.0.0.1:08000"} {
		base := fmt.Sprintf("unreal: {mcp: {enabled: true}}\nmcp: {enabled: true, address: %q}\n", address)
		_, _, err := Load(writeConfigFiles(t, t.TempDir(), base, ""))
		errContains(t, err, "unreal.mcp.port", "mcp.address", "8000")
	}
	explicit := "unreal: {mcp: {enabled: true, port: 8765}}\nmcp: {enabled: true}\n" // the default address
	_, _, err := Load(writeConfigFiles(t, t.TempDir(), explicit, ""))
	errContains(t, err, "unreal.mcp.port", "mcp.address", "8765")

	for name, base := range map[string]string{
		"different ports":  "unreal: {mcp: {enabled: true, port: 8001}}\nmcp: {enabled: true, address: \"127.0.0.1:8000\"}\n",
		"launcher MCP off": "unreal: {mcp: {enabled: true}}\nmcp: {enabled: false, address: \"127.0.0.1:8000\"}\n",
		"unreal MCP off":   "unreal: {mcp: {enabled: false}}\nmcp: {enabled: true, address: \"127.0.0.1:8000\"}\n",
	} {
		if _, _, err := Load(writeConfigFiles(t, t.TempDir(), base, "")); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// With the switch on, the launcher hands the agent the unreal-mcp entry itself
// (k265); a hand-written one of the same name would shadow it or be shadowed.
// Without the switch a hand-written entry is passed through as before.
func TestUnrealMCPRefusesHandWrittenEntry(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	entry := "mcp:\n  enabled: true\n  extraServers:\n    unreal-mcp: {type: http, url: \"http://127.0.0.1:8000/mcp\"}\n"
	_, _, err := Load(writeConfigFiles(t, t.TempDir(), "unreal: {mcp: {enabled: true}}\n"+entry, ""))
	errContains(t, err, "unreal.mcp.enabled", "mcp.extraServers.unreal-mcp")

	// The entry can come from the overlay too.
	_, _, err = Load(writeConfigFiles(t, t.TempDir(), "unreal: {mcp: {enabled: true}}\nmcp: {enabled: true}\n", entry))
	errContains(t, err, "unreal.mcp.enabled", "mcp.extraServers.unreal-mcp")

	c, _, err := Load(writeConfigFiles(t, t.TempDir(), entry, ""))
	if err != nil {
		t.Fatalf("switch off: %v", err)
	}
	if _, ok := c.MCP.ExtraServers[UnrealMCPServerName]; !ok {
		t.Error("switch off: the hand-written unreal-mcp entry is gone")
	}
	// Another name is no conflict.
	other := strings.Replace(entry, "unreal-mcp:", "epic:", 1)
	if _, _, err := Load(writeConfigFiles(t, t.TempDir(), "unreal: {mcp: {enabled: true}}\n"+other, "")); err != nil {
		t.Errorf("entry under another name: %v", err)
	}
}
