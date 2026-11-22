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
of a running Play In Editor session, and DataTable assets. Through the
plugin's MCP bridge it is listed as `unreal_game_framework`. It works only
while the editor is running and the plugin is reachable; `unreagent-iterate`
covers the wait for the plugin.

Section `datatables` holds the DataTable operations in full: load it before
`set_row`.

## Recipes

**Find out what the level spawns**

1. `{"operation": "inspect_game_mode"}`. The result names `game_mode_class`,
   `default_pawn_class`, `player_controller_class`, `hud_class` and
   `spectator_class`.
2. `No game mode configured. …` means the level has no GameMode Override. The
   tool does not read the project's default game mode (see "Game mode").

**Make the level spawn another pawn, controller or HUD**

1. `{"operation": "inspect_game_mode"}` and note the old values.
2. `{"operation": "set_game_mode_defaults", "default_pawn_class": "/Game/Characters/BP_Hero.BP_Hero_C"}`.
   Pass only the classes you change; each is a class path.
3. `{"operation": "inspect_game_mode"}` to read the values back.
4. Nothing is saved yet. For a Blueprint game mode, save its asset: `asset`
   `{"operation": "save_asset", "asset_path": "<the game mode Blueprint>"}`
   (skill `ue-assets`). The asset path is `game_mode_class` without its
   last part: `/Game/GM/BP_GM.BP_GM_C` is the class, `/Game/GM/BP_GM` the
   asset. Then read "What a success does not prove": this
   operation is the quick route, not the durable one.

**Show a widget in a running game**

1. Start PIE and wait until `pie_status` reports `running: true` (skill
   `ue-pie-testing`).
2. `{"operation": "show_widget", "widget_class": "/Game/UI/WBP_Menu.WBP_Menu_C", "z_order": 10}`.
3. For a menu: `{"operation": "set_input_mode", "input_mode": "ui_only"}` and
   `{"operation": "set_mouse_cursor", "show_cursor": true}`.
4. Look at it: `gameplay_debug` `capture_pie` (skill `ue-pie-testing`).
5. Undo: `{"operation": "remove_widget", "widget_class": "<same path>"}`,
   `set_input_mode` with `game_only`, `set_mouse_cursor` with
   `show_cursor: false`.

**Pause and resume a running game**

`{"operation": "set_pause", "paused": true}`, later
`{"operation": "set_pause", "paused": false}`. Always pass `paused`.

**Read and change a DataTable row**

1. `{"operation": "list_data_tables", "folder_path": "/Game/Data"}`, take a
   `path`.
2. `{"operation": "inspect_data_table", "table_path": "<path>"}` for the
   column names and types and the row names.
3. `{"operation": "get_row", "table_path": "<path>", "row_name": "<row>"}`.
4. `{"operation": "set_row", "table_path": "<path>", "row_name": "<row>", "row_data": {"<column>": <value>}}`.
   Only the columns you pass change. Value forms: section `datatables`.
5. Save: `asset` `{"operation": "save_asset", "asset_path": "<path>"}` (skill
   `ue-assets`).

## What a success does not prove

- **No operation of this tool saves.** `set_game_mode_defaults`, `set_row`
  and `delete_row` change objects in memory and mark their package dirty.
- **`set_game_mode_defaults` writes the game mode class's default object, in
  memory.** It does not edit a Blueprint through the Blueprint editor and
  does not compile. What the code does: assign the classes, then mark the
  class's package dirty. From there:
  - a **Blueprint** game mode has an asset that can be saved (recipe above).
    Whether the values survive a later compile of that Blueprint cannot be
    read from the plugin's source. The durable route is `blueprint_modify`
    `set_cdo_default` with `property` `DefaultPawnClass`,
    `PlayerControllerClass` or `HUDClass`, then compile and save (skill
    `ue-blueprints`, section `modify`);
  - a **C++** game mode has no asset to save. The change lasts at most
    until the editor closes; make it in the class's constructor instead.
- **It changes the class, not the level.** Every level that uses the same
  game mode class gets the new pawn, controller or HUD.
- **A failed `set_game_mode_defaults` can be half applied.** The classes are
  loaded and assigned one after another: pawn, player controller, HUD. When
  a later one fails to load, the earlier ones are already assigned, the call
  returns an error, and nothing is marked dirty. Run `inspect_game_mode`
  after an error.
- **The PIE operations last as long as the session.** Input mode, pause,
  cursor and shown widgets are state of the running game. Stopping PIE
  discards them; nothing reaches the level or an asset.
- **`set_pause` reports what it asked for.** The engine's answer to the pause
  request is not checked: the result says `paused: true` also when the game
  refuses to pause.
- **`remove_widget` succeeds when it removed nothing.** Read `removed_count`.
- **`set_row` can fail half way on an existing row.** Columns are written in
  the order given; when one fails, the ones before it stay changed in memory,
  and the table is not marked dirty. Run `get_row` after an error.
- **Unknown keys inside a struct value are ignored** by `set_row`, without a
  message. An unknown column is an error.

## Operations

| Operation | Required | Optional | Needs |
|---|---|---|---|
| `inspect_game_mode` | — | — | an open level |
| `set_game_mode_defaults` | at least one of `default_pawn_class`, `player_controller_class`, `hud_class` | — | a level with a GameMode Override |
| `set_input_mode` | `input_mode` | — | PIE |
| `show_widget` | `widget_class` | `z_order` | PIE |
| `remove_widget` | `widget_class` | — | PIE |
| `set_pause` | — | `paused` (default `true`) | PIE |
| `set_mouse_cursor` | — | `show_cursor` (default `true`) | PIE |
| `list_data_tables` | — | `folder_path` (default `/Game`) | — |
| `inspect_data_table` | `table_path` | — | — |
| `get_row` | `table_path`, `row_name` | — | — |
| `set_row` | `table_path`, `row_name`, `row_data` | — | — |
| `delete_row` | `table_path`, `row_name` | — | — |

`operation` is lowercased before dispatch. An unknown one returns
`Unknown operation: '<op>'. Valid: <the twelve names>`, with the name after
alias resolution.

### Aliases

The code accepts these besides the twelve names. The tool's description does
not list them. Prefer the full names: an alias changes the name only, never
the parameters.

| Alias | Runs | Mind |
|---|---|---|
| `inspect`, `info`, `get_info` | `inspect_game_mode` | not `inspect_data_table` |
| `set_defaults` | `set_game_mode_defaults` | — |
| `add_widget` | `show_widget` | — |
| `remove_all_widgets` | `remove_widget` | still needs `widget_class`, and removes that class only |
| `pause`, `unpause` | `set_pause` | **`unpause` without `paused: false` pauses**: both run `set_pause`, whose default is `true` |
| `cursor`, `mouse_cursor` | `set_mouse_cursor` | without `show_cursor` it shows the cursor |
| `list`, `list_tables` | `list_data_tables` | — |
| `inspect_table`, `table_info` | `inspect_data_table` | — |
| `get`, `get_row_data` | `get_row` | — |

`set_input_mode`, `set_row` and `delete_row` have no alias.

## Class paths

`default_pawn_class`, `player_controller_class`, `hud_class` and
`widget_class` are handed to the engine's class loader. They take a class
path:

| Class | Path |
|---|---|
| Blueprint | the generated class, with the `_C` suffix: `/Game/UI/WBP_Menu.WBP_Menu_C` |
| C++ | `/Script/<Module>.<Class>` without the `A` or `U` prefix: `/Script/Engine.DefaultPawn` |

The asset path of a Blueprint (`/Game/UI/WBP_Menu`) is not a class path. The
tool does not append `_C` for you, unlike `spawn_actor` (skill
`ue-level-actors`). The class must derive from the expected base: a pawn, a
player controller, a HUD actor, a user widget. Anything else fails with the
same message as a path that does not exist.

## Game mode

Both operations read the level open in the editor.

**`inspect_game_mode`** takes no parameters. It takes the class of a live
game mode in the editor's world when there is one, otherwise the level's
GameMode Override (World Settings), and reads the classes from that class's
defaults.
The result:

| Field | Holds |
|---|---|
| `game_mode_class` | class path of the game mode |
| `default_pawn_class`, `player_controller_class`, `hud_class`, `spectator_class` | class path, or the string `None` |
| `from_pie` | `true` when the editor's world has a live game mode object. The code asks the editor's world, not the PIE world; do not use this field to detect PIE, use `pie_status` (skill `ue-pie-testing`) |

A level without an override returns
`No game mode configured. Neither AuthGameMode nor WorldSettings->DefaultGameMode is set.`
The project's default game mode (Project Settings, Maps & Modes) is not
consulted, so this error does not mean the game has no game mode.

**`set_game_mode_defaults`** takes up to three class paths. An empty string
counts as absent, so a class cannot be cleared to `None` with this operation.
The spectator class has no parameter. The result has `changes`, a list of
strings such as `DefaultPawnClass=/Game/Characters/BP_Hero.BP_Hero_C`, and the
message `Updated <n> game mode defaults`.

| Error | Cause |
|---|---|
| `No DefaultGameMode set in WorldSettings` | the level has no GameMode Override. Unlike `inspect_game_mode`, a live game mode does not count |
| `Failed to load pawn class: <path>` | not a class path, or not a pawn class |
| `Failed to load player controller class: <path>` | likewise |
| `Failed to load HUD class: <path>` | likewise |
| `No class overrides specified. Provide at least one of: default_pawn_class, player_controller_class, hud_class` | all three absent or empty |

## Player controller (PIE only)

These five need a running PIE session. Without one each returns
`PIE is not running. Player Controller operations require Play-In-Editor.`
`start_pie` is asynchronous: wait for `pie_status` (skill `ue-pie-testing`).

Four of them act on the session's first player controller, and return
`No player controller found in PIE world` when it has none. `remove_widget`
uses no player controller and never returns that error.

**`set_input_mode`** — `input_mode` is `game_only`, `ui_only` or
`game_and_ui`, lowercased by the tool. Each sets the engine's plain input
mode with its default settings: no widget is given focus, and no mouse lock
or capture option can be passed. The result has `input_mode`. Any other value
returns `Invalid input_mode: '<v>'. Valid: game_only, ui_only, game_and_ui`.
The input mode does not show the cursor; that is `set_mouse_cursor`.

**`show_widget`** — creates a new instance of `widget_class`, owned by the
player controller, and adds it to the viewport at `z_order` (default 0). The
result has `widget_class` and `z_order`. **Each call adds one more
instance**; the tool does not check for one already showing. The widget's own
variables cannot be set here, and no reference to the instance comes back.
Errors: `Failed to load widget class: <path>`,
`Failed to create widget of class: <path>`.

**`remove_widget`** — removes every widget of `widget_class` that is in a
viewport, whoever added it: the game's own HUD of that class goes too. (The
engine call it uses also covers classes derived from `widget_class` by
default.) The result has `removed_count` and `widget_class`. There is no
operation that removes all widgets of all classes.

**`set_pause`** — `paused`, default `true`. The result has `paused` as
requested and the message `Game paused` or `Game unpaused`.

**`set_mouse_cursor`** — `show_cursor`, default `true`. The result has
`show_cursor`.

For both, **send a JSON boolean, `true` or `false`.** The tool reads the
value through `ExtractOptionalBool` (`MCPToolBase.h`), which calls the
engine's `FJsonObject::TryGetBoolField`, and that reads any string and any
number. `true`, `yes`, `on` and `false`, `no`, `off` count in any case; every
other string is `false` unless it starts with a non-zero integer, and a
number is `true` when it is not 0. So `"false"` is read as `false`, and a
typo such as `"ture"` is read as `false` too. Only `null`, an array or an
object is not read, and then the default `true` applies without an error
(`TJsonValueString::TryGetBool`,
`Engine/Source/Runtime/Json/Public/Dom/JsonValue.h`; read in the UE 5.8.4
source, not run, while the toolkit targets UE 5.6 and 5.7). Read `paused` or
`show_cursor` in the result.

## DataTables

Five operations work on any DataTable, whatever its row type:
`list_data_tables`, `inspect_data_table`, `get_row`, `set_row` and
`delete_row`. They need neither a level nor PIE. Parameters, results, value
forms and errors are in section `datatables`. In short:

- `list_data_tables` searches `folder_path` and its subfolders and returns
  `tables` (each `path`, `name`, and `row_struct_name` when known) and
  `count`;
- `inspect_data_table` returns `columns` and `row_names`;
- `get_row` returns `row_name` and `row_data`;
- `set_row` updates the given columns of a row, or adds the row when the name
  is new, and returns `row_data` and `is_new_row`;
- `delete_row` returns `deleted_row_name`.

The tool cannot create a DataTable or change its row type. For the toolkit's
character stats tables, `character_data` creates the table and checks values
(skill `ue-character-data`).

## Results through the bridge

Success is the tool's message, a blank line, then JSON. By default the bridge
goes through the task queue, so that JSON is a task envelope, and the fields
named here sit under its `data` key, next to a `success` and a `message`
field of the tool's own. A failure is only `Error: <message>`. Skill
`ue-tasks` covers the envelope and timeouts.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `PIE is not running. …` right after `start_pie` | the session is still starting | poll `pie_status`, then repeat |
| `Failed to load widget class` or `… pawn class` for a path that exists | the asset path was passed, or the class has another base | add `.<Name>_C`; check the parent class |
| the game paused after `unpause` | the alias ran `set_pause` with its default | `set_pause` with `paused: false` |
| the widget shows twice | `show_widget` was repeated | `remove_widget`, then `show_widget` once |
| the new pawn class is gone after an editor restart | the game mode asset was not saved, the game mode is a C++ class, or the value did not survive the Blueprint being compiled again, which this skill could not settle from the source | for a Blueprint game mode use `blueprint_modify` `set_cdo_default`, then compile and save (skill `ue-blueprints`, section `modify`); see "What a success does not prove" |
| `No game mode configured. …` but the game plays | the level relies on the project's default game mode | set a GameMode Override in the level's World Settings, or read the project setting in the editor |
| a row change is gone after a restart | the table was not saved | `asset` `save_asset` |

## Background in the plugin

`unreal_get_ue_context` has no category for this tool (skill `ue-context`).
Its `actor` and `character` categories cover the classes a game mode spawns.

## See also

- `ue-pie-testing`: starting PIE, `pie_status`, capturing the PIE viewport.
- `ue-widgets`: building the Widget Blueprint that `show_widget` displays.
- `ue-blueprints`: the durable way to set a Blueprint game mode's defaults.
- `ue-character`, `ue-character-data`: characters in the level, and the
  toolkit's character stats tables.
- `ue-assets`: finding and saving assets.
