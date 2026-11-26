# montage_modify: when a call fails

Paths are in the skill itself; the errors of the notify and curve operations
are in sections `notifies` and `curves`, those of sections, slots and
segments in `structure`.

## Errors

| Error | Cause | Fix |
|---|---|---|
| `Missing required parameter: start_time` | `add_notify_state` sent with `trigger_time`, as the tool's own example does | send `start_time` and `duration` |
| `Missing required parameter: <name>` | a required parameter is absent, an empty string, or a number sent as a string the tool cannot read (see below) | send it as a JSON number or non-empty string |
| `Cannot access engine or script Blueprints` | `montage_path` under `/Engine/` or `/Script/` | engine montages cannot be edited; duplicate into `/Game/` (skill `ue-assets`) |
| `Blueprint path contains invalid character: '<c>'` / `… path traversal sequences` | the Blueprint path check, applied to montages too | use a plain object path |
| `Failed to load AnimMontage '<path>'. …` | no montage there, a non-`/Game/` path, or another asset class | object path from `asset_search` |
| `Failed to load AnimSequence '<path>'. …` | `animation_path` is not a sequence under `/Game/` | a montage or blend space cannot be a segment |
| `Animation skeleton '<a>' is not compatible with montage skeleton '<m>'` | `add_segment` with another skeleton's animation | retarget it first (skill `ue-retarget`), or add the skeleton to Compatible Skeletons |
| `Section '<n>' already exists at index <i>` | duplicate name | pick another name |
| `Section '<n>' not found` / `Next section '<n>' not found` | misspelt name | names from `get_info` |
| `Cannot remove the first section (Default)` | `remove_section` on list entry 0 | keep it; use it as the first section |
| `Cannot remove the last slot track` | `remove_slot` with one track left | `add_slot` the replacement first |
| `Slot index …` / `Segment index … out of range` | stale or wrong index | `get_info`, then retry |
| `Trigger time <t> out of range (0-<len>)` / `Notify state range (…) exceeds montage length …` | notify past the end, often length 0 | add segments first |
| `Failed to load notify class: <v>` | short name of a non-`Engine` class, or a Blueprint path without `_C` | full class path |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools missing, every call fails | editor not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a change is gone after an editor restart | never saved | `save` after each finished change |
| every notify is out of range | `sequence_length` is 0 or short | add the segments first |
| the montage plays all sections in a row | `add_section` auto-linked each to the next | `link_sections` without `next_section` to unlink |
| a section shows a negative `length` | sections added out of time order | remove and re-add in time order |
| a segment starts at an unexpected time | `add_segment` without `start_pos` appends | pass `start_pos` |
| a swapped segment plays the wrong range | `set_segment` kept the old animation times | pass `anim_start_time` / `anim_end_time` with `animation_path` |
| the blend curve is linear | `blend_option` misspelt | use a listed name |
| a bare montage name loaded an unexpected asset | found in a fallback folder | full object path |
| the montage does nothing in game | no Slot node with its slot name in the AnimBP | add the Slot node (skill `ue-anim-blueprint`) |
| another montage stops when this one starts | both slots in the same slot group (unregistered slots count as `DefaultGroup`) | separate groups in the skeleton's Anim Slot Manager |
| Play Montage's On Notify Begin never fires | the notify is not a Montage Notify, or the montage was played with Play Anim Montage | `AnimNotify_PlayMontageNotify` class and the Play Montage node |
| a Play Sound notify is silent | the tool cannot set `Sound` | set it in the montage editor or Python |
| the character slides or stays put during an attack | root motion off on the sequence, or the AnimBP's Root Motion Mode excludes montages | section `workflows`, 5 |
| `notify_index` points at another notify after a reload | the engine sorted notifies by time on load | `get_info` again |

## How number parameters are read

The tool reads numbers as a `double`, through `ExtractOptionalNumber`
(`MCPToolBase.h`) or the engine's `FJsonObject::TryGetNumberField`, and
truncates indices to integers. That read also takes a string made of an
optional sign, digits and at most one dot (`"0.3"`), and `true` / `false` as 1
and 0. **Any other string, such as `"1e3"` or `"0.3s"`, counts as absent**: a
required number returns `Missing required parameter: <name>`, an optional one
keeps its default silently. A lone sign or dot (`"-"`, `"."`) reads as 0
(`TJsonValueString::TryGetNumber`, `Json/Public/Dom/JsonValue.h`;
`TCString::IsNumeric`, `Core/Public/Misc/CString.h`; UE 5.8.4 source, read
not run).

This holds for the operation's own parameters. Values inside a notify's
`properties` are checked against the property's type (section `notifies`);
curve keys whose `time` or `value` cannot be read are skipped (section
`curves`).
