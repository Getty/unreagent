# blueprint_query

`blueprint_query` reads Blueprints, Widget Blueprints and Animation Blueprints.
Apart from the node IDs it writes into node comments (see "Node IDs" in the
main document), it changes nothing.

Every operation except `list` needs `blueprint_path`. The aliases `asset_path`
and `path` are accepted in its place. The operation aliases `get_variables`,
`get_info` and `info` run `inspect`, and `get_properties` runs `get_defaults`.

| Operation | Use it to | Parameters besides `blueprint_path` |
|---|---|---|
| `list` | find Blueprints | `path_filter`, `type_filter`, `name_filter`, `limit` |
| `inspect` | parent class, variables, functions | `include_variables`, `include_functions`, `include_graphs` |
| `get_graph` | count nodes and events, list graph names | — |
| `get_components` | the component tree | `component_name` |
| `get_collision` | collision settings of the primitive components | `actor_label` |
| `get_event_graph` | the event graph as readable execution chains | `graph_name` |
| `get_anim_graph` | the anim graph pose chain | `graph_name` |
| `get_state_machine_detail` | states, transitions, rule expressions | `state_machine_name` |
| `get_defaults` | class default (CDO) values | `properties` |
| `find_nodes` | search nodes, get IDs | `search`, `node_class`, `graph_name`, `graph_type` |
| `get_node` | one node with every pin | `node_id`, `graph_name` |
| `verify_connection` | check one wire | `source_node_id`, `target_node_id`, `source_pin`, `target_pin` |
| `get_node_connections` | one node's wiring | `node_id`, `direction`, `pin_filter` |
| `get_graph_summary` | node census and all IDs, per graph | `graph_name`, `graph_type` |
| `get_exec_chain` | walk the exec flow from one node | `node_id`, `max_depth` |

For a large graph, prefer `get_graph_summary`, `find_nodes`, `get_node`,
`verify_connection` and `get_node_connections` to the full dumps of
`get_event_graph` and `get_anim_graph`.

## list

- `path_filter` (default `/Game/`) limits the search to a folder and its
  subfolders. It passes the same path check as `blueprint_path`.
- `type_filter` is matched, case-insensitively, as a substring of the
  **parent class path**, not of the Blueprint type. `Character` finds
  Blueprints derived directly from a class whose path contains it: the parent
  class path stored with the asset, `/Script/Engine.Character` or
  `/Game/BP/BP_Base.BP_Base_C`. `AnimInstance` finds Animation Blueprints on
  the engine's base. `UserWidget` finds Widget Blueprints whose parent is
  `UserWidget` itself, and misses one whose parent is a C++ or Common UI
  widget class (`CommonActivatableWidget`) or another Widget Blueprint;
  likewise a Blueprint whose parent is another Blueprint matches only that
  Blueprint's name (`MCPTool_BlueprintQuery.cpp:156`). When in doubt, leave out
  `type_filter`, filter by `name_filter` (`WBP_`, `BP_`) instead.
- `name_filter` is a case-insensitive substring of the asset name.
- `limit` defaults to 25 and is clamped to 1–1000.

Returns `blueprints` (each with `name`, `path`, `blueprint_type`,
`parent_class`), `count` and `total_matching`. `path` is the object path,
`/Game/BP/BP_Door.BP_Door`; both tools accept it as `blueprint_path`, as they
accept the plain asset path. `parent_class` is the bare class name, without
its module path and without a `_C` suffix. `truncated: true` is added only
when more Blueprints matched than were returned; a result without `truncated`
is complete. When it is there, narrow the filters or raise `limit`.

## inspect

Returns `name`, `path`, `blueprint_type`, `parent_class`, `parent_class_name`
and `generated_class`. The three flags each default to false and add:

- `include_variables`: `variables`, the Blueprint's own variables only (not
  the parent class's), each with `name`, `type`, `category`,
  `is_instance_editable`, `is_blueprint_read_only`, `is_exposed_on_spawn` and,
  when set, `default_value`.
- `include_functions`: `functions`. A function graph comes with `name`,
  `type: "Function"`, and `inputs` and `outputs` (each `name`, `type`) when it
  has an entry or result node. An Actor Blueprint lists its construction
  script here as `UserConstructionScript`. Every event graph with at least
  one event node is in the same list, as `type: "EventGraph"` with
  `event_count` and `node_count`.
- `include_graphs`: `graph_info`, the `get_graph` result.

**`is_instance_editable` does not mean the editor's "Instance Editable".** It
reports the engine flag `CPF_Edit`, which every Blueprint variable gets on
creation; the editor's checkbox is the absence of `CPF_DisableEditOnInstance`,
which the tool does not report. Read it as always true
(`BlueprintUtils.cpp:111`; `FBlueprintEditorUtils::AddMemberVariable` in the
UE 5.8.4 source, not run).

## get_graph

Returns `total_nodes`, `total_events`, `function_count`, `graph_names`,
`blueprint_name` and `blueprint_path` for the whole Blueprint. `graph_names`
lists the event graphs first, then the function graphs. It reads no `graph_name`. Use it for the graph names you
then pass to the other operations.

## get_components

Without `component_name`, returns the component tree and `total_components`.
With `component_name` (the CDO component name or the variable name of a
component added in the Blueprint), it returns that one component, or
`Component '<n>' not found. Available: …`. A Blueprint without a generated
class returns `total_components: 0`.

## get_collision

Without `actor_label`, it reads the collision settings of the Blueprint's class
defaults. With `actor_label`, it reads a placed actor in the open level instead,
found by name or label, and needs no `blueprint_path`. Errors on that path:

- `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?`
- `No active world. Open a level in the editor first`
- `Actor not found: <x>. Use level_query find to search for actors in the current level`

## get_event_graph, get_anim_graph, get_state_machine_detail

Each returns `summary`, a text rendering, and `metadata`. `get_event_graph` and
`get_anim_graph` take an optional `graph_name`. `get_state_machine_detail`
takes `state_machine_name`. A state machine's internal name can differ from the
name the editor shows; the plugin's domain notes recommend reading it here
before you change a state machine.

## get_defaults

Needs a compiled Blueprint: `Blueprint '<n>' has no GeneratedClass — compile it first`.

- With `properties` (an array of property names or dot paths), returns
  `values`, `count`, and `errors` for every name it could not read.
- Without it, returns `overrides`: the values that differ from the parent
  class, and `count`.

Both forms also return `blueprint_name`, `blueprint_path`, `class` and
`parent_class`.

## find_nodes

All filters are optional, and every given filter must match:

- `search`: case-insensitive substring of the node's label, the `label`
  field of the result.
- `node_class`: case-insensitive substring of the node class, such as
  `K2Node_CallFunction`.
- `graph_name`: only this graph.
- `graph_type`: `EventGraph`, `AnimGraph` or `All`.

Reroute (knot) and comment nodes are skipped. Without `graph_name`, and with
`graph_type` empty, `All`, `AnimGraph` or `StateGraph`, the search also covers
the graphs inside state machine states.

Returns `nodes` (each with `node_id`, `label`, `node_class`, `graph_name`,
`graph_type`, `pos_x`, `pos_y`, `pin_count`, `connected_pin_count`, plus
`state_machine` and `state_name` for nodes inside a state) and `count`.
`graph_type` is `EventGraph`, `AnimGraph`, `StateMachine`, `StateGraph` or
`Other`. A function graph counts as `EventGraph` (it has an entry node), so
`graph_type: "EventGraph"` also searches the functions and the construction
script.

The ID of a node found here for the first time is built from its kind:
`Event_ReceiveBeginPlay_<n>`, `FunctionEntry_<name>_<n>`,
`CallFunction_<function>_<n>`, `VarGet_<var>_<n>`, `Cast_<class>_<n>`, and so
on, or `Node_<n>` for a kind the tool does not name.

## get_node

Needs `node_id`. It searches every graph; `graph_name` is only where it looks
first. Returns the node with `graph_name` and `graph_type`. For an ordinary
Blueprint node, the result has `node_id`, `node_class`, `label`, `pos_x`,
`pos_y` and `pins`, each with `name`, `direction`, `type`, `connected`,
`linked_to` (`node_id` and `pin_name` per link) and `default_value`. Hidden
pins are left out; pins the editor folds away as "advanced" are listed. A
linked node that has no ID yet shows an empty `node_id`.
Error: `Node '<id>' not found`.

- `type` here is the engine's pin category (`exec`, `bool`, `real`, `struct`,
  `object`, …), not the type names `add_node` returns (`float`, `FVector`,
  `Actor*`).
- `default_value` is the pin's text value only. An object or class pin set to
  an asset keeps it as an object reference instead, so it shows **no**
  `default_value` even when it is set (`BlueprintGraphReader.cpp:1207`).

## verify_connection

Needs `source_node_id` and `target_node_id`. `source_pin` and `target_pin` are
optional; empty means any pin. It follows reroute nodes. The message is
`CONNECTED` or `NOT CONNECTED`; the data has `connected`, `source_label` and
`target_label`, and on a match `source_pin` and `target_pin`. Errors:
`Source node '<id>' not found`, `Target node '<id>' not found`.

## get_node_connections

Needs `node_id`. `direction` is `input`, `output` or `both` (empty means both).
`pin_filter` is a comma-separated list of pin names. Returns `node_id`, `label`,
`connections` with `input` and `output` lists (each entry has `pin_name`,
`pin_type`, and `connected_to` with `node_id`, `pin_name` and `label`), and
`unconnected_pins`. It reads no `graph_name`.

## get_graph_summary

Optional `graph_name` and `graph_type`, filtering as in `find_nodes`: the
graphs inside state machine states are included unless `graph_name` is given or
`graph_type` names another type. Returns `graphs`, each with `name`,
`type`, `total_nodes`, `nodes_by_type`, `entry_points` (`node_id`, `label`)
and `all_node_ids`, and `graph_count`. This is the cheapest way to get every ID
in a graph.

## get_exec_chain

Needs `node_id`, normally an event or other entry node. Returns `start_node`,
`start_label`, `chain_summary`, `nodes_in_chain`, `node_count` and
`max_depth_reached`. `max_depth` (default 50) does not limit the walk in the
current code. It only sets `max_depth_reached` when the walk visited at least
that many nodes. An entry in `nodes_in_chain` that looks like
`K2Node_CallFunction_3` is the engine's object name of a node without an ID;
every operation that takes a node ID accepts it.
