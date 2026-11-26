# sequencer: errors and failure modes

## Errors of `open`

| Error | Cause |
|---|---|
| `Missing required parameter: sequence_path` | missing or empty |
| `Failed to load Level Sequence: <path>` | nothing at that path, or the asset is not a Level Sequence. `asset_search` (skill `ue-assets`) finds the path |
| `Failed to open editor for: <path>` | the engine did not open an editor |
| `Editor not available`, `AssetEditorSubsystem not available` | the editor is not in a state to open asset editors |

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
| a capture at `playback_end_frame` shows something unexpected | that frame is one past the range | scrub to `playback_end_frame - 1` |
| every frame of a sequence with cuts shows the same view | the camera-cut lock is off, or the viewport is locked to one camera (as `create_camera` leaves it) | section `camera-cuts`, "Verifying the cuts" |
| a Python script that adds cameras or cuts did nothing | `create_camera` needs an open Sequencer; or the script failed | `open` first; read the script's output (skill `ue-python`) and `get_output_log` |
| `take_stop` succeeded with `sequence_path: ""` | the recorder produced no sequence | check `take_status`; record through `run_sequence` |
| `take_start` succeeded and the take is empty | no source was added | record through `run_sequence` with `take_record` |
| `Take Recorder is already recording` | a take runs, perhaps one a failed run left | `take_status`, `take_stop` |
| `TakeRecorder requires UE 5.7+` | the plugin was built for an older engine | the take operations are not available there |
| `Error: Task timed out after …ms (task_id: …)` | the bridge stopped waiting | skill `ue-tasks`; `get_state` or `take_status` before you retry |
