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
- `level_query` surveys the open level: gameplay actors by class, a search,
  and one actor in detail.

Placing and changing single actors: skill `ue-level-actors`.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

| Section | Holds | Load it |
|---|---|---|
| `workflows` | worked sequences: create a level and give it a home, switch levels, populate and verify, repair after a bad edit | before you create, switch or rebuild a level |
| `level-types` | World Partition versus classic levels, One File Per Actor and what a save writes, templates, sublevels and streaming (no tool) | before you work in a level you did not create, or when actors are missing from a query, or a save does not stick |
| `filtered-classes` | every class prefix and name `level_query` `list` and `find` leave out, the light classes `list` leaves out, and the keys of `filtered_categories` | before you conclude from `list` or `find` that the level holds no actor of a class |
| `failures` | the checks a level path goes through, the errors of `open`, `new` and `save_as`, the failure table | when a call returns an error or does not return, or a result is not what you expect |

## Before the first call

- **Tool names.** The bridge lists them as `unreal_open_level` and
  `unreal_level_query`; your client may add its own prefix.
- **Result shape.** Success is the message, a blank line, then JSON. By default
  the bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …) and the tool's own fields sit
  under its `data` key. Failure is `Error: <message>` (skill `ue-tasks`).
- **The editor world.** Both act on the level open in the editor, never on a
  Play-In-Editor session.
- **World Partition: only loaded actors exist for the tools.** In a World
  Partition level the editor loads only part of the map, and `level_query`
  (like the actor tools) sees only loaded actors. Section `level-types`.
- **Timeouts.** After 300 s (`MCP_ASYNC_TIMEOUT_MS`) the bridge returns `Error:
  Task timed out after 300000ms (task_id: …)`. That ends the wait, not the
  work: a level can still finish loading. Follow the task up (`ue-tasks`)
  before you retry. A large level can hold the editor for more than 30 s; run
  `open_level` as skill `ue-tasks`, "A 30 s timeout can crash the editor",
  describes.

## Level paths

`open_level` takes the **package path**: `/Game/Maps/Arena`, not a file path,
not an object path, and **without `.umap`** (a path ending in `.umap` skips
the existence check and fails later). `asset_search` (skill `ue-assets`) finds
levels with `class_filter: "World"`; its `path` is the object path
`/Game/Maps/Arena.Arena`, and the part before the dot is the package path.

A path is refused when it is empty, over 512 characters, under `/Engine/` or
`/Script/`, holds `..` or one of `` < > | & ; ` $ ( ) { } [ ] ! * ? ~ ``, or
does not start with `/Game/`. A level in a plugin's content folder therefore
cannot be opened or saved through `open_level`.

## The save prompt: never leave one open

The tool says: "The editor will prompt to save unsaved changes before
switching levels." `open` always uses the engine's save prompt; `new` prompts
unless `save_current: false` (the code only hands that flag to the engine).

A save prompt waits for a person. With nobody at the editor, expect the call
not to return before the bridge gives up (unverified). So:

1. Save first: `asset` `{"operation": "save_all"}` (skill `ue-assets`), or
   `save_as` for a level never saved.
2. Then `open`, or `new` with `save_current: false`.

No parameter discards unsaved changes. Drop them with an editor restart
(`unreagent-iterate`).

## open_level

`action` is required, lowercased and trimmed. Unknown: `Unknown action:
'<action>'. Use 'open', 'new', 'save_as', or 'list_templates'.`

**`open`** — `level_path` (required). Replaces the current level. Result:
`action`, `levelPath`, `mapName`, `worldName`; message `Opened level: Arena`.

**`new`** — `template` (default: blank level), `save_current` (default
`true`). Replaces the current level.

- Blank: result `action`, `template: "blank"`, `mapName`; message `Created new
  blank map: <name>`. The level has no `/Game/` path yet: `save_as` it before
  you rely on `save_all`, which skips packages under `/Temp/`.
- With `template`: the **first** template, in `list_templates` order, whose
  name equals the string (ignoring case) **or whose map path contains it**.
  Pass a `name` exactly as listed; a short string can hit an earlier template.
  The template's own map is **opened**, not copied: `save_as` to a `/Game/`
  path before you change anything. `save_current` is not used on this branch.

**`save_as`** — `save_path` (required, checked like any level path). Saves the
open level there through the engine's `FEditorFileUtils::SaveMap`. Result:
`action`, `save_path`, `filename` (the file on disk), `mapName`; message
`Saved level to: /Game/Maps/Arena`. What the engine does (read in the UE 5.8.4
source, `FileHelpers.cpp` `SaveWorld`, not run):

- **A level never saved** (after `new`) is renamed to the new path: you go on
  editing it there, and `mapName` is the new name.
- **A level already on disk** is **duplicated** to the new path; the editor
  keeps editing the original, and `mapName` is the old name. `open` the copy
  before editing it.
- **Map files already at the destination are deleted first**: `save_as`
  overwrites without asking. Check the path is free.
- A World Partition level has all its actors loaded before the rename, which
  can be slow and memory-heavy on a large map.

**`list_templates`** — no parameters. Result: `action`, `count`,
`templates[]` with `name` and `mapPath`.

## Saving a level

| Situation | Call |
|---|---|
| never saved (after `new`) | `open_level` `save_as` with a `/Game/` path |
| already has a `/Game/` path | `asset` `save_all` (skill `ue-assets`): every dirty package, no dialog, the level and changed assets included |
| a copy under a new path | `open_level` `save_as`, then check `mapName` |

In a One File Per Actor level, actor changes live in the actors' own files;
section `level-types` says what to check after a save.

## level_query

Read-only. The open level is checked first (`No active world. Open a level in
the editor first`), then `operation` (required, lowercased; aliases `search`
→ `find`, `get` / `get_info` / `inspect` → `info`). Unknown: `Unknown
operation: '<op>'. Valid: list, find, info`, plus ` Did you mean '<op>'?` when
one is close. It works on **labels** (what the World Outliner shows) and
leaves engine infrastructure out of `list` and `find`: landscapes, foliage,
navigation data, World Partition and level-instance actors, game-mode
classes, sky and fog actors, and more; `list` also leaves out the four light
classes, `find` keeps them. Section `filtered-classes` has every rule.

**`list`** — no paging; large on a large level. Result: `map_name`,
`total_actors`, `gameplay_actors`, `filtered_count`; `by_class` (class name →
array of `{label, class, location}`, classes and labels sorted); and, when
anything was filtered, `filtered_categories` (counts). Message `37 gameplay
actors (412 filtered)`.

**`find`** — `pattern` (required: `'find' operation requires a 'pattern'
parameter`): case-insensitive substring of label or class name. Result:
`map_name`, `pattern`, `total_actors`, `match_count`, `matches[]` sorted by
label, each with `label`, `class`, `location`, `rotation`, `scale` and, in an
Outliner folder, `folder`.

**`info`** — `actor_label` (required: `'info' operation requires an
'actor_label' parameter`): the whole label, case ignored; the object name does
not work. The first match is used. Result: `map_name`, `label`, `class`,
`name` (the object name for the `ue-level-actors` tools), `blueprint_path` for
a Blueprint class, `transform` (`location`, `rotation`, `scale`),
`component_count`, `components[]` (`name`, `class`, `is_primitive: true` for
components that render or collide) and, when there is one, `collision` for the
primitive components. `Actor '<label>' not found` is all a miss returns; the
plugin's suggestions are dropped. Run `find` with part of the label.

| Need | Tool |
|---|---|
| gameplay actors grouped by class | `level_query` `list` |
| search by label or class, without engine noise | `level_query` `find` |
| components, collision, Blueprint origin of one actor | `level_query` `info` |
| every actor, infrastructure and hidden ones included, paged | `get_level_actors` (skill `ue-level-actors`) |
| object names, to address actors reliably | `info`'s `name`, or `get_level_actors` |

## When a call fails

Load section `failures`.

## See also

- `ue-level-actors`: spawning, moving, deleting and editing actors.
- `ue-assets`: `asset save_all`, `asset_search` for levels, dependencies.
- `ue-lighting`: lights, sky and fog for a new level.
- `ue-game-framework`: the level's game mode override (what spawns at play).
- `ue-viewport`, `ue-pie-testing`: checking a level by eye and in play.
- `unreagent-iterate`: what an editor stop or restart does to unsaved levels.
