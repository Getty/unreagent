package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
)

// k44: the generated MCP config holds the bearer token and the extra servers'
// env values, and was written 0644 — readable by every user of the machine.
// A new file is created 0600.
func TestWriteMCPConfigsCreatesTheFilePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not apply on Windows")
	}
	dir := t.TempDir()
	var log syncLog
	writeMCPConfigs([]config.MCPOutput{{Path: ".mcp.json"}}, testServers(), dir, "", true, log.log, nil)

	path := filepath.Join(dir, ".mcp.json")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v\nlog:\n%s", path, err, log.String())
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("new MCP config has mode %04o, want 0600", got)
	}
	if strings.Contains(log.String(), "tightened") {
		t.Errorf("a new file is reported as tightened:\n%s", log.String())
	}
}

// k44: os.WriteFile keeps an existing file's mode, so a config written 0644
// by an earlier release stayed readable to others after the fix. One open to
// group or others is tightened to 0600, and the log says so; one that is
// private already is left alone, without a line. The new content replaces
// the old whole, also when the old was longer.
func TestWriteMCPConfigsTightensAFileOpenToOthers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not apply on Windows")
	}
	for _, tc := range []struct {
		was      os.FileMode
		tightens bool
	}{
		{0o644, true},
		{0o640, true},
		{0o604, true},
		{0o600, false},
	} {
		t.Run(fmt.Sprintf("%04o", tc.was), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".mcp.json")
			mustWriteFile(t, path, strings.Repeat("stale content of an earlier release\n", 500))
			if err := os.Chmod(path, tc.was); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			var log syncLog
			writeMCPConfigs([]config.MCPOutput{{Path: ".mcp.json"}}, testServers(), dir, "", true, log.log, nil)

			fi, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if got := fi.Mode().Perm(); got != 0o600 {
				t.Errorf("mode after the write is %04o, want 0600", got)
			}
			var doc map[string]interface{}
			if err := json.Unmarshal([]byte(mustReadFile(t, path)), &doc); err != nil {
				t.Errorf("the file is not the new config alone: %v\n%s", err, mustReadFile(t, path))
			}
			line := ""
			for _, l := range strings.Split(log.String(), "\n") {
				if strings.Contains(l, "tightened") {
					line = l
				}
			}
			if !tc.tightens {
				if line != "" {
					t.Errorf("a private file is reported as tightened: %s", line)
				}
				return
			}
			want := fmt.Sprintf("%04o", tc.was)
			if !strings.Contains(line, path) || !strings.Contains(line, want) || !strings.Contains(line, "0600") {
				t.Errorf("no log line naming %s, its old mode %s and 0600:\n%s", path, want, log.String())
			}
		})
	}
}

// testServers is an mcpServers map as buildMCPServers makes it with a token.
func testServers() map[string]interface{} {
	cfg := &config.Config{}
	cfg.MCP.Token = "tok"
	return buildMCPServers(cfg, "http://127.0.0.1:8765/mcp")
}

// k44: the "Agent command:" line logged the --mcp-config JSON, and with it
// the bearer token and every extra server's env values, into unreagent.log.
// redactAgentArgs gives the line its arguments: the --mcp-config value,
// whatever it holds, and the token anywhere else are replaced; everything
// else stays as the agent gets it. k181: the launcher's own config file
// (own), whose path is no secret, is shown as it is; any other --mcp-config
// value, a hand-wired path too, still goes.
func TestRedactAgentArgs(t *testing.T) {
	const (
		tok = "s3cr3t-token"
		own = "/tmp/unreagent-123/mcp-config.json"
	)
	launcherJSON := `{"mcpServers":{"unreagent":{"headers":{"Authorization":"Bearer ` + tok + `"}},"bridge":{"env":{"KEY":"v"}}}}`
	for _, tc := range []struct {
		name  string
		args  []string
		token string
		own   string
		want  []string
	}{
		{"an inline --mcp-config", []string{"--model", "opus", "--mcp-config", launcherJSON, "--strict-mcp-config"}, tok, "",
			[]string{"--model", "opus", "--mcp-config", "<redacted>", "--strict-mcp-config"}},
		{"without a token, the value still goes", []string{"--mcp-config", `{"mcpServers":{"b":{"env":{"KEY":"v"}}}}`}, "", "",
			[]string{"--mcp-config", "<redacted>"}},
		{"the = form", []string{"--mcp-config=" + launcherJSON}, tok, "",
			[]string{"--mcp-config=<redacted>"}},
		{"a hand-wired one before another", []string{"--mcp-config", "--mcp-config", launcherJSON}, tok, "",
			[]string{"--mcp-config", "<redacted>", "<redacted>"}},
		{"the token in another argument", []string{"--settings", `{"env":{"T":"` + tok + `"}}`}, tok, "",
			[]string{"--settings", `{"env":{"T":"<redacted>"}}`}},
		{"no token: nothing else changes", []string{"-p", "task", "--permission-prompt-tool", "mcp__unreagent__approve"}, "", "",
			[]string{"-p", "task", "--permission-prompt-tool", "mcp__unreagent__approve"}},
		{"a trailing --mcp-config has no value", []string{"-p", "--mcp-config"}, tok, "",
			[]string{"-p", "--mcp-config"}},
		{"the launcher's own config file is shown", []string{"-p", "task", "--mcp-config", own, "--strict-mcp-config"}, tok, own,
			[]string{"-p", "task", "--mcp-config", own, "--strict-mcp-config"}},
		{"a hand-wired config next to the launcher's own", []string{"--mcp-config", "/home/u/servers.json", "--mcp-config", own}, tok, own,
			[]string{"--mcp-config", "<redacted>", "--mcp-config", own}},
		{"inline JSON next to the launcher's own", []string{"--mcp-config", launcherJSON, "--mcp-config", own}, tok, own,
			[]string{"--mcp-config", "<redacted>", "--mcp-config", own}},
		{"the own path in the = form still goes", []string{"--mcp-config=" + own}, tok, own,
			[]string{"--mcp-config=<redacted>"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := slices.Clone(tc.args)
			got := redactAgentArgs(in, tc.token, tc.own)
			if !slices.Equal(got, tc.want) {
				t.Errorf("redactAgentArgs(%q)\n got %q\nwant %q", tc.args, got, tc.want)
			}
			if !slices.Equal(in, tc.args) {
				t.Errorf("redactAgentArgs changed its input, which the agent is started with: %q", in)
			}
		})
	}
}

// fakeGit makes PATH hold nothing but a git running body (sh), which first
// records its working directory and its arguments, one per line, in the
// file fakeGit returns. body "" leaves git off PATH.
func fakeGit(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	rec := filepath.Join(dir, "git.call")
	if body != "" {
		script := "#!/bin/sh\npwd > '" + rec + "'\nprintf '%s\\n' \"$@\" >> '" + rec + "'\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
			t.Fatalf("write fake git: %v", err)
		}
	}
	t.Setenv("PATH", dir)
	return rec
}

// k44: a token file inside the project tree that git does not ignore can be
// committed. The warning is quiet unless it is precise (main agent's
// decision): it asks git only for a target inside the project with a token
// set, and warns only when `git check-ignore -q` exits 1. No git, exit 0
// (ignored), exit 128 (no work tree, or an error) and a git that does not
// answer in time are all silence.
func TestUnignoredTokenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a shell script")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not on PATH")
	}
	for _, tc := range []struct {
		name     string
		git      string // fakeGit body
		outside  bool   // the target lies beside the project, in a directory whose name starts with the project's
		relative bool   // the target is given relative to the working directory, as writeMCPConfigs may pass it
		noProj   bool   // no project directory known
		noToken  bool
		want     bool
		asked    bool // git is run
	}{
		{name: "not ignored (exit 1)", git: "exit 1", want: true, asked: true},
		{name: "ignored (exit 0)", git: "exit 0", asked: true},
		{name: "no work tree (exit 128)", git: "echo 'fatal: not a git repository' >&2; exit 128", asked: true},
		{name: "git not on PATH"},
		{name: "no token", git: "exit 1", noToken: true},
		{name: "outside the project", git: "exit 1", outside: true},
		{name: "relative path, not ignored", git: "exit 1", relative: true, want: true, asked: true},
		{name: "no project", git: "exit 1", noProj: true},
		{name: "git does not answer in time", git: "exec " + sleep + " 30", asked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := gitCheckIgnoreTimeout
			gitCheckIgnoreTimeout = 300 * time.Millisecond
			t.Cleanup(func() { gitCheckIgnoreTimeout = old })

			base := t.TempDir()
			proj := filepath.Join(base, "Proj")
			target := filepath.Join(proj, ".vscode", "mcp.json")
			if tc.outside { // a prefix test would take it for inside
				target = filepath.Join(base, "Proj2", "mcp.json")
			}
			mustWriteFile(t, target, "{}\n")
			given := target
			if tc.relative {
				t.Chdir(base)
				given = filepath.Join("Proj", ".vscode", "mcp.json")
			}
			projectDir := proj
			if tc.noProj {
				projectDir = ""
			}
			rec := fakeGit(t, tc.git)

			start := time.Now()
			got := unignoredTokenFile(given, projectDir, !tc.noToken)
			if took := time.Since(start); took > 5*time.Second {
				t.Errorf("took %s; the git call must be bounded", took)
			}
			if got != tc.want {
				t.Errorf("unignoredTokenFile = %v, want %v", got, tc.want)
			}
			call, err := os.ReadFile(rec)
			if !tc.asked {
				if err == nil {
					t.Errorf("git was run, but must not be:\n%s", call)
				}
				return
			}
			if err != nil {
				t.Fatalf("git was not run: %v", err)
			}
			lines := strings.Split(strings.TrimRight(string(call), "\n"), "\n")
			wantArgs := []string{"check-ignore", "-q", target}
			if !slices.Equal(lines[1:], wantArgs) {
				t.Errorf("git was run with %q, want %q", lines[1:], wantArgs)
			}
			if wd, _ := filepath.EvalSymlinks(filepath.Dir(target)); lines[0] != wd && lines[0] != filepath.Dir(target) {
				t.Errorf("git ran in %s, want the target's directory %s", lines[0], filepath.Dir(target))
			}
		})
	}
}

// k44 through writeMCPConfigs: the decision above becomes one WARN line per
// target that names it; a target git ignores gets none. With an alert writer
// (stderr in window mode, where the console is the agent's) the WARN goes
// there too, once, with the "unreagent: " prefix; the line about a tightened
// mode reports a fix and stays in the log. Without one (the log is the
// console) the WARN is written once, to the log, and nothing reaches stderr.
func TestWriteMCPConfigsWarnsAboutAnUnignoredTokenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a shell script")
	}
	for _, tc := range []struct {
		name  string
		git   string
		alert bool
		warn  bool
	}{
		{"not ignored, alert set", "exit 1", true, true},
		{"not ignored, no alert", "exit 1", false, true},
		{"ignored, alert set", "exit 0", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := t.TempDir()
			path := filepath.Join(proj, ".mcp.json")
			mustWriteFile(t, path, "{}\n")
			if err := os.Chmod(path, 0o644); err != nil { // whatever the umask: its tightening is logged
				t.Fatalf("chmod: %v", err)
			}
			fakeGit(t, tc.git)
			var log syncLog
			var buf bytes.Buffer
			var alert io.Writer // an untyped nil when there is none, as run() passes it
			if tc.alert {
				alert = &buf
			}
			// Catches a fallback to stderr when alert is nil.
			stderr, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
			if err != nil {
				t.Fatalf("create stderr file: %v", err)
			}
			defer stderr.Close()
			realStderr := os.Stderr
			os.Stderr = stderr
			writeMCPConfigs([]config.MCPOutput{{Path: ".mcp.json"}}, testServers(), proj, proj, true, log.log, alert)
			os.Stderr = realStderr

			var warns []string
			for _, l := range strings.Split(log.String(), "\n") {
				if strings.HasPrefix(l, "WARN ") && strings.Contains(l, path) {
					warns = append(warns, l)
				}
			}
			if !strings.Contains(log.String(), "MCP config written: "+path) {
				t.Errorf("the file was not written:\n%s", log.String())
			}
			if !strings.Contains(log.String(), "tightened") {
				t.Errorf("the 0644 file's tightening is not logged:\n%s", log.String())
			}
			if got := mustReadFile(t, stderr.Name()); got != "" {
				t.Errorf("written to stderr directly: %q", got)
			}
			if !tc.warn {
				if len(warns) != 0 || buf.Len() != 0 {
					t.Errorf("warned about a file git ignores:\nlog: %q\nalert: %q", warns, buf.String())
				}
				return
			}
			if len(warns) != 1 || !strings.Contains(warns[0], "git does not ignore") {
				t.Fatalf("want one warning that git does not ignore %s in the log, got %q:\n%s", path, warns, log.String())
			}
			want := ""
			if tc.alert {
				want = "unreagent: " + warns[0] + "\n"
			}
			if buf.String() != want {
				t.Errorf("alert got %q, want %q", buf.String(), want)
			}
		})
	}
}

// k44 through run(), in window mode, where the launcher log is
// unreagent.log in the project directory: neither the bearer token nor an
// extra server's env value lands in it, nor on the console. The agent still
// gets both, in the file its --mcp-config names (k181); the log line shows
// that file's path.
func TestRunKeepsTheMCPSecretsOutOfTheLauncherLog(t *testing.T) {
	const (
		token  = "tok-k44-0123456789abcdef"
		secret = "env-k44-secret-value"
	)
	dir := t.TempDir()
	agent := recordingAgent(t, dir)
	yaml := withMCP(launcherYAML("sleep", "  enabled: true\n  command: "+agent+"\n  window: true\n  claudeIntegration: true\n  startDelaySeconds: 1\n"), freeLoopbackAddr(t))
	// The bridge's command does not exist: it is skipped with a WARN, and its
	// env value reaches nothing but the agent's MCP config.
	yaml = strings.Replace(yaml, "mcp:\n  enabled: true\n",
		"mcp:\n  enabled: true\n  token: \""+token+"\"\n  extraServers:\n    bridge:\n      command: unreagent-k44-no-such-bridge\n      env:\n        API_KEY: \""+secret+"\"\n", 1)
	l := startLauncherIn(t, dir, yaml)

	logPath := filepath.Join(dir, "unreagent.log")
	waitFileLine(t, logPath, "MCP server listening on ")
	path := mcpConfigArg(t, recordedArgs(t, agent))
	got := mustReadFile(t, path) // removed once the launcher ends
	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}

	log := mustReadFile(t, logPath)
	if !strings.Contains(log, "Agent command: "+agent+" --mcp-config "+path+"\n") {
		t.Errorf("the agent command line does not show the launcher's config file %s:\n%s", path, log)
	}
	for name, out := range map[string]string{"unreagent.log": log, "stdout": l.stdout.String(), "stderr": l.stderr.String()} {
		for _, s := range []string{token, secret} {
			if strings.Contains(out, s) {
				t.Errorf("%s holds %q:\n%s", name, s, out)
			}
		}
	}
	if !strings.Contains(got, "Bearer "+token) || !strings.Contains(got, secret) {
		t.Errorf("the agent's MCP config lost the token or the env value:\n%s", got)
	}
}

// recordingAgentScript is an agent stand-in that records its arguments, one
// per line, in <script>.args and then keeps running. The file is renamed
// into place, so whoever finds it finds it whole.
const recordingAgentScript = "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$0.args.tmp\" && mv \"$0.args.tmp\" \"$0.args\"\nexec sleep 20\n"

// recordingAgent writes recordingAgentScript into dir and returns its path.
func recordingAgent(t *testing.T, dir string) string {
	t.Helper()
	agent := filepath.Join(dir, "agent.sh")
	if err := os.WriteFile(agent, []byte(recordingAgentScript), 0o755); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	return agent
}

// recordedArgs waits for the arguments recordingAgent's agent was started
// with, and returns them.
func recordedArgs(t *testing.T, agent string) []string {
	t.Helper()
	waitForFile(t, agent+".args", 15*time.Second)
	return strings.Split(strings.TrimSuffix(mustReadFile(t, agent+".args"), "\n"), "\n")
}

// mcpConfigArg returns the value that follows the one --mcp-config in args.
func mcpConfigArg(t *testing.T, args []string) string {
	t.Helper()
	i := slices.Index(args, "--mcp-config")
	if i < 0 || i+1 == len(args) || slices.Contains(args[i+1:], "--mcp-config") {
		t.Fatalf("want one --mcp-config with a value in the agent's arguments: %q", args)
	}
	return args[i+1]
}

// k181: the agent's command line carried the whole MCP config inline, and
// with it the bearer token and the extra servers' env values, which any
// local user reads through /proc/<pid>/cmdline or ps. Now run() writes it to
// mcp-config.json, 0600, in a directory of its own, 0700, in the temp
// directory, passes the agent that file's path and logs the path, and
// removes the directory when it returns. No argument the agent gets holds
// the config, the token or an env value. The file holds the servers, and
// the Authorization header when mcp.token is set.
func TestRunHandsTheAgentItsMCPConfigAsAPrivateFile(t *testing.T) {
	const secret = "env-k181-secret-value"
	for _, tc := range []struct{ name, token string }{
		{"token set", "tok-k181-0123456789abcdef"},
		{"no token", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tmp := filepath.Join(t.TempDir(), "tmp") // the launcher's temp directory
			if err := os.Mkdir(tmp, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", tmp)
			agent := recordingAgent(t, dir)
			addr := freeLoopbackAddr(t)
			yaml := withMCP(launcherYAML("sleep", "  enabled: true\n  command: "+agent+"\n  window: false\n  claudeIntegration: true\n  startDelaySeconds: 1\n"), addr)
			mcp := "mcp:\n  enabled: true\n  extraServers:\n    bridge:\n      command: unreagent-k181-no-such-bridge\n      env:\n        API_KEY: \"" + secret + "\"\n"
			if tc.token != "" {
				mcp += "  token: \"" + tc.token + "\"\n"
			}
			yaml = strings.Replace(yaml, "mcp:\n  enabled: true\n", mcp, 1)
			l := startLauncherIn(t, dir, yaml)

			args := recordedArgs(t, agent)
			path := mcpConfigArg(t, args)
			for _, a := range args {
				for _, s := range []string{"mcpServers", "{", secret, tc.token} {
					if s != "" && strings.Contains(a, s) {
						t.Errorf("the agent's argument %q holds %q", a, s)
					}
				}
			}
			if filepath.Base(path) != "mcp-config.json" || filepath.Dir(filepath.Dir(path)) != tmp ||
				!strings.HasPrefix(filepath.Base(filepath.Dir(path)), "unreagent-") {
				t.Errorf("--mcp-config %s; want %s", path, filepath.Join(tmp, "unreagent-*", "mcp-config.json"))
			}
			for p, want := range map[string]os.FileMode{filepath.Dir(path): 0o700, path: 0o600} {
				fi, err := os.Stat(p)
				if err != nil {
					t.Fatalf("stat %s: %v\n%s", p, err, l.stdout)
				}
				if got := fi.Mode().Perm(); got != want {
					t.Errorf("%s has mode %04o, want %04o", p, got, want)
				}
			}
			var doc struct {
				MCPServers map[string]struct {
					URL     string            `json:"url"`
					Headers map[string]string `json:"headers"`
					Env     map[string]string `json:"env"`
				} `json:"mcpServers"`
			}
			content := mustReadFile(t, path)
			if err := json.Unmarshal([]byte(content), &doc); err != nil {
				t.Fatalf("%s is not an MCP config: %v\n%s", path, err, content)
			}
			own := doc.MCPServers[config.MCPServerName]
			if own.URL != "http://"+addr+"/mcp" || doc.MCPServers["bridge"].Env["API_KEY"] != secret {
				t.Errorf("%s does not hold the launcher's server at http://%s/mcp and the bridge with its env:\n%s", path, addr, content)
			}
			wantAuth := ""
			if tc.token != "" {
				wantAuth = "Bearer " + tc.token
			}
			if got := own.Headers["Authorization"]; got != wantAuth {
				t.Errorf("Authorization header %q, want %q:\n%s", got, wantAuth, content)
			}

			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}
			if !strings.Contains(l.stdout.String(), "Agent command: "+agent+" --mcp-config "+path+"\n") {
				t.Errorf("the agent command line does not show the launcher's config file %s:\n%s", path, l.stdout)
			}
			if left, err := os.ReadDir(tmp); err != nil || len(left) != 0 {
				t.Errorf("after the launcher ended, its temp directory holds %v (%v); want the config's directory removed", left, err)
			}
		})
	}
}

// k181: a config directory the launcher cannot remove when it ends, the
// token file still in it, is a WARN naming the directory, in unreagent.log
// and, in window mode, once on stderr.
func TestRunWarnsWhenTheAgentMCPConfigCannotBeRemoved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes the file whatever the directory's mode")
	}
	dir := t.TempDir()
	tmp := filepath.Join(t.TempDir(), "tmp")
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	agent := recordingAgent(t, dir)
	yaml := withMCP(launcherYAML("sleep", "  enabled: true\n  command: "+agent+"\n  window: true\n  claudeIntegration: true\n  startDelaySeconds: 1\n"), freeLoopbackAddr(t))
	l := startLauncherIn(t, dir, yaml)
	path := mcpConfigArg(t, recordedArgs(t, agent))
	cfgDir := filepath.Dir(path)
	if err := os.Chmod(cfgDir, 0o500); err != nil { // the file in it can no longer be removed
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfgDir, 0o700) }) // for t.TempDir's removal
	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the config file is gone (%v), so the read-only directory did not block its removal", err)
	}
	want := "WARN cannot remove " + cfgDir + ", the agent's MCP config: "
	log := mustReadFile(t, filepath.Join(dir, "unreagent.log"))
	if !strings.Contains(log, want) || !strings.Contains(log, "delete it by hand") {
		t.Errorf("unreagent.log has no %q:\n%s", want, log)
	}
	if got := strings.Count(l.stderr.String(), "unreagent: "+want); got != 1 {
		t.Errorf("stderr has the WARN %d times, want once:\n%s", got, l.stderr.String())
	}
}

// k181: there is no fallback to the inline JSON, which would put the token
// back on the agent's command line. A temp directory the launcher cannot
// create its directory in is a startup error, exit 1, naming the path and
// the cause, and neither the editor nor the agent is started.
func TestRunExitsWhenTheAgentMCPConfigCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "no-such-tmp")
	t.Setenv("TMPDIR", missing)
	script := filepath.Join(dir, "proc.sh") // editor and agent
	if err := os.WriteFile(script, []byte(markerScript), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	yaml := withMCP(launcherYAML(script, "  enabled: true\n  command: "+script+"\n  window: false\n  claudeIntegration: true\n  startDelaySeconds: 1\n"), freeLoopbackAddr(t))
	l := startLauncherIn(t, dir, yaml)
	if code := l.exitCode(t); code != 1 {
		t.Fatalf("exit code %d, want 1:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	stderr := l.stderr.String()
	for _, want := range []string{"Error: ", missing, "no such file or directory"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not say %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(l.stdout.String(), "Agent command: ") {
		t.Errorf("the agent was set up all the same:\n%s", l.stdout)
	}
	if _, err := os.Stat(script + ".ran"); err == nil {
		t.Errorf("the editor or the agent was started:\n%s", l.stdout)
	}
}

// k44 through run(), in window mode: a writeConfig target inside the project
// directory, with mcp.token set, that git does not ignore gets one WARN in
// unreagent.log and the same line once on stderr, the console the user sees
// while the agent holds the window. Without a token, or with the target
// outside the project, there is no WARN at all. This pins run()'s side of the
// wiring: the project directory, the token test and the alert writer it
// hands to writeMCPConfigs.
func TestRunWarnsOnStderrAboutAnUnignoredTokenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a shell script")
	}
	path := os.Getenv("PATH")
	for _, tc := range []struct {
		name    string
		token   bool
		outside bool
		warn    bool
	}{
		{name: "inside the project, token set", token: true, warn: true},
		{name: "no token"},
		{name: "outside the project", token: true, outside: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWriteFile(t, filepath.Join(dir, "Demo.uproject"), "{}\n") // the project directory is dir
			script := filepath.Join(dir, "proc.sh")                       // editor and agent; ignores its arguments
			if err := os.WriteFile(script, []byte(markerScript), 0o755); err != nil {
				t.Fatalf("write script: %v", err)
			}
			target := filepath.Join(dir, ".mcp.json")
			if tc.outside {
				target = filepath.Join(t.TempDir(), ".mcp.json")
			}
			rec := fakeGit(t, "exit 1") // not ignored; first on PATH, the rest after it
			t.Setenv("PATH", filepath.Dir(rec)+string(os.PathListSeparator)+path)

			yaml := withMCP(launcherYAML(script, "  enabled: true\n  command: "+script+"\n  window: true\n  claudeIntegration: true\n  startDelaySeconds: 1\n"), freeLoopbackAddr(t))
			mcp := "mcp:\n  enabled: true\n  writeConfig:\n    - { path: \"" + target + "\" }\n"
			if tc.token {
				mcp += "  token: \"tok-k44-in-tree\"\n"
			}
			yaml = strings.Replace(yaml, "mcp:\n  enabled: true\n", mcp, 1)
			l := startLauncherIn(t, dir, yaml)

			logPath := filepath.Join(dir, "unreagent.log")
			waitFileLine(t, logPath, "MCP server listening on ") // logged after the configs are written
			if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("SIGTERM: %v", err)
			}
			if code := l.exitCode(t); code != 0 {
				t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
			}

			log := mustReadFile(t, logPath)
			if !strings.Contains(log, "MCP config written: "+target) {
				t.Fatalf("the config was not written:\n%s", log)
			}
			const warn = "WARN MCP config "
			warns := 0
			for _, ln := range strings.Split(log, "\n") {
				if strings.Contains(ln, warn) && strings.Contains(ln, "git does not ignore") {
					warns++
				}
			}
			stderr := l.stderr.String()
			alerts := strings.Count(stderr, "unreagent: "+warn+target+" holds the bearer token")
			want := 0
			if tc.warn {
				want = 1
			}
			if warns != want {
				t.Errorf("unreagent.log has the WARN %d times, want %d:\n%s", warns, want, log)
			}
			if alerts != want {
				t.Errorf("stderr has the WARN %d times, want %d:\n%s", alerts, want, stderr)
			}
		})
	}
}

// restartingAgentScript is an agent stand-in that finds its --mcp-config
// file, appends "ok <path>" or "missing <path>" to <script>.starts, one line
// per start, and exits 0 at once.
const restartingAgentScript = `#!/bin/sh
p=
while [ $# -gt 0 ]; do
  [ "$1" = --mcp-config ] && p=$2
  shift
done
if [ -f "$p" ]; then echo "ok $p"; else echo "missing $p"; fi >> "$0.starts"
`

// startLauncherWithStdin is startLauncherIn with a pipe as the launcher's
// stdin, so a test can type console commands.
func startLauncherWithStdin(t *testing.T, dir, yaml string) (*launcherRun, io.WriteCloser) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the configs use sleep and the tests send SIGTERM")
	}
	for _, bin := range []string{"sleep", "sh"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	path := filepath.Join(dir, "unreagent.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	l := &launcherRun{lines: make(chan string, 256), stdout: &syncLog{}, done: make(chan struct{})}
	l.cmd = exec.Command(exe, "-test.run=^TestLauncherRunProcess$")
	l.cmd.Dir = dir
	l.cmd.Env = append(os.Environ(), runConfigEnv+"="+path, runArgsEnv+"=")
	l.cmd.Stderr = &l.stderr
	in, err := l.cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	out, err := l.cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := l.cmd.Start(); err != nil {
		t.Fatalf("start launcher: %v", err)
	}
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			l.stdout.log(sc.Text())
			l.lines <- sc.Text()
		}
		close(l.lines)
		l.err = l.cmd.Wait()
		close(l.done)
	}()
	t.Cleanup(func() {
		select {
		case <-l.done:
		default:
			_ = l.cmd.Process.Kill()
			<-l.done
		}
	})
	return l, in
}

// k186: the config file outlives the agent's exit. run() removes its
// directory only when it returns, so every later start of the agent finds
// the file at the same --mcp-config path. The agent here ends with exit 0 and
// is not restarted by its policy, which is what makes the supervisor call
// OnExit (it does not for an automatic restart); each next start is the
// console's "start agent". An OnExit that removed the directory (the
// reviewer's 'rmonexit' mutant of k181) leaves the second start "missing".
// The directory is gone once run() has returned.
func TestRunRestartedAgentFindsTheSameMCPConfigFile(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(t.TempDir(), "tmp")
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	agent := filepath.Join(dir, "agent.sh")
	if err := os.WriteFile(agent, []byte(restartingAgentScript), 0o755); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	yaml := withMCP(launcherYAML("sleep", "  enabled: true\n  command: "+agent+"\n  window: false\n  claudeIntegration: true\n  startDelaySeconds: 1\n  restart: on-failure\n  onExit: leave\n"), freeLoopbackAddr(t))
	l, stdin := startLauncherWithStdin(t, dir, yaml)
	defer stdin.Close()

	const starts = 3
	var want string
	for n := 1; n <= starts; n++ {
		if n > 1 {
			if _, err := io.WriteString(stdin, "start agent\n"); err != nil {
				t.Fatalf("type 'start agent': %v", err)
			}
		}
		// OnExit logs this as its last step, so a directory removed
		// earlier in OnExit is gone by then.
		l.waitLine(t, "agent.onExit=leave")
		got := strings.Split(strings.TrimSpace(mustReadFile(t, agent+".starts")), "\n")
		if len(got) != n {
			t.Fatalf("after start %d the agent recorded %d starts: %q\n%s", n, len(got), got, l.stdout)
		}
		if n == 1 {
			want = strings.TrimPrefix(got[0], "ok ")
			if want == got[0] || filepath.Base(want) != "mcp-config.json" || filepath.Dir(filepath.Dir(want)) != tmp {
				t.Fatalf("first start: %q; want \"ok %s\"\n%s", got[0], filepath.Join(tmp, "unreagent-*", "mcp-config.json"), l.stdout)
			}
		}
		if got[n-1] != "ok "+want {
			t.Errorf("start %d: %q, want %q (the file the first start found)", n, got[n-1], "ok "+want)
		}
	}

	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := l.exitCode(t); code != 0 {
		t.Fatalf("exit code %d after a signal, want 0:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	if left, err := os.ReadDir(tmp); err != nil || len(left) != 0 {
		t.Errorf("after the launcher ended, its temp directory holds %v (%v); want the config's directory removed", left, err)
	}
}
