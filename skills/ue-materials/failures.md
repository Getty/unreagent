# UE materials: when a call fails

"The parameters object" is a heading of the skill itself.

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
