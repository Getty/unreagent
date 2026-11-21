# Document 1 — Unreal Editor as seen from a parent process (UE 5.7 and 5.8)

Research date: 2026-11-21. Scope: Windows editor processes, not packaged games or UBT's own exit codes.
Evidence status: **D57/D58** means the corresponding Epic documentation was retrieved; **E58/E59** means a current Epic page labelled 5.8/5.9, without a pinned engine revision. **Unverified** means no implementation claim is established.
**Engine-source access failed:** authenticated reads of [EpicGames/UnrealEngine](https://github.com/EpicGames/UnrealEngine), including `Engine/Build/Build.version`, returned 404; branch enumeration also failed. No 5.7/5.8 implementation diff was possible. API pages establish documented contracts and source locations, not the unseen implementation.

## 1. Editor start

**Wrong assumption:** `-unattended` is a universal “never show UI, automatically say yes” switch. Epic describes cooperating unattended-aware APIs; the documented modal-dialog overload returns a supplied default answer. [Unattended][app58] [Dialogs][dialogs58]

| Fact / boundary | Source | Version evidence |
|---|---|---|
| Documented form: `UnrealEditor.exe MyGame.uproject [map-or-server-URL] [flags/key=value arguments]`. A map can include `?game=...`; `-game` switches away from full-editor mode. | [Command line 5.7][cmd57]; [5.8][cmd58] | D57 + D58; matching documented syntax. |
| General project-name lookup, relative-path base directory, `-project=` alternatives, and malformed/duplicate project tokens: **unverified**; inspect the checked build's command-line parser and test U1. | `Engine/Source/Runtime/Launch/Public/LaunchEngineLoop.h::FEngineLoop::PreInit` is documented by [FEngineLoop][loop58]; body not inspected. | Both implementations unchecked. |
| `-unattended` requests suppression of popups/dialogs. `OpenMsgDlgInt(..., InDefaultValue, ...)` logs and returns its default in unattended mode; this is not evidence that every native/plugin dialog uses that route. | [Flags][flags57]; [OpenMsgDlgInt][dialogs58] | D57 flag; D58 dialog contract. |
| Crash-reporter UI has its own unattended behavior; `bSendUnattendedBugReports` controls unattended sending. An unattended reporter can collect/send without displaying its report window. | [Crash reporting 5.7][crash57]; [5.8][crash58] | D57 + D58. Exact propagation from editor flag/config to every reporter launch is **unverified**. |
| Autosave recovery has a distinct `AutoDeclinePackageRecovery` flag; `IPackageAutoSaver::DisableRestorePromptAndDeclinePackageRecovery` explicitly bypasses the prompt and declines recovery. | [Flags][flags57]; [IPackageAutoSaver][autosave58] | D57 flag; D58 API. `-unattended` alone doing the same is **unverified**; U2. |
| “Rebuild modules?” and version-conversion prompts: exact unattended choices, emitted messages, and exit values are **unverified**; inspect those call sites in licensed source and perform U2/U6. Arbitrary plugin, native OS, credential, or driver dialogs have no verified suppression coverage here. | [FApp::IsUnattended][app58] documents caller-side detection; it does not specify these call sites. | No verified 5.7/5.8 prompt matrix. |
| `-stdout` sends logging to standard output. Epic documents `-StdOut -FullStdOutLogOutput` together for maximum stdout logging. It does not establish that compile-time/runtime category filters disappear. | [Flags][flags57]; [Pixel Streaming reference][pixel]; [Logging][logging57] | D57 stdout; current Epic combination. Exact 5.8 output filtering unchecked. |
| Documentation spelling discrepancy: the flag table lists `fullstdoutlogoutputs`, while Pixel Streaming documents singular `-FullStdOutLogOutput`. Whether both spellings work is **unverified**; source search and U3 settle it. | [Flags][flags57]; [Pixel Streaming reference][pixel] | Do not infer alias support from inconsistent documentation. |
| Project log directory: `<Project>/Saved/Logs`. The exact default basename/extension, rotation suffixes, and fallback before project resolution are **unverified** here; Epic's logging page calls it a `.txt` file rather than specifying a reliable basename. | [Logging][logging57]; [FPaths::ProjectLogDir][logdir58] | D57 directory; E58 API. |
| File encoding/BOM and Windows sharing flags are **unverified**. The existence of `FILEWRITE_AllowRead` does not prove the log writer uses it. Settle with the log-writer implementation and simultaneous-open/byte test U3. | [FOutputDeviceFile][output58]; [EFileWrite][filewrite58] | E58 API only; no verified “UTF-8” or “UTF-16” claim. |
| The output device has an asynchronous writer and an explicit `Flush()` operation; process exit or a line in stdout does not establish that every log byte reached the file. | [FOutputDeviceFile][output58], `AsyncWriter` and `Flush`; last clause is an inference from separate buffered outputs. | E58 documented API. |

## 2. Crash handling

**Wrong assumption:** the first crash directory means a complete report, and the reporter is necessarily created after the crash. Epic's API also describes an already-running out-of-process crash monitor. [Reporter mode][reporter58]

| Fact / boundary | Source | Version evidence |
|---|---|---|
| Editor reports are documented under project `Saved/Crashes`; Epic also describes a per-user `AppData/Local/<Project>/Saved/Crashes` location. Reports include context, stacks, logs and optional user comments. | [Crash reporting 5.7][crash57]; [5.8][crash58] | D57 + D58; same documented locations. |
| The API names `CrashReportClientEditor` for editor reporting and `CrashReportClient` generally. It supports both reporter-generated crash artifacts and artifacts generated inside the crashing process. | [FGenericCrashContext::SetOutOfProcessCrashReporterPid][reporter58] | D58. 5.7 implementation not inspected. |
| With the out-of-process reporter configured, it can wait for a crash, capture stacks, write a minidump, collect artifacts and send them. Therefore “spawn one reporter only after process exit” is not a verified lifecycle model. | [Reporter mode][reporter58] | D58 documented capability; default mode and process timing are **unverified**. |
| Exact executable suffix/path, command line, monitor-versus-UI process split, additional executables, and WER involvement: **unverified**; inspect 5.7/5.8 crash-launch code and capture process creation in U4. | [Reporter mode][reporter58] establishes the modes, not an exhaustive Windows process tree. | Both configurations require verification. |
| `CrashContext.runtime-xml` is a documented report filename. Names/order/last-write ordering of the minidump, copied log, configuration, metadata, and auxiliary files are **unverified**; U4 records create/write/close events. | [Crash reporting][crash58] names the XML file; no file-by-file transaction protocol is stated there. | D58 XML example; neither implementation inspected. |
| “Complete report” sentinel/rename/IPC contract: **unverified**. Directory existence, first XML visibility, first dump visibility, editor exit and reporter exit have not been established as interchangeable completion events. | Source needed: actual crash writer and reporter completion paths; U4 validates resulting files. | No verified completeness contract for either version. |
| Autosave API loads a restore file, records recovery state, and offers recovery of dirty packages with autosaves. The exact conditions for `Saved/Autosaves/PackageRestoreData.json` to trigger recovery, its schema and deletion/update rules are **unverified**. | [IPackageAutoSaver][autosave58], `LoadRestoreFile`, `UpdateRestoreFile`, `OfferToRestorePackages`; locate the requested filename in licensed source, then U2. | D58 API only; path supplied in the research question, not independently source-verified. |
| Ensures can generate unattended reports without terminating the editor; a new crash-report artifact alone does not prove the editor crashed. | [Crash reporting][crash57] | D57 + D58 documentation agrees. |
| Reporter survival after killing only the editor versus closing the launcher's job: **unverified for UE's actual creation flags/job membership**. “Detached” UI/console behavior is not evidence of job breakaway. | [Reporter mode][reporter58] establishes a separate process; actual Windows flags and parent-job interaction require U4 and Document 2. | Both builds and host job configurations. |

## 3. Exit codes

**Wrong assumption:** one universal nonzero value identifies an editor crash, or a build-tool error number is an editor exit code. UE exposes explicit exit-status requests; the startup/crash return paths remain uninspected. [Exit API][exit58]

| Termination path | Verified result / unresolved value | Source / settlement |
|---|---|---|
| Clean File > Exit / accepted window close | Exact editor DWORD **unverified**, including the often-assumed `0`. | [Exit API][exit58] is not a return-value contract for editor main; U5 measures both versions. |
| Crash, assertion, fatal log, unhandled OS exception | Exact DWORD and whether UE handles/replaces an exception status: **unverified**. No universal `1`, `3` or access-violation status is asserted. | [Crash reporting][crash58] classifies causes, not exit values; U4/U5 plus the actual Windows entry/exception handlers. |
| Missing, incompatible, failed-to-compile or failed-to-load module | **Unverified**; these are separate paths and need not share a status. | [FEngineLoop][loop58] documents initialization entry points, not their call-site return mapping; U5. |
| Forced editor API exit | `RequestExitWithStatus(Force, ReturnCode, ...)` documents a specified 0–255 status; forced exit can omit flushing configuration. Actual Windows override/caller mapping remains **unverified**. | [FGenericPlatformMisc::RequestExitWithStatus][exit58], generic-platform contract. |
| OS forced close, job termination, `taskkill /F` | No UE-defined universal code established. Exact result depends on the termination mechanism; see Document 2 and measure U5. | UE result **unverified**; OS behavior is documented separately in Document 2. |
| Cancelled startup module/conversion/project-selection dialog | Exact status and whether cancellation exits this process are **unverified**. | Inspect actual dialog callers; U2/U5/U6. |
| `ensure(...)` without a subsequent fatal failure | It does not itself require editor termination. | [Crash reporting][crash57]; therefore no exit code exists merely because an ensure report appeared. |

## 4. Engine discovery on Windows

**Wrong assumption:** `EngineAssociation` is always a dotted version and one registry key finds every build. Epic documents version, local identifier, custom identifier and blank association cases. [Installed builds][installed58]

| Fact / boundary | Source | Version evidence |
|---|---|---|
| Launcher-associated foreign project: version string; source/custom installed build: registered identifier, usually GUID; non-foreign project under the engine's project tree: empty association is valid. | [Installed builds][installed58] | D58. 5.7 implementation not inspected. |
| Custom identifiers need not be GUIDs: Epic's installed-build example uses `MyCustom419` mapped to an engine root. | [Installed builds][installed58] | D58 documentation. |
| Installed-build registration is documented as `HKCU\SOFTWARE\Epic Games\UnrealEngine\Builds`, identifier-to-root mapping. | [Installed builds][installed58] | D58. |
| **Documentation conflict:** Managing Game Code spells the key `HKCU\SOFTWARE\Epic Games\Unreal Engine\Builds`, with a space. The actual registry lookup spelling is **unverified here**, not evidence that both spellings are read. | [Managing Game Code][manage]; [Installed builds][installed58]; licensed source or U6 settles. | Current Epic pages disagree. |
| Epic's public integration reads subkeys of `HKLM\SOFTWARE\EpicGames\Unreal Engine` and their `InstalledDirectory` values. This establishes a real Epic lookup, not that every 5.7/5.8 install populates it or the engine's precedence among registry/manifests. | [Epic integration source][tk], `startup.py::EngineLauncher._find_software_from_registry`, fetched blob `483eb797c5ca1559442c3a689028e89fbc8115d2`; [IDesktopPlatform][desktop58]. | Ancillary Epic source; exact 5.7/5.8 registry population/precedence **unverified**, U6. |
| Epic documents Launcher manifests in ProgramData and separate optional overrides under `HKLM`/`HKCU\SOFTWARE\[WOW6432Node\]Epic Games\EpicGamesLauncher\Overrides\UE_<version>`, value `InstallLocation`. Overrides are not the source-build registration key. | [Multiple Launcher installs][launcher] | E58; the optional `WOW6432Node` notation abbreviates the four listed paths. |
| Documented conventional Launcher root: `C:\Program Files\Epic Games\UE_<version>`. Source build root is the chosen checkout location; a conventional directory is not a discovery guarantee. | [Project templates][templates]; [Installed builds][installed58] | E58/D58; exact fresh-machine defaults U6. |
| Missing association can prompt engine selection and rewrite the project. Project Browser conversion offers Open a Copy, Convert in-place, and Skip conversion. Direct `UnrealEditor.exe <project>` behavior is **unverified** and must not be inferred from that browser workflow. | [Installed builds][installed58]; [Updating projects][upgrade] | D58 association; E59 workflow. Explicit 5.7/5.8 direct-open comparison requires U6. |
| `.uproject` BOM is **unverified**, including whether generated/saved files contain UTF-8 BOM, no BOM, or a different encoding. `FProjectDescriptor::Save/Load` exists, but the API does not specify its encoding. | [FProjectDescriptor][descriptor58]; inspect Save/Load and test ASCII/non-ASCII projects in U6. | E58 API; neither writer implementation inspected. |

## 5. Building

**Wrong assumption:** BuildCookRun is the editor-target equivalent of Build.bat, or `-WaitMutex` waits until the editor closes. Epic documents BuildCookRun as a packaging pipeline; the UBT mutex contract must be verified separately. [Build operations][bcr57]

| Operation / condition | Command or fact | Source / status |
|---|---|---|
| Editor target build; substitute real target/project paths | `"<UE>\Engine\Build\BatchFiles\Build.bat" -Target="MyProjectEditor Win64 Development" -Project="C:\Work\MyProject\MyProject.uproject" -WaitMutex -FromMsBuild` | Composed from [5.7][module57]/[5.8][module58] target syntax and [sanitizer build flags][sanitizers]. Exact command not run; target must exist. |
| Equivalent positional form | `"<UE>\Engine\Build\BatchFiles\Build.bat" MyProjectEditor Win64 Development -Project="C:\Work\MyProject\MyProject.uproject" -WaitMutex -FromMsBuild` | [Sanitizers][sanitizers] documents target/platform/configuration position; [module docs][module57] documents Editor targets. Not run. |
| Documented RunUAT BuildCookRun invocation | `"<UE>\Engine\Build\BatchFiles\RunUAT.bat" BuildCookRun -project="C:\Work\MyProject\MyProject.uproject" -clientconfig=Development` | [Build operations][bcr57] gives this form for the build/cook/package workflow; it is **not verified as an editor-only target build**. |
| Exact editor-only BuildCookRun command | **Unverified.** No retrieved primary documentation establishes that `BuildCookRun -target=MyProjectEditor` has the requested semantics. Whether `-editor`/`-editorconfig`/`-skipbuildeditor`/`-nocompileeditor` selects, includes, skips, or only configures editor work must come from the tagged UAT parser/build-stage source and U7. | [Build operations][bcr57] establishes pipeline scope, not these options. No fabricated equivalent is supplied. |
| `-WaitMutex` | It appears in Epic's build examples; exact mutex name/scope, wait behavior, and relation to other UBT instances are **unverified**. Its spelling alone does not establish editor-lock waiting. | [Sanitizers][sanitizers]; locate `WaitMutex` in tagged UBT source and run two builds in U7. |
| `-FromMsBuild` | Formats messages for MSBuild; Epic states UHT supports the same logging options as UBT. | [Unreal Header Tool][uht]; E59 documentation, version-specific implementation still unchecked. |
| Editor open, Live Coding disabled | Epic documents compilation with the editor running and fallback from Live Coding to Hot Reload. Exact external UBT decision, DLL replacement/locking failures, and output text depend on target/build mode and are **unverified**. | [Module compilation][module57]; [Live Coding][live57]; U7. |
| Live Coding active | Live Coding builds/patches during editor/PIE execution; `Ctrl+Alt+F11` starts its build. Exact external UBT refusal predicate, exception type/status and **message text are unverified**; no familiar quotation is promoted to a checked 5.7/5.8 message. | [Live Coding][live57]; inspect tagged UBT Live Coding guard and capture full raw output in U7. |

## 6. Readiness

**Wrong assumption:** a visible window, open TCP port, or fixed delay proves all editor and project initialization has finished. Epic exposes distinct initialization phases and a configurable MCP service. [Delegates][delegates58] [MCP][mcp58]

| Signal / timing | Verified meaning / boundary | Source / version |
|---|---|---|
| Engine-init boundary | `OnFEngineLoopInitComplete` is at the end of engine initialization, just before ticking, and is not called for commandlets. It is an in-process delegate, not an automatically observable parent-process event. | [FCoreDelegates][delegates58], D58; last clause follows from its delegate type. |
| Earlier/later work | `OnPostEngineInit` precedes PostEngineInit module loading; `OnAllModuleLoadingPhasesComplete` marks module phases complete. These names do not establish that all project background work is finished. | [FCoreDelegates][delegates58], D58. |
| Log lines | Exact 5.7/5.8 ready-line text and its position relative to map load, asset scanning, shaders, plugin initialization and first tick are **unverified**. No uninspected log string is offered as a protocol. | Inspect log call sites around initialization; U8 correlates timestamps with actual task execution. |
| TCP port | UE 5.8's experimental Unreal MCP plugin can start automatically when configured; documented endpoint `http://127.0.0.1:8000/mcp` is configurable. Tool calls execute serially on the game thread. A successful connection alone establishes neither tool registration nor completion of unrelated loading. | [Unreal MCP 5.8][mcp58], D58; readiness boundary is an inference. 5.7 built-in equivalent not checked. |
| Window/splash existence | Relationship to project-ready state **unverified**. A window being created is not a documented UE initialization completion contract in the retrieved sources. | [FEngineLoop][loop58] distinguishes pre/post startup-screen phases; U8. |
| Cold shader cache startup duration | No portable seconds/minutes value is established. DDC misses generate derived data; Launcher builds may include a DDC Pak containing engine data. “Cold” must state which local/shared/pack caches were actually unavailable. | [DDC][ddc], E58. Actual duration requires U8; no guessed timeout. |
| Failures exiting “within seconds” | Exact timing and status for absent/bad project, missing/incompatible module/plugin, unsupported RHI/driver, missing prerequisites, unavailable cache and cancelled dialogs are **unverified**; none has a universal measured deadline here. | Source inspection plus U5/U8 on actual machine/build/project. |

[cmd57]: https://dev.epicgames.com/documentation/unreal-engine/command-line-arguments-in-unreal-engine?application_version=5.7
[cmd58]: https://dev.epicgames.com/documentation/unreal-engine/command-line-arguments-in-unreal-engine?application_version=5.8
[flags57]: https://dev.epicgames.com/documentation/en-us/unreal-engine/unreal-engine-command-line-arguments-reference?application_version=5.7
[app58]: https://dev.epicgames.com/documentation/unreal-engine/API/Runtime/Core/FApp/IsUnattended?application_version=5.8
[dialogs58]: https://dev.epicgames.com/documentation/unreal-engine/API/Editor/UnrealEd/OpenMsgDlgInt?application_version=5.8
[pixel]: https://dev.epicgames.com/documentation/unreal-engine/unreal-engine-pixel-streaming-reference
[logging57]: https://dev.epicgames.com/documentation/unreal-engine/logging-in-unreal-engine?application_version=5.7
[logdir58]: https://dev.epicgames.com/documentation/en-us/unreal-engine/API/Runtime/Core/FPaths/ProjectLogDir
[output58]: https://dev.epicgames.com/documentation/unreal-engine/API/Runtime/Core/FOutputDeviceFile
[filewrite58]: https://dev.epicgames.com/documentation/en-us/unreal-engine/API/Runtime/Core/EFileWrite
[autosave58]: https://dev.epicgames.com/documentation/unreal-engine/API/Editor/UnrealEd/IPackageAutoSaver?application_version=5.8
[crash57]: https://dev.epicgames.com/documentation/unreal-engine/crash-reporting-in-unreal-engine?application_version=5.7
[crash58]: https://dev.epicgames.com/documentation/unreal-engine/crash-reporting-in-unreal-engine?application_version=5.8
[reporter58]: https://dev.epicgames.com/documentation/unreal-engine/API/Runtime/Core/FGenericCrashContext/SetOutOfProcessCrashReporterPid?application_version=5.8
[exit58]: https://dev.epicgames.com/documentation/unreal-engine/API/Runtime/Core/FGenericPlatformMisc/RequestExitWithStatus
[installed58]: https://dev.epicgames.com/documentation/unreal-engine/installed-build-reference-guide-for-unreal-engine?application_version=5.8
[manage]: https://dev.epicgames.com/documentation/unreal-engine/managing-game-code-in-unreal-engine
[launcher]: https://dev.epicgames.com/documentation/en-us/unreal-engine/multiple-launcher-unreal-engine-installs
[templates]: https://dev.epicgames.com/documentation/unreal-engine/converting-a-project-to-an-unreal-engine-template
[desktop58]: https://dev.epicgames.com/documentation/unreal-engine/API/Developer/DesktopPlatform/IDesktopPlatform?application_version=5.8
[tk]: https://github.com/EpicGames/tk-unreal/blob/master/startup.py
[upgrade]: https://dev.epicgames.com/documentation/en-us/unreal-engine/updating-projects-to-newer-versions-of-unreal-engine
[descriptor58]: https://dev.epicgames.com/documentation/unreal-engine/API/Runtime/Projects/FProjectDescriptor
[module57]: https://dev.epicgames.com/documentation/unreal-engine/how-to-make-a-gameplay-module-in-unreal-engine?application_version=5.7
[module58]: https://dev.epicgames.com/documentation/unreal-engine/how-to-make-a-gameplay-module-in-unreal-engine?application_version=5.8
[sanitizers]: https://dev.epicgames.com/documentation/en-us/unreal-engine/using-clang-sanitizers-in-unreal-engine-projects
[bcr57]: https://dev.epicgames.com/documentation/unreal-engine/build-operations-cooking-packaging-deploying-and-running-projects-in-unreal-engine?application_version=5.7
[uht]: https://dev.epicgames.com/documentation/unreal-engine/unreal-header-tool-for-unreal-engine?lang=en-US
[live57]: https://dev.epicgames.com/documentation/unreal-engine/using-live-coding-to-recompile-unreal-engine-applications-at-runtime?application_version=5.7
[loop58]: https://dev.epicgames.com/documentation/en-us/unreal-engine/API/Runtime/Launch/FEngineLoop
[delegates58]: https://dev.epicgames.com/documentation/unreal-engine/API/Runtime/Core/FCoreDelegates?application_version=5.8
[mcp58]: https://dev.epicgames.com/documentation/unreal-engine/unreal-mcp-in-unreal-editor?application_version=5.8
[ddc]: https://dev.epicgames.com/documentation/unreal-engine/using-derived-data-cache-in-unreal-engine?lang=en-US
[debugcrash]: https://dev.epicgames.com/documentation/de-de/unreal-engine/unreal-engine-5.4-release-notes?application_version=5.4

## Open facts

All experiments below are **unverified until performed**. Run each on both exact 5.7 and 5.8 builds, recording `Build.version`, executable hash, Windows build, project revision, relevant plugins/settings, timestamps, raw stdout/stderr, logs and process exit DWORDs.

| ID | What to run on a real Windows machine | What to look at / fact settled |
|---|---|---|
| U1 | Launch the same disposable project by absolute and relative `.uproject` path, project name and `-project=`; vary working directory, spaces/non-ASCII, map URL, duplicate/malformed tokens. | Chosen project, log's actual path, dialogs, parse errors, exit values. |
| U2 | Modify a disposable asset, let autosave occur, force-stop that editor, then relaunch interactively, with `-unattended`, and with `-AutoDeclinePackageRecovery` separately and together. Repeat missing-module startup and each cancel/default response. | Bytes and timestamps of `PackageRestoreData.json`; prompt presence; restored/declined assets; whether recovery state persists; exact module-dialog response/text. |
| U3 | Launch with no stdout flags, `-stdout`, each singular/plural full-output spelling, and both flags; emit ASCII plus `ä漢🙂` at several verbosity levels; concurrently open/read the live log with Windows sharing combinations. | Actual filename, opening share mask, byte encoding/BOM, category visibility, stdout/file differences, last flushed line after forced termination. |
| U4 | In a disposable editor, execute the engine's `debug crash` console command after confirming it exists in that build; repeat normal crash, forced editor termination, and launcher job close during dump writing. | Process-tree creation timestamps/command lines/job membership; each crash-file create/write/close; reporter survival; validate dump with a debugger and XML with a parser. The command is listed in [Epic 5.4 release notes][debugcrash]; availability in these builds is part of the test. |
| U5 | Capture exit DWORD for File > Exit, accepted/cancelled window close, missing/incompatible module, failed compilation, each cancelled startup dialog, UE crash/assert, `taskkill /F`, and launcher-job closure. | Distinguish editor PID from UBT/UAT/reporter PIDs; record hexadecimal and unsigned decimal values plus preceding log lines. |
| U6 | On clean Windows profiles install Launcher 5.7/5.8 and register a source build; export both registry views; open copies with version/GUID/custom/empty associations, directly and through Project Browser/UnrealVersionSelector. | Actual registry spelling/value names and default paths; lookup precedence; selected engine; descriptor rewrite; BOM/encoding before/after; exact conversion behavior. |
| U7 | Run the displayed Build.bat command with editor closed/open, Live Coding off/on, and a second concurrent UBT build; capture `RunUAT.bat BuildCookRun -help` and the generated Project Launcher invocation. | Exact mutex wait/refusal behavior; target/configuration actually built; raw Live Coding message, error code, output DLL names; verified UAT editor-option semantics. |
| U8 | Start the same project with a recorded warm cache and an isolated genuinely empty DDC configuration; enable/disable relevant MCP plugin/auto-start settings; timestamp windows, log lines, listener and a harmless game-thread query. Repeat the startup-failure fixtures in section 6. | First usable editor/agent operation versus engine init, shader completion and asset scanning; observed latency distribution, failure exit times and project/plugin-specific readiness. |
