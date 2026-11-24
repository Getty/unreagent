# anim_edit: when a call fails

Paths, saving, batch results and "What a success does not prove" are in the
skill itself.

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
| after `resample` the sequence plays at a different speed or length | the frame rate was not changed (section `sequences`) | reimport the source at the wanted rate instead |
| `adjust_track` set every scale key to 0 | `scale_override` was partial or not an object | send all three components as an object |
| `rename_bone` worked, but another mesh still has the old name | only the mesh at `mesh_path` is renamed | `rename_bone` on that mesh too; it takes the mesh-only path |
| a change is gone after an editor restart | it was made with `"save": false` and never saved | `asset save_asset` |
