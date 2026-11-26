# The toolkit's notes: where they and the code disagree

The main skill says how to reach the notes. This section lists the places
where a note describes something the plugin's code does not do, found while
writing the skill from the bridge source (`Resources/mcp-bridge/index.js`,
`lib.js`) and the plugin's task queue (`MCPTaskQueue.cpp`). The code wins.

## `parallel_workflows`

- **Time limits.** It budgets a subagent's calls against the 2-minute task
  limit, but each call through the bridge is a task of its own with 300 s
  (`MCP_ASYNC_TIMEOUT_MS`, default 300000). The 2 minutes apply only to a
  `task_submit` without `timeout_ms`.
- **Concurrency.** Four tasks may run at once, but they take turns on the
  editor's one game thread, so parallel calls save waiting, not editor time.
  Meanwhile each call's requests wait behind the other calls, and one that
  waits 30 s may crash the editor (skill `ue-tasks`, "A 30 s timeout can
  crash the editor").

## The domain documents and `get_ue_context`

`domains/README.md` and `domains/meta.md` say that `get_ue_context` resolves
domain files along with the contexts. The bridge has no code that does so:
no query or category returns a domain document. Read them as files (main
skill, "Reading the files").

## Habits that do not carry over to MCP

- **`ue-tool.sh call <tool> '<json>'`** is a shell wrapper from the toolkit's
  own repository. The MCP equivalent is the tool `unreal_<tool>` with the same
  JSON as its arguments.
- **"Run `help <tool>`"** belongs to that same wrapper. Over MCP, read the
  tool's schema in the tool list instead.

## Project domains

The documents also describe project domains: four files that a `prep one time
init` session writes to `domains/` in the project root, from
`prep-init-guide.md`. No bridge code loads them either. If they exist, read
them like the plugin's own.
