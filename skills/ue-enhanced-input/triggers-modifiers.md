# enhanced_input: trigger and modifier types

Which mapping `add_trigger` and `add_modifier` change, that neither can be
removed, and "Check that they persist" are in the skill itself, under
"Triggers and modifiers".

**`trigger_type`:**

| Type | Settings | Accepted range |
|---|---|---|
| `Pressed`, `Released`, `Down` | — | — |
| `Hold`, `HoldAndRelease` | `hold_time` (default 0.5 s) | more than 0, at most 60 |
| `Tap` | `tap_release_time` (default 0.2 s) | more than 0, at most 5 |
| `Pulse` | `pulse_interval` (default 0.1 s) | more than 0.01, at most 10 |
| `ChordAction` | `chord_action_path` (required): the Input Action that must be active as well | — |

Out of range returns `hold_time must be between 0 and 60 seconds` and so on.
For `Pulse`, the message says "between 0.01 and 10", but 0.01 itself is
rejected. Other errors:
`ChordAction trigger requires 'chord_action_path' parameter`, the load errors
for the chord action, and
`Invalid trigger_type: <t>. Valid types: Pressed, Released, Down, Hold, HoldAndRelease, Tap, Pulse, ChordAction`.

**`modifier_type`:**

| Type | Settings |
|---|---|
| `Negate` | — |
| `Swizzle` | `swizzle_order`: `YXZ` (default), `ZYX`, `XZY`, `YZX`, `ZXY` |
| `Scalar` | `scalar`: `{x, y, z}`; an axis left out is 1 |
| `DeadZone` | `dead_zone_lower` (default 0.2), `dead_zone_upper` (default 1.0), each from 0 to 1 with lower below upper; `dead_zone_type`: `Radial`, any other value is axial |

Errors: `Invalid swizzle_order: <o>. Valid orders: YXZ, ZYX, XZY, YZX, ZXY`,
`dead_zone_lower must be between 0.0 and 1.0`, the same for `dead_zone_upper`,
`dead_zone_lower must be less than dead_zone_upper`,
`Invalid modifier_type: <m>. Valid types: Negate, Swizzle, Scalar, DeadZone`.
