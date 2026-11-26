# lighting: each operation in detail

The skill itself has the operations table, the result shape and how an actor
is addressed. The keys of `properties` and `settings` are in section
`properties`; the post-process operations in full are in section
`post-process`. Read in `LightingEditor.cpp` and `MCPTool_Lighting.cpp` at
toolkit commit c1b6709.

## list_lights

Returns `count`, `actors[]` and, when given, `type_filter`. Each entry has
`name`, `label`, `class`, `type` and `location`. The message is
`Found <n> lighting actors`.

`type` is one of `PointLight`, `SpotLight`, `DirectionalLight`, `RectLight`,
`SkyLight`, `SkyAtmosphere`, `ExponentialHeightFog`, `PostProcessVolume`.
`type_filter` must equal one of these in full, ignoring case. A filter that
equals none is not an error: it returns `count: 0`.

The type is decided in a fixed order: the four standard light classes and the
sky light first, by the actor's class. An actor that is none of them and has
a sky atmosphere component counts as `SkyAtmosphere`, whatever its class; a
light actor that also carries such a component is reported as its light type.
Then fog, then post-process volumes. All other actors are left out — a
volumetric cloud, a reflection capture, a Blueprint sun — so an empty list
does not mean an unlit level. `get_level_actors` with a `class_filter` (skill
`ue-level-actors`) sees everything.

## inspect_light

Returns `actor_name`, `actor_label`, `class`, `type`, `location`, `rotation`
and the fields of that type (section `inspect-light`).

- **The read names are not the write names.** `light_color` is read back with
  byte channels (0–255), and it is written as `color`. `source_type` reads
  `CapturedScene` and is written as `captured_scene`. Sending a read name or a
  read value back is ignored in silence. Take write keys from section
  `properties` only.
- **Not everything that can be set can be read back.** The inspect leaves out,
  among others, `indirect_lighting_intensity`, `volumetric_scattering_intensity`
  and `lighting_channels` on lights, the light's intensity units and mobility,
  every scattering colour of the atmosphere, and most fog keys. For those,
  `properties_set` is the only confirmation this tool gives;
  `level_query info` (skill `ue-levels`) dumps an actor's components.
- `settings_sample` (post-process volume) holds four fields only. Its
  `bloom_enabled` and `auto_exposure_enabled` are the override flags of bloom
  intensity and exposure bias, not on/off switches. Use `inspect_post_process`
  for a volume.
- An actor of any other kind fails with `Actor '<name>' is not a recognized
  lighting/atmosphere/post-process type (class: <class>)`. The message on
  success is `Light inspected`.

## spawn_light

| Parameter | Default | Meaning |
|---|---|---|
| `light_type` | required | `PointLight`, `SpotLight`, `DirectionalLight` or `RectLight`; also `point`, `spot`, `directional`, `rect`. Case is ignored |
| `location` | `{x:0, y:0, z:0}` | world location. A missing key is 0 |
| `rotation` | `{pitch:0, yaw:0, roll:0}` | world rotation in degrees. A missing key is 0. A light shines along the actor's forward axis, so a negative pitch aims it down |
| `label` | engine-chosen | the Outliner label. There is no parameter for the object name |
| `properties` | — | the light keys of section `properties` |

The result holds `actor_name`, `actor_label`, `class`, `type`, and `location`
and `rotation` read back from the new actor. The message is `Spawned <type>`.

- `properties` is applied, but the result carries **no `properties_set`**.
  Confirm with `inspect_light`.
- The light is spawned with the collision mode "always spawn", at the
  requested point, through the world's `SpawnActor`, not the editor's
  placement path: a point, spot or rect light keeps **unitless** intensity
  (skill body, "Must not get wrong"), and every light keeps the engine's
  default mobility, **Stationary** (UE 5.8.4 source, `Light.cpp`). Section
  `lumen-scene` says when that matters.
- Other light types fail with `Unknown light type: '<type>'. Expected: point,
  spot, directional, rect`. A sky light, a sky atmosphere and fog are not
  spawnable here; use `spawn_actor` (skill `ue-level-actors`).
- `Failed to spawn <type> actor` means the engine returned no actor.

## set_light_properties, set_sky_light, set_sky_atmosphere, set_fog

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
- A directional light has no keys of its own here. Its direction is the
  actor's rotation: `move_actor` (skill `ue-level-actors`).

## inspect_post_process, set_post_process, spawn_post_process_volume

Section `post-process` has their parameters, results and errors. Its
warnings, so that they are not missed:

- **`extent` has no effect**, and the code gives the volume no size.
  **`infinite: true` is the only setting known to make the volume apply.**
- `spawn_post_process_volume` returns **no `settings_applied`**. Confirm with
  `inspect_post_process`.
- Every key `set_post_process` applies **turns that setting's override on**,
  and **no override can be switched off** with this tool. It cannot change
  the volume's own fields either (enabled, infinite extent, blend radius,
  blend weight, priority), nor any setting it has no key for, such as the
  exposure method (section `exposure`). Sent in `settings` they are ignored
  without an error.
- `inspect_post_process` shows a setting only while its override is on, under
  read names that differ from the write keys (`bloom.intensity` there,
  `bloom_intensity` in `settings`). `bloom.enabled`, where present, is always
  `true`: it is not a switch. `ao_quality` can be set but is never reported.
- A `color_grading_lut` path that does not load as a texture is skipped
  without an error. A `bloom_method` other than `convolution` or `fft`
  selects the standard method and is still listed as applied.
