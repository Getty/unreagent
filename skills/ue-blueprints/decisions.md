# Blueprint, C++ or Python: which to use

Epic's own position is that there is no single right choice: C++ for the
foundation, Blueprint classes on top of it, and profiling (Unreal Insights)
before converting a Blueprint to C++ for speed (Epic, "Coding in Unreal
Engine: Blueprint vs. C++", linked below). For an agent the deciding factor
is narrower: **what the tools can build and verify**.

## Decision rules

| The work | Use | Why |
|---|---|---|
| A new gameplay actor, pawn, game mode or controller with a few variables, components and straight-line logic (BeginPlay, input, overlap, a timer via `Delay`) | Blueprint, with `blueprint_modify` | every piece is an operation or a node type the tool has; compile and queries verify it |
| Tuning: default values, component settings, which mesh or class an actor uses | Blueprint defaults (`set_cdo_default`, `set_component_default`) | no graph at all; `get_defaults` reads it back |
| Logic that needs a cast, an interface, an event dispatcher, a custom event, a timeline, a loop macro, function parameters or a return value, or many nodes | C++ (a base class or a function library), called from the Blueprint by one `CallFunction` or answered by one `Event` node | the tools cannot build those nodes (section `graphs`); text code diffs, reviews and debugs better (Epic, same page) |
| A property that must be instance editable, exposed on spawn, replicated, or in a category | C++ `UPROPERTY` with those specifiers, or a person in the editor | no operation sets variable flags (section `modify`) |
| Per-frame work over many actors, or anything shown slow by profiling | C++ | Epic's guidance: profile first, then move the bottleneck |
| Batch work on assets in the editor (rename, fix up many Blueprints, read data out) | Python through `execute_script` (skill `ue-python`) for what has no tool, the tools for what has one | editor-time automation, not game logic; Python does not run in a packaged game (unverified wording; Epic's Python pages describe it as an editor scripting feature) |
| Game logic that should ship | never Python | as above |

When a Blueprint needs one thing C++ must provide, add that one thing in C++
(a `BlueprintCallable` function, a `BlueprintImplementableEvent`, a
`UPROPERTY`), build (skill `unreagent-build`), restart or live-code, and keep
the rest in the Blueprint.

## UE facts the choice rests on

Each with its source; unsourced statements are marked.

- **Construction Script vs BeginPlay.** The construction script "runs
  following the Components list when an instance of a Blueprint Class is
  created" (Epic, "Construction Script"). In the editor it also reruns when a
  placed instance is moved or edited (unverified here). BeginPlay runs when
  play starts or the actor is spawned in a running game. Shape the actor in
  the construction script; touch other actors, the player or timers at
  BeginPlay.
- **Pure and impure functions.** A pure function has no exec pins and runs
  each time a connected node reads its output; an impure one runs once, where
  the exec wire reaches it (unverified here; standard Blueprint behaviour).
  Library functions such as `MakeVector` or `Conv_IntToString` are pure.
  Functions made with `add_function` are impure, with no inputs or outputs.
- **Variable settings.** "Instance Editable" decides whether the value can be
  edited per placed instance; "Expose on Spawn" makes it settable when the
  Blueprint is spawned; replication decides whether the value is synced to
  clients, optionally with a notify callback (Epic, "Blueprint Variables").
  The tools set none of these.
- **Event dispatchers and interfaces** let Blueprints talk without hard
  references (unverified here). The tools create neither; `create` with
  `blueprint_type: "Interface"` makes an empty interface asset whose
  functions cannot be added through the tools, because `add_function` takes
  no parameters and the interface graph rules are not verified.
- **Class defaults and placed actors.** By the plugin's domain notes, a value
  written with `set_cdo_default` reaches actors spawned later, while actors
  already placed keep what the level saved; set those through skill
  `ue-level-actors` as well (unverified beyond the plugin's notes).

## UE 5.6, 5.7, 5.8

Nothing in this skill differs between the three versions as far as the
toolkit source shows: the toolkit targets 5.6 and 5.7, and the engine facts
here were read in the 5.8.4 source. The one rename that matters, float math
to `*_DoubleDouble`, dates from UE 5's switch to double precision (the
`BaseEngine.ini` redirect `KismetMathLibrary.Add_FloatFloat` →
`Add_DoubleDouble`) and holds for all three. Version-specific behaviour of
the Blueprint editor beyond that is unverified.

## Sources

- Epic, Coding in Unreal Engine: Blueprint vs. C++:
  https://dev.epicgames.com/documentation/en-us/unreal-engine/coding-in-unreal-engine-blueprint-vs-cplusplus
- Epic, Construction Script:
  https://dev.epicgames.com/documentation/en-us/unreal-engine/construction-script-in-unreal-engine
- Epic, Blueprint Variables:
  https://dev.epicgames.com/documentation/en-us/unreal-engine/blueprint-variables-in-unreal-engine
