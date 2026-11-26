# unreagent build commands: when a call fails

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Error: unknown command "build"` | the name is not configured | use a name from `status`. Only the user can add commands |
| `Error: unknown command "compile"`, and `status` lists neither `compile` nor `package` | the launcher found no engine root or no `.uproject` at start and left the built-in commands out | the user sets `UE_ROOT` or `engineRoot`, or `unreal.project`, then restarts the launcher |
| `Error: start failed: fork/exec …/Engine/…: …` | the program is not at that path | the user checks the engine root (`UE_ROOT` or `engineRoot`) |
| `exit 6` while the editor is running, and the output has `Unable to build while Live Coding is active` | a Live Coding session is active in the open editor (UE 5.8.4 source) | `ue_stop`, then compile again |
| `exit` non-zero while the editor is running, and the output names the running editor or a module binary it cannot write, not your code (this case was not run; the wording is not confirmed) | the open editor is in the build's way | `ue_stop`, then compile again |
| `exit 10` at once, and the output says a conflicting instance is already running | another UnrealBuildTool of the same engine is running, and this command line has no `-waitmutex` (UE 5.8.4 source). The built-in `compile` has the flag and waits instead | wait for the other build, then run it again |
| `exit` non-zero, an omitted marker, and the errors look like follow-ons | their cause is among the omitted lines or bytes | fix what is visible, then compile again |
| `exit` non-zero right away, and the project has no `Source/` | there is no `<ProjectName>Editor` target (no `Source/<ProjectName>Editor.Target.cs`; standard engine behaviour, not run here) | if `Plugins/` holds C++ plugins, they need an `UnrealEditor` build, a command the user adds. Otherwise there is nothing to compile |
| `exit` non-zero, `[... N lines omitted]` between the first lines and the last | the output was more than a failing reply's 10,000 bytes; the N lines between its first 4 KiB and its end were cut (past 2 MiB, N includes the dropped middle) | they are gone, and a rerun of the same state cuts the same way. Fix what is visible and compile again; for the whole output, the user adds a command that writes it to a file, read with `read_file` |
| `[... B bytes omitted; a failing call's reply holds at most 10 KB]` | a single line was longer than its share of a failing reply: a first line over 4 KiB is cut to its start, a last line that does not fit to its end | as for the lines marker above |
| `exit 0`, then `[... N lines omitted]` | the first N lines of a successful run's output were cut | they are gone. Rerun only if you must |
| `exit 0`, then `[... B bytes omitted; a reply holds at most 48 KiB]`, alone or below the lines marker | the last 300 lines were more than a reply holds, so their oldest B bytes were cut (past 2 MiB, B can include the dropped middle); the first line can start mid-line | they are gone, and a rerun cuts the same way. For the whole output, the user adds a command that writes it to a file |
| the call never returns, or the client times out | no timeout exists, and the run continues on the host. A `compile` also waits, without a time limit, behind any other UnrealBuildTool run holding the same engine installation's mutex | do not retry. Watch the launcher log for `[cmd:<name>] done` |
| a run of more than 4 hours ends in a dropped connection, with no result | the reply missed the server's 4-hour write deadline | the run did finish. Read `[cmd:<name>] done (exit N)` in the launcher log |
