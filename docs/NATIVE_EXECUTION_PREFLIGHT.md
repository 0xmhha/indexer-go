# Native execution file preflight

`execution-preflight` compares explicit declared files without starting an App,
node, native executable, DB, RPC client, peer, listener or Docker. It creates no
storage directories, LOCK, checkpoints, migrations, permits, fences or recovery
state. Runtime startup, GC and restore remain **not authorized**, even on PASS.

## Supported products and inputs

Build one product binary; the products never link each other's chain modules:

```sh
go build -o build/arc-execution-preflight ./x/arc/cmd/execution-preflight
go build -o build/stablenet-execution-preflight ./x/stablenet/cmd/execution-preflight
```

| Input | Arc native | StableNet native |
| --- | --- | --- |
| `--app` | Production App YAML (`internal/config.Config`) with exclusive `source.execution` | Same production App schema |
| `--native` | JSON of production `collector.Config`/`collector.Options` | Actual executor JSON (`execwire/nativecfg.Config`, aliased by `nodebackend.Config`) |
| `--retention` | Required production `nativeops.Config` JSON | Refused: retention uses the executor's same config |
| `--checkpoint` | Separate bounded JSON `execwire/checkpoint.Manifest` metadata | Same metadata, with StableNet header codec when `Header` is present |
| `--declaration` | Supplemental `nativepreflight.Declaration` JSON, profile `arc-native` | Same schema, profile `stablenet-native` |

The metadata file is **not** a checkpoint stream, catalog, trusted permit or host
manifest. No paths referenced inside these files are followed. In particular,
`genesis_file`, checkpoint state, native storage, source spool, PGDATA and backup
artifacts are not opened. Supplied header bytes are decoded with the product's
production header codec and compared to height/hash/root; full state roots are
not rebuilt. A `Coverage: full` declaration is not independent full reconstruction.

Arc supports the collector's chain IDs 5042 and 5042002. The recorded known Arc
testnet genesis/network identity is checked for 5042002, using the existing
[identity evidence](analysis/arc-testnet-identity.md). 5042's network ID must be
5042; its genesis is cross-checked across inputs without claiming an independently
pinned mainnet genesis. StableNet's positive decimal chain ID/genesis/network are
compared across its declared files. Its genesis file contents are not verified.

Optional StableNet E6 configuration uses the existing production
`stablenet-wbft-4d17eea018c8` trust-policy decoder/verifier, including its bounded
`anzeon-v1` fork interval, validators and anchor header. The policy's original
anchor must agree with the native/checkpoint/App values. Unknown protocols,
profiles, fork metadata, malformed/duplicate policy keys and mismatched anchors
FAIL. External authentication of that declared trust policy remains unperformed.
Requiring blob sidecars adds HOLD for the uninspected external blob supply.
Arc has no supported independent E6 verifier here; adding an E6 policy to Arc
FAILs. Omitting optional E6 never becomes a claim of independent finality.
Legacy `source.feed.module: arc.eth68`, another source, mixed product schemas,
multichain and generic EVM configs cannot be substituted for native execution.

The App loader, Arc retention loader, new collector file loader and StableNet
executor loader use bounded strict decoding against production types. App checks include the engine’s pure `app.ValidateConfig`
rules. Selected proof policies compare their production normalized digests; custom
JSON selection decoding also rejects unknown/duplicate keys. Duplicate
keys (JSON also case collisions), unknown keys, scalar nulls, malformed types,
trailing documents and deep nesting FAIL. Optional pointer/list nulls retain
production optional semantics. YAML aliases and merge keys are refused. Opaque
feature settings retain their production extension contract; this command does
not certify a feature plugin's own settings.

Arc collector JSON is now a real input to `execution-collector --config FILE`.
It is exclusive of other flags, avoiding file/CLI precedence ambiguity. Its
capitalized keys follow Go's production fields; duration fields are integer
nanoseconds (the ordinary flags still accept duration strings). `Anchor.Height`
must be explicitly present, including zero; the file loader never turns an
omission into a genesis start. The preflight
uses the same pure explicit-config validator. Retention uses its pure validator;
it does not call the loader's native-path/maintenance inspection.

Preflight accepts only the five explicit flags above, once each, as `--flag FILE`.
It reads no implicit config, `.env`, ignored real config, environment overrides,
private secrets or host manifests. It does not prove the flags, environment,
process count, filesystem permissions or executable revision of an eventual
runtime process. Normal App runtime env/flag precedence remains unchanged.

## Reports and unresolved values

| File decision | Exit | Meaning |
| --- | --- | --- |
| PASS | 0 | All performed checks of the declared files agree |
| FAIL | 1 | Known conflict, malformed/unsupported/unsafe input, cancellation, CLI/output error or report limit |
| HOLD | 2 | No hard failure, but deliberate placeholders or absent operator inputs remain |

FAIL dominates HOLD. Default output is one deterministic JSON object, no banner,
timestamps, input values or raw parser errors. Fields are `version`, `status`,
`scope`, `operational_readiness`, `live_freshness`, `full_state_reconstruction`,
`genesis_contents`, `host_and_peer_verification` and ordered `findings` with
`field`, `status`, `code`. Operational readiness always says `not_authorized`;
live freshness and genesis contents say `not_checked`. Host/peer verification and
full reconstruction say `not_performed`. A broken output writer returns 1.

Only empty/missing operator strings or exact `HOLD` are unresolved. Selected
JSON identity fields (chain ID/genesis, original anchor height/hash/generation
and root; StableNet network ID) also support absent values or exact `HOLD`.
App anchor hash/root/socket support empty or `HOLD`. The selected App numeric
fields `source.execution.anchor_height` and `indexer.start_height` support absent
values, an empty quoted string, or exact `HOLD` (quoted or unquoted). An explicit
numeric zero remains known. Their original YAML structure is checked before a
detached validation copy is normalized; nulls, aliases, duplicate/unknown keys
and trailing documents still FAIL. Neutral values used solely
inside detached validation copies are tracked as unknown, never written or
reported as matching verified identities. Two identical placeholders therefore
remain HOLD. Known zero/invalid hashes, arbitrary `???`, invalid types, unknown
keys and malformed syntax FAIL. Numeric runtime limits must be valid numbers. Supplemental declaration network,
start/floor and capacity fields support absent values or exact `HOLD`; known
zero/invalid budgets FAIL. Known lower bounds are still checked when another
capacity component is unresolved. StableNet spool quota0 resolves to the actual
production default4GiB/262144 records, preserving its capacity requirement. A missing explicit input *file* is a CLI/I/O
failure; a missing authoritative checkpoint *path or authority declaration* is
HOLD. Production numeric defaults and requirements remain unchanged; literal
`HOLD` continues to fail the typed runtime App decoder. Preflight's neutral
copies never update an input file or install an operational anchor/start height.

The declaration supplies original `chain`, `network_id`, `anchor`, `root`,
`start_height` and `retained_floor`; `start_mode: initial` requires start=original anchor+1 and floor=original anchor.
`start_mode: resume` allows a declared retained floor at/after the original anchor
and a start strictly after that floor, cross-checked against App start. A known
App start still constrains initial mode when the declaration start is HOLD, and
the known start/floor relation remains enforced if heights are HOLD. These are
configuration declarations; current stored head/floor/ACK/generation are
not inspected or authorized. StableNet `start_mode: genesis` also fixes the
original anchor height to zero, even when `anchor_height` is unresolved; a known
nonzero anchor in another file therefore FAILs. Generation must be 1; it is
never moved to a tip or inferred from a Hello. Existing retained-store restart
and recovery gates remain separate operational checks.

`apfs_identity` is a UUID-shaped declared volume identity, not a mount probe.
`checkpoint_authority` is a bounded label/reference (letters/digits and
`._:/-`), not authentication. `enode` uses the production enode parser; StableNet
also checks declaration membership in its explicit peer list. Empty/HOLD APFS,
enode, authority and checkpoint path can HOLD while known checks continue.

Native/source/scratch/checkpoint/PGDATA and declared executable/socket/backup
paths must be clean absolute paths and mutually noncontaining. Arc collector and
retention may share the same native executable; that executable must still lie
outside every declared storage path. StableNet has no
collector scratch setting. No path existence or device identity is inferred.
Declared capacity checks use overflow-safe arithmetic:

- `backup_bytes >= native_bytes + spool quota + indexer_bytes`.
- `free_bytes >= 2*native_bytes + reserve + spool quota + indexer_bytes + checkpoint_bytes + scratch_bytes + backup_bytes`.
- Native/indexer/checkpoint/scratch budgets must be positive. Reserve0 means the
  production default1GiB. These conservative declarations are not reservations,
  measured usage, compression predictions or proof that the backup is recoverable.

The App must pass its production validator, including PostgreSQL pool4 per
chain/process, RPC5 physical attempts/s, full single-chain execution, prepared
state and head publication. Supplemental declarations pin part2, API reservation1,
seven logical writers and
`new_uncached_canonical_post_response_generation_fence_commit`. Retry/failover/
batch-fallback budgeting, two EVM executions, bound SQL/quoted identifiers,
E1–E4, full baseline, E3 exact permit/live lease/source ACK/window/intent/
completion/release and E6 trust scope remain runtime contracts. The command does
not install settings, instantiate a new pool/client, clear fences or grant GC,
bootstrap, latest/zero fallback or restore authority.

Each explicit file is at most1MiB, nesting at most32, report at most128 findings
and64KiB. Input must be a regular file; file and ancestor symlinks are rejected.
On macOS only the fixed system `/tmp` and `/var` aliases are normalized to their
`/private` locations. Opened-file identity is checked before reading and again
afterward; nonblocking/no-follow open prevents FIFO/link replacement from
blocking the checker. Errors name stable fields/reason codes, never DSNs,
credentialed URLs/query tokens, peer addresses or private key values.

## Synthetic PASS and expected HOLD

All examples below are synthetic declarations, never operator-ready manifests.
They neither require APFS/enode access nor touch the declared `/synthetic` paths.

```sh
build/arc-execution-preflight \
  --app configs/execution/preflight/synthetic-arc/app.yaml \
  --native configs/execution/preflight/synthetic-arc/native.json \
  --retention configs/execution/preflight/synthetic-arc/retention.json \
  --checkpoint configs/execution/preflight/synthetic-arc/checkpoint.json \
  --declaration configs/execution/preflight/synthetic-arc/declaration.json
# status PASS, exit0; operational_readiness remains not_authorized
```

Replace only the last file with
`configs/execution/preflight/synthetic-arc/declaration.hold.json` for HOLD/exit2:
APFS, enode and checkpoint authority/path are deliberately unresolved.
The StableNet tests construct a complete synthetic declaration from the existing
E6 native fixture and check both policy-present and policy-absent behavior; that
fixture anchor is never supplied as an operating-network authority.

[Plan/checklist](codemine/runs/20261028-native-preflight/plan.md) and
[validation](codemine/runs/20261028-native-preflight/validation.md) record tests and
handoff scope. The next offline [Native read-only diagnostics](NATIVE_EXECUTION_DIAGNOSTICS.md)
observes fixed maintenance metadata and exported snapshots. Later priorities are native Docker
wiring, backup adapter and monitoring; provisioning, actual operation and extra
performance measurements remain on hold.
