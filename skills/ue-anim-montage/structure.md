# montage_modify: structure reference

Everything about `get_info`, `create`, sections, slot tracks, segments, blends
and `save` beyond the common path in the skill itself. Notifies and curves
have their own sections.

## get_info

Returns:

- `name`, `path` (the object path), `sequence_length` (the montage's length in
  seconds), `skeleton` (when the montage has one), `blend_in_time`,
  `blend_out_time`.
- `slot_tracks`: `index`, `slot_name`, and `segments`, each with `index`,
  `start_pos` (where it starts on the montage timeline), `anim_start_time`,
  `anim_end_time`, `anim_play_rate`, and `animation` and `animation_name`
  when the segment has an animation.
- `sections`: `index`, `name`, `start_time`, `length`, and `next_section`
  (`None` when unlinked).
- `notifies` and `notify_tracks` (section `notifies`).
- `curves`: `name` and `key_count` per float curve. `get_curves` has the keys.

Not reported: the blend options (only the times), the montage's blend modes,
auto blend-out, sync group, each notify's tick type, the skeleton's slot
groups. (`FMontageEditor::SerializeMontageInfo`, toolkit
`Private/MontageEditor.cpp:104-230`.)

**`length` is computed from list order.** Each section's `length` runs to the
`start_time` of the next entry in the list, the last one to
`sequence_length`. The tool never sorts sections by time (see "Sections"), so a
section added out of time order shows a wrong, even negative, `length`.

## create

Creates `<package_path>/<montage_name>` for the skeleton at `skeleton_path`
through the engine's montage factory. The result has `path` (the object path)
and `name`; the message is `Created montage: <path>`. The tool does not save
the new montage.

**What a new montage holds** (read in the UE 5.8.4 engine source, not run):

| Field | Value | Source |
|---|---|---|
| slot tracks | one, `DefaultSlot`, no segments | `UAnimMontage` constructor, `AnimMontage.cpp` (`AddSlot(FAnimSlotGroup::DefaultSlotName)`) |
| sections | one, `Default`, at 0 | `UAnimMontageFactory::EnsureStartingSection`, `AnimMontageFactory.cpp` |
| blend in / out | 0.25 s each | `UAnimMontage` constructor |
| length | 0 until a segment is added | the tool adds no animation |

- `skeleton_path`, `package_path` and `montage_name` are required; `montage_path`
  is not read (the schema still marks it required, so send any value if your
  client insists).
- `package_path` is not checked, and the tool does not check whether an asset
  of that name already exists. What the engine does then is not covered here.
  Check with `asset_search` first.
- Errors: `Failed to load skeleton: <path>`, `Failed to create package: <path>`,
  `FactoryCreateNew failed to create montage`.

## Sections

| Operation | Required besides `montage_path` | Optional |
|---|---|---|
| `add_section` | `section_name` | `start_time` (default 0) |
| `remove_section` | `section_name` | — |
| `link_sections` | `section_name` | `next_section` (absent or empty unlinks) |

- **`add_section` appends to the list, unsorted**, and the engine then links
  the entry before it in the list to the new section, if that entry had no
  link yet (`UAnimMontage::AddAnimCompositeSection`, `AnimMontage.cpp`, UE
  5.8.4 source). In a new montage, `Default` therefore links to the first
  section you add, that one to the next, and so on: a chain that plays
  straight through. Add sections in time order, then set the links you want.
  The montage editor sorts sections by time when a person edits them; the
  tool does not call that sort.
- Errors: `Section '<n>' already exists at index <i>`,
  `Failed to add section '<n>'` (the engine refused it).
- **`remove_section`** removes by name. **The first section in the list cannot
  be removed**: `Cannot remove the first section (Default)`. Other sections'
  links to the removed one are not cleared; re-link them.
- **There is no rename.** Keep `Default` as the first section's name and use
  it in gameplay code, or plan the names before you add sections.
- **`link_sections`** sets which section plays after `section_name`. Both
  must exist: `Section '<n>' not found`, `Next section '<n>' not found`. The
  tool accepts a section linked to itself; that it then loops is unverified
  here. The message is `Linked section '<a>' -> '<b>'` or
  `Unlinked section '<a>'`.

At runtime, gameplay picks sections with the anim instance's
`Montage_JumpToSection` (jump now) and `Montage_SetNextSection` (change what
follows when the current section ends); both exist in `UAnimInstance`
(`AnimInstance.cpp`, UE 5.8.4 source). An unlinked section ends the montage
when it finishes, which then blends out.

## Slot tracks and segments

A slot track is addressed by `slot_index`, its segments by `segment_index`, as
`get_info` lists them. Removing an entry shifts the indices after it down.

| Operation | Required besides `montage_path` | Optional |
|---|---|---|
| `add_segment` | `animation_path` | `slot_index` (default 0), `start_pos` |
| `set_segment` | `segment_index` | `slot_index` (default 0), `animation_path`, `play_rate`, `anim_start_time`, `anim_end_time`, `start_pos` |
| `remove_segment` | `segment_index` | `slot_index` (default 0) |
| `add_slot` | `slot_name` | — |
| `remove_slot` | `slot_index` | — |

- **`add_segment`** puts the whole of `animation_path` on slot `slot_index` at
  play rate 1. **Without `start_pos` (or with a negative one) the segment is
  appended** after the end of the last segment on that slot
  (`MCPTool_MontageModify.cpp:323-337`); the tool's parameter list claims a
  default of 0 (`MCPTool_MontageModify.h:83`), but the code appends. The
  animation's skeleton must be compatible with the montage's:
  `Animation skeleton '<a>' is not compatible with montage skeleton '<m>'`.
  Compatible means the same skeleton, one listed in the montage skeleton's
  Compatible Skeletons, or an editor setting that makes all skeletons
  compatible (`USkeleton::IsCompatibleForEditor`, `Skeleton.cpp`, UE 5.8.4
  source). Segments may overlap; the tool does not check.
- **`set_segment`** changes only the fields you pass. With `animation_path` it
  swaps the animation **without** a skeleton check and **keeps** the old
  `anim_start_time` and `anim_end_time`; set those to the new animation's
  range in the same call. An absent `play_rate` is left unchanged, despite the
  `1.0` default in the parameter list. The values are not validated: an
  `anim_end_time` past the animation's length, or a negative `play_rate`, is
  written as sent.
- After each segment change the tool sets the montage's length to the end of
  the last segment on any slot (`RecalculateSequenceLength`,
  `MontageEditor.cpp:1016-1043`). It does not move sections or notifies that
  now lie past the end; check them with `get_info`.
- **`add_slot`** appends an empty slot track named `slot_name`:
  `Slot '<n>' already exists`. The tool does not register the name on the
  skeleton (the Anim Slot Manager). A slot name the skeleton does not list
  counts as group `DefaultGroup` (`USkeleton::GetSlotGroupName`,
  `Skeleton.cpp`, UE 5.8.4 source).
- **`remove_slot`** removes the slot at `slot_index`, with its segments. **The
  last slot track cannot be removed**: `Cannot remove the last slot track`.
- Index errors: `Slot index <n> out of range (0-<last>)` (`add_segment`),
  `Slot index <n> out of range` (the others),
  `Segment index <n> out of range (0-<last>)` (`remove_segment`),
  `Segment index <n> out of range` (`set_segment`).

**Slots and groups at runtime** (Epic, "Animation Slots in Unreal Engine",
see the links below): slots are stored on the skeleton; montages play only
through Slot nodes in the Animation Blueprint's AnimGraph; playing a montage
on a slot of the same group as a running montage stops the running one. The
montage's group is the group of its first slot track
(`UAnimMontage::GetGroupName`, `AnimMontage.cpp`). An AnimBP gets a Slot node
with `anim_blueprint_modify` `add_anim_node`, `animation_type: "slot"` and
`slot_name` (default `DefaultSlot`; skill `ue-anim-blueprint`).

## Blend in and blend out

`set_blend_in` and `set_blend_out` take `blend_time` (seconds, required) and
`blend_option` (default `Linear`): `Linear`, `Cubic`, `HermiteCubic`,
`Sinusoidal`, `QuadraticInOut`, `CubicInOut`, `QuarticInOut`, `QuinticInOut`,
`CircularIn`, `CircularOut`, `CircularInOut`, `ExpIn`, `ExpOut` or `ExpInOut`,
case-insensitive.

- **An unknown `blend_option` becomes `Linear` without an error**
  (`ParseBlendOption`, `MontageEditor.cpp:1045-1062`), and the result still
  echoes the string you sent. `get_info` does not report the option.
- `blend_time` is not validated; a negative value is written as sent.
- The result has only `blend_in_time` or `blend_out_time`, and `blend_option`.
- Blend modes (standard or inertialization), blend profiles, auto blend-out and
  the blend-out trigger time are not reachable through this tool.

## save

Saves the montage's package: `Saved montage: <montage_path>`. Errors:
`Failed to resolve filename for package: <package>`,
`SavePackage failed for: <file>`. The `asset` tool's `save_asset` and
`save_all` save it too (skill `ue-assets`).

## Sources

- Toolkit (`ue-llm-toolkit`, branch `main`):
  `Source/UELLMToolkit/Private/MCP/Tools/MCPTool_MontageModify.h` and `.cpp`,
  `Source/UELLMToolkit/Private/MontageEditor.cpp`,
  `Source/UELLMToolkit/Private/AnimAssetManager.{h,cpp}`.
- Engine, UE 5.8.4 source, read not run (the toolkit targets 5.6 and up and
  has no version-specific code in these files):
  `Runtime/Engine/Private/Animation/AnimMontage.cpp`,
  `Editor/UnrealEd/Private/Factories/AnimMontageFactory.cpp`,
  `Runtime/Engine/Private/Animation/Skeleton.cpp`.
- Epic documentation (UE 5.9 pages, read 2026-11):
  https://dev.epicgames.com/documentation/en-us/unreal-engine/animation-montage-in-unreal-engine,
  https://dev.epicgames.com/documentation/en-us/unreal-engine/animation-slots-in-unreal-engine.
