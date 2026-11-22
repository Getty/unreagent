---
name: ue-sequencer
description: >
  Open a Level Sequence in the Sequencer editor, scrub it to a frame, read
  its state and close it, and start, stop or check the Take Recorder, with the
  sequencer tool. Use before you look at a frame of a cinematic or a recorded
  take, or when a take is still recording.
toolkit_tools: [sequencer]
---

# UE Sequencer and Take Recorder: sequencer

The UE LLM Toolkit plugin's **`sequencer`** tool does two separate jobs. It
drives the **Sequencer editor** as a viewer: open a Level Sequence, move the
playhead, read the state, close. And it switches the **Take Recorder** on and
off. Through the plugin's MCP bridge it is listed as `unreal_sequencer`. It
works only while the editor is running and the plugin is reachable;
`unreagent-iterate` covers the wait for the plugin.

It does not create a sequence, add tracks or keys, play a sequence, or render
one. No toolkit tool does.

This skill was written from the toolkit's C++ source (`MCPTool_Sequencer.h`,
`MCPTool_Sequencer.cpp`, `SequencerController.cpp`, at toolkit commit
9f6a0f7). It has not been exercised against a running editor. Opening an asset
editor, evaluating a sequence and recording a take are the engine's work. The
skill says what the plugin asks the engine to do, and marks what follows from
that as unverified.

## Recipes

Argument names are exact, and **`operation` is case sensitive**.

**Look at one frame of a sequence**

1. Stop PIE if it runs (skill `ue-pie-testing`): a level-viewport capture
   takes the PIE view while a session is up.
2. `sequencer` `{"operation": "open", "sequence_path": "/Game/Cinematics/SEQ_Intro", "frame": 120, "camera_location": {"x": -400, "y": 0, "z": 200}, "camera_rotation": {"pitch": -10, "yaw": 0, "roll": 0}}`.
   Give `camera_location` and `camera_rotation` together, with all six values.
3. Check the result: `camera_set` must be `true`, and a `frame` field must be
   present. If `frame` is missing, the scrub failed without an error; send
   `scrub` yourself.
4. `sequencer` `{"operation": "get_state"}`. Confirm `sequence_path` and
   `current_frame`, and note `camera_location` and `camera_rotation`.
5. `capture_viewport` with `camera_location` and `camera_rotation` set to the
   values from step 4, all six (skill `ue-viewport`), and write the picture
   to a file as that skill describes. Without a camera the capture reads
   whatever the viewport last drew, which may be from before the scrub; with
   one it draws the viewport first (`ue-viewport`, "Freshness"). Passing the
   viewport's own values keeps the camera where it is.

**Step through frames**

1. `sequencer` `{"operation": "get_state"}` for `playback_start_frame`,
   `playback_end_frame` and `display_rate`.
2. `sequencer` `{"operation": "scrub", "frame": 48}`, then `capture_viewport`
   with an explicit camera, as in step 5 above, so that it draws the frame
   you scrubbed to. Repeat per frame.
3. `sequencer` `{"operation": "close"}` when done. It closes every open Level
   Sequence editor.

**Record gameplay as a take**

Do not use this tool. `gameplay_debug` `run_sequence` with `take_record: true`
starts PIE, adds the player's pawn as the recording source, stops the take and
writes its path to the manifest (skill `ue-pie-testing`, section `sequences`).
`take_start` here adds no source.

**A take is still recording**

`run_sequence` stops its take only when the run completes. After any other
end the plugin leaves it alone, and it may still be recording.

1. `sequencer` `{"operation": "take_status"}`. Look at `is_recording`.
2. If `true`: `sequencer` `{"operation": "take_stop"}`. Note `sequence_path`;
   an empty string means no sequence came out of it.

**Look at a recorded take**

1. Take the path from the manifest's `take_sequence_path`, from `take_stop`'s
   `sequence_path`, or from `take_status`'s `last_sequence_path`.
2. Continue as in "Look at one frame of a sequence".

## Before the first call

- **Result shape.** Success is the message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope, and the tool's own fields, the ones named in this skill, sit under
  its `data` key (skill `ue-tasks`). Failure is `Error: <message>` with no
  data.
- **No aliases, no lowercasing.** An operation in another case, or any other
  word, returns `Unknown operation '<op>'. Valid: take_start, take_stop,
  take_status, open, scrub, get_state, close`. A missing `operation` returns
  `Missing required parameter: operation`.
- **Marked as modifying, and it is.** The tool changes editor state: it opens
  and closes asset editors, moves the level viewport's camera, moves the
  playhead, and changes Take Recorder settings. It changes no asset by its own
  code, and saves none.
- **No PIE check.** No operation looks at whether PIE runs. How the Sequencer
  editor and the Take Recorder behave during a session is the engine's
  business.

## Operations

| Operation | Required | Optional | Result fields |
|---|---|---|---|
| `open` | `sequence_path` | `frame`, `camera_location`, `camera_rotation` | `sequence_path`, `camera_set`, `frame` (only when the scrub worked) |
| `scrub` | `frame` | — | `frame` |
| `get_state` | — | — | see below |
| `close` | — | — | none |
| `take_start` | — | `slate_name` | `recording`, `slate_name` (when given) |
| `take_stop` | — | — | `recording`, `sequence_path` |
| `take_status` | — | — | `is_recording`, `state`, `last_sequence_path` (when there is one) |

The tool's description and the dispatch list the same seven operations.

### Which Sequencer

`scrub` and `get_state` do not take a path. They act on "the active
Sequencer", which the code finds by walking all assets that have an editor
open and taking the **first Level Sequence** whose editor gives a Sequencer.
With one Level Sequence open, that is the one. With several, which comes first
is not defined by the toolkit. Keep one open, and confirm with `get_state`'s
`sequence_path`.

With none open, both fail with `No active Sequencer found`.

### open

Loads the Level Sequence and opens its editor, then, in this order, sets the
viewport camera and scrubs.

| Parameter | Meaning |
|---|---|
| `sequence_path` | required. A package path, `/Game/Cinematics/SEQ_Intro`, or an object path, `/Game/Cinematics/SEQ_Intro.SEQ_Intro`. A path without a dot gets the object name appended |
| `frame` | a number: the display frame to scrub to, as in `scrub` |
| `camera_location` | `{x, y, z}` |
| `camera_rotation` | `{pitch, yaw, roll}`, in degrees |

The message is `Opened sequence: <path>`, with ` at frame <n>` when the scrub
worked and ` with camera positioned` when the camera was set.

What the success does not prove:

- **The camera is set only when both camera objects are given.** With one of
  them, or with a value that is not an object, neither is used, there is no
  error, and `camera_set` is `false`.
- **`camera_set: true` means the call was made**, not that a camera moved. The
  code sets the camera of the level editor's first active viewport and ignores
  whether there was one.
- **A key left out of a camera object is not the current value.** The code
  reads all six keys with the engine's `FJsonObject::GetNumberField`, without
  a default of its own. That call cannot report a failure: a missing key
  reads as 0, and so does `null`, an array, an object or a string that is not
  numeric. `true` and `false` read as 1 and 0, and a string made of an
  optional sign, digits and at most one dot as that number
  (`FJsonObject::GetField` in
  `Engine/Source/Runtime/Json/Private/Dom/JsonObject.cpp`,
  `FJsonValue::AsNumber` in `JsonValue.cpp` beside it; read in the UE 5.8.4
  source, not run, while the toolkit targets UE 5.6 and 5.7). Pass all six.
- **A failed scrub is swallowed**, and so is a `frame` that is not a number.
  `open` still succeeds; the result just has no `frame` and the message no
  ` at frame`. When `frame` is present it is the number you sent, not a value
  read back.
- **The camera may not stay where you put it.** The scrub comes after the
  camera call. Whether evaluating the sequence, for example one with a camera
  cut track, takes the viewport camera over has not been verified. Compare
  `get_state`'s `camera_location` with what you sent.
- **Nothing is checked about the level.** The sequence is opened whatever
  level is loaded. Whether its bindings resolve there is not reported.

| Error | Cause |
|---|---|
| `Missing required parameter: sequence_path` | missing or empty |
| `Failed to load Level Sequence: <path>` | nothing at that path, or the asset is not a Level Sequence. `asset_search` (skill `ue-assets`) finds the path |
| `Failed to open editor for: <path>` | the engine did not open an editor |
| `Editor not available`, `AssetEditorSubsystem not available` | the editor is not in a state to open asset editors |

### scrub

Moves the playhead of the active Sequencer to a **display frame**: a frame
number at the sequence's display rate, as the Sequencer's timeline shows it.
The code converts it to the sequence's tick resolution and sets the time
directly.

- `frame` is required and must be a number: `Missing required parameter:
  frame` otherwise. A fraction is cut off towards zero (`12.9` becomes 12).
- **The frame is not checked against the playback range.** A frame before the
  start or after the end is sent to the Sequencer as it is; what the Sequencer
  does with it has not been verified. Take the range from `get_state`.
- The result's `frame` is the truncated number you sent. To see where the
  playhead is, call `get_state`.
- A success says the time was set. It does not say that the viewport has drawn
  that frame; skill `ue-viewport`, "Freshness", covers what a capture reads.

The message is `Scrubbed to frame <n>`.

### get_state

Read-only. The message is `Sequencer state retrieved`.

| Field | Present | Holds |
|---|---|---|
| `current_frame` | always | the playhead as a display frame, rounded down |
| `display_rate` | always | a string, numerator over denominator, such as `30/1` |
| `playback_speed` | always | the Sequencer's playback speed |
| `playback_start_frame`, `playback_end_frame` | when the playback range has both bounds | the range's lower and upper bound in display frames, rounded down |
| `sequence_name`, `sequence_path` | when a sequence is focused | the focused sequence's name and object path (`/Game/…/SEQ_Intro.SEQ_Intro`) |
| `camera_location`, `camera_rotation` | when the level editor has an active viewport | that viewport's camera, `{x, y, z}` and `{pitch, yaw, roll}` |

- `playback_end_frame` is the range's upper bound as the engine stores it.
  Whether that frame itself is still inside the range has not been verified;
  do not rely on a picture taken exactly there.
- The camera is the level viewport's, whether or not the Sequencer controls
  it. The fields say nothing about a camera actor in the sequence.
- "Focused" is the engine's term; with a sub-sequence open inside the editor
  it may be that sub-sequence, not the asset you opened. Not verified.

### close

Closes the editors of **every** open Level Sequence. The header says "Close
the active Sequencer"; the code closes all of them, and that is what this
skill documents. The message is `Sequencer closed`, with no data.

- With none open it fails: `No open Level Sequence editors found`. Treat that
  as "already closed".
- The code does not save the sequence and does not look at whether it has
  unsaved changes. This tool itself makes none, but the user may have. What
  the engine does on closing a dirty sequence has not been verified.
- The viewport camera stays where it was put.

### take_start

Starts the Take Recorder. Before it starts, the code:

1. fails with `Take Recorder is already recording` if a take runs;
2. **clears the Take Recorder's sources**, including any the user set up in
   the Take Recorder panel;
3. sets the slate name, when `slate_name` is given and not empty;
4. sets two of the user's Take Recorder settings: save recorded assets on, and
   the countdown to 0 seconds;
5. asks the subsystem to start recording.

It adds **no source**. The result is `recording: true` plus `slate_name` when
given, with the message `Take Recorder started`.

What the success does not prove:

- **That anything is being recorded.** With the sources cleared and none
  added, what a take contains is the engine's business and has not been
  verified. The tool has no operation to add a source. To record the player's
  pawn in PIE, use `run_sequence` with `take_record` (skill `ue-pie-testing`).
- **That the recorder reached the recording state.** `recording: true` is
  written by the tool once the start call returned true; it is not read from
  the recorder. Call `take_status` and look for `state: "started"`.
- The two settings stay changed after the take. Whether the engine writes
  them to the user's config has not been verified. Tell the user if they use
  the Take Recorder themselves.

`Failed to start Take Recorder` means the subsystem refused; the code gives no
reason.

### take_stop

Stops the running take and returns `recording: false` and `sequence_path`,
the package path of the recorded Level Sequence. The message is
`Take Recorder stopped. Sequence: <path>`.

- With no take running: `Take Recorder is not recording`.
- **An empty `sequence_path` is still a success.** The code asks for the last
  recorded sequence, falls back to the sequence that was being recorded, and
  returns `""` when it has neither (the log then has `Take recording stopped
  but no sequence was produced`).
- **A path is not a saved asset.** This operation does not save the sequence
  itself; it relies on the save-recorded-assets setting that a start through
  the toolkit switches on. `run_sequence` saves its take explicitly. Whether
  the stop has finished writing by the time the call returns has not been
  verified. Confirm the asset with `asset_search` (skill `ue-assets`) before
  you build on it.
- It stops whatever take is running, also one started by `run_sequence` or by
  the user.

### take_status

Read-only. Returns `is_recording`, `state` and, when the recorder has a last
recorded sequence, `last_sequence_path`. The message is
`Take Recorder state: <state> (recording: true|false)`.

`state` is one of `pre_initialization`, `counting_down`, `pre_record`,
`ticking_after_pre`, `started`, `stopped`, `cancelled`, or `unknown`.

`last_sequence_path` is an **object path** (`/Game/…/Take.Take`), while
`take_stop` returns the package path. `open` accepts both.

### Engine version

The Take Recorder code is compiled only for **UE 5.7 or later**. On an older
engine `take_start` and `take_stop` return `TakeRecorder requires UE 5.7+`,
and `take_status` returns `Failed to get Take Recorder status`. `open`,
`scrub`, `get_state` and `close` are not version-gated.

`take_status` returns the same `Failed to get Take Recorder status` when the
Take Recorder subsystem is missing. `take_start` and `take_stop` say
`TakeRecorderSubsystem not available` in that case.

## sequencer or run_sequence

Both reach the Take Recorder. They do different jobs:

| Job | Use |
|---|---|
| record the player's pawn during a scripted PIE run, with the take saved and its path in a manifest | `gameplay_debug` `run_sequence` with `take_record: true` (skill `ue-pie-testing`, section `sequences`) |
| check whether a take runs, or which sequence was recorded last | `sequencer` `take_status` |
| stop a take that a failed or cancelled run left recording | `sequencer` `take_stop` |
| start a take by hand | `sequencer` `take_start`, knowing it records with no source added |
| look at a recorded take, frame by frame | `sequencer` `open`, `scrub`, then `capture_viewport` |

**Do not start a `run_sequence` with `take_record` while a take runs.** Its
own start then fails with `Take Recorder is already recording`, which shows
only in the manifest's `diag` (`take_recorder_start_failed: …`) and in the
log, and the run goes on. When the run completes it stops the Take Recorder
all the same, so it ends the other take and writes that take's path into its
manifest as `take_sequence_path`. Check `take_status` first, and `take_stop`
what is running.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Unknown operation 'Open'. Valid: …` | the operation is case sensitive | lowercase it |
| `open` succeeded, `camera_set` is `false` | only one camera object was given, or one was not an object | pass both, with all six values |
| `open` succeeded, no `frame` in the result | the scrub after the open failed silently | `scrub`, and read its error |
| `No active Sequencer found` | no Level Sequence editor is open, or the open one gave no Sequencer | `open` first, then call again |
| the capture shows the game, not the sequence | PIE runs, and `capture_viewport` prefers it | stop PIE (skill `ue-pie-testing`) |
| the capture is not from where the camera was put | the sequence or a later call moved the viewport camera | compare `get_state`'s camera fields; `ue-viewport` |
| `scrub` and `get_state` act on another sequence | several Level Sequence editors are open | `close`, then `open` the one you mean |
| `take_stop` succeeded with `sequence_path: ""` | the recorder produced no sequence | check `take_status`; record through `run_sequence` |
| `take_start` succeeded and the take is empty | no source was added | record through `run_sequence` with `take_record` |
| `Take Recorder is already recording` | a take runs, perhaps one a failed run left | `take_status`, `take_stop` |
| `TakeRecorder requires UE 5.7+` | the plugin was built for an older engine | the take operations are not available there |
| `Error: Task timed out after …ms (task_id: …)` | the bridge stopped waiting | skill `ue-tasks`; `get_state` or `take_status` before you retry |

## Background in the plugin

The plugin ships no context or domain document on the Sequencer or the Take
Recorder. Skill `ue-context` says where its other documents are.

## See also

- `ue-viewport`: `capture_viewport`, for the picture after `open` or `scrub`.
- `ue-pie-testing`: `run_sequence` with `take_record`, section `sequences`.
- `ue-assets`: `asset_search`, for the path of a Level Sequence.
- `ue-tasks`: the task envelope and bridge timeouts.
- `unreagent-iterate`: the wait for the plugin, and the editor log.
