# Arc continuous execution collector

Use the [Arc execution guide](../../../docs/EXECUTION_ARC.md#continuous-collector) for prerequisites, flags, restart and held operational checks. `Start` owns a single pinned read-only native adapter; `Run` adds the [shared source spool](../../../execwire/spool/README.md) and exporter supervision. Native storage, source spool and indexer PostgreSQL are separate stores with separate ownership.

The adapter uses bounded private stdio; only the indexer connection uses an authenticated private Unix socket. There are no RPC clients, native node launches or native delete/retention calls in this package. Tests use a test-owned subprocess and synthetic Arc library fixtures; they do not open an operator native directory.

Startup holds the shared [native owner lease](../../../docs/EXECUTION_ARC_RETENTION.md#managed-ownership) through joined child cleanup and rejects pending maintenance fences. Collection never initiates GC or launches the native node guard.
