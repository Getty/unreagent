---
name: unreagent-permissions
description: >
  How unreagent's permission layer decides a tool call and how to change it
  without opening a bypass — Decide, deny before mode, the fail-closed rule per
  rule sense, the shell splitter and tokenizer, path rules, the known gaps and
  their cards, and how a bypass becomes a failing test first. Use before
  touching, testing or reviewing internal/config/permission.go or its tests,
  the approve tool, or the --permission-prompt-tool wiring in cmd/launcher, and
  before working any card tagged permissions.
---

# unreagent — the permission layer

Code: `internal/config/permission.go`, tests `permission_test.go` (same
package). Wiring: the `approve` tool in `registerTools` (`cmd/launcher/tools.go`)
and the agent-start block of `run()` (`cmd/launcher/main.go`).

Not here: the config surface (`permissions.enabled/mode/allow/deny`, defaults,
placeholder substitution into rules, the load check) is in skill
`unreagent-config`; the user-facing contract is README "Permission layer".

The framing that every change must keep: matching a command line by pattern is
**a guard rail, not a security boundary** (`denyCommand`'s comment, k59 item 4).
Allowlist mode is the only mode that fails closed by construction. Never
describe a deny list as closing anything.

## The model

`Permissions.Decide(toolName, toolInput) Decision` — `Decision{Allow, Message}`.

1. Every `Deny` rule is tried with `senseDeny`. The first match rejects, in
   every mode ("deny rules win over allow rules" in `TestModes`).
2. Then the mode: `ModeAllowAll` allows; `ModeDenyAll` rejects; `ModeAllowlist`
   allows only what `Allow` covers; anything else rejects ("unknown permission
   mode" — `validate` refuses it at load, `Decide` does not rely on that).
3. In allowlist mode a call whose primary input field is `command`
   (`primaryInput`) goes to `allowCommand`; every other call needs one `Allow`
   rule that matches with `senseAllow`.

Rule shapes (`splitRule`, `matchRule`): `Tool`, `Tool(inner)`, and a trailing
`*` on either part. `globEqual` knows only a trailing `*`; a `*` elsewhere is a
literal byte, and the comparison is case-sensitive. A bare tool name covers
every input, on either list.

## When the layer is enforced at all

`Decide` has one caller: the `approve` handler, registered only while
`permissions.enabled`. Claude Code reaches it through
`--permission-prompt-tool mcp__unreagent__approve` (`approveTool`).

- The launcher passes that flag only with `agent.claudeIntegration`, only to an
  agent that is not interactive, and only when `agent.args` wires no such flag
  by hand (k172; `promptToolsIn`, `TestRunPassesNoSecondPermissionPromptTool`).
- Interactive (`agentInteractive`: `agent.window` if set, otherwise no
  `-p`/`--print` in `agent.args`): the policy is **not applied**. With `allow_all` the
  launcher passes `--dangerously-skip-permissions` instead, so even a call a
  deny rule names runs. `permissionsInertReason` produces the warning; the
  launcher warns and starts anyway (maintainer's decision, k38 and k46). Keep
  that function and the agent-start block in step — its comment says so, and
  `TestPermissionsInertReason` pins the cases.
- The handler fails closed before `Decide`: no tool name, or no input object,
  is a deny (`TestApproveFailsClosedOnUnreadablePayload`) — `Decide("", {})`
  would allow under `allow_all`. Payload shape: skill `unreagent-mcp-tools`.
- Open: whether `agent.window: false` without `-p` makes Claude Code abort on
  the flag (k168). Do not state either answer.

## Fail closed, per rule sense

This is the rule the file is built on (`ruleSense`):

> A rule that cannot be evaluated with confidence fails closed, and "closed"
> is the opposite thing per sense. A **deny** rule in doubt **matches**. An
> **allow** rule in doubt **does not grant**.

Where the code already applies it — a new branch has to fit this table:

| Doubt | Deny (`senseDeny`) | Allow (`senseAllow`) | Pinned by |
|---|---|---|---|
| No known input field carries a string — a blank one counts as missing (`stringInput`, `matchInner`) | matches | no grant | `TestUnevaluableBracketRulesFailClosed` |
| Several known fields present | any field matching is enough | only the first present field is read | deny: "deny rule on Grep path hits" in `TestBracketRulesForNonBashTools`; allow: no test yet |
| Command line undecidable (`shellSegments` yields none) | every Bash deny rule matches (`denyCommand`) | nothing granted (`allowCommand`) | `TestUndecidableCommandsFailClosed` |
| Several commands on a line | ANY segment matching is enough | EVERY segment must be covered | `TestDenyRuleEvasions`, `TestAllowlistSegments` |
| Spelling of a command | tested against every `commandVariants` spelling | tested against the raw segment only | deny: `TestDenyRuleEvasions`; allow (`"git" status` is not granted by `Bash(git *)`): no test yet |
| Path with two readings (`pathReadings`) | any reading matching is enough | every reading must match | `TestPathMatchesAmbiguousLeadingSeparators` |
| Path pattern that names no absolute path below a root (`pathPatternProblem`) | matched as written — it can only block | no grant (`matchValue`) | `TestPathAllowRulesDoNotWiden`, `TestPathDenyRulesAtARoot` |
| Output redirection the rule does not name (`namesRedirections`) | not consulted | segment not covered | `TestAllowlistRedirections` |

What this obliges every new parser branch to do:

- **Answer for both senses.** If the branch has one outcome ("cannot follow
  this"), route it into the undecidable path (`ok = false` in
  `splitSegments`), which already means the right thing to both.
- **List what is followed, not what is refused.** `readBrace` names the `${…}`
  bodies it reads and rejects every other one (k113). A list of dangerous forms
  is always one form short.
- **Reading more is a risk.** Modelling a construct so that ordinary lines
  pass creates text the splitter now skips. k113's `$'…'` support and k127's
  comment and here-document support each opened bypasses the cruder code had
  denied (their review notes). Probe what the new reader hides.
- **Every reader must agree.** A construct is read in several places:
  `splitSegments`, `readDelimited`, `readBrace`, `outputRedirections`,
  `redirection`, `shellTokens`. One that disagrees with the others desyncs them
  — the `$$'…` bug of k113 was in all of them at once
  (`TestDollarDollarAndBraceNesting`).
- **A heuristic that errs must err closed**, and say so, as the comment on
  `outputRedirections` does for the `>` in `[[ a > b ]]`.
- A fail-closed choice that denies ordinary lines is a policy call. Those in
  the file are marked as the orchestrating agent's, not the maintainer's
  (`allowCommand` for k59, `shellSegments` for k113). Mark a new one likewise.

## How a shell line becomes segments (main)

**k127 is in review with a rewritten splitter that is not on main.** This
section describes main; when k127 lands, rewrite it (comments, line
continuation, backslash rules, here-documents, `\r`, and its new tests
`TestShellComments`, `TestHereDocuments`, `TestBackslashEscapes`,
`TestQuotedStringsInsideSubstitutions`).

`shellSegments` → `splitSegments(command, depth)`:

- Breaks (`isSegmentBreak`): unquoted `;` `&&` `||` `|` `&` newline, CR, `(`
  `)`, and `{` `}` standing alone as a word. Not a break: `&` inside a
  redirection (`2>&1`, `&>f`, `>&2`), the `|` of `>|`.
- Read as quoting: `'…'`, `"…"`, backslash, `$'…'` (`readANSIC`; decoded by
  `decodeANSIC` as bash 5.2 does, `TestANSICQuoting`), `$$` as one token.
- Bodies of `$(…)` (`readDelimited`) and backquotes (`readBackquote`) become
  segments of their own **and** stay in the segment they stand in, so
  `rm -rf $(pwd)` is still an `rm -rf ` command. Read inside double quotes too.
- `nestedScripts`: after a shell name (`isShellName`) with a script flag
  (`isScriptFlag`: `-c`, `/c`, `/k`, `-Command`, `-EncodedCommand`, combined
  short flags such as `-lc` for POSIX shells), the rest is split as a script.
- Each segment is collapsed by `normalizeSpace`. Table: `TestShellSegments`.

Undecidable (no segments at all): a `${…}` whose body `readBrace` does not
follow, and nesting deeper than `maxShellDepth` (4). `readBrace` follows only
`${#name}` and `${name}` with an optional `:- - :+ + :? ? # ## % %%` and a word
free of `$(`, `${`, `$[`, backquote, quotes, `<(`, `>(`.

Matching the segments:

- **Deny** (`denyCommand`): each segment in each `commandVariants` spelling —
  as written, quotes and escapes resolved (`shellTokens`), path-qualified
  command reduced to its base name (`commandWord`: lower case, no `.exe`), and
  for an `isWrapper` word (`sudo`, `env`, `xargs`, `eval`, `timeout`, …) every
  token suffix.
- **Allow** (`allowCommand`): segments may be covered by different rules
  (`TestAllowlistCoversChainsAcrossRules`); a rule without brackets covers the
  line. A file-writing redirection (`outputRedirections`, `redirection`: `>`
  `>>` `>|` `&>` `>&file` `<>`) must sit in the rule's literal part before the
  trailing `*`. fd duplication and closing and the null device do not count.
- No wrapper or variant logic on the allow side: `Bash(sudo *)` grants whatever
  `sudo` runs. A nested shell's script does need cover of its own
  (`TestAllowlistSegments`, "wrapped in a nested shell").

**Bash versus PowerShell.** There is one splitter, and it reads bash. The
`PowerShell` tool has no entry in `toolInputKeys`; it reaches the shell path
through `genericInputKeys`, whose first key is `command`. The only dialect
switch is `powershell` in `allowCommand`, and it selects the null device only
(`isNullDevice`: `/dev/null` for Bash, a bare `$null` in any case for
PowerShell; `NUL` is a file in both). Everything else about PowerShell is k114.

## Rules for other tools

- Field per tool: `toolInputKeys`, first entry primary (`Read`/`Write`/`Edit`/
  `MultiEdit`: `file_path`, `path`; `NotebookEdit`: `notebook_path` first;
  `Glob`/`Grep`: `path`, `pattern`; `WebFetch`: `url`; `WebSearch`: `query`).
  Any other tool, MCP tools above all: `genericInputKeys`.
- `matchValue` dispatches on the **key**, not the tool: `command` gets the
  shell treatment, a path key (`isPathKey`) the path treatment, the rest a
  plain `globMatch`. An MCP tool with a `command` argument is split like Bash.
- Consequence of "first present field": `Grep(TODO*)` grants a call with only
  `pattern`, and nothing once the call carries `path`.
- Path rules: both sides go through `normalizePath` (backslashes to slashes,
  `.` and `..` resolved; a drive letter and, with `windows`, a UNC
  `//server/share` stay as a root `..` cannot climb) — `TestPathRulesNormalize`.
- `checkRules` (load time, only while `permissions.enabled`) rejects a relative
  pattern for the file tools (`isPathTool`) on both lists, and a root-only one
  on the allow list (k104; `TestPathRulesMustBeAbsolute` in `load_test.go`).
- **Windows semantics are a parameter.** `windowsPaths` is a build constant
  (`runtime.GOOS == "windows"`); `pathMatches` and `pathPatternProblem` take
  `windows bool`, so both behaviours run on Linux (`TestPathMatchesPerOS`,
  `TestPathPatternProblem`). With it: case-insensitive, UNC roots. Without it
  only a pattern with a drive letter folds case; folding is to **upper** case
  on purpose (`pathMatches` comment). A new OS-dependent rule takes the same
  parameter. Tested through the pure helper is not tested on Windows — label
  it unverified.

## Known gaps — none of these is closed

| Card | Gap on main |
|---|---|
| k127 (review) | A quote inside a `#` comment opens a string; here-document bodies are split as commands; `readBackquote` drops every backslash; `shellTokens` drops every backslash inside double quotes; backslash-newline is not joined. Allowlist and deny bypasses. |
| k183 | A `case` pattern's `)` inside `$(…)` ends the substitution early (`readDelimited`). Deny bypass. Folded into k127 round 5 per its card. |
| k184 | `env -S` / `--split-string`: the split argument is never read as a command. Deny bypass. |
| k171 | A value planted by one segment runs in a later arithmetic context (`$((x))`, `((x))`, `let`, `[[ x -eq 0 ]]`). Deny rules only. Undecided: undecidable, or documented. |
| k114 | PowerShell is read with bash rules: backslash is taken as an escape, the backtick is not, comments and `@"…"@` here-strings are unknown. Allowlist and deny bypasses, unverified on a real pwsh. |
| k109 | Windows path aliases (`\\?\`, `\\.\`, 8.3 names, trailing dots, `::$DATA`), a relative value against an absolute deny rule, upper-case folding of `ſ` and `ı`. |
| k110 | The launcher's own file tools take paths relative to `files.root`, so a path rule for `mcp__unreagent__*` never meets a real call. Decision open. |
| k168 | See "When the layer is enforced". |

Accepted, no card, named in `denyCommand`'s comment and on k127: variable
indirection (`X=rm; $X …`), aliases, and output run as code (`eval "$(…)"`,
`… | bash`). They pass a deny rule. The allowlist grants them only when rules
cover every segment: `eval` or the shell itself for the last two, the
assignment and the `$X …` segment for indirection.

Card ids are the record; check `karr show <id>` before quoting a status.

## How to add a case

1. **The bypass is a failing test first.** Write the probe as a table row and
   watch it fail on the unchanged code, then change the parser.
2. **Run the probe in a real shell** with a harmless stand-in — the tables use
   `touch M` for a hidden command and `> out` for a hidden write — and record
   what the shell did (bash 5.2 so far). A probe nobody ran is a guess; for
   PowerShell say "unverified" (no pwsh on the dev host, k114).
3. **Use all three policies**, as `TestUndecidableCommandsFailClosed` does:
   allowlist `Bash(echo *)`; allowlist `Bash(bash *)` + `Bash(echo *)`;
   `allow_all` with deny `Bash(touch *)`. A fix checked under one sense only
   has not been checked.
4. **Where it goes.** Tests are grouped by behaviour, not by card, and go
   through `Decide` (`decideBash`, `bashInput`): deny spellings →
   `TestDenyRuleEvasions`; coverage → `TestAllowlistSegments`,
   `TestAllowlistCoversChainsAcrossRules`, `TestAllowlistShellSyntax`;
   redirections → `TestAllowlistRedirections`; undecidable →
   `TestUndecidableCommandsFailClosed`; segment shape → `TestShellSegments`;
   paths → the `TestPath*` tables; the pure helpers directly only where the OS
   is the variable. Row names say the shape (some k113 rows carry the
   reviewer's probe id); the card id goes in a comment.
5. **Pin the ordinary neighbour too**: the form that must stay allowed
   ("decidable:" rows, the `allowed` table in `TestDenyRuleEvasions`). Without
   it a parser that denies everything passes.
6. **Show the test bites**: revert or mutate the new branch with
   `go test -overlay` (the tree is never edited) and see the row go red. A
   surviving mutant is an unpinned branch or dead code (k59 nits, k127 M16).
7. Test candidates: `docs/research/2026-11-21-shell-constructs.md` is an
   external, **unchecked** corpus of constructs that hide a command. Take
   shapes from it, verify each in a real shell before pinning it, and do not
   read its static/refuse marks as decisions.

What the reviewer probes, beyond the card's own shapes:

- the new branch under both senses and inside every container: double quotes,
  `$(…)`, backquotes, `${…}`, a nested `bash -c`, and at `maxShellDepth`;
- what the new reader now **skips** — can a `;`, a `>` or a closing delimiter
  hide in it? A line main denied that is now allowed needs a reason;
- the redirection guard again after any quoting change (`outputRedirections`
  rescans the collapsed segment);
- the same line as a `PowerShell` call.

A README sentence per changed rule follows the code (`unreagent-doc-writer`),
and this skill's splitter section and gap table change with it.
