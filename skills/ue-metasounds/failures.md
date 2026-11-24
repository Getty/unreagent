# metasound: errors and failure modes

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or an empty string |
| `Failed to load asset: <path>` | `inspect` or `get_graph`: nothing at that path |
| `Not a MetaSound source: <path> (is <class>)` | the asset is something else, such as a MetaSound Patch |
| `Failed to load MetaSound: <path>` | any other operation: nothing at that path, or not a MetaSound Source |
| `Invalid GUID: <id>` and the like | a node id that is not a GUID string |
| `Failed to add node: <namespace>::<class>` | no such class, a missing namespace, no major version 1, or the class needs a variant |
| `Output pin not found: …` / `Input pin not found: …` / `Input not found: …` | a pin name that the node does not have |
| `Failed to remove edge - edge may not exist` | `disconnect` on pins that are not connected |
| `Failed to set default on input '<name>'` | the engine refused the default |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `unreal_metasound` is not in the tool list | the engine is not UE 5.7 or a later 5.x, or the plugin is not loaded | on UE 5.7 or a later 5.x, wait for the plugin (`unreagent-iterate`); otherwise this tool is not available |
| `connect` succeeded but `get_graph` shows no new edge | the tool does not check what the engine did with the edge; most likely it refused the connection | compare both pins' `type` in `get_graph` before you connect; the input may already be connected |
| a graph input's id is missing from the result | the field is `input_node_id` or `output_node_id` | read that field |
| an input default has no effect | `data_type` did not match the pin, such as `int` instead of `int32` | set it again with the pin's type |
| a new MetaSound has outputs you did not ask for, or not the format you asked for | `output_format` is ignored; the engine set up the document | work with what `inspect` reports, or tell the user |
| `add_node` fails for a class `list_nodes` lists | the class has no major version 1, or needs a variant | tell the user; this tool cannot add it |
| `preview` says playing but nothing is heard | the graph does not build, plays silence, or the preview does not include the latest edits | `get_graph`: check that the output node is connected; if it is, tell the user |
| changes are gone after an editor restart | never saved | `asset save_asset` after editing |
