# UE blend spaces: workflows

Each workflow ends with a verification against `inspect` and a `save`. Paths
are examples; use the object paths `list`, `asset_search` or `create` report.
The `operations` arrays use the batch shape (section `batch`). Load section
`ue-facts` for why the order matters.

## 0. Gather before you build

1. Find the skeleton and the clips: `asset_search` with
   `class_filter: "Skeleton"` and `"AnimSequence"` under the pack's folder
   (skill `ue-assets`). Every clip must use the same skeleton as the blend
   space (or a compatible one); a pack on another skeleton is retargeted
   first (skill `ue-retarget`).
2. Decide the axis ranges from the game, not from the clips: the Speed axis
   maximum is the character's top ground speed. `CharacterMovement`'s
   `MaxWalkSpeed` defaults to 600 (UE 5.8.4 source); read the real value of a
   placed character with the `character` tool (skill `ue-character`).
3. Note each clip's root speed if the pack documents it; place a cycle at the
   speed it was authored for, or correct with `rate_scale`.

## 1. 1D speed blend (idle → walk → run)

```json
{"operation":"create","package_path":"/Game/Anims","name":"BS_Speed","skeleton_path":"/Game/Characters/SK_Hero_Skeleton.SK_Hero_Skeleton","type":"BlendSpace1D"}
```

```json
{"operation":"batch","asset_path":"/Game/Anims/BS_Speed.BS_Speed","operations":[
  {"op":"set_axis","axis_index":0,"axis_name":"Speed","min":0,"max":600,"grid_divisions":4,
   "interp_type":"SpringDamper","interp_time":0.15,"damping_ratio":1.0},
  {"op":"add_sample","animation_path":"/Game/Anims/Idle.Idle","x":0},
  {"op":"add_sample","animation_path":"/Game/Anims/Walk_Fwd.Walk_Fwd","x":200},
  {"op":"add_sample","animation_path":"/Game/Anims/Run_Fwd.Run_Fwd","x":600}
]}
```

Verify: one axis `Speed` 0–600 with 4 divisions, `sample_count` 3, positions
0/200/600, `geometry.segments` > 0, `anim_length` > 0. Then `save`.

## 2. 2D locomotion: Speed × Direction

Axis 0 Speed 0–600, axis 1 Direction -180–180 (degrees, wrapping). Samples:
idle along speed 0, walk ring at the walk speed, run ring at the run speed;
forward at 0, the two strafes at ±90, backward at both -180 and 180 (the same
clip at both ends, since the axis wraps).

```json
{"operation":"create","package_path":"/Game/Anims","name":"BS_Locomotion","skeleton_path":"/Game/Characters/SK_Hero_Skeleton.SK_Hero_Skeleton","type":"BlendSpace"}
```

```json
{"operation":"batch","asset_path":"/Game/Anims/BS_Locomotion.BS_Locomotion","operations":[
  {"op":"set_axis","axis_index":0,"axis_name":"Speed","min":0,"max":600,"grid_divisions":4,
   "interp_type":"SpringDamper","interp_time":0.15,"damping_ratio":1.0},
  {"op":"set_axis","axis_index":1,"axis_name":"Direction","min":-180,"max":180,"grid_divisions":8,
   "wrap_input":true,"interp_type":"SpringDamper","interp_time":0.1,"damping_ratio":1.0,"max_speed":720},
  {"op":"add_sample","animation_path":"/Game/Anims/Idle.Idle","x":0,"y":-180},
  {"op":"add_sample","animation_path":"/Game/Anims/Idle.Idle","x":0,"y":-90},
  {"op":"add_sample","animation_path":"/Game/Anims/Idle.Idle","x":0,"y":0},
  {"op":"add_sample","animation_path":"/Game/Anims/Idle.Idle","x":0,"y":90},
  {"op":"add_sample","animation_path":"/Game/Anims/Idle.Idle","x":0,"y":180},
  {"op":"add_sample","animation_path":"/Game/Anims/Walk_Bwd.Walk_Bwd","x":200,"y":-180},
  {"op":"add_sample","animation_path":"/Game/Anims/Walk_Left.Walk_Left","x":200,"y":-90},
  {"op":"add_sample","animation_path":"/Game/Anims/Walk_Fwd.Walk_Fwd","x":200,"y":0},
  {"op":"add_sample","animation_path":"/Game/Anims/Walk_Right.Walk_Right","x":200,"y":90},
  {"op":"add_sample","animation_path":"/Game/Anims/Walk_Bwd.Walk_Bwd","x":200,"y":180},
  {"op":"add_sample","animation_path":"/Game/Anims/Run_Bwd.Run_Bwd","x":600,"y":-180},
  {"op":"add_sample","animation_path":"/Game/Anims/Run_Left.Run_Left","x":600,"y":-90},
  {"op":"add_sample","animation_path":"/Game/Anims/Run_Fwd.Run_Fwd","x":600,"y":0},
  {"op":"add_sample","animation_path":"/Game/Anims/Run_Right.Run_Right","x":600,"y":90},
  {"op":"add_sample","animation_path":"/Game/Anims/Run_Bwd.Run_Bwd","x":600,"y":180}
]}
```

- Whether -90 is left or right depends on how the Animation Blueprint
  computes Direction; match the strafe clips to that formula (which sign the
  engine's direction helpers use is not checked here — unverified).
- Diagonal clips (forward-left at -45 and so on) go on the same rings when the
  pack has them.
- Positions on a ring that has no clip simply blend between neighbours.

Verify: axes as sent (Speed 0–600/4, Direction -180–180/8, Direction
`wrap_input` true); `sample_count` 15; no `animation: "None"`;
`geometry.triangles` > 0; `anim_length` > 0. Then `save`.

When the batch returns `Error: Batch: <n> OK, <m> errors, <t> total`: `inspect`,
find the positions that are missing, and send those as single `add_sample`
calls to see each error.

## 3. Aim offset (yaw × pitch)

1. Prepare the pose sequences: each one **Additive Anim Type Mesh Space**,
   **Base Pose Type Selected animation frame**, the shared base pose
   animation, **Ref Frame Index 0** (Epic, Aim Offset; skill
   `ue-anim-sequence`). A non-additive clip is refused with
   `AddSample failed …`.
2. Create and fill:

```json
{"operation":"create","package_path":"/Game/Anims","name":"AO_Aim","skeleton_path":"/Game/Characters/SK_Hero_Skeleton.SK_Hero_Skeleton","type":"AimOffsetBlendSpace"}
```

```json
{"operation":"batch","asset_path":"/Game/Anims/AO_Aim.AO_Aim","operations":[
  {"op":"set_axis","axis_index":0,"axis_name":"Yaw","min":-90,"max":90,"grid_divisions":4},
  {"op":"set_axis","axis_index":1,"axis_name":"Pitch","min":-90,"max":90,"grid_divisions":2},
  {"op":"add_sample","animation_path":"/Game/Anims/Aim_CC.Aim_CC","x":0,"y":0},
  {"op":"add_sample","animation_path":"/Game/Anims/Aim_CU.Aim_CU","x":0,"y":90},
  {"op":"add_sample","animation_path":"/Game/Anims/Aim_CD.Aim_CD","x":0,"y":-90},
  {"op":"add_sample","animation_path":"/Game/Anims/Aim_LC.Aim_LC","x":-90,"y":0},
  {"op":"add_sample","animation_path":"/Game/Anims/Aim_LU.Aim_LU","x":-90,"y":90},
  {"op":"add_sample","animation_path":"/Game/Anims/Aim_LD.Aim_LD","x":-90,"y":-90},
  {"op":"add_sample","animation_path":"/Game/Anims/Aim_RC.Aim_RC","x":90,"y":0},
  {"op":"add_sample","animation_path":"/Game/Anims/Aim_RU.Aim_RU","x":90,"y":90},
  {"op":"add_sample","animation_path":"/Game/Anims/Aim_RD.Aim_RD","x":90,"y":-90}
]}
```

3. Verify: `inspect.type` reads `BlendSpace` (an aim offset is not named as
   such), 9 samples, `geometry.triangles` > 0. Then `save`.
4. In the Animation Blueprint the Aim Offset node needs a base pose input and
   Yaw/Pitch values (skill `ue-anim-blueprint`).

A 1D pitch-only aim offset (`AimOffsetBlendSpace1D`, axis Pitch, three
samples at -90/0/90) is the simpler choice when the capsule turns for yaw.

## 4. Swap the animations of an existing blend space

For a new pack on the same (or a compatible) skeleton, keep positions and
indices:

1. `inspect`; write down `index` → `animation` → `position`.
2. One `batch` of `set_sample_animation` ops, one per index, with the matching
   new clip. The tool checks skeleton and additive type per op.
3. `inspect`: every `animation` is the new path. `save`.

For a pack on a different skeleton, retarget first (skill `ue-retarget`), or
create a new blend space on the new skeleton — a blend space's skeleton is
fixed at `create`.

## 5. Repair a blend space that misbehaves

1. `inspect` and compare against what it should be:
   - an axis with odd range and many divisions → it grew; `set_axis` it back
     only after every sample is inside the intended range (move them first),
     or the outside ones turn invalid;
   - `animation: "None"` → `set_sample_animation` or `remove_sample`;
   - `anim_length` 0 with samples present → no sample is valid (wrong
     additive type, out of range, missing clip);
   - an `interpolation.type` that ignored your setting → you sent a name not in
     the six.
2. Fix one thing per call when you need the error text; `batch` when you know
   the fix.
3. `inspect` again, then `save`.
