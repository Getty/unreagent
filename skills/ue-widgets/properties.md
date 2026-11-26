# widget_editor: property, slot and brush keys

`set_property`, `set_slot` and `set_brush` each take a JSON object of keys.
**A key the tool does not know for that widget or slot is ignored without an
error.** So is a value it cannot parse, for the keys marked "skipped if
unknown" below. The success message lists the keys it applied:
`Set 2 properties on HealthBar: percent, fill_color`. Compare that list with
what you sent. When no key applied, `set_property` and `set_slot` fail with
`No recognized properties were set. Check property names and widget type.` and
`No recognized slot properties were set. Check property names and slot type.`

`get_properties` and `inspect` report most current values under the same
key names (`visual_properties`). Some keys you can set are **not reported**:
`justification` and `auto_wrap` of a `TextBlock`, `is_marquee` of a
`ProgressBar`, and `brush_texture` of an `Image` (its texture shows under
`brush.texture`). A `Button` reports only `brushes`, a `Border` its `brush`.

## Colors

`set_property` takes colors as **strings** only:

- a name: `red`, `green`, `blue`, `white`, `black`, `yellow`, `gray` (or
  `grey`), `transparent`;
- hex: `#RRGGBB` or `#RRGGBBAA`, with or without `#`. The digits are read as
  linear values, without sRGB conversion.

**Any other string is read by its length, without an error**
(`WidgetEditor.cpp:152-160`; engine `FColor::FromHex`, `Color.cpp:309-334`,
and `FParse::HexDigit`, `Parse.h:148`, UE 5.8.4):

- **6 characters** (after an optional `#`): `FF` is appended for alpha and
  each character read as a hex digit, a non-hex character as 0. A word of six
  letters such as `purple` comes out near-black (`#00000E`), opaque; a word
  holding `a`–`f` gives an unpredictable colour (`orange` → `#00A00E`).
- **8 characters**: the same, alpha included, so eight non-hex characters
  give transparent black.
- **any other length**: white (`#FFF` too, and any unknown name that is not 6
  or 8 characters long).

In a brush (`set_brush`), `tint` also takes an object: `{"hex": "#RRGGBB"}`, or
`{"r", "g", "b", "a"}` with values from 0 to 1 and 1 for any value left out.
`set_property` takes no color object: a color key given an object is dropped.

`inspect` and `get_properties` report colors as `{r, g, b, a, hex}`: `r` to
`a` are the stored linear values, and **`hex` is converted to sRGB**. So
the `hex` you read back differs from the `hex` you sent, except for black,
white and pure primaries: `#808080` in is stored as linear 0.5 and reported
as about `#BCBCBCFF` (the reported `hex` always has 8 digits, alpha last). Compare the `r`, `g`, `b` numbers, not the hex.

To get an sRGB color you picked (from a design or a color picker) on screen,
convert each channel to linear first (roughly `(c/255)^2.2`; UE's sRGB curve,
unverified as exactly this) and pass a brush `tint` object, or the hex of the
converted bytes. Whether the widget draws the stored value as linear (so the
reported sRGB `hex` is what you see) is the usual UMG behaviour, unverified
for this skill.

## set_property keys

These keys work on every widget:

| Key | Value |
|---|---|
| `visibility` | `Visible`, `Collapsed`, `Hidden`, `HitTestInvisible` or `SelfHitTestInvisible`, as `inspect` reports it (skipped if unknown; meanings in section `design`) |
| `render_opacity` | number |
| `is_enabled` | boolean |

Keys by widget class:

| Class | Keys |
|---|---|
| `ProgressBar` | `percent` (number), `fill_color` (color), `bar_fill_type` (an `EProgressBarFillType` name as `get_properties` reports it, skipped if unknown), `is_marquee` (boolean), `style_fill_tint` (color), `style_background_tint` (color) |
| `TextBlock` | `text` (string), `color` (color), `font_size` (number, cut to an integer), `justification` (`Left`, `Center`, `Right`), `auto_wrap` (boolean) |
| `Image` | `color_and_opacity` (color), `brush_texture` (a texture asset path; skipped if it does not load) |
| `SizeBox` | `width_override`, `height_override`, `min_desired_width`, `min_desired_height`, `max_desired_width`, `max_desired_height` (numbers) |
| `CheckBox` | `is_checked`: `true` or `false`, or `Checked`, `Unchecked`, `Undetermined`; any other string unchecks |
| `Slider` | `value`, `min_value`, `max_value`, `step_size` (numbers) |
| `EditableText`, `EditableTextBox` | `text`, `hint_text` (strings), `is_read_only` (boolean) |
| `ComboBoxString` | `selected_option` (string) |
| `ScrollBox` | `orientation` (`Horizontal`; any other value is vertical), `scroll_bar_visibility` (an `ESlateVisibility` name, skipped if unknown) |
| `RichTextBlock` | `text` (string) |
| `CircularThrobber` | `number_of_pieces` (number, cut to an integer), `period` (number) |
| `Throbber` | `number_of_pieces` (number, cut to an integer) |
| `BackgroundBlur` | `blur_strength` (number) |
| `WidgetSwitcher` | `active_widget_index` (number, cut to an integer) |

An unknown `justification` is still listed as set, but changes nothing.
Widgets of any other class take only the three common keys. For a `Button` or
a `Border`, use `set_brush`.

## set_slot keys

A widget's slot belongs to its parent panel. `inspect` and `get_properties`
report its `slot_type`. Six slot types are supported. Any other one fails with
`Unsupported slot type: <class>`, for example the slot of a widget inside a
`SizeBox`, `Border` or `Button`. The root widget has no slot:
`Widget '<n>' has no slot (is it the root?)`.

| Slot type | Keys |
|---|---|
| `CanvasPanelSlot` | `position` `{x, y}`, `size` `{x, y}`, `anchors` `{min_x, min_y, max_x, max_y}`, `alignment` `{x, y}`, `auto_size` (boolean), `z_order` (number) |
| `OverlaySlot` | `h_align`, `v_align`, `padding` |
| `VerticalBoxSlot`, `HorizontalBoxSlot` | `h_align`, `v_align`, `padding`, `size_rule` (`Auto` or `Fill`), `fill_weight` (number) |
| `ScrollBoxSlot` | `h_align`, `v_align`, `padding` |
| `GridSlot` | `row`, `column`, `row_span`, `column_span` (numbers), `h_align`, `v_align`, `padding` |

- `h_align`: `Left`, `Center` or `Right`. `v_align`: `Top`, `Center` or
  `Bottom`. **Any other value means `Fill`.**
- `padding` is `{left, top, right, bottom}`. A side left out becomes 0, not
  its current value.
- In the canvas objects (`position`, `size`, `anchors`, `alignment`), a field
  left out keeps its current value.
- An unknown `size_rule` is still listed as set, but changes nothing.
- Numbers for `z_order`, `row`, `column` and the spans are cut to integers.

The tool passes these numbers to the slot unchanged. What anchors, alignment,
position and size mean, and ready objects for the common placements, are in
section `layout`.

## set_brush targets

`brush_property` picks the brush on the widget. Without it, the default in
the first row of each class is used.

| Class | `brush_property` |
|---|---|
| `Image` | `brush` |
| `Button` | `style_normal` (default), `style_hovered`, `style_pressed`, `style_disabled` |
| `Border` | `background` |
| `ProgressBar` | `style_fill` (default), `style_background` |

Write the name exactly as listed. Any other value fails, and so does a widget
of another class:
`Button does not have brush property '<p>'. Valid: style_normal, style_hovered, style_pressed, style_disabled`,
`Widget '<n>' (class <c>) does not support set_brush`.

## Brush JSON

The `brush` object changes only the fields it names. The rest of the brush
stays as it is.

| Field | Value |
|---|---|
| `texture` | a texture asset path; `""` clears it |
| `tint` | a color string, or an object as under "Colors" |
| `image_size` | `{x, y}`; a side left out becomes 0 |
| `draw_as` | `NoDrawType`, `Box`, `Border`, `Image`, `RoundedBox` |
| `tiling` | `NoTile`, `Horizontal`, `Vertical`, `Both` |
| `margin` | `{left, top, right, bottom}`; a side left out becomes 0 |

`draw_as` and `tiling` are case-sensitive: `box` is not `Box`.
`set_brush` does not report unparsed values. A texture that does not load, or
a `draw_as` or `tiling` value outside the list, still appears in the
message's list of set fields. It also succeeds when no field applied. Check
the brush afterwards in `get_properties`, under `brush` or `brushes`.
