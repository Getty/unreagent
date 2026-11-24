package skills

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	shippedAgents "github.com/conflict-industries/unreagent/agents"
)

// preloadBudget caps what one agent pre-loads: the SKILL.md bytes of its
// skills, which stay in the subagent's context for its whole run. 80,000
// bytes is about 20,000 tokens, a tenth of a 200,000-token context, spent
// before the agent has read its task. It guards against two kinds of growth:
// an agent that gains a skill, and a SKILL.md that grows where a section file
// was due. When it trips, move text of the grown skill into a section first,
// or take a skill off the agent and name it in the body as loaded on demand
// (onDemandSkills, if no agent pre-loads it then). Raise the cap only with a
// reason. go test -v logs every agent's pre-load, so the margin is visible.
const preloadBudget = 80_000

// sharedSkills are on every agent (k256, M5): how to reach the plugin's own
// reference notes, and the check whether the plugin answers.
var sharedSkills = []string{"ue-context"}

// onDemandSkills are shipped skills no agent pre-loads, each with the agent
// that loads it when a task needs it. That agent's body must name the skill.
var onDemandSkills = map[string]string{
	// Belongs to animation work, but with it the agent would carry four tool
	// skills; key and skeleton edits are the rarer task there (k256 cut).
	"ue-anim-sequence": "animation",
}

// mainSessionOnlySkills are shipped skills that belong to no topic agent
// because only the main session uses them, each with the reason. Empty today:
// every shipped skill has an agent. A skill goes here only with its reason;
// "no agent fits yet" is not one.
var mainSessionOnlySkills = map[string]string{}

// agentMD builds a well-formed agent definition; extra is spliced into the
// frontmatter as-is.
func agentMD(name, extra string) *fstest.MapFile {
	return md("---\nname: " + name + "\ndescription: Work on " + name +
		".\nskills:\n  - ue-context\n" + extra + "---\n\nYou work on " + name + ".\n")
}

const (
	goodRules   = "# rules\n\n" + agentsMark + "\n"
	goodStarter = KickstartBegin + "\nAsk the user.\n" + KickstartEnd + "\n\n# Project\n"
)

// agentFS is a valid set, a-good plus the two project texts, with files laid
// over it; a nil entry removes that file.
func agentFS(files map[string]*fstest.MapFile) fstest.MapFS {
	fsys := fstest.MapFS{
		"a-good.md":          agentMD("a-good", ""),
		"project/rules.md":   md(goodRules),
		"project/starter.md": md(goodStarter),
	}
	for name, f := range files {
		if f == nil {
			delete(fsys, name)
			continue
		}
		fsys[name] = f
	}
	return fsys
}

// Every agent that ships in the binary must parse, and none may go missing on
// the way: one Agent per embedded definition, in file order, with the fields
// routing and pre-loading need.
func TestLoadAgentsParsesEveryEmbeddedAgent(t *testing.T) {
	set, err := LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	files, err := fs.Glob(shippedAgents.FS, "*.md")
	if err != nil {
		t.Fatalf("Glob(embedded): %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no agent definitions embedded")
	}
	if len(set.Agents) != len(files) {
		t.Fatalf("LoadAgents returned %d agents for %d embedded definitions", len(set.Agents), len(files))
	}
	for i, a := range set.Agents {
		if want := strings.TrimSuffix(files[i], ".md"); a.Name != want {
			t.Errorf("agent %d: Name = %q, want the file name %q", i, a.Name, want)
		}
		if a.Description == "" || len(a.Skills) == 0 || strings.TrimSpace(a.Body) == "" {
			t.Errorf("agent %q: description, skills or body is empty", a.Name)
		}
	}
	if strings.TrimSpace(set.Starter) == "" {
		t.Error("the starter text is empty")
	}
}

// A file under agents/ that the embed patterns do not match would silently not
// ship. Dot-named entries are local clutter and skipped, as for skills/.
func TestAgentsEmbedMissesNoSourceFile(t *testing.T) {
	root := filepath.Join("..", "..", "agents")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if _, err := fs.Stat(shippedAgents.FS, filepath.ToSlash(rel)); err != nil {
			t.Errorf("agents/%s is not embedded (pattern mismatch) and would not ship", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%s): %v", root, err)
	}
}

// A broken definition or project text must stop the load, never be skipped.
// Each case is laid over a valid set; want must appear in the error.
func TestLoadAgentsRejectsMalformedSet(t *testing.T) {
	manySkills := ""
	for i := 0; i < MaxAgentSkills; i++ { // one more than allowed, with agentMD's own
		manySkills += fmt.Sprintf("  - s%d\n", i)
	}
	sameDescription := md("---\nname: bad\ndescription: Work on  a-good.\nskills: [ue-context]\n---\nbody\n")

	cases := []struct {
		name  string
		files map[string]*fstest.MapFile
		want  string
	}{
		{"no frontmatter", map[string]*fstest.MapFile{"bad.md": md("# bad\n")}, "bad.md"},
		{"unterminated frontmatter", map[string]*fstest.MapFile{"bad.md": md("---\nname: bad\n")}, "bad.md"},
		{"trailing blank on a delimiter", map[string]*fstest.MapFile{"bad.md": md("--- \nname: bad\n---\nbody\n")}, "trailing whitespace"},
		{"missing name", map[string]*fstest.MapFile{"bad.md": md("---\ndescription: x\nskills: [a]\n---\nbody\n")}, "name is missing"},
		{"name differs from the file", map[string]*fstest.MapFile{"bad.md": agentMD("a-good", "")}, "differs from the file name"},
		{"uppercase name", map[string]*fstest.MapFile{"Bad.md": agentMD("Bad", "")}, "lowercase"},
		{"name with an underscore", map[string]*fstest.MapFile{"b_ad.md": agentMD("b_ad", "")}, "lowercase"},
		{"draft file", map[string]*fstest.MapFile{"_bad.md": agentMD("_bad", "")}, `"." or "_"`},
		{"missing description", map[string]*fstest.MapFile{"bad.md": md("---\nname: bad\nskills: [a]\n---\nbody\n")}, "description is missing"},
		{"description with a table bar", map[string]*fstest.MapFile{"bad.md": md("---\nname: bad\ndescription: a | b\nskills: [a]\n---\nbody\n")}, "agent table"},
		{"description of another agent", map[string]*fstest.MapFile{"bad.md": sameDescription}, "same description as a-good"},
		{"missing skills", map[string]*fstest.MapFile{"bad.md": md("---\nname: bad\ndescription: x\n---\nbody\n")}, "skills is missing or empty"},
		{"empty skills", map[string]*fstest.MapFile{"bad.md": md("---\nname: bad\ndescription: x\nskills: []\n---\nbody\n")}, "skills is missing or empty"},
		{"skills not a list", map[string]*fstest.MapFile{"bad.md": md("---\nname: bad\ndescription: x\nskills: a\n---\nbody\n")}, "bad.md"},
		{"skill named twice", map[string]*fstest.MapFile{"bad.md": agentMD("bad", "  - ue-context\n")}, "names ue-context twice"},
		{"empty skill name", map[string]*fstest.MapFile{"bad.md": agentMD("bad", "  - \"\"\n")}, "empty or padded"},
		{"more skills than an agent may name", map[string]*fstest.MapFile{"bad.md": agentMD("bad", manySkills)}, "33 skills, more than the 32"},
		{"undocumented key", map[string]*fstest.MapFile{"bad.md": agentMD("bad", "model: opus\n")}, "bad.md"},
		{"second YAML document", map[string]*fstest.MapFile{"bad.md": agentMD("bad", "--- {bogus: 1}\n")}, "content after the end of the YAML document"},
		{"empty body", map[string]*fstest.MapFile{"bad.md": md("---\nname: bad\ndescription: x\nskills: [a]\n---\n\n")}, "body is empty"},
		{"non-markdown file", map[string]*fstest.MapFile{"bad.txt": md("x\n")}, "bad.txt"},
		{"second directory", map[string]*fstest.MapFile{"bad/x.md": md("x\n")}, "the only directory is project"},

		{"no project directory", map[string]*fstest.MapFile{"project/rules.md": nil, "project/starter.md": nil}, "no project directory"},
		{"no rules text", map[string]*fstest.MapFile{"project/rules.md": nil}, "no project/rules.md"},
		{"no starter text", map[string]*fstest.MapFile{"project/starter.md": nil}, "no project/starter.md"},
		{"third project text", map[string]*fstest.MapFile{"project/notes.md": md("x\n")}, "project/notes.md"},
		{"rules without the table line", map[string]*fstest.MapFile{"project/rules.md": md("# rules\n")}, "0 lines of exactly {{agents}}"},
		{"rules with the table line inside a sentence", map[string]*fstest.MapFile{"project/rules.md": md("# rules\nsee " + agentsMark + "\n")}, "0 lines of exactly {{agents}}"},
		{"rules with two table lines", map[string]*fstest.MapFile{"project/rules.md": md(goodRules + agentsMark + "\n")}, "2 lines of exactly {{agents}}"},
		{"rules with nothing but the table", map[string]*fstest.MapFile{"project/rules.md": md(agentsMark + "\n")}, "nothing but the agent table"},
		{"starter without a begin line", map[string]*fstest.MapFile{"project/starter.md": md("Ask.\n" + KickstartEnd + "\n# Project\n")}, "0 begin and 1 end"},
		{"starter without an end line", map[string]*fstest.MapFile{"project/starter.md": md(KickstartBegin + "\nAsk.\n# Project\n")}, "1 begin and 0 end"},
		{"starter with the block turned round", map[string]*fstest.MapFile{"project/starter.md": md(KickstartEnd + "\nAsk.\n" + KickstartBegin + "\n# Project\n")}, "ends before it begins"},
		{"starter with an empty block", map[string]*fstest.MapFile{"project/starter.md": md(KickstartBegin + "\n\n" + KickstartEnd + "\n# Project\n")}, "kickstart block is empty"},
		{"starter without a project part", map[string]*fstest.MapFile{"project/starter.md": md(KickstartBegin + "\nAsk.\n" + KickstartEnd + "\n")}, "project part is missing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set, err := loadAgents(agentFS(c.files))
			if err == nil {
				t.Fatalf("loadAgents succeeded with %d agents; want an error", len(set.Agents))
			}
			if len(set.Agents) != 0 || set.Starter != "" || set.rules != "" {
				t.Errorf("loadAgents returned part of a set alongside the error; want none")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not say %q", err, c.want)
			}
		})
	}
}

// The valid base of the cases above loads, so each of them fails for its own
// reason; a folded description reads as one line, CRLF parses.
func TestLoadAgentsParsesFrontmatter(t *testing.T) {
	set, err := loadAgents(agentFS(map[string]*fstest.MapFile{
		"b-crlf.md": md("---\r\nname: b-crlf\r\ndescription: >\r\n  Work on\r\n  CRLF.\r\nskills: [ue-context, ue-x]\r\n---\r\n\r\nBody.\r\n"),
	}))
	if err != nil {
		t.Fatalf("loadAgents: %v", err)
	}
	if len(set.Agents) != 2 || set.Agents[0].Name != "a-good" || set.Agents[1].Name != "b-crlf" {
		t.Fatalf("got agents %+v, want a-good and b-crlf in file order", set.Agents)
	}
	b := set.Agents[1]
	if b.Description != "Work on CRLF." || b.Body != "Body.\r\n" || strings.Join(b.Skills, ",") != "ue-context,ue-x" {
		t.Errorf("b-crlf = %+v", b)
	}
	if set.Starter != goodStarter {
		t.Errorf("Starter = %q, want the embedded text %q", set.Starter, goodStarter)
	}
}

// The rules text carries the table where the embedded text marks it, renders
// the names it is given, and leaves no mark behind. The format is pinned: it
// is prompt text.
func TestRulesRendersTheAgentTable(t *testing.T) {
	set, err := loadAgents(agentFS(map[string]*fstest.MapFile{
		"project/rules.md": md("# rules\n\n" + agentsMark + "\n\nEnd.\n"),
	}))
	if err != nil {
		t.Fatalf("loadAgents: %v", err)
	}
	got := set.Rules([]Agent{
		{Name: "unreagent-a", Description: "Work on a."},
		{Name: "unreagent-b", Description: "Work on b."},
	})
	want := "# rules\n\n| Task | Agent |\n|---|---|\n| Work on a. | `unreagent-a` |\n| Work on b. | `unreagent-b` |\n\nEnd.\n"
	if got != want {
		t.Errorf("Rules = %q, want %q", got, want)
	}
}

// The same over the shipped set: one row per agent, no mark left, and the
// size logged, because the main session carries the text. The
// two launcher skills must be named by the fixed text itself, so they are
// looked for in the rules rendered without a single agent: a table row that
// happens to hold the same word cannot stand in for the pointer.
func TestRulesListsEveryShippedAgent(t *testing.T) {
	set, err := LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	out := set.Rules(set.Agents)
	if strings.Contains(out, agentsMark) {
		t.Errorf("the rendered rules still hold %s", agentsMark)
	}
	for _, a := range set.Agents {
		if row := "| " + a.Description + " | `" + a.Name + "` |\n"; !strings.Contains(out, row) {
			t.Errorf("the rendered rules lack the row of %q", a.Name)
		}
	}
	fixed := set.Rules(nil)
	for _, skill := range []string{"unreagent-build", "unreagent-iterate"} {
		if !strings.Contains(fixed, "`"+skill+"`") {
			t.Errorf("the fixed rules text does not point at skill %s", skill)
		}
	}
	t.Logf("rules: %d bytes for %d agents", len(out), len(set.Agents))
}

// agentPrefix is what the launcher puts in front of an agent's name when it
// writes the definition into a project (k259), and skillDirPrefix what it puts
// in front of a skill's directory. Both are restated here because this package
// must not import cmd/launcher; the source of the second is skillDirPrefix
// and skillDirName in cmd/launcher/skills.go.
const (
	agentPrefix    = "unreagent-"
	skillDirPrefix = "unreagent-"
)

// skillDirName restates skillDirName of cmd/launcher/skills.go: the directory
// a skill is materialized into is its name when that already starts with the
// prefix, otherwise the prefix plus its name.
func skillDirName(name string) string {
	if strings.HasPrefix(name, skillDirPrefix) {
		return name
	}
	return skillDirPrefix + name
}

// nameClashes returns one line per agent whose written name equals the
// materialized directory name of a shipped skill.
func nameClashes(agents []Agent, all []Skill) []string {
	dirs := map[string]string{} // directory name -> skill
	for _, s := range all {
		dirs[skillDirName(s.Name)] = s.Name
	}
	var clashes []string
	for _, a := range agents {
		if skill, ok := dirs[agentPrefix+a.Name]; ok {
			clashes = append(clashes, fmt.Sprintf("agent %s is written as %s, the name skill %s is materialized under", a.Name, agentPrefix+a.Name, skill))
		}
	}
	return clashes
}

// An agent and a skill under one name cannot be told apart: the rules text
// says "Skill `unreagent-build`" next to the agent table, and a main session
// reading both cannot know which is meant. An agent named `build` would do
// exactly that, which is why the agent is `build-test`.
func TestNoAgentIsWrittenUnderASkillsName(t *testing.T) {
	set, err := LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	all, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, c := range nameClashes(set.Agents, all) {
		t.Error(c)
	}

	// The check must be able to fail, for a launcher skill and for a ue- one.
	skills := []Skill{{Name: "unreagent-build"}, {Name: "ue-assets"}}
	got := nameClashes([]Agent{{Name: "build"}, {Name: "ue-assets"}, {Name: "assets"}, {Name: "build-test"}}, skills)
	want := []string{
		"agent build is written as unreagent-build, the name skill unreagent-build is materialized under",
		"agent ue-assets is written as unreagent-ue-assets, the name skill ue-assets is materialized under",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("nameClashes = %q, want %q", got, want)
	}
}

// checkCut holds the agents against the skill set and returns what is wrong,
// one line per problem: an agent naming a skill that is not shipped (Claude
// Code skips it silently), an agent without a shared skill, a pre-load over
// budget, a shipped skill that reaches no agent, and a list entry gone stale.
func checkCut(agents []Agent, all []Skill, onDemand, mainOnly map[string]string, budget int) []string {
	var problems []string
	size := map[string]int{}
	for _, s := range all {
		size[s.Name] = len(s.Source)
	}
	byName := map[string]Agent{}
	preloaded := map[string]bool{}
	for _, a := range agents {
		byName[a.Name] = a
		has := map[string]bool{}
		total := 0
		for _, s := range a.Skills {
			if _, ok := size[s]; !ok {
				problems = append(problems, fmt.Sprintf("agent %s names skill %s, which is not shipped", a.Name, s))
				continue
			}
			has[s], preloaded[s] = true, true
			total += size[s]
		}
		for _, s := range sharedSkills {
			if !has[s] {
				problems = append(problems, fmt.Sprintf("agent %s lacks the shared skill %s", a.Name, s))
			}
		}
		if total > budget {
			problems = append(problems, fmt.Sprintf("agent %s pre-loads %d bytes of SKILL.md, over the budget of %d", a.Name, total, budget))
		}
	}
	for _, s := range all {
		agent, later := onDemand[s.Name]
		_, main := mainOnly[s.Name]
		switch {
		case preloaded[s.Name] && (later || main):
			problems = append(problems, fmt.Sprintf("skill %s is pre-loaded by an agent and still on the on-demand or main-session-only list", s.Name))
		case later && main:
			problems = append(problems, fmt.Sprintf("skill %s is on both the on-demand and the main-session-only list", s.Name))
		case later:
			if a, ok := byName[agent]; !ok {
				problems = append(problems, fmt.Sprintf("skill %s is on demand for agent %s, which does not exist", s.Name, agent))
			} else if !strings.Contains(a.Body, "`"+s.Name+"`") {
				problems = append(problems, fmt.Sprintf("skill %s is on demand for agent %s, whose body does not name it", s.Name, agent))
			}
		case !preloaded[s.Name] && !main:
			problems = append(problems, fmt.Sprintf("skill %s is in no agent and on neither list", s.Name))
		}
	}
	for _, list := range []map[string]string{onDemand, mainOnly} {
		for name := range list {
			if _, ok := size[name]; !ok {
				problems = append(problems, fmt.Sprintf("listed skill %s is not shipped", name))
			}
		}
	}
	return problems
}

// The shipped agents against the shipped skills. Each agent's pre-load is
// logged (go test -v), skill by skill, so growth shows before it hits the
// budget.
func TestShippedAgentsFitTheShippedSkills(t *testing.T) {
	set, err := LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	all, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range checkCut(set.Agents, all, onDemandSkills, mainSessionOnlySkills, preloadBudget) {
		t.Error(p)
	}
	for name, why := range mainSessionOnlySkills {
		if strings.TrimSpace(why) == "" {
			t.Errorf("main-session-only skill %s has no reason", name)
		}
	}
	size := map[string]int{}
	for _, s := range all {
		size[s.Name] = len(s.Source)
	}
	for _, a := range set.Agents {
		total, parts := 0, []string{}
		for _, s := range a.Skills {
			total += size[s]
			parts = append(parts, fmt.Sprintf("%s %d", s, size[s]))
		}
		t.Logf("%-13s %6d of %d bytes: %s", a.Name, total, preloadBudget, strings.Join(parts, ", "))
	}
}

// checkCut must be able to fail: one case per problem it exists to catch,
// each a single change to a cut that passes.
func TestCheckCutCatchesEachProblem(t *testing.T) {
	sk := func(name string, n int) Skill { return Skill{Name: name, Source: strings.Repeat("x", n)} }
	all := []Skill{sk("ue-context", 10), sk("ue-a", 40), sk("ue-b", 40), sk("ue-later", 40), sk("ue-main", 40)}
	agents := func() []Agent {
		return []Agent{
			{Name: "one", Skills: []string{"ue-context", "ue-a"}, Body: "Load `ue-later` on demand."},
			{Name: "two", Skills: []string{"ue-context", "ue-b"}, Body: "Two."},
		}
	}
	onDemand := map[string]string{"ue-later": "one"}
	mainOnly := map[string]string{"ue-main": "only the main session plans"}
	const budget = 60

	if p := checkCut(agents(), all, onDemand, mainOnly, budget); len(p) != 0 {
		t.Fatalf("the passing cut has problems: %q", p)
	}

	cases := []struct {
		name   string
		change func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string)
		want   string
	}{
		{"an agent names a skill that is not shipped", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			a[0].Skills = append(a[0].Skills, "ue-typo")
			return a, all, onDemand, mainOnly
		}, "agent one names skill ue-typo, which is not shipped"},
		{"an agent lacks the shared skill", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			a[1].Skills = []string{"ue-b"}
			return a, all, onDemand, mainOnly
		}, "agent two lacks the shared skill ue-context"},
		{"a skill grows past the budget", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			grown := append([]Skill{}, all...)
			grown[1] = sk("ue-a", 51)
			return a, grown, onDemand, mainOnly
		}, "agent one pre-loads 61 bytes of SKILL.md, over the budget of 60"},
		{"an agent gains a skill past the budget", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			a[1].Skills = append(a[1].Skills, "ue-a")
			return a, all, onDemand, mainOnly
		}, "agent two pre-loads 90 bytes"},
		{"a new skill reaches no agent", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			return a, append(append([]Skill{}, all...), sk("ue-new", 5)), onDemand, mainOnly
		}, "skill ue-new is in no agent and on neither list"},
		{"a skill dropped from its only agent", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			a[1].Skills = []string{"ue-context"}
			return a, all, onDemand, mainOnly
		}, "skill ue-b is in no agent and on neither list"},
		{"the on-demand agent no longer names the skill", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			a[0].Body = "One."
			return a, all, onDemand, mainOnly
		}, "skill ue-later is on demand for agent one, whose body does not name it"},
		{"the on-demand agent is gone", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			return a, all, map[string]string{"ue-later": "three"}, mainOnly
		}, "agent three, which does not exist"},
		{"a listed skill became pre-loaded", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			return a, all, onDemand, map[string]string{"ue-main": "x", "ue-a": "x"}
		}, "skill ue-a is pre-loaded by an agent and still on"},
		{"a listed skill is no longer shipped", func(a []Agent) ([]Agent, []Skill, map[string]string, map[string]string) {
			return a, all, onDemand, map[string]string{"ue-main": "x", "ue-gone": "x"}
		}, "listed skill ue-gone is not shipped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, sk, later, main := c.change(agents())
			got := strings.Join(checkCut(a, sk, later, main, budget), "\n")
			if !strings.Contains(got, c.want) {
				t.Errorf("checkCut reported %q, want a line saying %q", got, c.want)
			}
		})
	}
}
