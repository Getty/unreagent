# game_framework: when a call fails

The recipes, "What a success does not prove" and the passage on each
operation are in the skill itself. The errors of the DataTable operations
are in section `datatables`.

## set_game_mode_defaults errors

| Error | Cause |
|---|---|
| `No DefaultGameMode set in WorldSettings` | the level has no GameMode Override. Unlike `inspect_game_mode`, a live game mode does not count |
| `Failed to load pawn class: <path>` | not a class path, or not a pawn class |
| `Failed to load player controller class: <path>` | likewise |
| `Failed to load HUD class: <path>` | likewise |
| `No class overrides specified. Provide at least one of: default_pawn_class, player_controller_class, hud_class` | all three absent or empty |

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
