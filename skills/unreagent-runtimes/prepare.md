# unreagent host runtimes: prepareOnStart

Two passages, one per runtime. Both steps run once, at launcher start.

## run_python

With `prepareOnStart: true` the launcher runs `uv sync` once at startup when the
working directory has a `pyproject.toml`, and logs `Runtime: preparing Python
(uv sync) …`. It runs in the background, so an early `run_python` can overlap
it; that is harmless, because `uv run` resolves the environment anyway. A first
call may simply take longer while dependencies are fetched. A sync that fails,
or a `uv` that cannot be started, leaves a warning in the launcher log, with
the last 20 lines of uv's output below it when there was any:

```
WARN Runtime: preparing Python failed — uv sync in <dir>: <cause>
```

`uv` is the command as `runtimes.python.uv` names it; `<cause>` is `exit N`,
or `start failed: …` when the command is not found. When that log is a file,
the warning's first line also appears on the user's console, pointing at the
log when there is output below it. A successful sync ends
with `[prepare:python] done (exit 0)` and no warning. Where the launcher log is
and how to read it: `unreagent-iterate`.

## run_node

With `prepareOnStart: true` the launcher runs `npm install` once at startup when
the working directory has a `package.json`, logging `Runtime: preparing Node
(npm install) …`. It runs in the background, at the same time as the Python
step. A failure is the same warning, with the last 20 lines of npm's output:
`WARN Runtime: preparing Node failed — npm install in <dir>: <cause>`.
