# UE Niagara: when a call fails

`set_parameter` is in the skill itself, `spawn_system` in section `spawn`.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | `operation`, `asset_path` or `parameter_name` is absent or an empty string |
| `Failed to load Niagara system: <path>` | nothing at that path, or not a Niagara system |
| `Must specify either emitter_name or emitter_index` | `get_emitter_info` without either |
| `Emitter '<name>' not found. Available: …` | no emitter of that name |
| `Emitter index <n> out of range (0-<last>)` | the index is past the last emitter |
| `Parameter '<name>' not found on system '<path>'. Available: …` | no user parameter of that name |
| `Parameter '<name>' is <type> -- 'value' must be …` | `value` has the wrong JSON type |
| `Unsupported parameter type '<type>' for parameter '<name>'` | the parameter's type cannot be set by this tool |
| `No active world. Open a level in the editor first` | `spawn_system` with no level open |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a vector parameter has a wrong component after `set_parameter` | a component was left out of `value` | send every component |
| `set_parameter` succeeded but the effect in the level looks the same | not verified whether placed effects pick up a new default at once | save the system; check again after a reload |
| a new default is gone after an editor restart | the system was never saved | `asset save_asset` after `set_parameter` |
| `spawn_system` renamed an actor you did not expect | `label` went to the actor that owns the component, probably World Settings | tell the user, with the old and new label; how to rename that actor back has not been verified. Spawn without `label` from then on |
| the spawned effect is gone | `auto_destroy` removed it, or it was never saved with the level | see section `spawn`; tell the user if it must stay |
