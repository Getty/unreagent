---
name: ue-blend-space
description: >
  Create, inspect and edit blend spaces and aim offsets, 1D and 2D, with
  blend_space: axes, axis interpolation, samples, saving. Use before you build
  or change a blend space, or when a blend_space call returns an error.
toolkit_tools: [blend_space]
---

# UE blend spaces: blend_space

The UE LLM Toolkit plugin's **`blend_space`** tool reads and edits blend space
assets: their axes, the interpolation of each axis, and the samples (one
animation sequence at one position each). It creates blend spaces and aim
offsets, 1D and 2D. Through the plugin's MCP bridge it is listed as
`unreal_blend_space`. It works only while the editor is running and the plugin
is reachable; `unreagent-iterate` covers the wait for the plugin.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

The tool takes an `operation` string and lowercases it before dispatch. `get`,
`get_info`, `info` and `get_samples` run `inspect`. An unknown operation
returns `Unknown operation: '<op>'. Valid: <list>`, plus ` Did you mean '<x>'?`
when a valid name is close.

## Before the first call

- **Result shape.** Success is the tool's message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope (`task_id`, `status`, `success`, `message`, …), and the tool's own
  fields, the ones named in this skill, sit under its `data` key. Failure is
  `Error: <message>` with no data. Skill `ue-tasks` covers the task tools.
- **Nothing saves except `save`.** Every write marks the asset as modified and
  lives only in the running editor until you call `save`, or the `asset`
  tool's `save_asset` or `save_all` (skill `ue-assets`).
- **No undo.** No operation opens an editor transaction. Undo a change with
  the inverse operation: `remove_sample` after `add_sample`, `move_sample` back
  to the old position.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the asset back with `inspect` before you retry (skill
  `ue-tasks`).

## Paths

Every operation except `list` and `create` needs `asset_path`. The tool takes
`blueprint_path` or `path` in its place when `asset_path` is absent.

- The path goes to the engine's loader as given. There is no path check and no
  `/Game/` fallback, unlike the Blueprint tools (skill `ue-blueprints`). The
  tool's own examples use package paths, such as `/Game/Anims/BS_Walk`.
  `create`, `list` and `inspect` report object paths, such as
  `/Game/Anims/BS_Walk.BS_Walk`. Prefer that form.
- A path that does not load as a blend space returns
  `Failed to load blend space: <path>`. The tool can also return
  `Asset is not a BlendSpace: <path> (is <class>)`. Which of the two a wrong
  asset type gives is up to the engine's loader.
- A failed load does not prove the asset is missing. Search with `list` or
  `asset_search` before you create one under that name.

`animation_path` takes an animation sequence. It fails with
`Failed to load animation: <path>` or
`Asset is not an AnimSequence: <path> (is <class>)`. The tool accepts only
animation sequences as samples, no montages and no other blend spaces.

## Operations

| Operation | Required | Optional |
|---|---|---|
| `inspect` | `asset_path` | — |
| `list` | `folder_path` | `recursive` (default `false`) |
| `create` | `package_path`, `name`, `skeleton_path` | `type` (default `BlendSpace1D`) |
| `add_sample` | `asset_path`, `animation_path` | `x`, `y` (default 0), `rate_scale` |
| `remove_sample` | `asset_path`, `sample_index` | — |
| `move_sample` | `asset_path`, `sample_index` | `x`, `y` (default 0), `rate_scale` |
| `set_sample_animation` | `asset_path`, `sample_index`, `animation_path` | — |
| `set_axis` | `asset_path`, `axis_index` | `axis_name`, `min`, `max`, `grid_divisions`, `snap_to_grid`, `wrap_input`, `interp_time`, `interp_type`, `damping_ratio`, `max_speed` |
| `save` | `asset_path` | — |
| `batch` | `asset_path`, `operations` | — |

A missing or empty string parameter returns
`Missing required parameter: <name>`. So does a `sample_index` or
`axis_index` that is absent or negative. The blend space is loaded before
these indices are checked, so a bad path is reported first.

Send numbers and booleans as JSON numbers and booleans. What the tool reads
otherwise (`ExtractOptionalNumber` and `ExtractOptionalBool` in
`MCPToolBase.h`, and the engine's `FJsonObject::TryGetNumberField` and
`TryGetBoolField` in `set_axis` and `batch`; the engine side is
`TJsonValueString` in `Engine/Source/Runtime/Json/Public/Dom/JsonValue.h`,
read in the UE 5.8.4 source, not run, while the toolkit targets UE 5.6 and
5.7):

- Every number is read as a `double`, and an index is truncated afterwards.
  The read also takes a string made of an optional sign, digits and at most
  one dot, such as `"150"`, and `true` and `false` as 1 and 0. Any other
  string counts as absent: the default applies without an error, and a
  `sample_index` or `axis_index` is reported as missing.
- **A boolean is read from any string and any number.** `true`, `yes`, `on`
  and `false`, `no`, `off` count in any case; every other string is `false`
  unless it starts with a non-zero integer. A typo in `snap_to_grid`,
  `wrap_input` or `recursive` therefore sets `false`, with no error.

## Reading

**`inspect`** returns:

- `name`, `path` (object path), `type`, `dimensions` (1 or 2), `anim_length`,
  `loop`.
- `type` is `BlendSpace1D` or `BlendSpace`; it never says that the asset is an
  aim offset.
- `axes`, one entry per dimension: `index`, `name`, `min`, `max`,
  `grid_divisions`, `snap_to_grid`, `wrap_input`, and `interpolation` with
  `time`, `damping_ratio`, `max_speed` and `type` (`Average`, `Linear`,
  `Cubic`, `EaseInOut`, `ExponentialDecay`, `SpringDamper`, or `Unknown`).
- `samples`: `index`, `animation` (object path) and `animation_name` (both
  `None` for a sample without an animation), `position` with `x` (plus `y` on a
  2D blend space), `rate_scale`. On UE 5.6 and later each sample also has
  `single_frame` (always `false` on 5.6); from 5.7, `frame_index` is added when
  `single_frame` is true and the sample has an animation. `sample_count` counts them.
- `settings`: `notify_trigger_mode`, `interpolate_using_grid`,
  `preferred_triangulation`, `target_weight_interpolation_speed`,
  `allow_mesh_space_blending`, `allow_marker_based_sync`. The tool reads these
  but cannot set them.
- `geometry`: the number of `triangles` and `segments` the engine built from
  the samples.

**`list`** searches the asset registry for blend spaces of every kind in
`folder_path`, such as `/Game/Anims`. **It searches only that folder** unless
you pass `recursive: true`. It returns `count` and `blend_spaces`, each with
`name`, `path` (object path) and `type`. That `type` is `BlendSpace1D` only for
a plain 1D blend space; every other kind, a 1D aim offset included, is listed as
`BlendSpace`. Use `inspect` for the dimensions. No match is a success with
`count: 0`.

`asset_search` with `class_filter: "BlendSpace"` finds them too, subclasses
included, and can search everywhere (skill `ue-assets`).

## create

Creates `<package_path>/<name>` for the skeleton at `skeleton_path`, for
example `package_path: "/Game/Anims"`, `name: "BS_Walk"`. The result has
`path` (the object path for every later call) and `type`. The message is
`Created <type>: <path>`. The tool does not save the new asset.

`type` is not validated; the tool reads it case-insensitively by substring:

| `type` contains | Creates |
|---|---|
| `AimOffset` and `1D` | `AimOffsetBlendSpace1D` |
| `AimOffset`, no `1D` | `AimOffsetBlendSpace` |
| `1D`, no `AimOffset` | `BlendSpace1D` (the default) |
| neither | `BlendSpace` (2D) |

A misspelt value therefore creates a 2D blend space without an error. Check the
result's `type`.

- The tool does not check whether an asset of that name already exists, and
  what the engine then does is not covered here. Check with `list` first.
- The axes of the new asset are the engine's defaults. Set them with
  `set_axis` before you add samples.
- Errors: `Failed to load skeleton: <path>`, `Asset is not a Skeleton: <path>`,
  `Failed to create blend space: <package_path>/<name>`.

## Samples

Samples are addressed by `sample_index`, as `inspect` lists them.

- **`add_sample`** adds `animation_path` at (`x`, `y`) and returns its
  `sample_index`. `x` and `y` default to 0; a 1D blend space uses `x`.
  According to a comment in the plugin, the engine widens the axis range to fit
  the new sample; read the axes back with `inspect`. Whether the animation fits
  the blend space (its skeleton, additive settings for an aim offset) is the
  engine's check, and a refusal reads
  `AddSample failed for <path> at (<x>, <y>)`.
- **`remove_sample`** deletes the sample at `sample_index`. **The last sample
  moves into the freed index**, so every index you noted for the last sample is
  stale. The message says so, and the result has `remaining_samples`. Run
  `inspect` before the next index-based call.
- **`move_sample`** sets the position of `sample_index` to (`x`, `y`). **Both
  default to 0**: on a 2D blend space, a call with only `x` moves the sample to
  `y = 0`. Pass both. Failure: `EditSampleValue failed for index <n>`, the
  engine's refusal of that position.
- **`set_sample_animation`** replaces the animation of `sample_index` and keeps
  its position. It first checks that the animation fits the blend space's
  skeleton: `Animation <path> is not compatible with this blend space's skeleton`.
- An index out of range: `Sample index <n> out of range (0-<last>)`.

**`rate_scale`** is the playback speed of one sample. In `add_sample` and
`move_sample`, a value above 0 is clamped to 0.01–64 and applied. **Leaving it
out, or passing 0 or a negative value, leaves the sample's rate untouched**:
the tool sets none on a new sample and keeps the current value on a moved
one. The tool's parameter list states a default of 1.0, but the code's
default is -1, which means "do not set". `inspect` shows the value that is in
effect.

After `add_sample`, `remove_sample`, `move_sample` and `set_axis` the tool has
the engine re-validate the samples and rebuild the blend space's geometry.

## set_axis

Changes only the fields you pass, on axis `axis_index`: 0 on a 1D blend space,
0 or 1 on a 2D one (`Axis index <n> out of range (0-<last> for 1D)` or
`… for 2D)`).

| Parameter | Sets |
|---|---|
| `axis_name` (or `name`, which wins when both are given) | the axis display name |
| `min`, `max` | the axis range |
| `grid_divisions` | the number of grid divisions (an integer) |
| `snap_to_grid`, `wrap_input` | the two axis flags |
| `interp_time` | interpolation time in seconds |
| `interp_type` | `Average`, `Linear`, `Cubic`, `EaseInOut`, `ExponentialDecay` or `SpringDamper`, case-insensitive |
| `damping_ratio` | damping for `SpringDamper` |
| `max_speed` | the maximum interpolation speed |

- **An unknown `interp_type` is ignored without an error**, and the axis keeps
  its interpolation type. Check `axes[].interpolation.type` with `inspect`.
- The tool does not check that `min` is below `max`.
- The message is `Configured axis <n> ('<name>': <min> to <max>, <divisions> divisions)`.
  It does not echo the interpolation settings.
- Binding an axis to a variable of an Animation Blueprint is
  `anim_blueprint_modify`'s job (skill `ue-anim-blueprint`).

## batch

`operations` is an array of objects, each with `op` and the parameters of one
of `add_sample`, `remove_sample`, `move_sample`, `set_sample_animation` or
`set_axis`. `asset_path` is given once, outside the array.

```json
{"operation": "batch", "asset_path": "/Game/Anims/BS_Walk.BS_Walk",
 "operations": [
   {"op": "set_axis", "axis_index": 0, "axis_name": "Speed", "min": 0, "max": 600},
   {"op": "add_sample", "animation_path": "/Game/Anims/Idle.Idle", "x": 0},
   {"op": "add_sample", "animation_path": "/Game/Anims/Walk.Walk", "x": 300}
 ]}
```

- The ops run in order on the same asset, and the geometry is rebuilt once at
  the end. A `remove_sample` moves the last sample into the freed index for
  every op after it.
- A failed op does not stop the ones after it, and nothing is rolled back.
- Per-op errors: `[<i>] not a JSON object`, `[<i>] missing 'op' field`,
  `<op>: missing animation_path`, `<op>: missing sample_index`,
  `set_axis: missing axis_index`, `Unknown batch op: '<op>'. Valid: add_sample, remove_sample, move_sample, set_sample_animation, set_axis`,
  and the single operations' errors.
- `batch` does not save. A missing array:
  `Missing required parameter: operations (JSON array)`.
- The message is `Batch: <n> OK, <m> errors, <t> total`. With every op
  successful, the data has `results` (one result per op), `ok_count`,
  `error_count` and `total`. **With one failed op, the call is an error, and
  the bridge passes only that message**: which op failed, and why, does not
  reach you. Run `inspect`, then repeat the missing change as a single
  operation to get its error.

## save

Saves the asset at `asset_path`, also when it has no unsaved changes:
`Saved: <path>`, or `Failed to save: <path>`. It does not check that the asset
is a blend space.

## The edit loop

1. **Find or create.** `list` (with `recursive: true` for a whole tree) or
   `create`.
2. **Read.** `inspect`: axes, sample indices, positions.
3. **Set the axes first**, then add samples. Several changes go in one
   `batch`.
4. **Read back.** `inspect` after `remove_sample`, after a range change, and
   after any `batch` that failed.
5. **Save.** `save`.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or empty; for an index, also a negative one |
| `Failed to load blend space: <path>` / `Asset is not a BlendSpace: …` | nothing loads at that path as a blend space |
| `Failed to load animation: <path>` / `Asset is not an AnimSequence: …` | `animation_path` is not an animation sequence |
| `Animation <path> is not compatible with this blend space's skeleton` | `set_sample_animation` with an animation for another skeleton |
| `Sample index <n> out of range (0-<last>)` | stale or wrong index; read it with `inspect` |
| `Axis index <n> out of range …` | axis 1 on a 1D blend space, or above 1 |
| `AddSample failed for <path> at (<x>, <y>)` | the engine refused the sample |
| `Error: Batch: <n> OK, <m> errors, <t> total` | at least one batch op failed |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a change is gone after an editor restart | it was never saved | `save` after each finished change |
| a later call changed the wrong sample | `remove_sample` moved the last sample into the freed index | `inspect`, then use the new indices |
| a 2D sample dropped to `y = 0` | `move_sample` without `y` | pass both `x` and `y` |
| `rate_scale` is not what you sent | you sent 0 or less, which leaves it unchanged, or more than 64 | send a value from 0.01 to 64 |
| the interpolation type did not change | the `interp_type` string was not one of the six names | use one of the six |
| `create` made a 2D blend space | `type` contained neither `1D` nor `AimOffset` as you meant | check the result's `type` |
| `list` returns nothing for a folder with blend spaces | they sit in subfolders | `recursive: true` |

## Background in the plugin

The plugin's `Resources/domains/blueprints.md` has short notes on blend space
parameter binding in Animation Blueprints and on axis interpolation tuning.
`unreal_get_ue_context` with `category: "animation"` serves its Animation
Blueprint notes; their blend space snippets call the plugin's internal C++,
not this tool (skill `ue-context`).

## See also

- `ue-anim-blueprint`: playing a blend space in an Animation Blueprint state
  and binding its axes to variables.
- `ue-anim-sequence`: the animation sequences used as samples.
- `ue-assets`: finding, saving, renaming and deleting assets.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
