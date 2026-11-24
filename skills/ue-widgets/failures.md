# widget_editor: when a call fails

Paths, the edit loop, `create`, `batch` and how a result reaches you are in
the skill itself.

## Errors

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | a required parameter is absent or empty |
| `Failed to load Widget Blueprint: <path>` | no asset at that path |
| `Asset is not a Widget Blueprint: <path> (is <class>)` | the asset is another kind |
| `Widget not found: <n>` | no widget of that name; names come from `inspect` |
| `No recognized properties were set. …` / `No recognized slot properties were set. …` | no key fit the widget class or slot type (section `properties`) |
| `Unsupported slot type: <class>` | the parent panel's slot has no `set_slot` support |
| `Widget '<n>' is not a variable in the Widget Blueprint. …` | `bind_event` before a compile, or Is Variable is off (section `events`) |
| `Error: Batch: <n> OK, <m> errors, <t> total` | at least one batch op failed |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a change is gone after an editor restart | it was never saved | `save` after each finished change |
| a key you sent is missing from the result message | the tool does not know it for that widget or slot | check the key in section `properties` |
| a color came out white | the color string was not a known name or a 6- or 8-digit hex | use `#RRGGBB` or `#RRGGBBAA` |
| a widget lost its position after `reparent_widget` | the new parent gave it a new slot | `set_slot` again |
| an animation plays two tracks on one property | `add_animation_track` was called twice (section `animations`) | `inspect_animation`; the tool cannot remove a track, so it has to be removed in the editor by hand |
| `bind_property` succeeded, but the binding does nothing | the property or function name is wrong; the tool does not check it | `list_bindings`, compile, and check the names |
