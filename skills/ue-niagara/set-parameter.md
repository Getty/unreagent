# UE Niagara: set_parameter

Sets the default value of one user parameter on the system asset. Order of
checks: `asset_path` and `parameter_name` present, the system loads, the
parameter exists, `value` fits its type.

## parameter_name

The name with or without `User.`, compared without regard to case. A name
that is not there fails with
`Parameter '<name>' not found on system '<path>'. Available: <names>`; the
list is the valid names without `User.`.

## value

Must match the parameter's type, as `get_parameters` reports it:

| Type | `value` |
|---|---|
| `float` | a JSON number |
| `int` | a JSON number, rounded to the nearest integer |
| `bool` | `true` or `false` |
| `vector2` | `{"x": …, "y": …}` |
| `vector3` | `{"x": …, "y": …, "z": …}` |
| `vector4` | `{"x": …, "y": …, "z": …, "w": …}` |
| `color` | `{"r": …, "g": …, "b": …, "a": …}`, linear colour |

Through the bridge, `value` is listed as a string, because the tool declares
it `string/number/object`, a type the bridge does not map. No parameter type
takes a string: send the JSON number, boolean or object from the table.

### Pass every component

The tool gives no component a default, `a` included, and reads each with the
engine's `FJsonObject::GetNumberField`, which cannot report a failure:

- a missing component reads as 0, and so does `null`, an array, an object or
  a string that is not numeric;
- `true` and `false` read as 1 and 0, and a string made of an optional sign,
  digits and at most one dot as that number; a lone sign or dot, such as `"-"`
  or `"."`, reads as 0 (`TCString::IsNumeric`,
  `Engine/Source/Runtime/Core/Public/Misc/CString.h`);
- **a colour without `a` gets alpha 0**, which can make the effect invisible.

The only trace of a missing or unreadable component is a `LogJson` line in the
editor log (`FJsonObject::GetField`,
`Engine/Source/Runtime/Json/Private/Dom/JsonObject.cpp`; `FJsonValue::AsNumber`
in `JsonValue.cpp` beside it; UE 5.8.4, read, not run).

### Wrong kinds and loose checks

A value of the wrong kind fails and names the expected form, with two
hyphens: `Parameter '<name>' is float -- 'value' must be a number`. The same
pattern covers `int`, `bool` (`must be a boolean`), `vector2`
(`must be an object {x,y}`), `vector3` (`{x,y,z}`), `vector4` (`{x,y,z,w}`)
and `color` (`{r,g,b,a}`).

The check is looser than those messages (`FJsonObject::TryGetNumberField`
into a `double`, and `TryGetBoolField`; same source and version):

- For `float` and `int`, a numeric string such as `"2.5"` passes as a number,
  and so do `true` and `false`, as 1 and 0.
- **For `bool`, any string and any number passes**: `true`, `yes`, `on` and
  `false`, `no`, `off` count in any case, and every other string is `false`
  unless it starts with a non-zero integer. The editor language's words for
  True, Yes, False and No count as well (`FToBoolHelper`,
  `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`). A typo sets `false`;
  `must be a boolean` is left for `null`, an array or an object.

Any other type fails with
`Unsupported parameter type '<type>' for parameter '<name>'`.

## The result

`parameter_name` (as you passed it), `type` and `asset_path`. The message is
`Set parameter '<name>' on '<system>'`. The package is marked dirty: save it
with `asset save_asset`.

## What it changes, and what it does not

The tool writes the value straight into the system's exposed parameter store,
the defaults. It does not run the engine's change notification for the
system.

- Read it back with `get_parameters`, which reads the same store.
- Whether open Niagara editors, or effects already placed in the level, show
  the new default before the system is saved and reloaded has not been
  verified. A placed component that overrides the parameter keeps its own
  value in any case.
- A component spawned by `spawn_system` after the change uses the new
  default, as far as the store is what the spawn reads; not verified.

To check the visible effect: save, then spawn a preview or capture a placed
actor (section `workflows`, "Verify"). If a placed actor still shows the old
look, reopen the level (skill `ue-levels`) and capture again.

## Ranges

The tool clamps nothing. A value far outside what the system's author
intended (a negative spawn rate, a huge lifetime) is written as given, and
what Niagara does with it depends on the modules that read it. Keep to the
order of magnitude of the old value unless the user says otherwise.
