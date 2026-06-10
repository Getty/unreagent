package main

import (
	"os/exec"
	"strings"
	"testing"
)

// Smoke-Test gegen einen Mini-stdio-MCP-Server (node -e), der auf initialize
// korrekt antwortet — muss als OK gemeldet werden.
func TestSmokeTestMCPOk(t *testing.T) {
	requireNode(t)
	logs := captureLogs(func(logger func(string)) {
		script := `process.stdin.on('data', d => { process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:1,result:{serverInfo:{name:"fake",version:"0"}}}) + "\n"); });`
		smokeTestMCP("fake", "node", []string{"-e", script}, nil, t.TempDir(), logger)
	})
	if !strings.Contains(logs, "Smoke-Test OK") {
		t.Fatalf("erwartet 'Smoke-Test OK', bekam:\n%s", logs)
	}
}

// Ein Server, der sofort mit Fehler stirbt, muss eine WARN mit dem stderr-
// Inhalt produzieren — beschweren statt aufgeben.
func TestSmokeTestMCPCrash(t *testing.T) {
	requireNode(t)
	logs := captureLogs(func(logger func(string)) {
		smokeTestMCP("kaputt", "node", []string{"-e", `console.error("Cannot find module 'foo'"); process.exit(1);`}, nil, t.TempDir(), logger)
	})
	if !strings.Contains(logs, "WARN") || !strings.Contains(logs, "Cannot find module") {
		t.Fatalf("erwartet WARN mit stderr-Ursache, bekam:\n%s", logs)
	}
}

func requireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node nicht im PATH")
	}
}

func captureLogs(fn func(logger func(string))) string {
	var sb strings.Builder
	fn(func(s string) { sb.WriteString(s + "\n") })
	return sb.String()
}
