# C++ in an unreagent project: when a step fails

The loop is in the skill itself. Rows about `run_command`, `ue_start` and
`status` in more depth: skills `unreagent-build` and `unreagent-iterate`.

| What you see | What it means | What to do |
|---|---|---|
| `compile` exits non-zero | a code or build error | section `compile-errors` |
| `Error: unknown command "compile"` | the launcher found no engine or no `.uproject` at its start | the user fixes the engine root or `unreal.project` and restarts the launcher (skill `unreagent-build`) |
| `compile` fails at once in a project without `Source/` | no editor target | section `project-layout`, "A Blueprint-only project gets its first C++ module" |
| `compile` does not come back | it waits behind another UnrealBuildTool of the same engine (`-waitmutex`), or builds a large project | do not call it again; watch the launcher log for `[cmd:compile] done` (skill `unreagent-build`) |
| `LNK1104` on a module DLL, or the build is refused with `exit 6` | an editor still runs with the module loaded or with Live Coding | `ue_stop`; if `status` already shows `ue` down, an editor outside the launcher runs: the user closes it |
| after `ue_start`, `lastExit: "exit 1"` within seconds, `logs` name a module not found | the module has no binary: the compile failed or never ran | `ue_stop`, `compile` until `exit 0`, `ue_start` |
| the editor crashes at startup after a good compile (`lastExit: "exit 3"`) | your code runs at load and crashes: a constructor doing world work, a null component, a failed `check` | read the editor's log (below); fix; the loop again |
| the launcher log says `WARN project modules look unbuilt` | it found the `.uproject`'s modules without binaries | compile before the editor starts |
| `spawn_actor` says `Class not found: …` | the class name or module in the path is wrong, or the editor runs the old binaries | `/Script/<Module>.<Name without prefix>`; check that the editor was restarted after the compile |
| a new `UPROPERTY` does not show in the editor | the editor runs an old binary, or the property has no edit specifier | restart after the compile; `EditAnywhere` or `VisibleAnywhere` |
| values set on a Blueprint or in a level are gone after a change | a `UPROPERTY` or a default subobject was renamed or removed | restore the name, or add a core redirect (section `reflection`) |
| a `Generated/UnrealClaude` file in the errors | a file from the toolkit's `execute_script` `cpp` type is built with the project | `cleanup_scripts`, or delete that file (section `live-coding`) |
| Blueprints derived from your class fail to compile after a change | a function or property they use was renamed or removed | restore it, or fix the Blueprints (skill `ue-blueprints`) |

## Reading a crash at startup

`logs` keeps the last 500 lines of the editor's output and survives the
restart, so the old run's last lines come before the new one's start. The
editor's own log file is `Saved/Logs/<ProjectName>.log` (UE 5.8.4 source):
read its end with `read_file` `{"path": "Saved/Logs/<ProjectName>.log",
"offset": -48128}` (skill `unreagent-files`). A crash leaves a folder in
`Saved/Crashes/`, but an ensure leaves one too without ending the editor
(Epic's crash reporting documentation). Look for your module's and classes'
names in the call stack lines and for the last lines of your log category
before the crash.
