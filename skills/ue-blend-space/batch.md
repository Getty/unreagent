# UE blend spaces: batch

`batch` runs several of `add_sample`, `remove_sample`, `move_sample`,
`set_sample_animation` and `set_axis` on one asset (`asset_path`, given once)
in one call, with a single geometry rebuild at the end
(`FBlendSpaceEditor::ExecuteBatch`).

`operations` is an array of objects. Each names its op in `op` (lowercased) and
carries **the same parameter names as the single operation**: `animation_path`,
`x`, `y`, `rate_scale`, `sample_index`, `axis_index`, and for `set_axis` every
field of the single call (`name` works as well as `axis_name`).

```json
{"operation": "batch", "asset_path": "/Game/Anims/BS_Walk.BS_Walk",
 "operations": [
   {"op": "set_axis", "axis_index": 0, "axis_name": "Speed", "min": 0, "max": 600},
   {"op": "add_sample", "animation_path": "/Game/Anims/Idle.Idle", "x": 0},
   {"op": "add_sample", "animation_path": "/Game/Anims/Walk.Walk", "x": 300}
 ]}
```

## How it runs

- Ops run in order on the same in-memory asset; each sees the effect of the
  ones before it. Put `set_axis` ops first, so no sample grows the range.
- A `remove_sample` moves the last sample into the freed index **for every op
  after it**. Avoid index-based ops after a removal in the same batch, or
  compute the shifted indices yourself.
- Validation and the geometry rebuild run **once, after the last op**, also
  when ops failed. Snapping and de-duplication (section `ue-facts`) therefore
  happen only then.
- A failed op does not stop the ones after it, and nothing is rolled back.
- `batch` does not save.

## Errors

- Per op: `[<i>] not a JSON object`, `[<i>] missing 'op' field`,
  `add_sample: missing animation_path`,
  `set_sample_animation: missing animation_path`,
  `remove_sample: missing sample_index`, `move_sample: missing sample_index`,
  `set_sample_animation: missing sample_index`,
  `set_axis: missing axis_index`,
  `Unknown batch op: '<op>'. Valid: add_sample, remove_sample, move_sample, set_sample_animation, set_axis`,
  and the single operations' errors. A batch index is not checked for being
  negative up front; a negative one fails as out of range.
- Whole call: `Missing required parameter: operations (JSON array)`, and the
  load errors of `asset_path`.

## Result

The message is `Batch: <n> OK, <m> errors, <t> total`. When every op
succeeded, `data` has `results` (one `{success, message, …}` per op, in
order; `add_sample` entries carry `sample_index`), `ok_count`, `error_count`,
`total`.

**With one failed op the call is an error, and the bridge passes only the
message**: `results` does not reach you. Recover:

1. `inspect` and compare with what the batch should have produced.
2. Send each missing change as a single operation; its error names the cause.
