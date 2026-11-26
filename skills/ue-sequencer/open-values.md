# sequencer: how `open` reads the camera keys and `frame`

The two items below belong to the list "What the success does not prove" of
`open` in section `operations`. `scrub` reads its `frame` like `open` does.

- **A key left out of a camera object is not the current value.** The code
  reads all six keys with the engine's `FJsonObject::GetNumberField`, without
  a default of its own. That call cannot report a failure: a missing key
  reads as 0, and so does `null`, an array, an object or a string that is not
  numeric. `true` and `false` read as 1 and 0, and a string made of an
  optional sign, digits and at most one dot as that number
  (`FJsonObject::GetField` in
  `Engine/Source/Runtime/Json/Private/Dom/JsonObject.cpp`,
  `FJsonValue::AsNumber` in `JsonValue.cpp` beside it; read in the UE 5.8.4
  source, not run, while the toolkit targets UE 5.6 and 5.7).
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
  Pass all six.
- **A failed scrub is swallowed**, and so is a `frame` the tool cannot read
  as a number. `open` still succeeds; the result just has no `frame` and the
  message no ` at frame`. The read is the engine's
  `FJsonObject::TryGetNumberField` into a `double`, and it accepts more than
  a JSON number: `true` and `false` scrub to frame 1 and 0, and a string made
  of an optional sign, digits and at most one dot scrubs to that number
  (`"12"` to frame 12). `null`, an array, an object, an empty string and any
  other string are not read, so no scrub is tried
  (`FJsonValueBoolean::TryGetNumber` in
  `Engine/Source/Runtime/Json/Private/Dom/JsonValue.cpp`,
  `TJsonValueString::TryGetNumber` in `Public/Dom/JsonValue.h`; read in the
  UE 5.8.4 source, not run).
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
  When `frame` is present it is the number that was read, fraction
  included, not a value read back from the Sequencer: `12.9` scrubs to frame
  12 and the message says ` at frame 12`, but `frame` is 12.9. `scrub`
  returns the truncated number instead.
