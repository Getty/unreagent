# The server: start, sessions, settings

Paths are relative to `Engine/Plugins/Experimental/ModelContextProtocol/`
`Source/` unless they name another plugin. Changing anything here — the
`.uproject`, the editor's ini files, unreagent's config — is the user's call;
this section is for explaining a problem and naming the fix.

## Starting it

- **Enabling the plugin does not start the server.** It starts only with the
  editor flag `-ModelContextProtocolStartServer` (the older
  `-StartModelContextProtocolServer` still works, with a deprecation warning)
  or the setting `bAutoStartServer`, default false and ignored for
  commandlets (`ModelContextProtocolEngine/Private/`
  `ModelContextProtocolSettings.cpp:32-61`).
- **Port 8000, path `/mcp`**: `http://127.0.0.1:8000/mcp`. The port comes
  from `-ModelContextProtocolPort=N` (1–65535, anything else falls back with
  a warning), else the setting `ServerPortNumber`
  (`ModelContextProtocolSettings.cpp:17-30`; `Public/`
  `ModelContextProtocolSettings.h:23-46`).
- **The plugins**: `ModelContextProtocol` and `AllToolsets` in the
  `.uproject`'s `Plugins` array; both are `EnabledByDefault: false`
  (measured). Enabling them needed no rebuild of a C++ project (measured).
- **Through unreagent** — the config key `unreal.mcp`, `{enabled, port}`,
  off by default, port 8000, server name `unreal-mcp`:
  - It needs `mcp.enabled`; without it, or with an `mcp.extraServers`
    entry named `unreal-mcp`, or with a port outside 1–65535 or equal to the
    launcher's own MCP port, the config does not load.
  - The launcher adds `-ModelContextProtocolStartServer` and
    `-ModelContextProtocolPort=N` to the editor's command line, unless
    `unreal.args` already carries them. The only port flag `unreal.args`
    may carry is exactly `-ModelContextProtocolPort=<unreal.mcp.port>`;
    any other mention of it is a load error.
  - It writes the http entry `unreal-mcp` (`http://127.0.0.1:<port>/mcp`)
    into the agent's MCP config and into every `mcp.writeConfig` file.
  - **The agent's first start waits** until the server answers a JSON-RPC
    ping — at most 5 minutes, then the agent starts anyway with a WARN. If
    the editor fails or exits and is not restarted, the wait ends at once,
    with a WARN, and the agent starts. Only the first start waits; a later
    agent restart does not. There is no wait with `unreal.manualStart` or
    without `agent.claudeIntegration`; then the launcher only reports when
    the server answers.

## When it is ready

- **An open port is not a ready server.** Measured on the first start with
  the new plugins: TCP accepted on 8000 at 32.5 s, pings timed out until
  44.1 s, then `ping` and `initialize` answered. Warm starts: about 16 s.
  The toolkit's port 3000 answered at the same moment — both wait for the
  editor's first game-thread tick.
- `ping` works without a session; `GET /mcp` is 405 (source,
  `ModelContextProtocolServer.cpp:1117-1127`).
- `initialize` returns empty `serverInfo` name, title and version
  (measured).
- The editor log shows `LogModelContextProtocol: Starting MCP server on port
  N`, then `Tool search enabled: registered 3 meta-tools (N toolsets
  discoverable via list_toolsets)` with N growing as toolsets register
  (`ModelContextProtocolServer.cpp:445`;
  `ModelContextProtocolEditor/Private/`
  `ModelContextProtocolToolsetRegistryAdapter.cpp:189`). Each call logs
  `Dispatching toolset tool: '<toolset>.<tool>'` (`…Adapter.cpp:256`).

## Sessions and editor restarts

- Every request but `initialize` and `ping` needs the `Mcp-Session-Id` header:
  missing is 400, unknown is 404 `Unknown session id '…' for '…'; client
  should reinitialize` (`ModelContextProtocol/Private/`
  `ModelContextProtocolServer.cpp:580-593`). Sessions live in editor memory;
  an editor restart drops them.
- **Restart while the agent runs: fine.** Claude Code got the 404,
  reinitialized in 31 ms and completed the same call; the model saw no error
  (measured, Claude Code 2.1.330).
- **Editor down when the agent starts: the server stays failed.** Claude
  Code tried 4 times in about 6 s, logged that `unreal-mcp` still failed, and
  never tried again; three turns later, with the editor up, it still had no
  `mcp__unreal-mcp__*` tool (measured). A headless session stays without
  Unreal MCP; an interactive one gets it back with `/mcp`. If you are such a
  session and the tools are missing although the editor runs, say so in your
  report instead of working around it. With `unreal.mcp`, the launcher's
  hold on the agent's first start (above) prevents this, unless that wait
  ran out or the editor ended first.

## Access

Loopback only by default, **no authentication**. A request without an
`Origin` header is accepted; a browser `Origin` must be a localhost variant
(`ModelContextProtocolServer.cpp:69-90`). The log warns on start that data
sent to an LLM through the plugin is Licensed Technology under the UE EULA
(`ModelContextProtocolServer.cpp:446`).

## Hiding toolsets — `BlockedNames` / `AllowedNames`

In `Config/DefaultEditorPerProjectUserSettings.ini`, section
`[/Script/ToolsetRegistry.ToolsetRegistrySettings]`
(`ToolsetRegistry/Source/ToolsetRegistry/Public/ToolsetRegistry/`
`ToolsetRegistrySubsystem.h:21-60`):

| Key | Effect |
|---|---|
| `+BlockedNames=<pattern>` | toolsets and tools whose name matches are hidden |
| `+AllowedNames=<pattern>` | when any is set, only matching toolsets and tools are visible |
| `+AgentSkillBlockedNames=` / `+AgentSkillAllowedNames=` | the same for agent skill paths (section `agent-skills`) |
| `+SetObjectPropertiesBlockedClasses=` | classes whose objects the registry's property setter refuses |

A pattern is a case-insensitive substring; one in slashes (`/^Fake.*/`) is a
regular expression; block wins over allow. Measured with
`+BlockedNames=GameplayTagsToolset`: the toolset vanished from
`list_toolsets`; `describe_toolset` answered isError `Toolset
'GameplayTagsToolset.GameplayTagsToolset' not found. Available toolsets: …`;
`call_tool` answered isError `Toolset not found`. So "not found" for a
toolset this skill lists can mean the project blocked it — check that file
before you report the toolset missing.

## Tool-search mode off — `bEnableToolSearch=False`

In `Config/DefaultEditorPerProjectUserSettings.ini`, section
`[/Script/ModelContextProtocolEngine.ModelContextProtocolSettings]`
(measured path; the setting: `ModelContextProtocolSettings.h:44-46`). Off,
every toolset tool registers as a native MCP tool named
`<toolset>.<tool>` (`…Adapter.cpp:191-200`) and the three meta-tools are
gone. Measured: 852 tools in one `tools/list` of 1.13 MB; Claude Code took
all of them, rewriting dots to underscores
(`mcp__unreal-mcp__EditorToolset_EditorAppToolset_IsPIERunning`, the
longest 118 characters) and deferring their schemas. Calls by those names
work. The default (on) keeps the context small; leave it unless the user
wants per-tool permission rules, which only the native names allow.

## `ModelContextProtocol.GenerateClientConfig`

An editor console command (not a tool) that writes a client config
(`ModelContextProtocolEngine/Private/`
`ModelContextProtocolEngineModule.cpp:40-70`): `ClaudeCode` writes
`.mcp.json` with `{"mcpServers": {"unreal-mcp": {"type": "http", "url":
"http://127.0.0.1:8000/mcp"}}}` (measured) into the project directory on an
installed engine, the workspace root on a source build
(`ModelContextProtocolClientConfig.cpp:159-165`), keeping the file's other
entries. **In an unreagent project it does not stick**: when unreagent's
`mcp.writeConfig` targets the same `.mcp.json`, the next launcher start
replaces the file whole and the `unreal-mcp` entry is gone (measured, with
`unreal.mcp` off). With `unreal.mcp` on, unreagent writes its own
`unreal-mcp` entry into every `mcp.writeConfig` file on each start. Do not
run the command there; the entry belongs in unreagent's config:
`unreal.mcp`, or by hand in `mcp.extraServers` when the switch is off.
