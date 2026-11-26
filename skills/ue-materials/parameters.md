# UE materials: the parameters object

`create_material_instance` and `set_material_parameters` take the same
`parameters` object. How each operation reports a failure inside it is in
section `instances`.

```json
{
  "scalars":  {"Roughness": 0.4, "Metallic": 0.0},
  "vectors":  {"BaseColor": {"r": 1.0, "g": 0.2, "b": 0.1, "a": 1.0}},
  "textures": {"Normal": "/Game/Textures/T_Rock_N"}
}
```

Only `scalars`, `vectors` and `textures` are read; any other key is ignored.
The three are applied in that order. Each is handled by the engine's
`UMaterialInstanceConstant::Set…ParameterValueEditorOnly` with the key as the
parameter name, global association (no layer parameters).

## Names

- **The key is the parameter's name in the parent**, as the parent's graph
  shows it, not a display group. Names are `FName`s and compare without regard
  to case: `roughness` reaches `Roughness`.
- **A name the parent does not define is not an error.** The tool does not
  look at the parent. The engine's setter
  (`UMaterialInstance::SetScalarParameterValueInternal` and its texture and
  vector siblings, `Engine/Source/Runtime/Engine/Private/Materials/MaterialInstance.cpp`,
  UE 5.8.4, read, not run) finds no entry, adds one under that name and
  returns. So the call succeeds, `get_material_info` lists the value, and
  nothing renders differently, because no parameter in the parent reads it.
  Whether the engine drops such an entry on a later load has not been
  verified.
- **A parent that is itself an instance** passes its parameters through: name
  the parameters of the base material at the root of the chain.
- **A name of the wrong kind**, such as a scalar value for a vector parameter,
  is stored the same way, in the scalar list, and reaches nothing.

The tool cannot list the parent's parameters. Section `limits` names the
Python calls that can; otherwise ask the user.

## scalars

JSON numbers. Each value is read with the engine's `FJsonValue::TryGetNumber`
into a `double` and stored as a `float`.

- A number written as a string, such as `"0.4"`, counts: the read takes a
  string made of an optional sign, digits and at most one dot. `true` and
  `false` count as 1 and 0. A lone sign or dot, such as `"-"` or `"."`,
  passes and reads as 0 (`TCString::IsNumeric`,
  `Engine/Source/Runtime/Core/Public/Misc/CString.h`).
- Any other string, such as `"0.4f"` or `"1e-2"`, an object, an array or
  `null` is **skipped without an error**.

## vectors

Objects with `r`, `g`, `b`, `a`, read with `FJsonObject::TryGetNumberField`.

- `a` defaults to 1. **`r`, `g` and `b` have no default**: pass all three.
  A missing one is whatever the tool's uninitialised `FLinearColor` held
  (its default constructor sets nothing: `FLinearColor() = default`,
  `Engine/Source/Runtime/Core/Public/Math/Color.h`, UE 5.8.4): an undefined
  value, not 0.
- Inside the object, the same number coercion applies as for `scalars`.
- A value that is not an object is skipped without an error.
- The value is linear colour, as a vector parameter stores it. Values above
  1 are kept, which is how emissive strength is usually driven.
- Vector parameters hold four components. A parameter used as a position or
  direction is still sent as `r`, `g`, `b`, `a`.

## textures

Texture asset paths, loaded as `UTexture`, so any texture class loads:
`Texture2D`, `TextureCube`, render targets, and others.

- A path that does not load fails with `Failed to load texture: <path>`;
  several failures are joined with `; `. Texture paths are not run through the
  path check, so engine textures under `/Engine/` load.
- A value that cannot be read as a string, such as an object, is skipped
  without an error. A number or a boolean is read as its text and then fails
  as a path, `Failed to load texture: 5` (`FJsonValueNumber::TryGetString`,
  `Engine/Source/Runtime/Json/Private/Dom/JsonValue.cpp`, UE 5.8.4).
- **The tool does not compare the texture with the parameter's type.** A
  cube map for a 2D sampler, a colour texture for a parameter whose sampler
  type is Normal, or a virtual texture for a non-virtual sampler is stored all
  the same. What the engine does then (a compile warning, a default texture, a
  wrong look) has not been verified. Match the texture to what the parameter
  samples: normal maps to normal parameters, the same texture class, the same
  virtual-texture setting.
- `null` or `"None"` cannot clear a texture: `"None"` does not load and fails.

## What get_material_info shows afterwards

`scalar_parameters`, `vector_parameters` and `texture_parameters` list the
values stored on the instance, keyed by name: the overrides this tool and the
editor set, including names the parent lacks. A parameter that inherits its
parent's value is not listed. A texture reads back as its object path, or
`None`.

**Check the read-back, not the success message**: a skipped value (wrong JSON
type) leaves no trace in the result, and a misspelt name shows up there under
the misspelling.
