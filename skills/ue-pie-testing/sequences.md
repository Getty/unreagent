# gameplay_debug: scripted runs

`run_sequence` is the plugin's preferred way to test gameplay: one call starts
PIE, plays timed steps, writes captures and a manifest to disk, and stops PIE.
The call returns as soon as the run is set up. The run itself goes on inside
the editor, and you follow it with `sequence_status`.

## What a run does

1. **Checks everything first.** Every step is parsed and every Input Action is
   loaded before PIE starts. Any error ends the call, and nothing runs.
2. **Starts PIE.** If a session is already running, it asks the editor to end
   it, waits half a second, then asks for a new one. Prefer to stop PIE
   yourself first ("Pitfalls").
3. **Waits for PIE**, checking every 0.1 s. This wait has no time limit.
4. **Settles.** Once PIE runs, it brings the editor window to the front, runs
   `t.MaxFPS 60` in the PIE world, starts the Take Recorder if asked, and
   waits `settle_ms` of game time.
5. **Plays the steps.** Auto-capture starts, every step is scheduled, and the
   `max_duration_ms` safety timer is armed.
6. **Finishes.** After the last step, and once the last `hold` or tape has run
   out, it stops the captures, the holds, the tapes and the Take Recorder,
   runs `t.MaxFPS 0`, writes `manifest.json` and asks the editor to end PIE.

## Parameters

| Parameter | Default | Notes |
|---|---|---|
| `steps` | — | the step list, below. Required unless `sequence_file` is given |
| `sequence_file` | — | a JSON file with the steps, below |
| `name` | `sequence` | the run's name, used in the default `output_dir` |
| `output_dir` | `<user temp dir>/pie-debug/<name>` | where captures and the manifest go. Created if missing |
| `settle_ms` | 500 | game time to wait after PIE starts. 0 or less skips the wait |
| `auto_capture_every_n_frames` | 0 (off) | capture every N frames during the steps ("Captures") |
| `max_duration_ms` | 120000 | safety limit, counted from the end of the settle. 0 turns it off |
| `take_record` | false | record the run with the Take Recorder ("Captures") |
| `take_slate` | — | the take's slate name, with `take_record` |

**Give an absolute `output_dir` inside the project**, such as
`<project>/Saved/pie-debug/<name>`. The default is in the user's temp
directory, outside the project, where `read_file` and `list_dir` cannot reach
(skill `unreagent-files`). How a relative path resolves is up to the engine
and has not been verified. The directory is not emptied: a second run with
the same name overwrites the manifest and same-named captures and leaves
everything else. Use a fresh `name` per run.

**`sequence_file`** holds either a step array or an object with a `steps`
array. Give an absolute path; it is read by the editor. When the steps come
from the file, its top-level `name`, `settle_ms`, `output_dir`,
`auto_capture_every_n_frames` and `max_duration_ms` fill in what the call left
out. `take_record` is not read from the file. When the call has its own
non-empty `steps`, the file is still read and parsed, but nothing in it is
used.

## Steps

Every step is an object with a `type` and an optional `delay_ms` (default 0).
The type names are case sensitive.

| `type` | Fields | Does |
|---|---|---|
| `input` | `action`, `value_x` (default 1), `value_y`, `value_z` (default 0) | injects the action's value for one frame |
| `hold` | the same, and `duration_ms` | injects the value every 1/60 s for `duration_ms`. 0 holds it until the run ends |
| `input_tape` | `action`, `values` | injects one value per engine frame, in order |
| `capture` | `name` (default `capture_<index>`) | writes `<output_dir>/<name>.jpg` |
| `console` | `command` | runs a console command in the PIE world |

**`delay_ms` counts from the previous step's start**, not from the end of the
settle, and not from the previous step's end. To run two holds back to back,
give the second the first one's `duration_ms` as its `delay_ms`. Steps whose
delays add up to 0 run at once, at the end of the settle.

**`action`** is the Input Action's path, `/Game/Input/IA_Jump`; the object
name is added when the path has no dot. The operations outside a run call the
same parameter `action_path`.

**Digital actions always get `true`** in `input` and `hold` steps: their
`value_x` is ignored. These steps stop pressing an action only where no step
injects it, so between two presses leave a gap: make the next press's
`delay_ms` longer than the previous hold's `duration_ms`. Whether the game
sees that gap as a release, and how long it must be, is the engine's
behaviour and has not been verified here. An `input_tape` sends `false`
explicitly on its `0` frames. The plugin's notes suggest a zero-value
hold instead, which in this code presses the action again. They also say a
digital action needs a `hold` of 100 ms or more, because a single-frame
`input` does not give its `Pressed` trigger the change it needs. Neither has
been verified here.

**`input_tape`** values are numbers or arrays `[x]`, `[x, y]`, `[x, y, z]`,
one per frame; missing axes are 0. A digital action gets `true` on frames
whose `x` is not 0 and `false` on the others. With the run's 60 fps cap, a value lasts 1/60 s if the editor
keeps up; at a lower frame rate the tape takes longer. While a tape plays, the
run logs a `[REPLAY-POS]` line with the pawn's position every frame.

**`capture`** reads the PIE viewport, stretches it to 1024x576 and saves a
quality-70 JPEG. The name is used as the file name as it is, so keep it to
letters, digits, `-` and `_`. A capture that fails before the JPEG exists (no
PIE viewport, no size yet, a failed read or encode) leaves no file and no
entry in the manifest. The file write itself is not checked: when it fails,
the manifest still lists the capture, so check that each listed file exists.
Either way the run goes on.

**`console`** runs in the PIE world, unlike `run_console_command`, and its
output goes only to the log. **It does not pass the blocklist of
`run_console_command`.** Keep to commands that tool would accept.

**A step that cannot act is skipped and still counted.** Without a player
controller or an Input Action, the step is logged as a warning, and the run
completes as if it had run. Check the log for `PIESequenceRunner:` warnings.

## The answer to the call

| Field | Present | Holds |
|---|---|---|
| `name`, `total_steps`, `output_dir` | always | as used |
| `estimated_duration_ms` | always | `settle_ms` plus the latest step end plus 500. It leaves out the time PIE needs to start |
| `settle_ms`, `max_duration_ms` | always | as used |
| `manifest_path`, `note` | always | where the manifest will be, and a reminder |
| `auto_capture_every_n_frames` | when above 0 | as used |
| `take_record` | when true | as used |
| `take_slate` | with `take_record`, when not empty | as used |

The message is
`run_sequence '<name>' started: <n> steps, est <ms>ms, output=<dir>`.

Errors before anything runs:

| Error | Cause |
|---|---|
| `A sequence is already running. Use sequence_status to check, or stop_pie to cancel.` | another run is waiting, settling or playing |
| `Failed to read sequence_file: <path>` | the file cannot be read |
| `Failed to parse JSON from sequence_file: <path>` | not valid JSON |
| `sequence_file must contain a JSON object or array` | a JSON value of another type |
| `sequence_file exists but contains no 'steps' array` | an object without a non-empty `steps` |
| `Missing or empty 'steps' — provide inline 'steps' array or 'sequence_file' path` | neither given |
| `Invalid step object at index <i>` | a step that is not an object |
| `Step <i> missing 'type' field` | — |
| `Step <i>: unknown type '<type>'. Valid: input, hold, capture, console, input_tape` | a misspelt or capitalised type |
| `Step <i> (<type>): missing 'action' field` | an input step without `action` |
| `Step <i> (input_tape): missing or empty 'values' array` | — |
| `Step <i> (input_tape): invalid value at index <j>` | a value that is neither a number nor an array |
| `Step <i> (console): missing 'command' field` | — |
| `Pre-validation failed: Failed to load InputAction: <path>` | no Input Action at that path |

## Following a run: sequence_status

`sequence_status` reports the last run, finished or not.

| Field | Present | Holds |
|---|---|---|
| `state` | always | `idle`, `waiting_for_pie`, `running`, `completed` or `failed` |
| `current_step`, `total_steps` | once a run exists | for `run_sequence`, the steps done so far |
| `captured_frames` | once a run exists | the `capture` steps done so far. While auto-capture runs, its frame count instead; once it stops, its frames are not counted here |
| `error` | after a failure | the reason, see below |
| `duration_sec`, `captured_files` | once `completed` or `failed` | seconds, and image paths |

The message is `Sequence <state>: step <i>/<n>, <k> frames captured`, or
`No sequence running` with `state: "idle"` before the first run. **During the
settle, `state` is an empty string**, and the message reads
`Sequence : step 0/<n>, …`. Treat it as "still starting".

Poll until `state` is `completed` or `failed`. `estimated_duration_ms` is a
lower bound, not a deadline.

## How a run ends

| End | `state` | `error` | Manifest | PIE |
|---|---|---|---|---|
| all steps done | `completed` | — | written | stopped |
| `max_duration_ms` reached | `failed` | `Safety timeout: sequence exceeded max_duration_ms=<n> (completed <k>/<n> steps)` | written | stopped |
| PIE ended by anyone else | `failed` | `PIE ended externally (step <k>/<n>)` | written | already ended |
| `stop_pie` during the run | `failed` | `Cancelled` | **not written** | stopped by `stop_pie` |
| the PIE timers were not available | `failed` | `PIE timer manager not available` or `PIE timer manager not available for settle` | written | stopped |

**`t.MaxFPS` is reset only on completion.** After any other end that came
after the settle began, the plugin leaves its cap of 60 in place. If the
editor seems capped afterwards, run `t.MaxFPS 0` with `run_console_command`,
the command the run itself uses to reset it.

## The manifest

`<output_dir>/manifest.json`:

| Field | Present | Holds |
|---|---|---|
| `name` | always | the last part of `output_dir` without any file extension, not necessarily `name` |
| `status` | always | `completed` or `failed` |
| `steps_executed`, `total_steps` | always | counts |
| `duration_s` | always | seconds from the call to the end |
| `captures` | always | one entry per `capture` step: `name`, `file`, `frame_number`, `game_time` |
| `take_sequence_path` | with a recorded take | the take's Level Sequence |
| `auto_capture_count`, `auto_capture_files` | when image files were written | file names |
| `diag` | always | the run's events: `t` (game time), `frame`, `event`, `pos`, `rot` of the player's pawn |
| `error` | always | the error, or `null` |

Without auto-capture, `auto_capture_files` lists the `capture` steps' files
as well. Use `captures` for those.

Read the manifest with `read_file`. The images are binary: open them with an
image reader of your client. During the run, the plugin also logs
`[PIE-DBG]` lines with the pawn's position, rotation and velocity at each step
and at the start and end of the run; section `log`, "Lines a run writes",
lists them, and section `recipes` turns them into assertions.

## Captures

- **`auto_capture_every_n_frames`** captures the PIE view every N frames
  through the engine's frame grabber, from the end of the settle until the run
  ends. Files are named `f<frame>_t<game time>.jpg`, the frame zero-padded to
  six digits. The frame and time are those of the moment the plugin got the
  image back from the grabber, which buffers a few frames, so they can be
  somewhat later than the frame shown. Encoding and writing run in the
  background; a file that failed is simply missing.
- **`take_record`** starts the Take Recorder during the settle and stops it
  when the run completes. On any other end the plugin does not stop it; what
  the Take Recorder does when PIE ends is the engine's business and has not
  been verified. It needs UE 5.7 or later. A failure, such as
  `TakeRecorder requires UE 5.7+`, does not fail the run; it shows only in
  the manifest's `diag` and in the log. Skill `ue-sequencer` has
  `take_status` and `take_stop`, to check for and stop a take that is still
  recording, and `open` and `scrub`, to look at the recorded sequence.

## Pitfalls

- **Stop PIE first.** With a session already running, `run_sequence` ends it
  and starts a new one itself. From the code, the end of the old session can be
  taken for the new run's PIE ending, failing the run at once with
  `PIE ended externally (step 0/<n>)` while a fresh session starts anyway. This
  has not been verified against an editor. `stop_pie`, then `pie_status`
  until `running` is `false`, avoids the question.
- **A run that never starts.** If PIE does not come up, the run waits for it
  forever, `max_duration_ms` is not armed yet, and `stop_pie` without a session
  does not cancel it. Every later `run_sequence` then fails with
  `A sequence is already running. …`. Starting PIE (`start_pie`) lets the run
  go on; `stop_pie` once PIE runs cancels it.
- **Unfocused editor.** The run brings the editor to the front itself. If the
  user switches away during a run, the plugin's notes say PIE slows to about
  3 fps, and holds and tapes stretch accordingly.

## execute_sequence (older)

`execute_sequence` is the plugin's earlier form, still available. It takes
`sequence`, an array of `{action, delay_ms, value_x, value_y, value_z}`
(value defaults 1, 0, 0). Every entry is a single-frame `input` step, and
`delay_ms` counts from the previous entry. It differs from `run_sequence`:

- It does not start PIE. It waits for a session, without a time limit, and
  plays when one runs. It does not stop PIE and writes no manifest.
- It captures all the time while it plays. `capture_mode` `normal` (the
  default; any unknown value counts as `normal`) reads the viewport every
  `capture_interval_ms` (default 200, at least 50). `high_speed` uses the
  frame grabber every `capture_every_n_frames` (default 3, at least 1). Files
  are `f<frame>_t<game time>.jpg` in `output_dir`, which defaults as for
  `run_sequence`.
- An entry without `action` is not refused; it is skipped at play time.

The result has `steps`, `capture_mode`, `output_dir` and the `note`
`Sequence is async. Poll with sequence_status.`; the message is
`Started sequence '<name>' with <n> steps`. Errors:
`Missing or empty 'sequence' array parameter`,
`Invalid sequence step at index <i>`, and the already-running error above.
`sequence_status` reports its progress as the index of the next entry.
