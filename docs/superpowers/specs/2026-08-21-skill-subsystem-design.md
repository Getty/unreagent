# Skill subsystem — design

**Date:** 2026-08-21
**Status:** approved for implementation (subsystem + skill classes A and D)

## 1. Context

`unreagent` starts the UE editor and an agent, and hands the agent a set of MCP
servers: its own (`status`, `ue_*`, `logs`, `run_command`, `run_python`,
`run_node`, `read_file`, …) plus the in-editor plugin's. What it does **not**
hand over is any knowledge of *how* to use them. There is currently no
prompt, skill, or instruction path at all — `run()` in `cmd/launcher/main.go`
appends only MCP and permission flags (`--mcp-config`, `--strict-mcp-config`,
`--permission-prompt-tool` or `--dangerously-skip-permissions`); nothing
reaches the agent as a prompt, skill or instruction.

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
                        (skills.materialize: true)
```

**Decided (k10, k12):** the target directory is not fixed to
`<Project>/.claude/skills/`: it is `skills.dir` (§5), default
`${PROJECT_DIR}/.claude/skills`, so a project can point it elsewhere, for
example at the `<Project>/Skills/` directory UE 5.8 reads. The directory
name rule is in §6.3.

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
information is then resident in context, and one `skills` call suffices. This
reduces the practical difference between the two delivery paths to a single
round trip, which is why MCP can be the default.

One capability remains exclusive to materialized skills, and it is not
decisive: invocation as `/skill-name`.

Auxiliary files (a script, an image) are not part of the format today. The
embed pattern is `*/*.md` (§4.1), so such a file, a nested directory, or a
`skills/README.md` does not ship, and `TestEmbedMissesNoSourceFile` fails on
purpose rather than let it vanish quietly; `Load` itself never sees the file.
This is the current decision and can be revisited together with
materialization (k12).

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
  Replaces VibeUE's `vibeue_classes`. To be lint-checked against the registry
  (§8, planned: k18).
- `unreal_classes` — relevant `unreal.*` Python classes, for skills that work
  through `execute_script`.

Sub-documents (`<skill>/<section>.md`) are permitted for long material,
following VibeUE's layout. By convention they are plain Markdown without
frontmatter; the loader does not check that. The section name is the file name
without `.md`.

### 4.1 Parser rules

`internal/skills` enforces the following; each violation is an error that
names what is wrong: the skill directory where there is one, the file where
there is one (a stray top-level file has no skill, a bad skill name no file).

- The frontmatter `name` equals the skill's directory name, so names stay
  unique and the source of a skill is obvious.
- The key set is closed: `name`, `description`, `toolkit_tools` and
  `unreal_classes`. An unknown key is rejected, so a misspelt `toolkit_tools`
  cannot slip past the registry lint planned in §8 (k18). `name` and
  `description` are required; the description is collapsed to one line.
- The frontmatter is exactly one YAML document. A bare `---` line always
  closes the frontmatter and the body follows. It is an error when a `...`
  line is followed by content, or when a line starts with `---` plus a space
  and has content on the same line (for example `--- {bogus: 1}`).
- The body, the Markdown after the frontmatter, must not be empty.
- Only `<name>/SKILL.md` and `<name>/<section>.md` are loaded. The embed
  pattern is `*/*.md`, so a non-`.md` file or a nested directory inside a
  skill, or any file directly under `skills/` (a `skills/README.md`), is left
  out of the binary and `Load` never sees it; `TestEmbedMissesNoSourceFile`
  fails instead. Fed such a layout directly (`load(fs.FS)`), the loader
  rejects it as well. Every other `.md` next to `SKILL.md` ships as a
  section: a `README.md` there becomes section `README`.
- A skill or section name starting with `.` or `_` is rejected. The explicit
  embed pattern matches such names, which Go would leave out of a
  whole-directory embed as hidden or draft files, so the loader refuses them
  instead of shipping a draft.
- An empty section name and an empty sub-document are rejected.
- A byte-order mark, or trailing whitespace on a `---` line, is an error that
  names the problem. It is deliberately not tolerated: once materialized, other
  parsers read the same file, so what ships must be the plain form.
- CRLF line endings are tolerated; `.gitattributes` forces LF for `*.md`
  anyway.
- A malformed skill stops `Load`. It is never skipped, because a skill that
  silently vanished would leave the agent without the knowledge it was meant
  to have.

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
  dir: "${PROJECT_DIR}/.claude/skills"   # default; takes the path placeholders
```

CLI override: `--no-skills-mcp` sets `mcp: false` and `materialize: true` — the
switch requested for users who prefer file-based skills.

`enabled: false` disables both paths. Setting both `mcp` and `materialize` is
legal (belt and braces); setting neither is a config error, reported at load
time rather than silently shipping nothing.

**Decided (k10):** `--no-skills-mcp` is applied after both YAML files are
loaded, so it wins over `unreagent.yaml` and `unreagent.local.yaml`; with
`enabled: false` it has no effect and logs that. An unused `dir` with an
unresolved placeholder is cleared rather than rejected (as `files.root`), so
the flag then finds no directory and warns. The Go flag is spelled
`-no-skills-mcp`.

## 6. Component contracts

### 6.1 `internal/skills` — the embedded collection

The collection is split over two packages, because a `go:embed` pattern
cannot reach outside its package directory and the documents live in `skills/`
at the repository root:

- `skills` (`skills/embed.go`) holds only the `//go:embed */*.md` directive and
  the resulting `FS`. The pattern matches Markdown files exactly one directory
  deep, so the Go file, top-level files and nested directories stay out of the
  binary.
- `internal/skills` is its only importer and the only component that parses
  skill files (§4.1).

Its API:

- `Load() ([]Skill, error)` — parse all embedded skills (called once at
  startup by the launcher's `setupSkills`, for the `skills` tool and for
  materialization), in directory-name order; returns an error if any skill
  breaks a rule in §4.1, so a broken skill fails loudly in
  `go test ./internal/skills` and at startup instead of silently
  vanishing. `load(fs.FS)` is the same over any filesystem, which is
  how the tests feed it malformed skills.
- `Skill{Name, Description, ToolkitTools, UnrealClasses, Body, Source, Files}` —
  `Files` maps section name to sub-document and is nil when there are none.
  **Decided (k12):** `Source` is `SKILL.md` byte for byte as embedded,
  frontmatter included; materialization starts from it instead of rebuilding
  the file from `Body`. When materialization adds the `unreagent-` prefix to
  the directory (`ue-x` becomes `unreagent-ue-x/`), the frontmatter `name:` of
  the written `SKILL.md` is set to the directory name, so name and directory
  match as the Agent Skills spec requires; nothing else in the file changes. A
  skill already named `unreagent-*` is written byte for byte. The `skills` MCP
  tool keeps the shipped names. Rejected: renaming every shipped skill to
  `unreagent-*`, which would churn every Class A skill and make the MCP names
  long.
- `DescribeAll([]Skill) string` — renders the selection list that becomes the
  `skills` MCP tool description (after its preamble): one line per
  skill, `- <name>: <description>`, in the order given.

Depends on nothing but the embedded FS and the YAML parser. Testable without a
launcher, an editor, or a network.

### 6.2 The `skills` MCP tool

Registered in `registerTools` (`cmd/launcher/tools.go`) alongside the existing tools, following
the `mcp.Tool` shape in `internal/mcp/mcp.go`.

- `Description` — a short preamble followed by `DescribeAll`, which lists
  every skill.
- Parameters: `name` (required) — the skill to load; `section` (optional) — a
  sub-document, with or without `.md`.
- Result: the skill body as text, or the sub-document when `section` is given.
  An unknown name returns an error result naming the available skills; an
  unknown section returns one naming the skill's sections.
- Registered only when `skills.enabled` and `skills.mcp`.

**Decided (k11):** the tool is named `skills`, not `load` as an earlier draft
of this section had it. No `list` action: the description already carries the
list. An agent that somehow needs it can call `skills` with an unknown name and
read the error.

### 6.3 Materialization

Writes each skill to `<dir>/<directory name>/SKILL.md` plus its sub-documents,
where `<dir>` is `skills.dir`.

The `unreagent-` prefix is load-bearing: it namespaces the launcher's skills so
a project's own skills in the same directory are never touched. On each start,
directories carrying the prefix are replaced and everything else is left alone.
A prefixed directory that is not in the current embedded set is removed, so
renamed or dropped skills do not linger.

**Decided (k12):**

- The directory name is the skill name when it already starts with
  `unreagent-` (the Class D skills), otherwise `unreagent-<name>` — never a
  doubled prefix. "Starts with `unreagent-`" is the one predicate for "ours".
- Two skills that map to the same directory (`ue-x` and `unreagent-ue-x`), or
  whose directory names (or a skill's file names) differ only in case, are a
  load error on every OS; the start stops.
- Only real directories named `unreagent-*` count as ours; a file with such a
  name is left alone.
- A relative `skills.dir` is resolved against the agent working directory.
- A section file named like a Windows device (`con`, `aux`, `nul`, `com1`,
  `conin$`, `conout$`, superscript digits; with any extension, so
  `con.backup` too) is a load error on every OS; the start stops. Windows 10
  opens such a name as the device, not as a file.
- A failed write only logs a warning; the editor and agent still start.
- Each skill is written whole, never in place: into a work directory
  `unreagent-.tmp-*` inside `<dir>` (new copy one level down, at
  `<work>/<name>`), then the previous copy, if a real directory, is renamed to
  `<work>/previous`, then the new copy is renamed to `<dir>/<name>`. Nothing is
  renamed onto an existing directory (Windows cannot, Linux only onto an empty
  one). A file or symlink in the way is refused at once. A work directory that
  cannot be created (`errNoWorkDir`, e.g. a read-only `skills.dir`) ends the
  run at once: no retry, no further skills, one cause ending in `(N of M
  skills not written)`; every skill in place is spared by the sweep. Any other
  failed attempt is retried from a fresh work directory, up to three attempts
  in all, with pauses of 50 ms and 200 ms between them (`skillRetryPauses`).
  When the rename-in fails, `<work>/previous` is renamed back into place, so
  the old copy stays. If that move back fails too, it simply tries again when
  the old copy went with a peer's sweep or a peer's copy is now in place;
  otherwise the old copy stays whole in `<work>/previous`, attempts for that
  skill stop, this run's sweep spares that work directory, and a later start
  removes it (a launcher starting at the same moment can remove it sooner,
  since its sweep does not know the copy is kept). The cause names it: `...,
  and moving the previous copy back failed too (rename <work>/previous
  <dir>/<name>: <err>): it is kept in <work>/previous until the next start
  removes it`. The sweep of dropped skills renames each to a fresh
  `unreagent-.tmp-*` name first and removes it after, so it cannot empty a
  peer's in-flight copy. Apart from `errNoWorkDir`, a skill that fails does
  not stop the others; no failure stops the sweep. Every error is returned,
  joined, and logged on the one warning line, separated by `; `. A crash
  leaves only `unreagent-*` names, and every skill directory in place is a
  whole copy; the next start removes the rest.
  Two launchers started at once end with the whole set. Accepted gap: two
  launchers of different versions (different sets) on one project can still
  remove each other's extra skills; that needs a lock.
- `SKILL.md` is written from `Skill.Source` (§6.1): byte for byte for a skill
  already named `unreagent-*`; otherwise with only the frontmatter `name:`
  value set to the directory name. Only a plain or quoted single-line `name:`
  value can be rewritten. Anything else makes materialization refuse the whole
  set before writing anything, for example a tag, an anchor, an alias, an
  escape, a folded or block value, or a value over several lines.

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

Go tests in the style of `internal/supervisor/supervisor_test.go`.

Written, in `internal/skills/skills_test.go`:

- every embedded skill parses; `name` and `description` present and non-empty
- `DescribeAll` output contains every skill name and has the `- <name>:
  <description>` format
- the embed holds only `<skill>/<doc>.md` and misses no source file under
  `skills/`; `Load` rejects each malformed case in §4.1

Planned, with the card that brings each:

- the `skills` tool returns a known skill, errors informatively on an unknown
  one, and resolves sub-documents (k16, with the tool, k11)
- materialization creates prefixed directories, replaces its own on rerun,
  removes prefixed directories no longer in the set, and leaves unprefixed
  sibling directories untouched (k17, with materialization, k12)
- config: defaults, `--no-skills-mcp` override, and the both-paths-off error
  (k10, k15)

**Skill content lint** (planned, k18; a test, not a separate binary): every
tool named in `toolkit_tools` must exist in the plugin's `MCPToolRegistry.cpp`.
This catches drift when the toolkit submodule is updated. It requires the
toolkit checkout to be present and skips cleanly when it is not, so CI without
the submodule stays green.

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

## 10. Note on an unrelated finding (resolved)

When this spec was written, the MCP tool descriptions were still German,
contradicting the language rule in `CLAUDE.md`. They are English now: the
descriptions in `cmd/launcher/tools.go` and `cmd/launcher/filetools.go` carry no
German text. Nothing is left to do here.
