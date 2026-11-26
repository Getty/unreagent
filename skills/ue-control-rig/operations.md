# control_rig: the single operations

Source: `MCPTool_ControlRig.h` / `.cpp` and `ControlRigEditor.cpp` in the
toolkit; the engine calls behind them are the RigVM controller's
(`RigVMController.h`, UE 5.8.4 source, read, not run).

## Common to all

- `rig_path` and `operation` are required on every call; a missing or empty
  one returns `Missing required parameter: <name>`. `operation` is lowercased,
  no aliases. Unknown:
  `Unknown operation: '<op>'. Valid: inspect, list_structs, …`.
- Load: `Failed to load Control Rig: <path>`,
  `Asset is not a ControlRigBlueprint: <path> (is <class>)`.
- Every operation except `inspect`, the member-variable operations and
  `recompile` also needs the rig's default graph and its controller:
  `No graph model found in Control Rig`, `Invalid blueprint or graph`,
  `Cannot obtain RigVM controller for graph`.
- Numbers and booleans: send JSON numbers and booleans. The toolkit's
  `ExtractOptionalNumber` / `ExtractOptionalBool` also accept numeric strings
  and any string as a boolean (`"yes"`, `"on"` true; a typo false) — see skill
  `ue-blend-space`, section `operations`, for the exact rules, which are the
  same helpers.

## inspect

Returns `graph`, `hierarchy`, and `summary` (both parts' text summaries joined).
Message: `Control Rig inspection: <name>`.

- `graph`: `name`, `path`, `graph_count`, `summary`, `graphs[]` — every model
  of the rig, each with `name`, `node_count`, `link_count`, `nodes[]`,
  `links[]`.
  - Node: `title` (display title, e.g. `Forwards Solve`), `path` (the name the
    write operations take), `pins[]`, `pin_count`, and for unit and template
    nodes with a resolved struct `struct_name` (e.g. `RigUnit_BeginExecution`)
    and `struct_path`.
  - Pin: `name`, `direction`, `cpp_type`, and `default_value` when it is
    neither empty nor `()`. `direction` is `IN ` (trailing blank), `OUT`,
    `I/O`, `HID` or `???`. Execute pins are `I/O` on mutable units and `OUT` on
    events.
  - Link: `source`, `target`, both full pin paths.
  - **Top-level pins only**; sub-pins are not listed. No node positions.
- `hierarchy`: `elements[]` (`type`, `name`), `total_elements`, `counts` (per
  type), `summary`. Types: `Bone`, `Control`, `Null`, `Curve`, `Connector`,
  `Socket`, `Physics`, `Reference`, `Unknown`. No parents, no transforms, no
  control settings. With no hierarchy object: `hierarchy` is only
  `{"error": "Hierarchy not available"}` and the call still succeeds.

The text `summary` lists each node as `[Node] <title> (<struct>)`, its path,
one line per pin `<dir> <name>: <type> = <default>`, then the links as
`<source> -> <target>` — handy for a quick read, but parse the JSON.

## list_structs and list_templates

- `list_structs`: `structs[]` (`name`, `path`), `count`, `total_available` —
  the engine's registered RigVM unit structs. `filter` is a case-insensitive
  substring on name or path. Message `Found <n> structs`.
- `list_templates`: `templates[]` (notation strings), `count`,
  `total_available`; `filter` on the notation. Message `Found <n> templates`.
- **At most 500 entries**, then `truncated: true`. `total_available` counts
  before the filter. Always pass a `filter` (`"Transform"`, `"TwoBone"`,
  `"Aim"`, `"Add"`).
- Both need a rig that loads, though the lists do not depend on it.

## add_node, add_template, add_var_node

- `add_node`: the unit struct at `struct_path`, the struct's object path
  without the `F` prefix (`/Script/ControlRig.RigUnit_GetTransform`; take it
  from `list_structs`). Method fixed to `Execute`.
- `add_template`: a template node from `notation` exactly as `list_templates`
  returns it. Its pin types resolve when you link or set them.
- `add_var_node`: a getter (`is_getter` true, default) or setter for
  `var_name`.
  - `node_name` is not read; the node is requested as `<var_name>_Get` or
    `<var_name>_Set`.
  - Without `cpp_type` the type comes from the rig's member variable of that
    name; none: `Cannot determine type for '<v>'. Pass cpp_type parameter (e.g. FTransform, float, FVector)`.
  - With `cpp_type`, the tool knows the type object of `bool`, `float`,
    `double`, `int32`, `FName`, `FString`, `FText`, `FVector`, `FRotator`,
    `FTransform`, `FQuat`, `FLinearColor`, `FRigElementKey`, `FVector2D`. Any
    other type goes on without a type object; whether the engine accepts that
    is not visible. For such a type, declare the member variable first and
    leave `cpp_type` out.
- `x`, `y`: graph position; cosmetic. Space nodes apart (e.g. 300 in x) so a
  human can read the graph.

All three return `node` (as in `inspect`) and the message `Added unit node`,
`Added template node`, `Added variable getter` / `Added variable setter`.
**Read the name from `node.path`**: without `node_name` a unit node is named
after its struct (`RigUnit_GetTransform`), a variable node as requested, and a
taken name is made unique by the engine's schema (`GetValidNodeName`, UE
5.8.4 source; the exact suffix is not checked here).

Errors, no reason given (see the output log): `Failed to add node (struct: <path>)`,
`Failed to add template (notation: <n>)`,
`Failed to add variable node for '<v>' (type: <t>)`.

**Adding an event that already exists succeeds and returns the existing
node**: for a unit that can exist only once (Forwards Solve,
`RigUnit_BeginExecution`, and the other solve events) the engine logs
`Rig Graph can only contain one single <name> node.` and hands back the node
already in the graph, so the tool reports `Added unit node`. Compare
`node.path` with `inspect` before you assume a new node (UE 5.8.4 source).

## remove_node

`node_name` is the node's `path`; the node's links go with it. Message
`Removed node '<n>'`; error `Failed to remove '<n>' (not found or protected)`.
Removing an event node disconnects its whole chain from execution.

## link, unlink, unlink_all

- `link` connects `output_pin` → `input_pin` (both full pin paths). The engine
  **breaks an existing link on the input first**, and **on an execute output
  it breaks that output's existing link**. No cast node is inserted for
  mismatched types (the tool passes `bCreateCastNode` false). Message
  `Linked <a> -> <b>`; error `Failed to link <a> -> <b>`.
- `unlink` removes exactly that link. Message `Unlinked <a> -/-> <b>`; error
  `Failed to unlink <a> -> <b>`.
- `unlink_all` removes every link on `pin_path`: input side by default,
  output side with `as_input: false`. Message
  `Broke all input links on <pin>` / `Broke all output links on <pin>`; error
  `Failed to break links on <pin>`.

## set_default

Sets the default of `pin_path` to the string `value` (required, non-empty, so
the single call cannot set an empty string). Message `<pin> = <value>`; error
`Failed to set default on <pin>`.

- The tool calls `SetPinDefaultValue(pin, value, bResizeArrays = false)`: an
  array pin's element count is not changed by the value.
- `value` is the engine's text form of the pin type. Take the form from a pin
  of the same type in `inspect`. Typical forms (engine text export,
  unverified against a running editor): float `0.5`; bool `True` / `False`;
  enum by name `GlobalSpace` / `LocalSpace`; FVector `(X=0,Y=0,Z=10)`;
  FRigElementKey `(Type=Bone,Name="hand_r")`; FTransform
  `(Rotation=(X=0,Y=0,Z=0,W=1),Translation=(X=0,Y=0,Z=0),Scale3D=(X=1,Y=1,Z=1))`.
- A sub-pin path (`Node.Item.Name`, value `hand_r`) sets one member; read it
  back through the parent's `default_value`.
- A pin with an incoming link evaluates the link; its default is ignored
  while linked (RigVM rule, unverified).

## set_expand

Expands or collapses a struct pin in the graph editor display only. Message
`Expanded <pin>` / `Collapsed <pin>`; error `Failed to set expansion on <pin>`.

## add_member_var and remove_member_var

- `add_member_var`: variable `var_name` of `cpp_type`, public by default,
  never read-only, with optional `default_value` (text form as above). Message
  `Added member variable '<name>' (<type>, public)` / `(…, private)` — it
  echoes your name, not the one the engine returned. Error
  `Failed to add member variable '<name>'`.
- `remove_member_var`: message `Removed member variable '<name>'`; error
  `Failed to remove variable '<name>'`. What happens to variable nodes that
  used it: `inspect`.
- Neither opens an undo bracket, and neither needs the graph controller.

## recompile

Calls the rig's `RecompileVM()` and returns `Recompiled RigVM`. It cannot fail
once the rig loaded, and it reports no compile result. No parameters besides
`rig_path`.

## The plugin's own note

`Resources/domains/tooling.md` lists `control_rig` as "Inspect + 16 write
ops"; the code has 16 operations in all: three that read, twelve that write,
and `batch`.
