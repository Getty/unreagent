# character_data: stats tables

A stats table is a `UDataTable` with row struct `FCharacterStatsRow`
(`Public/CharacterDataTypes.h` of the UE LLM Toolkit). Every table operation
loads the table and refuses one with another row struct:
`DataTable '<path>' does not use FCharacterStatsRow struct`. For those, and
for any table of the project's own struct, use `game_framework`
(`inspect_data_table`, `get_row`, `set_row`, `delete_row`; skill
`ue-game-framework`, section `datatables`).

## Operations

| Operation | Required | Optional | Saves |
|---|---|---|---|
| `create_stats_table` | `asset_name` | `package_path` | yes |
| `query_stats_table` | `table_path` | `row_name` (filter), `limit`, `offset` | no |
| `add_stats_row` | `table_path`, `row_name` | the row fields | yes |
| `update_stats_row` | `table_path`, `row_name` | the row fields | yes, also with no field passed |
| `remove_stats_row` | `table_path`, `row_name` | — | yes |

`package_path` and `asset_name` follow section `config`, "Paths" — the same
checks, the same fatal-error risks, and create does not look for an existing
asset. `table_path` is not checked before the load; a path that does not load
is `Failed to load DataTable: <path>`. The create returns the package path;
pass that.

## Row fields

For `add_stats_row` and `update_stats_row`. A new row starts from the
struct's defaults; an update leaves a field you do not pass as it is.

| Parameter | C++ property | Type | Default | Accepted |
|---|---|---|---|---|
| `stats_id` | `StatsId` (FName) | string | none | any |
| `display_name` | `DisplayName` | string | empty | any |
| `base_health`, `max_health`, `base_stamina`, `max_stamina` | `BaseHealth`, … | number | 100 each | a negative value becomes 0 |
| `walk_speed`, `run_speed` | `WalkSpeed`, `RunSpeed` | number | 600, 1000 | clamped to 0–10000 |
| `jump_velocity` | `JumpVelocity` | number | 420 | clamped to 0–5000 |
| `damage_multiplier`, `defense_multiplier`, `xp_multiplier` | `DamageMultiplier`, … | number | 1 each | clamped to 0–10 |
| `level` | `Level` (int32) | number | 1 | truncated toward zero, then at least 1 |
| `tags` | `Tags` (FName array) | array of strings | empty | replaces the list |

`display_name`, `base_health` and `base_stamina` exist on configs too; the
operation decides which asset a call writes. Values are read as described in
section `config`, "How values are read": send JSON numbers and strings.

Nothing applies a row to a character: `apply_character_data` uses the
config's own `base_…` values, not its stats table. A row is data for the
game's own logic (or a Blueprint calling the config's `GetStatsRow`).

## Results

- **`create_stats_table`** → `table_path` (package path), `asset_name`,
  `row_struct: "FCharacterStatsRow"`.
- **`query_stats_table`** → `table_path`, `rows`, `count`, `total` (no
  `offset`/`limit` echo). `row_name` is a case-sensitive substring filter;
  `limit` defaults to 25, clamped to 1–1000. Each row has `row_name` and
  every field above.
- **`add_stats_row`** → `table_path`, `row`. An existing name:
  `Row '<n>' already exists`.
- **`update_stats_row`** → `table_path`, `row`. Missing:
  `Row '<n>' not found`.
- **`remove_stats_row`** → `table_path`, `removed_row`. Missing:
  `Row '<n>' not found`.

The existence checks use `FindRow` with an empty context string; a miss also
writes a warning about the missing row to the output log, which is expected
noise for `add_stats_row` (engine behaviour of `UDataTable::FindRow`, not
run).

## Workflow: a level progression table

1. Search first: `asset_search` for the name (skill `ue-assets`).
2. `{"operation": "create_stats_table", "asset_name": "DT_HeroStats", "package_path": "/Game/Characters"}`.
3. One `add_stats_row` per level, a row name that sorts and reads well
   (`Level01` … `Level10`), with `level` set to match:
   `{"operation": "add_stats_row", "table_path": "/Game/Characters/DT_HeroStats", "row_name": "Level01", "level": 1, "max_health": 100, "base_health": 100, "damage_multiplier": 1.0}`.
4. Verify: `query_stats_table` → `total` equals the number of rows, and each
   `row` holds what you sent (clamps!).
5. Link it from a config: section `config`, "Linking a stats table".

To rename a row: `add_stats_row` under the new name with the old row's
values (from `query_stats_table`), then `remove_stats_row` the old one.
There is no rename operation.
