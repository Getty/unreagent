# UE viewport capture: the asset editor preview

The level and PIE modes, the missing-key rule for a camera of your own, the
result's fields and "Getting the picture" are in the skill itself.

## Asset editor preview

- **`asset_path`** is loaded as an object path. Pass the full form,
  `/Game/Characters/SK_Hero.SK_Hero`. The plugin's own examples pass the
  package path (`/Game/Meshes/SM_MyMesh`); whether that form loads has not
  been verified. On `Asset not found: <path>`, try the object path. Find the
  path with `asset_search` (skill `ue-assets`).
- **The editor opens if needed**, and stays open. The tool's description says
  the editor must already be open; its parameter text and its code open it.
- **Which editors work.** The tool looks for the Persona preview tab, then
  for the Blueprint editor's viewport tab, and inside the tab for a viewport
  widget of the plain editor or the animation editor type. Persona assets
  (skeletal meshes, animations, Animation Blueprints, Physics Assets, as the
  description lists them) are what it was built for. For Blueprints, static
  meshes and other editors it has not been verified, and the errors to expect
  are `No preview viewport tab found for asset: <path>. This editor type may not have a 3D preview.`
  and `Could not find a valid scene viewport in the preview tab.` The tab has
  to be open in that editor: a closed viewport tab is not found.
- **Camera presets.** Without `camera_location` and `camera_rotation`, the
  tool always places the preview camera at a preset. The names are not case
  sensitive:

  | `camera` | Location | Rotation (pitch, yaw, roll) |
  |---|---|---|
  | `front` (default) | `(61.84, 702.57, 117.44)` | `(-5.78, -97.59, 0)` |
  | `from_left` | `(-668.44, 238.13, 122.10)` | `(-4.18, -14.59, 0)` |
  | `from_right` | `(539.56, 322.97, 131.99)` | `(-4.98, -158.99, 0)` |

  These are fixed positions, not fitted to the asset's size: a much larger or
  smaller asset can be cut off or tiny. An unknown name gives the `front`
  position without an error, and the result's `camera_preset` still echoes
  the name you passed.
- **Your own camera.** With either `camera_location` or `camera_rotation`,
  no preset is applied and the result has no `camera_preset`. The same
  missing-key rule applies as in the level viewport. The preview camera stays
  where you put it.
- Before reading the pixels, the tool draws the preview once. A viewport that
  still has no size, as can happen right after the editor opens, gets five
  more tries 0.1 s apart. After that the call fails with
  `Viewport has invalid size.`

## Animation frames

`frame` poses an animation before the capture. It needs `asset_path`, and the
asset must be an animation asset, such as an Animation Sequence or a Montage.
Otherwise the call fails with
`'frame' parameter requires an animation asset (AnimSequence, AnimMontage, etc.).`

- The value is rounded to a whole frame. A negative `frame` is ignored.
- An Animation Sequence converts frames with its own sampling rate. Any other
  animation asset is treated as 30 fps, and its `total_frames` is 0.
- The time is clamped to the animation's length: a frame past the end is
  taken as the end, without an error. Compare `frame_time` with `play_length`.
- The tool looks for the first skeletal mesh component that previews exactly
  this asset, pauses it and moves it to that time. The preview stays paused
  there. Without such a component the call fails with
  `Could not find preview component playing this animation. Make sure the animation preview is visible.`
