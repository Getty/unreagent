# retarget: which tool for which job

Section of skill `ue-retarget`. Several plugin tools touch skeletons and
animations; this table says which one does the job, and where the neighbours
are documented.

| Job | Tool |
|---|---|
| FBX animations onto an existing skeleton, saved at once, replacing same-named assets | `retarget` `import_fbx`, `batch_import_fbx` (section `import`) |
| any other FBX import: meshes with materials and textures, static meshes, an import that must not save or not replace, reimport, export | `asset_import` (skill `ue-assets`) |
| convert animations to another skeleton's proportions | `retarget` `batch_retarget` (section `workflows`) |
| only point a sequence at another skeleton, keys unchanged (same bones, same proportions) | `anim_edit` `replace_skeleton` (skill `ue-anim-sequence`) |
| add a new bone to a Skeleton asset | `retarget` `add_bone` (section `rigs`) |
| give a skeletal mesh the bones its skeleton has, rename a bone, change a bind pose | `anim_edit` `sync_mesh_bones`, `rename_bone`, `set_ref_pose` (skill `ue-anim-sequence`, section `meshes`) |
| a mesh's bones with bind poses and skin weights | `anim_edit` `inspect_mesh` |
| a skeleton's bone tree and sockets, or its bind pose with rotations | `retarget` `inspect_skeleton`, `inspect_ref_pose` |
| copy bone tracks from one sequence into another | `retarget` `copy_bone_tracks` (section `anim-checks`) |
| offset one bone's keys, resample, cut a frame range | `anim_edit` `adjust_track`, `resample`, `extract_range` |
| one bone's keys in detail | `anim_edit` `inspect_track` |
| several bones at given frames, or an automatic health check | `retarget` `sample_bones`, `diagnose_anim` |
| an animation's length, frame count, root motion flags | `retarget` `inspect_anim` |
| move, rename, duplicate or delete the output assets | `asset` `rename`, `duplicate`, `delete` (skill `ue-assets`) |
| find skeletal meshes, IK Rigs, retargeters | `asset_search` (skill `ue-assets`); `list_skeletons` lists Skeleton assets only |
| look at a frame of an animation | `capture_viewport` with `asset_path` and `frame` (skill `ue-viewport`, section `asset-preview`) |
| read what the engine logged during a batch | `get_output_log` (skill `ue-pie-testing`, section `log`) |
| retarget poses, Root Motion op source, IK goals and solvers, override sets, auto-generated chains | not in any toolkit tool: the IK Rig / IK Retargeter editors, or editor Python (skill `ue-python`; untested here) |
| Control Rig graphs (other assets than IK Rigs) | `control_rig` (skill `ue-control-rig`) |
| use the retargeted sequences in an Animation Blueprint, montage or blend space | skills `ue-anim-blueprint`, `ue-anim-montage`, `ue-blend-space` |
