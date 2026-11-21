---
name: ue-python
description: >
  Run Python inside the Unreal Editor through the UE LLM Toolkit's
  execute_script, and read or clear its script history. Use only when no
  dedicated toolkit tool does the job, before the first execute_script call,
  or when a script's result is unclear. Python on the host is unreagent-runtimes.
toolkit_tools: [execute_script, cleanup_scripts, get_script_history]
---

# Python inside the editor

`execute_script` hands a script to the UE LLM Toolkit plugin, which runs it in
the editor process. It is the plugin's way in for anything its dedicated tools
do not cover. Make sure you have the right tool before you write the script:

| You want to | Use |
|---|---|
| spawn, move or edit actors, open levels, find or save assets | the dedicated tools (skills `ue-level-actors`, `ue-levels`, `ue-assets`). They are faster and need no approval |
| run one console command | `run_console_command` |
| run editor-side Python for what no dedicated tool does | `execute_script` with `script_type: "python"`, this skill |
| parse logs, transform files, run a helper on the host | unreagent's `run_python` / `run_node` (skill `unreagent-runtimes`). They never reach the editor |

`cleanup_scripts` and `get_script_history` work on the files and the history
that `execute_script` leaves behind.

This skill was written from the toolkit's C++ and bridge source. It has not
been exercised against a running editor. It covers how a script gets run and
how to read what comes back, not the editor's Python API.

## Before the first call

- **A person approves every script.** Before anything runs, the plugin has
  the engine open a modal window titled `Execute PYTHON Script?`. It shows the
  description and the first 2000 characters of the script, with Allow and
  Deny buttons. Only Allow runs the script. Deny, or closing the window, ends
  it with `Script execution denied by user`. The plugin has no setting that
  skips this step. In an editor started without `-unattended`, a script that
  nobody approves waits 10 minutes and then fails. unreagent starts the editor
  with `-unattended` by default. How the engine handles the window then is
  unverified, and the plugin does not decide it.
- **The editor needs its Python plugin.** The tool description says so. What
  a script does without it is unverified.
- **Tool names.** The bridge lists the tools as `unreal_execute_script`,
  `unreal_cleanup_scripts` and `unreal_get_script_history`. Your MCP client
  may put its own server prefix in front of that.
- **The editor world.** A script runs on the game thread, against the level
  open in the editor. It fails with `Editor not available` or `No active
  world` when there is none. While it runs, and while the approval window is
  open, other toolkit calls probably wait, so send none you do not need
  (skill `ue-tasks`, "A 30 s timeout can crash the editor").
- **Nothing is saved.** What a script changes stays unsaved in the editor
  until something saves it, such as `asset save_all` (skill `ue-assets`).
  An editor restart drops unsaved changes.

## The call

| Parameter | Required | Meaning |
|---|---|---|
| `script_type` | yes | `python` here. The others are under "Other script types". Case does not matter |
| `script_content` | yes | the script |
| `description` | no | one line for the approval window, the history and the file name. Without it the script must carry its own |

The script needs a description. The plugin looks for the first
`@Description:` anywhere in `script_content` and takes the rest of that line.
In Python, put it in a comment:

```python
# @Description: List the static meshes under /Game/Props
```

The header in the tool's own description and error message is `/** … */`,
which is C++ comment syntax and a syntax error in Python. Use `#`, or pass
`description`. With neither, the script fails with `Script MUST include
@Description in header comment, or provide 'description' parameter.`

Keep the script short and plain. The person deciding on Allow sees only its
first 2000 characters.

## What comes back

`execute_script` does not wait for the script. It queues the script as a task
of its own with a 10-minute limit, and returns at once:

```
Script execution queued. Task ID: <id>. Poll task_status('<id>') for progress.
```

**Poll the id in that message.** It is also `data.task_id` in the JSON. The
`task_id` at the top of that JSON belongs to the bridge's task for the
`execute_script` call itself, which has already finished. Then:

1. `task_status` `{"task_id": "<id>"}` until the status is no longer
   `pending` or `running`. A poll can time out while the approval window is
   open or the script runs, and that timeout may crash the editor. Poll
   seldom, and after a timeout follow skill `ue-tasks`, "A 30 s timeout can
   crash the editor", before you poll again.
2. `task_result` `{"task_id": "<id>"}` within 5 minutes of the end, or the
   result is gone.

Every check on the arguments runs inside that task, so a missing description
or an empty editor world appears only in `task_result`. Skill `ue-tasks` covers
polling, timeouts and what `task_cancel` does not do.

### Success

The message is `Python script executed (actors created: <n>)`, where `<n>` is
the change in the number of actors in the editor world. It is negative when a
script removes actors. The script's own output is `data.output` in the
`task_result` JSON. It holds everything written to the editor log while the
script ran, not only your script's lines.

`output` ends with one line the plugin appends. When `<n>` is above 0, the line
is `[<n> new actors added to level]`. Otherwise it is `[WARNING: Script
reported success but no new actors were created in the level. The script may
have failed silently.]` The plugin appends that warning to every successful
script that does not spawn actors. For any other script, ignore it, and judge
by your own output.

### Failure

The plugin does not read Python's own outcome. It scans the captured output
for any of these strings, without regard to case: `Traceback`, `Error:`,
`Exception:`, `SyntaxError`, `IndentationError`, `NameError`, `TypeError`,
`ValueError`, `KeyError`, `ImportError`, `ModuleNotFoundError`,
`AttributeError`, `RuntimeError`, `FileNotFoundError`. One match makes the
result `Python script execution failed`. That cuts both ways:

- **A working script can fail.** It fails if it prints one of those words, or
  if anything else in the editor logs one while it runs.
- **A broken script can succeed.** It succeeds if it catches its own exception
  and prints none of them.

On a failure, the bridge passes on only `Error: Python script execution
failed`. The traceback the plugin collected does not reach you. Read it from
the editor log: the plugin's `get_output_log` (parameters such as `lines`,
`filter` and `since`), or unreagent's `logs` (skill `unreagent-iterate`).
Python tracebacks reach the editor log through the engine, which this skill
assumes but has not verified.

### Reporting from a script

- Make the script print one line you can find, such as a fixed marker
  followed by JSON (`print("RESULT " + json.dumps(result))`), and read that
  line from `data.output`.
- Never print an exception's text or class name to say something went
  wrong. That text is likely to contain a scanned word, which turns the result
  into a failure and hides your output. Print your own marker, such as
  `RESULT_FAILED`, and keep the details out of the message.
- Print little. Everything the editor logs during the run comes back too.

## Files and history

Each Python script is written to a file before it runs, and that file stays:

| What | Where |
|---|---|
| Python scripts | `Content/UnrealClaude/Scripts/` in the project, named after the first 30 characters of the description plus a counter, such as `List_the_static_meshes_under___003.py` |
| C++ scripts | `Source/<ProjectName>/Generated/UnrealClaude/` |
| history | `Saved/UnrealClaude/script_history.json`, the last 100 scripts |

The counter starts again with each editor session, so a new session can
overwrite an older session's file of the same name.

**get_script_history** (`count`, default 10, clamped to 1–50) returns
`scripts[]`, newest first. Each entry has `type`, `filename`, `description`,
`success`, `result` (the message, cut to 200 characters. A failed C++ script
gets `Compilation failed: ` plus up to 200 characters of errors) and `timestamp`
(UTC). It also returns `count` and `formatted_context`, the same list as text.
It never returns the script itself. The history is kept on disk, so it
includes earlier editor sessions, although the tool description says "in this
session". A denied script, or one that failed an argument check, is not
recorded.

**cleanup_scripts** takes no parameters. It deletes every file under both
script directories, removes the directories, and empties the history. It
answers `Cleanup complete: Deleted <n> files, cleared <m> history entries`.
Its description names `Source/UnrealClaude/Generated/` and leaves out the
Python directory. The code deletes the two directories in the table above. It
cannot be undone, and the project's version control may already track those
files.

## Other script types

| `script_type` | What happens |
|---|---|
| `console` | one console command per line. Blank lines, `#` and `//` lines, and the `@Description:` line are skipped. Each line passes the same check as `run_console_command`. A blocked line becomes `Skipped blocked command: …` in the output, and the result is still `Executed <n> console commands` |
| `cpp` | written to the C++ directory above. The plugin then starts a Live Coding compile and waits up to 60 s. Whether that compile builds a newly added `.cpp` file is up to the engine and unverified. The plugin never calls the compiled code: success means only `C++ script compiled successfully via Live Coding`. A failure in the compile step starts with `Compilation failed. Fix these errors and call execute_script again:`, followed by the reason: up to five compile errors, `Live Coding is not enabled. Press Ctrl+Alt+F11 to enable.`, `Live Coding module not loaded`, or `Live Coding compilation timed out (60s)`. Two failures come without that prefix: `Failed to write C++ script file`, and `Live Coding not available - C++ scripts cannot be compiled` in an editor built without Live Coding. The tool description promises automatic retries. The code has none |
| `editor_utility` | not implemented. It still asks for approval, then fails with `Editor Utility script execution not yet implemented.` |
| anything else | runs as `console`, without a warning. Check the spelling |

## Typical sequences

**Run a script and read its result**

1. Check that no dedicated tool covers the job.
2. `execute_script` `{"script_type": "python", "script_content": "# @Description: …\n…"}`.
3. Take the id from the message. Ask the person at the editor to approve the
   script.
4. `task_status` with that id until it has ended, then `task_result`.
5. Find your marker line in `data.output`. Read the editor's state back if
   the script changed it.

**After `Python script execution failed`**

1. `get_output_log` `{"lines": 200, "strip_timestamps": true}`, or `logs`, to
   read the traceback and any other line that matched.
2. If the scan matched a word your script printed itself, change what it prints.
3. Fix the script and run it again. Each run asks for approval again.

**Before handing the project back**

`get_script_history` to see what ran, then `cleanup_scripts` if the generated
files should not stay in the project. That is your decision to make with the
person, because the history goes with them.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Script execution queued. Task ID: …` | normal. The script has not run yet | poll that id (skill `ue-tasks`) |
| `Error: Script execution denied by user` | someone clicked Deny or closed the window. In an editor started with `-unattended`, unreagent's default, whether the engine can also end the window this way is unverified | ask before you try again |
| `Error: Task execution timed out after 600 seconds`, or no answer for 10 minutes | nobody approved the script, or it ran too long | ask whether anyone is at the editor. An approval given later can still run the script |
| `Error: Script MUST include @Description …` | no description in either place | add `# @Description: …` or pass `description` |
| `Error: Python script execution failed` | the output matched one of the scanned words | read the editor log, then the "Failure" section above |
| `Python script executed (actors created: 0)` plus the warning line | success. The plugin warns about every script that spawns nothing | judge by your own output |
| `Executed <n> console commands` for a Python script | `script_type` was misspelt and the script ran as console commands | use `python` |
| `Error: Editor not available` or `Error: No active world` | no editor world to run against | skill `ue-levels`, or wait for the editor (skill `unreagent-iterate`) |
| `Error: Task not found: <id>` | the result is more than 5 minutes old, or the editor restarted | `get_script_history` shows whether the script ran |
| `ModuleNotFoundError: No module named 'unreal'` from `run_python` | you ran editor code on the host | use `execute_script` |

## See also

- `ue-tasks`: polling, result lifetime, cancellation.
- `unreagent-runtimes`: Python and Node on the host, outside the editor.
- `ue-level-actors`, `ue-levels`, `ue-assets`: the dedicated tools to try
  first, and the read tools for checking what a script changed.
- `ue-context`: the plugin's reference notes. None of them covers Python.
- `unreagent-iterate`: `logs`, and restarting the editor.
