# audio: add_audio_component, create_sound_cue, create_attenuation, spawn_ambient_sound

The four operations of the `audio` tool that create something, in full. None
of them saves: the main skill, "Saving".

## add_audio_component

Adds an audio component to a Blueprint's component tree, compiles the
Blueprint and marks it dirty.

| Parameter | Default | Meaning |
|---|---|---|
| `blueprint_path` | required | the Blueprint |
| `component_name` | `AudioComponent` | variable name of the new component |
| `sound_asset_path` | — | a sound to assign; any sound asset, MetaSounds included |

- **The component is added at the top of the tree**, with no parent. There is
  no parameter to attach it under another component.
- **The name is not checked.** The tool does not look for a component of the
  same name, and what the engine does with a duplicate has not been verified.
  Read the Blueprint with `get_audio_components` first and pick a new name.
- **The compile result is not checked.** A Blueprint that fails to compile
  still reports success. Skill `ue-blueprints` covers compiling and reading
  errors.
- Errors: `Failed to load Blueprint: <path>`,
  `Blueprint has no SimpleConstructionScript: <path>` (such as a Blueprint
  that is not an actor), `Failed to load sound asset: <path>`,
  `Failed to create SCS node for AudioComponent`.

The result has `component_name`, `blueprint_path`, and `sound_asset` when one
was set. The message is `Added audio component '<name>' to <path>`.

## create_sound_cue

Creates a Sound Cue at `<package_path>/<name>` that plays `sound_wave_path`:
one wave player, set as the cue's first node.

- **The wave player is not added to the editor graph.** The tool sets it as
  the cue's first node and adds it to the cue's node list, but not to the
  graph the Sound Cue editor shows. Whether the cue plays correctly and how
  its editor shows it have not been verified; test it before you build on it.
- **The tool does not check whether an asset already exists** at that path,
  and what the engine does then has not been verified. A `Failed to load …`
  from another tool does not mean the name is free. Check with
  `asset_search` first: `path_filter` set to the folder, `name_pattern` to the
  name. The name is taken only if an entry has exactly that `name` and that
  `package_path`, which `asset_search` writes without a trailing slash.
- Errors: `Failed to load SoundWave: <path>`,
  `Failed to create package: <path>`, `Failed to create USoundCue object`.

The result has `asset_path` (the object path) and `sound_wave`. The message is
`Created SoundCue: <path> from SoundWave: <wave>`. Save it.

## create_attenuation

Creates a Sound Attenuation asset at `<package_path>/<name>`. The settings
come from a `settings` object, from top-level parameters of the same names,
or both; a top-level value wins. `shape` is read only inside `settings`; at the
top level, use `attenuation_shape`.

| Setting | Values |
|---|---|
| `shape` or `attenuation_shape` | `Sphere`, `Capsule`, `Box` or `Cone`, case-insensitive. `shape` in `settings` wins over `attenuation_shape` anywhere |
| `inner_radius` | number: the shape's first extent, the radius of a sphere |
| `falloff_distance` | number: the distance over which the sound fades out |
| `outer_radius` | number: used only without `falloff_distance`. The falloff becomes `outer_radius − inner_radius`, at least 0 |
| `distance_algorithm` | `Linear`, `Logarithmic`, `NaturalSound`, `Custom`, `LogReverse` or `Inverse`, case-insensitive |
| `db_attenuation_at_max` | number |
| `is_spatialized` | boolean: the asset's spatialization switch |
| `spatialization_algorithm` | `Default` (or `Panning`), `HRTF` (or `Binaural`) |

- **Unknown shape, algorithm or spatialization names are skipped without an
  error.** The tool sets nothing for a setting you leave out. Read the asset
  back with `inspect`.
- Only the first extent can be set. The tool leaves a capsule's, box's or
  cone's other dimensions as the new asset has them.
- The existence caveat of `create_sound_cue` applies here too.
- Errors: `Failed to create package: <path>`,
  `Failed to create USoundAttenuation object`.

The result has `asset_path` (the object path). The message is
`Created SoundAttenuation: <path>`. Save it, then assign it:

- **on a placed actor**, with `set_property` on the audio component's
  `AttenuationSettings` (skill `ue-level-actors`), which marks the level
  dirty; `set_audio_properties` would not (see the main skill);
- **on a Blueprint**, with `set_audio_properties` and `attenuation_path`.

## spawn_ambient_sound

Places an Ambient Sound actor in the level open in the editor and gives its
audio component the sound.

| Parameter | Default | Meaning |
|---|---|---|
| `sound_asset_path` | required | any sound asset |
| `location` | `{"x": 0, "y": 0, "z": 0}` | world position; a missing component is 0 |
| `label` | `AmbientSound` | the actor's label |

The actor gets no rotation. Labels can repeat: the tool does not check
whether another actor already has the label, so use a distinct one and keep
the returned `actor_name`.

The result has `actor_name` (the object name), `actor_label`, `location` and
`sound_asset`. The message is
`Spawned AmbientSound '<label>' at (<x>, <y>, <z>)`. The level is marked
dirty: save with `asset save_all`.

Errors:

- `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?`
- `No active world. Open a level in the editor first`
- `Failed to load sound asset: <path>`
- `Failed to spawn AAmbientSound actor`
