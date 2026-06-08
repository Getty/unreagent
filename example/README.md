# unreagent — Demo zum sofort Ausprobieren

Diese `unreagent.yaml` startet **ohne Unreal Engine und ohne Agent**. Sie nutzt
`ping` als langlebigen Platzhalter-„Editor", damit du den Launcher und den
MCP-Server auf Windows in 30 Sekunden testen kannst.

> Das „LLM-Ding" (Claude Code) ist **nicht** enthalten — es ist eine separat
> installierte CLI. Die Demo lässt den Agenten deshalb bewusst aus. Wie du ihn
> dazuschaltest, steht unten.

## So geht's

1. `unreagent.exe` (aus dem [Release](https://github.com/Getty/unreagent/releases))
   in diesen Ordner legen — falls noch nicht da.
2. `unreagent.exe` doppelklicken (oder in einer Konsole starten).
3. Du siehst, wie der Platzhalter-„Editor" überwacht wird und der MCP-Server auf
   `http://127.0.0.1:8765/mcp` läuft.
4. **In der Konsole testen** (stdin-Befehle): `status` · `logs` · `c hello` ·
   `r ue` (neu starten) · `q` (beenden).
5. **MCP per curl testen** (zweite Konsole):
   ```bat
   curl -s -X POST http://127.0.0.1:8765/mcp -H "Content-Type: application/json" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}"
   ```

## Auf „echt" umschalten

1. Claude Code installieren (z.B. `npm install -g @anthropic-ai/claude-code`),
   sodass `claude` im PATH liegt.
2. In der `unreagent.yaml`:
   ```yaml
   agent: { enabled: true, command: claude, claudeIntegration: true }
   permissions: { enabled: true, mode: allow_all, deny: ["Bash(rm -rf *)"] }
   ```
3. Für ein echtes UE-Projekt: `unreal.editor`/`args` entfernen (Defaults aus
   `${ENGINE}` greifen) und die Dateien neben die `.uproject` legen. Details im
   Haupt-[README](../README.md).
