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
finish and returns its exit code and the tail of its output. This is how you
compile and package. There is no other build tool here, and you cannot pass a
command line of your own.

## The command list

`status` returns the available names under `commands`. The `run_command` tool
description lists them as well, as they were at launcher start. You get names
only: the `description` written in the config is not exposed through any tool.

Unless the project's config defines them differently, every launcher has two
commands:

| Name | Runs |
|---|---|
| `compile` | `<engine>/Engine/Build/BatchFiles/Build.bat <ProjectName>Editor Win64 Development -Project=<.uproject> -waitmutex -FromMsBuild` |
| `package` | `<engine>/Engine/Build/BatchFiles/RunUAT.bat BuildCookRun -project=<.uproject> -noP4 -platform=Win64 -clientconfig=Development -build -cook -stage -pak -archive -archivedirectory=<project dir>/Packaged` |

- **`compile`** builds the `<ProjectName>Editor` target: the project's own C++
  modules under `Source/`. A project without its own `Source/` has no such
  target, and `compile` fails there. That project can still have C++ to build:
  code plugins under `Plugins/`, for example an in-editor plugin installed as
  source. Those build through the `UnrealEditor` target with
  `-Project=<.uproject>`. The built-in `compile` does not run that target, so
  building them needs a command the user adds under `commands:`.
- **`package`** builds, cooks, stages and paks a Development Win64 game. It
  archives the result under `Packaged/` in the project directory.

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

```
exit <code>

[... N lines omitted]
<the last 300 lines of stdout and stderr, merged>
```

The `[... N lines omitted]` line is there only when the command printed more
than 300 lines. The result is marked as an error when the exit code is
non-zero. Decide success by the exit code, not by the text. `exit -1` means the
launcher could not determine an exit code.

These errors come back before anything runs:

| Result | Meaning |
|---|---|
| `Error: 'name' missing` | `name` is absent or empty |
| `Error: unknown command "<name>"` | no such command. Check `status` |
| `Error: start failed: exec: …` | the program could not be started, for example `Build.bat` is not at that path. If the path begins with `/Engine/`, the engine root was not found when the launcher started |

## Reading the result

- **Only the last 300 lines come back.** When the command printed more, they
  are led by `[... N lines omitted]` (`[... 1 line omitted]` for one), unless
  what the 2 MiB cap below kept fits in 300 lines and only its
  `[... B bytes omitted]` line marks the cut. The N lines are gone.
- **Past 2 MiB the middle is dropped as well.** While the command runs, the
  launcher keeps only the first and the last MiB of its output, and N counts
  the dropped lines too. The gap itself shows only when the last MiB holds
  fewer than 300 lines: a line `[... B bytes omitted]`, the lines on either
  side of it can be cut mid-line. N then counts only the lines above what you
  see.
- **Nothing else is retained.** The launcher discards the output once the call
  returns. `logs` holds no command output. The launcher log records only two
  lines per run:
  `[cmd:<name>] starting: <command> [<args>]` and `[cmd:<name>] done (exit N)`.
  Calling again runs the command again; it does not fetch more output.
- **stdout and stderr are merged** and cannot be told apart.

For `compile`, errors print as they occur, and other compile actions keep
running after the first failure. In a long failing build, the first error can
be among the omitted lines. If the result starts with `[... N lines omitted]`
and what you see looks like a follow-on error, for example an unknown type that
a missing include would explain, the cause may be among them. Fix what you can
see and compile again. A build with fewer errors fits in 300 lines.

For `package`, the output runs to thousands of lines, and you see its end:
whether the run failed, and in which stage.

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
  launcher serializes runs, and `status` does not show runs in progress. For
  `compile`, `-waitmutex` makes the second UnrealBuildTool wait for the first.
  Do not retry a build whose call you lost. Do not start the editor while a
  compile you lost track of may still be writing binaries.
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

- **Stop the editor before `compile`.** UE 5 enables Live Coding by default,
  and UnrealBuildTool refuses to build while a Live Coding session is active:
  `Unable to build while Live Coding is active`. Stopping first avoids that.
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
| `Error: start failed: exec: …/Engine/…` | the program is not at that path | the user checks the engine root (`UE_ROOT` or `engineRoot`) |
| `exit` non-zero, with `Unable to build while Live Coding is active` | the editor is running with Live Coding | `ue_stop`, then compile again |
| `exit` non-zero, `[... N lines omitted]`, and the errors look like follow-ons | the first errors are among the omitted lines | fix what is visible, then compile again |
| `exit` non-zero right away, and the project has no `Source/` | there is no `<ProjectName>Editor` target | if `Plugins/` holds C++ plugins, they need an `UnrealEditor` build, a command the user adds. Otherwise there is nothing to compile |
| `[... N lines omitted]` after the `exit` line | the first N lines of the output were cut | they are gone. Rerun only if you must |
| the call never returns, or the client times out | no timeout exists, and the run continues on the host | do not retry. Watch the launcher log for `[cmd:<name>] done` |
| a run of more than 4 hours ends in a dropped connection, with no result | the reply missed the server's 4-hour write deadline | the run did finish. Read `[cmd:<name>] done (exit N)` in the launcher log |

## See also

- `unreagent-iterate`: `ue_stop` / `ue_start` / `ue_restart`, `status`, `logs`,
  and the edit → compile → restart loop.
- `unreagent-files`: reading and editing the C++ and config files before a build.
- `unreagent-runtimes`: `run_python` / `run_node`, for searching a large log file
  on the host.
