---
name: ue-viewport
description: >
  Take a screenshot of the level viewport, the Play In Editor view or an asset
  editor's 3D preview with capture_viewport: which target to capture, camera
  placement and presets, animation frames, a look-adjust-recapture loop, and
  getting the JPEG into a file you can look at. Use before you check a change
  visually, or when a capture came back as a long base64 string.
toolkit_tools: [capture_viewport]
---

# UE viewport capture: capture_viewport

The UE LLM Toolkit plugin's **`capture_viewport`** tool renders one picture of
a viewport and returns it as a 1024x576 JPEG. The bridge lists it as
`unreal_capture_viewport`. It works only while the editor runs and the plugin
answers (`unreagent-iterate` covers the wait). Written from the toolkit's C++
and bridge source (commit c1b6709); not exercised against a running editor.

All parameters are optional:

| Parameter | Type | Read when |
|---|---|---|
| `asset_path` | string | always: chooses the asset editor mode |
| `frame` | integer | with `asset_path` only |
| `camera` | string: `front` (default), `from_left`, `from_right` | with `asset_path`, and only without the two below |
| `camera_location` | object `{x, y, z}` | not while PIE runs, unless `asset_path` is given |
| `camera_rotation` | object `{pitch, yaw, roll}`, in degrees | the same |

## Sections — load them

| Section | Holds | Load it |
|---|---|---|
| `verify-loop` | which target for which check, the look-adjust-recapture loop step by step, aiming a camera at a point, before/after comparisons, what changes a picture (view modes, stats) | before you check a change visually |
| `asset-preview` | the asset editor mode in full: `asset_path`, which editors work and their errors, the presets' positions, `frame` for animation assets | before you pass `asset_path` |
| `save-picture` | the `run_python` HTTP sketch that writes the JPEG to a file, when a direct call is safe, its timeouts | when you need to see the picture, not only its fields |
| `failures` | every error text, and what you see when the picture is wrong, with the fix | when a call fails, or the picture is not what you expect |

## Must not get wrong

- **Results through the bridge.** Success is the tool's message, a blank line,
  then JSON. By default the bridge runs the call as a task, so the fields
  named here sit under the envelope's `data` key (skill `ue-tasks`). Failure
  is only `Error: <message>`.
- **The picture is text.** `image_base64` arrives as part of that JSON, not
  as an image: a long string that costs as much context as the JPEG. See
  "Getting the picture".
- **Read-only in name only.** The tool is marked read-only, but it opens
  asset editors, moves the editor or preview camera, and pauses animation
  previews, and leaves them so.
- **It runs on the game thread**, like every toolkit call. Drawing and reading
  the pixels happen inside the call.

## Which viewport

| Call | Captures | `viewport_type` |
|---|---|---|
| with `asset_path` | the 3D preview in that asset's editor, also while PIE runs | `AssetEditor` |
| without `asset_path`, PIE running | the PIE viewport | `PIE` |
| without `asset_path`, no PIE | the editor's active viewport | `Editor` |

- **PIE comes first.** While PIE runs, a call without `asset_path` always
  captures the PIE view, and `camera_location` / `camera_rotation` are ignored
  without a word (the tool's description says otherwise). Stop PIE to capture
  the level.
- **The active viewport** is the engine's choice, normally the level
  viewport; with several open, which one counts is unverified. Check
  `viewport_type` and `camera_location` in the result.
- **Camera.** `camera_location` and `camera_rotation` move the editor camera
  before the capture, and it stays there. **Give all three keys of each**: a
  missing key becomes 0, not the current value, so `{"z": 500}` puts the
  camera at `(0, 0, 500)`. A value that is not an object is ignored.
- **Freshness.** Without a camera change the tool reads the pixels the
  viewport last drew; with one, it draws the viewport once first. Whether a
  frame is current while the editor sits in the background is unverified: if
  a picture looks stale, capture again with an explicit camera.
- `camera` and `frame` are read only with `asset_path`.

**Asset preview, the short form** (section `asset-preview` has the rest):
pass the object path, `/Game/Characters/SK_Hero.SK_Hero`; the editor opens if
needed and stays open. Without your own camera, **a preset is always applied**
— fixed positions, not fitted to the asset's size, and an unknown `camera` name
silently gives `front` while `camera_preset` echoes your name. `frame` needs an
animation asset; a frame past the end is clamped without an error (compare
`frame_time` with `play_length`), and the preview stays paused there.

## The result

| Field | Present | Holds |
|---|---|---|
| `image_base64` | always | the JPEG, base64-encoded |
| `width`, `height` | always | `1024`, `576` |
| `format`, `quality` | always | `jpeg`, `70` |
| `viewport_type` | always | `PIE`, `Editor` or `AssetEditor` |
| `original_width`, `original_height` | always | the viewport's own size in pixels |
| `camera_location`, `camera_rotation` | not for `PIE` | where the camera was for this picture |
| `asset_path` | `AssetEditor` | as you passed it |
| `camera_preset` | `AssetEditor` without your own camera | the name you passed, or `front` |
| `frame`, `frame_time`, `play_length`, `total_frames`, `frame_rate` | with `frame` | the frame used, its time and the animation's length in seconds, the frame count, the rate as a string such as `30/1` |

The message is `Captured <viewport_type> viewport: 1024x576 JPEG`.

**Every picture is 1024x576**, stretched to it, not cropped or letterboxed: a
viewport that is not 16:9 comes out distorted. `original_width` and
`original_height` tell you by how much.

**Keep `camera_location` and `camera_rotation` from the result.** Passing the
same six values again is the only way to get a second picture from the same
spot, for a before/after comparison.

## Getting the picture

To look at a capture you need it as a file and a reader of your client that
shows images; unreagent's `read_file` returns text only (skill
`unreagent-files`).

- **In PIE:** the `capture` steps and auto-capture of `gameplay_debug`
  `run_sequence` write JPEG files themselves (skill `ue-pie-testing`, section
  `sequences`).
- **Any mode:** a `run_python` script can call the plugin over plain HTTP,
  decode the image and print only the file's path. Load section
  `save-picture` for the script. **Call it only while the editor is idle**:
  a direct call that waits 30 s for the game thread answers with a timeout and
  can crash the editor later (skill `ue-tasks`).
- **Without either**, use only the other fields: `viewport_type`, the camera,
  `original_width`, the frame fields. Do not paste the base64 string anywhere.

## Which capture for which check

Load section `verify-loop` before you check a change visually: it has the
full decision table and the loop. The short form:

| You check | Capture |
|---|---|
| a placed actor, light, material, fog in the level | stop PIE, level viewport with your own camera, two angles |
| a mesh, skeletal mesh or animation on its own | `asset_path`, a preset first, your own camera when it does not frame |
| a pose at a frame | `asset_path` + `frame` |
| behaviour at runtime: movement, gameplay, effects that run only in game | PIE: `gameplay_debug` `capture_pie`, or `capture` steps in `run_sequence` for files (skill `ue-pie-testing`) |
| a Level Sequence shot | skill `ue-sequencer` (scrub, then a level capture) |

## Background in the plugin

The plugin's `Resources/domains/assets.md`, section "Visual Inspection", has
capture examples and inspection rules; skill `ue-context` says where it is.
Its "Response auto-saves image to disk" is the toolkit's own shell wrapper,
not the MCP bridge.

## See also

- `ue-pie-testing`: PIE, `capture_pie`, capture steps that write files, and
  `run_console_command` (view modes, stats) in section `console`.
- `ue-assets`: `asset_search`, for the path of the asset to capture.
- `ue-level-actors`: placing the actors you look at, and their locations.
- `ue-lighting`: lights, fog, post-process, whose effect a capture shows.
- `ue-sequencer`: scrubbing a Level Sequence before a level capture.
- `ue-tasks`: the task envelope and bridge timeouts.
- `unreagent-runtimes`, `unreagent-files`: `run_python`, `read_file`.
