# unreagent host runtimes: when a call fails

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `-32602 unknown tool: run_python` | the runtime is disabled in this project | the user enables `runtimes.python.enabled` |
| `Error: 'code' missing` | `code` missing or empty | pass the source as `code` |
| `Error: could not create the script file in the working directory of the python runtime (<dir>): <error>`, second line `The directory must exist and be writable — check runtimes.python.project or agent.workdir.` | the runtime's directory is missing or not writable, so no script could be written and nothing was started (`node runtime` / `runtimes.node.project` in the `run_node` wording) | fix `runtimes.<lang>.project` or `agent.workdir`; there is no fallback location, so retrying unchanged fails identically |
| `Error: start failed: exec: "uv": executable file not found in %PATH%` | `uv` / `node` not installed or not on the launcher's PATH (a Linux dev build of the launcher says `$PATH` instead) | install it, or set an absolute path in `unreagent.local.yaml` |
| `ModuleNotFoundError: No module named 'unreal'` | you tried to reach the editor from the host | use the plugin's `execute_script` instead |
| `ModuleNotFoundError` for a project dependency | not declared in `pyproject.toml` | the dependency has to be added there; the script cannot install it |
| `ERR_MODULE_NOT_FOUND` for a bare specifier | the package is not installed in the project | `npm install` it in the project (or via `prepareOnStart`) |
| `SyntaxError: … does not provide an export named …` | a CommonJS package with no usable named ESM exports | `createRequire(import.meta.url)`, or import its `default` |
| `ReferenceError: require is not defined` | the script is ESM | use `import`, or `createRequire` |
| exit non-zero with a traceback | the script itself failed | the traceback is in the output; fix and rerun |
| the call never returns | no timeout exists | the script is blocking; it has to be killed on the host |

## A package that ships CommonJS only

This is the last item of the list in the skill itself, "run_node specifics".

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
