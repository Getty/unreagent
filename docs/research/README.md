# Research notes

Background research for the people and agents who develop unreagent. Nothing
here ships in the binary, and nothing here is a specification.

**These documents are input, not truth.** They were written on 2026-11-21 by an
external language model from public documentation, at the maintainer's request.
No statement in them was checked against unreagent's code or run on a Windows
machine. A fact becomes usable once someone has checked it against its cited
source and against the code that depends on it; the place for checked facts is
the project skills under `.claude/skills/`, not this directory.

| Document | Covers | Cards it feeds |
|---|---|---|
| [Unreal Editor as seen from a parent process](2026-11-21-unreal-editor.md) | editor start and `-unattended`, crash handling, exit codes, engine discovery, build command lines, readiness | k198, k136, k154 |
| [Windows process and console facts](2026-11-21-windows-processes.md) | Job Objects and the assignment race, breakaway, nested jobs, consoles, Ctrl-C, path forms | k176, k154, k160, k185, k109 |
| [Shell constructs that hide a command](2026-11-21-shell-constructs.md) | test corpus for the permission layer, Bash and PowerShell | k114, k127, k171, k183, k184 |

Each document ends with an "Open facts" table of experiments that only a real
Windows machine can settle (ids `U1`–`U8` and `W1`–`W9`). The Unreal document
could not read the engine source, so its claims rest on Epic's documentation
alone.

The shell catalogue uses the harmless marker commands `id` and `whoami` only.
