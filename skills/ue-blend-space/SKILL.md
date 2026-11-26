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
assets: their axes, the smoothing of each axis, and the samples (one animation
sequence at one position each). It creates blend spaces and aim offsets, 1D and
2D. Through the bridge it is listed as `unreal_blend_space`. It works only while
the editor runs and the plugin is reachable (`unreagent-iterate`).

Written from the toolkit's C++ source (`MCPTool_BlendSpace.*`,
`BlendSpaceEditor.cpp`, `BlendSpaceReader.cpp`) and the UE 5.8.4 engine source;
not exercised against a running editor.

## Sections

| Section | What is in it | Load it when |
|---|---|---|
| `workflows` | step-by-step builds: 1D speed, 2D locomotion (speed × direction), aim offset, swapping an animation pack, repairing a broken blend space | before you build or rebuild a blend space |
| `operations` | every operation in full: `create`'s `type` table, sample ops, `set_axis` fields, `save`, `list`, every `inspect` field, how numbers and booleans are read | before a call the workflows do not show, or when you need a result field |
| `batch` | `batch`'s `operations` shape, per-op errors, the result | before you call `batch` |
| `ue-facts` | what the engine does with samples and axes: range growth, which samples count as valid, smoothing types, Use Grid, sync markers, aim offset additive rules, version notes, with sources | when a sample is refused, the axes changed on their own, or you choose smoothing settings |
| `failures` | error table and symptom → cause → fix table | when a call fails or the asset is not what you expect |

## Before the first call

- **Result shape.** Success is the message, a blank line, then JSON. Through
  the task queue (the default) that JSON is a task envelope and the tool's
  fields sit under `data` (skill `ue-tasks`). **Failure is only
  `Error: <message>`, with no data.**
- **Nothing saves except `save`** (or `asset` `save_asset`, skill `ue-assets`).
  Every write lives only in the running editor until then.
- **No undo.** No operation opens an editor transaction. Undo with the inverse
  operation.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`; the work may still
  finish. `inspect` before you retry.
- `operation` is lowercased; `get`, `get_info`, `info`, `get_samples` mean
  `inspect`. `blueprint_path` or `path` stand in for a missing `asset_path`.

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

`type` for `create`: `BlendSpace1D`, `BlendSpace` (2D), `AimOffsetBlendSpace1D`,
`AimOffsetBlendSpace`. Paths go to the engine's loader as given, no `/Game/`
fallback; use the object path the results report
(`/Game/Anims/BS_Walk.BS_Walk`). Samples are animation sequences only — no
montages, no nested blend spaces. Send numbers and booleans as JSON numbers and
booleans (section `operations` says what a string turns into).

## Must not get wrong

1. **Set the axes before you add samples.** A new asset has the engine's
   default axes (0–100, 4 divisions). A sample outside the range makes the
   engine grow the range one grid step at a time **and add a grid division per
   step**, so `add_sample` at x=600 on a fresh asset leaves an axis 0–600 with
   24 divisions (section `ue-facts`).
2. **`move_sample` without `y` moves a 2D sample to y=0.** Always pass both.
3. **`rate_scale` left out, 0 or negative means "do not touch"**, not 1.0 —
   the schema's stated default of 1.0 is not what the code does. Values above
   0 are clamped to 0.01–64.
4. **`remove_sample` moves the last sample into the freed index**, and every
   rebuild also drops samples sitting on the same position. Re-read indices
   with `inspect` after any removal before the next index-based call.
5. **An unknown `interp_type` is ignored silently.** Use `Average`, `Linear`,
   `Cubic`, `EaseInOut`, `ExponentialDecay` or `SpringDamper` (any case).
6. **`create`'s `type` is matched by substring.** Anything without `1D` or
   `AimOffset` creates a 2D `BlendSpace`. Check the result's `type`.
7. **`inspect` and `list` never say "aim offset".** `inspect.type` is
   `BlendSpace1D` or `BlendSpace`; `list` calls even a 1D aim offset
   `BlendSpace`. Track what you created, or check the class with `asset_search`.
8. **Aim offset samples must be additive, Mesh Space** (additive type
   `RotationOffsetMeshSpace`); a plain sequence is refused with
   `AddSample failed …`. Prepare the sequences first (skill `ue-anim-sequence`).
9. **A failed `batch` hides which op failed.** The call is an error with only
   `Batch: <n> OK, <m> errors, <t> total`; the ops that worked stay applied.
10. **Narrowing an axis does not remove samples outside it**; the engine marks
    them invalid and they stop contributing, and `inspect` does not show the
    flag. Keep every sample inside the range you set.

## Decision rules

- **One driving value** (speed only, or pitch only): `BlendSpace1D` /
  `AimOffsetBlendSpace1D`. **Two** (speed and direction, yaw and pitch): 2D.
- **Full-body cycles that replace the pose** (walk, run, strafe): a blend
  space. **Additive poses layered on top** (aim, look): an aim offset.
- **Several changes on one asset**: one `batch` (one geometry rebuild), then
  `inspect`. **One change you need the error of**: the single operation.
- **New sample vs. new animation on a slot**: `set_sample_animation` keeps the
  position and index and checks the skeleton; `remove_sample` + `add_sample`
  shuffles indices.
- **Smoothing**: start with `SpringDamper`, `interp_time` 0.1–0.2,
  `damping_ratio` 1.0 on the speed axis; `max_speed` limits how fast the input
  may change (section `ue-facts` has what each knob does).

## The edit loop

1. **Find or create.** `list` (`recursive: true` for a tree) or
   `asset_search` with `class_filter: "BlendSpace"` (subclasses included,
   skill `ue-assets`). Only then `create`; the tool does not check for an
   existing asset of that name.
2. **Read.** `inspect`: axes, sample indices, positions.
3. **Axes first** (`set_axis`), then samples. Several changes: one `batch`.
4. **Verify** with `inspect`:
   - every `axes[]` has the `min`, `max`, `grid_divisions` and
     `interpolation.type` you meant (no silent range growth);
   - `sample_count` and each `samples[]` `animation` / `position` match your
     plan, no sample shows `None`;
   - with `settings.interpolate_using_grid` false (new assets),
     `geometry.triangles` is above 0 on a 2D blend space whose samples span an
     area, `geometry.segments` above 0 on a 1D one with two or more samples;
     0 means the engine built no blend geometry from them;
   - `anim_length` is above 0 (it counts valid samples only, so 0 with samples
     present means none of them is valid).
5. **Save.** `save`, then confirm with `inspect` after a later reload if it
   matters.

Playing the blend space and binding its axes to variables is
`anim_blueprint_modify`'s job (skill `ue-anim-blueprint`; the plugin's
`Resources/domains/blueprints.md`, "Blend Space Parameter Binding", says a 1D
node's input pin is `X` whatever the axis is called).

## When a call fails

Load section `failures`: the error table and the symptom table. When
`add_sample` or `move_sample` is refused, section `ue-facts` lists the engine's
reasons (duplicate position, out of range, skeleton, additive type).

## See also

- `ue-anim-blueprint`: the blend space player, Aim Offset node and axis
  bindings in an Animation Blueprint.
- `ue-anim-sequence`: the sequences used as samples, additive settings for an
  aim offset, sync markers.
- `ue-assets`: finding, saving, renaming and deleting assets.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
