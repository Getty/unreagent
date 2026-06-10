# CLAUDE.md

Guidance for Claude Code when working in this repository.

## What this is

`unreagent` is a single-binary **launcher / orchestrator for Unreal Engine
projects** (Go, cross-compiled Linux → Windows). It supervises the UE editor
and an agent (e.g. Claude Code) and exposes an **MCP server** so the agent can
control the editor, run builds, read logs, and execute Python/Node in prepared
runtimes.

Layout:

| Path | Purpose |
|---|---|
| `cmd/launcher/` | main package, `versioninfo.json`, Windows manifest/icon |
| `internal/config/` | YAML config loading + `${ENGINE}` / `${PROJECT}` resolution |
| `internal/mcp/` | MCP server (HTTP) + tool handlers |
| `internal/supervisor/` | process supervisor (Job Objects on Windows) |
| `scripts/` | build / sign helpers (`gen-codesign-cert.sh`, `sign-windows.sh`) |
| `signing/` | self-signed code-signing cert + import script (private key NOT in repo) |
| `example/` | runnable demo (`ping`-as-UE) |
| `vendor/` | **vendored Go dependencies** — checked in for offline reproducible builds |

## Project language

**English for everything that ships in the repo and to external systems:**

- README, docs, comments
- Git commit messages
- GitHub repo description, release notes, issues, PRs
- MCP tool names / descriptions / log messages visible to the user

The author is fine chatting in German, but that does **not** leak into repo
artefacts. Translate before committing.

## Build & dev

Primary target is a Windows `.exe` cross-compiled from Linux. Go ≥ 1.19.

```bash
make windows       # → dist/unreagent.exe
make linux         # → dist/unreagent (dev)
make win-signed    # build + sign with signing/codesign.key via osslsigncode
make resource      # rebuild cmd/launcher/resource_windows_amd64.syso
make fmt vet       # gofmt + go vet
```

Dependencies are vendored (`vendor/`, `go.mod` with `vendor` mode). No network
needed at build time. CI is in `.github/workflows/ci.yml` and
`release.yml` (uses Git LFS for binary assets — see `.gitattributes`).

## Conventions

- **Go style** — `gofmt`, `go vet ./...` clean. No external lint config.
- **Standard library first** — only one non-vendored dep: a YAML parser.
  Anything new must be added to `vendor/`, not pulled at build time.
- **No machine-specific paths in `unreagent.yaml`** — use `${ENGINE}`,
  `${PROJECT}` placeholders. Machine overrides go in a git-ignored
  `unreagent.local.yaml` (overlay).
- **Windows metadata** — publisher name, version, description live in
  `cmd/launcher/versioninfo.json`. Bump + `make resource` when changing.
- **Signing** — never commit the private key (`signing/codesign.key`). Public
  cert + import script are checked in so users can silence the
  "Unknown Publisher" warning.
- **Process lifecycle on Windows** — every spawned process is attached to a
  Job Object with `KILL_ON_JOB_CLOSE`. This is load-bearing for "no zombies"
  and must not be removed.
- **Agent ↔ Launcher** — the launcher injects `--mcp-config` for Claude Code
  and `--permission-prompt-tool mcp__unreagent__approve` when permissions are
  enabled. Bridge smoke-tests run before the agent starts (initialize
  handshake, port-3000 readiness for the in-editor plugin).

## Out of scope here

This repo does **not** contain the UE project itself. `unreagent.exe` +
`unreagent.yaml` are dropped into a UE project, and the project's plugin
(e.g. UE LLM Toolkit) lives under `<UProject>/Plugins/`. Configs in this repo
are examples (`unreagent.example.yaml`).
