---
name: unreagent-ue
description: >
  The Unreal Engine and UE LLM Toolkit facts unreagent's Go code depends on,
  each with its source or the label unverified. Use when changing editor
  start, stop or restart, crash and recovery handling, engine detection, the
  build command lines, the bridge smoke test or editor readiness, or when a
  card turns on "what does UE do here".
---

# unreagent — what UE and the toolkit actually do

Nobody on this team has an editor at hand: every UE fact here comes from text.
State a fact from this file with its status, never stronger. Found a new one?
Add it with its source, or as unverified with the reason.

**Status.** `source` — read in source code: the toolkit (`../ue-llm-toolkit`
at 9f6a0f7; `T/` = `Plugin/UELLMToolkit/`), this repo, or Epic's public
`tk-unreal` ([tk], an Epic tool, not the engine). `doc` — Epic's documentation
says so (page re-read 2026-11-21); a documented contract, not observed
behavior. `research` — `docs/research/2026-11-21-unreal-editor.md` (R§n) cites
a page that was not re-read. `unverified` — no source holds; U1–U8 are that
document's experiments, all waiting for a Windows machine with an editor. The
engine source was not readable for the research or for this file.

Not repeated here: what the agent is told about builds, restarts and the
editor tools (`skills/unreagent-build`, `skills/unreagent-iterate`,
`skills/ue-python`, `skills/ue-*`); Job Objects and Win32 (skill
`unreagent-windows`); config semantics (skill `unreagent-config`).

## Editor launch and `-unattended`

**The obvious assumption:** `-unattended` means "no dialog, ever". Epic
documents it as a mode that cooperating code asks about; nothing lists which
dialogs honor it.

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| The command line is `UnrealEditor.exe <.uproject> [flags]`, project first | `run()` in `cmd/launcher/main.go` prepends `unreal.project` | R§1 [cmd57] | research; how the argument is parsed: open, U1 |
| `unattended`: "Run in unattended mode … Use this to disable UI pop-ups and other dialogs" | `run()` appends `-unattended` (`UnrealConfig.Unattended`, default true) | [flags] | doc |
| `-unattended` alone declines package recovery ("both recovery systems") | comment on `UnrealConfig.Unattended`; the log line in `run()`; README "Crashes & recovery", which says it was checked against the UE 5.7 source but names no file | Epic lists a separate flag, `AutoDeclinePackageRecovery` ("Auto decline package recovery") [flags] | **unverified** — no engine file recorded, and `cleanRecovery`'s own comment assumes the restore dialog still blocks; U2 |
| `-stdout`: "Use stdout for log output"; `-StdOut -FullStdOutLogOutput` together give "the maximum logging output" | default `unreal.args` in `applyDefaults` (`internal/config/config.go`) | [flags], [pixel] | doc |
| The flag table spells the second flag `fullstdoutlogoutputs`; whether both spellings work | same default (singular) | [flags] vs [pixel] | unverified; U3 |
| Flags are matched without regard to case | `hasArg` compares exactly, so `-Unattended` in `unreal.args` gets a second `-unattended` | none | unverified |
| A start that will fail does so within seconds | `FastFailWindow` (10 s) and `fastFailLimit` in `internal/supervisor/supervisor.go`: a failure after 10 s never counts toward the give-up | none; R§6 found no timing for any failure path | unverified; U5, U8 |

## Crash artefacts and the reporter

**The obvious assumption:** the reporter is a window that appears after a
crash, and a folder in `Saved/Crashes` is a finished report of a crash.

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| Editor crash reports are stored "in your project's `Saved/Crashes` folder" (a second, per-user location exists for shipped games) | `cleanRecovery`, `makeEditorExitHandler` (`cmd/launcher/lifecycle.go`) | [crash] | doc |
| The reporter is `CrashReportClient`, "`CrashReportClientEditor` for the Editor" | `crashReporterKillers` kills both image names | [reporter] | doc (the `.exe` names and their paths: unverified, U4) |
| The reporter "can be configured to wait for crash", capture the stack and write the minidump: it may run for the editor's whole life, not only after a crash | `editorPreStart` treats it as a leftover window. Inference, not observed (k154): if a reporter runs alongside every editor, `taskkill /IM` on a start that does not follow a crash also hits the reporter of any other editor on the machine | [reporter]; which mode a stock editor uses: unknown | doc / unverified; U4 |
| Unattended reporter: `bSendUnattendedBugReports` (default true) makes it "skip displaying to the user" | the reason the kill is only a backup | [crash] | doc; how the editor's `-unattended` reaches the reporter: unverified |
| An ensure sends a report without ending the editor, so a new report does not prove a crash | anything that reads `Saved/Crashes` as "it crashed" | [crash] | doc |
| When a report is complete (which file is written last, any sentinel) | `followsCrash` spares the reporter instead of waiting for it, because nothing says when it is done | none | unverified; U4 |
| `Saved/Autosaves/PackageRestoreData.json` triggers the restore prompt, and deleting it prevents it | `cleanRecovery` | none; the path came with the research question | unverified; U2 |
| Epic has its own flag `CleanCrashReports` ("Clean crash report folder located in `../Saved/Crashes`") | not used; `cleanRecovery` does it by hand | [flags] | doc |

History: k77, k128 and k153 each destroyed crash evidence by treating one more
kind of start as "clean". Which starts spare it is launcher logic: `followsCrash`.

## Engine discovery

**The obvious assumption:** `EngineAssociation` is a version, and one registry
key finds every build.

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| `EngineAssociation` is a version ("5.5") for a Launcher build, a "random unique identifier" for any other build of a foreign project, and blank for a non-foreign project, whose editor sits at a known relative location up the project's directory hierarchy (paraphrased) | `classifyAssociation`, `chooseEngine` (`internal/config/engine.go`). **A blank one falls back to the highest installed engine; the directory hierarchy is never searched** | [installed] | doc |
| A build identifier need not be a GUID (Epic's example: `MyCustom419` → `D:\CustomUE`) | `classifyAssociation` treats every non-version as a build; `normalizeBuildID` only strips braces and case | [installed] | doc |
| Builds key: `HKCU\SOFTWARE\Epic Games\Unreal Engine\Builds` (with a space) on one Epic page, `…\Epic Games\UnrealEngine\Builds` (no space) on another. A third place: Epic's `tk-unreal` also reads a `Builds` subkey under `HKLM\SOFTWARE\EpicGames\Unreal Engine`, taking every value's data as an engine root | `buildsRegKey` reads the HKCU spelling with the space only; `launcherInstalls` skips the HKLM `Builds` subkey (it has no `InstalledDirectory`) | [manage] vs [installed]; [tk] `EngineLauncher._find_software_from_registry` | **unverified** which the engine reads — the pages disagree; U6, k204 |
| Launcher installs: subkeys of `HKLM\SOFTWARE\EpicGames\Unreal Engine`, value `InstalledDirectory` | `launcherRegKey`, `launcherInstalls` | Epic's own `tk-unreal`, `startup.py`, `EngineLauncher._find_software_from_registry` [tk] | source (an Epic tool reads it; that every install writes it: unverified, U6) |
| Default root `C:/Program Files/Epic Games/UE_<version>`, editor at `Engine/Binaries/Win64/UnrealEditor.exe` | `defaultEnginePatterns`, `hasEditor` | [tk] `EXECUTABLE_TEMPLATES` | source for the `C:` pattern; the `D:`, `E:` and `<drive>:/Epic Games` patterns are convention without a source |
| The Launcher also records installs in ProgramData manifests and `EpicGamesLauncher\Overrides\UE_<version>` keys | not read | R§4 [launcher] | research |
| Opening a project in another engine may prompt to convert it | the warning in `chooseEngine` (k88) | R§4, for the Project Browser only | unverified for `UnrealEditor.exe <project>`; U6 |
| Encoding of a `.uproject` | `parseEngineAssociation` accepts UTF-8 with or without a BOM; anything else reads as "no association", silently | none | unverified; U6 |
| `reg.exe` output for a non-ASCII path | `regQuery`, `parseRegQuery` | k84 note | unverified |

History: k84 (fixed version list, lexical order), k88 (silent fallback).

## Build command lines and Live Coding

**The obvious assumption:** the two defaults are documented command lines.
Only their skeleton is.

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| `Build.bat <Target> <Platform> <Configuration> … -WaitMutex -FromMsBuild` is a form Epic prints | the `compile` default in `applyDefaults` | [sanitizers] (one example, for a Linux game target) | doc for the form |
| The editor target is `<ProjectName>Editor` | same: `${PROJECT_NAME}Editor` | none; nothing read here ties a target's name to the project's, or says every project has an editor target | unverified |
| What `-WaitMutex` waits for | none in Go; `skills/unreagent-build` says only that `compile` passes the flag, and treats a compile that does not come back as possibly waiting on another UnrealBuildTool | [sanitizers] shows the flag and explains nothing | unverified; U7 |
| UnrealBuildTool refuses to build while Live Coding is active, and the text of its message | none; the `run_command` description in `cmd/launcher/tools.go` and `skills/unreagent-build`, `-iterate` and `-files` give the stop-before-compile order for a reason that does not depend on it (the editor has the modules loaded), and call the outcome, fail or wait, unverified | R§5 [live57] documents Live Coding, not the refusal | unverified; U7 |
| `RunUAT.bat BuildCookRun -project=… -clientconfig=…` builds, cooks and packages | the `package` default | R§5 [bcr57] | research; `-noP4 -build -cook -stage -pak -archive -archivedirectory=` were not checked by anyone |

## Toolkit server and node bridge

**The obvious assumption:** a TCP connect on 3000 proves the editor's tools
are there. It proves that something listens on 3000, a port many dev servers
default to.

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| The server listens on port 3000; `-MCPPort=<n>` on the editor command line overrides it | `unreagent.example.yaml` (`UNREAL_MCP_URL`); the launcher never passes `-MCPPort` | `T/Source/UELLMToolkit/Public/UnrealClaudeConstants.h` `MCPServer::DefaultPort`; `Private/UnrealClaudeModule.cpp` `GetMCPServerPort` | source |
| It starts in the module's startup (editor module, `LoadingPhase: PostEngineInit`). When `GetHttpRouter` returns nothing, `Start` returns false, and the toolkit logs an error and the editor runs on without the server | `waitForEndpoint` then waits out its window | `T/UELLMToolkit.uplugin`; `UnrealClaudeModule.cpp` `StartMCPServer`; `FUnrealClaudeMCPServer::Start` | source, for the router-failure path only |
| A failed bind (port 3000 already taken): `Start` calls `StartAllListeners()`, sets `bIsRunning` and returns true without checking, so the toolkit reports nothing | `waitForEndpoint` connects to whoever holds the port and logs "is reachable" | `FUnrealClaudeMCPServer::Start`; whether UE's HTTP module logs the failure is unread engine code | unverified |
| Routes: `GET /mcp/status`, `GET /mcp/tools`, `POST /mcp/tool/<name>` | none yet | `Private/MCP/UnrealClaudeMCPServer.cpp` `SetupRoutes` | source |
| `/mcp/status` answers `status`, `port`, `version` (`"1.0.0"`), `toolCount`, `tools`, `projectName`, `projectDirectory`, `engineVersion`. The tools are registered in the server's constructor, before it listens | `waitForEndpoint` (`cmd/launcher/bridge.go`) makes a bare connect and logs "is reachable"; only this route tells the toolkit from another listener | `HandleStatus`; `FUnrealClaudeMCPServer()`; `MCPToolRegistry.cpp` `FMCPToolRegistry()` | source |
| A tool runs on the game thread; the HTTP handler waits 30 s (`GameThreadTimeoutMs`), then answers an error while the tool may still run. A failed tool is HTTP 400 with `{success, message, data}` | none in Go | `HandleExecuteTool`; `UnrealClaudeConstants.h` | source |
| Which interface the server binds | the "localhost only?" hint of `editorEndpoint` | not in the toolkit; it is UE's `FHttpServerModule` | unverified |
| The bridge needs `npm install`: one runtime dependency, `@modelcontextprotocol/sdk`, plus the devDependencies `vitest` and `@vitest/coverage-v8`; no `node_modules` in the repository, Node ≥ 18 | `prepareNodeBridge` runs a plain `npm install`, which fetches the devDependencies too — that is what `bridgeInstallTimeout` has to cover; `bridgeInstallMarker` | `T/Resources/mcp-bridge/package.json` | source |
| The bridge answers `initialize` (`serverInfo.name` `ue5-mcp-server`) without contacting the editor | `smokeTestMCP`, `initResponse`: a pass proves node and the dependencies, nothing about the editor | `mcp-bridge/index.js` `main` | source |
| Environment: `UNREAL_MCP_URL` (`http://localhost:3000`), `MCP_REQUEST_TIMEOUT_MS` (30000), `MCP_ASYNC_ENABLED` (on unless `"false"`), `MCP_ASYNC_TIMEOUT_MS` (300000), `MCP_POLL_INTERVAL_MS` (2000), `INJECT_CONTEXT` (off unless `"true"`). A number that does not parse, or 0, means the default | `prepareMCPBridges` reads only `UNREAL_MCP_URL` from the entry's `env`; unset, no endpoint is watched | `index.js` `CONFIG` | source |
| Every tool call goes through the queue, except `task_*` and the two the bridge answers itself before that branch (`unreal_status`, `unreal_get_ue_context`): `POST /mcp/tool/task_submit` `{tool_name, params, timeout_ms}` → `data.task_id`; `task_status` every 2 s until `completed`, `failed` or `cancelled`; then `task_result`. A failed submit falls back to the direct call | none in Go; it is why a call can take 300 s, and a direct one 30 s | `mcp-bridge/lib.js` `executeUnrealToolAsync` | source |
| The bridge lists its tools afresh on each request: `/mcp/status` unreachable → only `unreal_status`; otherwise `unreal_<name>` per toolkit tool plus `unreal_status` and `unreal_get_ue_context` | the agent starts before the editor is up (k72, k126: started is not ready) | `index.js`, the `ListToolsRequestSchema` handler | source |
| How long a cold editor takes to open the port | `endpointWaitWindow` (5 min) | none; R§6 found no bound | unverified; U8 |

## Open — nobody may state these as settled

| Question | Card | Experiment |
|---|---|---|
| How the editor parses the project argument: relative paths and their base directory, `-project=`, spaces and non-ASCII, malformed or duplicate tokens. The launch row above rests on it | — | U1 |
| Does the toolkit's `execute_script` approval window appear under `-unattended`? | k136 | none in the research; a Windows run of its own |
| Is the crash reporter in the editor's Job Object, so that the job closing at the editor's exit (`runService`) kills it mid-report? | k154 | U4 (and W3, W4 in `docs/research/2026-11-21-windows-processes.md`) |
| Everything said about `-unattended`, recovery and the reporter was claimed for UE 5.7; UE 5.8 and newer were never re-checked (README says so). The toolkit declares `EngineVersion` 5.6.0 | — | U2, U4 on both versions |
| Encoding of the editor's stdout under `-stdout` and of `Saved/Logs/*.log`, the log's file name, and whether the last lines reach either before the process is gone | — | U3 |
| Editor exit codes: that a clean exit is 0 and a crash is not. `restart: on-failure` (`shouldRestart`) and the fast-fail count rest on exactly that | — | U5 |
| Registry spelling and precedence, direct-open conversion, `.uproject` encoding | — | U6 |
| `-WaitMutex`, the Live Coding refusal and its message, the `BuildCookRun` flags | — | U7 |
| What "ready" is: no log line, window or port is a documented signal | — | U8 |

Epic pages, under `https://dev.epicgames.com/documentation/unreal-engine/`:
[flags] `unreal-engine-command-line-arguments-reference?application_version=5.7` ·
[pixel] `unreal-engine-pixel-streaming-reference` ·
[crash] `crash-reporting-in-unreal-engine?application_version=5.7` ·
[reporter] `API/Runtime/Core/FGenericCrashContext/SetOutOfProcessCrashReporterPid?application_version=5.8` ·
[installed] `installed-build-reference-guide-for-unreal-engine?application_version=5.8` ·
[manage] `managing-game-code-in-unreal-engine` ·
[sanitizers] `using-clang-sanitizers-in-unreal-engine-projects`.
[tk] is `https://github.com/EpicGames/tk-unreal/blob/master/startup.py`.
Tags with a version suffix ([cmd57], [live57], …) are defined in the research document.
