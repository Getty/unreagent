# Levels: World Partition, One File Per Actor, templates, streaming

The main skill has the tools. This section has the engine facts that decide
what those tools can see and what a save writes. Sources are Epic's
documentation, linked; the rest is marked unverified.

## Two kinds of level

| | Classic level | World Partition level |
|---|---|---|
| Actors in the editor | all loaded when the level opens | only actors with *Is Spatially Loaded* off, plus the regions someone loaded; the map is "initially unloaded" |
| Where actor data lives | usually inside the `.umap` | One File Per Actor: each actor in its own file |
| Streaming at play | sublevels, by hand or Blueprint | automatic grid cells (when streaming is on) |

Source: [World Partition](https://dev.epicgames.com/documentation/en-us/unreal-engine/world-partition-in-unreal-engine).
The **Open World** template has World Partition, One File Per Actor, Data
Layers and HLODs on. The game templates (Blank, First Person, Third Person, Top
Down, Vehicle) use World Partition with *Enable Streaming* off; whether every
actor is then loaded in the editor is unverified, though that is what turning
streaming off is for.

**What this means for the tools.** `level_query`, `get_level_actors`,
`move_actor`, `set_property` and `delete_actors` walk the editor world's
loaded actors. In a World Partition level with streaming on:

- an actor in an unloaded region is not listed, not found by name, and not
  deleted by a `class_filter`;
- `total_actors` and `gameplay_actors` count the loaded part only.

No toolkit tool loads a region. Loading one is an editor action (World
Partition window, drag a box, *Load Region from Selection*): ask the user, or
look for a route through `execute_script` (skill `ue-python`; unverified).

**Telling them apart.** No tool reports it directly. Signs, each unverified as
a rule: `level_query list` counting `WorldPartition` or `WorldDataLayers` under
`filtered_categories`; an `__ExternalActors__` folder for the map under the
project's `Content` directory (the launcher's `list_dir`, skill
`unreagent-files`). When it matters, ask the user.

## One File Per Actor (OFPA)

"OFPA reduces overlap between users by saving data for instances of Actors in
external files, removing the need to save the main Level file when making
changes to its Actors." It is on by default with World Partition and opt-in
per level otherwise (the level's *Use External Actors* setting). It is an
editor feature: a cooked game embeds the actors in the level again.
Source: [One File Per Actor](https://dev.epicgames.com/documentation/en-us/unreal-engine/one-file-per-actor-in-unreal-engine).

For the agent:

- **A save writes actor files.** `asset save_all` saves every dirty package,
  which in an OFPA level should include the changed actors' own packages.
  `move_actor` and `set_property` mark the actor's package dirty; `spawn_actor`
  and `delete_actors` mark only the level's. Whether a spawn or delete
  reaches disk through `save_all` is unverified: after saving, reopen the
  level (save first, then `open`) and query again.
- **Source control.** Each actor is a file; a change set touches many small
  files under `__ExternalActors__`, not the `.umap`. Do not hand-edit or
  delete those files.
- `save_as` of a World Partition level loads all its actors first (engine
  source, `FileHelpers.cpp`); what happens to the external actor files under
  the new path is unverified. Prefer building in a level that already has its
  final path.

## Templates

`list_templates` lists what the editor offers (`name`, `mapPath`); the set
depends on engine version and enabled plugins, so always list before you pick.
`new` with a template **opens the template's own map**: `save_as` to `/Game/`
before any change. Whether `new` without a template produces a World
Partition level is unverified; check as above.

## Sublevels, streaming, Level Instances, Data Layers

No toolkit tool creates sublevels, changes streaming, or edits Level Instances
or Data Layers. `level_query` filters `LevelInstance`, `PackedLevelActor` and
`DataLayer` classes out entirely (section `filtered-classes`);
`get_level_actors` shows them. `open_level` opens a persistent level only.
For these, `execute_script` (skill `ue-python`) or the user.

## Engine versions

The source of `open_level`, `level_query` and the actor tools has no
engine-version branches: the same calls
(`UEditorLoadingAndSavingUtils::LoadMap` / `NewBlankMap`,
`FEditorFileUtils::SaveMap`, the editor's template list) on every version the
plugin builds for. Version differences in World Partition behaviour itself are
not covered here (unverified).
