# Arc continuous execution collector

Use the [Arc execution guide](../../../docs/EXECUTION_ARC.md#continuous-collector) for prerequisites, flags, restart and held operational checks. `Start` owns a single pinned read-only native adapter; `Run` adds the [shared source spool](../../../execwire/spool/README.md) and exporter supervision. Native storage, source spool and indexer PostgreSQL are separate stores with separate ownership.

The adapter uses bounded private stdio; only the indexer connection uses an authenticated private Unix socket. There are no RPC clients, native node launches or native delete/retention calls in this package. Tests use a test-owned subprocess and synthetic Arc library fixtures; they do not open an operator native directory.

Startup holds the shared [native owner lease](../../../docs/EXECUTION_ARC_RETENTION.md#managed-ownership) through joined child cleanup and rejects pending maintenance fences. Collection never initiates GC or launches the native node guard.

## Optional selected state proofs

`--state-proofs /ABSOLUTE/PRIVATE/selection.json` enables native block-end account/code/storage evidence. The file is private, bounded and strictly validated before startup. The collector requests operation5 and independently verifies exact scope/root/provenance before source admission. Its versioned normalized policy is sealed into the spool manifest; existing spools cannot switch policies, even after draining. Resume with the same selection and original anchor. Match the receiver's `required_state_proofs`. Full quota/window, reorg/restart/exact ACK and native owner lease protections remain. [Contracts and JSON format](../../../docs/EXECUTION_STATE_PROOFS.md#arc-native-acquisition-and-continuous-collection).
