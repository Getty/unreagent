# gameplay_debug: input, monitor, montages

These operations act on a running PIE session one call at a time. Start PIE
first (`start_pie`, then `pie_status` until `running` is `true`). For timed
input that has to land on exact frames, use `run_sequence` instead (section
`sequences`): every call here is a round trip through the bridge and the task
queue, so the time between two calls is not under your control.

## Where injected input goes

All input goes to the **first player controller's local player**, through its
Enhanced Input subsystem, as the value of one Input Action. The plugin hands
the value to the engine's injection call with no extra modifiers or triggers.
The game reacts only if the pawn's bindings use the action (skill
`ue-enhanced-input`). **Injection skips the key mapping and the mapping
context**: the engine stores the injected value with the caller's modifiers
and triggers, of which the plugin passes none, and hands it straight to the
action's processing, never through a mapping (UE 5.8.4 source,
`EnhancedPlayerInput.cpp:284-291`, `:646-667`, `:216`; the plugin's call at
`MCPTool_GameplayDebug.cpp:254`). So the action need not be in any applied
context, and a working injection says nothing about the key mapping.

The mapping's triggers and modifiers (Swizzle, Negate, Hold on the context)
therefore do **not** apply; the Input Action asset's own triggers and
modifiers still do (`EnhancedPlayerInput.cpp:683-694`). The plugin's
`gameplay_debug` description ("the full Enhanced Input pipeline (triggers,
modifiers, mappings)") is wrong on this; its `debug.md` ("Bypasses IMC
modifiers") matches the source. Send the value the game should receive
after the mapping's modifiers, such as `{x: 0, y: 1}` for forward, not a raw
key value.

## Values

`value_x`, `value_y` and `value_z` build the value by the Input Action's type:

| Action type | Value sent | Defaults |
|---|---|---|
| Digital | `true` when `value_x` is not 0, else `false` | `value_x` 1, so `true` |
| Axis1D | `value_x` | 1 |
| Axis2D | `(value_x, value_y)` | `(1, 0)` |
| Axis3D | `(value_x, value_y, value_z)` | `(1, 0, 0)` |

**`value_x` defaults to 1 for every type**, so an Axis2D call without values
sends `(1, 0)`, not zero. Give every axis you mean. Unlike the steps of
`run_sequence`, these operations honour `value_x: 0` on a digital action.

`action_path` is the Input Action's path, `/Game/Input/IA_Jump`; the object
name is added when the path has no dot. An action that does not load returns
`Failed to load InputAction: <path>`.

## inject_input

Sends the value once. Errors: `PIE is not running. Call start_pie first.`,
the load error, and `Enhanced Input subsystem not available in PIE` when the
first player has no Enhanced Input subsystem.

The result has `action`, `action_path` (the full object path) and
`frame_number`; the message is `Injected <action> at frame <n>`.

The plugin's notes call a single injection a single-frame one, and say a
digital action with a `Pressed` trigger needs the value held across frames to
fire; for that use `start_continuous` and stop it again, or a `hold` step in
`run_sequence`.

## Continuous injection

**`start_continuous`** sends the value every 1/60 s until you stop it. Its
checks and errors are those of `inject_input`. The result has `injection_id`
(`ci_1`, `ci_2`, …) and `action`; the message is
`Started continuous injection <id> for <action>`. Keep the id.

**`update_continuous`** replaces the value of a running injection. **The value
is built again from this call alone**: an axis you leave out falls back to its
default, so pass every axis each time. Errors:
`Continuous injection not found: <id>` and
`InputAction has been garbage collected`. The message is
`Updated continuous injection <id>`.

**`stop_continuous`** ends one injection. The result has `injection_id` and
`action` (`Unknown` when the action is gone); the message is
`Stopped continuous injection <id> (<action>)`. An unknown id returns
`Continuous injection not found: <id>`.

`stop_pie` ends every injection. When PIE ends any other way, the injections
stop sending, but their ids stay on the plugin's list, `pie_status` still
counts them in a later session, and `update_continuous` still accepts them.
Stop each one you started.

## The input monitor

The monitor watches while someone plays and writes what it sees to the log.
It drives nothing.

**`start_monitor`** parameters:

| Parameter | Default | Does |
|---|---|---|
| `start_pie` | false | ask for a PIE session first, if none runs |
| `interval_ms` | 500 | how often to log the pawn's state |
| `axis_threshold` | 0.15 | how far an axis must move to count as started or released |
| `log_axes` | false | log every axis value on every tick, for replay |

The result has `monitoring`, `pie_running`, `pie_attached`, `interval_ms` and
`axis_threshold`. The message is `Monitor started (PIE attached)` whenever PIE
runs, even when attaching failed: check `pie_attached`. Without PIE it is
`Monitor started (waiting for PIE)`. A monitor that is already active returns
`Monitor already active` with `already_active`, `elapsed_ms` and
`events_logged`, and its settings stay as they were.

The monitor checks 60 times a second. It attaches once PIE runs and the first
player's pawn has an Enhanced Input component, and tracks only the actions
bound on that component. Every line goes to the log with the category
`LogUnrealClaude` and the prefix `[PIE-DBG]`:

| Line | When |
|---|---|
| `MONITOR_START interval=<ms>ms threshold=<t> pie=attached\|waiting` | start |
| `t=<ms> ATTACHED actions=<n>`, `t=<ms> ACTION_DISCOVERED <action> type=<type>` | on attaching |
| `t=<ms> INPUT <action> triggered` / `released` | a digital action changes |
| `t=<ms> INPUT <action> started val=…` / `changed val=…` / `released` | an axis action moves |
| `t=<ms> STATE pos=(…) rot=(…) vel=(…) montage=<montage>:<section>`, or `montage=None`, then the active inputs | every `interval_ms` |
| `t=<ms> AXIS…` | every tick on which an action is active, with `log_axes` |
| `t=<ms> DETACHED` | PIE ended |
| `MONITOR_STOP duration=<s>s events=<n>` | `stop_monitor` |
| `MONITOR_AUTO_STOP pie_ended duration=<s>s events=<n>` | PIE ended, with `log_axes` |

**Across PIE sessions.** Without `log_axes`, the monitor detaches when PIE
ends and attaches again to the next session, as the tool's description says.
**With `log_axes`, it stops for good when PIE ends**, although the description
says it stays active. `log_axes` also runs `t.MaxFPS 60` on attaching; only
`stop_monitor` runs `t.MaxFPS 0`, so after an automatic stop expect the cap of
60 to stay (not verified) until you lift it (`run_console_command` with
`t.MaxFPS 0`).

**`stop_monitor`** returns `duration_s` and `events_logged`, and the message
`Monitor stopped: <s>s, <n> events`; without a monitor, `Monitor not active`.

**`monitor_status`** returns `active`, and while active `elapsed_ms`,
`events_logged`, `pie_attached`, `tracked_actions` (a count), `interval_ms`
and `axis_threshold`.

Read the lines with `get_output_log` and `filter: "[PIE-DBG]"` (section `log`).

**As a check on injection.** The monitor reads each tracked action's value
from the player input of the same Enhanced Input subsystem that
`inject_input` and `start_continuous` hand their values to. Running it while you inject shows
in the log whether the value arrived (`INPUT <action> triggered`); that
injected values show up there is the engine's behaviour and unverified.
Section `recipes`, "Input does not reach the pawn", has the whole ladder.
The monitor reads the pawn's bindings once, when it attaches; a pawn that
has no Enhanced Input component then is never attached (`pie_attached`
stays `false`, and it tries again every tick).

## Montages

The montage operations act on a skeletal mesh component of the first player's
pawn. `component_name` empty or left out means the Character's mesh, or the
pawn's first skeletal mesh component when the pawn is not a Character. A name
is matched exactly against the components' object names. No pawn, or no match,
returns `SkeletalMeshComponent '<name>' not found on pawn`; a component
without an Animation Blueprint instance returns
`No AnimInstance on component '<name>'`. Montage paths work like
`action_path`, and a montage that does not load returns
`Failed to load AnimMontage: <path>`.

**`play_montage`** plays `montage_path` at `play_rate` (default 1).
`start_section` then jumps to that section. Errors besides the common ones:
`No PIE session running. Call start_pie first.` and
`Montage_Play returned 0 — montage may be incompatible with skeleton`. The
result has `montage`, `component`, `duration`, `play_rate` and, when given,
`start_section`.

**`montage_jump_to_section`** jumps to `section_name` in `montage_path`, or in
the montage playing now. Without either it returns
`No active montage and no montage_path specified`; without PIE,
`No PIE session running.` The result has `section`, `montage` and `component`.

**Section names are not checked.** A `start_section` or `section_name` the
montage does not have still returns success. Confirm the jump with a capture,
or with the monitor's `STATE` lines, which name the montage and section playing
on a Character's mesh.

**`montage_stop`** stops the montages on that component, blending out over
`blend_out_time` seconds (default 0.25). The result has `stopped_montage`
(the montage that was active, or `None`), `component` and `blend_out_time`.
Without PIE it returns `No PIE session running.`
