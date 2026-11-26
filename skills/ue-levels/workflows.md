# Levels: worked sequences

Built from the toolkit's source; none has been run against an editor. Actor
placement details: skill `ue-level-actors`, section `workflows`.

## Create a level and give it a home

1. `asset` `{"operation": "save_all"}` (skill `ue-assets`), so the current
   level has nothing left to save and no prompt can open.
2. `asset_search` `{"class_filter": "World", "path_filter": "/Game/Maps/",
   "name_pattern": "Arena"}`: make sure the target path is free. `save_as`
   deletes whatever map is there first.
3. `open_level` `{"action": "list_templates"}` if you want a template; else
   skip.
4. `open_level` `{"action": "new", "save_current": false}`, or with
   `"template": "<name exactly as listed>"`.
5. `open_level` `{"action": "save_as", "save_path": "/Game/Maps/Arena"}`.
   A never-saved level is renamed there; `mapName` is now `Arena`.
6. Build (below), then `asset` `save_all`.

## Populate and verify

1. `level_query` `{"operation": "list"}`: what the template brought
   (`filtered_categories` shows sky, fog and lights it left out of
   `by_class`; `find` `Light` lists the lights).
2. Floor, lights, PlayerStart, Blueprint actors, navigation: skill
   `ue-level-actors`, section `workflows`, "Populate an empty level". Lights,
   sky and fog: skill `ue-lighting`.
3. What spawns at play: the level's game mode override (skill
   `ue-game-framework`).
4. Verify:
   - `level_query list` again: every actor you placed under its class, at its
     location;
   - `level_query info` on anything with components or collision that matters;
   - a viewport capture (skill `ue-viewport`) for appearance;
   - Play In Editor and `get_output_log` (skill `ue-pie-testing`) for
     behaviour and warnings.
5. `asset save_all`. For a change that must last, prove it: `open` another
   level, then this one again (nothing is dirty, so no prompt), and query.

## Switch to another level

1. `asset` `{"operation": "save_all"}`.
2. `open_level` `{"action": "open", "level_path": "/Game/Maps/Arena"}`.
3. `level_query` `{"operation": "list"}`. In a World Partition level, expect
   only the loaded part (section `level-types`).

## Inspect an actor before changing it

1. `level_query` `{"operation": "find", "pattern": "door"}`.
2. `level_query` `{"operation": "info", "actor_label": "<label from step 1>"}`.
3. Keep the `transform` and the values you will change; use the `name` with
   `move_actor` or `set_property` (skill `ue-level-actors`).

## Repair after a bad edit

Nothing in these tools has undo.

- **Unsaved and unwanted:** restart the editor (skill `unreagent-iterate`);
  the last saved state loads. `open` of the same level would raise the save
  prompt instead.
- **Saved already:** restore the values you recorded before the edit (skill
  `ue-level-actors`, "Repairing a bad edit"), or the file from version
  control, which is the user's call.
- **A template edited in place:** an engine template cannot be saved through
  `save_all` (it skips `/Engine/`); restarting the editor drops the change.

## Copy a level

`open` the source (after `save_all`), then `save_as` the new path. A level
already on disk is duplicated, and the editor stays on the original
(`mapName` is the old name): `open` the copy before you edit it. For a One
File Per Actor level, check the copy after opening it (section `level-types`).
`asset duplicate` (skill `ue-assets`) is the other route; which one keeps
external actors correct is unverified.
