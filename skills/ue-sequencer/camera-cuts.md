# sequencer: a level sequence with camera cuts

The `sequencer` tool views a sequence; it cannot build one. No toolkit tool
creates a Level Sequence, adds a camera, a track, a section or a key, plays a
sequence or renders it. This section gives the decision rule, a short Python
path for authoring that is **not exercised against an editor**, and the
verification loop that uses the tool. Engine facts marked "UE 5.8.4 source"
were read there, not run; Python names were checked in Epic's Python API
reference (5.6 and 5.7 pages) and in the 5.8.4 source.

## Decide first

| Task | Route |
|---|---|
| look at frames of a sequence that exists, check where its cuts fall | this tool: `open`, `get_state`, `scrub`, `capture_viewport` (loop below) |
| create a sequence, add cameras and cuts, move a cut | `execute_script` with the Python below (skill `ue-python`), or ask the user to do it in the Sequencer editor. Prefer the user for anything beyond a few cameras and cuts: you cannot see the result until you capture it, and every script is a first run |
| animate a camera (keys on its transform or focal length) | Python through `execute_script`; not covered here |
| render frames or a movie | no toolkit tool; Movie Render Queue is the user's job |

## Camera cut track or cinematic shot track

- A **camera cut track** (`MovieSceneCameraCutTrack`) lives in one sequence
  and switches between that sequence's camera bindings. Each section names
  one camera and covers a frame range. Use it for one continuous scene with a
  few cameras.
- A **cinematic shot track** (`MovieSceneCinematicShotTrack`) arranges whole
  sub-sequences, one per shot, each with its own cameras and its own camera
  cut track. Use it for an edit of separately authored shots. When a sequence
  has a shot track, the editor adds no camera cut for a new camera
  (`CreateCameraCutSectionForCamera`, `MovieSceneToolHelpers.cpp`, UE 5.8.4
  source).

## Authoring with Python — not exercised

What `LevelSequenceEditorSubsystem.create_camera` does (UE 5.8.4 source,
`SequencerUtilities.cpp` and `MovieSceneToolHelpers.cpp`): it needs an open
Sequencer; it spawns a `CineCameraActor` **at the level viewport camera's
location and rotation**, binds it (spawnable or not), **locks the viewport to
that camera and switches the camera-cut lock off**, and — only when the
sequence has no camera cut section yet and no shot track — adds a camera cut
section over the whole playback range. Later cameras get no cut.

So: place the viewport first, then create the camera there. Two scripts with
a toolkit call between them:

1. `execute_script`, creating and saving the asset:

   ```python
   tools = unreal.AssetToolsHelpers.get_asset_tools()
   seq = tools.create_asset("SEQ_Demo", "/Game/Cinematics",
                            unreal.LevelSequence, unreal.LevelSequenceFactoryNew())
   seq.set_playback_start(0)
   seq.set_playback_end(240)  # display frames; the range is [start, end)
   unreal.EditorAssetLibrary.save_loaded_asset(seq)
   ```

2. `sequencer` `{"operation": "open", "sequence_path": "/Game/Cinematics/SEQ_Demo", "camera_location": {…}, "camera_rotation": {…}}`
   with the first camera's position, all six values.
3. `execute_script`: `les = unreal.get_editor_subsystem(unreal.LevelSequenceEditorSubsystem)`,
   then `binding_a, cam_a = les.create_camera(True)`. The camera gets the cut
   over the whole range.
4. Move the viewport to the second position. The viewport is now locked to
   camera A, so moving it may move camera A instead; whether `open`'s or
   `capture_viewport`'s camera parameters do that has not been verified.
   Safer: create every camera in one script after the user has placed the
   viewport, or move each camera afterwards with keys (not covered).
5. `execute_script`, adding the second cut. Do not rely on names from an
   earlier script; look everything up again:

   ```python
   seq = unreal.load_asset("/Game/Cinematics/SEQ_Demo")
   les = unreal.get_editor_subsystem(unreal.LevelSequenceEditorSubsystem)
   binding_b, cam_b = les.create_camera(True)
   track = seq.find_tracks_by_type(unreal.MovieSceneCameraCutTrack)[0]
   track.get_sections()[0].set_range(0, 120)   # camera A: frames 0..119
   cut_b = track.add_section()
   cut_b.set_range(120, 240)                    # camera B: frames 120..239
   cut_b.set_camera_binding_id(seq.get_binding_id(binding_b))
   unreal.LevelSequenceEditorBlueprintLibrary.set_lock_camera_cut_to_viewport(True)
   unreal.EditorAssetLibrary.save_loaded_asset(seq)
   ```

Notes on the names, from the sources above:

- `set_range(start, end)` takes **display frames**, start inclusive, end
  exclusive (`MovieSceneSectionExtensions::SetRange`).
- `find_tracks_by_type` includes the camera cut track, which the movie scene
  keeps apart from its other tracks; `add_track(unreal.MovieSceneCameraCutTrack)`
  creates one when none exists.
- `get_binding_id` gives an id valid inside the same sequence; a camera in a
  sub-sequence needs `get_portable_binding_id`.
- `set_lock_camera_cut_to_viewport(True)` gives the viewport back to the
  camera cuts, which `create_camera` switched off. Without it the viewport
  stays on the last camera created, and a capture shows that camera at every
  frame.
- `create_camera` returns a pair in Python, the binding and the actor.

Check after every script: its output and errors (skill `ue-python`),
`get_output_log` (skill `ue-pie-testing`), then the loop below.

## Verifying the cuts

1. Stop PIE. `sequencer` `{"operation": "get_state"}`: note `display_rate`,
   `playback_start_frame`, `playback_end_frame` (one past the last frame,
   section `operations`) and `sequence_path`.
2. For each cut, pick three frames: its first, one in the middle, its last
   (end − 1). For the example above: 0, 60, 119, 120, 180, 239.
3. Per frame: `scrub`, then `get_state` — with the camera cuts locked to the
   viewport, `camera_location` should be the cut's camera (not verified) —
   then `capture_viewport` with that `camera_location` and `camera_rotation`,
   all six values (skill `ue-viewport`), so the viewport redraws without
   moving.
4. A cut is right when the camera position changes between the last frame of
   one cut and the first of the next, and not within a cut (for unanimated
   cameras). Compare the positions with the camera actors' locations from
   `get_level_actors` (skill `ue-level-actors`).
5. `close`, after saving if a script changed the sequence.

If every frame shows the same view: the camera-cut lock is off (step 5's last
line), or the viewport is still locked to one camera. If the view is not a
camera's at all: PIE runs, or the sequence has no camera cut section at that
frame.
