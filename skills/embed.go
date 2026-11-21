// Package skills carries the skill documents the launcher ships to agents:
// <name>/SKILL.md plus optional sub-documents <name>/<section>.md.
//
// It exists only because a go:embed pattern cannot reach outside its package
// directory, and the documents live here, at the repository root. Parsing and
// serving belong to internal/skills — import that, not this.
package skills

import "embed"

// FS holds the embedded skill documents. The pattern matches Markdown files
// exactly one directory deep, so this file, top-level files and nested
// directories stay out of the binary. Unlike a whole-directory embed, an
// explicit pattern also matches names starting with "." or "_"; internal/skills
// rejects those, so a draft fails the load instead of shipping.
//
//go:embed */*.md
var FS embed.FS
