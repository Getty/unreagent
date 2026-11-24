# PIE testing: when a call fails

"Before the first call", the checks of `run_console_command` and "Which log
to read" are in the skill itself. How a scripted run ends is in section
`sequences`.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| `PIE is not running. Call start_pie first.` | no PIE session, or it has not started yet | `start_pie`, then poll `pie_status` |
| `pie_status` stays `running: false` after `start_pie` | the editor has not started the session | poll again, then look for errors in the output log |
| the game barely moves | probably the editor window is not in front (the plugin's notes) | ask the user to bring it to the front, or use `run_sequence` |
| `Command '<entry>' is blocked for safety` | the command begins with a blocked entry | use the dedicated tool, or ask the user to run it |
| `run_console_command` succeeds and nothing changes | the engine did not accept the command, or it did not reach PIE | check the log; in a run, use a `console` step |
| `A sequence is already running. …` | a run is in progress, or is still waiting for PIE | `sequence_status`; section `sequences` |
| a parameter seems to have no effect | its name is wrong, and queued calls do not warn | check the name, especially `action` versus `action_path` |
| two agents' tests interfere | the plugin's state is shared | test one at a time |
| everything is gone after an editor restart | the state lived in editor memory | start again |
