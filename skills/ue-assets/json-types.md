# How the `asset` tool reads a wrong JSON type

Three passages: the boolean parameters of the `asset` operations, the
booleans of the other asset tools, and the `value` of `set_asset_property`.
The rule in the main skill is to send the JSON type the parameter or property
has; this is what happens otherwise.

## Boolean parameters

`include_properties` belongs to `get_asset_info`.

**A wrong JSON type for `include_properties` is never an error.** The tool
reads it, like `recursive` in `list_assets` and `save` and `mark_dirty` in
`save_asset`, with the engine's `FJsonObject::GetBoolField`, which cannot
report a failure. The default applies only when the key is absent:

- a number is `true` unless it is 0;
- a string is `true` for `true`, `yes` and `on` and `false` for `false`, `no`
  and `off`, in any case; every other string is `false` unless it starts with
  a non-zero integer.
  The editor language's words for True, Yes, False and No count as well
  (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`);
- `null`, an array or an object is `false`, whatever the default is. The
  engine writes a line to the editor log and the call goes on.

(`FJsonValue::AsBool` and `FJsonValueNumber::TryGetBool` in
`Engine/Source/Runtime/Json/Private/Dom/JsonValue.cpp`,
`TJsonValueString::TryGetBool` in `Public/Dom/JsonValue.h`; read in the UE
5.8.4 source, not run, while the toolkit targets UE 5.6 and 5.7.)

## The booleans read with a fallback

`force` (`delete`), `include_dependencies` and `overwrite` (`migrate`),
`include_soft` (`asset_dependencies`, `asset_referencers`) and every boolean
of `asset_import` are read with the toolkit's `ExtractOptionalBool`
(`MCPToolBase.h`), through `FJsonObject::TryGetBoolField`. A number and a
string are read as above. `null`, an array or an object fails the read, and
**the default applies**, not `false`: `"overwrite": null` overwrites,
`"replace_existing": null` replaces. Same sources as above.

## The `value` of `set_asset_property`

A `TArray`, any struct the value table of section `properties` does not list and an
`enum class` property cannot be set with `set_asset_property`.

All of these fail with `Unsupported property type for: <path>`. So does a
value the tool cannot read as the property's type. It reads `value` with the
engine's `FJsonValue::TryGetNumber`, `TryGetBool` and `TryGetString`, and
those accept more than the matching JSON type
(`Engine/Source/Runtime/Json/Private/Dom/JsonValue.cpp`, and
`TJsonValueString` in `Public/Dom/JsonValue.h`; read in the UE 5.8.4 source,
not run, while the toolkit targets UE 5.6 and 5.7):

- **An integer or byte property, a `TEnumAsByte` included, accepts any
  string.** The read is into an `int64`: a string counts by its leading
  digits, and one without any is stored as 0, with no error. An enum value's
  name therefore writes 0. Send the number.
- **A `bool` property accepts any string and any number.** `true`, `yes`,
  `on` and `false`, `no`, `off` count in any case; every other string is
  `false` unless it starts with a non-zero integer. A typo writes `false`.
  The editor language's words for True, Yes, False and No count as well
  (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`).
- A `float` or `double` property also takes `true` and `false` as 1 and 0,
  and a string made of an optional sign, digits and at most one dot, such as
  `"0.5"`. Any other string fails with the error above.
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
- An `FString` or `FName` property stores a number or a boolean as its text.

So send the JSON type the property has (section `properties`).
