// Package agents carries the topic agents the launcher ships for a UE project:
// <name>.md, one agent definition each, plus the two project texts in
// project/ (rules.md, starter.md).
//
// It exists only because a go:embed pattern cannot reach outside its package
// directory, and the documents live here, at the repository root. Parsing
// belongs to internal/skills (LoadAgents) — import that, not this.
package agents

import "embed"

// FS holds the embedded agent definitions and project texts. The patterns
// match Markdown files here and in project/, so this file and anything nested
// elsewhere stay out of the binary. As in skills/, an explicit pattern also
// matches names starting with "." or "_"; internal/skills rejects those, so a
// draft fails the load instead of shipping.
//
//go:embed *.md project/*.md
var FS embed.FS
