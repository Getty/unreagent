---
name: ue-lighting
description: >
  Spawn and edit lights, the sky light, the sky atmosphere, height fog and
  post-process volumes in the open level with the lighting tool. Use before
  you light a scene or grade its look, or when a lighting call succeeded and
  the level looks the same.
toolkit_tools: [lighting]
---

# UE lighting and post-processing: lighting

The UE LLM Toolkit plugin's **`lighting`** tool works on the lighting actors of
the level open in the editor: point, spot, directional and rect lights, the
sky light, the sky atmosphere, exponential height fog and post-process volumes.
Through the plugin's MCP bridge it is listed as `unreal_lighting`. It works
only while the editor is running and the plugin is reachable;
`unreagent-iterate` covers the wait for the plugin.

This skill was written from the toolkit's C++ source (`MCPTool_Lighting.cpp`,
`LightingEditor.cpp`, at toolkit commit c1b6709). It has not been exercised
against a running editor. Engine facts marked "UE 5.8.4 source" were read in
that source, not run; the toolkit targets UE 5.6 and 5.7.

## Sections — load the one your task needs

| Section | Holds | Load it |
|---|---|---|
| `lumen-scene` | the end-to-end workflow: sun, sky light, atmosphere, fog, a global post-process volume, then the check with `capture_viewport`; what the tool cannot set on the way (mobility, real-time capture) | before you light a scene from scratch or fix one that is too dark, flat or blown out |
| `exposure` | auto vs manual exposure, the units of every exposure key (EV100 or cd/m2, stops, f-stops per second), a fixed exposure with this tool alone, the labelled paths to true manual mode | before you send any `exposure_*` key, or when brightness changes with the camera |
| `rendering-paths` | Lumen vs MegaLights vs baked lighting: decision rules, what each needs in the project settings, status per UE 5.6 / 5.7 / 5.8, how to read a project's settings | before you choose how a level is lit, or when a light has no effect on bounce or shadows |
| `operations` | each operation in detail: `list_lights`' type order, `inspect_light`'s traps, `spawn_light`'s parameters, the four set operations, the post-process warnings | before an operation the recipes below do not show |
| `properties` | every key of the `properties` and `settings` objects, per operation, with its JSON type, colour scale, units and source line | before you write a `properties` or `settings` object |
| `inspect-light` | the fields `inspect_light` returns, per actor type | when you need a field's name from its result |
| `post-process` | `inspect_post_process`, `set_post_process`, `spawn_post_process_volume` in full: parameters, result fields, errors | before you call one of the three beyond the recipe below |
| `failures` | what you see, what it means, what to do; which actor each set operation accepts | when a call returns an error, or succeeded and the level looks the same |

## Must not get wrong

- **A key the tool does not know is ignored without an error.** The tool's own
  description names no keys. Take every key from section `properties`, and
  compare `properties_set` / `settings_applied` with what you sent.
- **Colour keys do not share one scale, and the wrong one is not an error.**
  `ground_albedo` and `volumetric_fog_albedo` take bytes, 0–255, and come out
  black when given 0–1; a 0–1 colour key given 0–255 comes out far too bright.
  Section `properties` says which scale each key takes.
- **`intensity`'s unit depends on how the light was placed.** A point, spot
  or rect light spawned by `spawn_light` or `spawn_actor` is **unitless**:
  both call the world's `SpawnActor`, and only the editor's placement path
  (the actor factory) switches such a light to the project's default units,
  candela unless changed. A light placed by hand is therefore in candela, and
  the same number is 625 times brighter there (1 cd = 625 unitless;
  `ComputeLightBrightness`, UE 5.8.4 source). `inspect_light` does not report
  the units. A directional light's `intensity` is lux. Section `properties`.
- **Nothing is saved and nothing can be undone.** Save the level (skill
  `ue-levels`).
- **`extent` on `spawn_post_process_volume` has no effect.** Pass
  `infinite: true` for a volume that grades the whole level.

## Recipes

Argument names are exact. `<name>` stands for an actor's object name or label.
For a whole outdoor scene, load section `lumen-scene` instead.

**Add a light**

1. `lighting` `{"operation": "spawn_light", "light_type": "PointLight", "location": {"x": 0, "y": 0, "z": 300}, "label": "KeyLight", "properties": {"intensity": 5000, "attenuation_radius": 1200}}`.
   Note `actor_name` in the result.
2. `lighting` `{"operation": "inspect_light", "actor_name": "<actor_name>"}`.
   The spawn result does not say which `properties` keys were applied; this
   read does.
3. Save the level (skill `ue-levels`).

**Change a light that exists**

1. `lighting` `{"operation": "list_lights", "type_filter": "SpotLight"}` for
   the names.
2. `lighting` `{"operation": "set_light_properties", "actor_name": "<name>", "properties": {"color": {"r": 255, "g": 180, "b": 120}, "outer_cone_angle": 35}}`.
3. Compare `properties_set` in the result with the keys you sent.
4. To move or aim the light, use `move_actor` (skill `ue-level-actors`). This
   tool sets no transform after the spawn.

**Sky and fog**

`lighting` cannot spawn a sky light, a sky atmosphere or a fog actor. Place
them with `spawn_actor` (skill `ue-level-actors`), then `set_sky_light`,
`set_sky_atmosphere`, `set_fog` with their keys from section `properties`.
Pass `"recapture": true` in the `set_sky_light` call that changes the sky.

**Grade the whole level**

1. `lighting` `{"operation": "spawn_post_process_volume", "infinite": true, "label": "GlobalGrade", "settings": {"exposure_compensation": 0.5, "vignette_intensity": 0.3}}`.
2. `lighting` `{"operation": "inspect_post_process", "actor_name": "GlobalGrade"}`.
   The spawn result does not list the settings it applied; this read shows
   every setting whose override is on.
3. Later: `lighting` `{"operation": "set_post_process", "actor_name": "GlobalGrade", "settings": {"saturation": 1.1}}`,
   then compare `settings_applied` with what you sent.

**See the result**

Stop PIE, then `capture_viewport` with your own `camera_location` and
`camera_rotation` (skill `ue-viewport`), from at least two angles; keep the six
values for a before/after pair. Nothing in this tool reports what the scene
looks like. Auto exposure adapts over time, so compare pictures taken the
same way (section `exposure`).

## Before the first call

- **Result shape.** Success is the message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope, and the tool's own fields sit under its `data` key (skill
  `ue-tasks`). The tool's data repeats `success` and `message`. Failure is
  `Error: <message>` with no data.
- **The editor world.** Every operation acts on the level open in the editor,
  never on a Play-In-Editor session. Without one it fails with `Editor not
  available. Is the Unreal Editor running with UELLMToolkit plugin loaded?` or
  `No active world. Open a level in the editor first`.
- **`operation` is lowercased**, and these aliases are accepted: `inspect`,
  `info`, `get_info` for `inspect_light`; `list`, `find` for `list_lights`;
  `spawn` for `spawn_light`; `set_properties` for `set_light_properties`;
  `set_pp`, `inspect_pp` for the two post-process operations; `spawn_pp`,
  `spawn_ppv` for `spawn_post_process_volume`. Anything else returns
  `Unknown operation: '<op>'. Valid: inspect_light, list_lights, …`, with
  ` Did you mean '<op>'?` appended when a valid name is close.
- **`name` and `actor_label` are aliases of `actor_name`**, in every
  operation. On the two spawn operations that makes them useless: `actor_name`
  is not read there, so `"name": "Sun"` names nothing. Use `label`.

## What a success does not prove

- **A key was applied only if the result lists it.** The set operations
  succeed with `Set 0 properties on '<name>'` when no key matched. Unknown or
  misspelt keys, keys of another light type, and values the engine's JSON
  reader cannot read as the listed type are skipped in silence. The two spawn
  operations return no such list at all; inspect afterwards.
- **Values are not validated or clamped**, apart from the byte colours. A
  negative intensity or a cone angle of 400 is written as given.
- **Fields are written directly.** Most keys are raw field writes followed by
  a render-state refresh; the Details panel's property-changed notification is
  not sent. Post-process settings get no refresh call at all. Whether the
  viewport shows the change at once has not been verified: capture to check.
- **No lighting build, no mobility.** The tool builds no lighting and sets no
  mobility. In a level with baked lighting a changed light says nothing about
  the lightmaps (section `rendering-paths`).
- **The sky light is not recaptured** unless the call passes
  `"recapture": true`.
- A spawn marks the new actor's package and the world's package dirty; a set
  marks the actor's package dirty. No operation writes to disk; an editor
  restart drops unsaved changes (`unreagent-iterate`).

## Addressing an actor

`actor_name` takes the object name (`PointLight_0`) or the World Outliner
label. The tool takes the **first** actor whose label or object name equals the
string, ignoring case. Where labels repeat, use the object name. The string's
characters are not validated, so a label such as `Lamp (2)` is accepted. An
empty `actor_name` gives `Missing required parameter: actor_name`; no match
gives `Actor not found: <name>`.

## Operations

| Operation | Required | Optional | Writes | Result lists what was applied |
|---|---|---|---|---|
| `list_lights` | — | `type_filter` | no | — |
| `inspect_light` | `actor_name` | — | no | — |
| `spawn_light` | `light_type` | `location`, `rotation`, `label`, `properties` | yes | no: inspect |
| `set_light_properties` | `actor_name`, `properties` | — | yes | `properties_set` |
| `set_sky_light` | `actor_name`, `properties` | — | yes | `properties_set` |
| `set_sky_atmosphere` | `actor_name`, `properties` | — | yes | `properties_set` |
| `set_fog` | `actor_name`, `properties` | — | yes | `properties_set` |
| `inspect_post_process` | `actor_name` | — | no | — |
| `set_post_process` | `actor_name`, `settings` | — | yes | `settings_applied` |
| `spawn_post_process_volume` | — | `location`, `extent`, `infinite`, `label`, `settings` | yes | no: inspect |

The tool's header comment and its description list the same ten operations.
Load section `operations` before one the recipes do not show. Three traps
from it:

- **`inspect_light`'s read names are not the write names**: `light_color`
  reads back in bytes and is written as `color`; `source_type` reads
  `CapturedScene` and is written as `captured_scene`. Sending a read name or
  value back is ignored in silence.
- **Each set operation accepts only its own kind of actor**; any other is an
  error naming its class (section `failures`).
- **`set_sky_light` can fail half-way**: a `cubemap` that does not load fails
  the call after `intensity` and `color` are written.

## lighting or the actor tools

Both edit the same placed actors, mark the level dirty and save nothing.

| Job | Use |
|---|---|
| spawn a point, spot, directional or rect light with a label and values in one call | `lighting` `spawn_light` |
| spawn a sky light, sky atmosphere, height fog or any other actor | `spawn_actor` (`ue-level-actors`) |
| set a value that has a key in section `properties` | `lighting`: fixed key names, and the result lists what was applied |
| set a property with no key there: mobility, a sky light's real-time capture, a light's MegaLights flags | `set_property` (`ue-level-actors`), with the property's dot path; sections `lumen-scene` and `rendering-paths` name the paths |
| move, rotate, scale or delete a lighting actor | `move_actor`, `delete_actors` (`ue-level-actors`) |
| list lighting actors by kind | `lighting` `list_lights` |
| a setting no tool reaches (manual exposure method, project rendering settings) | `execute_script` (skill `ue-python`) or the project's config; sections `exposure`, `rendering-paths` |

## Background in the plugin

The plugin ships no context or domain document on lighting. Its
`Resources/mcp-bridge/contexts/actor.md`, served by `unreal_get_ue_context`
with category `actor`, covers actors in general (skill `ue-context`).

## See also

- `ue-level-actors`: `spawn_actor`, `move_actor`, `delete_actors`,
  `set_property` on the same actors.
- `ue-levels`: opening and saving the level.
- `ue-viewport`: `capture_viewport`, to look at the result.
- `ue-assets`: `asset_search` for cubemap and LUT paths, `asset save_all`.
- `ue-python`: `execute_script`, for what no tool reaches.
- `unreagent-files`: `read_file` on `Config/DefaultEngine.ini`.
- `ue-tasks`: the task envelope and bridge timeouts.
- `unreagent-iterate`: editor restarts and unsaved changes.
