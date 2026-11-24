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

This skill has four sections:

| Section | Holds | Load it |
|---|---|---|
| `reading` | `inspect`, `list_sounds` and `get_audio_components` in full: the result fields per sound type, how a Blueprint or an actor is looked up, the errors | before you rely on a field of their results, or when `get_audio_components` does not find what you named |
| `creating` | `add_audio_component`, `create_sound_cue`, `create_attenuation` and `spawn_ambient_sound` in full: parameters with defaults, the attenuation settings, results, errors | before you call one of the four |
| `set-properties` | how `set_audio_properties` finds the component, and how it reads a key of the wrong JSON type | when it does not find the component or the actor, or when `properties_set` lacks a key or holds a value you did not send |
| `failures` | the error table and the failure table (what you see, what it means, what to do) | when a call returns an error or a result you did not expect |

| Operation | Required | Optional |
|---|---|---|
| `inspect` | `asset_path` | — |
| `list_sounds` | `folder_path` | `class_filter`, `recursive` |
| `get_audio_components` | `asset_path` | — |
| `add_audio_component` | `blueprint_path` | `component_name`, `sound_asset_path` |
| `set_audio_properties` | **`target_path`** | `component_name`, `properties`, and the shortcuts below |
| `create_sound_cue` | `package_path`, `name`, `sound_wave_path` | — |
| `create_attenuation` | `package_path`, `name` | `settings`, and the settings in section `creating` |
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

## inspect, list_sounds, get_audio_components

The table at the top has their parameters. Load section `reading` before you
rely on a field of their results, or when `get_audio_components` does not
find what you named. What you must know without it:

- **`list_sounds` loads every SoundWave it lists**, to read its `duration`. A
  large folder, with `recursive: true` above all, can hold the editor for a
  while. Narrow the folder or set `class_filter`: `SoundWave`, `SoundCue`,
  `SoundAttenuation`, `SoundClass` or `MetaSoundSource`.
- **A MetaSound probably comes back as `SoundWave`** from `inspect`. Use skill
  `ue-metasounds` for MetaSounds.
- In `get_audio_components`, **a path starting with `/Game/`** is loaded as a
  Blueprint first, and the result lists the audio components the Blueprint
  adds itself, not those it inherits. **Anything else is an actor's label or
  object name.**

## add_audio_component, create_sound_cue, create_attenuation, spawn_ambient_sound

**Load section `creating` before you call one of these four.** It has their
parameters with defaults, the settings of `create_attenuation`, the results
and the errors. Its warnings, so that they are not missed:

- `add_audio_component`: **the name is not checked.** Read the Blueprint with
  `get_audio_components` first and pick a new name. **The compile result is
  not checked.** A Blueprint that fails to compile still reports success.
- `create_sound_cue` and `create_attenuation`: **the tool does not check
  whether an asset already exists** at that path, and what the engine does
  then has not been verified. Check with `asset_search` first.
- `create_sound_cue`: **the wave player is not added to the editor graph.**
  Whether the cue plays correctly and how its editor shows it have not been
  verified; test it before you build on it.
- `create_attenuation`: **unknown shape, algorithm or spatialization names are
  skipped without an error.** Read the asset back with `inspect`.
- `spawn_ambient_sound`: labels can repeat. Use a distinct one and keep the
  returned `actor_name`.

None of them saves ("Saving").

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
  setting in an attenuation asset and assign that (section `creating`, the end
  of `create_attenuation`).
- **Keys the engine's JSON classes cannot read as the listed type, unknown
  keys and unknown `distance_algorithm` names are skipped without an
  error.** **A boolean key reads any string and any number, and never skips
  one**, so a typo sets `false` and still shows up in `properties_set`. Send
  JSON numbers and booleans. Check `properties_set` in the result: it lists
  what was applied, such as `"volume=0.50"`. `Set 0 properties …` means
  nothing changed. Section `set-properties` has how each JSON type is read
  (read in the UE 5.8.4 source, not run, while the toolkit targets UE 5.6 and
  5.7): load it when `properties_set` lacks a key or holds a value you did
  not send.
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

**Finding the component.** A `target_path` that starts with `/Game/` is a
Blueprint, anything else a placed actor; `component_name` is the variable
name on a Blueprint and the object name on an actor. Section `set-properties`
has the lookup in full and its errors: load it when the call does not find
the component or the actor.

**The tool marks neither a placed actor nor its level dirty.** The change is
in memory; whether the engine flags the level on its own is not verified, so
`save_all` may not save it. For a placed actor, prefer `set_property` (skill
`ue-level-actors`) on the audio component's own properties, such as
`VolumeMultiplier`, `PitchMultiplier` or `Sound`, which marks the actor and
the level dirty. Whether the engine applies a change made through this tool
to a sound already playing in the editor has not been verified.

The result has `properties_set[]` and `target_path`. The message is
`Set <n> properties on audio component in <target>`.

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

## When a call fails

When a call returns an error or a result you did not expect, load section
`failures`: the errors with their causes, and a table from what you see to
what it means and what to do.

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
