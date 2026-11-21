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
- **Supported values**, each checked against the property's type:

| Property type | JSON value | Error on a mismatch |
|---|---|---|
| bool | `true` / `false` | `Property '<key>' expects bool` |
| integer or float | a number (integers are truncated) | `Property '<key>' expects number` |
| name, string | a string | `Property '<key>' expects string` |
| enum | the value's name as a string | `Property '<key>' expects string (enum name)`, `Enum value '<v>' not found in <enum>` |
| object | `{"_class": "<class path>", …}` | `Property '<key>' expects object with _class`, `Property '<key>' object missing _class` |

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
