# set_asset_property in full

Writes one property on an asset: a mesh, a material instance, a data asset,
a sound. The main skill has the rule (send the JSON type the property has,
read the value back, save); this is the whole contract.

| Parameter | Meaning |
|---|---|
| `asset_path` | required, checked like every `asset` path (section `failures`) |
| `property` | required. A dot path of letters, digits, `_` and `.`, at most 512 characters, no `..`, not starting or ending with a dot |
| `value` | required. A missing one gives `Missing required parameter: value` |

If the property is not found on the asset and the asset is a Blueprint, the
tool tries the Blueprint's class defaults (its CDO) instead. Skill
`ue-blueprints` covers Blueprint defaults in full and is the better route for
them.

After the write, the tool calls the object's property-changed handler
(`PostEditChange`) and marks the package dirty. **It does not save.** The
result holds `asset_path`, `property` and `modified: true`; `modified` is
always `true` on success and says nothing about whether the value is the one
you meant.

## The path walk

- A part names a property. If more parts follow, it must be an object
  reference, and the walk moves into the referenced object, or it must be an
  array followed by a number.
- A number indexes the array before it. For an array of objects, the walk
  moves into that element.
- **Struct members cannot be reached**:
  `Cannot navigate into struct property: <part>. Set the entire struct instead.`
  Any other type in the middle of a path gives
  `Cannot navigate into property type: <part>`.
- **An array of structs cannot be entered.** After `Materials.0`, the walk
  stays on the asset, so `Materials.0.MaterialInterface` looks for
  `MaterialInterface` on the mesh and fails with `Property not found`. The
  tool's own description uses that path as an example, and it does not work.
- **Never end a path with an array index, as in `Tags.0`.** At the last part
  the code takes the array's element type but keeps the asset as the
  container, so it writes at the start of the asset object itself, not into
  the array (`MCPTool_Asset.cpp`, `NavigateToProperty`). It does not fail
  cleanly: it can corrupt the asset in memory or crash the editor. There is no
  safe route to array contents in this tool.

## Values

| Property type | Pass |
|---|---|
| object reference | an object path string. `"None"` (any case) or `""` clears it. The loaded object must be of the property's class: `Object type mismatch. Expected <class>, got <class>` |
| class reference (`TSubclassOf`) | a class path. For a Blueprint class that is `/Game/BP/BP_Door.BP_Door_C`; the asset path loads the Blueprint, not its class, and fails with `Object type mismatch. Expected Class, got Blueprint`. `"None"` or `""` clears it. **The allowed base class is not checked**: any class is stored, even one the property's `TSubclassOf<>` does not allow |
| soft object or soft class reference | a path that loads. A path that does not load gives `Unsupported property type for: <path>`, not a load error. A soft class takes the `_C` path too |
| number | a JSON number |
| `TEnumAsByte` enum | its numeric value, as a JSON number. A name string is stored as 0, with no error (section `json-types`) |
| `bool` | a JSON boolean |
| `FString`, `FName` | a string |
| `FVector`, `FRotator`, `FLinearColor` | an object: `{x, y, z}`, `{pitch, yaw, roll}`, `{r, g, b, a}`. **Pass every key**: a key left out is not defaulted, the member keeps an uninitialised value, not even `a` defaults to 1 |
| `TMap` | a JSON object. It **replaces the whole map**. A non-object gives `Map property '<path>' requires a JSON object (key-value pairs)`. Keys are read as strings and converted to the key type; enum values may be given by name here |

Cannot be set at all, each failing with `Unsupported property type for: <path>`:

- `TArray` and `TSet`;
- any struct other than the three above, and the UE text form `"(X=1,…)"`;
- `enum class` properties (`TEnumAsByte` works, see above);
- `FText`;
- a value the tool cannot read as the property's type.

The reads accept more than the matching JSON type, though: a wrong string in
an integer or `bool` property is a wrong write, not an error. Section
`json-types` lists what each type accepts.

## Errors

`Failed to load asset: <path>`, `Property not found: <part> on <class>`,
`Cannot index without preceding array property`,
`Property is not an array, cannot use index`,
`Array index 3 out of bounds (size: 2)`, `Nested object is null: <part>`,
`Object property value must be a string path`, `Failed to load object: <path>`,
and the property path checks: `Property path cannot be empty`,
`Property path exceeds maximum length of 512 characters`,
`Property path contains invalid character: '<c>'. …`,
`Property path cannot contain consecutive dots`,
`Property path cannot start or end with a dot`.

## Read it back

`get_asset_info` with `include_properties: true` lists every editable
property with `name`, `type` and `editable`, not the values. To see a value,
the routes are the editor (`open_editor`), an export to `.t3d` (section
`import`, "export"), or Python (skill `ue-python`). For a Blueprint, skill
`ue-blueprints` reads the defaults.

Placed actors are not assets: `set_property` (skill `ue-level-actors`) edits
them and accepts more value forms.
