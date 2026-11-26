# Level actors: failure modes

| What you see | What it means | What to do |
|---|---|---|
| `No active world. Open a level in the editor first` | no level is open in the editor | open one (skill `ue-levels`) |
| `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?` | the plugin answered without an editor engine | check the editor (skill `unreagent-iterate`) |
| `Missing required parameter: <name>` | a required parameter is absent or an empty string | pass it |
| `Actor not found: … Use level_query find …` | no actor with that object name or label, or it sits in an unloaded World Partition region | `get_level_actors` with `name_filter`; for World Partition, skill `ue-levels`, section `level-types` |
| `Actor name contains invalid character: '('` | the label holds a character the validator rejects | address the actor by its object name |
| `Actor name exceeds maximum length of 256 characters`, `Actor name contains control characters`, `Actor name cannot be empty` | the name check | fix the string |
| `Class not found: …` | none of the five class lookups worked (below) | a full class path, as in the main skill's table; for an engine class outside `Engine`, `/Script/<Module>.<Class>` |
| `Class path contains invalid character: …`, `Class path cannot contain path traversal sequences` | the class-string check | pass a plain class or object path |
| `Failed to spawn actor of class: …` | the class resolved but the engine spawned nothing: an abstract class, or a class that is not placeable | spawn a concrete subclass; check `get_output_log` (skill `ue-pie-testing`) for the engine's reason |
| the spawned actor is not at `location` | the spawn mode moved it off a colliding point; the result echoes the request | read it back with `get_level_actors` `brief: false`, then `move_actor` |
| a spawned `StaticMeshActor` is invisible | it has no mesh yet | `set_property` `StaticMeshComponent.StaticMesh` (section `set-property`) |
| `No transform changes specified. …` from `move_actor` | no `location`, `rotation` or `scale` object, or keys spelt in uppercase | lowercase `x`, `y`, `z`, `pitch`, `yaw`, `roll` |
| `Property not found: <part> on <class>`, and the class is not where the part lives | a misspelt middle part was skipped, the walk stayed on the outer object | check every part against `level_query info` |
| a write landed one level above the component you named | the same skip, and the outer object has a property of that name | write the old value back, then fix the path |
| `set_property` succeeded, the viewport shows the old mesh, material or light | the raw write skipped the render-state refresh (unverified) | save and reopen the level (skill `ue-levels`), then capture again (skill `ue-viewport`) |
| `Unsupported property type '…' for: …` | `FText`, an `enum class`, an unsupported container, or a JSON value the tool could not read as the type | check the JSON type; `FText` and `enum class` are not settable here |
| a byte enum came out as 0 | a name string was stored as 0 | pass the numeric value |
| an array or map property is empty or short after an error | a container write fails mid-way without rollback | write the full old value back |
| a soft reference points at nothing | the path was stored unchecked | check the path with `asset_search`, then write it again |
| `deleted` lists an actor that is still there | `deleted` is what was collected, not what was destroyed | read the level back; delete it again by object name |
| `delete_actors` removed engine actors (sky light, physics volume) | `class_filter` matched more than the gameplay actors | re-create them (the `lighting` tool for lights, skill `ue-lighting`); preview filters with `get_level_actors include_hidden: true` |
| `get_level_actors` or a lookup misses actors you can see in the editor | a World Partition level shows only loaded actors; or they are hidden in game and `include_hidden` is off | load the region in the editor, or `include_hidden: true` |
| `Error: Task timed out after …ms (task_id: …)` | the bridge stopped waiting; the editor may still be working | read the state back before you retry (skill `ue-tasks`) |
| the editor goes away during a spawn with `name` | a taken name: the default `Required_Fatal` name mode ends the editor with a fatal error | restart it (skill `unreagent-iterate`); look names up first, or omit `name` |

## How `spawn_actor` resolves `class`

After the class-string check, the tool tries, in order:

1. the string as a class path, for example `/Script/Engine.PointLight`;
2. for a string that starts with `/Game/` and does not end in `_C`, the string
   with `_C` appended;
3. `/Script/Engine.<string>`, so a bare engine class name in the `Engine`
   module works (`PointLight`, `StaticMeshActor`, `PlayerStart`, …). A class
   of another module, such as `NavMeshBoundsVolume` (`NavigationSystem`),
   does not resolve by its bare name here;
4. `/Script/CoreUObject.<string>`;
5. `FindObject<UClass>` on the string as given. What that finds is up to the
   engine, so do not rely on it.

If all fail: `Class not found: <class>. For Blueprint classes, append '_C'
suffix (e.g., 'BP_MyActor_C')`. The hint's bare `BP_MyActor_C` reaches only
step 5, if anything; use the full path.
