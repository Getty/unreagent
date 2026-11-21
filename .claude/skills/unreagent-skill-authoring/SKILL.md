---
name: unreagent-skill-authoring
description: >
  How to author the UE domain skills that unreagent embeds and ships to agents —
  the SKILL.md format and the loader rules it must pass, the two delivery paths
  (MCP tool and materialization), which sources are authoritative, the
  toolkit-registry lint, and the licensing rule for text adapted from VibeUE.
  Use when writing or reviewing anything under skills/ in this repository, or
  when go test ./internal/skills fails.
---

# unreagent — authoring shipped skills

Design of record: `docs/superpowers/specs/2026-08-21-skill-subsystem-design.md`.
Read it before the first skill; this file is the working discipline, not the
design.

The problem being solved: an agent handed 43 editor tools with no instructions
guesses property names and spirals into discovery loops. The launcher therefore
ships UE domain knowledge with the binary so it is present without setup.

## Delivery — one source, two paths

`skills/<name>/SKILL.md` is `go:embed`-ed by `skills/embed.go` (pattern
`*/*.md`) and parsed by `internal/skills` (`Load`). It is to reach the agent
through the `skills` MCP tool (default), by being materialized to
`<Project>/.claude/skills/unreagent-<name>/`, or both. The switches belong to
a `skills:` config section that is planned (k10) and does not exist yet:
`skills.materialize: true` turns materialization on, `--no-skills-mcp` sets
`mcp: false` and `materialize: true`, and `mcp: false` on its own is a config
error, because `materialize` defaults to false and no path would be left.
Neither path is wired yet either: `cmd/launcher` does not import
`internal/skills` until the tool (k11) and materialization (k12) land, so
today the tests are the only consumer.

Two consequences bind the author:

- **The `skills` tool description is generated from the whole set**
  (`DescribeAll`: one `- <name>: <description>` line per skill, the
  description's whitespace collapsed to single spaces), so every `description`
  is permanently in the agent's context. It must state *when* to reach for the
  skill, in one or two lines, because that text is the entire basis for
  selection. Long descriptions tax every request.
- **The `unreagent-` prefix on materialized directories is load-bearing.** It is
  what lets the launcher replace its own skills on each start and remove ones no
  longer embedded, while never touching a project's own skills in the same
  directory. How it combines with names that already start with `unreagent-`
  (the shipped Class D skills) is open — k12; the loader puts no prefix rule on
  names.

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

- `name` — required; equals the skill's directory name.
- `description` — required, non-blank.
- `toolkit_tools` — a list of the plugin MCP tools this skill covers. To be
  lint-checked (see the lint below).
- `unreal_classes` — a list of relevant `unreal.*` Python classes, for skills
  that work through `execute_script`.
- Long material goes into sub-documents `<skill>/<section>.md`; the section
  name is the file name without `.md`, and the `skills` tool is to serve it
  through its `section` parameter (k11).

### What the loader rejects

`internal/skills/skills.go` is the authority; this is the checklist. Run
`go test ./internal/skills` after every change under `skills/`:
`TestLoadParsesEveryEmbeddedSkill` loads the real set, and
`TestEmbedMissesNoSourceFile` catches a file that would not ship.

- **Layout.** A skill directory must hold `SKILL.md`, and besides it only
  `<section>.md` files. The embed pattern is `*/*.md`, so a sub-directory, a
  non-`.md` file in a skill, or any file directly under `skills/` does not
  ship — a `skills/README.md` fails `TestEmbedMissesNoSourceFile` on purpose.
  Every other `.md` next to `SKILL.md` ships as a section: a `README.md` there
  becomes section `README`.
- **Names.** No skill or section name may be empty or start with `.` or `_`.
  Go marks those as hidden or draft files, but the explicit pattern embeds them
  anyway, so the loader refuses them rather than ship a draft. A file named just
  `.md` is an empty section name.
- **Delimiters.** The file's first line is exactly `---`, and so is the line
  that closes the frontmatter (CRLF is fine). No byte-order mark, no trailing
  blank or tab on either `---`: other parsers read the same file once it is
  materialized.
- **One YAML document.** A line of exactly `---` always closes the
  frontmatter. Two things inside it are errors, because what follows would
  escape the key check: a line starting with `--- ` that has content on the
  same line, such as `--- {bogus: 1}` (it opens a second YAML document), and
  content after a `...` line (it ends the first).
- **Closed key set.** The four keys above and no others — a misspelt
  `toolkit_tool` is an error, not an ignored key. `toolkit_tools` and
  `unreal_classes` must be YAML lists, not scalars.
- **Content.** The body after the frontmatter and every sub-document must hold
  more than whitespace.

A malformed skill stops `Load`: it returns no skills at all, only an error
naming the skill and the problem. Nothing is skipped — one broken file takes
the whole set down, by design.

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

Not written yet (k18): a Go test (not a separate binary) is to assert that
every name in `toolkit_tools` exists in `MCPToolRegistry.cpp`. It will require
the toolkit checkout and **skip cleanly when it is absent**, so CI without it
stays green. This is the drift alarm for upstream tool renames; when it fires,
the skill is wrong, not the lint. Until it lands, check every `toolkit_tools`
entry against the registry by hand.

Covered today, in `internal/skills/skills_test.go`: every embedded skill parses
and comes back with its directory name, a description and a body; the embed
holds only `<skill>/<doc>.md` and misses no source file; every rejection above
stops the load; `DescribeAll` lists every skill in its pinned format. Section
lookup and the unknown-name error come with the `skills` tool (k11, tests k16);
replacing the launcher's own directories while leaving foreign ones alone
comes with materialization (k12, tests k17).

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
