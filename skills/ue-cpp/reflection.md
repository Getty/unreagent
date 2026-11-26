# C++ in an unreagent project: reflection

UnrealHeaderTool (UHT) reads the macros below before the compiler runs and
generates the `.generated.h` code that makes a type visible to the editor,
Blueprint, serialization and the garbage collector. A mistake here fails the
build in UHT, or compiles and misbehaves at runtime.

Sources: Epic's documentation of gameplay classes, properties, UFunctions,
structs and interfaces (UE 5.x) and the toolkit's `domains/code.md`. Not
compiled for this skill. The toolkit's notes add patterns (components,
character movement, replication): skill `ue-context`.

## Names

| Prefix | For | Reflected name |
|---|---|---|
| `A` | classes derived from `AActor` | without the prefix: `AMyPickup` is `MyPickup` |
| `U` | classes derived from `UObject` (components, assets, subsystems) | without the prefix |
| `F` | structs and plain C++ classes | a `USTRUCT` without the prefix |
| `E` | enums | `UENUM` keeps its name |
| `I` | the interface class of a `UINTERFACE` (its `U` twin is the reflected one) | — |
| `T` | templates | not reflected |

UHT checks the `A` and `U` prefixes against the base class and fails the
build on a mismatch (the wording of that error: unverified). A class's path
in the editor is `/Script/<Module>.<Name without prefix>`.

## Types

```cpp
UCLASS(Blueprintable)                 // Blueprint can derive from it
class MYGAME_API AMyPickup : public AActor
{
	GENERATED_BODY()                  // first line of the body
	...
};

USTRUCT(BlueprintType)
struct FPickupInfo
{
	GENERATED_BODY()

	UPROPERTY(EditAnywhere, BlueprintReadWrite)
	int32 Count = 0;
};

UENUM(BlueprintType)
enum class EPickupKind : uint8        // uint8 for Blueprint use
{
	Health,
	Ammo
};

UINTERFACE(MinimalAPI, Blueprintable)
class UInteractable : public UInterface { GENERATED_BODY() };

class MYGAME_API IInteractable
{
	GENERATED_BODY()
public:
	UFUNCTION(BlueprintNativeEvent, BlueprintCallable)
	void Interact(AActor* Instigator);
};
```

- The header's last include is `#include "<HeaderName>.generated.h"`, and
  the file name must match the header's.
- One header may hold several reflected types; the `.generated.h` is per
  header.
- A reflected type cannot be nested in another class, and `UCLASS` types are
  not templates.
- Every `UCLASS` constructor runs for the class default object (CDO) at
  load, before any world exists: no spawning, no world access, no
  `GetWorld()` work in it. Use `BeginPlay` or `PostInitializeComponents`.

## Properties

| Specifier | Effect |
|---|---|
| `EditAnywhere` | editable on the class defaults and on each placed instance |
| `EditDefaultsOnly` | editable on the class defaults only (tuning values) |
| `EditInstanceOnly` | editable on placed instances only |
| `VisibleAnywhere` | shown, not editable; the usual one for a component pointer |
| `BlueprintReadWrite` / `BlueprintReadOnly` | readable (and writable) from Blueprint graphs; not allowed on a `private` member without `meta = (AllowPrivateAccess = "true")` |
| `Category = "A\|B"` | groups it in the Details panel and the Blueprint menus; give every exposed member one (UHT insists on it only in engine modules) |
| `Transient` | not saved |
| `Replicated`, `ReplicatedUsing = OnRep_X` | network replication; needs `GetLifetimeReplicatedProps` with `DOREPLIFETIME` (context `replication`) |
| `meta = (ClampMin = "0", ClampMax = "1")` | editor limits |

- **A `UObject` pointer member must be a `UPROPERTY`.** Without one, the
  garbage collector does not see the reference: the object can be collected
  while you hold it, and the pointer is not cleared. In headers use
  `TObjectPtr<UStaticMeshComponent> Mesh;`.
- Containers of `UObject` pointers (`TArray<TObjectPtr<UFoo>>`, `TMap`,
  `TSet`) need the `UPROPERTY` too.
- Only reflected types can be `UPROPERTY` members: `int32`, `float`,
  `double`, `bool`, `uint8`, `FString`, `FName`, `FText`, `FVector`,
  `FRotator`, `FTransform`, a `USTRUCT`, a `UENUM`, a pointer to a
  `UObject` type (`TObjectPtr<>`, `TWeakObjectPtr<>`), `TSubclassOf<>`,
  `TSoftObjectPtr<>`, `TArray`/`TMap`/`TSet` of these. Not a plain C++
  struct or class, and not a raw pointer to one. Blueprint handles `int32`,
  `int64`, `uint8`, `float` and `double` among the numbers.
- Removing or renaming a saved property loses the values stored for it in
  existing assets and levels. Renaming without loss needs a core redirect
  (`[CoreRedirects]` in `DefaultEngine.ini`); the syntax is in Epic's
  documentation, not checked here.

## Functions

| Specifier | Effect |
|---|---|
| `BlueprintCallable` | a Blueprint node with execution pins |
| `BlueprintPure` | a node without execution pins; mark the function `const` |
| `BlueprintImplementableEvent` | declared in C++, implemented only in Blueprint; no C++ body |
| `BlueprintNativeEvent` | C++ default in `<Name>_Implementation()`, overridable in Blueprint; call it as `<Name>()` |
| `CallInEditor` | a button in the Details panel |
| `Exec` | a console command, only on a few classes (player controller, cheat manager, game mode, …) |
| `Server`, `Client`, `NetMulticast` (+ `Reliable` / `Unreliable`) | RPCs; their bodies go in `<Name>_Implementation()` |

- A `UFUNCTION`'s parameters and return value must be reflected types.
- A delegate Blueprint can bind needs `DECLARE_DYNAMIC_MULTICAST_DELEGATE_…`
  and a `UPROPERTY(BlueprintAssignable)` member.

## Components

Created in the constructor with `CreateDefaultSubobject<T>(TEXT("Name"))`,
held in a `UPROPERTY(VisibleAnywhere)` `TObjectPtr`, attached with
`SetupAttachment`. A component added at runtime is `NewObject` plus
`RegisterComponent`. Renaming a default subobject breaks the Blueprints and
levels that store values for it. The toolkit's `domains/code.md` has the
patterns.

## Logging

```cpp
// in one .cpp, or DECLARE_LOG_CATEGORY_EXTERN in a header + DEFINE_LOG_CATEGORY in a .cpp
DEFINE_LOG_CATEGORY_STATIC(LogMyGame, Log, All);

UE_LOG(LogMyGame, Log, TEXT("Picked up %s for %f"), *GetName(), Value);
UE_LOG(LogMyGame, Warning, TEXT("…"));
```

The line reaches the editor's log as `LogMyGame: …` (`LogMyGame: Warning: …`
for a warning), so `get_output_log` `{"filter": "LogMyGame"}` or
`{"categories": ["LogMyGame"]}` finds it. `%s` takes a `TCHAR*`: pass
`*SomeFString`.
