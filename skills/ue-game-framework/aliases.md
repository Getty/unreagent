# game_framework: operation aliases

The twelve operation names and their parameters are in the skill itself,
under "Operations".

## Aliases

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
