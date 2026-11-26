# Transitions and their conditions

Operations of `anim_blueprint_modify` (bridge name
`unreal_anim_blueprint_modify`) that create transitions between states and
build the logic in a transition's graph that decides when it fires. Every
call takes `blueprint_path` (the AnimBP's asset path) and `operation`; the
operations here also take `state_machine`.

A transition is named by `from_state` and `to_state`, matched ignoring case.
A miss returns `Transition from '<a>' to '<b>' not found`; a wrong state
machine name returns `State machine '<n>' not found. Available: <names>`,
except in `setup_transition_conditions`, where it shows up as
`No transitions found in state machine '<n>'` inside a successful result.
Every operation here except `inspect_node_pins` and the state mode of
`set_pin_default_value` first requires a target skeleton
(`AnimBlueprint has no target skeleton`).

Writes compile the AnimBP and save it when the compile succeeds, but they do
not report a failed compile: run `validate_blueprint` afterwards (section
`state-machines`). Results sit under the task envelope's `data` key; the
tool's message is `Operation completed` unless noted.

## Node IDs in a transition graph

Condition nodes this tool creates get IDs such as `Cond_Greater_4`,
`GetVar_Speed_5`, `Comp_Greater_6` or `And_Chain_7`, stored in the node's
comment as `MCP_ANIM_ID:<id>`. A lookup by ID also accepts an `MCP_ID:` that
`blueprint_query` wrote, and the node's UE object name. The exceptions are
named below. `blueprint_query`'s `find_nodes`, `get_graph_summary` and
`get_node` overwrite the `MCP_ANIM_ID:` of these nodes with a new `MCP_ID:`;
after such a query, use the ID it returned. `get_transition_nodes` shows the
IDs as they are now.

## Transitions

| Operation | Parameters | Result |
|---|---|---|
| `add_transition` | `state_machine`, `from_state`, `to_state` | `node_id` (`Transition_<from>_To_<to>_<n>`), `from_state`, `to_state`, `message` |
| `remove_transition` | the same | `success` |
| `set_transition_duration` | the same, `duration` (default 0.2) | `duration`, `message` |
| `set_transition_priority` | the same, `priority` (default 1, a whole number) | `priority`, `message` |

The three names are required:
`state_machine, from_state, and to_state parameters required`.

- **`add_transition` ignores `duration`**, although the tool declares it. Set
  the blend time afterwards with `set_transition_duration`. A new transition
  gets the engine's defaults: `CrossfadeDuration` 0.2 s and `PriorityOrder` 1
  (`AnimStateTransitionNode.cpp:84` and `:97` in the UE 5.8.4 source).
- Both ends must be states of that state machine. A second transition between
  the same two states returns
  `Transition from '<a>' to '<b>' already exists`. Other errors:
  `Failed to create transition node`, `Failed to create transition graph`.
- `set_transition_duration` sets the transition's `CrossfadeDuration` in
  seconds; `set_transition_priority` sets its `PriorityOrder`.
- **The lower number wins.** The tool's parameter text says
  "higher = checked first"; that is wrong. When several transitions out of a
  state are true in the same update, the one with the smallest priority order
  is taken (Epic, "Transition Rules"; the `PriorityOrder` comment in
  `AnimStateTransitionNode.h:32` of the UE 5.8.4 source). Give the transition
  that must win, such as the jump out of locomotion, the smaller number.
- `remove_transition` removes the transition and its graph.

## Reading a transition

### get_transition_nodes

Needs `state_machine` (`state_machine parameter required`); `from_state` and
`to_state` are optional.

- **Both omitted**: every transition of the state machine. Returns
  `state_machine`, `transition_count` and `transitions`, each with
  `from_state`, `to_state`, `transition_name` (`<from> -> <to>`) and `graph`.
- **Both given**: that transition's graph. Returns `graph_name`, `nodes`,
  `node_count`, `from_state`, `to_state`.
- A graph lists its `nodes`, each with `node_id` (`(unnamed)` when the node
  has no tool ID), `node_class`, `node_title`, `pos_x`, `pos_y`,
  `is_result_node`, `input_pins` and `output_pins`. A node without a tool ID
  shows as `(unnamed)`; `inspect_node_pins` can still find it by a part of
  its title.

### inspect_node_pins

Needs `state_machine` and `node_id`
(`state_machine and node_id parameters required`), plus either `state_name`
(a state's graph) or `from_state` and `to_state` (a transition's graph):
`Either state_name OR (from_state + to_state) required`. With both, the state
wins. No skeleton check.

`node_id` is looked up by ID; failing that, **the first node whose title
contains it**, ignoring case. A miss returns `Node '<id>' not found`. Returns
`node_id`, `node_class`, `node_title`, `pos_x`, `pos_y`, `input_pins`,
`output_pins`. Each pin has `name`, `direction`, `type`, `is_connected`,
`connection_count`, and only when set: `sub_type`, `default_value`,
`auto_default_value`, `connected_to` (each `node_id` and `pin_name`; the
`node_id` is empty for a node without a tool ID).

### set_pin_default_value

Needs `state_machine`, `node_id` and `pin_name`
(`state_machine, node_id, and pin_name parameters required`), plus
`state_name` or `from_state` and `to_state`, as for `inspect_node_pins`.
`pin_value` is the value as text; an absent `pin_value` sets an empty value.
Returns `node_id`, `pin_name`, `value`, `message`. The two modes differ:

| | `state_name` given | `from_state` and `to_state` given |
|---|---|---|
| skeleton check | no | yes |
| node lookup | ID, then a title containing `node_id` | **a node whose comment contains `node_id`**: the tool's IDs and `MCP_ID`s work, UE object names do not, and a short ID can match a longer one (`Cond_Less_1` also matches `Cond_Less_12`) |
| node missing | `Node '<id>' not found` | `Node not found: <id>` |
| pin missing | `Input pin '<p>' not found. Available: <pins>` | `Input pin '<p>' not found on node <id>. Available: <pins>` |
| value check | none | by pin type, below |

The transition mode checks the value first:
`Pin '<p>' expects bool value (true/false), got: <v>` (it accepts `true`,
`false`, `1`, `0`), `Pin '<p>' expects integer value, got: <v>`,
`Pin '<p>' expects integer value (no decimals), got: <v>`,
`Pin '<p>' expects float/double value, got: <v>`,
`Pin '<p>' is an exec pin, cannot set default value`. Both modes then hand
the value to the graph's schema. **The schema can refuse a value without an
error**; read the pin back with `inspect_node_pins` (`default_value`).

## Building conditions node by node

### add_condition_node

Needs `state_machine`, `from_state`, `to_state` and `node_type`
(`state_machine, from_state, to_state, and node_type parameters required`);
optional `node_params` and `position`. Returns `node_id`
(`Cond_<node_type>_<n>`, with `node_type` as you passed it), `node_type`,
`message`. The node is not connected.

| `node_type` (ignoring case) | Node |
|---|---|
| `TimeRemaining` | a getter for the time left in an animation, tied to the first sequence or blend space player in the source state's graph; set the source state's animation first, since without a player it is tied to none, and whether that compiles is up to the engine |
| `Greater`, `Less`, `GreaterEqual` / `greater_equal`, `LessEqual` / `less_equal`, `Equal`, `NotEqual` / `not_equal` | a float comparison with inputs `A` and `B` |
| `CompareFloat` / `compare_float` | a float comparison; `node_params.comparison` picks the operator (default `Less`) |
| `CompareBool` / `compare_bool` | a boolean equality |
| `And`, `Or`, `Not` | boolean logic |

Any other type returns
`Unknown condition node type: <t>. Supported: TimeRemaining, CompareFloat, CompareBool, Greater, Less, GreaterEqual, LessEqual, Equal, NotEqual, And, Or, Not`.
The tool's description also offers `GetVariable` with
`node_params.variable_name`; it is rejected with that error. A variable reaches
a transition through `add_comparison_chain`. `node_params` is read only for
`comparison`. Other errors: `Failed to create logic call function node`,
`Failed to create TimeRemaining getter node`.

### delete_condition_node

Needs `state_machine`, `from_state`, `to_state` and `node_id`
(`state_machine, from_state, to_state, and node_id parameters required`).
Returns `deleted_node_id`, `message`. Errors:
`Node with ID '<id>' not found in transition graph`,
`Cannot delete the transition result node`.

### connect_condition_nodes

Needs `state_machine`, `from_state`, `to_state`, `source_node_id` and
`target_node_id`
(`state_machine, from_state, to_state, source_node_id, and target_node_id required`).
`source_pin` and `target_pin` are optional in the call, but without a
`target_pin` no pin is found. The tool's own wiring uses `A` and `B` on
comparison and logic nodes; read the pin names with `inspect_node_pins`.

- Source pin: `source_pin` by name (ignoring case), else `ReturnValue`, else
  `Result`. Target pin: `target_pin` by name (ignoring case), nothing else.
  A miss returns `Cannot find <direction> <pin>. Available <direction> pins: [<a>] [<b>] `.
- Node errors: `Source node not found: <id>`, `Target node not found: <id>`.
- **The link is added without a type check and without breaking the target
  pin's existing link.** Read the result with `inspect_node_pins`.
- **A comparison target can be replaced.** When the target is a comparison
  node and the source is an integer, byte, enum or boolean pin while the
  comparison is of another type, the tool deletes the comparison and creates
  one of the source's type. The new node keeps `B`'s value and its output
  links, **loses its other input links, and has no tool ID**. Find it again
  with `get_transition_nodes` (it shows as `(unnamed)`), or avoid the case by
  building variable comparisons with `add_comparison_chain`. Error:
  `Failed to find target pin after recreating comparison node`.
- **A boolean source can delete the comparison.** When the source is a
  boolean pin and the comparison is not `Equal` or `NotEqual`, the tool
  deletes the comparison node, fails to create the boolean one, and still
  returns success. The failure goes only to the editor log, and the link goes
  to the node that was deleted. **The comparison node is gone.** Run
  `get_transition_nodes` after every connection to a comparison node, and wire
  booleans only to `Equal` or `NotEqual`.

### connect_to_result

Needs `state_machine`, `from_state`, `to_state` and `source_node_id`
(`state_machine, from_state, to_state, and source_node_id required`);
`source_pin` defaults to `Result`. Connects a boolean output to the
transition's result (the `bCanEnterTransition` input). Errors:
`Condition node not found: <id>`, `Transition result node not found`.

- Source pin: `source_pin`, else `ReturnValue`, `Result`, `Output`, else the
  first boolean output. When none of these is found, the call fails with
  `Cannot find output pin. Available output pins: [<a>] [<b>] `, or
  `Cannot find output pin. No output pins available on node.` The message
  says `pin` literally, not the name you passed.
- **The link is added without breaking the result's existing link.** Whether
  the engine then keeps one or both links is not something this skill has
  verified. To replace a condition, delete the old node first.

## Building conditions in one call

### add_comparison_chain

Needs `state_machine`, `from_state`, `to_state` and `variable_name`
(`state_machine, from_state, to_state, and variable_name parameters required`);
optional `comparison_type` (default `Less`), `compare_value` (text) and
`position`.

Builds a variable getter at `position`, a comparison 200 units to the right
with the variable in `A` and `compare_value` in `B`, and connects the
comparison to the transition result. **When the result is already connected,
it adds an AND node** (400 units to the right) of the existing condition and
the new one. Calling it twice therefore requires both comparisons.

- The variable is looked up on the AnimBP's class:
  `Variable '<v>' not found in AnimBlueprint`. Add the variable first with
  `blueprint_modify add_variable` (skill `ue-blueprints`).
- The comparison takes the variable's type: boolean, integer, byte, enum, or
  float for everything else. `comparison_type` is `Greater`, `Less`,
  `GreaterEqual`, `LessEqual`, `Equal` or `NotEqual`, ignoring case.
  Booleans allow only `Equal` and `NotEqual`:
  `Boolean variables only support Equal/NotEqual comparisons, not '<c>'.`
  Other errors: `Unknown comparison type: <c>`,
  `Unknown comparison type for enum/byte: <c>`,
  `Failed to create comparison call function node`.
- **A comparison error leaves the getter behind.** The variable getter is
  added before the comparison is created. When the comparison then fails
  (an unknown `comparison_type`, or a boolean with anything but `Equal` or
  `NotEqual`), the getter stays in the transition graph, unconnected. The
  AnimBP is neither compiled nor saved, and the error carries no node ID. Find
  the getter with `get_transition_nodes` (an ID starting with `GetVar_`) and
  remove it with `delete_condition_node`. The same happens for such a condition
  inside `batch` and `setup_transition_conditions`. Both compile at the end and
  save when that compile succeeds, so there the stray getter is saved too.
- `compare_value` is handed to the schema without a type check and is
  skipped when empty. Check it with `inspect_node_pins` (pin `B`).
- The message is `Comparison chain created successfully`. `data` holds
  `variable_node_id`, `comparison_node_id`, `chained_with_existing`,
  `and_node_id` (only when an AND node was added), `variable`, `comparison`,
  `value`.

### setup_transition_conditions

Applies condition rules to every transition whose states match a pattern.
Needs `state_machine` (`state_machine parameter required`) and a `rules`
array (`rules array required for setup_transition_conditions`). Each rule is
`{"match": {"from": <pattern>, "to": <pattern>}, "conditions": [...], "logic": "AND"}`.

- **Patterns.** A string that starts with `^`, ends with `$`, or contains
  `\d`, `\w`, `[`, `+`, `?`, or both `.` and `*` is tried as a regular
  expression (case-sensitive) first. Otherwise `*` matches every state,
  `Walk*` a prefix and `*Walk` a suffix, ignoring case. `*Walk*` is read as
  the prefix `*Walk` and matches no state, so do not use it; use a regular
  expression such as `.*Walk.*` instead. Anything else must equal the state
  name, ignoring case. An array of strings matches any of them exactly,
  ignoring case.
- **Conditions.** `{"variable": "<v>", "comparison": "<c>", "value": "<x>"}`
  builds an `add_comparison_chain`; note the field is `comparison`, not
  `comparison_type`. `{"type": "TimeRemaining", "comparison": "<node type>", "value": "<x>"}`
  builds a `TimeRemaining` node and a comparison of the `node_type` named by
  `comparison` (such as `Less`); without `comparison` it fails with the unknown
  node type error. An entry with neither returns
  `Condition must have 'type' or 'variable'`.
- **Wiring is unreliable.** Variable conditions connect themselves to the
  result and AND with what is there. A `TimeRemaining` condition is connected
  to the result only under `"logic": "OR"` with more than one condition.
  `OR` adds its OR nodes' link to the result on top of the links the variable
  conditions already made. Connection failures are not reported. **Every run
  adds new nodes**; a second run duplicates the conditions. Read every
  matched transition back with `get_transition_nodes`.
- **Success is reported even on failure.** When anything failed, the call
  still succeeds. Its message is then the error for a missing skeleton or for
  a state machine without transitions (or a misspelt state machine name), and
  `Unknown error setting up transition conditions` for every failure inside a
  rule, and also when every rule worked but the final compile failed
  (`compiled: false`, `compile_error`). Otherwise the message is
  `Setup transition conditions: <n> rules processed, 0 transitions modified`;
  the second number is always 0.
- `data` holds `success` (no failure and the compile succeeded),
  `total_transitions_in_machine`, `matched_transitions`, `success_count`,
  `failure_count`, `rules_processed`, `compiled`, `compile_error` when the
  compile failed, and `results`: per rule `rule_index`, `matched_count` and
  `transitions` (each `from_state`, `to_state`, `success`, `conditions`), or
  `rule_index`, `success: false` and `error`.
- Rule errors: `Invalid rule format`, `Rule missing 'match' field`,
  `Rule match missing 'from' or 'to' patterns`,
  `Rule missing 'conditions' array`. A state machine without transitions:
  `No transitions found in state machine '<n>'`.

The plugin's domain notes call parts of this operation broken. Prefer
`add_comparison_chain` per transition, or `batch` (section `state-machines`).
