# lighting: when a call fails

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| success, and the level looks the same | the key was not applied, or the value changes little | compare `properties_set` or `settings_applied` with what you sent; check the key in section `properties` |
| `Set 0 properties on '<name>'` | no key matched | the same |
| a colour came out black or nearly black | a byte colour (`ground_albedo`, `volumetric_fog_albedo`) was given as 0–1 | pass 0–255 for those two keys |
| a colour came out far too bright | a 0–1 colour key was given 0–255 | section `properties` says which scale each colour key takes |
| a volume spawned with `extent` and without `infinite` shows no effect, or not where expected | `extent` is ignored, and the code gives the volume no size; its bounds are unverified | `infinite: true`; check `is_infinite` in the result |
| `"name": "Sun"` on a spawn named nothing | `name` is an alias of `actor_name`, which the spawns do not read | pass `label` |
| `Actor not found: <name>` | no actor with that label or object name | `list_lights` |
| `Properties object is required` / `Settings object is required` | the object is missing or not a JSON object; note `settings` for post-process, `properties` for the rest | fix the parameter name |
| `Failed to load cubemap: <path>` | not a loadable cube texture; earlier keys of the call were applied | find the path with `asset_search` (skill `ue-assets`), inspect, send again |
| `Internal error: null result from LightingEditor` | the helper returned nothing | not expected from the code as read; report it |
| the change is gone after an editor restart | nothing was saved | save the level (skill `ue-levels`) |
| `Error: Task timed out after …ms (task_id: …)` | the bridge stopped waiting | skill `ue-tasks`; read the state back before you retry |

## Which actor each set operation accepts

Each operation accepts only its own kind of actor:

| Operation | Accepts | Otherwise |
|---|---|---|
| `set_light_properties` | point, spot, directional, rect light | `Actor '<name>' (class: <class>) is not a standard light type (point/spot/directional/rect)` |
| `set_sky_light` | a sky light actor | `Actor '<name>' is not a SkyLight (class: <class>)` |
| `set_sky_atmosphere` | any actor with a sky atmosphere component | `Actor '<name>' has no SkyAtmosphereComponent (class: <class>)` |
| `set_fog` | an exponential height fog actor | `Actor '<name>' is not an ExponentialHeightFog (class: <class>)` |
