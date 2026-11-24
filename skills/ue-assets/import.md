# asset_import

`operation` is required and is lowercased: `import`, `batch_import`, `export`,
`reimport` or `get_source`. An unknown one gives `Unknown operation: '<op>'.
Valid: import, batch_import, export, reimport, get_source`.

File paths in this tool are paths on the machine that runs the editor.

The parameter names are `fbx_path` and `dest_path`. The plugin's
`domains/assets.md` shows an example with `file_path` and `destination`, but the
code does not read those names.

## import

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
| `custom_sample_rate` | `0` | animation sample rate. At 0 or below the tool sets none |
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

## batch_import

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

## export

| Parameter | Meaning |
|---|---|
| `asset_path` | required |
| `output_path` | required. A file path on disk. The tool asks for its folder to be created, without checking the result, and looks the exporter up by its extension |

`.fbx` exports a mesh or an animation. The plugin's `domains/tooling.md`
describes `.t3d` as a text dump, for reading properties no tool exposes. The
result holds `asset_path`, `output_path` and `asset_class`. Errors: `Asset not
found: <path>`, `Export failed for <path>. Check that the asset type supports
FBX export.`

## reimport and get_source

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

## Importing a character

1. `asset` `{"operation": "create_folder", "folder_path": "/Game/Characters/Hero"}`.
2. `asset_import` `{"operation": "import", "fbx_path": "D:/Art/Hero.fbx", "dest_path": "/Game/Characters/Hero", "mesh_type": "skeletal", "skeleton_path": "/Game/Characters/Shared/SK_Base"}`.
3. Note the skeletal mesh in `imported_assets`. Import the animations with
   `mesh_type: "animation"` and the same skeleton.
