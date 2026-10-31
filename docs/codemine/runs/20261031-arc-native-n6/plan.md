# N6 owned integrated acceptance

Current n: N6; n−1: managed inspection #257/CI333, n−2: runtime alignment
#256/CI332. Preserve [first-import](../20261029-arc-first-import/plan.md),
[N5](../20261030-arc-native-n5/plan.md), [E1](../20261023-e1-execution/02-design.md)
and the coordinator/recovery contracts. This qualifies the existing runtime;
external operational inputs remain HOLD.

1. Add cfg(test)-only official Arc5042002/genesis library fixture export with
   full H0 checkpoint, expected manifest, legitimate headers/bodies/receipts and
   a distinguishable alternate branch. No production custom genesis/test flag,
   full node or SDK modification. Fixture construction runs outside runtime VM
   counts. Check the two normal import/replay execution paths against real SDK
   outcomes and existing writer execution counter; Full persistence takes the
   executed object and cannot invoke another executor.
2. Add explicit build-tagged Go acceptance using production Arc App, loopback
   ETH/68 peer, actual importer/replay children and fsync-on disposable PG.
   Bootstrap SQL through independent full checkpoint verification and native
   through the production builder. Pool4/part2/API1/writers7, immutable anchor,
   bounded raw/source and shared physical HTTP5 remain. Observe real final
   canonical requests, seven part digests/publication/outbox/API and exact ACK.
3. Extend the same fixture with restart/lost reply, wrong root or incomplete
   state refusal, reorg boundaries, cancellation/join, raw preservation and
   explicit drain/E3/raw/native/source/SQL GC/recovery checks. Add only necessary
   runtime fixes after behavioral RED. Keep R/N/P, raw/native/read epochs and
   receiver generation separate. Never manufacture success from unavailable
   inputs or move original bootstrap/anchor.
4. Forward the existing raw Store clock through an optional Go-only Layout.Now
   embedding hook for controlled age qualification. Keep the24h minimum and
   every permit/ACK/epoch guard. No YAML/JSON/environment/CLI override and no
   clock calls during pure validation. Qualify the new hook RED→GREEN.
5. Keep heavy combined SDK/PG acceptance in the explicit full workflow and
   local qualification; default PR remains scoped short. Preserve tested native
   binaries before owned target cleanup and clean only owned fixture resources.
   Use one selected CPU1/race/200 only for new concurrent logic; heavyweight
   actual SDK/PG acceptance runs once per necessary fix, not200 times.

For each unit: compiled behavioral RED→GREEN, affected functional/static checks,
latest docs/checklist. Actual applicable exact-head short CI, guarded squash and
clean main synchronization complete release. Store current and previous two
contexts. No real peer/public RPC, operator config, deployment, format, restore
or new performance measurement; no other project/shared-cache cleanup.
