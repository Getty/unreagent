# Failure modes

For `get_ue_context` and `unreal_status`, the two tools of the main skill.

| What you see | What it means | What to do |
|---|---|---|
| `Unknown category: <name>. Available categories: …` | not an exact name | copy one from the list. Names are lower case |
| `No context found for query: "<query>"` | no keyword matched | call with no arguments and pick a category |
| a result far larger than expected | the query matched several categories | ask for one `category` |
| `get_ue_context` missing from the tool list | the bridge could not reach the editor when the list was fetched | read the files. See skill `unreagent-iterate` for getting the editor up |
| `unreal_status` returns `connected: false` | the plugin's HTTP server did not answer the bridge; `reason` says how | check the editor with unreagent's own `status` (skill `unreagent-iterate`) |
| a `status` result with `services` and `commands` instead of `connected` | you called unreagent's `status`, not the bridge's | read it with skill `unreagent-iterate` |
| a document asks you to run `ue-tool.sh` or `help` | the toolkit's shell wrapper | call `unreal_<tool>` and read its schema |
