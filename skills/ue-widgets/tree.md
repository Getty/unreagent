# widget_editor: moving and copying widgets

`add_widget`, `remove_widget` and the parameters of every operation are in
the skill itself.

## reparent_widget, reorder_child, clone_widget

**`reparent_widget`** moves a widget to another panel. The widget gets a new
slot there; its old slot settings are not carried over, so set them again with
`set_slot`. The result has `widget_name`, `old_parent`, `new_parent` and
`new_slot_type`. Errors: `Cannot reparent the root widget`,
`New parent widget not found: <p>`, `New parent '<p>' is not a panel widget (is <c>)`,
`Cannot reparent '<w>' to '<p>' — would create a cycle (new parent is a descendant)`,
`Failed to add widget to new parent` (the widget goes back to its old parent).

**`reorder_child`** moves `child_name` to position `new_index` among the
children of `parent_name`. The index is clamped to the valid range, and the
result's `new_index` is the one used. Errors: `Parent widget not found: <p>`,
`'<p>' is not a panel widget (is <c>)`, `Child widget not found: <c>`,
`'<c>' is not a child of '<p>'`.

**`clone_widget`** deep-copies a widget and its children into `target_parent`,
by default the source's parent. The copy is named `new_name`, and its children
`<new_name>_<child name>`, made unique if needed. The copy and each copied
child get new slots; slot settings are not copied, so set them with
`set_slot`. Property values are copied from the source (bindings and bound
events are not; they are keyed by widget name). Check the copy with
`inspect`. The result has `source_name`,
`clone_name`, `widgets_cloned` and `target_parent`. Errors: `Source widget not found: <n>`,
`Widget with name '<n>' already exists`, `Target parent not found: <p>`,
`Target parent '<p>' is not a panel widget (is <c>)`,
`No valid target parent (source widget has no parent and no target specified)`,
`Failed to clone widget`, `Failed to add cloned widget to target parent`.
In a `batch`, missing parameters show as `clone_widget: missing new_name`
and the like, one form per op (`<op>: missing <param>`).
