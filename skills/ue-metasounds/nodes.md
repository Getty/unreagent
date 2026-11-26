# metasound: node classes, variants, data types, a new Source's members

What `add_node` accepts, which names to use, and what a freshly created
MetaSound Source contains. The toolkit facts are from its source (commit
c1b6709). The engine facts are from the **UE 5.8.4 engine source, read, not
run**; the toolkit targets UE 5.7, whose source was not checked. Confirm each
with `list_nodes` and `get_graph` before you rely on it.

## How a class is named

A MetaSound node class name has three parts: a namespace, a name, and an
optional variant. `list_nodes` returns them as `namespace`, `class_name`
and `full_name`, where `full_name` is `<namespace>.<name>`, or
`<namespace>.<name>.<variant>` when there is a variant
(`FNodeClassName::FormatFullName`, `MetasoundNodeInterface.cpp`). There is no
separate variant field: **a `full_name` longer than
`<namespace>.<class_name>` carries a variant.**

`list_nodes` returns only the **highest version** of each class: the toolkit
calls `FindAllClasses(false)`, which asks for the highest version only.

## Why add_node fails for most audio nodes, and the workaround

- **The toolkit passes no variant.** `add_node` builds the class name from
  `node_namespace` and `node_class_name` alone and asks for major version 1
  (`MetaSoundEditor.cpp`, `AddNode`).
- **The engine looks the class up by its full name plus the major
  version**, as the text `<full_name>_v<major>`
  (`CreateKeyFromFullClassNameAndMajorVersion`,
  `MetasoundFrontendSearchEngineCore.cpp`). So `namespace` `UE` with
  `class_name` `Sine` looks for `UE.Sine_v1`, while the oscillator is
  registered as `UE.Sine.Audio`: not found, and `add_node` fails with
  `Failed to add node: UE::Sine`.
- **Most standard audio nodes have the variant `Audio`** in UE 5.8.4: the
  oscillators `Sine`, `Saw`, `Square`, `Triangle` and `LFO`, `Noise`, and the
  filters such as `Biquad Filter`, `Ladder Filter` and
  `State Variable Filter` (`MetasoundStandardNodes`). `Wave Player` has a
  channel variant. Some classes have none, such as `UE.TriggerRepeat`.
- **Workaround, derived from the source and not run:** pass the variant as
  part of the name. Set `node_namespace` to the entry's `namespace` and
  `node_class_name` to `full_name` with `<namespace>.` cut off the front, such
  as `Sine.Audio` for `UE.Sine.Audio`. The tool then formats `UE.Sine.Audio`,
  the same text the engine looks up, and the engine adds its registered class.
  If that fails too, the error in the editor log names the class
  (`get_output_log` with `categories: ["LogMetaSound"]`, skill
  `ue-pie-testing`): `Failed to add new node by class name '<name>' and major
  version '1': Class info not registered`.
- **`get_graph` does not show variants.** A node added as `Sine.Audio` is
  listed with `class_name` `Sine` and `namespace` `UE`: the toolkit writes
  only those two parts.
- **Major version 1 only.** A class whose `version` in `list_nodes` is `2.x`
  or higher may or may not still be registered at major version 1; the tool
  cannot ask for another. If it fails, tell the user.

## Pins of common nodes

Read pin names and types from `get_graph` after `add_node`; copy them exactly.
For orientation, the names in the UE 5.8.4 source:

| Class (`full_name`) | Inputs | Outputs |
|---|---|---|
| `UE.Sine.Audio` (1.1) | `Enabled`, `Bi Polar`, `Frequency`, `Modulation`, `Sync`, `Phase Offset`, `Glide`, `Type` | `Audio` |
| `UE.Saw.Audio`, `UE.Triangle.Audio` | as `Sine` | `Audio` |
| `UE.Square.Audio` | as `Sine`, plus `Pulse Width` | `Audio` |
| `UE.LFO.Audio` | `Frequency`, `Shape`, `Min Value`, `Max Value`, `Sync`, `Phase Offset`, … | `Out` |
| `UE.TriggerRepeat` | `Start`, `Stop`, `Period`, `Num Repeats` | `RepeatOut` |

`Frequency` is in Hz. Pin names hold spaces; send them as they are.

## Data type names

The type names the engine registers, which `get_graph` and `inspect` report
as `type` and which `add_graph_input` and `add_graph_output` take as
`data_type`: `Float`, `Int32`, `Bool`, `String`, `Trigger`, `Time`, `Audio`,
`WaveAsset`, and others (`MetasoundPrimitives.cpp`,
`MetasoundEngineModule.cpp`). Copy the `type` of the pin you want to connect.

`set_input_default` and `add_graph_input` convert a value by their own,
lowercased `data_type`: `float` (or `Float`), `int32`, `bool`, anything else
as a string (section `editing`). A `Time` pin takes a float literal, so pass
`data_type: "float"`.

## What a new MetaSound Source contains

`create` calls the engine's `InitDocument`, which adds the default
interfaces of the MetaSound Source class (`InitDocument`,
`MetasoundFrontendDocumentBuilder.cpp`). In UE 5.8.4 those are:

| Interface | Brings |
|---|---|
| `UE.Source` (1.1) | graph input `UE.Source.OnPlay`, type `Trigger`: fires when the sound starts |
| `UE.Source.OneShot` | graph output `UE.Source.OneShot.OnFinished`, type `Trigger`: firing it ends the sound |
| `UE.OutputFormat.Mono` | graph output `UE.OutputFormat.Mono.Audio:0`, type `Audio`: what you hear |

That is why `output_format` does not matter: Mono is the default, and the
tool cannot switch it. Check the actual list with `inspect` (`interfaces`,
`graph_inputs`, `graph_outputs`); on UE 5.7 it may differ.

- **Connect your audio to the output node's input** named
  `UE.OutputFormat.Mono.Audio:0` (most likely; the builder names a member's
  node vertex after the member, `SetNodeAndVertexNames`). Find that node in
  `get_graph` by the pin name.
- **One-shot sources play until `OnFinished` fires.** Epic's MetaSounds
  Reference Guide: "If the On Finished node is not triggered, the MetaSound
  will play indefinitely", and for music or ambience the `UE.Source.OneShot`
  interface should be removed. This tool cannot remove an interface. A tone
  built here therefore plays until it is stopped: call `stop_preview` after
  `preview`, and in the level stop it from gameplay or leave it as a looping
  sound on purpose.
- Removing an interface, switching to Stereo, or making a graph input a
  constructor input is editor work, or the Builder API (section
  `workflows`, "The Builder API path").
