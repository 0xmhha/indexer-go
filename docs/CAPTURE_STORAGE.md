# 원천 데이터 저장과 삭제 승인

`pkg/capture`는 체인 adapter가 검증한 입력을 Pebble/PostgreSQL에 저장하는 공통 계층이다. 필수 dataset 바이트·manifest·canonical pointer·연속 완료 위치·내부 `facts_ready` 이벤트를 하나의 durable transaction으로 반영한다. `Commit` 성공 뒤에만 원천 저장 완료를 알릴 수 있다. 기존 writer의 처리 완료 ACK·undo·사용자 outbox와는 별도다.

현재는 라이브러리 구현이다. [Arc 모듈](../x/arc/feed/README.md)이 ETH/68 header/body/receipt adapter와 두 worker를 `RunConfiguredSession`에 연결한다. `RunFeedSession`/`RunConfiguredFeed`와 Arc `RunManaged`는 명시적 RPC-finalized 정책·bounded reorg 재개·raw 소비자 ACK·선택적 GC를 연결한다. 실제 Arc peer/정책 운영 검증·독립 CL certificate·상태/trace/blob·기존 앱 활성화는 남아 있다. DB-only full-ingest projection은 [facts Source와 기존 writer](SOURCE_FACTS.md)에 연결했다. 삭제 잠금 복구 및 두 worker의 시작·취소·join lifecycle은 `RunSession`에 구현했다. 공유 volume 설정에서 instance를 선택하고 압력을 감시한다. [전체 설계](analysis/windowed-feed-design.md)와 [남은 작업](analysis/open-items.md)을 함께 따른다.

## 저장 계약

```go
// db는 *storage.PebbleStorage 또는 *postgres.Store다.
// fresh는 기존 공유 RPC pool을 사용하는 live CanonicalReader다.
facts, err := capture.New(db, chain, fresh)
if err != nil { return err }
if err := facts.Initialize(ctx, plan, bootstrapAnchor, rawOnlyConsumers); err != nil {
    return err
}
// input은 chain adapter가 검증한 정확한 encoded dataset과 evidence다.
if err := facts.Commit(ctx, input); err != nil { return err }
// 여기부터 원천 저장 완료다. projection 완료와 구분한다.
```

`Initialize`는 명시적 bootstrap checkpoint에서 시작한다. 그 이전 구간이나 checkpoint 자체를 수집했다고 표시하지 않는다. 같은 plan digest·checkpoint·raw-only consumer 집합으로만 재호출할 수 있다. chain ID와 genesis hash로 네트워크를 구분하고 시작 시 genesis/checkpoint의 fresh hash를 확인한다.

`Commit`은 현재 generation과 plan·coverage·상태·출처를 검사한다. 필수 dataset의 실제 바이트와 SHA-256 digest가 일치해야 하며, 다음 연속 높이와 parent hash가 맞아야 한다. 해당 블록의 fresh canonical 조회는 generation transaction 안에서 수행한다. 조회 실패나 불일치는 저장하지 않는다. 동일 입력의 재전달도 fresh 조회와 저장 바이트 대조를 거치며 이벤트를 중복 생성하거나 완료 위치를 뒤로 이동시키지 않는다. adapter가 수행한 commitment/proof/execution 검증의 진위까지 이 계층에서 다시 실행하는 것은 아니다.

`verified_empty`는 digest를 가진 명시적 payload가 필요하며 0바이트 값도 저장한다. `not_applicable`은 adapter의 fork rules 근거와 사유가 필요하고 payload를 받지 않는다. 누락·실패·pruned·unsupported를 빈 결과로 바꾸지 않는다. 호출 중 입력 slice/바이트를 다른 goroutine에서 변경하면 안 된다.

`Read(identity)`는 immutable fork 입력도 읽으며 dataset digest를 재검사한다. 조회 자체가 현재 canonical임을 뜻하지 않는다. `Events(after, limit)`는 최대256개의 별도 내부 이벤트를 반환한다. 소비 위치 저장·projection transaction 연결·이벤트 보관 정책은 후속 단계다.

inline dataset은 최대8MiB, 블록당 합계64MiB다. 더 큰 값은 별도 volume/path/quota의 `window.Objects`에 스트리밍 저장하고 `NewWithObjects`로 참조를 연결한다. 실제 파일 digest 검증·DB binding·행/manifest/cursor/내부 이벤트를 함께 확정하고, orphan은 영구 retirement 표식 뒤 정리한다. Arc 검증기는 header/body/transaction/receipt/log를 개별 행으로도 저장한다. [영구 객체·정규화 API와 장애 계약](PERMANENT_SOURCE_STORAGE.md)을 따른다. PostgreSQL 전용 관계형 테이블/전역 검색이나 account/storage/trace/blob 공급까지 완료된 것은 아니다.

## 두 DB 구현과 SQL 방어

`port.CaptureDatabase`의 `CaptureUpdate`는 체인별 독립 namespace에서 직렬화된 원자적 갱신을 제공한다. `CaptureView`는 일관된 읽기 snapshot이다. callback은 동기적으로 한 goroutine에서 사용하며 다른 transaction을 중첩하거나 writer를 callback 밖으로 보관하지 않는다. writer 오류를 무시하거나 context가 취소돼도 갱신을 커밋하지 않는다.

PostgreSQL은 선택적 `CaptureAppender`·`CaptureReceipter`·`CaptureProjectionViewer`로 같은 세대의 append/검증/ACK가 불필요하게 직렬화되지 않도록 한다. 모두 같은 Store/pool과 `capture_locks` 행을 사용하며 migration이나 추가 연결 pool은 없다.

| 경로 | chain row lock | 허용 범위 |
|---|---|---|
| `CaptureUpdate` | FOR UPDATE | Reset·finality·삭제·worker 등 일반 가변 갱신 |
| `CaptureAppend` | FOR NO KEY UPDATE | 다음 연속 높이의 facts/event/object 추가와 Through·sequence/다음 canonical pointer 전진. 다른 append와 직렬화 |
| `CaptureReceipt` | FOR KEY SHARE | durable projection commit 뒤 immutable projection/* receipt 추가/동일 재시도 |
| bound `CaptureProjectionView` | FOR KEY SHARE | 한 번 읽은 state의 Through 이하에 이미 공개된 입력과 generation 확인 |
| bound 일반 `CaptureView` | FOR UPDATE | append/ACK까지 배제하는 일반 callback의 전체 namespace 일관성 |

unbound 조회는 계속 RepeatableRead/read-only snapshot이다. `State`는 state 한 건만, `CanonicalAt`은 그 state의 공개 prefix만, `ValidateProjection`은 같은 state/pointer/manifest만 읽으므로 선택 prefix view를 사용할 수 있다. Through는 callback 사이에 전진할 수 있지만 기존 입력/세대는 writer commit/rollback까지 바뀌지 않는다. 큰 원천 입력·worker·일반 callback은 기존 full view를 사용한다. 한 연결 pool에서는 기존 bound transaction을 재사용한다. CaptureUpdate의 Reset/삭제/정책 변경은 prefix 검증·append·ACK 모두 끝날 때까지 기다린다.

append callback은 code-owned Commit의 generation/plan/next-height/parent/fresh 검사를 유지하며 공개 prefix를 변경하지 않는다. Reset 뒤 Through 위에 남은 이전 fork의 canonical pointer는 다음 높이 게시 때 교체할 수 있다. facts/event/object binding은 insert-only이고 같은 bytes의 재시도만 허용한다. ACK callback은 projection/* 이외의 쓰기를 거부한다. 동시 ACK가 모두 absent를 관측해도 서로 다른 bytes를 덮어쓰지 않으며 오류를 무시한 callback도 rollback한다. capture와 writer fresh는 각각 원래 transaction 내부에서 실제 RPC로 수행한다. 동시에 진행 가능한 두 조회가 기존 HTTP coalescer에서 묶일 수 있으나 캐시나 검사의 생략은 아니다.

이 선택 포트가 없는 driver/wrapper는 `CaptureUpdate`/`CaptureView`로 동작한다. fast 경로가 오류를 반환한 뒤 fallback하지 않는다. Pebble의 기존 WAL/직렬화, raw ACK/GC 권한, 영구 객체 retirement, 기능 확장/Source 계약은 유지한다. [연결4 잠금 경합 비교](SOURCE_FACTS.md)를 따른다.


| 항목 | Pebble | PostgreSQL |
| --- | --- | --- |
| 저장 위치 | `/source/capture/<chain>/<genesis>/` | migration0013의 `capture_locks`, `capture_records` |
| 원자성·동시성 | 기존 write mutex와 별도 indexed batch | 체인 row lock과 독립 SQL transaction, 여러 pool/process 간 직렬화 |
| 내구성 | WAL 필수, `pebble.Sync` | 서버 fsync 필수, transaction의 synchronous_commit을 on으로 고정 |
| projection rollback | source namespace는 undo batch에 넣지 않음 | source table은 undo 대상에서 제외 |
| 일반 재색인 | preserved prefix | preserved table, `all=true` 전체 삭제는 별도 |

PostgreSQL의 chain/key/value는 모두 `$1` 등으로 바인딩한다. peer 입력·dataset·codec·출처 문자열을 SQL 문법이나 table 이름으로 사용하지 않는다. schema validation/`pgx.Identifier`와 기존 prepared statement 연결 정책은 유지한다. embedded migration은 인자가 없는 고정 SQL이다. key256바이트·value16MiB·transaction 쓰기128MiB 한도를 둔다.

PostgreSQL reader의 선택 `port.CaptureBatchReader.GetMany`는 같은 view/transaction에서 최대32개 key를 바인딩된 `bytea[]`로 읽는다. node 조회를 위한 더 작은 한도(value1MiB·반환 key/value 합계4MiB)를 적용하며 없는 key는 생략하고 빈 값은 보존한다. 오류/초과는 callback을 실패시키며 partial success나 fallback으로 감추지 않는다. 읽기 전용·pool1·bound block transaction에서도 기존 snapshot과 해당 full/prefix generation fence를 사용한다. [source trie 조회](SOURCE_QUERIES.md#postgresql-trie-읽기-최적화)는 이 포트를 선택적으로 사용하고 hash 검증 뒤 snapshot 내부에만 node를 보유한다. Get-only backend도 계속 지원한다. trie cache는 metadata/epoch/fresh를 보유하지 않고 입력/worker/GC의 쓰기 계약은 바꾸지 않는다. 같은 snapshot의 state/pointer/manifest3개를 GetMany로 읽는 선택 경로와 큰 payload/영구 객체의 단일 view 읽기는 [canonical 원천 입력](SOURCE_QUERIES.md#canonical-원천-입력의-단일-스냅샷-읽기)을 따른다. metadata도 다른 view로 재사용하지 않는다.


## generation과 삭제 승인

`Reset(expectedGeneration, commonAncestor)`는 연속 완료 위치를 공통 조상으로 돌리고 generation을 증가시키며 별도 `capture_reset` 이벤트를 저장한다. 이전 fork의 facts와 이벤트는 보존한다. 이전 generation worker의 새 commit은 거부한다. finalized 아래로 돌아가는 reset도 거부한다. 새 cursor 위에 남아 있는 과거 canonical pointer는 완료나 삭제 근거로 쓰지 않는다.

`Finalize`는 **체인별 정책이 이미 인증한 finalized checkpoint**를 받는 내부 조립 API다. fresh hash 일치만으로 consensus finality를 증명하지 않는다. `ControlStep`은 명시적 체인 정책으로 finalized prefix를 확인하고 정책 ID/authority를 DB에 고정한 뒤 이 API를 호출한다. Arc의 현재 모드는 동일 live RPC의 finalized 태그를 신뢰하며 독립 CL certificate 검증과 구분한다. `RunManaged`의 명시적 GC 선택과 실환경 검증 없이 기존 앱 삭제를 활성화하지 않는다. [런타임 계약](FEED_RUNTIME.md)을 따른다. `AckRaw`는 등록된 raw-only consumer가 자기 작업을 durable하게 끝낸 뒤 호출한다. 동일 common ancestor의 이전 generation facts/ACK는 포크 뒤에도 유효하지만 교체된 fork의 ACK는 거부한다.

`window.Collect(ctx, facts)`는 다음 조건이 모두 충족된 segment만 삭제한다.

1. 윈도우 자체가 최소24시간 보관·닫힌 segment·읽기 lease 없음·volume identity를 확인한다.
2. DB guard가 모든 참조의 canonical/finalized block, 필수 plan 완료, **필수 dataset 전체의 실제 저장 바이트/digest**, 원본과 동일한 codec·digest·길이, raw-only consumer ACK를 검사한다.
3. 정확한 raw store·segment·참조 집합과 generation을 담은 삭제 intent 및 token을 DB에 먼저 durable commit한다. 이 fence가 있는 동안 새 capture/ACK/finality/reset을 차단한다. DB 연결이 끊겨 row lock이 풀려도 fence는 남는다.
4. guard가 동기 callback을 호출하고 윈도우가 lease/보관시간을 다시 확인한 뒤 `.gc` intent를 fsync하고 파일을 삭제한다. 삭제한 전체 참조 집합의 해시를 담은 `GC.completed.json`을 fsync한 뒤 `.gc`를 회수한다. callback이 성공한 뒤에만 DB fence를 해제한다.

삭제 callback 오류·panic·프로세스 종료·불확실한 DB commit은 fence를 그대로 남긴다. `State.PendingDelete`와 `PendingDeletion()`으로 token·generation·정확한 참조를 확인할 수 있다. 읽기는 계속 가능하지만 변경/추가 삭제는 거부한다. 재시작은 `facts.OpenWindow(ctx, opts)`가 process writer 잠금을 확보한 뒤 `.gc`의 남은 파일을 지우기 **전에** DB 승인과 필수 데이터를 확인한다. Open 후에는 `RecoverDeletion(ctx, raw)`가 GC mutex를 확보한 뒤 DB token·generation·필수 바이트·ACK를 재검증한다. 원본이 남았으면 정확한 segment 전체 참조·보관 시간·lease를 확인해 삭제를 끝내고, 이미 삭제됐으면 정확한 완료 기록과 파일 부재를 함께 확인한다. 그다음 동일 DB token만 해제한다. 파일 부재만으로 해제하거나 임의의 `ClearPending`/시간 초과 해제를 제공하지 않는다.

완료 기록은 store/segment와 참조 집합 전체의 해시를 묶으며 한 파일만 유지한다. DB는 한 번에 하나의 삭제만 승인하고 동일 raw store의 Collect/Recover/Close는 GC mutex로 직렬화한다. 다른 coordinator가 실행 중 GC의 오래된 token을 읽어도, 잠금을 기다린 뒤 DB token을 다시 대조하므로 새 generation이나 새 잠금을 해제할 수 없다. DB 완료 쓰기가 실패하면 파일이 사라졌어도 잠금을 유지하고 다음 시작에서 완료 기록으로 재개한다. 손상·다른 store·누락된 intent·lease·취소는 잠금을 유지한다. 예전 참조 해시 없는 `.gc`는 기존 Open 복구를 지원하지만, 정확한 완료 증거가 없는 과거 DB 잠금은 자동으로 해제하지 않는다. 단독 `window.Open`의 과거 승인 marker 복구와 DB 조정 모드인 `facts.OpenWindow`를 구분하며, 세션은 항상 후자를 사용한다.

현재 guard는 필수 plan에 포함되지 않은 추가 raw dataset이나 교체된 fork 원본도 자동 삭제하지 않는다. 이들의 보관/완료 정책은 별도 구현 대상이다. DB-only projection은 원천 DB 입력이 충분하면 늦게 처리해도 되지만, 기존 처리기가 그렇게 전환됐다는 뜻은 아니다.

## 수집 세션 시작·재시작·종료

```go
err := capture.RunSession(ctx, db, fresh, capture.SessionOptions{
    Window: windowOptions, // 기존 외장 volume/path/byte 한도 검증을 사용
    Plan: plan,
    Anchor: bootstrapAnchor,
    RawConsumers: rawOnlyConsumers,
    // Create: true는 최초 빈 저장소를 명시적으로 초기화할 때만 사용
}, receiveTask, captureTask)
```

volume 초기화는 별도의 명시적 작업이다. 재시작의 기본값은 DB 검증을 포함한 `facts.OpenWindow`이며 저장소가 없거나 mount/identity가 다르면 새 경로로 대체하지 않는다. DB 삭제 잠금이 남은 상태에서는 Create로 다른 빈 저장소를 만드는 것도 거부한다. DB 설정/plan/checkpoint 확인과 삭제 잠금 복구를 마쳐야 worker를 시작한다. `fresh`는 기존 공유 RPC pool의 live reader를 넘긴다. 세션이 별도의 RPC client나 limiter를 생성하지 않으며 기존 공개 RPC5와 projection의 fresh 검사도 유지한다.

세션은 두 Task를 각각 goroutine으로 실행한다. 한 Task가 끝나면 상대를 취소하고 **두 Task가 모두 반환한 뒤** 원본 저장소를 닫는다. Task는 자기 stream·lease·하위 goroutine을 취소/join해야 하며 raw store나 빌려 쓰는 DB를 닫지 않는다. 별도 worker 오류는 보존하고 DB 소유권은 호출자에게 남긴다. worker 종료가 raw backlog의 처리 완료를 뜻하지는 않는다. 남은 작업은 durable raw/facts 위치에서 재개한다. `PipelineTasks`는 adapter와 assembler를 연결하며 Arc의3-dataset 검증기는 `evmdata.Verifier`다. 수신은 `AppendWait` 후 bounded 참조 channel에 알리고, 검증기는 raw Page를 읽어 필수 입력을 모은다. channel이 가득 차면 알림을 합치며 작업은 디스크에 남긴다. 검증·Commit 성공 뒤에만 `OnCaptured`와 adapter의 선택적 `ObserveCaptured`를 호출한다. 한 블록의 참조만 모으며 gap/손상/canonical 불일치는 완료 높이를 전진시키지 않는다. 처리 지연 중 수신 지속과 부분 저장 재시작을 실제 RLPx oracle로 검증했다. 이 완료는 기존 feature/projection ACK가 아니다.

`RunConfiguredSession(ctx, db, fresh, SessionConfig{Storage: cfg.WindowStorage, Instance: "arc-testnet", Plan: plan, Anchor: bootstrapAnchor, Logger: log}, receiveTask, captureTask)`로 검증된 layout의 instance를 선택한다. chain/genesis는 검증된 Anchor에서 가져오며 디렉터리 이름에서 추론하지 않는다. 최초 실행은 기존 mount의 명시적 `InitializeVolume`과 별도의 `Create: true`가 필요하다. 실제 값/경로의 예시는 [window 설정](WINDOW_STORAGE.md#공유-volume-예약과-압력-보고)을 따른다.

managed volume은 시작 시와 기본10초 주기로 Usage를 확인한다. 공간이 없으면 수신 Task 시작을 기다리되 처리 Task는 즉시 시작한다. 수신 Task는 입력마다 `AppendWait`를 사용하면 추가 용량 부족도 같은 입력을 유지하며 대기할 수 있다. 세션이 임의로 feed 메시지를 버리거나 ACK하지 않는다. 처리 Task는 저장 데이터를 처리하고 명시적 finality/guard에 따라 GC를 진행할 수 있다.80/95% 로그는 severity가 바뀔 때만 남긴다. 감시 중 volume identity/장부 오류가 생기면 수신·처리를 취소하고 감시 worker까지 모두 join한 뒤 Close한다. 사용자 `OnUsage` callback은 동기 호출이므로 즉시 반환해야 한다.

YAML loader는 `window_storage`를 엄격하게 검증한다. 기존 `pkg/app.NewApp`/`cmd/indexer`는 이 값이 있으면 DB/RPC 시작 전에 `RunConfiguredSession`으로 검증된 Task를 공급해야 한다는 오류를 반환한다. 모드를 조용히 무시하고 기존 ingest로 시작하지 않는다. Arc header/body/receipt의 capability 감사와 bounded durable 참조 pipeline은 라이브러리에 연결했다. 명시적 finality/reorg/GC 정책은 `RunConfiguredFeed`가 연결하며 실제 네트워크/정책 운영 검증·독립 CL certificate·DB-only projection·앱 연결은 남는다. 세션 자체는 자동 GC나 adapter를 선택하지 않으며 Arc 모듈이 명시적으로 Task를 공급한다. `AppendWait`의 명시적 용량 재시도 외에 세션이 RPC 재시도 정책을 추가하지 않는다.

명시적 정책을 선택한 수집은 `RunFeedSession`/`RunConfiguredFeed`로 실행한다. 정책 일치는 peer 생성/기존 삭제 복구 전에 확인한다. 제어/소비자 worker도 같은 epoch에 속하며, reorg 뒤 모두 join하고 window를 닫은 다음에만 generation을 바꾼다. 완성된 단일 raw hash는 commit fresh로 확인하고, fork 후보/거절된 hint는 canonical hash Lookup으로 retained append 순서와 분리한다. 삭제 fence 동안 capture/ACK 입력을 유지하며 기다린다. 상세한 trust mode·consumer 계약·오류/재구축 범위는 [Feed 런타임](FEED_RUNTIME.md)을 따른다.

## 검증 범위

동일 입력 suite를 실제 Pebble과 시험 전용 PostgreSQL fsync-on에 적용했다. 단계별 쓰기 실패의 전체 rollback, 재시작·중복·순서/parent gap·fresh hash 불일치·pruned/빈 결과 구분·generation/포크·SQL 공격 문자열 바이트 보존·snapshot·갱신 경합·window 실제 삭제·lease 차단·필수 receipt 바이트 손상 거부를 검사한다. source facts가 projection undo/일반 재색인에 남는지, PostgreSQL의 서로 다른 pool이 직렬화되는지도 확인한다.

Pebble 자식 프로세스를 facts commit 직후와 삭제 fence commit 직후에 `os.Exit`해 재시작 후 데이터/내부 이벤트와 fence를 확인한다. callback이 오류를 반환한 뒤 다시 열어도 삭제 intent를 보존한다. 이 시험은 장치 전원 손실이나 consensus 검증·Arc live 처리량·24시간 SLO의 증거는 아니다. fsync-off 기능 시험 서버에서는 capture를 거부하고 내구성 suite는 명시적으로 skip한다.

추가 복구 시험은 실제 DB 완료 실패 뒤 receipt 재개·다른 store/필수 데이터 손상/누락된 intent·파일 부재만으로 해제 금지·GC와 다른 coordinator의 경합을 포함한다. 자식 프로세스를 `.gc` fsync 직후, 일부 unlink 직후, 완료 기록 fsync 직후에 종료하고 Open 복구를 검사한다. DB+window를 함께 쓰는 프로세스도 승인 직후와 파일 삭제 직후에 종료해 재개한다. 세션은 복구 이전 worker 실행 금지·두 worker 취소/join·lease 반환 뒤 Close·누락 경로 재생성 금지·복수 worker 오류 보존을 시험한다. 동일 블록의 여러 raw 참조는 한 DB transaction에서 필수 dataset을 한 번만 검증하며 다음 transaction에는 재검증한다.

Docker 공간 부족 뒤 미완료였던 전체 PostgreSQL package도 시험 volume 정리 후 fsync-on/race에서 capture와 함께 다시 통과했다(413/45개 test/subtest). 범위·공간 회수·시험 자원 정리는 [재검증 기록](analysis/work-log-2026-10.md#338-docker-공간-확보와-postgresql-전체-회귀-재검증-1015)을 따른다.

## DB-only projection receipt

`ValidateProjection`은 epoch와 정확한 captured identity를 검사한다. `AckProjection`은 기존 writer의 durable commit 뒤 consumer/epoch/input별 receipt를 기록하며 `ProjectionAck`로 역사 기록을 조회한다. raw ACK·삭제 승인·연속 완료 cursor로 쓰지 않는다. `source/facts`의 [fresh·내구성·재분기 계약](SOURCE_FACTS.md)을 따른다.
