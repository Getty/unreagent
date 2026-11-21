# game_framework: DataTable operations

The five DataTable operations of the `game_framework` tool, in full. They
work on any DataTable. None of them saves: `set_row` and `delete_row` mark
the table's package dirty, and `asset` `save_asset` with the table's path
writes it (skill `ue-assets`).

## table_path

The table's asset path, `/Game/Data/DT_Items`, or the object path that
`list_data_tables` returns, `/Game/Data/DT_Items.DT_Items`. The tool hands
it to the engine's loader unchecked. A path that does not load as a DataTable
returns `DataTable not found: <path>`. A table that has lost its row type
returns `DataTable has no row struct`.

`row_name` is the row's name as `inspect_data_table` lists it.

## list_data_tables

`folder_path` defaults to `/Game`; subfolders are included. The folder is
looked up in the Asset Registry and is not checked: a folder that does not
exist returns `count: 0`, not an error.

The result has `tables` and `count`. Each table has `path` (the object
path), `name`, and `row_struct_name` when the registry holds the table's row
type. The list is not paged.

## inspect_data_table

| Field | Holds |
|---|---|
| `table_name`, `table_path` | the asset's name, and the path as you passed it |
| `row_struct` | `name` and `path` of the row type |
| `columns` | one entry per column: `name`, `type` (the C++ type, such as `float`, `FString`, `FVector`, `TArray<FName>`), `is_editable`, `category` |
| `row_names`, `row_count` | every row name; not paged |

**Take column names from here.** A row type made in the editor (a user
defined struct) carries generated internal column names, longer than the
ones the editor displays; `set_row` and `row_data` use the names this
operation returns.

## get_row

Returns `row_name` and `row_data`, an object with one key per column:

| Column type | Value in `row_data` |
|---|---|
| number | JSON number |
| bool | JSON boolean |
| string, name, text | JSON string |
| enum | the enum value's name as a string |
| object or class reference | the path as a string, `None` when empty |
| soft reference | the path as a string, empty when unset |
| array | JSON array |
| map | JSON object, the keys in UE text form |
| `FVector`, `FRotator`, `FVector2D`, `FColor`, `FLinearColor` | JSON object with lower-case keys, such as `{"x": 0, "y": 0, "z": 0}` or `{"r": 1, "g": 1, "b": 1, "a": 1}` |
| `FTransform` | `{"location": {…}, "rotation": {…}, "scale": {…}}` |
| any other struct | one string in UE text form |

A missing row returns `Row '<name>' not found`.

## set_row

`row_data` is a JSON object of column name to value. When `row_name` exists,
only the columns in `row_data` change. When it does not, a new row is
created with the row type's defaults, the given columns are set, and the row
is added. The result has `row_name`, `row_data` (the whole row afterwards)
and `is_new_row`; the message says `Added` or `Updated` and how many fields
were written.

The tool has no separate "add" operation and no "must exist" switch: a
misspelt `row_name` creates a new row. Check `is_new_row`.

### Value forms

`set_row` does not accept everything `get_row` returns. What the code reads,
by column type:

| Column type | Pass | Not accepted |
|---|---|---|
| number | a JSON number, or a numeric string | — |
| bool | `true` or `false`, or the string `"true"` / `"false"` | — |
| string, name | a JSON string | — |
| text | a JSON string | a value the tool cannot read as a string: `FTextProperty '<col>' requires a string value` |
| enum | the value's name as a string | — |
| object, class or soft reference | the path as a string | — |
| struct | a JSON object with the struct's field names as keys, each value in the form of its own type; or one string in UE text form, `"(X=1.0,Y=2.0,Z=3.0)"` | a string that is not that struct's text form: `Failed to ImportText struct '<col>' from string`; a value that is neither an object nor readable as a string: `Struct property '<col>' requires a JSON object or string` |
| array, map, set | **one string in UE text form**, such as `"(Sword,Shield)"` for an array of names | a JSON array or object: `Cannot convert JSON to property '<col>' (type: <type>)` |

Whether a number or boolean is read as a string, for a text or struct
column, depends on the engine's JSON classes, which this skill has not
verified. Send each value in the JSON type the table names.

So an array or map read with `get_row` cannot be sent back as it came: turn
it into UE text form first. A string is handed to the engine's text import
for that column type; the exact text form the engine accepts for a given
type is the engine's, and this skill has not exercised it against an editor.
After a write, compare the returned `row_data` with what you meant.

Inside a struct object, a key that names no field of the struct is skipped
without a message. A field that fails stops the call with
`Failed to set '<col>': Field '<key>': <reason>`.

**A transform cannot be sent back the way `get_row` shows it.** The keys
`location` and `scale` are not field names of the engine's transform struct,
and `rotation` there is a quaternion, not pitch, yaw and roll. `set_row`
skips all of it and still reports the column as updated. Pass a transform as
one string in UE text form, and read the row back.

### Errors

| Error | Cause |
|---|---|
| `Missing required parameter: table_path` / `row_name` | absent or empty |
| `Missing required parameter: 'row_data' (must be a JSON object with column:value pairs)` | `row_data` is absent or not an object, such as a JSON string holding an object |
| `Property '<col>' not found in row struct '<struct>'` | not a column name; see `inspect_data_table` |
| `Failed to set '<col>': <reason>` | the value does not fit the column |

**An error can leave an existing row half written.** Columns are written
into the row one by one, in the order of `row_data`. When one fails, the
columns before it keep their new values in memory, the table is not marked
dirty, and the result is only the error. Run `get_row`, then repeat the call
with every column right. A new row is not affected: it is added only after
every column was set.

An empty `row_data` (`{}`) succeeds with "0 fields updated"; with a new
`row_name` it adds a row of defaults.

## delete_row

Removes the row and returns `deleted_row_name`. A missing row returns
`Row '<name>' not found`. There is no undo step in the tool; before you
delete, keep the `get_row` result so the row can be written back with
`set_row`.

## What these operations do not do

- create a DataTable, or change its row type;
- rename or reorder rows;
- import or export CSV or JSON;
- save.
