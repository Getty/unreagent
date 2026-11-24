---
name: ue-retarget
description: >
  Retarget animations between skeletons with retarget: skeleton inspection, IK
  Rigs and chains, IK Retargeters, FBX animation import, batch retarget, root
  motion, animation diagnosis. Use before you move animations to another
  skeleton or check an imported or retargeted animation.
toolkit_tools: [retarget]
---

# UE retargeting: retarget

The UE LLM Toolkit plugin's **`retarget`** tool covers the path from one
skeleton's animations to another's: it reads skeletons and bind poses, creates
IK Rigs and their retarget chains, creates and configures IK Retargeters,
imports FBX animations, runs a batch retarget, sets root motion and runs
checks on AnimSequences. Through the plugin's MCP bridge it is listed as
`unreal_retarget`. It works only while the editor is running and the plugin is
reachable; `unreagent-iterate` covers the wait for the plugin.

This skill was written from the toolkit's C++ source. It has not been exercised
against a running editor. Whether a retargeted animation looks right cannot be
read off any result of this tool: see "What a success does not prove".

This skill has four sections. Load the one for the operation you are about to
call; the recipes below need no section for their happy path.

| Section | Covers | Load it when |
|---|---|---|
| `rigs` | `inspect_skeleton`, `inspect_ref_pose`, `list_skeletons`, `add_bone`, `inspect_ik_rig`, `create_ik_rig`, `add_chain`, `remove_chain` | you read a skeleton or build or change an IK Rig |
| `retargeter` | `inspect_retargeter`, `create_retargeter`, `setup_ops`, `configure_fk`, `batch_retarget`, `set_root_motion`, `find_anims`, `save` | you build a retargeter, run a retarget, or a chain is not mapped |
| `import` | `import_fbx`, `batch_import_fbx` | you import FBX animations, or choose between this tool and `asset_import` |
| `anim-checks` | `inspect_anim`, `compare_bones`, `sample_bones`, `diagnose_anim`, `copy_bone_tracks` | you check an animation or copy bone tracks between two |

## Recipes

Each step is one call; `operation` is the name in front.

### Retarget animations from one skeleton to another

1. `inspect_skeleton` `{"asset_path": "<source mesh>"}`, then the same for the
   target mesh. Take the bone names for the chains from `bones[].name`.
2. `create_ik_rig` for the source:
   `{"package_path": "/Game/Retarget", "name": "IK_Source", "mesh_path": "<source mesh>", "retarget_root": "<pelvis bone>", "chains": [{"name": "Spine", "start_bone": "<bone>", "end_bone": "<bone>"}, …]}`.
3. `create_ik_rig` for the target, **with the same chain names**: the
   retargeter pairs chains by name.
4. `inspect_ik_rig` `{"asset_path": "<rig>"}` for both. `chain_count` must be
   the number of chains you sent, and `retarget_root` the bone you named. A
   chain or root the engine refused is dropped without an error.
5. `create_retargeter`
   `{"package_path": "/Game/Retarget", "name": "RTG_Source_To_Target", "source_rig_path": "<source rig>", "target_rig_path": "<target rig>"}`.
   This already adds the default ops and auto-maps the chains; `setup_ops` is
   not needed after it.
6. `inspect_retargeter` `{"retargeter_path": "<retargeter>"}`. In the FK
   Chains op, read `chain_mapping` (every target chain needs its source) and
   `fk_chains` (the modes per chain).
7. `configure_fk` only if a mode is wrong:
   `{"retargeter_path": "<retargeter>", "chain_settings": [{"chain_name": "Pelvis", "translation_mode": "GloballyScaled"}]}`.
   Then `inspect_retargeter` again: the count in its message is not proof.
8. `save` `{"asset_path": "<asset>"}` once per asset: both rigs and the
   retargeter. No operation above saves.
9. `find_anims` `{"folder_path": "/Game/Anims/Source", "recursive": true}` for
   the paths.
10. `batch_retarget`
    `{"retargeter_path": "<retargeter>", "source_mesh_path": "<source mesh>", "target_mesh_path": "<target mesh>", "anim_paths": ["<anim>", …], "prefix": "RTG_"}`.
    Compare `retargeted_count` with the number of paths you sent; a path that
    did not load is left out silently.
11. `diagnose_anim` `{"asset_path": "<new anim>"}` for each entry of
    `new_assets`. Then look at it: `capture_viewport` with `asset_path` and
    `frame` (skill `ue-viewport`, section `asset-preview`).
12. `save` each new animation.

### A chain is not mapped, or mapped to the wrong chain

The tool has no operation that sets one chain pair. Make the names match, then
map again:

1. `remove_chain` `{"rig_path": "<rig>", "chain_name": "<old>"}`.
2. `add_chain` `{"rig_path": "<rig>", "chain_name": "<name as in the other rig>", "start_bone": "<bone>", "end_bone": "<bone>"}`.
3. `setup_ops` `{"retargeter_path": "<retargeter>"}`. It maps all chains
   again.
4. `inspect_retargeter`, then `configure_fk` again if needed, then `save` the
   rig and the retargeter.

### Import FBX animations onto a skeleton, with root motion

1. `import_fbx`
   `{"fbx_path": "D:/Anims/Run.fbx", "dest_path": "/Game/Anims", "skeleton_path": "<skeleton or mesh>"}`,
   or `batch_import_fbx` with `fbx_directory` in place of `fbx_path`. Both ask
   the engine to save what they import, and replace an existing asset of the
   same name.
2. `find_anims` `{"folder_path": "/Game/Anims"}` when a batch import returned
   an error: the files that worked are imported all the same.
3. `set_root_motion` `{"asset_path": "<anim>", "enable": true}`, one animation
   per call. Import does not set the flag.
4. `save` `{"asset_path": "<anim>"}`: `set_root_motion` does not save.

### Check an animation

1. `inspect_anim` `{"asset_path": "<anim>"}`: length, `num_frames`, the root
   motion flags, the skeleton.
2. `diagnose_anim` `{"asset_path": "<anim>"}`: read `summary` and `issues`.
3. `sample_bones`
   `{"asset_path": "<anim>", "frames": [<prev_frame>, <frame>], "bones": ["<bone>"]}`
   around an issue. Always pass `bones`; without it every track is returned
   for every frame.

## Operations

`operation` is required and lowercased before dispatch. An unknown operation
returns `Unknown operation: '<op>'. Valid: inspect_skeleton, …`. A missing or
empty required string returns `Missing required parameter: <name>`.

| Operation | Required | Optional (default) | Writes | Saves |
|---|---|---|---|---|
| `inspect_skeleton` | `asset_path` | — | nothing | — |
| `inspect_ref_pose` | `asset_path` | — | nothing | — |
| `list_skeletons` | — | `folder_path` (`/Game`) | nothing | — |
| `add_bone` | `skeleton_path`, `bone_name`, `parent_bone` | `position`, `rotation` | the skeleton | no |
| `copy_bone_tracks` | `source_anim_path`, `target_anim_path`, `bone_names` | — | the target animation | no |
| `inspect_ik_rig` | `asset_path` or `rig_path` | — | nothing | — |
| `create_ik_rig` | `package_path`, `name`, `mesh_path` | `retarget_root`, `chains` | a new IK Rig | no |
| `add_chain` | `rig_path`, `chain_name`, `start_bone`, `end_bone` | — | the IK Rig | no |
| `remove_chain` | `rig_path`, `chain_name` | — | the IK Rig | no |
| `inspect_retargeter` | `retargeter_path` or `asset_path` | — | nothing | — |
| `create_retargeter` | `package_path`, `name`, `source_rig_path`, `target_rig_path` | — | a new IK Retargeter | no |
| `setup_ops` | `retargeter_path` | — | the retargeter | no |
| `configure_fk` | `retargeter_path`, `chain_settings` | — | the retargeter | no |
| `import_fbx` | `fbx_path`, `dest_path`, `skeleton_path` | `import_mesh` (false), `custom_sample_rate` (0), `snap_to_frame_boundary` (false) | imported assets | **yes** |
| `batch_import_fbx` | `fbx_directory`, `dest_path`, `skeleton_path` | `custom_sample_rate` (0), `snap_to_frame_boundary` (false) | imported assets | **yes** |
| `batch_retarget` | `retargeter_path`, `source_mesh_path`, `target_mesh_path`, `anim_paths` | `prefix` (`RTG_`), `auto_root_motion` (true), `root_motion_pattern` (section `retargeter`) | new animations | not by the tool |
| `set_root_motion` | `asset_path` | `enable` (true) | the animation | no |
| `find_anims` | `folder_path` | `recursive` (false) | nothing | — |
| `save` | `asset_path` | — | — | that asset |
| `inspect_anim` | `asset_path` | — | nothing | — |
| `compare_bones` | `source_anim_path`, `target_anim_path`, `bone_names` | — | nothing | — |
| `sample_bones` | `asset_path`, `frames` | `bones` | nothing | — |
| `diagnose_anim` | `asset_path` | `pop_threshold` (50.0), `rotation_flip_threshold` (0.0) | nothing | — |

- **The path parameter differs per operation, and only two operations take an
  alias.** `inspect_ik_rig` reads `asset_path` and then `rig_path`;
  `inspect_retargeter` reads `retargeter_path` and then `asset_path`.
  `add_chain` and `remove_chain` take only `rig_path`; `setup_ops`,
  `configure_fk` and `batch_retarget` only `retargeter_path`.
- **Three declared parameters do nothing.** `file_pattern`
  (`batch_import_fbx`), `sample_times` (`compare_bones`) and `recursive` for
  `list_skeletons` are not used by the code (sections `import`, `anim-checks`,
  `rigs`).
- **Arrays are JSON arrays.** A missing one returns, for example,
  `Missing required parameter: bone_names (JSON array)`. `anim_paths`,
  `bone_names` and `bones` hold strings, `frames` numbers (truncated to
  integers), `chains` and `chain_settings` objects.

## Paths and loading

- **No path checks.** Paths go to the engine's loader unchanged: there is no
  `/Game/` fallback, and `/Engine/` paths are not rejected.
- **A skeleton parameter also takes a skeletal mesh**, whose skeleton is then
  used: `asset_path` of `inspect_skeleton` and `inspect_ref_pose`, and
  `skeleton_path` of `add_bone`, `import_fbx` and `batch_import_fbx`. A mesh
  parameter (`mesh_path`, `source_mesh_path`, `target_mesh_path`) takes only a
  skeletal mesh.
- **Animations are AnimSequences only.** Montages and blend spaces do not
  load here.
- **Path form.** `create_ik_rig` and `create_retargeter` return `asset_path`
  as an object path, such as `/Game/Retarget/IK_Source.IK_Source`. The
  inspect and list operations return object paths too. When one form does not
  load, try the other.
- **File paths** (`fbx_path`, `fbx_directory`) are paths on the machine that
  runs the editor.

Load errors, by what was asked for:

| Error | Asked for |
|---|---|
| `Failed to load: <path>`, `Skeletal mesh has no skeleton: <path>`, `Asset is not a Skeleton or SkeletalMesh: <path> (is <class>)` | a skeleton or mesh |
| `Failed to load skeletal mesh: <path>`, `Asset is not a SkeletalMesh: <path>` | a skeletal mesh |
| `Failed to load IK Rig: <path>`, `Asset is not an IKRigDefinition: <path>` | an IK Rig |
| `Failed to load IK Retargeter: <path>`, `Asset is not an IKRetargeter: <path>` | a retargeter |
| `Failed to load anim sequence: <path>`, `Asset is not an AnimSequence: <path>` | an animation |

A load failure does not prove the asset is missing. Check with `asset_search`
(skill `ue-assets`).

## Saving and undo

- **Only the FBX imports save on their own.** Every other write changes the
  asset in the running editor and at most marks its package dirty. An editor
  stop or crash loses it.
- **`save` writes one asset by path**, dirty or not. It is not limited to
  retargeting assets: the code passes any asset path to the editor's save
  call. Message `Saved: <path>`, error `Failed to save: <path>`.
- **`add_bone`: the tool's code does not mark the skeleton dirty.** Whether
  the engine's hierarchy-change handler, which the tool calls, marks it dirty
  is not verified. `asset` `save_all` (skill `ue-assets`) saves dirty packages
  only, so do not rely on it here. Save the skeleton with `save`.
- **`batch_retarget`** creates its assets through the engine's batch
  operation. The tool does not save them, and whether the engine does is not
  visible in the tool's code, on any engine version. Save them.
- **No undo.** The tool opens no transaction for any write. `set_root_motion`
  calls `Modify` on the animation, and `copy_bone_tracks` brackets its edit in
  the animation's data controller; whether either leaves an undo entry is up
  to the engine. Treat every write as final. For a backup, copy the asset
  first with `asset` `duplicate` (skill `ue-assets`).

## What a success does not prove

| Operation | Success means | It does not prove |
|---|---|---|
| `create_ik_rig` | the rig exists in memory with a mesh | that every chain was added (the message has the count; refused chains are dropped silently), that the retarget root was accepted (the message echoes what you sent), saved |
| `add_chain` | the engine added a chain | that it has the name you sent: the message has the name the engine returned. Saved |
| `remove_chain` | the chain is gone | that the retargeter's mapping was updated: run `setup_ops`. Saved |
| `create_retargeter` | the asset exists in memory with default ops | that every chain is mapped, or mapped to the right chain: read `chain_mapping`. Saved |
| `setup_ops` | default ops were added and chains auto-mapped | the same; and earlier mappings may have been replaced |
| `configure_fk` | the FK Chains op was found | that anything changed: the count is the entries whose chain name matched, also when the mode string was not recognized. A message with 0 chains is still a success |
| `import_fbx` | at least one asset was imported | that the save happened (the tool requests it and does not check it), that the animation fits the skeleton, or that root motion is on |
| `batch_retarget` | the batch ran | that every path was retargeted (`retargeted_count`; 0 is still a success), saved, or that any result looks right |
| `set_root_motion` | the two flags were set in memory | saved |
| `add_bone` | the skeleton asset has the bone in memory | that any skeletal mesh has it, or that it is saved. The tool's code does not mark the package dirty; whether the engine does is not verified |
| `copy_bone_tracks` | the call ran | that any track was copied: read `copied_count`. Saved |
| `compare_bones` | both animations loaded | anything about poses: it compares bone names only |
| `sample_bones` | the animation has tracks | that every frame you asked for is there: frames out of range are left out |
| `diagnose_anim` | the checks ran | that the animation is fine. `total_issues: 0` means no jump above the thresholds and a root motion flag that matches the root bone's movement; when `root_motion.root_bone` is `NOT_FOUND`, the root motion check did not run at all. It says nothing about foot sliding, limbs passing through the body, wrong proportions or a wrong pose |
| `save` | the editor's save call returned true | nothing further |
| `find_anims`, `list_skeletons` | the registry was asked | that the folder exists: a misspelt folder gives `count: 0`, not an error |

**No result judges retargeting quality.** After `batch_retarget`, the numbers
of `diagnose_anim` catch broken output; a human or a `capture_viewport` image
(skill `ue-viewport`) has to judge the rest.

## Which tool for which job

| Job | Tool |
|---|---|
| FBX animations onto an existing skeleton, saved at once, replacing same-named assets | `retarget` `import_fbx`, `batch_import_fbx` (section `import`) |
| any other FBX import: meshes with materials and textures, static meshes, an import that must not save or not replace, reimport, export | `asset_import` (skill `ue-assets`) |
| convert animations to another skeleton's proportions | `retarget` `batch_retarget` |
| only point a sequence at another skeleton, keys unchanged | `anim_edit` `replace_skeleton` (skill `ue-anim-sequence`) |
| add a new bone to a Skeleton asset | `retarget` `add_bone` (section `rigs`) |
| give a skeletal mesh the bones its skeleton has, rename a bone, change a bind pose | `anim_edit` `sync_mesh_bones`, `rename_bone`, `set_ref_pose` (skill `ue-anim-sequence`, section `meshes`) |
| a mesh's bones with bind poses and skin weights | `anim_edit` `inspect_mesh` |
| a skeleton's bone tree and sockets, or its bind pose with rotations | `retarget` `inspect_skeleton`, `inspect_ref_pose` |
| copy bone tracks from one sequence into another | `retarget` `copy_bone_tracks` (section `anim-checks`) |
| offset one bone's keys, resample, cut a frame range | `anim_edit` `adjust_track`, `resample`, `extract_range` |
| one bone's keys in detail | `anim_edit` `inspect_track` |
| several bones at given frames, or an automatic health check | `retarget` `sample_bones`, `diagnose_anim` |
| an animation's length, frame count, root motion flags | `retarget` `inspect_anim` |

## Engine versions

The plugin's code at upstream commit 9f6a0f7 (43 tools, plugin version
1.1.0) has branches for UE 5.6 and for UE 5.7 or later, and none for older
engines:

| Where | UE 5.7 or later | UE 5.6 |
|---|---|---|
| `create_retargeter`, `setup_ops` | add default ops, remove duplicates, assign the source and target IK Rig to every op, auto-map | the same without assigning the rigs to the ops |
| `inspect_retargeter` `chain_mapping` | read from the FK Chains op's settings | read from the retargeter asset |
| `configure_fk` `rotation_mode` | also `CopyLocal` | `CopyLocal` is not recognized and is ignored without an error |
| `batch_retarget` | asks the engine to overwrite existing output assets | passes no overwrite flag; the engine's default applies |

**UE 5.8.** Upstream 9f6a0f7 declares engine version 5.6.0 in its `.uplugin`
and has no branch of its own for UE 5.8. The UE 5.8 port (fork `Getty/ue-llm-toolkit`, branch
`ue-5.8`, commit 9fc7d4f) gives `batch_retarget` a third branch: on UE 5.8 it
calls the engine's newer batch entry point, `RunBatchRetarget`, with the
animations, both meshes, the retargeter, the prefix, referenced assets left
out, and overwrite on. UE 5.7 and 5.6 keep the calls of the table. It sets no
target folder, so on UE 5.8 the output lands in `/Game` (UE 5.8.4 source, not
run; section `retargeter`, `batch_retarget`). The other rows of the table
are unchanged in that port: UE 5.8 takes the "5.7 or later" column.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work: a `batch_retarget` or `batch_import_fbx` may still finish. Read
  the state back with `find_anims` before you retry (skill `ue-tasks`). Both
  run as one call and can hold the editor for more than 30 s, and a request
  that waits 30 s behind it may crash the editor; run such a call as skill
  `ue-tasks`, "A 30 s timeout can crash the editor", describes. Smaller
  `anim_paths` lists keep each call short.
- **Misspelt parameters.** Calls the bridge queues do not warn about a
  parameter the tool does not declare (skill `ue-tasks`).

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

## Background in the plugin

- The plugin's `Resources/domains/assets.md`, "Skeleton & Retargeting", lists
  the chains it considers essential (Root, Pelvis, Spine, Neck, Head, arms,
  clavicles, legs, fingers) and its retargeting pitfalls. Read it there.
- The same file shows an `import_fbx` example with `destination`. The code
  reads `dest_path`.
- It also says root motion is not enabled on retargeted output. The code of
  `batch_retarget` does enable it for assets whose name matches
  `root_motion_pattern` (section `retargeter`).
- The warnings in results cite the plugin's numbered pitfalls (`PITFALL #11`,
  `PITFALL #15`). The numbered list itself is not in the checkout this skill
  was written from.

## See also

- `ue-anim-sequence`: `anim_edit` for keys, resampling, skeleton swaps and
  skeletal mesh bones.
- `ue-assets`: `asset_search`, `asset_import`, `save_all`, `duplicate`.
- `ue-viewport`: looking at a retargeted animation frame.
- `ue-control-rig`: Control Rig graphs, which are other assets than IK Rigs.
- `ue-anim-blueprint`, `ue-anim-montage`, `ue-blend-space`: using the
  retargeted sequences.
- `ue-tasks`: long calls and calls that timed out.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
