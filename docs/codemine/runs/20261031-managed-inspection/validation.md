# Managed inspection validation

## Implemented and checked

- Pure Arc product App hook: existing module registration, selected prepared
  raw/native/source/replay/socket/peer declarations and quotas. Unknown selected
  path/identity/fork/network values HOLD; known mismatch, overlap, numeric limit,
  provisioning flags or missing raw budget FAIL. Exact managed source-parent
  scratch is allowed; declared raw/source COW headroom is accounted for.
- Existing-pool PostgreSQL READ ONLY/REPEATABLE READ metadata exporter, explicit
  absence, bounded canonical artifact, pending retention/raw summary and no DML.
  A real reserved API-reader test reproduced a wait for an unavailable fifth
  connection. Both ReadSnapshot and exporter now use the existing synchronous
  connection-loan helper. Cancellation rolls back before returning the loan.
- Fixed native identity/head/bootstrap/current receipt, raw STORE/OWNER and
  sibling phase journal inspection under active owner locks, without recovery
  or input mutation. Source metadata retains admission and exact ACK digest.
- Explicit diagnostic raw/ingress artifact flags, independent R/N/P and epochs,
  strict monitor DTO compatibility, unsafe/cancel/missing/corrupt/owner/source
  conflict and report bounds. Source corruption is FAIL instead of unknown I/O.
- Diagnostic-only Docker Arc bindings add raw RO, source parent RO and explicit
  ingress export. Network-none/no-DSN/no-runtime properties stay unchanged.

## Evidence

[Behavioral RED hashes](../../../../verification/managed-inspection/red.json)
cover preflight, SQL export, reserved loan, locked metadata, CLI, wire source
fields, consumer DTO, Docker topology and source-corruption classification.
The source-corruption RED uses the actual `.exec-spool-state` file; an earlier
fixture used an unselected filename and was corrected before recording evidence.

[Functional/static hashes](../../../../verification/managed-inspection/checks.json)
cover affected short Go tests, fsync-on disposable PostgreSQL, one new
`GOMAXPROCS=1 -race -parallel=1 -count=200` reserved-reader case, root build,
affected root and wire vet/lint, source spool short suite, policy checks and
real host diagnostic binaries. Existing ReadSnapshot tests pass with the new
exporter tests. The200 invocation is for the single new connection-loan boundary;
it is not an unchanged native SDK/stress repeat or a throughput measurement.

Owned host CLI fixtures pass15 diagnostic and16 runtime tests with no skips.
They create no Docker/node/operating DB and remove their temporary binaries.
The separately owned PostgreSQL fixture runs fsync=on and cleans up only its own
container/volume. Root build and both affected linters pass (zero issues).
Final links/fmt/leak and source identities are retained beside these receipts.

## Release and limits

The [runtime alignment release](../../../../verification/managed-inspection/runtime-release.json)
is n−1 (#256/CI332); n−2 is coordinator #255/CI331. Accepted N5, first-import
and E1 plans remain. Actual applicable exact-head short CI, PR squash and clean
main sync are checked at release time, not pre-certified here. The associated
PR and post-merge receipt retain those identities.

No Rust/SDK source, AIN1/EXR1 frame, SQL schema, production config, pool/RPC
budget or final fresh/commit/ACK policy changes. Native full SDK state/lineage,
payload/part coverage, actual quotas/live freshness and runtime resume remain
uninspected. N6 combined App/mock ETH68/actual SDK/SQL/GC acceptance is next.
APFS, enode/fork/history, authenticated checkpoint and operating activation remain
HOLD; no node, format, operational GC/restore/deployment or performance run.
