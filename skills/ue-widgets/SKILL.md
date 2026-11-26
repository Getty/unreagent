---
name: ue-widgets
description: >
  Build and edit Widget Blueprints (UMG) with widget_editor: the widget tree,
  widget properties, layout slots, brushes, bound events, property bindings
  and widget animations. Use before you create a Widget Blueprint or change
  its widgets, layout or animations.
toolkit_tools: [widget_editor]
---

# UE widgets: widget_editor

The UE LLM Toolkit plugin's **`widget_editor`** tool reads and edits Widget
Blueprints (UMG): the widget tree, each widget's properties and layout slot,
brushes, delegate events, property bindings and widget animations. Through the
plugin's MCP bridge it is listed as `unreal_widget_editor`. It works only
while the editor is running and the plugin is reachable; `unreagent-iterate`
covers the wait for the plugin.

It edits the **design** of a widget. It does not put a widget on screen, does
not write the logic behind it, and does not change the input mode: those are
`ue-game-framework` (in PIE), `ue-blueprints` (graphs) and game code.

## Sections

| Section | Holds | Load it |
|---|---|---|
| `workflows` | end-to-end recipes: a HUD bound to gameplay data, a pause or main menu with buttons, and how to verify each (compile, PIE, capture, log) | before you build a HUD or a menu, or to check one you built |
| `design` | decision rules: event-driven updates vs property bindings, Canvas vs box panels, C++ parent class vs Blueprint-only vs Python, UMG Viewmodel and Common UI | before you decide how a widget gets its data, or which panel to use |
| `layout` | anchors, alignment, position and size on a Canvas Panel, box fill rules, padding, with ready slot objects for the common placements | before your first `set_slot` on a widget you want placed somewhere specific |
| `properties` | the keys of `set_property`, `set_slot` and `set_brush`, color formats, which keys `get_properties` reports back | before your first `set_property`, `set_slot` or `set_brush`: the keys are only there |
| `events` | `list_events`, `bind_event`, `bind_property`, `unbind_property`, `list_bindings`, and what `blueprint_modify` can and cannot wire in a Widget Blueprint graph | before you bind an event or a property, or wire a handler |
| `animations` | `list_animations`, `inspect_animation`, `create_animation`, `add_animation_track`: track types, keyframe form, result fields | before you create an animation or add a track |
| `tree` | `reparent_widget`, `reorder_child` and `clone_widget` in full: result fields and errors | when one of the three returns an error, or you need its result fields |
| `failures` | the error table, and the failure table (what you see, what it means, what to do) | when a call returns an error you cannot place, or what you see is not what you expect |

## Paths

Every operation except `create` needs `asset_path`. There is no
`blueprint_path` alias. The path goes to the engine's loader unchanged,
without the path checks and the `/Game/` fallback of the Blueprint tools
(skill `ue-blueprints`). `create` and `save` report the object path, such as
`/Game/UI/WBP_HUD.WBP_HUD`. Use that form.

- `Failed to load Widget Blueprint: <path>`: nothing loads at that path.
- `Asset is not a Widget Blueprint: <path> (is <class>)`: the asset is
  something else, such as an ordinary Blueprint.

To find Widget Blueprints, use `blueprint_query list` with `type_filter`. It
matches a substring of the parent class: `UserWidget` finds widgets made
directly from `UserWidget`; one made from a C++ subclass or a Common UI class
needs part of that class name (or `Widget`, which catches most).

Where a class is wanted instead (`show_widget` in `ue-game-framework`, a
`parent_class` made from another Widget Blueprint), pass the generated class:
`/Game/UI/WBP_HUD.WBP_HUD_C`.

## Operations

The tool takes an `operation` string and lowercases it before dispatch.
`reorder_children` is accepted for `reorder_child`. An unknown operation
returns `Unknown operation: '<op>'. Valid: <list>`.

| Operation | Required besides `asset_path` | Optional |
|---|---|---|
| `inspect` | — | — |
| `get_properties` | `widget_name` | — |
| `create` | `name`, `package_path` (no `asset_path`) | `parent_class`, `root_widget_class` |
| `add_widget` | `widget_class`, `widget_name` | `parent_name` |
| `remove_widget` | `widget_name` | — |
| `set_property` | `widget_name`, `properties` | — |
| `set_slot` | `widget_name`, `slot_properties` | — |
| `set_brush` | `widget_name`, `brush` | `brush_property` |
| `reparent_widget` | `widget_name`, `new_parent_name` | — |
| `reorder_child` | `parent_name`, `child_name`, `new_index` | — |
| `clone_widget` | `widget_name`, `new_name` | `target_parent` |
| `list_events` | `widget_name` | — |
| `bind_event` | `widget_name`, `event_name` | — |
| `bind_property` | `widget_name`, `property_name`, `function_name` | — |
| `unbind_property` | `widget_name`, `property_name` | — |
| `list_bindings` | — | — |
| `list_animations` | — | — |
| `inspect_animation` | `animation_name` | — |
| `create_animation` | `animation_name` | `length` |
| `add_animation_track` | `animation_name`, `widget_name`, `track_type`, `keyframes` | — |
| `save` | — | — |
| `batch` | `operations` | — |

`property_name` and `child_name` are not in the tool's parameter schema, but
the code requires them. Pass them anyway. A missing string parameter returns
`Missing required parameter: <name>`. A missing object or array names its
type, such as `Missing required parameter: properties (JSON object)`.

## Reading

- **`inspect`** returns `asset_path`, `blueprint_name`, `total_widgets` and
  `widget_tree`: the root widget, or the string `empty`. Each widget has
  `name`, `class`, `slot_type` and `slot_properties` (not on the root),
  `visual_properties`, `bindings` (`property`, `function`, `kind`) when it has
  any, and `children` for a panel.
- **`get_properties`** returns one widget: `widget_name`, `widget_class`,
  `visual_properties`, and `slot_type` with `slot_properties` unless it is the
  root. Error: `Widget not found: <n>`.

Widget names are unique in a Widget Blueprint, and every other operation
addresses widgets by them. Read the tree before you change it.

## The edit loop

1. **Create or find the asset.** `create`, or `blueprint_query list`.
2. **Read the tree.** `inspect`.
3. **Change it.** `add_widget`, then `set_slot` and `set_property`, or several
   of these in one `batch`. Compare the keys listed in each result's message
   with the keys you sent: unknown keys are dropped without an error.
4. **Compile.** `blueprint_modify compile` with the same `asset_path` compiles
   the Widget Blueprint and reports errors and warnings (skill
   `ue-blueprints`). Only `bind_event` compiles by itself, and it reports no
   compile result. Compile before PIE: play runs the compiled class.
5. **Save.** No operation saves except `save`, not even `batch`. Until then
   every change lives only in the running editor. `save` writes the asset
   (`Saved: <path>`, or `Failed to save: <path>`). The `asset` tool's
   `save_asset` and `save_all` work as well.
6. **Verify.** `inspect` again, then look at it: section `workflows`,
   "Verifying".

`widget_editor` has no undo. Remove a widget with `remove_widget`, a binding
with `unbind_property`, and a bound event's node with
`blueprint_modify delete_node`. There is no operation to remove an
animation or a track.

## Must not get wrong

- **Unknown keys and unparsable values are dropped silently.** Only when no
  key applied do `set_property` and `set_slot` fail. Read the applied list in
  the message (section `properties`).
- **Colors: hex is read as linear, but reported as sRGB.** `#808080` in, a
  different `hex` back from `get_properties`. A string that is not a known
  name is read by length: 6 or 8 characters as hex with non-hex characters
  as 0 (`purple` → near-black `#00000E`; the result is unpredictable), any other length white (section
  `properties`).
- **`set_slot` keys depend on the parent panel**, not on the widget. A widget
  inside a `SizeBox`, `Border`, `Button` or `ScaleBox` has a slot `set_slot`
  does not support. `padding` sides left out become 0.
- **Canvas `position`/`size` mean margins once anchors stretch** (min ≠ max on
  an axis). Section `layout` before you place anything.
- **`bind_property` checks neither the property name nor the function.** Read
  it back with `list_bindings`, then compile (section `events`).
- **`bind_event` needs the widget to be a variable of the class.** Compile
  first; if it still fails, Is Variable is off and no tool sets it.
- **Each `add_animation_track` call adds a new track**, even for a widget and
  track type that already have one. Send all keys of a track in one call.
- **`batch` failure hides which op failed** (see below).

## create

Creates the asset `<package_path>/<name>`, for example `name: "WBP_HUD"` and
`package_path: "/Game/UI"`. The result's `path` is the object path for every
later call.

- `parent_class` (default `UserWidget`): a class in `/Script/UMG` by its short
  name, or the path of a `UserWidget` subclass: `/Script/MyGame.MyHUDWidget`
  for C++, `/Game/UI/WBP_Base.WBP_Base_C` for a Widget Blueprint. Error:
  `Could not find parent class: <p>`.
- `root_widget_class` (default `CanvasPanel`): the root widget, named
  `RootPanel`. `None` creates no root.
- The tool does not check whether the asset exists. Check with
  `blueprint_query list` first.
- `Unknown root widget class: <c>` comes **after** the asset was created. The
  asset is there, without a root. `add_widget` then sets the first widget as
  the root.
- Other errors: `Failed to create Widget Blueprint: <pkg>/<name>`,
  `Created asset is not a Widget Blueprint`.

## The widget tree

**`add_widget`.** `widget_class` is one of `CanvasPanel`, `Overlay`,
`VerticalBox`, `HorizontalBox`, `SizeBox`, `Border`, `ProgressBar`,
`TextBlock`, `Image`, `Spacer`, `Button`, `ScaleBox`, `GridPanel`, `WrapBox`,
`CheckBox`, `Slider`, `EditableText`, `EditableTextBox`, `ComboBoxString`,
`ScrollBox`, `RichTextBlock`, `CircularThrobber`, `Throbber`,
`BackgroundBlur`, `InvalidationBox`, `RetainerBox`, `WidgetSwitcher`,
`NamedSlot` or `ListView`. Any other name is looked up as a class in
`/Script/UMG`. Other Widget Blueprints cannot be added as children.

- `parent_name` empty or `root` means the root widget. With no root at all, the
  new widget becomes the root (`slot_type: "None (root)"`).
- The result has `widget_name`, `widget_class` (as you passed it),
  `parent_name` and `slot_type`. `slot_type` decides which `set_slot` keys
  apply.
- Errors: `Unknown widget class: <c>`, `Widget with name '<n>' already exists`,
  `Parent widget not found: <p>`, `Parent '<p>' is not a panel widget (is <c>)`,
  `Root widget is not a panel — cannot add children`. `AddChild returned null slot`
  means the parent gave the child no slot, most likely a single-child
  container that already has one (unverified).

**`remove_widget`** removes a widget from the tree. Errors:
`Widget not found: <n>`, `Failed to remove widget: <n>`. Run `inspect`
afterwards to see what is left.

`reparent_widget`, `reorder_child` and `clone_widget` move or copy widgets.
The moved or copied widget gets a new slot: set it again with `set_slot`.
Section `tree` has their results and errors.

## batch

`operations` is an array of objects, each with `op` and the parameters of the
single operation. `op` is one of `add_widget`, `remove_widget`,
`set_property`, `set_slot`, `set_brush`, `reparent_widget`, `reorder_child`
and `clone_widget`; `reorder_children` is not accepted here.

```json
{"operation": "batch", "asset_path": "/Game/UI/WBP_HUD.WBP_HUD",
 "operations": [
   {"op": "add_widget", "widget_class": "ProgressBar", "widget_name": "HealthBar"},
   {"op": "set_slot", "widget_name": "HealthBar",
    "slot_properties": {"position": {"x": 40, "y": 40}, "size": {"x": 300, "y": 24}}},
   {"op": "set_property", "widget_name": "HealthBar",
    "properties": {"percent": 1.0, "fill_color": "#C03030"}}
 ]}
```

- The ops run in order. A failed op does not stop the ones after it, and
  nothing is rolled back.
- `batch` does not save, although the tool's description says so. Call `save`
  afterwards.
- The message is `Batch: <n> OK, <m> errors, <t> total`. With every op
  successful, the data has `results` (one per op), `ok_count`, `error_count`
  and `total`. **With one failed op, the call is an error, and the bridge
  passes only that message.** Run `inspect`, then repeat the missing change as
  a single operation to get its error.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the tree back with `inspect` before you retry.

## Background in the plugin

The plugin's `Resources/domains/tooling.md` lists `widget_editor` among its
tools. `unreal_get_ue_context` has no UMG category: its `slate` category
(also matched by the query `widget`) covers Slate in C++, not Widget
Blueprints.

## See also

- `ue-blueprints`: `blueprint_query` to find Widget Blueprints,
  `blueprint_modify` to compile them and to wire bound events.
- `ue-game-framework`: `show_widget` / `remove_widget` in a running PIE
  session, `set_input_mode`, `set_mouse_cursor`, `set_pause`.
- `ue-enhanced-input`: the input actions a menu or HUD reacts to.
- `ue-pie-testing`, `ue-viewport`: play the result and look at it.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
