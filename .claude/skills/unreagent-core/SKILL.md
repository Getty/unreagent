---
name: unreagent-core
description: >
  Architecture, invariants and build discipline of the unreagent launcher —
  the single-binary UE editor/agent orchestrator with an embedded MCP server.
  Use when implementing, refactoring, reviewing or debugging anything in this
  repository.
---

# unreagent — core

A single Windows `.exe`, cross-compiled from Linux, that supervises two long-lived
processes (the Unreal Editor and an agent such as Claude Code) and exposes an HTTP
MCP server so the agent can drive the editor, run builds, read logs and execute
Python/Node.

## The four layers

| Layer | Package | What it owns |
|---|---|---|
| Wiring | `cmd/launcher/main.go` | flag parsing, subcommands, tool registration, bridge smoke tests, agent lifecycle, console loop |
| Config | `internal/config/` | YAML load, `unreagent.local.yaml` overlay, `${…}` substitution, defaults, validation |
| MCP | `internal/mcp/` | minimal JSON-RPC-over-HTTP server, tool registry, bearer auth |
| Processes | `internal/supervisor/` | services + one-shot commands, restart policy, ring-buffer logs, Windows Job Objects |

Dependency direction is strictly downward: `main` knows all three, the three know
nothing of each other and nothing of `main`. A supervisor that imports `mcp` is a
design error, not a shortcut.

## Non-negotiable invariants

1. **Stdlib only.** The single external dependency is `gopkg.in/yaml.v3`. Anything
   new must be vendored into `vendor/` and justified — the offline-reproducible
   build is a feature, not an accident. Reach for `net/http`, `encoding/json`,
   `os/exec`, `syscall` before considering a library.
2. **`vendor/` is checked in** and `go.mod` runs in vendor mode. Never `go get`
   during a build; never edit `vendor/` by hand.
3. **Job Objects are load-bearing.** Every spawned process on Windows is attached
   to a Job Object with `KILL_ON_JOB_CLOSE` (`internal/supervisor/job_windows.go`).
   This is what makes "no zombie ShaderCompileWorker" an OS guarantee rather than a
   hope. Removing or bypassing it silently breaks the product's core promise.
4. **No machine-specific paths in shipped config.** `unreagent.example.yaml` uses
   `${ENGINE}` / `${PROJECT}` / `${PROJECT_DIR}` / `${PROJECT_NAME}` only. Machine
   overrides belong in the git-ignored `unreagent.local.yaml` overlay.
5. **English everywhere that ships** — code comments, log lines, MCP tool
   descriptions, README, commit messages. The author chats in German; that must not
   leak into repo artefacts. Note that older code still carries German comments
   (`internal/config/config.go`, `internal/supervisor/`): when you touch such a
   line, translate it; do not run a repo-wide translation sweep as a side quest.
6. **The launcher must never wedge unattended.** Every wait has a timeout, every
   failed precondition is logged with the concrete cause and the check continues.
   Silent failure is the one unacceptable outcome — see the bridge smoke tests in
   `prepareMCPBridges` / `smokeTestMCP` for the house pattern: check, log loudly,
   name the usual suspects, keep going.

## Build & verification gate

```bash
make windows    # dist/unreagent.exe — the real target (cross-compile)
make linux      # dist/unreagent — local test binary
make fmt vet    # gofmt -w . && go vet ./...
go test ./...   # tests live next to the code they cover
```

CI (`.github/workflows/ci.yml`) fails on: `gofmt -l cmd internal` non-empty,
`go vet ./...`, and both cross-compiles. The Windows build there runs with
`CGO_ENABLED=0` — the runner's gcc is Linux and rejects `-mconsole`. The project
has no CGO code, so this is safe; do not "fix" it by enabling CGO.

Before claiming work is done: `make fmt vet && go test ./... && make windows`.
A Linux-only build proves nothing about the actual shipping target.

## Testing reality

Only two test files exist (`internal/supervisor/supervisor_test.go`,
`cmd/launcher/mcpcheck_test.go`). There is no mock harness and no fixture system —
tests use the stdlib `testing` package, real temp dirs, and real short-lived
processes. New tests follow that style rather than introducing a framework.

Windows-only code paths (Job Objects, taskbar flash, registry engine detection)
cannot be exercised on the Linux dev box. Test the platform-independent logic and
guard the rest behind build tags; do not fake `syscall` to reach coverage.

## Where the product is documented

`README.md` is user-facing and thorough (setup, portability, in-editor MCP wiring,
troubleshooting, MCP tool table, CLI flags). It is the contract users read — a
behavior change that is not reflected there is half-finished. `CLAUDE.md` holds the
short repo rules; `docs/` holds design specs.
