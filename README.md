# unreagent

[![ci](https://github.com/Getty/unreagent/actions/workflows/ci.yml/badge.svg)](https://github.com/Getty/unreagent/actions/workflows/ci.yml)

Ein schlanker **Launcher/Orchestrator** für Unreal-Engine-Projekte: startet und
überwacht den Unreal-Editor **und** einen Agenten (z.B. Claude Code) und bietet
dem Agenten einen **MCP-Server**, über den er den Editor steuern, Builds
auslösen, Logs lesen und Python-/Node-Code in vorbereiteten Umgebungen ausführen
kann.

Eine einzige `.exe` — **von Linux aus nach Windows cross-kompilierbar**. Bis auf
einen vendored YAML-Parser nur Go-Standardbibliothek; `vendor/` ist eingecheckt,
Builds sind damit offline-reproduzierbar.

## Architektur

```
┌─────────────────── unreagent.exe ───────────────────┐
│  Supervisor                 MCP-Server (HTTP :8765)  │
│  • ue    (Job Object)  ◄───  status / logs           │
│  • agent (Job Object)        ue_start/stop/restart   │
│  • Restart-Policy + Backoff  run_command (compile…)  │
│  • Ring-Buffer-Logs          run_python / run_node   │
│       │                      approve (Permissions)   │
│       ▼                          ▲                    │
│  UnrealEditor.exe                │                    │
│   └─ In-Editor-MCP-Plugin        │                    │
│      (z.B. UE LLM Toolkit,       │                    │
│       HTTP :3000)                │                    │
│            ▲                     │                    │
│   Node-Bridge (stdio MCP)        │                    │
│            ▲                     │                    │
│  Claude Code ──────────┬─────────┘ unreagent-MCP      │
│                        └───────────► In-Editor-MCP    │
│                          (beide via --mcp-config)     │
└──────────────────────────────────────────────────────┘
```

Zwei MCP-Server bedienen den Agenten:
- **unreagent-MCP** (dieser Launcher, HTTP) — *Prozess*-Ebene: Editor starten/
  stoppen/neu starten, compilen, Logs, run_python/run_node, Permissions.
- **In-Editor-MCP** (z.B. [UE LLM Toolkit](https://github.com/ColtonWilley/ue-llm-toolkit),
  läuft *in* der Engine) — *Inhalts*-Ebene: Blueprints, Assets, Level, UE-Python.

Der Launcher gibt dem Agenten beide über `--mcp-config` mit (`mcp.extraServers`
in der Config). So muss der Agent nichts selbst einrichten.

## Bauen

Voraussetzung: Go (>= 1.19). Dependencies sind vendored (`vendor/`), Builds
laufen offline.

```bash
make windows   # -> dist/unreagent.exe  (Cross-Compile von Linux aus)
make linux     # -> dist/unreagent      (für lokale Tests)
make all
```

Oder direkt:

```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o dist/unreagent.exe ./cmd/launcher
```

## Sofort ausprobieren (Demo)

Im Release-Zip liegt eine fertige Demo-`unreagent.yaml`, die **ohne UE und ohne
Agent** läuft (`ping` als Platzhalter-„Editor"). Einfach das Zip entpacken und
`unreagent.exe` starten — der MCP-Server läuft dann auf
`http://127.0.0.1:8765/mcp`. Details: [`example/`](example/). In der Konsole:
`status`, `logs`, `c hello`, `r ue`, `q`.

> Das „LLM-Ding" (Claude Code) ist nicht enthalten — es ist eine separat zu
> installierende CLI. Die Demo lässt den Agenten deshalb aus; zum Scharfschalten
> `agent.enabled: true` setzen (siehe unten).

## Einrichten

Der Launcher wird **ins UE-Projekt gelegt** und die Config **mit-committed**:

1. `unreagent.exe` (aus dem Release) neben die `.uproject` legen — git-ignored.
2. `unreagent.example.yaml` → **`unreagent.yaml`** umbenennen, neben die
   `.uproject` legen und **ins Repo committen**. Diese Datei ist portabel.
3. `unreagent.exe` starten (Doppelklick oder Konsole). Alternativer Config-Pfad
   via `-config <pfad>`.

`unreagent.yaml` enthält **keine Maschinenpfade**. Editor, `compile` und
`package` werden automatisch abgeleitet. Eine minimale Config genügt:

```yaml
agent:       { enabled: true, command: claude, claudeIntegration: true }
permissions: { enabled: true, mode: allow_all, deny: ["Bash(rm -rf *)"] }
runtimes:    { python: { enabled: true }, node: { enabled: true } }
mcp:         { enabled: true }
```

### Portabilität: Pfade werden zur Laufzeit aufgelöst

| Platzhalter | Auflösung |
|---|---|
| `${ENGINE}` | Env `UE_ROOT` → `engineRoot` (local.yaml) → Auto-Detect der Epic-Standardpfade |
| `${PROJECT}` / `${PROJECT_DIR}` / `${PROJECT_NAME}` | `.uproject` neben der Config (Auto-Detect) |

Maschinenspezifisches gehört in eine **git-ignorierte `unreagent.local.yaml`**,
die als Overlay über `unreagent.yaml` gelegt wird:

```yaml
# unreagent.local.yaml  (NICHT committen)
engineRoot: "D:/UE/UE_5.7"                 # falls Auto-Detect scheitert
agent: { command: "C:/.../claude.cmd" }    # falls claude nicht im PATH
```

Empfohlener `.gitignore`-Eintrag im UE-Projekt:

```
unreagent.exe
unreagent.local.yaml
```

## Konfiguration (Kurzüberblick)

| Sektion | Zweck |
|---|---|
| `agent` | Agent-Command + Args; `claudeIntegration` injiziert `--mcp-config` (+ Permission-Tool) automatisch |
| `permissions` | Permission-Layer: `allow_all` / `allowlist` / `deny_all`, plus `allow`/`deny`-Regeln |
| `runtimes` | `python` (via `uv run`) und `node` für `run_python`/`run_node` |
| `mcp` | MCP-Server an/aus, `address` (Default `127.0.0.1:8765`) |
| `unreal` | *optional* — Editor-Args, Restart-Policy (`never`/`on-failure`/`always`) |
| `commands` | *optional* — eigene Einmal-Befehle (compile/package sind eingebaut) |
| `engineRoot` | *meist nur in local.yaml* — Engine-Pfad-Override |

### Keine Prozess-Leichen (Windows)

Jede laufende Instanz steckt in einem **Windows-Job-Object** mit
`KILL_ON_JOB_CLOSE`. Schließt sich das Handle — durch sauberes Stoppen,
Neustart **oder Absturz/Hard-Kill des Launchers** — beendet das Betriebssystem
den **kompletten Prozessbaum** (inkl. ShaderCompileWorker etc.). OS-erzwungen,
ohne externe Abhängigkeit. Auf Linux (Entwicklung) wird nur der direkte
Kindprozess beendet.

### Permission-Layer

Bei `permissions.enabled` + `agent.claudeIntegration` startet der Launcher den
Agenten mit `--permission-prompt-tool mcp__unreagent__approve`. Jede
Permission-Abfrage von Claude Code läuft dann über das `approve`-Tool, das nach
der konfigurierten Policy entscheidet. `mode: "allow_all"` = „alles automatisch
freigeben" (mit `deny`-Ausnahmen wie `Bash(rm -rf *)`).

> Hinweis: Das exakte **Input**-Schema von `--permission-prompt-tool` ist von
> Anthropic nicht offiziell dokumentiert. Der `approve`-Handler liest die
> Tool-Felder defensiv (`tool_name`/`toolName`/`name`, `tool_input`/`input`/
> `arguments`). Sollte ein Claude-Code-Update das Format ändern, ist nur dieser
> eine Handler in `cmd/launcher/main.go` anzupassen.

### Runtimes für den Agenten

`run_python` führt Code über `uv run python` aus — uv baut/synct das venv
automatisch aus `pyproject.toml`/`requirements` und installiert bei Bedarf die
richtige Python-Version. `run_node` führt JS im Projektkontext aus. Der Agent
muss die Umgebung **nicht selbst analysieren oder einrichten**; die Anleitung
dazu steht in der MCP-Tool-Beschreibung und ist damit automatisch im Kontext.

## MCP-Tools

| Tool | Funktion |
|---|---|
| `status` | Prozess-Status + verfügbare Befehle |
| `ue_start` / `ue_stop` / `ue_restart` | Editor-Lifecycle |
| `run_command` | vorkonfigurierten Befehl ausführen (compile, package) |
| `logs` | letzte Ausgabezeilen eines Service |
| `run_python` / `run_node` | Code in vorbereiteter Umgebung ausführen |
| `approve` | Permission-Prompt-Tool für Claude Code |

## Manuelle Bedienung (stdin)

`status` · `r [name]` (neu starten) · `start <name>` · `stop <name>` ·
`c <name>` (Befehl ausführen) · `q` (beenden)

## Manueller MCP-Test

```bash
curl -s -X POST http://127.0.0.1:8765/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```
