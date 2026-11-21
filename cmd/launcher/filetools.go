package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

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
		Name: "read_file",
		Description: fmt.Sprintf("Reads a text file from the UE project (path relative to the project root): source, config, logs. At most %[1]d KiB per call: ", readFileLimit>>10) +
			fmt.Sprintf("offset (bytes) picks the start, and a negative offset counts from the end, so offset=-%[1]d is the last %[2]d KiB — the end of a log, where a crash is; ", readFileLimit, readFileLimit>>10) +
			fmt.Sprintf("limit (bytes) caps the size, default and maximum %d. ", readFileLimit) +
			"Unless the text is the whole file, a line [bytes X-Y of Z] follows it (Y exclusive, Z the file size; the newline before it is not part of the file), with '; continue with offset=Y' when more follows. " +
			"Range edges move inward to UTF-8 character boundaries, by up to 3 bytes. An offset past the end is an error naming the size.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":   map[string]interface{}{"type": "string", "description": "Path to the file, relative to the project root"},
				"offset": map[string]interface{}{"type": "integer", "description": "First byte to read (default 0); negative counts from the end"},
				"limit":  map[string]interface{}{"type": "integer", "description": fmt.Sprintf("Maximum number of bytes (default and maximum %d)", readFileLimit)},
			},
			"required": []string{"path"},
		},
		Handler: func(args map[string]interface{}) mcp.ToolResult {
			path := getString(args, "path", "")
			if path == "" {
				return mcp.ToolResult{Text: "Error: 'path' missing", IsError: true}
			}
			offset, err := getInt(args, "offset", 0)
			if err != nil {
				return errResult(err)
			}
			limit, err := getInt(args, "limit", readFileLimit)
			if err != nil {
				return errResult(err)
			}
			if limit <= 0 {
				return errResult(fmt.Errorf("'limit' must be a positive number of bytes, got %d", limit))
			}
			r, name, err := confine(path)
			if err != nil {
				return errResult(err)
			}
			defer r.Close()
			// Opened through the root, and read through this handle only:
			// the range is reached by seeking, never by opening the path
			// again.
			f, err := r.Open(name)
			if err != nil {
				return fsErr(path, err)
			}
			defer f.Close()
			fi, err := f.Stat()
			if err != nil {
				return fsErr(path, err)
			}
			if fi.IsDir() {
				return errResult(fmt.Errorf("path \"%s\" is a directory; use list_dir", path))
			}
			text, err := readFileRange(f, path, fi.Size(), int64(offset), int64(min(limit, readFileLimit)))
			if err != nil {
				return errResult(err)
			}
			return mcp.ToolResult{Text: text}
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

// readFileLimit is read_file's default and maximum range, in bytes: the reply
// cap less 1 KiB for the trailer line, which is at most 113 bytes even for a
// file of the largest size an int64 holds. It stays a whole number of KiB, as
// read_file's description names it in KiB.
const readFileLimit = toolReplyBytes - 1<<10

// readFileRange reads the range of f that read_file returns: from offset
// (negative counts back from size, and a start before 0 becomes 0) for at
// most limit bytes, without reading anything outside the range. size is the
// file's size when it was opened; given is the path as the agent spelled it,
// for the error.
//
// The edges then move to UTF-8 character boundaries: a start inside a
// character moves forward past its continuation bytes, an end inside one
// moves back to the character's first byte. The start never moves at offset
// 0 and the end never moves at the end of the file, so a file returned whole
// is returned byte for byte, a byte order mark included. The end does not
// move when that would empty the range, which happens only when fewer than
// 4 bytes remain after the start moved; the range then keeps the cut
// character. The bytes are not checked for valid UTF-8 beyond that: the
// JSON encoding of the result replaces invalid bytes anyway. In bytes that
// are not UTF-8, a range continued at the previous Y can thus skip up to 3
// bytes at its start; its X shows that.
//
// Unless the range is the whole file, the text ends in a trailer line with
// the range returned, "[bytes X-Y of Z]" (Y exclusive), plus "; continue
// with offset=Y" when the file goes on. No trailer thus means the whole file.
func readFileRange(f *os.File, given string, size, offset, limit int64) (string, error) {
	start := offset
	if start < 0 {
		start = max(size+start, 0)
	}
	if start > size {
		return "", fmt.Errorf("'offset' %d is past the end of \"%s\" (%d bytes)", offset, given, size)
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(f, min(limit, size-start)))
	if err != nil {
		return "", err
	}
	if start > 0 {
		skip := 0
		for skip < len(b) && skip < utf8.UTFMax-1 && !utf8.RuneStart(b[skip]) {
			skip++
		}
		b = b[skip:]
		start += int64(skip)
	}
	end := start + int64(len(b))
	if end < size {
		// A character whose first byte lies in the last three bytes may be
		// cut; one that starts earlier is complete, at most 4 bytes long.
		for i := len(b) - 1; i > 0 && i >= len(b)-(utf8.UTFMax-1); i-- {
			if utf8.RuneStart(b[i]) {
				if !utf8.FullRune(b[i:]) {
					end -= int64(len(b) - i)
					b = b[:i]
				}
				break
			}
		}
	}
	if start == 0 && end == size {
		return string(b), nil
	}
	trailer := fmt.Sprintf("[bytes %d-%d of %d]", start, end, size)
	if end < size {
		trailer = fmt.Sprintf("[bytes %d-%d of %d; continue with offset=%d]", start, end, size, end)
	}
	return string(b) + "\n" + trailer, nil
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
