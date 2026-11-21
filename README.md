<p align="center">
  <img src="assets/github-social.png" alt="unreagent — Unreal Engine Launcher &amp; MCP Orchestrator" width="600">
</p>

# unreagent

[![ci](https://github.com/Getty/unreagent/actions/workflows/ci.yml/badge.svg)](https://github.com/Getty/unreagent/actions/workflows/ci.yml)

A small **launcher / orchestrator for Unreal Engine projects**: it starts and
supervises the Unreal Editor **and** an agent (e.g. Claude Code), and gives the
agent an **MCP server** to drive the editor, trigger builds, read logs, and run
Python/Node code in prepared runtimes.

A single `.exe` — **cross-compilable from Linux to Windows**. Stdlib Go only,
plus one vendored YAML parser; `vendor/` is checked in, so builds are
offline-reproducible.

Full working example — a real UE project wired to Claude Code with both MCP
servers running: [unreagent-example](https://github.com/Getty/unreagent-example).

## Architecture

<p align="center">
  <img src="assets/editor-mcp-flow.png" alt="Architecture: unreagent.exe contains the supervisor (UE and agent in Job Objects, restart policy, ring-buffer logs) and the MCP server on HTTP :8765. The supervisor starts UnrealEditor.exe, which hosts an in-editor MCP plugin (for example UE LLM Toolkit, HTTP :3000). Claude Code reaches the in-editor plugin through a Node stdio bridge and the unreagent MCP server directly, both wired via --mcp-config." width="800">
</p>

The graphic shows the UE LLM Toolkit path (Node bridge). Since UE 5.8 the
editor's built-in Unreal MCP can take the place of the plugin and the bridge:
the agent then talks to it directly over HTTP — see
[In-editor MCP](#in-editor-mcp-unreal-mcp-or-ue-llm-toolkit-windows).

<details>
<summary>Text version of the diagram (for terminals and the release archive)</summary>

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

</details>

Two MCP servers serve the agent:

- **unreagent-MCP** (this launcher, HTTP) — *process* layer: start / stop /
  restart the editor, compile, logs, run_python / run_node, permissions.
- **In-editor MCP** (running *inside* the engine) — *content* layer:
  blueprints, assets, levels, UE Python. Two options:
  [Unreal MCP](https://dev.epicgames.com/documentation/unreal-engine/unreal-mcp-in-unreal-editor)
  (built into UE 5.8+, experimental, plain HTTP, no bridge) or the
  [UE LLM Toolkit](https://github.com/ColtonWilley/ue-llm-toolkit) plugin
  (Node stdio bridge). Both can run side by side. Setup:
  [In-editor MCP](#in-editor-mcp-unreal-mcp-or-ue-llm-toolkit-windows).

The launcher hands the agent both via `--mcp-config` (`mcp.extraServers` in
the config), so the agent does not have to wire anything up itself. Stdio
bridges are fully checked before the agent starts, with each step logged
loudly, instead of letting the agent run into a meaningless `-32000`
connect error: binary on PATH? bridge script present? `node_modules` there
(or run `npm install`, cut off after 5 minutes; a failed install is logged as
a warning and skips the smoke test, but the bridge stays in the agent's
config)? does the bridge answer a real `initialize` handshake
(smoke test)? And once the in-editor server (`UNREAL_MCP_URL`) is reachable,
that's logged too — or a warning with the usual suspects if it isn't. These
checks run only for stdio entries (those with a `command`); an HTTP entry such
as Unreal MCP is passed through unchecked.

The same config can additionally be written to a file for **external clients**
(your own Claude session, Cursor, VS Code). Via `mcp.writeConfig` (a list of
`{path, format}`, format `mcp_json` (the default) or `vscode`; any other
format is a load error —
`mcp.writeConfig[0].format invalid: "json" (allowed: mcp_json, vscode)`; a
relative `path` is resolved against the agent working directory, normally the
project directory; the overlay replaces the list as a whole) or the flag
`-write-mcp-config <path>`. Handy with `-no-agent`: the launcher writes the
`.mcp.json` and keeps UE + MCP running, you connect externally.

## Building

Requirement: Go (>= 1.26). Dependencies are vendored (`vendor/`); builds run
offline. (An older Go would first try to download a 1.26 toolchain.)

Binary assets (the icon and `cmd/launcher/resource_windows_amd64.syso`) live
in **Git LFS**. Run `git lfs install` once, then clone / pull with LFS
enabled. Without it the `.syso` is just a small pointer file and the Windows
build fails.

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

| | [`example/`](example/) | [unreagent-example](https://github.com/Getty/unreagent-example) |
|---|---|---|
| UE editor | none (`ping` as a stand-in) | real UE 5.7 project |
| Agent | off by default | Claude Code |
| In-editor MCP | not wired | UE LLM Toolkit (submodule) |
| Purpose | try the MCP server in isolation, no downloads | full reference setup, both MCP servers wired |

Use `example/` to poke at the MCP server without installing UE or an agent.
For the real thing — a UE project with Claude Code driving both
unreagent-MCP and the in-editor MCP together — clone the standalone
[unreagent-example](https://github.com/Getty/unreagent-example) repo:

```bash
git clone --recursive https://github.com/Getty/unreagent-example
```

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
| `${ENGINE}` | env `UE_ROOT` -> `engineRoot` (local.yaml) -> the `.uproject`'s `EngineAssociation` (Launcher version, or a registered source build) -> the highest installed version found via the registry / Epic default paths. A relative `UE_ROOT` / `engineRoot` is resolved against the **working directory** and made absolute |
| `${PROJECT}` | `unreal.project` if set (absolute, or relative to the **config directory**; the file must exist), otherwise the first `.uproject` next to the config |
| `${PROJECT_DIR}` / `${PROJECT_NAME}` | directory of that `.uproject` / its file name without the extension |

A relative `-config <path>` is resolved against the working directory and made
absolute first, so every placeholder value ends up an absolute path.

**Where placeholders are replaced.** In `unreal.editor` and `unreal.args`;
`agent.command`, `agent.args`, the `agent.env` values, `agent.workdir` and
`agent.hermes.configPath`; `permissions.allow` / `permissions.deny`; the
runtimes' executables and project directories (`runtimes.python.uv` /
`project`, `runtimes.node.node` / `npm` / `project`); `files.root`; every
`commands.<name>` `command`, `args` and `dir`; every string inside
`mcp.extraServers`; and the `mcp.writeConfig` paths. There is no escape
syntax: a shell-style `${PROJECT}` inside a `Bash(...)` rule or in `agent.args`
is replaced as well.

**Where they are not.** `unreal.project` (the placeholders are derived from it;
a `${...}` there is a load error), `engineRoot`, `mcp.address`, `mcp.token`,
names and keys (`agent.hermes.serverName`, the `agent.env` names, the command
names), descriptions and every enum stay literal.

**An unresolved placeholder is a load error.** If the engine or the `.uproject`
cannot be found and a field the launcher will actually use still contains the
placeholder, loading stops instead of continuing with a path such as
`/Engine/Binaries/Win64/UnrealEditor.exe` or an allow rule `Edit(/*)`:

```
${ENGINE} is unresolved (engine not found — set UE_ROOT or engineRoot in unreagent.local.yaml) but used by unreal.editor
${PROJECT} is unresolved (no .uproject found next to the config — set unreal.project) but used by commands.cook.args[1]
```

A field counts as used when its section is enabled (`agent.*` needs
`agent.enabled`, `agent.hermes.configPath` needs `agent.hermes.enabled`,
`permissions.*` needs `permissions.enabled`, a runtime needs its `enabled`
(`runtimes.node.npm` also counts when an extra server is started with `node`),
`mcp.extraServers` and `mcp.writeConfig` need `mcp.enabled`, `files.root` needs
`files.enabled`). `unreal.*`, `agent.workdir` and your own `commands` always count.
Two exceptions never fail the load:

- The built-in `compile` and `package` commands are **left out with a warning**
  when the engine or the project does not resolve
  (`WARN built-in command compile not offered: it needs the engine and the .uproject`);
  they then do not appear in `status` or `run_command`. A command of that name
  you define yourself is always kept and is checked like any other.
- With `files.enabled: false`, an unresolved `files.root` is cleared instead of
  rejected, and `-files` is then refused at startup
  (`WARN file tools disabled: files.root names a placeholder that is unresolved`).
  Set `unreal.project` or an explicit `files.root`.

Without an engine and with the default `unreal.editor`, the launcher therefore
stops at load, and so does `unreagent hermes-setup`, although it does not start
the editor. Point `UE_ROOT` / `engineRoot` at an installation, or set
`unreal.editor` to a literal path.

Machine-specific overrides go in a **git-ignored `unreagent.local.yaml`**,
which is overlaid on top of `unreagent.yaml`:

```yaml
# unreagent.local.yaml  (DO NOT commit)
engineRoot: "D:/UE/UE_5.7"                 # if auto-detect fails (path is an example)
agent: { command: "C:/.../claude.cmd" }    # if claude is not on PATH
```

The overlay is applied by YAML decode, **not** a deep merge: a scalar replaces
the base value only when the local file sets it; a **list** replaces the base
list as a whole (`unreal.args`, `agent.args`, `permissions.allow` / `deny`,
`mcp.writeConfig`); a **map** merges by key, and each entry you give replaces
the base entry whole (`agent.env`, `commands`, `mcp.extraServers`). So
`agent.env: { HOME: "C:/Path/To/Home" }` adds or changes `HOME` and keeps the
other variables of the base file, while a command defined in both files comes
out with the local definition only — you cannot override just its `args`.

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

## In-editor MCP: Unreal MCP or UE LLM Toolkit (Windows)

For the agent to work **inside the editor** (blueprints, assets, levels, UE
Python), the editor needs an in-editor MCP server. Two options, which can also
run side by side (Toolkit on `:3000`, Unreal MCP on `:8000`):

| | Unreal MCP (built in) | UE LLM Toolkit (plugin) |
|---|---|---|
| Engine | UE 5.8 or newer | UE 5.7 (the reference project) |
| Status | experimental (Epic) | third-party plugin |
| Transport | Streamable HTTP / SSE, `http://127.0.0.1:8000/mcp` | HTTP `:3000` behind a Node stdio bridge |
| Needs | nothing to install | plugin in `Plugins/`, Visual Studio build once, Node.js |

The short version for the Toolkit: **drop the plugin into the `Plugins/`
folder — done.** No `.uproject` patching, no enabling in the plugin browser.
For Unreal MCP: enable two plugins and add one `extraServers` entry.

### What the machine must have

| Component | What for | Note |
|---|---|---|
| Unreal Engine 5.7 or newer | the editor itself | auto-detect (see the `${ENGINE}` row above); otherwise `engineRoot` in `unreagent.local.yaml`. The reference project ([unreagent-example](https://github.com/Getty/unreagent-example)) runs on 5.7; Unreal MCP needs 5.8+. unreagent itself has not been tested against 5.8 |
| Visual Studio 2022 17.14+ or Visual Studio 2026 18.0+ ("Game development with C++" workload) | **only for the Toolkit**: builds the plugin once | UE 5.8 lists VS 2022 17.14+ and VS 2026 18.0+ (2026 recommended); the plugin ships as C++ source, UE asks "rebuild?" on first start. Not needed for Unreal MCP |
| Node.js >= 18 | the Toolkit's stdio bridge, and `run_node` | `npm install` for the bridge is done by the launcher automatically. Not needed for Unreal MCP |
| Claude Code | the agent | `claude` on PATH; otherwise `agent.command` in local.yaml. Install: [setup docs](https://code.claude.com/docs/en/setup) (native installer; on Windows PowerShell `irm https://claude.ai/install.ps1 \| iex`) |
| `unreagent.exe` + `unreagent.yaml` | this launcher | see [Setup](#setup) |

### Option A: Unreal MCP (UE 5.8+, built in, experimental)

Source: [Unreal MCP in Unreal Editor](https://dev.epicgames.com/documentation/unreal-engine/unreal-mcp-in-unreal-editor).
No Node bridge, no Visual Studio build, no plugin download.

1. **Enable the plugins** (Edit -> Plugins): "Unreal MCP" (identifier
   `ModelContextProtocol`, enabled by default) and "All Toolsets". Both depend
   on "Toolset Registry", which is enabled automatically. Restart the editor.
2. **Server defaults** — "Auto Start Server" is on by default and binds
   `http://127.0.0.1:8000/mcp`. Port and path live in Editor Preferences ->
   Agentic Workflows -> Model Context Protocol. On the command line
   `-ModelContextProtocolPort=<port>` overrides the port and
   `-ModelContextProtocolStartServer` forces the server to start.
3. **Wire it into unreagent** — `mcp.extraServers` entries are passed through
   to the agent in `.mcp.json` format, so an HTTP entry works as is:

   ```yaml
   mcp:
     enabled: true
     extraServers:
       unreal-mcp:
         type: http
         url: "http://127.0.0.1:8000/mcp"
   ```

   `extraServers` is a **map keyed by server name**: an `extraServers` block in
   `unreagent.local.yaml` adds servers and replaces a same-named server's
   whole definition, while the other servers from `unreagent.yaml` stay.
   (Lists such as `unreal.args` or `permissions.deny` are replaced as a whole.)

4. *Optional:* a different port. `unreal.args` **replaces** the default
   `["-stdout", "-FullStdOutLogOutput"]` (the `.uproject` is always prepended),
   so give the full list and match the URL:

   ```yaml
   unreal:
     args: ["-stdout", "-FullStdOutLogOutput", "-ModelContextProtocolPort=8010"]
   mcp:
     extraServers:
       unreal-mcp:
         type: http
         url: "http://127.0.0.1:8010/mcp"
   ```

Things to know:

- **No bridge checks.** The launcher's startup checks (binary, `node_modules`,
  `initialize` smoke test, reachability log) run only for stdio entries with a
  `command`. An HTTP entry gets none, and nothing in `unreagent.log` tells you
  whether the server is up.
- **Start order.** Epic's docs say to start the agent after the editor's
  server is up; unreagent starts editor and agent together. If Claude Code
  reports the `unreal-mcp` server as failed, wait until the editor has
  finished loading and reconnect with `/mcp`.
- **No authentication.** Unreal MCP is loopback-only by default and checks the
  `Origin` header against localhost / `127.0.0.1` / `[::1]` plus any extras you
  configure. See [Access from another machine](#access-from-another-machine)
  before changing the bind address.
- **One call at a time.** Tool calls run sequentially on the game thread.
  By default `tools/list` returns only four meta-tools (`list_toolsets`,
  `describe_toolsets`, `describe_tools`, `call_tool`) instead of every tool
  ("tool search").

### Option B: UE LLM Toolkit

#### Steps

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
for port 3000 — every step with a clear message in `unreagent.log`. Ctrl-C
while the bridges are being prepared (a running `npm install` included) exits
at once; the editor is not started.

### Access from another machine

The UE HTTP server binds to `localhost` by default. If a remote Claude session
should reach the in-editor server (port 3000 for the Toolkit) directly:

```ini
; Config/DefaultEngine.ini of the UE project
[HTTPServer.Listeners]
DefaultBindAddress=any
```

Plus a Windows firewall rule for port 3000 — and only in trusted networks,
the server controls the entire editor. On the other side `UNREAL_MCP_URL`
then points to `http://<windows-ip>:3000`.

> **Warning:** `DefaultBindAddress=any` also exposes **Unreal MCP** (port 8000
> by default) to the network. Unreal MCP has no authentication, and anyone who
> can reach the port can drive the whole editor. Open it only in trusted
> networks, behind a firewall rule scoped to the machines that need it.

### Troubleshooting: "Failed to reconnect: -32000"

`-32000` from Claude Code just means "bridge process immediately gone again"
— the real cause is in `unreagent.log` (check chain) or in Claude Code's MCP
logs (`%LOCALAPPDATA%\claude-cli-nodejs\Cache\<project>\mcp-logs-*`). Usual
suspects, in this order:

1. **Bridge path wrong** (`Cannot find module ...`) — check the path in
   `extraServers` against the real plugin layout.
2. **Node missing** or not on PATH in the agent's session.
3. **`node_modules` missing, or the install was cut short.** The launcher
   runs `npm install` itself, but gives up after 5 minutes (the whole install,
   not a single request) — slow link, unreachable registry or proxy. The
   launcher then logs a warning (in `unreagent.log` in window mode, on the
   console otherwise) and skips the smoke test, but the bridge stays in the
   agent's config and the editor still starts, so Claude Code reports the
   server as failed. An install cut short (the timeout, or Ctrl-C) can leave
   a partial `node_modules`, and because the directory exists the next start
   skips the install. Delete the bridge's `node_modules` (the log names the
   directory) and start again once the network works, or run `npm install`
   there by hand.
4. **Editor (not yet) reachable** — port 3000 only appears once the editor
   is fully up; the launcher logs once it is.

For **Unreal MCP** the chain is shorter, because there is no bridge process:
if Claude Code shows the server as failed, the editor was most likely not up
yet when the agent started (reconnect with `/mcp`), the "Unreal MCP" /
"All Toolsets" plugins are not enabled, or the port or path in the
`extraServers` URL does not match Editor Preferences -> Agentic Workflows ->
Model Context Protocol (or `-ModelContextProtocolPort`). Since the launcher
runs no check on HTTP entries, `unreagent.log` will not say which.

## Configuration (quick reference)

| Section | Purpose |
|---|---|
| `agent` | agent command + args; `claudeIntegration` injects `--mcp-config` (+ permission tool); `window` (on by default) = interactive in the foreground (inherits console / TTY, launcher logs -> `unreagent.log`); on Windows, `claudeIntegration` additionally sets `CLAUDE_CODE_USE_POWERSHELL_TOOL=1` (`powershellTool: false` to disable); `startDelaySeconds` (0 = default 5) and `restartDelaySeconds` (0 = default 3) — never below 0 (with `agent.enabled`, a negative value is a load error, so a crashing agent cannot restart in a tight loop); `args` is a list (replaced as a whole by the overlay), `env` a map (merged by key by the overlay) |
| `permissions` | permission layer: `allow_all` / `allowlist` (default if unset) / `deny_all`, plus `allow` / `deny` rules — `Bash(...)` rules are matched per shell segment, see Permission layer below; `allow` / `deny` are lists (the overlay replaces them as a whole) and take the path placeholders |
| `runtimes` | `python` (via `uv run`) and `node` for `run_python` / `run_node` |
| `mcp` | MCP server on/off, `address` (default `127.0.0.1:8765`), optional `token` (Bearer auth — auto-set by `hermes-setup`); every request needs `Content-Type: application/json` and, if present, a loopback `Origin` — see MCP server hardening below; `extraServers` (map of in-editor / extra MCP servers in `.mcp.json` format, stdio or `type: http`) — `unreagent.local.yaml` adds servers by name and replaces a same-named one's whole definition; the others stay |
| `unreal` | *optional* — `project` (absolute, or relative to the config directory; must exist, no placeholders; default: the `.uproject` next to the config), editor `args` (unset or `~` = `-stdout -FullStdOutLogOutput`; `[]` = none; a list replaces the base list in the overlay), restart policy (`never` / `on-failure` / `always`), `restartDelaySeconds` (0 = default 3; negative is a load error) |
| `commands` | *optional* — your own one-shot commands (map keyed by name; the overlay merges by key and replaces a same-named command whole). `compile` / `package` are built-in and offered only when both the engine and the project resolve; defining your own `compile` / `package` replaces the built-in |
| `engineRoot` | *usually only in local.yaml* — engine path override; relative is resolved against the working directory |

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
and discards the unclean state instead of asking. This was verified against
the UE 5.7 source and **has not been re-checked for UE 5.8 or newer**.

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
the agent with `--permission-prompt-tool mcp__unreagent__approve` — but
**only in headless mode** (`agent.args` contains `-p`/`--print`, or
`agent.window: false`). Every permission prompt from Claude Code then goes
through the `approve` tool, which decides per the configured policy.

> **Interactive window mode (the default) does not enforce the policy this
> way.** With `mode: allow_all` the launcher passes
> `--dangerously-skip-permissions` instead (Claude Code's own prompting is
> skipped entirely); with any other mode no flag is added and Claude Code
> prompts the user directly in the window, same as without unreagent. To have
> the policy actually enforced, run headless or drive Claude Code externally
> against the MCP server (`-no-agent`).

Modes: `allow_all` = auto-approve everything, `deny` rules still apply (e.g.
`Bash(rm -rf *)`); `allowlist` (the default when `mode` is unset) = deny
everything except what an `allow` rule matches — the only mode that fails
closed by construction; `deny_all` = reject everything.

`Bash(...)` rules are matched **per shell segment**, not against the whole
command line: the command is split on `;`, `&&`, `||`, `|`, newlines, on
`$(...)`/backtick bodies, `{ ... }` groups, and on the script handed to a
nested `bash -c`/`sh -c`. A **deny** rule matches if *any* segment matches
(so `cd /tmp && rm -rf /` cannot walk past a deny rule for `Bash(rm -rf *)`);
an **allow** rule requires *every* segment to be covered by some allow rule
(so `git status | head` needs both `Bash(git *)` and `Bash(head *)`, and
`cd /proj && git status` needs both `Bash(cd *)` and `Bash(git *)`). Deny
matching additionally normalizes whitespace/quotes/escapes, reduces a
path-qualified command to its base name (`/bin/rm` -> `rm`), and looks
through wrapper commands (`sudo`, `env`, `eval`, `xargs`, ...) at what they
wrap. This is a **heuristic, not a shell parser** — it cannot follow variable
indirection, aliases, or data piped into an interpreter (`python -c ...`). A
deny list under `allow_all` is a guard rail, not a security boundary.

Bracket rules also work on non-Bash tools, matched against the tool's
path-like input — `Write(/etc/*)`, `Read(/secrets/*)`,
`mcp__unreagent__write_file(/etc/*)`. A rule whose tool carries no evaluable
input field fails closed: a deny rule still blocks, an allow rule still does
not grant.

**Windows:** `claudeIntegration` sets `CLAUDE_CODE_USE_POWERSHELL_TOOL=1` by
default (see the `agent` row above), so Claude Code's shell tool is named
`PowerShell`, not `Bash` — write rules as `PowerShell(...)` there (e.g.
`deny: ["PowerShell(Remove-Item *)"]`). The segment splitter above is
POSIX-shaped; there is no PowerShell-specific tokenizer yet.

> Note: the exact **input** schema of `--permission-prompt-tool` is not
> officially documented by Anthropic. The `approve` handler reads the tool
> fields defensively (`tool_name` / `toolName` / `name` / `tool`,
> `tool_input` / `input` / `arguments` / `parameters`). If a Claude Code update
> changes the format, only that one handler in `cmd/launcher/tools.go` needs
> adapting.
>
> A request the handler cannot read is **denied**, not handed to the policy —
> so not even `allow_all` lets it through. Unreadable means: none of the name
> fields holds a non-empty string, or none of the input fields holds an
> object. The denial message names the fields it looked for and the keys it
> got (or the name field that holds something other than a string), and the
> launcher logs it as an `[approve]` line on its console (the
> permission tool runs only in headless mode, where the launcher logs to the
> console, not to `unreagent.log`). If every prompt suddenly ends in
> "unreadable permission request", a Claude Code update most likely changed
> the format: the logged keys show the new field names to add to the alias
> lists in that handler.

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

1. Loads the unreagent config. This needs an engine like a normal start does
   (see "Portability" above): without one, `hermes-setup` stops with the
   `${ENGINE} is unresolved ...` load error, although it never starts the
   editor. Set `UE_ROOT` or `engineRoot` in `unreagent.local.yaml`.
2. Checks `agent.hermes.enabled`. If it is not set, it prints
   "nothing to do" and exits 0.
3. If `mcp.token` is empty, generates 32 random bytes (hex) and writes
   `mcp.token:` into `unreagent.local.yaml` (git-ignored). The launcher
   reads it back on the next start, so the token survives restarts. The file
   is re-serialized, so its comments are lost.
4. Resolves the target config: explicit `agent.hermes.configPath` (it takes the
   path placeholders) → `$HERMES_HOME/config.yaml` → `~/.hermes/config.yaml`.
5. Reads the existing Hermes config and merges into it: every other key and
   every other MCP server stays. A missing, empty or `null` file counts as an
   empty config.
6. Backs up the original, byte for byte, to `<path>.bak` — **once**. An
   existing `<path>.bak` is never overwritten, so it keeps the oldest copy.
   (No backup is made for a missing or empty file.)
7. Sets `mcp_servers.unreagent: { url, headers: { Authorization: Bearer } }`
   in the Hermes config. A second run **overwrites** the entry rather than
   duplicating it — the operation is idempotent.
8. Writes the file atomically (temp + rename) so a crash mid-write cannot
   leave a half-written config.

The file is re-serialized, so **comments and key order in the Hermes config are
not preserved**; only the data survives, and a one-line
`# unreagent-managed ...` marker is put on top. Keep anything you want to
retain verbatim in the `.bak`.

If `mcp_servers` is not a mapping with string keys — a list, or a key YAML
reads as a number — `hermes-setup` refuses with
`Hermes config <path> left untouched: mcp_servers is not a mapping of server names ...`
and the Hermes config is left untouched (no backup, no write). The same goes for
a Hermes config that cannot be parsed. A token generated in step 3 may already
be saved in `unreagent.local.yaml` by then; the re-run reuses it. Fix the block
by hand and re-run.

**Recovering after 0.0.9 or earlier.** Those releases moved the Hermes
config to `<path>.bak` and wrote a new file holding only the unreagent entry. If
that happened to you, the original is in `config.yaml.bak` next to the config
(`~/.hermes/` by default); later runs never overwrite that
backup. Copy it back, then run `hermes-setup` again — it now merges.

The MCP server enforces `Authorization: Bearer <token>` whenever
`mcp.token` is set — including for the embedded Claude Code agent, which
gets the matching `headers:` block in its `--mcp-config` automatically.
The token check uses `crypto/subtle.ConstantTimeCompare`; the empty-token
default (the historic behaviour) keeps the server open on 127.0.0.1.

Restart Hermes after running `hermes-setup` so it picks up the new entry.

### Runtimes for the agent

`run_python` executes code via `uv run python` — uv builds / syncs the venv
automatically from `pyproject.toml` / `requirements` and installs the right
Python version on demand. `run_node` runs JS in the project context. Both
tools write the script into the runtime's working directory (not a system
temp directory) and run it from there, so `run_python` can import modules
that sit next to `pyproject.toml` and `run_node` resolves bare imports
through the project's `node_modules`. The agent does **not** have to analyse
or set up the environment itself; the instructions are in the MCP tool
description and are therefore automatically in context.

## MCP tools

| Tool | Function |
|---|---|
| `status` | process status + available commands — a service whose control loop misses the internal 10s timeout is reported as `"unresponsive": true` (`running`/`pid` unknown, sent as `false`/`0`) instead of being dropped from the list |
| `ue_start` / `ue_stop` / `ue_restart` | editor lifecycle |
| `run_command` | run a preconfigured command (compile, package) |
| `logs` | last output lines of a service |
| `run_python` / `run_node` | run code in a prepared environment |
| `read_file` / `list_dir` / `write_file` / `edit_file` | file access on the project (only with `files.enabled` / `-files`, scoped to `root`) |
| `approve` | permission prompt tool for Claude Code |

### MCP server hardening

The `/mcp` HTTP endpoint rejects malformed or unexpected requests instead of
guessing:

- Every POST must carry `Content-Type: application/json` — anything else
  gets `415 Unsupported Media Type` (the manual test below already sets it).
- A request that carries an `Origin` header is rejected with `403 Forbidden`
  unless the origin is loopback (`http`/`https` on `localhost`, `127.0.0.1`
  or `::1`, any port) — a defence against a web page in the user's browser
  driving the launcher (CSRF/DNS-rebinding). Requests without an `Origin`
  header — every non-browser MCP client, including Claude Code and `curl` —
  are unaffected. The allowlist is not yet configurable, so a browser-hosted
  client on another origin cannot be pointed at unreagent today.
- `initialize` negotiates `protocolVersion` against `2025-06-18` (default)
  and `2025-03-26`; any other value is answered with the default rather than
  echoed back, since this server implements only the Streamable-HTTP
  transport (no SSE, no sessions).
- Connection timeouts guard against a wedged socket: `ReadHeaderTimeout` 10s,
  `ReadTimeout` 60s, `IdleTimeout` 120s. `WriteTimeout` is 4h — the ceiling
  for a single tool call, so a long-running `run_command` (e.g. a full UE
  build) has room; it is a backstop, not a request budget.

## CLI flags

| Flag | Effect |
|---|---|
| `-config <path>` | alternative path to `unreagent.yaml`; a relative path is made absolute against the working directory before anything is derived from it |
| `-no-agent` | **do not** start the agent — only UE + MCP server. An external agent (your own Claude Code session) then connects to the MCP server. |
| `-files` | enable the file tools (`read_file` / `write_file` / `list_dir` / `edit_file`) even if off in the config. Refused with a warning (file tools stay off) when `files.root` contains a placeholder that did not resolve — set `unreal.project` or an explicit `files.root`. |
| `-write-mcp-config <path>` | additionally write the assembled MCP config as `.mcp.json` to `<path>` (for external clients). |

## Sub-commands

| Sub-command | Purpose |
|---|---|
| `unreagent hermes-setup` | One-shot merge of the unreagent MCP server into Hermes Agent's `config.yaml` (idempotent; auto-generates `mcp.token` if missing; needs a resolvable engine, like a normal start). See "Hermes Agent" above. |

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
