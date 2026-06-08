package config

import (
	"fmt"
	"strings"
)

// Decision ist das Ergebnis einer Permission-Prüfung.
type Decision struct {
	Allow   bool
	Message string // Begründung, vor allem bei Ablehnung
}

// Decide entscheidet anhand der Policy, ob ein Tool-Aufruf erlaubt ist.
//
// toolName ist der Name des Tools, das der Agent aufrufen will (z.B. "Bash",
// "Edit", "mcp__foo__bar"). toolInput sind dessen Argumente; für Bash wird das
// Feld "command" für Klammer-Regeln herangezogen.
//
// Reihenfolge: deny-Regeln schlagen immer zu. Danach entscheidet der Mode.
func (p Permissions) Decide(toolName string, toolInput map[string]any) Decision {
	for _, rule := range p.Deny {
		if matchRule(rule, toolName, toolInput) {
			return Decision{Allow: false, Message: fmt.Sprintf("durch deny-Regel %q blockiert", rule)}
		}
	}

	switch p.Mode {
	case ModeAllowAll:
		return Decision{Allow: true, Message: "allow_all"}
	case ModeDenyAll:
		return Decision{Allow: false, Message: "deny_all: alle Anfragen werden abgelehnt"}
	case ModeAllowlist:
		for _, rule := range p.Allow {
			if matchRule(rule, toolName, toolInput) {
				return Decision{Allow: true, Message: fmt.Sprintf("durch allow-Regel %q erlaubt", rule)}
			}
		}
		return Decision{Allow: false, Message: fmt.Sprintf("kein allow-Eintrag passt auf %q", toolName)}
	default:
		return Decision{Allow: false, Message: "unbekannter permission mode"}
	}
}

// matchRule prüft eine einzelne Regel gegen Tool-Name und -Input.
//
// Unterstützte Formen:
//   - "Edit"            exakter Tool-Name
//   - "mcp__server__*"  Präfix-Wildcard (Stern am Ende)
//   - "*"               alles
//   - "Bash(git *)"     Tool + Inner-Pattern gegen toolInput["command"]
func matchRule(rule, toolName string, toolInput map[string]any) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false
	}
	// Klammer-Regel: Tool(inner)
	if open := strings.IndexByte(rule, '('); open >= 0 && strings.HasSuffix(rule, ")") {
		tool := strings.TrimSpace(rule[:open])
		inner := rule[open+1 : len(rule)-1]
		if !globEqual(tool, toolName) {
			return false
		}
		cmd, _ := toolInput["command"].(string)
		return globMatch(inner, cmd)
	}
	return globEqual(rule, toolName)
}

// globEqual vergleicht ein Pattern (optional mit Trailing-*) case-sensitiv
// gegen einen exakten Wert.
func globEqual(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == value
}

// globMatch verhält sich wie globEqual, wird aber für Inner-Patterns benutzt
// (z.B. der Bash-Command). Trailing-* erlaubt Präfix-Match.
func globMatch(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	return globEqual(pattern, strings.TrimSpace(value))
}
