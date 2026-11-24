# enhanced_input: when a call fails

"Check that they persist" is in the skill itself, under "Triggers and
modifiers".

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
