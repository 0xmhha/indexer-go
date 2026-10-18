# Feed finality·reorg·GC 런타임

`capture.RunFeedSession`과 layout용 `RunConfiguredFeed`는 기존 `RunSession`에 finality 정책·reorg supervisor·raw-only 소비자를 연결한다. Arc에서는 `arcfeed.RunManaged`로 선택한다. [앱의 source.feed](FEED_APP.md)는 등록된 모듈과 window를 선택하고 [DB facts 후처리](SOURCE_FACTS.md)·epoch 취소/join·새 Source·projection undo를 함께 실행한다. 지원 plan은 Arc 테스트넷의 header/body/receipt3개다. 실제 peer/mount 운영·450ms/24시간 검증은 남아 있다.

수신은 선택적 `chainfeed.BatchStream`이면 같은 블록의 최대3개 dataset을 함께 받는다. ETH68 adapter는 이미 검증·크기 제한한 header/body/receipt의 소유 bytes를 추가 요청/복사 없이 넘긴다. 기존 `Stream.Next` adapter도 계속 지원한다. [원본 묶음 저장](WINDOW_STORAGE.md)의 fsync가 모두 끝난 뒤에만 각 `OnReceived`/참조 알림을 순서대로 보낸다. source capture와 writer ACK는 각각 기존 검증·DB commit을 거쳐 별도로 진행한다. 다음 블록의 선행 수신 goroutine이나 cross-block payload 큐는 추가하지 않는다.

## Finality의 신뢰 근거

`FinalityPolicy.Checkpoint`는 해당 체인의 finalized canonical prefix를 인증하는 조립 경계다. fresh hash 일치나 임의 confirmation 수만 반환하는 정책을 연결하지 않는다. 정책 ID는 trust mode·규칙·authority를 포함하고 source DB에 고정한다. 이미 정책이 고정됐거나 근거 없이 내부 `Finalize`로 만든 checkpoint가 있는 DB에 다른 authority를 조용히 적용하지 않는다. `RunFeedSession`은 peer 생성과 pending GC 복구를 위한 window Open보다 먼저 이 일치를 검사한다. 기존 정책 DB에는 동일 정책을 명시해야 하며, steady tick의 일치 확인은 읽기만 수행한다.

Arc의 현재 선택지는 **명시적인 trusted RPC finalized 모드**다. 공식 Arc `6e764023`의 [결정 블록 확정](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/malachite-app/src/finalize.rs)이 Engine API를 호출하고, [RPC 구현](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/eth-engine/src/rpc/engine_rpc.rs)과 [IPC 구현](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/eth-engine/src/ipc/engine_ipc.rs)은 forkchoice의 head/safe/finalized hash를 같은 결정 블록으로 설정한다. 이 경로를 근거로 운영자가 선택한 동일 RPC source의 `finalized` 태그를 신뢰한다. indexer가 Malachite validator-set/certificate를 직접 검증했다는 뜻은 아니다.

```go
// rpcSource는 기존 체인별5 HTTP/s 공유 pool을 쓰는 live source/rpc.Source다.
// raw worker 이름은 session.RawConsumers와 정확히 일치해야 한다.
err := arcfeed.RunManaged(ctx, db, rpcSource, peer, session,
    capture.RuntimeOptions{
        Collect: true,
        ControlInterval: 10*time.Second,
        MaxReorgDepth: 128,
        Consumers: map[string]capture.RawConsumer{"audit": auditWorker},
        OnControl: reportControl, // 동기 callback, 즉시 반환
    }, "arc-testnet-authority")
```

마지막 authority는 비밀 URL/키가 아닌 안정적인 공개 label이다. 빈 authority는 reorg-only이고 GC를 허용하지 않는다. `Collect: true`에는 명시적 finality 정책이 필요하다. 이 호출은 volume 초기화나 저수준 진입점이다. 앱에서는 [source.feed](FEED_APP.md)가 module과 window를 명시적으로 선택한다. session의 원래 bootstrap checkpoint는 재시작에도 그대로 두고 현재 진행 위치는 DB에서 읽는다.

`source/rpc.FinalizedCheckpoint`는 같은 client/pool에 `eth_getBlockByNumber("finalized", false)`를 보낸다. null은 일시적 finality 부재로 보고 GC를 건너뛰며 capture는 계속한다. malformed/unsupported/통신 오류는 반환하고, latest/safe/depth로 대체하지 않는다. checkpoint와 capture candidate를 fresh로 확인하며 finalized 높이/hash 역행이나 이미 확정된 block의 canonical 불일치는 중단한다. 공개 RPC의 실제 태그 동작·trust authority·live Arc 접속은 아직 운영 검증하지 않았다.

## 재분기와 재시작

제어 worker는 기본10초마다 현재 finalized floor와 capture tip을 fresh로 대조한다. capture의 필수 commit 전 fresh 검사는 별도로 유지한다. 원래 captured suffix에서 공통 조상을 최대128높이(설정 최대4096)까지 찾되 finalized/bootstrap 아래로 내려가지 않는다. RPC head가 잠시 낮아져 tip이 `not found`인 경우도 더 낮은 일치 prefix를 확인한다. 일반 I/O/통신 오류를 fork 불일치로 바꾸지는 않는다.

reorg가 있으면 수신·검증·제어·raw 소비자와 peer를 모두 취소/join하고 window를 닫는다. 그다음 공통 조상을 다시 조회하고 DB transaction 안에서 fresh를 확인해 generation을 증가시킨다. 새 adapter를 만들어 같은 window를 다시 열고 다음 높이부터 수집한다. 기본 한 실행당 최대8회 재개하며 delay는100ms다. 깊은 reorg·finalized 침범·재개 한도는 `ErrRebuildRequired`/`ErrFinalityConflict`로 멈춘다. DB/원본을 자동 삭제하거나 최신 높이로 점프하지 않으며, 운영자가 원인을 확인한 뒤 명시적 checkpoint와 별도 재구축 저장소를 정한다.

처리기는 `raw.Candidate`로 다음 높이의 필수 dataset이 하나의 hash로 완성됐는지 확인한다. 이는 가용성 힌트이며 canonical 판정은 아니다. profile 검증/조립 뒤 기존 generation transaction 안의 fresh 검사를 반드시 거쳐야만 저장한다. 아직 없는 높이나 미완성 입력을 위해 미리 RPC를 보내지 않으므로 정상 경로의 중복 resolve 조회와 미래 높이의1초 negative 대기를 줄인다. 참조는 한 segment의 bounded index씩 읽고 전체24시간 블록 맵이나 payload cache를 만들지 않는다. bounded channel은 알림이며, 알림 유실/가득 찬 큐/재시작에도 durable metadata를 다시 확인한다.

같은 높이에 부분 fork까지 여러 hash가 남거나 hint의 commit fresh가 불일치/not-found를 반환하면, 실패 transaction은 저장하지 않고 RPC canonical hash와 `raw.Lookup` 경로로 전환한다. unresolved fork 후보는 기본1초마다 갱신하며 commit 직전 fresh는 여전히 별도다. 새 head보다 높은 retained orphan도 임의 latest/빈 데이터로 바꾸지 않는다. I/O·취소·rollback 오류가 함께 발생하면 canonical 오류로 숨기지 않는다. 기존 reorg supervisor의 join/reset·generation·삭제 fence·writer generation fence/커밋 fresh/ACK 계약은 유지한다. 별도 limiter/client를 만들지 않고 shared5 HTTP/s를 그대로 사용한다. 기존 `Run`의 Page 기반 baseline도 유지한다. [연결4 동시 부하 전후 측정](SOURCE_FACTS.md)을 참고한다.

## Raw 소비자와 삭제

`RawConsumer.Consume`는 해당 identity의 필수 raw envelope를 lease 동안 읽는다. 자기 작업을 durable하게 저장하고 하위 goroutine을 join한 뒤 성공해야 한다. worker는 lease를 모두 반환한 다음 자기 그룹의 `AckRaw`를 저장한다. 소비자 실패·취소·read budget 경합은 완료 ACK가 아니다. 재시작/commit-ACK 사이 crash로 재전달될 수 있으므로 identity에 대한 멱등성을 가져야 한다. 등록한 최대8개 그룹 모두에 실제 worker를 공급해야 하며 다른 그룹이나 기존 projection 완료 ACK를 대신 쓰지 않는다.

유효한 정책 tick만 `SealExpired`/`Collect`를 호출한다. `SealExpired`는 active segment의 최신 자료까지24시간 이상 지난 경우에만 seal하므로 매 tick마다 작은 파일을 만들지 않는다. 삭제는 기존 DB guard가 필수 encoded 바이트/digest/codec·canonical/finalized 높이·모든 raw ACK를 다시 확인한 뒤 durable fence를 잡고 수행한다. 삭제 중 capture/ACK는 짧게 기다리며 처리 입력을 유지한다. 오류나 중단으로 남은 fence는 다음 세션 시작에서 기존 정확한 intent/완료 기록으로 복구한다. TTL이나 파일 부재만으로 해제하지 않는다.

아직 저장되지 않은 자료·ACK 지연·이전 fork segment는 `ErrRetain`으로 보관하고 다른 삭제 가능한 segment를 계속 검사한다. **이전 fork 원본에는 자동 삭제 권한을 부여하지 않는다.** 해당 fork의 미처리 자료와 소비자 취소/완료를 입증하는 별도 정책이 필요하다. 이 보관량도 quota에 포함하며, 공간 부족 시 수신을 대기시킨다. `ErrRetain`에 독립 오류가 합쳐졌거나 삭제 callback을 이미 실행했다면 정상 보관으로 숨기지 않고 오류를 반환한다.

## 관측과 검증 범위

`OnControl`은 generation·finalized checkpoint·삭제 segment 수·보관/일시적 finality 부재를 반환한다. `OnReceived`/`OnCaptured`와 volume 압력 보고를 함께 사용한다. callback 안에서 durable 작업을 지연시키지 않는다.

독립 loopback RLPx의 fork 교체→join/reset→새 fork 저장, 이전 facts/raw 보존, 실제 제한 HTTP pool과 finalized 조회, Pebble/PostgreSQL의 ACK 지연·최소 보관·lease·정책 교체/역행·deep reorg·실패 후 무ACK를 검사한다. 기존 삭제 중단 복구·SQL 바인딩 회귀도 유지한다. 실제 Arc certificate 검증·실제 외장 장치의 전원 손실·450ms 수집·24시간 다중 체인 운영 검증은 이 결과에 포함하지 않는다.

## 영구 원천 자료

선택적 `SessionOptions.Objects`/`SessionConfig.Objects`는 열린 영구 객체 저장소를 빌려 쓴다. runtime의 모든 capture handle과 삭제 복구가 같은 store identity를 사용하고, 초기/주기 health 검사도 수행한다. 객체 저장소 Close는 세션이 모든 worker를 join한 뒤 호출자가 수행한다. Arc의 기본 plan은 그대로3종이며, 검증 후 header/body/transaction/receipt/log 행을 같은 capture transaction에 정규화한다. [저장/조회/GC 계약](PERMANENT_SOURCE_STORAGE.md)을 따른다. 실제 계정/storage/trace/blob 입력이나 새 feature writer를 자동 활성화하지 않는다.
