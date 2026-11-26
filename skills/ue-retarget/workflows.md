# retarget: end-to-end workflows

Section of skill `ue-retarget`. The skill body has the short recipe and the
operations table; section `concepts` explains chains, the retarget root, the
fuzzy auto-map and the FK modes these workflows rely on.

Every step is one `retarget` call unless another tool is named. Nothing here
has been run against an editor; the calls follow the tool's C++ source.

## A: An animation pack onto UE5 Manny/Quinn (Mixamo, Fab, marketplace)

The common job: a pack comes with its own skeleton (or as loose FBX files),
and the project's characters use the UE5 mannequin skeleton.

### 1. Find what you have

1. `list_skeletons` `{"folder_path": "/Game"}` — the pack's skeleton and the
   mannequin's (`SK_Mannequin` in the templates; the name and folder vary, so
   take them from this list, not from memory).
2. Skeletal meshes for both sides: `asset_search` (skill `ue-assets`), class
   `SkeletalMesh`. `batch_retarget` and `create_ik_rig` need meshes, not
   skeletons.
3. **Already on the mannequin skeleton?** `inspect_anim` on one pack
   animation: when `skeleton` is the mannequin's, there is nothing to
   retarget. Fab packs made for UE5 often ship that way (unverified, check
   each pack).
4. `inspect_skeleton` on both meshes. Write down the bone names for pelvis,
   spine start and end, neck, head, clavicles, upper arm, hand, thigh, foot or
   ball, and the finger bones. Use `summary` for a quick tree.
5. `inspect_ref_pose` on both meshes, the upper-arm bones: when one character
   stands in an A-pose and the other in a T-pose, expect to need a retarget
   pose (section `concepts`, "Retarget pose"), which this tool cannot make.

### 2. Loose FBX files: import onto the pack's own skeleton first

Retargeting runs from an AnimSequence on the source skeleton. FBX files that
are not in the project yet go in first, onto the skeleton they were authored
for (section `import`):

- `batch_import_fbx` `{"fbx_directory": "D:/Packs/Mixamo", "dest_path": "/Game/Mixamo/Anims", "skeleton_path": "<pack skeleton or mesh>"}`.
- No pack skeleton in the project yet: import one file with its mesh first,
  `import_fbx` with `"import_mesh": true` and a skeleton — or, for a mesh that
  must create its own skeleton, `asset_import` (skill `ue-assets`).
- Mixamo: download the character once "With Skin" for the mesh, the
  animations "Without Skin". Locomotion exported with "In Place" has no
  forward motion to turn into root motion (Mixamo download options; behaviour
  in UE unverified).

### 3. Two IK Rigs with identical chain names

Use the same chain names on both rigs; then the engine's fuzzy auto-map pairs
them exactly (section `concepts`). Bone names below are examples — use the
ones you wrote down in step 1.

```json
{"operation": "create_ik_rig", "package_path": "/Game/Retarget", "name": "IK_Mixamo",
 "mesh_path": "<pack mesh>", "retarget_root": "<hips bone>",
 "chains": [
  {"name": "Spine", "start_bone": "<Spine>", "end_bone": "<Spine2>"},
  {"name": "Neck", "start_bone": "<Neck>", "end_bone": "<Neck>"},
  {"name": "Head", "start_bone": "<Head>", "end_bone": "<Head>"},
  {"name": "LeftClavicle", "start_bone": "<LeftShoulder>", "end_bone": "<LeftShoulder>"},
  {"name": "LeftArm", "start_bone": "<LeftArm>", "end_bone": "<LeftHand>"},
  {"name": "LeftLeg", "start_bone": "<LeftUpLeg>", "end_bone": "<LeftToeBase>"},
  "… right side and fingers the same way"
 ]}
```

Then the same call for `IK_Mannequin` with the mannequin mesh, `pelvis` as
the root and the mannequin's bones. The mannequin template may already ship
an IK Rig (`asset_search` for class `IKRigDefinition`); reuse it only if its
chain names match yours — `inspect_ik_rig` shows them — or name your source
chains after it.

Check each rig, every time:

- `inspect_ik_rig`: `chain_count` equals the chains you sent;
  `retarget_root` is the bone you named, not `None`; no `warnings`.
- A chain missing: `add_chain` it alone, which returns an error naming the
  chain when a bone does not exist.

### 4. Retargeter

1. `create_retargeter`
   `{"package_path": "/Game/Retarget", "name": "RTG_Mixamo_To_Manny", "source_rig_path": "<IK_Mixamo>", "target_rig_path": "<IK_Mannequin>"}`.
2. `inspect_retargeter`. Pass when:
   - `source_rig` and `target_rig` are your two rigs;
   - in the op whose `type` is the FK Chains op, every `chain_mapping` entry
     has `source` equal to `target` (same names on both rigs) — a pair with
     different names was guessed by the fuzzy match;
   - `fk_chains`: the pelvis or hips chain has `translation`
     `GloballyScaled`, the others `None`; fingers may want `OneToOne`.
   - `warnings` holding `PITFALL #11` is expected: the default op set always
     contains Pelvis Motion (section `retargeter`, `inspect_retargeter`).
3. Fix modes with `configure_fk`, then `inspect_retargeter` again.
4. `save` both rigs and the retargeter.

### 5. Batch retarget, a few first

1. `find_anims` `{"folder_path": "/Game/Mixamo/Anims", "recursive": true}`.
2. `batch_retarget` with **two or three** animations first — one idle, one
   locomotion cycle, one with large motion — and
   `"auto_root_motion": false`, so the flags stay yours:
   `{"retargeter_path": "<RTG>", "source_mesh_path": "<pack mesh>", "target_mesh_path": "<mannequin mesh>", "anim_paths": ["…"], "prefix": "RTG_", "auto_root_motion": false}`.
3. Take the output paths from `new_assets[].path`. On UE 5.8 they land in
   `/Game` itself (section `engine-versions`); move them with `asset` `rename`
   (skill `ue-assets`) once they are right.
4. Verify them (workflow C). Only when the test set passes, run the rest in
   batches of about 20 paths per call, so no call holds the editor for long
   (skill `ue-tasks`).
5. `save` every new animation, or `asset` `save_all` (they are marked dirty
   by the engine's batch code, UE 5.8.4 source).

### 6. Root motion

Decide per animation, not per pack:

| The source animation | What to do |
|---|---|
| moves the character through space (walk forward, dodge, attack lunge) and the game uses root motion | `set_root_motion` `{"asset_path": "<new anim>", "enable": true}` |
| plays in place (idle, in-place cycles driven by the movement component) | `set_root_motion` `{"enable": false}` — this also turns Force Root Lock **on** |
| moves, but the game moves the capsule itself | leave root motion off, and expect sliding if the root bone moves |

Then `diagnose_anim`: `root_motion_mismatch` issues tell you the flag and
the root bone's movement disagree. A Mixamo source has no root bone of its
own; whether the retargeted copy's `root` bone moves at all depends on the
Root Motion op (section `concepts`). Check with `diagnose_anim`
`root_motion.range_xy` on the **output**: near 0 for a forward walk means no
root motion was produced, and setting the flag will not create it.

## B: One character's animations onto another, same kind of skeleton

Two characters with near-identical skeletons (two UE5 mannequin variants, a
MetaHuman body type and the mannequin) still need IK Rigs and a retargeter
when their proportions differ. When only the skeleton asset differs and the
bones and proportions match, `anim_edit` `replace_skeleton` (skill
`ue-anim-sequence`) re-points a sequence without re-keying it — faster, and
no output copies.

Decision:

- Same bone names, same proportions → `anim_edit` `replace_skeleton`.
- Same bone names, different proportions → retarget (workflow A, steps 3–6;
  chain names can be copied from one rig to the other).
- Different bone names → retarget.
- A few bones are missing on the target skeleton (a weapon bone, an IK bone) →
  `add_bone` to the skeleton, `anim_edit` `sync_mesh_bones` to the mesh, then
  `copy_bone_tracks` from the source animation (section `rigs`,
  `anim-checks`).

## C: Verify a retargeted animation

No result of the tool judges quality. This is the check an agent can run on
its own, cheapest first; stop at the first failure and use section
`failures`.

1. **It exists and has the right skeleton.** `inspect_anim` on the output:
   `skeleton` is the target skeleton, `length` and `num_frames` match the
   source's `inspect_anim`.
2. **It moves.** `sample_bones` on the output at the first, middle and last
   frame for `pelvis`, `hand_l`, `foot_l` (target names). Identical values in
   all three frames for a moving source mean the engine produced no motion:
   the retargeter could not initialize (section `failures`). Confirm with
   `get_output_log` `{"filter": "Retarget"}` (skill `ue-pie-testing`,
   section `log`).
3. **No breakage.** `diagnose_anim` on the output: `bone_pops` and
   `quaternion_flips` should be 0 or explained (section `anim-checks`, how to
   read a flip). Compare with `diagnose_anim` on the source: an issue the
   source already has is not the retarget's fault.
4. **Motion matches the source.** `sample_bones` on source and output at the
   same frames, for the pelvis: the output's height should be the source's
   scaled by the two characters' size ratio (from `inspect_ref_pose`), and
   its forward travel should follow the source's. Rotations of
   `Interpolated` chains are not comparable bone by bone; compare them by
   looking.
5. **Look at it.** `capture_viewport` with `asset_path` and two or three
   `frame` values (skill `ue-viewport`, section `asset-preview`): feet on the
   ground, no arms through the body, hands where the source's are. This is
   the only step that catches a wrong retarget pose.

Report what you checked and what you could not: a pass of steps 1–4 without
step 5 is "numbers look sane", not "the retarget is good".
