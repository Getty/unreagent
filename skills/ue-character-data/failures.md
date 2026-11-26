# character_data: errors and failure modes

## Error strings

| Error | Cause | Fix |
|---|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or empty | add it (section `config` / `stats-table` tables) |
| `Unknown operation: '<op>'. Valid operations: …` | not one of the ten names | no aliases exist |
| `Failed to load character config: <path>` | no config at the path, or the asset is another class | `query_character_data` for the right path |
| `Failed to load DataTable: <path>` | no DataTable at the path | `asset_search` (skill `ue-assets`) |
| `DataTable '<path>' does not use FCharacterStatsRow struct` | a table of another row type | `game_framework` (skill `ue-game-framework`) |
| `Row '<n>' already exists` | `add_stats_row` on an existing name | `update_stats_row` |
| `Row '<n>' not found` | update or remove of a missing row (names are exact) | `query_stats_table` for the names |
| `Failed to save asset: <file>` | `UPackage::Save` did not succeed; likely causes (unverified): the file is locked or read-only, e.g. under source control without a checkout | the change is in memory: read before retrying; fix the file, then `asset` `save_all` |
| `Failed to create package: <path>` | the package could not be created | check `package_path` / `asset_name` |
| `Cannot access engine or script Blueprints` | `package_path` under `/Engine/` or `/Script/` | use `/Game/…` or a plugin content root |
| `Blueprint path cannot contain path traversal sequences` / `Blueprint path contains invalid character: '<c>'` / `Blueprint path exceeds maximum length of 512 characters` / `Blueprint path cannot be empty` | `package_path` failed the shared path check | fix `package_path` |
| `Character not found: <name>` | no `ACharacter` with that name or label in the editor's level | skill `ue-character`, section `failures` |
| `No active world. Open a level in the editor first` | `apply_character_data` without an open level | open one (skill `ue-levels`) |

## Silent failures

| Symptom | Cause | Fix |
|---|---|---|
| editor crashed on a create | an object of another class already had that name in memory, or the joined path held `//` (engine fatal errors, read in source, not run) | search first; plain `asset_name`; no `//` |
| an existing asset was overwritten | create does not check | search first (main document, recipe 1); restore from source control |
| a field did not change | value unreadable as the expected type (skipped) or clamped | compare the returned `config` / `row`; send JSON types |
| `is_player_character` is `false` though you sent a value | a string that is not `true`/`yes`/`on` (or a non-zero integer prefix) reads as `false` | send JSON `true` |
| `applied_settings` lacks `skeletal_mesh` or `anim_blueprint` | flag not set, reference empty, or the path does not load (an Animation Blueprint given as the asset, not `…_C`) | fix the reference with `update_character_data`, apply again |
| character floats or sinks after apply | capsule half height changed, mesh not moved | section `apply`, "The capsule trap" |
| capsule changed although only movement was wanted | the capsule is always applied | set the config's capsule fields to the character's current size first |
| the applied character reverts | level not saved, or the player pawn is spawned from the class | `asset` `save_all`; change the Blueprint (skill `ue-character`) |
| a packaged build fails on these assets or on Blueprints that use them | the types live in an Editor module (main document) | keep shipping data in the project's own types |
| `stats_table` missing in `get_character_data` | no table linked; this tool cannot link one | section `config`, "Linking a stats table" |
| a warning about a missing row in the output log after `add_stats_row` | the existence check's `FindRow` warns on a miss | expected; ignore |

## After a timeout

The asset operations save before they answer, so a call that timed out at
the bridge may have completed. Read first (`get_character_data`,
`query_stats_table`), then retry only what is missing; a second
`add_stats_row` for a row that landed fails with `Row '<n>' already exists`,
a second create silently replaces. Task envelope and timeouts: skill
`ue-tasks`.
