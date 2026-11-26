# Character movement values

What the values `character` reads and writes do inside
`UCharacterMovementComponent` (CMC). Engine defaults and the formulas below
are read in the UE 5.8.4 engine source
(`Engine/Source/Runtime/Engine/Private/Components/CharacterMovementComponent.cpp`,
constructor; `Classes/GameFramework/CharacterMovementComponent.h`), not run;
the toolkit targets UE 5.6 and 5.7, and nothing here was found to differ
between those versions, which is itself unverified. Property descriptions:
Epic's API reference,
<https://dev.epicgames.com/documentation/en-us/unreal-engine/python-api/class/CharacterMovementComponent>.

A Blueprint or a project template usually sets its own values in the class
defaults, so the engine default is only what a bare `ACharacter` subclass
starts with. Read the actual value with `get_movement_params` before you
reason from a default.

## The ten settable values

| Parameter | C++ property | Engine default | What it does |
|---|---|---|---|
| `max_walk_speed` | `MaxWalkSpeed` | 600 cm/s | top ground speed while walking; also caps lateral speed while falling |
| `max_acceleration` | `MaxAcceleration` | 2048 cm/s² | how fast velocity changes toward the input direction |
| `ground_friction` | `GroundFriction` | 8 | higher = faster changes of direction; also the braking friction while walking, unless separate braking friction is on |
| `jump_z_velocity` | `JumpZVelocity` | 420 cm/s | upward speed at jump start |
| `air_control` | `AirControl` | 0.05 | lateral control while falling, 0 none to 1 full control at `MaxWalkSpeed` |
| `gravity_scale` | `GravityScale` | 1 | multiplies world gravity for this character (negative flips it) |
| `max_step_height` | `MaxStepHeight` | 45 cm | highest ledge the character steps up without jumping |
| `walkable_floor_angle` | `WalkableFloorAngle` | ≈44.77° (from `WalkableFloorZ` 0.71) | steepest walkable slope; set through `SetWalkableFloorAngle`, which recomputes `walkable_floor_z` |
| `braking_deceleration_walking` | `BrakingDecelerationWalking` | = `MaxAcceleration` (2048) | constant deceleration when walking without input |
| `braking_friction` | `BrakingFriction` | 0 | drag while braking — **used only when `bUseSeparateBrakingFriction` is true** |

**Jump height.** The engine's `GetMaxJumpHeight()` is
`JumpZVelocity² / (2 · |gravity|)`, gravity being the world's (project
default `DefaultGravityZ` −980 cm/s²) times `GravityScale`. Defaults give
420² / 1960 ≈ 90 cm. To reach a target height `h` at gravity scale `g`:
`jump_z_velocity = sqrt(2 · 980 · g · h)`. Holding the jump button adds
`JumpMaxHoldTime · JumpZVelocity` (an `ACharacter` property, not reachable
through this tool).

**Braking.** With no input the character slows by a constant
`BrakingDecelerationWalking` plus a friction term. That friction is
`BrakingFriction` when `bUseSeparateBrakingFriction` is true, otherwise the
current friction (`GroundFriction` while walking), each multiplied by
`BrakingFrictionFactor` (default 2). Consequences:

- `braking_friction` changes nothing while `use_separate_braking_friction`
  reads `false` — the tool stores it and reports success anyway.
- `bUseSeparateBrakingFriction` is `EditDefaultsOnly`: it can be set only on
  the class defaults, so set it on the Blueprint
  (`set_component_default` on `CharMoveComp`, skill `ue-blueprints`,
  section `modify`), never on a placed actor.
- To make a character stop sooner, raise `braking_deceleration_walking`
  and/or `ground_friction`; to make it slide, lower both.

**Floor angle.** `walkable_floor_angle` and `walkable_floor_z` are two views
of one setting (`WalkableFloorZ = cos(angle)`). The tool sets the angle
through the engine's setter, so the pair stays consistent; the engine's own
clamp is 0–90 as well.

## The read-only values and where to set them

| Field | C++ property | Engine default | Set it with |
|---|---|---|---|
| `max_walk_speed_crouched` | `MaxWalkSpeedCrouched` | `MaxWalkSpeed` × 0.5 = 300 | Blueprint: `set_component_default` on `CharMoveComp` |
| `air_control_boost_multiplier` | `AirControlBoostMultiplier` | 2 | same |
| `braking_deceleration_falling` | `BrakingDecelerationFalling` | 0 | same |
| `use_separate_braking_friction` | `bUseSeparateBrakingFriction` | false | same — class defaults only |
| `max_swim_speed` | `MaxSwimSpeed` | 300 cm/s | same |
| `max_fly_speed` | `MaxFlySpeed` | 600 cm/s | same |
| `walkable_floor_z` | `WalkableFloorZ` | 0.71 | set `walkable_floor_angle` instead |

On a placed actor, the toolkit's generic property tool may reach these
(skill `ue-level-actors`); this skill has not checked it against the CMC.

## Movement mode and state

`movement_mode` maps `MOVE_Walking`, `MOVE_Falling`, `MOVE_Swimming`,
`MOVE_Flying` and `MOVE_Custom`; anything else (`MOVE_None`,
`MOVE_NavWalking`) is reported as `None`. `velocity`, `is_moving_on_ground`
and `is_falling` are the editor actor's state, which outside play is
normally zero velocity and the mode the actor was left in. They are not the
running game's state, even during PIE.

## The capsule

`ACharacter` builds its capsule as `CollisionCylinder` with radius 34 and
half height 88 (`Private/Character.cpp`); the mesh `CharacterMesh0` and the
movement component `CharMoveComp` are its other native subobjects.
`get_character_info` reports the capsule scaled by the actor's scale. The
`character` tool cannot change the capsule; `character_data`
`apply_character_data` can (skill `ue-character-data`), as can the Blueprint
defaults.

## Typical value ranges (guidance, not engine facts)

These are starting points for tuning, not sourced numbers; confirm in play.

- A walk is roughly 150–250 cm/s, a jog 350–500, a sprint 600–900.
- Snappier control: higher `max_acceleration`, `ground_friction` and
  `braking_deceleration_walking`. Floatier: lower ones.
- More air steering: `air_control` 0.3–0.6; 1.0 feels like ground control.
- Heavier jump arc: raise `gravity_scale` (1.5–3) and raise
  `jump_z_velocity` to keep the height (formula above).

## Mover (UE 5.4 and later)

Epic's Mover plugin is a separate movement system meant to replace CMC
eventually; Epic's documentation still labels it Experimental
(<https://dev.epicgames.com/documentation/unreal-engine/mover-in-unreal-engine>),
and CMC stays supported. The `character` tool reads and writes only
`ACharacter` actors and their `UCharacterMovementComponent`: a pawn that moves
with a Mover component is not in `list_characters` (unless it also derives
from `ACharacter`), and its movement settings are not reachable through this
tool.
