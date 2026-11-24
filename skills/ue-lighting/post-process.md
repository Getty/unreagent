# lighting: the three post-process operations

The keys of `settings` are in section `properties`.

## inspect_post_process

Returns `actor_name`, `actor_label`, `class`, `location`, `rotation`,
`is_infinite`, `blend_radius`, `blend_weight`, `priority`, `is_enabled`, and
one object per group, **each present only when at least one of its settings
has its override on**:

| Object | Fields, each only when overridden |
|---|---|
| `bloom` | `enabled` (always `true`, with `intensity`), `intensity`, `threshold`, `method` (`convolution` or `standard`) |
| `depth_of_field` | `focal_distance`, `fstop`, `sensor_width`, `min_fstop`, `blade_count` |
| `motion_blur` | `amount`, `max` |
| `exposure` | `compensation`, `min_brightness`, `max_brightness`, `speed_up`, `speed_down` |
| `color_grading` | `white_balance_temp`, `white_balance_tint`, `saturation`, `contrast`, `gamma`, `gain` (each `{r, g, b, a}`), `color_grading_lut` (a path, or `None`) |
| `vignette` | `intensity` |
| `ambient_occlusion` | `intensity`, `radius` |

A setting that is absent is not overridden by this volume; its stored value is
not shown. `ao_quality` can be set but is never reported. These read names
differ from the write keys (`bloom.intensity` here, `bloom_intensity` in
`settings`).

Another kind of actor fails with `Actor '<name>' is not a PostProcessVolume
(class: <class>)`. The message on success is `Post-process volume inspected`.

## set_post_process

Takes `actor_name` and a `settings` object (section `properties`). Every key
applied **turns that setting's override on** and writes the value. The result
holds `actor_name`, `actor_label` and `settings_applied`; the message is
`Set <n> post-process settings on '<name>'`.

- A missing or non-object `settings` fails with `Settings object is required`.
- **No override can be switched off** with this tool, and it cannot change the
  volume's own fields: enabled, infinite extent, blend radius, blend weight,
  priority. Those are properties of the actor (`bEnabled`, `bUnbound`,
  `BlendRadius`, `BlendWeight`, `Priority` in the code this tool reads them
  from); `set_property` of skill `ue-level-actors` is the tool for actor
  properties. That path has not been verified for a post-process volume.
- A `color_grading_lut` path that does not load as a texture is skipped
  without an error.
- A `bloom_method` other than `convolution` or `fft` selects the standard
  method and is still listed as applied.

## spawn_post_process_volume

| Parameter | Default | Meaning |
|---|---|---|
| `location` | `{x:0, y:0, z:0}` | world location |
| `extent` | `{x:200, y:200, z:200}` | **read and then ignored**, see below |
| `infinite` | `false` | the volume affects the whole level |
| `label` | engine-chosen | the Outliner label |
| `settings` | — | the post-process keys of section `properties` |

The result holds `actor_name`, `actor_label`, `location`, `rotation` and
`is_infinite`. The message is `Spawned PostProcessVolume`. There is no
`rotation` parameter; the volume is spawned unrotated.

- **`extent` has no effect.** The tool's description documents it and the
  dispatch reads it, but the code that spawns the volume never uses the value.
  The code spawns the volume actor and gives it no size at all: it sets no
  brush and no bounds. What bounds such a volume has in the engine has not
  been verified; it may have none, in which case a volume that is not
  infinite affects nothing. **`infinite: true` is the only setting known to
  make the volume apply**, so pass it unless you can check the result. For a
  bounded volume, the size must be set by other means; `move_actor` with
  `scale` (skill `ue-level-actors`) is the only handle the actor tools offer,
  and whether scaling gives the bounds you want has not been verified.
- `settings` is applied, but the result carries **no `settings_applied`**.
  Confirm with `inspect_post_process`.
