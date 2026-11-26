# UE blend spaces: what the engine does

The tool hands most decisions to the engine. These are the engine rules behind
its results. Sources: the UE 5.8.4 engine source
(`Runtime/Engine/Private/Animation/BlendSpace.cpp`,
`Classes/Animation/BlendSpace.h`, `AimOffsetBlendSpace*.cpp`), read, not run;
and Epic's pages
[Blend Spaces](https://dev.epicgames.com/documentation/en-us/unreal-engine/blend-spaces-in-unreal-engine)
and
[Aim Offset](https://dev.epicgames.com/documentation/en-us/unreal-engine/aim-offset-in-unreal-engine).
The toolkit targets UE 5.6 and 5.7; where a rule comes only from 5.8.4 source
it may differ on an older engine (unverified there).

## Axes

- **Defaults of a new asset:** name `None`, range 0–100, 4 grid divisions,
  snap and wrap off; smoothing time 0 (off), type `SpringDamper`, damping 1.0,
  max speed 0 (`FBlendParameter`, `FInterpolationParameter`).
- **Range growth.** Before a sample is added or moved, the engine widens any
  axis whose range does not contain the position, in steps of one grid cell
  (`(max-min)/divisions`), **adding one grid division per step**
  (`ExpandRangeForSample`). Adding x=600 to a fresh 0–100/4 axis gives
  0–600 with 24 divisions. Set the range first; then nothing grows.
- **Grid Divisions** decide where snapping lands and, with Use Grid on, the
  sampling resolution; with Use Grid off they only change the grid shown
  (Epic, Blend Spaces).
- **Snap to Grid.** On a 2D blend space with snap on **both** axes, every
  rebuild moves each sample to its nearest grid point
  (`SnapSamplesToClosestGridPoint`). The position `inspect` shows can then
  differ from the one you sent.
- **Wrap Input** treats the axis as cyclic: input beyond min/max wraps
  around instead of being clamped. Use it for a full -180…180 direction axis;
  the samples at both ends should be the same motion (Epic: avoid popping).
  Epic's page suggests the legacy Use Grid mode for such arrangements; this
  tool cannot switch Use Grid (it is read-only in `inspect.settings`).

## Which samples count

`AddSample` and `EditSampleValue` refuse a sample, and the rebuild marks one
invalid, when:

- **the position is taken**: another sample sits at the same x and y (within
  `UE_KINDA_SMALL_NUMBER`). The rebuild also **deletes** later samples on an
  already used position, so indices can shift without a `remove_sample`;
- **the position is outside the range** (only possible after you narrow an
  axis, since adding grows it);
- **the animation's skeleton is not compatible** with the blend space's
  skeleton (`USkeleton::IsCompatibleForEditor`);
- **the additive type does not fit**: a blend space accepts non-additive,
  local-space additive and mesh-space additive sequences; an aim offset only
  `AAT_RotationOffsetMeshSpace` (mesh-space additive); and every sample must
  share the additive type of the samples already there;
- a single-frame sample points past the animation's frames (UE 5.7+).

Invalid samples stay in the list but do not blend; `inspect` cannot show the
flag. Clues: `anim_length` counts only valid samples, and in the editor each
rebuild posts an entry per invalid sample to the **Load Errors** message log
("… invalid due to being out of bounds", "… not unique", "… no/invalid
animation"). Whether those entries also reach `get_output_log` is unverified.

## Smoothing (interpolation)

Per axis, applied to the input value before it picks samples (Epic, Blend
Spaces; field comments in `BlendSpace.h`):

| Tool `interp_type` | Editor name | Notes |
|---|---|---|
| `Average` | Averaged | |
| `Linear` | Linear | |
| `Cubic` | Cubic | |
| `EaseInOut` | Ease In/Out | |
| `ExponentialDecay` | Exponential | `max_speed` applies |
| `SpringDamper` | Spring Damper | `damping_ratio` and `max_speed` apply; the engine default |

- `interp_time` (Smoothing Time) 0 disables smoothing; larger values smooth
  more.
- `damping_ratio` 1 reaches the target without overshoot; lower values
  overshoot, around 0.7 can look more natural (engine comment).
- `max_speed` caps the input's change per second in axis units (90 on a
  degree axis = 90°/s); 0 means no cap.
- **Weight Speed** (`settings.target_weight_interpolation_speed`) smooths the
  sample weights instead of the input; Epic advises not to combine it with
  Smoothing Time. Read-only for this tool.

## Triangulation and Use Grid

- New blend spaces blend through a triangulation of the samples (1D: line
  segments); `inspect.geometry` counts them. Assets saved before UE 5's
  runtime triangulation load with **Use Grid** on, and then `geometry` stays 0.
- `preferred_triangulation` (`Tangential`, `Radial`, `None`) only breaks ties
  for evenly placed samples. Read-only here.

## Sync and notifies

- `allow_marker_based_sync`: samples sync by their sync markers when markers
  exist; it does not force sync without them (`BlendSpace.h` comment). For a
  walk/run blend, give the cycles matching foot markers (skill
  `ue-anim-sequence`). If the samples' marker name sequences do not all match,
  the engine syncs none of them by markers (`SampleIndexWithMarkers` is reset).
- `notify_trigger_mode`: `AllAnimations` fires every sample's notifies,
  `HighestWeightedAnimation` only the dominant sample's, `None` none (Epic,
  Blend Spaces). Footstep notifies usually want `HighestWeightedAnimation`.
  Read-only here.

## Aim offsets

- An aim offset is a blend space of **mesh-space additive** poses, layered on
  a base pose by the Aim Offset node in the Animation Blueprint (Epic, Aim
  Offset).
- Each sample sequence needs **Additive Anim Type: Mesh Space**, **Base Pose
  Type: Selected animation frame**, a **Base Pose Animation** (the same base
  the aim offset previews against) and usually **Ref Frame Index 0** (Epic,
  Aim Offset). Local Space additive skews up-aim poses on leaning
  characters. Those settings belong to the sequence, not to this tool (skill
  `ue-anim-sequence`).
- Typical axes: Yaw and Pitch in degrees, often -90…90 or -180…180 (Epic's
  range examples); a common layout is 3 rows (up, centre, down) by up to 5
  yaw columns. Epic suggests limiting yaw while the character moves, or
  using pitch only and letting the capsule turn.

## Engine versions

| | 5.6 | 5.7 | 5.8 |
|---|---|---|---|
| per-sample single-frame blending in `inspect` | `single_frame` always `false` | reported | reported (5.8.4 source has the fields) |
| everything else this tool touches | same code path | same | same in the 5.8.4 source |

The toolkit's own code has no other engine-version branch for this tool
(`BlendSpaceReader.cpp` is its only `#if`). Whether 5.6 / 5.7 behave exactly
like the 5.8.4 source in the rules above is unverified.
