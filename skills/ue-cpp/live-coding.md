# C++ in an unreagent project: Live Coding, Hot Reload, full rebuild

The skill itself says to stop, compile and start. This section says why, and
what the other two ways would do.

## The three ways

| Way | What it does | What it can apply |
|---|---|---|
| full rebuild (unreagent's `compile` with the editor stopped) | builds the editor target's binaries; the next editor start loads them | everything |
| Live Coding (inside a running editor, Ctrl+Alt+F11) | rebuilds changed code and patches it into the running process | function bodies. Epic: "values set in the constructor implemented in the `.cpp` file will not update in existing instances of objects"; structural changes (new functions, new variables, the reflection macros) need Object Reinstancing and "without it, they risk crashes" |
| Hot Reload (the editor's fallback when Live Coding is off) | builds a new copy of the module DLL and loads it next to the old | Epic keeps it "as an alternative" to Live Coding. Do not use it under unreagent |

Quotes: Epic, "Using Live Coding to recompile Unreal Engine applications at
runtime" (read for this skill on the current page; earlier versions are
assumed to match, unverified).

The toolkit's `domains/code.md` lists changes it calls safe for Live Coding
and changes that need a full rebuild; it adds that changing a `USTRUCT`'s
layout under Live Coding crashes. Treat its list as advice, not as tested.

## Why unreagent's loop is the full rebuild

- **Live Coding does not start under `-unattended`**, which the launcher
  adds by default; `-LiveCoding` on the command line would start it anyway
  (UE 5.8.4 source, `LiveCodingModule.cpp`; not run). No tool here presses
  Ctrl+Alt+F11.
- **An active Live Coding session blocks a build from outside**: `exit 6`,
  `Unable to build while Live Coding is active` (UE 5.8.4 source; not run).
  If the user enabled it (`-LiveCoding` in `unreal.args`, `unreal.unattended`
  off, or by hand), stop the editor before every `compile`.
- **A patched editor is not what the next start loads.** Live Coding patches
  the running process; the binaries on disk come from a full build. A change
  verified only under Live Coding is not verified for the next session.
- **Reflected changes need a restart either way**: a new `UPROPERTY`,
  `UFUNCTION` or class, a changed struct layout, a `Build.cs` change.

## The toolkit's `execute_script` with `script_type: "cpp"`

It writes the script to `Source/<ProjectName>/Generated/UnrealClaude/`,
starts a Live Coding compile and waits up to 60 s. It needs Live Coding
enabled for the session (`Live Coding is not enabled. Press Ctrl+Alt+F11 to
enable.` otherwise, which is the case under `-unattended`), and it never
calls the compiled code (skill `ue-python`, section `script-types`; the
toolkit's `ScriptExecutionManager.cpp`). Two consequences:

- It is no way to run C++ in this setup.
- Its files stay inside the project's module directory, so the next
  `compile` builds them with the project. A script that does not compile
  breaks every later `compile` until it is removed: `cleanup_scripts`
  deletes that directory (and the Python scripts, and the history), or
  delete the one file.
