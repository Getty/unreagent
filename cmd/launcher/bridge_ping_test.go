package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// k266: Epic's Unreal MCP server accepts TCP on its port seconds before it
// answers (k221: accept at 32.5 s, the first answer at 44.1 s), and Claude
// Code marks an http server that fails at the agent's start as failed for
// good after about 6 s. So "up" for unreal-mcp is an HTTP 200 carrying the
// result of a session-less JSON-RPC ping POSTed to unreal.mcp's URL; an open
// port, a GET (405 there), an error object or any other answer is not.

// unrealMCPServer is a fake of the editor's Unreal MCP server at /mcp. Until
// ready is closed it answers every request with notReady (0 = it holds the
// request past the probe's timeout, as the editor does before its first game
// thread tick); then a POSTed ping gets answer, everything else 405.
type unrealMCPServer struct {
	srv      *httptest.Server
	ready    chan struct{}
	notReady int
	answer   func(w http.ResponseWriter, id json.RawMessage)

	mu       sync.Mutex
	requests []string // "METHOD path session=<Mcp-Session-Id> type=<Content-Type> body"
}

func newUnrealMCPServer(t *testing.T, notReady int, answer func(w http.ResponseWriter, id json.RawMessage)) *unrealMCPServer {
	t.Helper()
	s := &unrealMCPServer{ready: make(chan struct{}), notReady: notReady, answer: answer}
	stop := make(chan struct{})
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, r.Method+" "+r.URL.Path+" session="+r.Header.Get("Mcp-Session-Id")+
			" type="+r.Header.Get("Content-Type")+" "+string(body))
		s.mu.Unlock()
		select {
		case <-s.ready:
		default:
			if s.notReady == 0 {
				select {
				case <-stop:
				case <-time.After(2 * time.Second):
				}
				return
			}
			w.WriteHeader(s.notReady)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/mcp" || json.Unmarshal(body, &req) != nil || req.Method != "ping" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.answer(w, req.ID)
	}))
	t.Cleanup(func() { close(stop); s.srv.Close() })
	return s
}

// url is the server's address as unreal.mcp hands it out.
func (s *unrealMCPServer) url() string { return s.srv.URL + "/mcp" }

func (s *unrealMCPServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// pingResult answers a ping as the editor did on k221.
func pingResult(w http.ResponseWriter, id json.RawMessage) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(id) + `,"result":{}}`))
}

// watchUnrealMCP runs waitForEndpoint on the Unreal MCP endpoint to its
// return and gives back its log and its result.
func watchUnrealMCP(t *testing.T, rawURL string, notRunning func() string) (string, bool) {
	t.Helper()
	var log syncLog
	var up atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		up.Store(waitForEndpoint(context.Background(), config.UnrealMCPServerName, unrealMCPEndpoint, rawURL, notRunning, log.log))
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the watch is still running 10s in (window %s):\n%s", endpointWaitWindow, log.String())
	}
	return log.String(), up.Load()
}

func TestWaitForEndpointPingsUnrealMCP(t *testing.T) {
	shortenEndpointWait(t, 5*time.Second, 10*time.Millisecond)
	shortenStatusTimeout(t, 200*time.Millisecond)
	for _, c := range []struct {
		name     string
		notReady int
		answer   func(w http.ResponseWriter, id json.RawMessage)
	}{
		{"JSON answer", 0, pingResult},
		// The streamable HTTP transport may answer a POST as an event stream.
		{"event-stream answer", http.StatusServiceUnavailable, func(w http.ResponseWriter, id json.RawMessage) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":" + string(id) + ",\"result\":{}}\n\n"))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newUnrealMCPServer(t, c.notReady, c.answer)
			// The port is open from the start; the first tries must not count.
			go func() { time.Sleep(500 * time.Millisecond); close(s.ready) }()
			out, up := watchUnrealMCP(t, s.url(), nil)
			want := "MCP server 'unreal-mcp': Unreal MCP server " + s.url() + " is ready — it answers ping."
			if out != want || !up {
				t.Fatalf("got %v and log:\n%s\nwant true and:\n%s", up, out, want)
			}
			reqs := s.seen()
			if len(reqs) < 2 {
				t.Fatalf("want several tries before the server was ready, got %q", reqs)
			}
			for _, r := range reqs {
				if !strings.HasPrefix(r, "POST /mcp session= type=application/json {") || !strings.Contains(r, `"method":"ping"`) || !strings.Contains(r, `"jsonrpc":"2.0"`) {
					t.Errorf("want a session-less JSON-RPC ping POSTed to /mcp, got %q", r)
				}
			}
		})
	}
}

// Every answer that is not a ping's result keeps the watch going; at the
// window's end there is one warning, naming the last answer.
func TestWaitForEndpointUnrealMCPNeedsAPingResult(t *testing.T) {
	shortenEndpointWait(t, 400*time.Millisecond, 10*time.Millisecond)
	shortenStatusTimeout(t, 100*time.Millisecond)
	ready := func(answer func(w http.ResponseWriter, id json.RawMessage)) func(*unrealMCPServer) {
		return func(s *unrealMCPServer) { s.answer = answer; close(s.ready) }
	}
	for _, c := range []struct {
		name     string
		notReady int
		setup    func(*unrealMCPServer) // nil: never ready
		last     string                 // "" = no answer within the timeout
	}{
		{"no answer", 0, nil, ""},
		{"GET only", http.StatusMethodNotAllowed, nil, " (last answer: HTTP 405)"},
		{"not found", http.StatusNotFound, nil, " (last answer: HTTP 404)"},
		{"redirect", http.StatusFound, nil, " (last answer: HTTP 302)"},
		{"error object", 0, ready(func(w http.ResponseWriter, id json.RawMessage) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(id) + `,"error":{"code":-32601,"message":"no"}}`))
		}), " (last answer: HTTP 200 without a ping result)"},
		{"another id", 0, ready(func(w http.ResponseWriter, id json.RawMessage) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"other","result":{}}`))
		}), " (last answer: HTTP 200 without a ping result)"},
		// A numeric id that is not the ping's (pingID 1).
		{"numeric id of another request", 0, ready(func(w http.ResponseWriter, id json.RawMessage) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`))
		}), " (last answer: HTTP 200 without a ping result)"},
		{"result and error", 0, ready(func(w http.ResponseWriter, id json.RawMessage) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(id) + `,"result":{},"error":{"code":-32603,"message":"no"}}`))
		}), " (last answer: HTTP 200 without a ping result)"},
		{"no jsonrpc", 0, ready(func(w http.ResponseWriter, id json.RawMessage) {
			_, _ = w.Write([]byte(`{"id":` + string(id) + `,"result":{}}`))
		}), " (last answer: HTTP 200 without a ping result)"},
		{"null result", 0, ready(func(w http.ResponseWriter, id json.RawMessage) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(id) + `,"result":null}`))
		}), " (last answer: HTTP 200 without a ping result)"},
		{"HTML", 0, ready(func(w http.ResponseWriter, id json.RawMessage) {
			_, _ = w.Write([]byte(`<html>hello</html>`))
		}), " (last answer: HTTP 200 without a ping result)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newUnrealMCPServer(t, c.notReady, nil)
			if c.setup != nil {
				c.setup(s)
			}
			out, up := watchUnrealMCP(t, s.url(), nil)
			want := "WARN MCP server 'unreal-mcp': Unreal MCP server " + s.url() +
				": port open, but it did not answer a ping within 400ms" + c.last + " — "
			if up || strings.Contains(out, "\n") || !strings.HasPrefix(out, want) {
				t.Fatalf("got %v and log:\n%s\nwant false and one line starting:\n%s", up, out, want)
			}
			if !strings.Contains(out, "game thread") {
				t.Errorf("the warning names no cause:\n%s", out)
			}
		})
	}
}

// A closed port is "not reachable", with the launcher's knowledge of the
// editor in place of the guess when it has any (k253 for the toolkit).
func TestWaitForEndpointUnrealMCPNotReachable(t *testing.T) {
	shortenEndpointWait(t, 300*time.Millisecond, 20*time.Millisecond)
	u := "http://" + freeLoopbackAddr(t) + "/mcp"
	for _, c := range []struct {
		name       string
		notRunning func() string
		want       string
	}{
		{"editor state unknown", nil, "ModelContextProtocol"},
		{"editor down", func() string { return "the launcher's editor is not running" }, "the launcher's editor is not running"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, up := watchUnrealMCP(t, u, c.notRunning)
			want := "WARN MCP server 'unreal-mcp': Unreal MCP server " + u + " not reachable after 300ms — "
			if up || strings.Contains(out, "\n") || !strings.HasPrefix(out, want) || !strings.Contains(out, c.want) {
				t.Fatalf("got %v and log:\n%s\nwant false and one line starting %q holding %q", up, out, want, c.want)
			}
		})
	}
}

// The agent's first start waits for the ping (k266): Claude Code would mark
// an Unreal MCP that does not answer at its start as failed for good. At the
// window's end it starts anyway, with exactly one warning; a shutdown during
// the wait starts nothing.
func TestHoldAgentForUnrealMCP(t *testing.T) {
	shortenStatusTimeout(t, 100*time.Millisecond)

	holdEnded := func(ctx context.Context, rawURL string, ended <-chan supervisor.Exit, notRunning func() string) (*syncLog, *atomic.Int32, chan struct{}) {
		var log syncLog
		var starts atomic.Int32
		done := make(chan struct{})
		go func() {
			defer close(done)
			holdAgentForUnrealMCP(ctx, rawURL, ended, notRunning, log.log, func() { starts.Add(1) })
		}()
		return &log, &starts, done
	}
	hold := func(ctx context.Context, rawURL string, notRunning func() string) (*syncLog, *atomic.Int32, chan struct{}) {
		return holdEnded(ctx, rawURL, nil, notRunning)
	}
	finished := func(t *testing.T, done chan struct{}, log *syncLog) {
		t.Helper()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("the hold is still running 10s in:\n%s", log.String())
		}
	}

	t.Run("starts once the server answers", func(t *testing.T) {
		shortenEndpointWait(t, 10*time.Second, 10*time.Millisecond)
		s := newUnrealMCPServer(t, 0, pingResult)
		log, starts, done := hold(context.Background(), s.url(), nil)
		time.Sleep(400 * time.Millisecond)
		if n := starts.Load(); n != 0 {
			t.Fatalf("the agent was started %d time(s) while the port was open but the server did not answer:\n%s", n, log.String())
		}
		close(s.ready)
		finished(t, done, log)
		if n := starts.Load(); n != 1 {
			t.Fatalf("the agent was started %d times, want once:\n%s", n, log.String())
		}
		if out := log.String(); strings.Contains(out, "WARN") || !strings.Contains(out, "is ready — it answers ping.") {
			t.Fatalf("want the ready line and no warning:\n%s", out)
		}
	})

	t.Run("starts anyway at the window's end", func(t *testing.T) {
		shortenEndpointWait(t, 300*time.Millisecond, 10*time.Millisecond)
		s := newUnrealMCPServer(t, http.StatusServiceUnavailable, pingResult)
		log, starts, done := hold(context.Background(), s.url(), func() string { return "" })
		finished(t, done, log)
		if n := starts.Load(); n != 1 {
			t.Fatalf("the agent was started %d times, want once:\n%s", n, log.String())
		}
		out := log.String()
		if n := strings.Count(out, "WARN"); n != 1 {
			t.Fatalf("want exactly one warning, got %d:\n%s", n, out)
		}
		for _, want := range []string{"WARN MCP server 'unreal-mcp': Unreal MCP server " + s.url() + ": port open, but it did not answer a ping within 300ms (last answer: HTTP 503)",
			"the agent starts now without it"} {
			if !strings.Contains(out, want) {
				t.Errorf("the warning lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("the editor's state replaces the guess", func(t *testing.T) {
		shortenEndpointWait(t, 300*time.Millisecond, 20*time.Millisecond)
		u := "http://" + freeLoopbackAddr(t) + "/mcp"
		log, starts, done := hold(context.Background(), u, func() string { return "the launcher's editor is not running" })
		finished(t, done, log)
		out := log.String()
		if starts.Load() != 1 || strings.Count(out, "WARN") != 1 ||
			!strings.Contains(out, "the launcher's editor is not running") || !strings.Contains(out, "the agent starts now without it") {
			t.Fatalf("want one start and one warning naming the editor's state and the start:\n%s", out)
		}
	})

	// Main agent's decision on the k266 review: an editor the supervisor
	// gives up on (k69, k251) cannot answer any more, and its give-up line
	// tells the agent to fix the build, so the hold ends at once.
	for _, c := range []struct {
		name string
		exit supervisor.Exit
		how  string
	}{
		{"the editor gives up during the wait", supervisor.Exit{StartFailed: true}, "the editor failed and is not restarted"},
		{"the editor exits 0 during the wait", supervisor.Exit{Success: true}, "the editor exited and is not restarted"},
	} {
		t.Run(c.name, func(t *testing.T) {
			shortenEndpointWait(t, 10*time.Second, 10*time.Millisecond)
			s := newUnrealMCPServer(t, http.StatusServiceUnavailable, pingResult)
			ended := make(chan supervisor.Exit, 1)
			log, starts, done := holdEnded(context.Background(), s.url(), ended, nil)
			time.Sleep(100 * time.Millisecond)
			if n := starts.Load(); n != 0 {
				t.Fatalf("the agent was started %d time(s) before the editor ended:\n%s", n, log.String())
			}
			ended <- c.exit
			finished(t, done, log)
			if n := starts.Load(); n != 1 {
				t.Fatalf("the agent was started %d times, want once:\n%s", n, log.String())
			}
			out := log.String()
			want := "WARN MCP server 'unreal-mcp': Unreal MCP server " + s.url() + " will not answer: " + c.how + " — the agent starts now without it"
			if strings.Contains(out, "\n") || !strings.HasPrefix(out, want) {
				t.Fatalf("want exactly one line starting\n%s\ngot:\n%s", want, out)
			}
		})
	}

	t.Run("a shutdown during the wait starts nothing", func(t *testing.T) {
		shortenEndpointWait(t, 10*time.Second, 10*time.Millisecond)
		s := newUnrealMCPServer(t, http.StatusServiceUnavailable, pingResult)
		ctx, cancel := context.WithCancel(context.Background())
		log, starts, done := hold(ctx, s.url(), nil)
		time.Sleep(100 * time.Millisecond)
		cancel()
		finished(t, done, log)
		if n := starts.Load(); n != 0 {
			t.Fatalf("the agent was started %d time(s) after the shutdown:\n%s", n, log.String())
		}
		if out := log.String(); out != "" {
			t.Fatalf("a shutdown must end the hold silently, got:\n%s", out)
		}
	})
}

// The hold applies only where the agent's first start would race the
// editor's server: the switch on, an agent that gets the unreal-mcp entry
// (Claude integration), and an editor the launcher starts itself. Anything else starts the agent as before.
func TestAgentHeldForUnrealMCP(t *testing.T) {
	for _, c := range []struct {
		name                                     string
		unrealMCP, agent, manualStart, claudeInt bool
		want                                     bool
	}{
		{"switch on", true, true, false, true, true},
		{"switch off", false, true, false, true, false},
		{"manual start", true, true, true, true, false},
		{"no agent", true, false, false, true, false},
		// No Claude integration: the agent gets no unreal-mcp entry.
		{"no claude integration", true, true, false, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.MCP.Enabled = true
			cfg.Unreal.MCP.Enabled = c.unrealMCP
			cfg.Agent.Enabled = c.agent
			cfg.Unreal.ManualStart = c.manualStart
			cfg.Agent.ClaudeIntegration = c.claudeInt
			if got := agentHeldForUnrealMCP(cfg); got != c.want {
				t.Fatalf("agentHeldForUnrealMCP = %v, want %v", got, c.want)
			}
		})
	}
}

// Without a hold, prepareMCPBridges watches unreal-mcp like any endpoint, by
// the ping; with one, the hold does the watching, so it is not watched twice
// (one ready line, one warning at most).
func TestPrepareMCPBridgesWatchesUnrealMCP(t *testing.T) {
	shortenEndpointWait(t, 5*time.Second, 10*time.Millisecond)
	shortenStatusTimeout(t, 200*time.Millisecond)
	for _, held := range []bool{false, true} {
		name := "not held"
		if held {
			name = "held"
		}
		t.Run(name, func(t *testing.T) {
			s := newUnrealMCPServer(t, 0, pingResult)
			close(s.ready)
			cfg := &config.Config{}
			cfg.MCP.Enabled = true
			cfg.Unreal.MCP.Enabled = true
			cfg.Unreal.MCP.Port = mustPort(t, s.srv.Listener.Addr().String())
			cfg.Agent.Enabled = held
			cfg.Agent.ClaudeIntegration = true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var log syncLog
			prepared(t, ctx, cfg, &log)
			if !held {
				waitForLine(t, &log, "MCP server 'unreal-mcp': Unreal MCP server "+s.url()+" is ready — it answers ping.", 5*time.Second)
				return
			}
			time.Sleep(200 * time.Millisecond)
			if out := log.String(); out != "" || len(s.seen()) != 0 {
				t.Fatalf("a held agent's server was watched by the bridge preparation too: %q\n%s", s.seen(), out)
			}
		})
	}
}

// mustPort is the port of a host:port.
func mustPort(t *testing.T, hostPort string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(hostPort)
	n, err2 := strconv.Atoi(p)
	if err != nil || err2 != nil {
		t.Fatalf("no port in %q", hostPort)
	}
	return n
}
