# Blueprint workflows, end to end

Each workflow uses only operations and node types the two tools have, with
the parameters of sections `modify`, `graphs` and `query`. Engine facts are
marked with their source; "UE 5.8.4 source" means read in the engine source
and not run. None of these sequences has been run against a live editor for
this skill: treat every step's result as something to check, which is what
the verification steps are for.

## 1. An Actor Blueprint with components, a variable, a function and logic

Goal: `BP_Lamp`, a lamp that counts how often it was switched and prints the
count at BeginPlay.

1. **Check the name is free.** `blueprint_query list`, `path_filter:
   "/Game/Blueprints"`, `name_filter: "BP_Lamp"`. `create` does not check.
2. **Create.** `blueprint_modify create`, `package_path: "/Game/Blueprints"`,
   `blueprint_name: "BP_Lamp"`, `parent_class: "Actor"`. Then
   `blueprint_query get_components` to see the root (a new Actor Blueprint has
   a default scene root, unverified) and `find_nodes`, `node_class:
   "K2Node_Event"`, for the disabled placeholder events.
3. **Components.** `add_component`, `component_class: "StaticMeshComponent"`,
   `component_name: "Shade"`, `attach_to` the root's name from step 2; then
   `component_class: "PointLightComponent"`, `component_name: "Bulb"`,
   `attach_to: "Shade"`. Set template values with `set_component_default`:
   `component_name: "Bulb"`, `property: "Intensity"`, `value: 5000`;
   `component_name: "Shade"`, `property: "StaticMesh"`, `value:
   "/Engine/BasicShapes/Cone.Cone"` (an object path; unverified that this
   engine asset exists in every install).
4. **Variable and its default.** `add_variable`, `variable_name:
   "SwitchCount"`, `variable_type: "int"`; then `set_cdo_default`, `property:
   "SwitchCount"`, `value: 0`.
5. **Function.** `add_function`, `function_name: "Toggle"`. Build its body in
   one call: `add_nodes` with `graph_name: "Toggle"`, `is_function_graph:
   true`, nodes `VariableGet SwitchCount`, `CallFunction Add_IntInt`
   (`target_class: "KismetMathLibrary"`, `pin_values: {"B": "1"}`) and
   `VariableSet SwitchCount`. Wire: the `FunctionEntry` node (its ID from
   `find_nodes` in that graph) exec → the setter; getter `SwitchCount` → `A`
   of the add; add `ReturnValue` → the setter's `SwitchCount` input. Take the
   pin names from the `pins` in the `add_nodes` result; do not guess them.
   Switching the `Bulb` itself needs a reference to the component, which
   `VariableGet` cannot produce (section `graphs`): that part is a manual
   step or C++ (section "What to do when a node type is missing").
6. **Event graph.** Wire the placeholder `ReceiveBeginPlay` (step 2) to a
   `CallFunction Toggle` (`target_class: "/Game/Blueprints/BP_Lamp.BP_Lamp_C"`)
   and on to a `PrintString` whose `InString` is fed by `Conv_IntToString`
   (`KismetStringLibrary`) from a `SwitchCount` getter. After the first wire,
   the placeholder's ID is gone: `find_nodes` again (section `graphs`,
   "Pre-placed events").
7. **Check every wire.** `add_nodes` drops failed connections silently:
   `verify_connection` for each wire, or `get_exec_chain` from the BeginPlay
   node, whose `chain_summary` shows the order the nodes run in.
8. **Compile and read the result** (`compile`; section "Verify a change").
9. **Lay out once** (`layout_graph`), then **save** (`asset save_asset`,
   `asset_path: "/Game/Blueprints/BP_Lamp"`).

## 2. Reacting to an Input Action in a Pawn or Character

Goal: `BP_Hero` (parent `Character`) jumps on `IA_Jump`.

1. The Input Action and the Mapping Context exist (skill `ue-enhanced-input`:
   `create_input_action`, `create_mapping_context`, `add_mapping`).
2. `add_nodes` in the event graph: `{"type": "EnhancedInputAction",
   "params": {"action_path": "/Game/Input/IA_Jump"}}` and `{"type":
   "CallFunction", "function": "Jump"}` with `target_class:
   "/Script/Engine.Character"` (a member of the Blueprint's own class, so the
   `self` pin stays unwired). Connection: `from_pin: "Triggered"` (or
   `Started` for a single press) → the call's exec input. Name the pin: the
   first exec output of the action node is not `Triggered`.
3. Compile, `verify_connection` with `source_pin: "Triggered"`, save.

**The node fires only when two things hold at runtime** (Epic's Enhanced
Input page, linked below, and the UE 5.8.4 source):

- **A mapping context holding the action is on the local player's Enhanced
  Input subsystem.** The usual Blueprint for that, "Get Enhanced Input Local
  Player Subsystem" → "Add Mapping Context" at BeginPlay, **cannot be built
  with these tools**: the subsystem getter is a special node
  (`K2Node_GetSubsystemFromPC`), not a function call, and the library
  functions behind it are internal and return a type the `AddMappingContext`
  target does not accept (`SubsystemBlueprintLibrary.h`,
  `EnhancedInputSubsystemInterface.h`, UE 5.8.4 source). Three routes that
  work without that node:
  - **Default Mapping Contexts** in Project Settings → Engine → Enhanced
    Input: "Enable Default Mapping Contexts" is on by default
    (`EnhancedInputDeveloperSettings.cpp:18`, UE 5.8.4); add the context, and
    it is added to every player's subsystem. The settings are the
    `UEnhancedInputDeveloperSettings` class (config `Input`, so
    `Config/DefaultInput.ini`, section
    `[/Script/EnhancedInput.EnhancedInputDeveloperSettings]`, keys
    `bEnableDefaultMappingContexts` and `DefaultMappingContexts` with
    `InputMappingContext`, `Priority`, `bAddImmediately`,
    `bRegisterWithUserSettings`). Read from the UE 5.8.4 source; which
    engine version added it is unverified. The editor reads `DefaultInput.ini` at start: after editing it, restart the editor (skill `unreagent-iterate`); whether a running editor picks the edit up is unverified. Skill
    `ue-enhanced-input` owns this.
  - **A C++ base class** that adds the context in `BeginPlay` (the game code
    in `get_ue_context` category `enhanced_input` shows it), with the
    Blueprint reparented onto it (`reparent`).
  - **A person adds the two nodes in the editor**; the agent then wires the
    rest and verifies.
- **The actor receives input.** A Pawn or Character possessed by the player
  controller does (unverified beyond common practice); another actor needs
  `EnableInput` (`CallFunction`, `target_class: "/Script/Engine.Actor"`,
  `function: "EnableInput"`, with a player controller from
  `GetPlayerController` wired into `PlayerController`).

**Test it in play**: skill `ue-pie-testing` starts PIE and injects the
action (`gameplay_debug`). Injection hands the engine the action's value, not
a key: it skips the key mapping and the mapping context
(`EnhancedPlayerInput.cpp:284-291`, `:646-667`, UE 5.8.4), so it tests the
graph, not the mapping; a real key test needs the mapping context above.

## 3. A level-wide setting in the Construction Script

The construction script is the function graph `UserConstructionScript`
(`graph_name: "UserConstructionScript"`, `is_function_graph: true`); its
entry node is the `FunctionEntry` node in that graph. It runs when an
instance is created (Epic, "Construction Script", linked below), in the
editor also when the instance is placed, moved or edited (unverified for
each case); BeginPlay runs only when play starts. Put what shapes the actor
in the editor there (a mesh scale from a variable), and what needs the
running game (other actors, the player, timers) at BeginPlay.

## Verify a change

In order, stopping at the first failure:

1. **The call's own result.** A `blueprint_modify` call that compiled shows
   `compiled: true` and `compile_status` (section `modify`). A failed compile
   is an error whose message carries the compiler output, and the edit is
   still in the Blueprint.
2. **Read back what you changed.** `inspect` (variables, functions),
   `get_components`, `get_defaults` with `properties`, `get_node` for pin
   values, `verify_connection` or `get_exec_chain` for wiring.
3. **`compile` once more at the end**; `compile_status` must be `UpToDate`
   (or `UpToDateWithWarnings` with warnings you have read).
4. **Save** with `asset save_asset`. Then, for behaviour: start PIE (skill
   `ue-pie-testing`), and read the output log for your `PrintString` text and
   for Blueprint runtime errors ("Accessed None …"); skill `unreagent-iterate`
   covers starting the editor, reading the log and the build loop.
5. **A Blueprint that depends on new C++** compiles only after the C++ is
   built and the editor restarted or live-coded (skill `unreagent-build`).

## What to do when a node type is missing

The two tools cannot build casts, custom events, event dispatchers,
interface calls, timelines, macros, `SpawnActorFromClass`, `CreateWidget`,
subsystem getters, function parameters or variable flags (sections `graphs`
and `modify`). In order of preference:

1. **A library function instead of a special node.** Many special nodes have
   a plain function behind them or beside them: `IsValid`
   (`KismetSystemLibrary`) for the `IsValid` macro, `MakeVector` /
   `BreakVector` for the struct nodes, `GetPlayerController`,
   `GetPlayerCharacter`, `GetGameMode` (`GameplayStatics`) for a typed
   reference that would otherwise need a cast (the return type is still the
   base class).
2. **Move the logic into C++** where it needs casts, interfaces, dispatchers
   or many nodes: a `UFUNCTION(BlueprintCallable)` on a C++ base class that
   the Blueprint calls with one `CallFunction` node, or a
   `BlueprintImplementableEvent` the Blueprint answers with an `Event` node
   (section `decisions`).
3. **Hand the step to a person** with the exact node and wiring, and verify
   afterwards with the queries.

Do not reach for `execute_script` (Python) to place graph nodes as a way
around the tools: the editor's Python API for graph editing is not covered
by any shipped skill, and nothing here verifies it (skill `ue-python`).

## Sources

- Epic, Enhanced Input:
  https://dev.epicgames.com/documentation/en-us/unreal-engine/enhanced-input-in-unreal-engine
  (mapping contexts are applied through the local player's Enhanced Input
  subsystem).
- Epic, Construction Script:
  https://dev.epicgames.com/documentation/en-us/unreal-engine/construction-script-in-unreal-engine
- Engine facts without a link: UE 5.8.4 source, read and not run; the toolkit
  targets UE 5.6 and 5.7.
