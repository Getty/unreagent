# unreagent setup: converting an existing project

The project already has an `unreagent.yaml`, a `CLAUDE.md`, or a `.claude/`
with its own skills, agents or settings. The path in the main skill still
applies. This section says what differs: what the launcher leaves alone, the
one place where it deletes, and what you propose to the user instead of
writing it.

None of this was run on Windows or in a converted project: it is read in the
launcher's code.

## Before anything is written

That includes adding the toolkit (step 3 of the main skill), which is why
this section is loaded at step 1.

1. **Make the tree recoverable.** Under source control, ask the user to
   commit or stash what is open, so that the diff afterwards shows exactly
   what was added. Without source control, copy `.claude/`, `CLAUDE.md`,
   `unreagent.yaml` and `unreagent.local.yaml` to a place outside the project
   and tell the user where.
2. **Run `unreagent init -check` and read every `[WARN]`.** It writes nothing.
3. **Look into `.claude/skills/` for directories named `unreagent-*` that are
   the project's own.** This is the one place where a conversion loses files.

## The `unreagent-` prefix belongs to the launcher

With `skills.materialize` on, every start replaces each `unreagent-*`
directory in `skills.dir` that is named like a shipped skill, and deletes
every other one. A hand-edited file inside one is lost. Nothing else in
`.claude/skills/` is touched.

`init -check` names what it finds in a WARN that starts
`skills.dir <dir> holds unreagent-* directories`, each directory with
`(replaced)` or `(deleted)`. What `init` then does depends on whether the
launcher has written into this `.claude/` before:

| The WARN says | `init` | The next start |
|---|---|---|
| `unreagent cannot tell are its own` | leaves them and writes no skills, agents or rules file. It still writes `unreagent.yaml` with `skills.materialize` on, if there is none | replaces and deletes them |
| `not named like a shipped skill`, and `an earlier materialization there` | deletes them, after a line that starts `Deleting from <dir>`; its own WARN then reads `skills.dir <dir> held … so init deleted them`, with the fix to get them back from version control or a backup | deletes them |

These directories may be the user's own skills. Do not move, rename or edit
them on your own, and do not run `unreagent init` while the WARN is there and
the user has not decided. Make it a proposal:

1. **List every directory the WARN named**, each with what happens to it at
   the next launcher start if nothing is done: `(replaced)` means its content
   is overwritten with the shipped skill of that name, `(deleted)` means it
   is removed. In the second row of the table, `init` itself deletes them.
2. **Say which ones you take to be the launcher's own copies** from an older
   unreagent, and why (the content is a shipped skill's). For those there is
   nothing to keep. The user confirms; when you cannot tell, ask.
3. **For each of the others, propose one of two ways out, with the new name
   or place written out:**
   - a rename without the prefix, inside `skills.dir`: for example
     `unreagent-levels` to `project-levels`, with `name:` in its `SKILL.md`
     frontmatter set to the new directory name
   - a move out of `skills.dir`, to a directory you name. The skill is then
     no longer loaded from there.
4. **Say what a rename breaks.** The skill's name changes for everything
   that refers to it: the `skills:` lists of the project's agent files,
   `CLAUDE.md`, other skills. Search the project for the old name, show the
   user each place, and propose the edit there too.
5. **Act only after the user agrees**, and only on what they agreed to. Then
   run `init -check` again: the WARN is gone, or names only what the user
   chose to let the launcher replace or delete.

All of this happens before `init` and before the next launcher start. If the
user wants time to decide, stop here and tell them that a launcher start in
the meantime replaces and deletes the directories.

With `skills.materialize` off, the WARN reads
`once skills.materialize is on, the first start ...`: the same rule, not in
force yet.

## What `init` leaves alone

| There already | `init` | You |
|---|---|---|
| `unreagent.yaml` or `unreagent.yml` | never edits it. It says `exists and is left as it is` and prints, as fix lines, what it would add | propose each addition to the user, write it after they agree |
| `CLAUDE.md` or `.claude/CLAUDE.md` | never writes to it: no starter, no kickstart block | propose additions, below |
| the project's own skills, under names without the prefix | not touched | nothing |
| the project's own agents in `.claude/agents/` | not touched, whatever their names | nothing; advise names without the `unreagent-` prefix |
| a file at the name of a shipped agent (`.claude/agents/unreagent-<name>.md`) or at `.claude/rules/unreagent.md` that the launcher did not write, or that was edited since | left alone, named in `agents: in the way and left alone, not written by unreagent or edited since: ...` | ask the user: delete it to have the launcher's version written, or keep theirs under another name than `unreagent-*` |
| `.claude/settings.json` and everything else in `.claude/` | not touched | nothing |

## The existing config

Read the fix line of each finding; it names the file to change.

- **`skills: not written into the project — skills.materialize is off`.**
  Without it the launcher writes no skills, no topic agents and no rules
  file, and the agent has the `skills` tool alone. Turning it on is the
  project's choice and goes into `unreagent.yaml`, not into
  `unreagent.local.yaml`. The fix says which of the two files sets the value
  now and what to change there. Ask the user before you turn it on, and
  settle their `unreagent-*` directories first, as above.
- **`UE LLM Toolkit: the plugin is there (...) but the config has no
  mcp.extraServers.ue-llm-toolkit entry`.** The fix prints the document with
  the entry. Propose only the entry, for the existing `mcp:` section, and add
  it after the user agrees; do not replace the file with the printed
  document.
- **`agents: not written — skills.dir <dir> is not <project>/.claude/skills`.**
  The topic agents and the rules file are written only next to that one
  place. With another `skills.dir` the project gets the skills alone.
- **`agents: off (skills.agents is false)`.** The project chose to have no
  topic agents. Leave it.
- **`agent files: CLAUDE.md, the topic agents and the rules file are read by
  Claude Code only`.** The config runs an agent that is not Claude Code, or
  `agent.claudeIntegration` is false. Those files do nothing for that agent.
- **A CLAUDE.md finding that ends with `agent.workdir is <dir>, so the agent's
  working directory is elsewhere`.** The agent may never read the `CLAUDE.md`
  in the project directory. Tell the user; which directory is right is theirs
  to say.

## The existing CLAUDE.md

The launcher never rewrites it, so what a fresh project gets through the
starter reaches this one through you. Load section `interview`, then:

1. Read the file. Find which of these it already answers: what the project
   is, target platforms, what is C++ and what is Blueprint, source control
   and large binary assets, conventions.
2. Ask the user only about what is missing.
3. Propose the additions as text, say where in the file they go, and write
   after the user agrees. Keep the file's own structure: add what is missing,
   do not reorder it into the starter's sections.
4. **Do not copy the launcher's rules or the agent table into it.** They are
   in `.claude/rules/unreagent.md`, which the launcher keeps current. A copy
   in `CLAUDE.md` goes stale at the next unreagent update.
5. If the file gives instructions that contradict `.claude/rules/unreagent.md`
   (starting or killing the editor by hand, another way to build), or names
   agents of its own for a topic a shipped agent covers, show the user both
   and let them decide. Do not resolve it yourself.

## Check afterwards

- `unreagent init -check`: no `[WARN]` that you and the user did not decide to
  accept, and nothing `[BLOCKING]` except the port line of a running launcher.
- The diff, or a comparison with the copy: only new files, apart from the
  edits the user agreed to. New are `.claude/skills/unreagent-*/`,
  `.claude/agents/unreagent-*.md`, `.claude/rules/unreagent.md`, and
  `unreagent.yaml` or `CLAUDE.md` where there was none.
- The project's own skills, agents and settings: still there, unchanged.
- Tell the user that the launcher rewrites its own files at every start, so
  whether they are committed or ignored is a source-control decision for
  them.
