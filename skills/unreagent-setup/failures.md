# unreagent setup: failures

*Measured* means seen on the walk-through (Windows 11, UE 5.8.4, one
machine). The launcher lines are quoted from its code. Lines the launcher
logs are in `unreagent.log` next to the `.uproject` when the agent runs in
the launcher's window (skill `unreagent-iterate`, section `logs`).

Where a row says `ue_stop`, or to edit or delete a file the user made, the
main skill's rule holds: the user agrees first.

## `unreagent init`

| What you see | What it means | What to do |
|---|---|---|
| `no unreagent.yaml and no .uproject in <dir>` ... `(unreagent init wrote nothing)` | `init` looks next to the executable, and there is no project there | run the `unreagent.exe` that lies next to the `.uproject` |
| `no unreagent.yaml in <dir> and several .uproject files there` | without a config the launcher cannot tell which project is meant | ask the user which one; the fix is an `unreagent.yaml` that sets `unreal.project`, which you propose to them |
| a non-zero exit, `preflight: N blocking finding(s)`, and the only `[BLOCKING]` line is `MCP server cannot listen on` the address of the running launcher | the running launcher holds its own port | expected; read `(unreagent init wrote ...)` for what was written |
| `(unreagent init wrote nothing)` where you expected files | `-check` or `-config` was given; or the project already has a config and a `CLAUDE.md`, and `skills.materialize` is off | drop the flag; for the last case read the `skills: not written into the project` finding and section `convert` |
| `unreagent init does not overwrite, and could not create <path>` | the file appeared between the look and the write, or the directory cannot be written | read the cause at the end of the line; run `init -check` to see the state |
| after `init`, `init left them and wrote no skills, topic agents or rules file` | `unreagent-*` directories it cannot tell are its own are in `skills.dir`; the config it wrote has `skills.materialize` on, so the next start replaces and deletes them | section `convert`, before any launcher start |
| the same hold WARN after every unreagent update, in a project that was set up | with `skills.agents: false`, or a `skills.dir` that is not `<project>/.claude/skills`, nothing marks an earlier materialization, so every changed skill looks unproven | if the directories are the launcher's own copies, a launcher start refreshes them and the next `init` goes through |
| `agents: in the way and left alone` | a file at a launcher file's name was edited, or was never the launcher's | section `convert` |
| `skills.dir <dir> could not be read` | a read error in `.claude/skills` | fix the cause the line names; `init` writes no skills while it holds |

## The build and the editor

| What you see | What it means | What to do |
|---|---|---|
| `Error: unknown command "compile"` | the launcher found no engine or no `.uproject` at its start and offers no `compile` | skill `unreagent-build`; the engine root goes into `unreagent.local.yaml`, then the user restarts the launcher |
| `compile` exits non-zero | a build error | read the reply as skill `unreagent-build` says. If the errors are in the toolkit's own source and the engine is not UE 5.8, the branch most likely does not fit (unverified): tell the user, do not patch the plugin |
| `compile` does not come back | a long build, or one waiting behind another build | skill `unreagent-build`, "Duration, timeouts and calls that do not come back". Do not start a second one |
| the editor exits with code 1 some seconds after each start; `logs` names a module that was not found | the project's own module is not built (*measured*: seven starts, then the launcher gave up) | `ue_stop`, `run_command` `compile`, `ue_start`; skill `unreagent-iterate`, section `failures` |
| the editor runs for about 80 s, then exits with code 3221225477 (0xC0000005); crash folders appear under `Saved/Crashes`; `status` shows `restarts` climbing | an enabled plugin's module is not built (*measured* once, with the toolkit added and not built) | `ue_stop`, `run_command` `compile`, `ue_start` |
| `[ue] failed 3 times in a row, each within <window> of its start — giving up (no automatic restart)`, then `The editor failed and is not restarted.`, which goes on with `... still look unbuilt: ...` when the launcher's module check still finds some | the launcher stopped restarting an editor that fails the same way. `<window>` is `10s` when each of the three runs ended within 10 s of its start (*measured* with the unbuilt project module, two rows up), and `5m0s` when each ended within five minutes (the unbuilt plugin module, one row up; from the code) | build, then `ue_start`, which starts with a fresh count; skill `unreagent-iterate`, section `restarts` |
| `WARN plugin modules look unbuilt: ...` or `WARN project modules look unbuilt: ...` in the launcher log | the launcher's check before the editor's first start of this launcher run. It is a guess from the binaries on disk and says so | build before the editor is used. The check runs at launcher start only: a plugin added later gets no such line in this run |
| the editor stays up but its log has no `LogUnrealClaude: MCP Server started` line | the plugin did not load: not enabled, refused for its `EngineVersion`, or not the toolkit | `init -check`: the `plugin EngineVersion` and `plugin modules` lines; then the editor's log |

## After the restart

| What you see | What it means | What to do |
|---|---|---|
| no `unreal_status` tool | the launcher configured no bridge. Its log says `no UE LLM Toolkit bridge (plugin not found below Plugins/)` on built-in defaults; with an `unreagent.yaml`, the config has no `mcp.extraServers.ue-llm-toolkit` entry | `init -check`: the `UE LLM Toolkit:` finding prints the entry to add. Then another restart |
| `unreal_status` is there and returns `connected: false` | the bridge runs, the editor's plugin does not answer: the editor is down, still loading, or runs without the plugin | `status` and `logs` for `ue` (skill `unreagent-iterate`); skill `ue-context` for the result's fields |
| `WARN MCP server 'ue-llm-toolkit': ... not reachable after ...` in the launcher log | the launcher waited for the in-editor server and gave up waiting; the line names what it knows | the same as above. The bridge keeps running, and `unreal_status` works once the editor answers (*measured*: a bridge started while the editor was down answered after the editor came up) |
| `MCP server 'ue-llm-toolkit':` lines about `npm install` failing | the bridge's dependencies. *Measured*: the first `npm install` failed and the launcher's own retry succeeded, about 10 s in all | read the lines that follow, down to `smoke test OK — bridge answers initialize.` or a `WARN ... smoke test failed` with its cause. `npm` needs the network |
| the user says the launcher window closed at once | most likely the port (*measured*) | section `human-steps`, "A taken port" |
| the new session asks the project questions again | the kickstart block is still in `CLAUDE.md` | finish the interview and delete the block, both marker lines |
| the new session has no `unreagent-*` agents or skills as files | `skills.materialize` is off in the config that was loaded, or `init` held | `init -check`: the `skills:` and `agents:` findings |
