# character_data: apply_character_data

Copies values from a config onto one `ACharacter` in the editor's level
(`ExecuteApplyCharacterData`, `MCPTool_CharacterData.cpp`). The header and
the plugin's context call the target a "runtime character"; the code takes
the editor's world (`GEditor->GetEditorWorldContext().World()`), like the
`character` tool, so during PIE it changes the level's actor, not the
running game's copy.

| Parameter | Required | Default |
|---|---|---|
| `asset_path` | yes | — (a config path, section `config`) |
| `character_name` | yes | — object name or exact label, checked as in skill `ue-character`, section `operations` |
| `apply_movement` | no | `true` |
| `apply_mesh` | no | `false` |
| `apply_anim` | no | `false` |

The flags are read with `TryGetBoolField`, which also takes strings such as
`"yes"` and numbers (section `config`, "How values are read"); send JSON
booleans.

## What is written

| Part | Flag | Written | Skipped silently when |
|---|---|---|---|
| movement | `apply_movement` | `MaxWalkSpeed` ← `base_walk_speed`, `MaxAcceleration` ← `base_acceleration`, `JumpZVelocity` ← `base_jump_velocity`, `GroundFriction` ← `base_ground_friction`, `AirControl` ← `base_air_control`, `GravityScale` ← `base_gravity_scale` | the character has no movement component |
| capsule | **none — always** | `SetCapsuleRadius(capsule_radius)`, `SetCapsuleHalfHeight(capsule_half_height)` (unscaled values) | the character has no capsule |
| mesh | `apply_mesh` | `SetSkeletalMesh` with the config's mesh | the reference is empty, does not load, or there is no mesh component |
| animation | `apply_anim` | `SetAnimInstanceClass` with the config's class | the reference is empty, does not load, or there is no mesh component |

Not applied to anything: `base_run_speed`, the combat values, the tags,
`is_player_character` and the stats table. Movement values are written
as stored (the config's clamps apply, not the `character` tool's); walking
braking, step height and floor angle are untouched.

Afterwards the actor's package and the level are marked dirty. No
transaction, no save.

## Result

`character_name`, `config_applied` (the path you passed) and
`applied_settings`, a list out of `movement`, `capsule`, `skeletal_mesh`,
`anim_blueprint`. The call succeeds whatever that list holds: a part you
asked for that is missing was not applied. Errors:
`Failed to load character config: <path>`, `Character not found: <name>`,
the name-check errors of skill `ue-character`, and the editor/world errors.

## The capsule trap

The capsule is written with every call, and nothing else moves with it:

- **The mesh is not repositioned.** A character's mesh usually sits lowered
  by about the capsule's half height so the feet meet the capsule bottom. A
  config whose `capsule_half_height` differs from the character's current
  one leaves the mesh floating above or sunk below the new capsule bottom.
- **An untouched config still resizes.** The config defaults are 42/96; a
  bare `ACharacter` is 34/88 (engine `Private/Character.cpp`). Set the
  capsule fields in the config to the character's real size, or accept the
  change and fix the mesh.
- `character` `get_character_info` reports the capsule **scaled** by the
  actor's scale; the config's values are unscaled. On an actor with scale
  ≠ 1 they will not read back equal.

Fixing the mesh offset after a capsule change: set the mesh component's
relative location on the Blueprint (skill `ue-blueprints`) or on the placed
actor (skill `ue-level-actors`), lowering it by the new half height (the
exact offset depends on the mesh's pivot; check it visually with skill
`ue-viewport`).

## Workflow: apply and verify

1. Restore point, from `character` (skill `ue-character`):
   `get_character_info` → keep `movement`, `capsule`, and from
   `get_components` with `"component_class": "SkeletalMesh"` the mesh's
   `relative_location`.
2. `get_character_data` on the config: are `skeletal_mesh` /
   `anim_blueprint` present, and are the `collision` values the size you
   want?
3. `{"operation": "apply_character_data", "asset_path": "<config>", "character_name": "<name>", "apply_mesh": true, "apply_anim": true}`
   (drop flags for parts the config lacks).
4. Check `applied_settings` holds every requested part. A missing
   `skeletal_mesh` or `anim_blueprint`: the path in the config does not load
   — check it with `asset_search` (skill `ue-assets`), fix it with
   `update_character_data`, apply again.
5. `character` `get_character_info`: `movement` values equal the config's,
   `capsule` equals the config's times the actor scale,
   `skeletal_mesh.asset` and `anim_blueprint` are the config's.
6. Fix the mesh offset if the half height changed (above), then look at it
   (skill `ue-viewport`).
7. `asset` `{"operation": "save_all"}` (skill `ue-assets`).

**Restore**: `character` `set_movement_params` with the six movement values
from step 1; the capsule, mesh and animation class have no restore through
these two tools except applying another config, or the Blueprint and actor
tools.

## Instance or class

Like `character`, this changes one placed actor. For the player pawn or any
runtime-spawned character, the values belong on the Blueprint class (skill
`ue-character`, section `workflows`), or in game code that reads the
config at spawn — which, given the editor-only module (main document), is
editor-side only.
