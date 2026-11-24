# retarget: animation inspection, diagnosis, bone tracks

Section of skill `ue-retarget`. The skill body has the recipes, the
operations table, path rules, and what a success does not prove.

All operations here load AnimSequences only. `copy_bone_tracks` is the one
that writes.

## inspect_anim

`asset_path` is the animation. Returns `name`, `path`, `length` (seconds),
`num_frames` (the number of sampled keys), `rate_scale`, `root_motion`
(`bEnableRootMotion`), `force_root_lock`, and, when the animation has a
skeleton, `skeleton` (its path) and `bone_count` (the skeleton's bones, not
the animation's tracks). The success message is the fixed text
`Animation inspected`.

It returns no frame rate, no track list, no curves and no notifies. One bone's
keys: `anim_edit` `inspect_track` (skill `ue-anim-sequence`, section
`sequences`).

## compare_bones

Checks, for each name in `bone_names`, whether the bone exists in the
skeleton of the animation at `source_anim_path` and in the skeleton of the
one at `target_anim_path`.

Returns `source_anim`, `target_anim` (asset names) and `bones[]`: `bone`,
`found_in_source`, `found_in_target`.

- **It compares names, not motion.** No transform is sampled and nothing about
  the two animations' poses is compared. The tool's description of
  `sample_times` ("times to sample", default `[0.0, 0.5]`) does not match the
  code: the parameter is parsed and then not used.
- It looks at the skeletons' reference bones, not at the animations' bone
  tracks. A bone that exists in the skeleton but has no track still shows as
  found.
- To compare motion, call `sample_bones` on both animations with the same
  `frames` and `bones` and compare the numbers yourself.

Errors: the load errors of either animation,
`Could not get skeletons from animations`,
`Missing required parameter: bone_names (JSON array)`.

## sample_bones

Returns bone track transforms of one animation at given frames.

| Parameter | Meaning |
|---|---|
| `asset_path` | the animation |
| `frames` | array of 0-based frame indices; numbers are truncated to integers |
| `bones` | optional array of bone names, compared without regard to case. Without it every track is returned |

Returns `animation`, `total_frames`, `total_tracks` and `frames[]`. Each frame
has `frame` and `bones[]`; each bone has `bone`, `position` (`x`, `y`, `z`),
`rotation` (a quaternion: `x`, `y`, `z`, `w`), `scale`, and `bone_index` (the
bone's index in the skeleton; -1 when the skeleton has no such bone) when the
animation has a skeleton.

- **The values are the track's own keys**: each bone relative to its parent,
  as stored. They are not component-space or world positions.
- **A frame below 0 or at or above `total_frames` is left out without an
  error.** Compare the returned `frame` values with what you asked for.
- A name in `bones` that matches no track gives no entry and no error.
- The message is `Sampled <n> frames, <t> tracks`, where `<t>` is the
  animation's total number of tracks, not the number returned.
- **Size.** Without `bones`, the result is every track for every frame you
  list. Pass `bones`, and few frames.

Errors: `Animation has no data model`, `Animation has no bone tracks`,
`Missing required parameter: frames (JSON array of 0-based frame indices)`.

## diagnose_anim

Runs three checks over all keys of one animation. It changes nothing.

| Parameter | Default | Meaning |
|---|---|---|
| `asset_path` | required | the animation |
| `pop_threshold` | `50.0` | a position jump between two consecutive keys above this many cm is a pop |
| `rotation_flip_threshold` | `0.0` | a quaternion dot product between two consecutive keys below this is a flip |

Returns `animation`, `summary`, `thresholds`, `root_motion`, `issues[]` and
the message
`Diagnosis complete: <n> issues (<p> pops, <q> quat flips, <r> root motion mismatches)`.

- `summary`: `total_issues`, `bone_pops`, `quaternion_flips`,
  `root_motion_mismatches`.
- `thresholds`: `pop_threshold_cm`, `rotation_flip_dot_threshold`,
  `root_motion_movement_threshold_cm` (fixed at 5).
- `root_motion`: `root_bone` and `root_motion_flag`, and, when the root bone
  has a track with keys, `total_displacement_xy` (first key to last),
  `range_xy` (the diagonal of the XY extent over all keys), `pos_key_count`
  and `trajectory_samples[]` (up to 10 keys, evenly spaced: `frame`, `x`, `y`,
  `z`). `root_bone` is `NOT_FOUND` when the skeleton's bone 0 has no track.
- `issues[]`, each with `type`:
  - `bone_pop`: `bone`, `frame`, `prev_frame`, `distance`;
  - `quaternion_flip`: `bone`, `frame`, `prev_frame`, `dot_product`;
  - `root_motion_mismatch`: `detail`, `range_xy`.

The three checks:

1. **Root motion.** The root bone is bone 0 of the animation's skeleton. When
   its `range_xy` is above 5 cm and root motion is off, the issue reads
   `Root bone has significant XY movement but bEnableRootMotion is OFF — character will slide`.
   When it is at or below 5 cm and root motion is on:
   `bEnableRootMotion is ON but root bone has no significant XY movement`.
   Fix either with `set_root_motion` (section `retargeter`).
2. **Bone pops.** For every track, the distance between each key's position
   and the previous key's, in the bone's parent space.
3. **Quaternion flips.** For every track, the dot product of each key's
   rotation with the previous key's.

Reading the result:

- **The call succeeds whatever it finds.** Issues are in the data, never an
  error. Read `summary.total_issues`.
- **`issues[]` holds at most 50 pops and 50 flips.** The counts in `summary`
  are complete. When a count is above 50, the list is cut.
- `frame` in an issue is the key's position in the track's key list, and
  `prev_frame` the one before.
- **The thresholds are fixed distances, not judgments.** A fast, intended
  movement of a bone can exceed 50 cm between keys, and a real pop of a small
  bone can stay below it. Lower `pop_threshold` for hands and fingers.
- The root check looks at bone 0 only and at X and Y only. Movement that is
  on the pelvis and not on the root does not show there.
- **`total_issues: 0` does not mean the animation looks right.** Nothing here
  compares the result with the source animation, checks contact with the
  ground, or looks at the mesh. Look at frames with `capture_viewport` (skill
  `ue-viewport`, section `asset-preview`).

Errors: `Animation has no data model`, `Animation has no bone tracks`.

## copy_bone_tracks

Copies bone tracks from the animation at `source_anim_path` into the one at
`target_anim_path`, for the bones in `bone_names`.

- For each name the source has a track for, the target gets a bone track of
  that name, and all its keys are written: one key per key of the target
  animation.
- **The copy is resampled to the target's key count.** Key `i` of the target
  takes the source at the same relative position in the animation, with
  position and scale interpolated linearly and rotation by slerp. Animations
  of different length are stretched onto each other, not cut.
- **A track of that name in the target is replaced**, keys and all.
- **A name the source has no track for is skipped without an error.**

Returns `copied_count`, `source_keys`, `target_keys`, `target_total_tracks`
and the message
`Copied <n> bone tracks from '<source>' to '<target>': <names>`.

- **`copied_count: 0` is a success.** Compare the names in the message with
  what you sent.
- The tool does not check that the target's skeleton has the bone. What the
  engine does with a track for a bone the skeleton lacks is not visible in
  the tool's code; `compare_bones` first, and `add_bone` (section `rigs`) if
  the bone is missing.
- The target is marked dirty and not saved. Save it with `save`.
- There is no undo you can call. Copy the target first with `asset`
  `duplicate` (skill `ue-assets`) if you need the old tracks.

Errors: the load errors of either animation,
`Source animation has no data model`, `Target animation has no data model`,
`Missing required parameter: bone_names (JSON array)`.
