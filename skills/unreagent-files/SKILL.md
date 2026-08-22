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
returns the JSON-RPC error `-32602 unbekanntes Tool: read_file`. That message
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
`Datei-Tools aktiv (read/write | read-only) unter: <root>`. That line is the
authoritative statement of the root and the mode in effect.

## Root confinement

`files.root` defaults to `${PROJECT_DIR}`, the directory holding the `.uproject`,
and is substituted at config load. If it is set to an empty string, the root
falls back to the agent working directory, and failing that to the launcher's
own working directory.

Every path parameter is joined onto the root and the result is rejected unless
it is the root itself or lies below it. This is a guarantee: no argument to
these tools can read or write outside the root. What it costs you:

- **Paths are relative to the root. Always.** There is no way to address a file
  elsewhere on the machine.
- **An absolute path is not honoured** — it is joined onto the root like any
  other relative path. `/etc/passwd` becomes `<root>/etc/passwd`; `C:\Windows\x`
  becomes `<root>\C:\Windows\x`. You get a confusing "not found" or invalid-path
  error rather than the file you asked for. If a result makes no sense, check
  first whether you passed an absolute path.
- **Escaping with `..` is detected** after the path is cleaned, and produces
  `Fehler: Pfad außerhalb des erlaubten Roots`.
- **An empty or omitted path is not a shortcut to the root** — that holds per
  tool, not in general. `list_dir` defaults to the root. `read_file` and
  `write_file` reject an absent or empty `path` up front with
  `Fehler: 'path' fehlt`. `edit_file` is the odd one out: it is the only one
  of the four without that check, so an omitted `path` resolves to the root
  directory and the call fails later, when reading it, with an "is a
  directory" message that names neither the parameter nor the root. Pass
  `path` explicitly to all three writing and reading tools.
- The check is lexical; symlinks are not resolved. A symlink inside the root
  pointing outside it is followed. Treat the confinement as a guard against path
  mistakes, not as a security boundary around a hostile project tree.

## Tools

| Tool | Parameters | Returns |
|---|---|---|
| `read_file` | `path` | file content as text, truncated at 256 KiB |
| `list_dir` | `path` (optional) | one entry per line, directories with a trailing `/` |
| `write_file` | `path`, `content` | `geschrieben: <path> (<n> Bytes)` |
| `edit_file` | `path`, `old_string`, `new_string` | `<n> Vorkommen ersetzt in <path>` |

### read_file

`path` (string, required) — relative to the root.

Returns the raw file content, nothing else: no line numbers, no ranges, no
offset or limit parameter. Files larger than 256 KiB (262144 bytes) come back as
the first 256 KiB followed by `\n…[gekürzt]`; the cut is by byte count and can
split a multi-byte character. Truncation takes the head, so the end of a long
file is unreachable through this tool.

Text only. Binary content is returned as-is through a JSON string and arrives
mangled. Reading a directory instead of a file fails with the operating
system's error.

### list_dir

`path` (string, optional; defaults to the root).

One entry per line, directory names suffixed with `/`, sorted by name. Not
recursive; no sizes, no timestamps, no filtering, no glob. An empty directory
yields an empty result, which is a success, not an error. To walk a tree, call
`list_dir` per level.

### write_file

`path` (string, required), `content` (string, required).

Creates missing parent directories, then truncates and overwrites. There is no
backup, no existence check and no confirmation — an existing file is replaced
outright. Read before you write when you mean to preserve anything.

One sharp edge: `content` is read defensively, and a missing or misspelled
`content` argument is treated as the empty string. `write_file` with a typo in
that parameter name silently truncates the target to zero bytes. The byte count
in the result is your check that the payload arrived.

### edit_file

`path`, `old_string`, `new_string` (all strings, all required).

**Replaces every occurrence of `old_string`, not just the first, and not only a
unique match.** This differs from the single-match edit tools many agents have
natively; a short `old_string` will hit comments, strings and unrelated code in
the same file. Include enough surrounding context to make the match unique when
you mean to change one site.

Matching is literal and exact: no regex, no whitespace normalisation, no
case-insensitivity. If `old_string` does not occur, the call fails with
`Fehler: 'old_string' nicht gefunden` and the file is untouched — a clean,
non-destructive failure you can retry against. If `new_string` is empty or
omitted, every occurrence is deleted. The result reports how many occurrences
were replaced; a count higher than you expected means you hit more sites than
intended, and the previous content is gone.

The rewrite is not atomic: the file is read, replaced in memory and written
back.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `-32602 unbekanntes Tool: read_file` | file tools disabled | user sets `files.enabled: true`, or runs with `-files` |
| `-32602 unbekanntes Tool: write_file` while `read_file` works | `files.readOnly: true` | user clears `readOnly`; do not retry |
| `Fehler: Pfad außerhalb des erlaubten Roots` | the path escaped the root | make it relative to the project root |
| `Fehler: 'path' fehlt` | `path` absent or empty | supply it; for `list_dir`, empty means the root |
| `Fehler: 'old_string' nicht gefunden` | no literal match | re-read the file; check whitespace and line endings |
| `Fehler: 'old_string' fehlt` | `old_string` absent or empty | `edit_file` cannot insert into an empty match |
| content ends in `…[gekürzt]` | file exceeds 256 KiB | you have the head only; the tail is unreachable |
| file appears empty after a write | `content` was missing or misspelled | rewrite with the correct parameter name |

Error messages currently come back in German (the launcher's remaining
untranslated strings). Match them loosely; the wording is expected to change to
English, the behaviour is not.

## Do not hand-edit assets

The root contains the whole project, `Content/` included. `.uasset`, `.umap`,
`.uexp` and friends are binary Unreal packages: `read_file` mangles them and
`write_file` corrupts them. Everything about assets, actors, levels, blueprints
and materials goes through the in-editor plugin's tools instead.

What these tools are good for is the text half of a UE project: `.cpp`/`.h`
under `Source/`, `.cs` build rules, `.ini` under `Config/`, `.uproject` and
`.uplugin`, shaders, and any scripts or data files you keep alongside.

Editing C++ or config does not change the running editor. A source change needs
a compile (`run_command` with the project's `compile` command) and usually an
editor restart (`ue_restart`) before it has any effect; the `unreagent-build`
and `unreagent-iterate` skills cover that loop.

## See also

- `logs` — the last lines of a supervised service's output (`ue`, `agent`) from
  the supervisor's buffer. `read_file` only sees what is already on disk, and
  only from the top.
- `run_command` — compile and package, from the commands declared in the config.
- `unreagent-runtimes` — running Python or Node on the launcher host, including
  file processing that is too involved for these four tools.
