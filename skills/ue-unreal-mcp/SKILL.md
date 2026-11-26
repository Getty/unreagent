---
name: ue-unreal-mcp
description: >
  Use Epic's Unreal MCP server (UE 5.8, MCP server unreal-mcp): its three
  meta-tools list_toolsets, describe_toolset and call_tool, the 52 toolsets
  behind them, and the engine's own agent skills. Use when mcp__unreal-mcp__*
  tools are present, and for PCG, Dataflow, GAS, MVVM, StateTree, Behavior
  Trees, Slate UI automation, automation tests, config settings, plugins,
  gameplay tags or Live Coding, which the UE LLM Toolkit lacks.
---

# Epic's Unreal MCP server

Unreal MCP is an MCP server built into the UE 5.8 editor (plugin
`ModelContextProtocol`, experimental). It serves tools that live in toolset
plugins (`AllToolsets` and others) through Epic's Toolset Registry. It runs
inside the editor process, next to the UE LLM Toolkit plugin, and has no
Node bridge.

Sources. The engine's C++ source of UE 5.8.4; paths below are relative to
`Engine/Plugins/Experimental/`. And measurements taken on Windows with the
Launcher build of UE 5.8.4 (CL 58902052) and Claude Code 2.1.330. A fact
marked "measured" was seen there; "source" means read in the C++ and not run; "unverified" means neither. The Python toolsets (more than
half of the toolsets) were not available as source: everything about them is
measured or unverified.

This skill has four sections:

| Section | Covers | Load it |
|---|---|---|
| `toolsets` | the 52 toolsets by area, their names, sizes, and which are only here or also in the UE LLM Toolkit | before you pick a toolset, and when you decide between Unreal MCP and the toolkit |
| `catches` | per tool: what the description promises and what was measured (viewport capture, Live Coding, automation tests, config settings, plugins, PIE, logs, Slate) | before your first call into one of those toolsets, and when a result looks like success but nothing changed |
| `agent-skills` | the engine's own skills (`ListSkills`, `GetSkills`) and the rules for `CreateSkill` / `UpdateSkill` | before PCG, Dataflow, Niagara, blueprint, material or lighting work through Unreal MCP, and whenever the user mentions an agent skill in the project |
| `server` | how the server is started, its port, sessions, editor restarts, `BlockedNames` / `AllowedNames`, tool-search mode off, `GenerateClientConfig` | when the `unreal-mcp` tools are missing or fail, a toolset is missing, or someone wants to change the server's settings |

## How you reach it

- **The tools are `mcp__unreal-mcp__list_toolsets`,
  `mcp__unreal-mcp__describe_toolset` and `mcp__unreal-mcp__call_tool`** in
  Claude Code (server name `unreal-mcp`). Other clients show the three
  meta-tools under their own prefix. No other tool is listed (measured,
  default settings).
- **unreagent starts it with the config key `unreal.mcp`** —
  `{enabled, port}`, off by default, port 8000; it needs `mcp.enabled`. With
  it on, the launcher adds `-ModelContextProtocolStartServer` and
  `-ModelContextProtocolPort=N` to the editor's command line and writes an
  http entry `unreal-mcp` into the agent's MCP config and every
  `mcp.writeConfig` file. Changing the launcher config is the user's call
  (skill `unreagent-setup`); section `server` has the details.
- **The server must answer before the agent connects.** Claude Code that
  finds it down at its start marks it failed after about 6 s and never
  retries; that session has no `mcp__unreal-mcp__*` tools for its whole life
  (measured). Interactively, `/mcp` reconnects. With `unreal.mcp` on, the
  launcher therefore holds the agent's first start until the server answers
  a ping (section `server`). An editor restart while the session runs is
  harmless: the client reinitializes by itself (measured).
- **Not every project has it.** It needs the plugins `ModelContextProtocol`
  and `AllToolsets` enabled in the `.uproject` (both are off by default) and
  the start flag. No `mcp__unreal-mcp__*` tools means it is not set up; use
  the UE LLM Toolkit and do not set it up on your own.

## The three meta-tools

Source: `ModelContextProtocol/Source/ModelContextProtocolEditor/Private/`
`ModelContextProtocolToolSearch.cpp` and
`ModelContextProtocolToolsetRegistryAdapter.cpp`.

| Tool | Input | Returns |
|---|---|---|
| `list_toolsets` | `{}` | one line per toolset, `- <name>: <description>` (Adapter.cpp:300-321); about 14 KB for 52 toolsets (measured) |
| `describe_toolset` | `{"toolset_name": "<name>"}`, required | the toolset's JSON schema: every tool with `name`, `description` and `inputSchema` (Adapter.cpp:323-346); an unknown name is an error that lists every available toolset |
| `call_tool` | `{"toolset_name": "<toolset>", "tool_name": "<tool>", "arguments": {…}}` | the tool's result |

`call_tool`'s arguments (ToolSearch.cpp:93-146):

- `tool_name` — required. The tool's name **without** the toolset prefix:
  `IsPIERunning`, not `EditorToolset.EditorAppToolset.IsPIERunning`.
- `toolset_name` — the full toolset name as `list_toolsets` prints it, e.g.
  `EditorToolset.EditorAppToolset` or
  `editor_toolset.toolsets.blueprint.BlueprintTools`. Give it for every
  toolset tool. Omitted, `tool_name` is looked up among the server's own
  top-level tools instead (Adapter.cpp:281-297), which in the default mode
  are only the three meta-tools; `call_tool` refuses to call itself
  (Adapter.cpp:273-279).
- `arguments` — an object matching the tool's `inputSchema`; omitted, `{}`.

A call that works (measured, the exact payload Claude Code sent):

```json
{"toolset_name": "EditorToolset.EditorAppToolset", "tool_name": "IsPIERunning", "arguments": {}}
```

## Results

- **A text block holding JSON with one key, `returnValue`**
  (`ToolsetRegistry/.../ObjectFunctionToolCall.cpp:515-520`): `{"returnValue":
  false}`. A tool without a return value gives `{"returnValue": null}`
  (measured: `StartPIE`, `SetPluginEnabled`).
- **Some tools return a JSON *string* inside `returnValue`** — parse it a
  second time (measured: `AutomationTestToolset` `ListTests`, `RunTests`,
  `GetTestStatus`). Expect the same from any tool declared to return
  `FString` whose description says it returns JSON (inference).
- **An error is an MCP result with `isError: true`** and the message as text
  (measured: unknown toolset, missing parameter). **But a tool can report
  failure as an ordinary `returnValue`** — `CompileLiveCoding` returns
  `"Error: Live Coding is not enabled …"` without `isError` (measured). Read
  the value, not only the flag.
- **An omitted argument needs a default in the schema.** Leaving out a
  parameter that has none fails with `Function "X", input param "y" needs a
  default value` (`ObjectFunctionToolCall.cpp:221-240`) — even when the
  schema calls it optional. Pass it explicitly. `CaptureViewport` is the
  measured case (section `catches`).
- **Images come back as base64 text**, not as an MCP image block: a viewport
  capture is 0.86–0.94 MB of text in your context (measured). Capture only
  when you need the picture.

## Must not get wrong

1. **Describe only the toolset you need.** `describe_toolset` returns the
   whole schema: `NiagaraToolsets.NiagaraToolset_System` is 330 KB,
   `animation_toolset.toolsets.sequencer.SequencerTools` 141 KB, all 52
   together 1.37 MB (measured). Never describe toolsets in a loop to "get an
   overview"; `list_toolsets` and section `toolsets` are the overview.
2. **Names match without regard to case** (measured for toolset and tool
   names), but use the spelling `list_toolsets` prints.
3. **Descriptions are localized** into the editor's language — German on the
   measuring machine (measured). Tool and parameter names are not.
4. **No confirmation.** The server runs a call as soon as it arrives, with no
   prompt on its side (source: `ModelContextProtocolServer.cpp:786-860`,
   `ModelContextProtocolToolsetRegistryAdapter.cpp:233-297`).
   unreagent's permission layer sees every inner call as the one tool
   `mcp__unreal-mcp__call_tool`; a rule cannot single out a toolset, so an
   allow lets through every toolset tool at once.
5. **No C++ toolset runs Python, a console command or an arbitrary
   function** (source). The Python `editor_toolset` has modules named
   `programmatic` and `object`, and the registry's library has a generic
   `SetObjectProperties` (`ToolsetRegistry/Source/ToolsetRegistry/Public/`
   `ToolsetRegistry/ToolsetLibrary.h:83`); what those Python toolsets expose
   is unverified. For Python and console commands, use the toolkit
   (`ue-python`, `ue-pie-testing`).
6. **A toolset can be hidden** by the project's `BlockedNames` /
   `AllowedNames`; it is then absent from `list_toolsets` and every call to it
   answers "Toolset not found" (measured). Section `server`.
7. **`CreateSkill` and `UpdateSkill` only on the user's explicit instruction**
   (`ToolsetRegistry/.../AgentSkill.h:81-103`). Section `agent-skills`.

## Unreal MCP or the UE LLM Toolkit

Both can run in one editor (measured: toolkit on port 3000 and Unreal MCP on
8000 answered at the same moment). The short rule:

- **Only in Unreal MCP** — PCG, Dataflow, physics assets, GAS (gameplay
  cues, attribute sets, ability system inspection), MVVM, StateTree, Behavior
  Trees, Conversation, Slate UI automation, automation tests, config
  settings, plugin management, gameplay tags, data registries, game features,
  world conditions, Live Coding, semantic asset search, Chaos cloth assets.
- **Only in the toolkit** — Python (`execute_script`), console commands,
  the task queue, Enhanced Input, MetaSounds and audio, lighting presets,
  retargeting, character data, montage editing, scripted PIE input sequences.
- **In both** — blueprints, assets, level actors, materials, Niagara, UMG,
  sequencer, control rig, the viewport capture, PIE start/stop, the output
  log. Prefer the toolkit where the project's skills (`ue-blueprints`,
  `ue-materials`, …) already document the toolkit's tool; take Unreal MCP
  where it is deeper (Niagara: 56 C++ tools; Sequencer: 140 Python tools) or
  the toolkit fails at the task.

The overlap list and the sizes are in section `toolsets`.
