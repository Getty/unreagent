# character_data: config assets

Source: `MCPTool_CharacterData.h` / `.cpp` (`Private/MCP/Tools/`) and the
type in `Public/CharacterDataTypes.h` of the UE LLM Toolkit.

## Operations

| Operation | Required | Optional | Saves |
|---|---|---|---|
| `create_character_data` | `asset_name` | `package_path`, the config fields | yes |
| `query_character_data` | — | `search_name`, `search_tags`, `limit`, `offset` | no |
| `get_character_data` | `asset_path` | — | no |
| `update_character_data` | `asset_path` | the config fields | yes, also with no field passed |

`operation` names, for the whole tool, without aliases:
`create_character_data`, `query_character_data`, `get_character_data`,
`update_character_data`, `create_stats_table`, `query_stats_table`,
`add_stats_row`, `update_stats_row`, `remove_stats_row`,
`apply_character_data`. Anything else returns
`Unknown operation: '<op>'. Valid operations: <the ten names>`.

## Paths

- **`package_path`** is the folder, default `/Game/Characters`; it need not
  exist. It passes the toolkit's Blueprint path check, whose messages say
  "Blueprint path": not empty, at most 512 characters, not under `/Engine/`
  or `/Script/`, no `..`, none of `` <>|&;`$(){}[]!*?~ ``. A trailing `/` is
  fine (the join does not double it). A `//` inside it is not checked, and
  the engine's `CreatePackage` stops with a fatal error on one (engine source,
  not run).
- **`asset_name` is not checked at all.** Use a plain asset name
  (`DA_Hero`): letters, digits, `_`.
- **`asset_path`** names an existing config. The create operation returns the
  package path (`/Game/Characters/DA_Hero`); `query_character_data` returns
  the object path (`/Game/Characters/DA_Hero.DA_Hero`). The plugin's own
  example passes the package path; both forms are what `LoadObject` is given.
  Not checked before the load; a path that does not load is
  `Failed to load character config: <path>`.

## Config fields

For `create_character_data` and `update_character_data`. A new config starts
from the type's defaults; an update leaves a field you do not pass as it is.

| Parameter | C++ property | Type | Default | Accepted |
|---|---|---|---|---|
| `config_id` | `ConfigId` (FName) | string | none | any |
| `display_name` | `DisplayName` | string | empty | any |
| `description` | `Description` | string | empty | any |
| `is_player_character` | `bIsPlayerCharacter` | boolean | `false` | see "How values are read" |
| `skeletal_mesh` | `SkeletalMesh` (soft ref) | string | none | a skeletal mesh object path, not validated |
| `anim_blueprint` | `AnimBlueprintClass` (soft class ref) | string | none | an animation class path, not validated; for an Animation Blueprint the generated class, `/Game/Anim/ABP_Hero.ABP_Hero_C` |
| `base_walk_speed`, `base_run_speed` | `BaseWalkSpeed`, `BaseRunSpeed` | number | 600, 1000 | clamped to 0–10000 |
| `base_jump_velocity` | `BaseJumpVelocity` | number | 420 | clamped to 0–5000 |
| `base_acceleration` | `BaseAcceleration` | number | 2048 | clamped to 0–100000 |
| `base_ground_friction` | `BaseGroundFriction` | number | 8 | clamped to 0–100 |
| `base_air_control` | `BaseAirControl` | number | 0.35 | clamped to 0–1 |
| `base_gravity_scale` | `BaseGravityScale` | number | 1 | clamped to -10–10 |
| `base_health`, `base_stamina`, `base_damage`, `base_defense` | `BaseHealth`, … | number | 100, 100, 10, 0 | a negative value becomes 0 |
| `capsule_radius`, `capsule_half_height` | `CapsuleRadius`, `CapsuleHalfHeight` | number | 42, 96 | clamped to 1–500 |
| `gameplay_tags` | `GameplayTags` (FName array) | array of strings | empty | replaces the list; an entry that is not a string-readable value is left out |

- The tags are plain names (`FName`), not entries of the project's Gameplay
  Tag table; nothing checks them against it.
- The defaults are not the engine's: a bare `ACharacter` has a 34/88
  capsule and `AirControl` 0.05 (section `apply`). An untouched config
  applied to such a character changes its capsule to 42/96 and its air
  control to 0.35.
- `base_run_speed` and the combat values are stored only; nothing applies
  them.

## Results

`config` groups the fields: `config_id`, `display_name`, `description`,
`is_player_character` and `gameplay_tags` at the top; `skeletal_mesh` and
`anim_blueprint` only when set; the seven `base_…` movement values under
`movement`; `base_health`, `base_stamina`, `base_damage`, `base_defense`
under `combat`; `capsule_radius` and `capsule_half_height` under `collision`;
`stats_table` and `default_stats_row` only when a table is linked.

- `create_character_data` → `asset_path` (the package path), `asset_name`,
  `config`.
- `get_character_data`, `update_character_data` → `asset_path` (as you
  passed it), `config`.

## query_character_data

Lists config assets the Asset Registry knows.

| Parameter | Default | Meaning |
|---|---|---|
| `search_name` | none | case-sensitive substring of the asset name |
| `search_tags` | none | array of strings; a config matches when each is a substring of one of its tags. Loads every candidate asset |
| `limit` | 25 | clamped to 1–1000 |
| `offset` | 0 | entries to skip; negative counts as 0 |

The result has `configs`, `count`, `total`, `offset` and `limit`. Each entry
has `asset_path` (object path) and `asset_name`, plus `config_id`,
`display_name` and `is_player_character` when the asset loads. The message's
"showing a-b" is wrong for an empty page; read `count` and `total`.

## How values are read

Fields are read with the engine's `FJsonObject::TryGetNumberField` (into a
`double`), `TryGetStringField` and `TryGetBoolField`, which convert
(`Runtime/Json/Private/Dom/JsonObject.cpp`, `TJsonValueString` in
`Public/Dom/JsonValue.h`; read in the UE 5.8.4 source, not run, while the
toolkit targets UE 5.6 and 5.7):

- a number field also reads `true`/`false` as 1/0 and a string of an optional
  sign, digits and at most one dot (`"600"`); a lone `"-"` or `"."` reads as
  0 (`TCString::IsNumeric`); any other string is skipped without an error;
- a string field stores a number or a boolean as its text (`5`, `true`);
- **`is_player_character` reads any string and any number**: `true`, `yes`,
  `on` / `false`, `no`, `off` in any case, and every other string as `false`
  unless it starts with a non-zero integer, so a typo writes `false`
  (`FToBoolHelper`, `Runtime/Core/Private/Misc/CString.cpp`).

Send strings, numbers and booleans as those JSON types. An
`update_character_data` with no readable field still saves and succeeds.

## Linking a stats table

`StatsTable` and `DefaultStatsRowName` are reported by `get_character_data`
(`stats_table`, `default_stats_row`, only when a table is set) but no
parameter of this tool writes them. Options, none checked by this skill for
these two fields:

- `asset` `set_asset_property` on the config (skill `ue-assets`);
- `execute_script` with UE Python (skill `ue-python`), setting
  `stats_table` and `default_stats_row_name` on the loaded asset and saving
  it — the Python property names follow UE's snake_case mapping, unverified
  here;
- the editor's Details panel.

Verify with `get_character_data`: both fields appear once the link is set.
