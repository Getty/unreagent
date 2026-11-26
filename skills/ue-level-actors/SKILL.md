---
name: ue-level-actors
description: >
  Spawn, list, move, delete and edit actors in the level open in the editor,
  through the UE LLM Toolkit's actor tools. Use before placing or changing
  actors, or when spawn_actor, move_actor, delete_actors or set_property
  returns an error.
toolkit_tools: [spawn_actor, get_level_actors, move_actor, delete_actors, set_property]
---

# Level actors

Five tools of the UE LLM Toolkit plugin work on the actors of the level open
in the editor: `get_level_actors` lists them, `spawn_actor` places one,
`move_actor` changes a transform, `delete_actors` removes actors, and
`set_property` writes one property on an actor or on one of its components.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

| Section | Holds | Load it |
|---|---|---|
| `set-property` | `set_property` in full: the path walk, the value form per property type, ready paths for common actors, the traps, every value error | before your first `set_property` call |
| `workflows` | worked sequences: populate a level (floor, PlayerStart, light, Blueprint actor, nav volume), grid placement, safe bulk delete, repairing a bad edit, the verification loop | before you build or rearrange more than one or two actors |
| `failures` | the failure table (what you see, what it means, what to do) and the five lookups `spawn_actor` tries on `class`, in order | when a call returns an error or a result you did not expect, or before you pass `class` in a form the table below does not list |

## Before the first call

- **Tool names.** The bridge lists each as `unreal_<name>` (`unreal_spawn_actor`),
  and your client may add its own prefix. This skill uses the bare names.
- **Result shape.** Success is the message, a blank line, then JSON. By default
  the bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …) and the tool's own fields sit
  under its `data` key. Failure is `Error: <message>` with no data (skill
  `ue-tasks`).
- **The editor world only.** The tools act on the level open in the editor,
  never on a Play-In-Editor session. Without one: `Editor not available. …` or
  `No active world. Open a level in the editor first`.
- **No undo, nothing saved.** No tool here opens an editor transaction, so no
  write has an undo entry. Writes mark the level dirty (`move_actor` and
  `set_property` the actor's own package too) and leave the disk alone. Save
  with `asset save_all` (skill `ue-assets`) or as skill `ue-levels` says. An
  editor restart drops unsaved changes (`unreagent-iterate`). In a One File
  Per Actor level, whether `save_all` writes a spawned or deleted actor's own
  file is unverified: reopen the level after saving and check.
- **World Partition levels: only loaded actors exist for these tools.** They
  all walk the editor world's actor list. In a World Partition level the
  editor loads only part of the map (non-spatially-loaded actors plus the
  loaded regions), so an unloaded actor is not listed, not found, and not hit
  by `delete_actors class_filter`. Skill `ue-levels`, section `level-types`.
- **Units.** Location in centimetres, rotation in degrees, Z up, X forward,
  Y right (left-handed). `{pitch, yaw, roll}`: yaw turns about Z.
- **Timeouts.** After 300 s the bridge returns `Error: Task timed out after
  300000ms (task_id: …)`. That ends the wait, not the work: read the state back
  with `get_level_actors` before you retry.

## Addressing an actor

Every actor has an **object name** (`GetName()`, such as `PointLight_0`, unique
in the level) and a **label** (what the World Outliner shows, which can
repeat). `move_actor`, `delete_actors` and `set_property` take either: they
walk the level and use the **first** actor whose object name or label equals
the string (UE's `FString ==`, which ignores case). Where labels repeat, use
the object name. `get_level_actors` returns both as `name` and `label`,
`spawn_actor` as `actorName` and `actorLabel`.

The string is validated first: not empty, at most 256 characters, no control
characters, none of `` < > | & ; ` $ ( ) { } [ ] ! * ? ~ ``. A label such as
`Crate (2)` fails with `Actor name contains invalid character: '('`; use the
object name. A valid string that matches nothing gives `Actor not found:
<name>. Use level_query find to search for actors in the current level`.

`level_query` (skill `ue-levels`) addresses actors by label only; its `info`
lists an actor's components, which `set_property` paths need.

## get_level_actors

Read-only, paged.

| Parameter | Default | Meaning |
|---|---|---|
| `class_filter` | — | substring of the class name, case-insensitive, at most 256 characters |
| `name_filter` | — | substring of the object name or the label, case-insensitive, at most 256 characters |
| `include_hidden` | `false` | include actors whose `IsHidden()` is true: the **Actor Hidden In Game** flag (`bHidden`), not the editor-only eye toggle |
| `brief` | `true` | `false` adds `location`, `rotation`, `scale`, `hidden` and, when there are any, `tags` |
| `limit` | `25` | page size; 0 or below becomes 25, above 1000 becomes 1000 |
| `offset` | `0` | matches to skip; a negative value becomes 0 |

The result holds `actors[]` (`name`, `label`, `class` each), `count`, `total`,
`offset`, `limit`, `hasMore`, `levelName`, and `nextOffset` while more remain.
The message is `Found 25 actors`, plus ` (showing 1-25 of 112 total)` when
the page is not everything.

`class_filter` matches the short class name: `Light` matches `PointLight`,
`SpotLight`, `DirectionalLight` and `SkyLight` alike. A Blueprint actor's class
is its generated class, `BP_Door_C`.

## spawn_actor

| Parameter | Default | Meaning |
|---|---|---|
| `class` | required | the class to spawn (below) |
| `name` | engine-chosen | object name for the new actor, not the label |
| `location` | `{x:0, y:0, z:0}` | world location |
| `rotation` | `{pitch:0, yaw:0, roll:0}` | world rotation |
| `scale` | `{x:1, y:1, z:1}` | scale |

**Keys are lowercase.** A component left out, or spelt `X` or `Yaw`, takes the
default, silently.

**Which class string works.** Validated first: at most 1024 characters, no
`..`, none of the characters above. Then:

| Class | Form that resolves |
|---|---|
| engine class in module `Engine` | the bare name: `PointLight`, `SpotLight`, `DirectionalLight`, `RectLight`, `SkyLight`, `StaticMeshActor`, `CameraActor`, `PlayerStart`, `TriggerBox` |
| engine class in another module | `/Script/<Module>.<Class>`: `/Script/NavigationSystem.NavMeshBoundsVolume` |
| Blueprint under `/Game/` | `/Game/BP/BP_Door.BP_Door_C`, or the object path `/Game/BP/BP_Door.BP_Door` as `asset_search` returns it (the tool appends `_C`) |
| Blueprint in plugin content | `/MyPlugin/BP_Door.BP_Door_C`, `_C` spelt out (appended under `/Game/` only) |
| C++ class of a project module | `/Script/MyGame.MyActor` |

The tool's own description suggests `/Game/Blueprints/BP_MyActor`; that becomes
`/Game/Blueprints/BP_MyActor_C`, which is not a class path. Use the table.

**What comes back:** `actorName`, `actorClass`, `actorLabel` and `location`,
with `Spawned actor 'PointLight_0' of class 'PointLight'`.

- **`location` is the requested one, not read back.** The spawn uses "adjust
  if possible, but always spawn", so the engine may move the actor off a
  colliding point. Read it back with `get_level_actors` `brief: false`.
- **A taken `name` crashes the editor.** The tool does not check it and
  leaves the engine's name mode at its default, `Required_Fatal`, which logs
  a fatal error on a taken name (UE 5.8.4 source, `World.cpp`,
  `LevelActor.cpp`). Look a name up with `get_level_actors` `name_filter`
  first, or omit `name`.
- `Failed to spawn actor of class: <class>`: the class resolved, the engine
  returned no actor (an abstract class, for example).
- A spawned `StaticMeshActor` has no mesh, a volume no brush shape. Section
  `workflows` covers both.

## move_actor

| Parameter | Default | Meaning |
|---|---|---|
| `actor_name` | required | object name or label |
| `location` | — | `{x, y, z}` |
| `rotation` | — | `{pitch, yaw, roll}` |
| `scale` | — | `{x, y, z}` |
| `relative` | `false` | change relative to the current transform |

Only what you pass changes, down to the component: `{"location": {"z": 200}}`
keeps x and y. With `relative: true`, `location` and `rotation` components are
**added**; `scale` is **multiplied**, a left-out component counting as 1.
None of the three: `No transform changes specified. Provide location,
rotation, or scale.` The result holds `actor` (object name) and the
`location`, `rotation` and `scale` read back after the change.

Prefer `move_actor` over `set_property` on `RootComponent.RelativeLocation`:
it goes through the actor's transform setters, `set_property` writes the raw
field.

## delete_actors

"This is destructive and cannot be undone via MCP."

| Parameter | Meaning |
|---|---|
| `actor_name` | one object name or label |
| `actor_names` | an array of them |
| `class_filter` | every actor whose class name contains this substring, case-insensitive |

- **The union of all you pass is deleted**, though the description says "use
  one".
- **`class_filter` hits everything matching**: hidden actors and engine
  infrastructure included, unlike `get_level_actors`' default. `Light` takes
  the sky light too. Preview with `get_level_actors`, the same filter,
  `include_hidden: true`, `limit: 1000`.
- A name that fails validation aborts the whole call; nothing is deleted.
  Names not found do not stop it when something else matched; they come back
  in `notFound`.
- `deleted[]` lists what was **collected**, not checked after the destroy. Read
  the level back to confirm.

Result: `deleted[]` (object names), `count`, `notFound[]` when any. Errors:
`No actors found: a, b` (nothing matched), and `No actors specified or found
to delete. Provide actor_name, actor_names array, or class_filter.`

## set_property

Writes one property, addressed by a dot path, on an actor or one of its
components: `actor_name` (required), `property` (required, such as
`LightComponent.Intensity`), `value` (required, any JSON type). Success holds
`actor` and `property`. **Load section `set-property` before your first
call.** What must not be got wrong:

- **No change notification.** The write is raw: no `PostEditChangeProperty`,
  no render-state refresh. The viewport may keep showing the old mesh,
  material or light value until the level is reloaded (unverified). Verify
  with `level_query info` or a viewport capture, not with the success message.
- A path part that names no property is tried as a **component**: the first
  whose name contains the part. A middle part matching neither is **skipped
  without an error**, and the write can land one level up.
- Struct members cannot be reached through the path; set the whole struct,
  with every component (a left-out one is not merged from the current value).
- `TArray` and `TMap` values **replace the whole container**, and a failed
  element leaves it emptied or half-filled.
- `FText` and top-level `enum class` properties cannot be set; a `TEnumAsByte`
  takes its number only, a name string writes 0 without an error.
- An `FLinearColor` with `r`, `g` or `b` above 1.5 is read as 0–255.
- Soft references are stored as given, without loading: a typo is not an error.

`set_property` changes the placed actor only. A Blueprint default for every
instance: `ue-blueprints`. A property on an asset (mesh, material):
`asset set_asset_property` (`ue-assets`).

## When a call fails

Load section `failures`: what you see, what it means, what to do.

## See also

- `ue-levels`: opening, creating, saving levels; `level_query`; World Partition.
- `ue-assets`: `asset_search` for class and asset paths, `asset save_all`.
- `ue-blueprints`: Blueprint classes, defaults and variables.
- `ue-lighting`: the `lighting` tool, which spawns the standard lights and sets
  light, sky, fog and post-process values by fixed key names.
- `ue-viewport`: capturing the viewport to check placement by eye.
- `ue-python`: `execute_script`, for what no tool here does (actor labels,
  Outliner folders, attaching actors).
- The plugin's notes on actors in C++: `unreal_get_ue_context` with category
  `actor`; property-system notes (CDO defaults versus placed actors) in
  `Plugins/UELLMToolkit/Resources/domains/tooling.md`.
