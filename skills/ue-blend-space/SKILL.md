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

This skill has three sections:

| Section | Holds | Load it |
|---|---|---|
| `inspect-result` | the fields inside `inspect`'s `axes`, `samples`, `settings` and `geometry` | when you need the name or meaning of a field inside one of these four, or a value there is not what you expect |
| `batch` | `batch` in full: the shape of `operations`, an example, the per-op errors, the result | before you call `batch` |
| `failures` | the error table (error, cause) and the failure table (what you see, what it means, what to do) | when a call returns an error, or the asset is not in the state you expect |

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
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
- **A boolean is read from any string and any number.** `true`, `yes`, `on`
  and `false`, `no`, `off` count in any case; every other string is `false`
  unless it starts with a non-zero integer. A typo in `snap_to_grid`,
  `wrap_input` or `recursive` therefore sets `false`, with no error.
  The editor language's words for True, Yes, False and No count as well
  (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`).

## Reading

**`inspect`** returns:

- `name`, `path` (object path), `type`, `dimensions` (1 or 2), `anim_length`,
  `loop`.
- `type` is `BlendSpace1D` or `BlendSpace`; it never says that the asset is an
  aim offset.
- `axes`, `samples` (`sample_count` counts them), `settings` and `geometry`.
  The tool reads the `settings` but cannot set them. Load section
  `inspect-result` when you need the name or meaning of a field inside one of
  these four, or a value there is not what you expect.

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

Runs several of `add_sample`, `remove_sample`, `move_sample`,
`set_sample_animation` and `set_axis` on one asset in one call. Load section
`batch` before you call `batch`: it has the shape of `operations`, an
example, the per-op errors and the result. What goes wrong without a word:

- A `remove_sample` moves the last sample into the freed index for every op
  after it.
- A failed op does not stop the ones after it, and nothing is rolled back.
- **With one failed op, the call is an error, and the bridge passes only the
  message** `Batch: <n> OK, <m> errors, <t> total`: which op failed, and why,
  does not reach you. Run `inspect`, then repeat the missing change as a
  single operation to get its error.
- `batch` does not save.

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
   Load section `batch` before you call `batch`.
4. **Read back.** `inspect` after `remove_sample`, after a range change, and
   after any `batch` that failed.
5. **Save.** `save`.

## When a call fails

Load section `failures` when a call returns an error, or the asset is not in
the state you expect: it has the error table (error, cause) and the failure
table (what you see, what it means, what to do).

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
