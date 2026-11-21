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

## The layers

| Layer | Package | What it owns |
|---|---|---|
| Wiring | `cmd/launcher/` | flag parsing, subcommands, tool registration, skill delivery, bridge smoke tests, agent lifecycle, console loop |
| Config | `internal/config/` | YAML load, `unreagent.local.yaml` overlay, `${…}` substitution, defaults, validation |
| MCP | `internal/mcp/` | minimal JSON-RPC-over-HTTP server, tool registry, bearer auth |
| Processes | `internal/supervisor/` | services + one-shot commands, restart policy, ring-buffer logs, Windows Job Objects |
| Skills | `internal/skills/` | `Load` parses the shipped skill documents, `DescribeAll` renders their selection list |

The skill documents live in `skills/<name>/` at the repository root, and
`skills/embed.go` holds only the `go:embed` directive — a pattern cannot reach
outside its own package directory. Import `internal/skills`, never `skills`.
`internal/skills` only parses and knows no config; delivery is wiring.
`setupSkills` (`cmd/launcher/skills.go`) runs in `run()` before the agent
starts, loads the set and, as the `skills:` config says, hands it to the
`skills` MCP tool, writes it into `skills.dir`, or both — skill
`unreagent-skill-authoring` holds the rules.

Dependency direction is strictly downward: `main` knows the packages below it,
and they know nothing of each other and nothing of `main`. A supervisor that
imports `mcp` is a design error, not a shortcut.

## The wiring package

`cmd/launcher/` carries the wiring, one file per concern:

| File | What it holds |
|---|---|
| `main.go` | `run()` — flag parsing, sub-command dispatch, config load, MCP bind (`listenMCP`), service and command registration, MCP serving (`serveMCP`), shutdown — plus the sub-command table (`subcommands`, `extractSubcommand`; an unknown word is a `usageError`, which `main` exits 2 for) and small helpers (`hasArg`, …) |
| `tools.go` | `registerTools` (every MCP tool except the file tools), `serviceAction`, `startFailure` |
| `filetools.go` | `registerFileTools` — `read_file` / `list_dir` / `write_file` / `edit_file` and their root confinement |
| `scripts.go` | `scriptAction`, `runtimeDir`, the stale-script sweep, `prepareRuntimes` |
| `bridge.go` | `prepareMCPBridges`, `smokeTestMCP`, `waitForEndpoint` |
| `mcpconfig.go` | `buildMCPServers`, `writeMCPConfigs` (each target 0600 via `writePrivateFile`, Unix only; `unignoredTokenFile` warns when git does not ignore one holding the token), `writeAgentMCPConfig` / `removeAgentMCPConfig` (the embedded agent's `--mcp-config` file, k181: `mcp-config.json` in an `os.MkdirTemp` directory, 0600 in 0700 on Unix, the per-user `%TEMP%` on Windows; never inline JSON on its command line, which every local user can read), `redactAgentArgs` for the logged agent command (it shows the launcher's own config path, and replaces every other `--mcp-config` value, a hand-wired path too) |
| `lifecycle.go` | agent exit handling (`agentEnded`, the exit prompt), the stdin console loop, the session's one console line reader (`console`, `lineReader`), the window-mode launcher log (`openLauncherLog`), the editor's `editorPreStart` (`killCrashReporter` / `crashReporterKillers`, `cleanRecovery`) |
| `skills.go` | skill delivery — `setupSkills`, `checkSkillDirs`, `materializeSkills` (`installSkillDir`, `sweepSkillDirs`), the `skills` tool (`skillsTool`), `applyNoSkillsMCP` |
| `helpers.go` | argument readers and result helpers shared by the handlers |
| `hermes.go` | the `hermes-setup` sub-command |

`run()` is the one place everything is wired together, so a new service, tool
group, command or flag touches it as well as the file that owns the feature.
Five shared helpers give the handlers their shape — read them before changing
anything nearby:

| Helper | File | What it does |
|---|---|---|
| `getString(args, key, def)` | `helpers.go` | one string argument with a default |
| `firstString(args, keys…)` | `helpers.go` | first present key out of several aliases |
| `errResult(err)` | `helpers.go` | the MCP error result every handler returns |
| `serviceAction(sup, name, fn, starts)` | `tools.go` | wraps a service call into a tool handler; with `starts` (start, restart), a service not running afterwards is an error result naming its `lastError` |
| `scriptAction(sup, cmd, pre, dir, ext, label)` | `scripts.go` | the `run_python` / `run_node` handler: writes the agent's code to a script in the runtime's working directory and runs it via `RunOnce` (`run_command` calls `RunCommand` directly) |

The package only *looks* orthogonal to the packages below it. A change
that skips these helpers ends up with a second, subtly different way to do the
same thing — that is how the wiring grew to 1650 lines in one file before it
was split by concern.

## Non-negotiable invariants

1. **Stdlib only.** The single external dependency is `gopkg.in/yaml.v3`. Anything
   new must be vendored into `vendor/` and justified — the offline-reproducible
   build is a feature, not an accident. Reach for `net/http`, `encoding/json`,
   `os/exec`, `syscall` before considering a library.
2. **`vendor/` is checked in** and `go.mod` runs in vendor mode. Never `go get`
   during a build; never edit `vendor/` by hand.
3. **Job Objects are load-bearing.** On Windows every service, every one-shot
   command and the bridge smoke test is attached to a Job Object with
   `KILL_ON_JOB_CLOSE` (`internal/supervisor/job_windows.go`); only the
   short-lived `taskkill`, `reg query` and `git check-ignore`
   (`unignoredTokenFile`) helpers run without one. Off Windows
   the job is a no-op (`job_other.go`), the supervisor kills only the direct
   child, and the crash-reporter kill runs `pkill` instead of `taskkill`. This is
   what makes "no zombie ShaderCompileWorker" an OS guarantee rather than a hope.
   Removing or bypassing it silently breaks the product's core promise.
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
   `prepareMCPBridges` / `smokeTestMCP` for the house pattern: check the real
   answer (`initResponse` decodes each stdout line rather than match a
   substring: any JSON-RPC response with id 1 — `result` or `error`, no
   `method` — ends the wait, and passes only when `result.serverInfo` is an
   object; an error fails at once, named from its raw code and message by
   `initError`, any other result as "initialize answered without serverInfo";
   a request with id 1 from the bridge, or any other line, is read past), log
   loudly, name the usual suspects, keep going. Loudly means where the user
   looks: in window mode the log is a file and the console the agent's, so a
   line the user must see also goes to `alert` — stderr then, nil otherwise.
   One-line WARNs before the agent starts write to `alert` directly: the inert
   permissions in `run()`, k44's token file in `writeMCPConfigs`; so does
   `removeAgentMCPConfig` at exit, for an agent config directory it could not
   remove.
   `alertWarnings(logger, alert, logPath)` (`main.go`) wraps a logger for
   warnings that can come while the agent's terminal UI owns the console: the
   log gets every line in full, `alert` only a WARN's first line, plus
   " (the rest is in <logPath>)" when it has more. `run()` hands it to bridge
   preparation and to `prepareRuntimes`, which it starts with `go` and which
   warns on a failed `uv sync` / `npm install` with the cause and the output's
   last 20 lines. Past config load, three deliberate exceptions. The first,
   the MCP bind:
   `run()` binds `mcp.address` (`listenMCP`) right after config load (which
   already refuses an address that is not host:port with a port 1–65535), and a
   bind failure ends the launcher with exit 1 before it writes any file —
   keeping going there *is* the silent failure: an agent without the MCP server
   loses its tools and permission prompts, or talks to whoever holds the port.
   The second, the skill set: `setupSkills` ends the launcher with exit 1 when
   the embedded set does not load or `checkSkillDirs` rejects it — a broken
   build, whose skills must not vanish silently; `skills.enabled: false` starts
   without them, and a failed write into `skills.dir` only warns.
   The third, the agent's MCP config (k181): `run()` writes it with
   `writeAgentMCPConfig` before any service starts, and a failure ends the
   launcher with exit 1 — falling back to inline JSON would put `mcp.token`
   back on the agent's command line. The file lives for the whole run, so a
   restarted agent reuses it, and `run()` defers its removal.
   A serve failure after a successful bind is not fatal (`serveMCP`): one loud
   ERROR line in the launcher log, also on stderr when that log is a file
   (window mode), MCP marked not serving, and launcher, editor and agent keep
   running so the user can save; there is no automatic rebind.

## Build & verification gate

```bash
make windows    # dist/unreagent.exe — the real target (cross-compile)
make linux      # dist/unreagent — local test binary
make fmt vet    # gofmt -w . && go vet ./...
go test ./...   # tests live next to the code they cover
```

CI (`.github/workflows/ci.yml`) fails on: `gofmt -l cmd internal skills`
non-empty, the release-notes check of `Changes` via `scripts/changes-section.sh`,
`go vet ./...`, `go test ./... -race`, and both cross-compiles. The Windows
build there runs with `CGO_ENABLED=0` — the runner's gcc is Linux and rejects
`-mconsole`. The project has no CGO code, so this is safe; do not "fix" it by
enabling CGO.

Before claiming work is done: `make fmt vet && go test ./... && make windows`.
A Linux-only build proves nothing about the actual shipping target.

## Testing reality

Every package has tests next to the code it covers
(`git ls-files '*_test.go'` lists them) — except `skills/`, which holds only the
embed directive; its guards live in `internal/skills/skills_test.go`. There is
no mock harness and no fixture system — tests use the stdlib `testing` package,
real temp dirs, real short-lived processes, and `testing/fstest` for in-memory
file trees (`internal/skills`). New tests follow that style rather than
introducing a framework.

Windows-only code paths (Job Objects, registry engine detection) cannot be
exercised on the Linux dev box. Test the platform-independent logic and
guard the rest behind build tags; do not fake `syscall` to reach coverage.

## Where the product is documented

`README.md` is user-facing and thorough (setup, portability, in-editor MCP wiring,
troubleshooting, MCP tool table, CLI flags). It is the contract users read — a
behavior change that is not reflected there is half-finished. `CLAUDE.md` holds the
short repo rules; `docs/` holds design specs.
