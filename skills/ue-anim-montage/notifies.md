# montage_modify: notifies

A montage's notifies are events on its timeline. A plain notify fires at one
time; a notify state spans a range. Each sits on a notify track, addressed by
`track_index`. Notifies are addressed by name or by `notify_index`, as
`get_info` lists them.

## Reading

`get_info` lists:

- `notifies`: `index`, `name`, `trigger_time`, `duration`, `track_index`, and
  `notify_class` for a notify with a class, or `notify_state_class` with
  `is_state: true` for a notify state with a class. An untyped notify has
  neither.
- `notify_tracks`: `index` and `track_name`.

Re-read the indices after every add or remove. The write operations return
the montage in this shape.

## Adding

| Operation | Required besides `montage_path` | Optional |
|---|---|---|
| `add_notify` | `notify_name`, `trigger_time` | `track_index` (default 0), `notify_class`, `properties` |
| `add_notify_state` | `notify_name`, `start_time`, `duration` | `track_index` (default 0), `notify_class`, `properties` |

- **Times are in seconds and must lie inside the montage**:
  `Trigger time <t> out of range (0-<length>)`, and for a state
  `Notify state range (<start>-<end>) exceeds montage length <length>`.
  The length is the montage's `sequence_length`; add the segments first.
- **Tracks are created as needed.** A `track_index` past the last track adds
  tracks named `NotifyTrack_<n>` up to it. `cleanup_tracks` removes the
  unused ones at the end.
- **`notify_class`** gives the notify its class: a subclass of `AnimNotify`
  for `add_notify`, of `AnimNotifyState` for `add_notify_state`. The tool
  tries the value as a class path, then as `/Script/Engine.<value>`, then
  looks it up among the classes already loaded. A short name such as
  `AnimNotify_PlaySound` therefore works for classes of the `Engine` module.
  For a class from another module, or a Blueprint notify, pass the class path;
  a Blueprint's class path ends in `_C`. Failure:
  `Failed to load notify class: <value>`.
- **Without `notify_class`** the notify is untyped: it has a name and a time,
  and `set_notify_properties` cannot reach it.
- **`properties`** sets properties on the new notify right away, as
  `set_notify_properties` does. When that fails, the notify stays, and the
  call returns `Notify added but property setting failed: <reason>`.

**Check where `properties` landed.** The tool applies them to the last entry
of the notify list. Whether the new notify is still the last entry after the
engine refreshes the montage is not covered here. Run `get_info` and check
that the notify with the new name carries them, or add the notify without
`properties` and then call `set_notify_properties` with the index `get_info`
shows.

## Changing and removing

- **`remove_notify`** removes the first notify named `notify_name`. Others of
  the same name stay. Error: `Notify '<n>' not found`.
- **`move_notify`** moves `notify_index` to `track_index`, both required;
  tracks are created as needed. Errors:
  `Notify index <n> out of range (0-<last>)`, `Track index <n> is invalid`
  (negative).
- **`rename_track`** sets the display name of `track_index` to `track_name`,
  both required. Error: `Track index <n> out of range (0-<last>)`.
- **`cleanup_tracks`** removes the tracks after the highest track a notify
  uses, and keeps at least one. Empty tracks before it stay. The message is
  `Removed <n> empty notify tracks`.

There is no operation to change a notify's time; remove it and add it again.

## set_notify_properties

`notify_index` and `properties` (a JSON object) are required.

```json
{"operation": "set_notify_properties",
 "montage_path": "/Game/Anims/AM_Attack.AM_Attack",
 "notify_index": 0,
 "properties": {"<PropertyName>": 1.5}}
```

- The keys are the C++ property names of the notify's class. An unknown key
  stops the call: `Property '<key>' not found on <class>`.
- An untyped notify has no object to set:
  `Notify at index <n> has no UObject (untyped notify)`.
- **Supported values.** The tool picks the read by the property's type, and
  most reads accept more than the matching JSON type, so a wrong type is
  often a wrong write, not an error:

| Property type | Send | Also accepted, without an error | Fails |
|---|---|---|---|
| bool | `true` / `false` | any number (`true` unless it is 0) and **any string**: `true`, `yes`, `on` and `false`, `no`, `off` count in any case, and every other string is `false` unless it starts with a non-zero integer. The editor language's words for True, Yes, False and No count as well (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`) | `null`, an array or an object: `Property '<key>' expects bool` |
| integer, float, byte (a `TEnumAsByte` included) | a number (an integer property truncates it) | `true` and `false`, as 1 and 0; a string made of an optional sign, digits and at most one dot, such as `"1.5"`. A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0 (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`) | any other string, `null`, an array or an object: `Property '<key>' expects number` |
| name, string | a string | a number or a boolean, stored as its text, `5` or `true` | `null`, an array or an object: `Property '<key>' expects string` |
| `enum class` | the value's name as a string | — | a name the enum does not have, and a number or a boolean, which is looked up as its text: `Enum value '<v>' not found in <enum>`; `null`, an array or an object: `Property '<key>' expects string (enum name)` |
| object | `{"_class": "<class path>", …}` | — | a value that is not an object: `Property '<key>' expects object with _class`; an object without `_class`: `Property '<key>' object missing _class` |

- **A typo in a bool value writes `false`**, and a number sent for a name or
  string property is stored as text. Neither is reported; read the notify
  back.
- **A `TEnumAsByte` property takes its numeric value, not the value's name.**
  The tool tests for a numeric property before it tests for a byte enum, and
  a byte property is a numeric one, so the name fails with
  `Property '<key>' expects number`. Only an `enum class` property takes the
  name.
- Source for the table: `SetPropertiesOnObject` in the plugin's
  `MontageEditor.cpp`; `FJsonValueNumber` and `FJsonValueBoolean` in
  `Engine/Source/Runtime/Json/Private/Dom/JsonValue.cpp`, `TJsonValueString`
  in `Public/Dom/JsonValue.h`, and `FByteProperty` in
  `Engine/Source/Runtime/CoreUObject/Public/UObject/UnrealType.h`; read in the
  UE 5.8.4 source, not run, while the toolkit targets UE 5.6 and 5.7.

- **An object value always creates a new object** of `_class`, owned by the
  notify, and sets the other keys of that JSON object on it. The class must
  derive from the property's class: `Failed to load class: <path>`,
  `Class <c> is not a subclass of <p>`. The tool cannot point a property at an
  existing asset.
- Any other property type, structs and arrays included, returns
  `Unsupported property type '<type>' for '<key>'`.
- **No rollback.** The call stops at the first property that fails, and the
  ones it had already set stay set. Read the notify back with `get_info` and
  set the rest again.
