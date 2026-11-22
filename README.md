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
- **In-editor MCP** (running *inside* the engine) — *content* layer. Two
  servers, covering different sets: the
  [UE LLM Toolkit](https://github.com/ColtonWilley/ue-llm-toolkit) plugin
  (Node stdio bridge) is the one for blueprints, assets, levels and UE Python;
  [Unreal MCP](https://dev.epicgames.com/documentation/unreal-engine/unreal-mcp-in-unreal-editor)
  (built into UE 5.8+, experimental, plain HTTP, no bridge) covers other
  ground (Niagara, UMG, PCG and more, see Option A). Both can run side by side. Setup:
  [In-editor MCP](#in-editor-mcp-unreal-mcp-or-ue-llm-toolkit-windows).

The launcher hands the agent both via `--mcp-config` (`mcp.extraServers` in
the config), so the agent does not have to wire anything up itself. Stdio
bridges are fully checked before the agent starts, with each step logged
loudly, instead of letting the agent run into a meaningless `-32000`
connect error: binary on PATH? bridge script present? `node_modules` there
and marked as working (`.unreagent-installed`, written once the bridge passed
its smoke test; otherwise the launcher runs `npm install`, cut off after 5
minutes; a failed install is logged as a warning and skips the smoke test,
but the bridge stays in the agent's config — unless a `node_modules` was
already there, which is kept and left to the smoke test)? does the bridge
answer a real `initialize` handshake
(smoke test: only a JSON-RPC response with id 1 and a `result.serverInfo`
object passes; any other response with id 1 fails at once — an error names
its code and message, a result without `serverInfo` says so; any other
stdout line is skipped, and 20 seconds without an answer fail it)? And once
the in-editor server (`UNREAL_MCP_URL`) is reachable, that's logged too — or
a warning with the usual suspects if it isn't. These checks run only for
stdio entries (those with a `command`). An HTTP / SSE entry such as Unreal
MCP gets only a reachability watch, see
[Option A: Unreal MCP](#option-a-unreal-mcp-ue-58-built-in-experimental) below.

The same config can additionally be written to a file for **external clients**
(your own Claude session, Cursor, VS Code). Via `mcp.writeConfig` (a list of
`{path, format}`, format `mcp_json` (the default) or `vscode`; any other
format is a load error —
`mcp.writeConfig[0].format invalid: "json" (allowed: mcp_json, vscode)`; a
relative `path` is resolved against the agent working directory, normally the
project directory; the overlay replaces the list as a whole) or the flag
`-write-mcp-config <path>`. Handy with `-no-agent`: the launcher writes the
`.mcp.json` and keeps UE + MCP running, you connect externally. The `unreagent`
entry's url is the address clients connect to, so a wildcard `mcp.address`
host is written as `127.0.0.1` (`[::1]` for `::`).

The written file holds the bearer token when `mcp.token` is set, so it is
created with mode `0600`. It is written atomically: the content goes into a
temp file `<name>.tmp-<digits>` in the target's directory, which is then
renamed over the target, so a crash mid-write cannot leave a truncated file.
That directory must be writable; if it is not, or the target is a dangling
symlink, the old file is left untouched and a `WARN` is logged. An existing
file keeps its mode, unless it is open to group or others: then it comes out
`0600`, and the log names the old mode. This is Unix only: on Windows the mode
is not handled. Since the target is replaced, not rewritten, a hard link to it
keeps the old content. With `mcp.token` set, a written file that sits inside
the project directory and that git does not ignore (`git check-ignore` exits
1) gets a `WARN` in the log (in window mode also on stderr), since a commit
would take the token along: add the file to `.gitignore`, and if git already
tracks it, `git rm --cached` it as well. No warning when git is missing, no
project is known (`unreal.project` unset and no `.uproject` found), the
project is not a git work tree, the path is outside the project, or git does
not answer within 5 seconds.

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
| UE editor | none (`ping` as a stand-in) | real UE 5.8 project (C++ module) |
| Agent | off by default | Claude Code |
| In-editor MCP | not wired | UE LLM Toolkit (submodule, fork branch `ue-5.8`); Unreal MCP as a commented entry |
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
3. Start `unreagent.exe` (double-click or console). It reads `unreagent.yaml`
   next to the .exe, or `unreagent.yml` when there is no `unreagent.yaml`;
   alternative config path via `-config <path>`. The .exe opens no file
   passed to it: dropping a file onto it ends with a usage error (exit 2)
   instead of starting.

`unreagent.yaml` contains **no machine-specific paths**. Editor, `compile` and
`package` are auto-derived. A minimal config is enough:

```yaml
agent:       { enabled: true, command: claude, claudeIntegration: true }
runtimes:    { python: { enabled: true }, node: { enabled: true } }
mcp:         { enabled: true }
```

This starts Claude Code in the window, with no permission policy. Adding
`permissions: { enabled: true, ... }` to it does **not** make the policy apply:
in window mode it cannot, and the launcher logs a `WARN` saying so (see
[Permission layer](#permission-layer)).

### Portability: paths are resolved at runtime

| Placeholder | Resolution |
|---|---|
| `${ENGINE}` | env `UE_ROOT` -> `engineRoot` (local.yaml) -> the `.uproject`'s `EngineAssociation` (a Launcher version; a registered source build; a value with `/` or `\` is an engine root path, relative to the `.uproject`'s folder or absolute; blank, missing or not a string means the engine tree the project sits in, the nearest folder above the `.uproject`'s folder that has `Engine/Binaries` and `Engine/Build`, as the engine does it; the `.uproject` may be UTF-8 with or without BOM, or UTF-16 LE/BE with BOM) -> the highest installed version with an editor binary (unreagent's own fallback, the engine has none; used for a project outside any engine tree). Launcher installs are looked up in `%ProgramData%\Epic\UnrealEngineLauncher\LauncherInstalled.dat` (the file the engine itself reads; unreagent uses the entries named `UE_<version>`), then in the `HKLM\SOFTWARE\EpicGames\Unreal Engine\<version>\InstalledDirectory` registry key as a fallback, then in the Epic default install paths; both the `EngineAssociation` match and the highest-version fallback use all three, and for a version listed in both the `.dat` and the registry the `.dat` directory is used unless it has no editor binary, then the registry's (when the project names an engine that is not usable — an `EngineAssociation` that is not installed, installed without an editor binary, or a path that is no engine root, or a blank one in an engine tree without an editor binary — the highest-version step logs a `WARN` naming the engine it picked, because the editor then opens the project with that engine, not the one the project names). A relative `UE_ROOT` / `engineRoot` is resolved against the **working directory** and made absolute |
| `${PROJECT}` | `unreal.project` if set (absolute, or relative to the **config directory**; the file must exist), otherwise the first `.uproject` next to the config |
| `${PROJECT_DIR}` / `${PROJECT_NAME}` | directory of that `.uproject` / its file name without the extension |

A relative `-config <path>` is resolved against the working directory and made
absolute first, so every placeholder value ends up an absolute path.

**Where placeholders are replaced.** In `unreal.editor` and `unreal.args`;
`agent.command`, `agent.args`, the `agent.env` values, `agent.workdir` and
`agent.hermes.configPath`; `permissions.allow` / `permissions.deny`; the
runtimes' executables and project directories (`runtimes.python.uv` /
`project`, `runtimes.node.node` / `npm` / `project`); `files.root`; `skills.dir`; every
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
`files.enabled`, `skills.dir` needs `skills.enabled` with `skills.materialize`).
`unreal.*`, `agent.workdir` and your own `commands` always count.
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
- An unresolved `skills.dir` that is not in use (skills are not materialized) is
  cleared instead of rejected. `-no-skills-mcp` then has no directory to write
  to and logs
  `WARN skills not written: skills.dir names a placeholder that is unresolved — set unreal.project or skills.dir`.

Without an engine and with the default `unreal.editor`, the launcher therefore
stops at load, and so does `unreagent hermes-setup`, although it does not start
the editor. Point `UE_ROOT` / `engineRoot` at an installation, or set
`unreal.editor` to a literal path.

Machine-specific overrides go in a **git-ignored `unreagent.local.yaml`**,
which is overlaid on top of `unreagent.yaml`:

```yaml
# unreagent.local.yaml  (DO NOT commit)
engineRoot: "D:/UE/UE_5.8"                 # if auto-detect fails (path is an example)
agent: { command: "C:/.../claude.cmd" }    # if claude is not on PATH
```

The overlay is applied by YAML decode, **not** a deep merge: a scalar replaces
the base value only when the local file sets it; a **list** replaces the base
list as a whole (`unreal.args`, `agent.args`, `permissions.allow` / `deny`,
`mcp.writeConfig`, `mcp.allowedOrigins`); a **map** merges by key, and each
entry you give replaces the base entry whole (`agent.env`, `commands`,
`mcp.extraServers`). So
`agent.env: { HOME: "C:/Path/To/Home" }` adds or changes `HOME` and keeps the
other variables of the base file, while a command defined in both files comes
out with the local definition only — you cannot override just its `args`.
The `skills` section has no list or map option: each of its keys overlays on
its own, like a scalar.

Each file holds one YAML document. Anything after a second `---` is a load
error rather than being ignored: `<file>: it holds more than one YAML document
(another one starts at line N); refusing to drop all but the first`, where
`<file>` is the file's name (`unreagent.yaml` or `unreagent.local.yaml`;
`foo.yaml` or `foo.local.yaml` with `-config foo.yaml`). A bare `---` or `...`
at the end is fine.
The config files may be symlinks — `unreagent.yaml` or `unreagent.yml` next
to the .exe, and the overlay (`unreagent.local.yaml`, or `unreagent.local.yml`
next to an `unreagent.yml`) — but one that leads to no file is a load error
naming the link, not skipped like a missing file; a broken `unreagent.yaml`
link is never passed over for an `unreagent.yml`:

- dangling: `config not readable: <link> is a symlink to <target>, which does
  not exist`;
- a chain of links whose end is missing: `config not readable: <link> is a
  symlink to <hop1>; following the links leads to <missing>, which does not
  exist`;
- a loop: `config not readable: <link> is a symlink that cannot be resolved:
  ...`.

Fix the link or delete it.

Recommended `.gitignore` entry in the UE project:

```
unreagent.exe
unreagent.local.yaml
unreagent.local.yaml.tmp-*
unreagent.log*
.mcp.json.tmp-*
```

The `.tmp-*` lines cover a leftover of a file that holds the bearer token and
is saved through a temp file: `unreagent hermes-setup` saves
`unreagent.local.yaml` that way, and `mcp.writeConfig` and `-write-mcp-config`
save their `.mcp.json` that way. A kill mid-write can leave
`<name>.tmp-<digits>` behind with the token in it. Delete such a file or keep
it out of git; for a target with another name (such as `.vscode/mcp.json`,
or a `-write-mcp-config` path), add its own `<name>.tmp-*` line.

`unreagent.log*` is there because in window mode the launcher log sits in the
project directory, and the log of releases up to 0.0.9 can carry the bearer
token in plain text (the full agent command, `--mcp-config` included).
`unreagent.log.1` is the previous run's log, so after an upgrade it can still
hold that older line; the trailing `*` covers both. Later releases log the agent
command with the path of the launcher's own MCP config file after
`--mcp-config`, never its contents.

### When the agent ends (`/quit` or crash)

In window mode the agent is the lead process — if it ends (e.g. `/quit`) and
is not restarted, the launcher prompts on the console (`agent.onExit: ask`,
the default):

```
Agent exited.
  [Enter] stop everything (UE + launcher)
  [k]     keep the editor running, launcher console
  [r]     restart the agent
> _   (30s -> stop everything)
```

The first line says how the run ended: `Agent exited.` (exit 0),
`Agent crashed.`, or `Agent did not start: <reason>` when the agent could not
be started at all (for example a wrong `agent.command`). The prompt and the
launcher console read the same input, so lines piped into the launcher are not
lost between them.

While the restart policy retries a failed agent start, window mode prints one
console line per retry: `unreagent: Agent did not start: <error> — retry N[/M]
in D` (`/M` only with an explicit `maxRestarts`; `D` is a duration such as `5s`
or `1m30s`). The start the policy gives up on gets no such line; with
`onExit: ask` the prompt above names it.

`r` (and `start agent`) from the launcher console hands the terminal back to
the agent, see [Manual control](#manual-control-stdin). Without a TTY
(headless `-p`), it shuts down cleanly instead. Alternatives:
`onExit: shutdown` (always shut everything down immediately) or `onExit: leave`
(everything keeps running, warning only — manual Ctrl-C required).

### Restart policy and the fast-fail give-up

`unreal.restart` and `agent.restart` take `never`, `on-failure` (default) or
`always`. With `maxRestarts: 0` (the default) there is no fixed bound, but a
service that **fails** 3 times in a row, each within 10 s of its start, is no
longer restarted; the launcher logs one line saying it gave up. A failure is a
non-zero exit or a failed start. A clean exit 0 resets the count (so
`restart: always` with a quick job keeps restarting), as does a run longer
than 10 s or a manual start / restart. An explicit `maxRestarts` keeps its
exact bound and the fast-fail rule does not apply then. It covers both
`unreal` and `agent`; there is no extra config field. When a failed start is
retried, the launcher log says so: `[name] restart N[/M] in D` (the same line
as after an exit; `/M` only with an explicit `maxRestarts`).

### Exit codes (CI / headless)

| Exit | When |
|---|---|
| `0` | a signal (Ctrl-C, SIGTERM), or the agent's last run exited 0; also `-h` (`-help`, `--help`) for the launcher or for `hermes-setup`, which prints the usage and starts nothing |
| `1` | the agent failed and is not restarted: a non-zero exit with `restart: never`, after an explicit `maxRestarts` is used up, or after the fast-fail give-up; also a failed agent start. Interactively, after a crash, leaving the exit prompt with Enter, any other input, EOF or the 30 s timeout |
| `1` | an `mcp.address` that is not `host:port` with a port from 1 to 65535 and a host that is empty, an IP address or a plain name (see `mcp` in the configuration quick reference; checked when `mcp.enabled` or `agent.hermes.enabled` is on) is a load error, before anything starts; or the MCP server cannot bind `mcp.address` (port taken, for example by a second unreagent on the same project, or not an address of this machine). The launcher binds before it writes any file, so the running instance's files stay as they are |
| `1` | no agent configured **and MCP not serving** (off, or stopped serving after the bind), and the editor's last run was a quick failure (a failed start, or a failure within 10 s of its start) with no restart following: `restart: never`, an exhausted `maxRestarts`, or the fast-fail give-up. The order does not matter: MCP that stops serving after such a give-up ends the launcher too, unless the editor was started again in between |
| `2` | a usage error on the command line: a flag the launcher does not know or one missing its value, or a word that is not a sub-command (a file dropped onto the .exe included); for `hermes-setup`, any flag but `-config` and `-h` (a launcher flag such as `-no-agent` included), `-config` without its value, or a word among its arguments. The launcher checks the command line first, so nothing is started or written |

An agent that fails and **is** restarted produces no exit code until it stops
failing or gives up. With the default `restart: on-failure`, a headless agent
that fails after more than 10 s is restarted and re-runs its prompt, so
**for one-shot `-p` / CI runs set `agent.restart: never`**; the exit code then
reflects that single run.

The first reason to end the session wins: a Ctrl-C that also kills the agent
normally gives 0, but in a narrow race the agent's own failure can win and
give 1. How Ctrl-C reaches a foreground agent on Windows is unverified. An
error line on stderr names the reason. With MCP on
(including `-no-agent` and `manualStart` setups) an editor give-up does not end
the launcher: it logs the failure and keeps serving MCP, so an external agent
can read the logs and retry `ue_start`. If no agent is configured and MCP
then stops serving before anything started the editor again, nothing is left
to retry with, and the launcher ends with exit 1.

Every editor give-up, with or without an agent, ends its log line with where
the crash dumps are, for example `The editor failed and is not restarted. Any
crash dumps are in <project>/Saved/Crashes.` With `unreal.cleanOnRestart` the
line adds what empties them: a retry from the launcher keeps them, a new
launcher run empties them.

## In-editor MCP: Unreal MCP or UE LLM Toolkit (Windows)

For the agent to work **inside the editor**, the editor needs an in-editor MCP
server. Two servers are available and they cover different sets: the Toolkit is
the one for blueprints, assets, levels and UE Python (Unreal MCP has no tools
for those, see Option A); Unreal MCP covers other areas. They can run side by
side (Toolkit on `:3000`, Unreal MCP on `:8000`):

| | Unreal MCP (built in) | UE LLM Toolkit (plugin) |
|---|---|---|
| Engine | UE 5.8 or newer | upstream: written for UE 5.6 and 5.7; UE 5.8 needs a source port (the reference project uses a fork branch, see Option B) |
| Status | experimental (Epic) | third-party plugin |
| Transport | JSON-RPC over HTTP POST, `http://127.0.0.1:8000/mcp` | HTTP `:3000` behind a Node stdio bridge |
| Needs | two engine plugins enabled **and** a start flag (see Option A); no bridge | plugin in `Plugins/`, Visual Studio build once, Node.js |

The short version for the Toolkit: copy the plugin into the `Plugins/` folder,
build once, and make sure the editor actually loads it — two things can stop
that (an `EngineVersion` in the `.uplugin`, and the bridge's `npm install`),
see Option B. For Unreal MCP: enable two plugins, start the server with a
command-line flag, and add one `extraServers` entry. Neither is "drop it in and
done".

### What the machine must have

| Component | What for | Note |
|---|---|---|
| Unreal Engine 5.7 or newer | the editor itself | auto-detect (see the `${ENGINE}` row above); otherwise `engineRoot` in `unreagent.local.yaml`. The reference project ([unreagent-example](https://github.com/Getty/unreagent-example)) runs on 5.8; Unreal MCP needs 5.8+. Package recovery, editor exit codes and the missing-module case were measured on Windows with the Launcher builds 5.7.4 and 5.8.4; the crash reporter UI and disaster recovery are from reading the engine source only, and other versions are unverified |
| Visual Studio 2022 17.14+ or Visual Studio 2026 18.0+ ("Game development with C++" workload) | **only for the Toolkit**: builds the plugin once | UE 5.8 lists VS 2022 17.14+ and VS 2026 18.0+ (2026 recommended); the plugin ships as C++ source, UE asks "rebuild?" on first start. Not needed for Unreal MCP |
| Node.js >= 18 | the Toolkit's stdio bridge, and `run_node` | `npm install` for the bridge is done by the launcher automatically. Not needed for Unreal MCP |
| Claude Code | the agent | `claude` on PATH; otherwise `agent.command` in local.yaml. Install: [setup docs](https://code.claude.com/docs/en/setup) (native installer; on Windows PowerShell `irm https://claude.ai/install.ps1 \| iex`) |
| `unreagent.exe` + `unreagent.yaml` | this launcher | see [Setup](#setup) |

### Option A: Unreal MCP (UE 5.8+, built in, experimental)

Source: [Unreal MCP in Unreal Editor](https://dev.epicgames.com/documentation/unreal-engine/unreal-mcp-in-unreal-editor).
No Node bridge, no Visual Studio build, no plugin download.

1. **Enable the plugins** (Edit -> Plugins): "Unreal MCP" (identifier
   `ModelContextProtocol`) and "All Toolsets" (`AllToolsets`). Both are
   **disabled by default** (`EnabledByDefault: false` in their `.uplugin`
   files) and marked `NoRedist` and experimental; "Unreal MCP" pulls in
   "Toolset Registry", which is enabled automatically. To do it in the project
   file, add these two entries to the `Plugins` array of the `.uproject` (not
   a whole file) and restart the editor:

   ```json
   { "Name": "ModelContextProtocol", "Enabled": true },
   { "Name": "AllToolsets", "Enabled": true }
   ```

   Whether a Launcher build of the engine ships these `NoRedist` plugins is
   **unverified** — if they are missing from Edit -> Plugins, that is why.
2. **Start the server — enabling the plugin does not start it.** The setting
   "Auto Start Server" (`bAutoStartServer` of `UModelContextProtocolSettings`,
   shown as "Model Context Protocol" under Editor Preferences, stored in
   `EditorPerProjectUserSettings`) is **off** by default. The server starts only
   when the editor command line carries `-ModelContextProtocolStartServer` or
   that setting is turned on. The launcher can pass the flag for you:
   `unreal.args` replaces the default list **as a whole**, so give the full
   list, defaults included:

   ```yaml
   unreal:
     args: ["-stdout", "-FullStdOutLogOutput", "-ModelContextProtocolStartServer"]
   ```

   The default list is `["-stdout", "-FullStdOutLogOutput"]`; the `.uproject`
   is always prepended and `-unattended` is appended by the `unattended`
   option. The server then listens on `http://127.0.0.1:8000/mcp` (path and
   port from the same settings class; `-ModelContextProtocolPort=<port>`
   overrides the port, a value outside 1-65535 is ignored with a warning). The
   old spelling `-StartModelContextProtocolServer` still works but is
   deprecated. The settings menu path is unverified (no menu text was found in
   the engine source; look for the "Model Context Protocol" section).
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
   so give the full list (including `-ModelContextProtocolStartServer`) and
   match the URL:

   ```yaml
   unreal:
     args: ["-stdout", "-FullStdOutLogOutput", "-ModelContextProtocolStartServer", "-ModelContextProtocolPort=8010"]
   mcp:
     extraServers:
       unreal-mcp:
         type: http
         url: "http://127.0.0.1:8010/mcp"
   ```

Things to know:

- **Reachability watch only.** The launcher's startup checks (binary,
  `node_modules`, `initialize` smoke test) run only for stdio entries with a
  `command`. An HTTP / SSE entry is instead watched for reachability, like
  the in-editor `UNREAL_MCP_URL`: once bridge preparation is done, the
  launcher tries a TCP connect to the URL's host and port for 5 minutes and
  logs one line, either
  `MCP server 'unreal-mcp': http://127.0.0.1:8010/mcp is reachable.` or, if
  nothing answered, a warning that names the URL (`... not reachable after
  5m0s — is the server running, and does the url match its port and path?
  The agent may have marked the server failed; once it answers, /mcp in
  Claude Code reconnects it.`). The limits: it is a connect only, no HTTP
  request and no MCP session, so "reachable" does not prove the server
  speaks MCP; an `https` URL without a port probes 443; the agent still
  starts at once and is never held back; and the watch ends with the
  launcher. The log shows only scheme, host and path — user info and query
  string are never logged, and headers are never read. A URL without a host
  gets the warning `WARN MCP server '<name>': url is not a URL with a host —
  its reachability is not checked.`
- **Start order.** Epic's docs say to start the agent after the editor's
  server is up; unreagent starts editor and agent together. If Claude Code
  reports the `unreal-mcp` server as failed, wait until the editor has
  finished loading and reconnect with `/mcp`.
- **No authentication.** Unreal MCP listens on loopback by default and has no
  login; a request without an `Origin` header is accepted, and a browser
  `Origin` must be `localhost`, `127.0.0.1` or `[::1]`. See [Access from another machine](#access-from-another-machine)
  before changing the bind address.
- **Three tools, not 278.** With "tool search" on (`bEnableToolSearch`, the
  default) `tools/list` returns only `list_toolsets`, `describe_toolset` and
  `call_tool`; the agent discovers a toolset's tools with the first two and
  invokes them through `call_tool` (`toolset_name` plus the tool name). With
  it off, every tool registers natively as `<Toolset>.<Function>`. The
  roughly 278 tools in 21 toolset classes are a count from reading the 5.8.4
  source, not from a running editor. Consequence for the
  [permission layer](#permission-layer): every call is seen as
  `mcp__unreal-mcp__call_tool` (the server key is part of the name); the real
  tool is in the arguments.
- **Not a Toolkit replacement.** From the source (nothing run): no tool
  runs Python or a console command, and there are no tools for blueprint
  graphs, assets, level actors, materials, animation, sequencer, control rig,
  retarget or audio. It is deeper than the Toolkit for Niagara and UMG and
  adds PCG, Dataflow, physics assets, GAS, Slate inspection, automation
  tests, config settings and plugin management (MVVM needs its own plugin
  entry, see below). Use it next to the Toolkit, not instead.
- **`AllToolsets` is not everything.** It leaves out the Live Coding,
  MVVM, Chaos Cloth Asset, Sequencer Anim Mixer and MetaHumanGenerator
  toolsets (`LiveCodingToolset`, `MVVMToolset`, `ChaosClothAssetToolset`,
  `SequencerAnimMixerToolset`, `MetaHumanGenerator`); each is a separate plugin
  to add to the `.uproject` by name. Some toolset modules
  (AnimationAssistant, StateTree, AIModule, Conversation)
  looked empty in source; whether they carry tools in content is unverified.
- **Sessions end with the editor.** The server hands out an `Mcp-Session-Id`;
  after an editor restart the old id is answered with 404 ("reinitialize").
  How Claude Code recovers is unverified; reconnect with `/mcp`.
- **One call at a time.** Tool calls run sequentially on the game thread.
- **Offscreen rendering unverified.** How the server behaves with `-unattended`
  and offscreen rendering has not been checked.

### Option B: UE LLM Toolkit

#### Steps

1. **Drop the plugin into the project** — copy or clone the repo/folder to
   `<Project>/Plugins/UELLMToolkit/`. The `.uplugin` has
   `EnabledByDefault: true` and activates its own engine dependencies
   (EditorScriptingUtilities, Niagara, ControlRig, IKRig, ...) — nothing
   to change on the `.uproject`. **But check its `EngineVersion` field.**
   Upstream HEAD (9f6a0f7) declares `"EngineVersion": "5.6.0"`; the reference
   project does not use it but pins a fork branch (see below).

   The engine accepts an `EngineVersion` only if major, minor **and patch**
   equal its own compatible version (patch 0 on an Epic build). On a mismatch
   the editor asks "designed for build 5.6.0, attempt to load it anyway?", and
   under `-unattended` (the launcher's default) the answer is No: the plugin is
   **not loaded** and port 3000 never opens, with no error beyond the editor log
   line `... is Incompatible` / `Message dialog closed, result: No`. A two-part
   value such as `5.6` does not parse, and the plugin loads anyway. Measured on
   Windows: `5.6.0` is refused on 5.7.4 and 5.8.4, `5.7.0` loads on 5.7.4; on
   5.8.4, `5.8.4` is refused, `5.8.0` and `5.8` load. **Fix:** remove the
   `EngineVersion` line or set it to `<major>.<minor>.0` of your engine.

   The launcher warns about this at startup: one `WARN` per enabled plugin
   under `<Project>/Plugins` that the editor will refuse, naming the plugin,
   the value and the fix (console, or `unreagent.log` and stderr in window
   mode); it keeps starting. It predicts from the files, so its limits are:
   - *Not covered:* plugins outside `<Project>/Plugins`, behind a junction or
     symlink, enabled only as a dependency or by `-EnablePlugins=`; plugin
     extensions; a name found twice; `.uproject` entries with a list or
     `Optional`; an `Enabled` or `EnabledByDefault` that is not a JSON boolean
     (`true`/`false`). Any editor argument containing `DisablePlugins=` switches the
     check off (one info line), `-NoEnginePlugins` skips it.
   - *May warn wrongly:* a plugin the target's own plugin list disables, a
     same-named plugin in another plugin directory, an engine whose
     `Build.version` and `Version.h` disagree, and a start that uses a different
     engine than the one checked (with `unreal.editor` a wrapper script or a bare
     name on PATH, the check reads the resolved engine root's version, so it can
     warn wrongly or miss a refusal).

   On UE 5.8 the plugin also needs a source port (it was written for 5.6 and
   5.7); the reference project's fork branch `ue-5.8` carries it and drops the
   `EngineVersion` field. Whether upstream will carry such a port is open.
2. **First editor start** — UE reports "Missing Modules ... rebuild?" -> Yes.
   The build needs Visual Studio (once; afterwards the binaries live in the
   plugin). Alternatively ahead of time via the launcher: console -> `c compile`.
3. **Wire it up.** Once the plugin is loaded, its HTTP server starts with the
   editor automatically on port `3000`. The launcher forwards the bridge via `--mcp-config` to the
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

**The bridge's `npm install` can fail.** With npm 10.9.2 (Node 22.17.1) the
install in the Toolkit's `mcp-bridge` folder exits 1 with
`Cannot read properties of null (reading 'edgesOut')` while npm resolves peer
dependencies of the bridge's dev dependencies (measured on
Windows; the same with `--omit=dev` and `npm ci`). The launcher then logs the
warning from the `-32000` chain below and the bridge never starts. Working by
hand, in that folder:

```bash
npm install --omit=dev --legacy-peer-deps
```

(the bridge needs its runtime dependencies only); `npx npm@11.21.3 install`
also worked. The launcher's own install runs a bare `npm install`, so do this
once by hand before the first start; the launcher keeps an existing
`node_modules` and lets the smoke test decide. Upstream's
`package-lock.json` does not match its `package.json`, so every successful
install rewrites the tracked lock file and leaves a git checkout of the plugin
dirty; restore it with `git checkout -- package-lock.json`.

What the launcher does **automatically** on startup (see above): install
bridge `node_modules`, check binary / script, `initialize` smoke test, wait
for port 3000 — every step with a clear message in `unreagent.log`. In window
mode the first line of a `WARN` from this preparation (a missing bridge
command, a failed install or smoke test, an unreachable `UNREAL_MCP_URL`, a
failed runtime preparation) also goes to stderr; the full text stays in
`unreagent.log`. Most of these come before the agent takes the window, but
not all: an unreachable `UNREAL_MCP_URL` is reported after 5 minutes, and
runtime preparation runs while the agent starts, so its `WARN` can land in
the agent's window. Ctrl-C while the bridges are being prepared (a running
`npm install` included) exits at once; the editor is not started.

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
— the real cause is in `unreagent.log` (check chain; in window mode a
`WARN`'s first line is also on stderr) or in Claude Code's MCP logs
(`%LOCALAPPDATA%\claude-cli-nodejs\Cache\<project>\mcp-logs-*`). Usual
suspects, in this order:

1. **Bridge path wrong** (`Cannot find module ...`) — check the path in
   `extraServers` against the real plugin layout. Related: **plugin not
   loaded** — the bridge is fine but port 3000 never opens. Search the editor
   log for `is Incompatible` / `Message dialog closed, result: No`: the
   `.uplugin`'s `EngineVersion` does not match the engine (major, minor and
   patch must equal its compatible version, e.g. `5.8.0`). The launcher warns
   about this at startup, within the limits listed in Option B, step 1.
2. **Node missing** or not on PATH in the agent's session.
3. **`node_modules` missing, or the install was cut short.** The launcher
   runs `npm install` itself, but gives up after 5 minutes (the whole install,
   not a single request) — slow link, unreachable registry or proxy. The
   launcher then logs a warning (in window mode to `unreagent.log`, with its
   first line also on stderr; otherwise on the console) and skips the smoke
   test, but the bridge stays in the agent's config and the editor still
   starts, so Claude Code reports the server as failed. Once the bridge
   passes the smoke test, the launcher writes
   `node_modules/.unreagent-installed`; the next start skips the install
   only if that marker exists. (A failed install whose npm error mentions
   `edgesOut` is the npm 10.9.2 problem from Option B.) Without it, the launcher runs
   `npm install` over whatever `node_modules` is there — say, one left by an
   install cut short (the timeout, or Ctrl-C) — once the network works, or
   you run `npm install` there by hand (the launcher deletes nothing; npm
   itself may remove extraneous packages). If an install fails while a
   `node_modules` was already there (say, offline with a complete one from
   before the marker existed), the folder is kept and the smoke test
   decides: a pass writes the marker, a fail is the `-32000` case. When the
   smoke test fails over a `node_modules` that was already there and its
   error names a missing module or file, the log names the repair: for a
   marked folder, delete the marker so the next start runs `npm install`,
   then the folder itself if that does not help; for an unmarked one, delete
   the folder — `npm install` can report a partial tree as up to date, so
   installing again does not repair it.
4. **Editor (not yet) reachable** — port 3000 only appears once the editor
   is fully up; the launcher logs once it is.

For **Unreal MCP** the chain is shorter, because there is no bridge process:
if Claude Code shows the server as failed, the usual suspects are, in this
order: the `-ModelContextProtocolStartServer` flag is missing from
`unreal.args` (or you set `unreal.args` and dropped the defaults — it replaces
the list as a whole), so the plugin loaded but the server never started; the
"Unreal MCP" / "All Toolsets" plugins are not enabled (off by default); the
editor was not up yet when the agent started (reconnect with `/mcp`); or the
port or path in the `extraServers` URL does not match the "Model Context
Protocol" settings (or `-ModelContextProtocolPort`). Since the launcher
only watches HTTP entries for reachability (a TCP connect, not an MCP
check), `unreagent.log` tells you whether the port answered at all: a
`... is reachable.` line means the editor's server was up (reconnect with
`/mcp`), the `not reachable after 5m0s` warning means it never answered
within 5 minutes, so check the plugins, port and path first. It cannot tell
you which of the remaining causes applies.

## Configuration (quick reference)

| Section | Purpose |
|---|---|
| `agent` | agent command + args; `claudeIntegration` injects `--mcp-config <file>` (a private temp file, see Bearer auth) (+ the permission tool, only to a headless agent with `permissions.enabled`: see Permission layer); `window` (on by default) = interactive in the foreground (inherits console / TTY, launcher logs -> `unreagent.log`, the previous session's log is kept as `unreagent.log.1`; the logged `Agent command:` line shows the launcher's own `--mcp-config` file path, any other `--mcp-config` value as `<redacted>` and the token blanked in any other argument, while the agent itself gets the real arguments); on Windows, `claudeIntegration` additionally sets `CLAUDE_CODE_USE_POWERSHELL_TOOL=1` (`powershellTool: false` to disable); `restart` / `maxRestarts` (same policy and fast-fail give-up as `unreal`); `startDelaySeconds` (0 = default 5) and `restartDelaySeconds` (0 = default 3) — never below 0 (with `agent.enabled`, a negative value is a load error, so a crashing agent cannot restart in a tight loop); `args` is a list (replaced as a whole by the overlay), `env` a map (merged by key by the overlay) |
| `permissions` | permission layer: `allow_all` / `allowlist` (default if unset) / `deny_all`, plus `allow` / `deny` rules; enforced only for a headless Claude Code agent with `claudeIntegration` (`-p`/`--print` with a task); for an interactive agent or with `claudeIntegration: false` the launcher logs a startup `WARN` and the policy is not applied (exceptions, and the open `agent.window: false` case: see Permission layer) — `Bash(...)` rules are matched per shell segment, see Permission layer below; `allow` / `deny` are lists (the overlay replaces them as a whole) and take the path placeholders; a path rule for a file tool must be absolute, see Permission layer |
| `runtimes` | `python` (via `uv run`) and `node` for `run_python` / `run_node` |
| `mcp` | MCP server on/off, `address` (default `127.0.0.1:8765`; must be `host:port` with a port from 1 to 65535 and a host that is empty, an IP address (IPv6 in brackets, without a zone such as `%eth0`) or a name of ASCII letters, digits, `-`, `_` and `.`; anything else is a load error; an empty or wildcard host (`:8765`, `0.0.0.0:8765`, `[::]:8765`) listens on every interface, and clients are given `http://127.0.0.1:<port>/mcp` (`[::1]` for `::`); a port that cannot be bound ends the launcher with exit 1), optional `token` (Bearer auth — auto-set by `hermes-setup`); every request needs `Content-Type: application/json` and, if present, an allowed `Origin` (loopback by default) — see MCP server hardening below; optional `allowedOrigins` (list of browser origins, `scheme://host[:port]`; absent = loopback only; a non-empty list *replaces* that built-in allowlist, and `unreagent.local.yaml` replaces the base list whole; an invalid entry is a load error while `mcp.enabled` is on); `extraServers` (map of in-editor / extra MCP servers in `.mcp.json` format, stdio or `type: http`) — `unreagent.local.yaml` adds servers by name and replaces a same-named one's whole definition; the others stay |
| `unreal` | *optional* — `project` (absolute, or relative to the config directory; must exist, no placeholders; default: the `.uproject` next to the config), editor `args` (unset or `~` = `-stdout -FullStdOutLogOutput`; `[]` = none; a list replaces the base list in the overlay), restart policy (`restart`: `never` / `on-failure` / `always`; `maxRestarts`, 0 = unbounded but fast-fail give-up applies, see Restart policy above), `restartDelaySeconds` (0 = default 3; negative is a load error) |
| `files` | file tools for the agent: `enabled` (default `false`; `-files` turns it on), `root` (default `${PROJECT_DIR}`; every path is confined to it, see File-tool confinement below), `readOnly` (default `false`; `true` = no `write_file` / `edit_file`) |
| `commands` | *optional* — your own one-shot commands (map keyed by name; the overlay merges by key and replaces a same-named command whole). `compile` / `package` are built-in and offered only when both the engine and the project resolve; defining your own `compile` / `package` replaces the built-in |
| `skills` | *optional* — how the shipped UE skill documents reach the agent, see [Skills](#skills). `enabled` (default `true`; `false` turns both paths off), `mcp` (default `true`: the `skills` MCP tool), `materialize` (default `false`: write the skills as files into `dir`), `dir` (default `${PROJECT_DIR}/.claude/skills`; takes the path placeholders; a relative path is taken relative to the agent working directory). `enabled: true` with `mcp: false` and `materialize: false` is a load error |
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
with **`-unattended`**. Checked against the UE 5.8.4 source
(paths relative to the engine root) and, for package recovery, measured on
Windows with 5.7.4 and 5.8.4:

- **Suppressed:** the crash reporter UI
  (`Runtime/Core/Private/Windows/WindowsPlatformCrashContext.cpp`), the
  package restore prompt (`Editor/UnrealEd/Private/PackageAutoSaver.cpp`) and
  disaster recovery (`Plugins/Developer/Concert/.../DisasterRecoveryClientModule.cpp`;
  source only, not run).
- **Skipped, not declined.** `-unattended` does not restore the autosaved
  packages and does not ask, but it also does not discard the offer:
  `Saved/Autosaves/PackageRestoreData.json` keeps its entry until the editor
  exits cleanly or the session's first package is dirtied. A crash before
  either leaves the old offer for the next start, and a later start **without**
  `-unattended` shows the restore prompt again. `-AutoDeclinePackageRecovery`
  exists but adds nothing next to `-unattended`; with `unattended: false` it
  drops a stale offer without a prompt (the file is cleared at start).
- **Not suppressed:** prompts the engine raises directly as a message box
  (for example "Target Upgrade Required") and Slate modal windows. A missing
  project module is the case to know: by the source (`LaunchEngineLoop.cpp`)
  the editor logs `Incompatible or missing module` and exits 1 without a
  dialog, and without `-unattended` that is what was measured (exit after about
  1.5 seconds). **With `-unattended` it was not:** the editor loaded on,
  answered its own "module not found" message box automatically and exited 1
  after 12 to 17 seconds when the editor was started directly, and after 9 to 11
  seconds per attempt when started through the launcher (measured on Windows
  with 5.7.4 and 5.8.4). A project
  whose C++ module has not been built yet therefore fails slowly and loops
  through the restart policy; build first (`c compile`, about 3.5 minutes on a
  fresh clone of the reference project).
- **A plugin is skipped silently** when its `.uplugin` `EngineVersion`
  does not match the engine's compatible version in major, minor or patch (log
  `Skipping load of` or the declined "Incompatible" question;
  `PluginManager.cpp`). A two-part value such as `5.6` does not parse and loads
  anyway. The launcher warns about this at startup, within the limits listed
  in Option B, step 1. See the Toolkit's `EngineVersion` there.

| Option (`unreal:`) | Default | Effect |
|---|---|---|
| `unattended` | `true` | appends `-unattended` — no crash dialog, no package-restore prompt (the restore is skipped, not discarded; see above) |
| `killCrashReporter` | `true` | kills `CrashReportClientEditor.exe` / `CrashReportClient.exe` before a start (belt and suspenders), except a start that follows a crash — an automatic restart, a `ue_start` / `ue_restart` / console `start` / `r` issued in place of a pending automatic restart (during the restart delay, or the retry delay of a failed start), or the first start after the launcher gave up on the editor — where the reporter may still be writing the dump; a `ue_stop` / console `stop` during the restart delay ends the crash episode, so a later start kills the reporter again; off Windows (`pkill -f`) only a process whose program is `CrashReportClientEditor` or `CrashReportClient` (no `.exe`) matches, not any command line that merely contains the name; a reporter started from a path with a space is not matched |
| `cleanOnRestart` | `false` | removes `Saved/Autosaves/PackageRestoreData.json` before every start; empties `Saved/Crashes/` before a start that does not follow a crash: the first start of a launcher run and every other manual start (console `start` / `r`, MCP `ue_start` / `ue_restart`). The crash folder is kept on an automatic restart by the restart policy (including a retry of a failed start), on a start issued in place of a pending automatic restart (during the restart delay after a crash, or the retry delay of a failed start; the log reason reads `start in place of the pending automatic restart`) and on the first start after the launcher gave up on the editor, so the dumps stay readable. A `ue_stop` / console `stop` during the restart delay cancels the pending restart and ends the crash episode, so a later start counts as a fresh manual start and empties the folder. When it holds anything the launcher logs `Recovery: Saved/Crashes kept (…)` with the reason |

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
**only in headless mode**: `agent.args` carries `-p`/`--print` with a task.
Every permission prompt from Claude Code then goes through the `approve` tool,
which decides per the configured policy. (Whether `agent.window: false` alone,
without `-p`, is headless is not settled; use `-p`/`--print`.)

> **Interactive window mode (the default) does not enforce the policy this
> way.** With `mode: allow_all` the launcher passes
> `--dangerously-skip-permissions` instead (Claude Code's own prompting is
> skipped entirely); with any other mode no flag is added and Claude Code
> prompts the user directly in the window, same as without unreagent. To have
> the policy actually enforced, run headless or drive Claude Code externally
> against the MCP server (`-no-agent`).

**Startup warning.** When `permissions.enabled` is set and the policy cannot
take effect, the launcher logs one line starting with
`WARN permissions are not enforced:` and starts anyway; it does not refuse the
config. With the log in a file (window mode) the line is also printed to
stderr, so you see it before Claude Code takes the window. It appears when:

- **the agent runs interactively** (`agent.window: true`, or `agent.window`
  unset and no `-p`/`--print` in `agent.args`): the launcher passes no
  `--permission-prompt-tool`, so `mode`, `allow` and `deny` are never
  consulted. With
  `claudeIntegration: true` and no hand-wired flag, the warning also says what
  the agent gets instead: `--dangerously-skip-permissions` under `allow_all`
  (every tool call runs, including one a `deny` rule names), or no permission
  flag under `allowlist` / `deny_all` (Claude Code asks you in its window as
  usual). With a `--permission-prompt-tool` wired into `agent.args` by hand,
  it names that flag, which takes effect only for a headless agent.
- **`agent.claudeIntegration` is `false`**: the launcher passes the agent no
  `--permission-prompt-tool` at all, so the policy is dead config. The fix is
  `claudeIntegration: true` (Claude Code only), plus `-p`/`--print` with a task
  if the agent is interactive.

**A hand-wired `--permission-prompt-tool`.** If `agent.args` already holds
`--permission-prompt-tool`, in either form (`--permission-prompt-tool X` or
`--permission-prompt-tool=X`), the launcher does not add a second one. When
the hand-wired value is `mcp__unreagent__approve`, the launcher logs a plain
line saying so (`Agent: --permission-prompt-tool mcp__unreagent__approve is
set in agent.args already; ...`), not a warning. When it names another tool,
`approve` is never consulted and the
startup warning above fires with the tool's name, also with
`claudeIntegration: false`; the fix is to set the flag to
`mcp__unreagent__approve`, or with `claudeIntegration: true` to remove it.

To enforce the policy: put `"-p"` (or `"--print"`) and its task in
`agent.args`, and leave `agent.window` unset or false. There is no warning for
an external agent (`-no-agent`, or `agent.enabled: false`), which can call
`approve` itself. Nor is there one for a headless agent with
`claudeIntegration: false` where you wired
`--permission-prompt-tool mcp__unreagent__approve` into `agent.args` by hand;
an interactive agent always warns, hand-wired flag or not.

Modes (where the policy is enforced, see above; under an interactive agent
with `claudeIntegration: true`, `allow_all` makes the launcher pass
`--dangerously-skip-permissions`, so Claude Code's prompts are skipped and no
`deny` rule applies):
`allow_all` = auto-approve everything, `deny` rules still apply (e.g.
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

**Redirections.** In allowlist mode a segment with a file-writing output
redirection (`>`, `>>`, `>|`, `&>file`, `>&file`) is covered only by an allow
rule that spells the redirection out before its trailing `*`:
`Bash(echo *)` does not allow `echo x > notes.txt`, `Bash(echo x > *)` does.
`Bash(*)` does not cover a redirection either; the bare `Bash` rule (no
brackets) allows everything. File-descriptor duplication (`2>&1`) and
the null device write no file and do not count: `/dev/null` for `Bash`,
the bare `$null` for `PowerShell`. For `PowerShell` a redirection to `NUL` or
`/dev/null` counts as a file.

**Unreadable command lines are denied.** A `Bash` command the tokenizer
cannot read with certainty is refused rather than guessed at: in allowlist
mode no allow rule grants it, and every `Bash(...)` deny rule matches it, so
it is blocked under `allow_all` too as soon as one such deny rule exists (a
bare `allow_all` with no `Bash` deny rule has nothing to match). That covers
`$'...'` ANSI-C quoting it cannot follow, `${...}` bodies outside the plain
forms (`${x}`, `${x:-default}`, `${#x}`, `${x%.*}` and the like — not
indirection `${!x}`, replacement `${x/a/b}`, substring `${x:1}`, `${x@P}`,
subscripts or assignments `${x:=...}`), process substitution (`<(...)`,
`>(...)`) and command substitutions nested too deep. Rewrite the command in
plain form and it goes through.

**What no rule sees.** A matcher reads the command line, not what the tool
does with it, so every escape that lives inside a tool is invisible to it:
`git -c core.pager=...` / `core.sshCommand` / `alias.x=!sh`, `GIT_*` /
`PAGER` / `EDITOR` in the environment, `find -exec` / `-execdir` / `-delete`,
`xargs` reading its commands from a file, `tar --to-command` /
`--checkpoint-action`, `rsync -e`, `ssh -o ProxyCommand`, `awk 'BEGIN{system(...)}'`,
`sed -i` / `sed e`, `tee` / `cp` / `dd of=` / `Out-File` / `Set-Content`
writing a file, `python -c` / `node -e` / `perl -e`, and `npm` / `make`
scripts. `Bash(git *)` grants all of these. An allowlist is therefore a guard
rail against an agent's mistakes, not a security boundary against a hostile
one.

Bracket rules also work on non-Bash tools, matched against the tool's
path-like input — `Write(/etc/*)`, `Read(/secrets/*)`,
`mcp__unreagent__write_file(/etc/*)`. A rule whose tool carries no evaluable
input field fails closed: a deny rule still blocks, an allow rule still does
not grant.

**Path rules for file tools** (`Read`, `Write`, `Edit`, `MultiEdit`,
`NotebookEdit`) must name an absolute path (`${PROJECT_DIR}/Source/*` or a
full path) below a root. A relative rule is a load error, in `allow` and in
`deny` (`Write(./*)` would otherwise clean to `*` and match everything).
An `allow` rule at a root (`Write(/*)`, `Write(C:\*)`) is a load error as
well, because it would allow the whole file system; a `deny` rule at a root
loads, since it can only block. To cover every path, write the bare tool name
(`Write`). Other tools get no load error, but an allow rule that names no
absolute path never grants a call that carries a path: `Grep(*)`, `Glob(*)`
and `mcp__x__*(*)` do not (the bare name does).

On Windows write rules with a drive letter (`'Write(C:\Proj\*)'`, single
quoted in YAML): a drive-less deny rule such as `Write(/etc/*)` loads but
never matches a `C:\` path. Path rules ignore case there, and `..` cannot
climb above a drive letter or a `\\server\share` root.

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

The entry goes under `mcp_servers.<name>` in the Hermes config, where `<name>`
is `agent.hermes.serverName` (default `unreagent`). The name may hold only
ASCII letters, digits, `_` and `-`, must start with a letter, and must not be
a word YAML reads as a boolean or null (`yes`, `no`, `on`, `off`, `true`,
`null`, ...). While `agent.hermes.enabled` is set, a bad name is a load error
for a normal start as well as for `hermes-setup`.

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
3. If `mcp.token` is empty, generates 32 random bytes (hex). The token is
   only held in memory for now; step 7 saves it.
4. Resolves the target config: explicit `agent.hermes.configPath` (it takes the
   path placeholders) → `$HERMES_HOME/config.yaml` → `~/.hermes/config.yaml`.
5. Reads the existing Hermes config and edits it node by node: every other key
   and every other MCP server stays. A missing, empty or `null` file counts as
   an empty config. If `mcp_servers` or the unreagent entry in it is refused
   (see below), nothing is written at all.
6. Backs up the original, byte for byte, to `<path>.bak` — **once**. An
   existing `<path>.bak` is never overwritten, so it keeps the oldest copy.
   (No backup is made for a missing or zero-byte file; a file holding only
   whitespace does get one.)
7. Writes, in this order: the new token to `unreagent.local.yaml` (only if
   step 3 generated one — an existing `mcp.token` is never touched), then the
   Hermes config. The launcher reads the token back on the next start, so it
   survives restarts and keeps matching the bearer header Hermes sends. The
   edit of `unreagent.local.yaml` keeps its comments and other keys; the new
   `mcp.token` key carries a short comment. If an existing
   `unreagent.local.yaml` is open to group or others (any group or other
   permission bit set, typically 0644), `hermes-setup` tightens it to 0600 as
   it writes the token, since the file now holds the bearer token, and says so
   after the "New bearer token generated" line:

   ```
   unreagent.local.yaml was open to group or others (mode 0644); tightened it to 0600, since it now holds the bearer token.
   ```

   The mode is set on the temp file before the rename, so the token is never
   on disk under the old bits. A new file is created 0600, and a file that is
   already closed (0600, 0400) keeps its mode without a message. Only this
   token write tightens. A token already in the file is not touched: one you
   wrote yourself, or one `hermes-setup` 0.0.9 or earlier wrote into a 0644
   file. For those, run `chmod 600 unreagent.local.yaml` yourself.
   The Hermes config, which holds the same bearer but belongs to another
   program, keeps its mode. If it is open to group or others (any group or
   other permission bit set), `hermes-setup` warns after writing it, naming
   the mode and the command that closes it (`<path>` is the Hermes config
   path from step 4):

   ```
   The Hermes config is open to group or others (mode 0644) and holds the bearer token; it belongs to Hermes, so its mode is kept. To close it: chmod 600 <path>
   ```

   The mode checked is that of the file written, the target when the config
   is a symlink, and `chmod` follows the link to it. A new Hermes config is
   created 0600 and gets no warning. The warning comes on every run while the
   file stays open, whether or not the token was generated on this run. Unix
   only: on Windows the mode bits do not decide who can read a file (it
   inherits its directory's ACL), so nothing is tightened or warned about
   there.
8. In the Hermes config, the entry `mcp_servers.unreagent` is edited in place,
   or appended as the last server if it is new; a new `mcp_servers` block goes
   at the end of the file. unreagent owns two keys of the entry, `url` and
   `headers.Authorization` (`Bearer <token>`): `url` is replaced where it
   stands, or put first if missing; a missing `headers` is added at the end of
   the entry, and a missing `Authorization` at the end of `headers`. The
   header name is matched ignoring case, so an existing `authorization:` keeps
   its spelling and gets the new token. Every other key of the entry — a
   `timeout`, other headers — keeps its value, place and comments, and so do
   the comments on `url` and `Authorization`. A second run **refreshes** those
   two keys rather than duplicating the entry — the operation is idempotent.
   The entry is headed by a comment, ``# unreagent-managed: re-running
   `unreagent hermes-setup` refreshes url and headers.Authorization; other
   keys in this entry are kept``; the one-line marker that earlier releases
   put on top of the file is removed.
9. Writes each file atomically: a temp file `<name>.tmp-<digits>` in the
   target's directory, synced to disk, then renamed over the target, so a
   crash mid-write cannot leave a half-written config.

**What stays, what changes.** Outside its own entry the Hermes config keeps
its comments, key order, quoting style, anchors, aliases and merge keys, and
the other MCP servers are not touched. Inside the entry, only `url` and
`headers.Authorization` change; the rest of it keeps its keys, order and
comments. One exception: when the entry is the last server, a comment at its
very end — even one inside `headers` — is moved behind the entry, at the
servers' indentation, since YAML cannot tell it from a comment that follows
the entry. With another server after the entry, such a comment stays where
it is. The file is re-encoded, though, so its formatting is normalised:

- spaces before an inline comment collapse to one (`model: x   # m` becomes
  `model: x # m`), so a hand-aligned config changes in many lines;
- two-space indentation, and block sequences under a key are indented;
- blank lines between nodes are dropped;
- a leading `---`, a `...` and a byte-order mark are dropped (a file that
  holds only comments keeps its byte-order mark on the first run), and so is
  a bare trailing `---` (a second YAML document that holds anything is
  refused, see below);
- CRLF line breaks become LF (the `.bak` keeps the original bytes);
- a foot comment can move to the deepest node before it;
- folded and multi-line plain or quoted scalars are joined onto one line with
  the same value (a literal `|` block is kept unless it holds an emoji, see
  below; a folded `>` block can gain a trailing blank line);
- a flow collection written over several lines is put on one line;
- an emoji or other character outside the Basic Multilingual Plane is
  rewritten as a `\U` escape inside double quotes — in plain, single- and
  double-quoted values, in keys, and in a literal `|` block, which then
  becomes one double-quoted line — with the same value;
- a flow-style `mcp_servers: { ... }` loses its own layout: the marker comment
  breaks it over several lines, and a comma that follows an inner comment
  moves to the next line.

The `.bak` holds the exact original if you want any of it back.

**When it refuses.** Nothing is written, the file stays as it was and no
`.bak` is made, in these cases. For the first eight the message reads
`Hermes config <path> left untouched: ...`:

- `mcp_servers` is not a mapping of server names (a list), has a key YAML
  reads as a number, is an alias (`*name`), carries an anchor (`&name`), or
  comes in through a merge key (`<<`);
- the existing `mcp_servers.<name>` entry, or its `headers`, is not a mapping
  with string keys (a list, a single value, a key YAML reads as a number), is
  an alias, or comes in through a merge key (`<<`) — editing it would change
  the anchored mapping or hide the new keys;
- the entry contains an anchor, since changing it would change what its
  aliases see or leave them undefined; the message ends `refusing to change
  it`;
- the entry is a stdio server (it sets `command`): hermes-setup only manages
  an HTTP entry. Remove `command` and `args` from it, or rename or delete the
  entry, then re-run;
- the entry's `headers` spell `Authorization` more than one way
  (`Authorization` and `authorization`): header names ignore case, so which
  token counts is ambiguous. Keep one, then re-run;
- a key hermes-setup looks for is not written as a plain string: an alias
  (`*u` for `url`) or a tag other than `!!str` (`!custom url`,
  `!!binary dXJs`; `!!str url` is a plain string and is accepted). The keys are
  `mcp_servers`, the entry name, `url`, `headers` and `Authorization` (in any
  case), and `mcp` and `token` in `unreagent.local.yaml`. hermes-setup finds
  a key by its plain spelling, so it would add a second one beside it. The
  message names the key and how it is written:

  ```
  the key mcp_servers.unreagent.url is written as *u, not as a plain string; hermes-setup would add a second url instead of changing it; write the key as plain url, then re-run hermes-setup
  ```

  Write the key as a plain `url`, then re-run;
- a merge key is written quoted with the `!!merge` tag, `!!merge "<<"` or
  `!!merge '<<'`, anywhere in the Hermes config or in
  `unreagent.local.yaml`. The YAML writer would turn it into a plain string
  key `"<<"` that merges nothing. A plain `!!merge <<` and an untagged
  `"<<"` are not refused. The message names the line:

  ```
  the merge key at line N is written quoted with the !!merge tag (!!merge "<<"); the YAML writer would turn it into a plain string key "<<" that merges nothing; write it unquoted as <<, then re-run hermes-setup
  ```

  Write it unquoted as `<<`, then re-run. For `unreagent.local.yaml` the
  message, like the plain-string one above, starts with `add the token to
  <path>: ` instead of the Hermes prefix;
- the edit would change a value other than the one it sets, because the YAML
  writer re-encodes that value differently (a built-in safety check). Two
  inputs trigger it: a folded `>` block scalar with a more-indented line, and
  a literal `|` block scalar that starts with a blank line. The message names
  the value, as `mcp_servers.github.description` or `prompts[0].text` (keys
  are printed as they are, so a key that itself contains `.` or `[` reads
  like a nested path):

  ```
  the result would change more than mcp_servers.unreagent: the value at <path> would change (the YAML writer re-encodes it differently, e.g. a block scalar); rewrite that value as a quoted string, or as a block scalar without a leading blank line or extra-indented lines, then re-run hermes-setup
  ```

  Rewrite that value as a quoted string, or as a block scalar without a
  leading blank line or extra-indented lines, then re-run. The same check
  covers the token edit of `unreagent.local.yaml` (it then reads `more than
  mcp.token`, and the line starts with `add the token to <path>: `); a
  value it cannot name ends at `more than mcp_servers.unreagent` alone.
  The same check names a block scalar inside the `mcp_servers.<name>` entry
  itself, as `the result would not hold the new mcp_servers.unreagent: the
  value at mcp_servers.unreagent.description would change (...)`, with the
  same fix. Only a token edit, or a difference it cannot place, keeps the bare
  `the result would not hold the new ...` message.
- the Hermes config has a duplicate key anywhere, or cannot be parsed: the
  message is `Hermes config <path> not parseable: ...`;
- the Hermes config holds more than one YAML document (a `---` followed by
  content, even only `--- ~` or `--- # note` on that line), since the rewrite
  would drop all but the first. A bare trailing `---` or `...` at the end of
  the file is not refused, and neither is a comment directly below a single
  trailing `---`; a blank line, a `...` or another `---` before such a
  comment starts a second document, which is. The message is `Hermes config
  <path> not parseable: it holds more than one YAML document (another one
  starts at line N); refusing to drop all but the first`, with N the line of
  the next document's `---`. `unreagent.yaml` and `unreagent.local.yaml` are
  refused the same way when the config loads, before `hermes-setup` does
  anything. Merge the documents into one, or delete the extra ones, and
  re-run;
- the Hermes config is a dangling symlink (a link whose target does not
  exist) or one that cannot be resolved. The message names the link and the
  target as the link stores it, so a relative target prints relative, as
  `ls -l` shows it:

  ```
  Error: <link> is a symlink to <target>, which does not exist; refusing to replace the link with a new file (nothing written)
  ```

  The exit code is 1; the link and both files stay as they were, with no
  `.bak` and no temp file. The target was moved, renamed or deleted, the link
  has a typo, or the target lives somewhere unavailable right now (for
  example an unmounted drive or a missing dotfiles checkout): bring it back,
  point the link at a real file, or delete the link so `hermes-setup` writes
  a new file, and re-run. When the link points at another link, the message
  names the first hop and the missing end of the chain:

  ```
  Error: <link> is a symlink to <hop1>; following the links leads to <missing>, which does not exist; refusing to replace the link with a new file (nothing written)
  ```

  A link that cannot be resolved at all, such as a loop, reads
  `<path> is a symlink that cannot be resolved: ... (nothing written)`; break
  the loop and re-run.

  A dangling or looping `unreagent.local.yaml` stops the config load first
  (`config not readable: <link> is a symlink ...`, without "(nothing
  written)"); nothing is written then either.

Fix the file by hand and re-run. A token generated in step 3 is **not** saved
in these cases: it goes to `unreagent.local.yaml` only after the Hermes merge
has succeeded. An `unreagent.local.yaml` that cannot be parsed, or whose `mcp`
is not a mapping, is refused earlier still, when the config loads.

**When a write fails.** The token is saved first, so the Hermes config never
holds a token the launcher does not know. If the Hermes write then fails, the
error says that the new token is already in `unreagent.local.yaml`: fix the
cause and re-run, which reuses the saved token. If writing
`unreagent.local.yaml` itself fails, the Hermes config is left as it was (the
`.bak` may already exist). Writes need a writable *directory*, not just a
writable file, because of the temp file: a read-only Hermes config
directory is refused, and so is a read-only project directory (the one
holding `unreagent.local.yaml`) — the latter only when a token has to be
generated and saved. The error names the directory. A symlinked config file
is followed and its target replaced, so the link stays a link; a dangling
one is refused (see above). A path that goes through a symlinked directory is
used as given. A replaced file keeps its permission
bits and a new file gets mode 0600, except that an existing
`unreagent.local.yaml` open to group or others is tightened to 0600 when the
token is written into it (step 7). Modes do not apply on Windows; the
rename-replace step there is the same code path, but has only been compiled,
not run.

**Recovering after 0.0.9 or earlier.** Those releases moved the Hermes
config to `<path>.bak` and wrote a new file holding only the unreagent entry. If
that happened to you, the original is in `config.yaml.bak` next to the config
(`~/.hermes/` by default); later runs never overwrite that
backup. Copy it back, then run `hermes-setup` again — it now merges.

The MCP server enforces `Authorization: Bearer <token>` whenever
`mcp.token` is set — including for the embedded Claude Code agent, which
gets the matching `headers:` block in its `--mcp-config` automatically.

The agent gets that config as a file, never inline, so the token is not in its
process arguments, which other local users can read (`/proc/<pid>/cmdline`,
`ps`). At startup the launcher creates a directory `unreagent-<random>` in the
temp directory (`TMPDIR`; `%TMP%` on Windows) and writes `mcp-config.json`
into it, then starts the agent with `--mcp-config <that path>`. On Unix the
directory is `0700` and the file `0600`; on Windows the mode bits do not
apply and the per-user `%TMP%` keeps it private. The file lives for the whole
launcher run, so agent restarts reuse it, and the launcher removes the
directory when it exits. If removal fails, a `WARN` names the directory (in
the log; in window mode also on stderr), and what is left there can hold the
token: delete it by hand. A launcher that is killed hard cannot clean up and
leaves the directory behind. A temp directory the launcher cannot write to
stops it at startup with exit code 1, naming the path and the cause; there is
no fallback to inline JSON. Point `TMPDIR` (`TMP` on Windows) at a writable
directory to fix it. Windows: a temp path containing a space, passed through
a `.cmd` agent such as `claude.cmd`, is unverified.

The logged `Agent command:` line shows the launcher's own config path after
`--mcp-config`. A `--mcp-config` you wired into `agent.args` by hand stays
`<redacted>` there, and the token is blanked in every other argument, so the
token does not reach `unreagent.log`; the agent itself still gets the real
arguments. Config files written for
external clients (`mcp.writeConfig`, `-write-mcp-config`) carry the token and
are created `0600`; see the `mcp.writeConfig` paragraph near the top.
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

## Skills

unreagent ships UE skill documents inside the `.exe`: operating instructions
for one kind of work each (which tools to call, in what order, what their
errors mean). Two paths bring them to the agent; they can run together.

| Path | Setting | What happens | Use it when |
|---|---|---|---|
| MCP tool (default) | `skills.mcp: true` | the `skills` tool returns a skill by `name` (and a sub-document by `section`). Its description lists every skill with its one-line description, so the agent chooses without an extra round trip | any agent; nothing is written to disk |
| Files | `skills.materialize: true`, or the `-no-skills-mcp` flag (which also turns the MCP tool off) | every start, the skills are written to `skills.dir` | the agent reads skills from disk itself (Claude Code reads `<project>/.claude/skills`, and can invoke one as `/skill-name`) |

`skills.dir` defaults to `${PROJECT_DIR}/.claude/skills`. UE 5.8 reads skills
from `<Project>/Skills/`; point `skills.dir` at `${PROJECT_DIR}/Skills` to
target it. With `skills.enabled: false` neither path runs. Switching both
paths off while `skills.enabled` is `true` is a load error:

```
skills.mcp and skills.materialize are both false, so no skill would reach the agent — set skills.mcp: true (the skills MCP tool), skills.materialize: true (files in skills.dir), or skills.enabled: false to turn skills off
```

**What materialization owns.** Each skill goes to `<dir>/<name>/` with its
`SKILL.md` plus its sub-documents; the directory carries the `unreagent-`
prefix (`ue-materials` becomes `unreagent-ue-materials`; a name that already
starts with it, such as `unreagent-files`, is used as is, and its `SKILL.md`
is written exactly as shipped). When the prefix is added, the `name:` in the
written `SKILL.md` frontmatter is set to the directory name, so name and
directory match as the Agent Skills spec requires; nothing else in the file
changes. Skill texts refer to each other by their shipped names (`ue-tasks`),
which on disk are `unreagent-ue-tasks`. The `skills` MCP tool keeps the
shipped names.
On every start the launcher:

- replaces every directory in `skills.dir` whose name starts with `unreagent-`
  (a hand-edited file inside one is lost). Each skill is written whole into a
  work directory `unreagent-.tmp-*` inside `skills.dir` and then renamed into
  place, so two launchers started at once do not leave a half-written skill,
- removes such a directory that is no longer shipped, so renamed or dropped
  skills do not linger; the next start also removes any `unreagent-.tmp-*`
  work directory a crash left behind,
- never touches anything else in `skills.dir`: your own skills and any other
  file or directory stay as they are.

Do not put your own skills under an `unreagent-` name in that directory.

**Failure modes.** A skill set that does not load, or two skills that would land
in the same directory (names that differ only in case count as the same), stops
the start with an error, and so does a section file named like a Windows device
(`con`, `aux`, `nul`, `com1`, `conout$` and so on, also with an extension
such as `con.backup`), on every OS; `skills.enabled: false` starts without
skills. A write that fails (read-only directory, a file in the way) does not
stop the start; it is logged:

```
WARN skills not fully written to <dir>: <cause> — its unreagent-* directories may be missing, incomplete or left from an earlier start
```

The editor and agent start anyway, but the `unreagent-*` directories in
`skills.dir` may then be missing or from the previous start: each skill is
replaced whole, so a skill that fails to write keeps its previous copy (rarely
none), the other skills are still written, and a removal that fails leaves the
previous start's files. Each skill gets three attempts, with short pauses
between them; if no work directory can be created in `skills.dir` at all (a
read-only directory), the launcher gives up at once with one cause ending in
`(N of M skills not written)`. When several skills fail, all their errors are
joined on the one warning line, separated by `; `. In the rare case that
putting the new copy in place fails and moving the previous copy back fails
too, the warning says the previous copy is kept in `unreagent-.tmp-*/previous`
inside `skills.dir`; a later start removes it (a second launcher starting at
the same moment can remove it sooner). Two launchers of different versions on
one project can still remove each other's extra skills.
The `skills` MCP tool, when on, is unaffected, since it serves from the binary.

With `mcp.enabled: false` the `skills` tool cannot be served; unless the skills
are materialized the launcher logs
`WARN no skills reach the agent: the skills MCP tool needs mcp.enabled — enable it, set skills.materialize: true, or skills.enabled: false`.

## MCP tools

| Tool | Function |
|---|---|
| `status` | process status + available commands; `lastError` is why the last start failed (absent once a start succeeds); `lastExit` is how the last run that ended on its own ended, e.g. `exit 1` (absent until one ends; `ue_start` / `ue_restart` clear it) — a service whose control loop misses the internal 10s timeout is reported as `"unresponsive": true` (`running`/`pid` unknown, sent as `false`/`0`) instead of being dropped from the list, with `"busy": "pre-start"` next to it while that loop is still running the editor's pre-start step (crash-reporter kill, crash cleanup) — a slow start, not a hung loop |
| `ue_start` / `ue_stop` / `ue_restart` | editor lifecycle. If the editor is not running after `ue_start` / `ue_restart`, the result is an error, `Error: service "ue" did not start: <lastError>`, followed by the status JSON (e.g. a missing `unreal.editor` program). Running only means the editor was spawned: for one that dies at once, check `status` after a few seconds (`lastExit`) |
| `run_command` | run a preconfigured command (compile, package). The reply is the exit line and the output: on success its last 300 lines within 48 KiB; on failure at most 10 KB: the whole output if it fits, else its first 4 KiB (where the first errors are), a marker counting what is left out, and as much of its end as fits. Claude Code itself cuts an error reply over about 11,000 characters to its first and last 5,000, which would drop a failing build's first errors. What is left out is not kept: re-run narrower, or have the command write to a file and read it with `read_file` |
| `logs` | last output lines of a service |
| `run_python` / `run_node` | run code in a prepared environment; the reply is shaped like `run_command`'s |
| `read_file` / `list_dir` / `write_file` / `edit_file` | file access on the project (only with `files.enabled` / `-files`); `read_file` takes optional `offset` / `limit` in bytes (a negative `offset` counts from the end, so `offset=-48128` is the last 47 KiB of a log; `limit` defaults to and is capped at 47 KiB, so one reply with its trailer stays within the 48 KiB a tool reply is held to) and ends a partial result with `[bytes X-Y of Z]`, or `[bytes X-Y of Z; continue with offset=Y]` when more follows. Confined to `files.root`: paths are relative to it, and an absolute path is accepted only under it. Refused: any other absolute or rooted path ("must be relative to the project root"), a relative path that climbs out with `..`, a Windows reserved device name (`CON`, `NUL`, `AUX`, `COM1`, `aux.h`, …) in any path component (on every platform), and any symlink or Windows junction on the path that is absolute or leads outside the root — also on the way to a file that does not exist yet. A plugin linked into `Plugins/` from outside the root is therefore out of reach through the link: put the plugin into the project, or set `files.root` to a directory holding both and reach the plugin at its real path (see File-tool confinement below) |
| `skills` | load a shipped skill document by `name`, or one of its sub-documents by `section` (with or without `.md`). The tool description lists every skill with its one-line description, so the agent can pick one without a list call. Only with `skills.enabled` and `skills.mcp`, see [Skills](#skills) |
| `approve` | permission prompt tool for Claude Code |

### File-tool confinement

`read_file`, `list_dir`, `write_file` and `edit_file` never leave `files.root`.
Each call opens the root afresh and resolves the path inside it, link by link,
so a link cannot be swapped in between the check and the use.

The confinement covers path resolution only. A hard link inside the root is a
second directory entry for a file that may live elsewhere, and on Linux a bind
mount inside the root leads out of it; neither is detected, so do not put one
there.

- A relative path is taken against the root. An absolute path is accepted only
  when it lies under the root; any other absolute or rooted path is refused:
  `path "/etc/passwd" must be relative to the project root "<root>"; an absolute path is accepted only under it`.
- A relative path that climbs out (`../x`) is refused: `path outside the allowed root`.
- A symlink or Windows junction on the path is refused when its target is
  absolute or leads outside the root. A junction always has an absolute target,
  so it is refused even when it points back into the root. The check also
  covers the way to a file that does not exist yet, so `write_file` cannot
  create a file through such a link:
  `a link on the path (symlink or junction) is absolute or leads outside the allowed root "<root>"`.
  A relative link that stays inside the root works.
- A Windows reserved device name (`CON`, `PRN`, `AUX`, `NUL`, `COM1`-`COM9`,
  `LPT1`-`LPT9`, also with an extension such as `aux.h`) in any path component
  is refused: `path component "aux.h" is a reserved Windows device name`. The
  check runs on every platform, because such a file created on Linux cannot be
  checked out on Windows.

**A plugin linked into `Plugins/` from outside the project.** A common layout
links a shared plugin into `<Project>/Plugins/` with a symlink or junction. The
file tools cannot reach through that link, and a read of it fails with the link
error above. Either put the plugin into the project as a real directory, or
set `files.root` to a directory that holds both the project and the plugin's
real location. A wider root does not make the link itself work — a junction
or absolute symlink is still refused — so the plugin is then reached at its
real path, and every path gains the project directory as a prefix
(`MyGame/Source/...`).

On Windows the junction and reserved-name handling is built but not yet
verified on a Windows host; the behavior above is exercised on Linux only.

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
  are unaffected; `mcp.token` stays the access control. The allowlist is
  configurable, see below.
- `initialize` negotiates `protocolVersion` against `2025-06-18` (default)
  and `2025-03-26`; any other value is answered with the default rather than
  echoed back, since this server implements only the Streamable-HTTP
  transport (no SSE, no sessions).
- Connection timeouts guard against a wedged socket: `ReadHeaderTimeout` 10s,
  `ReadTimeout` 60s, `IdleTimeout` 120s. `WriteTimeout` is 4h — the ceiling
  for a single tool call, so a long-running `run_command` (e.g. a full UE
  build) has room; it is a backstop, not a request budget.

#### Allowing a browser-based MCP client (`mcp.allowedOrigins`)

Only browser-hosted MCP clients send an `Origin` header. To serve one from a
non-loopback origin, list it:

```yaml
mcp:
  allowedOrigins:
    - https://ide.example.com
    - http://localhost:5173   # keep a loopback client working, see below
```

- **Absent by default.** Without the option the built-in allowlist applies:
  `http`/`https` on `localhost`, `127.0.0.1` and `[::1]`, any port.
- **It replaces, it does not add.** A non-empty list replaces the built-in
  allowlist, so list every origin you still need, loopback ones included
  (`http://localhost:5173` above). Origins not on the list get `403`.
- **Entry form.** Each entry is an origin exactly as the browser sends it:
  `scheme://host[:port]` with `http` or `https`. Refused: a path (not even
  `/`), a query, a fragment, user info, wildcards, a character the URL
  Standard forbids in a host (`<`, `>`, `^`, `|`) and a `%` in the host
  (`%25` included). Leave out the default port (`https://h`, not
  `https://h:443`) and leading zeros in a port, and write an international
  host as punycode (`https://xn--...`). Matching is
  exact on the whole string and ignores case. No placeholders are
  substituted.
- **Browser extensions.** Any other scheme with a host is accepted as exactly
  `scheme://host`, with no port, user info, path, query, fragment, wildcard
  or character the URL Standard forbids in a host (`<`, `>`, `^`, `|`):
  `chrome-extension://<id>`, `moz-extension://<uuid>`,
  `vscode-webview://<id>`. Still refused, each with its own cause: `null`
  (every sandboxed frame sends it), a missing scheme, and `file`, `ftp`, `ws`
  and `wss`, which a browser never sends as an Origin.
- **IP hosts in canonical form.** A browser rewrites an IP host before it
  sends the Origin, so write the rewritten form or the entry never matches.
  IPv4 is a dotted quad without leading zeros; `127.1`, `0x7f.0.0.1`,
  `2130706433` and `192.168.001.020` (octal to a browser) are refused with
  the form to write instead. IPv6 is bracketed, lowercase and compressed
  (`http://[::1]:5173`, not `[0:0:0:0:0:0:0:1]`), an IPv4-mapped address is
  the hex form `[::ffff:7f00:1]`, and a zone is refused.
- **Overlay.** In `unreagent.local.yaml` the list replaces the base list
  whole; it does not append. `allowedOrigins: []` or `~` there clears it and
  brings the built-in loopback allowlist back.
- **Scope.** Requests without an `Origin` header (Claude Code, Hermes, `curl`)
  are always accepted; the list changes nothing for them. Set `mcp.token` when
  a non-loopback origin may reach the server.

An invalid entry is a load error naming its index, value and cause, checked
only while `mcp.enabled` is on. A trailing `/` in the first entry:

```
Error: mcp.allowedOrigins[0] invalid: "https://ide.example.com/" (no path allowed, not even a trailing /; want scheme://host[:port] (http or https) or scheme://host for a browser extension, e.g. https://ide.example.com)
```

A non-canonical IP host in the first entry:

```
Error: mcp.allowedOrigins[0] invalid: "http://127.1:5173" (non-canonical IPv4 host, write it as 127.0.0.1, the form a browser sends; want scheme://host[:port] (http or https) or scheme://host for a browser extension, e.g. https://ide.example.com)
```

When a list is set the startup log names it:
`MCP server: browser Origins allowed: https://ide.example.com, http://localhost:5173 (mcp.allowedOrigins, replacing the built-in loopback origins)`.
A browser client that gets `403 Forbidden` has an origin that is not on the
list. Compare what the browser sends with the entry: `http` vs `https`, a
different port, a loopback origin you left off the list (the built-in ones are
gone once the list is set), or a different extension id. Uppercase is fine.

## CLI flags

| Flag | Effect |
|---|---|
| `-config <path>` | alternative path to `unreagent.yaml`; a relative path is made absolute against the working directory before anything is derived from it |
| `-no-agent` | **do not** start the agent — only UE + MCP server. An external agent (your own Claude Code session) then connects to the MCP server. |
| `-files` | enable the file tools (`read_file` / `write_file` / `list_dir` / `edit_file`) even if off in the config. Refused with a warning (file tools stay off) when `files.root` contains a placeholder that did not resolve — set `unreal.project` or an explicit `files.root`. |
| `-no-skills-mcp` | deliver the skills as files in `skills.dir` instead of through the `skills` MCP tool (`skills.mcp: false`, `skills.materialize: true`). It is applied after both YAML files are loaded, so it wins over `unreagent.yaml` and `unreagent.local.yaml`; with `skills.enabled: false` it has no effect and says so in the log. See [Skills](#skills). |
| `-write-mcp-config <path>` | additionally write the assembled MCP config as `.mcp.json` to `<path>` (for external clients). Created `0600` and replaced atomically through a temp file in its directory, which must be writable (an existing file open to group or others comes out `0600`; Unix only), since it holds `mcp.token` when set; with `mcp.token` set, a target inside the project that git does not ignore gets a `WARN` (in the log; in window mode also on stderr). |

## Sub-commands

| Sub-command | Purpose |
|---|---|
| `unreagent hermes-setup` | One-shot merge of the unreagent MCP server into Hermes Agent's `config.yaml` (idempotent; auto-generates `mcp.token` if missing; needs a resolvable engine, like a normal start). See "Hermes Agent" above. |

The sub-command is the first argument that is neither a flag nor a flag's
value. Flags may stand before or after it and all go to the sub-command.
`hermes-setup` takes only `-config` and `-h`. Any other flag, a launcher flag
such as `-no-agent` included, is a usage error (exit 2) on either side of the
word, printed with `hermes-setup`'s usage; a flag the launcher does not know at
all, put before the word, is refused by the launcher instead, with its own
usage. A word after the sub-command is a usage error too, printed as one line:
`hermes-setup takes flags only, not "<word>"`. `unreagent -h` prints the
launcher's usage with one line per sub-command, `unreagent hermes-setup -h`
that of `hermes-setup`; both exit 0. A first word that names no sub-command
is a usage error (exit 2) that names it and lists the sub-commands:
`unknown sub-command "<word>" (sub-commands: hermes-setup)`. If
the word is an existing path, as for a file dropped onto the .exe, the message
says instead that unreagent does not open files passed to it and reads
`unreagent.yaml` next to the .exe, or the file given with `-config <path>`.
On Windows a console opened by Explorer may close before the message can be read.

### How the agent runs (three modes)

Claude Code is an interactive TUI and needs a TTY — as a raw background
subprocess it won't start. Hence three ways:

1. **Interactive in the foreground** (`agent.window`, **on by default**) — the
   agent inherits the real console (TTY) and runs as an interactive TUI in
   the window you started `unreagent` in. UE + MCP run in the background,
   launcher logs go to `unreagent.log` (next to the project; the previous
   session's log is kept as `unreagent.log.1`, one generation, replaced on
   each start) so the agent has the window to itself. If the old log cannot
   be moved aside, a warning says so and the new session is appended to
   `unreagent.log`. The log also holds the startup lines the console showed
   before the switch (config, engine, project and their warnings, such as a
   fallback engine), with their original times. The `Agent command:` line in
   that log shows the launcher's own `--mcp-config` file path, any other
   `--mcp-config` value as `<redacted>`, and the token is blanked in any other
   argument; the agent still gets the real arguments. The launcher console's
   replies (see [Manual control](#manual-control-stdin)) show on the console as
   well as in the log. If the log cannot be created, the launcher prints a
   warning to stderr and its logs go to the console instead. Disable with `window: false`; off in `-p` mode.
   An interactive agent is not subject to the permission policy: with
   `permissions.enabled` the launcher logs a `WARN` at startup, see
   [Permission layer](#permission-layer).
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

If the address cannot be bound the launcher exits 1 before it writes any file.
The error starts with `Error: MCP server cannot listen on 127.0.0.1:8765:` and
goes on with the system's reason and what to do about it. The reason is the
operating system's own text: on Linux a taken port reads
`address already in use`, while Windows words it in the system language.
Stop the other program (often a second unreagent), or set `mcp.address` to a
free `host:port` (for this machine only, in `unreagent.local.yaml`).
An `mcp.address` that is not `host:port` never gets that far:
`Error: mcp.address invalid: "127.0.0.1" (missing port in address; want host:port, e.g. 127.0.0.1:8765 or [::1]:8765)`.

If the server stops serving after a successful bind, the launcher does not
exit: it logs one `ERROR` line (in window mode also on stderr) saying that the
agent's unreagent tools and permission prompts no longer work, and keeps
everything else running: the editor, and the agent if one is configured. Save
your work in the editor, then restart unreagent; there is no automatic rebind.
The one exception is a launcher with no agent whose editor's last run was a
quick failure with no restart following (see Exit codes): it ends with exit 1
right after that line.

For access from another machine set `mcp.address` to `0.0.0.0:8765` (only
in trusted networks — the server then has file-write access; set `mcp.token`).
The agent, the `mcp.writeConfig` files and `hermes-setup` still get
`http://127.0.0.1:8765/mcp`, which serves clients on this machine; a client on
another machine uses this machine's LAN address, `http://<lan-ip>:8765/mcp`.
The startup line names both: `MCP server listening on 0.0.0.0:8765 — clients connect to http://127.0.0.1:8765/mcp`.

## Manual control (stdin)

`status` · `r [name]` (restart) · `start <name>` · `stop <name>` ·
`c <name>` (run command) · `q` (quit)

The console answers each command on the console itself; in window mode, where
the launcher log goes to `unreagent.log`, the same line is logged there too.
That covers the command list, the `status` JSON, `c <name>` results,
`Error: …`, `Unknown command: …`, `Stopping …` and
`Launcher console active — …`. A `start`, `r <name>`, `r` or `restart` that
leaves the service not running answers `Error: service "ue" did not start:
<lastError>`; `start` or `stop` of a name that is not a service answers
`Error: unknown service "nosuch"`. `status` shows `lastExit` for a run that
ended on its own.

In the launcher console opened with `k` in window mode, `start agent`,
`r agent`, `restart agent` and a plain `r` hand the terminal back to the agent
instead of reading stdin alongside it; a plain `r` restarts the other services
first. The agent's next exit shows the exit prompt again. (Console handover is
unverified on Windows.)

In window mode a `start`, `stop`, `r` or `restart` that went through also
answers on the console: `<name> is running (pid N)`, `<name> restarted (pid N)`
or `<name> stopped`. For a start, stop or restart it carried out, the log has
the supervisor's own line; a `start` of a service that is already running only
answers on the console. The `agent.onExit: leave` and `shutdown` lines show
on the console as well as in the log. (Window-mode console lines are
unverified on Windows.)

## Manual MCP test

```bash
curl -s -X POST http://127.0.0.1:8765/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```
