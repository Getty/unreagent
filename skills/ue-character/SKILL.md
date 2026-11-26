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
the level that is open in the editor and changes ten values of their
`UCharacterMovementComponent`. Through the plugin's MCP bridge it is listed as
`unreal_character`. It works only while the editor is running and the plugin
is reachable; `unreagent-iterate` covers the wait for the plugin.

Five operations, no aliases: `list_characters`, `get_character_info`,
`get_movement_params`, `set_movement_params`, `get_components`. Parameters,
result fields and errors: section `operations`.

## Must not get wrong

- **It changes one placed actor, not the class.** Other actors of the same
  Blueprint, and every character spawned at runtime (the player pawn the game
  mode spawns at a Player Start among them), keep the class defaults. To
  change what the game spawns, change the Blueprint (recipe 3).
- **It reads and writes the editor world, also while Play In Editor runs.**
  During PIE you see and change the level's actors, not their copies in the
  game; `velocity`, `movement_mode`, `is_falling` and `can_jump` are then
  not the game's state. Tune before `start_pie` (skill `ue-pie-testing`).
- **Nothing is saved and nothing can be undone.** `set_movement_params`
  marks the actor's package and the level dirty, records no editor
  transaction, and saves nothing. Note the old values first; save with
  `asset` `{"operation": "save_all"}` (skill `ue-assets`).
- **Values are clamped silently, and an unreadable value is skipped.** The
  call succeeds with the clamped value; a value that is not a number is left
  out of `modified_params` without an error. Send JSON numbers and compare
  the returned `movement` with what you sent.
- **`character_name` is matched whole, first hit wins**, against the object
  name (`BP_Hero_C_0`) or the outliner label (`BP_Hero`). A label with
  `(`, `)` or another of `` <>|&;`$(){}[]!*?~ `` is refused before the
  search: pass the object name from `list_characters`.
- **Seven movement values are read-only here** (crouched speed, swim and fly
  speed, falling braking, the separate-braking-friction flag, …). Section
  `movement` says where each one is set instead.

## Recipes

**1. Find a character and read what it is made of**

1. `{"operation": "list_characters"}` (optionally `"class_filter": "BP_Hero"`).
   Take `name` from an entry of `characters`; `total: 0` means none is placed
   in the editor's level (recipe 3).
2. `{"operation": "get_character_info", "character_name": "<name>"}`: class,
   transform, mesh asset and bone count, scaled capsule, animation class,
   movement values.
3. `{"operation": "get_components", "character_name": "<name>"}`; add
   `"component_class": "Skeletal"` to filter.

**2. Tune a placed character and prove it**

1. `{"operation": "get_movement_params", "character_name": "<name>"}`; keep
   the `movement` object as your restore point.
2. `{"operation": "set_movement_params", "character_name": "<name>", "max_walk_speed": 450, "jump_z_velocity": 600}`.
   Pass only what changes.
3. Check the result: every name you sent is in `modified_params`, and each
   value in `movement` equals what you sent (or its clamp bound).
4. `asset` `{"operation": "save_all"}`.
5. To prove it persisted, open another level and then this one again (skill
   `ue-levels`) and repeat step 1; the values should match step 3. This round
   trip is the only check the tools offer that the value reached disk.

**3. Change the class instead (the usual case for a player pawn)**

1. Which class the game spawns: `game_framework` `inspect_game_mode` (skill
   `ue-game-framework`).
2. `blueprint_modify` `{"operation": "set_component_default", "blueprint_path": "/Game/Characters/BP_Hero", "component_name": "CharMoveComp", "property": "MaxWalkSpeed", "value": 450}`
   (skill `ue-blueprints`, section `modify`). `CharMoveComp`,
   `CollisionCylinder` and `CharacterMesh0` are the object names `ACharacter`
   gives its movement, capsule and mesh components; `property` is the C++
   property name. The Blueprint compiles on the call.
3. Verify with `blueprint_query` (skill `ue-blueprints`, section `query`),
   then in play (skill `ue-pie-testing`).

Section `workflows` holds these end to end, with the decision between
instance and class, restoring, and the checks after each step.

## Sections

Load one with the `skills` tool (`name: "ue-character"`, `section: "<name>"`)
or read `<section>.md` next to this file.

| Section | What is in it | Load it when |
|---|---|---|
| `operations` | every operation: parameters, defaults, clamps, result fields, the name check, bridge envelope, error strings | you build a call or read a result field you are not sure of |
| `movement` | what each movement value does in the engine, its engine default and unit, the read-only ones and where to set them, braking and floor-angle details, Mover | you choose values, or a change "does nothing" in play |
| `workflows` | end-to-end procedures: inspect, tune and verify, class vs instance, restore, characters that exist only in play | you do more than one call, or decide where a change belongs |
| `failures` | symptoms with cause and fix: not found, change lost, no effect in play, clamped values, mesh and capsule surprises | a call fails, or succeeds without the effect you expected |

## Background in the plugin

`unreal_get_ue_context` with `category: "character"` serves the plugin's own
notes on `ACharacter` and `UCharacterMovementComponent` (skill `ue-context`).

## See also

- `ue-character-data`: the toolkit's character config assets and stats
  tables, and applying a config to a placed character.
- `ue-game-framework`: which pawn class the game mode spawns.
- `ue-blueprints`: movement defaults of a character Blueprint.
- `ue-level-actors`: other properties of a placed actor, and moving it.
- `ue-anim-blueprint`: the animation class on the mesh.
- `ue-pie-testing`: play-testing the result.
