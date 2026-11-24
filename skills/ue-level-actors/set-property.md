# set_property: the property path and the value

The main skill has the parameters and the result of `set_property`, and how
an actor is addressed. This section has the rest of the operation.

## The property path

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

## The value

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
