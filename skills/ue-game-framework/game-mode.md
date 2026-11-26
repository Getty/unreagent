# game_framework: game mode in full

The recipes and the must-not-get-wrong list are in the skill itself. This
section has the engine background, the two game mode operations in detail,
and the durable routes for what `set_game_mode_defaults` only does in memory.

## The engine's model

| Class | Role | Lives |
|---|---|---|
| Game mode (`AGameModeBase`, or `AGameMode`) | the rules; names the classes below and spawns the player's pawn at a Player Start | server only |
| Game state (`AGameStateBase` / `AGameState`) | match state everyone sees | server, replicated |
| Player controller (`APlayerController`) | the player's will: input, camera, UI; possesses a pawn | one per player |
| Pawn / Character (`APawn`, `ACharacter`) | the body the controller possesses; `ACharacter` adds a capsule, a skeletal mesh and walking movement | in the world |
| Player state (`APlayerState`) | per-player data everyone sees (name, score) | replicated |
| HUD (`AHUD`) | legacy canvas HUD class; UMG widgets are usually created by the controller instead | one per player controller |

- **`AGameModeBase`** is the default for new projects; **`AGameMode`** adds
  match states (waiting, in progress, …) for match-based multiplayer games
  (Epic, "Game Mode and Game State"). Pair them: `AGameMode` with
  `AGameState`, `AGameModeBase` with `AGameStateBase`. A mismatch logs
  `Mixing AGameStateBase with AGameMode is not compatible. …` or
  `Mixing AGameState with AGameModeBase is not compatible. …` as an error
  (`GameMode.cpp`, `GameState.cpp`, UE 5.8.4). Look for it in the output
  log after PIE.
- **Which game mode a level uses.** The project default is Project Settings
  → Maps & Modes → Default GameMode, stored as `GlobalDefaultGameMode=` under
  `[/Script/EngineSettings.GameMapsSettings]` in `Config/DefaultEngine.ini`
  (as in the engine's Third Person template). A level overrides it with
  **GameMode Override** in its World Settings (`AWorldSettings::DefaultGameMode`;
  "If this value is NULL, the INI setting for default game type is used",
  `WorldSettings.h`). A `?game=` URL option and map-name prefixes also
  exist (Epic, same page).
- **The pawn appears** when the game mode spawns `DefaultPawnClass` for the
  player at a Player Start (`GetDefaultPawnClassForController`,
  `GameModeBase.cpp`), and the player controller possesses it. A pawn
  placed in the level can instead be taken over through its
  `Auto Possess Player` setting (`APawn::AutoPossessPlayer`, `Pawn.h`).
  Without a Player Start the engine still spawns somewhere (unverified where;
  place one, skill `ue-level-actors`).

## inspect_game_mode

No parameters. Reads `GEditor->GetEditorWorldContext().World()`, the
**editor** world, also while PIE runs (`GameFrameworkEditor.cpp`):

1. the world's live game mode (`GetAuthGameMode`), when it has one — an
   editor world normally has none;
2. otherwise the level's GameMode Override;
3. otherwise the error
   `No game mode configured. Neither AuthGameMode nor WorldSettings->DefaultGameMode is set.`
   The project default is **not** consulted, so this error does not mean the
   game has no game mode. Read `GlobalDefaultGameMode` in
   `Config/DefaultEngine.ini` (skill `unreagent-files`) to see it.

It then reads the class defaults of that game mode class:

| Field | Holds |
|---|---|
| `game_mode_class` | class path |
| `default_pawn_class`, `player_controller_class`, `hud_class`, `spectator_class` | class path, or the string `None` |
| `from_pie` | `true` only when the editor world has a live game mode. Not a PIE indicator: use `pie_status` (skill `ue-pie-testing`) |

The game state and player state classes are not reported. Read them with
`blueprint_query` on a Blueprint game mode (skill `ue-blueprints`).

## set_game_mode_defaults

Up to three class paths: `default_pawn_class`, `player_controller_class`,
`hud_class`. An empty string counts as absent, so a class cannot be cleared
to `None`; there is no parameter for the spectator, game state or player
state class.

What the code does: take the level's **GameMode Override** (not a live game
mode, not the project default), load each given class (`LoadClass<APawn>`,
`<APlayerController>`, `<AHUD>`), assign it to the game mode class's default
object in the order pawn, controller, HUD, then `Modify` and mark the
package dirty. Result: `changes` (strings such as
`DefaultPawnClass=/Game/Characters/BP_Hero.BP_Hero_C`) and the message
`Updated <n> game mode defaults`.

Consequences:

- **The class, not the level, changes.** Every level and the project default
  that use the same game mode class get the new classes.
- **Nothing is saved.** For a Blueprint game mode, save its asset (`asset`
  `save_asset` with the Blueprint's asset path: `game_mode_class` without
  the `.<Name>_C` part, skill `ue-assets`). For a C++ game mode
  (`/Script/…`) there is no asset; the change lasts until the editor closes.
- **Whether a later compile of the Blueprint keeps the values** cannot be
  read from the plugin's source. Prefer the durable route below.
- **Half application on error**: a class that fails to load stops the call,
  the earlier ones stay assigned, nothing is marked dirty. Run
  `inspect_game_mode` after an error.

Errors: section `failures`.

## Durable routes

| Goal | Route |
|---|---|
| a game mode Blueprint | `blueprint_modify` `create` with `parent_class: "GameModeBase"` (the toolkit resolves short names under `/Script/Engine`), then `compile` (skill `ue-blueprints`) |
| its pawn / controller / HUD / game state / player state class | `blueprint_modify` `set_cdo_default`, `property` `DefaultPawnClass`, `PlayerControllerClass`, `HUDClass`, `GameStateClass` or `PlayerStateClass`, `value` the class path; save (skill `ue-blueprints`, section `modify`). The operation compiles the Blueprint itself, and loads a class value with `LoadClass<UObject>` **without checking the base class** (`MCPTool_BlueprintModify.cpp`): a controller class in `DefaultPawnClass` is accepted. Read it back with `inspect_game_mode` or `blueprint_query` |
| this level uses that game mode | the level's GameMode Override. Through the toolkit: `set_property` on the actor `WorldSettings`, `property` `DefaultGameMode`, `value` the game mode class path (`…/BP_GM.BP_GM_C`), then save the level (skills `ue-level-actors`, `ue-levels`). Read in the source, not run: `set_property` finds actors by name or label with an actor iterator, and loads a class value with `LoadClass<UObject>`, **without checking that it is a game mode**: pass a game mode class only. Verify with `inspect_game_mode` |
| every level uses it | `GlobalDefaultGameMode=` in `Config/DefaultEngine.ini` (skill `unreagent-files`), editor restart; or Project Settings → Maps & Modes by hand |
| a C++ game mode's classes | its constructor (`DefaultPawnClass = …`), rebuild (skill `unreagent-build`) |

Decision rule: a quick experiment in this session → `set_game_mode_defaults`.
Anything that must still hold tomorrow → the durable routes.
