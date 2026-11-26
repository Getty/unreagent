package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
)

// k265: with unreal.mcp on, the editor gets -ModelContextProtocolStartServer
// and -ModelContextProtocolPort=<unreal.mcp.port>, each exactly once: a
// start switch unreal.args already carry, in any case and with "-" or "/",
// the deprecated -StartModelContextProtocolServer included, gets no second
// one, and neither does the exact port flag for unreal.mcp.port (config.Load
// has refused any other mention). "-ModelContextProtocolStartServer=1" is not the switch to
// the engine (FParse::Param), so the launcher's still goes on.
func TestEditorArgsAddsTheUnrealMCPFlagsOnce(t *testing.T) {
	off := false
	start, port := "-ModelContextProtocolStartServer", "-ModelContextProtocolPort=8123"
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"absent", []string{"-stdout"}, []string{"-stdout", start, port}},
		{"start switch given", []string{start}, []string{start, port}},
		{"start switch, other case", []string{"-modelcontextprotocolstartserver"}, []string{"-modelcontextprotocolstartserver", port}},
		{"start switch, slash form", []string{"/ModelContextProtocolStartServer"}, []string{"/ModelContextProtocolStartServer", port}},
		{"deprecated start switch", []string{"-StartModelContextProtocolServer"}, []string{"-StartModelContextProtocolServer", port}},
		{"deprecated, other case", []string{"/STARTMODELCONTEXTPROTOCOLSERVER"}, []string{"/STARTMODELCONTEXTPROTOCOLSERVER", port}},
		{"start switch with a value", []string{start + "=1"}, []string{start + "=1", start, port}},
		{"port given", []string{port}, []string{port, start}},
		{"port given, other case", []string{"/modelcontextprotocolport=8123"}, []string{"/modelcontextprotocolport=8123", start}},
		{"both given", []string{"-StartModelContextProtocolServer", "-MODELCONTEXTPROTOCOLPORT=08123"}, []string{"-StartModelContextProtocolServer", "-MODELCONTEXTPROTOCOLPORT=08123"}},
		{"port without a value", []string{"-ModelContextProtocolPort"}, []string{"-ModelContextProtocolPort", start, port}},
		// Only the exact flag for unreal.mcp.port (config.UnrealPortArg)
		// counts; config.Load refuses every other element that mentions it.
		{"port without a prefix", []string{"ModelContextProtocolPort=8123"}, []string{"ModelContextProtocolPort=8123", start, port}},
		{"port after an underscore", []string{"-X_ModelContextProtocolPort=8123"}, []string{"-X_ModelContextProtocolPort=8123", start, port}},
		{"port with more after it", []string{"-ModelContextProtocolPort=8123 x"}, []string{"-ModelContextProtocolPort=8123 x", start, port}},
		{"another port", []string{"-ModelContextProtocolPort=9000"}, []string{"-ModelContextProtocolPort=9000", start, port}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := config.UnrealConfig{Args: tc.args, Unattended: &off, MCP: config.UnrealMCPConfig{Enabled: true, Port: 8123}}
			var logged []string
			got := editorArgs(u, func(s string) { logged = append(logged, s) })
			if !slices.Equal(got, tc.want) {
				t.Errorf("editorArgs(%q) = %q, want %q", tc.args, got, tc.want)
			}
			if len(logged) != 1 || !strings.Contains(logged[0], "http://127.0.0.1:8123/mcp") {
				t.Errorf("logged %q; want one line naming http://127.0.0.1:8123/mcp", logged)
			}
		})
	}
}

// k265: the switch off leaves the editor's arguments exactly as they were
// before it existed: unreal.args, flags of Unreal MCP among them, untouched,
// and nothing added but -unattended.
func TestEditorArgsUnchangedWithUnrealMCPOff(t *testing.T) {
	on := true
	for _, args := range [][]string{
		{"-stdout", "-FullStdOutLogOutput"},
		{"-ModelContextProtocolStartServer", "-ModelContextProtocolPort=9000"},
	} {
		u := config.UnrealConfig{Project: "/p/Game.uproject", Args: args, Unattended: &on, MCP: config.UnrealMCPConfig{Port: 8000}}
		var logged []string
		got := editorArgs(u, func(s string) { logged = append(logged, s) })
		want := append(append([]string{"/p/Game.uproject"}, args...), "-unattended")
		if !slices.Equal(got, want) {
			t.Errorf("editorArgs(%q) = %q, want %q", args, got, want)
		}
		if len(logged) != 1 || strings.Contains(logged[0], "MCP") {
			t.Errorf("logged %q; want the -unattended line only", logged)
		}
	}
}

// k265: with unreal.mcp on, the servers the agent's --mcp-config and every
// mcp.writeConfig target get hold the http entry unreal-mcp at
// unreal.mcp's URL, next to the launcher's own and the extra servers.
func TestBuildMCPServersAddsUnrealMCP(t *testing.T) {
	cfg := &config.Config{}
	cfg.Unreal.MCP = config.UnrealMCPConfig{Enabled: true, Port: 8123}
	cfg.MCP.ExtraServers = map[string]map[string]interface{}{"bridge": {"command": "node"}}
	servers := buildMCPServers(cfg, "http://127.0.0.1:8765/mcp")
	got, _ := json.Marshal(servers[config.UnrealMCPServerName])
	if want := `{"type":"http","url":"http://127.0.0.1:8123/mcp"}`; string(got) != want {
		t.Errorf("unreal-mcp entry %s, want %s", got, want)
	}
	if _, ok := servers["bridge"]; !ok || servers[config.MCPServerName] == nil || len(servers) != 3 {
		t.Errorf("servers %v; want unreagent, bridge and unreal-mcp", servers)
	}
}

// k265: the switch off, the servers are byte for byte what they were before
// it existed: the launcher's own entry (with the token's header) and the
// extra servers as written, a hand-written unreal-mcp entry among them.
func TestBuildMCPServersUnchangedWithUnrealMCPOff(t *testing.T) {
	cfg := &config.Config{}
	cfg.Unreal.MCP = config.UnrealMCPConfig{Port: 8000}
	cfg.MCP.Token = "tok"
	cfg.MCP.ExtraServers = map[string]map[string]interface{}{
		"bridge":     map[string]interface{}{"command": "node", "args": []interface{}{"index.js"}},
		"unreal-mcp": map[string]interface{}{"type": "http", "url": "http://127.0.0.1:9000/mcp"},
	}
	got, _ := json.Marshal(buildMCPServers(cfg, "http://127.0.0.1:8765/mcp"))
	want := `{"bridge":{"args":["index.js"],"command":"node"},"unreagent":{"headers":{"Authorization":"Bearer tok"},"type":"http","url":"http://127.0.0.1:8765/mcp"},"unreal-mcp":{"type":"http","url":"http://127.0.0.1:9000/mcp"}}`
	if string(got) != want {
		t.Errorf("servers\n%s\nwant\n%s", got, want)
	}
}

// k265 through run(): with unreal.mcp on, the editor is started with the two
// flags, and the agent's --mcp-config file and the mcp.writeConfig target
// both hold the unreal-mcp entry. (Since k266 the agent starts only once
// the server answers a ping, so a fake of it answers on the port.)
func TestRunHandsUnrealMCPToEditorAndAgent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", t.TempDir())
	agent := recordingAgent(t, dir)
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte(recordingAgentScript), 0o755); err != nil {
		t.Fatal(err)
	}
	server := newUnrealMCPServer(t, 0, pingResult)
	close(server.ready)
	port := strconv.Itoa(mustPort(t, server.srv.Listener.Addr().String()))
	entryURL := "http://127.0.0.1:" + port + "/mcp"
	addr := freeLoopbackAddr(t)
	yaml := withMCP(launcherYAML(editor, "  enabled: true\n  command: "+agent+"\n  window: false\n  claudeIntegration: true\n  startDelaySeconds: 1\n"), addr)
	yaml = strings.Replace(yaml, "  unattended: false\n", "  unattended: false\n  mcp: {enabled: true, port: "+port+"}\n", 1)
	yaml = strings.Replace(yaml, "mcp:\n  enabled: true\n", "mcp:\n  enabled: true\n  writeConfig: [{path: out.json}]\n", 1)
	l := startLauncherIn(t, dir, yaml)

	if got, want := recordedArgs(t, editor), []string{"20", "-ModelContextProtocolStartServer", "-ModelContextProtocolPort=" + port}; !slices.Equal(got, want) {
		t.Errorf("editor args %q, want %q", got, want)
	}
	agentConfig := mustReadFile(t, mcpConfigArg(t, recordedArgs(t, agent)))
	waitForFile(t, filepath.Join(dir, "out.json"), 15*time.Second)
	for name, content := range map[string]string{"--mcp-config": agentConfig, "writeConfig": mustReadFile(t, filepath.Join(dir, "out.json"))} {
		var doc struct {
			MCPServers map[string]map[string]interface{} `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, content)
		}
		if e := doc.MCPServers[config.UnrealMCPServerName]; e["type"] != "http" || e["url"] != entryURL {
			t.Errorf("%s holds no unreal-mcp http entry at %s:\n%s", name, entryURL, content)
		}
	}
	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d, want 0:\n%s", code, l.stdout)
	}
}

// k266 through run(): with unreal.mcp on, the agent's first start waits
// until the editor's Unreal MCP server answers a ping, past agent.startDelay
// and past an open port; a shutdown during the wait starts no agent; an
// editor the supervisor does not restart ends the wait at once, with one
// warning, and the agent starts. With the switch off, unreal.manualStart or
// no Claude integration (the agent gets no unreal-mcp entry), the agent
// starts after its delay as before, whether the server answers or not; with
// the switch on, the server is watched even without extraServers.
func TestRunHoldsTheAgentUntilUnrealMCPAnswers(t *testing.T) {
	const (
		waits  = "Agent: its first start waits until the Unreal MCP server at "
		sleepy = "#!/bin/sh\nexec sleep 20\n"
	)
	type mode int
	const (
		notHeld mode = iota
		answered
		signaled
		gaveUp
	)
	for _, tc := range []struct {
		name    string
		unreal  string // the unreal: lines added; "%s" is the server's port
		claude  bool   // agent.claudeIntegration
		editor  string // the editor script; it ignores the launcher's flags
		answers bool   // the server answers pings from the start
		mode    mode
	}{
		{"held until the ping is answered", "  mcp: {enabled: true, port: %s}\n", true, sleepy, false, answered},
		{"shutdown during the wait", "  mcp: {enabled: true, port: %s}\n", true, sleepy, false, signaled},
		{"the editor gives up during the wait", "  restart: never\n  mcp: {enabled: true, port: %s}\n", true, "#!/bin/sh\nexit 1\n", false, gaveUp},
		{"manual start", "  manualStart: true\n  mcp: {enabled: true, port: %s}\n", true, sleepy, true, notHeld},
		{"no claude integration", "  mcp: {enabled: true, port: %s}\n", false, sleepy, false, notHeld},
		{"switch off", "  mcp: {enabled: false, port: %s}\n", true, sleepy, true, notHeld},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", t.TempDir())
			agent := recordingAgent(t, dir)
			editor := filepath.Join(dir, "editor.sh")
			if err := os.WriteFile(editor, []byte(tc.editor), 0o755); err != nil {
				t.Fatal(err)
			}
			// The port is open from the start; the server answers no ping
			// until ready is closed.
			server := newUnrealMCPServer(t, http.StatusServiceUnavailable, pingResult)
			if tc.answers {
				close(server.ready)
			}
			port := strconv.Itoa(mustPort(t, server.srv.Listener.Addr().String()))
			url := "http://127.0.0.1:" + port + "/mcp"
			yaml := withMCP(launcherYAML(editor, fmt.Sprintf("  enabled: true\n  command: %s\n  window: false\n  claudeIntegration: %v\n  startDelaySeconds: 1\n", agent, tc.claude)), freeLoopbackAddr(t))
			yaml = strings.Replace(yaml, "  unattended: false\n", "  unattended: false\n"+fmt.Sprintf(tc.unreal, port), 1)
			l := startLauncherIn(t, dir, yaml)

			switch tc.mode {
			case notHeld:
				recordedArgs(t, agent)
				if tc.answers && strings.Contains(tc.unreal, "enabled: true") {
					// unreal.mcp on and no extraServers: the watch runs.
					l.waitLine(t, "MCP server 'unreal-mcp': Unreal MCP server "+url+" is ready — it answers ping.")
				}
			case gaveUp:
				l.waitLine(t, waits+url+" answers a ping")
				l.waitLine(t, "WARN MCP server 'unreal-mcp': Unreal MCP server "+url+" will not answer: the editor failed and is not restarted — the agent starts now without it")
				recordedArgs(t, agent)
			default:
				l.waitLine(t, waits+url+" answers a ping")
				// Past the agent's start delay of 1 s, with the port open.
				time.Sleep(2500 * time.Millisecond)
				if _, err := os.Stat(agent + ".args"); err == nil {
					t.Fatalf("the agent started before the server answered a ping:\n%s", l.stdout)
				}
				if len(server.seen()) == 0 {
					t.Fatalf("no ping reached the server during the wait:\n%s", l.stdout)
				}
				if tc.mode == answered {
					close(server.ready)
					l.waitLine(t, "MCP server 'unreal-mcp': Unreal MCP server "+url+" is ready — it answers ping.")
					recordedArgs(t, agent)
				}
			}
			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d, want 0:\n%s", code, l.stdout)
			}
			out := l.stdout.String()
			if got, want := strings.Count(out, waits), map[bool]int{true: 1}[tc.mode != notHeld]; got != want {
				t.Errorf("the log has the wait line %d times, want %d:\n%s", got, want, out)
			}
			if got, want := strings.Count(out, "WARN MCP server 'unreal-mcp'"), map[bool]int{true: 1}[tc.mode == gaveUp]; got != want {
				t.Errorf("the log has %d unreal-mcp warnings, want %d:\n%s", got, want, out)
			}
			if tc.mode == signaled {
				if _, err := os.Stat(agent + ".args"); err == nil {
					t.Errorf("the agent was started after the shutdown:\n%s", out)
				}
			}
			if strings.Contains(tc.unreal, "enabled: false") && len(server.seen()) != 0 {
				t.Errorf("the switch is off, but the server got %d request(s)", len(server.seen()))
			}
		})
	}
}

// k266 in window mode: the log is a file and the console the agent's, so the
// line that says the agent waits reaches the console too.
func TestRunShowsTheAgentHoldOnTheConsole(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", t.TempDir())
	agent := recordingAgent(t, dir)
	server := newUnrealMCPServer(t, http.StatusServiceUnavailable, pingResult)
	port := strconv.Itoa(mustPort(t, server.srv.Listener.Addr().String()))
	yaml := withMCP(launcherYAML("sleep", "  enabled: true\n  command: "+agent+"\n  window: true\n  claudeIntegration: true\n  startDelaySeconds: 1\n"), freeLoopbackAddr(t))
	yaml = strings.Replace(yaml, "  unattended: false\n", "  unattended: false\n  mcp: {enabled: true, port: "+port+"}\n", 1)
	l := startLauncherIn(t, dir, yaml)
	l.waitStderr(t, "unreagent: Agent: its first start waits until the Unreal MCP server at http://127.0.0.1:"+port+"/mcp answers a ping")
	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
}
