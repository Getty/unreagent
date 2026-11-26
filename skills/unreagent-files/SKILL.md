---
name: unreagent-files
description: >
  Read, list, write and edit files under the UE project root through the
  launcher's MCP file tools. Use when working on project files without local
  filesystem access, or when read_file / list_dir / write_file / edit_file
  errors out or is missing from the tool list.
---

# unreagent file tools

The launcher can expose four MCP tools — `read_file`, `list_dir`, `write_file`,
`edit_file` — that operate on files under one configured root on the machine
running the launcher, normally the UE project directory. They exist so an agent
with no local filesystem access (connected over MCP from another machine, or
running while nobody is at the keyboard) can still work on the project.

They are **off by default** and they are **strictly confined to one root**.
Both facts change what you should do when a call fails, so read Availability and
Root confinement before concluding that a path is wrong.

These tools see files on disk. They do not see anything the running editor holds
in memory, and they are the wrong instrument for `.uasset` / `.umap` content —
see Do not hand-edit assets.

This skill has three sections:

| Section | Holds | Load it |
|---|---|---|
| `confinement` | every confinement rule in full with its error text (absolute paths, `..`, symlinks and junctions, link loops, reserved device names), what an unresolved `files.root` does, and the two fixes for a plugin linked in from outside the root | when a call returns a path error, when the root or a plugin lies behind a symlink or junction, or when `-files` started without the four tools |
| `read-ranges` | how `read_file` moves a range's edges to UTF-8 character boundaries, the one case that leaves a character cut, and an `offset` at or past the end of the file | when a page does not begin or end where you asked, when you read with a `limit` below 7 (the one case that leaves a character cut), or when an `offset` lies at or past the end of the file |
| `failures` | the failure table: each error text, what it means, what to do | when a call returns an error this file does not explain |

## Availability

The four tools are registered only when file access is enabled. When it is not,
they are not registered at all: they are absent from `tools/list`, and a call
returns the JSON-RPC error `-32602 unknown tool: read_file`. That message
means the feature is switched off in this project's configuration — not that
the path was rejected, and not that permission was denied.

| Configuration | Result |
|---|---|
| `files.enabled: false` (the default) | none of the four tools exist |
| `files.enabled: true` | all four exist |
| `files.enabled: true`, `files.readOnly: true` | only `read_file` and `list_dir` exist |
| `mcp.enabled: false` | no unreagent tools at all, file tools included |

Two failure modes that look alike but are not: with `enabled: false` nothing
works; with `readOnly: true` reads work normally and the two writing tools are
gone. In both cases the symptom is the same unknown-tool error, so check which
of `read_file` / `write_file` are present in `tools/list` to tell them apart.

Turning it on is the user's job, not something a tool call can do:

```yaml
files:
  enabled: false           # default; set to true
  root: "${PROJECT_DIR}"   # every path is confined to this
  readOnly: false          # true = read_file and list_dir only
```

The command line can also force it on for one run: `unreagent.exe -files`.
The related `-no-agent` flag does **not** enable file tools — it only keeps the
launcher from starting its own agent so an external one can connect. The two are
often used together (`-no-agent -files`), which is why they are easy to confuse.

When file tools come up, the launcher logs
`File tools active (read/write | read-only) under: <root>`. That line is the
authoritative statement of the mode, and of the root as configured: a
relative `files.root` is made absolute against the launcher's working
directory, and error messages quote that absolute form. File tools switched
on while `mcp.enabled` is false log
`WARN file tools not served: they need mcp.enabled` instead, and no tool
exists.

## Root confinement

Every path is confined to `files.root`, by default `${PROJECT_DIR}`, the
directory holding the `.uproject`. No path argument to these tools reaches
outside the root: not through `..`, not through an absolute path, not through
a symlink or junction. The rules in short:

- **Paths are relative to the root. Always.** An absolute path is accepted
  only under the root; any other is refused.
- **Escaping with `..` is refused** (`Error: path outside the allowed root`).
  Do not write a `..` that stays inside the root either
  (`Source/../Config/x.ini`): on a Linux host it is walked, not cleaned away,
  and can fail. Write `Config/x.ini` instead.
- **A link that leaves the root is not followed**: a symlink or junction
  whose target is absolute or leaves the root on the way, so a Windows
  junction never, and neither a relative link that climbs out of the root
  and back in. The link itself still shows up when you list its parent; only
  following it fails. Do not retry, and do not try an absolute spelling.
- **Windows reserved device names are refused** in any path component, on
  every host, also with an extension: `aux.h`, `NUL.txt` and
  `Source/CON/x.txt` are all refused.
- **An empty or omitted path is not a shortcut to the root** — that holds per
  tool, not in general. `list_dir` defaults to the root. `read_file`,
  `write_file` and `edit_file` reject an absent or empty `path` up front with
  `Error: 'path' missing`. Pass `path` explicitly to all three.

**Load section `confinement` when a call returns a path error** (`must be
relative to the project root`, `path outside the allowed root`, `a link on
the path …`, `too many levels of symbolic links`, `is a reserved Windows
device name`), when the root lies behind a symlink or junction, or when
`-files` was given and the four tools are missing: it has each rule in full
with its error text, and what an unresolved `files.root` does.

What the confinement does not cover: a hard link is not a link in this sense
but a second directory entry for the same file, so one inside the root reads and
writes that file wherever its other names live; on a Linux host a bind mount
inside the root is crossed as well.

### A plugin linked in from outside the root

A plugin linked into `<Project>/Plugins/` from a checkout elsewhere, with a
junction or a symlink, cannot be reached with the file tools: listing
`Plugins` still shows the entry, but every path through it fails with the
link error. Neither a retry nor an absolute spelling of the path gets
through. Tell the user; both fixes are theirs. Load section `confinement`,
"A plugin linked in from outside the root", for the two fixes before you
tell them.

## Tools

| Tool | Parameters | Returns |
|---|---|---|
| `read_file` | `path`, `offset` (optional), `limit` (optional) | file content as text, at most 47 KiB per call; a `[bytes X-Y of Z]` line follows unless it is the whole file |
| `list_dir` | `path` (optional) | one entry per line, directories with a trailing `/` |
| `write_file` | `path`, `content` | `written: <path> (<n> bytes)` |
| `edit_file` | `path`, `old_string`, `new_string` | `<n> occurrence(s) replaced in <path>` |

### read_file

- `path` (string, required) — relative to the root.
- `offset` (integer, optional, default 0) — the first byte to read. A negative
  offset counts from the end of the file, so `-48128` is the last 47 KiB; one
  that reaches before the start reads from byte 0.
- `limit` (integer, optional) — the most bytes to return. Default and maximum
  48128 (47 KiB), so a reply with its trailer line stays within 48 KiB; a
  larger value is clamped to it, and 0 or less is an error.

Both accept an integer string such as `"65536"`; missing, `null` or `""` gives
the default. Any other value, such as `1.5`, `"end"` or `true`, returns
`Error: 'offset' must be an integer, got …` (or `'limit'`). A whole number past
the 64-bit range, such as `1e20`, returns
`Error: 'offset' must be an integer between -9223372036854775808 and 9223372036854775807, got 1e+20`.

Returns the bytes of the range as text, nothing else: no line numbers, no
header. When the range is the whole file, that is all. Otherwise one line
follows, after a newline that is not part of the file:

```
[bytes X-Y of Z]
[bytes X-Y of Z; continue with offset=Y]
```

`X` is the first byte returned, `Y` the byte after the last one (exclusive), `Z`
the file size when the call opened it. No trailer line means you have the whole
file. The second form appears when the file goes on past `Y`; call again with
`offset=Y` to go on. Nothing is ever repeated. For valid UTF-8 read with a
`limit` of 4 or more, nothing is skipped either, unless a page was left with a
cut character (section `read-ranges`). Otherwise a page can begin up to 3 bytes after the
offset asked for: continuation bytes at its start are dropped, whether they are
stray (invalid UTF-8, binary) or the rest of a character the previous page cut.
The trailer's `X` shows where a page really begins, so an `X` larger than the
`offset` you passed marks the gap. A log that is still being written grows
between calls, so a later call can report a larger `Z`.

Range edges move inward to UTF-8 character boundaries, by up to 3 bytes, and
the trailer reports the range after the move. Load section `read-ranges` when
a page does not begin or end where you asked, when you read with a `limit`
below 7 (the one case that leaves a character cut), or when an `offset` lies
at or past the end of the file.

The end of a log, where a crash is, takes one call. With the default root:

```json
{"path": "Saved/Logs/<ProjectName>.log", "offset": -48128}
```

A log longer than 47 KiB comes back as its last 47 KiB followed by
`[bytes X-Z of Z]`; a shorter one comes back whole, without a trailer. That is
the most one call returns. A larger negative offset, such as `-65536`, starts
further back but does not reach the end: it returns 47 KiB from there, followed
by `[bytes X-Y of Z; continue with offset=Y]`. Page on with `offset=Y` to the
end.

Text only. Binary content is returned as-is through a JSON string and arrives
mangled. Reading a directory fails with
`Error: path "<given>" is a directory; use list_dir`.

### list_dir

`path` (string, optional). An empty or omitted path, `.`, or the root's
absolute path lists the root.

One entry per line, directory names suffixed with `/`, sorted by byte value, so
upper case comes first (`Q.txt` before `a.txt`). A symlink or junction is listed
under its own name without the `/`, even when it points to a directory and even
when it leads out of the root; whether it can be followed shows only when you
list or read through it. Not recursive; no sizes, no timestamps, no filtering,
no glob. An empty directory yields an empty result, which is a success, not an
error. To walk a tree, call `list_dir` per level.

### write_file

`path` (string, required), `content` (string, required).

Creates missing parent directories, then truncates and overwrites. There is no
backup, no existence check and no confirmation — an existing file is replaced
outright. Read before you write when you mean to preserve anything.

A missing or misspelled `content` argument is rejected with
`Error: 'content' missing` and nothing is written. An explicit empty string is
accepted, though, and truncates the target to zero bytes. The byte count in the
result is your check that the payload arrived.

### edit_file

`path`, `old_string`, `new_string` (all strings, all required).

**Replaces every occurrence of `old_string`, not just the first, and not only a
unique match.** This differs from the single-match edit tools many agents have
natively; a short `old_string` will hit comments, strings and unrelated code in
the same file. Include enough surrounding context to make the match unique when
you mean to change one site.

Matching is literal and exact: no regex, no whitespace normalisation, no
case-insensitivity. If `old_string` does not occur, the call fails with
`Error: 'old_string' not found` and the file is untouched — a clean,
non-destructive failure you can retry against. An omitted `new_string` is
rejected with `Error: 'new_string' missing`; an explicit empty string deletes
every occurrence. The result reports how many occurrences were replaced; a
count higher than you expected means you hit more sites than intended, and the
previous content is gone.

The rewrite is not atomic: the whole file is read, replaced in memory and
written back, with no size cap. Neither `edit_file` nor `write_file` limits
the size; `write_file` creates files with mode 0644 and missing directories
with 0755 (the modes matter only off Windows).

## When a call fails

`-32602 unknown tool: …` is covered under Availability, a path error under
Root confinement. For any other error, or when those two do not settle it,
load section `failures`: a table from each error text to what it means and
what to do.

## Do not hand-edit assets

The root contains the whole project, `Content/` included. `.uasset`, `.umap`,
`.uexp` and friends are binary Unreal packages: `read_file` mangles them and
`write_file` corrupts them. Everything about assets, actors, levels, blueprints
and materials goes through the in-editor plugin's tools instead.

What these tools are good for is the text half of a UE project: `.cpp`/`.h`
under `Source/`, `.cs` build rules, `.ini` under `Config/`, `.uproject` and
`.uplugin`, shaders, and any scripts or data files you keep alongside.

Editing C++ or config does not change the running editor. Through unreagent's
tools, a C++ change takes effect in a new editor process: stop the editor
(`ue_stop`), compile (`run_command` with the project's `compile` command),
then start it (`ue_start`) to load the new modules. Stop it before the
compile: with the editor up, expect the build to fail. UnrealBuildTool refuses
outright while a Live Coding session is active (exit 6, read in the UE 5.8.4
source); what it does against a running editor without one is not verified.
A config change (`Config/*.ini`, `.uproject`, `.uplugin`) needs an editor
restart (`ue_restart`); a C++ plugin newly enabled there needs the C++ steps
instead. The `unreagent-build` and `unreagent-iterate` skills cover that loop.

## See also

- `logs` — the last lines of a supervised service's output (`ue`, `agent`) from
  the supervisor's buffer. `read_file` only sees what is already on disk; for
  the end of a log file, give it a negative `offset`.
- `run_command` — compile and package, from the commands declared in the config.
- `unreagent-runtimes` — running Python or Node on the launcher host, including
  file processing that is too involved for these four tools.
