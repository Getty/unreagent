# `logs` in full, and the launcher log file

## The `logs` tool

- `service` (string, default `"ue"`): `ue`, or `agent` when the launcher runs
  its own agent. Any other name returns `Error: unknown service "<name>"`.
  `run_command` commands are not services. Their output exists only in the
  result of the call that ran them.
- `lines` (integer, default 50). An integer string such as `"200"` counts as
  200. Missing, `null` or `""` gives 50. `0`, a negative integer, or more lines
  than are buffered returns the whole buffer. Any other value, such as `1.5`,
  `"all"` or `true`, returns `Error: 'lines' must be an integer, got …`; a
  whole number past the 64-bit range, such as `1e20`, returns
  `Error: 'lines' must be an integer between -9223372036854775808 and 9223372036854775807, got 1e+20`.

The result is the lines joined by newlines: no header, no timestamps. An empty
result means nothing is buffered. Once the buffer has dropped older lines, a
request for more than it holds, or for all of it (`0` or negative), starts with
`[... N lines omitted; the log keeps only the last 500]`. N is how many lines
it has dropped since the launcher started, restarts included; a larger `lines`
does not bring them back.

A reply holds at most 48 KiB. When the lines you asked for are longer, their
oldest bytes go, and `[... B bytes omitted; a reply holds at most 48 KiB]`
comes first, or right below the lines marker. The first line you see can then
start mid-line, and a single line longer than 48 KiB comes back as its end
only. No `lines` value reaches the cut bytes, since every reply ends at the
newest line. Read them in the launcher log or the editor's log file with
`read_file` (below).

What the buffer holds is in the main skill, "logs".

## The launcher log file

The main skill, "Where the output is", has the table of the three sources.

When the launcher runs its own agent in the foreground (the default), it writes
its log to `unreagent.log` next to the `.uproject`. Such a run, once it has
loaded its config and bound the MCP port, first renames the existing
`unreagent.log` to `unreagent.log.1`, replacing the one there, and then begins
`unreagent.log` empty. One earlier log is kept; older ones are gone. A restart
of the editor or the agent within one launcher run does not rotate the log.
When the rename fails, the new run is appended to `unreagent.log` and
`unreagent.log.1` stays as it was. The warning that says so goes to the
launcher's console before the agent takes it over, not into the log. Every
launcher run opens its part of the file with a line that reads
`unreagent <version> — Config: <path>` after the timestamp, so more than one
such line in `unreagent.log` marks a failed rename; this run starts at the
last one. Without a foreground agent (an external agent, as with `-no-agent`,
or headless), the log goes to the launcher's console, which no tool can read,
and neither file is touched: such a run leaves no log file, and an
`unreagent.log` on disk is an earlier run's. Each line starts with a
`HH:MM:SS` timestamp, with no date. Lines to look for:

```
[ue] started (pid 12345)
[ue] exited (exit 3)
[ue] restart 1 in 3s
[ue] no automatic restart (policy=on-failure)
[ue] failed 3 times in a row, each within 10s of its start — giving up (no automatic restart)
[ue] failed 3 times in a row, each within 5m0s of its start — giving up (no automatic restart)
The editor failed and is not restarted. Any crash dumps are in <project dir>\Saved\Crashes.
[ue] start failed: <error>
[ue] program not found — no further attempt (check path / installation)
[ue] stopped (manual)
[ue] restarted (manual)
```

`restarted (manual)` is logged only when `ue_restart` spawned a new editor. A
start that failed logs `start failed: …` instead.

`read_file` (`unreagent-files`) returns at most 47 KiB per call, and a negative
`offset` counts from the end of the file: `offset: -48128` returns the last
47 KiB of `unreagent.log` or of the editor's log file, where a crash is.
`unreagent.log.1` holds the last earlier run that had a foreground agent and
got as far as opening the log. That is not always the run right before this
one: a run without a foreground agent rotates nothing, nor does one that ended
on a config error or on an MCP port already taken. After such a run,
`unreagent.log.1` holds an older run, and with no date on the timestamps the
file does not show it. A crash from the run in `unreagent.log.1` is at the
file's end: read it the same way. After a failed rename, the earlier run sits
in `unreagent.log` instead, just above the last `Config:` line. Both files sit
next to the `.uproject`, in the default `files.root`, and `read_file` exists
only while file access is enabled. To search a long log, use `run_python` /
`run_node` (`unreagent-runtimes`).
