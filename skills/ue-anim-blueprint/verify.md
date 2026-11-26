# Reading results back and verifying a change

A successful `anim_blueprint_modify` call proves less than it seems: single
writes do not report a failed compile, several links are made without a
schema check, and `batch` and `setup_transition_conditions` report failure as
success. This section says how a result reaches you, which read tells you
what, and what to check after each kind of change.

## How a result reaches you

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
  readable; their `data.success` and per-item `results` say what failed.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the AnimBP back before you retry (skill `ue-tasks`).
  `find_animations` with a skeleton filter loads every candidate asset in one
  call and can hold the editor for more than 30 s on a large project, and a
  request that waits 30 s behind it may crash the editor; run it as skill
  `ue-tasks`, "A 30 s timeout can crash the editor", describes.

## Which read answers what

| Question | Read |
|---|---|
| Does it compile? | `validate_blueprint` → `status` (`Error` means no); messages from `blueprint_modify compile` (skill `ue-blueprints`) |
| Which state machines and states exist, under which names? | `get_info` |
| Positions, transitions, durations, priorities? | `get_state_machine` |
| Entry state, each state's animation, a condition summary per transition? | `get_state_machine_diagram` (`enhanced_info`) |
| What logic sits in one transition? | `get_transition_nodes` with `from_state` and `to_state`; `inspect_node_pins` on one node |
| A transition's rule as one expression, every state machine at once? | `blueprint_query get_state_machine_detail` |
| The pose chain from the output back to the players? | `blueprint_query get_anim_graph` |
| A node's settings (play rate, looping)? | `get_anim_node_property` without `property_name` |
| Which layers exist and which nodes call them? | `list_layers`, `list_linked_layer_nodes`, `inspect_layer_graph` |

## blueprint_query on an AnimBP

`blueprint_query` covers AnimBPs with these operations; skill
`ue-blueprints`, section `query`, documents their parameters and results.

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
  transitions with their rule expressions; an optional `state_machine_name`
  picks one by name, ignoring case. A name that matches nothing is not an
  error: the summary says `(no state machines found)`.
- Both summaries mark a data input of a pose node that nothing is linked to
  as `[UNWIRED=<value>]`: a blend space player whose `X` shows it is not
  driven by a variable.
- **`find_nodes`** and **`get_graph_summary`** take `graph_type`
  (`EventGraph`, `AnimGraph`, `StateMachine`, `All`). Their node IDs can
  replace this tool's IDs in transition graphs (skill, "Node IDs").

## What to check after each kind of change

| Change | Check |
|---|---|
| `create_state_machine` | `get_info`: the real name (it is not the one you passed) |
| states, entry | `get_state_machine_diagram`: `is_entry`, the state list |
| `set_state_animation` | the diagram's `animation`; for a blend space, `get_state_machine_detail` shows no `UNWIRED` on the bound axis |
| transitions | `get_state_machine`: `from_state`, `to_state`, `duration`, `priority` |
| conditions | `get_transition_nodes` for that transition: each comparison's `A` linked, `B` set, the result linked; after `connect_condition_nodes` to a comparison, the comparison is still there |
| AnimGraph wiring | `blueprint_query get_anim_graph`: output ← slot ← state machine (or whatever chain you built) |
| node settings | `get_anim_node_property` with the `property_name` you set |
| layers | `list_linked_layer_nodes`: `layer_name`, `instance_class`, `connected_to_output` |
| anything | `validate_blueprint` `status` last; a write is saved only when its compile succeeded |

What these reads cannot show is how the character looks in motion. For that,
run PIE (skill `ue-pie-testing`) and capture the viewport (skill
`ue-viewport`); `blueprint_modify add_debug_print` (skill `ue-blueprints`)
logs the variables the graph reads.
