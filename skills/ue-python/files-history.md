# Python inside the editor: script files and history

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
