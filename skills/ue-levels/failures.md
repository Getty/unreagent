# Levels: when a call fails

"Level paths", the save prompt and the passages on `open`, `new`, `save_as`
and `level_query` are in the skill itself.

## Level path checks

Each path goes through these checks, in order:

| Check | Error |
|---|---|
| not empty | `Level path cannot be empty` |
| at most 512 characters | `Level path exceeds maximum length of 512 characters` |
| not under `/Engine/` or `/Script/` | `Cannot open engine or script levels` |
| no `..` | `Level path cannot contain path traversal sequences` |
| none of `` < > \| & ; ` $ ( ) { } [ ] ! * ? ~ `` | `Level path contains invalid character: '<c>'` |
| starts with `/Game/` | `Level path must start with '/Game/' to reference project content` |

## open_level errors

### open

| Error | Meaning |
|---|---|
| `Missing required parameter: level_path` | absent or an empty string |
| `Level not found: '<path>'. Use asset_search to find available maps.` | no package exists at that path. A path ending in `.umap` skips this check and fails at the next step instead |
| `Could not resolve level path: '<path>'` | the path does not map to a file on disk |
| `Failed to load level: '<path>'` | the engine returned no world |

### new

| Error | Meaning |
|---|---|
| `Template '<name>' not found. Use action 'list_templates' to see available templates.` | no template matched |
| `Could not resolve template path: '<path>'` | the template's map path did not map to a file |
| `Failed to create map from template: '<name>'` | the engine returned no world. Fall back to a blank level |
| `Failed to create new blank map.` | the engine returned no world for a blank level |
| `Editor engine not available.` | the editor's engine object is missing |

### save_as

| Error | Meaning |
|---|---|
| `Missing required parameter: save_path` | absent or an empty string |
| `Editor not available` | no editor |
| `No world currently loaded` | no level is open |
| `Failed to save level to: '<path>'` | the engine's save failed |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Level path must start with '/Game/' …` | a file path, an engine path or plugin content | pass the `/Game/` package path |
| `Level not found: '…'` | no package at that path | `asset_search` with `class_filter: "World"`. Use the part of `path` before the dot |
| an `open` or `new` call that never returns | probably a save prompt with nobody to answer it | save first. Pass `save_current: false` to `new` |
| `Error: Task timed out after …ms (task_id: …)` | the bridge stopped waiting, but the level may still be loading | `task_status` with the `task_id`, then `level_query list` |
| your edits to a level from `new` are not saved by `save_all` | the level still lives under `/Temp/`, or is an engine template | `save_as` to a `/Game/` path |
| `Actor '…' not found` from `info` | no actor with exactly that label (the object name does not count) | `find` with part of the label |
| an actor you expect is missing from `list` | it is a light or infrastructure, or its class name starts with a filtered prefix (section `filtered-classes`) | `find` includes lights. `get_level_actors` includes everything |
| actors you can see in the editor are missing from every query | a World Partition level: only loaded regions exist for the tools | section `level-types`; ask the user to load the region |
| `No active world. Open a level in the editor first`, even for a mistyped operation | `level_query` checks the open level before anything else | open a level |
| a template you named is not the one that opened | the first template whose name equals **or whose map path contains** the string won | pass the `name` exactly as `list_templates` returns it |
| `mapName` after `save_as` is not the name you saved under | the level was already on disk, so the engine saved a duplicate and the editor stays on the original | `open` the saved path before further edits |
| a level at the `save_as` path is gone | `save_as` deletes the destination's map files before saving | restore from version control; check paths with `asset_search` first |
| a save does not stick after reopening | `/Temp/` or `/Engine/` package skipped by `save_all`, or (One File Per Actor) an actor file not written | `save_as` to `/Game/`; section `level-types` |
