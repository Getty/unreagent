---
name: ue-cpp
description: >
  Write and change the project's C++ gameplay code (modules, classes,
  Build.cs, UCLASS/UPROPERTY/UFUNCTION) and get it into the editor: edit, stop,
  compile, start, verify. Use before touching anything under Source/, when a
  compile fails, or when a new C++ class does not show up in the editor.
---

# C++ in an unreagent project

The project's C++ lives under `Source/` and in code plugins under `Plugins/`.
Under unreagent it gets into the editor one way: **edit the files, stop the
editor, `compile`, start the editor, verify.** This skill covers that loop
for gameplay code, the reflection rules a compile does not forgive, and how
to read what fails. The tools are unreagent's own (`ue_stop`, `run_command`,
`ue_start`, `status`, `logs`, the file tools) plus a few toolkit tools to
check the result.

What this skill states about the engine is marked by its source. Facts the
launcher's own code depends on were read in the UE 5.8.4 source or measured
on Windows with 5.7.4 and 5.8.4; C++ syntax and conventions follow Epic's
documentation and templates, and were not compiled for this skill. The
toolkit's own C++ notes (`Plugins/UELLMToolkit/Resources/domains/code.md`, and
the `actor`, `character`, `replication` contexts of `get_ue_context`, skill
`ue-context`) hold gameplay patterns; this skill does not repeat them.

This skill has five sections:

| Section | Holds | Load it |
|---|---|---|
| `project-layout` | `Source/` and its files: targets, `Build.cs`, the module file, the `.uproject` `Modules` list; templates for a first C++ module in a Blueprint-only project and for a second module; dependencies | before you add a module, a target or a dependency, or give a Blueprint-only project its first C++ |
| `reflection` | `UCLASS`, `USTRUCT`, `UENUM`, `UINTERFACE`, `UPROPERTY`, `UFUNCTION`: the rules, the common specifiers, naming prefixes, garbage collection, what Blueprint can see | before you write or change a reflected type or member |
| `compile-errors` | how to read a failing `compile` reply: compiler, linker and header-tool errors, each common one with its usual cause and fix | when `compile` returns a non-zero exit |
| `live-coding` | Live Coding, Hot Reload and the full rebuild: what each can apply, why unreagent's loop is the full rebuild, the toolkit's `execute_script` `cpp` type | before you consider Live Coding, `script_type: "cpp"`, or a compile with the editor running |
| `failures` | the failure table for the whole loop: what you see, what it means, what to do | when a step of the loop does not give what this skill says |

## Decide first: C++, Blueprint, or a tool

| The job | Do it with |
|---|---|
| a value, a component, a Blueprint graph, an asset | the toolkit's tools (skills `ue-blueprints`, `ue-assets`, `ue-level-actors`), live at once, no compile |
| a new gameplay class, a system Blueprint cannot express or runs too slow in, a base class Blueprints derive from | C++, this skill |
| a one-off bulk edit in the editor | Python (skill `ue-python`), no compile |

A C++ change costs a stop of the editor (a kill: unsaved work is lost), a
compile of seconds to minutes, and a start. Batch C++ edits and compile once.

## The loop

1. **Read before you write**: the module's `Build.cs` and the headers next to
   the change. A class of another module needs that module in `Build.cs`
   (section `project-layout`).
2. **Edit** with your own file access, or with unreagent's `write_file` /
   `edit_file` (skill `unreagent-files`). The editor does not notice.
3. **Save the editor's work** through the toolkit (`asset` `{"operation": "save_all"}`,
   skill `ue-assets`), then **`ue_stop`**. It is a kill. Ask first if the user may
   have unsaved work in the editor.
4. **`run_command` `{"name": "compile"}`** (skill `unreagent-build`). Decide
   by the `exit` line. On a non-zero exit, load section `compile-errors`, fix,
   and compile again; leave the editor stopped meanwhile.
5. **`ue_start`**, then `status` some seconds later: the same `pid`,
   `restarts: 0` and no `lastExit` mean it stayed up (skill
   `unreagent-iterate`).
6. **Wait for the plugin**: `unreal_status` until `connected: true`.
7. **Verify** (below). Then report what you verified and how.

Never start the editor between a failed compile and the next good one: a
module without a binary stops the editor at startup, and the launcher keeps
restarting it until it gives up (skill `unreagent-iterate`, section
`failures`).

## What you must not get wrong

- **Stop before you compile.** With the editor running, a build can be
  refused (`exit 6`, `Unable to build while Live Coding is active`, when a
  Live Coding session is active; UE 5.8.4 source) or meet module binaries the
  running editor holds (outcome unverified). Stopping first rules both out.
- **No Live Coding in unreagent's loop.** The launcher starts the editor
  with `-unattended`, and Live Coding does not start by itself then (UE 5.8.4
  source). A compile from outside is the way; a reflected change needs a full
  restart anyway. Section `live-coding`.
- **Do not use `execute_script` with `script_type: "cpp"`** for project code.
  It needs an active Live Coding session (none under `-unattended`), never
  calls the code, and writes its file into
  `Source/<ProjectName>/Generated/UnrealClaude/`, inside the project's module,
  where the next `compile` builds it as well: a broken script file there
  breaks the project's build. `cleanup_scripts` deletes that directory (skill
  `ue-python`).
- **The `.generated.h` include comes last** among a header's includes, and
  `GENERATED_BODY()` opens the body of every `UCLASS` and `USTRUCT`. A
  reflected type in a header without its `#include "<File>.generated.h"`
  does not compile.
- **A `UObject` pointer member needs `UPROPERTY()`**, or the garbage
  collector neither keeps its object alive nor clears the pointer. In UE 5,
  write it as `TObjectPtr<UType>` in headers.
- **A class used by another module is exported** with the module's API
  macro: `class MYGAME_API AMyActor : public AActor` (the module name in
  capitals, then `_API`).
- **New files need no project-file step for the build.** UnrealBuildTool
  finds the `.h` and `.cpp` files under a module's directory itself;
  `GenerateProjectFiles.bat` only refreshes an IDE's solution (Epic's module
  documentation ties it to the IDE; not run here).
- **Changing `Build.cs`, `Target.cs` or the `.uproject` also needs the full
  loop**, and a `.uproject` change only takes effect in a new editor.

## Verify the change

A compile with `exit 0` proves the code builds, not that it works. In order:

1. **The editor stayed up** (`status`, step 5 of the loop) and its startup
   `logs` hold no `Error:` line from your module. A missing module binary
   names itself (`Incompatible or missing module`, or a module-not-found
   text and `Message dialog closed`).
2. **The class exists.** The reflected name drops the `A`, `U` or `F`
   prefix: `AMyPickup` in module `MyGame` is `/Script/MyGame.MyPickup`.
   - An actor class: `spawn_actor` `{"class": "/Script/MyGame.MyPickup"}`
     (skill `ue-level-actors`), then `delete_actors` the test actor. `Class
     not found: …` means the class is not in the loaded binary.
   - A base for Blueprints: `blueprint_modify` `create` with
     `parent_class: "/Script/MyGame.MyPickup"` (skill `ue-blueprints`).
3. **The behaviour.** Log from your code (`UE_LOG`, section `reflection`)
   and read it with `get_output_log` `{"filter": "<your category>"}` during
   Play In Editor (skill `ue-pie-testing`), or set a `UPROPERTY` with
   `set_property` and read it back.

## Reading a compile reply

`compile` returns `exit <code>` and the output, merged and cut (skill
`unreagent-build`). A failing reply holds its first 4 KiB and its end.

- The compiler's errors read `<file>(<line>): error C<nnnn>: <text>`, the
  linker's `<object>: error LNK<nnnn>: <text>`, the header tool's carry
  `Error:` with a file and line. Fix the first error first: later ones are
  often its follow-ons.
- One error can be hidden in the omitted middle; fix what you see and
  compile again. A smaller error set fits the reply whole.

Load section `compile-errors` for the common errors and their fixes.

## When something fails

Load section `failures` when a step of the loop does not give what this
skill says: a table from what you see to what it means and what to do.

## See also

- `unreagent-build`: `run_command`, the compile command, reading and cutting
  the reply, a `compile` that does not come back.
- `unreagent-iterate`: `ue_stop`, `ue_start`, `status`, `logs`, restarts,
  give-ups.
- `unreagent-files`: `read_file`, `write_file`, `edit_file` for `Source/`.
- `ue-context`: the toolkit's C++ notes (`domains/code.md`; the `actor`,
  `character`, `replication`, `slate` contexts).
- `ue-blueprints`, `ue-level-actors`, `ue-pie-testing`: checking the new
  class in the editor.
