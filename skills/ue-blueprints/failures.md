# Blueprint tools: when a call fails

Paths, the edit loop, node IDs and how a result reaches you are in the skill
itself. Each operation's own errors are in the section that covers it.

## Errors common to both tools

| Error | Cause |
|---|---|
| `Missing required parameter: <name>` | the parameter is absent or an empty string |
| `Unknown operation: '<op>'. Valid: …` | misspelt operation |
| `Cannot access engine or script Blueprints` | `/Engine/` or `/Script/` path |
| `Could not load Blueprint: <path>` | no Blueprint at that path |
| `Cannot modify engine Blueprints` / `Blueprint package is read-only (cooked)` | `blueprint_modify` only |
| `Blueprint '<n>' has no GeneratedClass — compile it first` (`get_defaults`), `Blueprint has no GeneratedClass — compile it first` (`set_cdo_default`) | the Blueprint was never compiled: run `blueprint_modify compile` |
| `<op> succeeded but compilation failed: …` | the edit was applied, but the Blueprint does not compile |

An optional parameter the tool cannot read as the type it expects is not an
error: the tool uses the default instead. Both tools read their optional
number and boolean parameters through `ExtractOptionalNumber` and `ExtractOptionalBool`
(`MCPToolBase.h`), which call the engine's `FJsonObject::TryGetNumberField`
(into a `double`) and `TryGetBoolField`:

- A number is also read from a string made of an optional sign, digits and at
  most one dot, such as `"5"`, and from `true` and `false` as 1 and 0. Any
  other string falls back to the default.
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
- **A boolean is read from any string and any number, so it never falls back
  to the default.** `true`, `yes`, `on` and `false`, `no`, `off` count in any
  case; every other string is `false` unless it starts with a non-zero
  integer. A typo reads as `false`, also where the default is `true`.
  The editor language's words for True, Yes, False and No count as well
  (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`).

This holds for the parameters at the top level of a call. The fields of a
node spec in `add_nodes` (`pos_x`, `pos_y`, `num_outputs`) and `num_outputs`
inside `node_params` are read with the engine's `FJsonObject::GetNumberField`
instead, which cannot report a failure: a missing field, `null`, an array, an
object or a string that is not numeric reads as 0, and `true` and `false` as
1 and 0. For a position, 0 is the default anyway, and `num_outputs` below 2
becomes 2.

Send numbers and booleans as JSON numbers and booleans. The engine side is
`TJsonValueString` in `Engine/Source/Runtime/Json/Public/Dom/JsonValue.h` and
`FJsonValueNumber` and `FJsonValueBoolean` in `Private/Dom/JsonValue.cpp`,
read in the UE 5.8.4 source and not run, while the toolkit targets UE 5.6 and
5.7; the sections rest on the same files.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| a change is gone after an editor restart | it was never saved | save with `asset save_asset` after each finished change |
| `Node '<id>' not found` for an ID you just used | the node is in another graph | pass the same `graph_name` and `is_function_graph` as when you created it |
| a node's comment was replaced by `MCP_ID:…` | a query assigned it an ID | expected; the comment text is gone |
| `set_pin_value` succeeded, but `get_node` shows the old value | the pin did not accept that string | use the exact `default_value` format `get_node` reports for that pin type |
| after `add_nodes`, some pins are not wired | that connection failed, and `add_nodes` skips failed connections silently | `get_node_connections`, then `connect_pins`, which reports why it fails |
| `add_node` fails with a compile error, and no `node_id` came back | the node was created, but the Blueprint does not compile | `find_nodes` to get its ID, then fix or `delete_node` it |
| `set_cdo_default` succeeded, but actors already in the level are unchanged | placed actors keep the values serialized in the level (the plugin's domain notes) | set the property on the level actor as well |
| `Parent class not found: Character…` for a class you know exists, or `Unknown class: <c>` | a bare class name outside `/Script/Engine` and `/Script/CoreUObject` | pass the full path, `/Script/<Module>.<Class>` (section `modify`) |
| `CallFunction` created a node, but not from the class you named | `target_class` did not resolve and was ignored; a library function of the same name was used | full class path in `target_class`; check the result's `pins` (section `graphs`) |
| `Math function 'Add_FloatFloat' not found` | the `Add`/`Subtract`/`Multiply`/`Divide` node types look up UE 4 names | `CallFunction` with `Add_DoubleDouble` and friends (section `graphs`) |
| `Function '<f>' not found` for a function the editor shows | the editor shows the display title; the tool needs the engine name | `K2_GetActorLocation`, not `GetActorLocation` (section `graphs`) |
| `Source node '<id>' not found` right after wiring a BeginPlay, Tick or overlap event of a new Blueprint | wiring a disabled placeholder event enabled it and cleared its comment, which held the ID | `find_nodes` again, or use the node's object name (section `graphs`, "Pre-placed events") |
| a variable shows `is_instance_editable: true` but cannot be edited per instance | the field reports `CPF_Edit`, set on every Blueprint variable | no tool operation sets the flag; editor or C++ (section `decisions`) |
| a set object pin (an asset on `AddMappingContext`, a class on a spawn) reads empty | object pins keep an object, not text; or the path lacked `.Object` | pass `/Game/X/Asset.Asset`; confirm in play (section `graphs`, `set_pin_value`) |
| a node, a variable, a function you want has no operation (cast, custom event, dispatcher, interface, timeline, function parameter, replication) | outside what the two tools build | section `workflows`, "What to do when a node type is missing" |
| an Enhanced Input event node compiles but never fires in play | no mapping context on the player, or the actor does not receive input | section `workflows`, "Reacting to an Input Action" |
