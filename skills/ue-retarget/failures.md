# retarget: when a call fails

Section of skill `ue-retarget`. The recipe, the operations table and "Paths,
saving, undo" are in the skill body.

## Load errors

Load errors, by what was asked for:

| Error | Asked for |
|---|---|
| `Failed to load: <path>`, `Skeletal mesh has no skeleton: <path>`, `Asset is not a Skeleton or SkeletalMesh: <path> (is <class>)` | a skeleton or mesh |
| `Failed to load skeletal mesh: <path>`, `Asset is not a SkeletalMesh: <path>` | a skeletal mesh |
| `Failed to load IK Rig: <path>`, `Asset is not an IKRigDefinition: <path>` | an IK Rig |
| `Failed to load IK Retargeter: <path>`, `Asset is not an IKRetargeter: <path>` | a retargeter |
| `Failed to load anim sequence: <path>`, `Asset is not an AnimSequence: <path>` | an animation |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| `Missing required parameter: rig_path` although you passed a path | `add_chain` and `remove_chain` take `rig_path`, not `asset_path` | use the name in the operations table |
| `create_ik_rig` reports fewer chains than you sent | a chain entry lacked `name`, `start_bone` or `end_bone`, or the engine refused the bones | `inspect_ik_rig`, then `add_chain`, which reports a failure |
| `inspect_ik_rig` returns `warnings` about goals or solvers | the plugin expects batch retarget to fail with them | this tool cannot remove goals or solvers; build a rig with `create_ik_rig`, which adds none |
| a target chain has no source in `chain_mapping` | the auto-map found no chain of a similar name | recipe "A chain is not mapped" |
| the retargeted character stays at bind height or slides | pelvis translation or root motion | `configure_fk` the pelvis chain to `GloballyScaled`; `set_root_motion` (section `retargeter`) |
| `batch_retarget` succeeds with `retargeted_count: 0` or fewer than sent | paths did not load as AnimSequences, or the engine produced nothing | `inspect_anim` on a path; `inspect_ik_rig` warnings |
| `batch_import_fbx` fails with `Unknown error` | at least one file failed; the per-file results are dropped | `find_anims` on `dest_path`, then `import_fbx` for the missing files |
| `configure_fk` succeeded, nothing changed | chain name or mode string did not match | names from `inspect_retargeter` `fk_chains[].chain`; modes from section `retargeter` |
| a change is gone after an editor restart | it was never saved | `save` after each finished change |
| `inspect_ik_rig` `retarget_root` is `None` after `create_ik_rig` named one | the bone name does not exist on the mesh; the engine sets `None` without a failure (UE 5.8.4 source) | bone names from `inspect_skeleton` of that mesh; the tool cannot set the root again, so delete and recreate the rig (`asset` `delete`, skill `ue-assets`) |
| a chain is named `Spine_0` or similar | a chain of that name existed; the engine made the new name unique | `remove_chain` both, `add_chain` once |
| `chain_mapping` pairs `LeftHand` with `RightHand`, or similar | fuzzy auto-map picks the closest name when no identical one exists | rename the chain so both rigs use the same name (recipe "A chain is not mapped") |
| `inspect_retargeter` always shows the `PITFALL #11` warning | the engine's default op set contains Pelvis Motion | not a fault by itself; check the pelvis chain's `translation` and the output's pelvis height |
| output animations exist but do not move (same `sample_bones` values in every frame) | the retargeter's processor did not initialize; the copies were made and cleared first (UE 5.8.4 source) | `get_output_log` `{"filter": "IK Retargeter"}` (skill `ue-pie-testing`); check both rigs' `skeletal_mesh` against the meshes you passed, the retarget roots, the chain bones |
| `batch_retarget` output is in `/Game`, not near the source | UE 5.8 with no target folder (section `engine-versions`) | take paths from `new_assets[].path`; move them with `asset` `rename` (skill `ue-assets`) |
| arms or shoulders rotated by the same angle in every frame | the two characters' reference poses differ (A-pose against T-pose) and the retargeter has no matching retarget pose | not fixable with this tool: retarget pose in the IK Retargeter editor (section `concepts`); report it |
| feet slide or float although root motion is set | the retargeted root bone does not move (`diagnose_anim` `root_motion.range_xy` near 0 on the output), e.g. a source without a root bone | the Root Motion op's source must generate from the pelvis, which this tool cannot set (section `concepts`); report it |
| many `quaternion_flip` issues with `dot_product` near -1 | sign changes of equivalent quaternions, no visible jump | ignore those; look at values near 0 (section `anim-checks`) |
| `copy_bone_tracks` result looks stretched in time | the copy is resampled onto the target's key count | trim or resample first with `anim_edit` (skill `ue-anim-sequence`) so the lengths match |
