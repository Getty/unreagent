package config

import (
	"fmt"
	"path"
	"strings"
)

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
func (p Permissions) allowCommand(toolName, command string) Decision {
	deny := Decision{Allow: false, Message: fmt.Sprintf("no allow entry matches %q", toolName)}
	segments := shellSegments(command)
	covered := make([]bool, len(segments))
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
			if !covered[i] && globMatch(inner, segment) {
				covered[i], hit = true, true
			}
		}
		if hit {
			used = append(used, rule)
		}
	}
	if len(segments) == 0 {
		// Nothing recognisable to match against.
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
	switch key {
	case "command":
		// The allow side of a command line is decided across all rules in
		// allowCommand; a single allow rule cannot answer it and does not
		// grant.
		return sense == senseDeny && denyCommand(pattern, value)
	case "file_path", "path", "notebook_path":
		return matchPath(pattern, value)
	}
	return globMatch(pattern, value)
}

// matchPath applies a path pattern to a path. Both sides are normalised
// first — backslashes become slashes, "." and ".." are resolved — so that
// neither the two spellings Claude Code produces on Windows ("C:\Proj\a"
// and "C:/Proj/a") nor a traversal ("/proj/../etc/passwd") can walk around a
// rule. A pattern that names a drive letter compares case-insensitively, like
// the file system it refers to; everything else stays case-sensitive.
//
// YAML note: inside double quotes a backslash is an escape character and
// "Write(C:\Proj\*)" is rejected by the parser, so a Windows pattern has to
// be single-quoted ('Write(C:\Proj\*)') or written with forward slashes.
func matchPath(pattern, value string) bool {
	pattern = normalizePath(strings.TrimSpace(pattern))
	value = normalizePath(strings.TrimSpace(value))
	if hasDriveLetter(pattern) {
		pattern = strings.ToLower(pattern)
		value = strings.ToLower(value)
	}
	return globEqual(pattern, value)
}

// normalizePath turns backslashes into slashes and cleans the path. A drive
// letter is kept in front of the cleaned remainder so that ".." cannot climb
// above the drive root ("C:\..\x" is "C:\x" on Windows). A trailing "*"
// survives path.Clean unchanged.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if hasDriveLetter(p) {
		if len(p) == 2 {
			return p
		}
		return p[:2] + path.Clean(p[2:])
	}
	return path.Clean(p)
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
		// Nothing recognisable to match against.
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
// shell scripts.
const maxShellDepth = 4

// shellSegments splits a command line into the individual commands a shell
// would execute. It breaks on the unquoted operators ; && || | & newline and
// on the grouping characters ( ) and the stand-alone words { }, and
// additionally yields the bodies of command substitutions ($(...) and
// backticks, also inside a ${...} expansion) plus the scripts handed to a
// nested shell ("bash -c ...", "cmd /c ...") as segments of their own. A
// substitution stays part of the segment it appears in, so that
// "rm -rf $(pwd)" is still an "rm -rf " command.
func shellSegments(command string) []string {
	return splitSegments(command, 0)
}

func splitSegments(command string, depth int) []string {
	if depth > maxShellDepth {
		return nil
	}
	var out []string
	var cur strings.Builder
	flush := func() {
		segment := normalizeSpace(cur.String())
		cur.Reset()
		if segment == "" {
			return
		}
		out = append(out, segment)
		out = append(out, nestedScripts(segment, depth)...)
	}

	var quote byte
	for i := 0; i < len(command); i++ {
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
			case c == '$' && i+1 < len(command) && command[i+1] == '(':
				body, next := readDelimited(command, i+2, '(', ')')
				out = append(out, splitSegments(body, depth+1)...)
				cur.WriteString(command[i : next+1])
				i = next
			case c == '`':
				body, next := readBackquote(command, i+1)
				out = append(out, splitSegments(body, depth+1)...)
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
		case c == '$' && i+1 < len(command) && command[i+1] == '(':
			body, next := readDelimited(command, i+2, '(', ')')
			out = append(out, splitSegments(body, depth+1)...)
			cur.WriteString(command[i : next+1])
			i = next
		case c == '$' && i+1 < len(command) && command[i+1] == '{':
			body, next := readDelimited(command, i+2, '{', '}')
			out = append(out, substitutions(body, depth+1)...)
			cur.WriteString(command[i : next+1])
			i = next
		case c == '`':
			body, next := readBackquote(command, i+1)
			out = append(out, splitSegments(body, depth+1)...)
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
	return out
}

// substitutions returns the segments of every command substitution inside a
// ${...} expansion body ("${x:-$(id)}" runs id) without treating the body
// itself as a command.
func substitutions(s string, depth int) []string {
	if depth > maxShellDepth {
		return nil
	}
	var out []string
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
		case c == '\\' && i+1 < len(s):
			i++
		case c == '\'' && quote == 0:
			quote = c
		case c == '"':
			if quote == '"' {
				quote = 0
			} else {
				quote = c
			}
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			body, next := readDelimited(s, i+2, '(', ')')
			out = append(out, splitSegments(body, depth+1)...)
			i = next
		case c == '$' && i+1 < len(s) && s[i+1] == '{':
			body, next := readDelimited(s, i+2, '{', '}')
			out = append(out, substitutions(body, depth+1)...)
			i = next
		case c == '`':
			body, next := readBackquote(s, i+1)
			out = append(out, splitSegments(body, depth+1)...)
			i = next
		}
	}
	return out
}

// isSegmentBreak reports whether the unquoted byte at i starts a new command.
// & is an operator unless it belongs to a redirection (2>&1, &>file, >&2);
// { and } only group commands when they stand alone as a word ("{ a; b; }"),
// not inside one ("{}", "file{,.bak}").
func isSegmentBreak(command string, i int) bool {
	switch command[i] {
	case ';', '|', '\n', '\r', '(', ')':
		return true
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

// readDelimited returns the body up to the matching closing byte (honouring
// nesting and quotes) and the index of that byte.
func readDelimited(s string, start int, openByte, closeByte byte) (string, int) {
	var body strings.Builder
	var quote byte
	depth := 1
	for i := start; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && quote == '"' && i+1 < len(s) {
				body.WriteByte(c)
				i++
				body.WriteByte(s[i])
				continue
			}
			body.WriteByte(c)
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			body.WriteByte(c)
		case openByte:
			depth++
			body.WriteByte(c)
		case closeByte:
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
// another shell, e.g. `bash -c "rm -rf /"` or `cmd /c del *`.
func nestedScripts(segment string, depth int) []string {
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
		return nil
	}
	return nil
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
// backslash escapes, so that `r\m -rf /` and `"rm" -rf /` both become
// "rm -rf /".
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
