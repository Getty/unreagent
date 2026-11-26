# UE materials: workflows

Step lists with the exact calls. Tool names are the bare toolkit names; the
bridge lists them as `unreal_<name>`. Every result's fields sit under the task
envelope's `data` key (skill `ue-tasks`). None of these sequences has been run
against an editor; each step names the check that tells you it worked.

## 1. Find out what you are working with

1. The master material: `asset_search`
   `{"class_filter": "Material", "path_filter": "/Game"}` (skill `ue-assets`).
   Existing instances: the same with `"class_filter": "MaterialInstanceConstant"`.
2. For an instance the user names: `material` `get_material_info`
   `{"asset_path": "/Game/Materials/MI_Rock"}`. Follow `parent` with further
   `get_material_info` calls until `is_instance` is `false`: that is the
   master material.
3. **Get the parameter names.** The tool cannot list what the master
   exposes. In this order:
   - names already overridden on an existing instance of the same master
     (`scalar_parameters`, … in step 2) are real names;
   - the user, or the project's documentation;
   - the Python route in section `limits` ("Listing a parent's
     parameters"), not exercised against an editor.
   Never guess names from conventions such as `BaseColor`: a wrong name is
   stored without an error and changes nothing.

## 2. Instances of a master material, with parameters

1. Check the target name is free: `asset_search`
   `{"path_filter": "/Game/Materials", "name_pattern": "MI_Rock_Wet"}`; the
   name is taken only if an entry has exactly that `name` and `package_path`
   `/Game/Materials`. Taken: pick another name, or ask before you replace it
   (section `instances`: replacing can crash the editor if the class differs).
2. `material` `create_material_instance`
   `{"asset_name": "MI_Rock_Wet", "parent_material": "/Game/Materials/M_Rock", "package_path": "/Game/Materials/", "parameters": {"scalars": {"Roughness": 0.15, "Wetness": 1.0}, "vectors": {"Tint": {"r": 0.6, "g": 0.6, "b": 0.65, "a": 1}}, "textures": {"Normal": "/Game/Textures/T_Rock_Wet_N"}}}`.
   Check: `saved: true`.
3. `material` `get_material_info` `{"asset_path": "/Game/Materials/MI_Rock_Wet"}`.
   Check: `parent` is the master's object path; every name you sent is in
   the matching `*_parameters` object with your value. A missing texture
   failed to load (see the editor log); a missing scalar or vector had the
   wrong JSON type (section `parameters`).
4. Fix what is missing with `set_material_parameters`
   `{"material_instance_path": "/Game/Materials/MI_Rock_Wet", "parameters": {…only the missing entries…}}`,
   then `asset save_asset` `{"asset_path": "/Game/Materials/MI_Rock_Wet"}`.

## 3. A set of variants

Create one instance per variant from the same master, each with only the
parameters that differ (step 2 per variant). Prefer instances of one master
over instances of instances: a chain inherits every change made higher up,
which is wanted for a deliberate hierarchy (a base "rock" instance with
per-biome children) and a surprise otherwise.

To tune an existing instance: `get_material_info` first and keep the old
values from the result, so you can put them back with
`set_material_parameters`; there is no undo.

## 4. Assign to a skeletal mesh (every use of the mesh)

1. `asset` `get_asset_info` `{"asset_path": "/Game/Characters/SK_Hero"}`:
   read the slots (`slot`, `slot_name`, `material`) and pick the index by
   `slot_name` (skill `ue-assets`).
2. `material` `set_skeletal_mesh_material`
   `{"skeletal_mesh_path": "/Game/Characters/SK_Hero", "material_path": "/Game/Materials/MI_Hero_Red", "material_slot": 1}`.
   Check: `slot` is 1; note `old_material`.
3. `asset` `save_asset` `{"asset_path": "/Game/Characters/SK_Hero"}`.
4. Verify (section 6, asset preview).

## 5. Assign to a placed actor (this actor only)

1. Find the actor: `get_level_actors` or `level_query` `find` (skills
   `ue-level-actors`, `ue-levels`). Use the object `name` when the label
   contains `(`, `)` or another character the actor-name check refuses.
2. Know its slots: the slot count is the first mesh component's. A skeletal
   mesh's slots are listed by `asset get_asset_info` on the mesh; a static
   mesh's are not (only skeletal meshes add `materials[]`). Without a list,
   which index is which part is the user's to say; a slot past the end fails
   with an `out of range` message that states the count.
3. `material` `set_actor_material`
   `{"actor_name": "SM_Rock_12", "material_path": "/Game/Materials/MI_Rock_Wet", "material_slot": 0}`.
   Check: `component` is the component you meant (an actor with several mesh
   components gets the first one only); note `old_material`.
4. `asset` `save_all`.
5. Verify (section 6, level viewport).

## 6. Verify what you changed

The data check is `get_material_info` (values) and the slot result
(`new_material`). To see it, use `capture_viewport` (skill `ue-viewport`,
which also explains how to view the JPEG it returns):

- **Placed actor**: frame the actor in the level viewport.
  `capture_viewport`
  `{"camera_location": {"x": <actor x - 300>, "y": <actor y>, "z": <actor z + 100>}, "camera_rotation": {"pitch": -15, "yaw": 0, "roll": 0}}`.
  Give all three keys of each object; a missing key becomes 0. The actor's
  location comes from `get_level_actors`.
- **Skeletal mesh**: the asset editor preview,
  `capture_viewport` `{"asset_path": "/Game/Characters/SK_Hero.SK_Hero", "camera": "front"}`
  (object path; the preset positions are fixed, not fitted to the mesh).
- **The instance itself**: a capture of the Material Instance Editor has not
  been verified to work (skill `ue-viewport`, section `asset-preview`); look
  at an actor that uses it instead.
- Shaders compile after a new parameter combination or a first use. A grey or
  checkered surface right after the change can be compilation in progress:
  capture again after a few seconds before you conclude anything (not
  verified how long).
- `get_output_log` (skill `ue-pie-testing`) for texture or shader warnings
  after the change.

Compare before and after: capture once before the change with the same
camera, so the difference is visible.

## 7. Undo by hand

No operation undoes. Put values back with `set_material_parameters` from the
values you read first, put `old_material` back with the same slot operation,
and save. An instance you created can be deleted with the `asset` tool if the
user wants it gone (skill `ue-assets`).
