# enhanced_input: key aliases

The rules of `add_mapping`, and what a name the engine does not know returns,
are in the skill itself, under "Mappings".

`key` is an engine key name, such as `SpaceBar`, `W`, `LeftShift`,
`Gamepad_FaceButton_Bottom` or `Gamepad_Left2D`, or one of these aliases:

| Alias | Key |
|---|---|
| `LMB`, `RMB`, `MMB` | `LeftMouseButton`, `RightMouseButton`, `MiddleMouseButton` |
| `Cross`, `FaceA` | `Gamepad_FaceButton_Bottom` |
| `Circle`, `FaceB` | `Gamepad_FaceButton_Right` |
| `Square`, `FaceX` | `Gamepad_FaceButton_Left` |
| `Triangle`, `FaceY` | `Gamepad_FaceButton_Top` |
| `L1`, `LB` / `R1`, `RB` | `Gamepad_LeftShoulder` / `Gamepad_RightShoulder` |
| `L2`, `LT` / `R2`, `RT` | `Gamepad_LeftTrigger` / `Gamepad_RightTrigger` |
| `L2Axis`, `LTAxis` / `R2Axis`, `RTAxis` | `Gamepad_LeftTriggerAxis` / `Gamepad_RightTriggerAxis` |
| `L3` / `R3` | `Gamepad_LeftThumbstick` / `Gamepad_RightThumbstick` (stick click) |
| `LeftStickX`, `LeftStickY` | `Gamepad_LeftX`, `Gamepad_LeftY` |
| `RightStickX`, `RightStickY` | `Gamepad_RightX`, `Gamepad_RightY` |
| `LeftStick2D` / `RightStick2D` | `Gamepad_Left2D` / `Gamepad_Right2D` |
| `DPadUp`, `DPadDown`, `DPadLeft`, `DPadRight` | `Gamepad_DPad_Up`, `Gamepad_DPad_Down`, `Gamepad_DPad_Left`, `Gamepad_DPad_Right` |
| `Start`, `Options`, `Menu` | `Gamepad_Special_Right` |
| `Select`, `Share`, `View` | `Gamepad_Special_Left` |

The aliases sit in a `TMap` keyed by `FString`, whose lookup ignores case,
so `lmb` should work as well (read, not run); write them as shown. The list in
the `Invalid key name` message names only some of them; this table is the
whole list (`ParseKey`, `MCPTool_EnhancedInput.cpp`).

Common engine key names, for reference: letters `A`–`Z`, `SpaceBar`,
`LeftShift`, `LeftControl`, `LeftAlt`, `Tab`, `Escape`, `Enter`,
`One`–`Zero` for the number row, `Up`/`Down`/`Left`/`Right` for the arrow
keys, `Mouse2D` (mouse movement as one 2D value), `MouseX`, `MouseY`,
`MouseWheelAxis`, `Gamepad_Left2D` / `Gamepad_Right2D` (a whole stick). Any
key `FKey` knows is accepted; the result's `key` echoes what you sent, and
`query_context` shows the engine name.
