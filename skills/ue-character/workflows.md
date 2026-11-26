# character: workflows

End-to-end procedures. Every step names the call and what to check in its
result before the next one. Call shapes: section `operations`.

## Decide first: instance or class

| You want | Change | Tool |
|---|---|---|
| one hand-placed NPC to differ from its siblings | the placed actor | `character` `set_movement_params` |
| the player pawn, or anything spawned at runtime | the Blueprint class | `blueprint_modify` `set_component_default` (skill `ue-blueprints`) |
| every placed copy of a Blueprint | the Blueprint class | same |
| a value `set_movement_params` has no parameter for | the Blueprint class | same (section `movement` lists them) |
| a reusable preset applied to several placed characters | a config asset | `character_data` (skill `ue-character-data`) |

A Blueprint that sets movement values in its construction script or
`BeginPlay` overrides both routes at that point; read its graphs with
`blueprint_query` (skill `ue-blueprints`) when a value does not hold.

## A. Survey the characters in the level

1. `{"operation": "list_characters"}`. Check `total`; when it exceeds
   `count`, page with `offset`.
2. For each one of interest, `get_character_info`. Note `class_path` (the
   Blueprint to change for class-wide edits), `skeletal_mesh.asset`,
   `anim_blueprint`, and `capsule`.
3. `get_components` when you need the component names, for example before a
   `set_component_default` on a Blueprint-added component.

## B. Tune one placed character and verify

1. `get_movement_params` → keep `movement` as the restore point.
2. Decide the values (section `movement` for formulas such as jump height).
3. `set_movement_params` with only those values.
4. Verify in the result:
   - each name you sent is in `modified_params` (a missing one was not a
     readable number);
   - each sent value equals the one in `movement`, or the clamp bound
     (section `operations`);
   - `braking_friction` only matters when `use_separate_braking_friction`
     is `true`.
5. Save: `asset` `{"operation": "save_all"}` (skill `ue-assets`).
6. Prove persistence when it matters: open another level and then this one
   again with `open_level` (skill `ue-levels`), then `get_movement_params`.
   Whether reopening the level that is already open rereads it from disk is
   unverified, hence the detour.
7. Play-test (skill `ue-pie-testing`). The `character` tool cannot read the
   running game's copy.

**Restore** = `set_movement_params` with the ten settable values from step 1,
then save. There is no undo.

## C. Change what the game spawns

1. `game_framework` `{"operation": "inspect_game_mode"}` (skill
   `ue-game-framework`): the default pawn class.
2. `blueprint_query` on that Blueprint for its components and current
   defaults (skill `ue-blueprints`, section `query`).
3. One `blueprint_modify` `set_component_default` per value, on component
   `CharMoveComp` with the C++ property name (`MaxWalkSpeed`,
   `JumpZVelocity`, `MaxWalkSpeedCrouched`, `bUseSeparateBrakingFriction`,
   …). The call compiles the Blueprint. Do not set `WalkableFloorAngle`
   this way and expect `WalkableFloorZ` to follow: that pairing happens in
   the engine's setter, which a reflection write does not call
   (unverified whether a later load or edit re-syncs it).
4. Save (`asset` `save_all`) and play-test.

Placed copies of that Blueprint whose value you had overridden keep the
override. Whether copies that still had the old default follow the new
one at once is standard UE default propagation, unverified through these
tools: read one back with `get_movement_params`.

## D. The character is not in the list

`list_characters` reads the editor's level. Missing are: pawns spawned only
in play (the game mode's default pawn at a Player Start), actors in a
sublevel or World Partition cell that is not loaded, and pawns that do not
derive from `ACharacter` (a `DefaultPawn`, a Mover-based pawn). For a pawn
spawned in play, use recipe C. For a level that is not loaded, open it
(skill `ue-levels`).

## E. Check the visual setup of a character

1. `get_character_info`: no `skeletal_mesh.asset` → no mesh assigned; no
   `anim_blueprint` → no animation class.
2. `get_components` with `"component_class": "SkeletalMesh"`: the mesh's
   `relative_location` (usually lowered by about the capsule's half height so
   the feet meet the capsule bottom) and `relative_rotation` (often yaw −90
   for meshes authored facing +Y; a convention, not an engine rule).
3. Mesh and animation class are changed on the Blueprint (skill
   `ue-blueprints`) or, from a config asset, with `apply_character_data`
   (skill `ue-character-data`). Building the animation class: skill
   `ue-anim-blueprint`.
