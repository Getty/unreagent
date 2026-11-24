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

The UE LLM Toolkit plugin runs inside the editor and offers five tools for the
actors of the level that is open there: `get_level_actors` lists them,
`spawn_actor` places one, `move_actor` changes a transform, `delete_actors`
removes actors, and `set_property` writes one property on an actor or on one of
its components.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

This skill has two sections:

| Section | Holds | Load it |
|---|---|---|
| `set-property` | `set_property` in full: how the property path is walked, the value form for every property type, the traps, the value errors, how a wrong JSON type is read | before your first `set_property` call |
| `failures` | the failure table: what you see, what it means, what to do; the five lookups `spawn_actor` tries on its `class` string, in order | when a call returns an error or a result you did not expect, or before you pass `spawn_actor` a class string in a form the table under `spawn_actor` does not list |

## Before the first call

- **Tool names.** The bridge lists each tool as `unreal_<name>`, for example
  `unreal_spawn_actor`, and your client may add its own prefix. This skill uses
  the bare names, as the plugin's errors do.
- **Result shape.** Success is the message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope (`task_id`, `status`, `success`, `message`, …), and the tool's own
  fields, the ones named in this skill, sit under its `data` key. Failure is
  `Error: <message>` with no data. Skill `ue-tasks` covers the task tools.
- **The editor world.** The tools act on the level open in the editor, never
  on a Play-In-Editor session. Without one they fail with `Editor not
  available. …` or `No active world. Open a level in the editor first`.
- **No undo, nothing saved.** No tool here opens an editor transaction, so a
  write has no undo entry. Writes mark the level dirty and leave the disk alone.
  Saving: skills `ue-levels` and `ue-assets` (`asset save_all`). An editor
  restart drops unsaved changes (`unreagent-iterate`).
- **Timeouts.** After 300 s (`MCP_ASYNC_TIMEOUT_MS`) the bridge returns `Error:
  Task timed out after 300000ms (task_id: …)`. That ends the wait, not the
  work, so read the state back with `get_level_actors` before you retry.

## Addressing an actor

Every actor has two names:

- the **object name**, `GetName()`, such as `PointLight_0`, unique in the level;
- the **label**, what the World Outliner shows, which can repeat.

`move_actor`, `delete_actors` and `set_property` accept either. They walk the
level's actors and take the **first** one whose object name or label equals the
string. The comparison is UE's `FString ==`; expect it to ignore case (not
verified here). Where labels repeat, address the actor by its object name.
`get_level_actors` returns both as `name` and `label`, and `spawn_actor`
returns them as `actorName` and `actorLabel`.

Before the lookup, the string is validated:

- it must not be empty;
- it has at most 256 characters;
- it contains no control characters;
- it contains none of `` < > | & ; ` $ ( ) { } [ ] ! * ? ~ ``.

A label such as `Crate (2)` is therefore rejected with `Actor name contains
invalid character: '('`. Use that actor's object name instead. A name that
passes validation but matches no actor gives `Actor not found: <name>. Use
level_query find to search for actors in the current level`.

`level_query` (skill `ue-levels`) addresses actors by label only, and its
`info` operation gives a full dump of one actor's components and collision.

## get_level_actors

Lists the actors of the level, one page at a time. It is read-only.

| Parameter | Default | Meaning |
|---|---|---|
| `class_filter` | — | substring of the class name, case-insensitive, at most 256 characters |
| `name_filter` | — | substring of the object name or the label, case-insensitive, at most 256 characters |
| `include_hidden` | `false` | include actors for which the engine's `IsHidden()` is true, most likely the Actor Hidden In Game flag. Hiding an actor in the editor viewport probably does not count (not verified) |
| `brief` | `true` | `false` adds `location`, `rotation`, `scale`, `hidden` and, when there are any, `tags` |
| `limit` | `25` | page size. Values of 0 or below become 25, and values above 1000 become 1000 |
| `offset` | `0` | matches to skip |

The result holds `actors[]`, with `name`, `label` and `class` in every entry,
plus `count`, `total`, `offset`, `limit`, `hasMore` and `levelName`.
`nextOffset` is present while more matches remain: pass it as the next
`offset`. The message reads `Found 25 actors (showing 1-25 of 112 total)`.

`class_filter` matches a substring of the class's short name. `Light` matches
`PointLight`, `SpotLight`, `DirectionalLight` and `SkyLight` alike. A Blueprint
actor's class is its generated class, `BP_Door_C`.

## spawn_actor

Places one actor of a class in the level.

| Parameter | Default | Meaning |
|---|---|---|
| `class` | required | the class to spawn, resolved as described below |
| `name` | engine-chosen | object name for the new actor. It is not the label |
| `location` | `{x:0, y:0, z:0}` | world location |
| `rotation` | `{pitch:0, yaw:0, roll:0}` | world rotation, in degrees |
| `scale` | `{x:1, y:1, z:1}` | scale |

The keys are lowercase. A missing component takes the default shown, which is 0
for location and rotation and 1 for scale.

### Which class string works

The class string is validated: at most 1024 characters, no `..`, and none of
the characters listed above.

A bare engine class name such as `PointLight`, `SpotLight`,
`DirectionalLight`, `StaticMeshActor`, `CameraActor` or `PlayerStart` works.
For anything other than an engine class, pass a full path:

| Class | Form that resolves |
|---|---|
| Blueprint under `/Game/` | `/Game/Blueprints/BP_Door.BP_Door_C`, or the object path `/Game/Blueprints/BP_Door.BP_Door` as `asset_search` returns it (the tool appends `_C`) |
| Blueprint in plugin content | `/MyPlugin/BP_Door.BP_Door_C`. The tool appends `_C` under `/Game/` only, so spell out `_C` |
| C++ class from a project or plugin module | `/Script/<Module>.<Class>`, for example `/Script/MyGame.MyActor` |

The tool's own description gives `/Game/Blueprints/BP_MyActor` as an example.
The tool turns that into `/Game/Blueprints/BP_MyActor_C`, which is not a class
path. Prefer the forms in the table.

Load section `failures` when `spawn_actor` answers `Class not found`, or
before you pass a class string in a form this table does not list: it has the
five lookups the tool tries, in order.

### What comes back

`actorName`, `actorClass`, `actorLabel` and `location`, with the message
`Spawned actor 'PointLight_0' of class 'PointLight'`.

- **`location` is the one you asked for, not read back.** The spawn uses the
  collision mode "adjust if possible, but always spawn". The engine can move the
  actor off the requested point when it would collide there. To see where it
  ended up, use `get_level_actors` with `brief: false`.
- **`name` is validated, but not checked for collisions.** It must pass the
  actor-name rules under "Addressing an actor". The tool does not look for an
  actor that already has the name, and it leaves the engine's name mode at its
  default. What the engine then does is outside the toolkit's code. UE's
  default name mode probably treats a taken name as fatal, which would take
  the editor down. That has not been verified against a running editor. Before
  you pass `name`, look it up with `get_level_actors` and `name_filter`.
  Without a `name`, the tool passes none and leaves naming to the engine.
- `Failed to spawn actor of class: <class>` means the class resolved but the
  engine returned no actor.

## move_actor

Changes the location, rotation or scale of one actor.

| Parameter | Default | Meaning |
|---|---|---|
| `actor_name` | required | object name or label |
| `location` | — | `{x, y, z}` |
| `rotation` | — | `{pitch, yaw, roll}` |
| `scale` | — | `{x, y, z}` |
| `relative` | `false` | change relative to the current transform instead of replacing it |

Only what you pass changes. Within a vector, only the components you pass
change: `{"location": {"z": 200}}` sets the height and keeps x and y. With
`relative: true`:

- `location` and `rotation` components are **added** to the current values;
- `scale` is **multiplied**. Components you leave out count as 1, so
  `{"scale": {"x": 2}, "relative": true}` doubles the width only.

Passing none of the three fails with `No transform changes specified. Provide
location, rotation, or scale.`

The result holds the actor's object name in `actor`, and its `location`,
`rotation` and `scale` read back after the change. The message names what
changed, for example `Updated location, rotation for actor 'Cube_3'`.

Prefer `move_actor` over `set_property` on `RelativeLocation` and friends. It
goes through the actor's own transform setters. `set_property` writes the raw
field.

## delete_actors

Removes actors from the level. The tool's description warns: "This is
destructive and cannot be undone via MCP."

| Parameter | Meaning |
|---|---|
| `actor_name` | one object name or label |
| `actor_names` | an array of object names or labels |
| `class_filter` | every actor whose class name contains this substring, case-insensitively |

The tool description says to use one of the three, but the code takes the
**union** of all you pass: a call with both `actor_name` and `class_filter`
deletes both sets.

- **`class_filter` includes hidden actors.** Unlike `get_level_actors`, it does
  not skip them. To preview exactly what it hits, run `get_level_actors` with
  the same `class_filter` and `include_hidden: true`. A short filter matches
  broadly: `Light` takes every light, the sky light included.
- **A name that fails validation aborts the whole call**, and nothing is deleted.
- **Names that are not found do not stop the call.** If at least one actor
  matched, the deletion goes ahead, and the names that did not resolve come back
  in `notFound`.

The result holds `deleted[]` (object names), `count` and, when some names did
not resolve, `notFound[]`. The message is `Deleted 3 actor(s)`.

| Error | Meaning |
|---|---|
| `No actors found: a, b` | none of the given names matched, and the class filter matched nothing either |
| `No actors specified or found to delete. Provide actor_name, actor_names array, or class_filter.` | no parameter given, or only a class filter that matched nothing |

## set_property

Writes one property, addressed by a dot path, on an actor or on one of its
components.

| Parameter | Meaning |
|---|---|
| `actor_name` | required. Object name or label |
| `property` | required. Dot path, for example `LightComponent.Intensity` |
| `value` | required. Any JSON type. Which type depends on the property |

A missing `value` gives `Missing required parameter: value`. On success, the
result holds `actor` and `property`, with the message `Set property
'LightComponent.Intensity' on actor 'PointLight_0'`.

The tool marks the actor and the level dirty, but it does not send the
property-changed notification an edit in the Details panel sends. A value that
depends on that notification, such as a recomputed derived field, may not
update until the next real edit.

**Load section `set-property` before your first `set_property` call.** It has
the path walk step by step, the value form for every property type (vectors,
rotators, colours, other structs, class and object references, arrays, maps),
the value errors, and how a wrong JSON type is read. What must not be got
wrong, in short:

- A path part that names no property is tried as a **component**, and the
  match is the first component whose name contains the part, ignoring case.
  Use a part that only one component's name contains.
- **A middle part that matches neither a property nor a component is skipped
  without an error.** The walk goes on with the same object, and if that
  object has a property of that name, the write lands on it. Check every part
  of a nested path against `level_query info` (skill `ue-levels`).
- Struct members cannot be reached through the path. Set the whole struct.
- A number is a JSON number, a `bool` a JSON boolean, an `FString` or `FName`
  a string. Every other type has its own form in the section.
- **Pass every component of an `FVector`, `FRotator` or `FLinearColor`
  object.** The tool builds the struct from scratch and does not merge it with
  the current value. **Colour keys are lowercase.**
- **An `FLinearColor` with `r`, `g` or `b` above 1.5 is read as 0–255**: all
  of them are divided by 255, with no error, so an HDR or emissive colour
  comes out wrong.
- A `TArray` value **replaces the whole array** and a `TMap` value **replaces
  the whole map**, so pass every element you want to keep.
- A top-level `enum class` property cannot be set at all, and a `TEnumAsByte`
  property takes its numeric value only.
- **A value of the wrong JSON type is often accepted instead.** An integer or
  byte property, a `TEnumAsByte` included, accepts any string: it counts by
  its leading digits, and one without any is stored as 0, with no error, so an
  enum value's name writes 0. A `bool` property accepts any string and any
  number, and a typo writes `false`. (Read in the UE 5.8.4 source, not run,
  while the toolkit targets UE 5.6 and 5.7.)

`set_property` changes the placed actor only. To change a Blueprint's default
for every instance, see `ue-blueprints`. To change a property on an asset, such
as a mesh or a material, use `asset set_asset_property` (skill `ue-assets`).

## Typical sequences

**Place and adjust a light**

1. `spawn_actor` `{"class": "PointLight", "location": {"x": 0, "y": 0, "z": 300}}`.
   Note `actorName` in the result.
2. `set_property` `{"actor_name": "<actorName>", "property": "LightComponent.Intensity", "value": 5000}`.
3. `move_actor` `{"actor_name": "<actorName>", "location": {"z": 400}}`.
4. Save the level (skill `ue-levels`).

**Delete by class, safely**

1. `get_level_actors` `{"class_filter": "StaticMeshActor", "include_hidden": true, "limit": 1000}`
   shows what will go.
2. If the list is not exactly what you mean, delete by `actor_names` instead,
   using the object names from step 1.
3. `delete_actors` with the chosen parameter. Compare `deleted` with step 1.

**Find an actor whose name you do not know**

`get_level_actors` with `name_filter`. For gameplay actors, use `level_query`
`find` (skill `ue-levels`), which also searches class names and leaves out
engine infrastructure.

## When a call fails

When a call returns an error or a result you did not expect, load section
`failures`: a table from what you see to what it means and what to do.

## See also

- `ue-levels`: `open_level`, saving a level, and `level_query` (`list`,
  `find`, `info`).
- `ue-assets`: `asset_search` for class and asset paths, `asset save_all`, and
  properties on assets.
- `ue-blueprints`: Blueprint classes, defaults and variables.
- `ue-lighting`: the `lighting` tool, which spawns the four standard lights
  and sets light, sky light, sky atmosphere, fog and post-process values by
  fixed key names. Its set operations list the keys they applied; its two
  spawns do not.
- `unreagent-iterate`: what an editor stop or restart does to unsaved changes.
- The plugin's background notes on actors in C++, served by
  `unreal_get_ue_context` with category `actor`
  (`Plugins/UELLMToolkit/Resources/mcp-bridge/contexts/actor.md`). The
  property-system notes, which cover CDO defaults versus placed actors, are in
  `Plugins/UELLMToolkit/Resources/domains/tooling.md`.
