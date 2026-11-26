# control_rig: workflows

`rig_path` is required on every call and left out of the snippets below; it is
the same rig in each. Names in angle brackets come from `inspect` or from an
add result — never type them from memory. Pin names below are those of the
UE 5.8.4 headers (section `ue-facts`); confirm them in `inspect` on your
engine.

## 1. Read a rig

1. `{"operation":"inspect"}`.
2. From the graph in `data.graph.graphs[]` that holds the event nodes (the
   writes go to the rig's default model; other graphs are read-only here):
   - the event nodes: `struct_name` `RigUnit_BeginExecution` (Forwards
     Solve), `RigUnit_InverseExecution` (Backwards Solve),
     `RigUnit_PrepareForExecution` (Construction Event);
   - the execute chain: follow `links[]` from `<event>.ExecutePin` through
     each node's execute pin;
   - the last node on the Forwards Solve chain — new mutable nodes go after it.
3. From `data.hierarchy.elements[]`: the exact bone / control names you will
   reference (`hand_r`, `foot_l`, …).

## 2. Offset a bone in Forwards Solve (Get → Set Transform)

Goal: each frame, read a bone's transform and write it back with a weight.

1. Find the units:
   `{"operation":"list_structs","filter":"RigUnit_GetTransform"}` and
   `"RigUnit_SetTransform"`; take each `path`.
2. Add them:
   ```json
   {"operation":"add_node","struct_path":"/Script/ControlRig.RigUnit_GetTransform","node_name":"GetHand","x":200,"y":200}
   {"operation":"add_node","struct_path":"/Script/ControlRig.RigUnit_SetTransform","node_name":"SetHand","x":600,"y":0}
   ```
   Read `node.path` and `node.pins` of each result.
3. Point both at the bone and choose the space:
   ```json
   {"operation":"set_default","pin_path":"GetHand.Item","value":"(Type=Bone,Name=\"hand_r\")"}
   {"operation":"set_default","pin_path":"SetHand.Item","value":"(Type=Bone,Name=\"hand_r\")"}
   {"operation":"set_default","pin_path":"SetHand.Space","value":"GlobalSpace"}
   {"operation":"set_default","pin_path":"SetHand.Weight","value":"0.5"}
   ```
   If a struct value is refused, look at the `default_value` text of the same
   pin in `inspect` and copy its form, or set the sub-pin
   (`GetHand.Item.Name` = `hand_r`).
4. Wire data and execution. Here the Forwards Solve output had no link yet;
   if it had, see workflow 4.
   ```json
   {"operation":"link","output_pin":"GetHand.Transform","input_pin":"SetHand.Value"}
   {"operation":"link","output_pin":"<ForwardsSolveNode>.ExecutePin","input_pin":"SetHand.ExecutePin"}
   ```
5. `{"operation":"recompile"}`, then `get_output_log` with
   `filter: "Script Msg"` and a look for RigVM / Control Rig errors.
6. Verify with `inspect`: both nodes present; `links[]` holds
   `GetHand.Transform → SetHand.Value` and the execute link; the `Item`,
   `Space`, `Weight` pins show the values you set.
7. Save: `asset` `{"operation":"save_asset","asset_path":"<rig_path>"}`.

The same steps as one batch (batch key names, section `batch`):

```json
{"operation":"batch","operations":[
  {"op":"add_node","struct":"/Script/ControlRig.RigUnit_GetTransform","name":"GetHand","x":200,"y":200},
  {"op":"add_node","struct":"/Script/ControlRig.RigUnit_SetTransform","name":"SetHand","x":600,"y":0},
  {"op":"set_default","pin":"GetHand.Item","value":"(Type=Bone,Name=\"hand_r\")"},
  {"op":"set_default","pin":"SetHand.Item","value":"(Type=Bone,Name=\"hand_r\")"},
  {"op":"set_default","pin":"SetHand.Weight","value":"0.5"},
  {"op":"link","source":"GetHand.Transform","target":"SetHand.Value"},
  {"op":"link","source":"<ForwardsSolveNode>.ExecutePin","target":"SetHand.ExecutePin"},
  {"op":"recompile"}
]}
```

Run it as a batch only after a single-call run has shown that the node names
come out as requested; a renamed node makes every later entry miss.

## 3. Drive a node from a member variable

Goal: let the AnimBP (or Sequencer) set the weight.

1. `{"operation":"add_member_var","var_name":"HandWeight","cpp_type":"float","default_value":"1.0"}`
   (`is_public` true by default — needed for the AnimBP node's pin).
2. `{"operation":"add_var_node","var_name":"HandWeight","is_getter":true,"x":300,"y":300}`
   → node `HandWeight_Get` (read `node.path`); its output pin is in
   `node.pins` (direction `OUT`).
3. `link` `<HandWeight_Get>.<out pin>` → `SetHand.Weight`. The pin's default is
   now ignored; the variable drives it.
4. `recompile`, `inspect` (link present), save.
5. In the Animation Blueprint, enable the variable as a pin on the Control Rig
   node (skill `ue-anim-blueprint`; Epic: "Use Pin").

## 4. Insert a node into an existing execute chain

Chain before: `A.ExecutePin → B.ExecutePin`. Goal: `A → N → B`.

1. `link` `A.ExecutePin` → `N.ExecutePin`. The engine breaks `A → B` itself
   (an execute output feeds one input).
2. `link` `N.ExecutePin` → `B.ExecutePin`.
3. `inspect`: `links[]` has `A→N` and `N→B`, and no `A→B`.

In the other order the result is the same: `N → B` replaces `A → B` on B's
input, then `A → N` is added. Check with `inspect` either way.

## 5. Remove a node cleanly

1. `inspect`; note the node's incoming and outgoing execute links.
2. `remove_node` with its `path` — its links go with it.
3. If it sat mid-chain, re-link the gap: `prev.ExecutePin → next.ExecutePin`.
4. `recompile`, `inspect` (chain unbroken from the event), save.

## 6. Verification checklist (after any edit)

- [ ] `inspect` shows every node you added, under the `path` you use.
- [ ] Every mutable node is reachable from an event through `links[]`.
- [ ] Every input you wired is a `target` in `links[]` exactly once.
- [ ] Every default you set appears in the pin's (or parent pin's)
      `default_value`.
- [ ] `get_output_log` after `recompile` holds no new `Script Msg` error and
      no compile error for the rig.
- [ ] Saved with `asset` `save_asset`.
- [ ] For behaviour, preview the rig in the editor or test the AnimBP in PIE
      (skills `ue-viewport`, `ue-pie-testing`); this tool cannot tell whether
      the pose is right.
