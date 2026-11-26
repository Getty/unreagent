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
    # npm: "npm"           # prepareOnStart, and the npm install of node-started MCP bridges
    # project: ""
```

`uv`, `node` and `npm` are looked up on the launcher's `PATH` unless the config
gives an absolute path. A machine-specific absolute path belongs in the
git-ignored `unreagent.local.yaml`, not in the committed config. `project`
accepts the usual placeholders, so `${PROJECT_DIR}` is the portable way to pin a
runtime to the UE project directory. A relative `project` is used as written,
so it resolves against the launcher's working directory, not the project's.

`npm` is not a Node-runtime setting only: the launcher also uses it for the
`npm install` of an MCP bridge started by `node` under `mcp.extraServers`
(such as the UE LLM Toolkit's), and `init` checks it, even with
`runtimes.node.enabled: false`.
