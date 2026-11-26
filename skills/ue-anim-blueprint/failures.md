# anim_blueprint_modify: when a call fails

Paths, the target skeleton, node IDs and saving are in the skill itself; how
a result reaches you is in section `verify`. Each operation's own errors are
in the section that covers it.

The operations' own checks treat a parameter that is present but empty as
missing. A parameter the tool cannot read as the type it expects falls back
to its default; send numbers and booleans as JSON numbers and booleans.

For a parameter the tool does not declare, the plugin's registry appends a
warning that starts `WARNING: Unknown parameter` to a call it runs directly.
Calls the bridge queues, the default, get no such warning (skill `ue-tasks`),
so a misspelt parameter is silently ignored. The tool still reads
`asset_path`, `path` and `use_attached_parent` (section `anim-nodes`)
although it does not declare them.

## Errors common to every operation

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | `blueprint_path` or `operation` is absent or an empty string |
| `Unknown operation: '<op>'. Valid: …` | misspelt operation |
| `Cannot access engine or script Blueprints` | `/Engine/` or `/Script/` path |
| `Blueprint path exceeds maximum length of 512 characters` | path too long |
| `Blueprint path cannot contain path traversal sequences` | `..` in the path |
| `Blueprint path contains invalid character: '<c>'` | one of `` <>|&;`$(){}[]!*?~ `` in the path |
| `Failed to load Animation Blueprint: <path>` | nothing loads at that path as an AnimBP; search before you conclude it is missing |
| `Asset is not an Animation Blueprint: <path>` | the path loads, but not as an AnimBP |
| `AnimBlueprint has no target skeleton` | the AnimBP has no skeleton; see "The target skeleton" |
| `State machine '<n>' not found. Available: …` | wrong name; the list shows the names the tool sees |
| `<x> parameter required`, `<a>, <b>, and <c> parameters required` | the operation's own required parameters; each section lists them |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a write succeeded, but the change is gone after a restart | the compile failed, so nothing was saved | `validate_blueprint`; fix the graph, or save with `asset save_asset` |
| `create_state_machine` returned a name, but `get_state_machine` cannot find it | the name you pass is not applied; the state machine has a generated name | read the names with `get_info` and use those; the `node_id` still works with `connect_anim_nodes` |
| the wrong transition fires when two can | the smaller `priority` wins, not the larger (the tool's own text says the opposite) | `set_transition_priority` with a smaller number on the one that must win |
| the character stays in its entry state, or slides in idle | the variables the conditions and bindings read are never written | build the EventGraph update (section `locomotion`, step 2); log the values with `blueprint_modify add_debug_print` |
| a montage plays on the character but nothing shows | no slot node for the montage's slot in the AnimGraph, or it is not in the chain to the output | `add_anim_node` `slot` with the montage's slot name, wired state machine → slot → output (section `locomotion`) |
| `Skeleton mismatch: …` on `set_state_animation` or `add_anim_node` | the asset uses another skeleton than the AnimBP | pick an asset on the AnimBP's skeleton (`find_animations`), or retarget it (skill `ue-retarget`) |
| `Variable '<v>' not found in AnimBlueprint` | the variable does not exist on the AnimBP's class yet | `blueprint_modify add_variable` (skill `ue-blueprints`), then repeat |
| a comparison node vanished after `connect_condition_nodes` | a boolean source replaced or deleted it | `get_transition_nodes`; build variable conditions with `add_comparison_chain` (section `transitions`) |
| `set_state_animation` with `montage` fails | a state cannot play a montage | a slot node in the AnimGraph; montages play from gameplay code (skill `ue-anim-montage`) |
| a node ID that worked before is not found | `blueprint_query` replaced it, or the node is in another graph | `get_transition_nodes` or `inspect_node_pins` to read the current IDs; pass the same graph parameters as when you created it |
| `batch` or `setup_transition_conditions` "succeeded", but nothing changed | both report failure as success | read `data.success`, `failure_count` and `results` |
| `setup_transition_conditions` run twice gives doubled conditions | it adds nodes on every run | delete the extra nodes with `delete_condition_node`, or build conditions one by one |
| `validate_blueprint` says `0 error(s)`, but `is_valid` is false | the compiler's errors are not counted, only the tool's own checks | read `status`; get the messages with `blueprint_modify compile` |
