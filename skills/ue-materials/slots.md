# UE materials: the two slot operations

Saving is under "Saving" in the skill itself; the end-to-end steps are in
section `workflows`.

## material_slot

Both operations take `material_slot`, default 0, the 0-based slot index.

Through the bridge it is listed as a string, because the tool declares it
`integer`, a type the bridge does not map. **Send a JSON number.** The tool
reads it with the engine's `FJsonObject::GetIntegerField`, which cannot report
a failure:

- a string made of an optional sign, digits and at most one dot is read as
  that number and truncated; `true` and `false` read as 1 and 0, so `true`
  goes to slot 1;
- **any other value, such as `"one"`, `null`, an array or an object, reads as
  0: the call goes to slot 0 without an error.** A lone sign or dot, such as
  `"-"` or `"."`, also reads as 0
  (`FJsonObject::GetField`, `Engine/Source/Runtime/Json/Private/Dom/JsonObject.cpp`;
  `FJsonValue::AsNumber` in `JsonValue.cpp` beside it; `TCString::IsNumeric`,
  `Engine/Source/Runtime/Core/Public/Misc/CString.h`; UE 5.8.4, read, not
  run).

`slot` in the result is the slot that was written: compare it with the one
you meant.

Slots are addressed by index only, not by slot name. Read the index first.

## set_skeletal_mesh_material

Puts `material_path` into slot `material_slot` of the skeletal mesh asset.
**This changes the asset, and so every use of that mesh**: every placed
character, every Blueprint that uses it, unless they override the slot.

- `asset get_asset_info` on the skeletal mesh lists its slots with `slot`,
  `slot_name` and `material` (skill `ue-assets`). Pick the index by
  `slot_name`.
- Order of checks: both paths present and path-checked, `material_slot >= 0`,
  the mesh loads, the material loads, the slot is in range.
- Errors: `material_slot must be >= 0`,
  `Failed to load skeletal mesh: <path>`, `Failed to load material: <path>`,
  `Material slot <n> out of range. Skeletal mesh has <m> material slots.`
- The result has `skeletal_mesh` (your path), `slot`, `old_material` and
  `new_material` (object names; `None` for an empty slot). The message is
  `Set material slot <n> on <mesh> to <material>`.
- The tool runs the mesh's change notification and marks it dirty: save it
  with `asset save_asset` `{"asset_path": "<mesh path>"}`.
- Keep `old_material` from the result: it is the only record of what to put
  back.

## set_actor_material

Puts `material_path` into slot `material_slot` of one actor in the level open
in the editor. Only that actor's component changes, not the mesh asset: the
engine's `UMeshComponent::SetMaterial` writes the component's override list
(`OverrideMaterials`, `Engine/Source/Runtime/Engine/Private/Components/MeshComponent.cpp`,
UE 5.8.4), which belongs to the placed actor.

- **`actor_name`** is the actor's object name or its label; the first actor in
  the level with either one is taken. The name passes the toolkit's
  actor-name check, which rejects a label such as `Crate (2)` (skill
  `ue-level-actors` covers addressing actors; use the object name then). An
  actor that is not found gives
  `Actor not found: <name>. Use level_query find to search for actors in the current level`.
- **The first mesh component of the actor is used** — the first component
  that is a static mesh, skeletal mesh or other mesh component. There is no
  parameter to pick another one. The result's `component` and
  `component_type` say which one it was. An actor with several mesh
  components (a Blueprint with a body and a weapon, say) may get the change
  on the wrong one: check `component`, and if it is wrong, put `old_material`
  back and tell the user.
- An actor without a mesh component fails with
  `Actor '<name>' has no mesh component`. A Niagara component or a light is
  not a mesh component.
- The slot count is the component's (`GetNumMaterials`), which for a static
  or skeletal mesh component follows its mesh.
- Order of checks: actor name, `material_path` present and path-checked,
  `material_slot >= 0`, editor and level, the actor, its mesh component, the
  slot in range, then the material loads.
- Other errors: `material_slot must be >= 0`,
  `Material slot <n> out of range. Mesh component has <m> material slots.`,
  `Failed to load material: <path>`,
  `No active world. Open a level in the editor first`,
  `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?`.
- The result has `actor` (the object name), `component`, `component_type`,
  `slot`, `old_material` and `new_material`. The message is
  `Set material slot <n> on actor '<name>' (<component class>) to <material>`.
- The actor and the level are marked dirty: save with `asset save_all`.
- The level is the editor's level, not a running PIE session: the change
  shows in PIE only after PIE is started again.

## Which one

| You want | Use |
|---|---|
| every use of a character mesh to change | `set_skeletal_mesh_material`, then save the mesh |
| one placed actor to look different | `set_actor_material`, then `save_all` |
| a static mesh asset's own default material | neither: section `limits` |
| a component other than the actor's first mesh component | neither: section `limits` |
| a material that changes during play | neither: section `limits` (dynamic instances) |
