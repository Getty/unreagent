# PIE testing: when a call fails

"Must not get wrong" and "Which log to read" are in the skill itself; how
a scripted run ends is in section `sequences`, the console checks in section
`console`, the diagnosis ladder for input in section `recipes`.

## Failure modes

| What you see | What it means | What to do |
|---|---|---|
| plugin tools are missing or every call fails | the editor is not running or still loading | wait for the plugin (`unreagent-iterate`) |
| `No world context available` | the editor has no world yet | wait; open a level (skill `ue-levels`) |
| `PIE is not running. Call start_pie first.`, `No PIE session running.` | no PIE session, or it has not started yet | `start_pie`, then poll `pie_status` |
| `pie_status` stays `running: false` after `start_pie` | the editor has not started the session | poll again, seldom; then look for errors in the output log |
| `running: true` but no `pawn_location` | the session exists, the player has no pawn yet, or the game spawns none | poll until it appears; else skill `ue-game-framework` |
| `Enhanced Input subsystem not available in PIE` | the first player has no Enhanced Input subsystem | check the project's input setup (skill `ue-enhanced-input`) |
| an injection succeeds and the game ignores it | binding, the action's own trigger, or game logic (injection bypasses mapping contexts, section `input`) | section `recipes`, "Input does not reach the pawn" |
| a digital action fires only sometimes | a single-frame `inject_input` against a `Pressed` trigger (plugin notes) | `start_continuous` then `stop_continuous`, or a `hold` step of 100 ms or more |
| the pawn keeps moving after the test | a continuous injection still runs | `stop_continuous` per id, or `stop_pie` |
| the game barely moves | probably the editor window is not in front (plugin notes) | ask the user to bring it to the front, or use `run_sequence`, which does |
| everything runs at 60 fps or less after a run or the monitor | the plugin's `t.MaxFPS 60` stayed | `run_console_command` with `t.MaxFPS 0` |
| `A sequence is already running. …` | a run is in progress, or still waiting for PIE | `sequence_status`; section `sequences`, "Pitfalls" |
| `sequence_status` shows `state: ""` | the run is settling | poll again |
| a run fails at once with `PIE ended externally (step 0/<n>)` | PIE was running when the run started (unverified) | `stop_pie` and wait for `running: false` before `run_sequence` |
| `failed` with `Cancelled`, no manifest | `stop_pie` cancelled the run | run it again without stopping PIE |
| manifest lists a capture whose file is missing | the file write failed after the JPEG was made | check `output_dir` is writable and inside the project |
| `steps_executed` is complete but nothing happened | steps without a player controller or Input Action are skipped and still counted | `get_output_log`, `filter: "PIESequenceRunner"`, `min_verbosity: "Warning"` |
| `Command '<entry>' is blocked for safety` | the command begins with a blocked entry | use the dedicated tool, or ask the user to run it (section `console`) |
| `run_console_command` succeeds and nothing changes | the engine did not know the command, or it did not reach PIE | check the log; for PIE, a `console` step |
| `SkeletalMeshComponent '' not found on pawn` | no pawn, or a pawn without a skeletal mesh | check `pawn_location`; pass `component_name` |
| `Montage_Play returned 0 — …` | the montage does not fit the mesh's skeleton | skill `ue-anim-montage` |
| a montage section jump does nothing visible | section names are not checked | check the name; confirm with a capture or the monitor's `STATE` lines |
| a parameter seems to have no effect | its name is wrong, and queued calls do not warn | check the name, especially `action` versus `action_path`; the log has `Tool '<tool>': unknown params: <names>` |
| a `since` read misses a line you expect | `since` drops the first new line and never returns a single one | plain read with `filter` (section `log`) |
| `get_output_log` shows an old session | it read an older log file | check `log_file`; read the right file with `read_file` |
| two agents' tests interfere | the plugin's state is shared | test one at a time |
| everything is gone after an editor restart | the state lived in editor memory | start again |
