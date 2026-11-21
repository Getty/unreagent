# widget_editor: animations

Widget animations live in the Widget Blueprint. Each animates float
properties of widgets in its tree over time.

## Operations

- **`create_animation`** with `animation_name` and `length` in seconds
  (default 1.0) creates an empty animation that plays from 0 to `length`.
  Error: `Animation '<n>' already exists`.
- **`add_animation_track`** animates one property of one widget. `track_type`
  is `opacity` (render opacity), `translation_x`, `translation_y`, `scale_x`,
  `scale_y` or `rotation` (render transform). `keyframes` is an array of
  `{time, value, interp}`: `time` in seconds, `interp` `linear`, `constant`
  or `cubic` (the default; any other value also means cubic). A missing `time`
  or `value` is 0, and entries that are not objects are skipped; compare the
  result's `keyframe_count` with what you sent.
- **Each call adds a new track**, even for a widget and track type that
  already have one. Send all keys of a track in one call. The track spans the
  animation's range, 0 to `length`.
- **`list_animations`** returns `animations` (`name`, `start_time`,
  `end_time`, `length`, `bindings_count`). **`inspect_animation`** returns the
  widgets the animation binds, with their tracks, sections and keyframes.
  `animation_name` is matched against the display name and the object name.

Errors: `Animation not found: <n>`, `Widget not found: <n>`,
`Unknown track type: '<t>'. Valid: opacity, translation_x, translation_y, scale_x, scale_y, rotation`,
`Animation has no MovieScene`, `Failed to create float track`,
`Failed to create track section`, `Section has no float channels`.
