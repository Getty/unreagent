# Python inside the editor: the editor's Python API

How a script is sent, approved, polled and judged is the skill itself. This
section is what Epic documents of the editor's own `unreal` module, the advice
for a bulk edit, and the script contents that trip the toolkit's error scan.

Every `unreal.*` call below is **documented by Epic, not run** for this skill:
each is cited to the Epic page it was read on (UE 5.7 documentation). Check an
unfamiliar call with `help(unreal.<Class>)` in a script before relying on it.

Pages:

- [S] "Scripting the Unreal Editor Using Python",
  https://dev.epicgames.com/documentation/en-us/unreal-engine/scripting-the-unreal-editor-using-python?application_version=5.7
- [EAL] Python API, `EditorAssetLibrary`,
  https://dev.epicgames.com/documentation/en-us/unreal-engine/python-api/class/EditorAssetLibrary?application_version=5.7
- [EAS] Python API, `EditorActorSubsystem`,
  https://dev.epicgames.com/documentation/en-us/unreal-engine/python-api/class/EditorActorSubsystem?application_version=5.7
- [SET] Python API, `ScopedEditorTransaction`,
  https://dev.epicgames.com/documentation/en-us/unreal-engine/python-api/class/ScopedEditorTransaction?application_version=5.7

## Facts from Epic's pages

- The `unreal` module exists only inside the editor, "not when your Project
  is running in the Unreal Engine in any mode". The editor embeds its own
  Python (3.11.8 in UE 5.7). [S]
- "While the Unreal Editor is running a Python script, its UI becomes
  blocked": the script holds the game thread, and every toolkit call waits
  behind it (skill `ue-tasks`). [S]
- Undo: `with unreal.ScopedEditorTransaction("<description>"):` puts the
  changes inside it into one undoable step. "Not every Editor operation is
  undoable." [S], [SET]
- Progress: `with unreal.ScopedSlowTask(total, "<label>") as task:` with
  `task.make_dialog(True)`, `task.enter_progress_frame(1)` and
  `task.should_cancel()`. [S]
- The editor puts `Content/Python` of the project and of each enabled plugin
  on `sys.path`, and runs any `init_unreal.py` found there at startup, after
  the startup level has loaded. [S]
- Asset work: Epic recommends `unreal.EditorAssetLibrary` (Editor Scripting
  Utilities plugin). [S]

## Documented calls

| Job | Call, as documented | Page |
|---|---|---|
| list assets under a folder | `unreal.EditorAssetLibrary.list_assets(directory_path, recursive=True, include_folder=False)` | [EAL] |
| load one | `unreal.EditorAssetLibrary.load_asset(asset_path)` | [EAL] |
| exists? | `unreal.EditorAssetLibrary.does_asset_exist(asset_path)` | [EAL] |
| asset data without loading | `unreal.EditorAssetLibrary.find_asset_data(asset_path)` | [EAL] |
| save | `unreal.EditorAssetLibrary.save_loaded_asset(asset_to_save, only_if_is_dirty=True)`, `save_directory(directory_path, only_if_is_dirty=True, recursive=True)` | [EAL] |
| rename, duplicate, delete | `rename_asset(source_asset_path, destination_asset_path)`, `duplicate_asset(…)`, `delete_asset(asset_path_to_delete)` | [EAL] |
| set a property | `obj.set_editor_property("<name>", value)` | [S] (its undo example) |
| actors in the open level | `unreal.get_editor_subsystem(unreal.EditorActorSubsystem).get_all_level_actors()` | [EAS] |
| selected actors | the same subsystem's `get_selected_level_actors()` | [EAS] |
| spawn, destroy | `spawn_actor_from_class(actor_class, location, rotation=…)`, `destroy_actor(actor_to_destroy)` | [EAS] |
| load a level | `unreal.get_editor_subsystem(unreal.LevelEditorSubsystem).load_level("<path>")` | [S] |

Before a script, check the dedicated tools: `asset_search`, `asset`,
`get_level_actors`, `set_property`, `spawn_actor` (skills `ue-assets`,
`ue-level-actors`) need no approval.

## A bulk edit: how to go about it

- **Read first** with the dedicated tools, so you know what the filter
  should hit.
- **Dry run.** A first script that only filters and prints the count of what
  it would change. Each run asks for approval again, so say in the
  description which run it is.
- **One transaction for the change**, so the user can undo the whole job in
  one step. A transaction does not save.
- **Report a count**, as one marker line (the skill itself, "Reporting from
  a script"), never the full list.
- **Save afterwards** (`asset` `{"operation": "save_all"}`, skill
  `ue-assets`), then **read back** with a dedicated tool or a read-only
  script.
- **Stay under 10 minutes.** The script's task has a 600 s limit, the wait
  for approval included, and the editor is blocked while it runs. A job over
  many thousand assets goes in batches (a folder per run), each its own
  approval.

## Pitfalls of the toolkit's error scan

The toolkit captures the text of each log line written while the script
runs, without the category and verbosity prefix (`FUnrealClaudeOutputDevice`
in `UnrealClaudeUtils.h` keeps only the message), and calls the script failed
when that text contains `Error:`, `Exception:`, `Traceback` or one of the
scanned exception names (`KeyError`, `TypeError`, …), in any case
(`ScriptExecutionManager.cpp`; the full list is in the skill itself,
"Failure"). In a script that means:

- **What decides is the message text.** `unreal.log_error("done")` does not
  fail the script; `print("Error: none found")` does, and so does any
  message, yours or the engine's, whose text holds a scanned word.
- **Names can trip it.** An asset or actor name containing a scanned name,
  such as `KeyErrorHandler` (it contains `KeyError`), fails a script that
  prints it. `ErrorWidget` does not: the check is for `Error:` with the
  colon. Print counts, or break the word before printing.
- **Engine lines count too.** A line the engine logs during the run whose
  text holds a scanned word fails the script even when your code is right.
- On a failure verdict, `data.output` does not reach you: read the run's
  lines with `get_output_log` `{"lines": 200}` and judge by your marker
  there. On success, judge by `data.output`, not by the verdict.
