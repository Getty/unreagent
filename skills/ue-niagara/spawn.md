# UE Niagara: spawn_system

Spawns the system in the level open in the editor through UE's
`UNiagaraFunctionLibrary::SpawnSystemAtLocation`, with auto-activate on and
the engine's defaults for the rest (no pooling, pre-cull check on).

| Parameter | Default | Meaning |
|---|---|---|
| `location` | `{"x": 0, "y": 0, "z": 0}` | world position |
| `rotation` | `{"pitch": 0, "yaw": 0, "roll": 0}` | rotation in degrees |
| `scale` | `{"x": 1, "y": 1, "z": 1}` | scale |
| `auto_destroy` | `true` | the component destroys itself when the system finishes |
| `label` | — | **do not use**: renames World Settings, below |

A missing key of `location`, `rotation` or `scale` keeps its default; a value
that is not an object is ignored.

The result has `actor_name` and `actor_label` (the actor that owns the new
component), `asset_path` (as you passed it), `location`, `rotation` and
`scale`. The message is `Spawned Niagara system '<system>' as '<label>'`.

## What the engine does (UE 5.8.4 source)

`SpawnSystemAtLocationWithParams` and `CreateNiagaraSystem`
(`Engine/Plugins/FX/Niagara/Source/Niagara/Private/NiagaraFunctionLibrary.cpp`,
read, not run):

- It creates a `UNiagaraComponent` with `RF_Transient`, whose outer is the
  world's **World Settings** actor, registers it with the world, places it
  with absolute location, rotation and scale, and activates it.
- **No actor is created.** `actor_name` and `actor_label` are World
  Settings'.
- **`RF_Transient` means the component is never saved.** Saving the level
  does not keep the effect; it is gone after the level is reopened or the
  editor restarts.
- With the pre-cull check, the engine may decide the effect would be culled
  at that location and create nothing; the tool then fails with
  `Failed to spawn Niagara system '<path>' in the level`. It also creates
  nothing when the process cannot render (a dedicated server or a null RHI).
- With `auto_destroy` `true`, a system that finishes removes its component.
  A looping system never finishes, so it stays until the level is reloaded.

## Consequences

- **Treat `spawn_system` as a preview**: to see what a system looks like at a
  place, or after a `set_parameter`. It is not how an effect is placed in a
  level; section `workflows` has the route for that.
- **`label` renames World Settings.** The tool calls `SetActorLabel` on the
  owning actor. Never pass it. If it was passed, tell the user, with the old
  and new label; this skill knows no verified way back.
- **Do not try to delete the spawned effect with `delete_actors`**: the only
  actor involved is World Settings. To clear previews, reopen the level
  (skill `ue-levels`) after saving your other work, or wait for a non-looping
  system to finish.
- Each call adds another component; repeated previews of a looping system
  pile up at their locations.
- The level is marked dirty although nothing saveable changed; a later
  `save_all` saves the level as it is otherwise.

## Errors, after the system has loaded

- `Editor not available. Is the Unreal Editor running with UELLMToolkit plugin loaded?`
- `No active world. Open a level in the editor first`
- `Failed to spawn Niagara system '<path>' in the level`
- `Niagara component spawned but has no owning actor`
