---
name: ue-character
description: >
  Read and tune ACharacter actors placed in the open level with character:
  list them, read mesh, capsule, animation and components, and change
  movement values (walk speed, jump, friction, gravity). Use before you tune
  a placed character's movement, or to find out what a character consists of.
toolkit_tools: [character]
---

# UE characters: character

The UE LLM Toolkit plugin's **`character`** tool reads `ACharacter` actors in
the level that is open in the editor and changes ten values of their movement
component. Through the plugin's MCP bridge it is listed as
`unreal_character`. It works only while the editor is running and the plugin
is reachable; `unreagent-iterate` covers the wait for the plugin.

## Recipes

**Find a character and read what it is made of**

1. `{"operation": "list_characters"}`. Take `name` or `label` from an entry
   of `characters`.
2. `{"operation": "get_character_info", "character_name": "<name>"}` for
   class, transform, mesh, capsule, animation class and movement values.
3. `{"operation": "get_components", "character_name": "<name>"}` for the
   component list; add `"component_class": "Skeletal"` to filter.

**Tune the movement of a placed character**

1. `{"operation": "get_movement_params", "character_name": "<name>"}` and
   note the old values.
2. `{"operation": "set_movement_params", "character_name": "<name>", "max_walk_speed": 800, "jump_z_velocity": 600}`.
   Pass only the values you change.
3. Compare the returned `movement` with what you sent: values are clamped
   without a message (see "set_movement_params").
4. Save the level: `asset` with `{"operation": "save_all"}` (skill
   `ue-assets`). The tool marks the level dirty and saves nothing.

**The character is not in the list**

`list_characters` returned `total: 0`, or not the one you play. The tool reads
the editor's level, not a running game. A pawn that the game mode spawns at a
Player Start exists only during play and is never listed. Change that
character's class instead: `blueprint_modify` with `set_component_default` on
the Blueprint's movement component, whose name `blueprint_query`
`get_components` shows (skill `ue-blueprints`, section `modify`).
Which class the game spawns: `game_framework` `inspect_game_mode` (skill
`ue-game-framework`).

## What a success does not prove

- **Nothing is saved.** `set_movement_params` writes the value into the actor
  in memory and marks the actor's package and the level dirty. It is lost
  when the level is closed without saving.
- **It changes one placed actor, not the class.** Other actors of the same
  Blueprint, and every character spawned later, keep the class defaults.
- **It reads and writes the editor world, also while Play In Editor runs.**
  Every operation takes the editor's own world. During PIE the tool still
  shows and changes the level's actors, not their copies in the running game,
  so `velocity`, `movement_mode`, `is_falling` and `can_jump` are not the
  state of the game. What a change made during PIE does to the running
  session cannot be read from the source; make it before `start_pie` (skill
  `ue-pie-testing`).
- **Values are clamped silently.** The call succeeds with the clamped value,
  and `modified_params` still names the parameter.
- **A value the tool cannot read as a number is skipped, not rejected.** It
  does not appear in `modified_params`, and the call still succeeds when
  another one was applied. Whether a numeric string such as `"800"` can be
  read as a number depends on the engine's JSON classes, which this skill has
  not verified. Send JSON numbers, and check `modified_params`.
- **No undo.** The change is not recorded as an editor transaction. Restore
  the values you read in step 1.

## Operations

| Operation | Required | Optional |
|---|---|---|
| `list_characters` | — | `class_filter`, `limit`, `offset` |
| `get_character_info` | `character_name` | — |
| `get_movement_params` | `character_name` | — |
| `set_movement_params` | `character_name`, at least one movement value | the ten movement values |
| `get_components` | `character_name` | `component_class` |

`operation` must be one of these names; there are no aliases. Anything else
returns
`Unknown operation: '<op>'. Valid: list_characters, get_character_info, get_movement_params, set_movement_params, get_components`.

### character_name

The actor's object name (`BP_Hero_C_0`) or its label in the outliner
(`BP_Hero`), compared whole. The first actor that matches wins, so two actors
with the same label are not told apart: use the object name. Only `ACharacter`
actors are searched; any other actor returns `Character not found: <name>`.

The name is checked before the search: at most 256 characters, no control
characters, and none of `` <>|&;`$(){}[]!*?~ ``
(`Actor name contains invalid character: '<c>'`). A label with one of these,
such as `Hero (2)`, cannot be passed; use the object name.

### list_characters

| Parameter | Default | Meaning |
|---|---|---|
| `class_filter` | none | substring of the class name; a Blueprint class is named `BP_Hero_C` |
| `limit` | 100 | clamped to 1–1000 |
| `offset` | 0 | entries to skip; a negative value counts as 0 |

The result has `characters`, `count` (entries returned), `total` (all
matches), `offset` and `limit`. Each entry has `name`, `label`, `class`,
`class_path`, `location`, `rotation`, `can_jump` and `is_crouched`. The
message's "showing a-b of n" is wrong for an empty page; read `count` and
`total`.

### get_character_info

Returns the fields of a list entry, plus:

| Field | Present | Holds |
|---|---|---|
| `movement` | when the character has a movement component | the object described under "get_movement_params" |
| `skeletal_mesh` | when the character has a mesh component | `asset` (only when a mesh is assigned), `visible`, `num_bones` |
| `capsule` | when it has a capsule | `radius`, `half_height`, both scaled |
| `anim_instance` | when the mesh component has a live animation instance | `class`, the instance's class path |
| `anim_blueprint` | when the mesh component has an animation class set | that class path |

### get_movement_params

Returns `character_name` and `movement`:

| Group | Fields |
|---|---|
| can be set | `max_walk_speed`, `max_acceleration`, `ground_friction`, `jump_z_velocity`, `air_control`, `gravity_scale`, `max_step_height`, `walkable_floor_angle`, `braking_deceleration_walking`, `braking_friction` |
| read only | `max_walk_speed_crouched`, `air_control_boost_multiplier`, `walkable_floor_z`, `braking_deceleration_falling`, `use_separate_braking_friction`, `max_swim_speed`, `max_fly_speed` |
| state | `movement_mode` (`Walking`, `Falling`, `Swimming`, `Flying`, `Custom`, otherwise `None`), `velocity`, `is_moving_on_ground`, `is_falling` |

A character without a movement component returns
`Character has no CharacterMovementComponent`.

### set_movement_params

| Parameter | Clamped to | Unit |
|---|---|---|
| `max_walk_speed` | 0–10000 | cm/s |
| `max_acceleration` | 0–100000 | cm/s² |
| `ground_friction` | 0–100 | — |
| `jump_z_velocity` | 0–10000 | cm/s |
| `air_control` | 0–1 | — |
| `gravity_scale` | -10–10 | — |
| `max_step_height` | 0–500 | cm |
| `walkable_floor_angle` | 0–90 | degrees |
| `braking_deceleration_walking` | 0–100000 | cm/s² |
| `braking_friction` | 0–100 | — |

The result has `character_name`, `modified_params` (the names that were
applied) and `movement` (all values after the change). With no readable
movement value the call returns
`No movement parameters specified to modify`.

The read-only fields above have no parameter. Set those on the Blueprint
(skill `ue-blueprints`, section `modify`) or on the actor with the toolkit's
actor tools (skill `ue-level-actors`).

`braking_friction` is stored whatever `use_separate_braking_friction` says;
the tool cannot set that flag.

### get_components

`component_class` is a substring of the component's class name
(`SkeletalMeshComponent`, `CapsuleComponent`). The result has
`character_name`, `components` and `count`. Each component has `name`,
`class` and `active`; a scene component adds `visible`, `relative_location`,
`relative_rotation` and, when attached, `attach_parent`. The list is not
paged.

## Results through the bridge

Success is the tool's message, a blank line, then JSON. By default the bridge
goes through the task queue, so that JSON is a task envelope, and the fields
named here sit under its `data` key. A failure is only `Error: <message>`.
Skill `ue-tasks` covers the envelope and timeouts.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | `operation` or `character_name` is absent or empty |
| `Character not found: <name>` | no `ACharacter` in the editor's level has that name or label |
| `Character has no CharacterMovementComponent` | the movement operations need one |
| `No movement parameters specified to modify` | no movement value was passed, or none could be read as a number |
| `Actor name contains invalid character: '<c>'` | see "character_name" |
| `No active world. Open a level in the editor first` | no level is open |

## Background in the plugin

`unreal_get_ue_context` with `category: "character"` serves the plugin's notes
on `ACharacter` and `UCharacterMovementComponent` (skill `ue-context`). Its
example for this tool matches the code.

## See also

- `ue-character-data`: character config assets and stats tables, and applying
  a config to a placed character.
- `ue-game-framework`: which pawn class the game mode spawns.
- `ue-blueprints`: movement defaults of a character Blueprint.
- `ue-level-actors`: other properties of a placed actor, and moving it.
- `ue-pie-testing`: play-testing the result.
