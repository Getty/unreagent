// Tests for writing the shipped topic agents and the rules file next to the
// materialized skills (k259): where they go, the ownership rule (the hash
// marker), the sweep of dropped agents, and the two ways to write none.
package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/skills"
)

// claudeProject is a project root with skills.dir at <root>/.claude/skills,
// the only place agents are written next to.
func claudeProject(t *testing.T) (claude, skillsDir string) {
	t.Helper()
	claude = filepath.Join(t.TempDir(), ".claude")
	return claude, filepath.Join(claude, "skills")
}

// agentsConfig materializes the skills into dir with skills.agents as given.
func agentsConfig(dir string, agents bool) *config.Config {
	s := skillsVia(false, true, dir)
	s.Agents = ptr(agents)
	return &config.Config{Skills: s}
}

func shippedAgentSet(t *testing.T) skills.AgentSet {
	t.Helper()
	set, err := skills.LoadAgents()
	if err != nil {
		t.Fatalf("skills.LoadAgents: %v", err)
	}
	return set
}

// useAgentSet makes setupSkills write set instead of the embedded one.
func useAgentSet(t *testing.T, set skills.AgentSet) {
	t.Helper()
	saved := loadEmbeddedAgents
	loadEmbeddedAgents = func() (skills.AgentSet, error) { return set, nil }
	t.Cleanup(func() { loadEmbeddedAgents = saved })
}

func agentPath(claude, name string) string {
	return filepath.Join(claude, "agents", "unreagent-"+name+".md")
}

func rulesPath(claude string) string { return filepath.Join(claude, "rules", "unreagent.md") }

func start(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	logger, lines := newLogCollector()
	if _, err := setupSkills(cfg, t.TempDir(), logger); err != nil {
		t.Fatalf("setupSkills: %v", err)
	}
	return *lines
}

// warnLines are the log lines that carry WARN.
func warnLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.Contains(l, "WARN") {
			out = append(out, l)
		}
	}
	return out
}

// writtenAgent is the frontmatter of a written agent file.
type writtenAgent struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Skills      []string `yaml:"skills"`
	Hash        string   `yaml:"unreagent-hash"`
}

func readAgentFile(t *testing.T, path string) (writtenAgent, string) {
	t.Helper()
	src := mustReadFile(t, path)
	rest, ok := strings.CutPrefix(src, "---\n")
	if !ok {
		t.Fatalf("%s does not start with a frontmatter line:\n%s", path, src)
	}
	front, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		t.Fatalf("%s has no closing frontmatter line:\n%s", path, src)
	}
	var fm writtenAgent
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		t.Fatalf("%s: frontmatter: %v\n%s", path, err, front)
	}
	return fm, strings.TrimLeft(body, "\n")
}

// The first start writes every shipped agent under its prefixed name, with
// its skills under their materialized names — each of which exists as a
// skill directory after the same start — its body verbatim and a valid
// marker, and the rules file that names the prefixed agents.
func TestAgentsWrittenOnFirstStart(t *testing.T) {
	claude, dir := claudeProject(t)
	start(t, agentsConfig(dir, true))
	set := shippedAgentSet(t)
	for _, a := range set.Agents {
		p := agentPath(claude, a.Name)
		fm, body := readAgentFile(t, p)
		if fm.Name != "unreagent-"+a.Name {
			t.Errorf("%s: name %q; want unreagent-%s", p, fm.Name, a.Name)
		}
		if fm.Description != a.Description {
			t.Errorf("%s: description %q; want %q", p, fm.Description, a.Description)
		}
		var want []string
		for _, s := range a.Skills {
			want = append(want, skillDirName(s))
		}
		if !slices.Equal(fm.Skills, want) {
			t.Errorf("%s: skills %v; want %v", p, fm.Skills, want)
		}
		for _, s := range fm.Skills {
			if _, err := os.Stat(filepath.Join(dir, s, skillFile)); err != nil {
				t.Errorf("%s names skill %s, which was not materialized: %v", p, s, err)
			}
		}
		if body != a.Body {
			t.Errorf("%s: body differs from the shipped one", p)
		}
		if fm.Hash == "" || !ownedFile(mustReadFile(t, p), agentMarker) {
			t.Errorf("%s: no valid marker (unreagent-hash %q)", p, fm.Hash)
		}
	}
	rules := mustReadFile(t, rulesPath(claude))
	for _, a := range set.Agents {
		if row := "| " + a.Description + " | `unreagent-" + a.Name + "` |"; !strings.Contains(rules, row) {
			t.Errorf("rules file has no row %q", row)
		}
	}
	if !ownedFile(rules, rulesMarker) {
		t.Errorf("rules file has no valid marker:\n%s", rules)
	}
	if !strings.HasPrefix(rules, "# ") {
		t.Errorf("rules file does not start with its heading; the marker goes last:\n%.80s", rules)
	}
}

// A second start finds every file ours and identical and writes none of them.
func TestAgentsSecondStartLeavesIdenticalFiles(t *testing.T) {
	claude, dir := claudeProject(t)
	cfg := agentsConfig(dir, true)
	start(t, cfg)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	files := []string{rulesPath(claude)}
	for _, a := range shippedAgentSet(t).Agents {
		files = append(files, agentPath(claude, a.Name))
	}
	for _, f := range files {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if w := warnLines(start(t, cfg)); len(w) != 0 {
		t.Errorf("second start warned: %q", w)
	}
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if !fi.ModTime().Equal(old) {
			t.Errorf("%s rewritten on the second start (mtime %v, was %v)", f, fi.ModTime(), old)
		}
	}
}

// A changed shipped definition replaces the unedited file of the earlier
// start, and the rules file with it.
func TestAgentsChangedDefinitionReplacesAnUneditedFile(t *testing.T) {
	claude, dir := claudeProject(t)
	cfg := agentsConfig(dir, true)
	start(t, cfg)
	set := shippedAgentSet(t)
	set.Agents = slices.Clone(set.Agents)
	a := &set.Agents[0]
	a.Description += " Changed."
	a.Body += "\nA changed line.\n"
	useAgentSet(t, set)
	if w := warnLines(start(t, cfg)); len(w) != 0 {
		t.Errorf("start warned: %q", w)
	}
	fm, body := readAgentFile(t, agentPath(claude, a.Name))
	if fm.Description != a.Description || body != a.Body {
		t.Errorf("%s not replaced: description %q, body ends %q", agentPath(claude, a.Name), fm.Description, body[max(0, len(body)-40):])
	}
	if rules := mustReadFile(t, rulesPath(claude)); !strings.Contains(rules, a.Description) || !ownedFile(rules, rulesMarker) {
		t.Errorf("rules file not replaced with the changed description")
	}
}

// A file the user edited after the launcher wrote it — the marker still
// there, its hash no longer matching — is never written, and both kinds are
// named on one WARN line.
func TestAgentsUserEditedFileSurvives(t *testing.T) {
	claude, dir := claudeProject(t)
	cfg := agentsConfig(dir, true)
	start(t, cfg)
	a := shippedAgentSet(t).Agents[0]
	edited := map[string]string{
		agentPath(claude, a.Name): mustReadFile(t, agentPath(claude, a.Name)) + "\nMy own rule.\n",
		rulesPath(claude):         strings.Replace(mustReadFile(t, rulesPath(claude)), "# unreagent", "# unreagent, edited", 1),
	}
	for p, s := range edited {
		mustWriteFile(t, p, s)
	}
	w := warnLines(start(t, cfg))
	for p, s := range edited {
		if got := mustReadFile(t, p); got != s {
			t.Errorf("%s: the user's edit was overwritten", p)
		}
	}
	if len(w) != 1 {
		t.Fatalf("WARN lines %q; want one naming both files", w)
	}
	for p := range edited {
		if !strings.Contains(w[0], p) {
			t.Errorf("WARN %q does not name %s", w[0], p)
		}
	}
}

// A file without the marker is foreign: at a shipped agent's name it is
// left alone and named in the WARN; at any other unreagent-*.md name — this
// repository's own team agents are named so — it is neither touched nor
// swept, and not warned about either.
func TestAgentsForeignFilesSurvive(t *testing.T) {
	claude, dir := claudeProject(t)
	a := shippedAgentSet(t).Agents[0]
	foreign := map[string]string{
		agentPath(claude, a.Name): "---\nname: unreagent-" + a.Name + "\ndescription: mine\n---\n\nMine.\n",
		agentPath(claude, "foo"):  "---\nname: unreagent-foo\ndescription: a team agent\n---\n\nTeam.\n",
		// A marker in the right form whose hash matches nothing.
		agentPath(claude, "bar"): "---\nname: unreagent-bar\nunreagent-hash: sha256:" + strings.Repeat("0", 64) + "\n---\n\nBar.\n",
	}
	for p, s := range foreign {
		mustWriteFile(t, p, s)
	}
	w := warnLines(start(t, agentsConfig(dir, true)))
	for p, s := range foreign {
		if got, err := os.ReadFile(p); err != nil || string(got) != s {
			t.Errorf("%s: foreign file changed or removed (%v)", p, err)
		}
	}
	if len(w) != 1 || !strings.Contains(w[0], agentPath(claude, a.Name)) {
		t.Fatalf("WARN lines %q; want one naming %s", w, agentPath(claude, a.Name))
	}
	for _, n := range []string{"foo", "bar"} {
		if strings.Contains(w[0], agentPath(claude, n)) {
			t.Errorf("WARN %q names %s, which is no shipped agent's file", w[0], agentPath(claude, n))
		}
	}
}

// The file of an agent no longer shipped, written by an earlier start and
// unedited, is removed; the rest stay.
func TestAgentsDroppedAgentIsSwept(t *testing.T) {
	claude, dir := claudeProject(t)
	cfg := agentsConfig(dir, true)
	start(t, cfg)
	set := shippedAgentSet(t)
	dropped := set.Agents[0]
	set.Agents = slices.Clone(set.Agents[1:])
	useAgentSet(t, set)
	if w := warnLines(start(t, cfg)); len(w) != 0 {
		t.Errorf("start warned: %q", w)
	}
	if _, err := os.Lstat(agentPath(claude, dropped.Name)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s still there (%v); want it swept", agentPath(claude, dropped.Name), err)
	}
	for _, a := range set.Agents {
		if _, err := os.Stat(agentPath(claude, a.Name)); err != nil {
			t.Errorf("%s: %v", agentPath(claude, a.Name), err)
		}
	}
	if rules := mustReadFile(t, rulesPath(claude)); strings.Contains(rules, "`unreagent-"+dropped.Name+"`") {
		t.Errorf("rules file still names the dropped agent")
	}
}

// skills.agents: false writes neither kind and says why; the skills are
// still materialized.
func TestAgentsOffWritesNone(t *testing.T) {
	claude, dir := claudeProject(t)
	lines := start(t, agentsConfig(dir, false))
	for _, sub := range []string{"agents", "rules"} {
		if got := tree(t, filepath.Join(claude, sub)); got != nil {
			t.Errorf("wrote %v into %s; want nothing", got, sub)
		}
	}
	if got := tree(t, dir); len(got) == 0 {
		t.Error("no skills materialized")
	}
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "skills.agents") }) {
		t.Errorf("log %q gives no reason naming skills.agents", lines)
	}
}

// Turning the agents off, like turning materialization off for the skills,
// removes nothing: the files of the earlier start stay as they are.
func TestAgentsTurnedOffLeavesEarlierFiles(t *testing.T) {
	claude, dir := claudeProject(t)
	start(t, agentsConfig(dir, true))
	before := tree(t, claude)
	start(t, agentsConfig(dir, false))
	cfg := agentsConfig(dir, true)
	cfg.Skills.Materialize = false
	cfg.Skills.MCP = ptr(true)
	start(t, cfg)
	if after := tree(t, claude); len(after) != len(before) {
		t.Errorf("%d files under .claude, %d before; want the earlier start's files left", len(after), len(before))
	}
}

// A skills.dir that is not <x>/.claude/skills gets no agents: nothing lands
// beside it, and the log says why.
func TestAgentsOutsideClaudeWritesNone(t *testing.T) {
	for _, rel := range []string{
		"skills",
		filepath.Join(".claude", "my-skills"),
		filepath.Join("claude", "skills"),
		// The match is exact-case: a differently cased directory is not the
		// one Claude Code reads.
		filepath.Join(".Claude", "Skills"),
		filepath.Join(".Claude", "skills"),
		filepath.Join(".claude", "Skills"),
	} {
		root := t.TempDir()
		dir := filepath.Join(root, rel)
		lines := start(t, agentsConfig(dir, true))
		parent := filepath.Dir(dir)
		for _, sub := range []string{"agents", "rules"} {
			if got := tree(t, filepath.Join(parent, sub)); got != nil {
				t.Errorf("%s: wrote %v into %s", rel, got, sub)
			}
		}
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, ".claude") && strings.Contains(l, dir) }) {
			t.Errorf("%s: log %q gives no reason naming the dir and .claude", rel, lines)
		}
	}
}

// The marker is the line the launcher wrote; the hash covers the rest of the
// file. A checkout that turned LF into CRLF is still ours and unedited; a
// second marker line or one changed byte is not.
func TestOwnedFile(t *testing.T) {
	set := shippedAgentSet(t)
	agent := renderAgent(set.Agents[0])
	rules := renderRules(set)
	for _, tc := range []struct {
		name   string
		data   string
		marker markerKind
		want   bool
	}{
		{"agent", agent, agentMarker, true},
		{"rules", rules, rulesMarker, true},
		{"agent CRLF", strings.ReplaceAll(agent, "\n", "\r\n"), agentMarker, true},
		{"agent one byte", strings.Replace(agent, "---\n\n", "---\n\n ", 1), agentMarker, false},
		{"rules one byte", strings.Replace(rules, "# ", "## ", 1), rulesMarker, false},
		{"agent marker doubled", strings.Replace(agent, "\n---\n", "\n"+markerLine(agent, agentMarker)+"\n---\n", 1), agentMarker, false},
		{"rules marker as agent", rules, agentMarker, false},
		{"agent marker as rules", agent, rulesMarker, false},
		{"no marker", set.Agents[0].Body, agentMarker, false},
	} {
		if got := ownedFile(tc.data, tc.marker); got != tc.want {
			t.Errorf("%s: ownedFile = %v; want %v", tc.name, got, tc.want)
		}
	}
}

// markerLine is the marker line of data.
func markerLine(data string, m markerKind) string {
	for _, l := range strings.Split(data, "\n") {
		if m.re.MatchString(l) {
			return l
		}
	}
	return ""
}

// An unedited launcher file the user copied under another unreagent-*.md
// name is not swept: its frontmatter name is not its file name, so it is not
// recognisably the launcher's file under that name. Nor is it warned about.
func TestAgentsRenamedCopySurvives(t *testing.T) {
	claude, dir := claudeProject(t)
	cfg := agentsConfig(dir, true)
	start(t, cfg)
	a := shippedAgentSet(t).Agents[0]
	cp := agentPath(claude, a.Name+"-mine")
	src := mustReadFile(t, agentPath(claude, a.Name))
	mustWriteFile(t, cp, src)
	if w := warnLines(start(t, cfg)); len(w) != 0 {
		t.Errorf("start warned: %q", w)
	}
	if got, err := os.ReadFile(cp); err != nil || string(got) != src {
		t.Errorf("%s: the user's copy was changed or removed (%v)", cp, err)
	}
}

// A symlink at an agent's or the rules file's path is never the launcher's,
// even when its target carries a valid marker: the target is not replaced,
// the link stays, and both are named in the WARN.
func TestAgentsSymlinkIsForeign(t *testing.T) {
	claude, dir := claudeProject(t)
	set := shippedAgentSet(t)
	old := set
	old.Agents = slices.Clone(set.Agents)
	old.Agents[0].Body += "\nAn older line.\n"
	outside := t.TempDir()
	targets := map[string]string{
		agentPath(claude, set.Agents[0].Name): renderAgent(old.Agents[0]),
		rulesPath(claude):                     renderRules(old),
	}
	i := 0
	for link, data := range targets {
		i++
		target := filepath.Join(outside, "target"+string(rune('0'+i))+".md")
		mustWriteFile(t, target, data)
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink: %v", err)
		}
		targets[link] = target + "\x00" + data
	}
	w := warnLines(start(t, agentsConfig(dir, true)))
	for link, v := range targets {
		target, data, _ := strings.Cut(v, "\x00")
		if got := mustReadFile(t, target); got != data {
			t.Errorf("%s: the link's target %s was replaced", link, target)
		}
		if fi, err := os.Lstat(link); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
			t.Errorf("%s is no longer a symlink (%v)", link, err)
		}
		if len(w) != 1 || !strings.Contains(w[0], link) {
			t.Errorf("WARN lines %q; want one naming %s", w, link)
		}
	}
}

// New agent and rules files get the mode the materialized skill files beside
// them get.
func TestAgentsNewFilesModeMatchesSkills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not decide access on Windows")
	}
	claude, dir := claudeProject(t)
	start(t, agentsConfig(dir, true))
	set := shippedAgentSet(t)
	skill, err := os.Stat(filepath.Join(dir, skillDirName(set.Agents[0].Skills[0]), skillFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{agentPath(claude, set.Agents[0].Name), rulesPath(claude)} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != skill.Mode().Perm() {
			t.Errorf("%s: mode %v; want %v like the skill files", p, fi.Mode().Perm(), skill.Mode().Perm())
		}
	}
}

// The sweep looks at plain files only: a symlink at a dropped agent's name
// whose target is an unedited launcher file, and a directory at such a name,
// both stay, and neither is warned about.
func TestAgentsSweepTouchesPlainFilesOnly(t *testing.T) {
	claude, dir := claudeProject(t)
	gone := skills.Agent{Name: "gone", Description: "Dropped.", Body: "Gone.\n"}
	target := filepath.Join(t.TempDir(), "unreagent-gone.md")
	mustWriteFile(t, target, renderAgent(gone))
	link := agentPath(claude, "gone")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	sub := agentPath(claude, "dir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if w := warnLines(start(t, agentsConfig(dir, true))); len(w) != 0 {
		t.Errorf("start warned: %q", w)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("symlink %s swept: %v", link, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("symlink target %s gone: %v", target, err)
	}
	if fi, err := os.Stat(sub); err != nil || !fi.IsDir() {
		t.Errorf("directory %s changed (%v)", sub, err)
	}
}

// One file that cannot be read or written does not stop the others: every
// other agent and the rules file are written, and the WARN names the one.
func TestAgentsOneFailingFileDoesNotStopOthers(t *testing.T) {
	claude, dir := claudeProject(t)
	set := shippedAgentSet(t)
	bad := agentPath(claude, set.Agents[0].Name)
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	w := warnLines(start(t, agentsConfig(dir, true)))
	for _, a := range set.Agents[1:] {
		if !ownedFile(mustReadFile(t, agentPath(claude, a.Name)), agentMarker) {
			t.Errorf("%s not written", agentPath(claude, a.Name))
		}
	}
	if !ownedFile(mustReadFile(t, rulesPath(claude)), rulesMarker) {
		t.Errorf("rules file not written")
	}
	if !slices.ContainsFunc(w, func(l string) bool { return strings.Contains(l, bad) }) {
		t.Errorf("WARN lines %q do not name %s", w, bad)
	}
}

// A dropped agent's file the sweep cannot delete is reported as not removed,
// by path, not as a write failure.
func TestAgentsSweepFailureSaysNotRemoved(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test user cannot delete from")
	}
	claude, dir := claudeProject(t)
	cfg := agentsConfig(dir, true)
	start(t, cfg)
	set := shippedAgentSet(t)
	dropped := set.Agents[0]
	set.Agents = slices.Clone(set.Agents[1:])
	useAgentSet(t, set)
	agentsDir := filepath.Join(claude, "agents")
	if err := os.Chmod(agentsDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agentsDir, 0o755) })
	w := warnLines(start(t, cfg))
	p := agentPath(claude, dropped.Name)
	if len(w) != 1 || !strings.Contains(w[0], "not removed") || !strings.Contains(w[0], p) || strings.Contains(w[0], "not fully written") {
		t.Errorf("WARN lines %q; want one saying %s was not removed", w, p)
	}
}

// A dropped agent's file that vanishes between the listing and the removal —
// another launcher swept it — is no failure and gets no WARN.
func TestAgentsSweepVanishedFileIsQuiet(t *testing.T) {
	_, dir := claudeProject(t)
	cfg := agentsConfig(dir, true)
	start(t, cfg)
	set := shippedAgentSet(t)
	set.Agents = slices.Clone(set.Agents[1:])
	useAgentSet(t, set)
	saved := agentSweepHook
	agentSweepHook = func(p string) { _ = os.Remove(p) }
	t.Cleanup(func() { agentSweepHook = saved })
	if w := warnLines(start(t, cfg)); len(w) != 0 {
		t.Errorf("start warned: %q", w)
	}
}
