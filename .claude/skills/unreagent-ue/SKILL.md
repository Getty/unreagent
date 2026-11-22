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

No editor runs on the dev box: a UE fact here comes from text, from the engine
source, or from one set of Windows runs. State a fact from this file with its
status, never stronger. Found a new one? Add it with its source, or as
unverified with the reason.

**Status.** `source` — read in source code: the toolkit (`../ue-llm-toolkit`
at 9f6a0f7, 43 tools, `VersionName` 1.1.0; its tag v1.0.0 is 4854c4e, 38
tools; `T/` = `Plugin/UELLMToolkit/`), this repo, or Epic's public `tk-unreal`
([tk], an Epic tool, not the engine). `source (UE 5.8.4)` — read in the engine
source at 5.8.4 on 2026-11-22 (`E/` = `Engine/Source/`, symbol and line given;
lines move between versions). Read, not run: say "the 5.8.4 source", never
"tested", and nothing about another engine version follows from it.
`measured (kNN)` — observed in the Windows runs of 2026-11-22 noted on that
card (Launcher builds of UE 5.7.4 and 5.8.4, one machine): an observation, not
a bound. `doc` — Epic's documentation says so (page re-read 2026-11-21); a
documented contract, not observed behavior. `research` —
`docs/research/2026-11-21-unreal-editor.md` (R§n) cites a page that was not
re-read; that document was written without the engine source. `unverified` —
no source holds; U1–U8 are that document's experiments.

Not repeated here: what the agent is told about builds, restarts and the
editor tools (`skills/unreagent-build`, `skills/unreagent-iterate`,
`skills/ue-python`, `skills/ue-*`); Job Objects and Win32 (skill
`unreagent-windows`); config semantics (skill `unreagent-config`).

## Editor launch and `-unattended`

**The obvious assumption:** `-unattended` means "no dialog, ever". It is a
mode that cooperating code asks about (`FApp::IsUnattended`); each row below
names one piece of code that does, and nothing lists them all.

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| The command line is `UnrealEditor.exe <.uproject> [flags]`, project first | `run()` in `cmd/launcher/main.go` prepends `unreal.project` | R§1 [cmd57] | research; how the argument is parsed: open, U1 |
| `unattended`: "Run in unattended mode … Use this to disable UI pop-ups and other dialogs" | `run()` appends `-unattended` (`UnrealConfig.Unattended`, default true) | [flags] | doc |
| Under `-unattended` the editor does not offer to restore packages: `HasPackagesToRestore` is false, and `OfferToRestorePackages` skips the prompt. `-AutoDeclinePackageRecovery` exists as a flag of its own and adds nothing next to `-unattended` | comment on `UnrealConfig.Unattended`; the log line in `run()` ("no recovery prompt") is accurate; no flag change needed (k202) | `E/Editor/UnrealEd/Private/PackageAutoSaver.cpp` 157 (`bAutoDeclineRecovery`), 673-690 | source (UE 5.8.4); measured (k202) on 5.7.4 and 5.8.4: no prompt, nothing restored |
| Under `-unattended` the restore file is not rewritten at start. `OfferToRestorePackages`, which writes it as "restore off", has one caller, `FUnrealEdMisc::OnInit`, and that calls it only when `HasPackagesToRestore()` is true, which it never is under `-unattended`. The run on k202: the file is left untouched until a clean exit or the session's first dirty package, so a kill before that hands the old offer to the next start without `-unattended` | the comment on `cleanRecovery` ("its restore dialog would block an unattended restart") holds only with `unattended: false`; deleting the file stays harmless | `E/Editor/UnrealEd/Private/UnrealEdMisc.cpp` 370-377 (the caller); `PackageAutoSaver.cpp` 673-677 (`HasPackagesToRestore`), 679-697; k202 | source (UE 5.8.4); measured (k202). Source and run agree. An earlier source reading on k202 ("rewritten at start") had skipped the caller |
| Disaster recovery (Concert) is skipped under `-unattended`: it logs "Skipped recovery process" and opens a new session | the same log line in `run()` | `Engine/Plugins/Developer/Concert/ConcertApp/DisasterRecoveryClient/Source/DisasterRecoveryClient/Private/DisasterRecoveryClientModule.cpp` 382 | source (UE 5.8.4); not run |
| With `-unattended` or `-stdout` (the default args carry both), project or plugin modules that are missing or built for another engine version do not raise the "rebuild now?" box: the editor logs `Incompatible or missing module: <file>` and startup fails, exit 1 | `shouldRestart`, the fast-fail count | `E/Runtime/Launch/Private/LaunchEngineLoop.cpp` 6688, 6704-6708; the startup-failure `return 1` near 2779 | source (UE 5.8.4). measured (k208), a `.uproject` naming a module with no binary, both halves: without `-unattended`, exit 1 after 1.3-1.5 s with `Incompatible or missing module: <name>` and no dialog, the path read here; with `-unattended`, exit 1 after 15.7 s (5.7.4) and 17.1 s (5.8.4) with an auto-answered message box (`Message dialog closed, result: Ok` and the module-not-found text in the log), 9-11 s per attempt through unreagent. Why the `-unattended` run does not end at the line read: unread |
| A plugin whose `.uplugin` sets an `EngineVersion` other than the engine's compatible version is asked about in a Yes/No box; under `-unattended` the answer is No and the plugin is not loaded (`Skipping load of '<name>'`). The comparison (`GetNewest`) takes major, minor and patch, and the changelists only when both sides carry one; the compatible version is built with patch 0 unless `Build.version` marks a licensee version, which keeps the engine's patch. So `5.8.0` passes on 5.8.4 and `5.8.4` does not. Upstream toolkit 9f6a0f7 declares 5.6.0, so on any other engine the bridge port never opens; the 5.8 port (fork `Getty/ue-llm-toolkit`, branch `ue-5.8`, 9fc7d4f) removes the field | `waitForEndpoint` waits out its window and cannot name the cause (k214, k205) | `E/Runtime/Projects/Private/PluginManager.cpp` 1860-1864, `IsPluginCompatible` and `PromptToLoadIncompatiblePlugin` 2688-2715; `E/Runtime/Core/Private/Misc/EngineVersion.cpp` 56-90 (`GetNewest`), 199-208 (`FGlobalEngineVersions`) | source (UE 5.8.4); measured (k214) on 5.7.4 and 5.8.4: `Message dialog closed, result: No, title: 'UELLMToolkit' is Incompatible`, no port 3000; with the field removed, 43 tools on 5.8.4; `5.7.0` loads on 5.7.4. The licensee case: source only |
| A Slate modal window is cancelled only under `GIsRunningUnattendedScript`, not under `-unattended`; the toolkit's `execute_script` approval window is such a modal, and a cancel would deny the script, not approve it | none; `-unattended` is no lever for k136 | `E/Runtime/Slate/Private/Framework/Application/SlateApplication.cpp` `AddModalWindow`, near 2134; the toolkit's `ScriptPermissionDialog.cpp` | source (UE 5.8.4); the window itself under `-unattended`: not run |
| `-stdout` adds the stdout log device; alone it carries Display and above. `-FullStdOutLogOutput` (singular) raises it to all verbosities, `-AllowStdOutLogVerbosity` to Log | default `unreal.args` in `applyDefaults` (`internal/config/config.go`): `-stdout -FullStdOutLogOutput` | `E/Runtime/Core/Public/Misc/OutputDeviceStdOut.h` 35 (`AllowedLogVerbosity`); `E/Runtime/Core/Private/Misc/OutputDeviceStdOut.cpp` 190-198; `LaunchEngineLoop.cpp` 1892 | source (UE 5.8.4). The plural `fullstdoutlogoutputs` of Epic's flag table [flags] is not what this source reads |
| The log file is `Saved/Logs/<ProjectName>.log` (`Unreal.log` without a project name), UTF-8 with a BOM, written by an async writer that flushes every 0.2 s (`log.flushInterval`, `-LOGFLUSHINTERVAL=`). `-LOG=<name>` or `-LogFileName=<name>` renames it inside the log directory, `-ABSLOG=<path>` gives a full path; a name whose extension is not `log` or `txt` is ignored. An existing file is copied to a timestamped backup first; if the file cannot be opened, `_2`, `_3`, … are tried | `logs` and the skills point agents at this file; `read_file` sees the BOM | `E/Runtime/Core/Private/GenericPlatform/GenericPlatformOutputDevices.cpp` 75-127 (`GetAbsoluteLogFilename`); `E/Runtime/Core/Private/Misc/OutputDeviceFile.cpp` 44-518 | source (UE 5.8.4). Encoding of the stdout stream, and whether the last lines before a kill or crash reach either: unverified, U3 |
| Flags are matched without regard to case. A flag needs `-` or `/` right before it, whitespace (or the start) before that, and whitespace or the end after it, so `-unattended=1` is not `-unattended` | `hasArg` compares exactly, so `-Unattended` in `unreal.args` gets a second `-unattended` (k207) | `E/Runtime/Core/Private/Misc/Parse.cpp` 325-351 (`FParse::Param`) | source (UE 5.8.4) |
| A start that will fail does so within seconds | `FastFailWindow` (10 s) and `fastFailLimit` in `internal/supervisor/supervisor.go`: a failure after 10 s never counts toward the give-up | nothing in the source sets a time | unverified as a bound. measured (k208): a missing module took 9-17 s to exit, 8 restarts in 95 s without a give-up; a `.uproject` path that does not exist, 0.48 s |

## Exit codes

**The obvious assumption:** 0 is a clean run and anything else a crash.

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| Windows: a clean quit is 0, a fatal error or crash 3 (`RequestExit` with `GIsCriticalError`), a startup failure 1, a command-line failure -1 | `restart: on-failure` (`shouldRestart`), the fast-fail count | `E/Runtime/Core/Private/Windows/WindowsPlatformMisc.cpp` 1455-1470; `LaunchEngineLoop.cpp` near 2779 | source (UE 5.8.4); measured (k208) on 5.7.4 and 5.8.4: quit 0, `debug crash` and `debug gpf` 3, missing module 1, `TerminateProcess` 1 |
| Crash-machinery codes: 777003 the reporter crashed, 777004 the crash handler crashed, 777006 a crash during static init; a console control event ends with 0xC000013A | anything that reads `lastExit` | 7770xx: `E/Runtime/Core/Public/GenericPlatform/GenericPlatformCrashContext.h` 36-60; 0xC000013A: `E/Runtime/Core/Private/Windows/WindowsPlatformMisc.cpp` 1161-1168 (the console control handler's `TerminateProcess`) | source (UE 5.8.4); none observed |
| Exit 0 does not prove a clean run: the editor's init returns 0 when `EngineLoop.Init()` fails | `shouldRestart` reads 0 as "do not restart" | `E/Editor/UnrealEd/Private/UnrealEdGlobals.cpp` 244-249 | source (UE 5.8.4); which failures take this path: unread |
| An editor whose Job Object is closed exits 0 too | harmless today: unreagent closes the job only on a stop it asked for | k208, k154 | measured (k208, k154) |

## Crash artefacts and the reporter

**The obvious assumption:** the reporter is a window that appears after a
crash, and a folder in `Saved/Crashes` is a finished report of a crash.

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| Editor crash reports are stored "in your project's `Saved/Crashes` folder" (a second, per-user location exists for shipped games) | `cleanRecovery`, `makeEditorExitHandler` (`cmd/launcher/lifecycle.go`) | [crash] | doc |
| The reporter is `CrashReportClient`, "`CrashReportClientEditor` for the Editor" | `crashReporterKillers` kills both image names | [reporter] | doc; measured (k154): the running process is `CrashReportClientEditor.exe … -MONITOR=<editor pid>` |
| The editor starts the reporter as a monitor in its static init, and it runs for the editor's whole life, not only after a crash | `editorPreStart` treats it as a leftover window. Inference, not observed (k154): `taskkill /IM` on a start that does not follow a crash also hits the monitor of any other editor on the machine | `E/Runtime/Core/Private/Windows/WindowsPlatformCrashContext.cpp` 54-55 (`USE_CRASH_REPORTER_MONITOR`), 1390-1393 | source (UE 5.8.4); measured (k154): started about 1 s after the editor, and the instance that stays is a respawn whose parent is not the editor |
| The monitor is created detached and without `CREATE_BREAKAWAY_FROM_JOB`, so it sits in the editor's Job Object | `runService` closes the job when the editor exits | `E/Runtime/Core/Private/Windows/WindowsPlatformProcess.cpp` 567-593 (`CreateProcInternal`, `bBreakawayFromJob`) | source (UE 5.8.4); measured (k154): the reporter is gone within 2 s of a job close |
| On a crash the reporter acknowledges the editor once minidump and stacks are written, then still writes `CrashContext.runtime-xml` and does the unattended send; the editor exits right after the acknowledgement | a job close at the editor's exit can cut the report short (k154) | `E/Programs/CrashReportClient/Private/CrashReportClientApp.cpp` 691-720, 1329-1340 | source (UE 5.8.4) for the order. Whether the job close does cut it: **open**, U4 — one run on k154 (5.7.4) found the report without call stack and module list under unreagent and complete 5 s after a direct start; not repeated, not run on 5.8.4 |
| Unattended reporter: `bSendUnattendedBugReports` (default true) makes it "skip displaying to the user" | the reason the kill is only a backup | [crash] | doc; measured (k154): the monitor carries `-unattended` only when the editor has it, and without it the reporter's window waits for a human |
| An ensure sends a report without ending the editor, so a new report does not prove a crash | anything that reads `Saved/Crashes` as "it crashed" | [crash] | doc |
| When a report is complete (which file is written last, any sentinel) | `followsCrash` spares the reporter instead of waiting for it, because nothing says when it is done | none | unverified; U4 |
| `Saved/Autosaves/PackageRestoreData.json` holds the restore offer (`RestoreEnabled`, `Packages`) | `cleanRecovery` deletes it | `PackageAutoSaver.cpp` 107 (`RestoreFilename`); k202 | source (UE 5.8.4) for the name; measured (k202) for the content. That deleting it prevents the prompt: not run on its own |
| Epic has its own flag `CleanCrashReports` ("Clean crash report folder located in `../Saved/Crashes`") | not used; `cleanRecovery` does it by hand | [flags] | doc |

History: k77, k128 and k153 each destroyed crash evidence by treating one more
kind of start as "clean". Which starts spare it is launcher logic: `followsCrash`.

## Engine discovery

**The obvious assumption:** `EngineAssociation` is a version, and one registry
key finds every build.

`D/` = `E/Developer/DesktopPlatform/Private/`. These rows describe the
engine's own lookup (version selector, UnrealBuildTool, project generation).

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| `EngineAssociation` is read as a string; a non-string value counts as blank. A value containing `/` or `\` is an engine root relative to the project's directory. A blank one is resolved by walking up the parent directories; a parent is an engine root when it has `Engine/Binaries` and `Engine/Build`. There is no fallback to the highest installed engine | `classifyAssociation`, `chooseEngine` (`internal/config/engine.go`). **A blank one falls back to the highest installed engine and the hierarchy is never searched; a path value is treated as a build id** (k203) | `D/DesktopPlatformBase.cpp` 401-421 (`IsValidRootDirectory`), 454-498 (`GetEngineIdentifierForProject`) | source (UE 5.8.4) |
| A build identifier need not be a GUID (Epic's example: `MyCustom419` → `D:\CustomUE`) | `classifyAssociation` treats every non-version as a build; `normalizeBuildID` only strips braces and case | [installed] | doc |
| Source builds: the engine reads only `HKCU\SOFTWARE\Epic Games\Unreal Engine\Builds`, with the spaces, as [manage] spells it; value name = identifier, data = root directory, and values with an invalid root are deleted while enumerating. The spelling without the space ([installed]) and a `Builds` key under HKLM ([tk]) are not read | `buildsRegKey` is correct as is (k204) | `D/Windows/DesktopPlatformWindows.cpp` 28 (`InstallationsSubKey`), 335-390 | source (UE 5.8.4); measured (k204): the shipped binaries of 5.7.4 and 5.8.4 hold only that spelling. No machine with a registered source build was checked |
| Launcher installs: the engine reads `%ProgramData%\Epic\UnrealEngineLauncher\LauncherInstalled.dat`, JSON `InstallationList[]` with `AppName` (`UE_5.8` → identifier `5.8`) and `InstallLocation`. It does not read `HKLM\SOFTWARE\EpicGames\Unreal Engine\<ver>\InstalledDirectory` for this | `launcherRegKey`, `launcherInstalls` read only that HKLM key, which Epic's `tk-unreal` also reads [tk]; the `.dat` is not read (k211) | `D/DesktopPlatformBase.cpp` 72-86, 1575-1612; `E/Runtime/Core/Private/Windows/WindowsPlatformProcess.cpp` 1516-1530 (`ApplicationSettingsDir`) | source (UE 5.8.4); measured (k211): a Launcher-installed 5.8.4 with a `.dat` entry and no HKLM subkey, so unreagent picked 5.7 for a 5.8 project |
| Default root `C:/Program Files/Epic Games/UE_<version>`, editor at `Engine/Binaries/Win64/UnrealEditor.exe` | `defaultEnginePatterns`, `hasEditor` | [tk] `EXECUTABLE_TEMPLATES` | source for the `C:` pattern; the `D:`, `E:` and `<drive>:/Epic Games` patterns are convention without a source |
| The Launcher also records installs in `EpicGamesLauncher\Overrides\UE_<version>` keys | not read | R§4 [launcher] | research |
| `UnrealEditor.exe <project>` itself never looks at the association; only the lookup above does | the warning in `chooseEngine` (k88), "the editor may offer to convert the project", has no source (k203) | the scout note on k203; it names no engine file | source (UE 5.8.4), a negative: no such call found. What the editor does with a project saved by another version: not run, U6 |
| A text file such as the `.uproject` is read as UTF-16 LE or BE when it starts with that BOM, else as UTF-8 with or without a BOM | `parseEngineAssociation` accepts UTF-8 only; anything else reads as "no association", silently (k203) | `E/Runtime/Core/Private/Misc/FileHelper.cpp` 145-185 | source (UE 5.8.4) |
| `reg.exe` output for a non-ASCII path | `regQuery`, `parseRegQuery` | k84 note | unverified |

History: k84 (fixed version list, lexical order), k88 (silent fallback).

## Build command lines and Live Coding

**The obvious assumption:** the two defaults are documented command lines.
Only their skeleton is.

`U/` = `E/Programs/UnrealBuildTool/`.

| Fact | Code that depends on it | Source | Status |
|---|---|---|---|
| `Build.bat <Target> <Platform> <Configuration> … -WaitMutex -FromMsBuild` is a form Epic prints | the `compile` default in `applyDefaults` | [sanitizers] (one example, for a Linux game target) | doc for the form |
| The editor target is `<ProjectName>Editor` | same: `${PROJECT_NAME}Editor` | none; nothing read here ties a target's name to the project's, or says every project has an editor target | unverified |
| UnrealBuildTool takes one named global mutex per installation (the name is hashed from the path of its own assembly). With `-WaitMutex` it waits for it without a time limit; without it, it fails at once with exit 10 (`ConflictingInstance`, "A conflicting instance … is already running") | none in Go; `skills/unreagent-build` tells the agent that a second `compile` waits | `U/UnrealBuildTool.cs` 441-448; `E/Programs/Shared/EpicGames.Build/System/GlobalSingleInstanceMutex.cs`; `E/Programs/Shared/EpicGames.Core/SingleInstanceMutex.cs` (`Acquire`); `E/Programs/Shared/EpicGames.Core/UnrealEngineTypes.cs` (`CompilationResult`) | source (UE 5.8.4); not run. Only modes flagged single-instance take it; which modes those are: unread |
| With a Live Coding session active for the target, a build fails: `Unable to build while Live Coding is active`, exit 6 (`OtherCompilationError`). The session is found by a named mutex built from the target's executable path. The check is skipped for a foreign plugin, for the Live Coding modes themselves, and when `bAllowHotReloadFromIDE` is off | none in Go; the `run_command` description in `cmd/launcher/tools.go` and `skills/unreagent-build`, `-iterate` and `-files` give the stop-before-compile order | `U/System/HotReload.cs` 270-278 (`CheckForLiveCodingSessionActive`), `IsLiveCodingSessionActive`; called from `U/Modes/BuildMode.cs` | source (UE 5.8.4); not run |
| Live Coding does not auto-start under `-unattended`. `-LiveCoding` on the command line starts it anyway, `-LiveCoding=false` keeps it off | with the default args the refusal above does not fire by itself | `E/Developer/Windows/LiveCoding/Private/LiveCodingModule.cpp` 487-507 (`StartupModule`) | source (UE 5.8.4); not run |
| What UnrealBuildTool does against a module DLL the running editor has loaded, with no Live Coding session | the stop-before-compile order rests on "the editor has the modules loaded", which needs no answer here | none | **open** — needs a run, U7 |
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
| `/mcp/status` answers `status`, `port`, `version`, `toolCount`, `tools`, `projectName`, `projectDirectory`, `engineVersion`. `version` is the literal `"1.0.0"` in `HandleStatus`; it is not the plugin's `VersionName`, which is 1.1.0 at 9f6a0f7. The tools are registered in the server's constructor, before it listens | `waitForEndpoint` (`cmd/launcher/bridge.go`) makes a bare connect and logs "is reachable"; only this route tells the toolkit from another listener | `HandleStatus`; `FUnrealClaudeMCPServer()`; `MCPToolRegistry.cpp` `FMCPToolRegistry()`; `T/UELLMToolkit.uplugin` | source |
| A tool runs on the game thread; the HTTP handler waits 30 s (`GameThreadTimeoutMs`), then answers an error while the tool may still run. A failed tool is HTTP 400 with `{success, message, data}` | none in Go | `HandleExecuteTool`; `UnrealClaudeConstants.h` | source |
| Which interface the server binds | the "localhost only?" hint of `editorEndpoint` | not in the toolkit; it is UE's `FHttpServerModule` | unverified |
| The bridge needs `npm install`: one runtime dependency, `@modelcontextprotocol/sdk`, plus the devDependencies `vitest` and `@vitest/coverage-v8`; no `node_modules` in the repository, Node ≥ 18 | `prepareNodeBridge` runs a plain `npm install`, which fetches the devDependencies too — that is what `bridgeInstallTimeout` has to cover; `bridgeInstallMarker` | `T/Resources/mcp-bridge/package.json` | source |
| The bridge answers `initialize` (`serverInfo.name` `ue5-mcp-server`) without contacting the editor | `smokeTestMCP`, `initResponse`: a pass proves node and the dependencies, nothing about the editor | `mcp-bridge/index.js` `main` | source |
| Environment: `UNREAL_MCP_URL` (`http://localhost:3000`), `MCP_REQUEST_TIMEOUT_MS` (30000), `MCP_ASYNC_ENABLED` (on unless `"false"`), `MCP_ASYNC_TIMEOUT_MS` (300000), `MCP_POLL_INTERVAL_MS` (2000), `INJECT_CONTEXT` (off unless `"true"`). A number that does not parse, or 0, means the default | `prepareMCPBridges` reads only `UNREAL_MCP_URL` from the entry's `env`; unset, no endpoint is watched | `index.js` `CONFIG` | source |
| Every tool call goes through the queue, except `task_*` and the two the bridge answers itself before that branch (`unreal_status`, `unreal_get_ue_context`): `POST /mcp/tool/task_submit` `{tool_name, params, timeout_ms}` → `data.task_id`; `task_status` every 2 s until `completed`, `failed` or `cancelled`; then `task_result`. A failed submit falls back to the direct call | none in Go; it is why a call can take 300 s, and a direct one 30 s | `mcp-bridge/lib.js` `executeUnrealToolAsync` | source |
| The bridge lists its tools afresh on each request: `/mcp/status` unreachable → only `unreal_status`; otherwise `unreal_<name>` per toolkit tool plus `unreal_status` and `unreal_get_ue_context` | the agent starts before the editor is up (k72, k126: started is not ready) | `index.js`, the `ListToolsRequestSchema` handler | source |
| `PostEngineInit` modules, the toolkit among them, load inside the engine loop's init; the log line `(Engine Initialization) Total time` is printed after it | a candidate readiness signal (k205): an upper bound for "module loaded", not proof the port is bound | `E/Runtime/Launch/Private/Launch.cpp` near 175 | source (UE 5.8.4) |
| How long a cold editor takes to open the port | `endpointWaitWindow` (5 min) | nothing in the source sets a time | unverified as a bound. measured (k205, 5.7.4, toolkit 1.1.0): TCP accepted after 10.0-12.4 s warm and 48.5 s on a fresh project's first start; HTTP 200 came 2-7 s (first start: 12 s) after the connect, with the first game-thread tick. Cold shader cache: not run, U8 |

## Open — nobody may state these as settled

| Question | Card | Experiment |
|---|---|---|
| How the editor parses the project argument: relative paths and their base directory, `-project=`, spaces and non-ASCII, malformed or duplicate tokens. The launch row above rests on it | — | U1 |
| Does the toolkit's `execute_script` approval window appear under `-unattended`? The source says `-unattended` does not cancel it; the window was not run | k136 | a Windows run of its own |
| Does the job closing at the editor's exit (`runService`) cut the crash report short? The source allows it and one run saw it | k154 | U4 (and W3, W4 in `docs/research/2026-11-21-windows-processes.md`) |
| Engine facts are read from 5.8.4 only; the runs cover 5.7.4 and 5.8.4. Nothing here is checked for another version | — | — |
| Encoding of the editor's stdout under `-stdout`, and whether the last lines reach stdout or the log file before the process is gone | — | U3 |
| The timing windows: `FastFailWindow` (10 s) and `endpointWaitWindow` (5 min). The source holds no time; the measurements are from one machine | k208, k205 | U5, U8 |
| A source-build registration on a real machine; what the editor does with a project saved by another engine version | k204, k203 | U6 |
| UnrealBuildTool against a DLL the editor has loaded, with no Live Coding session; the `BuildCookRun` flags | — | U7 |
| What "ready" is: no log line, window or port is a documented signal | k205 | U8 |

Epic pages, under `https://dev.epicgames.com/documentation/unreal-engine/`:
[flags] `unreal-engine-command-line-arguments-reference?application_version=5.7` ·
[crash] `crash-reporting-in-unreal-engine?application_version=5.7` ·
[reporter] `API/Runtime/Core/FGenericCrashContext/SetOutOfProcessCrashReporterPid?application_version=5.8` ·
[installed] `installed-build-reference-guide-for-unreal-engine?application_version=5.8` ·
[manage] `managing-game-code-in-unreal-engine` ·
[sanitizers] `using-clang-sanitizers-in-unreal-engine-projects`.
[tk] is `https://github.com/EpicGames/tk-unreal/blob/master/startup.py`.
Tags with a version suffix ([cmd57], [live57], …) are defined in the research document.
