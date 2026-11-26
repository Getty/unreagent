# UE Niagara: workflows

Step lists with the exact calls. Tool names are the bare toolkit names; the
bridge lists them as `unreal_<name>`. Every result's fields sit under the task
envelope's `data` key (skill `ue-tasks`). None of these sequences has been run
against an editor; each step names the check that tells you it worked.

## 1. Inspect a system

1. Find it: `niagara` `list` `{"folder_path": "/Game/FX/"}`, or `asset_search`
   `{"class_filter": "NiagaraSystem", "name_pattern": "Fire"}` (skill
   `ue-assets`).
2. `niagara` `inspect` `{"asset_path": "/Game/FX/NS_Fire"}`: the emitters
   (enabled, CPU/GPU, renderers) and the user parameters with their types and
   defaults.
3. For one emitter's renderers: `get_emitter_info`
   `{"asset_path": "/Game/FX/NS_Fire", "emitter_name": "Flames"}`.
4. Report what can be tuned: the `user_parameters`. Anything else the user
   asks for is an authoring change (section `limits`).

## 2. Tune a system's user parameters

1. `get_parameters` `{"asset_path": "/Game/FX/NS_Fire"}`. Keep the old
   `value`s from this result: they are your undo.
2. One `set_parameter` per parameter, the value in the form for its `type`
   (section `set-parameter`):
   - `{"asset_path": "/Game/FX/NS_Fire", "parameter_name": "SpawnRate", "value": 200}`
   - `{"asset_path": "/Game/FX/NS_Fire", "parameter_name": "FlameColor", "value": {"r": 1, "g": 0.35, "b": 0.05, "a": 1}}`
   - `{"asset_path": "/Game/FX/NS_Fire", "parameter_name": "WindDirection", "value": {"x": 1, "y": 0, "z": 0}}`
3. `get_parameters` again. Check each value: numbers as sent (an `int`
   rounded), vectors and colours as strings with every component you meant,
   and `a` not 0.
4. `asset` `save_asset` `{"asset_path": "/Game/FX/NS_Fire"}`.
5. Verify (section 5).

The change is the asset's default: every placed use of the system that does
not override the parameter changes too. Tell the user so when the system is
used in more than one place (`asset_referencers`, skill `ue-assets`).

## 3. Preview a system at a place

1. `niagara` `spawn_system`
   `{"asset_path": "/Game/FX/NS_Fire", "location": {"x": 0, "y": 0, "z": 100}}`.
   No `label`. Check: `actor_name` is the World Settings actor, as expected.
2. Capture it (section 5) while it plays. A non-looping system with the
   default `auto_destroy` disappears once it has finished; capture right
   away, or spawn again.
3. The preview is transient and is not saved (section `spawn`).

## 4. Put an effect into the level that stays

`spawn_system` cannot do this. Two routes; neither has been run against an
editor.

**Route A, toolkit tools only.** The toolkit's own steps are read in its
source; how the engine reacts to the second step is not verified.

1. `spawn_actor`
   `{"class": "/Script/Niagara.NiagaraActor", "location": {"x": 0, "y": 0, "z": 100}, "name": "FX_Campfire"}`
   (skill `ue-level-actors`). `NiagaraActor` is the engine's actor for a
   placed system; its component is named `NiagaraComponent0`.
   Check: `actorClass` is `NiagaraActor`.
2. `set_property`
   `{"actor_name": "FX_Campfire", "property": "NiagaraComponent.Asset", "value": "/Game/FX/NS_Fire.NS_Fire"}`.
   The path reaches the actor's `NiagaraComponent` property and the
   component's `Asset` property (`UNiagaraComponent::Asset`,
   `NiagaraComponent.h`, UE 5.8.4), and the tool loads the system by path.
   **The tool writes the property directly**: it does not call the
   component's `SetAsset` or a change notification, so the component may not
   start the new system until it is re-registered. Not verified.
3. `asset` `save_all`, then verify (section 5). If nothing shows, reopen the
   level (skill `ue-levels`) and capture again: a loaded component starts
   from its saved `Asset`. If it still shows nothing, use Route B or tell
   the user.

**Route B, Python** through `execute_script` (skill `ue-python`), named in
Epic's Python API for UE 5.6, not exercised:

```python
import unreal
system = unreal.EditorAssetLibrary.load_asset("/Game/FX/NS_Fire")
actors = unreal.get_editor_subsystem(unreal.EditorActorSubsystem)
actor = actors.spawn_actor_from_object(system, unreal.Vector(0, 0, 100))
print(actor.get_name(), actor.get_actor_label())
```

`EditorActorSubsystem.spawn_actor_from_object(object_to_use, location, rotation=…, transient=False) -> Actor`
places an actor for an asset as dragging it into the level does. Then
`save_all` and verify.

A placed actor is an ordinary level actor: move it with `move_actor`, remove
it with `delete_actors` (skill `ue-level-actors`).

## 5. Verify

- **Data**: `get_parameters` for the defaults; `get_level_actors` for a
  placed actor (skill `ue-level-actors`).
- **Picture**: `capture_viewport` (skill `ue-viewport`) with the camera
  aimed at the effect, all three keys of each object given:
  `{"camera_location": {"x": -400, "y": 0, "z": 150}, "camera_rotation": {"pitch": -5, "yaw": 0, "roll": 0}}`
  for an effect at the origin. Capture before and after a change with the same
  camera.
- **The level viewport may not be animating.** Whether effects advance in an
  editor viewport that is not in real-time mode, or while the editor window
  is in the background, has not been verified. A capture that shows nothing,
  or the same frame twice, is not proof the effect is broken; capture again,
  or check during PIE.
- **In PIE** a placed actor plays as in the game: start PIE (skill
  `ue-pie-testing`) and capture without `asset_path`; the PIE view is taken.
  A `spawn_system` preview belongs to the editor's world, not to PIE.
- **The log**: `get_output_log` (skill `ue-pie-testing`) for Niagara
  compile or GPU-simulation warnings after a change.

## 6. Undo by hand

No operation undoes. Put old defaults back with `set_parameter` from the
values you read first, and save. Remove a placed actor with `delete_actors`.
A `spawn_system` preview goes away when the level is reopened.
