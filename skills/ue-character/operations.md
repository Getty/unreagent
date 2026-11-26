# character: operations reference

Source: `MCPTool_Character.h` / `MCPTool_Character.cpp` in the UE LLM
Toolkit (`Private/MCP/Tools/`), with the shared helpers in
`MCPToolBase.h` and `MCPParamValidator.cpp`.

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

Every operation works on the editor's own world
(`GEditor->GetEditorWorldContext().World()`), also while PIE runs.

## character_name

The actor's object name (`BP_Hero_C_0`) or its outliner label (`BP_Hero`),
compared whole and case-sensitively against every `ACharacter` in the
editor's level. The first actor that matches either wins, so two actors with
the same label are not told apart: use the object name. Any other actor
class returns `Character not found: <name>`.

The name is checked before the search:

- at most 256 characters
  (`Actor name exceeds maximum length of 256 characters`);
- none of `` <>|&;`$(){}[]!*?~ ``
  (`Actor name contains invalid character: '<c>'`). A label such as
  `Hero (2)` therefore cannot be passed; use the object name;
- no control characters (`Actor name contains control characters`).

## list_characters

| Parameter | Default | Meaning |
|---|---|---|
| `class_filter` | none | case-sensitive substring of the class name; a Blueprint class is named `BP_Hero_C` |
| `limit` | 100 | clamped to 1–1000 |
| `offset` | 0 | entries to skip; a negative value counts as 0 |

The result has `characters`, `count` (entries returned), `total` (all
matches), `offset` and `limit`. Each entry has `name`, `label`, `class`,
`class_path`, `location` (`x`, `y`, `z`), `rotation` (`pitch`, `yaw`,
`roll`), `can_jump` and `is_crouched`. The message's "showing a-b of n" is
wrong for an empty page; read `count` and `total`.

`can_jump` is `ACharacter::CanJump()` evaluated on the editor actor; outside
play it says little about the game.

## get_character_info

Returns the fields of a list entry, plus:

| Field | Present | Holds |
|---|---|---|
| `movement` | the character has a movement component | the object described under "get_movement_params" |
| `skeletal_mesh` | the character has a mesh component | `asset` (only when a mesh is assigned), `visible`, `num_bones` |
| `capsule` | it has a capsule | `radius`, `half_height`, both **scaled** by the actor's scale |
| `anim_instance` | the mesh component has a live animation instance | `class`, the instance's class path |
| `anim_blueprint` | the mesh component has an animation class set | that class path (`…/ABP_Hero.ABP_Hero_C`) |

A missing `skeletal_mesh.asset` means the mesh component has no mesh. A
missing `anim_blueprint` means no animation class is set (the mesh may use an
animation asset instead, or nothing).

## get_movement_params

Returns `character_name` and `movement`:

| Group | Fields |
|---|---|
| can be set | `max_walk_speed`, `max_acceleration`, `ground_friction`, `jump_z_velocity`, `air_control`, `gravity_scale`, `max_step_height`, `walkable_floor_angle`, `braking_deceleration_walking`, `braking_friction` |
| read only | `max_walk_speed_crouched`, `air_control_boost_multiplier`, `walkable_floor_z`, `braking_deceleration_falling`, `use_separate_braking_friction`, `max_swim_speed`, `max_fly_speed` |
| state | `movement_mode` (`Walking`, `Falling`, `Swimming`, `Flying`, `Custom`, otherwise `None`), `velocity` (`x`, `y`, `z`), `is_moving_on_ground`, `is_falling` |

A character without a movement component returns
`Character has no CharacterMovementComponent`. What each value does: section
`movement`.

## set_movement_params

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

The values are written straight into the component's properties;
`walkable_floor_angle` goes through `SetWalkableFloorAngle`, which also
updates `walkable_floor_z`. Then the actor's package and the level are
marked dirty. No `Modify()`, no transaction, no save.

The result has `character_name`, `modified_params` (the names that were
applied) and `movement` (all values after the change). With no readable
movement value the call returns `No movement parameters specified to modify`.

**How values are read.** Each one with `FJsonObject::TryGetNumberField` into a
`double`. Besides JSON numbers that also takes `true`/`false` (1/0) and a
string made of an optional sign, digits and at most one dot (`"800"`); a lone
sign or dot (`"-"`, `"."`) reads as 0. Any other string (`"800.0f"`, `"1e3"`),
`null`, an array or an object is skipped: not in `modified_params`, no error
(`TJsonValueString::TryGetNumber` in `Runtime/Json/Public/Dom/JsonValue.h`,
`TCString::IsNumeric` in `Runtime/Core/Public/Misc/CString.h`; read in the UE
5.8.4 source, not run, while the toolkit targets UE 5.6 and 5.7).

## get_components

`component_class` is a case-sensitive substring of the component's class name
(`SkeletalMeshComponent`, `CapsuleComponent`, `CharacterMovementComponent`).
The result has `character_name`, `components` and `count`. Each component has
`name`, `class` and `active`; a scene component adds `visible`,
`relative_location`, `relative_rotation` and, when attached, `attach_parent`
(the parent component's name). The list is not paged and includes
components added in the Blueprint as well as the native ones
(`CollisionCylinder`, `CharacterMesh0`, `CharMoveComp` on a plain
`ACharacter` subclass).

## Results through the bridge

Success is the tool's message, a blank line, then JSON. By default the bridge
goes through the task queue, so that JSON is a task envelope, and the fields
named here sit under its `data` key. A failure is only `Error: <message>`.
Skill `ue-tasks` covers the envelope and timeouts.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | `operation` or `character_name` is absent or empty (a number or boolean is read as its text, then fails the name match) |
| `Character not found: <name>` | no `ACharacter` in the editor's level has that name or label |
| `Character has no CharacterMovementComponent` | the movement operations need one |
| `No movement parameters specified to modify` | no movement value was passed, or none could be read as a number |
| `Actor name contains invalid character: '<c>'` | see "character_name" |
| `Actor name contains control characters` / `Actor name exceeds maximum length of 256 characters` | see "character_name" |
| `No active world. Open a level in the editor first` | no level is open |
| `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?` | no editor (`GEditor` is null) |
