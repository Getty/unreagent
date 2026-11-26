---
name: ue-anim-sequence
description: >
  Edit AnimSequence assets with anim_edit (bone track keys, resampling, frame
  range extraction, additive settings, skeleton swaps) and fix skeletal meshes
  (bones, bind pose, vertices). Use before you change an animation's keys or
  skeleton, or repair a skeletal mesh's bones or bind pose.
toolkit_tools: [anim_edit]
---

# UE animation sequences: anim_edit

The UE LLM Toolkit plugin's **`anim_edit`** tool works on two kinds of assets:

- **AnimSequence assets.** It reads and offsets bone track keys, resamples,
  copies a frame range into a new asset, sets the additive type, and points a
  sequence at another skeleton.
- **Skeletal meshes and skeletons.** It dumps a mesh's bones, bind poses and
  skin weights, adds bones the mesh lacks, renames a bone, changes a bone's
  reference pose, and transforms the mesh's vertices.

Through the plugin's MCP bridge it is listed as `unreal_anim_edit`. It works
only while the editor is running and the plugin is reachable;
`unreagent-iterate` covers the wait for the plugin.

Written from the toolkit's C++ source (`MCPTool_AnimEdit.{h,cpp}`,
`AnimTrackEditor.cpp`) and the UE 5.8.4 engine source; not exercised against
a running editor. The toolkit has no version-specific code here, so 5.6 to
5.8 behave alike as far as the tool is concerned.

## Sections of this skill

| Section | What is in it | Load it when |
|---|---|---|
| `workflows` | recipes: read-first table, skeleton swap within one rig, cutting a clip, fixing a floating root, additive layers and aim poses, wrong mesh scale or axis, missing or misnamed bones, how to verify | you have a goal ("fix X"), not just one call |
| `sequences` | `inspect_track`, `adjust_track`, `resample`, `extract_range`, `set_additive_type` (with what each additive value means), `replace_skeleton` vs retarget | before the first sequence write, or to read a result field or error |
| `meshes` | `inspect_mesh`, `sync_mesh_bones`, `rename_bone`, `set_ref_pose`, `transform_vertices` | before the first mesh write: they change a mesh everything depends on |
| `reference` | parameter reading, paths and loading, saving and undo, batch results, results through the bridge and timeouts | a path does not load, you rely on `"save": false`, or you read a batch result |
| `failures` | error table and failure table (what you see, what it means, what to do) | a call fails in a way you cannot place, or the result is not what you expected |

## Must not get wrong

1. **Every write saves by default** (`save` defaults to `true`), and a failed
   save is not reported. Read first, pass `"save": false` while trying
   things, or `asset duplicate` first (skill `ue-assets`). No undo.
2. **`resample` changes the play length, not the frame rate.** It sets a new
   frame count at the old rate (`AnimTrackEditor.cpp:205-319`; the engine's
   length is frames at the model's rate), so 30 → 60 fps doubles the length
   and halves the speed, whatever the tool's description claims. To change a
   rate, reimport with `custom_sample_rate` (skill `ue-assets`).
3. **The curve operations do not run.** The description lists `get_curves`,
   `add_curve`, `remove_curve`, `set_curve_keys`; `Execute` never dispatches
   them (`MCPTool_AnimEdit.cpp:72-117`): each returns
   `Unknown operation: '<op>'. Valid: …`. Sequence curves are out of reach
   here; montage curves: skill `ue-anim-montage`.
4. **`replace_skeleton` checks nothing.** It only sets the sequence's
   skeleton. Use it for the same rig under another skeleton asset; use
   `retarget` (skill `ue-retarget`) for another rig. Verify with
   `inspect_track` before and after.
5. **`adjust_track` adds to what is there**, every call. `scale_override` is
   an absolute scale: send all three components, as an object; a missing one
   or a bare number writes 0.
6. **`set_additive_type` writes all four settings every call**: an omitted
   `base_pose_type` becomes `RefPose`, an omitted `ref_pose_seq` is cleared,
   `ref_frame_index` becomes 0. `base_pose_type: "None"` with an additive type
   leaves an invalid additive (engine `IsValidAdditive`).
7. **Batch calls hide per-asset reasons when all fail.** A partial failure is
   a success: read `fail_count` and `results[].error`.
8. **`transform_vertices` and `set_ref_pose` re-apply on every call** and
   change a mesh every placed actor uses; `rename_bone` renames only the mesh
   at `mesh_path`.

## Operations

| Operation | Required | Optional | Writes |
|---|---|---|---|
| `inspect_track` | `asset_path`, `bone_name` | `sample_frames` (`[0, -1]`) | nothing |
| `adjust_track` | `asset_paths`, `bone_name`, one of `location_offset`, `rotation_offset`, `scale_override` | `save` | each sequence |
| `resample` | `asset_paths`, `target_fps` | `save` | each sequence |
| `extract_range` | `asset_path`, `start_frame`, `end_frame` | `dest_path`, `new_name`, `save` | a new sequence |
| `set_additive_type` | `asset_path`, `additive_anim_type` | `base_pose_type` (`RefPose`), `ref_pose_seq`, `ref_frame_index` (0), `save` | the sequence |
| `replace_skeleton` | `asset_paths`, `skeleton_path` | `save` | each sequence |
| `inspect_mesh` | `mesh_path` | — | nothing |
| `sync_mesh_bones` | `mesh_path` | `bone_names`, `save` | the mesh |
| `rename_bone` | `mesh_path`, `old_name`, `new_name` | `save` | the mesh, and in `full` mode its skeleton |
| `set_ref_pose` | `mesh_path`, `bone_name`, `position` or `rotation` | `retransform_vertices` (`true`), `save` | the mesh, or a skeleton |
| `transform_vertices` | `mesh_path`, one of `rotation`, `scale`, `translation` | `save` | the mesh |

- `operation` is lowercased; `get_info`, `get_anim_info`, `inspect` and
  `info` run `inspect_track` (which still needs `bone_name`). An unknown one
  returns `Unknown operation: '<op>'. Valid: <list>`, plus a ` Did you mean`
  hint when close.
- **`asset_paths` is a JSON array of strings**; `asset_path` does not stand
  in for it. `blueprint_path` and `path` are aliases of `asset_path` only.
- Vectors are `{x, y, z}`, rotations `{pitch, yaw, roll}`; a component you
  leave out is 0, except in `transform_vertices` `scale` (1; a bare number is
  uniform). Frame numbers and `target_fps` are truncated to integers.
- Frames are key indices: 0 to `total_frames - 1` as `inspect_track` reports;
  `-1` is the last.

## Paths, saving, results — the short rules

- **No path checks and no `/Game/` fallback**: paths go to the engine loader
  as given, `/Engine/` content included, so nothing stops a write to engine
  content. The tool's examples use package paths (`/Game/Anims/Walk`);
  `asset_search` returns object paths (`/Game/Anims/Walk.Walk`). If one form
  fails to load, try the other. Sequence operations load only
  `UAnimSequence` (`Failed to load anim sequence: <path>` or
  `Asset is not an AnimSequence: <path>`), mesh operations `USkeletalMesh`;
  static meshes are not supported. Section `reference` has the details.
- **With `"save": false`**, some writes still mark the package dirty and some
  do not; save with `asset save_asset` by path (section `reference`).
- **Success** is the message, a blank line, then JSON; through the task queue
  the tool's fields sit under `data` (skill `ue-tasks`). **Failure** is
  `Error: <message>` only. A 300 s bridge timeout ends the wait, not the work:
  read the state back before you retry, since `adjust_track` and
  `transform_vertices` would apply twice. Large batches and vertex edits can
  hold the editor long enough to need the `ue-tasks` pattern.

## The edit loop

1. **Find** the assets (`asset_search`, skill `ue-assets`).
2. **Read**: `inspect_track` / `inspect_mesh`, and `retarget` `inspect_anim`
   for length and root motion (skill `ue-retarget`).
3. **Write** with `"save": false`, one asset first, then the batch.
4. **Read again** with the same call and compare numbers.
5. **Save** (`asset save_all` or `save_asset`) and check `is_dirty`.

## Background in the plugin

- `Resources/domains/assets.md`, "Frame Rate": `anim_edit resample` changes
  key data, not the rate; reimport with a custom sample rate instead.
- `Resources/domains/tooling.md` lists `anim_edit` as
  "Inspect + adjust/resample/replace_skeleton".
- `unreal_get_ue_context` with `category: "animation"` serves Animation
  Blueprint notes, not `anim_edit` notes; with `INJECT_CONTEXT=true` that
  context is appended to every successful `anim_edit` result (skill
  `ue-context`, section `injection`).

## See also

- `ue-retarget`: `inspect_anim`, `set_root_motion`, `inspect_skeleton`,
  `compare_bones`, `diagnose_anim`, and retargeting between rigs.
- `ue-assets`: search, save, duplicate, delete, reimport.
- `ue-anim-montage`, `ue-blend-space`, `ue-anim-blueprint`: where sequences
  are used.
- `ue-tasks`: following up a call that timed out.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
