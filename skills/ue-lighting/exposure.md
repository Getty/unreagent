# lighting: exposure — auto, fixed, manual, and the units

Exposure decides how bright the final picture is, independent of how much
light the scene holds. The `lighting` tool writes five exposure settings into
a post-process volume (section `properties`, "Post-process"). Engine facts
here are from the UE 5.8.4 source (`Engine/Classes/Engine/Scene.h`,
`Engine/Private/Scene.cpp`, `SceneView.cpp`), read, not run, and from Epic's
"Auto Exposure" documentation page.

## What the five keys write

| Key | Engine field | Unit / meaning |
|---|---|---|
| `exposure_compensation` | `AutoExposureBias` ("Exposure Compensation") | stops, base 2: 0 no change, +1 twice as bright, −1 half. Only used with a tonemapper. Its default comes from the project (`r.DefaultFeature.AutoExposure.Bias`, engine default 1.0) |
| `exposure_min_brightness` | `AutoExposureMinBrightness` | **EV100 or cd/m2, depending on the project** (below) |
| `exposure_max_brightness` | `AutoExposureMaxBrightness` | the same |
| `exposure_speed_up` | `AutoExposureSpeedUp` | f-stops per second, dark to bright; default 3 |
| `exposure_speed_down` | `AutoExposureSpeedDown` | f-stops per second, bright to dark; default 1 |

Each key also turns its override on, and no override can be switched off
again with this tool. `inspect_post_process` reports them under `exposure`
(`compensation`, `min_brightness`, `max_brightness`, `speed_up`,
`speed_down`).

## Which unit Min and Max are in

The project setting **"Extend default luminance range in Auto Exposure
settings"** (`r.DefaultFeature.AutoExposure.ExtendDefaultLuminanceRange`)
decides:

| Setting | Min / Max are | Engine defaults for Min / Max |
|---|---|---|
| on (`True` / `1`) | **EV100** | −10 / 20 |
| off (`0`), the engine's own default | pixel luminance, **cd/m2** | 0.03 / 8 |

The fields are the same; only their meaning changes (the `Scene.h` comment on
both fields). The editor labels them "Min EV100" / "Max EV100" when the
setting is on. **The project wizard turns it on for new projects**
(`GameProjectUtils.cpp`, `AddPostProcessingConfigValues`, UE 5.8.4 source); a
project converted from UE4 may have it off.

To find out: `read_file` on `Config/DefaultEngine.ini` (skill
`unreagent-files`) and look for that line under
`[/Script/Engine.RendererSettings]`. Absent means the engine default, off.
`run_console_command` cannot read it: every `r.` command is blocked there
(skill `ue-pie-testing`).

**Send Min/Max in the wrong unit and nothing errors**: an EV100 value of 8
read as 8 cd/m2 (or the reverse) gives a very different picture.

## Auto exposure (the default)

The engine default method is **Auto Exposure Histogram**; "Basic" is the
cheaper alternative (Epic, "Auto Exposure"). The picture adapts to the scene's
luminance between Min and Max, at the two speeds. Use it for gameplay, where
the camera moves between bright and dark areas. Tune with
`exposure_compensation` first; narrow Min/Max when the adaptation swings too
far.

## A fixed exposure with this tool

**Eye adaptation is disabled when Min equals Max** (`Scene.h`, comment on both
fields). That fixes the exposure without changing the method, which this tool
cannot do:

```json
{"operation": "set_post_process", "actor_name": "GlobalPP",
 "settings": {"exposure_min_brightness": 4, "exposure_max_brightness": 4}}
```

There is no universal value: it depends on the light levels in the scene.
Bracket it — three captures at, say, 0, 4 and 8 EV100, a higher value giving
a darker picture — then narrow in, and fine-tune with
`exposure_compensation`. Use this before you compare two
captures of the same scene: with adaptation on, brightness depends on where
the camera looked last and for how long.

## True manual mode — not reachable with `lighting`

The method (`AutoExposureMethod`: Histogram, Basic, Manual) and "Apply
Physical Camera Exposure" have no key in this tool. In **Manual**, exposure
ignores scene luminance and comes from the camera settings; with "Apply
Physical Camera Exposure" on, ISO, aperture and shutter speed drive it, and
most scenes look much darker (Epic, "Auto Exposure"). A post-process volume's
settings have that flag **on** by default (`Scene.cpp`, the
`FPostProcessSettings` constructor), so a switch to Manual alone may darken
the scene.

Two paths, **neither exercised against an editor**:

- `execute_script` (skill `ue-python`). Python names from Epic's Python API
  reference (`PostProcessSettings`, `AutoExposureMethod`):

  ```python
  ppv = ...  # the APostProcessVolume actor, found by label
  s = ppv.get_editor_property("settings")
  s.set_editor_property("override_auto_exposure_method", True)
  s.set_editor_property("auto_exposure_method", unreal.AutoExposureMethod.AEM_MANUAL)
  s.set_editor_property("override_auto_exposure_apply_physical_camera_exposure", True)
  s.set_editor_property("auto_exposure_apply_physical_camera_exposure", False)
  ppv.set_editor_property("settings", s)
  ```

  `settings` is a struct, returned as a copy: the last line writes it back.
- `set_property` (skill `ue-level-actors`) on the whole struct `Settings` in
  UE's text form, for example
  `"(bOverride_AutoExposureMethod=True,AutoExposureMethod=AEM_Manual)"`. The
  toolkit imports the text into the existing struct; whether the members you
  do not name keep their values is the engine's import behaviour and has not
  been verified. Inspect the volume before and after.

With Manual and physical camera exposure off, the camera's defaults (ISO 100,
aperture 1.0, shutter 1.0; Epic, "Auto Exposure") apply and
`exposure_compensation` shifts from there. Confirm every one of these with a capture:
`inspect_post_process` does not report the method.
