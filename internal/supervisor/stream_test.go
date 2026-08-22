package supervisor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	helperEnv        = "UNREAGENT_TEST_HELPER"
	helperTailMarker = "TAIL-MARKER"
	helperHeadMarker = "HEAD-MARKER"
	helperLongLine   = 3 << 20 // three times the per-line cap
	helperBurstLines = 5000
	helperFloodBytes = 3 << 20 // more than head + tail of a command's output
)

// helperSpec builds a ServiceSpec that re-executes this test binary in the
// given helper mode. Using the test binary as the child keeps the streaming
// tests free of shell tools and behaves the same on every platform.
func helperSpec(t *testing.T, name, mode string) ServiceSpec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return ServiceSpec{
		Name:      name,
		Command:   exe,
		Args:      []string{"-test.run=TestSupervisorHelperProcess"},
		Env:       []string{helperEnv + "=" + mode},
		Autostart: true,
		Restart:   "never",
	}
}

// TestSupervisorHelperProcess is not a test: it is the child process for the
// streaming tests, selected via the helper environment variable.
func TestSupervisorHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		return
	}
	out := bufio.NewWriterSize(os.Stdout, 8192)
	switch mode {
	case "longline":
		out.Write(bytes.Repeat([]byte("x"), helperLongLine))
		out.WriteString("\n" + helperTailMarker + "\n")
	case "burst":
		// Continuous output followed by an immediate exit — an editor dumping
		// its crash log. The last lines are still in the pipe when it dies.
		for i := 0; i < helperBurstLines; i++ {
			fmt.Fprintf(out, "line-%04d %s\n", i, strings.Repeat("y", 180))
		}
	case "flood":
		// A build log too large to keep in memory, bracketed by markers.
		fmt.Fprintln(out, helperHeadMarker)
		line := strings.Repeat("z", 1023) + "\n"
		for i := 0; i < helperFloodBytes/1024; i++ {
			out.WriteString(line)
		}
		fmt.Fprintln(out, helperTailMarker)
	}
	out.Flush()
	os.Exit(0)
}

// runHelper starts the helper as a service and returns its log lines once it
// has exited. Waiting for OnExit is what makes this deterministic: the exit
// path drains the pipes before firing it.
//
// perLine is the cost the log sink takes per line. The real sink writes every
// line to unreagent.log, so a reader that keeps up in a tight loop is not the
// case worth testing.
func runHelper(t *testing.T, mode string, perLine time.Duration) []string {
	t.Helper()
	exited := make(chan struct{})
	sup := New(func(string) { time.Sleep(perLine) })
	spec := helperSpec(t, "x", mode)
	spec.OnExit = func(bool) { close(exited) }
	sup.AddService(spec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)

	select {
	case <-exited:
	case <-time.After(30 * time.Second):
		t.Fatal("helper process did not finish — a stalled reader blocks it on a full pipe")
	}
	lines, err := sup.Logs("x", 0)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	cancel()
	wg.Wait()
	return lines
}

// One line longer than the cap must not end the stream. bufio.Scanner returned
// false there, sc.Err() was never checked and the goroutine returned, so the
// pipe was never drained again and the child blocked on its next write — while
// the supervisor still reported it as running.
func TestStreamSurvivesOverlongLine(t *testing.T) {
	lines := runHelper(t, "longline", 0)

	var xs int
	seenMarker := false
	for _, l := range lines {
		xs += strings.Count(l, "x")
		if l == helperTailMarker {
			seenMarker = true
		}
	}
	if !seenMarker {
		t.Fatalf("output after the over-long line was lost (%d lines captured)", len(lines))
	}
	if xs != helperLongLine {
		t.Fatalf("over-long line was truncated: got %d bytes, want %d", xs, helperLongLine)
	}
}

// What a process wrote just before dying must survive its exit — those are the
// crash lines. cmd.Wait() closes the pipes it created itself as soon as the
// process is gone, which discarded whatever was still buffered.
func TestStreamKeepsOutputWrittenBeforeExit(t *testing.T) {
	lines := runHelper(t, "burst", 20*time.Microsecond)

	if len(lines) == 0 {
		t.Fatal("no output captured at all")
	}
	last := fmt.Sprintf("line-%04d", helperBurstLines-1)
	if !strings.HasPrefix(lines[len(lines)-1], last) {
		t.Fatalf("trailing output lost at process exit: last captured line is %q, want %s",
			firstField(lines[len(lines)-1]), last)
	}
	// The ring buffer keeps the last 500 lines; they must be gapless.
	for i, l := range lines {
		want := fmt.Sprintf("line-%04d", helperBurstLines-len(lines)+i)
		if !strings.HasPrefix(l, want) {
			t.Fatalf("gap in the captured output at position %d: got %q, want %s",
				i, firstField(l), want)
		}
	}
}

func firstField(line string) string {
	if i := strings.IndexByte(line, ' '); i > 0 {
		return line[:i]
	}
	return line
}
