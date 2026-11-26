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

This skill has five sections:

| Section | Holds | Load it |
|---|---|---|
| `restarts` | the two give-up rules with their launcher-log lines and what resets the count, which starts spare a crash's evidence, `unreal.cleanOnRestart` in full, `unreal.killCrashReporter` | when the editor failed and does not come back, when `restarts` keeps climbing, or before you start or restart an editor after a crash whose dumps you still need |
| `process-tree` | how a stop ends the editor's process tree on Windows (the Job Object), the crash reporter that may outlive it, when children survive, the non-Windows build | when a process outlived a stop, a file stays locked after one, or the launcher log holds a `job object` or `job assign` WARN |
| `logs` | `logs` in full (what `lines` accepts, the omitted-lines and omitted-bytes markers, the 48 KiB cut) and the launcher log file: `unreagent.log`, its rotation, `unreagent.log.1`, the launcher's lines to look for, reading a log's end with `read_file` | when `logs` starts with an `[... omitted` marker or rejects `lines`, and before you read `unreagent.log` or `unreagent.log.1` |
| `crash-diagnosis` | reading a crash or a failed start: what each `lastExit` code means, the order of the evidence (`logs`, the editor's log file and its backups, the launcher log, `Saved/Crashes/`), what to look for, what to do | when `lastExit` is not `exit 0`, the launcher gave up, or the editor hangs without the plugin answering |
| `failures` | the failure table (what you see, what it means, what to do), what an editor does when it is started before a successful compile, and what a compile meets while the editor is running (Live Coding, the loaded binaries) | when one of these tools returns an error, the editor does not come up or dies after a start, `unreal_status` stays `connected: false`, `logs` is empty, or a `compile` with the editor up failed on something other than your code |

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
| `unresponsive` | present, as `true`, only when the service's control loop did not answer within 10 s. Every other field is then unknown: `running`, `pid`, `desired` and `restarts` are sent as `false` / `0` / `false` / `0`, so do not read them |
| `busy` | present, as `"pre-start"`, only next to `unresponsive`: the loop is running the pre-start step (the crash cleanup of `unreal.cleanOnRestart`; see Automatic restarts). A slow pre-start, not a hung loop: call `status` again |
| `waiting` | present, as `"crash-reporter"`, while the editor's next start is held until the crash reporter of its last, failed run has finished (see Automatic restarts). The loop answers: `running: false`, `desired: true`. That start comes on its own, within 30 s at most: do not call `ue_start` for it |

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
start. It does that work itself, including the pre-start step below. Wait and
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
- **Held for a crash reporter**: after a failed run whose crash reporter is
  still running, the start waits for it (see Automatic restarts). The call
  says so and the `ue` entry shows `waiting: "crash-reporter"`; that is not an
  error, and the start comes on its own.
- **Returns at spawn time.** After that, the editor still needs anything from
  seconds to many minutes to load, depending on the project and the state of
  its shader cache.

**A start that spawns no process is an error result**:
`Error: service "ue" did not start: <lastError>`, with the `ue` entry as JSON
on the next line. The fields of that entry say whether a retry comes:

| Result | Meaning |
|---|---|
| `running: true`, no error | a process was spawned. It can still die while starting up |
| error, `desired: false` | the executable does not exist (a wrong `unreal.editor`, or an engine root from `UE_ROOT` or `engineRoot` that holds no editor). There will be no retry |
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
editor's last lines are in `logs`. Like `ue_start` and `ue_restart`, it waits
10 s at most in all. Past that it returns an error instead of this result;
section `failures` says which of those errors may be retried.

```json
{"name":"ue","running":false,"pid":0,"restarts":0,"desired":false}
```

## ue_restart

One step that does `ue_stop` and then `ue_start`: the same kill, then the same
spawn-time report, as long as the kill, the pre-start step (see Automatic
restarts) and the spawn take 10 s at most together. The step can take longer.
The call then returns
`Error: service "ue" took the request and is still carrying it out after 10s …`
while the restart goes on. Do not call `ue_restart` again; poll `status`
(section `failures`). While an automatic restart is pending, it takes that
restart's place as `ue_start` does and spares the crash evidence; a separate
`ue_stop` first would cancel the restart and not spare it (see Automatic
restarts). It also works as a plain start when the editor is not running, and
it resets `restarts` to 0 and removes `lastExit`. All the `ue_start` caveats
apply. In particular, a new editor that never came up is the same
`Error: service "ue" did not start: …` result.

## What a stop does to the process tree

On Windows, `ue_stop`, `ue_restart` and the launcher's own exit terminate the
editor's whole process tree at once: the editor, its ShaderCompileWorkers, a
crash reporter it started, and any tool it launched. Section `process-tree`
has the mechanics, the one exception (after a failed exit, a crash reporter
and whatever else that run left behind can outlive a stop by up to 30 s; not
yet verified on Windows) and the cases in which children survive: load it
when a process outlived a stop, a file stays locked after one, or the
launcher log holds a `job object` or `job assign` WARN. What follows from a
stop:

- **Unsaved changes are lost.** Save through the in-editor plugin before you
  stop or restart. These tools cannot save.
- **Do not count on recovering them later.** The editor runs with
  `-unattended`, on by default. With it, the start after a crash or a kill
  does not offer to restore packages: no prompt, and nothing is restored
  (UE 5.8.4 source; seen on Windows with UE 5.7.4 and 5.8.4). Only when the
  user turned `unreal.unattended` off does a Restore Packages dialog wait on
  the host after an unclean exit. The editor is then running, but the
  in-editor plugin stays unreachable until a human answers. Other dialogs
  are not ruled out: each one decides for itself whether it honors
  `-unattended`.
- **Work in flight inside the editor is cut off mid-way**: shader compiles,
  saves, imports, anything it was running.
- **`run_command` builds are not affected.** Each one runs in its own job, so
  `ue_stop` and `ue_restart` leave it alone. All of them die when the launcher
  exits.

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
start, and by a second rule on 3 failed runs in a row, each ending within
5 minutes of its start. `status` then shows `running: false, desired: true`
with the last `lastExit` (or `lastError`, when the spawn itself failed). An
editor that runs longer than 5 minutes before each crash is restarted without
end, and `restarts` keeps climbing. `ue_stop` breaks any loop, and `ue_start`
tries again with the count reset. Load section `restarts` when the editor
failed and does not come back, or when `restarts` keeps climbing: it has both
rules in full, the launcher-log lines that name them and the dump folder, and
what resets the count.

Before every start (the first one, a manual one, or an automatic one), the
launcher runs one pre-start step, the cleanup of `unreal.cleanOnRestart`
(default off).

**With `unreal.cleanOnRestart` on, a start that does not follow a crash
empties `Saved/Crashes/`.** To keep a crash's dumps past the next start,
leave the restart to the launcher, or call `ue_start` while `status` shows
`running: false, desired: true`. Do not call `ue_stop` before that
`ue_start`: a `ue_stop` during the restart delay cancels the pending restart,
and the `ue_start` after it is a plain manual start and does not spare the
evidence. After a give-up (no restart coming), the first start spares it
even with a `ue_stop` in between. Load section `restarts` before you start or
restart an editor after a crash whose dumps you still need: it lists which
starts count as following a crash.

After a failed run whose crash reporter is still running, the start (the
restart's, or one you ask for) is held until that reporter has finished, 30 s
at most. `status` shows `waiting: "crash-reporter"`, and `ue_start` and
`ue_restart` answer that the start is held, which is not an error: it comes on
its own. A restart then follows at the later of `restartDelaySeconds` and the
end of the reporter, not after their sum. A `ue_stop` there cancels the held
start. (Windows only; not yet verified on Windows.)

## logs

- `service` (string, default `"ue"`): `ue`, or `agent` when the launcher runs
  its own agent. Any other name returns `Error: unknown service "<name>"`.
  `run_command` commands are not services. Their output exists only in the
  result of the call that ran them.
- `lines` (integer, default 50). `0`, a negative integer, or more lines than
  are buffered returns the whole buffer.

The result is the lines joined by newlines: no header, no timestamps. An empty
result means nothing is buffered. A reply holds at most 48 KiB. Load section
`logs` when the result starts with an `[... omitted` marker (lines the buffer
dropped, or bytes cut from the reply: no `lines` value brings either back) or
when `lines` is rejected.

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
its log to `unreagent.log` next to the `.uproject`. Without a foreground agent
(an external agent, as with `-no-agent`, or headless), the log goes to the
launcher's console, which no tool can read. Each line starts with a `HH:MM:SS`
timestamp, with no date.

**Load section `logs` before you read `unreagent.log` or `unreagent.log.1`.**
The file is rotated by each launcher run that has a foreground agent, so an
`unreagent.log` on disk can be an earlier run's, and `unreagent.log.1` is not
always the run right before this one. The section says how to tell, lists the
launcher's lines to look for, and shows how to read the end of a long log,
where a crash is, with `read_file`.

## The edit loop

| You changed | Before it takes effect |
|---|---|
| assets, levels, Blueprints, through the in-editor plugin | nothing. The change is already live |
| C++ source, `*.Build.cs`, `*.Target.cs` | stop, compile, start |
| `Config/*.ini`, `.uproject`, `.uplugin` | a restart, because most settings are read at startup. A C++ plugin newly enabled also needs a compile |

For C++ (skill `ue-cpp` has the code side: reflection rules, compile errors,
how to check a new class):

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
to load the new modules. **A `compile` with the editor up can fail on
something other than your code**: stop the editor and compile again. Load
section `failures`, "A compile while the editor is running", when that
happened, or before a compile against an editor the user started by hand or
with changed arguments: it has what Live Coding and the loaded binaries do to
the build.

If the editor was started before the compile succeeded: under the default
arguments it does not wait at a dialog. It exits with code 1 and names the
missing module in its log, the launcher restarts it by its restart policy, and
each start ends the same way: `ue_stop`, compile, `ue_start`. Section
`failures`, "A start before the compile succeeded", has what was seen on
Windows and the one configuration in which the editor waits at a box for a
human instead.

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

## When something fails

Load section `failures` when one of these tools returns an error, when the
editor does not come up or dies after a start, when `unreal_status` stays
`connected: false`, or when `logs` is empty: its table goes from what you see
to what it means and what to do. Two of its rows, so that they are not
missed:

- `Error: service "ue" took the request and is still carrying it out after 10s`:
  the call is still under way. It has not failed. Do not repeat it; poll
  `status`.
- `Error: service "ue" is not responding: it did not take the request within 10s; nothing was done`:
  wait, call `status`, then retry.

## See also

- `unreagent-build`: `run_command`, compile and package, and how to read a
  build result.
- `unreagent-files`: `read_file` for logs and config on disk.
- `unreagent-runtimes`: `run_python` / `run_node`, for searching large log
  files on the host.
