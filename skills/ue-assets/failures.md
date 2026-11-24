# Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Cannot access engine or script Blueprints` | an `/Engine/` or `/Script/` path passed to `asset` | engine content is read-only through this tool |
| `asset_search` returns nothing for a class you know exists | none of the four lookups resolved the short class name | pass `/Script/<Module>.<Class>` |
| `Property not found: MaterialInterface on SkeletalMesh` | the path tried to enter an array of structs | not reachable through `set_asset_property` |
| the editor crashes after `set_asset_property` | the path probably ended in an array index | never end the path with an index |
| `Unsupported property type for: …` | an array, an `enum class`, an unlisted struct, or a value the tool could not read as the property's type | check the JSON type. Arrays and `enum class` properties cannot be set here |
| `Deleted 0 of 1 assets` | the engine deleted nothing. The toolkit does not say why, and a reference to the asset is a likely cause (unverified) | `asset_referencers`, then decide on `force` |
| `save_all` success with `failed_count` above 0 | some packages did not save | retry them with `save_asset` and read its error |
| `Error: Batch import: … failed …` | at least one file failed, and the per-file list is gone | `asset_search` on `dest_path` shows what landed |
| a bare `Error: ` from `batch_import` | no file matched, or probably a folder that does not exist | check `fbx_directory` and `file_pattern` |
| `Missing required parameter: new_name` from `duplicate` | the code requires it | pass `new_name` |

## Other errors of `set_asset_property`

Other errors: `Failed to load asset: <path>`, `Property not found: <part> on
<class>`, `Cannot index without preceding array property`, `Property is not an
array, cannot use index`, `Array index 3 out of bounds (size: 2)`, `Nested object
is null: <part>`, `Object property value must be a string path`, `Failed to load
object: <path>`.

## The path checks of `asset`

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
