# metasound: workflows

Step lists with the exact calls. None has been run against an editor: the
steps follow from the toolkit's source (commit c1b6709) and the UE 5.8.4
engine source (read, not run). Sections `nodes` and `editing` hold the why.

## A tone, built node by node

1. **Free name.** `asset_search` with `path_filter` `/Game/Audio` and
   `name_pattern` `MS_Tone` (skill `ue-assets`).
2. **Create.** `metasound`
   `{"operation": "create", "package_path": "/Game/Audio", "name": "MS_Tone"}`;
   keep the returned `asset_path`.
3. **Look at what it starts with.** `inspect`: `interfaces` should include
   `UE.OutputFormat.Mono`, `graph_outputs` `UE.OutputFormat.Mono.Audio:0`
   (type `Audio`). Then `get_graph`: note the `node_id` of the node with the
   input pin `UE.OutputFormat.Mono.Audio:0`, and that no edge ends there.
4. **Pick the class.** `metasound`
   `{"operation": "list_nodes", "name_filter": "Sine"}`: the oscillator is
   `namespace` `UE`, `full_name` `UE.Sine.Audio`.
5. **Save first:** `asset save_all`. The next steps edit the graph with no
   undo, and `connect` can assert (section `editing`).
6. **Add it.** `metasound`
   `{"operation": "add_node", "asset_path": "<asset_path>", "node_namespace": "UE", "node_class_name": "Sine.Audio"}`
   — the name carries the variant (section `nodes`; derived from the engine
   source, not run). Keep `node_id`. If it fails, read
   `get_output_log` `{"categories": ["LogMetaSound"], "lines": 20}`.
7. **Read its pins.** `get_graph`: on the new node, `Frequency` (type
   `Float`) among the inputs and `Audio` (type `Audio`) as the output.
8. **Set the pitch.** `metasound`
   `{"operation": "set_input_default", "asset_path": "<asset_path>", "node_id": "<sine>", "input_name": "Frequency", "value": "440", "data_type": "float"}`.
9. **Connect.** Both pins are `Audio`, and step 3 showed the output's input
   free:
   `{"operation": "connect", "asset_path": "<asset_path>", "from_node_id": "<sine>", "from_pin": "Audio", "to_node_id": "<output node>", "to_pin": "UE.OutputFormat.Mono.Audio:0"}`.
10. **Confirm** with `get_graph`: an edge from the Sine's `Audio`
    `vertex_id` to the output node's.
11. **Save:** `asset save_asset` with the path.
12. **Listen.** `preview`, ask the user whether a tone is heard, then
    `stop_preview`. The tone does not end on its own: nothing triggers
    `UE.Source.OneShot.OnFinished`.
13. **Check the log.** `get_output_log`
    `{"categories": ["LogMetaSound"], "min_verbosity": "Warning", "since": true}`.

## A parameter that gameplay sets

The goal: a graph input such as `Pitch` (type `Float`), connected to the
Sine's `Frequency`, which a Blueprint sets with **Set Float Parameter** on
the audio component that plays the MetaSound.

Create the input by one of two routes, both outside this tool:

- **The user, in the MetaSound editor**: add the input in the Members panel
  and connect it (Epic, MetaSounds Reference Guide: inputs are created in the
  Members panel and allow value changes during playback).
- **The Builder API** (below), through Python.

**Never through `add_graph_input` on UE 5.8.x**: the engine asserts on every
call the toolkit makes and the editor crashes (section `editing`). On 5.7
the check is unverified; there, and only with the user's consent after
`asset save_all`, the tool route is:

1. `metasound`
   `{"operation": "add_graph_input", "asset_path": "<asset_path>", "input_name": "Pitch", "data_type": "Float", "default_value": "440"}`;
   keep `input_node_id`.
2. `get_graph`: the input node's output pin is most likely named `Pitch`.
   If the Sine's `Frequency` already has an edge, `disconnect` it.
3. `connect` from `<input_node_id>` / `Pitch` to `<sine>` / `Frequency`,
   then `get_graph` to confirm, then save.

Then the playing side:

1. Give an actor or Blueprint an audio component with this MetaSound as its
   sound: skill `ue-audio`, section `workflows`.
2. The Blueprint call is **Set Float Parameter** with `In Name` `Pitch`
   (also Set Integer / Boolean Parameter, Execute Trigger Parameter; UE 5.8.4
   `AudioParameterControllerInterface.h`). Writing that event graph belongs
   to skill `ue-blueprints`, and in a project with the shipped agents to
   agent `unreagent-gameplay`: hand over the component name and the input
   name and type.
3. A **constructor** input takes its value only before the sound plays
   (Reference Guide); a normal input follows changes during playback.

## Changing an existing MetaSound

1. `inspect` and `get_graph`; read the connections you will touch
   (section `reading`, "Reading a connection").
2. `asset save_all`.
3. `disconnect` the old edge (exact `from_*` and `to_*` from step 1),
   `connect` the new one after the same two checks, `get_graph` to confirm.
4. Save, `preview`, `stop_preview`.

## The Builder API path

Epic's MetaSound Builder API (documented as **Beta**) builds a MetaSound
from Blueprint or C++: Create Source Builder, Connect Nodes, Build To Asset,
Audition. In the UE 5.8.4 headers it also takes what this tool cannot: a
class name with its variant (`AddNodeByClassName` with an
`FMetasoundFrontendClassName` and a major version), graph inputs with a
default and a constructor flag (`AddGraphInputNode`), output format and
interfaces (`SetFormat`, `AddInterface`, `RemoveInterface`). The functions:
`UMetaSoundBuilderSubsystem::CreateSourceBuilder`
(`MetasoundBuilderSubsystem.h`), `AddGraphInputNode`, `AddNodeByClassName`,
`ConnectNodes`, `SetNodeInputDefault`, `FindGraphInputNode`
(`MetasoundBuilderBase.h`), and `UMetaSoundEditorSubsystem::BuildToAsset`
(`MetasoundEditorSubsystem.h`).

From Python (`execute_script`, skill `ue-python`) these are reachable as
`unreal.MetaSoundBuilderSubsystem` and so on, under the usual snake_case
names; **none of this has been exercised, and the Python signatures were not
checked** (out parameters such as the build result come back differently in
Python). Treat it as Class B: write a short script, read its result and the
`LogMetaSound` lines, and say in your report that the route was untested.

## Verifying

- **Structure**: `inspect` (counts, interfaces, members) and `get_graph`
  (every edge you made). Neither shows input defaults.
- **Sound**: `preview` / `stop_preview`; only the user can say what is heard.
- **Log**: `get_output_log` with `categories: ["LogMetaSound"]` (skill
  `ue-pie-testing`, section `log`). The engine logs a failed `add_node`, a
  taken member name and an unregistered type there (UE 5.8.4 source).
- **In play**: place it (skill `ue-audio`), then PIE (skill
  `ue-pie-testing`).
