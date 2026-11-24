# control_rig: batch

"The parameter names above" and "the single operations" are those of the
skill's operations table.

`operations` is a JSON array of objects. Each object names its operation in
`op` (lowercased) and uses **the batch's own keys, not the parameter names
above**:

| `op` | Keys |
|---|---|
| `add_node` | `struct`, `name`, `x`, `y` |
| `add_template` | `notation`, `name`, `x`, `y` |
| `add_var_node` | `var`, `mode`, `cpp_type`, `x`, `y` |
| `remove_node` | `name` |
| `link`, `unlink` | `source`, `target` |
| `unlink_all` | `pin`, `direction` |
| `set_default` | `pin`, `value` |
| `set_expand` | `pin`, `expanded` (default true) |
| `add_member_var` | `name`, `type`, `public` (default true), `default` |
| `remove_member_var` | `name` |
| `recompile` | — |
| `list_structs`, `list_templates` | `filter` |

- **Two defaults are the opposite of the single operations.**
  `add_var_node` adds a getter only when `mode` is `get` or `getter`; without
  `mode` it adds a **setter**. `unlink_all` breaks the input side only when
  `direction` is `in` or `input`; without `direction` it breaks the **output**
  side.
- `inspect` and `batch` are not batch operations:
  `Unknown batch operation: '<op>'`.
- `list_structs` and `list_templates` run, but the batch counts them as
  errors, so a batch that contains one is always reported as failed. Call them
  on their own.
- A key the tool's top-level parameter names would suggest is not read.
  - For a key the entry needs (`struct_path` for `struct`, `output_pin` and
    `input_pin` for `source` and `target`, `pin_path` for `pin`), the
    entry runs with an empty value and fails like any other. `var_name` for
    `var` fails the same way when the entry has no `cpp_type`
    (`Cannot determine type for ''. …`); with `cpp_type`, the empty name goes
    to the engine, and whether that fails is not visible in the tool's code.
  - **For an optional key, the entry succeeds with the default**, and nothing
    tells you: `node_name` for `name` (the engine picks the node's name),
    `default_value` for `default` (the variable has no default), `is_public`
    for `public` (the variable is public), `is_getter` for `mode` (a setter),
    `as_input` for `direction` (the output side).

The result has `results[]` (one object per entry, in order: `success` with
`message` and, for added nodes, `node`; or `error`), `ok_count`, `error_count`
and `total`. The message is `Batch: <ok> OK, <n> errors, <total> total`.

**A batch is not a transaction.** The header calls it "a single undo bracket",
and the code does open one bracket around all entries. But a failing entry
neither stops the batch nor undoes the entries before it: every entry runs,
and the ones that worked stay. An entry that is not an object, or has no `op`,
counts as an error (`[<i>] not a JSON object`, `[<i>] missing 'op' field`).

**When any entry failed, the whole call is an error**, and through the bridge
an error is only its message: `results` does not reach you, so you cannot see
which entry failed. `inspect` the graph, or repeat the doubtful entries as
single operations, which return their own error.

Without `operations`: `Missing required parameter: operations (JSON array)`.

