<!-- ack: shape=project manager=none board=karr committer=release-manager outside_input=on headless=off -->
<!-- ack-tune: since=2026-08-21T22:46:13Z schema=1 -->
# unreagent House Rules

The shared house rules come from skilletor — bundle `getty-house@getty`: engineering
discipline, the delegation lock, the publish gate, karr coordination. They load as
`.claude/rules/.local.*.md`; if they are missing, run `skilletor sync` and publish nothing
until they load. This file adds what is true only here, and wins where it differs.

## What this repo is

A single-binary launcher / orchestrator for Unreal Engine projects (Go, cross-compiled
Linux → Windows): it supervises the UE editor and an agent and exposes an MCP server for
them. Read first: the callers and the shared helpers around the change.

## Agent team

`OWNERS` is the authoritative write-path map; this table routes responsibility. Before
handing out or reviewing a card, resolve its touched or planned paths with
`ack-team owner <file>`; an unknown path is fixed in `OWNERS` first.

| Task | Agent |
|---|---|
| Implement / refactor / debug Go code | `unreagent-worker` (default) |
| Write/extend tests, a bug as a failing test | `unreagent-test-writer` |
| Shipped UE skill documents + the skills subsystem; the project's `unreagent-*` skills | `unreagent-skill-author` |
| README / example config / docs | `unreagent-doc-writer` |
| Read-only research, audit, gap analysis, card triage | `unreagent-scout` |
| Review of a finished card before its commit (runs the check itself) | `unreagent-reviewer` |
| Commits, `Changes`, card → done, pre-release audit | `unreagent-release-manager` |

Behavior-relevant = process lifecycle, MCP protocol and tools, config semantics,
platform-guarded code, error handling, tests. Prose docs are not.

Card flow: owner works it → `review` → `unreagent-reviewer` notes its verdict →
`unreagent-release-manager` commits the checked state and moves it to `done`.

## Release

Builds and tests are fine anytime. **Pushing a `v*` tag IS the release** — it triggers
`release.yml`, which builds, packages and publishes to GitHub. No dry run, no undo.

## Hazards

- A green Linux build says nothing about the product — the artefact is a Windows `.exe`.
  Run `make windows` as well and label Windows-path changes unverified, not tested.
- `syscall.Call()`'s third return value is the error; a separate `GetLastError` races
  the scheduler. Do not reintroduce the bug already fixed in `22f8ca6`.
- Job Objects with `KILL_ON_JOB_CLOSE` are the no-zombie guarantee — never drop the job
  assignment as "cleanup".
- `unreagent.local.yaml` overlays by YAML decode, not deep merge: a list replaces the base
  list, a map merges by key (each given entry replaced whole) — pinned by
  `internal/config/overlay_test.go`. Say so whenever you document a list- or map-valued option.
- CGO breaks the cross-compile (`CGO_ENABLED=0` in CI) — Win32 goes through
  `syscall.NewLazyDLL`, never cgo.
- Stdlib-only on purpose, and `vendor/` is committed with an offline build — never `go get`
  mid-task; a new dependency needs a real argument and has to be vendored.
- `cmd/launcher` is split by concern (k93: `main.go`, `tools.go`, `filetools.go`,
  `scripts.go`, `bridge.go`, `lifecycle.go`, `mcpconfig.go`, `helpers.go`, `hermes.go`),
  but `run()` in `main.go` still wires everything and many cards touch it. Cards that
  edit the same file — `run()` above all — run one after another; fan out only across
  disjoint files, and give parallel Go workers separate worktrees, since a half-edited
  file breaks the whole package's build for everyone else.

## Language — English in everything that ships

README, docs, comments, log lines, MCP tool descriptions, commit messages: German chat must not
leak into repo artefacts. Older files still carry German comments — translate what you touch,
don't sweep. Architecture lives in the `unreagent-*` skills — reference, don't restate.
