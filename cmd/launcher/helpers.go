package main

import (
	"fmt"
	"os"
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

func getInt(args map[string]interface{}, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
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
