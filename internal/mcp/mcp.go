// Package mcp implements a minimal, spec-conformant MCP server over the
// Streamable-HTTP transport — using only the Go standard library.
//
// Only POST with a JSON body is served (no SSE):
//   - POST with a JSON-RPC request      → 200 + JSON-RPC response
//   - POST with a JSON-RPC notification → 202 Accepted, empty body
//   - GET / DELETE / anything else      → 405 Method Not Allowed
//
// Every POST must carry "Content-Type: application/json"; a POST that carries
// an Origin header must carry an allowlisted one (see SetAllowedOrigins).
// Both are browser defences — a regular MCP client sends no Origin at all.
//
// Implemented methods: initialize, notifications/initialized, ping,
// tools/list, tools/call. Sessions are deliberately omitted (stateless).
package mcp

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
)

const defaultProtocolVersion = "2025-06-18"

// supportedProtocolVersions are the MCP revisions this server actually speaks.
// All of them use the Streamable-HTTP transport (POST + JSON body). 2024-11-05
// is deliberately absent: it mandates the HTTP+SSE transport, which this server
// does not implement. A client asking for anything else is answered with
// defaultProtocolVersion and can then decide whether to continue or disconnect.
var supportedProtocolVersions = []string{"2025-06-18", "2025-03-26"}

// maxRequestBody caps an accepted POST body.
const maxRequestBody = 16 << 20

// ToolResult is the result of a tool call.
type ToolResult struct {
	Text    string
	IsError bool
}

// ToolHandler executes a tool call.
type ToolHandler func(args map[string]interface{}) ToolResult

// Tool is a registered MCP tool definition. Description is delivered to the
// agent as context — this is where the tool's "manual" belongs.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     ToolHandler
}

// Server is a minimal MCP server (http.Handler).
type Server struct {
	name    string
	version string
	log     func(string)
	// token, if set, requires an "Authorization: Bearer <token>" header on
	// every POST request. Empty = open (compatible with the prior behavior
	// that targets 127.0.0.1 only).
	token string
	// allowedOrigins, if non-empty, replaces the default loopback allowlist.
	allowedOrigins []string

	mu    sync.RWMutex
	tools []Tool
	index map[string]int
}

// NewServer creates an MCP server.
func NewServer(name, version string, log func(string)) *Server {
	if log == nil {
		log = func(string) {}
	}
	return &Server{name: name, version: version, log: log, index: map[string]int{}}
}

// SetToken enables bearer authentication. An empty string disables it (open
// server, default). Header comparison uses subtle.ConstantTimeCompare to
// thwart timing attacks. Call before serving.
func (s *Server) SetToken(token string) {
	s.token = token
}

// SetAllowedOrigins replaces the built-in Origin allowlist. Entries are matched
// case-insensitively against the complete Origin header ("https://host:port").
// An empty slice restores the default: loopback origins (http/https on
// localhost, 127.0.0.1 or ::1, any port). Requests without an Origin header are
// always accepted — that is what a non-browser MCP client sends. Call before
// serving.
func (s *Server) SetAllowedOrigins(origins []string) {
	s.allowedOrigins = append([]string(nil), origins...)
}

// AddTool registers a tool (safe to call while serving).
func (s *Server) AddTool(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.index[t.Name]; ok {
		s.tools[i] = t
		return
	}
	s.index[t.Name] = len(s.tools)
	s.tools = append(s.tools, t)
}

// --- JSON-RPC types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
)

// nullID is the id of a response to a message whose id could not be
// determined; JSON-RPC 2.0 requires an explicit null there.
var nullID = json.RawMessage("null")

// ServeHTTP serves the MCP endpoint.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// handled below
	default:
		// No SSE/session support, so GET and DELETE are refused as well.
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Browser defence (DNS rebinding / cross-site request forgery). A browser
	// attaches Origin to every cross-site POST, including the "simple
	// requests" that skip the CORS preflight — without this check any web page
	// the user visits could drive the toolset of an open localhost server. A
	// regular MCP client sends no Origin at all; that case stays allowed.
	if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin) {
		s.log("MCP: rejected request from disallowed Origin " + origin)
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	// Demanding application/json takes the request out of the CORS "simple
	// request" set: a cross-site page cannot set that header without a
	// preflight, and the preflight (OPTIONS) is answered with 405 above.
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "unsupported media type: expected application/json", http.StatusUnsupportedMediaType)
		return
	}

	// Optional bearer authentication. When no token is set, the server is
	// open (the default for 127.0.0.1). When a token is set, the credentials
	// MUST match — otherwise 401, so external clients (e.g. Hermes) cannot
	// reach the toolset through an open server. The scheme name is compared
	// case-insensitively (RFC 7235), the token in constant time.
	if s.token != "" {
		if !s.authorized(r.Header.Get("Authorization")) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="unreagent"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			// Distinguishable from a syntax error: truncating the body and
			// reporting "invalid JSON" would send the client hunting for a bug
			// in its own serialization.
			writeErrorStatus(w, http.StatusRequestEntityTooLarge, nil, errInvalidRequest,
				fmt.Sprintf("request body exceeds %d bytes", maxRequestBody))
			return
		}
		writeError(w, nil, errParse, "could not read request body")
		return
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		writeError(w, nil, errInvalidRequest, "empty request")
		return
	}

	// Batch (array) vs. single message.
	if trimmed[0] == '[' {
		s.serveBatch(w, body)
		return
	}
	s.serveSingle(w, body)
}

// serveBatch answers a JSON-RPC batch. Elements are parsed and validated
// independently: one malformed element must not fail the valid requests
// beside it.
func (s *Server) serveBatch(w http.ResponseWriter, body []byte) {
	var batch []json.RawMessage
	if err := json.Unmarshal(body, &batch); err != nil {
		writeError(w, nil, errParse, "invalid JSON")
		return
	}
	if len(batch) == 0 {
		writeError(w, nil, errInvalidRequest, "empty batch")
		return
	}
	responses := make([]rpcResponse, 0, len(batch))
	for _, raw := range batch {
		if resp, ok := s.dispatch(raw); ok {
			responses = append(responses, resp)
		}
	}
	if len(responses) == 0 {
		// Notifications only → no body.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, responses)
}

// serveSingle answers a single JSON-RPC message.
func (s *Server) serveSingle(w http.ResponseWriter, body []byte) {
	resp, ok := s.dispatch(body)
	if !ok {
		// Notification → no body.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	status := http.StatusOK
	if resp.Error != nil && (resp.Error.Code == errParse || resp.Error.Code == errInvalidRequest) {
		// Malformed at the transport level, not a failed method call.
		status = http.StatusBadRequest
	}
	writeJSONStatus(w, status, resp)
}

// dispatch processes one JSON-RPC message. ok=false means notification (no
// response).
func (s *Server) dispatch(raw json.RawMessage) (rpcResponse, bool) {
	req, rerr := parseMessage(raw)
	if rerr != nil {
		return rpcResponse{JSONRPC: "2.0", ID: nullID, Error: rerr}, true
	}

	// Only an ABSENT id makes a message a notification. An explicit
	// "id": null is a (discouraged, but legal) request and must be answered —
	// treating it as a notification leaves the client waiting forever.
	if len(req.ID) == 0 {
		_, _ = s.handle(req.Method, req.Params)
		return rpcResponse{}, false
	}

	result, rerr := s.handle(req.Method, req.Params)
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if rerr != nil {
		resp.Error = rerr
		return resp, true
	}
	if result == nil {
		// Notification-only methods (notifications/*) produce no payload. A
		// response object needs one of result/error, otherwise the client gets
		// a message it cannot interpret.
		result = map[string]interface{}{}
	}
	resp.Result = result
	return resp, true
}

// parseMessage decodes and validates one JSON-RPC 2.0 message.
func parseMessage(raw json.RawMessage) (rpcRequest, *rpcError) {
	if !json.Valid(raw) {
		return rpcRequest{}, &rpcError{Code: errParse, Message: "invalid JSON"}
	}
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcRequest{}, &rpcError{Code: errInvalidRequest, Message: "not a JSON-RPC request object"}
	}
	if req.JSONRPC != "2.0" {
		return rpcRequest{}, &rpcError{Code: errInvalidRequest, Message: `"jsonrpc" must be "2.0"`}
	}
	if req.Method == "" {
		return rpcRequest{}, &rpcError{Code: errInvalidRequest, Message: `"method" is missing`}
	}
	return req, nil
}

func (s *Server) handle(method string, params json.RawMessage) (interface{}, *rpcError) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &p)
		// Never echo a version we do not implement: the client would assume a
		// transport this server does not speak.
		ver := defaultProtocolVersion
		for _, v := range supportedProtocolVersions {
			if p.ProtocolVersion == v {
				ver = v
				break
			}
		}
		return map[string]interface{}{
			"protocolVersion": ver,
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": s.name, "version": s.version},
		}, nil

	case "notifications/initialized", "notifications/cancelled":
		return nil, nil

	case "ping":
		return map[string]interface{}{}, nil

	case "tools/list":
		s.mu.RLock()
		defer s.mu.RUnlock()
		tools := make([]map[string]interface{}, 0, len(s.tools))
		for _, t := range s.tools {
			schema := t.InputSchema
			if schema == nil {
				schema = map[string]interface{}{"type": "object", "additionalProperties": false}
			}
			tools = append(tools, map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": schema,
			})
		}
		return map[string]interface{}{"tools": tools}, nil

	case "tools/call":
		var p struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: errInvalidParams, Message: "invalid params"}
		}
		s.mu.RLock()
		idx, ok := s.index[p.Name]
		var handler ToolHandler
		if ok {
			handler = s.tools[idx].Handler
		}
		s.mu.RUnlock()
		if !ok || handler == nil {
			return nil, &rpcError{Code: errInvalidParams, Message: "unknown tool: " + p.Name}
		}
		if p.Arguments == nil {
			p.Arguments = map[string]interface{}{}
		}
		res := s.callTool(p.Name, handler, p.Arguments)
		return map[string]interface{}{
			"content": []map[string]interface{}{{"type": "text", "text": res.Text}},
			"isError": res.IsError,
		}, nil

	default:
		return nil, &rpcError{Code: errMethodNotFound, Message: "unknown method: " + method}
	}
}

// callTool runs a tool handler and turns a panic into a tool error. Without
// this the panic reaches the client as a transport error and the Go stack
// trace lands in the agent's TUI (stderr is not redirected in window mode).
func (s *Server) callTool(name string, handler ToolHandler, args map[string]interface{}) (res ToolResult) {
	defer func() {
		if rec := recover(); rec != nil {
			s.log(fmt.Sprintf("MCP: tool %q panicked: %v\n%s", name, rec, debug.Stack()))
			res = ToolResult{Text: fmt.Sprintf("tool %q panicked: %v", name, rec), IsError: true}
		}
	}()
	return handler(args)
}

// authorized checks an Authorization header against the configured token.
func (s *Server) authorized(hdr string) bool {
	scheme, credentials, ok := cutSpace(hdr)
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(credentials), []byte(s.token)) == 1
}

// cutSpace splits "<scheme> <credentials>" at the first space and trims the
// extra spaces RFC 7235 permits between the two.
func cutSpace(s string) (string, string, bool) {
	i := strings.IndexByte(s, ' ')
	if i < 0 {
		return "", "", false
	}
	return s[:i], strings.TrimSpace(s[i+1:]), true
}

// originAllowed reports whether a browser Origin may drive this server.
func (s *Server) originAllowed(origin string) bool {
	if len(s.allowedOrigins) > 0 {
		for _, allowed := range s.allowedOrigins {
			if strings.EqualFold(strings.TrimSpace(allowed), origin) {
				return true
			}
		}
		return false
	}
	return isLoopbackOrigin(origin)
}

// isLoopbackOrigin is the default allowlist: http/https on a loopback host,
// any port. Everything else — including the literal "null" of a sandboxed
// document — is refused.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// isJSONContentType accepts application/json with optional parameters.
func isJSONContentType(v string) bool {
	if v == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(v)
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	writeErrorStatus(w, http.StatusBadRequest, id, code, msg)
}

func writeErrorStatus(w http.ResponseWriter, status int, id json.RawMessage, code int, msg string) {
	writeJSONStatus(w, status, rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}
