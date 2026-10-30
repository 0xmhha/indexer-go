# N4 local qualification and release boundary

N4 implementation and required local gates are complete. Exact-head CI5, guarded
squash and clean main synchronization remain required before N5 implementation.
N3 is released by [PR244](https://github.com/0xmhha/indexer-go/pull/244),
[CI316](https://github.com/0xmhha/indexer-go/actions/runs/40933966055), with
[release proof](../../../../verification/native-import-n3/release.json).

## Implemented and qualified behavior

[Behavioral RED evidence](../../../../verification/native-import-n4/red.json)
precedes managed OFD/read-view lifetime, complete decode/cancel/join, AXM1 and
current/original-input proof, private canonical artifacts, child environment,
source policy, epoch/SDK unwind/lineage, early E1 fence, same-session proposal,
App recovery order, raw schema2 and owned round/recovery/Stop lifetime. Compiler
and setup failures are excluded from behavioral RED evidence.

Additional RED/GREEN covers the current SQL published prefix below native N,
wrong current hash/root and inspection-time visibility changes; local exclusive
recovery blocks inspection, and live SQL fences are checked before/after it.
Normal publication pending is evaluated only after SH, while leftover pending
still refuses reads. Missing canonical raw with a matching tip stays HOLD.
Startup identity errors retain their cause, phase and bounded child diagnostic.

Real pinned SDK/process qualification includes six epoch/transition cases,
eight actual SIGKILL boundaries and two inherited RW/RO processes. The same RO
factory refreshes after parent unlock and performs the second replay. Cancel
joins the child before unlock. Selected Go/App boundary race tests also pass.
These individual proofs do not substitute for N6 combined acceptance.

## Completed local gates

[Source](../../../../verification/native-import-n4/source.json) and
[checks/log digests](../../../../verification/native-import-n4/checks.json) record:

- Build, vet, lint0 and fmt; separate wire/StableNet/AST modules, examples, cache
  lifecycle and offline Docker draft tests.
- One complete GOMAXPROCS=1/race/count200 root invocation across all four new
  concurrency packages, including managed startup and recovery inspection. No
  partial counts are combined. The separate wire module's new proposal case
  passes its own complete single200.
- Whole root/wire/StableNet `make test-race`, with the documented20-minute package
  timeout, package concurrency2 and top-level parallel2; all packages pass.
- Changed Rust fmt, all-target build, clippy `-D warnings`, and all-target tests:
  **85 library + 6 command tests; zero failed/ignored**.
- Owned PostgreSQL fsync-on adapter, App, StableNet and API functionality. The
  initial passing package results and isolated same-code API retry are composed
  explicitly rather than misreporting the first aggregate exit1 as success.
- Relative links/anchors, exact pins, final source maps and unchanged private
  configuration SHA256/mode. Staged/history scans include reviewed public
  historical fixture false positives; no unreviewed secret findings.

The [concurrency source boundary](../../../../verification/native-import-n4/concurrency-source.json)
proves the only edit during the last200 was an equivalent tagged switch in a
separate pure test excluded by its selector. Inverting that spelling reproduces
its exact original file hash. Selected cases/helpers and all production source
were unchanged; final whole race and CI compile/check the final tree.

## Failed-first evidence and limits

The first whole race hit App's default10-minute package timer during a crash-
recovery keyspace comparison and large sparse fixture; an existing blob HTTP
fixture also hit its request deadline. No data race was reported. Concurrent
owned durable fixtures may have contributed; host/performance cause is unproved.
The final full run passes after serializing heavy suites and using the documented
20-minute package budget and reduced test parallelism. Product limits/deadlines
are unchanged.

An initial root200 exposed19 active-import pause test failures: its signal lived
only inside NextBatch, but canceled capture may correctly exit admission before
its first read. The fixture now observes stream Close and cancels/completes/joins
on every exit; its targeted single200 and the final full200 pass. Another aggregate
recorded one positive Go-child identity5s timeout. Its delay source is unconfirmed.
The positive process fixture now uses30 seconds rather than a startup-latency SLO;
product default1m and deterministic cancellation/join barriers remain unchanged.
Failed and deliberately terminated incomplete runs remain recorded and never
count as GREEN. A scanner false positive on a public log SHA256 field was resolved
by an unambiguous evidence field name, without disabling a rule.

Keep pool4/part2/API1/writers7, shared5 physical HTTP attempts/s, two normal EVM
passes, and NEW final fresh → generation fence → COMMIT → exact ACK. No node,
operator peer/DB, disk formatting, operational GC/restore or performance campaign
was run. APFS/enode/authenticated operating inputs remain HOLD; N5/N6 remain.
