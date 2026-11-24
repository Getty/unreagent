# UE blend spaces: batch

`operations` is an array of objects, each with `op` and the parameters of one
of `add_sample`, `remove_sample`, `move_sample`, `set_sample_animation` or
`set_axis`. `asset_path` is given once, outside the array.

```json
{"operation": "batch", "asset_path": "/Game/Anims/BS_Walk.BS_Walk",
 "operations": [
   {"op": "set_axis", "axis_index": 0, "axis_name": "Speed", "min": 0, "max": 600},
   {"op": "add_sample", "animation_path": "/Game/Anims/Idle.Idle", "x": 0},
   {"op": "add_sample", "animation_path": "/Game/Anims/Walk.Walk", "x": 300}
 ]}
```

- The ops run in order on the same asset, and the geometry is rebuilt once at
  the end. A `remove_sample` moves the last sample into the freed index for
  every op after it.
- A failed op does not stop the ones after it, and nothing is rolled back.
- Per-op errors: `[<i>] not a JSON object`, `[<i>] missing 'op' field`,
  `<op>: missing animation_path`, `<op>: missing sample_index`,
  `set_axis: missing axis_index`, `Unknown batch op: '<op>'. Valid: add_sample, remove_sample, move_sample, set_sample_animation, set_axis`,
  and the single operations' errors.
- `batch` does not save. A missing array:
  `Missing required parameter: operations (JSON array)`.
- The message is `Batch: <n> OK, <m> errors, <t> total`. With every op
  successful, the data has `results` (one result per op), `ok_count`,
  `error_count` and `total`. **With one failed op, the call is an error, and
  the bridge passes only that message**: which op failed, and why, does not
  reach you. Run `inspect`, then repeat the missing change as a single
  operation to get its error.
