---
name: ue-enhanced-input
description: >
  Create and edit Enhanced Input assets with enhanced_input: Input Actions,
  Input Mapping Contexts, key mappings, and the triggers and modifiers on a
  mapping. Use before you add or change player input bindings, or when a key
  mapping does not behave as expected.
toolkit_tools: [enhanced_input]
---

# UE Enhanced Input: enhanced_input

The UE LLM Toolkit plugin's **`enhanced_input`** tool creates and edits the two
Enhanced Input asset types: **Input Actions** (`IA_…`, what the player does)
and **Input Mapping Contexts** (`IMC_…`, which key triggers which action).
Through the plugin's MCP bridge it is listed as `unreal_enhanced_input`. It
works only while the editor is running and the plugin is reachable;
`unreagent-iterate` covers the wait for the plugin.

The tool takes an `operation` string. An unknown operation returns
`Unknown operation: <op>. Valid operations: create_input_action, create_mapping_context, add_mapping, remove_mapping, add_trigger, add_modifier, query_context, query_action`.

| Operation | Required | Optional |
|---|---|---|
| `create_input_action` | `action_name` | `package_path`, `value_type` |
| `create_mapping_context` | `context_name` | `package_path` |
| `add_mapping` | `context_path`, `action_path`, `key` | — |
| `remove_mapping` | `context_path`, `mapping_index` | — |
| `add_trigger` | `context_path`, `action_path`, `trigger_type` | `mapping_index`, the trigger's settings |
| `add_modifier` | `context_path`, `action_path`, `modifier_type` | `mapping_index`, the modifier's settings |
| `query_context` | `context_path` | — |
| `query_action` | `action_path` | — |

## Every write saves

Unlike the Blueprint and widget tools, **every operation that changes an asset
saves it to disk right away**. There is no separate save step and no undo.
Undo a mapping with `remove_mapping`. There is no operation to remove a
trigger or a modifier, or to delete an asset.

When the save fails, the call returns `Failed to save asset: <package>`, but
the change is already made in the editor's copy of the asset. Run the query
before you retry, so the change is not made twice.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work, and a write may still save. Run the query before you retry (skill
  `ue-tasks`).

## Paths

`context_path` and `action_path` take the asset path, such as
`/Game/Input/IMC_Default` and `/Game/Input/IA_Jump`; the tool adds the object
name itself. The object path that the create operations return
(`/Game/Input/IA_Jump.IA_Jump`) works too.

These paths and `package_path` pass the same check as Blueprint paths (skill
`ue-blueprints`, "Paths"), with the same messages, which say "Blueprint path":
no empty path, no `/Engine/` or `/Script/`, no `..`, none of the characters
`` <>|&;`$(){}[]!*?~ ``, at most 512 characters. A path that passes but does
not load returns `Failed to load InputAction: <path>` or
`Failed to load InputMappingContext: <path>`.

## Creating assets

**`create_input_action`** creates `<package_path>/<action_name>`.
`package_path` defaults to `/Game/Input`. `value_type` (default `Digital`) is
the action's value:

| `value_type` | Also accepted | Value |
|---|---|---|
| `Digital` | `Boolean`, `Bool` | on or off |
| `Axis1D` | `Float` | one float |
| `Axis2D` | `Vector2D` | two floats, such as a stick or mouse movement |
| `Axis3D` | `Vector` | three floats |

Any other value returns
`Invalid value_type: <v>. Valid types: Digital, Axis1D, Axis2D, Axis3D`. The
result has `asset_path`, `name` and `value_type` as you passed it.

**`create_mapping_context`** creates `<package_path>/<context_name>`. The
result has `asset_path`, `name` and `mapping_count: 0`.

The parameters are `action_name` and `context_name`. The plugin's context
notes show `name` in their example; the tool does not read `name` and fails
with `Missing required parameter: action_name`.

**Neither operation checks whether an asset already exists at
`<package_path>/<name>`.** Each builds the new asset and saves it, so it can
overwrite or collide with whatever is there. A `Failed to load …` from
`query_action` or `query_context` does not mean the name is free: it means only
that no asset of that type is at the path, and an asset of another type there
fails to load too. Check the path with the toolkit's `asset_search` tool
first: `path_filter` set to the folder, `name_pattern` to the name. Its
`name_pattern` matches substrings and `path_filter` includes subfolders, so the
name is taken only if an entry has exactly that `name` and that
`package_path`. Other errors:
`Failed to create package: <path>`, `Failed to create InputAction`,
`Failed to create InputMappingContext`.

## Mappings

**`add_mapping`** maps `key` to the action in the context. The result has
`context_path`, `action_path`, `key` and `mapping_index`, the new mapping's
position in the context. The tool does not check for an existing mapping of
the same key and action.

`key` is an engine key name, such as `SpaceBar`, `W`, `LeftShift`,
`Gamepad_FaceButton_Bottom` or `Gamepad_Left2D`, or one of these aliases:

| Alias | Key |
|---|---|
| `LMB`, `RMB`, `MMB` | `LeftMouseButton`, `RightMouseButton`, `MiddleMouseButton` |
| `Cross`, `FaceA` | `Gamepad_FaceButton_Bottom` |
| `Circle`, `FaceB` | `Gamepad_FaceButton_Right` |
| `Square`, `FaceX` | `Gamepad_FaceButton_Left` |
| `Triangle`, `FaceY` | `Gamepad_FaceButton_Top` |
| `L1`, `LB` / `R1`, `RB` | `Gamepad_LeftShoulder` / `Gamepad_RightShoulder` |
| `L2`, `LT` / `R2`, `RT` | `Gamepad_LeftTrigger` / `Gamepad_RightTrigger` |
| `L2Axis`, `LTAxis` / `R2Axis`, `RTAxis` | `Gamepad_LeftTriggerAxis` / `Gamepad_RightTriggerAxis` |
| `L3` / `R3` | `Gamepad_LeftThumbstick` / `Gamepad_RightThumbstick` (stick click) |
| `LeftStickX`, `LeftStickY` | `Gamepad_LeftX`, `Gamepad_LeftY` |
| `RightStickX`, `RightStickY` | `Gamepad_RightX`, `Gamepad_RightY` |
| `LeftStick2D` / `RightStick2D` | `Gamepad_Left2D` / `Gamepad_Right2D` |
| `DPadUp`, `DPadDown`, `DPadLeft`, `DPadRight` | `Gamepad_DPad_Up`, `Gamepad_DPad_Down`, `Gamepad_DPad_Left`, `Gamepad_DPad_Right` |
| `Start`, `Options`, `Menu` | `Gamepad_Special_Right` |
| `Select`, `Share`, `View` | `Gamepad_Special_Left` |

A name the engine does not know returns `Invalid key name: <k>. Use standard UE key names …`.
The result's `key` echoes what you passed; `query_context` shows the engine
name.

**`remove_mapping`** takes the `mapping_index` from `query_context`. A missing
or negative index returns `Missing or invalid mapping_index parameter`, and
an index at or past the end returns
`Invalid mapping_index <i>. Context has <n> mappings (0-<n-1>)`. The result
has `removed_index`, `removed_key` and `removed_action`.

The tool removes the mapping through the engine by its action and key, not by
position. When the context maps the same key to the same action twice, check
which one went. **Indices can change after a removal**: run `query_context`
again before the next call that takes an index.

## Triggers and modifiers

`add_trigger` and `add_modifier` change one mapping of the context: the one at
`mapping_index`, or without it the first mapping of `action_path`. Errors:
`No mapping found for action '<a>' in context '<c>'. Use query_context to see available mappings.`,
`Invalid mapping_index <i>. Context has <n> mappings.`,
`Mapping at index <i> is for action '<x>', not '<a>'`.

Each call adds one more trigger or modifier to the mapping. The results have
`trigger_count` or `modifier_count`. Calling twice adds the same one twice, and
neither can be removed with this tool.

**`trigger_type`:**

| Type | Settings | Accepted range |
|---|---|---|
| `Pressed`, `Released`, `Down` | — | — |
| `Hold`, `HoldAndRelease` | `hold_time` (default 0.5 s) | more than 0, at most 60 |
| `Tap` | `tap_release_time` (default 0.2 s) | more than 0, at most 5 |
| `Pulse` | `pulse_interval` (default 0.1 s) | more than 0.01, at most 10 |
| `ChordAction` | `chord_action_path` (required): the Input Action that must be active as well | — |

Out of range returns `hold_time must be between 0 and 60 seconds` and so on.
For `Pulse`, the message says "between 0.01 and 10", but 0.01 itself is
rejected. Other errors:
`ChordAction trigger requires 'chord_action_path' parameter`, the load errors
for the chord action, and
`Invalid trigger_type: <t>. Valid types: Pressed, Released, Down, Hold, HoldAndRelease, Tap, Pulse, ChordAction`.

**`modifier_type`:**

| Type | Settings |
|---|---|
| `Negate` | — |
| `Swizzle` | `swizzle_order`: `YXZ` (default), `ZYX`, `XZY`, `YZX`, `ZXY` |
| `Scalar` | `scalar`: `{x, y, z}`; an axis left out is 1 |
| `DeadZone` | `dead_zone_lower` (default 0.2), `dead_zone_upper` (default 1.0), each from 0 to 1 with lower below upper; `dead_zone_type`: `Radial`, any other value is axial |

Errors: `Invalid swizzle_order: <o>. Valid orders: YXZ, ZYX, XZY, YZX, ZXY`,
`dead_zone_lower must be between 0.0 and 1.0`, the same for `dead_zone_upper`,
`dead_zone_lower must be less than dead_zone_upper`,
`Invalid modifier_type: <m>. Valid types: Negate, Swizzle, Scalar, DeadZone`.

**Check that they persist.** The tool creates trigger and modifier objects
that do not belong to the context asset, and then saves the context. Whether
they survive a save and an editor restart has not been verified for this
skill. Before you rely on one, restart the editor and check the mapping's
`triggers` and `modifiers` in `query_context`.

## Queries

- **`query_context`** returns `name`, `path`, `mapping_count` and `mappings`,
  each with `index`, `key`, `action`, `action_path`, and `triggers` and
  `modifiers` (each entry's `type` is the engine class, such as
  `InputTriggerHold`).
- **`query_action`** returns `name`, `path`, `value_type` (`Digital`,
  `Axis1D`, `Axis2D`, `Axis3D` or `Unknown`), and the action's own `triggers`
  and `modifiers`. These are set on the action itself, which this tool cannot
  do; `add_trigger` and `add_modifier` change mappings only.

## A typical sequence

1. `asset_search` for the names you plan to create (see "Creating assets").
   Reuse an existing Input Action or Mapping Context, and read it with
   `query_action` or `query_context`.
2. `create_input_action` with `action_name: "IA_Jump"`, and
   `create_mapping_context` with `context_name: "IMC_Default"`, if needed.
3. `add_mapping` with `key: "SpaceBar"`, and again with `key: "Cross"` for the
   gamepad.
4. `add_trigger` or `add_modifier` where the mapping needs one, with the
   `mapping_index` from step 3.
5. `query_context` to verify.
6. Use the action in a Blueprint: the `EnhancedInputAction` node of
   `blueprint_modify add_node`, with `action_path` (skill `ue-blueprints`,
   section `graphs`).

An action fires at runtime only when the game adds the mapping context to the
player's Enhanced Input subsystem. This tool does not do that; the plugin's
context notes show the game code.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or empty |
| `Failed to load InputAction: <path>` / `Failed to load InputMappingContext: <path>` | no asset of that type at the path |
| `Invalid key name: <k>. …` | not an engine key name or an alias |
| `No mapping found for action '<a>' in context '<c>'. …` | `add_mapping` first |
| `Mapping at index <i> is for action '<x>', not '<a>'` | the index belongs to another action; check `query_context` |
| `Failed to save asset: <package>` | the change was made but not saved |
| `Cannot access engine or script Blueprints` | an `/Engine/` or `/Script/` path |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| the action never fires in play | the mapping context is not added at runtime, or there is no mapping for the key | `query_context`, then check the game code that adds the context |
| a trigger or modifier is gone after a restart | it did not survive the save (see "Check that they persist") | set it in the editor by hand, and tell the user |
| `remove_mapping` removed a different mapping than expected | the indices changed, or the same key and action are mapped twice | `query_context` before every index-based call |
| a mapping or trigger exists twice | the call was repeated | `remove_mapping` for a duplicate mapping; a duplicate trigger stays |

## Background in the plugin

- `unreal_get_ue_context` with `category: "enhanced_input"` serves the plugin's
  UE API notes on Enhanced Input: the classes, trigger and modifier types, key
  names, and the game code that adds a mapping context. Its "MCP Tool
  Operations" example passes `name` to the create operations, which this tool
  does not read.
- The plugin's `Resources/domains/debug.md` covers driving Input Actions
  during play with `gameplay_debug`.

## See also

- `ue-blueprints`: the `EnhancedInputAction` node that reacts to an action.
- `ue-widgets`: widget events, for input handled by the UI.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
