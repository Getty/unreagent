# Asset workflows, end to end

Each workflow names the calls in order and how to check the result through the
tools. The parameters and results are in the main skill and in sections
`operations`, `import` and `properties`. Engine facts marked "engine source"
were read in the UE 5.8.4 source, not run; the toolkit targets UE 5.6 and 5.7.

## Which route

| You want to | Use | Not |
|---|---|---|
| find assets by class, folder or name | `asset_search` (pages, counts the total) | `list_assets`, which cannot page and counts before its class filter |
| look at one folder quickly | `asset` `list_assets` | — |
| change a property of an asset | `asset` `set_asset_property` | `set_property` (placed actors only) |
| change a Blueprint's defaults, variables, components | `blueprint_query` / `blueprint_modify` (skill `ue-blueprints`) | `set_asset_property`, which reaches only what the CDO fallback finds |
| set an array, a struct, an `enum class` or `FText` on an asset | Python through `execute_script` (skill `ue-python`) | `set_asset_property`, which cannot |
| import FBX meshes and animations | `asset_import` | — |
| import textures, sounds, glTF, USD | not a documented case of `asset_import` (its whole contract is FBX). Python through `execute_script` (skill `ue-python`) or the editor's Import | guessing `fbx_path` for another format |
| fix up redirectors | no toolkit operation does it: the editor (right-click the redirector, **Fixup**), or Python (skill `ue-python`) | a delete of the redirector |
| write anything to disk | `asset` `save_all` or `save_asset` | the console command `SaveAll` (opens a dialog) |
| edit a `.uasset` file | never as a file (skill `unreagent-files`) | `write_file`, `edit_file` |

Python is the escape hatch, not the default: it needs the script approval and
its API is not covered by these skills. Prefer a dedicated tool where one
exists.

## Naming

Epic's recommended pattern is `[AssetTypePrefix]_[AssetName]_[Descriptor]_[Variant]`,
with prefixes such as `SM_` (static mesh), `SK_` (skeletal mesh), `SKEL_`
(skeleton), `T_` (texture), `M_` / `MI_` (material / instance), `BP_`,
`ABP_`, `WBP_`, `DT_`, `AS_` (animation sequence), `AM_` (montage), `BS_`,
`FXS_` (Niagara system), `LS_` (level sequence). It is a recommendation; a
project's own convention wins, so look at neighbouring assets with
`asset_search` first
([Epic: recommended asset naming conventions](https://dev.epicgames.com/documentation/unreal-engine/recommended-asset-naming-conventions-in-unreal-engine-projects)).

## Import a batch of FBX files and verify it

1. `asset` `create_folder` for the destination, such as `/Game/Props/Rocks`.
2. `asset_search` with `path_filter` set to that folder: note what is already
   there. **`replace_existing` defaults to `true`**: a file whose asset name
   is taken overwrites that asset, finally.
3. `asset_import` `batch_import` with `fbx_directory`, `dest_path` and an
   explicit `mesh_type` (`static` for props). Do not rely on `auto` for
   skinned files (section `import`, "mesh_type auto").
4. Read the outcome with care: a bare `Error: ` means no file matched; an
   `Error: Batch import: N succeeded, M failed …` drops the per-file list.
5. Verify through the registry, not the message: `asset_search` with
   `path_filter` set to `dest_path` and `limit` high enough. Expect one
   `StaticMesh` per file, plus materials and textures unless you turned
   `import_materials` / `import_textures` off.
6. Spot-check one asset: `get_asset_info` (class, `is_dirty`), and for a
   skeletal mesh its `materials[]`. A picture: `capture_viewport` with
   `asset_path` (skill `ue-viewport`).
7. `asset` `save_all` if you passed `save: false`; check `failed_count`.

For a skeletal mesh sharing a skeleton, and the animations that go with it:
section `import`, "Importing a character".

## Move or rename, then deal with redirectors

1. `asset_referencers` on the asset: what will have to be fixed up.
2. `asset` `rename` with `new_path` and/or `new_name`, or `rename_folder` for
   a whole folder.
3. What the engine does (engine source, `AssetRenameManager.cpp`): it loads
   the referencing packages, rewrites their references and **saves** them,
   the renamed asset, and any redirector it left. No dialog, and files under
   revision control are checked out automatically. It leaves a **redirector**
   at the old path only when a referencer could not be fixed: a level (`.umap`)
   that references it, a read-only or not-current file, or a collection that
   lists it. A rename while the asset registry is still scanning fails.
4. Find what is left: `asset_search` with `class_filter: "ObjectRedirector"`
   and `path_filter` set to the old folder. The short name resolves through
   `/Script/CoreUObject`.
5. Fixing up a redirector re-saves every referencer and then deletes the
   redirector, and only if all re-saves succeeded
   ([Epic: asset redirectors](https://dev.epicgames.com/documentation/unreal-engine/asset-redirectors-in-unreal-engine)).
   No toolkit operation does that; see "Which route". Until then, leave the
   redirector alone: deleting it breaks the levels that still go through it.
6. Verify: `asset_referencers` on the new path lists the referencers;
   on the redirector's path it lists those that still go through it.

Pitfalls from the same Epic page: a new asset cannot take the name of a
redirector, and renaming A to B and back leaves a redirector at B pointing to A.
Fix up redirectors before you delete the assets they point to.

## Find unused assets and delete them safely

1. List the candidates: `asset_search` on the folder.
2. For each, `asset_referencers` with `include_soft: true` (the default). The
   message `No referencers found for '<name>' - this asset appears unused`
   means no package on disk references it. It does not see references from
   code or config (a `ConstructorHelpers` path, an `.ini` entry, a string
   built at runtime), nor assets that are only loaded by path (unverified
   which of these the registry records). Search the project's `Source/` and
   `Config/` for the name (skill `unreagent-files`) before deleting.
3. Save first (`save_all`): the registry's references come from the saved
   packages; whether unsaved edits show up has not been verified.
4. `asset` `delete` with all of them in `asset_paths`, without `force`.
   **A non-forced delete is all or nothing** (engine source,
   `FAssetDeleteModel::DoDelete`): if any asset in the batch is still
   referenced, on disk or in memory, nothing is deleted and you get
   `Deleted 0 of N assets`. Split the batch to find the one.
5. Verify: `deleted_count` equals `requested_count`, and `asset_search` no
   longer finds them.

Never `delete_folder` to clean up: it force-deletes everything inside
(section `operations`).

## Audit an asset's dependencies

`asset_dependencies` goes one level down; to see a tree, call it again for
each entry, keeping a visited set (cycles are possible). Use it to:

- see what a `migrate` will pull along (it copies the same tree);
- find what a level or Blueprint loads, `include_soft: false` for what loads
  with it at once, the default for soft references too;
- check a mesh uses the materials you expect.

Engine content (`/Engine/`) and native classes (`/Script/`) are left out of
both tools' results; plugin content (`/<PluginName>/`) is not.

## Change a property across many assets

1. `asset_search` for the set (class and folder).
2. `get_asset_info` with `include_properties: true` on one of them: the
   property's exact name and `type`.
3. `set_asset_property` on each, with the JSON type the property has
   (section `properties`).
4. One `save_all` at the end; check `failed_count`.

## Copy assets into another project

`asset` `migrate` with package paths and the other project's absolute
`Content` folder. `overwrite` and `include_dependencies` default to `true`.
Its result always says success: verify by listing the target folder on disk,
or by opening that project.
