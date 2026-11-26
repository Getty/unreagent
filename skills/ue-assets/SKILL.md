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
| `asset_search` | finding assets by class, folder and name (read-only) |
| `asset` | 15 operations: info, listing, properties, saving, rename, duplicate, delete, migrate, folders, editors |
| `asset_dependencies` | what an asset uses (read-only) |
| `asset_referencers` | what uses an asset (read-only) |
| `asset_import` | FBX import, batch import, export, reimport, source-file info. `retarget` has an animation-only FBX import of its own (skill `ue-retarget`, section `import`) |

Placed actors are the subject of `ue-level-actors`, levels of `ue-levels`,
Blueprint internals of `ue-blueprints`.

This skill was written from the toolkit's C++ source; engine behaviour marked
"engine source" was read in the UE 5.8.4 source. None of it has been
exercised against a running editor.

This skill has six sections:

| Section | Holds | Load it |
|---|---|---|
| `workflows` | which tool for which job (and when Python), naming conventions, and end to end: batch import and verify, move or rename and redirectors, find unused assets and delete them, audit dependencies, bulk property edits, migrate | before a task of more than one call, or when no tool seems to fit |
| `properties` | `set_asset_property` in full: the path walk, the value per property type, what cannot be set, its errors | before you call `set_asset_property` |
| `operations` | the `asset` operations not described here: `list_assets`, `rename`, `duplicate`, `migrate`, `create_folder`, `rename_folder`, `delete_folder`, `open_editor`, `set_preview_mesh`, `get_hashes` | before you call one of them |
| `import` | `asset_import` in full: every parameter with its default, Interchange and `mesh_type: "auto"`, errors, importing a character | before you call `asset_import` |
| `json-types` | how the tools read a boolean parameter, and a `set_asset_property` value, of the wrong JSON type | when a call behaved as if you had sent another value |
| `failures` | the failure table (what you see, what it means, what to do) and the path checks of `asset` | when a call returns an error or a result you did not expect |

## Before the first call

- **Tool names.** The bridge lists each tool as `unreal_<name>`, for example
  `unreal_asset`, and your client may add its own prefix. This skill uses the
  bare names, as the plugin's errors do.
- **Result shape.** Success is the message, a blank line, then JSON. By
  default the bridge goes through the task queue, so that JSON is a task
  envelope (`task_id`, `status`, `success`, `message`, …), and the tool's own
  fields, the ones named in this skill, sit under its `data` key. Failure is
  `Error: <message>` with no data. Skill `ue-tasks` covers the task tools.
- **No undo.** No tool here opens an editor transaction. `delete`, `migrate`,
  `rename`, the folder operations and an overwriting import are final.
- **Timeouts.** After 300 s the bridge returns `Error: Task timed out after
  300000ms (task_id: …)`. That ends the wait, not the work: follow the task up
  (`ue-tasks`) and read the state back before you start it again. A large
  import, a `save_all` over many packages and a big `migrate` each run as one
  call; run them as skill `ue-tasks`, "A 30 s timeout can crash the editor",
  describes.

## What must not go wrong

1. **Edits are in memory until saved.** `set_asset_property`,
   `set_preview_mesh` and the other tools' edits only mark packages dirty.
   Save with `asset` `save_all` and read `failed_count`; a partial failure is
   still a success. Never the console command `SaveAll`: it opens a dialog.
2. **Some operations save on their own.** `rename` and `rename_folder` save the
   moved assets and every referencer they fix up (engine source); an import
   saves unless `save: false`.
3. **A non-forced `delete` is all or nothing** (engine source): one referenced
   asset in the batch and nothing is deleted, reported as success with
   `Deleted 0 of N assets`. `force: true` deletes anyway and leaves the
   referencers broken; force-deleting a Blueprint also deletes its placed
   instances.
4. **`delete_folder` force-deletes everything inside, recursively** (engine
   source), whatever its error text says about empty folders.
5. **Never end a `set_asset_property` path with an array index** (`Tags.0`):
   it writes into the asset object itself and can crash the editor.
6. **`mesh_type: "auto"` imports static meshes only** when Interchange handles
   FBX, the engine default (engine source). Pass `mesh_type` explicitly.
7. **`asset_search` with an unresolved short class name finds nothing**, with
   no error. Use `/Script/<Module>.<Class>` for classes outside Engine,
   CoreUObject and Niagara.
8. **`.uasset` files are binary**: never read or edit them as files (skill
   `unreagent-files`).

## Asset paths

| Form | Example |
|---|---|
| package path | `/Game/Meshes/SM_Rock` |
| object path | `/Game/Meshes/SM_Rock.SM_Rock` |
| Blueprint class path | `/Game/BP/BP_Door.BP_Door_C` |
| content folder | `/Game/Meshes` |

- The `asset` operations that take `asset_path` load it as given, then retry
  with `_C` appended. Package and object paths both work. Pass a Blueprint by
  its asset path, without `_C`. Aliases: `blueprint_path` and `path` for
  `asset_path`, `asset_class` for `class_filter`.
- `migrate` and `get_hashes` take **package paths** in `asset_paths`;
  `delete` loads each entry like `asset_path`.
- `asset_dependencies` and `asset_referencers` take either form.
- `asset_import` takes a content folder in `dest_path`, an asset path in
  `asset_path`, and paths on the editor's machine for files.
- `asset` rejects `/Engine/` and `/Script/` paths and some characters
  (section `failures`); `asset_search` still finds engine content.

## asset_search

Finds assets through the asset registry, without loading them.

| Parameter | Default | Meaning |
|---|---|---|
| `class_filter` | — | class of the assets. Subclasses match too: `Blueprint` includes Widget and Animation Blueprints |
| `path_filter` | `/Game/` | folder, searched recursively. `""` searches everywhere, engine and plugin content included |
| `name_pattern` | — | case-insensitive substring of the asset name. `search_term` is an alias, used only without `name_pattern` |
| `limit` | `25` | 1 to 1000 |
| `offset` | `0` | matches to skip |

A short `class_filter` is looked up as `/Script/Engine.<name>`, then
`/Script/CoreUObject.<name>`, then `/Script/Niagara.<name>`, then as given.
That covers `StaticMesh`, `SkeletalMesh`, `Skeleton`, `Material`,
`MaterialInstanceConstant`, `Texture2D`, `SoundWave`, `AnimSequence`,
`AnimMontage`, `DataTable`, `Blueprint`, `World` (levels), `ObjectRedirector`
and `NiagaraSystem`. Others need the full path, such as
`/Script/UMGEditor.WidgetBlueprint` or `/Script/EnhancedInput.InputAction`.

The result holds `assets[]` (`path` as an object path, `name`, `class`,
`package_path`), `count`, `total`, `offset`, `limit`, `hasMore` and, while
more remain, `nextOffset`. **`package_path` is the content folder**, not a
package path; the package path is `path` before the dot. An empty result is a
success: `No assets found matching the search criteria`.

## The asset tool

`operation` is required and lowercased. Aliases: `get_info`, `info` →
`get_asset_info`; `search`, `find`, `list` → `list_assets`; `property` →
`set_asset_property`; **`save` → `save_all`**, not `save_asset`. An unknown
operation gives `Unknown operation: '<op>'. Valid: …`, with a "Did you mean"
when one is close.

**get_asset_info** — `asset_path`, `include_properties` (default `false`).
The result holds `name`, `path`, `class`, `package` and `is_dirty`; a skeletal
mesh adds `materials[]` (`slot`, `slot_name`, `material`). With
`include_properties`, `properties[]` lists each editable property's `name`,
`type` (`float`, `integer`, `bool`, `string`, `name`, `struct:<S>`,
`object:<C>`, `array`, `other`) and `editable`: names and types, not values.
A Blueprint reports the Blueprint object's properties, not its class defaults.

**set_asset_property** — `asset_path`, `property` (a dot path), `value`. Send
the JSON type the property has: a wrong type is often a wrong write, not an
error. Arrays, most structs, `enum class` and `FText` cannot be set. Load
section `properties` first.

**save_asset** — `asset_path`, `save` (default `true`), `mark_dirty`
(default: the opposite of `save`). Saves one package without a dialog and
clears its dirty flag. `{"save": false}` only marks it dirty. The result holds
`asset_path`, `saved` and `marked_dirty`; check `saved`, because a wrong JSON
type for `save` skips the save without an error (section `json-types`).
Failure: `Failed to save asset: <path>`.

**save_all** — no parameters. Saves every dirty package except transient ones
and those under `/Temp/` or `/Engine/`: levels, assets, plugin content. The
result holds `saved_count`, `failed_count`, `total_dirty` and, on failures,
`failed_packages[]`; the message is `Saved 12/12 dirty packages`. Only when
every package fails is it an error (`All 3 packages failed to save`, list
dropped). A never-saved level lives under `/Temp/` and is skipped: give it a
path with `open_level save_as` (skill `ue-levels`).

**delete** — `asset_paths` (non-empty array), `force` (default `false`). Run
`asset_referencers` on each asset first. The result holds `deleted_count`,
`requested_count`, `requested_paths` (the paths that loaded), `not_found` and
`success` (true when at least one was deleted). Compare `deleted_count` with
`requested_count`. Errors: `Missing required parameter: asset_paths
(non-empty array)`, `No valid assets found to delete`. Deleting the open
level opens a modal dialog in the editor (engine source): open another level
first.

The other ten operations: section `operations`.

## Dependencies and referencers

| Parameter | Default | Meaning |
|---|---|---|
| `asset_path` | required | a package path or an object path |
| `include_soft` | `true` | `false` lists hard references only |
| `limit` | `25` | 1 to 1000 |
| `offset` | `0` | entries to skip |

- **One level only.** To walk the tree, call again for each entry.
- `/Script/` and `/Engine/` packages are left out; plugin content is not.
- Each entry holds `path` (a package path), plus `class` and `name` when the
  registry knows the package. A level that places the asset shows up as a
  referencer; in a World Partition level the referencer is probably an
  actor's own package under `/Game/__ExternalActors__/` rather than the
  `.umap` (unverified). A redirector pointing at the asset probably shows
  up as a referencer too (unverified).
- The result holds `asset_path`, `dependencies[]` or `referencers[]`,
  `count`, `total`, `offset`, `limit`, `hasMore`, `include_soft` and, while
  more remain, `nextOffset`. `Asset not found: <path>` means the registry has
  no asset there. An unreferenced asset gives `No referencers found for
  '<name>' - this asset appears unused`, which does not see references from
  C++ or config (section `workflows`, "Find unused assets").

## asset_import

Five operations: `import`, `batch_import`, `export`, `reimport`,
`get_source`. **Load section `import` before you call it.** The parameter
names are `fbx_path`, `fbx_directory` and `dest_path`; the plugin's
`domains/assets.md` example uses `file_path` and `destination`, which the code
does not read. `replace_existing` defaults to `true` and overwrites finally.
In `batch_import`, no matching file gives a bare `Error: `, and one failed
file turns the whole call into an error without the per-file list.

## Typical sequences

**Find an asset and change it**

1. `asset_search` `{"class_filter": "MaterialInstanceConstant", "name_pattern": "Rock"}`.
2. `asset` `{"operation": "get_asset_info", "asset_path": "<path>", "include_properties": true}`.
3. `asset` `{"operation": "set_asset_property", "asset_path": "<path>", "property": "<name>", "value": …}`.
4. `asset` `{"operation": "save_all"}`. Check `failed_count`.

**Delete an asset safely**

1. `asset_referencers` `{"asset_path": "<path>"}`: anything listed breaks if
   you force the delete.
2. `asset` `{"operation": "delete", "asset_paths": ["<path>"]}`.
3. Compare `deleted_count` with `requested_count`.

Importing, moving with redirectors, finding unused assets, auditing
dependencies: section `workflows`.

## See also

- `ue-level-actors`: placed actors, and `set_property` for their values.
- `ue-levels`: opening and saving levels, `open_level save_as`.
- `ue-blueprints`: Blueprint classes, defaults and variables.
- `ue-python`: `execute_script`, for what no asset tool does.
- `ue-viewport`: `capture_viewport` to look at an imported asset.
- `unreagent-files`: why `.uasset` files must never be edited as files.
- The plugin's background notes on asset loading and references, served by
  `unreal_get_ue_context` with category `assets`
  (`Plugins/UELLMToolkit/Resources/mcp-bridge/contexts/assets.md`), and its
  field notes on imports and asset operations in
  `Plugins/UELLMToolkit/Resources/domains/assets.md`.
