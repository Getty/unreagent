# metasound: the write operations in full

`create`, `add_node`, `remove_node`, `connect`, `disconnect`,
`set_input_default`, `add_graph_input`, `add_graph_output`, `preview` and
`stop_preview`. Toolkit facts are from its source (commit c1b6709,
`MCPTool_MetaSound.cpp`, `MetaSoundEditor.cpp`). Engine facts are marked
**UE 5.8.4 source**: read, not run; the UE 5.7 source was not checked.

Every write loads the asset, edits its document through the engine's
`FMetaSoundFrontendDocumentBuilder`, and marks the package dirty. None saves.

## Engine assertions: the calls that can crash the editor

The engine's document builder guards some calls with `checkf`. In an editor
built with checks, which a Development editor is (`DO_CHECK` 1,
`Misc/Build.h`), a failed `checkf` is a fatal error: the editor crashes and
every unsaved change is lost. **Save before these calls** (`asset save_all`).

| Call | Assertion in UE 5.8.4 source | Avoid it by |
|---|---|---|
| `connect` | `AddEdge`: the input is already connected; or the edge is invalid — the output's type cannot be cast to the input's, or the access types (value vs. reference, such as a constructor pin) do not match | `get_graph` first: no edge in `edges[]` ends at the input's `vertex_id` (else `disconnect` it), and both pins have the same `type` |
| `add_graph_input` | `AddGraphInput`: `checkf(ClassInput.NodeID.IsValid(), "Unassigned NodeID when adding graph input")` — the toolkit sets the new input's `VertexID` but never its `NodeID` | nothing the caller can pass: the check is the function's first statement and the toolkit's `NodeID` is always unset, so on UE 5.8.x **every call crashes**. Do not call it; use the user or the Builder API path (section `workflows`) |
| `add_graph_output` | the same check in `AddGraphOutput` | as `add_graph_input` |

Whether UE 5.7 has the same checks was not verified. If the editor dies
after one of these calls, the crash most likely names the failed assertion
in the editor's log; tell the user what you called, and do not repeat it.

## create

Creates a MetaSound Source at `<package_path>/<name>`, such as
`/Game/Audio/MS_Tone`, and initializes its document with the class's default
interfaces (section `nodes`, "What a new MetaSound Source contains").

- **`output_format` is read and ignored** (default `Mono`). Read the
  interfaces, inputs and outputs back with `inspect` and `get_graph`.
- **The tool does not check whether an asset already exists** at that path,
  and what the engine does then has not been verified. Check with
  `asset_search` first: `path_filter` the folder, `name_pattern` the name;
  the name is taken only if an entry has exactly that `name` and that
  `package_path` (skill `ue-assets`).
- Errors: `Failed to create package: <path>`,
  `Failed to create UMetaSoundSource object`.
- Result: `asset_path` (the object path). Message: `Created MetaSound: <path>`.

## add_node

| Parameter | Meaning |
|---|---|
| `node_class_name` | the class name; with a variant, `<name>.<variant>` (section `nodes`) |
| `node_namespace` | the class's `namespace` from `list_nodes`. Without it the tool looks in an empty namespace |

Always major version 1, no separate variant. Failure:
`Failed to add node: <namespace>::<class_name>`; the editor log then has the
engine's reason under `LogMetaSound`. Result: `node_id`, the new node's GUID.
Message: `Added node: <namespace>::<class_name>`. Read the new node's pins
with `get_graph`. The node gets no position in the editor's graph view.

## remove_node

Removes the edges of `node_id`, then the node. Message:
`Removed node: <id>`. A node that is not in the graph fails with
`Failed to remove node: <id>`, possibly after its edges were already removed.

**Do not use it on a graph input's or output's node.** In the UE 5.8.4
source, `RemoveNode` removes the node only: the member stays in the
MetaSound's interface, so `inspect` still lists it, now without a node. What
the engine then does with the asset has not been verified.

## connect and disconnect

Both take the output side (`from_node_id`, `from_pin`) and the input side
(`to_node_id`, `to_pin`). The pins are looked up by name first:
`Output pin not found: <pin> on node <id>`,
`Input pin not found: <pin> on node <id>`. Bad GUIDs:
`Invalid from-node GUID: <id>`, `Invalid to-node GUID: <id>`.

- **`connect` adds the edge without any check of its own**, and the engine's
  only checks are the assertions above. Run the checks in the table before
  every `connect`, then confirm the edge with `get_graph`.
- The `connect` result has `from_node`, `from_pin`, `to_node` and `to_pin` as
  you passed them. Message: `Connected nodes`.
- `disconnect` removes the edge into that input if it comes from that
  output; otherwise `Failed to remove edge - edge may not exist`. Message:
  `Disconnected nodes`.

## set_input_default

Sets the default of one input pin on one node.

| Parameter | Meaning |
|---|---|
| `node_id` | the node |
| `input_name` | the input pin's `name` |
| `value` | the value **as a string**, such as `"440"`. An empty string counts as missing |
| `data_type` | how to convert `value`, lowercased: `float` (default), `int32`, `bool`, anything else stores the text as a string |

- **`data_type` is not checked against the pin**, and in the UE 5.8.4 source
  `SetNodeInputDefault` does not check the literal's type either: it stores
  it. `float` reads a number (`FCString::Atof`, anything else 0), `int32` an
  integer, `bool` is true only for `true` (any case) or `1`. **`int` is not
  `int32`**: it stores a string. Pick the conversion that matches the pin's
  `type` in `get_graph`; a mismatch is stored silently and its effect is not
  verified.
- A JSON number or boolean in `value` is turned into its text by the
  engine's `TryGetStringField` (`440` → `"440"`, `true` → `"true"`), then
  converted as above; `null`, an array or an object counts as missing.
- Errors: `Invalid node GUID: <id>`, `Input not found: <name> on node <id>`,
  `Failed to set default on input '<name>'` (the node or vertex was not found
  by the engine).
- Message: `Set default on input '<name>' to '<value>'`. `get_graph` does not
  show defaults, so no tool here reads it back.

## add_graph_input and add_graph_output

**Read the assertion table above first.** On UE 5.8.x both calls always
crash the editor: do not call them. On 5.7 the check is unverified; call
them there only with the user's consent, after `asset save_all`.

What the toolkit does: builds a member with `input_name` / `output_name`
and `data_type` as the engine type name, a new `VertexID`, and for an input
an optional `default_value` converted like `set_input_default`'s `value`.
The default is written to the member's `DefaultLiteral`, a field the UE 5.8.4
source marks deprecated since 5.5 ("rolled into DefaultLiterals Array");
whether the engine still reads it is not verified.

What the engine does in UE 5.8.4, past the assertion: a name already taken
returns the existing member's node (the call then reports success for the
old member) and logs an error; a `data_type` that is not a registered type
fails, logged as `Cannot add MetaSound graph input '<name>' with unregistered
TypeName '<type>'`.

Results: `input_node_id` or `output_node_id` (not `node_id`). Messages:
`Added graph input: <name> (<type>)`, `Added graph output: <name> (<type>)`.
Errors: `Failed to add graph input: <name>`,
`Failed to add graph output: <name>`.

## preview and stop_preview

`preview` loads the MetaSound, stops and destroys a preview already playing,
then creates a transient audio component in the editor world, without
spatialization, flagged as a preview sound, and calls Play. One preview at a
time.

- **`status: "playing"` is set whatever happens.** The tool does not check
  that sound started; a graph that does not build may play silence.
- Whether the preview includes edits made through this tool since the asset
  was loaded has not been verified.
- A Source with the `UE.Source.OneShot` interface whose `OnFinished` is never
  triggered plays until stopped (section `nodes`).
- Errors: `Failed to load MetaSound: <path>`, `Editor not available`.
- Result: `asset_path` as passed, `status`. Message: `Playing MetaSound: <name>`.

`stop_preview` takes no parameters: `Preview stopped`, or
`No preview was playing`; both are successes. Always call it after
`preview`.
