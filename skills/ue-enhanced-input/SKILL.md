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
Enhanced Input asset types: **Input Actions** (`IA_…`, what the player does,
with a value type) and **Input Mapping Contexts** (`IMC_…`, which key drives
which action). Through the plugin's MCP bridge it is listed as
`unreal_enhanced_input`. It works only while the editor is running and the
plugin is reachable; `unreagent-iterate` covers the wait for the plugin.

## Sections

| Section | Holds | Load it |
|---|---|---|
| `operations` | every operation's parameters, defaults, results and errors; the path rules; `value_type` names; how `remove_mapping` really picks its victim; what `query_context` does not show | before a create, a `remove_mapping`, or any call whose result or error you need to read exactly |
| `runtime-setup` | what makes a mapping work in play: adding the context (project default in `DefaultInput.ini`, player controller, pawn), priority, reacting to the action in Blueprint or C++, input mode, pause, user settings, 5.6–5.8 changes, how to verify | whenever the input must work in PIE or the game, not just exist as assets |
| `triggers-modifiers` | trigger events (`Started`, `Triggered`, …), every `trigger_type` and `modifier_type` with settings and ranges, the WASD pattern, why the tool's triggers and modifiers are not saved | before `add_trigger` or `add_modifier`, or when an action fires too often or on the wrong axis |
| `key-aliases` | the short key names (`LMB`, `Cross`, `L2`, `DPadUp`, …) with the engine key each stands for, and common engine key names | before you pass a key that is not an engine key name, or on `Invalid key name` |
| `failures` | the error table with fixes, and the in-play table (key does nothing, works until a menu, fires every frame, …) | when a call errors, or input in play is not what you expect |

## Must not get wrong

- **Assets are not input.** A mapping does nothing until the game adds its
  context to the player's Enhanced Input subsystem and something reacts to
  the action. This tool does neither: section `runtime-setup`.
- **Every write saves to disk at once.** No separate save, no undo. Undo a
  mapping with `remove_mapping`; triggers, modifiers and assets cannot be
  removed with this tool.
- **Creates overwrite.** `create_input_action` and `create_mapping_context`
  do not check for an existing asset. Look first with `asset_search`
  (skill `ue-assets`); a `Failed to load …` from a query does not prove the
  name is free.
- **The parameters are `action_name` and `context_name`**, not `name` (the
  plugin's own context notes are wrong there).
- **Triggers and modifiers added by this tool are not saved with the
  asset.** The code creates them in the transient package; the save writes
  the reference as empty (read in the toolkit and UE 5.8.4 source, not run).
  They work in this editor session only. Section `triggers-modifiers`,
  "Why they do not survive a reload", has what to do instead.
- **Without `mapping_index`, `add_trigger` and `add_modifier` change the
  first mapping of the action**, whichever key you meant.
- **`remove_mapping` removes the first mapping with that key and action and
  moves the last mapping into its slot.** Every index you held is stale
  afterwards: run `query_context` again.
- **A mapping with no trigger fires `Triggered` every frame** the key is
  held. React on `Started` for one-shots such as jump.
- **`value_type` cannot be changed later** by the tool. Movement and look are
  `Axis2D`, buttons `Digital`.

## Operations

`operation` selects one; there are no aliases. An unknown one returns
`Unknown operation: <op>. Valid operations: create_input_action, create_mapping_context, add_mapping, remove_mapping, add_trigger, add_modifier, query_context, query_action`.

| Operation | Required | Optional | Result |
|---|---|---|---|
| `create_input_action` | `action_name` | `package_path` (`/Game/Input`), `value_type` (`Digital`) | `asset_path`, `name`, `value_type` |
| `create_mapping_context` | `context_name` | `package_path` (`/Game/Input`) | `asset_path`, `name`, `mapping_count` |
| `add_mapping` | `context_path`, `action_path`, `key` | — | `mapping_index` of the new mapping, … |
| `remove_mapping` | `context_path`, `mapping_index` | — | `removed_index`, `removed_key`, `removed_action` |
| `add_trigger` | `context_path`, `action_path`, `trigger_type` | `mapping_index`, the trigger's settings | `trigger_count` |
| `add_modifier` | `context_path`, `action_path`, `modifier_type` | `mapping_index`, the modifier's settings | `modifier_count` |
| `query_context` | `context_path` | — | `mappings`: `index`, `key`, `action`, `action_path`, `triggers`, `modifiers` |
| `query_action` | `action_path` | — | `value_type`, the action's own `triggers`, `modifiers` |

Paths are package paths such as `/Game/Input/IA_Jump`; the object path
(`…/IA_Jump.IA_Jump`) works too. `/Engine/` and `/Script/` are refused.
`value_type`: `Digital`, `Axis1D`, `Axis2D`, `Axis3D`, case-sensitive.

## Common path: a player can jump and move

1. `asset_search` for `IA_Jump`, `IA_Move`, `IMC_Default` in the folder
   (skill `ue-assets`). Reuse what exists; read it with `query_action` /
   `query_context`.
2. `create_input_action` `action_name: "IA_Jump"` (`Digital`) and
   `action_name: "IA_Move"`, `value_type: "Axis2D"`;
   `create_mapping_context` `context_name: "IMC_Default"`.
3. `add_mapping` `IA_Jump` ← `SpaceBar`, and again ← `Cross` for the gamepad.
   `IA_Move` ← `Gamepad_Left2D`, and ← `W`, `A`, `S`, `D` for the keyboard.
   Keep each returned `mapping_index`.
4. The WASD axes need Swizzle and Negate per key (section
   `triggers-modifiers`, WASD pattern), and those are not saved by this
   tool: set them in the editor, or tell the user to. A stick
   (`Gamepad_Left2D`) needs none.
5. `query_context` to verify keys, actions and indices.
6. Make it work in play (section `runtime-setup`): add `IMC_Default` to the
   player (through the tools: the project default context; the Blueprint
   `Add Mapping Context` node chain cannot be built with `blueprint_modify`),
   and react with an `EnhancedInputAction` node
   (skill `ue-blueprints`, section `graphs`): Jump on `Started`, Move on
   `Triggered`. Movement on the character: skill `ue-character`; the level's
   pawn and controller classes: skill `ue-game-framework`.
7. Verify in PIE (skill `ue-pie-testing`). `gameplay_debug` injects the
   action's value, not a key press: it skips the key mapping and the mapping
   context, and their triggers and modifiers (UE 5.8.4 source: `InjectInputForAction` stores the value with the caller's modifiers and triggers, `EnhancedPlayerInput.cpp:284-291`, and the toolkit passes none, `MCPTool_GameplayDebug.cpp:254`; the injected value goes straight to `ProcessActionMappingEvent`, `:646-667` and `:216`, never through a key mapping or a mapping context). It tests the
   reaction, not the mapping; check `get_output_log` for errors.

## Results through the bridge

Success is the tool's message, a blank line, then JSON: by default a task
envelope (`task_id`, `status`, `success`, `message`, …) with the fields named
here under `data`. Failure is only `Error: <message>`. After 300 s the bridge
returns `Error: Task timed out after 300000ms (task_id: …)`: that ends the
wait, not the work, and a write may still save. Query before you retry.
Skill `ue-tasks` covers the envelope and the task tools.

## Background in the plugin

`unreal_get_ue_context` with `category: "enhanced_input"` serves the plugin's
notes on the Enhanced Input classes, trigger and modifier types, key names
and the game code that adds a mapping context. Its "MCP Tool Operations"
example passes `name` to the create operations, which this tool does not
read. The plugin's `Resources/domains/debug.md` covers driving Input Actions
during play with `gameplay_debug`.

## See also

- `ue-blueprints`: the `EnhancedInputAction` node and the nodes that add a
  mapping context.
- `ue-game-framework`: the level's pawn and controller classes, and the
  input mode of a running PIE session; its section `playable-character` has
  the whole sequence from game mode to working input.
- `ue-pie-testing`: injecting actions in PIE, and the input monitor.
- `ue-widgets`: input handled by the UI.
- `unreagent-iterate`: starting and restarting the editor.
