# anim_edit: paths, saving, batches and results

The reference behind the skill's short rules. Load it when a path does not
load, before relying on `"save": false`, or when you read a batch result.

## Parameters

A missing or empty required string returns
`Missing required parameter: <name>`.

- **`asset_paths` is a JSON array of strings.** A single `asset_path` does not
  take its place in `adjust_track`, `resample` and `replace_skeleton`. A missing
  or empty array returns
  `Missing required parameter: asset_paths (non-empty array)`. Each entry is
  read with the engine's `FJsonValue::TryGetString`. An empty string, `null`,
  an array or an object is skipped; when nothing is left, the call returns
  `asset_paths contains no valid paths`. **A number or a boolean is not
  skipped**: it is read as its text, `5` or `true`, and goes on as a path that
  names no asset. Send strings. (`FJsonValueNumber::TryGetString`,
  `Engine/Source/Runtime/Json/Private/Dom/JsonValue.cpp`; read in the UE
  5.8.4 source, not run, while the toolkit targets UE 5.6 and 5.7. The
  number reads in the sections rest on the same file and on
  `TJsonValueString` in `Public/Dom/JsonValue.h`.)
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

