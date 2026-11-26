# enhanced_input: making a mapping work in play

`enhanced_input` builds the assets. A key does nothing in play until three
more things hold, none of which this tool does:

1. the player's Enhanced Input subsystem has the mapping context **added**;
2. some Blueprint or C++ **reacts** to the action;
3. the player controller lets game input through (**input mode**, pause).

Engine facts below were read in the UE 5.8.4 source and templates under the
named files. The toolkit declares `"EngineVersion": "5.6.0"`
(`UELLMToolkit.uplugin:18`) and labels its injected API notes "UE 5.7 API
Context" (bridge `index.js`), so a 5.6/5.7 difference is possible where it
says so.

## 1. Add the mapping context

Contexts are added per local player, to
`UEnhancedInputLocalPlayerSubsystem`:
`AddMappingContext(Context, Priority, Options)`
(`EnhancedInputSubsystemInterface.h`). Three routes, pick one:

| Route | How | When |
|---|---|---|
| **Project default** (no code) | `Config/DefaultInput.ini`, section `[/Script/EnhancedInput.EnhancedInputDeveloperSettings]`, a line `+DefaultMappingContexts=(InputMappingContext="/Game/Input/IMC_Default.IMC_Default",Priority=0,bAddImmediately=True,bRegisterWithUserSettings=False)` | one context that every player always has. The form is copied from the engine's VR template config. The switch `bEnableDefaultMappingContexts` is on by default (`EnhancedInputDeveloperSettings.cpp:18`, UE 5.8.4). Only contexts under the game's own content, not a plugin's (header note). Edit the file with the launcher's file tools (skill `unreagent-files`). The editor reads `DefaultInput.ini` at start: after editing it, restart the editor (skill `unreagent-iterate`); whether a running editor picks the edit up is unverified. |
| **Player controller** (the engine templates' choice) | Blueprint: in the controller's `Event BeginPlay`, `Get Enhanced Input Local Player Subsystem` → `Add Mapping Context` (context, priority 0). C++: `SetupInputComponent`, as `TP_ThirdPersonPlayerController.cpp` does, guarded by `IsLocalPlayerController()` | the context belongs to the player whatever pawn they control |
| **Pawn / Character** | in the pawn's `Event Possessed` or `BeginPlay`: `Get Controller` → cast to `PlayerController` → its subsystem → `Add Mapping Context`; remove it on `Unpossessed` | contexts that belong to one pawn (a vehicle, a turret) |

**Through the toolkit, use the project default or C++.** The Blueprint
routes need the "Get Enhanced Input Local Player Subsystem" node, a special
node that `blueprint_modify` cannot add (skill `ue-blueprints`, section
`workflows`, read in the UE 5.8.4 source). Without a C++ module, the project
default is the one route an agent completes alone; otherwise a person adds
the two nodes and the agent wires and verifies the rest. A pawn's `BeginPlay` can run before it
is possessed; a context added there through `Get Controller` then finds no
controller. Prefer the controller or `Possessed` (unverified in play for
this skill).

**Priority**: higher priority contexts are applied first and, when their
action consumes input (`bConsumeInput`, default true on an Input Action),
block the same key in lower ones (`EnhancedInputSubsystemInterface.h`). Give
a menu or vehicle context a higher number than the default one, and remove
it when it ends.

`RemoveMappingContext` removes a context; by default one removal undoes any
number of adds (`RegistrationTrackingMode` `Untracked`;
`CountRegistrations` makes it count, `InputMappingContext.h`). The context
asset's tracking mode is set in the editor, not by this tool.

## 2. React to the action

- **Blueprint**: the `EnhancedInputAction` event node for the action (skill
  `ue-blueprints`, section `graphs`, `action_path`). It sits in the graph of
  the actor that receives input: the possessed pawn, or the player
  controller. Its exec pins are the trigger events: use `Triggered` for
  movement and look, `Started` for a one-shot (section
  `triggers-modifiers`, "What a trigger decides"); `Action Value` carries
  the value in the action's value type.
- **C++**: `UEnhancedInputComponent::BindAction(Action, ETriggerEvent::…,
  this, &Class::Fn)` in `SetupPlayerInputComponent`, as the Third Person
  template's character does. The project must use the Enhanced Input
  classes: `DefaultInputComponentClass=/Script/EnhancedInput.EnhancedInputComponent`
  and `DefaultPlayerInputClass=/Script/EnhancedInput.EnhancedPlayerInput`
  under `[/Script/Engine.InputSettings]` in `DefaultInput.ini`. The editor
  switches a project still on the legacy classes on load
  (`EnhancedInputEditorModule.cpp`), but not one on custom classes.

Character movement itself (`Add Movement Input`, `Add Controller Yaw Input`,
`Jump`) is the character's: skill `ue-character`.

## 3. Let input through

- **Input mode**: `ui_only` blocks game input. After a menu, set
  `game_only` or `game_and_ui` again. In PIE, `game_framework`
  `set_input_mode` does it for a test (skill `ue-game-framework`); the game
  itself calls `Set Input Mode Game Only` in its Blueprint.
- **Pause**: while the game is paused, an action triggers only when its
  Input Action has `Trigger when Paused` set (`bTriggerWhenPaused`, default
  false, `InputAction.h`; checked in `EnhancedPlayerInput.cpp`). An
  "unpause" key needs it, set in the editor: this tool cannot set it.
- **Input mode filtering** (Enhanced Input's own gameplay-tag "input modes",
  unrelated to the controller's input mode): `bEnableInputModeFiltering` in
  Project Settings → Enhanced Input is true by default in UE 5.8.4
  (`EnhancedInputDeveloperSettings.cpp`), and a context is matched against
  the project's default query unless its `InputModeFilterOptions` says
  otherwise (`InputMappingContext.h`). Which engine version introduced it,
  and what an empty default query lets through, is unverified here; look
  there when everything else checks out.

## Player-mappable keys and user settings

Rebinding by the player goes through `UEnhancedInputUserSettings`, switched
on by `bEnableUserSettings` in Project Settings → Enhanced Input, and the
mapping's player-mappable key settings. From UE 5.7 a context holds its
mappings in `DefaultKeyMappings` plus per-profile overrides
(`MappingProfileOverrides`; the old `Mappings` property is deprecated in
5.7, `InputMappingContext.h`). `enhanced_input` reads and writes the
default set only. A community report says the user settings object is not
yet available in `PlayerController::InitInputSystem` in 5.6 (Epic forum,
unverified); register contexts with user settings from `BeginPlay`.

## Verify

1. `query_context`: the mapping, key and action are there.
2. Read the Blueprint that adds the context and the one with the
   `EnhancedInputAction` node (`blueprint_query`, skill `ue-blueprints`);
   compile it, a compile error means it does not run.
3. PIE (skill `ue-pie-testing`): `gameplay_debug` injection drives the
   **action**, not the key: it skips the key mapping, the context and their
   triggers and modifiers; the Input Action asset's own triggers and
   modifiers still apply (`EnhancedPlayerInput.cpp:646-667`, `:683-694`, UE
   5.8.4). So it tests step 2 and 3 but not the mapping or step 1. A key is tested only by a person pressing it, or by reading the
   context's registration in the log.
4. `get_output_log` after PIE: Enhanced Input warnings and
   `Mixing AGameStateBase with AGameMode` style framework errors show there.
