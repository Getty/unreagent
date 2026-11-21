// Regression tests for karr ticket k78: the approve tool used to pass whatever
// it could (or could not) find in an unknown payload shape straight to
// Permissions.Decide — an empty tool name and an empty input map — so under
// allow_all, or with no deny rule matching "", a request it could not read was
// allowed with updatedInput {}. And getInt ignored numeric strings, so
// logs lines:"5" silently returned 50 lines.
package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// newToolsServer registers the real tool set (registerTools) on a fresh
// server, backed by a supervisor that is never started: the handlers under
// test only read its registry and its (empty) log buffers.
func newToolsServer(t *testing.T, perms config.Permissions) *mcp.Server {
	t.Helper()
	sup := supervisor.New(nil)
	sup.AddService(supervisor.ServiceSpec{Name: "ue", Command: "unused"})
	return toolsServerOn(t, sup, perms, func(string) {})
}

// toolsServerOn registers the real tool set on a fresh server backed by sup,
// with logger as the launcher log sink registerTools writes to.
func toolsServerOn(t *testing.T, sup *supervisor.Supervisor, perms config.Permissions, logger func(string)) *mcp.Server {
	t.Helper()
	cfg := &config.Config{Permissions: perms}
	srv := mcp.NewServer("tools-test", "0", nil)
	registerTools(srv, sup, cfg, t.TempDir(), logger)
	return srv
}

type approveReply struct {
	Behavior     string                 `json:"behavior"`
	Message      string                 `json:"message"`
	UpdatedInput map[string]interface{} `json:"updatedInput"`
}

func callApprove(t *testing.T, srv *mcp.Server, args map[string]interface{}) approveReply {
	t.Helper()
	res := callTool(t, srv, "approve", args)
	if res.IsError {
		t.Fatalf("approve returned a tool error: %q", res.Text)
	}
	var reply approveReply
	if err := json.Unmarshal([]byte(res.Text), &reply); err != nil {
		t.Fatalf("approve reply is not JSON: %v\ntext: %s", err, res.Text)
	}
	return reply
}

// TestApproveFailsClosedOnUnreadablePayload runs under allow_all, the most
// permissive policy, so nothing but the payload check can produce the deny.
// Each case is a request the handler cannot attribute to a tool and its
// input; every one of them used to come back "allow".
func TestApproveFailsClosedOnUnreadablePayload(t *testing.T) {
	srv := newToolsServer(t, config.Permissions{Enabled: true, Mode: config.ModeAllowAll})

	cases := []struct {
		name string
		args map[string]interface{}
		want string // what the deny message must say
	}{
		{"empty payload", map[string]interface{}{}, "no tool name"},
		{"input without tool name", map[string]interface{}{"input": map[string]interface{}{"command": "rm -rf /"}}, "no tool name"},
		{"tool name null", map[string]interface{}{"tool_name": nil, "input": map[string]interface{}{"command": "ls"}}, "no tool name"},
		// Present but not a string: "no tool name" would send whoever reads
		// the message looking for a key that is right there.
		{"tool name not a string", map[string]interface{}{"tool_name": 42, "input": map[string]interface{}{"command": "ls"}}, "tool name is not a string"},
		{"tool name without input", map[string]interface{}{"tool_name": "Bash"}, "tool input"},
		{"input not an object", map[string]interface{}{"tool_name": "Bash", "input": "rm -rf /"}, "tool input"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := callApprove(t, srv, tc.args)
			if reply.Behavior != "deny" {
				t.Fatalf("behavior = %q (updatedInput %v), want \"deny\" for an unreadable request", reply.Behavior, reply.UpdatedInput)
			}
			if !strings.Contains(reply.Message, tc.want) {
				t.Errorf("deny message = %q, want it to say %q", reply.Message, tc.want)
			}
			if reply.UpdatedInput != nil {
				t.Errorf("a deny must not carry updatedInput, got %v", reply.UpdatedInput)
			}
		})
	}
}

// TestApproveAllowsWellFormedPayload is the control for the test above: the
// shape Claude Code sends (tool_name + input) still reaches the policy, and an
// allow hands the input back unchanged. Without it, a handler that denied
// everything would pass the fail-closed test.
func TestApproveAllowsWellFormedPayload(t *testing.T) {
	srv := newToolsServer(t, config.Permissions{Enabled: true, Mode: config.ModeAllowAll})

	for _, args := range []map[string]interface{}{
		{"tool_name": "Bash", "input": map[string]interface{}{"command": "ls"}, "tool_use_id": "x"},
		{"toolName": "Bash", "arguments": map[string]interface{}{"command": "ls"}},
		{"tool_name": "mcp__unreagent__status", "input": map[string]interface{}{}},
	} {
		reply := callApprove(t, srv, args)
		if reply.Behavior != "allow" {
			t.Errorf("%v: behavior = %q (%s), want \"allow\"", args, reply.Behavior, reply.Message)
			continue
		}
		want := firstMap(args, "input", "arguments")
		if len(reply.UpdatedInput) != len(want) || (len(want) > 0 && reply.UpdatedInput["command"] != want["command"]) {
			t.Errorf("%v: updatedInput = %v, want the input unchanged (%v)", args, reply.UpdatedInput, want)
		}
	}
}

// TestApproveLogNamesTheTool checks the launcher log line approve writes per
// decision. A request without a tool name used to log "[approve]  → deny",
// with a blank where the tool belongs; it must show a placeholder instead,
// while a readable request still logs the tool's real name.
func TestApproveLogNamesTheTool(t *testing.T) {
	var logged []string
	sup := supervisor.New(nil)
	srv := toolsServerOn(t, sup, config.Permissions{Enabled: true, Mode: config.ModeAllowAll}, func(line string) { logged = append(logged, line) })

	for _, tc := range []struct {
		args map[string]interface{}
		tool string // "" = any non-blank placeholder
	}{
		{map[string]interface{}{"input": map[string]interface{}{"command": "ls"}}, ""},
		{map[string]interface{}{"tool_name": 42, "input": map[string]interface{}{"command": "ls"}}, ""},
		{map[string]interface{}{"tool_name": "Bash", "input": map[string]interface{}{"command": "ls"}}, "Bash"},
	} {
		logged = nil
		callApprove(t, srv, tc.args)
		if len(logged) != 1 {
			t.Fatalf("%v: logged %q, want exactly one [approve] line", tc.args, logged)
		}
		head, _, ok := strings.Cut(logged[0], " → ")
		if !ok || !strings.HasPrefix(head, "[approve] ") {
			t.Fatalf("%v: log line %q is not \"[approve] <tool> → <decision> (...)\"", tc.args, logged[0])
		}
		got := strings.TrimPrefix(head, "[approve] ")
		switch {
		case strings.TrimSpace(got) == "":
			t.Errorf("%v: log line %q leaves the tool name blank, want a placeholder", tc.args, logged[0])
		case tc.tool != "" && got != tc.tool:
			t.Errorf("%v: log line %q names tool %q, want %q", tc.args, logged[0], got, tc.tool)
		}
	}
}

// TestGetIntAcceptsNumericString: some MCP clients send every argument as a
// string. "5" must read as 5, not as the default.
func TestGetIntAcceptsNumericString(t *testing.T) {
	for _, in := range []interface{}{"5", " 5 ", float64(5), 5} {
		got, err := getInt(map[string]interface{}{"lines": in}, "lines", 50)
		if err != nil || got != 5 {
			t.Errorf("getInt(lines=%#v) = %d, %v, want 5, nil", in, got, err)
		}
	}
	if got, err := getInt(map[string]interface{}{}, "lines", 50); err != nil || got != 50 {
		t.Errorf("getInt(absent) = %d, %v, want the default 50, nil", got, err)
	}
}

// TestLogsRejectsNonNumericLines drives the logs tool with a value that is
// neither a number nor a numeric string. It must say so instead of quietly
// answering with the default line count.
func TestLogsRejectsNonNumericLines(t *testing.T) {
	srv := newToolsServer(t, config.Permissions{})

	for _, lines := range []interface{}{"abc", "5 lines", true, map[string]interface{}{}} {
		res := callTool(t, srv, "logs", map[string]interface{}{"service": "ue", "lines": lines})
		if !res.IsError {
			t.Errorf("logs lines=%#v: IsError = false (text %q), want an error naming 'lines'", lines, res.Text)
			continue
		}
		if !strings.Contains(res.Text, "lines") {
			t.Errorf("logs lines=%#v: error = %q, want it to name 'lines'", lines, res.Text)
		}
	}

	// The lenient paths stay lenient: absent, JSON null and "" read as the
	// default, as an empty string does for getString.
	for _, args := range []map[string]interface{}{
		{"service": "ue"},
		{"service": "ue", "lines": nil},
		{"service": "ue", "lines": ""},
	} {
		if res := callTool(t, srv, "logs", args); res.IsError {
			t.Errorf("logs %v: unexpected error %q", args, res.Text)
		}
	}
}

// TestLogsLinesLimitsOutput fills the ue log buffer through the supervisor's
// own path — a real service that prints six lines and exits — and checks that
// lines limits what logs returns, given as a numeric string as well as a number.
// Against an empty buffer every count returns nothing, so only a filled one
// can tell "3" from the default 50.
func TestLogsLinesLimitsOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not in PATH — skipping")
	}
	exited := make(chan struct{})
	sup := supervisor.New(nil)
	sup.AddService(supervisor.ServiceSpec{
		Name:      "ue",
		Command:   "sh",
		Args:      []string{"-c", `printf 'line-%s\n' 1 2 3 4 5 6`},
		Autostart: true,
		Restart:   "never",
		// OnExit fires only after the exit path has drained the output into
		// the log buffer, so the buffer is complete once it has fired.
		OnExit: func(supervisor.Exit) { close(exited) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the ue service did not exit")
	}

	srv := toolsServerOn(t, sup, config.Permissions{}, func(string) {})
	const want = "line-4\nline-5\nline-6"
	for _, lines := range []interface{}{"3", " 3 ", 3} {
		res := callTool(t, srv, "logs", map[string]interface{}{"service": "ue", "lines": lines})
		if res.IsError || res.Text != want {
			t.Errorf("logs lines=%#v = %q (IsError %v), want the last three lines %q", lines, res.Text, res.IsError, want)
		}
	}
}

// startedSupervisor runs sup until the test ends.
func startedSupervisor(t *testing.T, sup *supervisor.Supervisor) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })
}

// k72: ue_start and ue_restart on an editor that cannot start used to answer
// with the bare snapshot — running false, isError false — and the agent took
// that for a success. They must fail and name the start error; ue_stop on that
// editor stays an ordinary result.
func TestUEStartAndRestartFailWhenTheEditorDoesNotStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-editor")
	sup := supervisor.New(nil)
	sup.AddService(supervisor.ServiceSpec{Name: "ue", Command: missing, Restart: "never"})
	startedSupervisor(t, sup)
	srv := toolsServerOn(t, sup, config.Permissions{}, func(string) {})

	for _, tool := range []string{"ue_start", "ue_restart"} {
		res := callTool(t, srv, tool, map[string]interface{}{})
		if !res.IsError {
			t.Fatalf("%s on a missing editor: IsError = false (text %q), want an error", tool, res.Text)
		}
		// The start error leads the text, ahead of the snapshot — which carries
		// lastError as well, so finding it anywhere in the text proves nothing.
		lastErr := statusOf(t, sup, "ue").LastError
		if !strings.Contains(lastErr, missing) {
			t.Fatalf("%s on a missing editor: lastError = %q, want the start error naming %s", tool, lastErr, missing)
		}
		if want := `Error: service "ue" did not start: ` + lastErr + "\n"; !strings.HasPrefix(res.Text, want) {
			t.Fatalf("%s on a missing editor: text %q does not start with %q", tool, res.Text, want)
		}
	}
	if res := callTool(t, srv, "ue_stop", map[string]interface{}{}); res.IsError {
		t.Fatalf("ue_stop: IsError = true (text %q), want an ordinary result", res.Text)
	}
}

// The control for the test above: on an editor that runs, ue_start (a no-op)
// and ue_restart are ordinary results. Without it, a handler that always
// failed would pass the test above.
func TestUEStartAndRestartSucceedOnARunningEditor(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not on PATH")
	}
	sup := supervisor.New(nil)
	sup.AddService(supervisor.ServiceSpec{Name: "ue", Command: "sleep", Args: []string{"20"}, Autostart: true, Restart: "never"})
	startedSupervisor(t, sup)
	waitRunning(t, sup, "ue")
	srv := toolsServerOn(t, sup, config.Permissions{}, func(string) {})

	for _, tool := range []string{"ue_start", "ue_restart"} {
		res := callTool(t, srv, tool, map[string]interface{}{})
		if res.IsError {
			t.Fatalf("%s on a running editor: IsError = true (text %q), want an ordinary result", tool, res.Text)
		}
		var st supervisor.ServiceStatus
		if err := json.Unmarshal([]byte(res.Text), &st); err != nil || !st.Running {
			t.Fatalf("%s on a running editor: text %q is not a running snapshot (%v)", tool, res.Text, err)
		}
	}
}
