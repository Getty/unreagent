package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// warnIfMissing warns when path names nothing that could be started, by the
// rules exec.Command applies when the service starts: a bare name is looked
// up on PATH, anything else is checked by pathProblem — as given, and on
// Windows also with a PATHEXT extension added.
func warnIfMissing(logger func(string), label, path string) {
	if filepath.Base(path) == path {
		found, err := exec.LookPath(path)
		switch {
		case errors.Is(err, exec.ErrDot):
			logger(fmt.Sprintf("WARN %s: %s is found only relative to the current directory (%s), which exec refuses — give its full path", label, path, found))
		case err != nil:
			logger(fmt.Sprintf("WARN %s: %s not found on PATH", label, path))
		}
		return
	}
	if why := pathProblem(path); why != "" {
		logger(fmt.Sprintf("WARN %s: path %s: %s", label, why, path))
	}
}

// pathProblem says why path, which has a directory part, names nothing that
// exec.Command starts: "not found", "is a directory" or "is not executable";
// it is "" when exec starts it. On Windows exec adds a PATHEXT extension to a
// path that has none, so C:/UE/Engine/Binaries/Win64/UnrealEditor starts
// UnrealEditor.exe; exec.LookPath applies that rule to a path with
// separators and does not search PATH for it, and whatever it refuses is
// "not found". Elsewhere exec adds nothing: the path must exist as given and
// be a file this process may execute. That is decided as exec.LookPath
// decides it, by access(2) for the process's effective user rather than by
// whether any execute bit is set (LookPath falls back to that only where
// access is unavailable), so a file that only another user may execute
// warns too: execve refuses it as well.
func pathProblem(path string) string {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath(path); err != nil {
			return "not found"
		}
		return ""
	}
	fi, err := os.Stat(path)
	switch {
	case err != nil:
		return "not found"
	case fi.IsDir():
		return "is a directory"
	}
	// With a "/" in path, LookPath checks only that file, not PATH.
	if _, err := exec.LookPath(path); err != nil {
		return "is not executable"
	}
	return ""
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// writeTarget returns the file a write to path replaces: the target of a
// symlinked file, or path itself. Only a symlinked file is resolved: a path
// through a symlinked directory stays as given, so errors name the directory
// the user knows, and the rename lands in the real directory either way. A
// dangling symlink is refused: the real file usually lives somewhere that is
// unavailable right now, and writing would replace the link with a new file
// that holds none of it. So is a link that cannot be resolved at all, such as
// a loop. Every error names path (config.ResolveLink).
func writeTarget(path string) (string, error) {
	target, err := config.ResolveLink(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w; refusing to replace the link with a new file", err)
	}
	return target, err
}

// writeFileAtomic is the writer for every file the launcher writes that can
// hold a secret: the MCP configs (mcp.writeConfig, -write-mcp-config, the
// agent's --mcp-config file) and hermes-setup's unreagent.local.yaml and
// Hermes config (k178). It writes data to a temp file <name>.tmp-<random> in
// the target's directory, synced to disk, then renamed over path, so a kill
// mid-write leaves the old file whole; on Windows, too, the rename replaces
// an existing file. On a failure the temp file is removed, the old file is
// left as it was and the mode returned is 0. A symlinked file is followed and
// its target replaced, so the link stays a link; a dangling one is refused
// (see writeTarget). A new file gets 0600. A replaced file keeps its
// permission bits, and when they let group or others at it, the mode
// returned is those bits (0 for a closed file and for a new one). With
// tighten such a file gets 0600 instead, so the mode returned is the one it
// had. The mode goes onto the temp file before the rename, so the data is
// never on disk under the old bits. On Windows the bits do not decide who
// can read the file (it inherits its directory's ACL), so nothing is
// tightened or returned there. The directory must be writable for the temp
// file; there is no in-place fallback.
func writeFileAtomic(path string, data []byte, tighten bool) (os.FileMode, error) {
	path, err := writeTarget(path)
	if err != nil {
		return 0, err
	}
	mode, open := os.FileMode(0o600), os.FileMode(0)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if runtime.GOOS != "windows" && mode&0o077 != 0 {
		open = mode
		if tighten {
			mode = 0o600
		}
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("create a temp file in directory %s: %w", dir, err)
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return 0, err
	}
	return open, nil
}

func errResult(err error) mcp.ToolResult {
	return mcp.ToolResult{Text: "Error: " + err.Error(), IsError: true}
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// toolOutputLines is how many lines of a command's output run_command,
// run_python and run_node return when it succeeds (failedReplyBytes for when
// it fails).
const toolOutputLines = 300

// toolReplyBytes caps a successful run_*, a logs or a read_file reply
// (readFileLimit is derived from it). Claude Code saves a text reply longer
// than 50,000 characters to a file instead of putting it in the agent's
// context; 48 KiB of bytes is at most 49,152 characters, marker included.
// Uncapped, 300 long lines (or one) can reach the 2 MiB the supervisor keeps
// of a command's output, and the log buffer's 500 lines can be up to 1 MiB
// each.
const toolReplyBytes = 48 << 10

// replyCutReason is why capReply's marker cut: "a reply holds at most 48 KiB".
var replyCutReason = fmt.Sprintf("a reply holds at most %d KiB", toolReplyBytes>>10)

// failedReplyBytes caps the reply of a one-shot command that failed, its
// "exit N" line and markers included. Claude Code shortens an error reply
// (isError) longer than about 11,000 characters to its first and last 5,000,
// and the middle it drops is where a failing build's first errors usually
// stand. 10,000 bytes are at most 10,000 characters, so Claude Code never
// cuts such a reply, and what capFailed leaves out is marked and counted.
const failedReplyBytes = 10000

// failedHeadBytes is how much of a failed command's output capFailed keeps
// from its start, for the first errors; the tail gets what is left.
const failedHeadBytes = 4 << 10

// failedCutReason is why capFailed's byte marker cut: "a failing call's reply
// holds at most 10 KB".
var failedCutReason = fmt.Sprintf("a failing call's reply holds at most %d KB", failedReplyBytes/1000)

// commandResult is a one-shot tool's result: commandReply for a command that
// exited 0, failedReply, as an error, for one that did not.
func commandResult(res supervisor.CommandResult) mcp.ToolResult {
	if res.ExitCode != 0 {
		return mcp.ToolResult{Text: failedReply(res), IsError: true}
	}
	return mcp.ToolResult{Text: commandReply(res, toolOutputLines)}
}

// failedReply is a failed command's reply: "exit N", a blank line and its
// output, held to failedReplyBytes by capFailed, which keeps the start and
// the end of it. LinesBefore(0) is how many lines the whole output has, a
// middle the supervisor dropped included.
func failedReply(res supervisor.CommandResult) string {
	lines := strings.Split(strings.TrimRight(res.Output, "\n"), "\n")
	gap, dropped, ok := res.Gap(len(lines))
	if !ok {
		gap = -1
	}
	return capFailed(fmt.Sprintf("exit %d\n\n", res.ExitCode), lines, gap, dropped, res.LinesBefore(0))
}

// commandReply is a one-shot tool's reply: "exit N", a blank line and the
// last n lines of the command's output, led by an omittedMarker line when it
// printed more, the whole held to toolReplyBytes by capReply. Where the
// supervisor dropped the middle, the lines reach back as far as Window says,
// so that both counts are of the output's own lines and bytes.
func commandReply(res supervisor.CommandResult, n int) string {
	n = res.Window(n)
	head := fmt.Sprintf("exit %d\n\n", res.ExitCode)
	if k := res.LinesBefore(n); k > 0 {
		head += omittedMarker(k, "") + "\n"
	}
	gap, dropped, ok := res.Gap(n)
	if !ok {
		gap = -1
	}
	return capReply(head, strings.Split(tailLines(res.Output, n), "\n"), gap, dropped)
}

// capReply is head followed by lines joined with "\n", held to at most
// toolReplyBytes. When that is longer, the oldest bytes of the lines go:
// whole lines, then the start of the oldest line kept, which may be the only
// one. A line "[... N bytes omitted; a reply holds at most 48 KiB]" right
// after head says how many. The cut never splits a UTF-8 sequence, and a
// line it leaves empty goes with its line break. head is never cut. Only the
// lines kept are joined, so a log buffer of long lines is not copied whole.
//
// gap is the index in lines of the supervisor's "[... N bytes omitted]" line
// for a dropped middle, dropped its N; gap is -1 when lines hold none. The
// output never held that line or the line breaks around it, so what the cut
// takes of them does not count. When the cut reaches the line, the whole line
// goes and N counts instead: the count is of the output's own bytes.
func capReply(head string, lines []string, gap, dropped int) string {
	size := max(len(lines)-1, 0) // the line breaks of the join
	for _, l := range lines {
		size += len(l)
	}
	if len(head)+size <= toolReplyBytes {
		return head + strings.Join(lines, "\n")
	}
	// The marker is sized for the most that can be omitted, so the reply
	// stays within the cap whatever count it ends up naming.
	most := size
	if gap >= 0 {
		most += dropped
	}
	keep := max(toolReplyBytes-len(head)-len(bytesMarker(most, replyCutReason))-1, 0)
	// Walk back to the oldest line the kept bytes reach into; rest counts the
	// bytes after it, its line break included.
	i, rest := len(lines)-1, 0
	for i > 0 && rest+len(lines[i]) < keep {
		rest += len(lines[i]) + 1
		i--
	}
	first := lines[i]
	cut := len(first) - (keep - rest)
	for k := 1; k < utf8.UTFMax && cut < len(first) && !utf8.RuneStart(first[cut]); k++ {
		cut++
	}
	if i == gap && cut > 0 {
		cut = len(first) // the rest of the marker would name no count
	}
	if cut == len(first) {
		i, cut = i+1, 0 // nothing of it is kept: it goes with its line break
	}
	// The text kept is lines[i:], the first of them less its first cut bytes.
	kept := lines[i:]
	if cut > 0 {
		kept = append([]string{lines[i][cut:]}, lines[i+1:]...)
	}
	body := strings.Join(kept, "\n")
	omitted := size - len(body)
	if gap >= 0 {
		// The output never held the marker line or the line breaks around
		// it; when the line went, the bytes it stands for count instead.
		if gap > 0 && gap <= i {
			omitted-- // the line break before it
		}
		if gap < i {
			omitted += dropped - len(lines[gap])
			if gap < len(lines)-1 {
				omitted-- // the line break after it
			}
		}
	}
	return head + bytesMarker(omitted, replyCutReason) + "\n" + body
}

// capFailed is top followed by lines joined with "\n", held to at most
// failedReplyBytes. When that is longer, it keeps a head, the first lines
// within failedHeadBytes, and a tail, the last lines within what is left,
// and between them a line "[... N lines omitted]" that counts every line it
// leaves out. Lines go whole. Only a single line longer than its share is
// cut: the head keeps the start of the first line, the tail the end of the
// last, which may be the same one. The text left out then starts or ends
// inside a line, and the marker counts its bytes instead, as capReply counts
// them: "[... N bytes omitted; a failing call's reply holds at most 10 KB]".
// No cut splits a UTF-8 sequence, and top is never cut.
//
// total is how many lines the output has: len(lines), unless the supervisor
// dropped its middle. Then gap is the index in lines of its "[... N bytes
// omitted]" line and dropped its N; gap is -1 when lines hold none. Where a
// count takes in that line, it takes in the middle instead, its lines from
// total, its bytes from dropped, and not the line or the line breaks around
// it, which the output never held. The lines next to it are not whole: the
// one above runs on into the middle, the one below begins in it. So the tail
// holds those three together or none of them, as Window keeps them; where it
// would start on the gap line or the one below, it starts below both, and
// when that leaves it no line, it is the last line alone under the byte
// marker. The head never reaches them, as the supervisor keeps 1 MiB above.
func capFailed(top string, lines []string, gap, dropped, total int) string {
	size := max(len(lines)-1, 0) // the line breaks of the join
	for _, l := range lines {
		size += len(l)
	}
	if len(top)+size <= failedReplyBytes {
		return top + strings.Join(lines, "\n")
	}
	// The marker is sized for the most either unit can count, so the reply
	// stays within the cap whatever it ends up naming.
	most := size
	if gap >= 0 {
		most += dropped
	}
	room := failedReplyBytes - len(top) - 2 - // the line breaks around the marker
		max(len(bytesMarker(most, failedCutReason)), len(omittedMarker(max(total, len(lines)), "")))

	// The head: its first e lines, a bytes joined, or the start of the first.
	e, a := 0, -1
	for e < len(lines) && a+1+len(lines[e]) <= failedHeadBytes {
		a += 1 + len(lines[e])
		e++
	}
	cut := e == 0
	var head string
	if cut {
		a = failedHeadBytes
		for k := 1; k < utf8.UTFMax && a > 0 && !utf8.RuneStart(lines[0][a]); k++ {
			a--
		}
		head = lines[0][:a]
	} else {
		head = strings.Join(lines[:e], "\n")
	}
	room -= a

	// The tail: the lines from s on, below the head's.
	s, t := len(lines), -1
	for s > max(e, 1) && t+1+len(lines[s-1]) <= room {
		t += 1 + len(lines[s-1])
		s--
	}
	if gap >= 0 && (s == gap || s == gap+1) {
		s = min(gap+2, len(lines))
	}
	from := s // the line the tail starts in
	var tail string
	if s == len(lines) {
		// No whole line: the end of the last one.
		cut, from = true, len(lines)-1
		last := lines[from]
		c := max(len(last)-room, 0)
		for k := 1; k < utf8.UTFMax && c < len(last) && !utf8.RuneStart(last[c]); k++ {
			c++
		}
		tail = last[c:]
	} else {
		tail = strings.Join(lines[s:], "\n")
	}

	var marker string
	if cut {
		omitted := size - a - len(tail)
		switch {
		case gap >= 0 && gap < from:
			omitted += dropped - len(lines[gap])
			if gap > 0 {
				omitted-- // the line break before it
			}
			if gap < len(lines)-1 {
				omitted-- // the line break after it
			}
		case gap > 0 && gap == from:
			omitted-- // the tail is the gap line; the line break before it is not the output's
		}
		marker = bytesMarker(omitted, failedCutReason)
	} else {
		n := s - e
		if gap >= 0 && gap < s {
			n += total - len(lines)
		}
		marker = omittedMarker(n, "")
	}
	reply := top + head + "\n" + marker
	if tail != "" {
		reply += "\n" + tail
	}
	return reply
}

// omittedMarker is the line a tool reply puts where k lines are left out,
// with why after a semicolon when given: "[... 100 lines omitted]". It
// matches the supervisor's "[... N bytes omitted]".
func omittedMarker(k int, why string) string {
	return omittedLine(k, "line", why)
}

// bytesMarker is omittedMarker for k bytes: "[... 100 bytes omitted]".
func bytesMarker(k int, why string) string {
	return omittedLine(k, "byte", why)
}

// omittedLine is the marker line for k units left out, why after a semicolon.
func omittedLine(k int, unit, why string) string {
	if k != 1 {
		unit += "s"
	}
	s := fmt.Sprintf("[... %d %s omitted", k, unit)
	if why != "" {
		s += "; " + why
	}
	return s + "]"
}

func getString(args map[string]interface{}, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

// stringArg reads a string argument and reports whether the key was actually
// present and held a string. Unlike getString it tells a missing key apart from
// an empty value, which matters wherever "" is a legal payload but a typo in the
// key name would otherwise silently destroy data.
func stringArg(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key].(string)
	return v, ok
}

// getInt reads an integer argument. A JSON number arrives as float64 and must
// be integral and fit an int; a numeric string ("5") is accepted too, because
// some MCP clients send every argument as a string. An absent key, null or ""
// yields def, as for getString. Anything else is an error, not a silent
// default.
func getInt(args map[string]interface{}, key string, def int) (int, error) {
	switch v := args[key].(type) {
	case nil:
		return def, nil
	case float64:
		if v != math.Trunc(v) { // also NaN
			return 0, fmt.Errorf("'%s' must be an integer, got %v", key, v)
		}
		// -float64(math.MinInt) is the first value past math.MaxInt;
		// float64(math.MaxInt) rounds up to it and cannot be the bound.
		if v < float64(math.MinInt) || v >= -float64(math.MinInt) {
			return 0, intRangeErr(key, fmt.Sprint(v))
		}
		return int(v), nil
	case int:
		return v, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return def, nil
		}
		n, err := strconv.Atoi(s)
		if errors.Is(err, strconv.ErrRange) {
			return 0, intRangeErr(key, strconv.Quote(v))
		}
		if err != nil {
			return 0, fmt.Errorf("'%s' must be an integer, got %q", key, v)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("'%s' must be an integer, got %T", key, v)
	}
}

// intRangeErr refuses an integer argument that does not fit an int; got is
// the value as the caller received it.
func intRangeErr(key, got string) error {
	return fmt.Errorf("'%s' must be an integer between %d and %d, got %s", key, math.MinInt, math.MaxInt, got)
}

func firstString(args map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func firstMap(args map[string]interface{}, keys ...string) map[string]interface{} {
	for _, k := range keys {
		if v, ok := args[k].(map[string]interface{}); ok {
			return v
		}
	}
	return nil
}
