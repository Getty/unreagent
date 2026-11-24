# What a stop does to the process tree

On Windows, each editor instance runs inside a Job Object created with
`KILL_ON_JOB_CLOSE`. `ue_stop`, `ue_restart` and the launcher's own exit close
that job, and the operating system then terminates every process in it at once:
the editor, its ShaderCompileWorkers, a crash reporter it started, and any tool
it launched. When the editor exits on its own, its job is closed too, which
kills whatever it left behind. One exception, for a failed exit (non-zero)
with a crash reporter still in the job: the launcher keeps the job open until
the reporter has finished its report, for 30 s at most, so a crash report is
not cut short. The next start waits for that (`waiting` in `status`), and a
`ue_stop` during the wait cancels the held start but leaves the job to end
within those 30 s, so the reporter and whatever else that run left behind can
outlive the stop by up to 30 s. A clean exit closes the job at once, and so
does the launcher's own exit. (Windows only. The keep-open is not yet verified
on Windows: it is cross-compiled only. The old behaviour, a reporter killed
with the job, was measured.)

What follows from this for the unsaved work, the work in flight and
`run_command` builds is in the main skill. For orphans:

- **Nothing is left behind as an orphan, as long as the job exists.** No stray
  ShaderCompileWorker holds files locked for the next build. If the launcher
  could not create the job, or could not assign the editor to it, the launcher
  log shows `[ue] WARN job object could not be created: …` or
  `[ue] WARN job assign failed: … — the process runs unsupervised`. In that
  case, only the editor process itself is killed, and its children can
  survive.

A non-Windows launcher build, used only for development, kills just the direct
editor process. Its children survive there.
