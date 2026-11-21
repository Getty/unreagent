// Tests for the skills delivery paths (k10–k12; acceptance of k15–k17): the
// skills MCP tool driven through JSON-RPC tools/list and tools/call, and
// materialization into a real temp directory.
package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/skills"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

func ptr(b bool) *bool { return &b }

// skillsVia is a skills section with skills enabled and the given paths on.
func skillsVia(viaMCP, viaFiles bool, dir string) config.SkillsConfig {
	return config.SkillsConfig{Enabled: ptr(true), MCP: ptr(viaMCP), Materialize: viaFiles, Dir: dir}
}

// shippedSkills is the embedded set, as the launcher loads it.
func shippedSkills(t *testing.T) []skills.Skill {
	t.Helper()
	all, err := skills.Load()
	if err != nil {
		t.Fatalf("skills.Load: %v", err)
	}
	return all
}

// testSkills is a hand-built set: one name that carries the unreagent- prefix,
// one that does not and has sub-documents, which no shipped skill has yet.
func testSkills() []skills.Skill {
	return []skills.Skill{
		{
			Name: "unreagent-alpha", Description: "Use when testing alpha.", Body: "# alpha\n",
			Source: "---\nname: unreagent-alpha\ndescription: Use when testing alpha.\n---\n\n# alpha\n",
		},
		{
			Name: "ue-beta", Description: "Use when testing beta.", Body: "# beta\n\nSee nodes.md.\n",
			Source: "---\nname: ue-beta\ndescription: >\n  Use when testing beta.\n---\n\n# beta\n\nSee nodes.md.\n",
			Files:  map[string]string{"nodes": "# beta nodes\n", "graphs": "# beta graphs\n"},
		},
	}
}

// skillsServer registers the real tool set (registerTools) with the given
// skills section and skill set.
func skillsServer(t *testing.T, s config.SkillsConfig, all []skills.Skill) *mcp.Server {
	t.Helper()
	srv := mcp.NewServer("skills-test", "0", nil)
	registerTools(srv, supervisor.New(nil), &config.Config{Skills: s}, t.TempDir(), all, func(string) {})
	return srv
}

// toolDescriptions drives tools/list and returns each tool's description.
func toolDescriptions(t *testing.T, srv *mcp.Server) map[string]string {
	t.Helper()
	rec := postJSONRPC(t, srv, map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	var resp struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal tools/list: %v\nbody: %s", err, rec.Body.String())
	}
	out := map[string]string{}
	for _, tl := range resp.Result.Tools {
		out[tl.Name] = tl.Description
	}
	return out
}

// tree returns every file under dir, by slash path relative to dir, with its
// content; nil when dir does not exist.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = mustReadFile(t, p)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

// materialized is the tree materializeSkills must leave for all: SKILL.md as
// embedded and every sub-document, in the skill's directory. In a directory
// that adds the prefix, the frontmatter's "name: <name>" line (the line after
// the opening ---, in every skill this is used on) names the directory.
func materialized(all []skills.Skill) map[string]string {
	out := map[string]string{}
	for _, s := range all {
		d := skillDirName(s.Name)
		out[d+"/SKILL.md"] = strings.Replace(s.Source, "---\nname: "+s.Name+"\n", "---\nname: "+d+"\n", 1)
		for section, doc := range s.Files {
			out[d+"/"+section+".md"] = doc
		}
	}
	return out
}

// --- k11 / k16: the skills tool ------------------------------------------

// A known name returns that skill's body, for every skill — over the shipped
// set as well as over the hand-built one.
func TestSkillsToolReturnsTheSkillBody(t *testing.T) {
	for _, all := range [][]skills.Skill{testSkills(), shippedSkills(t)} {
		srv := skillsServer(t, skillsVia(true, false, ""), all)
		for _, s := range all {
			res := callTool(t, srv, "skills", map[string]interface{}{"name": s.Name})
			if res.IsError || res.Text != s.Body {
				t.Errorf("skills name=%s: IsError %v, text %q; want the body %q", s.Name, res.IsError, res.Text, s.Body)
			}
		}
	}
}

// An unknown or missing name is an error result that names every available
// skill: that error is the only list there is, besides the description.
func TestSkillsToolUnknownNameListsTheSkills(t *testing.T) {
	all := testSkills()
	srv := skillsServer(t, skillsVia(true, false, ""), all)
	for _, args := range []map[string]interface{}{
		{"name": "ue-gamma"},
		{"name": "beta"},              // no prefix matching: names are exact
		{"name": "unreagent-ue-beta"}, // the directory name is not the skill name
		{},
		{"name": 7},
		{"section": "nodes"},
	} {
		res := callTool(t, srv, "skills", args)
		if !res.IsError {
			t.Errorf("skills %v: IsError = false (text %q); want an error", args, res.Text)
			continue
		}
		for _, s := range all {
			if !strings.Contains(res.Text, s.Name) {
				t.Errorf("skills %v: error %q does not name the available skill %s", args, res.Text, s.Name)
			}
		}
	}
}

// section loads a sub-document, given as the skill names it, with or without
// .md; an unknown section is an error naming the sections there are.
func TestSkillsToolSections(t *testing.T) {
	all := testSkills()
	beta := all[1]
	srv := skillsServer(t, skillsVia(true, false, ""), all)
	for section, want := range map[string]string{
		"nodes":    beta.Files["nodes"],
		"nodes.md": beta.Files["nodes"],
		"graphs":   beta.Files["graphs"],
		"":         beta.Body, // no section: the skill itself
	} {
		res := callTool(t, srv, "skills", map[string]interface{}{"name": beta.Name, "section": section})
		if res.IsError || res.Text != want {
			t.Errorf("skills %s section=%q: IsError %v, text %q; want %q", beta.Name, section, res.IsError, res.Text, want)
		}
	}

	res := callTool(t, srv, "skills", map[string]interface{}{"name": beta.Name, "section": "edges"})
	if !res.IsError {
		t.Fatalf("unknown section: IsError = false (text %q); want an error", res.Text)
	}
	for _, w := range []string{"edges", "nodes", "graphs"} {
		if !strings.Contains(res.Text, w) {
			t.Errorf("unknown section: error %q does not mention %q", res.Text, w)
		}
	}
	res = callTool(t, srv, "skills", map[string]interface{}{"name": "unreagent-alpha", "section": "nodes"})
	if !res.IsError || !strings.Contains(res.Text, "no sections") {
		t.Errorf("section of a skill without sections: IsError %v, text %q; want an error saying it has no sections", res.IsError, res.Text)
	}
}

// The tool is registered only while skills are enabled and served over MCP.
func TestSkillsToolRegisteredOnlyWithSkillsAndMCPOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    config.SkillsConfig
		want bool
	}{
		{"enabled, mcp", skillsVia(true, false, ""), true},
		{"enabled, mcp and materialize", skillsVia(true, true, "x"), true},
		{"enabled, materialize only", skillsVia(false, true, "x"), false},
		{"disabled", config.SkillsConfig{Enabled: ptr(false), MCP: ptr(true)}, false},
	} {
		_, got := listTools(t, skillsServer(t, tc.s, testSkills()))["skills"]
		if got != tc.want {
			t.Errorf("%s: skills tool registered = %v; want %v", tc.name, got, tc.want)
		}
	}
}

// The description carries every skill's selection line (DescribeAll), so one
// call is enough; the schema has name (required) and section, and no list
// action — the description is the list.
func TestSkillsToolDescriptionAndSchema(t *testing.T) {
	all := shippedSkills(t)
	srv := skillsServer(t, skillsVia(true, false, ""), all)
	desc := toolDescriptions(t, srv)["skills"]
	if !strings.Contains(desc, skills.DescribeAll(all)) {
		t.Errorf("skills description does not carry DescribeAll of the set:\n%s", desc)
	}
	schema := listTools(t, srv)["skills"]
	props, _ := schema["properties"].(map[string]interface{})
	if got := slices.Sorted(maps.Keys(props)); !slices.Equal(got, []string{"name", "section"}) {
		t.Errorf("skills parameters = %v; want [name section]", got)
	}
	if req, _ := schema["required"].([]interface{}); len(req) != 1 || req[0] != "name" {
		t.Errorf("skills required = %v; want [name]", schema["required"])
	}
}

// --- k12 / k17: materialization -------------------------------------------

// The unreagent- prefix is added to a name without it and never doubled.
func TestSkillDirName(t *testing.T) {
	for name, want := range map[string]string{
		"unreagent-files": "unreagent-files",
		"ue-materials":    "unreagent-ue-materials",
		"unreagent":       "unreagent-unreagent",
		"Unreagent-x":     "unreagent-Unreagent-x",
	} {
		if got := skillDirName(name); got != want {
			t.Errorf("skillDirName(%q) = %q; want %q", name, got, want)
		}
	}
}

// Every skill of the set gets its prefixed directory, holding SKILL.md exactly
// as embedded and its sub-documents next to it — and nothing else. The
// directory is created when it does not exist yet.
func TestMaterializeWritesEverySkill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".claude", "skills")
	all := append(shippedSkills(t), testSkills()...)
	if err := materializeSkills(dir, all); err != nil {
		t.Fatal(err)
	}
	got, want := tree(t, dir), materialized(all)
	if !maps.Equal(got, want) {
		t.Errorf("materialized tree:\n%v\nwant:\n%v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
	for _, p := range []string{"unreagent-files/SKILL.md", "unreagent-ue-beta/SKILL.md", "unreagent-ue-beta/nodes.md"} {
		if _, ok := got[p]; !ok {
			t.Errorf("%s not written", p)
		}
	}
}

// A rerun replaces the launcher's own directories: a file that is no longer
// part of a skill goes, the current content replaces the old.
func TestMaterializeReplacesItsOwnDirectories(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "unreagent-alpha", "SKILL.md"), "old")
	mustWriteFile(t, filepath.Join(dir, "unreagent-alpha", "stale.md"), "stale")
	mustWriteFile(t, filepath.Join(dir, "unreagent-ue-beta", "removed-section.md"), "stale")
	mustWriteFile(t, filepath.Join(dir, "unreagent-ue-beta", "assets", "old.png"), "stale")
	all := testSkills()
	for run := 1; run <= 2; run++ {
		if err := materializeSkills(dir, all); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if got := tree(t, dir); !maps.Equal(got, materialized(all)) {
			t.Errorf("run %d: tree = %v; want exactly the current set", run, got)
		}
	}
}

// A prefixed directory that is not in the current set is removed, so a renamed
// or dropped skill does not linger.
func TestMaterializeRemovesDroppedSkills(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "unreagent-dropped", "SKILL.md"), "dropped")
	mustWriteFile(t, filepath.Join(dir, "unreagent-dropped", "nested", "x.md"), "dropped")
	if err := materializeSkills(dir, testSkills()); err != nil {
		t.Fatal(err)
	}
	if exists(t, filepath.Join(dir, "unreagent-dropped")) {
		t.Error("unreagent-dropped survived; want it removed")
	}
}

// Everything without the prefix is the project's own and survives untouched —
// including a directory named exactly like an embedded skill, and a file that
// has the prefix but is not a directory.
func TestMaterializeLeavesForeignEntriesAlone(t *testing.T) {
	dir := t.TempDir()
	foreign := map[string]string{
		"my-skill/SKILL.md":       "mine",
		"my-skill/notes/deep.md":  "deep",
		"ue-beta/SKILL.md":        "the project's own ue-beta",
		"alpha/SKILL.md":          "the project's own alpha",
		"README.md":               "a top-level file",
		"unreagent-notes.md":      "a prefixed file, not a directory",
		"xunreagent-alpha/a.md":   "the prefix not at the start",
		"unreagentalpha/SKILL.md": "the prefix without its dash",
	}
	for p, content := range foreign {
		mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(p)), content)
	}
	all := testSkills()
	if err := materializeSkills(dir, all); err != nil {
		t.Fatal(err)
	}
	want := materialized(all)
	maps.Copy(want, foreign)
	if got := tree(t, dir); !maps.Equal(got, want) {
		t.Errorf("tree = %v\nwant %v", got, want)
	}
}

// Every write stays inside dir: a name that would leave it, or that is not a
// single path element, aborts before anything is removed or written. The names
// carry the prefix, so no rename of the SKILL.md name is attempted, which
// would refuse these hand-built skills for a reason of its own.
func TestMaterializeKeepsEveryWriteInsideDir(t *testing.T) {
	for _, bad := range []skills.Skill{
		{Name: "unreagent-x/../../escape", Source: "x"},
		{Name: "unreagent-a/b", Source: "x"},
		{Name: "unreagent-beta", Source: "x", Files: map[string]string{"../../escape": "x"}},
		{Name: "unreagent-beta", Source: "x", Files: map[string]string{"sub/doc": "x"}},
	} {
		root := t.TempDir()
		dir := filepath.Join(root, "skills")
		mustWriteFile(t, filepath.Join(dir, "unreagent-keep", "SKILL.md"), "keep")
		before := tree(t, root)
		if err := materializeSkills(dir, append(testSkills()[:1], bad)); err == nil {
			t.Errorf("%s %v: materializeSkills succeeded; want an error", bad.Name, slices.Collect(maps.Keys(bad.Files)))
		}
		if got := tree(t, root); !maps.Equal(got, before) {
			t.Errorf("%s %v: the tree changed: %v; want it untouched (%v)", bad.Name, slices.Collect(maps.Keys(bad.Files)), got, before)
		}
	}
}

// Only skills.dir itself is swept: an unreagent-* directory beside it, in its
// parent, is not ours and survives untouched.
func TestMaterializeLeavesTheParentOfDirAlone(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills")
	mustWriteFile(t, filepath.Join(root, "unreagent-x", "SKILL.md"), "beside skills.dir")
	mustWriteFile(t, filepath.Join(root, "unreagent-x", "deep", "a.md"), "deep")
	all := testSkills()
	if err := materializeSkills(dir, all); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"unreagent-x/SKILL.md": "beside skills.dir", "unreagent-x/deep/a.md": "deep"}
	for p, c := range materialized(all) {
		want["skills/"+p] = c
	}
	if got := tree(t, root); !maps.Equal(got, want) {
		t.Errorf("tree = %v\nwant %v", got, want)
	}
}

// A skill materialized into a directory that adds the prefix gets that
// directory as its name — the Agent Skills format wants name to equal the
// parent directory — and nothing else changes: not the rest of the
// frontmatter (quotes, comment, CRLF), not a name: line in the body. A skill
// already named unreagent-* is written byte for byte as embedded.
func TestMaterializeNamesTheSkillLikeItsDirectory(t *testing.T) {
	for _, tc := range []struct {
		name, front, body, want string
	}{
		{
			"ue-x",
			"---\nname: ue-x\ndescription: Use when testing x.\n---\n\n",
			"# x\n\nname: ue-x\n",
			"---\nname: unreagent-ue-x\ndescription: Use when testing x.\n---\n\n# x\n\nname: ue-x\n",
		},
		{
			"ue-y",
			"---\r\ndescription: >\r\n  Use when testing y.\r\nname: 'ue-y'   # the directory name\r\n---\r\n",
			"name: ue-y\r\n",
			"---\r\ndescription: >\r\n  Use when testing y.\r\nname: 'unreagent-ue-y'   # the directory name\r\n---\r\nname: ue-y\r\n",
		},
		{
			"ue-z",
			"---\ndescription: Use when testing z.\nname: \"ue-z\"\n---\n",
			"# z\n---\nname: ue-z\n",
			"---\ndescription: Use when testing z.\nname: \"unreagent-ue-z\"\n---\n# z\n---\nname: ue-z\n",
		},
		{
			"unreagent-w",
			"---\nname:   unreagent-w # kept as it is\ndescription: Use when testing w.\n---\n",
			"# w\nname: unreagent-w\n",
			"---\nname:   unreagent-w # kept as it is\ndescription: Use when testing w.\n---\n# w\nname: unreagent-w\n",
		},
		{
			// A form a rewrite would refuse: never looked at, the name fits.
			"unreagent-v",
			"---\nname: >-\n  unreagent-v\ndescription: Use when testing v.\n---\n",
			"# v\n",
			"---\nname: >-\n  unreagent-v\ndescription: Use when testing v.\n---\n# v\n",
		},
	} {
		dir := t.TempDir()
		s := skills.Skill{Name: tc.name, Description: "d", Body: tc.body, Source: tc.front + tc.body}
		if err := materializeSkills(dir, []skills.Skill{s}); err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got := mustReadFile(t, filepath.Join(dir, skillDirName(tc.name), "SKILL.md")); got != tc.want {
			t.Errorf("%s: SKILL.md =\n%q\nwant\n%q", tc.name, got, tc.want)
		}
	}
}

// A name whose text is not the name itself, plain or quoted (a block scalar,
// an escape — both of which Load accepts), is refused before anything in dir
// changes rather than rewritten by guess; so is a skill whose fields do not
// fit together, which only a hand-built set can be.
func TestMaterializeRefusesANameItCannotRewrite(t *testing.T) {
	const notPlain = "not a plain or quoted scalar"
	for _, tc := range []struct{ why, source, body, want string }{
		{"folded name", "---\nname: >-\n  ue-f\ndescription: d\n---\n# f\n", "# f\n", notPlain},
		{"escaped name", "---\nname: \"ue\\x2df\"\ndescription: d\n---\n# f\n", "# f\n", notPlain},
		{"body not at the end", "---\nname: ue-f\ndescription: d\n---\n# f\n", "# other\n", "body is not the end"},
		{"frontmatter not YAML", "---\nname: [ue-f\n---\n# f\n", "# f\n", "yaml:"},
		{"no name key", "---\ndescription: d\n---\n# f\n", "# f\n", "no name key"},
		{"frontmatter not a mapping", "---\n- name\n- ue-f\n---\n# f\n", "# f\n", "no name key"},
		{"no frontmatter", "# f\n", "# f\n", "no name key"},
	} {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "unreagent-keep", "SKILL.md"), "keep")
		before := tree(t, dir)
		s := skills.Skill{Name: "ue-f", Description: "d", Body: tc.body, Source: tc.source}
		err := materializeSkills(dir, []skills.Skill{s})
		if err == nil || !strings.Contains(err.Error(), "ue-f") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: materializeSkills = %v; want an error naming ue-f and saying %q", tc.why, err, tc.want)
		}
		if got := tree(t, dir); !maps.Equal(got, before) {
			t.Errorf("%s: tree = %v; want it untouched (%v)", tc.why, got, before)
		}
	}
}

// Two skills that land in one directory, or two files of a skill that a
// case-insensitive file system (Windows, macOS) cannot tell apart, are a load
// error: one would overwrite the other.
func TestCheckSkillDirsRejectsCollisions(t *testing.T) {
	sk := func(name string, sections ...string) skills.Skill {
		s := skills.Skill{Name: name, Source: "x", Body: "x"}
		for _, sec := range sections {
			if s.Files == nil {
				s.Files = map[string]string{}
			}
			s.Files[sec] = "x"
		}
		return s
	}
	for _, tc := range []struct {
		name string
		set  []skills.Skill
		want []string
	}{
		{"prefixed and unprefixed name", []skills.Skill{sk("ue-x"), sk("unreagent-ue-x")}, []string{"ue-x", "unreagent-ue-x"}},
		{"names differing in case", []skills.Skill{sk("Foo"), sk("foo")}, []string{"Foo", "foo"}},
		{"prefix and case", []skills.Skill{sk("UE-x"), sk("unreagent-ue-x")}, []string{"UE-x", "unreagent-ue-x"}},
		{"sections differing in case", []skills.Skill{sk("a", "Nodes", "nodes")}, []string{"Nodes.md", "nodes.md"}},
		{"colliding pair not adjacent", []skills.Skill{sk("ue-x"), sk("ue-y"), sk("unreagent-ue-x")}, []string{"ue-x", "unreagent-ue-x"}},
		{"colliding pair not adjacent, last first", []skills.Skill{sk("unreagent-ue-x"), sk("ue-y"), sk("ue-z"), sk("ue-x")}, []string{"ue-x", "unreagent-ue-x"}},
		{"section named like SKILL.md", []skills.Skill{sk("a", "skill")}, []string{"skill.md", "SKILL.md"}},
	} {
		err := checkSkillDirs(tc.set)
		if err == nil {
			t.Errorf("%s: no error", tc.name)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q does not name %s", tc.name, err, w)
			}
		}
	}
	for _, set := range [][]skills.Skill{shippedSkills(t), testSkills(), {sk("ue-x"), sk("unreagent-ue-y"), sk("b", "nodes", "graphs")}} {
		if err := checkSkillDirs(set); err != nil {
			t.Errorf("%d skills: %v; want no collision", len(set), err)
		}
	}
}

// materializeSkills checks for collisions before it touches dir. Both skills
// are well-formed, so nothing but the collision check can refuse them.
func TestMaterializeRefusesCollisionsBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "unreagent-keep", "SKILL.md"), "keep")
	before := tree(t, dir)
	set := []skills.Skill{
		{Name: "ue-x", Description: "d", Body: "a\n", Source: "---\nname: ue-x\ndescription: d\n---\na\n"},
		{Name: "unreagent-ue-x", Description: "d", Body: "b\n", Source: "---\nname: unreagent-ue-x\ndescription: d\n---\nb\n"},
	}
	if err := materializeSkills(dir, set); err == nil {
		t.Fatal("materializeSkills succeeded on a colliding set; want an error")
	}
	if got := tree(t, dir); !maps.Equal(got, before) {
		t.Errorf("tree = %v; want it untouched (%v)", got, before)
	}
}

// --- startup (setupSkills) and the -no-skills-mcp flag -------------------

// With materialize on, the start writes the shipped set into skills.dir; a
// relative dir is taken relative to the agent's working directory, like the
// mcp.writeConfig paths.
func TestSetupSkillsMaterializesTheShippedSet(t *testing.T) {
	work := t.TempDir()
	logger, lines := newLogCollector()
	cfg := &config.Config{Skills: skillsVia(false, true, "rel/skills")}
	all, err := setupSkills(cfg, work, logger)
	if err != nil {
		t.Fatal(err)
	}
	shipped := shippedSkills(t)
	if len(all) != len(shipped) {
		t.Errorf("setupSkills returned %d skills; want the %d shipped", len(all), len(shipped))
	}
	if got := tree(t, filepath.Join(work, "rel", "skills")); !maps.Equal(got, materialized(shipped)) {
		t.Errorf("materialized %v; want the shipped set", slices.Sorted(maps.Keys(got)))
	}
	if !strings.Contains(strings.Join(*lines, "\n"), "materialized") {
		t.Errorf("log %q does not report the materialization", *lines)
	}
}

// A failed write warns and the start goes on, with the set still handed to
// the MCP tool: the editor and the agent matter more than the skill files.
func TestSetupSkillsWarnsWhenMaterializationFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	mustWriteFile(t, blocker, "a file where skills.dir needs a directory")
	dir := filepath.Join(blocker, "skills")
	logger, lines := newLogCollector()
	all, err := setupSkills(&config.Config{Skills: skillsVia(true, true, dir)}, t.TempDir(), logger)
	if err != nil {
		t.Fatalf("setupSkills: %v; want a warning, not an error", err)
	}
	if len(all) == 0 {
		t.Error("setupSkills returned no skills; the MCP tool still needs them")
	}
	// The text claims no more than it knows: a write can fail after the old
	// unreagent-* directories are gone, or a removal before anything is new.
	prefix := "WARN skills not fully written to " + dir + ": "
	const suffix = " — its unreagent-* directories may be missing, incomplete or left from an earlier start"
	var warned bool
	for _, l := range *lines {
		if cause, ok := strings.CutPrefix(l, prefix); ok && strings.HasSuffix(cause, suffix) && len(cause) > len(suffix) {
			warned = true
		}
	}
	if !warned {
		t.Errorf("log %q; want a line %q<cause>%q", *lines, prefix, suffix)
	}
}

// -no-skills-mcp on a config whose skills.dir was cleared (an unresolved
// ${PROJECT_DIR}) must not materialize anywhere else — an empty dir joined to
// the working directory would be the project root.
func TestSetupSkillsWithoutDirWritesNothing(t *testing.T) {
	work := t.TempDir()
	logger, lines := newLogCollector()
	if _, err := setupSkills(&config.Config{Skills: skillsVia(false, true, "")}, work, logger); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, work); len(got) != 0 {
		t.Errorf("wrote %v into the working directory; want nothing", got)
	}
	if log := strings.Join(*lines, "\n"); !strings.Contains(log, "WARN") || !strings.Contains(log, "skills.dir") {
		t.Errorf("log %q; want a WARN naming skills.dir", log)
	}
}

// skills.enabled: false is the escape hatch: nothing is loaded, nothing
// written, and a set that would not load does not stop the start.
func TestSetupSkillsDisabledDoesNothing(t *testing.T) {
	saved := loadEmbeddedSkills
	loadEmbeddedSkills = func() ([]skills.Skill, error) { return nil, errors.New("must not be called") }
	t.Cleanup(func() { loadEmbeddedSkills = saved })
	dir := t.TempDir()
	cfg := &config.Config{Skills: config.SkillsConfig{Enabled: ptr(false), MCP: ptr(true), Materialize: true, Dir: dir}}
	all, err := setupSkills(cfg, t.TempDir(), func(string) {})
	if err != nil || all != nil {
		t.Errorf("setupSkills = %d skills, %v; want none, nil", len(all), err)
	}
	if got := tree(t, dir); len(got) != 0 {
		t.Errorf("wrote %v; want nothing", got)
	}
}

// A set that does not load — a broken skill, or two that collide on disk —
// stops the start, naming the cause and the way out. It cannot happen in a
// tested build; if it does, the skills must not vanish silently.
func TestSetupSkillsLoadErrorStopsTheStart(t *testing.T) {
	saved := loadEmbeddedSkills
	t.Cleanup(func() { loadEmbeddedSkills = saved })
	for _, tc := range []struct {
		name string
		load func() ([]skills.Skill, error)
		want string
	}{
		{"broken skill", func() ([]skills.Skill, error) {
			return nil, errors.New("skills: bad: frontmatter: name is missing")
		}, "name is missing"},
		{"collision", func() ([]skills.Skill, error) {
			return []skills.Skill{{Name: "ue-x", Source: "a"}, {Name: "unreagent-ue-x", Source: "b"}}, nil
		}, "unreagent-ue-x"},
	} {
		loadEmbeddedSkills = tc.load
		_, err := setupSkills(&config.Config{Skills: skillsVia(true, false, "")}, t.TempDir(), func(string) {})
		if err == nil {
			t.Errorf("%s: setupSkills succeeded; want an error", tc.name)
			continue
		}
		for _, w := range []string{tc.want, "skills.enabled: false"} {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q does not mention %q", tc.name, err, w)
			}
		}
	}
}

// Skills that reach the agent by no path — the tool is on but the MCP server
// is off, and nothing is materialized — are worth a warning, not silence.
func TestSetupSkillsWarnsWhenNoPathReachesTheAgent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mcpEnabled bool
		warn       bool
	}{
		{"mcp server off", false, true},
		{"mcp server on", true, false},
	} {
		logger, lines := newLogCollector()
		cfg := &config.Config{Skills: skillsVia(true, false, ""), MCP: config.MCPConfig{Enabled: tc.mcpEnabled}}
		if _, err := setupSkills(cfg, t.TempDir(), logger); err != nil {
			t.Fatal(err)
		}
		log := strings.Join(*lines, "\n")
		if got := strings.Contains(log, "WARN") && strings.Contains(log, "mcp.enabled"); got != tc.warn {
			t.Errorf("%s: log %q; want a WARN naming mcp.enabled: %v", tc.name, log, tc.warn)
		}
	}
}

// -no-skills-mcp switches delivery to files whatever the loaded config said;
// with skills disabled it changes nothing that matters.
func TestApplyNoSkillsMCP(t *testing.T) {
	for _, tc := range []struct {
		name           string
		s              config.SkillsConfig
		viaMCP, viaDir bool
	}{
		{"mcp only", skillsVia(true, false, "d"), false, true},
		{"both", skillsVia(true, true, "d"), false, true},
		{"disabled", config.SkillsConfig{Enabled: ptr(false), MCP: ptr(true)}, false, false},
	} {
		s := tc.s
		applyNoSkillsMCP(&s, func(string) {})
		if s.ViaMCP() != tc.viaMCP || s.ViaFiles() != tc.viaDir {
			t.Errorf("%s: ViaMCP/ViaFiles = %v/%v; want %v/%v", tc.name, s.ViaMCP(), s.ViaFiles(), tc.viaMCP, tc.viaDir)
		}
	}
}

// -no-skills-mcp wins over unreagent.local.yaml: run() applies it after
// config.Load has overlaid the local file. This drives the real run() to pin
// that order, and that the start materializes into skills.dir before the
// session runs. The local file asks for the MCP tool only; the MCP server is
// off, so without the flag no skill reaches the agent.
func TestRunNoSkillsMCPWinsOverTheLocalOverlay(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		files bool
		line  string
	}{
		{"without the flag", nil, false, "WARN no skills reach the agent"},
		{"with -no-skills-mcp", []string{"-no-skills-mcp"}, true, "materialized into"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			skillsDir := filepath.Join(dir, "skills")
			mustWriteFile(t, filepath.Join(dir, "unreagent.local.yaml"), "skills: {mcp: true, materialize: false}\n")
			yaml := launcherYAML("sleep", "  enabled: false\n") + "skills:\n  dir: \"" + filepath.ToSlash(skillsDir) + "\"\n"
			l := startLauncherIn(t, dir, yaml, tc.args...)
			l.waitLine(t, tc.line)
			got := tree(t, skillsDir)
			if tc.files {
				if want := materialized(shippedSkills(t)); !maps.Equal(got, want) {
					t.Errorf("materialized %v; want the shipped set %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
				}
			} else if got != nil {
				t.Errorf("materialized %v without the flag; want nothing", slices.Sorted(maps.Keys(got)))
			}
		})
	}
}

// --- k12: what materialization must never touch ---------------------------

// mustSymlink makes link point at target, or skips the test where the OS or
// the account cannot create symlinks (Windows without the privilege).
func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
}

// requireSymlink fails unless path is still a symlink (not followed, not
// replaced by a directory).
func requireSymlink(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("%s is gone: %v", path, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is %v; want the symlink untouched", path, fi.Mode())
	}
}

// A symlink named like a skill directory is not ours and is in the way: Mkdir
// must refuse it (not MkdirAll, not an ignored error), so nothing is written
// through it into the user's own skill directory behind it.
func TestMaterializeDoesNotWriteThroughASymlinkedSkillDirectory(t *testing.T) {
	dir, foreign := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(foreign, "SKILL.md"), "the user's own skill")
	mustWriteFile(t, filepath.Join(foreign, "nodes.md"), "the user's own section")
	before := tree(t, foreign)
	mustSymlink(t, foreign, filepath.Join(dir, "unreagent-alpha"))

	if err := materializeSkills(dir, testSkills()); err == nil {
		t.Error("materializeSkills succeeded with a symlink in the way of unreagent-alpha; want an error")
	}
	if got := tree(t, foreign); !maps.Equal(got, before) {
		t.Errorf("the symlink target changed: %v; want %v", got, before)
	}
	requireSymlink(t, filepath.Join(dir, "unreagent-alpha"))
}

// The prefix is matched case-sensitively: Unreagent-* and UNREAGENT-* are the
// project's own. (Names no shipped skill has, so a case-insensitive file
// system cannot make them collide with a real skill directory.)
func TestMaterializeOwnershipIsCaseSensitive(t *testing.T) {
	dir := t.TempDir()
	foreign := map[string]string{
		"Unreagent-keep/SKILL.md":  "capitalized prefix",
		"UNREAGENT-other/SKILL.md": "upper-case prefix",
	}
	for p, content := range foreign {
		mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(p)), content)
	}
	all := testSkills()
	if err := materializeSkills(dir, all); err != nil {
		t.Fatal(err)
	}
	want := materialized(all)
	maps.Copy(want, foreign)
	if got := tree(t, dir); !maps.Equal(got, want) {
		t.Errorf("tree = %v\nwant %v", got, want)
	}
}

// A symlink to a directory is not a directory of ours, even with the prefix
// and no shipped skill of that name: it is neither removed nor followed.
func TestMaterializeDoesNotFollowASymlinkWithThePrefix(t *testing.T) {
	dir, foreign := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(foreign, "SKILL.md"), "the user's own skill")
	mustWriteFile(t, filepath.Join(foreign, "deep", "a.md"), "deep")
	before := tree(t, foreign)
	mustSymlink(t, foreign, filepath.Join(dir, "unreagent-zzz"))

	all := testSkills()
	if err := materializeSkills(dir, all); err != nil {
		t.Fatal(err)
	}
	requireSymlink(t, filepath.Join(dir, "unreagent-zzz"))
	if got := tree(t, foreign); !maps.Equal(got, before) {
		t.Errorf("the symlink target changed: %v; want %v", got, before)
	}
}

// --- run() wiring: the skill set and the load error ----------------------

// loadFailEnv makes the launcher process of TestLauncherRunProcess load a
// skill set that fails, as a build with a broken embedded skill would.
const loadFailEnv = "UNREAGENT_TEST_SKILLS_LOAD_FAIL"

// run() hands registerTools the skill set setupSkills loaded: the skills tool
// of a real launcher serves a shipped skill, and lists it in its description.
func TestRunRegistersTheSkillsToolWithTheLoadedSkills(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick a free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	yaml := strings.Replace(launcherYAML("sleep", "  enabled: false\n"),
		"mcp:\n  enabled: false\n", "mcp:\n  enabled: true\n  address: "+addr+"\n", 1)
	l := startLauncher(t, yaml)
	l.waitLine(t, "MCP server listening on")

	shipped := shippedSkills(t)[0]
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"skills","arguments":{"name":"` + shipped.Name + `"}}}`
	// No retry: the launcher logs "MCP server listening" only once the port
	// is bound (k141).
	resp, err := http.Post("http://"+addr+"/mcp", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("MCP server on %s does not answer right after the listening line: %v\n%s", addr, err, l.stdout)
	}
	var out struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("decode tools/call: %v", err)
	}
	if out.Result.IsError || len(out.Result.Content) == 0 || out.Result.Content[0].Text != shipped.Body {
		t.Errorf("skills tool for %q returned %+v; want its body (the launcher's tool carries the loaded skills)", shipped.Name, out.Result)
	}

	if err := l.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	l.exitCode(t)
}

// A skill set that does not load stops run(): the launcher exits 1 and names
// the way out, instead of starting without skills.
func TestRunStopsWhenTheSkillSetDoesNotLoad(t *testing.T) {
	t.Setenv(loadFailEnv, "1")
	l := startLauncher(t, launcherYAML("sleep", "  enabled: false\n")+"skills:\n  mcp: true\n")
	if code := l.exitCode(t); code != 1 {
		t.Fatalf("exit code %d, want 1 for a skill set that does not load:\n%s\nstderr: %s", code, l.stdout, l.stderr.String())
	}
	if !strings.Contains(l.stderr.String(), "skills.enabled: false") {
		t.Errorf("stderr does not name the way out: %q", l.stderr.String())
	}
}
