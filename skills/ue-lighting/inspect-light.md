# lighting: the fields `inspect_light` returns

What this result must not be taken for (the read names are not the write
names, what cannot be read back, `settings_sample`) is in section
`operations`, under `inspect_light`. The result names no intensity units and
no mobility (section `properties`, "Intensity units").

Returns `actor_name`, `actor_label`, `class`, `type`, `location`, `rotation`
and the fields of that type:

| `type` | Fields |
|---|---|
| all four standard lights | `intensity`, `light_color`, `temperature`, `use_temperature`, `cast_shadows`, `affects_world` |
| `PointLight` | also `attenuation_radius`, `source_radius`, `soft_source_radius` |
| `SpotLight` | also `attenuation_radius`, `inner_cone_angle`, `outer_cone_angle`, `source_radius` |
| `DirectionalLight` | also `light_shaft_occlusion`, `light_shaft_bloom` |
| `RectLight` | also `source_width`, `source_height`, `barn_door_angle`, `barn_door_length` |
| `SkyLight` | `intensity`, `light_color`, `cast_shadows`, `cubemap` (a path, or `None`), `sky_distance_threshold`, `lower_hemisphere_is_solid_color`, `source_type` (`CapturedScene`, `SpecifiedCubemap` or `Unknown`), `captures_scene` |
| `SkyAtmosphere` | `bottom_radius`, `atmosphere_height`, `ground_albedo`, `rayleigh_scattering_scale`, `mie_scattering_scale` |
| `ExponentialHeightFog` | `fog_density`, `fog_height_falloff`, `start_distance`, `fog_cutoff_distance`, `fog_max_opacity` |
| `PostProcessVolume` | `enabled`, `infinite_extent`, `blend_radius`, `blend_weight`, `priority`, `settings_sample` |
