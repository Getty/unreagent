// Regression tests for karr ticket k149: a run_* or logs reply had no byte
// cap. A run_* reply could reach about 2 MiB when its last 300 lines were
// long, and logs lines=50 up to 50 MiB, since a log line can be 1 MiB. Every
// reply must stay within toolReplyBytes, keep the newest output, and say how
// many bytes it cut, without splitting a UTF-8 sequence. k175: that count
// takes in the middle the supervisor dropped past 2 MiB.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// cutMarker is the line a reply cut to the cap carries, written out here
// rather than built by the code under test.
func cutMarker(n int) string {
	return fmt.Sprintf("[... %d bytes omitted; a reply holds at most 48 KiB]", n)
}

// widthLines is n lines of width digits each, line i reading i, so a kept
// suffix shows where the cut fell.
func widthLines(n, width int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%0*d", width, i+1)
	}
	return lines
}

// longLine is one line of size bytes, "000000001,000000002,…" cut to size.
func longLine(size int) string {
	var b strings.Builder
	for i := 1; b.Len() < size; i++ {
		fmt.Fprintf(&b, "%09d,", i)
	}
	return b.String()[:size]
}

// checkCapped fails unless got is a reply cut to the cap: head, the cut
// marker naming exactly how many bytes of the joined lines are missing, then
// the rest of them. It returns that rest. The reply must use the cap, less
// the few bytes a UTF-8 boundary, a dropped line break and the marker's
// digits can cost, so returning the marker alone does not pass.
func checkCapped(t *testing.T, head string, lines []string, got string) string {
	t.Helper()
	joined := strings.Join(lines, "\n")
	if len(got) > toolReplyBytes {
		t.Fatalf("reply is %d bytes, more than the %d cap", len(got), toolReplyBytes)
	}
	if len(got) < toolReplyBytes-16 {
		t.Errorf("reply is %d bytes, well short of the %d cap: the cut kept less than it could", len(got), toolReplyBytes)
	}
	rest, ok := strings.CutPrefix(got, head)
	if !ok {
		t.Fatalf("reply does not start with %q: %.120q…", head, got)
	}
	markerLine, body, ok := strings.Cut(rest, "\n")
	if !ok {
		t.Fatalf("no line after the head: %.120q…", rest)
	}
	if !strings.HasSuffix(joined, body) {
		t.Fatalf("what the reply keeps is not the newest output: starts %.80q…", body)
	}
	if want := cutMarker(len(joined) - len(body)); markerLine != want {
		t.Fatalf("marker line = %q, want %q", markerLine, want)
	}
	if utf8.ValidString(joined) && !utf8.ValidString(body) {
		t.Fatalf("the cut split a UTF-8 sequence: kept text starts %q", body[:min(8, len(body))])
	}
	return body
}

func TestCapReplyLeavesAReplyThatFits(t *testing.T) {
	const head = "exit 0\n\n"
	one := longLine(toolReplyBytes - len(head))
	many := widthLines(100, 99)
	many = append(many, longLine(toolReplyBytes-len(head)-len(strings.Join(many, "\n"))-1))
	for _, tc := range []struct {
		name  string
		lines []string
	}{
		{"one line at the cap", []string{one}},
		{"one line a byte under the cap", []string{one[1:]}},
		{"lines at the cap", many},
		{"no lines", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := head + strings.Join(tc.lines, "\n")
			if got := capReply(head, tc.lines, -1, 0); got != want {
				t.Fatalf("capReply changed a reply of %d bytes:\n got  %.120q…\n want %.120q…", len(want), got, want)
			}
		})
	}
}

func TestCapReplyCutsTheOldestBytes(t *testing.T) {
	const head = "exit 0\n\n[... 7 lines omitted]\n"
	over := widthLines(100, 99)
	over = append(over, longLine(toolReplyBytes-len(head)-len(strings.Join(over, "\n"))-1+1))
	for _, tc := range []struct {
		name  string
		lines []string
	}{
		{"a byte over the cap", over},
		{"long lines", widthLines(300, 999)},
		{"a single oversized line", []string{longLine(300 << 10)}},
		{"an oversized line before short ones", append([]string{longLine(300 << 10)}, widthLines(10, 9)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkCapped(t, head, tc.lines, capReply(head, tc.lines, -1, 0))
		})
	}
}

// The cut lands a few bytes either side of the line break between a long
// line a and the line b after it. Where it would keep nothing of a, a goes
// with its line break: the kept text does not start with an empty line.
func TestCapReplyDropsALineTheCutLeavesEmpty(t *testing.T) {
	const head = "exit 0\n\n"
	a := longLine(100000)
	// keep is what capReply keeps of the lines: the size of the joined lines
	// is 100001+len(b), six digits for every b below.
	keep := toolReplyBytes - len(head) - len(cutMarker(100000)) - 1
	for _, tc := range []struct {
		lenB     int
		wantBody func(b string) string
	}{
		{keep, func(b string) string { return b }},     // the cut falls on b's start
		{keep - 1, func(b string) string { return b }}, // on the line break: a and the break go
		{keep - 2, func(b string) string { return a[len(a)-1:] + "\n" + b }},
	} {
		t.Run(fmt.Sprint(tc.lenB), func(t *testing.T) {
			b := longLine(tc.lenB)
			got := capReply(head, []string{a, b}, -1, 0)
			if body := checkCapped(t, head, []string{a, b}, got); body != tc.wantBody(b) {
				t.Fatalf("kept text starts %.40q…, want %.40q…", body, tc.wantBody(b))
			}
		})
	}
}

// Every alignment of the cut against 2-, 3- and 4-byte sequences, in the
// only line and in the oldest of two.
func TestCapReplyNeverSplitsAUTF8Sequence(t *testing.T) {
	const head = "exit 0\n\n"
	for _, r := range []string{"é", "€", "😀"} {
		for pad := 0; pad < utf8.UTFMax; pad++ {
			runes := strings.Repeat(r, (200<<10)/len(r))
			for name, lines := range map[string][]string{
				"one line":  {runes + strings.Repeat("a", pad)},
				"two lines": {runes, strings.Repeat("a", 1000+pad)},
			} {
				t.Run(fmt.Sprintf("%s/pad %d/%s", r, pad, name), func(t *testing.T) {
					checkCapped(t, head, lines, capReply(head, lines, -1, 0))
				})
			}
		}
	}
}

// catFile writes content to a file and returns its path, for cat to print.
func catFile(t *testing.T, content string) string {
	t.Helper()
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not in PATH — skipping")
	}
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// splitOutput is content as a tool splits it into lines: trailing line
// breaks trimmed, the last n lines.
func splitOutput(content string, n int) []string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	return lines[max(len(lines)-n, 0):]
}

// End to end: run_command's reply through the MCP server, the output printed
// by a real command.
func TestRunCommandCapsTheReply(t *testing.T) {
	const head = "exit 0\n\n"
	line := longLine(toolReplyBytes - len(head))
	fits := map[string]string{
		"at the cap":           line + "\n",
		"a byte under the cap": line[1:] + "\n",
	}
	cut := map[string]struct{ content, head string }{
		"a byte over the cap":     {line + "x\n", head},
		"300 long lines":          {strings.Join(widthLines(300, 999), "\n") + "\n", head},
		"400 long lines":          {strings.Join(widthLines(400, 999), "\n") + "\n", head + "[... 100 lines omitted]\n"},
		"a single oversized line": {longLine(300 << 10), head},
		"3-byte runes, pad 0":     {strings.Repeat("€", 40000), head},
		"3-byte runes, pad 1":     {strings.Repeat("€", 40000) + "a", head},
		"3-byte runes, pad 2":     {strings.Repeat("€", 40000) + "ab", head},
	}
	sup := supervisor.New(nil)
	for name, content := range fits {
		sup.AddCommand(name, supervisor.CommandSpec{Command: "cat", Args: []string{catFile(t, content)}})
	}
	for name, tc := range cut {
		sup.AddCommand(name, supervisor.CommandSpec{Command: "cat", Args: []string{catFile(t, tc.content)}})
	}
	srv := toolsServerOn(t, sup, config.Permissions{}, func(string) {})

	for name, content := range fits {
		t.Run(name, func(t *testing.T) {
			res := callTool(t, srv, "run_command", map[string]interface{}{"name": name})
			if want := head + strings.TrimRight(content, "\n"); res.IsError || res.Text != want {
				t.Fatalf("run_command (IsError %v) changed a reply of %d bytes: got %d bytes, %.120q…", res.IsError, len(want), len(res.Text), res.Text)
			}
		})
	}
	for name, tc := range cut {
		t.Run(name, func(t *testing.T) {
			res := callTool(t, srv, "run_command", map[string]interface{}{"name": name})
			if res.IsError {
				t.Fatalf("run_command: IsError, %.200q", res.Text)
			}
			checkCapped(t, tc.head, splitOutput(tc.content, toolOutputLines), res.Text)
		})
	}
}

// k175: past 2 MiB the supervisor drops the middle of a command's output and
// leaves a line "[... N bytes omitted]" in its place. When the cut takes that
// line, the cut marker's count has to take in the N bytes and leave out the
// line's own text and the line breaks around it, which the output never
// held. Each case gives the output its lines stand for, from the start of
// the first one; the count is of what the reply does not show of it.
func TestCapReplyCountsTheDroppedMiddle(t *testing.T) {
	const head = "exit 0\n\n"
	const d = 1000000
	gapLine := fmt.Sprintf("[... %d bytes omitted]", d)
	middle := strings.Repeat("m", d)
	// The kept head ends mid-line, as the buffer cuts it: its last line goes
	// on in the middle.
	hs := strings.Join(widthLines(100, 99), "\n") + "\npartial"
	ts := strings.Join(widthLines(100, 999), "\n")
	split := func(s string) []string { return strings.Split(s, "\n") }
	for _, tc := range []struct {
		name   string
		lines  []string
		gap    int
		output string
	}{
		{"head lines above the marker", slices.Concat(split(hs), []string{gapLine}, split(ts)), 101, hs + middle + ts},
		// The line marker counts the head's lines; the bytes from the middle on are left.
		{"the marker first", slices.Concat([]string{gapLine}, split(ts)), 0, middle + ts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkCapped(t, head, []string{tc.output}, capReply(head, tc.lines, tc.gap, d))
		})
	}

	// The cut falls inside the marker line, keeping 5 bytes of it. Those
	// would name no count, so the line goes whole.
	t.Run("the cut inside the marker", func(t *testing.T) {
		// What capReply keeps: the marker's count has the 7 digits of d.
		keep := toolReplyBytes - len(head) - len(cutMarker(d)) - 1
		tail := longLine(keep - 1 - 5)
		got := capReply(head, slices.Concat(split(hs), []string{gapLine, tail}), 101, d)
		if want := head + cutMarker(len(hs)+d) + "\n" + tail; got != want {
			t.Fatalf("reply:\n got  %.120q…\n want %.120q…", got, want)
		}
	})

	// k175 review: the cut stops right at the marker line, which is kept
	// whole. The line break before it, which the cut takes, is not the
	// output's either. Either the cut falls on the line's start, or the
	// head's last line goes with that break.
	for name, short := range map[string]int{"on the marker's start": 1, "the line above goes": 2} {
		t.Run("the cut stops at the marker, "+name, func(t *testing.T) {
			keep := toolReplyBytes - len(head) - len(cutMarker(d)) - 1
			tail := longLine(keep - short - len(gapLine))
			got := capReply(head, slices.Concat(split(hs), []string{gapLine, tail}), 101, d)
			if want := head + cutMarker(len(hs)) + "\n" + gapLine + "\n" + tail; got != want {
				t.Fatalf("reply:\n got  %.120q…\n want %.120q…", got, want)
			}
		})
	}

	// Control: the cut stops short of the marker, which then shows the
	// middle itself, and the count is of the head bytes cut alone.
	t.Run("the marker kept", func(t *testing.T) {
		lines := slices.Concat(split(ts), []string{gapLine, "a short tail"})
		if body := checkCapped(t, head, lines, capReply(head, lines, 100, d)); !strings.Contains(body, "\n"+gapLine+"\n") {
			t.Fatalf("the marker line went: kept text ends %q", body[max(0, len(body)-80):])
		}
	})
}

// k175 end to end: run_command's reply for an output past 2 MiB, printed by
// a real command, its cut marker's count checked against that output.
func TestRunCommandCountsTheDroppedMiddle(t *testing.T) {
	const head = "exit 0\n\n"
	cases := []struct {
		name    string
		content string
		skip    int // lines above the reply, as its line marker counts them
	}{
		// The card's case. The 1 MiB kept at each end holds 105 lines, so
		// the 211 lines kept, the marker included, are fewer than 300: no
		// line marker, and the cut takes the byte marker.
		{"3 MiB of 10,000-byte lines", strings.Join(widthLines(315, 9999), "\n") + "\n", 0},
		// The kept head is 8,192 lines of 128 bytes. The last 300 lines are
		// the tail's 105, the marker, the empty line the head's last line
		// break leaves before it, and 193 head lines.
		{"short lines, then long ones", strings.Join(widthLines(12288, 127), "\n") + "\n" + strings.Join(widthLines(158, 9999), "\n") + "\n", 8192 - 193},
		// Control: the last 300 lines lie in the kept tail, and the line
		// marker counts the middle's lines (k74).
		{"3 MiB of 1,000-byte lines", strings.Join(widthLines(3200, 999), "\n") + "\n", 3200 - 300},
	}
	// k175 review: the last 300 lines would start on the kept tail's first
	// line, which begins in the dropped middle, when the last MiB holds 300
	// lines, and on the marker line when it holds 299. The reply then reaches
	// back to the first MiB's last line, and the line marker counts the
	// first MiB's whole lines.
	block := func(n, width int) string { return strings.Join(widthLines(n, width), "\n") + "\n" }
	headEnd := block(128, 8191) // exactly 1 MiB: the head ends in a line break
	for name, content := range map[string]string{
		"the last MiB holds 300 lines":                   block(900, 3500),
		"the last MiB holds 299 lines":                   block(900, 3510),
		"300 lines, the head ending in a line break":     headEnd + block(600, 3500),
		"299 lines, the head ending in a line break":     headEnd + block(600, 3510),
		"a 2.9 MiB line, then 299 short ones":            strings.Repeat("H", 2900<<10) + "\n" + block(299, 9),
		"the last MiB holds 302 lines, the window as is": block(900, 3480),
	} {
		skip := strings.Count(content[:1<<20], "\n")
		if strings.HasSuffix(name, "as is") {
			skip = 900 - 300
		}
		cases = append(cases, struct {
			name    string
			content string
			skip    int
		}{name, content, skip})
	}
	// Control, from the k175 re-review: a 2.9 MiB line, then 300 short ones.
	// The last MiB holds 301 lines, the long line's end counted, so the
	// window stays as it is: the 300 short lines, whole, below the one line
	// above them, and the reply fits.
	short := block(300, 9)
	const fits = "a 2.9 MiB line, then 300 short ones"
	sup := supervisor.New(nil)
	sup.AddCommand(fits, supervisor.CommandSpec{Command: "cat", Args: []string{catFile(t, strings.Repeat("H", 2900<<10)+"\n"+short)}})
	for _, tc := range cases {
		sup.AddCommand(tc.name, supervisor.CommandSpec{Command: "cat", Args: []string{catFile(t, tc.content)}})
	}
	srv := toolsServerOn(t, sup, config.Permissions{}, func(string) {})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callTool(t, srv, "run_command", map[string]interface{}{"name": tc.name})
			if res.IsError {
				t.Fatalf("run_command: IsError, %.200q", res.Text)
			}
			h := head
			if tc.skip > 0 {
				h += fmt.Sprintf("[... %d lines omitted]\n", tc.skip)
			}
			checkCapped(t, h, strings.Split(strings.TrimRight(tc.content, "\n"), "\n")[tc.skip:], res.Text)
		})
	}
	t.Run(fits, func(t *testing.T) {
		res := callTool(t, srv, "run_command", map[string]interface{}{"name": fits})
		if want := head + "[... 1 line omitted]\n" + strings.TrimRight(short, "\n"); res.IsError || res.Text != want {
			t.Fatalf("run_command (IsError %v): got %d bytes, %.120q…, want %d bytes, %.120q…", res.IsError, len(res.Text), res.Text, len(want), want)
		}
	})
}

// run_python and run_node share scriptAction; sh stands in for uv/node.
func TestScriptActionCapsTheReply(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not in PATH — skipping")
	}
	content := longLine(300 << 10)
	path := catFile(t, content)
	handler := scriptAction(supervisor.New(nil), "sh", nil, t.TempDir(), "sh", "test")
	res := handler(map[string]interface{}{"code": "cat '" + path + "'"})
	if res.IsError {
		t.Fatalf("script: IsError, %.200q", res.Text)
	}
	checkCapped(t, "exit 0\n\n", []string{content}, res.Text)
}

// logs: long lines, the k74 line marker above the cut, and a single log line
// larger than the cap.
func TestLogsCapsTheReply(t *testing.T) {
	const dropped = "[... 100 lines omitted; the log keeps only the last 500]\n"
	for _, tc := range []struct {
		name, content string
		lines         int
		head          string
	}{
		{"200 long lines", strings.Join(widthLines(200, 999), "\n") + "\n", 200, ""},
		{"600 long lines, 100 dropped", strings.Join(widthLines(600, 999), "\n") + "\n", 1000, dropped},
		{"a single oversized line", longLine(300<<10) + "\n", 50, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := toolsServerOn(t, exitedService(t, "cat '"+catFile(t, tc.content)+"'"), config.Permissions{}, func(string) {})
			res := callTool(t, srv, "logs", map[string]interface{}{"service": "ue", "lines": tc.lines})
			if res.IsError {
				t.Fatalf("logs: IsError, %.200q", res.Text)
			}
			checkCapped(t, tc.head, splitOutput(tc.content, supervisor.LogLines), res.Text)
		})
	}

	// The control: ten of those lines fit and come back whole.
	content := strings.Join(widthLines(200, 999), "\n") + "\n"
	srv := toolsServerOn(t, exitedService(t, "cat '"+catFile(t, content)+"'"), config.Permissions{}, func(string) {})
	res := callTool(t, srv, "logs", map[string]interface{}{"service": "ue", "lines": 10})
	if want := strings.Join(splitOutput(content, 10), "\n"); res.IsError || res.Text != want {
		t.Fatalf("logs lines=10 (IsError %v): got %d bytes, want the last ten lines, %d bytes", res.IsError, len(res.Text), len(want))
	}
}
