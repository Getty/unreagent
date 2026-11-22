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

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

The tool takes an `operation` string and lowercases it before dispatch.
`get_info`, `get_anim_info`, `inspect` and `info` run `inspect_track`, which
needs a `bone_name`. An unknown operation returns
`Unknown operation: '<op>'. Valid: <list>`, plus ` Did you mean '<x>'?` when a
valid name is close.

This skill has two sections:

| Section | Covers |
|---|---|
| `sequences` | `inspect_track`, `adjust_track`, `resample`, `extract_range`, `set_additive_type`, `replace_skeleton` |
| `meshes` | `inspect_mesh`, `sync_mesh_bones`, `rename_bone`, `set_ref_pose`, `transform_vertices` |

## Curve operations do not run

The tool's description and parameter list name `get_curves`, `add_curve`,
`remove_curve` and `set_curve_keys`, with the parameters `curve_name` and
`keys`. The tool does not dispatch them: each one returns
`Unknown operation: '<op>'. Valid: …`. An AnimSequence's float curves cannot be
read or edited through `anim_edit`. Montage curves belong to another tool
(skill `ue-anim-montage`).

## Operations

| Operation | Required | Optional | Writes |
|---|---|---|---|
| `inspect_track` | `asset_path`, `bone_name` | `sample_frames` | nothing |
| `adjust_track` | `asset_paths`, `bone_name`, one of `location_offset`, `rotation_offset`, `scale_override` | `save` | each sequence |
| `resample` | `asset_paths`, `target_fps` | `save` | each sequence |
| `extract_range` | `asset_path`, `start_frame`, `end_frame` | `dest_path`, `new_name`, `save` | a new sequence |
| `set_additive_type` | `asset_path`, `additive_anim_type` | `base_pose_type`, `ref_pose_seq`, `ref_frame_index`, `save` | the sequence |
| `replace_skeleton` | `asset_paths`, `skeleton_path` | `save` | each sequence |
| `inspect_mesh` | `mesh_path` | — | nothing |
| `sync_mesh_bones` | `mesh_path` | `bone_names`, `save` | the mesh |
| `rename_bone` | `mesh_path`, `old_name`, `new_name` | `save` | the mesh, and in one mode its skeleton |
| `set_ref_pose` | `mesh_path`, `bone_name`, `position` or `rotation` | `retransform_vertices`, `save` | the mesh, or a skeleton |
| `transform_vertices` | `mesh_path`, one of `rotation`, `scale`, `translation` | `save` | the mesh |

A missing or empty required string returns
`Missing required parameter: <name>`.

- **`asset_paths` is a JSON array of strings.** A single `asset_path` does not
  take its place in `adjust_track`, `resample` and `replace_skeleton`. A missing
  or empty array returns
  `Missing required parameter: asset_paths (non-empty array)`. Entries the tool
  cannot read as a non-empty string are skipped; when none is left, the call
  returns `asset_paths contains no valid paths`. Whether the engine's JSON
  classes read a number as a string is not verified, so send strings.
- **Vectors and rotations are objects**: `{x, y, z}` and
  `{pitch, yaw, roll}`. A component you leave out is 0. That matters for
  `scale_override` (section `sequences`) and `set_ref_pose` (section `meshes`).
  The exception is `transform_vertices` `scale`: a component you leave out is
  1 there, and a bare number is a uniform scale (section `meshes`).
- **Numbers that the code turns into integers are truncated**: `target_fps`,
  `start_frame`, `end_frame`, `ref_frame_index` and the `sample_frames` entries.

## Paths and loading

- **No path checks.** Unlike the Blueprint tools (skill `ue-blueprints`),
  `anim_edit` passes paths to the engine's loader unchanged: there is no
  `/Game/` fallback, and `/Engine/` paths are not rejected. Nothing in the tool
  stops a write to engine content.
- **Aliases.** `blueprint_path` and `path` are accepted for `asset_path`.
  `asset_paths`, `mesh_path`, `skeleton_path` and `ref_pose_seq` have none.
- **Path form.** The tool's own examples use package paths, such as
  `/Game/Anims/Walk`. `asset_search` returns object paths, such as
  `/Game/Anims/Walk.Walk`. When one form does not load, try the other.
- **Sequences only.** The sequence operations load a `UAnimSequence`. Montages,
  blend spaces and other animation assets are not sequences. A path that does
  not load as one returns `Failed to load anim sequence: <path>`
  (`set_additive_type`: `Failed to load AnimSequence: <path>`), or
  `Asset is not an AnimSequence: <path>`. Which of the two an asset of another
  class gives is up to the engine's loader.
- **Meshes.** The mesh operations load a `USkeletalMesh`:
  `Failed to load USkeletalMesh: <path>`. `set_ref_pose` also takes a skeleton.
  Static meshes are not supported.
- **A load failure does not prove the asset is missing.** Check with
  `asset_search`, `class_filter: "AnimSequence"` or `"SkeletalMesh"` (skill
  `ue-assets`).

## Saving and undo

- **Every write saves by default.** `save` defaults to `true`, and the tool
  then calls the engine's asset save in the same call. There is no dry run.
  Read first (`inspect_track`, `inspect_mesh`), and pass `"save": false` to
  keep a change in the editor only.
- **A failed save is not reported.** The tool ignores the result of its save
  call, so a save that did not happen still returns success. `asset`
  `get_asset_info` reports `is_dirty` (skill `ue-assets`); a package that is
  still dirty after a saving call was most likely not written.
- **With `"save": false`**, `adjust_track`, `resample`, `extract_range`,
  `set_additive_type`, `replace_skeleton` and `transform_vertices` still mark
  the package dirty, so `asset save_all` picks them up later.
  `sync_mesh_bones`, `rename_bone` and `set_ref_pose` mark it dirty only when
  they save. Whether the engine marks it dirty on its own is not verified, so
  save those with `asset save_asset` and the asset's path. After a full
  `rename_bone`, that means the mesh and its skeleton.
- **No undo.** `adjust_track` and `resample` call `Modify` and bracket the edit
  in the animation's data controller; whether that leaves an undo entry is up
  to the engine. The other writes open no transaction. Treat every write as
  final, and, with saving on by default, as already on disk. To keep the old
  state, copy the asset first with `asset duplicate` (skill `ue-assets`).

## Batch results

`adjust_track`, `resample` and `replace_skeleton` run once per entry of
`asset_paths`.

- **The call succeeds when at least one asset succeeded.** The data has
  `success_count`, `fail_count`, `total` and `results`: one entry per path,
  with `success`, and `error` when that asset failed. Every entry, a failed
  one too, has its path: under `asset_path` for `adjust_track` and
  `resample`, under `path` for `replace_skeleton`. A succeeded `adjust_track`
  or `resample` entry has a `message`; a succeeded `replace_skeleton` entry
  has `old_skeleton` and `new_skeleton` instead (section `sequences`). A
  partial failure is still a success: read `fail_count`.
- **When every asset failed, the call is an error**, for example
  `Batch adjust: 0 succeeded, 2 failed out of 2`. The reasons per asset do not
  reach you, and that holds for a single path too. `inspect_track` on the same
  asset and bone returns the load and track errors directly.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work: a `resample` over many sequences or a `set_ref_pose` on a large
  mesh may still finish. Read the state back before you retry (skill
  `ue-tasks`). A retried `adjust_track` or `transform_vertices` applies its
  change a second time. A batch over many sequences (`adjust_track`,
  `resample`, `replace_skeleton`) and a vertex edit on a large mesh
  (`set_ref_pose`, `transform_vertices`, each over every LOD) run as one call
  and can hold the editor for more than 30 s, and a request that waits 30 s
  behind it may crash the editor; run such a call as skill `ue-tasks`, "A 30 s
  timeout can crash the editor", describes.
- **Misspelt parameters.** Calls the bridge queues do not warn about a
  parameter the tool does not declare (skill `ue-tasks`). The curve parameters
  are declared, so they draw no warning either.

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

## Background in the plugin

- The plugin's `Resources/domains/assets.md` notes, under "Frame Rate", that
  `anim_edit resample` changes key data and not the frame rate, and recommends
  a reimport with a custom sample rate for a real rate change. The code agrees
  (section `sequences`). `asset_import` has that `custom_sample_rate`
  parameter (skill `ue-assets`).
- `Resources/domains/tooling.md` lists `anim_edit` as
  "Inspect + adjust/resample/replace_skeleton".
- `unreal_get_ue_context` with `category: "animation"` serves notes on
  Animation Blueprints, not on `anim_edit`. With `INJECT_CONTEXT=true` in the
  bridge's environment, that context is appended to every successful
  `anim_edit` result (skill `ue-context`).

## See also

- `ue-assets`: `asset_search` to find sequences and meshes, `save_asset` and
  `save_all`, `duplicate` for a backup, `asset_import` for a reimport.
- `ue-anim-montage`: montages, including their curves.
- `ue-blend-space`: blend spaces built from these sequences.
- `ue-anim-blueprint`: playing sequences in an Animation Blueprint.
- `ue-tasks`: following up a call that timed out.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
