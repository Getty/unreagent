# widget_editor: events, property bindings, graph wiring

Both hook Blueprint logic to a widget. The widget is addressed by its name
from `inspect` (main document). Which of the two to use: section `design`.

## Events

1. **`list_events`** lists a widget's delegate events: `widget_name`,
   `widget_class` and `events`, each with `name`, `owner_class` and
   `parameters` (`name`, `type` as a C++ type). The tool's description names
   the common ones: Button `OnClicked`, `OnPressed`, `OnReleased`,
   `OnHovered`, `OnUnhovered`; CheckBox `OnCheckStateChanged`; Slider
   `OnValueChanged`; EditableText `OnTextChanged`, `OnTextCommitted`;
   ComboBoxString `OnSelectionChanged`, `OnOpening`.
2. **`bind_event`** with `event_name` as `list_events` names it creates the
   event node in the Widget Blueprint's first event graph, below the existing
   nodes, and compiles. The result has `widget_name`, `event_name`,
   `node_name`, `node_x`, `node_y` and `function_name`, the name the engine
   generated for the handler. The tool's `function_name` parameter is
   ignored.
3. **Wire the handler** with `blueprint_modify` (skill `ue-blueprints`):
   `node_name` works as `node_id` there, in the default graph. Then compile.

Errors: `Delegate event '<e>' not found on widget class <c>`,
`Event '<e>' is already bound for widget '<n>'`,
`Widget Blueprint has no event graph (UbergraphPages is empty)`,
`Could not get generated class for Widget Blueprint`, and
`Widget '<n>' is not a variable in the Widget Blueprint. Make sure 'Is Variable' is checked.`
The tool looks the widget up as a member variable of the Blueprint's skeleton
class, or of its generated class when there is no skeleton class. A widget
added in this session may not be in that class yet (unverified), so compile
with `blueprint_modify compile` and retry. If the error stays, the widget's
Is Variable flag is most likely off, as the error text says, and no toolkit
tool sets it.

## Property bindings

**`bind_property`** binds a widget property to a function of the Widget
Blueprint, `kind: Function`. The tool's description names the properties in
UMG delegate form: `TextDelegate`, `PercentDelegate`, `VisibilityDelegate`,
`ColorAndOpacityDelegate`; this skill could not check that naming against
the engine. **The tool checks neither the property name nor the function.**
It stores what you pass, so read it back with `list_bindings`, then compile:
the compile is where a wrong name shows (unverified what it reports). A
second binding for the same widget and property replaces the first. The
result has `widget_name`, `property_name`, `function_name`, `kind` and
`updated_existing`. A widget that does not exist returns
`Widget '<n>' not found in tree` (not `Widget not found`).

The function must already exist and return the bound property's type.
`blueprint_modify add_function` creates functions without parameters or
return values, so it cannot create one. A function in a C++ parent class may
work (unverified).

**`unbind_property`** removes the binding:
`No binding found for property '<p>' on widget '<w>'`. It does not check the
widget. **`list_bindings`** returns `asset_path`, `bindings` (`widget_name`,
`property_name`, `function_name`, `source_property`, `kind`) and `count`.
`inspect` shows the same bindings per widget under `bindings`, with the keys
`property`, `function` and `kind`.

## What blueprint_modify can wire in a Widget Blueprint

`blueprint_modify` works on a Widget Blueprint's event graph like on any
Blueprint (skill `ue-blueprints`, section `graphs`). What the toolkit code
does there, read from `BlueprintGraphEditor.cpp`, not run:

- **Lifecycle events.** `add_node` with `node_type: "Event"` and
  `event: "Construct"` (or `PreConstruct`, `Destruct`) is looked up in the
  parent class, `UserWidget`. **`Tick` and `BeginPlay` do not work here**:
  the tool maps them to the Actor events `ReceiveTick` and `ReceiveBeginPlay`,
  which a widget does not have. Avoid widget tick anyway (section `design`).
- **No reference to a child widget.** `VariableGet` looks only at the
  Blueprint's own variables and its parent class. The variables the widget
  tree generates (`HealthBar`) are neither, so `VariableGet` of a widget name
  fails with `Variable '<n>' not found in Blueprint or parent class`, unless a
  C++ parent declares it (`BindWidget`).
- **Functions on self and in libraries work.** `CallFunction` finds a function
  in `target_class`, then in `KismetSystemLibrary`, `KismetMathLibrary`,
  `KismetStringLibrary` and `GameplayStatics`. For a UMG class pass its
  object path, such as `target_class: "/Script/UMG.Widget"` for
  `RemoveFromParent` (a short name like `Widget` is unverified to resolve). A
  member function of the widget's own class targets self when its target
  pin is left unconnected (unverified).
- **No Create Widget node.** It is a special node type, not a function call,
  and `add_node` has no type for it. Creating the widget at runtime is game
  code written in C++ or by hand; in PIE, `game_framework show_widget` does
  it for a test.
- **No custom events, no function parameters or return values.** A function
  that game code calls to push a value into the widget has to come from C++
  or the editor.

What you can build fully: button events whose handlers call library or self
functions (pause, resume, open level, quit, remove the menu), a
`PrintString` for checking, and anything on `Construct` that needs no widget
reference.
