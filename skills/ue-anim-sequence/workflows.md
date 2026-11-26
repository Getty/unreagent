# anim_edit: workflows

Recipes built only from operations read in the toolkit source; none has been
run against a live editor. Every write saves by default: while you try
things, pass `"save": false`, or `asset duplicate` the asset first (skill
`ue-assets`).

## 0. Read before you write

| Question | Call |
|---|---|
| length, key count, root motion flag, skeleton | `retarget` `inspect_anim` `{"asset_path": …}` → `length`, `num_frames`, `rate_scale`, `root_motion`, `force_root_lock`, `skeleton`, `bone_count` (skill `ue-retarget`) |
| where a bone is on given frames | `anim_edit` `inspect_track` `{"asset_path", "bone_name", "sample_frames": [0, -1]}` |
| the mesh's bones, bind pose, skin weights | `anim_edit` `inspect_mesh` `{"mesh_path"}` (large result; see the crash note in section `meshes`) |
| pops, flips, root motion health | `retarget` `diagnose_anim` (skill `ue-retarget`) |
| which sequences exist | `asset_search` `class_filter: "AnimSequence"` (skill `ue-assets`) or `retarget` `find_anims` |

## 1. Animations moved to another skeleton asset of the same rig

Typical case: a Fab or marketplace pack imported with its own copy of the
UE mannequin skeleton, while your character uses the project's skeleton.

1. Confirm the rigs match: `retarget` `inspect_skeleton` on both skeletons
   (skill `ue-retarget`), same bone names and parents. If they differ, stop:
   this is a retarget, not a swap.
2. Record a baseline: `inspect_track` on `root` and one hand bone of a pack
   sequence, frames `[0, -1]`.
3. `replace_skeleton` `{"asset_paths": [ … ], "skeleton_path": "<project skeleton>", "save": false}`.
   Read `fail_count` and each entry's `error`.
4. Re-run the same `inspect_track` calls: the values should match the
   baseline. Then save (`asset save_all`).
5. Montages and blend spaces built on the old skeleton keep their own
   skeleton reference; rebuild or swap them separately (skills
   `ue-anim-montage`, `ue-blend-space`).

## 2. Cut one clip out of a long take

1. `inspect_track` with `sample_frames: [-1]` gives `total_frames`; frames
   are key indices, 0 to `total_frames - 1`.
2. `extract_range` `{"asset_path", "start_frame": 30, "end_frame": 75, "new_name": "A_Swing"}`
   (inclusive both ends; `end_frame: -1` means the last). The source is not
   changed; the copy lands next to it unless `dest_path` is given.
3. Check `extracted_frame_count` and `new_asset_path`, then `retarget`
   `inspect_anim` on the copy for its `length`. Use the copy as a montage
   segment (skill `ue-anim-montage`).
4. If the call fails after the copy (`Trim tail failed`, `Trim head failed`),
   delete the leftover copy (`asset delete`).

## 3. Character floats or sinks in one animation

1. `inspect_track` on the root (or pelvis) at `[0, -1]` in a good and the bad
   sequence; the difference in `position.z` is the offset.
2. `adjust_track` `{"asset_paths": ["<bad>"], "bone_name": "pelvis", "location_offset": {"x": 0, "y": 0, "z": -3.5}, "save": false}`.
   It adds to every key: a second call adds again.
3. `inspect_track` again; save when right.

Apply the same to a batch of sequences with one call when they share the
error; read `results` per path.

## 4. An additive layer or aim offset poses

- **Additive layer** (breathing, lean, hit flinch played on top of
  locomotion): `set_additive_type` with `additive_anim_type: "LocalSpaceBase"`
  and `base_pose_type: "LocalAnimFrame"`, `ref_frame_index: 0` (difference
  to its own first frame) or `RefPose`.
- **Aim offset poses**: `RotationOffsetMeshSpace`, `AnimFrame`, the base
  animation as `ref_pose_seq`, `ref_frame_index: 0` — Epic's aim-offset page
  (section `sequences` has the call and the link).
- Each call writes all four settings: pass every one you want. Check `new`
  in the result. Then build the blend space or aim offset (skill
  `ue-blend-space`).

## 5. A mesh imported at the wrong scale or axis

Decision first: **vertices or bones?**

| Symptom | Fix |
|---|---|
| mesh 100× too big or small, or lying on its side, while the skeleton looks right | `transform_vertices` (`scale` or `rotation`); touches no bone |
| one bone's rest pose is wrong (a twisted wrist in the bind pose) | `set_ref_pose` on the mesh, `retransform_vertices` true, so the skin keeps its shape |
| animation plays with a bone at scale 0 or negative | `adjust_track` `scale_override` `{"x": 1, "y": 1, "z": 1}` on that bone |
| the whole rig is wrong (bones and mesh together) | reimport with the right import settings (skill `ue-assets`, section `import`); these tools cannot rescale a skeleton |

Every `transform_vertices` call applies again on top of the last; read
`inspect_mesh`'s `sample_vertices` before and after.

## 6. A mesh lacks bones its skeleton has

Used for Copy Pose From Mesh setups (the tool's own description): the
follower mesh must contain the leader's bones.

1. `inspect_mesh` the mesh: `num_bones`. `retarget` `inspect_skeleton` the
   skeleton for the full list.
2. `sync_mesh_bones` `{"mesh_path", "bone_names": [ … ]}` or without
   `bone_names` for all missing. Compare `added_bones` with what you asked;
   a refused bone is only in the editor log (`get_output_log`, skill
   `ue-pie-testing`).
3. The new bones have no skin weights; they move no vertex.

## 7. Bone names that do not match

`rename_bone` renames in the skeleton and the mesh at `mesh_path`
(`mode: "full"`), or only in the mesh when the skeleton already has the new
name (`mode: "mesh_only"`). Run it once per mesh on that skeleton. Then
`inspect_track` a sequence under the new name: whether animation tracks
follow the rename is the engine's part and unverified here.

## Verify your own result

- Track edits: the same `inspect_track` call before and after; compare the
  numbers, not the success message.
- Length or rate: `retarget` `inspect_anim` (`length`, `num_frames`).
- Saves: `asset` `get_asset_info` and `is_dirty` (skill `ue-assets`); a
  saving call does not report a failed save.
- In game: play it (skill `ue-pie-testing`) or render a frame
  (`capture_viewport`, skill `ue-viewport`).
