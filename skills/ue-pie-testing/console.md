# run_console_command and console steps

Two ways run a console command through the plugin, and they differ:

| | `run_console_command` | a `console` step of `run_sequence` |
|---|---|---|
| world | the **editor's** world, also while PIE runs | the **PIE** world |
| checks | the blocklist and the rules below | none |
| output | `output` in the result | only the log |
| when | at the call | at the step's time in a run (section `sequences`) |

## run_console_command

Runs one command. `command` (string) is required. The checks run in this
order, and the first that matches fails the call:

| Rule | Error |
|---|---|
| no editor | `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?` |
| no editor world | `No active world. Open a level in the editor first` |
| absent or empty | `Missing required parameter: command` |
| more than 2048 characters | `Command exceeds maximum length of 2048 characters` |
| begins with a blocked entry | `Command '<blocked>' is blocked for safety` |
| contains `;`, `\|` or `&&` | `Command chaining is not allowed` |
| contains a backtick, `$(` or `${` | `Shell escape sequences are not allowed` |

**The blocklist** is matched against the start of the command, ignoring case
and surrounding spaces: `quit`, `exit`, `crash`, `forcegc`, `forcecrash`,
`debug crash`, `mem`, `memreport`, `obj`, `exec`, `savepackage`,
`deletepackage`, `net`, `admin`, `shutdown`, `restartlevel`, `open`,
`servertravel`, `toggledebugcamera`, `enablecheats`, `stat slow`, `gc.` and
`r.` (`MCPParamValidator.cpp`). It is a prefix match, so it also refuses
commands that merely begin with those letters, such as anything starting with
`net`, `obj` or `mem`. Every rendering variable `r.…` is refused ("as they can
crash", the plugin's comment), although the tool's own description suggests
`r.SetRes`. The error names the blocked entry, not your command. To open a
level, use `open_level` (skill `ue-levels`).

The result has `command` and `output` (what the command wrote back, trailing
whitespace removed); the message is `Executed command: <command>`.
**Success means only that the command passed the checks and was handed to the
engine.** It is not checked whether the engine knew the command, and many
commands write to the log or the screen, not to `output`. Check the effect:
in the log (`get_output_log` with `filter`), with a capture, or by reading
the state back.

**PIE.** The command goes to the editor's world even while PIE runs. The
tool's description lists `slomo 0.5` as working in PIE only; whether such
game commands reach the PIE session from here is unverified. For a command
that must act on the game, use a `console` step in `run_sequence`.

The plugin's notes say never to run `SaveAll`: it opens the editor's own
confirmation dialog. Save through the asset tools (skill `ue-assets`).

## console steps

A `console` step runs `GEngine->Exec` on the PIE world at its time in the
run, logs `PIESequenceRunner: Console step - '<command>' (frame <n>)` and a
`[PIE-DBG] CONSOLE <command>` line, and writes nothing back. **It skips the
blocklist**: an `r.` command or `open` runs there. The plugin blocks those in
`run_console_command` on purpose; ask the user before you run one this way.

## Commands useful for verification

From the tool's own description, Epic's documentation (sources below) and
the plugin's notes. That a command exists is sourced; **what it does when sent
through `run_console_command` to the editor's world is not verified**, since
nothing here has run against an editor. Look at the effect before you rely
on it.

| Command | Does | Source |
|---|---|---|
| `stat fps` | frame-rate counter drawn on the viewport | tool description; Epic, Stat Commands |
| `stat unit` | frame, game thread, render thread and GPU times on the viewport | the same |
| `stat unitgraph` | `stat unit` as a graph | Epic, Stat Commands |
| `stat gpu` | GPU time per rendering pass | Epic, Stat Commands |
| `stat game`, `stat anim`, `stat physics` | gameplay tick, skinned-mesh and physics times | Epic, Stat Commands |
| `stat none` | hides the stat overlays (unverified: not on Epic's page) | — |
| `viewmode lit`, `unlit`, `wireframe`, `lit_detaillighting`, `lightingonly`, `lightcomplexity`, `shadercomplexity`, `lodcoloration` | the view mode of the viewport; `viewmode lit` is the normal one | Epic, Viewport Modes |
| `show collision`, `show bounds` | toggle collision and bounds display | tool description |
| `ShowFlag.<Name> 0` or `1` | set one show flag (unverified: forum reports only) | — |
| `HighResShot` | a high-resolution screenshot file, not stretched to 1024x576, in the project's `Saved/Screenshots/<platform>` (Epic names `Saved\Screenshots\Windows`) | Epic, Taking Screenshots; which viewport it takes from here is unverified |
| `slomo <factor>` | game speed, PIE only | tool description |
| `t.MaxFPS <n>` | frame cap; `0` lifts it. `run_sequence` and the monitor set it themselves | plugin code |
| `ce <EventName>` | calls a custom event | tool description |

Stat overlays and view modes are drawn into the viewport, so a capture shows
them: run `stat unit`, then capture, to read the times off the picture. In
PIE, put the `console` step before a `capture` step in the same run. Many
`stat` and `show` commands toggle: sending the same command again turns it
off. **Reset what you changed** and tell the user, since the viewport is
theirs.

Sources: dev.epicgames.com, "Stat Commands in Unreal Engine", "Viewport Modes
in Unreal Engine", "Taking Screenshots in Unreal Engine".
