// Tests for the skills delivery paths (k10–k12; acceptance of k15–k17): the
// skills MCP tool driven through JSON-RPC tools/list and tools/call, and
// materialization into a real temp directory.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

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

// Every skill of the set gets its prefixed directory, holding SKILL.md as
// embedded, its name set to that directory where the prefix is added
// (materialized), and its sub-documents next to it — and nothing else. The
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
// an escape, a tag, an anchor, an alias — all of which Load accepts), is
// refused before anything in dir changes rather than rewritten by guess; so is
// a skill whose fields do not fit together, which only a hand-built set can be.
func TestMaterializeRefusesANameItCannotRewrite(t *testing.T) {
	const notPlain = "not a plain or quoted scalar"
	for _, tc := range []struct{ why, source, body, want string }{
		{"folded name", "---\nname: >-\n  ue-f\ndescription: d\n---\n# f\n", "# f\n", notPlain},
		{"escaped name", "---\nname: \"ue\\x2df\"\ndescription: d\n---\n# f\n", "# f\n", notPlain},
		{"tagged name", "---\nname: !!str ue-f\ndescription: d\n---\n# f\n", "# f\n", notPlain},
		{"anchored name", "---\nname: &n ue-f\ndescription: d\n---\n# f\n", "# f\n", notPlain},
		{"aliased name", "---\ndescription: &n ue-f\nname: *n\n---\n# f\n", "# f\n", notPlain},
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

// A section named like a Windows device is refused on every OS: Windows 10
// opens aux.md as the AUX device, so filepath.IsLocal fails there and the whole
// set would be skipped on Windows alone. Names that merely contain a device
// name are fine.
func TestCheckSkillDirsRejectsWindowsDeviceNames(t *testing.T) {
	sk := func(section string) []skills.Skill {
		return []skills.Skill{
			{Name: "ue-ok", Source: "x", Body: "x", Files: map[string]string{"nodes": "x"}},
			{Name: "ue-x", Source: "x", Body: "x", Files: map[string]string{"graphs": "x", section: "x"}},
		}
	}
	for _, section := range []string{"aux", "CON", "nul", "Prn", "com1", "LPT9", "COM¹", "conin$", "con.backup", "aux "} {
		err := checkSkillDirs(sk(section))
		if err == nil {
			t.Errorf("section %q: no error; want it refused as a Windows device name", section)
			continue
		}
		for _, w := range []string{"ue-x", section + ".md", "Windows device"} {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("section %q: error %q does not mention %q", section, err, w)
			}
		}
	}
	for _, section := range []string{"console", "auxiliary", "com10", "nullable", "lpt", "my-aux"} {
		if err := checkSkillDirs(sk(section)); err != nil {
			t.Errorf("section %q: %v; want it accepted", section, err)
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
	// The text claims no more than it knows: a skill that could not be put in
	// place may be missing or the previous start's, and a removal that fails
	// leaves files of an earlier start.
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

// Every skill that fails is named, in the one WARN line: the log is read line
// by line, and a kept previous copy must not drop out of it.
func TestSetupSkillsWarnsOnOneLineForEverySkillThatFails(t *testing.T) {
	saved := loadEmbeddedSkills
	loadEmbeddedSkills = func() ([]skills.Skill, error) { return testSkills(), nil }
	t.Cleanup(func() { loadEmbeddedSkills = saved })
	dir := t.TempDir()
	for _, s := range testSkills() {
		mustWriteFile(t, filepath.Join(dir, skillDirName(s.Name)), "a file in the way")
	}
	logger, lines := newLogCollector()
	if _, err := setupSkills(&config.Config{Skills: skillsVia(true, true, dir)}, t.TempDir(), logger); err != nil {
		t.Fatalf("setupSkills: %v; want a warning, not an error", err)
	}
	var warns []string
	for _, l := range *lines {
		if strings.Contains(l, "WARN skills not fully written") {
			warns = append(warns, l)
		}
	}
	if len(warns) != 1 || strings.Contains(warns[0], "\n") {
		t.Fatalf("warnings %q; want one line", warns)
	}
	for _, s := range testSkills() {
		if p := filepath.Join(dir, skillDirName(s.Name)); !strings.Contains(warns[0], p) {
			t.Errorf("warning %q does not name %s", warns[0], p)
		}
	}
}

// A skills.dir no work directory can be created in (read-only) fails every
// skill the same way, and no second launcher or scanner causes that: the
// first failure ends the run at once — no pause, no further skill — and the
// one cause is reported once, with how many skills were not written.
func TestSetupSkillsGivesUpAtOnceWhenNoWorkDirectoryCanBeCreated(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	saved := loadEmbeddedSkills
	loadEmbeddedSkills = func() ([]skills.Skill, error) { return testSkills(), nil }
	t.Cleanup(func() { loadEmbeddedSkills = saved })
	pause := time.Second
	setRetryPauses(t, pause, pause) // any attempt after the first shows in the time
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	logger, lines := newLogCollector()
	start := time.Now()
	if _, err := setupSkills(&config.Config{Skills: skillsVia(true, true, dir)}, t.TempDir(), logger); err != nil {
		t.Fatalf("setupSkills: %v; want a warning, not an error", err)
	}
	if took := time.Since(start); took >= pause {
		t.Errorf("setupSkills took %v; want it to give up without a pause (%v)", took, pause)
	}
	var warns []string
	for _, l := range *lines {
		if strings.Contains(l, "WARN skills not fully written") {
			warns = append(warns, l)
		}
	}
	if len(warns) != 1 {
		t.Fatalf("warnings %q; want one", warns)
	}
	n := len(testSkills())
	if c := strings.Count(warns[0], "permission denied"); c != 1 {
		t.Errorf("warning %q names the cause %d times; want once", warns[0], c)
	}
	if want := fmt.Sprintf("(%d of %d skills not written)", n, n); !strings.Contains(warns[0], want) {
		t.Errorf("warning %q does not say %q", warns[0], want)
	}
}

// A run that stops at a work directory it cannot create still spares the
// skills it never tried in its sweep: their copies from the earlier start
// stay, and only what is not in the set goes. Here skills.dir is read-only
// until the sweep has listed it, so the first skill fails, the second is
// never tried, and the sweep can still rename and remove.
func TestMaterializeSparesTheSkillsItNeverTried(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	all := testSkills()
	dir := t.TempDir()
	previousStart(t, dir, all)
	previous := byTopDir(previousSet(all))
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	setMaterializeHook(t, func(step string) {
		if step == "listed" {
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	})
	err := materializeSkills(dir, all)
	if want := fmt.Sprintf("(%d of %d skills not written)", len(all), len(all)); !errors.Is(err, errNoWorkDir) || !strings.Contains(err.Error(), want) {
		t.Errorf("materializeSkills: %v; want the work directory error with %q", err, want)
	}
	got := byTopDir(tree(t, dir))
	for _, s := range all {
		d := skillDirName(s.Name)
		if !maps.Equal(got[d], previous[d]) {
			t.Errorf("%s holds %v; want its copy from the earlier start %v", d, got[d], previous[d])
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "unreagent-dropped")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unreagent-dropped is still there (%v); want the sweep to remove it", err)
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
		{"section named like a Windows device", func() ([]skills.Skill, error) {
			return []skills.Skill{{Name: "ue-x", Source: "a", Files: map[string]string{"aux": "x"}}}, nil
		}, "aux.md"},
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

// A symlink named like a skill directory is not ours and is in the way: it
// must be refused (not moved aside, not an ignored error), so nothing is
// written through it into the user's own skill directory behind it. The other
// skills are still written, and the run leaves no work directory behind —
// the symlink would otherwise stop every start before its sweep.
func TestMaterializeDoesNotWriteThroughASymlinkedSkillDirectory(t *testing.T) {
	dir, foreign := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(foreign, "SKILL.md"), "the user's own skill")
	mustWriteFile(t, filepath.Join(foreign, "nodes.md"), "the user's own section")
	before := tree(t, foreign)
	mustSymlink(t, foreign, filepath.Join(dir, "unreagent-alpha"))

	all := testSkills()
	if err := materializeSkills(dir, all); err == nil || !strings.Contains(err.Error(), "unreagent-alpha") {
		t.Errorf("materializeSkills = %v with a symlink in the way of unreagent-alpha; want an error naming it", err)
	}
	if got := tree(t, foreign); !maps.Equal(got, before) {
		t.Errorf("the symlink target changed: %v; want %v", got, before)
	}
	requireSymlink(t, filepath.Join(dir, "unreagent-alpha"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"unreagent-alpha", "unreagent-ue-beta"}; !slices.Equal(names, want) {
		t.Errorf("dir holds %v; want %v — the symlink and the other skill, no work directory", names, want)
	}
	if got, want := tree(t, filepath.Join(dir, "unreagent-ue-beta")), byTopDir(materialized(all))["unreagent-ue-beta"]; !maps.Equal(got, want) {
		t.Errorf("unreagent-ue-beta = %v; want %v", got, want)
	}
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

// --- k139: two starts at once, and a crash in between ---------------------

// setMaterializeHook replaces materializeHook for the rest of the test.
func setMaterializeHook(t *testing.T, hook func(step string)) {
	t.Helper()
	saved := materializeHook
	materializeHook = hook
	t.Cleanup(func() { materializeHook = saved })
}

// setRetryPauses replaces skillRetryPauses for the rest of the test, so the
// tests that make attempts fail stay fast.
func setRetryPauses(t *testing.T, pauses ...time.Duration) {
	t.Helper()
	saved := skillRetryPauses
	skillRetryPauses = pauses
	t.Cleanup(func() { skillRetryPauses = saved })
}

// workDirs lists the work directories in dir.
func workDirs(t *testing.T, dir string) []string {
	t.Helper()
	works, err := filepath.Glob(filepath.Join(dir, skillDirPrefix+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	return works
}

// previousSet is what an earlier start left of the launcher's own directories
// for all: each with an older SKILL.md and a file no longer shipped, and a
// skill since dropped.
func previousSet(all []skills.Skill) map[string]string {
	out := map[string]string{"unreagent-dropped/SKILL.md": "a dropped skill"}
	for _, s := range all {
		d := skillDirName(s.Name)
		out[d+"/SKILL.md"] = "an older " + d
		out[d+"/stale.md"] = "no longer shipped"
	}
	return out
}

// previousStart fills dir with previousSet(all) next to the project's own
// entries, and returns those.
func previousStart(t *testing.T, dir string, all []skills.Skill) map[string]string {
	t.Helper()
	foreign := map[string]string{"my-skill/SKILL.md": "mine", "README.md": "the project's own"}
	for _, m := range []map[string]string{previousSet(all), foreign} {
		for p, c := range m {
			mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(p)), c)
		}
	}
	return foreign
}

// byTopDir groups a tree by its first path element; top-level files go under "".
func byTopDir(tree map[string]string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for p, c := range tree {
		top, rest, ok := strings.Cut(p, "/")
		if !ok {
			top, rest = "", p
		}
		if out[top] == nil {
			out[top] = map[string]string{}
		}
		out[top][rest] = c
	}
	return out
}

// countMaterializeSteps runs one undisturbed materialization of all into a
// previous start and returns how often it called materializeHook.
func countMaterializeSteps(t *testing.T, all []skills.Skill) int {
	t.Helper()
	steps := 0
	setMaterializeHook(t, func(string) { steps++ })
	dir := t.TempDir()
	previousStart(t, dir, all)
	if err := materializeSkills(dir, all); err != nil {
		t.Fatal(err)
	}
	if steps == 0 {
		t.Fatal("materializeSkills never called materializeHook")
	}
	return steps
}

// A second launcher on the same project may run its whole materialization in
// any gap of the first one's. Both must succeed and leave exactly the set:
// neither may remove what the other just wrote, nor fail on it. This runs the
// second materialization in every gap the first one has, one after another.
func TestMaterializeWithASecondRunInEveryGap(t *testing.T) {
	setRetryPauses(t, time.Millisecond, 2*time.Millisecond)
	all := testSkills()
	steps := countMaterializeSteps(t, all)
	for k := 1; k <= steps; k++ {
		dir := t.TempDir()
		want := previousStart(t, dir, all)
		maps.Copy(want, materialized(all))
		var at string
		var peerErr error
		n := 0
		setMaterializeHook(t, func(step string) {
			if n++; n == k {
				at = step
				peerErr = materializeSkills(dir, all) // its own steps only count on
			}
		})
		err := materializeSkills(dir, all)
		if err != nil || peerErr != nil {
			t.Errorf("second run at step %d (%s): first run %v, second run %v; want both to succeed", k, at, err, peerErr)
		}
		if got := tree(t, dir); !maps.Equal(got, want) {
			t.Errorf("second run at step %d (%s): tree = %v\nwant %v", k, at, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
		}
	}
}

// crash is the panic value TestMaterializeAfterACrashAtEveryStep stops a run
// with.
type crash struct{}

// A launcher killed between any two steps leaves every skill directory whole —
// the previous copy or the new one, nothing in between — and no copy of a
// current skill outside its directory where an agent looks (<dir>/*/SKILL.md).
// Whatever else it leaves carries the prefix, and the next start removes it.
func TestMaterializeAfterACrashAtEveryStep(t *testing.T) {
	all := testSkills()
	steps := countMaterializeSteps(t, all)
	previous, current := byTopDir(previousSet(all)), byTopDir(materialized(all))
	for k := 1; k <= steps; k++ {
		dir := t.TempDir()
		foreign := previousStart(t, dir, all)
		var at string
		n := 0
		setMaterializeHook(t, func(step string) {
			if n++; n == k {
				at = step
				panic(crash{})
			}
		})
		func() {
			defer func() {
				if r := recover(); r != (crash{}) {
					t.Fatalf("crash at step %d: recovered %v; want the crash", k, r)
				}
			}()
			_ = materializeSkills(dir, all)
		}()

		for top, files := range byTopDir(tree(t, dir)) {
			switch {
			case current[top] != nil || previous[top] != nil:
				if !maps.Equal(files, current[top]) && !maps.Equal(files, previous[top]) {
					t.Errorf("crash at step %d (%s): %s holds %v; want the previous or the new copy whole", k, at, top, files)
				}
			case strings.HasPrefix(top, skillDirPrefix):
				// The sweep renames before it removes, so the dropped skill
				// may sit here under a fresh name; a current one never.
				if doc, ok := files[skillFile]; ok && doc != previous["unreagent-dropped"][skillFile] {
					t.Errorf("crash at step %d (%s): %s/%s is a copy of a current skill an agent would load: %q", k, at, top, skillFile, doc)
				}
			case !maps.Equal(files, byTopDir(foreign)[top]):
				t.Errorf("crash at step %d (%s): %q holds %v; want the project's own entries untouched", k, at, top, files)
			}
		}

		setMaterializeHook(t, func(string) {})
		if err := materializeSkills(dir, all); err != nil {
			t.Fatalf("start after a crash at step %d (%s): %v", k, at, err)
		}
		want := materialized(all)
		maps.Copy(want, foreign)
		if got := tree(t, dir); !maps.Equal(got, want) {
			t.Errorf("start after a crash at step %d (%s): tree = %v\nwant %v", k, at, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
		}
	}
}

// A rename in that fails after the previous copy was moved aside — a virus
// scanner holding a file of the new copy, on Windows — puts the previous copy
// back before the next attempt: a failed attempt leaves the skill as it found
// it, not missing. Here the new copy is taken away under the attempt.
func TestMaterializeMovesThePreviousCopyBackWhenTheRenameInFails(t *testing.T) {
	setRetryPauses(t, time.Millisecond, 2*time.Millisecond)
	all := testSkills()
	dir := t.TempDir()
	want := previousStart(t, dir, all)
	maps.Copy(want, materialized(all))
	previous := byTopDir(previousSet(all))
	var broken string
	var between map[string]string
	setMaterializeHook(t, func(step string) {
		switch {
		case broken == "" && step == "aside":
			works := workDirs(t, dir)
			if len(works) != 1 {
				t.Fatalf("work directories at the first aside: %v; want one", works)
			}
			entries, err := os.ReadDir(works[0])
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() != "previous" {
					broken = e.Name()
					if err := os.RemoveAll(filepath.Join(works[0], broken)); err != nil {
						t.Fatal(err)
					}
				}
			}
		case broken != "" && between == nil:
			// The next attempt's first step: what the failed one left.
			between = byTopDir(tree(t, dir))[broken]
		}
	})
	if err := materializeSkills(dir, all); err != nil {
		t.Fatalf("materializeSkills: %v; want the next attempt to succeed", err)
	}
	if broken == "" {
		t.Fatal("no attempt moved a previous copy aside")
	}
	if !maps.Equal(between, previous[broken]) {
		t.Errorf("after the failed attempt %s holds %v; want the previous copy %v whole", broken, between, previous[broken])
	}
	if got := tree(t, dir); !maps.Equal(got, want) {
		t.Errorf("tree = %v\nwant %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
}

// A work directory a second launcher's sweep took after the previous copy went
// into it — a launcher of another version, whose set lacks the skill — took
// that copy along: nothing is left to keep, so nothing is reported as kept,
// and the next attempt puts the skill in place.
func TestMaterializeTriesAgainWhenItsWorkDirectoryIsTaken(t *testing.T) {
	setRetryPauses(t, time.Millisecond, 2*time.Millisecond)
	all := testSkills()
	dir := t.TempDir()
	want := previousStart(t, dir, all)
	maps.Copy(want, materialized(all))
	taken := false
	setMaterializeHook(t, func(step string) {
		if taken || step != "aside" {
			return
		}
		taken = true
		for _, w := range workDirs(t, dir) {
			if err := os.RemoveAll(w); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err := materializeSkills(dir, all); err != nil {
		t.Fatalf("materializeSkills: %v; want the next attempt to succeed", err)
	}
	if !taken {
		t.Fatal("no attempt moved a previous copy aside")
	}
	if got := tree(t, dir); !maps.Equal(got, want) {
		t.Errorf("tree = %v\nwant %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
}

// When the previous copy cannot be moved back either, it is not thrown away:
// it stays whole in its work directory, which the sweep of this run spares,
// the error names where it is, and the attempts for that skill end. The other
// skills are still written. Here the work directory is made read-only after
// the previous copy went into it, so neither copy can leave it.
func TestMaterializeKeepsThePreviousCopyItCannotMoveBack(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	setRetryPauses(t, time.Millisecond, 2*time.Millisecond)
	all := testSkills()
	dir := t.TempDir()
	previousStart(t, dir, all)
	previous, current := byTopDir(previousSet(all)), byTopDir(materialized(all))
	var work, broken string
	setMaterializeHook(t, func(step string) {
		if work != "" || step != "aside" {
			return
		}
		works := workDirs(t, dir)
		if len(works) != 1 {
			t.Fatalf("work directories at the first aside: %v; want one", works)
		}
		work = works[0]
		entries, err := os.ReadDir(work)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() != "previous" {
				broken = e.Name()
			}
		}
		if err := os.Chmod(work, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(work, 0o755) })
	})
	err := materializeSkills(dir, all)
	if work == "" {
		t.Fatal("no attempt moved a previous copy aside")
	}
	kept := filepath.Join(work, "previous")
	if err == nil || !strings.Contains(err.Error(), "it is kept in "+kept+" until the next start removes it") {
		t.Errorf("materializeSkills: %v; want an error naming %s", err, kept)
	}
	if got := byTopDir(tree(t, work))["previous"]; !maps.Equal(got, previous[broken]) {
		t.Errorf("%s holds %v; want the previous copy of %s whole", kept, got, broken)
	}
	got := byTopDir(tree(t, dir))
	if _, err := os.Lstat(filepath.Join(dir, broken)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is in place (%v); want the attempts to have ended with the previous copy kept", broken, err)
	}
	for top, files := range current {
		if top != broken && !maps.Equal(got[top], files) {
			t.Errorf("%s holds %v; want the new copy %v", top, got[top], files)
		}
	}
	if works := workDirs(t, dir); !slices.Equal(works, []string{work}) {
		t.Errorf("work directories after the run: %v; want only the one that keeps the previous copy, %s", works, work)
	}
}

// installSkillDir makes one attempt more than skillRetryPauses has pauses, and
// waits each of them in turn before the next attempt, so a short lock or a
// second launcher's run has passed. The pauses shipped grow and stay short.
func TestInstallSkillDirPausesBetweenAttempts(t *testing.T) {
	var total time.Duration
	for _, p := range skillRetryPauses {
		total += p
	}
	if len(skillRetryPauses) == 0 || !slices.IsSorted(skillRetryPauses) || total > time.Second {
		t.Errorf("skillRetryPauses = %v; want growing pauses that add up to a second at most", skillRetryPauses)
	}

	pauses := []time.Duration{20 * time.Millisecond, 60 * time.Millisecond}
	setRetryPauses(t, pauses...)
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "unreagent-x", skillFile), "an older copy")
	var attempts []time.Time
	setMaterializeHook(t, func(string) {
		// The copy is written: take the work directory away, so moving the
		// previous copy aside into it fails.
		attempts = append(attempts, time.Now())
		for _, w := range workDirs(t, dir) {
			if err := os.RemoveAll(w); err != nil {
				t.Fatal(err)
			}
		}
	})
	if _, err := installSkillDir(dir, "unreagent-x", map[string]string{skillFile: "a new copy"}); err == nil {
		t.Error("installSkillDir succeeded; want every attempt to fail")
	}
	if len(attempts) != len(pauses)+1 {
		t.Fatalf("%d attempts; want %d", len(attempts), len(pauses)+1)
	}
	for i, p := range pauses {
		if gap := attempts[i+1].Sub(attempts[i]); gap < p {
			t.Errorf("attempt %d came %v after attempt %d; want a pause of %v at least", i+2, gap, i+1, p)
		}
	}
	if got := mustReadFile(t, filepath.Join(dir, "unreagent-x", skillFile)); got != "an older copy" {
		t.Errorf("unreagent-x/%s = %q; want the previous copy untouched", skillFile, got)
	}
}

// Two launchers started at once over the shipped set, for real: both succeed
// and the set is whole every time. TestMaterializeWithASecondRunInEveryGap
// pins the interleavings one by one; this catches the ones between its steps.
func TestMaterializeTwoRunsAtOnce(t *testing.T) {
	setRetryPauses(t, time.Millisecond, 2*time.Millisecond)
	all := append(shippedSkills(t), testSkills()...)
	for i := range 40 {
		dir := t.TempDir()
		want := previousStart(t, dir, all)
		maps.Copy(want, materialized(all))
		var errs [2]error
		var wg sync.WaitGroup
		start := make(chan struct{})
		for j := range errs {
			wg.Go(func() {
				<-start
				errs[j] = materializeSkills(dir, all)
			})
		}
		close(start)
		wg.Wait()
		if errs[0] != nil || errs[1] != nil {
			t.Errorf("round %d: %v, %v; want both runs to succeed", i, errs[0], errs[1])
		}
		if got := tree(t, dir); !maps.Equal(got, want) {
			t.Fatalf("round %d: tree = %v\nwant %v", i, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
		}
	}
}

// --- k164: leftovers the sweep cannot remove ------------------------------

// A leftover the sweep cannot remove (a file held open by a virus scanner on
// Windows; here a read-only directory) does not stop the sweep: every later
// one still goes, and each that stays is named in the one WARN line. The
// leftovers sort a, b, c, so b comes after the first that fails.
func TestSweepRemovesEveryLeftoverItCan(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	saved := loadEmbeddedSkills
	loadEmbeddedSkills = func() ([]skills.Skill, error) { return testSkills(), nil }
	t.Cleanup(func() { loadEmbeddedSkills = saved })
	dir := t.TempDir()
	// Registered after TempDir, so it runs before TempDir's removal; the
	// sweep renamed the held leftovers, so every directory is opened up.
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	held := []string{skillDirPrefix + ".tmp-a", skillDirPrefix + ".tmp-c"}
	free := skillDirPrefix + ".tmp-b"
	for _, name := range append([]string{free}, held...) {
		mustWriteFile(t, filepath.Join(dir, name, "previous", skillFile), "left by an interrupted start")
	}
	for _, name := range held {
		if err := os.Chmod(filepath.Join(dir, name), 0o555); err != nil {
			t.Fatal(err)
		}
	}
	logger, lines := newLogCollector()
	if _, err := setupSkills(&config.Config{Skills: skillsVia(true, true, dir)}, t.TempDir(), logger); err != nil {
		t.Fatalf("setupSkills: %v; want a warning, not an error", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, free)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is still there (%v); want the sweep to remove it although %s could not be", free, err, held[0])
	}
	if works := workDirs(t, dir); len(works) != len(held) {
		t.Errorf("work directories after the run: %v; want only the %d held leftovers", works, len(held))
	}
	var warns []string
	for _, l := range *lines {
		if strings.Contains(l, "WARN skills not fully written") {
			warns = append(warns, l)
		}
	}
	if len(warns) != 1 || strings.Contains(warns[0], "\n") {
		t.Fatalf("warnings %q; want one line", warns)
	}
	if c := strings.Count(warns[0], "permission denied"); c != len(held) {
		t.Errorf("warning %q names %d failed removals; want %d", warns[0], c, len(held))
	}
	got := tree(t, dir)
	maps.DeleteFunc(got, func(p, _ string) bool { return strings.HasPrefix(p, skillDirPrefix+".tmp-") })
	if want := materialized(testSkills()); !maps.Equal(got, want) {
		t.Errorf("skills.dir outside the held leftovers = %v; want the set in place, %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
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
