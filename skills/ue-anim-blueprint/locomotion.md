# Building a locomotion AnimBP, end to end

A worked order of calls for the most common job: a character AnimBP with a
ground locomotion blend space, an in-air state for jumping and falling, and a
slot so montages can play over it. Every `anim_blueprint_modify` call here is
documented in sections `state-machines`, `transitions` and `anim-nodes`; the
calls of other tools are documented in the skills named beside them. This
order was assembled from the toolkit's source and Epic's documentation, not
run against an editor: check each step's result as described before you take
the next one.

Names such as `ABP_Hero`, `BS_Locomotion` or a speed range of 0 to 600 are
examples, not requirements. Use the project's own assets and values.

## What you need first

| Need | How to get it |
|---|---|
| the skeleton the character's mesh uses | `asset_search` (skill `ue-assets`); a Mixamo or Fab pack on another skeleton is retargeted first (skill `ue-retarget`) |
| idle, walk and run sequences on that skeleton | `find_animations` with an AnimBP on that skeleton (section `anim-nodes`), or `asset_search` |
| a 1D blend space over speed with those three samples | skill `ue-blend-space`; note the axis range you give it |
| jump and fall sequences (a loop for in-air at least) | as above |

Every animation must use the AnimBP's target skeleton: the players check it
and fail with `Skeleton mismatch: …`.

## 1. The AnimBP

1. Look for an existing one: `blueprint_query list` with
   `type_filter: "AnimInstance"` (skill `ue-blueprints`, section `query`).
   Edit it rather than create a second one.
2. Otherwise create it with `blueprint_modify create`, `parent_class`
   `AnimInstance` (skill `ue-blueprints`, section `modify`). Do not duplicate
   another AnimBP (the plugin's domain notes: stale generated-class references
   that do not compile).
3. Set its target skeleton (`asset` `set_asset_property`, `TargetSkeleton`;
   skill `ue-assets`). Until then most write operations of this tool fail
   with `AnimBlueprint has no target skeleton`.
4. Check: `get_info` returns `skeleton` and `skeleton_path`.

## 2. The variables the graph reads

Add them with `blueprint_modify add_variable` (skill `ue-blueprints`): here
`Speed` as a float and `IsFalling` as a bool. Transition conditions and blend
space bindings look the variable up on the AnimBP's class and fail with
`Variable '<v>' not found in AnimBlueprint` without it.

**Something must write them every frame.** Epic's documentation describes two
places: the EventGraph's `Event Blueprint Update Animation`, which runs on
the game thread, and the `Blueprint Thread Safe Update Animation` function
with property access, which runs on a worker thread
("Graphing in Animation Blueprints", URL in section `background`). This tool
builds neither. The usual EventGraph chain is: `Try Get Pawn Owner` → its
velocity → vector length → set `Speed`; the owner's character movement
component → `Is Falling` → set `IsFalling`. Building those nodes is
`blueprint_modify` work (skill `ue-blueprints`, section `modify`); take the
node and function names from that skill, not from this paragraph. An AnimBP
whose variables are never written compiles and stays in its entry state.

## 3. The state machine

1. `create_state_machine` with `state_machine: "Locomotion"`. Keep the
   returned `node_id` (`StateMachine_Locomotion`).
2. **`get_info` at once**: the state machine's real name is a generated one,
   not `Locomotion` (section `state-machines`). Every later `state_machine`
   parameter takes the real name; below it is written `<SM>`.
3. `add_state` `Ground` with `is_entry_state: true`, then `add_state` `InAir`.
   Positions only matter for reading the graph; `{"x":0,"y":0}` and
   `{"x":400,"y":0}` keep them apart.
4. `set_state_animation` on `Ground`: `animation_type: "blendspace1d"`,
   `animation_path` the blend space's object path, `parameter_bindings`
   `"Speed"` (bound to the player's `X` pin).
5. `set_state_animation` on `InAir`: `animation_type: "sequence"` with the
   in-air loop. A sequence player loops by default (`bLoopAnimation = true`,
   `AnimNode_SequencePlayer.h:138` in the UE 5.8.4 source); read it with
   `get_anim_node_property`.
6. Check: `get_state_machine_diagram` shows both states, `is_entry` on
   `Ground`, and the animation of each.

A three-part jump (start, loop, land) is the same pattern with more states:
`JumpStart` → `InAir` on a `TimeRemaining` condition (below), `InAir` →
`Land` on `IsFalling == false`, `Land` → `Ground` on `TimeRemaining`. Turn
looping off on the start and land players with `set_anim_node_property`
(`bLoopAnimation`, `false`); find their node IDs as described under
"Finding a node you did not create" below.

## 4. The transitions

1. `add_transition` `Ground` → `InAir`, and `InAir` → `Ground` (a transition
   goes one way only; Epic documents the transition's bidirectional setting
   as not functioning).
2. `add_comparison_chain` on `Ground` → `InAir`: `variable_name: "IsFalling"`,
   `comparison_type: "Equal"`, `compare_value: "true"`. Booleans allow only
   `Equal` and `NotEqual`.
3. `add_comparison_chain` on `InAir` → `Ground`: the same with
   `compare_value: "false"`.
4. `set_transition_duration` on each, for example `0.15` into the air and
   `0.2` back. A second `add_comparison_chain` on the same transition ANDs
   with the first; an OR is built node by node (`add_condition_node` `Or`,
   section `transitions`).
5. Where two transitions out of one state can be true at once, the one with
   the **smaller** `priority` wins (section `transitions`).
6. Check: `get_transition_nodes` with `from_state` and `to_state` shows the
   getter, the comparison and the link into the result; `inspect_node_pins`
   on the comparison shows `B`'s `default_value`.

**A condition on the remaining time** (leave a state when its animation is
nearly over): `add_condition_node` `TimeRemaining` (it reads the first
sequence or blend space player in the source state, so set that state's
animation first), `add_condition_node` `Less`, `connect_condition_nodes` from
the getter to the comparison with `target_pin: "A"`, `set_pin_default_value`
`B` to the threshold in seconds, then `connect_to_result` from the
comparison. Read the getter's output pin name with `inspect_node_pins` before
you connect.

## 5. The AnimGraph: slot and output

A montage plays only through a slot node of its slot in the AnimGraph (Epic,
"Animation Slots"; skill `ue-anim-montage`). Put a slot between the state
machine and the output pose from the start:

1. `add_anim_node` with `animation_type: "slot"` and `slot_name` the
   montage's slot (default `DefaultSlot`). Keep its `node_id`; its pose input
   is the pin `Source` (`FAnimNode_Slot::Source` in the engine source), its
   `pins` in the result say so too.
2. `connect_anim_nodes` from the state machine's `node_id` to the slot's
   `node_id`.
3. `connect_anim_nodes` from the slot to `target_node_id: "output"`.

Without a slot, `connect_state_machine_to_output` with `<SM>` does the
direct link in one call. For an upper-body-only montage over legs that keep
walking, the usual graph caches the locomotion pose and blends a slot over it
with `layered_blend_per_bone` from the spine. This tool has no cached-pose
node type, and a pose output feeds one input only, so that layout cannot be
built with it; build it in the editor or with the Python route (skill
`ue-python`). That a pose output takes a single link is from community
answers, not Epic's documentation: unverified.

## 6. Compile, verify, look

1. `validate_blueprint`: `status` must be `UpToDate` (or
   `UpToDateWithWarnings`); `error_count` does not count compiler errors.
2. On `Error`, `blueprint_modify compile` on the same path gives the
   compiler's messages (skill `ue-blueprints`).
3. `blueprint_query get_anim_graph` and `get_state_machine_detail`: the pose
   chain from the output back through the slot to the state machine, the
   states, and each transition's rule expression; `[UNWIRED=<value>]` marks
   a data input nothing drives.
4. In play: assign the AnimBP to the character's mesh (skill `ue-character`
   reads what a placed character uses), run PIE (skill `ue-pie-testing`) and
   look (skill `ue-viewport`). `blueprint_modify add_debug_print` can log
   `Speed` and `IsFalling` from `BlueprintUpdateAnimation` (skill
   `ue-blueprints`): a character that slides in its idle pose usually has a
   `Speed` that never changes.

## Finding a node you did not create

`set_state_animation` creates the state's player with a tool ID but does not
return it. `inspect_node_pins` with `state_machine`, `state_name` and a
`node_id` that is part of the player's title (the asset's name, for
example) finds the first node whose title contains it and returns its real
`node_id`. Use that ID with `get_anim_node_property`,
`set_anim_node_property` and `bind_variable`, which look nodes up by ID. In
the AnimGraph, `blueprint_query find_nodes` with `graph_type: "AnimGraph"`
reports animation nodes by their UE object name, which works as an ID too.
