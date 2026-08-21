# unreagent House Rules

Apply to every task in this repository unless explicitly overridden. Bias: caution over
speed on non-trivial work. Subagents get their discipline from the skills force-loaded
via `briefing.skills` — this file is for the orchestrating agent.

## Engineering discipline

1. **Think before coding** — State assumptions. When uncertain, ask rather than guess.
   Push back when a simpler approach exists. Stop when confused; name what's unclear.
2. **Simplicity first** — Minimum code that solves the problem. Nothing speculative.
   This project is stdlib-only on purpose; a new dependency needs a real argument.
3. **Surgical changes** — Touch only what you must. Don't "improve" adjacent code,
   comments or formatting. Conformance to existing style beats taste.
4. **Read before you write** — Read the callers and the shared helpers in
   `cmd/launcher/main.go` (`getString`, `firstString`, `errResult`, `serviceAction`,
   `scriptAction`). "Looks orthogonal" is dangerous in a 1500-line wiring file.
5. **Surface conflicts, don't average them** — Contradicting patterns: pick one, explain
   why, flag the other. Don't blend.
6. **Tests verify intent** — A test that can't fail when the logic changes is wrong.
   Reproduce a bug before fixing it; leave a regression test behind.
7. **Fail loud** — "Done" is wrong if anything was skipped silently. "It compiles" is
   not "it works", and here it is specifically not "it works on Windows".

## Delegation

This rule depends on whether the Agent/Task tool is available to you.

- **You can spawn subagents** (orchestrating main agent): Do NOT touch behavior-relevant
  code yourself — delegate to `unreagent-worker`. Your lane: coordinate, inspect, plan,
  review diffs, run builds and tests, manage git, edit prose. When in doubt, delegate.
  Why: only the `unreagent-*` agents get their skills force-loaded via `briefing.skills`;
  you get no briefing and would touch internals with too little context.

  | Task | Agent |
  |---|---|
  | Implement / refactor / debug Go code | `unreagent-worker` (default) |
  | Write/extend tests | `unreagent-test-writer` |
  | Shipped UE skill documents + the skills subsystem | `unreagent-skill-author` |
  | Pre-release audit | `unreagent-release-checker` |
  | README / example config / docs | `unreagent-doc-writer` |

- **You cannot spawn subagents** (you ARE an `unreagent-*` agent): the delegation lock
  does not apply to you — implement, refactor, debug and test per these rules.

Behavior-relevant = process lifecycle, MCP protocol and tools, config semantics,
platform-guarded code, error handling, tests. Prose docs are not.

## Coordination — karr board (always in scope)

Ticket coordination is the orchestrating agent's job, so `karr` is always in scope —
don't invoke the `kanban-issues-karr-cli` skill first, just use it. State lives in
`refs/karr/*`.

`karr list --compact` / `karr board` · `karr show ID` · `karr create "Title" --priority
high --tags a,b` · `karr move ID in-progress --claim NAME` · `karr handoff ID --note "…"`

**Serialize board mutations when fanning out.** Keep implementation parallel, then loop
the `karr move`/`handoff`/`sync` calls sequentially — N landing at once is a resource
event, not a cheap command. No background poll loops on the board.

## Release — never without permission

Builds and tests are fine anytime. **Pushing a `v*` tag IS the release** — it triggers
`release.yml`, which builds, packages and publishes to GitHub. No dry run, no undo.
`git tag`, `git push --tags` and `gh release create` are strictly forbidden without the
maintainer's explicit go-ahead, even if a plan lists "release" as the next step.

## Hazards — the mechanisms, not the morals

- **A green Linux build says nothing about the product.** The artefact is a Windows
  `.exe` cross-compiled from here; Job Objects, the registry engine lookup and any Win32
  call are unreachable on this box. Run `make windows` as well, and label Windows-path
  changes as unverified rather than tested.
- **`syscall.Call()`'s third return value is the error.** Calling `GetLastError`
  separately races the Go scheduler, which can run another call on the same thread in
  between. Already fixed once (`22f8ca6`) — do not reintroduce it.
- **Job Objects with `KILL_ON_JOB_CLOSE` are the no-zombie guarantee.** If a refactor of
  the supervisor drops the job assignment, a crashed launcher leaves ShaderCompileWorker
  and CrashReportClient running. Never remove it as "cleanup".
- **`unreagent.local.yaml` overlays by YAML decode, not deep merge.** A list or map in
  the local file *replaces* the base one. Anyone expecting to append one `agent.args`
  entry silently loses the rest — say so whenever you document a list-valued option.
- **CGO breaks the cross-compile.** CI builds Windows with `CGO_ENABLED=0` because the
  runner's gcc rejects `-mconsole`. Win32 goes through `syscall.NewLazyDLL`, never cgo.
- **`vendor/` is committed and the build is offline.** Never `go get` mid-task; a new
  dependency is a decision, and it has to be vendored.

## Language — English in everything that ships

README, docs, comments, log lines, MCP tool descriptions, commit messages. German chat
does not leak into repo artefacts. Older files still carry German comments: translate
what you touch, don't launch a sweep. Commit message conventions: skill
`git-commit-style`. Architecture and API details live in the `unreagent-*` skills —
do not duplicate them here.
