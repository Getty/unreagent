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
`LightingEditor.cpp`, at toolkit commit 9f6a0f7). It has not been exercised
against a running editor.

This skill has four sections:

| Section | Holds | Load it |
|---|---|---|
| `properties` | every key of the `properties` and `settings` objects, per operation, with its JSON type, colour scale and source line | before you write a `properties` or `settings` object |
| `inspect-light` | the fields `inspect_light` returns, per actor type | when you need a field's name from its result, or a value you set is missing from it |
| `post-process` | `inspect_post_process`, `set_post_process` and `spawn_post_process_volume` in full: parameters, result fields, errors | before you call one of the three in any way the recipe "Grade the whole level" does not show |
| `failures` | the failure table (what you see, what it means, what to do), and which kind of actor each set operation accepts, with the error for any other | when a call returns an error, or succeeded and the level looks the same |

Section `properties` lists every key the `properties` and `settings` objects
accept, per operation, with its JSON type and the source line that reads it.
Load it before you write a `properties` or `settings` object: the tool's own
description names no keys, and **a key it does not know is ignored without an
error**.

**Colour keys do not share one scale, and the wrong one is not an error.**
`ground_albedo` and `volumetric_fog_albedo` take bytes, 0–255, and come out
black or nearly black when given 0–1; a 0–1 colour key given 0–255 comes out
far too bright. Section `properties` says which scale each colour key takes.

## Recipes

Argument names are exact. `<name>` stands for an actor's object name or label.

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
3. Compare `properties_set` in the result with the keys you sent. A key that
   is missing there was not applied.
4. To move or aim the light, use `move_actor` (skill `ue-level-actors`). This
   tool sets no transform after the spawn.

**Sky and fog**

`lighting` cannot spawn a sky light, a sky atmosphere or a fog actor. If
`list_lights` shows none, place one with `spawn_actor` (skill
`ue-level-actors`), then:

1. `lighting` `{"operation": "set_sky_light", "actor_name": "<name>", "properties": {"intensity": 1.5, "recapture": true}}`.
2. `lighting` `{"operation": "set_sky_atmosphere", "actor_name": "<name>", "properties": {"mie_anisotropy": 0.8}}`.
3. `lighting` `{"operation": "set_fog", "actor_name": "<name>", "properties": {"fog_density": 0.02, "volumetric_fog": true}}`.
4. Check `properties_set` after each call.

**Grade the whole level**

1. `lighting` `{"operation": "spawn_post_process_volume", "infinite": true, "label": "GlobalGrade", "settings": {"exposure_compensation": 0.5, "vignette_intensity": 0.3}}`.
2. `lighting` `{"operation": "inspect_post_process", "actor_name": "GlobalGrade"}`.
   The spawn result does not list the settings it applied; this read shows
   every setting whose override is on.
3. Later changes: `lighting` `{"operation": "set_post_process", "actor_name": "GlobalGrade", "settings": {"saturation": 1.1}}`,
   then compare `settings_applied` with what you sent.

**See the result**

Capture the level viewport with `capture_viewport` (skill `ue-viewport`).
Nothing in this tool reports what the scene looks like.

## Before the first call

- **Result shape.** Success is the message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope, and the tool's own fields, the ones named in this skill, sit under
  its `data` key (skill `ue-tasks`). The tool's data repeats `success` and
  `message`. Failure is `Error: <message>` with no data.
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
  is not read there, so `"name": "Sun"` or `"actor_label": "Sun"` names
  nothing. Use `label`.

## What a success does not prove

This applies to every write operation of the tool.

- **Nothing is saved.** A spawn marks the new actor's package and the world's
  package dirty; a set marks the actor's package dirty. No operation writes to
  disk. Saving: skills `ue-levels` and `ue-assets` (`asset save_all`). An
  editor restart drops unsaved changes (`unreagent-iterate`).
- **No undo.** No operation opens an editor transaction.
- **A key was applied only if the result lists it.** The set operations
  succeed with `Set 0 properties on '<name>'` when no key matched. Unknown
  keys, misspelt keys, keys that belong to another light type, and values the
  engine's JSON reader cannot read as the listed type are all skipped in
  silence (four colour-grading keys behave differently, section
  `properties`). Read `properties_set` or `settings_applied`. The two spawn
  operations return no such list at all.
- **Values are not validated or clamped**, apart from the colours noted in
  section `properties`. A negative intensity or a cone angle of 400 is written
  as given, as far as the toolkit's code goes.
- **Fields are written directly.** Most keys are raw field writes followed by
  a render-state refresh on the component; the Details panel's
  property-changed notification is not sent. Post-process settings are written
  into the volume with no refresh call at all. Whether the viewport shows the
  change at once is the engine's behaviour and has not been verified.
- **No lighting build.** The tool has no operation that builds lighting and
  sets no light's mobility. In a level with baked lighting, a new or changed
  light says nothing about the lightmaps; rebuilding them is outside this
  tool.
- **The sky light is not recaptured** unless the same call passes
  `"recapture": true`.

## Addressing an actor

`actor_name` takes the object name (`PointLight_0`) or the label shown in the
World Outliner. The tool walks every actor of the world and takes the **first**
whose label or object name equals the string. The comparison is UE's
`FString ==`, which ignores case. Where labels repeat, use the object name.

Unlike the actor tools of `ue-level-actors`, this tool does not validate the
string's characters, so a label such as `Lamp (2)` is accepted here.

An empty or missing `actor_name` gives `Missing required parameter:
actor_name`; no match gives `Actor not found: <name>`.

## Operations

| Operation | Required | Optional | Writes |
|---|---|---|---|
| `list_lights` | — | `type_filter` | no |
| `inspect_light` | `actor_name` | — | no |
| `spawn_light` | `light_type` | `location`, `rotation`, `label`, `properties` | yes |
| `set_light_properties` | `actor_name`, `properties` | — | yes |
| `set_sky_light` | `actor_name`, `properties` | — | yes |
| `set_sky_atmosphere` | `actor_name`, `properties` | — | yes |
| `set_fog` | `actor_name`, `properties` | — | yes |
| `inspect_post_process` | `actor_name` | — | no |
| `set_post_process` | `actor_name`, `settings` | — | yes |
| `spawn_post_process_volume` | — | `location`, `extent`, `infinite`, `label`, `settings` | yes |

The tool's header comment and its description list the same ten operations.

### list_lights

Returns `count`, `actors[]` and, when given, `type_filter`. Each entry has
`name`, `label`, `class`, `type` and `location`. The message is
`Found <n> lighting actors`.

`type` is one of `PointLight`, `SpotLight`, `DirectionalLight`, `RectLight`,
`SkyLight`, `SkyAtmosphere`, `ExponentialHeightFog`, `PostProcessVolume`.
`type_filter` must equal one of these in full, ignoring case. A filter that
equals none is not an error: it returns `count: 0`.

The type is decided in a fixed order: the four standard light classes and
the sky light are checked first, by the actor's class. An actor that is none
of them and has a sky atmosphere component counts as `SkyAtmosphere`,
whatever its class; a light actor that also carries such a component is
reported as its light type. All other actors are left out, so an empty list
does not mean an empty level.

### inspect_light

Returns `actor_name`, `actor_label`, `class`, `type`, `location`, `rotation`
and the fields of that type. Section `inspect-light` lists the fields per
`type`; load it when you need a field's name, or a value you set is missing
from the result.

- **The read names are not the write names.** `light_color` is read back with
  byte channels (0–255), and it is written as `color`. `source_type` reads
  `CapturedScene` and is written as `captured_scene`. Sending a read name or a
  read value back is ignored in silence. Take write keys from section
  `properties` only.
- **Not everything that can be set can be read back.** The inspect leaves out,
  among others, `indirect_lighting_intensity`, `volumetric_scattering_intensity`
  and `lighting_channels` on lights, every scattering colour of the atmosphere,
  and most fog keys. For those, `properties_set` is the only confirmation this
  tool gives.
- `settings_sample` holds four fields only. Its `bloom_enabled` and
  `auto_exposure_enabled` are the override flags of bloom intensity and
  exposure bias, not on/off switches. Use `inspect_post_process` for a volume.
- An actor of any other kind fails with `Actor '<name>' is not a recognized
  lighting/atmosphere/post-process type (class: <class>)`.

### spawn_light

| Parameter | Default | Meaning |
|---|---|---|
| `light_type` | required | `PointLight`, `SpotLight`, `DirectionalLight` or `RectLight`; also `point`, `spot`, `directional`, `rect`. Case is ignored |
| `location` | `{x:0, y:0, z:0}` | world location. A missing key is 0 |
| `rotation` | `{pitch:0, yaw:0, roll:0}` | world rotation in degrees. A missing key is 0 |
| `label` | engine-chosen | the Outliner label. There is no parameter for the object name |
| `properties` | — | the light keys of section `properties` |

The result holds `actor_name`, `actor_label`, `class`, `type`, and `location`
and `rotation` read back from the new actor. The message is `Spawned <type>`.

- `properties` is applied, but the result carries **no `properties_set`**.
  Confirm with `inspect_light`.
- The light is spawned with the collision mode "always spawn", at the requested
  point.
- Other light types fail with `Unknown light type: '<type>'. Expected: point,
  spot, directional, rect`. A sky light, a sky atmosphere and fog are not
  spawnable here; use `spawn_actor` (skill `ue-level-actors`).
- `Failed to spawn <type> actor` means the engine returned no actor.

### set_light_properties, set_sky_light, set_sky_atmosphere, set_fog

Each takes `actor_name` and a `properties` object, and writes the keys it
knows (section `properties`). The result holds `actor_name`, `properties_set`
(the keys applied, in the tool's own order) and, for `set_light_properties`
only, `actor_label`. The messages are `Set <n> properties on '<name>'`,
`Set <n> properties on SkyLight '<name>'`, `… on SkyAtmosphere '<name>'` and
`… on ExponentialHeightFog '<name>'`.

Each operation accepts only its own kind of actor (`set_sky_atmosphere`: any
actor with a sky atmosphere component). Any other actor is an error that
names its class; section `failures` lists the four messages.

- A missing `properties`, or one that is not a JSON object, fails with
  `Properties object is required`. An empty object succeeds with 0 properties
  and still marks the actor dirty.
- **`set_sky_light` can fail half-way.** A `cubemap` path that does not load as
  a cube texture returns `Failed to load cubemap: <path>`. By then `intensity`
  and `color` from the same call are already written, and the actor is not
  marked dirty. After this error, read the state with `inspect_light` and send
  the call again.
- A `source_type` other than `captured_scene` or `specified_cubemap` is
  skipped without an error, like an unknown key.

### inspect_post_process, set_post_process, spawn_post_process_volume

**Load section `post-process` before you call one of the three** in any way
the recipe "Grade the whole level" does not show: it has their parameters,
results and errors. Its warnings, so that they are not missed:

- **`extent` has no effect**, and the code gives the volume no size.
  **`infinite: true` is the only setting known to make the volume apply**, so
  pass it unless you can check the result.
- `spawn_post_process_volume` returns **no `settings_applied`**. Confirm with
  `inspect_post_process`.
- Every key `set_post_process` applies **turns that setting's override on**,
  and **no override can be switched off** with this tool. It cannot change
  the volume's own fields either (enabled, infinite extent, blend radius,
  blend weight, priority): those are properties of the actor, for
  `set_property` of skill `ue-level-actors`, a path not verified for a
  post-process volume. Sent in `settings` they are ignored without an error,
  like any key the tool does not know.
- `inspect_post_process` shows a setting only while its override is on: an
  absent setting is not overridden by this volume, and its stored value is
  not shown. Its read names differ from the write keys (`bloom.intensity`
  there, `bloom_intensity` in `settings`). `bloom.enabled`, where present,
  is always `true`: it is not a switch. `ao_quality` can be set but is never
  reported.
- A `color_grading_lut` path that does not load as a texture is skipped
  without an error. A `bloom_method` other than `convolution` or `fft`
  selects the standard method and is still listed as applied.

## lighting or the actor tools

Both sets of tools edit the same placed actors, mark the level dirty and save
nothing.

| Job | Use |
|---|---|
| spawn a point, spot, directional or rect light with a label and values in one call | `lighting` `spawn_light` |
| spawn a sky light, sky atmosphere, height fog or any other actor | `spawn_actor` (`ue-level-actors`) |
| set a value that has a key in section `properties` | `lighting`: the key names are fixed, and the result lists what was applied |
| set a property that has no key there, such as a light's mobility | `set_property` (`ue-level-actors`), with the property's dot path |
| move, rotate, scale or delete a lighting actor | `move_actor`, `delete_actors` (`ue-level-actors`) |
| list lighting actors by kind | `lighting` `list_lights` |

## When a call fails

When a call returns an error, or succeeded and the level looks the same, load
section `failures`: a table from what you see to what it means and what to
do, and which kind of actor each set operation accepts.

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
- `ue-tasks`: the task envelope and bridge timeouts.
- `unreagent-iterate`: what an editor stop or restart does to unsaved changes.
