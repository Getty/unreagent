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
