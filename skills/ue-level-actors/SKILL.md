---
name: ue-level-actors
description: >
  Spawn, list, move, delete and edit actors in the level open in the editor,
  through the UE LLM Toolkit's actor tools. Use before placing or changing
  actors, or when spawn_actor, move_actor, delete_actors or set_property
  returns an error.
toolkit_tools: [spawn_actor, get_level_actors, move_actor, delete_actors, set_property]
---

# Level actors

The UE LLM Toolkit plugin runs inside the editor and offers five tools for the
actors of the level that is open there: `get_level_actors` lists them,
`spawn_actor` places one, `move_actor` changes a transform, `delete_actors`
removes actors, and `set_property` writes one property on an actor or on one of
its components.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

## Before the first call

- **Tool names.** The bridge lists each tool as `unreal_<name>`, for example
  `unreal_spawn_actor`, and your client may add its own prefix. This skill uses
  the bare names, as the plugin's errors do.
- **Result shape.** Success is the message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope (`task_id`, `status`, `success`, `message`, …), and the tool's own
  fields, the ones named in this skill, sit under its `data` key. Failure is
  `Error: <message>` with no data. Skill `ue-tasks` covers the task tools.
- **The editor world.** The tools act on the level open in the editor, never
  on a Play-In-Editor session. Without one they fail with `Editor not
  available. …` or `No active world. Open a level in the editor first`.
- **No undo, nothing saved.** No tool here opens an editor transaction, so a
  write has no undo entry. Writes mark the level dirty and leave the disk alone.
  Saving: skills `ue-levels` and `ue-assets` (`asset save_all`). An editor
  restart drops unsaved changes (`unreagent-iterate`).
- **Timeouts.** After 300 s (`MCP_ASYNC_TIMEOUT_MS`) the bridge returns `Error:
  Task timed out after 300000ms (task_id: …)`. That ends the wait, not the
  work, so read the state back with `get_level_actors` before you retry.

## Addressing an actor

Every actor has two names:

- the **object name**, `GetName()`, such as `PointLight_0`, unique in the level;
- the **label**, what the World Outliner shows, which can repeat.

`move_actor`, `delete_actors` and `set_property` accept either. They walk the
level's actors and take the **first** one whose object name or label equals the
string. The comparison is UE's `FString ==`; expect it to ignore case (not
verified here). Where labels repeat, address the actor by its object name.
`get_level_actors` returns both as `name` and `label`, and `spawn_actor`
returns them as `actorName` and `actorLabel`.

Before the lookup, the string is validated:

- it must not be empty;
- it has at most 256 characters;
- it contains no control characters;
- it contains none of `` < > | & ; ` $ ( ) { } [ ] ! * ? ~ ``.

A label such as `Crate (2)` is therefore rejected with `Actor name contains
invalid character: '('`. Use that actor's object name instead. A name that
passes validation but matches no actor gives `Actor not found: <name>. Use
level_query find to search for actors in the current level`.

`level_query` (skill `ue-levels`) addresses actors by label only, and its
`info` operation gives a full dump of one actor's components and collision.

## get_level_actors

Lists the actors of the level, one page at a time. It is read-only.

| Parameter | Default | Meaning |
|---|---|---|
| `class_filter` | — | substring of the class name, case-insensitive, at most 256 characters |
| `name_filter` | — | substring of the object name or the label, case-insensitive, at most 256 characters |
| `include_hidden` | `false` | include actors for which the engine's `IsHidden()` is true, most likely the Actor Hidden In Game flag. Hiding an actor in the editor viewport probably does not count (not verified) |
| `brief` | `true` | `false` adds `location`, `rotation`, `scale`, `hidden` and, when there are any, `tags` |
| `limit` | `25` | page size. Values of 0 or below become 25, and values above 1000 become 1000 |
| `offset` | `0` | matches to skip |

The result holds `actors[]`, with `name`, `label` and `class` in every entry,
plus `count`, `total`, `offset`, `limit`, `hasMore` and `levelName`.
`nextOffset` is present while more matches remain: pass it as the next
`offset`. The message reads `Found 25 actors (showing 1-25 of 112 total)`.

`class_filter` matches a substring of the class's short name. `Light` matches
`PointLight`, `SpotLight`, `DirectionalLight` and `SkyLight` alike. A Blueprint
actor's class is its generated class, `BP_Door_C`.

## spawn_actor

Places one actor of a class in the level.

| Parameter | Default | Meaning |
|---|---|---|
| `class` | required | the class to spawn, resolved as described below |
| `name` | engine-chosen | object name for the new actor. It is not the label |
| `location` | `{x:0, y:0, z:0}` | world location |
| `rotation` | `{pitch:0, yaw:0, roll:0}` | world rotation, in degrees |
| `scale` | `{x:1, y:1, z:1}` | scale |

The keys are lowercase. A missing component takes the default shown, which is 0
for location and rotation and 1 for scale.

### Which class string works

The class string is validated: at most 1024 characters, no `..`, and none of
the characters listed above. Then the tool tries, in order:

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

For anything other than an engine class, pass a full path:

| Class | Form that resolves |
|---|---|
| Blueprint under `/Game/` | `/Game/Blueprints/BP_Door.BP_Door_C`, or the object path `/Game/Blueprints/BP_Door.BP_Door` as `asset_search` returns it (step 2 appends `_C`) |
| Blueprint in plugin content | `/MyPlugin/BP_Door.BP_Door_C`. Step 2 covers `/Game/` only, so spell out `_C` |
| C++ class from a project or plugin module | `/Script/<Module>.<Class>`, for example `/Script/MyGame.MyActor` |

The tool's own description gives `/Game/Blueprints/BP_MyActor` as an example.
Step 2 turns that into `/Game/Blueprints/BP_MyActor_C`, which is not a class
path. Prefer the forms in the table.

### What comes back

`actorName`, `actorClass`, `actorLabel` and `location`, with the message
`Spawned actor 'PointLight_0' of class 'PointLight'`.

- **`location` is the one you asked for, not read back.** The spawn uses the
  collision mode "adjust if possible, but always spawn". The engine can move the
  actor off the requested point when it would collide there. To see where it
  ended up, use `get_level_actors` with `brief: false`.
- **`name` is validated, but not checked for collisions.** It must pass the
  actor-name rules under "Addressing an actor". The tool does not look for an
  actor that already has the name, and it leaves the engine's name mode at its
  default. What the engine then does is outside the toolkit's code. UE's
  default name mode probably treats a taken name as fatal, which would take
  the editor down. That has not been verified against a running editor. Before
  you pass `name`, look it up with `get_level_actors` and `name_filter`.
  Without a `name`, the tool passes none and leaves naming to the engine.
- `Failed to spawn actor of class: <class>` means the class resolved but the
  engine returned no actor.

## move_actor

Changes the location, rotation or scale of one actor.

| Parameter | Default | Meaning |
|---|---|---|
| `actor_name` | required | object name or label |
| `location` | — | `{x, y, z}` |
| `rotation` | — | `{pitch, yaw, roll}` |
| `scale` | — | `{x, y, z}` |
| `relative` | `false` | change relative to the current transform instead of replacing it |

Only what you pass changes. Within a vector, only the components you pass
change: `{"location": {"z": 200}}` sets the height and keeps x and y. With
`relative: true`:

- `location` and `rotation` components are **added** to the current values;
- `scale` is **multiplied**. Components you leave out count as 1, so
  `{"scale": {"x": 2}, "relative": true}` doubles the width only.

Passing none of the three fails with `No transform changes specified. Provide
location, rotation, or scale.`

The result holds the actor's object name in `actor`, and its `location`,
`rotation` and `scale` read back after the change. The message names what
changed, for example `Updated location, rotation for actor 'Cube_3'`.

Prefer `move_actor` over `set_property` on `RelativeLocation` and friends. It
goes through the actor's own transform setters. `set_property` writes the raw
field.

## delete_actors

Removes actors from the level. The tool's description warns: "This is
destructive and cannot be undone via MCP."

| Parameter | Meaning |
|---|---|
| `actor_name` | one object name or label |
| `actor_names` | an array of object names or labels |
| `class_filter` | every actor whose class name contains this substring, case-insensitively |

The tool description says to use one of the three, but the code takes the
**union** of all you pass: a call with both `actor_name` and `class_filter`
deletes both sets.

- **`class_filter` includes hidden actors.** Unlike `get_level_actors`, it does
  not skip them. To preview exactly what it hits, run `get_level_actors` with
  the same `class_filter` and `include_hidden: true`. A short filter matches
  broadly: `Light` takes every light, the sky light included.
- **A name that fails validation aborts the whole call**, and nothing is deleted.
- **Names that are not found do not stop the call.** If at least one actor
  matched, the deletion goes ahead, and the names that did not resolve come back
  in `notFound`.

The result holds `deleted[]` (object names), `count` and, when some names did
not resolve, `notFound[]`. The message is `Deleted 3 actor(s)`.

| Error | Meaning |
|---|---|
| `No actors found: a, b` | none of the given names matched, and the class filter matched nothing either |
| `No actors specified or found to delete. Provide actor_name, actor_names array, or class_filter.` | no parameter given, or only a class filter that matched nothing |

## set_property

Writes one property, addressed by a dot path, on an actor or on one of its
components.

| Parameter | Meaning |
|---|---|
| `actor_name` | required. Object name or label |
| `property` | required. Dot path, for example `LightComponent.Intensity` |
| `value` | required. Any JSON type. Which type depends on the property |

A missing `value` gives `Missing required parameter: value`. On success, the
result holds `actor` and `property`, with the message `Set property
'LightComponent.Intensity' on actor 'PointLight_0'`.

The tool marks the actor and the level dirty, but it does not send the
property-changed notification an edit in the Details panel sends. A value that
depends on that notification, such as a recomputed derived field, may not
update until the next real edit.

### The property path

The path is validated first:

- it is not empty and has at most 512 characters;
- it uses letters, digits, `_` and `.` only;
- it has no `..`, and no leading or trailing dot.

Then the tool walks it one part at a time, starting on the actor:

1. A part that names a property of the current object is used. If more parts
   follow, it must be an object reference, and the walk moves into the
   referenced object.
2. A part that names no property is tried as a **component**, on an actor
   only. The match is the first component whose name contains the part,
   ignoring case. `Light` can therefore land on `LightComponent0`, but also on
   any other component whose name contains `Light`. Use a part that only one
   component's name contains.
3. A number indexes the array named by the part before it: `Tags.0`. An
   element that is an object can be walked into: `SomeArray.2.Prop`.

Several cases fail, or do something you did not mean:

- **A middle part that matches neither a property nor a component is skipped
  without an error.** The walk goes on with the same object. Usually the next
  part is not found there either, and the error then names the outer class, not
  the part you misspelt. If the outer object does have a property of that name,
  the write lands on it. Check every part of a nested path against
  `level_query info` (skill `ue-levels`), which lists the actor's components.
- The last part must be a property: `Property not found: <part> on <class>`.
- A component as the last part fails: `Cannot set component as value: <part>`.
- Struct members cannot be reached through the path:
  `Cannot navigate into non-object property: <part>`. Set the whole struct.
- Other walk errors: `Nested object is null: <part>`,
  `Array index 5 out of bounds (size 2) for '<part>'`,
  `Array element 1 is null for '<part>'`,
  `Cannot navigate into non-object array element at index 1`.

### The value

| Property type | Pass |
|---|---|
| number (`float`, `int32`, …) | a JSON number |
| `bool` | a JSON boolean |
| `FString`, `FName` | a string |
| `FVector` | `{x, y, z}`, or the text form `"(X=1,Y=2,Z=3)"` |
| `FRotator` | `{pitch, yaw, roll}`, or `"(Pitch=0,Yaw=90,Roll=0)"` |
| `FColor` | `"#RRGGBB"`, `"#RRGGBBAA"` (the `#` is optional), or `{r, g, b, a}` on 0–255, with `a` defaulting to 255 |
| `FLinearColor` | the same hex strings, or `{r, g, b, a}` on 0–1, with `a` defaulting to 1. If `r`, `g` or `b` is above 1.5, all of them are read as 0–255 and divided by 255 |
| any other struct | its UE text form, such as `"(Key=Value,…)"`, or a JSON object. The tool rewrites an object to that text form with the keys uppercased |
| class reference (`TSubclassOf`) | a class path, such as `/Script/Engine.PointLight` or, for a Blueprint, `/Game/BP/BP_Door.BP_Door_C`. `"None"` or `""` clears it |
| object reference | an object path. For an actor-typed property, an actor's object name or label also works. `"None"` or `""` clears it |
| soft object or soft class reference | a path that loads. The tool loads the object and stores a reference to it, so a path that does not load is an error. `"None"` or `""` clears it |
| `TArray` | a JSON array. It **replaces the whole array**, so pass every element you want to keep |
| `TMap` | a JSON object of key–value pairs. It **replaces the whole map** |

Some types and values are traps:

- **Pass every component of an `FVector`, `FRotator` or `FLinearColor`
  object.** The tool builds the struct from scratch and does not merge it with
  the current value. Only `a` gets a default. For `FColor` alone, a missing `r`,
  `g` or `b` becomes 0.
- **Colour keys are lowercase.** The tool's description writes `{R, G, B, A}`,
  but the code reads `r`, `g`, `b` and `a`. For uppercase keys, use the text
  form `"(R=255,G=0,B=0,A=255)"`.
- **Enums.**
  - A top-level `enum class` property (`FEnumProperty`) cannot be set at all:
    `Unsupported property type '<type>' for: <path>`.
  - A `TEnumAsByte` property takes its numeric value only.
  - Inside an array or map, an `enum class` element takes its name. A
    `TEnumAsByte` element given a name string is stored as 0, without an
    error. Pass the number.
- Array elements and map entries can be numbers, booleans (also `"true"` as a
  string), strings, names, enums and references. A **struct inside an array or
  map** is not supported, and neither is an array inside an array.
- **A JSON value the tool cannot read as the property's type** also ends in
  `Unsupported property type '<type>' for: <path>`, even when the type itself
  is supported. Send the JSON type from the table before you conclude the
  type cannot be set.
- **A value of the wrong JSON type is often accepted instead.** The tool
  reads `value` with the engine's `FJsonValue::TryGetNumber`, `TryGetBool`
  and `TryGetString` (`Engine/Source/Runtime/Json/Private/Dom/JsonValue.cpp`,
  and `TJsonValueString` in `Public/Dom/JsonValue.h`; read in the UE 5.8.4
  source, not run, while the toolkit targets UE 5.6 and 5.7):
  - **An integer or byte property, a `TEnumAsByte` included, accepts any
    string.** The read is into an `int64`: a string counts by its leading
    digits, and one without any is stored as 0, with no error. An enum
    value's name therefore writes 0, at the top level as inside an array.
  - **A `bool` property accepts any string and any number.** `true`, `yes`,
    `on` and `false`, `no`, `off` count in any case; every other string is
    `false` unless it starts with a non-zero integer. A typo writes `false`.
    The editor language's words for True, Yes, False and No count as well
    (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`).
  - A `float` or `double` property also takes `true` and `false` as 1 and 0,
    and a string made of an optional sign, digits and at most one dot, such
    as `"0.5"`. Any other string ends in the error above.
    A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
    (`TCString::IsNumeric`,
    `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
  - An `FString` or `FName` property stores a number or a boolean as its
    text, `5` or `true`.

Other value errors:

| Error | Meaning |
|---|---|
| `Failed to set struct property '<path>' (type: F<Struct>). Supported formats: …` | none of the struct forms parsed |
| `Failed to load class '<value>' for property '<path>'` | the class path did not load |
| `Failed to load object or find actor '<value>' for property '<path>'` | neither an object path nor, for actor-typed properties, an actor of that name or label |
| `Array property '<path>' requires a JSON array` | the value for a `TArray` was not an array |
| `Map property '<path>' requires a JSON object (key-value pairs)` | the value for a `TMap` was not an object |
| `Enum value '<name>' not found for '<context>'` | an `enum class` element name that the enum does not define |

`set_property` changes the placed actor only. To change a Blueprint's default
for every instance, see `ue-blueprints`. To change a property on an asset, such
as a mesh or a material, use `asset set_asset_property` (skill `ue-assets`).

## Typical sequences

**Place and adjust a light**

1. `spawn_actor` `{"class": "PointLight", "location": {"x": 0, "y": 0, "z": 300}}`.
   Note `actorName` in the result.
2. `set_property` `{"actor_name": "<actorName>", "property": "LightComponent.Intensity", "value": 5000}`.
3. `move_actor` `{"actor_name": "<actorName>", "location": {"z": 400}}`.
4. Save the level (skill `ue-levels`).

**Delete by class, safely**

1. `get_level_actors` `{"class_filter": "StaticMeshActor", "include_hidden": true, "limit": 1000}`
   shows what will go.
2. If the list is not exactly what you mean, delete by `actor_names` instead,
   using the object names from step 1.
3. `delete_actors` with the chosen parameter. Compare `deleted` with step 1.

**Find an actor whose name you do not know**

`get_level_actors` with `name_filter`. For gameplay actors, use `level_query`
`find` (skill `ue-levels`), which also searches class names and leaves out
engine infrastructure.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `No active world. Open a level in the editor first` | no level is open in the editor | open one (skill `ue-levels`) |
| `Actor not found: … Use level_query find …` | no actor with that object name or label | `get_level_actors` with `name_filter` |
| `Actor name contains invalid character: '('` | the label holds a character the validator rejects | address the actor by its object name |
| `Class not found: …` | none of the five class lookups worked | use a full class path, as in the table under spawn_actor |
| `Property not found: <part> on <class>`, and the class is not where the part lives | a misspelt middle part was skipped, and the walk stayed on the outer object | check every part against `level_query info` |
| a write landed one level above the component you named | the same skip, and the outer object has a property of that name | set the old value back, then fix the path |
| `Unsupported property type '…' for: …` | an `enum class`, an unsupported container, or a value the tool could not read as the property's type | check the JSON type. An `enum class` is not settable through this tool |
| a byte enum, or a byte enum element, came out as value 0 | a name string was stored as 0 | pass the numeric value |
| `Error: Task timed out after …ms (task_id: …)` | the bridge stopped waiting, but the editor may still be working on the call | read the state back before you retry |
| the editor goes away during a spawn with `name` | probably a name that was already taken, which UE's default name mode treats as fatal (unverified) | look the name up before you pass it, or omit `name` |

## See also

- `ue-levels`: `open_level`, saving a level, and `level_query` (`list`,
  `find`, `info`).
- `ue-assets`: `asset_search` for class and asset paths, `asset save_all`, and
  properties on assets.
- `ue-blueprints`: Blueprint classes, defaults and variables.
- `ue-lighting`: the `lighting` tool, which spawns the four standard lights
  and sets light, sky light, sky atmosphere, fog and post-process values by
  fixed key names. Its set operations list the keys they applied; its two
  spawns do not.
- `unreagent-iterate`: what an editor stop or restart does to unsaved changes.
- The plugin's background notes on actors in C++, served by
  `unreal_get_ue_context` with category `actor`
  (`Plugins/UELLMToolkit/Resources/mcp-bridge/contexts/actor.md`). The
  property-system notes, which cover CDO defaults versus placed actors, are in
  `Plugins/UELLMToolkit/Resources/domains/tooling.md`.
