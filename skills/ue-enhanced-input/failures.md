# enhanced_input: when a call fails or play does not react

Per-operation errors in full are in section `operations`; trigger and
modifier errors in section `triggers-modifiers`.

## Errors

| Error | Cause | Fix |
|---|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or `""` | `action_name` / `context_name`, not `name` |
| `Unknown operation: <op>. Valid operations: …` | not one of the eight; no aliases exist | — |
| `Failed to load InputAction: <path>` / `Failed to load InputMappingContext: <path>` | nothing of that type at the path (or something of another type) | `asset_search` for the name (skill `ue-assets`) |
| `Blueprint path …` / `Cannot access engine or script Blueprints` | the shared path check (section `operations`, "Paths") | a `/Game/…` package path |
| `Invalid value_type: <v>. …` | case-sensitive names | `Digital`, `Axis1D`, `Axis2D`, `Axis3D` |
| `Invalid key name: <k>. …` | not an engine key name or an alias | section `key-aliases`; the error's own alias list is incomplete |
| `Missing or invalid mapping_index parameter` | `remove_mapping` without a non-negative number | index from `query_context` |
| `Invalid mapping_index <i>. Context has <n> mappings …` | past the end | `query_context` again |
| `No mapping found for action '<a>' in context '<c>'. …` | no mapping of that action yet | `add_mapping` first |
| `Mapping at index <i> is for action '<x>', not '<a>'` | index and action disagree | `query_context` |
| `Failed to save asset: <package>` | the change was made in memory, the engine's `SaveAsset` refused (a read-only file is a likely cause, unverified) | `query_context` / `query_action` **before** retrying, so the change is not made twice; then `asset` `save_asset` (skill `ue-assets`) |
| `Error: Task timed out after 300000ms (task_id: …)` | the bridge stopped waiting; the work may still finish | query before retrying (skill `ue-tasks`) |

## In play

| What you see | What it means | What to do |
|---|---|---|
| the key does nothing in PIE | the context is not added to the player | section `runtime-setup`, step 1; check the Blueprint that should add it, and that it compiles |
| an injected action works, the key does not | injection skips the key mapping and the context (`EnhancedPlayerInput.cpp:646-667`, UE 5.8.4; skill `ue-pie-testing`) | `query_context`: the key, the action, the context really added |
| nothing reacts, even an injected action | no `EnhancedInputAction` node / `BindAction` for it, or it lives on an actor that receives no input | section `runtime-setup`, step 2 |
| input works until a menu opened, then never again | input mode left at `ui_only` | `Set Input Mode Game Only` when the menu closes; `game_framework` `set_input_mode` to test |
| nothing works while paused, not even "unpause" | the action lacks `Trigger when Paused` | set it on the Input Action in the editor |
| jump fires many times per press | reacting on `Triggered` with no trigger: Triggered fires every frame while held | react on `Started`, or `add_trigger` `Pressed` (but see the next row) |
| a trigger or modifier is gone after an editor restart | the tool's triggers and modifiers are not saved with the asset (section `triggers-modifiers`, "Why they do not survive a reload") | set it in the editor by hand, and tell the user |
| WASD moves diagonally or only on one axis | Swizzle/Negate on the wrong mapping: without `mapping_index` every call changes the first mapping of the action | section `triggers-modifiers`, WASD pattern, with `mapping_index` per key |
| the same key fires two actions, or one is blocked | two contexts map the key; the higher priority one consumes it | adjust the priorities in the code that adds them, or the action's `Consume Input` |
| `remove_mapping` removed a different mapping, or the list order changed | the engine removes the first mapping with that key and action, and moves the last mapping into its slot | `query_context` before every index-based call |
| a mapping exists twice | `add_mapping` was repeated (no duplicate check) | `remove_mapping` one of them |
| a context or action was replaced by an empty one | a create operation ran on a name that existed | restore from source control; check with `asset_search` before creating |
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (skill `unreagent-iterate`) |
