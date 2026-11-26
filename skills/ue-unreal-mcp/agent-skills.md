# The engine's own agent skills

The Toolset Registry carries a skill mechanism of its own: **agent skills**,
written by Epic for its toolsets and loadable through Unreal MCP. They are not
unreagent's skills and do not replace them; they are the engine authors'
instructions for driving their own tools. Paths are relative to
`Engine/Plugins/Experimental/ToolsetRegistry/Source/ToolsetRegistry/`.

## What an agent skill is

A class derived from `UAgentSkill` (`Public/ToolsetRegistry/AgentSkill.h:`
`28-56`) with a short `Description` and longer `Instructions`. It can be a
C++ class, a Python class, or a Blueprint asset. The text you get is
generated when you ask (`GeneratePrompt`, `AgentSkill.cpp:19-24`), so a skill
may assemble its instructions at run time.

Toolset `ToolsetRegistry.AgentSkillToolset` serves them (registered by the
Toolset Registry subsystem itself, `Private/ToolsetRegistry/`
`ToolsetRegistrySubsystem.cpp:49`), so it is there whenever Unreal MCP is.

## Reading them

**`ListSkills`** — `{"toolset_name": "ToolsetRegistry.AgentSkillToolset",
"tool_name": "ListSkills", "arguments": {}}`.

- Returns `{"returnValue": {"<skill path>": "<description>", …}}`
  (`AgentSkill.h:65-70`). A skill with an empty description is left out, and
  so is every path the project's `AgentSkillBlockedNames` /
  `AgentSkillAllowedNames` filter out (`AgentSkill.cpp:26-58`; settings in
  section `server`).
- Measured with `AllToolsets` (UE 5.8.4, Launcher build CL 58902052):
  **20 skills, 6.5 KB** — 8 PCG (`/PCGToolset/Skills/...`), 7 Niagara
  (`/NiagaraToolsets/Python/skills/...`; by the engine's file names:
  fundamentals, renderers, custom HLSL, blueprint interop, level placement,
  optimization, cleanup), 4 from the editor toolset (`blueprint_basics`,
  `default_outdoor_lighting`, `material_basics`,
  `unreal_skill_best_practices`), 1 Dataflow. The Chaos cloth skill appears
  only with `ChaosClothAssetToolset` enabled (it is not in `AllToolsets`).
  Skills a project defines itself are listed as well.

**`GetSkills`** — `"arguments": {"skillPaths": ["<path>", …]}` (argument name
derived, see section `catches`).

- Pass the paths **exactly as `ListSkills` returned them** — they are class
  paths, not asset paths.
- Returns `{"returnValue": {"<path>": {"instructions": "…"}}}`
  (`AgentSkill.h:72-78`, `FAgentSkillDetails`; the inner key is derived from
  the field `Instructions`, not measured). A path that does not load or
  is filtered out is **dropped without an error** (`AgentSkill.cpp:60-86`):
  a missing key in the answer means that.
- The size of one skill's instructions was not measured; fetch the one or two
  you need, not all twenty.

## When to use them

- **Before working a toolset that has skills** — PCG, Niagara, Dataflow,
  cloth, and blueprint, material or outdoor-lighting work through the
  `editor_toolset` tools: `ListSkills` once, `GetSkills` for the one that
  matches, then follow it. They describe the engine's tools (what to call in
  which order); they say nothing about the UE LLM Toolkit.
- **They rank below the project.** The project's `CLAUDE.md` and the user's
  instructions win over an engine skill. Where an engine skill and one of
  unreagent's `ue-*` skills disagree about the toolkit, the `ue-*` skill is
  about the toolkit; the engine skill is about Unreal MCP.
- **Not into `.claude/skills/`.** Do not copy engine skills into the project's
  skill directory; they are fetched live and may differ per engine version.

## Writing them — only when the user says so

`CreateSkill` and `UpdateSkill` both carry the engine's own rule: call them
**only after explicit direction or permission from the user**
(`AgentSkill.h:80-103`). A user asking you to "remember" something is not
that; a user asking for an agent skill in the project is.

- **`CreateSkill`** — `folderPath` (starts with `/`, e.g. `/Game/Skills/`),
  `assetName`, `description`, `details` (`{"instructions": "…"}`). Creates a
  Blueprint asset derived from `UAgentSkill` in the project's content and
  returns its class path, empty on failure (`AgentSkill.cpp:88-131`). Whether
  the new asset is saved to disk without a separate save was not measured;
  tell the user to save it, or save it through the toolkit (skill
  `ue-assets`).
- **`UpdateSkill`** — `skillPath` (the class path, e.g.
  `/Game/Skills/MySkill.MySkill_C`), `description`, `details`. Works only on
  Blueprint-backed skills: C++ and Python skills — all the engine's own — are
  refused with an error (`AgentSkill.cpp:152-162`). It changes the class
  defaults and marks the package dirty; it **does not save**
  (`AgentSkill.cpp:164-168`).
- The editor-toolset skill `unreal_skill_best_practices` is presumably the
  engine's guide to writing one (by its name; its text was not read). Read it
  with `GetSkills` before `CreateSkill`.
