# UE viewport capture: checking a change visually

The skill itself has the parameters, the result and "Getting the picture";
section `asset-preview` the asset editor mode; section `save-picture` the
script that writes a capture to a file.

## Pick the target first

| You check | Target | Why |
|---|---|---|
| an actor, light, fog, post-process or material in the level, at edit time | level viewport: PIE stopped, no `asset_path`, your own camera | the only mode where your camera is applied outside an asset editor |
| a mesh, skeletal mesh, Physics Asset or Animation Blueprint on its own | `asset_path` with a preset, then your own camera | no level clutter; the presets give repeatable views |
| a pose at a frame of an animation | `asset_path` + `frame` | the tool poses the preview itself |
| a Blueprint's components | `asset_path` on the Blueprint (its viewport tab must be open), or the placed actor in the level | Blueprint previews are unverified (section `asset-preview`) |
| what the player sees: movement, a camera rig, effects or materials driven at runtime | PIE: `gameplay_debug` `capture_pie`, or `capture` steps in `run_sequence` | edit-time captures show the level, not the game; skill `ue-pie-testing` |
| a timed moment in play (after a jump, mid-montage) | `run_sequence` with `capture` steps at set delays | manual calls are round trips; you cannot hit a moment by hand |
| a Level Sequence shot | skill `ue-sequencer` (scrub, then a level capture) | — |

Rules that decide it:

- **PIE runs → you get PIE.** Without `asset_path` there is no way to the
  level viewport while PIE runs. `stop_pie`, then `pie_status` until
  `running` is `false` (skill `ue-pie-testing`).
- **Edit time versus play time.** Construction scripts, placed lights and
  materials show at edit time; Tick, BeginPlay, spawned actors, possessed
  cameras and most gameplay-driven effects need PIE.
- **The PIE camera is the game's.** No parameter moves it; the picture shows
  what the player camera shows (a spring arm behind the pawn, per the
  plugin's `debug.md`). Move the pawn or the camera actor through the game.

## The loop: place, capture, look, adjust

1. **Write down what you expect to see** before the first capture: which
   object, roughly where in the frame, what colour or shape. A picture judged
   without an expectation proves little.
2. **Find the target's location**: the actor's transform from the level tools
   (skill `ue-level-actors`), or the asset's size from the asset tools.
3. **Place the camera** some distance from the target, slightly above, and
   aim at it ("Aiming at a point"). Pass all six values.
4. **Capture.** Check `viewport_type` (`Editor`, not `PIE`), and that
   `camera_location` / `camera_rotation` in the result are the values you
   passed.
5. **Look** (skill, "Getting the picture"). Compare with step 1.
6. **Adjust and capture again**:

   | You see | Do |
   |---|---|
   | target not in the picture | recompute the rotation; check the target's location |
   | target tiny | move the camera along the view direction toward it |
   | target cut off | move away, or raise the camera and pitch down more |
   | something blocks the view | another angle: change the yaw by 90° around the target |
   | picture distorted | the viewport is not 16:9 (`original_width`, `original_height`); judge proportions with care |
   | everything black, grey or flat | lighting, view mode or exposure; "What else changes a picture" |

7. **Before concluding anything about depth or position, capture a second
   angle**, roughly 90° of yaw apart. One picture cannot tell near from far.
8. **Report** what you saw against what you expected, with the camera values
   and file names, so the user can reproduce it.

## Before and after

1. Capture with your own camera and keep the result's `camera_location` and
   `camera_rotation`.
2. Make the change.
3. Capture again with exactly those six values, into a second file
   (`before.jpg`, `after.jpg`).

Without the same camera, a difference in the pictures may be the camera's.
An asset capture without your own camera always resets the preview camera to
a preset, which is repeatable as well.

## Aiming at a point

UE uses X forward, Y right, Z up, in centimetres. A rotation is
`{pitch, yaw, roll}` in degrees: yaw turns around Z with 0 along +X and
positive toward +Y (right), pitch looks up for positive values (Epic's
`TRotator` reference). Keep roll at 0. To look from camera `C` at target `T`:

```
dx, dy, dz = T.x - C.x, T.y - C.y, T.z - C.z
yaw   = degrees(atan2(dy, dx))
pitch = degrees(atan2(dz, sqrt(dx*dx + dy*dy)))
```

This is derived from those conventions, not taken from the plugin: confirm it
with the first capture (the target should sit near the centre). For the
distance, a starting guess (not from any source) for a human-sized object is
400 to 600 cm away and 150 to 250 cm above it; adjust from the picture.

## What else changes a picture

The capture shows the viewport as it is drawn, with its view mode, show
flags and stat overlays. Console commands change these: `viewmode lit`,
`viewmode unlit`, `viewmode wireframe`, `viewmode lightingonly`,
`show collision`, `show bounds`, `stat fps`, `stat unit`. They run through
`run_console_command`, which targets the editor's world, or as a `console`
step in `run_sequence` for PIE. Skill `ue-pie-testing`, section `console`,
lists them with their sources and what is unverified. **Reset what you set**
(`viewmode lit`, the same `show` command again) and tell the user, since the
viewport is theirs.

Other reasons a picture looks wrong, none verified through this tool:
lighting or shaders still compiling right after a change, auto-exposure
adapting over the first frames of PIE, and a user who left the viewport in an
unlit or wireframe mode.

## Animation pose check

1. `asset_path` set to the animation, `frame` set to the frame, a `camera`
   preset, or your own camera when the presets do not frame it.
2. Confirm `frame_time` against `play_length`, so you know the frame was not
   clamped.
3. The preview stays paused at that frame, and the tool cannot resume it.
   Tell the user if they work in that editor.
