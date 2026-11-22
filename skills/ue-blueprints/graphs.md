# blueprint_modify: graph nodes and wiring

These operations add, delete and wire nodes in one graph of a Blueprint. Get
node IDs and pin names with `blueprint_query` first (section `query`), and
check the result afterwards with `get_node` or `verify_connection`.

| Operation | Required besides `blueprint_path` | Optional |
|---|---|---|
| `add_node` | `node_type` | `node_params`, `pos_x`, `pos_y`, graph selection |
| `add_nodes` | `nodes` | `connections`, graph selection |
| `delete_node` | `node_id` | graph selection |
| `connect_pins` | `source_node_id`, `target_node_id` | `source_pin`, `target_pin`, graph selection |
| `disconnect_pins` | `source_node_id`, `source_pin`, `target_node_id`, `target_pin` | graph selection |
| `set_pin_value` | `node_id`, `pin_name`, `pin_value` | graph selection |
| `layout_graph` | — | `spacing_x`, `spacing_y`, `preserve_existing`, graph selection |

Each operation compiles the Blueprint after the change.

## Graph selection

`graph_name` and `is_function_graph` (default false) pick the graph:

- No `graph_name`: the first event graph, or with `is_function_graph: true`
  the first function graph. Errors: `No event graphs found`,
  `No function graphs found`.
- `graph_name: "AnimGraph"`: the AnimGraph of an Animation Blueprint, whatever
  `is_function_graph` says: `AnimGraph not found. Is this an Animation Blueprint?`.
- Any other name is looked up among the event graphs, or with
  `is_function_graph: true` among the function graphs:
  `Graph '<g>' not found. Available: <names>`. The list names only graphs of
  the kind you asked for. A function's graph has the function's name.

Macro graphs and the graphs inside state machine states cannot be reached
this way. For state graphs, the plugin's domain notes point to
`anim_blueprint_modify` with `state_machine` and `state_name`.

Nodes are looked up inside the selected graph only. Pass the same graph
selection to every call that touches a node.

## Node types

`node_type` is matched regardless of case. `node_params` carries the type's
parameters:

| `node_type` | `node_params` | Creates |
|---|---|---|
| `CallFunction` | `function` (required), `target_class` | a function call |
| `PrintString` | — | a `PrintString` call |
| `Branch` (also `IfThenElse`) | — | a branch |
| `Event` | `event` (required) | an event node |
| `EnhancedInputAction` | `action_path` (required) | an Enhanced Input action event |
| `VariableGet` (also `GetVariable`) | `variable` (required) | a getter |
| `VariableSet` (also `SetVariable`) | `variable` (required) | a setter |
| `Sequence` | `num_outputs` (default and minimum 2) | an exec sequence |
| `Add`, `Subtract`, `Multiply`, `Divide` | — | a float math call |
| `ModifyBone` | `bone_name` (required), `translation_mode`, `translation_space` | AnimGraph: Modify Bone |
| `TwoBoneIK` | `bone_name` (required), `effector_space` | AnimGraph: Two Bone IK |
| `ControlRig` | `control_rig_class` (required) | AnimGraph: Control Rig |

The tool's description lists the types of the first nine rows. `ModifyBone`,
`TwoBoneIK` and `ControlRig` exist in the code and belong in an AnimGraph; the tool does
not check the graph's kind. Any other type returns
`Unknown node type: '<t>'. Supported: …`.

**`CallFunction`.** The function is looked up in `target_class`, then in
`KismetSystemLibrary`, `KismetMathLibrary`, `KismetStringLibrary` and
`GameplayStatics`, in that order. `target_class` is tried as a loaded class
name, and the short names `KismetSystemLibrary`, `KismetMathLibrary`,
`KismetStringLibrary`, `AnimInstance` and `GameplayStatics` always resolve. A
`target_class` that does not resolve is ignored, and a library function of the
same name can be picked instead. Read the result's `pins` to see which
function you got. Errors: `Function name is required`, `Function '<f>' not found`.

**`Event`.** `BeginPlay`, `Tick` and `EndPlay` map to the Actor events
`ReceiveBeginPlay`, `ReceiveTick` and `ReceiveEndPlay`. Any other name must be
a function of the parent class, such as `BlueprintUpdateAnimation` in an
Animation Blueprint. There is no custom event type. The tool does not check
whether the event already has a node. Run `find_nodes` first. Errors:
`Event name is required`, `Event '<e>' not found. Common events: BeginPlay, Tick, EndPlay`.

**`EnhancedInputAction`.** `action_path` is the Input Action asset, such as
`/Game/Input/IA_Look` (skill `ue-enhanced-input`). The plugin's source
comments name the node's pins: one exec output per trigger event (`Started`,
`Ongoing`, `Triggered`, `Completed`, `Canceled`), plus `ActionValue`,
`ElapsedSeconds`, `TriggeredSeconds` and `InputAction`. `Triggered` is the
usual exec pin. Errors:
`action_path is required for EnhancedInputAction (e.g. '/Game/Input/IA_Look')`,
`InputAction '<p>' not found`.

**`VariableGet`, `VariableSet`.** A getter takes a variable of the Blueprint or
a property of its parent class. A setter takes only the Blueprint's own
variables. Errors: `Variable name is required`,
`Variable '<v>' not found in Blueprint or parent class` (getter),
`Variable '<v>' not found in Blueprint` (setter).

**Math.** `Add`, `Subtract`, `Multiply` and `Divide` call the math library
functions `Add_FloatFloat`, `Subtract_FloatFloat`, `Multiply_FloatFloat` and
`Divide_FloatFloat`. When the engine has no such function, the error is
`Math function '<f>' not found`. Then use `CallFunction` with
`target_class: "KismetMathLibrary"` and a function the engine has.

**AnimGraph nodes.**

- `ModifyBone`: translation is additive in component space, and rotation and
  scale are ignored. `translation_mode` takes `Replace` or `Ignore`.
  `translation_space` takes `WorldSpace`, `BoneSpace` or `ParentBoneSpace`.
  Error: `bone_name is required for ModifyBone`.
- `TwoBoneIK`: the effector location is in bone space. `effector_space` takes
  `ComponentSpace`, `WorldSpace` or `ParentBoneSpace`.
  Error: `bone_name is required for TwoBoneIK`.
- `ControlRig`: `control_rig_class` is the Control Rig Blueprint's asset path,
  such as `/Game/Blueprints/CR_MyControlRig`. Every Control Rig variable that
  can be shown as a pin is shown. Errors: `Control Rig Blueprint '<p>' not found`,
  `Control Rig '<p>' has no generated class (compile it first)`,
  `'<p>' is not a Control Rig (generated class is not a UControlRig subclass)`.
  The rig's own graph is edited with `control_rig` (skill `ue-control-rig`).

**Node IDs.** A new node's ID is `<node_type>_<detail>_<n>`: the type as you
spelled it, then the function, event, action path, variable, math operation,
bone or rig, then a counter. `Branch_7` and `Sequence_8` have no detail. Use
the `node_id` from the result rather than building one.

## add_node

Creates one node. `pos_x` and `pos_y` default to 0. To set input pins at the
same time, put `pin_values` inside `node_params`: an object of pin name to
value string. Each value is read with the engine's `FJsonValue::TryGetString`:
a JSON number or boolean is read as its text, `5`, `0.5` or `true`, and
`null`, an array or an object is skipped (main document, "Errors common to
both tools", for the engine source). Send strings, so the text is yours. A pin
value that does not apply is skipped without an error, as with `set_pin_value`.

```json
{"operation": "add_node", "blueprint_path": "/Game/BP/BP_Door",
 "node_type": "CallFunction",
 "node_params": {"function": "PrintString", "pin_values": {"InString": "Door opened"}}}
```

The result is the node itself: `node_id`, `class`, `pos_x`, `pos_y` and
`pins` (each with `name`, `direction` `Input` or `Output`, `type`,
`default_value` when set, and `connections`, the link count), plus
`blueprint_path` and `graph_name`. Use these pin names for wiring.

## add_nodes

Creates several nodes and wires them in one call, with one compile at the end.

`nodes` is an array of node specs:

- `type`: the node type (required).
- `pos_x`, `pos_y`: position (default 0).
- `params`: the `node_params` object. Without `params`, the fields `function`,
  `target_class`, `event`, `variable` and `num_outputs` may sit in the spec
  directly. Every other parameter (`action_path`, `bone_name`,
  `control_rig_class` and the mode and space settings) works only inside
  `params`.
- `pin_values`: input pin values, **at the spec level**. Unlike `add_node`,
  `pin_values` inside `params` is ignored here.

`connections` is an array of `{from_node, from_pin, to_node, to_pin}`. A number
in `from_node` or `to_node` is an index into this call's `nodes`. A string is
the ID of a node already in the graph. Empty or missing pin names wire the
first exec pins, as in `connect_pins`.

```json
{"operation": "add_nodes", "blueprint_path": "/Game/BP/BP_Door",
 "nodes": [
   {"type": "Event", "event": "BeginPlay", "pos_x": 0, "pos_y": 0},
   {"type": "PrintString", "pos_x": 300, "pos_y": 0,
    "pin_values": {"InString": "Door ready"}}
 ],
 "connections": [{"from_node": 0, "to_node": 1}]}
```

The result adds `graph_name`, `nodes` (each as in `add_node`, plus its
`index`) and `node_count` to the compile fields.

What it does not report:

- **A connection that fails is skipped silently**, and so is an index out of
  range or an unknown node ID. Check every wire with `verify_connection` or
  `get_node_connections`, and redo a missing one with `connect_pins`. That
  call reports why it fails.
- **A node it cannot create stops the call** with
  `Failed to create node <i>: <reason>`, and the nodes before it stay in the
  graph. Spec errors look the same: `Node at index <i> is not a valid object`,
  `Node at index <i> missing 'type' field`. A missing array returns
  `'nodes' array is required`. Find the leftover nodes with `find_nodes`.

## delete_node

Breaks every link of the node and removes it. Error: `Node '<id>' not found`.
The result has `node_id`.

## connect_pins

Wires one pin to another through the graph's schema, which checks the pin
types.

- `source_pin` empty: the source node's first exec output.
  `target_pin` empty: the target node's first exec input. Errors:
  `No exec output pin found on node '<id>'`, `No exec input pin found on node '<id>'`.
- On a node with several exec outputs, such as a branch, a sequence (`then_0`,
  `then_1`, …) or an Enhanced Input action, name the pin. The first one is
  rarely the one you want.
- A named pin is matched regardless of case. The source pin is looked for
  among the outputs first, the target pin among the inputs first.

Errors: `Source node '<id>' not found`, `Target node '<id>' not found`,
`Pin '<p>' not found on source node '<id>'`,
`Pin '<p>' not found on target node '<id>'`,
`Cannot connect pins: <reason>`, `Schema rejected connection`.

The result has `source_node_id`, `source_pin`, `target_node_id` and
`target_pin`, with `(auto exec)` for an empty pin name.

## disconnect_pins

Breaks the link between two named pins. All four parameters are required.
Errors are the node and pin errors of `connect_pins`. **It succeeds even when
the two pins were not linked.** Check with `verify_connection`.

## set_pin_value

Sets the default value of an input pin. `pin_value` is a string in the pin's
default-value format and cannot be empty. Error:
`Input pin '<p>' not found on node '<id>'`.

**The schema can reject the value, and the call still succeeds.** The result
then echoes your value, but the pin keeps its old one. Read the pin back with
`blueprint_query get_node`, and match the format of the `default_value` shown
there for pins of that type.

## layout_graph

Arranges the nodes of one graph into a grid. Starting at every event, custom
event and function entry, it follows the exec wiring: each step goes one
column to the right. A data-only node goes one column left of the first node
that uses it, and nodes reached from no entry are stacked in the first column.

- `spacing_x` (default 400): the column width.
- `spacing_y` (default 200): the row height.
- `preserve_existing` (default false): leave every node whose position is not
  `0, 0` where it is.

The result adds `success`, `graph_name`, `total_nodes`, `layout_nodes`,
`skipped_nodes`, `disconnected_nodes`, `data_only_nodes` and `entry_points` to
the compile fields. `graph_name` reads `EventGraph` whenever you passed none.

An AnimGraph has no event or function entry, so here every node counts as
disconnected and ends up in the first column. The plugin's domain notes point
to `anim_blueprint_modify`'s own `layout_graph` for Animation Blueprints. They
also advise running the layout once, after a graph is built, rather than after
every node.
