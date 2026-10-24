# U08: fenced rewind and restart

상태: [PR #213](https://github.com/0xmhha/indexer-go/pull/213), merge `08098f2`에서 필수 CI 4개·squash/main 동기화를 완료했다. 영속 marker·원자적 rewind·단계별 장애/재시작·128/129 경계·retarget·읽기 차단을 검증했다. 실제 노드·peer·성능 측정은 보류다. 현재 작업은 [체크리스트](checklist.md)를 따른다.

## 저장과 복구

BeginExecutionRewind는 기존 pool4에서 shared reader/part/publication이 끝난 뒤 schema 독점 advisory lock을 얻는다. 원래 공개 tip·target의 높이/hash/root·anchor·연속 parent·undo 존재·최대128블록을 검사하고, 새 generation과 exec_rewinds marker를 durable commit한다. marker 또는 rewinding flag 중 하나만 있어도 새 공개/part/API는 차단한다. 기존 모드 RollbackTo로 이 경계를 우회할 수 없다.

ResumeExecutionRewind는 독점 fence 안의 단일 transaction으로 다음을 수행한다.

1. 기존 feature rollback hook·orphan archive·undo·reorg outbox.
2. target 이후 account/slot/clear/contract 이력을 제거하고, 영향받은 key의 남은 이력에서 최신 값을 복원한다. tombstone·새 계정·storage clear·creation/activation metadata를 포함한다. 초기 상태를 임의0이나 Before 값으로 새로 만들지 않는다.
3. 모든 미공개 core와 part/publication proof의 target 이후 범위를 제거한다. content-addressed code blob은 보존하며 node/raw spool GC를 수행하지 않는다.
4. 7개 cursor 모두를 같은 generation/target hash/parent와 digest0인 rewind base로 초기화하고, 공개 높이·root·latest/count metadata를 맞춘다.
5. 모든 application write를 보낸 뒤 uncached target fresh와 응답 뒤 generation/hash/root fence를 확인하고 실제 COMMIT한다.

중간 오류/취소/종료는 위 transaction 전체를 rollback한다. 먼저 commit한 marker는 남으므로 일부 표만 복구된 상태로 재개하지 않는다. 재시작은 같은 marker를 읽고 전체 원자적 복구를 다시 수행한다. 상태 coverage·필요한 code·undo·부모가 없으면 marker를 보존하고 멈춘다. 128블록 이내여도 undo가 이미 없으면 성공시키지 않는다.

복구 중 canonical 가지가 다시 바뀌면 현재 generation을 지정하여 더 낮은 target으로만 retarget할 수 있다. 새 generation을 또 부여하고 원래 공개 tip은 보존한다. 동일 요청 generation/target/root 재시도는 멱등이며 이전 generation의 다른 요청은 거절한다. 전체 깊이가128을 넘는 retarget도 거절한다.

## queue와 feature writer

Controller는 DB의 공개 높이를 queue 시작점으로 사용한다. 재시작 marker가 있으면 worker를 만들기 전에 복구한다. rewind는 durable fence 뒤 이전 scheduler를 취소·join하고 기존 Future들을 실패시킨 뒤 복구한다. 성공한 새 generation에서만 새 scheduler를 만든다. queue capacity를 기다리는 Submit이 lifecycle lock을 잡고 rewind를 막지 않는다. Close는 진행 중 복구와 context callback까지 취소·join한다.

복구 요청/복구가 실패하면 로컬 admission은 차단한 채로 남는다. 검증된 target으로 재시도하거나 소유 앱을 종료해야 한다. 실패한 이전 세대의 Future를 성공 처리하거나 ACK하지 않는다.

처리 중 같은 ref/parent/digest의 재전송은 기존 Future를 공유하고 예산을 두 번 차지하지 않는다. 다른 digest는 충돌이다. 이미 공개된 결과의 재시도는 U06의 exact durable proof + 새 fresh 경로이며 parts/features를 재적용하지 않는다.

Fetcher.RecoverExecution은 기존 단일 feature writer에서 rollback hook과 outbox를 처리한다. ExecutionRecoveryTarget은 공개 tip에서 anchor/128 경계까지 새 RPC hash를 비교하여 가장 최신의 공통 부모를 찾는다. 수집이 더 앞서 있어도 공개 undo 범위를 넓히지 않는다. 상태 root는 검증되어 저장된 해당 header에서 읽고, 탐색 뒤 DB generation이 달라졌으면 거절한다. RPC는 기존 체인별5 physical HTTP/s pool을 사용한다.

publication의 fresh 대상은 feature에 전달한 가변 model이 아니라 불변 execution ref다. feature가 identity를 바꾸면 공개 전에 거절한다. 다른 높이로 fresh를 돌려 원래 블록을 ACK할 수 없다.

## 근거

실제 fsync-on PostgreSQL에서 marker/restart/fresh/미공개 purge stub RED4 → 같은 검사 GREEN. 복합 상태 복원 RED1과 nonempty trie의 missing explicit-zero baseline RED1도 수정 후 GREEN이다. feature_undo/state_restore/core_purge/cursor_reset/fresh 각 단계의 오류 후 논리 자료 전체가 동일하게 보존되고 재open 복구됨을 확인했다. 정확히128 복구·129 거절·deeper retarget·old generation 거절, reader가 끝나기 전 rewind 대기 및 이후 새 reader/part의 즉시 retry 오류도 PG/race GREEN이다.

Controller stub RED3, feature recovery/target stub RED2, pending duplicate Future RED1, mutable-model fresh 대상 RED1 → 같은 검사 race GREEN. 최종 새 동시성200회와 필수 전체 검사는 통합 PR 전에 완료한다. Go 기능 검사·고정 EVM fixture를 실제 peer 운전이나 처리량 증거로 확대하지 않는다.
