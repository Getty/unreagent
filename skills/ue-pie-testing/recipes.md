# PIE test recipes

End-to-end loops with the exact calls. Every operation below is
`gameplay_debug` with that `operation` unless another tool is named. The
details are in the other sections: `sequences` (step format, manifest),
`input` (injection, monitor), `log` (filters, cursor), `console` (commands),
`failures`.

Poll seldom: every poll is a call on the game thread, and the game does not
advance while one runs (skill, "Must not get wrong").

## 1. Scripted test with assertions (preferred)

1. **Clear the ground.** `sequence_status`: if `state` is `waiting_for_pie`,
   `running` or the empty string, a run is in progress (perhaps another
   agent's); wait for it or ask. `pie_status`: if `running`, `stop_pie`, then
   `pie_status` until `running` is `false`.
2. **Write down the claim** the test checks, in observable terms: "after
   holding `IA_Move` forward for 1 s the pawn is at least 200 cm further along
   +X", "after `IA_Jump` no `Error` line in `LogTemp`".
3. **Build the steps.** A `capture` at delay 0 for the "before", the input
   (`hold` of 100 ms or more for a digital action), a `capture` after it, and
   `console` steps if the picture needs `stat` overlays. Remember that
   `delay_ms` counts from the previous step's start.
4. **Start it**: `run_sequence` with `steps`, a fresh `name` and an absolute
   `output_dir` inside the project, `<project>/Saved/pie-debug/<name>`. Keep
   `estimated_duration_ms` and `output_dir`.
5. **Wait**: PIE's start is not in the estimate. Then `sequence_status` until
   `state` is `completed` or `failed`. On `failed`, read `error` and go to
   section `failures`.
6. **Assert on the manifest**: `read_file` `<output_dir>/manifest.json`.
   - `status` is `completed`, `error` is `null`.
   - `steps_executed` equals `total_steps`. A step that could not act (no
     player controller, no Input Action) is still counted, so check the log
     too (step 7).
   - every `captures[].file` exists (`list_dir` on the output directory):
     a failed file write still lands in the manifest.
   - `diag` holds each event with game time, frame and the pawn's `pos` and
     `rot`: compare `pos` at `settle_complete` with `pos` at `hold_end:…` or
     `sequence_complete` for movement claims.
7. **Assert on the log** (section `log`, "Lines a run writes"):
   - `get_output_log` with `filter: "PIESequenceRunner"` and
     `min_verbosity: "Warning"`: any line here is a skipped step, a failed
     capture, a Take Recorder problem or the failure itself.
   - your game's own lines: `categories: ["LogTemp"]` (or the game's
     category) with `filter` on the text you expect.
   - for lines that belong to this run only, see "Only this run's lines".
8. **Look** at the capture files with an image reader of your client
   (skill `ue-viewport`, "Getting the picture"). Judge them against step 2.
9. **Report** pass or fail per claim, with the manifest values and log lines
   that show it. A skipped check is not a pass.

`run_sequence` stops PIE itself. After a failed run, check `pie_status`, and
whether the editor is still capped (`t.MaxFPS 0`, section `sequences`,
"How a run ends").

## 2. By hand: start, wait, inject, look, stop

1. `start_pie`, then `pie_status` until **`running` is `true` and
   `pawn_location` is present**. `running` alone means the session exists;
   `pawn_location` appears only once the first player controls a pawn, which
   is what input needs.
2. Note `pawn_location` as the baseline.
3. Optional: `start_monitor` (no `log_axes`) and check `pie_attached`, to see
   in the log whether the input arrives (recipe 3).
4. `start_continuous` with `action_path` and every axis value
   (the plugin's `debug.md` uses `value_x: 0, value_y: 1` for forward on its
   2D move action; your project's mapping decides). Keep the
   `injection_id`.
5. `pie_status` again: `pawn_location` should change. `capture_pie` when you
   need the picture; it arrives as base64 (skill `ue-viewport`).
6. `stop_continuous` with the id, `stop_monitor` if started, `stop_pie`,
   then `pie_status` until `running` is `false`.

Single presses: `inject_input` is one frame, which a digital action with a
`Pressed` trigger may not register (the plugin's notes). Prefer
`start_continuous` and stop it on the next call, or a `hold` step in a run.

## 3. Input does not reach the pawn

Work down until one step fails:

1. **The call's own error.** `Enhanced Input subsystem not available in PIE`
   means the first player has no Enhanced Input subsystem; `Failed to load
   InputAction` a wrong path (`/Game/Input/IA_Jump`, or the object path).
2. **A pawn.** `pie_status` without `pawn_location`: nothing is possessed
   yet, or the game mode spawns no pawn (skill `ue-game-framework`).
3. **The binding.** `start_monitor`, then `get_output_log` with
   `filter: "[PIE-DBG]"`: `ATTACHED actions=<n>` and one
   `ACTION_DISCOVERED <action>` line per action the pawn binds. Your action
   is not listed: the pawn's input setup does not bind it (skill
   `ue-enhanced-input`). No `ATTACHED` at all: the pawn has no Enhanced Input
   component (`pie_attached` stays `false`).
4. **The value.** With the monitor running, inject again and read the log:
   `INPUT <action> triggered` (digital) or `started val=…` (axis) means the
   value reached the player's input. The monitor reads the action's value
   from the player's Enhanced Input state; that injected values show there is
   the engine's behaviour and unverified. If it shows and the game still does
   nothing, the fault is in the game's handler.
5. **No line**: injection bypasses the mapping contexts, so an applied
   context is not the cause (section `input`). The action asset's own
   triggers and modifiers still apply: check them, and use a `hold` of
   100 ms or more for digital actions. Then look at the binding (the pawn or
   controller binds that action) and the game logic behind it.
6. **Slow game**: the editor window not in front runs PIE at about 3 fps
   (plugin notes); everything happens, only late.

## 4. Watching the user play

1. `start_monitor` with `start_pie: true`. The user plays.
2. `stop_monitor`, then `get_output_log` with `filter: "[PIE-DBG]"` and
   `lines` up to 1000. `STATE` lines give position, rotation, velocity and
   the montage every `interval_ms`; `INPUT` lines give each press and release.

## 5. A performance or rendering glance in PIE

`run_sequence` with a settle, a `console` step `stat unit` (or `stat fps`),
a `capture` step about a second later, and nothing else. Read the times off
the captured picture. The run caps the frame rate at 60 (`t.MaxFPS 60`), so
the frame rate shown cannot exceed it. `run_console_command` with `stat unit`
targets the editor's world instead (section `console`).

## Only this run's lines

`since` loses the first new line of each incremental read (section `log`),
so do not build assertions on it. A run brackets its lines with two of its
own, both naming the output directory:

- start: `PIESequenceRunner: run_sequence started with <n> steps, settle=<ms>ms, output=<dir>`
- end: `PIESequenceRunner: Wrote manifest to <dir>/manifest.json` (not after
  a cancel by `stop_pie`)

With a fresh `name` the directory is unique, so a plain `get_output_log` with
`filter` set to the run's name finds both. For everything in between, read
the log file itself: `read_file` with a negative `offset` on
`Saved/Logs/<ProjectName>.log`, or a `run_python` script that cuts the file
between the two anchors and greps it (skills `unreagent-files`,
`unreagent-runtimes`).
