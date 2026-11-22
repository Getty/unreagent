---
name: ue-levels
description: >
  Open, create and save levels in the editor, and survey what a level holds,
  through the UE LLM Toolkit's open_level and level_query tools. Use before
  switching or saving a level, to get an overview of a level's gameplay actors,
  or when either tool returns an error.
toolkit_tools: [open_level, level_query]
---

# Levels

Two tools of the UE LLM Toolkit plugin work on levels as a whole:

- `open_level` opens a level, creates one, saves the current level under a
  path, and lists the editor's level templates.
- `level_query` surveys the open level. It lists the gameplay actors by class,
  searches them, and dumps one actor in detail.

Placing and changing single actors is the subject of `ue-level-actors`.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

## Before the first call

- **Tool names.** The bridge lists them as `unreal_open_level` and
  `unreal_level_query`, and your client may add its own prefix. This skill uses
  the bare names, as the plugin's errors do.
- **Result shape.** Success is the message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope (`task_id`, `status`, `success`, `message`, …), and the tool's own
  fields, the ones named in this skill, sit under its `data` key. Failure is
  `Error: <message>` with no data. Skill `ue-tasks` covers the task tools.
- **The editor world.** Both tools act on the level open in the editor, never
  on a Play-In-Editor session. `level_query` fails with `No active world. Open
  a level in the editor first` when no level is open.
- **Timeouts.** After 300 s (`MCP_ASYNC_TIMEOUT_MS`) the bridge returns `Error:
  Task timed out after 300000ms (task_id: …)`. That ends the wait, not the
  work: a level can still finish loading afterwards. Follow the task up
  (`ue-tasks`) before you try again. Loading a large level can hold the editor
  for more than 30 s; run `open_level` as skill `ue-tasks`, "A 30 s timeout
  can crash the editor", describes.

## Level paths

`open_level` takes the level's **package path**, not a file path and not an
object path: `/Game/Maps/Arena`, without `.umap`. `asset_search` (skill
`ue-assets`) finds levels with `class_filter: "World"`. Its `path` field is the
object path `/Game/Maps/Arena.Arena`. The part before the dot is the package
path.

Each path goes through these checks, in order:

| Check | Error |
|---|---|
| not empty | `Level path cannot be empty` |
| at most 512 characters | `Level path exceeds maximum length of 512 characters` |
| not under `/Engine/` or `/Script/` | `Cannot open engine or script levels` |
| no `..` | `Level path cannot contain path traversal sequences` |
| none of `` < > \| & ; ` $ ( ) { } [ ] ! * ? ~ `` | `Level path contains invalid character: '<c>'` |
| starts with `/Game/` | `Level path must start with '/Game/' to reference project content` |

A level inside a plugin's content folder therefore cannot be opened or saved
through `open_level`.

## Unsaved changes and the save prompt

The tool's own description says: "The editor will prompt to save unsaved
changes before switching levels." By its parameter text, `open` always uses
the engine's built-in save prompt, and `new` prompts unless you pass
`save_current: false`. The code only hands that flag to the engine, so when
the engine prompts has not been verified.

A save prompt waits for a person to click. With nobody at the editor, nothing
answers it, so expect the call not to return before the bridge gives up. This
has not been verified against a running editor. Leave the prompt nothing to ask:

1. Save first: `asset` with `{"operation": "save_all"}` (skill `ue-assets`),
   or `save_as` for a level that has never been saved.
2. Then call `open`, or `new` with `save_current: false`.

No parameter discards unsaved changes without a prompt. Changes you do not want
have to be undone by hand, or dropped with an editor restart
(`unreagent-iterate`).

## open_level

`action` is required. It is lowercased and trimmed, so `Open` works too. An
unknown action gives `Unknown action: '<action>'. Use 'open', 'new',
'save_as', or 'list_templates'.`

### open

Opens a level and replaces the current one.

| Parameter | Meaning |
|---|---|
| `level_path` | required. Package path, such as `/Game/Maps/Arena` |

The result holds `action`, `levelPath`, `mapName` and `worldName`, and the
message is `Opened level: Arena`.

| Error | Meaning |
|---|---|
| `Level not found: '<path>'. Use asset_search to find available maps.` | no package exists at that path |
| `Could not resolve level path: '<path>'` | the path does not map to a file on disk |
| `Failed to load level: '<path>'` | the engine returned no world |

### new

Creates a new level and replaces the current one.

| Parameter | Default | Meaning |
|---|---|---|
| `template` | blank level | the name of a template from `list_templates` |
| `save_current` | `true` | by its parameter text, `true` prompts to save the current level first. Pass `false` after you have saved |

Without `template`, the result holds `action`, `template: "blank"` and
`mapName`, and the message is `Created new blank map: <name>`. The new level
has no package path yet: give it one with `save_as` before you rely on
`save_all`, which skips packages under `/Temp/`.

With `template`, the tool picks the first template whose name equals the string
(ignoring case) or whose map path contains it. Pass a `name` exactly as
`list_templates` returns it, because a short string can match more than one
template.

- **The template's own package is opened.** The code loads the template map
  itself, not an untitled copy. Before you change anything, `save_as` to a
  `/Game/` path. `save_all` skips `/Engine/` packages, so edits to an engine
  template are not saved in place.
- `save_current` is not used on this branch. Any prompt is the engine's own,
  as for `open`.

| Error | Meaning |
|---|---|
| `Template '<name>' not found. Use action 'list_templates' to see available templates.` | no template matched |
| `Could not resolve template path: '<path>'` | the template's map path did not map to a file |
| `Failed to create map from template: '<name>'` | the engine returned no world. Fall back to a blank level |
| `Failed to create new blank map.` | the engine returned no world for a blank level |
| `Editor engine not available.` | the editor's engine object is missing |

### save_as

Saves the level that is open in the editor under a package path.

| Parameter | Meaning |
|---|---|
| `save_path` | required. Package path, such as `/Game/Maps/Arena`, checked like any level path |

The tool hands the engine's map save the file name for that path. The result
holds `action`, `save_path`, `filename` (the file path on disk) and `mapName`,
and the message is `Saved level to: /Game/Maps/Arena`. Whether the editor then
goes on editing the level under the new path is not verified: compare
`mapName` with the name you saved under, and if they differ, `open` the saved
path before you go on editing, so that later saves land in the file you meant.

| Error | Meaning |
|---|---|
| `Editor not available` | no editor |
| `No world currently loaded` | no level is open |
| `Failed to save level to: '<path>'` | the engine's save failed |

### list_templates

Takes no parameters. The result holds `action`, `count` and `templates[]`, each
entry with `name` and `mapPath`. The message is `Found 4 available map
templates.`

## Saving a level

| Situation | Call |
|---|---|
| a level that has never been saved (after `new`) | `open_level` `save_as` with a `/Game/` path |
| a level that already has a `/Game/` path | `asset` `save_all` (skill `ue-assets`). It saves every dirty package without a dialog, the level and every changed asset included |
| a copy under a new path | `open_level` `save_as`, then check `mapName` |

## level_query

`operation` is required. It is lowercased, and these aliases are accepted:
`search` for `find`, and `get`, `get_info` and `inspect` for `info`. An
unknown operation gives `Unknown operation: '<op>'. Valid: list, find, info`,
with a "Did you mean" suggestion when one is close.

`level_query` is read-only. It works on **labels**, the names the World
Outliner shows, and leaves engine infrastructure out of `list` and `find`.

### What counts as infrastructure

The filter goes by the actor's class name:

| Rule | Classes |
|---|---|
| class name starts with | `Landscape`, `Foliage`, `InstancedFoliage`, `WorldPartition`, `WorldDataLayers`, `NavMesh`, `RecastNavMesh`, `AbstractNavData`, `NavigationData`, `HLOD`, `DataLayer`, `LevelInstance`, `PackedLevelActor` |
| class name is exactly | `WorldSettings`, `GameModeBase`, `GameMode`, `GameStateBase`, `GameState`, `PlayerState`, `GameSession`, `HUD`, `GameNetworkManager`, `AtmosphericFog`, `SkyAtmosphere`, `SkyLight`, `VolumetricCloud`, `ExponentialHeightFog`, `Note`, `GroupActor`, `LevelBounds`, `LevelScriptActor`, `DefaultPhysicsVolume`, `Brush`, `WorldPartitionReplay` |
| lights, filtered by `list` only | `DirectionalLight`, `PointLight`, `SpotLight`, `RectLight` |

A level-instance actor or a landscape never shows up here. Use
`get_level_actors` (skill `ue-level-actors`) for those.

### list

Takes no other parameters and has no paging. On a large level, the result is
large.

The result holds:

- `map_name`, `total_actors`, `gameplay_actors` and `filtered_count`;
- `by_class`, an object that maps each class name to an array of
  `{label, class, location}`;
- `filtered_categories`, counts of what the filter left out, lights under
  `Lights` among them.

The message is `37 gameplay actors (412 filtered)`.

### find

| Parameter | Meaning |
|---|---|
| `pattern` | required. Case-insensitive substring of the label or of the class name |

A missing pattern gives `'find' operation requires a 'pattern' parameter`.
Infrastructure is left out, but lights are included. The matches are sorted by
label.

The result holds `map_name`, `pattern`, `total_actors`, `match_count` and
`matches[]`. Each match carries `label`, `class`, `location`, `rotation`,
`scale` and, when the actor sits in an Outliner folder, `folder`.

### info

| Parameter | Meaning |
|---|---|
| `actor_label` | required. The actor's whole label, compared without regard to case. The object name does not work here |

A missing label gives `'info' operation requires an 'actor_label' parameter`.
The first actor with that label is used.

The result holds:

- `map_name`, `label`, `class` and `name`, the object name to use with the
  `ue-level-actors` tools;
- `blueprint_path`, for an actor of a Blueprint class;
- `transform`, with `location`, `rotation` and `scale`;
- `component_count` and `components[]`. Each component carries `name` and
  `class`, plus `is_primitive: true` for components that render or collide;
- `collision`: the collision setup of the actor's primitive components.

`Actor '<label>' not found` is all you get when nothing matches. The plugin
works out near-miss suggestions, but the tool returns only the error message
and leaves them out. Run `find` with part of the label instead.

### level_query or get_level_actors

| Need | Tool |
|---|---|
| an overview of the gameplay actors, grouped by class | `level_query` `list` |
| a search by label or class name, without engine noise | `level_query` `find` |
| components, collision and Blueprint origin of one actor | `level_query` `info` |
| every actor, infrastructure included, paged (hidden ones with `include_hidden: true`) | `get_level_actors` |
| object names, for addressing actors reliably | either: `info` returns `name`, and `get_level_actors` returns `name` for every actor |

## Typical sequences

**Create a level and give it a home**

1. `asset` `{"operation": "save_all"}`, so the current level has nothing left to
   save.
2. `open_level` `{"action": "new", "save_current": false}`.
3. `open_level` `{"action": "save_as", "save_path": "/Game/Maps/Arena"}`.
4. Build the level (skill `ue-level-actors`), then `asset` `save_all`.

**Switch to another level**

1. `asset` `{"operation": "save_all"}`.
2. `open_level` `{"action": "open", "level_path": "/Game/Maps/Arena"}`.
3. `level_query` `{"operation": "list"}` to see what is there.

**Inspect an actor before changing it**

1. `level_query` `{"operation": "find", "pattern": "door"}`.
2. `level_query` `{"operation": "info", "actor_label": "<label from step 1>"}`.
3. Use the `name` from step 2 with `move_actor` or `set_property`.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Level path must start with '/Game/' …` | a file path, an engine path or plugin content | pass the `/Game/` package path |
| `Level not found: '…'` | no package at that path | `asset_search` with `class_filter: "World"`. Use the part of `path` before the dot |
| an `open` or `new` call that never returns | probably a save prompt with nobody to answer it | save first. Pass `save_current: false` to `new` |
| `Error: Task timed out after …ms (task_id: …)` | the bridge stopped waiting, but the level may still be loading | `task_status` with the `task_id`, then `level_query list` |
| your edits to a level from `new` are not saved by `save_all` | the level still lives under `/Temp/`, or is an engine template | `save_as` to a `/Game/` path |
| `Actor '…' not found` from `info` | no actor with exactly that label | `find` with part of the label |
| an actor you expect is missing from `list` | it is a light or infrastructure | `find` includes lights. `get_level_actors` includes everything |

## See also

- `ue-level-actors`: spawning, moving, deleting and editing actors.
- `ue-assets`: `asset save_all`, `asset_search` for levels, and the asset
  dependency tools.
- `unreagent-iterate`: what an editor stop or restart does to unsaved levels.
