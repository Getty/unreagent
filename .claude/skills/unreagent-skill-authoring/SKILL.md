---
name: unreagent-skill-authoring
description: >
  How to author the UE domain skills that unreagent embeds and ships to agents —
  the SKILL.md format, the two delivery paths (MCP tool and materialization),
  which sources are authoritative, the toolkit-registry lint, and the licensing
  rule for text adapted from VibeUE. Use when writing or reviewing anything
  under skills/ in this repository.
---

# unreagent — authoring shipped skills

Design of record: `docs/superpowers/specs/2026-08-21-skill-subsystem-design.md`.
Read it before the first skill; this file is the working discipline, not the
design.

The problem being solved: an agent handed 43 editor tools with no instructions
guesses property names and spirals into discovery loops. The launcher therefore
ships UE domain knowledge with the binary so it is present without setup.

## Delivery — one source, two paths

`skills/<name>/SKILL.md` is `go:embed`-ed into the `.exe` and reaches the agent
either through the `skills` MCP tool (default) or by being materialized to
`<Project>/.claude/skills/unreagent-<name>/` when `skills.mcp: false`.

Two consequences bind the author:

- **The `skills` tool description is generated from the whole set** (`DescribeAll`),
  so every `description` is permanently in the agent's context. It must state
  *when* to reach for the skill, in one or two lines, because that text is the
  entire basis for selection. Long descriptions tax every request.
- **The `unreagent-` prefix on materialized directories is load-bearing.** It is
  what lets the launcher replace its own skills on each start and remove ones no
  longer embedded, while never touching a project's own skills in the same
  directory.

## Format

Standard `SKILL.md` with YAML frontmatter, deliberately free of
Claude-Code-specific constructs so a different agent can read the same file off
disk:

```yaml
---
name: ue-materials
description: >
  Create and edit materials and material instances. Use when working on shaders,
  material parameters, textures, or material instances in UE.
toolkit_tools: [material, asset_search]
unreal_classes: [MaterialEditingLibrary]
---
```

- `name`, `description` — required, non-empty.
- `toolkit_tools` — the plugin MCP tools this skill covers. Lint-checked.
- `unreal_classes` — relevant `unreal.*` Python classes, for skills that work
  through `execute_script`.
- Long material goes into sub-documents `<skill>/<section>.md`, reachable via
  the tool's `section` parameter.

## Authoritative sources, in order

1. **The toolkit's C++ headers** — `../ue-llm-toolkit/Plugin/UELLMToolkit/Source/
   UELLMToolkit/Private/MCP/Tools/MCPTool_*.h`. Each declares its `Info.Name` and
   enumerates its operations. This is the only in-repo, checkable source of truth
   for tool schemas, and it is why Class A skills can be written offline at all.
2. **`MCPToolRegistry.cpp`** — the definitive list of registered tool names.
3. **The plugin's own docs** — `Resources/mcp-bridge/contexts/*.md` (served by
   `get_ue_context`) and `Resources/domains/*.md`. Useful background; skills
   should *point at* them rather than duplicate them.
4. **VibeUE** (`../VibeUE/Content/Skills/<name>/SKILL.md`) — a quarry for text
   only, never a runtime dependency. See the licensing rule below.

Set the toolkit location via `UE_LLM_TOOLKIT` when the checkout is not the
sibling `../ue-llm-toolkit`.

## The line you may not cross

Write skills only for what can be verified **without a running editor**. A skill
documenting an API nobody has exercised produces text of unknown correctness —
exactly the failure the skills exist to prevent. In practice: a dedicated
toolkit tool with a documented schema in-repo (Class A) and unreagent's own
tools (Class D) are in scope. Domains reachable only through raw UE Python
(Class B) wait for an editor.

When a header describes an operation whose runtime behavior is uncertain, state
the documented contract and stop — do not invent examples that look tested.

## The lint

A Go test (not a separate binary) asserts that every name in `toolkit_tools`
exists in `MCPToolRegistry.cpp`. It requires the toolkit checkout and **skips
cleanly when it is absent**, so CI without it stays green. This is the drift
alarm for upstream tool renames; when it fires, the skill is wrong, not the
lint.

Also covered by tests: every embedded skill parses with non-empty `name` and
`description`, `DescribeAll` lists all of them, the tool resolves sub-documents
and errors informatively on an unknown name, and materialization replaces its
own prefixed directories while leaving foreign ones alone.

## Licensing

The VibeUE repository is MIT. Text adapted from it keeps the copyright notice:
an MIT attribution line in the skill and an entry in the repo's collected
`NOTICE`. VibeUE was evaluated and rejected as a runtime dependency in June 2026
(its MCP server validates an API key against vibeue.com — an account
requirement and a phone-home are disqualifying for unattended operation). That
decision is settled and is not revisited when borrowing text.

## Language

English, like everything else that ships. These documents are read by agents in
other people's projects.
