---
name: unreagent-setup
description: >
  Set up an Unreal project for unreagent from inside a session: the config, the
  UE LLM Toolkit, the first build, CLAUDE.md. Use when the user says "set up
  the rest" or "set up unreagent for this project", wants an existing Unreal
  project (one with its own CLAUDE.md, .claude/ or unreagent.yaml) set up for
  unreagent, or when CLAUDE.md still holds the kickstart block. Main session
  only.
---

# Setting up a project for unreagent

The launcher started you in a project that is not set up yet, or only partly.
At the end the project has an `unreagent.yaml`, the UE LLM Toolkit built and
loading, a `CLAUDE.md` that describes the project, and the launcher's skills,
topic agents and rules file. Work from what the project tree shows and from
what `unreagent init` finds. Ask the user only what neither can tell.

This skill is for the main session, the one talking to the user. If you were
started as a subagent, do not run it and ask the user nothing: do your task
and say in your report that the project is not set up.

**C++ projects only.** Read the `.uproject` first. If it has no `Modules`
list, stop and tell the user that this setup covers C++ projects only. `init`
does not flag this: it reports
`project modules: the .uproject declares none, so there is nothing to build`
as `[ok]`. If the user wants the project to become a C++ project, that is a
change to their project to agree on first: skill `ue-cpp`, section
`project-layout`, "A Blueprint-only project gets its first C++ module", has
the files; then come back here.

Two labels are used below. *Measured* means seen on one walk-through: Windows
11, UE 5.8.4, one machine. *Unverified* means neither read in the launcher's
code nor measured. Everything else is read in the launcher's code, which is
cross-compiled and was not run on Windows for the parts newer than that
walk-through: what `init` writes into the project, the port finding, the
plugin-module check, and the give-up after three failed runs.

This skill has four sections:

| Section | Holds | Load it |
|---|---|---|
| `convert` | a project that already has an `unreagent.yaml`, a `CLAUDE.md`, or a `.claude/` with its own skills or agents: what `init` leaves alone, what a start deletes, what you propose to the user, what to check afterwards | at step 1, as soon as the first look finds an `unreagent.yaml`, a `CLAUDE.md`, or a `.claude/` with its own skills or agents: before step 2 and before anything is written |
| `interview` | each question for the user: what to read in the tree first, what to ask, which `CLAUDE.md` section the answer goes into, and where the permissions answer goes | before you ask the user the first question about the project, and before you propose additions to an existing `CLAUDE.md` |
| `human-steps` | what only the user can do (the trust question, a login, restarting the launcher, a taken port, installing a program), with what to tell them for each | before you ask the user to restart the launcher, and when a finding's fix is outside the project directory |
| `failures` | the failure table (what you see, what it means, what to do) | when `init` ends with an error you did not expect, `compile` fails or is not offered, the editor does not stay up after the build, or `unreal_status` is missing or not connected after the restart |

## What this session has

The launcher read its configuration once, when it started. Nothing you write
changes the running launcher.

- **Started without an `unreagent.yaml`**, the launcher runs on built-in
  defaults. You have its own tools (`status`, `ue_start`, `ue_stop`,
  `ue_restart`, `logs`, `run_command`) and the `skills` tool. The file tools
  and `run_python` / `run_node` are off on the defaults; use your own file and
  shell tools.
- **Toolkit tools** (`unreal_status` and the editor tools) are there only if
  the launcher found the plugin under `Plugins/` when it started. Its log
  says which: `running on built-in defaults: agent on, MCP server on,` and
  then either `no UE LLM Toolkit bridge (plugin not found below Plugins/)` or
  `UE LLM Toolkit bridge configured (<path>)`.
- **No tool restarts the launcher.** A new config, a plugin added since the
  start, a changed port: each takes effect at the next launcher start, and
  that start is the user's step. Whether this conversation continues after it
  is unverified. Plan for a new session: finish every write first.
- **A start without an `unreagent.yaml` writes no skills, agents, rules file
  or `CLAUDE.md` into the project.** `unreagent init` does, and the
  `unreagent.yaml` it writes turns `skills.materialize` on. A start with a
  config that has it on writes the launcher's skills, agents and rules file
  again, every time.

## The user agrees first

"Set up the rest" covers what creates new files: `init -check`, `init` in a
project where it deletes nothing, the build. It does not cover the following.
For each, say what you are about to do and what it changes, and act only
after the user agrees:

- changing or deleting anything the user made: an existing `CLAUDE.md`,
  `unreagent.yaml` or `unreagent.local.yaml`, their skills and agents, their
  ignore rules
- writing the interview's answers into `CLAUDE.md` and deleting the kickstart
  block
- adding the toolkit: a network fetch into their project, and in a git
  repository a change to `.gitmodules`
- `ue_stop` and `ue_restart`: a stop is a kill and saves nothing
- an `init` or a launcher start that deletes or replaces `unreagent-*`
  directories (section `convert`)

## The path for a fresh project

### 1. Look before you ask

Read, in the project directory:

- the `.uproject`: `EngineAssociation`, `Modules`, `Plugins`
- `Source/` and `Plugins/`: what is there, and whether a toolkit checkout is
- source control: `.git`, `.gitignore`, `.gitattributes`, or the files of
  another system
- `unreagent.yaml` or `unreagent.yml`, `unreagent.local.yaml`, `CLAUDE.md`,
  `.claude/CLAUDE.md`, `.claude/`
- the first lines of `unreagent.log` next to the `.uproject` (written only
  when the launcher runs its own agent in a window, the default): config or
  built-in defaults, the `Engine:` and `Project:` lines, every `WARN`

As soon as the first look finds an `unreagent.yaml`, a `CLAUDE.md`, or a
`.claude/` with its own skills or agents, load section `convert`: before
step 2 and before anything is written. The rest of this path still applies.

### 2. Run the preflight: `unreagent init -check`

`unreagent init -check` writes nothing and prints one line per finding. Run it
with your shell tool from the project directory, where `unreagent.exe` lies
next to the `.uproject`. Calling it from an agent's shell is unverified: the
walk-through ran it from a console.

`init` reads the config next to the executable. Do not pass `-config`: with it
`init` writes nothing, like `-check`.

Read every line. The level says what a start does about it:

| Level | Means | You |
|---|---|---|
| `[ok]` | checked and fine | nothing |
| `[info]` | a state, or what `init` would write | read it: it is how you learn what the next `init` writes |
| `[WARN]` | the launcher starts, and the line says what is missing then | act on each one, or decide with the user to accept it |
| `[BLOCKING]` | a start ends, or the editor cannot start | fix it before you hand the restart to the user |

A `Fix:` under a finding is the action for that finding. Take commands and
paths from it, not from memory: the text is built for this project and this
engine.

By kind of finding:

| Finding starts with | What to do |
|---|---|
| `MCP server cannot listen on <address>` | Expected while a launcher runs: it holds that port itself, and `init` cannot tell this launcher from another program. Compare the address with the launcher log's `MCP server listening on <address>` line. The same address is this launcher's own port: go on. Any other address is taken by another program: section `human-steps`. Seen on Linux with the port held; unverified on Windows. |
| `engine: not found`, or a WARN about the `.uproject`'s `EngineAssociation` | The engine root is a machine value: `engineRoot` in `unreagent.local.yaml` (see "Config values"). Ask the user for the path if the tree and the log do not show it. |
| `agent:`, `node:`, `npm:` not on PATH | A program to install: the user's step. Section `human-steps`. |
| `UE LLM Toolkit: not found below Plugins/` | Step 3. |
| `UE LLM Toolkit: the plugin is there (...) but the config has no mcp.extraServers.ue-llm-toolkit entry` | An `unreagent.yaml` written before the plugin was added. The fix prints the document with the entry. Propose that edit to the user: the launcher never edits an existing config. |
| `project modules: ... look unbuilt`, `plugin modules: ... look unbuilt` | Step 5. |
| a plugin `EngineVersion` WARN | The message names its fix. |
| `skills.dir ... holds unreagent-* directories` | Stop. Section `convert`: the next `init` or start deletes them. |
| `agents: in the way and left alone` | Section `convert`. |

### 3. Add the toolkit, before `init` writes the config

Take the command from the fix line of the `not found below Plugins/` finding.
It gives two forms, one for a git repository (a submodule) and one for a
project that is none (a clone), and it names the branch.

- **The branch is made for UE 5.8.** For another engine the fix line says so,
  and that unreagent does not know which version fits. Then do not add it:
  tell the user what the line says.
- Adding it is a network fetch into the user's project. Show the user the
  command and run it after they agree.
- **Add it before you run `unreagent init`.** `init` writes the toolkit's
  entry into the new `unreagent.yaml` only if the plugin is there at that
  moment. A config written earlier has no entry, a start with that config
  does not search `Plugins/`, and the agent then gets no toolkit tools, with
  one WARN in `init` as the only sign.

### 4. Run `unreagent init`

It never overwrites `unreagent.yaml` or `CLAUDE.md`. The skills and the
topic agents it writes again on every run, replacing older copies; for the
agent files and the rules file its output counts how many are new, refreshed
and unchanged. It names each file:

- `unreagent.yaml`: the built-in defaults plus `skills.materialize: true`
- `CLAUDE.md`: a starter with a kickstart block, only when the project has
  neither `CLAUDE.md` nor `.claude/CLAUDE.md`
- the shipped skills into `.claude/skills/unreagent-*/`, the topic agents as
  `.claude/agents/unreagent-*.md`, and the rules file `.claude/rules/unreagent.md`

**Read how the output ends, not the exit code.** There are two endings:

- With a `[BLOCKING]` finding, `init` ends with an error and a non-zero exit:
  `Error: preflight: N blocking finding(s), see [BLOCKING] above (unreagent init wrote <what>)`,
  where `<what>` lists what this run wrote, or is `nothing`. Inside a running
  launcher this is the ending you get, because of the port finding, and
  `init` has still written. A configuration that does not load ends the same
  way, with its own message before the bracket.
- Without one, the last line is the count,
  `N blocking, M warning(s). BLOCKING keeps the editor from starting; ...`,
  and the exit is 0. What was written is in the lines that start `Wrote`,
  above `Preflight:`.

If `init` ends with an error you did not expect, load section `failures`.

The launcher owns everything named `unreagent-*` in those three places and
rewrites it at every start; an agent or rules file someone edited is left
alone and warned about instead, and so no longer refreshed. Do not edit those files, and do not put project
rules into `.claude/rules/unreagent.md`: an edited file is no longer
refreshed. Project rules belong in `CLAUDE.md`.

### 5. Build, then start the editor

The toolkit is C++ source. An editor started before the build fails, and not
at once. *Measured*, with the editor started by the launcher at its own
start: it shut down and exited about 80 s after its start, with crash reports
written. From the launcher's code, not measured: `ue_start` reports success
as soon as the editor process is spawned, so a success there does not show
that the editor stays up, and the launcher gives up after three such runs in
a row, each within five minutes of its start.

1. `ue_stop`, after the user agrees. A stop is a kill and saves nothing: tell
   them so, and that it is needed because the build cannot run while the
   editor does.
2. `run_command` with `compile`. *Measured*, one machine: 3:01 for the toolkit
   in a blank C++ project, 6:42 for a template project together with the
   toolkit. The call returns only when the build ends. Skill
   `unreagent-build` before the first call. If `compile` fails or is not
   offered, load section `failures`.
3. `ue_start`, then `status` and `logs` for `ue`. *Measured*: the editor was
   up 11 to 12 s after its start, and its log then held
   `LogUnrealClaude: MCP Server started on http://localhost:3000`. That line
   shows the plugin loaded, also in a session that has no toolkit tools yet.
   Once, after each launcher start, the launcher log also says
   `MCP server 'ue-llm-toolkit': in-editor server … is ready — the toolkit
   lists <n> tools (project …, engine …).`: the plugin answered its status
   route, so its tools can run.
   Skill `unreagent-iterate` for reading `status`. If the editor does not
   stay up after the build, load section `failures`.

Run `unreagent init -check` once more. Apart from the port line, nothing
`[BLOCKING]` and no `look unbuilt` may be left.

### 6. Describe the project

The kickstart block in the starter `CLAUDE.md` tells the main session to ask
the user about the project. This step is its long form. Load section
`interview` before the first question.

- **Ask:** genre and scope, target platforms, what is written in C++ and what
  in Blueprint, how large binary assets are handled, whether unreagent should
  apply a permission policy.
- **Answer from the tree, and only confirm:** the engine version, the modules,
  the plugins, which source control system is in use.
- Show the user the text for each section, and say that you will then
  remove the kickstart block. Write after they agree: each answer into its
  section of `CLAUDE.md`, replacing `Not described yet.` Do not copy the
  launcher's rules or the agent table into `CLAUDE.md`: they are in
  `.claude/rules/unreagent.md`.
- **Then delete the kickstart block**: from the
  `<!-- unreagent:kickstart:begin -->` line to the
  `<!-- unreagent:kickstart:end -->` line, both lines included. While the
  block is there, it tells every new main session to ask again.

### 7. Hand over the restart, then verify

When every file is written, load section `human-steps` and ask the user to
restart the launcher. Tell them what the restart is for, and what to say to
the new session if this one does not continue, for example "verify the
unreagent setup".

After the restart, call `unreal_status` (skill `ue-context`). `connected: true`
is the end of the setup. If `unreal_status` is missing or not connected after
the restart, load section `failures`.

Then report to the user: what was written, what was built, every finding that
was accepted and not fixed, and every step that is still theirs.

## Config values

- **Machine values go into `unreagent.local.yaml`**, next to `unreagent.yaml`:
  an engine path, a port, a token, a full path to a program. `unreagent.yaml`
  is the project's and stays free of machine paths; it takes `${ENGINE}`,
  `${PROJECT}`, `${PROJECT_DIR}` and `${PROJECT_NAME}`. Check that source
  control ignores `unreagent.local.yaml`.
- **The local file is laid over the config key by key: a list replaces the
  whole list, a map merges by key.** A local `unreal.args` with one flag
  replaces the whole argument list of the config, or the default list
  (`-stdout`, `-FullStdOutLogOutput`), with that one flag.
- **A permission policy does not reach the agent in this window.** With
  `permissions.enabled` on and an interactive agent, the launcher warns
  `permissions are not enforced` at its start and the policy is never
  consulted. It applies only to an agent that runs headless. Say so before the
  user relies on a policy; section `interview` has what to write.
- Config edits are proposals. Show the lines, say which file, and write after
  the user agrees.

## See also

- `unreagent-build`: `run_command`, the `compile` reply, a build that fails or
  does not come back.
- `unreagent-iterate`: `ue_start`, `ue_stop`, `status`, `logs`, the give-up
  rules, the launcher log.
- `ue-context`: `unreal_status` and its result.
- `ue-cpp`: a project's first C++ module, and the C++ loop after setup.
