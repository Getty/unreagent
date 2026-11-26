---
name: unreagent-runtimes
description: >
  Run Python or JavaScript on the machine that hosts the launcher, using the
  run_python and run_node MCP tools. Use for scripting, file processing and
  tooling around a UE project — and read it before assuming either one reaches
  the editor: neither executes inside Unreal.
---

# unreagent host runtimes

`run_python` and `run_node` execute code you supply **on the machine running the
launcher**, in an ordinary operating-system process next to the editor. They are
prepared environments: `uv` provisions the Python interpreter, virtual
environment and declared dependencies; `node` runs JavaScript with the project
as its working directory. You never create a venv, install anything, or inspect
the environment first — you pass source code and get back output and an exit
code.

This skill has four sections:

| Section | Holds | Load it |
|---|---|---|
| `enabling` | the `runtimes:` config block with its keys, where `uv`, `node` and `npm` are looked up, the placeholders `project` accepts | before you tell the user how to enable a runtime or set its executable or directory |
| `script-file` | the two side effects of the script being a real file in the project (briefly visible there, left behind by a hard kill), the startup sweep with its log lines, the `.gitignore` entries | when an `unreagent-<digits>.py` or `.mjs` file shows up in the project or in the launcher log, or before you offer the project `.gitignore` entries for them |
| `prepare` | what `prepareOnStart` runs at launcher start (`uv sync`, `npm install`), its log lines, the warning a failure leaves | when the launcher log or the user's console has a `Runtime: preparing …` line, or a first call takes long |
| `failures` | the failure table (what you see, what it means, what to do), and `createRequire` for a package that ships CommonJS only | when a call returns an error or a non-zero exit, an `import` that fails included, or does not come back |

## These are not Unreal Python

This is the mistake worth preventing before anything else. `run_python` starts a
separate Python process on the host. It has no `unreal` module, no editor state,
no access to the running editor's memory, and nothing it does is visible inside
UE. `import unreal` fails with `ModuleNotFoundError` — if your script opens with
that line, you have reached for the wrong tool.

Python **inside** the editor is a different tool on a different server: the
in-editor plugin's `execute_script`, covered by the `ue-python` skill. It runs in
the editor's embedded interpreter, where `unreal` exists and asset and actor APIs
work.

| Goal | Tool |
|---|---|
| Query or modify assets, actors, levels, blueprints, materials | the plugin's editor tools; `execute_script` for raw UE Python |
| Parse a log, transform JSON/CSV, generate files, call an HTTP API, run a helper script | `run_python` / `run_node` |
| Compile or package the project | `run_command` |
| Read or write individual project files | `read_file` / `write_file` (`unreagent-files`) |

## Availability

Each runtime is registered only when it is enabled in the launcher's config, and
each defaults to off. A disabled runtime is not a refused call: the tool is
absent from `tools/list`, and calling it returns `-32602 unknown tool:
run_python`. There is no command-line flag for these — enabling them is a config
edit by the user. Both tools also need the launcher's MCP server
(`mcp.enabled`); without it no tool exists at all.

The shipped `unreagent.example.yaml` enables both, so a project set up from it
has them; a project with a hand-written config may not.

Load section `enabling` before you tell the user how to enable a runtime or
set its executable or directory: it has the `runtimes:` config block with its
keys, where `uv`, `node` and `npm` are looked up, and the placeholders
`project` accepts.

## Execution model

Both tools take a single parameter and behave the same way around it.

`code` (string, required) — the complete program as source text. There is no
file parameter, no argv, no stdin, no environment parameter and no timeout
parameter.

The launcher writes `code` to a file **inside the runtime's working directory**,
runs it there, deletes it, and returns a reply shaped by the exit code, as
`run_command`'s is (`unreagent-build`). A script that exits 0 returns:

```
exit 0

[... N lines omitted]
[... B bytes omitted; a reply holds at most 48 KiB]
<the last 300 lines of stdout and stderr, merged, at most 48 KiB>
```

A script that exits non-zero returns an error result of at most 10,000 bytes,
the `exit` line and the marker included:

```
exit <code>

<the first lines of stdout and stderr, merged, up to 4 KiB>
[... N lines omitted]
<as many of the last lines as fit>
```

stdout and stderr go into one buffer and cannot be told apart afterwards, so
label your own output if the distinction matters.

**On success** only the last 300 lines survive: a script that printed more
gets the `[... N lines omitted]` line (`[... 1 line omitted]` for one) and
loses those N lines. The reply as a whole holds at most 48 KiB: when those
lines are longer, their oldest bytes go too, and
`[... B bytes omitted; a reply holds at most 48 KiB]` follows the `exit` line
and any lines marker. The first line you see can then start mid-line, and a
single line longer than 48 KiB comes back as its end only.

**On failure** the output comes back whole when it fits in the 10,000 bytes
with the `exit` line, even past 300 lines. Otherwise you get its first whole
lines within 4 KiB, then `[... N lines omitted]`, which counts every line
left out, then as many of its last whole lines as fit. A single line longer
than its share is cut, the first one to its start, the last one to its end,
and the marker then counts bytes instead:
`[... B bytes omitted; a failing call's reply holds at most 10 KB]`. The
limit is lower than for a success because Claude Code cuts an error result
over about 11,000 characters to its first and last 5,000.

Output past 2 MiB also loses its middle, as `run_command` output does, and
the markers count it too (`unreagent-build`, section `reply-cuts`).

Any marker means output is gone: the launcher keeps none of it, and a rerun
prints the same. Narrow the script instead: print only the lines you need, a
count, or a summary. Printing a large payload is not a way to retrieve it —
write it to a file under the project root and read it with `read_file`
instead.

A script that can fail should put the error that decides it at the start or
at the end of its output, where a failing reply keeps it. An uncaught
exception does that on its own: the traceback or stack prints last. Write
long diagnostics to a file and read it back with `read_file` rather than
print them. Python buffers stdout when, as here, it is not a terminal, so a
line written to `sys.stderr` can land above stdout printed before it: keep
the deciding line on one stream, or print with `flush=True`.

| | `run_python` | `run_node` |
|---|---|---|
| Command | `<uv> run python <script>` | `<node> <script>` |
| Script file | `unreagent-<digits>.py`, deleted after the run | `unreagent-<digits>.mjs`, deleted after the run |
| Directory — working directory **and** script location | `runtimes.python.project`, else the agent workdir | `runtimes.node.project`, else the agent workdir |
| Environment | inherited from the launcher | inherited from the launcher |

### Where the script lands

The script file sits in the same directory the process runs in. There is no
second location to reason about: relative paths you open at runtime and module
lookup in both languages resolve against that one directory, which is normally
the project.

The directory is chosen once, identically for both tools and for the startup
sweep (section `script-file`):

1. `runtimes.<lang>.project`, when set;
2. otherwise the agent workdir (`agent.workdir`, which itself defaults to the
   directory of the `.uproject`);
3. otherwise the launcher's own working directory.

It has to exist and be writable. If it does not, the call fails at script
creation — **before** the interpreter is started — with the message in the
failure table (section `failures`). There is deliberately no fallback to the
system temp directory. A script running outside the project resolves neither its
`node_modules` nor its sibling Python modules, so a fallback would quietly hand
you a subtly different environment; a loud failure the user can fix in the
config is the intended outcome.

The script being a real file in the project has two side effects:

- **It is briefly visible there.** Anything watching that directory can
  observe an `unreagent-<digits>.py` / `.mjs` appear and vanish within one
  run.
- **A hard kill leaves it behind.** Killing the launcher skips the deletion.
  At the next start the launcher sweeps the directories of the runtimes that
  are enabled then; a disabled runtime's leftovers stay.

The project's own `.gitignore` should ignore these script files; the entries
are in section `script-file`.

Load section `script-file` when an `unreagent-<digits>.py` or `.mjs` file
shows up in the project or in the launcher log, or before you offer the
project `.gitignore` entries for them: it has the sweep's log lines, the file
names the sweep matches, and the `.gitignore` entries.

### No timeout

Nothing kills a run. The launcher waits for the process to exit, so a script
that blocks — waiting on input, serving HTTP, looping forever — hangs the tool
call until the process dies on its own. Bound your own loops, never start a
server here, and do not use these tools for anything meant to stay running.
(The launcher supervises exactly two long-lived processes, the editor and the
agent; a run started here cannot become one of them.)

Each run is attached to a Job Object, so its children die with the launcher
rather than lingering as orphans.

## run_python specifics

`uv run` discovers the project from the working directory and provisions the
interpreter, the virtual environment and the dependencies declared in its
`pyproject.toml` before running your script. That is the whole setup story:
there is nothing for you to install.

- **Dependencies must already be declared.** Anything in the project's
  `pyproject.toml` (or an equivalent uv-managed environment) imports. Ad-hoc
  installation from inside the script is not supported; if a package is missing,
  it has to be added to the project's dependencies.
- **The standard library always works**, which covers most of what these scripts
  are used for: `json`, `csv`, `pathlib`, `re`, `subprocess`, `urllib`.
- **`sys.path[0]` is the working directory**, because that is where the script
  file lives. Loose `.py` modules next to the `pyproject.toml` are importable by
  name — `import my_helper` works with no `sys.path` surgery.
- `__file__` points at the script inside that same directory, so its parent is
  the project. Prefer relative paths or `os.getcwd()` anyway: they say the same
  thing more plainly, and the file itself is deleted when the run ends, so its
  path is worthless to anything that outlives the call.
- **The first import of a loose module writes `__pycache__/` into the project.**
  That is Python's doing, not the launcher's, and the startup sweep leaves it
  alone. Most projects already ignore it in git; a UE project that does not may
  want to.

With `prepareOnStart: true` the launcher runs `uv sync` once at startup when
the working directory has a `pyproject.toml`. It runs in the background, so
an early `run_python` can overlap it; that is harmless. A sync that fails, or
a `uv` that cannot be started, leaves a warning in the launcher log. Load
section `prepare` when the launcher log or the user's console has a
`Runtime: preparing …` line, or a first call takes long.

## run_node specifics

- **The script is always an ES module.** The file is written with an `.mjs`
  extension, so Node treats it as ESM regardless of what `package.json` says.
  Use `import`; `require` is not defined, and top-level `await` is available.
  This is fixed on purpose — an extension that depended on the project's
  `package.json` would make the same code work in one project and fail in the
  next.
- **Bare imports resolve through the project's `node_modules`.** Node walks up
  from the importing file, and the importing file is in the project, so
  `import lodash from 'lodash'` works whenever the package is actually installed
  there. `ERR_MODULE_NOT_FOUND` therefore means what it says: the dependency is
  missing, not merely out of reach. Node built-ins (`node:fs`, `node:path`,
  `node:process`, …) are always available.
- **`createRequire` is an option, not a necessity.** Its one genuine use is a
  package that ships CommonJS only and offers no ESM entry point. Load section
  `failures` when an `import` of such a package fails: it has the two cases
  and the snippet.

With `prepareOnStart: true` the launcher runs `npm install` once at startup
when the working directory has a `package.json`, in the background, at the
same time as the Python step. A failure is the same warning (section
`prepare`).
Besides that step, `runtimes.node.npm` serves only the launcher's own
installs of node-started MCP bridges — there is no tool that runs npm on
demand. A package script belongs in the launcher's
`commands:` section and is then reachable through `run_command`.

## When a call fails

When a call returns an error or a non-zero exit, an `import` that fails
included, or does not come back, load section `failures`: a table from what
you see to what it means and what to do, and `createRequire` for a package
that ships CommonJS only.

## See also

- `ue-python` — `execute_script` and the editor's embedded interpreter, which is
  what you want whenever `unreal` has to be importable.
- `unreagent-files` — `read_file` / `write_file` / `list_dir` / `edit_file`, the
  way to get a generated artefact out of a run and back to yourself.
- `run_command` — the configured one-shot commands (compile, package).
