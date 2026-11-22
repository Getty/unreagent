---
name: ue-viewport
description: >
  Take a screenshot of the level viewport, the Play In Editor view or an asset
  editor's 3D preview with capture_viewport: camera placement, the preview
  camera presets, animation frames, and getting the JPEG into a file you can
  look at. Use before you check a change visually, or when a capture came back
  as a long base64 string.
toolkit_tools: [capture_viewport]
---

# UE viewport capture: capture_viewport

The UE LLM Toolkit plugin's **`capture_viewport`** tool renders one picture of
a viewport and returns it as a 1024x576 JPEG. Through the plugin's MCP bridge
it is listed as `unreal_capture_viewport`. It works only while the editor is
running and the plugin is reachable. `unreagent-iterate` covers the wait for
the plugin.

All parameters are optional:

| Parameter | Type | Read when |
|---|---|---|
| `asset_path` | string | always: chooses the asset editor mode |
| `frame` | integer | with `asset_path` only |
| `camera` | string: `front` (default), `from_left`, `from_right` | with `asset_path`, and only without the two below |
| `camera_location` | object `{x, y, z}` | not while PIE runs, unless `asset_path` is given |
| `camera_rotation` | object `{pitch, yaw, roll}`, in degrees | the same |

This skill was written from the toolkit's C++ and bridge source. It has not
been exercised against a running editor.

## Before the first call

- **Results through the bridge.** Success is the tool's message, a blank line,
  then JSON. By default the bridge runs the call as a task, so the JSON is a
  task envelope and the fields named here sit under its `data` key (skill
  `ue-tasks`). Failure is only `Error: <message>`.
- **The picture is text.** The bridge passes `image_base64` on as part of
  that JSON. It is not turned into an image, so your client shows you a long
  string, not a picture. "Getting the picture" below shows how to look at it.
- **Read-only in name only.** The tool is marked read-only, but it changes
  editor state and leaves it changed: it opens asset editors, moves the editor
  or preview camera, and pauses animation previews.
- **It runs on the game thread**, like every toolkit call. Rendering and
  reading the pixels happen inside the call.

## Which viewport

| Call | Captures | `viewport_type` |
|---|---|---|
| with `asset_path` | the 3D preview in that asset's editor | `AssetEditor` |
| without `asset_path`, PIE running | the PIE viewport | `PIE` |
| without `asset_path`, no PIE | the editor's active viewport | `Editor` |

### Level and PIE

- **PIE comes first.** While a PIE session runs, a call without `asset_path`
  always captures the PIE view. The level viewport cannot be captured until
  PIE has stopped.
- **The active viewport** is the engine's choice: normally the level
  viewport. Which one counts as active when several level viewports are open,
  or an asset editor was used last, has not been verified. Check
  `viewport_type` and `camera_location` in the result.
- **Camera.** `camera_location` and `camera_rotation` move the editor camera
  before the capture, and it stays there afterwards. Give all three keys of
  each: a key you leave out becomes 0, not the current value, so
  `{"camera_location": {"z": 500}}` puts the camera at `(0, 0, 500)`. A value
  that is not an object is ignored. While PIE runs, both are ignored without a
  word, although the tool's own description says they work with level
  viewports.
- **Freshness.** Without a camera change the tool reads the pixels the
  viewport last drew; with one, it draws the viewport once first. Whether a
  frame is current while the editor sits in the background has not been
  verified. If a picture looks stale, capture again with an explicit camera.
- `camera` and `frame` are not read in this mode.

### Asset editor preview

- **`asset_path`** is loaded as an object path. Pass the full form,
  `/Game/Characters/SK_Hero.SK_Hero`. The plugin's own examples pass the
  package path (`/Game/Meshes/SM_MyMesh`); whether that form loads has not
  been verified. On `Asset not found: <path>`, try the object path. Find the
  path with `asset_search` (skill `ue-assets`).
- **The editor opens if needed**, and stays open. The tool's description says
  the editor must already be open; its parameter text and its code open it.
- **Which editors work.** The tool looks for the Persona preview tab, then
  for the Blueprint editor's viewport tab, and inside the tab for a viewport
  widget of the plain editor or the animation editor type. Persona assets
  (skeletal meshes, animations, Animation Blueprints, Physics Assets, as the
  description lists them) are what it was built for. For Blueprints, static
  meshes and other editors it has not been verified, and the errors to expect
  are `No preview viewport tab found for asset: <path>. This editor type may not have a 3D preview.`
  and `Could not find a valid scene viewport in the preview tab.` The tab has
  to be open in that editor: a closed viewport tab is not found.
- **Camera presets.** Without `camera_location` and `camera_rotation`, the
  tool always places the preview camera at a preset. The names are not case
  sensitive:

  | `camera` | Location | Rotation (pitch, yaw, roll) |
  |---|---|---|
  | `front` (default) | `(61.84, 702.57, 117.44)` | `(-5.78, -97.59, 0)` |
  | `from_left` | `(-668.44, 238.13, 122.10)` | `(-4.18, -14.59, 0)` |
  | `from_right` | `(539.56, 322.97, 131.99)` | `(-4.98, -158.99, 0)` |

  These are fixed positions, not fitted to the asset's size: a much larger or
  smaller asset can be cut off or tiny. An unknown name gives the `front`
  position without an error, and the result's `camera_preset` still echoes
  the name you passed.
- **Your own camera.** With either `camera_location` or `camera_rotation`,
  no preset is applied and the result has no `camera_preset`. The same
  missing-key rule applies as in the level viewport. The preview camera stays
  where you put it.
- Before reading the pixels, the tool draws the preview once. A viewport that
  still has no size, as can happen right after the editor opens, gets five
  more tries 0.1 s apart. After that the call fails with
  `Viewport has invalid size.`

### Animation frames

`frame` poses an animation before the capture. It needs `asset_path`, and the
asset must be an animation asset, such as an Animation Sequence or a Montage.
Otherwise the call fails with
`'frame' parameter requires an animation asset (AnimSequence, AnimMontage, etc.).`

- The value is rounded to a whole frame. A negative `frame` is ignored.
- An Animation Sequence converts frames with its own sampling rate. Any other
  animation asset is treated as 30 fps, and its `total_frames` is 0.
- The time is clamped to the animation's length: a frame past the end is
  taken as the end, without an error. Compare `frame_time` with `play_length`.
- The tool looks for the first skeletal mesh component that previews exactly
  this asset, pauses it and moves it to that time. The preview stays paused
  there. Without such a component the call fails with
  `Could not find preview component playing this animation. Make sure the animation preview is visible.`

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
| `frame`, `frame_time`, `play_length`, `total_frames`, `frame_rate` | with `frame` | the frame used, its time and the animation's length in seconds, the frame count, and the rate as a string such as `30/1` |

The message is `Captured <viewport_type> viewport: 1024x576 JPEG`.

**Every picture is 1024x576**, whatever the viewport's shape. It is stretched
to that size, not cropped or letterboxed, so a viewport that is not 16:9 comes
out distorted. `original_width` and `original_height` tell you by how much.

## Getting the picture

Through the bridge, a capture is only useful for its other fields, and the
base64 string costs as much context as the whole JPEG. To look at the picture,
get it into a file and open that file with a reader of your client that shows
images. unreagent's `read_file` returns text only (skill `unreagent-files`).

**In PIE:** the `capture` steps and auto-capture of `gameplay_debug`'s
`run_sequence` write JPEG files to disk themselves (skill `ue-pie-testing`,
section `sequences`).

**Any mode, through `run_python`:** the plugin also answers plain HTTP. A
script on the host can call the tool there, decode the image and print only
the file's path. `run_python` is off unless the project enables it (skill
`unreagent-runtimes`). This is a sketch, not exercised against an editor:

```python
import base64, json, os, urllib.error, urllib.request

# The bridge's UNREAL_MCP_URL; the port is 3000 unless the editor was
# started with -MCPPort=<n>.
URL = "http://127.0.0.1:3000/mcp/tool/capture_viewport"
params = {"asset_path": "/Game/Characters/SK_Hero.SK_Hero", "camera": "front"}

req = urllib.request.Request(URL, data=json.dumps(params).encode(),
                             headers={"Content-Type": "application/json"})
try:
    with urllib.request.urlopen(req, timeout=60) as r:
        res = json.load(r)
except urllib.error.HTTPError as e:  # a failed tool answers with HTTP 400
    res = json.load(e)
if not res.get("success"):
    # Tool failures carry "message"; the server's own errors carry "error".
    # A string passed to SystemExit is printed and gives exit code 1.
    raise SystemExit("capture failed: " + str(
        res.get("message") or res.get("error") or json.dumps(res)))

out = os.path.abspath("Saved/agent-captures/capture.jpg")
os.makedirs(os.path.dirname(out), exist_ok=True)
with open(out, "wb") as f:
    f.write(base64.b64decode(res["data"]["image_base64"]))
print(out)
```

The body is the tool's arguments as they are; the answer is
`{success, message, data}` without a task around it. Errors of the HTTP server
itself (a bad body, no tool name) come as `{success: false, error}` instead.
Unknown parameter names are reported in the message instead of being ignored.
The relative path lands in the runtime's working directory, normally the
project.

**Call directly only while the editor is idle**: no other toolkit call,
script, import or `run_sequence` in progress. Which thread handles the request
is up to the engine's HTTP server; the plugin's comments say one other than
the game thread. In that case a direct call waits at most 30 s for the game
thread to run it, then answers
`Tool 'capture_viewport' execution timed out after 30 seconds`. That answer is
not an ordinary error. From the plugin's code (`UnrealClaudeMCPServer.cpp`, an
upstream bug), the job is still queued on the game thread when the answer
goes out, and when it runs later it writes into memory the plugin has already
given back. That can crash the editor. Do not retry: check with unreagent's
`status` that the editor is still running (skill `unreagent-iterate`) before
anything else. On the game thread there is no such answer: the sketch's own
60 s timeout ends the wait with a Python exception, and the capture may still
run. Skill `ue-tasks`, "A 30 s timeout can crash the editor", covers both
cases for every toolkit call.

## Typical sequences

**Check a placement in the level**

1. With PIE stopped, capture with `camera_location` and `camera_rotation`
   aimed at the spot. Give all six values.
2. Write it to a file and look at it ("Getting the picture").
3. Capture again from a second angle before you conclude anything about
   depth.

**Check a pose in an animation**

1. `asset_path` set to the animation, `frame` set to the frame, and a
   `camera` preset, or your own camera when the presets do not frame it.
2. Confirm `frame_time` against `play_length`, so you know the frame was not
   clamped.
3. The preview stays paused at that frame, and the tool has no way to resume
   it. Tell the user if they work in that editor.

**Look at the running game**

Capture without `asset_path` while PIE runs, or use `gameplay_debug`
`capture_pie`, which also returns the frame number and game time (skill
`ue-pie-testing`).

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| a long base64 string, no picture | the bridge returns text | write it to a file ("Getting the picture") |
| `No viewport available. Open a level or start PIE.` | no PIE and no active editor viewport | open a level (skill `ue-levels`) |
| `Asset not found: <path>` | the path did not load | pass the object path `/Game/…/Name.Name`; check it with `asset_search` |
| `No preview viewport tab found for asset: <path>. …` | that editor has no tab the tool knows, or its viewport tab is closed | open the viewport tab, or use the level viewport with your own camera |
| `Could not find a valid scene viewport in the preview tab.` | the tab holds a viewport type the tool does not search for | the same |
| `Could not find preview component playing this animation. …` | the editor previews something else, or the preview is hidden | open the animation's own editor and show its viewport |
| `Viewport has invalid size.` | the viewport has no size yet, or is collapsed | show the viewport, then call again |
| the camera parameters have no effect | PIE is running | stop PIE, or capture in the asset editor |
| the camera ends up at the origin of an axis | a key is missing from `camera_location` or `camera_rotation` | pass all three keys |
| the asset is cut off or tiny | the presets are fixed positions | pass your own camera |
| a distorted picture | the viewport is not 16:9 | compare `original_width` and `original_height` |
| `Error: Task timed out after 300000ms (task_id: …)` | the bridge stopped waiting | skill `ue-tasks` |
| `Tool 'capture_viewport' execution timed out after 30 seconds`, from the `run_python` sketch | the request was handled off the game thread and waited 30 s for it; the job left queued can crash the editor | do not retry; check the editor with unreagent's `status` ("Getting the picture") |
| `TimeoutError: timed out` after 60 s, from the `run_python` sketch | the request was handled on the game thread, which stayed busy past the sketch's own 60 s; the capture may still run | check the editor with unreagent's `status`; capture again only while the editor is idle ("Getting the picture") |

## Background in the plugin

- The plugin's `Resources/domains/assets.md`, section "Visual Inspection",
  has capture examples and rules for judging what you see. Its "Response
  auto-saves image to disk" is done by the toolkit's own shell wrapper, not by
  the MCP bridge. Skill `ue-context` says where the domain documents are.

## See also

- `ue-pie-testing`: PIE sessions, `capture_pie`, and capture steps that write
  files.
- `ue-assets`: `asset_search`, for the path of the asset to capture.
- `ue-level-actors`: placing the actors you want to look at.
- `ue-lighting`: lights, fog and post-process volumes, whose effect a capture
  shows.
- `ue-sequencer`: opening a Level Sequence and scrubbing it to a frame before
  a level-viewport capture.
- `ue-tasks`: the task envelope and bridge timeouts.
- `unreagent-runtimes`: `run_python`, for the HTTP sketch.
- `unreagent-files`: `read_file`, which returns text only.
