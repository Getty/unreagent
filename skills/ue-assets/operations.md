# The other `asset` operations

The ten operations of the `asset` tool that the main skill does not describe.
The main skill covers `get_asset_info`, `save_asset`, `save_all` and
`delete`, the path forms ("Asset paths") and the result shape; section
`properties` covers `set_asset_property`.

## list_assets

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

**A `limit` the tool cannot read as a number lists one asset, not 25.** The
read is the engine's `FJsonObject::GetIntegerField`, which cannot report a
failure: `null`, an array, an object, an empty string or a string that is not
numeric reads as 0, and the tool then raises it to 1. `false` reads as 0 and
`true` as 1, so both list one asset too. A string made of an optional sign,
digits and at most one dot is read as that number, and a fraction is cut off
towards zero.
A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
(`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
The default 25 applies only when the key is absent
(`FJsonObject::GetIntegerField`
in `Engine/Source/Runtime/Json/Private/Dom/JsonObject.cpp`,
`FJsonValue::AsNumber` in `JsonValue.cpp` beside it; read in the UE 5.8.4
source, not run). A wrong type for `recursive` is not an error either: see
section `json-types`.

## rename

| Parameter | Meaning |
|---|---|
| `asset_path` | required |
| `new_name` | the new asset name |
| `new_path` | the new folder, such as `/Game/Props`. At least one of `new_name` and `new_path` is required |

The rename goes through the engine's asset tools (`IAssetTools::RenameAssets`).
Read in the UE 5.8.4 source (`AssetRenameManager.cpp`), that shows no dialog,
checks files out of revision control automatically, loads the referencing
packages, rewrites their references and **saves** them together with the
renamed asset. A rename is therefore on disk at once. A **redirector** stays
at the old path only when a referencer could not be fixed up: a level that
references the asset, a read-only or out-of-date file, or a collection. The
tool's header and the plugin's `domains/assets.md` say it always creates one.
Section `workflows`, "Move or rename", has the follow-up.

The result holds `old_path`, `new_path` (folder plus name) and `success`.
Errors: `At least one of new_name or new_path is required`, and
`Failed to rename asset <path> to <folder>/<name>`, which the engine also
causes while the asset registry is still scanning after editor start.

## duplicate

| Parameter | Meaning |
|---|---|
| `asset_path` | required |
| `dest_path` | required. The folder of the copy |
| `new_name` | **required by the code**, although the tool's description calls it optional |

The result holds `success`, `source_path` and `new_asset_path`. A failure gives
`Failed to duplicate asset <path> to <folder>/<name>`. The plugin's
`domains/assets.md` notes that duplicating an Animation Blueprint leaves stale
class references.

## migrate

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

## create_folder, rename_folder, delete_folder

| Operation | Parameters | Notes |
|---|---|---|
| `create_folder` | `folder_path` | a folder that already exists is a success, with `already_existed: true`. Error: `Failed to create folder: <path>` |
| `rename_folder` | `folder_path`, `new_folder_path` | moves the folder with its assets. Errors: `Source folder does not exist: <path>`, `Failed to rename folder <a> to <b>` |
| `delete_folder` | `folder_path` | errors: `Folder does not exist: <path>`, `Failed to delete folder: <path> (must be empty — delete contained assets first)` |

**`delete_folder` is a recursive force delete.** The toolkit's error text says
the folder has to be empty. The engine function it calls
(`UEditorAssetSubsystem::DeleteDirectory`, read in the UE 5.8.4 source) loads
every asset in the folder and its subfolders, **force-deletes** them whether
referenced or not, then removes the folder. A force delete of a Blueprint
also deletes its placed instances in loaded levels. The error comes when not
every asset could be deleted, and some may be gone already. List the folder
with `list_assets` and `recursive: true`, and run `asset_referencers` on what
you find, before you call it.

All three folder operations go through the engine's editor-scripting
functions, which refuse to run during Play In Editor (the log says
`The Editor is currently in a play mode.`). So does their existence check,
so during PIE `create_folder` fails with `Failed to create folder: <path>`,
and the other two report an existing folder as missing. `rename_folder`
renames each asset as `rename` does, with the same saving and redirectors.

## open_editor and set_preview_mesh

`open_editor` takes `asset_path` and opens the asset's editor. The result holds
`asset_path`, `asset_class` and `opened`. Errors: `AssetEditorSubsystem not
available.`, `Failed to open editor for: <path>`.

`set_preview_mesh` takes `asset_path`, an animation asset, and `mesh_path`, a
skeletal mesh. It sets the preview mesh, marks the animation dirty, and reopens
the animation's editor if it was open. The result holds `asset_path`,
`mesh_path`, `mesh_name` and `editor_reopened`. Errors: `Asset is not an
animation: <path> (<class>)`, `Failed to load skeletal mesh: <path>`.

## get_hashes

Detects changes cheaply, without loading the assets.

| Parameter | Meaning |
|---|---|
| `asset_paths` | required. An array of **package paths** |

The result holds `count` and `assets[]`. Each entry holds `path`, `hash`, `file_size`, `mtime` (ISO 8601)
and `is_dirty`, or `error: "file_not_found"`. The hash is built from the file
size, the modification time and the dirty flag. It is not a content hash,
although the tool's description calls it one. Compare the hashes of one asset
over time: an unchanged hash means the file's size and modification time and
the package's dirty flag are as they were. Two different assets with equal
hashes say nothing about their content.
