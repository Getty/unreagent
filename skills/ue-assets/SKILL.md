---
name: ue-assets
description: >
  Find, inspect, edit, save, move, delete and import Content Browser assets
  through the UE LLM Toolkit's asset tools (asset, asset_search, asset_import,
  asset_dependencies, asset_referencers). Use before working on assets, before
  saving editor changes to disk, or when one of these tools returns an error.
toolkit_tools: [asset, asset_search, asset_import, asset_dependencies, asset_referencers]
---

# Assets

Five tools of the UE LLM Toolkit plugin work on Content Browser assets:

| Tool | For |
|---|---|
| `asset_search` | finding assets by class, folder and name |
| `asset` | 15 operations: info, listing, properties, saving, rename, duplicate, delete, migrate, folders, editors |
| `asset_dependencies` | what an asset uses |
| `asset_referencers` | what uses an asset |
| `asset_import` | FBX import, batch import, export, reimport, source-file info. `retarget` has an animation-only FBX import of its own (skill `ue-retarget`, section `import`) |

Actors placed in a level are the subject of `ue-level-actors`, and levels of
`ue-levels`.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

This skill has four sections:

| Section | Holds | Load it |
|---|---|---|
| `operations` | the ten `asset` operations not described here: `list_assets`, `rename`, `duplicate`, `migrate`, `create_folder`, `rename_folder`, `delete_folder`, `open_editor`, `set_preview_mesh`, `get_hashes` | before you call one of them |
| `import` | `asset_import` in full: `import`, `batch_import`, `export`, `reimport`, `get_source`, their errors, and the sequence for importing a character | before you call `asset_import` |
| `json-types` | how the `asset` tool reads a boolean parameter, and a `set_asset_property` value, of the wrong JSON type | when a call behaved as if you had sent another value, or a write went through with a value you did not send |
| `failures` | the failure table (what you see, what it means, what to do), the path checks of `asset`, the remaining errors of `set_asset_property` | when a call returns an error or a result you did not expect |

## Before the first call

- **Tool names.** The bridge lists each tool as `unreal_<name>`, for example
  `unreal_asset`, and your client may add its own prefix. This skill uses the
  bare names, as the plugin's errors do.
- **Result shape.** Success is the message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope (`task_id`, `status`, `success`, `message`, …), and the tool's own
  fields, the ones named in this skill, sit under its `data` key. Failure is
  `Error: <message>` with no data. Skill `ue-tasks` covers the task tools.
- **No undo.** No tool here opens an editor transaction, so a change has no
  undo entry. `delete`, `migrate` and an overwriting import are final.
- **Timeouts.** After 300 s (`MCP_ASYNC_TIMEOUT_MS`) the bridge returns `Error:
  Task timed out after 300000ms (task_id: …)`. That ends the wait, not the
  work. Before you start the job again, follow the task up (`ue-tasks`). After
  an import, also read the state back with `asset_search`, `path_filter` set to
  the `dest_path`. A large import or `batch_import`, a `save_all` over many
  dirty packages and a `migrate` of many packages each run as one call and can
  hold the editor for more than 30 s; run them as skill `ue-tasks`, "A 30 s
  timeout can crash the editor", describes.

## Asset paths

| Form | Example |
|---|---|
| package path | `/Game/Meshes/SM_Rock` |
| object path | `/Game/Meshes/SM_Rock.SM_Rock` |
| Blueprint class path | `/Game/BP/BP_Door.BP_Door_C` |
| content folder | `/Game/Meshes` |

Which form each tool takes:

- The `asset` operations that take `asset_path` load it as given, then retry
  with `_C` appended. A package path and an object path both work: the tool's
  own examples use package paths. Pass a Blueprint by its asset path, without
  `_C`.
- `migrate` and `get_hashes` take **package paths** in `asset_paths`. `delete`
  loads each entry like `asset_path`.
- `asset_dependencies` and `asset_referencers` take either form.
- `asset_import` takes a content folder in `dest_path`, and an asset path in
  `asset_path`.

`asset` checks `asset_path`, `directory`, `folder_path` and `new_folder_path`
before it uses them, and rejects anything under `/Engine/` or `/Script/`.
Engine content can still be found with `asset_search`. Section `failures`
lists every check, the errors (which begin with "Blueprint path" even for
assets that are not Blueprints) and the path parameters that are not checked
at all.

`asset` also accepts `blueprint_path` and `path` as aliases for `asset_path`,
and `asset_class` for `class_filter`.

## asset_search

Finds assets through the asset registry, without loading them. It is read-only.

| Parameter | Default | Meaning |
|---|---|---|
| `class_filter` | — | class of the assets. Subclasses match too |
| `path_filter` | `/Game/` | folder, searched recursively. `""` searches everywhere, engine and plugin content included |
| `name_pattern` | — | case-insensitive substring of the asset name |
| `search_term` | — | alias for `name_pattern`, used only when `name_pattern` is absent |
| `limit` | `25` | 1 to 1000 |
| `offset` | `0` | matches to skip |

A `class_filter` without a leading `/` goes through four lookups:

1. `/Script/Engine.<name>`;
2. `/Script/CoreUObject.<name>`;
3. `/Script/Niagara.<name>`;
4. `FindObject<UClass>` on the name as given.

The first three cover `StaticMesh`, `SkeletalMesh`, `Material`,
`MaterialInstanceConstant`, `Texture2D`, `Blueprint`, `World` (levels) and
`NiagaraSystem`. What the fourth finds is up to the engine.

**If all four miss, the search silently matches nothing.** The tool falls back
to `/Script/Engine.<name>` and returns an empty result rather than an error.
For a class from any other module, pass the full class path, such as
`/Script/UMGEditor.WidgetBlueprint` or `/Script/MyGame.MyDataAsset`.

The result holds `assets[]` (`path` as an object path, `name`, `class` and
`package_path`), `count`, `total`, `offset`, `limit` and `hasMore`. While more
matches remain, it also holds `nextOffset`. **`package_path` is the content
folder**, such as `/Game/Meshes`, not a package path. The package path is
`path` before the dot: pass that to `migrate` and `get_hashes`. An empty result is a success:
`No assets found matching the search criteria`.

## The asset tool

`operation` is required and is lowercased. These aliases are accepted:

| Alias | Operation |
|---|---|
| `get_info`, `info` | `get_asset_info` |
| `search`, `find`, `list` | `list_assets` |
| `property` | `set_asset_property` |
| `save` | **`save_all`**, not `save_asset` |

An unknown operation gives `Unknown operation: '<op>'. Valid: …`, with a "Did
you mean" suggestion when one is close.

Five of the 15 operations are described below: `get_asset_info`,
`set_asset_property`, `save_asset`, `save_all` and `delete`. **Load section
`operations` before you call any of the other ten**: `list_assets`, `rename`,
`duplicate`, `migrate`, `create_folder`, `rename_folder`, `delete_folder`,
`open_editor`, `set_preview_mesh` or `get_hashes`. Three of its warnings, so
that they are not missed:

- **Treat `delete_folder` as a recursive delete.**
- **`migrate`'s result always says success.** Check the target folder on disk.
- `duplicate` needs `new_name`: it is **required by the code**, although the
  tool's description calls it optional.

### get_asset_info

| Parameter | Default | Meaning |
|---|---|---|
| `asset_path` | required | the asset |
| `include_properties` | `false` | add the asset's editable properties |

The result holds `name`, `path`, `class`, `package` and `is_dirty`. A skeletal
mesh adds `materials[]`, each entry with `slot`, `slot_name` and `material`.
With `include_properties`, `properties[]` lists each editable property with
`name`, `type` and `editable`. `type` is one of `float`, `integer`, `bool`,
`string`, `name`, `struct:<Struct>`, `object:<Class>`, `array` or `other`.

A Blueprint asset reports the properties of the Blueprint object, not the
defaults of the class it generates.

**A wrong JSON type for `include_properties` is never an error.** Send a
JSON boolean. Section `json-types` says how every other type is read (read
in the UE 5.8.4 source, not run); the same holds for `recursive` in
`list_assets` and for `save` and `mark_dirty` in `save_asset`.

### set_asset_property

Writes one property on an asset, such as a mesh, a material instance or a data
asset.

| Parameter | Meaning |
|---|---|
| `asset_path` | required |
| `property` | required. A dot path of letters, digits, `_` and `.`, at most 512 characters |
| `value` | required. A missing one gives `Missing required parameter: value` |

If the property is not found on the asset and the asset is a Blueprint, the tool
tries the Blueprint's class defaults instead. The `ue-blueprints` skill covers
Blueprint defaults in full.

After the write, the tool sends the property-changed notification and marks the
package dirty. It does not save. The result holds `asset_path`, `property` and
`modified: true`.

The path walk:

- A part names a property. If more parts follow, it must be an object
  reference, and the walk moves into the referenced object, or it must be an
  array followed by a number.
- A number indexes the array before it. For an array of objects, the walk moves
  into that element.
- **Struct members cannot be reached**:
  `Cannot navigate into struct property: <part>. Set the entire struct instead.`
- **An array of structs cannot be entered.** After `Materials.0`, the walk
  stays on the asset, so `Materials.0.MaterialInterface` looks for
  `MaterialInterface` on the mesh and fails with `Property not found`. The
  tool's own description uses that path as an example, and it does not work.
- **Never end a path with an array index, as in `Tags.0`.** At the last part,
  the code writes to the wrong address: the asset object itself, not the array
  element. It does not fail cleanly, and it can corrupt the asset in memory or
  crash the editor. Set the whole array instead, which this operation cannot do
  either (see below). For array contents on an asset, there is no safe route in
  this tool.

Values:

| Property type | Pass |
|---|---|
| object reference | an object path string. `"None"` or `""` clears it. The loaded object must be of the property's class: `Object type mismatch. Expected <class>, got <class>` |
| class reference | a class path. For a Blueprint, that is `/Game/BP/BP_Door.BP_Door_C`: the asset path loads the Blueprint, not its class, and fails the type check. `"None"` or `""` clears it |
| number | a JSON number |
| `TEnumAsByte` enum | its numeric value, as a JSON number. A name string is not refused: it is stored as 0, with no error (read in the UE 5.8.4 source, not run, while the toolkit targets UE 5.6 and 5.7; section `json-types`) |
| `bool` | a JSON boolean |
| `FString`, `FName` | a string |
| `FVector`, `FRotator`, `FLinearColor` | an object: `{x, y, z}`, `{pitch, yaw, roll}`, `{r, g, b, a}`. Pass every key, because nothing is defaulted, not even `a` |
| soft object or soft class reference | a path that loads. A path that does not load gives `Unsupported property type for: <path>`, not a load error |
| `TMap` | a JSON object. It replaces the whole map |

Some types cannot be set here at all:

- `TArray`;
- other structs, and the UE text form `"(X=1,…)"`;
- `enum class` properties.

All of these fail with `Unsupported property type for: <path>`. So does a
value the tool cannot read as the property's type. The tool's reads accept
more than the matching JSON type, though (read in the UE 5.8.4 source, not
run, while the toolkit targets UE 5.6 and 5.7): section `json-types` lists
what each property type accepts. Load it when a write went through with a
value you did not send.

So send the JSON type the property has: a wrong string in an integer or
`bool` property is a wrong write, not an error. Read the value back with
`get_asset_info` and `include_properties: true`.
`set_property` (skill `ue-level-actors`) accepts more value forms, but for
placed actors only.

The other errors of `set_asset_property` are listed in section `failures`.

### save_asset and save_all

Neither shows a dialog.

`save_asset` saves one asset's package:

| Parameter | Default | Meaning |
|---|---|---|
| `asset_path` | required | the asset |
| `save` | `true` | write the package to disk |
| `mark_dirty` | the opposite of `save` | mark the package dirty |

`{"save": false}` marks the asset dirty without saving it. The result holds
`asset_path`, `saved` and `marked_dirty`. A failed save gives `Failed to save
asset: <path>`.

**A wrong JSON type for `save` can skip the save without an error.** Both
parameters are read as `include_properties` is (section `json-types`):
`null`, an array, an object or a string such as `"maybe"` for `save` reads as
`false`: the package is not written, and it is marked dirty unless
`mark_dirty` reads as `false`. `saved` in the result says which happened;
check it.

`save_all` takes no parameters. It saves every dirty package in memory except
transient ones and those under `/Temp/` or `/Engine/`. That covers levels,
assets and plugin content alike. The result holds `saved_count`,
`failed_count`, `total_dirty` and, when some failed, `failed_packages[]`. The
message is `Saved 12/12 dirty packages`.

- **A partial failure is still a success.** Read `failed_count`, not just the
  outcome. Only when every package fails does the call return `Error: All 3
  packages failed to save`, and then the list of packages is dropped.
- **A level that was never saved lives under `/Temp/`** and is skipped. Give it
  a path with `open_level save_as` (skill `ue-levels`).

The plugin's `domains/tooling.md` recommends `save_all` after every write
operation, and warns against the console command `SaveAll`, which opens the
engine's confirmation dialog.

### delete

| Parameter | Default | Meaning |
|---|---|---|
| `asset_paths` | required | a non-empty array of asset paths |
| `force` | `false` | delete even when other assets reference them |

Run `asset_referencers` on each asset first. The toolkit hands the deletion to
the engine (`ObjectTools::DeleteObjects`, or `ForceDeleteObjects` with
`force`) and reports only the count. A forced delete probably leaves the
referencers with broken references.

- **The call succeeds even when nothing was deleted.** Compare `deleted_count`
  with `requested_count`. `success` in the data is `true` only when at least one
  asset was deleted. The message is `Deleted 0 of 2 assets`.
- `requested_paths` lists the paths that loaded, and `not_found` those that did
  not.

Errors: `Missing required parameter: asset_paths (non-empty array)`, and `No
valid assets found to delete` when none of the paths loaded.

## Dependencies and referencers

`asset_dependencies` lists what an asset uses. `asset_referencers` lists what
uses it. Both are read-only.

| Parameter | Default | Meaning |
|---|---|---|
| `asset_path` | required | a package path or an object path |
| `include_soft` | `true` | `false` lists hard references only |
| `limit` | `25` | 1 to 1000 |
| `offset` | `0` | entries to skip |

- **One level only.** Each call returns direct dependencies or direct
  referencers. The `asset_dependencies` description promises "all
  dependencies". To walk the tree, call again for each entry.
- Packages under `/Script/` and `/Engine/` are left out, so native classes and
  engine content never appear.
- Each entry holds `path` (a package path), plus `class` and `name` when the
  registry knows the package.

The result holds `asset_path`, `dependencies[]` or `referencers[]`, `count`,
`total`, `offset`, `limit`, `hasMore`, `include_soft` and, while more remain,
`nextOffset`. `Asset not found: <path>` means the registry has no asset at that
path. An asset nothing references gives `No referencers found for '<path>' -
this asset appears unused`.

## asset_import

`asset_import` has five operations: `import`, `batch_import`, `export`,
`reimport` and `get_source`. **Load section `import` before you call it**: it
has every parameter with its default, the errors, and the sequence for
importing a character. What you must know even before that:

- File paths in this tool are paths on the machine that runs the editor.
- The parameter names are `fbx_path` and `dest_path`. The plugin's
  `domains/assets.md` shows an example with `file_path` and `destination`,
  but the code does not read those names.
- `replace_existing` defaults to `true`: an import overwrites an asset of the
  same name in `dest_path`, and that is final ("No undo" above).
- A large import or `batch_import` can hold the editor for more than 30 s
  ("Timeouts" above).
- In `batch_import`, **no matching file gives a bare `Error: ` with an empty
  message**, and **any failed file turns the whole call into an error**, with
  the per-file `results[]` dropped.

## Typical sequences

**Find an asset and change it**

1. `asset_search` `{"class_filter": "MaterialInstanceConstant", "name_pattern": "Rock"}`.
2. `asset` `{"operation": "get_asset_info", "asset_path": "<path>", "include_properties": true}`.
3. `asset` `{"operation": "set_asset_property", "asset_path": "<path>", "property": "<name>", "value": …}`.
4. `asset` `{"operation": "save_all"}`. Check `failed_count`.

**Delete an asset safely**

1. `asset_referencers` `{"asset_path": "<path>"}`. Anything listed probably
   breaks if you force the delete.
2. `asset` `{"operation": "delete", "asset_paths": ["<path>"]}`.
3. Compare `deleted_count` with `requested_count`.

Importing a character: section `import`.

## When a call fails

When a call returns an error or a result you did not expect, load section
`failures`: a table from what you see to what it means and what to do, the
path checks of `asset`, and the remaining errors of `set_asset_property`.

## See also

- `ue-level-actors`: placed actors, and `set_property` for their values.
- `ue-levels`: opening and saving levels, and `open_level save_as` for a level
  that `save_all` skips.
- `ue-blueprints`: Blueprint classes, defaults and variables.
- `unreagent-files`: why `.uasset` files must never be edited as files.
- The plugin's background notes on asset loading and references, served by
  `unreal_get_ue_context` with category `assets`
  (`Plugins/UELLMToolkit/Resources/mcp-bridge/contexts/assets.md`). The
  plugin's field notes on imports and asset operations are in
  `Plugins/UELLMToolkit/Resources/domains/assets.md`.
