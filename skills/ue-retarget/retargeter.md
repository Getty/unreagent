# retarget: retargeters, batch retarget, root motion, save

Section of skill `ue-retarget`. The skill body has the recipes, the
operations table, path rules, saving, and what a success does not prove.

None of the writes here saves. Save with `save`, at the end of this section.

## inspect_retargeter

The path is read from `retargeter_path`, then from `asset_path`. Without
either: `Missing required parameter: retargeter_path or asset_path`.

Returns `name`, `path`, `source_rig`, `target_rig`, `source_mesh`,
`target_mesh` (each a path, or the string `None`), `op_count`, `ops[]`, and
`warnings[]` when there are any.

An op has `index`, `name`, `enabled`, and `type` (the op's struct name). The
FK Chains op also has:

- `fk_chains[]`: `chain` (the target chain's name), `enable_fk`,
  `translation`, `rotation`, `rotation_alpha`, `translation_alpha`.
  `translation` is `None`, `GloballyScaled`, `Absolute` or `Other`.
  `rotation` is `None`, `Interpolated`, `OneToOne`, `OneToOneReversed`,
  `MatchChain`, `MatchScaledChain`, `CopyLocal` (UE 5.7 or later) or `Other`.
- `chain_mapping[]`: `target` and `source` chain names. On UE 5.7 or later it
  is read from the FK Chains op's settings, on UE 5.6 from the retargeter
  asset.

The mapping and the modes are reported for FK Chains ops only. Settings of
other ops are not returned.

One warning exists:
`PITFALL #11: Pelvis Motion op is present. DuplicateAndRetarget ignores this op — pelvis translation must come through FK chain with TranslationMode=GloballyScaled.`
That is the plugin's claim; this skill has not tested it. When you see it,
check the pelvis chain's `translation` in `fk_chains`.

Error: `Failed to get retargeter controller`.

## create_retargeter

Creates an IK Retargeter `name` in `package_path` from two IK Rigs. In the
same call it:

1. sets `source_rig_path` and `target_rig_path` as the retargeter's rigs;
2. adds the engine's default ops;
3. removes duplicate ops: any op whose name ends in `_<digits>` while an op
   with the name before that suffix exists;
4. on UE 5.7 or later, assigns both rigs to every op;
5. auto-maps the chains with the engine's fuzzy name matching;
6. in the first FK Chains op, sets `TranslationMode` to `GloballyScaled` for
   every target chain whose name contains `Pelvis`, `Hips` or `Root`, in any
   case.

Returns `asset_path` (an object path) and the message
`Created retargeter '<name>' with <n> ops, auto-mapped chains`.

- **"auto-mapped" is not a count.** The message does not say how many chains
  found a partner. Read `chain_mapping` with `inspect_retargeter`.
- Step 6 matches by substring, so a chain named `RootIK` or `LeftHipsTwist` is
  set as well.
- The tool sets no preview meshes on the retargeter, and has no operation for
  it. `batch_retarget` takes its meshes as parameters.
- What happens when an asset of that name already exists is not visible in
  the tool's code: it creates the package and the object without a check.
  Check with `asset_search` first (skill `ue-assets`).
- The asset is marked dirty and not saved. The tool does not announce the new
  asset to the asset registry itself; if `asset_search` does not list it
  before a save, `save` it and search again.

Errors: the load errors of either rig, `Failed to create package: <path>`,
`Factory failed to create IKRetargeter`, `Failed to get retargeter controller`.

## setup_ops

Runs steps 2 to 5 of `create_retargeter` on an existing retargeter at
`retargeter_path`, with the rigs the retargeter already has. Message:
`SetupOps complete: <n> ops after cleanup`.

Use it for a retargeter that was not made by `create_retargeter`, or after an
IK Rig's chains changed. Not after `create_retargeter`: that has done the
same.

- **It maps all chains again.** The code passes `true` as the auto-map call's
  second argument, which in the engine's API forces a remap. Expect a mapping
  set by hand in the editor to be replaced.
- It does not repeat step 6. Check the pelvis chain's `translation` afterwards
  and set it with `configure_fk`.
- It cannot change which rigs the retargeter uses. No operation of this tool
  can; create a new retargeter instead.
- An op you named `Something_2` yourself is removed as a duplicate when an op
  `Something` exists.

## configure_fk

Changes per-chain settings of the retargeter's first FK Chains op.

`chain_settings` is an array of objects:

| Key | Values |
|---|---|
| `chain_name` | a target chain name, compared without regard to case |
| `translation_mode` | `None`, `GloballyScaled`, `Absolute` |
| `rotation_mode` | `None`, `Interpolated`, `OneToOne`, `OneToOneReversed`, `MatchChain`, `MatchScaledChain`, and on UE 5.7 or later `CopyLocal` |
| `rotation_alpha` | number |
| `translation_alpha` | number |
| `enable_fk` | boolean |

The tool's parameter description names only the first three keys; the code
reads all six. The mode strings are compared without regard to case. A key
you leave out keeps its value.

Message: `Configured FK settings for <n> chains`.

- **An entry whose `chain_name` matches no chain is skipped without an
  error.** So is an entry that is not an object or has no `chain_name`.
- **A mode string the code does not know is ignored without an error**, and
  the entry still counts in `<n>`. `<n>` is the number of entries whose chain
  was found, not the number of changes.
- A call where nothing matched returns success with `0 chains`.
- Verify with `inspect_retargeter`: `fk_chains[].translation` and `.rotation`.

Errors: `No FK Chains op found in retargeter stack` (run `setup_ops`),
`Failed to access FK chain settings`,
`Missing required parameter: chain_settings (JSON array)`.

## batch_retarget

Runs the engine's duplicate-and-retarget batch over AnimSequences. Which
engine call that is depends on the engine version (section
`engine-versions`).

| Parameter | Default | Meaning |
|---|---|---|
| `retargeter_path` | required | the IK Retargeter |
| `source_mesh_path` | required | skeletal mesh of the source skeleton |
| `target_mesh_path` | required | skeletal mesh of the target skeleton |
| `anim_paths` | required | array of AnimSequence paths |
| `prefix` | `RTG_` | prefix of each new asset's name |
| `auto_root_motion` | `true` | enable root motion by name |
| `root_motion_pattern` | `RootMotion`, `Attack` and `Dodge`, joined by the pipe character | substrings, separated by the pipe character |

Returns `retargeted_count`, `root_motion_count`, `new_assets[]` (`name`,
`path`, and `root_motion_enabled: true` where it was set) and the message
`Retargeted <n> animations (<m> with root motion enabled)`.

- **Paths that do not load as an AnimSequence are dropped without a word.**
  Empty strings too. Only when none is left does the call fail:
  `No valid animation assets found to retarget`. Compare `retargeted_count`
  with what you sent, and match `new_assets[].name` against your list.
- **`retargeted_count: 0` is a success.** The tool reports whatever the engine
  returned.
- **Where the new assets land** is decided by the engine. On UE 5.6 and 5.7
  the tool passes only the prefix, no folder, and empty search, replace and
  suffix strings. In the UE 5.8 port it passes the prefix and sets no folder,
  search, replace or suffix at all. On UE 5.8 the engine call then keeps its
  default folder: a target folder is taken only when one is passed, and the
  "same folder as the source" option is off unless asked for. The default is
  the content root, so the new assets land directly in `/Game`, not beside
  the source animations and not beside the target mesh (UE 5.8.4 source, not
  run). The tool's own header comment says "auto-move results"; the tool's
  code moves nothing after the engine call, on any version. Take the paths
  from `new_assets[].path`.
- **Overwriting.** On UE 5.7 or later, UE 5.8 included, the tool asks the
  engine to overwrite existing output assets; on UE 5.6 it passes no such
  flag. A second run over the same input can therefore replace the first
  run's output.
- Referenced assets are not included: only the sequences themselves are
  duplicated.
- **Root motion by name.** With `auto_root_motion`, each new asset whose name
  contains one of the pattern's substrings, in any case, gets
  `bEnableRootMotion` set. The test runs on the **new** name, prefix
  included: a prefix that contains a pattern word turns root motion on for
  every asset. This path sets only that one flag (it leaves Force Root Lock
  alone, unlike `set_root_motion`) and never turns root motion off. For an
  exact result, pass `"auto_root_motion": false` and call `set_root_motion`
  per asset.
- The tool does not save the new assets.
- What the retarget itself does with IK goals, the Pelvis Motion op or
  mismatched proportions is the engine's work and cannot be read off the
  result. Check with `diagnose_anim` and `capture_viewport`.

Errors: the load errors of the retargeter and the two meshes.

## set_root_motion

Sets root motion on **one** AnimSequence, `asset_path`. The tool's description
says "anim(s)", but there is no array parameter.

- `"enable": true` (the default) sets `bEnableRootMotion` on and Force Root
  Lock off.
- `"enable": false` sets `bEnableRootMotion` off **and Force Root Lock on**.

Message: `Root motion enabled on <name>` or `Root motion disabled on <name>`.
The package is marked dirty and not saved. Read the flags back with
`inspect_anim` (`root_motion`, `force_root_lock`).

## find_anims

Lists AnimSequence assets in `folder_path` (required), in that folder only
unless `"recursive": true`.

Returns `count` and `animations[]` (`name`, `path`). It never fails: a folder
that does not exist gives `count: 0`. Write the folder without a trailing `/`.
Montages and blend spaces are not listed.

## save

Saves the asset at `asset_path`, whether its package is dirty or not. One
asset per call. Message `Saved: <path>`; error `Failed to save: <path>`, with
no reason (an asset that does not exist gives the same error).

The code is not specific to retargeting assets. After a batch, `asset`
`save_all` (skill `ue-assets`) saves every dirty package in one call. It skips
what was never marked dirty, and for a skeleton changed by `add_bone` that is
not verified (section `rigs`): save that one with `save`.
