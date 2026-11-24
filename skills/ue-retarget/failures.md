# retarget: when a call fails

Section of skill `ue-retarget`. The recipes, the operations table and "Paths
and loading" are in the skill body.

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
