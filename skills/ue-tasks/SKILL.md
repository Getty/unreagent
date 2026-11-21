---
name: ue-tasks
description: >
  Poll, collect, list and cancel calls in the UE LLM Toolkit's task queue
  (task_submit, task_status, task_result, task_list, task_cancel). Use when a
  toolkit call returned a task_id or timed out, after every execute_script
  call, or to give one call a longer limit than the bridge's 300 s.
toolkit_tools: [task_submit, task_status, task_result, task_list, task_cancel]
---

# Toolkit task queue

The UE LLM Toolkit plugin keeps a task queue inside the editor. A task is one
call of one toolkit tool. It has an id, a status and, once it has ended, a
result. Five tools work on the queue:

| Tool | For |
|---|---|
| `task_submit` | start a tool call as a task and get its id back at once |
| `task_status` | where one task stands |
| `task_result` | the result of a task that has ended |
| `task_list` | recent tasks, newest first |
| `task_cancel` | withdraw a task that has not started yet |

You seldom need `task_submit`, because the plugin's bridge already runs every
other toolkit call as a task and waits for it. The other four matter whenever
a call hands you a `task_id`: after a bridge timeout, and after every
`execute_script` call (skill `ue-python`).

This skill was written from the toolkit's C++ and bridge source. It has not
been exercised against a running editor.

## Before the first call

- **Tool names.** The bridge lists the tools as `unreal_task_submit`,
  `unreal_task_status`, and so on. Your MCP client may put its own server prefix
  in front of that. Inside the queue, tools go by their bare names:
  `tool_name: "asset_search"`, never `unreal_asset_search`.
- **These five are not queued.** The bridge calls the `task_*` tools directly,
  and each such call waits at most 30 s for the editor.
- **Every toolkit call runs on the editor's game thread**, one after another.
  These five tools are no exception. While a long call holds the game thread
  (a script, an import, a level load), a `task_status` call waits behind it.
  After 30 s it fails with `Error: Failed to execute tool: Request timeout
  after 30000ms` or `Error: Tool 'task_status' execution timed out after 30
  seconds`. That means busy, not lost: wait, then ask again.
- **Tasks live in editor memory.** An editor stop, crash or restart loses every
  task and every result.

## What the bridge does with an ordinary call

With its default settings, the bridge turns each toolkit call other than
`task_*` into three steps:

1. `task_submit` with your arguments and `timeout_ms: 300000`;
2. `task_status` every 2 s;
3. `task_result` once the status is `completed`, `failed` or `cancelled`.

Three consequences apply to every toolkit skill:

- **Result shape.** On success you get the tool's message, a blank line, and
  the task as JSON. The tool's own result is under the task's `data` key, next
  to `task_id`, `tool_name`, `status`, `progress`, `submitted_at`, `started_at`,
  `completed_at`, `duration_ms`, `success` and `message`. On failure you get
  `Error: <message>`, and any data is dropped. If the submit step itself fails,
  the bridge falls back to a direct call, and the JSON is then the tool's own
  result without the task around it.
- **Timeouts.** After 300 s the bridge stops waiting and returns `Error: Task
  timed out after 300000ms (task_id: …)`. The task has the same 300 s limit,
  but a limit ends the wait, never the tool: work already running on the game
  thread runs to its end. `task_status` can therefore say `failed`, `timed_out`
  or `cancelled` while the change is still on its way. Read the editor's state
  back before you retry.
- **Misspelt parameters go unreported.** Called directly, the plugin appends
  `WARNING: Unknown parameter ignored: …` to the message when you pass a
  parameter the tool does not declare. A task with a limit above 30 s skips
  that check, and every call the bridge queues has 300 s. A wrong parameter name
  is ignored without a word, so check names against the tool's schema.

The bridge reads `MCP_ASYNC_ENABLED` (`false` makes every call direct),
`MCP_ASYNC_TIMEOUT_MS`, `MCP_POLL_INTERVAL_MS` and `MCP_REQUEST_TIMEOUT_MS`
from its environment. `MCP_REQUEST_TIMEOUT_MS` (default 30000) is the
bridge's 30 s limit on each direct call, the `task_*` calls included. In
unreagent, these variables go in the bridge server's `env` under
`mcp.extraServers`. `extraServers` is a map keyed by server name: an entry of
the same name in `unreagent.local.yaml` replaces that server's whole
definition, `env` included.

## Statuses

| Status | Meaning |
|---|---|
| `pending` | queued. At most four tasks run at once, so a fifth waits |
| `running` | started |
| `completed` | the tool returned success |
| `failed` | the tool returned an error, or the task's wait ran past its limit |
| `timed_out` | a check that runs once a minute found the task still running past its limit |
| `cancelled` | cancelled before it started, or marked for cancellation while running and then no longer waited for |

- **No progress in between.** `progress` is `-1` while a task waits or runs,
  and `100` once the queue has finished with it. It stays `-1` for a task
  cancelled while pending and for a `timed_out` one. No tool reports anything
  else, despite the `progress: 50` in `task_submit`'s own example.
- **A task past its limit** ends as `failed` (`Task execution timed out after
  <n> seconds`), `timed_out` or `cancelled`, depending on which of two internal
  checks fires first. A `timed_out` task turns `cancelled` when the queue
  stops waiting for its tool, which can be before the tool has finished. None
  of them stops the tool.
- **Four running tasks still take turns.** They share the one game thread.
  Submitting several tasks saves your own waiting, not editor time.
- **Results expire.** A task and its result stay for 5 minutes after it ends,
  and a sweep runs once a minute. After 5–6 minutes the id gives `Task not
  found: <id>`.
- **At most 100 unfinished tasks.** Beyond that, `task_submit` fails.

## task_submit

| Parameter | Default | Meaning |
|---|---|---|
| `tool_name` | required | the tool's bare name, such as `asset_search` |
| `params` | `{}` | the tool's arguments, as a JSON object |
| `timeout_ms` | `120000` | the task's limit in milliseconds. `0` also means 120000 |

The result holds `task_id`, `tool_name`, `status: "pending"` and `timeout_ms`.
The message is `Task submitted: <id>`.

- **`params` must be an object.** A JSON string in its place is ignored, and
  the tool then runs with no arguments at all.
- **The tool's arguments are checked when the task runs.** A bad argument
  surfaces in `task_result`, not here.
- **Only plugin tools can be queued.** `get_ue_context` and `status` are
  answered by the bridge itself, and naming them fails like an unknown tool.
- **A limit of 30000 or less** does not end the wait sooner than 30 s: such a
  task goes through the plugin's ordinary 30 s wait for the game thread.

Submit a call yourself when it needs more than the bridge's 300 s: pass a
larger `timeout_ms`, then poll. While that call holds the game thread, your
`task_status` calls time out as described above. That is expected.

## task_status

`task_id` (required). Returns the task without its result: `task_id`,
`tool_name`, `status`, `progress`, `submitted_at`, and `started_at` once it has
started. When it has ended, it adds `completed_at` and `duration_ms`. The
message is `Task <id>: <status>`.

## task_result

`task_id` (required). It works only once the task has ended. Before that it
fails with `Task is still pending - use task_status to poll` (or `still
running`).

- **The task succeeded.** You get the tool's own message, then the task as JSON
  with the tool's result under `data`, the same shape the bridge hands you.
- **The task failed, timed out or was cancelled.** You get `Error: <message>`
  and nothing else. The data the tool returned with its error is dropped on the
  way to you.

## task_list

| Parameter | Default | Meaning |
|---|---|---|
| `include_completed` | `true` | include ended tasks of every terminal status |
| `limit` | `50` | at most this many, clamped to 1–500 |

Returns `tasks[]`, newest first and shaped like `task_status`, plus `count`,
`total_pending`, `total_running` and `total_completed`. `total_completed`
counts every ended task, failed ones included. Because the bridge queues every
call, the list shows your own recent calls, which is how to recover an id you
did not note.

## task_cancel

`task_id` (required).

- **A pending task** is cancelled at once and never runs.
- **A running task is not stopped.** The call answers `Cancellation requested
  for task <id>` with `cancelled: true`, but no tool checks for cancellation.
  The work runs to its end, its changes land, and only its result is replaced
  by `Task cancelled during execution`.
- **An ended task** gives `Cannot cancel task (status: <status>)`.

Treat `task_cancel` as "forget the result", never as "undo".

## Typical sequences

**After a bridge timeout**

1. Take the id from `Error: Task timed out after 300000ms (task_id: <id>)`.
2. `task_status` `{"task_id": "<id>"}`. While it says `running`, wait and ask
   again.
3. `completed`: `task_result` gives the result you were waiting for.
4. `failed`, `timed_out` or `cancelled`: the tool may still be at work. Read
   the state back with the domain's own read tool, such as `get_level_actors`
   or `asset_search`, before you run the call again.

**After execute_script**

The message names the script's own task id. Poll that one, not the `task_id`
at the top of the JSON, which is the bridge's finished task for the call. Skill
`ue-python` has the details.

**A call that needs more time**

`task_submit` `{"tool_name": "<tool>", "params": {<the tool's arguments>},
"timeout_ms": 900000}`, then `task_status` and `task_result` with the returned
`task_id`.

**An id you did not note**

`task_list` `{"limit": 10}` and match `tool_name` and `submitted_at`. Ended tasks
are only listed for about 5 minutes.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Error: Task timed out after 300000ms (task_id: …)` | the bridge stopped waiting. The task may still be running | `task_status` with that id. Read the state back before you retry |
| `Error: Task not found: <id>` | a mistyped id, a result older than about 5 minutes, or an editor restart | `task_list`. Read the state back |
| `Error: Invalid task_id format` | the string is not a task id | copy the id exactly |
| `Error: Task is still running - use task_status to poll` | `task_result` came too early | poll `task_status` |
| `Error: Failed to submit task - queue may be at capacity or tool not found` | `tool_name` is unknown, prefixed with `unreal_`, or a bridge-only tool, or 100 tasks are unfinished | use the bare name. Check `task_list` |
| `Error: Failed to execute tool: Request timeout after 30000ms` | the game thread is busy with another call | wait, then ask again |
| `Error: Tool 'task_status' execution timed out after 30 seconds` | the same | the same |
| status `failed` or `cancelled`, and the change appears later anyway | the limit ended the wait, not the tool | read the state back before you repeat anything |
| a parameter seems to have no effect | its name is wrong, and queued calls do not warn | check the name against the tool's schema |

## See also

- `ue-python`: `execute_script`, which always answers with a task id.
- `ue-level-actors`, `ue-levels`, `ue-assets`: the read tools for checking state
  after a timeout.
- `ue-context`: the plugin's `parallel_workflows` notes on splitting work
  across parallel calls. Two of its rules differ from the code: it budgets a
  subagent's calls against the 2-minute task limit, although each bridge call
  is a task of its own with 300 s, and it treats four running tasks as four
  at once, although they share one game thread.
- `unreagent-iterate`: editor restarts, which drop the queue, and waiting for
  the plugin to come back.
