---
name: unreagent-build
description: >
  Compile the project's C++ or package a build with run_command, and read what
  it returns. Use before a compile or package run, when a build result looks
  cut off, or when a run_command call does not come back.
---

# unreagent build commands

`run_command` runs one command from a fixed list that the launcher's config
declares. The command runs on the launcher's host. The tool waits for it to
finish and returns its exit code and its output; a long output is cut to its
end, or, when the command failed, to its start and its end. This is how you
compile and package. There is no other build tool here, and you cannot pass a
command line of your own.

## The command list

`status` returns the available names under `commands`. The `run_command` tool
description lists them as well, as they were at launcher start. You get names
only: the `description` written in the config is not exposed through any tool.

Unless the project's config defines them differently, the launcher has two
built-in commands:

| Name | Runs |
|---|---|
| `compile` | `<engine>/Engine/Build/BatchFiles/Build.bat <ProjectName>Editor Win64 Development -Project=<.uproject> -waitmutex -FromMsBuild` |
| `package` | `<engine>/Engine/Build/BatchFiles/RunUAT.bat BuildCookRun -project=<.uproject> -noP4 -platform=Win64 -clientconfig=Development -build -cook -stage -pak -archive -archivedirectory=<project dir>/Packaged` |

- **`compile`** builds the `<ProjectName>Editor` target: the project's own C++
  modules under `Source/`, and with them the enabled code plugins under
  `Plugins/`, so such a project needs no other command for its plugins. A
  project without its own `Source/` has no such target, and `compile` fails
  there. That project can still have C++ to build: code plugins under
  `Plugins/`, for example an in-editor plugin installed as source. Those build
  through the `UnrealEditor` target with `-Project=<.uproject>`. The built-in
  `compile` does not run that target, so building them needs a command the
  user adds under `commands:`.
- **`package`** builds, cooks, stages and paks a Development Win64 game. It
  archives the result under `Packaged/` in the project directory.

Both need the engine root and the `.uproject`. When the launcher did not find
one of them at start, it leaves both out: they are not in `status` or in the
`run_command` description, a call returns `Error: unknown command "compile"`,
and the launcher log says
`WARN built-in command compile not offered: it needs the engine and the .uproject (see above)`.
The user sets `UE_ROOT` or `engineRoot`, or `unreal.project`, and restarts the
launcher.

The list is fixed when the launcher starts. You cannot add a command, change
one, or pass it arguments through MCP. New or changed commands are a config
edit by the user, followed by a launcher restart. A command defined under
`commands:` in the config replaces the built-in one of the same name:

```yaml
commands:
  cook:
    description: "Cook content only"   # not shown to the agent
    command: "${ENGINE}/Engine/Build/BatchFiles/RunUAT.bat"
    args: ["BuildCookRun", "-project=${PROJECT}", "-noP4", "-cook", "-skipstage"]
    # dir: ""                           # working directory; empty = the launcher's own
```

`commands:` is a map. In `unreagent.local.yaml` it merges by name: an entry
there replaces that whole command, and every other command stays. The launcher
starts `command` directly with `args`, without a shell of its own, so pipes and
redirection do not exist. The environment is inherited from the launcher.

## Calling it

`name` (string, required) is the only parameter. There is no argument list,
no timeout and no working-directory parameter.

The exit code decides the shape of the reply. A command that exits 0 returns:

```
exit 0

[... N lines omitted]
[... B bytes omitted; a reply holds at most 48 KiB]
<the last 300 lines of stdout and stderr, merged, at most 48 KiB>
```

The `[... N lines omitted]` line is there only when the command printed more
than 300 lines, the `[... B bytes omitted; …]` line only when those lines are
more than a reply holds.

A command that exits non-zero returns an error result of at most 10,000
bytes, the `exit` line and the marker included:

```
exit <code>

<the first lines of stdout and stderr, merged, up to 4 KiB>
[... N lines omitted]
<as many of the last lines as fit>
```

When the `exit` line and the whole output fit in 10,000 bytes, the output
comes back whole instead, with no marker, even past 300 lines. The limit is
lower than for a success because Claude Code cuts an error result over about
11,000 characters to its first and last 5,000. 10,000 bytes are at most
10,000 characters, so the launcher's reply reaches you as it was sent, and
what it leaves out is marked and counted.

Decide success by the exit code, not by the text. `exit -1` means the
launcher could not determine an exit code.

These errors come back before anything runs:

| Result | Meaning |
|---|---|
| `Error: 'name' missing` | `name` is absent or empty |
| `Error: unknown command "<name>"` | no such command. Check `status` |
| `Error: start failed: …` | the program could not be started. A program missing at its path, such as `Build.bat`, reads `start failed: fork/exec <path>: …`: the engine root the launcher uses (the `Engine:` line of its log) holds no engine, or a command the user defined names a wrong path. A bare program name that is not on the launcher's PATH reads `start failed: exec: "<name>": …` |

## Reading the result

- **A successful reply holds only the last 300 lines.** When the command
  printed more, they are led by `[... N lines omitted]` (`[... 1 line
  omitted]` for one). The N lines are gone.
- **A successful reply holds at most 48 KiB**, the `exit` line and the
  markers included. When the last 300 lines are longer (about 160 bytes a
  line on average), their oldest bytes go as well, and
  `[... B bytes omitted; a reply holds at most 48 KiB]` follows the `exit`
  line and any lines marker. Whole lines go first, then the start of the
  oldest line kept: you see at most 300 lines, and the first one can start
  mid-line. A single line longer than 48 KiB comes back as its end only. The
  cut never splits a UTF-8 character. The B bytes are gone.
- **A failing reply holds the start and the end.** When the `exit` line and
  the output are more than 10,000 bytes, the head is the output's first whole
  lines within 4 KiB, then comes `[... N lines omitted]`, then the tail: as
  many of the last whole lines as fit in the rest, about 5.8 KB when the head
  is full. N counts every line between head and tail. They are gone. No
  300-line limit applies to a failing reply.
- **A failing reply cuts a line only when it is longer than its share.** A
  first line over 4 KiB leaves the head as its start; a last line that does
  not fit leaves the tail as its end. The marker then counts bytes instead:
  `[... B bytes omitted; a failing call's reply holds at most 10 KB]`. The
  cut never splits a UTF-8 character.
- **Past 2 MiB the middle is dropped as well.** While the command runs, the
  launcher keeps only the first and the last MiB of its output, and the
  markers count the dropped middle too. In a failing reply, the head comes
  from the first MiB and the tail from the last, so the marker between them
  stands where the middle was dropped, and N (or B) includes it. In a
  successful reply, N whole lines, then B bytes, are everything above what
  you see. When the last MiB holds more than 300 lines
  (its first line, which can begin in the dropped part, counts as one), the
  300 lines all come from it: N includes the dropped lines, and B counts bytes
  of the 300 only. When it holds 300 or fewer, the lines reach back into the
  first MiB, at least to its last line: N counts only the first MiB's lines
  above them, and B includes the dropped bytes. In the rare reply that still
  shows the launcher's own `[... D bytes omitted]` line, that line names the
  middle, B leaves it out, and the reply can hold up to 302 lines, that line
  included. A failing reply shows that line only when the output ends in
  nearly a MiB of line breaks, with no text after them; the marker above it
  then leaves the middle out.
- **Nothing else is retained.** The launcher discards the output once the call
  returns. `logs` holds no command output. The launcher log records only two
  lines per run:
  `[cmd:<name>] starting: <command> [<args>]` and `[cmd:<name>] done (exit N)`.
  Calling again runs the command again; it does not fetch more output.
- **stdout and stderr are merged** and cannot be told apart.

For `compile`, errors print as they occur, and other compile actions keep
running after the first failure. A failing build's reply holds the first
4 KiB of its output, which takes in the first errors when they come that
early, and its end. Errors printed in between are among the omitted lines. If
what you see looks like a follow-on error, for example an unknown type that a
missing include would explain, the cause may be among them. Fix what you can
see and compile again. A build with fewer errors is more likely to fit.

What a reply leaves out is not kept anywhere, and you cannot narrow a
`run_command` run: it takes no arguments, and a rerun of the same state cuts
the same way. Output that must be read whole belongs in a file. Only the user
can arrange that, with a script added as a command that writes the output to
a file under the project; `read_file` (`unreagent-files`) then reads it in
pieces.

For `package`, the output runs to thousands of lines. A successful run shows
its end; a failing one its first 4 KiB and its end: that the run failed, and
in which stage.

## Duration, timeouts and calls that do not come back

- **The call is synchronous.** It returns when the command has exited, with no
  progress in between. `compile` takes seconds to minutes. `package` can take
  far longer.
- **No timeout, no cancellation.** Your client may give up long before the
  build ends. The command keeps running on the host anyway, and its result is
  discarded. The launcher's MCP server also has a 4-hour write deadline per
  call. A run that takes longer still finishes, but its reply can no longer be
  sent: the connection drops at that point, and no result arrives.
- **A repeated call starts a second run beside the first.** Nothing in the
  launcher serializes runs, and `status` does not show runs in progress.
  UnrealBuildTool serializes itself instead. The `compile` command line
  passes `-waitmutex`, and with it UnrealBuildTool waits, with no time limit,
  for any other build, clean or project file generation on the same engine
  installation to end, whoever started it (read in the UE 5.8.4 source, not run). A `compile`
  that does not come back is therefore most likely waiting behind another
  build. Without the flag, as in a command the user wrote without it,
  UnrealBuildTool does not wait: while another UnrealBuildTool of the same
  engine is running, it exits 10 at once. Do not retry a build
  whose call you lost. Do not start the editor while a compile you lost track
  of may still be writing binaries.
- **The launcher log is the only trace of a lost run.** It records
  `[cmd:<name>] done (exit N)` when the run ends. That log is `unreagent.log`
  next to the `.uproject` when the launcher runs its own agent in the
  foreground; `unreagent-iterate` covers how to read it.
- **The call returns when the command itself exits.** On Windows, the command's
  job is closed at that moment, which kills anything the build left running
  inside it, such as a build-server node or a worker. The launcher then
  collects the remaining output for at most 2 s. Output written after that, by
  a process outside the job, is dropped from the result.

## Build processes and Job Objects

On Windows, each run is placed in its own Job Object with `KILL_ON_JOB_CLOSE`.
When the command exits, the job is closed before the call returns, and every
process still inside it is killed, so a build leaves no stray processes
behind. That holds only when the job exists. If the launcher could not create
it, or could not assign the command to it, the launcher log shows
`[cmd:<name>] WARN job object could not be created: …` or
`[cmd:<name>] WARN job assign failed: … — the process runs unsupervised`. In
that case, leftover processes survive the run.

When the launcher exits, an unfinished run is killed the same way, together with
everything it started. Stopping the launcher in the middle of `package` aborts
the package. `ue_stop` and `ue_restart` do not touch a run, because the editor
lives in a job of its own.

A non-Windows launcher build, used only for development, has no Job Objects.
There, a run's children are not killed with it.

## The editor and the build

- **Stop the editor before `compile`.** A running editor has the project's
  modules loaded. Two cases, both read in the UE 5.8.4 source and not run:
  - *A Live Coding session is active in the editor.* UnrealBuildTool refuses
    the build: exit 6 and `Unable to build while Live Coding is active`. It
    does not wait.
  - *No session.* Under `-unattended`, the launcher's default, Live Coding
    does not start by itself, so the refusal above does not fire. It can
    still be active when the user turned `unreal.unattended` off, put
    `-LiveCoding` into `unreal.args`, or started it by hand. What
    UnrealBuildTool does against module binaries the editor holds open
    without a session is not verified: expect the build to fail.

  Stopping first rules both out.
  `unreagent-iterate` has the full stop → compile → start loop, including what
  a stop does to unsaved work.
- **A new build takes effect only in a newly started editor.** After a
  successful `compile`, use `ue_start`, or `ue_restart` if the editor kept
  running.
- **`package` does its own build of the game target and its own cook.** If it
  fails because of the open editor, stop the editor and run it again.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| `Error: unknown command "build"` | the name is not configured | use a name from `status`. Only the user can add commands |
| `Error: unknown command "compile"`, and `status` lists neither `compile` nor `package` | the launcher found no engine root or no `.uproject` at start and left the built-in commands out | the user sets `UE_ROOT` or `engineRoot`, or `unreal.project`, then restarts the launcher |
| `Error: start failed: fork/exec …/Engine/…: …` | the program is not at that path | the user checks the engine root (`UE_ROOT` or `engineRoot`) |
| `exit 6` while the editor is running, and the output has `Unable to build while Live Coding is active` | a Live Coding session is active in the open editor (UE 5.8.4 source) | `ue_stop`, then compile again |
| `exit` non-zero while the editor is running, and the output names the running editor or a module binary it cannot write, not your code (this case was not run; the wording is not confirmed) | the open editor is in the build's way | `ue_stop`, then compile again |
| `exit 10` at once, and the output says a conflicting instance is already running | another UnrealBuildTool of the same engine is running, and this command line has no `-waitmutex` (UE 5.8.4 source). The built-in `compile` has the flag and waits instead | wait for the other build, then run it again |
| `exit` non-zero, an omitted marker, and the errors look like follow-ons | their cause is among the omitted lines or bytes | fix what is visible, then compile again |
| `exit` non-zero right away, and the project has no `Source/` | there is no `<ProjectName>Editor` target | if `Plugins/` holds C++ plugins, they need an `UnrealEditor` build, a command the user adds. Otherwise there is nothing to compile |
| `exit` non-zero, `[... N lines omitted]` between the first lines and the last | the output was more than a failing reply's 10,000 bytes; the N lines between its first 4 KiB and its end were cut (past 2 MiB, N includes the dropped middle) | they are gone, and a rerun of the same state cuts the same way. Fix what is visible and compile again; for the whole output, the user adds a command that writes it to a file, read with `read_file` |
| `[... B bytes omitted; a failing call's reply holds at most 10 KB]` | a single line was longer than its share of a failing reply: a first line over 4 KiB is cut to its start, a last line that does not fit to its end | as for the lines marker above |
| `exit 0`, then `[... N lines omitted]` | the first N lines of a successful run's output were cut | they are gone. Rerun only if you must |
| `exit 0`, then `[... B bytes omitted; a reply holds at most 48 KiB]`, alone or below the lines marker | the last 300 lines were more than a reply holds, so their oldest B bytes were cut (past 2 MiB, B can include the dropped middle); the first line can start mid-line | they are gone, and a rerun cuts the same way. For the whole output, the user adds a command that writes it to a file |
| the call never returns, or the client times out | no timeout exists, and the run continues on the host. A `compile` also waits, without a time limit, behind any other build, clean or project file generation on the same engine | do not retry. Watch the launcher log for `[cmd:<name>] done` |
| a run of more than 4 hours ends in a dropped connection, with no result | the reply missed the server's 4-hour write deadline | the run did finish. Read `[cmd:<name>] done (exit N)` in the launcher log |

## See also

- `unreagent-iterate`: `ue_stop` / `ue_start` / `ue_restart`, `status`, `logs`,
  and the edit → compile → restart loop.
- `unreagent-files`: reading and editing the C++ and config files before a build.
- `unreagent-runtimes`: `run_python` / `run_node`, for searching a large log file
  on the host.
