# retarget: engine versions

Section of skill `ue-retarget`. The skill body names the four differences
that show no error.

## Engine versions

The plugin's code at upstream commit 9f6a0f7 (43 tools, plugin version
1.1.0) has branches for UE 5.6 and for UE 5.7 or later, and none for older
engines:

| Where | UE 5.7 or later | UE 5.6 |
|---|---|---|
| `create_retargeter`, `setup_ops` | add default ops, remove duplicates, assign the source and target IK Rig to every op, auto-map | the same without assigning the rigs to the ops |
| `inspect_retargeter` `chain_mapping` | read from the FK Chains op's settings | read from the retargeter asset |
| `configure_fk` `rotation_mode` | also `CopyLocal` | `CopyLocal` is not recognized and is ignored without an error |
| `batch_retarget` | asks the engine to overwrite existing output assets | passes no overwrite flag; the engine's default applies |

**UE 5.8.** Upstream 9f6a0f7 declares engine version 5.6.0 in its `.uplugin`
and has no branch of its own for UE 5.8. The UE 5.8 port (fork `Getty/ue-llm-toolkit`, branch
`ue-5.8`, commit 9fc7d4f) gives `batch_retarget` a third branch: on UE 5.8 it
calls the engine's newer batch entry point, `RunBatchRetarget`, with the
animations, both meshes, the retargeter, the prefix, referenced assets left
out, and overwrite on. UE 5.7 and 5.6 keep the calls of the table. It sets no
target folder, so on UE 5.8 the output lands in `/Game` (UE 5.8.4 source, not
run; section `retargeter`, `batch_retarget`). The other rows of the table
are unchanged in that port: UE 5.8 takes the "5.7 or later" column.
