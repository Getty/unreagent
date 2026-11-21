---
name: ue-character-data
description: >
  Create and edit the toolkit's own character config DataAssets and character
  stats DataTables with character_data, and apply a config to a character
  placed in the level. Use before you store character stats in these assets
  or apply them; for any other DataTable use ue-game-framework.
toolkit_tools: [character_data]
---

# UE character data: character_data

The UE LLM Toolkit plugin's **`character_data`** tool manages two asset kinds
that the plugin itself defines:

- a **character config**, a `UCharacterConfigDataAsset`: identity, mesh and
  animation references, base movement and combat values, capsule size, tags;
- a **stats table**, a DataTable whose rows are `FCharacterStatsRow`: vitals,
  speeds, multipliers and a level per row.

Both types live in the plugin's module, so the assets load only in a project
that has the plugin. Nothing in the engine reads them: a game uses the values
only where its own code or Blueprints do. Through the plugin's MCP bridge the
tool is listed as `unreal_character_data`. It works only while the editor is
running and the plugin is reachable; `unreagent-iterate` covers the wait.

## Recipes

**Create a character config**

1. `{"operation": "query_character_data", "search_name": "DA_Hero"}` and an
   `asset_search` for the name (skill `ue-assets`): the create operation does
   not check whether something is already there.
2. `{"operation": "create_character_data", "asset_name": "DA_Hero", "package_path": "/Game/Characters", "config_id": "hero", "display_name": "Hero", "base_walk_speed": 600, "base_health": 100, "is_player_character": true, "gameplay_tags": ["Player"]}`.
3. `{"operation": "get_character_data", "asset_path": "/Game/Characters/DA_Hero"}`
   and compare `config` with what you sent: values are clamped silently.

**Change a config**

`{"operation": "update_character_data", "asset_path": "/Game/Characters/DA_Hero", "base_jump_velocity": 520}`.
Only the fields you pass change, except `gameplay_tags`, which replaces the
whole list.

**Build a stats table**

1. `{"operation": "create_stats_table", "asset_name": "DT_HeroStats"}`.
2. `{"operation": "add_stats_row", "table_path": "/Game/Characters/DT_HeroStats", "row_name": "Level1", "stats_id": "lvl1", "max_health": 100, "walk_speed": 600, "level": 1}`,
   once per row.
3. `{"operation": "query_stats_table", "table_path": "/Game/Characters/DT_HeroStats"}`.
4. Later: `update_stats_row` with the fields to change, `remove_stats_row`
   with `table_path` and `row_name`.

**Apply a config to a placed character**

1. `character` `{"operation": "list_characters"}` for the name (skill
   `ue-character`).
2. `{"operation": "apply_character_data", "asset_path": "/Game/Characters/DA_Hero", "character_name": "<name>", "apply_mesh": true, "apply_anim": true}`.
3. Read `applied_settings`: it names what was written. A mesh or animation
   class that did not load is left out without an error.
4. Save the level: `asset` `{"operation": "save_all"}` (skill `ue-assets`).

## What a success does not prove

- **Asset writes are saved, at once.** `create_character_data`,
  `update_character_data`, `create_stats_table`, `add_stats_row`,
  `update_stats_row` and `remove_stats_row` write the package to disk before
  they return. There is no separate save step and no undo.
- **`apply_character_data` saves nothing.** It writes into the placed actor
  in memory and marks the actor's package and the level dirty.
- **A failed save leaves the change in memory.** `Failed to save asset: <file>`
  comes after the asset was changed in the editor. Read the asset before you
  retry, so a row is not added twice (`Row '<n>' already exists` then tells
  you the first attempt reached memory).
- **Create does not check for an existing asset.** Both create operations
  build a new object at `<package_path>/<asset_name>` and save it, whatever
  is there. Check first (recipe 1).
- **References are not validated.** `skeletal_mesh` and `anim_blueprint` are
  stored as given; a path that names nothing still succeeds.
- **Values are clamped silently**, and a parameter the tool cannot read as
  the type it expects is skipped, not rejected. Whether a value of another
  JSON type converts (a numeric string to a number, a number to a string)
  depends on the engine's JSON classes, which this skill has not verified.
  Send strings, numbers and booleans as those JSON types.
  `update_character_data` with no readable field still saves and succeeds.
  Compare the returned `config` or `row`.
- **The link to a stats table cannot be set.** `get_character_data` reports
  `stats_table` and `default_stats_row`, but no parameter writes them. Set
  them in the editor; `asset` `set_asset_property` (skill `ue-assets`) is
  the tool to try, which this skill has not checked for these two fields.
- **Applying a config changes one actor in the editor's level**, not the
  class and not a running game. The header calls the target a "runtime
  character"; the code takes the editor's world, as `character` does (skill
  `ue-character`, "What a success does not prove").

## Operations

| Operation | Required | Optional |
|---|---|---|
| `create_character_data` | `asset_name` | `package_path`, the config fields |
| `query_character_data` | — | `search_name`, `search_tags`, `limit`, `offset` |
| `get_character_data` | `asset_path` | — |
| `update_character_data` | `asset_path` | the config fields |
| `create_stats_table` | `asset_name` | `package_path` |
| `query_stats_table` | `table_path` | `row_name`, `limit`, `offset` |
| `add_stats_row` | `table_path`, `row_name` | the row fields |
| `update_stats_row` | `table_path`, `row_name` | the row fields |
| `remove_stats_row` | `table_path`, `row_name` | — |
| `apply_character_data` | `asset_path`, `character_name` | `apply_movement`, `apply_mesh`, `apply_anim` |

`operation` must be one of these names; there are no aliases. Anything else
returns `Unknown operation: '<op>'. Valid operations: <the ten names>`.

## Paths

- **`package_path`** is the folder, default `/Game/Characters`. It passes the
  same check as Blueprint paths (skill `ue-blueprints`, "Paths"), with
  messages that say "Blueprint path": no `/Engine/` or `/Script/`, no `..`,
  none of `` <>|&;`$(){}[]!*?~ ``, at most 512 characters. **`asset_name` is
  not checked.**
- **`asset_path`** and **`table_path`** name an existing asset. The create
  operations return the package path (`/Game/Characters/DA_Hero`);
  `query_character_data` returns the object path
  (`/Game/Characters/DA_Hero.DA_Hero`). The plugin's own example passes the
  package path to the later calls. These two parameters are not checked
  before the load; a path that does not load returns
  `Failed to load character config: <path>` or
  `Failed to load DataTable: <path>`.
- A table with any other row type returns
  `DataTable '<path>' does not use FCharacterStatsRow struct`. Use
  `game_framework` for it (skill `ue-game-framework`).

## Config fields

For `create_character_data` and `update_character_data`. A new config starts
from the defaults; an update leaves a field you do not pass as it is.

| Parameter | Type | Default | Accepted |
|---|---|---|---|
| `config_id` | string | none | any |
| `display_name` | string | empty | any |
| `description` | string | empty | any |
| `is_player_character` | boolean | `false` | — |
| `skeletal_mesh` | string | none | a skeletal mesh path, not validated |
| `anim_blueprint` | string | none | an animation class path, not validated; for an Animation Blueprint that is the generated class, `/Game/Anim/ABP_Hero.ABP_Hero_C` |
| `base_walk_speed`, `base_run_speed` | number | 600, 1000 | clamped to 0–10000 |
| `base_jump_velocity` | number | 420 | clamped to 0–5000 |
| `base_acceleration` | number | 2048 | clamped to 0–100000 |
| `base_ground_friction` | number | 8 | clamped to 0–100 |
| `base_air_control` | number | 0.35 | clamped to 0–1 |
| `base_gravity_scale` | number | 1 | clamped to -10–10 |
| `base_health`, `base_stamina`, `base_damage`, `base_defense` | number | 100, 100, 10, 0 | a negative value becomes 0 |
| `capsule_radius`, `capsule_half_height` | number | 42, 96 | clamped to 1–500 |
| `gameplay_tags` | array of strings | empty | replaces the list; an entry the tool cannot read as a string is left out without an error. Send strings |

The tags are plain names, not entries of the project's Gameplay Tag table.

The returned `config` groups them: `config_id`, `display_name`,
`description`, `is_player_character` and `gameplay_tags` at the top;
`skeletal_mesh` and `anim_blueprint` only when set; the `base_…` movement
values under `movement`, the four combat values under `combat`, the capsule
values under `collision`; `stats_table` and `default_stats_row` only when a
table is linked.

`create_character_data` returns `asset_path`, `asset_name` and `config`;
`get_character_data` and `update_character_data` return `asset_path` (as you
passed it) and `config`.

## query_character_data

Lists the config assets the Asset Registry knows.

| Parameter | Default | Meaning |
|---|---|---|
| `search_name` | none | substring of the asset name |
| `search_tags` | none | array of strings; a config matches when each is a substring of one of its tags. Loads every candidate asset |
| `limit` | 25 | clamped to 1–1000 |
| `offset` | 0 | entries to skip |

The result has `configs`, `count`, `total`, `offset` and `limit`. Each entry
has `asset_path` and `asset_name`, and `config_id`, `display_name` and
`is_player_character` when the asset loads.

## Stats rows

For `add_stats_row` and `update_stats_row`. A new row starts from the
defaults; an update leaves a field you do not pass as it is.

| Parameter | Type | Default | Accepted |
|---|---|---|---|
| `stats_id` | string | none | any |
| `display_name` | string | empty | any |
| `base_health`, `max_health`, `base_stamina`, `max_stamina` | number | 100 each | a negative value becomes 0 |
| `walk_speed`, `run_speed` | number | 600, 1000 | clamped to 0–10000 |
| `jump_velocity` | number | 420 | clamped to 0–5000 |
| `damage_multiplier`, `defense_multiplier`, `xp_multiplier` | number | 1 each | clamped to 0–10 |
| `level` | number | 1 | truncated to an integer, at least 1 |
| `tags` | array of strings | empty | replaces the list |

`display_name`, `base_health` and `base_stamina` are also config fields; which
one a call writes follows from the operation.

- **`create_stats_table`** returns `table_path`, `asset_name` and
  `row_struct: "FCharacterStatsRow"`.
- **`query_stats_table`** takes `row_name` as a substring filter, `limit`
  (default 25, clamped to 1–1000) and `offset`. The result has `table_path`,
  `rows`, `count` and `total`; each row has `row_name` and the fields above.
- **`add_stats_row`** returns `table_path` and `row`. An existing name
  returns `Row '<n>' already exists`.
- **`update_stats_row`** returns `table_path` and `row`; a missing row
  returns `Row '<n>' not found`.
- **`remove_stats_row`** returns `table_path` and `removed_row`; a missing
  row returns `Row '<n>' not found`.

## apply_character_data

Copies values from a config onto one `ACharacter` in the editor's level.
`character_name` is the actor's object name or label, checked as in skill
`ue-character`, "character_name".

| Part | Flag | Default | What is written |
|---|---|---|---|
| movement | `apply_movement` | `true` | walk speed, acceleration, jump velocity, ground friction, air control and gravity scale, from the six matching `base_…` fields |
| capsule | none | always | radius and half height |
| mesh | `apply_mesh` | `false` | the config's skeletal mesh, when set and loadable |
| animation | `apply_anim` | `false` | the config's animation class, when set and loadable |

**The capsule is always written**, also with every flag `false`.
`base_run_speed`, the combat values, the tags and the stats table are not
applied to anything.

The result has `character_name`, `config_applied` (the path you passed) and
`applied_settings`, a list out of `movement`, `capsule`, `skeletal_mesh` and
`anim_blueprint`. The call succeeds whatever that list holds, so a requested
part that is missing from it was not applied: the reference is empty or did
not load, or the character lacks the component. Errors:
`Character not found: <name>`, `Failed to load character config: <path>`.

## Results through the bridge

Success is the tool's message, a blank line, then JSON. By default the bridge
goes through the task queue, so that JSON is a task envelope, and the fields
named here sit under its `data` key. A failure is only `Error: <message>`.
After a timeout a write may still have been saved: read before you retry.
Skill `ue-tasks` covers the envelope and timeouts.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or empty |
| `Failed to load character config: <path>` | no config asset at the path |
| `Failed to load DataTable: <path>` | no DataTable at the path |
| `DataTable '<path>' does not use FCharacterStatsRow struct` | not a stats table of this tool |
| `Row '<n>' already exists` / `Row '<n>' not found` | use `update_stats_row` / `add_stats_row` |
| `Failed to save asset: <file>` | changed in memory, not on disk |
| `Failed to create package: <path>` | the package could not be created |
| `Cannot access engine or script Blueprints` | `package_path` under `/Engine/` or `/Script/` |

## Background in the plugin

`unreal_get_ue_context` with `category: "character"` shows both types as C++
and a worked example (skill `ue-context`). Its step 5 says "runtime
character"; see "What a success does not prove".

## See also

- `ue-character`: the characters in the level and their movement values.
- `ue-game-framework`: DataTables with any row type.
- `ue-assets`: finding assets, `save_all`, `set_asset_property`.
