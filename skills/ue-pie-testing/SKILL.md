---
name: ue-pie-testing
description: >
  Play-test in Play In Editor through the UE LLM Toolkit: start and stop PIE,
  run scripted input sequences that capture frames and write a manifest,
  inject Enhanced Input actions, play montages, log input with the monitor,
  run console commands, read the editor's output log and assert on it
  (gameplay_debug, run_console_command, get_output_log). Use before you test
  a gameplay change in PIE, or to read the log of a running editor.
toolkit_tools: [gameplay_debug, run_console_command, get_output_log]
---

# UE play testing in the editor

Three tools of the UE LLM Toolkit plugin cover testing a game inside the
editor; the bridge lists them as `unreal_gameplay_debug`,
`unreal_run_console_command` and `unreal_get_output_log`:

| Tool | For |
|---|---|
| `gameplay_debug` | Play In Editor (PIE): sessions, scripted runs, input injection, captures, the input monitor, montages |
| `run_console_command` | one console command in the editor's world |
| `get_output_log` | the editor's log file, filtered |

They work only while the editor runs and the plugin answers
(`unreagent-iterate` covers the wait). Written from the toolkit's C++ and
bridge source (commit c1b6709); not exercised against a running editor.
Starting PIE, drawing frames, running a console command and passing injected
input on to the game are the engine's work: the skill says what the plugin
asks for, and marks what follows from that as unverified.

## Sections — load them

| Section | Covers | Load it |
|---|---|---|
| `recipes` | end-to-end loops with the exact calls: a scripted test with assertions on manifest and log, the manual start-inject-look-stop loop, the ladder for input that does not arrive, watching the user, a stat glance | before you run any test; it is the workflow |
| `sequences` | `run_sequence` step by step, the step format, how a run ends, the manifest, `sequence_status`, the older `execute_sequence` | before you write or start a scripted run, and when a run does not start, end, or write what you expect |
| `input` | `inject_input` and continuous injection, the input monitor, montages | before your first injection, monitor or montage call |
| `log` | `get_output_log`: which file, the filters, the `since` cursor, the lines a run writes | before you pass more than `filter` and `lines`, or assert on log lines |
| `console` | `run_console_command`'s checks and blocklist, `console` steps, commands useful for verification (stat, view modes, show flags) with their sources | before you run a console command |
| `failures` | what you see, what it means, what to do | when a call fails, or the result is not what you expect |

## Must not get wrong

- **Results through the bridge.** Success is the tool's message, a blank line,
  then JSON. By default the bridge runs the call as a task, so the fields
  named here sit under the envelope's `data` key (skill `ue-tasks`). Failure
  is only `Error: <message>`.
- **Misspelt parameters are ignored without a word** on queued calls (skill
  `ue-tasks`); the editor log still gets `Tool '<tool>': unknown params: <names>`.
  This bites here: the operations take `action_path`, the steps of
  `run_sequence` take `action`.
- **One state for every caller.** The running sequence, the continuous
  injections, the monitor and the log cursor live in editor memory. Every
  agent and session shares them; an editor restart loses them.
- **PIE starts and stops later.** `start_pie` and `stop_pie` only ask the
  editor and return at once. Poll `pie_status` until `running` changes, and
  poll seldom: a large project's start can keep the game thread busy (skill
  `ue-tasks`, "A 30 s timeout can crash the editor").
- **The game waits for every toolkit call.** Toolkit calls run on the game
  thread, and PIE most likely ticks on it too (not verified).
- **Focus.** The plugin's notes say PIE runs at about 3 fps while the editor
  window is not in front. `run_sequence` brings the PIE window to the front
  and sets `t.MaxFPS 60`; the manual operations do neither.

## gameplay_debug at a glance

`gameplay_debug` takes an `operation` string, not case sensitive, with five
short forms: `run`, `status`, `start`, `stop` and `capture`.

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
returns `Unknown operation: '<op>'. Valid: run_sequence, start_pie, …`, with
` Did you mean '<name>'?` when one is close.

## PIE sessions

**`start_pie`** asks for a session and returns `PIE session requested` with
`requested: true` and the `note` `PIE start is async. Poll pie_status until running.`
With a session running: `PIE session already running`, `already_running: true`.

**`stop_pie`** clears every continuous injection, cancels a running sequence
(without a manifest), asks the editor to end PIE, and returns
`PIE stop requested` with `stop_requested: true`. Without a session it returns
only `No PIE session running` and does nothing else: a sequence still waiting
for PIE is not cancelled. It does not stop the monitor.

**`pie_status`** returns `running` and the message `PIE is running` or
`PIE is not running`. While PIE runs, it adds:

| Field | Present | Holds |
|---|---|---|
| `frame_number` | while running | the engine's frame counter |
| `game_time`, `real_time` | while the PIE world exists | seconds of game and real time in PIE |
| `pawn_location` | while the first player controls a pawn | `{x, y, z}` |
| `continuous_injections` | while running | active continuous injections |

**Ready for input means `running: true` and `pawn_location` present.**
`running` alone says the session exists, not that a pawn is possessed.

**`capture_pie`** captures the PIE viewport as a 1024x576 JPEG, quality 70,
stretched to that size like `capture_viewport`. The result has
`image_base64`, `width`, `height`, `format`, `frame_number`, `game_time` and
`viewport_type: "PIE"`; the message is
`Captured PIE viewport at frame <n> (t=<seconds>)`. The image is base64 text,
not a picture (skill `ue-viewport`, "Getting the picture"); for JPEG files on
disk, use `capture` steps in `run_sequence`. Errors:
`PIE viewport not available. Is PIE running?`, `PIE viewport has invalid size`,
`Failed to read PIE viewport pixels`, and `Failed to create image wrapper`,
`Failed to set image data` or `Failed to compress JPEG`.

## run_console_command

Runs one command (`command`, required) against the **editor's world**, also
while PIE runs; a `console` step of `run_sequence` runs in the PIE world
instead. It refuses commands that begin with a blocked entry — among them
`open`, `exit`, `quit`, `obj`, `mem`, `net`, `gc.` and **every `r.…`
variable** — and any command with `;`, `|`, `&&`, a backtick, `$(` or `${`.
The result has `command` and `output`. **Success means only that the command
was handed to the engine**: many commands write to the log or the screen, not
to `output`, so check the effect. Never run `SaveAll` (plugin notes: it opens
a confirmation dialog). Load section `console` for the full checks and the
verification commands (`stat unit`, `viewmode …`, `show …`).

## Which log to read

| Source | Reads | Use it when |
|---|---|---|
| `get_output_log` | the editor's log file through the plugin: filters, up to 1000 lines, a `since` cursor | the plugin answers: errors after a call, a run's lines (section `log`) |
| unreagent's `logs`, `service: "ue"` | the editor's console output as the launcher buffered it | the editor is starting, hung, crashed or stopped |
| `read_file` on `Saved/Logs/<ProjectName>.log` | the file `get_output_log` normally reads | a long log, the end of a crashed session, a run's lines between its anchors |

`logs` needs no plugin (skill `unreagent-iterate`); `get_output_log` cannot
tell you why the editor died. After a crash use `logs`, then `read_file` with
a negative `offset` (skill `unreagent-files`); to search a large log,
`run_python` (skill `unreagent-runtimes`). `get_output_log` takes the first
log file it finds and names it in `log_file`: check it, it can be an older
session's.

**Do not assert on `since` reads**: from the code, each incremental read
drops the first new line (section `log`). Read with a plain `filter`, or the
file itself.

## The test loop

Load section `recipes` before you test: it has each loop with the exact
calls. In short, a scripted test:

1. `stop_pie` if PIE runs; `pie_status` until `running` is `false`.
2. `run_sequence` with `steps`, a fresh `name` and an absolute `output_dir`
   inside the project, `<project>/Saved/pie-debug/<name>`.
3. `sequence_status` until `state` is `completed` or `failed`.
4. `read_file` on `<output_dir>/manifest.json`: `status`, `error`,
   `steps_executed`, the captures, the `diag` positions.
5. `get_output_log` with `filter: "PIESequenceRunner"` and
   `min_verbosity: "Warning"` for skipped steps; your game's lines by
   `categories` and `filter`.
6. Look at the capture files; report each claim as passed or failed.

What injection, monitor and montages do without telling you (section
`input` has each in full):

- **`value_x` defaults to 1 for every action type**, so an Axis2D call
  without values sends `(1, 0)`. Give every axis you mean.
- **`update_continuous` builds the value from that call alone**: an axis
  left out falls back to its default.
- **Stop each continuous injection you started.** `stop_pie` ends them all;
  when PIE ends any other way they stop sending, but their ids stay, and
  `pie_status` still counts them in a later session.
- **A single `inject_input` is one frame**, and a digital action with a
  `Pressed` trigger needs the value held across frames (plugin notes): use
  `start_continuous` and stop it, or a `hold` step of 100 ms or more.
- **Digital `input` and `hold` steps always send `true`**: their `value_x`
  is ignored (section `sequences`).
- **`start_monitor` says `Monitor started (PIE attached)` whenever PIE runs,
  even when attaching failed**: check `pie_attached`.
- **With `log_axes`, the monitor stops for good when PIE ends**, although the
  description says it stays; its `t.MaxFPS 60` stays (not verified) until
  `t.MaxFPS 0`.
- **Montage section names are not checked**: a wrong one still succeeds.

## Background in the plugin

- `Resources/domains/debug.md` is the plugin's guide to PIE automation (skill
  `ue-context` says where). Where it differs from the code: its `hold`
  example passes `"value": true`, which no step reads; its "zero-value hold"
  between two presses does not release a digital action (section
  `sequences`); it says injected input "Bypasses IMC modifiers" while
  `gameplay_debug`'s description says it respects them (section `input`); and
  its advice to grep the log file means, here, `read_file` or `run_python`.
- `Resources/domains/tooling.md` has the `SaveAll` warning.

## See also

- `ue-viewport`: `capture_viewport`, and getting a picture out of base64.
- `ue-enhanced-input`: the Input Actions injection triggers (injection
  bypasses the Mapping Contexts, section `input`).
- `ue-game-framework`: game mode and pawn, when PIE has no pawn.
- `ue-levels`: opening the level to test in.
- `ue-tasks`, `ue-context`: the task envelope; where the plugin docs are.
- `unreagent-iterate`, `unreagent-files`, `unreagent-runtimes`: `logs`,
  `read_file`, `run_python`.
