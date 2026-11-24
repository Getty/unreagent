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
| `Level not found: '<path>'. Use asset_search to find available maps.` | no package exists at that path |
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
| `Actor '…' not found` from `info` | no actor with exactly that label | `find` with part of the label |
| an actor you expect is missing from `list` | it is a light or infrastructure | `find` includes lights. `get_level_actors` includes everything |
