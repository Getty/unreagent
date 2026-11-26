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

Written from the toolkit's C++ source and the UE 5.8.4 engine source, not
exercised against a running editor. Whether a retargeted animation looks right
cannot be read off any result of this tool: see "What a success does not
prove".

## Sections

Load a section with the `skills` tool (`section`), or read `<section>.md` next
to this file. The recipe below needs none for its happy path.

| Section | What's in it | Load it when |
|---|---|---|
| `workflows` | end-to-end: a pack (Mixamo, Fab) onto UE5 Manny/Quinn, same-skeleton cases, root motion per animation, the verification ladder | you start a real retarget job, or must prove a result |
| `concepts` | what the engine does: retarget root, chains and good chain names, the fuzzy auto-map, the op stack, FK modes, retarget pose, root motion flags | you choose chains, a root or FK modes, or an output looks wrong |
| `rigs` | `inspect_skeleton`, `inspect_ref_pose`, `list_skeletons`, `add_bone`, `inspect_ik_rig`, `create_ik_rig`, `add_chain`, `remove_chain` | you read a skeleton or build or change an IK Rig |
| `retargeter` | `inspect_retargeter`, `create_retargeter`, `setup_ops`, `configure_fk`, `batch_retarget`, `set_root_motion`, `find_anims`, `save` | you build a retargeter, run a retarget, or a chain is not mapped |
| `import` | `import_fbx`, `batch_import_fbx`, their timing options, Interchange | you import FBX animations, or choose between this tool and `asset_import` |
| `anim-checks` | `inspect_anim`, `compare_bones`, `sample_bones`, `diagnose_anim` (how to read pops and flips), `copy_bone_tracks` | you check an animation or copy bone tracks between two |
| `tool-choice` | which plugin tool does which skeleton or animation job, and what no tool can do | the job is near retargeting but you are not sure it is `retarget` |
| `engine-versions` | per-version branches of the plugin, engine changes 5.6–5.8, sources | the editor is not UE 5.7, or you need the sources |
| `failures` | load errors, and the failure table: what you see, what it means, what to do | a call fails, or a result or an output animation is not what you expect |

## Must not get wrong

- **Chains pair by name.** Give both IK Rigs identical chain names. The
  engine's auto-map is fuzzy: a target chain without a namesake still gets
  the closest name (`LeftHand` ↔ `RightHand`), not "unmapped". Read
  `chain_mapping` and distrust every pair whose names differ.
- **The retarget root is the pelvis or hips, not the `root` bone.** A bone
  name the mesh lacks becomes `None` without an error: read it back.
- **Nothing is saved except FBX imports.** `save` each rig, retargeter,
  skeleton and output animation, or `asset` `save_all` (skill `ue-assets`) —
  not for a skeleton changed by `add_bone` (section `rigs`).
- **A success is not a result.** `create_ik_rig` drops bad chains silently,
  `configure_fk` counts entries, `batch_retarget` counts copies — even copies
  holding no motion when the retargeter failed to initialize. Verify (section
  `workflows`, C).
- **On UE 5.8 the batch output lands in `/Game`**; take the paths from
  `new_assets[].path`.
- **This tool cannot set** retarget poses, the Root Motion op's source, IK
  goals or solvers. Arms rotated in every frame, or no root motion from a
  source without a root bone, need the IK Retargeter editor: report it.

## Recipe: retarget animations from one skeleton to another

Each step is one call; `operation` is the name in front. Section `workflows`,
A, is the same path with every check spelled out.

1. `inspect_skeleton` `{"asset_path": "<source mesh>"}`, then the target
   mesh. Take bone names from `bones[].name`.
2. `create_ik_rig` for the source:
   `{"package_path": "/Game/Retarget", "name": "IK_Source", "mesh_path": "<source mesh>", "retarget_root": "<pelvis bone>", "chains": [{"name": "Spine", "start_bone": "<bone>", "end_bone": "<bone>"}, …]}`.
3. `create_ik_rig` for the target, **with the same chain names**.
4. `inspect_ik_rig` `{"asset_path": "<rig>"}` for both: `chain_count` is the
   number you sent, `retarget_root` the bone you named.
5. `create_retargeter`
   `{"package_path": "/Game/Retarget", "name": "RTG_Source_To_Target", "source_rig_path": "<source rig>", "target_rig_path": "<target rig>"}`.
   It adds the default ops, auto-maps, and sets chains named like
   `Pelvis`/`Hips`/`Root` to `GloballyScaled`; no `setup_ops` after it.
6. `inspect_retargeter` `{"retargeter_path": "<retargeter>"}`: in the FK
   Chains op, every `chain_mapping` pair has equal names; `fk_chains` modes
   are what you want.
7. `configure_fk` only if a mode is wrong:
   `{"retargeter_path": "<retargeter>", "chain_settings": [{"chain_name": "Pelvis", "translation_mode": "GloballyScaled"}]}`,
   then `inspect_retargeter` again.
8. `save` `{"asset_path": "<asset>"}` for both rigs and the retargeter.
9. `find_anims` `{"folder_path": "/Game/Anims/Source", "recursive": true}`.
10. `batch_retarget` on two or three animations first:
    `{"retargeter_path": "<retargeter>", "source_mesh_path": "<source mesh>", "target_mesh_path": "<target mesh>", "anim_paths": ["<anim>", …], "prefix": "RTG_", "auto_root_motion": false}`.
    `retargeted_count` must equal the paths you sent.
11. Verify each output (section `workflows`, C): `inspect_anim`,
    `sample_bones` over a few frames, `diagnose_anim`, then
    `capture_viewport` with `asset_path` and `frame` (skill `ue-viewport`).
12. `set_root_motion` per animation as needed, then the rest of the batch in
    chunks of about 20, then `save` every new animation.

**A chain is not mapped, or mapped wrongly.** No operation sets one pair.
`remove_chain` `{"rig_path": "<rig>", "chain_name": "<old>"}`, `add_chain`
with the other rig's name, `setup_ops` `{"retargeter_path": "<retargeter>"}`
(remaps every chain and does not redo the pelvis mode), `inspect_retargeter`,
`configure_fk` if needed, `save` rig and retargeter.

**Import FBX animations with root motion.** `import_fbx`
`{"fbx_path": "D:/Anims/Run.fbx", "dest_path": "/Game/Anims", "skeleton_path": "<skeleton or mesh>"}`
(or `batch_import_fbx` with `fbx_directory`; both save and replace), then
`set_root_motion` `{"asset_path": "<anim>", "enable": true}` per animation
(it also sets Force Root Lock to the opposite of `enable`, and does not
save), then `save` it. After a failed batch import, `find_anims` on `dest_path`
shows what did import.

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
| `batch_retarget` | `retargeter_path`, `source_mesh_path`, `target_mesh_path`, `anim_paths` | `prefix` (`RTG_`), `auto_root_motion` (true), `root_motion_pattern` (section `retargeter`) | new animations | no |
| `set_root_motion` | `asset_path` | `enable` (true) | the animation | no |
| `find_anims` | `folder_path` | `recursive` (false) | nothing | — |
| `save` | `asset_path` | — | — | that asset |
| `inspect_anim` | `asset_path` | — | nothing | — |
| `compare_bones` | `source_anim_path`, `target_anim_path`, `bone_names` | — | nothing | — |
| `sample_bones` | `asset_path`, `frames` | `bones` | nothing | — |
| `diagnose_anim` | `asset_path` | `pop_threshold` (50.0), `rotation_flip_threshold` (0.0) | nothing | — |

- **The path parameter differs per operation.** Only two take an alias:
  `inspect_ik_rig` reads `asset_path`, then `rig_path`; `inspect_retargeter`
  reads `retargeter_path`, then `asset_path`. `add_chain` and `remove_chain`
  take only `rig_path`; `setup_ops`, `configure_fk` and `batch_retarget` only
  `retargeter_path`.
- **`file_pattern` (`batch_import_fbx`) does nothing:** read, then ignored;
  every `.fbx` in the directory is imported.
- **Declared but not read:** `import_mesh` for `batch_import_fbx`,
  `sample_times` (`compare_bones`),
  `recursive` for `list_skeletons`, `bones` for `inspect_ref_pose`.
- **Arrays are JSON arrays.** A missing one returns, for example,
  `Missing required parameter: bone_names (JSON array)`. `anim_paths`,
  `bone_names` and `bones` hold strings, `frames` numbers (truncated),
  `chains` and `chain_settings` objects.

## Paths, saving, undo

- **No path checks.** Paths go to the engine's loader unchanged: no `/Game/`
  fallback, `/Engine/` not rejected. Creating ops return object paths
  (`/Game/Retarget/IK_Source.IK_Source`); when one form does not load, try
  the other. A load failure does not prove the asset is missing: check with
  `asset_search` (skill `ue-assets`).
- **A skeleton parameter also takes a skeletal mesh** (its skeleton is used):
  `asset_path` of `inspect_skeleton`, `inspect_ref_pose`; `skeleton_path` of
  `add_bone`, `import_fbx`, `batch_import_fbx`. Mesh parameters take only
  meshes. Animations are AnimSequences only, never montages or blend spaces.
- **File paths** (`fbx_path`, `fbx_directory`) are on the editor's machine.
- **`save`** writes one asset by path, dirty or not, any asset class.
- **`add_bone` does not mark the skeleton dirty** in the tool's code; save it
  with `save` by path.
- **No undo through this tool.** It opens no transaction; the engine's IK Rig
  and retargeter controllers record their own (UE 5.8.4 source), which only
  the editor's Undo reaches. Copy an asset first with `asset` `duplicate`
  when you may need the old state.

## What a success does not prove

| Operation | It does not prove |
|---|---|
| `create_ik_rig` | that every chain was added (only the count in the message shows a drop), that the root was accepted, saved |
| `add_chain` | that the stored name is yours (compare the message) |
| `remove_chain`, `add_chain` | that a retargeter using the rig was remapped: run `setup_ops` |
| `create_retargeter`, `setup_ops` | that chains are mapped to the right partners; `setup_ops` replaces earlier mappings |
| `configure_fk` | that anything changed: `<n>` counts matched chain names, unknown mode strings included; 0 is a success |
| `import_fbx` | that the save happened, that the animation fits the skeleton, root motion |
| `batch_retarget` | that every path was retargeted (`retargeted_count`, 0 is a success), that the copies hold motion, saved |
| `diagnose_anim` | that the animation looks right: `total_issues: 0` says nothing about sliding feet, limbs through the body or a wrong pose |
| `copy_bone_tracks` | that any track was copied (`copied_count`) |
| `find_anims`, `list_skeletons` | that the folder exists (`count: 0`, no error) |

## Engine versions

The plugin branches for UE 5.6 and for 5.7 or later; the UE 5.8 port changes
only `batch_retarget`. Differences without an error (section
`engine-versions` has the table):

- **UE 5.6:** `create_retargeter` and `setup_ops` do not assign the rigs to
  the ops; `CopyLocal` is ignored by `configure_fk`; `batch_retarget` passes
  no overwrite flag.
- **UE 5.8:** `batch_retarget` output lands in `/Game`; FK translation modes
  the engine added show as `Other` and cannot be set.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope and the
  tool's own fields sit under its `data` key (skill `ue-tasks`).
- **Failure** is only `Error: <message>`, with no data.
- **Long calls.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait,
  not the work: read the state back with `find_anims` before you retry.
  `batch_retarget` and `batch_import_fbx` run as one call and can hold the
  editor long; keep `anim_paths` short and follow skill `ue-tasks`, "A 30 s
  timeout can crash the editor".
- Calls the bridge queues do not warn about a misspelt parameter.

## Background in the plugin

The plugin's `Resources/domains/assets.md`, "Skeleton & Retargeting", lists
the chains it considers essential and its pitfalls; its `import_fbx` example
uses `destination`, but the code reads `dest_path`, and it says retargeted
output gets no root motion, while `batch_retarget` does enable it for names
matching `root_motion_pattern`. Result warnings cite numbered pitfalls
(`PITFALL #11`, `#15`) whose list is not in the checkout.

## See also

`ue-anim-sequence` (keys, skeleton swaps, mesh bones), `ue-assets`
(search, import, save, move), `ue-viewport` (look at a frame),
`ue-pie-testing` (output log), `ue-control-rig`, `ue-anim-blueprint`,
`ue-anim-montage`, `ue-blend-space`, `ue-tasks`, `unreagent-iterate`.
