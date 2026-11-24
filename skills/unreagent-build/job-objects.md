# unreagent build commands: build processes and Job Objects

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
