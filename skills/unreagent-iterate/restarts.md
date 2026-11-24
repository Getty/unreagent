# Automatic restarts: giving up, and the pre-start step

The main skill, "Automatic restarts", has the restart policy, the delay and
the hold for a crash reporter. This section has when the launcher gives up
and what the cleanup before a start keeps and deletes.

## When the launcher gives up

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
A second rule covers an editor that fails the same way on every run but
takes longer than 10 s to do so (a plugin module that was never compiled, about
80 s per run): 3 failed runs in a row, each ending within 5 minutes of its
start, give up with
`[ue] failed 3 times in a row, each within 5m0s of its start — giving up (no automatic restart)`.
The cause is the same on every run: read the log (`logs`, the editor's log
file), fix it (for an unbuilt module, compile it, which the follow-up line
names), then `ue_start`, which begins a fresh count. A run of 5 minutes or
more, an exit 0, `ue_start` and `ue_restart` reset the count. An editor that
runs longer than 5 minutes before each crash is restarted without end, and
`restarts` keeps climbing. A `maxRestarts` above 0 stops after that many
restarts instead, however long each run lasted. `ue_stop` breaks any loop,
and `ue_start` tries again with the count reset. The output of the last
attempts stays in `logs`.

## The pre-start step

Before every start (the first one, a manual one, or an automatic one), the
launcher runs one pre-start step, the cleanup of `unreal.cleanOnRestart`. It
spares the evidence of a crash on a start that follows one:

- An automatic restart.
- A `ue_start` or `ue_restart` while an automatic restart is still pending,
  within `restartDelaySeconds` of an exit the policy restarts (a crash, and
  under `always` a clean exit too) or of a failed spawn. It starts the editor
  in place of that restart (or of a restart held for the crash reporter:
  the main skill, "Automatic restarts") and spares the evidence the same way.
- The first `ue_start` or `ue_restart` after the editor failed and was not
  restarted (given up on, `maxRestarts` used up, policy `never`).

A `ue_stop` during the restart delay cancels the pending restart. The
`ue_start` after it is a plain manual start and does not spare the evidence.

The launcher kills no crash reporter before a start. `unreal.killCrashReporter`
is still accepted but has no effect and logs a startup WARN: remove it. The old
kill ended the crash monitor of every editor on the machine, not only this
editor's.

The step:

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
