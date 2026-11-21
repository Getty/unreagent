---
name: unreagent-skill-authoring
description: >
  How to author the UE domain skills that unreagent embeds and ships to agents —
  the SKILL.md format and the loader rules it must pass, the two delivery paths
  (MCP tool and materialization), which sources are authoritative, the
  toolkit-registry lint, and the licensing rule for text adapted from VibeUE.
  Use when writing or reviewing anything under skills/ or internal/skills/ in
  this repository, the delivery code in cmd/launcher/skills.go, or when go test
  ./internal/skills fails.
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
`*/*.md`) and parsed by `internal/skills` (`Load`), which only parses.
Delivery is wiring, in `cmd/launcher/skills.go`: `setupSkills` runs in `run()`
before the agent starts, loads the set and hands it to whichever paths the
`skills:` config section turns on (skill `unreagent-config`; contract: design
spec §5, §6.2, §6.3):

- **The `skills` MCP tool** — the default. `name` returns the skill's `Body`
  (frontmatter stripped), `section` one sub-document. Registered only when
  `skills.enabled` and `skills.mcp`, and only while `mcp.enabled`.
- **Materialization** — `skills.materialize: true`, or `-no-skills-mcp`. Every
  start writes the set into `skills.dir` (default
  `${PROJECT_DIR}/.claude/skills`).

What binds the author:

- **The `skills` tool description is generated from the whole set**
  (`skillsToolPreamble` plus `DescribeAll`: one `- <name>: <description>` line
  per skill, the description's whitespace collapsed to single spaces), so every
  `description` is permanently in the agent's context. It must state *when* to
  reach for the skill, in one or two lines, because that text is the entire
  basis for selection. Long descriptions tax every request.
- **What you commit is what lands on disk, bar one value.** Materialization
  writes `Skill.Source` — `SKILL.md` as embedded, frontmatter included — and
  each sub-document verbatim as a sibling `<section>.md`. Nothing is rebuilt
  from the parsed fields, which is why the delimiter rules below insist on the
  plain form: other agents parse that file. The one change: when the directory
  gains the `unreagent-` prefix (next point), the `name:` value is set to the
  directory name (`ue-tasks` → `unreagent-ue-tasks`), as the Agent Skills
  format requires; every other byte stays. A skill already named `unreagent-*`
  is written byte for byte (`materializedSource`). The `skills` tool keeps the
  shipped names.
- **The directory is `<name>` if the name already starts with `unreagent-`,
  else `unreagent-<name>`** (`skillDirName`) — never a doubled prefix. That
  prefix is the one predicate for "ours". On each start every skill is written
  into an `unreagent-.tmp-*` work directory and renamed into place
  (`installSkillDir`), then every other real directory carrying the prefix is
  removed (`sweepSkillDirs`), so a dropped or renamed skill does not linger,
  and nothing else there is touched.
- **`name:` is a plain or quoted value on one line** — `ue-tasks`,
  `'ue-tasks'` or `"ue-tasks"`, a comment after it is fine. Any other form
  cannot be rewritten in place — for example a tag, an anchor, an alias, an
  escape, a folded or block scalar (`>`, `|`), a value over several lines
  (`materializedSource`). `Load` accepts it, but materialization refuses the
  whole set before writing
  anything (WARN, the start goes on), and `go test ./cmd/launcher` fails over
  the shipped set (`TestMaterializeWritesEverySkill`). Only names that gain
  the prefix are checked; write every skill this way.
- **Refer to other skills by the shipped name** — `ue-tasks`, never
  `unreagent-ue-tasks`. It is the name the `skills` tool takes; on disk the
  same skill is `unreagent-ue-tasks/`, and the body is not rewritten.
- **Collisions ignore case and stop the start**, on every OS and whichever path
  is on: `ue-x` next to `unreagent-ue-x` (one directory), two skill names or
  two sections of one skill that differ only in case, a section named `skill`
  (it meets `SKILL.md`). `checkSkillDirs` raises them, not `Load`, so
  `go test ./internal/skills` stays green; `go test ./cmd/launcher`
  (`TestCheckSkillDirsRejectsCollisions`, over the shipped set) fails.
- **No section named after a Windows device** — `con`, `prn`, `aux`, `nul`,
  `conin$`, `conout$`, `com` or `lpt` with `1`–`9` or `¹`–`³`, any case, also
  with a further extension (`con.backup`); `reservedDeviceName`
  (`cmd/launcher/filetools.go`) is the list. Windows 10 opens `aux.md` as the
  device. `checkSkillDirs` refuses it like a collision: a load error that stops
  the start, on every OS and whichever path is on. The `unreagent-` prefix
  keeps directory names clear of this.

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

- `name` — required; equals the skill's directory name, and sets the
  materialized one; plain or quoted, on one line (Delivery).
- `description` — required, non-blank.
- `toolkit_tools` — a list of the plugin MCP tools this skill covers. To be
  lint-checked (see the lint below).
- `unreal_classes` — a list of relevant `unreal.*` Python classes, for skills
  that work through `execute_script`.
- Long material goes into sub-documents `<skill>/<section>.md`; the section
  name is the file name without `.md`. The `skills` tool serves one through
  `section` (with or without `.md`; an unknown one is an error listing the
  skill's sections). The tool description lists skills, not sections, so the
  body names each section and when to load it.

### What the loader rejects

`internal/skills/skills.go` is the authority; this is the checklist. Run
`go test ./internal/skills ./cmd/launcher` after every change under `skills/`:
`TestLoadParsesEveryEmbeddedSkill` loads the real set,
`TestEmbedMissesNoSourceFile` catches a file that would not ship, and the
launcher's tests serve and materialize the shipped set.

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
the whole set down, by design, and at startup it stops the launcher (the error
names `skills.enabled: false` as the way out).

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
stops the load; `DescribeAll` lists every skill in its pinned format; `Source`
is the embedded file byte for byte. In `cmd/launcher/skills_test.go`: the
tool's body, section and unknown-name results and its registration gate; the
directory rule; replacing the launcher's own directories, removing dropped
ones and leaving foreign entries alone, also with a second run at once or a
crash at any step; collisions over the shipped set; device-named sections.

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
