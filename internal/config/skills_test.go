package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// k10/k15: a config that says nothing about skills delivers them through the
// MCP tool, does not write files, and has its dir substituted from the
// project.
func TestSkillsDefaults(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	dir := t.TempDir()
	writeUProject(t, dir, "Game.uproject")
	c, _, err := Load(writeConfigFiles(t, dir, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	s := c.Skills
	if !boolVal(s.Enabled) || !boolVal(s.MCP) || s.Materialize {
		t.Errorf("skills enabled/mcp/materialize = %v/%v/%v; want true/true/false", boolVal(s.Enabled), boolVal(s.MCP), s.Materialize)
	}
	if want := filepath.ToSlash(dir) + "/.claude/skills"; s.Dir != want {
		t.Errorf("skills.dir = %q; want %q", s.Dir, want)
	}
	if !s.ViaMCP() || s.ViaFiles() {
		t.Errorf("ViaMCP/ViaFiles = %v/%v; want true/false", s.ViaMCP(), s.ViaFiles())
	}
}

// k10/k15: mcp off with materialize off would ship nothing — a load error
// that names the knobs, not a silent no-op. materialize defaults to false, so
// mcp: false on its own is the same error.
func TestSkillsBothPathsOffIsRejected(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, yaml := range []string{
		"skills: {mcp: false}\n",
		"skills: {enabled: true, mcp: false, materialize: false}\n",
	} {
		_, _, err := Load(writeConfigFiles(t, t.TempDir(), yaml, ""))
		errContains(t, err, "skills.mcp", "skills.materialize", "skills.enabled: false")
	}
}

// k10/k15: enabled: false turns both paths off, so the same two false knobs
// are no error then; both paths on together is legal too.
func TestSkillsLegalCombinations(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, tc := range []struct {
		yaml           string
		viaMCP, viaDir bool
	}{
		{"skills: {enabled: false, mcp: false, materialize: false}\n", false, false},
		{"skills: {enabled: false, materialize: true}\n", false, false},
		{"skills: {mcp: true, materialize: true}\n", true, true},
		{"skills: {mcp: false, materialize: true}\n", false, true},
	} {
		dir := t.TempDir()
		writeUProject(t, dir, "Game.uproject")
		c, _, err := Load(writeConfigFiles(t, dir, tc.yaml, ""))
		if err != nil {
			t.Errorf("%q: %v", tc.yaml, err)
			continue
		}
		if c.Skills.ViaMCP() != tc.viaMCP || c.Skills.ViaFiles() != tc.viaDir {
			t.Errorf("%q: ViaMCP/ViaFiles = %v/%v; want %v/%v", tc.yaml, c.Skills.ViaMCP(), c.Skills.ViaFiles(), tc.viaMCP, tc.viaDir)
		}
	}
}

// skills.dir counts as used only while materialization is on. Unused and
// unresolved, it is cleared like files.root, so that -no-skills-mcp, which
// turns materialization on after loading, finds no dir rather than a garbage
// one such as /.claude/skills — and a config without a .uproject (the demo)
// keeps loading.
func TestSkillsDirUnresolvedWhenUnused(t *testing.T) {
	noEngine(t)
	for _, yaml := range []string{
		"unreal: {editor: ue}\n",
		"unreal: {editor: ue}\nskills: {enabled: false, materialize: true}\n",
	} {
		c, _, err := Load(writeConfigFiles(t, t.TempDir(), yaml, ""))
		if err != nil {
			t.Errorf("%q: %v", yaml, err)
			continue
		}
		if c.Skills.Dir != "" {
			t.Errorf("%q: skills.dir = %q; want empty while ${PROJECT_DIR} is unresolved", yaml, c.Skills.Dir)
		}
	}
}

// k259: skills.agents defaults to true but takes effect only while the skills
// are materialized; false turns the agent files off, and the local overlay can
// turn them off over a base that leaves them on.
func TestSkillsAgents(t *testing.T) {
	t.Setenv("UE_ROOT", "/ue")
	for _, tc := range []struct {
		base, local string
		agents      bool // the field, default applied
		files       bool // AgentFiles
	}{
		{"", "", true, false},
		{"skills: {materialize: true}\n", "", true, true},
		{"skills: {materialize: true, agents: false}\n", "", false, false},
		{"skills: {materialize: true}\n", "skills: {agents: false}\n", false, false},
		{"skills: {agents: true}\n", "", true, false},
		{"skills: {enabled: false, materialize: true, agents: true}\n", "", true, false},
	} {
		dir := t.TempDir()
		writeUProject(t, dir, "Game.uproject")
		c, _, err := Load(writeConfigFiles(t, dir, tc.base, tc.local))
		if err != nil {
			t.Errorf("%q + %q: %v", tc.base, tc.local, err)
			continue
		}
		if got := boolVal(c.Skills.Agents); got != tc.agents {
			t.Errorf("%q + %q: skills.agents = %v; want %v", tc.base, tc.local, got, tc.agents)
		}
		if got := c.Skills.AgentFiles(); got != tc.files {
			t.Errorf("%q + %q: AgentFiles = %v; want %v", tc.base, tc.local, got, tc.files)
		}
	}
}

// MaterializeSetting tells a file that sets skills.materialize, to either
// value, from one that does not set it: the value an overlay file wins with.
func TestMaterializeSetting(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct {
		doc     string
		on, set bool
	}{
		"on":              {"skills: {materialize: true}\n", true, true},
		"off":             {"skills:\n  materialize: false\n", false, true},
		"null":            {"skills: {materialize: null}\n", false, false},
		"other key":       {"skills: {agents: false}\n", false, false},
		"no skills":       {"agent: {enabled: true}\n", false, false},
		"empty":           {"", false, false},
		"not YAML at all": {"skills: [\n", false, false},
	} {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".yaml")
		if err := os.WriteFile(p, []byte(c.doc), 0o644); err != nil {
			t.Fatal(err)
		}
		if on, set := MaterializeSetting(p); on != c.on || set != c.set {
			t.Errorf("%s: MaterializeSetting = %v, %v; want %v, %v", name, on, set, c.on, c.set)
		}
	}
	if on, set := MaterializeSetting(filepath.Join(dir, "missing.yaml")); on || set {
		t.Errorf("missing file: MaterializeSetting = %v, %v; want false, false", on, set)
	}
}
