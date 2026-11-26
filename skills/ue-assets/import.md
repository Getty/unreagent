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

### Interchange, and mesh_type auto

The tool fills the legacy FBX options (`UFbxImportUI`) and hands them to the
engine's import tasks. When Interchange handles FBX, the engine converts
those options into its generic assets pipeline. Read in the UE 5.8.4 source:
the switch is the console variable `Interchange.FeatureFlags.Import.FBX`,
default `true` (`InterchangeFbxTranslator.cpp`), and the conversion
(`InterchangeFbxAssetImportDataConverter.cpp`, `ConvertToInterchange`)
carries `import_materials`, `import_textures`, `generate_collision`,
`combine_meshes`, `normal_import`, the skeleton, `import_animations`,
`custom_sample_rate` and `snap_to_frame_boundary` over. The plugin's
`domains/assets.md` says UE 5.7 imports through Interchange; for 5.6 this has
not been checked (forum reports date Interchange as the FBX default to 5.5).

- **`auto` imports every mesh as a static mesh under Interchange.** It sets
  the static-mesh type, and the conversion turns that into "force all meshes
  to static mesh, no skeletal meshes". The legacy factory's switch to a
  skeletal mesh does not happen there. Pass `mesh_type` explicitly: `static`
  for props, `skeletal` with `skeleton_path` for a character. To give a
  skinned mesh a new skeleton of its own, no `mesh_type` of this tool fits:
  `skeletal` requires an existing one. Use the editor's Import, or Python
  (skill `ue-python`).
- Only FBX is this tool's documented case. `fbx_path` is checked for
  existence only, so another file type reaches the engine's importer for its
  extension with FBX options attached; what comes out is not verified. For
  textures, sounds, glTF or USD see section `workflows`, "Which route".

The result holds `imported_assets[]` (`name`, `path`, `class`), and the message
is `Imported 2 asset(s) from SM_Rock`. An import also creates the materials
and textures the FBX names unless you turn them off; whether they are listed
in `imported_assets` has not been verified, so read the folder back. After
an import, check the classes: a `StaticMesh` where you expected a
`SkeletalMesh` means the type was wrong.

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

A key left out of `import_rotation` or `import_translation` counts as 0, not
as the current value. The overrides are applied to the asset's FBX import
data, or to its Interchange generic-assets pipeline. The result holds
`asset_path`, `asset_class` and, only when you passed it, `new_source_path`.
The plugin's `domains/assets.md` notes that the overrides do not change a
skeleton's reference pose.

**The new source path and the overrides are written before the reimport runs.**
When the reimport then fails, the asset keeps them in memory (unsaved), and the
next reimport uses them. Check with `get_source`.

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
3. Check `imported_assets`: a `SkeletalMesh` (and possibly a `PhysicsAsset`),
   not a `StaticMesh`. `get_asset_info` on the mesh lists its material
   `slot`s.
4. Import the animations with `mesh_type: "animation"` and the same skeleton,
   one file per call or `batch_import` over their folder. For animations the
   `retarget` tool has its own FBX import (skill `ue-retarget`, section
   `import`); the plugin notes that root motion is not enabled by an import.
5. `asset` `set_preview_mesh` on an animation to see it on the new mesh, then
   `capture_viewport` (skill `ue-viewport`) to look at it.
6. `asset` `save_all` if you passed `save: false`.

`skeleton_path` may name the Skeleton or a SkeletalMesh that uses it. The
FBX's bone hierarchy has to match that skeleton; a mismatch fails the import
or produces a broken mesh (engine behaviour, not verified here).
