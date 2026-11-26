# C++ in an unreagent project: reading compile errors

What a `compile` reply holds and how it is cut is skill `unreagent-build`.
This section is what the errors in it usually mean.

The error codes are Microsoft's compiler (`C<nnnn>`) and linker
(`LNK<nnnn>`) codes, whose meanings Microsoft documents. The causes given are
the usual ones in Unreal projects; the exact wording of UnrealBuildTool's and
UnrealHeaderTool's own messages varies by engine version and was not
captured for this skill: match on the code or the gist, not on the text.

## Order of reading

1. The `exit` line. `0` is success whatever the text says.
2. The first `error` in the reply. A missing include or a header-tool error
   causes dozens of follow-on errors; fix the first, compile again.
3. When the first lines are header-tool errors, nothing was compiled yet: no
   compiler error in that reply is real until they are fixed.
4. The end of the reply: whether the build failed in compiling, linking or
   before either.

## Header tool (UHT): before anything compiles

| What you see | Usual cause | Fix |
|---|---|---|
| an error naming a `.generated.h` include, such as that it must be the last include | an `#include` below `#include "X.generated.h"` | move the generated include to the end |
| `Cannot open include file: 'X.generated.h'` (compiler `C1083`) | the header has no reflected type, the file name and the include differ, or UHT failed first | match the names; read the earlier errors |
| an error about a missing or unknown type in a `UPROPERTY` or `UFUNCTION` | a type UHT cannot reflect (a plain struct, an unknown class without a forward declaration it can see) | make it a `USTRUCT`, forward-declare the class, or drop the macro |
| an error about the class name prefix | `A`/`U` does not match the base class | rename (section `reflection`) |
| an error about `BlueprintReadWrite` on a private member | private and Blueprint-exposed | `meta = (AllowPrivateAccess = "true")`, or make it protected/public |
| an error about a missing `GENERATED_BODY` | the macro is missing or not first in the body | add it as the first line of the type's body |

## Compiler

| Code | Meaning | Usual cause in UE | Fix |
|---|---|---|---|
| `C1083` | cannot open include file | wrong include path; the header lies in a module not in `Build.cs` | the path relative to the module's `Public/` (or `Classes/`), e.g. `GameFramework/Character.h`; add the module |
| `C2065` | undeclared identifier | a missing include; a typo | include the header that declares it |
| `C2039` | is not a member of | wrong class; the member moved or was renamed in this engine version | check the engine header |
| `C2027` / `C2079` | use of undefined type | only a forward declaration is visible where the full type is needed | include its header in the `.cpp` |
| `C2511` / `C2556` | overloaded member function not found / differs only in return type | the `.cpp` signature does not match the header | make them identical, `const` included |
| `C2664` | cannot convert argument | wrong argument type; `FString` where `TCHAR*` is wanted (`*Str`), `TObjectPtr` vs raw pointer | convert explicitly |
| `C3668` | method with override specifier did not override | the base class signature differs (often `const`, or a renamed engine virtual) | copy the base signature |
| `C4430` | missing type specifier | an unknown type name used in a declaration, often a missing include or forward declaration | include or forward-declare |

## Linker

| Code | Meaning | Usual cause in UE | Fix |
|---|---|---|---|
| `LNK2019` / `LNK2001` | unresolved external symbol | the module that defines it is not in `Build.cs`; a function declared and never defined; a `BlueprintNativeEvent` or RPC without its `_Implementation` | add the module; write the body |
| `LNK1120` | n unresolved externals | the summary of the above | fix the `LNK2019` lines |
| `LNK2005` | already defined | a function defined in a header without `inline`; a `.cpp` included somewhere | move it to a `.cpp` or mark it `inline` |
| `LNK1104` | cannot open file `…UnrealEditor-<Module>.dll` (or `.lib`/`.pdb`) | the file is in use: an editor (or a debugger) still has the module loaded | `ue_stop` (check `status` shows it down), then compile again. An editor the launcher did not start must be closed by the user |

## Build system

| What you see | Usual cause | Fix |
|---|---|---|
| `exit 6` and `Unable to build while Live Coding is active` | a Live Coding session in a running editor (UE 5.8.4 source) | `ue_stop`, compile again |
| `exit 10` at once, a conflicting instance is running | another UnrealBuildTool of this engine, and a command without `-waitmutex` | wait, run again (skill `unreagent-build`) |
| an error that the target `<Name>Editor` was not found, or no `.Target.cs` | no editor target: a Blueprint-only project, or a renamed project | section `project-layout` |
| an error naming a module the `.uproject` or `Build.cs` lists but no directory provides | a typo in the module name, a missing `Build.cs`, a class name in `Build.cs` that differs from the file name | names must match: directory, `<Name>.Build.cs`, the class inside, the `.uproject` entry |
| a `Build.cs` compile error (C# `CS<nnnn>`) | a syntax error in a `.Build.cs` or `.Target.cs` | fix the C#; these compile before anything else |

## When the reply does not show the cause

The reply holds the first 4 KiB and the end of a failing build. An error in
the omitted middle is gone for this run. Fix what is visible and compile
again: the next run's errors move up. If that does not converge, the editor's
own log does not help (the build ran outside the editor); the user can add a
command that writes the build output to a file (skill `unreagent-build`,
section `custom-commands`), which `read_file` then reads in pieces.
