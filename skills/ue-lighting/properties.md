# lighting: the keys of `properties` and `settings`

These are all the keys the `lighting` tool reads. The tool's description and
header name none of them; the tables come from `LightingEditor.cpp` at toolkit
commit 9f6a0f7, and the last column is the line in that file that reads the
key. Any other key is ignored without an error.

Rules for every table:

- **Pass the JSON type shown.** The tool reads each key with the engine's
  `FJsonObject::TryGetNumberField` (into a `double`), `TryGetBoolField`,
  `TryGetStringField` or `TryGetObjectField`, and a key that call cannot read
  is skipped like an unknown key. What those calls accept besides the type
  shown (`Engine/Source/Runtime/Json/Private/Dom/JsonObject.cpp`, and
  `TJsonValueString` in `Public/Dom/JsonValue.h`; read in the UE 5.8.4 source,
  not run, while the toolkit targets UE 5.6 and 5.7):
  - A number key also reads `true` and `false` as 1 and 0, and a string made
    of an optional sign, digits and at most one dot, such as `"0.5"`. Any
    other string, such as `"1e3"` or `"5 lux"`, is skipped.
  - **A boolean key reads any string and any number, and never skips one.**
    `true`, `yes`, `on` and `false`, `no`, `off` count in any case; every
    other string is `false` unless it starts with a non-zero integer, so a
    typo sets `false` and the key is still listed as applied. A number is
    `true` when it is not 0.
  - A string key reads a number or a boolean as its text, `5` or `true`.
- **Check the result.** `properties_set` or `settings_applied` lists the keys
  that were applied. `spawn_light` and `spawn_post_process_volume` return no
  list; inspect afterwards.
- **No value is range-checked** by the toolkit, apart from the byte colours.
- **Units and meaning are the engine's.** Each key is written to the engine
  field named in the table. This document does not restate what the field
  does.

## Colours

Colour keys take an object with lowercase `r`, `g`, `b`. A channel you leave
out is 0. The scale differs by key, and the tool does not tell you when you
got it wrong:

| Scale | Keys | Rule |
|---|---|---|
| either | `color` (lights, sky light) | if **any** channel is above 1, all three are read as bytes, clamped to 0–255 and truncated. Otherwise they are read as linear 0–1 and converted to an sRGB byte colour |
| bytes only | `ground_albedo`, `volumetric_fog_albedo` | clamped to 0–255 and truncated. `{r: 0.4, g: 0.4, b: 0.4}` becomes `(0, 0, 0)` |
| linear, as given | `rayleigh_scattering`, `mie_scattering`, `mie_absorption`, `fog_inscattering_color`, `directional_inscattering_color`, `volumetric_fog_emission` | written unchanged, no clamp |
| linear with alpha | `lower_hemisphere_color` | also reads `a`, default 1 |

For `color`, the two readings meet badly at the low end: `{r: 1, g: 0, b: 0}`
is full red, not a byte value of 1. `inspect_light` returns the stored colour
as `light_color` in bytes, whichever form you sent.

## Lights: spawn_light, set_light_properties

Read by `ApplyLightProperties`.

| Key | JSON | Light types | Written to | Line |
|---|---|---|---|---|
| `intensity` | number | all | `SetIntensity` | 103 |
| `color` | `{r, g, b}` | all | `SetLightColor` | 110 |
| `temperature` | number | all | `Temperature` | 136 |
| `use_temperature` | boolean | all | `bUseTemperature` | 143 |
| `cast_shadows` | boolean | all | `SetCastShadows` | 149 |
| `affects_world` | boolean | all | `bAffectsWorld` | 155 |
| `indirect_lighting_intensity` | number | all | `IndirectLightingIntensity` | 161 |
| `volumetric_scattering_intensity` | number | all | `VolumetricScatteringIntensity` | 167 |
| `lighting_channels` | `{channel0, channel1, channel2}`, booleans | all | `LightingChannels` | 174 |
| `attenuation_radius` | number | point, spot | `SetAttenuationRadius` | 194 |
| `source_radius` | number | point, spot | `SourceRadius` | 199 |
| `soft_source_radius` | number | point, spot | `SoftSourceRadius` | 204 |
| `inner_cone_angle` | number | spot | `SetInnerConeAngle` | 212 |
| `outer_cone_angle` | number | spot | `SetOuterConeAngle` | 217 |
| `source_width` | number | rect | `SourceWidth` | 227 |
| `source_height` | number | rect | `SourceHeight` | 232 |
| `barn_door_angle` | number | rect | `BarnDoorAngle` | 237 |
| `barn_door_length` | number | rect | `BarnDoorLength` | 242 |

- **A key for another light type is skipped**, without an error: the
  type-specific keys sit behind a cast of the light's component (lines 192,
  210, 225). `inner_cone_angle` on a point light does nothing.
- `attenuation_radius`, `source_radius` and `soft_source_radius` are read only
  where the component is a point light component (line 192). The code treats a
  spot light as one. A rect light has its own block, without these three keys;
  if UE's rect light component is not a point light component, which is the
  engine's class layout and was not checked here, the three are skipped on a
  rect light. `properties_set` tells.
- `lighting_channels` changes only the channels you name. It is listed as
  applied even when the object names none.
- A directional light has no keys of its own. Its direction is the actor's
  rotation: `move_actor` (skill `ue-level-actors`). `light_shaft_occlusion`
  and `light_shaft_bloom` can be read with `inspect_light` and not set here.
- The keys marked with a setter (`Set…`) go through the component's function.
  The others are raw field writes. All are followed by one render-state
  refresh (line 249).

## Sky light: set_sky_light

Read in `SetSkyLight`, in this order. The order matters when `cubemap` fails.

| Key | JSON | Written to | Line |
|---|---|---|---|
| `intensity` | number | `Intensity` | 865 |
| `color` | `{r, g, b}` | `LightColor` | 872 |
| `cubemap` | string, an asset path | `Cubemap` | 899 |
| `source_type` | string: `captured_scene` or `specified_cubemap`, case ignored | `SourceType` | 915 |
| `lower_hemisphere_color` | `{r, g, b, a}` | `LowerHemisphereColor` | 930 |
| `lower_hemisphere_is_solid_color` | boolean | `bLowerHemisphereIsBlack` | 944 |
| `cloud_ambient_occlusion` | boolean | `bCloudAmbientOcclusion` | 950 |
| `cloud_ambient_occlusion_strength` | number | `CloudAmbientOcclusionStrength` | 956 |
| `recapture` | boolean | calls `RecaptureSky` when `true` | 962 |

- **`cubemap` is the one key that can fail the call**: `Failed to load
  cubemap: <path>` (line 910) when the path does not load as a cube texture.
  `intensity` and `color` from the same call are already written by then.
  Setting `cubemap` does not change `source_type`; pass
  `"source_type": "specified_cubemap"` as well if that is what you mean.
- **Any other `source_type` string is skipped**, including `CapturedScene` and
  `SpecifiedCubemap`, the spellings `inspect_light` returns.
- `lower_hemisphere_is_solid_color` writes the field the engine calls
  `bLowerHemisphereIsBlack`. The key's name is the toolkit's.
- `recapture` is listed as applied only when `true`. `false` does nothing.
  Without it the tool asks for no recapture; whether the sky light shows a new
  cubemap, source type or scene without one has not been verified. Pass
  `"recapture": true` in the call that changes them.

## Sky atmosphere: set_sky_atmosphere

Read in `SetSkyAtmosphere`.

| Key | JSON | Written to | Line |
|---|---|---|---|
| `rayleigh_scattering` | `{r, g, b}` | `RayleighScattering` | 1018 |
| `rayleigh_exponential_distribution` | number | `RayleighExponentialDistribution` | 1029 |
| `mie_scattering` | `{r, g, b}` | `MieScattering` | 1036 |
| `mie_absorption` | `{r, g, b}` | `MieAbsorption` | 1048 |
| `mie_anisotropy` | number | `MieAnisotropy` | 1059 |
| `mie_exponential_distribution` | number | `MieExponentialDistribution` | 1065 |
| `atmosphere_height` | number | `AtmosphereHeight` | 1071 |
| `ground_albedo` | `{r, g, b}`, bytes 0–255 | `GroundAlbedo` | 1078 |
| `multi_scattering_factor` | number | `MultiScatteringFactor` | 1091 |

`inspect_light` returns `bottom_radius`, `rayleigh_scattering_scale` and
`mie_scattering_scale` for an atmosphere. None of the three is a write key.
Of the nine keys above it reads back only `atmosphere_height` and
`ground_albedo`.

## Height fog: set_fog

Read in `SetFog`.

| Key | JSON | Written to | Line |
|---|---|---|---|
| `fog_density` | number | `FogDensity` | 1153 |
| `fog_height_falloff` | number | `FogHeightFalloff` | 1159 |
| `fog_inscattering_color` | `{r, g, b}` | `FogInscatteringLuminance` | 1166 |
| `fog_max_opacity` | number | `FogMaxOpacity` | 1177 |
| `start_distance` | number | `StartDistance` | 1183 |
| `fog_cutoff_distance` | number | `FogCutoffDistance` | 1189 |
| `directional_inscattering_exponent` | number | `DirectionalInscatteringExponent` | 1195 |
| `directional_inscattering_start_distance` | number | `DirectionalInscatteringStartDistance` | 1201 |
| `directional_inscattering_color` | `{r, g, b}` | `DirectionalInscatteringLuminance` | 1208 |
| `volumetric_fog` | boolean | `bEnableVolumetricFog` | 1219 |
| `volumetric_fog_scattering_distribution` | number | `VolumetricFogScatteringDistribution` | 1225 |
| `volumetric_fog_albedo` | `{r, g, b}`, bytes 0–255 | `VolumetricFogAlbedo` | 1232 |
| `volumetric_fog_emission` | `{r, g, b}` | `VolumetricFogEmissive` | 1246 |
| `volumetric_fog_extinction_scale` | number | `VolumetricFogExtinctionScale` | 1257 |
| `volumetric_fog_distance` | number | `VolumetricFogDistance` | 1263 |
| `second_fog_density` | number | `SecondFogData.FogDensity` | 1269 |
| `second_fog_height_falloff` | number | `SecondFogData.FogHeightFalloff` | 1275 |
| `second_fog_height_offset` | number | `SecondFogData.FogHeightOffset` | 1281 |

- The two `…_color` keys write the engine's luminance fields. The key names
  are the toolkit's.
- `inspect_light` reads back `fog_density`, `fog_height_falloff`,
  `start_distance`, `fog_cutoff_distance` and `fog_max_opacity` only.
- The fog's height is the actor's location: `move_actor` (skill
  `ue-level-actors`).

## Post-process: set_post_process, spawn_post_process_volume

Read by `ApplyPostProcessSettingsFromJson`. Every key applied also sets the
setting's override flag.

| Key | JSON | Written to | Line |
|---|---|---|---|
| `bloom_intensity` | number | `BloomIntensity` | 292 |
| `bloom_threshold` | number | `BloomThreshold` | 298 |
| `bloom_method` | string | `BloomMethod` | 305 |
| `dof_focal_distance` | number | `DepthOfFieldFocalDistance` | 319 |
| `dof_fstop` | number | `DepthOfFieldFstop` | 325 |
| `dof_sensor_width` | number | `DepthOfFieldSensorWidth` | 331 |
| `dof_min_fstop` | number | `DepthOfFieldMinFstop` | 337 |
| `dof_blade_count` | number, truncated to an integer | `DepthOfFieldBladeCount` | 343 |
| `motion_blur_amount` | number | `MotionBlurAmount` | 350 |
| `motion_blur_max` | number | `MotionBlurMax` | 356 |
| `exposure_compensation` | number | `AutoExposureBias` | 363 |
| `exposure_min_brightness` | number | `AutoExposureMinBrightness` | 369 |
| `exposure_max_brightness` | number | `AutoExposureMaxBrightness` | 375 |
| `exposure_speed_up` | number | `AutoExposureSpeedUp` | 381 |
| `exposure_speed_down` | number | `AutoExposureSpeedDown` | 387 |
| `white_balance_temp` | number | `WhiteTemp` | 394 |
| `white_balance_tint` | number | `WhiteTint` | 400 |
| `saturation` | number, or `{r, g, b, a}` | `ColorSaturation` | 406 |
| `contrast` | number, or `{r, g, b, a}` | `ColorContrast` | 412 |
| `gamma` | number, or `{r, g, b, a}` | `ColorGamma` | 418 |
| `gain` | number, or `{r, g, b, a}` | `ColorGain` | 424 |
| `color_grading_lut` | string, a texture's asset path | `ColorGradingLUT` | 431 |
| `vignette_intensity` | number | `VignetteIntensity` | 443 |
| `ao_intensity` | number | `AmbientOcclusionIntensity` | 450 |
| `ao_radius` | number | `AmbientOcclusionRadius` | 456 |
| `ao_quality` | number | `AmbientOcclusionQuality` | 462 |

- **`bloom_method`**: `convolution` or `fft`, case ignored, selects the
  convolution method. **Every other string selects the standard method** and
  is listed as applied, a typo included.
- **`saturation`, `contrast`, `gamma`, `gain`** take a number, which is used
  for all four channels, or an object whose missing channels are 1. The key
  only has to be present: a value of any other JSON type sets the setting to
  `(1, 1, 1, 1)`, turns the override on, and is listed as applied (lines
  262–283).
- **`color_grading_lut`** is applied only when the path loads as a texture.
  Otherwise it is skipped, with no error and no entry in `settings_applied`.
- `inspect_post_process` reports these settings under other names, grouped
  (`bloom.intensity`, `exposure.compensation`, …), and never reports
  `ao_quality`.
- No key switches an override off, and none reaches the volume's own fields
  (enabled, blend, priority, infinite extent). See `set_post_process` in the
  skill body.
