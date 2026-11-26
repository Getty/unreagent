# retarget: skeletons and IK Rigs

Section of skill `ue-retarget`. The skill body has the recipes, the
operations table, path rules, saving, and what a success does not prove.

None of the writes here saves. `create_ik_rig`, `add_chain` and `remove_chain`
mark the package dirty; the code of `add_bone` does not (see there). Save with
`retarget` `save`.

## inspect_skeleton

`asset_path` is a Skeleton, or a skeletal mesh whose skeleton is read.

Returns `skeleton_name`, `skeleton_path`, `bone_count`, `bones[]`,
`socket_count`, `sockets[]` and `summary` (the bone tree as indented text).

- A bone has `index`, `name`, `parent_index`, `parent_name` and
  `ref_location` (`x`, `y`, `z`). The root has `parent_index: -1` and
  `parent_name: "ROOT"`. Rotations are not included: use `inspect_ref_pose`.
- A socket has `name`, `bone_name`, `source`, `relative_location`,
  `relative_rotation` (`pitch`, `yaw`, `roll`) and `relative_scale`. `source`
  is `skeleton` for the skeleton's sockets and `mesh` for mesh-only sockets,
  which are listed only when `asset_path` is a mesh.

The bones are always the skeleton asset's, also when you pass a mesh. A mesh
can have fewer bones than its skeleton; `anim_edit` `inspect_mesh` reads the
mesh's own (skill `ue-anim-sequence`, section `meshes`).

`bone_count` and `bones[]` come from the skeleton's reference skeleton as a
whole (`GetNum`), which in the engine includes virtual bones (unverified);
`inspect_ref_pose` on a mesh lists raw bones only. Indices therefore need not
line up between the two results: match bones by name.

## inspect_ref_pose

`asset_path` is tried as a skeletal mesh first, then as a Skeleton.

Returns `asset`, `path`, `bone_count`, `source` (`skeletal_mesh` or
`skeleton`) and `bones[]`: `index`, `name`, `parent_index`, `parent_name`,
`position` (`x`, `y`, `z`), `rotation` (`pitch`, `yaw`, `roll`) and
`rotation_quat` (`x`, `y`, `z`, `w`). The transforms are each bone's reference
pose as stored, relative to its parent. With a mesh, they are the mesh's own
bones and bind pose.

- Every bone is returned. The `bones` parameter is not read by this
  operation.
- The success message is the fixed text `Ref pose inspected`.
- Error when neither loads:
  `Not a skeletal mesh or skeleton: <path> — <mesh error> / <skeleton error>`.

## list_skeletons

Lists Skeleton assets under `folder_path` (default `/Game`). The search is
always recursive; `recursive` is not read here.

Returns `count` and `skeletons[]` (`name`, `path`). It never fails: a folder
that does not exist gives `count: 0`. Write the folder without a trailing `/`.
It lists Skeleton assets only, not skeletal meshes; find those with
`asset_search` (skill `ue-assets`).

## add_bone

Adds a bone to a Skeleton asset.

| Parameter | Meaning |
|---|---|
| `skeleton_path` | the Skeleton, or a skeletal mesh whose skeleton is changed |
| `bone_name` | the new bone |
| `parent_bone` | an existing bone |
| `position` | `{x, y, z}`, relative to the parent. A component you leave out is 0 |
| `rotation` | `{x, y, z, w}`, a quaternion. Left out: identity. A component you leave out is 0, `w` is 1. The tool does not normalize it |

The bone gets scale 1. Returns `bone_index`, `total_bones` and the message
`Added bone '<b>' (index <i>) as child of '<p>' (index <j>)`.

Errors: `Bone '<b>' already exists in skeleton`,
`Parent bone '<p>' not found in skeleton`.

- **Only the Skeleton asset is changed.** No skeletal mesh gets the bone, also
  when `skeleton_path` was a mesh. To add it to a mesh, run `anim_edit`
  `sync_mesh_bones` afterwards (skill `ue-anim-sequence`, section `meshes`).
- **The tool's code does not mark the package dirty** and calls no `Modify`.
  It does call the engine's skeleton hierarchy-change handler; whether that
  marks the package dirty is not verified. Do not count on the editor offering
  to save the skeleton, or on `asset` `save_all` picking it up. Save it with
  `retarget` `save` and the skeleton's path.
- The tool has no operation that removes a bone from a skeleton.

## inspect_ik_rig

The path is read from `asset_path`, then from `rig_path`. Without either:
`Missing required parameter: asset_path or rig_path`.

Returns `name`, `path`, `retarget_root`, `chains[]`, `chain_count`,
`goal_count`, `solver_count`, and:

- `skeletal_mesh`, only when the rig has a mesh;
- `goals[]` (names), only when there are goals;
- `warnings[]`, only when there are goals or solvers.

A chain has `name`, `start_bone`, `end_bone`, and `goal` when the chain has an
IK goal.

The warnings read
`PITFALL #15: IK goals present — batch retarget will fail. Remove all goals for pure FK retargeting.`
and the same for solvers. That is the plugin's claim, not tested here, and
the UE 5.8.4 batch code contains no such refusal: it runs the retargeter's
whole op stack, Run IK Rig included. Goals and solvers are what the engine's
IK ops use to plant feet and hands; a rig without them gives a pure FK
retarget. The tool has no operation that removes a goal or a solver; a rig
made by `create_ik_rig` has none.

Error: `Failed to get IK Rig controller`.

## create_ik_rig

Creates an IK Rig asset `name` in `package_path`, assigns the skeletal mesh at
`mesh_path`, sets the retarget root and adds chains. It adds no goals and no
solvers.

- `retarget_root` (optional) is a bone name, normally the pelvis or hips
  (section `concepts`). The result of setting it is not checked, and the
  message repeats the name you sent. In UE 5.8.4 source a bone the mesh does
  not have sets the root to `None` without a failure. Read it back with
  `inspect_ik_rig`. Left out, the tool sets nothing; what a new rig starts
  with is the engine's (not read; check `retarget_root`).
- `chains` (optional) is an array of `{name, start_bone, end_bone}`. An entry
  that is not an object, or lacks one of the three, is skipped. So is a chain
  the engine refuses — in UE 5.8.4 source, one whose start or end bone the
  mesh does not have. Neither is reported. Two entries with the same `name`
  both go in, the second renamed with a numeric suffix (see `add_chain`).

Returns `asset_path` (an object path) and the message
`Created IK Rig '<name>' with <n> chains, retarget root: <root>`. **`<n>` is
the only sign of a dropped chain.**

Errors:

- `Failed to load skeletal mesh: <path>` and the like: checked first, nothing
  is created.
- `Failed to create IK Rig: <package_path>/<name>`. What the engine's factory
  does when an asset of that name already exists is not visible in the tool's
  code. Check with `asset_search` first.
- `Failed to get controller for new IK Rig`,
  `Failed to set skeletal mesh: <path>`. **The new asset already exists in
  memory when these are returned.**

The rig is marked dirty and not saved.

## add_chain

Adds a retarget chain `chain_name` from `start_bone` to `end_bone` to the rig
at `rig_path`. The chain gets no IK goal.

Message: `Added chain '<name>' (<start> -> <end>)`, where `<name>` is the name
the engine stored. In UE 5.8.4 source it differs from the one you sent when
the rig already has a chain of that name: the engine appends a number (FName
numbering, shown like `Spine_0`). Such a chain pairs with nothing of that name
on the other rig. Compare the name; on a mismatch, `remove_chain` both and
`add_chain` once.

Error: `Failed to add chain '<name>' (<start> -> <end>)`, with no reason. In
UE 5.8.4 source the engine refuses a start or end bone the rig's skeleton does
not have; check the names against `inspect_skeleton` of the rig's mesh
(`inspect_ik_rig` `skeletal_mesh`). Whether it also refuses an end bone that is
not below the start bone was not found in that code (unverified).

## remove_chain

Removes the chain `chain_name` from the rig at `rig_path`; the name is
compared as an FName, which ignores case. Message `Removed chain '<name>'`;
error
`Chain '<name>' not found or could not be removed`.

To rename a chain or change its bones, remove it and add it again. A
retargeter that uses the rig is not touched by either call: run `setup_ops` on
it (section `retargeter`).
