# UE Niagara: spawn_system

Spawns the system in the level open in the editor, through UE's
`UNiagaraFunctionLibrary::SpawnSystemAtLocation`, and activates it.

| Parameter | Default | Meaning |
|---|---|---|
| `location` | `{"x": 0, "y": 0, "z": 0}` | world position |
| `rotation` | `{"pitch": 0, "yaw": 0, "roll": 0}` | rotation in degrees |
| `scale` | `{"x": 1, "y": 1, "z": 1}` | scale |
| `auto_destroy` | `true` | handed to the engine's spawn call as its auto-destroy flag. By the tool's description the spawned system then auto-destroys; the engine's auto-destroy removes it once it has finished (not verified) |
| `label` | — | label for the owning actor, see below |

A missing component of `location`, `rotation` or `scale` takes its default.

The result has `actor_name` and `actor_label` (the actor that owns the new
Niagara component), `asset_path`, `location`, `rotation` and `scale`. The
message is `Spawned Niagara system '<system>' as '<label>'`.

**Treat this operation as a preview, not as placing an actor.** UE's function
is made for effects at run time. It creates a Niagara component, not an actor
of its own. Which actor the component belongs to has not been verified for
this skill; it is probably one the level already has, such as World Settings.
Two consequences follow:

- **`label` renames that actor**, whichever it is. Spawn once without `label`
  and look at `actor_name` before you pass one.
- **The effect is probably not saved with the level.** The level is marked
  dirty, but whether the component survives a save and reload has not been
  verified. With `auto_destroy` left at `true`, an effect that does not loop
  probably removes itself once it has played.

For an effect that must stay in the level, check after a save and reload
whether it is still there. If it is not, tell the user: this tool has no
other way to place one.

Errors, after the system has loaded:

- `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?`
- `No active world. Open a level in the editor first`
- `Failed to spawn Niagara system '<path>' in the level`
- `Niagara component spawned but has no owning actor`
