package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/conflict-industries/unreagent/internal/mcp"
)

func warnIfMissing(logger func(string), label, path string) {
	if !fileExists(path) {
		logger(fmt.Sprintf("WARN %s: path not found: %s", label, path))
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func errResult(err error) mcp.ToolResult {
	return mcp.ToolResult{Text: "Error: " + err.Error(), IsError: true}
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func getString(args map[string]interface{}, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

// stringArg reads a string argument and reports whether the key was actually
// present and held a string. Unlike getString it tells a missing key apart from
// an empty value, which matters wherever "" is a legal payload but a typo in the
// key name would otherwise silently destroy data.
func stringArg(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key].(string)
	return v, ok
}

// getInt reads an integer argument. A JSON number arrives as float64 and is
// truncated; a numeric string ("5") is accepted too, because some MCP clients
// send every argument as a string. An absent key, null or "" yields def, as
// for getString. Anything else is an error, not a silent default.
func getInt(args map[string]interface{}, key string, def int) (int, error) {
	switch v := args[key].(type) {
	case nil:
		return def, nil
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return def, nil
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("'%s' must be an integer, got %q", key, v)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("'%s' must be an integer, got %T", key, v)
	}
}

func firstString(args map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func firstMap(args map[string]interface{}, keys ...string) map[string]interface{} {
	for _, k := range keys {
		if v, ok := args[k].(map[string]interface{}); ok {
			return v
		}
	}
	return nil
}
