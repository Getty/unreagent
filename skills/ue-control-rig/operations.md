# control_rig: the single operations

`set_default` and `set_expand` are described in the skill itself, and so is
what a success of each operation does not prove.

## inspect

Returns `graph`, `hierarchy` and `summary` (both parts as text).

- `graph`: `name`, `path`, `graph_count`, `summary`, and `graphs[]`, each with
  `name`, `node_count`, `link_count`, `nodes[]` and `links[]`.
  - A node has `title`, `path`, `pins[]`, `pin_count`, and, for a unit node,
    `struct_name` and `struct_path`. `path` is the name the write operations
    take.
  - A pin has `name`, `direction` and `cpp_type`, and `default_value` when it
    is neither empty nor `()`. `direction` is one of `IN `, `OUT`, `I/O`,
    `HID` or `???`; `IN ` has a trailing blank.
  - A link has `source` and `target`, both pin paths.
- `hierarchy`: `elements[]` (`type`, `name`), `total_elements`, `counts` (per
  type) and `summary`. Types: `Bone`, `Control`, `Null`, `Curve`, `Connector`,
  `Socket`, `Physics`, `Reference`, `Unknown`. Parents and transforms are not
  reported.

**Only top-level pins are listed.** Sub-pins, such as the members of a
transform pin, do not appear; the header gives their path form as
`NodeName.PinName.SubPin`. Node positions are not reported either.

When the rig has no hierarchy object, `hierarchy` holds only
`error: "Hierarchy not available"`, and the call is still a success.

## list_structs and list_templates

`list_structs` returns `structs[]` (`name`, `path`), `count` and
`total_available`. `list_templates` returns `templates[]` (notation strings),
`count` and `total_available`. `filter` is a case-insensitive substring: on
the struct's name or path, or on the notation.

**At most 500 entries are returned**, and then `truncated` is `true`.
`total_available` counts everything before the filter, so it does not say how
many matched. Always pass a `filter`.

## add_node, add_template, add_var_node

- `add_node` adds the unit struct at `struct_path`, such as
  `/Script/ControlRig.RigUnit_GetTransform` (the header's example). The method
  name is fixed to `Execute`.
- `add_template` adds a template node from a `notation` string, as
  `list_templates` returns it.
- `add_var_node` adds a getter or setter for `var_name`.
  - `node_name` is not read. The node is requested as `<var_name>_Get` or
    `<var_name>_Set`.
  - Without `cpp_type`, the type comes from the rig's member variable of that
    name; when there is none:
    `Cannot determine type for '<v>'. Pass cpp_type parameter (e.g. FTransform, float, FVector)`.
  - With `cpp_type`, the tool knows the type objects of `bool`, `float`,
    `double`, `int32`, `FName`, `FString`, `FText`, `FVector`, `FRotator`,
    `FTransform`, `FQuat`, `FLinearColor`, `FRigElementKey` and `FVector2D`.
    Any other type is passed on without a type object; whether the engine
    accepts that is not visible in the tool's code. For such a type, declare
    the member variable first and leave `cpp_type` out.

All three return `node` (as in `inspect`) and the message `Added unit node`,
`Added template node`, `Added variable getter` or `Added variable setter`.
Without `node_name` the engine names the node. Take the name from `node.path`
in either case.

Errors: `Failed to add node (struct: <path>)`,
`Failed to add template (notation: <n>)`,
`Failed to add variable node for '<v>' (type: <t>)`. None says why.

## remove_node

`node_name` is the node's `path`. Error:
`Failed to remove '<n>' (not found or protected)`.

## link, unlink, unlink_all

- `link` connects `output_pin` to `input_pin`; both are pin paths. Message:
  `Linked <a> -> <b>`. Error: `Failed to link <a> -> <b>`, with no reason.
- `unlink` removes that one link. Error: `Failed to unlink <a> -> <b>`.
- `unlink_all` removes every link on `pin_path`, on its input side by default
  and on its output side with `"as_input": false`. Message:
  `Broke all input links on <pin>` or `Broke all output links on <pin>`.
  Error: `Failed to break links on <pin>`.

## add_member_var and remove_member_var

`add_member_var` adds a variable `var_name` of `cpp_type` to the rig, public
by default and never read-only. Message:
`Added member variable '<name>' (<type>, public)` or `(…, private)`. Errors:
`Failed to add member variable '<name>'`, `Failed to remove variable '<name>'`.

## recompile

Calls the rig's VM recompile and returns `Recompiled RigVM`. It takes no
parameters besides `rig_path`.

