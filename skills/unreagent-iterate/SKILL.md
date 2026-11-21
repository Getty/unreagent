---
name: unreagent-iterate
description: >
  Start, stop and restart the Unreal Editor through the launcher (ue_start,
  ue_stop, ue_restart) and check it with status and logs. Use for the edit ->
  compile -> restart loop, after a crash or hang, and before trusting a
  "running": true.
---

# unreagent editor loop

The launcher runs the Unreal Editor as a supervised process, the service `ue`,
and gives you five tools around it:

| Tool | Parameters | What it does |
|---|---|---|
| `status` | none | a snapshot of every supervised process, plus the command names `run_command` accepts |
| `ue_start` | none | starts the editor if no editor process exists |
| `ue_stop` | none | kills the editor and its process tree, then keeps it down |
| `ue_restart` | none | `ue_stop` followed by `ue_start`, in one step |
| `logs` | `service`, `lines` | the last lines the process wrote to stdout/stderr |

Compiling is `run_command`, covered in `unreagent-build`.

Three facts shape everything below:

1. **These tools manage a process, not an editor session.** `running: true`
   means an editor process exists. It does not mean the editor has finished
   loading, the project is open, or the in-editor plugin is answering.
2. **Stopping is a kill.** There is no graceful shutdown, no save and no
   confirmation.
3. **A start reports success as soon as the process is spawned.** A start that
   spawns no process is an error result. An editor that spawns and dies a
   moment later is not: `status` shows that afterwards, in `lastExit`.

## status

```json
{
  "services": [
    {"name": "ue", "running": true, "pid": 12345, "restarts": 0, "desired": true},
    {"name": "agent", "running": true, "pid": 12346, "restarts": 0, "desired": true}
  ],
  "commands": ["compile", "package"]
}
```

| Field | Meaning |
|---|---|
| `running` | a process exists right now: spawned and not yet exited |
| `pid` | its process id; `0` when nothing is running |
| `desired` | the launcher wants it up. Set by `ue_start` / `ue_restart` and at launch (unless `unreal.manualStart` is set). Cleared by `ue_stop`, and by a start that found no executable |
| `restarts` | automatic restarts since the last `ue_start` / `ue_restart`, which reset it to 0. `ue_stop` leaves it unchanged |
| `lastError` | present only while the last start attempt failed: why no process was spawned (the executable is missing, cannot be run, …). A start that spawns a process removes it |
| `lastExit` | how the last run that ended on its own ended, such as `"exit 1"`. Absent until one ends. `ue_start` and `ue_restart` remove it, the kill of `ue_stop` or `ue_restart` does not set it, and automatic restarts keep it |
| `unresponsive` | present, as `true`, only when the service's control loop did not answer within 10 s. `running` and `pid` are then unknown and sent as `false` / `0` |

`agent` is listed only when the launcher runs its own agent. `commands` holds
names only, in no stable order. The command descriptions from the config are
not exposed through any tool.

Here is how to read the combinations:

| `running` | `desired` | What is going on |
|---|---|---|
| `true` | `true` | a process is up, which does not mean it is ready |
| `false` | `false` | stopped by `ue_stop`, not started at launch (`manualStart`), or the editor executable does not exist |
| `false` | `true` | the process ended. Either a restart is pending (after a crash, waiting out `restartDelaySeconds`), or none will come (exit 0 under `on-failure`, policy `never`, `maxRestarts` reached, the launcher gave up on an editor that keeps failing at once, or a failed start) |

No field tells "restart pending" apart from "given up". `restarts` goes up when
a restart is scheduled, not when it happens, so `status` already shows the
higher count during the wait. Call `status` again after the restart delay, 3 s
by default. A pending restart now shows `running: true` with the same
`restarts`. A service that gave up still shows `running: false`. Either way,
`ue_start` brings it up.

`unresponsive: true` usually means the loop is busy carrying out a stop or a
start. It does that work itself, including the pre-start hooks below. Wait and
call `status` again. Do not call `ue_start` on it: the call would queue behind
the work already in progress.

## ue_start

The result is the `ue` entry from `status`, as it stands right after the
attempt:

```json
{"name":"ue","running":true,"pid":12345,"restarts":0,"desired":true}
```

- **Already running**: the process is left alone. `desired` becomes `true`,
  `restarts` resets to 0 and `lastExit` is removed.
- **Restart pending**: the editor starts now instead of waiting.
- **Returns at spawn time.** After that, the editor still needs anything from
  seconds to many minutes to load, depending on the project and the state of
  its shader cache.

**A start that spawns no process is an error result**:
`Error: service "ue" did not start: <lastError>`, with the `ue` entry as JSON
on the next line. The fields of that entry say whether a retry comes:

| Result | Meaning |
|---|---|
| `running: true`, no error | a process was spawned. It can still die while starting up |
| error, `desired: false` | the executable does not exist (wrong `unreal.editor`, engine root not found). There will be no retry |
| error, `desired: true, restarts: 1` | spawning failed for some other reason, such as permissions. It is retried after `restartDelaySeconds` |
| error, `desired: true, restarts: 0` | spawning failed and will not be retried, as under `unreal.restart: never` |

`ue_start` cannot see a process that spawns and then dies during startup
(missing or stale modules, a project that does not load, a crash). To catch
that, call `status` again some seconds later and look at `logs`. `ue_start`
removed `lastExit`, so a `lastExit` now means a run ended since the call. A
different `pid` or a higher `restarts` means the launcher has restarted it or
is about to. `running: false` that stays past the restart delay means no
restart is coming (see Automatic restarts).

## ue_stop

Kills the editor right away and clears `desired`, so the editor stays down
until `ue_start` or `ue_restart`. The call blocks until the process has exited
and its remaining output has been drained (at most 2 s). When it returns, the
editor's last lines are in `logs`.

```json
{"name":"ue","running":false,"pid":0,"restarts":0,"desired":false}
```

## ue_restart

One step that does `ue_stop` and then `ue_start`: the same kill, then the same
spawn-time report. While an automatic restart is pending, it takes that
restart's place as `ue_start` does and spares the crash evidence; a separate
`ue_stop` first would cancel the restart and not spare it (see Automatic
restarts). It also works as a plain start when the editor is not running, and
it resets `restarts` to 0 and removes `lastExit`. All the `ue_start` caveats
apply. In particular, a new editor that never came up is the same
`Error: service "ue" did not start: …` result.

## What a stop does to the process tree

On Windows, each editor instance runs inside a Job Object created with
`KILL_ON_JOB_CLOSE`. `ue_stop`, `ue_restart` and the launcher's own exit close
that job, and the operating system then terminates every process in it at once:
the editor, its ShaderCompileWorkers, a crash reporter it started, and any tool
it launched. When the editor exits on its own, its job is closed too, which
kills whatever it left behind. What follows from this:

- **Unsaved changes are lost.** Save through the in-editor plugin before you
  stop or restart. These tools cannot save.
- **Nothing offers to recover them later.** `-unattended`, on by default, also
  suppresses the recovery prompt on the next start.
- **Work in flight inside the editor is cut off mid-way**: shader compiles,
  saves, imports, anything it was running.
- **Nothing is left behind as an orphan, as long as the job exists.** No stray
  ShaderCompileWorker holds files locked for the next build. If the launcher
  could not create the job, or could not assign the editor to it, the launcher
  log shows `[ue] WARN job object could not be created: …` or
  `[ue] WARN job assign failed: … — the process runs unsupervised`. In that
  case, only the editor process itself is killed, and its children can
  survive.
- **`run_command` builds are not affected.** Each one runs in its own job, so
  `ue_stop` and `ue_restart` leave it alone. All of them die when the launcher
  exits.

A non-Windows launcher build, used only for development, kills just the direct
editor process. Its children survive there.

## Automatic restarts

`unreal.restart` sets the policy:

| Policy | Restarts after |
|---|---|
| `on-failure` (default) | a non-zero exit. Exit 0, a normal close, is not restarted |
| `always` | any exit |
| `never` | nothing |

The restart follows after `unreal.restartDelaySeconds` (default 3). The limit is
`unreal.maxRestarts`. Only automatic restarts count. A manual `ue_start` or
`ue_restart` resets the count.

**Crash loop**: with `maxRestarts` at its default 0, the launcher gives up on
an editor that fails 3 times in a row, each run ending within 10 s of its
start (a non-zero exit or a failed spawn). The launcher log says
`[ue] failed 3 times in a row, each within 10s of its start — giving up (no automatic restart)`,
and `status` shows `running: false, desired: true` with the last `lastExit`
(or `lastError`, when the spawn itself failed). Every failure the launcher
does not restart (this give-up, `maxRestarts` used up, policy `never`) is
followed by a line that names the dump folder:
`The editor failed and is not restarted. Any crash dumps are in <project dir>\Saved\Crashes.`
With `unreal.cleanOnRestart` on, the line ends
`… Saved\Crashes — a retry keeps them, a new launcher run empties them (unreal.cleanOnRestart).`
Without an agent of the launcher's own, a quick failure gets a longer line
that ends with the same hint. With `unreal.project` unset, no hint is logged.
An editor that runs longer than 10 s before each crash is restarted without
end, and `restarts` keeps climbing. A `maxRestarts` above 0 stops after that
many restarts instead, however long each run lasted. `ue_stop` breaks any loop,
and `ue_start` tries again with the count reset. The output of the last
attempts stays in `logs`.

Before every start (the first one, a manual one, or an automatic one), the
launcher runs two hooks. Both spare the evidence of a crash on a start that
follows one:

- An automatic restart.
- A `ue_start` or `ue_restart` while an automatic restart is still pending,
  within `restartDelaySeconds` of an exit the policy restarts (a crash, and
  under `always` a clean exit too) or of a failed spawn. It starts the editor
  in place of that restart and spares the evidence the same way.
- The first `ue_start` or `ue_restart` after the editor failed and was not
  restarted (given up on, `maxRestarts` used up, policy `never`).

A `ue_stop` during the restart delay cancels the pending restart. The
`ue_start` after it is a plain manual start and does not spare the evidence.

The hooks:

- **`unreal.killCrashReporter`** (default on) kills `CrashReportClientEditor.exe`
  and `CrashReportClient.exe` by image name, so it hits every running crash
  reporter, not only this editor's. A start that follows a crash skips the
  kill, because the reporter may still be writing the report.
- **`unreal.cleanOnRestart`** (default off) deletes
  `Saved/Autosaves/PackageRestoreData.json` before every start, and everything
  under `Saved/Crashes/` before every start that does not follow a crash. A
  start that keeps them logs `Recovery: Saved/Crashes kept (…)`. The crash
  reports are in `Saved/Crashes/` in the project directory (`read_file`,
  `list_dir`, see `unreagent-files`). With the option on, read them before
  a start that does not follow a crash, such as a `ue_restart` of an editor
  that came back up, or a `ue_start` after a `ue_stop` that cancelled a
  pending restart: that start empties the folder. To keep a crash's dumps
  past the next start, leave the restart to the launcher, or call `ue_start`
  while `status` shows `running: false, desired: true`. Do not call
  `ue_stop` before that `ue_start`.

## logs

- `service` (string, default `"ue"`): `ue`, or `agent` when the launcher runs
  its own agent. Any other name returns `Error: unknown service "<name>"`.
  `run_command` commands are not services. Their output exists only in the
  result of the call that ran them.
- `lines` (integer, default 50). An integer string such as `"200"` counts as
  200. Missing, `null` or `""` gives 50. `0`, a negative integer, or more lines
  than are buffered returns the whole buffer. Any other value, such as `1.5`,
  `"all"` or `true`, returns `Error: 'lines' must be an integer, got …`; a
  whole number past the 64-bit range, such as `1e20`, returns
  `Error: 'lines' must be an integer between -9223372036854775808 and 9223372036854775807, got 1e+20`.

The result is the lines joined by newlines: no header, no timestamps. An empty
result means nothing is buffered. Once the buffer has dropped older lines, a
request for more than it holds, or for all of it (`0` or negative), starts with
`[... N lines omitted; the log keeps only the last 500]`. N is how many lines
it has dropped since the launcher started, restarts included; a larger `lines`
does not bring them back.

A reply holds at most 48 KiB. When the lines you asked for are longer, their
oldest bytes go, and `[... B bytes omitted; a reply holds at most 48 KiB]`
comes first, or right below the lines marker. The first line you see can then
start mid-line, and a single line longer than 48 KiB comes back as its end
only. No `lines` value reaches the cut bytes, since every reply ends at the
newest line. Read them in the launcher log or the editor's log file with
`read_file` (below).

What the buffer holds:

- **The last 500 lines per service.** On a busy editor startup, 500 lines is a
  short stretch; `lines: 0` shows whether older ones were dropped.
- **Only the process's own stdout and stderr**, interleaved as they arrived.
  The editor writes its log there because of the default arguments
  `-stdout -FullStdOutLogOutput`. If `unreal.args` is set without them, `logs ue`
  stays nearly empty. `unreal.args` is a list: setting it replaces the
  defaults whole, and so does a value in `unreagent.local.yaml`.
- **It survives restarts, and nothing marks where one instance ends and the
  next begins.** After `ue_restart`, the old editor's last lines are followed
  directly by the new editor's startup. Tell them apart by content, not by
  position.
- **None of the launcher's own lines.** Start failures, `exited (exit 3)` and
  restart notices go to the launcher log, not to this buffer.
- **`service: "agent"` is always empty while the agent runs in the
  foreground**, which is the default for the launcher's own agent. Its output
  goes straight to the console.

## Where the output is

| Source | Holds | Limits |
|---|---|---|
| `logs` | the last 500 lines of editor stdout/stderr | no launcher lines, no build output, at most 48 KiB per reply |
| launcher log | every editor line as `[ue] <line>`, plus the launcher's own lines | is a file only when the launcher runs its own agent in the foreground |
| `Saved/Logs/<ProjectName>.log` in the project | the log file the editor writes itself | written by UE, not by the launcher |

When the launcher runs its own agent in the foreground (the default), it writes
its log to `unreagent.log` next to the `.uproject`. The file is truncated at
every launcher start. Without a foreground agent (an external agent, or
headless), the log goes to the launcher's console, which no tool can read.
Each line starts with a `HH:MM:SS` timestamp. Lines to look for:

```
[ue] started (pid 12345)
[ue] exited (exit 3)
[ue] restart 1 in 3s
[ue] no automatic restart (policy=on-failure)
[ue] failed 3 times in a row, each within 10s of its start — giving up (no automatic restart)
The editor failed and is not restarted. Any crash dumps are in <project dir>\Saved\Crashes.
[ue] start failed: <error>
[ue] program not found — no further attempt (check path / installation)
[ue] stopped (manual)
[ue] restarted (manual)
```

`restarted (manual)` is logged only when `ue_restart` spawned a new editor. A
start that failed logs `start failed: …` instead.

`read_file` (`unreagent-files`) returns at most 256 KiB per call, and a negative
`offset` counts from the end of the file: `offset: -65536` returns the last
64 KiB of `unreagent.log` or of the editor's log file, where a crash is. To
search a long log, use `run_python` / `run_node` (`unreagent-runtimes`).

## The edit loop

| You changed | Before it takes effect |
|---|---|
| assets, levels, Blueprints, through the in-editor plugin | nothing. The change is already live |
| C++ source, `*.Build.cs`, `*.Target.cs` | stop, compile, start |
| `Config/*.ini`, `.uproject`, `.uplugin` | a restart, because most settings are read at startup. A C++ plugin newly enabled also needs a compile |

For C++:

1. **Save** open work through the in-editor plugin, because the stop is a
   kill.
2. **`ue_stop`.**
3. **`run_command` with `{"name": "compile"}`.** Read the exit code first; see
   `unreagent-build`.
4. **Non-zero exit**: fix the error and compile again. Leave the editor
   stopped in the meantime. A start now would load the old binaries or fail on
   modules that are out of date.
5. **`ue_start`.**
6. **`status` some seconds later.** Expect `running: true`, the same `pid`,
   `restarts: 0` and no `lastExit`. A `lastExit`, a new `pid` or a higher
   `restarts` means the editor crashed.
7. **`logs`**: look for errors in the startup output.
8. **Wait for the in-editor plugin** before you use its tools (see below).

Stop the editor before you compile, not after. The `run_command` tool
description gives the same order: `ue_stop` before `compile`, then `ue_start`
to load the new modules. UE 5 enables Live Coding by default, and
UnrealBuildTool refuses to build while a Live Coding session is active. It
says so in its output: `Unable to build while Live Coding is active`. Stopping
first rules that conflict out.

### Waiting for the in-editor plugin

The in-editor plugin's MCP server runs inside the editor process. It goes away
with every stop, kill or crash, and comes back only once the restarted editor
has loaded far enough to start it. Until then, that server's tools fail, and
nothing in `status` reflects this. Check the plugin itself:

- With the UE LLM Toolkit bridge, `unreal_status` returns `connected: false`
  (as an error result) until the editor is reachable, and `connected: true`
  after that.
- While the editor is unreachable, the bridge's tool list contains only
  `unreal_status`. If your client fetched the list during that window, whether
  it picks up the full list later depends on the client.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `ue_start` returns `Error: service "ue" did not start: …` with `desired: false` | the editor executable does not exist | the user fixes `unreal.editor` or the engine root. The error names the path |
| `ue_start` returns `Error: service "ue" did not start: …` with `desired: true, restarts: 1` | the spawn failed, and a retry is scheduled | the error names the cause |
| `running: true` at start, then `lastExit`, a new `pid` or a higher `restarts` | the editor died during startup. The launcher restarts it, or gave up (next row) | `ue_stop`, then read `logs` |
| `running: false, desired: true` and `lastExit` that stay past the restart delay | the editor failed and no restart is coming, for example after 3 quick failures in a row | read `logs` and `Saved/Crashes/`, fix the cause, then `ue_start` |
| `restarts` climbs every few seconds | a crash loop of an editor that runs more than 10 s each time, or one under a `maxRestarts` limit | `ue_stop` breaks it |
| `Error: service "ue" is not responding` | the control loop was busy for more than 10 s | wait, call `status`, then retry |
| `"unresponsive": true` in `status` | same cause | do not `ue_start`; check again |
| `logs` is empty for `ue` | the editor has not written anything yet, or `unreal.args` lacks `-stdout` | check `status`. The user restores the args |
| `logs` is empty for `agent` | the agent runs in the foreground | expected. Its output is on the console |
| `Error: unknown service "agent"` | the launcher runs no agent of its own | expected with an external agent |
| `logs` starts with `[... N lines omitted; the log keeps only the last 500]` | N older lines were dropped from the buffer | a larger `lines` does not help; use the launcher log or `Saved/Logs/` |
| `logs` starts with `[... B bytes omitted; a reply holds at most 48 KiB]`, or has it below the lines marker | the lines asked for are more than a reply holds; their oldest B bytes were cut, and the first line can start mid-line | no `lines` value brings them back; read the launcher log or `Saved/Logs/` with `read_file` |
| plugin tools fail right after a restart | the editor is still loading | wait for the plugin, as above |

## See also

- `unreagent-build`: `run_command`, compile and package, and how to read a
  build result.
- `unreagent-files`: `read_file` for logs and config on disk.
- `unreagent-runtimes`: `run_python` / `run_node`, for searching large log
  files on the host.
