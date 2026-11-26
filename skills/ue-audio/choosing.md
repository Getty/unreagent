# audio: choosing the asset and the approach

Decision rules for UE audio, each with where it comes from. Epic's pages are
cited by title; "UE 5.8.4 source" means the engine headers were read, not run.
The toolkit targets UE 5.6 and 5.7 (the `metasound` tool 5.7 and later).

## MetaSound or Sound Cue

Epic's "Audio in Unreal Engine 5" calls Sound Cues "Unreal Engine's legacy
audio objects": parameter graphs that play sources and change volume and
pitch, without sample-accurate control or custom DSP. MetaSounds are a DSP
rendering graph that Epic presents as their replacement. No deprecation date
for Sound Cues was found; they still load and play.

| You need | Use | Through |
|---|---|---|
| one wave, played as it is | the SoundWave itself, or a one-node Sound Cue | `spawn_ambient_sound` / `add_audio_component` take any sound; `create_sound_cue` makes the one-node cue |
| a cue as a container that people will extend by hand later (random, modulator, mixer nodes) | Sound Cue | `create_sound_cue`; the extra nodes are editor work, no tool here adds them |
| sound that gameplay drives through parameters (engine RPM, intensity, a trigger) | MetaSound Source with graph inputs | skill `ue-metasounds`; UE 5.7+ only for graph edits |
| synthesis or DSP (oscillators, filters, envelopes) | MetaSound Source | skill `ue-metasounds` |
| a reusable piece of graph shared by several MetaSounds | MetaSound Patch | not reachable: the `metasound` tool edits Sources only |

On an engine below 5.7 the `metasound` tool is not registered: a MetaSound
can still be listed, placed and assigned here, not built.

## Sound Class, Sound Mix, Audio Modulation

Epic's "Audio in Unreal Engine 5" lists Sound Classes and Sound Mixes under
"Legacy Features", still supported by the audio mixer, and presents Audio
Modulation (a generic parameter bus) as their successor. Sound Classes group
sounds under shared static settings; Sound Mixes change those settings at
runtime.

- With this toolkit, Sound Classes can be **listed and inspected** (`volume`,
  `pitch`, `low_pass_filter_frequency`, `attenuation_distance_scale`), not
  created or changed. Sound Mixes and Modulation assets have no operation.
- Assigning a class to a sound asset: no operation here. `asset
  set_asset_property` (skill `ue-assets`) may reach `SoundClassObject`; that
  has not been verified. The audio component's own override is
  `SoundClassOverride` (UE 5.8.4 `AudioComponent.h`), reachable on a placed
  actor with `set_property` (skill `ue-level-actors`), not verified for this
  property.
- For a new project-wide mix, prefer telling the user it is editor work over
  reaching for raw Python: this is Class B territory (skill `ue-python`).

## Concurrency

From Epic's "Sound Concurrency Reference Guide": **Max Count** is how many
sounds of a concurrency group may be active before the **Resolution Rule**
acts; every active component counts, not only those currently audible;
**Limit to Owner** applies the limit per actor. The Python API reference for
`SoundConcurrencySettings` (UE 5.0) gives the defaults: `max_count` 16,
`resolution_rule` `STOP_FARTHEST_THEN_OLDEST`.

- No operation here creates a Sound Concurrency asset or assigns one. The
  audio component holds them in `ConcurrencySet` (UE 5.8.4
  `AudioComponent.h`), a set of assets; whether `set_property` can write a
  set has not been verified.
- A starting point (a suggestion built on those definitions, not Epic's
  rule): footsteps or impacts that may
  stack get a small Max Count with a rule that drops the farthest or oldest;
  a voice line that must not overlap itself gets Max Count 1.

## Attenuation

What `create_attenuation` can set, and what it cannot:

- **Shape**: `Sphere` is the only shape fully settable here. For `Capsule`,
  `Box` and `Cone` the tool sets the first extent only and leaves the others
  as the new asset has them.
- **Distance algorithm**: `Linear`, `Logarithmic`, `Inverse`, `LogReverse`,
  `NaturalSound` or `Custom`. `Custom` needs a curve no operation sets.
- **Spatialization**: `is_spatialized` and `spatialization_algorithm`
  (`Default` or `HRTF`) on the asset. On an audio component, the tool's
  `is_spatialized` key does something else (main skill,
  `set_audio_properties`).
- **One asset, many users**: one attenuation asset assigned to many
  components keeps them consistent; `attenuation_overrides` on a component
  is a per-component exception, and no tool here can read its values back.

## Gaps: what goes to the user or to Python

No operation here imports audio files, edits a SoundWave's or Sound Cue's
own properties (looping, volume, class, attenuation on the asset), adds
Sound Cue nodes, or creates Sound Classes, Mixes, Concurrency or Modulation
assets. Each of these is either editor work for the user or raw Python
through `execute_script` (skill `ue-python`), which is outside what these
skills have verified. Say which in your report rather than guessing a
Python API.
