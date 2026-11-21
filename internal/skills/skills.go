// Package skills is the embedded skill collection: the UE domain knowledge the
// launcher ships to agents. It is the only component that parses skill files.
//
// The documents themselves live in skills/ at the repository root, because a
// go:embed pattern cannot reach outside its own package directory; that
// package holds nothing but the embed directive and is read only from here.
package skills

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	shipped "github.com/conflict-industries/unreagent/skills"
)

// skillFile is the main document of every skill directory.
const skillFile = "SKILL.md"

// Skill is one shipped skill: the frontmatter of <name>/SKILL.md, the Markdown
// after it, the file itself as embedded, and the skill's sub-documents.
type Skill struct {
	Name          string            // equals the skill's directory name
	Description   string            // when to use the skill, collapsed to one line
	ToolkitTools  []string          // nil when the frontmatter omits toolkit_tools
	UnrealClasses []string          // nil when the frontmatter omits unreal_classes
	Body          string            // SKILL.md after the frontmatter
	Source        string            // SKILL.md byte for byte as embedded, frontmatter included; materialized unchanged for an unreagent-* name, else with name: set to unreagent-<name>
	Files         map[string]string // sub-documents <section>.md by section name; nil when there are none
}

// frontmatter is the complete set of keys a SKILL.md may carry. Unknown keys
// are rejected, so a misspelt toolkit_tools is an error rather than an ignored
// key — and cannot slip past the toolkit-registry lint planned in k18.
type frontmatter struct {
	Name          string   `yaml:"name"`
	Description   string   `yaml:"description"`
	ToolkitTools  []string `yaml:"toolkit_tools"`
	UnrealClasses []string `yaml:"unreal_classes"`
}

// Load parses every embedded skill, in directory-name order. A malformed skill
// is an error naming it — missing or invalid frontmatter, a name that differs
// from its directory, an empty description, body or sub-document, a skill or
// section name starting with "." or "_", a file the layout does not allow.
// Nothing is skipped: a skill that silently vanished would leave the agent
// without the knowledge it was meant to have.
func Load() ([]Skill, error) { return load(shipped.FS) }

// load is Load over any FS laid out as <name>/SKILL.md plus <name>/<section>.md.
func load(fsys fs.FS) ([]Skill, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("skills: %w", err)
	}
	var all []Skill
	for _, e := range entries {
		if !e.IsDir() {
			return nil, fmt.Errorf("skills: %s: not inside a skill directory", e.Name())
		}
		if err := checkName("skill", e.Name()); err != nil {
			return nil, fmt.Errorf("skills: %s: %w", e.Name(), err)
		}
		s, err := loadSkill(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("skills: %s: %w", e.Name(), err)
		}
		all = append(all, s)
	}
	return all, nil
}

func loadSkill(fsys fs.FS, dir string) (Skill, error) {
	src, err := fs.ReadFile(fsys, path.Join(dir, skillFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Skill{}, errors.New("no " + skillFile)
	}
	if err != nil {
		return Skill{}, err
	}
	s, err := parse(dir, string(src))
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", skillFile, err)
	}
	s.Source = string(src)

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return Skill{}, err
	}
	for _, e := range entries {
		name := e.Name()
		if name == skillFile {
			continue
		}
		if e.IsDir() || path.Ext(name) != ".md" {
			return Skill{}, fmt.Errorf("%s: a skill directory holds only %s and <section>.md files", name, skillFile)
		}
		section := strings.TrimSuffix(name, ".md")
		if err := checkName("section", section); err != nil {
			return Skill{}, fmt.Errorf("%s: %w", name, err)
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return Skill{}, err
		}
		if strings.TrimSpace(string(data)) == "" {
			return Skill{}, fmt.Errorf("%s is empty", name)
		}
		if s.Files == nil {
			s.Files = map[string]string{}
		}
		s.Files[section] = string(data)
	}
	return s, nil
}

// checkName rejects an empty skill or section name and one starting with "."
// or "_". Go leaves such names out of a whole-directory embed as hidden or
// draft files, but the explicit */*.md pattern matches them, so the loader
// refuses them instead of shipping a draft.
func checkName(kind, name string) error {
	switch {
	case name == "":
		return fmt.Errorf("empty %s name", kind)
	case name[0] == '.' || name[0] == '_':
		return fmt.Errorf("%s name %q starts with \".\" or \"_\", which marks a hidden or draft file", kind, name)
	}
	return nil
}

// parse reads one SKILL.md whose directory is dir.
func parse(dir, src string) (Skill, error) {
	front, body, err := splitFrontmatter(src)
	if err != nil {
		return Skill{}, err
	}
	var fm frontmatter
	dec := yaml.NewDecoder(strings.NewReader(front))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil && !errors.Is(err, io.EOF) { // EOF: empty frontmatter
		return Skill{}, fmt.Errorf("frontmatter: %w", err)
	}
	// A "..." line, or a "--- " line with content, ends the YAML document;
	// whatever follows is never checked against the closed key set.
	var more yaml.Node
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		const msg = `frontmatter: content after the end of the YAML document (a "..." line, or "---" followed by content, ends it)`
		if err != nil {
			return Skill{}, fmt.Errorf("%s: %w", msg, err)
		}
		return Skill{}, errors.New(msg)
	}
	if fm.Name == "" {
		return Skill{}, errors.New("frontmatter: name is missing")
	}
	// The tool loads by name and materialization writes by name; tying it to
	// the directory keeps names unique and the source of a skill obvious.
	if fm.Name != dir {
		return Skill{}, fmt.Errorf("frontmatter: name %q differs from the directory name", fm.Name)
	}
	desc := strings.Join(strings.Fields(fm.Description), " ")
	if desc == "" {
		return Skill{}, errors.New("frontmatter: description is missing or empty")
	}
	if strings.TrimSpace(body) == "" {
		return Skill{}, errors.New("body is empty")
	}
	return Skill{
		Name:          fm.Name,
		Description:   desc,
		ToolkitTools:  fm.ToolkitTools,
		UnrealClasses: fm.UnrealClasses,
		Body:          body,
	}, nil
}

// splitFrontmatter splits "---\n<yaml>---\n<body>". Both delimiters are lines
// of exactly "---" (a CR before the newline aside), and the opening one is the
// file's first line. A byte-order mark or trailing blanks on a delimiter are
// named as errors rather than tolerated: once materialized, other parsers read
// the same file, so what ships must be the plain form.
func splitFrontmatter(src string) (front, body string, err error) {
	if strings.HasPrefix(src, "\ufeff") {
		return "", "", errors.New("starts with a byte-order mark; save it as UTF-8 without one")
	}
	first, rest, _ := strings.Cut(src, "\n")
	ok, err := delimiter(first, 1)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", errors.New("does not start with a --- frontmatter line")
	}
	for off, n := 0, 2; off < len(rest); n++ {
		line, after, _ := strings.Cut(rest[off:], "\n")
		ok, err := delimiter(line, n)
		if err != nil {
			return "", "", err
		}
		if ok {
			return rest[:off], strings.TrimLeft(after, "\r\n"), nil
		}
		off += len(line) + 1
	}
	return "", "", errors.New("frontmatter has no closing --- line")
}

// delimiter reports whether line, line number n of the file, is a "---"
// delimiter. One that is a delimiter except for trailing blanks is an error.
func delimiter(line string, n int) (bool, error) {
	line = strings.TrimSuffix(line, "\r")
	if line == "---" {
		return true, nil
	}
	if strings.TrimRight(line, " \t") == "---" {
		return false, fmt.Errorf("line %d: --- has trailing whitespace", n)
	}
	return false, nil
}

// DescribeAll renders the selection list the skills MCP tool carries as its
// description: one line per skill, "- <name>: <description>", in the order
// given. It is resident in the agent's context on every request.
func DescribeAll(all []Skill) string {
	var b strings.Builder
	for _, s := range all {
		b.WriteString("- " + s.Name + ": " + s.Description + "\n")
	}
	return b.String()
}
