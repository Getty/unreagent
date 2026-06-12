// Package mcp implementiert einen minimalen, spec-konformen MCP-Server über den
// Streamable-HTTP-Transport — nur mit der Go-Standardbibliothek.
//
// Es wird ausschließlich POST→application/json bedient (kein SSE):
//   - POST mit JSON-RPC-Request  → 200 + JSON-RPC-Response
//   - POST mit JSON-RPC-Notification → 202 Accepted, leerer Body
//   - GET                         → 405 Method Not Allowed
//
// Implementierte Methoden: initialize, notifications/initialized, ping,
// tools/list, tools/call. Sessions werden bewusst weggelassen (stateless).
package mcp

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

const defaultProtocolVersion = "2025-06-18"

// ToolResult ist das Ergebnis eines Tool-Aufrufs.
type ToolResult struct {
	Text    string
	IsError bool
}

// ToolHandler führt einen Tool-Aufruf aus.
type ToolHandler func(args map[string]interface{}) ToolResult

// Tool ist eine registrierte MCP-Tool-Definition. Description wird dem Agenten
// als Kontext geliefert — hier gehört die "Bedienungsanleitung" des Tools rein.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     ToolHandler
}

// Server ist ein minimaler MCP-Server (http.Handler).
type Server struct {
	name    string
	version string
	log     func(string)
	// token, if set, requires an "Authorization: Bearer <token>" header on
	// every POST request. Empty = open (compatible with the prior behavior
	// that targets 127.0.0.1 only).
	token string

	mu    sync.RWMutex
	tools []Tool
	index map[string]int
}

// NewServer erzeugt einen MCP-Server.
func NewServer(name, version string, log func(string)) *Server {
	if log == nil {
		log = func(string) {}
	}
	return &Server{name: name, version: version, log: log, index: map[string]int{}}
}

// SetToken enables bearer authentication. An empty string disables it (open
// server, default). Header comparison uses subtle.ConstantTimeCompare to
// thwart timing attacks.
func (s *Server) SetToken(token string) {
	s.token = token
}

// AddTool registriert ein Tool (vor dem Start aufrufen).
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

// --- JSON-RPC-Typen ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
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

// ServeHTTP bedient den MCP-Endpoint.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodDelete:
		// Kein SSE/Session-Support.
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	case http.MethodPost:
		// weiter unten
	default:
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Optional bearer authentication. When no token is set, the server is
	// open (the default for 127.0.0.1). When a token is set, the header MUST
	// match exactly — otherwise 401, so external clients (e.g. Hermes) cannot
	// reach the toolset through an open server.
	if s.token != "" {
		const prefix = "Bearer "
		hdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(hdr, prefix) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="unreagent"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		got := hdr[len(prefix):]
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="unreagent"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeError(w, nil, errParse, "Body konnte nicht gelesen werden")
		return
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		writeError(w, nil, errInvalidRequest, "leerer Request")
		return
	}

	// Batch (Array) vs. Einzelnachricht.
	if trimmed[0] == '[' {
		var batch []rpcRequest
		if err := json.Unmarshal(body, &batch); err != nil {
			writeError(w, nil, errParse, "ungültiges JSON")
			return
		}
		var responses []rpcResponse
		for _, req := range batch {
			if resp, ok := s.dispatch(req); ok {
				responses = append(responses, resp)
			}
		}
		if len(responses) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, responses)
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, nil, errParse, "ungültiges JSON")
		return
	}
	resp, ok := s.dispatch(req)
	if !ok {
		// Notification → kein Body.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, resp)
}

// dispatch verarbeitet eine Nachricht. ok=false bedeutet Notification (keine
// Antwort).
func (s *Server) dispatch(req rpcRequest) (rpcResponse, bool) {
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	result, rerr := s.handle(req.Method, req.Params)

	if isNotification {
		return rpcResponse{}, false
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if rerr != nil {
		resp.Error = rerr
	} else {
		resp.Result = result
	}
	return resp, true
}

func (s *Server) handle(method string, params json.RawMessage) (interface{}, *rpcError) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &p)
		ver := p.ProtocolVersion
		if ver == "" {
			ver = defaultProtocolVersion
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
			return nil, &rpcError{Code: errInvalidParams, Message: "ungültige params"}
		}
		s.mu.RLock()
		idx, ok := s.index[p.Name]
		var handler ToolHandler
		if ok {
			handler = s.tools[idx].Handler
		}
		s.mu.RUnlock()
		if !ok || handler == nil {
			return nil, &rpcError{Code: errInvalidParams, Message: "unbekanntes Tool: " + p.Name}
		}
		if p.Arguments == nil {
			p.Arguments = map[string]interface{}{}
		}
		res := handler(p.Arguments)
		return map[string]interface{}{
			"content": []map[string]interface{}{{"type": "text", "text": res.Text}},
			"isError": res.IsError,
		}, nil

	default:
		return nil, &rpcError{Code: errMethodNotFound, Message: "unbekannte Methode: " + method}
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}
