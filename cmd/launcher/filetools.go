package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/conflict-industries/unreagent/internal/mcp"
)

// registerFileTools registers read/list (+ write/edit, unless readOnly) on the
// MCP server, all paths strictly restricted to root: no "..", no absolute
// path outside root, no symlink (or Windows junction) that is absolute or
// leads out of root, no Windows reserved device name.
func registerFileTools(srv *mcp.Server, root string, readOnly bool) {
	rootAbs, err := filepath.Abs(root)
	if err != nil || root == "" {
		rootAbs, _ = filepath.Abs(".")
	}
	// confine checks the given path lexically (an absolute one under root is
	// made relative first) and opens root for one call; the handler then
	// touches the returned relative name only through the returned *os.Root
	// and closes it.
	// os.Root resolves symlinks itself, component by component and without a
	// check-then-use race, and refuses any that lead out of root, also on the
	// way to a file or directory that does not exist yet (k49). It refuses
	// absolute link targets as well, so a junction (always absolute) is
	// refused even when it points back inside root. The root is opened per
	// call rather than once, so no handle keeps the project dir open on
	// Windows and a root created after start-up still works.
	//
	// Paths in the messages are quoted with "%s", not %q, so a Windows path
	// keeps single backslashes.
	confine := func(given string) (*os.Root, string, error) {
		rel := given
		if rel == "" {
			rel = "."
		}
		mustBeRelative := func() error {
			return fmt.Errorf("path \"%s\" must be relative to the project root \"%s\"; an absolute path is accepted only under it", given, rootAbs)
		}
		if filepath.IsAbs(rel) {
			var ok bool
			if rel, ok = relToRoot(rootAbs, rel); !ok {
				return nil, "", mustBeRelative()
			}
		}
		if part, ok := reservedDeviceName(rel); ok {
			return nil, "", fmt.Errorf("path component %q is a reserved Windows device name", part)
		}
		if !filepath.IsLocal(rel) {
			// Rooted (\Source on Windows) or drive-relative (C:Source).
			if filepath.VolumeName(rel) != "" || os.IsPathSeparator(rel[0]) {
				return nil, "", mustBeRelative()
			}
			return nil, "", fmt.Errorf("path outside the allowed root")
		}
		r, err := os.OpenRoot(rootAbs)
		if err != nil {
			return nil, "", err
		}
		return r, rel, nil
	}
	// fsErr is the result for a failed os.Root call on the path the agent gave.
	fsErr := func(given string, err error) mcp.ToolResult {
		return errResult(explainLinkEscape(given, rootAbs, err))
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
			r, name, err := confine(path)
			if err != nil {
				return errResult(err)
			}
			defer r.Close()
			b, err := r.ReadFile(name)
			if err != nil {
				return fsErr(path, err)
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
			path := getString(args, "path", ".")
			r, name, err := confine(path)
			if err != nil {
				return errResult(err)
			}
			defer r.Close()
			dir, err := r.Open(name)
			if err != nil {
				return fsErr(path, err)
			}
			entries, err := dir.ReadDir(-1)
			dir.Close()
			if err != nil {
				return fsErr(path, err)
			}
			// Sorted by name, as os.ReadDir returned them.
			slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
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
			r, name, err := confine(path)
			if err != nil {
				return errResult(err)
			}
			defer r.Close()
			content, ok := stringArg(args, "content")
			if !ok {
				return mcp.ToolResult{Text: "Error: 'content' missing", IsError: true}
			}
			if err := r.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return fsErr(path, err)
			}
			if err := r.WriteFile(name, []byte(content), 0o644); err != nil {
				return fsErr(path, err)
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
			r, name, err := confine(path)
			if err != nil {
				return errResult(err)
			}
			defer r.Close()
			oldS := getString(args, "old_string", "")
			if oldS == "" {
				return mcp.ToolResult{Text: "Error: 'old_string' missing", IsError: true}
			}
			newS, ok := stringArg(args, "new_string")
			if !ok {
				return mcp.ToolResult{Text: "Error: 'new_string' missing", IsError: true}
			}
			b, err := r.ReadFile(name)
			if err != nil {
				return fsErr(path, err)
			}
			content := string(b)
			n := strings.Count(content, oldS)
			if n == 0 {
				return mcp.ToolResult{Text: "Error: 'old_string' not found", IsError: true}
			}
			content = strings.ReplaceAll(content, oldS, newS)
			if err := r.WriteFile(name, []byte(content), 0o644); err != nil {
				return fsErr(path, err)
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

// relToRoot maps an absolute path that lies under root to a path relative to
// it. root counts as spelled in the config and with its symlinks resolved, so
// either spelling an agent picked up works. The mapping is lexical (Rel
// cleans, and on Windows ignores letter case); the os.Root the result is then
// opened through still decides what the path really reaches. Only escaping
// is tested here, not filepath.IsLocal: on Windows that also refuses reserved
// names, which confine reports with its own message.
func relToRoot(root, abs string) (string, bool) {
	bases := []string{root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
		bases = append(bases, resolved)
	}
	up := ".." + string(filepath.Separator)
	for _, base := range bases {
		if rel, err := filepath.Rel(base, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, up) {
			return rel, true
		}
	}
	return "", false
}

// explainLinkEscape wraps the error os.Root returns when a link on the path is
// absolute or leads out of the root. Go reports that only as "path escapes
// from parent", which names neither the cause nor the root; confine has
// already refused every lexical escape, so here a link is the only cause.
// The sentinel is not exported, so it is matched by its text; the tests pin
// that text, so a Go release that rewords it fails the suite instead of
// silently dropping the explanation. Any other error is returned unchanged.
func explainLinkEscape(given, root string, err error) error {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if e.Error() == "path escapes from parent" {
			return fmt.Errorf("path \"%s\": a link on the path (symlink or junction) is absolute or leads outside the allowed root \"%s\" (%w)", given, root, err)
		}
	}
	return err
}

// reservedDeviceName returns the first component of rel that Windows opens as
// a device instead of a file: CON, PRN, AUX, NUL, COM1-9, LPT1-9 (also with
// the superscript digits ¹ ² ³), CONIN$ and CONOUT$, in any letter case, also
// followed by an extension, a colon or trailing spaces ("aux.h", "NUL.txt",
// "CON "). The matching follows filepath.IsLocal on Windows, with two
// deliberate differences: it applies on every platform, because such a file
// created on Linux cannot be checked out on Windows, and it does not exempt
// names with an extension the way Windows 11 does, because Windows 10 still
// opens "aux.h" as the AUX device. Both separators split components, since
// agents send Windows-style paths on any host (k76).
func reservedDeviceName(rel string) (string, bool) {
	parts := strings.FieldsFunc(rel, func(c rune) bool { return c == '/' || c == '\\' })
	for _, part := range parts {
		base := part
		if i := strings.IndexAny(base, ".:"); i >= 0 {
			base = base[:i]
		}
		base = strings.TrimRight(base, " ")
		// ASCII-only upper-casing: strings.ToUpper would also fold
		// look-alike letters (ı to I) that Windows does not treat as such.
		base = strings.Map(func(c rune) rune {
			if 'a' <= c && c <= 'z' {
				return c - 'a' + 'A'
			}
			return c
		}, base)
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return part, true
		}
		if len(base) > 3 && (base[:3] == "COM" || base[:3] == "LPT") {
			switch base[3:] {
			case "1", "2", "3", "4", "5", "6", "7", "8", "9", "\u00b9", "\u00b2", "\u00b3":
				return part, true
			}
		}
	}
	return "", false
}
