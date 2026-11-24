# The `unreal_status` result

What the bridge's `unreal_status` tool returns, field by field. The main
skill, "status", says what the tool is and how it differs from unreagent's
own `status`.

- **Plugin reachable.** The result is JSON text with `connected: true`,
  `project`, `engine`, `context_system`, `tool_summary`, `total_tools` and
  `message`.
  - `project` and `engine` are what the plugin reports about itself.
  - `context_system` is `OK (<n> categories: …)` when the bridge could load
    its `animation` context, else `FAILED - context files not loading`.
  - `tool_summary` counts the plugin's tools in six buckets by name pattern
    (`blueprint`, `animation`, `asset`, `task_queue`, `actor`, `utility`). It
    is a rough count, not a tool list.
  - `message` is `Unreal Editor connected. All tools operational.` That means
    only that every tool has a description; no tool was called. Otherwise
    `broken_tools` lists each tool without one (`name`, `issue`), and the
    message becomes `Unreal Editor connected. <n> tool(s) have issues.`
- **Plugin not reachable.** The result is an error whose text is JSON:
  `connected: false`, `reason` and a `message` asking to start the editor
  with the plugin. `reason` is `timeout`, `HTTP <code>` or the network
  error's own text. The bridge waits up to 30 s for the plugin
  (`MCP_REQUEST_TIMEOUT_MS`) before it says `timeout`.
- **The tool list tells the same story.** While the plugin is unreachable,
  `unreal_status` is the only tool the bridge lists, and its description ends
  in `Currently: NOT CONNECTED. …`. Once reachable, the description reads
  `Currently: CONNECTED to <project> (<engine>)`. Both are from the moment
  the list was fetched; call the tool for the current state.
