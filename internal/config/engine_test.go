package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// editorIn returns a hasEditor stand-in that is true for exactly dirs.
func editorIn(dirs ...string) func(string) bool {
	set := map[string]bool{}
	for _, d := range dirs {
		set[d] = true
	}
	return func(dir string) bool { return set[dir] }
}

func TestParseEngineVersion(t *testing.T) {
	valid := map[string][]int{
		"5.8":   {5, 8},
		"5.10":  {5, 10},
		"5.8.1": {5, 8, 1},
		" 5.9 ": {5, 9},
		"4.27":  {4, 27},
	}
	for in, want := range valid {
		got, ok := parseEngineVersion(in)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("parseEngineVersion(%q) = %v, %v; want %v, true", in, got, ok, want)
		}
	}
	for _, in := range []string{
		"", "5", "5.", ".8", "5..8", "5.x", "+5.8", "-5.8", "5.9EA", "5.8 Preview",
		"{6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}", "UE_5.8", "5.99999999999999999999",
	} {
		if got, ok := parseEngineVersion(in); ok {
			t.Errorf("parseEngineVersion(%q) = %v, true; want not a version", in, got)
		}
	}
}

func TestCompareEngineVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"5.10", "5.9", 1}, // numeric, not lexical
		{"5.9", "5.10", -1},
		{"5.9", "5.8", 1},
		{"5.8", "5.8", 0},
		{"5.8", "5.8.0", 0},
		{"5.8.1", "5.8", 1},
		{"6.0", "5.10", 1},
		{"4.27", "5.0", -1},
	}
	for _, c := range cases {
		a, _ := parseEngineVersion(c.a)
		b, _ := parseEngineVersion(c.b)
		if got := compareEngineVersions(a, b); got != c.want {
			t.Errorf("compare(%s, %s) = %d; want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestClassifyAssociation(t *testing.T) {
	cases := []struct {
		in   string
		kind associationKind
		id   string
	}{
		{"", assocNone, ""},
		{"   ", assocNone, ""},
		{"{}", assocNone, ""},
		{"5.8", assocVersion, "5.8"},
		{" 5.10 ", assocVersion, "5.10"},
		{"{6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}", assocBuild, "6a3b8c2e-1f4d-4e5a-9b7c-0d1e2f3a4b5c"},
		{"6a3b8c2e-1f4d-4e5a-9b7c-0d1e2f3a4b5c", assocBuild, "6a3b8c2e-1f4d-4e5a-9b7c-0d1e2f3a4b5c"},
		{"My Fork", assocBuild, "my fork"},
	}
	for _, c := range cases {
		kind, id := classifyAssociation(c.in)
		if kind != c.kind || id != c.id {
			t.Errorf("classifyAssociation(%q) = %v, %q; want %v, %q", c.in, kind, id, c.kind, c.id)
		}
	}
}

func TestParseEngineAssociation(t *testing.T) {
	cases := []struct {
		name, json, want string
	}{
		{"launcher version", `{"FileVersion": 3, "EngineAssociation": "5.8", "Category": ""}`, "5.8"},
		{"source build", `{"EngineAssociation": "{6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}"}`, "{6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}"},
		{"utf-8 bom", "\xef\xbb\xbf{\"EngineAssociation\": \"5.9\"}", "5.9"},
		{"empty", `{"EngineAssociation": ""}`, ""},
		{"missing", `{"FileVersion": 3}`, ""},
		{"not a string", `{"EngineAssociation": 5.8}`, ""},
		{"invalid json", `{"EngineAssociation": "5.8",`, ""},
	}
	for _, c := range cases {
		if got := parseEngineAssociation([]byte(c.json)); got != c.want {
			t.Errorf("%s: got %q; want %q", c.name, got, c.want)
		}
	}
}

func TestReadEngineAssociation(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "Game.uproject")
	if err := os.WriteFile(project, []byte(`{"EngineAssociation": "5.8"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readEngineAssociation(project); got != "5.8" {
		t.Errorf("got %q; want 5.8", got)
	}
	if got := readEngineAssociation(""); got != "" {
		t.Errorf("no project: got %q; want empty", got)
	}
	if got := readEngineAssociation(filepath.Join(dir, "Missing.uproject")); got != "" {
		t.Errorf("unreadable project: got %q; want empty", got)
	}
}

// launcherQueryOutput mimics `reg query "HKLM\SOFTWARE\EpicGames\Unreal Engine" /s`
// (reg.exe prints the long root name and CRLF line ends).
const launcherQueryOutput = "\r\n" +
	"HKEY_LOCAL_MACHINE\\SOFTWARE\\EpicGames\\Unreal Engine\r\n" +
	"    Unrelated    REG_SZ    x\r\n" +
	"\r\n" +
	"HKEY_LOCAL_MACHINE\\SOFTWARE\\EpicGames\\Unreal Engine\\5.7\r\n" +
	"    InstalledDirectory    REG_SZ    C:\\Program Files\\Epic Games\\UE_5.7\r\n" +
	"\r\n" +
	"HKEY_LOCAL_MACHINE\\SOFTWARE\\EpicGames\\Unreal Engine\\5.8\r\n" +
	"    InstalledDirectory    REG_SZ    D:\\Epic Games\\UE_5.8\r\n" +
	"\r\n" +
	"HKEY_LOCAL_MACHINE\\SOFTWARE\\EpicGames\\Unreal Engine\\5.8\\Components\r\n" +
	"    InstalledDirectory    REG_SZ    D:\\not-an-engine\r\n" +
	"\r\n" +
	"HKEY_LOCAL_MACHINE\\SOFTWARE\\EpicGames\\Unreal Engine\\5.9\r\n" +
	"\r\n" +
	"HKEY_LOCAL_MACHINE\\SOFTWARE\\EpicGames\\Unreal Engine\\5.10\r\n" +
	"    installeddirectory    REG_EXPAND_SZ    E:\\UE\\UE_5.10\r\n" +
	"\r\n"

// buildsQueryOutput mimics `reg query "HKCU\Software\Epic Games\Unreal Engine\Builds"`.
const buildsQueryOutput = "\r\n" +
	"HKEY_CURRENT_USER\\Software\\Epic Games\\Unreal Engine\\Builds\r\n" +
	"    {6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}    REG_SZ    D:/Source/UnrealEngine\r\n" +
	"    My Fork    REG_SZ    E:/Fork/UE5\r\n" +
	"\tTabbed\tREG_SZ\tF:/Tabbed\r\n" +
	"    Empty    REG_SZ\r\n" +
	"\r\n"

func TestParseRegQuery(t *testing.T) {
	keys := parseRegQuery(buildsQueryOutput)
	want := []regKey{{
		Path: `HKEY_CURRENT_USER\Software\Epic Games\Unreal Engine\Builds`,
		Values: []regValue{
			{"{6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}", "D:/Source/UnrealEngine"},
			{"My Fork", "E:/Fork/UE5"},
			{"Tabbed", "F:/Tabbed"},
			{"Empty", ""},
		},
	}}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("parseRegQuery(builds) =\n%#v\nwant\n%#v", keys, want)
	}

	// The /v form of the query (one value) parses the same way.
	single := "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\EpicGames\\Unreal Engine\\5.7\r\n" +
		"    InstalledDirectory    REG_SZ    C:\\Program Files\\Epic Games\\UE_5.7\r\n\r\n"
	keys = parseRegQuery(single)
	if len(keys) != 1 || keys[0].value("InstalledDirectory") != `C:\Program Files\Epic Games\UE_5.7` {
		t.Errorf("parseRegQuery(single) = %#v", keys)
	}

	if keys := parseRegQuery(""); len(keys) != 0 {
		t.Errorf("parseRegQuery(empty) = %#v; want none", keys)
	}
}

func TestLauncherInstalls(t *testing.T) {
	got := launcherInstalls(parseRegQuery(launcherQueryOutput))
	want := []engineCandidate{
		{"5.7", `C:\Program Files\Epic Games\UE_5.7`},
		{"5.8", `D:\Epic Games\UE_5.8`},
		{"5.10", `E:\UE\UE_5.10`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("launcherInstalls =\n%#v\nwant\n%#v", got, want)
	}
}

func TestSourceBuilds(t *testing.T) {
	got := sourceBuilds(parseRegQuery(buildsQueryOutput))
	want := []engineCandidate{
		{"{6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}", "D:/Source/UnrealEngine"},
		{"My Fork", "E:/Fork/UE5"},
		{"Tabbed", "F:/Tabbed"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sourceBuilds =\n%#v\nwant\n%#v", got, want)
	}
}

func TestRegSubkeyName(t *testing.T) {
	cases := []struct {
		path, parent, name string
		ok                 bool
	}{
		{`HKEY_LOCAL_MACHINE\SOFTWARE\EpicGames\Unreal Engine\5.8`, launcherRegKey, "5.8", true},
		{`hkey_local_machine\software\epicgames\unreal engine\5.10`, launcherRegKey, "5.10", true},
		{`HKLM\SOFTWARE\EpicGames\Unreal Engine\5.8\`, launcherRegKey, "5.8", true},
		{`HKEY_LOCAL_MACHINE\SOFTWARE\EpicGames\Unreal Engine`, launcherRegKey, "", false},
		{`HKEY_LOCAL_MACHINE\SOFTWARE\EpicGames\Unreal Engine\5.8\Components`, launcherRegKey, "", false},
		{`HKEY_CURRENT_USER\SOFTWARE\EpicGames\Unreal Engine\5.8`, launcherRegKey, "", false},
		{`HKEY_LOCAL_MACHINE\SOFTWARE\EpicGames\Unreal Engine Extra\5.8`, launcherRegKey, "", false},
	}
	for _, c := range cases {
		name, ok := regSubkeyName(c.path, c.parent)
		if name != c.name || ok != c.ok {
			t.Errorf("regSubkeyName(%q) = %q, %v; want %q, %v", c.path, name, ok, c.name, c.ok)
		}
	}
}

func TestHighestEngine(t *testing.T) {
	cases := []struct {
		name   string
		cands  []engineCandidate
		editor []string
		want   string
	}{
		{
			// Sorting full paths in reverse put E:/…/UE_5.6 above C:/…/UE_5.8.
			name:   "cross drive",
			cands:  []engineCandidate{{"5.8", "C:/Program Files/Epic Games/UE_5.8"}, {"5.6", "E:/Program Files/Epic Games/UE_5.6"}},
			editor: []string{"C:/Program Files/Epic Games/UE_5.8", "E:/Program Files/Epic Games/UE_5.6"},
			want:   "C:/Program Files/Epic Games/UE_5.8",
		},
		{
			name:   "5.10 above 5.9",
			cands:  []engineCandidate{{"5.9", "C:/UE_5.9"}, {"5.10", "C:/UE_5.10"}, {"5.8", "C:/UE_5.8"}},
			editor: []string{"C:/UE_5.9", "C:/UE_5.10", "C:/UE_5.8"},
			want:   "C:/UE_5.10",
		},
		{
			name:   "no editor, no pick",
			cands:  []engineCandidate{{"5.10", "C:/UE_5.10"}, {"5.8", "C:/UE_5.8"}},
			editor: []string{"C:/UE_5.8"},
			want:   "C:/UE_5.8",
		},
		{
			name:   "tie keeps the first (registry before default path)",
			cands:  []engineCandidate{{"5.8", "C:/reg/UE_5.8"}, {"5.8", "D:/glob/UE_5.8"}},
			editor: []string{"C:/reg/UE_5.8", "D:/glob/UE_5.8"},
			want:   "C:/reg/UE_5.8",
		},
		{
			name:   "unparsable version ranks last",
			cands:  []engineCandidate{{"Custom", "C:/UE_Custom"}, {"5.4", "C:/UE_5.4"}},
			editor: []string{"C:/UE_Custom", "C:/UE_5.4"},
			want:   "C:/UE_5.4",
		},
		{
			name:   "unparsable version still beats nothing",
			cands:  []engineCandidate{{"Custom", "C:/UE_Custom"}},
			editor: []string{"C:/UE_Custom"},
			want:   "C:/UE_Custom",
		},
		{
			name:  "nothing installed",
			cands: []engineCandidate{{"5.8", "C:/UE_5.8"}},
			want:  "",
		},
	}
	for _, c := range cases {
		if got := highestEngine(c.cands, editorIn(c.editor...)); got != c.want {
			t.Errorf("%s: got %q; want %q", c.name, got, c.want)
		}
	}
}

func TestChooseEngine(t *testing.T) {
	launcher := []engineCandidate{
		{"5.7", "C:/Program Files/Epic Games/UE_5.7"},
		{"5.8", "C:/Program Files/Epic Games/UE_5.8"},
	}
	defaults := []engineCandidate{
		{"5.9", "D:/Epic Games/UE_5.9"},
		{"5.6", "D:/Epic Games/UE_5.6"},
	}
	builds := []engineCandidate{
		{"{6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}", "D:/Source/UnrealEngine"},
	}
	all := editorIn(
		"C:/Program Files/Epic Games/UE_5.7", "C:/Program Files/Epic Games/UE_5.8",
		"D:/Epic Games/UE_5.9", "D:/Epic Games/UE_5.6", "D:/Source/UnrealEngine",
	)

	cases := []struct {
		name      string
		assoc     string
		launcher  []engineCandidate
		hasEditor func(string) bool
		want      string
	}{
		{"no association: highest", "", launcher, all, "D:/Epic Games/UE_5.9"},
		{"association beats a higher version", "5.7", launcher, all, "C:/Program Files/Epic Games/UE_5.7"},
		{"association found via default path", "5.6", launcher, all, "D:/Epic Games/UE_5.6"},
		{"association found via default path without registry", "5.9", nil, all, "D:/Epic Games/UE_5.9"},
		{"association not installed: highest", "5.5", launcher, all, "D:/Epic Games/UE_5.9"},
		{"association without editor: highest", "5.7", launcher, editorIn("C:/Program Files/Epic Games/UE_5.8"), "C:/Program Files/Epic Games/UE_5.8"},
		{"source build guid", "{6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}", launcher, all, "D:/Source/UnrealEngine"},
		{"source build guid lower case, no braces", "6a3b8c2e-1f4d-4e5a-9b7c-0d1e2f3a4b5c", launcher, all, "D:/Source/UnrealEngine"},
		{"source build unknown: highest", "{00000000-0000-0000-0000-000000000000}", launcher, all, "D:/Epic Games/UE_5.9"},
		{"nothing installed", "5.8", nil, editorIn(), ""},
	}
	for _, c := range cases {
		if got := chooseEngine(c.assoc, c.launcher, defaults, builds, c.hasEditor); got != c.want {
			t.Errorf("%s: got %q; want %q", c.name, got, c.want)
		}
	}
}

// TestDefaultPathsOnDisk runs the default-path half of auto-detection against
// real directories: two "drives", the older engine on the later one, plus a
// 5.10 next to a 5.9 and an engine without an editor binary.
func TestDefaultPathsOnDisk(t *testing.T) {
	root := t.TempDir()
	mkEngine := func(rel string, editor bool) string {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		bin := filepath.Join(dir, "Engine", "Binaries", "Win64")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if editor {
			if err := os.WriteFile(filepath.Join(bin, "UnrealEditor.exe"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	mkEngine("C/Epic Games/UE_5.9", true)
	want := mkEngine("C/Epic Games/UE_5.10", true)
	mkEngine("C/Epic Games/UE_5.11", false)
	mkEngine("E/Epic Games/UE_5.6", true)
	patterns := []string{
		filepath.ToSlash(root) + "/C/Epic Games/UE_*",
		filepath.ToSlash(root) + "/E/Epic Games/UE_*",
	}

	found := globEngines(patterns)
	if len(found) != 4 {
		t.Fatalf("globEngines found %d dirs, want 4: %#v", len(found), found)
	}
	for _, c := range found {
		if c.ID != filepath.Base(c.Dir)[len("UE_"):] {
			t.Errorf("ID %q does not match dir %q", c.ID, c.Dir)
		}
	}
	if got := chooseEngine("", nil, found, nil, hasEditor); got != filepath.ToSlash(want) {
		t.Errorf("got %q; want %q", got, filepath.ToSlash(want))
	}
	e56 := filepath.ToSlash(filepath.Join(root, "E", "Epic Games", "UE_5.6"))
	if got := chooseEngine("5.6", nil, found, nil, hasEditor); got != e56 {
		t.Errorf("association 5.6: got %q; want %q", got, e56)
	}
}
