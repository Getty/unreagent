# blueprint_modify: structure, components, defaults, compiling

This section covers every `blueprint_modify` operation that does not edit graph
nodes. The node and wiring operations are in section `graphs`.

Operation aliases: `add_var` and `remove_var` run `add_variable` and
`remove_variable`, `add_func` and `remove_func` run `add_function` and
`remove_function`, `cdo` runs `set_cdo_default`, `component_default` runs
`set_component_default`, `wire` runs `connect_pins`, and `layout` runs
`layout_graph`.

| Operation | Required besides `blueprint_path` | Optional |
|---|---|---|
| `create` | `package_path`, `blueprint_name`, `parent_class` (no `blueprint_path`) | `blueprint_type` |
| `reparent` | `parent_class` | — |
| `add_variable` | `variable_name`, `variable_type` | — |
| `remove_variable` | `variable_name` | — |
| `add_function` | `function_name` | — |
| `remove_function` | `function_name` | — |
| `add_component` | `component_class`, `component_name` | `attach_to`, `socket_name` |
| `remove_component` | `component_name` | `promote_children` |
| `set_component_default` | `component_name`, `property`, `value` | — |
| `set_cdo_default` | `property`, `value` | — |
| `add_debug_print` | `label`, and `variables` or `functions` | `event`, `print_to_screen`, `print_to_log` |
| `remove_debug_print` | `label` | — |
| `compile` | — | — |
| `compile_all` | — (no `blueprint_path`) | `path` |

Every operation in this table except `create`, `compile` and `compile_all`
compiles the Blueprint after the change (main document, "The edit loop").
Two calls that change nothing return before the compile: `reparent` to the
parent the Blueprint already has, and `remove_debug_print` with nothing to
remove (`nodes_removed: 0`).

## create

Creates a Blueprint asset at `package_path` + `/` + `blueprint_name`.

- `package_path` is a folder such as `/Game/Blueprints`. It passes the same
  path check as `blueprint_path`.
- `blueprint_name` may contain only letters, digits and underscores, must start
  with a letter or underscore, and is at most 128 characters. A bad name returns
  `Invalid Blueprint name: <reason>`.
- `parent_class` is tried as a full class path first, then as a class in
  `/Script/Engine.` and in `/Script/CoreUObject.`, then as a loaded class name.
  `Actor`, `Pawn` or `Character` resolve through the engine prefix. A Blueprint
  parent needs its generated class path, with the `_C` suffix:
  `/Game/Blueprints/BP_Base.BP_Base_C`. Failure: `Parent class not found: <name>`.
- `blueprint_type` (default `Normal`): `Normal` (also `Actor`, `Object`),
  `FunctionLibrary` (also `function_library`), `Interface`, `MacroLibrary`
  (also `macro_library`, `macro`). An unknown value silently creates a `Normal`
  Blueprint.

The tool does not check whether an asset already exists at that path. Look
with `blueprint_query list` first. It runs no compile step of its own: the
result's `compiled: true` is a fixed value, not a compile result. When
`set_cdo_default` or `blueprint_query get_defaults` then reports no generated
class, run `compile`. The result has `blueprint_name`,
`blueprint_path`, `parent_class` and `blueprint_type`. The new asset is marked
as modified but not saved.

## reparent

Sets `parent_class`, resolved as in `create`, then refreshes every node and
compiles. When the Blueprint already has that parent, it returns
`Blueprint already has this parent class` and changes nothing. The result adds
`new_parent_class` (the class path). Reparenting can break nodes that used the
old parent's members. Read the compile result.

## add_variable, remove_variable

`variable_name` follows the same rule as `blueprint_name` in `create`. Errors:
`Variable '<n>' already exists`, `Variable '<n>' not found`.

`variable_type` takes these spellings:

| Kind | Spellings |
|---|---|
| boolean | `bool`, `Boolean` |
| integers | `int`, `int32`, `Integer`, `int64`, `byte`, `uint8`, `Byte` |
| floating point | `float`, `Float`, `double`, `Double` |
| text | `FString`, `String`, `FName`, `Name`, `FText`, `Text` |
| structs | `FVector`, `FRotator`, `FTransform`, `FLinearColor`, `FColor`, `FVector2D`, each also without the `F` |
| other structs | the struct's object path, such as `/Script/Engine.HitResult` |
| object reference | a class name followed by `*` |
| containers | `TArray<T>`, `TSet<T>` with any of the above as `T` |

For an object reference, the class name comes **without** its C++ prefix:
`Actor*`, `StaticMeshComponent*`, `Object*`. The name is tried as a loaded
class, then in `/Script/Engine.` and `/Script/CoreUObject.`. The `AActor*` in
the tool's own description does not match a class name that way. A class from
another module needs its object path: `/Script/UMG.UserWidget*`.

There is no map type, no enum type, no class reference and no soft reference.
Errors: `Unknown type: <t>`, `Unknown class: <c>`.

`add_variable` sets only name and type. The tool sets no default value,
category or flags; `blueprint_query inspect` with `include_variables: true`
shows what the variable got. To give it a default value, run
`set_cdo_default` with the variable name as `property` afterwards.
`remove_variable` does not look for nodes that still use the variable. Run
`blueprint_query find_nodes` with `search` set to the variable name first.

## add_function, remove_function

`add_function` creates an empty function graph with its entry node.
`function_name` follows the same character rule. It takes no inputs, outputs or
flags, so there is no way to add parameters through this tool. Fill the body
with the operations in section `graphs`, passing `graph_name` set to the
function name and `is_function_graph: true`. Errors:
`Function '<n>' already exists`, `Function '<n>' not found`.

## add_component, remove_component

`add_component` adds a component to the Blueprint's construction script. It
works only on Actor-based Blueprints:
`Blueprint has no SimpleConstructionScript (not an Actor-based BP?)`.

- `component_class`: a class name such as `StaticMeshComponent`, or a class
  path. Errors: `Component class '<c>' not found`,
  `'<c>' is not an ActorComponent class`.
- `component_name`: the variable name for the new component. Use the
  `component_name` that comes back in the result.
- `attach_to`: the variable name of the parent component. When no component of
  the Blueprint has that name, the tool assumes a component inherited from the
  C++ parent class and attaches by that name **without checking it**. A typo
  is not reported. Read `get_components` before you set it.
- `socket_name`: the bone or socket on the parent to attach to.

Without `attach_to`, the component is added at the top level. The result has
`component_name`, `component_class`, and `attached_to` and `socket_name` when
given.

`remove_component` removes only components added in this Blueprint:
`SCS component '<n>' not found. Available SCS components: …`. Components
inherited from the C++ parent cannot be removed. With `promote_children: true`,
the first child takes the removed component's place. Without it, check
`get_components` afterwards for what became of the children. The result has `removed_component`, `removed_class` and,
when children were promoted, `promoted_children`.

## set_component_default, set_cdo_default

Both write a default value through reflection. `set_component_default` writes
to a component template, `set_cdo_default` to the Blueprint's class default
object. `value` is required, and its JSON type matters.

`set_component_default` finds `component_name` among the components added in
the Blueprint by variable name, then among the components of the class default
object by object name. Failure:
`Component '<n>' not found in Blueprint '<path>'. Use blueprint_query get_components to see available components.`

`set_cdo_default` looks `property` up on the Blueprint's own class, its
variables and the inherited C++ properties. It needs a compiled Blueprint. A
property that belongs to a component is not found there by its plain name. Use
`set_component_default`, or a path through the component property. The tool's
own example sets `MaxWalkSpeed` this way, which only works for a class that has
that property itself.

**`property`** is a property name or a dot path. Each step before the last must
be a struct property or an object property. Struct steps lead into the struct,
object steps into the object it points to. A null object fails:
`Nested object '<x>' is null`. The names `SkeletalMeshAsset`, `SkeletalMesh` and
`skeletal_mesh` are rewritten to `SkinnedAsset`. Errors:
`Property '<p>' not found on <class>`,
`Cannot navigate through '<p>' (type: <t>) — not a struct or object property`.

**`value`** by property type:

| Property type | `value` |
|---|---|
| float, double, integer, byte (also `TEnumAsByte` enums) | a JSON number |
| bool | `true` or `false` |
| FString, FName | a string |
| enum (`enum class`) | the enum name as a string, or the number |
| object reference | the object path as a string; `""` or `"None"` clears it |
| class reference | the class path, with `_C` for a Blueprint class; `""` or `"None"` clears it |
| soft object or soft class reference | the path as a string |
| map | a JSON object; it **replaces** the whole map |
| struct | see below |

For enums, the plugin's domain notes call for the qualified name
`EMyEnum::ValueName`. The number is written as the enum's value directly. Text
(`FText`) and array properties are not supported:
`Unsupported property type '<t>' for: <path>`. A failed map entry fails the
call and leaves the map partly rewritten.

Struct values:

- **`FVector`**: `{"x": …, "y": …, "z": …}`. **`FRotator`**:
  `{"pitch": …, "yaw": …, "roll": …}`. Give every component: the struct is
  built new from the object, not merged into the current value.
- **`FColor`**: `{"r", "g", "b"}` 0–255, `a` defaults to 255.
  **`FLinearColor`**: `{"r", "g", "b"}` 0–1, `a` defaults to 1. If any of r, g
  or b is above 1.5, r, g and b are read as 0–255, and so is `a` when it is
  above 1.5. Both also take a hex string,
  `"#RRGGBB"` or `"#RRGGBBAA"`.
- **Any struct**: a string in UE text form, `"(X=1.0,Y=2.0,Z=3.0)"`. Another
  JSON object is converted to that form with upper-cased keys; only number and
  string fields carry over.

Errors name the expected form: `Failed to set numeric property '<p>'`,
`Failed to set bool property '<p>' — value must be true/false`,
`Failed to set struct property '<p>' (type: F<s>). Supported: JSON object, hex color, or UE text format.`,
`Failed to load object '<v>' for property '<p>' (expected class: <c>)`,
`Failed to load class '<v>' for property '<p>'`.

Through the bridge, `value` is listed as a string, because the tool declares
it `any`. The tool reads `value` with the engine's `FJsonValue::TryGetNumber`,
`TryGetBool` and `TryGetString` (section `failures`, "Errors common to both
tools", for the engine source). If your client sends only strings, struct values
still work in UE text form, and for the other types:

- A float or double property reads a string made of an optional sign, digits
  and at most one dot, such as `"0.5"`. Any other string fails with
  `Failed to set numeric property '<p>'`.
  A lone sign or dot, such as `"-"` or `"."`, passes too and reads as 0
  (`TCString::IsNumeric`, `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
- **An integer or byte property, a `TEnumAsByte` enum included, accepts any
  string.** The read is into an `int64`: a string counts by its leading
  digits, and one without any is stored as 0, with no error. An enum value's
  name therefore writes 0.
- **A bool property accepts any string and any number.** `true`, `yes`, `on`
  and `false`, `no`, `off` count in any case; every other string is `false`
  unless it starts with a non-zero integer. A typo writes `false`, so
  `Failed to set bool property …` is left for `null`, an array or an object.
  The editor language's words for True, Yes, False and No count as well
  (`FToBoolHelper`, `Engine/Source/Runtime/Core/Private/Misc/CString.cpp`).
- An FString or FName property stores a JSON number or boolean as its text,
  `5` or `true`.

By the plugin's domain notes, class defaults reach only actors spawned later,
and actors already placed in a level keep their saved values (section
`failures`, "Failure modes").

## add_debug_print, remove_debug_print

`add_debug_print` builds a print subgraph into the Blueprint's first event
graph. It prints each name in `variables` and the return value of each call in
`functions`. At least one is required:
`At least one variable or function is required`.

- `label` names the group. Every node it creates gets an ID starting with
  `DbgPrint_<label>_`. Running it again with the same label first removes the
  earlier group, so it is safe to repeat.
- `event` (default `BlueprintUpdateAnimation`, the Animation Blueprint update
  event): the event to hook into. An event node whose function has that name
  is reused. Its current exec chain moves to the last output of the inserted
  Sequence node and still runs. Without one, the tool creates the event, as
  `add_node` with node type `Event` would (section `graphs`). For an Actor
  Blueprint, pass the engine function name, `ReceiveBeginPlay` or
  `ReceiveTick`. That name matches an existing event node. `BeginPlay` and
  `Tick` would not, so they add a second event node next to the existing one.
- `variables`: variable names of the Blueprint or its parent class:
  `Variable '<v>' not found in Blueprint or parent class`.
- `functions`: objects with `function`, optional `target_class`, `params` and
  `label`. `target_class` is tried as a loaded class name and as
  `AnimInstance`, `KismetSystemLibrary` or `KismetStringLibrary`. A function
  not found there is looked up in `KismetSystemLibrary`:
  `Function '<f>' not found`.
  `params` sets input pins by name. Numbers in it are cut to integers.
- `print_to_screen` (default false), `print_to_log` (default true).

The result has `label`, `nodes_created` and `node_ids`.

`remove_debug_print` deletes every node of the group and reconnects the event
to the chain that ran after it. It returns `nodes_removed`, which is 0 for an
unknown label.

**A partial failure loses the old chain.** When `add_debug_print` fails after
it has started building, the nodes built so far stay in the graph. An event's
existing exec chain is not reattached. `remove_debug_print` with the same label
removes the nodes but cannot restore that link. Reconnect it with
`connect_pins` and check with `blueprint_query get_exec_chain`.

## compile

Compiles one Blueprint. **It always returns success**, also when the compile
fails. The message is `Compile <path>: <status> (<n> errors, <m> warnings)`.
Read the result data:

- `compiled`: true or false.
- `compile_status`: `UpToDate`, `UpToDateWithWarnings`, `Error`, `Dirty` or
  `Unknown`.
- `error_count`, `warning_count`, `compile_output` and `compile_messages`
  (each with `severity`, `message`, and `node` and `object_path` when known),
  present on failure or when there are warnings.

## compile_all

Compiles every Blueprint under `path` (default `/Game`) and its subfolders.
`path` passes the same path check as `blueprint_path`. It also returns success
whatever the outcome. The result has `scan_path`, `total`, `succeeded`,
`failed` and `with_warnings`. It also has `failures` (each with `path`,
`status` and the error `messages`) and `warnings` (each with `path` and the
warning `messages`) when there are any.

`compile_all` loads every Blueprint under the path, and a large project takes
time. Narrow `path` to the folder you changed. It marks each Blueprint as
structurally modified before it compiles it. Save what you changed with
`asset save_asset` rather than `save_all` afterwards.
