# UE materials: creating, changing and reading instances

The `parameters` object itself is in section `parameters`, saving and the
path check in the skill itself.

## create_material_instance

Creates `<package_path><asset_name>` as a Material Instance Constant of
`parent_material`, applies `parameters` and saves the package.

| Parameter | Default | Notes |
|---|---|---|
| `asset_name` | — (required) | not checked: keep to letters, digits and `_` |
| `parent_material` | — (required) | a material or another material instance; path-checked |
| `package_path` | `/Game/Materials/` | path-checked; a missing trailing `/` is added |
| `parameters` | — | applied before the save |

The result has `asset_path` (the package path, such as
`/Game/Materials/MI_Rock_Wet`), `asset_name`, `parent_material` as you passed
it, and `saved: true`. The message is `Created material instance: <path>`.

- **The name is not checked for a clash.** The tool calls `CreatePackage` and
  the factory with that name and saves.
  - An instance already loaded at that path is rebuilt in place: its old
    parameters are gone, the new ones are applied and the file is overwritten.
  - **An asset of another class loaded under the same name, such as a
    material or a texture, stops the editor**: the engine's object allocator
    logs `Cannot replace existing object of a different class.` at Fatal
    level (`StaticAllocateObject`,
    `Engine/Source/Runtime/CoreUObject/Private/UObject/UObjectGlobals.cpp`,
    UE 5.8.4, read, not run). The editor crashes and unsaved work is lost.
  - An asset on disk that is not loaded is probably overwritten by the save;
    not verified.

  Check first with `asset_search` (skill `ue-assets`): `path_filter` set to
  the folder, `name_pattern` to the name. `name_pattern` matches substrings
  and `path_filter` includes subfolders, so the name is taken only if an entry
  has exactly that `name` and that `package_path`. `asset_search` writes
  `package_path` without the trailing slash: `/Game/Materials`.
- **Parameter failures do not fail the call.** A texture that does not load is
  only logged in the editor log, as
  `Material instance created but some parameters failed: …`, and the instance
  is saved without it. Read it back with `get_material_info`, then send the
  texture again with `set_material_parameters`.
- Errors: `Failed to load parent material: <path>`,
  `Failed to create package: <path>`, `Failed to create material instance`,
  `Material instance created but failed to save: <path>`. After the last one
  the instance exists in the editor's memory: save it with `asset save_asset`
  rather than creating it again.

## set_material_parameters

Applies `parameters` to an existing Material Instance Constant. The order of
checks: `material_instance_path` present and path-checked, the instance loads,
then `parameters`.

- A base material, a Material Instance Dynamic, or any path that does not load
  as a Material Instance Constant fails with
  `Failed to load material instance: <path>`.
- A missing `parameters` object, or one that is not an object, fails with
  `Missing required parameter: parameters`.
- The result has `material_instance` (your path) and `modified: true`. The
  message is `Updated parameters on material instance: <path>`. On success the
  tool runs the instance's change notification and marks the package dirty:
  save afterwards.

**A texture that does not load fails the call, but the rest of the change
lands.** The tool applies every entry it can read and collects the failures:
all scalars, all vectors and every texture that loads are set in memory, and
only the textures in the error message are missing. On this path the tool
skips its change notification and does not mark the package dirty itself;
whether the engine's setters do is not verified. Send the failed textures
again with fixed paths; a call that succeeds marks the package dirty, and
then you save.

To change the parent of an instance, the tool has no operation: create a new
instance of the other parent instead (or section `limits`).

## get_material_info

Takes `asset_path`, a material or a material instance asset. The result has
`name`, `path` (the object path), `class` (such as `Material`,
`MaterialInstanceConstant`) and `is_instance`. The message is
`Material info: <name>`. A path that does not load as a material fails with
`Failed to load material: <path>`.

For an instance it adds:

- `parent`: the parent's object path, present only when there is a parent;
- `scalar_parameters`, `vector_parameters` (`{r, g, b, a}` each) and
  `texture_parameters` (a texture path, or `None`), keyed by parameter name.

These are the values stored on the instance itself, the overrides. Inherited
values are not listed, and a base material (`is_instance: false`) returns no
parameters at all: this operation cannot tell you which parameters a material
exposes. Static switch values are not reported.

Use it:

- after every write, to confirm each value and name;
- to read an existing instance before you change it, so you can put values
  back;
- to follow a chain: an instance's `parent` can itself be an instance; call
  `get_material_info` on it until `is_instance` is `false`. The last one is
  the master material whose graph defines the parameter names.
