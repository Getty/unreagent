# State machines and states

Operations of `anim_blueprint_modify` (bridge name
`unreal_anim_blueprint_modify`) that read an Animation Blueprint, build state
machines and their states, and assign the animation a state plays. Every call
takes `blueprint_path` (the AnimBP's asset path, such as
`/Game/Characters/ABP_Hero`) and `operation`.

The tool loads the AnimBP before it checks the other parameters, so a load
error comes first. State machine and state names are matched ignoring case. A
miss returns `State machine '<n>' not found. Available: <names>` or
`State '<n>' not found. Available: <names>`.

Writes compile the AnimBP and save it when the compile succeeds. They do not
report a failed compile: run `validate_blueprint` afterwards. Results sit under
the task envelope's `data` key; the tool's message is `Operation completed`
unless noted.

## Reading

### get_info (aliases `inspect`, `info`)

No further parameters, no skeleton check. Returns `name`, `path`,
`state_machines` (each `name`, `node_id`, `states` as a list of names), and
`skeleton` and `skeleton_path` only when the AnimBP has a target skeleton.

### get_state_machine (aliases `get_graph`, `list_states`)

Needs `state_machine` (`state_machine parameter required`). Returns `name`,
`node_id`, `states` (each `name`, `node_id`, `pos_x`, `pos_y`) and
`transitions` (each `node_id`, `duration`, `priority`, and `from_state` and
`to_state`, each only when the transition is linked to that state).

### get_state_machine_diagram (alias `get_diagram`)

Needs `state_machine` and a target skeleton. **The tool's message is the
diagram itself**, an ASCII drawing of the states and transitions. `data` holds
`state_machine`, `ascii_diagram` (the same text) and `enhanced_info`: `states`
(each `name`, `grid_x`, `grid_y`, `is_entry`, and `animation` when the state
plays one) and `transitions` (each `from`, `to`, `condition_summary`,
`duration`). A failure without its own message returns
`Failed to generate diagram`.

### validate_blueprint

No further parameters. Compiles the AnimBP and **does not save it**. Returns
`blueprint_path`, `blueprint_name`, `is_valid`, `status` (`UpToDate`,
`UpToDateWithWarnings`, `Dirty`, `Error`, `Unknown`), `state_machines` (each
`name`, `state_count`, `transition_count`), `total_state_machines`,
`total_states`, `total_transitions`, `issues`, `error_count` and
`warning_count`.

- `issues` holds only two checks of the tool's own: `MissingSkeleton`
  (severity `Error`, `Animation Blueprint has no target skeleton assigned`)
  and `EmptyStateMachine` (severity `Warning`,
  `State machine '<n>' has no states`).
- **Compiler errors are not listed or counted.** `is_valid` is false when the
  compile ends in `Error` or a `MissingSkeleton` issue exists, but
  `error_count` counts only the issues. The message is `Blueprint is valid`,
  or `Blueprint has <n> error(s), <n> warning(s)` with those counts, so it can
  read `0 error(s)` for an AnimBP that does not compile. For the compiler's
  messages, run `blueprint_modify compile` on the same path (skill
  `ue-blueprints`).

## Building

### create_state_machine

| Parameter | Default | Meaning |
|---|---|---|
| `state_machine` | `Locomotion` | name for the new state machine |
| `target_graph` | — | a layer function graph to create it in (section `layers`); without it, the AnimGraph |
| `position` | `{"x":0,"y":0}` | node position |

Returns `node_id` (`StateMachine_<name>`, spaces turned into `_`), `name` (the
name you asked for) and `target_graph` when given; `message` is
`Created state machine '<n>'` plus ` in layer graph '<g>'` with
`target_graph`.

- **The real name can differ from `name`.** The tool sets the name before the
  state machine's own graph exists, and the plugin's domain notes report
  internal names such as `AnimationStateMachineGraph_1`. Read the name back
  with `get_info` and use that one in later calls.
- There is no duplicate check: a second call with the same name adds a second
  state machine.
- The new state machine is not connected to anything; use
  `connect_state_machine_to_output`.
- The skeleton check runs only without `target_graph`.
- Errors: `Animation Blueprint has no AnimGraph`, for `target_graph`
  `Layer function graph '<n>' not found. Available: <names>` and
  `Target graph '<g>' is not an animation graph (class: <c>). State machines can only be created in UAnimationGraph instances.`,
  and `Failed to create state machine node`,
  `Failed to create state machine graph`.

### connect_state_machine_to_output

Needs `state_machine`; optional `target_graph` (a layer function graph,
otherwise the AnimGraph). No skeleton check. Connects the state machine's pose
output to the graph's output pose node. Returns `state_machine` and
`target_graph` when given; `message` is
`Connected State Machine '<n>' to '<g>' Output Pose`, with `AnimGraph` or the
layer graph's name.

- **The output pose's existing link is broken first**, before the connection
  is checked. When the graph's schema then rejects the connection
  (`Schema rejected connection: <msg>`), the output pose is left unconnected.
- Errors: `State machine '<n>' not found in graph '<g>'. Available: <names>`
  (the state machine is in another graph), `Result/output node not found in graph '<g>'`,
  `State Machine '<n>' has no output pose pin. <detail>`,
  `Result node has no input pose pin. <detail>`.

### add_state

Needs `state_machine` (`state_machine parameter required`) and `state_name`
(`state_name parameter required`); optional `position` and `is_entry_state`
(default false). Returns `node_id` (`State_<name>_<n>`), `state_name`,
`is_entry_state` and `message`.

- The state gets its own graph with an output pose node and nothing wired to
  it. Fill it with `set_state_animation` or `add_anim_node`.
- `is_entry_state: true` makes it the entry state as `set_entry_state` does.
  **If that step fails, the call still succeeds** and still returns
  `is_entry_state: true`; the failure goes only to the editor log. Check with
  `get_state_machine_diagram` (`is_entry`).
- Errors: `State '<n>' already exists`, `Failed to create state node`,
  `Failed to create state graph`.

### remove_state

Needs `state_machine` and `state_name`
(`state_machine and state_name parameters required`). Removes the state and
**every transition into or out of it**.

### set_entry_state

Needs `state_machine` and `state_name`. No skeleton check. Links the state
machine's entry to the state and breaks the entry's previous link. Returns
`state_machine` and `entry_state`. Errors: `State machine has no entry node`,
`State '<n>' not found in state machine`, `Entry node has no output pin`,
`State '<n>' has no input pin`.

## The animation a state plays

### set_state_animation

| Parameter | Default | Meaning |
|---|---|---|
| `state_machine`, `state_name` | — | the state; required |
| `animation_path` | — | the asset; required |
| `animation_type` | `sequence` | `sequence`, `blendspace`, `blendspace1d`, `montage`, ignoring case |
| `parameter_bindings` | — | variables for the blend space's inputs (below) |

The three required parameters give
`state_machine, state_name, and animation_path required`. Another
`animation_type` gives `Unknown animation type: <t>`.

What it does, in order: loads the asset, checks its skeleton against the
AnimBP's, **removes every node from the state's graph** except the output pose
node, creates a player node at (200, 100), connects it to the output pose,
and binds the variables. Returns `state_name`, `animation_type`,
`animation_path` and `message`.

- **Bindings.** For `blendspace`, `parameter_bindings` is an object of
  `{"<pin>": "<variable>"}`. For `blendspace1d` it is a variable name, or an
  object whose single value is one; the variable is bound to the pin `X`.
  Each binding works like `bind_variable` (section `anim-nodes`): the key is
  matched against the player's input pins by name, ignoring case, then as a
  substring. The tool's own example uses `X` and `Y`; the plugin's domain
  notes say 2D blend spaces use the axis display names. Check the pin names
  with `inspect_node_pins` on the player node.
- **A failed binding fails the call after the state was already changed**:
  the graph holds the new player, and the call neither compiles nor saves.
  Fix the binding with `bind_variable`, or run the call again.
- **`montage` always fails**, after loading and checking the asset:
  `Montage assignment to states not yet implemented. Use PlayMontage via AnimInstance.`
  A montage plays at runtime instead, as the message says; the graph needs a
  slot node for the montage's slot (`add_anim_node` with
  `animation_type: "slot"`, section `anim-nodes`; skill `ue-anim-montage`).
- **Asset paths.** A path that starts with `/Game/` or `/Script/` is loaded as
  given; pass the full object path, such as
  `/Game/Anims/A_Walk.A_Walk`. Another path is tried under
  `/Game/Animations/` and a few other folders. A miss returns
  `Failed to load <Type> '<p>'. Use full path like '/Game/Animations/MyAsset.MyAsset'.`,
  with `AnimSequence`, `BlendSpace`, `BlendSpace1D` or `AnimMontage` as type.
  The load asks the engine for that class, so an asset of another class
  normally fails the same way. A failed load does not prove the asset is
  missing; search with `find_animations` or `asset_search`.
- **Skeleton errors**: `Animation Blueprint has no target skeleton`,
  `Animation asset has no skeleton`, and
  `Skeleton mismatch: AnimBlueprint uses '<a>', but asset uses '<b>'`. Which
  skeletons count as compatible is up to the engine.

## batch

Runs several state and transition operations, then compiles once. Needs an
`operations` array (`operations array required for batch mode`). Each entry
is `{"type": "<operation>", "params": {…}}`; `type` is matched ignoring
case. Inside `params` the tool reads its fields without
defaults; what a missing field reads as is up to the engine's JSON classes,
so send every field.

| `type` | `params` fields |
|---|---|
| `add_state` | `state_machine`, `state_name`, `x`, `y`, `is_entry` |
| `remove_state` | `state_machine`, `state_name` |
| `add_transition`, `remove_transition` | `state_machine`, `from_state`, `to_state` |
| `set_transition_duration` | `state_machine`, `from_state`, `to_state`, `duration` |
| `set_state_animation` | `state_machine`, `state_name`, `animation_path`, `animation_type` (empty means `sequence`), `parameter_bindings` object for `blendspace`, **`parameter_binding`** string for `blendspace1d` |
| `add_condition_node` | `state_machine`, `from_state`, `to_state`, `node_type`, `node_params`, `x`, `y` |
| `connect_condition_nodes` | `state_machine`, `from_state`, `to_state`, `source_node_id`, `source_pin`, `target_node_id`, `target_pin` |
| `connect_to_result` | `state_machine`, `from_state`, `to_state`, `source_node_id`, `source_pin` |
| `add_comparison_chain` | `state_machine`, `from_state`, `to_state`, `variable_name`, `comparison_type`, `compare_value`, `x`, `y` |

Note the differences from the single operations: `is_entry` and `x`/`y`
instead of `is_entry_state` and `position`, `parameter_binding` for 1D. The
transition entries behave as the single operations do (section `transitions`).
`set_transition_priority` has no batch form.

**`batch` always returns success.** The message is
`Batch operation completed successfully` or
`Batch operation completed with errors`. `data` holds `success` (no entry
failed and the compile succeeded), `success_count`, `failure_count`,
`compiled`, `compile_error` when the compile failed, and `results`: one entry
per operation, in order, with `type`, `success`, `error` when it failed, and
`node_id`, or `variable_node_id` and `comparison_node_id`, when it created
nodes.

- **Not atomic.** Entries that succeeded stay applied when another fails.
- Entry errors: `Invalid operation format` (not an object; that entry has
  only `success` and `error`, no `type`), `Unknown operation type: <t>`. A
  `set_state_animation` entry with an unknown `animation_type` fails with an
  empty `error`.
- A missing skeleton stops the batch before the first entry: `data` holds
  only `success: false` and `error`.
- The compile at the end saves the AnimBP when it succeeds, even when entries
  failed.

## Errors

| Error | Operation |
|---|---|
| `state_machine parameter required` | `get_state_machine`, `get_state_machine_diagram`, `add_state`, `set_entry_state`, `connect_state_machine_to_output` |
| `state_name parameter required` | `add_state`, `set_entry_state` |
| `state_machine and state_name parameters required` | `remove_state` |
| `state_machine, state_name, and animation_path required` | `set_state_animation` |
| `operations array required for batch mode` | `batch` |
| `AnimBlueprint has no target skeleton` | `add_state`, `remove_state`, `set_state_animation`, `get_state_machine_diagram`, `create_state_machine` without `target_graph`; `batch` reports it in `data` |
| `State machine '<n>' not found. Available: …` / `State '<n>' not found. Available: …` | any operation that names one |
