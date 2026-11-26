# UE blend spaces: when a call fails

## Errors

| Error | Cause | Fix |
|---|---|---|
| `Missing required parameter: <name>` | required parameter absent or empty; an index absent, negative or a non-numeric string | send it, as a JSON number for indices |
| `Unknown operation: '<op>'. Valid: …` | misspelt operation | use a listed name |
| `Failed to load blend space: <path>` / `Asset is not a BlendSpace: …` | nothing at that path loads as a blend space | `list` / `asset_search`, use the reported object path |
| `Failed to load animation: <path>` / `Asset is not an AnimSequence: …` | `animation_path` is not an animation sequence (a montage or blend space is not accepted) | point at a sequence |
| `Failed to load skeleton: <path>` / `Asset is not a Skeleton: <path>` | `create` got a mesh or wrong path | pass the Skeleton asset (`asset_search` `class_filter: "Skeleton"`) |
| `Failed to create blend space: <pkg>/<name>` | the engine's factory returned nothing (bad package path, name in use: unverified which) | check the folder and the name with `list` |
| `AddSample failed for <path> at (<x>, <y>)` | the engine refused: position taken, incompatible skeleton, additive type not allowed or not matching the other samples (section `ue-facts`) | `inspect` for a sample at that position; check the clip's skeleton and additive type |
| `EditSampleValue failed for index <i>` | `move_sample` onto a taken position, or a single-frame index past the clip | pick a free position |
| `Animation <path> is not compatible with this blend space's skeleton` | `set_sample_animation` refused: skeleton **or** additive type (the text says skeleton for both) | check both |
| `Sample index <n> out of range (0-<last>)` | stale or wrong index | `inspect`, use current indices |
| `Axis index <n> out of range (0-<last> for 1D)` / `… for 2D)` | axis 1 on 1D, or above 1 | 0 on 1D, 0/1 on 2D |
| `Failed to save: <path>` | the engine's save returned false (wrong path; other causes not visible to the tool) | check the path with `inspect`; look at `get_output_log` |
| `Error: Batch: <n> OK, <m> errors, <t> total` | at least one batch op failed; the others applied | `inspect`, repeat the missing ops one by one |
| `Error: Task timed out after 300000ms …` | the bridge stopped waiting | `inspect` before retrying (skill `ue-tasks`) |

## Symptoms

| What you see | What it means | What to do |
|---|---|---|
| plugin tools missing, every call fails | editor not running or still loading | `unreagent-iterate` |
| change gone after an editor restart | never saved | `save` after each finished change |
| an axis has a strange range and many divisions | samples were added before the range was set; the engine grew it | move samples inside the intended range, then `set_axis` |
| a later call changed the wrong sample | `remove_sample` moved the last sample into the freed index, or a rebuild dropped a duplicate | `inspect`, use the new indices |
| a 2D sample sits at y=0 | `move_sample` without `y` | pass both coordinates |
| positions differ from what you sent | snap is on for both axes; the rebuild snapped them to the grid | turn `snap_to_grid` off or place samples on grid points |
| `rate_scale` unchanged | you sent 0, a negative value, or nothing | send a value from 0.01 to 64 |
| interpolation type unchanged | `interp_type` not one of the six names | use one of the six |
| `create` made a 2D blend space | `type` lacked `1D` / `AimOffset` | check the result's `type`; delete and recreate |
| `list` returns nothing | the blend spaces sit in subfolders | `recursive: true` |
| `inspect` says `BlendSpace` for your aim offset | the reader names only the base kind | expected; nothing to fix |
| the blend space plays nothing / `anim_length` 0 | no sample is valid (out of range after narrowing, wrong additive type, missing clip) | `inspect`, fix ranges and clips |
| `geometry.triangles` 0 on a 2D blend space | fewer than three samples, all on one line, or Use Grid is on (`settings.interpolate_using_grid`) | add samples off the line; with Use Grid on, 0 is expected |
| feet slide or the blend looks out of step | clips placed at the wrong speed, or no matching sync markers | place clips at their authored speed or adjust `rate_scale`; give cycles matching sync markers (skill `ue-anim-sequence`) |
| aim offset bends the body oddly | samples are local-space additive, or not additive against the same base pose | re-author as Mesh Space additive (section `ue-facts`, skill `ue-anim-sequence`) |
