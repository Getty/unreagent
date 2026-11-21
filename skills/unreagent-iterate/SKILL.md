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
   fails comes back as an ordinary result, never as an error.

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
| `unresponsive` | present, as `true`, only when the service's control loop did not answer within 10 s. `running` and `pid` are then unknown and sent as `false` / `0` |

`agent` is listed only when the launcher runs its own agent. `commands` holds
names only, in no stable order. The command descriptions from the config are
not exposed through any tool.

Here is how to read the combinations:

| `running` | `desired` | What is going on |
|---|---|---|
| `true` | `true` | a process is up, which does not mean it is ready |
| `false` | `false` | stopped by `ue_stop`, not started at launch (`manualStart`), or the editor executable does not exist |
| `false` | `true` | the process ended. Either a restart is pending (after a crash, waiting out `restartDelaySeconds`), or none will come (exit 0 under `on-failure`, policy `never`, `maxRestarts` reached, or a failed start) |

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

- **Already running**: the process is left alone. `desired` becomes `true` and
  `restarts` resets to 0.
- **Restart pending**: the editor starts now instead of waiting.
- **Returns at spawn time.** After that, the editor still needs anything from
  seconds to many minutes to load, depending on the project and the state of
  its shader cache.

**A failed start is never an error result.** The JSON looks like success even
when no editor came up. The cause goes only to the launcher log (see Where the
output is). Read the fields, not the absence of an error:

| Result | Meaning |
|---|---|
| `running: true` | a process was spawned. It can still die while starting up |
| `running: false, desired: false` | the executable does not exist (wrong `unreal.editor`, engine root not found). There will be no retry |
| `running: false, desired: true, restarts: 1` | spawning failed for some other reason, such as permissions. It is retried after `restartDelaySeconds` |
| `running: false, desired: true, restarts: 0` | spawning failed and `unreal.restart` is `never`. There will be no retry |

`ue_start` cannot see a process that spawns and then dies during startup
(missing or stale modules, a project that does not load, a crash). To catch
that, call `status` again some seconds later and look at `logs`. A different
`pid` or a higher `restarts` means it died, and the launcher has restarted it
or is about to.

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
spawn-time report. It also works as a plain start when the editor is not
running, and it resets `restarts` to 0. All the `ue_start` caveats apply. In
particular, `running: false` in its result means the new editor never came up.

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
`unreal.maxRestarts`, where the default 0 means unlimited. Only automatic
restarts count. A manual `ue_start` or `ue_restart` resets the count.

**Crash loop**: an editor that dies during startup is restarted every few
seconds forever, and `restarts` keeps climbing. `ue_stop` breaks the loop. The
output of the last attempts stays in `logs`.

Before every start (the first one, a manual one, or an automatic one), the
launcher runs two hooks:

- **`unreal.killCrashReporter`** (default on) kills `CrashReportClientEditor.exe`
  and `CrashReportClient.exe` by image name, so it hits every running crash
  reporter, not only this editor's.
- **`unreal.cleanOnRestart`** (default off) deletes
  `Saved/Autosaves/PackageRestoreData.json` and everything under
  `Saved/Crashes/`. With it on, a crash's report is gone a few seconds later,
  when the automatic restart runs. `logs` still holds the editor's last
  output. To keep the crash report itself, the user has to turn the option
  off.

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
| `logs` | the last 500 lines of editor stdout/stderr | no launcher lines, no build output |
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
[ue] start failed: <error>
[ue] program not found — no further attempt (check path / installation)
[ue] stopped (manual)
[ue] restarted (manual)
```

`restarted (manual)` is logged after every `ue_restart`, also when the start
failed. Look for `start failed` just above it.

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
   and `restarts: 0`. A new `pid` or a higher `restarts` means the editor
   crashed, and the launcher has restarted it or is about to.
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
| `ue_start` returns `running: false, desired: false` | the editor executable does not exist | the user fixes `unreal.editor` or the engine root. The launcher log names the path |
| `ue_start` returns `running: false, desired: true, restarts: 1` | the spawn failed, and a retry is scheduled | see `start failed:` in the launcher log |
| `running: true` at start, then a new `pid` or higher `restarts` | the editor crashed during startup and is being restarted | `ue_stop`, then read `logs` |
| `restarts` climbs every few seconds | a crash loop, unlimited by default | `ue_stop` breaks it |
| `Error: service "ue" is not responding` | the control loop was busy for more than 10 s | wait, call `status`, then retry |
| `"unresponsive": true` in `status` | same cause | do not `ue_start`; check again |
| `logs` is empty for `ue` | the editor has not written anything yet, or `unreal.args` lacks `-stdout` | check `status`. The user restores the args |
| `logs` is empty for `agent` | the agent runs in the foreground | expected. Its output is on the console |
| `Error: unknown service "agent"` | the launcher runs no agent of its own | expected with an external agent |
| `logs` starts with `[... N lines omitted; the log keeps only the last 500]` | N older lines were dropped from the buffer | a larger `lines` does not help; use the launcher log or `Saved/Logs/` |
| plugin tools fail right after a restart | the editor is still loading | wait for the plugin, as above |

## See also

- `unreagent-build`: `run_command`, compile and package, and how to read a
  build result.
- `unreagent-files`: `read_file` for logs and config on disk.
- `unreagent-runtimes`: `run_python` / `run_node`, for searching large log
  files on the host.
