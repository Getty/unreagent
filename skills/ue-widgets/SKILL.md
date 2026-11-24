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
Blueprints: the widget tree, each widget's properties and layout slot, brushes,
delegate events, property bindings and widget animations. Through the plugin's
MCP bridge it is listed as `unreal_widget_editor`. It works only while the
editor is running and the plugin is reachable; `unreagent-iterate` covers the
wait for the plugin.

The tool takes an `operation` string and lowercases it before dispatch.
`reorder_children` is accepted for `reorder_child`. An unknown operation
returns `Unknown operation: '<op>'. Valid: <list>`.

This skill has five sections:

| Section | Covers | Load it |
|---|---|---|
| `properties` | the keys of `set_property`, `set_slot` and `set_brush`; color formats | before your first `set_property`, `set_slot` or `set_brush`: the keys are only there |
| `events` | `list_events`, `bind_event`, `bind_property`, `unbind_property`, `list_bindings` | before you bind an event or a property |
| `animations` | `list_animations`, `inspect_animation`, `create_animation`, `add_animation_track` | before you add an animation track: the track types and the keyframe form are only there |
| `tree` | `reparent_widget`, `reorder_child` and `clone_widget` in full: result fields and errors | when one of the three returns an error, or you need its result fields |
| `failures` | the error table, and the failure table (what you see, what it means, what to do) | when a call returns an error you cannot place, or what you see after a call is not what you expect |

## Paths

Every operation except `create` needs `asset_path`. There is no
`blueprint_path` alias. The path goes to the engine's loader unchanged,
without the path checks and the `/Game/` fallback of the Blueprint tools
(skill `ue-blueprints`). `create` and `save` report the object path, such as
`/Game/UI/WBP_HUD.WBP_HUD`. Use that form.

- `Failed to load Widget Blueprint: <path>`: nothing loads at that path.
- `Asset is not a Widget Blueprint: <path> (is <class>)`: the asset is
  something else, such as an ordinary Blueprint.

`blueprint_query list` with `type_filter: "UserWidget"` finds Widget
Blueprints.

## Operations

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

The keys that go into `properties`, `slot_properties` and `brush` are only
in section `properties`, and the track types and the form of `keyframes`
only in section `animations`: load the section before your first such call.
Load section `events` before you bind an event or a property.

## Reading

- **`inspect`** returns `asset_path`, `blueprint_name`, `total_widgets` and
  `widget_tree`: the root widget, or the string `empty`. Each widget has
  `name`, `class`, `slot_type` and `slot_properties` (not on the root),
  `visual_properties`, `bindings` when it has any, and `children` for a panel.
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
   with the keys you sent: unknown keys are dropped without an error
   (section `properties`).
4. **Compile.** Only `bind_event` compiles, and it reports no compile result.
   `blueprint_modify compile` with the same `asset_path` compiles the Widget
   Blueprint and reports errors and warnings (skill `ue-blueprints`).
5. **Save.** No operation saves except `save`, not even `batch`. Until then
   every change lives only in the running editor. `save` writes the asset
   (`Saved: <path>`, or `Failed to save: <path>`). The `asset` tool's
   `save_asset` and `save_all` work as well.

`widget_editor` has no undo. Remove a widget with `remove_widget`, a binding
with `unbind_property`, and a bound event's node with
`blueprint_modify delete_node`. There is no operation to remove an
animation or a track.

What the sections hold that shows no error:

- **A color string that is not a known name or a 6- or 8-digit hex becomes
  white.** Hex digits are read as linear values, without sRGB conversion
  (section `properties`).
- **`bind_property` checks neither the property name nor the function.** It
  stores what you pass, so read the binding back with `list_bindings`
  (section `events`).
- **Each `add_animation_track` call adds a new track**, even for a widget
  and track type that already have one. Send all keys of a track in one call
  (section `animations`).

## create

Creates the asset `<package_path>/<name>`, for example `name: "WBP_HUD"` and
`package_path: "/Game/UI"`. The result's `path` is the object path for every
later call.

- `parent_class` (default `UserWidget`): a class in `/Script/UMG`, given by its
  short name, or the full path of a `UserWidget` subclass. Error:
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
- The result has `widget_name`, `widget_class`, `parent_name` and `slot_type`.
  `slot_type` decides which `set_slot` keys apply.
- Errors: `Unknown widget class: <c>`, `Widget with name '<n>' already exists`,
  `Parent widget not found: <p>`, `Parent '<p>' is not a panel widget (is <c>)`,
  `Root widget is not a panel — cannot add children`. `AddChild returned null slot`
  means the engine gave the parent no slot for the child, most likely a
  container that holds one child and already has it (not verified).

**`remove_widget`** removes a widget from the tree. Errors:
`Widget not found: <n>`, `Failed to remove widget: <n>`. Run `inspect`
afterwards to see what is left.

**`reparent_widget`**, **`reorder_child`** and **`clone_widget`** move or
copy a widget; their parameters are in the table under "Operations".

- `reparent_widget` moves a widget to another panel. The widget gets a new
  slot there; its old slot settings are not carried over, so set them again
  with `set_slot`.
- `reorder_child` clamps `new_index` to the valid range; the result's
  `new_index` is the one used.
- `clone_widget` deep-copies a widget and its children into `target_parent`,
  by default the source's parent. The copy is named `new_name`, and its
  children `<new_name>_<child name>`, made unique if needed. The copy and
  each copied child get new slots; slot settings are not copied, so set them
  with `set_slot`. Check the copy with `inspect`.

Load section `tree` when one of the three returns an error, or you need its
result fields.

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
  successful, the data has `results` (one result per op), `ok_count`,
  `error_count` and `total`. **With one failed op, the call is an error, and
  the bridge passes only that message**: which op failed, and why, does not
  reach you. Run `inspect`, then repeat the missing change as a single
  operation to get its error.

## Results through the bridge

- **Success** is the tool's message, a blank line, then JSON. By default the
  bridge goes through the task queue, so that JSON is a task envelope
  (`task_id`, `status`, `success`, `message`, …), and the tool's own fields,
  the ones named in this skill, sit under its `data` key. Skill `ue-tasks`
  covers the task tools.
- **Failure** is only `Error: <message>`, with no data.
- **Timeouts.** After 300 s the bridge returns
  `Error: Task timed out after 300000ms (task_id: …)`. That ends the wait, not
  the work. Read the tree back with `inspect` before you retry (skill
  `ue-tasks`).

The messages of `set_property`, `set_slot` and `set_brush` list the keys they
applied.

## Errors and failure modes

Load section `failures` when a call returns an error you cannot place, or
what you see after a call is not what you expect: it has the error table and
the failure table (what you see, what it means, what to do).

## Background in the plugin

The plugin's `Resources/domains/tooling.md` lists `widget_editor` among its
tools. `unreal_get_ue_context` has no Widget Blueprint category: its `slate`
category covers Slate UI in C++, not UMG.

## See also

- `ue-blueprints`: `blueprint_query` to find Widget Blueprints,
  `blueprint_modify` to compile them and to wire bound events.
- `ue-enhanced-input`: input actions and mapping contexts for the controls a
  widget reacts to.
- `ue-game-framework`: `game_framework` `show_widget` puts a Widget Blueprint
  on screen in a running PIE session, `remove_widget` takes it off.
- `unreagent-iterate`: starting the editor and waiting for the plugin.
