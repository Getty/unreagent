# widget_editor: design decisions

Rules for choosing how a widget is built and how it gets its data. UMG facts
carry their source; the rest is labelled unverified.

## How a widget gets its data

| Way | Cost | With these tools |
|---|---|---|
| **Event-driven update**: game code sets the widget's value when it changes (`SetPercent`, `SetText`) | only on change | layout yes; the update code in C++ or hand-written graph (section `events`) |
| **Property binding**: a function the widget calls to fetch the value | polled every frame while shown | `bind_property` stores it, but the function must exist and return the right type, which `blueprint_modify` cannot create |
| **UMG Viewmodel (MVVM)**: FieldNotify fields on a viewmodel, view bindings | only on change | not covered by any toolkit tool |

Epic's UMG optimization guide: bound attributes "poll the attribute every
frame", and it recommends event-driven updates instead ("Never use raw
attribute binding"); it also says to avoid running logic in On Tick or On
Paint. Default to event-driven updates. A property binding is acceptable for
a few values on a small widget, or for a prototype.
(https://dev.epicgames.com/documentation/en-us/unreal-engine/optimization-guidelines-for-umg-in-unreal-engine)

UMG Viewmodel is a plugin (enable **UMG Viewmodel** in the Plugins menu) and
Epic marks it **Beta**; widgets update only when a FieldNotify value changes.
(https://dev.epicgames.com/documentation/en-us/unreal-engine/umg-viewmodel-for-unreal-engine)
Use it only when the project already does; the toolkit cannot set up its
bindings, so that part is editor work.

## C++ parent, Blueprint only, or Python

- **C++ parent class** (`UUserWidget` subclass, widgets as
  `UPROPERTY(meta = (BindWidget))`, update functions in C++): the route when
  logic must reach the widgets and the project builds C++. `widget_editor`
  builds the tree against it (`create` with `parent_class`), the names and
  classes must match the properties. `blueprint_modify`'s `VariableGet` also
  finds such properties, because it looks in the parent class.
- **Blueprint only**: fine for layout, styling, animations and button events
  whose handlers call library functions (`SetGamePaused`, `OpenLevel`,
  `QuitGame`, `RemoveFromParent`). Logic that needs a reference to a child
  widget, a Create Widget node or a function with a return value cannot be
  written through the toolkit (section `events`).
- **Python (`execute_script`, skill `ue-python`)**: whether editor scripting
  reaches a Widget Blueprint's widget tree or graph has not been checked for
  this skill (unverified). Prefer `widget_editor` for the tree.
- **Slate in C++**: for editor tools and engine-level UI, not for game HUDs.
  `get_ue_context` category `slate` covers it.

## Which panel

- **Canvas Panel** places children absolutely, with anchors (section
  `layout`). It is the default root here. Epic: Canvas Panels have "high
  performance demands" and use multiple draw calls, while Vertical or
  Horizontal Boxes consolidate their children's layer IDs (optimization
  guide above). Use one Canvas at the root of a full-screen HUD to anchor
  groups to screen edges; inside each group use boxes.
- **Overlay** stacks children on top of each other, aligned in the parent:
  a background image under text, an icon over a bar.
- **Vertical/Horizontal Box** for lists, menus and rows; `size_rule` and
  `fill_weight` share space (section `layout`).
- **Grid Panel** for inventory-like grids (`row`, `column` in `set_slot`).
- **Size Box** to force a size on one child, **Scale Box** to scale it. Their
  child's slot is not supported by `set_slot`: set the size on the Size Box
  itself (`width_override`, `height_override`).
- **Widget Switcher** for tabs or pages: one child visible,
  `active_widget_index`.
- **Invalidation Box** caches its children's layout and paint ("cache the
  information of their child widgets"); **Retainer Box** renders its
  children into a texture (optimization guide above). Wrap static parts of a
  busy HUD; neither has keys in `set_property`.

## Visibility

`visibility` takes `Visible`, `Collapsed`, `Hidden`, `HitTestInvisible` or
`SelfHitTestInvisible` (the `ESlateVisibility` values). `Collapsed` takes no
space in the layout, `Hidden` keeps its space; the two `HitTestInvisible`
values draw the widget but let clicks pass through, `Self…` only for the
widget itself, not its children (unverified against Epic's docs). A HUD
that must not block clicks on a menu below it uses `HitTestInvisible` or
`SelfHitTestInvisible` on its root (unverified).

## Common UI

Common UI is a plugin for multiplatform menus: it routes input to the
topmost *activatable* widget, and each activatable widget declares its input
config (Menu, Game or All), replacing manual Set Input Mode calls.
(https://dev.epicgames.com/documentation/en-us/unreal-engine/commonui-input-technical-guide-for-unreal-engine)
Its widget classes are not in `add_widget`'s list and are looked up only in
`/Script/UMG`, so `widget_editor` cannot add Common UI widgets; a Widget
Blueprint whose parent is a Common UI class can be created with
`parent_class` set to that class's path (unverified for Common UI classes).
If the project uses Common UI, do not add `set_input_mode` calls to its
menus.

## Naming

`WBP_` for Widget Blueprints is the common convention (unverified as an Epic
rule). Give widgets that code or bindings use a descriptive name
(`HealthBar`, `ResumeButton`): it is the variable name, and with a C++ parent
it must equal the `BindWidget` property.
