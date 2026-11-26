# UE viewport capture: when a call fails

"Getting the picture" is a heading of the skill itself; the HTTP sketch is in
section `save-picture`, the capture loop in section `verify-loop`.

## Error texts

Errors come through the bridge as `Error: <message>`.

| Error | Mode | What it means | What to do |
|---|---|---|---|
| `Editor is not available.` | any | no editor | wait for the plugin (skill `unreagent-iterate`) |
| `No viewport available. Open a level or start PIE.` | level / PIE | no PIE and no active editor viewport | open a level (skill `ue-levels`) |
| `Asset not found: <path>` | asset | the path did not load | pass the object path `/Game/…/Name.Name`; check it with `asset_search` (skill `ue-assets`) |
| `AssetEditorSubsystem not available.` | asset | the editor's asset editor service is missing | the editor is starting or shutting down; try again later |
| `Failed to open editor for asset: <path>` | asset | the asset has no editor that opened | capture it placed in the level instead |
| `Could not get tab manager from asset editor.` | asset | the editor has no tab layout the tool can search | the same |
| `No preview viewport tab found for asset: <path>. This editor type may not have a 3D preview.` | asset | that editor has no tab the tool knows, or its viewport tab is closed | open the viewport tab, or use the level viewport with your own camera |
| `Could not find a valid scene viewport in the preview tab.` | asset | the tab holds a viewport type the tool does not search for | the same |
| `'frame' parameter requires an animation asset (AnimSequence, AnimMontage, etc.).` | asset | `frame` on a mesh or other asset | leave `frame` out |
| `Could not find preview component playing this animation. Make sure the animation preview is visible.` | asset | the editor previews something else, or the preview is hidden | open the animation's own editor and show its viewport |
| `Viewport has invalid size.` | any | the viewport has no size yet, or is collapsed | show the viewport, then call again |
| `Failed to read viewport pixels.`, `Pixel array size mismatch.` | any | the engine gave no usable pixels | capture again; if it persists, the viewport may be minimised or hidden (unverified) |
| `Failed to create image wrapper.`, `Failed to set image data.`, `Failed to compress image to JPEG.` | any | JPEG encoding failed in the engine | capture again; report it if it persists |
| `Error: Task timed out after 300000ms (task_id: …)` | any | the bridge stopped waiting | skill `ue-tasks` |
| `Tool 'capture_viewport' execution timed out after 30 seconds`, from the `run_python` sketch | any | the request was handled off the game thread and waited 30 s for it; the job left queued can crash the editor | do not retry; check the editor with unreagent's `status` (section `save-picture`) |
| `TimeoutError: timed out` after 60 s, from the `run_python` sketch | any | the request was handled on the game thread, which stayed busy past the sketch's own 60 s; the capture may still run | check the editor with `status`; capture again only while the editor is idle |

## The picture is not what you expected

| What you see | What it means | What to do |
|---|---|---|
| a long base64 string, no picture | the bridge returns text | write it to a file (section `save-picture`) |
| `viewport_type` is `PIE` though you wanted the level | PIE runs, and it comes first | stop PIE (skill `ue-pie-testing`) |
| the camera parameters have no effect | PIE is running | stop PIE, or capture in the asset editor |
| a parameter seems to have no effect | its name is misspelt; queued calls do not warn | check the name; the editor log has `Tool 'capture_viewport': unknown params: <names>` (`get_output_log`, `filter: "unknown params"`) |
| the camera ends up at the origin of an axis | a key is missing from `camera_location` or `camera_rotation` | pass all three keys |
| the camera looks away from the target | yaw or pitch wrong | section `verify-loop`, "Aiming at a point" |
| the asset is cut off or tiny | the presets are fixed positions | pass your own camera |
| `camera_preset` says `side`, the picture is the front | an unknown preset name gives `front` | use `front`, `from_left` or `from_right` |
| `frame_time` equals `play_length` for a frame you thought inside | the frame was past the end and clamped | check `total_frames` (0 for anything but an Animation Sequence, which is counted at 30 fps) |
| a distorted picture | the viewport is not 16:9 | compare `original_width` and `original_height` |
| the picture looks stale | the viewport had not drawn since the change | capture again with an explicit camera, which draws first |
| black, grey, flat or wireframe | view mode, show flags, lighting or exposure | section `verify-loop`, "What else changes a picture" |
| the user's preview is frozen on a frame | `frame` paused the preview, and the tool never resumes it | tell the user |
