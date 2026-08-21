# Skill subsystem — design

**Date:** 2026-08-21
**Status:** approved for implementation (subsystem + skill classes A and D)

## 1. Context

`unreagent` starts the UE editor and an agent, and hands the agent a set of MCP
servers: its own (`status`, `ue_*`, `logs`, `run_command`, `run_python`,
`run_node`, `read_file`, …) plus the in-editor plugin's. What it does **not**
hand over is any knowledge of *how* to use them. There is currently no
prompt, skill, or instruction path at all — `cmd/launcher/main.go:204-219`
appends `--mcp-config` and `--permission-prompt-tool`, nothing else.

The consequence is the failure mode the UE plugin ecosystem already knows well:
an agent facing 43 editor tools guesses property names and spirals into
discovery loops.

### Prior art surveyed

**UE LLM Toolkit** (`ColtonWilley/ue-llm-toolkit`, the plugin unreagent
targets) ships two document sets: `Resources/mcp-bridge/contexts/*.md` (10 API
references, served by the `get_ue_context` bridge tool) and
`Resources/domains/*.md` (6 operational docs, meant to be `Read` directly).
Neither has frontmatter or trigger descriptions, so nothing makes an agent
reach for them at the right moment.

**VibeUE** (`kevinpbuckley/VibeUE`) ships 34 real skills under
`Content/Skills/<name>/SKILL.md` with YAML frontmatter and sub-documents,
served by its own `manage_skills(action=list|load)` MCP tool.

VibeUE itself was evaluated and **rejected** for this project in June 2026: the
plugin's MCP server validates a vibeue.com API key remotely
(`ValidateVibeUEApiKeyAsync()`, `bIsVibeUEApiKeyValid` in
`Source/VibeUE/Public/MCP/MCPServer.h:162,232`). An account requirement and a
phone-home are disqualifying for unattended operation, which is unreagent's
whole point. That decision stands and is not revisited here.

The VibeUE *repository* is MIT-licensed, however, so its skill **documents**
may be copied and modified with the copyright notice preserved. VibeUE is
therefore a quarry for text, never a runtime dependency.

## 2. Goals

- Ship UE domain knowledge with the launcher so an agent has it without setup.
- Default delivery over MCP, because it works with any agent.
- A switch to turn the MCP path off and instead lay the skills down as plain
  files any agent can consume directly.
- Skill documents that are agent-agnostic: no Claude-Code-specific constructs.

### Non-goals

- Replacing the plugin's own `get_ue_context` / `domains` system. Skills point
  at it where useful.
- Authoring skills for domains that can only be verified against a running
  editor (see §7).
- Any runtime dependency on VibeUE.

## 3. Architecture

One source, two delivery paths:

```
Unreagent repo
  skills/<name>/SKILL.md            ← go:embed into the .exe
        │                    │
        ▼                    ▼
  MCP tool `skills`     materialization
  (default)             <Project>/.claude/skills/unreagent-<name>/
                        (skills.mcp: false)
```

### 3.1 Why the MCP tool needs the list in its description

Native skills work because `name` + `description` of every skill sit
permanently in the agent's system prompt; the selection decision costs no tool
call. An MCP tool only contributes its own description, so an agent must first
think to call `list`, then `load` — two round trips gated on the agent having
the idea at all.

VibeUE demonstrates the gap: it ships a skill
(`Content/Skills/vibeue/SKILL.md`) whose entire content is *"VibeUE exposes its
own skills system via MCP. Use it instead of this file for all tasks."* — a
native skill built solely to advertise the MCP tool.

The fix is to generate the `skills` tool description from the embedded set, so
it carries every skill's name and one-line description. The same selection
information is then resident in context, and one `load` call suffices. This
reduces the practical difference between the two delivery paths to a single
round trip, which is why MCP can be the default.

Two capabilities remain exclusive to materialized skills, neither decisive:
invocation as `/skill-name`, and skills that carry auxiliary files.

## 4. Skill format

Standard `SKILL.md` with YAML frontmatter. Deliberately free of
Claude-Code-specific fields so the same file works when a different agent reads
it off disk:

```yaml
---
name: ue-materials
description: >
  Create and edit materials and material instances. Use when working on
  shaders, material parameters, textures, or material instances in UE.
toolkit_tools: [material, asset_search]
unreal_classes: [MaterialEditingLibrary]
---
```

- `name`, `description` — required. `description` must state *when* to use the
  skill, since it is the whole basis for selection.
- `toolkit_tools` — which of the plugin's 43 MCP tools the skill covers.
  Replaces VibeUE's `vibeue_classes`. Lint-checked against the registry (§8).
- `unreal_classes` — relevant `unreal.*` Python classes, for skills that work
  through `execute_script`.

Sub-documents (`<skill>/<section>.md`) are permitted for long material,
following VibeUE's layout.

Skills adapted from VibeUE carry an MIT attribution line; the repo gets a
collected `NOTICE` entry.

## 5. Configuration

New `skills:` section, consistent with the existing `files:` and `runtimes:`
sections in `internal/config/config.go`:

```yaml
skills:
  enabled: true       # default true
  mcp: true           # default true — serve via the `skills` MCP tool
  materialize: false  # default false — write to <dir> instead
  dir: "${PROJECT_DIR}/.claude/skills"
```

CLI override: `--no-skills-mcp` sets `mcp: false` and `materialize: true` — the
switch requested for users who prefer file-based skills.

`enabled: false` disables both paths. Setting both `mcp` and `materialize` is
legal (belt and braces); setting neither is a config error, reported at load
time rather than silently shipping nothing.

## 6. Component contracts

### 6.1 `internal/skills` — the embedded collection

Owns the embedded FS and is the only component that parses skill files.

- `Load() ([]Skill, error)` — parse all embedded skills once at startup;
  returns an error if any frontmatter is malformed, so a broken skill fails
  loudly at build/test time instead of silently vanishing.
- `Skill{Name, Description, ToolkitTools, UnrealClasses, Body, Files}`.
- `DescribeAll([]Skill) string` — renders the name/description table used as
  the MCP tool description.

Depends on nothing but `embed` and the YAML parser. Testable without a
launcher, an editor, or a network.

### 6.2 The `skills` MCP tool

Registered in `cmd/launcher/main.go` alongside the existing tools, following
the `mcp.Tool` shape in `internal/mcp/mcp.go`.

- `Description` — generated via `DescribeAll`, listing every skill.
- Parameters: `name` (required) — the skill to load; `section` (optional) — a
  sub-document.
- Result: the skill body as text. Unknown name returns an error result naming
  the available skills.

No `list` action: the description already carries the list. An agent that
somehow needs it can call `load` with an unknown name and read the error.

### 6.3 Materialization

Writes each skill to `<dir>/unreagent-<name>/SKILL.md` plus its sub-documents.

The `unreagent-` prefix is load-bearing: it namespaces the launcher's skills so
a project's own skills in the same directory are never touched. On each start,
directories carrying the prefix are replaced and everything else is left alone.
A prefixed directory that is not in the current embedded set is removed, so
renamed or dropped skills do not linger.

## 7. Scope

Skills split by whether they can be written correctly **without a running
editor**. `unreal_status` currently reports `NOT CONNECTED`; writing a skill
for an API nobody can exercise produces documents whose correctness is unknown
— precisely the failure the skills exist to prevent.

### In scope now

**Class A — a dedicated toolkit tool exists**, with operations documented in
its C++ header (e.g. `MCPTool_BlueprintQuery.h:34` declares
`Info.Name = TEXT("blueprint_query")` and enumerates 15 operations). The header
is in-repo and checkable, so these can be written and reviewed offline.

| Skill | Toolkit tools |
|---|---|
| `ue-blueprints` | `blueprint_query`, `blueprint_modify` |
| `ue-materials` | `material` |
| `ue-niagara` | `niagara` |
| `ue-widgets` | `widget_editor` |
| `ue-enhanced-input` | `enhanced_input` |
| `ue-metasounds` | `metasound` |
| `ue-audio` | `audio` |
| `ue-anim-blueprint` | `anim_blueprint_modify`, `blueprint_query` |
| `ue-anim-montage` | `montage_modify` |
| `ue-anim-sequence` | `anim_edit` |
| `ue-blend-space` | `blend_space` |
| `ue-level-actors` | `spawn_actor`, `get_level_actors`, `move_actor`, `delete_actors`, `set_property` |
| `ue-levels` | `open_level`, `level_query` |
| `ue-assets` | `asset`, `asset_search`, `asset_import`, `asset_dependencies`, `asset_referencers` |
| `ue-viewport` | `capture_viewport` |
| `ue-pie-testing` | `gameplay_debug`, `run_console_command`, `get_output_log` |
| `ue-python` | `execute_script`, `cleanup_scripts`, `get_script_history` |
| `ue-tasks` | `task_submit`, `task_status`, `task_result`, `task_list`, `task_cancel` |
| `ue-context` | `get_ue_context` (bridge tool; points at the plugin's own contexts/domains) |

**Class D — unreagent's own tools.** No external source covers these; they are
the skills only this project can supply.

| Skill | Unreagent tools |
|---|---|
| `unreagent-iterate` | `ue_restart`, `logs`, `status` — the edit → compile → restart → read-logs loop |
| `unreagent-build` | `run_command` (compile/package), `logs` |
| `unreagent-files` | `read_file`, `list_dir`, `write_file`, `edit_file` |
| `unreagent-runtimes` | `run_python`, `run_node` — host-side runtimes, explicitly *not* UE Python |

23 skills.

### Deferred

**Class B — reachable only through `execute_script` and raw UE Python**: pcg,
terrain-data, landscape (+ auto-material, materials), foliage, state-trees,
gameplay-tags, data-tables, data-assets, enum-struct, uv-mapping,
project-settings, engine-settings, skeleton — 15 skills. These require a
running editor to verify. Note that VibeUE's `pcg` and `terrain-data` skills
are near-portable as-is (`pcg/SKILL.md` states "No VibeUE service wrapper is
needed — the Python API is complete"), so this class is cheaper than its size
suggests once an editor is available.

**Class C — toolkit-only domains with no VibeUE precedent**: `character`,
`character_data`, `lighting`, `game_framework`, `control_rig`, `sequencer`,
`retarget`. Roughly 7 skills.

> **Open decision.** Class C was deferred alongside B because it has no
> template to adapt. By the criterion actually used to draw the line — a
> dedicated tool with a documented schema in-repo — Class C qualifies for
> offline authoring exactly as Class A does. Folding it into the current batch
> would add ~7 skills without needing an editor. Flagged rather than assumed,
> since the approved scope was A and D.

## 8. Testing

Go tests in the style of `internal/supervisor/supervisor_test.go`:

- every embedded skill parses; `name` and `description` present and non-empty
- `DescribeAll` output contains every skill name
- the `skills` tool returns a known skill, errors informatively on an unknown
  one, and resolves sub-documents
- materialization creates prefixed directories, replaces its own on rerun,
  removes prefixed directories no longer in the set, and leaves unprefixed
  sibling directories untouched
- config: defaults, `--no-skills-mcp` override, and the both-paths-off error

**Skill content lint** (a test, not a separate binary): every tool named in
`toolkit_tools` must exist in the plugin's `MCPToolRegistry.cpp`. This catches
drift when the toolkit submodule is updated. It requires the toolkit checkout
to be present and skips cleanly when it is not, so CI without the submodule
stays green.

## 9. Risks

- **Unverified content.** Class A skills are written against C++ headers, not
  against a running editor. A header can describe an operation whose runtime
  behaviour differs. Mitigation: keep skills to documented operations and
  observed patterns; revisit once an editor is available.
- **Toolkit drift.** Tool names and operations change upstream. Mitigated by
  the lint in §8, which fails loudly rather than leaving skills quietly wrong.
- **Binary size.** 23 skills of prose is a modest addition to the `.exe`;
  monitor rather than pre-optimize.
- **Writing into the project directory.** Materialization writes under
  `<Project>/.claude/`. The prefix rule keeps it from touching anything it does
  not own, and the path is configurable.

## 10. Note on an unrelated finding

The existing MCP tool descriptions in `cmd/launcher/main.go` are still German
("Liest eine Textdatei aus dem UE-Projekt…"), which contradicts the language
rule in `CLAUDE.md`; commit 4b3ff55 evidently missed them. Out of scope here,
recorded so it is not lost.
