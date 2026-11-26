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
Layer Interfaces. **`blueprint_query`** reads AnimBPs as well (section
`verify`). Through the plugin's MCP bridge the two tools are listed as
`unreal_anim_blueprint_modify` and `unreal_blueprint_query`. They work only
while the editor is running and the plugin is reachable; `unreagent-iterate`
covers the wait for the plugin.

This skill was written from the toolkit's C++ source and, for engine facts,
from Epic's documentation and the UE 5.8.4 source. It has not been exercised
against a running editor.

The tool takes an `operation` string and lowercases it before dispatch.
`inspect` and `info` run `get_info`; `get_graph` and `list_states` run
`get_state_machine`; `get_diagram` runs `get_state_machine_diagram`. An unknown
operation returns `Unknown operation: '<op>'. Valid: <list>`, plus
` Did you mean '<x>'?` when a valid name is close. The tool does not create,
duplicate or delete an AnimBP (see "Creating an AnimBP").

## Sections

No operation's parameters are in this file: load the section that covers an
operation before you call it.

| Section | What's in it | Load it |
|---|---|---|
| `locomotion` | the end-to-end build: variables, a locomotion state machine with a blend space, jump states, transition conditions, a montage slot, verification | before you build or extend a character's AnimBP |
| `state-machines` | `get_info`, `get_state_machine`, `get_state_machine_diagram`, `validate_blueprint`, `create_state_machine`, `connect_state_machine_to_output`, `add_state`, `remove_state`, `set_entry_state`, `set_state_animation`, `batch` | before you read state machines, create or change one or a state, validate, or run `batch` |
| `transitions` | `add_transition`, `remove_transition`, `set_transition_duration`, `set_transition_priority`, `get_transition_nodes`, `inspect_node_pins`, `set_pin_default_value`, `add_condition_node`, `delete_condition_node`, `connect_condition_nodes`, `connect_to_result`, `add_comparison_chain`, `setup_transition_conditions` | before you add, remove or time a transition, or read or build its condition logic |
| `anim-nodes` | `add_anim_node` (players, slot, blends, bone controls, Control Rig), `delete_anim_node`, `connect_anim_nodes`, `bind_variable`, `get_anim_node_property`, `set_anim_node_property`, `layout_graph`, `find_animations` | before you add, delete, connect, bind or set an animation node, lay out a graph, or search animations |
| `layers` | `list_layer_interfaces`, `list_layers`, `list_linked_layer_nodes`, `add_layer_interface`, `add_linked_layer_node`, `set_layer_instance`, `inspect_layer_graph` | before you read or change layer interfaces, layers or linked layer nodes, or pass `target_graph` |
| `verify` | how a result reaches you through the bridge, `blueprint_query` on AnimBPs, which read answers which question, the check after each kind of change | after every change, and before you report an AnimBP as done |
| `background` | how the engine runs an AnimBP (EventGraph, AnimGraph, thread-safe update), transition priority and timing, slots, cached poses, linked layers, and what this tool cannot build; each with its Epic source | when you choose how to structure a graph, or a feature seems missing from the tool |
| `failures` | errors common to every operation, parameter handling, and the failure table (what you see, what it means, what to do) | when a call returns an error you cannot place, or the result is not what you expected |

## Must not get wrong

- **The state machine name you pass is not applied.** `create_state_machine`
  leaves a generated name (such as `AnimationStateMachineGraph_1`). Read it
  with `get_info` right after and pass that name as `state_machine`; the
  returned `node_id` still works for `connect_anim_nodes`.
- **The smaller transition priority wins**, although the tool's parameter
  text says "higher = checked first" (Epic, "Transition Rules").
- **A write can succeed while the AnimBP no longer compiles**, and then it is
  not saved. Run `validate_blueprint` after changes and read `status`.
- **A montage plays only through a slot node** of its slot in the AnimGraph;
  a state cannot play a montage (`set_state_animation` with `montage` always
  fails).
- **Variables must exist and be written.** Conditions and bindings need the
  variable on the AnimBP (`blueprint_modify add_variable`), and something,
  usually the EventGraph's update event, must set it every frame; this tool
  builds neither.
- **Every asset must use the AnimBP's target skeleton**, and most writes need
  that skeleton set.

## Paths and validation

Every operation needs `blueprint_path`. When it is absent, `asset_path` or
`path` stands in for it. An absent or empty value returns
`Missing required parameter: blueprint_path`; the path is checked before
`operation` is read.

- **The path is validated before anything loads.** These are rejected: more
  than 512 characters; `/Engine/` and `/Script/` paths
  (`Cannot access engine or script Blueprints`); `..`; any of the characters
  `` <>|&;`$(){}[]!*?~ ``. Section `failures` has the messages.
- **Use the asset path**, such as `/Game/Characters/ABP_Hero`, not the
  generated class (`_C`). If loading it as given fails, the tool tries once
  more with `/Game/` in front (when the path has no leading `/`) and
  `.<asset name>` appended (when it has no `.`).
- **A path that does not load** returns
  `Failed to load Animation Blueprint: <path>`; a Blueprint of another kind
  can instead give `Asset is not an Animation Blueprint: <path>`. Neither
  proves the AnimBP is missing: search first with `blueprint_query list` and
  `type_filter: "AnimInstance"` (section `verify`), or `asset_search` (skill
  `ue-assets`).

## The target skeleton

These operations fail with `AnimBlueprint has no target skeleton` when the
AnimBP has none: `add_state`, `remove_state`, every transition and condition
operation except `inspect_node_pins` and state-mode `set_pin_default_value`,
`set_state_animation`, `get_state_machine_diagram`, and
`create_state_machine` without `target_graph`. `batch` and
`setup_transition_conditions` stop on it inside a successful result.
`validate_blueprint` reports it as an issue. The plugin's domain notes set
the skeleton with the `asset` tool's `set_asset_property` on
`TargetSkeleton` (skill `ue-assets`); this skill has not traced that call.

## Creating an AnimBP

`anim_blueprint_modify` has no create operation. Create the AnimBP with
`blueprint_modify create` and `parent_class` `AnimInstance` (skill
`ue-blueprints`, section `modify`), set the skeleton, add the variables with
`blueprint_modify add_variable`, then build the graphs with this tool;
section `locomotion` walks through it. The plugin's domain notes say never to
duplicate an AnimBP; build a new one instead.

## Graphs and names

- **State machines, states and transitions are found by name, ignoring case.**
  A miss lists what exists: `State machine '<n>' not found. Available: <names>`,
  `State '<n>' not found. Available: <names>`,
  `Transition from '<a>' to '<b>' not found`.
- **The graph a node operation works in**: `state_machine` with `state_name`
  for a state's own graph, `target_graph` for a layer function graph, else
  the AnimGraph. When both are given, the state wins. Without them, nodes in
  a state's graph are not found.
- **The AnimGraph** is the first animation graph of the AnimBP. Without one the
  operations that need it fail with `Animation Blueprint has no AnimGraph`.

## Node IDs

- Nodes this tool creates carry an ID such as `State_Idle_3`,
  `Cond_Greater_12` or `Anim_Walk_7`, stored in the node's comment as
  `MCP_ANIM_ID:<id>`; it **replaces any comment the node had**. Results that
  create a node return it as `node_id`, or as `variable_node_id` from
  `bind_variable` and `add_comparison_chain`. `set_state_animation` does not
  return its player's ID (section `locomotion`, "Finding a node you did not
  create").
- The trailing number comes from a counter that restarts with the editor:
  read IDs back rather than predicting them.
- A lookup by ID tries `MCP_ANIM_ID:`, then `MCP_ID:` (written by
  `blueprint_query`), then the node's UE object name, such as
  `AnimGraphNode_Root_0`. Some operations also accept a substring of the
  node's title; each section says which.
- **`blueprint_query` can replace these IDs.** Its `find_nodes`,
  `get_graph_summary` and `get_node` write a new `MCP_ID:` over the
  `MCP_ANIM_ID:` of Blueprint graph nodes without one, which hits condition
  and variable nodes in transition graphs. Afterwards use the `MCP_ID` it
  returned. Animation nodes keep their comment.

## The edit loop

1. **Find the AnimBP** with `blueprint_query list` or `asset_search`.
2. **Read before you write**: `get_info`, `get_state_machine`,
   `get_transition_nodes`, `list_layers` (section `verify` says which read
   answers what).
3. **Change one thing.** Every write compiles the AnimBP and saves it when
   the compile succeeds. `batch` and `setup_transition_conditions` apply
   several changes and compile once.
4. **Check the compile** with `validate_blueprint` (`status`), and for the
   compiler's messages `blueprint_modify compile` (skill `ue-blueprints`).
5. **Verify what you wired**: several links are made without a schema check
   and some reports omit failures. Section `verify` has the check per change.

## Saving and undo

- **A successful compile saves the asset**; a failed save is only logged.
- **A failed compile saves nothing.** The edit stays in the editor's copy and
  is lost on an editor stop unless you fix the AnimBP or save it yourself:
  `asset` with `operation: "save_asset"` (skill `ue-assets`).
- **Not saved by this tool:** `validate_blueprint` (it compiles only) and
  `layout_graph` in a state's graph (it only marks the asset as modified).
- **No undo.** No operation opens an editor transaction. Undo a change with
  the inverse operation: `remove_state`, `remove_transition`,
  `delete_condition_node`, `delete_anim_node`. `batch` is not atomic.

## Background in the plugin

- The plugin's `Resources/domains/blueprints.md` holds its notes on
  Animation Blueprints: the build order, the layer setup, state machine
  names. Where it disagrees with this skill, this skill follows the source.
- `unreal_get_ue_context` with `category: "animation"` serves the plugin's
  animation notes (skill `ue-context`).

## See also

- `ue-blueprints`: `blueprint_query` and `blueprint_modify`; creating the
  AnimBP, its variables, the EventGraph, compiling with messages.
- `ue-assets`: `asset_search`, saving, setting the target skeleton.
- `ue-anim-sequence`, `ue-blend-space`, `ue-anim-montage`: the assets a state
  or an animation node plays; `ue-retarget` for assets on another skeleton.
- `ue-control-rig`: the rig a `control_rig` node runs.
- `ue-tasks`: the task queue behind every call.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
