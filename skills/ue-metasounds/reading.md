# metasound: inspect, list_nodes, get_graph

The three read operations in full (toolkit commit c1b6709,
`MetaSoundEditor.cpp`). They change nothing.

## inspect

Takes `asset_path`. Loads any asset, then requires a MetaSound Source:
`Failed to load asset: <path>`, or
`Not a MetaSound source: <path> (is <class>)` (a MetaSound Patch, for one).

The result has `type: "MetaSoundSource"`, `asset_path` (the object path),
`asset_name`, `duration`, `node_count`, `edge_count`, `graph_inputs[]` and
`graph_outputs[]` (each `name` and `type`), and `interfaces[]` (each `name`,
`major_version`, `minor_version`). The message is
`MetaSound: <name> (<n> nodes, <e> edges, <i> inputs, <o> outputs)`.

- The tool's description promises `output_format`; it is not returned. The
  `interfaces` list says it: `UE.OutputFormat.Mono`, `UE.OutputFormat.Stereo`
  and so on (section `nodes`).
- Interface members count as graph inputs and outputs: a new Source already
  lists `UE.Source.OnPlay` and the output format's outputs.
- The `audio` tool's `inspect` reports a MetaSound as `SoundWave`; use this
  one.

## list_nodes

Searches the node classes registered with UE's MetaSound system; only the
highest version of each class is listed.

| Parameter | Default | Meaning |
|---|---|---|
| `name_filter` | — | case-insensitive substring of `full_name` |
| `category_filter` | — | case-insensitive substring of the **namespace**, not of any category the MetaSound editor shows |
| `max_results` | `50` | how many to return; `0` or less also means 50; no upper limit. Send a JSON number |

Each entry has `class_name`, `namespace`, `full_name` and `version`
(`"<major>.<minor>"`). The result has `nodes[]`, `count` (returned) and
`total_matches`. The message is
`Found <total> registered node classes (<n> shown, max <max>)`. When
`total_matches` is above `count`, narrow the filters or raise `max_results`.

Most standard classes sit in the namespace `UE`, so `category_filter: "UE"`
narrows little; filter by name (`"Sine"`, `"Filter"`, `"Trigger"`). Section
`nodes` explains `full_name` and the variant that `add_node` needs.

## get_graph

Takes `asset_path`; the same load errors as `inspect`. The message is
`MetaSound graph: <n> nodes, <e> edges, <i> inputs, <o> outputs`.

- `nodes[]`: `node_id`, `class_name` and `namespace` (both `unknown` when the
  class is not among the document's dependencies), `inputs[]` and
  `outputs[]`. Each pin has `name`, `type` and `vertex_id`. **No default
  values** and **no variant**, despite the tool's description.
- `edges[]`: `from_node_id`, `from_vertex_id`, `to_node_id`, `to_vertex_id`.
  Edges name pins by `vertex_id`; match them against the nodes' pins.
- `graph_inputs[]` and `graph_outputs[]`: `name` and `type`.

Graph inputs and outputs have nodes of their own in `nodes[]`. Their pins
carry the member's name, so the node that takes the audio output is the one
with an input pin named `UE.OutputFormat.Mono.Audio:0` (for a Mono source;
section `nodes`).

### Reading a connection

To answer "is input X of node N connected, and from where": find N in
`nodes[]`, take the `vertex_id` of its input X, and look for an edge with
`to_node_id` N and `to_vertex_id` that id. Its `from_node_id` and
`from_vertex_id` name the source node and its output pin. An input has at
most one edge; an output can feed many. Do this before every `connect`
(section `editing`, the assertion table).
