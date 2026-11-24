# unreagent file tools: failure modes

Where a message names `<root>`, that is `files.root` made absolute, as
configured, not resolved through links (section `confinement`).

| What you see | What it means | What to do |
|---|---|---|
| `-32602 unknown tool: read_file` | file tools disabled | user sets `files.enabled: true`, or runs with `-files` |
| `-32602 unknown tool: write_file` while `read_file` works | `files.readOnly: true` | user clears `readOnly`; do not retry |
| `Error: path "<given>" must be relative to the project root "<root>"; an absolute path is accepted only under it` | an absolute or rooted path that is not under the root | make it relative to the root; if it should be under it, compare with the `File tools active … under:` log line |
| `Error: path outside the allowed root` | a relative path climbs out with `..`; on a Windows host also a relative path with a colon | drop the `..`; nothing above the root is reachable |
| `Error: path "<given>": a link on the path (symlink or junction) is absolute or leads outside the allowed root "<root>" (…)` | a symlink or junction on the path has an absolute target or leads out of the root | do not retry; see section `confinement`, "A plugin linked in from outside the root" |
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
