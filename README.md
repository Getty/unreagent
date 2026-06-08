# unreagent

Ein schlanker **Launcher/Orchestrator** für Unreal-Engine-Projekte: startet und
überwacht den Unreal-Editor **und** einen Agenten (z.B. Claude Code) und bietet
dem Agenten einen **MCP-Server**, über den er den Editor steuern, Builds
auslösen, Logs lesen und Python-/Node-Code in vorbereiteten Umgebungen ausführen
kann.

Eine einzige, abhängigkeitsfreie `.exe` (nur Go-Standardbibliothek) — **von
Linux aus nach Windows cross-kompilierbar**.

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
│  UnrealEditor.exe                │ HTTP/JSON-RPC      │
│  Claude Code  ───────────────────┘ (--mcp-config)    │
└──────────────────────────────────────────────────────┘
```

## Bauen

Voraussetzung: Go (>= 1.19). Keine externen Module.

```bash
make windows   # -> dist/unreagent.exe  (Cross-Compile von Linux aus)
make linux     # -> dist/unreagent      (für lokale Tests)
make all
```

Oder direkt:

```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o dist/unreagent.exe ./cmd/launcher
```

## Einrichten

1. `config.example.json` → `config.json` (neben `unreagent.exe`) kopieren.
2. Pfade anpassen: `unreal.editor`, `unreal.project`, die `commands` (Build.bat-
   Pfad + Target-Name `<Projekt>Editor`).
3. `unreagent.exe` starten. Alternativer Config-Pfad via `-config <pfad>`.

## Konfiguration (Kurzüberblick)

| Sektion | Zweck |
|---|---|
| `unreal` | Editor-Pfad, Projekt, Restart-Policy (`never`/`on-failure`/`always`) |
| `agent` | Agent-Command + Args; `claudeIntegration` injiziert `--mcp-config` (+ Permission-Tool) automatisch |
| `commands` | benannte Einmal-Befehle (compile, package) für das MCP-Tool `run_command` |
| `mcp` | MCP-Server an/aus, `address` (Default `127.0.0.1:8765`) |
| `permissions` | Permission-Layer: `allow_all` / `allowlist` / `deny_all`, plus `allow`/`deny`-Regeln |
| `runtimes` | `python` (via `uv run`) und `node` für `run_python`/`run_node` |

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
