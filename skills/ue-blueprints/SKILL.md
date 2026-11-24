---
name: ue-blueprints
description: >
  Read and change Blueprints with blueprint_query and blueprint_modify:
  variables, functions, components, defaults, graph nodes, wiring, compiling.
  Use before you inspect or edit any Blueprint asset, or when an edit does not compile.
toolkit_tools: [blueprint_query, blueprint_modify]
---

# UE Blueprints: blueprint_query and blueprint_modify

The UE LLM Toolkit plugin runs inside the editor and has two Blueprint tools:

- **`blueprint_query`** reads. It lists Blueprints and returns their
  variables, functions, components, defaults, graphs, nodes and connections.
- **`blueprint_modify`** writes. It creates Blueprints and edits their
  structure, components, defaults, graph nodes and wiring, and it compiles.

Through the plugin's MCP bridge, the two tools are listed as
`unreal_blueprint_query` and `unreal_blueprint_modify`. They work only while the
editor is running and the plugin is reachable; `unreagent-iterate` covers the
wait for the plugin.

Both tools take an `operation` string. The tool lowercases it before
dispatch. An unknown operation returns
`Unknown operation: '<op>'. Valid: <list>`, plus ` Did you mean '<x>'?` when a
valid name is close.

This skill has four sections. No operation's parameters are in this file:
load the section that covers an operation before you call it.

| Section | Covers | Load it |
|---|---|---|
| `query` | every `blueprint_query` operation: parameters, results, errors | before you list, inspect or search Blueprints, or read a graph, a node or a connection |
| `modify` | `create`, `reparent`, variables, functions, components, defaults, `compile`, `compile_all`, debug prints | before you create or reparent a Blueprint, change its variables, functions, components or defaults, or compile it |
| `graphs` | graph nodes and wiring: `add_node`, `add_nodes`, `delete_node`, `connect_pins`, `disconnect_pins`, `set_pin_value`, `layout_graph` | before you add, delete, wire or lay out graph nodes, or set a pin value |
| `failures` | the errors common to both tools, how the tools read numbers and booleans, and the failure table (what you see, what it means, what to do) | when a call returns an error you cannot place, what you see after a call is not what you expect, or a number or boolean you sent seems not to have arrived |

## Paths

Every operation takes `blueprint_path`, except `blueprint_query list`,
`blueprint_query get_collision` with `actor_label`, `blueprint_modify create`
and `blueprint_modify compile_all`. When `blueprint_path` is absent, both tools
take `asset_path` or `path` in its place. `compile_all` reads `path` as the
folder to scan.

- **Use the asset path**, such as `/Game/Blueprints/BP_Door`. Do not add a
  `_C` suffix; that suffix names the generated class, not the asset. A path
  without a leading `/` is tried as given and then with `/Game/` in front.
- **The path is validated before anything loads.** These are rejected:
  - an empty path: `Blueprint path cannot be empty`;
  - a path longer than 512 characters;
  - `/Engine/` and `/Script/` paths: `Cannot access engine or script Blueprints`.
    Engine Blueprints can be neither read nor changed;
  - `..`: `Blueprint path cannot contain path traversal sequences`;
  - any of the characters `` <>|&;`$(){}[]!*?~ ``:
    `Blueprint path contains invalid character: '<c>'`.

  The same check applies to every path these tools take: `path_filter`,
  `package_path`, the `compile_all` folder.
- **A path that does not load** returns `Could not load Blueprint: <path>`.
- **Before a change**, `blueprint_modify` also refuses engine Blueprints
  (`Cannot modify engine Blueprints`) and cooked packages
  (`Blueprint package is read-only (cooked)`).

Widget Blueprints and Animation Blueprints are Blueprints too. Both tools
load them, so `blueprint_modify compile` and `blueprint_query inspect` work on
a Widget Blueprint. Their own editing tools are `widget_editor` (skill
`ue-widgets`) and `anim_blueprint_modify`.

## The edit loop

1. **Find the asset.** `blueprint_query list` with `name_filter` or
   `path_filter` (section `query`).
2. **Read before you write.** `inspect` with `include_variables` and
   `include_functions` gives you the members. `get_components` gives the
   component tree, `get_defaults` the CDO values. `get_graph_summary` or
   `find_nodes` gives node IDs. Use these targeted queries rather than
   `get_event_graph` on a large graph.
3. **Change one thing.** Every `blueprint_modify` change compiles the Blueprint
   right after the edit. `add_nodes` builds several nodes and their wiring in
   one call.
4. **Read the compile result.** On success, most results carry `compiled` and
   `compile_status` (`UpToDate`, `UpToDateWithWarnings`), and `error_count`,
   `warning_count`, `compile_output` and `compile_messages` whenever there were
   warnings. `add_node` returns the new node instead, and `create` runs no
   compile step of its own (section `modify`). When the compile fails, the call is an error:
   `<what was done> succeeded but compilation failed:` followed by the compiler
   output. **The edit is not rolled back.** It stays in the editor's copy of the
   Blueprint. Fix it, or undo it with the inverse operation (`delete_node`,
   `remove_variable`, and so on).
5. **Verify what you wired.** Several writes succeed without having fully
   taken effect: `set_pin_value`, pin values in `add_node` and `add_nodes`, and
   connections in `add_nodes` (section `graphs`). Check with `get_node` or
   `verify_connection`.
6. **Save.** Neither tool saves. Every change lives only in the running editor
   until the asset is saved, and an editor stop or crash loses it. Save with the
   toolkit's `asset` tool: `operation: "save_asset"` with `asset_path`, or
   `operation: "save_all"` for every modified package, maps included.

`blueprint_modify` has no undo of its own and no transaction around a call.
`add_nodes` stops at the first node it cannot create and leaves the nodes it
had already created in the graph.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools.
- **Failure** is only `Error: <message>`. The data of a failed call does not
  reach you. Errors in these tools carry their detail in the message itself,
  for example the compiler output after a failed compile.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work: a long `compile_all` may still finish. Read the state back before
  you retry (skill `ue-tasks`). A `compile_all` over many Blueprints can hold
  the editor for more than 30 s; run it as skill `ue-tasks`, "A 30 s timeout
  can crash the editor", describes.

## Node IDs

Graph operations address nodes by ID.

- Nodes that `blueprint_modify` creates get an ID such as
  `CallFunction_PrintString_3`. Every result that creates a node returns it as
  `node_id`.
- For a node created any other way, `find_nodes`, `get_graph_summary`,
  `get_node_connections` and `get_node` assign an ID on first sight. These
  queries are read-only in name only: the ID is stored as `MCP_ID:<id>` in the
  node's comment, and **it replaces any comment the node already had**. The
  queries do not mark the package as modified, so the ID is saved only if
  something else saves the Blueprint.
- Animation graph nodes are addressed by their UE object name, for example
  `AnimGraphNode_Root_0`. A lookup by ID falls back to that name for every
  node.
- `blueprint_modify` looks a node up only inside one graph, which
  `graph_name` and `is_function_graph` select. A node in a function graph is
  "not found" unless you pass both (section `graphs`).

## Errors and failure modes

Load section `failures` when a call returns an error you cannot place, what
you see after a call is not what you expect, or a number or boolean you sent
seems not to have arrived: it has the errors common to both tools, how the
tools read numbers and booleans, and the failure table (what you see, what
it means, what to do). What it holds that shows no error (the first three
rest on the engine's JSON reads: read in the UE 5.8.4 source and not run,
while the toolkit targets UE 5.6 and 5.7):

- **An optional parameter the tool cannot read as the type it expects is not
  an error: the tool uses the default instead.** A number string that is
  only a sign or a dot, such as `"-"`, reads as 0. Send numbers and booleans
  as JSON numbers and booleans.
- **A boolean is read from any string and any number, so it never falls back
  to the default.** A typo reads as `false`, also where the default is
  `true`.
- **In a node spec of `add_nodes`, `pos_x`, `pos_y` and `num_outputs` read
  as 0** when the field is missing or not numeric; `num_outputs` below 2
  becomes 2.
- **After `add_node` fails with a compile error, no `node_id` came back, but
  the node was created.** `find_nodes` gets its ID; then fix or
  `delete_node` it.

## Background in the plugin

- `unreal_get_ue_context` with `category: "blueprint"` serves the plugin's
  UE API notes on Blueprint graphs. Its table of "MCP Blueprint Operations"
  names operations these tools do not have (`remove_node`, `set_pin_default`,
  `get_functions`, `get_graph_nodes`, `get_node_pins`, `find_references`). The
  operations in this skill come from the tools' own source.
- The plugin's `Resources/domains/blueprints.md` holds its operational notes:
  node IDs, targeted queries, building Animation Blueprints, `layout_graph`,
  CDO defaults.

## See also

- `ue-widgets`: `widget_editor`, the widget tree of a Widget Blueprint.
- `ue-enhanced-input`: the Input Action assets an `EnhancedInputAction` node
  refers to.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
