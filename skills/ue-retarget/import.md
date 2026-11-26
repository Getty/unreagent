# retarget: FBX animation import

Section of skill `ue-retarget`. The skill body has the recipes, the
operations table, path rules, and what a success does not prove.

## This tool or asset_import

`retarget` `import_fbx` and `asset_import` `import` (skill `ue-assets`) both
run the editor's FBX import. They differ in what you can choose:

| | `retarget` `import_fbx` | `asset_import` `import` |
|---|---|---|
| skeleton | required | required for `skeletal` and `animation` |
| animations | always imported | by `mesh_type` and `import_animations` |
| materials, textures | never | by `import_materials`, `import_textures` (default on) |
| replace an existing asset | always | `replace_existing` (default on) |
| save | always | `save` (default on) |
| static meshes, export, reimport, source-file info | no | yes |

Take `retarget` `import_fbx` for animation files that go onto an existing
skeleton with nothing to decide. Take `asset_import` for everything else, and
whenever the import must not save or must not replace.

## import_fbx

| Parameter | Default | Meaning |
|---|---|---|
| `fbx_path` | required | the FBX file, a path on the machine that runs the editor |
| `dest_path` | required | content folder for the result, such as `/Game/Anims` |
| `skeleton_path` | required | a Skeleton, or a skeletal mesh whose skeleton is used |
| `import_mesh` | `false` | `true` imports the FBX as a skeletal mesh with its animations; `false` as animation only |
| `custom_sample_rate` | `0` | animation sample rate in frames per second, truncated to an integer. Above 0 it also turns off "use default sample rate" (30 fps). 0 changes neither setting: the import then uses its configured defaults, where the engine's own tooltip says 0 means "find automatically the best sample rate" (UE 5.8.4 `FbxAnimSequenceImportData.h`; the configured default of the 30 fps switch is unverified) |
| `snap_to_frame_boundary` | `false` | snap the animation length to the closest frame boundary |

The parameter is `dest_path`. The plugin's `domains/assets.md` shows an
example with `destination`, which the code does not read.

The import runs automated, replaces an existing asset of the same name, asks
the engine to save the result (the tool does not check that it did), and
imports neither materials nor textures. It also sets bone-track import and
scene conversion (FBX axes to UE axes) on. There is no dry run and no way to
switch any of that off here.

When to set the two timing options:

- `custom_sample_rate`: the file was authored at a rate the project needs to
  keep (60 fps mocap, for example), or `inspect_anim` after a first import
  shows a `num_frames` that does not fit the source's frame count. The
  plugin's `domains/assets.md` ("Frame Rate") recommends re-importing with it
  rather than changing a sequence's rate afterwards, because, it says,
  `SetFrameRate()` can crash on non-multiple rate changes (plugin claim,
  unverified).
- `snap_to_frame_boundary`: the FBX's length is not a whole number of frames
  at the import rate (sub-frame end time), which shows as a last key that is
  odd or a `length` slightly off.

Returns `imported_assets[]` (`name`, `path`, `class`) and the message
`Imported <n> asset(s) from <file name without extension>`.

Errors: `FBX file not found: <path>`, the skeleton load errors (section
`failures`), and `Import failed for: <path>` when the import produced no
asset. The result gives no reason for a failed import.

What the code cannot tell you:

- **Whether the animation fits the skeleton.** The tool checks no bone names.
  Check a few bones with `sample_bones`, or run `diagnose_anim` (section
  `anim-checks`).
- **Root motion.** The import does not set it; call `set_root_motion`
  (section `retargeter`).
- **Interchange.** The options above are the legacy FBX factory's
  (`UFbxImportUI`). In UE 5.8.4 source the console variable
  `Interchange.FeatureFlags.Import.FBX` defaults to on, and the engine has a
  converter (`InterchangeFbxAssetImportDataConverter`) that maps the legacy
  options — skeleton, sample rate, frame snap, materials, textures — onto the
  Interchange pipeline. Whether this tool's import task goes through that
  converter was not traced: check the result with `inspect_anim` (`skeleton`,
  `num_frames`, `length`). The plugin's `domains/assets.md` says UE 5.7
  imports through Interchange too; in which version the switch came is not
  established here.

## batch_import_fbx

Imports every `.fbx` file of one folder with `import_fbx`, always as animation
only (`import_mesh` is not read).

| Parameter | Default | Meaning |
|---|---|---|
| `fbx_directory` | required | the folder on disk |
| `dest_path` | required | content folder for the results |
| `skeleton_path` | required | as for `import_fbx` |
| `custom_sample_rate` | `0` | as for `import_fbx` |
| `snap_to_frame_boundary` | `false` | as for `import_fbx` |

- **`file_pattern` does nothing.** The tool declares it with the default
  `*.fbx`, but the code searches for every file with the extension `fbx` and
  ignores the pattern. To import a subset, call `import_fbx` per file.
- Subfolders are not searched.

When every file imported, the result holds `success_count`, `fail_count`,
`total`, and `results[]`: one `import_fbx` result per file, plus `file` (the
file name). The message is
`Batch import: <ok> succeeded, <failed> failed, <total> total`.

**When at least one file failed, the call returns `Error: Unknown error`.**
The counts and the per-file results are lost. The files that did import are
imported all the same, with a save requested. Recover like this:

1. `find_anims` with `folder_path` set to your `dest_path` (section
   `retargeter`), and compare with the files in the folder. The asset names
   are chosen by the import, not by this tool.
2. `import_fbx` for each missing file. It returns its own error.

Error: `No FBX files found in: <dir>`. A folder that does not exist gives the
same error.
