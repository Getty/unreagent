package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The built-in defaults: what the launcher runs on when it is started next to
// a .uproject with no config file (see load), and the document `unreagent
// init` writes out. Both come from DefaultYAML, and load decodes that document
// like a file, so a start without a config and a start with the written one
// cannot drift apart.

// ToolkitServerName is the mcp.extraServers key of the UE LLM Toolkit bridge
// in the built-in defaults.
const ToolkitServerName = "ue-llm-toolkit"

// bridgeEntry is the bridge's entry point inside a plugin directory.
const bridgeEntry = "Resources/mcp-bridge/index.js"

// bridgeSearchDepth is how many directory levels below Plugins/ a plugin
// directory may sit to be found: 1 is the flat Plugins/UELLMToolkit, 3 the
// submodule layout Plugins/ue-llm-toolkit/Plugin/UELLMToolkit.
const bridgeSearchDepth = 4

// defaultDoc is the YAML document of the built-in defaults — only what
// differs from applyDefaults, in the order it is written.
type defaultDoc struct {
	Agent struct {
		Enabled           bool `yaml:"enabled"`
		ClaudeIntegration bool `yaml:"claudeIntegration"`
	} `yaml:"agent"`
	MCP struct {
		Enabled      bool                     `yaml:"enabled"`
		ExtraServers map[string]defaultServer `yaml:"extraServers,omitempty"`
	} `yaml:"mcp"`
}

type defaultServer struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
}

// DefaultYAML returns the built-in default configuration for the project in
// projectDir as a YAML document: agent.enabled, agent.claudeIntegration and
// mcp.enabled true and, if the UE LLM Toolkit's bridge is found below
// projectDir/Plugins (findBridges), its mcp.extraServers entry. Permissions
// are not in it and so stay off. The document is portable: the bridge path is
// written with ${PROJECT_DIR}, never as a machine path.
//
// bridges are the bridge entry points found, relative to projectDir with
// forward slashes; the document uses the first. More than one is the caller's
// to report.
func DefaultYAML(projectDir string) (doc []byte, bridges []string, err error) {
	bridges, err = findBridges(projectDir)
	if err != nil {
		return nil, nil, err
	}
	var d defaultDoc
	d.Agent.Enabled = true
	d.Agent.ClaudeIntegration = true
	d.MCP.Enabled = true
	if len(bridges) > 0 {
		d.MCP.ExtraServers = map[string]defaultServer{ToolkitServerName: {
			Command: "node",
			Args:    []string{"${PROJECT_DIR}/" + bridges[0]},
			Env:     map[string]string{"UNREAL_MCP_URL": "http://127.0.0.1:3000"},
		}}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return nil, nil, fmt.Errorf("built-in defaults: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, nil, fmt.Errorf("built-in defaults: %w", err)
	}
	return buf.Bytes(), bridges, nil
}

// findBridges returns every Resources/mcp-bridge/index.js in a directory up
// to bridgeSearchDepth levels below projectDir/Plugins, relative to projectDir
// with forward slashes, the shallowest first and by name within one depth.
// Hidden directories, node_modules, Binaries and Intermediate are not
// searched. No Plugins directory is no bridge; one that cannot be read is an
// error.
func findBridges(projectDir string) ([]string, error) {
	var found []string
	level := []string{"Plugins"}
	for depth := 0; depth <= bridgeSearchDepth && len(level) > 0; depth++ {
		var next []string
		for _, rel := range level {
			entries, err := os.ReadDir(filepath.Join(projectDir, filepath.FromSlash(rel)))
			if err != nil {
				if depth == 0 && !os.IsNotExist(err) {
					return nil, fmt.Errorf("looking for the UE LLM Toolkit: %w", err)
				}
				continue // not a directory, or one that cannot be read
			}
			if depth > 0 && fileExists(filepath.Join(projectDir, filepath.FromSlash(rel), filepath.FromSlash(bridgeEntry))) {
				found = append(found, rel+"/"+bridgeEntry)
				continue // a plugin holds no further plugin
			}
			for _, e := range entries { // os.ReadDir sorts by name
				switch name := e.Name(); {
				case strings.HasPrefix(name, "."), name == "node_modules", name == "Binaries", name == "Intermediate":
				case e.IsDir() || e.Type()&os.ModeSymlink != 0:
					next = append(next, rel+"/"+name)
				}
			}
		}
		level = next
	}
	return found, nil
}

// loadDefaults decodes the built-in defaults into c for a start without a
// config file: path is where findConfig looked for it. The project is the one
// .uproject in that directory; none or several is an error naming them, as
// there is no unreal.project to choose with. warning reports a bridge chosen
// among several.
func loadDefaults(c *Config, path string) (warning string, err error) {
	dir := filepath.Dir(path)
	matches, _ := filepath.Glob(filepath.Join(dir, "*.uproject")) // as resolveProject looks
	sort.Strings(matches)
	const fix = "put the launcher next to your .uproject, or write an unreagent.yaml that sets unreal.project"
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no unreagent.yaml and no .uproject in %s: without a config file the launcher runs on built-in defaults for the project next to it — %s (-config <path> names a config elsewhere)", filepath.ToSlash(dir), fix)
	case 1:
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = filepath.Base(m)
		}
		return "", fmt.Errorf("no unreagent.yaml in %s and several .uproject files there (%s): without a config file the launcher cannot tell which project is meant — %s", filepath.ToSlash(dir), strings.Join(names, ", "), fix)
	}
	doc, bridges, err := DefaultYAML(dir)
	if err != nil {
		return "", err
	}
	if len(bridges) > 1 {
		warning = fmt.Sprintf("several UE LLM Toolkit bridges found below Plugins/, using the first: %s — write an unreagent.yaml to choose another", strings.Join(bridges, ", "))
	}
	return warning, decodeDoc("built-in defaults", doc, c)
}
