# Level actors: worked sequences

Each step names the tool and the skill that owns it. The calls are built from
the toolkit's source; none has been run against an editor. Opening, creating
and saving the level itself: skill `ue-levels`, section `workflows`.

## The verification loop

After every batch of writes, before you report success:

1. **Read back what you wrote.** `get_level_actors` with `name_filter` or
   `class_filter` and `brief: false` for transforms; `level_query info`
   (skill `ue-levels`) for components and collision. The success message of a
   write says what was asked, not what happened (`spawn_actor` returns the
   requested location; `delete_actors` the collected names).
2. **Look at it** when placement or appearance matters: a viewport capture
   (skill `ue-viewport`). Values written by `set_property` can be stored but
   not yet rendered (section `set-property`).
3. **Check the log** for warnings the call did not return: `get_output_log`
   (skill `ue-pie-testing`).
4. **Play it** when behaviour matters: Play In Editor (skill
   `ue-pie-testing`). The actor tools never touch the PIE world, so check
   there with that skill's tools.
5. **Save**, then reopen the level (skill `ue-levels`) when the change must
   survive: what comes back after a reload is what is on disk.

## Populate an empty level

Starting from a saved, empty level (skill `ue-levels`, section `workflows`):

1. **Floor.** `spawn_actor` `{"class": "StaticMeshActor", "location": {"x": 0,
   "y": 0, "z": 0}, "scale": {"x": 20, "y": 20, "z": 1}}`. Then
   `set_property` `{"actor_name": "<actorName>", "property":
   "StaticMeshComponent.StaticMesh", "value": "/Engine/BasicShapes/Plane.Plane"}`.
   The engine's basic plane is 100 × 100 cm, so scale 20 gives 20 × 20 m
   (engine content, unverified for your engine build: check with
   `asset_search` under `/Engine/BasicShapes`). If the mesh does not show,
   save and reopen the level (section `set-property`, first part).
2. **Light.** The `lighting` tool (skill `ue-lighting`) spawns the standard
   lights and sets sky and fog; or `spawn_actor` `DirectionalLight` /
   `SkyLight`, then `set_property` on `LightComponent.Intensity`. A level from
   a template usually has lights already: `level_query find` `Light` first.
3. **Player start.** `spawn_actor` `{"class": "PlayerStart", "location": {"x":
   0, "y": 0, "z": 100}}`. Keep it above the floor: the capsule must not start
   inside geometry. The game mode decides what spawns there (skill
   `ue-game-framework`).
4. **A Blueprint actor.** Find its path: `asset_search` with `class_filter:
   "Blueprint"` and a `name_pattern` (skill `ue-assets`). `spawn_actor` with
   `/Game/…/BP_Door.BP_Door` (the tool appends `_C`). A Blueprint that does
   not compile may spawn with stale or missing logic; compile first (skill
   `ue-blueprints`).
5. **Navigation**, when AI must walk: `spawn_actor`
   `{"class": "/Script/NavigationSystem.NavMeshBoundsVolume", "scale": {…}}`
   sized over the floor. A volume spawned this way may have no brush shape,
   which the editor's own placement gives it; whether the navmesh then builds
   is unverified. Check with `level_query list`, whose `filtered_categories`
   counts a `RecastNavMesh` once navigation exists, and test pathing in PIE.
   If it does not build, place the volume through `execute_script` (skill
   `ue-python`) or by hand.
6. Verification loop above, then save.

## Grid and bulk placement

There is no batch spawn: one `spawn_actor` per actor. Plan the positions
first, then issue the calls one after another and keep every returned
`actorName`. A 5 × 5 grid at 300 cm spacing is `x = 300·i`, `y = 300·j`.

- Pass `name` only after checking it is free (a taken name crashes the
  editor; main skill, `spawn_actor`), or leave names to the engine and track the returned `actorName`s.
- Spawns that collide are moved by the engine: read the positions back with
  `get_level_actors` `class_filter`, `brief: false`, and paging (`limit`
  1000, `nextOffset`).
- Dozens of calls through the task queue are slow; for hundreds, a script
  through `execute_script` (skill `ue-python`) is the better tool.

## Rearrange and organise

- Move with `move_actor`; `relative: true` for nudges (`{"location": {"z":
  50}, "relative": true}` raises by 50 cm; `{"rotation": {"yaw": 90},
  "relative": true}` turns a quarter).
- Tags: `set_property` `Tags` with the **whole** new array (read the old one
  first with `get_level_actors brief: false`).
- Labels and Outliner folders: no tool here sets them. `ActorLabel` is an
  editor property that `set_property` could reach, but a raw write skips the
  rename the editor does (unverified, do not rely on it). Use
  `execute_script` (skill `ue-python`) or leave them.

## Safe bulk delete

1. `get_level_actors` `{"class_filter": "StaticMeshActor", "include_hidden":
   true, "limit": 1000}`: exactly what `class_filter` will hit.
2. If that list is not exactly what you mean, delete by `actor_names` with the
   object names from step 1.
3. `delete_actors`. Compare `deleted` with step 1, then run step 1 again: it
   should come back empty (or without the deleted names).

Never delete by a short filter (`Light`, `Actor`, `Volume`): it reaches
engine actors the level needs (sky light, physics volume).

## Repairing a bad edit

There is no undo. In order of preference:

1. **Write the old value back.** Before any `set_property` or `move_actor` on
   something that matters, read the old value (`get_level_actors brief:
   false`, `level_query info`) and keep it; restoring is one more call.
2. **Drop unsaved changes.** If the level was saved before the edit, every
   change since is only in memory. `open_level` of the same level would hit
   the save prompt (skill `ue-levels`); restart the editor instead
   (skill `unreagent-iterate`), and the saved state loads.
3. **Re-create.** A deleted actor comes back only by spawning it again with the
   transform and properties you recorded.

Save only once the verification loop is clean.
