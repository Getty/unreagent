package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// scriptPrefix is the name prefix of the script files scriptAction creates.
const scriptPrefix = "unreagent-"

// scriptExts are the extensions scriptAction assigns (run_python, run_node).
var scriptExts = []string{"py", "mjs"}

// runtimeDir returns the working directory of a runtime: the configured project
// directory, otherwise the agent workdir, otherwise the current directory.
// Storing the script (scriptAction) and sweeping it up (sweepStaleScripts) must
// agree on the very same directory — which is why the decision is made here and
// nowhere else.
func runtimeDir(project, agentWorkdir string) string {
	if project != "" {
		return project
	}
	if agentWorkdir != "" {
		return agentWorkdir
	}
	return "."
}

// scriptAction builds a handler that writes code to a temporary file and runs it
// via command (pre-args + file).
//
// The file deliberately lands IN the working directory of the runtime and not in
// the system temp directory: only there does Node resolve bare specifiers via the
// project's node_modules, and only there is sys.path[0] the project directory for
// Python (so modules sitting next to it become importable). There is deliberately
// no fallback to the system temp directory — it would silently break exactly that
// resolution again.
func scriptAction(sup *supervisor.Supervisor, command string, pre []string, dir, ext, label string) mcp.ToolHandler {
	return func(args map[string]interface{}) mcp.ToolResult {
		code := getString(args, "code", "")
		if code == "" {
			return mcp.ToolResult{Text: "Error: 'code' missing", IsError: true}
		}
		f, err := os.CreateTemp(dir, scriptPrefix+"*."+ext)
		if err != nil {
			return mcp.ToolResult{
				Text: fmt.Sprintf("Error: could not create the script file in the working directory of the %s runtime (%s): %v"+
					"\nThe directory must exist and be writable — check runtimes.%s.project or agent.workdir.",
					label, dir, err, label),
				IsError: true,
			}
		}
		name := f.Name()
		defer os.Remove(name)
		if _, err := f.WriteString(code); err != nil {
			f.Close()
			return errResult(err)
		}
		f.Close()
		callArgs := append(append([]string{}, pre...), name)
		res, err := sup.RunOnce(command, callArgs, dir, nil, label)
		if err != nil {
			return errResult(err)
		}
		return mcp.ToolResult{
			Text:    fmt.Sprintf("exit %d\n\n%s", res.ExitCode, tailLines(res.Output, 300)),
			IsError: res.ExitCode != 0,
		}
	}
}

// sweepStaleScripts removes script files that a hard kill of the launcher left
// behind in the working directories of the runtimes: the defer os.Remove in
// scriptAction no longer runs in that case, and ever since scripts are stored
// there, the file sits in the user's project instead of the system temp
// directory. Called at startup, before the MCP server can write new scripts.
func sweepStaleScripts(cfg *config.Config, agentWorkdir string, logger func(string)) {
	var dirs []string
	add := func(d string) {
		for _, seen := range dirs {
			if seen == d {
				return
			}
		}
		dirs = append(dirs, d)
	}
	if cfg.Runtimes.Python.Enabled {
		add(runtimeDir(cfg.Runtimes.Python.Project, agentWorkdir))
	}
	if cfg.Runtimes.Node.Enabled {
		add(runtimeDir(cfg.Runtimes.Node.Project, agentWorkdir))
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // missing/unreadable: scriptAction reports that loudly enough
		}
		for _, e := range entries {
			if e.IsDir() || !isStaleScriptName(e.Name()) {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if err := os.Remove(path); err != nil {
				logger("WARN could not remove stale script file " + path + ": " + err.Error())
				continue
			}
			logger("Stale script file removed: " + path)
		}
	}
}

// isStaleScriptName matches exactly the names os.CreateTemp generates from
// "unreagent-*.<ext>": the prefix, then decimal digits only (the replacement for
// '*'), then one of the script extensions. The user's own files such as
// "unreagent-helper.py" therefore deliberately do not match.
func isStaleScriptName(name string) bool {
	if !strings.HasPrefix(name, scriptPrefix) {
		return false
	}
	rest := name[len(scriptPrefix):]
	dot := strings.LastIndexByte(rest, '.')
	if dot < 1 { // at least one digit before the extension
		return false
	}
	var known bool
	for _, ext := range scriptExts {
		if rest[dot+1:] == ext {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	for i := 0; i < dot; i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	return true
}

// prepareRuntimes warms up the environments (uv sync / npm install) if they are
// configured and a manifest is present. Runs asynchronously.
func prepareRuntimes(sup *supervisor.Supervisor, cfg *config.Config, agentWorkdir string, logger func(string)) {
	if cfg.Runtimes.Python.Enabled && cfg.Runtimes.Python.PrepareOnStart {
		dir := runtimeDir(cfg.Runtimes.Python.Project, agentWorkdir)
		if fileExists(filepath.Join(dir, "pyproject.toml")) {
			logger("Runtime: preparing Python (uv sync) …")
			go func() { _, _ = sup.RunOnce(cfg.Runtimes.Python.UV, []string{"sync"}, dir, nil, "prepare:python") }()
		}
	}
	if cfg.Runtimes.Node.Enabled && cfg.Runtimes.Node.PrepareOnStart {
		dir := runtimeDir(cfg.Runtimes.Node.Project, agentWorkdir)
		if fileExists(filepath.Join(dir, "package.json")) {
			logger("Runtime: preparing Node (npm install) …")
			go func() { _, _ = sup.RunOnce(cfg.Runtimes.Node.Npm, []string{"install"}, dir, nil, "prepare:node") }()
		}
	}
}
