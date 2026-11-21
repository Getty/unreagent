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
| `asset_import` | FBX import, batch import, export, reimport, source-file info |

Actors placed in a level are the subject of `ue-level-actors`, and levels of
`ue-levels`.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor.

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
before it uses them:

- it rejects an empty path, one longer than 512 characters, and one containing `..`;
- it rejects any of `` < > | & ; ` $ ( ) { } [ ] ! * ? ~ ``;
- it rejects anything under `/Engine/` or `/Script/`.

The errors begin with "Blueprint path" even for assets that are not Blueprints,
for example `Cannot access engine or script Blueprints`. Engine content can
still be found with `asset_search`.

Some paths are not checked at all:

- `asset_paths` in `delete`, `migrate` and `get_hashes`;
- `new_path` in `rename`;
- `dest_path` and `new_name` in `duplicate`;
- both paths in `set_preview_mesh`.

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

### list_assets

Lists one folder. It is the quick alternative to `asset_search` when you know
the folder.

| Parameter | Default | Meaning |
|---|---|---|
| `directory` | `/Game/` | the folder |
| `class_filter` | — | case-insensitive substring of the class name, such as `AnimMontage` |
| `recursive` | `false` | include subfolders |
| `limit` | `25` | 1 to 1000, counted after the class filter. There is no `offset` |

The result holds `directory`, `count`, `total_found` and `assets[]` (`name`,
`path` as an object path, `class`, and `package` as a package path).
`total_found` counts the whole folder **before**
the class filter, so it does not tell you how many matches remain. Use
`asset_search` when you need to page.

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
| `TEnumAsByte` enum | its numeric value, as a JSON number. A name string fails |
| `bool` | a JSON boolean |
| `FString`, `FName` | a string |
| `FVector`, `FRotator`, `FLinearColor` | an object: `{x, y, z}`, `{pitch, yaw, roll}`, `{r, g, b, a}`. Pass every key, because nothing is defaulted, not even `a` |
| soft object or soft class reference | a path that loads. A path that does not load gives `Unsupported property type for: <path>`, not a load error |
| `TMap` | a JSON object. It replaces the whole map |

Some types cannot be set here at all:

- `TArray`;
- other structs, and the UE text form `"(X=1,…)"`;
- `enum class` properties.

All of these fail with `Unsupported property type for: <path>`. So does any
value whose JSON type does not fit, such as a name for a `TEnumAsByte`.
`set_property` (skill `ue-level-actors`) accepts more value forms, but for
placed actors only.

Other errors: `Failed to load asset: <path>`, `Property not found: <part> on
<class>`, `Cannot index without preceding array property`, `Property is not an
array, cannot use index`, `Array index 3 out of bounds (size: 2)`, `Nested object
is null: <part>`, `Object property value must be a string path`, `Failed to load
object: <path>`.

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

### rename

| Parameter | Meaning |
|---|---|
| `asset_path` | required |
| `new_name` | the new asset name |
| `new_path` | the new folder, such as `/Game/Props`. At least one of `new_name` and `new_path` is required |

The rename goes through the editor's asset tools. The header says it leaves a
redirector at the old path. The result holds `old_path`, `new_path` (folder
plus name) and `success`. Errors: `At least one of new_name or new_path is
required`, and `Failed to rename asset <path> to <folder>/<name>`.

### duplicate

| Parameter | Meaning |
|---|---|
| `asset_path` | required |
| `dest_path` | required. The folder of the copy |
| `new_name` | **required by the code**, although the tool's description calls it optional |

The result holds `success`, `source_path` and `new_asset_path`. A failure gives
`Failed to duplicate asset <path> to <folder>/<name>`. The plugin's
`domains/assets.md` notes that duplicating an Animation Blueprint leaves stale
class references.

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

### migrate

Copies packages into another project's `Content` folder, without a prompt.

| Parameter | Default | Meaning |
|---|---|---|
| `asset_paths` | required | package paths |
| `target_content_dir` | required | absolute path on disk to the other project's `Content` folder. It must exist |
| `include_dependencies` | `true` | copy what the packages depend on as well |
| `overwrite` | `true` | overwrite existing assets at the target. `false` skips them |

**The result always says success.** The engine call returns nothing to check, so
`migrated_packages` is the list you asked for, not a confirmation. Check the
target folder on disk. Errors: `Target directory does not exist: <dir>`,
`Missing required parameter: asset_paths (non-empty array of package paths)`,
`No valid package paths found in asset_paths array`.

### create_folder, rename_folder, delete_folder

| Operation | Parameters | Notes |
|---|---|---|
| `create_folder` | `folder_path` | a folder that already exists is a success, with `already_existed: true`. Error: `Failed to create folder: <path>` |
| `rename_folder` | `folder_path`, `new_folder_path` | moves the folder with its assets. Errors: `Source folder does not exist: <path>`, `Failed to rename folder <a> to <b>` |
| `delete_folder` | `folder_path` | errors: `Folder does not exist: <path>`, `Failed to delete folder: <path> (must be empty — delete contained assets first)` |

**Treat `delete_folder` as a recursive delete.** The toolkit's text says the
folder has to be empty. The engine function it calls,
`UEditorAssetLibrary::DeleteDirectory`, is documented in UE as deleting the
assets inside as well. List the folder with `list_assets` and `recursive:
true` before you call it.

### open_editor and set_preview_mesh

`open_editor` takes `asset_path` and opens the asset's editor. The result holds
`asset_path`, `asset_class` and `opened`. Errors: `AssetEditorSubsystem not
available.`, `Failed to open editor for: <path>`.

`set_preview_mesh` takes `asset_path`, an animation asset, and `mesh_path`, a
skeletal mesh. It sets the preview mesh, marks the animation dirty, and reopens
the animation's editor if it was open. The result holds `asset_path`,
`mesh_path`, `mesh_name` and `editor_reopened`. Errors: `Asset is not an
animation: <path> (<class>)`, `Failed to load skeletal mesh: <path>`.

### get_hashes

Detects changes cheaply, without loading the assets.

| Parameter | Meaning |
|---|---|
| `asset_paths` | required. An array of **package paths** |

Each entry in `assets[]` holds `path`, `hash`, `file_size`, `mtime` (ISO 8601)
and `is_dirty`, or `error: "file_not_found"`. The hash is built from the file
size, the modification time and the dirty flag. It is not a content hash,
although the tool's description calls it one. Compare the hashes of one asset
over time: an unchanged hash means the file's size and modification time and
the package's dirty flag are as they were. Two different assets with equal
hashes say nothing about their content.

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

`operation` is required and is lowercased: `import`, `batch_import`, `export`,
`reimport` or `get_source`. An unknown one gives `Unknown operation: '<op>'.
Valid: import, batch_import, export, reimport, get_source`.

File paths in this tool are paths on the machine that runs the editor.

The parameter names are `fbx_path` and `dest_path`. The plugin's
`domains/assets.md` shows an example with `file_path` and `destination`, but the
code does not read those names.

### import

| Parameter | Default | Meaning |
|---|---|---|
| `fbx_path` | required | the FBX file on disk |
| `dest_path` | required | content folder for the result, such as `/Game/Meshes` |
| `mesh_type` | `auto` | `auto`, `static`, `skeletal` or `animation` |
| `skeleton_path` | — | a Skeleton or a SkeletalMesh. Required for `skeletal` and `animation`, and ignored for `auto` |
| `import_materials` | `true` | import the FBX's materials |
| `import_textures` | `true` | import the FBX's textures |
| `import_animations` | `false` | import animations alongside a mesh. `static` forces `false`, and `animation` forces `true` |
| `generate_collision` | `true` | static mesh option |
| `combine_meshes` | `false` | static mesh option |
| `normal_import` | `import` | static mesh option: `compute`, `mikk` (normals and tangents), or anything else for imported normals |
| `replace_existing` | `true` | overwrite an asset of the same name in `dest_path` |
| `save` | `true` | save the imported assets |
| `custom_sample_rate` | `0` | animation sample rate. 0 means the default |
| `snap_to_frame_boundary` | `false` | animation option |

`auto` starts from the static-mesh options and leaves it to the engine's FBX
factory to switch to a skeletal mesh when the file holds one. For a skeletal
mesh that must share an existing skeleton, use `mesh_type: "skeletal"` with
`skeleton_path`.

The options above belong to the legacy FBX factory. The plugin's
`domains/assets.md` says UE 5.7 imports through Interchange. Whether every
option still applies there cannot be seen in this code: check the result.

The result holds `imported_assets[]` (`name`, `path`, `class`), and the message
is `Imported 2 asset(s) from SM_Rock`.

| Error | Meaning |
|---|---|
| `FBX file not found: <path>` | no file at `fbx_path` |
| `Invalid mesh_type: '<type>'. Valid: auto, static, skeletal, animation` | the type is misspelt |
| `skeleton_path is required for mesh_type '<type>'` | skeletal or animation without a skeleton |
| `Asset not found: <path>` | `skeleton_path` did not load |
| `Could not get skeleton from: <path>` | `skeleton_path` is neither a Skeleton nor a SkeletalMesh with one |
| `Import failed for: <path>` | the import produced no assets |

### batch_import

Imports every file with one extension from one folder. It takes the import
parameters above, except that the folder is given in `fbx_directory`, and it
adds `file_pattern`, default `*.fbx`.

- Only the extension after the last dot of `file_pattern` counts. `Hero_*.fbx`
  imports every `.fbx` file, not only those starting with `Hero_`.
- Subfolders are not searched.
- **No matching file gives a bare `Error: ` with an empty message.** The
  importer puts its `No .<ext> files found in: <dir>` text in a field this
  operation does not read. The toolkit does not check that `fbx_directory`
  exists, so a missing folder probably looks the same. Check `fbx_directory`
  and `file_pattern`.
- **Any failed file turns the whole call into an error**, `Error: Batch import:
  5 succeeded, 1 failed, 6 total`. The per-file `results[]` is dropped with it.
  To see what did land, run `asset_search` with `path_filter` set to
  `dest_path`. When every file succeeds, `results[]` is in the data, alongside
  `success_count`, `fail_count` and `total`.

### export

| Parameter | Meaning |
|---|---|
| `asset_path` | required |
| `output_path` | required. A file path on disk. Its folder is created, and its extension picks the exporter |

`.fbx` exports a mesh or an animation. The plugin's `domains/tooling.md`
describes `.t3d` as a text dump, for reading properties no tool exposes. The
result holds `asset_path`, `output_path` and `asset_class`. Errors: `Asset not
found: <path>`, `Export failed for <path>. Check that the asset type supports
FBX export.`

### reimport and get_source

`get_source` takes `asset_path` and reports where the asset was imported from:
`asset_path`, `asset_class`, `has_import_data`, `source_files[]` (`path` and
`exists`) and `can_reimport`. Import data is read for static meshes, skeletal
meshes and animation sequences only. The tool's description promises
timestamps, but none are returned.

`reimport` reimports an asset from its source file:

| Parameter | Meaning |
|---|---|
| `asset_path` | required |
| `new_source_path` | a different source file, which must exist |
| `import_rotation` | `{pitch, yaw, roll}` in degrees. Pass all three |
| `import_translation` | `{x, y, z}`. Pass all three |
| `import_uniform_scale` | a scale override. A value of 0 or below is ignored |

The overrides are applied to the asset's FBX import data, or to its Interchange
generic-assets pipeline. The result holds `asset_path`, `asset_class` and
`new_source_path`.

| Error | Meaning |
|---|---|
| `New source file not found: <path>` | `new_source_path` does not exist |
| `Cannot update source path — asset <path> has no AssetImportData` | the asset was not imported, or its type is not one of the three above |
| `Cannot apply transform overrides — asset <path> has no GenericAssetsPipeline in its Interchange data` | the overrides have nowhere to go |
| `Cannot apply transform overrides — asset <path> import data type <class> is not supported` | as above, for other import data |
| `Asset cannot be reimported: <path>. No source file found.` | the engine finds no source to reimport from |
| `Reimport failed for: <path>` | the engine's reimport failed |

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

**Import a character**

1. `asset` `{"operation": "create_folder", "folder_path": "/Game/Characters/Hero"}`.
2. `asset_import` `{"operation": "import", "fbx_path": "D:/Art/Hero.fbx", "dest_path": "/Game/Characters/Hero", "mesh_type": "skeletal", "skeleton_path": "/Game/Characters/Shared/SK_Base"}`.
3. Note the skeletal mesh in `imported_assets`. Import the animations with
   `mesh_type: "animation"` and the same skeleton.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Cannot access engine or script Blueprints` | an `/Engine/` or `/Script/` path passed to `asset` | engine content is read-only through this tool |
| `asset_search` returns nothing for a class you know exists | none of the four lookups resolved the short class name | pass `/Script/<Module>.<Class>` |
| `Property not found: MaterialInterface on SkeletalMesh` | the path tried to enter an array of structs | not reachable through `set_asset_property` |
| the editor crashes after `set_asset_property` | the path probably ended in an array index | never end the path with an index |
| `Unsupported property type for: …` | an array, an `enum class`, an unlisted struct, or the wrong JSON type (such as a name for a `TEnumAsByte`) | check the JSON type. Arrays and `enum class` properties cannot be set here |
| `Deleted 0 of 1 assets` | the engine deleted nothing. The toolkit does not say why, and a reference to the asset is a likely cause (unverified) | `asset_referencers`, then decide on `force` |
| `save_all` success with `failed_count` above 0 | some packages did not save | retry them with `save_asset` and read its error |
| `Error: Batch import: … failed …` | at least one file failed, and the per-file list is gone | `asset_search` on `dest_path` shows what landed |
| a bare `Error: ` from `batch_import` | no file matched, or probably a folder that does not exist | check `fbx_directory` and `file_pattern` |
| `Missing required parameter: new_name` from `duplicate` | the code requires it | pass `new_name` |

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
