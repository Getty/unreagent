---
name: ue-tasks
description: >
  Poll, collect, list and cancel calls in the UE LLM Toolkit's task queue
  (task_submit, task_status, task_result, task_list, task_cancel). Use when a
  toolkit call returned a task_id or timed out, after every execute_script
  call, before a call that may hold the editor for more than 30 s, or to give
  one call a longer limit than the bridge's 300 s.
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
  seconds`. That is not an ordinary error: it may crash the editor (see
  "A 30 s timeout can crash the editor").
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
bridge's 30 s limit on each direct call, the `task_*` calls included. If the
plugin handles requests off the game thread, it has a 30 s limit of its own,
and raising this one gives no call more time; on the game thread it has none,
and a higher value lets the bridge wait longer (see "A 30 s timeout can crash
the editor"). In unreagent, these variables go in the bridge server's
`env` under `mcp.extraServers`. `extraServers` is a map keyed by server name:
an entry of the same name in `unreagent.local.yaml` replaces that server's
whole definition, `env` included.

## A 30 s timeout can crash the editor

Each time the bridge calls a plugin tool, it waits at most 30 s for the
answer: a direct call, each `task_*` call, and so each step of a queued call.
Which thread handles the request is up to the engine's HTTP server, and this
skill could not check it. On the game thread, the plugin runs the tool at once
and sets no limit of its own. Off it, as the plugin's comments say, the plugin
queues the job for the game thread and waits at most 30 s as well.

When a wait on your call runs out, you see `Error: Failed to execute tool:
Request timeout after 30000ms` (the bridge's limit) or `Error: Tool '<name>'
execution timed out after 30 seconds` (the plugin's). The plugin gives that
second answer only off the game thread, so it confirms the dangerous case
below. The first one tells you nothing either way: with the default settings,
the bridge's limit often runs out first.

That timeout is not an ordinary error:

- **In any case**, the call may still run once the game thread is free, and
  its change lands. After a timed-out `task_submit` it may run twice (below).
- **Off the game thread**, the timed-out request leaves its job queued, and
  when the job runs it writes its result into memory the plugin has already
  given back (`UnrealClaudeMCPServer.cpp`, an upstream bug). That can crash the
  editor, possibly minutes after the timeout.

The bridge's own steps can run into this with no error shown. It asks again
without a word when a status check times out, and when `task_submit` fails or
times out, it makes the call directly instead. After a timeout, the submitted
copy can still run later, so the change may land twice, even after the direct
call succeeds; that result has no task envelope around its JSON.

What to do:

- **Make long calls as tasks.** A queued call's tool runs inside the queue,
  which handles a long wait safely; only the quick steps around it come under
  the 30 s limit. A direct call puts the whole tool under it. Leave
  `MCP_ASYNC_ENABLED` on. For a call that may hold the game thread for more
  than about 30 s, `task_submit` it yourself (see "task_submit"): the bridge
  checks a queued call every 2 s, a check sent while the tool runs waits
  behind it, and you would not see that check time out.
- **Leave a busy editor alone.** While a script, an import, a level load or
  another long call holds the game thread, every further call waits behind
  it, the `task_*` calls included. Send only what you must. Poll a long task
  seldom: first when you expect it to be done, then at intervals under about
  5 minutes, because a result is gone 5 minutes after its task ends.
- **Do not retry after such a timeout.** Before a long call, note the `ue`
  service's `pid` and `restarts` from unreagent's `status` (skill
  `unreagent-iterate`), not the bridge's `unreal_status`. After a timeout,
  call it again: another `pid` or a higher `restarts` means the editor crashed
  and was restarted. Then read the state back before you repeat anything,
  because the call may still land. If a later call cannot reach the editor,
  check `status` again.

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
- **Only plugin tools can be queued.** `get_ue_context` and `status` (listed
  as `unreal_get_ue_context` and `unreal_status`) are answered by the bridge
  itself, and naming them fails like an unknown tool.
- **A limit of 30000 or less** does not end the wait sooner than 30 s: such a
  task goes through the plugin's ordinary 30 s wait for the game thread.

Submit a call yourself when it may hold the game thread for more than about
30 s, or needs more than the bridge's 300 s: pass a `timeout_ms` that covers
it, then poll as "A 30 s timeout can crash the editor" says. A
`task_status` sent while the call still holds the game thread waits behind
it, and one that waits 30 s is the timeout described there.

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
2. Wait before the first `task_status` `{"task_id": "<id>"}` too: the
   bridge's and the task's 300 s run out together, so the tool may still hold
   the game thread. While it says `running`, poll as "A 30 s timeout can crash
   the editor" says.
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
"timeout_ms": 900000}`, then `task_status` and `task_result` with the
returned `task_id`, polled as "A 30 s timeout can crash the editor" says.

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
| `Error: Failed to execute tool: Request timeout after 30000ms` | the bridge waited 30 s for the editor. The call may still run, and if the plugin handles requests off the game thread, the job left queued may crash the editor | do not retry. Check unreagent's `status`, then read the state back ("A 30 s timeout can crash the editor") |
| `Error: Tool '<name>' execution timed out after 30 seconds` | the plugin waited 30 s off the game thread. The job left queued may crash the editor, and the call may still run | the same |
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
- `unreagent-iterate`: `status`, for checking the editor after a 30 s timeout,
  editor restarts, which drop the queue, and waiting for the plugin to come
  back.
