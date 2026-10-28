# Offline backup adapter

Base: main8fd812c, completed Native diagnostic Docker #234. Implement the next
offline backup-provider boundary before monitoring; no operational activation.

1. RED: a valid provider-authenticated exact receipt must verify; a rejecting
   stub fails this behavioral requirement before implementation.
2. Verify Ed25519 domain-separated typed statements using an independently pinned
   key, chain/genesis and DB identity; bind exact E3 permit/size/digest.
3. Stream private bounded artifact verification, with nofollow descriptor paths,
   strict JSON, cancellation, no input changes or runtime dependencies.
4. Add redacted PASS/HOLD/FAIL CLI and optional private atomic exclusive receipt
   export compatible with existing matched-backup inputs. Fail closed on unsafe
   inputs/claims/output; grant no restore/GC/startup/live freshness authority.
5. Update current guides, backlog and n/n−1/n−2. Freeze source; run functional,
   fmt/build/vet/lint, whole root/wire/executor race, cache/examples/offline checks.
   No new concurrent runtime state; no new concurrency200 campaign is needed.
6. Explicit stage and staged/history secrets; Conventional commit/PR, actual
   final-head CI5, guarded squash and clean primary main sync, preserve branch.

Trust custody, provider snapshot/WAL generation, content verification/actual
restore and deployment remain HOLD. No DB/SQL/runtime/RPC configuration changes,
operator services, Docker image/container runs, format or performance work.
Pool4/part2/API1/writer7, RPC5/final NEW fresh/two EVM/upstream and E1–E6 fences
remain intact. Preserve other projects/processes/containers/shared caches.

[Guide](../../../EXECUTION_BACKUP_ADAPTER.md), [checklist](checklist.md),
[validation](validation.md), [continuity](continuity.json).
Accepted E1 [design](../20261023-e1-execution/02-design.md) and
[plan](../20261023-e1-execution/03-plan.md) remain authoritative.
