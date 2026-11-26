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

Written from the toolkit's C++ source (`MCPTool_MontageModify.{h,cpp}`,
`MontageEditor.cpp`) and the UE 5.8.4 engine source; not exercised against a
running editor. The toolkit has no version-specific code here, so 5.6 to 5.8
behave alike as far as the tool is concerned.

## Sections of this skill

| Section | What is in it | Load it when |
|---|---|---|
| `workflows` | end-to-end recipes: attack montage with a hit window, section combo, notifies gameplay reacts to, upper-body slot, root motion, how to verify | you build a montage for a gameplay purpose, not just one edit |
| `structure` | `get_info` fields, what `create` makes, sections, slots and segments, blends, `save`, sources | before you add, link or remove sections, slots or segments, or need a result field |
| `notifies` | the notify operations, `properties` and how each value type is read | before you add, move, remove or configure a notify |
| `curves` | `get_curves`, `add_curve`, `remove_curve`, `set_curve_keys`, the key format | before you touch a float curve |
| `failures` | error table, failure table (what you see, what it means, what to do), how numbers are read | a call fails in a way you cannot place, or the montage is not what you expect |

## Must not get wrong

1. **`add_notify_state` takes `start_time`, not `trigger_time`.** The tool's
   own description shows `trigger_time` in its notify-state example
   (`MCPTool_MontageModify.h:54`); the code reads `start_time` and `duration`
   and fails with `Missing required parameter: start_time`
   (`MCPTool_MontageModify.cpp:537-547`). `add_notify` takes `trigger_time`.
2. **Segments before everything timed.** A new montage has length 0. Notify
   times are checked against the length, and section times are measured to it;
   the length comes only from the segments.
3. **Nothing saves except `save`** (or `asset` `save_asset` / `save_all`,
   skill `ue-assets`). No operation opens an editor transaction, so there is
   no undo: reverse a change with the inverse operation.
4. **A new montage is not empty**: slot `DefaultSlot` (index 0), section
   `Default` at 0 that cannot be removed or renamed, blend in and out 0.25 s.
5. **`add_section` auto-links and does not sort.** The engine links the
   previous list entry to the new section if it had no link, and the list
   stays in the order you add. Add sections in time order, then set or clear
   links with `link_sections`.
6. **`add_segment` without `start_pos` appends** after the last segment on
   that slot, despite the `0` default in the parameter list.
7. **Silent fallbacks.** An unknown `blend_option` becomes `Linear`; an
   unknown curve `interp_mode` / `tangent_mode` becomes `Linear` / `Auto`; a
   curve key without a readable `time` or `value` is dropped. None of these
   errors. Check the data returned.
8. **The tool cannot point a notify property at an asset.** An object value
   always creates a new sub-object, so a Sound or particle notify cannot get
   its sound or system here. Nor can it set a notify's tick type (queued or
   branching point), trigger chance or filters.
9. **The montage plays only through a Slot node** of the same slot name in
   the character's Animation Blueprint (Epic, "Animation Slots"); the tool
   neither checks nor registers slots on the skeleton.

## Calling the tool

- `operation` is lowercased; `info`, `inspect` and `get` run `get_info`. An
  unknown operation returns `Unknown operation: '<op>'. Valid: <list>`, plus
  ` Did you mean '<x>'?` when one is close.
- Every operation except `create` needs `montage_path`; `asset_path`,
  `blueprint_path` and `path` are accepted in its place. The schema marks it
  required for `create` too; `create` ignores it, so send any value if your
  client insists.
- **Use the object path**, `/Game/Anims/AM_Attack.AM_Attack`. `montage_path`
  is first run through the Blueprint path check: empty, over 512 characters,
  under `/Engine/` or `/Script/` (`Cannot access engine or script Blueprints`),
  containing `..` or any of `` <>|&;`$(){}[]!*?~ `` is refused. **Only
  `/Game/` content loads**: any other path is tried as `/Game/Animations/<path>`,
  then under `/Game/Characters`, `/Game/Characters/Animations`,
  `/Game/Assets/Animations` and `/Game` (`AnimAssetManager.{h,cpp}`). Plugin
  content cannot be edited, and a bare name may load a same-named montage from
  one of those folders: check `path` in the result. A failed load reads
  `Failed to load AnimMontage '<path>'. Use full path like '/Game/Animations/MyAsset.MyAsset'.`
  and does not prove absence: `asset_search` with `class_filter: "AnimMontage"`.
- `animation_path` loads only animation sequences, the same way, without the
  path check.
- Send numbers as JSON numbers and objects as objects (section `failures`
  says what else is accepted).
- **Results.** Success is the message, a blank line, then JSON. Through the
  task queue (the bridge's default) the tool's fields sit under `data` of the
  task envelope (skill `ue-tasks`). Failure is `Error: <message>` only. Most
  writes return the whole montage in `get_info` shape; curve operations
  return the curves, `set_blend_in` / `set_blend_out` only what they set.
- After 300 s the bridge returns `Error: Task timed out after 300000ms …`; that
  ends the wait, not the work. `get_info` before you retry.

## Operations

| Operation | Required besides `montage_path` | Optional |
|---|---|---|
| `get_info` | — | — |
| `create` | `skeleton_path`, `package_path`, `montage_name` | — |
| `save` | — | — |
| `add_section` | `section_name` | `start_time` (0) |
| `remove_section` | `section_name` | — |
| `link_sections` | `section_name` | `next_section` (absent: unlink) |
| `add_segment` | `animation_path` | `slot_index` (0), `start_pos` (absent: append) |
| `set_segment` | `segment_index` | `slot_index` (0), `animation_path`, `play_rate`, `anim_start_time`, `anim_end_time`, `start_pos` |
| `remove_segment` | `segment_index` | `slot_index` (0) |
| `add_slot` | `slot_name` | — |
| `remove_slot` | `slot_index` | — |
| `set_blend_in`, `set_blend_out` | `blend_time` | `blend_option` (`Linear`) |
| `add_notify` | `notify_name`, `trigger_time` | `track_index` (0), `notify_class`, `properties` |
| `add_notify_state` | `notify_name`, `start_time`, `duration` | `track_index` (0), `notify_class`, `properties` |
| `remove_notify` | `notify_name` | — |
| `move_notify` | `notify_index`, `track_index` | — |
| `rename_track` | `track_index`, `track_name` | — |
| `cleanup_tracks` | — | — |
| `set_notify_properties` | `notify_index`, `properties` | — |
| `get_curves` | — | — |
| `add_curve` | `curve_name` | `keys` |
| `remove_curve` | `curve_name` | — |
| `set_curve_keys` | `curve_name`, `keys` | — |

Load the matching section before the first call of a group; the table only
names the parameters.

## The edit loop

1. **Find or create.** `asset_search` (`class_filter: "AnimMontage"`), or
   `create`.
2. **Read.** `get_info`: slots, segment and section indices, notifies.
3. **Segments first** (`add_segment`; check `sequence_length`).
4. **Then sections and links** (in time order), **notifies, blends, curves.**
   Take indices from the data each write returns; removals shift them.
5. **Verify** with `get_info` against what you meant (section `workflows`,
   "Verify your own result"), then **`save`**.

## Background in the plugin

The plugin's `Resources/domains/tooling.md` lists `montage_modify`.
`unreal_get_ue_context` with `category: "animation"` serves Animation
Blueprint notes, in which montages appear only as a keyword (skill
`ue-context`).

## See also

- `ue-anim-blueprint`: Slot nodes (`add_anim_node`, `animation_type: "slot"`)
  through which an Animation Blueprint plays a montage.
- `ue-anim-sequence`: the sequences used as segments; root bone inspection.
- `ue-retarget`: `inspect_anim` and `set_root_motion` for a sequence's root
  motion flag.
- `ue-assets`: finding, saving, renaming and deleting assets.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
