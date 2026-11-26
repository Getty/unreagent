# retarget: how the engine retargets

Section of skill `ue-retarget`. The skill body has the operations and the
recipes; this section explains the engine objects they create, so you can
choose chains, modes and a retarget root yourself instead of guessing.

Sources are named per statement. "UE 5.8.4 source" means the engine code was
read, not run. Statements marked unverified have no source.

## The three assets

| Asset | What it holds | Made by |
|---|---|---|
| **IK Rig** (`IKRigDefinition`) | one skeletal mesh, a retarget root ("pelvis"), named retarget chains, and optionally IK goals and solvers | `create_ik_rig`, edited with `add_chain`, `remove_chain` |
| **IK Retargeter** (`IKRetargeter`) | a source IK Rig, a target IK Rig, preview meshes, retarget poses, and a stack of ops, each op with its own chain mapping | `create_retargeter`, `setup_ops`, `configure_fk` |
| **AnimSequence** (the output) | a copy of each source animation, re-keyed for the target skeleton | `batch_retarget` |

One IK Rig per skeleton (per proportions, strictly: per mesh). The same rig
serves as source in one retargeter and as target in another.

## Retarget root

The retarget root is the bone whose motion is carried over as a whole and
scaled by the height difference: in practice the pelvis or hips, **not** the
skeleton's top `root` bone. The IK Rig editor's command for it is "Set Pelvis"; Epic's docs
say it is set "so the character's root motion can be defined and transferred
in a proportional way" (dev.epicgames.com, "IK Rig Animation Retargeting").

- UE 5.8.4 source (`UIKRigController::SetRetargetRoot`): a bone name the rig's
  skeleton does not have sets the root to `None`, and the call still returns
  true. `create_ik_rig` therefore succeeds with a misspelt root, and its
  message echoes your text. Read it back: `inspect_ik_rig` `retarget_root`
  is `None` then.

## Retarget chains

A chain is a name plus a start bone and an end bone, the end a descendant of
the start. Chains on the two rigs are paired **by name**; bone counts may
differ (Epic's retargeting docs).

- UE 5.8.4 source (`AddRetargetChainInternal`): a start or end bone the rig's
  skeleton lacks makes the add fail (`NAME_None`); `add_chain` then returns
  its error, `create_ik_rig` drops the chain without one.
- The same function makes the name unique: adding `Spine` to a rig that has a
  `Spine` chain stores it with a numeric suffix (FName numbering, displayed
  like `Spine_0`). `add_chain` reports the stored name; compare it.
- `remove_chain` compares names as FNames, which ignore case.

A chain set that covers a biped, with names that pair one to one:

| Chain | Start → end on UE5 Manny/Quinn (bone names unverified, read them with `inspect_skeleton`) |
|---|---|
| `Spine` | `spine_01` → `spine_05` |
| `Neck` | `neck_01` → `neck_02` |
| `Head` | `head` → `head` |
| `LeftClavicle`, `RightClavicle` | `clavicle_l` → `clavicle_l` |
| `LeftArm`, `RightArm` | `upperarm_l` → `hand_l` |
| `LeftLeg`, `RightLeg` | `thigh_l` → `ball_l` |
| finger chains (`LeftIndex`, …) | `index_metacarpal_l` or `index_01_l` → `index_03_l` |

The plugin's `domains/assets.md`, "Skeleton & Retargeting", lists the same
set and adds a `Root` chain; it says to name the pelvis chain `Pelvis`, not
`Root`. A chain may hold one bone (start = end), which is how `Head` and the
clavicles are usually set up.

## Chain mapping and the fuzzy auto-map

Since UE 5.6 each op of the retargeter that works on chains keeps its own
mapping of target chain → source chain (Epic, "Retargeting Operation Stack in
UE 5.8"). `create_retargeter` and `setup_ops` call the engine's auto-map in
its **fuzzy** mode with a forced remap, over every op.

UE 5.8.4 source (`FRetargetChainMapping::AutoMapChains`): fuzzy scores every
source chain against each target chain as
`1 - Levenshtein(target, source) / (len(target) + len(source))`, names
lowercased, and takes the best score above 0.2. Consequences:

- **An identical name always wins** (score 1). Give both rigs the same chain
  names and the mapping is exact.
- **A target chain with no namesake is still mapped**, to the closest name:
  `LeftHand` can pair with `RightHand` (score about 0.76). Fuzzy almost never
  leaves a chain unmapped. Read `chain_mapping` and treat every pair whose two
  names differ as suspect.
- Ties go to the first source chain in the list.

## The op stack (UE 5.6 and later)

UE 5.6 turned the retargeter into an ordered stack of ops (Epic's public
roadmap, "Retarget Operation Stack"; Epic docs above). The engine's default
set, added by `AddDefaultOps` (UE 5.8.4 source, in this order): **Pelvis
Motion, FK Chains, Run IK Rig, Root Motion, Curve Remap**.

| Op | What it does (Epic docs) |
|---|---|
| Pelvis Motion | carries the retarget root's motion over, translation scaled by proportion |
| FK Chains | copies chain rotations, and per mode translations, onto the target chains |
| Run IK Rig | runs the target IK Rig's goals and solvers (none in a rig made by `create_ik_rig`) |
| Root Motion | copies root motion from the source root (default) or generates it from the target pelvis |
| Curve Remap | copies curves of the same name, remaps others |

The tool reads every op's name, type and enabled flag, but **settings only
for the FK Chains op**, and it can change only FK chain settings. Everything
else in an op (Root Motion's source, Pelvis Motion's scaling, IK settings,
retarget poses, override sets) needs the IK Retargeter editor or editor
Python (skill `ue-python`; not covered by this skill, not verified).

### FK chain modes

Defaults per chain (UE 5.8.4 source, `FRetargetFKChainSettings`): rotation
`Interpolated`, translation `None`.

| Rotation mode | Use it when (Epic docs) |
|---|---|
| `Interpolated` | bone counts or proportions differ: spine, neck, limbs. The default |
| `OneToOne` | the chains have the same bone count, such as fingers |
| `OneToOneReversed` | the same, counted from the end bone |
| `MatchChain`, `MatchScaledChain` | match the chain's overall shape (no Epic description read; unverified) |
| `CopyLocal` | UE 5.7 and later only (enum value present in UE 5.8.4 source; meaning unverified) |
| `None` | copy no rotation |

| Translation mode | Use it when (Epic docs) |
|---|---|
| `None` | the usual choice for every chain except the pelvis |
| `GloballyScaled` | source translations scaled to the target's size: the pelvis or hips chain |
| `Absolute` | target bones take the source's absolute positions |
| `StretchBoneLengthUniformly`, `StretchBoneLengthNonUniformly`, `OrientAndScale` | exist in UE 5.8.4 source; this tool reports them as `Other` and cannot set them |

The plugin's `domains/assets.md` warns that pelvis translation `None`
"freezes pelvis at bind height"; `create_retargeter` sets every chain whose
name contains `Pelvis`, `Hips` or `Root` to `GloballyScaled` for that reason.

## Retarget pose

When the two characters' reference poses differ (A-pose against T-pose, the
usual case for Mixamo against Manny), the retargeter needs a retarget pose
that lines them up, or arms come out rotated. Epic: "Matching these retarget
poses increases the accuracy of the retargeting", with Auto Align tools in
the retargeter editor. **This tool has no operation for retarget poses.** If
the arms of the output are off by a constant angle in every frame, that is the
cause; it is fixed in the IK Retargeter editor, by a human or through editor
Python, not with `retarget`.

## Root motion in an AnimSequence

Two flags on the sequence decide what the engine does with the root bone's
motion (`inspect_anim` reads both):

- `bEnableRootMotion` (`root_motion`): the root bone's motion is extracted and
  moves the character; the mesh stays on the capsule.
- `bForceRootLock` (`force_root_lock`): the root bone is held at its reference
  position without extracting its motion.

Root motion is taken from the skeleton's root bone (bone 0). A skeleton whose
bone 0 is the hips, such as a Mixamo export, has no separate root to extract;
its retargeted copy on a skeleton with a `root` bone gets root motion only if
the retargeter's Root Motion op produces it (default: copied from the source
root; option: generated from the target pelvis — UE 5.8.4 source,
`FIKRetargetRootMotionOpSettings`). That option is not reachable through this
tool.
