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
how to read what comes back; section `editor-api` has what Epic documents
of the editor's Python API.

This skill has four sections:

| Section | Holds | Load it |
|---|---|---|
| `editor-api` | the editor's `unreal` module as Epic documents it (each call cited, none run): undo transactions, progress dialogs, the asset and actor calls; how to go about a bulk edit; the script contents that trip the toolkit's error scan | before you write the script itself, above all one that changes many assets or actors |
| `script-types` | the `script_type` values other than `python` (`console`, `cpp`, `editor_utility`): what each does, its results and its errors | before you send a script of one of these types |
| `files-history` | where the script files and the history are kept and how the files are named; the parameter and the result fields of `get_script_history`; what `cleanup_scripts` deletes and answers | when you look for a script's file or the history on disk, want more history entries than `get_script_history` returns by default, or a field of its result needs explaining |
| `failures` | the failure table (what you see, what it means, what to do) | when a call or a `task_result` returns an error, no answer comes for 10 minutes, or a result is not what you expect |

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
| `script_type` | yes | `python` here. The others are in section `script-types`. Case does not matter |
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

Each Python script is written to a file under
`Content/UnrealClaude/Scripts/` in the project before it runs, and that file
stays. `get_script_history` lists what ran; `cleanup_scripts` removes the
files and the history. Load section `files-history` when you look for a
script's file or the history on disk, want more history entries than
`get_script_history` returns by default, or a field of its result needs
explaining. What you must know without it:

- **A new editor session can overwrite an older session's file of the same
  name**: the counter in the file name starts again with each session.
- **`cleanup_scripts` cannot be undone.** It deletes every file under both
  script directories, the Python one and the C++ one, removes the
  directories and empties the history, and the project's version control may
  already track those files.
- **The history is kept on disk, so it includes earlier editor sessions.** A
  denied script, or one that failed an argument check, is not recorded.

## Other script types

`script_type` also takes `console` (one console command per line), `cpp`
(written to a file and compiled through Live Coding; the plugin never calls
the compiled code) and `editor_utility` (not implemented: it still asks for
approval, then fails). Load section `script-types` before you send a script
of one of these types: it has what each does, its results and its errors.

**Any other value runs as `console`, without a warning.** Check the spelling.

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

**A bulk edit of assets or actors**

1. Load section `editor-api`. Read the current state with the dedicated tools.
2. A dry run: the script filters and prints a count, changes nothing.
3. The real run: the same filter, the changes inside one
   `unreal.ScopedEditorTransaction`, one `RESULT` line with the count.
4. Save (`asset` `{"operation": "save_all"}`, skill `ue-assets`), then read
   the state back.

**Before handing the project back**

`get_script_history` to see what ran, then `cleanup_scripts` if the generated
files should not stay in the project. That is your decision to make with the
person, because the history goes with them.

## When a call fails

Load section `failures` when a call or a `task_result` returns an error, no
answer comes for 10 minutes, or a result is not what you expect: a table from
what you see to what it means and what to do.

**A timeout does not withdraw the call.** After `Error: Task execution timed
out after 600 seconds`, or no answer for 10 minutes, an approval given later
may still run the script.

## See also

- `ue-tasks`: polling, result lifetime, cancellation.
- `unreagent-runtimes`: Python and Node on the host, outside the editor.
- `ue-level-actors`, `ue-levels`, `ue-assets`: the dedicated tools to try
  first, and the read tools for checking what a script changed.
- `ue-context`: the plugin's reference notes. None of them covers Python.
- `unreagent-iterate`: `logs`, and restarting the editor.
