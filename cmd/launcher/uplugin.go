package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// The editor refuses a plugin whose .uplugin declares an EngineVersion its
// engine does not accept, and under -unattended it does so without a word
// on the console: the plugin's "load it anyway?" question is answered No
// and the editor starts without it (k214; for the UE LLM Toolkit the bridge
// port then never opens). checkPluginEngineVersions predicts that refusal
// from the files the engine reads, before the editor starts.
//
// Engine source read for this, UE 5.8.4 (E/ = Engine/Source/):
//   - E/Runtime/Projects/Private/PluginManager.cpp 1859-1864: every enabled
//     plugin goes through IsPluginCompatible (2688-2707); on false,
//     PromptToLoadIncompatiblePlugin (2709-2715) asks a Yes/No question, and
//     No skips the plugin ("Skipping load of").
//   - E/Runtime/Core/Private/Misc/MessageDialog.cpp 89-135 and 153-174: a
//     Yes/No question defaults to No, and under FApp::IsUnattended (App.cpp
//     274-280: -unattended) no box is shown and the default is the answer.
//   - E/Runtime/Core/Private/Misc/EngineVersion.cpp 162-197 (Parse), 56-91
//     (GetNewest), 199-208 (CompatibleWith): see parseUEVersion, accepts and
//     compatibleEngineVersion.
//
// Which plugins: those FindPluginsInDirectory (PluginManager.cpp 1120-1225)
// finds under <project>/Plugins (findProjectPlugins) that the editor
// enables (pluginEnabled). Where the launcher cannot tell, it stays quiet,
// so these are never warned about, and a refusal among them goes unnoticed:
//   - every plugin when the editor's arguments carry -DisablePlugins= or
//     -NoEnginePlugins (see checkPluginEngineVersions), and a plugin only
//     -EnablePlugins= or -EnableAllPlugins enables;
//   - a plugin whose first .uproject entry carries a field that can keep it
//     from the editor (referenceLimits) or an Enabled that is no JSON
//     boolean; one without such an entry whose EnabledByDefault is no JSON
//     boolean; a plugin extension, which the engine merges into its parent
//     plugin (1266-1270, then 946 on), and one whose bIsPluginExtension is
//     no JSON boolean;
//   - a name found more than once under <project>/Plugins: the engine keeps
//     one of them by descriptor Version (CreatePluginObject, 1261-1328), and
//     a .uproject Version can pick another (2440-2472);
//   - a plugin enabled only as another plugin's dependency, plugins outside
//     <project>/Plugins (the engine's, platform extension directories, Mods,
//     AdditionalPluginDirectories), a directory reached through a junction
//     or symlink, and a file the launcher cannot read or parse.
//
// What can still warn wrongly: the target's own plugin list (its receipt,
// or the lists UnrealBuildTool compiles in; 1614-1702), which the engine
// applies after the command line and before the .uproject, so a plugin the
// target disables is still checked; a plugin of the same name in another
// plugin directory, which can take the name over (the engine keeps the
// higher descriptor Version, else the one found first, 1300-1326; an engine
// plugin it gives up for the project's, 1280-1285); an engine whose
// Build.version and Version.h disagree (compatibleEngineVersion); and an
// unreal.editor not of the <root>/Engine/Binaries/<platform>/<exe> shape (a
// wrapper script, a bare name on PATH), for which engineDirOf takes the
// resolved engine root's Build.version: if that editor starts another
// engine, the check can warn wrongly or miss a refusal.

// checkPluginEngineVersions logs one WARN per project plugin the editor in
// engineDir (its Engine directory, engineDirOf), started with args, will
// refuse for the EngineVersion its .uplugin declares; -unattended among the
// args decides what the refusal looks like. The engine's version is read
// only once a plugin declares one; if it cannot be read, one WARN names the
// first such plugin and the cause, and nothing is checked. No project, no
// plugins or no engine directory: nothing to say.
//
// The editor's command line is not modelled. With -NoEnginePlugins the
// editor enables no project plugin by default or through the .uproject
// (FindTargetPlugins, PluginManager.cpp 1607-1759): nothing is checked. An
// argument that holds "DisablePlugins=", in any case, may switch off any
// plugin (FindCommandLinePlugins, 1469-1602): where a warning would follow,
// one plain line says the check is skipped, and nothing more is said.
func checkPluginEngineVersions(project, engineDir string, args []string, logger func(string)) {
	if project == "" || engineDir == "" || hasEditorSwitch(args, "NoEnginePlugins") {
		return
	}
	plugins := findProjectPlugins(filepath.Join(filepath.Dir(project), "Plugins"))
	if len(plugins) == 0 {
		return
	}
	disablePlugins := false
	for _, a := range args {
		if strings.Contains(strings.ToLower(a), "disableplugins=") {
			disablePlugins = true
		}
	}
	refusal := "the editor asks on every start whether to load it anyway and waits for an answer (No starts it without the plugin)"
	if hasEditorSwitch(args, "unattended") {
		refusal = "the editor asks whether to load it anyway, and under -unattended the answer is No: the editor starts without it"
	}
	found := map[string]int{} // plugin names compare without regard to case
	for _, path := range plugins {
		found[strings.ToLower(pluginName(path))]++
	}
	switches := projectPluginSwitches(project)
	var engine ueVersion
	var engineErr error
	engineRead := false
	for _, path := range plugins {
		name := pluginName(path)
		if found[strings.ToLower(name)] > 1 {
			continue
		}
		declared, enabledByDefault, ok := readPluginDescriptor(path)
		if !ok || !pluginEnabled(name, enabledByDefault, switches) {
			continue
		}
		v, ok := parseUEVersion(declared)
		if !ok {
			continue // none declared, or the engine cannot parse it: it loads the plugin
		}
		if !engineRead {
			engine, engineErr = compatibleEngineVersion(engineDir)
			engineRead = true
		}
		if engineErr == nil && engine.accepts(v) {
			continue
		}
		switch {
		case disablePlugins:
			logger("Plugins: the EngineVersion check is skipped: the editor's arguments carry -DisablePlugins=, which the launcher does not read.")
			return
		case engineErr != nil:
			logger(fmt.Sprintf("WARN plugin '%s' declares EngineVersion %q, which is not checked: the engine's version is unknown (%v). If it does not match the engine, %s.", name, declared, engineErr, refusal))
			return
		}
		logger(fmt.Sprintf("WARN plugin '%s': its EngineVersion %q does not match the engine's compatible version %s, so %s. Remove \"EngineVersion\" from %s, or set it to %q.",
			name, declared, engine, refusal, path, engine.short()))
	}
}

// pluginName is the name the engine gives the plugin of a .uplugin file.
func pluginName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".uplugin")
}

// engineDirOf is the Engine directory whose plugin manager decides: the one
// the editor binary sits in (<root>/Engine/Binaries/<platform>/<exe>), else
// that of the engine root config resolved; "" if neither is known. The
// editor counts first: unreal.editor may name a binary of another engine.
func engineDirOf(editor, engineRoot string) string {
	binaries := filepath.Dir(filepath.Dir(editor))
	if engine := filepath.Dir(binaries); strings.EqualFold(filepath.Base(binaries), "Binaries") && strings.EqualFold(filepath.Base(engine), "Engine") {
		return engine
	}
	if engineRoot == "" {
		return ""
	}
	return filepath.Join(engineRoot, "Engine")
}

// findProjectPlugins lists the .uplugin files under dir the way the engine
// discovers plugins (FindPluginsInDirectory, PluginManager.cpp 1120-1225): a
// directory holding a file whose name ends in ".uplugin" (case-sensitive) is
// a plugin, and nothing below it is searched; directories named Content, in
// any case, are skipped. Of several .uplugin files in one directory the
// engine takes the first its directory listing returns, in the platform's
// order (1162-1167); this takes the first in name order, which may differ.
// The engine walks breadth-first; the order here is depth-first by name.
// Only real directories are entered (see the file comment); an unreadable
// one is passed over.
func findProjectPlugins(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".uplugin") {
			return []string{filepath.Join(dir, e.Name())}
		}
	}
	var found []string
	for _, e := range entries {
		if e.IsDir() && !strings.EqualFold(e.Name(), "Content") {
			found = append(found, findProjectPlugins(filepath.Join(dir, e.Name()))...)
		}
	}
	return found
}

// readPluginDescriptor reads a .uplugin's EngineVersion ("" when it is
// absent or no string) and EnabledByDefault: true when absent, as a project
// plugin that sets nothing is enabled (FPlugin::IsEnabledByDefault,
// PluginManager.cpp 421-435), and false when it is there but no JSON
// boolean, which the engine would convert and the launcher does not, so
// only a .uproject entry can enable such a plugin here. ok is false, and the
// plugin passed over, when the file cannot be read or parsed, for a plugin
// extension, and when bIsPluginExtension is there but no JSON boolean.
//
// The engine looks a member up by name without regard to case and takes
// the first match (RapidJsonPluginLoading.cpp 15-33). A number or a
// boolean it converts to a string (192-234), which never parses as a
// version, so only a string can be refused.
func readPluginDescriptor(path string) (engineVersion string, enabledByDefault, ok bool) {
	data, err := readUEJSON(path)
	if err != nil {
		return "", false, false
	}
	m, err := firstMembers(data, "EngineVersion", "EnabledByDefault", "bIsPluginExtension")
	if err != nil {
		return "", false, false
	}
	enabledByDefault, _ = boolMember(m, "EnabledByDefault", true)
	if extension, isBool := boolMember(m, "bIsPluginExtension", false); !isBool || extension {
		return "", false, false
	}
	engineVersion, _ = jsonString(m["EngineVersion"])
	return engineVersion, enabledByDefault, true
}

// jsonString is raw as a string if it is a JSON string. null reads as "",
// which names no plugin and declares no version.
func jsonString(raw json.RawMessage) (s string, ok bool) {
	return s, json.Unmarshal(raw, &s) == nil
}

// jsonBool is raw as a boolean if it is a JSON boolean; null, a missing
// value and any other type are none.
func jsonBool(raw json.RawMessage) (v, ok bool) {
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// boolMember is the member name of m as a JSON boolean, def when there is
// none; ok false when it is there but no JSON boolean.
func boolMember(m map[string]json.RawMessage, name string, def bool) (v, ok bool) {
	raw, present := m[name]
	if !present {
		return def, true
	}
	return jsonBool(raw)
}

// projectPluginSwitches reads the Plugins entries of the .uproject: the
// lower-cased name of each to whether it enables the plugin, the first
// entry per name winning, as the engine configures them (PluginManager.cpp
// 1704-1737; names compare without regard to case). An entry whose Enabled
// is no JSON boolean, or that carries one of referenceLimits, counts as not
// enabling it. The engine converts a number or a string, refuses a missing
// Enabled (PluginReferenceDescriptor.cpp 137-141), and ignores an entry
// whose lists leave out the editor's platform, target or configuration while
// the name counts as configured all the same, so EnabledByDefault does not
// apply either (PluginManager.cpp 2385-2389, 2424-2429, 2544-2548;
// 1708-1715). The launcher evaluates none of that and stays quiet about such
// a plugin: it can miss a refusal, not warn about a plugin the editor does
// not enable. An entry whose Name is no string decides nothing; neither
// does a .uproject that cannot be read.
func projectPluginSwitches(project string) map[string]bool {
	switches := map[string]bool{}
	data, err := readUEJSON(project)
	if err != nil {
		return switches
	}
	m, err := firstMembers(data, "Plugins")
	if err != nil {
		return switches
	}
	var refs []json.RawMessage
	if json.Unmarshal(m["Plugins"], &refs) != nil {
		return switches
	}
	for _, raw := range refs {
		ref, err := firstMembers(raw, append([]string{"Name", "Enabled"}, referenceLimits...)...)
		if err != nil {
			continue
		}
		name, isString := jsonString(ref["Name"])
		if !isString {
			continue
		}
		enabled, _ := jsonBool(ref["Enabled"])
		for _, field := range referenceLimits {
			if _, limited := ref[field]; limited {
				enabled = false
			}
		}
		if _, seen := switches[strings.ToLower(name)]; !seen {
			switches[strings.ToLower(name)] = enabled
		}
	}
	return switches
}

// referenceLimits are the fields of a plugin reference that can keep the
// engine from enabling it for the editor although Enabled is true
// (PluginReferenceDescriptor.cpp 143-169, read by the checks at 41-108):
// Optional (an optional reference to a plugin the target did not build is
// ignored, PluginManager.cpp 2544-2548), the platform, target and
// configuration allow and deny lists, each also under its deprecated
// Whitelist/Blacklist name, HasExplicitPlatforms (true without an allow
// list leaves out every platform) and TargetsToExplicitlyLoad (a listed
// target loads the plugin only on demand, 2385-2389).
// SupportedTargetPlatforms is not among them: the editor ignores it
// (LOAD_PLUGINS_FOR_TARGET_PLATFORMS, 2431; Projects.Build.cs 54-61;
// bIncludePluginsForTargetPlatforms is true for an editor target).
var referenceLimits = []string{
	"Optional",
	"PlatformAllowList", "WhitelistPlatforms",
	"PlatformDenyList", "BlacklistPlatforms",
	"TargetConfigurationAllowList", "WhitelistTargetConfigurations",
	"TargetConfigurationDenyList", "BlacklistTargetConfigurations",
	"TargetAllowList", "WhitelistTargets",
	"TargetDenyList", "BlacklistTargets",
	"HasExplicitPlatforms", "TargetsToExplicitlyLoad",
}

// pluginEnabled reports whether the editor enables the project plugin name
// as far as the launcher can tell: as its first .uproject entry says
// (projectPluginSwitches; PluginManager.cpp 1721-1737), else as its
// EnabledByDefault says (1741-1756). The engine configures the command line
// and the target's plugin list first (ConfigureEnabledPlugins, 2042-2053;
// 1469-1602, 1614-1702), the first source to name a plugin deciding; see
// checkPluginEngineVersions for the command line, and the file comment.
func pluginEnabled(name string, enabledByDefault bool, switches map[string]bool) bool {
	if enabled, listed := switches[strings.ToLower(name)]; listed {
		return enabled
	}
	return enabledByDefault
}

// ueVersion is an FEngineVersionBase without the branch, which no
// comparison here reads. Bit 31 of changelist marks a licensee changelist
// (EngineVersion.cpp 34-44).
type ueVersion struct {
	major, minor, patch uint16
	changelist          uint32
}

func (v ueVersion) cl() uint32     { return v.changelist & 0x7fffffff }
func (v ueVersion) licensee() bool { return v.changelist&0x80000000 != 0 }
func (v ueVersion) short() string  { return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch) }
func (v ueVersion) String() string {
	if v.cl() == 0 {
		return v.short()
	}
	return fmt.Sprintf("%s-%d", v.short(), v.cl())
}

// accepts reports whether an engine whose compatible version is engine
// loads a plugin declaring v: FEngineVersion::GetNewest of the two must find
// neither newer (IsPluginCompatible). Major, minor and patch must be equal;
// the changelists are compared only when both carry one and both are, or
// both are not, licensee changelists (EngineVersion.cpp 56-91).
func (engine ueVersion) accepts(v ueVersion) bool {
	if engine.major != v.major || engine.minor != v.minor || engine.patch != v.patch {
		return false
	}
	return engine.licensee() != v.licensee() || engine.cl() == 0 || v.cl() == 0 || engine.cl() == v.cl()
}

// parseUEVersion parses an EngineVersion as FEngineVersion::Parse does
// (EngineVersion.cpp 162-197): three numbers separated by ".", an optional
// "-" and changelist; the rest (a "+" and branch, or anything else) is not
// read. Each number is read as _tcstoui64 reads it (strtou), so a part
// without digits is 0. It fails on a missing "." and on a number past
// 16 bits (32 for the changelist).
func parseUEVersion(text string) (ueVersion, bool) {
	major, rest := strtou(text)
	if major > math.MaxUint16 || !strings.HasPrefix(rest, ".") {
		return ueVersion{}, false
	}
	minor, rest := strtou(rest[1:])
	if minor > math.MaxUint16 || !strings.HasPrefix(rest, ".") {
		return ueVersion{}, false
	}
	patch, rest := strtou(rest[1:])
	if patch > math.MaxUint16 {
		return ueVersion{}, false
	}
	var cl uint64
	if strings.HasPrefix(rest, "-") {
		if cl, _ = strtou(rest[1:]); cl > math.MaxUint32 {
			return ueVersion{}, false
		}
	}
	return ueVersion{uint16(major), uint16(minor), uint16(patch), uint32(cl)}, true
}

// strtou reads a decimal number at the start of s as the C runtime's
// _wcstoui64 does: white space and one sign may lead, a minus negates the
// value (modulo 2^64), and a value past 64 bits is the largest one. Without
// a digit the value is 0 and rest is all of s.
func strtou(s string) (v uint64, rest string) {
	i := 0
	for i < len(s) && strings.IndexByte(" \t\n\v\f\r", s[i]) >= 0 {
		i++
	}
	neg := i < len(s) && s[i] == '-'
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	start, overflow := i, false
	for ; i < len(s) && '0' <= s[i] && s[i] <= '9'; i++ {
		d := uint64(s[i] - '0')
		if v > (math.MaxUint64-d)/10 {
			overflow = true
		}
		v = v*10 + d
	}
	switch {
	case i == start:
		return 0, s
	case overflow:
		return math.MaxUint64, s[i:]
	case neg:
		return -v, s[i:]
	}
	return v, s[i:]
}

// compatibleEngineVersion is FEngineVersion::CompatibleWith() of the engine
// in engineDir (EngineVersion.cpp 199-208): its major and minor, the patch
// only on a licensee build (0 otherwise), and the compatible changelist,
// marked as a licensee one on a licensee build.
//
// The engine takes major, minor and patch from Version.h
// (ENGINE_*_VERSION, EngineVersion.cpp 205-208), the changelist and the
// licensee flag from BuildSettings, which UnrealBuildTool compiles from
// Build/Build.version (BuildSettings.cpp 9-11, 39-41; BuildSettings.Build.cs
// 26-38; BuildVersion.cs 77, 100-103, 140-160), taking the changelist itself
// when either is 0 (EffectiveCompatibleChangelist). All of it is read from
// Build.version here, since only that file has the changelist and the flag.
// That is enough as long as both files carry the same three numbers, which
// they do on the installed 5.7.4 and 5.8.4 and which AutomationTool's
// UpdateLocalVersion keeps so (Version.h 27-28); a source build with only
// one of them edited would be judged by its Build.version.
func compatibleEngineVersion(engineDir string) (ueVersion, error) {
	path := filepath.Join(engineDir, "Build", "Build.version")
	data, err := readUEJSON(path)
	if err != nil {
		return ueVersion{}, err
	}
	var bv struct {
		MajorVersion, MinorVersion, PatchVersion            *uint16
		Changelist, CompatibleChangelist, IsLicenseeVersion uint32
	}
	if err := json.Unmarshal(data, &bv); err != nil {
		return ueVersion{}, fmt.Errorf("%s: %w", path, err)
	}
	if bv.MajorVersion == nil || bv.MinorVersion == nil || bv.PatchVersion == nil {
		return ueVersion{}, fmt.Errorf("%s: no MajorVersion, MinorVersion and PatchVersion", path)
	}
	v := ueVersion{major: *bv.MajorVersion, minor: *bv.MinorVersion, changelist: bv.Changelist}
	if bv.Changelist != 0 && bv.CompatibleChangelist != 0 {
		v.changelist = bv.CompatibleChangelist
	}
	if bv.IsLicenseeVersion != 0 {
		v.patch = *bv.PatchVersion
		v.changelist |= 0x80000000
	}
	return v, nil
}

// readUEJSON reads a descriptor as the engine reads its text and JSON: UTF-16
// little or big endian after that byte order mark, UTF-8 otherwise, a UTF-8
// byte order mark dropped (FFileHelper::BufferToString, FileHelper.cpp
// 147-190), and a comma before a closing bracket allowed (RapidJsonUtils.cpp
// 9, kParseTrailingCommasFlag) — dropped here so encoding/json takes it.
func readUEJSON(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) >= 2 && len(b)%2 == 0 && (b[0] == 0xff && b[1] == 0xfe || b[0] == 0xfe && b[1] == 0xff) {
		units := make([]uint16, 0, len(b)/2-1)
		for i := 2; i < len(b); i += 2 {
			if b[0] == 0xff {
				units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
			} else {
				units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
			}
		}
		b = []byte(string(utf16.Decode(units)))
	} else {
		b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	}
	return dropTrailingCommas(b), nil
}

// dropTrailingCommas removes each comma outside a string that a closing
// bracket follows, past white space only, and that comes after a value —
// what RapidJSON's trailing-comma flag allows. A comma right after an
// opening bracket or another comma stays, for the decoder to refuse.
func dropTrailingCommas(b []byte) []byte {
	out := make([]byte, 0, len(b))
	inString, escaped := false, false
	var last byte // the last byte outside a string that is no white space
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString, last = false, c
			}
			continue
		}
		if c == ',' && last != '{' && last != '[' && last != ',' {
			j := i + 1
			for j < len(b) && strings.IndexByte(" \t\n\r", b[j]) >= 0 {
				j++
			}
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				continue
			}
		}
		out = append(out, c)
		if c == '"' {
			inString = true
		} else if strings.IndexByte(" \t\n\r", c) < 0 {
			last = c
		}
	}
	return out
}

// firstMembers returns the raw value of each named member of the JSON
// object data holds, matched without regard to case, the first of several
// with one name counting (as RapidJsonPluginLoading.cpp 15-33 looks members
// up); a name that is not there is missing from the map. Anything but one
// whole object is an error.
func firstMembers(data []byte, names ...string) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	found := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		for _, n := range names {
			if _, seen := found[n]; !seen && strings.EqualFold(key, n) {
				found[n] = raw
			}
		}
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("more than one JSON value")
	}
	return found, nil
}
