# unreagent build commands: commands the user defines

The list is fixed when the launcher starts. You cannot add a command, change
one, or pass it arguments through MCP. New or changed commands are a config
edit by the user, followed by a launcher restart. A command defined under
`commands:` in the config replaces the built-in one of the same name:

```yaml
commands:
  cook:
    description: "Cook content only"   # not shown to the agent
    command: "${ENGINE}/Engine/Build/BatchFiles/RunUAT.bat"
    args: ["BuildCookRun", "-project=${PROJECT}", "-noP4", "-cook", "-skipstage"]
    # dir: ""                           # working directory; empty = the launcher's own
```

`commands:` is a map. In `unreagent.local.yaml` it merges by name: an entry
there replaces that whole command, and every other command stays. The launcher
starts `command` directly with `args`, without a shell of its own, so pipes and
redirection do not exist. The environment is inherited from the launcher.
