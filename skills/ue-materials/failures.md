# UE materials: when a call fails

Details of each operation are in sections `instances`, `parameters` and
`slots`.

## Errors

| Error | Cause | Fix |
|---|---|---|
| `Missing required parameter: <name>` | a required parameter is absent, an empty string, or (for `parameters`) not an object | send it with the right JSON type |
| `Cannot access engine or script Blueprints` | a path under `/Engine/` or `/Script/` | use or create a material under `/Game/` |
| other messages naming a "Blueprint path" | the path check: `..`, a forbidden character, over 512 characters | fix the path (skill `ue-blueprints`, "Paths") |
| `Failed to load parent material: <path>` / `Failed to load material: <path>` | no material at that path | find it with `asset_search` (skill `ue-assets`) |
| `Failed to load material instance: <path>` | not a Material Instance Constant (a base material, say), or nothing at that path | target an instance; for a base material create one |
| `Failed to load texture: <path>` | a texture in `parameters` did not load; the other entries landed | resend only that texture with a fixed path, then save |
| `Failed to create package: <path>` / `Failed to create material instance` | the engine refused the package or object | check `asset_name` for odd characters; tell the user |
| `Material instance created but failed to save: <path>` | the instance exists in memory but not on disk | `asset save_asset`; do not create it again |
| `Failed to load skeletal mesh: <path>` | no skeletal mesh at that path | `asset_search` with `class_filter: "SkeletalMesh"` |
| `material_slot must be >= 0` | a negative slot | send the index from the slot list |
| `Material slot <n> out of range. …` | past the last slot; the message states the count | pick an index below that count |
| `Actor not found: <name>. Use level_query find …` | no actor with that name or label in the editor's level | `level_query` `find` or `get_level_actors` |
| `Actor name contains invalid character: '<c>'` | the label has one of `` <>|&;`$(){}[]!*?~ `` | use the actor's object name |
| `Actor '<name>' has no mesh component` | the actor has no mesh to take a material | pick the actor that carries the mesh |
| `No active world. Open a level in the editor first` | no level open | open one (skill `ue-levels`) |
| `Unknown operation: '<op>'. Valid: …` | a misspelt operation | use a name from the operation table |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| the editor crashed during `create_material_instance` | probably an asset of another class already had that name (section `instances`) | restart the editor; search before creating; pick a free name |
| an instance you had is reset to its parent's look | `create_material_instance` with its name rebuilt it | put the values back from your earlier `get_material_info`, or from source control |
| a value you set is not in `get_material_info` | its JSON type was wrong and it was skipped, or a texture failed during `create_material_instance` | resend with the types in section `parameters` |
| a value is in `get_material_info`, the material looks unchanged | most likely no parameter of that name in the master (a wrong name is stored silently); or shaders are still compiling | check the name against the master; capture again after a few seconds |
| a static switch or another parameter kind is needed | the tool sets scalars, vectors and textures only | section `limits` |
| a vector colour is wrong in one channel | `r`, `g` or `b` was missing; it has no default | send all four components |
| a texture parameter looks wrong (flat, too bright, a default texture) | the texture does not match what the parameter samples: normal vs colour, 2D vs cube, virtual vs not | pick a matching texture (section `parameters`) |
| the change landed in slot 0 instead of the slot you meant | `material_slot` was not a JSON number | resend with a number; put slot 0 back from `old_material` |
| `set_actor_material` changed the wrong component | the actor has several mesh components; the first was taken | put `old_material` back; section `limits` |
| every copy of a character changed | `set_skeletal_mesh_material` changes the asset | put `old_material` back; use `set_actor_material` for one actor |
| a translucent material on a Nanite mesh shows a default material | Nanite supports opaque and masked materials only | `get_output_log` for the warning; tell the user (section `limits`) |
| a change is gone after an editor restart | the asset or level was never saved | `asset save_asset` or `save_all` after each change |
| a change made in the editor does not show in a running PIE session | PIE runs on a copy of the level | restart PIE |
