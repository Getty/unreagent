# audio: errors and failure modes

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
| `is_spatialized` shows the old value after `set_audio_properties` | the key sets a different flag | see `set_audio_properties` in the main skill |
| a change to a placed actor's audio is gone after a save and reload | the tool did not mark the level dirty | use `set_property` for placed actors |
| `inspect` on a MetaSound says `SoundWave` | the SoundWave test runs first | use the `metasound` tool (`ue-metasounds`) |
| `list_sounds` takes long | it loads every SoundWave in the folder | narrow `folder_path`, drop `recursive` |
| a new Sound Cue plays wrong or looks empty in its editor (neither is verified) | the tool adds the wave player to no editor graph | test the cue before you build on it; this tool cannot add the graph node |
