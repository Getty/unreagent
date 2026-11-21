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
authoritative statement of the root and the mode in effect.

## Root confinement

`files.root` defaults to `${PROJECT_DIR}`, the directory holding the `.uproject`,
and is substituted at config load; an empty `files.root` gets the same default.
There is no fallback to any other directory. When a placeholder in
`files.root` cannot be resolved (for the default: no `.uproject` found next to
the config and no `unreal.project` set), `files.enabled: true` stops the
launcher at config load with `${PROJECT_DIR} is unresolved (…) but used by …`,
`files.root` among the named fields, and `-files` starts without the four tools
and logs
`WARN file tools disabled: files.root names a placeholder that is unresolved — set unreal.project or files.root`.
Where an error message below names `<root>`, that is `files.root` made
absolute, as configured, not resolved through links.

Every call opens the root afresh and resolves the path inside it one component
at a time, links included, so nothing can be swapped in between the check and
the use. No path argument to these tools reaches outside the root: not through
`..`, not through an absolute path, not through a symlink or junction. What it
costs you:

- **Paths are relative to the root. Always.** There is no way to address a file
  elsewhere on the machine.
- **An absolute path is accepted only under the root.** It is made relative to
  the root and then gets every check below; the root's own absolute path means
  the root. A root that lies behind a symlink may be spelled either way, as
  configured or as its real location. Behind a Windows junction only the
  configured spelling works: Go does not resolve junctions, so the junction's
  real location counts as outside the root. Any other absolute path is refused
  with
  `Error: path "<given>" must be relative to the project root "<root>"; an absolute path is accepted only under it`.
  On a Windows host that includes a rooted path (`\Source\Foo.cpp`) and a
  drive-relative one (`C:Source\Foo.cpp`).
- **Escaping with `..` is refused** before anything is opened, with
  `Error: path outside the allowed root`: `../x` and `Source/../../x` never
  get through. A `..` that stays inside the root is not cleaned away but
  walked: on a Linux host `Source/../Config/x.ini` works only while `Source`
  exists as a directory, and otherwise fails with
  `Error: openat <name>: no such file or directory` (or `not a directory`). A
  Windows host cleans the path before walking it. Write `Config/x.ini` instead.
  On a Windows host a relative path with a colon in it (`a.txt:stream`) gets
  the `path outside the allowed root` error as well.
- **A link that leaves the root is not followed.** A symlink or junction
  anywhere on the path is refused when its target is absolute or lies outside
  the root:
  `Error: path "<given>": a link on the path (symlink or junction) is absolute or leads outside the allowed root "<root>" (<go-error>)`,
  where `<go-error>` is Go's own, such as
  `openat <name>: path escapes from parent` or
  `mkdirat <dir>: path escapes from parent`. The refusal holds on the way to a
  path that does not exist yet, so `write_file` creates neither the file nor
  missing directories beyond such a link, and it holds through a dangling link.
  An absolute target is refused even when it points back into the root, so a
  Windows junction, whose target is always absolute, is never followed. A
  relative link works normally as long as its target never leaves the root:
  one that climbs out and back in (`Cfg` → `../MyGame/Config` in a root named
  `MyGame`) is refused with the same error. The link itself still shows up
  when you list its parent; only following it fails.
- **A link loop, or more than 8 links on one path,** fails with
  `Error: openat <name>: too many levels of symbolic links`.
- **Windows reserved device names are refused** in any path component, on every
  host: `CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9` (also with the
  superscript digits `¹ ² ³`), `CONIN$` and `CONOUT$`, in any letter case, also
  with an extension or trailing spaces. `aux.h`, `NUL.txt` and
  `Source/CON/x.txt` are all refused with
  `Error: path component "<component>" is a reserved Windows device name`.
  On Windows such a name opens a device instead of a file, and a file of that
  name written on another host cannot be checked out on Windows. Names that only
  start like one (`CONTRIBUTING.md`, `console.h`, `COM10.txt`) and `COM0` /
  `LPT0` are ordinary names.
- **An empty or omitted path is not a shortcut to the root** — that holds per
  tool, not in general. `list_dir` defaults to the root. `read_file`,
  `write_file` and `edit_file` reject an absent or empty `path` up front with
  `Error: 'path' missing`. Pass `path` explicitly to all three.

What the confinement does not cover: a hard link is not a link in this sense
but a second directory entry for the same file, so one inside the root reads and
writes that file wherever its other names live; on a Linux host a bind mount
inside the root is crossed as well.

### A plugin linked in from outside the root

A plugin is often linked into `<Project>/Plugins/` from a shared checkout
elsewhere, with a junction or a symlink. The file tools cannot follow that
link: listing `Plugins` still shows the entry (without a trailing `/`, see
list_dir), but every path through it fails with the link error above. Neither a
retry nor an absolute spelling of the path gets through. Tell the user; both
fixes are theirs:

- **Put the plugin into the project**, as a real directory under `Plugins/`
  rather than a link.
- **Or set `files.root` to a directory that holds both** the project and the
  plugin. The plugin is then reachable at its real location, not through the
  link: a junction or an absolute symlink is still refused, and only a relative
  symlink that stays inside the new root works. Every path is then relative to
  the new root, so the project's own files gain the project directory as a
  prefix (`MyGame/Source/…`); the `File tools active` log line shows the root in
  effect.

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
cut character (see below). Otherwise a page can begin up to 3 bytes after the
offset asked for: continuation bytes at its start are dropped, whether they are
stray (invalid UTF-8, binary) or the rest of a character the previous page cut.
The trailer's `X` shows where a page really begins, so an `X` larger than the
`offset` you passed marks the gap. A log that is still being written grows
between calls, so a later call can report a larger `Z`.

Range edges move inward to UTF-8 character boundaries, by up to 3 bytes: a
start inside a character moves forward past its continuation bytes, an end
inside one moves back to the character's first byte. The trailer reports the
range after the move. Neither edge moves at the start or the end of the file,
so a file returned whole comes back byte for byte, a UTF-8 byte order mark at
offset 0 included (it arrives as U+FEFF, the first character). A range that
starts at offset 1 or 2 begins after it, at 3. One case leaves a character
cut: when it begins at the range's first byte once the start has moved, the end
stays, because moving it back would empty the range. That takes fewer than 4
bytes left after the start moved, so a `limit` below 7, or below 4 from a
character boundary.

An offset past the end is an error that names the file size. An offset exactly
at the end is not: it returns an empty range followed by `[bytes Z-Z of Z]`.

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

The rewrite is not atomic: the file is read, replaced in memory and written
back.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `-32602 unknown tool: read_file` | file tools disabled | user sets `files.enabled: true`, or runs with `-files` |
| `-32602 unknown tool: write_file` while `read_file` works | `files.readOnly: true` | user clears `readOnly`; do not retry |
| `Error: path "<given>" must be relative to the project root "<root>"; an absolute path is accepted only under it` | an absolute or rooted path that is not under the root | make it relative to the root; if it should be under it, compare with the `File tools active … under:` log line |
| `Error: path outside the allowed root` | a relative path climbs out with `..`; on a Windows host also a relative path with a colon | drop the `..`; nothing above the root is reachable |
| `Error: path "<given>": a link on the path (symlink or junction) is absolute or leads outside the allowed root "<root>" (…)` | a symlink or junction on the path has an absolute target or leads out of the root | do not retry; see A plugin linked in from outside the root |
| `Error: openat <name>: too many levels of symbolic links` | a link loop, or more than 8 links on the path | do not retry; tell the user |
| `Error: path component "<component>" is a reserved Windows device name` | `CON`, `NUL`, `AUX`, `COM1`, `aux.h`, … in some component | pick another name; such a file cannot exist on Windows |
| `Error: 'path' missing` | `path` absent or empty | supply it; for `list_dir`, empty means the root |
| `Error: 'old_string' not found` | no literal match | re-read the file; check whitespace and line endings |
| `Error: 'old_string' missing` | `old_string` absent or empty | `edit_file` cannot insert into an empty match |
| content ends in a line `[bytes X-Y of Z]` or `[bytes X-Y of Z; continue with offset=Y]` | `read_file` returned bytes `X` to `Y` (exclusive) of a `Z`-byte file, not the whole file; the line and the newline before it are not file content | to go on, call again with `offset=Y`; for the end of the file, a negative `offset` |
| `Error: 'offset' <n> is past the end of "<given>" (<size> bytes)` | `offset` is larger than the file | use an offset up to the size named, or a negative one to count from the end |
| `Error: 'limit' must be a positive number of bytes, got <n>` | `limit` is 0 or negative | omit it for the default of 48128, or pass a positive byte count |
| `Error: 'offset' must be an integer, got …` (or `'limit'`) | neither a whole number nor an integer string, such as `1.5` or `"end"` | pass an integer number of bytes |
| `Error: 'offset' must be an integer between -9223372036854775808 and 9223372036854775807, got …` (or `'limit'`) | the number does not fit a 64-bit integer | pass a byte count; a negative `offset` counts from the end |
| `Error: 'content' missing` | `content` absent or not a string — typically a misspelled parameter name | rewrite with the correct parameter name; nothing was written |
| `Error: open <root>: no such file or directory` | the root itself does not exist; every call fails | user fixes `files.root` |
| `Error: openat <name>: no such file or directory` | nothing at that path under the root | check with `list_dir` |
| `Error: path "<given>" is a directory; use list_dir` | `read_file` on a directory | use `list_dir` |
| `Error: readdirent <abs path>: not a directory` | `list_dir` on a file | use `read_file` |

The `open`, `openat` and `readdirent` rows quote a Linux host. On a Windows
host the operating system's part of the message (`no such file or directory`,
`not a directory`) reads differently. The directory error of `read_file` is the
launcher's own and reads the same on every host.

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
compile: with the editor up, expect the build to fail or to wait (which of the
two is not verified against an editor).
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
