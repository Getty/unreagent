# audio: inspect, list_sounds, get_audio_components

The three read operations of the `audio` tool in full. Paths and the result
envelope are in the main skill.

## inspect

Takes `asset_path`, any sound asset. Every result has `asset_path` (the object
path), `asset_name` and `type`. The rest depends on the type:

| `type` | Fields |
|---|---|
| `SoundWave` | `duration`, `sample_rate`, `num_channels`, `is_looping`; `sound_class` and `attenuation_settings` when set |
| `SoundCue` | `duration`, `volume_multiplier`, `pitch_multiplier`, `nodes[]` (`class`, and `sound_wave` for a wave player), `node_count`; `sound_class`, `attenuation_settings` and `first_node` when set |
| `SoundAttenuation` | `shape`, `inner_radius`, `outer_radius`, `falloff_distance`, `distance_algorithm`, `db_attenuation_at_max`, `is_spatialized`, `spatialization_algorithm` |
| `SoundClass` | `volume`, `pitch`, `properties` (`volume`, `pitch`, `low_pass_filter_frequency`, `attenuation_distance_scale`) |
| `MetaSoundSource` | `duration`, `hint`; in practice not returned, see below |

- The message sums it up, such as `SoundWave: SW_Rain (12.40s, 2ch, 48000Hz)`.
- Asset paths in the fields are object paths.
- **A SoundWave has no subtitle field**, though the tool's description
  promises one.
- **A MetaSound comes back as `SoundWave`.** The tool tests for a SoundWave
  first (`AudioEditor.cpp`, toolkit c1b6709), and a MetaSound Source is one:
  `UMetaSoundSource` derives from `USoundWaveProcedural`, which derives from
  `USoundWave` (UE 5.8.4 `MetasoundSource.h`, `SoundWaveProcedural.h`). The
  `MetaSoundSource` row in the table above is therefore not reached. Use the
  `metasound` tool's `inspect` (skill `ue-metasounds`).
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
subclasses, such as other kinds of sound wave, are not listed. A MetaSound
Source is listed under its own class `MetaSoundSource`, without `duration`. There is no
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
