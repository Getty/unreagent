# UE blend spaces: the `inspect` result

The fields at the top of the result (`name`, `path`, `type`, `dimensions`,
`anim_length`, `loop`) are in the skill itself, "Reading". These are the
remaining items of that list.

- `axes`, one entry per dimension: `index`, `name`, `min`, `max`,
  `grid_divisions`, `snap_to_grid`, `wrap_input`, and `interpolation` with
  `time`, `damping_ratio`, `max_speed` and `type` (`Average`, `Linear`,
  `Cubic`, `EaseInOut`, `ExponentialDecay`, `SpringDamper`, or `Unknown`).
- `samples`: `index`, `animation` (object path) and `animation_name` (both
  `None` for a sample without an animation), `position` with `x` (plus `y` on a
  2D blend space), `rate_scale`. On UE 5.6 and later each sample also has
  `single_frame` (always `false` on 5.6); from 5.7, `frame_index` is added when
  `single_frame` is true and the sample has an animation. `sample_count` counts them.
- `settings`: `notify_trigger_mode`, `interpolate_using_grid`,
  `preferred_triangulation`, `target_weight_interpolation_speed`,
  `allow_mesh_space_blending`, `allow_marker_based_sync`. The tool reads these
  but cannot set them.
- `geometry`: the number of `triangles` and `segments` the engine built from
  the samples.
