# Automatic injection

The bridge can append the context for a tool's area to each successful tool
result, under a `## Relevant UE 5.7 API Context` heading. That is off unless
the bridge's environment sets `INJECT_CONTEXT=true`. Every context file but
`parallel_workflows` opens with "This context is automatically loaded when
…", which is true only with that setting. In unreagent, the bridge's
environment is the `env` of its entry under `mcp.extraServers`. That option
is a map keyed by server name: an entry of the same name in
`unreagent.local.yaml` replaces the whole server definition, `env` included.
Injection adds several thousand characters to every matching result, so
asking for one category when you need it costs less.
