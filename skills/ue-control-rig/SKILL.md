---
name: ue-control-rig
description: >
  Read and edit a Control Rig's RigVM graph with control_rig: unit and template
  nodes, variable nodes, links, pin defaults, member variables, recompile, batch.
  Use before you inspect or change a Control Rig asset's graph.
toolkit_tools: [control_rig]
---

# UE Control Rig: control_rig

The UE LLM Toolkit plugin's **`control_rig`** tool reads a Control Rig
Blueprint and edits its RigVM graph: it adds unit, template and variable
nodes, links pins, sets pin defaults, adds member variables and recompiles the
VM. Through the plugin's MCP bridge it is listed as `unreal_control_rig`. It
works only while the editor is running and the plugin is reachable;
`unreagent-iterate` covers the wait for the plugin.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

What the tool cannot do:

- **It cannot create a Control Rig asset.** Every call loads an existing one.
- **It cannot edit the hierarchy.** `inspect` lists bones, controls and nulls;
  no operation adds, removes or moves one.
- **It writes to one graph only**: the rig's default model. `inspect` lists
  every graph model of the rig, but a node in any other graph cannot be
  changed with this tool.
- **It never saves.** See "Saving, compiling and undo".

This skill has three sections:

| Section | Holds | Load it |
|---|---|---|
| `operations` | per single operation, the result fields, messages and errors: what `inspect` returns for a graph, node, pin, link and the hierarchy; `list_structs`, `list_templates`; the three add operations with the `cpp_type` list of `add_var_node`; `remove_node`; `link`, `unlink`, `unlink_all`; the member-variable operations; `recompile` | before you call an operation in a way the recipes do not show, and when you need a field of a result |
| `batch` | `batch` in full: the key table per `op`, the result, what a failed entry does | before you call `batch` |
| `failures` | the error table (error, cause) and the failure table (what you see, what it means, what to do) | when a call returns an error, or a result you did not expect |

## Recipes

Every call needs `rig_path` and `operation`. `rig_path` is left out of the
calls below; it is the same in each.

### Read a rig before changing it

1. `{"operation": "inspect"}`. Take node names from `graph.graphs[].nodes[].path`
   and pin names from each node's `pins[].name`. A pin path is
   `<node path>.<pin name>`.

### Add a unit node and wire it

1. `{"operation": "list_structs", "filter": "<part of the name>"}`. Take `path`
   from the entry you want.
2. `{"operation": "add_node", "struct_path": "<path>", "node_name": "<name>", "x": 0, "y": 0}`.
   Read the node's real name from `node.path` in the result, and its pins from
   `node.pins`.
3. `{"operation": "set_default", "pin_path": "<node>.<pin>", "value": "<text>"}`
   for each input that is a constant.
4. `{"operation": "link", "output_pin": "<node>.<pin>", "input_pin": "<node>.<pin>"}`
   for each connection, the execute chain included.
5. `{"operation": "recompile"}`.
6. `{"operation": "inspect"}` and check `links` and the pins' `default_value`.
7. Save with `asset` `{"operation": "save_asset", "asset_path": "<rig_path>"}`
   (skill `ue-assets`).

### Add a variable and use it in the graph

1. `{"operation": "add_member_var", "var_name": "<name>", "cpp_type": "float"}`,
   plus `is_public` and `default_value` when needed.
2. `{"operation": "add_var_node", "var_name": "<name>", "is_getter": true}`.
   The node is named `<name>_Get` (`<name>_Set` for a setter). Read its pins
   from `node.pins` in the result.
3. `link` it as above, then `recompile`, `inspect`, save.

### Build several nodes in one call

1. `{"operation": "batch", "operations": [ … ]}`. **The entries use other key
   names than the single operations** (`op`, `struct`, `name`, `source`,
   `target`, `pin`, …): see "batch".
2. When the call is an error (`Batch: <ok> OK, <n> errors, <total> total`),
   the entries that worked are in the graph. `inspect` to see which.
3. `recompile` (it can be the last entry of the batch), `inspect`, save.

### Run the rig in an Animation Blueprint

Not this tool. `anim_blueprint_modify` adds the Control Rig node and takes the
rig's generated class path (skill `ue-anim-blueprint`, section `anim-nodes`);
`blueprint_modify` `add_node` type `ControlRig` takes the asset path (skill
`ue-blueprints`, section `graphs`).

## Operations

`operation` is lowercased before dispatch. `rig_path` is required by every
operation, `list_structs` and `list_templates` included: the rig must load
even though the lists do not depend on it. An unknown operation returns
`Unknown operation: '<op>'. Valid: inspect, list_structs, …`.

| Operation | Required | Optional (default) | Writes |
|---|---|---|---|
| `inspect` | — | — | nothing |
| `list_structs` | — | `filter` | nothing |
| `list_templates` | — | `filter` | nothing |
| `add_node` | `struct_path` | `node_name`, `x` (0), `y` (0) | a unit node |
| `add_template` | `notation` | `node_name`, `x` (0), `y` (0) | a template node |
| `add_var_node` | `var_name` | `is_getter` (true), `cpp_type`, `x` (0), `y` (0) | a variable node |
| `remove_node` | `node_name` | — | removes the node |
| `link` | `output_pin`, `input_pin` | — | a link |
| `unlink` | `output_pin`, `input_pin` | — | removes one link |
| `unlink_all` | `pin_path` | `as_input` (true) | removes a pin's links |
| `set_default` | `pin_path`, `value` | — | a pin default |
| `set_expand` | `pin_path` | `expanded` (true) | the pin's expanded state |
| `add_member_var` | `var_name`, `cpp_type` | `is_public` (true), `default_value` | a member variable |
| `remove_member_var` | `var_name` | — | removes the variable |
| `recompile` | — | — | recompiles the VM |
| `batch` | `operations` | — | whatever its entries write |

A missing or empty required string returns
`Missing required parameter: <name>`. `value` is required as a non-empty
string, so the single `set_default` cannot set an empty string. A `set_default`
entry of a `batch` passes an empty `value` on to the engine.

### rig_path

The path goes to the engine's loader unchanged, with no `/Game/` fallback and
no check against `/Engine/` paths. The tool's example is a package path,
`/Game/Rigs/CR_FootIK`. Errors: `Failed to load Control Rig: <path>` and
`Asset is not a ControlRigBlueprint: <path> (is <class>)`.

### The single operations

**Load section `operations` before you call an operation in a way the recipes
above do not show, and when you need a field of a result**: it has, per
operation, the result fields, the messages and the errors. What must not be
missed is here. In the result of `inspect`:

**Only top-level pins are listed.** Sub-pins, such as the members of a
transform pin, do not appear; the header gives their path form as
`NodeName.PinName.SubPin`. Node positions are not reported either.

When the rig has no hierarchy object, `hierarchy` holds only
`error: "Hierarchy not available"`, and the call is still a success.

In `list_structs` and `list_templates`:

**At most 500 entries are returned**, and then `truncated` is `true`.
`total_available` counts everything before the filter, so it does not say how
many matched. Always pass a `filter`.

In the other operations:

- A pin's `default_value` is in `inspect` only when it is neither empty nor
  `()`. Its `direction` is one of `IN `, `OUT`, `I/O`, `HID` or `???`; `IN `
  has a trailing blank.
- `add_var_node` does not read `node_name`: the node is requested as
  `<var_name>_Get` or `<var_name>_Set`. A `cpp_type` outside the list the
  tool knows (section `operations`) is passed on without a type object, and
  whether the engine accepts that is not visible in the tool's code: for
  such a type, declare the member variable first and leave `cpp_type` out.
- Without `node_name` the engine names the node. Take the name from
  `node.path` in either case.
- The `Failed to …` errors of the three add operations and of `link` give no
  reason.

### set_default and set_expand

- `set_default` sets the default of `pin_path` to the string `value`. The
  tool's code gives no value formats. Use the text form `inspect` reports in
  `default_value` for a pin of the same type. Message: `<pin> = <value>`.
  Error: `Failed to set default on <pin>`.
- `set_expand` expands or collapses a compound pin in the graph editor. It
  changes the display only. Message: `Expanded <pin>` or `Collapsed <pin>`.
  Error: `Failed to set expansion on <pin>`.

### batch

**Load section `batch` before you call `batch`**: it has the keys per `op`,
the result and what a failed entry does. Its warnings, so that they are not
missed:

- Each entry names its operation in `op` and uses **the batch's own keys, not
  the parameter names above** (`struct`, `name`, `source`, `target`, `pin`,
  `var`, `mode`, `direction`, `type`, `public`, `default`).
- **Two defaults are the opposite of the single operations.** Without `mode`,
  `add_var_node` adds a **setter**; without `direction`, `unlink_all` breaks
  the **output** side.
- **A top-level parameter name in an entry is not read. For an optional key,
  the entry succeeds with the default**, and nothing tells you: `node_name`,
  `default_value`, `is_public`, `is_getter`, `as_input`.
- `list_structs` and `list_templates` run in a batch, but it counts them as
  errors. Call them on their own.
- **A batch is not a transaction.** A failing entry neither stops the batch
  nor undoes the entries before it.
- **When any entry failed, the whole call is an error**, and `results` does
  not reach you. `inspect` the graph, or repeat the doubtful entries as
  single operations, which return their own error.

## Saving, compiling and undo

- **Nothing is saved.** The tool has no save operation, and its code neither
  saves nor marks the package dirty; whether the engine's graph controller
  marks it dirty is not visible in the tool's code. So do not rely on
  `asset` `save_all`, which saves dirty packages only. Save the rig by path:
  `asset` `{"operation": "save_asset", "asset_path": "<rig_path>"}` (skill
  `ue-assets`). Until then every change lives only in the running editor.
- **No write compiles on its own in the tool's code.** `recompile` is the only
  operation that asks for it. Whether the engine recompiles after a graph
  change is up to the engine.
- **`recompile` reports no compile errors.** It returns success whenever the
  rig loaded. Compile errors, if any, are not in the result.
- **Undo.** Each graph write runs inside an undo bracket named
  `CR Edit: <operation>`, and `batch` inside one named `CR Edit: batch`.
  `add_member_var`, `remove_member_var` and `recompile` open none as single
  operations. The tool has no undo operation of its own; reverse a change with
  the inverse operation (`remove_node`, `unlink`, `remove_member_var`).

## What a success does not prove

| Operation | Success means | It does not prove |
|---|---|---|
| `inspect` | the rig loaded | that a hierarchy was read (`hierarchy.error`); sub-pin values |
| `list_structs`, `list_templates` | the rig loaded | that the list is complete (`truncated`) |
| `add_node`, `add_template`, `add_var_node` | the engine returned a node | that it has the name you asked for (read `node.path`), that it is linked into the execute chain, compiled or saved |
| `remove_node`, `link`, `unlink`, `unlink_all` | the engine's controller reported success | compiled, saved |
| `set_default` | the controller accepted the string | that the pin holds the value you meant: read it back with `inspect`. A sub-pin's value shows only inside its parent's `default_value` |
| `set_expand` | the display state changed | anything about the rig's behavior |
| `add_member_var` | the engine returned a variable name | that the name is the one you asked for: the message echoes your `var_name`, not the name the engine returned |
| `remove_member_var` | the engine reported success | what happened to variable nodes that used it: `inspect` |
| `recompile` | the rig loaded and the recompile was called | that the VM compiled without errors |
| `batch` (success) | every entry reported success | the same limits as each entry |
| `batch` (error) | at least one entry failed | that nothing was changed: the other entries were applied |

Whether a rig that compiles also poses the character as intended is not
something this tool reports at all.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the graph back with `inspect` before you retry: a repeated
  `add_node` or `batch` adds its nodes a second time.
- **Misspelt parameters.** Calls the bridge queues do not warn about a
  parameter the tool does not declare (skill `ue-tasks`).

## When a call fails

When a call returns an error, or a result you did not expect, load section
`failures`: the errors with their causes, and a table from what you see to
what it means and what to do.

## Engine versions

The tool's code switches one include on UE 5.7 or later. No operation of this
tool behaves differently by engine version in the tool's own code.

## Background in the plugin

- `Resources/domains/tooling.md` lists `control_rig` as "Inspect + 16 write
  ops". The code has 16 operations in all: three that read, twelve that write,
  and `batch`.
- The plugin ships no `get_ue_context` category for Control Rig (skill
  `ue-context`).

## See also

- `ue-anim-blueprint`: the Control Rig node in an AnimGraph
  (`control_rig_class`).
- `ue-blueprints`: `blueprint_modify` `add_node` type `ControlRig`.
- `ue-retarget`: IK Rigs and IK Retargeters, which are other assets than a
  Control Rig.
- `ue-assets`: `asset_search` to find rigs, `save_asset` to save one.
- `ue-tasks`: following up a call that timed out.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
