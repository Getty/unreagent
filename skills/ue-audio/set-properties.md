# set_audio_properties: finding the component, reading the keys

The main skill has the parameters, the keys and the warnings of
`set_audio_properties`. This section has the two passages it leaves out.

## How a key of the wrong JSON type is read

- **Keys the engine's JSON classes cannot read as the listed type, unknown
  keys and unknown `distance_algorithm` names are skipped without an
  error.** The tool reads the keys with the engine's
  `FJsonObject::TryGetNumberField` (into a `double`), `TryGetBoolField` and
  `TryGetStringField`. A number key also reads `true` and `false` as 1 and 0,
  and a string made of an optional sign, digits and at most one dot, such as
  `"0.5"`; any other string is skipped.
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
  **A boolean key reads any string and
  any number, and never skips one**: `true`, `yes`, `on` and `false`, `no`,
  `off` count in any case, and every other string is `false` unless it starts
  with a non-zero integer, so a typo sets `false` and still shows up in
  `properties_set`.
  The editor language's words for True, Yes, False and No count as well
  (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`).
  A string key reads a number or a boolean as its text.
  (`Engine/Source/Runtime/Json/Private/Dom/JsonObject.cpp`, and
  `TJsonValueString` in `Public/Dom/JsonValue.h`; read in the UE 5.8.4
  source, not run, while the toolkit targets UE 5.6 and 5.7.) Send JSON
  numbers and booleans. Check `properties_set` in the
  result: it lists what was applied, such as `"volume=0.50"`.
  `Set 0 properties …` means nothing changed.

## Finding the component

- **A Blueprint** (`target_path` starts with `/Game/`): the first audio
  component in its own tree whose variable name is `component_name`, or the
  first audio component at all. The Blueprint is compiled and marked dirty.
  Errors: `Failed to load Blueprint or no SCS: <path>`,
  `No audio component '<name>' found in Blueprint: <path>` (with an empty name
  when you gave none).
- **A placed actor**: the first actor whose label or object name equals
  `target_path`, then the audio component whose object name is
  `component_name`, or its first audio component. Errors:
  `Could not find actor: <name>`,
  `No audio component '<name>' found on actor: <name>`, `Editor not available`,
  `No editor world available`.
