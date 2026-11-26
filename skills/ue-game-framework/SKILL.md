---
name: ue-game-framework
description: >
  Inspect and set a level's game mode classes (pawn, player controller, HUD),
  drive the player controller of a running PIE session (input mode, pause,
  cursor, show or remove a widget), and read and edit DataTable rows, all
  with game_framework. Use before you change what a level spawns, put a
  widget on screen in PIE, or touch a DataTable.
toolkit_tools: [game_framework]
---

# UE game framework: game_framework

The UE LLM Toolkit plugin's **`game_framework`** tool has three unrelated
groups of operations: the game mode of the open level, the player controller
of a running Play In Editor (PIE) session, and DataTable assets. Through the
plugin's MCP bridge it is listed as `unreal_game_framework`. It works only
while the editor is running and the plugin is reachable; `unreagent-iterate`
covers the wait for the plugin.

## Sections

| Section | Holds | Load it |
|---|---|---|
| `playable-character` | the whole sequence from empty level to a character the player moves: input assets, character, controller, game mode, level override, Player Start, save, PIE checks; Blueprint vs C++ vs Python; a symptom table | when you build or repair a playable character, or "nothing spawns / keys do nothing" in PIE |
| `game-mode` | the engine's model (GameModeBase vs GameMode, game state pairing, where a level's game mode comes from, how the pawn is spawned), both game mode operations in full, and the durable routes (Blueprint defaults, the level's GameMode Override, the project default) | before you change which pawn, controller or game mode a level uses |
| `player-controller` | the five PIE operations in full: what each input mode lets through, widget instances and z-order, what `remove_widget` really removes, pause and `Trigger when Paused` | before you drive a running PIE session |
| `datatables` | the safe row-editing workflow, DataTable vs Data Asset, `table_path` forms, every result field, the value form `set_row` accepts per column type, the errors | before your first `set_row`, and when a DataTable call returns an error |
| `aliases` | every short operation name the code accepts, and what it runs | when a call uses, or you are about to send, an operation name that is not one of the twelve |
| `failures` | every error with its cause, how `paused` and `show_cursor` are read from a string or a number, and the failure table (what you see, what it means, what to do) | when a call returns an error, or what you see after a call is not what you expect |

## Must not get wrong

- **No operation of this tool saves.** `set_game_mode_defaults`, `set_row`
  and `delete_row` change memory and mark the package dirty; save with
  `asset` `save_asset` (skill `ue-assets`).
- **`set_game_mode_defaults` is the quick route, not the durable one.** It
  writes the class defaults of the level's GameMode Override in memory: it
  changes every level using that game mode class, a C++ game mode loses it
  when the editor closes, and whether a Blueprint game mode keeps it through
  a later compile cannot be read from the source. For anything that must
  last use `blueprint_modify` `set_cdo_default` (section `game-mode`,
  "Durable routes").
- **`inspect_game_mode` does not read the project default game mode.**
  `No game mode configured. …` means only that the level has no GameMode
  Override; the game may still have a game mode (Project Settings → Maps &
  Modes, `GlobalDefaultGameMode` in `Config/DefaultEngine.ini`). It reads
  the editor world even while PIE runs; `from_pie` is not a PIE indicator.
- **Class paths end in `_C`** for Blueprints: `/Game/UI/WBP_Menu.WBP_Menu_C`.
  The asset path fails with the same message as a missing class.
- **The PIE operations are a test harness.** Input mode, pause, cursor and
  shown widgets end with the session; nothing reaches the level or an asset.
  Each `show_widget` adds one more instance. `remove_widget` removes every
  instance of the class and its subclasses in the viewport, the game's own
  too, and succeeds with `removed_count: 0`.
- **`ui_only` blocks game input**; put it back to `game_only`.
- **Send `paused` / `show_cursor` as JSON booleans, and always pass
  `paused`.** Its default is `true`, so the alias `unpause` alone pauses.
  `set_pause` reports what it asked for, not what the engine did.
- **`set_row` on a misspelt `row_name` adds a new row**: check `is_new_row`.
  An error can leave an existing row half written: `get_row` after it.
- **`set_row` does not accept everything `get_row` returns.** Arrays, maps
  and sets go in as one UE text-form string; a transform in `get_row` form
  is skipped without an error. Section `datatables`, value forms.
- **The tool cannot create a DataTable, a game mode or a level override.**
  Section `game-mode` and `datatables` name the routes that can.

## Operations

| Operation | Required | Optional | Needs |
|---|---|---|---|
| `inspect_game_mode` | — | — | an open level |
| `set_game_mode_defaults` | at least one of `default_pawn_class`, `player_controller_class`, `hud_class` | — | a level with a GameMode Override |
| `set_input_mode` | `input_mode`: `game_only`, `ui_only`, `game_and_ui` | — | PIE |
| `show_widget` | `widget_class` | `z_order` (0) | PIE |
| `remove_widget` | `widget_class` | — | PIE |
| `set_pause` | — | `paused` (`true`) | PIE |
| `set_mouse_cursor` | — | `show_cursor` (`true`) | PIE |
| `list_data_tables` | — | `folder_path` (`/Game`, with subfolders) | — |
| `inspect_data_table` | `table_path` | — | — |
| `get_row` | `table_path`, `row_name` | — | — |
| `set_row` | `table_path`, `row_name`, `row_data` (object) | — | — |
| `delete_row` | `table_path`, `row_name` | — | — |

`operation` is lowercased before dispatch; short aliases exist (section
`aliases`), but prefer these names. An unknown one returns
`Unknown operation: '<op>'. Valid: <the twelve names>`.

Main result fields: `inspect_game_mode` → `game_mode_class`,
`default_pawn_class`, `player_controller_class`, `hud_class`,
`spectator_class` (class path or `None`), `from_pie`;
`set_game_mode_defaults` → `changes`; `show_widget` → `widget_class`,
`z_order`; `remove_widget` → `removed_count`; `list_data_tables` → `tables`
(`path`, `name`, `row_struct_name`), `count`; `inspect_data_table` →
`row_struct`, `columns` (`name`, `type`, `is_editable`, `category`),
`row_names`, `row_count`; `get_row` / `set_row` → `row_name`, `row_data`
(+ `is_new_row`); `delete_row` → `deleted_row_name`.

## Class paths

`default_pawn_class`, `player_controller_class`, `hud_class` and
`widget_class` go to the engine's class loader and must derive from pawn,
player controller, HUD or user widget respectively:

| Class | Path |
|---|---|
| Blueprint | the generated class: `/Game/Characters/BP_Hero.BP_Hero_C` |
| C++ | `/Script/<Module>.<Class>` without the `A`/`U` prefix: `/Script/Engine.DefaultPawn` |

The tool does not append `_C` for you, unlike `spawn_actor` (skill
`ue-level-actors`). `table_path` instead takes the asset path
(`/Game/Data/DT_Items`) or the object path `list_data_tables` returns.

## Recipes

**What does the level spawn?** `inspect_game_mode`. On
`No game mode configured`, read `GlobalDefaultGameMode` in
`Config/DefaultEngine.ini` (skill `unreagent-files`).

**Make the level spawn another pawn or controller, for good:** section
`game-mode`, "Durable routes" (`set_cdo_default` on the game mode Blueprint,
then save; the level's override through `set_property` on `WorldSettings`).
For a quick try in this session only: `set_game_mode_defaults` with the
class paths you change, then `inspect_game_mode` to read them back.

**A playable character from scratch:** section `playable-character`.

**Show a menu in a running game:**

1. Start PIE; wait until `pie_status` reports it running (skill
   `ue-pie-testing`).
2. `{"operation": "show_widget", "widget_class": "/Game/UI/WBP_Menu.WBP_Menu_C", "z_order": 10}`.
3. `set_input_mode` `ui_only`, `set_mouse_cursor` `show_cursor: true`.
4. Look: `gameplay_debug` `capture_pie` (skill `ue-pie-testing`).
5. Undo: `remove_widget` with the same class, `set_input_mode` `game_only`,
   `set_mouse_cursor` `show_cursor: false`.

**Pause and resume:** `{"operation": "set_pause", "paused": true}`, later
`"paused": false`.

**Change a DataTable row:** `list_data_tables` → `inspect_data_table` (column
names) → `get_row` (keep it as your undo) → `set_row` with only the changed
columns → check `is_new_row` and `row_data` → `asset` `save_asset`. Load
section `datatables` before the first `set_row`.

## Results through the bridge

Success is the tool's message, a blank line, then JSON: by default a task
envelope whose `data` holds the fields named here, next to the tool's own
`success` and `message`. A failure is only `Error: <message>`. Skill
`ue-tasks` covers the envelope and timeouts.

## Background in the plugin

`unreal_get_ue_context` has no category for this tool (skill `ue-context`);
its `actor` and `character` categories cover the classes a game mode spawns.
Epic's "Game Mode and Game State" page
(dev.epicgames.com/documentation/en-us/unreal-engine/game-mode-and-game-state-in-unreal-engine)
is the engine background summarized in section `game-mode`.

## See also

- `ue-enhanced-input`: input assets, and (section `runtime-setup`) adding
  the mapping context so keys work in play.
- `ue-blueprints`: creating and editing the game mode, controller and
  character Blueprints; `set_cdo_default`.
- `ue-character`, `ue-character-data`: characters, and the toolkit's
  character stats tables.
- `ue-pie-testing`: starting PIE, `pie_status`, captures, input injection.
- `ue-widgets`: the Widget Blueprint that `show_widget` displays.
- `ue-level-actors`, `ue-levels`: Player Start, `WorldSettings`, saving the
  level.
- `ue-assets`: finding and saving assets.
