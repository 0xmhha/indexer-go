# Native read-only diagnostics

`execution-diagnose` observes selected existing source/native maintenance metadata
and supported exported snapshots. It is the next offline path after
[PR232 file preflight](NATIVE_EXECUTION_PREFLIGHT.md). Preflight compares static
configuration declarations; diagnosis examines stored ACK/generation/floor and
pending records. Neither authorizes operation.

Build the product you use, with the pinned Go toolchain:

```sh
go build -o build/arc-execution-diagnose ./x/arc/cmd/execution-diagnose
go build -o build/stablenet-execution-diagnose ./x/stablenet/cmd/execution-diagnose
build/arc-execution-diagnose
# HOLD, exit 2: no native/source/indexer observations selected
```

All flags are optional, explicit `--flag PATH` pairs, once each. An omitted flag
is HOLD, not an assertion that a store or pending record is absent. An exact
`HOLD` or empty quoted path value is also unresolved/HOLD; it is never opened.
A flag without its argument is a syntax error. There is no
config/env/DSN/RPC traversal. Examples below name operator-selected **existing**
paths; they are not instructions to provision or export operating resources:

```sh
build/arc-execution-diagnose \
  --spool /selected/source \
  --native /selected/native \
  --source-artifact /selected/exports/source.json \
  --checkpoint-artifact /selected/exports/checkpoint.json \
  --retention-artifact /selected/exports/retention.json
```

The separate [offline monitor](NATIVE_OFFLINE_MONITORING.md) consumes this JSON
as submitted evidence alongside explicit sample times and declared metrics. It
never invokes this command, upgrades its HOLD to readiness, or clears fences.

## Inputs and production contracts

| Input | Observations and limits |
| --- | --- |
| `--spool` | Existing shared `spool.InspectMetadata`: sealed manifest v1/v2, original chain/anchor, committed durable ACK, active retained Base, optional generation-transition journal, GC intent and GC completion. Uses the production manifest, transition state and GC validators. No payload, directory enumeration, locks or recovery. |
| Arc `--native` | Native directory, sibling `.<basename>.execution-retention` fence, in-directory `.execution-retention-floor` and archived `.execution-retention-pending`. The sibling fence is still inspected when the current directory is missing during a swap. |
| StableNet `--native` | Executor data root, `.execution-retention` fence and `indexer-stablenet/chaindata/.execution-retention-floor`. A missing chaindata parent is uninitialized/uninspected, distinct from an absent optional marker in an existing directory. |
| `--source-artifact` | Production `execwire/checkpoint.SourceObservation`, including original identity, committed ACK, exact parent header/root availability, state availability and pending flags. This is the JSON produced by supported checkpoint inspection, not a checkpoint stream or native state database. |
| `--checkpoint-artifact` | Existing `execution-checkpoint inspect` JSON: typed `executionops.CheckpointInspection` (`Control`, `Bootstrap`, optional `Recovery`, `Scope`). The types remain the production `port.ExecutionControl` and PostgreSQL `ExecutionBootstrapInfo`. |
| `--retention-artifact` | Existing `execution-retention inspect` JSON: typed `executionops.RetentionInspection` (`Control`, `Permit`, `Pending`, optional `Receipt`, `Scope`). Validates the real retention permit/receipt contracts. |

Supported artifact JSON must contain every nonoptional field of its production
export, including false/zero fields. Omitted scalars FAIL schema validation; they
never imply an observed genesis height or absence of pending state. Explicit
false header/state availability or incomplete baseline flags remain HOLD.

The two products have isolated adapters. Arc supports 5042 and 5042002, checking
the already recorded testnet genesis for 5042002. StableNet keeps its existing
positive chain-ID/genesis contract. The root module never imports the StableNet
executor's EVM fork; its read-only adapter shares the exact maintenance formats
with `nodestore`. Product-specific layout paths stay under `x/<chain>`; the
shared reader has no chain-name switch. Native fence/floor structs are shared aliases of the prior
production schemas, preserving field names, ordering, digest envelopes and bytes.
The existing Arc/StableNet `Status` methods and format are preserved; the new
sampler supplies ancestor, ownership, strict decode and repeat-read checks.

The command never calls Arc `Native.Inspect`: that method launches a maintenance
child and creates scratch. It never calls StableNet `nodestore.Inspect`: that
method walks the native DB tree. It does not route through plan/apply/release,
resume-check or recovery helpers. No App, peer, listener, RPC, executable, DB,
LOCK, lease, migration, directory, latch, manifest, fence or checkpoint is created.
No pending state, generation, head, ACK, original or operational data is changed.
Native head/state/indexes and the operating indexer DB are explicitly uninspected.
An existing empty native directory does not prove an initialized DB.

Artifacts are **artifact observations**, not live, atomic or authenticated proof.
Do not run an operating exporter merely to satisfy this offline command. Without
an already available supported artifact the observation stays HOLD. Stored
`Scope`/`Recovery` strings do not grant authority and are never echoed. Native
header parents have height/hash/root, not source generation authority; ACK
and indexer generations are compared separately.

## Decisions and stable codes

Output is one deterministic JSON object, without timestamps, paths, opaque strings or raw
errors. It contains version1, the product profile, ordered findings with
`field/status/code`, the aggregate status, scope, at most12 typed `observations`, and these unconditional limits:

Observations contain only validated chain/genesis/original anchor, selected
ACK/Base/public/parent references, roots, generations, retained-through heights,
phases and availability/pending flags. Each carries a fixed basis label. Artifact
basis is explicitly unauthenticated/not live. Native permit observations name
the recorded permit's public boundary, never the current native head. Unknown
references are omitted; absent parent metadata never creates a head/root.
Opaque Scope/Recovery text, permit tokens and input paths are not reported.

Unconditional limits:

- `start_ack_deletion_gc_recovery_restore_activation: not_authorized`.
- `live_canonical_freshness: not_performed`.
- `full_state_reconstruction: not_performed`.
- `cross_store_consistency: separate_samples_not_atomic_not_a_lease`.

| Status / exit | Meaning |
| --- | --- |
| PASS finding | Only that performed metadata comparison is compatible, or that an optional marker was absent in an existing sampled directory. It is never readiness. |
| HOLD / 2 | At least one observation is incomplete, missing, changing, pending or unavailable and no hard failure is known. This offline command always has HOLD for actual native DB and operating indexer DB inspection; it cannot return overall PASS/0 today. |
| FAIL / 1 | Known corruption/conflict, unsupported format/profile/phase, unsafe input, invalid arguments, cancellation/deadline or output failure. FAIL dominates HOLD. |

Common stable codes include `compatible_observation`,
`artifact_observation_unauthenticated_not_live`, `not_selected`,
`actual_native_db_uninspected`, `operating_db_uninspected`,
`store_missing_or_uninitialized`, `optional_marker_absent`,
`directory_only_db_uninitialized_or_uninspected`, `retained_floor_uninspected`,
`transition_or_maintenance_pending`, `maintenance_fence_present`,
`checkpoint_pending_or_incomplete`, `generation_rebind_unperformed`,
`production_diagnosis_incomplete`, `changed_during_read`, `io_uninspected`,
`unsafe_metadata_input`, `corrupt_source_metadata`,
`conflicting_source_metadata`, `invalid_artifact_schema`, `artifact_missing`,
`invalid_original_identity`, `invalid_checkpoint_control`,
`invalid_full_baseline_metadata`, `partial_baseline_metadata`,
`bootstrap_digest_uninspected`, `conflicting_baseline_mode`, `invalid_ack`, `invalid_parent`,
`conflicting_observations`, `conflicting_phase`, `invalid_permit`,
`missing_or_partial_permit`, `invalid_receipt`,
`ack_ahead_of_public_checkpoint`, `ack_hash_conflict`,
`floor_ahead_of_ack`, `floor_outside_public_boundary`,
`floor_ahead_of_public_checkpoint`, `permit_ahead_of_public_checkpoint`,
`invalid_arguments`, `unsupported_profile`, `unsupported_chain`,
`cancelled_or_deadline`, `finding_limit_exceeded` and `observation_limit_exceeded`.
The finding's field identifies the comparison; it never contains an input path.
Output short writes/errors return 1 even if a complete report cannot be emitted.

Known chain/original anchor/root/public height/ACK/generation/retained-floor or
phase conflicts FAIL even if another store is unavailable. Active permits must
agree across independently observed pending fences, source ACK and indexer
metadata. Historical retained floors/completions are not current pending permits:
they constrain immutable identity, generation and floor ordering and matching
known public roots, without requiring today's ACK to equal an older permit.
Supported partial/prepared indexer exports HOLD without full-baseline authority;
unsupported modes, zero/malformed known digests and conflicting full/prepared
metadata FAIL. Copy/swap/cleanup/done fences all HOLD; cleanup/done with a conflicting known
floor FAIL. Pending, reorg, baseline-incomplete and older-generation observations
remain HOLD. Production `checkpoint.Diagnose` is composed as a pure observation;
its compatible result does not remove any HOLD or grant recovery.

Readers check each file and ancestor for unsafe links, enforce same-owner private
regular files with one hard link and non-writable existing metadata parents, and
check byte/file identity again. The fixed macOS `/tmp` and `/var` aliases are
normalized as in the existing strict reader. Each selected metadata file/artifact
is at most1MiB, JSON nesting at most32; each sampler permits at most32 file reads
and32 directory checks, and paths at most4096 bytes. Files are opened read-only,
nonblocking and no-follow. Canonical digest envelopes, strict production types,
duplicate/case-colliding/unknown keys, scalar nulls, truncated/trailing documents,
oversize/deep/invalid types and foreign owners are checked. Directory entries and
multi-GB DB/payload trees are never scanned. Report limits are128 findings,12 observations and64KiB.
Readers redact parser/OS errors, DSNs, credentialed RPC URLs/query tokens,
private keys and paths containing them. Equal repeat samples are not freshness,
a lease or a cross-store transaction. Changes detected while sampling HOLD;
cancellation/I/O never yield readiness. External APFS/enode/private manifest
inputs remain unavailable and HOLD, without an operator questionnaire.

## Validation and unchanged runtime boundaries

The synthetic tests exercise compatible products, all/mixed unknowns, missing
and uninitialized stores, optional markers, pending/partial/reorg/generation
transitions, independent conflicts, historical floors, corruption, unsafe links,
private ownership, bounds, deterministic redaction, exits, cancellation, output
failure, actual CLI invocation and byte/inode preservation. They need no native
binary, credentials, operating DB/node, APFS volume, enode or public RPC.
The [plan/checklist](codemine/runs/20261028-native-diagnostics/plan.md) and
[validation receipts](codemine/runs/20261028-native-diagnostics/validation.md)
record this attempt's source-bound checks and local handoff scope.

There is no new runtime concurrency or count200 campaign. Runtime DB pool4 per
chain/process, part2/API reservation1, seven logical writers, two EVM executions,
RPC5 physical attempts/s including retries/failover/fallback, final NEW uncached
canonical lookup before post-response generation fence/COMMIT, bound SQL/quoted
identifiers, E1–E4/full baseline/E3 exact permit/live lease/sourceACK/window/
intent/completion/release and E6 authenticated trust/fork boundaries are unchanged.
No pool/client or hot-path collector/writer/API refactor is introduced. Functional
library/mock/owned PostgreSQL checks are not P2P, performance or operational proof.
The separate [Docker diagnostic draft](../deployments/native/README.md) now wires these commands with read-only native-parent/source/export bindings. It grants no execution approval. Next priorities are the backup adapter, then monitoring. Start/GC/recovery/restore/activation remain held.
