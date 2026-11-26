# How an Animation Blueprint works, and what this tool cannot build

Engine background for decisions the operations do not make for you. Each
fact names its source: an Epic documentation page (URLs at the end) or the
UE 5.8.4 engine source, read, not run. The toolkit declares UE 5.6 as its
minimum (`UELLMToolkit.uplugin`, `EngineVersion` 5.6.0) and its animation
code has no version guards, so the operations behave alike on 5.6, 5.7 and
5.8 as far as the toolkit is concerned. Engine behaviour that changed
between those versions is not covered here: unverified.

## The two halves of an AnimBP

- **The EventGraph** computes gameplay values (speed, falling, aim) into
  variables. Its `Event Blueprint Update Animation` runs every frame on the
  game thread [Graphing].
- **The AnimGraph** turns those variables into a pose: players, blends, state
  machines, slots, bone controls, ending in the Output Pose node. It runs on
  a worker thread unless the class setting *Use Multi Threaded Animation
  Update* is off [Graphing].
- **`Blueprint Thread Safe Update Animation`** is a function you override to
  compute the variables off the game thread, reading gameplay data through
  property access [Graphing]. This tool and the toolkit's
  `blueprint_modify` documentation in this skill set do not cover building
  property access nodes: unverified whether `blueprint_modify` can.
- **Decision:** compute variables in the EventGraph unless the project
  already uses the thread-safe function; follow what the AnimBP has.

## State machines

- **Entry**: the entry links to the default state, usually idle [State
  Machines]. `set_entry_state` or `add_state` with `is_entry_state` sets it.
- **Transitions go one way.** Two-way movement needs a transition in each
  direction; the transition's *Bidirectional* setting is documented as
  unsupported [Transition Rules].
- **Priority**: when several transitions out of a state are true at once, the
  smallest priority order wins [Transition Rules]. Default 1.
- **Duration**: the crossfade length in seconds [Transition Rules]. Default
  0.2 (`AnimStateTransitionNode.cpp:84`).
- **Time Remaining** in a rule reads the remaining time of an animation in
  the state [Transition Rules]; the toolkit's `TimeRemaining` node is bound
  to the first sequence or blend space player of the source state.
- **Automatic Rule Based on Sequence Player in State** starts the blend from
  the most relevant player's remaining time, without rule logic [Transition
  Rules]. This tool cannot set it (it is a property of the transition, not of
  an animation node); use a `TimeRemaining` condition instead.
- **Conduits and state aliases** (one-to-many transitions, shared rules)
  exist in the engine [State Machines]. This tool cannot create either; a
  state machine built with it uses plain states and transitions.
- **Max Transitions Per Frame** on the state machine limits how many
  transitions can be taken in one update [State Machines]; this tool does
  not set it.

## Slots and montages

- A montage plays only through Slot nodes in the AnimGraph; every skeleton
  starts with a slot `DefaultSlot`; one montage can drive several slots of
  the same slot group [Slots].
- An AnimBP without a slot node for the montage's slot shows nothing of that
  montage, since montages play exclusively through slots [Slots]. Whether
  `Play Montage` then reports a failure is not documented there: unverified.
- Place the slot after the locomotion pose (state machine → slot → output),
  so a montage overrides it while it plays. Section `locomotion` has the
  calls; the montage itself is skill `ue-anim-montage`.

## Cached poses and layered blends

- A *Save Cached Pose* node lets one pose feed several places; the engine
  documents only the node classes (`UAnimGraphNode_SaveCachedPose`,
  `UAnimGraphNode_UseCachedPose` in the API reference). This tool has no
  cached-pose node type.
- `layered_blend_per_bone` blends poses per bone branch: its inputs are
  `BasePose` and the array `BlendPoses` (`FAnimNode_LayeredBoneBlend` in the
  engine source). Read the exact pin names from `add_anim_node`'s `pins`.
- Consequence: an upper-body montage over running legs, which needs the
  locomotion pose both as the base and under the slot, needs a cached pose and
  cannot be built with this tool alone.

## Linked anim layers

- An **Animation Layer Interface** (ALI) asset declares layers and their
  inputs; the AnimBP must implement (bind) it before it can use its layers
  [Linking].
- A **Linked Anim Layer** node in the AnimGraph calls a layer; its *Instance
  Class* is "the Animation Blueprint class to use" for it [Linking]. At
  runtime *Link Anim Class Layers* swaps the implementing class, for example
  per weapon, so only that weapon's animations load [Linking].
- Epic's page does not state whether the linked AnimBP must share the
  skeleton: unverified.
- The tool's calls for all of this are in section `layers`.

## Sources

- [Graphing] https://dev.epicgames.com/documentation/en-us/unreal-engine/graphing-in-animation-blueprints-in-unreal-engine
- [State Machines] https://dev.epicgames.com/documentation/en-us/unreal-engine/state-machines-in-unreal-engine
- [Transition Rules] https://dev.epicgames.com/documentation/en-us/unreal-engine/transition-rules-in-unreal-engine
- [Slots] https://dev.epicgames.com/documentation/en-us/unreal-engine/animation-slots-in-unreal-engine
- [Linking] https://dev.epicgames.com/documentation/en-us/unreal-engine/animation-blueprint-linking-in-unreal-engine
- Cached pose API: https://dev.epicgames.com/documentation/en-us/unreal-engine/API/Editor/AnimGraph/UAnimGraphNode_SaveCachedPose
