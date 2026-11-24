---
name: ue-anim-montage
description: >
  Create and edit Animation Montages with montage_modify: sections and their
  links, slot tracks and segments, notifies and notify states, blend in and
  out, float curves. Use before you build or change a montage, or when a
  montage_modify call returns an error.
toolkit_tools: [montage_modify]
---

# UE montages: montage_modify

The UE LLM Toolkit plugin's **`montage_modify`** tool reads and edits
Animation Montage assets: composite sections and the order they play in, slot
tracks and the animation segments on them, notifies, blend-in and blend-out,
and float curves. Through the plugin's MCP bridge it is listed as
`unreal_montage_modify`. It works only while the editor is running and the
plugin is reachable; `unreagent-iterate` covers the wait for the plugin.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

The tool takes an `operation` string and lowercases it before dispatch.
`info`, `inspect` and `get` run `get_info`. An unknown operation returns
`Unknown operation: '<op>'. Valid: <list>`, plus ` Did you mean '<x>'?` when a
valid name is close.

This skill has three sections. The parameters of the notify and curve
operations are only in their sections:

| Section | Covers | Load it |
|---|---|---|
| `notifies` | `add_notify`, `add_notify_state`, `remove_notify`, `move_notify`, `rename_track`, `cleanup_tracks`, `set_notify_properties` | before you add, remove or move a notify, set properties on one, or rename or clean up notify tracks |
| `curves` | `get_curves`, `add_curve`, `remove_curve`, `set_curve_keys` | before you read, add or remove a float curve or set its keys |
| `failures` | the error table, and the failure table (what you see, what it means, what to do) | when a call returns an error you cannot place, or the montage is not what you expect after a call |

## Before the first call

- **Result shape.** Success is the tool's message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope (`task_id`, `status`, `success`, `message`, …), and the tool's own
  fields, the ones named in this skill, sit under its `data` key. Failure is
  `Error: <message>` with no data. Skill `ue-tasks` covers the task tools.
- **Nothing saves except `save`.** Every write marks the montage as modified
  and lives only in the running editor until you call `save`, or the `asset`
  tool's `save_asset` or `save_all` (skill `ue-assets`).
- **No undo.** No operation opens an editor transaction. Undo a change with the
  inverse operation, such as `remove_section` after `add_section`.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the montage back with `get_info` before you retry (skill
  `ue-tasks`).

## Paths

Every operation except `create` needs `montage_path`. The tool takes
`asset_path`, `blueprint_path` or `path` in its place when `montage_path` is
absent.

- **The tool's schema marks `montage_path` as required for every operation**,
  `create` included, so a client that checks the schema may refuse a
  `create` call without it. Neither the bridge nor the plugin enforces this,
  and `create` never reads `montage_path`: when you have to send one, any
  value, such as the new montage's path, is ignored.
- **`montage_path` is checked before the operation runs**, with the Blueprint
  tools' path check:
  it is rejected when empty (`Missing required parameter: montage_path`),
  longer than 512 characters, under `/Engine/` or `/Script/`
  (`Cannot access engine or script Blueprints`), containing `..`, or containing
  any of `` <>|&;`$(){}[]!*?~ ``
  (`Blueprint path contains invalid character: '<c>'`). The messages say
  "Blueprint" for montages too. Engine montages cannot be edited.
- **Use the object path**, such as `/Game/Anims/AM_Attack.AM_Attack`; the
  tool's own error message asks for that form. `create` and `get_info` report
  it as `path`. Whether a package path without the `.AM_Attack` part loads is
  up to the engine's loader.
- **Only paths under `/Game/` are loaded as given.** Any other path is looked
  up as `/Game/Animations/<path>`, then under `/Game/Characters`,
  `/Game/Characters/Animations`, `/Game/Assets/Animations` and `/Game`. A bare
  name such as `AM_Attack` can therefore load a montage from one of those
  folders: check the `path` that `get_info` reports. **A montage or animation
  in a plugin's content folder cannot be loaded**, because its path does not
  start with `/Game/`.
- **A path that does not load as a montage**, another kind of asset included,
  returns
  `Failed to load AnimMontage '<path>'. Use full path like '/Game/Animations/MyAsset.MyAsset'.`
  A failed load does not prove the montage is missing: search with
  `asset_search` (`class_filter: "AnimMontage"`, skill `ue-assets`).

`animation_path` (segments) loads the same way, but only animation sequences:
`Failed to load AnimSequence '<path>'. …`. It skips the path check, but an
`/Engine/` animation still fails to load, because its path does not start with
`/Game/`.

## Operations

| Operation | Required besides `montage_path` | Optional |
|---|---|---|
| `get_info` | — | — |
| `create` | `skeleton_path`, `package_path`, `montage_name`; `montage_path` is not read (see "Paths") | — |
| `save` | — | — |
| `add_section` | `section_name` | `start_time` (default 0) |
| `remove_section` | `section_name` | — |
| `link_sections` | `section_name` | `next_section` |
| `add_segment` | `animation_path` | `slot_index` (default 0), `start_pos` |
| `remove_segment` | `segment_index` | `slot_index` (default 0) |
| `set_segment` | `segment_index` | `slot_index` (default 0), `animation_path`, `play_rate`, `anim_start_time`, `anim_end_time`, `start_pos` |
| `add_slot` | `slot_name` | — |
| `remove_slot` | `slot_index` | — |
| `set_blend_in`, `set_blend_out` | `blend_time` | `blend_option` (default `Linear`) |
| notify operations | section `notifies`; load it before the first one | |
| curve operations | section `curves`; load it before the first one | |

A missing or empty string parameter returns
`Missing required parameter: <name>`, and so does a missing number such as
`segment_index`. Send numbers and booleans as JSON numbers and booleans.

The tool reads the number parameters of an operation, those in the table
above and in the sections' tables, as a `double`, through
`ExtractOptionalNumber` (`MCPToolBase.h`) or the engine's
`FJsonObject::TryGetNumberField` directly, and truncates an index to an
integer afterwards. That read also takes a string made of an optional sign,
digits and at most one dot, such as `"0.3"`, and `true` and `false` as 1 and
0. **Any other string, such as `"1e3"` or `"0.3s"`, counts as absent**: a
required number returns `Missing required parameter: <name>`, and an optional
one keeps its default without an error (`TJsonValueString::TryGetNumber`,
`Engine/Source/Runtime/Json/Public/Dom/JsonValue.h`; read in the UE 5.8.4
source, not run, while the toolkit targets UE 5.6 and 5.7).
A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
(`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).

This holds for the operation's own parameters only. A value inside a notify's
`properties` is checked against the property's type, and a string that is not
numeric fails there with `Property '<key>' expects number` (section
`notifies`). A curve key whose `time` or `value` cannot be read is skipped
(section `curves`).

Most writes return the whole montage, shaped like `get_info`, as their data.
The curve operations return the curves instead, `set_blend_in` and
`set_blend_out` only what they set.

## get_info

Returns:

- `name`, `path`, `sequence_length` (the montage's length in seconds),
  `skeleton` (when the montage has one), `blend_in_time`, `blend_out_time`.
- `slot_tracks`: `index`, `slot_name`, and `segments`, each with `index`,
  `start_pos` (where it starts on the montage timeline), `anim_start_time`,
  `anim_end_time`, `anim_play_rate`, and `animation` and `animation_name`
  when the segment has an animation.
- `sections`: `index`, `name`, `start_time`, `length` (up to the start of the
  next section in the list, the last one up to `sequence_length`), and
  `next_section` (`None` when unlinked).
- `notifies` and `notify_tracks` (section `notifies`).
- `curves`: `name` and `key_count` per float curve. `get_curves` has the keys.

The blend options are not reported, only the blend times.

## create

Creates `<package_path>/<montage_name>` for the skeleton at `skeleton_path`,
for example `package_path: "/Game/Anims"`, `montage_name: "AM_Attack"`. The
result has `path` (the object path) and `name`. The message is
`Created montage: <path>`. The tool does not save the new montage.

- `package_path` is not checked, and the tool does not check whether an asset
  of that name already exists. What the engine does then is not covered here.
  Check with `asset_search` first.
- The tool adds no animation to the new montage and sets no slot tracks,
  sections or length. Read what it starts with through `get_info`, and add
  segments before notifies.
- Errors: `Failed to load skeleton: <path>`, `Failed to create package: <path>`,
  `FactoryCreateNew failed to create montage`.

## Sections

- **`add_section`** adds `section_name` at `start_time` seconds:
  `Section '<n>' already exists at index <i>`, or
  `Failed to add section '<n>'` when the engine refuses it.
- **`remove_section`** removes it by name. **The first section in the list
  cannot be removed**: `Cannot remove the first section (Default)`.
  Other sections' links to the removed one are not cleared.
- **`link_sections`** sets which section plays after `section_name`. An empty
  or absent `next_section` unlinks it (`Unlinked section '<n>'`). Both sections
  must exist: `Section '<n>' not found`, `Next section '<n>' not found`.

## Slot tracks and segments

A slot track is addressed by `slot_index`, its segments by `segment_index`, as
`get_info` lists them. Removing an entry shifts the indices after it down by
one.

- **`add_segment`** puts the whole of `animation_path` on slot `slot_index` at
  play rate 1. **Without `start_pos` (or with a negative one) the segment is
  appended** after the end of the last segment on that slot; the tool's
  parameter list claims a default of 0, but the code appends. The animation's
  skeleton must be compatible with the montage's:
  `Animation skeleton '<a>' is not compatible with montage skeleton '<m>'`.
  The tool does not check whether segments overlap.
- **`set_segment`** changes only the fields you pass. With `animation_path` it
  swaps the animation **without** a skeleton check and **keeps** the old
  `anim_start_time` and `anim_end_time`; set those to the new animation's
  range in the same call. The parameter list shows a `play_rate` default of
  1.0, but an absent `play_rate` is left unchanged. The values are not
  validated.
- **`remove_segment`** removes one segment.
- After each segment change the tool sets the montage's length to the end of
  the last segment on any slot. It does not move sections or notifies that
  now lie past the end; check them with `get_info`.
- **`add_slot`** appends an empty slot track named `slot_name`:
  `Slot '<n>' already exists`.
- **`remove_slot`** removes the slot at `slot_index`, with its segments. **The
  last slot track cannot be removed**: `Cannot remove the last slot track`.
- Index errors: `Slot index <n> out of range (0-<last>)` (`add_segment`),
  `Slot index <n> out of range` (the others), `Segment index <n> out of range (0-<last>)`
  (`remove_segment`), `Segment index <n> out of range` (`set_segment`).

The slot name is what an Animation Blueprint's slot node plays the montage
through; skill `ue-anim-blueprint` covers adding one.

## Blend in and blend out

`set_blend_in` and `set_blend_out` set `blend_time` in seconds and the curve
`blend_option`: `Linear`, `Cubic`, `HermiteCubic`, `Sinusoidal`,
`QuadraticInOut`, `CubicInOut`, `QuarticInOut`, `QuinticInOut`, `CircularIn`,
`CircularOut`, `CircularInOut`, `ExpIn`, `ExpOut` or `ExpInOut`,
case-insensitive.

- **An unknown `blend_option` becomes `Linear` without an error**, and the
  result still echoes the string you sent as `blend_option`. `get_info` does
  not report the option either, so check the spelling before you send it.
- The result has `blend_in_time` or `blend_out_time`, and `blend_option`.

## save

Saves the montage's package to disk: `Saved montage: <montage_path>`. Errors:
`Failed to resolve filename for package: <package>`,
`SavePackage failed for: <file>`.

## The edit loop

1. **Find or create.** `asset_search` with `class_filter: "AnimMontage"`, or
   `create`.
2. **Read.** `get_info`: slots, segment and section indices, notifies.
3. **Segments first.** Notify and section times are checked against, or
   measured to, the montage's length, which comes from the segments.
4. **Then sections, links, notifies, blends and curves.** Read the indices from
   the data each write returns.
5. **Save.** `save`.

## Errors and failure modes

Load section `failures` when a call returns an error you cannot place, or
the montage is not what you expect after a call: it has the error table and
the failure table (what you see, what it means, what to do). The errors of
the notify and curve operations are in their own sections.

## Background in the plugin

The plugin's `Resources/domains/tooling.md` lists `montage_modify` among its
tools. `unreal_get_ue_context` with `category: "animation"` serves the
plugin's Animation Blueprint notes; montages appear there only as a keyword
(skill `ue-context`).

## See also

- `ue-anim-blueprint`: slot nodes, through which an Animation Blueprint plays
  a montage.
- `ue-anim-sequence`: the animation sequences used as segments.
- `ue-assets`: finding, saving, renaming and deleting assets.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
