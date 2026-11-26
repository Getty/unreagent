# widget_editor: animations

Widget animations live in the Widget Blueprint. Each animates float
properties of widgets in its tree over time. This tool animates render
opacity and the render transform only; colors, slot positions and other
properties need the editor's animation timeline.

## Operations

- **`create_animation`** with `animation_name` and `length` in seconds
  (default 1.0) creates an empty animation that plays from 0 to `length`.
  The result has `animation_name` and `length`. Error:
  `Animation '<n>' already exists`; the check ignores case.
- **`add_animation_track`** animates one property of one widget:

  | `track_type` | Property |
  |---|---|
  | `opacity` | `RenderOpacity` |
  | `translation_x`, `translation_y` | `RenderTransform.Translation.X` / `.Y`, in Slate units |
  | `scale_x`, `scale_y` | `RenderTransform.Scale.X` / `.Y` |
  | `rotation` | `RenderTransform.Angle`, in degrees (unverified unit) |

  `keyframes` is an array of `{time, value, interp}`: `time` in seconds,
  `interp` `linear`, `constant` or `cubic`. Anything else, `cubic` included,
  becomes the engine's smart-auto cubic key, and `inspect_animation` reports
  it as `cubic`. A missing `time` or `value` is 0, and entries that are not
  objects are skipped. Keys outside 0 to `length` are written anyway and
  will not play inside the animation's range (unverified). The result has
  `track_type`, `property_path` and `keyframe_count`: compare it with the
  number of keys you sent.
- **Each call adds a new track**, even for a widget and track type that
  already have one. Send all keys of a track in one call. The track spans the
  animation's range, 0 to `length`.
- **`list_animations`** returns `animations` (`name`, `start_time`,
  `end_time`, `length`, `bindings_count`).
- **`inspect_animation`** returns `name`, `start_time`, `end_time` and
  `bindings`: per animated widget `widget_name`, `slot_widget_name`,
  `is_root_widget`, `guid` and `tracks` (`class`, `property_name`,
  `property_path`, `sections` with `float_channel_count` and `keyframes`
  `{time, value, interp}`).
- `animation_name` is matched against the display name and the object name,
  ignoring case.

Errors: `Animation not found: <n>`, `Widget not found: <n>`,
`Unknown track type: '<t>'. Valid: opacity, translation_x, translation_y, scale_x, scale_y, rotation`,
`Animation has no MovieScene`, `Failed to create float track`,
`Failed to create track section`, `Section has no float channels`.

## A fade-in and slide

```json
{"operation": "create_animation", "asset_path": "/Game/UI/WBP_PauseMenu.WBP_PauseMenu",
 "animation_name": "Intro", "length": 0.4}
{"operation": "add_animation_track", "asset_path": "/Game/UI/WBP_PauseMenu.WBP_PauseMenu",
 "animation_name": "Intro", "widget_name": "MenuBox", "track_type": "opacity",
 "keyframes": [{"time": 0, "value": 0, "interp": "linear"}, {"time": 0.4, "value": 1}]}
{"operation": "add_animation_track", "asset_path": "/Game/UI/WBP_PauseMenu.WBP_PauseMenu",
 "animation_name": "Intro", "widget_name": "MenuBox", "track_type": "translation_y",
 "keyframes": [{"time": 0, "value": 40}, {"time": 0.4, "value": 0}]}
```

Then `inspect_animation`, `blueprint_modify compile` and `save`.

## Playing it

The tool creates animations, it does not play them. At runtime the widget's
`PlayAnimation` plays one, usually from `Construct` (unverified as reachable
through `blueprint_modify CallFunction`, since the animation is a widget
variable that `VariableGet` does not find; section `events`). A C++ parent can
declare it with `UPROPERTY(Transient, meta = (BindWidgetAnim))` (unverified
for this skill).
