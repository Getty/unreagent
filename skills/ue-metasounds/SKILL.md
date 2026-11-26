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
SoundWaves, Sound Cues, audio components, and whether to use a MetaSound at
all, are skill `ue-audio` (section `choosing`).

**The tool exists only on UE 5.7 or a later 5.x.** On any other engine it is
not registered, `unreal_metasound` is missing from the tool list, and the
editor's log says
`MetaSound tool skipped — requires UE 5.7+ (MetasoundFrontend API changes)`.

This skill was written from the toolkit's C++ source (commit c1b6709) and,
where marked, the UE 5.8.4 engine source (read, not run; UE 5.7's was not
checked). It has not been exercised against a running editor.

## Sections — load the one you need

| Section | Holds | Load it |
|---|---|---|
| `workflows` | step lists with the exact calls: a tone built node by node, a parameter gameplay sets, changing an existing graph, the Builder API path through Python, verification | **before you build or change a graph** |
| `nodes` | how class names and variants work, the `add_node` workaround for variant classes, pins of common nodes, data type names, what a new Source contains (OnPlay, OnFinished, the Mono output) | before your first `add_node` or `connect`, and when `add_node` fails |
| `editing` | every write operation in full, and **the engine assertions that crash the editor** | before `connect`, `add_graph_input`, `add_graph_output` or `remove_node`, and before any write you have not made before |
| `reading` | `inspect`, `list_nodes` and `get_graph` in full, and how to read a connection | before you rely on a result field |
| `failures` | the error table and the failure table (what you see, what it means, what to do) | when a call returns an error, an edit does not take effect, or the editor crashed |

| Operation | Required | Optional |
|---|---|---|
| `inspect` (`info`) | `asset_path` | — |
| `list_nodes` | — | `name_filter`, `category_filter`, `max_results` |
| `get_graph` (`graph`) | `asset_path` | — |
| `create` (`create_metasound`) | `package_path`, `name` | `output_format` (ignored) |
| `add_node` | `asset_path`, `node_class_name` | `node_namespace` |
| `remove_node` | `asset_path`, `node_id` | — |
| `connect` | `asset_path`, `from_node_id`, `from_pin`, `to_node_id`, `to_pin` | — |
| `disconnect` | `asset_path`, `from_node_id`, `from_pin`, `to_node_id`, `to_pin` | — |
| `set_input_default` | `asset_path`, `node_id`, `input_name`, `value` | `data_type` |
| `add_graph_input` | `asset_path`, `input_name`, `data_type` | `default_value` |
| `add_graph_output` | `asset_path`, `output_name`, `data_type` | — |
| `preview` | `asset_path` | — |
| `stop_preview` | — | — |

`operation` is lowercased; the aliases are in brackets. An unknown operation
returns `Unknown operation: '<op>'. Valid: inspect, list_nodes, get_graph, create, add_node, remove_node, connect, disconnect, set_input_default, add_graph_input, add_graph_output, preview, stop_preview`.
Every parameter but `max_results` is a string. The bridge lists
`max_results` as a string too (the tool declares `integer`, a type the bridge
does not map): send a JSON number.

## Must not get wrong

- **Never call `add_graph_input` / `add_graph_output` on UE 5.8.x**: the
  engine asserts on every such call and the editor always crashes (UE 5.8.4
  source; section `editing`). Graph members go through the user or the
  Builder API (section `workflows`). On 5.7 the check is unverified: only
  with the user's consent, after `asset save_all`.
- **`connect` can crash the editor** into an input that is already connected
  or from a pin of another type. **Save before every graph edit**
  (`asset save_all`) and check pins with `get_graph` before every `connect`.
- **`add_node` passes no variant**, and most audio nodes have one
  (`UE.Sine.Audio`). Pass `node_namespace` `UE` and `node_class_name`
  `Sine.Audio`: `full_name` minus the namespace (section `nodes`; derived from
  engine source, not run).
- **A new Source is a one-shot Mono source**: graph input `UE.Source.OnPlay`,
  outputs `UE.OutputFormat.Mono.Audio:0` (connect your audio here) and
  `UE.Source.OneShot.OnFinished`. Until something triggers `OnFinished` it
  plays forever: always `stop_preview`.
- **Nothing is checked for you**: `connect` reports success without looking,
  `data_type` is not compared with the pin, `status: "playing"` is set
  whatever happens. Read back with `get_graph` after every write.
- **No operation saves, and there is no undo.**
- **Ids**: node ids are GUID strings from `get_graph` / `add_node`;
  `add_graph_input` and `add_graph_output` return `input_node_id` and
  `output_node_id`, not `node_id`. Pin names are the `name` of a node's pin
  in `get_graph`, copied exactly (they hold spaces).

## What the tool cannot do

No layout (nodes get no position); no output format, interface or
constructor-input choice; no removing or renaming graph inputs or outputs;
no MetaSound Patches (every operation expects a Source); no reading of input
defaults (`get_graph` lists pins without defaults); no check that the graph
builds. Section `workflows` names the routes for these: the user in the
MetaSound editor, or the Builder API through Python.

## Saving

**No operation saves.** Every write marks the MetaSound's package dirty and
leaves the change in the editor's memory, where an editor stop or crash
loses it. Save with the `asset` tool (skill `ue-assets`): `save_asset` with
the MetaSound's path, or `save_all`.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …) and the tool's own fields sit
  under its `data` key, which also carries its own `success: true` and
  `message` (skill `ue-tasks`).
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the graph back with `get_graph` before you retry: a repeated
  `add_node` adds a second node.

## Paths

`asset_path` is loaded as given: the asset path (`/Game/Audio/MS_Engine`) and
the object path both work, with no path check. A path that is not a MetaSound
Source gives `Failed to load asset: <path>` or
`Not a MetaSound source: <path> (is <class>)` from `inspect` and `get_graph`,
and `Failed to load MetaSound: <path>` from every other operation.

To find MetaSounds, use `audio` `list_sounds` with
`class_filter: "MetaSoundSource"` (skill `ue-audio`). The `audio` tool's
`inspect` reports a MetaSound as a SoundWave; use this tool's `inspect`.

## A typical sequence

Section `workflows` has it with every call: `create` → `inspect` and
`get_graph` (find the output node) → `list_nodes` → `save_all` → `add_node`
with the variant in the name → `get_graph` (pins) → `set_input_default` →
check both pins, `connect` → `get_graph` (the edge) → `save_asset` →
`preview`, `stop_preview` → `get_output_log` for `LogMetaSound`.

## Background in the plugin

The plugin ships no context notes for MetaSounds: `unreal_get_ue_context`
(skill `ue-context`) has no MetaSound category.

## See also

- `ue-audio`: `list_sounds` to find MetaSounds, audio components and ambient
  sounds to play them in the level, MetaSound vs. Sound Cue.
- `ue-blueprints`: the event graph that plays the component and sets its
  parameters (Set Float Parameter).
- `ue-assets`: `asset_search`, `save_asset` and `save_all`.
- `ue-pie-testing`: `get_output_log` and PIE.
- `ue-python`: `execute_script`, for the Builder API path.
- `ue-tasks`: the task envelope and timeouts.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
