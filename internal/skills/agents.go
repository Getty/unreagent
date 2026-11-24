package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	shippedAgents "github.com/conflict-industries/unreagent/agents"
)

// MaxAgentSkills is the most skills one agent definition may name: Claude
// Code pre-loads up to 32 and documents nothing about the rest.
const MaxAgentSkills = 32

// The kickstart block of the starter CLAUDE.md lies between these two lines,
// both included. They are HTML comments so the rendered file does not show
// them; the block's own text tells the agent to remove it.
const (
	KickstartBegin = "<!-- unreagent:kickstart:begin -->"
	KickstartEnd   = "<!-- unreagent:kickstart:end -->"
)

const (
	projectDir  = "project"    // the directory of the two project texts
	rulesFile   = "rules.md"   // the launcher-owned rules file, with agentsMark
	starterFile = "starter.md" // the CLAUDE.md a project without one starts with

	// agentsMark is the line of rules.md that AgentTable's output replaces.
	agentsMark = "{{agents}}"
)

// agentName is the form Claude Code documents for an agent's name: lowercase
// letters and hyphens. Digits are let through as well.
var agentName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// Agent is one shipped topic agent: the frontmatter of <name>.md and the
// Markdown after it, the agent's standing instructions.
type Agent struct {
	Name        string   // equals the file name without .md
	Description string   // what a main agent routes on, collapsed to one line
	Skills      []string // the skills pre-loaded at spawn, by their shipped names
	Body        string   // the file after the frontmatter
}

// agentFrontmatter is the complete set of keys an agent definition may carry.
// All three are documented Claude Code subagent fields; tools and model are
// left out on purpose, so the agent inherits both.
type agentFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Skills      []string `yaml:"skills"`
}

// AgentSet is everything the launcher ships for a project's agent setup.
type AgentSet struct {
	Agents  []Agent // in file-name order
	Starter string  // the starter CLAUDE.md, as embedded
	rules   string  // rules.md as embedded, agentsMark still in it
}

// LoadAgents parses every embedded agent definition and the two project
// texts. It is as strict as Load: one malformed file is an error naming it,
// and nothing is skipped. It does not know the skill set — that an agent's
// skills exist is checked by the package's tests.
func LoadAgents() (AgentSet, error) { return loadAgents(shippedAgents.FS) }

// loadAgents is LoadAgents over any FS laid out as <name>.md plus
// project/rules.md and project/starter.md.
func loadAgents(fsys fs.FS) (AgentSet, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return AgentSet{}, fmt.Errorf("agents: %w", err)
	}
	var set AgentSet
	described := map[string]string{} // description -> agent
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if name != projectDir {
				return AgentSet{}, fmt.Errorf("agents: %s: the only directory is %s", name, projectDir)
			}
			continue
		}
		a, err := loadAgent(fsys, name)
		if err != nil {
			return AgentSet{}, fmt.Errorf("agents: %s: %w", name, err)
		}
		if other, ok := described[a.Description]; ok {
			return AgentSet{}, fmt.Errorf("agents: %s: same description as %s, so nothing tells the two apart", name, other)
		}
		described[a.Description] = a.Name
		set.Agents = append(set.Agents, a)
	}
	if len(set.Agents) == 0 {
		return AgentSet{}, errors.New("agents: no agent definition")
	}
	if set.rules, set.Starter, err = loadProjectTexts(fsys); err != nil {
		return AgentSet{}, fmt.Errorf("agents: %w", err)
	}
	return set, nil
}

func loadAgent(fsys fs.FS, file string) (Agent, error) {
	if path.Ext(file) != ".md" {
		return Agent{}, errors.New("an agent definition is <name>.md")
	}
	stem := strings.TrimSuffix(file, ".md")
	if err := checkName("agent", stem); err != nil {
		return Agent{}, err
	}
	src, err := fs.ReadFile(fsys, file)
	if err != nil {
		return Agent{}, err
	}
	front, body, err := splitFrontmatter(string(src))
	if err != nil {
		return Agent{}, err
	}
	var fm agentFrontmatter
	if err := decodeFrontmatter(front, &fm); err != nil {
		return Agent{}, err
	}
	if fm.Name == "" {
		return Agent{}, errors.New("frontmatter: name is missing")
	}
	// The file name is what keeps two agents from sharing a name.
	if fm.Name != stem {
		return Agent{}, fmt.Errorf("frontmatter: name %q differs from the file name", fm.Name)
	}
	if !agentName.MatchString(fm.Name) {
		return Agent{}, fmt.Errorf("frontmatter: name %q: an agent name is lowercase letters, digits and single hyphens, starting with a letter", fm.Name)
	}
	desc := strings.Join(strings.Fields(fm.Description), " ")
	if desc == "" {
		return Agent{}, errors.New("frontmatter: description is missing or empty")
	}
	if strings.Contains(desc, "|") {
		return Agent{}, errors.New(`frontmatter: description holds a "|", which would break the agent table`)
	}
	if len(fm.Skills) == 0 {
		return Agent{}, errors.New("frontmatter: skills is missing or empty")
	}
	if len(fm.Skills) > MaxAgentSkills {
		return Agent{}, fmt.Errorf("frontmatter: %d skills, more than the %d an agent may name", len(fm.Skills), MaxAgentSkills)
	}
	seen := map[string]bool{}
	for _, s := range fm.Skills {
		if s == "" || strings.TrimSpace(s) != s {
			return Agent{}, fmt.Errorf("frontmatter: skills holds %q, an empty or padded name", s)
		}
		if seen[s] {
			return Agent{}, fmt.Errorf("frontmatter: skills names %s twice", s)
		}
		seen[s] = true
	}
	if strings.TrimSpace(body) == "" {
		return Agent{}, errors.New("body is empty")
	}
	return Agent{Name: fm.Name, Description: desc, Skills: fm.Skills, Body: body}, nil
}

// loadProjectTexts reads project/rules.md and project/starter.md, which must
// be the only two entries there.
func loadProjectTexts(fsys fs.FS) (rules, starter string, err error) {
	entries, err := fs.ReadDir(fsys, projectDir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", fmt.Errorf("no %s directory", projectDir)
	}
	if err != nil {
		return "", "", err
	}
	texts := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (name != rulesFile && name != starterFile) {
			return "", "", fmt.Errorf("%s/%s: %s holds only %s and %s", projectDir, name, projectDir, rulesFile, starterFile)
		}
		data, err := fs.ReadFile(fsys, path.Join(projectDir, name))
		if err != nil {
			return "", "", err
		}
		texts[name] = string(data)
	}
	for _, name := range []string{rulesFile, starterFile} {
		if _, ok := texts[name]; !ok {
			return "", "", fmt.Errorf("no %s/%s", projectDir, name)
		}
	}
	rules, starter = texts[rulesFile], texts[starterFile]
	if n := countLines(rules, agentsMark); n != 1 {
		return "", "", fmt.Errorf("%s/%s: %d lines of exactly %s, want one: the agent table goes there", projectDir, rulesFile, n, agentsMark)
	}
	if strings.TrimSpace(strings.Replace(rules, agentsMark, "", 1)) == "" {
		return "", "", fmt.Errorf("%s/%s: nothing but the agent table", projectDir, rulesFile)
	}
	if err := checkStarter(starter); err != nil {
		return "", "", fmt.Errorf("%s/%s: %w", projectDir, starterFile, err)
	}
	return rules, starter, nil
}

// checkStarter holds the starter to its shape: one kickstart block between
// the two marker lines, with text in it, and the project part after it.
func checkStarter(starter string) error {
	if b, e := countLines(starter, KickstartBegin), countLines(starter, KickstartEnd); b != 1 || e != 1 {
		return fmt.Errorf("%d begin and %d end lines of the kickstart block, want one of each, each alone on its line", b, e)
	}
	_, after, _ := strings.Cut(starter, KickstartBegin)
	block, rest, ok := strings.Cut(after, KickstartEnd)
	if !ok {
		return errors.New("the kickstart block ends before it begins")
	}
	if strings.TrimSpace(block) == "" {
		return errors.New("the kickstart block is empty")
	}
	if strings.TrimSpace(rest) == "" {
		return errors.New("nothing after the kickstart block: the project part is missing")
	}
	return nil
}

// countLines counts the lines of text that are exactly line (a CR aside).
func countLines(text, line string) int {
	n := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSuffix(l, "\r") == line {
			n++
		}
	}
	return n
}

// Rules renders the launcher-owned rules file: the fixed launcher facts, with
// the agent table for agents where the embedded text marks it. The table
// shows each Name as given, so a caller that writes the agents under other
// names passes them renamed. The table is one line per agent, so the text
// stays cheap if subagents carry it too. Unverified: Claude Code documents
// that a subagent loads CLAUDE.md, and says nothing of rules files.
func (s AgentSet) Rules(agents []Agent) string {
	lines := strings.Split(s.rules, "\n")
	for i, l := range lines {
		if strings.TrimSuffix(l, "\r") == agentsMark {
			lines[i] = strings.TrimSuffix(AgentTable(agents), "\n")
		}
	}
	return strings.Join(lines, "\n")
}

// AgentTable renders the task-to-agent table, one row per agent in the order
// given: the description is the task.
func AgentTable(agents []Agent) string {
	var b strings.Builder
	b.WriteString("| Task | Agent |\n|---|---|\n")
	for _, a := range agents {
		b.WriteString("| " + a.Description + " | `" + a.Name + "` |\n")
	}
	return b.String()
}
