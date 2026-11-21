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

1. Resolve the path: `-config <path>`, else `unreagent.yaml` (then
   `unreagent.yml`) next to the executable (`findConfig`) — and make it
   absolute. The auto-detected or relative project path derives from the
   config directory and lands in placeholders and permission rules: a relative
   `-config` once made
   `${PROJECT_DIR}` `.`, and `Write(${PROJECT_DIR}/*)` then matched anything.
2. Decode `unreagent.yaml`. Unknown keys are an error (`KnownFields(true)`,
   typo protection); an empty file is fine.
3. If `unreagent.local.yaml` (the config's name with `.local` before the
   extension, `localPathFor`) exists, decode it **into the same struct**. This is
   a YAML-decode overlay with yaml.v3 semantics, not a deep merge (pinned by
   `internal/config/overlay_test.go`): a scalar wins; an absent key leaves the
   base value alone; a nested struct (`unreal:`, `agent.hermes:` …) is decoded
   field by field, so only the fields given change; a list **replaces** the
   base list; a map **merges by key**: base-only keys survive, and each key
   the local file gives replaces that entry's value whole, even when the value
   is a map or struct (`commands.hello: {command: x}` drops the base entry's
   `args` and `description`). So `{}` cannot clear a base map; `~` (null) can.
   Users expecting to append one item to `agent.args` will instead replace the
   list — remember this when documenting a new list- or map-valued field.
4. `applyDefaults()` — fills unset values and returns the built-in commands it
   added (`compile`, `package`, each only when the user defined none of that
   name). Note that the zero value is indistinguishable from "not set" for
   plain `bool` and `int`; fields whose default is `true` are therefore `*bool`
   (`Unattended`, `KillCrashReporter`, `PowershellTool`, `Window`) and read
   through `boolVal()`. Use `*bool` for any new option that defaults to on.
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
   not an error — the project values are then empty.
6. `resolveEngine` — env `UE_ROOT` (trimmed), then `engineRoot`; a relative
   value is made absolute against the **working directory**, not the config
   directory (unlike `unreal.project`). Otherwise `autodetect(project)`;
   finding nothing is an empty engine, not an error by itself.
7. Write the resolved project path back into `unreal.project`, then
   `substitute(...)` (next section). It drops a built-in command that needs an
   unresolved placeholder and returns its name in `Info.SkippedCommands`
   (sorted; `run()` logs a WARN per name); an unresolved placeholder in a
   *used* field is a load error naming the placeholder, a hint and the fields.
8. `validate()` — reject bad values here, not at the point of use: the enums
   (restart policies, `agent.onExit`, `permissions.mode`,
   `mcp.writeConfig[].format`), negative delays, `permissions.enabled` without
   `mcp.enabled`, a command without `command`. The `agent.*` checks run only
   when `agent.enabled`.

`unreal.*` always counts as used and `unreal.editor` defaults to
`${ENGINE}/Engine/Binaries/Win64/UnrealEditor.exe`, so without a resolvable
engine every `Load` fails unless `unreal.editor` is set literally — the
`hermes-setup` sub-command included, which calls `config.Load` as well.
`example/unreagent.yaml` (`editor: ping`) loads without one; that is pinned by
`TestExampleDemoConfigLoads`.

## Placeholders

| Placeholder | Resolution |
|---|---|
| `${ENGINE}` | env `UE_ROOT` → `engineRoot` (usually local.yaml) → the `.uproject`'s `EngineAssociation` (version → Launcher registry / `UE_<ver>` default path; other id e.g. `{GUID}` → registered source builds) → highest installed version, compared numerically (Launcher registry + Epic default paths); see `internal/config/engine.go` |
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
| `commands.<name>.command`, `.args`, `.dir` | always for the user's commands, a user-defined `compile` or `package` included |
| every string value in `mcp.extraServers.<name>`, at any depth (`substituteAny`; keys stay literal) | `mcp.enabled` |
| `mcp.writeConfig[].path` | `mcp.enabled` |

Everything else stays literal on purpose: `engineRoot` and `unreal.project`
(the placeholders are derived from them), names and keys (`agent.env` names,
`agent.hermes.serverName`, extra-server names), enums, `mcp.address`,
`mcp.token` (a token must never change under the user's hands) and command
descriptions. The doc comment on `substitute` lists both sides — keep it in
step with the code.

An unresolved placeholder (resolved to `""`) in a used field is a load error:
the result would be a garbage path such as
`/Engine/Binaries/Win64/UnrealEditor.exe` or, in an allow rule, `Edit(/*)`. An
unused field keeps whatever the replacement produces, with two exceptions: a
built-in command that needs the placeholder is removed and reported in
`Info.SkippedCommands`, and `files.root` is cleared, so that `-files` finds no
root and `run()` disables the file tools with a WARN instead of rooting them
somewhere wrong. CLI flags apply after `Load` and do not change what counted
as used: `-no-agent` does not rescue an unresolved placeholder in an enabled
agent's fields, and a `-write-mcp-config` path is not substituted.

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
   `files`, `commands`).
2. **Default** in `applyDefaults()` — `*bool` if it defaults to true.
3. **Substitution — yes or no, and when the field counts as used.** A path,
   command, argument, environment value or permission pattern goes into
   `substitute` with its used-condition (usually its section's `enabled`);
   anything else stays literal. Either way, update the doc comment on
   `substitute`. Nothing is substituted by default.
4. **Validation** in `validate()` for an enum, a range or a cross-field
   requirement — gated on the section's `enabled` where the section can be off,
   as the `agent.*` checks are.
5. **Use site** — usually `run()` in `cmd/launcher/main.go`, plus the file that
   owns the feature (`tools.go`, `scripts.go`, `bridge.go`, `mcpconfig.go`,
   `lifecycle.go`, `hermes.go`).
6. **`unreagent.example.yaml`** — commented, with the default stated.
7. **`README.md`** — the quick-reference table under "Configuration", plus prose
   if the option changes observable behavior.
8. **Test** in the style of `internal/config/load_test.go`: defaults applied,
   override honoured, invalid value rejected; a substituted field also gets its
   case in `TestSubstitutionCoversPathAndArgumentFields` and an unresolved case
   in `TestUnresolvedPlaceholderInUsedFieldFails`. `noEngine(t)` makes
   `${ENGINE}` unresolvable whatever the host has installed — it clears
   `UE_ROOT` and swaps `autodetect`, the auto-detection seam of
   `resolveEngine`; a test that needs an engine sets `UE_ROOT` instead (the
   default `unreal.editor` needs one).

Skipping 6 and 7 is the usual failure — an option nobody can discover is an
option that does not exist.

## Things that are auto-derived, deliberately

`unreal.editor`, the built-in `compile` and `package` commands and `files.root`
have placeholder-based defaults, and the project path is auto-detected, so that
a minimal config is genuinely minimal (see README "Setup"). Preserve that
property: a new option should have a default that works, not one that forces an
entry in every user's config.

## Secrets

`mcp.token` is generated by `hermes-setup` and written to
`unreagent.local.yaml` (git-ignored) via `persistGeneratedToken`, never to the
committed file. Any future secret follows the same path. `writeYAMLAtomic`
(temp + rename) is the house pattern for writing user config — a crash
mid-write must not leave a half-file.
