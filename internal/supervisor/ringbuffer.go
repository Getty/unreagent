package supervisor

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
)

// ringBuffer keeps the last N lines of a process output (thread-safe) and
// counts the older lines it dropped.
type ringBuffer struct {
	mu      sync.Mutex
	lines   []string
	max     int
	dropped int
}

func newRingBuffer(max int) *ringBuffer {
	if max <= 0 {
		max = 200
	}
	return &ringBuffer{max: max}
}

func (r *ringBuffer) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if len(r.lines) > r.max {
		r.dropped += len(r.lines) - r.max
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

// tail returns the last n lines (a copy; all of them for n <= 0) and how many
// lines the buffer dropped before the ones it holds.
func (r *ringBuffer) tail(n int) ([]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || n > len(r.lines) {
		n = len(r.lines)
	}
	out := make([]string, n)
	copy(out, r.lines[len(r.lines)-n:])
	return out, r.dropped
}

// commandOutputLimit is how many bytes of a one-shot command's output are kept
// at each end.
const commandOutputLimit = 1 << 20

var newline = []byte("\n")

// headTailBuffer keeps the first and the last limit bytes written to it and
// counts what fell out in between. A full UE package log runs into tens or
// hundreds of megabytes, while the callers only ever show the tail — collecting
// all of it in memory (and then copying it into a string) is what has to be
// avoided.
type headTailBuffer struct {
	limit int
	head  []byte
	tail  []byte
	total int64
	// newlines counts the '\n' bytes written, trailing the ones among them
	// that end the output so far: the dropped middle's lines count too.
	newlines, trailing int64
}

func newHeadTailBuffer(limit int) *headTailBuffer {
	if limit <= 0 {
		limit = 64 * 1024
	}
	return &headTailBuffer{limit: limit}
}

// Write never fails. It is not synchronised: runOnce writes to it from the one
// goroutine draining the command's output and reads it only after joining
// that goroutine.
func (b *headTailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.total += int64(n)
	b.newlines += int64(bytes.Count(p, newline))
	if k := len(bytes.TrimRight(p, "\n")); k > 0 {
		b.trailing = int64(n - k)
	} else {
		b.trailing += int64(n)
	}
	if len(b.head) < b.limit {
		take := b.limit - len(b.head)
		if take > len(p) {
			take = len(p)
		}
		b.head = append(b.head, p[:take]...)
		p = p[take:]
	}
	if len(p) == 0 {
		return n, nil
	}
	b.tail = append(b.tail, p...)
	if len(b.tail) > b.limit {
		// Move the surviving bytes to the front instead of just resliceing, so
		// the backing array stays bounded.
		b.tail = append(b.tail[:0], b.tail[len(b.tail)-b.limit:]...)
	}
	return n, nil
}

// String returns head + tail, with a marker line for the part that was
// dropped.
func (b *headTailBuffer) String() string {
	dropped := b.total - int64(len(b.head)) - int64(len(b.tail))
	if dropped <= 0 {
		return string(b.head) + string(b.tail)
	}
	return fmt.Sprintf("%s\n[... %d bytes omitted]\n%s", b.head, dropped, b.tail)
}

// result is the CommandResult for what was written to b. When the middle was
// dropped, it carries the line counts LinesBefore needs: the whole output's,
// and the kept tail's as Output splits it — none when the tail is newlines
// only, as Output's last line is then the marker. It carries the dropped
// byte count too, for Gap.
func (b *headTailBuffer) result(exitCode int) CommandResult {
	res := CommandResult{Output: b.String(), ExitCode: exitCode}
	if dropped := b.total - int64(len(b.head)+len(b.tail)); dropped > 0 {
		res.dropped = int(dropped)
		res.lines = int(b.newlines - b.trailing + 1)
		if kept := bytes.TrimRight(b.tail, "\n"); len(kept) > 0 {
			res.keptLines = bytes.Count(kept, newline) + 1
		}
	}
	return res
}

// LinesBefore returns how many lines of the command's whole output come before
// the last n lines of Output, lines being what splitting Output at "\n" gives
// once its trailing newlines are trimmed; 0 when Output has at most n lines.
// When the middle of a long output was dropped and those n lines lie below
// the "[... N bytes omitted]" line, the dropped lines count as well; when they
// reach that line, it shows the gap itself, and only the lines above it count.
func (r CommandResult) LinesBefore(n int) int {
	have := strings.Count(strings.TrimRight(r.Output, "\n"), "\n") + 1
	switch {
	case have <= n:
		return 0
	case r.lines > 0 && n <= r.keptLines:
		return r.lines - n
	default:
		return have - n
	}
}

// Gap finds the "[... N bytes omitted]" line among the last n lines of
// Output, lines counted as LinesBefore counts them: its index among those n,
// and the N bytes of the dropped middle it stands for. ok is false when
// nothing was dropped, or when the n lines all lie below that line.
func (r CommandResult) Gap(n int) (index, dropped int, ok bool) {
	if r.dropped == 0 || n <= r.keptLines {
		return 0, 0, false
	}
	have := strings.Count(strings.TrimRight(r.Output, "\n"), "\n") + 1
	return min(n, have) - 1 - r.keptLines, r.dropped, true
}

// Window is how many of Output's last lines to show for its last n, so that
// they start where a line of the whole output starts: n, unless the middle
// was dropped and those n lines would start on the kept tail's first line,
// which can begin inside the middle, or on the "[... N bytes omitted]" line.
// Then it reaches back to the kept head's last line, which begins where a
// line does: two lines above the tail.
func (r CommandResult) Window(n int) int {
	if r.dropped > 0 && n > 0 && r.keptLines <= n && n < r.keptLines+2 {
		return r.keptLines + 2
	}
	return n
}
