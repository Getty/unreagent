# Failure modes

Engine facts here were read in the UE 5.8.4 source, not run.

| What you see | What it means | What to do |
|---|---|---|
| `Cannot access engine or script Blueprints` | an `/Engine/` or `/Script/` path passed to `asset` | engine content is read-only through this tool; `asset_search` still finds it |
| `Blueprint path contains invalid character: '<c>'` | the path check of `asset`, for any asset type | see "The path checks of `asset`" below |
| `Failed to load asset: <path>` | nothing loads at that path, also not with `_C` appended | `asset_search` for the real path; take `path` from its result |
| `asset_search` returns nothing for a class you know exists | none of the four lookups resolved the short class name, and the search silently fell back to `/Script/Engine.<name>` | pass `/Script/<Module>.<Class>`, such as `/Script/UMGEditor.WidgetBlueprint` |
| `asset_search` with `class_filter: "Blueprint"` also lists Widget and Animation Blueprints | subclasses match | filter by the `class` of each entry, or pass the subclass |
| `Property not found: MaterialInterface on SkeletalMesh` | the path tried to enter an array of structs | not reachable through `set_asset_property` (section `properties`) |
| the editor crashes or an asset behaves oddly after `set_asset_property` | the path probably ended in an array index | never end the path with an index. Do not save: restart the editor without saving, if the asset is corrupt in memory |
| `Unsupported property type for: …` | an array, a set, an `enum class`, `FText`, an unlisted struct, a soft reference that does not load, or a value the tool could not read as the property's type | check the JSON type; for the types it cannot set, see section `workflows`, "Which route" |
| `Object type mismatch. Expected Class, got Blueprint` | a Blueprint asset path given for a class property | pass the class path, `…/BP_X.BP_X_C` |
| `Deleted 0 of N assets` (a success) | a non-forced delete is all or nothing: at least one asset of the batch is still referenced, on disk or in memory (an actor in the open level counts) | `asset_referencers` on each; remove the references, or split the batch to find the one; `force` only when the breakage is intended |
| a modal "Unable to delete level while it is open" in the editor | `delete` was given the level that is open | open another level first (skill `ue-levels`); the dialog blocks the editor until someone closes it |
| `Failed to delete folder: … (must be empty …)` | not every asset in the folder could be force-deleted; others may be gone already | `list_assets` with `recursive: true` to see what is left |
| `Failed to create folder: …`, `Source folder does not exist: …` or `Folder does not exist: …` while a PIE session runs | the folder operations, and their existence check, refuse during Play In Editor, so an existing folder reads as missing | stop PIE (skill `ue-pie-testing`) and retry |
| `Failed to rename asset …` at editor start | the asset registry is still scanning; the engine refuses renames then | wait, then retry |
| a redirector left behind after `rename` | a level, a read-only file or a collection referenced the asset | section `workflows`, "Move or rename" |
| `save_all` success with `failed_count` above 0 | some packages did not save | retry them with `save_asset` and read its error. A read-only file (revision control) is a likely cause (unverified) |
| `Error: All N packages failed to save` | every dirty package failed, and the list is dropped | `save_asset` on each asset you changed, to get a per-asset error |
| `save_all` reports `Saved 0/0` but the level is unsaved | a never-saved level lives under `/Temp/` and is skipped | `open_level save_as` (skill `ue-levels`) |
| a mesh imported with `mesh_type: "auto"` is a `StaticMesh` though the FBX is skinned | under Interchange `auto` forces static meshes | delete it, reimport with `mesh_type: "skeletal"` and `skeleton_path` (section `import`) |
| `Error: Batch import: … failed …` | at least one file failed, and the per-file list is gone | `asset_search` on `dest_path` shows what landed; import the missing files one by one with `import` to see each error |
| a bare `Error: ` from `batch_import` | no file matched, or a folder that does not exist | check `fbx_directory` and `file_pattern` |
| `Import failed for: <path>` | the engine produced no asset | check the output log (`get_output_log`) for the importer's reason |
| `Missing required parameter: new_name` from `duplicate` | the code requires it | pass `new_name` |
| `migrate` says success but nothing arrived | its result is not a confirmation | check the target folder on disk; the output log has the engine's messages |
| `Error: Task timed out after 300000ms …` | the bridge stopped waiting; the work may go on | do not repeat it: follow the task up (skill `ue-tasks`), then read the state back with `asset_search` |

## The path checks of `asset`

`asset` checks `asset_path` (in every operation except `set_preview_mesh`),
`directory`, `folder_path` and `new_folder_path` before it uses them, in this
order, with these errors (`MCPParamValidator.cpp`, `ValidateBlueprintPath`):

1. empty: `Blueprint path cannot be empty`;
2. longer than 512 characters: `Blueprint path exceeds maximum length of 512 characters`;
3. under `/Engine/` or `/Script/`: `Cannot access engine or script Blueprints`;
4. containing `..`: `Blueprint path cannot contain path traversal sequences`;
5. containing any of `` < > | & ; ` $ ( ) { } [ ] ! * ? ~ ``:
   `Blueprint path contains invalid character: '<c>'`.

The messages speak of Blueprints for every asset type.

Some paths are not checked at all:

- `asset_paths` in `delete`, `migrate` and `get_hashes`;
- `new_path` in `rename`;
- `dest_path` and `new_name` in `duplicate`;
- both paths in `set_preview_mesh`;
- every path of `asset_search`, `asset_dependencies`, `asset_referencers` and
  `asset_import`, which can therefore read and export engine content.

The errors of `set_asset_property` are in section `properties`.
