package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// --- helpers ---

type reqOpt func(*http.Request)

// header sets a request header; an empty value removes it entirely.
func header(key, value string) reqOpt {
	return func(r *http.Request) {
		if value == "" {
			r.Header.Del(key)
			return
		}
		r.Header.Set(key, value)
	}
}

// post sends a POST with the defaults a non-browser MCP client uses: JSON
// content type, no Origin, no Authorization.
func post(t *testing.T, s *Server, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// spy is a tool handler that records how it was called.
type spy struct {
	mu    sync.Mutex
	calls int
	args  map[string]interface{}
}

func (s *spy) handler(args map[string]interface{}) ToolResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.args = args
	return ToolResult{Text: "ok"}
}

func (s *spy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// newTestServer builds a server with an "echo" tool driven by the returned spy.
func newTestServer(t *testing.T) (*Server, *spy) {
	t.Helper()
	sp := &spy{}
	s := NewServer("unreagent-test", "0.0.0", nil)
	s.AddTool(Tool{
		Name:        "echo",
		Description: "test tool",
		Handler:     sp.handler,
	})
	return s, sp
}

// --- transport-level guards ---

func TestNonPostMethodsRejected(t *testing.T) {
	s, _ := newTestServer(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut, http.MethodOptions, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "/mcp", nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
			}
			if got := w.Header().Get("Allow"); got != "POST" {
				t.Fatalf("Allow = %q, want POST", got)
			}
		})
	}
}

func TestOriginValidation(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		wantStatus int
	}{
		{"absent (regular MCP client)", "", http.StatusOK},
		{"foreign https", "https://evil.example", http.StatusForbidden},
		{"foreign http", "http://evil.example:8765", http.StatusForbidden},
		{"loopback name", "http://localhost:3000", http.StatusOK},
		{"loopback ip", "http://127.0.0.1:8765", http.StatusOK},
		{"loopback ipv6", "http://[::1]:8765", http.StatusOK},
		{"loopback https", "https://localhost", http.StatusOK},
		{"loopback uppercase", "HTTP://LOCALHOST:5173", http.StatusOK},
		{"sandboxed document", "null", http.StatusForbidden},
		{"suffix lookalike", "http://127.0.0.1.evil.example", http.StatusForbidden},
		{"prefix lookalike", "http://localhost.evil.example", http.StatusForbidden},
		{"non-http scheme", "file://", http.StatusForbidden},
		{"garbage", "://", http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, sp := newTestServer(t)
			w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`,
				header("Origin", tc.origin))
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			wantCalls := 0
			if tc.wantStatus == http.StatusOK {
				wantCalls = 1
			}
			if sp.count() != wantCalls {
				t.Fatalf("tool calls = %d, want %d", sp.count(), wantCalls)
			}
		})
	}
}

// TestCORSSimpleRequestRejected reproduces the exact shape a malicious page can
// send without a CORS preflight: text/plain body from a foreign origin.
func TestCORSSimpleRequestRejected(t *testing.T) {
	s, sp := newTestServer(t)
	w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`,
		header("Origin", "https://evil.example"),
		header("Content-Type", "text/plain;charset=UTF-8"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if sp.count() != 0 {
		t.Fatalf("tool ran %d times, want 0 — side effect happened", sp.count())
	}
}

func TestOriginAllowlistOverride(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		wantStatus int
	}{
		{"allowlisted", "https://ide.example.com", http.StatusOK},
		{"allowlisted other case", "https://IDE.example.com", http.StatusOK},
		{"loopback no longer implied", "http://localhost:3000", http.StatusForbidden},
		{"foreign", "https://evil.example", http.StatusForbidden},
		{"absent", "", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			s.SetAllowedOrigins([]string{"https://ide.example.com"})
			w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, header("Origin", tc.origin))
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
		})
	}
}

func TestOriginAllowlistResetToDefault(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetAllowedOrigins([]string{"https://ide.example.com"})
	s.SetAllowedOrigins(nil)
	if w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, header("Origin", "http://localhost:9")); w.Code != http.StatusOK {
		t.Fatalf("loopback origin status = %d, want 200", w.Code)
	}
	if w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, header("Origin", "https://evil.example")); w.Code != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d, want 403", w.Code)
	}
}

func TestContentTypeRequired(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		wantStatus  int
	}{
		{"json", "application/json", http.StatusOK},
		{"json with charset", "application/json; charset=utf-8", http.StatusOK},
		{"json uppercase", "APPLICATION/JSON", http.StatusOK},
		{"absent", "", http.StatusUnsupportedMediaType},
		{"text", "text/plain", http.StatusUnsupportedMediaType},
		{"form", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"multipart", "multipart/form-data; boundary=x", http.StatusUnsupportedMediaType},
		{"malformed", "application/json; charset", http.StatusUnsupportedMediaType},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, sp := newTestServer(t)
			w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`,
				header("Content-Type", tc.contentType))
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantStatus != http.StatusOK && sp.count() != 0 {
				t.Fatalf("tool ran %d times despite %d", sp.count(), w.Code)
			}
		})
	}
}
