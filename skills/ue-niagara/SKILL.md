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

Written from the toolkit's C++ source (commit c1b6709) and, where it says so,
from the UE 5.8.4 engine source; the toolkit targets UE 5.6 and 5.7. Nothing
here has been exercised against a running editor.

## Sections — load the one you need before the call

| Section | What is in it | Load it |
|---|---|---|
| `workflows` | end-to-end step lists with the exact calls: inspecting a system, tuning its user parameters, putting an effect into the level that stays, checking it with `capture_viewport` | at the start of any Niagara task |
| `reading` | `inspect`, `list`, `get_parameters`, `get_emitter_info` in full: every result field, the value formats, the renderer details | before you read a system, or when a result field is unclear |
| `set-parameter` | `set_parameter` in full: the value per type, the loose coercions that pass a typo, the errors | before you call `set_parameter` |
| `spawn` | `spawn_system` in full: why it is a transient preview owned by World Settings, `label`, culling, its errors | before you call `spawn_system` |
| `limits` | what cannot be done with this tool (authoring systems and emitters, per-instance overrides, removing a spawned preview) and where each goes | before you promise a VFX change, or when no operation fits |
| `failures` | the error table and the failure table (what you see, what it means, what to do) | when a call returns an error, or a parameter or effect is not in the state you expect |

## Operations

The tool takes an `operation` string, lowercases it and accepts these aliases:

| Operation | Aliases | Required | Optional |
|---|---|---|---|
| `inspect` | `info`, `get_info` | `asset_path` | — |
| `list` | `find`, `search` | — | `folder_path` (`/Game/`), `recursive` (`true`) |
| `get_parameters` | `params`, `get_params` | `asset_path` | — |
| `get_emitter_info` | `emitter`, `emitter_info` | `asset_path`, and `emitter_name` or `emitter_index` | — |
| `set_parameter` | `set_param` | `asset_path`, `parameter_name`, `value` | — |
| `spawn_system` | `spawn` | `asset_path` | `location`, `rotation`, `scale`, `auto_destroy` (`true`), `label` |

`path` and `system_path` are accepted in place of `asset_path`. An unknown
operation returns
`Unknown operation: '<op>'. Valid: inspect, list, get_parameters, get_emitter_info, set_parameter, spawn_system`,
plus ` Did you mean '<x>'?` when a valid name is close.

## Must not get wrong

- **No authoring.** The tool creates no system or emitter, adds or removes no
  emitter, module or renderer, and changes nothing but the default value of a
  `User.` parameter on the system asset. Everything else: section `limits`.
- **`set_parameter` writes the asset's default**, so every use of the system
  that does not override the parameter changes. There is no operation for one
  placed component's value.
- **Seven settable types**: float, int, bool, vector2, vector3, vector4,
  color. Read the type with `get_parameters` first.
- **Send every component of a vector or colour.** A missing component, `a`
  included, reads as 0 without an error: a colour without `a` gets alpha 0.
- **A bool takes any string or number.** A typo such as `"ture"` sets
  `false` without an error. Send JSON `true` or `false`.
- **`spawn_system` is a preview, not placement.** UE's run-time spawn creates
  a transient Niagara component owned by the level's World Settings actor; it
  is never saved with the level (UE 5.8.4 source, section `spawn`). For an
  effect that must stay, section `workflows`.
- **Never pass `label` to `spawn_system`.** It renames the owning actor,
  which is World Settings.
- **No path check.** Systems under `/Engine/` and plugin content load and can
  be changed in memory; do not `set_parameter` outside `/Game/` (below).

## Saving

**No operation saves.** `set_parameter` marks the system's package dirty, and
`spawn_system` marks the level dirty (its effect is not saved even so). The
change stays in the editor's memory, where an editor stop or crash loses it.
Save with the `asset` tool (skill `ue-assets`): `save_asset` with the system's
`asset_path`, or `save_all`. No operation has an undo: read the old value
with `get_parameters` before you change it.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key (skill `ue-tasks`).
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the state back with `get_parameters` before you retry.

## Paths

`asset_path` is loaded as given: the asset path (`/Game/FX/NS_Fire`) and the
object path that `list` returns (`/Game/FX/NS_Fire.NS_Fire`) both work.
Anything that does not load as a Niagara system fails with
`Failed to load Niagara system: <path>`.

**Do not `set_parameter` on a system outside `/Game/`.** A change to an
`/Engine/` system stays in memory and cannot be saved: `asset save_asset`
rejects `/Engine/` paths and `save_all` skips them (skill `ue-assets`).
Whether a change to plugin content, such as a system under `/Niagara/`, saves
has not been verified, and it is not the project's content either way. Tell
the user, who can duplicate the system into `/Game/` in the Content Browser;
`asset duplicate` rejects `/Engine/` paths as well.

To find systems, use `list` here or `asset_search` with
`class_filter: "NiagaraSystem"` (skill `ue-assets`), which also pages and
filters by name.

## Short reference

- **`inspect`**: `system_name`, `asset_path` (object path), `emitter_count`,
  `emitters[]` (`name`, `enabled`, usually `sim_target`, `local_space`,
  `renderer_count`, `renderer_types`) and `user_parameters[]`.
- **`get_parameters`**: `parameters[]` with `name` (without `User.`), `type`,
  `value`, `size_bytes`. Vectors and colours come back as **strings** such as
  `"(1, 0.5, 0)"`; send objects to `set_parameter`.
- **`set_parameter`**: `parameter_name` with or without `User.`, any case.
  Values: a number for float and int, `true`/`false` for bool,
  `{"x","y"}`, `{"x","y","z"}`, `{"x","y","z","w"}`, `{"r","g","b","a"}`.
- **`spawn_system`**: `location`, `rotation` (`pitch`, `yaw`, `roll`,
  degrees), `scale`; a missing key keeps its default.

Sections `reading`, `set-parameter` and `spawn` have each in full.

## A typical sequence

1. `list` `{"folder_path": "/Game/FX/"}` or `asset_search` to find the system.
2. `get_parameters` `{"asset_path": "/Game/FX/NS_Fire"}`: names, types, old
   values.
3. `set_parameter`
   `{"asset_path": "/Game/FX/NS_Fire", "parameter_name": "SpawnRate", "value": 200}`.
4. `get_parameters` again: the new default is there.
5. `asset save_asset` `{"asset_path": "/Game/FX/NS_Fire"}`.
6. Look at it: section `workflows`, "Verify".

## Background in the plugin

The plugin ships no context notes for Niagara: `unreal_get_ue_context`
(skill `ue-context`) has no Niagara category.

## See also

- `ue-assets`: `asset_search`, `save_asset`, `save_all`.
- `ue-level-actors`: `spawn_actor` and `set_property`, the route to a placed
  Niagara actor (section `workflows`).
- `ue-viewport`: checking an effect in a picture.
- `ue-materials`: the materials a Niagara renderer uses are ordinary
  materials and instances.
- `ue-python`: the `execute_script` routes in section `limits`.
- `ue-tasks`: the task envelope and timeouts.
