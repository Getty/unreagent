package config

import (
	"fmt"
	"strings"
	"testing"
)

// k57: the client URL was "http://" + mcp.address + "/mcp". A Go listen
// address with an empty or wildcard host listens on every interface but is
// nothing to dial: ":8765" became http://:8765/mcp and "0.0.0.0:8765" (the
// README's remote-access setting) http://0.0.0.0:8765/mcp, handed to the agent
// and written into every mcp.writeConfig file and the Hermes entry. Such a host
// becomes the loopback of its family; any other host stays as written, and an
// IPv6 literal stays bracketed.
func TestMCPClientURL(t *testing.T) {
	for _, tc := range []struct{ address, want string }{
		// Every interface: the loopback of the same family.
		{":8765", "http://127.0.0.1:8765/mcp"},
		{"0.0.0.0:8765", "http://127.0.0.1:8765/mcp"},
		{"[::]:8765", "http://[::1]:8765/mcp"},
		{"[::0]:8765", "http://[::1]:8765/mcp"},
		{"[0:0:0:0:0:0:0:0]:8765", "http://[::1]:8765/mcp"},
		{"[::ffff:0.0.0.0]:8765", "http://127.0.0.1:8765/mcp"},
		// A host to dial stays as written.
		{"127.0.0.1:8765", "http://127.0.0.1:8765/mcp"},
		{"localhost:8765", "http://localhost:8765/mcp"},
		{"192.168.1.20:9000", "http://192.168.1.20:9000/mcp"},
		{"ue-box.lan:8765", "http://ue-box.lan:8765/mcp"},
		{"[::1]:8765", "http://[::1]:8765/mcp"},
		{"[2001:db8::5]:8765", "http://[2001:db8::5]:8765/mcp"},
	} {
		if got := (MCPConfig{Address: tc.address}).ClientURL(); got != tc.want {
			t.Errorf("mcp.address %q: client URL %q; want %q", tc.address, got, tc.want)
		}
	}
}

// mcpAddressConfig is an unreagent.yaml with the MCP server on, at address.
func mcpAddressConfig(address string) string {
	return fmt.Sprintf("mcp: {enabled: true, address: %q}\n", address)
}

// k57: Load never looked at mcp.address. One without a port reached the bind
// only, a port 0 bound a random port the client URL could not name, and a
// service name ("http"), which net.Listen accepts, went into the URL as
// written. Every such address is a load error naming the option and the value.
func TestMCPAddressRefused(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, tc := range []struct{ address, cause string }{
		{"127.0.0.1", "missing port"},
		{"localhost", "missing port"},
		{"[::1]", "missing port"},
		{"::1:8765", "too many colons"},
		{"not an address", "missing port"},
		{"127.0.0.1:", "port must be a number from 1 to 65535"},
		{"127.0.0.1:0", "port must be a number from 1 to 65535"},
		{"127.0.0.1:65536", "port must be a number from 1 to 65535"},
		{"127.0.0.1:70000", "port must be a number from 1 to 65535"},
		{"127.0.0.1:-1", "port must be a number from 1 to 65535"},
		{"127.0.0.1:+8765", "port must be a number from 1 to 65535"},
		{"127.0.0.1:http", "port must be a number from 1 to 65535"},
		{"127.0.0.1:87 65", "port must be a number from 1 to 65535"},
	} {
		_, _, err := Load(writeConfigFiles(t, t.TempDir(), mcpAddressConfig(tc.address), ""))
		if err == nil {
			t.Errorf("mcp.address %q loaded; want a load error", tc.address)
			continue
		}
		for _, want := range []string{fmt.Sprintf("mcp.address invalid: %q", tc.address), tc.cause, "host:port"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("mcp.address %q: error %q does not mention %q", tc.address, err, want)
			}
		}
	}
}

// k57: every host:port with a port from 1 to 65535 loads, the wildcard hosts
// included — listening on every interface is the remote-access setting.
func TestMCPAddressAccepted(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, address := range []string{
		":8765", "0.0.0.0:8765", "[::]:8765", "127.0.0.1:1", "localhost:65535",
		"[::1]:8765", "192.168.1.20:9000",
	} {
		c, _, err := Load(writeConfigFiles(t, t.TempDir(), mcpAddressConfig(address), ""))
		if err != nil {
			t.Errorf("mcp.address %q: %v", address, err)
			continue
		}
		if c.MCP.Address != address {
			t.Errorf("mcp.address = %q; want it kept as written, %q", c.MCP.Address, address)
		}
	}
}

// k57: like the other checks of an optional section, the address is checked
// only where something uses it: the MCP server (mcp.enabled) and the Hermes
// entry hermes-setup writes (agent.hermes.enabled, which hermes-setup checks
// without looking at mcp.enabled).
func TestMCPAddressCheckedOnlyWhenUsed(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	unused := "mcp: {enabled: false, address: \"127.0.0.1\"}\n"
	if _, _, err := Load(writeConfigFiles(t, t.TempDir(), unused, "")); err != nil {
		t.Errorf("MCP off, Hermes off: %v", err)
	}
	hermes := unused + "agent: {hermes: {enabled: true}}\n"
	_, _, err := Load(writeConfigFiles(t, t.TempDir(), hermes, ""))
	errContains(t, err, `mcp.address invalid: "127.0.0.1"`)
}
