# metasound: errors and failure modes

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or an empty string |
| `Failed to load asset: <path>` | `inspect` or `get_graph`: nothing at that path |
| `Not a MetaSound source: <path> (is <class>)` | the asset is something else, such as a MetaSound Patch |
| `Failed to load MetaSound: <path>` | any other operation: nothing at that path, or not a MetaSound Source |
| `Invalid GUID: <id>`, `Invalid node GUID: <id>`, `Invalid from-node GUID: <id>`, `Invalid to-node GUID: <id>` | a node id that is not a GUID string |
| `Failed to add node: <namespace>::<class>` | no class with that full name at major version 1. Usually the missing variant: pass `<name>.<variant>` (section `nodes`). The editor log has the engine's reason under `LogMetaSound` |
| `Failed to remove node: <id>` | no node with that id |
| `Output pin not found: …` / `Input pin not found: …` / `Input not found: …` | a pin name the node does not have; copy it from `get_graph` |
| `Failed to remove edge - edge may not exist` | `disconnect`: no edge from that output into that input |
| `Failed to set default on input '<name>'` | the engine did not find the node or the input |
| `Failed to add graph input: <name>` / `Failed to add graph output: <name>` | an unregistered `data_type`, among others; see `LogMetaSound` |
| `Editor not available` | `preview` without an editor |
| `Unknown operation: '<op>'. Valid: …` | a misspelt operation |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `unreal_metasound` is not in the tool list | the engine is not UE 5.7 or a later 5.x, or the plugin is not loaded; the log says `MetaSound tool skipped — requires UE 5.7+ (MetasoundFrontend API changes)` | on 5.7+, wait for the plugin (`unreagent-iterate`); otherwise MetaSounds can only be listed and placed (skill `ue-audio`) |
| the editor crashed right after `connect`, `add_graph_input` or `add_graph_output` | most likely an engine assertion (section `editing`, the table); unsaved edits are gone | do not repeat the call. Re-read the graph after the restart; for `connect`, check the input is free and the types match; for graph members, use the editor or the Builder API path (section `workflows`), and on 5.8.x never call `add_graph_input` / `add_graph_output` again |
| `add_node` fails for a class `list_nodes` lists | its `full_name` has a variant the tool does not pass, or it has no major version 1 | `node_class_name` `<name>.<variant>` (section `nodes`); if it still fails, tell the user |
| `get_graph` shows `Sine` where you added `Sine.Audio` | the tool does not print variants | expected |
| `connect` succeeded but `get_graph` shows no new edge | not expected from the source: the engine adds the edge or asserts | read `LogMetaSound`, re-read the graph, tell the user |
| a graph input's id is missing from the result | the field is `input_node_id` / `output_node_id` | read that field |
| `add_graph_input` returned the id of an older node (UE 5.7 only, unverified; on 5.8.x the call crashes before this check) | the name was taken: the engine hands back the existing member | pick a new name; `inspect` lists the members |
| an input default has no effect | `data_type` did not match the pin, such as `int` instead of `int32` | set it again with the conversion that matches the pin's `type` |
| the new MetaSound has `UE.Source.OnPlay`, `UE.Source.OneShot.OnFinished` and a Mono output you did not ask for | the default interfaces of a MetaSound Source; `output_format` is ignored | work with them (section `nodes`); Stereo or no OneShot is editor or Builder API work |
| `preview` plays and never stops | a OneShot source whose `OnFinished` is not triggered plays until stopped | `stop_preview`; for a sound that should end, the graph must trigger `OnFinished` |
| `preview` says playing but nothing is heard | the graph does not build, plays silence, or the preview does not include the latest edits (not verified) | `get_graph`: is the output node's input connected? Save, `preview` again; then tell the user |
| `remove_node` on an input's node, and `inspect` still lists the input | the engine removed the node, not the member (UE 5.8.4 source) | tell the user; the member must be removed in the editor |
| changes are gone after an editor restart | never saved, or the editor crashed | `asset save_asset` after editing, `save_all` before risky calls |
