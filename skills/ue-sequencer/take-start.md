# sequencer: take_start

Starts the Take Recorder. Before it starts, the code:

1. fails with `Take Recorder is already recording` if a take runs;
2. **clears the Take Recorder's sources**, including any the user set up in
   the Take Recorder panel;
3. sets the slate name, when `slate_name` is given and not empty;
4. sets two of the user's Take Recorder settings: save recorded assets on, and
   the countdown to 0 seconds;
5. asks the subsystem to start recording.

It adds **no source**. The result is `recording: true` plus `slate_name` when
given, with the message `Take Recorder started`.

What the success does not prove:

- **That anything is being recorded.** With the sources cleared and none
  added, what a take contains is the engine's business and has not been
  verified. The tool has no operation to add a source. To record the player's
  pawn in PIE, use `run_sequence` with `take_record` (skill `ue-pie-testing`).
- **That the recorder reached the recording state.** `recording: true` is
  written by the tool once the start call returned true; it is not read from
  the recorder. Call `take_status` and look for `state: "started"`.
- The two settings stay changed after the take. Whether the engine writes
  them to the user's config has not been verified. Tell the user if they use
  the Take Recorder themselves.

`Failed to start Take Recorder` means the subsystem refused; the code gives no
reason.
