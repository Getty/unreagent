# anim_edit: skeletal meshes and skeletons

These operations change a skeletal mesh, and some its skeleton asset. Every
animation, Animation Blueprint and placed actor that uses the mesh depends on
what they change, and they save by default. Read the mesh with `inspect_mesh`
first, and consider `"save": false` or a copy made with `asset duplicate`
(skill `ue-assets`).

The bone, skeleton and vertex work itself is done by the engine's skeleton
modifier and mesh description functions. The tool decides which bones and
values go in; how the engine applies them has not been verified.

## inspect_mesh

Reads a skeletal mesh. It changes nothing. `mesh_path` is required.

Returns `mesh_path`, `skeleton` (the skeleton's path, or `none`), `num_bones`
and `bones`, one entry per bone of the mesh:

- `name`, `index`, `parent_index` (`-1` for the root), and `parent_name` on
  every bone but the root;
- `bind_local`, the bone's local reference transform, with `pos`, `rot` and
  `scale`;
- `bind_cs`, the reference transform in component space, with `pos` and `rot`,
  when the code could compute it;
- `inv_bind_matrix` with `row0` to `row3`, when the mesh has inverse bind
  matrices.

These values are formatted strings, not JSON objects: `(<x>, <y>, <z>)` and
`(P=<pitch>, Y=<yaw>, R=<roll>)` with three decimals, and four numbers with
four decimals per matrix row.

When the mesh has render data, the result also holds, all from LOD 0:

- `lod0_vertex_count` and `max_bone_influences`;
- `skin_weight_summary`, one entry per bone with `bone`,
  `vertices_influenced` and `weight_sum`;
- `sample_vertices`, up to five vertices spread over the vertex buffer, each
  with `vertex_index`, `position` (a string) and `weights` (`bone`, `weight`).

The tool names a bone by taking the buffer's bone index as an index into the
reference skeleton. Whether the engine stores the index that way is not
verified, so check the bone names in these two lists against a mesh you know.

The message is `Mesh inspection: <path> (<n> bones)`. The result grows with the
bone count: each bone appears in `bones` and again in `skin_weight_summary`.

**A possible crash.** The code reads LOD 0's skin weight buffer without
checking that the mesh has one. Whether the engine can hand back no buffer for
a mesh that has render data has not been verified. If it can, `inspect_mesh`
on such a mesh would crash the editor instead of returning an error. If the
editor goes down during `inspect_mesh`, do not repeat the call on that mesh.

## sync_mesh_bones

Adds bones that the mesh's skeleton asset has and the mesh lacks.

- `bone_names` (optional) is an array of bone names. Without it, every
  missing bone is added.
- Bones are added in skeleton order, parents before children, each with the
  skeleton's reference pose and parent, and then committed to the mesh. The
  tool adds no skin weights, so the new bones move no vertices. The tool's
  description gives the purpose: Copy Pose From Mesh for bones the mesh did
  not have.
- **Nothing missing** returns success with no data:
  `All requested bones already exist in mesh — nothing to add.` This check
  comes first. A name in `bone_names` that the skeleton does not have fails
  with `Bone '<b>' not found in USkeleton. Cannot add a bone that doesn't exist in the skeleton.`
  only when some other requested bone is missing. Otherwise the misspelt name
  gets the "nothing to add" success.
- **A bone the engine refuses is skipped.** The reason goes only to the
  editor's log, as `SyncMeshBones: AddBone failed for '<b>' (parent='<p>')`.
  Compare `added_bones` with what you expected.

Returns `bones_added`, `added_bones` and `mesh_bone_count_after`. The message
is `Added <n> bones to <path>`.

Errors: `Mesh has no USkeleton: <path>`,
`All AddBone calls failed — no bones were added.`,
`CommitSkeletonToSkeletalMesh failed.`

## rename_bone

Renames a bone. Which assets change depends on where the two names are found
in the mesh's skeleton:

| `old_name` in the skeleton | `new_name` in the skeleton | What happens |
|---|---|---|
| yes | no | `full`: the bone is renamed in the skeleton and in the mesh |
| no | yes | `mesh_only`: renamed in the mesh only, to match a skeleton that already has the new name |
| yes | yes | `Both '<old>' and '<new>' already exist in skeleton` |
| no | no | `Bone '<old>' not found in skeleton (under old or new name)` |

Then the mesh is checked: `Bone '<old>' not found in mesh`,
`Bone '<new>' already exists in mesh`.

- **Only the mesh at `mesh_path` is renamed.** Other meshes on the same
  skeleton keep the old name. Run `rename_bone` on each of them with the same
  names afterwards; it then takes the `mesh_only` path.
- After a full rename, the tool calls the skeleton's handling for a hierarchy
  change; the tool's description says this rebuilds animation linkup data.
  Whether animation tracks, sockets, Animation Blueprint nodes or other assets
  that name the bone follow the rename is up to the engine and has not been
  verified. Check sequences with `inspect_track` under the new name.
- With `save`, a full rename saves the skeleton and the mesh; `mesh_only` saves
  the mesh.

Returns `mode` (`full` or `mesh_only`), `old_name`, `new_name`, `mesh` and
`skeleton` (the skeleton's path). The message is
`Renamed bone '<old>' -> '<new>' (skeleton and mesh)`, or
`(mesh only — skeleton already had new name)`.

Errors besides the ones above: `Mesh has no USkeleton: <path>`.

## set_ref_pose

Changes one bone's reference (bind) pose.

- `mesh_path` is a skeletal mesh, or, when it does not load as one, a
  skeleton. Neither:
  `Failed to load as USkeletalMesh or USkeleton: <path>`.
- `position` `{x, y, z}` and `rotation` `{pitch, yaw, roll}` are the bone's new
  local reference transform, the one `inspect_mesh` shows as `bind_local`.
  They are absolute values, not offsets. The one you leave out keeps its value;
  inside one you give, a missing component is 0. Scale is never changed.
- At least one must be an object. This is checked before anything loads:
  `At least one of 'position' {x,y,z} or 'rotation' {pitch,yaw,roll} must be provided`.

**Mesh mode** (`mode: "mesh"`) changes the mesh's reference pose. The skeleton
asset's reference pose stays as it was.

- With `retransform_vertices` (default `true`), the vertices of every LOD that
  has a mesh description are re-posed so the mesh keeps its shape. How many
  LODs were done appears only in the editor's log, as
  `set_ref_pose: Retransformed vertices in <n>/<total> LODs`.
- `"retransform_vertices": false` changes the bind pose without moving any
  vertex.
- Either way, the mesh's inverse bind matrices are rebuilt.

**Skeleton mode** (`mode: "skeleton_only"`) changes the skeleton's reference
pose and then calls the skeleton's handling for a hierarchy change. The tool's
description says this propagates to dependent assets; that is the engine's
part. No vertex is touched.

Returns `mode`, `mesh_path` or `skeleton_path` (the key follows the mode),
`bone_name`, `retransform_vertices` (mesh mode only), and `old_transform` and
`new_transform`, each with `position` and `rotation` objects rounded to three
decimals. The message is
`Updated ref pose for bone '<b>' in <path> (mesh)`, or `(skeleton only)`.

Errors: `Mesh has no USkeleton: <path>`, `Bone '<b>' not found in mesh`,
`Bone '<b>' not found in skeleton`.

## transform_vertices

Moves the mesh's vertices in every LOD that has a mesh description. The tool
touches no bone, skin weight or bind pose.

| Parameter | Form | A component left out |
|---|---|---|
| `rotation` | `{pitch, yaw, roll}` | 0 |
| `scale` | `{x, y, z}`, or a number for a uniform scale | 1 |
| `translation` | `{x, y, z}` | 0 |

- At least one is required:
  `At least one of 'rotation' {pitch,yaw,roll}, 'scale' {x,y,z}/float, or 'translation' {x,y,z} must be provided`.
  `rotation` and `translation` count only as objects; `scale` counts in any
  form. This is checked before the mesh loads.
- The three are combined into one transform and applied once. **Every call
  applies it again** on top of the last one.

Returns `mesh_path`, `lods_transformed`, `total_lods` and `applied_transform`
with `rotation`, `scale` and `translation`. The message is
`Transformed vertices in <n>/<total> LODs of <path>`.

Error, besides the load error:
`No LODs with MeshDescription found in <path> — cannot transform vertices`.
