---
name: unreagent-config
description: >
  How unreagent's YAML configuration works and how to extend it — load order and
  the local.yaml overlay, ${ENGINE}/${PROJECT} substitution, defaults,
  validation, and the artefacts that must be updated alongside a new field. Use
  when touching internal/config/ or adding a config option.
---

# unreagent — configuration

One config file lives next to the `.uproject` and is **committed by the user**;
a git-ignored `unreagent.local.yaml` overlays it with machine specifics. The
committed file must stay portable — that portability is the reason the
placeholder system exists.

## Load order (`config.Load`)

1. Resolve the path: `-config <path>`, else the first of `unreagent.yaml` and
   `unreagent.yml` next to the executable that is there, else
   `unreagent.yaml` (`findConfig(dir)`) — and make it absolute. Each
   candidate goes through `configPresent` (`os.Lstat`, then `ResolveLink`,
   the check `writeTarget` (`helpers.go`) uses): only a name that does not
   exist falls through. A symlink that leads to no file (dangling, a chain,
   a loop), or any other `Lstat` error, ends the search with a load error
   naming it, so `unreagent.yml` is never loaded in place of a broken
   `unreagent.yaml` link (`TestFindConfigRefusesBrokenLink`). A valid link
   counts under its own name: the config directory is the link's directory,
   and the overlay is named after the link and looked up next to it, not next
   to its target (`Load` takes `filepath.Dir` and `localPathFor` of the path
   it found, which `filepath.Abs` does not resolve). An explicit `-config`
   skips the check: a broken link there fails in `decodeYAML`'s read, without
   naming its target. The auto-detected or relative project path derives from
   the config directory and lands in placeholders and permission rules: a
   relative `-config` once made `${PROJECT_DIR}` `.`, and
   `Write(${PROJECT_DIR}/*)` then matched anything.
   No `-config` and neither candidate there is not an error: `load` sets
   `Info.Defaults` and takes the **defaults path** (section "Without a config
   file" below), which replaces step 2 and tightens steps 3 and 5.
2. Decode `unreagent.yaml`. Unknown keys are an error (`KnownFields(true)`,
   typo protection); an empty file is fine. In it and in the overlay, a
   second YAML document, even one holding only a comment, is an error naming
   the file and the line it starts on; a bare trailing `---` or `...` goes
   through (`EmptyDocument`, the same test as hermes-setup's
   `parseYAMLFile`). Where a comment lands is yaml.v3's call: on the line
   right after a final `---` it is the first document's, so the file loads
   (`TestLoadAcceptsEmptyTrailingDocument`).
3. If `unreagent.local.yaml` (the config's name with `.local` before the
   extension, `localPathFor`) is there, decode it **into the same struct**.
   Only a missing path is absent: the overlay goes through the same
   `configPresent` check as the step 1 candidates, so a broken link there is
   a load error too. This is a YAML-decode overlay with yaml.v3 semantics,
   not a deep merge (pinned by `internal/config/overlay_test.go`): a scalar
   wins; an absent key leaves the base value alone; a nested struct
   (`unreal:`, `agent.hermes:`, `skills:` …) is decoded field by field, so
   only the fields given change; a list **replaces** the base list; a map
   **merges by key**: base-only keys survive, and each key the local file
   gives replaces that entry's value whole, even when the value is a map or
   struct (`commands.hello: {command: x}` drops the base entry's `args` and
   `description`). So `{}` cannot clear a base map; `~` (null) can. Users
   expecting to append one item to `agent.args` will instead replace the list
   — remember this when documenting a new list- or map-valued field.
4. `applyDefaults()` — fills unset values and returns the built-in commands it
   added (`compile`, `package`, each only when the user defined none of that
   name). Note that the zero value is indistinguishable from "not set" for
   plain `bool` and `int`; fields whose default is `true` are therefore `*bool`
   (`Unattended`, `KillCrashReporter`, `PowershellTool`, `Window`,
   `skills.enabled`, `skills.mcp`) and read through `boolVal()`. Use `*bool`
   for any new option that defaults to on. `SkillsConfig` wraps its pair in
   `ViaMCP()` / `ViaFiles()`, which apply `enabled`; callers ask those, not
   the raw fields.
   For the plain-`int` delays, 0 means the default (`unreal.restartDelaySeconds`
   and `agent.restartDelaySeconds` 3, `agent.startDelaySeconds` 5), so the
   smallest configurable delay is 1 second — for the restart delay that floor
   keeps a crashing process out of a tight restart loop; a negative delay is
   rejected in step 8. A nil list is "not set" as well, and null in the overlay
   decodes to nil: `unreal.args: ~` restores the default
   `-stdout -FullStdOutLogOutput`, `unreal.args: []` clears it.
5. `resolveProject` — before the engine, because engine auto-detection reads
   the project's `EngineAssociation`. A `${` in `unreal.project` is a load
   error: the project placeholders are derived from that setting. An explicit
   `unreal.project` is absolute or relative to the **config directory** and
   must exist as a file (missing, or a directory, is a load error). Without
   one, the first `*.uproject` in the config directory (sorted); none at all is
   not an error — the project values are then empty. Both hold only with a
   config file: the defaults path has already insisted on exactly one.
6. `resolveEngine` — env `UE_ROOT` (trimmed), then `engineRoot`; a relative
   value is made absolute against the **working directory**, not the config
   directory (unlike `unreal.project`). Otherwise `autodetect(project)`, which
   returns `(root, warning)`; finding nothing is an empty engine, not an error
   by itself. For a blank (or non-string) `EngineAssociation` it searches above
   the project first: the nearest directory, starting at the one above the
   `.uproject`'s directory, that has `Engine/Binaries` and `Engine/Build`
   (`projectEngineRoot`). A value with `/` or `\` is that engine root,
   relative to the `.uproject`'s directory unless absolute. A `.uproject` that
   is not a JSON object is searched for nothing. The warning comes from
   `chooseEngine` when the project names an engine that is not usable — an
   association that names no usable engine, or a blank one inside an engine
   tree without an editor binary — and the highest-installed fallback picks
   one; `Load` appends it to `Info.Warnings`, and `run()` logs each entry as a
   WARN after the `Engine:` line (`hermes-setup` does not). `UE_ROOT` and
   `engineRoot` never warn.
7. Write the resolved project path back into `unreal.project`, then
   `substitute(...)` (next section). It drops a built-in command that needs an
   unresolved placeholder and returns its name in `Info.SkippedCommands`
   (sorted; `run()` logs a WARN per name); an unresolved placeholder in a
   *used* field is a load error naming the placeholder, a hint and the fields.
8. `validate()` — reject bad values here, not at the point of use: the enums
   (restart policies, `agent.onExit`, `permissions.mode`,
   `mcp.writeConfig[].format`), negative delays, `permissions.enabled` without
   `mcp.enabled`, a command without `command`, `skills.mcp` and
   `skills.materialize` both false (no skill would reach the agent; the error
   names both and `skills.enabled: false`), an `agent.hermes.serverName` that
   is not a plain YAML key (`validHermesServerName`: a character allowlist
   starting with a letter, which already rules out every YAML number and
   date, then anything YAML would read as a boolean or null; an empty name is
   already `unreagent` from step 4), an `mcp.address` the listener or the
   client URL cannot take (`validMCPAddress`) and an `mcp.allowedOrigins`
   entry that is no Origin header a browser sends (`validOrigin`):
   - `validMCPAddress` wants host:port with a port from 1 to 65535:
     `net.Listen` would take port 0 or a service name, but the client URL
     names the port as written. The host reaches that URL as written too (a
     wildcard aside, below), so it is empty (every interface), an IPv6
     address in brackets, or a name of ASCII letters, digits, `-`, `_` and
     `.`, which covers an IPv4 address. Brackets hold IPv6 only —
     `[127.0.0.1]`, `[localhost]` and `[]` are refused — and no zone:
     `[fe80::1%eth0]` would need `%25` in the URL, and a Node client takes
     none. User info, a path or a space (`user@host`, `a/b`) would fail the
     bind, but `hermes-setup` writes the URL with the server off. Every
     error ends with the hint
     `want host:port, e.g. 127.0.0.1:8765 or [::1]:8765`. An empty address
     is already `127.0.0.1:8765` from step 4.
   - `validOrigin`: the server compares each entry with the whole header,
     case-insensitively and without wildcards, so only that form can match —
     http and https as `scheme://host[:port]` with a port from 1 to 65535,
     no leading zeros and no default `:80`/`:443`, any other scheme with a
     host (a browser extension's) as exactly `scheme://host`, and an IP host
     as a browser writes it (`canonicalIP`: an http/https IPv4 as a dotted
     quad, IPv6 compressed, IPv4-mapped in hex, no zone). Whether an
     http/https host is an IPv4 address goes by its characters, as in the
     URL Standard (`browserIPv4`): a last label of decimal digits, or of
     `0x` and nothing but hex digits, makes it one, so
     `http://a.0xffffffffffffffffffff` is refused as a numeric host no
     browser accepts although the number is too large to parse, while
     `http://ide.example.0xg` is a name. A host outside brackets that holds
     one of the URL Standard's forbidden host code points
     (`forbiddenHostChars`; `url.Parse` lets `<`, `>` and `]` through) is
     refused for http(s) and extension schemes alike, and an http(s) host,
     a domain, takes no `%` either (`https://a%25b.example`); an
     extension's opaque host keeps it, and a bracketed host is left to
     `canonicalIP`. Refused too: an empty entry, a `*`, `null`, a missing
     scheme or host, `file`/`ftp`/`ws`/`wss`, user info, a path (even a
     trailing `/`), a query or fragment and a non-ASCII host (write
     punycode). The list has no default; empty or absent keeps the server's
     built-in loopback origins.

   The `agent.*` checks run only
   when `agent.enabled`, except serverName, which runs on
   `agent.hermes.enabled` alone — the only flag `hermes-setup` checks; the
   address check when `mcp.enabled` or `agent.hermes.enabled` (`hermes-setup`
   writes the client URL with the server off, too); the origins check only
   when `mcp.enabled`; the skills check only when `skills.enabled`.

   Clients never get `mcp.address` as written: only the listener does.
   `MCPConfig.ClientURL()` is the one builder of the client URL — the
   agent's `--mcp-config`, every `mcp.writeConfig` file, the Hermes entry and
   the url `hermes-setup` prints all take it, and nothing else builds the URL
   (no `"http://" + address`). An empty or wildcard host (`:8765`,
   `0.0.0.0:8765`, `[::]:8765`) listens on every interface but is nothing to
   connect to, so it becomes the loopback of its family, `127.0.0.1` or
   `[::1]`; any other host stays as written (pinned by `TestMCPClientURL`).

`unreal.*` always counts as used and `unreal.editor` defaults to
`${ENGINE}/Engine/Binaries/Win64/UnrealEditor.exe`, so without a resolvable
engine every `Load` fails unless `unreal.editor` is set literally — the
`hermes-setup` sub-command included, which calls `config.Load` as well.
`example/unreagent.yaml` (`editor: ping`) loads without one; that is pinned by
`TestExampleDemoConfigLoads`.

## Without a config file — the defaults path (k227)

Started next to a `.uproject` with no config, the launcher runs on built-in
defaults instead of failing. Code: `internal/config/defaults.go`, the branch
on `info.Defaults` in `load`; pinned by `internal/config/defaults_test.go`.

- **When.** Only with an empty `explicitPath` and when the path `findConfig`
  falls back to is not there (`configPresent`). Everything else is still a
  config-file start or an error: a `-config` that does not exist
  (`TestExplicitMissingConfigStillFails`), a broken `unreagent.yaml` link
  (`TestBrokenConfigLinkIsNotDefaults`), and an empty `unreagent.yaml`, which
  is a config file that switches nothing on (`TestConfigFileStartIsUnchanged`).
- **`Info.Defaults`** is true on this path and nowhere else. `Info.ConfigPath`
  is then the absolute path of the `unreagent.yaml` that was looked for and is
  **not there** — do not read or stat it on the strength of `ConfigPath`
  alone. Its directory is still the config directory, so the overlay name and
  everything in steps 5 to 7 derive from it as usual. `run()` logs the path
  and, on `Info.Defaults`, one INFO line per entry of `defaultsNotice(cfg)`
  (`cmd/launcher/main.go`). The first line is read off the loaded `cfg`, not
  off the defaults: agent and MCP server on or off, and the bridge named only
  when the `ToolkitServerName` entry is configured — the overlay is the one
  thing that can have switched agent or MCP server off on this path. The next
  names `unreagent init` (below). The last says that unreagent applies no
  permission policy on this path and is dropped when the overlay enabled
  permissions, so the notice is two or three lines (`TestDefaultsNotice` in
  `cmd/launcher/main_test.go`).
- **Exactly one `.uproject`** in that directory (`loadDefaults`): none, or
  several, is a load error naming the directory or the files, because there is
  no `unreal.project` to choose with. This rule is the defaults path's alone;
  step 5 keeps its lenient reading for a start with a config file.
- **`DefaultYAML(projectDir)`** returns
  `(doc []byte, bridges, warnings []string, err error)`: the defaults as a
  YAML document, the bridge entry points it found, and what kept the bridge
  search from looking everywhere, each warning a complete message for the
  caller to report. The document holds only what differs from
  `applyDefaults` (`defaultDoc`): `agent.enabled`, `agent.claudeIntegration`
  and `mcp.enabled` on, and the toolkit's `mcp.extraServers` entry
  (`ToolkitServerName`) when a bridge was found. Permissions are not in it, so
  they stay off. It is portable: the bridge path is written with
  `${PROJECT_DIR}`, which step 7 resolves like any extra-server string, and
  the entry's `node` command makes it a node-started server for bridge
  preparation (`hasNodeExtraServer`). `DefaultYAML` does not check for a
  `.uproject` itself — that check is `loadDefaults`'.
- **Decoded like a file.** `loadDefaults` hands those bytes to `decodeDoc`,
  the decoder behind `decodeYAML`, and steps 4 to 8 then run unchanged. There
  is no second way to build the default `Config`, so `unreagent init` (next
  bullet) can write the same bytes and get the same configuration; a start on the
  defaults and a start on the written document are compared whole in
  `TestDefaultYAMLRoundTrips`, which also pins the document's text. A default
  **value** that should differ on a start without a file goes into
  `defaultDoc`, never into a branch on `Info.Defaults` further down. The rule
  is about values, not checks: a check that refuses something on this path
  may branch on `Info.Defaults`, as the `unreal.project` refusal in the next
  bullet does.
- **`unreagent init`** (k228, `cmd/launcher/init.go`) is `DefaultYAML`'s
  second consumer. It loads the configuration first, and only on
  `Info.Defaults` — neither `unreagent.yaml` nor `unreagent.yml` there, and no
  `-check` — writes a header comment plus that document to `Info.ConfigPath`
  (`writeDefaultConfig`). The file is created exclusively, so one that
  appeared since the load is not overwritten either. With a config file in
  place it writes nothing. In every case it then prints a preflight of the
  loaded configuration; what that checks and what counts as blocking is in
  `preflight`, pinned by `cmd/launcher/init_test.go`
  (`TestInitWritesTheDefaultsAndACleanPreflight`,
  `TestInitNeverOverwritesAConfig`). A load error ends it before anything is
  written or checked — on the defaults that includes a missing engine
  (`TestInitWithoutEngineFails`).
- **The overlay still applies** (step 3): `unreagent.local.yaml` next to the
  executable is decoded over the defaults, so `engineRoot` there works without
  a config file (`TestDefaultsTakeLocalOverlay`). One key is refused: a
  non-blank `unreal.project` after the overlay is a load error naming the
  local file — without a config file the project is the `.uproject` next to
  the launcher, and the setting belongs in an `unreagent.yaml`.
- **Bridge search** (`findBridges`): a level-by-level walk below
  `projectDir/Plugins` for a directory holding `bridgeEntry`, bounded by
  `bridgeSearchDepth`; a directory that has the entry is a plugin and is not
  searched further. Results are relative to the project directory with
  forward slashes, the shallowest first and by name within a depth. A link to
  a directory is followed, and each real directory is read once: the walk
  keys the directories it has read by `filepath.EvalSymlinks`, so a link back
  up, or several links to one plugin, yield one bridge under the first path
  that reached it and no "several bridges" report
  (`TestDefaultsToolkitFoundOncePerDirectory`). A path that cannot be
  resolved is read without that guard. The
  document uses the first; more than one is the caller's to report, and
  `loadDefaults` does so through `Info.Warnings`. The search never makes the
  load fail: every problem it meets — a `Plugins` that is not a directory, a
  directory it cannot read — becomes an `Info.Warnings` entry, and the
  launcher starts with whatever bridge it still found, or none. Which
  directories the walk skips is in `findBridges` (`bridgeSearchSkips`) and the
  cases of `TestDefaultsToolkitDetection`; unreadable directories are pinned
  in `TestDefaultsUnsearchablePluginsWarns`, links to a plugin directory in
  `TestDefaultsToolkitBehindSymlink` — read the names, the depth and the
  wording there.

`load(explicitPath, dir)` is `Load` with the executable's directory as a
parameter (`Load` passes `exeDir()`), and the entry point for every test of
this path: `load("", t.TempDir())` after `writeUProject`, with `UE_ROOT` set
or `noEngine(t)`. `LoadIn(dir)` is its exported form for other packages,
`load("", dir)` and nothing else: no explicit path, so `findConfig(dir)`
decides between a config file there and the defaults path. Its only callers
are the tests in `cmd/launcher/init_test.go`; `initCommand` itself goes
through `Load` with its `-config` value.

## Placeholders

| Placeholder | Resolution |
|---|---|
| `${ENGINE}` | env `UE_ROOT` → `engineRoot` (usually local.yaml) → the engine the `.uproject` names by its `EngineAssociation` (version → Launcher installs / `UE_<ver>` default path; other id e.g. `{GUID}` → registered source builds; a path → that engine root; blank → the engine tree above the `.uproject`) → highest installed version, compared numerically (Launcher installs + Epic default paths), with a WARN via `Info.Warnings` when the named engine is not usable (step 6). Launcher installs: the `UE_<ver>` entries of `%ProgramData%\Epic\UnrealEngineLauncher\LauncherInstalled.dat` (what the engine reads), then the `HKLM\SOFTWARE\EpicGames\Unreal Engine\<ver>` keys as the fallback (`launcherEngines`); see `internal/config/engine.go` and skill `unreagent-ue` |
| `${PROJECT}` | full path to the `.uproject`: `unreal.project` if set, else the first `*.uproject` next to the config (step 5) |
| `${PROJECT_DIR}` | its directory |
| `${PROJECT_NAME}` | its filename without extension |

`substitute` applies one `strings.Replacer`, but only to the fields it names —
there is no generic walk over the struct. A new field gets **no** placeholder
support until it is added to `substitute` on purpose, together with the
condition under which it counts as *used*:

| Substituted field | Used when |
|---|---|
| `unreal.editor`, `unreal.args` | always |
| `agent.command`, `agent.args`, the `agent.env` values | `agent.enabled` |
| `agent.workdir` | always — it also roots the runtimes and the written MCP configs |
| `agent.hermes.configPath` | `agent.hermes.enabled` |
| `permissions.allow`, `permissions.deny` | `permissions.enabled` |
| `runtimes.python.uv`, `runtimes.python.project` | `runtimes.python.enabled` |
| `runtimes.node.node`, `runtimes.node.project` | `runtimes.node.enabled` |
| `runtimes.node.npm` | `runtimes.node.enabled`, or `mcp.enabled` with an extra server started by `node` (`hasNodeExtraServer` — the same test as `prepareMCPBridges` in `cmd/launcher/bridge.go`; change both together) |
| `files.root` | `files.enabled` |
| `skills.dir` | `skills.enabled` and `skills.materialize` (`ViaFiles`) |
| `commands.<name>.command`, `.args`, `.dir` | always for the user's commands, a user-defined `compile` or `package` included |
| every string value in `mcp.extraServers.<name>`, at any depth (`substituteAny`; keys stay literal) | `mcp.enabled` |
| `mcp.writeConfig[].path` | `mcp.enabled` |

Everything else stays literal on purpose: `engineRoot` and `unreal.project`
(the placeholders are derived from them), names and keys (`agent.env` names,
`agent.hermes.serverName`, extra-server names), enums, `mcp.address`,
`mcp.allowedOrigins`, `mcp.token` (a token must never change under the
user's hands) and command descriptions. The doc comment on `substitute`
lists both sides — keep it in step with the code.

An unresolved placeholder (resolved to `""`) in a used field is a load error:
the result would be a garbage path such as
`/Engine/Binaries/Win64/UnrealEditor.exe` or, in an allow rule, `Edit(/*)`. An
unused field keeps whatever the replacement produces, with three exceptions: a
built-in command that needs the placeholder is removed and reported in
`Info.SkippedCommands`, and `files.root` and `skills.dir` are cleared, so that
`-files` finds no root and `run()` disables the file tools with a WARN instead
of rooting them somewhere wrong, and `-no-skills-mcp` finds no directory and
`setupSkills` warns instead of writing. CLI flags apply after `Load` and do not
change what counted as used: `-no-agent` does not rescue an unresolved
placeholder in an enabled agent's fields, and a `-write-mcp-config` path is not
substituted.

`-no-skills-mcp` (`applyNoSkillsMCP`) sets `skills.mcp: false` and
`skills.materialize: true` after `Load` has overlaid the local file, so it wins
over `unreagent.yaml` and `unreagent.local.yaml` alike (pinned by
`TestRunNoSkillsMCPWinsOverTheLocalOverlay`); with `skills.enabled: false` it
only logs that it has no effect. `Load` leaves a relative `skills.dir`
relative; `setupSkills` resolves it against the agent working directory.

Substituting into permission patterns is safe only because every placeholder
value is absolute (steps 1 and 6): with `${PROJECT_DIR}` = `.`, the matcher
cleans the pattern of `Write(${PROJECT_DIR}/*)` to `*`, which grants
everything. A new placeholder must keep that property.

The order of the pairs in the replacer does not matter: every key ends in
`}`, so `${PROJECT}` cannot match inside `${PROJECT_DIR}` or `${PROJECT_NAME}`
— no key is a prefix of another. Keep the `${NAME}` form for any new one.

## Adding a field — the full checklist

A config option is not done when the struct compiles. All of these move
together:

1. **Struct field + `yaml:` tag** in `internal/config/config.go`, in the section
   it belongs to (`unreal`, `agent`, `mcp`, `permissions`, `runtimes`,
   `files`, `skills`, `commands`).
2. **Default** in `applyDefaults()` — `*bool` if it defaults to true.
3. **Substitution — yes or no, and when the field counts as used.** A path,
   command, argument, environment value or permission pattern goes into
   `substitute` with its used-condition (usually its section's `enabled`);
   anything else stays literal. Either way, update the doc comment on
   `substitute`. Nothing is substituted by default.
4. **Validation** in `validate()` for an enum, a range or a cross-field
   requirement — gated on the section's `enabled` where the section can be off,
   as the `agent.*` checks are. A value that works but is likely wrong is no
   load error: `Load` reports it in `Info.Warnings` as a complete message
   (step 6) — `internal/config` never logs.
5. **Use site** — usually `run()` in `cmd/launcher/main.go`, plus the file that
   owns the feature (`tools.go`, `scripts.go`, `bridge.go`, `mcpconfig.go`,
   `lifecycle.go`, `hermes.go`, `skills.go`).
6. **`unreagent.example.yaml`** — commented, with the default stated.
7. **`README.md`** — the quick-reference table under "Configuration", plus prose
   if the option changes observable behavior.
8. **Test** in the style of `internal/config/load_test.go`: defaults applied,
   override honoured, invalid value rejected; a substituted field also gets its
   case in `TestSubstitutionCoversPathAndArgumentFields` and an unresolved case
   in `TestUnresolvedPlaceholderInUsedFieldFails`. `noEngine(t)` makes
   `${ENGINE}` unresolvable whatever the host has installed — it clears
   `UE_ROOT` and swaps `autodetect`, the auto-detection seam of
   `resolveEngine` (`func(project string) (root, warning string)`); a test
   that needs an engine sets `UE_ROOT` instead (the default `unreal.editor`
   needs one).

Skipping 6 and 7 is the usual failure — an option nobody can discover is an
option that does not exist.

## Things that are auto-derived, deliberately

`unreal.editor`, the built-in `compile` and `package` commands, `files.root`
and `skills.dir` have placeholder-based defaults, and the project path is
auto-detected, so that a minimal config is genuinely minimal (see README
"Setup"). Preserve that property: a new option should have a default that
works, not one that forces an entry in every user's config.

## Secrets

`mcp.token` is generated by `hermes-setup` when none is set and written to
`unreagent.local.yaml` (git-ignored), never to the committed file, through
`localConfigWithToken` (`cmd/launcher/hermes.go`) and `writeFileAtomic`
(`cmd/launcher/helpers.go`):

- `localConfigWithToken` builds the new local file by a node edit
  (`parseYAMLFile`) that keeps comments, other keys and their order, and
  writes nothing. It first refuses a quoted `!!merge "<<"` key, or an `mcp` or
  `token` key written as an alias or with a tag, and `checkEdit` then confirms
  the result parses and changed nothing but `mcp.token`.
- `writeFileAtomic(path, data, tighten)` is the writer of every file the
  launcher writes that can hold a secret: hermes-setup's two files and the MCP
  configs (every `mcp.writeConfig` target, `-write-mcp-config`, the agent's
  `--mcp-config` file), which `writeMCPConfigs` and `writeAgentMCPConfig`
  write with `tighten` set (k178). It writes a temp file `<name>.tmp-*` in the
  target's directory, syncs it and renames it over the target, so a crash
  mid-write leaves the old file whole. On a failure it removes the temp file,
  leaves the old file as it was and returns `0`. It follows a symlink instead
  of replacing it, and `writeTarget` refuses a dangling one. A new file is
  created as `0600`; a replaced one keeps its permission bits. Every write
  returns the mode of a replaced file whose bits let group or others in
  (`mode & 0o077 != 0`), for the caller to report, and `0` for a closed file
  or a new one. With `tighten` set such a file also becomes `0600`, so the
  returned mode is the one it had. The mode checked is the written file's, the
  target when the path is a symlink, never the link's own. The mode goes on
  the temp file before the rename, so the data is never on disk under the old
  bits. Unix only: on Windows the file inherits its directory's ACL, the bits
  do not decide who can read it, and nothing is tightened or returned. The
  rename has consequences a caller inherits: the target's directory must be
  writable, since there is no in-place fallback, and another hard link to the
  old file keeps the old content, since the rename replaces the name, not the
  file.

`hermesSetup` builds every edit before it writes anything, then saves the
token before the Hermes config, so Hermes never holds a token the launcher
does not know. Only the token write into `unreagent.local.yaml` passes
`tighten`, and the command prints the old mode when it tightened the file. The
Hermes config holds the same bearer but belongs to another program, so its
write keeps its mode. When that write returns an open mode, the command warns,
naming the mode and `chmod 600 <hermesPath>` with the path as given (`chmod`
follows a link to the file whose mode was named). It warns on every run while
the file stays open, whether or not this run generated the token, since the
Hermes config always holds the bearer (pinned by
`TestHermesSetupWarnsOpenHermesConfig`). A token already in
`unreagent.local.yaml` is never rewritten, so its mode is never touched: one
the user wrote by hand, or one `hermes-setup` 0.0.9 or earlier saved with
`os.WriteFile(..., 0o600)`, which keeps an existing file's mode. An upgrade
user may therefore already hold the token in a `0644` file; the README tells
them to `chmod 600` it.

Any future secret follows the same pattern: the git-ignored local file, a
node edit checked by `checkEdit`, and `writeFileAtomic` with `tighten` set.
`localConfigWithToken` only ever sets `mcp.token` (its key checks and
`checkEdit` are pinned to `mcp`/`token`), so generalize it first rather than
copying it.
