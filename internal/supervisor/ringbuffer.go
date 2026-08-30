package supervisor

import (
	"fmt"
	"sync"
)

// ringBuffer keeps the last N lines of a process output (thread-safe).
type ringBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
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
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

// tail returns the last n lines (a copy).
func (r *ringBuffer) tail(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || n > len(r.lines) {
		n = len(r.lines)
	}
	out := make([]string, n)
	copy(out, r.lines[len(r.lines)-n:])
	return out
}

// commandOutputLimit is how many bytes of a one-shot command's output are kept
// at each end.
const commandOutputLimit = 1 << 20

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
}

func newHeadTailBuffer(limit int) *headTailBuffer {
	if limit <= 0 {
		limit = 64 * 1024
	}
	return &headTailBuffer{limit: limit}
}

// Write never fails. It is not synchronised: os/exec calls it from a single
// goroutine as long as the same writer is used for stdout and stderr.
func (b *headTailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.total += int64(n)
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

// String returns head + tail, with a marker for the part that was dropped.
func (b *headTailBuffer) String() string {
	dropped := b.total - int64(len(b.head)) - int64(len(b.tail))
	if dropped <= 0 {
		return string(b.head) + string(b.tail)
	}
	return fmt.Sprintf("%s\n... [%d bytes omitted] ...\n%s", b.head, dropped, b.tail)
}
