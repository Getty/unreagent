package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// k79: prepareRuntimes threw away what uv sync and npm install returned: a
// missing uv showed only the supervisor's "starting" line, a failing sync
// only "done (exit 1)", its output gone. A failure is a WARN now that names
// the error and the last 20 lines of the output; one that works adds nothing
// to the log. The fake tool stands in for uv and npm alike.
func TestPrepareRuntimesWarnsOnFailure(t *testing.T) {
	var lines []string
	for i := 1; i <= 25; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	failing := "i=1\nwhile [ $i -le 25 ]; do echo \"line $i\"; i=$((i+1)); done\nexit 1\n"
	for _, c := range []struct {
		name string
		tool string // the fake tool's body; "" = no tool at that path
		// cause is what the WARN names after the directory, "" for no WARN;
		// prefix means the cause only starts with it.
		cause  string
		prefix bool
	}{
		{"a non-zero exit", failing, "exit 1\n" + strings.Join(lines[5:], "\n"), false},
		{"the tool missing", "", "start failed: ", true},
		{"a preparation that works", "echo ok\nexit 0\n", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newScripts(t)
			dir := t.TempDir()
			tool := filepath.Join(dir, "bin", "tool")
			if c.tool != "" {
				s.write(tool, c.tool)
			}
			for _, f := range []string{"pyproject.toml", "package.json"} {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.Config{}
			cfg.Runtimes.Python = config.PythonRuntime{Enabled: true, UV: tool, Project: dir, PrepareOnStart: true}
			cfg.Runtimes.Node = config.NodeRuntime{Enabled: true, Npm: tool, Project: dir, PrepareOnStart: true}

			var log syncLog
			prepareRuntimes(supervisor.New(nil), cfg, "", log.log)

			var warns []string
			for _, l := range log.lines {
				if strings.HasPrefix(l, "WARN ") {
					warns = append(warns, l)
				}
			}
			if c.cause == "" {
				if len(warns) != 0 {
					t.Fatalf("a preparation that works warned:\n%s", strings.Join(warns, "\n"))
				}
				return
			}
			for _, rt := range []struct{ lang, verb string }{{"Python", "sync"}, {"Node", "install"}} {
				head := fmt.Sprintf("WARN Runtime: preparing %s failed — %s %s in %s: ", rt.lang, tool, rt.verb, dir)
				var got string
				for _, w := range warns {
					if strings.HasPrefix(w, head) {
						got = strings.TrimPrefix(w, head)
					}
				}
				switch {
				case got == "":
					t.Errorf("no WARN starting %q; log:\n%s", head, log.String())
				case c.prefix && !strings.HasPrefix(got, c.cause):
					t.Errorf("%s: the WARN names %q, want it to start with %q", rt.lang, got, c.cause)
				case !c.prefix && got != c.cause:
					t.Errorf("%s: the WARN names\n%s\n\nwant\n%s", rt.lang, got, c.cause)
				}
				if c.tool == "" && !strings.Contains(got, tool) {
					t.Errorf("%s: the WARN for a missing tool does not name its path %s: %q", rt.lang, tool, got)
				}
			}
			if len(warns) != 2 {
				t.Errorf("%d WARN lines, want one per runtime:\n%s", len(warns), strings.Join(warns, "\n"))
			}
		})
	}
}
