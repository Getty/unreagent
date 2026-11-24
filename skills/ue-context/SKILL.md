---
name: ue-context
description: >
  Load the UE LLM Toolkit's own reference notes: its UE API contexts through
  get_ue_context, and its domain documents, which that tool does not serve,
  from disk. Use before writing UE C++, Blueprint or editor logic from memory,
  or to pick the plugin document that fits a task. Also covers unreal_status,
  the bridge's check whether the plugin answers.
toolkit_tools: [get_ue_context, status]
---

# The toolkit's reference notes

The UE LLM Toolkit plugin ships two sets of Markdown notes. This skill tells
you how to reach them and which to pick. The notes themselves are the
reference. This skill does not repeat them.

| Set | What it holds | How to get it |
|---|---|---|
| contexts | UE API reference: classes, methods, specifiers, patterns, ten files | `get_ue_context`, or read the files |
| domains | how to get work done through the plugin: workflows, hard rules, known traps, seven files | read the files. `get_ue_context` does not serve them |

This skill was written from the plugin's bridge source and its documents. It
has not been exercised against a running editor.

This skill has three sections:

| Section | Holds | Load it |
|---|---|---|
| `status-result` | every field of an `unreal_status` result, reachable and not, and the `Currently: …` text in the tool's description | before you rely on anything in that result beyond `connected` |
| `injection` | the bridge's automatic context injection (`INJECT_CONTEXT`) and where unreagent sets it | when a tool result ends in a `## Relevant UE 5.7 API Context` block, or before you switch injection on |
| `failures` | the failure table: what you see, what it means, what to do | when a call from this skill returns an error or something you did not expect |

## Before the first call

- **The bridge answers, not the editor.** `get_ue_context` reads files next to
  the bridge and never reaches the editor. It needs no editor world and does
  not wait for the game thread.
- **It is listed only while the editor is reachable.** When the bridge cannot
  reach the plugin, its tool list holds `unreal_status` alone (see "status").
  Whether your client fetches the list again once the editor is up depends on
  the client. The files are on disk either way (see "Reading the files").
- **Tool name.** The bridge lists it as `unreal_get_ue_context`. Your MCP
  client may put its own server prefix in front of that.
- **Written for UE 5.7.** A result starts with `# UE 5.7 Context: …`, and a
  call with no arguments with `# Available UE 5.7 Context Categories`. How
  well a note fits another engine version is unverified.

## get_ue_context

| Parameter | Meaning |
|---|---|
| `category` | one category name from the table below, exactly: lower case, `blueprint` singular, `assets` plural |
| `query` | free text, matched against each category's keywords |

- **No arguments** lists the categories, each with its first five keywords.
- **`category` wins** when you pass both. An unknown or misspelt name, such as
  `Animation` or `blueprints`, is an error that lists the valid names.
- **`query`** loads every category that has a keyword inside your text,
  joined by `---` lines. With no match you get `No context found for query:
  "<query>". Try categories: …`, which is not an error.

**Prefer `category`.** The query matches keywords as plain substrings, so
short keywords hit inside other words. `build a level` loads `slate` as well
as `actor` and `parallel_workflows`, because slate's keyword `ui` is inside
"build". The result is about 27,000 characters, most of them unrelated. Call
without arguments once, then ask for the one category you need.

A tool result that ends in a `## Relevant UE 5.7 API Context` block carries
the bridge's automatic injection, off by default: load section `injection`.
Every context file but `parallel_workflows` opens with "This context is
automatically loaded when …". That is true only with the injection switched
on (`INJECT_CONTEXT=true` in the bridge's environment): without it, nothing
is loaded until you ask for the category.

## status

The bridge's second tool of its own, listed as `unreal_status`. It takes no
parameters and asks the plugin's HTTP server whether it answers.

**It is not unreagent's `status` tool.** That one belongs to the launcher and
reports its supervised processes, such as whether an editor process exists
and how the last one ended; it is the `status` that skills
`unreagent-iterate` and `ue-tasks` mean. `unreal_status` comes from the
plugin's bridge and says only whether the plugin answers. Tell the two apart
by the server prefix your MCP client shows.

The result is JSON text with `connected: true` while the plugin is
reachable, and an error whose text is JSON with `connected: false` and a
`reason` while it is not. Before you rely on any other field, or on the
`Currently: …` text in the tool's description, load section `status-result`.
Know one now: the `message`
`Unreal Editor connected. All tools operational.` means only that every tool
has a description; no tool was called.

To get the editor up, or to find out why it is down, use unreagent's tools
(skill `unreagent-iterate`), not this one.

## Categories

| `category` | Covers | Size |
|---|---|---|
| `actor` | spawning, lifecycle, iterating and destroying actors, components | 10 KB |
| `assets` | asset references, synchronous and asynchronous loading, Asset Manager, Asset Registry | 8 KB |
| `blueprint` | Blueprint graph classes, K2 nodes, pin types, creating and wiring nodes | 6 KB |
| `animation` | Animation Blueprints, state machines, transition rules, blend spaces | 5 KB |
| `character` | character classes, and the toolkit's `character` and `character_data` tools (skills `ue-character`, `ue-character-data`) | 10 KB |
| `material` | material classes and parameters, and the toolkit's `material` tool | 10 KB |
| `enhanced_input` | Enhanced Input classes, triggers, modifiers, key names | 8 KB |
| `replication` | property replication, conditions, RPCs, network roles | 7 KB |
| `slate` | Slate widgets, layout, custom widgets | 6 KB |
| `parallel_workflows` | how many toolkit calls run at once, timeouts, patterns for parallel calls | 11 KB |

- **None covers the editor's Python API.** For `execute_script`, see skill
  `ue-python`.
- **Where a context describes a toolkit tool**, the tool's own schema in the
  tool list is the authority. This skill did not check those passages.
- **`parallel_workflows` differs from the code in two places.** It budgets a
  subagent's calls against the 2-minute task limit, but each call through
  the bridge is a task of its own with 300 s. The 2 minutes apply only to a
  `task_submit` without `timeout_ms`. And four tasks may run at once, but they
  take turns on the editor's one game thread, so parallel calls save waiting,
  not editor time. Meanwhile each call's requests wait behind the other calls,
  and one that waits 30 s may crash the editor (skill `ue-tasks`, "A 30 s
  timeout can crash the editor").

## The domain documents

`domains/README.md` and `domains/meta.md` say that `get_ue_context` resolves
domain files along with the contexts. The bridge has no code that does so. No
query or category returns a domain document. Read them as files:

| File | When to read it |
|---|---|
| `meta.md` | first, for an overview of the domain notes |
| `code.md` | before writing C++ gameplay code |
| `blueprints.md` | before editing Blueprints or Animation Blueprints through the plugin |
| `assets.md` | before importing, retargeting or inspecting assets |
| `debug.md` | before testing in Play In Editor, injecting input or reading the output log |
| `tooling.md` | before extending the plugin |
| `README.md` | how domains and contexts relate |

Two habits in these documents do not carry over to MCP:

- **`ue-tool.sh call <tool> '<json>'`** is a shell wrapper from the toolkit's
  own repository. The MCP equivalent is the tool `unreal_<tool>` with the same
  JSON as its arguments.
- **"Run `help <tool>`"** belongs to that same wrapper. Over MCP, read the
  tool's schema in the tool list instead.

The documents also describe project domains: four files that a `prep one time
init` session writes to `domains/` in the project root, from
`prep-init-guide.md`. No bridge code loads them either. If they exist, read
them like the plugin's own.

## Reading the files

With the plugin installed in the project, the files are at:

| What | Path inside the project |
|---|---|
| contexts | `Plugins/UELLMToolkit/Resources/mcp-bridge/contexts/<category>.md` |
| plugin domains | `Plugins/UELLMToolkit/Resources/domains/<file>.md` |
| project domains | `domains/<file>.md` |
| the project-domain procedure | `Plugins/UELLMToolkit/Resources/prep-init-guide.md` |

Read them with your own file access, or with unreagent's `read_file` when the
project has file access switched on (skill `unreagent-files`). Its paths start
at the configured files root, which is normally the project directory.

## When a call fails

If `get_ue_context` or `unreal_status` returns an error, a result far larger
than expected or a result of another shape than this skill describes, or if
`get_ue_context` is missing from the tool list, load section `failures`: it
maps what you see to what it means and what to do.

## See also

- `ue-tasks`: what `parallel_workflows` gets wrong about timeouts and
  concurrency.
- `ue-python`: running editor Python, which no note covers.
- `ue-level-actors`, `ue-levels`, `ue-assets`: the tool skills that go with
  the `actor` and `assets` contexts and the `assets` domain.
- `unreagent-files`: `read_file` and the files root.
