# unreagent

This project runs under the unreagent launcher. The launcher writes this file
and replaces it on every start: project rules belong in `CLAUDE.md`, not here.

- The launcher runs the Unreal Editor as a supervised process. Start, stop and
  restart it only with the launcher's tools (`ue_start`, `ue_stop`,
  `ue_restart`), never by starting or killing the editor yourself. A stop or
  restart is a kill and saves nothing, so save first. Skill
  `unreagent-iterate`.
- Compile and package with the launcher's `run_command`. Skill
  `unreagent-build`.
- The launcher ships skills for the editor's tools and its own. They are files
  under `.claude/skills/`, in directories starting with `unreagent-`, and the
  launcher's `skills` MCP tool serves the same text by name. Read the skill
  before the first call of a tool it covers.

## Agents

The main session hands a task to the agent whose line fits: that agent starts
with the matching skills loaded. A request that spans several topics is split
by the main session, one part per agent. An agent working on such a task does
it and reports back.

{{agents}}
