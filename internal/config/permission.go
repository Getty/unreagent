package config

import (
	"fmt"
	"path"
	"runtime"
	"strings"
)

// windowsPaths selects Windows semantics for path rules — case-insensitive
// comparison and UNC roots — when the launcher is built for Windows. It is a
// constant, not a setting; pathMatches and pathPatternProblem take it as a
// parameter so that both behaviours are testable on any host.
const windowsPaths = runtime.GOOS == "windows"

// Decision is the result of a permission check.
type Decision struct {
	Allow   bool
	Message string // rationale, mostly for rejections
}

// ruleSense says how a rule must fail when it cannot be evaluated with
// confidence. Both directions fail closed, but "closed" means the opposite
// thing per sense: a restrictive rule fails closed by matching (blocking), a
// permissive rule fails closed by not matching (not granting).
type ruleSense int

const (
	senseAllow ruleSense = iota // permissive rule: doubt must not grant
	senseDeny                   // restrictive rule: doubt must not pass
)

// Decide decides whether a tool call is permitted by the policy.
//
// toolName is the name of the tool the agent wants to call (e.g. "Bash",
// "Edit", "mcp__foo__bar"). toolInput are its arguments; bracket rules match
// their inner pattern against the tool's relevant input field ("command" for
// Bash, "file_path"/"path" for file tools, ...).
//
// Order: deny rules always win. The mode decides afterwards.
func (p Permissions) Decide(toolName string, toolInput map[string]any) Decision {
	for _, rule := range p.Deny {
		if matchRule(rule, toolName, toolInput, senseDeny) {
			return Decision{Allow: false, Message: fmt.Sprintf("blocked by deny rule %q", rule)}
		}
	}

	switch p.Mode {
	case ModeAllowAll:
		return Decision{Allow: true, Message: "allow_all"}
	case ModeDenyAll:
		return Decision{Allow: false, Message: "deny_all: every request is rejected"}
	case ModeAllowlist:
		if key, command, ok := primaryInput(toolName, toolInput); ok && key == "command" {
			return p.allowCommand(toolName, command)
		}
		for _, rule := range p.Allow {
			if matchRule(rule, toolName, toolInput, senseAllow) {
				return Decision{Allow: true, Message: fmt.Sprintf("allowed by allow rule %q", rule)}
			}
		}
		return Decision{Allow: false, Message: fmt.Sprintf("no allow entry matches %q", toolName)}
	default:
		return Decision{Allow: false, Message: "unknown permission mode"}
	}
}

// allowCommand is the allowlist decision for a command line. Every command
// on the line (see shellSegments) has to be covered by an allow rule for the
// tool, but not by the same one: "cd /proj && git status" passes with
// "Bash(cd *)" plus "Bash(git *)". A rule without an inner pattern covers the
// whole line. Deny rules have already been applied by Decide.
//
// A segment with an output redirection is covered only by a rule that names
// the redirection in its literal part, before the trailing * (see
// namesRedirections): "Bash(echo *)" does not cover "echo x > /etc/passwd",
// which falls through to the default decision, while "Bash(echo x > *)"
// does. Failing closed here is the policy the orchestrating agent chose for
// k59, not a requirement from the maintainer. fd duplication ("2>&1") and the
// null device of the tool's shell write no file and do not count (see
// outputRedirections).
func (p Permissions) allowCommand(toolName, command string) Decision {
	deny := Decision{Allow: false, Message: fmt.Sprintf("no allow entry matches %q", toolName)}
	segments := shellSegments(command)
	covered := make([]bool, len(segments))
	redirects := make([][]int, len(segments))
	// The command line is read with bash's syntax for every tool; the tool
	// only decides which null device counts (see isNullDevice).
	powershell := toolName == "PowerShell"
	for i, segment := range segments {
		redirects[i] = outputRedirections(segment, powershell)
	}
	var used []string
	for _, rule := range p.Allow {
		tool, inner, bracket := splitRule(rule)
		if tool == "" || !globEqual(tool, toolName) {
			continue
		}
		if !bracket {
			return Decision{Allow: true, Message: fmt.Sprintf("allowed by allow rule %q", rule)}
		}
		hit := false
		for i, segment := range segments {
			if !covered[i] && globMatch(inner, segment) && namesRedirections(inner, redirects[i]) {
				covered[i], hit = true, true
			}
		}
		if hit {
			used = append(used, rule)
		}
	}
	if len(segments) == 0 {
		// Nothing recognisable to match against, or undecidable (see
		// shellSegments).
		return deny
	}
	for _, ok := range covered {
		if !ok {
			return deny
		}
	}
	if len(used) == 1 {
		return Decision{Allow: true, Message: fmt.Sprintf("allowed by allow rule %q", used[0])}
	}
	return Decision{Allow: true, Message: fmt.Sprintf("allowed by allow rules %q", used)}
}

// matchRule checks a single rule against tool name and input.
//
// Supported forms:
//   - "Edit"            exact tool name
//   - "mcp__server__*"  prefix wildcard (trailing star)
//   - "*"               everything
//   - "Bash(git *)"     tool + inner pattern against the tool's input field
func matchRule(rule, toolName string, toolInput map[string]any, sense ruleSense) bool {
	tool, inner, bracket := splitRule(rule)
	if tool == "" || !globEqual(tool, toolName) {
		return false
	}
	if !bracket {
		return true
	}
	return matchInner(inner, toolName, toolInput, sense)
}

// splitRule parses "Tool" or "Tool(inner)" into its parts; bracket reports
// whether the rule carries an inner pattern.
func splitRule(rule string) (tool, inner string, bracket bool) {
	rule = strings.TrimSpace(rule)
	open := strings.IndexByte(rule, '(')
	if open < 0 || !strings.HasSuffix(rule, ")") {
		return rule, "", false
	}
	return strings.TrimSpace(rule[:open]), strings.TrimSpace(rule[open+1 : len(rule)-1]), true
}

// toolInputKeys maps well-known tools to the input fields a bracket rule's
// inner pattern is matched against. The first entry is the primary field.
var toolInputKeys = map[string][]string{
	"Bash":         {"command"},
	"Read":         {"file_path", "path"},
	"Write":        {"file_path", "path"},
	"Edit":         {"file_path", "path"},
	"MultiEdit":    {"file_path", "path"},
	"NotebookEdit": {"notebook_path", "file_path", "path"},
	"Glob":         {"path", "pattern"},
	"Grep":         {"path", "pattern"},
	"WebFetch":     {"url"},
	"WebSearch":    {"query"},
}

// genericInputKeys is probed for tools that are not in toolInputKeys — MCP
// tools above all, whose argument names the launcher cannot know. A rule whose
// tool carries none of these fields cannot be evaluated and therefore fails
// closed (see matchInner).
var genericInputKeys = []string{"command", "file_path", "path", "pattern", "notebook_path", "url", "query"}

func inputKeysFor(toolName string) []string {
	if keys, ok := toolInputKeys[toolName]; ok {
		return keys
	}
	return genericInputKeys
}

// matchInner matches the inner pattern of a bracket rule against the tool
// input. A deny rule matches if any known input field matches; an allow rule
// only looks at the first field the tool actually carries. If no known field
// carries a string value the rule is not evaluable: the deny rule blocks, the
// allow rule does not grant.
func matchInner(pattern, toolName string, toolInput map[string]any, sense ruleSense) bool {
	keys := inputKeysFor(toolName)
	if sense == senseDeny {
		evaluable := false
		for _, key := range keys {
			value, ok := stringInput(toolInput, key)
			if !ok {
				continue
			}
			evaluable = true
			if matchValue(pattern, key, value, sense) {
				return true
			}
		}
		return !evaluable
	}
	key, value, ok := primaryInput(toolName, toolInput)
	if !ok {
		return false
	}
	return matchValue(pattern, key, value, sense)
}

// primaryInput returns the first known input field the tool actually carries
// with a string value.
func primaryInput(toolName string, toolInput map[string]any) (key, value string, ok bool) {
	for _, key := range inputKeysFor(toolName) {
		if value, ok := stringInput(toolInput, key); ok {
			return key, value, true
		}
	}
	return "", "", false
}

func stringInput(toolInput map[string]any, key string) (string, bool) {
	value, ok := toolInput[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// matchValue applies the inner pattern to one input value. Command lines get
// shell-aware treatment, paths are normalised first, every other field is a
// plain glob comparison.
func matchValue(pattern, key, value string, sense ruleSense) bool {
	switch {
	case key == "command":
		// The allow side of a command line is decided across all rules in
		// allowCommand; a single allow rule cannot answer it and does not
		// grant.
		return sense == senseDeny && denyCommand(pattern, value)
	case isPathKey(key):
		// An allow rule grants only a path it names, and a pattern that names
		// none (see pathPatternProblem) does not grant. checkRules rejects
		// such a rule for the file tools at load; this covers the rules it
		// cannot judge there: MCP tools, Glob/Grep, tool-name globs. A deny
		// rule is matched as written — whatever it names, it can only block.
		if sense == senseAllow && pathPatternProblem(pattern, windowsPaths) != "" {
			return false
		}
		return matchPath(pattern, value, sense)
	}
	return globMatch(pattern, value)
}

func isPathKey(key string) bool {
	switch key {
	case "file_path", "path", "notebook_path":
		return true
	}
	return false
}

// isPathTool reports whether every input field a rule for the tool is
// matched against is a path, so that its bracket rules are path rules.
func isPathTool(tool string) bool {
	keys, ok := toolInputKeys[tool]
	if !ok {
		return false
	}
	for _, key := range keys {
		if !isPathKey(key) {
			return false
		}
	}
	return true
}

// checkRules rejects, naming it, a bracket rule for a file tool (Read, Write,
// Edit, MultiEdit, NotebookEdit) whose pattern cannot be a path rule (see
// pathPatternProblem): a relative pattern in either list, and in the allow
// list one anchored at a file system root, which would allow that whole file
// system. A deny rule at a root only blocks and stays. Other tools' rules are
// not checked here: whether their pattern meets a path is only known per
// call, and matchValue keeps such an allow rule from granting there.
func (p Permissions) checkRules() error {
	for _, list := range []struct {
		name, verb string
		rules      []string
	}{{"permissions.allow", "allow", p.Allow}, {"permissions.deny", "deny", p.Deny}} {
		for i, rule := range list.rules {
			tool, inner, bracket := splitRule(rule)
			if !bracket || !isPathTool(tool) {
				continue
			}
			switch problem := pathPatternProblem(inner, windowsPaths); {
			case problem == notAbsolute:
				return fmt.Errorf("%s[%d] %q: the path %q %s — a path rule must name an absolute path, e.g. \"%s(${PROJECT_DIR}/Source/*)\"; to %s every path, write the bare tool name %q",
					list.name, i, rule, inner, problem, tool, list.verb, tool)
			case problem == namesRoot && list.verb == "allow":
				return fmt.Errorf("%s[%d] %q: the path %q %s — an allow rule must name an absolute path below one, e.g. \"%s(${PROJECT_DIR}/Source/*)\"; to allow every path, write the bare tool name %q",
					list.name, i, rule, inner, problem, tool, tool)
			}
		}
	}
	return nil
}

// pathPatternProblem says why a path pattern cannot be a rule, "" if it can.
// A path rule has to name an absolute path below a file system root.
// Cleaning turns a relative pattern into something else entirely — "./*" and
// "x/../*" both become "*", which matches every path — and a relative
// pattern never meets the absolute paths the file tools send. A pattern
// anchored at nothing more than a root ("/*", "C:\*") covers a whole file
// system, which the bare tool name says without a pattern.
func pathPatternProblem(pattern string, windows bool) string {
	p := normalizePath(strings.TrimSpace(pattern), windows)
	switch prefix := strings.TrimSuffix(p, "*"); {
	case !strings.HasPrefix(p, "/") && !(hasDriveLetter(p) && len(p) > 2 && p[2] == '/'):
		return notAbsolute
	case prefix == "/" || prefix == "//" || hasDriveLetter(prefix) && prefix[2:] == "/":
		return namesRoot
	}
	return ""
}

// The problems pathPatternProblem reports.
const (
	notAbsolute = "is not an absolute path"
	namesRoot   = "names a file system root"
)

// matchPath applies a path pattern to a path with the semantics of the
// target OS (see pathMatches).
//
// YAML note: inside double quotes a backslash is an escape character and
// "Write(C:\Proj\*)" is rejected by the parser, so a Windows pattern has to
// be single-quoted ('Write(C:\Proj\*)') or written with forward slashes.
func matchPath(pattern, value string, sense ruleSense) bool {
	return pathMatches(pattern, value, windowsPaths, sense)
}

// pathMatches applies a path pattern to a path. Both sides are normalised
// first — backslashes become slashes, "." and ".." are resolved — so that
// neither the two spellings Claude Code produces on Windows ("C:\Proj\a"
// and "C:/Proj/a") nor a traversal ("/proj/../etc/passwd") can walk around a
// rule.
//
// With windows set the comparison ignores case, like the file systems there,
// and a UNC path keeps its \\server\share root (see normalizePath). Without
// it only a pattern that names a drive letter ignores case; everything else
// stays case-sensitive. Case is folded rune by rune to upper case, the
// direction NTFS compares in: folding to lower case would make "C:\Gİt"
// (capital I with dot) equal "C:\Git", which is a different directory.
//
// A path with more than one reading (see pathReadings) fails closed in the
// rule's sense: a deny rule matches if any reading of the pattern matches any
// reading of the path, an allow rule only if every one does.
func pathMatches(pattern, value string, windows bool, sense ruleSense) bool {
	fold := windows || hasDriveLetter(strings.TrimSpace(pattern))
	values := pathReadings(value, windows, fold)
	for _, p := range pathReadings(pattern, windows, fold) {
		for _, v := range values {
			switch match := globEqual(p, v); {
			case match && sense == senseDeny:
				return true
			case !match && sense == senseAllow:
				return false
			}
		}
	}
	return sense == senseAllow
}

// pathReadings returns the normalised spellings of a path (see
// normalizePath), upper-cased when fold is set. With windows set a path with
// three or more leading separators ("\\\server\share\x") has two: the UNC
// path with the extra separators dropped, and the rooted path
// "\server\share\x". Which one Win32 takes is unverified here.
func pathReadings(p string, windows, fold bool) []string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	readings := []string{normalizePath(p, windows)}
	if windows && strings.HasPrefix(p, "///") {
		readings = append(readings, normalizePath("//"+strings.TrimLeft(p, "/"), windows))
	}
	if fold {
		for i := range readings {
			readings[i] = strings.ToUpper(readings[i])
		}
	}
	return readings
}

// normalizePath turns backslashes into slashes and cleans the path. A root
// that ".." cannot climb on Windows stays in front of the cleaned remainder:
// a drive letter ("C:\..\x" is "C:\x"), and with windows set also the
// //server/share of a UNC path ("\\server\share\..\x" is
// "\\server\share\x"), which thereby stays distinct from the rooted path
// "\server\share". On POSIX "//x" is "/x", so without windows a leading "//"
// is cleaned like any other. A trailing "*" survives path.Clean unchanged.
func normalizePath(p string, windows bool) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if hasDriveLetter(p) {
		if len(p) == 2 {
			return p
		}
		return p[:2] + path.Clean(p[2:])
	}
	if windows {
		if root, rest, ok := uncRoot(p); ok {
			if rest == "" {
				return root
			}
			return root + path.Clean("/"+rest)
		}
	}
	return path.Clean(p)
}

// uncRoot splits a UNC path, slashes already normalised, into its root
// "//server/share" and the rest. A path with three or more leading slashes
// is not taken as UNC here; pathReadings gives it both readings.
func uncRoot(p string) (root, rest string, ok bool) {
	if len(p) < 3 || !strings.HasPrefix(p, "//") || p[2] == '/' {
		return "", "", false
	}
	server, after, _ := strings.Cut(p[2:], "/")
	share, rest, _ := strings.Cut(strings.TrimLeft(after, "/"), "/")
	root = "//" + server
	if share != "" {
		root += "/" + share
	}
	return root, rest, true
}

func hasDriveLetter(p string) bool {
	return len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0])
}

func isASCIILetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// denyCommand matches the inner pattern of a deny rule against a shell
// command line.
//
// The command is split into the individual commands a shell would run (see
// shellSegments), so that neither a second command on the same line nor a
// wrapper around the interesting one can slip past the pattern: the rule
// matches if ANY segment matches — otherwise "cd /tmp && rm -rf /" would walk
// past a deny rule for "rm -rf *". The allow direction lives in allowCommand,
// where EVERY segment has to be covered.
//
// This is a heuristic, not a shell parser: it cannot follow variable
// indirection ("X=rm; $X -rf /"), aliases, or data piped into an interpreter.
// A prefix-glob deny list is a guard rail, not a security boundary; allowlist
// mode is the only mode that fails closed by construction.
func denyCommand(pattern, command string) bool {
	segments := shellSegments(command)
	if len(segments) == 0 {
		// Nothing recognisable to match against, or undecidable (see
		// shellSegments).
		return true
	}
	for _, segment := range segments {
		for _, variant := range commandVariants(segment) {
			if globMatch(pattern, variant) {
				return true
			}
		}
	}
	return false
}

// maxShellDepth caps the recursion into command substitutions and nested
// shell scripts. A command line nested deeper is undecidable (see
// shellSegments).
const maxShellDepth = 4

// shellSegments splits a command line into the individual commands a shell
// would execute. It breaks on the unquoted operators ; && || | & newline and
// on the grouping characters ( ) and the stand-alone words { }, and
// additionally yields the bodies of command substitutions ($(...) and
// backticks) plus the scripts handed to a nested shell ("bash -c ...",
// "cmd /c ...") as segments of their own. A substitution stays part of the
// segment it appears in, so that "rm -rf $(pwd)" is still an "rm -rf "
// command.
//
// A command line it cannot follow is undecidable and yields no segments,
// like a blank one: allowCommand grants nothing for it and denyCommand takes
// it as a match. That is a line with a ${...} expansion whose body is not one
// of the plain forms readBrace follows, or one nested deeper than
// maxShellDepth. Failing closed instead of following bash inside ${...} — and
// listing the bodies that are followed rather than the ones that are not —
// is the policy the orchestrating agent chose for k113, not a requirement
// from the maintainer.
func shellSegments(command string) []string {
	segments, ok := splitSegments(command, 0)
	if !ok {
		return nil
	}
	return segments
}

// splitSegments is shellSegments at a nesting depth; ok is false when the
// command line is undecidable.
func splitSegments(command string, depth int) (segments []string, ok bool) {
	if depth > maxShellDepth {
		return nil, false
	}
	var out []string
	var cur strings.Builder
	ok = true
	nest := func(body string) {
		inner, decided := splitSegments(body, depth+1)
		out = append(out, inner...)
		ok = ok && decided
	}
	flush := func() {
		segment := normalizeSpace(cur.String())
		cur.Reset()
		if segment == "" {
			return
		}
		out = append(out, segment)
		inner, decided := nestedScripts(segment, depth)
		out = append(out, inner...)
		ok = ok && decided
	}

	var quote byte
	for i := 0; ok && i < len(command); i++ {
		c := command[i]
		switch quote {
		case '\'':
			cur.WriteByte(c)
			if c == '\'' {
				quote = 0
			}
			continue
		case '"':
			switch {
			case c == '\\' && i+1 < len(command):
				cur.WriteByte(c)
				i++
				cur.WriteByte(command[i])
			case c == '"':
				quote = 0
				cur.WriteByte(c)
			case c == '$' && i+1 < len(command) && command[i+1] == '$':
				cur.WriteString("$$")
				i++
			case c == '$' && i+1 < len(command) && command[i+1] == '(':
				body, next := readDelimited(command, i+2, '(', ')')
				nest(body)
				cur.WriteString(command[i : next+1])
				i = next
			case c == '$' && i+1 < len(command) && command[i+1] == '{':
				next, decided := readBrace(command, i+2)
				ok = decided
				cur.WriteString(command[i : next+1])
				i = next
			case c == '`':
				body, next := readBackquote(command, i+1)
				nest(body)
				cur.WriteString(command[i : next+1])
				i = next
			default:
				cur.WriteByte(c)
			}
			continue
		}

		switch {
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
		case c == '\\' && i+1 < len(command):
			cur.WriteByte(c)
			i++
			cur.WriteByte(command[i])
		case c == '$' && i+1 < len(command) && command[i+1] == '$':
			// "$$" is one parameter; the '$' after it starts nothing.
			cur.WriteString("$$")
			i++
		case c == '$' && i+1 < len(command) && command[i+1] == '\'':
			_, next := readANSIC(command, i+2)
			cur.WriteString(command[i : next+1])
			i = next
		case c == '$' && i+1 < len(command) && command[i+1] == '(':
			body, next := readDelimited(command, i+2, '(', ')')
			nest(body)
			cur.WriteString(command[i : next+1])
			i = next
		case c == '$' && i+1 < len(command) && command[i+1] == '{':
			next, decided := readBrace(command, i+2)
			ok = decided
			cur.WriteString(command[i : next+1])
			i = next
		case c == '`':
			body, next := readBackquote(command, i+1)
			nest(body)
			cur.WriteString(command[i : next+1])
			i = next
		case isSegmentBreak(command, i):
			flush()
			for i+1 < len(command) && isSegmentBreak(command, i+1) {
				i++
			}
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	if !ok {
		return nil, false
	}
	return out, true
}

// readBrace returns the index of the '}' that closes a ${...} expansion —
// start is the index just after its "${" — and whether the body is one the
// tokenizer follows. It follows only these bodies:
//
//   - #name, the length of a parameter;
//   - name, optionally followed by one of the operators :- - :+ + :? ? # ##
//     % %% and a word.
//
// A name is an identifier, positional digits or one of the special
// parameters @ * # ? $ ! -. The word holds no "$(", "${", "$[", backquote,
// quote, "<(" or ">(" — a plain $name is fine, "$$" is one token, a
// backslash escapes the next byte — and ends at the first other '}' ("${x:-\}}"
// is one word in bash; a bare '{' nests nothing).
//
// Every other body is not followed, which is a list of what is safe rather
// than of what is not: bash reads quotes and nested substitutions inside the
// braces by rules of their own, runs a process substitution even in a
// pattern, and evaluates indirection (${!x}), transformations (${x@P}),
// assignments (${x:=...}), substring offsets (${x:1}, arithmetic),
// replacements (${x/a/b}) and array subscripts — offsets, subscripts and @P
// on a value that an assignment earlier on the line can plant. For those ok
// is false and end is the index where reading stopped; an unterminated body
// is not followed either.
func readBrace(s string, start int) (end int, ok bool) {
	stop := func(i int) (int, bool) { return min(i, len(s)-1), false }
	if start < len(s) && s[start] == '#' {
		if i := parameterName(s, start+1); i > start+1 && i < len(s) && s[i] == '}' {
			return i, true
		}
	}
	i := parameterName(s, start)
	switch {
	case i == start:
		return stop(i)
	case i < len(s) && s[i] == '}':
		return i, true
	}
	op := ""
	for _, o := range []string{":-", ":+", ":?", "##", "%%", "-", "+", "?", "#", "%"} {
		if strings.HasPrefix(s[i:], o) {
			op = o
			break
		}
	}
	if op == "" {
		return stop(i)
	}
	for i += len(op); i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			i++
		case c == '\'' || c == '"' || c == '`':
			return i, false
		case c == '$' && i+1 < len(s) && s[i+1] == '$':
			i++
		case c == '$' && i+1 < len(s) && strings.IndexByte("({[", s[i+1]) >= 0,
			(c == '<' || c == '>') && i+1 < len(s) && s[i+1] == '(':
			return i, false
		case c == '}':
			return i, true
		}
	}
	return stop(i)
}

// parameterName returns the index just after the parameter name that starts
// at i — an identifier, positional digits or one special parameter — or i
// when none starts there.
func parameterName(s string, i int) int {
	isDigit := func(j int) bool { return j < len(s) && '0' <= s[j] && s[j] <= '9' }
	switch {
	case i >= len(s):
		return i
	case s[i] == '_' || isASCIILetter(s[i]):
		for i++; i < len(s) && (s[i] == '_' || isASCIILetter(s[i]) || isDigit(i)); i++ {
		}
	case isDigit(i):
		for i++; isDigit(i); i++ {
		}
	case strings.IndexByte("@*#?$!-", s[i]) >= 0:
		i++
	}
	return i
}

// isSegmentBreak reports whether the unquoted byte at i starts a new command.
// & is an operator unless it belongs to a redirection (2>&1, &>file, >&2);
// | is one unless it ends the redirection ">|"; { and } only group commands
// when they stand alone as a word ("{ a; b; }"), not inside one ("{}",
// "file{,.bak}").
func isSegmentBreak(command string, i int) bool {
	switch command[i] {
	case ';', '\n', '\r', '(', ')':
		return true
	case '|':
		// Only after a lone '>': an escaped one is a literal, and after ">>",
		// "&>" or "<>" bash rejects the '|' as a syntax error — splitting
		// there costs nothing.
		clobber := i > 0 && command[i-1] == '>' && (i < 2 || strings.IndexByte(`\<>&`, command[i-2]) < 0)
		return !clobber
	case '&':
		afterRedirect := i > 0 && (command[i-1] == '>' || command[i-1] == '<') &&
			(i < 2 || command[i-2] != '\\') // "\>&" is a literal > and a background &
		beforeRedirect := i+1 < len(command) && command[i+1] == '>'
		return !afterRedirect && !beforeRedirect
	case '{', '}':
		return isWordBoundary(command, i-1) && isWordBoundary(command, i+1)
	}
	return false
}

// isWordBoundary reports whether position i (which may lie outside the
// string) cannot belong to the same word as its neighbour.
func isWordBoundary(command string, i int) bool {
	if i < 0 || i >= len(command) {
		return true
	}
	switch command[i] {
	case ' ', '\t', '\n', '\r', ';', '&', '|', '(', ')', '<', '>':
		return true
	}
	return false
}

// namesRedirections reports whether an allow pattern that matched a segment
// also names each of the segment's output redirections (positions from
// outputRedirections): one that falls into the pattern's trailing * is not
// covered. An exact pattern spells out the whole segment.
func namesRedirections(pattern string, redirects []int) bool {
	pattern = strings.TrimSpace(pattern)
	if !strings.HasSuffix(pattern, "*") {
		return true
	}
	literal := len(pattern) - 1
	for _, at := range redirects {
		if at >= literal {
			return false
		}
	}
	return true
}

// outputRedirections returns where the unquoted output redirections of a
// segment start — the index of the operator's '>', for "<>" (open
// read-write, which creates the file like '>') its second byte — leaving out
// those that write no file: fd duplication and closing ("2>&1", ">&2",
// "2>&-") and the null device (see isNullDevice). Input redirections,
// here-documents and here-strings read and are not reported. The bodies of
// command substitutions are skipped; they are segments of their own (see
// splitSegments). A heuristic like the splitter: a '>' that the shell does
// not take as a redirection ("[[ a > b ]]") still counts, which errs on the
// side of not granting.
func outputRedirections(segment string, powershell bool) []int {
	var out []int
	var quote byte
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		if quote == '\'' {
			if c == '\'' {
				quote = 0
			}
			continue
		}
		switch {
		case c == '\\' && i+1 < len(segment):
			i++
		case c == '"':
			if quote == '"' {
				quote = 0
			} else {
				quote = c
			}
		case c == '\'' && quote == 0:
			quote = c
		case c == '$' && i+1 < len(segment) && segment[i+1] == '$':
			i++
		case c == '$' && quote == 0 && i+1 < len(segment) && segment[i+1] == '\'':
			_, i = readANSIC(segment, i+2)
		case c == '$' && i+1 < len(segment) && segment[i+1] == '(':
			_, i = readDelimited(segment, i+2, '(', ')')
		case c == '$' && i+1 < len(segment) && segment[i+1] == '{':
			// A segment only holds expansions readBrace can read (see
			// shellSegments).
			i, _ = readBrace(segment, i+2)
		case c == '`':
			_, i = readBackquote(segment, i+1)
		case quote == '"':
			// Literal inside double quotes.
		case c == '>':
			end, writes := redirection(segment, i, powershell)
			if writes {
				out = append(out, i)
			}
			i = end
		}
	}
	return out
}

// redirection reads the output redirection whose '>' is at i and returns the
// index of its last byte and whether it writes a file. ">>" and ">|" (write
// despite noclobber) are one operator each. ">&word" duplicates or closes a
// descriptor when word is a number or "-"; with any other word it sends
// stdout and stderr to that file. A missing target counts as a file.
func redirection(s string, i int, powershell bool) (end int, writes bool) {
	j := i + 1
	dup := false
	switch {
	case j < len(s) && (s[j] == '>' || s[j] == '|'):
		j++ // ">>", ">|"
	case j < len(s) && s[j] == '&':
		j++
		dup = true
	}
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	start := j
	var quote byte
word:
	for ; j < len(s); j++ {
		c := s[j]
		if quote != 0 {
			if c == '\\' && quote == '"' && j+1 < len(s) {
				j++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch {
		case c == '$' && j+1 < len(s) && s[j+1] == '$':
			j++
		case c == '$' && j+1 < len(s) && s[j+1] == '\'':
			_, j = readANSIC(s, j+2)
		case c == '\'' || c == '"':
			quote = c
		case c == '\\' && j+1 < len(s):
			j++
		case strings.IndexByte(" \t<>&;|()", c) >= 0:
			break word
		}
	}
	raw := s[start:j]
	word := strings.Join(shellTokens(raw), "")
	if dup && (word == "-" || word != "" && strings.Trim(word, "0123456789") == "") {
		return j - 1, false
	}
	return j - 1, !isNullDevice(raw, word, powershell)
}

// isNullDevice reports whether a redirection target — raw as written, word
// with quotes and escapes resolved — is the null device of the tool's shell.
// In bash, the Bash tool's shell on every OS (Git Bash on Windows), that is
// /dev/null and nothing else: NUL is an ordinary file name there and $null an
// ordinary variable. In PowerShell it is the automatic variable $null,
// written bare, in any case; '$null' in single quotes is a file name there.
// Every other target counts as a file, NUL and /dev/null in PowerShell
// included — what either does there is unverified.
func isNullDevice(raw, word string, powershell bool) bool {
	if powershell {
		return strings.EqualFold(raw, "$null")
	}
	return word == "/dev/null"
}

// readDelimited returns the body up to the matching closing byte and the
// index of that byte. It honours nesting, quotes, backslash escapes and
// backquote substitutions, whose content bash cuts out before it looks for
// the closing byte, and skips a ${...} expansion as readBrace reads it
// ("$(echo ${x:-)})" is one substitution). "$$" is one token, so its second
// '$' starts nothing. It reads $(...); ${...} is readBrace's.
func readDelimited(s string, start int, openByte, closeByte byte) (string, int) {
	var body strings.Builder
	var quote byte
	depth := 1
	for i := start; i < len(s); i++ {
		c := s[i]
		if quote != '\'' {
			switch {
			case c == '\\' && i+1 < len(s):
				body.WriteByte(c)
				i++
				body.WriteByte(s[i])
				continue
			case c == '`':
				_, end := readBackquote(s, i+1)
				body.WriteString(s[i : end+1])
				i = end
				continue
			case c == '$' && i+1 < len(s) && s[i+1] == '$':
				body.WriteString("$$")
				i++
				continue
			case c == '$' && i+1 < len(s) && s[i+1] == '{':
				// A body readBrace cannot follow is kept up to the byte it
				// stopped at, which is then read as usual; splitSegments
				// meets it again in the returned body and gives up.
				end, ok := readBrace(s, i+2)
				if !ok {
					end--
				}
				body.WriteString(s[i : end+1])
				i = end
				continue
			}
		}
		if quote != 0 {
			body.WriteByte(c)
			if c == quote {
				quote = 0
			}
			continue
		}
		switch {
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			_, end := readANSIC(s, i+2)
			body.WriteString(s[i : end+1])
			i = end
		case c == '\'' || c == '"':
			quote = c
			body.WriteByte(c)
		case c == openByte:
			depth++
			body.WriteByte(c)
		case c == closeByte:
			depth--
			if depth == 0 {
				return body.String(), i
			}
			body.WriteByte(c)
		default:
			body.WriteByte(c)
		}
	}
	return body.String(), len(s) - 1
}

// readANSIC returns the raw body of an ANSI-C quoted string $'...' — start
// is the index just after its opening quote — and the index of its closing
// quote. Inside it a backslash escapes the next byte, so "\'" does not close
// it; bash finds the end the same way, whatever the escape means (see
// decodeANSIC). Unterminated, the body runs to the end of s.
func readANSIC(s string, start int) (string, int) {
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\'':
			return s[start:i], i
		}
	}
	return s[start:], len(s) - 1
}

// decodeANSIC resolves the escapes of an ANSI-C quoted body as bash 5.2 does:
// \a \b \e \E \f \n \r \t \v and \\ \' \" \?; \nnn with one to three octal
// digits and \xHH with one or two hex digits as a byte; \uHHHH and
// \UHHHHHHHH with up to four and eight hex digits as UTF-8; \cX as control-X
// ("\c?" is DEL, "\c\\" control-backslash). An escape without its digits, and
// any other one, stays as written. A NUL byte ends the value, as in C. A code
// point UTF-8 cannot encode becomes U+FFFD where bash writes raw bytes;
// neither is ASCII, which is all a rule compares.
func decodeANSIC(body string) string {
	var out strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' || i+1 == len(body) {
			out.WriteByte(c)
			continue
		}
		i++
		e := body[i]
		value := -1 // the escape's byte; -1 for one written as is or as UTF-8
		switch e {
		case 'a':
			value = '\a'
		case 'b':
			value = '\b'
		case 'e', 'E':
			value = 0x1b
		case 'f':
			value = '\f'
		case 'n':
			value = '\n'
		case 'r':
			value = '\r'
		case 't':
			value = '\t'
		case 'v':
			value = '\v'
		case '\\', '\'', '"', '?':
			value = int(e)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			n, width := escapeDigits(body[i:], 8, 3)
			value = n & 0xff
			i += width - 1
		case 'x', 'u', 'U':
			limit := 2
			switch e {
			case 'u':
				limit = 4
			case 'U':
				limit = 8
			}
			n, width := escapeDigits(body[i+1:], 16, limit)
			switch {
			case width == 0:
				out.WriteByte('\\')
				out.WriteByte(e)
				continue
			case n == 0:
				return out.String()
			case e == 'x':
				value = n
			default:
				out.WriteRune(rune(n))
			}
			i += width
		case 'c':
			if i+1 == len(body) {
				out.WriteString(`\c`)
				continue
			}
			i++
			x := body[i]
			if x == '\\' && i+1 < len(body) && body[i+1] == '\\' {
				i++
			}
			value = int(x & 0x1f)
			if x == '?' {
				value = 0x7f
			}
		default:
			out.WriteByte('\\')
			out.WriteByte(e)
		}
		switch {
		case value == 0:
			return out.String()
		case value > 0:
			out.WriteByte(byte(value))
		}
	}
	return out.String()
}

// escapeDigits reads up to limit digits of base at the start of s and
// returns their value and how many there were.
func escapeDigits(s string, base, limit int) (value, width int) {
	for width < limit && width < len(s) {
		var d int
		switch c := s[width]; {
		case '0' <= c && c <= '9':
			d = int(c - '0')
		case 'a' <= c && c <= 'f':
			d = int(c-'a') + 10
		case 'A' <= c && c <= 'F':
			d = int(c-'A') + 10
		default:
			d = base
		}
		if d >= base {
			break
		}
		value = value*base + d
		width++
	}
	return value, width
}

// readBackquote returns the body up to the closing backquote and its index.
func readBackquote(s string, start int) (string, int) {
	var body strings.Builder
	for i := start; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			body.WriteByte(s[i])
			continue
		}
		if c == '`' {
			return body.String(), i
		}
		body.WriteByte(c)
	}
	return body.String(), len(s) - 1
}

// nestedScripts returns the segments of a script that a segment hands to
// another shell, e.g. `bash -c "rm -rf /"` or `cmd /c del *`; ok is false
// when that script is undecidable (see shellSegments).
func nestedScripts(segment string, depth int) (segments []string, ok bool) {
	tokens := shellTokens(segment)
	for i := 0; i+2 < len(tokens); i++ {
		shell := commandWord(tokens[i])
		if !isShellName(shell) {
			continue
		}
		for j := i + 1; j+1 < len(tokens); j++ {
			if isScriptFlag(shell, tokens[j]) {
				return splitSegments(strings.Join(tokens[j+1:], " "), depth+1)
			}
		}
		return nil, true
	}
	return nil, true
}

func isShellName(word string) bool {
	switch word {
	case "sh", "bash", "zsh", "dash", "ksh", "ash", "busybox", "cmd", "powershell", "pwsh":
		return true
	}
	return false
}

// isScriptFlag reports whether a token of a shell invocation introduces the
// script. POSIX shells also accept -c combined with other short options
// ("bash -lc", "sh -ec"); cmd and PowerShell parameters are words, where a
// contained "c" means nothing ("-ExecutionPolicy").
func isScriptFlag(shell, token string) bool {
	switch strings.ToLower(token) {
	case "-c", "/c", "/k", "-command", "-encodedcommand":
		return true
	}
	switch shell {
	case "cmd", "powershell", "pwsh":
		return false
	}
	if len(token) < 2 || token[0] != '-' {
		return false
	}
	flags := token[1:]
	for i := 0; i < len(flags); i++ {
		if !isASCIILetter(flags[i]) {
			return false
		}
	}
	return strings.ContainsRune(strings.ToLower(flags), 'c')
}

// isWrapper reports whether a command word merely runs another command, so
// that the wrapped command has to be checked as well ("sudo rm -rf /").
func isWrapper(word string) bool {
	switch word {
	case "sudo", "doas", "runuser", "env", "nohup", "nice", "ionice", "setsid",
		"time", "timeout", "command", "builtin", "exec", "eval", "xargs", "stdbuf", "start":
		return true
	}
	return false
}

// commandVariants returns the spellings of one segment that a deny pattern is
// tested against: the segment itself, the same segment with quotes and escapes
// resolved, the same again with a path-qualified command reduced to its base
// name ("/bin/rm" -> "rm"), and — for wrapper commands — the wrapped command
// lines ("sudo -u root rm -rf /" -> "rm -rf /").
func commandVariants(segment string) []string {
	variants := []string{segment}
	tokens := shellTokens(segment)
	if len(tokens) == 0 {
		return variants
	}
	add := func(ts []string) {
		if len(ts) == 0 {
			return
		}
		variants = append(variants, strings.Join(ts, " "))
		if base := commandWord(ts[0]); base != "" && base != ts[0] {
			variants = append(variants, strings.Join(append([]string{base}, ts[1:]...), " "))
		}
	}
	add(tokens)
	if isWrapper(commandWord(tokens[0])) {
		for i := 1; i < len(tokens); i++ {
			add(tokens[i:])
		}
	}
	return variants
}

// commandWord reduces a command token to a comparable name: base name, lower
// case, without a Windows ".exe" suffix.
func commandWord(token string) string {
	word := token
	if i := strings.LastIndexAny(word, `/\`); i >= 0 {
		word = word[i+1:]
	}
	return strings.TrimSuffix(strings.ToLower(word), ".exe")
}

// shellTokens splits a command line into arguments and resolves quotes and
// backslash escapes, so that `r\m -rf /`, `"rm" -rf /` and `$'\x72m' -rf /`
// all become "rm -rf /".
func shellTokens(s string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	started := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch quote {
		case '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
			continue
		case '"':
			switch {
			case c == '\\' && i+1 < len(s):
				i++
				cur.WriteByte(s[i])
			case c == '"':
				quote = 0
			default:
				cur.WriteByte(c)
			}
			continue
		}

		switch {
		case c == '\'' || c == '"':
			quote = c
			started = true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			started = true
		case c == '$' && i+1 < len(s) && s[i+1] == '$':
			cur.WriteString("$$")
			i++
			started = true
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			body, end := readANSIC(s, i+2)
			cur.WriteString(decodeANSIC(body))
			i = end
			started = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if started {
				out = append(out, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	if started {
		out = append(out, cur.String())
	}
	return out
}

// normalizeSpace collapses runs of whitespace, so that a doubled space cannot
// break a prefix pattern.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// globEqual compares a pattern (optionally with a trailing *) case-sensitively
// against an exact value.
func globEqual(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == value
}

// globMatch behaves like globEqual but is used for inner patterns. A trailing
// * allows a prefix match.
func globMatch(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	return globEqual(pattern, strings.TrimSpace(value))
}
