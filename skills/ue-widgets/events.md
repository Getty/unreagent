# widget_editor: events and property bindings

Both hook Blueprint logic to a widget. The widget is addressed by its name
from `inspect` (main document).

## Events

1. **`list_events`** lists a widget's delegate events: `events`, each with
   `name`, `owner_class` and `parameters` (`name`, `type`).
2. **`bind_event`** with `event_name`, such as `OnClicked` on a `Button`,
   creates the event node in the Widget Blueprint's first event graph, below
   the existing nodes, and compiles. The result has `widget_name`,
   `event_name`, `node_name`, `node_x`, `node_y` and `function_name`, the name
   the engine generated for the handler. The tool's `function_name` parameter
   is ignored.
3. **Wire the handler** with `blueprint_modify` (skill `ue-blueprints`):
   `node_name` works as `node_id` there, in the default graph.

Errors: `Delegate event '<e>' not found on widget class <c>`,
`Event '<e>' is already bound for widget '<n>'`,
`Widget Blueprint has no event graph (UbergraphPages is empty)`,
`Could not get generated class for Widget Blueprint`, and
`Widget '<n>' is not a variable in the Widget Blueprint. Make sure 'Is Variable' is checked.`
The tool looks the widget up as a member variable of the Blueprint's skeleton
class, or of its generated class when there is no skeleton class. Whether the
skeleton class already holds a widget added in this session has not been
verified for this skill, so compile with `blueprint_modify compile` and retry.
If the error stays, the widget's Is Variable flag is off, and no toolkit tool
sets it.

## Property bindings

**`bind_property`** binds a widget property to a function of the Widget
Blueprint, `kind: Function`. The tool's description names the properties in
UMG delegate form: `TextDelegate`, `PercentDelegate`, `VisibilityDelegate`,
`ColorAndOpacityDelegate`; this skill could not check that naming against
the engine. **The tool checks neither the property name nor the function.**
It stores what you pass, so read the binding back with `list_bindings`. A
second binding for the same widget and property replaces the
first (`updated_existing: true`).

The function must already exist and return the bound property's type.
`blueprint_modify add_function` creates functions without parameters or
return values, so it cannot create one.

**`unbind_property`** removes the binding:
`No binding found for property '<p>' on widget '<w>'`. **`list_bindings`**
returns `bindings` (`widget_name`, `property_name`, `function_name`,
`source_property`, `kind`) and `count`.
