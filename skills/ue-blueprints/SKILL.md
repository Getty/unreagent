---
name: ue-blueprints
description: >
  Read and change Blueprints with blueprint_query and blueprint_modify:
  variables, functions, components, defaults, graph nodes, wiring, compiling.
  Use before you inspect or edit any Blueprint asset, or when an edit does not compile.
toolkit_tools: [blueprint_query, blueprint_modify]
---

# UE Blueprints: blueprint_query and blueprint_modify

The UE LLM Toolkit plugin has two Blueprint tools, listed through its MCP
bridge as `unreal_blueprint_query` and `unreal_blueprint_modify`:

- **`blueprint_query`** reads: lists Blueprints, returns variables,
  functions, components, defaults, graphs, nodes and connections.
- **`blueprint_modify`** writes: creates Blueprints, edits structure,
  components, defaults, graph nodes and wiring, and compiles.

They work only while the editor runs and the plugin answers
(`unreagent-iterate` covers the wait). Both take an `operation` string,
lowercased before dispatch; an unknown one returns
`Unknown operation: '<op>'. Valid: <list>`, plus ` Did you mean '<x>'?`.

No operation's parameters are in this file. Load the section before the call:

| Section | Holds | Load it |
|---|---|---|
| `workflows` | end-to-end recipes (an Actor Blueprint with components, variable, function and logic; reacting to an Input Action, and why the mapping context needs another route; the construction script), how to verify a change, what to do when a node type is missing | before you build or extend a Blueprint, or when a node you need has no node type |
| `decisions` | Blueprint vs C++ vs Python rules, the UE facts behind them (construction script, pure functions, variable flags, placed actors), 5.6–5.8 notes | before you decide where new logic or a new property goes |
| `query` | every `blueprint_query` operation: parameters, results, errors | before you list, inspect or search Blueprints, or read a graph, node or connection |
| `modify` | `create`, `reparent`, variables, functions, components, defaults, `compile`, `compile_all`, debug prints | before you create or reparent a Blueprint, change its members, components or defaults, or compile |
| `graphs` | `add_node`, `add_nodes`, `delete_node`, `connect_pins`, `disconnect_pins`, `set_pin_value`, `layout_graph`, the node types and what they cannot build | before you add, delete, wire or lay out nodes, or set a pin value |
| `failures` | errors common to both tools, how numbers and booleans are read, the failure table (what you see, what it means, what to do) | when a call fails in a way you cannot place, or a result is not what you expect |

## Must not get wrong

- **Neither tool saves.** Save with the `asset` tool, `save_asset` with
  `asset_path`, after each finished change; an editor stop loses the rest.
- **A failed compile does not roll back the edit.** The call is an error
  `<what was done> succeeded but compilation failed:` + compiler output, and
  the change stays. `compile` itself always "succeeds": read `compiled` and
  `compile_status` (`Error` and `Dirty` mean failed).
- **Class names outside `/Script/Engine` and `/Script/CoreUObject` need the
  full path**: `/Script/UMG.UserWidget`, `/Script/<Module>.<Class>`, a
  Blueprint's `/Game/X/BP_Y.BP_Y_C`. That holds for `parent_class`, object
  variable types (`/Script/UMG.UserWidget*`) and `CallFunction`'s
  `target_class`, where a class that does not resolve is **ignored without
  an error**. Only `add_component` finds bare names everywhere.
- **Functions go by engine name**, not the editor's title:
  `K2_GetActorLocation`, `Add_DoubleDouble`. The node types `Add`,
  `Subtract`, `Multiply`, `Divide` look up UE 4 names and fail on UE 5; use
  `CallFunction` (section `graphs`).
- **A new Actor Blueprint already has disabled BeginPlay, ActorBeginOverlap
  and Tick events.** Wire those instead of adding a second node; the first
  wire enables one and **erases the node ID** a query gave it, so look it up
  again (section `graphs`, "Pre-placed events").
- **`Event` `BeginPlay`/`Tick`/`EndPlay` always mean the Actor events**, also
  in a Widget Blueprint (skill `ue-widgets`, section `events`).
- **No cast, custom event, dispatcher, interface, timeline, macro, subsystem
  getter, function parameter or variable flag** can be built; section
  `workflows` says what to do instead. That includes adding a mapping context
  at BeginPlay.
- **`inspect`'s `is_instance_editable` is true for every variable**; it is
  not the editor's checkbox (section `query`).

## Paths

Every operation takes `blueprint_path`, except `list`, `get_collision` with
`actor_label`, `create` and `compile_all`. `asset_path` or `path` stand in
when it is absent; `compile_all` reads `path` as its folder.

- **The asset path**, `/Game/Blueprints/BP_Door`, or the object path `list`
  returns (`/Game/Blueprints/BP_Door.BP_Door`). No `_C`. A path without a
  leading `/` is tried as given, then under `/Game/`.
- **Checked before loading**, for every path these tools take: not empty
  (`Blueprint path cannot be empty`), at most 512 characters, no `/Engine/`
  or `/Script/` (`Cannot access engine or script Blueprints`), no `..`, none
  of `` <>|&;`$(){}[]!*?~ `` (`Blueprint path contains invalid character: '<c>'`).
- Not loadable: `Could not load Blueprint: <path>`. `blueprint_modify` also
  refuses `Cannot modify engine Blueprints` and
  `Blueprint package is read-only (cooked)`.

Widget and Animation Blueprints load too (`compile` and `inspect` work);
their own editors are `widget_editor` (skill `ue-widgets`) and
`anim_blueprint_modify` (skill `ue-anim-blueprint`).

## The edit loop

1. **Find** with `list` (`name_filter`, `path_filter`).
2. **Read before you write**: `inspect` with `include_variables` and
   `include_functions`, `get_components`, `get_defaults`, and
   `get_graph_summary` or `find_nodes` for node IDs, rather than
   `get_event_graph` on a large graph.
3. **Change one thing.** Every `blueprint_modify` change compiles right after
   the edit (`create` and `add_node` return no compile fields of their own).
   `add_nodes` builds several nodes and wires in one call and one compile.
4. **Read the compile result**; fix, or undo with the inverse operation
   (`delete_node`, `remove_variable`, …). There is no undo and no
   transaction; `add_nodes` stops at the first node it cannot create and
   keeps the ones before.
5. **Verify what you wired.** `set_pin_value`, pin values in `add_node` /
   `add_nodes` and connections in `add_nodes` can succeed without taking
   effect: `get_node`, `verify_connection`, `get_exec_chain`.
6. **Save.** Behaviour then in PIE with the output log (section `workflows`,
   "Verify a change").

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON: by default a
  task envelope (`task_id`, `status`, `success`, `message`, …) with the
  tool's fields under `data` (skill `ue-tasks`).
- **Failure** is only `Error: <message>`; the detail is in the message.
- **Timeouts**: after 300 s `Error: Task timed out after 300000ms (task_id: …)`
  ends the wait, not the work. Read the state back before a retry. A large
  `compile_all` can hold the editor past 30 s: run it as skill `ue-tasks`,
  "A 30 s timeout can crash the editor", describes.

## Node IDs

- Nodes `blueprint_modify` creates get an ID such as
  `CallFunction_PrintString_3`, returned as `node_id`.
- Other nodes get one from `find_nodes`, `get_graph_summary`,
  `get_node_connections` or `get_node` on first sight, stored as
  `MCP_ID:<id>` in the node's comment, **replacing any comment**. The queries
  do not dirty the package, so the ID persists only if the Blueprint is saved.
- The engine's object name (`K2Node_Event_0`, `AnimGraphNode_Root_0`) works
  wherever an ID does.
- `blueprint_modify` finds a node only in the graph `graph_name` and
  `is_function_graph` select; pass both for a function graph.

Number and boolean parameters that do not parse fall back to defaults or
read as 0 or `false` without an error: send JSON numbers and booleans
(section `failures`).

## Background in the plugin

- `unreal_get_ue_context`, `category: "blueprint"`: the plugin's notes on
  Blueprint graphs. Its "MCP Blueprint Operations" table names operations
  these tools do not have (`remove_node`, `set_pin_default`, `get_functions`,
  `get_graph_nodes`, `get_node_pins`, `find_references`); this skill follows
  the tools' source.
- `Resources/domains/blueprints.md`: node IDs, targeted queries,
  `layout_graph`, CDO defaults.

## See also

`ue-enhanced-input` (the Input Action assets), `ue-pie-testing` (play and
input injection), `ue-level-actors` (placed instances), `unreagent-build`
(C++ the Blueprint depends on), `unreagent-iterate` (editor and plugin wait).
