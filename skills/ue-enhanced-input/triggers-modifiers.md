# enhanced_input: triggers and modifiers

Which mapping `add_trigger` and `add_modifier` change is in section
`operations`; that neither survives a reload is in the skill itself. This
section has the values the tool accepts and what each does in the engine.

## What a trigger decides

A trigger decides when an action fires; the Blueprint or C++ code then
reacts to one of the engine's trigger events (`ETriggerEvent`, read in the
UE 5.8.4 `InputTriggers.h`):

| Event | Fires when |
|---|---|
| `Started` | evaluation begins (first frame of a press); always before `Triggered` |
| `Ongoing` | still evaluating, e.g. a Hold not held long enough yet |
| `Triggered` | the triggers are met. **Can fire every frame** |
| `Completed` | Triggered → none: released after triggering |
| `Canceled` | Ongoing → none: let go before a Hold finished |

**A mapping with no trigger is Triggered every frame the input is
non-zero** (`SetStateForNoTriggers`, `EnhancedPlayerInput.cpp`, UE 5.8.4).
That is right for movement and look (`Axis2D`, react on `Triggered`), wrong
for a one-shot such as jump: react on `Started` instead, or add a `Pressed`
trigger. The engine's Third Person template binds Jump on `Started` and
StopJumping on `Completed`, Move and Look on `Triggered`
(`TP_ThirdPersonCharacter.cpp`, UE 5.8.4).

Explicit triggers (Pressed, Released, Down, Hold, HoldAndRelease, Tap, Pulse)
fire when **any** of them fires; implicit ones (ChordAction) must **all**
fire as well.

## trigger_type

The match is case-sensitive.

| Type | Engine class | Settings | Accepted range | Typical use |
|---|---|---|---|---|
| `Pressed` | `InputTriggerPressed` | — | — | fire once on press |
| `Released` | `InputTriggerReleased` | — | — | fire once on release |
| `Down` | `InputTriggerDown` | — | — | every frame while held |
| `Hold` | `InputTriggerHold` | `hold_time` (default 0.5 s) | more than 0, at most 60 | charge, long press |
| `HoldAndRelease` | `InputTriggerHoldAndRelease` | `hold_time` (default 0.5 s) | more than 0, at most 60 | fire on release after a hold |
| `Tap` | `InputTriggerTap` | `tap_release_time` (default 0.2 s) | more than 0, at most 5 | quick tap only |
| `Pulse` | `InputTriggerPulse` | `pulse_interval` (default 0.1 s) | more than 0.01, at most 10 | repeat while held |
| `ChordAction` | `InputTriggerChordAction` | `chord_action_path` (required) | an Input Action that loads | fire only while another action (a modifier key such as Shift) is active |

The tool sets only the setting named; other properties of the trigger (the
Hold's "Is One Shot", the Pulse's "Trigger Limit", the actuation threshold)
keep the engine's defaults.

Errors: `hold_time must be between 0 and 60 seconds`,
`tap_release_time must be between 0 and 5 seconds`,
`pulse_interval must be between 0.01 and 10 seconds` (0.01 itself is
rejected), `ChordAction trigger requires 'chord_action_path' parameter`, the
load errors for the chord action, and
`Invalid trigger_type: <t>. Valid types: Pressed, Released, Down, Hold, HoldAndRelease, Tap, Pulse, ChordAction`.
Engine triggers the tool has no type for (Chord Blocker, Combo — deprecated
in 5.8 —, custom triggers) are set in the editor.

## modifier_type

Modifiers change the value before the triggers see it, in the order they
were added.

| Type | Engine class | Settings | Typical use |
|---|---|---|---|
| `Negate` | `InputModifierNegate` | — (negates all axes) | `S` and `A` on a WASD `Axis2D` move |
| `Swizzle` | `InputModifierSwizzleAxis` | `swizzle_order`: `YXZ` (default), `ZYX`, `XZY`, `YZX`, `ZXY` | `W`/`S` on a WASD move: a 1D key value lands on X; `YXZ` moves it to Y |
| `Scalar` | `InputModifierScalar` | `scalar`: `{x, y, z}`; an axis left out is 1 | mouse sensitivity, invert one axis with `-1` |
| `DeadZone` | `InputModifierDeadZone` | `dead_zone_lower` (0.2), `dead_zone_upper` (1.0), each 0 to 1, lower below upper; `dead_zone_type` | stick drift |

`dead_zone_type`: exactly `Radial` gives radial; **any other value, a typo
included, gives axial** without an error. `swizzle_order` and the type names
are case-sensitive.

The WASD pattern for one `Axis2D` move action (as in the engine's templates;
the template assets are binary, so this layout is unverified here):
`W` Swizzle `YXZ`; `S` Swizzle `YXZ` and Negate; `A` Negate; `D` none. Pass
`mapping_index` on every call (section `operations`).

Errors: `Invalid swizzle_order: <o>. Valid orders: YXZ, ZYX, XZY, YZX, ZXY`,
`dead_zone_lower must be between 0.0 and 1.0`, the same for
`dead_zone_upper`, `dead_zone_lower must be less than dead_zone_upper`,
`Invalid modifier_type: <m>. Valid types: Negate, Swizzle, Scalar, DeadZone`.

## Why they do not survive a reload

The tool creates each trigger and modifier with `NewObject<…>()` and no outer
(`MCPTool_EnhancedInput.cpp`, `CreateTrigger` / `CreateModifier`), so it
lives in the engine's transient package. The mapping's `Triggers` and
`Modifiers` are `Instanced` properties (`EnhancedActionKeyMapping.h`), which
the editor fills with objects inside the context asset. On save, the engine
treats an object whose outer chain is transient as unsaveable and writes the
reference as empty (`FSaveContext::IsTransient`,
`PackageHarvester.cpp`, UE 5.8.4). Read in the source, not run: expect the
trigger or modifier to work in this editor session, including PIE, and to be
gone once the asset is loaded again; `query_context` skips empty entries.

What to do instead, for anything that must last:

1. set it in the editor's Input Mapping Context asset by hand, and tell the
   user so; or
2. put it on the Input Action asset, where it applies to every mapping (also
   by hand: the tool cannot set an action's triggers);
3. a script route through `execute_script` (skill `ue-python`) that creates
   the object with the context as outer is possible in principle but has not
   been exercised for this skill.

Verify by restarting the editor (skill `unreagent-iterate`) and running
`query_context`.
