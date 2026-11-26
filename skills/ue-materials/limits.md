# UE materials: what the material tool cannot do

For each gap: what is missing, the decision rule, and the route. A route
through `execute_script` (skill `ue-python`) names `unreal.*` calls checked
against Epic's Python API reference for UE 5.6 and the UE 5.8.4 headers;
**none of these sketches has been run against an editor.** Run one only when
the user agrees, read its output, and verify the effect with the `material`
tool or `capture_viewport` afterwards, as for any other change.

## Decision rule

| The user wants | Route |
|---|---|
| a look that the master's parameters can express | the `material` tool: a new instance or new values (section `workflows`) |
| a parameter the master does not have, a new node, a new shading behaviour | a change to the master material's graph: **ask the user**; Python only on request (below) |
| a static switch flipped on an instance | Python (below), or the user in the Material Instance Editor |
| a static mesh asset's default material | Python `StaticMesh.set_material` (below), or per actor with `set_actor_material` |
| a material that changes during play (hit flash, dissolve) | a Material Instance Dynamic, created by gameplay code or a Blueprint at run time: skill `ue-blueprints`, not this tool |
| one value shared by many materials (wind, time of day, wetness) | a Material Parameter Collection (below) |

When a request needs a graph change, say so before you start creating
instances: instances cannot add what the master lacks.

## Master materials and the node graph

The tool creates no `Material`, reads no graph and connects no nodes. A
master material is authored in the Material Editor. `unreal.MaterialEditingLibrary`
can author one from Python —
`create_material_expression(material, expression_class, node_pos_x=0, node_pos_y=0)`,
`connect_material_property(from_expression, from_output_name, property)`,
`recompile_material(material)` — but building a graph that way is long,
untested code: do it only when the user asks for it, and keep each script
small enough to check by `get_material_info` and a capture.

## Listing a parent's parameters

`get_material_info` lists only an instance's overrides. Python can list what a
material or instance exposes (not exercised):

```python
import unreal
mat = unreal.EditorAssetLibrary.load_asset("/Game/Materials/M_Rock")
mel = unreal.MaterialEditingLibrary
print("scalar", mel.get_scalar_parameter_names(mat))
print("vector", mel.get_vector_parameter_names(mat))
print("texture", mel.get_texture_parameter_names(mat))
print("switch", mel.get_static_switch_parameter_names(mat))
```

Each returns an array of names (Python API 5.6:
`get_scalar_parameter_names(material) -> Array[Name]`, likewise the others).
The toolkit's material tool has an unused helper that would list the same
three kinds (`GetMaterialParameters`, `MCPTool_Material.cpp`), but no
operation calls it.

## Static switch parameters

Static switches pick shader permutations; the tool cannot set them, and
`get_material_info` does not report them. Changing one recompiles shaders for
that instance. Python (not exercised):

```python
mel.set_material_instance_static_switch_parameter_value(mi, "UseWetness", True)
mel.update_material_instance(mi)
unreal.EditorAssetLibrary.save_loaded_asset(mi)
```

`set_material_instance_static_switch_parameter_value(instance, parameter_name, value, association=GLOBAL_PARAMETER, update_material_instance=True) -> bool`
(Python API 5.6). Static component mask parameters have no setter in this
library.

## Other parameter kinds

Double-vector, runtime virtual texture, sparse volume texture, font and
texture-collection parameters, material layers and the instance's base
property overrides (blend mode, two-sided, opacity mask clip value) cannot be
set by the tool. Some have `MaterialEditingLibrary` setters in the UE 5.8.4
headers; they are not covered here. Tell the user.

## Changing an instance's parent

No operation. Create a new instance of the other parent with the same
parameters (section `workflows`), and reassign it. Python's
`MaterialEditingLibrary.set_material_instance_parent` exists in the UE 5.8.4
header; not verified for 5.6/5.7 here.

## Substrate

Substrate is a project setting, `r.Substrate` (read-only at runtime, engine
default 0, UE 5.8.4 `RenderUtils.cpp`). To tell whether a project uses it,
read `Config/DefaultEngine.ini` (`read_file`): `r.Substrate=True` under
`[/Script/Engine.RendererSettings]` means on; no such line means off. The
UE 5.8.4 new-project wizard writes `r.Substrate=True` and
`r.Substrate.ProjectGBufferFormat=0` (`GameProjectUtils.cpp`,
`AddSubstrateConfigValues`, lines 199-211); projects created before that, or
converted ones, keep it off unless someone turned it on. Epic's 5.7 release
notes call Substrate production-ready and enabled by default for new
projects (second-hand: read through search summaries, not the page). It changes how master materials are authored
(Substrate slabs and operators instead of fixed shading models), which is
graph work: outside this tool. Instances of a Substrate master are still
Material Instance Constants with scalar, vector and texture parameters, so
the tool's operations apply unchanged. Whether a project uses Substrate is a
project setting; how legacy materials behave in a Substrate project has not
been verified here. Do not convert materials on your own initiative.

## Nanite meshes

Per Epic's Nanite documentation, Nanite supports materials with the Opaque
and Masked blend modes; for an unsupported material, a default material is
assigned and a warning goes to the output log. World Position Offset is
supported with limits. The tool does not check any of this: it assigns a
translucent material to a Nanite mesh without an error. After assigning to a
Nanite mesh, read `get_output_log` for that warning and capture the result.
Which meshes are Nanite, and a mesh's Nanite fallback or override material,
are mesh settings, not reachable with this tool.

## Material Parameter Collections

A collection is a separate asset whose values every referencing material
reads; per Epic's documentation a material can reference at most two
collections, and a collection holds up to 1024 scalar and 1024 vector
parameters. Changing the number of parameters recompiles every material that
references it. The tool can neither create a collection nor set its values.
Default values are edited in the collection asset (the user, or
`asset set_asset_property` on the collection if its properties are reachable
there; not verified, skill `ue-assets`); values during play are set by
gameplay code or Blueprints.

## Static mesh assets

`set_skeletal_mesh_material` covers skeletal mesh assets only, and
`asset set_asset_property` cannot reach a static mesh's material list (an
array of structs; skill `ue-assets`). Per placed actor, `set_actor_material`
works. For the asset itself, Python (not exercised):

```python
sm = unreal.EditorAssetLibrary.load_asset("/Game/Meshes/SM_Rock")
idx = sm.get_material_index("Rock")          # slot name -> index
sm.set_material(idx, unreal.EditorAssetLibrary.load_asset("/Game/Materials/MI_Rock_Wet"))
unreal.EditorAssetLibrary.save_loaded_asset(sm)
```

(`StaticMesh.set_material(material_index, new_material)` and
`get_material_index(material_slot_name) -> int32`, Python API 5.6.)

## A component other than the first

`set_actor_material` always takes the actor's first mesh component. For
another component: `set_property` on the actor (skill `ue-level-actors`,
section `set-property`) with a path into that component's `OverrideMaterials`
array may reach it; not verified. Otherwise tell the user.
