# UE materials: the two slot operations

How `material_slot` is read, and what a value that is not a JSON number does,
is in the skill itself, above "What the tool cannot do". Saving is under
"Saving" there.

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
