# Catches, tool by tool

What a tool's description promises and what was measured on UE 5.8.4
(Launcher build CL 58902052, under `-unattended`). Paths are relative to
`Engine/Plugins/Experimental/Toolsets/` unless they start with another plugin.
Arguments are written as `call_tool`'s `arguments` object. The C++ tools take
their C++ parameter names with the first letter lowered: measured for
`captureTransform`, `options`, `pluginName` and `bEnabled`. The other
argument names below follow that pattern but were not seen in a call
(derived); `describe_toolset` has the real ones.

## Rules that hold for every tool

- **"needs a default value" means: pass that argument.** A parameter the
  caller leaves out is filled from the schema's `default`; one without a
  default fails the call with `Function "X", input param "y" needs a default
  value` (`ToolsetRegistry/Source/ToolsetRegistry/Private/ToolsetRegistry/`
  `ObjectFunctionToolCall.cpp:221-240`). The schema may still call it
  optional. Add the named argument and call again.
- **`isError` is not the whole story.** Some tools return their failure as a
  string in `returnValue` (Live Coding below). Read the value.
- **Double-encoded JSON.** A tool declared to return `FString` that
  "returns a JSON object" gives you `{"returnValue": "{\"tests\": …}"}`: parse
  `returnValue` a second time (measured for the automation tests).
- **Descriptions arrive in the editor's language** (measured: German). If you
  cannot read one, the parameter names and types in `inputSchema` still
  hold.
- **Names ignore case** (measured: `editortoolset.editorapptoolset` /
  `ispierunning` worked).

## Viewport capture — `EditorToolset.EditorAppToolset`

`CaptureViewport(CaptureTransform?, Annotations?, bShowUI = false)`
(`EditorToolset/Source/EditorToolset/Private/EditorAppToolset.h:225-249`).

- **`{}` fails** with isError `Function "CaptureViewport", input param
  "captureTransform" needs a default value`, although the header and schema
  call the transform optional (measured). The working route (measured):
  1. `GetCameraTransform` with `{}` (`EditorAppToolset.h:270`);
  2. `CaptureViewport` with `{"captureTransform": <that returnValue>}`.

  If the call then names `annotations` as missing, pass it as well; whether
  it does was not recorded (unverified). Annotations draw a world grid and
  actor labels into the image (`EditorAppToolset.h:129-151`: `GridSpacing`
  500, `GridExtent` 5000, `GridHeight` 0, `MaxLabelDistance` 5000,
  `ClassFilter`); the header says to use them only when you need the
  positions.
- **The image is base64 inside a text block**: `returnValue.image.data` with
  `mimeType` `image/png`, plus `cameraLocation`, `cameraRotation`,
  `cameraFOV`, `grid`, `labeledActors` (measured; fields:
  `EditorAppToolset.h:164-190`). Not an MCP image block, so the client does
  not show it as a picture: 0.86–0.94 MB of text per capture land in your
  context (measured, 1014×550). The toolkit's capture comes back as base64
  text through its bridge as well; only `run_sequence` captures write files
  (skill `ue-viewport`, "Getting the picture").
- Works under `-unattended`, also with `-RenderOffscreen` (measured: the
  default map rendered).
- `CaptureEditorImage` (whole editor window) and `CaptureAssetImage`
  (thumbnail) return images too (`EditorAppToolset.h:209-223`); their size
  and shape are unmeasured.

## Play In Editor — `EditorToolset.EditorAppToolset`

- `StartPIE` takes one argument `options` (`FPIESessionOptions`,
  `EditorAppToolset.h:27-66`): `bSimulate` (default false), `PlayMode`
  (in-viewport; out-of-process modes are downgraded), `StartTransform`,
  `WarmupSeconds` (default 2). `{"options": {}}` works (measured).
- **It blocks until PIE is up**: it completes after `PostPIEStarted` plus the
  warm-up (`EditorAppToolset.h:354-365`); measured 2.4–2.8 s, result
  `null`. It is an error when a session already runs.
- `StopPIE` `{}`: 0.2 s (measured). `IsPIERunning` `{}`: `true` / `false`.
- There is no input injection, input monitor or scripted sequence here; for
  those use the toolkit (skill `ue-pie-testing`).

## Live Coding — `LiveCodingToolset.LiveCodingToolset`

- **Not in `AllToolsets`**: the project must enable plugin
  `LiveCodingToolset` itself (measured: then listed with 1 tool).
- `CompileLiveCoding` `{}` compiles and waits (`LiveCodingToolset/Source/`
  `LiveCodingToolset/Public/LiveCodingToolset.h:24-27`).
- **Failure looks like success.** Without Live Coding enabled for the session
  it returns `{"returnValue": "Error: Live Coding is not enabled for this
  session. Enable it in Editor Preferences."}` **without** `isError`
  (measured; the strings: `LiveCodingToolset/Private/LiveCodingToolset.cpp:`
  `110, 115`). Treat a `returnValue` that starts with `Error:` as failed.
- Whether Live Coding can be on in a session the launcher started under
  `-unattended` was not measured. A full C++ build goes through the
  launcher's `run_command` (skill `unreagent-build`).

## Automation tests — `AutomationTestToolset.AutomationTestToolset`

Order (`AutomationTestToolset/Source/AutomationTestToolset/Public/`
`AutomationTestToolset.h:17-92`):

1. `DiscoverTests` `{}` once per session (`bForceRediscover` after
   reloading Python tests). Measured 0.8 s; the header warns of several
   seconds.
2. `ListTests` — `nameFilter`, `tagFilter`, `limit` (default 200, 0 = all).
   Returns a JSON **string**: `{"tests": [...], "total": N, "returned": n}`
   (measured).
3. `RunTests` `{"testNames": ["System.Core.Math.FColor.Smoke Test"]}` with
   full paths from `ListTests` — or `RunTestsByFilter` with
   `filterExpression` (`StartsWith:System.Engine`, `^Foo`, `Bar$`, a bare
   substring, `Group:Smoke`; joined by `+`). **It blocks until every test
   has finished** and returns the summary in the same call, again as a JSON
   string: `{"tests": [{"name", "state", "duration", "errors", "warnings"}],
   "passed", "failed", "skipped", "total", "duration"}` (measured, one test
   in 0.04 s). Run long suites in small batches: how long the client waits
   for one call was not measured.
4. `GetTestStatus` — `{"state", "numEnabled", "numComplete", "numPassed",
   "numFailed"}` (measured, JSON string). `GetTestResults` — the last run in
   detail. `StopTests` — aborts; a pending `RunTests` then completes with an
   error (header).

## Config settings — `ConfigSettingsToolset.ConfigSettingsToolset`

The Project and Editor settings pages, as the settings UI shows them
(`ConfigSettingsToolset/Source/ConfigSettingsToolset/Private/`
`ConfigSettingsToolset.h:12-127`).

- **Display names, not class names**: container `Project`, category
  `Project`, section `General` — not `GeneralProjectSettings` (measured).
  Walk `ListContainers` → `ListCategories` → `ListSections`.
- `GetSectionSchema`, then `GetSectionPropertyValues` with `propertyNames`.
- **`SetSectionProperties` takes `propertiesJson` as a string** that holds a
  JSON object (`ConfigSettingsToolset.h:96-100`), not as an object.
- **It saves at once, and it writes the whole section** (measured): setting
  only `Description` on Project / Project / General added 22 lines of
  defaults (`CompanyName=`, `CopyrightNotice=`, `ProjectVersion=1.0.0.0`,
  `bAllowClose=True`, …) to `[/Script/EngineSettings.GeneralProjectSettings]`
  in `Config/DefaultGame.ini`. `SaveSection` writes it again. Tell the user
  that the ini will grow, or change a single key in the ini file directly
  when they want the file kept small.
- `ResetSectionToDefaults`: unmeasured.

## Plugins — `PluginToolset.PluginToolset`

- **`SetPluginEnabled` does nothing you can see** (measured): `{"pluginName":
  "Text3D", "bEnabled": true}` returned `null`, no dialog, no log line, the
  `.uproject` untouched even after the editor quit, `IsEnabled` still false.
  The description promises a project-config change on the next restart
  (`PluginToolset/Source/PluginToolset/Private/PluginToolset.h:287-293`).
  The source sets the flag through `IProjectManager::SetPluginEnabled` and
  saves nothing (`PluginToolset.cpp:426-442`) — that it is lost for that
  reason is an inference. When plugin modification is off in Editor
  Settings it raises `PluginToolset: Plugin modification is disabled in
  Editor Settings.` instead (`PluginToolset.cpp:413-431`); whether that
  reaches you as `isError` is unverified.
  **To enable a plugin, the `.uproject`'s `Plugins` array has to change**, and
  the editor restarts — the user's decision.
- `CreatePlugin`, `UpdatePluginDescriptor`, `AddPluginDependency`,
  `RemovePluginDependency` change files on disk (source); unmeasured.
- The read tools (`ListEnabledPlugins`, `GetPluginInfo`, `IsEnabled`,
  `GetPluginDependencies`, `GetPluginForAsset`, …) are safe to use.

## Slate UI automation — `SlateInspectorToolset.SlateInspectorToolset`

Drives the editor's own interface (`SlateInspectorToolset/Source/`
`SlateInspectorToolset/Public/SlateInspectorToolset.h:92-181`): `Snapshot`
of the widget tree, then `Click`, `Type`, `PressKey`, `SelectOption`,
`Drag`, `FillForm`, `WaitFor` by widget reference; `Windows` lists the open
windows.

- `Windows` `{}` answered `[{"index": 0, "title": "<Project> - Unreal
  Editor"}]` under `-unattended`, also offscreen (measured). Nothing else in
  this toolset was measured.
- A click lands in the user's editor. Use it for UI that no other tool
  reaches, and say what you clicked.

## Output log — `EditorToolset.LogsToolset`

`GetLogEntries(category, pattern, maxEntries = 1000)`, `GetLogCategories`,
`GetVerbosity`, `SetVerbosity` (`EditorToolset/Source/EditorToolset/`
`Private/LogsToolset.h:29-58`). Unmeasured. Pass a `category` or `pattern`
and a small `maxEntries`; 1000 lines is a lot of context. The toolkit's
`get_output_log` (skill `ue-pie-testing`) reads the log file with a cursor.
