---
name: ue-metasounds
description: >
  Build and edit MetaSound Source graphs with metasound: nodes, connections,
  input defaults, graph inputs and outputs, and editor preview. Use before you
  create or change a MetaSound, or when a MetaSound edit does not take effect.
toolkit_tools: [metasound]
---

# UE MetaSounds: metasound

The UE LLM Toolkit plugin's **`metasound`** tool reads and edits the graph of
a MetaSound Source: it lists the node classes UE knows, adds and removes
nodes, connects pins, sets input defaults, adds graph inputs and outputs, and
plays a MetaSound in the editor. Through the plugin's MCP bridge it is listed
as `unreal_metasound`. It works only while the editor is running and the
plugin is reachable; `unreagent-iterate` covers the wait for the plugin.
SoundWaves, Sound Cues and audio components are a separate tool (skill
`ue-audio`).

**The tool exists only on UE 5.7 or a later 5.x.** The plugin checks for
major version 5 and minor version 7 or above. On any other engine the tool
is not registered, `unreal_metasound` is missing from the tool list, and the
editor's log says
`MetaSound tool skipped — requires UE 5.7+ (MetasoundFrontend API changes)`.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

| Operation | Required | Optional |
|---|---|---|
| `inspect` | `asset_path` | — |
| `list_nodes` | — | `name_filter`, `category_filter`, `max_results` |
| `get_graph` | `asset_path` | — |
| `create` | `package_path`, `name` | `output_format` (ignored) |
| `add_node` | `asset_path`, `node_class_name` | `node_namespace` |
| `remove_node` | `asset_path`, `node_id` | — |
| `connect` | `asset_path`, `from_node_id`, `from_pin`, `to_node_id`, `to_pin` | — |
| `disconnect` | `asset_path`, `from_node_id`, `from_pin`, `to_node_id`, `to_pin` | — |
| `set_input_default` | `asset_path`, `node_id`, `input_name`, `value` | `data_type` |
| `add_graph_input` | `asset_path`, `input_name`, `data_type` | `default_value` |
| `add_graph_output` | `asset_path`, `output_name`, `data_type` | — |
| `preview` | `asset_path` | — |
| `stop_preview` | — | — |

`operation` is lowercased. `info` means `inspect`, `graph` means `get_graph`
and `create_metasound` means `create`. An unknown operation returns
`Unknown operation: '<op>'. Valid: inspect, list_nodes, get_graph, create, add_node, remove_node, connect, disconnect, set_input_default, add_graph_input, add_graph_output, preview, stop_preview`.

Every parameter but `max_results` is a string. Through the bridge,
`max_results` is listed as a string too, because the tool declares it
`integer`, a type the bridge does not map: send a JSON number.

## What the tool cannot do

- **No layout.** Nodes get no position; the tool does not touch the graph the
  MetaSound editor draws. How new nodes appear there has not been verified.
- **No interface or output-format choice.** `output_format` is read and then
  ignored.
- **No operation for removing graph inputs or outputs**, and none for
  renaming them. What `remove_node` does to a graph input's or output's node
  has not been verified.
- **No MetaSound Patches.** Every operation that loads an asset expects a
  MetaSound Source.
- **No reading of input defaults.** `get_graph` lists pins without their
  default values, despite the tool's description.
- **No undo, and no check that the graph still builds.** Check the result
  with `get_graph` and `preview`.

## Saving

**No operation saves.** Every write marks the MetaSound's package dirty and
leaves the change in the editor's memory, where an editor stop or crash loses
it. Save with the `asset` tool (skill `ue-assets`): `save_asset` with the
MetaSound's path, or `save_all`.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. With this tool, that
  `data` object also carries its own `success: true` and `message`. Skill
  `ue-tasks` covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the graph back with `get_graph` before you retry: a repeated
  `add_node` adds a second node (skill `ue-tasks`).

## Paths and ids

- `asset_path` is loaded as given: the asset path
  (`/Game/Audio/MS_Engine`) and the object path both work. The tool runs no
  path check. A path that does not load as a MetaSound Source gives
  `Failed to load asset: <path>` or `Not a MetaSound source: <path> (is <class>)`
  from `inspect` and `get_graph`, and `Failed to load MetaSound: <path>` from
  every other operation.
- **Node ids are GUID strings**, as `get_graph` and `add_node` return them.
  A string that does not parse gives `Invalid GUID: <id>`,
  `Invalid node GUID: <id>`, `Invalid from-node GUID: <id>` or
  `Invalid to-node GUID: <id>`.
- **Pin names** are the `name` of an entry in a node's `inputs[]` or
  `outputs[]` in `get_graph`. Copy them exactly.

To find MetaSounds, use `list_sounds` with `class_filter: "MetaSoundSource"`
(skill `ue-audio`). The `audio` tool's `inspect` probably reports a MetaSound
as a SoundWave; use this tool's `inspect`.

## inspect

The result has `type: "MetaSoundSource"`, `asset_path` (the object path),
`asset_name`, `duration`, `node_count`, `edge_count`, `graph_inputs[]` and
`graph_outputs[]` (each with `name` and `type`), and `interfaces[]` (each
with `name`, `major_version` and `minor_version`). The message is
`MetaSound: <name> (<n> nodes, <e> edges, <i> inputs, <o> outputs)`.

The tool's description promises `output_format`; it is not returned. The
`interfaces` list is where UE records what the MetaSound implements, probably
including its output format.

## list_nodes

Searches the node classes registered with UE's MetaSound system.

| Parameter | Default | Meaning |
|---|---|---|
| `name_filter` | — | case-insensitive substring of `full_name` |
| `category_filter` | — | case-insensitive substring of the **namespace**, not of any category shown in the MetaSound editor |
| `max_results` | `50` | how many to return. `0` or less also means 50. There is no upper limit |

Each entry has `class_name`, `namespace`, `full_name` and `version`
(`"<major>.<minor>"`). The result has `nodes[]`, `count` (returned) and
`total_matches` (all that matched). The message is
`Found <total> registered node classes (<n> shown, max <max>)`. When
`total_matches` is above `count`, narrow the filters or raise `max_results`.

## get_graph

The whole graph. The message is
`MetaSound graph: <n> nodes, <e> edges, <i> inputs, <o> outputs`.

- `nodes[]`: `node_id`, `class_name` and `namespace` (both `unknown` when the
  class cannot be resolved), `inputs[]` and `outputs[]`. Each pin has `name`,
  `type` and `vertex_id`.
- `edges[]`: `from_node_id`, `from_vertex_id`, `to_node_id` and
  `to_vertex_id`. Edges name pins by `vertex_id`; match them against the
  nodes' pins to read a connection.
- `graph_inputs[]` and `graph_outputs[]`: `name` and `type`.

Graph inputs and outputs have nodes of their own in `nodes[]`; their ids are
what `add_graph_input` and `add_graph_output` return.

## create

Creates a MetaSound Source at `<package_path>/<name>`, such as
`/Game/Audio/MS_Engine`, and sets up its document.

- **`output_format` is ignored.** Which interfaces, inputs and outputs a new
  MetaSound starts with is up to the engine. Read them back with `inspect`
  (`interfaces`, `graph_outputs`) and `get_graph` before you connect anything.
- **The tool does not check whether an asset already exists** at that path,
  and what the engine does then has not been verified. A `Failed to load …`
  from another tool does not mean the name is free. Check with the toolkit's
  `asset_search` tool first: `path_filter` set to the folder, `name_pattern`
  set to the name. The name is taken only if an entry has exactly that `name`
  and that `package_path`, which `asset_search` writes without a trailing
  slash (skill `ue-assets`).
- Errors: `Failed to create package: <path>`,
  `Failed to create UMetaSoundSource object`.

The result has `asset_path` (the object path). The message is
`Created MetaSound: <path>`.

## add_node

Adds one node of a registered class.

- **`node_class_name`** is the `class_name` from `list_nodes`, and
  **`node_namespace`** its `namespace`. Pass both; with no namespace the tool
  looks for the name in an empty namespace.
- **The tool always asks for major version 1** of the class and passes no
  variant. A class that `list_nodes` shows with another major version may fail
  to add. `full_name` is UE's full class name: if it holds a part besides the
  namespace and the class name, that is probably a variant, which this tool
  cannot pass, and the class then probably fails to add.
- Failure: `Failed to add node: <namespace>::<class_name>`.

The result has `node_id`, the new node's GUID. The message is
`Added node: <namespace>::<class_name>`. Read the node's pins with `get_graph`.

## remove_node

Removes the node with `node_id`, together with its connections. The message is
`Removed node: <id>`. A node that is not in the graph fails with
`Failed to remove node: <id>`. The connections are removed before the node, so
in a case where the node itself cannot be removed, they may already be gone.
What this does to the node of a graph input or output, and to the input or
output itself, has not been verified; check `inspect` afterwards.

## connect and disconnect

Both take the output side (`from_node_id`, `from_pin`) and the input side
(`to_node_id`, `to_pin`). The pins are looked up first:
`Output pin not found: <pin> on node <id>`,
`Input pin not found: <pin> on node <id>`.

- **Compare the pin types first.** Before `connect`, read both pins' `type`
  in `get_graph` and connect only pins whose types match. The tool does not
  compare them.
- **`connect` does not check that the connection was made.** A type mismatch,
  an input that is already connected or a cycle is left to the engine, and the
  call reports success either way. After every `connect`, read `edges[]` with
  `get_graph` and confirm the new edge is there. A present edge shows that it
  was recorded, not that the connection is valid: whether the engine records
  an edge between mismatched types has not been verified.
- The `connect` result has `from_node`, `from_pin`, `to_node` and `to_pin` as
  you passed them. The message is `Connected nodes`.
- `disconnect` fails with `Failed to remove edge - edge may not exist` when
  there is no such edge. Its message is `Disconnected nodes`.

## set_input_default

Sets the default value of one input pin on one node.

| Parameter | Meaning |
|---|---|
| `node_id` | the node |
| `input_name` | the input pin |
| `value` | the value **as a string**, such as `"440"`. An empty string counts as missing |
| `data_type` | how to read `value`: `float` (the default), `int32`, `bool`, or anything else for a string |

- **`data_type` is not checked against the pin.** It only decides how `value`
  is converted. `float` reads a number and turns anything else into 0;
  `int32` reads an integer; `bool` is true only for `true` (any case) or `1`.
  **`int` is not `int32`**: it stores the text as a string. Pick the type that
  matches the pin's `type` in `get_graph`.
- A JSON number or boolean in `value` is converted to its text: the tool
  reads `value` with the engine's `FJsonObject::TryGetStringField`, which
  turns `440` into `"440"`, `0.5` into `"0.5"` and `true` into `"true"`
  (`FJsonValueNumber::TryGetString` and `FJsonValueBoolean::TryGetString`,
  `Engine/Source/Runtime/Json/Private/Dom/JsonValue.cpp`; read in the UE
  5.8.4 source, not run, while the toolkit targets UE 5.6 and 5.7).
  `data_type` then applies to that text as above. `null`, an array or an
  object counts as missing. Send a string, so the text is yours.
- Errors: `Input not found: <name> on node <id>`, and
  `Failed to set default on input '<name>'` when the engine refuses the value.

The message is `Set default on input '<name>' to '<value>'`.

## add_graph_input and add_graph_output

Add an input or an output to the MetaSound itself, with a node for it in the
graph.

- **`data_type` is used as the MetaSound type name as given.** Copy a `type`
  that `get_graph` or `inspect` reports, such as the type of a pin you want to
  connect it to. What the engine does with a type name it does not know has
  not been verified.
- `add_graph_input` takes an optional `default_value`, a string converted
  like `set_input_default`'s `value`: by `data_type` lowercased, so `float`,
  `int32` and `bool` are converted and anything else is stored as a string.
- **The results name the id differently from the description.**
  `add_graph_input` returns `input_node_id` and `add_graph_output` returns
  `output_node_id`, not `node_id`. Pass that id as `from_node_id` (input) or
  `to_node_id` (output) in `connect`; the pin names are in `get_graph`.
- Errors: `Failed to add graph input: <name>`,
  `Failed to add graph output: <name>`. The engine decides, for example, what
  happens to a name that is already taken.

The messages are `Added graph input: <name> (<type>)` and
`Added graph output: <name> (<type>)`.

## preview and stop_preview

`preview` plays the MetaSound in the editor, without spatialization, through a
temporary audio component. A preview already playing is stopped first, so only
one plays at a time. Whether the preview includes edits made through this
tool since the MetaSound was loaded has not been verified.

- **`status: "playing"` is set whatever happens.** The tool does not check
  that sound actually started. A graph that does not build may play silence.
- Errors: `Failed to load MetaSound: <path>`, `Editor not available`.
- The result has `asset_path` (as passed) and `status`. The message is
  `Playing MetaSound: <name>`.

`stop_preview` takes no parameters. Its message is `Preview stopped`, or
`No preview was playing`; both are successes. Call it when you are done with a
`preview`: whether a preview ends on its own has not been verified.

## A typical sequence

1. `create` `{"package_path": "/Game/Audio", "name": "MS_Tone"}`, then
   `get_graph` with the returned `asset_path`: note the existing nodes, such
   as the output node, and their pins.
2. `list_nodes` `{"name_filter": "Sine"}`: take `class_name` and `namespace`
   of an entry whose `full_name` holds no variant (see `add_node`).
3. `add_node` with both, and keep the returned `node_id`.
4. `get_graph` again: read the new node's pin names and types.
5. `set_input_default` for an input, with `data_type` matching the pin.
6. Check that the two pins' `type` match, `connect` the node's output to the
   output node's input, then `get_graph` and confirm the edge.
7. `preview`, then `stop_preview`.
8. Save: `asset save_asset` with the MetaSound's path.

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

## Background in the plugin

The plugin ships no context notes for MetaSounds: `unreal_get_ue_context`
(skill `ue-context`) has no MetaSound category.

## See also

- `ue-audio`: `list_sounds` to find MetaSounds, and audio components to play
  them in the level.
- `ue-assets`: `asset_search`, `save_asset` and `save_all`.
- `ue-tasks`: the task envelope and timeouts.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
