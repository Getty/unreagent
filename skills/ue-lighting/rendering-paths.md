# lighting: Lumen, MegaLights or baked lighting

How a level is lit is a **project** decision, made in Project Settings >
Engine > Rendering, not by the `lighting` tool. The tool places and tunes
lights; whether they bounce, how their shadows are computed and whether a
lighting build is needed follows from these settings. Sources: Epic's
documentation pages "Lumen Global Illumination and Reflections" and
"MegaLights", the UE 5.7 and 5.8 release announcements
(https://forums.unrealengine.com/t/unreal-engine-5-7-released/2673913,
https://forums.unrealengine.com/t/unreal-engine-5-8-released/2729274), the
UE 5.8 release notes
(https://dev.epicgames.com/documentation/en-us/unreal-engine/unreal-engine-5-8-release-notes),
all read 2026-11-26, and the UE 5.8.4 source (read, not run).

## The three paths

| Path | What it is | Needs | Pick it when |
|---|---|---|---|
| **Lumen** | dynamic global illumination and reflections, no build step | GI method and reflection method set to Lumen; mesh distance fields for software ray tracing; hardware ray tracing optional | the default. Time of day, moving lights or objects, fast iteration; current-generation consoles and PCs |
| **MegaLights** | stochastic direct lighting for many shadow-casting lights at a roughly constant cost | "Support Hardware Ray Tracing", Shader Model 6; not the forward renderer | dozens or more shadowed local lights (interiors, cities at night). It replaces direct lighting and shadowing, not GI, and is meant to run beside Lumen |
| **Baked** (static lighting, lightmaps) | GI precomputed into lightmaps by a lighting build | "Allow Static Lighting" on; Static or Stationary lights; a lighting build after every change | hardware where Lumen is too expensive — mobile, Switch, VR, low-end PCs — and fully static scenes |

Rules that decide between them:

- **Lumen hides lightmaps.** "When Lumen is enabled for a project,
  precomputed static lighting contributions are disabled and all lightmaps are
  hidden", and Static lights are not supported with Lumen (Epic, Lumen page).
  Do not mix the two in one project.
- **A lighting build is outside this toolkit.** No tool builds lighting. In a
  baked project, every change you make with `lighting` is wrong in the picture
  until someone runs Build > Build Lighting; say so in your report.
- **MegaLights needs hardware ray tracing**, and does not support mobile,
  Switch or previous-generation consoles, the forward renderer, water,
  clouds or heterogeneous volumes (Epic, MegaLights page). Directional lights
  are off by default (`r.MegaLights.DirectionalLights` 0 in the UE 5.8.4
  source); a sun stays on the regular path.
- **Do not switch a project's path on your own.** It changes every level, and
  some settings need an editor restart (distance fields, ray tracing). Ask the
  user, and report the change.

## Status per engine version

| | UE 5.6 | UE 5.7 | UE 5.8 |
|---|---|---|---|
| Lumen | default for new projects (Epic's Lumen page) | the same | the same; adds **Lumen Lite**, "up to twice as fast as Lumen High Quality" for handheld and lower-end PCs |
| MegaLights | Experimental | **Beta** ("advances to Beta"; better directional light, translucency, Niagara shadow support) | **Production-Ready** |

New-project defaults: Epic's Lumen page says new projects have Lumen GI and
reflections on, and projects converted from UE4 do not. The UE 5.8.4 project
wizard writes `r.DynamicGlobalIlluminationMethod=1`, `r.ReflectionMethod=1`,
`r.GenerateMeshDistanceFields=True`, `r.Shadow.Virtual.Enable=1` and, for
most project types, `r.AllowStaticLighting=0` (`GameProjectUtils.cpp`). The
5.6 and 5.7 wizards were not read. How Lumen Lite is switched on is not
covered here: not sourced. The UE 5.8.4 project-settings tooltip of MegaLights
still says "Experimental feature"; the release announcement is the later word.

## Reading a project's settings

`read_file` on `Config/DefaultEngine.ini` (skill `unreagent-files`), section
`[/Script/Engine.RendererSettings]`:

| Line | Values | Engine default when absent (UE 5.8.4 source) |
|---|---|---|
| `r.DynamicGlobalIlluminationMethod` | 0 none, 1 Lumen, 2 SSGI, 3 plugin | 0 |
| `r.ReflectionMethod` | 0 none, 1 Lumen, 2 SSR | 2 |
| `r.AllowStaticLighting` | `True`/`False` | on |
| `r.GenerateMeshDistanceFields` | `True`/`False` | — |
| `r.RayTracing` | "Support Hardware Ray Tracing" | — |
| `r.MegaLights.EnableForProject` | the MegaLights project setting | — |
| `r.DefaultFeature.AutoExposure.ExtendDefaultLuminanceRange` | exposure units (section `exposure`) | 0 |

"—": not checked here. Platform configs, device profiles and scalability can
override these per platform; this table reads the project default only.
`run_console_command` cannot read or set any of them: every `r.` command is
blocked (skill `ue-pie-testing`).

To change one: ask the user to change it in Project Settings, or edit the
line with `edit_file` while the editor is stopped and restart it
(`unreagent-iterate`). The editor reads these at start; whether it overwrites
a hand edit made while it runs has not been verified.

## Per volume and per light

A post-process volume can override the project's choice for the space it
covers: GI method, reflection method and MegaLights on/off are post-process
settings (`DynamicGlobalIlluminationMethod`, `ReflectionMethod`,
`bMegaLights` with their override flags in `Scene.h`). The `lighting` tool has
no keys for them; the paths of section `exposure`, "True manual mode", apply
the same way (Python names `override_b_mega_lights`, `mega_lights` in Epic's
`PostProcessSettings` reference). Not exercised.

Per light, under MegaLights: `bAllowMegaLights` (a `bool`) excludes the light,
and `MegaLightsShadowMethod` (a `TEnumAsByte`) picks Default, Ray Tracing or
Virtual Shadow Map — Epic recommends ray tracing for most local lights and
virtual shadow maps sparingly. Both are on the light component:
`set_property` with `LightComponent.bAllowMegaLights` (skill
`ue-level-actors`). Not exercised.

## When a light has no visible effect on bounce or shadows

| Symptom | Check |
|---|---|
| direct light but no bounce, dark shadowed areas | `r.DynamicGlobalIlluminationMethod` is not 1, or the light is Static |
| a light change shows only after a long delay, or never | a baked project without a rebuild |
| reflections show only what is on screen | `r.ReflectionMethod` 2 (SSR), not Lumen |
| many local lights, shadows missing or noisy | MegaLights on and the light excluded, or MegaLights' own noise; compare with it off in a test volume |
