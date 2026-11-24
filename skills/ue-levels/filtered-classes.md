# Levels: what level_query leaves out

`list`, `find` and the choice between `level_query` and `get_level_actors`
are in the skill itself.

## What counts as infrastructure

The filter goes by the actor's class name:

| Rule | Classes |
|---|---|
| class name starts with | `Landscape`, `Foliage`, `InstancedFoliage`, `WorldPartition`, `WorldDataLayers`, `NavMesh`, `RecastNavMesh`, `AbstractNavData`, `NavigationData`, `HLOD`, `DataLayer`, `LevelInstance`, `PackedLevelActor` |
| class name is exactly | `WorldSettings`, `GameModeBase`, `GameMode`, `GameStateBase`, `GameState`, `PlayerState`, `GameSession`, `HUD`, `GameNetworkManager`, `AtmosphericFog`, `SkyAtmosphere`, `SkyLight`, `VolumetricCloud`, `ExponentialHeightFog`, `Note`, `GroupActor`, `LevelBounds`, `LevelScriptActor`, `DefaultPhysicsVolume`, `Brush`, `WorldPartitionReplay` |
| lights, filtered by `list` only | `DirectionalLight`, `PointLight`, `SpotLight`, `RectLight` |

A level-instance actor or a landscape never shows up here. Use
`get_level_actors` (skill `ue-level-actors`) for those.
