# retarget: engine versions

Section of skill `ue-retarget`. The skill body names the differences that
show no error; this section has the full table, the engine-side changes, and
the sources.

## What the plugin does per version

The plugin's code at upstream commit 9f6a0f7 (43 tools, plugin version
1.1.0; the checkout's `main` adds no change to the retarget files) has
branches for UE 5.6 and for UE 5.7 or later, and none for older engines
(`RetargetEditor.cpp`, the `ENGINE_MINOR_VERSION` guards):

| Where | UE 5.7 or later | UE 5.6 |
|---|---|---|
| `create_retargeter`, `setup_ops` | add default ops, remove duplicates, assign the source and target IK Rig to every op, auto-map | the same without assigning the rigs to the ops |
| `inspect_retargeter` `chain_mapping` | read from the FK Chains op's settings | read from the retargeter asset |
| `inspect_retargeter` `rotation`, `configure_fk` `rotation_mode` | also `CopyLocal` | `CopyLocal` is not known to the code: ignored without an error when sent |
| `batch_retarget` | asks the engine to overwrite existing output assets | passes no overwrite flag; the engine's default applies |

**UE 5.8.** Upstream 9f6a0f7 declares engine version 5.6.0 in its `.uplugin`
and has no branch of its own for UE 5.8. The UE 5.8 port (fork
`Getty/ue-llm-toolkit`, branch `ue-5.8`, commit 9fc7d4f) changes one call:
on UE 5.8 `batch_retarget` calls the engine's `RunBatchRetarget` with the
animations, both meshes, the retargeter, the prefix, referenced assets left
out and overwrite on, because UE 5.8 deprecates `DuplicateAndRetarget` and
adds a target-folder parameter to it. UE 5.7 and 5.6 keep the calls of the
table; every other row is unchanged, so UE 5.8 takes the "5.7 or later"
column. Without that port the 5.7 call does not fit UE 5.8's signature (a
`TargetPath` string now sits where the tool passes a bool), so upstream code
is not expected to compile there (inferred from both sources, not built).

## What the engine changed

| Version | Change | Source |
|---|---|---|
| 5.6 | the IK Retargeter becomes an ordered stack of ops with a chain mapping per op (the default set — Pelvis Motion, FK Chains, Run IK Rig, Root Motion, Curve Remap — read from UE 5.8.4 source; whether 5.6 had the same set is unverified) | Epic public roadmap "Retarget Operation Stack"; Epic docs "Retargeting Operation Stack" |
| 5.7 | retargeter additions for foot-to-ground contact, squash and stretch, and characters of different scale | Epic's 5.7 release announcement (unrealengine.com news, 2025-11-12) |
| 5.8 | `DuplicateAndRetarget` deprecated for `RunBatchRetarget` (a struct with `TargetPath` and `bUseSourcePath`); without a target path the output goes to `/Game` | UE 5.8.4 source, `IKRetargetBatchOperation.h/.cpp` |
| 5.8 | FK translation modes `StretchBoneLengthUniformly`, `StretchBoneLengthNonUniformly`, `OrientAndScale` exist (this tool shows them as `Other`); since which version is not established | UE 5.8.4 source, `FKChainsOp.h`; Epic docs list them |
| 5.8 | "Override Sets" replace the former retarget profiles | Epic docs "IK Rig Animation Retargeting" |

Which of the 5.7 additions are ops a retargeter made by this tool contains
is unverified; the tool adds only the engine's default set.

## Practical consequences

- **On UE 5.6, check `chain_mapping` and the result harder.** The ops do not
  get the rigs assigned by the tool; what that changes in a retarget is not
  known (C++ source, not run).
- **On UE 5.8, collect the output from `/Game`** and move it with `asset` `rename` (skill
  `ue-assets`). Two runs with the same prefix overwrite each other there.
- **On every version, a second `batch_retarget` over the same input** can
  replace the first run's output (5.7 and later certainly; 5.6 by the
  engine's default, unverified).

## Sources

- Toolkit: `Private/RetargetEditor.cpp`, `Private/MCP/Tools/MCPTool_Retarget.{h,cpp}` at 9f6a0f7; the port's diff at 9fc7d4f.
- UE 5.8.4 engine source: `Plugins/Animation/IKRig/Source/IKRigEditor/…/IKRetargetBatchOperation.*`, `IKRetargeterController.cpp`, `IKRigController.cpp`, `IKRig/…/IKRetargetChainMapping.cpp`, `RetargetOps/FKChainsOp.h`, `RetargetOps/RootMotionGeneratorOp.h`.
- https://dev.epicgames.com/documentation/unreal-engine/ik-rig-animation-retargeting-in-unreal-engine
- https://dev.epicgames.com/documentation/unreal-engine/retargeting-operation-stack-in-unreal-engine-5-8
- https://portal.productboard.com/epicgames/1-unreal-engine-public-roadmap/c/456-retarget-operation-stack
- https://www.unrealengine.com/news/unreal-engine-5-7-is-now-available
