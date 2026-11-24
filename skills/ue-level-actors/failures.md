# Level actors: failure modes

| What you see | What it means | What to do |
|---|---|---|
| `No active world. Open a level in the editor first` | no level is open in the editor | open one (skill `ue-levels`) |
| `Actor not found: … Use level_query find …` | no actor with that object name or label | `get_level_actors` with `name_filter` |
| `Actor name contains invalid character: '('` | the label holds a character the validator rejects | address the actor by its object name |
| `Class not found: …` | none of the five class lookups worked (listed below) | use a full class path, as in the table under spawn_actor in the main skill |
| `Property not found: <part> on <class>`, and the class is not where the part lives | a misspelt middle part was skipped, and the walk stayed on the outer object | check every part against `level_query info` |
| a write landed one level above the component you named | the same skip, and the outer object has a property of that name | set the old value back, then fix the path |
| `Unsupported property type '…' for: …` | an `enum class`, an unsupported container, or a value the tool could not read as the property's type | check the JSON type. An `enum class` is not settable through this tool |
| a byte enum, or a byte enum element, came out as value 0 | a name string was stored as 0 | pass the numeric value |
| `Error: Task timed out after …ms (task_id: …)` | the bridge stopped waiting, but the editor may still be working on the call | read the state back before you retry |
| the editor goes away during a spawn with `name` | probably a name that was already taken, which UE's default name mode treats as fatal (unverified) | look the name up before you pass it, or omit `name` |

## How `spawn_actor` resolves `class`

The main skill has the forms that resolve and the validation of the class
string. After that validation, the tool tries, in order:

1. the string as a class path, for example `/Script/Engine.PointLight`;
2. for a string that starts with `/Game/` and does not end in `_C`, the string
   with `_C` appended;
3. `/Script/Engine.<string>`, so a bare engine class name such as
   `PointLight`, `SpotLight`, `DirectionalLight`, `StaticMeshActor`,
   `CameraActor` or `PlayerStart` works;
4. `/Script/CoreUObject.<string>`;
5. `FindObject<UClass>` on the string as given. What that finds is up to the
   engine, so do not rely on it.

If all of these fail, the result is `Class not found: <class>. For Blueprint
classes, append '_C' suffix (e.g., 'BP_MyActor_C')`.
