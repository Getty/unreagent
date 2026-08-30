package main

import (
	"os/exec"
	"strings"
	"testing"
)

// Smoke test against a minimal stdio MCP server (node -e) that answers
// initialize correctly — it must be reported as OK.
func TestSmokeTestMCPOk(t *testing.T) {
	requireNode(t)
	logs := captureLogs(func(logger func(string)) {
		script := `process.stdin.on('data', d => { process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:1,result:{serverInfo:{name:"fake",version:"0"}}}) + "\n"); });`
		smokeTestMCP("fake", "node", []string{"-e", script}, nil, t.TempDir(), logger)
	})
	if !strings.Contains(logs, "smoke test OK") {
		t.Fatalf("expected 'smoke test OK', got:\n%s", logs)
	}
}

// A server that dies immediately with an error must produce a WARN containing
// the stderr output — complain instead of giving up.
func TestSmokeTestMCPCrash(t *testing.T) {
	requireNode(t)
	logs := captureLogs(func(logger func(string)) {
		smokeTestMCP("kaputt", "node", []string{"-e", `console.error("Cannot find module 'foo'"); process.exit(1);`}, nil, t.TempDir(), logger)
	})
	if !strings.Contains(logs, "WARN") || !strings.Contains(logs, "Cannot find module") {
		t.Fatalf("expected WARN with the stderr cause, got:\n%s", logs)
	}
}

func requireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}
}

func captureLogs(fn func(logger func(string))) string {
	var sb strings.Builder
	fn(func(s string) { sb.WriteString(s + "\n") })
	return sb.String()
}
