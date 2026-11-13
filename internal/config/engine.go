package config

// Engine auto-detection — step 3 and 4 of the ${ENGINE} resolution (steps 1
// and 2, env UE_ROOT and engineRoot, are in resolveEngine).
//
// The decisions (version order, association matching, parsing `reg query`
// output) are pure functions; only regQuery touches the Windows registry, by
// running reg.exe.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const (
	// launcherRegKey holds one subkey per Epic Launcher install, named by its
	// version ("5.8"), with an InstalledDirectory value.
	launcherRegKey = `HKLM\SOFTWARE\EpicGames\Unreal Engine`
	// buildsRegKey holds one value per source build registered by
	// UnrealVersionSelector: name = build identifier (usually "{GUID}"),
	// data = engine root.
	buildsRegKey = `HKCU\Software\Epic Games\Unreal Engine\Builds`
)

// defaultEnginePatterns are the standard Epic installation paths, searched in
// addition to the registry.
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
// may be empty); chooseEngine documents the order.
func autodetectEngine(project string) string {
	assoc := readEngineAssociation(project)
	var builds []engineCandidate
	if kind, _ := classifyAssociation(assoc); kind == assocBuild {
		builds = registrySourceBuilds()
	}
	return chooseEngine(assoc, registryLauncherInstalls(), globEngines(defaultEnginePatterns), builds, hasEditor)
}

// chooseEngine picks the engine root from the gathered candidates:
//
//  1. the engine the project's EngineAssociation names: a version ("5.8") is
//     looked up among the Launcher installs, then the default paths (UE_5.8);
//     any other identifier (a source build's "{GUID}") among the registered
//     source builds, case-insensitively and with or without braces;
//  2. otherwise — no association, or it names no usable engine — the highest
//     version among the Launcher installs and default paths, compared
//     numerically (5.10 > 5.9).
//
// Only directories for which hasEditor is true count; "" if none qualifies.
func chooseEngine(assoc string, launcher, defaults, builds []engineCandidate, hasEditor func(dir string) bool) string {
	installed := append(append([]engineCandidate(nil), launcher...), defaults...)

	kind, id := classifyAssociation(assoc)
	var named []engineCandidate
	switch kind {
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
			return filepath.ToSlash(c.Dir)
		}
	}
	return highestEngine(installed, hasEditor)
}

// highestEngine returns the directory of the candidate with the highest
// version that has an editor. IDs that are not a version rank below every
// version; on a tie the earlier candidate wins.
func highestEngine(cands []engineCandidate, hasEditor func(dir string) bool) string {
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
		return ""
	}
	return filepath.ToSlash(cands[best].Dir)
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
	assocNone    associationKind = iota // empty: no association
	assocVersion                        // Launcher install, e.g. "5.8"
	assocBuild                          // source build, e.g. "{GUID}"
)

// classifyAssociation classifies a .uproject EngineAssociation and returns the
// identifier to match: a version as is, anything else normalized by
// normalizeBuildID.
func classifyAssociation(assoc string) (associationKind, string) {
	assoc = strings.TrimSpace(assoc)
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

// readEngineAssociation returns the project's EngineAssociation, "" if there is
// no project or it cannot be read.
func readEngineAssociation(project string) string {
	if project == "" {
		return ""
	}
	b, err := os.ReadFile(project)
	if err != nil {
		return ""
	}
	return parseEngineAssociation(b)
}

// parseEngineAssociation extracts EngineAssociation from .uproject JSON; ""
// if it is missing or the JSON is invalid. A UTF-8 BOM is tolerated.
func parseEngineAssociation(b []byte) string {
	var p struct {
		EngineAssociation string `json:"EngineAssociation"`
	}
	if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &p) != nil {
		return ""
	}
	return strings.TrimSpace(p.EngineAssociation)
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
