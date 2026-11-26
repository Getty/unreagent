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

**It does not create a sequence, add cameras, tracks, cuts or keys, play a
sequence, or render one. No toolkit tool does.** Section `camera-cuts` has
the decision rule and a labelled Python path for that.

This skill was written from the toolkit's C++ source (`MCPTool_Sequencer.h`,
`MCPTool_Sequencer.cpp`, `SequencerController.cpp`, at toolkit commit
c1b6709). It has not been exercised against a running editor. Opening an
asset editor, evaluating a sequence and recording a take are the engine's
work; facts marked "UE 5.8.4 source" were read there, not run, and the rest
of the engine's behaviour is marked unverified.

## Sections — load the one your task needs

| Section | Holds | Load it |
|---|---|---|
| `camera-cuts` | camera cut track vs cinematic shot track; what `create_camera` does to the viewport; a short, unexercised Python path to a sequence with two cuts; the loop that verifies each cut frame by frame | before you build or change a sequence's cameras or cuts, or check that its cuts fall where they should |
| `operations` | every operation in detail: `open`'s parameters and what its success does not prove, `scrub`, `get_state`'s fields, `close`, `take_stop`, `take_status`, the engine version gate | before an operation the recipes below do not show, or when a result field is not what you expect |
| `open-values` | how `open` and `scrub` read the camera keys and `frame` from each JSON type | when the camera or the playhead is not where you sent it |
| `take-start` | `take_start` in full: the steps the code takes, the result, what its success does not prove | before you call `take_start` |
| `failures` | `open`'s errors, and the failure table (what you see, what it means, what to do) | when a call returns an error, or a capture or a take is not what you expected |

## Must not get wrong

- **`operation` is case sensitive**, with no aliases.
- **`playback_end_frame` is one past the last frame**: the engine's playback
  range has an exclusive upper bound (`UMovieScene::SetPlaybackRange`, UE
  5.8.4 source). Scrub to `playback_end_frame - 1` for the last frame.
- **`scrub` and `get_state` act on the first open Level Sequence editor.**
  Keep one open; confirm with `get_state`'s `sequence_path`.
- **A capture after a scrub needs an explicit camera** to redraw the frame
  (skill `ue-viewport`, "Freshness"), and PIE must be stopped.
- **`take_start` clears the Take Recorder's sources and adds none**, and
  changes two of the user's Take Recorder settings, at least for the
  session. To record the
  player in PIE, use `run_sequence` with `take_record` (skill
  `ue-pie-testing`).
- **`close` closes every open Level Sequence editor**, unsaved or not.

## Recipes

Argument names are exact.

**Look at one frame of a sequence**

1. Stop PIE if it runs (skill `ue-pie-testing`): a level-viewport capture
   takes the PIE view while a session is up.
2. `sequencer` `{"operation": "open", "sequence_path": "/Game/Cinematics/SEQ_Intro", "frame": 120, "camera_location": {"x": -400, "y": 0, "z": 200}, "camera_rotation": {"pitch": -10, "yaw": 0, "roll": 0}}`.
   Give `camera_location` and `camera_rotation` together, with all six
   values; omit both when the sequence's own camera cuts should frame it.
3. Check the result: when you passed a camera, `camera_set` must be `true`;
   a `frame` field must be present. If `frame` is missing, the scrub failed
   without an error; send `scrub` yourself.
4. `sequencer` `{"operation": "get_state"}`. Confirm `sequence_path` and
   `current_frame`, and note `camera_location` and `camera_rotation`.
5. `capture_viewport` with `camera_location` and `camera_rotation` set to the
   values from step 4, all six (skill `ue-viewport`), and write the picture
   to a file as that skill describes. Passing the viewport's own values keeps
   the camera where it is and makes the capture draw first.

**Step through frames**

1. `sequencer` `{"operation": "get_state"}` for `playback_start_frame`,
   `playback_end_frame` and `display_rate`.
2. `sequencer` `{"operation": "scrub", "frame": 48}`, then `get_state` and
   `capture_viewport` with its camera, as in steps 4–5 above. Repeat per
   frame, staying below `playback_end_frame`.
3. `sequencer` `{"operation": "close"}` when done.

**Check a sequence's camera cuts**

Load section `camera-cuts`: three frames per cut (first, middle, last), the
camera-cut lock, and what a wrong picture means.

**Record gameplay as a take**

Do not use this tool. `gameplay_debug` `run_sequence` with `take_record: true`
starts PIE, adds the player's pawn as the recording source, stops the take,
saves it and writes its path to the manifest (skill `ue-pie-testing`, section
`sequences`). `take_start` here adds no source.

**A take is still recording**

`run_sequence` stops its take only when the run completes. After any other
end the plugin leaves it alone, and it may still be recording.

1. `sequencer` `{"operation": "take_status"}`. Look at `is_recording`.
2. If `true`: `sequencer` `{"operation": "take_stop"}`. Note `sequence_path`;
   an empty string means no sequence came out of it.

**Look at a recorded take**

1. Take the path from the manifest's `take_sequence_path`, from `take_stop`'s
   `sequence_path`, or from `take_status`'s `last_sequence_path`.
2. Confirm the asset exists with `asset_search` (skill `ue-assets`), then
   continue as in "Look at one frame of a sequence".

## Before the first call

- **Result shape.** Success is the message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope, and the tool's own fields sit under its `data` key (skill
  `ue-tasks`). Failure is `Error: <message>` with no data.
- **No aliases, no lowercasing.** An operation in another case, or any other
  word, returns `Unknown operation '<op>'. Valid: take_start, take_stop,
  take_status, open, scrub, get_state, close`. A missing `operation` returns
  `Missing required parameter: operation`.
- **Marked as modifying, and it is.** The tool opens and closes asset
  editors, moves the level viewport's camera, moves the playhead, and changes
  Take Recorder settings. It changes no asset by its own code, and saves none.
- **No PIE check.** No operation looks at whether PIE runs. How the Sequencer
  editor and the Take Recorder behave during a session is the engine's
  business.

## Operations

| Operation | Required | Optional | Result fields |
|---|---|---|---|
| `open` | `sequence_path` | `frame`, `camera_location`, `camera_rotation` | `sequence_path`, `camera_set`, `frame` (only when the scrub worked) |
| `scrub` | `frame` | — | `frame` |
| `get_state` | — | — | `current_frame`, `display_rate`, `playback_speed`, `playback_start_frame`, `playback_end_frame`, `sequence_name`, `sequence_path`, `camera_location`, `camera_rotation` (section `operations` says when each is present) |
| `close` | — | — | none |
| `take_start` | — | `slate_name` | `recording`, `slate_name` (when given) |
| `take_stop` | — | — | `recording`, `sequence_path` |
| `take_status` | — | — | `is_recording`, `state`, `last_sequence_path` (when there is one) |

The tool's description and the dispatch list the same seven operations.
`frame` is a **display frame**, as the Sequencer's timeline shows it, cut off
towards zero; it is not checked against the playback range. The Take
Recorder operations exist only on **UE 5.7 or later**: before that they fail
with `TakeRecorder requires UE 5.7+` (`take_status`: `Failed to get Take
Recorder status`).

### Which Sequencer

`scrub` and `get_state` do not take a path. They act on "the active
Sequencer", which the code finds by walking all assets that have an editor
open and taking the **first Level Sequence** whose editor gives a Sequencer.
With several open, which comes first is not defined by the toolkit. Keep one
open, and confirm with `get_state`'s `sequence_path`. With none open, both
fail with `No active Sequencer found`.

### What `open` does not prove

The short form; section `operations` has the full list.

- The camera is set only when **both** camera objects are given;
  `camera_set: true` means the call was made, not that a camera moved, and a
  key left out reads as 0.
- A failed scrub inside `open` is swallowed: no `frame` in the result.
- A sequence with camera cuts may take the viewport camera over after the
  scrub (not verified). Compare `get_state`'s camera with what you sent.
- Nothing checks that the sequence's bindings resolve in the open level.

### take_start, take_stop, take_status

**Load section `take-start` before you call `take_start`.** `take_stop`
stops whatever take runs, also one started by `run_sequence` or the user; an
empty `sequence_path` is still a success, and a path is not yet a confirmed
saved asset. `take_status`'s `state: "started"` is the only proof that a
take records. Details: section `operations`.

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

## When a call fails

Load section `failures`: `open`'s errors, and a table from what you see to
what it means and what to do.

## Background in the plugin

The plugin ships no context or domain document on the Sequencer or the Take
Recorder. Skill `ue-context` says where its other documents are.

## See also

- `ue-viewport`: `capture_viewport`, for the picture after `open` or `scrub`.
- `ue-pie-testing`: `run_sequence` with `take_record`, section `sequences`;
  `get_output_log`.
- `ue-python`: `execute_script`, for authoring (section `camera-cuts`).
- `ue-assets`: `asset_search` for a Level Sequence's path, `asset save_all`.
- `ue-level-actors`: `get_level_actors`, for camera actors' positions.
- `ue-tasks`: the task envelope and bridge timeouts.
- `unreagent-iterate`: the wait for the plugin, and the editor log.
