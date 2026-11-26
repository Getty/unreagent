# unreagent setup: the questions, and writing the answers down

The starter `CLAUDE.md` that `unreagent init` writes has five sections under
`# Project`, each holding `Not described yet.`:

| Section | Holds |
|---|---|
| `## What it is` | genre, scope, who works on it |
| `## Target platforms` | where the game ships |
| `## C++ and Blueprint` | what is written in which |
| `## Source control` | the system, and how large binary assets are handled |
| `## Conventions` | everything else the user wants every session to follow |

Claude Code loads the file into the sessions of this project, so write short
statements of fact, not a transcript of the conversation. A section the
user cannot answer yet gets one line saying so, not a guess.

## How to ask

- Look first, per question below. Say what you found and ask the user to
  confirm or correct it. Ask openly only where the tree shows nothing.
- Ask the questions together, in one message, unless an answer changes what
  you would ask next.
- Write nothing the user did not say or confirm. A choice you made is yours:
  mark it as a proposal until they agree.

## The questions

### What it is

The tree does not say. Ask: what kind of game, how large it is meant to
become (a prototype, a jam game, a production), and who works on it (one
person, a team, other agents).

### Target platforms

Ask. The `.uproject` may carry a `TargetPlatforms` list; that field is
unverified here, so treat it as a hint to confirm, not as the answer.

The launcher's built-in `compile` and `package` commands build for Win64. If
the user names another platform, say that those two commands do not cover it
and that another build is a command they add to the config (skill
`unreagent-build`, section `custom-commands`).

### C++ and Blueprint

From the tree: the modules in the `.uproject` and under `Source/` show that
there is C++, and which plugins bring their own. What the tree cannot show is
the policy. Ask what is written in C++ and what in Blueprint: gameplay
classes, UI, data assets, prototypes. Ask whether an agent may add C++
classes, or works in Blueprint unless told otherwise.

This sets how sessions work later: every C++ change goes through the stop,
compile and start loop of skill `unreagent-iterate`.

### Source control

From the tree:

- `.git` is a git repository. `.gitmodules` lists submodules; a toolkit added
  with `git submodule add` is one. `.gitattributes` lines with `filter=lfs`
  mean Git LFS is in use.
- Marker files of other systems, such as `.p4config` or `.p4ignore` for
  Perforce and `.plastic/` for Plastic SCM, are unverified hints: confirm
  with the user.
- Nothing of the kind: the project is not under source control. The
  walk-through's project, made in Unreal Editor, was not.

Ask what the tree leaves open: how large binary assets (`.uasset`, `.umap`,
source art) are handled, whether binary files are locked while edited, and
whether an agent may commit or only leaves changes for the user.

Check the ignore rules while you are here, and propose what is missing:
`unreagent.local.yaml` (machine values, perhaps a token), `unreagent.exe`,
`unreagent.log` and `unreagent.log.1`, and every MCP config file the launcher
writes for an external agent (`mcp.writeConfig`, such as `.mcp.json`) when
`mcp.token` is set: it holds the token, and the launcher warns at start when
git does not ignore such a file. Whether `.claude/skills/unreagent-*`,
`.claude/agents/unreagent-*.md` and `.claude/rules/unreagent.md` are committed
is the user's decision: the launcher rewrites them at every start.

If the project is not under source control, do not set one up as part of
this. Write that it has none, and tell the user that the conversion and
toolkit steps are easier to undo with one.

### Permissions

Ask whether unreagent should decide which tool calls the agent may make, or
whether Claude Code's own prompts and settings are enough. Explain the state
first, because the answer depends on it:

- With no `permissions:` section, unreagent applies no policy. Claude Code's
  own permission prompts and settings alone decide.
- A policy in `unreagent.yaml` reaches Claude Code only when it runs headless
  (`-p` in `agent.args`). An agent in the launcher's window is interactive,
  and the policy is then never consulted.
- **`permissions.enabled: true` with `mode: allow_all` and an agent in the
  window makes things looser, not stricter**: the launcher then passes
  `--dangerously-skip-permissions`, and every tool call runs without a
  prompt, including one a deny rule names. The launcher warns about it at
  its start, in `unreagent.log`. Never write that combination unless the user
  asked for exactly this effect.

What to write:

- The user is content with Claude Code's prompts: no `permissions:` section.
  Nothing to write in the config.
- The user wants a policy: do not write a `permissions:` section from memory.
  Its options are documented in `unreagent.example.yaml`, which comes with
  the launcher's release archive. Read that file if it is at hand, otherwise
  ask the user for it. Say that the policy takes effect only for a headless
  agent.
- Either way, write what the user said about what an agent may and may not
  do into `## Conventions`, in their terms. That reaches every session,
  whatever the launcher enforces.

### Conventions

Ask last, and openly: naming and folder rules for assets, a code style, what
an agent must never touch, how the user wants to be asked. If there is
nothing, write "None stated yet." in place of `Not described yet.`

## Writing the file

- Show the user the text for each section and write after they agree.
  Replace each `Not described yet.` with the answer. Keep the five headings
  of a starter file; in an existing `CLAUDE.md` keep that file's structure
  (section `convert`).
- Do not write into `CLAUDE.md` what the launcher's rules file already says:
  how the editor is started and stopped, how to build, the agent table. It
  is in `.claude/rules/unreagent.md`.
- Facts you read from the tree (engine version, module names, plugins) may
  go into `## What it is` in one line each. They go stale: write only those
  a session would otherwise have to look up every time.
- Then delete the kickstart block: the `<!-- unreagent:kickstart:begin -->`
  line, the `<!-- unreagent:kickstart:end -->` line, and everything between.
  Delete it only when the sections are filled in. If the user stops the
  interview halfway, leave the block and say so.
- `unreagent init -check` shows the result: `CLAUDE.md: <path> is there,
  without the kickstart block`, or `still holds the kickstart block`. It
  looks for the two marker lines only: it reports the block as gone when one
  of them is missing, even if the block's text is still there.

Whether Claude Code shows the two comment lines to a session that has not
opened the file is unverified. Read the file before you edit it.
