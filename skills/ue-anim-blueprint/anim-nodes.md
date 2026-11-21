# Animation nodes, wiring and properties

Operations of `anim_blueprint_modify` (bridge name
`unreal_anim_blueprint_modify`) that place animation nodes (players, blends,
bone controls, slots), connect them, bind variables to their pins, read and
set their properties, lay a graph out, and find animation assets. Every call
takes `blueprint_path` (the AnimBP's asset path) and `operation`. None of them
checks for a target skeleton; the asset players check the asset's skeleton
against the AnimBP's.

Writes compile the AnimBP and save it when the compile succeeds, but they do
not report a failed compile: run `validate_blueprint` afterwards (section
`state-machines`). Results sit under the task envelope's `data` key; the
tool's message is `Operation completed` unless noted.

## Which graph

All operations here except `find_animations` and `layout_graph` pick their
graph the same way:

| Given | Graph |
|---|---|
| `state_machine` and `state_name` | that state's own graph (both are needed; one alone is ignored) |
| `target_graph` | a layer function graph by name, ignoring case (section `layers`) |
| neither | the AnimGraph |

When both are given, the state wins. A missing layer graph returns
`Layer function graph '<n>' not found. Available: <names>`; a missing state
machine or state returns its `not found. Available:` error. Results echo
`target_graph` when it was given, and `state_machine` with `state_name` when
`state_machine` was given.

## Node IDs

Nodes created here get IDs such as `Anim_A_Walk_3`, `BlendSpace_BS_Run_4`,
`BlendSpace1D_<asset>_<n>`, `Slot_DefaultSlot_5`, `ModBone_spine_01_6`,
`TwoBoneIK_<bone>_<n>`, `LayeredBlend_<first bone>_<n>`, `AimOffset_<asset>_<n>`,
`CtrlRig__<n>`, `Inertial__<n>`, `DeadBlend__<n>`, `CopyPose__<n>`, `L2C__<n>`,
`C2L__<n>`, and `Var_<variable>_<n>` for a variable getter. They are stored in
the node's comment as `MCP_ANIM_ID:<id>`. A lookup by ID also accepts an
`MCP_ID:` and the UE object name, such as `AnimGraphNode_Root_0` for the
output pose. `connect_anim_nodes` and `bind_variable` also accept **a part of
the node's title**, ignoring case: the first node whose title contains it wins,
so prefer IDs. `blueprint_query`'s node queries report animation nodes by
their UE object name, which works here too.

## add_anim_node

Creates one node. `animation_type` is required, ignoring case:
`animation_type parameter required (sequence, blendspace, blendspace1d, slot, inertialization, dead_blending, copy_pose_from_mesh, modify_bone, two_bone_ik, control_rig, layered_blend_per_bone, aim_offset, local_to_component, component_to_local)`.
Optional for every type: `position` (default `{"x":0,"y":0}`) and
`auto_connect` (default false).

| `animation_type` | Node class | Own parameters |
|---|---|---|
| `sequence` | `AnimGraphNode_SequencePlayer` | `animation_path` (an AnimSequence) |
| `blendspace` | `AnimGraphNode_BlendSpacePlayer` | `animation_path` (a BlendSpace) |
| `blendspace1d` | `AnimGraphNode_BlendSpacePlayer` | `animation_path` (a BlendSpace1D) |
| `aim_offset` | `AnimGraphNode_RotationOffsetBlendSpace` | `animation_path` (an AimOffsetBlendSpace) |
| `slot` | `AnimGraphNode_Slot` | `slot_name` (default `DefaultSlot`) |
| `inertialization` | `AnimGraphNode_Inertialization` | — |
| `dead_blending` | `AnimGraphNode_DeadBlending` | — |
| `copy_pose_from_mesh` | `AnimGraphNode_CopyPoseFromMesh` | `use_attached_parent` (default true; read although the tool does not declare it) |
| `modify_bone` | `AnimGraphNode_ModifyBone` | `bone_name` (required), `rotation`, `translation`, modes and spaces (below) |
| `two_bone_ik` | `AnimGraphNode_TwoBoneIK` | `bone_name` and `effector_bone` (required), `effector_space`, `joint_target_bone`, `allow_stretching` (default false) |
| `layered_blend_per_bone` | `AnimGraphNode_LayeredBoneBlend` | `bone_name` (required), `blend_depth` (default 0), `mesh_space_rotation_blend` (default false) |
| `control_rig` | `AnimGraphNode_ControlRig` | `control_rig_class` (required) |
| `local_to_component` | `AnimGraphNode_LocalToComponentSpace` | — |
| `component_to_local` | `AnimGraphNode_ComponentToLocalSpace` | — |

The tool's description leaves out `two_bone_ik`, `local_to_component` and
`component_to_local`; they are dispatched all the same. Another type returns
`Unknown animation_type: '<t>'. Expected: …`, but only when `animation_path`
is given; without it an unknown type returns
`animation_path parameter required`, as do the four asset types.

- **Asset paths** (`sequence`, `blendspace`, `blendspace1d`, `aim_offset`). A
  path starting with `/Game/` or `/Script/` is loaded as given; pass the full
  object path, such as `/Game/Anims/A_Walk.A_Walk`. Another path is tried
  under `/Game/Animations/` and a few other folders. A miss returns
  `Failed to load <Type> '<p>'. Use full path like '/Game/Animations/MyAsset.MyAsset'.`
  (`AnimSequence`, `BlendSpace`, `BlendSpace1D`, `AimOffsetBlendSpace`). The
  load asks for that class, so an asset of another class normally fails the
  same way; a failed load does not prove the asset is missing. Search with
  `find_animations`. The skeleton check then gives
  `Animation Blueprint has no target skeleton`,
  `Animation asset has no skeleton` or
  `Skeleton mismatch: AnimBlueprint uses '<a>', but asset uses '<b>'`.
- **`modify_bone`.** `rotation` is `{"pitch", "yaw", "roll"}`, `translation`
  `{"x", "y", "z"}`, both default zero. `rotation_mode` is `replace`, `ignore`
  or else additive. The translation mode is ignore when `translation` is
  absent; with `translation`, `translation_mode` is `replace` or else
  additive, so `ignore` does not work there although the tool lists it.
  `rotation_space` and `translation_space` are `component`, `parent`, `world`
  or else bone space. Error: `bone_name parameter required for modify_bone`.
- **`two_bone_ik`.** `effector_space` is `ComponentSpace`, `WorldSpace`,
  `ParentBoneSpace` or else `BoneSpace`. Errors:
  `bone_name parameter required for two_bone_ik`,
  `effector_bone parameter required for two_bone_ik`.
- **`layered_blend_per_bone`.** `bone_name` is a comma-separated list of
  bones, each optionally `bone:depth` to override `blend_depth`; they become
  the branch filters of the node's first blend layer. Error:
  `bone_name parameter required for layered_blend_per_bone`. The node has
  more than one pose input; connect them with explicit pins (below).
- **`control_rig`.** `control_rig_class` is the rig's generated class path,
  such as `/Game/Rigs/CR_Hand.CR_Hand_C`. Errors:
  `control_rig_class parameter required for control_rig type`,
  `Invalid control_rig_class: '<p>' — must be a UControlRig subclass`.
- **`auto_connect: true`** links the new node's pose output to the graph's
  output pose without breaking the link already there. If no pose pin
  matches, **the call fails after the node was created**:
  `Node created (id=<id>) but auto_connect failed: <reason>`. That call
  neither compiles nor saves; delete the node or connect it with
  `connect_anim_nodes`.

Returns `node_id`, `node_class`, `animation_type`, `animation_path`,
`auto_connected`, `pins` (each pin's name, direction, type and connections)
and `message` (`Created <type> node in <graph>`), plus the graph fields
above.

## delete_anim_node

Needs `node_id` (`node_id parameter required`), looked up by ID only.
Removes the node and its links. Returns `deleted_node_id` and `message`
(`Deleted node '<id>' from <graph>`). Errors: `Node '<id>' not found in <graph>`,
and `Cannot delete protected node '<id>' (type: <class>)` for the output pose
and the result nodes of states and transitions. It works in any graph this
section can address, including a state's graph.

## connect_anim_nodes

Needs `source_node_id` (`source_node_id parameter required`) and
`target_node_id` (`target_node_id parameter required`). Both are looked up by
ID, then by title. `target_node_id` `root`, `output` or `output_pose`
(ignoring case) means the graph's output pose node.

- **Without both pins**: the pose output of the source (`Pose`, `Output`,
  `OutputPose`, `Result`, then any pin with `Pose` in its name, then the first
  non-exec output) goes to the pose input of the target (`Result`, `Pose`,
  `InPose`, `InputPose`, then a name with `Pose`, then the first non-exec
  input). On a node with several pose inputs, that picks one for you; pass
  the pins.
- **With `source_pin` and `target_pin`** (both, or neither is used): each is
  matched by name, ignoring case, then as a substring. Pose pins are linked
  directly. Any other pin goes through the graph's schema:
  `Schema rejected connection '<s>' -> '<t>': <reason>`.
- **The target pin's existing links are broken first**, also before the
  schema check, so a rejected connection leaves the target pin unconnected.

Returns `source_node_id`, `target_node_id`, `source_pin` and `target_pin`
when given, the graph fields, and `message`
(`Connected '<s>' -> '<t>'` plus the graph). Errors:
`Source node '<id>' not found in graph '<g>'`,
`Target node '<id>' not found in graph '<g>'`,
`Result/output node not found in graph '<g>'`,
`Output pin '<p>' not found on source node '<id>'. <pins>`,
`Input pin '<p>' not found on target node '<id>'. <pins>`,
`No output pose pin on source node '<id>'. <detail>`,
`No input pose pin on target node '<id>'. <detail>`.

## bind_variable

Drives a node's input pin from a Blueprint variable. Needs `variable_name`
(`variable_name parameter required`), `target_node_id`
(`target_node_id parameter required`) and `target_pin`
(`target_pin parameter required`). The node is looked up by ID, then by title;
the pin by name, ignoring case, then as a substring.

It creates a variable getter 300 units left of the target, **breaks the pin's
existing links**, and connects the getter through the schema. Returns
`variable_node_id`, `variable_name`, `target_node_id`, `target_pin`, the graph
fields and `message`.

- The variable must exist on the AnimBP's class:
  `Variable '<v>' not found in AnimBlueprint`. Add it with
  `blueprint_modify add_variable` (skill `ue-blueprints`) first.
- **A rejected connection leaves debris**:
  `Schema rejected connection: variable '<v>' output -> pin '<p>': <reason>`
  is returned after the getter was created and the pin's old link was
  broken. Remove the getter with `delete_anim_node` and restore the link.
- Other errors: `Target node '<id>' not found in graph '<g>'`,
  `Input pin '<p>' not found on target node '<id>'. <pins>`,
  `Failed to create variable get node`,
  `Variable node for '<v>' has no output pin`.

## get_anim_node_property and set_anim_node_property

These read and write the settings struct inside an animation node, such as a
sequence player's `PlayRate` or `bLoopAnimation`. Both take `node_id`, looked
up by ID only, and the graph parameters above.

**`get_anim_node_property`** needs `node_id` (`node_id parameter required`).
With `property_name`, it returns `property_name`, `property_value` and
`property_type`; without it, `properties` (each `name`, `value`, `type`,
skipping deprecated, transient and delegate properties) and
`property_count`. Both forms also return `node_id`, `node_class` and
`struct_type`. Use the form without `property_name` to learn the names.

**`set_anim_node_property`** needs `node_id` and `property_name`
(`node_id and property_name parameters required`); `property_value` is the
value as text in the engine's property text format, such as `1.5` or
`true`; a struct takes its text form, which the engine parses. Read the
current value with `get_anim_node_property` to see the format. When the node has an input pin of the
same name, its default is updated to match. Returns `node_id`,
`property_name`, `property_value` and `message`
(`Set <p> = <v> on node <id>`).

Errors for both: `Node '<id>' not found in graph '<g>'`,
`Node '<id>' is not an anim graph node (class: <c>)`,
`Node '<id>' (class: <c>) has no embedded FAnimNode struct`,
`Property '<p>' not found on <struct>. Available: <names>`, and for set
`Failed to set '<p>' to '<v>' (type: <type>)`.

## layout_graph

Arranges the nodes of the AnimGraph, or of a state's graph with
`state_machine` and `state_name`. It takes no `target_graph`, so a layer
graph cannot be laid out. Optional `spacing_x` (default 400), `spacing_y`
(default 200) and `preserve_existing` (default false). With
`preserve_existing: true`, nodes whose position is not 0, 0 keep their place
and are counted in `skipped_nodes`; with the default, no node is skipped.

The message is
`Laid out <n>/<n> nodes in <graph> (<n> entry points, <n> disconnected, <n> data-only)`.
`data` holds `graph_name` (the graph's description, such as `AnimGraph`),
`blueprint_path`, `total_nodes`, `layout_nodes`, `skipped_nodes`,
`disconnected_nodes`, `data_only_nodes`, `entry_points`, `compiled` and
`compile_status`.

- In the AnimGraph it compiles and saves on success; `compile_status` is `OK`
  or the compile error.
- **In a state's graph it neither compiles nor saves**; it only marks the
  asset as modified, and `compile_status` is `skipped (state-bound graph)`.
  Save with `asset save_asset` (skill `ue-assets`).

## find_animations

Lists animation assets under `/Game`. `blueprint_path` is still required and
validated; when it loads as an AnimBP, the list is limited to assets whose
skeleton is exactly the AnimBP's target skeleton. When it does not load, the
list is not filtered and no error is returned.

| Parameter | Default | Meaning |
|---|---|---|
| `search_pattern` | `*` | a substring of the asset name, ignoring case; not a wildcard |
| `asset_type` | `All` | `AnimSequence`, `BlendSpace`, `BlendSpace1D`, `Montage`, ignoring case; anything else means all four |

- **The default `*` is taken literally** and matches only names that contain
  `*`. Pass `search_pattern: ""` to list everything, or a part of the name.
- The class filter includes subclasses, as the engine's asset registry
  defines them.
- With a skeleton filter, every candidate asset is loaded to read its
  skeleton; on a large project this is slow. Assets that share a compatible
  but different skeleton are left out.
- Returns `count`, `assets` (object paths) and `skeleton_filter` when the
  AnimBP loaded. **`skeleton_filter` holds the AnimBP's name**, not the
  skeleton's.
