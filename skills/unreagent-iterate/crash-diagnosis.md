# Diagnosing an editor that crashed or will not stay up

The main skill has the tools; section `restarts` when the launcher gives up,
section `logs` the launcher log and `logs`' limits. This section is the order
in which to read the evidence. The engine facts come from the launcher's
measured record (UE 5.7.4 and 5.8.4 on Windows, and the UE 5.8.4 source).

## 1. What `lastExit` says

| `lastExit` | Means |
|---|---|
| `exit 0` | a normal quit, or an editor whose init failed early and returned 0 (UE source): exit 0 does not prove a clean run; `restart: on-failure` does not restart it |
| `exit 1` | a startup failure, such as a module without a binary (`Incompatible or missing module`); also what a forced kill from outside gives |
| `exit 3` | a fatal error or crash: an assert (`check`), an access violation the engine caught, `debug crash` |
| `exit 3221225477` | `0xC0000005`, an access violation the engine did not turn into exit 3. Measured for an unbuilt *plugin* module: the editor shut down, then hung about 60 s and ended this way |
| `exit 777003`, `777004`, `777006` | the crash reporter crashed, the crash handler crashed, a crash during static initialization (UE source; never observed here) |
| `exit 3221225786` | `0xC000013A`, a console control event (Ctrl+C or a closed console) |
| absent, `lastError` set | no process was spawned: the editor path or its permissions |

How long the run lasted helps, but it does not decide: a project module
without a binary ended each run after 9-17 s, an unbuilt plugin module after
about 80 s (measured), and the launcher counts any failed run within 5
minutes toward its give-up (section `restarts`). Read the log before you
conclude; exit 3 after the editor was up and working points at code running
in it.

## 2. Read, in this order

1. **`logs` `{"lines": 0}`** for the editor's own output, the last 500
   lines. After restarts, the failed run's last lines are followed by the
   next run's start; read from the bottom up to the last start, then above
   it.
2. **The editor's log file**, `Saved/Logs/<ProjectName>.log`: the whole run,
   not 500 lines. Read its end: `read_file` `{"path":
   "Saved/Logs/<ProjectName>.log", "offset": -48128}` (skill
   `unreagent-files`). It starts with a byte-order mark. Each start copies
   the previous file to a timestamped backup in `Saved/Logs/` first, so the
   run before a restart is in the newest backup, not in
   `<ProjectName>.log`.
3. **The launcher log** (`unreagent.log`, window mode only): the
   `[ue] exited (…)`, restart and give-up lines with their times, the
   startup WARNs (unbuilt modules, a plugin's `EngineVersion`, the plugin's
   readiness).
4. **`Saved/Crashes/`**: one folder per report (`list_dir`, then
   `read_file`). A report is also written for an ensure, which does not end
   the editor, so a folder there does not prove a crash. With
   `unreal.cleanOnRestart` on, a start that does not follow a crash empties
   the folder: read it before you start the editor by hand (section
   `restarts`).

## 3. What to look for

- **The first error, not the last.** A crash ends with the engine's crash
  lines; the cause is usually earlier: the first `Error:` line of the run, a
  failed `check` or `ensure` with its file and line, a module that failed to
  load.
- **Your own code.** Names of your module and classes in the call stack
  lines, and your log category's last lines before the end (skill `ue-cpp`).
- **Startup failures** name a module, a plugin or the project file. A
  plugin refused for its `EngineVersion` is skipped without a dialog under
  `-unattended`: the editor runs, but the plugin's port never opens (the
  launcher warns at its start).
- **A hang, not a crash.** `running: true` for minutes with `unreal_status`
  `connected: false`: the game thread may wait at a dialog (with
  `unreal.unattended` off) or in a long load. The launcher log's readiness
  WARN (`port open, but the toolkit did not answer /mcp/status within 5m0s`)
  says the plugin's port is open and the game thread does not answer.

## 4. Act

| Found | Do |
|---|---|
| a module without a binary | `ue_stop`, `run_command` `compile`, `ue_start` (skill `unreagent-build`) |
| a crash in your C++ | fix it, then the C++ loop (skill `ue-cpp`) |
| a crash in an asset or Blueprint on load | the editor cannot open it to fix it; name the asset to the user, who may move it aside |
| a plugin refused or failing | the launcher's WARN names the fix; the user changes the `.uplugin` or the plugin |
| nothing conclusive | report what you read and where, with the lines; do not restart in a loop |

After a fix, `ue_start` once and check `status` some seconds later; a give-up
is reset by that start.
