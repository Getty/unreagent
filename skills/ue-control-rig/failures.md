# control_rig: errors and failure modes

**First move for any bare `Failed to …`:** `get_output_log` with
`filter: "Script Msg"` (or `categories: ["LogScript"]`). The RigVM controller
writes its reason there as `Script Msg: <package> : <reason>` (section
`ue-facts`). The tool itself never passes the reason on.

## Errors

| Error | Cause | Fix |
|---|---|---|
| `Missing required parameter: <name>` | a required parameter absent or empty; `rig_path` and `operation` on every call; `value` cannot be empty in the single `set_default` | send it |
| `Unknown operation: '<op>'. Valid: …` | misspelt operation (no aliases) | use a listed name |
| `Failed to load Control Rig: <path>` | nothing loads there as a Control Rig Blueprint | `asset_search`, use the object path |
| `Asset is not a ControlRigBlueprint: <path> (is <class>)` | another asset class (an IK Rig is not a Control Rig) | skill `ue-retarget` for IK Rigs |
| `No graph model found in Control Rig` | the rig has no graph model | not editable with this tool |
| `Invalid blueprint or graph`, `Cannot obtain RigVM controller for graph` | the engine gave no controller | reopen the editor; not fixable through the tool |
| `Failed to add node (struct: <path>)` | wrong struct path, or not a RigVM unit | `list_structs` with a `filter`, copy `path` |
| `Failed to add template (notation: <n>)` | notation not registered or misspelt | `list_templates`, copy the string exactly |
| `Cannot determine type for '<v>'. Pass cpp_type …` | `add_var_node` without `cpp_type` and no member variable of that name | `add_member_var` first, or pass `cpp_type` |
| `Failed to add variable node for '<v>' (type: <t>)` | the engine refused the variable / type | declare the member variable, omit `cpp_type` |
| `Failed to remove '<n>' (not found or protected)` | wrong node name, or a node in another graph | use `path` from `inspect` |
| `Failed to link <a> -> <b>` | wrong pin path, direction, type mismatch, or a cycle; reason in the log as `Cannot link …` | compare both pins' `direction` and `cpp_type` in `inspect` |
| `Failed to unlink <a> -> <b>` | that link does not exist | `inspect` `links[]` |
| `Failed to break links on <pin>` | wrong pin path | `inspect` |
| `Failed to set default on <pin>` | wrong pin path, or a value text the type cannot parse | copy the text form from a pin of the same type; try the sub-pin |
| `Failed to set expansion on <pin>` | wrong pin path or not a struct pin | — |
| `Failed to add member variable '<name>'` | name taken or invalid, or type unknown | another name; a type from the `cpp_type` list |
| `Failed to remove variable '<name>'` | no such variable | `inspect` the variable nodes / the rig in the editor |
| `Batch: <ok> OK, <n> errors, <total> total` | at least one batch entry failed; the others applied | `inspect`, repeat doubtful entries as single operations |
| `Operation returned null result`, `Batch operation returned null result` | internal; not expected | report |
| `Error: Task timed out after 300000ms …` | the bridge stopped waiting | `inspect` before you retry (skill `ue-tasks`) |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools missing, every call fails | editor not running or still loading | `unreagent-iterate` |
| `list_structs` fails with a load error | it needs a rig that loads | pass a valid `rig_path` |
| `add_node` of Forwards Solve "succeeded" but no new node | the event exists once; the engine returned the existing one | expected; use the existing node |
| a node has another name than you asked | the name was taken; the engine made it unique | use `node.path` from the result |
| a link you made earlier is gone | a later `link` into the same input, or from the same execute output, replaced it | re-link; insert into chains as in section `workflows` |
| a Set node does nothing at runtime | not on an event's execute chain | link it from the chain (section `workflows`, 4) |
| the default you set has no effect | the pin is linked; the link wins | `unlink_all` the input or drive the value through the link |
| a batch `add_var_node` added a setter | `mode` missing or not `get` | `"mode": "get"` |
| a batch `unlink_all` broke the wrong side | `direction` missing or not `in` | `"direction": "in"` |
| a batch entry fails though the same single call works | it used the single operation's parameter names | use the batch keys |
| a batch with `list_structs` is always an error | the batch counts list entries as failed | call the lists on their own |
| a node in `inspect` cannot be removed or linked | it is in a graph other than the default model | not editable with this tool |
| `recompile` succeeded but the rig misbehaves | `recompile` reports no compile result | `get_output_log`; preview in the editor |
| the AnimBP's Control Rig node shows no pin for the variable | the variable is private, or "Use Pin" is off on the node | `add_member_var` with `is_public: true`; skill `ue-anim-blueprint` |
| a change is gone after an editor restart | never saved | `asset` `save_asset` with the rig's path |
