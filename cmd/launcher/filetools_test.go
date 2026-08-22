// Regression tests for karr ticket #28: registerFileTools' mutating tools
// (write_file, edit_file) must never destroy data on a misspelled argument
// name. getString(args, key, "") cannot tell "key absent" from "key present
// but empty", so a typo'd argument used to be silently read as the empty
// string — write_file truncated the target file to 0 bytes and reported
// success; edit_file deleted every occurrence of old_string and reported
// success. These tests drive the tools the way an MCP client does, through
// mcp.Server's JSON-RPC tools/call and tools/list, not by calling the
// handler closures directly.
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/conflict-industries/unreagent/internal/mcp"
)

// --- test scaffolding -------------------------------------------------

func newFileToolsServer(t *testing.T, readOnly bool) (*mcp.Server, string) {
	t.Helper()
	root := t.TempDir()
	srv := mcp.NewServer("filetools-test", "0", nil)
	registerFileTools(srv, root, readOnly)
	return srv, root
}

type toolCallResult struct {
	Text    string
	IsError bool
}

// callTool invokes a registered tool through the server's JSON-RPC
// tools/call, exactly as an MCP client would.
func callTool(t *testing.T, srv *mcp.Server, name string, args map[string]interface{}) toolCallResult {
	t.Helper()
	reqBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call %q: HTTP %d: %s", name, rec.Code, rec.Body.String())
	}

	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal tools/call %q response: %v\nbody: %s", name, err, rec.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("tools/call %q returned a JSON-RPC error: %s", name, resp.Error.Message)
	}
	result := toolCallResult{IsError: resp.Result.IsError}
	if len(resp.Result.Content) > 0 {
		result.Text = resp.Result.Content[0].Text
	}
	return result
}

// listTools drives tools/list and returns each registered tool's raw
// inputSchema, keyed by name.
func listTools(t *testing.T, srv *mcp.Server) map[string]map[string]interface{} {
	t.Helper()
	reqBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list: HTTP %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Result struct {
			Tools []struct {
				Name        string                 `json:"name"`
				InputSchema map[string]interface{} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal tools/list response: %v\nbody: %s", err, rec.Body.String())
	}
	out := map[string]map[string]interface{}{}
	for _, tl := range resp.Result.Tools {
		out[tl.Name] = tl.InputSchema
	}
	return out
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// --- write_file -----------------------------------------------------------

// TestWriteFileMissingContentKeyLeavesFileUntouched pins the first ticket
// #28 regression: a misspelled "content" key (here "contents") must be
// reported as a missing argument and must NOT truncate the target file.
func TestWriteFileMissingContentKeyLeavesFileUntouched(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	target := filepath.Join(root, "keep.txt")
	const original = "original content, must survive\n"
	mustWriteFile(t, target, original)

	res := callTool(t, srv, "write_file", map[string]interface{}{
		"path":     "keep.txt",
		"contents": "clobbered", // typo: "content" misspelled
	})

	if !res.IsError {
		t.Errorf("write_file with misspelled 'content' key: IsError = false, want true (text: %q)", res.Text)
	}
	if res.Text != "Fehler: 'content' fehlt" {
		t.Errorf("write_file with misspelled 'content' key: text = %q, want %q", res.Text, "Fehler: 'content' fehlt")
	}
	if got := mustReadFile(t, target); got != original {
		t.Errorf("write_file with misspelled 'content' key TRUNCATED THE FILE: got %q, want unchanged %q", got, original)
	}
}

// TestWriteFileEmptyContentEmptiesFile pins the legitimate case that must
// keep working once the presence check lands: an explicit empty string is a
// deliberate "empty this file" instruction, not a missing argument.
func TestWriteFileEmptyContentEmptiesFile(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	target := filepath.Join(root, "keep.txt")
	mustWriteFile(t, target, "will be emptied")

	res := callTool(t, srv, "write_file", map[string]interface{}{
		"path":    "keep.txt",
		"content": "",
	})

	if res.IsError {
		t.Fatalf("write_file with content:\"\" returned an error: %q", res.Text)
	}
	if got := mustReadFile(t, target); got != "" {
		t.Errorf("write_file with content:\"\" left file non-empty: %q", got)
	}
}

// TestWriteFileCreatesMissingParentDirs pins the existing, correct behavior
// of write_file for a path whose parent directories don't exist yet.
func TestWriteFileCreatesMissingParentDirs(t *testing.T) {
	srv, root := newFileToolsServer(t, false)

	res := callTool(t, srv, "write_file", map[string]interface{}{
		"path":    "sub/dir/new.txt",
		"content": "hello",
	})

	if res.IsError {
		t.Fatalf("write_file into new subdirectory returned an error: %q", res.Text)
	}
	if got := mustReadFile(t, filepath.Join(root, "sub", "dir", "new.txt")); got != "hello" {
		t.Errorf("write_file into new subdirectory: content = %q, want %q", got, "hello")
	}
}

// --- edit_file --------------------------------------------------------------

// TestEditFileMissingNewStringLeavesFileUntouched pins the second ticket #28
// regression: a misspelled "new_string" key must be reported as missing and
// must NOT delete every occurrence of old_string from the file.
func TestEditFileMissingNewStringLeavesFileUntouched(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	target := filepath.Join(root, "a.txt")
	const original = "foo bar foo"
	mustWriteFile(t, target, original)

	res := callTool(t, srv, "edit_file", map[string]interface{}{
		"path":       "a.txt",
		"old_string": "foo",
		"new_str":    "baz", // typo: "new_string" misspelled
	})

	if !res.IsError {
		t.Errorf("edit_file with misspelled 'new_string' key: IsError = false, want true (text: %q)", res.Text)
	}
	if res.Text != "Fehler: 'new_string' fehlt" {
		t.Errorf("edit_file with misspelled 'new_string' key: text = %q, want %q", res.Text, "Fehler: 'new_string' fehlt")
	}
	if got := mustReadFile(t, target); got != original {
		t.Errorf("edit_file with misspelled 'new_string' key DELETED old_string OCCURRENCES: got %q, want unchanged %q", got, original)
	}
}

// TestEditFileEmptyNewStringDeletesAllOccurrences pins the legitimate case:
// an explicit empty new_string is a deliberate deletion, not a missing
// argument, and must keep working.
func TestEditFileEmptyNewStringDeletesAllOccurrences(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	target := filepath.Join(root, "a.txt")
	mustWriteFile(t, target, "foo bar foo baz foo")

	res := callTool(t, srv, "edit_file", map[string]interface{}{
		"path":       "a.txt",
		"old_string": "foo",
		"new_string": "",
	})

	if res.IsError {
		t.Fatalf("edit_file with new_string:\"\" returned an error: %q", res.Text)
	}
	want := " bar  baz "
	if got := mustReadFile(t, target); got != want {
		t.Errorf("edit_file with new_string:\"\": content = %q, want %q", got, want)
	}
}

// TestEditFileMissingPathReturnsError pins the third asymmetry from ticket
// #28: edit_file, unlike read_file and write_file, never checked for an
// absent path and silently resolved it to the project root instead.
func TestEditFileMissingPathReturnsError(t *testing.T) {
	srv, _ := newFileToolsServer(t, false)

	res := callTool(t, srv, "edit_file", map[string]interface{}{
		"old_string": "foo",
		"new_string": "bar",
	})

	if !res.IsError {
		t.Errorf("edit_file with no 'path' key: IsError = false, want true (text: %q)", res.Text)
	}
	if res.Text != "Fehler: 'path' fehlt" {
		t.Errorf("edit_file with no 'path' key: text = %q, want %q", res.Text, "Fehler: 'path' fehlt")
	}
}

// TestEditFileOldStringNotFoundLeavesFileUntouched pins already-correct
// behavior: an old_string absent from the file is an error, not a silent
// no-op.
func TestEditFileOldStringNotFoundLeavesFileUntouched(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	target := filepath.Join(root, "a.txt")
	const original = "hello world"
	mustWriteFile(t, target, original)

	res := callTool(t, srv, "edit_file", map[string]interface{}{
		"path":       "a.txt",
		"old_string": "notfound",
		"new_string": "x",
	})

	if !res.IsError {
		t.Errorf("edit_file with absent old_string: IsError = false, want true (text: %q)", res.Text)
	}
	if res.Text != "Fehler: 'old_string' nicht gefunden" {
		t.Errorf("edit_file with absent old_string: text = %q, want %q", res.Text, "Fehler: 'old_string' nicht gefunden")
	}
	if got := mustReadFile(t, target); got != original {
		t.Errorf("edit_file with absent old_string modified the file: got %q, want unchanged %q", got, original)
	}
}

// TestEditFileReplacesAllOccurrences pins edit_file's defining behavior:
// every occurrence of old_string is replaced, not just the first.
func TestEditFileReplacesAllOccurrences(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	target := filepath.Join(root, "a.txt")
	mustWriteFile(t, target, "foo foo foo")

	res := callTool(t, srv, "edit_file", map[string]interface{}{
		"path":       "a.txt",
		"old_string": "foo",
		"new_string": "bar",
	})

	if res.IsError {
		t.Fatalf("edit_file replace-all returned an error: %q", res.Text)
	}
	want := "3 Vorkommen ersetzt in a.txt"
	if res.Text != want {
		t.Errorf("edit_file replace-all: text = %q, want %q", res.Text, want)
	}
	if got := mustReadFile(t, target); got != "bar bar bar" {
		t.Errorf("edit_file replace-all: content = %q, want %q", got, "bar bar bar")
	}
}

// --- root confinement, readOnly wiring, schema -----------------------------

// TestWriteFileRejectsPathEscapeAndCreatesNothingOutsideRoot pins root
// confinement for the one file tool that could otherwise write outside it.
func TestWriteFileRejectsPathEscapeAndCreatesNothingOutsideRoot(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	escaped := filepath.Join(filepath.Dir(root), "evil.txt")
	_ = os.Remove(escaped) // paranoia: don't false-pass on a stale leftover

	res := callTool(t, srv, "write_file", map[string]interface{}{
		"path":    "../evil.txt",
		"content": "malicious",
	})

	if !res.IsError {
		t.Errorf("write_file with '..' escape: IsError = false, want true (text: %q)", res.Text)
	}
	want := "Fehler: Pfad außerhalb des erlaubten Roots"
	if res.Text != want {
		t.Errorf("write_file with '..' escape: text = %q, want %q", res.Text, want)
	}
	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Errorf("write_file with '..' escape created a file outside the root: %s", escaped)
	}
}

// TestReadOnlyRegistersOnlyReadTools pins that readOnly:true must not expose
// write_file or edit_file at all — not just refuse their calls.
func TestReadOnlyRegistersOnlyReadTools(t *testing.T) {
	srv, _ := newFileToolsServer(t, true)
	tools := listTools(t, srv)

	if _, ok := tools["read_file"]; !ok {
		t.Errorf("readOnly server: read_file not registered")
	}
	if _, ok := tools["list_dir"]; !ok {
		t.Errorf("readOnly server: list_dir not registered")
	}
	if _, ok := tools["write_file"]; ok {
		t.Errorf("readOnly server: write_file must not be registered")
	}
	if _, ok := tools["edit_file"]; ok {
		t.Errorf("readOnly server: edit_file must not be registered")
	}
}

// TestFileToolSchemasRequiredParamsMatchHandlers pins ticket #28's third
// finding: a tool's advertised inputSchema.required must match what its
// handler actually treats as mandatory. Calling pathSchema() directly proved
// nothing about the registered tools — list_dir feeds the same helper as
// read_file but its handler defaults an absent path to the project root
// (main.go's resolve(getString(args, "path", "."))), so advertising "path"
// as required there is a real drift between schema and handler, not just a
// helper property. This goes through tools/list, the way a schema-validating
// MCP client would, for all four file tools.
func TestFileToolSchemasRequiredParamsMatchHandlers(t *testing.T) {
	srv, _ := newFileToolsServer(t, false)
	tools := listTools(t, srv)

	// requiredParams decodes inputSchema.required the way a real client
	// would: over JSON, so as []interface{}, not the []string a Go caller
	// could get away with by reading pathSchema()'s return value directly.
	requiredParams := func(t *testing.T, toolName string) []string {
		t.Helper()
		schema, ok := tools[toolName]
		if !ok {
			t.Fatalf("tools/list: tool %q not registered", toolName)
		}
		raw, ok := schema["required"]
		if !ok {
			return nil // "required" key absent entirely: nothing is required
		}
		list, ok := raw.([]interface{})
		if !ok {
			t.Fatalf("%s: inputSchema.required = %#v (%T), want []interface{} as decoded from JSON", toolName, raw, raw)
		}
		out := make([]string, len(list))
		for i, v := range list {
			s, ok := v.(string)
			if !ok {
				t.Fatalf("%s: inputSchema.required[%d] = %#v, want string", toolName, i, v)
			}
			out[i] = s
		}
		return out
	}

	contains := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}

	if req := requiredParams(t, "read_file"); !contains(req, "path") {
		t.Errorf("read_file: inputSchema.required = %v, want to include \"path\"", req)
	}

	// list_dir's handler defaults an absent path to ".". The schema must not
	// advertise "path" as required, whether "required" is absent entirely or
	// present without "path" in it — either shape must pass this assertion.
	if req := requiredParams(t, "list_dir"); contains(req, "path") {
		t.Errorf("list_dir: inputSchema.required = %v, must NOT include \"path\" (handler defaults to project root, main.go:585)", req)
	}

	if req := requiredParams(t, "write_file"); len(req) != 2 || !contains(req, "path") || !contains(req, "content") {
		t.Errorf("write_file: inputSchema.required = %v, want exactly [\"path\", \"content\"]", req)
	}

	if req := requiredParams(t, "edit_file"); len(req) != 3 || !contains(req, "path") || !contains(req, "old_string") || !contains(req, "new_string") {
		t.Errorf("edit_file: inputSchema.required = %v, want exactly [\"path\", \"old_string\", \"new_string\"]", req)
	}
}
