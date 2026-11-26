# widget_editor: end-to-end workflows

Recipes that span several tools. Each says which tool does which part. The
tool facts come from the toolkit's code; the UMG facts carry their source or
the label unverified. None of these sequences has been run end to end for this
skill: check each step's result before the next.

Operations, paths and the edit loop are in the skill itself; the keys of
`set_property` / `set_slot` in section `properties`, placements in section
`layout`, the binding choice in section `design`.

## A HUD that shows gameplay data (health, score)

**What the tools can do, and what they cannot.** `widget_editor` builds the
layout completely. The data path is the hard part: `blueprint_modify` cannot
reach the widgets of a Widget Blueprint from a graph (its `VariableGet`
looks only at Blueprint variables and the parent class), has no Create Widget
node, and `add_function` makes functions without a return value, so it cannot
write a binding function either (section `events`). Pick the route first
(section `design`):

- **C++ parent class (recommended when the project has C++).** A `UUserWidget`
  subclass declares the widgets with `UPROPERTY(meta = (BindWidget))` and
  pushes values in its own functions; `widget_editor` builds the tree with
  the same widget names and classes. Build with skill `unreagent-build`.
- **Blueprint-only.** `widget_editor` builds the layout; the update logic
  (Create Widget, the widget reference, the function that sets the value) is
  written in the editor by hand, or the work is handed back with that note.
- **Static test only.** No data at all: lay it out, set preview values with
  `set_property`, show it in PIE with `show_widget`.

**Steps (C++ parent route).**

1. In C++ (not this tool): `UMyHUDWidget : UUserWidget` with
   `UPROPERTY(meta = (BindWidget)) UProgressBar* HealthBar;` and
   `UTextBlock* ScoreText;`, plus a `BlueprintCallable` function such as
   `SetHealth(float Percent)` that calls `HealthBar->SetPercent`. Who calls it
   (the pawn on damage, a delegate) is game code. Build and restart the
   editor (`unreagent-build`, `unreagent-iterate`).
2. `create` with `name: "WBP_HUD"`, `package_path: "/Game/UI"`,
   `parent_class: "/Script/<Module>.MyHUDWidget"`.
3. `batch`: `add_widget` `ProgressBar` named exactly `HealthBar`, `TextBlock`
   named exactly `ScoreText`; `set_slot` from section `layout` (top-left bar,
   top-right score); `set_property` preview values (`percent: 1.0`,
   `text: "0"`, `font_size: 28`).
4. `blueprint_modify compile` with the same path (it takes `asset_path` as
   an alias of `blueprint_path`). A `BindWidget` property without a
   widget of that name and class fails the compile (the exact wording is
   unverified); fix the name or class and compile again.
5. `save`.
6. Show it: game code creates it (Create Widget, Add to Viewport) at
   BeginPlay, usually in the player controller or the `AHUD` subclass. For a
   test without that code, `game_framework show_widget` with
   `widget_class: "/Game/UI/WBP_HUD.WBP_HUD_C"` while PIE runs (skill
   `ue-game-framework`).
7. Verify (below).

The game mode's `hud_class` (`game_framework set_game_mode_defaults`) takes an
`AHUD` class, not a Widget Blueprint. An `AHUD` Blueprint or C++ class is one
common place to create the widget (unverified as a convention; any actor or
controller can).

## A pause menu or main menu

1. `create` `WBP_PauseMenu` with the default `CanvasPanel` root.
2. `batch`: a `VerticalBox` named `MenuBox`, centered (section `layout`); in
   it `Button` `ResumeButton` and `QuitButton`, each with a `TextBlock` child
   (`parent_name` the button) holding the label. Button slots are
   `VerticalBoxSlot`: `padding` for spacing, `h_align: "Fill"`.
   The text inside a button has a `ButtonSlot`, which `set_slot` does not
   support.
3. Style: `set_brush` on each button (`style_normal`, `style_hovered`,
   `style_pressed`), `tint` per state.
4. `blueprint_modify compile`, then `bind_event` with `event_name: "OnClicked"`
   for each button. Each result's `node_name` is the event node.
5. Wire the handlers with `blueprint_modify` (skill `ue-blueprints`, section
   `graphs`): `CallFunction` nodes after the event, such as
   `SetGamePaused` (`GameplayStatics`), `RemoveFromParent`
   (`target_class: "/Script/UMG.Widget"`), `QuitGame`
   (`KismetSystemLibrary`) or `OpenLevel` (`GameplayStatics`). Section
   `events` lists what fails here.
6. `blueprint_modify compile`, `save`.
7. In PIE (`game_framework`, skill `ue-game-framework`): `show_widget`, then
   `set_input_mode` with `input_mode: "ui_only"` (or `game_and_ui`),
   `set_mouse_cursor` with `show_cursor: true`, `set_pause` with
   `paused: true`. Resume reverses all three; in the game this is graph or
   C++ code
   (`SetInputModeUIOnly`/`SetInputModeGameOnly` live in
   `WidgetBlueprintLibrary`, unverified as reachable through
   `CallFunction`).

A paused game still routes input to UMG (unverified; it is the usual pattern
for pause menus). Common UI handles the input mode per activatable widget
instead (section `design`).

## Verifying

Check in this order; stop at the first that fails.

1. **The tree.** `inspect`: names, classes, `slot_type` and `slot_properties`
   as intended. Colors come back as sRGB `hex` (section `properties`).
2. **Bindings.** `list_bindings` and `list_events` for what you bound.
3. **Compile.** `blueprint_modify compile`; read its errors and warnings
   (skill `ue-blueprints`: the call can succeed with a failed compile, so
   read the diagnostics).
4. **Saved.** `save`, or `asset save_asset`. An unsaved change is gone after
   an editor restart.
5. **On screen.** Start PIE (skill `ue-pie-testing`), `show_widget` if game
   code does not create it, then `capture_viewport` (skill `ue-viewport`).
   The capture reads the viewport's pixels; whether widgets in the viewport
   appear in it is unverified. If the HUD is missing from the picture, do not
   conclude it is missing on screen.
6. **Behaviour.** Add a `PrintString` to a handler (`blueprint_modify`
   `add_node` with `node_type: "PrintString"`) and read it back with
   `get_output_log` after a click; drive gameplay input with `gameplay_debug`
   (skill `ue-pie-testing`). Look for `LogUMG` and `LogBlueprint` warnings in
   the same log.
7. `remove_widget` (game framework) and stop PIE.
