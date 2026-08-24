package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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

// object decodes a JSON object while keeping the raw member values, so tests
// can tell "member absent" from "member present and null".
func object(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode object: %v (body %q)", err, string(b))
	}
	return m
}

func array(t *testing.T, b []byte) []json.RawMessage {
	t.Helper()
	var a []json.RawMessage
	if err := json.Unmarshal(b, &a); err != nil {
		t.Fatalf("decode array: %v (body %q)", err, string(b))
	}
	return a
}

// errorCode returns the JSON-RPC error code of a response object, or 0 when
// the response carries no error member.
func errorCode(t *testing.T, obj map[string]json.RawMessage) int {
	t.Helper()
	raw, ok := obj["error"]
	if !ok {
		return 0
	}
	var e rpcError
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("decode error member: %v", err)
	}
	return e.Code
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

func (s *spy) lastArgs() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.args
}

// logSink collects log lines emitted by the server.
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) log(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, s)
}

func (l *logSink) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
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

func TestBearerAuth(t *testing.T) {
	const token = "s3cret-token"
	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"canonical scheme", "Bearer " + token, http.StatusOK},
		{"lowercase scheme", "bearer " + token, http.StatusOK},
		{"uppercase scheme", "BEARER " + token, http.StatusOK},
		{"mixed scheme", "BeArEr " + token, http.StatusOK},
		{"extra spaces", "Bearer   " + token, http.StatusOK},
		{"absent", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"token prefix", "Bearer " + token[:4], http.StatusUnauthorized},
		{"token with suffix", "Bearer " + token + "x", http.StatusUnauthorized},
		{"other scheme", "Basic " + token, http.StatusUnauthorized},
		{"no scheme", token, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, sp := newTestServer(t)
			s.SetToken(token)
			w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`,
				header("Authorization", tc.authHeader))
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusUnauthorized {
				if got := w.Header().Get("WWW-Authenticate"); got == "" {
					t.Fatal("missing WWW-Authenticate header on 401")
				}
				if sp.count() != 0 {
					t.Fatalf("tool ran %d times despite 401", sp.count())
				}
			}
		})
	}
}

func TestNoTokenLeavesServerOpen(t *testing.T) {
	s, _ := newTestServer(t)
	for _, hdr := range []string{"", "Bearer whatever", "garbage"} {
		w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, header("Authorization", hdr))
		if w.Code != http.StatusOK {
			t.Fatalf("Authorization %q → status %d, want 200", hdr, w.Code)
		}
	}
}

// --- JSON-RPC framing ---

func TestRequestNotificationAndNullID(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantID     string // raw id member of the response, "" = expect no body
		wantResult bool
	}{
		{
			name:       "request with numeric id",
			body:       `{"jsonrpc":"2.0","id":1,"method":"ping"}`,
			wantStatus: http.StatusOK, wantID: "1", wantResult: true,
		},
		{
			name:       "request with string id",
			body:       `{"jsonrpc":"2.0","id":"abc","method":"ping"}`,
			wantStatus: http.StatusOK, wantID: `"abc"`, wantResult: true,
		},
		{
			// Only an ABSENT id makes a message a notification.
			name:       "explicit null id is a request",
			body:       `{"jsonrpc":"2.0","id":null,"method":"ping"}`,
			wantStatus: http.StatusOK, wantID: "null", wantResult: true,
		},
		{
			name:       "absent id is a notification",
			body:       `{"jsonrpc":"2.0","method":"ping"}`,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "notification method without id",
			body:       `{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			wantStatus: http.StatusAccepted,
		},
		{
			// A response object must carry result or error, never neither.
			name:       "notification method with id",
			body:       `{"jsonrpc":"2.0","id":7,"method":"notifications/initialized"}`,
			wantStatus: http.StatusOK, wantID: "7", wantResult: true,
		},
		{
			name:       "cancelled notification with id",
			body:       `{"jsonrpc":"2.0","id":8,"method":"notifications/cancelled"}`,
			wantStatus: http.StatusOK, wantID: "8", wantResult: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			w := post(t, s, tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantStatus == http.StatusAccepted {
				if body := strings.TrimSpace(w.Body.String()); body != "" {
					t.Fatalf("notification answered with body %q", body)
				}
				return
			}
			obj := object(t, w.Body.Bytes())
			if got := string(obj["id"]); got != tc.wantID {
				t.Fatalf("id = %s, want %s", got, tc.wantID)
			}
			_, hasResult := obj["result"]
			_, hasError := obj["error"]
			if hasResult == hasError {
				t.Fatalf("response must carry exactly one of result/error, got result=%v error=%v (body %q)",
					hasResult, hasError, w.Body.String())
			}
			if hasResult != tc.wantResult {
				t.Fatalf("hasResult = %v, want %v", hasResult, tc.wantResult)
			}
		})
	}
}

func TestJSONRPCVersionValidated(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   int
	}{
		{"valid", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, http.StatusOK, 0},
		{"version 1.0", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, http.StatusBadRequest, errInvalidRequest},
		{"version absent", `{"id":1,"method":"ping"}`, http.StatusBadRequest, errInvalidRequest},
		{"version numeric", `{"jsonrpc":2.0,"id":1,"method":"ping"}`, http.StatusBadRequest, errInvalidRequest},
		{"method absent", `{"jsonrpc":"2.0","id":1}`, http.StatusBadRequest, errInvalidRequest},
		{"method wrong type", `{"jsonrpc":"2.0","id":1,"method":42}`, http.StatusBadRequest, errInvalidRequest},
		{"not an object", `42`, http.StatusBadRequest, errInvalidRequest},
		{"json string", `"hello"`, http.StatusBadRequest, errInvalidRequest},
		{"invalid json", `{"jsonrpc":`, http.StatusBadRequest, errParse},
		{"empty body", ``, http.StatusBadRequest, errInvalidRequest},
		{"whitespace body", "  \n\t ", http.StatusBadRequest, errInvalidRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			w := post(t, s, tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			obj := object(t, w.Body.Bytes())
			if got := errorCode(t, obj); got != tc.wantCode {
				t.Fatalf("error code = %d, want %d (body %q)", got, tc.wantCode, w.Body.String())
			}
			if tc.wantCode != 0 {
				if _, ok := obj["id"]; !ok {
					t.Fatal("error response must carry an id member (null when unknown)")
				}
			}
		})
	}
}

func TestBatchHandling(t *testing.T) {
	t.Run("one malformed element does not fail the others", func(t *testing.T) {
		s, sp := newTestServer(t)
		body := `[
			{"jsonrpc":"2.0","id":1,"method":"ping"},
			"not an object",
			{"jsonrpc":"1.0","id":2,"method":"ping"},
			{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo"}}
		]`
		w := post(t, s, body)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		items := array(t, w.Body.Bytes())
		if len(items) != 4 {
			t.Fatalf("got %d responses, want 4 (body %q)", len(items), w.Body.String())
		}
		want := []struct {
			id   string
			code int
		}{
			{"1", 0},
			{"null", errInvalidRequest},
			{"null", errInvalidRequest},
			{"3", 0},
		}
		for i, tc := range want {
			obj := object(t, items[i])
			if got := string(obj["id"]); got != tc.id {
				t.Errorf("element %d: id = %s, want %s", i, got, tc.id)
			}
			if got := errorCode(t, obj); got != tc.code {
				t.Errorf("element %d: error code = %d, want %d", i, got, tc.code)
			}
		}
		if sp.count() != 1 {
			t.Fatalf("tool calls = %d, want 1 — the valid request beside a bad element must still run", sp.count())
		}
	})

	t.Run("empty batch", func(t *testing.T) {
		s, _ := newTestServer(t)
		w := post(t, s, `[]`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
		if got := errorCode(t, object(t, w.Body.Bytes())); got != errInvalidRequest {
			t.Fatalf("error code = %d, want %d", got, errInvalidRequest)
		}
	})

	t.Run("notifications only", func(t *testing.T) {
		s, _ := newTestServer(t)
		w := post(t, s, `[{"jsonrpc":"2.0","method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"}]`)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202 (body %q)", w.Code, w.Body.String())
		}
		if body := strings.TrimSpace(w.Body.String()); body != "" {
			t.Fatalf("body = %q, want empty", body)
		}
	})

	t.Run("mixed notification and request", func(t *testing.T) {
		s, _ := newTestServer(t)
		w := post(t, s, `[{"jsonrpc":"2.0","method":"ping"},{"jsonrpc":"2.0","id":5,"method":"ping"}]`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		items := array(t, w.Body.Bytes())
		if len(items) != 1 {
			t.Fatalf("got %d responses, want 1 (only the request)", len(items))
		}
		if got := string(object(t, items[0])["id"]); got != "5" {
			t.Fatalf("id = %s, want 5", got)
		}
	})

	t.Run("batch with null id element", func(t *testing.T) {
		s, _ := newTestServer(t)
		w := post(t, s, `[{"jsonrpc":"2.0","id":null,"method":"ping"}]`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		items := array(t, w.Body.Bytes())
		if len(items) != 1 {
			t.Fatalf("got %d responses, want 1", len(items))
		}
		obj := object(t, items[0])
		if got := string(obj["id"]); got != "null" {
			t.Fatalf("id = %s, want null", got)
		}
		if _, ok := obj["result"]; !ok {
			t.Fatalf("missing result member (body %q)", w.Body.String())
		}
	})

	t.Run("invalid json array", func(t *testing.T) {
		s, _ := newTestServer(t)
		w := post(t, s, `[{"jsonrpc":"2.0","id":1,"method":"ping"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
		if got := errorCode(t, object(t, w.Body.Bytes())); got != errParse {
			t.Fatalf("error code = %d, want %d", got, errParse)
		}
	})
}

func TestBodyTooLargeIsDistinguishable(t *testing.T) {
	s, _ := newTestServer(t)
	prefix := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`
	suffix := `"}}`
	pad := strings.Repeat("a", maxRequestBody+1-len(prefix)-len(suffix))
	w := post(t, s, prefix+pad+suffix)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (body %q)", w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
	if got := errorCode(t, object(t, w.Body.Bytes())); got != errInvalidRequest {
		t.Fatalf("error code = %d, want %d — an oversized body must not look like a syntax error", got, errInvalidRequest)
	}
}

// --- methods ---

func TestInitializeProtocolNegotiation(t *testing.T) {
	tests := []struct {
		name     string
		params   string
		wantVer  string
		wantName string
	}{
		{"current", `{"protocolVersion":"2025-06-18"}`, "2025-06-18", "unreagent-test"},
		{"previous streamable http", `{"protocolVersion":"2025-03-26"}`, "2025-03-26", "unreagent-test"},
		{"http+sse era", `{"protocolVersion":"2024-11-05"}`, defaultProtocolVersion, "unreagent-test"},
		{"unknown future", `{"protocolVersion":"2099-01-01"}`, defaultProtocolVersion, "unreagent-test"},
		{"garbage", `{"protocolVersion":"lolwut"}`, defaultProtocolVersion, "unreagent-test"},
		{"empty", `{}`, defaultProtocolVersion, "unreagent-test"},
		{"wrong type", `{"protocolVersion":42}`, defaultProtocolVersion, "unreagent-test"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":`+tc.params+`}`)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
			}
			var resp struct {
				Result struct {
					ProtocolVersion string `json:"protocolVersion"`
					ServerInfo      struct {
						Name string `json:"name"`
					} `json:"serverInfo"`
					Capabilities map[string]interface{} `json:"capabilities"`
				} `json:"result"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Result.ProtocolVersion != tc.wantVer {
				t.Fatalf("protocolVersion = %q, want %q", resp.Result.ProtocolVersion, tc.wantVer)
			}
			if resp.Result.ServerInfo.Name != tc.wantName {
				t.Fatalf("serverInfo.name = %q, want %q", resp.Result.ServerInfo.Name, tc.wantName)
			}
			if _, ok := resp.Result.Capabilities["tools"]; !ok {
				t.Fatal("capabilities.tools missing")
			}
		})
	}
}

func TestInitializeNeverAnnouncesUnsupportedVersion(t *testing.T) {
	// Guards the invariant behind the negotiation table: whatever a client
	// asks for, the answer is a version this server actually implements.
	s, _ := newTestServer(t)
	for _, ask := range []string{"2024-11-05", "2025-03-26", "2025-06-18", "1999-01-01", ""} {
		w := post(t, s, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`, ask))
		var resp struct {
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		found := false
		for _, v := range supportedProtocolVersions {
			if v == resp.Result.ProtocolVersion {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("asked %q, server announced unsupported %q", ask, resp.Result.ProtocolVersion)
		}
	}
}

func TestUnknownMethod(t *testing.T) {
	s, _ := newTestServer(t)
	w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := errorCode(t, object(t, w.Body.Bytes())); got != errMethodNotFound {
		t.Fatalf("error code = %d, want %d", got, errMethodNotFound)
	}
}

func TestToolsList(t *testing.T) {
	s, _ := newTestServer(t)
	s.AddTool(Tool{
		Name:        "with_schema",
		Description: "has an explicit schema",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Handler:     func(map[string]interface{}) ToolResult { return ToolResult{} },
	})
	w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name        string                 `json:"name"`
				Description string                 `json:"description"`
				InputSchema map[string]interface{} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Result.Tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(resp.Result.Tools))
	}
	byName := map[string]map[string]interface{}{}
	for _, tool := range resp.Result.Tools {
		byName[tool.Name] = tool.InputSchema
	}
	echo, ok := byName["echo"]
	if !ok {
		t.Fatal("echo tool missing")
	}
	// A tool without an InputSchema gets the empty-object default.
	if echo["type"] != "object" || echo["additionalProperties"] != false {
		t.Fatalf("default schema = %v, want empty object schema", echo)
	}
	if _, ok := byName["with_schema"]["properties"]; !ok {
		t.Fatalf("explicit schema not passed through: %v", byName["with_schema"])
	}
}

func TestToolsCallArguments(t *testing.T) {
	tests := []struct {
		name      string
		params    string
		wantCode  int
		wantCalls int
		wantArgs  map[string]interface{}
	}{
		{
			name:   "object arguments",
			params: `{"name":"echo","arguments":{"a":"b","n":1}}`,
			// JSON numbers decode to float64.
			wantCalls: 1, wantArgs: map[string]interface{}{"a": "b", "n": float64(1)},
		},
		{
			name:   "arguments absent",
			params: `{"name":"echo"}`,
			// Absent arguments must reach the handler as an empty map, never nil.
			wantCalls: 1, wantArgs: map[string]interface{}{},
		},
		{
			name:      "arguments null",
			params:    `{"name":"echo","arguments":null}`,
			wantCalls: 1, wantArgs: map[string]interface{}{},
		},
		{
			name:     "arguments not an object",
			params:   `{"name":"echo","arguments":5}`,
			wantCode: errInvalidParams,
		},
		{
			name:     "arguments array",
			params:   `{"name":"echo","arguments":["a"]}`,
			wantCode: errInvalidParams,
		},
		{
			name:     "unknown tool",
			params:   `{"name":"nope"}`,
			wantCode: errInvalidParams,
		},
		{
			name:     "name missing",
			params:   `{"arguments":{}}`,
			wantCode: errInvalidParams,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, sp := newTestServer(t)
			w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+tc.params+`}`)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
			}
			obj := object(t, w.Body.Bytes())
			if got := errorCode(t, obj); got != tc.wantCode {
				t.Fatalf("error code = %d, want %d (body %q)", got, tc.wantCode, w.Body.String())
			}
			if sp.count() != tc.wantCalls {
				t.Fatalf("tool calls = %d, want %d", sp.count(), tc.wantCalls)
			}
			if tc.wantCalls == 0 {
				return
			}
			args := sp.lastArgs()
			if args == nil {
				t.Fatal("handler received nil arguments map")
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("arguments = %v, want %v", args, tc.wantArgs)
			}
			for k, want := range tc.wantArgs {
				if args[k] != want {
					t.Fatalf("arguments[%q] = %#v, want %#v", k, args[k], want)
				}
			}
		})
	}
}

func TestToolsCallWithoutParams(t *testing.T) {
	s, sp := newTestServer(t)
	w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := errorCode(t, object(t, w.Body.Bytes())); got != errInvalidParams {
		t.Fatalf("error code = %d, want %d", got, errInvalidParams)
	}
	if sp.count() != 0 {
		t.Fatalf("tool calls = %d, want 0", sp.count())
	}
}

func TestToolResultShape(t *testing.T) {
	s := NewServer("unreagent-test", "0.0.0", nil)
	s.AddTool(Tool{Name: "boom", Handler: func(map[string]interface{}) ToolResult {
		return ToolResult{Text: "it failed", IsError: true}
	}})
	w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom"}}`)
	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Result.Content) != 1 || resp.Result.Content[0].Type != "text" || resp.Result.Content[0].Text != "it failed" {
		t.Fatalf("content = %+v", resp.Result.Content)
	}
	if !resp.Result.IsError {
		t.Fatal("isError = false, want true")
	}
}

func TestToolPanicBecomesToolError(t *testing.T) {
	sink := &logSink{}
	s := NewServer("unreagent-test", "0.0.0", sink.log)
	s.AddTool(Tool{Name: "panicky", Handler: func(map[string]interface{}) ToolResult {
		panic("nil map write")
	}})
	s.AddTool(Tool{Name: "healthy", Handler: func(map[string]interface{}) ToolResult {
		return ToolResult{Text: "still here"}
	}})

	w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"panicky"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a panic must not become a transport error", w.Code)
	}
	obj := object(t, w.Body.Bytes())
	if got := errorCode(t, obj); got != 0 {
		t.Fatalf("error code = %d, want a tool result", got)
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(obj["result"], &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.IsError {
		t.Fatal("isError = false, want true")
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "nil map write") {
		t.Fatalf("content = %+v, want the panic value", result.Content)
	}
	if strings.Contains(result.Content[0].Text, "goroutine ") {
		t.Fatal("the Go stack trace must stay in the log, not go to the client")
	}
	if !strings.Contains(sink.joined(), "panicked") {
		t.Fatalf("panic not logged: %q", sink.joined())
	}

	// The server keeps serving afterwards.
	w = post(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"healthy"}}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "still here") {
		t.Fatalf("server unusable after panic: %d %q", w.Code, w.Body.String())
	}
}

// TestHandlerRunsWithoutLock pins the invariant that the read lock is released
// before the tool handler runs: a handler registering a tool would otherwise
// deadlock against its own server.
func TestHandlerRunsWithoutLock(t *testing.T) {
	s := NewServer("unreagent-test", "0.0.0", nil)
	s.AddTool(Tool{Name: "register", Handler: func(map[string]interface{}) ToolResult {
		s.AddTool(Tool{Name: "late", Handler: func(map[string]interface{}) ToolResult {
			return ToolResult{Text: "late"}
		}})
		return ToolResult{Text: "registered"}
	}})

	done := make(chan int, 1)
	go func() {
		w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"register"}}`)
		done <- w.Code
	}()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: the tool handler ran while the server held its lock")
	}

	w := post(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"late"}}`)
	if !strings.Contains(w.Body.String(), "late") {
		t.Fatalf("tool registered from a handler not callable: %q", w.Body.String())
	}
}

func TestConcurrentRequests(t *testing.T) {
	s, _ := newTestServer(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"echo","arguments":{"i":%d}}}`, j, i)
				if w := post(t, s, body); w.Code != http.StatusOK {
					t.Errorf("status = %d", w.Code)
					return
				}
				if w := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); w.Code != http.StatusOK {
					t.Errorf("status = %d", w.Code)
					return
				}
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 20; j++ {
			s.AddTool(Tool{Name: fmt.Sprintf("dyn%d", j), Handler: func(map[string]interface{}) ToolResult {
				return ToolResult{Text: "dyn"}
			}})
		}
	}()
	wg.Wait()
}
