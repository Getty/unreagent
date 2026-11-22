package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// launcherInstalledFixture is cut down from a real LauncherInstalled.dat
// (k211): engines next to marketplace and Fab plugins, which the Launcher
// lists under their own AppNames (one of them ending in _5.8), and an engine
// entry whose AppVersion lags behind the installed 5.8.4.
const launcherInstalledFixture = `{
	"InstallationList": [
		{
			"InstallLocation": "C:\\EpicGames\\UE_5.7",
			"NamespaceId": "89efe5924d3d467c839449ab6ab52e7f",
			"ItemId": "066124cd12464764b6b56e5d1a0567c4",
			"ArtifactId": "MagicNod90a5feb629feV16",
			"AppVersion": "5.7.0-48543606+++UE5+Dev-Marketplace-Windows",
			"AppName": "MagicNod90a5feb629feV16"
		},
		{
			"InstallLocation": "C:\\EpicGames\\UE_5.8",
			"NamespaceId": "ue",
			"ItemId": "c5c30696e4e0482f9c0a170873975230",
			"ArtifactId": "FabPlugin_5.8",
			"AppVersion": "5.8.0-55116800+++UE5+Release-5.8-0.0.22-60393117-Windows",
			"AppName": "FabPlugin_5.8"
		},
		{
			"InstallLocation": "C:\\EpicGames\\UE_5.7",
			"NamespaceId": "ue",
			"ItemId": "7299c1cd206b4bb58658a98199b66860",
			"ArtifactId": "UE_5.7",
			"AppVersion": "5.7.4-51494982+++UE5+Release-5.7-Windows",
			"AppName": "UE_5.7"
		},
		{
			"InstallLocation": "C:\\EpicGames\\UE_5.8",
			"NamespaceId": "ue",
			"ItemId": "18b3b415bc434c5b974f50488360ca31",
			"ArtifactId": "UE_5.8",
			"AppVersion": "5.8.3-58210709+++UE5+Release-5.8-Windows",
			"AppName": "UE_5.8"
		}
	]
}`

func TestParseLauncherInstalled(t *testing.T) {
	want := []engineCandidate{
		{"5.7", `C:\EpicGames\UE_5.7`},
		{"5.8", `C:\EpicGames\UE_5.8`}, // from AppName, not AppVersion's 5.8.3
	}
	if got := parseLauncherInstalled([]byte(launcherInstalledFixture)); !reflect.DeepEqual(got, want) {
		t.Errorf("parseLauncherInstalled(fixture) =\n%#v\nwant\n%#v", got, want)
	}
	if got := parseLauncherInstalled([]byte("\xef\xbb\xbf" + launcherInstalledFixture)); !reflect.DeepEqual(got, want) {
		t.Errorf("parseLauncherInstalled(fixture with utf-8 bom) =\n%#v\nwant\n%#v", got, want)
	}

	// Entries unreagent skips; the one good entry after them still counts.
	// The engine drops these too: no case-sensitive "UE_" prefix (ue_5.9,
	// XUE_5.9), an empty or missing InstallLocation or AppName. It keeps
	// "UE_" and "UE_Custom" as the identifiers "" and "Custom"; unreagent
	// skips them, since an association that is no version is looked up among
	// the source builds only. The rest are not an object of strings.
	skipped := `{"InstallationList": [
		{"AppName": "ue_5.9", "InstallLocation": "C:/lower/UE_5.9"},
		{"AppName": "UE_", "InstallLocation": "C:/bare"},
		{"AppName": "UE_Custom", "InstallLocation": "C:/custom"},
		{"AppName": "XUE_5.9", "InstallLocation": "C:/prefixed"},
		{"AppName": "UE_5.6", "InstallLocation": ""},
		{"AppName": "UE_5.6"},
		{"InstallLocation": "C:/no-name"},
		{"AppName": 5.6, "InstallLocation": "C:/number"},
		{"AppName": "UE_5.6", "InstallLocation": ["C:/array"]},
		"UE_5.6",
		null,
		{"AppName": "UE_5.10", "InstallLocation": "E:/UE/UE_5.10"}
	]}`
	want = []engineCandidate{{"5.10", "E:/UE/UE_5.10"}}
	if got := parseLauncherInstalled([]byte(skipped)); !reflect.DeepEqual(got, want) {
		t.Errorf("parseLauncherInstalled(skipped) =\n%#v\nwant\n%#v", got, want)
	}

	// Malformed: no candidates, so auto-detection goes on without the file.
	for _, in := range []string{
		"",
		"not json",
		`{"InstallationList": [{"AppName": "UE_5.8", "InstallLocation": "C:/UE_5.8"}`, // truncated
		`{}`,
		`{"InstallationList": null}`,
		`{"InstallationList": "UE_5.8"}`,
		`[{"AppName": "UE_5.8", "InstallLocation": "C:/UE_5.8"}]`,
	} {
		if got := parseLauncherInstalled([]byte(in)); len(got) != 0 {
			t.Errorf("parseLauncherInstalled(%q) = %#v; want none", in, got)
		}
	}
}

func TestLauncherInstalledPath(t *testing.T) {
	if got := launcherInstalledPath(""); got != "" {
		t.Errorf("launcherInstalledPath(\"\") = %q; want empty (no ProgramData, no file)", got)
	}
	root := t.TempDir()
	want := filepath.Join(root, "Epic", "UnrealEngineLauncher", "LauncherInstalled.dat")
	if got := launcherInstalledPath(root); got != want {
		t.Errorf("launcherInstalledPath(%q) = %q; want %q", root, got, want)
	}
}

// writeLauncherInstalled writes a LauncherInstalled.dat holding content under
// the ProgramData directory programData.
func writeLauncherInstalled(t *testing.T, programData, content string) {
	t.Helper()
	path := launcherInstalledPath(programData)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// launcherInstalledJSON renders a LauncherInstalled.dat listing entries, each
// an AppName and its InstallLocation.
func launcherInstalledJSON(t *testing.T, entries ...[2]string) string {
	t.Helper()
	type entry struct {
		InstallLocation, AppVersion, AppName string
	}
	var list struct{ InstallationList []entry }
	for _, e := range entries {
		list.InstallationList = append(list.InstallationList, entry{e[1], "0.0.0-0+++UE5+Release-Windows", e[0]})
	}
	b, err := json.MarshalIndent(list, "", "\t")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestLauncherEnginesOrder pins how the two Launcher sources combine: the
// LauncherInstalled.dat (what the engine reads) first, the HKLM registry
// subkeys after it as the fallback — for a version both list, the registry's
// directory counts only when the .dat's has no editor.
func TestLauncherEnginesOrder(t *testing.T) {
	programData := t.TempDir()
	writeLauncherInstalled(t, programData, launcherInstalledJSON(t,
		[2]string{"UE_5.8", "C:/dat/UE_5.8"},
		[2]string{"UE_5.9", "C:/dat/UE_5.9"},
	))
	registry := []engineCandidate{
		{"5.7", "C:/reg/UE_5.7"},
		{"5.8", "C:/reg/UE_5.8"},
	}
	launcher := launcherEngines(programData, registry)
	want := []engineCandidate{
		{"5.8", "C:/dat/UE_5.8"},
		{"5.9", "C:/dat/UE_5.9"},
		{"5.7", "C:/reg/UE_5.7"},
		{"5.8", "C:/reg/UE_5.8"},
	}
	if !reflect.DeepEqual(launcher, want) {
		t.Fatalf("launcherEngines =\n%#v\nwant\n%#v", launcher, want)
	}

	cases := []struct {
		name, assoc string
		hasEditor   func(string) bool
		want        string
	}{
		{"both list it: the .dat wins", "5.8", editorIn("C:/dat/UE_5.8", "C:/reg/UE_5.8"), "C:/dat/UE_5.8"},
		{"the .dat's has no editor: the registry's", "5.8", editorIn("C:/reg/UE_5.8"), "C:/reg/UE_5.8"},
		{"only the registry lists it", "5.7", editorIn("C:/dat/UE_5.8", "C:/reg/UE_5.7"), "C:/reg/UE_5.7"},
		{"no association, tie on the version: the .dat wins", "", editorIn("C:/dat/UE_5.8", "C:/reg/UE_5.8"), "C:/dat/UE_5.8"},
	}
	for _, c := range cases {
		got, warning := chooseEngine(c.assoc, launcher, nil, nil, c.hasEditor)
		if got != c.want || warning != "" {
			t.Errorf("%s: got %q, warning %q; want %q, none", c.name, got, warning, c.want)
		}
	}

	// Without a usable .dat the registry alone is what is left — the
	// detection as it was before the .dat was read.
	broken := t.TempDir()
	writeLauncherInstalled(t, broken, `{"InstallationList": [`)
	unreadable := t.TempDir()
	if err := os.MkdirAll(launcherInstalledPath(unreadable), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, pd := range map[string]string{
		"no ProgramData":      "",
		"no .dat":             t.TempDir(),
		"malformed .dat":      broken,
		".dat is a directory": unreadable,
	} {
		if got := launcherEngines(pd, registry); !reflect.DeepEqual(got, registry) {
			t.Errorf("%s: launcherEngines = %#v; want the registry's %#v", name, got, registry)
		}
	}
}

// TestLauncherInstalledOnDisk is the k211 machine as a test: the Launcher
// installed 5.7 and 5.8 and lists both in its LauncherInstalled.dat, but
// wrote an HKLM subkey only for 5.7, and neither directory matches a default
// path. A 5.8 project used to get 5.7 and a warning.
func TestLauncherInstalledOnDisk(t *testing.T) {
	root := t.TempDir()
	e57 := installTestEngine(t, filepath.Join(root, "EpicGames", "UE_5.7"))
	e58 := installTestEngine(t, filepath.Join(root, "EpicGames", "UE_5.8"))
	programData := filepath.Join(root, "ProgramData")
	writeLauncherInstalled(t, programData, launcherInstalledJSON(t,
		[2]string{"FabPlugin_5.8", e58},
		[2]string{"UE_5.7", e57},
		[2]string{"UE_5.8", e58},
	))
	registry := []engineCandidate{{"5.7", e57}}

	before, warning := chooseEngine("5.8", registry, nil, nil, hasEditor)
	if before != filepath.ToSlash(e57) || warning == "" {
		t.Fatalf("registry only: got %q, warning %q; want %q with a warning (the k211 bug as the baseline)", before, warning, filepath.ToSlash(e57))
	}

	launcher := launcherEngines(programData, registry)
	if got, warning := chooseEngine("5.8", launcher, nil, nil, hasEditor); got != filepath.ToSlash(e58) || warning != "" {
		t.Errorf("association 5.8: got %q, warning %q; want %q, none", got, warning, filepath.ToSlash(e58))
	}
	if got, warning := chooseEngine("5.7", launcher, nil, nil, hasEditor); got != filepath.ToSlash(e57) || warning != "" {
		t.Errorf("association 5.7: got %q, warning %q; want %q, none", got, warning, filepath.ToSlash(e57))
	}
	if got, _ := chooseEngine("", launcher, nil, nil, hasEditor); got != filepath.ToSlash(e58) {
		t.Errorf("no association: got %q; want the highest, %q", got, filepath.ToSlash(e58))
	}
}

// installTestEngine makes dir an engine root with an editor binary and
// returns it.
func installTestEngine(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "Engine", "Binaries", "Win64")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "UnrealEditor.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestAutodetectEngineReadsLauncherInstalled runs the whole auto-detection,
// the wiring included: %ProgramData% points at a temp directory whose
// LauncherInstalled.dat lists UE_99.9, and the project is associated with
// 99.9. No machine has a 99.9 in its registry or default paths, so only the
// .dat under this ProgramData can find it, and the host's engines cannot
// change the result.
func TestAutodetectEngineReadsLauncherInstalled(t *testing.T) {
	root := t.TempDir()
	engine := installTestEngine(t, filepath.Join(root, "Engines", "UE_99.9"))
	programData := filepath.Join(root, "ProgramData")
	writeLauncherInstalled(t, programData, launcherInstalledJSON(t, [2]string{"UE_99.9", engine}))
	t.Setenv("ProgramData", programData)
	project := filepath.Join(root, "Game.uproject")
	if err := os.WriteFile(project, []byte(`{"FileVersion": 3, "EngineAssociation": "99.9"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, warning := autodetectEngine(project); got != filepath.ToSlash(engine) || warning != "" {
		t.Errorf("autodetectEngine = %q, warning %q; want %q, none", got, warning, filepath.ToSlash(engine))
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
		if got := highestEngine(c.cands, editorIn(c.editor...)).Dir; got != c.want {
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

	// warn is the version of the fallback the warning names, "" for no
	// warning: one is due exactly when the association names an engine and
	// another one is chosen (k88). With nothing chosen, the launcher's own
	// "engine not found" warning says enough.
	cases := []struct {
		name      string
		assoc     string
		launcher  []engineCandidate
		hasEditor func(string) bool
		want      string
		warn      string
	}{
		{"no association: highest", "", launcher, all, "D:/Epic Games/UE_5.9", ""},
		{"association beats a higher version", "5.7", launcher, all, "C:/Program Files/Epic Games/UE_5.7", ""},
		{"association found via default path", "5.6", launcher, all, "D:/Epic Games/UE_5.6", ""},
		{"association found via default path without registry", "5.9", nil, all, "D:/Epic Games/UE_5.9", ""},
		{"association not installed: highest", "5.5", launcher, all, "D:/Epic Games/UE_5.9", "5.9"},
		{"association without editor: highest", "5.7", launcher, editorIn("C:/Program Files/Epic Games/UE_5.8"), "C:/Program Files/Epic Games/UE_5.8", "5.8"},
		{"source build guid", "{6A3B8C2E-1F4D-4E5A-9B7C-0D1E2F3A4B5C}", launcher, all, "D:/Source/UnrealEngine", ""},
		{"source build guid lower case, no braces", "6a3b8c2e-1f4d-4e5a-9b7c-0d1e2f3a4b5c", launcher, all, "D:/Source/UnrealEngine", ""},
		{"source build unknown: highest", "{00000000-0000-0000-0000-000000000000}", launcher, all, "D:/Epic Games/UE_5.9", "5.9"},
		{"nothing installed", "5.8", nil, editorIn(), "", ""},
	}
	for _, c := range cases {
		got, warning := chooseEngine(c.assoc, c.launcher, defaults, builds, c.hasEditor)
		if got != c.want {
			t.Errorf("%s: got %q; want %q", c.name, got, c.want)
		}
		if c.warn == "" {
			if warning != "" {
				t.Errorf("%s: warning %q; want none", c.name, warning)
			}
			continue
		}
		// "no usable engine", not "no installed engine": the case without an
		// editor binary has the associated engine installed.
		for _, w := range []string{`"` + c.assoc + `" names no usable engine`, c.warn + " at " + c.want, "engineRoot", "UE_ROOT"} {
			if !strings.Contains(warning, w) {
				t.Errorf("%s: warning %q does not mention %q", c.name, warning, w)
			}
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
	if got, _ := chooseEngine("", nil, found, nil, hasEditor); got != filepath.ToSlash(want) {
		t.Errorf("got %q; want %q", got, filepath.ToSlash(want))
	}
	e56 := filepath.ToSlash(filepath.Join(root, "E", "Epic Games", "UE_5.6"))
	if got, _ := chooseEngine("5.6", nil, found, nil, hasEditor); got != e56 {
		t.Errorf("association 5.6: got %q; want %q", got, e56)
	}
}
