# UE blend spaces: when a call fails

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or empty; for an index, also a negative one |
| `Failed to load blend space: <path>` / `Asset is not a BlendSpace: …` | nothing loads at that path as a blend space |
| `Failed to load animation: <path>` / `Asset is not an AnimSequence: …` | `animation_path` is not an animation sequence |
| `Animation <path> is not compatible with this blend space's skeleton` | `set_sample_animation` with an animation for another skeleton |
| `Sample index <n> out of range (0-<last>)` | stale or wrong index; read it with `inspect` |
| `Axis index <n> out of range …` | axis 1 on a 1D blend space, or above 1 |
| `AddSample failed for <path> at (<x>, <y>)` | the engine refused the sample |
| `Error: Batch: <n> OK, <m> errors, <t> total` | at least one batch op failed |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a change is gone after an editor restart | it was never saved | `save` after each finished change |
| a later call changed the wrong sample | `remove_sample` moved the last sample into the freed index | `inspect`, then use the new indices |
| a 2D sample dropped to `y = 0` | `move_sample` without `y` | pass both `x` and `y` |
| `rate_scale` is not what you sent | you sent 0 or less, which leaves it unchanged, or more than 64 | send a value from 0.01 to 64 |
| the interpolation type did not change | the `interp_type` string was not one of the six names | use one of the six |
| `create` made a 2D blend space | `type` contained neither `1D` nor `AimOffset` as you meant | check the result's `type` |
| `list` returns nothing for a folder with blend spaces | they sit in subfolders | `recursive: true` |
