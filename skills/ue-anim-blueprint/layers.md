# Animation layers

Operations of `anim_blueprint_modify` (bridge name
`unreal_anim_blueprint_modify`) for Animation Layer Interfaces (ALIs), the
layer function graphs they add to an AnimBP, and the Linked Anim Layer nodes
that call a layer. Every call takes `blueprint_path` (the AnimBP's asset
path) and `operation`. None of them checks for a target skeleton.

Writes compile the AnimBP and save it when the compile succeeds, but they do
not report a failed compile: run `validate_blueprint` afterwards (section
`state-machines`). Results sit under the task envelope's `data` key; the
tool's message is `Operation completed` unless noted.

## Layer graphs and `target_graph`

A layer function graph is found by name, ignoring case: first among the
graphs of the implemented layer interfaces, then among the AnimBP's own
animation function graphs. A miss lists what exists:
`Layer function graph '<n>' not found. Available: <names>`, or `(none)`.

`target_graph` names such a graph in these operations:
`create_state_machine`, `connect_state_machine_to_output` (section
`state-machines`), `add_anim_node`, `delete_anim_node`,
`connect_anim_nodes`, `bind_variable`, `get_anim_node_property`,
`set_anim_node_property` (section `anim-nodes`), and `inspect_layer_graph`
here. Where `state_machine` with `state_name` is also given, the state's graph
wins. `layout_graph` cannot address a layer graph.

The plugin's domain notes build a layer this way: `add_layer_interface`, the
variables (`blueprint_modify add_variable`), `create_state_machine` with
`target_graph` set to the layer's name, `connect_state_machine_to_output`
with the same `target_graph` (without it, the state machine is looked for in
the AnimGraph), then states, animations and transitions as usual. The notes
also say that the AnimBP's generated class is stale after
`add_layer_interface` and that you should recompile before `list_layers`.
`add_layer_interface` already compiles at its end, and whether that compile
leaves the class `list_layers` reads up to date has not been verified. Follow
the notes: run `blueprint_modify compile` (skill `ue-blueprints`) before
`list_layers`.

## Reading

### list_layer_interfaces

No further parameters. Returns `blueprint` (the AnimBP's name), `count` and
`interfaces`, each with `name`, `class_path` and `layer_functions` (the
names of the interface's graphs). Only layer interfaces are listed, not other
Blueprint interfaces.

### list_layers

No further parameters. Returns `blueprint`, `count` and `layers`, each with
`name`, `group`, `implemented` and `input_poses`.

- The list comes from the compiled class: every animation function of the
  AnimBP except `AnimGraph`, which includes the layers of its interfaces and
  layers of its own. When the AnimBP has no compiled class yet, the tool
  falls back to the interface graphs; those entries carry `source_interface`,
  an empty `group`, `implemented: true` and no input poses.
- A layer added since the last successful compile may be missing. Compile
  (any write, or `blueprint_modify compile`) and list again.

### list_linked_layer_nodes

No further parameters. Lists the Linked Anim Layer nodes in the AnimGraph
only. Returns `blueprint`, `count` and `nodes`, each with `node_id`
(`(unnamed)` when the node has no tool ID), `layer_name`, `pos_x`, `pos_y`,
`interface_class` (the interface's class path, or `(self)` for a layer of the
AnimBP itself), `interface_name` with an interface, `instance_class` when an
instance class other than the interface is set, and `connected_to_output`
(true when any output pin of the node is linked, not only to the output
pose).

### inspect_layer_graph

Needs `target_graph` (`target_graph parameter required`). Returns
`graph_name`, `graph_class`, `node_count` and `nodes`, each with `node_id`
(`(unnamed)` without a tool ID), `node_class`, `node_title`, `pos_x`, `pos_y`
and `pins` (each pin's name, direction, type, default and connections).

## Writing

### add_layer_interface

Needs `interface_path` (`interface_path parameter required`). The tool loads
**a class**, not an asset: pass the interface's generated class path, such
as `/Game/Anim/ALI_Locomotion.ALI_Locomotion_C`. The tool describes the
parameter as an asset path; the plugin's domain notes use the `_C` form.

Implements the interface, which adds one graph per layer function, then
compiles. The message is `Added interface '<name>' with <n> layer graph(s)`.
`data` holds `interface_name`, `interface_path`, `layer_graphs_created` (the
interface's graph names) and `message`.

Errors: `Could not load interface class: <path>` (a wrong path, or an asset
path without the class suffix; search with `asset_search` before you
conclude it is missing),
`Class '<c>' is not an Animation Layer Interface (must derive from UAnimLayerInterface)`,
`Interface '<c>' is already implemented`,
`Failed to implement interface '<c>'`.

The tool cannot remove an interface; there is no `remove_layer_interface`
operation.

### add_linked_layer_node

Needs `layer_name` (`layer_name parameter required`); optional `position`
and `instance_class`. Creates a Linked Anim Layer node in the AnimGraph (no
other graph) and compiles. The message is
`Created LinkedAnimLayer node for layer '<n>'`. `data` holds `node_id`
(`LinkedLayer_<layer>_<n>`), `layer_name`, `interface_class` and
`interface_name` (or `interface_class: "(self)"`) and `message`.

- The layer is bound to the first implemented interface that has a graph of
  that name. Without one, the node calls a layer of the AnimBP itself
  (`(self)`).
- **The name is not checked.** A misspelt `layer_name` still creates a node,
  bound to no existing layer; what the compile then says is up to the
  engine. Take the names from `list_layers` or `list_layer_interfaces`.
- `instance_class` is an AnimBP asset path, as for `set_layer_instance`.
  **If it does not load, it is ignored without an error.** Check with
  `list_linked_layer_nodes` (`instance_class`), or set it with
  `set_layer_instance`, which reports failures.
- The node is not connected; use `connect_anim_nodes` (section
  `anim-nodes`) with its `node_id`.
- Error: `Failed to create LinkedAnimLayer node`.

### set_layer_instance

Needs `node_id` (`node_id parameter required`) and `instance_class`
(`instance_class parameter required`). `node_id` is looked up by ID only, in
the AnimGraph. `instance_class` is the **asset path of the AnimBP** that
implements the layer, without `_C`; the tool uses its compiled class. Compiles
afterwards. The message field is
`Set instance class to '<AnimBP name>' on node '<id>'`; `data` holds
`node_id` and `instance_class` (the compiled class's path).

Errors: `Node with ID '<id>' not found in AnimGraph`,
`Node '<id>' is not a LinkedAnimLayer node (class: <c>)`,
`Could not load AnimBlueprint: <path>`,
`AnimBlueprint '<path>' has no generated class (needs compilation)` (compile
that AnimBP first, for example with `blueprint_modify compile`).
