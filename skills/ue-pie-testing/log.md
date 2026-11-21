# get_output_log

`get_output_log` reads the editor's log file on disk and returns its last
lines, filtered. It is the plugin's way to see what a call or a PIE run wrote
to the log. It needs the editor up and the plugin answering; for an editor
that is starting, hung or gone, use unreagent's `logs` and `read_file`
instead (the main skill, "Which log to read").

## Which file

The tool takes the first of these that exists:

1. `<project>/Saved/Logs/<ProjectName>.log`
2. `<project>/Saved/Logs/UnrealEditor.log`
3. any `.log` file in `<project>/Saved/Logs/`, the first one found
4. `<engine>/Saved/Logs/UnrealEditor.log`
5. any `.log` file in `<engine>/Saved/Logs/`

The result's `log_file` names the file it read. Check it: steps 3 and 5 can
pick an older log, such as a backup of an earlier session. When none exists,
the call fails with
`No log file found. Searched paths: <paths>. Also scanned directories: <project log dir>, <engine log dir>`.

The engine writes the file, not the plugin. How soon a line written in the
editor reaches the file is up to the engine and has not been verified, so the
newest lines can be missing for a moment.

## Parameters

| Parameter | Default | Does |
|---|---|---|
| `lines` | 100 | how many lines to return, from the end, after filtering. Limited to 1 to 1000, so 0 gives 1 |
| `filter` | — | keep lines containing this text, ignoring case |
| `categories` | — | keep only these categories, ignoring case. An array of strings |
| `exclude_categories` | — | drop these categories. Ignored when `categories` is given |
| `min_verbosity` | — | keep this level and above |
| `compact` | false | collapse repeated messages |
| `strip_timestamps` | false | drop the `[time][frame]` prefix |
| `since` | false | read only what was added since the last read ("The cursor") |

`categories` and `exclude_categories` must be arrays: a plain string in their
place is ignored. `min_verbosity` takes `VeryVerbose`, `Verbose`, `Log`,
`Display`, `Warning`, `Error` or `Fatal`, ignoring case, although the tool's
description lists only the last four. `Log` and `Display` are the same level,
so `min_verbosity: "Display"` keeps `Log` lines too. Another value fails with
`Invalid min_verbosity '<value>'. Valid values: VeryVerbose, Verbose, Log, Display, Warning, Error, Fatal`.

## How lines are filtered

A log line such as `[2026.11.21-09.15.02:123][ 42]LogTemp: Warning: Text`
counts as structured: a `[…][…]` prefix, a category, an optional verbosity,
then the message. A line without a verbosity counts as `Log`. Every other
line, such as the second line of a call stack or a line written before the
log had timestamps, is not tested on its own: it is kept or dropped together
with the structured line before it.

The filters run in this order: `filter` on the whole line, then the
categories, then `min_verbosity`, then `compact`, and only then `lines` takes
the last N of what is left.

**`compact`** merges each run of structured lines that carry the same message
text into the first one, with ` (x<count>)` appended. Only the message is
compared, not the category or the level, and the repeated lines' own
continuation lines are dropped.

**`strip_timestamps`** turns a structured line into `Category: Message`, or
`Category: Verbosity: Message` when the level is not `Log`.

## The result

| Field | Present | Holds |
|---|---|---|
| `log_file` | always | the file read |
| `total_lines` | always | lines read before any filter |
| `returned_lines` | always | lines in `content` |
| `cursor` | always | the read position in bytes, after this call |
| `content` | always | the lines, joined by newlines |
| `filter`, `filtered_lines` | with `filter` | the text, and how many lines passed it |
| `since_offset`, `new_bytes` | with `since` | where this read started, and how many bytes it found |
| `categories` | with `categories` | as given |
| `exclude_categories` | with `exclude_categories` and no `categories` | as given |
| `min_verbosity` | with `min_verbosity` | as given |
| `compact`, `duplicates_collapsed` | with `compact` | `true`, and how many lines were merged |

The message is `Retrieved <n> log lines from <file name>`. A `since` read that
finds nothing new returns `No new log content since last read` or
`No complete new lines since last read`, with `total_lines` and
`returned_lines` 0, `cursor`, `since_offset`, `new_bytes` and an empty
`content`.

Other errors: `Failed to read log file: <path>`,
`Cannot stat log file: <path>`,
`Failed to open log file for incremental read: <path>`.

## The cursor

The plugin keeps one read position in the log file, in editor memory:

- **Every read moves it to the end** of what was read, with or without
  `since`. A plain read in between therefore hides from the next `since` read
  everything up to that point.
- **It is shared** by every caller of the plugin, so another agent's read
  moves your cursor.
- **It starts at 0** when the editor starts, when the log file changes, or
  when the file got shorter. The first `since` read after that returns the
  last `lines` of the whole file.
- **It counts bytes**, and keeps only complete lines.
- **From the code, a `since` read after the first one drops the first new
  line.** The plugin skips everything up to the first line break, to discard a
  half-written line, but the cursor already sits at the start of a line, so a
  whole line is lost. This has not been verified against an editor. Do not
  rely on `since` for a line that must not be missed, such as a single error
  or a `[PIE-DBG]` event.

The plugin's notes give the safer pattern for a finished run: read the file
afterwards. A plain read with `filter` gets the last matching lines,
`[PIE-DBG]` for a run or the monitor. For more than 1000 lines, or a pattern a
substring cannot express, read the file itself with `read_file` and a
negative `offset` (skill `unreagent-files`), or search it with `run_python`
(skill `unreagent-runtimes`).

## Useful filters

- `filter: "[PIE-DBG]"`: the lines of `run_sequence` and the monitor.
- `filter: "PIESequenceRunner"`: a run's own progress and warnings, such as
  a step that found no player controller.
- `categories: ["LogUnrealClaude"]`: everything the plugin logged.
- `min_verbosity: "Warning"` with `compact: true` and
  `strip_timestamps: true`: a short list of what went wrong.
- `exclude_categories: ["LogStreaming", "LogLinker"]`: the plugin's notes name
  these as noisy.
