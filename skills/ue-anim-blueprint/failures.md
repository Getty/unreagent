# anim_blueprint_modify: when a call fails

Paths, the target skeleton, node IDs, saving and how a result reaches you
are in the skill itself. Each operation's own errors are in the section
that covers it.

## Errors common to every operation

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | `blueprint_path` or `operation` is absent or an empty string |
| `Unknown operation: '<op>'. Valid: …` | misspelt operation |
| `Cannot access engine or script Blueprints` | `/Engine/` or `/Script/` path |
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
| `create_state_machine` returned a name, but `get_state_machine` cannot find it | the state machine's real name can differ from the one you asked for | read the names with `get_info` and use those |
| a node ID that worked before is not found | `blueprint_query` replaced it, or the node is in another graph | `get_transition_nodes` or `inspect_node_pins` to read the current IDs; pass the same graph parameters as when you created it |
| `batch` or `setup_transition_conditions` "succeeded", but nothing changed | both report failure as success | read `data.success`, `failure_count` and `results` |
| `setup_transition_conditions` run twice gives doubled conditions | it adds nodes on every run | delete the extra nodes with `delete_condition_node`, or build conditions one by one |
| `validate_blueprint` says `0 error(s)`, but `is_valid` is false | the compiler's errors are not counted, only the tool's own checks | read `status`; get the messages with `blueprint_modify compile` |
