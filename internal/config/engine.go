package config

// Engine auto-detection — step 3 and 4 of the ${ENGINE} resolution (steps 1
// and 2, env UE_ROOT and engineRoot, are in resolveEngine).
//
// The decisions (version order, association matching, parsing `reg query`
// output and LauncherInstalled.dat) are pure functions; only regQuery touches
// the Windows registry, by running reg.exe, only datLauncherInstalls reads
// the Launcher's install list, and only readEngineAssociation, globEngines,
// isEngineRoot and hasEditor look at the project and the engine directories.

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	// launcherInstalledFile is the Epic Launcher's list of what it installed,
	// relative to the ProgramData directory, which unreagent takes from
	// %ProgramData%. The engine reads its Launcher installs from it (UE 5.8.4:
	// FDesktopPlatformBase::ReadLauncherInstallationList, under
	// FPlatformProcess::ApplicationSettingsDir, the known folder
	// FOLDERID_ProgramData plus "/Epic/"). Only when the file does not exist
	// does it look for the UE 4.0 and 4.1 manifests in
	// UnrealEngineLauncher/Data/Manifests instead; unreagent does not.
	launcherInstalledFile = `Epic/UnrealEngineLauncher/LauncherInstalled.dat`
	// launcherRegKey holds one subkey per Epic Launcher install, named by its
	// version ("5.8"), with an InstalledDirectory value. The engine does not
	// read it, and the Launcher does not always write it (k211); it is the
	// fallback after launcherInstalledFile.
	launcherRegKey = `HKLM\SOFTWARE\EpicGames\Unreal Engine`
	// buildsRegKey holds one value per source build registered by
	// UnrealVersionSelector: name = build identifier (usually "{GUID}"),
	// data = engine root.
	buildsRegKey = `HKCU\Software\Epic Games\Unreal Engine\Builds`
)

// defaultEnginePatterns are the standard Epic installation paths, searched in
// addition to the Launcher's install list and the registry.
var defaultEnginePatterns = []string{
	`C:/Program Files/Epic Games/UE_*`,
	`D:/Program Files/Epic Games/UE_*`,
	`E:/Program Files/Epic Games/UE_*`,
	`C:/Epic Games/UE_*`,
	`D:/Epic Games/UE_*`,
}

// engineCandidate is an engine installation found by auto-detection. ID is the
// version ("5.8") for Launcher installs and default paths, and the build
// identifier for registered source builds.
type engineCandidate struct {
	ID  string
	Dir string
}

// autodetectEngine finds the engine root for project (path to the .uproject,
// may be empty) and the warning chooseEngine gives for it; chooseEngine
// documents the order.
func autodetectEngine(project string) (root, warning string) {
	assoc, ok := readEngineAssociation(project)
	var builds []engineCandidate
	if kind, _ := classifyAssociation(assoc); kind == assocBuild {
		builds = registrySourceBuilds()
	}
	// A .uproject the engine cannot read as JSON has no association to it,
	// and it does not search the project's surroundings then either.
	var tree string
	if ok {
		tree = projectEngineRoot(project, assoc, isEngineRoot)
	}
	launcher := launcherEngines(os.Getenv("ProgramData"), registryLauncherInstalls())
	return chooseEngine(assoc, tree, launcher, globEngines(defaultEnginePatterns), builds, hasEditor)
}

// launcherEngines lists the Launcher installs: first those in the
// LauncherInstalled.dat under the ProgramData directory programData — the
// list the engine reads — then registry, the launcherRegKey subkeys, as the
// fallback. For a version both name, chooseEngine takes the .dat's directory
// and the registry's only if the .dat's has no editor. Without a usable .dat
// (programData "", the file missing, unreadable or malformed) the registry is
// all there is, as before the .dat was read.
func launcherEngines(programData string, registry []engineCandidate) []engineCandidate {
	return append(datLauncherInstalls(launcherInstalledPath(programData)), registry...)
}

// chooseEngine picks the engine root from the gathered candidates:
//
//  1. the engine the project names. A version association ("5.8") is looked
//     up among the Launcher installs (launcherEngines), then the default
//     paths (UE_5.8); a path association ("../UE") and a blank one name
//     tree, the engine root projectEngineRoot found for the project ("" for
//     none); any other identifier (a source build's "{GUID}") is looked up
//     among the registered source builds, case-insensitively and with or
//     without braces;
//  2. otherwise — the project names no engine, or none that is usable — the
//     highest version among the Launcher installs and default paths,
//     compared numerically (5.10 > 5.9). This step is unreagent's: the
//     engine has no such fallback and leaves the project without one
//     (GetEngineIdentifierForProject, DesktopPlatformBase.cpp 454-498).
//
// Only directories for which hasEditor is true count; "" if none qualifies.
// That holds for tree too, unreagent's choice (k203): the engine takes an
// engine tree without an editor binary, but unreagent has no editor to start
// there, so a blank or path association then goes on to step 2 as an
// uninstalled version does, with a warning.
//
// When the project names an engine that is not usable and step 2 picks one,
// warning says so (k88): the editor then opens the project with that engine,
// not the one the project names. It claims nothing beyond that: an editor
// started on a .uproject does not act on the association — in the UE 5.8.4
// source only GetEngineIdentifierForProject resolves it, and the editor calls
// that from its project browser and home screen only.
func chooseEngine(assoc, tree string, launcher, defaults, builds []engineCandidate, hasEditor func(dir string) bool) (root, warning string) {
	installed := append(append([]engineCandidate(nil), launcher...), defaults...)

	kind, id := classifyAssociation(assoc)
	var named []engineCandidate
	switch kind {
	case assocNone, assocPath:
		if tree != "" {
			named = append(named, engineCandidate{Dir: tree})
		}
	case assocVersion:
		for _, c := range installed {
			if c.ID == id {
				named = append(named, c)
			}
		}
	case assocBuild:
		for _, c := range builds {
			if normalizeBuildID(c.ID) == id {
				named = append(named, c)
			}
		}
	}
	for _, c := range named {
		if hasEditor(c.Dir) {
			return filepath.ToSlash(c.Dir), ""
		}
	}
	best := highestEngine(installed, hasEditor)
	if best.Dir == "" {
		return "", ""
	}
	const instead = "using the highest usable one instead, %s at %s, so the editor opens the project with that engine, not the one the project names; %s, or pin one with engineRoot in unreagent.local.yaml (or env UE_ROOT)"
	switch {
	case kind == assocNone && tree != "":
		warning = fmt.Sprintf("the .uproject's EngineAssociation names no engine (blank, missing or not a string), so its engine is the engine tree it lies in, %s, which has no editor binary — "+instead,
			tree, best.ID, best.Dir, "build the editor there")
	case kind == assocPath:
		warning = fmt.Sprintf("the .uproject's EngineAssociation %q names no usable engine (no engine root at that path, a relative one taken from the project's directory, or one without an editor binary) — "+instead,
			strings.TrimSpace(assoc), best.ID, best.Dir, "correct the path or build the editor there")
	case kind != assocNone:
		warning = fmt.Sprintf("the .uproject's EngineAssociation %q names no usable engine (not installed, or installed without an editor binary) — "+instead,
			strings.TrimSpace(assoc), best.ID, best.Dir, "install or build the associated engine")
	}
	return best.Dir, warning
}

// highestEngine returns the candidate with the highest version that has an
// editor, its directory with forward slashes; the zero candidate if none has
// one. IDs that are not a version rank below every version; on a tie the
// earlier candidate wins.
func highestEngine(cands []engineCandidate, hasEditor func(dir string) bool) engineCandidate {
	best := -1
	var bestVer []int
	bestOK := false
	for i, c := range cands {
		if !hasEditor(c.Dir) {
			continue
		}
		v, ok := parseEngineVersion(c.ID)
		if best < 0 || (ok && !bestOK) || (ok && bestOK && compareEngineVersions(v, bestVer) > 0) {
			best, bestVer, bestOK = i, v, ok
		}
	}
	if best < 0 {
		return engineCandidate{}
	}
	return engineCandidate{ID: cands[best].ID, Dir: filepath.ToSlash(cands[best].Dir)}
}

// parseEngineVersion parses a dotted engine version of at least major.minor
// ("5.8", "5.10", "5.8.1"). Every component must be plain decimal digits.
func parseEngineVersion(s string) ([]int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) < 2 {
		return nil, false
	}
	v := make([]int, len(parts))
	for i, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		v[i] = n
	}
	return v, true
}

// compareEngineVersions compares numerically, component by component; a
// missing component counts as 0 (5.8 == 5.8.0).
func compareEngineVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

type associationKind int

const (
	assocNone    associationKind = iota // blank: the engine tree the project lies in
	assocVersion                        // Launcher install, e.g. "5.8"
	assocBuild                          // source build, e.g. "{GUID}"
	assocPath                           // engine root, e.g. "../UE": relative to the project's directory
)

// classifyAssociation classifies a .uproject EngineAssociation and returns the
// identifier to match: a path — any value with a / or \, as the engine tells
// one apart (DesktopPlatformBase.cpp 468-480) — and a version as they are,
// anything else normalized by normalizeBuildID.
func classifyAssociation(assoc string) (associationKind, string) {
	assoc = strings.TrimSpace(assoc)
	if strings.ContainsAny(assoc, `/\`) {
		return assocPath, assoc
	}
	if _, ok := parseEngineVersion(assoc); ok {
		return assocVersion, assoc
	}
	if id := normalizeBuildID(assoc); id != "" {
		return assocBuild, id
	}
	return assocNone, ""
}

// normalizeBuildID makes build identifiers comparable: case-insensitive, with
// or without the braces around a GUID.
func normalizeBuildID(id string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(id), "{}"))
}

// readEngineAssociation returns the project's EngineAssociation as
// parseEngineAssociation does; "", false if there is no project or it cannot
// be read.
func readEngineAssociation(project string) (assoc string, ok bool) {
	if project == "" {
		return "", false
	}
	b, err := os.ReadFile(project)
	if err != nil {
		return "", false
	}
	return parseEngineAssociation(b)
}

// parseEngineAssociation extracts EngineAssociation from the .uproject's
// bytes b, decoded by decodeUEText. ok is false when they hold no JSON object
// — the engine then has no association for the project at all. A value that
// is missing or not a string reads as "", as blank: the engine takes only a
// string (DesktopPlatformBase.cpp 466-467). The engine reads the file with
// its TJsonReader (LoadProjectFile, 1661-1678), which refuses a trailing
// comma in an object as encoding/json does — unlike the RapidJSON reader its
// plugin and project descriptors go through.
func parseEngineAssociation(b []byte) (assoc string, ok bool) {
	var p *struct {
		EngineAssociation json.RawMessage `json:"EngineAssociation"`
	}
	if json.Unmarshal(decodeUEText(b), &p) != nil || p == nil {
		return "", false
	}
	var s string
	if json.Unmarshal(p.EngineAssociation, &s) != nil {
		return "", true
	}
	return strings.TrimSpace(s), true
}

// decodeUEText returns the text file content b as UTF-8, decoded as the
// engine decodes a text file: UTF-16 little or big endian when b has an even
// length and starts with that byte order mark, UTF-8 otherwise, a UTF-8 byte
// order mark dropped (FFileHelper::BufferToString,
// Runtime/Core/Private/Misc/FileHelper.cpp 145-185).
func decodeUEText(b []byte) []byte {
	var order binary.ByteOrder
	switch {
	case len(b) < 2 || len(b)%2 != 0:
	case b[0] == 0xff && b[1] == 0xfe:
		order = binary.LittleEndian
	case b[0] == 0xfe && b[1] == 0xff:
		order = binary.BigEndian
	}
	if order == nil {
		return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	}
	units := make([]uint16, 0, len(b)/2-1)
	for i := 2; i < len(b); i += 2 {
		units = append(units, order.Uint16(b[i:]))
	}
	return []byte(string(utf16.Decode(units)))
}

// projectEngineRoot returns the engine root the project's place on disk
// names, as the engine finds it (GetEngineIdentifierForProject,
// DesktopPlatformBase.cpp 454-498), isEngineRoot telling engine roots apart.
// For a path association (classifyAssociation) it is the directory
// associationPath resolves it to, if that is one; nothing else is searched
// then, as the engine searches nothing else. For a blank association it is
// the nearest engine root above the project's directory, up to the root of
// the drive; the project's directory itself is not searched. "" for any other
// association, for no project, and when there is none.
func projectEngineRoot(project, assoc string, isEngineRoot func(dir string) bool) string {
	if project == "" {
		return ""
	}
	switch kind, _ := classifyAssociation(assoc); kind {
	case assocPath:
		if dir := associationPath(project, assoc); isEngineRoot(dir) {
			return dir
		}
	case assocNone:
		dir := filepath.Dir(filepath.Clean(project))
		for parent := filepath.Dir(dir); parent != dir; dir, parent = parent, filepath.Dir(parent) {
			if isEngineRoot(parent) {
				return filepath.ToSlash(parent)
			}
		}
	}
	return ""
}

// associationPath resolves the path association assoc against the directory
// of project, with forward slashes. As in the engine, \ counts as a
// separator, and a value that starts with / or \\ or a drive letter and a
// colon is taken as it is, anything else relative to the project's directory
// (FPaths::ConvertRelativePathToFull and FPaths::IsRelative,
// Runtime/Core/Private/Misc/Paths.cpp 123-148, 1265-1280).
func associationPath(project, assoc string) string {
	assoc = strings.TrimSpace(assoc)
	rooted := strings.HasPrefix(assoc, "/") || strings.HasPrefix(assoc, `\\`) ||
		len(assoc) >= 2 && assoc[1] == ':' && ('a' <= assoc[0]|0x20 && assoc[0]|0x20 <= 'z')
	p := filepath.FromSlash(strings.ReplaceAll(assoc, `\`, "/"))
	if !rooted {
		p = filepath.Join(filepath.Dir(project), p)
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// globEngines lists the directories matching patterns; the ID is the version
// taken from the directory name (UE_5.8 → "5.8").
func globEngines(patterns []string) []engineCandidate {
	var out []engineCandidate
	for _, p := range patterns {
		// filepath.Glob uses the OS separator (\ on Windows) — forward-slash
		// patterns would not match there, hence FromSlash.
		matches, _ := filepath.Glob(filepath.FromSlash(p))
		for _, m := range matches {
			base := filepath.Base(m)
			if len(base) > 3 && strings.EqualFold(base[:3], "UE_") {
				base = base[3:]
			}
			out = append(out, engineCandidate{ID: base, Dir: m})
		}
	}
	return out
}

// hasEditor reports whether dir is an engine root with an UnrealEditor.exe.
func hasEditor(dir string) bool {
	return fileExists(filepath.Join(dir, "Engine", "Binaries", "Win64", "UnrealEditor.exe"))
}

// isEngineRoot reports whether dir is an engine root by the engine's own
// test: it has an Engine/Binaries and an Engine/Build directory
// (FDesktopPlatformBase::IsValidRootDirectory, DesktopPlatformBase.cpp
// 401-421). It need not hold an editor binary; that is hasEditor's test.
func isEngineRoot(dir string) bool {
	for _, sub := range []string{"Binaries", "Build"} {
		st, err := os.Stat(filepath.Join(dir, "Engine", sub))
		if err != nil || !st.IsDir() {
			return false
		}
	}
	return true
}

// --- LauncherInstalled.dat (Windows) ---

// launcherInstalledPath returns the path of the Launcher's install list under
// the ProgramData directory programData; "" if programData is "", as it is
// off Windows.
func launcherInstalledPath(programData string) string {
	if programData == "" {
		return ""
	}
	return filepath.Join(programData, filepath.FromSlash(launcherInstalledFile))
}

// datLauncherInstalls lists the Launcher installs in the LauncherInstalled.dat
// at path; none if path is "" or the file cannot be read.
func datLauncherInstalls(path string) []engineCandidate {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseLauncherInstalled(b)
}

// parseLauncherInstalled turns LauncherInstalled.dat JSON into candidates:
// every InstallationList entry whose AppName is "UE_" plus a version, with ID
// that version ("UE_5.8" → "5.8") and Dir its InstallLocation. The engine
// strips the "UE_" case-sensitively, so "ue_5.8" is no engine, and keeps any
// rest as an identifier ("UE_Custom" → "Custom"); unreagent takes only a
// version, since an association that is no version is looked up among the
// source builds only. The other entries are what the Launcher installed
// besides engines — marketplace and Fab plugins, under their own AppNames —
// and are skipped, as are entries without an InstallLocation or with a field
// that is not a string. AppVersion is not read: it can lag behind the
// installed engine (5.8.3 listed for a 5.8.4, k211). A UTF-8 BOM is
// tolerated; JSON that is not an object with an InstallationList array yields
// none.
func parseLauncherInstalled(b []byte) []engineCandidate {
	var list struct {
		InstallationList []json.RawMessage `json:"InstallationList"`
	}
	if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &list) != nil {
		return nil
	}
	var out []engineCandidate
	for _, raw := range list.InstallationList {
		var e struct {
			AppName         string `json:"AppName"`
			InstallLocation string `json:"InstallLocation"`
		}
		if json.Unmarshal(raw, &e) != nil || e.InstallLocation == "" {
			continue
		}
		ver, ok := strings.CutPrefix(e.AppName, "UE_")
		if !ok {
			continue
		}
		if _, ok := parseEngineVersion(ver); !ok {
			continue
		}
		out = append(out, engineCandidate{ID: ver, Dir: e.InstallLocation})
	}
	return out
}

// --- registry (Windows) ---

// registryLauncherInstalls enumerates the Launcher installs (one reg.exe run).
func registryLauncherInstalls() []engineCandidate {
	out, ok := regQuery(launcherRegKey, "/s")
	if !ok {
		return nil
	}
	return launcherInstalls(parseRegQuery(out))
}

// registrySourceBuilds lists the registered source builds.
func registrySourceBuilds() []engineCandidate {
	out, ok := regQuery(buildsRegKey)
	if !ok {
		return nil
	}
	return sourceBuilds(parseRegQuery(out))
}

// regQuery runs `reg query key args…`. It reports false off Windows and when
// reg.exe fails, including for a missing key.
func regQuery(key string, args ...string) (string, bool) {
	if runtime.GOOS != "windows" {
		return "", false
	}
	out, err := exec.Command("reg", append([]string{"query", key}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// launcherInstalls turns the parsed `reg query launcherRegKey /s` output into
// candidates: each direct subkey with an InstalledDirectory value.
func launcherInstalls(keys []regKey) []engineCandidate {
	var out []engineCandidate
	for _, k := range keys {
		name, ok := regSubkeyName(k.Path, launcherRegKey)
		if !ok {
			continue
		}
		if dir := k.value("InstalledDirectory"); dir != "" {
			out = append(out, engineCandidate{ID: name, Dir: dir})
		}
	}
	return out
}

// sourceBuilds turns the parsed `reg query buildsRegKey` output into
// candidates: every value of the key, name = build identifier.
func sourceBuilds(keys []regKey) []engineCandidate {
	var out []engineCandidate
	for _, k := range keys {
		if canonicalRegKey(k.Path) != canonicalRegKey(buildsRegKey) {
			continue
		}
		for _, v := range k.Values {
			if v.Data != "" {
				out = append(out, engineCandidate{ID: v.Name, Dir: v.Data})
			}
		}
	}
	return out
}

// regKey is one key block of `reg query` output.
type regKey struct {
	Path   string
	Values []regValue
}

type regValue struct {
	Name, Data string
}

func (k regKey) value(name string) string {
	for _, v := range k.Values {
		if strings.EqualFold(v.Name, name) {
			return v.Data
		}
	}
	return ""
}

// regValueLine matches an (indented) value line of `reg query` output:
// "    <name>    REG_<TYPE>    <data>". Names may contain spaces.
var regValueLine = regexp.MustCompile(`^[ \t]+(.*?)[ \t]+REG_[A-Z0-9_]+(?:[ \t]+(.*?))?[ \t]*$`)

// parseRegQuery parses `reg query` output: every unindented "HKEY_…" line
// starts a key, the value lines below it belong to that key.
func parseRegQuery(out string) []regKey {
	var keys []regKey
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "HKEY_") {
			keys = append(keys, regKey{Path: strings.TrimSpace(line)})
			continue
		}
		if len(keys) == 0 {
			continue
		}
		if m := regValueLine.FindStringSubmatch(line); m != nil {
			k := &keys[len(keys)-1]
			k.Values = append(k.Values, regValue{Name: m[1], Data: m[2]})
		}
	}
	return keys
}

// regSubkeyName returns the last path component of path if path is a direct
// subkey of parent.
func regSubkeyName(path, parent string) (string, bool) {
	p, q := canonicalRegKey(path), canonicalRegKey(parent)+`\`
	if !strings.HasPrefix(p, q) || len(p) == len(q) || strings.Contains(p[len(q):], `\`) {
		return "", false
	}
	path = strings.TrimRight(strings.TrimSpace(path), `\`)
	return path[strings.LastIndex(path, `\`)+1:], true
}

// canonicalRegKey makes key paths comparable: reg.exe prints the long root
// (HKEY_LOCAL_MACHINE) for a query made with the short one (HKLM), and key
// names are case-insensitive.
func canonicalRegKey(path string) string {
	path = strings.TrimRight(strings.TrimSpace(path), `\`)
	root, rest := path, ""
	if i := strings.IndexByte(path, '\\'); i >= 0 {
		root, rest = path[:i], path[i:]
	}
	switch strings.ToUpper(root) {
	case "HKLM":
		root = "HKEY_LOCAL_MACHINE"
	case "HKCU":
		root = "HKEY_CURRENT_USER"
	}
	return strings.ToUpper(root + rest)
}
