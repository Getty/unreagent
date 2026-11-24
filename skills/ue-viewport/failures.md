# UE viewport capture: when a call fails

"Getting the picture" is a heading of the skill itself.

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
