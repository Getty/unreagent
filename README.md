# unreagent

[![ci](https://github.com/Getty/unreagent/actions/workflows/ci.yml/badge.svg)](https://github.com/Getty/unreagent/actions/workflows/ci.yml)

A small **launcher / orchestrator for Unreal Engine projects**: it starts and
supervises the Unreal Editor **and** an agent (e.g. Claude Code), and gives the
agent an **MCP server** to drive the editor, trigger builds, read logs, and run
Python/Node code in prepared runtimes.

A single `.exe` — **cross-compilable from Linux to Windows**. Stdlib Go only,
plus one vendored YAML parser; `vendor/` is checked in, so builds are
offline-reproducible.

## Architecture

```
+----------------------- unreagent.exe -----------------------+
|  Supervisor                 MCP server (HTTP :8765)        |
|  * ue    (Job Object)  <---  status / logs                 |
|  * agent (Job Object)        ue_start / stop / restart     |
|  * restart policy + backoff  run_command (compile, ...)    |
|  * ring-buffer logs          run_python / run_node         |
|       |                      approve (permissions)         |
|       v                          ^                         |
|  UnrealEditor.exe                |                         |
|   +- in-editor MCP plugin        |                         |
|      (e.g. UE LLM Toolkit,       |                         |
|       HTTP :3000)                |                         |
|            ^                     |                         |
|   Node bridge (stdio MCP)        |                         |
|            ^                     |                         |
|  Claude Code ----------+--------- unreagent-MCP             |
|                        +----------> in-editor MCP           |
|                          (both via --mcp-config)           |
+------------------------------------------------------------+
```

Two MCP servers serve the agent:

- **unreagent-MCP** (this launcher, HTTP) — *process* layer: start / stop /
  restart the editor, compile, logs, run_python / run_node, permissions.
- **In-editor MCP** (e.g. [UE LLM Toolkit](https://github.com/ColtonWilley/ue-llm-toolkit),
  running *inside* the engine) — *content* layer: blueprints, assets, levels,
  UE Python. Setup: [In-editor MCP setup](#in-editor-mcp-ue-llm-toolkit-setup-windows).

The launcher hands the agent both via `--mcp-config` (`mcp.extraServers` in
the config), so the agent does not have to wire anything up itself. Stdio
bridges are fully checked before the agent starts, with each step logged
loudly, instead of letting the agent run into a meaningless `-32000`
connect error: binary on PATH? bridge script present? `node_modules` there
(or run `npm install`)? does the bridge answer a real `initialize` handshake
(smoke test)? And once the in-editor server (`UNREAL_MCP_URL`) is reachable,
that's logged too — or a warning with the usual suspects if it isn't.

The same config can additionally be written to a file for **external clients**
(your own Claude session, Cursor, VS Code). Via `mcp.writeConfig` (a list of
`{path, format}`, format `mcp_json` or `vscode`) or the flag
`-write-mcp-config <path>`. Handy with `-no-agent`: the launcher writes the
`.mcp.json` and keeps UE + MCP running, you connect externally.

## Building

Requirement: Go (>= 1.19). Dependencies are vendored (`vendor/`); builds run
offline.

```bash
make windows   # -> dist/unreagent.exe  (cross-compile from Linux)
make linux     # -> dist/unreagent      (for local tests)
make all
```

Or directly:

```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o dist/unreagent.exe ./cmd/launcher
```

## Windows: metadata & "Unknown Publisher"

The `.exe` carries an icon + **version info** (CompanyName, ProductName,
description, copyright) and an `asInvoker` manifest (no UAC elevation
prompt). This fills `Properties -> Details` and the program name in dialogs.

The "Unknown Publisher" warning itself comes from the **signature**, not
from metadata. Two paths:

**A) Self-signed (in the repo, for your own team / known users)** — the
`.exe` is signed with a self-signed certificate from "conflict.industries
digital GmbH". Users import the public certificate once:

```powershell
powershell -ExecutionPolicy Bypass -File signing\import-cert.ps1   # as admin
```

After that: no publisher warning, the GmbH appears as publisher. Details
and trade-offs: [`signing/`](signing/). (The private key is **not** in the
repo.)

**B) CA certificate (for a wider audience)** — an OV / EV code-signing
cert does not need user-side import (EV gives immediate SmartScreen
reputation). With such a cert you also sign the `.exe` **from Linux**
(`osslsigncode`).

Change publisher / version: `cmd/launcher/versioninfo.json` -> `make resource`.

## Try it now (demo)

The release zip ships a ready-to-go demo `unreagent.yaml` that runs **without
UE and without an agent** (`ping` as a placeholder "editor"). Just unpack the
zip and start `unreagent.exe` — the MCP server will then run on
`http://127.0.0.1:8765/mcp`. Details: [`example/`](example/). In the console:
`status`, `logs`, `c hello`, `r ue`, `q`.

> The "LLM thing" (Claude Code) is not included — it is a CLI you install
> separately. The demo therefore leaves the agent off; to arm it, set
> `agent.enabled: true` (see below).

## Setup

The launcher is **dropped into the UE project** and the config is **committed**:

1. Put `unreagent.exe` (from the release) next to the `.uproject` — gitignored.
2. Rename `unreagent.example.yaml` -> **`unreagent.yaml`**, place it next to
   the `.uproject` and **commit it to the repo**. This file is portable.
3. Start `unreagent.exe` (double-click or console). Alternative config path
   via `-config <path>`.

`unreagent.yaml` contains **no machine-specific paths**. Editor, `compile` and
`package` are auto-derived. A minimal config is enough:

```yaml
agent:       { enabled: true, command: claude, claudeIntegration: true }
permissions: { enabled: true, mode: allow_all, deny: ["Bash(rm -rf *)"] }
runtimes:    { python: { enabled: true }, node: { enabled: true } }
mcp:         { enabled: true }
```

### Portability: paths are resolved at runtime

| Placeholder | Resolution |
|---|---|
| `${ENGINE}` | env `UE_ROOT` -> `engineRoot` (local.yaml) -> auto-detect of the Epic default paths |
| `${PROJECT}` / `${PROJECT_DIR}` / `${PROJECT_NAME}` | `.uproject` next to the config (auto-detect) |

Machine-specific overrides go in a **git-ignored `unreagent.local.yaml`**,
which is overlaid on top of `unreagent.yaml`:

```yaml
# unreagent.local.yaml  (DO NOT commit)
engineRoot: "D:/UE/UE_5.7"                 # if auto-detect fails
agent: { command: "C:/.../claude.cmd" }    # if claude is not on PATH
```

Recommended `.gitignore` entry in the UE project:

```
unreagent.exe
unreagent.local.yaml
```

### When the agent ends (`/quit` or crash)

In window mode the agent is the lead process — if it ends (e.g. `/quit`) and
is not restarted, the launcher prompts on the console (`agent.onExit: ask`,
the default):

```
Agent ended.
  [Enter] shut down everything (UE + launcher)
  [k]     keep editor running, drop to launcher console
  [r]     restart the agent
> _   (30s -> shut down everything)
```

Without a TTY (headless `-p`), it shuts down cleanly instead. Alternatives:
`onExit: shutdown` (always shut everything down immediately) or `onExit: leave`
(everything keeps running, warning only — manual Ctrl-C required).

## In-editor MCP: UE LLM Toolkit setup (Windows)

For the agent to work **inside the editor** (blueprints, assets, levels, UE
Python), the UE project needs the in-editor plugin. The short version:
**drop the plugin into the `Plugins/` folder — done.** No `.uproject` patching,
no enabling in the plugin browser.

### What the machine must have

| Component | What for | Note |
|---|---|---|
| Unreal Engine 5.7 | the editor itself | auto-detect via registry / default paths; otherwise `engineRoot` in `unreagent.local.yaml` |
| Visual Studio 2022 ("Game development with C++" workload) | **once** to build the plugin | the plugin ships as C++ source; UE asks "rebuild?" on first start |
| Node.js >= 18 | the plugin's stdio bridge | `npm install` for the bridge is done by the launcher automatically |
| Claude Code | the agent | `claude` on PATH; otherwise `agent.command` in local.yaml |
| `unreagent.exe` + `unreagent.yaml` | this launcher | see [Setup](#setup) |

### Steps

1. **Drop the plugin into the project** — copy or clone the repo/folder to
   `<Project>/Plugins/UELLMToolkit/`. The `.uplugin` has
   `EnabledByDefault: true` and activates its own engine dependencies
   (EditorScriptingUtilities, Niagara, ControlRig, IKRig, ...) — **nothing**
   to change on the `.uproject`.
2. **First editor start** — UE reports "Missing Modules ... rebuild?" -> Yes.
   The build needs Visual Studio (once; afterwards the binaries live in the
   plugin). Alternatively ahead of time via the launcher: console -> `c compile`.
3. **Done.** The plugin's HTTP server starts with the editor automatically on
   port `3000`. The launcher forwards the bridge via `--mcp-config` to the
   agent — for that, in `unreagent.yaml`:

   ```yaml
   mcp:
     enabled: true
     extraServers:
       ue-llm-toolkit:
         command: node
         args: ["${PROJECT_DIR}/Plugins/UELLMToolkit/Resources/mcp-bridge/index.js"]
         env:
           UNREAL_MCP_URL: "http://127.0.0.1:3000"
   ```

4. *Optional:* For `unreal_execute_script` with Python scripts, enable the UE
   plugin **"Python Editor Script Plugin"** (Edit -> Plugins) — the other
   tools don't need it.

What the launcher does **automatically** on startup (see above): install
bridge `node_modules`, check binary / script, `initialize` smoke test, wait
for port 3000 — every step with a clear message in `unreagent.log`.

### Access from another machine

The UE HTTP server binds to `localhost` by default. If a remote Claude session
should reach the in-editor server (port 3000) directly:

```ini
; Config/DefaultEngine.ini of the UE project
[HTTPServer.Listeners]
DefaultBindAddress=any
```

Plus a Windows firewall rule for port 3000 — and only in trusted networks,
the server controls the entire editor. On the other side `UNREAL_MCP_URL`
then points to `http://<windows-ip>:3000`.

### Troubleshooting: "Failed to reconnect: -32000"

`-32000` from Claude Code just means "bridge process immediately gone again"
— the real cause is in `unreagent.log` (check chain) or in Claude Code's MCP
logs (`%LOCALAPPDATA%\claude-cli-nodejs\Cache\<project>\mcp-logs-*`). Usual
suspects, in this order:

1. **Bridge path wrong** (`Cannot find module ...`) — check the path in
   `extraServers` against the real plugin layout.
2. **Node missing** or not on PATH in the agent's session.
3. **`node_modules` missing** — this no longer happens, the launcher installs
   them.
4. **Editor (not yet) reachable** — port 3000 only appears once the editor
   is fully up; the launcher logs once it is.

## Configuration (quick reference)

| Section | Purpose |
|---|---|
| `agent` | agent command + args; `claudeIntegration` injects `--mcp-config` (+ permission tool); `window` (on by default) = interactive in the foreground (inherits console / TTY, launcher logs -> `unreagent.log`); on Windows, `claudeIntegration` additionally sets `CLAUDE_CODE_USE_POWERSHELL_TOOL=1` (`powershellTool: false` to disable) |
| `permissions` | permission layer: `allow_all` / `allowlist` / `deny_all`, plus `allow` / `deny` rules |
| `runtimes` | `python` (via `uv run`) and `node` for `run_python` / `run_node` |
| `mcp` | MCP server on/off, `address` (default `127.0.0.1:8765`), optional `token` (Bearer auth — auto-set by `hermes-setup`) |
| `unreal` | *optional* — editor args, restart policy (`never` / `on-failure` / `always`) |
| `commands` | *optional* — your own one-shot commands (compile / package are built-in) |
| `engineRoot` | *usually only in local.yaml* — engine path override |

### No process zombies (Windows)

Every running instance is attached to a **Windows Job Object** with
`KILL_ON_JOB_CLOSE`. If the handle closes — by clean stop, restart,
**or crash / hard-kill of the launcher** — the operating system terminates
the **entire process tree** (including ShaderCompileWorker etc.). OS-enforced,
no external dependency. On Linux (development) only the direct child is
killed.

### Crashes & recovery (unattended)

Two UE dialogs otherwise block agent-driven operation: the **crash reporter**
("Send and Restart / Close") and the **recovery prompt** on restart
("not closed cleanly, restore?"). The launcher therefore starts the editor
with **`-unattended`** — this suppresses **both** in UE 5.7 code (crash
dialog and both recovery systems: PackageAutoSaver *and* disaster recovery)
and discards the unclean state instead of asking.

| Option (`unreal:`) | Default | Effect |
|---|---|---|
| `unattended` | `true` | appends `-unattended` — no crash dialog, no recovery prompt |
| `killCrashReporter` | `true` | kills `CrashReportClientEditor.exe` before every (re)start (belt and suspenders) |
| `cleanOnRestart` | `false` | cleans `Saved/Autosaves/PackageRestoreData.json` + `Saved/Crashes/*` |

If a **human** is working interactively in the editor in parallel, set
`unattended: false`. In addition (so the reporter never even sends/starts)
in the project:

```ini
; Config/DefaultEngine.ini
[CrashReportClient]
bAgreeToCrashUpload=false
bImplicitSend=false

[/Script/DisasterRecoveryClient.DisasterRecoverClientConfig]
bIsEnabled=false
```

### Permission layer

With `permissions.enabled` + `agent.claudeIntegration` the launcher starts
the agent with `--permission-prompt-tool mcp__unreagent__approve`. Every
permission prompt from Claude Code then goes through the `approve` tool,
which decides per the configured policy. `mode: "allow_all"` = "auto-approve
everything" (with `deny` exceptions like `Bash(rm -rf *)`).

> Note: the exact **input** schema of `--permission-prompt-tool` is not
> officially documented by Anthropic. The `approve` handler reads the tool
> fields defensively (`tool_name` / `toolName` / `name`,
> `tool_input` / `input` / `arguments`). If a Claude Code update changes the
> format, only that one handler in `cmd/launcher/main.go` needs adapting.

### Notifying the user: taskbar flash (Windows)

When Claude Code stops a turn — finished working, or about to ask a
question — the terminal is often buried under other windows. On Windows,
`unreagent` can flash its console entry in the taskbar so the user notices.

**No setup needed.** When `agent.claudeIntegration: true` (the default in
`unreagent.example.yaml`), the launcher already passes the right flag to
Claude Code:

```text
claude --mcp-config '<…>' --settings '{"hooks":{"Stop":[{"hooks":[
  {"type":"command","command":"\"<unreagent.exe>\" flash"}
]}]}}'
```

The `--settings` JSON is built by the launcher from
`os.Executable()` — so the hook command is a full, quoted path to
the running `unreagent.exe` and works even when `unreagent` is not on
the agent's `PATH`. It fires on every turn end, which is what we want:
the user gets feedback every time the agent yields, whether it's a
question, a decision point, or just "done".

What happens on `Stop`:

1. Claude Code spawns the hook command — `<unreagent.exe> flash`.
2. `unreagent flash` POSTs a `tools/call flash_task` request to the
   running launcher (URL from `UNREAGENT_MCP_URL`, which the launcher
   exports to the agent's env — the hook subprocess inherits it).
3. The launcher's `flash_task` MCP tool calls
   `FlashWindowEx(FLASHW_TRAY | FLASHW_TIMERNOFG)` on the console HWND.
4. The taskbar entry flashes until the user clicks it — one call is
   enough, the flash self-stops on activation.

The agent can also call `flash_task` **explicitly** via MCP when it
knows it's about to yield to the user (a question, a decision, a
completed piece of work the user should review). On non-Windows, the
call is a no-op.

To turn it off, disable `agent.claudeIntegration` (or wrap your own
`claude` invocation via `agent.command` + `agent.args` and skip the
auto-attached `--settings`). Manual test from a second shell:

```bash
unreagent flash   # should start the flash; "[flash] console window flashing" appears in unreagent.log
```

### Hermes Agent (alternative agent)

[Hermes Agent](https://hermes-agent.nousresearch.com/docs/) (Nous Research)
is an autonomous agent with its own MCP client — useful as an alternative to
Claude Code. `unreagent` plugs in by registering the same MCP server
(`ue_start` / `ue_stop` / `logs` / `run_command` / `run_python` / …) under
Hermes' `mcp_servers:` block. The launcher does **not** auto-merge on
normal runs; you opt in once with a sub-command:

```yaml
# unreagent.yaml
agent:
  hermes:
    enabled: true
```

```bash
unreagent hermes-setup      # merges MCP entry into ~/.hermes/config.yaml
```

What `hermes-setup` does, in order:

1. Resolves the target config: explicit `agent.hermes.configPath` →
   `$HERMES_HOME/config.yaml` → `~/.hermes/config.yaml`.
2. If `mcp.token` is empty, generates 32 random bytes (hex) and writes
   `mcp.token:` into `unreagent.local.yaml` (git-ignored). The launcher
   reads it back on the next start, so the token survives restarts.
3. Backs up an existing Hermes config to `<path>.bak` (once; never
   overwrites a previous backup).
4. Sets `mcp_servers.unreagent: { url, headers: { Authorization: Bearer } }`
   in the Hermes config. A second run **overwrites** the entry rather than
   duplicating it — the operation is idempotent.
5. Writes the file atomically (temp + rename) so a crash mid-write cannot
   leave a half-written config.

The MCP server enforces `Authorization: Bearer <token>` whenever
`mcp.token` is set — including for the embedded Claude Code agent, which
gets the matching `headers:` block in its `--mcp-config` automatically.
The token check uses `crypto/subtle.ConstantTimeCompare`; the empty-token
default (the historic behaviour) keeps the server open on 127.0.0.1.

Restart Hermes after running `hermes-setup` so it picks up the new entry.

### Runtimes for the agent

`run_python` executes code via `uv run python` — uv builds / syncs the venv
automatically from `pyproject.toml` / `requirements` and installs the right
Python version on demand. `run_node` runs JS in the project context. The
agent does **not** have to analyse or set up the environment itself; the
instructions are in the MCP tool description and are therefore automatically
in context.

## MCP tools

| Tool | Function |
|---|---|
| `status` | process status + available commands |
| `ue_start` / `ue_stop` / `ue_restart` | editor lifecycle |
| `run_command` | run a preconfigured command (compile, package) |
| `logs` | last output lines of a service |
| `run_python` / `run_node` | run code in a prepared environment |
| `read_file` / `list_dir` / `write_file` / `edit_file` | file access on the project (only with `files.enabled` / `-files`, scoped to `root`) |
| `approve` | permission prompt tool for Claude Code |
| `flash_task` | flashes the unreagent terminal in the Windows taskbar (no-op on other platforms) |

## CLI flags

| Flag | Effect |
|---|---|
| `-config <path>` | alternative path to `unreagent.yaml` |
| `-no-agent` | **do not** start the agent — only UE + MCP server. An external agent (your own Claude Code session) then connects to the MCP server. |
| `-files` | enable the file tools (`read_file` / `write_file` / `list_dir` / `edit_file`) even if off in the config. |
| `-write-mcp-config <path>` | additionally write the assembled MCP config as `.mcp.json` to `<path>` (for external clients). |

## Sub-commands

| Sub-command | Purpose |
|---|---|
| `unreagent flash` | POSTs a `tools/call flash_task` to the running launcher so the console window starts flashing in the taskbar. Used by the Claude Code Stop hook; also runnable by hand. |
| `unreagent hermes-setup` | One-shot merge of the unreagent MCP server into Hermes Agent's `config.yaml` (idempotent; auto-generates `mcp.token` if missing). See "Hermes Agent" above. |

### How the agent runs (three modes)

Claude Code is an interactive TUI and needs a TTY — as a raw background
subprocess it won't start. Hence three ways:

1. **Interactive in the foreground** (`agent.window`, **on by default**) — the
   agent inherits the real console (TTY) and runs as an interactive TUI in
   the window you started `unreagent` in. UE + MCP run in the background,
   launcher logs go to `unreagent.log` (next to the project) so the agent
   has the window to itself. Disable with `window: false`; off in `-p` mode.
2. **Headless / task** (`agent.args: ["-p", "<task>"]`) — one-shot task
   without a window, then the agent exits.
3. **External** (`-no-agent`) — the launcher does not start an agent; you
   connect your own Claude session with the `.mcp.json` / the MCP server.

### Headless / external agent

With `-no-agent` everything runs without an embedded agent — UE + MCP server.
That's how you hook up your own Claude Code to work on the project "without
anyone being present locally":

```bash
unreagent.exe -no-agent -files          # UE + MCP + file tools, no embedded agent
# in your Claude Code session:
claude --mcp-config '{"mcpServers":{"unreagent":{"type":"http","url":"http://127.0.0.1:8765/mcp"}}}'
```

For access from another machine set `mcp.address` to `0.0.0.0:8765` (only
in trusted networks — the server then has file-write access).

## Manual control (stdin)

`status` · `r [name]` (restart) · `start <name>` · `stop <name>` ·
`c <name>` (run command) · `q` (quit)

## Manual MCP test

```bash
curl -s -X POST http://127.0.0.1:8765/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```
