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

| Task | Agent |
|---|---|
| Implement / refactor / debug Go code | `unreagent-worker` (default) |
| Write/extend tests | `unreagent-test-writer` |
| Shipped UE skill documents + the skills subsystem | `unreagent-skill-author` |
| Commits, `Changes`, card → done, pre-release audit | `unreagent-release-manager` |
| README / example config / docs | `unreagent-doc-writer` |
| Read-only research, audit, gap analysis | `unreagent-worker` ("read-only, change nothing") |

Behavior-relevant = process lifecycle, MCP protocol and tools, config semantics,
platform-guarded code, error handling, tests. Prose docs are not.

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
- `unreagent.local.yaml` overlays by YAML decode, not deep merge: a list or map in the
  local file replaces the base one. Say so whenever you document a list-valued option.
- CGO breaks the cross-compile (`CGO_ENABLED=0` in CI) — Win32 goes through
  `syscall.NewLazyDLL`, never cgo.
- Stdlib-only on purpose, and `vendor/` is committed with an offline build — never `go get`
  mid-task; a new dependency needs a real argument and has to be vendored.

## Language — English in everything that ships

README, docs, comments, log lines, MCP tool descriptions, commit messages: German chat must not
leak into repo artefacts. Older files still carry German comments — translate what you touch,
don't sweep. Architecture lives in the `unreagent-*` skills — reference, don't restate.
