package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/skills"
)

// skillDirPrefix marks the directories in skills.dir that materialization
// owns. It is the one predicate for "ours": a directory whose name starts with
// it is replaced or removed on every start; anything else is never touched.
const skillDirPrefix = "unreagent-"

// skillFile is the main document of a skill directory.
const skillFile = "SKILL.md"

// loadEmbeddedSkills is skills.Load; tests replace it to feed setupSkills a
// set that does not load.
var loadEmbeddedSkills = skills.Load

// skillDirName is the directory a skill is materialized into: its name when
// that already starts with skillDirPrefix (the unreagent-* skills), otherwise
// the prefix plus its name — never a doubled prefix.
func skillDirName(name string) string {
	if strings.HasPrefix(name, skillDirPrefix) {
		return name
	}
	return skillDirPrefix + name
}

// applyNoSkillsMCP is the -no-skills-mcp flag: skills reach the agent as files
// in skills.dir instead of through the skills MCP tool. run() calls it after
// config.Load, so it wins over unreagent.yaml and unreagent.local.yaml alike.
func applyNoSkillsMCP(s *config.SkillsConfig, logger func(string)) {
	off := false
	s.MCP = &off
	s.Materialize = true
	if !s.ViaFiles() {
		logger("Flag -no-skills-mcp: no effect, skills.enabled is false")
		return
	}
	logger("Flag -no-skills-mcp: skills are written to skills.dir instead of served by the skills MCP tool")
}

// setupSkills loads the embedded skills while skills are enabled and writes
// them into skills.dir when they are materialized; it returns the set for the
// skills MCP tool (nil while disabled). A set that does not load is an error
// that stops the start: it cannot happen in a tested build, and if it does the
// skills must not vanish silently. A failed write only warns — the editor and
// the agent still start, and the log names the cause. A relative skills.dir is
// taken relative to the agent's working directory, like mcp.writeConfig paths
// and MCP server scripts.
func setupSkills(cfg *config.Config, agentWorkdir string, logger func(string)) ([]skills.Skill, error) {
	s := cfg.Skills
	if !s.ViaMCP() && !s.ViaFiles() {
		return nil, nil
	}
	all, err := loadEmbeddedSkills()
	if err == nil {
		err = checkSkillDirs(all)
	}
	if err != nil {
		return nil, fmt.Errorf("%w (skills.enabled: false starts without them)", err)
	}

	switch {
	case s.ViaMCP() && cfg.MCP.Enabled:
		logger(fmt.Sprintf("Skills: %d served by the skills MCP tool", len(all)))
	case s.ViaMCP() && !s.ViaFiles():
		logger("WARN no skills reach the agent: the skills MCP tool needs mcp.enabled — enable it, set skills.materialize: true, or skills.enabled: false")
	}
	if !s.ViaFiles() {
		return all, nil
	}
	dir := s.Dir
	if dir == "" {
		// Only -no-skills-mcp gets here: config.Load clears an unused dir
		// whose placeholder is unresolved, and rejects a used one.
		logger("WARN skills not written: skills.dir names a placeholder that is unresolved — set unreal.project or skills.dir")
		return all, nil
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(agentWorkdir, dir)
	}
	if err := materializeSkills(dir, all); err != nil {
		logger("WARN skills not fully written to " + dir + ": " + err.Error() + " — its unreagent-* directories may be missing, incomplete or left from an earlier start")
		return all, nil
	}
	logger(fmt.Sprintf("Skills: %d materialized into %s", len(all), dir))
	return all, nil
}

// checkSkillDirs rejects a set materialization could not write one to one:
// two skills that land in the same directory (ue-x and unreagent-ue-x), or two
// directories or files of one skill whose names differ only in case, which a
// case-insensitive file system (Windows, macOS) cannot tell apart. It is the
// same on every OS, so a colliding skill fails the tests, not a Windows start.
func checkSkillDirs(all []skills.Skill) error {
	var dirs, owners []string
	for _, s := range all {
		d := skillDirName(s.Name)
		for i, other := range dirs {
			if strings.EqualFold(d, other) {
				return fmt.Errorf("skills: %s and %s would both be materialized into %s (directory names are compared ignoring case)", owners[i], s.Name, other)
			}
		}
		dirs, owners = append(dirs, d), append(owners, s.Name)

		files := []string{skillFile}
		for _, section := range slices.Sorted(maps.Keys(s.Files)) {
			f := section + ".md"
			for _, other := range files {
				if strings.EqualFold(f, other) {
					return fmt.Errorf("skills: %s: %s and %s differ only in case and would overwrite each other on Windows", s.Name, other, f)
				}
			}
			files = append(files, f)
		}
	}
	return nil
}

// materializeSkills writes every skill into dir: <dir>/<skillDirName>/SKILL.md
// as embedded, except that its name names the directory (materializedSource),
// plus its <section>.md sub-documents. Every directory in dir whose name
// starts with skillDirPrefix belongs to the launcher and is removed first, so
// a dropped skill and a stale file inside a skill's directory do not linger;
// nothing else in dir is touched. All names are checked and every SKILL.md is
// prepared before the first change, so a set that would write outside dir,
// collide, or carry a name that cannot be rewritten leaves dir as it was. A
// removal or write that fails after that leaves dir partly updated.
func materializeSkills(dir string, all []skills.Skill) error {
	if err := checkSkillDirs(all); err != nil {
		return err
	}
	sources := make([]string, len(all))
	for i, s := range all {
		d := skillDirName(s.Name)
		if !isPlainName(d) {
			return fmt.Errorf("skill %q: directory %q is not a plain name inside %s", s.Name, d, dir)
		}
		for section := range s.Files {
			if !isPlainName(section + ".md") {
				return fmt.Errorf("skill %q: section %q is not a plain file name", s.Name, section)
			}
		}
		src, err := materializedSource(s, d)
		if err != nil {
			return err
		}
		sources[i] = src
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), skillDirPrefix) {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	for i, s := range all {
		target := filepath.Join(dir, skillDirName(s.Name))
		// Mkdir, not MkdirAll: anything still in the way is not ours.
		if err := os.Mkdir(target, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(target, skillFile), []byte(sources[i]), 0o644); err != nil {
			return err
		}
		for section, doc := range s.Files {
			if err := os.WriteFile(filepath.Join(target, section+".md"), []byte(doc), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// materializedSource is the SKILL.md materialization writes for s into the
// directory dir: s.Source byte for byte, except that when dir is not the
// skill's name the frontmatter's name value becomes dir — the Agent Skills
// format wants name to equal the parent directory, and a prefixed name cannot
// clash with a project's own skill of the shipped name. The frontmatter is
// s.Source without s.Body, the split Load made; internal/skills exposes no
// position inside it, so the YAML parser locates the value here. The text at
// that position must be the name, plain or in the quotes its style says, and
// is replaced inside them; any other form (a block scalar, an escape, a tag)
// is an error rather than a rewrite by guess.
func materializedSource(s skills.Skill, dir string) (string, error) {
	if dir == s.Name {
		return s.Source, nil
	}
	fail := func(why string) (string, error) {
		return "", fmt.Errorf("skill %q: cannot set the name in its SKILL.md to %s: %s", s.Name, dir, why)
	}
	front, ok := strings.CutSuffix(s.Source, s.Body)
	if !ok {
		return fail("its body is not the end of the file")
	}
	_, yamlText, _ := strings.Cut(front, "\n") // after the opening --- line
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlText), &doc); err != nil {
		return fail(err.Error())
	}
	i := -1
	if len(doc.Content) == 1 && doc.Content[0].Kind == yaml.MappingNode {
		i = mappingEntry(doc.Content[0], "name")
	}
	if i < 0 {
		return fail("its frontmatter has no name key")
	}
	v := doc.Content[0].Content[i+1]
	quote := map[yaml.Style]string{yaml.SingleQuotedStyle: "'", yaml.DoubleQuotedStyle: `"`}[v.Style]
	old := quote + s.Name + quote
	lines := strings.SplitAfter(front, "\n") // yamlText's line n is lines[n]
	var line []rune
	if v.Line < len(lines) {
		line = []rune(lines[v.Line])
	}
	col := v.Column - 1 // Column counts runes from 1
	if col < 0 || col > len(line) || !strings.HasPrefix(string(line[col:]), old) {
		return fail("its name is not a plain or quoted scalar on one line")
	}
	lines[v.Line] = string(line[:col]) + quote + dir + quote + strings.TrimPrefix(string(line[col:]), old)
	return strings.Join(lines, "") + s.Body, nil
}

// isPlainName reports whether name is one path element that stays where it is
// joined: no separator, no "..", nothing the OS reserves.
func isPlainName(name string) bool {
	return filepath.IsLocal(name) && filepath.Base(name) == name
}

// skillsToolPreamble leads the skills tool's description; skills.DescribeAll
// appends one "- <name>: <description>" line per skill.
const skillsToolPreamble = "Loads a skill: operating instructions shipped with unreagent for one kind of work in this setup — which tools to call, in what order, what their errors mean. " +
	"When your task matches a skill listed below, load it before you start and follow it; a call returns its main document, which names any sub-documents — each is one more call with section. " +
	"name: the skill, exactly as listed. section (optional): one of its sub-documents, as the skill names it (with or without .md). " +
	"Skills (name: when to use it):\n"

// skillsTool is the skills MCP tool over all. Its description carries every
// skill's selection line, so the agent needs no list call before loading one.
func skillsTool(all []skills.Skill) mcp.Tool {
	names := make([]string, len(all))
	byName := make(map[string]skills.Skill, len(all))
	for i, s := range all {
		names[i] = s.Name
		byName[s.Name] = s
	}
	available := "available skills: " + strings.Join(names, ", ")
	return mcp.Tool{
		Name:        "skills",
		Description: skillsToolPreamble + skills.DescribeAll(all),
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":    map[string]interface{}{"type": "string", "description": "Skill name, exactly as listed in this tool's description"},
				"section": map[string]interface{}{"type": "string", "description": "Optional sub-document of the skill, as the skill names it (with or without .md); omit it for the skill itself"},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			name := getString(args, "name", "")
			if name == "" {
				return errResult(errors.New("'name' missing — " + available))
			}
			s, ok := byName[name]
			if !ok {
				return errResult(fmt.Errorf("unknown skill %q — %s", name, available))
			}
			section := getString(args, "section", "")
			if section == "" {
				return mcp.ToolResult{Text: s.Body}
			}
			doc, ok := s.Files[section]
			if !ok {
				doc, ok = s.Files[strings.TrimSuffix(section, ".md")]
			}
			if !ok {
				if len(s.Files) == 0 {
					return errResult(fmt.Errorf("skill %q has no section %q: it has no sections", name, section))
				}
				return errResult(fmt.Errorf("skill %q has no section %q — sections: %s", name, section, strings.Join(slices.Sorted(maps.Keys(s.Files)), ", ")))
			}
			return mcp.ToolResult{Text: doc}
		},
	}
}
