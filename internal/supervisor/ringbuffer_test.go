package supervisor

import (
	"bytes"
	"strings"
	"testing"
)

// The buffer must keep both ends of a stream and say how much it dropped.
func TestHeadTailBufferKeepsBothEnds(t *testing.T) {
	cases := []struct {
		name   string
		writes []string
		want   string
	}{
		{"fits in the head", []string{"AAAA", "BBBB"}, "AAAABBBB"},
		{"spills into the tail", []string{"AAAA", "BBBB", "CCCC"}, "AAAABBBBCCCC"},
		{
			"drops the middle",
			[]string{"AAAA", "BBBB", "CCCC", "DDDD", "EEEE"},
			"AAAABBBB\n[... 4 bytes omitted]\nDDDDEEEE",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newHeadTailBuffer(8)
			for _, w := range tc.writes {
				if n, err := b.Write([]byte(w)); n != len(w) || err != nil {
					t.Fatalf("Write(%q) = %d, %v", w, n, err)
				}
			}
			if got := b.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// k74: the logs tool must be able to tell a log that lost lines from one that
// is whole, so the buffer counts what it lets go — under the same lock as the
// lines it returns.
func TestRingBufferCountsDroppedLines(t *testing.T) {
	r := newRingBuffer(3)
	for _, l := range []string{"a", "b", "c"} {
		r.add(l)
	}
	if lines, dropped := r.tail(10); strings.Join(lines, ",") != "a,b,c" || dropped != 0 {
		t.Fatalf("full, nothing dropped: tail(10) = %q, %d; want [a b c], 0", lines, dropped)
	}
	r.add("d")
	r.add("e")
	if lines, dropped := r.tail(10); strings.Join(lines, ",") != "c,d,e" || dropped != 2 {
		t.Fatalf("two lines past the size: tail(10) = %q, %d; want [c d e], 2", lines, dropped)
	}
	if lines, dropped := r.tail(1); strings.Join(lines, ",") != "e" || dropped != 2 {
		t.Fatalf("tail(1) = %q, %d; want [e], 2", lines, dropped)
	}
}

// k74: the launcher shows the last n lines of a command's output and has to
// say how many lines of the whole output came before them. Past 2*limit bytes
// the buffer drops the middle, and counting the lines it kept would leave out
// every line in between. Here limit 8 keeps "l1\nl2\nl3" and "6\nl7\nl8\n" of
// eight lines: the middle "\nl4\nl5\nl" goes, and its three lines with it.
func TestCommandResultLinesBefore(t *testing.T) {
	result := func(writes ...string) CommandResult {
		b := newHeadTailBuffer(8)
		for _, w := range writes {
			b.Write([]byte(w))
		}
		return b.result(0)
	}
	dropped := result("l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n")
	if want := "l1\nl2\nl3\n[... 8 bytes omitted]\n6\nl7\nl8\n"; dropped.Output != want {
		t.Fatalf("Output = %q, want %q", dropped.Output, want)
	}
	whole := result("l1\nl2\nl3\n")
	// The kept tail is eight newlines, which trimming removes from Output:
	// its last line is the byte marker, with l1..l3 above it.
	blankTail := result("l1\nl2\nl3\nl4\nl5\nl6\n" + strings.Repeat("\n", 8))
	// A pipe can deliver the closing newlines in writes of their own; all of
	// them end the output, so l8 is still the eighth and last line.
	splitNewlines := result("l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8", "\n", "\n")
	// UE and MSBuild end their lines in \r\n on Windows. Only the \n ends the
	// output; the \r stays on the last line, as Output's trim leaves it.
	crlf := result("l1\r\nl2\r\nl3\r\nl4\r\nl5\r\nl6\r\nl7\r\nl8\r\n")
	for _, tc := range []struct {
		name string
		res  CommandResult
		n    int
		want int
	}{
		{"whole output, cut", whole, 2, 1},
		{"whole output, fits exactly", whole, 3, 0},
		{"whole output, fits", whole, 5, 0},
		// The last n lines lie in the kept tail: everything before them in
		// the whole output counts, the dropped middle included.
		{"dropped middle, tail lines", dropped, 2, 6},
		// "6" is what is left of l6; the lines before it are l1..l5.
		{"dropped middle, the whole kept tail", dropped, 3, 5},
		// The last n lines reach the byte marker, which shows the dropped
		// middle itself: only the kept head lines above it count.
		{"dropped middle, from the marker", dropped, 4, 3},
		{"dropped middle, all of Output", dropped, 7, 0},
		{"dropped middle, more than Output", dropped, 10, 0},
		{"dropped middle, tail of newlines only", blankTail, 1, 3},
		{"dropped middle, newlines written apart", splitNewlines, 2, 6},
		{"dropped middle, \\r\\n lines", crlf, 2, 6},
		// A result the buffer did not build counts Output's own lines.
		{"zero value", CommandResult{Output: "a\nb\nc\n"}, 1, 2},
		// n = 0 shows nothing (tailLines), so every line comes before.
		{"zero value, n = 0", CommandResult{Output: "a\nb\nc\n"}, 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.res.LinesBefore(tc.n); got != tc.want {
				t.Fatalf("LinesBefore(%d) = %d, want %d", tc.n, got, tc.want)
			}
		})
	}
}

// k175: the launcher's reply cap cuts the oldest bytes of the last n lines,
// and when it cuts the "[... N bytes omitted]" line, its own count has to take
// in the N bytes. Gap finds that line among the last n lines and hands over
// N. With limit 8, as above, Output's lines are l1, l2, l3, the marker, 6, l7,
// l8; the middle "\nl4\nl5\nl" was 8 bytes.
func TestCommandResultGap(t *testing.T) {
	result := func(s string) CommandResult {
		b := newHeadTailBuffer(8)
		b.Write([]byte(s))
		return b.result(0)
	}
	dropped := result("l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n")
	// The tail is newlines only: the marker is Output's last line. The
	// middle "\nl4\nl5\nl6\n" was 10 bytes.
	blankTail := result("l1\nl2\nl3\nl4\nl5\nl6\n" + strings.Repeat("\n", 8))
	for _, tc := range []struct {
		name       string
		res        CommandResult
		n          int
		wantIndex  int
		wantBytes  int
		wantInside bool
	}{
		{"tail lines only", dropped, 3, 0, 0, false},
		{"from the marker", dropped, 4, 0, 8, true},
		{"one head line above it", dropped, 5, 1, 8, true},
		{"all of Output", dropped, 7, 3, 8, true},
		{"more than Output", dropped, 10, 3, 8, true},
		{"tail of newlines only", blankTail, 1, 0, 10, true},
		{"tail of newlines only, all of Output", blankTail, 4, 3, 10, true},
		{"n = 0", dropped, 0, 0, 0, false},
		{"nothing dropped", result("l1\nl2\nl3\n"), 3, 0, 0, false},
		{"zero value", CommandResult{Output: "a\nb\nc\n"}, 3, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index, bytes, ok := tc.res.Gap(tc.n)
			if index != tc.wantIndex || bytes != tc.wantBytes || ok != tc.wantInside {
				t.Fatalf("Gap(%d) = %d, %d, %v; want %d, %d, %v", tc.n, index, bytes, ok, tc.wantIndex, tc.wantBytes, tc.wantInside)
			}
		})
	}
}

// k175 review: the last n lines must not start on a line the dropped middle
// splits. The kept tail's first line ("6" above) began in the middle, and the
// marker line stands for it, so counting from either miscounts: a window
// starting there reaches back to the kept head's last line, l3, two lines
// above the tail.
func TestCommandResultWindow(t *testing.T) {
	result := func(s string) CommandResult {
		b := newHeadTailBuffer(8)
		b.Write([]byte(s))
		return b.result(0)
	}
	dropped := result("l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n")                     // tail lines 6, l7, l8
	blankTail := result("l1\nl2\nl3\nl4\nl5\nl6\n" + strings.Repeat("\n", 8)) // the marker is last
	for _, tc := range []struct {
		name string
		res  CommandResult
		n    int
		want int
	}{
		{"below the tail's first line", dropped, 2, 2},
		{"from the tail's first line", dropped, 3, 5},
		{"from the marker", dropped, 4, 5},
		{"from the head's last line", dropped, 5, 5},
		{"above it", dropped, 6, 6},
		{"n = 0", dropped, 0, 0},
		{"tail of newlines only, from the marker", blankTail, 1, 2},
		{"tail of newlines only, from the head's last line", blankTail, 2, 2},
		{"nothing dropped", result("l1\nl2\nl3\n"), 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.res.Window(tc.n); got != tc.want {
				t.Fatalf("Window(%d) = %d, want %d", tc.n, got, tc.want)
			}
		})
	}
}

// The whole point: memory must not grow with the size of the output.
func TestHeadTailBufferMemoryStaysBounded(t *testing.T) {
	const chunk = 64 * 1024
	b := newHeadTailBuffer(1024)
	block := bytes.Repeat([]byte("z"), chunk)
	for i := 0; i < 160; i++ { // 10 MiB
		b.Write(block)
	}
	if cap(b.tail) > 4*chunk {
		t.Fatalf("tail buffer grew to %d bytes for a 10 MiB stream", cap(b.tail))
	}
	if len(b.String()) > 2*b.limit+128 {
		t.Fatalf("String() returned %d bytes, want at most head+tail plus marker", len(b.String()))
	}
}

// runOnce collected a build log in an unbounded bytes.Buffer and then copied it
// into a string, so a UE package run held the whole log twice in memory. The
// result must stay bounded while keeping the start and — what the callers show
// — the end of the output.
func TestRunOnceBoundsCommandOutput(t *testing.T) {
	spec := helperSpec(t, "flood", "flood")
	sup := New(nil)
	res, err := sup.RunOnce(spec.Command, spec.Args, "", spec.Env, "flood")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code %d, want 0", res.ExitCode)
	}
	if len(res.Output) > 2*commandOutputLimit+1024 {
		t.Fatalf("output is %d bytes, want at most head+tail (%d) plus marker",
			len(res.Output), 2*commandOutputLimit)
	}
	if !strings.HasPrefix(res.Output, helperHeadMarker) {
		t.Fatal("start of the output was dropped")
	}
	if !strings.HasSuffix(strings.TrimRight(res.Output, "\n"), helperTailMarker) {
		t.Fatal("end of the output was dropped — that is what the callers show")
	}
	if !strings.Contains(res.Output, "bytes omitted") {
		t.Fatal("dropped output was not reported")
	}
	// The flood helper prints its head marker, helperFloodBytes/1024 lines of
	// 1 KiB and its tail marker. The MiB kept at each end holds about a third
	// of them; the count must cover the dropped middle as well.
	lines := helperFloodBytes/1024 + 2
	if got := res.LinesBefore(300); got != lines-300 {
		t.Fatalf("LinesBefore(300) = %d, want %d: the flood printed %d lines", got, lines-300, lines)
	}
}
