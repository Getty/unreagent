# unreagent — instant-try demo

This `unreagent.yaml` starts **without Unreal Engine and without an agent**. It
uses `ping` as a long-running placeholder "editor" so you can test the launcher
and the MCP server on Windows in 30 seconds.

> The "LLM thing" (Claude Code) is **not** included — it's a separately
> installed CLI. The demo deliberately leaves the agent out. How to enable it
> is below.

## Getting started

1. Place `unreagent.exe` (from the [release](https://github.com/Getty/unreagent/releases))
   in this folder — if it isn't there yet.
2. Double-click `unreagent.exe` (or start it in a console).
3. You'll see the placeholder "editor" being supervised and the MCP server
   running on `http://127.0.0.1:8765/mcp`.
4. **Test in the console** (stdin commands): `status` · `logs` · `c hello` ·
   `r ue` (restart) · `q` (quit).
5. **Test MCP via curl** (second console):
   ```bat
   curl -s -X POST http://127.0.0.1:8765/mcp -H "Content-Type: application/json" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}"
   ```

## Switching to "real"

1. Install Claude Code (e.g. `npm install -g @anthropic-ai/claude-code`) so
   that `claude` is on the PATH.
2. In `unreagent.yaml`:
   ```yaml
   agent: { enabled: true, command: claude, claudeIntegration: true }
   permissions: { enabled: true, mode: allow_all, deny: ["Bash(rm -rf *)"] }
   ```
3. For a real UE project: remove `unreal.editor`/`args` (the defaults from
   `${ENGINE}` take over) and place the files next to the `.uproject`. Details
   in the main [README](../README.md).
