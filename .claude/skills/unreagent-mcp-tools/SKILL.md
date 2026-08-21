---
name: unreagent-mcp-tools
description: >
  How to add, change or debug an MCP tool in unreagent — the Tool shape, input
  schemas, defensive argument reading, tool descriptions as prompt surface, and
  the bearer-auth path. Use when touching internal/mcp/ or registerTools /
  registerFileTools in cmd/launcher/main.go.
---

# unreagent — MCP tools

`internal/mcp/mcp.go` is a hand-rolled MCP server: JSON-RPC 2.0 over a single
HTTP POST endpoint, ~300 lines, no SDK. It implements exactly `initialize`,
`notifications/initialized`, `notifications/cancelled`, `ping`, `tools/list`,
`tools/call`. That is the whole protocol surface the agents need — resist
growing it speculatively.

## Adding a tool

Tools are registered in `registerTools` (and `registerFileTools` for the
file-access group) in `cmd/launcher/main.go`:

```go
srv.AddTool(mcp.Tool{
    Name:        "ue_restart",
    Description: "…",                 // see below — this is prompt, not docs
    InputSchema: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "name": map[string]interface{}{"type": "string", "description": "…"},
        },
        "required":             []string{"name"},
        "additionalProperties": false,
    },
    Handler: func(args map[string]interface{}) mcp.ToolResult { … },
})
```

`AddTool` maintains a name→index map; registering the same name twice replaces
rather than duplicates. A nil `InputSchema` is served as an empty object schema.

## The description is prompt surface, not documentation

Every tool description ends up permanently in the agent's context. Write the
operating instructions there — when to reach for the tool, what the arguments
mean, what the failure modes look like — because that text is the only thing
standing between the agent and a guessing spiral. This is why `run_python`
explains the `uv` environment inline instead of assuming the agent will
investigate. Keep it dense; every token is paid on every request.

Descriptions are **English**. Some legacy descriptions in `main.go` are still
German — translate the ones you touch.

## Handler contract

- Signature: `func(args map[string]interface{}) mcp.ToolResult`.
- Return `mcp.ToolResult{Text: …}` on success, `{Text: …, IsError: true}` on a
  tool-level failure. Use the `errResult(err)` helper for the common case.
  A tool-level failure is **not** a JSON-RPC error — protocol errors are
  reserved for unknown methods and malformed params.
- Read arguments through the helpers in `main.go`: `getString`, `getInt`,
  `firstString`, `firstMap`. They exist because JSON gives you
  `map[string]interface{}` with numbers as `float64`; direct type assertions
  are how this code gets a panic in production.
- **Be defensive about field names.** `approve` reads `tool_name` / `toolName` /
  `name` and `tool_input` / `input` / `arguments` because Anthropic does not
  document the `--permission-prompt-tool` input schema. Follow that pattern for
  anything driven by an external contract you do not control.
- Handlers run on the HTTP server's goroutine and may be concurrent. Anything
  touching shared state goes through the supervisor, which is already
  serialized behind its control channel.

## Long-running work

`serviceAction` and `scriptAction` are the two adapters that turn supervisor
operations into tool handlers. Reuse them rather than calling the supervisor
directly from a closure — they already normalize status output and errors.

Commands are one-shot and blocking (`RunCommand` / `RunOnce`); a UE `compile`
takes minutes and the agent's MCP client is waiting the whole time. If you add
anything slower, return a handle and let the agent poll `logs`, rather than
holding the request open.

## Auth

`Server.SetToken` enables `Authorization: Bearer <token>` on every POST,
compared with `crypto/subtle.ConstantTimeCompare`. Empty token = open, which is
the historic behavior and safe only because the default bind is `127.0.0.1`.
When `mcp.token` is set, the token must also reach the embedded agent — the
`headers:` block written into `--mcp-config` by `buildMCPServers`. Change one,
change both, or the launcher's own agent locks itself out.

## Verifying by hand

```bash
curl -s -X POST http://127.0.0.1:8765/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | python3 -m json.tool
```

`cmd/launcher/mcpcheck_test.go` covers the handshake path; extend it rather
than writing a new harness.
