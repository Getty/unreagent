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

This skill has three sections:

| Section | Covers |
|---|---|
| `query` | every `blueprint_query` operation: parameters, results, errors |
| `modify` | `create`, `reparent`, variables, functions, components, defaults, `compile`, `compile_all`, debug prints |
| `graphs` | graph nodes and wiring: `add_node`, `add_nodes`, `delete_node`, `connect_pins`, `disconnect_pins`, `set_pin_value`, `layout_graph` |

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
  package is not marked as modified, so nothing is saved unless something else
  saves it.
- Animation graph nodes are addressed by their UE object name, for example
  `AnimGraphNode_Root_0`. A lookup by ID falls back to that name for every
  node.
- `blueprint_modify` looks a node up only inside one graph, which
  `graph_name` and `is_function_graph` select. A node in a function graph is
  "not found" unless you pass both (section `graphs`).

## Errors common to both tools

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | the parameter is absent or an empty string |
| `Unknown operation: '<op>'. Valid: …` | misspelt operation |
| `Cannot access engine or script Blueprints` | `/Engine/` or `/Script/` path |
| `Could not load Blueprint: <path>` | no Blueprint at that path |
| `Cannot modify engine Blueprints` / `Blueprint package is read-only (cooked)` | `blueprint_modify` only |
| `Blueprint '<n>' has no GeneratedClass — compile it first` (`get_defaults`), `Blueprint has no GeneratedClass — compile it first` (`set_cdo_default`) | the Blueprint was never compiled: run `blueprint_modify compile` |
| `<op> succeeded but compilation failed: …` | the edit was applied, but the Blueprint does not compile |

An optional parameter the tool cannot read as the type it expects is not an
error: the tool uses the default instead. Whether a numeric or boolean string,
such as `"5"` or `"true"`, can be read as a number or a boolean depends on the
engine's JSON classes, which this skill has not verified. Send numbers and
booleans as JSON numbers and booleans.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a change is gone after an editor restart | it was never saved | save with `asset save_asset` after each finished change |
| `Node '<id>' not found` for an ID you just used | the node is in another graph | pass the same `graph_name` and `is_function_graph` as when you created it |
| a node's comment was replaced by `MCP_ID:…` | a query assigned it an ID | expected; the comment text is gone |
| `set_pin_value` succeeded, but `get_node` shows the old value | the pin did not accept that string | use the exact `default_value` format `get_node` reports for that pin type |
| after `add_nodes`, some pins are not wired | that connection failed, and `add_nodes` skips failed connections silently | `get_node_connections`, then `connect_pins`, which reports why it fails |
| `add_node` fails with a compile error, and no `node_id` came back | the node was created, but the Blueprint does not compile | `find_nodes` to get its ID, then fix or `delete_node` it |
| `set_cdo_default` succeeded, but actors already in the level are unchanged | placed actors keep the values serialized in the level (the plugin's domain notes) | set the property on the level actor as well |

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
