// Regression tests for karr ticket k177: Claude Code shortens an error reply
// (isError) longer than about 11,000 characters to its first and last 5,000,
// which drops the middle of a failing build's output, where its first errors
// usually are. A failing run_command, run_python or run_node reply is held to
// 10,000 bytes now: the first lines, about 4 KiB, a marker that counts what
// it leaves out, the middle the supervisor dropped past 2 MiB included, and
// as many of the last lines as fit. A successful reply keeps its 48 KiB cap.
package main

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/mcp"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// failedCap and failedHead are k177's limits, written out here rather than
// taken from the code under test: the whole reply, and the head's share.
const (
	failedCap  = 10000
	failedHead = 4096
)

// failedLinesMarker and failedBytesMarker are the lines a failed reply puts
// between its head and its tail.
func failedLinesMarker(n int) string {
	unit := "lines"
	if n == 1 {
		unit = "line"
	}
	return fmt.Sprintf("[... %d %s omitted]", n, unit)
}

func failedBytesMarker(n int) string {
	return fmt.Sprintf("[... %d bytes omitted; a failing call's reply holds at most 10 KB]", n)
}

// splitFailed splits a failed reply below top into its head, its marker line
// and its tail. The marker is the first line that starts with "[... ".
func splitFailed(t *testing.T, top, got string) (head, marker, tail string) {
	t.Helper()
	rest, ok := strings.CutPrefix(got, top)
	if !ok {
		t.Fatalf("reply does not start with %q: %.120q…", top, got)
	}
	i := strings.Index(rest, "\n[... ")
	if i < 0 {
		t.Fatalf("no marker line in the reply: %.200q…", rest)
	}
	marker, tail, _ = strings.Cut(rest[i+1:], "\n")
	return rest[:i], marker, tail
}

// checkFailed fails unless got is the failed reply for lines below top: at
// most failedCap bytes; the lines unchanged when they fit; otherwise a head
// that starts the output, a tail that ends it, and between them a marker
// that names exactly what is missing. The head is the first lines within
// failedHead bytes, or the start of the first line when that alone is
// longer. The tail is as many of the last lines as fit, or the end of the
// last line when that alone does not, the marker reserved at its longest:
// the byte marker for every byte. Where both ends are whole lines, the
// marker counts the lines between them, otherwise their bytes.
func checkFailed(t *testing.T, top string, lines []string, got string) {
	t.Helper()
	joined := strings.Join(lines, "\n")
	if len(got) > failedCap {
		t.Fatalf("reply is %d bytes, more than the %d cap", len(got), failedCap)
	}
	if len(top)+len(joined) <= failedCap {
		if got != top+joined {
			t.Fatalf("a reply of %d bytes that fits was changed:\n got  %.120q…\n want %.120q…", len(top)+len(joined), got, top+joined)
		}
		return
	}
	head, marker, tail := splitFailed(t, top, got)
	if !strings.HasPrefix(joined, head) {
		t.Fatalf("the head is not the start of the output: %.80q…", head)
	}
	if !strings.HasSuffix(joined, tail) {
		t.Fatalf("the tail is not the end of the output: …%.80q", tail[max(0, len(tail)-80):])
	}
	if len(head)+len(tail) >= len(joined) {
		t.Fatalf("head (%d bytes) and tail (%d bytes) overlap in %d bytes of output", len(head), len(tail), len(joined))
	}
	if utf8.ValidString(joined) && !(utf8.ValidString(head) && utf8.ValidString(tail)) {
		t.Fatalf("a cut split a UTF-8 sequence: head ends %q, tail starts %q", head[max(0, len(head)-4):], tail[:min(4, len(tail))])
	}

	wholeHead, headLines := len(lines[0]) <= failedHead, 1
	if wholeHead {
		e, n := 0, -1
		for e < len(lines) && n+1+len(lines[e]) <= failedHead {
			n += 1 + len(lines[e])
			e++
		}
		if want := strings.Join(lines[:e], "\n"); head != want {
			t.Fatalf("head is %d bytes, %d lines; want the first %d lines, %d bytes", len(head), strings.Count(head, "\n")+1, e, len(want))
		}
		headLines = e
	} else if len(head) < failedHead-(utf8.UTFMax-1) || len(head) > failedHead {
		t.Fatalf("the head cut of a %d-byte first line keeps %d bytes, want %d less at most %d", len(lines[0]), len(head), failedHead, utf8.UTFMax-1)
	}

	room := failedCap - len(top) - len(head) - 2 - len(failedBytesMarker(len(joined)))
	if len(tail) > room {
		t.Fatalf("tail is %d bytes, more than the %d left for it", len(tail), room)
	}
	k := strings.Count(tail, "\n") + 1
	s := len(lines) - k
	wholeTail := tail == strings.Join(lines[s:], "\n")
	switch {
	case wholeTail:
		if s-1 >= headLines && len(tail)+1+len(lines[s-1]) <= room {
			t.Fatalf("the tail stops short: the %d-byte line above it fits in the %d bytes left", len(lines[s-1]), room-len(tail))
		}
	case k != 1:
		t.Fatalf("a cut tail must be the end of one line, got %d lines", k)
	case len(tail) < room-(utf8.UTFMax-1):
		t.Fatalf("the tail cut of the last line keeps %d bytes, want %d less at most %d", len(tail), room, utf8.UTFMax-1)
	}

	want := failedBytesMarker(len(joined) - len(head) - len(tail))
	if wholeHead && wholeTail {
		want = failedLinesMarker(len(lines) - headLines - k)
	}
	if marker != want {
		t.Fatalf("marker = %q, want %q", marker, want)
	}
}

// failedLines is n lines of about width bytes each: line i starts with i in
// six digits, so a head or tail shows where it was taken from, then pad
// ASCII bytes, then r repeated. pad shifts every rune boundary against the
// head's cut; the length of the exit line shifts it against the tail's.
func failedLines(n, width int, r string, pad int) []string {
	fill := strings.Repeat("a", pad) + strings.Repeat(r, max(width-6-pad, 0)/len(r))
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%06d", i+1) + fill
	}
	return lines
}

// Sizes and line lengths around the cap and the head's share, one line and
// many, long lines first, last and at both ends, in 1- to 4-byte runes.
func TestCapFailedSweep(t *testing.T) {
	tops := []string{"exit 1\n\n", "exit 12\n\n", "exit 123\n\n", "exit -1073741819\n\n"}
	type shape struct {
		name  string
		lines func(r string, pad int) []string
	}
	uniform := func(n, width int) shape {
		return shape{fmt.Sprintf("%d×%d", n, width), func(r string, pad int) []string { return failedLines(n, width, r, pad) }}
	}
	shapes := []shape{
		uniform(1, 9990), uniform(1, 10010), uniform(1, 300<<10),
		uniform(2, 4096), uniform(2, 6000), uniform(2, 300<<10),
		uniform(3, 4095), uniform(3, 4097),
		uniform(40, 99), uniform(40, 999), uniform(99, 99), uniform(101, 99),
		uniform(500, 9), uniform(1000, 99), uniform(1000, 999), uniform(200, 4095), uniform(5000, 7),
		{"a long first line", func(r string, pad int) []string {
			return append(failedLines(1, 300<<10, r, pad), failedLines(50, 99, r, pad)...)
		}},
		{"a long last line", func(r string, pad int) []string {
			return append(failedLines(50, 99, r, pad), failedLines(1, 300<<10, r, pad)...)
		}},
		{"long lines at both ends", func(r string, pad int) []string {
			long := failedLines(1, 300<<10, r, pad)
			return append(append(long, failedLines(50, 99, r, pad)...), long...)
		}},
		{"empty lines", func(string, int) []string { return make([]string, 20000) }},
	}
	for _, r := range []string{"a", "é", "€", "😀"} {
		for pad := 0; pad < len(r); pad++ {
			for _, sh := range shapes {
				lines := sh.lines(r, pad)
				for _, top := range tops {
					t.Run(fmt.Sprintf("%s/pad %d/%s/top %d", r, pad, sh.name, len(top)), func(t *testing.T) {
						checkFailed(t, top, lines, capFailed(top, lines, -1, 0, len(lines)))
					})
				}
			}
		}
	}
}

// The edges of the cap, a byte either side, and an output that fits though
// it has more lines than the 300 a successful reply shows: a failing one is
// held by its bytes alone.
func TestCapFailedLeavesAReplyThatFits(t *testing.T) {
	const top = "exit 1\n\n"
	many := widthLines(99, 99)
	many = append(many, longLine(failedCap-len(top)-len(strings.Join(many, "\n"))-1))
	for _, tc := range []struct {
		name  string
		lines []string
		fits  bool
	}{
		{"one line at the cap", []string{longLine(failedCap - len(top))}, true},
		{"one line a byte over", []string{longLine(failedCap - len(top) + 1)}, false},
		{"lines at the cap", many, true},
		{"lines a byte over", append(many[:len(many)-1:len(many)-1], many[len(many)-1]+"x"), false},
		{"500 short lines", widthLines(500, 9), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := capFailed(top, tc.lines, -1, 0, len(tc.lines))
			if want := top + strings.Join(tc.lines, "\n"); (got == want) != tc.fits {
				t.Fatalf("reply of %d bytes for %d bytes of output: unchanged = %v, want %v", len(got), len(want), got == want, tc.fits)
			}
			checkFailed(t, top, tc.lines, got)
		})
	}
}

// One reply written out: 1,000 lines of 99 bytes. The head is 40 of them,
// 3,999 bytes; the 41st would make 4,099. That leaves 10,000 - 8 for the exit
// line - 2 line breaks - 69 for the longest marker - 3,999 = 5,922 bytes, so
// the tail is the last 59 lines, 5,899 bytes.
func TestCapFailedKeepsTheFirstAndLastLines(t *testing.T) {
	const top = "exit 1\n\n"
	lines := widthLines(1000, 99)
	want := top + strings.Join(lines[:40], "\n") + "\n[... 901 lines omitted]\n" + strings.Join(lines[941:], "\n")
	if got := capFailed(top, lines, -1, 0, len(lines)); got != want {
		t.Fatalf("reply:\n got  %d bytes, %.120q…\n want %d bytes, %.120q…", len(got), got, len(want), want)
	}
}

// failingCommand adds a command to sup that prints content and exits 1.
func failingCommand(t *testing.T, sup *supervisor.Supervisor, name, content string) {
	t.Helper()
	sup.AddCommand(name, supervisor.CommandSpec{Command: "sh", Args: []string{"-c", "cat '" + catFile(t, content) + "'; exit 1"}})
}

// k177 with k175, end to end: past 2 MiB the supervisor keeps 1 MiB at each
// end and puts a line "[... N bytes omitted]" between them. The failed
// reply's marker counts the middle with what else it leaves out, checked
// against the output itself; the kept tail's first line begins in the
// middle and the kept head's last line runs on into it, so neither shows as
// a whole line.
func TestRunCommandFailedCountsTheDroppedMiddle(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not in PATH — skipping")
	}
	const top = "exit 1\n\n"
	mib := 1 << 20
	block := func(n, width int) string { return strings.Join(widthLines(n, width), "\n") + "\n" }
	x3 := "short1\nshort2\n" + strings.Repeat("X", 3*mib)
	// Checked against the output's own lines.
	checked := map[string]string{
		// The head is 4 whole lines and the tail the last ones; the lines
		// marker takes in the middle's.
		"3 MiB of 1,000-byte lines": block(3200, 999),
		// The kept head is one line, cut; the tail is the 50 short lines.
		"a 3 MiB line, then short ones": strings.Repeat("Z", 3*mib) + "\n" + block(50, 99),
		// The kept tail is one line that begins in the middle, cut.
		"short lines, then a 1.5 MiB line": block(16000, 99) + strings.Repeat("Y", 1536<<10) + "\n",
	}
	gapLine := func(content string) string { return fmt.Sprintf("[... %d bytes omitted]", len(content)-2*mib) }
	// Written out: the kept tail is line breaks but for a few bytes, and the
	// kept head's last line is too long for the tail, so the tail cannot
	// start where an output line does above the gap line.
	written := map[string]struct {
		content string
		want    func(content string) string
	}{
		// The tail would be the gap line and "line", which begins in the
		// middle; it is "line" alone, counted in bytes.
		"the kept tail one short line": {x3 + "line" + strings.Repeat("\n", mib-4), func(c string) string {
			return top + "short1\nshort2\n" + failedBytesMarker(len(strings.TrimRight(c, "\n"))-13-4) + "\nline"
		}},
		// It is "cd", whole, and "ab" counts as the line it ends.
		"the kept tail two short lines": {x3 + "ab\ncd" + strings.Repeat("\n", mib-5), func(string) string {
			return top + "short1\nshort2\n" + failedLinesMarker(1) + "\ncd"
		}},
		// The kept tail is line breaks only: the gap line ends the reply,
		// and the marker above it counts the kept head's bytes the head
		// leaves out, the line break before the gap line not among them.
		"the kept tail line breaks only": {x3 + strings.Repeat("\n", mib), func(c string) string {
			return top + "short1\nshort2\n" + failedBytesMarker(mib-13) + "\n" + gapLine(c)
		}},
	}
	// The kept tail's last line is 9 bytes, the kept head's last 76: the
	// tail holds the gap line with both, and the marker counts the kept
	// head's lines between head and tail alone.
	together := block(22000, 99) + strings.Repeat("\n", mib-10)

	sup := supervisor.New(nil)
	for name, content := range checked {
		failingCommand(t, sup, name, content)
	}
	for name, tc := range written {
		failingCommand(t, sup, name, tc.content)
	}
	failingCommand(t, sup, "together", together)
	srv := toolsServerOn(t, sup, config.Permissions{}, func(string) {})
	run := func(t *testing.T, name string) string {
		t.Helper()
		res := callTool(t, srv, "run_command", map[string]interface{}{"name": name})
		if !res.IsError {
			t.Fatalf("run_command of a command that exits 1: IsError false, %.200q", res.Text)
		}
		return res.Text
	}

	for name, content := range checked {
		t.Run(name, func(t *testing.T) {
			checkFailed(t, top, strings.Split(strings.TrimRight(content, "\n"), "\n"), run(t, name))
		})
	}
	for name, tc := range written {
		t.Run(name, func(t *testing.T) {
			if got, want := run(t, name), tc.want(tc.content); got != want {
				t.Fatalf("reply:\n got  %q\n want %q", got, want)
			}
		})
	}
	t.Run("the tail holds the gap line whole", func(t *testing.T) {
		got := run(t, "together")
		if len(got) > failedCap {
			t.Fatalf("reply is %d bytes, more than the %d cap", len(got), failedCap)
		}
		head, marker, tail := splitFailed(t, top, got)
		lines := widthLines(22000, 99)
		if want := strings.Join(lines[:40], "\n"); head != want {
			t.Fatalf("head = %.80q…, want the first 40 lines", head)
		}
		// The tail's first line reads its number k.
		var k int
		if _, err := fmt.Sscanf(tail[:99], "%d", &k); err != nil || k <= 41 {
			t.Fatalf("tail starts %.120q: not a line below the head (%v)", tail, err)
		}
		want := together[(k-1)*100:mib] + "\n" + gapLine(together) + "\n" + strings.TrimRight(together[len(together)-mib:], "\n")
		if tail != want {
			t.Fatalf("tail = %q…, want %q…", tail[:min(300, len(tail))], want[:min(300, len(want))])
		}
		if want := failedLinesMarker(k - 1 - 40); marker != want {
			t.Fatalf("marker = %q, want %q", marker, want)
		}
	})
}

// A failing reply is shaped by capFailed, a successful one keeps the 48 KiB
// cap: 30,000 bytes come back whole on exit 0, and within 10,000 on exit 1,
// from run_command and from run_python and run_node's scriptAction alike.
func TestFailedReplyLeavesSuccessAlone(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not in PATH — skipping")
	}
	content := strings.Join(widthLines(300, 99), "\n") + "\n"
	path := catFile(t, content)
	sup := supervisor.New(nil)
	sup.AddCommand("succeeds", supervisor.CommandSpec{Command: "cat", Args: []string{path}})
	failingCommand(t, sup, "fails", content)
	srv := toolsServerOn(t, sup, config.Permissions{}, func(string) {})
	script := scriptAction(supervisor.New(nil), "sh", nil, t.TempDir(), "sh", "test")
	lines := splitOutput(content, len(content))

	for name, run := range map[string]func(t *testing.T, fail bool) toolCallResult{
		"run_command": func(t *testing.T, fail bool) toolCallResult {
			name := "succeeds"
			if fail {
				name = "fails"
			}
			return callTool(t, srv, "run_command", map[string]interface{}{"name": name})
		},
		"scriptAction": func(_ *testing.T, fail bool) toolCallResult {
			code := "cat '" + path + "'"
			if fail {
				code += "; exit 1"
			}
			res := script(map[string]interface{}{"code": code})
			return toolCallResult{Text: res.Text, IsError: res.IsError}
		},
	} {
		t.Run(name, func(t *testing.T) {
			if res, want := run(t, false), "exit 0\n\n"+strings.Join(lines, "\n"); res.IsError || res.Text != want {
				t.Fatalf("exit 0 (IsError %v): got %d bytes, %.120q…, want the whole output, %d bytes", res.IsError, len(res.Text), res.Text, len(want))
			}
			res := run(t, true)
			if !res.IsError {
				t.Fatalf("exit 1: IsError false, %.120q…", res.Text)
			}
			checkFailed(t, "exit 1\n\n", lines, res.Text)
		})
	}
}

// The tool descriptions say how a failing call's reply is cut.
func TestRunToolsDescribeTheFailedReply(t *testing.T) {
	cfg := &config.Config{}
	cfg.Runtimes.Python.Enabled = true
	cfg.Runtimes.Node.Enabled = true
	srv := mcp.NewServer("tools-test", "0", nil)
	registerTools(srv, supervisor.New(nil), cfg, t.TempDir(), nil, func(string) {})
	desc := toolDescriptions(t, srv)
	const clause = "A failing call's reply instead keeps the first and last part of the output within 10 KB."
	for _, name := range []string{"run_command", "run_python", "run_node"} {
		if !strings.Contains(desc[name], clause) {
			t.Errorf("%s description lacks %q: %q", name, clause, desc[name])
		}
	}
}
