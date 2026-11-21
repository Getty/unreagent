// Regression tests for karr ticket k78: the approve tool used to pass whatever
// it could (or could not) find in an unknown payload shape straight to
// Permissions.Decide — an empty tool name and an empty input map — so under
// allow_all, or with no deny rule matching "", a request it could not read was
// allowed with updatedInput {}. And getInt ignored numeric strings, so
// logs lines:"5" silently returned 50 lines.
package main

import (
	"encoding/json"
	"strings"
	"testing"

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
	cfg := &config.Config{Permissions: perms}
	srv := mcp.NewServer("tools-test", "0", nil)
	registerTools(srv, sup, cfg, t.TempDir(), func(string) {})
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
		name    string
		args    map[string]interface{}
		missing string // what the deny message must name
	}{
		{"empty payload", map[string]interface{}{}, "tool name"},
		{"input without tool name", map[string]interface{}{"input": map[string]interface{}{"command": "rm -rf /"}}, "tool name"},
		{"tool name not a string", map[string]interface{}{"tool_name": 42, "input": map[string]interface{}{"command": "ls"}}, "tool name"},
		{"tool name without input", map[string]interface{}{"tool_name": "Bash"}, "tool input"},
		{"input not an object", map[string]interface{}{"tool_name": "Bash", "input": "rm -rf /"}, "tool input"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := callApprove(t, srv, tc.args)
			if reply.Behavior != "deny" {
				t.Fatalf("behavior = %q (updatedInput %v), want \"deny\" for an unreadable request", reply.Behavior, reply.UpdatedInput)
			}
			if !strings.Contains(reply.Message, tc.missing) {
				t.Errorf("deny message = %q, want it to name the missing %s", reply.Message, tc.missing)
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
		{"service": "ue", "lines": "3"},
	} {
		if res := callTool(t, srv, "logs", args); res.IsError {
			t.Errorf("logs %v: unexpected error %q", args, res.Text)
		}
	}
}
