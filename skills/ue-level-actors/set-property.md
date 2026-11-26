# set_property: the property path and the value

The main skill has the parameters, the result, and how an actor is addressed.
This section has the rest.

## What the write does, and does not do

The tool finds the property by reflection and writes its memory directly, then
marks the actor's package and the level dirty. It does **not** call
`Modify` (no undo), `PreEditChange` / `PostEditChangeProperty` (no Details-panel
side effects), or the component's own setter (`SetStaticMesh`,
`SetIntensity`, `SetMobility`, …). Consequences, by engine design, not run
against an editor (unverified):

- A render-relevant value (a component's mesh, material, light colour or
  intensity) may not show in the viewport until the component is re-created,
  for example when the level is saved and opened again (skill `ue-levels`).
- A derived value the setter would recompute (bounds, collision, cached
  state) stays stale.
- A property whose setter does more than store the value, `Mobility` above
  all, can be left inconsistent. Prefer a tool that calls the setter where one
  exists: `move_actor` for transforms, the `lighting` tool (skill `ue-lighting`)
  for light values.

Verify the stored value with `level_query info` or `get_level_actors
brief: false` where they show it, and what is rendered with a viewport capture
(skill `ue-viewport`).

## The property path

Validated first: not empty, at most 512 characters, letters, digits, `_` and
`.` only, no `..`, no leading or trailing dot. Errors:
`Property path cannot be empty`, `… exceeds maximum length of 512 characters`,
`Property path contains invalid character: '<c>'. Only alphanumeric,
underscore, and dot are allowed.`, `Property path cannot contain consecutive
dots`, `Property path cannot start or end with a dot`.

Then the walk, one part at a time, starting on the actor:

1. A part that names a property of the current object is used. If more parts
   follow, it must be an object reference, and the walk moves into the
   referenced object.
2. A part that names no property is tried as a **component**, on an actor
   only: the first component whose name contains the part, ignoring case. `Light` can land on `LightComponent0`, or on any other
   component whose name contains `Light`.
3. A number indexes the array named by the part before it: `Tags.0`. An
   element that is an object can be walked into: `SomeArray.2.Prop`. An
   element at the end of the path is written with the rules below, a struct
   element included, so one existing struct element can be replaced through
   its index even though a whole-array write cannot hold structs.

What fails, or does something you did not mean:

- **A middle part that matches neither a property nor a component is skipped
  without an error.** The walk stays on the same object. Usually the next part
  is not found there either and the error names the outer class, not your
  typo; if the outer object has a property of that name, the write lands on
  it. Check every part against `level_query info`, which lists components.
- The last part must be a property: `Property not found: <part> on <class>`.
- A component as the last part: `Cannot set component as value: <part>`.
- A struct member: `Cannot navigate into non-object property: <part>`. Set the
  whole struct.
- `Nested object is null: <part>`, `Array index 5 out of bounds (size 2) for
  '<part>'`, `Array element 1 is null for '<part>'`, `Cannot navigate into
  non-object array element at index 1`.

## Paths for common actors

Property names below are the engine's reflected names (from engine headers,
not run here); confirm the component names with `level_query info` first.

| Actor | Path | Value |
|---|---|---|
| any light (`PointLight`, `SpotLight`, `RectLight`, `DirectionalLight`) | `LightComponent.Intensity` | number. Units depend on the light type and its intensity-units setting |
| any light | `LightComponent.LightColor` | `FColor`: `"#FFCC88"` |
| `PointLight`, `SpotLight` | `LightComponent.AttenuationRadius` | number, cm |
| `StaticMeshActor` | `StaticMeshComponent.StaticMesh` | object path, such as `/Engine/BasicShapes/Cube.Cube` (render refresh caveat above) |
| `StaticMeshActor` | `StaticMeshComponent.OverrideMaterials` | array of material paths; replaces all slots' overrides |
| any actor | `Tags` | array of strings; replaces all tags |
| any actor | `bHidden` | boolean, Actor Hidden In Game |
| the level's `WorldSettings` actor | `DefaultGameMode` | class path of a game mode: the level's GameMode Override. Not checked against the base class; skill `ue-game-framework`, section `game-mode`, has the whole route |

For lights, prefer the `lighting` tool (skill `ue-lighting`) where it has a key
for the value.

## The value

| Property type | Pass |
|---|---|
| number (`float`, `int32`, …) | a JSON number |
| `bool` | a JSON boolean |
| `FString`, `FName` | a string |
| `FText` | not settable: `Unsupported property type 'FText' for: <path>` |
| `FVector` | `{x, y, z}`, or the text form `"(X=1,Y=2,Z=3)"` |
| `FRotator` | `{pitch, yaw, roll}`, or `"(Pitch=0,Yaw=90,Roll=0)"` |
| `FColor` | `"#RRGGBB"`, `"#RRGGBBAA"` (the `#` optional), or `{r, g, b, a}` on 0–255, `a` defaulting to 255 |
| `FLinearColor` | the same hex strings, or `{r, g, b, a}` on 0–1, `a` defaulting to 1. If `r`, `g` or `b` is above 1.5, all are divided by 255 |
| any other struct | its UE text form, `"(Key=Value,…)"`, or a JSON object (rewritten to that text form, keys uppercased; only number and string values are carried, a boolean or nested object becomes an empty value, so use the text form for those) |
| class reference (`TSubclassOf`) | a full class path: `/Script/Engine.PointLight`, or `/Game/BP/BP_Door.BP_Door_C` (no `_C` is appended). The class is not checked against the property's base class. `"None"` or `""` clears it |
| object reference | an object path that loads, or, for an actor-typed property, an actor's object name or label. `"None"` or `""` clears it |
| soft object or soft class reference | a path string. **It is stored as given, not loaded and not checked**: a misspelt path is accepted and simply resolves to nothing later. Check it with `asset_search` first. `""` or `"None"` clears it |
| `TArray` | a JSON array; **replaces the whole array** |
| `TMap` | a JSON object of key–value pairs; **replaces the whole map**. Keys arrive as strings and are converted to the key type |

Traps:

- **Pass every component of an `FVector`, `FRotator` or `FLinearColor`
  object.** The struct is built fresh, not merged with the current value; only
  `a` gets a default. A left-out component is not reliably 0 (the engine's
  default constructors for these types do not initialise; unverified for the
  build in use). For `FColor` alone, a missing `r`, `g` or `b` is 0.
- **Colour keys are lowercase.** The tool's description writes `{R, G, B, A}`;
  the code reads `r`, `g`, `b`, `a`. For uppercase, use the text form.
- **A failed container write is not rolled back.** The array or map is emptied
  first and filled element by element; an element that fails stops the call
  with the container emptied or half-filled (a map then also skips its
  rehash). Read the old value first (`level_query info` does not show every
  property; `get_level_actors brief: false` shows `tags`) and write it back.
- **Enums.** A top-level `enum class` property (`FEnumProperty`) cannot be set:
  `Unsupported property type …`. A `TEnumAsByte` property, top level or in a
  container, takes its numeric value only; a name string is stored as 0
  without an error. An `enum class` element inside an array or map takes its
  name (`Enum value '<name>' not found for '<context>'` when unknown) or its
  number.
- Containers hold numbers, booleans (also `"true"` as a string), strings,
  names, enums and references. A **struct element** in a whole-array or map
  write, and an array inside an array, are unsupported.
- **A JSON value the tool cannot read as the property's type** also ends in
  `Unsupported property type '<type>' for: <path>`, even for a supported type.
  Send the JSON type from the table before you conclude the type is unsettable.
- **A value of the wrong JSON type is often accepted instead.** The tool reads
  with the engine's `FJsonValue::TryGetNumber`, `TryGetBool` and
  `TryGetString` (read in the UE 5.8.4 source, not run; the toolkit targets UE
  5.6 and 5.7):
  - an integer or byte property accepts any string, by its leading digits; one
    without any stores 0, with no error;
  - a `bool` property accepts any string and any number: `true`, `yes`, `on`
    count as true in any case, so does a string starting with a non-zero
    integer; a typo writes `false`;
  - a `float` or `double` property takes `true`/`false` as 1/0 and a string of
    sign, digits and at most one dot (`"0.5"`; a lone `"-"` or `"."` reads as
    0); any other string ends in the error above;
  - an `FString` or `FName` property stores a number or boolean as its text.

## Errors of the value

| Error | Meaning |
|---|---|
| `Failed to set struct property '<path>' (type: F<Struct>). Supported formats: …` | none of the struct forms parsed |
| `Failed to load class '<value>' for property '<path>'` | the class path did not load |
| `Failed to load object or find actor '<value>' for property '<path>'` | neither an object path nor, for an actor-typed property, an actor name or label |
| `Array property '<path>' requires a JSON array` | a `TArray` got something else |
| `Map property '<path>' requires a JSON object (key-value pairs)` | a `TMap` got something else |
| `Failed to set numeric value for '<path>[i]'`, `… bool value …`, `… string value …`, `… name value …`, `… byte value …`, `… enum value …` | element `i` of an array (or `[key='k']` / `[value for key='k']` of a map) had the wrong JSON type; the container is now partly written |
| `Class property '<ctx>' requires a string path`, `Object property …`, `Soft class property …`, `Soft object property …` | a reference element was not a string |
| `Enum value '<name>' not found for '<context>'` | an `enum class` element name the enum does not define |
| `Unsupported property type '<type>' for '<ctx>'` | an element type the container write does not handle (a struct, a nested array) |
