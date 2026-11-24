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

This skill has four sections:

| Section | Holds | Load it |
|---|---|---|
| `custom-commands` | what the user writes under `commands:`: an example entry, how it replaces a built-in command, how `unreagent.local.yaml` merges the map, how the launcher starts a command | before you tell the user how to add or change a command |
| `reply-cuts` | the exact cut rules of a reply: the 48 KiB cut of a success, head and tail of a failing reply, a line longer than its share, the dropped middle past 2 MiB, and what N and B count in each case | when a reply carries an omitted marker and you need to know exactly what was cut, or what N or B counts |
| `job-objects` | how a run's processes are killed on Windows, the two launcher-log warnings that mean they are not, what a launcher exit does to a run, the non-Windows build | when processes are left over after a run, or before the launcher is stopped while a run is going |
| `failures` | the failure table (what you see, what it means, what to do) | when a call returns an error or a non-zero exit, or does not come back |

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

**The list is fixed when the launcher starts.** You cannot add a command,
change one, or pass it arguments through MCP. New or changed commands are a
config edit by the user, followed by a launcher restart. Load section
`custom-commands` before you tell the user how to add or change a command:
it has an example
entry, how a command of the same name replaces a built-in one, how
`unreagent.local.yaml` merges the map, and that a command is started
without a shell.

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
- **Either reply can also be cut by bytes, and past 2 MiB of output the
  middle is dropped as well.** A successful reply then carries
  `[... B bytes omitted; a reply holds at most 48 KiB]`, and its first line
  can start mid-line. A failing reply holds the output's first whole lines
  within 4 KiB and as many of the last as fit, and counts bytes instead of
  lines when a single line is longer than its share. Whatever a marker
  counts is gone. Load section `reply-cuts` when a reply carries an omitted
  marker and you need to know exactly what was cut, or what N or B counts.
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
  foreground; `unreagent-iterate`, section `logs`, covers how to read it.
- **The call returns when the command itself exits.** On Windows, the command's
  job is closed at that moment, which kills anything the build left running
  inside it, such as a build-server node or a worker. The launcher then
  collects the remaining output for at most 2 s. Output written after that, by
  a process outside the job, is dropped from the result.

## Build processes and Job Objects

On Windows, each run is placed in its own Job Object. When the command
exits, every process still inside the job is killed, so a build leaves no
stray processes behind. **When the launcher exits, an unfinished run is
killed the same way**: stopping the launcher in the middle of `package`
aborts the package. `ue_stop` and `ue_restart` do not touch a run. Load
section `job-objects` when processes are left over after a run, or before
the launcher is stopped while a run is going: it has the two launcher-log
warnings that mean the job did not exist, and the non-Windows build.

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

## When a call fails

When a call returns an error or a non-zero exit, or does not come back, load
section `failures`: a table from what you see to what it means and what to
do.

## See also

- `unreagent-iterate`: `ue_stop` / `ue_start` / `ue_restart`, `status`, `logs`,
  and the edit → compile → restart loop.
- `unreagent-files`: reading and editing the C++ and config files before a build.
- `unreagent-runtimes`: `run_python` / `run_node`, for searching a large log file
  on the host.
