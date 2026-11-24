package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	writeSkillFiles(s, all, agentWorkdir, logger)
	return all, nil
}

// skillsDirFor is skills.dir as the files are written to it: a relative one
// taken relative to agentWorkdir; "" when there is none.
func skillsDirFor(s config.SkillsConfig, agentWorkdir string) string {
	if s.Dir == "" || filepath.IsAbs(s.Dir) {
		return s.Dir
	}
	return filepath.Join(agentWorkdir, s.Dir)
}

// skillFiles is what writeSkillFiles wrote: the skills into skillsDir (""
// when it wrote none; skillsPartly when materializeSkills failed for some),
// and agents agent and rules files into claude.
type skillFiles struct {
	skillsDir    string
	skillsPartly bool
	agents       int
	claude       string
}

// writeSkillFiles is the part of setupSkills that writes into the project,
// while the skills are materialized: all into skills.dir, then the topic
// agents and the rules file next to it (setupAgents). `unreagent init` runs it
// too, so that a project it sets up is complete before the first start, and
// names what it wrote from the result.
func writeSkillFiles(s config.SkillsConfig, all []skills.Skill, agentWorkdir string, logger func(string)) skillFiles {
	var w skillFiles
	if !s.ViaFiles() {
		return w
	}
	dir := skillsDirFor(s, agentWorkdir)
	if dir == "" {
		// Only -no-skills-mcp gets here: config.Load clears an unused dir
		// whose placeholder is unresolved, and rejects a used one.
		logger("WARN skills not written: skills.dir names a placeholder that is unresolved — set unreal.project or skills.dir")
		return w
	}
	w.skillsDir = dir
	if err := materializeSkills(dir, all); err != nil {
		// One error per skill that failed, joined by newlines; the log line stays one.
		cause := strings.ReplaceAll(err.Error(), "\n", "; ")
		logger("WARN skills not fully written to " + dir + ": " + cause + " — its unreagent-* directories may be missing, incomplete or left from an earlier start")
		w.skillsPartly = true
	} else {
		logger(fmt.Sprintf("Skills: %d materialized into %s", len(all), dir))
	}
	// The agents are written even when a skill failed: an agent whose skill
	// is missing loses only that pre-load (Claude Code documents that it skips
	// a skill it cannot find).
	w.claude, w.agents = setupAgents(s, dir, logger)
	return w
}

// checkSkillDirs rejects a set materialization could not write one to one:
// two skills that land in the same directory (ue-x and unreagent-ue-x), two
// directories or files of one skill whose names differ only in case, which a
// case-insensitive file system (Windows, macOS) cannot tell apart, or a
// section named like a Windows device (aux.md), which Windows 10 opens as the
// device, so isPlainName refuses it there alone (reservedDeviceName; a
// directory name starts with skillDirPrefix and never is one). It is the same
// on every OS, so such a skill fails the tests, not a Windows start.
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
			if _, ok := reservedDeviceName(f); ok {
				return fmt.Errorf("skills: %s: section file %s is named like a Windows device, which Windows opens instead of the file", s.Name, f)
			}
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
// starts with skillDirPrefix belongs to the launcher: each skill's directory
// is replaced whole (installSkillDir), then every other one is removed
// (sweepSkillDirs), so a dropped skill, a stale file inside a skill's
// directory and whatever an interrupted start left do not linger; nothing
// else in dir is touched. All names are checked and every SKILL.md is
// prepared before the first change, so a set that would write outside dir,
// collide, or carry a name that cannot be rewritten leaves dir as it was.
//
// A second launcher may materialize into the same dir at the same time. A
// skill's directory only ever comes into place as a whole copy, so whichever
// run comes last, the set is whole. A skill that still cannot be put in place
// does not stop the others or the sweep; every such error is returned, joined.
// Only a work directory that cannot be created stops the skills after it,
// once, with how many were not written.
func materializeSkills(dir string, all []skills.Skill) error {
	if err := checkSkillDirs(all); err != nil {
		return err
	}
	docs := make([]map[string]string, len(all))
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
		docs[i] = map[string]string{skillFile: src}
		for section, doc := range s.Files {
			docs[i][section+".md"] = doc
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The whole set, before the first install: a skill not tried below keeps
	// its copy through the sweep.
	keep := make(map[string]bool, len(all))
	for _, s := range all {
		keep[skillDirName(s.Name)] = true
	}
	var errs []error
	for i, s := range all {
		kept, err := installSkillDir(dir, skillDirName(s.Name), docs[i])
		if kept != "" {
			keep[filepath.Base(kept)] = true
		}
		if errors.Is(err, errNoWorkDir) {
			// Every further skill would fail the same way in the same dir.
			errs = append(errs, fmt.Errorf("%w (%d of %d skills not written)", err, len(all)-i, len(all)))
			break
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if err := sweepSkillDirs(dir, keep); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// skillWorkPattern names the work directories materialization creates in
// skills.dir (os.MkdirTemp). A skill's new copy is written into one and the
// copy it replaces is moved into the same one, so neither ever sits where an
// agent looks for skills (<dir>/*/SKILL.md). The name carries skillDirPrefix
// on purpose: a work directory is ours by the one predicate and never part of
// the set, so the sweep at the end of the run removes it — and so does the
// next start's, for one an interrupted start left behind. No second rule for
// what is ours. The one exception is a work directory that keeps a previous
// copy installSkillDir could move neither in nor back: this run's sweep spares
// it, and the warning names it.
const skillWorkPattern = skillDirPrefix + ".tmp-*"

// skillRetryPauses are the pauses installSkillDir makes before its second and
// third attempt. They grow, so that a second launcher's run or a short lock on
// a file (a virus scanner on a copy just written) is likely over by the last
// attempt. There is one attempt more than pauses.
var skillRetryPauses = []time.Duration{50 * time.Millisecond, 200 * time.Millisecond}

// errNotOurs marks something in the place of a skill directory that is not a
// directory of ours; installSkillDir refuses it at once instead of trying
// again.
var errNotOurs = errors.New("in the way and not a directory of ours")

// errNoWorkDir marks a work directory os.MkdirTemp could not create in dir —
// read-only, or no space. No second launcher or scanner causes that, and every
// skill needs one there, so installSkillDir does not try again and
// materializeSkills tries no further skill.
var errNoWorkDir = errors.New("cannot create a work directory")

// installSkillDir puts a new copy of the skill directory name, holding docs
// (file name to content), in place of the one in dir. An attempt fails when a
// second launcher gets in between — it put its own copy in place first, or its
// sweep took this attempt's work directory — or a rename fails; it is then
// made again from a fresh work directory after the next of skillRetryPauses.
// The attempts end at once when something not ours is in the place, when no
// work directory can be created (errNoWorkDir), or when an attempt kept the
// previous copy in its work directory: kept names that work directory, which
// this run's sweep must spare.
func installSkillDir(dir, name string, docs map[string]string) (kept string, err error) {
	for i := 0; ; i++ {
		kept, err = tryInstallSkillDir(dir, name, docs)
		if err == nil || kept != "" || errors.Is(err, errNotOurs) || errors.Is(err, errNoWorkDir) || i == len(skillRetryPauses) {
			return kept, err
		}
		time.Sleep(skillRetryPauses[i])
	}
}

// tryInstallSkillDir is one attempt of installSkillDir: write the new copy in
// full into a fresh work directory, move the copy in place (if any) aside into
// that work directory, rename the new copy in. Between the two renames the
// skill is missing; at no point is it a partial copy. A file or symlink in the
// place is not ours and is never moved: Lstat does not follow it, and a
// directory renamed onto it fails.
//
// When the rename in fails, the previous copy is moved back, so a failed
// attempt leaves the skill as it found it. If that fails too, and the previous
// copy is still in the work directory with no second launcher's copy in its
// place, it stays there whole: the work directory is returned as kept, and the
// error says where.
func tryInstallSkillDir(dir, name string, docs map[string]string) (kept string, err error) {
	work, err := os.MkdirTemp(dir, skillWorkPattern)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errNoWorkDir, err)
	}
	next := filepath.Join(work, name)
	// Mkdir, not MkdirAll: if a second launcher's sweep took work, this
	// attempt fails instead of writing into a directory recreated under it.
	if err := os.Mkdir(next, 0o755); err != nil {
		return "", err
	}
	for f, doc := range docs {
		if err := os.WriteFile(filepath.Join(next, f), []byte(doc), 0o644); err != nil {
			return "", err
		}
		materializeHook("written")
	}

	target := filepath.Join(dir, name)
	previous := ""
	switch fi, err := os.Lstat(target); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", err
	case !fi.IsDir():
		return "", fmt.Errorf("%s: %w", target, errNotOurs)
	default:
		// Not renamed onto target: Windows cannot rename a directory onto
		// an existing one, Linux only onto an empty one.
		previous = filepath.Join(work, "previous")
		if err := os.Rename(target, previous); err != nil {
			return "", err
		}
		materializeHook("aside")
	}
	if err := os.Rename(next, target); err != nil {
		if previous == "" {
			return "", err
		}
		restoreErr := os.Rename(previous, target)
		if restoreErr == nil {
			return "", err
		}
		// The previous copy gone: a second launcher's sweep took work with
		// it. The place taken: a second launcher put its whole copy there.
		// Either way there is nothing to keep, and the next attempt goes on.
		_, prevErr := os.Lstat(previous)
		_, targetErr := os.Lstat(target)
		if errors.Is(prevErr, fs.ErrNotExist) || targetErr == nil {
			return "", err
		}
		return work, fmt.Errorf("%w, and moving the previous copy back failed too (%v): it is kept in %s until the next start removes it", err, restoreErr, previous)
	}
	materializeHook("installed")
	return "", nil
}

// sweepSkillDirs removes every directory of ours in dir that keep (the set,
// and a work directory that keeps a previous copy) does not name: a skill no
// longer shipped, this run's other work directories, and any a second
// launcher or an interrupted start left. Each one is first renamed to a fresh
// name and only then removed. A work directory of a second launcher
// still running may be among them; removed in place, that launcher could
// rename its new copy out of it into the skill's place while the removal is
// still emptying it. Renamed first, either that launcher's rename comes first
// and its copy is whole in place, or it fails and that launcher tries again.
// An entry gone before it is renamed was taken by a second sweep. One that
// cannot be renamed or removed (a file a virus scanner holds open) does not
// stop the sweep: every other one is still tried, and each such error is
// returned, joined; the next start tries it again.
func sweepSkillDirs(dir string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	materializeHook("listed")
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), skillDirPrefix) || keep[e.Name()] {
			continue
		}
		path := filepath.Join(dir, e.Name())
		gone := filepath.Join(dir, skillDirPrefix+".tmp-"+rand.Text())
		if err := os.Rename(path, gone); err != nil {
			if _, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		materializeHook("claimed")
		if err := os.RemoveAll(gone); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// unprovenSkillDirs lists the directories in dir that the materialization of
// all would replace (named like a skill of the set) or remove (any other
// unreagent-* name), except those it can show to be its own copy: a
// directory that holds exactly the files materializeSkills writes for that
// skill now, byte for byte, and the work directories it names
// skillWorkPattern. It mirrors sweepSkillDirs' rule — real directories whose
// name starts with skillDirPrefix; a symlink or a file is never touched there
// — and changes nothing. `unreagent init` uses it to see, before it
// materializes, what a project's own unreagent-* skills would lose (k256).
// A dir that does not exist holds none.
func unprovenSkillDirs(dir string, all []skills.Skill) (replaced, removed []string, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	byDir := make(map[string]skills.Skill, len(all))
	for _, s := range all {
		byDir[skillDirName(s.Name)] = s
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || !strings.HasPrefix(n, skillDirPrefix) {
			continue
		}
		if ok, _ := filepath.Match(skillWorkPattern, n); ok {
			continue
		}
		s, shipped := byDir[n]
		if !shipped {
			removed = append(removed, n)
			continue
		}
		if !holdsCopyOf(filepath.Join(dir, n), s, n) {
			replaced = append(replaced, n)
		}
	}
	return replaced, removed, nil
}

// holdsCopyOf reports whether path, the directory name, holds exactly what
// materializeSkills writes for s there: SKILL.md and the section files, each
// a regular file with that content, and nothing else.
func holdsCopyOf(path string, s skills.Skill, name string) bool {
	src, err := materializedSource(s, name)
	if err != nil {
		return false
	}
	want := map[string]string{skillFile: src}
	for section, doc := range s.Files {
		want[section+".md"] = doc
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != len(want) {
		return false
	}
	for _, e := range entries {
		doc, ok := want[e.Name()]
		if !ok || !e.Type().IsRegular() {
			return false
		}
		if data, err := os.ReadFile(filepath.Join(path, e.Name())); err != nil || string(data) != doc {
			return false
		}
	}
	return true
}

// materializeHook is called between the steps of materializeSkills; tests
// replace it to run a second materialization, or to stop one, in each gap.
var materializeHook = func(step string) {}

// materializedSource is the SKILL.md materialization writes for s into the
// directory dir: s.Source byte for byte, except that when dir is not the
// skill's name the frontmatter's name value becomes dir — the Agent Skills
// format wants name to equal the parent directory, and a prefixed name cannot
// clash with a project's own skill of the shipped name. The frontmatter is
// s.Source without s.Body, the split Load made; internal/skills exposes no
// position inside it, so the YAML parser locates the value here. The text at
// that position must be the name, plain or in the quotes its style says, on
// one line, and is replaced inside them. Any other form is an error rather
// than a rewrite by guess — for example a tag, an anchor, an alias, an escape,
// a folded or block scalar, or a value over several lines.
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
