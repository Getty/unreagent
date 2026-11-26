# game_framework: a playable character end to end

The whole sequence from an empty level to a character the player moves,
with the tool route per step and the skill that owns it. Each step ends with
its check; do not go on from a step you have not checked.

Before you start: does the project already have a game mode, character and
input? `inspect_game_mode`, `asset_search` for `GameMode`, `Character`,
`PlayerController`, `IA_`, `IMC_` (skill `ue-assets`), and
`GlobalDefaultGameMode` in `Config/DefaultEngine.ini`. A project made from
an engine template has all of it: change what is there rather than build a
second set.

## Blueprint, C++ or Python

| Choose | When |
|---|---|
| Blueprint (this sequence) | the project is Blueprint-only, or the class is gameplay glue; the toolkit can create, edit and compile it while the editor runs |
| C++ | the project already has a C++ game module with these classes, or the logic is heavy; needs a build and an editor restart (skills `unreagent-build`, `unreagent-iterate`). The engine's Third Person C++ template is the reference: input contexts in the controller's `SetupInputComponent`, `BindAction` in the character's `SetupPlayerInputComponent` |
| Python (`execute_script`, skill `ue-python`) | one-off editor automation the dedicated tools lack, such as a property no tool reaches; not for gameplay logic, which does not run in a shipped game |

## The sequence

| # | Step | Route | Owner skill | Check |
|---|---|---|---|---|
| 1 | Input assets: `IA_Move` (`Axis2D`), `IA_Look` (`Axis2D`), `IA_Jump` (`Digital`), `IMC_Default` with the key mappings | `enhanced_input` | `ue-enhanced-input` | `query_context` |
| 2 | Character Blueprint `BP_Hero`, parent `Character`: mesh, animation, camera boom and camera | `blueprint_modify` `create`, `add_component`, `set_component_default`; character tools | `ue-blueprints`, `ue-character` | `blueprint_query` |
| 3 | Its input reaction: `EnhancedInputAction` nodes for `IA_Move` (→ `Add Movement Input`), `IA_Look` (→ `Add Controller Yaw/Pitch Input`), `IA_Jump` (`Started` → `Jump`, `Completed` → `Stop Jumping`) | `blueprint_modify` `add_node` / `add_nodes`, `connect_pins`, `compile` | `ue-blueprints` (section `graphs`) | compile without errors |
| 4 | Add `IMC_Default` to the player: through the tools, the project-wide default context in `DefaultInput.ini` (on by default; the editor reads the file at start, so restart it after the edit, skill `unreagent-iterate`; whether a running editor picks the edit up is unverified), or a C++ controller base class. The Blueprint route ("Get Enhanced Input Local Player Subsystem" → "Add Mapping Context" in `Event BeginPlay`) cannot be built with `blueprint_modify`; a person adds those two nodes | the launcher's file tools; or C++ | `ue-enhanced-input` (section `runtime-setup`); `ue-blueprints` (section `workflows`) | read the ini back; in PIE a key moves the pawn |
| 5 | Game mode Blueprint `BP_GM`, parent `GameModeBase`; `DefaultPawnClass` = `BP_Hero_C`, `PlayerControllerClass` = your controller class if you have one (else leave the engine's `PlayerController`) | `blueprint_modify` `create`, `set_cdo_default` | `ue-blueprints`; section `game-mode` here | `inspect_game_mode` after step 6 |
| 6 | The level uses `BP_GM`: GameMode Override, or the project default | `set_property` on `WorldSettings` (`DefaultGameMode`); or `GlobalDefaultGameMode` in `DefaultEngine.ini` | section `game-mode` here, "Durable routes"; `ue-level-actors` | `inspect_game_mode` names `BP_GM` and its classes |
| 7 | A Player Start in the level, on the floor | `spawn_actor` | `ue-level-actors` | `get_level_actors` |
| 8 | Save everything | `asset` `save_asset` for each new asset; save the level | `ue-assets`, `ue-levels` | no dirty packages left |
| 9 | Play | start PIE, wait for `pie_status` running | `ue-pie-testing` | see "Verify" |

Use class paths with `_C` wherever a class is set: `/Game/Hero/BP_Hero.BP_Hero_C`
(section `game-mode`; the asset path is not a class path).

## Verify

1. `inspect_game_mode`: `game_mode_class`, `default_pawn_class` and
   `player_controller_class` are yours.
2. PIE running, then `capture_pie` (skill `ue-pie-testing`): the character
   is there, seen from its camera.
3. `gameplay_debug` injects `IA_Move` / `IA_Jump`: the character moves and
   jumps. This tests the reaction (step 3), not the key mapping or the added
   context (step 4): injection skips both (`EnhancedPlayerInput.cpp:284-291`,
   `:646-667`, UE 5.8.4); only a key pressed by a person covers those. The input
   monitor (skill `ue-pie-testing`) shows what the game received.
4. `get_output_log`: no `Mixing AGameStateBase with AGameMode`, no
   Blueprint runtime errors (`Accessed None`), no Enhanced Input warnings.
5. Stop PIE. PIE state never reaches an asset.

## When it does not work

| Symptom | Likely cause | Check / fix |
|---|---|---|
| PIE shows a free-flying camera, no character | the level runs another game mode (no override, project default differs), or `DefaultPawnClass` is `DefaultPawn`/`None` | `inspect_game_mode` |
| `No game mode configured` but something plays | the project default game mode is in use | read `GlobalDefaultGameMode`; set the override (step 6) |
| the character spawns but ignores keys | `IMC_Default` never added (step 4), or the controller in use is not yours | read `DefaultInput.ini` back and restart the editor after an edit; `inspect_game_mode` `player_controller_class` |
| injected actions work, keys do not | mapping or context, not the Blueprint: injection skips both (`EnhancedPlayerInput.cpp:646-667`, UE 5.8.4) | `query_context`; step 4 |
| nothing reacts, even injected actions | step 3 graph not wired, or not compiled | `blueprint_query`, `compile` |
| keys stop after a menu was shown | input mode left at UI only | `set_input_mode` `game_only` to test; fix the menu's close logic |
| the character falls through the world or spawns at 0,0,0 | no Player Start, or it is inside geometry | step 7 |
| changes gone after an editor restart | something not saved, or `set_game_mode_defaults` was used instead of the durable route | step 8; section `game-mode` |
