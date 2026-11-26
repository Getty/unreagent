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
that the plugin itself defines (`Public/CharacterDataTypes.h`):

- a **character config**, a `UCharacterConfigDataAsset` (a `UDataAsset`):
  identity, soft references to a skeletal mesh and an animation class, base
  movement and combat values, capsule size, name tags, and an optional link
  to a stats table;
- a **stats table**, a `UDataTable` whose row struct is
  `FCharacterStatsRow`: vitals, speeds, multipliers and a level per row.

Through the plugin's MCP bridge the tool is listed as
`unreal_character_data`. It works only while the editor is running and the
plugin is reachable; `unreagent-iterate` covers the wait.

## Is this the right tool?

- **Both types live in the plugin's module, and that module is an Editor
  module** (`"Type": "Editor"` in `UELLMToolkit.uplugin`). The assets load
  only in the editor of a project with the plugin enabled (PIE included). A
  packaged game does not load Editor modules, so it cannot load these assets
  or anything that references their types — a consequence of the module type;
  packaging was not run (unverified in practice). Treat them as editor-side
  data. Game data that must ship belongs in the project's own row struct or
  DataAsset class.
- **Nothing in the engine reads them**: values reach a character only through
  `apply_character_data` (editor, one placed actor) or code that reads them.
  In the editor a Blueprint can read a config's fields and call its
  `GetStatsRow(RowName)` (BlueprintCallable; an empty name uses
  `DefaultStatsRowName`).
- **For a project's own row struct or DataAsset class, use
  `game_framework`** (skill `ue-game-framework`, section `datatables`). This
  tool refuses any table whose row struct is not `FCharacterStatsRow`.
- **For movement values on one placed character, `character` is enough**
  (skill `ue-character`).

## Must not get wrong

- **Asset writes save to disk at once**, before the call returns:
  `create_character_data`, `update_character_data`, `create_stats_table`,
  `add_stats_row`, `update_stats_row`, `remove_stats_row`. No undo.
- **Create does not check what is there.** Both creates build
  `<package_path>/<asset_name>` and save it. An existing asset of the same
  class is replaced; an object of another class under that name, loaded in
  the editor, makes the engine stop with a fatal error ("Cannot replace
  existing object of a different class", read in engine source, not run).
  **Search first** (recipe 1). Keep `asset_name` a plain name: no `/`, no
  `.`, and never a doubled `//` in the joined path (the engine's
  `CreatePackage` stops with a fatal error on `//`).
- **`apply_character_data` always writes the capsule**, also with every
  flag `false`, and does not move the mesh to match; it saves nothing (it
  marks the actor and level dirty).
- **References are not validated.** `skeletal_mesh` and `anim_blueprint` are
  stored as given; an animation Blueprint must be named by its generated
  class, `/Game/Anim/ABP_Hero.ABP_Hero_C`. Check `applied_settings` after an
  apply.
- **Values are clamped silently, unreadable ones skipped.** Compare the
  returned `config` or `row` with what you sent.
- **The stats-table link (`stats_table`, `default_stats_row`) cannot be set
  with this tool.** Section `config`, "Linking a stats table".

## Recipes

**1. Create a config**

1. `{"operation": "query_character_data", "search_name": "DA_Hero"}` and
   `asset_search` for the name (skill `ue-assets`). Stop if anything is
   there.
2. `{"operation": "create_character_data", "asset_name": "DA_Hero", "package_path": "/Game/Characters", "config_id": "hero", "display_name": "Hero", "base_walk_speed": 500, "base_jump_velocity": 520, "capsule_radius": 42, "capsule_half_height": 96, "is_player_character": true, "gameplay_tags": ["Player"]}`.
3. Verify: `{"operation": "get_character_data", "asset_path": "/Game/Characters/DA_Hero"}`
   and compare `config` field by field.

**2. Change a config**: `update_character_data` with `asset_path` and only
the fields that change; `gameplay_tags` replaces the whole list.

**3. Build a stats table**

1. `{"operation": "create_stats_table", "asset_name": "DT_HeroStats", "package_path": "/Game/Characters"}`.
2. `{"operation": "add_stats_row", "table_path": "/Game/Characters/DT_HeroStats", "row_name": "Level1", "level": 1, "max_health": 100, "walk_speed": 500}`,
   once per row.
3. Verify: `{"operation": "query_stats_table", "table_path": "/Game/Characters/DT_HeroStats"}`;
   `total` equals the rows you added.

**4. Apply a config to a placed character**

1. `character` `{"operation": "get_character_info", "character_name": "<name>"}`
   and keep `movement`, `capsule` and the mesh's position (skill
   `ue-character`) as your restore point.
2. `{"operation": "apply_character_data", "asset_path": "/Game/Characters/DA_Hero", "character_name": "<name>"}`
   (add `"apply_mesh": true`, `"apply_anim": true` when the config holds them).
3. Verify: `applied_settings` lists every part you asked for; then
   `character` `get_character_info` shows the new movement values and
   capsule.
4. Save the level: `asset` `{"operation": "save_all"}`.

## Sections

Load one with the `skills` tool (`name: "ue-character-data"`,
`section: "<name>"`) or read `<section>.md` next to this file.

| Section | What is in it | Load it when |
|---|---|---|
| `config` | config fields with types, defaults and clamps; `create`/`get`/`update`/`query_character_data` results; paths; how values are read; linking a stats table | you create, change or search a config |
| `stats-table` | the row fields, the five table operations and their results, when to use `game_framework` instead | you build or edit a stats table |
| `apply` | exactly what `apply_character_data` writes and skips, the capsule and mesh offset trap, verification and restore | you apply a config to a placed character |
| `failures` | error strings and silent failures with cause and fix, retrying after a timeout | a call fails, or succeeds without the effect you expected |

## Results through the bridge

Success is the tool's message, a blank line, then JSON. By default the bridge
goes through the task queue, so the fields named here sit under the task
envelope's `data` key. A failure is only `Error: <message>`. After a timeout
a write may still have been saved: read before you retry. Skill `ue-tasks`.

## Background in the plugin

`unreal_get_ue_context` with `category: "character"` shows both types as C++
and a worked example (skill `ue-context`). Its "apply to a runtime character"
step works on the editor's level, not a running game.

## See also

- `ue-character`: placed characters and their movement values.
- `ue-game-framework`: DataTables with any row type.
- `ue-assets`: finding assets, `save_all`, `set_asset_property`.
- `ue-blueprints`: reading a config from a character Blueprint.
