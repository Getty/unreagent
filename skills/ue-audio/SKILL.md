---
name: ue-audio
description: >
  Inspect and list sound assets, create Sound Cues and attenuation assets, and
  add or edit audio components and ambient sounds with audio. Use before you
  work on SoundWaves, Sound Cues, attenuation or audio components in UE.
toolkit_tools: [audio]
---

# UE audio: audio

The UE LLM Toolkit plugin's **`audio`** tool reads sound assets, creates Sound
Cues and Sound Attenuation assets, adds audio components to Blueprints, edits
audio components and places Ambient Sound actors. Through the plugin's MCP
bridge it is listed as `unreal_audio`. It works only while the editor is
running and the plugin is reachable; `unreagent-iterate` covers the wait for
the plugin. MetaSound graphs are a separate tool (skill `ue-metasounds`).

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

| Operation | Required | Optional |
|---|---|---|
| `inspect` | `asset_path` | — |
| `list_sounds` | `folder_path` | `class_filter`, `recursive` |
| `get_audio_components` | `asset_path` | — |
| `add_audio_component` | `blueprint_path` | `component_name`, `sound_asset_path` |
| `set_audio_properties` | **`target_path`** | `component_name`, `properties`, and the shortcuts below |
| `create_sound_cue` | `package_path`, `name`, `sound_wave_path` | — |
| `create_attenuation` | `package_path`, `name` | `settings`, and the settings below |
| `spawn_ambient_sound` | `sound_asset_path` | `location`, `label` |

`operation` is lowercased; there are no aliases, and no parameter aliases
either. An unknown operation returns
`Unknown operation: '<op>'. Valid: inspect, list_sounds, get_audio_components, add_audio_component, set_audio_properties, create_sound_cue, create_attenuation, spawn_ambient_sound`.

## Parameters the schema does not list

The tool reads several parameters it does not declare:

- **`set_audio_properties` requires `target_path`**, not the `asset_path` its
  description names. With only `asset_path` it fails with
  `Missing required parameter: target_path`.
- `set_audio_properties` also reads `volume`, `pitch`, `auto_activate`,
  `is_spatialized`, `sound` and `attenuation_path` at the top level.
- `create_attenuation` reads `settings`, `db_attenuation_at_max`,
  `is_spatialized` and `spatialization_algorithm`.

Pass them anyway. Queued calls do not check parameter names (skill
`ue-tasks`). A direct call appends `WARNING: Unknown parameter ignored: …`
(`parameters` for several) to the message after the tool has run, but the
tool has read them all the same.

## What the tool cannot do

- **No Sound Cue graph editing.** `create_sound_cue` builds a cue with one
  wave player. Nodes cannot be added, removed or connected afterwards.
- **No Sound Class or Sound Mix authoring.** Sound Classes can be inspected
  and listed, not created or changed.
- **No import.** The toolkit's `asset_import` (skill `ue-assets`) is built
  for FBX files; whether it imports audio files has not been verified.
- **No preview.** No operation plays a sound in the editor; MetaSounds have
  a preview (skill `ue-metasounds`).
- **No asset property edits.** A SoundWave's or Sound Cue's own volume, class
  or attenuation cannot be set here. `asset set_asset_property` (skill
  `ue-assets`) may reach them; that has not been verified for sound assets.

## Saving

**No operation saves.** The two create operations, `add_audio_component` and
`set_audio_properties` on a Blueprint mark the package dirty, and
`spawn_ambient_sound` marks the level dirty. The change stays in the editor's
memory, where an editor stop or crash loses it. Save with the `asset` tool
(skill `ue-assets`): `save_asset` with the asset's path, or `save_all`.

**For a placed actor, `set_audio_properties` marks nothing dirty itself.**
Whether the engine does on its own is not verified, so `save_all` may skip
that level. See `set_audio_properties`. No operation has an undo.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. With this tool, that
  `data` object also carries its own `success: true` and `message`. Skill
  `ue-tasks` covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the state back with `inspect`, `list_sounds` or
  `get_audio_components` before you retry (skill `ue-tasks`).

## Paths

Asset paths are loaded as given: the asset path (`/Game/Audio/SW_Rain`) and
the object path (`/Game/Audio/SW_Rain.SW_Rain`) both work. The tool runs **no
path check**, so engine content under `/Engine/` loads too. A sound that does
not load gives the operation's own `Failed to load …` message.

New assets go to `<package_path>/<name>`; `package_path` is a content folder
such as `/Game/Audio`, with or without a trailing `/`. `list_sounds` returns
object paths.

To find sounds, use `list_sounds`, or the toolkit's `asset_search` tool
(skill `ue-assets`), which also pages and filters by name. Its `class_filter`
resolves the short names `SoundWave`, `SoundCue`, `SoundAttenuation` and
`SoundClass`. `MetaSoundSource` lives in another module and probably does not
resolve: pass the full class path `/Script/MetasoundEngine.MetaSoundSource`,
or use `list_sounds`. `asset_search` matches subclasses too, so a `SoundWave`
search probably includes MetaSound Sources, which most likely derive from
SoundWave; `list_sounds` matches each class exactly.

## inspect

Takes `asset_path`, any sound asset. Every result has `asset_path` (the object
path), `asset_name` and `type`. The rest depends on the type:

| `type` | Fields |
|---|---|
| `SoundWave` | `duration`, `sample_rate`, `num_channels`, `is_looping`; `sound_class` and `attenuation_settings` when set |
| `SoundCue` | `duration`, `volume_multiplier`, `pitch_multiplier`, `nodes[]` (`class`, and `sound_wave` for a wave player), `node_count`; `sound_class`, `attenuation_settings` and `first_node` when set |
| `SoundAttenuation` | `shape`, `inner_radius`, `outer_radius`, `falloff_distance`, `distance_algorithm`, `db_attenuation_at_max`, `is_spatialized`, `spatialization_algorithm` |
| `SoundClass` | `volume`, `pitch`, `properties` (`volume`, `pitch`, `low_pass_filter_frequency`, `attenuation_distance_scale`) |
| `MetaSoundSource` | `duration`, `hint` (use the `metasound` tool) |

- The message sums it up, such as `SoundWave: SW_Rain (12.40s, 2ch, 48000Hz)`.
- Asset paths in the fields are object paths.
- **A SoundWave has no subtitle field**, though the tool's description
  promises one.
- **A MetaSound probably comes back as `SoundWave`.** The tool tests for a
  SoundWave before it tests for a MetaSound Source, and in UE 5 a MetaSound
  Source is most likely a kind of SoundWave. Use skill `ue-metasounds` for
  MetaSounds.
- **Attenuation radii.** `inner_radius` is the shape's first extent and
  `outer_radius` is that plus `falloff_distance`. For a sphere that is the
  radius; for a capsule, box or cone, the other extents are not reported.
  `distance_algorithm` is `Linear`, `Logarithmic`, `Inverse`, `LogReverse`,
  `NaturalSound` or `Custom`; `spatialization_algorithm` is `Default` or
  `HRTF`. Anything else shows as `Unknown`.
- Errors: `Failed to load asset: <path>`, and for any other kind of asset
  `Asset is not a recognized sound type: <path> (is <class>)`.

## list_sounds

| Parameter | Default | Meaning |
|---|---|---|
| `folder_path` | required | content folder, such as `/Game/Audio` |
| `recursive` | `false` | include subfolders |
| `class_filter` | all five | `SoundWave`, `SoundCue`, `SoundAttenuation`, `SoundClass` or `MetaSoundSource`, case-insensitive |

Lists from the asset registry. Only these five classes match exactly:
subclasses, such as other kinds of sound wave, are not listed. There is no
paging and no name filter. Any other `class_filter` fails with
`Unknown class filter: '<filter>'. Valid: SoundWave, SoundCue, SoundAttenuation, SoundClass, MetaSoundSource`.

The result has `assets[]` (`name`, `path` as the object path, `class`, and
`duration` for a SoundWave) and `count`. The message is
`Found <n> sound assets in <folder>`.

**It loads every SoundWave it lists**, to read its `duration`. A large folder,
with `recursive: true` above all, can hold the editor for a while. Narrow the
folder or set `class_filter`.

## get_audio_components

Takes `asset_path`: a Blueprint or an actor in the level open in the editor.

- **A path starting with `/Game/`** is loaded as a Blueprint first. The
  result lists the audio components the Blueprint adds itself, with
  `source: "blueprint"` and `component_name` set to the component's variable
  name. Components the Blueprint inherits from its parent class are not in
  that list.
- **Anything else is an actor's label or object name**, compared without
  regard to case; the first match is taken. The result lists every audio
  component of the actor, with `source: "world_actor"` and `component_name`
  set to the component's object name. Labels with characters such as `(` work
  here.
- A Blueprint outside `/Game/`, such as plugin content, is looked up as an
  actor name and not found. A `/Game/` path that does not load as a Blueprint
  falls through to the actor lookup as well.

Each component has `component_name`, `volume`, `pitch`, `is_auto_activate`
and `is_spatialized`, plus `sound_asset` and `attenuation_settings_path` when
set. The result has `components[]`, `count` and `source`. The message is
`Found <n> audio components in Blueprint <path>` or
`Found <n> audio components on actor <name>`.

Errors: `Could not find Blueprint or actor: <path>`, `Editor not available`,
`No editor world available`.

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

## set_audio_properties

Changes one audio component, on a Blueprint or on a placed actor.

| Parameter | Meaning |
|---|---|
| `target_path` | required. A `/Game/` Blueprint path, or an actor's label or object name |
| `component_name` | which audio component. Default: the first one |
| `properties` | an object with the keys below |

The top-level keys `volume`, `pitch`, `auto_activate`, `is_spatialized`,
`sound` and `attenuation_path` are copied into `properties` and override the
same key there. `attenuation_overrides` works only inside `properties`.

| Key | Type | Sets |
|---|---|---|
| `volume` | number | the volume multiplier |
| `pitch` | number | the pitch multiplier |
| `auto_activate` | boolean | whether the component starts playing on its own |
| `is_spatialized` | boolean | **not spatialization**, see below |
| `sound` | sound asset path | the sound |
| `attenuation_path` | attenuation asset path | the attenuation asset |
| `attenuation_overrides` | object | switches the component's own attenuation on, then sets `inner_radius`, `falloff_distance` and `distance_algorithm` on it |

- **`is_spatialized` does not switch spatialization.** It sets the
  component's "UI sound" flag to the opposite value, while
  `get_audio_components` reports the component's spatialization flag. The two
  will disagree. To switch spatialization, set `bAllowSpatialization` on a
  placed actor with `set_property` (skill `ue-level-actors`), or put the
  setting in an attenuation asset and assign that (see the end of
  `create_attenuation`).
- **Keys the engine's JSON classes cannot read as the listed type, unknown
  keys and unknown `distance_algorithm` names are skipped without an
  error.** The tool reads the keys with the engine's
  `FJsonObject::TryGetNumberField` (into a `double`), `TryGetBoolField` and
  `TryGetStringField`. A number key also reads `true` and `false` as 1 and 0,
  and a string made of an optional sign, digits and at most one dot, such as
  `"0.5"`; any other string is skipped.
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
  **A boolean key reads any string and
  any number, and never skips one**: `true`, `yes`, `on` and `false`, `no`,
  `off` count in any case, and every other string is `false` unless it starts
  with a non-zero integer, so a typo sets `false` and still shows up in
  `properties_set`.
  The editor language's words for True, Yes, False and No count as well
  (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`).
  A string key reads a number or a boolean as its text.
  (`Engine/Source/Runtime/Json/Private/Dom/JsonObject.cpp`, and
  `TJsonValueString` in `Public/Dom/JsonValue.h`; read in the UE 5.8.4
  source, not run, while the toolkit targets UE 5.6 and 5.7.) Send JSON
  numbers and booleans. Check `properties_set` in the
  result: it lists what was applied, such as `"volume=0.50"`.
  `Set 0 properties …` means nothing changed.
- **Any `attenuation_overrides` object switches the component's own
  attenuation on**, even `{}` or one that holds only unknown keys, and it
  adds `attenuation_overrides` to `properties_set` either way. That entry
  does not prove that a value inside the object was applied, and an empty
  object turns the overrides on with whatever values they already hold: the
  tool sets none. `get_audio_components` does not report the override values,
  so no tool here can read them back.
- `distance_algorithm` takes `Linear`, `Logarithmic`, `Inverse`,
  `NaturalSound`, `LogReverse` or `Custom`, case-insensitive.
- **A failed `sound` or `attenuation_path` stops the call, but not the
  change.** `volume`, `pitch`, `auto_activate` and `is_spatialized` are
  applied first and stay applied in memory, and a failed `attenuation_path`
  also leaves `sound` applied. A Blueprint is then neither compiled nor marked
  dirty.

**Finding the component.**

- **A Blueprint** (`target_path` starts with `/Game/`): the first audio
  component in its own tree whose variable name is `component_name`, or the
  first audio component at all. The Blueprint is compiled and marked dirty.
  Errors: `Failed to load Blueprint or no SCS: <path>`,
  `No audio component '<name>' found in Blueprint: <path>` (with an empty name
  when you gave none).
- **A placed actor**: the first actor whose label or object name equals
  `target_path`, then the audio component whose object name is
  `component_name`, or its first audio component. Errors:
  `Could not find actor: <name>`,
  `No audio component '<name>' found on actor: <name>`, `Editor not available`,
  `No editor world available`.

**The tool marks neither a placed actor nor its level dirty.** The change is
in memory; whether the engine flags the level on its own is not verified, so
`save_all` may not save it. For a placed actor, prefer `set_property` (skill
`ue-level-actors`) on the audio component's own properties, such as
`VolumeMultiplier`, `PitchMultiplier` or `Sound`, which marks the actor and
the level dirty. Whether the engine applies a change made through this tool
to a sound already playing in the editor has not been verified.

The result has `properties_set[]` and `target_path`. The message is
`Set <n> properties on audio component in <target>`.

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
  dirty; `set_audio_properties` would not (see there);
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

## A typical sequence

1. `list_sounds` `{"folder_path": "/Game/Audio", "class_filter": "SoundWave"}`.
2. `create_attenuation`
   `{"package_path": "/Game/Audio", "name": "ATT_Rain", "attenuation_shape": "Sphere", "inner_radius": 200, "falloff_distance": 1800}`,
   then `inspect` it.
3. `spawn_ambient_sound`
   `{"sound_asset_path": "/Game/Audio/SW_Rain", "location": {"x": 0, "y": 0, "z": 100}, "label": "Rain_Ambience"}`.
4. `set_property` on the new actor (skill `ue-level-actors`) to assign the
   attenuation: `{"actor_name": "<actor_name>", "property": "AudioComponent.AttenuationSettings", "value": "/Game/Audio/ATT_Rain"}`.
5. Save: `asset save_all`.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: target_path` | `set_audio_properties` with `asset_path` instead of `target_path` |
| `Missing required parameter: <name>` | another required parameter is absent or an empty string |
| `Failed to load asset: <path>` | `inspect`: nothing at that path |
| `Asset is not a recognized sound type: <path> (is <class>)` | `inspect` on an asset that is not one of the five sound types |
| `Unknown class filter: '<filter>'. Valid: …` | `list_sounds` with another class |
| `Could not find Blueprint or actor: <path>` | `get_audio_components`: neither a loadable `/Game/` Blueprint nor an actor's label or name |
| `Could not find actor: <name>` | `set_audio_properties`: no actor with that label or name |
| `No audio component '<name>' found …` | no audio component of that name, or none at all |
| `Failed to load sound asset: <path>` / `Failed to load SoundWave: <path>` / `Failed to load attenuation asset: <path>` | the asset did not load |
| `Failed to create package: <path>` | the engine could not create the package, probably an invalid path |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| `set_audio_properties` fails with `Missing required parameter: target_path` | the description names `asset_path`, the code reads `target_path` | pass `target_path` |
| `properties_set` is missing a key you sent | a value the tool could not read as the listed type, a wrong key name, or an unknown enum name | fix the key and send it again, numbers and booleans as JSON numbers and booleans |
| `is_spatialized` shows the old value after `set_audio_properties` | the key sets a different flag | see `set_audio_properties` |
| a change to a placed actor's audio is gone after a save and reload | the tool did not mark the level dirty | use `set_property` for placed actors |
| `inspect` on a MetaSound says `SoundWave` | the SoundWave test runs first | use the `metasound` tool (`ue-metasounds`) |
| `list_sounds` takes long | it loads every SoundWave in the folder | narrow `folder_path`, drop `recursive` |
| a new Sound Cue plays wrong or looks empty in its editor (neither is verified) | the tool adds the wave player to no editor graph | test the cue before you build on it; this tool cannot add the graph node |

## Background in the plugin

The plugin ships no context notes for audio: `unreal_get_ue_context`
(skill `ue-context`) has no audio category.

## See also

- `ue-metasounds`: MetaSound Source graphs and preview.
- `ue-assets`: `asset_search`, `asset_import`, `set_asset_property`,
  `save_asset` and `save_all`.
- `ue-level-actors`: `set_property` on placed actors, and addressing actors.
- `ue-blueprints`: compiling a Blueprint and reading its errors.
- `ue-tasks`: the task envelope and timeouts.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
