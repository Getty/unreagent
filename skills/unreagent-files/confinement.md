# unreagent file tools: root confinement in full

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
  with an extension, a colon or trailing spaces. `aux.h`, `NUL.txt`, `CON:` and
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

## A plugin linked in from outside the root

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
