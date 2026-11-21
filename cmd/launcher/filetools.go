package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conflict-industries/unreagent/internal/mcp"
)

// registerFileTools registers read/list (+ write/edit, unless readOnly) on the
// MCP server, all paths strictly restricted to root.
func registerFileTools(srv *mcp.Server, root string, readOnly bool) {
	rootAbs, err := filepath.Abs(root)
	if err != nil || root == "" {
		rootAbs, _ = filepath.Abs(".")
	}
	resolve := func(rel string) (string, error) {
		if rel == "" {
			rel = "."
		}
		abs, err := filepath.Abs(filepath.Join(rootAbs, rel))
		if err != nil {
			return "", err
		}
		if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
			return "", fmt.Errorf("path outside the allowed root")
		}
		return abs, nil
	}

	srv.AddTool(mcp.Tool{
		Name:        "read_file",
		Description: "Reads a text file from the UE project (path relative to the project root). Lets an agent read source code/config/logs without being present locally. Large files are truncated to their head (first 256 KiB), marked with …[truncated]; the tail is unreachable.",
		InputSchema: requiredPathSchema("path", "Path to the file, relative to the project root"),
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			if path == "" {
				return mcp.ToolResult{Text: "Error: 'path' missing", IsError: true}
			}
			abs, err := resolve(path)
			if err != nil {
				return errResult(err)
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return errResult(err)
			}
			const max = 256 * 1024
			if len(b) > max {
				return mcp.ToolResult{Text: string(b[:max]) + "\n…[truncated]"}
			}
			return mcp.ToolResult{Text: string(b)}
		},
	})

	srv.AddTool(mcp.Tool{
		Name:        "list_dir",
		Description: "Lists the entries of a directory in the UE project (path relative to the root, empty = root). Directories end with /.",
		InputSchema: optionalPathSchema("path", "Directory relative to the root (empty = root)"),
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			abs, err := resolve(getString(args, "path", "."))
			if err != nil {
				return errResult(err)
			}
			entries, err := os.ReadDir(abs)
			if err != nil {
				return errResult(err)
			}
			var sb strings.Builder
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				sb.WriteString(name)
				sb.WriteByte('\n')
			}
			return mcp.ToolResult{Text: sb.String()}
		},
	})

	if readOnly {
		return
	}

	srv.AddTool(mcp.Tool{
		Name:        "write_file",
		Description: "Writes/overwrites a text file in the UE project (path relative to the root). Missing directories are created. Lets an agent create/modify files without being present locally.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "Path relative to the root"},
				"content": map[string]interface{}{"type": "string", "description": "New file content"},
			},
			"required": []string{"path", "content"},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			if path == "" {
				return mcp.ToolResult{Text: "Error: 'path' missing", IsError: true}
			}
			abs, err := resolve(path)
			if err != nil {
				return errResult(err)
			}
			content, ok := stringArg(args, "content")
			if !ok {
				return mcp.ToolResult{Text: "Error: 'content' missing", IsError: true}
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return errResult(err)
			}
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				return errResult(err)
			}
			return mcp.ToolResult{Text: fmt.Sprintf("written: %s (%d bytes)", path, len(content))}
		},
	})

	srv.AddTool(mcp.Tool{
		Name:        "edit_file",
		Description: "Replaces all occurrences of old_string with new_string in a text file (path relative to the root). Error if old_string does not occur.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":       map[string]interface{}{"type": "string", "description": "Path relative to the root"},
				"old_string": map[string]interface{}{"type": "string", "description": "Text to replace"},
				"new_string": map[string]interface{}{"type": "string", "description": "Replacement text"},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			if path == "" {
				return mcp.ToolResult{Text: "Error: 'path' missing", IsError: true}
			}
			abs, err := resolve(path)
			if err != nil {
				return errResult(err)
			}
			oldS := getString(args, "old_string", "")
			if oldS == "" {
				return mcp.ToolResult{Text: "Error: 'old_string' missing", IsError: true}
			}
			newS, ok := stringArg(args, "new_string")
			if !ok {
				return mcp.ToolResult{Text: "Error: 'new_string' missing", IsError: true}
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return errResult(err)
			}
			content := string(b)
			n := strings.Count(content, oldS)
			if n == 0 {
				return mcp.ToolResult{Text: "Error: 'old_string' not found", IsError: true}
			}
			content = strings.ReplaceAll(content, oldS, newS)
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				return errResult(err)
			}
			return mcp.ToolResult{Text: fmt.Sprintf("%d occurrence(s) replaced in %s", n, path)}
		},
	})
}

// requiredPathSchema describes a single string path argument that the tool
// refuses to run without. The "required" key lets a schema-validating client
// reject the call before it ever reaches the handler.
func requiredPathSchema(name, desc string) map[string]interface{} {
	schema := optionalPathSchema(name, desc)
	schema["required"] = []string{name}
	return schema
}

// optionalPathSchema describes a single string path argument the tool may be
// called without, because the handler substitutes a default. It deliberately
// carries no "required" key: omitting the argument must stay a legal call.
func optionalPathSchema(name, desc string) map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			name: map[string]interface{}{"type": "string", "description": desc},
		},
	}
}
