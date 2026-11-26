# anim_edit: when a call fails

Paths, saving and batch results are in section `reference`; the "Must not
get wrong" list is in the skill itself.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or an empty string |
| `Missing required parameter: asset_paths (non-empty array)` | no `asset_paths` array, or an empty one |
| `asset_paths contains no valid paths` | no entry of `asset_paths` could be read as a non-empty string |
| `Unknown operation: '<op>'. Valid: …` | misspelt operation, or one of the curve operations |
| `Failed to load anim sequence: <path>` / `Asset is not an AnimSequence: <path>` | the path does not load as an AnimSequence |
| `Failed to load USkeletalMesh: <path>` | the path does not load as a skeletal mesh |
| `Mesh has no USkeleton: <path>` | the mesh has no skeleton asset |
| `Batch adjust: 0 succeeded, …` / `Resample to <fps> FPS: 0 succeeded, …` / `Replace skeleton: 0 succeeded, …` | every asset of a batch failed; run `inspect_track` for the reason |

The errors of each operation are in its section.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| `Unknown operation: 'get_curves'` | the curve operations are advertised but not dispatched | no `anim_edit` route; montage curves: `ue-anim-montage` |
| a batch call failed with only a count | every asset failed, and the reasons are dropped | `inspect_track` on one of the assets |
| `success` with `fail_count` above 0 | some assets failed | read `error` in `results` |
| a test edit was saved with the call | `save` defaults to `true` | pass `"save": false` while trying things |
| after `resample` the sequence is longer or shorter and plays slower or faster | `resample` changed the frame count at the old rate (section `sequences`) | restore from a backup or source control, reimport at the wanted rate |
| `adjust_track` set every scale key to 0 | `scale_override` was partial or not an object | send all three components as an object |
| `rename_bone` worked, but another mesh still has the old name | only the mesh at `mesh_path` is renamed | `rename_bone` on that mesh too; it takes the mesh-only path |
| a change is gone after an editor restart | it was made with `"save": false` and never saved | `asset save_asset` |
| `set_additive_type` succeeded, but the sequence still plays as a full pose | `base_pose_type: "None"`, or `AnimScaled` / `AnimFrame` without a valid base | a base pose of `RefPose`, `LocalAnimFrame`, or one with `ref_pose_seq` |
| an aim offset rejects or mis-blends a pose | not every pose is `RotationOffsetMeshSpace` with the same base | section `sequences`, aim offset poses |
| after `replace_skeleton` limbs stretch or twist | the two skeletons are not the same rig | undo with `replace_skeleton` back to `old_skeleton`, then retarget (skill `ue-retarget`) |
| a second `adjust_track` / `transform_vertices` doubled the change | both add to the current state | apply the inverse offset, or restore the backup |
| `extract_range` copy is off by one frame | both ends are inclusive key indices | `end_frame` is the last frame you keep |
| a montage or blend space broke after a skeleton swap | they keep their own skeleton reference | rebuild them on the new skeleton (skills `ue-anim-montage`, `ue-blend-space`) |
