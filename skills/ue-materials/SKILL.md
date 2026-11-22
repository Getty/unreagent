---
name: ue-materials
description: >
  Create material instances, set their scalar, vector and texture parameters,
  and assign materials to skeletal mesh slots or level actors with material.
  Use before you work on material instances or material slots in UE.
toolkit_tools: [material]
---

# UE materials: material

The UE LLM Toolkit plugin's **`material`** tool creates Material Instance
Constants, sets their parameters, reads a material's info and puts a material
into a slot of a skeletal mesh or of an actor placed in the level. Through the
plugin's MCP bridge it is listed as `unreal_material`. It works only while the
editor is running and the plugin is reachable; `unreagent-iterate` covers the
wait for the plugin.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

The tool takes an `operation` string, lowercases it and accepts these aliases:

| Operation | Aliases | Required | Optional |
|---|---|---|---|
| `create_material_instance` | `create` | `asset_name`, `parent_material` | `package_path`, `parameters` |
| `set_material_parameters` | `set_params`, `set_parameters` | `material_instance_path`, `parameters` | — |
| `get_material_info` | `get_info`, `info`, `inspect` | `asset_path` | — |
| `set_skeletal_mesh_material` | `set_mesh_material` | `skeletal_mesh_path`, `material_path` | `material_slot` |
| `set_actor_material` | `set_actor` | `actor_name`, `material_path` | `material_slot` |

`blueprint_path` and `path` are accepted in place of `asset_path`. An unknown
operation returns
`Unknown operation: '<op>'. Valid: create_material_instance, set_material_parameters, set_skeletal_mesh_material, set_actor_material, get_material_info`,
plus ` Did you mean '<x>'?` when a valid name is close.

Through the bridge, `material_slot` is listed as a string, because the tool
declares it `integer`, a type the bridge does not map. Send a JSON number.
Whether a numeric string converts depends on the engine's JSON classes, which
this skill has not verified.

## What the tool cannot do

- **No base materials.** It creates Material Instance Constants only, never a
  `Material`, and it cannot read or change a material's node graph.
- **Scalar, vector and texture parameters only.** Static switches and other
  parameter types cannot be set.
- **It cannot list the parameters a parent material exposes.**
  `get_material_info` returns only the values stored on an instance (see
  below). Get the parameter names from the parent material itself.
- **No static mesh asset slots.** Of mesh assets, only skeletal meshes can be
  changed. A placed actor's mesh component, a static mesh component included,
  is reached with `set_actor_material`. The `asset` tool's
  `set_asset_property` cannot reach a static mesh asset's material slots
  either: they are an array of structs (skill `ue-assets`).

## Saving

`create_material_instance` saves the new asset to disk itself. **The other
writes do not save.** They mark the package dirty and leave the change in the
editor's memory, where an editor stop or crash loses it. Save with the `asset`
tool (skill `ue-assets`): `save_asset` with `asset_path` for a material
instance or a skeletal mesh, `save_all` after `set_actor_material`, which
changes the level. No operation has an undo.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the state back with `get_material_info` before you retry
  (skill `ue-tasks`).

## Paths

`parent_material`, `package_path`, `material_instance_path`, `asset_path`,
`skeletal_mesh_path` and `material_path` pass the toolkit's Blueprint path
check (skill `ue-blueprints`, "Paths"), with its messages, which say
"Blueprint path": no empty path, no `..`, none of the characters
`` <>|&;`$(){}[]!*?~ ``, at most 512 characters, and **nothing under
`/Engine/` or `/Script/`** (`Cannot access engine or script Blueprints`). An
engine material can therefore be neither a parent nor assigned to a slot.
Texture paths inside `parameters` are not checked.

The tool's own examples use asset paths such as `/Game/Materials/MI_Glow`.
`get_material_info` reports the object path (`/Game/Materials/MI_Glow.MI_Glow`).

## The parameters object

`create_material_instance` and `set_material_parameters` take the same
`parameters` object:

```json
{
  "scalars":  {"Roughness": 0.4, "Metallic": 0.0},
  "vectors":  {"BaseColor": {"r": 1.0, "g": 0.2, "b": 0.1, "a": 1.0}},
  "textures": {"Normal": "/Game/Textures/T_Rock_N"}
}
```

- Each key is the name of a parameter in the parent material. **The tool does
  not check that the parent has a parameter of that name.** It hands the value
  to the engine's `Set…ParameterValueEditorOnly`. What the engine does with a
  name the parent does not define has not been verified for this skill.
- `scalars` take JSON numbers. A value the engine's JSON classes cannot read
  as a number, such as an object, is skipped without an error.
- `vectors` take objects with `r`, `g`, `b` and `a`. The tool defaults `a` to
  1 and nothing else: pass `r`, `g` and `b`. A value that is not an object is
  skipped without an error.
- `textures` take texture asset paths. A path that does not load fails with
  `Failed to load texture: <path>`; several failures are joined with `; `. A
  value that cannot be read as a string, such as an object, is skipped without
  an error.
- Whether a number written as a string, such as `"0.4"`, counts as a number
  is up to the engine's JSON classes and has not been verified. Send the JSON
  types above.
- Any other key in `parameters` is ignored.

## create_material_instance

Creates `<package_path><asset_name>` as a Material Instance Constant of
`parent_material`, applies `parameters` and saves the package.
`package_path` defaults to `/Game/Materials/`; a missing trailing `/` is
added. The parent may be a material or another material instance.

The result has `asset_path` (the package path), `asset_name`,
`parent_material` as you passed it, and `saved: true`. The message is
`Created material instance: <path>`.

- **Parameter failures do not fail the call.** A texture that does not load is
  only logged in the editor's log, as
  `Material instance created but some parameters failed: …`, and the instance
  is saved without it. Read the instance back with `get_material_info`.
- **The tool does not check whether an asset already exists** at that path. It
  builds the new instance there and saves it, so it can overwrite or collide
  with whatever is there. A `Failed to load …` from another tool does not mean
  the name is free. Check with the toolkit's `asset_search` tool first:
  `path_filter` set to the folder, `name_pattern` to the name. Its
  `name_pattern` matches substrings and `path_filter` includes subfolders, so
  the name is taken only if an entry has exactly that `name` and that
  `package_path`. `asset_search` writes `package_path` without the trailing
  slash: `/Game/Materials`, not `/Game/Materials/`.
- Errors: `Failed to load parent material: <path>`,
  `Failed to create package: <path>`, `Failed to create material instance`,
  and `Material instance created but failed to save: <path>`. After the last
  one the instance exists in the editor's memory: save it with
  `asset save_asset` rather than creating it again.

## set_material_parameters

Applies `parameters` to an existing Material Instance Constant. A base
material, or any path that does not load as a Material Instance Constant,
fails with `Failed to load material instance: <path>`. A missing `parameters`
object fails with `Missing required parameter: parameters`.

The result has `material_instance` (the path you passed) and `modified: true`.
The message is `Updated parameters on material instance: <path>`. Save
afterwards.

**A texture that does not load fails the call, but the rest of the change
lands.** The tool does not stop at the first error. It applies every entry it
can read and collects the failures: all scalars, all vectors and every texture
that loads are set in memory, and only the textures in the error message are
missing. On this path the tool skips its change notification and does not
mark the package dirty itself; whether the engine's setters do is not
verified. Send the failed textures again with fixed paths; a call that
succeeds marks the package dirty, and then you save.

## get_material_info

Takes `asset_path`, a material or a material instance. The result has `name`,
`path` (the object path), `class` (such as `Material` or
`MaterialInstanceConstant`) and `is_instance`. The message is
`Material info: <name>`.

For an instance, it also has:

- `parent`: the parent's object path, present only when the instance has a
  parent;
- `scalar_parameters`, `vector_parameters` (`{r, g, b, a}` each) and
  `texture_parameters` (a texture path, or `None`), keyed by parameter name.

These are the values stored on the instance itself. In UE those are the
parameters the instance overrides; parameters it inherits unchanged from its
parent are not listed. A base material (`is_instance: false`) returns no
parameters at all, so this operation cannot tell you which parameters a
material exposes.

## set_skeletal_mesh_material

Puts `material_path` into slot `material_slot` (default 0) of the skeletal
mesh asset. This changes the asset, and so every use of that mesh.

- `asset get_asset_info` on the skeletal mesh lists its slots with `slot`,
  `slot_name` and `material` (skill `ue-assets`).
- A negative slot fails with `material_slot must be >= 0`; one past the end
  with
  `Material slot <n> out of range. Skeletal mesh has <m> material slots.`
- Load errors: `Failed to load skeletal mesh: <path>`,
  `Failed to load material: <path>`.

The result has `skeletal_mesh` (your path), `slot`, `old_material` and
`new_material` (object names; `None` for an empty slot). The message is
`Set material slot <n> on <mesh> to <material>`. Save the mesh afterwards.

## set_actor_material

Puts `material_path` into slot `material_slot` (default 0) of an actor in the
level open in the editor. Only that actor's component changes, not the mesh
asset.

- **`actor_name`** is the actor's object name or its label; the first actor
  with either one is taken. The name passes the toolkit's actor-name check,
  which rejects a label such as `Crate (2)` (skill `ue-level-actors` covers
  addressing actors). An actor that is not found gives
  `Actor not found: <name>. Use level_query find to search for actors in the current level`.
- **The first mesh component of the actor is used.** There is no parameter to
  pick another one. The result's `component` and `component_type` tell you
  which one it was. An actor without one fails with
  `Actor '<name>' has no mesh component`.
- Slot errors: `material_slot must be >= 0`,
  `Material slot <n> out of range. Mesh component has <m> material slots.`
- Other errors: `Failed to load material: <path>`, and
  `No active world. Open a level in the editor first`.

The result has `actor` (the object name), `component`, `component_type`,
`slot`, `old_material` and `new_material`. The message is
`Set material slot <n> on actor '<name>' (<component class>) to <material>`.
The actor and the level are marked dirty: save with `asset save_all`.

## A typical sequence

1. `asset_search` with `class_filter: "Material"` (or
   `MaterialInstanceConstant`) to find the parent, and once more for the name
   you plan to create (see `create_material_instance`).
2. `create_material_instance`
   `{"asset_name": "MI_Rock_Wet", "parent_material": "/Game/Materials/M_Rock", "parameters": {"scalars": {"Roughness": 0.2}}}`.
3. `get_material_info` `{"asset_path": "/Game/Materials/MI_Rock_Wet"}`: check
   that every value you sent is there.
4. `set_actor_material` or `set_skeletal_mesh_material` with the new
   `asset_path`.
5. Save: `asset save_all`.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or an empty string |
| `Cannot access engine or script Blueprints` | a path under `/Engine/` or `/Script/` |
| `Failed to load parent material: <path>` / `Failed to load material: <path>` | no material at that path |
| `Failed to load material instance: <path>` | not a Material Instance Constant, or nothing at that path |
| `Failed to load texture: <path>` | a texture in `parameters` did not load |
| `Material instance created but failed to save: <path>` | the instance exists in memory but is not on disk |
| `Material slot <n> out of range. …` | the slot index is past the mesh's or component's last slot |
| `Actor '<name>' has no mesh component` | the actor has no mesh to take a material |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a parameter you set is not in `get_material_info` | the tool could not read its value as the listed type and skipped it, or a texture failed during `create_material_instance` | send it again with the type from "The parameters object" |
| a parameter shows in `get_material_info` but the material looks unchanged | probably no parameter of that name in the parent | check the name in the parent material |
| a slot change is gone after an editor restart | the mesh or level was never saved | `asset save_asset` or `save_all` after each change |
| `set_actor_material` changed the wrong component | the actor has several mesh components and the first was taken | not selectable with this tool; tell the user |
| `Cannot access engine or script Blueprints` for a material | engine materials are refused by the path check | use or create a material under `/Game/` |

## Background in the plugin

- `unreal_get_ue_context` with `category: "material"` (skill `ue-context`)
  serves the plugin's notes on UE's material classes and on this tool. Its
  example for `get_material_info` shows `path` as an asset path, but the code
  returns the object path. Its list of parameter names is a naming
  convention, not what any given parent material defines.
- The plugin's `Resources/domains/tooling.md` lists `material` as "Write only";
  `get_material_info` reads all the same.

## See also

- `ue-assets`: `asset_search`, `save_asset`, `save_all`, and a skeletal mesh's
  slots through `get_asset_info`.
- `ue-level-actors`: finding the actor for `set_actor_material`.
- `ue-blueprints`: the path check these paths share.
- `ue-tasks`: the task envelope and timeouts.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
