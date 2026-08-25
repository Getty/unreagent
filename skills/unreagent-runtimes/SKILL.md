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
edit by the user.

```yaml
runtimes:
  python:
    enabled: true
    prepareOnStart: true   # uv sync at launcher start, if a pyproject.toml is there
    # uv: "uv"             # executable, resolved on PATH
    # project: ""          # working directory AND script location; empty = agent workdir
  node:
    enabled: true
    # node: "node"
    # npm: "npm"           # used only by prepareOnStart
    # project: ""
```

The shipped `unreagent.example.yaml` enables both, so a project set up from it
has them; a project with a hand-written config may not.

`uv`, `node` and `npm` are looked up on the launcher's `PATH` unless the config
gives an absolute path. A machine-specific absolute path belongs in the
git-ignored `unreagent.local.yaml`, not in the committed config. `project`
accepts the usual placeholders, so `${PROJECT_DIR}` is the portable way to pin a
runtime to the UE project directory.

## Execution model

Both tools take a single parameter and behave the same way around it.

`code` (string, required) — the complete program as source text. There is no
file parameter, no argv, no stdin, no environment parameter and no timeout
parameter.

The launcher writes `code` to a file **inside the runtime's working directory**,
runs it there, deletes it, and returns:

```
exit <code>

<the last 300 lines of stdout and stderr, merged>
```

The result is marked as an error when the exit code is non-zero. stdout and
stderr go into one buffer and cannot be told apart afterwards, so label your own
output if the distinction matters. Only the last 300 lines survive: a chatty
script loses its beginning, and printing a large payload is not a way to
retrieve it — write it to a file under the project root and read it with
`read_file` instead.

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
sweep below:

1. `runtimes.<lang>.project`, when set;
2. otherwise the agent workdir (`agent.workdir`, which itself defaults to the
   directory of the `.uproject`);
3. otherwise the launcher's own working directory.

It has to exist and be writable. If it does not, the call fails at script
creation — **before** the interpreter is started — with the message in the
failure table below. There is deliberately no fallback to the system temp
directory. A script running outside the project resolves neither its
`node_modules` nor its sibling Python modules, so a fallback would quietly hand
you a subtly different environment; a loud failure the user can fix in the
config is the intended outcome.

Two side effects follow from the script being a real file in the project:

- **It is briefly visible there.** Anything watching that directory — UE's
  directory watcher, a source-control client, a bundler in watch mode — can
  observe an `unreagent-<digits>.py` / `.mjs` appear and vanish within one run.
- **A hard kill leaves it behind.** The deletion is a deferred call in the
  launcher; killing the launcher skips it. At the next start the launcher sweeps
  the runtime directories and logs one line per file it finds:

  ```
  Stale script file removed: <path>
  WARN could not remove stale script file <path>: <error>
  ```

  The sweep matches only the generated shape — the prefix, then decimal digits,
  then `.py` or `.mjs`. A hand-written `unreagent-helper.py` is deliberately
  safe from it.

Because a leftover script sits in the user's repository, the UE project's own
`.gitignore` should carry:

```gitignore
unreagent-[0-9]*.py
unreagent-[0-9]*.mjs
```

The launcher's own repository has these entries, which does nothing for the
project the launcher is running in. If you are set up in a project that lacks
them, adding them is a reasonable thing to offer.

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

With `prepareOnStart: true` the launcher runs `uv sync` once at startup when the
working directory has a `pyproject.toml`, and logs `Runtime: preparing Python
(uv sync) …`. It is fire-and-forget in the background, so an early `run_python`
can overlap it; that is harmless, because `uv run` resolves the environment
anyway. A first call may simply take longer while dependencies are fetched.

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
- **`createRequire` is an option, not a necessity.** It has one genuine use:
  a package that ships CommonJS only and offers no ESM entry point, where the
  ESM loader cannot give you named exports (`SyntaxError: The requested module
  … does not provide an export named …`) or where the package's `exports` map
  offers only a `require` condition.

  ```js
  import { createRequire } from 'node:module';
  const require = createRequire(import.meta.url);
  const dep = require('some-cjs-only-package');
  ```

  `import.meta.url` is the correct anchor: the script lives in the project, so
  no absolute path has to be hard-coded. A dynamic `import()` of a CommonJS
  module works too and hands you its `default`.

With `prepareOnStart: true` the launcher runs `npm install` once at startup when
the working directory has a `package.json`, logging `Runtime: preparing Node
(npm install) …`. `runtimes.node.npm` is used for that step only — there is no
tool that runs npm on demand. A package script belongs in the launcher's
`commands:` section and is then reachable through `run_command`.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `-32602 unknown tool: run_python` | the runtime is disabled in this project | the user enables `runtimes.python.enabled` |
| `Error: 'code' missing` | `code` missing or empty | pass the source as `code` |
| `Error: could not create the script file in the working directory of the python runtime (<dir>): <error>`, second line `The directory must exist and be writable — check runtimes.python.project or agent.workdir.` | the runtime's directory is missing or not writable, so no script could be written and nothing was started (`node runtime` / `runtimes.node.project` in the `run_node` wording) | fix `runtimes.<lang>.project` or `agent.workdir`; there is no fallback location, so retrying unchanged fails identically |
| `Error: Start fehlgeschlagen: exec: "uv": executable file not found in %PATH%` | `uv` / `node` not installed or not on the launcher's PATH (a Linux dev build of the launcher says `$PATH` instead) | install it, or set an absolute path in `unreagent.local.yaml` |
| `ModuleNotFoundError: No module named 'unreal'` | you tried to reach the editor from the host | use the plugin's `execute_script` instead |
| `ModuleNotFoundError` for a project dependency | not declared in `pyproject.toml` | the dependency has to be added there; the script cannot install it |
| `ERR_MODULE_NOT_FOUND` for a bare specifier | the package is not installed in the project | `npm install` it in the project (or via `prepareOnStart`) |
| `SyntaxError: … does not provide an export named …` | a CommonJS package with no usable named ESM exports | `createRequire(import.meta.url)`, or import its `default` |
| `ReferenceError: require is not defined` | the script is ESM | use `import`, or `createRequire` |
| exit non-zero with a traceback | the script itself failed | the traceback is in the output; fix and rerun |
| the call never returns | no timeout exists | the script is blocking; it has to be killed on the host |

One launcher-side string is still German: the `Start fehlgeschlagen:` prefix the
supervisor puts on a process it could not start. Match it loosely; the wording
is expected to change to English, the behaviour is not.

## See also

- `ue-python` — `execute_script` and the editor's embedded interpreter, which is
  what you want whenever `unreal` has to be importable.
- `unreagent-files` — `read_file` / `write_file` / `list_dir` / `edit_file`, the
  way to get a generated artefact out of a run and back to yourself.
- `run_command` — the configured one-shot commands (compile, package).
