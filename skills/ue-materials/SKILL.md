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

Written from the toolkit's C++ source (commit c1b6709) and, where it says so,
from the UE 5.8.4 engine source; the toolkit targets UE 5.6 and 5.7. Nothing
here has been exercised against a running editor.

## Sections — load the one you need before the call

| Section | What is in it | Load it |
|---|---|---|
| `workflows` | end-to-end step lists with the exact calls: instances of a master material, variants, assigning to a skeletal mesh or a level actor, checking the result with `capture_viewport` | at the start of any material task |
| `instances` | `create_material_instance`, `set_material_parameters`, `get_material_info` in full: results, errors, the overwrite hazard, partial failures | before you create or change an instance |
| `parameters` | the `parameters` object in full: JSON types and coercions, names, texture types, what a name the parent lacks does | before you write a `parameters` object, or when a value did not land |
| `slots` | `set_skeletal_mesh_material` and `set_actor_material` in full: reading a mesh's slots, which actor and component are taken, `material_slot` coercion, errors, results | before you call either slot operation |
| `limits` | what the tool cannot do (master graphs, static switches, listing a parent's parameters, Substrate, Nanite, parameter collections, dynamic instances, static mesh assets), with a decision rule and the route for each | before you promise a material change, or when the table below has no operation for it |
| `failures` | the error table and the failure table (what you see, what it means, what to do) | when a call returns an error, or a material or slot is not in the state you expect |

## Operations

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

## Must not get wrong

- **Instances only.** The tool makes Material Instance Constants. It never
  creates a `Material`, never reads or edits a node graph, and sets only
  scalar, vector and texture parameters. Everything else: section `limits`.
- **It cannot tell you which parameters a parent exposes.**
  `get_material_info` lists only the values an instance overrides; a base
  material returns none. Get the names from the user or from the parent's
  graph (section `limits` has a Python route).
- **A wrong parameter name is not an error.** The tool does not check the
  name against the parent. The engine stores the value under that name on the
  instance, `get_material_info` then lists it, and the material looks the same
  (UE 5.8.4 source, section `parameters`). Names compare without regard to
  case.
- **`create_material_instance` does not check whether the name is taken.**
  An instance already at that path is replaced; an asset of another class
  loaded under that name can crash the editor (section `instances`). Search
  first.
- **Parameter failures inside `create_material_instance` do not fail it**:
  a texture that does not load is only logged, and the instance is saved
  without it. `set_material_parameters` fails, but applies the rest. Read back
  with `get_material_info` after every write.
- **`set_skeletal_mesh_material` changes the mesh asset**, and so every use
  of it. `set_actor_material` changes one placed actor's **first** mesh
  component only; no parameter picks another.
- **`material_slot` must be a JSON number.** An unreadable value such as
  `"one"` or `null` reads as 0, so the call writes slot 0 without an error
  (section `slots`).
- **Engine content is refused.** No path under `/Engine/` or `/Script/` can be
  a parent, a target or a material to assign.

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
  the ones named in this skill, sit under its `data` key (skill `ue-tasks`).
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the state back with `get_material_info` before you retry.

## Paths

`parent_material`, `package_path`, `material_instance_path`, `asset_path`,
`skeletal_mesh_path` and `material_path` pass the toolkit's Blueprint path
check (skill `ue-blueprints`, "Paths"), whose messages say "Blueprint path":
no empty path, no `..`, none of `` <>|&;`$(){}[]!*?~ ``, at most 512
characters, nothing under `/Engine/` or `/Script/`
(`Cannot access engine or script Blueprints`). `asset_name` and the texture
paths inside `parameters` are not checked. Use asset paths such as
`/Game/Materials/MI_Glow`; `get_material_info` reports the object path
(`/Game/Materials/MI_Glow.MI_Glow`).

## The parameters object, short form

```json
{
  "scalars":  {"Roughness": 0.4, "Metallic": 0.0},
  "vectors":  {"BaseColor": {"r": 1.0, "g": 0.2, "b": 0.1, "a": 1.0}},
  "textures": {"Normal": "/Game/Textures/T_Rock_N"}
}
```

Scalars are JSON numbers, vectors objects with `r`, `g`, `b` (and `a`,
default 1), textures texture asset paths. A value of the wrong JSON type is
skipped without an error, and any other key is ignored. Load section
`parameters` before you write one.

## A typical sequence

1. `asset_search` `{"class_filter": "Material", "path_filter": "/Game/Materials"}`
   for the parent, and again for the name you plan to create (skill
   `ue-assets`).
2. `create_material_instance`
   `{"asset_name": "MI_Rock_Wet", "parent_material": "/Game/Materials/M_Rock", "parameters": {"scalars": {"Roughness": 0.2}}}`.
3. `get_material_info` `{"asset_path": "/Game/Materials/MI_Rock_Wet"}`: every
   value you sent is there.
4. `set_actor_material` or `set_skeletal_mesh_material` (section `slots`).
5. Save (`asset save_all`), then look: `capture_viewport` (skill
   `ue-viewport`). Section `workflows` has this in full.

## Background in the plugin

`unreal_get_ue_context` with `category: "material"` (skill `ue-context`)
serves the plugin's notes on UE's material classes and this tool. Its
`get_material_info` example shows `path` as an asset path, but the code
returns the object path, and its parameter names are a naming convention, not
what a given parent defines. The plugin's `Resources/domains/tooling.md` lists
`material` as "Write only"; `get_material_info` reads all the same.

## See also

- `ue-assets`: `asset_search`, `save_asset`, `save_all`, a skeletal mesh's
  slots through `get_asset_info`.
- `ue-level-actors`: finding the actor; `set_property` for component
  properties.
- `ue-viewport`: checking the result in a picture.
- `ue-python`: the `execute_script` routes named in section `limits`.
- `ue-tasks`: the task envelope and timeouts.
