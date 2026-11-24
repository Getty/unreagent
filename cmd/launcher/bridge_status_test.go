package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// k205: a TCP connect on the editor's port was logged as "is reachable",
// although the toolkit's listener accepts seconds before it serves HTTP (on
// Windows: ~12 s on a warm start, indefinitely while a modal dialog blocks
// the game thread). The editor endpoint now reads GET <url>/mcp/status; only a
// 200 is "ready", a 404 falls back to the bare-connect result, and anything
// else keeps the watch going.

// shortenStatusTimeout sets the read timeout of one status GET for one test.
func shortenStatusTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := endpointStatusTimeout
	endpointStatusTimeout = d
	t.Cleanup(func() { endpointStatusTimeout = old })
}

// watchEditor runs waitForEndpoint on the editor endpoint to its return.
func watchEditor(t *testing.T, rawURL string, notRunning func() string) string {
	t.Helper()
	var log syncLog
	done := make(chan struct{})
	go func() {
		defer close(done)
		waitForEndpoint(context.Background(), "ue", editorEndpoint, rawURL, notRunning, log.log)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the watch is still running 10s in (window %s):\n%s", endpointWaitWindow, log.String())
	}
	return log.String()
}

// statusBody is a trimmed answer of the toolkit's HandleStatus (ue-5.8).
const statusBody = `{"status":"running","port":3000,"version":"1.0.0","toolCount":43,` +
	`"tools":[{"name":"spawn_actor","description":"Spawn an actor"}],` +
	`"projectName":"Demo","projectDirectory":"C:/Demo/","engineVersion":"5.7.4-51494982+++UE5+Release-5.7"}`

func TestWaitForEndpointReadsToolkitStatus(t *testing.T) {
	shortenEndpointWait(t, 5*time.Second, 10*time.Millisecond)
	shortenStatusTimeout(t, time.Second)
	for _, c := range []struct {
		name, body, path, want string
	}{
		{"tool count", statusBody, "", " is ready — the toolkit lists 43 tools (project Demo, engine 5.7.4-51494982+++UE5+Release-5.7)."},
		{"trailing slash", statusBody, "/", " is ready — the toolkit lists 43 tools (project Demo, engine 5.7.4-51494982+++UE5+Release-5.7)."},
		{"count only", `{"toolCount":0}`, "", " is ready — the toolkit lists 0 tools."},
		{"no count", `{"status":"running"}`, "", " is ready — the toolkit answers /mcp/status, tool count unknown."},
		{"count of the wrong type", `{"toolCount":"many","projectName":"Demo"}`, "", " is ready — the toolkit answers /mcp/status, tool count unknown."},
		{"control characters", `{"toolCount":2,"projectName":"a\nWARN b","engineVersion":"5.8"}`, "", " is ready — the toolkit lists 2 tools (engine 5.8)."},
		// U+202E (right-to-left override) is a format character, not a
		// control character: it would reorder the rest of the line.
		{"format characters", `{"toolCount":2,"projectName":"a\u202eb","engineVersion":"5.8\u200b"}`, "", " is ready — the toolkit lists 2 tools."},
	} {
		t.Run(c.name, func(t *testing.T) {
			var paths []string
			var mu sync.Mutex
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.Method+" "+r.URL.Path)
				mu.Unlock()
				if r.URL.Path != "/mcp/status" {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			out := watchEditor(t, srv.URL+c.path, nil)
			want := "MCP server 'ue': in-editor server " + srv.URL + c.path + c.want
			if out != want {
				t.Fatalf("logged:\n%s\nwant:\n%s", out, want)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(paths) != 1 || paths[0] != "GET /mcp/status" {
				t.Fatalf("requests %q, want one GET /mcp/status", paths)
			}
		})
	}
}

// A 404 on the status route is another listener or an older toolkit: the
// connect is reported as before, saying the count is unknown, with no retry.
func TestWaitForEndpointStatusRouteMissing(t *testing.T) {
	shortenEndpointWait(t, 5*time.Second, 10*time.Millisecond)
	shortenStatusTimeout(t, time.Second)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	out := watchEditor(t, srv.URL+"/mcp", nil)
	want := "MCP server 'ue': in-editor server " + srv.URL + "/mcp is reachable, but /mcp/status answers 404 — another server on that port, or a toolkit without the status route; tool count unknown."
	if out != want {
		t.Fatalf("logged:\n%s\nwant:\n%s", out, want)
	}
	if n := requests.Load(); n != 1 {
		t.Fatalf("%d requests, want 1: a 404 ends the watch", n)
	}
}

// A 200 on the status route is not the toolkit unless it carries a JSON
// object, and a redirect is not the toolkit either: both end the watch like a
// 404, without claiming the toolkit answered and without the five-minute wait.
func TestWaitForEndpointStatusNotTheToolkit(t *testing.T) {
	shortenEndpointWait(t, 5*time.Second, 10*time.Millisecond)
	shortenStatusTimeout(t, time.Second)
	for _, c := range []struct {
		name string
		h    http.HandlerFunc
		want string
	}{
		{"html page", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html>ok</html>"))
		}, " is reachable and answers /mcp/status with 200, but not with the toolkit's JSON — probably another server on that port; tool count unknown."},
		{"json array", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[{"toolCount":43}]`))
		}, " is reachable and answers /mcp/status with 200, but not with the toolkit's JSON — probably another server on that port; tool count unknown."},
		{"empty body", func(w http.ResponseWriter, r *http.Request) {},
			" is reachable and answers /mcp/status with 200, but not with the toolkit's JSON — probably another server on that port; tool count unknown."},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/login", http.StatusFound)
		}, " is reachable, but /mcp/status redirects (HTTP 302) — not the toolkit's answer; tool count unknown."},
		{"permanent redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://example.invalid/mcp/status", http.StatusMovedPermanently)
		}, " is reachable, but /mcp/status redirects (HTTP 301) — not the toolkit's answer; tool count unknown."},
	} {
		t.Run(c.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				c.h(w, r)
			}))
			defer srv.Close()
			out := watchEditor(t, srv.URL, nil)
			if want := "MCP server 'ue': in-editor server " + srv.URL + c.want; out != want {
				t.Fatalf("logged:\n%s\nwant:\n%s", out, want)
			}
			if n := requests.Load(); n != 1 {
				t.Fatalf("%d requests, want 1: the answer ends the watch", n)
			}
		})
	}
}

// The measured startup: the port accepts, the status GET times out several
// times, then answers. The timeouts are "not ready yet" and must neither end
// the watch nor warn.
func TestWaitForEndpointWaitsForLateStatus(t *testing.T) {
	shortenEndpointWait(t, 5*time.Second, 10*time.Millisecond)
	shortenStatusTimeout(t, 50*time.Millisecond)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= 3 {
			<-r.Context().Done() // hold until the client gives up
			return
		}
		_, _ = w.Write([]byte(`{"toolCount":43}`))
	}))
	defer srv.Close()
	out := watchEditor(t, srv.URL, nil)
	want := "MCP server 'ue': in-editor server " + srv.URL + " is ready — the toolkit lists 43 tools."
	if out != want {
		t.Fatalf("logged:\n%s\nwant:\n%s", out, want)
	}
	if n := requests.Load(); n != 4 {
		t.Fatalf("%d requests, want 4 (three timed out, the fourth answered)", n)
	}
}

// heldListener accepts connections and never answers them: the editor's port
// while the game thread is blocked by a modal dialog.
func heldListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return ln.Addr().String()
}

// A port that accepts but never answers the status route is neither
// "reachable" nor "not reachable": the warning says the port is open and the
// toolkit did not answer. What the launcher knows about the editor still
// replaces the guess at a blocked game thread.
func TestWaitForEndpointPortOpenStatusNeverAnswers(t *testing.T) {
	shortenEndpointWait(t, 300*time.Millisecond, 20*time.Millisecond)
	shortenStatusTimeout(t, 50*time.Millisecond)
	u := "http://" + heldListener(t)
	prefix := "WARN MCP server 'ue': in-editor server " + u + ": port open, but the toolkit did not answer /mcp/status within 300ms — "

	out := watchEditor(t, u, func() string { return "" })
	want := prefix + "is the editor's game thread blocked, e.g. by a modal dialog such as \"Restore Packages\" after a crash, or is it still loading? No editor tool can run until it answers."
	if out != want {
		t.Fatalf("logged:\n%s\nwant:\n%s", out, want)
	}

	out = watchEditor(t, u, func() string { return "the launcher's editor is not running" })
	if want := prefix + "the launcher's editor is not running"; out != want {
		t.Fatalf("logged:\n%s\nwant:\n%s", out, want)
	}
}

// The warning says "port open" when any try connected, not only the last
// one: the editor's port accepts once, the status GET is held, and the
// listener then closes, so every later dial is refused (the editor went away,
// or the window ended inside the last dial).
func TestWaitForEndpointPortOpenOnceThenClosed(t *testing.T) {
	shortenEndpointWait(t, 300*time.Millisecond, 20*time.Millisecond)
	shortenStatusTimeout(t, 50*time.Millisecond)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	held := make(chan net.Conn, 1)
	go func() {
		// First the bare connect, then the status GET; the listener closes
		// before the GET times out, so no later dial can connect.
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c.Close()
		c, err = ln.Accept()
		_ = ln.Close()
		if err == nil {
			held <- c
		}
	}()
	t.Cleanup(func() {
		select {
		case c := <-held:
			_ = c.Close()
		default:
		}
	})
	u := "http://" + ln.Addr().String()
	out := watchEditor(t, u, nil)
	want := "WARN MCP server 'ue': in-editor server " + u + ": port open, but the toolkit did not answer /mcp/status within 300ms — "
	if !strings.HasPrefix(out, want) || strings.Contains(out, "\n") {
		t.Fatalf("logged:\n%s\nwant one line starting:\n%s", out, want)
	}
}

// Another status code is "not ready yet" too; the warning names the last one.
func TestWaitForEndpointStatusErrorKeepsWaiting(t *testing.T) {
	shortenEndpointWait(t, 300*time.Millisecond, 20*time.Millisecond)
	shortenStatusTimeout(t, 50*time.Millisecond)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	out := watchEditor(t, srv.URL, nil)
	want := "WARN MCP server 'ue': in-editor server " + srv.URL + ": port open, but the toolkit did not answer /mcp/status within 300ms (last answer: HTTP 503) — "
	if !strings.HasPrefix(out, want) || strings.Contains(out, "\n") {
		t.Fatalf("logged:\n%s\nwant one line starting:\n%s", out, want)
	}
	if n := requests.Load(); n < 2 {
		t.Fatalf("%d requests: a 503 must not end the watch", n)
	}
}
