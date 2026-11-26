# UE Niagara: what the niagara tool cannot do

For each gap: the decision rule and where it goes. A Python route through
`execute_script` (skill `ue-python`) names calls from Epic's Python API
reference for UE 5.6; **none has been run against an editor.** Run one only
when the user agrees, and verify the effect afterwards (section `workflows`,
"Verify").

## Decision rule

| The user wants | Route |
|---|---|
| a different value for something the system exposes as a `User.` parameter, everywhere | `set_parameter` on the system (section `workflows`) |
| a different value for one placed effect only | a per-component override: below |
| a property that is not a user parameter (spawn rate inside a module, a curve, a renderer setting) | authoring: the user in the Niagara editor, or ask them to expose it as a user parameter first |
| a new system, a new emitter, modules, renderers, events | authoring: the user in the Niagara editor |
| an effect in the level that stays | a placed Niagara actor (section `workflows`, 4) |
| a quick look at a system | `spawn_system` (section `spawn`) |
| an effect triggered by gameplay | gameplay code or a Blueprint spawning it at run time: skill `ue-blueprints` |
| a different look of the particles themselves (colour ramp in the material, texture) | the renderer's material: skill `ue-materials` |

Say early when a request needs authoring: no tool here can add what the
system does not expose.

## Authoring systems and emitters

The tool cannot create a system or an emitter, add, remove, enable or disable
emitters, modules or renderers, change module inputs, curves, events, bounds,
scalability or the simulation target. `inspect` and `get_emitter_info` show
some of these; nothing writes them. This skill names no Python route for
Niagara graph authoring, and none has been checked: tell the user what to
change in the Niagara editor, naming the emitter and renderer from
`inspect`.

## Exposing a new user parameter

Adding a `User.` parameter, and wiring a module input to it, is authoring.
Ask the user; once it exists, `set_parameter` reaches it.

## One placed effect's value

Every Niagara component can override user parameters for itself. The tool has
no operation for that: `set_parameter` writes the system's defaults.

- `set_property` (skill `ue-level-actors`) cannot reach it: the overrides
  live in the component's parameter store, not in plain properties.
- Python, on the component of a placed actor (not exercised; whether the
  value is kept when the level is saved has not been verified):

  ```python
  import unreal
  actor = ...  # the placed NiagaraActor, found by label
  comp = actor.get_component_by_class(unreal.NiagaraComponent)
  comp.set_variable_float("SpawnRate", 50.0)
  comp.set_variable_linear_color("FlameColor", unreal.LinearColor(0.2, 0.4, 1.0, 1.0))
  ```

  `set_variable_float(variable_name, value)`, `set_variable_int`,
  `set_variable_bool`, `set_variable_vec3`, `set_variable_linear_color`
  (`NiagaraComponent`, Python API 5.6). Whether the name needs the `User.`
  prefix has not been verified here.
- Otherwise the user sets it in the actor's Details panel, under the
  component's user parameters.

## Swapping the system on a placed actor

`set_property` on `NiagaraComponent.Asset` writes the property without the
engine's `SetAsset` (section `workflows`, 4, Route A). Python's
`NiagaraComponent.set_asset(asset, reset_existing_override_parameters=True)`
is the engine's own call (Python API 5.6; not exercised); it resets the
component's overrides by default.

## Removing a spawn_system preview

Its component belongs to World Settings and is transient: no tool removes it.
Reopen the level after saving other work (skill `ue-levels`), or let a
non-looping system finish. Never `delete_actors` World Settings.

## Emitter assets and other content

`list` and `asset_search` with `class_filter: "NiagaraSystem"` find systems
only. Emitter assets, Niagara parameter collections, scratch-pad modules and
data channels are not read by this tool; `asset_search` with their class
finds them, `asset get_asset_info` shows their editable properties (skill
`ue-assets`), and changing them is authoring.
