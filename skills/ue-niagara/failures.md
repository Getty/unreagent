# UE Niagara: when a call fails

Details of each operation are in sections `reading`, `set-parameter` and
`spawn`.

## Errors

| Error | Cause | Fix |
|---|---|---|
| `Missing required parameter: <name>` | `operation`, `asset_path` or `parameter_name` is absent or an empty string | send it |
| `Failed to load Niagara system: <path>` | nothing at that path, or not a Niagara system (an emitter asset, say) | `list` or `asset_search` with `class_filter: "NiagaraSystem"` |
| `Must specify either emitter_name or emitter_index` | `get_emitter_info` without either | pass a name from `inspect` |
| `Emitter '<name>' not found. Available: …` | no emitter of that name | use a name from the list |
| `Emitter index <n> out of range (0-<last>)` | the index is past the last emitter | 0 to `emitter_count - 1` |
| `Parameter '<name>' not found on system '<path>'. Available: …` | no user parameter of that name; only `User.` parameters count | use a name from the list; anything else is authoring (section `limits`) |
| `Parameter '<name>' is <type> -- 'value' must be …` | `value` has the wrong JSON kind | the form from section `set-parameter` |
| `Unsupported parameter type '<type>' for parameter '<name>'` | the parameter's type is none of the seven settable ones | tell the user; section `limits` |
| `Editor not available. …` / `No active world. Open a level in the editor first` | `spawn_system` with no editor world or no level open | open a level (skill `ue-levels`) |
| `Failed to spawn Niagara system '<path>' in the level` | the engine created nothing: pre-culled at that location, or the process cannot render | spawn at another location, nearer the camera; check `get_output_log` |
| `Niagara component spawned but has no owning actor` | the world had no World Settings to own it | tell the user |
| `Unknown operation: '<op>'. Valid: …` | a misspelt operation | use a name from the operation table |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a vector parameter has a 0 component after `set_parameter` | a component was missing or unreadable | send every component |
| a colour parameter made the effect invisible | `a` was left out and read as 0 | resend with `"a": 1` |
| a bool is `false` although you meant `true` | a string or typo was sent; anything not recognised is `false` | send JSON `true` |
| `set_parameter` succeeded, the effect in the level looks the same | the placed component overrides the parameter, or it has not picked up the new default (not verified) | save, reopen the level, capture again; per-component values: section `limits` |
| a new default is gone after an editor restart | the system was never saved | `asset save_asset` after `set_parameter` |
| the system changed for every level that uses it | `set_parameter` writes the asset's default | put the old value back if that was not wanted; per-component values: section `limits` |
| World Settings has a new label | `spawn_system` was called with `label` | tell the user, with the old and new label; spawn without `label` from then on |
| the spawned effect is gone | it finished and `auto_destroy` removed it, or the level was reopened: `spawn_system` effects are transient | for an effect that stays, section `workflows`, 4 |
| a placed `NiagaraActor` shows nothing after `set_property` on `Asset` | the component was not restarted with the new system (not verified) | `save_all`, reopen the level, capture again; else Route B in section `workflows` |
| a capture shows no particles | the viewport may not animate, the effect finished, or the camera points elsewhere | capture again with an explicit camera; check during PIE (section `workflows`, 5) |
| `delete_actors` on the `actor_name` of a spawn result | that is World Settings | do not delete it; section `limits` |
