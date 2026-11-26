# game_framework: the PIE player controller operations in full

`set_input_mode`, `show_widget`, `remove_widget`, `set_pause` and
`set_mouse_cursor` act on a running Play In Editor session
(`GEditor->PlayWorld`, `GameFrameworkEditor.cpp`). They are a test harness:
everything they change is state of that session and is gone when PIE stops.
The game's own behaviour (a menu that pauses, a HUD at start) has to be
built in Blueprint or C++: skills `ue-blueprints`, `ue-widgets`.

Without PIE each returns
`PIE is not running. Player Controller operations require Play-In-Editor.`
`start_pie` is asynchronous: wait until `pie_status` reports it running
(skill `ue-pie-testing`). All but `remove_widget` use the session's **first**
player controller, and return `No player controller found in PIE world`
when there is none; with several PIE players, only player 0 is reached.

## set_input_mode

`input_mode`: `game_only`, `ui_only` or `game_and_ui`, lowercased by the
tool. Each calls the engine's `SetInputMode` with a default-constructed
`FInputModeGameOnly`, `FInputModeUIOnly` or `FInputModeGameAndUI`: no widget
gets focus, and no mouse lock or capture option can be passed. Result:
`input_mode`. Any other value:
`Invalid input_mode: '<v>'. Valid: game_only, ui_only, game_and_ui`.

| Mode | Game input (Enhanced Input actions) | UI input | Use |
|---|---|---|---|
| `game_only` | yes | no | normal play |
| `ui_only` | **no** | yes | a pause or main menu |
| `game_and_ui` | yes | yes | a HUD with clickable parts, inventory over the running game |

The input mode does not show the cursor: that is `set_mouse_cursor`. A menu
widget usually needs `ui_only` plus the cursor; going back needs
`game_only` and the cursor hidden, or game input stays blocked.

## show_widget

`widget_class` (required), `z_order` (number, default 0; higher is drawn on
top). Loads the class with `LoadClass<UUserWidget>`, creates an instance
owned by the first player controller (`CreateWidget`) and calls
`AddToViewport(z_order)`. Result: `widget_class`, `z_order`.

- **Each call adds one more instance**; there is no check for one already
  showing.
- The widget's variables cannot be set, and no reference to the instance
  comes back. A widget whose `Construct` needs data from its creator shows
  defaults.
- Errors: `Failed to load widget class: <path>` (asset path instead of class
  path, or not a user widget), `Failed to create widget of class: <path>`.

## remove_widget

`widget_class` (required). Iterates every live object of the class **and its
subclasses** (`ForEachObjectOfClass`, which includes derived classes by
default) and calls `RemoveFromParent` on each that is in the viewport —
whoever created it, the game's own HUD of that class too. Result:
`removed_count`, `widget_class`. It succeeds with `removed_count: 0` when
nothing was showing; read the count. It uses no player controller, so it
never returns the controller error. There is no "remove all classes"
operation.

## set_pause

`paused`, default **`true`**. Calls the controller's `SetPause`. Result:
`paused` as requested and the message `Game paused` / `Game unpaused`. The
engine's answer is not checked: the result says `paused: true` also when the
game refuses to pause (for example when the game mode's pause rules say no;
the details are the engine's). While paused, Enhanced Input actions trigger
only if their Input Action has `Trigger when Paused` (skill
`ue-enhanced-input`, section `runtime-setup`).

The aliases `pause` and `unpause` both run `set_pause` with its default:
**`unpause` without `paused: false` pauses.**

## set_mouse_cursor

`show_cursor`, default `true`. Sets the controller's `bShowMouseCursor`.
Result: `show_cursor`.

## Booleans

Send `paused` and `show_cursor` as JSON `true` / `false`. Other values are
read leniently, not refused; section `failures` has how.
