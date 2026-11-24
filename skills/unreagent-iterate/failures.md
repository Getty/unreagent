# Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `ue_start` returns `Error: service "ue" did not start: …` with `desired: false` | the editor executable does not exist | the user fixes `unreal.editor` or the engine root. The error names the path |
| `ue_start` returns `Error: service "ue" did not start: …` with `desired: true, restarts: 1` | the spawn failed, and a retry is scheduled | the error names the cause |
| `running: true` at start, then `lastExit`, a new `pid` or a higher `restarts` | the editor died during startup. The launcher restarts it, or gave up (next row) | `ue_stop`, then read `logs` |
| `running: false, desired: true` and `lastExit` that stay past the restart delay | the editor failed and no restart is coming, for example after 3 quick failures in a row | read `logs` and `Saved/Crashes/`, fix the cause, then `ue_start` |
| `restarts` climbs every few seconds | a crash loop of an editor that runs more than 5 minutes each time, or one under a `maxRestarts` limit | `ue_stop` breaks it |
| `lastExit` is exit 1 some seconds after every start, and `logs` hold `Message dialog closed, result: Ok` with a module-not-found text (the form seen under the default arguments) or `Incompatible or missing module` (seen on a start without `-unattended`) | the project's C++ module is not built; the UE 5.8.4 source treats a module built for another engine version the same way (not run). The editor exits instead of waiting for a click while `-unattended` or `-stdout` is among its arguments; with neither (`unreal.unattended` off and an `unreal.args` list without `-stdout`) it stays running at a `Missing <Project> Modules` box instead (UE 5.8.4 source, not run) | `ue_stop`, `run_command` `compile`, then `ue_start` |
| the editor runs, `unreal_status` stays `connected: false` for minutes, and `logs` or `Saved/Logs/` hold `Skipping load of '<plugin>'` or `'<plugin>' is Incompatible` | the plugin's `.uplugin` sets an `EngineVersion` other than this engine's compatible version (`<major>.<minor>.0`), and under `-unattended` the editor skips it instead of asking (UE 5.8.4 source; seen on Windows with UE 5.7.4 and 5.8.4) | removing the `EngineVersion` field from the `.uplugin` lifts the check, and so does setting it to `<major>.<minor>.0` of the engine: `5.8.0` for UE 5.8.4, not `5.8.4` (seen: `5.7.0` loads on 5.7.4). An engine build marked as a licensee version compares its own patch number instead (read in the UE 5.8.4 source, not run). The plugin must still be built for this engine. It is a change to a third-party plugin: ask the user, then `ue_restart` |
| `ue_start`, `ue_stop` or `ue_restart` returns `Error: service "ue" is not responding: it did not take the request within 10s; nothing was done` | the control loop was busy for more than 10 s and never took the call | wait, call `status`, then retry |
| a control call returns `Error: service "ue" is busy with its pre-start step and did not take the request within 10s; nothing was done, try again shortly` | the loop is running the pre-start step of another start and never took the call | wait a few seconds, then retry |
| a control call returns `Error: service "ue" took the request and is still carrying it out after 10s` (with `(busy with its pre-start step)` while the hooks run), ending in `do not repeat it, check status for the outcome` | the call is still under way. It has not failed | do not repeat it: a second `ue_restart` waits behind the first and then restarts the editor the first one just spawned. Poll `status` until it answers without `unresponsive`, then read the `ue` entry as the call's result: `running: true` for a start that spawned, `desired: false` for a stop, a `lastError` for a start that failed |
| `"unresponsive": true` in `status` | the control loop did not answer within 10 s | do not `ue_start`; check again |
| `logs` is empty for `ue` | the editor has not written anything yet, or `unreal.args` lacks `-stdout` | check `status`. The user restores the args |
| `logs` is empty for `agent` | the agent runs in the foreground | expected. Its output is on the console |
| `Error: unknown service "agent"` | the launcher runs no agent of its own | expected with an external agent |
| `logs` starts with `[... N lines omitted; the log keeps only the last 500]` | N older lines were dropped from the buffer | a larger `lines` does not help; use the launcher log or `Saved/Logs/` |
| `logs` starts with `[... B bytes omitted; a reply holds at most 48 KiB]`, or has it below the lines marker | the lines asked for are more than a reply holds; their oldest B bytes were cut, and the first line can start mid-line | no `lines` value brings them back; read the launcher log or `Saved/Logs/` with `read_file` |
| plugin tools fail right after a restart | the editor is still loading | wait for the plugin (the main skill, "Waiting for the in-editor plugin") |

## A start before the compile succeeded

The edit loop in the main skill leaves the editor stopped until the compile
succeeded. This is what happens when it is started anyway.

A start before the compile succeeded does not wait at a dialog either, as
long as `-unattended` or `-stdout` is among the editor's arguments. The
default arguments carry both. An
editor whose project module is missing exits with code 1 and names the module
in its log. Under the default arguments, which include `-unattended`, what
was seen is a message box the editor answered itself: a
`Message dialog closed, result: Ok` line carrying the module-not-found text,
some 9 to 17 s after the start. A start without `-unattended` showed
`Incompatible or missing module: <name>` and exited within 2 s (both seen on
Windows with UE 5.7.4 and 5.8.4). With either flag present, nothing waits for
a click. The
launcher then restarts the editor by its restart policy, and each start ends
the same way: `ue_stop`, compile, `ue_start`.

With neither flag, the editor asks in a Yes/No box titled
`Missing <Project> Modules` whether to rebuild, and waits there for a human
(read in the UE 5.8.4 source, not run). That takes both `unreal.unattended`
turned off and an `unreal.args` list without `-stdout`: setting `unreal.args`
replaces the default list, so a config with its own list has only the flags
it names. If the editor stays `running: true` after a start that came before
a successful compile, with no exit and the in-editor plugin unreachable, read
`unreal.args` and `unreal.unattended` in `unreagent.yaml` and
`unreagent.local.yaml`. With neither flag there, expect that box: `ue_stop`,
compile, `ue_start`.

## A compile while the editor is running

The edit loop in the main skill stops the editor before the compile. This is
what a compile meets when the editor is up.

A running editor has the module binaries loaded. With
a Live Coding session active in it, UnrealBuildTool refuses the build: exit 6
and `Unable to build while Live Coding is active` (UE 5.8.4 source, not run).
Under `-unattended`, the default, Live Coding does not start by itself, so
expect that only when the user changed the editor arguments or started it by
hand. What a build does against the loaded binaries without a session is not
verified: expect it to fail. If a `compile` with the editor up fails on
something other than your code, look in its output for Live Coding, the
running editor or a binary it cannot write, then stop the editor and compile
again. `unreagent-build` has the exit codes.
