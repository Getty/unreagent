# montage_modify: when a call fails

Paths, how number parameters are read, and the passage on each operation
are in the skill itself. The errors of the notify and curve operations are
in sections `notifies` and `curves`.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent, or an empty string |
| `Cannot access engine or script Blueprints` | `montage_path` under `/Engine/` or `/Script/` |
| `Failed to load AnimMontage '<path>'. …` | no montage at that path, or a path outside `/Game/`; use the object path |
| `Failed to load AnimSequence '<path>'. …` | `animation_path` is not an animation sequence |
| `Animation skeleton '<a>' is not compatible with montage skeleton '<m>'` | `add_segment` with an animation for another skeleton |
| `Section '<n>' not found` / `Next section '<n>' not found` | misspelt section name; names come from `get_info` |
| `Cannot remove the first section (Default)` | `remove_section` on the first section |
| `Cannot remove the last slot track` | `remove_slot` with one slot left |
| `Slot index …` / `Segment index … out of range` | stale or wrong index |
| `Trigger time <t> out of range (0-<len>)` | a notify past the montage's length (section `notifies`) |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a change is gone after an editor restart | it was never saved | `save` after each finished change |
| every notify is out of range | the montage is shorter than the notify times; check `sequence_length` | add the segments first |
| a segment started at an unexpected time | `add_segment` without `start_pos` appends after the last segment | pass `start_pos` |
| a swapped segment plays the wrong range | `set_segment` kept the old animation times | pass `anim_start_time` and `anim_end_time` with `animation_path` |
| the blend curve is linear | `blend_option` was misspelt | use one of the listed names |
| a bare montage name loaded an unexpected asset | the name was found in one of the fallback folders | use the full object path |
