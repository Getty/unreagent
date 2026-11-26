# widget_editor: layout and placement

How `set_slot` places a widget, with slot objects for common placements. The
keys themselves are in section `properties`.

## Canvas Panel slots

A child of a Canvas Panel has a `CanvasPanelSlot` with `anchors`,
`alignment`, `position`, `size`, `auto_size` and `z_order`.

- **Anchors** (`min_x`, `min_y`, `max_x`, `max_y`, fractions of the parent,
  0 to 1) fix the widget to a point or a region of the canvas, so it follows
  screen size and aspect changes. (Epic, UMG anchors:
  https://dev.epicgames.com/documentation/en-us/unreal-engine/umg-anchors-in-unreal-engine-ui)
- **Alignment** (`x`, `y`, 0 to 1) is the widget's own pivot: `{0, 0}` its
  upper-left corner, `{1, 1}` its lower-right. (Epic, `FAnchorData`:
  https://dev.epicgames.com/documentation/unreal-engine/API/Runtime/UMG/FAnchorData)
- **Position and size are the slot's offsets.** With a point anchor (min =
  max on an axis), `position` is the distance from the anchor point to the
  pivot and `size` the widget's size. With a stretched anchor (min ≠ max), the
  same numbers become margins: `position` the left/top inset, `size` the
  right/bottom inset. Epic's API says the offsets are "position and size, or
  margins depending on the anchor points"
  (https://dev.epicgames.com/documentation/en-us/unreal-engine/API/Runtime/UMG/UCanvasPanelSlot);
  the mapping of `position`/`size` onto left/top and right/bottom is the
  engine's `SetPosition`/`SetSize` (unverified against the source).
- **`auto_size: true`** sizes the widget to its content and ignores `size`
  (unverified).
- **`z_order`**: higher draws on top among siblings (unverified).

The tool keeps any field you leave out of `position`, `size`, `anchors` or
`alignment`. Send the whole object when the placement matters.

### Ready placements

Each is a `slot_properties` object for `set_slot`. Adjust the numbers.

| Placement | `slot_properties` |
|---|---|
| top-left, 40 px in | `{"anchors": {"min_x": 0, "min_y": 0, "max_x": 0, "max_y": 0}, "alignment": {"x": 0, "y": 0}, "position": {"x": 40, "y": 40}, "size": {"x": 300, "y": 24}}` |
| top-right | `{"anchors": {"min_x": 1, "min_y": 0, "max_x": 1, "max_y": 0}, "alignment": {"x": 1, "y": 0}, "position": {"x": -40, "y": 40}, "size": {"x": 200, "y": 40}}` |
| bottom-center | `{"anchors": {"min_x": 0.5, "min_y": 1, "max_x": 0.5, "max_y": 1}, "alignment": {"x": 0.5, "y": 1}, "position": {"x": 0, "y": -40}, "size": {"x": 600, "y": 80}}` |
| screen center | `{"anchors": {"min_x": 0.5, "min_y": 0.5, "max_x": 0.5, "max_y": 0.5}, "alignment": {"x": 0.5, "y": 0.5}, "position": {"x": 0, "y": 0}, "size": {"x": 400, "y": 300}}` |
| fill the screen | `{"anchors": {"min_x": 0, "min_y": 0, "max_x": 1, "max_y": 1}, "alignment": {"x": 0, "y": 0}, "position": {"x": 0, "y": 0}, "size": {"x": 0, "y": 0}}` |
| full-width bar at the top, 60 px high | `{"anchors": {"min_x": 0, "min_y": 0, "max_x": 1, "max_y": 0}, "position": {"x": 0, "y": 0}, "size": {"x": 0, "y": 60}}` |

For the right and bottom anchors the position is negative: the offset runs
from the anchor point, and the alignment puts the widget's right or bottom
edge there. The last row stretches on X only, so its X numbers are margins
(0, 0) and its Y numbers are position and height.

Read the result back with `get_properties`: `slot_properties` shows
`anchors`, `alignment`, `position`, `size`, `auto_size` and `z_order` as the
slot holds them.

## Box slots

Children of a Vertical Box or Horizontal Box have `VerticalBoxSlot` or
`HorizontalBoxSlot`:

- `size_rule: "Auto"` takes the child's desired size; `"Fill"` shares the
  remaining space by `fill_weight` (two children with weight 1 and 2 split it
  one to two). Fill is the usual choice for a stretching spacer or list.
- `h_align` / `v_align` place the child inside its share; `Fill` stretches
  it. Any value other than `Left`, `Center`, `Right` (or `Top`, `Center`,
  `Bottom`) means `Fill`.
- `padding` `{left, top, right, bottom}`: space around the child. A side left
  out becomes 0. Spacing in a menu column is a `bottom` padding on each
  button, or a `Spacer` widget.

Children of an Overlay (`OverlaySlot`) and a Scroll Box (`ScrollBoxSlot`)
take `h_align`, `v_align` and `padding`; children of a Grid Panel
(`GridSlot`) add `row`, `column`, `row_span` and `column_span`.

## Slots set_slot does not support

Single-child containers give their child their own slot type: `SizeBoxSlot`,
`BorderSlot`, `ButtonSlot`, `ScaleBoxSlot` and others. `inspect` shows the
`slot_type` with empty `slot_properties`, and `set_slot` returns
`Unsupported slot type: <class>`. Set padding and alignment on the container
where it has keys (`SizeBox` overrides), or wrap the child in an `Overlay`
inside the container and set the `OverlaySlot`. The same applies to
`WrapBox`, `WidgetSwitcher` and `UniformGridPanel` children (their slot
classes are not in the tool's list).

## Order and moves

Children draw in order; `reorder_child` changes it, `z_order` overrides it on
a canvas. `reparent_widget` and `clone_widget` give the widget a new slot with
default settings: set it again.
