---
name: ue-niagara
description: >
  Inspect Niagara systems, set their user parameters and spawn them in the
  level with niagara. Use before you work on a Niagara VFX asset or place an
  effect in the editor.
toolkit_tools: [niagara]
---

# UE Niagara: niagara

The UE LLM Toolkit plugin's **`niagara`** tool reads Niagara systems, sets the
defaults of their user parameters and spawns a system in the level open in the
editor. Through the plugin's MCP bridge it is listed as `unreal_niagara`. It
works only while the editor is running and the plugin is reachable;
`unreagent-iterate` covers the wait for the plugin.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

The tool takes an `operation` string, lowercases it and accepts these aliases:

| Operation | Aliases | Required | Optional |
|---|---|---|---|
| `inspect` | `info`, `get_info` | `asset_path` | — |
| `list` | `find`, `search` | — | `folder_path`, `recursive` |
| `get_parameters` | `params`, `get_params` | `asset_path` | — |
| `get_emitter_info` | `emitter`, `emitter_info` | `asset_path`, and `emitter_name` or `emitter_index` | — |
| `set_parameter` | `set_param` | `asset_path`, `parameter_name`, `value` | — |
| `spawn_system` | `spawn` | `asset_path` | `location`, `rotation`, `scale`, `auto_destroy`, `label` |

`path` and `system_path` are accepted in place of `asset_path`. An unknown
operation returns
`Unknown operation: '<op>'. Valid: inspect, list, get_parameters, get_emitter_info, set_parameter, spawn_system`,
plus ` Did you mean '<x>'?` when a valid name is close.

## What the tool cannot do

- **No authoring.** It cannot create a system or an emitter, add or remove
  emitters, modules or renderers, or change anything but the default value of
  a user parameter.
- **User parameters only.** `set_parameter` reaches the system's exposed
  `User.` parameters, nothing inside an emitter.
- **Seven parameter types.** float, int, bool, vector2, vector3, vector4 and
  color can be set. Any other type fails (see `set_parameter`).
- **It changes no placed effect.** `set_parameter` writes the system asset.
  There is no operation that sets a parameter on a Niagara component in the
  level.

## Saving

**No operation saves.** `set_parameter` marks the system's package dirty, and
`spawn_system` marks the level dirty. The change stays in the editor's memory,
where an editor stop or crash loses it. Save with the `asset` tool (skill
`ue-assets`): `save_asset` with the system's `asset_path`, or `save_all`. No
operation has an undo.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the state back with `get_parameters` or the level tools
  before you retry (skill `ue-tasks`).

## Paths

`asset_path` is loaded as given: the asset path (`/Game/FX/NS_Fire`) and the
object path that `list` returns (`/Game/FX/NS_Fire.NS_Fire`) both work.
Anything that does not load as a Niagara system fails with
`Failed to load Niagara system: <path>`. The tool runs no path check of its
own, so systems under `/Engine/` and plugin content load as well. That
includes `set_parameter`, which then changes a system outside your project.

**Do not `set_parameter` on a system outside `/Game/`.** A change to an
`/Engine/` system stays in memory and cannot be saved: `asset save_asset`
rejects `/Engine/` paths and `save_all` skips them (skill `ue-assets`).
Whether a change to plugin content, such as a system under `/Niagara/`, saves
has not been verified, and it is not the project's content either way. Tell
the user, who can duplicate the system into `/Game/` in the Content Browser;
`asset duplicate` rejects `/Engine/` paths as well.

To find systems, use `list` here or the toolkit's `asset_search` tool with
`class_filter: "NiagaraSystem"` (skill `ue-assets`), which also pages and
filters by name.

## inspect

The whole system in one call. The result has `system_name`, `asset_path` (the
object path), `emitter_count`, `emitters[]` and `user_parameters[]`. The
message is `Niagara system inspected`.

- Each emitter has `name` and `enabled`. When the emitter has data, which is
  the usual case, it adds `sim_target` (`CPU` or `GPU`), `local_space`,
  `renderer_count` and `renderer_types`, the renderer classes without
  `Niagara` and `RendererProperties`, such as `Sprite`, `Mesh`, `Ribbon` or
  `Light`.
- `user_parameters[]` has the same entries as `get_parameters`, without
  `size_bytes`.

## list

| Parameter | Default | Meaning |
|---|---|---|
| `folder_path` | `/Game/` | content folder |
| `recursive` | `true` | include subfolders |

Lists the Niagara systems in the folder from the asset registry, without
loading them. Emitter assets are not listed, nor classes derived from
`NiagaraSystem`. There is no paging and no name filter. The result has
`folder`, `count` and `systems[]`, each with `name` and `path` (the object
path). The message is `Found <n> Niagara systems`. An empty list is a success.

## get_parameters

The system's user parameters. The result has `system_name`, `asset_path` (as
you passed it), `parameter_count` and `parameters[]`. The message is
`Found <n> user parameters`. Each entry has:

| Field | Meaning |
|---|---|
| `name` | the parameter's name without the `User.` prefix |
| `type` | `float`, `int`, `bool`, `vector2`, `vector3`, `vector4`, `color`, `quaternion`, `matrix`, or the engine's own type name for anything else |
| `value` | the default value, see below |
| `size_bytes` | the value's size |

`value` is a JSON number for `float` and `int` and a boolean for `bool`.
Vectors and colors come back as **strings**, such as `"(1, 0.5, 0)"` for a
vector3 or `"(1, 0, 0, 1)"` for a color in r, g, b, a order. Other types come
back as their type in angle brackets, such as `"<matrix>"`, and a parameter
without data as `"<no data>"`. To set a vector, send an object, not this
string (see `set_parameter`).

## get_emitter_info

One emitter in more detail. Pass `emitter_name` (compared without regard to
case) or `emitter_index` (0-based). When both are given, `emitter_name` wins.
A negative `emitter_index` counts as not given.

- Neither: `Must specify either emitter_name or emitter_index`.
- No emitter of that name: `Emitter '<name>' not found. Available: <names>`.
- Index too high: `Emitter index <n> out of range (0-<last>)`.

The result has `name` and `enabled`. When the emitter has data, it adds
`sim_target`, `local_space`, `deterministic`, `random_seed` and `renderers[]`.
The message is `Emitter info retrieved`.

Each renderer has `class_name` (shortened as in `inspect`) and `enabled`, plus:

| Renderer | Extra fields |
|---|---|
| Sprite | `alignment`, `facing_mode`, `sub_image_size` (`"<w>x<h>"`) |
| Mesh | `facing_mode`, `mesh_count` |
| Ribbon | `facing_mode`, `shape_mode`, `draw_direction` |
| Light | `type: "Light"` |

The enum values are UE's own names for those settings.

## set_parameter

Sets the default value of one user parameter on the system asset.

- **`parameter_name`** is the name with or without `User.`, compared without
  regard to case. A name that is not there fails with
  `Parameter '<name>' not found on system '<path>'. Available: <names>`.
- **`value`** must match the parameter's type:

| Type | `value` |
|---|---|
| `float` | a JSON number |
| `int` | a JSON number, rounded to the nearest integer |
| `bool` | `true` or `false` |
| `vector2` | `{"x": …, "y": …}` |
| `vector3` | `{"x": …, "y": …, "z": …}` |
| `vector4` | `{"x": …, "y": …, "z": …, "w": …}` |
| `color` | `{"r": …, "g": …, "b": …, "a": …}` |

- **Pass every component.** The tool gives no component a default, `a`
  included, and reads each with the engine's `FJsonObject::GetNumberField`,
  which cannot report a failure: a missing component reads as 0, and so does
  `null`, an array, an object or a string that is not numeric. `true` and
  `false` read as 1 and 0, and a string made of an optional sign, digits and
  at most one dot as that number.
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
  A colour without `a` gets alpha 0. The only
  trace of a missing or unreadable component is a `LogJson` line in the
  editor log
  (`FJsonObject::GetField` in
  `Engine/Source/Runtime/Json/Private/Dom/JsonObject.cpp`,
  `FJsonValue::AsNumber` in `JsonValue.cpp` beside it; read in the UE 5.8.4
  source, not run, while the toolkit targets UE 5.6 and 5.7).
- **A value of the wrong kind fails** and names the expected form, with two
  hyphens:
  `Parameter '<name>' is float -- 'value' must be a number`. The same pattern
  covers `int`, `bool` (`must be a boolean`), `vector2`
  (`must be an object {x,y}`), `vector3` (`{x,y,z}`), `vector4`
  (`{x,y,z,w}`) and `color` (`{r,g,b,a}`).
- **The check is looser than those messages** (`FJsonObject::TryGetNumberField`
  into a `double`, and `TryGetBoolField`; same source and version as above).
  For `float` and `int`, a string made of an optional sign, digits and at most
  one dot, such as `"2.5"`, passes as a number, and so do `true` and `false`,
  as 1 and 0; an `int` is rounded.
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
  **For `bool`, any string and any number
  passes**: `true`, `yes`, `on` and `false`, `no`, `off` count in any case,
  and every other string is `false` unless it starts with a non-zero integer.
  The editor language's words for True, Yes, False and No count as well
  (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`).
  A typo sets `false`, and `must be a boolean` is left for `null`, an array or
  an object. Send the JSON types from the table.
- **Any other type** fails with
  `Unsupported parameter type '<type>' for parameter '<name>'`. Read the type
  with `get_parameters` first.

Through the bridge, `value` is listed as a string, because the tool declares
it `string/number/object`, a type the bridge does not map. No parameter type
takes a string: send the JSON number, boolean or object from the table.

The result has `parameter_name` (as you passed it), `type` and `asset_path`.
The message is `Set parameter '<name>' on '<system>'`. The package is marked
dirty: save it.

The tool writes the value straight into the system's parameter store. Whether
open Niagara editors, or effects already placed in the level, show the new
default before the system is saved and reloaded has not been verified for this
skill. Read it back with `get_parameters`, which reads the same store.

## spawn_system

Spawns the system in the level open in the editor, through UE's
`UNiagaraFunctionLibrary::SpawnSystemAtLocation`, and activates it.

| Parameter | Default | Meaning |
|---|---|---|
| `location` | `{"x": 0, "y": 0, "z": 0}` | world position |
| `rotation` | `{"pitch": 0, "yaw": 0, "roll": 0}` | rotation in degrees |
| `scale` | `{"x": 1, "y": 1, "z": 1}` | scale |
| `auto_destroy` | `true` | handed to the engine's spawn call as its auto-destroy flag. By the tool's description the spawned system then auto-destroys; the engine's auto-destroy removes it once it has finished (not verified) |
| `label` | — | label for the owning actor, see below |

A missing component of `location`, `rotation` or `scale` takes its default.

The result has `actor_name` and `actor_label` (the actor that owns the new
Niagara component), `asset_path`, `location`, `rotation` and `scale`. The
message is `Spawned Niagara system '<system>' as '<label>'`.

**Treat this operation as a preview, not as placing an actor.** UE's function
is made for effects at run time. It creates a Niagara component, not an actor
of its own. Which actor the component belongs to has not been verified for
this skill; it is probably one the level already has, such as World Settings.
Two consequences follow:

- **`label` renames that actor**, whichever it is. Spawn once without `label`
  and look at `actor_name` before you pass one.
- **The effect is probably not saved with the level.** The level is marked
  dirty, but whether the component survives a save and reload has not been
  verified. With `auto_destroy` left at `true`, an effect that does not loop
  probably removes itself once it has played.

For an effect that must stay in the level, check after a save and reload
whether it is still there. If it is not, tell the user: this tool has no
other way to place one.

Errors, after the system has loaded:

- `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?`
- `No active world. Open a level in the editor first`
- `Failed to spawn Niagara system '<path>' in the level`
- `Niagara component spawned but has no owning actor`

## A typical sequence

1. `list` `{"folder_path": "/Game/FX/"}` or `asset_search` to find the system.
2. `get_parameters` `{"asset_path": "/Game/FX/NS_Fire"}`: read the names and
   types.
3. `set_parameter`
   `{"asset_path": "/Game/FX/NS_Fire", "parameter_name": "SpawnRate", "value": 200}`.
4. `get_parameters` again to check the new default.
5. Save: `asset save_asset` `{"asset_path": "/Game/FX/NS_Fire"}`.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | `operation`, `asset_path` or `parameter_name` is absent or an empty string |
| `Failed to load Niagara system: <path>` | nothing at that path, or not a Niagara system |
| `Must specify either emitter_name or emitter_index` | `get_emitter_info` without either |
| `Emitter '<name>' not found. Available: …` | no emitter of that name |
| `Emitter index <n> out of range (0-<last>)` | the index is past the last emitter |
| `Parameter '<name>' not found on system '<path>'. Available: …` | no user parameter of that name |
| `Parameter '<name>' is <type> -- 'value' must be …` | `value` has the wrong JSON type |
| `Unsupported parameter type '<type>' for parameter '<name>'` | the parameter's type cannot be set by this tool |
| `No active world. Open a level in the editor first` | `spawn_system` with no level open |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a vector parameter has a wrong component after `set_parameter` | a component was left out of `value` | send every component |
| `set_parameter` succeeded but the effect in the level looks the same | not verified whether placed effects pick up a new default at once | save the system; check again after a reload |
| a new default is gone after an editor restart | the system was never saved | `asset save_asset` after `set_parameter` |
| `spawn_system` renamed an actor you did not expect | `label` went to the actor that owns the component, probably World Settings | tell the user, with the old and new label; how to rename that actor back has not been verified. Spawn without `label` from then on |
| the spawned effect is gone | `auto_destroy` removed it, or it was never saved with the level | see `spawn_system`; tell the user if it must stay |

## Background in the plugin

The plugin ships no context notes for Niagara: `unreal_get_ue_context`
(skill `ue-context`) has no Niagara category.

## See also

- `ue-assets`: `asset_search`, `save_asset` and `save_all`.
- `ue-level-actors`: `get_level_actors`, to see which actor `actor_name` names.
- `ue-tasks`: the task envelope and timeouts.
- `ue-context`: the plugin's context notes.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
