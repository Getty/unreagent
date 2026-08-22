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
			"AAAABBBB\n... [4 bytes omitted] ...\nDDDDEEEE",
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
}
