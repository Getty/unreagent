# anim_edit: sequences

These operations work on `UAnimSequence` assets. Frames are numbered by key:
`0` is the first key, and the last is `total_frames - 1` as `inspect_track`
reports it. `extract_range` uses the same numbering.

## inspect_track

Reads one bone track at chosen frames. It changes nothing.

- `asset_path` and `bone_name` are required.
- `sample_frames` (default `[0, -1]`) is an array of frame indices. A negative
  index counts from the end: `-1` is the last frame, `-2` the one before it.
  Indices outside the animation are skipped without an error. Entries that are
  not numbers are skipped, and when none is left, `[0, -1]` is used.

Returns `animation` (the asset name), `bone`, `total_frames` and `frames`, one
entry per sampled frame with `frame`, `position` `{x, y, z}`, `rotation`
`{pitch, yaw, roll}`, `rotation_quat` `{x, y, z, w}` and `scale` `{x, y, z}`.
The message is `Inspected <n> frames of '<bone>' in <animation>`; a count below
the number you asked for means some entries were skipped.

Errors: `Animation has no data model`, `Bone track '<bone>' not found`. Bone
names come from the skeleton, and `inspect_mesh` lists a mesh's bones (section
`meshes`). Not every bone needs a track in every animation.

## adjust_track

Offsets one bone's track, in every key, in one or more sequences.

- `location_offset` `{x, y, z}` is added to every position key.
- `rotation_offset` `{pitch, yaw, roll}` is pre-multiplied into every rotation
  key: the new key is the quaternion product `offset * key`.
- `scale_override` `{x, y, z}` replaces the scale of every key. It is an
  absolute value, not a factor. **Send all three components**: a missing
  component is 0. A `scale_override` that is not an object, such as `1.0`,
  still counts as given and sets every scale key to 0.
- At least one of the three must be given, and an offset of all zeros does not
  count: `At least one of location_offset, rotation_offset, or scale_override must be provided`.
- The offsets apply to whatever the keys hold now, so a second call adds them
  again. Read the track with `inspect_track` before and after.

Each entry's message is `Adjusted '<bone>' in <animation> (<n> keys)`. The call's
message is `Batch adjust: <ok> succeeded, <failed> failed out of <total>`.

Errors per asset, in the entry's `error`: `No data model: <path>`,
`Bone track '<bone>' not found in <path>`,
`Bone track '<bone>' has 0 keys in <path>`, `SetBoneTrackKeys failed for <path>`.

## resample

Rebuilds every bone track of each sequence with a new key count.

- `target_fps` is required: a number above 0, truncated to an integer, so
  `29.97` becomes `29`. Otherwise:
  `Missing required parameter: target_fps (number > 0)`.
- Per sequence, the code samples each bone track at evenly spaced times from 0
  to the play length, with linear interpolation, and writes
  `round(length × target_fps) + 1` keys. It sets the number of frames to
  `round(length × target_fps)`.
- **The frame rate is not changed.** The code never sets the sequence's frame
  rate. The tool's description says `resample` "fixes metadata" and "preserves
  animation length exactly"; the code does not set the rate, and the
  plugin's `domains/assets.md` agrees that it changes key data only. In the
  engine's animation data the length follows from frame count and frame rate,
  so expect a resample to a rate other than the current one to change the play
  length. This has not been checked against a running editor. For a real
  frame-rate change, reimport the source with `asset_import` and
  `custom_sample_rate` (skill `ue-assets`), as the plugin's notes recommend.
- A sequence already at exactly `target_fps` (a whole-number rate) is skipped
  and counted as a success: `Skipped <animation> — already at <fps> FPS`.
- Only bone tracks are rewritten. The tool does nothing to curves or notifies.
- **A failed track write is not reported.** The code ignores a track that
  could not be written, and the sequence still counts as resampled.

Each resampled entry has `old_fps`, `new_fps`, `old_keys` and `new_keys`, and
the message
`Resampled <animation>: <num>/<den> FPS → <fps> FPS, <old> → <new> keys (<bones> bones, length <s>s)`.
A skipped entry has only the message. The call's message is
`Resample to <fps> FPS: <ok> succeeded, <failed> failed out of <total>`.

Errors per asset: `No data model: <path>`,
`Target FPS <fps> produces 0 frames for <path> (length <s>s)`,
`No bone tracks in <path>`.

## extract_range

Copies a sequence and trims the copy to a frame range. The source is not
changed.

- `start_frame` and `end_frame` are required numbers, both inclusive.
  Absent: `Missing required parameter: start_frame` (or `end_frame`). A
  negative `end_frame` means the last frame.
- `dest_path` is the folder for the copy, by default the source's folder.
  `new_name` defaults to `<source name>_F<start>_<end>`. The tool checks
  neither.
- The code duplicates the source to `<dest_path>/<new_name>`, then trims the
  frames after `end_frame` and before `start_frame` with the engine's trim
  helper. What the trim does to notifies and curves in the cut-off part is up
  to the engine.
- What happens when an asset already exists at that path is up to the engine's
  duplicate. Check with `asset_search` first (skill `ue-assets`).
- **A failure after the copy leaves the copy behind**:
  `Duplicated asset is not an AnimSequence`, `Trim tail failed` and
  `Trim head failed` come after the duplicate was made. Remove it with `asset`
  `delete`.

Returns `source_path`, `new_asset_path` (`<dest_path>/<new_name>`),
`source_total_frames`, `extracted_start_frame`, `extracted_end_frame`,
`extracted_frame_count` and `frame_rate`, a string such as `30fps`. The message
is `Extracted frames <start>-<end> from <source> -> <new_name> (<n> frames)`.

Errors: `No data model: <path>`, `Animation has 0 keys: <path>`,
`start_frame <s> out of range [0, <last>]`,
`end_frame <e> out of range [<start>, <last>]`,
`DuplicateAsset failed: <dest_path>/<new_name>`.

## set_additive_type

Sets a sequence's additive animation type and base pose.

| Parameter | Default | Values |
|---|---|---|
| `additive_anim_type` | required | `None`, `LocalSpaceBase`, `RotationOffsetMeshSpace` |
| `base_pose_type` | `RefPose` | `None`, `RefPose`, `AnimScaled`, `AnimFrame`, `LocalAnimFrame` |
| `ref_pose_seq` | — | the base pose sequence. Required for `AnimScaled` and `AnimFrame` |
| `ref_frame_index` | `0` | the base pose frame |

- Both type names are matched case-insensitively.
- **Every call writes all four settings.** Leaving out `base_pose_type` sets
  `RefPose`, leaving out `ref_pose_seq` clears the sequence that was set, and
  leaving out `ref_frame_index` sets 0. Pass every setting you want to keep;
  the result's `old` shows what was there.
- The tool does not require `ref_pose_seq` for `LocalAnimFrame`. Whether the
  engine needs one there is not verified.
- After the change, the tool fires the engine's property-change notification
  for the additive type. The tool's description says this triggers
  recompression; that is the engine's part.

Returns `asset_path`, `old` and `new`, each with `additive_anim_type`,
`base_pose_type`, `ref_frame_index`, and `ref_pose_seq` when one is set. The
message is `Set additive type on <animation>: <type> (base: <base>)`.

Errors:

- `Unknown additive_anim_type: '<t>'. Valid: None, LocalSpaceBase, RotationOffsetMeshSpace`
- `Unknown base_pose_type: '<t>'. Valid: None, RefPose, AnimScaled, AnimFrame, LocalAnimFrame`
- `base_pose_type '<t>' requires ref_pose_seq (base pose animation path)`
- `Failed to load ref_pose_seq: <path>`
- `Skeleton mismatch: anim uses '<a>' but ref_pose_seq uses '<b>'`, checked
  only when both sequences have a skeleton.

## replace_skeleton

Points one or more sequences at another skeleton.

- `skeleton_path` is a skeleton, or a skeletal mesh whose skeleton is used.
  When it loads as neither, the whole call fails before any sequence is
  touched:
  `Failed to load skeleton from path: <path> (tried as USkeleton and USkeletalMesh)`.
- **The tool checks nothing about compatibility.** It compares no bone names or
  hierarchy and retargets no keys; it only sets the sequence's skeleton. How the
  engine matches the tracks to the new skeleton is up to it. Check a few bones
  with `inspect_track` afterwards.

Each entry has `path`, `success`, and `old_skeleton` (`None` when there was
none) and `new_skeleton`, or `error`. The call's message is
`Replace skeleton: <ok> succeeded, <failed> failed out of <total>`.
