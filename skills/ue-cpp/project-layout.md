# C++ in an unreagent project: the project's layout

The loop and the must-not-get-wrong facts are in the skill itself. This
section has the files a C++ project is made of, and templates for adding to
them.

Sources: Epic's module documentation (UE 5.7, "Unreal Engine Modules") for
the module files, the `Public`/`Private` split, the `.uproject` entry and the
dependency lists. The templates follow the shape of Epic's C++ project
templates for UE 5; they were not compiled for this skill. The launcher's
own facts (`compile` builds `<ProjectName>Editor`) are skill
`unreagent-build`.

## The files

| Path | What it is |
|---|---|
| `<Project>.uproject` | JSON. `Modules` lists the project's modules: `Name`, `Type` (`Runtime` for gameplay code, `Editor` for editor-only code), `LoadingPhase` (`Default` when absent) |
| `Source/<Project>.Target.cs` | the game target, `TargetType.Game` |
| `Source/<Project>Editor.Target.cs` | the editor target, `TargetType.Editor`: what the launcher's `compile` builds |
| `Source/<Module>/<Module>.Build.cs` | the module's rules: its dependencies, its precompiled-header mode |
| `Source/<Module>/<Module>.cpp` | the module's implementation macro |
| `Source/<Module>/Public/`, `Private/` | headers other modules may include, and everything else. Optional for the primary game module, whose files often sit flat in `Source/<Module>/` |
| `Plugins/<Plugin>/Source/<Module>/` | a code plugin's modules, listed in its `.uplugin` instead of the `.uproject` |
| `Binaries/Win64/`, `Intermediate/` | build output. Never edit; `Intermediate/` can be deleted for a clean build, which then takes longer |

A project whose `.uproject` has no `Modules` and that has no `Source/` is a
Blueprint-only project. Its `compile` fails: there is no editor target
(skill `unreagent-build`).

## Dependencies

A type from another module needs that module in the `Build.cs`:

- `PublicDependencyModuleNames`: the module's own public headers use the
  other module's types.
- `PrivateDependencyModuleNames`: only its `.cpp` files and private headers
  do.

Common modules: `Core`, `CoreUObject`, `Engine`, `InputCore` (the template
base); `EnhancedInput` (Input Actions, mapping contexts); `UMG`, `Slate`,
`SlateCore` (widgets); `AIModule`, `NavigationSystem`, `GameplayTasks` (AI);
`Niagara`; `PhysicsCore`. A missing one shows up as a linker error
(`LNK2019 unresolved external symbol`), or as a missing include when the
header lies in that module (section `compile-errors`). An editor-only module
(`UnrealEd` and the like) in a `Runtime` module's list builds for the editor
target and fails the game target, so `package` breaks; guard it with
`if (Target.bBuildEditor)` in `Build.cs` or move the code into an `Editor`
module (usual practice; not run here).

## A Blueprint-only project gets its first C++ module

The editor's "New C++ Class" wizard does this and also compiles, but it is a
dialog: under unreagent, write the files. With project name `MyGame` (use the
`.uproject`'s file name):

**The two target files: copy them from the installed engine.** The engine
ships a blank C++ template at `<Engine>/Templates/TP_Blank/Source/`
(`TP_Blank.Target.cs`, `TP_BlankEditor.Target.cs`, and the module
`TP_Blank/`). Copy the two `.Target.cs` files to `Source/MyGame.Target.cs`
and `Source/MyGameEditor.Target.cs`, and replace `TP_Blank` with `MyGame` in
the file names and inside (class names `MyGameTarget`, `MyGameEditorTarget`,
`ExtraModuleNames.Add("MyGame")`). They carry the settings versions that
engine expects: on UE 5.8.4, `BuildSettingsVersion.V7` and
`EngineIncludeOrderVersion.Unreal5_8`. Do not write `Latest` for either:
UnrealBuildTool's own comment warns it "may cause compatibility issues when
upgrading" (UE 5.8.4 source, `TargetRules.cs`). Read the engine directory
from the launcher log's `Engine:` line; when the template is out of reach
(no file access outside the project), ask the user to copy it.

`Source/MyGame/MyGame.Build.cs` (the template's `TP_Blank.Build.cs`, renamed,
has the same shape and also lists `EnhancedInput`)

```csharp
using UnrealBuildTool;

public class MyGame : ModuleRules
{
	public MyGame(ReadOnlyTargetRules Target) : base(Target)
	{
		PCHUsage = PCHUsageMode.UseExplicitOrSharedPCHs;
		PublicDependencyModuleNames.AddRange(new string[] { "Core", "CoreUObject", "Engine", "InputCore" });
	}
}
```

`Source/MyGame/MyGame.cpp`

```cpp
#include "Modules/ModuleManager.h"

IMPLEMENT_PRIMARY_GAME_MODULE(FDefaultGameModuleImpl, MyGame, "MyGame");
```

In `MyGame.uproject`, add (or extend) `Modules`:

```json
"Modules": [
	{ "Name": "MyGame", "Type": "Runtime", "LoadingPhase": "Default" }
]
```

Then the loop: `ue_stop`, `compile`, `ue_start`. A module listed in the
`.uproject` without a binary stops the editor at startup, so compile before
any start. The launcher's unbuilt-module check (`WARN project modules look
unbuilt`) runs at each launcher start and in `unreagent init -check`.

A project that already has target files keeps their versions; change them
only when the user upgrades the engine.

## A second module

`Source/MyGameTools/MyGameTools.Build.cs` as above (class `MyGameTools`), a
`MyGameTools.cpp` with `IMPLEMENT_MODULE(FDefaultModuleImpl, MyGameTools);`,
and a `.uproject` entry with its `Type`. That entry is enough: UnrealBuildTool
adds every `.uproject` module that compiles for the target being built (UE
5.8.4 source, `UEBuildTarget.cs`, `SetupProjectModules`). Do not add it to
the targets' `ExtraModuleNames`: an `Editor`-type module named in the game
target's list breaks `package`. A module that uses another lists it in its
`Build.cs`; the used module exports its classes with its API macro
(`MYGAMETOOLS_API`).

## A new class in an existing module

Two files, named after the class without its prefix: `MyPickup.h` and
`MyPickup.cpp` for `AMyPickup`. A minimal actor:

```cpp
// MyPickup.h
#pragma once

#include "CoreMinimal.h"
#include "GameFramework/Actor.h"
#include "MyPickup.generated.h"

UCLASS()
class MYGAME_API AMyPickup : public AActor
{
	GENERATED_BODY()

public:
	AMyPickup();

	UPROPERTY(EditAnywhere, BlueprintReadWrite, Category = "Pickup")
	float Value = 10.f;

protected:
	virtual void BeginPlay() override;
};
```

```cpp
// MyPickup.cpp
#include "MyPickup.h"

AMyPickup::AMyPickup()
{
	PrimaryActorTick.bCanEverTick = false;
}

void AMyPickup::BeginPlay()
{
	Super::BeginPlay();
}
```

The header lies where the module keeps its headers (`Public/` when it has
one); the `.cpp` in `Private/` or next to it. Section `reflection` has the
rules for what goes inside.
