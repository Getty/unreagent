# control_rig: errors and failure modes

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or an empty string; `rig_path` and `operation` on every call |
| `Unknown operation: '<op>'. Valid: …` | misspelt operation |
| `Failed to load Control Rig: <path>` | nothing loads at `rig_path` as a Control Rig Blueprint |
| `Asset is not a ControlRigBlueprint: <path> (is <class>)` | the asset is of another class |
| `No graph model found in Control Rig` | the rig has no graph model; every operation except `inspect`, the member-variable operations and `recompile` needs one |
| `Invalid blueprint or graph`, `Cannot obtain RigVM controller for graph` | the engine gave no controller for the graph |
| `Failed to …` | the engine refused the edit (see the operation) |
| `Batch: <ok> OK, <n> errors, <total> total` | at least one batch entry failed; the others were applied |
| `Operation returned null result`, `Batch operation returned null result` | internal; not expected |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| `list_structs` fails with a load error | it needs a rig that loads, like every operation | pass a valid `rig_path` |
| a batch `add_var_node` added a setter | `mode` was missing or not `get` | pass `"mode": "get"` |
| a batch `unlink_all` broke the wrong side | `direction` was missing or not `in` | pass `"direction": "in"` |
| a batch entry fails although the same call works alone | it used the single operation's parameter names | use the batch keys |
| a batch with `list_structs` is always an error | the batch counts list entries as failed | call the lists on their own |
| `Failed to link …` with no reason | the engine refused: wrong pin path, direction or type | `inspect` both nodes and compare pin names, `direction` and `cpp_type` |
| a node in `inspect` cannot be removed or linked | it is in a graph other than the default model | not editable with this tool |
| a change is gone after an editor restart | it was never saved | `asset` `save_asset` with the rig's path |

