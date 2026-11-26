# montage_modify: workflows

End-to-end recipes. Every call shown exists in the toolkit's source; none has
been run against a live editor. Paths are examples. Read the indices from the
data each write returns, not from these examples.

## 1. An attack montage with a hit window

Goal: `AM_Attack` from `A_Swing`, a damage window, a sound cue point, blends.

1. **Find inputs.** `asset_search` for the skeleton and the sequence (skill
   `ue-assets`). Note the object paths.
2. **Create.**
   ```json
   {"operation": "create", "montage_path": "/Game/Anims/AM_Attack",
    "package_path": "/Game/Anims", "montage_name": "AM_Attack",
    "skeleton_path": "/Game/Characters/SK_Hero_Skeleton.SK_Hero_Skeleton"}
   ```
   The new montage has slot `DefaultSlot`, section `Default` at 0, length 0
   (section `structure`).
3. **Add the animation** — before any notify, because notify times are
   checked against the length the segments give:
   ```json
   {"operation": "add_segment", "montage_path": "/Game/Anims/AM_Attack.AM_Attack",
    "animation_path": "/Game/Anims/A_Swing.A_Swing", "start_pos": 0}
   ```
   Check `sequence_length` in the returned data.
4. **Hit window as a notify state.** Use your project's notify state class
   (a Blueprint class path ends in `_C`) or the engine's Montage Notify Window
   (see 3):
   ```json
   {"operation": "add_notify_state", "montage_path": "/Game/Anims/AM_Attack.AM_Attack",
    "notify_name": "HitWindow", "start_time": 0.35, "duration": 0.2,
    "notify_class": "/Game/Combat/ANS_HitWindow.ANS_HitWindow_C"}
   ```
   `start_time`, not `trigger_time` — the tool description's example is wrong.
5. **Blends.** `set_blend_in` `{"blend_time": 0.1}`, `set_blend_out`
   `{"blend_time": 0.2, "blend_option": "CubicInOut"}`. Spell the option
   exactly; a typo becomes `Linear` silently.
6. **Verify, then save.** `get_info`: one segment, `notifies` holds
   `HitWindow` with `is_state: true`, the right `notify_state_class` and
   `duration`. Then `save`.
7. **Play it.** The character's Animation Blueprint needs a Slot node for
   `DefaultSlot` between its locomotion and the output pose (step 4 of
   workflow 4). Gameplay plays the montage; this tool does not.

## 2. A three-hit combo with sections

Goal: one montage, sections `Default` (hit 1), `Hit2`, `Hit3`; each hit ends
the montage unless the player presses attack inside a combo window.

1. Create as above and add the three swings as segments, appended in order
   (`add_segment` without `start_pos` appends). Read each segment's
   `start_pos` from the result.
2. Add the sections **in time order**, each at the `start_pos` of its swing:
   `add_section` `{"section_name": "Hit2", "start_time": <pos of swing 2>}`,
   then `Hit3`. The engine links each previous unlinked section to the new
   one, so you now have `Default -> Hit2 -> Hit3`: the montage would play
   all three hits in a row.
3. **Break the chain**: `link_sections` `{"section_name": "Default"}` and
   `{"section_name": "Hit2"}` with no `next_section`. Check `get_info`:
   every `next_section` is `None`.
4. **Combo windows**: a notify state per hit, near the end of each swing
   (`add_notify_state`, `notify_name: "ComboWindow"`, a Montage Notify Window
   or your own class). Inside the window, gameplay calls
   `Montage_SetNextSection("Default", "Hit2", Montage)` (or
   `Montage_JumpToSection`); outside it the section ends and the montage
   blends out.
5. Save.

Decision: **linked chain** when a sequence always plays through (intro, loop,
outro); **unlinked sections plus gameplay calls** when input decides. A loop
section (charge, hold) links to itself; that the engine then loops it is
unverified here.

## 3. Notifies that gameplay reacts to

| Need | Class (`notify_class`) | Fires | Set with `properties` |
|---|---|---|---|
| Event on the Play Montage node's `OnNotifyBegin` pin | `/Script/AnimGraphRuntime.AnimNotify_PlayMontageNotify` (Montage Notify) | once | `NotifyName` (name) |
| Begin and end on the Play Montage node | `/Script/AnimGraphRuntime.AnimNotify_PlayMontageNotifyWindow` (Montage Notify Window, a notify state: `add_notify_state`) | at start and end | `NotifyName` |
| Plain named event (the anim instance's `AnimNotify_<Name>` event) | none: omit `notify_class` (see section `notifies` on the skeleton list) | once | — |
| Your own logic | your Blueprint notify (`…/AN_Foo.AN_Foo_C`) or C++ class | once / over a range | its properties |
| Let input steer during part of a montage | `AnimNotifyState_DisableRootMotion` (`add_notify_state`) | over a range | — |

- The Montage Notify classes are declared in the engine's `AnimGraphRuntime`
  module (`AnimNotify_PlayMontageNotify.h`: `UAnimNotify_PlayMontageNotify`
  and `UAnimNotify_PlayMontageNotifyWindow`, each with an `FName NotifyName`).
  Their class paths are therefore
  `/Script/AnimGraphRuntime.AnimNotify_PlayMontageNotify` and
  `/Script/AnimGraphRuntime.AnimNotify_PlayMontageNotifyWindow`. A short name
  only resolves for `Engine` module classes, so always pass these full paths.
- Epic's notify page: Montage Notify events reach the **Play Montage** node's
  On Notify Begin / End pins, not the Play Anim Montage node. Both Montage
  Notify classes set `bIsNativeBranchingPoint` (engine source), so they fire
  synchronously although the tool cannot set the tick type.
- **Sound and particle notifies cannot be wired with this tool.** Their
  `Sound` / `PSTemplate` properties reference assets, and the tool's object
  values always create a new object instead of pointing at an asset (section
  `notifies`). Add them with the `Sound` field empty and tell the user, or use
  a Blueprint notify whose sound is set in its class defaults.

## 4. Upper-body montage (attack while running)

1. In the montage: `add_slot` `{"slot_name": "UpperBody"}`, then move the
   segments there — `add_segment` with the new `slot_index`, and
   `remove_slot` the empty `DefaultSlot` track (index 0) once the other track
   exists. The montage's group follows its **first** slot track.
2. Register `UpperBody` on the skeleton's slots (Anim Slot Manager). This tool
   cannot; without it the slot counts as `DefaultGroup` (engine source), so
   it stops and is stopped by other `DefaultGroup` montages. Whether Python
   can register it (skill `ue-python`) is unverified here.
3. In the AnimBP (skill `ue-anim-blueprint`): locomotion → a Slot node
   `UpperBody` → Layered Blend Per Bone (base: locomotion, blend pose: the
   slot output, branch filter at the spine) → output. `add_anim_node` types
   `slot` and `layered_blend_per_bone` exist.

## 5. Root motion attacks

A montage moves the capsule only if (a) the sequence has Enable Root Motion
on, and (b) the AnimBP's Root Motion Mode is "Root Motion from Montages Only"
(the engine default, `UAnimInstance` constructor) or "from everything".

- Check (a): `retarget` `inspect_anim` `{"asset_path": "<sequence>"}` returns
  `root_motion` and `force_root_lock`; `retarget` `set_root_motion`
  `{"asset_path": …, "enable": true}` turns it on (it does not save; skill
  `ue-retarget`).
- Read the root bone's movement with `anim_edit` `inspect_track`
  `{"bone_name": "root", "sample_frames": [0, -1]}` (skill
  `ue-anim-sequence`): no translation means nothing to extract.
- `AnimNotifyState_DisableRootMotion` suspends root motion for its range.

## Verify your own result

After the last write, one `get_info` and check:

- `sequence_length` equals the end of the last segment you meant to have;
- every section's `start_time` is inside it, `length` is positive (else the
  list is out of time order), `next_section` is what you planned;
- every notify's `trigger_time + duration` is inside the length, it has the
  class you asked for (`notify_class` / `notify_state_class`), and sits on the
  track you meant;
- the slot names match the Slot nodes in the AnimBP (`blueprint_query` on the
  AnimBP, skill `ue-anim-blueprint`).

Then `save`, and if the editor runs PIE, play it (skill `ue-pie-testing`).

## Sources

- Toolkit: `MCPTool_MontageModify.{h,cpp}`, `MontageEditor.cpp`,
  `MCPTool_Retarget.cpp` / `RetargetEditor.cpp` (`inspect_anim`,
  `set_root_motion`), `MCPTool_AnimBlueprintModify.h` (`add_anim_node` types).
- Engine, UE 5.8.4 source, read not run: `AnimMontage.cpp`,
  `AnimInstance.cpp` (Root Motion Mode default, `Montage_JumpToSection`,
  `Montage_SetNextSection`), `AnimGraphRuntime/.../AnimNotify_PlayMontageNotify.{h,cpp}`,
  `AnimNotifyState_DisableRootMotion.cpp`, `Skeleton.cpp`.
- Epic documentation (UE 5.9 pages):
  https://dev.epicgames.com/documentation/unreal-engine/animation-notifies-in-unreal-engine,
  https://dev.epicgames.com/documentation/en-us/unreal-engine/animation-slots-in-unreal-engine,
  https://dev.epicgames.com/documentation/en-us/unreal-engine/root-motion-in-unreal-engine.
