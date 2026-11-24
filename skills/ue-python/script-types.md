# Python inside the editor: the other script types

The call's parameters, the description rule and the approval window are in
the skill itself, under "The call" and "Before the first call". "The C++
directory above" is `Source/<ProjectName>/Generated/UnrealClaude/` (section
`files-history`).

## Other script types

| `script_type` | What happens |
|---|---|
| `console` | one console command per line. Blank lines, `#` and `//` lines, and the `@Description:` line are skipped. Each line passes the same check as `run_console_command`. A blocked line becomes `Skipped blocked command: …` in the output, and the result is still `Executed <n> console commands` |
| `cpp` | written to the C++ directory above. The plugin then starts a Live Coding compile and waits up to 60 s. Whether that compile builds a newly added `.cpp` file is up to the engine and unverified. The plugin never calls the compiled code: success means only `C++ script compiled successfully via Live Coding`. A failure in the compile step starts with `Compilation failed. Fix these errors and call execute_script again:`, followed by the reason: up to five compile errors, `Live Coding is not enabled. Press Ctrl+Alt+F11 to enable.`, `Live Coding module not loaded`, or `Live Coding compilation timed out (60s)`. Two failures come without that prefix: `Failed to write C++ script file`, and `Live Coding not available - C++ scripts cannot be compiled` in an editor built without Live Coding. The tool description promises automatic retries. The code has none |
| `editor_utility` | not implemented. It still asks for approval, then fails with `Editor Utility script execution not yet implemented.` |
| anything else | runs as `console`, without a warning. Check the spelling |
