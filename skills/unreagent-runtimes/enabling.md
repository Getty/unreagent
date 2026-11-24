# unreagent host runtimes: enabling a runtime

Each runtime defaults to off, and enabling one is a config edit by the user;
the skill itself, "Availability", says what a disabled runtime looks like.
This is what the user writes:

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

`uv`, `node` and `npm` are looked up on the launcher's `PATH` unless the config
gives an absolute path. A machine-specific absolute path belongs in the
git-ignored `unreagent.local.yaml`, not in the committed config. `project`
accepts the usual placeholders, so `${PROJECT_DIR}` is the portable way to pin a
runtime to the UE project directory.
