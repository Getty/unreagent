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
| `Widget '<n>' not found in tree` | the same, from `bind_property` |
| `No recognized properties were set. …` / `No recognized slot properties were set. …` | no key fit the widget class or slot type (section `properties`) |
| `Unsupported slot type: <class>` | the parent is a single-child container or another panel without `set_slot` support (section `layout`) |
| `Widget '<n>' has no slot (is it the root?)` | `set_slot` on the root widget |
| `Widget '<n>' (class <c>) does not support set_brush` | only `Image`, `Button`, `Border`, `ProgressBar` have brushes here |
| `Widget '<n>' is not a variable in the Widget Blueprint. …` | `bind_event` before a compile, or Is Variable is off (section `events`) |
| `Could not find parent class: <p>` | `create` with a `parent_class` that neither `/Script/UMG.<p>` nor the path loads; a Blueprint parent needs `_C` |
| `Unknown root widget class: <c>` | the asset was still created, without a root |
| `Error: Batch: <n> OK, <m> errors, <t> total` | at least one batch op failed; which one does not reach you |
| `Unknown batch op: '<op>'. Valid: …` / `[<i>] missing 'op' field` / `<op>: missing <param>` | a batch entry is malformed; only visible when the whole batch is fine, so repeat the op singly |

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a change is gone after an editor restart | it was never saved | `save` after each finished change |
| a change does not show in PIE | the Widget Blueprint was not compiled after the edit | `blueprint_modify compile`, restart PIE |
| a key you sent is missing from the result message | the tool does not know it for that widget or slot, or could not parse the value | check the key in section `properties` |
| a color came out white | the string was not a known name or a 6- or 8-digit hex, or was an object in `set_property` | use `#RRGGBB` or `#RRGGBBAA` |
| a color came out black or near-black, or invisible | a 6- or 8-character string that is not hex (such as `purple` → `#00000E`): non-hex characters read as 0, and 8 of them give alpha 0 | use a known name or `#RRGGBB` |
| the `hex` read back differs from the one sent | input hex is stored as linear, output hex is sRGB | compare `r`, `g`, `b` (section `properties`) |
| a color looks brighter than the designer's value | an sRGB hex was passed, which the tool reads as linear | convert to linear first (section `properties`) |
| a widget sits in the wrong place or stretches oddly | anchors stretch, so `position`/`size` are margins; or a field kept its old value | send full `anchors`, `alignment`, `position`, `size` (section `layout`) |
| a widget lost its position after `reparent_widget` or `clone_widget` | the new parent gave it a new slot | `set_slot` again |
| an animation plays two tracks on one property | `add_animation_track` was called twice (section `animations`) | `inspect_animation`; the tool cannot remove a track, so remove it in the editor by hand |
| `bind_property` succeeded, but the binding does nothing | the property or function name is wrong; the tool does not check it | `list_bindings`, compile, read the compile messages |
| `VariableGet` of a widget name fails in `blueprint_modify` | widget variables are not visible to that node | C++ parent with `BindWidget`, or editor work (section `events`) |
| a `Tick` or `BeginPlay` event added to a widget graph never fires, or the compile fails (unverified which) | the tool maps those names to Actor events, which a widget does not have | use `Construct`; avoid widget tick (section `events`) |
| the HUD shows in PIE but is missing from `capture_viewport` | unverified whether captures include viewport widgets | check with a `PrintString` in `Construct` and `get_output_log` instead |
| `show_widget` fails with `Failed to load widget class` | the path is the asset, not the class | add `_C`: `/Game/UI/WBP_HUD.WBP_HUD_C` |
| clicks do not reach the menu, or reach the game too | input mode or cursor not set, or a HUD above it is hit-testable | `set_input_mode`, `set_mouse_cursor` (skill `ue-game-framework`); `HitTestInvisible` on the HUD (section `design`) |
