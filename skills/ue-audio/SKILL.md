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

This skill was written from the toolkit's C++ source (commit c1b6709). It has
not been exercised against a running editor.

## Sections — load the one you need

| Section | Holds | Load it |
|---|---|---|
| `workflows` | step lists with the exact calls: a Sound Cue with attenuation placed as an ambient sound, an audio component on a Blueprint, a MetaSound in the level, and how to verify (read-back, log, PIE) | **before any task that creates or places something** |
| `choosing` | MetaSound or Sound Cue, Sound Class / Sound Mix / Audio Modulation, concurrency, attenuation choices, and what no tool here can do, each with its source | when you decide which asset or approach to use, or the user asks for something this tool lacks |
| `creating` | `add_audio_component`, `create_sound_cue`, `create_attenuation` and `spawn_ambient_sound` in full: parameters with defaults, the attenuation settings, results, errors | before you call one of the four |
| `reading` | `inspect`, `list_sounds` and `get_audio_components` in full: result fields per sound type, how a Blueprint or an actor is looked up, errors | before you rely on a result field, or when `get_audio_components` does not find what you named |
| `set-properties` | how `set_audio_properties` finds the component, and how it reads a key of the wrong JSON type | when it does not find the component or actor, or `properties_set` lacks a key or holds a value you did not send |
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

## Must not get wrong

- **`set_audio_properties` requires `target_path`**, not the `asset_path` its
  description names: `Missing required parameter: target_path`.
- **Its `is_spatialized` key does not switch spatialization** (below).
- **On a placed actor, `set_audio_properties` marks nothing dirty**, so
  `save_all` may skip the level. Use `set_property` (skill
  `ue-level-actors`) for placed actors.
- **No operation saves** ("Saving").
- **The create operations do not check whether the asset exists.** Check
  with `asset_search` first (skill `ue-assets`).
- **`create_sound_cue` builds one wave player** and does not add it to the
  editor graph; whether that cue plays and displays correctly is not
  verified. Test it before you build on it.
- **Misspelt enum names are skipped without an error** in
  `create_attenuation` and `attenuation_overrides`. Read the result back.
- **A MetaSound Source is a SoundWave subclass**, so `inspect` reports it as
  `SoundWave`, and `asset_search` for `SoundWave` returns MetaSounds too. Use
  the `metasound` tool's `inspect`, and `list_sounds` with
  `class_filter: "MetaSoundSource"` to find them.
- **`list_sounds` loads every SoundWave it lists**, to read its `duration`;
  a large recursive folder can hold the editor for a while.
- In `get_audio_components` and `set_audio_properties`, **a path starting
  with `/Game/` is a Blueprint**, anything else an actor's label or object
  name. Inherited Blueprint components are not found.

## Parameters the schema does not list

`set_audio_properties` reads `target_path` and the top-level shortcuts
`volume`, `pitch`, `auto_activate`, `is_spatialized`, `sound` and
`attenuation_path`; `create_attenuation` reads `settings`,
`db_attenuation_at_max`, `is_spatialized` and `spatialization_algorithm`.
Pass them anyway. Queued calls do not check parameter names (skill
`ue-tasks`). A direct call appends `WARNING: Unknown parameter ignored: …` to
the message after the tool has run, but the tool has read them all the same.

## What the tool cannot do

No Sound Cue graph editing beyond the one wave player; no Sound Class, Sound
Mix, Concurrency or Modulation authoring (Sound Classes can be inspected); no
audio import (`asset_import`, skill `ue-assets`, is built for FBX; audio is
not verified); no playback in the level (MetaSounds have an editor preview,
skill `ue-metasounds`); no edits to a SoundWave's or Sound Cue's own
properties (`asset set_asset_property` may reach them, not verified). Section
`choosing` says where each of these goes.

## Saving

**No operation saves.** The create operations, `add_audio_component` and
`set_audio_properties` on a Blueprint mark the package dirty, and
`spawn_ambient_sound` marks the level dirty. The change stays in the editor's
memory, where a stop or crash loses it. Save with the `asset` tool (skill
`ue-assets`): `save_asset` with the asset's path, or `save_all`. No operation
has an undo.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …) and the tool's own fields sit
  under its `data` key, which also carries its own `success: true` and
  `message` (skill `ue-tasks`).
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work: read the state back with `inspect`, `list_sounds` or
  `get_audio_components` before you retry.

## Paths

Asset paths are loaded as given: the asset path (`/Game/Audio/SW_Rain`) and
the object path (`/Game/Audio/SW_Rain.SW_Rain`) both work, with **no path
check**, so `/Engine/` content loads too. A sound that does not load gives the
operation's own `Failed to load …`. New assets go to `<package_path>/<name>`,
`package_path` with or without a trailing `/`. Results give object paths.

`asset_search` (skill `ue-assets`) pages and filters by name; its
`class_filter` resolves `SoundWave`, `SoundCue`, `SoundAttenuation` and
`SoundClass`. `MetaSoundSource` lives in another module and probably does not
resolve: pass `/Script/MetasoundEngine.MetaSoundSource`, or use
`list_sounds`.

## set_audio_properties

Changes one audio component, on a Blueprint or on a placed actor.

| Parameter | Meaning |
|---|---|
| `target_path` | required. A `/Game/` Blueprint path, or an actor's label or object name |
| `component_name` | the variable name on a Blueprint, the object name on an actor. Default: the first audio component |
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
| `sound` | sound asset path | the sound, MetaSounds included |
| `attenuation_path` | attenuation asset path | the attenuation asset |
| `attenuation_overrides` | object | switches the component's own attenuation on, then sets `inner_radius`, `falloff_distance` and `distance_algorithm` on it |

- **`is_spatialized` sets the component's "UI sound" flag to the opposite
  value**, while `get_audio_components` reports the spatialization flag
  (`bAllowSpatialization`). The two will disagree. To switch spatialization,
  set `bAllowSpatialization` with `set_property` (skill `ue-level-actors`), or
  put it in an attenuation asset and assign that.
- **Keys of the wrong JSON type, unknown keys and unknown
  `distance_algorithm` names are skipped without an error**, and **a boolean
  key reads any string or number**, so a typo sets `false` and still shows
  in `properties_set`. Send JSON numbers and booleans, and check
  `properties_set` (such as `"volume=0.50"`); `Set 0 properties …` means
  nothing changed. Section `set-properties` has the detail.
- **Any `attenuation_overrides` object, even `{}`, switches the overrides
  on** and adds `attenuation_overrides` to `properties_set`, whether or not a
  value inside was applied. No tool here reads override values back. Its
  `distance_algorithm` takes `Linear`, `Logarithmic`, `Inverse`,
  `NaturalSound`, `LogReverse` or `Custom`, case-insensitive.
- **A failed `sound` or `attenuation_path` stops the call, but not the
  change.** `volume`, `pitch`, `auto_activate` and `is_spatialized` are
  applied first and stay applied in memory, a failed `attenuation_path` also
  leaves `sound` applied, and a Blueprint is then neither compiled nor marked
  dirty.
- On a Blueprint the call compiles it and marks it dirty. On a placed actor
  it marks nothing dirty; prefer `set_property` on `VolumeMultiplier`,
  `PitchMultiplier`, `Sound` or `AttenuationSettings`. Whether a change
  reaches a sound already playing has not been verified.

The result has `properties_set[]` and `target_path`. The message is
`Set <n> properties on audio component in <target>`.

## A typical sequence

Section `workflows` has it in full, with the check after each step:
`list_sounds` → `asset_search` for free names → `create_sound_cue` and
`create_attenuation`, each read back with `inspect` → `save_asset` →
`spawn_ambient_sound` → `set_property` with
`AudioComponent.AttenuationSettings` (skill `ue-level-actors`) →
`get_audio_components` → `save_all` → PIE and `get_output_log` (skill
`ue-pie-testing`).

## Background in the plugin

The plugin ships no context notes for audio: `unreal_get_ue_context`
(skill `ue-context`) has no audio category.

## See also

- `ue-metasounds`: MetaSound Source graphs and preview.
- `ue-assets`: `asset_search`, `asset_import`, `set_asset_property`,
  `save_asset` and `save_all`.
- `ue-level-actors`: `set_property` and `move_actor` on placed actors.
- `ue-blueprints`: compiling a Blueprint, and the event graph that plays a
  component (the gameplay half of a sound).
- `ue-pie-testing`: PIE and `get_output_log`, to verify.
- `ue-tasks`: the task envelope and timeouts.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
