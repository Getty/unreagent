# Toolkit task queue: when a call fails

"A 30 s timeout can crash the editor" is a heading of the skill itself.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Error: Task timed out after 300000ms (task_id: …)` | the bridge stopped waiting. The task may still be running | `task_status` with that id. Read the state back before you retry |
| `Error: Task not found: <id>` | a mistyped id, a result older than about 5 minutes, or an editor restart | `task_list`. Read the state back |
| `Error: Invalid task_id format` | the string is not a task id | copy the id exactly |
| `Error: Task is still running - use task_status to poll` | `task_result` came too early | poll `task_status` |
| `Error: Failed to submit task - queue may be at capacity or tool not found` | `tool_name` is unknown, prefixed with `unreal_`, or a bridge-only tool, or 100 tasks are unfinished | use the bare name. Check `task_list` |
| `Error: Failed to execute tool: Request timeout after 30000ms` | the bridge waited 30 s for the editor. The call may still run, and if the plugin handles requests off the game thread, the job left queued may crash the editor | do not retry. Check unreagent's `status`, then read the state back ("A 30 s timeout can crash the editor") |
| `Error: Tool '<name>' execution timed out after 30 seconds` | the plugin waited 30 s off the game thread. The job left queued may crash the editor, and the call may still run | the same |
| `task_result` gives `Error: Tool execution timed out after 30 seconds` (no tool name) | a task submitted with `timeout_ms` of 30000 or less ran past 30 s; the queue's worker waited through the plugin's ordinary 30 s limit (`MCPToolRegistry.cpp`, `ExecuteTool`). This path keeps its state safely and is not the crash risk above, but the tool still runs to its end | read the state back; resubmit with a `timeout_ms` above 30000 if it must run longer |
| status `timed_out`, then `cancelled`, and `task_result` gives `Error: Task cancelled during execution` | a task with `timeout_ms` below 30000 outlived its own limit: the queue's check, which runs once a minute, marked it timed out and for cancellation before the 30 s wait ended (`MCPTaskQueue.cpp`, `CheckTimeouts`). The tool still ran to its end | the same |
| status `failed` or `cancelled`, and the change appears later anyway | the limit ended the wait, not the tool | read the state back before you repeat anything |
| a parameter seems to have no effect | its name is wrong, and queued calls do not warn | check the name against the tool's schema |
