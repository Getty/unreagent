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
Blueprint and edits its RigVM graph: unit, template and variable nodes, links,
pin defaults, member variables, recompile. Through the bridge it is listed as
`unreal_control_rig`. It works only while the editor runs and the plugin is
reachable (`unreagent-iterate`).

Written from the toolkit's C++ source (`MCPTool_ControlRig.*`,
`ControlRigEditor.cpp`) and the UE 5.8.4 engine source (RigVM controller,
Control Rig units); not exercised against a running editor.

**What the tool cannot do:**

- **Create a Control Rig asset.** Every call loads an existing one.
- **Edit the hierarchy.** `inspect` lists bones, controls, nulls and the rest
  by type and name; nothing adds, removes, reparents or moves one.
- **Edit any graph but the default model.** `inspect` lists every graph; the
  writes go to `GetDefaultModel()` only (function graphs and collapsed graphs
  are read-only here).
- **Save.** It has no save operation. Save with `asset` `save_asset`.
- **Report compile errors.** `recompile` succeeds whenever the rig loaded.

## Sections

| Section | What is in it | Load it when |
|---|---|---|
| `workflows` | step-by-step edits: read a rig, add a Get/Set Transform chain in Forwards Solve, drive a node from a member variable, insert a node into the execute chain, remove a node cleanly, verify | before your first edit of a rig |
| `operations` | every single operation in full: `inspect` fields, the lists, the three add operations and the `cpp_type` list, links, defaults and value text, member variables, recompile | before a call the workflows do not show, or when you need a result field |
| `batch` | `batch`'s own key names per `op`, its inverted defaults, its result | before you call `batch` |
| `ue-facts` | RigVM and Control Rig background: solve events, the execute chain, hierarchy element types, units vs templates, pin paths and text forms, link rules, where errors go, versions — with sources | when you design a graph change, or a link or default is refused |
| `failures` | error table and symptom → cause → fix table | when a call fails or a result is not what you expected |

## Before the first call

- **Every call needs `rig_path` and `operation`** — the two lists too. The rig
  must load. `rig_path` goes to the loader unchanged, no `/Game/` fallback.
  Find rigs with `asset_search` (skill `ue-assets`); use the object path.
- **Result shape.** Success is the message, a blank line, then JSON; through
  the task queue the tool's fields sit under `data` (skill `ue-tasks`).
  **Failure is only `Error: <message>`, with no data.**
- **The engine's reason for a refusal is in the output log, not in the
  result.** The tool's `Failed to …` errors give no reason; the RigVM
  controller reports it as a `LogScript` error,
  `Script Msg: <package> : <reason>` (for a link:
  `Cannot link '<a>' to '<b>': <why>.`). Read it with `get_output_log`
  (`filter: "Script Msg"`; skill `ue-pie-testing`). From the 5.8.4 source;
  unverified on 5.6/5.7.
- **Timeouts.** After 300 s: `Error: Task timed out after 300000ms …`. The work
  may still finish. `inspect` before you retry — a repeated `add_node` or
  `batch` adds its nodes again.
- **Undo.** Graph writes run in an undo bracket `CR Edit: <operation>` (batch:
  `CR Edit: batch`); the member-variable operations and `recompile` open none.
  The tool has no undo; reverse with the inverse operation.

## Operations

| Operation | Required (besides `rig_path`) | Optional (default) |
|---|---|---|
| `inspect` | — | — |
| `list_structs` | — | `filter` |
| `list_templates` | — | `filter` |
| `add_node` | `struct_path` | `node_name`, `x` (0), `y` (0) |
| `add_template` | `notation` | `node_name`, `x` (0), `y` (0) |
| `add_var_node` | `var_name` | `is_getter` (true), `cpp_type`, `x` (0), `y` (0) |
| `remove_node` | `node_name` | — |
| `link` | `output_pin`, `input_pin` | — |
| `unlink` | `output_pin`, `input_pin` | — |
| `unlink_all` | `pin_path` | `as_input` (true) |
| `set_default` | `pin_path`, `value` | — |
| `set_expand` | `pin_path` | `expanded` (true) |
| `add_member_var` | `var_name`, `cpp_type` | `is_public` (true), `default_value` |
| `remove_member_var` | `var_name` | — |
| `recompile` | — | — |
| `batch` | `operations` | — |

`operation` is lowercased; there are no aliases. Pin paths are
`<node>.<pin>` and `<node>.<pin>.<subpin>`; `<node>` is the node's `path` from
`inspect` or the add result.

## Must not get wrong

1. **Read before you write.** Take node names from `graph.graphs[].nodes[].path`
   and pin names from `pins[].name`; never guess them. A node may be named
   otherwise than you asked; read `node.path` from the add result.
2. **`link` replaces.** Linking into an input that already has a link breaks
   the old link; linking **from an execute output** breaks that output's
   existing link (an execute output drives one node). To insert a node into
   the execute chain, link `prev → new` and `new → next`, in that order (section
   `ue-facts`).
3. **A node not on the execute chain of an event does nothing.** Mutable units
   (Set Transform and the like) must be chained from Forwards Solve (or
   another event); pure ones (Get Transform) only need their outputs used.
4. **`inspect` lists top-level pins only.** Sub-pins (`Item.Name`,
   `Value.Translation.X`) are not listed, and a default is shown only when it
   is neither empty nor `()`. Read struct values from the parent pin's
   `default_value`.
5. **`set_default` takes the engine's text form**, e.g.
   `(Type=Bone,Name="hand_r")` for a rig element key. Copy the form from a pin
   of the same type in `inspect`; arrays are not resized by this call.
6. **Batch entries use other key names** (`struct`, `name`, `source`,
   `target`, `pin`, `var`, `mode`, …) and **two defaults are inverted**:
   `add_var_node` without `mode: "get"` adds a **setter**; `unlink_all`
   without `direction: "in"` breaks the **output** side.
7. **A failed batch hides which entry failed**, and the entries that worked
   stay applied.
8. **`recompile` proves nothing.** After it, check `get_output_log` for
   Control Rig / RigVM errors (whether every compiler message reaches the
   log is unverified).
9. **Save explicitly**: `asset` `{"operation":"save_asset","asset_path":"<rig_path>"}`.
   The tool does not mark the package dirty itself, so do not rely on
   `save_all`.

## The edit loop

1. **Read**: `inspect`. Note the event nodes (struct `RigUnit_BeginExecution` is
   Forwards Solve), their execute pins, the chain and the hierarchy names you
   will reference.
2. **Find units**: `list_structs` with a `filter` (max 500 results, check
   `truncated`); templates via `list_templates`.
3. **Edit**: add nodes, set defaults, link — single calls while you learn the
   pins, `batch` once you know them.
4. **Recompile**: `recompile`, then `get_output_log` for errors.
5. **Verify** with `inspect`: every new node present under the expected
   `path`; `links[]` contains each `source → target` you made and the
   execute chain is unbroken from the event; each pin you set shows the
   `default_value` you meant (sub-pins via the parent).
6. **Save** with `asset` `save_asset`.

Seeing the rig pose the character is outside this tool: preview it in the
editor, or run it in an Animation Blueprint and check in PIE (skills
`ue-anim-blueprint`, `ue-pie-testing`, `ue-viewport`).

## Decision rules

- **Graph logic** (what the rig computes) → this tool. **Hierarchy** (new
  bones, controls, nulls) → the Control Rig editor or Python (skill
  `ue-python`); not this tool.
- **Concrete unit** (`RigUnit_*`, fixed pin types) → `add_node`.
  **Generic node** whose type resolves on link (math, some array nodes) →
  `add_template` with a notation from `list_templates`.
- **Value set once** → `set_default`. **Value tuned from outside** (an AnimBP,
  Sequencer) → a public member variable + `add_var_node` getter.
- **One edit whose error you need** → the single operation. **Many known edits**
  → `batch`, followed by `inspect`.
- **Running the rig on a character** → `anim_blueprint_modify` (Control Rig
  node, skill `ue-anim-blueprint`, section `anim-nodes`) or `blueprint_modify`
  `add_node` type `ControlRig` (skill `ue-blueprints`, section `graphs`).

## When a call fails

Load section `failures`. First stop for a bare `Failed to …`: the
`Script Msg` line in `get_output_log`.

## See also

- `ue-anim-blueprint`: the Control Rig node in an AnimGraph
  (`control_rig_class`).
- `ue-blueprints`: `blueprint_modify` `add_node` type `ControlRig`.
- `ue-retarget`: IK Rigs and IK Retargeters, which are other assets.
- `ue-assets`: `asset_search` to find rigs, `save_asset` to save one.
- `ue-pie-testing`: `get_output_log`.
- `ue-tasks`: following up a call that timed out.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
