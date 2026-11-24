package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/skills"
)

// loadEmbeddedAgents is skills.LoadAgents; tests replace it to write a set
// that differs from the shipped one.
var loadEmbeddedAgents = skills.LoadAgents

// rulesFileName is the launcher's rules file in <.claude>/rules.
const rulesFileName = "unreagent.md"

// markerKind is the form of the ownership marker in one kind of file: a line
// of its own that carries the SHA-256 of the rest of the file. A file is the
// launcher's and unedited when it holds exactly one such line and the hash
// matches; anything else is the user's.
type markerKind struct {
	re     *regexp.Regexp // the marker line, the hash in group 1
	format string         // the marker line for a hash, as a Sprintf format
}

var (
	// agentMarker is a frontmatter key of its own, the last one. Claude Code
	// documents that it ignores frontmatter fields it does not know.
	agentMarker = markerKind{regexp.MustCompile(`^unreagent-hash: sha256:([0-9a-f]{64})$`), "unreagent-hash: sha256:%s"}
	// rulesMarker is an HTML comment as the file's last line: a rules file has
	// no frontmatter of its own, and the end keeps line 1 free for one
	// (paths:) should the text ever get it.
	rulesMarker = markerKind{regexp.MustCompile(`^<!-- unreagent-hash: sha256:([0-9a-f]{64}) -->$`), "<!-- unreagent-hash: sha256:%s -->"}
)

// stamp is before + the marker line + after, with the hash taken over
// before + after: exactly what is left of the file when the line is taken out.
func stamp(before, after string, m markerKind) string {
	sum := sha256.Sum256([]byte(before + after))
	return before + fmt.Sprintf(m.format, hex.EncodeToString(sum[:])) + "\n" + after
}

// ownedFile reports whether data is a file the launcher wrote and nobody
// edited since: one marker line, and its hash matches the rest. CRLF line
// ends are read as LF first, so a checkout that converts them (git's autocrlf
// on Windows) does not turn the file into the user's.
func ownedFile(data string, m markerKind) bool {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	var rest strings.Builder
	hash := ""
	for _, l := range strings.SplitAfter(data, "\n") {
		if g := m.re.FindStringSubmatch(strings.TrimSuffix(l, "\n")); g != nil {
			if hash != "" {
				return false
			}
			hash = g[1]
			continue
		}
		rest.WriteString(l)
	}
	sum := sha256.Sum256([]byte(rest.String()))
	return hash != "" && hash == hex.EncodeToString(sum[:])
}

// renderAgent is the agent file for a: the frontmatter rendered from its
// fields — name with the prefix, skills under their materialized names — the
// marker as its last key, then the body as shipped.
func renderAgent(a skills.Agent) string {
	fm := struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Skills      []string `yaml:"skills"`
	}{Name: skillDirName(a.Name), Description: a.Description}
	for _, s := range a.Skills {
		fm.Skills = append(fm.Skills, skillDirName(s))
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	// Three plain fields: encoding cannot fail.
	_ = enc.Encode(fm)
	_ = enc.Close()
	return stamp("---\n"+b.String(), "---\n\n"+a.Body, agentMarker)
}

// renameAgents is the set's agents under the names they are written as.
func renameAgents(set skills.AgentSet) []skills.Agent {
	out := make([]skills.Agent, len(set.Agents))
	for i, a := range set.Agents {
		a.Name = skillDirName(a.Name)
		out[i] = a
	}
	return out
}

// renderRules is the rules file: the set's rules text with the agents under
// their written names, and the marker as the last line.
func renderRules(set skills.AgentSet) string {
	return stamp(strings.TrimRight(set.Rules(renameAgents(set)), "\n")+"\n\n", "", rulesMarker)
}

// claudeDirOf is the .claude directory skillsDir lies in, when skillsDir is
// <x>/.claude/skills — the only layout in which the agents' and the rules
// text's word for where the skills lie (.claude/skills/) is true.
func claudeDirOf(skillsDir string) (string, bool) {
	skillsDir = filepath.Clean(skillsDir)
	parent := filepath.Dir(skillsDir)
	if filepath.Base(skillsDir) != "skills" || filepath.Base(parent) != ".claude" {
		return "", false
	}
	return parent, true
}

// setupAgents writes the shipped topic agents and the rules file next to the
// materialized skills in skillsDir (absolute), when skills.agents is on and
// skillsDir is <x>/.claude/skills; otherwise it writes none and logs why. A
// failure only warns, like a failed materialization. Turned off, it removes
// nothing — as materialization turned off leaves the skill directories. It
// returns the .claude directory and how many files it wrote there.
func setupAgents(s config.SkillsConfig, skillsDir string, logger func(string)) (string, int) {
	if !s.AgentFiles() {
		logger("Agents: not written, skills.agents is false; agent and rules files of an earlier start are left as they are")
		return "", 0
	}
	claude, ok := claudeDirOf(skillsDir)
	if !ok {
		logger("Agents: not written, skills.dir " + skillsDir + " is not <project>/.claude/skills, the only place the agents and the rules file are written next to")
		return "", 0
	}
	set, err := loadEmbeddedAgents()
	if err != nil {
		logger("WARN agents not written: " + err.Error())
		return claude, 0
	}
	r := writeAgentFiles(claude, set)
	logger(fmt.Sprintf("Agents: %d agents and the rules file in %s: %d written, %d unchanged, %d removed", len(set.Agents), claude, r.written, r.unchanged, r.removed))
	if len(r.foreign) > 0 {
		logger("WARN agent files left alone, not written by unreagent or edited since: " + strings.Join(r.foreign, ", ") + " — delete one to have it written again")
	}
	if r.err != nil {
		logger("WARN agent files not fully written in " + claude + ": " + strings.ReplaceAll(r.err.Error(), "\n", "; "))
	}
	if r.sweepErr != nil {
		logger("WARN files of dropped agents not removed from " + filepath.Join(claude, "agents") + ": " + strings.ReplaceAll(r.sweepErr.Error(), "\n", "; "))
	}
	return claude, r.written
}

type agentWriteResult struct {
	written, unchanged, removed int
	foreign                     []string // files in the way that are not ours, by path
	err                         error    // every write failure, joined
	sweepErr                    error    // every failure of the sweep, joined
}

// agentFileName is the value of the name key in the frontmatter of the agent
// file data, or "" when it has none.
func agentFileName(data string) string {
	rest, ok := strings.CutPrefix(strings.ReplaceAll(data, "\r\n", "\n"), "---\n")
	if !ok {
		return ""
	}
	front, _, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return ""
	}
	var fm struct {
		Name string `yaml:"name"`
	}
	if yaml.Unmarshal([]byte(front), &fm) != nil {
		return ""
	}
	return fm.Name
}

// agentFile is one file writeAgentFiles puts in place: where, what, and the
// form of its ownership marker.
type agentFile struct {
	path, data string
	m          markerKind
}

// agentFiles are the files writeAgentFiles puts into claude for set — each
// agent at <claude>/agents/unreagent-<name>.md, the rules file at
// <claude>/rules/unreagent.md — and the agent file names the sweep keeps.
func agentFiles(claude string, set skills.AgentSet) (files []agentFile, keep map[string]bool) {
	agentsDir := filepath.Join(claude, "agents")
	keep = map[string]bool{}
	for _, a := range set.Agents {
		name := skillDirName(a.Name) + ".md"
		files = append(files, agentFile{filepath.Join(agentsDir, name), renderAgent(a), agentMarker})
		keep[name] = true
	}
	files = append(files, agentFile{filepath.Join(claude, "rules", rulesFileName), renderRules(set), rulesMarker})
	return files, keep
}

// agentFileState is what is at the place of one agent or rules file.
type agentFileState int

const (
	fileAbsent  agentFileState = iota // nothing there: written
	fileStale                         // ours and unedited, content differs: replaced
	fileSame                          // ours and the same content: left
	fileForeign                       // not ours or edited, or a symlink: never written
)

// stateOf reads the place of f. A symlink is never ours, whatever its target
// holds; a file is ours when ownedFile says so.
func stateOf(f agentFile) (agentFileState, error) {
	if fi, err := os.Lstat(f.path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return fileForeign, nil
	}
	old, err := os.ReadFile(f.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fileAbsent, nil
	case err != nil:
		return 0, err
	case !ownedFile(string(old), f.m):
		return fileForeign, nil
	case strings.ReplaceAll(string(old), "\r\n", "\n") == f.data:
		return fileSame, nil
	}
	return fileStale, nil
}

// agentPlan is what writeAgentFiles would do in claude for set, without
// doing it: the files it would write (absent), replace (stale), leave as they
// are (same), and those in its way (foreign, by path). err joins what could
// not be read. The sweep of dropped agents is not predicted.
type agentPlan struct {
	absent, stale, same int
	foreign             []string
	err                 error
}

func planAgentFiles(claude string, set skills.AgentSet) agentPlan {
	var p agentPlan
	var errs []error
	files, _ := agentFiles(claude, set)
	for _, f := range files {
		st, err := stateOf(f)
		switch {
		case err != nil:
			errs = append(errs, err)
		case st == fileAbsent:
			p.absent++
		case st == fileStale:
			p.stale++
		case st == fileSame:
			p.same++
		default:
			p.foreign = append(p.foreign, f.path)
		}
	}
	p.err = errors.Join(errs...)
	return p
}

// launcherWroteAgentsIn reports whether claude holds an agent or rules file
// the launcher wrote and nobody edited since (the ownership marker; for an
// agent file also its name, as the sweep checks it). Such a file is left only
// by a start or an init that materialized next to it, so the sweep of
// unreagent-* skill directories has run in skills.dir before.
func launcherWroteAgentsIn(claude string) bool {
	if data, err := os.ReadFile(filepath.Join(claude, "rules", rulesFileName)); err == nil && ownedFile(string(data), rulesMarker) {
		return true
	}
	dir := filepath.Join(claude, "agents")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(n, skillDirPrefix) || !strings.HasSuffix(n, ".md") {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(dir, n)); err == nil && ownedFile(string(data), agentMarker) && agentFileName(string(data))+".md" == n {
			return true
		}
	}
	return false
}

// writeAgentFiles puts each agent at <claude>/agents/unreagent-<name>.md and
// the rules file at <claude>/rules/unreagent.md, then sweeps the agents
// directory. For each file: absent, it is written; ours and unedited
// (ownedFile), it is replaced when its content differs and left when it is
// the same; anything else is the user's and is never written, only listed in
// foreign. A symlink is never ours, whatever its target holds. The sweep
// removes a plain unreagent-*.md file of ours that no shipped agent writes
// any more: a valid marker, and the frontmatter name is the file name, so an
// unedited copy under another name stays; so does a file without a valid
// marker, whatever its name. Every write goes through writeFileAtomic; a new
// file gets 0644 like the materialized skill files.
func writeAgentFiles(claude string, set skills.AgentSet) agentWriteResult {
	var r agentWriteResult
	var errs []error
	agentsDir := filepath.Join(claude, "agents")
	files, keep := agentFiles(claude, set)

	for _, f := range files {
		p := f.path
		st, err := stateOf(f)
		switch {
		case err != nil:
			errs = append(errs, err)
			continue
		case st == fileForeign:
			r.foreign = append(r.foreign, p)
			continue
		case st == fileSame:
			r.unchanged++
			continue
		}
		isNew := st == fileAbsent
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, err := writeFileAtomic(p, []byte(f.data), false); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		if isNew {
			// writeFileAtomic's 0600 is for secrets; these are not.
			if err := os.Chmod(p, 0o644); err != nil {
				errs = append(errs, err)
			}
		}
		r.written++
	}

	r.err = errors.Join(errs...)

	// A file that vanishes between the listing and its removal was swept by
	// another launcher: nothing to report.
	var sweepErrs []error
	entries, err := os.ReadDir(agentsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		sweepErrs = append(sweepErrs, err)
	}
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || keep[n] || !strings.HasPrefix(n, skillDirPrefix) || !strings.HasSuffix(n, ".md") {
			continue
		}
		p := filepath.Join(agentsDir, n)
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			sweepErrs = append(sweepErrs, err)
			continue
		}
		if !ownedFile(string(data), agentMarker) || agentFileName(string(data))+".md" != n {
			continue
		}
		agentSweepHook(p)
		if err := os.Remove(p); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			sweepErrs = append(sweepErrs, err)
			continue
		}
		r.removed++
	}
	r.sweepErr = errors.Join(sweepErrs...)
	return r
}

// agentSweepHook is called by the sweep between reading a dropped agent's
// file and removing it; tests replace it.
var agentSweepHook = func(path string) {}
