package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// The Build.version files of the two Launcher installs the k214 runs used.
const (
	buildVersion574 = `{
	"MajorVersion": 5,
	"MinorVersion": 7,
	"PatchVersion": 4,
	"Changelist": 51494982,
	"CompatibleChangelist": 47537391,
	"IsLicenseeVersion": 0,
	"IsPromotedBuild": 1,
	"BranchName": "++UE5+Release-5.7"
}`
	buildVersion584 = `{
	"MajorVersion": 5,
	"MinorVersion": 8,
	"PatchVersion": 4,
	"Changelist": 58902052,
	"CompatibleChangelist": 55116800,
	"IsLicenseeVersion": 0,
	"IsPromotedBuild": 1,
	"BranchName": "++UE5+Release-5.8"
}`
)

// engineDir writes an engine's Engine/Build/Build.version and returns its
// Engine directory.
func engineDir(t *testing.T, buildVersion string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "UE", "Engine")
	mustWriteFile(t, filepath.Join(dir, "Build", "Build.version"), buildVersion)
	return dir
}

func TestParseUEVersion(t *testing.T) {
	for _, tc := range []struct {
		text string
		want ueVersion
		ok   bool
	}{
		{"5.6.0", ueVersion{5, 6, 0, 0}, true},
		{"5.8.0-55116800+++UE5+Release-5.8", ueVersion{5, 8, 0, 55116800}, true},
		{"5.8.0+branch", ueVersion{5, 8, 0, 0}, true},
		{"5.6.0abc", ueVersion{5, 6, 0, 0}, true}, // the rest after the patch is not read
		{"5.6.0-", ueVersion{5, 6, 0, 0}, true},   // no digits: changelist 0
		{"5..0", ueVersion{5, 0, 0, 0}, true},     // no digits: minor 0
		{" 5.6.0", ueVersion{5, 6, 0, 0}, true},   // _tcstoui64 skips leading white space
		{"+5.6.0", ueVersion{5, 6, 0, 0}, true},   // and takes a sign
		{"5.8.0-2147483771", ueVersion{5, 8, 0, 0x8000007b}, true},
		{"+.6.0", ueVersion{}, false}, // no digit: nothing is read, the sign included
		{" .6.0", ueVersion{}, false}, // nor the white space
		{"5.6", ueVersion{}, false},   // two numbers do not parse; the engine loads the plugin
		{"5", ueVersion{}, false},
		{"", ueVersion{}, false},
		{"x.6.0", ueVersion{}, false},
		{"-1.0.0", ueVersion{}, false},           // negated: past the 16-bit range
		{"65536.0.0", ueVersion{}, false},        // past the 16-bit range
		{"5.6.0-4294967296", ueVersion{}, false}, // past the 32-bit range
	} {
		got, ok := parseUEVersion(tc.text)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseUEVersion(%q) = %+v, %v; want %+v, %v", tc.text, got, ok, tc.want, tc.ok)
		}
	}
}

// The engine side of the comparison: Build.version read into the compatible
// version, and GetNewest's verdict on what a plugin declares. The 5.7.4 and
// 5.8.4 rows with 5.6.0 and 5.7.0 were measured on k214.
func TestEngineAcceptsDeclaredVersion(t *testing.T) {
	const (
		licensee583 = `{"MajorVersion": 5, "MinorVersion": 8, "PatchVersion": 3, "Changelist": 0, "IsLicenseeVersion": 1}`
		source580   = `{"MajorVersion": 5, "MinorVersion": 8, "PatchVersion": 0, "Changelist": 0, "CompatibleChangelist": 0, "IsLicenseeVersion": 0}`
		// No CompatibleChangelist: UnrealBuildTool compiles the changelist in.
		noCompatible = `{"MajorVersion": 5, "MinorVersion": 8, "PatchVersion": 4, "Changelist": 58902052}`
		// EffectiveCompatibleChangelist needs both: here it is the changelist, 0.
		noChangelist = `{"MajorVersion": 5, "MinorVersion": 8, "PatchVersion": 4, "Changelist": 0, "CompatibleChangelist": 55116800}`
	)
	for _, tc := range []struct {
		name, buildVersion, declared string
		accepted                     bool
	}{
		{"5.6.0 on 5.7.4 (measured)", buildVersion574, "5.6.0", false},
		{"5.7.0 on 5.7.4 (measured)", buildVersion574, "5.7.0", true},
		{"5.6.0 on 5.8.4 (measured)", buildVersion584, "5.6.0", false},
		{"5.8.0 on 5.8.4", buildVersion584, "5.8.0", true},
		{"the patch counts: 5.8.4 on 5.8.4", buildVersion584, "5.8.4", false},
		{"a newer one is refused as well", buildVersion584, "5.9.0", false},
		{"another major", buildVersion584, "4.8.0", false},
		{"the compatible changelist", buildVersion584, "5.8.0-55116800", true},
		{"another changelist", buildVersion584, "5.8.0-58902052", false},
		{"a licensee changelist is not compared with Epic's", buildVersion584, "5.8.0-2147483771", true},
		{"no compatible changelist: the changelist", noCompatible, "5.8.0-58902052", true},
		{"no compatible changelist: another one is refused", noCompatible, "5.8.0-123", false},
		{"a compatible changelist without a changelist counts for nothing", noChangelist, "5.8.0-123", true},
		{"licensee build keeps its patch", licensee583, "5.8.3", true},
		{"licensee build refuses patch 0", licensee583, "5.8.0", false},
		{"engine without changelist compares none", source580, "5.8.0-12345", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, err := compatibleEngineVersion(engineDir(t, tc.buildVersion))
			if err != nil {
				t.Fatalf("compatibleEngineVersion: %v", err)
			}
			v, ok := parseUEVersion(tc.declared)
			if !ok {
				t.Fatalf("parseUEVersion(%q) failed", tc.declared)
			}
			if got := engine.accepts(v); got != tc.accepted {
				t.Errorf("engine %+v accepts %q = %v, want %v", engine, tc.declared, got, tc.accepted)
			}
		})
	}
}

func TestCompatibleEngineVersionErrors(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"not JSON", `{"MajorVersion": 5,`},
		{"no patch", `{"MajorVersion": 5, "MinorVersion": 8}`},
		{"a string", `{"MajorVersion": "5", "MinorVersion": 8, "PatchVersion": 4}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if v, err := compatibleEngineVersion(engineDir(t, tc.content)); err == nil {
				t.Errorf("no error, got %+v", v)
			}
		})
	}
	if _, err := compatibleEngineVersion(t.TempDir()); err == nil {
		t.Error("no error for an engine without Build/Build.version")
	}
}

func TestEngineDirOf(t *testing.T) {
	p := filepath.FromSlash
	// The engine root as config resolves it: forward slashes.
	const root = "C:/UE_5.8"
	for _, tc := range []struct{ name, editor, engineRoot, want string }{
		{"the editor's engine", p("D:/UE_5.7/Engine/Binaries/Win64/UnrealEditor.exe"), root, p("D:/UE_5.7/Engine")},
		{"case of the directories", p("D:/UE_5.7/engine/binaries/Win64/UnrealEditor.exe"), root, p("D:/UE_5.7/engine")},
		{"forward slashes", "D:/UE_5.7/Engine/Binaries/Win64/UnrealEditor.exe", root, p("D:/UE_5.7/Engine")},
		{"the default editor", root + "/Engine/Binaries/Win64/UnrealEditor.exe", root, p(root + "/Engine")},
		{"a name on PATH", "ping", root, p(root + "/Engine")},
		{"a wrapper", p("D:/UE_5.7/tools/editor.cmd"), root, p(root + "/Engine")},
		{"neither", "ping", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := engineDirOf(tc.editor, tc.engineRoot); got != tc.want {
				t.Errorf("engineDirOf(%q, %q) = %q, want %q", tc.editor, tc.engineRoot, got, tc.want)
			}
		})
	}
}

// The engine's discovery (FindPluginsInDirectory): a directory holding a
// .uplugin is a plugin and is not searched further; Content directories
// are skipped; the extension is compared case-sensitively.
func TestFindProjectPlugins(t *testing.T) {
	plugins := filepath.Join(t.TempDir(), "Plugins")
	for _, p := range []string{
		"A/A.uplugin",
		"A/Sub/Inner.uplugin", // below a plugin
		"Group/B/B.uplugin",   // nested
		"ue-llm-toolkit/Plugin/UELLMToolkit/UELLMToolkit.uplugin",
		"ue-llm-toolkit/Plugin/UELLMToolkit/Resources/mcp-bridge/node_modules/x/x.uplugin",
		"Content/C/C.uplugin", // a Content directory
		"D/content/D.uplugin", // in any case
		"E/E.UPLUGIN",         // not the extension
		"F/readme.txt",
	} {
		mustWriteFile(t, filepath.Join(plugins, filepath.FromSlash(p)), "{}")
	}
	var got []string
	for _, p := range findProjectPlugins(plugins) {
		rel, err := filepath.Rel(plugins, p)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.ToSlash(rel))
	}
	want := []string{"A/A.uplugin", "Group/B/B.uplugin", "ue-llm-toolkit/Plugin/UELLMToolkit/UELLMToolkit.uplugin"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("found\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := findProjectPlugins(filepath.Join(t.TempDir(), "Plugins")); len(got) != 0 {
		t.Errorf("no Plugins directory, found %v", got)
	}
}

// utf16File is text as a UTF-16 file with its byte order mark.
func utf16File(text string, bigEndian bool) string {
	var b []byte
	for _, u := range append([]uint16{0xfeff}, utf16.Encode([]rune(text))...) {
		if bigEndian {
			b = append(b, byte(u>>8), byte(u))
		} else {
			b = append(b, byte(u), byte(u>>8))
		}
	}
	return string(b)
}

func TestCheckPluginEngineVersions(t *testing.T) {
	const toolkit = "UELLMToolkit"
	plain := func(fields string) string {
		return `{"FileVersion": 3, "FriendlyName": "UE LLM Toolkit"` + fields + `}`
	}
	for _, tc := range []struct {
		name       string
		uplugin    string
		uproject   string            // "" = {}
		unattended bool              // args get the project in front and -unattended behind, as editorArgs builds them
		args       []string          // the editor's other arguments
		noEngine   bool              // the engine has no Build.version
		others     map[string]string // more .uplugin files, by their path below Plugins
		want       []string          // in the one line logged, which starts with the first; none = no line
		notWant    []string          // not in that line
	}{
		{name: "5.6.0 under -unattended", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'", `"5.6.0"`, "5.8.0-55116800", "-unattended", "the answer is No", "starts without it", "Remove", `"5.8.0"`, toolkit + ".uplugin"}},
		{name: "5.6.0 without -unattended", uplugin: plain(`, "EngineVersion": "5.6.0"`),
			want: []string{"WARN plugin 'UELLMToolkit'", `"5.6.0"`, "asks on every start", "waits for an answer", `"5.8.0"`}},
		{name: "the patch counts", uplugin: plain(`, "EngineVersion": "5.8.4"`), unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'", `"5.8.4"`, `set it to "5.8.0"`}},
		{name: "matching version", uplugin: plain(`, "EngineVersion": "5.8.0"`), unattended: true},
		{name: "no EngineVersion", uplugin: plain(``), unattended: true},
		{name: "empty EngineVersion", uplugin: plain(`, "EngineVersion": ""`), unattended: true},
		{name: "unparsable EngineVersion: the engine loads it", uplugin: plain(`, "EngineVersion": "5.6"`), unattended: true},
		{name: "a number is no version", uplugin: plain(`, "EngineVersion": 5.6`), unattended: true},
		{name: "malformed .uplugin", uplugin: `{"EngineVersion": "5.6.0"`, unattended: true},
		{name: "not an object", uplugin: `["EngineVersion", "5.6.0"]`, unattended: true},
		{name: "something after the object", uplugin: plain(`, "EngineVersion": "5.6.0"`) + ` {}`, unattended: true},
		{name: "empty file", uplugin: ``, unattended: true},
		{name: "trailing commas, as the engine reads them", uplugin: `{"Modules": [{"Name": "M",},], "EngineVersion": "5.6.0",}`, unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'", `"5.6.0"`}},
		{name: "a comma after an opening bracket is no trailing comma", uplugin: `{"Modules": [,], "EngineVersion": "5.6.0"}`, unattended: true},
		{name: "a comma in a string stays", uplugin: plain(`, "Description": "a,}", "EngineVersion": "5.6.0"`), unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "UTF-8 BOM", uplugin: "\ufeff" + plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "UTF-16 LE", uplugin: utf16File(plain(`, "EngineVersion": "5.6.0"`), false), unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "UTF-16 BE", uplugin: utf16File(plain(`, "EngineVersion": "5.6.0"`), true), unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "the key in another case", uplugin: plain(`, "engineversion": "5.6.0"`), unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "of two keys the first counts", uplugin: plain(`, "EngineVersion": "5.8.0", "engineVersion": "5.6.0"`), unattended: true},
		{name: "of two keys the first counts, refused", uplugin: plain(`, "EngineVersion": "5.6.0", "engineVersion": "5.8.0"`), unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'", `"5.6.0"`}},
		{name: "disabled in the .uproject", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "uellmtoolkit", "Enabled": false}]}`},
		{name: "the first .uproject entry counts", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": false}, {"Name": "UELLMToolkit", "Enabled": true}]}`},
		{name: "enabled in the .uproject", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "Other", "Enabled": false}, {"Name": "UELLMToolkit", "Enabled": true}]}`,
			want:     []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "disabled by default", uplugin: plain(`, "EnabledByDefault": false, "EngineVersion": "5.6.0"`), unattended: true},
		{name: "EnabledByDefault null is no JSON boolean", uplugin: plain(`, "EnabledByDefault": null, "EngineVersion": "5.6.0"`), unattended: true},
		{name: "a .uproject entry with Enabled null enables nothing", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": null}]}`},
		{name: "disabled by default, enabled in the .uproject", uplugin: plain(`, "EnabledByDefault": false, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": true}]}`,
			want:     []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "a malformed .uproject decides nothing", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [`,
			want:     []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "engine version unknown", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true, noEngine: true,
			want: []string{"WARN plugin 'UELLMToolkit'", `"5.6.0"`, "not checked", "Build.version", "under -unattended the answer is No"}},
		{name: "engine version unknown, without -unattended", uplugin: plain(`, "EngineVersion": "5.6.0"`), noEngine: true,
			want:    []string{"WARN plugin 'UELLMToolkit'", "not checked", "asks on every start"},
			notWant: []string{"unattended"}},
		{name: "engine version unknown, nothing declared", uplugin: plain(``), unattended: true, noEngine: true},

		// Enabled, EnabledByDefault and bIsPluginExtension count only as JSON
		// booleans: the engine converts other values, the launcher stays quiet.
		{name: "EnabledByDefault 1", uplugin: plain(`, "EnabledByDefault": 1, "EngineVersion": "5.6.0"`), unattended: true},
		{name: "EnabledByDefault as a string", uplugin: plain(`, "EnabledByDefault": "true", "EngineVersion": "5.6.0"`), unattended: true},
		{name: "a .uproject entry with Enabled as a string", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": "true"}]}`},
		{name: "a .uproject entry without Enabled", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit"}]}`},
		{name: "a plugin extension is merged into its parent", uplugin: plain(`, "bIsPluginExtension": true, "EngineVersion": "5.6.0"`), unattended: true},
		{name: "bIsPluginExtension 1", uplugin: plain(`, "bisPluginExtension": 1, "EngineVersion": "5.6.0"`), unattended: true},
		{name: "bIsPluginExtension null", uplugin: plain(`, "bIsPluginExtension": null, "EngineVersion": "5.6.0"`), unattended: true},
		{name: "EnabledByDefault 1, enabled in the .uproject", uplugin: plain(`, "EnabledByDefault": 1, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": true}]}`,
			want:     []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "bIsPluginExtension false", uplugin: plain(`, "bIsPluginExtension": false, "EngineVersion": "5.6.0"`), unattended: true,
			want: []string{"WARN plugin 'UELLMToolkit'"}},

		// The engine keeps one plugin per name; which one, the launcher does
		// not predict.
		{name: "a second plugin of the name", uplugin: plain(`, "Version": 1, "EngineVersion": "5.6.0"`), unattended: true,
			others: map[string]string{"new/UELLMToolkit/UELLMToolkit.uplugin": `{"FileVersion": 3, "Version": 2}`}},
		{name: "a second plugin of the name in another case", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			others: map[string]string{"x/uellmtoolkit/uellmtoolkit.uplugin": `{"FileVersion": 3, "EngineVersion": "5.6.0"}`}},
		{name: "a plugin of another name does not count", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			others: map[string]string{"Other/Other.uplugin": `{"FileVersion": 3}`},
			want:   []string{"WARN plugin 'UELLMToolkit'"}},

		// A .uproject entry with platform, target or configuration lists may
		// leave out the editor; the launcher does not evaluate them.
		{name: "a .uproject entry with a platform allow list", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": true, "PlatformAllowList": ["Mac"]}]}`},
		{name: "a .uproject entry with a deprecated list name in another case", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": true, "blacklisttargets": ["Editor"]}]}`},
		{name: "an optional .uproject entry", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": true, "Optional": true}]}`},
		{name: "a .uproject entry loading the plugin on demand", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": true, "TargetsToExplicitlyLoad": ["Editor"]}]}`},
		{name: "a limited entry decides the name: EnabledByDefault does not apply", uplugin: plain(`, "EnabledByDefault": true, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": true, "HasExplicitPlatforms": true}, {"Name": "UELLMToolkit", "Enabled": true}]}`},
		{name: "a later limited entry does not count", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": true}, {"Name": "UELLMToolkit", "Enabled": true, "TargetDenyList": ["Editor"]}]}`,
			want:     []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "SupportedTargetPlatforms does not limit the editor", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			uproject: `{"Plugins": [{"Name": "UELLMToolkit", "Enabled": true, "SupportedTargetPlatforms": ["Mac"]}]}`,
			want:     []string{"WARN plugin 'UELLMToolkit'"}},

		// The arguments of the editor: any DisablePlugins= turns the check
		// off where it would warn, -NoEnginePlugins turns it off entirely.
		{name: "-DisablePlugins=", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			args: []string{"-DisablePlugins=Other"},
			want: []string{"Plugins: the EngineVersion check is skipped", "-DisablePlugins="}},
		{name: "DisablePlugins= in any case and place", uplugin: plain(`, "EngineVersion": "5.6.0"`),
			args: []string{"-ExecCmds=x/disableplugins=Foo"},
			want: []string{"Plugins: the EngineVersion check is skipped"}},
		{name: "-DisablePlugins= with the engine version unknown", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true, noEngine: true,
			args: []string{"-DisablePlugins=Other"},
			want: []string{"Plugins: the EngineVersion check is skipped"}},
		{name: "-DisablePlugins= and nothing to warn about", uplugin: plain(`, "EngineVersion": "5.8.0"`), unattended: true,
			args: []string{"-DisablePlugins=Other"}},
		{name: "-EnablePlugins= is no -DisablePlugins=", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			args: []string{"-EnablePlugins=Other"},
			want: []string{"WARN plugin 'UELLMToolkit'"}},
		{name: "-NoEnginePlugins", uplugin: plain(`, "EngineVersion": "5.6.0"`), unattended: true,
			args: []string{"-NoEnginePlugins"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			project := filepath.Join(dir, "Demo.uproject")
			uproject := tc.uproject
			if uproject == "" {
				uproject = "{}"
			}
			mustWriteFile(t, project, uproject)
			mustWriteFile(t, filepath.Join(dir, "Plugins", "ue-llm-toolkit", "Plugin", toolkit, toolkit+".uplugin"), tc.uplugin)
			for rel, body := range tc.others {
				mustWriteFile(t, filepath.Join(dir, "Plugins", filepath.FromSlash(rel)), body)
			}
			engine := engineDir(t, buildVersion584)
			if tc.noEngine {
				engine = t.TempDir()
			}
			args := tc.args
			if tc.unattended {
				args = append([]string{project}, append(args, "-unattended")...)
			}
			var lines []string
			checkPluginEngineVersions(project, engine, args, func(s string) { lines = append(lines, s) })
			if len(tc.want) == 0 {
				if len(lines) != 0 {
					t.Fatalf("logged %q, want nothing", lines)
				}
				return
			}
			if len(lines) != 1 || !strings.HasPrefix(lines[0], tc.want[0]) {
				t.Fatalf("logged %q, want one line starting with %q", lines, tc.want[0])
			}
			for _, want := range tc.want {
				if !strings.Contains(lines[0], want) {
					t.Errorf("line lacks %q: %s", want, lines[0])
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(lines[0], notWant) {
					t.Errorf("line has %q: %s", notWant, lines[0])
				}
			}
		})
	}
}

// One line per refused plugin, none for the others.
func TestCheckPluginEngineVersionsSeveralPlugins(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "Demo.uproject")
	mustWriteFile(t, project, "{}")
	for name, version := range map[string]string{"Old": "5.6.0", "Fab": "5.7.0", "Fine": "5.8.0", "Plain": ""} {
		body := `{"FileVersion": 3}`
		if version != "" {
			body = `{"FileVersion": 3, "EngineVersion": "` + version + `"}`
		}
		mustWriteFile(t, filepath.Join(dir, "Plugins", name, name+".uplugin"), body)
	}
	var lines []string
	checkPluginEngineVersions(project, engineDir(t, buildVersion584), []string{"-unattended"}, func(s string) { lines = append(lines, s) })
	if len(lines) != 2 || !strings.Contains(lines[0], "'Fab'") || !strings.Contains(lines[1], "'Old'") {
		t.Fatalf("logged %q, want one WARN for Fab and one for Old, in that order", lines)
	}
}

func TestCheckPluginEngineVersionsWithoutProject(t *testing.T) {
	var lines []string
	checkPluginEngineVersions("", engineDir(t, buildVersion584), nil, func(s string) { lines = append(lines, s) })
	if len(lines) != 0 {
		t.Errorf("logged %q without a project", lines)
	}
	// A project without a Plugins directory is quiet as well, and does not
	// need the engine's version.
	dir := t.TempDir()
	project := filepath.Join(dir, "Demo.uproject")
	if err := os.WriteFile(project, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkPluginEngineVersions(project, "", nil, func(s string) { lines = append(lines, s) })
	if len(lines) != 0 {
		t.Errorf("logged %q for a project without plugins", lines)
	}
}
