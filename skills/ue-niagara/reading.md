# UE Niagara: reading systems

The four read operations. None changes anything. Paths are in the skill
itself.

## inspect

The whole system in one call. The message is `Niagara system inspected`.

| Field | Holds |
|---|---|
| `system_name` | the asset's name |
| `asset_path` | the **object path**, `/Game/FX/NS_Fire.NS_Fire`, whatever form you passed |
| `emitter_count` | the number of emitters |
| `emitters[]` | one entry per emitter, below |
| `user_parameters[]` | the same entries as `get_parameters`, without `size_bytes` |

Each emitter has `name` and `enabled`. When the emitter has data, the usual
case, it adds:

- `sim_target`: `CPU`, or `GPU` for anything that is not CPU simulation;
- `local_space`: whether particles move with the component;
- `renderer_count` and `renderer_types`, the renderer classes without
  `Niagara` and `RendererProperties`, such as `Sprite`, `Mesh`, `Ribbon`,
  `Light`, or another class shortened the same way.

`enabled` is the emitter's switch in the system; a disabled emitter draws
nothing.

## list

| Parameter | Default | Meaning |
|---|---|---|
| `folder_path` | `/Game/` | content folder |
| `recursive` | `true` | include subfolders |

Lists the Niagara systems in the folder from the asset registry, without
loading them. Emitter assets are not listed, nor classes derived from
`NiagaraSystem`. There is no paging and no name filter. The result has
`folder`, `count` and `systems[]`, each with `name` and `path` (the object
path). The message is `Found <n> Niagara systems`. An empty list is a
success, also for a folder that does not exist: check the spelling before you
conclude there are no systems. `folder_path` is used as given; a trailing `/`
in the default suggests either form works, not verified.

## get_parameters

The system's user parameters, the ones its users may override. The result has
`system_name`, `asset_path` (as you passed it), `parameter_count` and
`parameters[]`. The message is `Found <n> user parameters`.

| Field | Meaning |
|---|---|
| `name` | the parameter's name without the `User.` prefix |
| `type` | `float`, `int`, `bool`, `vector2`, `vector3`, `vector4`, `color`, `quaternion`, `matrix`, or the engine's own type name for anything else |
| `value` | the default value, below |
| `size_bytes` | the value's size in bytes |

`value` by type:

| Type | `value` |
|---|---|
| `float`, `int` | a JSON number |
| `bool` | a JSON boolean |
| `vector2`, `vector3`, `vector4` | a **string**, such as `"(1, 0.5, 0)"` |
| `color` | a **string** in r, g, b, a order, such as `"(1, 0, 0, 1)"` |
| anything else | the type in angle brackets, such as `"<matrix>"` |
| no data in the store | `"<no data>"` |

The numbers in those strings are printed with `%g`: about six significant
digits. To set a vector, send an object, not this string (section
`set-parameter`).

Types beyond the seven settable ones, such as a quaternion, a matrix, a
texture, a mesh, a data interface or a position, show up here but
`set_parameter` refuses them.

## get_emitter_info

One emitter in more detail. Pass `emitter_name` (compared without regard to
case) or `emitter_index` (0-based, a JSON number). When both are given,
`emitter_name` wins. A negative `emitter_index`, or one that cannot be read as a number,
counts as not given.

- Neither: `Must specify either emitter_name or emitter_index`.
- No emitter of that name: `Emitter '<name>' not found. Available: <names>`.
- Index too high: `Emitter index <n> out of range (0-<last>)`.

The result has `name` and `enabled`. When the emitter has data, it adds
`sim_target`, `local_space`, `deterministic`, `random_seed` and `renderers[]`.
The message is `Emitter info retrieved`.

Each renderer has `class_name` (shortened as in `inspect`) and `enabled`,
plus:

| Renderer | Extra fields |
|---|---|
| Sprite | `alignment`, `facing_mode`, `sub_image_size` (`"<w>x<h>"`, rounded) |
| Mesh | `facing_mode`, `mesh_count` |
| Ribbon | `facing_mode`, `shape_mode`, `draw_direction` |
| Light | `type: "Light"` |

The enum values are UE's own names for those settings. Other renderer classes
get only `class_name` and `enabled`.

## What reading tells you

- **Which knobs exist**: only `User.` parameters can be changed with this
  tool. If the effect the user wants is not one of them, it is an authoring
  change (section `limits`).
- **Why nothing shows**: an emitter with `enabled: false`, or one with no
  renderers, draws nothing.
- **What the renderers draw**: the materials they use are not reported.
  Those are ordinary materials (skill `ue-materials`).
