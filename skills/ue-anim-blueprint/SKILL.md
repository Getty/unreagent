---
name: ue-anim-blueprint
description: >
  Build and edit Animation Blueprints with anim_blueprint_modify: state machines,
  transitions and their conditions, anim graph nodes, bindings, layers. Use before
  you change an AnimBP's graphs, or when an anim_blueprint_modify call fails.
toolkit_tools: [anim_blueprint_modify, blueprint_query]
---

# UE Animation Blueprints: anim_blueprint_modify

The UE LLM Toolkit plugin's **`anim_blueprint_modify`** tool reads and edits the
graphs of an Animation Blueprint (AnimBP): state machines, their states and
transitions, the condition logic inside a transition, animation nodes in the
AnimGraph, in a state or in a layer graph, variable bindings, and Animation
Layer Interfaces. **`blueprint_query`** reads AnimBPs as well (below). Through
the plugin's MCP bridge the two tools are listed as
`unreal_anim_blueprint_modify` and `unreal_blueprint_query`. They work only
while the editor is running and the plugin is reachable; `unreagent-iterate`
covers the wait for the plugin.

This skill was written from the toolkit's C++ source. It has not been exercised against a running editor.

The tool takes an `operation` string and lowercases it before dispatch.
`inspect` and `info` run `get_info`; `get_graph` and `list_states` run
`get_state_machine`; `get_diagram` runs `get_state_machine_diagram`. An unknown
operation returns `Unknown operation: '<op>'. Valid: <list>`, plus
` Did you mean '<x>'?` when a valid name is close. The tool does not create,
duplicate or delete an AnimBP (see "Creating an AnimBP").

This skill has five sections. No operation's parameters are in this file:
load the section that covers an operation before you call it. Each holds its
operations' parameters, results and errors, and what a success of each does
not prove.

| Section | Covers | Load it |
|---|---|---|
| `state-machines` | `get_info`, `get_state_machine`, `get_state_machine_diagram`, `validate_blueprint`, `create_state_machine`, `connect_state_machine_to_output`, `add_state`, `remove_state`, `set_entry_state`, `set_state_animation`, `batch` | before you read an AnimBP's state machines, create or change a state machine or a state, validate, or run `batch` |
| `transitions` | `add_transition`, `remove_transition`, `set_transition_duration`, `set_transition_priority`, `get_transition_nodes`, `inspect_node_pins`, `set_pin_default_value`, `add_condition_node`, `delete_condition_node`, `connect_condition_nodes`, `connect_to_result`, `add_comparison_chain`, `setup_transition_conditions` | before you add, remove or time a transition, or read or build the condition logic inside one |
| `anim-nodes` | `add_anim_node`, `delete_anim_node`, `connect_anim_nodes`, `bind_variable`, `get_anim_node_property`, `set_anim_node_property`, `layout_graph`, `find_animations` | before you add, delete, connect, bind or set an animation node, lay out a graph, or search animations with this tool |
| `layers` | `list_layer_interfaces`, `list_layers`, `list_linked_layer_nodes`, `add_layer_interface`, `add_linked_layer_node`, `set_layer_instance`, `inspect_layer_graph` | before you read or change layer interfaces, layers or linked layer nodes, or pass `target_graph` |
| `failures` | the errors common to every operation, and the failure table (what you see, what it means, what to do) | when a call returns an error you cannot place, or what you see after a call is not what you expect |

## Paths and validation

Every operation needs `blueprint_path`. When it is absent, `asset_path` or
`path` stands in for it. An absent or empty value returns
`Missing required parameter: blueprint_path`; the path is checked before
`operation` is read.

- **The path is validated before anything loads.** These are rejected:
  - more than 512 characters:
    `Blueprint path exceeds maximum length of 512 characters`;
  - `/Engine/` and `/Script/` paths: `Cannot access engine or script Blueprints`;
  - `..`: `Blueprint path cannot contain path traversal sequences`;
  - any of the characters `` <>|&;`$(){}[]!*?~ ``:
    `Blueprint path contains invalid character: '<c>'`.
- **Use the asset path**, such as `/Game/Characters/ABP_Hero`, not the
  generated class (`_C`). The path is loaded as given. If that fails, the tool
  tries once more with `/Game/` in front (when the path has no leading `/`) and
  `.<asset name>` appended (when it has no `.`).
- **A path that does not load** returns
  `Failed to load Animation Blueprint: <path>`. A Blueprint of another kind can
  instead give `Asset is not an Animation Blueprint: <path>`; which of the two
  you get is up to the engine's loader.
- **A failed load does not prove the AnimBP is missing.** Search before you
  create one: `blueprint_query list` with `type_filter: "AnimInstance"` (below),
  or the `asset_search` tool (skill `ue-assets`).

## The target skeleton

These operations first check that the AnimBP has a target skeleton and fail
with `AnimBlueprint has no target skeleton` when it has none: `add_state`,
`remove_state`, every transition and condition operation except
`inspect_node_pins` and state-mode `set_pin_default_value`,
`set_state_animation`, `get_state_machine_diagram`, and
`create_state_machine` without `target_graph`. `batch` and
`setup_transition_conditions` stop on it as well, but inside a successful
result (sections `state-machines` and `transitions`). `validate_blueprint`
reports a missing skeleton as an issue instead. The plugin's domain notes set it with the
`asset` tool's `set_asset_property` on `TargetSkeleton` (skill `ue-assets`);
this skill has not traced that call.

## Creating an AnimBP

`anim_blueprint_modify` has no create operation. The plugin's domain notes
create an AnimBP with `blueprint_modify create` and `parent_class` `AnimInstance`
(skill `ue-blueprints`, section `modify`), then set the skeleton, add the
layer interface, add variables with `blueprint_modify`, and only then build the
graphs with this tool. The same notes say never to duplicate an AnimBP; build
a new one instead.

## Graphs and names

- **State machines, states and transitions are found by name, ignoring case.**
  A miss lists what exists: `State machine '<n>' not found. Available: <names>`,
  `State '<n>' not found. Available: <names>`,
  `Transition from '<a>' to '<b>' not found`.
- **The graph an operation works in.** The node operations of section
  `anim-nodes` take `state_machine` with `state_name` for a state's own graph,
  `target_graph` for a layer function graph, and otherwise use the AnimGraph.
  When both are given, `state_machine` with `state_name` wins. Section `layers`
  lists where `target_graph` is read.
- **The AnimGraph** is the first animation graph of the AnimBP. Without one the
  operations that need it fail with `Animation Blueprint has no AnimGraph`.

## Node IDs

- Nodes this tool creates carry an ID such as `State_Idle_3`,
  `Cond_Greater_12` or `Anim_Walk_7`. It is stored in the node's comment as
  `MCP_ANIM_ID:<id>` and **replaces any comment the node had**. Every result
  that creates a node returns the ID: as `node_id`, or as `variable_node_id`
  from `bind_variable` and `add_comparison_chain`. A state machine's ID is
  `StateMachine_<name>`, with spaces turned into `_`.
- The trailing number comes from a counter that restarts with the editor, so
  IDs are unique within a graph, not stable names. Read them back rather than
  predicting them.
- A lookup by ID tries `MCP_ANIM_ID:`, then `MCP_ID:` (written by
  `blueprint_query`), then the node's UE object name, such as
  `AnimGraphNode_Root_0`. Some operations also accept a substring of the node's
  title; each section says which.
- **`blueprint_query` can replace these IDs.** Its `find_nodes`,
  `get_graph_summary` and `get_node` give every Blueprint graph node without an
  `MCP_ID:` a new one, written into the comment over the `MCP_ANIM_ID:`. That
  hits the condition and variable nodes in transition graphs. Afterwards the
  old ID no longer resolves; use the `MCP_ID` that `blueprint_query` returned.
  Animation nodes (players, blends, state machines) keep their comment; the
  query reports their UE object name.

## The edit loop

1. **Find the AnimBP** with `blueprint_query list` (below) or `asset_search`.
2. **Read before you write.** `get_info` lists the state machines and their
   states; `get_state_machine` adds positions and transitions;
   `get_transition_nodes` and `inspect_node_pins` show a transition's logic;
   `list_layers` and `inspect_layer_graph` show the layers.
3. **Change one thing.** Every write compiles the AnimBP and, when the compile
   succeeds, saves it (below). `batch` and `setup_transition_conditions` apply
   several changes and compile once.
4. **Check the compile.** Single operations do not report it: the call
   succeeds even when the AnimBP no longer compiles. Run `validate_blueprint`
   after a change; its `status` is `Error` when the compile failed. For the
   compiler's messages run `blueprint_modify compile` on the same path (skill
   `ue-blueprints`, section `modify`).
5. **Verify what you wired.** Several connections are made without a schema
   check and some reports omit failures (sections `transitions` and
   `anim-nodes`). Read the graph back with `get_transition_nodes`,
   `inspect_node_pins` or `inspect_layer_graph`. The summaries of
   `blueprint_query get_anim_graph` and `get_state_machine_detail` mark an
   unlinked data input of a pose node as `[UNWIRED=<value>]`.

## Saving and undo

- **A successful compile saves the asset.** The write operations compile
  through one helper that saves the AnimBP afterwards. A failed save is only
  logged.
- **A failed compile saves nothing.** The edit stays in the editor's copy and
  is lost on an editor stop unless you fix the AnimBP or save it yourself:
  `asset` with `operation: "save_asset"` (skill `ue-assets`).
- **Not saved by this tool:** `validate_blueprint` (it compiles only) and
  `layout_graph` in a state's graph (it only marks the asset as modified).
- **No undo.** No operation opens an editor transaction. Undo a change with the
  inverse operation: `remove_state`, `remove_transition`,
  `delete_condition_node`, `delete_anim_node`. `batch` is not atomic: the
  operations that succeeded stay applied when a later one fails.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools. Most operations return the message
  `Operation completed`; the descriptive text, where there is one, is a
  `message` field under `data`.
- **Failure** is only `Error: <message>`. The data of a failed call does not
  reach you.
- **Two operations report failure as success.** `batch` and
  `setup_transition_conditions` always return success once their input is
  readable; their `data.success` and per-item `results` say what failed
  (section `state-machines` and `transitions`).
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the AnimBP back before you retry (skill `ue-tasks`).
  `find_animations` with a skeleton filter (section `anim-nodes`) loads every
  candidate asset in one call and can hold the editor for more than 30 s on a
  large project, and a request that waits 30 s behind it may crash the
  editor; run it as skill `ue-tasks`, "A 30 s timeout can crash the editor",
  describes.

## Reading with blueprint_query

`blueprint_query` covers AnimBPs with these operations; skill `ue-blueprints`,
section `query`, documents their parameters and results.

- **`list`**: `type_filter` is matched, ignoring case, as a substring of the
  parent class path that the asset registry records for the Blueprint. An
  AnimBP's parent is `AnimInstance` or a class derived from it, so
  `type_filter: "AnimInstance"` finds the AnimBPs whose parent class path
  contains it. The tool's own example `AnimBlueprint` names the asset type,
  not a parent class. An AnimBP derived from another AnimBP has that AnimBP's
  generated class as its parent; filter by that AnimBP's name, or by
  `name_filter` and `path_filter`.
- **`get_anim_graph`** summarizes the AnimGraph and the layer graphs; an
  optional `graph_name` picks one by name, ignoring case.
- **`get_state_machine_detail`** lists each state machine's states and
  transitions; an optional `state_machine_name` picks one by name, ignoring case.
  A name that matches nothing is not an error: the summary says
  `(no state machines found)`.
- **`find_nodes`** and **`get_graph_summary`** take `graph_type`
  (`EventGraph`, `AnimGraph`, `StateMachine`, `All`). Their node IDs can
  replace this tool's IDs (see "Node IDs").

## Errors common to every operation

The table of these errors and their causes is in section `failures`.

The operations' own checks treat a parameter that is present but empty as
missing. A parameter the tool cannot read as the type it expects falls back to
its default; send numbers and booleans as JSON numbers and booleans.

For a parameter the tool does not declare, the plugin's registry appends a
warning that starts `WARNING: Unknown parameter` to a call it runs directly.
Calls the bridge queues, the default, get no such warning (skill `ue-tasks`),
so a misspelt parameter is silently ignored. The tool still reads
`asset_path`, `path` and `use_attached_parent` (section `anim-nodes`) although
it does not declare them.

## Failure modes

Load section `failures` when a call returns an error you cannot place, or
what you see after a call is not what you expect: it has the errors common
to every operation and the failure table (what you see, what it means, what
to do). One of its rows is on the call the edit loop above has you make:
`validate_blueprint` can say `0 error(s)` while `is_valid` is false. The
compiler's errors are not counted, only the tool's own checks. Read
`status`.

## Background in the plugin

- The plugin's `Resources/domains/blueprints.md` holds its operational notes
  on Animation Blueprints: the build order, the layer setup, state machine
  names. Where it disagrees with this skill, this skill follows the source.
- `unreal_get_ue_context` with `category: "animation"` serves the plugin's
  animation notes, including a partial table of this tool's operations (skill
  `ue-context`).

## See also

- `ue-blueprints`: `blueprint_query` and `blueprint_modify`; creating the
  AnimBP, its variables, compiling with messages.
- `ue-assets`: `asset_search`, saving, setting the target skeleton.
- `ue-anim-sequence`, `ue-blend-space`, `ue-anim-montage`: the assets a state
  or an animation node plays.
- `ue-tasks`: the task queue behind every call.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
