# The toolsets

What `list_toolsets` returns in a project with `ModelContextProtocol` and
`AllToolsets` enabled, by area, and how it compares to the UE LLM Toolkit.
Paths are relative to `Engine/Plugins/Experimental/` of the UE 5.8.4 source.

## Names and counts

- **52 toolsets, 830 tools** with `AllToolsets` (measured on UE 5.8.4, Launcher build CL 58902052). The
  descriptions over all 52 total 1.37 MB; `list_toolsets` itself is 14 KB.
- **Two kinds of names** (`ToolsetRegistry/Source/ToolsetRegistry/Private/`
  `ToolsetRegistry/FunctionLibraryToolset.cpp:211-240`):
  - a C++ toolset is `<Module>.<Class>`, the class without its `U`:
    `EditorToolset.EditorAppToolset`, `NiagaraToolsets.NiagaraToolset_System`.
    25 of the 52 (measured).
  - a Python toolset is `<package>.<module>.<Class>`:
    `editor_toolset.toolsets.blueprint.BlueprintTools`. 27 of the 52
    (measured).
- **A toolset name in the tables below is exact where marked "measured"**;
  the other C++ names are derived from the class and module by that rule
  (source, not seen in a listing). Python names other than the measured ones
  are not given: take them from `list_toolsets`.
- **Tool counts**: C++ counts are the `AICallable` functions in the toolset's
  header (source); Python counts are measured.

## Editor, level and assets

| Toolset | Tools | What |
|---|---|---|
| `EditorToolset.EditorAppToolset` (measured) | 21 | CVars search, asset thumbnail, editor screenshot, `CaptureViewport`, actor selection, viewport camera get/set, focus, visible actors, world↔screen coordinates, content browser path and selection, open asset editor, PIE start/stop/status (`EditorToolset/Source/EditorToolset/Private/EditorAppToolset.h:197-379`) |
| `EditorToolset.LogsToolset` | 4 | `GetLogEntries` (category, pattern, max entries), `GetLogCategories`, `GetVerbosity`, `SetVerbosity` (`LogsToolset.h:29-58`) |
| `editor_toolset.toolsets.*` (Python) | 16 toolsets | actors, assets, blueprints (graph nodes, layout, a blueprint DSL), curve tables, data assets, data tables, materials and material instances, objects, primitives, `programmatic` (contents unverified), the scene, skeletal and static meshes, string tables, textures — module names from the engine's file list; the source was not available, `editor_toolset.toolsets.blueprint.BlueprintTools` is the one measured name |
| `SemanticSearchToolset.SemanticSearchToolset` | 2 | hybrid vector + keyword asset search (plugin description); unverified whether it returns anything without the SemanticSearch index |

## Animation and cinematics (Python, measured)

`AnimationAssistantToolset` brings package `animation_toolset`: 8 toolsets,
319 tools — Sequencer 140, SequencerControlRig 72, ControlRig 44,
Keyframing 22, Outliner 18, Conditions 9, CustomBindings 8,
ImportExport 6. `animation_toolset.toolsets.sequencer.SequencerTools` alone
describes to 141 KB. `SequencerAnimMixerToolset` (package
`sequencer_animmixer_toolset`, 21 tools) is not in `AllToolsets`.

## VFX and UI

| Toolset | Tools | What |
|---|---|---|
| `NiagaraToolsets.NiagaraToolset_System` (measured) | 46 | systems, emitters, modules, stack issues, compile state — **330 KB** when described |
| `NiagaraToolsets.NiagaraToolset_Component`, `_Assets`, `_Blueprint`, `_Info` | 4, 3, 2, 1 | components, asset creation, blueprint interop, info |
| `UMGToolSet.UMGToolSet` | 23 | widget blueprints (`UMGToolSet/Source/UMGToolSet/Private/UMGToolSet.h`) |
| `MVVMToolset.MVVMToolset` | 9 | UMG view models and bindings; **not in `AllToolsets`** |

## Gameplay systems

| Toolset | Tools | What |
|---|---|---|
| `GASToolsets.GameplayCueToolset`, `.AttributeSetToolset`, `.AbilitySystemInspectorToolset` | 8, 2, 4 | gameplay cues, attribute sets, inspecting ability system components |
| `GameplayTagsToolset.GameplayTagsToolset` (measured) | 6 | `ListTags`, `GetTagInfo`, `AddTag`, `RemoveTag`, `RenameTag`, `FindReferencersByTag` (`GameplayTagsToolset.h:45-89`) |
| `GameFeaturesToolset.GameFeaturesToolset` | 7 | game feature plugins |
| `DataRegistryToolset.DataRegistryTools` | 7 | data registries |
| `WorldConditionsToolset.WorldConditionTools` | 2 | world conditions |
| `state_tree_toolset.toolsets.state_tree.StateTreeTools` (measured) | 9 | StateTree assets |
| `aimodule_toolset.toolsets.behavior_tree.BehaviorTreeTools` (measured) | 7 | Behavior Trees |
| `conversation_toolset.toolsets.conversation.ConversationTools` (measured) | 7 | CommonConversation assets |

## Procedural, physics, cloth

| Toolset | Tools | What |
|---|---|---|
| `PCGToolset.PCGToolset`, `PCGToolset.PCGSpatialToolset` | 30, 1 | PCG graphs: build, edit, execute; ships 8 agent skills (section `agent-skills`) |
| `DataflowAgent.DataflowAgentToolset` | 22 | Dataflow graphs; ships a skill |
| `PhysicsToolsets.PhysicsAssetToolset` | 17 | physics assets: bodies, constraints |
| `ChaosClothAssetToolset.ChaosClothAssetToolset` | 6 | cloth assets; **not in `AllToolsets`**; ships a skill |

## Project, plugins, tests, code

| Toolset | Tools | What |
|---|---|---|
| `ConfigSettingsToolset.ConfigSettingsToolset` | 8 | the Project and Editor settings pages: list containers/categories/sections, schema, read, set, save, reset (`ConfigSettingsToolset.h:25-127`) — catches in section `catches` |
| `PluginToolset.PluginToolset` | 17 | list, inspect, create plugins, dependencies, descriptor editing, `SetPluginEnabled` (`PluginToolset.h:170-333`) — `SetPluginEnabled` is measured a no-op, section `catches` |
| `AutomationTestToolset.AutomationTestToolset` | 7 | discover, list, run (by names or filter), status, results, stop (`AutomationTestToolset.h:40-92`) |
| `LiveCodingToolset.LiveCodingToolset` (measured) | 1 | `CompileLiveCoding`; **not in `AllToolsets`** |
| `SlateInspectorToolset.SlateInspectorToolset` | 14 | drive the editor's own UI: widget tree snapshot, observe, screenshot, click, hover, type, press key, select option, drag, windows, wait for text, fill form (`SlateInspectorToolset.h:92-181`) |
| `ToolsetRegistry.AgentSkillToolset` (measured) | 4 | `ListSkills`, `GetSkills`, `CreateSkill`, `UpdateSkill` — section `agent-skills` |

## Not loaded by `AllToolsets`

`AllToolsets` enables 21 toolset plugins (`Toolsets/AllToolsets/`
`AllToolsets.uplugin:14-94`). Not among them: `LiveCodingToolset`,
`MVVMToolset`, `ChaosClothAssetToolset`, `SequencerAnimMixerToolset`,
`MetaHumanGenerator`. Each is a plugin of its own that the project adds to
its `.uproject` by name; until then its toolset is not listed. Ask the user
rather than edit the `.uproject` (an editor restart follows).
`MCPClientToolset` is in `AllToolsets` but is an adapter for connecting the
registry to other MCP servers, not a toolset of its own (plugin description).

## Unreal MCP compared with the UE LLM Toolkit

The toolkit's tools are documented in the project's `ue-*` skills; the
right-hand column names the skill.

| Area | Unreal MCP | UE LLM Toolkit |
|---|---|---|
| Blueprints | `editor_toolset` Python toolsets (graph nodes, layout) | `ue-blueprints` |
| Assets, data tables, data assets | `editor_toolset` | `ue-assets` |
| Level actors, scene | `editor_toolset`, `EditorAppToolset` selection | `ue-level-actors`, `ue-levels` |
| Materials | `editor_toolset` | `ue-materials` |
| Niagara | 56 C++ tools, plus 7 Niagara agent skills | `ue-niagara` |
| UMG widgets | 23 tools, plus MVVM | `ue-widgets` |
| Sequencer, control rig | 319 Python tools | `ue-sequencer`, `ue-control-rig` |
| Viewport capture | `CaptureViewport` (base64 as text, ~0.9 MB) | `ue-viewport` (base64 as text too; files only from `run_sequence` captures) |
| PIE | start, stop, is running | `ue-pie-testing` (also scripted input, captures, input monitor) |
| Output log | `LogsToolset` | `ue-pie-testing` (`get_output_log`) |
| Python, console commands | — | `ue-python`, `ue-pie-testing` |
| Long jobs, task queue | — | `ue-tasks` |
| Anim blueprints, blend spaces, montages, retargeting | — | `ue-anim-blueprint`, `ue-blend-space`, `ue-anim-montage`, `ue-retarget` |
| Audio, MetaSounds | — | `ue-audio`, `ue-metasounds` |
| Enhanced Input, characters, game framework | — | `ue-enhanced-input`, `ue-character`, `ue-character-data`, `ue-game-framework` |
| Lighting | no lighting toolset; one agent skill (`default_outdoor_lighting`) | `ue-lighting` |
| PCG, Dataflow, physics assets, cloth | yes | — |
| GAS, gameplay tags, game features, data registries, world conditions | yes | — |
| StateTree, Behavior Trees, Conversation | yes | — |
| Slate UI automation | yes | — |
| Automation tests | yes | — |
| Project/editor settings, plugins | yes | — |
| Live Coding | yes (own plugin) | — (the launcher's `run_command` builds, skill `unreagent-build`) |

How to choose where both have it:

- **The toolkit first where a `ue-*` skill documents the job** — those skills
  carry the parameter names and traps. Unreal MCP's tools have only their
  (localized) descriptions.
- **Unreal MCP where it is deeper** (Niagara stacks and compile state,
  Sequencer keyframing, UMG) or where the toolkit's tool lacks an operation.
- **One editor, two writers.** Both change the same assets in the same
  editor process. Keep one asset's edit within one route, and check the
  result through that route; how the two interleave on one asset is
  unverified.
