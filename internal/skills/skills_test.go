package skills

import (
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	shipped "github.com/conflict-industries/unreagent/skills"
)

// skillMD builds a well-formed SKILL.md for dir; extra is spliced into the
// frontmatter as-is.
func skillMD(name, extra string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("---\nname: " + name +
		"\ndescription: Use when testing " + name + ".\n" + extra + "---\n\n# " + name + "\n")}
}

func md(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

// Every skill that ships in the binary must parse, and none may go missing on
// the way: Load returns exactly one Skill per embedded directory, in directory
// order, with the fields an agent selects on filled in.
func TestLoadParsesEveryEmbeddedSkill(t *testing.T) {
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	dirs, err := fs.ReadDir(shipped.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir(embedded): %v", err)
	}
	if len(dirs) == 0 {
		t.Fatal("no skill directories embedded")
	}
	if len(got) != len(dirs) {
		t.Fatalf("Load returned %d skills for %d embedded directories", len(got), len(dirs))
	}
	for i, s := range got {
		if s.Name != dirs[i].Name() {
			t.Errorf("skill %d: Name = %q, want the directory name %q", i, s.Name, dirs[i].Name())
		}
		if strings.TrimSpace(s.Description) == "" {
			t.Errorf("skill %q: empty description", s.Name)
		}
		if strings.TrimSpace(s.Body) == "" {
			t.Errorf("skill %q: empty body", s.Name)
		}
	}
}

// The embed pattern must pick up skill documents and nothing else — in
// particular not the Go file that carries the directive.
func TestEmbedHoldsOnlySkillDocuments(t *testing.T) {
	err := fs.WalkDir(shipped.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.Count(p, "/") != 1 || !strings.HasSuffix(p, ".md") {
			t.Errorf("embedded %q: only <skill>/<doc>.md belongs in the embed", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(embedded): %v", err)
	}
}

// The reverse direction: a file placed in a skill directory that the embed
// pattern does not match (a nested folder, a .txt, a typo'd extension) would
// silently not ship. Every non-Go file under skills/ must be embedded.
// Dot-named entries are local clutter (.DS_Store, editor swap files) and are
// skipped here; a dot-named .md is embedded anyway, and Load rejects it.
func TestEmbedMissesNoSourceFile(t *testing.T) {
	root := filepath.Join("..", "..", "skills")
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
		if _, err := fs.Stat(shipped.FS, filepath.ToSlash(rel)); err != nil {
			t.Errorf("skills/%s is not embedded (pattern mismatch) and would not ship", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%s): %v", root, err)
	}
}

// A broken skill must stop Load, never be skipped: a skill that vanishes from
// the set is exactly the silent failure the subsystem exists to prevent. Each
// case sits next to a valid skill, a-good: a loader that drops the bad skill
// is caught, and so, where a-good sorts first, is one that returns what it
// loaded before it. (.bad and _bad sort before a-good, so for them a partial
// result is empty and this case cannot catch a partial return.) want, when
// set, must appear in the error — for the cases where only the message tells
// a precise check from a lucky one.
func TestLoadRejectsMalformedSkill(t *testing.T) {
	cases := []struct {
		name  string
		files fstest.MapFS
		want  string
	}{
		{"no frontmatter", fstest.MapFS{"bad/SKILL.md": md("# bad\n")}, ""},
		{"frontmatter not on the first line", fstest.MapFS{"bad/SKILL.md": md("\n---\nname: bad\ndescription: x\n---\nbody\n")}, ""},
		{"unterminated frontmatter", fstest.MapFS{"bad/SKILL.md": md("---\nname: bad\ndescription: x\n# bad\n")}, ""},
		{"invalid yaml", fstest.MapFS{"bad/SKILL.md": md("---\nname: [bad\ndescription: x\n---\nbody\n")}, ""},
		{"empty frontmatter", fstest.MapFS{"bad/SKILL.md": md("---\n---\nbody\n")}, ""},
		{"missing name", fstest.MapFS{"bad/SKILL.md": md("---\ndescription: Use when.\n---\nbody\n")}, ""},
		{"missing description", fstest.MapFS{"bad/SKILL.md": md("---\nname: bad\n---\nbody\n")}, ""},
		{"blank description", fstest.MapFS{"bad/SKILL.md": md("---\nname: bad\ndescription: \"  \"\n---\nbody\n")}, ""},
		{"name differs from directory", fstest.MapFS{"bad/SKILL.md": skillMD("other", "")}, ""},
		{"unknown key", fstest.MapFS{"bad/SKILL.md": skillMD("bad", "toolkit_tool: [material]\n")}, ""},
		{"toolkit_tools not a list", fstest.MapFS{"bad/SKILL.md": skillMD("bad", "toolkit_tools: material\n")}, ""},
		{"empty body", fstest.MapFS{"bad/SKILL.md": md("---\nname: bad\ndescription: Use when.\n---\n\n")}, ""},
		{"missing SKILL.md", fstest.MapFS{"bad/notes.md": md("# notes\n")}, ""},
		{"sub-directory in a skill", fstest.MapFS{"bad/SKILL.md": skillMD("bad", ""), "bad/more/x.md": md("x\n")}, ""},
		{"non-markdown file in a skill", fstest.MapFS{"bad/SKILL.md": skillMD("bad", ""), "bad/notes.txt": md("x\n")}, ""},

		// go:embed's explicit */*.md also matches the dot and underscore names
		// a whole-directory embed would leave out: drafts and hidden files.
		{"skill directory starting with a dot", fstest.MapFS{".bad/SKILL.md": skillMD(".bad", "")}, `"." or "_"`},
		{"skill directory starting with an underscore", fstest.MapFS{"_bad/SKILL.md": skillMD("_bad", "")}, `"." or "_"`},
		{"section starting with a dot", fstest.MapFS{"bad/SKILL.md": skillMD("bad", ""), "bad/.draft.md": md("x\n")}, `"." or "_"`},
		{"section starting with an underscore", fstest.MapFS{"bad/SKILL.md": skillMD("bad", ""), "bad/_wip.md": md("x\n")}, `"." or "_"`},
		{"empty section name", fstest.MapFS{"bad/SKILL.md": skillMD("bad", ""), "bad/.md": md("x\n")}, "empty section name"},
		{"empty sub-document", fstest.MapFS{"bad/SKILL.md": skillMD("bad", ""), "bad/notes.md": md("\n \n")}, "notes.md is empty"},

		// A YAML "..." line, or a "--- " line with content, ends the
		// document; keys after it must not slip past the closed key set. The
		// first fails to parse as a second document, the second parses.
		{"keys after a YAML document end", fstest.MapFS{"bad/SKILL.md": skillMD("bad", "...\nbogus: 1\n")}, "content after the end of the YAML document"},
		{"second YAML document", fstest.MapFS{"bad/SKILL.md": skillMD("bad", "--- {bogus: 1}\n")}, "content after the end of the YAML document"},

		// Delimiters must be exact: other parsers read the same file once it
		// is materialized. The error names what is wrong.
		{"byte-order mark", fstest.MapFS{"bad/SKILL.md": md("\ufeff---\nname: bad\ndescription: x\n---\nbody\n")}, "byte-order mark"},
		{"trailing blank on the opening line", fstest.MapFS{"bad/SKILL.md": md("--- \nname: bad\ndescription: x\n---\nbody\n")}, "line 1: --- has trailing whitespace"},
		{"trailing tab on the closing line", fstest.MapFS{"bad/SKILL.md": md("---\nname: bad\ndescription: x\n---\t\nbody\n")}, "line 4: --- has trailing whitespace"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.files["a-good/SKILL.md"] = skillMD("a-good", "")
			got, err := load(c.files)
			if err == nil {
				t.Fatalf("load succeeded with %d skills; want an error for the broken skill", len(got))
			}
			if got != nil {
				t.Errorf("load returned %d skills alongside the error; want none", len(got))
			}
			if !strings.Contains(err.Error(), "bad") {
				t.Errorf("error %q does not name the broken skill", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not say %q", err, c.want)
			}
		})
	}
}

// Files outside a skill directory are not skills; finding one means the embed
// pattern changed without the loader, and that must not pass quietly either.
func TestLoadRejectsStrayTopLevelFile(t *testing.T) {
	got, err := load(fstest.MapFS{
		"a-good/SKILL.md": skillMD("a-good", ""),
		"z-stray.md":      md("# skills\n"),
	})
	if err == nil {
		t.Fatal("load accepted a top-level file")
	}
	if got != nil {
		t.Errorf("load returned %d skills alongside the error; want none", len(got))
	}
}

// The shipped skills carry only name and description, so toolkit_tools and
// unreal_classes must read as absent (nil), not as an empty list someone wrote
// down — and a list someone did write down empty stays empty, not absent. The
// body excludes the frontmatter, and a folded description reads as one line.
func TestLoadParsesFrontmatter(t *testing.T) {
	got, err := load(fstest.MapFS{
		"plain/SKILL.md": md("---\nname: plain\ndescription: >\n  Use when the\n  skill is plain.\n---\n\n# plain\n"),
		"full/SKILL.md": skillMD("full",
			"toolkit_tools: [material, asset_search]\nunreal_classes: [MaterialEditingLibrary]\n"),
		"empty/SKILL.md": skillMD("empty", "toolkit_tools: []\nunreal_classes: []\n"),
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d skills, want 3", len(got))
	}
	if names := []string{got[0].Name, got[1].Name, got[2].Name}; !reflect.DeepEqual(names, []string{"empty", "full", "plain"}) {
		t.Fatalf("got skills %q, want empty, full, plain in directory order", names)
	}
	empty, full, plain := got[0], got[1], got[2]

	if empty.ToolkitTools == nil || len(empty.ToolkitTools) != 0 {
		t.Errorf("empty.ToolkitTools = %#v, want an empty, non-nil list", empty.ToolkitTools)
	}
	if empty.UnrealClasses == nil || len(empty.UnrealClasses) != 0 {
		t.Errorf("empty.UnrealClasses = %#v, want an empty, non-nil list", empty.UnrealClasses)
	}

	if plain.ToolkitTools != nil {
		t.Errorf("plain.ToolkitTools = %#v, want nil (absent)", plain.ToolkitTools)
	}
	if plain.UnrealClasses != nil {
		t.Errorf("plain.UnrealClasses = %#v, want nil (absent)", plain.UnrealClasses)
	}
	if want := "Use when the skill is plain."; plain.Description != want {
		t.Errorf("plain.Description = %q, want %q", plain.Description, want)
	}
	if want := "# plain\n"; plain.Body != want {
		t.Errorf("plain.Body = %q, want %q", plain.Body, want)
	}

	if want := []string{"material", "asset_search"}; !reflect.DeepEqual(full.ToolkitTools, want) {
		t.Errorf("full.ToolkitTools = %#v, want %#v", full.ToolkitTools, want)
	}
	if want := []string{"MaterialEditingLibrary"}; !reflect.DeepEqual(full.UnrealClasses, want) {
		t.Errorf("full.UnrealClasses = %#v, want %#v", full.UnrealClasses, want)
	}
}

// .gitattributes keeps *.md at LF, but a working copy edited on Windows can
// still carry CRLF before it is committed; that must parse, not fail with a
// misleading "no frontmatter".
func TestLoadAcceptsCRLF(t *testing.T) {
	got, err := load(fstest.MapFS{
		"crlf/SKILL.md": md("---\r\nname: crlf\r\ndescription: Use when\r\n  CRLF.\r\n---\r\n\r\n# crlf\r\n"),
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s := got[0]; s.Description != "Use when CRLF." || s.Body != "# crlf\r\n" {
		t.Errorf("got Description %q, Body %q", s.Description, s.Body)
	}
}

// Source is what materialization writes, and other parsers read that file, so
// it must be SKILL.md exactly as embedded, not rebuilt from the parsed fields
// and Body. Each input holds something a rebuild would lose: a comment and a
// folded description in the frontmatter, blank lines after the closing ---
// (Body trims them), CRLF line endings.
func TestLoadKeepsSourceVerbatim(t *testing.T) {
	want := map[string]string{
		"lf":   "---\n# kept as written\nname: lf\ndescription: >\n  Use when the source\n  must survive.\ntoolkit_tools: [material]\n---\n\n\n# lf\n",
		"crlf": "---\r\nname: crlf\r\ndescription: Use when\r\n  CRLF.\r\n---\r\n\r\n# crlf\r\n",
	}
	files := fstest.MapFS{}
	for name, src := range want {
		files[name+"/SKILL.md"] = md(src)
	}
	got, err := load(files)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d skills, want %d", len(got), len(want))
	}
	for _, s := range got {
		if s.Source != want[s.Name] {
			t.Errorf("%s.Source = %q, want the SKILL.md input %q", s.Name, s.Source, want[s.Name])
		}
	}
}

// The same over the shipped set: every embedded skill's Source is its SKILL.md
// as go:embed holds it.
func TestLoadSourceIsTheEmbeddedFile(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("Load returned no skills")
	}
	for _, s := range all {
		raw, err := fs.ReadFile(shipped.FS, s.Name+"/SKILL.md")
		if err != nil {
			t.Errorf("%s: %v", s.Name, err)
			continue
		}
		if s.Source != string(raw) {
			t.Errorf("%s: Source (%d bytes) differs from the embedded SKILL.md (%d bytes)", s.Name, len(s.Source), len(raw))
		}
	}
}

// Sub-documents belong to the directory they sit in, keyed by section name
// (file name without .md); SKILL.md itself is the body, not a section.
func TestLoadAttachesSubDocumentsToTheirSkill(t *testing.T) {
	got, err := load(fstest.MapFS{
		"alpha/SKILL.md":  skillMD("alpha", ""),
		"alpha/nodes.md":  md("alpha nodes\n"),
		"alpha/pins.md":   md("alpha pins\n"),
		"beta/SKILL.md":   skillMD("beta", ""),
		"beta/graphs.md":  md("beta graphs\n"),
		"gamma/SKILL.md":  skillMD("gamma", ""),
		"delta/SKILL.md":  skillMD("delta", ""),
		"delta/nodes.md":  md("delta nodes\n"),
		"delta/README.md": md("delta readme\n"),
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := map[string]map[string]string{
		"alpha": {"nodes": "alpha nodes\n", "pins": "alpha pins\n"},
		"beta":  {"graphs": "beta graphs\n"},
		"gamma": nil,
		"delta": {"nodes": "delta nodes\n", "README": "delta readme\n"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d skills, want %d", len(got), len(want))
	}
	for _, s := range got {
		w, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected skill %q", s.Name)
			continue
		}
		if len(w) == 0 && len(s.Files) == 0 {
			continue
		}
		if !reflect.DeepEqual(s.Files, w) {
			t.Errorf("%s.Files = %#v, want %#v", s.Name, s.Files, w)
		}
	}
}

// DescribeAll is the selection surface of the skills MCP tool: every skill's
// name and description must be in it, one line each. Its size is logged
// because it sits in the agent's context on every request.
func TestDescribeAllListsEverySkill(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out := DescribeAll(all)
	for _, s := range all {
		if !strings.Contains(out, s.Name) {
			t.Errorf("DescribeAll lacks skill %q", s.Name)
		}
		if !strings.Contains(out, s.Description) {
			t.Errorf("DescribeAll lacks the description of %q", s.Name)
		}
	}
	if n := strings.Count(out, "\n"); n != len(all) {
		t.Errorf("DescribeAll has %d lines for %d skills", n, len(all))
	}
	t.Logf("DescribeAll: %d bytes for %d skills", len(out), len(all))
}

// The rendering itself, pinned: it is prompt text, and a format change should
// be a deliberate edit here, not an accident.
func TestDescribeAllFormat(t *testing.T) {
	got := DescribeAll([]Skill{
		{Name: "a", Description: "Use when a."},
		{Name: "b", Description: "Use when b."},
	})
	if want := "- a: Use when a.\n- b: Use when b.\n"; got != want {
		t.Errorf("DescribeAll = %q, want %q", got, want)
	}
	if got := DescribeAll(nil); got != "" {
		t.Errorf("DescribeAll(nil) = %q, want empty", got)
	}
}
