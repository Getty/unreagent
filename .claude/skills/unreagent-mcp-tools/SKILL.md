---
name: unreagent-mcp-tools
description: >
  How to add, change or debug an MCP tool in unreagent — the Tool shape, input
  schemas, defensive argument reading, tool descriptions as prompt surface, and
  the bearer-auth path. Use when touching internal/mcp/ or registerTools /
  registerFileTools in cmd/launcher/tools.go and filetools.go.
---

# unreagent — MCP tools

`internal/mcp/mcp.go` is a hand-rolled MCP server: JSON-RPC 2.0 over a single
HTTP POST endpoint, no SDK. It implements exactly `initialize`,
`notifications/initialized`, `notifications/cancelled`, `ping`, `tools/list`,
`tools/call`. That is the whole protocol surface the agents need — resist
growing it speculatively.

## Adding a tool

Tools are registered in `registerTools` (`cmd/launcher/tools.go`) and, for the
file-access group, `registerFileTools` (`cmd/launcher/filetools.go`):

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

A tool that belongs to a feature is gated where it is registered: the `skills`
tool is added only when `cfg.Skills.ViaMCP()`, and `registerTools` itself runs
only while `mcp.enabled`. A gate that can leave a feature with no way to the
agent gets a startup WARN (`setupSkills` warns when the skills tool is the
only path and `mcp.enabled` is off). A tool too large for `tools.go` lives in
the feature's file behind a constructor returning `mcp.Tool` (`skillsTool` in
`skills.go`).

## The description is prompt surface, not documentation

Every tool description ends up permanently in the agent's context. Write the
operating instructions there — when to reach for the tool, what the arguments
mean, what the failure modes look like — because that text is the only thing
standing between the agent and a guessing spiral. This is why `run_python`
explains the `uv` environment inline instead of assuming the agent will
investigate. Keep it dense; every token is paid on every request.

A description can be generated from data. The `skills` tool's is
`skillsToolPreamble` plus `skills.DescribeAll(set)`, one line per shipped
skill, so it grows with the set and every skill's frontmatter `description` is
tool prompt (skill `unreagent-skill-authoring` holds the rule for writing
one). Its unknown-name error lists the available names, which is why the tool
has no `list` action.

Descriptions are **English**, like everything that ships — every description in
`tools.go` and `filetools.go` is by now; keep it that way.

## Handler contract

- Signature: `func(args map[string]interface{}) mcp.ToolResult`.
- Return `mcp.ToolResult{Text: …}` on success, `{Text: …, IsError: true}` on a
  tool-level failure. Use the `errResult(err)` helper for the common case.
  A tool-level failure is **not** a JSON-RPC error — protocol errors are
  reserved for unknown methods and malformed params.
- Read arguments through the helpers in `helpers.go`: `getString`, `getInt`,
  `firstString`, `firstMap`. They exist because JSON gives you
  `map[string]interface{}` with numbers as `float64`; direct type assertions
  are how this code gets a panic in production.
- **Be defensive about field names.** `approve` reads `tool_name` / `toolName` /
  `name` / `tool` and `tool_input` / `input` / `arguments` / `parameters`
  because Anthropic does not document the `--permission-prompt-tool` input
  schema, and denies a request it cannot read. Follow that pattern for
  anything driven by an external contract you do not control.
- Handlers run on the HTTP server's goroutine and may be concurrent. Anything
  touching shared state goes through the supervisor, which is already
  serialized behind its control channel.

## Long-running work

Two adapters turn supervisor operations into tool handlers: `serviceAction`
(`tools.go`) serves the service lifecycle tools `ue_start` / `ue_stop` /
`ue_restart`, and `scriptAction` (`scripts.go`) the code-execution tools
`run_python` / `run_node`. Reuse them for a tool of the same kind. A lifecycle
tool meant to leave the service running passes `starts` true: `StartService` /
`RestartService` answer only after the start attempt, so a service not running
in their snapshot did not start, and the result is `IsError` naming
`ServiceStatus.LastError` — the bare snapshot would read as success. Its text
comes from `startFailure` (`tools.go`), which the launcher console's `start` /
`r` answers share (`commandLoop`); keep the two on it. The other
tools — `status`, `logs`, `run_command` — call the supervisor from their own
closure; a one-shot tool returns what `run_command` and `scriptAction` return,
`commandReply(res, toolOutputLines)`, with `IsError` on a non-zero exit.
`commandReply` (`helpers.go`) is `exit N`, a blank line and the last 300
lines, led by an `omittedMarker` line `[... N lines omitted]` when there were
more; N comes from `CommandResult.LinesBefore`, which also counts what the
supervisor's 2 MiB head/tail buffer dropped. `logs` uses `omittedMarker` with a
reason for the lines its 500-line buffer dropped. Both hand that head and the
lines to `capReply(head, lines)`, which holds the whole reply to
`toolReplyBytes` (48 KiB, a named constant, not a config option). Claude Code
saves a text reply over 50,000 characters to a file instead of putting it in
the agent's context; 48 KiB is at most 49,152 characters, marker included, so
the cap stays below that. It keeps the newest bytes, cuts a single oversized
line too, never splits a UTF-8 sequence, and puts the `bytesMarker` line
`[... N bytes omitted; a reply holds at most 48 KiB]` right after `head`,
which it never cuts. It joins only the lines it keeps, so pass the lines, not
a joined string: the log buffer can hold 500 lines of up to 1 MiB each.
`tailLines` alone, without a marker, is for log excerpts (`bridge.go`,
`lifecycle.go`), not tool replies.

Commands are one-shot and blocking (`RunCommand` / `RunOnce`); a UE `compile`
takes minutes and the agent's MCP client is waiting the whole time. If you add
anything slower, return a handle and let the agent poll `logs`, rather than
holding the request open.

`Supervisor.RunOnceContext(ctx, …)` is `RunOnce` bounded by a context: a ctx
already done starts nothing; a cancel mid-run closes the job, kills the child,
drains as on a normal exit and returns the partial output — either way the
error wraps `ctx.Err()`. Only bridge preparation (`npm install` in
`cmd/launcher/bridge.go`) uses it today. The tool handlers and
`prepareRuntimes` stay on `RunOnce` / `RunCommand` until k75, which adds a ctx
to `ToolHandler` for every handler at once — do not convert one ad hoc. The
console `c <name>` command (`commandLoop`, `cmd/launcher/lifecycle.go`) also
calls `RunCommand` and is not part of k75.

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

The server's own protocol — `initialize`, `tools/list`, `tools/call`, batches,
bearer auth, origin checks — is tested in `internal/mcp/mcp_test.go`. Bridge
preparation is tested in `cmd/launcher/bridge_test.go`: most of the smoke-test
cases (`smokeTestMCP` against a fake stdio server, `initResponse`,
`stderrTail`), the node bridge install, `prepareMCPBridges`,
`waitForEndpoint` and `dialAddr`. `cmd/launcher/mcpcheck_test.go` holds only
the two oldest smoke-test cases, a good answer and a crash. Extend the matching
file rather than writing a new harness.
