# montage_modify: curves

A montage can carry named float curves. `curve_name` addresses them.

| Operation | Required besides `montage_path` | Optional |
|---|---|---|
| `get_curves` | — | — |
| `add_curve` | `curve_name` | `keys` |
| `remove_curve` | `curve_name` | — |
| `set_curve_keys` | `curve_name`, `keys` | — |

All four return `montage` (its path) and `curves`, each with `name` and
`keys`. A key has `time`, `value`, `interp_mode`, `tangent_mode`,
`arrive_tangent` and `leave_tangent`. `get_info` lists only each curve's
`name` and `key_count`.

## Keys

`keys` is an array of objects:

```json
[{"time": 0.0, "value": 0.0},
 {"time": 0.5, "value": 1.0, "interp_mode": "Cubic", "tangent_mode": "Auto"}]
```

- `time` (seconds) and `value` are required in each key. **An entry without
  either, or one that is not an object, is skipped without an error.** Compare
  the count in the message, or the returned `keys`, with what you sent.
- `interp_mode`: `Linear`, `Constant`, `Cubic` or `None`. `tangent_mode`:
  `Auto`, `User`, `Break`, `SmartAuto` or `None`. Both are case-insensitive.
  **An unknown value becomes `Linear` or `Auto`** without an error. An absent
  one leaves the engine's default for a new key; `get_curves` shows what was
  set.
- `arrive_tangent` and `leave_tangent` are numbers. What the engine does with
  them under each tangent mode is not covered here.

## Operations

- **`add_curve`** creates the curve, with `keys` when given:
  `Added curve '<n>'`. Error: `Curve '<n>' already exists`. Send the keys in
  time order: `add_curve` passes them on as given, unlike `set_curve_keys`.
- **`set_curve_keys`** replaces every key of an existing curve with `keys`,
  sorted by time: `Set <count> keys on curve '<n>'`. **An array in which no
  entry is valid still replaces the keys, with an empty list.** Error:
  `Curve '<n>' not found`.
- **`remove_curve`** deletes the curve: `Removed curve '<n>'`. Error:
  `Curve '<n>' not found`.
- To change one key, read the curve with `get_curves`, edit the list, and send
  all of it with `set_curve_keys`.

Whether a curve name has to exist on the skeleton, and how the curve is used
at runtime, is the engine's matter and not covered here.
