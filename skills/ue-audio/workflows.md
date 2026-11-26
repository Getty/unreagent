# audio: workflows

End-to-end step lists with the exact calls. Every step names the tool; the
parameters are in the main skill and in sections `creating` and `reading`.
Nothing here has been run against an editor: the steps follow from the
toolkit's source, and each "check" says what the source puts in the result.

## A Sound Cue with attenuation, placed as an ambient sound

1. **Find the wave.** `audio`
   `{"operation": "list_sounds", "folder_path": "/Game/Audio", "class_filter": "SoundWave"}`,
   then `inspect` the one you pick. Note `duration`, `num_channels` and
   `is_looping`. For an ambience that should loop, a wave with
   `is_looping: true` is the simple route (UE knowledge, not verified here);
   no operation here can change the flag or add a Looping node to a cue
   (section `choosing`, "Gaps").
2. **Check the names are free.** `asset_search` with `path_filter`
   `/Game/Audio` and `name_pattern` `SC_Rain`, then again for `ATT_Rain`
   (skill `ue-assets`). Neither create operation checks.
3. **Create the cue.** `audio`
   `{"operation": "create_sound_cue", "package_path": "/Game/Audio", "name": "SC_Rain", "sound_wave_path": "/Game/Audio/SW_Rain"}`.
   Check with `inspect` on the returned `asset_path`: `type` `SoundCue`,
   `node_count` 1, `first_node` `SoundNodeWavePlayer`, and `nodes[0]` with
   that class and `sound_wave` set to the wave's object path.
4. **Create the attenuation.** `audio`
   `{"operation": "create_attenuation", "package_path": "/Game/Audio", "name": "ATT_Rain", "attenuation_shape": "Sphere", "inner_radius": 200, "falloff_distance": 1800, "distance_algorithm": "NaturalSound"}`.
   Check with `inspect`: `shape`, `inner_radius`, `falloff_distance` and
   `distance_algorithm` must be what you sent. A misspelt name is skipped
   without an error and leaves the engine's default in place.
5. **Save both** before anything references them: `asset save_asset` with
   each `asset_path` (skill `ue-assets`).
6. **Place it.** `audio`
   `{"operation": "spawn_ambient_sound", "sound_asset_path": "/Game/Audio/SC_Rain", "location": {"x": 0, "y": 0, "z": 100}, "label": "Rain_Ambience"}`.
   Keep the returned `actor_name`; labels can repeat.
7. **Assign the attenuation** on the placed actor with `set_property`
   (skill `ue-level-actors`), which marks the level dirty:
   `{"actor_name": "<actor_name>", "property": "AudioComponent.AttenuationSettings", "value": "/Game/Audio/ATT_Rain"}`.
   Not `set_audio_properties`: on a placed actor it marks nothing dirty.
8. **Read it back.** `audio`
   `{"operation": "get_audio_components", "asset_path": "<actor_name>"}`:
   `source` `world_actor`, one component with `sound_asset` the cue's object
   path and `attenuation_settings_path` the attenuation's.
9. **Save the level:** `asset save_all`.
10. **Verify in play** (section "Verifying", below).

## An audio component on a Blueprint

For a sound that gameplay starts and stops, such as a door or an engine.

1. `audio` `{"operation": "get_audio_components", "asset_path": "/Game/Blueprints/BP_Door"}`:
   pick a `component_name` that is not in the list. The name is not checked.
2. `audio`
   `{"operation": "add_audio_component", "blueprint_path": "/Game/Blueprints/BP_Door", "component_name": "DoorAudio", "sound_asset_path": "/Game/Audio/SC_DoorOpen"}`.
   The component goes to the top of the tree, with no parent, and the
   compile result is not checked: read compile errors with skill
   `ue-blueprints`.
3. `audio`
   `{"operation": "set_audio_properties", "target_path": "/Game/Blueprints/BP_Door", "component_name": "DoorAudio", "auto_activate": false, "attenuation_path": "/Game/Audio/ATT_Door"}`.
   Check `properties_set`: it must hold `auto_activate=false` and
   `attenuation_path=…`. With `auto_activate` false the sound plays only when
   gameplay calls Play.
4. `get_audio_components` on the Blueprint again: `is_auto_activate` false,
   `attenuation_settings_path` set.
5. Save: `asset save_asset` with the Blueprint's path.
6. **The gameplay half is not this tool's.** Calling Play, Stop or a
   parameter setter on the component from the Blueprint's event graph is
   Blueprint graph work: skill `ue-blueprints`, and in a project with the
   shipped agents, agent `unreagent-gameplay`. Hand it the Blueprint path and
   the component name. The Blueprint functions on an audio component, from
   the UE 5.8.4 headers (`AudioParameterControllerInterface.h`): **Set Float
   Parameter**, **Set Integer Parameter**, **Set Boolean Parameter** and
   **Execute Trigger Parameter**, each taking the parameter's name. For a
   MetaSound that name is a graph input's name (skill `ue-metasounds`,
   section `workflows`). Execute Trigger Parameter does not cache the
   trigger: it fires only on a sound already playing.

## A MetaSound in the level

A MetaSound Source is a sound asset like any other here: `spawn_ambient_sound`
and `add_audio_component` take its path as `sound_asset_path`, and
`set_audio_properties` takes it as `sound`. Build the graph with skill
`ue-metasounds` first, and save it before you reference it.

## Verifying

The tools here read assets and components; none plays a sound in the level
and none reports what is audible. What can be checked:

- **The data**: `inspect` each created asset, `get_audio_components` on each
  Blueprint or actor you changed (the checks in the steps above).
- **The log**: `get_output_log` with `min_verbosity` `Warning` and
  `since: true` after each step, and before PIE (skill `ue-pie-testing`,
  section `log`). A sound asset that fails to load or a Blueprint that fails
  to compile is most likely to show there; which messages the engine writes
  for audio has not been verified.
- **In play**: `gameplay_debug` `start_pie`, poll `pie_status`, read
  `get_output_log`, `stop_pie` (skill `ue-pie-testing`). Whether the sound is
  heard is for the user to confirm: say in your report that you could not
  listen.
- **A MetaSound** can be heard in the editor without PIE: `metasound`
  `preview`, then `stop_preview` (skill `ue-metasounds`).
