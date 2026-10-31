# N6 validation

## Confirmed offline behavior

The actual production Arc App, owned loopback ETH/68 peer, real pinned native
bootstrap/import/replay/maintenance binaries and disposable fsync-on PostgreSQL
pass the integrated acceptance. No operating node or remote peer was started.

- Official5042002/genesis full H0 state is independently verified/imported by Go
  SQL bootstrap and native builder. Main/short/deep input branches are generated
  only in cfg(test); no production custom genesis.
- A real canonical HTTP response is withheld and public height remains behind
  it. Then all seven part digests, exact original header/body/receipts archive,
  GraphQL through a reserved reader, publication/outbox and source ACK agree.
- Actual EVM-entry observations show one import and one replay for normal1..3
  and stable-generation4..131. Full persistence consumes the first executed
  object. Restart/exact retransmission add neither EVM nor outbox publication.
- Short fork/return uses receiver generations2/3. At131/Keep129, raw payload
  files are actually deleted, their original-generation SQL archive remains
  readable, native/source COW completes and the same identity reopens.
- A real prune child exits42 after raw completion. SQL permit and journal remain
  pending, resume and changed-executable recovery are refused, read-only
  diagnostics stay HOLD, and exact explicit recovery resumes safely.
- Deep fork below retained floor fences API/publication, and Stop joins without
  rewriting anchor. Incomplete native intent and wrong state root refuse startup/
  import and preserve their owned evidence/raw data.

The integrated fixture contains official system/empty blocks, not funded live
transactions. Source construction, including the expected retransmission payload,
precedes the counted runtime. Failed/reorg attempts are separate from the healthy
one-plus-one execution claim. Prior N0–N5/E1 qualification retains its own scope.

## Testability correction and diagnosed attempts

A successful early E3 with24h-old eligibility still absent legitimately produced
zero raw candidates. It was not positive raw deletion proof. Attempting a shorter
retention was correctly refused by the existing24h minimum. Those failed fixture
attempts are not passing release evidence. The final check forwards the existing
Store clock through a Go-only Layout hook and advances only the owned raw clock
25h; it preserves24h/Keep129/ACK/permit/epoch/fresh gates and actual RPC/native/SQL
clocks. Pure validation does not invoke the hook; YAML/JSON cannot configure it.

[RED receipts](../../../../verification/native-n6/red.json) record compiled
checkpoint-export, actual3-block trace requirement, clock propagation and explicit
full-workflow assertions. [Checks](../../../../verification/native-n6/checks.json)
retain log hashes for the positive matrix, exact recovery, final refusal cases,
new local clock/mock single CPU1/race/count200, affected short/build/vet/lint,
Rust default-feature bounded checks/trace all-target lint, format and CI policy.
No heavyweight SDK/SQL case was repeated200 times and no performance measurement
was made. Further runs followed new requirements or diagnosed fixture failures,
not a host-load/SLO screening loop.

The tagged acceptance has no silent skip for missing inputs. Default PR CI adds
only a bounded four-block Rust fixture check when Arc Rust is applicable; heavy
131-block App/SDK/PG runs only in explicit full mode in the existing arc job.
All four independent state-proof200 batches and existing required checks remain.

## Continuity, release and limitations

[n/n−1/n−2](continuity.json) retain managed inspection #257/CI333 and runtime
alignment #256/CI332, accepted first-import/N5/E1 plans and per-feature evidence.
The [prior release](../../../../verification/native-n6/inspection-release.json)
is supporting evidence, not this feature's final CI. Actual applicable exact-head
short CI, squash and clean main synchronization are verified at release time;
the associated PR and post-merge receipt retain those executed identities.

No SQL schema/upstream pin/production config/pool/RPC or final-fresh/ACK policy
change. Native default binaries omit acceptance trace; stdout/wire is unchanged.
Operating APFS, real peer/fork/history, externally authenticated operating
checkpoint, image/private DSN/key and matched backup/WAL/restore evidence remain
HOLD. No provisioning/format, actual deployment, operational GC/restore, external
chain or new throughput/latency experiment was performed. Other projects and
shared caches remain untouched; only owned build intermediates are disposable.
