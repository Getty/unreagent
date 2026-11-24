# unreagent setup: what only the user can do

You cannot do these from inside the session. For each, tell the user what to
do, why, and what they will see. *Measured* means seen on the walk-through
(Windows 11, UE 5.8.4, Claude Code in the launcher's window, one machine).

| Step | Why it is the user's |
|---|---|
| restarting the launcher | no tool restarts it, and it reads its config and looks for the toolkit only at its start |
| answering Claude Code's trust question | it is asked in the window, before a session exists |
| logging in to Claude | the same |
| freeing a taken port, or choosing another one | another program holds it; the start that fails has no session |
| installing a program (Claude Code, Node.js, Visual Studio, the engine) | outside the project directory |
| saving work in the editor before a stop | `ue_stop` is a kill |
| committing, or deciding what source control ignores | theirs to decide |

## Restarting the launcher

Ask for it once, at the end, when every file is written: after the config,
the toolkit, the build and `CLAUDE.md`. Whether this conversation continues
after the restart is unverified, so the files have to carry everything.

Tell the user, in your own words:

1. Save any work that is open in the editor. Stopping the launcher kills the
   editor.
2. Leave Claude Code. With the default `agent.onExit`, the launcher then asks
   in the same window:

   ```
     [Enter] stop everything (UE + launcher)
     [k]     keep the editor running, launcher console
     [r]     restart the agent
   ```

   Enter stops everything. So does no answer within 30 s. `[r]` restarts only
   the agent: the launcher keeps its old configuration, so it is not the
   restart you need.
3. Start `unreagent.exe` again, as before.
4. If this conversation is gone, say to the new session: "verify the
   unreagent setup". Give them the sentence.

Also tell them what the restart does: the launcher reads the new
`unreagent.yaml`, finds the toolkit, and installs the bridge's dependencies
with `npm install` on its first start with the plugin (*measured*: about 10 s,
with the network). The editor comes up with the plugin (*measured*: 11 to 12 s
after its start).

In the new session, read the first lines of `unreagent.log` yourself. In
window mode the launcher's startup lines stay on screen only until Claude
Code draws its own screen (*measured*: a few seconds once the folder is
trusted), so the user has most likely not seen a startup `WARN`.

## The trust question and the login

*Measured*: on a folder Claude Code has not seen before, it asks whether the
folder is trusted before it starts, with "No, exit" preselected. A reflex
Enter ends the agent. It asks again at the next start if it was not answered.
Warn the user before a restart in a project where Claude Code has not been
trusted yet. In a session that is already running, the question was answered.

Claude Code may also ask the user to log in, in that window. *Measured* once:
the agent exited around a login and the launcher restarted it 3 s later.

## A taken port

A launcher start ends at once when it cannot bind `mcp.address`. The error
starts `MCP server cannot listen on <address>` and names both ways out.

*Measured*: started by double-click, the window closes before the error can
be read, and no `unreagent.log` is written for it. If the user reports that
the launcher "does nothing" after a restart, ask them to start it from a
console in the project directory and to read you the line.

The two ways out:

- The user stops the other program.
- You propose another port for this machine, in `unreagent.local.yaml`
  (*measured*: this worked):

  ```yaml
  mcp:
    address: 127.0.0.1:8766
  ```

  Which port is free is the user's to say. The override applies at the next
  start.

Inside a running launcher, `unreagent init` reports the launcher's own port
as taken. That is not this case; the main skill, step 2, says how to tell
them apart.

## Installing a program

`init`'s fix lines name what is missing and where to get it: Claude Code so
that `claude` is on PATH, Node.js for `node` and `npm`. After an install the
user opens a new console, so that PATH is current, and starts the launcher
from there.

Building needs a C++ toolchain. *Measured*: the machine had Visual Studio
2022, and `compile` built the project and the toolkit there. `init` does not
check for a toolchain; a failing `compile` is the first sign, and its reply
says why (skill `unreagent-build`).

A full path to a program on this machine goes into `unreagent.local.yaml`,
for example `agent.command`, as the fix line for a missing agent command
says.
