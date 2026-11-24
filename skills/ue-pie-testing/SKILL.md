---
name: ue-pie-testing
description: >
  Play-test in Play In Editor through the UE LLM Toolkit: start and stop PIE,
  run scripted input sequences that capture frames and write a manifest,
  inject Enhanced Input actions, play montages, log input with the monitor,
  run console commands, and read the editor's output log (gameplay_debug,
  run_console_command, get_output_log). Use before you test a gameplay change
  in PIE, or to read the log of a running editor.
toolkit_tools: [gameplay_debug, run_console_command, get_output_log]
---

# UE play testing in the editor

Three tools of the UE LLM Toolkit plugin cover testing a game inside the
editor:

| Tool | For |
|---|---|
| `gameplay_debug` | Play In Editor (PIE): sessions, scripted runs, input injection, captures, the input monitor, montages |
| `run_console_command` | one console command in the editor |
| `get_output_log` | the editor's log file, filtered |

They work only while the editor is running and the plugin is reachable.
`unreagent-iterate` covers the wait for the plugin.

This skill was written from the toolkit's C++ and bridge source. It has not
been exercised against a running editor. Starting and stopping PIE, drawing
frames, running a console command and passing injected input on to the game
are the engine's work. The skill says what the plugin asks the engine to do,
and marks what follows from that as unverified.

This skill has four sections:

| Section | Covers | Load it |
|---|---|---|
| `sequences` | `run_sequence` step by step, how a run ends, the manifest, `sequence_status`, and the older `execute_sequence` | before you write or start a scripted run (the step format is only there), and when a run does not start or end, or its manifest or captures are not what you expect |
| `input` | `inject_input` and continuous injection, the input monitor, montages | before your first injection, monitor or montage call, and when the game ignores an injected action or a value arrives changed |
| `log` | `get_output_log`: which file, the filters, the `since` cursor | before you pass `get_output_log` more than `filter` and `lines`, and when a read misses lines you expect |
| `failures` | the failure table (what you see, what it means, what to do) | when a call returns an error you cannot place, or what you see after a call is not what you expect |

## Before the first call

- **Tool names.** The bridge lists the tools as `unreal_gameplay_debug`,
  `unreal_run_console_command` and `unreal_get_output_log`.
- **Results through the bridge.** Success is the tool's message, a blank line,
  then JSON. By default the bridge runs the call as a task, so the JSON is a
  task envelope and the fields named here sit under its `data` key (skill
  `ue-tasks`). Failure is only `Error: <message>`.
- **Misspelt parameters are ignored without a word** on queued calls (skill
  `ue-tasks`). This bites here: the operations take `action_path`, but the
  steps of `run_sequence` take `action`.
- **One state for every caller.** The plugin keeps the running sequence, the
  continuous injections, the monitor and the log cursor in editor memory. Every
  agent and every session that calls the plugin shares them, and an editor
  restart loses them.
- **PIE starts and stops later.** `start_pie` and `stop_pie` only ask the
  editor, and return at once. Poll `pie_status` until `running` changes.
  Starting a session in a large project can keep the game thread busy for a
  while, so poll seldom (skill `ue-tasks`, "A 30 s timeout can crash the
  editor").
- **Expect the game to wait for every toolkit call.** Toolkit calls run on the
  game thread, and PIE most likely ticks on it too (not verified), so the game
  would not advance while a call runs.
- **Focus.** The plugin's notes say PIE runs at about 3 fps while the editor
  window is not in front. `run_sequence` asks the system to bring the PIE
  window to the front and runs `t.MaxFPS 60`. The manual operations do neither.

## gameplay_debug at a glance

`gameplay_debug` takes an `operation` string. The name is not case sensitive,
and five short forms exist: `run`, `status`, `start`, `stop` and `capture`.

| Operation | Required | Optional | Details |
|---|---|---|---|
| `start_pie` (`start`) | — | — | below |
| `stop_pie` (`stop`) | — | — | below |
| `pie_status` (`status`) | — | — | below |
| `capture_pie` (`capture`) | — | — | below |
| `run_sequence` (`run`) | `steps` or `sequence_file` | `name`, `settle_ms`, `output_dir`, `auto_capture_every_n_frames`, `max_duration_ms`, `take_record`, `take_slate` | section `sequences` |
| `sequence_status` | — | — | section `sequences` |
| `execute_sequence` | `sequence` | `capture_mode`, `capture_interval_ms`, `capture_every_n_frames`, `output_dir`, `name` | section `sequences` |
| `inject_input` | `action_path` | `value_x`, `value_y`, `value_z` | section `input` |
| `start_continuous` | `action_path` | `value_x`, `value_y`, `value_z` | section `input` |
| `update_continuous` | `injection_id` | `value_x`, `value_y`, `value_z` | section `input` |
| `stop_continuous` | `injection_id` | — | section `input` |
| `start_monitor` | — | `start_pie`, `interval_ms`, `axis_threshold`, `log_axes` | section `input` |
| `stop_monitor`, `monitor_status` | — | — | section `input` |
| `play_montage` | `montage_path` | `component_name`, `play_rate`, `start_section` | section `input` |
| `montage_jump_to_section` | `section_name` | `montage_path`, `component_name` | section `input` |
| `montage_stop` | — | `component_name`, `blend_out_time` | section `input` |

Every operation first needs the editor's world and otherwise fails with
`No world context available`. A required text parameter that is absent or
empty returns `Missing required parameter: <name>`. An unknown operation
returns `Unknown operation: '<op>'. Valid: run_sequence, start_pie, …` with the
whole list, and ` Did you mean '<name>'?` when one is close.

## PIE sessions

**`start_pie`** asks the editor for a PIE session and returns
`PIE session requested` with `requested: true` and the `note`
`PIE start is async. Poll pie_status until running.` When a session already
runs, it returns `PIE session already running` with `already_running: true`.

**`stop_pie`** clears every continuous injection, cancels a running sequence
(section `sequences`, "How a run ends"), asks the editor to end PIE, and
returns `PIE stop requested` with `stop_requested: true`. Without a session it
returns only `No PIE session running` and does nothing else: a sequence still
waiting for PIE to start is not cancelled.

**`pie_status`** returns `running`, and the message `PIE is running` or
`PIE is not running`. While PIE runs, it adds:

| Field | Present | Holds |
|---|---|---|
| `frame_number` | while running | the engine's frame counter |
| `game_time`, `real_time` | while the PIE world exists | seconds of game time, and of real time, in PIE |
| `pawn_location` | while the first player controls a pawn | `{x, y, z}` |
| `continuous_injections` | while running | how many continuous injections are active |

**`capture_pie`** captures the PIE viewport as a 1024x576 JPEG, quality 70,
stretched to that size like `capture_viewport`. The result has
`image_base64`, `width`, `height`, `format`, `frame_number`, `game_time` and
`viewport_type: "PIE"`, and the message is
`Captured PIE viewport at frame <n> (t=<seconds>)`. The image arrives as base64
text, not as a picture: skill `ue-viewport`, "Getting the picture". To get
JPEG files on disk, use the `capture` steps of `run_sequence`. Errors:
`PIE viewport not available. Is PIE running?`,
`PIE viewport has invalid size`, `Failed to read PIE viewport pixels`, and
`Failed to create image wrapper`, `Failed to set image data` or
`Failed to compress JPEG` when encoding fails.

## run_console_command

Runs one console command. `command` (string) is required.

Before anything runs, the command is checked, and the first rule that matches
fails the call:

| Rule | Error |
|---|---|
| no editor | `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?` |
| no editor world | `No active world. Open a level in the editor first` |
| absent or empty | `Missing required parameter: command` |
| more than 2048 characters | `Command exceeds maximum length of 2048 characters` |
| begins with a blocked command | `Command '<blocked>' is blocked for safety` |
| contains `;`, `\|` or `&&` | `Command chaining is not allowed` |
| contains a backtick, `$(` or `${` | `Shell escape sequences are not allowed` |

The blocked commands are matched against the start of the command, ignoring
case and surrounding spaces: `quit`, `exit`, `crash`, `forcegc`, `forcecrash`,
`debug crash`, `mem`, `memreport`, `obj`, `exec`, `savepackage`,
`deletepackage`, `net`, `admin`, `shutdown`, `restartlevel`, `open`,
`servertravel`, `toggledebugcamera`, `enablecheats`, `stat slow`, `gc.` and
`r.`. Anything beginning with one of them is refused, such as every rendering
variable `r.…`, although the tool's own description suggests `r.SetRes`. The
error names the blocked entry, not your command. To open a level, use
`open_level` (skill `ue-levels`).

The command runs against the **editor's world**, also while PIE runs. The
tool's description lists `slomo 0.5` as working in PIE only; whether such game
commands reach the PIE session from here has not been verified. A `console`
step of `run_sequence` runs against the PIE world instead (section
`sequences`).

The result has `command` and `output`, whatever the command wrote back, with
trailing whitespace removed; the message is `Executed command: <command>`.
**Success means only that the command passed the checks and was handed to the
engine.** The tool does not check whether the engine accepted it, and many
commands write to the log or the screen, not to `output`. Check the effect: in
the log (`get_output_log` with `filter`), with `capture_viewport`, or by
reading the state back.

The tool's description gives `stat fps`, `stat unit`, `show collision`,
`show bounds` and `ce <EventName>` as examples. The plugin's notes say never to
run `SaveAll`: it opens the editor's own confirmation dialog. Save through the
asset tools (skill `ue-assets`).

## Which log to read

| Source | What it reads | Use it when |
|---|---|---|
| `get_output_log` | the editor's log file, through the plugin: filters, up to 1000 lines, a `since` cursor | the editor is up and the plugin answers: errors after a call, `[PIE-DBG]` lines from a run (section `log`) |
| unreagent's `logs`, `service: "ue"` | the editor's console output as the launcher buffered it | the editor is starting, hung, crashed or stopped, or the plugin does not answer |
| `read_file` on `Saved/Logs/<ProjectName>.log` | the file `get_output_log` normally reads, on disk | a long log, the end of a crashed session; to search it, see below |

`logs` needs no plugin and keeps the last lines across editor restarts; skill
`unreagent-iterate` says how much it holds and what it leaves out.
`get_output_log` needs the plugin to answer, so it cannot tell you why the
editor died. After a crash, use `logs`, then `read_file` with a negative
`offset` on the log file (skill `unreagent-files`). To search a large log, use
`run_python` (skill `unreagent-runtimes`).

`get_output_log` takes the first log file it finds and names it in
`log_file`. Check it: it can be an older log, such as a backup of an earlier
session. Load section `log` before you pass more than `filter` and `lines`,
and when a read misses lines you expect.

## Typical sequences

**A scripted play test** (load section `sequences` first: the step format
is only there)

1. `stop_pie`, then `pie_status` until `running` is `false`.
2. `run_sequence` with `steps`, a fresh `name` and an absolute `output_dir`
   inside the project, such as `<project>/Saved/pie-debug/<name>`.
3. `sequence_status` until `state` is `completed` or `failed`.
4. `read_file` on `<output_dir>/manifest.json`. Look at the capture files with
   an image reader of your client.
5. `get_output_log` with `filter: "[PIE-DBG]"`, or `read_file` on the log file.

**Trying input by hand** (load section `input` before the first call)

1. `start_pie`, then `pie_status` until `running` is `true`.
2. `inject_input` for a single press, or `start_continuous` for a held stick.
3. `capture_pie` or `capture_viewport` (skill `ue-viewport`), and
   `pie_status` for the pawn's position.
4. `stop_continuous` for each id, then `stop_pie`.

**Watching the user play** (load section `input` before the first call)

1. `start_monitor` with `start_pie: true`. The user plays.
2. `stop_monitor`, then `get_output_log` with `filter: "[PIE-DBG]"`.

What the injection, monitor and montage operations do without telling you;
section `input` has each in full:

- **`value_x` defaults to 1 for every action type**, so an Axis2D call
  without values sends `(1, 0)`, not zero. Give every axis you mean.
- **`update_continuous` builds the value from that call alone**: an axis you
  leave out falls back to its default, so pass every axis each time.
- **Stop each continuous injection you started.** `stop_pie` ends them all;
  when PIE ends any other way they stop sending, but their ids stay, and
  `pie_status` still counts them in a later session.
- **A single `inject_input` is a single-frame one, and a digital action
  with a `Pressed` trigger needs the value held across frames** (both from
  the plugin's notes): use `start_continuous` and stop it again, or a `hold`
  step in `run_sequence`.
- **`start_monitor` says `Monitor started (PIE attached)` whenever PIE runs,
  even when attaching failed**: check `pie_attached`.
- **With `log_axes`, the monitor stops for good when PIE ends**, although
  the tool's description says it stays active. Expect its frame cap of 60 to
  stay (not verified) until `run_console_command` with `t.MaxFPS 0`.
- **Montage section names are not checked.** A `start_section` or
  `section_name` the montage does not have still returns success.

Load section `input` as well when the game ignores an injected action or a
value arrives changed: whether a mapping's triggers and modifiers apply to
injected values has not been verified.

## Failure modes

Load section `failures` when a call returns an error you cannot place, or
what you see after a call is not what you expect: it has the failure table
(what you see, what it means, what to do).

## Background in the plugin

- The plugin's `Resources/domains/debug.md` is its own guide to PIE
  automation, and skill `ue-context` says where it is. Where it differs from the
  code: its `hold` example passes `"value": true`, which no step reads; its
  "zero-value hold" between two presses does not release a digital action
  (section `sequences`); it says injected input "Bypasses IMC modifiers" while
  `gameplay_debug`'s description says it respects them (section `input`); and
  its advice to grep the log file means, here, `read_file` or `run_python`.
- `Resources/domains/tooling.md` has the `SaveAll` warning.

## See also

- `ue-viewport`: `capture_viewport`, and getting a picture out of base64.
- `ue-enhanced-input`: the Input Actions and Mapping Contexts that injected
  input goes through.
- `ue-levels`: opening the level to test in.
- `ue-tasks`: the task envelope and bridge timeouts.
- `ue-context`: where the plugin's domain documents are.
- `unreagent-iterate`: `logs`, and starting the editor.
- `unreagent-files`: `read_file` for manifests and log files.
- `unreagent-runtimes`: `run_python` for searching a large log.
