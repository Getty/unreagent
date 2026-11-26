# Control Rig: what the engine does

Background for designing a graph edit. Sources: the UE 5.8.4 engine source
(`Plugins/Runtime/RigVM/.../RigVMController.h|.cpp`,
`Plugins/Animation/ControlRig/.../Units/`, `RigHierarchyDefines.h`), read, not
run; Epic's pages
[Solve Directions](https://dev.epicgames.com/documentation/en-us/unreal-engine/control-rig-forwards-solve-and-backwards-solve-in-unreal-engine),
[Controls, Bones, and Nulls](https://dev.epicgames.com/documentation/en-us/unreal-engine/controls-bones-and-nulls-in-control-rig-in-unreal-engine),
[Control Rig in Animation Blueprints](https://dev.epicgames.com/documentation/en-us/unreal-engine/control-rig-in-animation-blueprints-in-unreal-engine).
The toolkit targets UE 5.6 and 5.7; rules taken only from the 5.8.4 source
are unverified on those.

## Events and the execute chain

A Control Rig graph runs from event nodes along **execute links**:

| Event (display) | Struct (`struct_name` in `inspect`) | Runs |
|---|---|---|
| Forwards Solve | `RigUnit_BeginExecution` | every evaluation; drives bones from controls and variables. Present in a new rig; used by Sequencer and the AnimBP node |
| Backwards Solve | `RigUnit_InverseExecution` | drives controls from bones; used to bake animation onto the rig |
| Construction Event | `RigUnit_PrepareForExecution` | once, at post-initialization: set up variables, initial control positions, spawn elements |

(Epic, Solve Directions; struct names from the 5.8.4 headers.)

- Each event exists at most once in the main graph. Adding it again returns
  the existing node and logs
  `Rig Graph can only contain one single <name> node.`
- Execute pins: the event's output pin and each mutable unit's in/out pin are
  named `ExecutePin` in the 5.8.4 source (`cpp_type` an execute-pin struct,
  direction `OUT` on events and `I/O` on mutable units). Older rigs and
  engines may show another name; take it from `inspect`.
- **Mutable units** (Set Transform, Set Control …) do something only when they
  sit on an event's execute chain. **Pure units** (Get Transform, math) have no
  execute pin; they run because a node on the chain uses their output
  (general RigVM behaviour, not traced in the source here).
- **An execute output feeds one input.** Linking from an execute output
  breaks its previous execute link (`AddLink` → `BreakAllLinks(OutputPin)` for
  execute pins). Linking into any input breaks that input's previous link.
  Insert a node into the chain as `prev → new`, then `new → next`.

## Hierarchy

Element types (`ERigElementType`): `Bone`, `Null`, `Control`, `Curve`,
`Reference`, `Connector`, `Socket` (`Physics` is hidden/legacy).

- **Bones** come from the skeleton, plus extra rig-only bones (IK targets,
  helpers). **Controls** are what animators move and Sequencer keys.
  **Nulls** group and offset other elements (Epic, Controls, Bones, and Nulls).
- `inspect` gives only type and name per element; parents, transforms and
  control settings are not reported, and the tool cannot change any of it.

## Units, templates, variables

- **Unit** (`add_node`): a C++ `FRigUnit_*` struct with fixed pin types,
  addressed as `/Script/<Module>.RigUnit_<Name>` (the `F` dropped), e.g.
  `/Script/ControlRig.RigUnit_GetTransform`,
  `/Script/ControlRig.RigUnit_SetTransform`.
- **Template** (`add_template`): a generic node whose pin types resolve when
  linked or set; addressed by the notation `list_templates` returns.
- **Member variables** are the rig's inputs. A public one (`is_public: true`,
  the default) is what Epic calls instance-editable — the precondition for
  showing it as a pin on the AnimBP's Control Rig node ("Use Pin"). The 5.8.4
  source sets the same flag (`SetVariablePublic` → not "blueprint only
  editable").

### Get / Set Transform pins (5.8.4 headers)

| Unit | Pins |
|---|---|
| `RigUnit_GetTransform` | in `Item` (FRigElementKey), `Space` (`GlobalSpace` default / `LocalSpace`), `bInitial`; out `Transform` |
| `RigUnit_SetTransform` | execute `ExecutePin`; in `Item`, `Space`, `bInitial`, `Value` (FTransform), `Weight` (0–1), `bPropagateToChildren` |

`LocalSpace` is relative to the parent, `GlobalSpace` to the rig. `bInitial`
reads or writes the element's initial value, "the starting value of the
element before control rig logic … executes" (Epic, Controls, Bones, and
Nulls) instead of the current one.

## Pin paths and text values

- Pin path: `<node>.<pin>`, sub-pins `<node>.<pin>.<member>`
  (`GetTransform.Item.Name`, `SetTransform.Value.Translation.X`).
- Defaults are the engine's struct text form; read an existing pin of the same
  type for the exact spelling. Examples are in section `operations`,
  "set_default".

## Where the engine's errors go

The RigVM controller reports refusals through `ReportError`, which ends in the
script exception handler: by default a `LogScript` line
`Script Msg: <package path> : <message>` (`CoreMisc.cpp`,
`RigVMController.cpp`, 5.8.4 source). Some refusals also show an editor
notification. `get_output_log` with `filter: "Script Msg"` (or
`categories: ["LogScript"]`) reads them. Typical messages:

- `Cannot link '<a>' to '<b>': <reason>.` — type mismatch, direction,
  a cycle.
- `Rig Graph can only contain one single <name> node.`

## Running the rig on a character

- In an Animation Blueprint: the **Control Rig** anim node, Control Rig Class
  set on it; it reads the incoming pose only with **Transfer Input Pose** on;
  public variables become pins with **Use Pin** (Epic, Control Rig in
  Animation Blueprints). With this toolkit that node is
  `anim_blueprint_modify`'s (skill `ue-anim-blueprint`).
- In Sequencer the rig is keyed through its controls (skill `ue-sequencer`).

## Engine versions

| | 5.6 | 5.7 | 5.8 |
|---|---|---|---|
| Control Rig Blueprint class header the toolkit includes | `ControlRigBlueprint.h` | `ControlRigBlueprintLegacy.h` | `ControlRigBlueprintLegacy.h` exists in the 5.8.4 source |
| tool behaviour | same code | same code | not built against by the toolkit's `#if`s (they test `>= 7`); unverified |

The toolkit switches only that include (`MCPTool_ControlRig.cpp:141-145`,
`ControlRigEditor.cpp:4-8`); no operation behaves differently by version in
its own code. The "Legacy" header name hints that Epic is moving Control Rig
assets to a new asset base in 5.7+; what that means for rigs created in 5.7 or
later is unverified.
