# character: failure modes

Each row: what you see, why, what to do. Exact error strings: section
`operations`, "Errors". Rows that blame a construction script or a runtime
write describe general UE behaviour, not exercised through these tools
(unverified); confirm by reading the Blueprint's graphs.

| Symptom | Cause | Fix |
|---|---|---|
| `Character not found: <name>` for a pawn you see in play | it is spawned by the game mode and exists only during play | change the Blueprint (section `workflows`, C) |
| `Character not found: <name>` for a placed actor | the name is neither its object name nor its exact label, the actor's level is not loaded, or it is not an `ACharacter` | `list_characters` and copy `name`; open the level (skill `ue-levels`) |
| `Actor name contains invalid character: '('` | labels like `Hero (2)` cannot be passed | use the object name from `list_characters` |
| `No movement parameters specified to modify` although you sent values | every value was a string such as `"600.0f"` or `"1e3"`, `null`, or a nested object | send plain JSON numbers |
| a parameter missing from `modified_params` | that value was unreadable; the others were applied | resend that one as a JSON number |
| the returned value differs from what you sent | silent clamp (section `operations`) | pick a value inside the range, or set the class default |
| the change is gone after reopening the level or restarting the editor | it was never saved | `asset` `save_all` after the change; verify by reopening (section `workflows`, B) |
| the change is gone after moving the actor or recompiling its Blueprint | a construction script sets the value and reran | change the Blueprint's construction script or class defaults instead (skill `ue-blueprints`) |
| no effect in play | the played pawn is spawned from the class, not your placed actor; or `BeginPlay` / an ability / an animation blueprint sets the value at runtime | change the class; read the Blueprint's graphs for writes to the property |
| change during PIE has no effect in the running game | the tool writes the editor world, not the PIE copy | stop PIE, change, start again (skill `ue-pie-testing`) |
| `braking_friction` changes nothing | `use_separate_braking_friction` is false, so ground friction is used | set `bUseSeparateBrakingFriction` on the Blueprint (class defaults only) |
| jump too low after raising `gravity_scale` | jump height falls with gravity | recompute `jump_z_velocity` (section `movement`, jump height) |
| character cannot climb stairs or ramps | `max_step_height` below the step, or slope above `walkable_floor_angle` | raise them; check `walkable_floor_z` follows |
| `velocity` and `movement_mode` look wrong during play | they are the editor actor's state | observe the game through skill `ue-pie-testing` |
| two actors share a label and the wrong one changed | the first match wins | use the object name |
| a pawn is missing from `list_characters` | not an `ACharacter` (Mover pawn, `DefaultPawn`) | this tool cannot reach it; use the Blueprint tools |

## Restoring

There is no undo: no transaction is recorded. Restore by sending the values
you read before the change with `set_movement_params`, then save. When you
did not read them first, the class defaults are the fallback:
`blueprint_query` on the class (skill `ue-blueprints`).
