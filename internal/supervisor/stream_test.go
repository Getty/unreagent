package supervisor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	helperLinger     = 5 * time.Second
	// helperHang is how long the "hang" helper sleeps: far longer than any
	// bound a cancellation test waits for, so returning early proves a kill.
	helperHang = 60 * time.Second
	// helperReadyEnv names a file the "hang" helper writes its PID to once it is
	// up, so a test cancels a running process instead of racing its start, and
	// a failing test can still kill it.
	helperReadyEnv = "UNREAGENT_TEST_READY"
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
	case "orphan":
		// A build tool that leaves a process behind (MSBuild node reuse): the
		// grandchild inherits stdout and outlives this process by far.
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(out, "os.Executable:", err)
			out.Flush()
			os.Exit(1)
		}
		gc := exec.Command(exe, "-test.run=TestSupervisorHelperProcess")
		gc.Env = append(os.Environ(), helperEnv+"=linger")
		gc.Stdout = os.Stdout
		if err := gc.Start(); err != nil {
			fmt.Fprintln(out, "grandchild start:", err)
			out.Flush()
			os.Exit(1)
		}
		fmt.Fprintln(out, helperTailMarker)
	case "linger":
		time.Sleep(helperLinger)
	case "hang":
		// A command that never finishes on its own — npm against a stalled
		// registry. What it wrote before it got stuck must survive the kill.
		fmt.Fprintln(out, helperHeadMarker)
		out.Flush()
		if p := os.Getenv(helperReadyEnv); p != "" {
			_ = os.WriteFile(p, []byte(fmt.Sprint(os.Getpid())), 0o600)
		}
		time.Sleep(helperHang)
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

// A one-shot command whose grandchild inherits its output and outlives it must
// not hold RunOnce for the grandchild's whole lifetime. exec.Cmd.Wait() waited
// for every holder of the pipe to let go, and the job that would end the
// grandchild was only closed after that — a run_command handler hung for as
// long as MSBuild kept its reused node alive. What the command wrote itself
// must still be in the result.
func TestRunOnceReturnsWhileGrandchildHoldsOutput(t *testing.T) {
	spec := helperSpec(t, "orphan", "orphan")
	sup := New(nil)
	// Short enough to keep the test fast on Linux, where no job object ends the
	// grandchild and the drain bound is what returns; long enough that a busy
	// runner still copies the marker out of the pipe before it.
	sup.drainTimeout = 300 * time.Millisecond

	start := time.Now()
	res, err := sup.RunOnce(spec.Command, spec.Args, "", spec.Env, "orphan")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// The grandchild keeps the pipe open for helperLinger at least, so waiting
	// for it can never come in under this bound. Returning after the command
	// plus the drain bound takes about 0.3s, and 1.3s under -race (the race
	// runtime sleeps a second before the helper exits) — far below it even on a
	// slow runner.
	if bound := 3 * time.Second; elapsed >= bound {
		t.Fatalf("RunOnce returned after %s (bound %s): it waited for the grandchild (lives %s), not for the command",
			elapsed.Round(time.Millisecond), bound, helperLinger)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code %d, want 0 (output %q)", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, helperTailMarker) {
		t.Fatalf("output written before the exit was lost: %q", res.Output)
	}
	t.Logf("RunOnce returned after %s", elapsed.Round(time.Millisecond))
}

func firstField(line string) string {
	if i := strings.IndexByte(line, ' '); i > 0 {
		return line[:i]
	}
	return line
}
