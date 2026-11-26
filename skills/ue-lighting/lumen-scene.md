# lighting: an outdoor scene lit for Lumen

The end-to-end workflow: a sun, a sky atmosphere, a sky light, height fog and
one global post-process volume, then the check. The tool calls are read in the
toolkit source (commit c1b6709); engine defaults named "UE 5.8.4 source" were
read there, not run. The whole workflow has not been exercised against a
running editor. Numbers in the calls are starting points, not tuned values.

## Before you start

1. **The project uses Lumen.** Section `rendering-paths` says how to read
   that from `Config/DefaultEngine.ini`. With baked lighting instead, the
   steps below still place the actors, but what you see depends on a
   lighting build this toolkit cannot run.
2. A level is open in the editor (skill `ue-levels`) and PIE is stopped
   (skill `ue-pie-testing`): `capture_viewport` shows the PIE view while a
   session runs.
3. `lighting` `{"operation": "list_lights"}`. **Reuse what is there**: the
   editor's default levels often ship a directional light, a sky light, an
   atmosphere and fog already (not verified per template). Two of a kind
   (two suns, two sky lights) give results this skill cannot predict; remove
   extras with `delete_actors` (skill `ue-level-actors`) after asking the user.

## The steps

| # | Call | Why |
|---|---|---|
| 1 | `lighting` `{"operation": "spawn_light", "light_type": "DirectionalLight", "label": "Sun", "rotation": {"pitch": -40, "yaw": 30, "roll": 0}, "properties": {"intensity": 10, "cast_shadows": true}}` | the sun. Its direction is the rotation; a negative pitch aims it down. `intensity` is lux; 10 is the engine's default. The component's "atmosphere sun light" flag is on by default (UE 5.8.4 source, `DirectionalLightComponent.cpp`), so the atmosphere takes this light as its sun with no further call |
| 2 | `spawn_actor` `{"class": "SkyAtmosphere"}` (skill `ue-level-actors`) | the sky colour and the sun disc. Defaults are a usable Earth-like sky; tune with `set_sky_atmosphere` (section `properties`) |
| 3 | `spawn_actor` `{"class": "SkyLight"}` | the ambient light from the sky, which Lumen feeds into its final gather |
| 4 | `lighting` `{"operation": "set_sky_light", "actor_name": "<sky light>", "properties": {"source_type": "captured_scene", "intensity": 1, "recapture": true}}` | capture the sky you just built. **Repeat `"recapture": true` after every change to the sun or the atmosphere**, unless real-time capture is on (below) |
| 5 | `spawn_actor` `{"class": "ExponentialHeightFog"}`, then `lighting` `{"operation": "set_fog", "actor_name": "<fog>", "properties": {"fog_density": 0.02, "volumetric_fog": true}}` | depth and light shafts. The fog's height is the actor's location: `move_actor` |
| 6 | `lighting` `{"operation": "spawn_post_process_volume", "infinite": true, "label": "GlobalPP", "settings": {"exposure_compensation": 0}}` | one place for exposure and grading. **Load section `exposure` before you set any other exposure key** |
| 7 | the check, below | — |
| 8 | save the level (skill `ue-levels`) | nothing above is saved |

After each `spawn_actor`, take the actor's name from its result for the next
call. After each `lighting` set call, compare `properties_set` /
`settings_applied` with the keys you sent; after the two spawns, inspect.

## What the lighting tool cannot set on the way

These are properties of the light components, reachable only through
`set_property` (skill `ue-level-actors`; load its section `set-property`
first) or `execute_script` (skill `ue-python`). Neither path is verified for
these properties; check each with a capture.

| Want | Default (UE 5.8.4 source) | Path |
|---|---|---|
| sky light follows sky changes by itself | real-time capture **off** (`SkyLightComponent.cpp`) | `set_property` `{"actor_name": "<sky light>", "property": "LightComponent.bRealTimeCapture", "value": true}`: the sky light actor's component property is `LightComponent` (`SkyLight.h`). Without it, keep sending `"recapture": true` |
| a sun or sky that moves at runtime (time of day) | lights and sky light spawn **Stationary** | `Mobility` is a `TEnumAsByte`, so `set_property` takes the number: 0 Static, 1 Stationary, 2 Movable, on `LightComponent.Mobility`. This is a raw field write without the engine's `SetMobility`; the Python sketch below goes through it |
| point, spot or rect light in candela | spawned **unitless** (skill body) | `IntensityUnits` is an `enum class`, which `set_property` cannot set. Either convert (candela × 625 = unitless) and keep the light unitless, or set the units with Python |
| a light left out of MegaLights | allowed | `LightComponent.bAllowMegaLights` `false` (section `rendering-paths`) |

Static lights are not supported with Lumen: their contribution is disabled
(Epic, "Lumen Global Illumination and Reflections"). Do not set Mobility 0 in
a Lumen project.

Python sketch, **not exercised against an editor** (skill `ue-python` for how
to run it; API names from Epic's Python API reference):

```python
actors = unreal.get_editor_subsystem(unreal.EditorActorSubsystem).get_all_level_actors()
sky = next(a for a in actors if a.get_actor_label() == "SkyLight")  # your label
comp = sky.get_component_by_class(unreal.SkyLightComponent)
comp.set_mobility(unreal.ComponentMobility.MOVABLE)  # re-registers the component
comp.set_real_time_capture(True)
```

`set_mobility` and `set_real_time_capture` are the components' own setters
(`SceneComponent.set_mobility`, `SkyLightComponent.set_real_time_capture` in
the Python API reference). Save the level afterwards.

## The check

1. `capture_viewport` with `camera_location` and `camera_rotation`, all six
   values (skill `ue-viewport`), from two angles: one towards the sun, one
   away from it. Keep the values for later comparisons.
2. Auto exposure adapts over time and per view. Two pictures of one scene
   may differ in brightness for that reason alone. For a fair comparison,
   fix the exposure first (section `exposure`, "A fixed exposure with this
   tool").
3. `get_output_log` with a filter such as `Lumen` or `Warning` (skill
   `ue-pie-testing`) for rendering warnings after the changes.
4. Read back: `inspect_light` on each actor, `inspect_post_process` on the
   volume.

| What the picture shows | Likely cause | What to do |
|---|---|---|
| everything nearly black | exposure, or a sky light that never captured | section `exposure`; `set_sky_light` with `"recapture": true` |
| a placed point light is invisible | its intensity is unitless (×1/625 of candela) | raise it, or see the units row above |
| shadows but no bounce light, flat shadowed areas | the project's GI method is not Lumen, or the light is Static | section `rendering-paths` |
| black or missing sky | no sky atmosphere, or the sun's atmosphere flag is off | step 2; `inspect_light` on the sun |
| the sky light's colour does not follow the sun | no recapture since the change | step 4 again, or real-time capture |
| the picture is grey and washed out | fog too dense | lower `fog_density` |
| a change is not visible at all | the key was not applied, or the viewport did not redraw | compare `properties_set`; capture with an explicit camera, which redraws (skill `ue-viewport`) |

Pass and fail here are judged from pictures; say in your report what you
looked at, and that the brightness judgement is yours.
