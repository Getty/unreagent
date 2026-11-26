# enhanced_input: every operation in full

The operation table, the must-not-get-wrong rules and the common sequence are
in the skill itself. This section has each operation's parameters, results
and errors, as read in the toolkit's `MCPTool_EnhancedInput.cpp`.

## Paths

`context_path`, `action_path`, `chord_action_path` and `package_path` take a
package path such as `/Game/Input/IMC_Default` or `/Game/Input/IA_Jump`; the
tool appends `.<Name>` itself when the path holds no `.`. The object path the
create operations return (`/Game/Input/IA_Jump.IA_Jump`) works too.

All of them pass the toolkit's Blueprint path check first, so the messages
say "Blueprint path":

| Error | Cause |
|---|---|
| `Blueprint path cannot be empty` | `package_path: ""` (a missing one takes the default) |
| `Blueprint path exceeds maximum length of 512 characters` | — |
| `Cannot access engine or script Blueprints` | starts with `/Engine/` or `/Script/` |
| `Blueprint path cannot contain path traversal sequences` | contains `..` |
| `Blueprint path contains invalid character: '<c>'` | one of `` <>|&;`$(){}[]!*?~ `` |

A path that passes but does not load as the expected type returns
`Failed to load InputAction: <path>` or
`Failed to load InputMappingContext: <path>`. That message does not tell a
missing asset from an asset of another type.

`action_name` and `context_name` are not checked at all beyond being
non-empty. Keep them to letters, digits and `_`; a `/` in a name ends up in
the package path.

## create_input_action

| Parameter | Default | Notes |
|---|---|---|
| `action_name` | required | e.g. `IA_Jump`; `name` is not read |
| `package_path` | `/Game/Input` | the folder |
| `value_type` | `Digital` | see below |

| `value_type` | Also accepted | Engine type | Value |
|---|---|---|---|
| `Digital` | `Boolean`, `Bool` | `Boolean` | on or off |
| `Axis1D` | `Float` | `Axis1D` | one float |
| `Axis2D` | `Vector2D` | `Axis2D` | two floats: a stick, WASD movement, mouse look |
| `Axis3D` | `Vector` | `Axis3D` | three floats |

The match is case-sensitive: `digital` returns
`Invalid value_type: digital. Valid types: Digital, Axis1D, Axis2D, Axis3D`.
The result has `asset_path` (object path), `name`, and `value_type` **as you
passed it** (`Bool` stays `Bool`; `query_action` reports `Digital`).

No operation changes an action's value type later, nor sets the action's own
triggers or modifiers, its "Consume Input" or "Trigger when Paused" flags.
Choose `value_type` right the first time: movement and look are `Axis2D`,
jump and interact `Digital`, a throttle `Axis1D`.

## create_mapping_context

`context_name` required, `package_path` default `/Game/Input`. The result has
`asset_path`, `name` and `mapping_count: 0`. Nothing about the context's
own settings (description, input mode filter, registration tracking) can be
set.

**Neither create operation checks for an existing asset.** Each calls the
engine's `CreatePackage` and `NewObject` with the name and saves, so it can
replace or collide with whatever is at `<package_path>/<name>`. Look first:
`asset_search` with `path_filter` the folder and `name_pattern` the name
(skill `ue-assets`); it matches substrings and includes subfolders, so the
name is taken only when an entry has exactly that `name` and `package_path`.
Other errors: `Failed to create package: <path>`,
`Failed to create InputAction`, `Failed to create InputMappingContext`,
`Failed to save asset: <package>`.

## add_mapping

`context_path`, `action_path`, `key`, all required. Calls the engine's
`UInputMappingContext::MapKey`, which appends a mapping with no triggers and
no modifiers. The result has `context_path`, `action_path` (object paths),
`key` as you passed it (an alias stays an alias; `query_context` shows the
engine name) and `mapping_index`, the new last position.

There is no duplicate check: the same key and action twice gives two
mappings. `key` rules and aliases: section `key-aliases`. Errors: the path
errors above, `Invalid key name: <k>. Use standard UE key names …`.

## remove_mapping

`context_path` and `mapping_index` required. A missing, non-numeric or
negative index returns `Missing or invalid mapping_index parameter`; one at
or past the end returns
`Invalid mapping_index <i>. Context has <n> mappings (0-<n-1>)`.

What it does: reads the action and key at `mapping_index`, then calls the
engine's `UnmapKey(action, key)`. That removes the **first** mapping with
that action and key, and fills the hole by moving the **last** mapping into
it (`RemoveAtSwap`, `InputMappingContext.cpp`, read in the UE 5.8.4 source).
So:

- with the same key and action mapped twice, the lower index goes, whatever
  index you passed;
- the former last mapping now has the removed index. Every index you held
  is stale: run `query_context` again.

The result has `context_path`, `removed_index` (as passed), `removed_key`
(engine name) and `removed_action` (asset name).

## add_trigger and add_modifier

`context_path`, `action_path` and `trigger_type` / `modifier_type` required;
`mapping_index` optional, plus the type's settings (section
`triggers-modifiers`). Which mapping changes:

- with `mapping_index`: that one, and it must belong to `action_path`, or
  `Mapping at index <i> is for action '<x>', not '<a>'`; past the end:
  `Invalid mapping_index <i>. Context has <n> mappings.`
- without it: the first mapping of `action_path`, or
  `No mapping found for action '<a>' in context '<c>'. Use query_context to see available mappings.`

So for a WASD move action with four mappings, pass `mapping_index` for each
key; without it all four calls change the `W` mapping.

Each call appends one more. Results: `context_path`, `action_path`,
`trigger_type` or `modifier_type`, and `trigger_count` or `modifier_count`
of that mapping. **Read "Triggers and modifiers do not survive a reload" in
the skill before you rely on either.**

## query_context

`context_path`. Result: `name`, `path`, `mapping_count`, and `mappings`, each
with `index`, `key` (engine name), `action` (asset name, `None` when the
mapping has no action), `action_path` (absent when `None`), and `triggers`
and `modifiers`, each entry `{type}` with the engine class name, such as
`InputTriggerHold` or `InputModifierNegate`. Settings of a trigger or
modifier (hold time, dead zone values) are not shown.

It reads the context's default mappings only (`GetMappings`). From UE 5.7 a
context can also hold per-profile overrides (`MappingProfileOverrides`, read
in the UE 5.8.4 header); the tool neither shows nor changes them.

## query_action

`action_path`. Result: `name`, `path`, `value_type` (`Digital`, `Axis1D`,
`Axis2D`, `Axis3D` or `Unknown`), and the action's own `triggers` and
`modifiers` (`{type}` entries). Those apply to every mapping of the action
and are set in the editor's Input Action asset, not by this tool.
