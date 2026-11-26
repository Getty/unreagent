# game_framework: when a call fails

The must-not-get-wrong list is in the skill itself; each operation in full
is in sections `game-mode`, `player-controller` and `datatables` (which has
the DataTable errors). A character that does not play is in section
`playable-character`, "When it does not work".

## set_game_mode_defaults errors

| Error | Cause |
|---|---|
| `No DefaultGameMode set in WorldSettings` | the level has no GameMode Override. Unlike `inspect_game_mode`, a live game mode does not count |
| `Failed to load pawn class: <path>` | not a class path, or not a pawn class |
| `Failed to load player controller class: <path>` | likewise |
| `Failed to load HUD class: <path>` | likewise |
| `No class overrides specified. Provide at least one of: default_pawn_class, player_controller_class, hud_class` | all three absent or empty |
| `Failed to get game mode CDO` | the GameMode Override holds a class that is not a game mode, possible after `set_property` on `WorldSettings`, which does not check the base class (section `game-mode`) |
| `Editor not available` / `No editor world available` | the editor is still starting or shutting down |

## Other errors

| Error | Cause |
|---|---|
| `Unknown operation: '<op>'. Valid: …` | not one of the twelve and not an alias (section `aliases`); the name shown is lowercased |
| `Missing required parameter: <name>` | a required string absent or `""` |
| `Unknown error` | the editor-side helper returned no message (unexpected) |

## How paused and show_cursor are read

For both, **send a JSON boolean, `true` or `false`.** The tool reads the
value through `ExtractOptionalBool` (`MCPToolBase.h`), which calls the
engine's `FJsonObject::TryGetBoolField`, and that reads any string and any
number. `true`, `yes`, `on` and `false`, `no`, `off` count in any case; every
other string is `false` unless it starts with a non-zero integer, and a
number is `true` when it is not 0.
The editor language's words for True, Yes, False and No count as well
(`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`).
So `"false"` is read as `false`, and a
typo such as `"ture"` is read as `false` too. Only `null`, an array or an
object is not read, and then the default `true` applies without an error
(`TJsonValueString::TryGetBool`,
`Engine/Source/Runtime/Json/Public/Dom/JsonValue.h`; read in the UE 5.8.4
source, not run, while the toolkit targets UE 5.6 and 5.7). Read `paused` or
`show_cursor` in the result.

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
| `set_row` reports `is_new_row: true` for an update | the `row_name` was misspelt or cased differently | `delete_row` the new row, retry with the name from `inspect_data_table` |
| a struct or transform column did not change, no error | unknown keys inside a struct object are skipped; a transform in `get_row` form is skipped | section `datatables`, value forms; send UE text form, read back |
| input stops working after `set_input_mode` `ui_only` | UI only blocks game input | `set_input_mode` `game_only` |
| `inspect_game_mode` shows the old classes during PIE | it reads the editor world and class defaults, not the PIE session | expected; look at the session itself: `capture_pie` (skill `ue-pie-testing`) |
| `Mixing AGameStateBase with AGameMode is not compatible` in the log | game mode and game state from different families | pair `GameMode`+`GameState` or `GameModeBase`+`GameStateBase` (section `game-mode`) |
