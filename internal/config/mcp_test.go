package config

import (
	"fmt"
	"slices"
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
		// k162: a zone would go into the client URL unescaped, and a Node
		// client takes none at all; [::%eth0] was no wildcard either.
		{"[fe80::1%eth0]:8765", "no IPv6 zone allowed"},
		{"[::%eth0]:8765", "no IPv6 zone allowed"},
		{"[fe80::1%]:8765", "no IPv6 zone allowed"},
		// k162: brackets hold an IPv6 address and nothing else.
		{"[127.0.0.1]:8765", "brackets hold an IPv6 address only"},
		{"[localhost]:8765", "brackets hold an IPv6 address only"},
		{"[]:8765", "brackets hold an IPv6 address only"},
		// k162: any other host is a name of ASCII letters, digits, -, _ and .;
		// user info, a path or a space reached the Hermes entry as written.
		{"user@host:8765", "host must be an IP address or a name"},
		{"a/b:8765", "host must be an IP address or a name"},
		{" 127.0.0.1:8765", "host must be an IP address or a name"},
		{"ue box:8765", "host must be an IP address or a name"},
		{"bücher.example:8765", "host must be an IP address or a name"},
		{"127.0.0.1%eth0:8765", "host must be an IP address or a name"},
	} {
		_, _, err := Load(writeConfigFiles(t, t.TempDir(), mcpAddressConfig(tc.address), ""))
		if err == nil {
			t.Errorf("mcp.address %q loaded; want a load error", tc.address)
			continue
		}
		// k162: the hint shows the bracketed IPv6 form too, the one
		// "::1:8765" is missing.
		for _, want := range []string{fmt.Sprintf("mcp.address invalid: %q", tc.address), tc.cause, "want host:port, e.g. 127.0.0.1:8765 or [::1]:8765"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("mcp.address %q: error %q does not mention %q", tc.address, err, want)
			}
		}
	}
}

// k57: every host:port with a port from 1 to 65535 loads, the wildcard hosts
// included — listening on every interface is the remote-access setting.
// k162: the host is empty, an IP address (IPv6 in brackets) or a name of ASCII
// letters, digits, -, _ (a Windows computer name) and ., in any case and with
// a trailing dot.
func TestMCPAddressAccepted(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, address := range []string{
		":8765", "0.0.0.0:8765", "[::]:8765", "127.0.0.1:1", "localhost:65535",
		"[::1]:8765", "192.168.1.20:9000",
		"127.0.0.1:8765", "localhost:8765", "ue-box.lan:8765", "UE-Box.LAN.:8765",
		"my_pc:8765",
		"[2001:db8::5]:8765", "[0:0:0:0:0:0:0:0]:8765", "[::ffff:0.0.0.0]:8765",
		"[::ffff:127.0.0.1]:8765",
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

// allowedOriginsConfig is an unreagent.yaml with the MCP server on and the
// given origins as mcp.allowedOrigins.
func allowedOriginsConfig(origins ...string) string {
	var b strings.Builder
	b.WriteString("mcp:\n  enabled: true\n  allowedOrigins:\n")
	for _, o := range origins {
		fmt.Fprintf(&b, "    - %q\n", o)
	}
	return b.String()
}

// k60: every origin a browser sends — scheme://host[:port] with http or
// https, scheme://host for a browser extension — loads and is kept as
// written; the server compares case-insensitively, so case is no reason to
// refuse. A port is the default only for its own scheme. An IP host is
// accepted in the one form a browser writes it.
func TestMCPAllowedOriginsAccepted(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	origins := []string{
		"https://ide.example.com",
		"http://localhost:3000",
		"HTTPS://IDE.Example.com:8443",
		"http://ide.example.com.",
		"https://ide.example.com:65535",
		"http://ide.example.com:443",
		"https://ide.example.com:80",
		"https://xn--bcher-kva.example",
		"http://192.168.1.20:8080",
		"http://127.0.0.1",
		"http://0.0.0.0",
		"http://[::1]:5173",
		"http://[2001:DB8::1]",
		"http://[::ffff:7f00:1]",
		"chrome-extension://abcdefghijklmnopabcdefghijklmnop",
		"moz-extension://3f2c8a1e-1111-2222-3333-444455556666",
		"vscode-webview://1abc2def",
		"CHROME-EXTENSION://Abcdefghijklmnop",
		// To the URL Standard an extension's host is opaque: no IPv4 reading.
		// Chromium parses its own extension scheme like http, but no extension
		// id is numeric.
		"moz-extension://2130706433",
		// k166: a last label of 0x and a non-hex digit is no number to a
		// browser, so the host is a name; so is an empty last label, left
		// after the one trailing dot a browser drops.
		"http://ide.example.0xg",
		"http://ide.example..",
	}
	c, _, err := Load(writeConfigFiles(t, t.TempDir(), allowedOriginsConfig(origins...), ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(c.MCP.AllowedOrigins, origins) {
		t.Errorf("mcp.allowedOrigins = %q; want it kept as written, %q", c.MCP.AllowedOrigins, origins)
	}
}

// k60: the server compares an Origin header with each entry as a whole
// string, so an entry no browser sends would never match — the allowlist
// would refuse the very client it was written for. Every such entry is a load
// error naming the option, its index, the value and the cause.
func TestMCPAllowedOriginsRefused(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, tc := range []struct{ origin, cause string }{
		{"", "empty"},
		{"https://*.example.com", "wildcards are not supported"},
		{"*", "wildcards are not supported"},
		{"https://ide.example.com:*", "wildcards are not supported"},
		{"ide.example.com", "missing scheme"},
		{"null", `"null" is the Origin of every sandboxed frame and local file`},
		{"NULL", `"null" is the Origin of every sandboxed frame and local file`},
		{"ftp://ide.example.com", "a browser sends no Origin with scheme ftp"},
		{"ws://ide.example.com", "a browser sends no Origin with scheme ws"},
		{"wss://ide.example.com", "a browser sends no Origin with scheme wss"},
		{"file://", "a browser sends no Origin with scheme file"},
		{"file://server", "a browser sends no Origin with scheme file"},
		{"about:blank", "missing host"},
		{"https://ide.example.com/", "no path"},
		{"https://ide.example.com/app", "no path"},
		{"https://ide.example.com?x=1", "no query"},
		{"https://ide.example.com?", "no query"},
		{"https://ide.example.com#top", "no fragment"},
		{"https://ide.example.com#", "no fragment"},
		{"https://user@ide.example.com", "no user info"},
		{"https://", "missing host"},
		{"https://:8443", "missing host"},
		{"https:ide.example.com", "missing host"},
		{"https://ide.example.com:", "port must be a number from 1 to 65535"},
		{"https://ide.example.com:0", "port must be a number from 1 to 65535"},
		{"https://ide.example.com:65536", "port must be a number from 1 to 65535"},
		{"https://ide.example.com:08443", "without leading zeros"},
		{"https://ide.example.com:http", "invalid port"},
		{"https://ide.example.com:443", "default port"},
		{"http://ide.example.com:80", "default port"},
		{"HTTPS://ide.example.com:443", "default port"},
		{"https://bücher.example", "punycode"},
		{"https://ide.example.com ", "invalid character"},
		{"https://%69de.example.com", "invalid URL escape"},
		// A browser extension's origin is exactly scheme://host.
		{"chrome-extension://abcdefghijklmnop:8443", "no port allowed with scheme chrome-extension"},
		{"chrome-extension://abcdefghijklmnop:", "no port allowed with scheme chrome-extension"},
		{"chrome-extension://abcdefghijklmnop/", "no path"},
		{"chrome-extension://abcdefghijklmnop/popup.html", "no path"},
		{"chrome-extension://abcdefghijklmnop?x=1", "no query"},
		{"chrome-extension://abcdefghijklmnop#top", "no fragment"},
		{"chrome-extension://user@abcdefghijklmnop", "no user info"},
		{"chrome-extension://*", "wildcards are not supported"},
		{"chrome-extension://", "missing host"},
		// An IP host must be in the one form a browser writes it.
		{"http://127.1", "non-canonical IPv4 host, write it as 127.0.0.1, the form a browser sends"},
		{"http://0x7f.0.0.1", "non-canonical IPv4 host, write it as 127.0.0.1, the form a browser sends"},
		{"http://2130706433", "non-canonical IPv4 host, write it as 127.0.0.1, the form a browser sends"},
		{"http://127.0.0.1.", "non-canonical IPv4 host, write it as 127.0.0.1, the form a browser sends"},
		{"http://192.168.001.020", "non-canonical IPv4 host, write it as 192.168.1.16, the form a browser sends (a part with a leading 0 is octal)"},
		{"http://999.1.1.1", "numeric host but no IPv4 address a browser accepts"},
		{"http://1.2.3.4.5", "numeric host but no IPv4 address a browser accepts"},
		{"http://08.1.1.1", "numeric host but no IPv4 address a browser accepts"},
		{"http://ide.example.123", "numeric host but no IPv4 address a browser accepts"},
		// k166: numeric or not goes by the characters, as a browser decides
		// it; a hex number too large to parse still makes the host an
		// address, one the browser refuses.
		{"http://a.0xffffffffffffffffffff", "numeric host but no IPv4 address a browser accepts"},
		{"http://0xffffffffffffffffffff", "numeric host but no IPv4 address a browser accepts"},
		{"http://ide.example.0X1", "numeric host but no IPv4 address a browser accepts"},
		{"http://0x7F000001", "non-canonical IPv4 host, write it as 127.0.0.1, the form a browser sends"},
		{"http://a.0xFF", "numeric host but no IPv4 address a browser accepts"},
		// k166: the URL Standard's forbidden host code points that url.Parse
		// lets through, for a web origin and an extension's alike; a web
		// host, a domain, takes no % either (a %25 url.Parse decoded).
		{"http://a<b.example", `no "<" allowed in a host, a browser refuses the URL`},
		{"https://a>b.example", `no ">" allowed in a host, a browser refuses the URL`},
		{"http://a]b.example", `no "]" allowed in a host, a browser refuses the URL`},
		{"https://a%25b.example", `no "%" allowed in a host, a browser refuses the URL`},
		{"chrome-extension://a<b", `no "<" allowed in a host, a browser refuses the URL`},
		{"chrome-extension://a>b", `no ">" allowed in a host, a browser refuses the URL`},
		{"moz-extension://a]b", `no "]" allowed in a host, a browser refuses the URL`},
		{"http://[0:0:0:0:0:0:0:1]:5173", "non-canonical IPv6 host, write it as [::1], the form a browser sends"},
		{"http://[2001:0db8::1]", "non-canonical IPv6 host, write it as [2001:db8::1], the form a browser sends"},
		{"http://[::ffff:127.0.0.1]", "non-canonical IPv6 host, write it as [::ffff:7f00:1], the form a browser sends"},
		{"chrome-extension://[0:0::1]", "non-canonical IPv6 host, write it as [::1]"},
		{"http://[fe80::1%25eth0]:8080", "no IPv6 zone allowed, a browser sends none"},
	} {
		_, _, err := Load(writeConfigFiles(t, t.TempDir(), allowedOriginsConfig("https://ok.example.com", tc.origin), ""))
		if err == nil {
			t.Errorf("mcp.allowedOrigins entry %q loaded; want a load error", tc.origin)
			continue
		}
		for _, want := range []string{fmt.Sprintf("mcp.allowedOrigins[1] invalid: %q", tc.origin), tc.cause, "want scheme://host[:port] (http or https) or scheme://host for a browser extension"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("mcp.allowedOrigins entry %q: error %q does not mention %q", tc.origin, err, want)
			}
		}
	}
}

// k60: like mcp.address, the entries are checked only where something uses
// them — the MCP server, so only while mcp.enabled. Hermes does not read them.
func TestMCPAllowedOriginsCheckedOnlyWhenUsed(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	off := strings.Replace(allowedOriginsConfig("https://*.example.com"), "enabled: true", "enabled: false", 1)
	if _, _, err := Load(writeConfigFiles(t, t.TempDir(), off, "")); err != nil {
		t.Errorf("MCP off: %v", err)
	}
	hermes := off + "agent: {hermes: {enabled: true}}\n"
	if _, _, err := Load(writeConfigFiles(t, t.TempDir(), hermes, "")); err != nil {
		t.Errorf("MCP off, Hermes on: %v", err)
	}
}

// k60: absent, empty and null all leave the list empty, which keeps the
// server's built-in loopback allowlist (run() calls SetAllowedOrigins only
// for a non-empty list). In unreagent.local.yaml the list replaces the base
// list whole — it does not append — and an empty or null one clears it, back
// to the built-in allowlist.
func TestMCPAllowedOriginsAbsentEmptyAndOverlay(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	base := allowedOriginsConfig("https://ide.example.com", "https://other.example.com")
	for _, tc := range []struct {
		name, base, local string
		want              []string
	}{
		{"absent", "mcp: {enabled: true}\n", "", nil},
		{"empty", "mcp: {enabled: true, allowedOrigins: []}\n", "", nil},
		{"null", "mcp: {enabled: true, allowedOrigins: ~}\n", "", nil},
		{"local replaces the list", base, "mcp: {allowedOrigins: [\"https://local.example.com\"]}\n", []string{"https://local.example.com"}},
		{"local empty clears it", base, "mcp: {allowedOrigins: []}\n", nil},
		{"local null clears it", base, "mcp: {allowedOrigins: ~}\n", nil},
		{"local without the key keeps it", base, "mcp: {strict: true}\n", []string{"https://ide.example.com", "https://other.example.com"}},
	} {
		c, _, err := Load(writeConfigFiles(t, t.TempDir(), tc.base, tc.local))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !slices.Equal(c.MCP.AllowedOrigins, tc.want) {
			t.Errorf("%s: mcp.allowedOrigins = %q; want %q", tc.name, c.MCP.AllowedOrigins, tc.want)
		}
	}
}
