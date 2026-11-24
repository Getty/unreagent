# Python inside the editor: when a call fails

"Failure" is a heading of the skill itself, under "What comes back".

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Script execution queued. Task ID: …` | normal. The script has not run yet | poll that id (skill `ue-tasks`) |
| `Error: Script execution denied by user` | someone clicked Deny or closed the window. In an editor started with `-unattended`, unreagent's default, whether the engine can also end the window this way is unverified | ask before you try again |
| `Error: Task execution timed out after 600 seconds`, or no answer for 10 minutes | nobody approved the script, or it ran too long | ask whether anyone is at the editor. The timeout does not withdraw the call, so an approval given later may still run the script |
| `Error: Script MUST include @Description …` | no description in either place | add `# @Description: …` or pass `description` |
| `Error: Python script execution failed` | the output matched one of the scanned words | read the editor log, then "Failure" in the skill itself |
| `Python script executed (actors created: 0)` plus the warning line | success. The plugin warns about every script that spawns nothing | judge by your own output |
| `Executed <n> console commands` for a Python script | `script_type` was misspelt and the script ran as console commands | use `python` |
| `Error: Editor not available` or `Error: No active world` | no editor world to run against | skill `ue-levels`, or wait for the editor (skill `unreagent-iterate`) |
| `Error: Task not found: <id>` | the result is more than 5 minutes old, or the editor restarted | `get_script_history` shows whether the script ran |
| `ModuleNotFoundError: No module named 'unreal'` from `run_python` | you ran editor code on the host | use `execute_script` |
