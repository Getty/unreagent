# UE blend spaces: every operation in full

Source: `MCPTool_BlendSpace.h` / `.cpp`, `BlendSpaceEditor.cpp`,
`BlendSpaceReader.cpp` in the toolkit. Engine behaviour behind the calls is in
section `ue-facts`.

## Parameters common to all operations

- `operation` is required and lowercased. `get`, `get_info`, `info` and
  `get_samples` run `inspect`. An unknown operation returns
  `Unknown operation: '<op>'. Valid: <list>`, plus ` Did you mean '<x>'?` when
  a valid name is close.
- `asset_path` is required by every operation except `list` and `create`.
  `blueprint_path` or `path` are taken in its place when it is absent.
- A missing or empty required string returns
  `Missing required parameter: <name>`. So does a `sample_index` or
  `axis_index` that is absent or negative. The blend space is loaded before
  those indices are checked, so a bad path is reported first.
- Load errors: `Failed to load blend space: <path>` or
  `Asset is not a BlendSpace: <path> (is <class>)`; for `animation_path`
  `Failed to load animation: <path>` or
  `Asset is not an AnimSequence: <path> (is <class>)`. A failed load does not
  prove the asset is missing — search before you create one under that name.

### How numbers and booleans are read

Send JSON numbers and booleans. What happens otherwise (the toolkit's
`ExtractOptionalNumber` / `ExtractOptionalBool` in `MCPToolBase.h`, and the
engine's `FJsonObject::TryGetNumberField` / `TryGetBoolField` in `set_axis`
and `batch`; engine side read in the UE 5.8.4 source, `TJsonValueString` in
`Runtime/Json/Public/Dom/JsonValue.h`, not run):

- A number is read as a `double`; an index is truncated afterwards. A string
  of an optional sign, digits and at most one dot (`"150"`) is read as a
  number, and so are `true` / `false` (1 / 0). Any other string counts as
  absent: the default applies with no error, and an index is reported as
  missing. A lone `"-"` or `"."` reads as 0 (`TCString::IsNumeric`).
- **A boolean is read from any string and any number.** `true`, `yes`, `on`
  and `false`, `no`, `off` count in any case; any other string is `false`
  unless it starts with a non-zero integer. A typo in `snap_to_grid`,
  `wrap_input` or `recursive` therefore sets `false` silently. The editor
  language's words for True/Yes/False/No count too (`FToBoolHelper`,
  `Runtime/Core/Private/Misc/CString.cpp`).

## inspect

Returns, under `data`:

- `name`, `path` (object path), `type` (`BlendSpace1D` or `BlendSpace`; an aim
  offset reports its base kind), `dimensions` (1 or 2), `anim_length` (the
  longest play length among the valid samples), `loop`.
- `axes[]`, one per dimension: `index`, `name`, `min`, `max`,
  `grid_divisions`, `snap_to_grid`, `wrap_input`, and `interpolation` with
  `time`, `damping_ratio`, `max_speed`, `type` (`Average`, `Linear`, `Cubic`,
  `EaseInOut`, `ExponentialDecay`, `SpringDamper`, or `Unknown`).
- `samples[]`: `index`, `animation` (object path) and `animation_name` (both
  `None` without an animation), `position` with `x` (plus `y` on 2D),
  `rate_scale`. On UE 5.7 and later also `single_frame`, and `frame_index`
  when `single_frame` is true and an animation is set; on 5.6 `single_frame`
  is always `false` (the tool's `#if` on the engine version). `sample_count`
  counts them. **The engine's per-sample valid flag is not reported.**
- `settings` (read-only for this tool): `notify_trigger_mode`
  (`AllAnimations`, `HighestWeightedAnimation`, `None`),
  `interpolate_using_grid`, `preferred_triangulation` (`None`, `Tangential`,
  `Radial`), `target_weight_interpolation_speed`, `allow_mesh_space_blending`,
  `allow_marker_based_sync`.
- `geometry`: `triangles` and `segments`, the counts of the blend geometry the
  engine built. Both stay 0 while `interpolate_using_grid` is true; the grid
  is used instead.

## list

Searches the asset registry for blend spaces of every kind in `folder_path`
(a package folder such as `/Game/Anims`). **Only that folder** unless
`recursive: true`. Returns `count` and `blend_spaces[]` with `name`, `path`
(object path) and `type`. That `type` is `BlendSpace1D` only for a plain 1D
blend space; every other class, a 1D aim offset included, is listed as
`BlendSpace`. No match is a success with `count: 0`.

## create

Creates `<package_path>/<name>` for the skeleton at `skeleton_path` through the
engine's asset factory. The result has `path` (object path) and `type`; the
message is `Created <type>: <path>`. The asset is marked dirty, **not saved**.

`type` is read case-insensitively by substring:

| `type` contains | Creates |
|---|---|
| `AimOffset` and `1D` | `AimOffsetBlendSpace1D` |
| `AimOffset`, no `1D` | `AimOffsetBlendSpace` |
| `1D`, no `AimOffset` | `BlendSpace1D` (the default) |
| neither | `BlendSpace` (2D) |

- The tool does not check for an existing asset of that name; what the
  engine's `CreateAsset` then does is not covered here. Look first.
- Errors: `Failed to load skeleton: <path>`, `Asset is not a Skeleton: <path>`,
  `Failed to create blend space: <package_path>/<name>`.

## add_sample

Adds `animation_path` at (`x`, `y`), both defaulting to 0 (a 1D blend space
uses `x`; leave `y` out). Returns `sample_index`; the message is
`Added sample <i>: <anim> at (<x>, <y>)`, plus `, rate_scale=<r>` when one was
applied. The engine first grows the axis range to fit the position, then
refuses the sample when the position is taken or out of range, or the
animation is incompatible (section `ue-facts`):
`AddSample failed for <path> at (<x>, <y>)`, no reason given. The tool does
not run its own skeleton check here, the engine's check does it.

## remove_sample

Deletes `sample_index`. **The last sample moves into the freed index.** The
message says so; the result has `remaining_samples`. Re-read with `inspect`.

## move_sample

Sets `sample_index` to (`x`, `y`). **Both default to 0**: pass both on 2D.
Optional `rate_scale` as in `add_sample`. Message
`Moved sample <i> to (<x>, <y>)`. Refusal: `EditSampleValue failed for index <i>`
(position taken by another sample, out of range after growth, or a
single-frame index the animation does not have).

## set_sample_animation

Replaces the animation of `sample_index`, keeping position and index. The tool
checks compatibility first (skeleton, additive type allowed by this kind of
blend space, same additive type as the other samples):
`Animation <path> is not compatible with this blend space's skeleton` — the
message names the skeleton for every one of those reasons. No geometry
rebuild follows (none is needed).

## rate_scale

Playback speed of one sample. In `add_sample`, `move_sample` and their batch
ops a value above 0 is clamped to 0.01–64 and applied. Absent, 0 or negative
leaves the sample's rate as it is (the code's default is -1; the schema's
"default 1.0" is not what happens). A new sample without one keeps the engine's
default, 1.0 (`FBlendSample`, UE 5.8.4 source). `inspect` shows the value in effect.

## set_axis

Changes only the fields you pass, on `axis_index` 0 (1D) or 0/1 (2D).
Out of range: `Axis index <n> out of range (0-<last> for 1D)` / `… for 2D)`.

| Parameter | Sets (editor label) |
|---|---|
| `axis_name` or `name` (`name` wins when both are sent) | Name — the axis label (the plugin's binding notes use it for 2D bindings, skill `ue-anim-blueprint`) |
| `min`, `max` | Minimum / Maximum Axis Value |
| `grid_divisions` | Grid Divisions (integer; the editor clamps to ≥ 1, the tool does not) |
| `snap_to_grid`, `wrap_input` | Snap to Grid, Wrap Input |
| `interp_time` | Smoothing Time, seconds; 0 disables smoothing |
| `interp_type` | Smoothing Type: `Average`, `Linear`, `Cubic`, `EaseInOut`, `ExponentialDecay`, `SpringDamper` (any case; anything else ignored) |
| `damping_ratio` | Damping Ratio, SpringDamper only |
| `max_speed` | Max Speed, axis units per second; 0 means unlimited |

- No check that `min` < `max`, nor that `grid_divisions` ≥ 1.
- Message: `Configured axis <n> ('<name>': <min> to <max>, <d> divisions)`;
  the smoothing values are not echoed — `inspect` them.

## save

`UEditorAssetLibrary::SaveAsset(asset_path, false)`: saves even without
unsaved changes. `Saved: <path>` or `Failed to save: <path>`. It does not check
that the asset is a blend space.

## The rebuild after a write

After `add_sample`, `remove_sample`, `move_sample` and `set_axis` (and once at
the end of a `batch`) the tool calls the engine's `ValidateSampleData` and
`ResampleData`. That re-snaps, de-duplicates, re-validates and rebuilds the
geometry; section `ue-facts` says what each step can change.
`set_sample_animation` alone does not trigger it.
