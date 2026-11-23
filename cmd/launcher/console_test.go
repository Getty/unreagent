package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// pipedConsole is a console on a real OS pipe, the way piped stdin reaches
// the launcher: what the test writes sits in the pipe until somebody reads
// it, however much that reader takes.
func pipedConsole(t *testing.T) (console, *os.File, *syncBuffer) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	out := &syncBuffer{}
	return console{in: &lineReader{in: r}, out: out, logSeparate: true}, w, out
}

// k121: the exit prompt read its line through a bufio.Scanner of its own and
// the launcher console through another. With piped input a scanner takes
// whatever the pipe holds, so after 'k' the console found the lines typed
// ahead gone and never saw the 'q'. The prompt and the console share one
// reader that takes no more than the line it returns.
func TestPipedInputReachesThePromptAndThenTheConsole(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("false not on PATH")
	}
	con, w, out := pipedConsole(t)
	sess, _, logs := agentSessionOn(t, con, "false")
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "> ") {
		if time.Now().After(deadline) {
			t.Fatalf("no exit prompt within 10s; console:\n%s", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// One write, as a script piping its input delivers it.
	if _, err := w.WriteString("k\nstatus\nq\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-sess.ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatalf("the console never read the 'q' typed after 'k'; console:\n%s\nlog:\n%s", out, logs)
	}
	if !strings.Contains(logs.String(), `"name": "agent"`) {
		t.Fatalf("the console did not run the 'status' typed after 'k'; log:\n%s", logs)
	}
}

// What the console reader leaves in the pipe is what the foreground agent
// reads after a handover (k71): the reader must stop at the line end, CRLF
// included, and leave the rest where it was.
func TestConsoleReaderTakesNoMoreThanItsLine(t *testing.T) {
	con, w, _ := pipedConsole(t)
	if _, err := w.WriteString("start agent\r\nhello agent\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if line, ok := con.in.readLine(); !ok || line != "start agent" {
		t.Fatalf("readLine = %q, %v; want %q, true", line, ok, "start agent")
	}
	_ = w.Close()
	rest, err := io.ReadAll(con.in.in)
	if err != nil {
		t.Fatalf("read the rest: %v", err)
	}
	if got, want := string(rest), "hello agent\n"; got != want {
		t.Fatalf("the pipe holds %q after one line, want %q: the reader took input meant for the next reader", got, want)
	}
}

// k213: Windows PowerShell 5.1 starts what it pipes into a native program
// with a byte order mark when its $OutputEncoding is UTF-8, so
// 'quit' | unreagent.exe reached the launcher console as "\xef\xbb\xbfquit": an
// unknown command, and the launcher kept running. The console takes the q.
func TestConsoleQuitsOnAPipedLineAfterAByteOrderMark(t *testing.T) {
	con, w, out := pipedConsole(t)
	if _, err := w.WriteString("\xef\xbb\xbfq\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = w.Close()
	stopped := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		commandLoop(context.Background(), con, func() { close(stopped) }, supervisor.New(nil), func(string) {}, "")
	}()
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatalf("the console did not end on q and EOF within 10s; console:\n%s", out)
	}
	select {
	case <-stopped:
	default:
		t.Fatalf("the console did not take the q after the byte order mark; console:\n%s", out)
	}
}

// The mark is the stream's, not a line's: PowerShell writes it once, before
// the first byte it pipes, however many lines follow and however far apart
// they come. The reader drops it from the first line only; a U+FEFF further on
// is input like any other.
func TestConsoleReaderDropsTheByteOrderMarkOfTheStream(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     []string
	}{
		{"LF", "\xef\xbb\xbfq\n", []string{"q"}},
		{"PowerShell's CRLF lines", "\xef\xbb\xbfquit\r\nstatus\r\n", []string{"quit", "status"}},
		{"no line end", "\xef\xbb\xbfq", []string{"q"}},
		{"nothing but the mark", "\xef\xbb\xbf", nil},
		{"once per stream", "\xef\xbb\xbfq\n\xef\xbb\xbfs\n", []string{"q", "\xef\xbb\xbfs"}},
		{"first line without the mark", "q\n\xef\xbb\xbfs\n", []string{"q", "\xef\xbb\xbfs"}},
		{"two marks", "\xef\xbb\xbf\xef\xbb\xbfq\n", []string{"\xef\xbb\xbfq"}},
		{"partial mark", "\xef\xbbq\n", []string{"\xef\xbbq"}},
		{"no mark", "q\n", []string{"q"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &lineReader{in: strings.NewReader(tc.in)}
			var got []string
			for {
				line, ok := r.readLine()
				if !ok {
					break
				}
				got = append(got, line)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("lines of %q = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The exit prompt reads through the same reader, so a choice piped with the
// mark in front is the choice: a marked k or r fell through to "stop
// everything" before.
func TestPromptTakesAChoiceAfterAByteOrderMark(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("false not on PATH")
	}
	for _, tc := range []struct{ choice, want string }{
		{"k", "Editor keeps running."},
		{"r", "Restarting agent"},
	} {
		t.Run(tc.choice, func(t *testing.T) {
			con, w, out := pipedConsole(t)
			sess, _, _ := agentSessionOn(t, con, "false")
			// One write ahead of the prompt, as PowerShell pipes it: the mark,
			// the choice, then the q that ends the session — read by the
			// launcher console after k, by the next prompt after r.
			if _, err := w.WriteString("\xef\xbb\xbf" + tc.choice + "\nq\n"); err != nil {
				t.Fatalf("write: %v", err)
			}
			select {
			case <-sess.ctx.Done():
			case <-time.After(10 * time.Second):
				t.Fatalf("the session did not end on the q; console:\n%s", out)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("the prompt did not take the %s after the byte order mark (want %q); console:\n%s", tc.choice, tc.want, out)
			}
		})
	}
}

// consoleLoop runs commandLoop on con against a supervisor with a running
// "ue" and a command "hello" (echo hi) until the test ends.
func consoleLoop(t *testing.T, con console) (*supervisor.Supervisor, *syncLog) {
	t.Helper()
	for _, bin := range []string{"sleep", "echo"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	logs := &syncLog{}
	sup := supervisor.New(logs.log)
	sup.AddService(supervisor.ServiceSpec{Name: "ue", Command: "sleep", Args: []string{"20"}, Autostart: true, Restart: "never"})
	sup.AddCommand("hello", supervisor.CommandSpec{Command: "echo", Args: []string{"hi"}})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })
	waitRunning(t, sup, "ue")
	go commandLoop(ctx, con, func() {}, sup, logs.log, "agent")
	return sup, logs
}

// The launcher console's answers to what the test types below.
var consoleAnswers = []struct{ typed, answer string }{
	{"", "Commands: 'status'"},
	{"status", `"name": "ue"`},
	{"c hello", "exit 0\nhi"},
	{"nonsense", "Unknown command: nonsense"},
	{"q", "Stopping …"},
}

// k120: in window mode the launcher log goes to unreagent.log, and the
// launcher console answered through that log — the command list, status, the
// c <name> results, every hint — so the user typing at the console saw none
// of it. The answers go to the console, and still to the log.
func TestConsoleAnswersOnTheConsoleInWindowMode(t *testing.T) {
	fc := newFakeConsole(t) // window mode: the log is a file
	_, logs := consoleLoop(t, fc.con)
	for _, a := range consoleAnswers {
		if a.typed != "" {
			fc.typeLine(t, a.typed)
		}
		fc.waitOutput(t, a.answer, 1)
		if !strings.Contains(logs.String(), a.answer) {
			t.Fatalf("the answer to %q is not in the launcher log:\n%s", a.typed, logs)
		}
	}
}

// The control: when the launcher log is the console (no window mode), every
// answer is on it once, through the log — not a second time beside it.
func TestConsoleAnswersOnceWhenTheLogIsTheConsole(t *testing.T) {
	fc := newFakeConsole(t)
	fc.con.logSeparate = false
	_, logs := consoleLoop(t, fc.con)
	for _, a := range consoleAnswers {
		if a.typed != "" {
			fc.typeLine(t, a.typed)
		}
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(logs.String(), a.answer) {
			if time.Now().After(deadline) {
				t.Fatalf("the answer to %q is not in the launcher log:\n%s", a.typed, logs)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if got := fc.out.String(); got != "" {
		t.Fatalf("with the log on the console, the answers were written beside it as well:\n%s", got)
	}
}

// k126: start and r in the launcher console ignored what StartService and
// RestartService answered, so a start that failed showed only in the
// supervisor's own log line — in window mode a file the user at the console
// does not see. The console names it the way ue_start does, and an unknown
// service as ue_stop would.
func TestConsoleNamesAFailedStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-editor")
	sup := supervisor.New(nil)
	sup.AddService(supervisor.ServiceSpec{Name: "ue", Command: missing, Restart: "never"})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })
	fc := newFakeConsole(t)
	go commandLoop(ctx, fc.con, func() {}, sup, func(string) {}, "")

	const failed = `Error: service "ue" did not start: `
	starts := []string{"start ue", "r ue", "restart ue", "r"}
	for i, cmd := range starts {
		fc.typeLine(t, cmd)
		fc.waitOutput(t, failed, i+1)
	}
	lastErr := statusOf(t, sup, "ue").LastError
	if !strings.Contains(lastErr, missing) {
		t.Fatalf("lastError = %q, want the start error naming %s", lastErr, missing)
	}
	if got := strings.Count(fc.out.String(), failed+lastErr+"\n"); got != len(starts) {
		t.Fatalf("%d of %d failed starts named with the start error %q; console:\n%s", got, len(starts), lastErr, fc.out)
	}
	for i, cmd := range []string{"start nosuch", "stop nosuch"} {
		fc.typeLine(t, cmd)
		fc.waitOutput(t, `Error: unknown service "nosuch"`, i+1)
	}
}

// k151: in window mode the launcher console answered a lifecycle command only
// when it failed. A stop, start or restart that went through left its
// confirmation, the supervisor's own line, in unreagent.log alone, so the user
// who typed it saw nothing; and so did the agent's onExit leave and shutdown
// lines. The console now shows what the service is after the command, and the
// onExit line, while the log keeps its lines. When the log is the console, the
// supervisor's line is the confirmation and nothing is written beside it.
func TestConsoleShowsTheOutcomeInWindowMode(t *testing.T) {
	t.Run("stop, start and restart", func(t *testing.T) {
		fc := newFakeConsole(t) // window mode: the log is a file
		sup, logs := consoleLoop(t, fc.con)
		fc.typeLine(t, "stop ue")
		fc.waitOutput(t, "ue stopped\n", 1)
		fc.typeLine(t, "start ue")
		fc.waitOutput(t, "ue is running (pid ", 1)
		started := statusOf(t, sup, "ue").PID
		fc.typeLine(t, "r ue")
		fc.waitOutput(t, "ue restarted (pid ", 1)
		restarted := statusOf(t, sup, "ue").PID
		if restarted == started {
			t.Fatalf("r ue left pid %d running; the premise is a new process", started)
		}
		for _, want := range []string{
			fmt.Sprintf("ue is running (pid %d)\n", started),
			fmt.Sprintf("ue restarted (pid %d)\n", restarted),
		} {
			if !strings.Contains(fc.out.String(), want) {
				t.Fatalf("the console does not show %q, the pid status reports; console:\n%s", want, fc.out)
			}
		}
		for _, want := range []string{"[ue] stopped (manual)", fmt.Sprintf("[ue] started (pid %d)", started), "[ue] restarted (manual)"} {
			if !strings.Contains(logs.String(), want) {
				t.Fatalf("the launcher log lost the supervisor's line %q:\n%s", want, logs)
			}
		}
	})
	t.Run("the log is the console", func(t *testing.T) {
		fc := newFakeConsole(t)
		fc.con.logSeparate = false
		_, logs := consoleLoop(t, fc.con)
		for _, cmd := range []string{"stop ue", "start ue", "r ue", "status"} {
			fc.typeLine(t, cmd)
		}
		// The loop takes one line at a time: once the status answer is in
		// the log, the commands typed before it are done.
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(logs.String(), `"name": "ue"`) {
			if time.Now().After(deadline) {
				t.Fatalf("the console did not answer status:\n%s", logs)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if got := fc.out.String(); got != "" {
			t.Fatalf("with the log on the console, the outcome was written beside the supervisor's lines:\n%s", got)
		}
		if !strings.Contains(logs.String(), "[ue] restarted (manual)") {
			t.Fatalf("the supervisor's line is missing from the log, which is the console here:\n%s", logs)
		}
	})
	// k188: the onExit line on the console did not say how the agent ended;
	// the log's line for that stayed in the log. On the console the onExit
	// line is now led by the exit prompt's words for it (agentEnded); the log
	// keeps both lines as they were.
	missing := filepath.Join(t.TempDir(), "no-such-agent")
	for _, tc := range []struct {
		name, onExit, command string
		ended, logged, line   string
	}{
		{"leave after a crash", config.OnExitLeave, "false",
			"Agent crashed.", "Agent failed and will not be restarted automatically.",
			"agent.onExit=leave — editor/MCP keep running. Ctrl-C stops the launcher."},
		{"shutdown after exit 0", config.OnExitShutdown, "true",
			"Agent exited.", "Agent exited (exit 0).",
			"agent.onExit=shutdown — stopping everything."},
		{"leave after a failed start", config.OnExitLeave, missing,
			"Agent did not start: ", "Agent failed and will not be restarted automatically.",
			"agent.onExit=leave — editor/MCP keep running. Ctrl-C stops the launcher."},
	} {
		for _, separate := range []bool{true, false} {
			t.Run(fmt.Sprintf("onExit %s, log separate %v", tc.name, separate), func(t *testing.T) {
				if tc.command != missing {
					if _, err := exec.LookPath(tc.command); err != nil {
						t.Skipf("%s not on PATH", tc.command)
					}
				}
				fc := newFakeConsole(t)
				fc.con.logSeparate = separate
				sess, sup, logs := agentSessionWith(t, fc.con, tc.onExit, tc.command)
				deadline := time.Now().Add(10 * time.Second)
				for !strings.Contains(logs.String(), tc.line) {
					if time.Now().After(deadline) {
						t.Fatalf("the launcher log has no %q:\n%s", tc.line, logs)
					}
					time.Sleep(10 * time.Millisecond)
				}
				if tc.onExit == config.OnExitShutdown {
					waitEnded(t, sess, fc)
				}
				// The log keeps its two lines, each on its own.
				lines := strings.Split(logs.String(), "\n")
				for _, want := range []string{tc.logged, tc.line} {
					if !slices.Contains(lines, want) {
						t.Fatalf("the launcher log lost its line %q:\n%s", want, logs)
					}
				}
				if !separate {
					if got := fc.out.String(); got != "" {
						t.Fatalf("with the log on the console, the onExit line was written beside it:\n%s", got)
					}
					return
				}
				ended := tc.ended
				if tc.command == missing {
					ended += statusOf(t, sup, "agent").LastError + "."
				}
				if got, want := fc.out.String(), ended+" "+tc.line+"\n"; got != want {
					t.Fatalf("console = %q, want %q: one line, how the agent ended, then the onExit line", got, want)
				}
			})
		}
	}
}

// k126: an agent that does not start at all — the program is missing — came
// back to the exit prompt as "Agent crashed.", the start error only in the
// launcher log; and the prompt's r, starting it again, did not say why
// nothing happened. The prompt names the start error, each time.
func TestPromptNamesAFailedAgentStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-agent")
	fc := newFakeConsole(t)
	sess, sup, _ := agentSessionOn(t, fc.con, missing)
	fc.waitOutput(t, "> ", 1)
	lastErr := statusOf(t, sup, "agent").LastError
	if !strings.Contains(lastErr, missing) {
		t.Fatalf("lastError = %q, want the start error naming %s", lastErr, missing)
	}
	want := "\nAgent did not start: " + lastErr + "\n"
	fc.waitOutput(t, want, 1)
	fc.typeLine(t, "r")
	fc.waitOutput(t, want, 2)
	fc.typeLine(t, "q")
	waitEnded(t, sess, fc)
	if strings.Contains(fc.out.String(), "Agent crashed.") {
		t.Fatalf("a start that failed is shown as a crash; console:\n%s", fc.out)
	}
}

// k120, k156: newConsole decides the window-mode switch. A launcher whose log
// is a file (window mode's unreagent.log) must answer on the console as well
// as in the log; one whose log is the console must answer once, through the
// log. The loop tests above set logSeparate by hand; this one pins how
// newConsole derives it from the log it is given.
func TestNewConsoleAnswersOnTheConsoleOnlyWhenTheLogIsElsewhere(t *testing.T) {
	t.Run("the log is a file", func(t *testing.T) {
		out := &syncBuffer{}
		logFile, err := os.Create(filepath.Join(t.TempDir(), "unreagent.log"))
		if err != nil {
			t.Fatalf("create log: %v", err)
		}
		t.Cleanup(func() { _ = logFile.Close() })
		var logged []string
		newConsole(strings.NewReader(""), out, logFile).reply(func(l string) { logged = append(logged, l) }, "answer")
		if got := out.String(); got != "answer\n" {
			t.Fatalf("console output = %q, want the answer on the console when the log is a separate file", got)
		}
		if len(logged) != 1 || logged[0] != "answer" {
			t.Fatalf("logged = %q, want the answer in the launcher log too", logged)
		}
	})
	t.Run("the log is the console", func(t *testing.T) {
		out := &syncBuffer{}
		var logged []string
		newConsole(strings.NewReader(""), out, out).reply(func(l string) { logged = append(logged, l) }, "answer")
		if got := out.String(); got != "" {
			t.Fatalf("console output = %q, want nothing written directly: the log is the console and carries the answer", got)
		}
		if len(logged) != 1 || logged[0] != "answer" {
			t.Fatalf("logged = %q, want the answer once, through the log", logged)
		}
	})
}
