# sequencer: each operation in detail

The skill itself has the operations table, the result shape and which
Sequencer `scrub` and `get_state` act on. `take_start` is in section
`take-start`, how `open` reads its numbers in section `open-values`. Read in
`MCPTool_Sequencer.cpp` and `SequencerController.cpp` at toolkit commit
c1b6709; engine facts marked "UE 5.8.4 source" were read there, not run.

## open

Loads the Level Sequence and opens its editor, then, in this order, sets the
viewport camera and scrubs.

| Parameter | Meaning |
|---|---|
| `sequence_path` | required. A package path, `/Game/Cinematics/SEQ_Intro`, or an object path, `/Game/Cinematics/SEQ_Intro.SEQ_Intro`. A path without a dot gets the object name appended; if that does not load, the path is tried as given |
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
- **A key left out of a camera object is not the current value**: it reads
  as 0, with no error (section `open-values`). Pass all six.
- **A failed scrub is swallowed**, and so is a `frame` the tool cannot read
  as a number. `open` still succeeds; the result just has no `frame` and the
  message no ` at frame`. When `frame` is present it is the number that was
  read, fraction included, not a value read back from the Sequencer: `12.9`
  scrubs to frame 12, the message says ` at frame 12`, `frame` is 12.9.
- **The camera may not stay where you put it.** The scrub comes after the
  camera call. A sequence whose camera cuts are locked to the viewport puts
  the cut's camera there instead (section `camera-cuts`); whether that
  happens on this scrub has not been verified. Compare `get_state`'s
  `camera_location` with what you sent.
- **Nothing is checked about the level.** The sequence is opened whatever
  level is loaded. Whether its bindings resolve there is not reported.

Errors: section `failures`.

## scrub

Moves the playhead of the active Sequencer to a **display frame**: a frame
number at the sequence's display rate, as the Sequencer's timeline shows it.
The code converts it to the sequence's tick resolution and sets the local time
directly, with evaluation.

- `frame` is required, and it is read like `open`'s `frame`: `true` scrubs
  to frame 1 and `"12"` to frame 12. Only a missing `frame`, or one that
  read rejects (`null`, an array, an object, any other string), returns
  `Missing required parameter: frame`. A fraction is cut off towards zero.
- **The frame is not checked against the playback range.** A frame outside
  it is sent as it is; what the Sequencer does with it has not been verified.
  Take the range from `get_state`.
- The result's `frame` is the truncated number you sent. To see where the
  playhead is, call `get_state`.
- A success says the time was set. It does not say that the viewport has drawn
  that frame; skill `ue-viewport`, "Freshness", covers what a capture reads.

The message is `Scrubbed to frame <n>`.

## get_state

Read-only. The message is `Sequencer state retrieved`.

| Field | Present | Holds |
|---|---|---|
| `current_frame` | always | the playhead as a display frame, rounded down |
| `display_rate` | always | a string, numerator over denominator, such as `30/1` |
| `playback_speed` | always | the Sequencer's playback speed |
| `playback_start_frame`, `playback_end_frame` | when the playback range has both bounds | the range's bounds in display frames, rounded down |
| `sequence_name`, `sequence_path` | when a sequence is focused | the focused sequence's name and object path (`/Game/…/SEQ_Intro.SEQ_Intro`) |
| `camera_location`, `camera_rotation` | when the level editor has an active viewport | that viewport's camera, `{x, y, z}` and `{pitch, yaw, roll}` |

- **`playback_end_frame` is one past the last frame.** The engine stores the
  playback range with an inclusive lower and an **exclusive** upper bound
  (`UMovieScene::SetPlaybackRange`, `MovieScene.cpp`, UE 5.8.4 source), and the
  Sequencer returns the focused movie scene's range as it is
  (`FSequencer::GetPlaybackRange`). The last frame to scrub to is
  `playback_end_frame - 1`. With a display rate that does not divide the
  tick resolution evenly, the rounding can shift this by one.
- The camera is the level viewport's, whether or not the Sequencer controls
  it. The fields say nothing about a camera actor in the sequence.
- "Focused" is the engine's term for the sequence entered in the editor:
  with a shot or sub-sequence entered, presumably that one, not the asset you
  opened, and `scrub` sets its local time (`SetLocalTimeDirectly`). Not
  verified; compare `sequence_path` with what you opened.

## close

Closes the editors of **every** open Level Sequence. The header says "Close
the active Sequencer"; the code closes all of them. The message is
`Sequencer closed`, with no data.

- With none open it fails: `No open Level Sequence editors found`. Treat that
  as "already closed".
- The code does not save the sequence and does not look at whether it has
  unsaved changes. This tool makes none, but a script or the user may have.
  What the engine does on closing a dirty sequence has not been verified;
  save first (skill `ue-assets`, `asset save_all`).
- The viewport camera stays where it was put.

## take_stop

Stops the running take and returns `recording: false` and `sequence_path`,
the package path of the recorded Level Sequence. The message is
`Take Recorder stopped. Sequence: <path>`.

- With no take running: `Take Recorder is not recording`.
- **An empty `sequence_path` is still a success.** The code asks for the last
  recorded sequence, falls back to the sequence that was being recorded, and
  returns `""` when it has neither (the log then has `Take recording stopped
  but no sequence was produced`).
- **A path is not a saved asset.** This operation does not save the sequence;
  it relies on the save-recorded-assets setting that a start through the
  toolkit switches on. `run_sequence` saves its take explicitly. Whether the
  stop has finished writing when the call returns has not been verified.
  Confirm the asset with `asset_search` (skill `ue-assets`) before you build
  on it.
- It stops whatever take is running, also one started by `run_sequence` or by
  the user.

## take_status

Read-only. Returns `is_recording`, `state` and, when the recorder has a last
recorded sequence, `last_sequence_path`. The message is
`Take Recorder state: <state> (recording: true|false)`.

`state` is one of `pre_initialization`, `counting_down`, `pre_record`,
`ticking_after_pre`, `started`, `stopped`, `cancelled`, or `unknown`.

`last_sequence_path` is an **object path** (`/Game/…/Take.Take`), while
`take_stop` returns the package path. `open` accepts both.

## Engine version

The Take Recorder code is compiled only for **UE 5.7 or later**. On an older
engine `take_start` and `take_stop` return `TakeRecorder requires UE 5.7+`,
and `take_status` returns `Failed to get Take Recorder status`. `open`,
`scrub`, `get_state` and `close` are not version-gated.

`take_status` returns the same `Failed to get Take Recorder status` when the
Take Recorder subsystem is missing. `take_start` and `take_stop` say
`TakeRecorderSubsystem not available` in that case, and `Editor not
available` without an editor.
