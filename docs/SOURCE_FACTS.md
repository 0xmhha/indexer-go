# 원천 DB를 이용한 기존 writer 후처리

`pkg/source/facts`는 capture DB의 검증된 encoded header/body/receipt를 기존 `source.Source`와 fetch writer에 연결하는 라이브러리다. Arc에는 동일한 profile과 native 회계를 고정하는 `arcfeed.NewFactsSource`가 있다. 원본 window를 제거한 뒤에도 source DB/필요한 영구 객체에서 블록·transaction·receipt·log를 재생한다. 앱의 opt-in 설정과 epoch 교체/undo/종료 조립은 [feed 실행](FEED_APP.md)에 있다. 실제 peer/외장 디스크 운영 검증은 남아 있다.

## 읽기 경계

`facts.New`에는 열린 `capture.Store`, capturing module과 같은 `chains.BinaryProfile`, 기존 shared pool의 fresh `CanonicalReader`, 안정적인 projection consumer ID를 전달한다. consumer는 공백 없는 printable ASCII 최대128자다. DB/객체/RPC를 빌려 쓰며 새 HTTP client나 goroutine을 만들지 않는다. 기존 Go/geth 버전과 Source/SDK 필수 인터페이스는 유지한다.

첫 지원 범위는 complete header/body/receipt3종 plan이다. 추가 state/trace/blob를 요구하는 plan은 생성 단계에서 거부한다. 각 dataset의 codec/commitment evidence를 확인하고 `evmdata.Decode`로 cardinality·profile hash/sender·transaction/uncle/withdrawal/receipt roots·receipt 타입·gas/bloom을 다시 검사한다. window 수집 verifier와 DB 재생이 같은 검증 함수를 사용한다. 정규화 행을 읽은 것만으로 encoded commitment 검증을 대신하지 않는다. 파생 receipt 필드는 binary profile 규칙을 따르며, 예를 들어 체인별 blob 가격 schedule이 없어 계산할 수 없는 BlobGasPrice는 기존 era 경로처럼 nil이다. RPC 전용 부가 필드를0이나 latest 조회로 채우지 않는다.

`Head`는 DB의 연속 `Through`다. 관측한 네트워크 head나 가장 큰 보관 높이가 아니다. `BlockWithReceipts`, `Block`, `Logs`는 DB 입력을 읽고 노드로 누락 자료를 보충하지 않는다. 각 실행 dataset은 최대8MiB, header는64KiB이며 고정된 decoder cardinality 한도를 유지한다. 영구 객체를 쓰는 입력도 지원하지만 이 실행 decoder 한도를 넘는 값은 거부한다. 객체 저장소 자체의 더 큰 한도가 곧 실행 plan의 지원 범위를 늘리지는 않는다.

`BlockWithReceipts`는 `ReadProjectionInput(epoch, identity)`으로 canonical metadata와 inline/영구 객체 bytes를 한 capture snapshot에서 읽는다. PostgreSQL은 state/canonical/manifest 세 작은 record만 바인딩된 선택 `GetMany`로 읽으며 큰 payload는 단건 읽기 한도를 유지한다. 영구 객체는 DB binding을 대조한 뒤 한 번의 bounded Copy로 descriptor·길이·digest·volume lease를 검사한다. 같은 입력을 객체마다 `Read`/`Verify`/`CopyPayload`로 다시 읽지 않는다. 이 소비 경로는 profile 디코딩 뒤 **독립적인 최신 epoch/canonical 검사**를 유지하고, 실제 writer의 커밋 직전 fresh RPC 및 durable commit 뒤 처리 ACK도 그대로 따른다.

실행 materialization 한도보다 큰 영구 descriptor는 `capture.ErrProjectionInputLimit`와 `port.ErrInvalidData`로 식별하며 facts는 기존 `ErrCoverage`를 함께 반환한다. 손상·누락·binding/volume 오류를 coverage로 바꾸거나 RPC/window로 재시도하지 않는다. 일반 `Read`/streaming `CopyPayload`의 더 큰 객체·historical fork 지원은 유지한다.

bootstrap anchor는 hash/높이 checkpoint이고 captured input은 아니다. anchor 이하의 header/body 조회는 `ErrCoverage`, 아직 Through 위인 높이는 `source.ErrNotFound`다. 이전 상태나 bootstrap header가 필요한 feature를 이 범위에서 실행하려면 별도의 검증된 자료가 필요하다. RPC/latest/빈 결과로 대신하지 않는다.

## Writer와 feature 연결

```go
// factsDB는 Initialize/복구를 마친 capture.Store다.
blocks, err := arcfeed.NewFactsSource(ctx, factsDB, liveCanonical, "explorer-v1")
if err != nil { return err }
writer := fetch.NewFetcher(blocks, projectionDB, fetchConfig, logger, eventBus)
writer.SetSource(blocks)

deps := blocks.BindFeatures(feature.Deps{
    Storage: projectionDB, Logger: logger,
    Publish: writer.Publish, Settings: featureSettings,
})
pipeline, err := feature.Build(enabledFeatures, deps)
if err != nil { return err }
writer.SetFeatures(pipeline)
// StartHeight는 명시적 checkpoint 뒤의 첫 높이여야 한다.
// 모든 호출을 취소/join한 뒤 writer.Close(), 그 뒤 빌린 자원을 닫는다.
```

일반 EVM에서는 `facts.New(ctx, facts.Config{Store: store, Profile: profile, Canonical: live, Consumer: consumer})`에 해당 module의 profile을 명시한다. Arc를 generic EVM 회계로 열지 않는다. 이 예는 full ingest용이다. declared range/era chaining과 운영 YAML을 자동 활성화하지 않는다. Source를 감싸는 wrapper는 선택적 `CommitValidator`와 `CommitAcknowledger`도 보존해야 한다. 기본 `source.Source` 인터페이스만 embedding한 wrapper가 이 선택 경계를 전달한다고 가정하지 않는다.

`BindFeatures`는 알려진 balance/hash/batch·contract/code/hash/batch·BlockAt 의존성을 DB facts로 바꾸며 lazy genesis resolver도 이 경계로 지정한다. 임의 custom handler의 자체 RPC closure까지 가로채는 기능은 아니다. 그런 handler도 제공된 의존성/coverage 계약을 사용해야 한다.

현재 실행 plan에는 계정 상태·code·contract call 결과가 없다. 해당 메서드는 일반 `ErrCoverage`를 반환하며 provider의 pruned-state 응답처럼 취급하지 않는다. `allow_pruned_state`가 켜져 있어도0/latest fallback을 허용하지 않는다. 상태가 필요 없는 feature는 재생할 수 있다. native balance는 projection DB에 이미 검증하여 초기화한 모든 해당 계정 기록이 있으면 delta로 계속 진행할 수 있고, 처음 만난 계정이나 resync에 필요한 상태가 없으면 블록을 중단한다. 이번 검증의 초기 잔액은 fixture의 명시적인 알려진 checkpoint를 넣은 것이며 운영용 snapshot import/증명 공급을 구현한 것은 아니다.

## Fresh 검사와 커밋 내구성

새 Source는 선택적 `source.CommitValidator`를 구현한다. writer는 일반 block·이미 저장된 block 재시도·feature backfill의 커밋 직전에 이를 호출한다. capture generation/plan/정확한 canonical input identity를 확인하고 기존 live canonical reader로 현재 hash를 다시 조회한다. state preparation을 하지 않은 block도 검사한다. 공개 RPC의5 HTTP/s 제한을 그대로 공유하며 hash를 입력/DB cache로 대체하지 않는다.

이 경로의 projection 저장소와 그 wrapper는 `port.BlockDurability`를 구현/전달해야 한다. Pebble은 WAL 비활성화를 거부하고 기존 Sync commit을 사용한다. PostgreSQL은 fsync=on을 요구하며 transaction의 synchronous_commit을 on으로 강제한다. 일반 RPC/era/replay의 기존 경로는 이 opt-in 검사를 새로 요구하지 않는다.

source와 projection이 **같은 Store handle**을 쓸 때 PostgreSQL은 bound block transaction을 재사용한다. 공개 prefix 검증은 선택 `CaptureProjectionView`의 KEY SHARE로 세대를 고정하며, 일반 full `CaptureView`는 FOR UPDATE로 namespace 전체를 고정한다. 한 연결 pool도 중첩 transaction 대기 없이 동작하며 capture generation 변경은 projection commit/rollback까지 기다린다. queued SQL은 같은 transaction 안에서 먼저 flush한다. Pebble은 원래 block write lock이 같은 DB의 capture 변경을 배제한다.

이 한 연결 지원은 커밋 검증이 같은 transaction을 재사용하는 계약이다. 독립적인 사전 facts 읽기는 pool 연결을 빌리므로 연결1개에서는 writer의 fresh 대기/commit까지 기다린다. 기존 `database.postgres.max_conns: 2`(`INDEXER_DB_POSTGRES_MAX_CONNS=2`)로 pool 예산을 지정하면 writer 연결 외에 acquisition 읽기를 진행할 여유가 생긴다. 새 pool/Store handle을 만들지 않아 같은 transaction의 source/projection generation fence를 유지한다. 명시적인1을 자동으로 늘리지 않으며 운영 DB/서버 연결 예산에 맞춰 정한다.0은 DSN/pgxpool 기본값이다.

별도 handle/DB/서버를 쓰는 경우 두 commit을 원자적으로 묶는 분산 transaction은 없다. fresh 검사와 commit 사이의 fork/epoch 변화 가능성은 남고, 후속 ACK는 새 generation을 발견하면 거부한다. durable projection의 잘못된 fork는 기존 undo/reorg 복구로 처리한다. RPC 검사가 consensus finality를 증명한다고 해석하지 않는다.

## 처리 ACK와 재분기

`CommitAcknowledger`는 기존과 같이 projection transaction 성공 뒤에만 호출된다. 쓰기 가능한 capture Store에 `ProjectionReceipt{consumer, epoch, identity}`를 durable하게 기록한다. receipt는 **정확한 입력별 처리 증거**이며 contiguous cursor나 현재 feature coverage가 아니다. 진행 높이/undo/outbox는 계속 projection writer가 소유한다. raw 소비자 ACK·capture FactsReady·원본 GC 승인과도 독립적이다. raw deletion fence 중에도 DB-only receipt를 기록할 수 있다.

projection commit 뒤 ACK를 잃어도 DB 자료는 남는다. 동일 block 재시도는 fresh 검사를 다시 수행하고 cursor/receipt를 확인하며 feature 변경/undo/outbox 이벤트를 중복 생성하지 않는다. 오래된 receipt는 fork/reset 뒤에도 역사 기록으로 남으며 현재 canonical 처리 완료를 주장하지 않는다. consumer ID는 한 projection 정의에 안정적으로 쓰고 다른 정의에는 새 이름을 사용한다.

Source는 생성 때의 capture generation을 고정한다. Reset 뒤 기존 handle의 head/read/commit/ACK는 `capture.ErrStale`로 중단한다. 조립자는 기존 작업을 취소/join하고 새 generation의 Source를 만든 뒤, 기존 `HandleReorg`/undo로 projection을 공통 조상까지 복구하고 대체 블록을 순차 처리해야 한다. `source.feed` 앱은 이 supervisor를 연결한다. 라이브러리를 직접 조립하면 같은 lifecycle을 제공해야 하며 Source 자체가 `Fetcher.Run` 안에서 자동 교체되지는 않는다. 같은 Source로 무한 재시도하거나 최신 높이로 점프하지 않는다.

## 계측과 검증

`GetPipelineMetrics`의 `source_validate`는 projection 내구성 확인·필요한 queued SQL flush·source DB fence·fresh RPC 대기를 포함한다. `source_ack`는 projection commit 뒤 source receipt 저장/확인 시간이다. 기존 `fetch_decode`, `ordered_write`, `database_commit`도 유지한다. Commit 필드는 실제 BlockTx.Commit 호출만 측정하므로 앞 단계에서 flush된 SQL을 포함하지 않는다.

테스트는 raw 제거 후 일반 EVM/Arc의 JSON-RPC 디코딩 경로와 block/receipt/log·초기화된 잔액·outbox의 의미 값을 대조한다. event의 created_at 수집 시각과 RPC가 보고한 block Size는 비교에서 제외한다. binary Size는 profile이 encoded 입력에서 계산한다. 커밋 중단·fresh 불일치·ACK 유실·재시작·실제 branch 교체/undo/outbox·영구 객체·잘못된 commitment·없는 계정/계약 상태·한 연결 SQL pool·내구성·generation 경합을 검사한다. 실제 Arc peer 접속/24시간 운영이나450ms 처리량 달성을 이 검사로 주장하지 않는다.

`ProjectionReporter`는 실제 source identity·epoch·anchor를 transaction 안의 완료 기록에 제공하며 추가 network 조회는 하지 않는다. 기존 CommitValidator/fresh 검사는 그대로다. feature/part의 bounded 완료 구간은 block undo와 함께 기록하고 API는 generation·definition·canonical hash와 gap을 확인한다. 선택 블록 요약 worker는 별도 cursor/event transaction을 사용한다. [진행/독립 worker API](PROJECTION_PROGRESS.md)를 따른다.

선택 상태 공급 library가 저장한 지정 계정·code·슬롯은 `StateSnapshot(ctx, height, selection)`으로 RPC 없이 읽는다. exact selection과 epoch를 검사하고 proof/code를 header state root에 재검증한다. 없는 selection은 ErrCoverage이며 다른 I/O·stale·증명 오류를 숨기지 않는다. 이 메서드는 BalanceAt/CodeAt/eth_call 또는 앱의 state feature를 자동 활성화하지 않는다. [상태 증명 공급](STATE_PROOFS.md)의 획득 시점·범위·예산·기본 plan/GC와의 구분을 따른다.

## 지정 상태 transition

[상태 proof stream](STATE_PROOFS.md)의 parent checkpoint 모드에서 `facts.Source.StateTransition(ctx, height, descriptor)`는 DB-only before/after를 반환한다. exact selection·범위·현재 epoch·canonical input·연속 완료와 header/MPT/code를 검증한다. 첫 before는 bootstrap 또는 captured parent의 checkpoint다. `required: true` source는 같은 조건을 raw GC에도 강제하고, 설정 제거/변경을 거부한다. 기존 block plan/Through·state-free projection 완료점은 독립적이며, native balance/eth_call/stateful feature는 자동 활성화하지 않는다.

선택 `source.lookup.v1` worker는 source DB만 읽어 블록/거래 해시별 canonical prefix 색인을 만들고, `/indexing/source/*`에서 버전이 있는 header/body/transaction/receipt/log 행을 제공한다. 별도 cursor·immutable root·fresh/동일5 HTTP/s 예산과 reorg/page 고정은 [원천 검색](SOURCE_QUERIES.md)을 따른다. 주소/topic 전역 검색과 기존 projected API의 대체는 포함하지 않는다.

이미 capture reset이 기록된 epoch>1에서 이전 projection head가 현재 capture prefix보다 높으면, 그 prefix부터 기존 `HandleReorg`로 fresh hash·bootstrap·undo 범위를 확인하고 writer/outbox transaction으로 되돌린 뒤 재개한다. 노드에서도 사라진 과거 tip을 필수로 먼저 조회하다 종료하지 않는다. 초기 epoch의 기존 DB 호환성 검사를 완화하거나 없는 블록을0/latest로 대체하지 않는다.

## 실제 facts writer 전후 측정 (2026-10-17)

`INDEXER_MEASURE_FACTS_WRITER=1 go test -count=1 -v -run '^TestMeasureFactsWriter$' ./pkg/source/facts`로 재현한다. PostgreSQL은 fsync-on `INDEXER_TEST_POSTGRES`를 지정한다. 이 opt-in fixture는 Arc profile의 같은32블록·64거래·512로그를 inline과 영구 객체3종으로 각각 저장한다. warm 뒤 DB-only 읽기64표본과 실제 ordered `FetchRange`32블록을 분리해 측정했다. 기존 `address.index`/`token.transfers`, block transaction/undo/outbox, fresh validator와 durable ACK를 실행했다. base facts에 상태 coverage가 없으므로 `balance.native`는 켜지 않는다. capture와 projection은 같은 DB이며 PostgreSQL 연결은1개다.

변경 전 소비 source는 SHA를 확인한 Go overlay로 재현했고 두 바이너리는 같은 측정/공통 코드·입력을 쓴다. macOS/Pebble 전후, 같은 Docker PostgreSQL16 서버의 별도 schema 전후 순으로 실행했다. 비교 중 다른 테스트는 실행하지 않았다. PostgreSQL DSN은 의도적으로 simple_protocol/synchronous_commit=off를 요청했지만 기존 adapter가 extended parameter binding과 capture/projection 동기 커밋을 강제했다. 운영 설정·한도는 변경하지 않았다.

DB-only BlockWithReceipts 지연(ms):

| 저장/입력 | 전 p50 | 후 p50 | 전 p95 | 후 p95 | 전 p99 | 후 p99 |
|---|---:|---:|---:|---:|---:|---:|
| Pebble inline | 0.183 | 0.212 | 0.564 | 1.892 | 5.482 | 7.171 |
| Pebble objects | 3.382 | 1.247 | 4.934 | 5.810 | 5.472 | 21.729 |
| PostgreSQL inline | 6.993 | 5.362 | 15.418 | 6.090 | 19.568 | 7.280 |
| PostgreSQL objects | 19.658 | 6.521 | 24.977 | 7.129 | 53.609 | 8.336 |

실제 writer 안에서 측정한 source 읽기 지연과 최종 durable ACK까지의 처리량:

| 저장/입력 | 전 읽기 p50 ms | 후 읽기 p50 ms | 전 블록/s | 후 블록/s |
|---|---:|---:|---:|---:|
| Pebble inline | 0.313 | 0.354 | 5.014 | 5.070 |
| Pebble objects | 6.087 | 1.683 | 5.134 | 5.125 |
| PostgreSQL inline | 192.294 | 190.009 | 5.097 | 5.111 |
| PostgreSQL objects | 204.636 | 189.795 | 5.085 | 5.108 |

PostgreSQL 영구 객체의 DB-only p50은 약66.8% 줄었다. 블록당 capture KV 호출은23 Get+1 batch=24회에서6 Get+2 batch=8회, CaptureView는7→4회다. 실제 writer 전체가 사용한 capture 읽기는37→21회/블록, view는17→14회다. 이 진단값은 CaptureView의 Get/GetMany만 세며 모든 SQL/transaction/쓰기/ACK 내부 호출의 합계가 아니다. Pebble inline은 Get-only metadata 경로에서11→12회/블록으로 늘고 p50도 소폭 느려졌다. Pebble 객체 단독 읽기의 p99도 악화됐다. 따라서 모든 경로/tail의 성능 개선으로 해석하지 않는다.

실제 PostgreSQL writer의 객체 읽기 p50은204.636→189.795ms로 약7.3% 줄었다. 이 값에는 연결1개의 pool 대기와 writer의 fresh 검사 동안 유지되는 block transaction의 영향이 포함된다. validator p50도 약162~170ms이며 최종 처리량은 두 경로 모두 약5.1블록/s로 비슷했다. 이번 입력에서는 source byte 읽기 개선이 최종 처리량 증가로 이어지지 않았다. fresh 검사를 transaction 밖으로 옮기거나 cache하지 않았다.

각 writer 실행은 fresh canonical HTTP32회·source validation32회·durable ACK32회로 오류0이었다. 물리 transport 시각으로 shared5 HTTP/s·burst1 envelope를 확인했다(스케줄러 오차20ms 허용). 초기 token1개를 포함한 짧은32블록 범위의 평균은5를 조금 넘을 수 있으며 지속 제한은5다. 읽기 자체의 RPC는0이다. 전후/DB/layout 모두 decoded 입력 digest·저장 block/receipt 및 생성 시각을 제외한608 outbox event digest가 일치하고 각32 ACK identity를 확인했다.

이는 고정된 작은 로컬 입력의 replay 비교다.64표본 nearest-rank p99는 최대 표본이며 tail/SLO 추정치가 아니다.32블록 처리량은 반복 운영 평균이 아니다. 실제 Arc P2P 수신·public RPC 지연·멀티MiB 블록·외장 디스크·450ms/24시간 추종을 검증한 수치는 아니다. 전체 증거는 `/private/tmp/indexer-facts-writer-20261017/measurements.json`과 비교/검증 로그에 있다.

## PostgreSQL 연결 경합 비교 (2026-10-17)

앞 절의 pool1은 facts 테스트 fixture에서 명시적으로 고정한 예산이었다. 운영 adapter는 이미 `database.postgres.max_conns`를 지원하며0은 DSN/pgxpool 기본값이다. 같은 Store handle의 기존 bounded pool에2개 연결을 허용해 writer와 사전 facts 읽기가 함께 진행하도록 예제를 정리했다. 테스트의 기본1은 유지하고 비교용 `INDEXER_MEASURE_FACTS_MAX_CONNS=1|2|4`를 추가했다. 커밋 직전 ValidateCommit은 writer의 bound transaction과 SHARE fence에 그대로 묶인다. source/projection 데이터의 원자성·fresh/ACK 계약을 유지한다.

PostgreSQL 선택/DSN을 설정한 구성에서는 다음 예산으로 시작할 수 있다.

```yaml
database:
  postgres:
    max_conns: 2
```

환경 변수는 `INDEXER_DB_POSTGRES_MAX_CONNS=2`다. config.example.yaml도2를 사용한다. 명시적인1은 자동 확장하지 않는다. 사전 facts 읽기는 read-only RepeatableRead이고 writer가 fresh를 기다리는 동안 여유 연결을 사용한다. 실제 수집·API·선택 worker가 함께 실행하는 작업량은 별도 측정해4 이상이 필요한지 정한다. 이번 fixture는 이미 원천 저장이 끝난32블록의 후처리 비교다.

같은 바이너리·고정32블록/64거래/512로그·영구 객체3종/inline·fsync-on PostgreSQL16 서버에서 예산별3회씩 실행했다. 순서는 `[1,4,2] → [4,2,1] → [2,1,4]`로 바꿨다. 각 실행은 별도 schema, warm 뒤64 DB-only 읽기, 실제 writer32블록, fetch workers2와 address.index/token.transfers를 썼다. 다른 검증 명령은 측정 전에 종료했다. 단독 원천 읽기·SQL 호출 수가 같은 상태에서 연결 경합의 영향을 비교한다.

다음은 **3회 실행 각각의 percentile/처리량을 구한 뒤 그 중앙값**이다. 표본을 합친 percentile이 아니다. writer 내부 읽기는 연결 대기/디코딩/후속 epoch barrier를 포함하고 처리량은 durable ACK까지다.

| 입력 | 연결 상한 | 읽기 p50 ms | 읽기 p95 ms | 읽기 p99 ms | 블록/s |
|---|---:|---:|---:|---:|---:|
| inline | 1 | 191.633 | 200.316 | 218.321 | 5.105 |
| inline | 2 | 7.330 | 11.140 | 12.321 | 5.116 |
| inline | 4 | 6.790 | 22.248 | 89.711 | 4.920 |
| objects | 1 | 192.529 | 203.953 | 211.889 | 5.104 |
| objects | 2 | 9.193 | 14.927 | 15.387 | 5.105 |
| objects | 4 | 8.244 | 17.124 | 27.834 | 5.110 |

연결1→2에서 영구 객체의 writer 읽기 p50은192.529→9.193ms(약95.2% 감소), inline은191.633→7.330ms(약96.2% 감소)였다. 영구 객체의 DB-only p50은6.862→6.450ms로 비슷하고 CaptureView/Get/GetMany 수·입력/결과도 동일했다. 큰 차이는 writer 연결을 기다리던 시간의 감소다.4개도 경합을 줄였지만2개 대비 처리량 증가는 거의 없고 inline tail/처리량 편차가 더 컸다. 따라서 현재 후처리 작업량의 시작 예산은2로 정했다.4개가 항상 더 빠르거나 더 느리다는 일반 결론은 아니다.

최종 객체 처리량은 약5.1블록/s로 비슷하며 sourceValidate p50은 약172~178ms였다. 물리 RPC5 HTTP/s·burst1 제한과 매 블록 커밋 직전 fresh를 유지한 결과다. 단독 읽기64개/실제 writer32개 표본의 p99와3회 중앙값은 장기 tail/SLO를 뜻하지 않는다. 실제 Arc peer 수신·공개 RPC 지연·연속 수집/외장 디스크/24시간 보존은 이 fixture로 검증하지 않았다.

18개 입력별 측정(9개 실행×2 layout) 모두 decoded 입력 digest·저장 block/receipt/생성 시각을 제외한608 outbox event digest와32 ACK identity가 일치했다. writer 측정 구간마다 fresh HTTP32회·source validation32회·source ACK32회·오류0, DB-only 읽기 RPC0을 확인했다. Source 생성 단계의 genesis/anchor fresh2회는 이 처리량 구간에서 제외했다. 기존 물리 transport 시각의5 HTTP/s/burst1 envelope 검사도 통과했다. 짧은 범위의 평균은 초기 token1개를 포함한다.

`TestFactsReadsDuringFresh`는 실제 writer를 커밋 직전 fresh 안에서 gate로 멈춘다.1연결의 읽기 대기/취소와2·4연결의 독립 facts 읽기, 미커밋 projection 행 비노출, reset의 SHARE fence 대기, fresh 불일치 시 block/outbox/ACK0을 확인한다.4연결 성공 경합은 단일 Go CPU/race에서200회 반복 통과했다. 별도 SQL 바인딩/내구성·기존 facts/검색/앱 회귀도 통과했다. 증거는 `/private/tmp/indexer-facts-pool-20261017/measurements.json`·`validation.json`·회귀/반복/소유권 로그다. 소유 컨테이너/명시적 볼륨/측정 바이너리는 정리했다.

## 수집·writer·HTTP API 동시 부하 비교 (2026-10-17)

앞 절의 원천 저장 완료 후 writer 비교에 이어 **실제 ETH68/RLPx peer 수신·raw window fsync·capture DB commit·기존 ordered writer·HTTP GraphQL 조회**를 동시에 실행했다. 같은 App/Store/기존 bounded PostgreSQL pool의 연결 상한2와4를 비교했다. 커밋 직전 bound transaction/SHARE fence/fresh와 기존 shared5 HTTP/s·burst1을 유지했다. 운영 config.yaml과 설정 예제는 변경하지 않았다.

재현은 `INDEXER_TEST_DRIVER=postgres`, fsync-on `INDEXER_TEST_POSTGRES`, `INDEXER_MEASURE_MIXED_POOL=1`, `INDEXER_MEASURE_MIXED_CONNS=2` 또는4를 지정하고 `go test -count=1 -v -run '^TestMeasureManagedFeedPoolLoad$' ./pkg/app`으로 실행한다.32블록이 기본이고4블록 모드는 race/lifecycle 확인용이다.

- 고정 Arc profile 입력은 warm2블록 뒤 같은32블록을500ms마다 peer/RPC에 공개한다. 전체34블록/68거래/544로그이며 블록당2거래/16로그다. 원천을 미리 DB에 채우지 않았다. peer는 로컬 프로토콜 oracle이며 실행/합의 노드는 아니다. encoded fixture timestamp와 실제 공개 주기는 별개다.
- 기존 managed App의 실제 intake/capture goroutine과 facts writer를 사용한다. workers2/chunk1, 기본 acquisition_queue off, poll50ms/control1s, 기존 원자적 undo/outbox/ACK를 유지했다. balance.native/token.metadata 및 추가 proof/state/trace/blob/lookup/search worker는 켜지 않았다.
- production `api.NewServer`의 GraphQL router를 실제 loopback HTTP 서버에 연결해 같은 Store를 조회한다. `{block(number:1){number hash transactionCount}}`와 최근8블록 목록을 각 client가 번갈아 요청한다. 별도 API pool/RPC proxy/추가 RPC를 만들지 않았다.
- API client2개가 각100ms ticker로 요청하며 최대20회/s를 목표로 한다. 각 client는 한 요청이 끝나야 다음 요청을 시작하므로 지연 시 launch 기회가 합쳐진다. 무한 큐를 만드는 open-loop20 RPS 시험이 아니다. **실제 완료율과 오류를 함께 보고한다.** HTTP timeout은2초이며 오류/timeout을 latency 표본에서 제외하지 않는다.
- 모든 블록 공개가 끝나도 capture와 writer ACK가 최종 높이에 도달할 때까지 API 부하를 유지한다. 공개16초 구간과 이후 drain의 API 표본을 분리한다. 주 표의 API 지연은 공개 구간에 시작한 요청 cohort이고 완료율은 실제 해당 구간 안에 응답이 끝난 요청 수다. 구간 끝의 in-flight 요청도 결과/지연에 포함한다.
- 진행 관찰은20ms 메모리 snapshot만 사용하며 추가 SQL/RPC를 보내지 않는다. capture 완료는 기존 bounded telemetry ledger 시각, ACK 완료는20ms nominal polling의 상한 관측이다. scheduler 지연이 더해질 수 있다. 관측한 수신/capture/ACK 높이는 원자적인 DB snapshot이 아니다. fixture가 공개한 높이는 알고 있지만 이를 운영 network latest로 표시하지 않는다.

같은 바이너리/입력, 동일 fsync-on PostgreSQL16 서버의 별도 schema로 `[2,4]→[4,2]→[2,4]` 순서로3회씩 실행했다. 측정 중 다른 테스트는 실행하지 않았다. 다음 표는 **각 실행의 percentile/rate를 먼저 구한 뒤3개 값의 중앙값**이다. 표본을 합친 percentile이 아니다.

| 연결 상한 | 최근8블록 API p50 / p95 / p99 ms | 공개 구간 API 완료/초 | 32블록 ACK 완료까지 초 | 공개 종료 뒤 drain 초 |
|---|---:|---:|---:|---:|
| 2 | 3.449 / 191.973 / 210.168 | 19.062 | 17.200 | 1.199 |
| 4 | 2.314 / 9.457 / 21.038 | 19.875 | 17.621 | 1.620 |

연결4에서 최근8블록 API p95는191.973→9.457ms로약95.1% 감소했다. 단일 블록 API p95도187.042→4.453ms였다. API 완료율은19.062→19.875회/s였다. 실제 수집/capture가 source row lock을, writer가 자신의 transaction을 유지하는 동안에도4개 pool은 API가 쓸 여유 연결을 제공한다.

수집/writer 처리량의 개선은 관측하지 못했다.32블록 ACK 완료는17.200→17.621초, 공개 속도를 포함한 전체 평균은1.860→1.816블록/s였다. 공개→capture p50/p95는715.860/1185.569→808.842/1411.778ms, 공개→ACK 관측 p50/p95는1119.622/1619.802→1219.944/1820.002ms였다. 최대 총 적체는 양쪽 모두4블록, 수신→capture3블록, capture→writer2블록(각 실행 최대값의 중앙값)이었다. API를 빠르게 제공하면서 실제 완료 요청 수도 늘어난 조건이며,3회 비교로 수집 지연 차이의 일반적인 원인을 확정하지 않는다. 이전 backlog-only5.1블록/s와는 공개 속도/동시 source commit/API 부하가 달라 직접 속도 비교하지 않는다.

따라서 **원천이 준비된 writer 위주는2개, 같은 DB에서 수집·writer·API를 함께 실행하고 API tail이 중요하면4개를 시작점**으로 권장한다.4개가 수집 자체를 더 빠르게 만들었다고 해석하지 않는다. 더 많은 API 동시성·무거운 log/search/state 조회·여러 체인·실제 RPC 지연의 적정 예산은 별도로 측정한다.

6회 모두 요청 오류0(연결2 총988건,4 총1040건의 측정 cohort), 모든 API 응답의 hash/높이/거래 수를 확인했다. API 결과는 진행 시점이 달라 전체 response bytes를 동일하다고 주장하지 않는다. 최종34블록/68 receipt와 생성 시각을 뺀646 outbox event digest가 같고, 각34 canonical identity/ACK를 확인했다. captureCommit34회·writer sourceValidate34회·sourceACK34회·오류0이었다. writer/수집이 shared5 HTTP/s를 사용하는 동안 각 timed 구간 HTTP 도착 수는 중앙값73회였으며 기존 burst1 envelope 검사를 통과했다. Source/RPC 초기 확인은 timed 구간 밖이고, 중단 뒤 같은 API 조회가 RPC0인 것도 확인했다.

race 검증은 현재 동일 경로의4블록 부하를 연결2/4에서 각각 통과했다. native feed telemetry/fetch/RPC pool/GraphQL439 tests/subtests 통과·opt-in2skip, 실제 PostgreSQL9패키지119통과·test skip0, 전체 compile/vet/최종 lint0/예제2모듈도 확인했다. 신규 helper는 성능 fixture이며 운영 API나 writer interface를 바꾸지 않았다. 실제 Arc P2P/공개 RPC·외장 디스크·450ms/24시간 SLO를 검증한 결과는 아니다. 증거는 `/private/tmp/indexer-mixed-pool-20261017/measurements.json`·`validation.json`·SQL/race/비교/소유권 로그다. UUID 소유 PostgreSQL 컨테이너/명시적 볼륨/측정 바이너리를 정리하고 기존 Docker10개를 유지했다.

## 연결4 수집·writer 병목 개선 전후 (2026-10-17)

앞 절의 혼합 부하에서 연결4를 고정하고 canonical 후보 처리 경로를 개선했다. **일반 경로의 선행 resolve RPC를 줄였으며 capture와 writer의 커밋 직전 fresh 검사는 그대로 유지했다.** 운영/예제 YAML, 의존성, 기존 writer·facts Source·PostgreSQL transaction/fence·RPC pool 코드는 변경하지 않았다.

확인한 병목은 다음과 같다.

- 개선 전 streaming 단계 계측의 rawQueue p50은482.958ms였다. 처리기는 raw 입력 유무와 관계없이 다음 높이를 조회하고, 미래 높이의 not-found를 기본1초 동안 candidate로 유지했다. 새 raw 알림도 이 시간을 무효화하지 않았다. 정상 블록에는 후보 선택 RPC와 capture commit fresh가 각각 있었다.
- DB commit p50은1.926ms, RPC HTTP p50은0.483ms인데 RPC limit wait는173.718ms였다. 이 로컬 입력에서 DB commit/HTTP 전송보다 공유 요청 예산과 트랜잭션 대기의 비중이 컸다. 실제 Arc의 네트워크 지연으로 일반화하지 않는다.
- capture는 generation row를 FOR UPDATE로 고정한 동안 fresh를 기다리고, writer는 자신의 bound transaction/SHARE fence에서 fresh를 확인한다. writer ACK도 CaptureUpdate의 FOR UPDATE가 필요하므로 앞선 capture와 경합한다. 개선 전 sourceACK p50은212.874ms였다. 기존 계측은 각 SQL lock 대기와 limiter 대기의 기여도를 완전히 분해하지 않으므로 그 비율을 확정하지 않는다.

`window.Candidate`는 다음 높이의 모든 필수 dataset이 단일 hash로 완성된 경우에만 참조를 반환한다. segment 높이 요약으로 범위를 거르고 한 번에 하나의 bounded index만 읽는다.24시간 블록 전체의 새 메모리 맵이나 payload cache는 만들지 않는다. 이 결과는 가용성 힌트다. 기존 profile 검증/조립과 generation transaction 내부의 fresh 검사를 통과해야만 원천을 저장하고 peer에 공개한다. 아직 없는 높이/부분 입력에는 선행 RPC를 보내지 않으며 durable metadata를 다시 보므로 알림 유실/가득 찬 channel/재시작에도 동작한다.

부분 fork까지 여러 hash가 있거나 hint의 fresh가 불일치/not-found이면 해당 transaction을 저장하지 않고 canonical RPC/Lookup으로 전환한다. 낮아진 RPC head보다 높은 retained orphan도 포함한다. fork 후보 갱신의 기본1초 간격은 유지한다. 오류 wrapper는 풀되 I/O·취소·rollback 오류가 함께 있으면 삼키지 않는다. 별도 limiter/캐시나 latest/빈 데이터 fallback을 추가하지 않았다. writer의 독립 디코딩 후 epoch 검사·bound SHARE fence·커밋 직전 fresh·durable commit 후 exact ACK, 기존 finality/GC/undo/outbox 계약도 유지한다.

측정은 같은 PostgreSQL16 fsync-on 서버의 별도 schema, pool4, 동일 Arc profile34블록(2 warm+32 timed)/68거래/544로그, 실제 ETH68/RLPx intake·window fsync·capture·writer·production HTTP GraphQL로 실행했다. 각 조건은 `[before,after]→[after,before]→[before,after]`로3회씩, 총12회다. 검증/정적 검사/200회 반복을 모두 끝내고 측정했다. 기준 바이너리는 기존 `resolved.go`를 Go build overlay로 복원했으며 양쪽은 **동일한 측정 코드**를 사용했다. 최초 SQL 실패 시 스크립트가 기준 바이너리도 정리해 overlay로 다시 만든 경위와 입력 SHA를 보존했다.

기존 opt-in `TestMeasureManagedFeedPoolLoad`에 `INDEXER_MEASURE_MIXED_INTERVAL_MS`를 추가했다. 기본500은 기존 초당2블록 부하이고1은32블록을 약32ms에 공개하는 backlog fixture다.1ms는 네트워크 생성 속도 주장이나 운영 설정이 아니다. 두 API client/각100ms ticker/목표 최대20회/s의 closed loop를 최종 ACK까지 유지했다. workers2/chunk1/acquisition_queue off/control1s/base plan/state feature off도 같다. 진행 관찰은20ms 메모리 snapshot이며 ACK 시각은 polling 상한이다. 단계 계측은 warm을 포함한34블록, 공개→완료 지연은 timed32블록이다. 아래는 **각 실행 percentile/rate를 구한 뒤3회 중앙값**이다.

| 지표 | 개선 전 | 개선 후 |
|---|---:|---:|
| 초당2블록:32블록 ACK 완료까지 초 | 17.420 | 16.380 |
| 초당2블록:공개 종료 후 drain 초 | 1.420 | 0.380 |
| 초당2블록:공개→capture p50/p95 ms | 807.135 / 1,414.645 | 178.940 / 346.977 |
| 초당2블록:공개→ACK p50/p95 ms | 1,119.945 / 1,719.982 | 380.953 / 759.947 |
| 초당2블록:rawQueue p50 ms | 482.958 | 9.434 |
| 초당2블록:sourceACK p50 ms | 212.874 | 8.584 |
| 초당2블록:최대 total gap 중앙값 | 4 | 2 |
| backlog:32블록 ACK 완료까지 초 | 14.021 | 13.020 |
| backlog:최종 ACK 처리 블록/초 | 2.282 | 2.458 |
| backlog:공개→capture p50/p95 ms | 7,191.445 / 13,373.827 | 5,037.948 / 9,561.791 |
| backlog:공개→ACK p50/p95 ms | 7,603.946 / 13,789.474 | 9,643.989 / 12,788.986 |
| timed 논리 block RPC(streaming/backlog) | 118 / 112 | 81 / 77 |
| timed 물리 HTTP(streaming/backlog) | 72 / 67 | 66 / 65 |

backlog 최종 처리량은약7.7% 개선됐다.2블록/s 조건의 전체 평균1.837→1.954블록/s에는16초의 공개 대기가 포함되므로 최대 처리 능력으로 해석하지 않는다. intake 자체의 무제한 속도나 실제 Arc SLO를 측정한 결과도 아니다.

**부하에 따른 지연 차이도 있다.** backlog에서 capture가 먼저 진행하며 peak capture→writer gap 중앙값이2→16블록으로 늘었다. 전체 완료와 ACK p95는 개선됐지만 공개→ACK p50은7,603.946→9,643.989ms로 늘었다. 빠른 원천 저장과 중간 projection 지연의 차이이며 모든 블록의 ACK 지연이 줄었다고 주장하지 않는다. streaming의 total gap 중앙값은4→2블록이었다. peak gap은 각 실행의 최대값 중앙값이며 독립 단계 관찰은 원자적 DB snapshot이 아니다.

| 최근8블록 API | 개선 전 p50/p95/p99 ms | 개선 후 p50/p95/p99 ms |
|---|---:|---:|
| streaming 공개 구간 시작 cohort | 2.227 / 3.797 / 7.887 | 2.706 / 7.255 / 65.466 |
| backlog drain 구간 시작 cohort | 2.319 / 4.024 / 7.413 | 2.639 / 9.071 / 34.163 |

API tail은 일부 증가했다. 실제 완료율은 streaming 공개 구간19.875→19.874회/s, backlog 전체19.971→19.938회/s였다. API 오류는12회 모두0이고 전체 측정 cohort는3641건이다. latency와 단순 요청 수 감소를 혼동하지 않는다. 다음 개선 대상은 필수 fresh/세대 fence를 보존하면서 수집과 writer/ACK 사이의 잠금 점유·공정성을 줄이는 것이다. 추가 proof/trace/lookup/search worker·무거운 API 부하의 예산은 별도로 측정한다.

12회 모두 최종34블록/68receipt·646 normalized outbox event digest와34 canonical input/ACK가 같았다. captureCommit/sourceValidate/sourceACK 각34회·오류0, timed receipt/balance/eth_call RPC0, 중단 뒤 API의 추가 RPC0을 확인했다. 기존 shared5 HTTP/s/burst1 검사도 매 실행 통과했다. 논리 요청 감소와 배치된 물리 HTTP 감소는 다르며 fresh 두 경로를 생략한 결과가 아니다.

native 관련8패키지 race와 최종 변경3패키지 재검증, App managed/node-feed/prepared60 tests/subtests(선택 Rust/측정2skip), 실제 fsync-on PostgreSQL10패키지129통과·test skip0, 최종 단일 CPU/race200회1000 tests/subtests, streaming/backlog 각각4블록 race를 통과했다. 전체 compile/vet·최종 관련 vet/전체 lint0·예제2모듈도 확인했다. 첫 reorg 회귀가 드러낸 retained future not-found를 보완한 뒤 같은 App/SQL 경로를 다시 통과했으며 초기 실패 로그도 보존했다. 테스트 전체/실제 Arc450ms/외장 mount/24시간 SLO를 완료한 것으로 표시하지 않는다.

증거는 `/private/tmp/indexer-capture-latency-20261017/measurements.json`·`validation.json`·전후 로그·입력 SHA다. UUID PostgreSQL 컨테이너/명시적 볼륨과 두 측정 바이너리를 정리하고 기존 Docker10개를 유지했다. 운영 config/노드/공개 RPC 활성화나 commit/push는 수행하지 않았다.

## 연결4 잠금 분리와 ACK 공정성 (2026-10-17)

앞 절에서 남은 capture/writer/ACK 잠금 경합을 줄였다. 이번 단계 시작 때 실제 코드를 기준 바이너리로 보존하고 같은 PostgreSQL16 fsync-on 서버·동일 입력으로 다시 비교했다. 이전 절의 수치와 직접 비율을 계산하지 않는다. 운영/예제 YAML, 의존성, 기존 writer·facts Source·raw Candidate·RPC pool·측정 fixture는 그대로다. DB 연결4, shared5 HTTP/s/burst1, capture와 writer의 transaction 내부 fresh를 유지했다.

기존에는 capture가 FOR UPDATE에서 fresh를 기다리는 동안 writer 검증/ACK가 같은 chain row를 기다렸다. 이제 optional port로 역할을 나눈다. `CaptureAppend`는 FOR NO KEY UPDATE로 append끼리만 직렬화하고, 이미 공개된 prefix의 writer 검증과 immutable `CaptureReceipt`는 KEY SHARE로 보호한다. Reset·finality·삭제·기타 가변 worker 갱신은 계속 FOR UPDATE다. **한 transaction에서 arbitrary mutable 값까지 읽는 일반 bound CaptureView는 FOR UPDATE로 전체 namespace를 고정**한다. unbound API/사전 읽기는 기존 RepeatableRead/read-only snapshot이다. 추가 pool/Store나 migration은 없다.

State 한 건 읽기·공개 prefix의 CanonicalAt·state/pointer/manifest ValidateProjection만 좁은 prefix view를 사용한다. Through는 전진할 수 있지만 writer가 검증한 generation/plan/anchor/기존 입력은 commit/rollback까지 바뀌지 않는다. capture Commit의 next-height/parent/epoch/plan/fresh 검사는 원래 위치에 남아 있다. Reset 뒤 Through보다 위에 남은 이전 fork pointer는 다음 높이 게시 때 교체할 수 있다. 그 이외 facts/event/object binding/ACK는 insert-only이고 동일 bytes만 재시도한다. ACK 쓰기는 projection/* 밖을 거부하고 두 callback이 모두 absent를 봤어도 서로 다른 bytes를 덮어쓰지 않는다. callback이 오류를 무시해도 transaction 전체를 rollback한다. optional port를 숨기는 wrapper/다른 driver는 기존 직렬 경로를 사용하며 fast 경로 오류 뒤 fallback은 없다. [정확한 잠금/범위 계약](CAPTURE_STORAGE.md)을 따른다.

실제 writer의2번 블록과 capture의4번 블록을 각각 별도 fresh 안에서 gate로 멈춰, 둘이 같은 세대에서 동시에 진입하는 것을 검사했다. 그 상태에서 이전 durable ACK 재시도·원천 읽기·API용 DB 읽기가 진행하고, staged projection/capture는 보이지 않으며 Reset은 막혔다. capture fresh 거절/취소 시 facts/cursor/event가 저장되지 않고, writer를 마친 뒤 Reset과 stale ACK/append 거절도 확인했다. SQL lock test와 동시 ACK의 동일/충돌 bytes 시험은 CPU1/race/count200에서800 tests/subtests를 통과했다. 일반 full view는 새 append/ACK까지 모두 차단한다.

비교 조건은 이전과 동일하다.2 warm+32 timed Arc profile 블록/68거래/544로그, 실제 로컬 ETH68/RLPx intake·window fsync·capture·ordered writer·production HTTP GraphQL, API 두 client/각100ms ticker(목표 최대20/s), workers2/chunk1/acquisition_queue off/control1s/base plan/state feature off다.500ms streaming과1ms backlog 공개를 각각 `[before,after]→[after,before]→[before,after]`로3쌍씩 실행했다. 검증/CPU 반복/static 검사를 끝내고 비교했으며 baseline/after는 같은 fixture를 사용했다. 다음 표는 실행별 percentile/rate를 먼저 계산한 뒤3회 중앙값이며 pooled percentile이 아니다. ACK는20ms nominal polling의 상한, 단계 계측은34/warm 포함, 공개→완료 지연은 timed32블록이다.

| 누적32블록 + API 부하 | 개선 전 | 개선 후 |
|---|---:|---:|
| 최종 ACK 완료까지 초 | 14.540 | 8.041 |
| 최종 처리 블록/초 | 2.201 | 3.980 |
| 공개→ACK p50/p95/p99 ms | 9,004.156 / 14,328.976 / 14,508.294 | 4,620.655 / 7,824.005 / 8,003.909 |
| 공개→capture p50/p95/p99 ms | 5,201.953 / 9,607.539 / 9,972.874 | 4,024.682 / 7,010.308 / 7,208.995 |
| capture→writer 최대 gap 중앙값 | 14 | 3 |
| sourceACK 단계 p50/p95/p99 ms | 64.353 / 277.680 / 381.887 | 16.631 / 49.813 / 80.781 |
| 최근8블록 API p50/p95/p99 ms | 4.942 / 33.828 / 89.179 | 5.709 / 17.023 / 41.285 |
| 실제 API 완료/초 | 19.876 | 19.745 |
| timed 논리 block RPC / 물리 HTTP | 79 / 66 | 72 / 39 |

누적 부하에서 ACK p50은약48.7%, 최근8블록 API p95/p99는약49.7%/53.7% 줄었고 처리량은약80.8% 늘었다. capture만 먼저 진행하던 peak writer gap이14→3블록으로 줄어 중간 ACK도 빨라졌다. 이는 관측한 공정성 개선이며 모든 부하의 strict FIFO/최대 대기시간 보장은 아니다.

**모든 실행/지표가 개선되지는 않았다.** backlog API p99의3개 실행 값은 전860.750, 89.179, 68.704ms, 후41.285, 200.769, 18.143ms였다. 두 번째 쌍에서는 tail이 늘었다. 같은 backlog API p50은4.942→5.709ms, DB commit p99도15.103→32.889ms였다. tail의 실행별 편차가 크므로 단기3회 중앙값을 장기 SLO나 통계적 유의성으로 해석하지 않는다. API 완료율/오류와 함께 보고하며 느린 표본을 제외하지 않았다.

| 초당2블록 + API 부하 | 개선 전 | 개선 후 |
|---|---:|---:|
| 32블록 최종 완료까지 초(공개16초 포함) | 16.361 | 16.420 |
| 공개→ACK p50/p95/p99 ms | 399.466 / 840.019 / 858.810 | 380.059 / 520.568 / 520.596 |
| 최근8블록 API p50/p95/p99 ms | 4.029 / 17.903 / 62.833 | 3.585 / 10.330 / 20.298 |
| sourceACK 단계 p95/p99 ms | 203.054 / 222.870 | 26.141 / 43.694 |
| 공개 구간 실제 API 완료/초 | 19.874 | 19.875 |
| timed 논리 block RPC / 물리 HTTP | 81 / 70 | 81 / 78 |

streaming 최종 완료는16.361→16.420초로 소폭 늘었고 공개 속도가 상한이므로 최대 처리량 증가로 표시하지 않는다. ACK/API tail 중앙값은 줄었지만 첫 after의 API p99는353.043ms로 높았다. streaming에서는 배치 타이밍 변화로 물리 HTTP가70→78회로 늘었다.5 HTTP/s 제한은 유지하며 더 낮은 부하에서도 물리 요청이 항상 감소한다고 주장하지 않는다.

12회 모두 최종34블록/68receipt·646 normalized outbox event digest,34 canonical input/ACK가 같았다. captureCommit/sourceValidate/sourceACK 각34회·오류0, API 전체3248건 오류0, timed receipt/balance/eth_call RPC0, 중단 뒤 API 추가 RPC0이었다. **필수 fresh 수는 줄지 않았다.** 누적 물리 HTTP66→39회는 독립된 두 transaction의 fresh가 기존 coalescer에서 함께 처리될 수 있기 때문이다. 논리 RPC79→72회에는 전체 실행이 짧아져 줄어든 control tick이 포함된다. 모든 물리 요청은 같은5 HTTP/s/burst1 envelope를 통과했다.

PostgreSQL10패키지169 tests/subtests·skip0, App managed/node-feed/prepared60통과/선택2skip, native capture/facts/port140통과/SQL 등54skip, SQL CPU1/race200회800통과, 같은 부하4블록 streaming/backlog race, 전체 compile/vet·최종 lint0·예제2모듈을 확인했다. fsync-off 서버에서 Update/Append/Receipt3경로 모두 callback 진입 전에 거절했다. 첫 SQL 회귀의 retained pointer 오류를 보완해 같은 suite를 다시 통과했다. fsync-off 전환은 command-line fsync=on 우선순위 때문에 한 번 적용되지 않아, 명시적 off의 별도 UUID 서버에서 재검증하고 정리했다. 실패/재실행 경위와 로그를 보존했다. 전체 app/SLO·실제 Arc peer·외장 장치·450ms/24시간 운영을 검증한 결과는 아니다.

증거는 `/private/tmp/indexer-lock-fairness-20261017/measurements.json`·`validation.json`·baseline 소스/바이너리 SHA·SQL/race/비교/소유권 로그다. 양/음성 시험 UUID 컨테이너·명시적 볼륨과 두 측정 바이너리를 정리하고 기존 Docker10개를 유지했다. 실제 RPC/노드 활성화·운영 YAML 변경·commit/push는 수행하지 않았다.


## API tail 계측과 불필요 작업 지연 (2026-10-17)

연결4 잠금 분리 뒤에도 실행별 API tail 편차가 남았다. 과거 느린 실행에는 세부 span이 없어 해당 표본 자체의 원인을 사후 확정할 수 없다. 같은 부하를 재현하기 위해 opt-in `postgres.ObserveQueries(ctx, callback)`으로 요청의 pool acquisition과 SQL 왕복/결과 소비 시간을 구분했다. SQL/인자/행 데이터는 전달하지 않고, 계측 context가 없는 호출은 시계를 기록하거나 callback을 호출하지 않는다. 실제 HTTP fixture만 server 전체·나머지 server 시간·client 전체 시간·요청별 SQL 수를 추가 기록한다. 진단 SQL/RPC나 제품 응답 변경은 없다. SQL 시간에는 서버 실행 외에도 전송·스케줄링/GC 정지가 포함될 수 있으며, 내부 wait event나 VM I/O로 단정하지 않는다.

개선 전 최근8블록 API는 매번 SQL3회를 실행했다. `GetLatestHeight`→`GetBlocks` 뒤 요청하지 않은 `totalCount`도 계산하며 `GetBlockCount`가 최신 높이를 다시 읽었다. 최초 진단의 느린181.050ms 요청은 SQL171.331ms·연결 획득1.734ms·server 전체180.567ms였다. streaming 공개 구간의 누적 연결 획득 p99는340.735ms, SQL p99는319.832ms였다. 서로 다른 요청일 수 있으므로 두 percentile을 합산하지 않는다. 단일 블록 조회도 느린 실행에서 함께 증가해 반복 조회 외의 조회 횟수만으로 설명되지 않는 실행 편차를 확인했다.

최종 변경은 기존 DB 조회 구조를 유지하고 GraphQL의 불필요한 작업을 지연한다. `blocks.totalCount`는 필드를 선택한 경우에만 계산하고, block의 `transactions` 상세 변환도 해당 필드가 요청될 때만 수행한다. `transactionCount`/`txCount`는 원본 거래 수를 사용한다. 두 값은 각각 요청 안에서 한 번만 계산해 aliases/fragments에서도 중복하지 않는다. 계약 생성 거래의 receipt 조회도 메타데이터 조회에서는 실행하지 않으며 거래 상세 요청에서는 기존 주소/필드 응답을 유지한다. 기존 필터·범위·페이지·오류·count fallback 의미를 유지하며 요청간 캐시/RPC/새 pool은 없다. 수집·writer·generation lock·fresh·원천 디코딩/ACK 코드는 그대로다.

연결 대기를 줄이기 위해 최종 구성은 **같은 pool4에서 API에 최소1개를 예약하고 나머지3개는 일반 작업이 공유**한다. API는 사용 가능한 일반 연결도 짧게 빌려 병렬로 읽는다. 별도 pool/Store나 추가 연결은 만들지 않는다. optional `port.QueryReaderReserver`의 lease가 기존 pool에서 연결 하나를 빌리고 API request context로만 선택한다. `q(ctx)`는 같은 Store의 bound transaction을 먼저 확인하므로 커밋 검증·staged 읽기·undo/outbox는 원래 연결을 쓴다. API용 연결은 transaction을 유지하지 않고 매 statement의 committed snapshot을 읽는다. 다른 Store의 lease/transaction은 선택하지 않는다.

예약 슬롯은 한 SQL의 rows/Scan/Exec 동안만 cancellable gate를 점유하며, model 디코딩/HTTP 응답/RPC 대기에는 점유하지 않는다. rows의 EOF/Close와 Row.Scan이 gate를 정확히 한 번 반환한다. 끊어진 연결은 같은 pool 안에서 다시 얻고, API Stop/최종 참조 해제는 원래 pool에 슬롯을 반환한다. Store.Close도 lease를 회수한다. SQL 인자는 기존 server parameter binding을 그대로 전달한다. 예약 슬롯이 바쁘면 같은 pool의 일반 연결을1ms씩 시도하고, 입장만 재시도하며 SQL은 재실행하지 않는다. 빈 일반 연결이 나중에 생기는 nested query도 진행할 수 있다. 연결 생성 오류에는50ms backoff를 두고 요청 취소/종료를 즉시 관찰한다. `ObserveQueries`의 acquire는 선택·대기·재연결 시간을 한 번만 측정하고 내부 tracer의 중복 기록을 막는다. 종료는 예약/차용 양쪽 rows와 대기자가 모두 해제될 때까지 기다린다.

단일 체인의 `node.role: all`과 명시적 `database.postgres.max_conns: 4` 조합에서 앱이 이 정책을 선택한다. source/validate/fresh는 같은 Store의 일반 pool 연결에서 계속 동작한다. 전용 API/ingest 역할, 예제의2연결, 명시적1/default0 및 다른 예산은 자동 변경하지 않는다. 읽기 전용 Store와4 미만 pool은 예약하지 않는다. 직접 API 생성은 `api.Config.ReserveQueryReader`로 opt-in하며 기본값은 false다. 현재 예약은 일반 `q(ctx)` projection 조회의 root API 범위이고, source의 별도 snapshot API·멀티체인 route/추가 worker·무거운 조회 예산은 별도 검증 범위다.

실제 pool 연결3개를 일반 작업이 점유한 상태에서 네 번째 일반 작업은 대기/취소되고 production HTTP API는 예약 슬롯으로 진행했다. staged projection은 API에 보이지 않고, lease context를 붙인 bound 읽기는 staged 값을 보아 원래 transaction을 우선함을 확인했다. 다른 Store의 context 격리, API 대기 취소/rows 반환, 연결 단절 복구,4개 상한, SQL 공격 문자열의 값 바인딩, lease 참조/Close 회수와1/2연결·read-only 제외를 검사했다.

중간 후보인 optional 최신 페이지 단일 SQL은 **최종 코드에서 제외했다**. 같은 바이너리로 최초3쌍과 고정 추가2쌍(별도 같은 설정의 서버 구간)을 비교한20회에서 streaming API p99는 개선됐지만 누적 부하의 tail이 악화됐다. 관련 port/SQL/테스트도 제거했고 모든 원본 결과·실패하지 않은 느린 표본을 보존했다. 후보5회 중앙값의 누적 API p95/p99는6.712/13.573→10.430/65.307ms였다. SQL 수가 그대로인 단일 블록 API도 같은 느린 실행에서 증가했다. 후보의 결과를 최종 변경의 성능 근거로 재사용하지 않는다.

추가 프로파일2회는 큰 API tail을 재현하지 못했다. GC 정지 최대2.418ms(전)/0.121ms(후),10ms 초과 PostgreSQL lock wait0, 서버 commit 최대178.548ms를 기록했다. 이 관측만으로 앞서 나온354ms 연결 획득 표본의 원인을 확정하지 않는다. GC/실행 trace와 서버 duration/lock 로그가 있는 진단 실행은 일반 비교 percentile에 섞지 않는다.

최종 정책 전의 지연 계산 단독 변경도32블록/128블록으로 비교했다.128블록 누적 부하의 API p50은2.267→2.038ms였지만 연결 대기 p99는10.189→41.482ms, API p99는28.135→56.522ms였다. 이 결과로 불필요 작업 제거만으로 tail이 안정되지 않음을 확인하고 예약 정책을 추가했다. 이 중간 결과도 최종 정책의 개선 수치로 재사용하지 않는다.

예약 슬롯 하나만 고정 사용하는 중간 정책은 실제 일반 작업과 API의 연결 경합을 막았지만 API끼리 직렬화되어 streaming 입장 p50이0.003→0.755ms로 증가했다. 이를 최종 정책으로 채택하지 않고 최소 예약+사용 가능한 일반 연결 차용으로 보완했다. 실제 다른 backend PID에서 두 SQL이 동시에 열린 상태를 검증했으며, 종료는 예약 rows를 닫아도 차용 rows가 남아 있으면 기다린다.


최종 비교는2 warm+128 timed Arc profile 블록(130블록/260거래·receipt/2080로그), 실제 로컬 ETH68/RLPx·window fsync·capture·ordered writer·production HTTP GraphQL을 사용했다. pool4/workers2/chunk1/acquisition_queue off/control1s/base plan/선택 worker off, 두 API client/각100ms ticker(목표 최대20/s closed loop)를 고정했다.500ms streaming과1ms backlog 공개를 `[before,after]→[after,before]→[before,after]`로3쌍씩 비교했다. 최적화 전은 보존한 원본 GraphQL2파일과 예약 비활성 method의 Go overlay이며 공통 fixture/계측은 같다. 모든 build/static/race 검사를 끝내고 측정했다. 실제 운영/예제 YAML과 의존성은 바꾸지 않았다.

아래는 **실행별 percentile/rate의3회 중앙값**이며 pooled percentile이 아니다. backlog API는 약128ms 공개 구간 이후 drain cohort, streaming API는64초 공개 구간의 cohort다. 나머지 cohort와 늦게 완료한 요청도 원본에 보존하고 전체 오류/완료율에 포함한다. API timeout/오류도 AllMS에 남긴다. ACK는20ms nominal 관측 지연을 포함하며 단계 Count130은 warm 포함, bounded latency sample은 최근 범위다.

| 누적128블록 + API | 개선 전 | 최종 |
|---|---:|---:|
| 최근8블록 API p50/p95/p99 ms | 2.224 / 4.675 / 12.173 | 1.950 / 3.348 / 5.901 |
| API 입장 대기 p99 ms | 3.391 | 0.042 |
| 전체128블록 최종 완료 초 | 26.040 | 26.040 |
| 블록/초 | 4.915 | 4.915 |
| 공개→ACK p50/p95/p99 ms | 13,116.051 / 24,678.312 / 25,673.099 | 13,176.123 / 24,698.059 / 25,693.781 |
| 실제 전체 API 완료/초 | 19.939 | 19.923 |
| timed 논리 block RPC / 물리 HTTP | 282 / 130 | 282 / 130 |

| 초당2블록 + API | 개선 전 | 최종 |
|---|---:|---:|
| 최근8블록 API p50/p95/p99 ms | 2.568 / 6.615 / 31.347 | 1.979 / 3.348 / 8.239 |
| API 입장 대기 p99 ms | 1.012 | 0.056 |
| 공개64초 포함 최종 완료 초 | 64.281 | 64.340 |
| 공개→ACK p50/p95/p99 ms | 380.264 / 480.301 / 500.568 | 379.761 / 479.996 / 500.795 |
| 실제 전체 API 완료/초 | 19.975 | 19.975 |
| timed 논리 block RPC / 물리 HTTP | 320 / 315 | 320 / 312 |

최근8블록 API p95/p99는 backlog 약28.4%/51.5%, streaming 약49.4%/73.7% 줄었다.3개 실행의 p99는 backlog 전12.173/8.190/20.104→후5.901/3.958/9.826ms, streaming 전31.347/37.506/16.728→후6.814/8.239/13.441ms였다. 모든 개별 지표가 개선되지는 않았다. backlog의 단일 블록 API p95/p99는2.023/3.640→2.163/4.735ms로 늘었고 ACK p50도약60ms 늘었다. streaming의 단일 블록 API p50은1.148→1.191ms였다. 최종 처리량 증가/모든 요청의 최대 지연/통계적 유의성/장기 SLO를 주장하지 않는다.

12회 모두 최종130블록/260receipt·2,470 normalized outbox event digest와130 canonical input/ACK가 같았다. captureCommit/sourceValidate/sourceACK 각각130회·오류0, 전체 API10,826건 오류0, timed receipt/balance/eth_call RPC0, 중단 뒤 API 추가 RPC0이었다. 최근8블록의 SQL3→2회, 단일 블록1회, before 예약0/after 최소예약1을 확인했다. 모든 물리 HTTP는 같은5/s/burst1 envelope를 통과했고 필수 fresh는 capture와 writer의 원래 transaction 안에 남았다.

최종 PostgreSQL/race65 tests/subtests·skip0, 프런트엔드 앱12통과,130블록/전체 outbox의 혼합 부하 race, native API/GraphQL286통과/선택1skip, 포트/계층17통과, compile/vet/lint0 및 예제2모듈을 확인했다. 초기 전체 SQL894통과와 App202개 최상위 항목의 분할 완료도 원본 기록에 보존한다. App의8분 전체 한도와 턴 중단으로 종료된 비교는 구분했다. 완료4회는 보존하고 미완료 로그를 별도 보관한 뒤 같은 바이너리 SHA·소유 서버·fsync를 확인해 남은8회만 재개했다.

원본·중간 후보·프로파일·최종12회와 감사 기록은 `/private/tmp/indexer-api-tail-20261017`에 있다. 모든 UUID-owned PostgreSQL/볼륨을 제거하고 기존10개 컨테이너의 이름/status/StartedAt을 보존했다. 비교 바이너리9개/602,449,650bytes를 정리했다. 로컬 고정 입력/짧은 metadata API의 결과이며 실제 Arc peer·외장 mount·24시간 운영·무거운 API/선택 worker SLO 증거는 아니다.

## 단일 블록 p99와 예약 연결 반환 대기 (2026-10-17)

3.64의 단일 블록 backlog p99 중앙값은3.640→4.735ms였지만, 각 실행은 전3.640/2.161/18.684ms와 후4.735/3.058/9.684ms로 크게 겹쳤다. 요청 약259개의 nearest-rank p99는 세 번째로 느린 표본이다. 기존 집계 percentile끼리 더하거나 이를 재현 가능한 SQL/잠금 회귀로 단정하지 않는다. 후속 fixture는 opt-in 혼합 부하에서 **모든 요청의 상관된 입장·SQL·서버·HTTP 시간과 오류**를 보존한다. 블록1 metadata 조회는 여전히 SQL1회, 최근8블록 metadata는2회다. production 응답/캐시/스키마는 바꾸지 않았다.

예약 슬롯이 사용 중일 때 기존 `reservedQueryReader.acquire`는 일반 pool 입장에 최대1ms 동안 동기 대기했다. 이 사이 예약 연결이 반환되어도 해당 pool 대기가 끝날 때까지 반환을 관찰하지 못했다. 1ms 한도를 보존한 후보 구현은 같은1ms 차용 예산 안에서 pool 확보와 예약 gate 반환을 함께 기다린다. 실패/취소/종료/예약 우선 전환 때 probe를 취소하고 합류하며, 경합에서 이미 얻은 일반 연결도 반환한다. 한 waiter의 동시 probe는 최대1개다. SQL은 한 번만 실행하며, 연결 오류50ms backoff·4연결 전체 예산·최소 API 예약1개·bound transaction 우선·같은 Store/세대 fence를 유지한다. capture/writer의 uncached commit fresh와 공유5 HTTP/s/burst1은 바꾸지 않았다.

일반 연결 입장을 무기한 기다리는 첫 후보는 전체12회에서 streaming 단일 블록 p99가45.700→14.921ms였지만 backlog는15.114→45.644ms, 처리량은4.808→4.441블록/s였다. 이 정책은 제외했다. 호스트 부하도 크게 흔들렸으므로 무기한 대기가 해당 회귀의 원인이라고 확정하지 않는다. 후속 후보는 원래의1ms 입장 한도를 보존한다. 중간12회는 `indefinite-probe/`에 별도 보존하고 최종 비교와 합치지 않는다.

후속 후보의 반환 전환 분리 측정은 같은 pool4·같은 SQL adapter에서, 일반 연결의 prepare 입장이 막힌 동안 예약 연결을 반환하고 waiter가 전환할 때까지256회 관측했다. 전후3쌍의 실행별 percentile 중앙값은 p50 **1.127500→0.002542ms**, p95 **1.146542→0.007792ms**, p99 **1.212667→0.020375ms**다. 이 통제된 admission-only 측정에는 실제 HTTP/SQL 실행·수집 처리량이 포함되지 않으며 혼합 부하의 p99로 재사용하지 않는다. 같은 후보 회귀 검사는 보존한 이전 코드에서 실패하고 후보 코드에서 통과했다. 후보와 해당 회귀 검사는 원본 실험 폴더에 보존하고 제품 코드에서는 원복했다. 특정1회의 타이머 기한을 앞서야 하는 assertion 대신 반복 반환에서 deadline-only 동작과 명시적 probe 취소를 구분한다.

별도 PostgreSQL duration 진단의 block1 SQL280회(warm 포함)는 execute p99/max **0.322/0.566ms**, parse+bind+execute 합 p99/max **1.375/18.203ms**였다. 같은 실행의 drain HTTP274회는 p99 **32.933ms**, client SQL 왕복 p99 **10.861ms**였다. 서로 다른 모집단/percentile을 빼서 잔여 시간을 계산하지 않는다. 실제 HTTP p99 요청은 입장30.080ms·SQL1.558ms·서버31.902ms·전체32.933ms였다. SQL query span은 wire/결과 소비를 포함하며 순수 PostgreSQL 실행 시간이 아니다. 이 진단은 SQL duration 로깅을 켠 별도 실행이므로 주 비교에 섞지 않고, 종료 때 설정을 복원했다.

공유 macOS12 CPU의 부하 평균은 진단 중28.08/53.80/40.35, 후속16.45/35.58/35.22였고 FSEvents·가상화·여러 기존 앱의 CPU 사용도 관측했다. 이는 SQL 왕복/서버 밖 구간의 흔들림에 대한 환경 단서이며, 과거 특정 요청의 지연 원인이나 이번 코드의 인과 효과를 독립적으로 증명하지 않는다. 기존 프로세스/컨테이너를 중지하거나 설정을 바꾸지 않았다. SQL의 한 번 조회·정확성과 예약 반환 반응 개선을 전체 HTTP p99/운영 SLO 개선과 구분한다.

1ms 한도 후보의128블록 주 비교도 전후3쌍씩 실행했다. 단일 블록 p99 중앙값은 backlog **4.527→5.971ms**, streaming **18.655→22.450ms**였다. backlog 처리량4.852→4.848블록/s, streaming1.988→1.989블록/s였다. 일부 입장 지연은 줄었지만 목표인 HTTP p99 개선은 확인되지 않았다. 따라서 **두 연결 정책 후보 모두 채택하지 않고 `query_reader.go`와 기존 unit tests를 작업 시작 상태로 복원했다**. 예약1+짧은 차용이라는3.64 정책과 운영 설정을 유지한다. 새로 남긴 코드는 opt-in fixture의 전체 요청 기록과 SQL 없는 반환 전환 측정 helper다.

각 후보의 주 비교12회씩 총24회는130블록/260receipt/2080로그/2470 outbox digest, capture/sourceValidate/sourceACK 각각130회·오류0과5 HTTP/s/fresh를 보존했다. 전체 API는 첫 후보11,074건·후속10,841건, 오류0이었다. API p99 증가 원인을 알고리즘 하나로 확정하거나 admission-only 개선을 종단 성능 개선으로 보고하지 않는다.

별도32블록 실행 위치 진단은 원래 제품 코드로 macOS process→Docker DB와 같은 PostgreSQL 컨테이너 안 Linux process→DB를3쌍씩 비교했다. API point p99 중앙값은 backlog6.548→1.838ms, streaming4.113→2.402ms였다. 처리량은 각4.848/약1.96블록/s로 비슷했다.12회 모두34블록/68receipt/544로그/646 outbox digest와34 capture/validate/ACK, API2,744건 오류0을 보존했다. native CGO와 Linux CGO0·OS·raw temp 저장 위치가 달라 pure network 비교가 아니다. 특히 backlog의65~66개 표본에서는 p99가 최대값이므로 탐색 결과로만 취급한다. 이 진단을128블록 개선 수치나 운영 SLO로 재사용하지 않는다.

### 빌드 옵션을 맞춘128블록 실행 위치 비교

32블록 진단 뒤 native와 Linux를 모두 **동일 Go1.26.8·CGO0**으로 빌드했다. 원래 제품 코드(예약1+1ms 동기 차용)를 쓰고, native→Docker PostgreSQL과 **서로 별도인 app/DB 컨테이너→전용 bridge**를 비교했다. 같은 PostgreSQL 서버/영구 named volume·pool4·5 HTTP/s/burst1·fresh를 유지했다. raw window는 각 OS의 test temp 디렉터리다. OS/runtime·네트워크·raw 저장 위치를 함께 비교한 결과이며 pure network causal A/B는 아니다. 실제 Arc peer/외장 mount/장기 운영은 포함하지 않는다.

2 warm+128 timed/260거래·receipt/2080로그, workers2/chunk1/base plan/선택 worker off, API 두 client/100ms bounded closed loop(최대20/s)를 고정했다.1ms backlog 공개와500ms streaming을 각각 `[native,Docker]→[Docker,native]→[native,Docker]`로3쌍씩 실행했다. 모든 build/race/static 검사를 끝내고 측정했으며 계측은 양쪽에 같다. 전체 API 행을 JSON에 추가하는 일은 timed 종료 뒤라 새 SQL/RPC/요청 중 clock을 추가하지 않는다.

아래는 **실행별 percentile/rate의3회 중앙값**이다. backlog API는 drain, streaming API는 injection cohort다. timeout/오류·다른 cohort·늦은 완료도 원본에 남긴다. percentile끼리 더하지 않는다.

| 누적128블록 | native→Docker DB | Docker app→DB |
|---|---:|---:|
| 단일 블록 p50/p95/p99 ms | 1.059 / 1.664 / 3.059 | 0.698 / 1.117 / 1.855 |
| 최근8블록 p99 ms | 5.351 | 2.953 |
| 최종 블록/s | 4.885 | 4.961 |
| 공개→ACK p50/p95/p99 ms | 13,336.058 / 24,898.498 / 25,873.097 | 12,927.044 / 24,462.036 / 25,455.403 |
| 실제 API 완료/s | 19.924 | 19.922 |

| 초당2블록·128블록 | native→Docker DB | Docker app→DB |
|---|---:|---:|
| 단일 블록 p50/p95/p99 ms | 1.093 / 3.234 / 10.379 | 0.875 / 1.839 / 4.780 |
| 최근8블록 p99 ms | 25.809 | 7.614 |
| 최종 블록/s | 1.988 | 1.989 |
| 공개→ACK p50/p95/p99 ms | 400.366 / 599.970 / 840.789 | 340.521 / 440.496 / 515.088 |
| 실제 API 완료/s | 19.975 | 19.987 |

단일 p99 중앙값은 backlog 약39.4%, streaming 약53.9% 낮았다. backlog 각 쌍은2.659/4.559/3.059→1.855/1.889/1.265ms였다. streaming은2.508/10.379/28.022→3.592/7.138/4.780ms로 **첫 쌍은 악화했다**. 따라서 모든 실행/요청의 개선·코드의 인과 효과·통계적 유의성·운영 SLO를 주장하지 않는다. 처리량은 여전히5 HTTP/s와 입력률에 가까웠다. 이 결과는 실행 환경을 고정해 재측정할 근거이며 새로운 연결 정책 채택 근거가 아니다.

12회 모두130 canonical 입력/ACK·260receipt·2080로그·2470 normalized outbox digest가 같았다. capture/sourceValidate/sourceACK 각각130회·오류0, API **10,839건 오류0**이었다. 단일 SQL1회·목록 SQL2회, 같은 Store/bound transaction·post-decode epoch·커밋 fresh·파라미터 바인딩을 유지했다. 운영/예제 YAML·의존성·handoff와 제품의 연결 코드는 바꾸지 않았다.

### 재현과 남은 범위

`python3 scripts/measure-api-placement.py --output /private/tmp/indexer-api-placement-run --blocks 128 --rounds 3`로 새 디렉터리에 재현한다. Go/Docker/Python3와 로컬 Docker Unix socket이 필요하다. 이 도구는 실제 노드나 운영 config를 열지 않는다. 고유 owner label의 DB/app 컨테이너·전용 network·named DB volume을 만들고, CGO0 두 바이너리 빌드를 끝낸 뒤 순차 측정한다. API/수집의 실제 callback/서버/HTTP 기록과 source hash·build info·정확성 검증을 보존한다. 성공/실패/중단에서 소유 자원과 비교 바이너리를 정리하고 로그/요약은 남긴다. 원격 Docker endpoint는 자원 생성 전에 거부한다. 앱 이미지의 불필요한 DB volume은 tmpfs로 덮어 익명 volume을 만들지 않는다. `--blocks 4 --rounds 1`은 기능·정리 smoke이며 성능 근거로 쓰지 않는다.

과거3.64 API 결과의 증가를 재현 가능한 단일 SQL 알고리즘 회귀로 확정하지 못했다. 현재 결론은 **제어된 반환 대기 문제와 HTTP 전체 tail을 구분하고, 실행 환경을 맞춰 측정하며, 종단 개선이 검증되지 않은 연결 변경은 제외한다**는 것이다. 다음은 실제 배포의 app/DB 배치·CPU/IO 부하를 고정한 반복/장기 측정과 무거운 API/추가 worker 예산이다. 실제 Arc peer·외장 mount·24시간 SLO는 계속 미검증이다.

원본은 `/private/tmp/indexer-point-tail-20261017`에 있다. root의 `measurements.json`은 제외한1ms 비동기 probe 후보이며 **최종 실행 위치 결과는 `placement128/measurements.json`**이다. 무기한 후보는 `indefinite-probe/`,32블록 탐색은 `placement/`, SQL duration은 `profile-*`, 반환 분리는 `wake-*`로 구분한다. 초기 test helper의 비동기 connection destroy 즉시 assertion·고정pgx5.7.5에 없는 hook 및 driver env 중복은 본 측정 전 수정했고 실패 로그를 남겼다. 이전 후보의 실패/예상 negative control과 최종 제품 검사를 혼동하지 않는다.

최종 관련 race tests/subtests는 반복 포함367회 통과·선택2skip이었다(Reader/binding16, facts/fresh49, CPU1 wake3,128블록 혼합1, API/GraphQL286, frontend12). compile/vet/lint도 통과했다. 재현 도구의4블록 정상 실행, 의도한 build 실패 뒤 자원 정리, 원격 endpoint 사전 거부를 검증했다. 모든 테스트 소유 DB/app 컨테이너·network·volume(이미지가 만든 빈 volume 포함)을 제거했고, 기존10개 컨테이너의 이름/status/StartedAt을 보존했다. 비교 바이너리7개·367,070,381bytes도 정리했다.


## Docker streaming p99와 조회 완료 대기 (2026-10-17)

3.65의 첫 streaming 쌍은 단일 블록 p99가 native2.508→Docker3.592ms, 최근8블록이4.690→6.281ms로 증가했다. 해당 Docker 단일 p99 요청의 admission은0.005709ms, SQL wire1.661208ms, 나머지 server1.289083ms, HTTP 나머지0.636ms였다. p99 이상7건 중 단일6건·목록7건에서 SQL wire가 가장 컸다. **SQL wire에는 서버 실행뿐 아니라 클라이언트 통신·행 반환·드라이버 종료 대기가 포함된다.** 당시 실행에 CPU/스케줄 추적은 없어 단일 원인으로 소급 확정하지 않는다.

별도32블록 Docker 진단에서 고정 pgx5.7.5의 취소 감시는 취소 가능한 각 쿼리마다 고루틴을 만들고, 정상 완료에서도 unbuffered channel 전송으로 해당 고루틴을 기다렸다. `queryReaderRow.Scan` 하위 누적 scheduling delay가22.066760ms였다. 변경 후 같은 진단은0.008256ms였다. 이 값은 전체 HTTP p99가 아니라 추적 중 누적 스케줄 대기다. 전체 앱 scheduling delay는432.294→550.161ms로 늘었고, 배경 작업·프로파일러·공유 호스트 변동을 포함하므로 앱 전체 개선으로 해석하지 않는다.

예약된 API 조회에만 `context.AfterFunc`와 pgx의 기존 socket-deadline handler를 적용했다. 정상 완료는 실행되지 않은 callback을 멈추고, 취소된 IO는 기존 socket deadline으로 중단한다. 행이 닫힐 때까지 감시를 유지하고, 시작된 callback을 join·deadline reset한 뒤 예약/차용 연결을 반환한다. `WithoutCancel`은 값/계측을 유지하며 이 감시가 취소를 담당하게 한다. 원래 context의 취소·deadline 오류는 `errors.Join`으로 복원해 pgconn timeout/retry 오류도 보존한다. SQL·Scan·not-found 오류와 성공 결과를 뒤늦은 취소로 덮지 않는다. admission과 SQL 직전 취소 확인, SQL1회 실행, 모든 반환/Close의 once 계약을 유지한다.

same Store의 bound transaction이 여전히 먼저 선택된다. source/facts/writer·독립 post-decode epoch·capture와 writer 커밋 직전 uncached fresh·generation fence·GC/undo/outbox·5 HTTP/s/burst1은 변경하지 않았다. **4개 물리 연결 중 최소1개 API 예약과 유휴 일반 연결 차용**도 동일하다. API 전용/read-only/1·2연결/default0/멀티체인·source snapshot의 기존 범위를 확대하지 않았다. Go 최소 버전·의존성·운영/예제 YAML·handoff·GraphQL과 측정 fixture/계측은 변경하지 않았다. 구현은 `pkg/storage/postgres/query_io.go`, 연결/행 수명 통합은 `query_reader.go`다.

### 동일 Docker 조건의 재측정

같은 Go1.26.8/CGO0 Linux arm64 바이너리2개를 미리 빌드했다. 동일한 별도 app/PG16 컨테이너·bridge·named DB volume을 사용하고,2 warm+128 timed/260거래·receipt/2080로그, workers2/chunk1/base plan/추가 worker off, API2 clients/최대20/s를 고정했다. 각 모드에서 `[전,후]→[후,전]→[전,후]`의3쌍을 순차 실행했다. 본 측정에는 프로파일러·SQL duration logging·동시 테스트/빌드를 사용하지 않았다. 아래는 **실행별 percentile/rate의3회 중앙값**이다. 다른 cohort·오류·늦은 완료를 원본에서 제외하지 않았으며, 표는 명시한 API cohort만 비교한다. ACK는20ms nominal polling의 관측 상한이다.

### 초당2블록·128블록 (injection API cohort)

| 지표 | 개선 전 | 개선 후 |
|---|---:|---:|
| 단일 블록 p50/p95/p99 ms | 0.749 / 1.317 / 4.059 | 0.713 / 1.276 / 3.518 |
| 최근8블록 p50/p95/p99 ms | 1.127 / 2.394 / 5.343 | 1.080 / 1.969 / 4.466 |
| 최종 블록/s | 1.989 | 1.989 |
| 공개→ACK p50/p95/p99 ms | 360.156 / 460.135 / 480.975 | 360.535 / 460.725 / 479.996 |

### 누적128블록 (drain API cohort)

| 지표 | 개선 전 | 개선 후 |
|---|---:|---:|
| 단일 블록 p50/p95/p99 ms | 0.707 / 1.230 / 4.904 | 0.660 / 1.115 / 1.893 |
| 최근8블록 p50/p95/p99 ms | 1.098 / 1.959 / 9.985 | 0.986 / 1.764 / 3.674 |
| 최종 블록/s | 4.961 | 4.961 |
| 공개→ACK p50/p95/p99 ms | 12,932.144 / 24,467.313 / 25,461.431 | 12,947.293 / 24,480.390 / 25,455.918 |

streaming 단일 p99는 각 쌍4.058500/3.621750/5.230958→2.808458/3.517709/5.123291ms였다. 목록은5.342500/5.026708/5.969042→3.813333/4.465625/6.793750ms로 **세 번째 쌍은 증가했다**. 같은 쌍에서 공개→capture p99가332.595→1311.422ms, 공개→ACK가499.760→1519.851ms, rawAppend 최근-window p99가131.111→196.481ms, 블록 수신 간격 p99가570.979→1108.042ms로 늘었다. writer fresh p99는185.019→185.995ms였다. 해당 목록 p99 요청은 admission0.007999ms/SQL wire5.595875ms였다. 저장·수신 지연이 함께 관측됐지만 profiler 없는 본 실행에서 OS/VM/IO의 직접 원인을 확정하지 않는다.

누적 부하에서도 첫 목록 p99는9.984917→11.146125ms로 증가했다. 모든 지표의 개선은 아니다. streaming 단일 SQL wire p99 중앙값도1.864375→2.290458ms로 증가했다. 요청별 percentile은 서로 다른 요청일 수 있어 admission/SQL/server p99끼리 합산·차감하지 않는다. 이 변경은 API의 확인된 완료 비용을 제거하고 짧은 로컬 비교의 종단 p99 중앙값을 낮췄으며, 모든 실행의 tail 해소나 통계적 유의성·운영 SLO를 증명하지 않는다.

12회 모두130 capture/sourceValidate/sourceACK·오류0,260 receipt·2080로그·2470 normalized outbox digest가 같았다. **API10,820건·오류0**, 단일 SQL1회/목록2회, 공유 물리 RPC5/s envelope와 offline API의 RPC0을 기존 fixture assertion으로 확인했다. 처리량은 입력률 또는5 HTTP/s 제한에 가까웠다. 실제 Arc peer/외장 mount/24시간·무거운 API/추가 worker는 계속 별도 범위다.

CPU1/race 반복은 관련 tests/subtests45회, facts/fresh/epoch17회, 실제 full-background HTTP API1회와4블록 혼합 race1회가 통과했다. 전체 API suite·vet·lint(0 issues)도 통과했다. helper의 초기 실패는 pgx Query의 지연 오류를 rows.Err에서 읽는 것과 취소된 차용 연결의 비동기 destroy 완료를 기다리도록 수정한 뒤 최종 통과했다. 실패 로그도 보존했다.

실험 원본은 `/private/tmp/indexer-docker-stream-tail-20261017/measurements.json`이다. `compare.py`는 소유 테스트 컨테이너의 두 사전 빌드 바이너리를 실행한 정확한 드라이버이며 `comparison-builds.json`·source hash·전체 요청·프로파일·`streaming-round3-diagnostics.json`을 보존한다. 이 경로의 measurements.json은 최종 결과이며, 이전 `/private/tmp/indexer-point-tail-20261017/measurements.json`의 제외 후보와 다르다. 기존 실행 위치 도구는 그대로 `scripts/measure-api-placement.py`를 사용한다. 두 비교 바이너리125,120,439bytes와 테스트 소유 컨테이너2개·network·volume을 제거했다. 기존10개 컨테이너의 이름/status/StartedAt과 보호 파일 hash가 유지됐고 정리 오류0이었다.


## 원본 저장·수신·ACK 분리와 같은 블록 fsync 공유 (2026-10-17)

3.66의 일부 streaming 실행에서 raw 저장·수신 간격과 ACK가 함께 지연됐다. 이 단계는 **DB 연결4개·기존 최소 API 예약1개/유휴 연결 차용·공유5 HTTP/s/burst1·capture와 writer 커밋 직전 uncached fresh**를 유지하며 원본 경로를 점검했다. 같은 Store의 bound transaction·post-decode epoch·generation/삭제 fence·SQL 바인딩·undo/outbox/ACK 의미도 유지한다.

### 원인 분리와 제외한 후보

별도32블록 trace의 느린30번 블록은 공개→수신1.720ms, 공개→원본 durable395.076ms, 공개→capture570.810ms, 실제 ACK782.674ms였다. 그 블록의 원본 append393.322ms에는 data fsync161.881ms·세그먼트 seal207.754ms·생성22.936ms가 포함됐고 mutex 대기는0.093ms였다. **파일 동기화와 교체가 같은 수신 worker의 다음 요청을 늦추는 구간**을 확인했다. 모든 tail의 단일 원인이나 과거 실행의 OS/VM 원인을 소급 확정한 것은 아니다. ACK에는 이후 fresh/RPC admission·DB commit·완료 기록 대기도 포함된다.

첫 후보는 Next 호출자1개와1칸 채널로 다음 입력 수신만 선행시켰다. 별도 trace에서는 다음 블록 요청과 이전 저장의 겹침이0/32→32/32로 확인됐지만,128블록·3쌍의 streaming capture p99 중앙값566.732→397.001ms와 달리 ACK799.864→979.559ms, 누적 ACK26111.895→26478.970ms로 증가했다. **이 후보는 제외하고 수신 pipeline을 원복했다.** 코드/검사/12회 원본은 `prefetch-probe/`에 보존하며 최종 결과와 섞지 않는다. 추가 수신 goroutine·cross-block payload 큐는 남기지 않았다.

### 적용한 저장 경로

`chainfeed.BatchStream`은 이미 검증·길이 제한한 **같은 block identity의 서로 다른 dataset 최대3개**를 함께 반환한다. ETH68 adapter가 기존에 한 번에 fetch하던 header/body/receipt 소유 bytes를 추가 요청/복사 없이 제공한다. 일반 `Stream.Next`도 그대로 지원한다. `capture.PipelineTasks`는 전체 그룹을 검사하고 `window.AppendBatchWait`로 저장한 뒤에만 각 durable reference의 callback/알림을 순서대로 보낸다. 원본 fsync 자체는 source/projection ACK가 아니다.

`AppendBatch`는 같은 segment 안의 fsync를 공유한다. segment를 넘을 때는 먼저 이전 원본을 동기화한 뒤 기존 index/seal을 쓰고, 마지막 segment도 fsync·volume guard를 마쳐야 잠금을 풀거나 참조를 반환한다. 용량 부족·취소 중 완성 prefix도 동기화하고 재시도로 이어간다. 독립 I/O/guard 오류를 단순 capacity 대기로 숨기지 않으며 store fault는 fail closed한다. 기존 파일 형식·checksum·중복/충돌·최초 수신 시각·quota·retention·lease/GC·SQL/RPC 계약을 바꾸지 않는다. 단일 `Append`의 durable 반환도 유지한다. [저장 계약](WINDOW_STORAGE.md), [수신 연결](FEED_RUNTIME.md)을 따른다.

일반 runtime trace region/log를 raw lock/sync/seal/create·수신·capture·writer fresh·projection commit·ACK에 추가했다. trace가 꺼져 있을 때 진단용 시계/SQL/RPC/영구 기록을 추가하지 않는다. 측정 fixture는 기존 공개/capture/ACK 시각을 출력에도 보존하며 입력·부하·동기화 설정은 유지한다. 기준/후보 바이너리 둘 다 같은 계측을 쓴다. `rawAppend`/`receiveNext`는 기존 dataset별 작업에서 배치별 작업으로 분모가 달라지므로 **작업 p99끼리 직접 비교하지 않는다**. 블록별 시각·같은 자료 수의 누적 시간과 실제 sync 횟수를 구분한다.

### 본 측정과 분리한32블록 진단

32 timed+2 warm의102개 같은 원본 자료에서 data fsync가 **102→42회**였다. 세그먼트 seal12회·생성13회는 동일했다. 원본 append 누적은1037.433→780.167ms, data fsync 누적572.519→249.978ms였고 seal 누적은206.984→321.042ms로 증가했다. 블록별 원본 저장 p50/최대는18.681/155.981→7.500/128.244ms, 공개→원본 durable p50/최대는81.785/222.655→62.775/164.282ms였다. 이는 별도 짧은 진단이며128블록 결과를 대신하지 않는다.

같은 trace의 writer fresh p50/p99는159.046/171.816→164.905/184.806ms로, 제한된 RPC의 대기가 남았다. projection commit p99는9.725→42.732ms로 증가했다. PostgreSQL WAL sync delta는151→147회, 누적422.122→408.111ms였다. 이 서버 delta는 warm/setup/assert/cleanup까지 포함하며 블록별 percentile이 아니다. 진단에만 켠 `track_io_timing`/`track_wal_io_timing`은 모두 off로 복원했다. Go1.26 trace 도구의 예전 `-d=1` 옵션 실패는 지원되는 `-d=parsed`로 수정해 재분석했으며 성능 실행 실패가 아니다.

### 같은128블록·3쌍의 최종 비교

Go1.26.8/CGO0 Linux arm64의 기준/후보 바이너리를 미리 빌드했다. 같은 별도 app/PG16 컨테이너·bridge·named DB volume과 fsync-on, raw segment64KiB/최대8 records,2 warm+128 timed, workers2/chunk1/base plan/추가 worker off,2 HTTP API clients/목표 최대20요청/s를 유지했다. streaming은500ms 간격, backlog는1ms 간격 공개다. 각 모드는 `[전,후]→[후,전]→[전,후]`3쌍을 순차 실행했다. 본 측정 중 profiler·SQL timing logging·동시 테스트/빌드는 없었다. frozen baseline/candidate의 심볼과 source/build hash로 순차 원본/묶음 저장 및 prefetch 부재를 확인했다.

아래는 **각 실행 percentile/rate의3회 중앙값**이다. streaming API는 injection, backlog API는 drain cohort다. 다른 cohort·오류·늦은 완료도 원본에 남긴다. ACK는 nominal20ms polling의 관측 상한이며,0.3~2ms 차이를 개선/회귀로 해석하지 않는다. 수신 간격은 warm을 포함한 실행 metric이다.

| 초당2블록·128블록 | 전 | 후 |
|---|---:|---:|
| 공개→capture p50/p95/p99 ms | 136.569 / 244.668 / 305.143 | 139.817 / 238.027 / 240.024 |
| 공개→ACK p50/p95/p99 ms | 340.382 / 444.612 / 500.385 | 342.754 / 440.932 / 500.701 |
| 블록 수신 간격 p99 ms | 583.913 | 579.171 |
| 원본 append 누적 ms / 동일390개 자료 | 1984.438 | 2403.527 |
| 단일 블록 API p99 ms | 6.630 | 9.690 |
| 최근8블록 API p99 ms | 6.357 | 16.084 |
| 최종 블록/s | 1.990 | 1.989 |

| 누적128블록 | 전 | 후 |
|---|---:|---:|
| 공개→capture p50/p95/p99 ms | 12733.528 / 24259.860 / 25254.103 | 12722.931 / 24253.740 / 25249.076 |
| 공개→ACK p50/p95/p99 ms | 12929.430 / 24464.878 / 25458.085 | 12927.841 / 24464.991 / 25460.214 |
| 블록 수신 간격 p99 ms | 108.258 | 53.474 |
| 원본 append 누적 ms / 동일390개 자료 | 529.775 | 525.377 |
| 단일 블록 API p99 ms | 5.940 | 3.295 |
| 최근8블록 API p99 ms | 5.172 | 6.413 |
| 최종 블록/s | 4.961 | 4.961 |

각 streaming 쌍의 capture p99는284.389/791.285/305.143→277.930/240.024/232.336ms, ACK는480.665/1112.280/500.385→585.672/500.701/459.406ms였다. **첫 ACK는 증가했고 중앙값은 사실상 같다.** 원본 누적도2311.465/1984.438/1743.881→2403.527/3460.101/885.869ms로 두 쌍이 증가했다. 짧은 진단의 저장 시간 감소를 모든 본 실행으로 확대하지 않는다.

streaming API 단일 p99는6.630/21.817/4.472→18.522/9.690/5.416ms, 목록은6.357/47.226/5.467→24.112/16.084/6.287ms로 두 쌍이 증가했다. backlog 단일은6.238/5.940/3.736→3.135/3.935/3.295ms로 모두 낮았지만, 목록은33.601/5.172/4.494→9.139/6.413/3.829ms로 두 번째 쌍이 증가했다. 모든 지표/실행의 개선은 아니다.

streaming capture p99 중앙값은약21.3%, backlog 수신 간격 p99는약50.6% 낮았다. 반면 **ACK·최종 처리량은 거의 같고 streaming API tail·원본 누적 중앙값은 증가**했다. 본 실행 사이 host1분 load average가14.17~30.92였고 환경을 완전히 고립하지 못했다. 실행별 편차와 소수3쌍만으로 모든 차이를 코드 단독 효과나 통계적 유의성으로 확정하지 않는다. 채택 근거는 제한된 동일 블록의 durability barrier 횟수 감소와 관측한 수집 지표이며, 전체 tail/SLO 해결 선언이 아니다. streaming 입력 자체가500ms이므로 수신 간격을450ms 성공 기준으로 오해하지 않는다. 실제 Arc peer/외장 mount/24시간 추종은 미검증이다.

12회 모두 raw 작업은390→130회였고 자료 수는390개로 같다.130 capture/sourceValidate/sourceACK·오류0,260 거래/receipt·2080로그·2470 normalized outbox digest가 같았다. **API10807건·오류0**, 단일 SQL1회/목록2회, 공유 물리 RPC5/s envelope·offline API RPC0을 fixture로 확인했다. streaming physical HTTP는315/312/312→307/319/312회, backlog는모두129회였다. 별도 제한기/캐시 fresh는 추가하지 않았다.

최종 관련 race tests/subtests는 반복 포함466회 통과·선택 mount/측정/helper/Rust9 skip이었다. 원본/배치 충돌·재생·segment 경계·capacity prefix/취소·guard fault·fsync 직후 프로세스 종료 복구·기존 stream과 혼용·capture/facts/epoch/fresh·실제128블록 writer/API·재시작/reorg/종료·Arc 모듈을 포함한다. vet와lint0 issues도 통과했다. 이 수치는 제외한 prefetch 후보의 별도 검사를 포함하지 않는다.

근거는 `/private/tmp/indexer-raw-latency-20261017/measurements.json`의 최종12회와 `supplemental-summary.json`, `trace-batch-before/after-analysis.json`, `validation.json`, `comparison-builds.json`·source hash다. 제외 후보는 `prefetch-probe/measurements.json`이다. `compare.py`/`diagnose-batch.py`·baseline/source snapshots·전체 API/block 관측을 보존했다. 소유 label의 컨테이너2개·network·named volume과 비교 바이너리3개187703550bytes를 제거했고 기존10개 컨테이너의 name/status/StartedAt이 같았다. 운영/예제 YAML·Go/의존성·handoff·이전 API query IO/reader와 writer 코드는 초기 hash를 유지한다.

다음은 segment seal/create의 metadata/index I/O와 공유5 HTTP/s 안의 capture/writer fresh admission 공정성이다. 동일128블록·3쌍과 API regression을 계속 확인해야 한다. fresh·generation·삭제/ACK 경계를 생략하거나 더 큰 비내구성 큐로 지연을 감추지 않는다.


## Seal과 다음 번호의 directory fsync 공유 (2026-10-17)

3.67의 블록별 최대3개 원본 묶음 저장은 양쪽에서 동일하게 유지했다. 이 단계는 자동 세그먼트 교체의 metadata I/O를 줄이고 fresh/ACK의 남은 비용을 분리했다. DB 연결4·동일 pool 안의 최소 API 예약1/유휴 연결 차용·공유5 HTTP/s/burst1·capture와 writer 커밋 직전 uncached fresh·same Store/bound transaction·epoch/삭제 fence·SQL 바인딩은 바꾸지 않았다. RPC pool/source/facts/writer/API와 부하 fixture도 초기 hash를 유지한다.

### 교체 순서와 복구 보장

기존 자동 교체는 index·seal·다음 번호를 담은 STORE.json·새 raw entry 각각의 directory fsync로4개 barrier를 거쳤다. **index를 먼저 durable하게 하는 barrier는 유지**한다. 이어 seal과 다음 번호의 파일 내용을 각각 fsync·rename하고 **같은 부모 directory fsync1회**로 둘을 확정한다. 이 barrier가 성공해야 다음 raw 파일을 생성·directory sync하고 원본을 기록한다. 자동 교체마다 directory sync1회를 줄이며 파일 내용 sync를 생략하지 않는다. 다음 번호의 저장 비용이 create에서 seal 구간으로 이동하므로 두 구간을 합쳐 비교한다.

공유 barrier 전에는 seal과 다음 번호 중 어떤 부분만 남아도 기존 마지막 raw tail/이미 확정된 index로 재개할 수 있다. 이때 다음 raw는 아직 존재하지 않는다. barrier 이후 새 raw는 이미 확정한 next 아래의 ID만 쓴다. 사용하지 않은 예약은 건너뛸 수 있고 기존 파일을 덮어쓰지 않는다. 명시적 Seal/Close나 segment/번호 한도에서는 다음 ID를 예약하지 않는다. 기존 파일 형식·checksum/손상 판정·volume pin/guard·quota·완성 prefix·lease/GC/DB ACK 계약은 그대로다. 새 온디스크 intent나 느슨한 자동 복구 규칙은 추가하지 않았다. 구현은 `pkg/window/rotation.go`, `files.go`, `store.go`다.

각 index sync·seal rename·metadata rename·공유 sync·다음 raw 생성 경계에서 자식 프로세스를 종료했다. 공유 sync 전 두 metadata의 가능한 네 생존 조합도 파일 snapshot으로 모델링해 기존 prefix 조회와 다음 append를 검사했다. 이는 실제 장치의 전원 손실 시험을 대신하지 않는다. 다음 raw 생성 실패의 store poison/무참조 반환·재시작 뒤 미사용 ID 건너뛰기, explicit Seal/Close, 기존 batch capacity/중복/충돌·영구 객체·GC 회귀를 확인했다.

### 별도32블록 진단과 fresh 대기

32 timed+2 warm,102개 원본에서 **directory sync61→49회**, raw data sync는42회로 같았다. 자동 seal12회·create13회도 같았다. seal+create 누적336.903→81.737ms, raw append 누적582.999→180.790ms였다. 다만 수정하지 않은 raw data sync 누적도230.506→84.387ms로 달라져 **시간 감소 전체를 directory barrier 공유의 단독 효과로 돌리지 않는다**. 횟수 감소와 시간 변동을 구분한다.

기준 진단의22번 블록은 공개→raw125.715ms, capture148.170ms, 실제 ACK455.098ms였다. 같은 블록의 writer fresh237.250ms·projection commit42.528ms·ACK 기록4.524ms가 후속 지연에 기여했다. fresh span은 DB 확인과 HTTP/RPC 대기를 함께 포함한다. 실행 전체 RPC limiter 대기 p99는188.449→189.021ms로5 HTTP/s의200ms 슬롯에 가까웠다. 이 비용을 캐시나 이른 fresh 검사로 줄이지 않았다. 현재 순차 capture가 매 commit 전에 새 응답을 기다리는 경로에서는 누적 처리량이 약5블록/s 제약에 가까울 수 있다. 이는 모든 구현의 보편적 처리량 상한이라는 주장이 아니다.

서버 WAL sync delta는151→150회, 누적274.654→299.917ms였다. warm/setup/assert/cleanup을 포함한 누적값이며 블록 p99가 아니다. 진단에만 켠 PG IO/WAL timing은 두 설정 모두 off로 복원했다. 본 측정과 profiler·추적 분석·테스트/빌드를 분리했다.

### 동일128블록·모드별3쌍

Go1.26.8/CGO0 Linux arm64의 두 바이너리를 먼저 빌드하고 frozen source/심볼로 구분했다. 같은 별도 Docker app/PG16·bridge·named DB volume·fsync-on, raw segment64KiB/최대8 records,2 warm+128 timed/260거래·receipt/2080로그, workers2/chunk1/base plan/추가 worker off, API2 clients/목표 최대20/s를 유지했다. 각 모드에서 `[전,후]→[후,전]→[전,후]`3쌍을 순차 실행했다. streaming 공개 간격500ms, backlog1ms다. 아래는 **각 실행 percentile/rate의3회 중앙값**이며 API는 streaming injection/backlog drain cohort다. 다른 cohort·오류·늦은 완료도 원본에 남긴다.

rawAppend는 양쪽 모두 같은 블록별 배치130회(390개 자료)라 작업 percentile을 비교할 수 있다. raw/수신 interval은 warm을 포함한 실행 metric이고 공개→capture/ACK는128 timed 블록이다. ACK는 nominal20ms polling의 관측 상한이므로 수~십수ms 차이를 확정적 개선으로 읽지 않는다. 서로 다른 요청/블록의 stage percentile을 더하거나 차감하지 않는다.

| 초당2블록·128블록 | 전 | 후 |
|---|---:|---:|
| 원본 append p50/p95/p99 ms | 6.625 / 87.965 / 117.574 | 6.376 / 73.343 / 101.277 |
| 원본 append 누적 ms | 2615.574 | 2197.110 |
| 공개→capture p50/p95/p99 ms | 137.863 / 235.611 / 247.272 | 124.979 / 223.855 / 237.260 |
| 공개→ACK p50/p95/p99 ms | 340.329 / 458.789 / 459.994 | 338.987 / 439.950 / 458.814 |
| 수신 간격 p99 ms | 562.516 | 580.149 |
| 단일 블록/목록8 API p99 ms | 3.450 / 4.608 | 4.521 / 5.688 |
| 최종 블록/s | 1.990 | 1.990 |

| 누적128블록 | 전 | 후 |
|---|---:|---:|
| 원본 append p50/p95/p99 ms | 0.727 / 15.588 / 99.247 | 0.738 / 7.371 / 28.885 |
| 원본 append 누적 ms | 570.131 | 349.387 |
| 공개→capture p99 ms | 25249.265 | 25251.962 |
| 공개→ACK p50/p95/p99 ms | 12928.871 / 24483.950 / 25478.034 | 12930.981 / 24464.647 / 25461.724 |
| 수신 간격 p99 ms | 115.262 | 79.864 |
| 단일 블록/목록8 API p99 ms | 2.241 / 3.838 | 2.276 / 2.870 |
| 최종 블록/s | 4.961 | 4.961 |

원본 p99 중앙값은 streaming약13.9%·누적약70.9% 낮았다. streaming 각 쌍은117.574/124.311/56.713→117.627/34.631/101.277ms로 **첫·세 번째 쌍은 증가**했다. 누적은96.892/104.397/99.247→68.684/28.885/18.435ms로 모두 낮았다. 원본 누적 중앙값은 streaming약16.0%·누적약38.7% 낮았지만 마지막 streaming 실행에서는1028→2197ms로 늘었다. 실행별 편차를 숨기지 않는다.

streaming capture p99는247.272/256.245/246.608→231.448/237.260/258.616ms, ACK는440.929/460.091/459.994→440.772/458.814/480.078ms로 마지막 쌍이 증가했다. 수신 간격 중앙값도 증가했다. API 단일 p99는2.799/4.310/3.450→4.521/3.669/5.318ms, 목록은4.608/5.453/4.147→5.688/4.868/7.108ms로 **두 쌍과 중앙값이 증가**했다. 누적 API도 두 번째 쌍은 단일2.241→2.987ms·목록2.842→4.661ms로 늘었다.

누적 ACK 각 쌍은25676.988/25478.034/25452.230→25461.724/25451.859/25479.085ms였다. **ACK·최종 처리량은 사실상 같다.** 본 실행 writer fresh p99 중앙값은 streaming186.086→185.387ms·누적184.693→181.954ms였다. 공유 RPC와 직전 fresh를 유지해 남은 대기를 기록했다. streaming physical HTTP는319/299/315→302/312/301회, 누적은130/129/129→129/129/129회였다. 제한기를 별도로 만들거나 검사 결과를 재사용하지 않았다.

본 실행 사이 host1분 load average는9.88~28.10이었다. 소수3쌍/공유 호스트의 결과로 시간 차이 전체를 코드 단독 효과나 통계적 유의성으로 확정하지 않는다. 채택한 범위는 복구 순서를 보존한 barrier 감소와 관측된 원본 저장 지표 개선이다. **API·ACK·450ms/24시간 SLO 해결이나 모든 tail의 개선을 주장하지 않는다.** 실제 Arc peer·외장 mount 운영은 미검증이다.

12회 모두130 capture/sourceValidate/sourceACK·오류0,260 receipt·2080로그·2470 normalized outbox digest가 같았다. API **10812건 오류0**, 단일 SQL1회/목록2회, 물리 RPC5/s envelope·offline API RPC0을 fixture assertion으로 확인했다. 최종 관련 race tests/subtests는 반복 포함348회 통과·선택/helper7skip, vet/lint0 issues였다. 초기 window 회귀82회는 이 최종 수에 중복 포함하지 않았다. 원본·객체·GC·capture/facts·Arc·공유 RPC·128블록 writer/API·앱 restart/reorg/종료/4체인 격리를 검사했다.

최종 근거는 `/private/tmp/indexer-rotation-20261017/measurements.json`, `supplemental-summary.json`, `trace-rotation-before/after-analysis.json`, `validation.json`, build/source hash·baseline overlay·변경 diff다. `compare.py`/`diagnose-batch.py`/`summarize.py`와 전체 요청/블록 관측을 보존했다. 테스트 소유 app/DB2개·network·named volume과 바이너리2개125142322bytes를 정리했고 원래10개 컨테이너의 name/status/StartedAt이 같았다. 보호한 운영/예제 YAML·Go/의존성·handoff·RPC/source/writer/API/fixture hash는 유지했다. 다음은 fresh 내부 DB/admission의 세분화와 API tail 증가를 통제된 IO 부하에서 재현하는 작업이다.

## Fresh DB/RPC 대기와 API tail 분리 (2026-10-17)

3.68의 원본 배치·자동 교체 durability 순서는 양쪽에 그대로 두었다. DB 연결4개·같은 pool 안의 최소 API 예약1개와 유휴 연결 차용·공유 물리 RPC5 HTTP/s/burst1·capture와 writer 커밋 직전 fresh를 유지한다. 이번 제품 변경은 **writer fresh가 반복하던 scope·canonical pointer·manifest 조회를 한 snapshot으로 검증**하는 것이다. API SQL/연결 정책이나 RPC admission 순서는 바꾸지 않았다.

### DB 검증을 한 view로 묶기

`capture.Store.ValidateProjectionBlock`은 먼저 state와 높이별 canonical pointer를 bounded batch로 읽고, 기대한 chain/epoch/plan과 공개 범위를 검사한다. 이어 같은 view에서 해당 immutable identity의 manifest를 읽고 기존 `checkProjection`으로 검증한다. PostgreSQL의 bound writer에서는 같은 Store/transaction의 generation KEY SHARE fence가 commit까지 유지된다. unbound는 기존 RepeatableRead, optional port가 없는 backend는 기존 callback 계약을 사용한다. GetMany 미지원 시 Get-only 경로와 metadata1MiB/합계4MiB 제한·오류 처리를 유지한다. payload 읽기·검증 결과의 호출 간 cache는 추가하지 않았다.

Source는 이 사전 검사 뒤 **매 호출마다 새 uncached canonical RPC**를 보내고 응답 뒤 `current`로 epoch/plan을 다시 확인한다. fresh 위치는 writer commit 직전이며 capture commit의 fresh·디코딩 뒤 검증·재시도·ACK/undo/outbox 경계는 그대로다. bootstrap/미수집·다른 hash·잘못된 manifest·DB I/O 오류를 성공이나0으로 바꾸지 않는다. RPC 도중 unbound Reset이 완료되면 응답 뒤 stale을 거부하고, bound Reset 차단은 기존 transaction fence가 담당한다. SQL 값 바인딩도 그대로다.

### 별도32블록 진단

2 warm+32 timed의 별도 runtime trace에서 bound writer fresh34건 모두 사전 SQL9→3회·응답 후2→2회, 합계 **11→5회**였다. 이 구간의 pool acquire는 양쪽0회로, 이미 writer에 묶인 연결을 다시 얻는 대기가 아니었다.130블록이면 이 검증 경로에서 SQL 왕복780회를 줄인다. 사전 DB phase p50/p99는 **1.731/3.924→0.735/2.570ms**였다.

반면 fresh 전체 p50/p99는165.737/179.766→155.211/176.987ms, 논리 RPC 큐 p50/p99는162.174/176.585→153.244/174.869ms였다. 실제 local HTTP p50은0.399→0.466ms였다. 큐와 공유 limiter 대기가 겹친 구간의 p50은151.737→142.282ms다. **주된 대기는 원격 응답 시간이 아닌 공유 RPC의 큐/예약 구간에 남았다.** 겹침은 token 소유권별 인과 분해가 아니며, phase percentile들을 합산하지 않는다. 이 짧은 진단의 시간 차이를 본 성능 비교의 결과로 쓰지 않는다.

API handler와 GraphQL resolver의 goroutine이 달라 같은 goroutine의 SQL child만 세면0으로 보였다. 요청 context의 기존 진단과 handler 시각을 연결해326/326·330/330건의 admission·SQL 응답/행 소비·나머지 서버 구간을 분리했다. 단일 SQL1회/목록2회는 변하지 않았다. 진단 단일/목록 API p99는7.290/5.532→19.005/24.136ms로 **오히려 증가**했다. admission p99는0.032/0.072→0.140/0.164ms였고 SQL-wire p99는1.662/4.334→5.496/14.874ms였다. 다만 기준의 한 단일 요청에는5.382ms admission 대기도 있어 연결 대기가 전혀 없다고 해석하지 않는다.

같은 요청의 예로 기준 단일 HTTP13.897ms 중 SQL-wire12.317ms·admission0.005ms였고, 후보의 느린 목록은HTTP188.231ms·handler71.571ms·SQL-wire70.732ms였다. 후자의 약116.660ms는 handler 관측 바깥에 있어 HTTP/client/host scheduling 영향 가능성이 있지만 이를 특정 원인으로 확정하지 않는다. 별도 서버 로그의 짧은 `SELECT ... FROM blocks` 집계 p99는0.154→0.218ms였다. 이 집계는 API 요청과 일대일로 연결되지 않았고 다른 blocks 읽기도 포함하므로 API percentile과 차감하지 않는다. 서버 SQL 실행 하나만으로 tail을 설명할 수 없다는 단서로 사용한다.

전체 sched profile에서 queryReader/reservedQueryReader/ContextWatcher를 거친 누적 scheduling delay는445→683ms였고 pgx watcher의 channel rendezvous가 남았다. 이는 요청별 p99가 아니며 모든 DB 작업의 watcher를 교체한 근거가 되지 않는다. 기존 API 전용 취소 watcher 개선은 유지했다. 이번 변경은 fresh의 불필요한 DB 왕복을 줄이는 범위이며 API의 HTTP/SQL-wire/host tail을 해결했다고 표시하지 않는다. trace·SQL duration logging·IO/WAL timing은 별도 진단에만 켰고 원래 값으로 복원했다.

### 동일128블록·모드별3쌍 결과와 한계

기준/후보 양쪽에 같은 opt-in trace 계측을 넣고 후보의 단일-view 검증만 달리했다. Go1.26.8/CGO0 Linux arm64 바이너리를 미리 빌드한 뒤 같은 별도 Docker app/PG16·bridge·named volume·fsync-on에서 실행했다. raw64KiB/최대8 records·블록별3개 묶음과 metadata barrier,2 warm+128 timed, workers2/chunk1/base plan/추가 worker off, API2 clients/목표 최대20요청/s를 유지했다. streaming500ms/backlog1ms 각각 `[전,후]→[후,전]→[전,후]`3쌍이며 본 비교 중 profiler·SQL duration/IO timing·추가 빌드/테스트는 없었다.

아래는 **실행별 percentile/rate의3회 중앙값**이다. API는 streaming injection/backlog drain cohort이며 전체 cohort·요청 기록도 보존한다. raw/수신 간격은 warm 포함 metric, capture/ACK는128 timed다. ACK는 nominal20ms polling의 관측 상한이다.

| 초당2블록·128블록 | 전 | 후 |
|---|---:|---:|
| writer fresh p50/p99 ms | 167.760 / 185.766 | 172.536 / 185.544 |
| 공개→capture p50/p95/p99 ms | 125.332 / 226.496 / 233.728 | 143.686 / 248.632 / 269.861 |
| 공개→ACK p50/p95/p99 ms | 339.966 / 440.362 / 459.421 | 360.055 / 458.767 / 479.407 |
| 단일 블록 API p99 ms | 5.292 | 3.233 |
| 최근8블록 API p99 ms | 6.660 | 4.456 |
| 원본 append p99 / 누적 ms | 79.061 / 1284.523 | 157.230 / 4557.282 |
| 수신 간격 p99 ms | 558.042 | 574.745 |
| 최종 블록/s | 1.990 | 1.990 |

| 누적128블록 | 전 | 후 |
|---|---:|---:|
| writer fresh p50/p99 ms | 136.170 / 185.097 | 135.884 / 184.877 |
| 공개→capture p99 ms | 25256.356 | 25250.722 |
| 공개→ACK p50/p95/p99 ms | 12929.366 / 24483.892 / 25458.680 | 12930.236 / 24467.458 / 25461.021 |
| 단일 블록 API p99 ms | 1.609 | 1.813 |
| 최근8블록 API p99 ms | 2.200 | 2.732 |
| 원본 append p99 / 누적 ms | 27.017 / 412.844 | 16.930 / 385.584 |
| 수신 간격 p99 ms | 37.200 | 17.412 |
| 최종 블록/s | 4.957 | 4.957 |

streaming API p99 중앙값은 단일약38.9%·목록약33.1% 낮았지만, **누적 단일약12.7%·목록약24.2% 증가**했다. streaming 각 쌍의 단일/목록은5.292/6.660,2.741/3.919,9.360/8.511→2.843/4.281,3.233/4.456,3.545/5.737ms로 두 번째 쌍이 증가했다. 누적은7.148/7.779,1.347/2.200,1.609/2.191→1.813/2.042,1.533/2.904,1.979/2.732ms로 두 번째와 세 번째 쌍이 증가했다. 가장 느린 기준 한 번만 고른 개선율로 보고하지 않는다.

streaming 단일/목록 SQL-wire p99 중앙값은2.185/3.816→1.626/2.635ms, admission은0.060/0.087→0.028/0.133ms였다. 누적 단일은 SQL-wire0.691→0.589ms인데 HTTP p99는 증가했고, 목록은 SQL-wire1.596→1.650ms·나머지 서버0.761→1.100ms였다. 별도 phase percentile의 차감으로 요청별 원인을 확정하지 않는다. 요청별 관측과 trace의 한계에 따라 **연결 수 부족만으로 API tail을 설명하지 않고 HTTP/row 소비/runtime 구간을 계속 분리**한다.

fresh p99·누적 ACK·최종 처리량은 사실상 같았다. streaming capture p99는약15.5% 늘었고 원본 append도 세 쌍 모두 증가했다. 이번에 원본 저장 코드는 바꾸지 않았지만 이 회귀를 측정에서 제외하지 않는다. 본 실행 사이 host1분 load average는13.86~57.11이었다. 현재3쌍의 공유 호스트 결과만으로 모든 시간 차이를 코드 단독 효과로 확정하지 않는다. **채택 근거는 같은 view의 검증과 SQL11→5회라는 구조적 비용 감소이며, streaming/누적의 모든 API·capture·ACK tail 개선이나 SLO 완료를 주장하지 않는다.** RPC queue와 HTTP/host scheduling의 남은 원인은 별도다.

12회 모두 raw append130회/390개 자료,130 capture/validate/ACK·오류0,260거래/receipt·2080로그·2470 normalized outbox digest가 같았다. API10812건 오류0·단일 SQL1회/목록2회, 기존 물리5/s envelope·offline API RPC0을 확인했다. streaming physical HTTP는316/297/312→318/313/313회, 누적은양쪽 모두129회씩이었다. 커밋 직전 fresh·응답 뒤 epoch·same Store/transaction·SQL 바인딩·원본/삭제/ACK 경계를 유지했다.

최종 관련 race tests/subtests는 반복 포함337회 통과·선택 측정1skip, vet/lint0 issues였다. 새 preflight의 두 backend·hash/plan/epoch/range/manifest/metadata 한도/IO 오류, 호출마다 새 fresh·RPC 도중 Reset, 기존 bound publication/reset·reorg/재시작/종료/공유 RPC·실제128블록 writer/API와 SQL 바인딩을 검사했다. 기존 설정/Go/의존성/handoff·raw/rotation/pipeline/writer/query IO는 초기 hash와 같았다. API reader 변경은 공통 opt-in trace뿐이다.

근거는 `/private/tmp/indexer-fresh-tail-20261017/measurements.json`, `supplemental-summary.json`, `trace-fresh-before/after-analysis.json`, `validation.json`, build/source hash·전후 source snapshot·`compare.py`/`diagnose-batch.py`/`analyze.py`다. 테스트 소유 app/DB·network·named volume과 바이너리2개125152749bytes를 정리했고 기존10개 컨테이너의 name/status/StartedAt을 보존했다. 다음은 공유5 HTTP/s 안의 capture/writer admission 공정성과 고정된 CPU/IO 조건의 API HTTP·row 소비 tail 재현이다. 실제 Arc peer·외장 mount·24시간 추종은 아직 미검증이다.

## Capture와 writer SQL 종료 대기 감소 (2026-10-17)

3.69의 단일-view fresh 검증을 양쪽에 유지하고 streaming capture와 누적 API tail을 재점검했다. 이전 비교에서 원본 append 시간이 코드 변경 없이 세 streaming 쌍 모두 늘었고, API는 작은 admission 뒤 SQL-wire·행 소비/handler/HTTP 바깥에서 늦어진 요청이 있었다. 이 관측만으로 모든 회귀를 DB 연결 부족이나 특정 코드 변경의 인과로 확정하지 않는다. 같은 로컬 ETH68/raw/capture/writer/API 입력을 사용해 재현했다.

### 변경 범위와 취소 계약

pgx5.7.5의 context watcher는 정상 SQL 종료에도 `Unwatch`가 감시 goroutine과 unbuffered channel로 만나야 한다. 이전 API 개선과 같은 `AfterFunc`/socket deadline 감시를 **이미 소유한 transaction의 Exec/Query/QueryRow**에도 적용했다. `transactionQuery`는 capture callback과 bound writer의 pending flush 이후 SQL 실행에만 사용한다. rows/row 소비가 끝날 때 감시를 종료하고, 취소 callback이 이미 시작됐으면 완료를 기다린 뒤 deadline을 복원한다. 따라서 이전 statement의 취소가 다음 statement/socket을 뒤늦게 건드리지 않는다.

원래 context는 capture scope·취소·fresh 검사에 그대로 사용한다. 드라이버에 넘기는 statement context만 취소 감시를 중복하지 않도록 바꾸며 값/진단은 보존한다. timeout은 원래 context 오류를 함께 돌려준다. BEGIN/COMMIT/ROLLBACK·SendBatch·pool admission과 generation lock, writer commit context는 기존 경로다. capture callback이 오류를 무시해도 기존 context/error poison 검사가 commit을 막는다. 같은 Store/transaction·SQL 값 바인딩·fsync/synchronous_commit·undo/outbox/ACK·커밋 직전 uncached fresh와 응답 뒤 epoch 검사는 그대로다. RPC pool·API SQL·예약1/차용/총4연결·raw/rotation 코드는 이번에 바꾸지 않았다.

### 별도32블록 진단과 남은 대기

2 warm+32 timed의 별도 trace에서 `ContextWatcher/captureWrite/captureView`를 거친 누적 scheduling delay는472.811→277.774ms, 그중 channel send는361.856→157.215ms였다. capture Commit을 거친 누적 delay는61.494→4.480ms였다. 이는 **profile의 누적 scheduling delay**이며 블록별 p99나 모든 시간 감소의 코드 단독 효과가 아니다. BEGIN/COMMIT 등 기존 watcher를 유지한 구간의 대기는 남아 있다.

capture34건은 양쪽 모두 블록당 SQL36회였고 fresh는 pre3/post2·pool acquire0으로 같았다. capture SQL 합산 p50은9.479→8.811ms, fresh 사전 DB p99는2.301→1.187ms였다. 반면 capture SQL 합산 p99는20.685→47.820ms, RPC 뒤 구간 p99는14.925→50.516ms로 증가했다. 후보16번 블록은 capture75.848ms 중 RPC24.046ms·응답 뒤50.516ms였고 SQL span 합계47.820ms였다. 쿼리 종료 대기 감소가 모든 SQL/commit tail을 제거했다고 해석하지 않는다.

같은 진단의 API 단일/목록8 HTTP p99도5.791/6.437→22.493/27.302ms로 증가했다. 후보의 느린 목록 요청은 HTTP28.974ms·handler22.441ms·SQL-wire15.639ms·admission0.003ms였다. 서버의 blocks SELECT 집계 p99는0.200→0.193ms였지만 요청별로 대응한 집계가 아니므로 API percentile과 차감하지 않는다. SQL 실행 하나만으로 HTTP/행 소비/runtime tail을 설명할 수 없다는 단서다.

WAL sync delta는162→146회·누적310.273→320.487ms였다. warm/setup/assert/cleanup도 포함한 서버 delta이며 블록 p99가 아니다. raw data sync42회·directory sync49회는 같고 raw append 누적296.932→191.523ms로 달랐다. 이번에 이 파일 경로는 수정하지 않았으므로 raw 시간 차이를 SQL watcher의 직접 효과로 단정하지 않는다. 진단 설정의 IO/WAL timing과 SQL duration logging은 원래 값으로 복원했다. 성능 본 비교는 trace/SQL logging/추가 빌드·테스트를 모두 종료한 뒤 진행했다.

### 동일128블록·모드별3쌍

Go1.26.8/CGO0 Linux arm64의 수정 전/후 바이너리를 미리 빌드하고 새 transactionQuery 심볼의 부재/존재로 구분했다. 같은 별도 Docker app/PG16·bridge·named volume·fsync-on, raw64KiB/최대8 records/블록별3개 묶음·metadata barrier,2 warm+128 timed, workers2/chunk1/base plan/추가 worker off, API2 clients/목표 최대20요청/s다. DB 연결4·API 예약1/유휴 차용·shared5 HTTP/s/burst1·커밋 직전 fresh·응답 뒤 epoch 검사를 고정했다. streaming500ms/backlog1ms 각각 `[전,후]→[후,전]→[전,후]`3쌍을 순차 실행했다.

아래는 **실행별 percentile/rate의3회 중앙값**이다. API는 streaming injection/backlog drain cohort이며 실패/늦은 완료·다른 cohort도 원본에 남긴다. raw/수신 간격은 warm 포함 metric, 공개→capture/ACK는128 timed다. ACK는 nominal20ms polling의 관측 상한이다. 서로 다른 stage percentile을 합산/차감해 원인을 확정하지 않는다.

| 초당2블록·128블록 | 전 | 후 |
|---|---:|---:|
| 공개→capture p50/p95/p99 ms | 131.966 / 237.906 / 246.489 | 132.086 / 236.217 / 253.436 |
| capture commit p99 ms | 219.107 | 219.677 |
| 공개→ACK p50/p95/p99 ms | 340.376 / 440.510 / 440.739 | 340.253 / 440.296 / 440.736 |
| 단일 블록 API p99 ms | 3.948 | 2.491 |
| 최근8블록 API p99 ms | 7.688 | 3.746 |
| 원본 append p99 / 누적 ms | 108.241 / 1503.238 | 120.965 / 3039.533 |
| 수신 간격 p99 ms | 538.898 | 570.472 |
| writer fresh p99 ms | 181.114 | 180.731 |
| 최종 블록/s | 1.991 | 1.990 |

| 누적128블록 | 전 | 후 |
|---|---:|---:|
| 공개→capture p99 ms | 25247.420 | 25327.374 |
| 공개→ACK p50/p95/p99 ms | 12940.134 / 24675.480 / 25649.222 | 12929.093 / 24742.363 / 25737.093 |
| 단일 블록 API p99 ms | 2.767 | 1.777 |
| 최근8블록 API p99 ms | 3.856 | 3.637 |
| 원본 append p99 / 누적 ms | 28.979 / 510.148 | 15.490 / 401.116 |
| 수신 간격 p99 ms | 82.720 | 24.123 |
| writer fresh p99 ms | 190.092 | 189.511 |
| 최종 블록/s | 4.919 | 4.908 |

streaming API p99 중앙값은 단일약36.9%·목록약51.3%, 누적은약35.8%·5.7% 낮았다. streaming 각 쌍의 단일/목록은12.160/11.453,3.507/4.511,3.948/7.688→4.678/7.456,2.355/3.746,2.491/3.567ms로 **세 쌍 모두 낮았다**. 누적은1.636/2.247,4.669/5.996,2.767/3.856→1.704/3.637,3.672/5.071,1.777/2.552ms로 첫 쌍이 증가했다. API p50 중앙값은 양쪽 모드/두 route 모두 소폭 증가했다. 모든 API 지표/실행의 개선은 아니다.

streaming 단일/목록 SQL-wire p99 중앙값은1.353/3.267→1.154/1.814ms, admission은0.079/0.120→0.076/0.145ms였다. 누적 단일은 SQL-wire1.443→0.933ms·나머지 서버1.057→0.744ms였으나 목록 SQL-wire는1.955→2.598ms로 증가했다. 이 수치와 요청별 원본을 함께 보며, 연결 개수나 서버 SQL 실행 하나로 tail을 설명하지 않는다.

**streaming capture p99 중앙값은약2.8% 증가했다.** 각 쌍은284.669/246.489/230.740→253.436/267.062/242.796ms로 한 쌍 감소·두 쌍 증가였다. ACK는500.826/440.739/440.588→440.424/440.736/441.153ms로 중앙값이 사실상 같았다. capture commit p99도약219ms로 비슷했고, 수정하지 않은 raw p99·누적 시간·수신 간격 중앙값은 증가했다. 이 aggregate 차이들을 차감해 공개→capture의 증가분을 raw/SQL/RPC 중 하나로 확정하지 않는다. **capture tail 개선 완료나450ms/장기 SLO 성공으로 표시하지 않는다.**

누적 ACK는25455/25649/26451→25462/25737/25769ms로 첫·두 번째 쌍이 증가했고 세 번째만 낮았다. 최종 처리량은4.919→4.908블록/s로 비슷했다. 원본 p99는 세 누적 쌍 모두 낮았지만 해당 파일 코드는 초기 hash와 같아 직접 코드 효과로 돌리지 않는다. 본 실행 사이 host1분 load average는6.03~16.69였다. 소수3쌍과 공유 호스트/IO/runtime 편차가 있어 모든 시간 차이의 코드 단독 인과·통계적 유의성을 주장하지 않는다. 채택한 범위는 정상 SQL 종료 rendezvous 제거와 관측된 API tail 감소이며 RPC admission/원본 I/O 대기는 남았다.

12회 모두 raw append130회/390개 자료,130 capture/validate/ACK·오류0,260거래/receipt·2080로그·2470 normalized outbox digest가 같았다. API10836건 오류0·단일 SQL1회/목록2회, shared 물리5/s envelope·offline API RPC0을 fixture로 확인했다. streaming physical HTTP는313/318/315→315/315/316회, 누적은129/130/131→129/130/130회였다. source/fresh·RPC pool·window/rotation·API SQL/예약/연결 정책·설정/Go/의존성/handoff·writer 파일은 초기 hash를 유지했다.

관련 race tests/subtests는 반복 포함729회 통과·선택 측정2skip이었다. 전체 PostgreSQL과 capture/facts/RPC/telemetry, 실제128블록 writer/API·managed feature/독립 worker·reorg/재시작/종료를 포함한다. 새 검사에서는 Exec/Query/QueryRow와 스트리밍 취소, 완료 뒤 취소와 같은 transaction의 다음 statement, EOF/중복 Close·SQL/NoRows·이미 취소된 요청의 SQL0회·context 진단 보존을 확인했다. 기존 API watcher·pool4한도·SQL 값 바인딩·fsync/fresh/generation/ACK 경계도 통과했다. vet와 변경 PostgreSQL 패키지 lint0 issues가 통과했다.

근거는 `/private/tmp/indexer-capture-api-tail-20261017/measurements.json`, `supplemental-summary.json`, `trace-capture-before/after-analysis.json`, `capture-capture-before/after-analysis.json`, `capture-before/after-sched.txt`, `validation.json`, source/build hash와 전후 소스다. 재현/분석 스크립트·원본 요청/블록 관측을 보존한다. 다음은 공유 RPC에서 head/capture/writer의 예약 시점과 원본 fsync를 동일 블록으로 연결해 streaming capture tail을 줄이는 작업이다. 실제 Arc peer·외장 mount·24시간 운영은 미검증이다.

테스트 소유 app/DB·network·named volume과 바이너리2개125158887bytes를 제거했고 기존10개 컨테이너의 name/status/StartedAt을 보존했다. 원본·trace·측정 기록은 남겼다. 운영 데이터나 다른 Docker 자원은 정리 대상에 포함하지 않았다.

## 유지보수 RPC의 fresh 배치 합류 (2026-10-17)

3.70의 transaction SQL 취소 감시를 그대로 유지하고 streaming capture의 원본 fsync와 RPC 예약을 같은 블록으로 연결했다. 이 fixture의 정기 부하는 별도의 `eth_blockNumber` 조회가 아니라 `ControlStep`의 canonical 조상/최종화 사전 검사였다. 유지보수 단독 HTTP가 먼저 나간 뒤 capture fresh가20~65ms 늦게 도착해 다음200ms 슬롯을 기다리는 사례가 있었다. 유지보수 검사나 커밋 fresh를 생략하는 대신 독립 요청의 배치 합류를 개선했다.

### 정책과 유지할 경계

`port.WithRPCMaintenance`는 선택적인 실행 context 힌트다. `ControlStep`의 **트랜잭션 밖** 조상·checkpoint 조회/사전 검증에만 붙인다. 데이터 커밋·writer·최종화 transaction은 원래 context를 사용한다. 같은 caller context에 표시가 새어나가지 않고 finality commit fresh도 foreground인 점을 두 DB에서 검사했다. chain/height/hash/params/ID와 응답 검증을 바꾸지 않으며 hint 자체가 canonicality나 삭제 권한을 주지 않는다.

기존 read coalescer가 켜진 **10 HTTP/s 이하·burst1** pool에서 첫 요청이 유지보수이면 최대500ms 동안 다음 독립 foreground 요청과 합류할 수 있다. foreground가 도착하면 모으기 대기를 즉시 끝낸다. foreground가 먼저 온 경우와 unlimited/높은 rate/burst>1/이미 batch 미지원으로 판정한 pool은 기존10ms 경로다. 개별 caller deadline·취소는 대기 중에도 그대로 적용된다. 이는 유지보수 응답을 늦출 수 있는 모으기 예산이며 foreground의500ms 대기나 전체 RPC latency 상한을 뜻하지 않는다.

같은 dispatcher/channel128·최대64 members·4 in-flight groups를 사용하고 새 worker/대기 큐/limiter는 추가하지 않았다. 추가 유지보수 요청은 원래 expiry를 연장하지 않는다. 인증/URL/header나 member 한도에서 다음 요청을 carry해 FIFO를 유지한다. **모으기가 끝난 뒤** 원래 shared limiter에서 물리 token을 얻는다. token을 먼저 얻고500ms 보류하지 않으므로 오래 들고 있던 token으로 전송을 몰아 보내지 않는다. failover와 batch 거부 뒤 원래 envelope 재시도는 계속 같은 limiter를 쓴다. member 오류/누락/중복 응답과 서로 다른 credential도 기존 검사를 유지한다.

매 capture/writer commit과 재시도의 uncached fresh, 응답 뒤 epoch 검사·동일 Store/transaction·generation fence·SQL 바인딩·undo/outbox/ACK·raw durability/삭제 조건은 그대로다. RPC 메서드 결과를 저장하거나 같은 높이의 새 요청을 이전 응답으로 대체하지 않는다. 별도 노드나 새 transport를 실행하지 않았다.

### 짧은 후보와 별도32블록 진단

최초75ms 후보는 혼합 배치를1→6회로 늘렸지만 만료 직후 fresh가 도착해 약200ms 기다린 사례가 남았다. 이 후보의 capture RPC 큐 p99는179.661→200.508ms였다. 해당 trace·소스·검사는 `grace75-probe/`에 보존했고 최종128블록 비교의 후보로 쓰지 않았다. 최종 정책은 다음 foreground와의 합류 범위를500ms까지 늘리되 foreground 도착 시 즉시 전송 준비로 넘어가도록 했다.

최종2 warm+32 timed trace의 전체 논리 요청은 양쪽99건(foreground82·maintenance17)이었고, **timed 구간의 실제 논리 RPC도80건씩 같았다.** 혼합 dispatch는1→16회, timed physical HTTP는79→64회였다.32 timed capture 중 유지보수와 같은 배치를 쓴 수는1→16이다. capture SQL은 양쪽 블록당36회, writer fresh pre3/post2/acquire0도 유지했다. 새 fresh 응답 없이 처리하거나 원본을 덜 저장해서 얻은 감소가 아니다.

같은 timed 블록들의 capture RPC 큐 p50/p99는 **63.875/179.661→0.031/96.778ms**였다. 유지보수 전송 뒤65ms 이내 도착하고100ms 넘게 기다린 capture 사례는5→0건이었다. 이는 해당 짧은 관측의 분류이며 모든 부하에서 슬롯 경합이 사라졌다는 보장이 아니다. 반면 raw append p99는32.213→122.448ms로 늘어 공개→capture p99는221.096→232.075ms로 증가했다. 공개→capture p50은109.085→65.700ms였다. 서로 다른 percentile을 합산/차감하지 않는다.

블록별로 보면 기준33번은 공개→raw54.743ms·raw append4.777ms 뒤 capture RPC 큐144.091ms를 기다려 공개→capture221.096ms였다. 후보의 가장 늦은25번은 공개→raw220.322ms·raw append122.448ms·RPC 큐0.023ms·capture11.048ms로, 공개→capture232.075ms였다. 이 raw append의 lock은0.00032ms였고 seal81.389ms·create16.687ms·data sync24.061ms가 대부분을 차지했다. directory sync는 seal/create 안에 중첩되므로 별도로 더하지 않는다. **RPC 예약 지연과 segment 교체/디스크 지연을 구분**하고 수정하지 않은 원본 파일 시간 변화를 배치 정책의 직접 효과로 확정하지 않는다.

진단 API 단일/목록8 p99는 injection2.044/4.072→4.974/4.963ms로 증가했고 짧은 drain cohort는1.440/3.065→0.707/1.074ms였다. 이 진단을 본 성능 비교의 개선 수치로 쓰지 않는다. runtime trace·PG IO/WAL timing·SQL duration logging을 별도 실행에만 켰고 원래 설정으로 복원했다. 모든 빌드/회귀/trace 분석을 마친 뒤 본 비교를 시작했다.

### 동일128블록·모드별3쌍 결과

Go1.26.8/CGO0 Linux arm64의 기준/최종 후보를 미리 빌드하고 dispatcher 심볼·build/source hash로 구분했다. 양쪽에 같은 역할 추적을 넣고 유지보수 합류 정책만 달리했다. 같은 별도 Docker app/PG16·bridge·named volume·fsync-on, raw64KiB/최대8 records/블록별3개 묶음·metadata barrier,2 warm+128 timed, workers2/chunk1/base plan/추가 worker off, API2 clients/목표 최대20요청/s를 유지했다. streaming500ms/backlog1ms 각각 `[전,후]→[후,전]→[전,후]`3쌍을 순차 실행했다. 본 비교 중 추가 빌드·테스트·프로파일러·SQL duration/IO timing은 없었다.

아래는 **실행별 percentile/rate의3회 중앙값**이다. API는 streaming injection/backlog drain cohort이며 모든 요청·오류·늦은 완료·다른 cohort도 보존한다. 원본/수신 간격은 warm 포함 metric, 공개→capture/ACK는128 timed다. ACK는 nominal20ms polling의 관측 상한이다.

| 초당2블록·128블록 | 전 | 후 |
|---|---:|---:|
| 공개→capture p50/p95/p99 ms | 136.475 / 239.010 / 259.427 | 76.391 / 130.635 / 213.765 |
| 공개→ACK p50/p95/p99 ms | 340.741 / 460.073 / 480.103 | 299.587 / 341.892 / 419.706 |
| 단일 블록 API p99 ms | 3.051 | 2.692 |
| 최근8블록 API p99 ms | 5.162 | 3.940 |
| 원본 append p99 / 누적 ms | 129.646 / 2669.147 | 103.871 / 1169.402 |
| 수신 간격 p99 ms | 569.321 | 534.290 |
| writer fresh p50/p99 ms | 169.504 / 186.391 | 148.991 / 185.550 |
| 최종 블록/s | 1.989 | 1.990 |

streaming capture p99 중앙값은약17.6%·ACK는약12.6% 낮았다. 각 capture 쌍은252.515/263.731/259.427→218.846/213.765/126.993ms, ACK는460.607/480.103/480.121→420.805/419.706/340.595ms로 **세 쌍 모두 낮았다**. API도 단일3.051/6.320/2.661→2.692/3.433/2.411ms, 목록5.162/7.944/3.636→3.940/4.723/2.898ms로 모두 낮았다. 입력500ms에 묶인 최종 처리량은 비슷하다.

세 streaming 실행 모두 논리 `eth_getBlockByNumber`는320회씩 같았고, physical HTTP는319/302/305→256/256/256회였다. fresh 요청을 덜 보내거나 응답을 재사용하지 않고 기존 유지보수 요청을 합류시켰다. 다만 수정하지 않은 raw 시간도 감소했으므로 종단 지연 감소 전체를 RPC 정책의 단독 효과로 확정하지 않는다.

| 누적128블록 | 전 | 후 |
|---|---:|---:|
| 공개→capture p99 ms | 25246.152 | 25238.831 |
| 공개→ACK p50/p95/p99 ms | 12927.494 / 24467.074 / 25460.508 | 12920.021 / 24451.909 / 25444.814 |
| 단일 블록 API p99 ms | 1.607 | 1.875 |
| 최근8블록 API p99 ms | 2.456 | 3.157 |
| 원본 append p99 / 누적 ms | 17.432 / 386.751 | 18.237 / 355.492 |
| 수신 간격 p99 ms | 34.185 | 26.689 |
| writer fresh p99 ms | 183.216 | 185.352 |
| 최종 블록/s | 4.961 | 4.961 |

누적은 기존에도 token 대기 동안 capture/writer를 합류해 physical HTTP가 양쪽 모두129회씩이었다. ACK·최종 처리량은 사실상 같다. **누적 API p99는 단일약16.7%·목록약28.5% 증가했고 세 쌍 모두 증가했다.** 단일은1.607/1.663/1.465→2.787/1.848/1.875ms, 목록은2.951/2.456/2.295→3.996/3.157/2.626ms였다. streaming 성과로 이 회귀를 덮지 않는다.

누적 API의 admission p99 중앙값은 단일0.0258→0.0174ms·목록0.0662→0.0514ms로 낮았지만 SQL-wire는0.672→1.089ms·1.086→1.260ms로 늘었다. 목록의 나머지 서버 구간도0.965→1.065ms였다. 연결 획득 하나로 회귀를 설명하지 않는다. 현재 관측으로는 wire/행 소비·handler/DB 처리와 RPC에 뒤따르는 작업 시점의 영향을 더 분리해야 하며, 요청별 server SQL과 연결하지 않은 percentile 차감으로 인과를 확정하지 않는다.

본 실행 사이 host1분 load average는3.22~9.22였다. 소수3쌍/공유 호스트·IO 편차가 있어 모든 시간 차이의 코드 단독 인과·통계적 유의성·실제 Arc/450ms/24시간 SLO를 주장하지 않는다. 채택 범위는 같은 fresh 의미의 물리 HTTP 감소와 세 streaming 쌍의 지연 개선이다. 유지보수 응답의 추가 모으기 대기와 누적 API tail 회귀를 운영상 한계로 남긴다.

12회 모두 raw append130회/390개 자료,130 capture/validate/ACK·오류0,260거래/receipt·2080로그·2470 normalized outbox digest가 같았다. API **10808건 오류0**, 단일 SQL1회/목록2회·shared5 HTTP/s/burst1 envelope·offline API RPC0을 확인했다. DB 연결4·API 예약1/차용·커밋 직전 fresh·응답 뒤 epoch·동일 Store/transaction fence를 유지했다. 운영/예제 YAML·Go/의존성/handoff·SQL watcher/reader/writer·window/rotation/pipeline·facts Source의 초기 hash는 그대로다.

최종 관련 race tests/subtests는 반복 포함1728회 통과·선택 측정2skip이었다. 이 중 새 큐 합류/expiry/취소/인증/한도 carry의 단일 CPU200회는1400 tests/subtests다. 호출 deadline을 연장하지 않는 점, 다음 fresh 응답을 재사용하지 않는 점, shared 물리 budget, ControlStep의 힌트가 데이터·finality commit으로 새지 않는 점도 검사했다. capture/facts/Arc/공유 RPC·실제128블록 writer/API·managed feature/독립 worker/reorg/재시작/종료와 vet/lint0 issues를 통과했다.75ms 후보의 검사는 최종 수에 포함하지 않았다.

근거는 `/private/tmp/indexer-rpc-admission-20261017/measurements.json`, `supplemental-summary.json`, `block-stages-admission-before/after.json`, `trace-admission-before/after-analysis.json`, `validation.json`, build/source hash·baseline overlay·전후 소스와 재현 스크립트다. 최초 역할 probe와75ms 후보는 별도 폴더에 보존했다. 테스트 소유 app/DB·network·named volume과 기준/최종/75ms 후보 바이너리3개187742684bytes를 정리했고 기존10개 컨테이너의 name/status/StartedAt을 유지했다.

다음은 **누적 API SQL-wire/행 소비 tail 회귀**와 raw seal/create의 파일 sync 비용이다. 이미 token이 부족한 구간에서 추가 모으기 대기가 필요한지와 API/commit 시점의 겹침을 같은 요청·블록으로 확인한다. 실제 peer·외장 mount·장기 운영은 별도로 남는다.

## 누적 API의 최신 높이·블록 윈도우 통합 조회 (2026-10-17)

3.71의 Streaming 배치 정책을 유지하고 누적 API의 admission·SQL 프로토콜/행 소비·서버 parse/bind/execute·나머지 handler 시간을 분리했다. 최종 변경은 PostgreSQL의 선택적 `port.LatestBlockWindowReader`와 GraphQL의 기본 높이 윈도우 조회다. RPC·capture·writer·fresh·raw 저장 코드는 기준과 같은 파일 해시다.

### 원인 분리와 제외한 후보

연결 획득만으로 이전 API 회귀를 설명할 수 없었다. 별도128블록 진단에서 API handler와 GraphQL resolver의 goroutine을 구분하고, resolver의 admission/SQL task·요청별 QueryMS·backend PID와 시각을 PostgreSQL 로그에 연결했다. 시간·조회 수·PID/SQL 종류 대응이 모호한 요청은 제외했다. 기준520개 API 중489개가 모든 SQL의 parse/bind/execute까지 대응됐고, 최종 후보516개는 모두 대응됐다. 진단 전용 observer overlay에만 PID/시각을 넣었으며 운영 observer·본 비교 바이너리는 바꾸지 않았다. observer에는 SQL 본문/인자를 넣지 않았고 PostgreSQL의 바인딩값 기록도 껐다.

같은 요청 안에서도 원인이 달랐다. 기준의35.340ms 단일 API는 admission0.00146ms·SQL-wire3.109ms였고 해당 SQL의 서버 parse/bind/execute 합은0.284ms였다. 다른 인라인 CTE/busy 후보 진단에서는109.643ms 단일 API의 wire108.781ms 중 서버 parse63.950ms·bind0.092ms·execute0.040ms가 관측됐다. 따라서 큰 wire 시간을 모두 연결 부족·서버 SELECT 실행·순수 네트워크 중 하나로 부르지 않는다. 후자의 goroutine은 query 안에서108.481ms Waiting·0.080ms Runnable이었다. 이 수치는 제외한 후보의 진단이며 최종 성능 수치가 아니다. 호스트/DB 실행 스케줄·프로토콜 왕복·응답 소비와 handler 시간이 함께 달라졌고, 이전 p99 증가 전체의 코드 단독 원인은 확정하지 않는다.

limiter가 이미 바쁘면 유지보수 grace를 줄이는 후보도 제외했다. 바쁜 token만으로 다음 foreground가 다음 슬롯 전에 도착한다고 보장할 수 없었다. 완료된 Streaming probe의 같은320개 논리 조회에서 기준 physical HTTP256회가 후보308/305회로 늘었고 첫 쌍 capture p99도239.661→349.524ms였다. 이 후보의 비교를 중단하고 **3.71의500ms 유지보수 합류 정책을 그대로 복원**했다. 중단 실행·완료된 probe·소스는 `busy-window-probe/`, 앞선 limiter 진단은 `limiter-probe/`에 보존했으며 최종3쌍에 섞지 않았다.

### 최종 구현과 계약

기존 기본 블록 목록은 최신 indexed height를 읽은 뒤 그 높이의 범위를 다시 조회해 두 번의 SQL/연결 admission이 필요했다. PostgreSQL은 `meta.latest_height`와 해당 `blocks` 범위를 **한 statement snapshot**으로 조회한다. `tip AS MATERIALIZED`로 높이 변환을 한 번 계산하고 offset/limit은 서버 매개변수로 바인딩한다. 최신 높이를 따로 cache하거나 RPC로 가져오지 않는다. 일반 높이의 목록은 SQL2→1회이며 단일 블록은 기존1회다. helper의 검증 기대값도 기준2·후보1로 명시했다.

offset/limit은 행 개수가 아니라 **높이 개수**다. 높이 사이의 빈칸을 다른 블록으로 채우지 않고 indexed tip보다 높은 저장 블록은 포함하지 않는다. 본문 순서·timestamp/miner 사후 필터·페이지 정보·지연 totalCount의 기존 동작을 유지한다. 명시적 number 범위와 이 선택적 port가 없는 저장소는 기존 경로다. totalCount는 여전히 선택할 때 별도 조회하므로 응답 전체가 한 snapshot이라는 뜻은 아니다. 비정상 길이의 tip·본문 decode 실패·취소/SQL 오류를 빈 결과로 숨기지 않는다. SQL bigint 경계를 넘는 uint64 높이는 기존 GetBlocks 범위 동작으로 돌아간다.

`s.q(ctx)`를 사용해 같은 Store에 묶인 transaction이 API lease보다 우선한다. 기존 pool4·API 예약1/유휴 capacity 차용·행 종료/취소 감시를 사용하며 새 pool/연결/worker/큐/limiter를 만들지 않았다. fresh의 사전3/사후2 SQL·호출마다 새 canonical RPC·응답 뒤 epoch·커밋/undo/outbox·ACK·raw fsync/회전/삭제 fence는 바꾸지 않았다. 운영/예제 config·의존성·DB schema/migration도 그대로다.

별도 최종 진단의 목록 SQL-wire p50은0.541→0.453ms였고, 대응된 요청마다 서버 parse/bind/execute를 제외한 나머지 시간의 p50은0.301→0.143ms였다. 반면 서버 단계 합 p50은0.238→0.308ms로 늘었다. 한 statement가 서버 계산 비용까지 모두 줄인 것은 아니다. 이 residual에는 프로토콜/응답/행 소비와 단계 사이 스케줄 시간이 포함된다. 서로 다른 percentile의 차감이나 진단 수치를 본 비교의 개선율로 쓰지 않는다.

### 동일128블록·모드별3쌍 결과

같은 Go1.26.8/CGO0 Linux arm64·격리 Docker app/PG16·bridge/named DB volume·fsync-on(동일 세션 synchronous_commit=off), raw64KiB/최대8 records/블록별3개 묶음과 barrier,2 warm+128 timed, workers2/chunk1/base plan/추가 worker off, API2 clients/목표20요청/s, DB4/예약1·shared5 HTTP/s/burst1·커밋 직전 fresh를 고정했다. Streaming500ms/누적1ms 각각 `[전,후]→[후,전]→[전,후]`3쌍, 총12회다. 빌드/회귀/별도 trace와 DB 진단 설정 복원을 끝낸 뒤 본 비교를 실행했고 중간 추가 profiler/테스트/빌드/SQL timing은 없었다.

아래는 **실행별 percentile/rate의3회 중앙값**이다. API는 Streaming injection/누적 drain cohort다. ACK는 nominal20ms polling의 관측 상한이다.

| 누적128블록 | 전 | 후 |
|---|---:|---:|
| 단일 블록 API p99 ms | 2.812 | 1.945 |
| 최근8블록 API p99 ms | 3.682 | 2.648 |
| API p50 단일/목록 ms | 0.711 / 1.085 | 0.693 / 1.084 |
| API admission p99 단일/목록 ms | 0.019 / 0.163 | 0.077 / 0.055 |
| API SQL-wire p99 단일/목록 ms | 1.307 / 1.638 | 0.790 / 1.657 |
| 공개→capture p99 ms | 25245.175 | 25258.711 |
| 공개→ACK p99 ms | 25454.429 | 25458.747 |
| 원본 append p99 ms | 82.544 | 25.740 |
| 최종 블록/s | 4.961 | 4.961 |

누적 API p99 중앙값은 단일약30.8%·목록약28.1% 낮았다. 단일은 쌍별2.812/2.111/6.540→1.830/1.945/5.080ms로 모두 낮았고, 목록은3.682/2.628/7.380→2.648/2.506/9.162ms로 **마지막 쌍은 증가**했다. 목록 SQL-wire p50도0.476→0.522ms로 늘어 왕복 수 감소를 모든 wire percentile 개선으로 표시하지 않는다. 누적 capture/ACK·처리량은 사실상 같다. physical HTTP는129/129/130→129/129/129회로 마지막 기준 실행의 정기 검사 한 번이 더 있었으며, 모든 실행의 data capture/validate/ACK130회는 같다.

| Streaming128블록 | 전 | 후 |
|---|---:|---:|
| 단일 블록 API p99 ms | 3.605 | 4.045 |
| 최근8블록 API p99 ms | 5.424 | 4.713 |
| 공개→capture p50/p95/p99 ms | 67.968 / 125.266 / 190.575 | 83.442 / 179.503 / 206.360 |
| 공개→ACK p99 ms | 380.905 | 420.129 |
| 원본 append p99 / 누적 ms | 77.412 / 891.846 | 135.747 / 3641.981 |
| 수신 간격 p99 ms | 544.751 | 561.767 |
| writer fresh p99 ms | 187.841 | 186.344 |
| 최종 블록/s | 1.992 | 1.991 |

Streaming은 같은320개 논리 조회에서 **6회 모두 physical HTTP256회**로3.71의 배치 이점을 유지했다. RPC 소스/정책의 초기 해시도 같다. 하지만 관측 capture p99는약8.3%·ACK는약10.3% 증가했고 capture는 세 쌍 모두 증가했다(119.877/190.845/190.575→206.360/296.277/203.106ms). API 단일은 중앙값이 증가하고 목록은 감소했다. Streaming의 수치상 지연 개선 유지까지 달성했다고 표시하지 않는다. 변경하지 않은 raw append p99·총시간도 늘었지만 그 차이만으로 원인을 디스크나 API 변경 하나로 확정하지 않는다.

본 실행 사이 host1분 load average는5.81~17.18이었다. 단일 DB 연결 경합이 주원인이라는 가설과 busy-token 단축 정책을 채택하지 않고, 조회 왕복 감소·같은 snapshot·누적 API p99 중앙값 감소를 채택 범위로 삼는다. 공유 호스트·원본 I/O·서버/handler 편차가 커3쌍 전체의 일관된 개선·코드 단독 인과·실제 Arc/450ms/24시간 SLO는 주장하지 않는다.

12회 모두 raw append130회/390자료·capture/validate/ACK130회·오류0,260거래/receipt·2080로그·2470 normalized outbox digest가 같았다. API **10804건 오류0**, 기준 단일1/목록2 SQL·후보 모두1 SQL, shared5 HTTP/s envelope·offline API RPC0을 확인했다. 전체 RPC/capture/facts/Arc·PostgreSQL/API/GraphQL 회귀 이후 최종 경로 race·실제128블록 앱 race·vet/lint0 issues를 다시 통과했다. gap/offset·미공개 높이·SQL 경계·같은 transaction/외부 미커밋 차단·메타/본문 손상·취소·기존 fallback/페이지 결과도 검사했다. 제외한 busy 후보의 반복 테스트를 최종 코드의 새 concurrency 테스트 수로 세지 않는다.

근거는 `/private/tmp/indexer-backlog-api-20261017/measurements.json`, `supplemental-summary.json`, `sql-match-sql-before/after.json`, `api-schedule-sql-before/after.json`, `validation.json`, 전후 소스/해시·공통 진단 overlay·재현 스크립트다. trace의 파싱 출력은 stack을 제외한 event header를 보존하며 원본 trace도 남겼다. 테스트 소유 app/DB·network·named volume과 임시 바이너리7개438077110bytes를 정리했고 기존10개 컨테이너의 name/status/StartedAt은 유지했다.

다음은 **Streaming raw seal/create sync와 API handler/서버 대기의 동시 변화**를 같은 요청·블록으로 연결하는 작업이다. 누적 목록의 마지막 쌍 악화도 함께 추적한다. 원본 삭제나 fresh 생략으로 지연을 줄이지 않고, 실제 peer·외장 mount·장기 운영은 별도 검증으로 남긴다.

## Streaming capture와 회전 메타데이터 준비 (2026-10-17)

3.72의 기본 목록 SQL1회·동일 snapshot 구현과3.71의 유지보수 RPC 합류 정책을 유지하고, raw append·세그먼트 회전·capture의 RPC 큐/SQL을 같은 블록으로 구분했다. 이번 변경은 `pkg/window/files.go`, `rotation.go`, `rotation_test.go`뿐이다. API/DB/RPC/fresh·운영 설정/의존성은 기준 파일 해시와 같다.

### 원인 분리와 변경 범위

별도32 timed+2 warm 진단에서 raw 저장의 긴 지연이 회전에 몰리는 사례를 확인했다. 최초 SQL logging probe의22번 블록은 raw208.185ms 중 seal163.543ms·다음 raw 생성14.631ms·data sync 합28.441ms였다. 같은 블록의 capture RPC 큐는98.014ms였다. 이 probe에는 PostgreSQL timing 기록 비용이 포함되므로 아래의 본 비교와 합치지 않는다.

PG 진단 설정을 복원한 뒤 양쪽32블록을 runtime trace만으로 다시 조사했다. 기준의 가장 늦은 timed capture인20번은 raw8.197ms·capture273.080ms 중 SQL span252.303ms·RPC 큐10.070ms였다. 반면 후보의4번은 raw30.703ms·capture436.337ms 중 RPC 큐189.675ms·RPC 전체328.412ms·SQL96.454ms였다. 따라서 capture tail을 raw 회전 하나로 설명하지 않는다. SQL span에는 프로토콜/응답 소비와 스케줄 대기가 포함되고, 서버 SQL 실행 시간과 같지 않다.

이 최종32블록 진단 자체는 **개선되지 않았다**. raw p99는107.265→249.155ms, capture SQL p99는252.303→309.848ms, RPC 큐 p99는135.764→190.172ms, 공개→capture p99는374.506→552.765ms였다. 서로 다른 블록의 percentile을 차감하지 않는다. trace 비용·실행 시점의 편차도 있으므로 이 수치를128블록 개선율의 근거로 쓰지 않는다. capture SQL36회/블록과 writer fresh 사전3·사후2 SQL/별도 admission0회는 양쪽 같다. 빈 PostgreSQL duration 파일은 이 최종 진단에서 서버 timing을 **수집하지 않았음**을 뜻하며 SQL0회의 증거가 아니다.

회전에서는 독립된 seal 임시 파일과 다음 번호를 담은 `STORE.json.tmp`를 모두 쓴 다음 기존 순서로 각각 **full Sync→Close→rename**한다. 파일시스템이 두 임시 쓰기를 함께 처리할 기회를 주는 변경이며, fsync 횟수를 줄이거나 실제 병합을 보장하는 변경은 아니다. 기존 index 파일·부모 directory barrier가 먼저이고, seal rename→STORE rename→공유 directory fsync가 끝나야 다음 raw를 생성한다. 임시 파일은 authoritative하지 않고 ACK/다음 raw 생성의 근거가 아니다.

같은 Store/volume 잠금 안에서 동시에 열린 임시 metadata 파일은 최대2개다. raw 데이터 sync·파일 형식·quota/reservation·nofollow/Root 경계·prefix 복구·poison·GC/삭제 fence는 유지한다. 명시적 Seal/Close와 capacity/ID 소진 경로도 기존대로다. 새 goroutine/worker/큐·cache·fdatasync·사전 생성 raw·새 DB 연결은 없다. 커밋 직전 새 canonical RPC·응답 뒤 epoch·transaction/generation fence·outbox/undo·서버 파라미터 바인딩도 그대로다.

추가한 `metadataPrepared` 경계와 기존 rename/metadata barrier/next-created 경계에서 복구를 검사했다. metadata rename 전 authoritative STORE가 바뀌지 않고, 기존 barrier 전 raw가 생성되지 않음을 확인했다. metadata 준비 실패·seal rename 실패는 ACK/ref 없이 fail closed하며 재시작 때 임시 파일을 버리고 기존 prefix를 유지한다. 이전 metaRenamed의 두 directory 변경 생존 조합 검사도 유지했다.

### 동일128블록·모드별3쌍 결과

같은 Go1.26.8/CGO0 Linux arm64·Docker app/PG16·bridge/named DB volume·fsync-on, raw64KiB/최대8 records/블록별3개 묶음·같은 barrier,2 warm+128 timed, workers2/chunk1/base plan/추가 worker off, API2 clients/목표20요청/s·DB4/API 예약1과 유휴 차용·shared5 HTTP/s/burst1을 고정했다. 접속 DSN의 synchronous_commit=off도 양쪽 같고 **capture write는 기존 SET LOCAL synchronous_commit=on**을 유지한다. Streaming500ms/누적1ms 각각 `[전,후]→[후,전]→[전,후]`3쌍, 총12회다. 본 비교에는 trace/PG timing/추가 빌드·테스트를 실행하지 않았다.

아래는 **실행별 percentile/rate의3회 중앙값**이며 API cohort는 Streaming injection/누적 drain이다. ACK는 nominal20ms polling으로 관측한 상한이다.

| Streaming128블록 | 전 | 후 |
|---|---:|---:|
| 공개→capture p50/p95/p99 ms | 96.233 / 208.381 / 477.593 | 97.971 / 194.009 / 239.330 |
| 공개→ACK p99 ms | 686.458 | 533.842 |
| 원본 append p99 / 누적 ms | 207.834 / 3334.537 | 109.630 / 3394.926 |
| 수신 간격 p99 ms | 679.690 | 578.415 |
| writer fresh p99 ms | 182.393 | 175.742 |
| 단일 블록 API p99 ms | 10.807 | 13.499 |
| 최근8블록 API p99 ms | 10.272 | 21.682 |
| 최종 블록/s | 1.991 | 1.991 |

Streaming capture p99 중앙값은약49.9%·ACK는약22.2% 낮았다. 그러나 capture 쌍별137.759/911.114/477.593→239.330/247.682/230.243ms로 **첫 쌍은 악화**했다. raw p99도69.485/616.327/207.834→77.281/147.146/109.630ms로 첫 쌍은 증가했다. raw 누적 시간 중앙값과 API p99 중앙값은 증가했으며, 입력 속도에 묶인 최종 처리량은 같다. 모든 실행에서450ms 이내라는 결과가 아니다.

| 누적128블록 | 전 | 후 |
|---|---:|---:|
| 단일 블록 API p99 ms | 14.832 | 16.148 |
| 최근8블록 API p99 ms | 13.569 | 58.024 |
| API admission p99 단일/목록 ms | 0.612 / 0.130 | 0.636 / 0.776 |
| API SQL-wire p99 단일/목록 ms | 5.310 / 4.275 | 7.546 / 10.448 |
| API 나머지 서버 p99 단일/목록 ms | 0.962 / 2.929 | 2.719 / 18.353 |
| 공개→capture p99 ms | 25437.038 | 25290.295 |
| 공개→ACK p99 ms | 25654.151 | 25897.868 |
| 원본 append p99 / 누적 ms | 74.239 / 1526.365 | 85.512 / 1239.423 |
| 최종 블록/s | 4.923 | 4.878 |

누적 목록 API p99는37.305/10.235/13.569→58.024/14.244/67.031ms로 **세 쌍 모두 증가**했다. 단일은 첫 두 쌍 증가·마지막 쌍 소폭 감소다. API SQL1회 구현·예약/차용 정책은 그대로지만 이전 단계의 **수치상 API 개선까지 유지됐다고 표시하지 않는다**. 후보 마지막 쌍의105.625ms 목록 요청은 admission0.0024ms·SQL-wire92.750ms·서버100.156ms였다. 기준 마지막 쌍에는 별도로923.280ms 목록 요청(SQL-wire919.858ms)도 있어 최대값 한 개와 p99의 방향이 같지도 않았다. 새 SQL 연결 부족 하나로 회귀 원인을 확정하지 않는다. 본 비교는 서버 SQL timing을 수집하지 않아 wire를 서버 실행/순수 네트워크로 더 분해할 수 없다.

본 비교 사이 호스트1분 load average는**164.67~243.08**로 이전3.72의5.81~17.18보다 훨씬 높았다. 작업 소유의 테스트·profiler·빌드는 본 비교 중 실행하지 않았지만 공유 호스트 전체를 격리하지는 않았다. 소수3쌍/큰 I/O·실행 스케줄 편차와32블록 진단의 악화까지 있어 약50%의 코드 단독 효과·일관된 tail 개선·실제 Arc/450ms/24시간 SLO를 주장하지 않는다. 구현 범위는 기존 내구성 순서에서 독립 임시 쓰기를 준비하는 것으로 제한하며 **성능 수용 판정은 보류**한다. 다음 성능 검증은 낮고 일정한 호스트 부하의 같은3쌍에서 누적 API 회귀까지 확인해야 한다.

### 정확성·회귀와 정리

12회 모두130 raw append/390자료·130 capture/validate/ACK·오류0,260거래/receipt·2080로그·2470 normalized outbox digest가 같았다. API **10834건 오류0**·단일/목록 모두SQL1회·offline API RPC0·shared5 HTTP/s envelope를 통과했다. Streaming 논리조회는 기준320/320/321·후보320/320/320, physical HTTP256/252/254→256/256/256회다. 누적은 논리282/282/283→283/282/282·물리130/129/130→131/130/131회다. 정기 검사와 도착/합류 시점의 차이가 있어 요청 수가 모든 실행에서 같았다고 표시하지 않으며, data fresh130회는 유지했다. 유지보수 grace를 우회하는 후보를 재적용하지 않았다.

최종 관련 race **407 tests/subtests 통과·선택7skip**, Linux 전체 window **98통과·선택6skip**, 실제128블록 writer/API·capture/facts/RPC/Arc·회전/복구/실패·vet/lint0 issues를 확인했다. 최초 병렬 회귀에서 RPC telemetry 대기시간 하한 assertion과 Arc reorg의5초 DB I/O deadline이 각각 한 번 실패했다. 같은 코드의 두 패키지 전체를 순차 race로 다시 실행해 모두 통과했으며 assertion/timeout을 바꾸지 않았다. 이 최초 실패를 해결된 제품 결함이나 성공 검사 수에 포함하지 않는다. Linux 첫 실행은 상대 YAML fixture 누락으로 실패해 원본 로그를 보존하고 fixture/cwd를 맞춘 뒤 같은 바이너리 전체를 재실행했다. 선택적으로 시작한 CPU1/race200회 검사는 반복 I/O를 중단했으며 **200회 통과로 세지 않는다**.

근거는 `/private/tmp/indexer-stream-capture-20261017/measurements.json`, `supplemental-summary.json`, `api-tail-check.json`, `block-stages-capture-before/after.json`, `trace-capture-before/after-analysis.json`, `validation.json`, `final-audit.json`, 전후 소스/해시·원본 trace·재현 스크립트다. 최초 SQL logging probe는 `sql-logging-probe/`에 분리했다. 보호58파일 중 위 window3파일만 달라졌으며 측정 바이너리의 소스 해시와 최종 코드가 같다. 소유 app/DB·network·named volume과 바이너리3개133632465bytes를 정리했고 **시작 시점 기존12개 컨테이너**의 name/status/StartedAt을 보존했다.

다음은 **부하가 일정한 호스트에서 API 회귀 재현**, 그리고 capture의36회 SQL 왕복에서 immutable row 저장을 묶을 수 있는지 검사하는 것이다. 새 batch도 충돌 검증·바인딩·동일 transaction/fresh/ACK를 보존해야 한다. 실제 peer·외장 mount·장기 운영은 별도로 남는다.

## 누적 API 재검증과 호스트 부하 확인 (2026-10-17)

3.73의 API 회귀를 다시 확인했다. **낮은 부하의128블록·3쌍 검증은 미완료**다. 시작 시12 CPU 호스트의1분 load가약305였고 CPU 여유도 매우 작았다. 다른 작업을 종료하거나 설정을 바꾸지 않았다. 추가 성능 변경에 앞서 같은 바이너리 대조군과 변경 전후 비교를 계획하고 모든 실행을 보존했다. 이번 변경은 측정 도구 `scripts/measure-api-placement.py`뿐이며 Go 실행 코드·DB/API/RPC/fresh/회전·설정/의존성은3.73 그대로다.

### 계획과 중단 결과

이전과 같은 Go1.26.8/CGO0 Linux arm64·Docker app/PG16/fsync-on·동일 raw 배치/barrier·DB4/API 예약1·5 HTTP/s/burst1·commit fresh·2 warm+128 timed·workers2/base plan·API2 clients/목표20rps를 고정했다. 변경 전은3.73 이전 window3파일만 overlay로 바꿔 빌드했고, 현재 작업 파일을 교체하지 않았다. 두 바이너리의 SHA256은3.73 본 비교와 각각 같다.

누적 변경 전후3쌍에 **현재의 동일 바이너리 대조3쌍**을 교차 배치하고 이후 Streaming 변경 전후3쌍을 계획했다(총18회). 같은 바이너리의 양쪽 label은 동일 SHA256이다. 성능값에 따라 실행을 고르거나 재시도하지 않았다.1초 간격 host load도 기록했다. 관찰상 낮은 부하의 기준은1분 load≤24(논리 CPU의2배)·쌍 안의 max/min≤1.25로 사전에 정했다. 이는 스크리닝 기준이며 CPU/메모리/I/O 격리나 통계적 유의성의 보장이 아니다.

실제로는 **6회 시도·5회 검증 통과·기준 코드1회 API timeout 실패** 뒤 중단했다. 측정 중1분 load는137.56~220.67이었고 낮은 부하 조건을 충족한 실행은0회다. 나머지12회는 실행하지 않았으므로3쌍 중앙값이나 Streaming 후속 수치를 만들지 않는다.

아래는 **개별 실행 p99**이며3회 중앙값이 아니다. 두 번째 변경 전후 쌍은 후보→기준 순서였고 기준에 API 오류가 있어 유효한 비교 쌍으로 채택하지 않는다.

| 누적128블록 실행 | 단일 API p99 ms | 최근8블록 API p99 ms | API 오류 |
|---|---:|---:|---:|
| 동일 바이너리 대조1 A | 5.400 | 6.845 | 0 |
| 동일 바이너리 대조1 B | 5.319 | 7.758 | 0 |
| 변경 전후1 기준 | 13.368 | 13.815 | 0 |
| 변경 전후1 후보 | 4.461 | 11.338 | 0 |
| 변경 전후2 후보 | 268.229 | 192.761 | 0 |
| 변경 전후2 기준 — 검증 실패 | 674.423 | 713.624 | timeout1 |

첫 변경 전후 쌍은 API 두 경로 모두 낮았지만 후보의 다음 실행은 크게 늘었고 기준 코드에서도 더 큰 지연과 timeout이 발생했다. 이를3.73 변경의 무해함이나 회귀의 코드 단독 인과로 확정하지 않는다. 동일 바이너리 대조도 한 쌍뿐이므로 환경 편차의 전체 분포나 유의성을 추정하지 않는다.

### 요청별 대기와 표본 한계

누적 API는 경로별 약260개 표본에서 nearest-rank p99를 계산해 대략 세 번째로 느린 요청이 선택됐다. 이전3.73의 목록 마지막 기준/후보는20ms 이상 요청이2/10건이었다. 최대값이 더 큰 기준 실행의 p99가 더 낮았던 것은 최대값 하나가 아니라 느린 표본 수와 순위가 다르기 때문이다. 이 사실만으로 제품의 모든 p99 증가가 환경 문제라고 판단하지 않는다.

이번 후보2의 단일 p99 요청은 전체268.229ms였으나 **같은 요청**의 서버 구간은7.058ms·연결 획득6.280ms·SQL span0.180ms였다. 서버 handler 밖의 HTTP/응답 소비·client 처리/스케줄 구간에도 긴 시간이 있었다. 실패한 기준2의 목록 요청은 전체2074.453ms·서버1866.161ms·획득55.333ms·SQL170.805ms였다. 같은 요청 안에서 SQL/획득 외 서버 구간도 약1640ms였다. SQL span은 프로토콜/행 소비/스케줄을 포함하며 서버 SQL 실행 시간 자체는 아니다. 별도 trace/서버 timing이 없으므로 나머지를 순수 CPU·network·메모리·raw I/O 중 하나로 확정하지 않는다. 이번에는 API의 기존2초 timeout과 검증 조건을 그대로 유지했다.

시도6회 모두130 capture/validate/ACK·260거래/receipt·2080로그·2470 normalized outbox digest·DB4/예약1·단일/목록 SQL1회·물리5 HTTP/s envelope를 유지했다. API3178건 중1건 timeout으로 **API 오류0 검증은 실패**했다. Go 측정 함수의 PASS 출력만으로 외부 검증기의 오류0 조건까지 통과했다고 표시하지 않는다. 추가 Go 회귀 검사를 다시 실행한 단계가 아니며3.73의 이전 검사 수를 이번 성공 수에 넣지 않는다.

### 측정 도구 보완과 다음 조건

`scripts/measure-api-placement.py`는 선택적인 `--max-host-load`를 추가했다. 예를 들어12 CPU 호스트에서 `--max-host-load 24`를 사용할 수 있다. 시작 시 초과하면 **Docker 조회/자원 생성 전**에 종료하고, 각 실행 전후에도 확인한다. 부하 표본은 `host-load.json`, 통과한 부분 실행은 `runs.json`, 원본은 각 log에 보존한다. 실행 도중 종료할 때 기존 소유 라벨 cleanup을 그대로 사용한다. 음수/0/NaN/무한대는 인자 오류로 거부한다. flag를 생략하면 기존 실행을 제한하지 않고 부하 기록만 추가한다. 전후 시점의 부하 확인은 실행 내 모든 순간의 안정성이나 장비 격리를 보장하지 않는다.

AST·help·잘못된 인자4종·실제 높은 부하의 startup 중단을 확인했다. 마지막 검사는 `host-load.json`만 만들고 소유 metadata/Docker 자원을 만들지 않았다. 변경된 도구로 저부하128블록 실험을 완료한 것은 아니다. 별도 저부하 gate가 있는 동일18회 계획도 준비했지만 호스트가 기준 아래로 내려오지 않아 **시작하지 않았고** 임시 바이너리를 제거했다.

근거는 `/private/tmp/indexer-api-recheck-20261017/attempted-runs.json`, `tail-requests.json`, `incomplete-audit.json`, `protocol.json`, `host-load-by-run.json`, `host-summary-after.txt`, `guard-validation.json`, 원본 logs·전후 script/해시다. 미시작 계획은 `/private/tmp/indexer-api-recheck-lowload-20261017/not-started.json`을 따른다. 측정 소유 app/DB/network/named volume과 첫 바이너리2개125168526bytes를 정리했고 시작 시점 기존12개 컨테이너 상태를 보존했다. 미시작 바이너리2개도125168526bytes 정리했다.

다음 조건은 **다른 CPU/메모리 집중 작업이 종료된 안정된 호스트**다. 같은128블록·3쌍/DB4/RPC5/fresh로 API 회귀를 재검증한 뒤 capture SQL 묶음을 평가한다. timeout 연장·호출 한도 증가·fresh 생략·미처리 원본 삭제로 측정을 통과시키지 않는다.3.73의 성능 수용과 실제 peer/외장 mount/장기 SLO는 계속 미완료다.

### 호스트 안정성 재확인 (2026-10-17 22:49~22:54 KST)

사용자 요청으로 호스트를 다시 확인했다. 최초1분 load는23.27이었고 별도 CPU 표본은47.56% idle이었으나, 빌드 완료 후 **22:51:16~22:54:16의180초/5초 간격37개 표본**에서1분 load가27.96~119.23으로 상승했다. 마지막은117.29였다. 기존 기준인 연속60초 load≤24·같은 구간 max/min≤1.25를 충족하지 않았다. 마지막60초는67.30~119.23·비율1.77이었다. 첫 순간의 낮은 load나 CPU idle 표본만으로 안정됐다고 판단하지 않는다.

요청한 누적128블록·3 alternating pairs의 동일 baseline/candidate 바이너리를 준비했으나 **측정0회·Docker 자원 생성0회**로 끝냈다. 두 바이너리 SHA는3.73 비교와 각각 같았으며 모두 제거했다(125168526bytes). Go 실행 코드·API/DB/RPC/fresh·설정/의존성·측정 도구를 포함한 보호58파일은 이번 확인 시작 때와 같은 해시다. DB4·5 HTTP/s·commit fresh 조건을 바꾸거나 새 API 성능 수치를 만들지 않았다. 저부하3쌍 검증은 계속 미완료다.

근거는 `/private/tmp/indexer-backlog-recheck-20261017/preflight.json`, `host-summary-preflight.txt`, `final-audit.json`, 소스/바이너리 해시·미실행6회 비교 스크립트다. 이는 호스트의 안정성 재확인 결과이며 추가 성능 개선이나 API 회귀 해소의 증거가 아니다.

## 호스트 부하 원인과 고아 CI 조회 정리 (2026-10-17)

사용자 요청으로 프로세스 CPU/메모리·부모 관계·실행 파일·작업 디렉터리·입출력 경로, Docker 소유 라벨과 기존 정리 기록을 대조했다. 이번에는 단순 load 재확인에 앞서 indexer 잔여 프로세스와 다른 프로젝트/시스템 작업을 구분했다. Go 실행 코드·측정 도구·설정/의존성의 보호58파일은 시작 해시와 같다.

### 관측된 부하

아래는12 CPU 호스트의 **개별 시점 표본**이다. CPU100%는 한 논리 CPU 수준이며 호스트 전체 사용률이나 load average와 같은 값이 아니다.

| 관측 대상 | CPU 표본 | 소유/확인 내용 |
|---|---:|---|
| macOS `fseventsd` | 약96~99% | 파일 이벤트 처리; 메모리 비중도약9~11%였다. 시스템 프로세스로 유지했다. |
| Finder / WindowServer | 약95% / 45~66% | 파일/UI 처리 활동이 있었다. |
| Unity editor와 하위 프로세스 | 합계약210% | 별도 digital-tennis 작업 디렉터리와 실행 중 부모 프로세스를 확인했다. |
| WBFT native 동기화 | 약39~73% | 별도 consensus 디렉터리의 장기 실행이었다. |
| Go 링크 작업 | 초기 표본약81%, 이후 새 빌드도 관측 | WBFT의 별도 임시 작업 디렉터리/출력 경로를 확인했다. indexer 측정 빌드가 끝난 뒤에도 다른 빌드가 있었다. |
| 게임·브라우저·메타데이터/미디어 분석 | 여러 프로세스에서 지속/변동 | 추가 CPU 활동이 있었다. |

약66.7초 VM 표본에서 압축기가 차지한 메모리는25.89→24.83GiB였고, 처리된 페이지 합으로 압축5.40GiB·해제7.37GiB를 관측했다. 같은 기간 새 swap-out 증가0·swap-in108페이지였다. 메모리 압축/해제 활동은 확인했지만 현재 swap-out 폭증이나 메모리 누수를 확정하지 않는다. 별도 짧은 I/O 표본은 CPU idle37%·disk0 약39.7MB/s/2757tps였다. 이 수치를 디스크 대역폭 포화나 순수 I/O 병목의 증거로 쓰지 않는다.

현재 부하에는 다른 프로젝트의 앱/빌드·동기화와 OS 파일/메타데이터/UI·메모리 작업이 함께 존재했다. 각 작업이 load 급증 전체에 기여한 비율이나 `fseventsd`가 처리하는 파일/볼륨의 원인까지 추적한 것은 아니다. 과거 indexer 파일 쓰기의 OS 처리 영향도 이 표본만으로 분리하지 않는다.

### 정리한 프로세스와 보존한 작업

indexer 디렉터리를 cwd로 둔 **5일 넘은 고아 CI 조회 쉘**을 찾았다(PID76116, 부모1). 이는 과거 Claude job으로,20초마다 오래된 커밋 `7a919e9`의 CI 완료를 기다리고 있었다. 현재 측정에서 누수된 수집/writer 프로세스로 기록하지 않는다. 짧은 SHA 조회는 빈 배열이었으나 전체 SHA 조회에서는 [해당 CI](https://github.com/0xmhha/indexer-go/actions/runs/38160410575)가 이미10/12 KST 성공 완료한 상태였다. 빈 조회로 완료 조건에 도달하지 못하는 반복이었다.

사용자의 불필요한 프로젝트 프로세스 정리 범위에서 PID·시작 시각·명령 hash·하위 실행 종류를 재확인한 뒤 **쉘76116과 하위 `sleep 20`39614에 SIGTERM**을 보냈다. 둘 다 종료됐고 남은 active PID는0이었다. 이 조회 작업은 CPU가 거의0%였으므로 정리만으로 큰 호스트 부하 감소를 기대하지 않는다. 다른 프로젝트의 WBFT/Unity/게임·브라우저와 시스템 daemon은 종료하지 않았다.

실행 파일/인자와 대상 쉘의 cwd 조사에서 추가 indexer 수집/writer/test 실행 파일은 발견되지 않았다. 과거 측정 소유 기록9개와 전체 Docker 목록을 대조한 결과 해당 소유 컨테이너도0이었다. 기존12개 컨테이너의 name/state/StartedAt은 최종 확인까지 같았다. 이번 Docker 호출은 조회만 했고 다른 작업의 컨테이너를 정리하지 않았다.

### 동일 조건 재측정 여부

기존과 같은 변경 전후 바이너리 SHA로 누적128 timed+2 warm·3 alternating pairs를 준비했다. DB4/API 예약1·shared5 HTTP/s/burst1·커밋 직전 fresh·기존 API SQL1회·raw/barrier는 유지했다. 빌드와 부하 조사를 마친 뒤23:05:02~23:08:02 KST의180초/37표본으로 안정성을 확인했다.

이 구간1분 load는**73.75~196.89·마지막188.58**이었다. 기존 연속60초≤24/비율≤1.25 조건을 충족하지 않아 **비교0회·측정 Docker 자원 생성0회**로 끝냈다. 임시 바이너리2개125168526bytes는 제거했다. 새 API 지연/개선율이나 추가 Go 회귀 검사 성공 수를 만들지 않는다. DB 연결/RPC 한도/fresh 조건을 완화하지 않았다.

근거는 `/private/tmp/indexer-host-audit-20261017/final-audit.json`, `process-cleanup.json`, `external-ownership.json`, `memory-summary.json`, `docker-stats.jsonl`, `stale-ci-full-sha.json`, `measurement/preflight.json`·`measurement/final-audit.json`이다. 원본 process argv 표본은 비공개 임시 폴더/파일 권한으로 보존했다. 다음 재측정 조건은 **별도 CPU/메모리 집중 작업 종료 후 안정된 호스트**이며 API3쌍 검증과3.73 성능 수용은 계속 미완료다.

### 다른 프로젝트 유지 후 안정성 재확인 (23:12~23:15 KST)

다른 프로젝트를 유지하라는 후속 요청에 따라 종료/수정 명령 없이 부하만 다시 확인했다. 약181초/5초 간격37표본에서1분 load는184.75~293.78·마지막293.78이었다. 연속60초≤24/비율≤1.25 조건을 충족하지 않아 동일128블록·3쌍 비교를 시작하지 않았다. 이번에는 측정 바이너리 빌드0·측정0·Docker 자원 생성0회로 추가 부하를 최소화했다. DB4·RPC5 HTTP/s·commit fresh를 포함한 보호58 source/config/deps 파일은 같은 해시이며 새 API 성능 수치나 검사 성공 수는 없다. 근거는 `/private/tmp/indexer-backlog-stability-20261017/preflight.json`과 `final-audit.json`이다.

## 누적 API 비교 준비와 최종 호스트 안정성 확인 (2026-10-18)

다른 프로젝트를 유지한 채 다시 확인했다. 시작1분 load는24.74였고 이후 낮아져, 비교 바이너리 준비 후00:09:43 KST에 **마지막60초14.04~16.96·비율1.208**로 기존 안정 기준을 한 번 통과했다. 두 바이너리는3.73 변경 전후와 각각 같은 SHA다. 빌드와 겹친 최초 관찰은 별도 `preflight-build-overlap.json`에 보존하고 최종 안정성 근거로 사용하지 않았다.

소유 라벨의 app/PG16·network/named volume을 준비하고 fsync-on·진단 timing/logging off를 확인했다. 그러나 첫 실행 시작 guard에서 load가35로 올라 **블록을 실행하기 전에 중단**했다. 이 시점의 프로토콜과 로그는 `before-final-screen-protocol.json`/`before-final-screen-compare.log`로 보존했다. API 지연에 따른 결과 선택/재시도가 아니며 생성된 블록 비교 결과는0개다.

준비를 모두 마친 뒤00:11:36~00:14:36 KST에180초/37표본으로 다시 관찰했다.1분 load는**30.15~82.39·마지막42.67**이었고 마지막60초 max/min도1.93이었다. 연속60초≤24/비율≤1.25를 충족하지 않아 본128블록·3 alternating pairs를 실행하지 않았다. 준비 작업의 잔여 OS 처리와 외부 작업의 기여도를 이 관찰만으로 분리하지 않는다. 한때 통과한 screen을 이후 안정 상태까지 보장하는 것으로 해석하지 않는다.

DB4/API 예약1·shared5 HTTP/s/burst1·커밋 직전 새 fresh·기존 API SQL1회·raw/barrier를 바꾸지 않았다. 보호58 source/config/deps/측정 도구 해시와 작업 branch도 같다. 이번에는 app 비교 바이너리만 빌드했으며 새 Go 회귀 검사 성공 수나 API p99/개선율을 만들지 않는다.3쌍 재검증과3.73 성능 수용은 계속 미완료다.

소유 app/DB/network/named volume과 바이너리2개125168526bytes를 정리했고 기존12개 컨테이너의 name/state/StartedAt은 같았다. 다른 프로젝트에 종료/수정 명령을 내리지 않았다. 근거는 `/private/tmp/indexer-backlog-stability-20261018/final-audit.json`, 전후 screen JSON, `comparison-builds.json`, 준비된 프로토콜·guard 로그와 `cleanup.json`이다.5분까지 관찰을 연장하는 보조 스크립트는 작성했지만 실행하지 않았다.

## 모든 측정 준비 후 호스트 부하 재확인 (2026-10-18)

이번에는 바이너리 두 개의 빌드·기존3.73 비교 SHA 일치 확인, 소유 app/PG16·network/named volume 생성, 컨테이너 내부 복사와 DB 설정 확인을 **모두 끝낸 뒤** 안정성 관찰을 시작했다. 준비 완료는00:21:11 KST, 관찰은00:21:22~00:24:22의180.26초/37표본이다. 관찰 중 빌드·추가 DB 준비·benchmark는 실행하지 않았다.

12 CPU 호스트의1분 load는**35.97~145.08·마지막132.45**였다. 마지막60초 max/min은1.142로 변동 기준1.25 이내였지만 부하 상한24를 계속 넘었다. 연속60초의 두 조건을 함께 충족하지 않아 **128+2warm·3 alternating pairs는0회**다. 새 API percentile이나 성능 개선율·Go 회귀 검사 성공 수를 만들지 않는다. 이번 관찰만으로 다른 프로젝트와 준비 작업의 잔여 OS 처리 기여도를 분리하지 않는다.

DB4/API 예약1·shared5 HTTP/s/burst1·커밋 직전 uncached fresh와 응답 후 epoch/transaction fence·API SQL1·raw/barrier는 그대로다. 보호58 source/config/deps/측정 도구 해시와 `refactor/arc-node-feed`도 같다. 소유 app/DB/network/named volume과 바이너리2개125168526bytes를 제거했고 기존12개 컨테이너의 name/state/StartedAt은 모두 같았다. 다른 프로젝트에 종료/수정 명령을 내리지 않았다.

근거는 `/private/tmp/indexer-prepared-recheck-20261018`의 `all-preparation-confirmed-finished.json`, `prepared-builds.json`, `postgres-settings.json`, `preflight.json`, `planned-protocol.json`, `not-started.json`, `cleanup.json`, `final-audit.json`이다. API3쌍 재검증과3.73 성능 수용은 안정된 호스트에서 계속해야 한다.

사용자의 후속 요청으로00:31:19 KST에 빌드·동일 SHA·소유 Docker 환경·복사·PG 설정 확인을 다시 모두 마친 뒤00:31:23~00:34:23에180.24초/37표본을 관찰했다. 이번1분 load는**257.92~351.68·마지막257.92**, 마지막60초 비율1.276으로 두 기준 모두 미달했다. **128블록·3쌍 실행0회**이며 새 API 수치나 Go 회귀 검사 성공 수는 없다. 다른 프로젝트에 종료/수정 명령을 내리지 않았고 기존12개 컨테이너 name/state/StartedAt·보호58파일·branch·DB4/예약1·RPC5/burst1·commit fresh·API SQL1·raw/barrier를 유지했다. 소유 app/DB/network/volume과 바이너리2개125168526bytes를 정리했다. 이번 근거는 `/private/tmp/indexer-prepared-recheck-20261018-0030/final-audit.json` 및 같은 폴더의 준비·screen·계획·정리 JSON이다. 이전 관찰/결과와 별도로 보존했다.

## 기존 API 로그와 코드의 원인 검토·수정 제안 (2026-10-18)

사용자 요청에 따라 **새 측정·빌드·테스트·호스트 관찰·Docker/DB/RPC 조작 없이** 기존 자료와 코드를 검토했다. 아래는 원인 분석과 제안이며 Go 구현·설정·의존성은 수정하지 않았다. DB 연결4/API 예약1·RPC5 HTTP/s/burst1·커밋 직전 새 canonical RPC와 post-RPC epoch/transaction 검사를 유지한다. 다른 프로젝트에 작업 명령을 내리지 않았다.

### 확인된 사실

3.73 원본12회/10,834 API 요청과3.74 재확인6회/3,178 요청을 재분석하고, 원본 log18개의 JSON과 저장된 요청 배열이 같은지 확인했다.3.73은 API 오류0,3.74는 성공3,177/timeout1이다. 모두 요청당 SQL1회였다.3.73 누적 API의 실행별 p99 중앙값은 단일14.832→16.148ms·목록13.569→58.024ms이며 목록은 세 쌍 모두 증가했다. 이 관측을 취소하거나 ‘회귀 없음’으로 바꾸지 않는다.3.74는 유효한3쌍을 완료하지 못했다.

아래는 서로 다른 percentile을 차감한 값이 아니라 **각 실행의 실제 p99 순위 요청 하나**를 분해한 것이다. timeout은 p99 요청과 별도다. 단위는ms다.

| 자료/요청 | 전체 | DB 입장 | SQL 왕복/행 소비 | 나머지 handler | handler 밖 잔여 |
|---|---:|---:|---:|---:|---:|
| 3.73 후보1 목록8 p99 | 58.024 | 3.019 | 12.207 | 0.474 | 42.325 |
| 3.73 후보3 목록8 p99 | 67.031 | 0.002 | 3.162 | 1.162 | 62.706 |
| 3.74 후보2 단일 p99 | 268.229 | 6.280 | 0.180 | 0.599 | 261.170 |
| 3.74 후보2 목록8 p99 | 192.761 | 130.959 | 27.490 | 33.698 | 0.613 |
| 3.74 기준2 목록8 timeout | 2074.453 | 55.333 | 170.805 | 1640.023 | 208.291 |

따라서 연결 입장·SQL 왕복·handler의 미분리 구간·handler 밖 잔여가 서로 다른 요청에서 길어졌다. DB 연결 부족 또는 SQL 실행 하나가 모든 tail의 원인이라는 설명은 맞지 않는다. 표본당 약257~277요청의 p99는 세 번째로 느린 요청이며 최악1건만으로 p99 증가를 설명하지 않는다.

시간의 의미는 `pkg/app/mixed_pool_measure_test.go`와 `pkg/storage/postgres/observe.go`로 확인했다.

- `AllMS`: 클라이언트 요청 JSON 생성부터 HTTP·본문 읽기·JSON 해석·fixture 블록/거래 수 검증까지다. 순수 HTTP 왕복만을 재지 않는다. `httptest.NewServer`와 그 client가 같은 Go 프로세스에서 loopback으로 통신한다. Docker 실행에서도 API client/server 사이에 외부 호스트나 Docker bridge는 없고, DB 접속은 별도 app/DB bridge를 사용한다.
- `ServerMS`: 테스트 wrapper가 production router를 호출한 뒤 반환할 때까지다. middleware·요청 읽기·파싱/validation·resolver·모델 decode·응답 인코딩/Write·스케줄링 대기가 포함된다. HTTP 서버가 handler를 부르기 전, 응답의 최종 전달과 클라이언트 작업은 별도다.
- `AcquireMS`: 예약 슬롯 선택과 일반 pool 차용/재시도·필요한 재접속을 포함한 admission 합계다. pool 대기만 또는1ms timer만을 뜻하지 않는다. 기존 로그에는 pinned/borrowed 분기나 probe 횟수가 없어130.959ms의 내부 원인은 더 나눌 수 없다.
- `QueryMS`: pgx QueryStart~QueryEnd, 행 소비 종료까지의 시간이다. 프로토콜·DB 처리·응답 대기·행 읽기/스케줄링을 포함하며 PostgreSQL의 순수 실행 시간과 다르다.
- `전체−ServerMS`와 `ServerMS−AcquireMS−QueryMS`는 잔여 항목이다. 현재 표에서 음수는 없지만, 완료/취소가 겹치는 일반 사례에서는 절대 시각/interval을 맞춰야 하며 잔여값을 순수 네트워크/CPU 또는 특정 함수 비용으로 부르지 않는다. 별도 SQL/서버 span의 p99끼리 차감하지 않는다.

API의 두 resolver는 DB-only이며 추가 RPC를 하지 않는다. fixture의 offline API RPC0 assertion도 기존 실행에서 통과했다. 최신 목록은 `GetLatestBlockWindow` 한 SQL statement snapshot을 쓰고, 단일은 `GetBlock` 한 쿼리다. 일반 SELECT이며 raw window lock이나 writer의 fresh 함수를 API가 직접 호출하는 경로는 없다. 수집/commit이 나머지 pool·DB/CPU/I/O 자원에 주는 간접 경합까지 배제하는 뜻은 아니다. `q(ctx)`는 같은 Store의 bound transaction을 먼저 쓰고, API 예약1개도 총4개 안에 있다. QueryRow Scan/Rows 종료 후 모델 decode가 진행하므로 ‘모델/JSON decode 내내 DB 연결을 잡는다’는 가정으로 반납 시점을 앞당기는 수정은 필요하지 않다.

코드에서 실제 중복/추가 작업도 확인했다. `handler.go`의 `checkLimits`는 body를 복사·요청을 해석하고 `limits.go`에서 AST를 파싱한다. 이어 의존성 graphql-go/handler0.2.4가 요청을 다시 해석하고 graphql-go/graphql0.8.1 `Do`가 AST를 다시 파싱한 뒤 표준 validation을 한다. 기본 depth/complexity 제한이 활성화된 이번 fixture에도 해당한다. 또 `Pretty: true`여서 JSON은 `MarshalIndent`로 만들어진다. 한편 거래 map과 totalCount는 이미 lazy이며 이번 요청은 이를 추가 실행하지 않는다. 조회 모델은 선택 필드3개여도 full block/transactions를 decode하지만, 이번 fixture는2 tx/block이므로 큰 블록 최적화의 이득을 이 결과로 확정할 수 없다.

별도 과거3.72의 PID/시각 대응 자료도 client SQL span이 DB 실행과 같지 않음을 보인다. 예를 들어 단일35.340ms 요청은 admission0.00146ms·SQL span3.109ms·그 SQL의 server parse/bind/execute 합0.284ms였다. 이는3.73/3.74의 느린 요청을 소급해 진단한 자료가 아니므로 그 요청의 원인으로 일반화하지 않는다.

### 미확인 가설과 배제할 단정

1. 공유 호스트/VM의 CPU 스케줄링·메모리 압축·I/O 편차가 큰 tail에 기여했을 가능성은 있다. 하지만 해당 요청의 CPU/GC/VM/DB 대기 원인과 대응된 자료가 없어서 각 기여도와 단독 원인은 미확인이다. macOS load 평균만으로 DB I/O 병목이나 코드 무결성을 확정하지 않는다.
2. 3.73의 두 metadata 임시 파일 준비 시점이 API와 자원 경합을 바꿨을 가능성은 있다. 변경은 `pkg/window`3파일이고 API/DB/RPC 구현은 양쪽 같지만, 이는 간접 성능 영향을 배제하지 않는다. 원본128블록 비교에는 API 요청과 정확히 연결된 회전/fsync event가 없고, 별도32블록 진단은 같은 표본이 아니다. 변경이 회귀의 원인인지와 rollback 시 이득은 미확정이다.
3. 중복 파싱·full block decode·GraphQL 실행의 goroutine 전환·JSON 인코딩이 handler tail에 기여할 수 있다. 코드에 작업이 존재하는 것은 확인했지만 각 단계 시간이 없으므로1640ms를 파싱이나 GC 하나에 귀속할 수 없다. HTTP client/server가 공유하는 Go 스케줄러와 클라이언트 검증도 handler 밖 잔여의 후보다.
4. API admission1ms loop의 반환 통지 지연은 기존3.65에서 이미 재현됐으나, 두 wake 후보는 실제128블록 API p99가 악화해 제외했다. 동일한 wake 수정을 새로운 해결책으로 재적용하지 않는다. 이번130.959ms admission이 같은 이유라는 근거도 없다.

### 먼저 공유하는 수정안 — 아직 미구현

1. **진단/실패 판정의 구분을 바로잡는다.** 기존 `AllMS`를 보존하되 클라이언트 HTTP 구간과 fixture 응답 검증을 별도로 기록하도록 설계한다. opt-in으로 handler의 요청/제한검사·표준 validation/실행·모델 decode·인코딩/Write 구간, admission의 pinned/borrowed와 재시도 횟수를 구분한다. 쿼리/인자/비밀 값은 기록하지 않으며 SQL/RPC/연결을 추가하지 않는다. fixture는 현재 `invalid_data`만 직접 실패시키므로 API timeout이 있어도 Go PASS가 찍힐 수 있고 외부 `verify`가 이를 거부한다. JSON/실패 원본을 먼저 보존한 뒤 모든 API 오류를 최종 실패로 처리하는 흐름을 제안한다. 이 안을 설명하는 것과 새 측정을 실행하는 것은 다르며 이번에는 실행하지 않는다.
2. **GraphQL 요청 내부의 중복 파싱을 없앤다.** 한 번 읽고 만든 request options/AST를 제한 검사와 표준 validation/실행이 함께 사용하게 한다. schema validation·depth/complexity·variables·operationName·fragment/alias·기존 요청 형식/오류/취소·extension 계약을 유지한다. global AST/result cache나 latest 캐시는 추가하지 않는다. 범위는 `pkg/api/graphql/handler.go`/`limits.go`와 해당 기능 검사이며, 대형 의존성 교체/DB 변경 없이 단계를 나눈다. 불필요 작업 감소는 예상할 수 있으나 수백ms tail 해결이나 p99 개선율은 약속하지 않는다.
3. **JSON 꾸밈 출력은 별도 작은 변경으로 분리한다.** API JSON은 compact하게 만들고 playground는 유지하는 안이다. JSON 데이터/오류 의미는 같아야 하며, 응답 문자열 형식에 의존하는 소비자가 있는지는 먼저 확인한다. 이 역시 대형 tail 해결의 증거는 아니다.

첫 수정 범위에 연결 추가·새 pool·API/수집 프로세스 분리·fresh 생략/선행 캐시·RPC 한도/timeout 완화·raw fsync 감소·실험에서 제외된 wake/token 정책을 넣지 않는다. full block을 요약 projection으로 바꾸는 DB/schema/port 확장도 현재 원인 근거 없이 먼저 하지 않는다. 새로운 측정은 사용자 요청 없이 실행하지 않는다.

재분석 결과와 기존 자료 SHA/요청별 정확한 수치는 `/private/tmp/indexer-api-static-review-20261018/request-review.json`에 남겼다. 기존18개 raw log를 다시 읽어 대조한 것이며18개를 새로 실행한 것이 아니다. 별도 분기 `refactor/arc-node-feed`와 읽은 Go/측정 소스15개·기존 보호58파일의 해시를 확인했다. 이번 변경은 분석/작업 문서뿐이다.

## API 시간 구분과 오류 판정 구현 (2026-10-18)

3.78의1번만 구현했다. 추가 성능 측정·호스트 관찰·Docker/실제 DB/RPC 조작은 하지 않았다. 요청 내부의 중복 파싱과 Pretty JSON은 아직 그대로이며 성능 개선율도 만들지 않는다. DB4/API 예약1·shared5 HTTP/s/burst1·커밋 직전 새 canonical과 post-RPC epoch/transaction fence·raw/barrier·설정/의존성은 유지했다.

- 클라이언트: 기존 전체 `MS`/`AllMS`를 보존하고 `Client.PrepareMS`, `HTTPMS`, `ValidationMS`를 분리했다. HTTP는 `Client.Do`부터 본문 읽기까지이며, validation은 JSON/fixture 검증이다. 시작하지 않은 단계는 flag로 구분하고 해당 histogram에0을 넣지 않는다. 본문 읽기의 timeout도 timeout으로 판정한다.
- 서버: 신뢰된 request context의 `graphql.ObserveRequest`로만 `limits`, `request_prepare`, `parse`, `validation`, `execution`, `response`를 관측한다. 일반 요청은 기존 handler/schema를 사용해 추가 extension callback/clock이 없고, HTTP header만으로 켤 수 없다. 관측용 schema에만 graphql-go의 기존 extension API를 연결했으며 요청 해석/검증/실행/응답은 기존 handler에 맡긴다. 쿼리·인자·결과는 진단에 넣지 않고 API 응답에도 진단을 노출하지 않는다.
- DB: 별도 `postgres.ObserveReads`가 켜진 context에만 `block_decode`와 admission의 `pinned`/`borrowed`/`unavailable`, 일반 pool 입장 시도 수·실패·경과를 남긴다. SQL을 재시도하는 횟수가 아니다. 기존 `ObserveQueries`의 acquire/query 계약은 유지했다. 예약/차용·1ms 입장·50ms 오류 backoff·취소/반납 정책과 SQL 문자열/바인딩은 같다.
- 미완료/잔여: `HandlerComplete`와 서버 시작/끝을 기록한다. handler 미완료 표본은 서버/SQL histogram에서 빼고 `IncompleteDiagnostics`로 센다. 서버 interval이 클라이언트 interval 안에 있을 때만 `OutsideHandlerMS`를 기록하며, 겹치거나 불명확하면 null이다. 음수 서버 잔여도 histogram에 넣지 않고 별도 센다. GraphQL execution은 DB와 decode를 포함하는 중첩 단계이므로 이 값들을 더하지 않는다. 늦은 취소 callback이 저장된 snapshot을 바꾸지 않도록 map은 복사한다.
- 실패 보존: 측정 API의 timeout/HTTP/GraphQL/data/body/transport 오류와 handler 미완료를 `apiValidation`으로 판정한다. 종료 직후 시작돼 집계에서 제외된 요청도 `apiExcludedObservations`에 남기고 전체 요청의 verdict에 포함한다. 결과 JSON을 먼저 출력한 다음 오류가 있으면 Go fixture를 실패시킨다. startup/다른 invariant 실패 등 JSON 생성 전의 실패에는 완성된 결과가 없을 수 있다.
- 도구: `measured`는 subprocess가 실패해도 원본 log에서 JSON을 추출한다. `MeasurementError`와 `<run>.failed.json`에 유효하지 않은 결과·사유를 보존하며 성공 집계에는 넣지 않는다. Go PASS가 있어도 API 오류·미완료·실패 verdict면 거부한다. 구형 성공 log도 지원한다. JSON이 없거나 손상/복수이면 원본 log와 실패 사유를 남긴다.

새 기능 검사는 실제 DB/노드/HTTP server를 사용하지 않는다. GraphQL은 mock storage+recorder, client는 in-memory transport, admission은 취소/종료 경로를 사용했다. 정상/구문/표준 검증/제한 거절의 응답 동일성, header/body timeout과 오류 분류, 미완료/겹친 interval/음수 잔여, 실패 verdict와 raw/JSON 보존을 확인했다. 대상3 Go 패키지 기능 검사·최종 GOMAXPROCS1/race 검사와 Python4 검사가 통과했다. race 대상에는 늦은 callback과 저장 snapshot의 경쟁 검사도 포함했다. 결과·범위 감사는 `/private/tmp/indexer-api-diagnostics-20261018/final-audit.json`에 기록했다. 실제 DB의 예약/차용 성공 경로 검사는 assertion만 보강했으며 이번에 DB 통합 실행은 하지 않았다.128블록·3쌍이나 운영 SLO 검증을 이번 검사로 대체하지 않는다. 계측을 켠 요청에는 관측 비용이 있으므로 예전 결과와의 차이를 성능 개선/회귀로 해석하지 않는다.

## GraphQL 요청의 단일 파싱 구현 (2026-10-18)

사용자 요청으로3.78의2번을 구현했다. HTTP 요청 옵션을 pinned adapter의 `NewRequestOptions`로 한 번 해석하고, `request.go`에서 만든 **요청별 AST 한 개**를 깊이·복잡도 검사와 `graphql.ValidateDocument`·`graphql.Execute`가 함께 사용한다. AST/result/latest cache나 의존성 교체는 없다. REST의 고정 문서 `Execute`/`ExecuteQuery`·독립 `Validate` helper는 그대로다.3번 compact JSON은 구현하지 않아 기존 들여쓰기 JSON을 유지한다.

`QueryCost`의 계산·상한·변수 처리와 제한 오류 code/HTTP200 JSON을 유지했다. 표준 검증을 통과해야 resolver를 실행하며 operationName/변수 coercion/fragment/alias와 원래 request context를 표준 executor에 넘긴다. 실행 전/중 취소와 context 값 전달을 channel 기반 기능 검사로 확인했다. 기존 adapter와 GET/JSON·charset/raw GraphQL body/form·문자열 variables·URL 우선순위·잘못된 문서/변수·빈 요청·HTML/`raw` 협상의 응답 status/header/body를 비교했다. 제한 활성/비활성 각20종, 총40개 호환 비교이며 성능 측정이 아니다.

본문은 복사해 다시 재생하지 않는다. 제한 활성 상태에서 본문 읽기 오류가 HTTP400이던 계약은 read wrapper가 보존하며, URL 옵션을 우선할 때도 사용하지 않은 본문의 오류를 확인한다. 브라우저 HTML을 만들기 위해 기존 실행 adapter를 다시 호출하면 쿼리를 다시 파싱/실행하므로, pinned handler0.2.4의 Playground renderer만 MIT 고지와 함께 분리했다. UI template·endpoint escaping·협상과 `/playground` helper는 유지했고 HTML도 호환 비교에 포함했다.

3.79의 opt-in 진단은 명시적인 단계 callback으로 연결했다. 순서는 `request_prepare → parse → limits(활성 시) → validation → execution → response`다. 구문 오류는 parse 뒤, 제한 초과는 limits 뒤 응답하며 실행하지 않는다. 이제 `limits`는 AST를 만드는 시간이 아닌 비용 검사 시간만 뜻한다. 일반 요청은 추가 진단 clock/callback이 없고, API/오류 판정·서버 interval·DB 입장 진단의 값 구조는 유지한다. 이전 phase별 시간과의 직접적인 성능 비교는 하지 않는다.

기능 검사에서 순환 fragment를 넣자 graphql-go0.8.1의 `overlappingFieldsCanBeMergedRule.collectConflictsBetweenFieldsAndFragment`가 무한 재귀해 stack overflow로 종료되는 문제가 드러났다. 실패 원본을 `functional-tests.log`에 보존했다. fragment 정의가 있는 문서에만 같은 AST로 라이브러리의 공식 `NoFragmentCyclesRule`을 먼저 적용한다. 순환이면 표준 GraphQL 오류로 거절하고, 순환이 없으면 **모든 표준 검증 규칙을 그대로 실행**한다. 자기/상호 순환과 제한 비활성 경우도 검사하며 standard validation을 생략하거나 panic을 성공으로 처리하지 않는다.

첫 검사는 공유 Go cache의 누락 파일과 std package 조회 오류로 시작 단계에서 실패했다. 표준 소스 파일이 있는 것을 확인하고 공유 cache/다른 프로젝트의 Go 환경을 삭제·수정하지 않은 채 작업 전용 `GOCACHE`로 전환했다. 분리 과정의 `json.Number` import 누락도 바로잡았고 각 실패 로그를 보존했다. 최종 GraphQL·REST 기능 검사는 전용 cache에서 통과했다. 전체 race 실행은 기존 `TestKeepAlive/answering_client_stays`의 연결 유지 assertion에서 실패했다. 이 검사는200ms pong deadline/180ms ping 주기를 쓰는 별도 WebSocket 경로로, 이번 HTTP handler를 사용하지 않으며 관련 소스는 변경하지 않았다. 데이터 경쟁 경고는 없었지만 실패 원인은 확정하지 않았고 전체 race 통과로 표기하지 않는다. 변경한 GraphQL HTTP/제한/비용/진단/취소 경로의 별도 race 검사와 REST race 검사는 통과했다. 원본 실패/성공 로그·범위 감사·작업 전용 cache 정리 결과는 `/private/tmp/indexer-graphql-single-parse-20261018/final-audit.json`에 기록했다. 공유 cache는 정리하지 않았다. DB 접속 변수를 비우고 mock·기능 검사 내부의 로컬 서버만 사용했다.

추가 성능 측정·호스트 부하 관찰·Docker/실제 DB/공개 RPC 조작은0이다. 다른 프로젝트·DB4/API 예약1·5 HTTP/s/burst1·커밋 직전 새 canonical과 응답 후 epoch/transaction fence·SQL 바인딩·raw/barrier·설정/Go 의존성은 유지했다.130개 보호 소스 중 의도한 GraphQL4파일 이외 해시가 같으며, 새 request/renderer/검사 파일 외에 수집/writer/DB 정책은 수정하지 않았다. 처리 속도·p99·운영 SLO의 개선은 아직 측정/확정하지 않았다.

## GraphQL JSON 꾸밈 출력 제거 (2026-10-18)

사용자 요청으로3.78의3번을 구현했다. GraphQL HTTP 결과의 `json.MarshalIndent`를 `json.Marshal`로 바꿨다. 문자열 안의 공백/escape·숫자·null·배열 순서와 오류 message/locations/path/extensions의 의미, HTTP status/Content-Type을 유지한다. 기존 제한 오류는 이미 들여쓰기 없는 JSON이므로 해당 encoder/마지막 개행은 그대로다. 본문 읽기 실패의 HTTP400 응답도 변경하지 않았다.

기존 Pretty adapter의 JSON에 `json.Compact`를 적용한 결과와 실제 응답을 비교하도록 호환 검사를 갱신했다. 제한 on/off 각21종으로 정상/구문·표준 검증·변수 오류·operationName/fragment/alias·정상 데이터와 resolver 오류가 함께 있는 응답을 포함한다. HTML은 공백 제거 없이 원본과 byte 단위로 비교한다. Playground renderer와 독립 `/playground` helper, REST 고정 문서 실행·응답 코드는 변경하지 않았다. 단일 파싱·깊이/복잡도·전체 표준 검증/fragment cycle guard·원래 context와 취소 처리·opt-in 관측도 변경하지 않았다.

추가 성능 측정·호스트 부하 관찰·Docker/실제 DB/공개 RPC 조작은 하지 않았다. 다른 프로젝트·DB4/API 예약1·RPC5 HTTP/s/burst1·커밋 직전 fresh와 응답 후 epoch/transaction fence·SQL 바인딩·raw/barrier·설정/의존성은 유지한다. 응답 문자열의 꾸밈 공백은 의도적으로 달라지며, 처리 속도/p99/SLO 개선을 기능 검사로 확정하지 않는다.

GraphQL·REST 두 패키지 기능 검사를1회 실행해 모두 통과했다. JSON/HTML 호환42개 비교, 기존 제한·표준 검증·순환 fragment·실행 전/중 취소·context·본문 오류와 Playground 검사를 포함하며 성능 부하는 실행하지 않았다. 이번에는 동시성 구현을 바꾸지 않아 별도 race 실행을 추가하지 않았다.3.80의 이전 전체 race keepalive 실패 원인은 여전히 미확정이다.135개 보호 파일 중 이번에 바꾼 것은 `request.go`·`request_test.go`뿐이고, production 변경은 인코딩 함수 한 줄이다. 결과와 범위 근거는 `/private/tmp/indexer-graphql-compact-json-20261018/final-audit.json`·`functional-tests.jsonl`·전후 diff/해시에 보존했다. 작업 전용 임시 Go cache만 정리하고 공유 cache는 유지했다.

## API 변경1~3 종합 검토와 남은 우선순위 (2026-10-18)

이번3.82는 기존 코드·검사 원본·분석 자료만 읽은 종합 검토다. 새 빌드/기능·race 검사/성능 측정/호스트 관찰/Docker/실제 DB/공개 RPC 조작은0이다. 실행 코드는 수정하지 않았다.1~3은 구현과 각 대상 기능 검사까지 완료됐으며, 수백ms API tail의 주원인·성능 회귀 해소·개선율은 여전히 미확정이다. 기능 검사 통과와 p99 개선을 구분한다.

| 변경 | 현재 코드와 기존 검사로 확인한 내용 | 남는 한계 |
|---|---|---|
| 1 시간 구분·오류 판정 | client 준비/HTTP/검증, GraphQL 단계, DB 입장 경로·probe/모델 decode를 opt-in으로 구분한다. 완료/interval·null 잔여·snapshot 복사와 모든 API 오류/미완료의 실패·원본 보존이 있다. 대상3 Go 패키지 기능/race와 Python4 검사 원본이 있다. | 실제 DB의 진단 성공 경로 통합 실행과 새 단계별 부하 자료는 없다. |
| 2 단일 파싱 | 옵션/AST를 요청별 한 번 만들고 기존 QueryCost·표준 validation·동일 context의 cancellable executor에 공유한다. 순환 fragment는 공식 cycle rule로 먼저 거절한다. 제한/변수/operationName/fragment/alias/취소·HTML 호환 검사가 있다. | 전체 GraphQL/REST race 실행은 기존 별도 WebSocket keepalive assertion 실패. 변경 HTTP 경로/REST race는 통과했지만 전체 race 통과는 아니다. |
| 3 compact JSON | 정상·오류·부분 data+error JSON에서 꾸밈 공백만 제거한다. Playground HTML은 원본과 동일하다. 제한 on/off42개 호환 비교 및 최종 GraphQL·REST 기능 검사가 통과했다. | 처리 속도/지연 개선은 측정하지 않았다. |

2/3 이후에도1의 진단 context API와 오류 verdict는 연결돼 있다. 실행 단계는 DB/모델 decode를 포함하는 중첩 구간이므로 따로 더하지 않는다.2 이후 `limits`는 비용 검사만 뜻하므로 이전의 요청 읽기/첫 파싱까지 포함한 값과 같은 항목명만 보고 직접 비교하면 안 된다. 오래된 전체 시간에는 client 준비/검증도 들어가고 SQL span은 DB 순수 실행 시간이 아니다. 이번 정적 검토 범위에서1~3 사이의 기능 계약 충돌이나 추가 확정 결함은 찾지 못했으나 전체 운영/통합 검증 완료를 뜻하지 않는다.

**지연에 대해 확인된 사실:** 기존3.73/3.74의18개 실행 자료는 각각10,834 API/오류0·3,178 API/timeout1이며 요청당 SQL1회다.3.73 목록 p99의 실행별 중앙값13.569→58.024ms 증가는 기존 관측으로 남는다.3.74는 유효한3쌍을 완료하지 못했다. 같은 요청을 분해하면 단일268.229ms 중 handler7.058ms·밖 잔여261.170ms였고, 다른 목록192.761ms는 DB 입장130.959ms·SQL 왕복/행 소비27.490ms였다. timeout1건은 전체2074.453ms 중 기존 계측으로 분리되지 않은 handler 구간이1640.023ms였다. 서로 다른 요청의 p99를 차감한 값이 아니다. API 두 조회는 DB-only이며 writer의 fresh/RPC를 직접 실행하지 않는다. 수집/writer와 CPU·DB·I/O를 공유하는 간접 영향은 배제하지 않는다.

**미확인 가설:** 공유 호스트/VM 스케줄링·GC·압축·I/O가 tail에 기여한 비율,3.73 metadata 파일 준비 변경의 인과,full block decode/GraphQL/JSON 비용의 실제 비중,130.959ms admission의 pinned/borrowed/재접속 내역은 미확인이다. handler 밖 잔여를 순수 네트워크로,SQL span을 순수 SQL 실행으로 단정하지 않는다. 중복 파싱/꾸밈 작업은 코드에서 확인해 제거했지만 큰 지연의 주범이었다는 증거는 없다. 새 단계는 과거 log에 없어 소급 분해할 수 없으며 새 부하 자료도 없다.

API 관련 남은 작업은 다음 순서다.

1. **기존 keepalive race 실패 원인 분석.**200ms pong deadline/180ms ping 간격의 시간 조건과 종료/읽기 경로를 기존 코드·실패 log로 먼저 확인한다. HTTP 성능 원인과 별도로 관리하고, 근거 없이 timeout을 늘려 전체 통과로 바꾸지 않는다. 정상/무응답/비활성 연결의 계약을 모두 유지하는 수정·회귀 검사가 완료 기준이다. 이번에는 실행하거나 수정하지 않았다.
2. **1~3의 실제 DB 경로 통합 기능 검증.** 총4개/예약1개에서 pinned·borrowed 성공,취소/반납,SQL1회와 decode/phase,미완료·오류의 실패 보존을 함께 확인하는 범위를 먼저 고정한다. 기존 mock/개별 검사로 대체할 수 없는 공백이다. 처리량/p99 부하는 별도이며 현재 미실행이다.
3. **성능 인과 검증은 보류.** 사용자 별도 재개 요청이 있을 때 새 진단이 포함된 자료로 긴 요청 하나의 구간과 실행/오류를 함께 판정한다. 비교 가능한 전체/HTTP 경계와 변경된 phase 정의를 명시하고,반복 호스트 안정성 확인을 자동 재개하지 않는다. 지금은 새 수치/회귀 해소를 선언하지 않는다.
4. **추가 최적화는 원인이 확인된 구간에 한정.** 큰 블록의 선택 필드용 decode/projection이나 SQL/입장 개선은 후순위 후보이며 현재 확정 수정안이 아니다. 기존 부하에서 악화해 제외한 wake 후보는 재적용하지 않는다. 연결 증가·RPC 한도/커밋 fresh 완화·raw 내구성 저하는 우선순위에 넣지 않는다.

DB4/API 예약1·공유 RPC5 HTTP/s/burst1·커밋 직전 fresh canonical과 post-RPC epoch/transaction fence·SQL 바인딩·raw/barrier·설정/의존성·다른 프로젝트는 그대로다. 기존3.79·3.80·3.81 audit/검사 원본과3.78 `request-review.json`을 근거로 검토했으며, 최종 코드/분석 범위 기록은 `/private/tmp/indexer-api-integrated-review-20261018/final-audit.json`이다. 이번 변경은 이 종합 기록과 작업 상태 문서뿐이다.

## WebSocket keepalive 검사 안정화 (2026-10-18)

사용자 요청으로3.82 우선순위1을 분석·보완했다. 기존3.80 실패는 `TestKeepAlive/answering_client_stays`의 연결 생존 assertion이며 DATA RACE 경고는 없었다. 당시 테스트는 `zap.NewNop`을 사용하고 `alive`가 종료 오류를 bool로 버렸으므로 그 실행의 실제 소켓 오류/정확한 스케줄링 원인은 복구할 수 없다. 과거 특정 실행의 원인을 확정했다고 표시하지 않는다.

코드로 확인한 취약 조건은 테스트의200ms read 만료·180ms ping 간격이다. 양쪽 goroutine/전송/pong 수락에 남는 여유가20ms뿐인데, 클라이언트 ping callback3회 계수 뒤400ms sleep하고 살아 있기를 기대했다. callback 계수는 서버가 pong을 읽어 deadline을 갱신했다는 증거가 아니다. gorilla/websocket1.5.3 `SetReadDeadline` 계약상 한 번 read timeout이 나면 연결을 복구할 수 없다. 이 테스트는 정상 응답 정책 검증을 짧은 wall-clock 스케줄링 조건과 섞고 있었다.20ms 여유를 넘긴 스케줄링/응답 지연으로 실패할 수 있다는 경로는 확인했지만, 과거 해당 실행이 정확히 어떤 이유로 그 조건을 밟았는지는 미확인이다.

수정은 `keepalive_test.go`뿐이다. 운영의60초 만료/54초 ping,200ms 테스트 만료 값,read/write/종료 코드는 바꾸지 않았다. HTTP hijack이 끝난 뒤 테스트 transport를 감싸 HTTP 자체의 임시 deadline 처리를 방해하지 않는다.

- **pong 갱신:** 실제 server ticker·WebSocket ping/pong·readPump를 사용한다. 이 경우만 OS 만료 타이머를 분리하고 서버의 초기 deadline 및 pong3회 뒤 deadline 갱신을 직접 관찰한다. client callback 수만 보거나 sleep으로 생존을 추정하지 않는다. 실제 GraphQL protocol ping/pong도 확인한다. 이는200ms 실시간 생존 성능/SLO 시험이 아니다.
- **무응답 만료:** OS에 실제200ms deadline을 적용한 연결에서 server read 오류가 `net.Error.Timeout()`인지,관찰한 만료 시점 전에는 닫히지 않았는지와 client 종료를 확인한다. ping 도착 횟수는 이 검사에서 스케줄링 조건으로 삼지 않는다.
- **늦은 pong:** 실제 소켓의 read deadline을 이미 지난 시점으로 명시적으로 설정해 read timeout을 확인하고,pong을 나중에 보내도 연결이 닫히며 deadline 갱신이 없는 것을 확인한다. 고정 sleep 없이 만료 경로를 재현한다.
- **비활성:** keepalive가 꺼지면 idle read deadline이 설치되지 않고 제어 ping이 없으며 protocol ping/pong이 동작한다. client reader 종료도 join하고 기존 package goleak 검사를 유지한다.10초 watchdog은 검사 hang 방지이며 운영 TTL이나 응답 성능 기준이 아니다.

GOMAXPROCS1/race에서 keepalive10회·하위40경우를 통과했고, 이어 GOMAXPROCS2/race로 GraphQL·REST 두 패키지를1회 실행해 모두 통과했다. 데이터 경쟁 경고와 goleak 실패는 없었다. 현 코드의 해당 패키지 race 실패는 재발하지 않았으나 과거 실행의 정확한 종료 원인 규명이나 repository 전체 race 완료를 뜻하지 않는다. 새 성능 측정/호스트 관찰/Docker/실제 DB/공개 RPC 조작은0이다. DB4/예약1·RPC5 HTTP/s/burst1·커밋 직전 fresh/post-RPC epoch/transaction fence·raw/SQL/설정/의존성과 다른 프로젝트는 유지했다.

원본 실패 log는 이전 위치에 유지하고 새 분석/검사/전후 diff·141개 보호 파일 해시는 `/private/tmp/indexer-keepalive-functional-20261018`에 보존했다. 보호 파일 중 테스트1개만 변경됐으며 production은 모두 같은 해시다. 공유 Go cache 오류를 피한 전용 `go-cache/`는 후속 기능 검사에서 재빌드 낭비를 줄이도록 재사용 가능하게 보존하고 공유 cache는 정리하지 않았다. 다음 API 관련 작업은 실제 DB 경로의1~3 통합 기능 검증이며,성능 인과 검증/반복 host screen은 보류 상태다.

## 실제 DB에서 API 변경1~3 통합 기능 검증 (2026-10-18)

사용자 요청으로3.82의 실제 DB 통합 검증 공백을 검사했다. 기존 PostgreSQL16-alpine 이미지로 라벨/ID가 있는 작업 전용 container·named volume만 만들고,각 Store의 pool을4개/API 예약1개로 고정했다. 기존12개 container는 중지/수정하지 않았다. 부하 도착 루프·처리량/p99 집계·host screen·실제 node/공개 RPC는 실행하지 않았다. 한 블록/두 거래의 로컬 데이터와 유한한 HTTP 요청8개로 기능 계약을 확인했다. 진단 clock은 단계 존재/오류/경로 검사용이며 시간 크기나 성능 기준을 판정하지 않는다.

- **예약·차용:** 실제 production router와 GraphQL 경로에서 단일 블록·목록이 예약 연결로 동작하고,예약 연결을 점유한 동안 두 번째 HTTP 조회가 같은 pool의 spare를 차용한다. 각 정상 조회의 SQL은1회,admission/decode callback은 각1회이며 GraphQL 준비/파싱/제한/검증/실행/응답·client HTTP/검증 단계가 있다. JSON은 compact이며 DB의 hash/높이/거래 수와 일치한다.
- **취소·반납:**4개 모두 점유한 admission에서 실제 probe가 발생한 뒤 취소하면 unavailable/failed·SQL0·decode0과 활성 참조 반납을 확인했다. 별도 SQL 검사에서는 같은4개 pool 안의 control transaction이 blocks table을 잠그고 PostgreSQL의 `pg_blocking_pids`로 SELECT 대기를 확인한 뒤 취소했다. 오류는 `context.Canceled`로 보존되고,입장 성공/SQL 취소가 구분되며 다음 읽기가 같은4개 한도에서 복구된다. 기존 rows Close 중복·lease 공유/종료·차용 rows 종료 대기·bound transaction 우선/다른 Store 격리·미커밋 데이터 비노출 검사도 실제 DB에서 통과했다.
- **HTTP 취소·복구:** 예약 입장 callback에서 대기 중인 HTTP 요청을 취소하면 transport_error로 판정한다(명시적 취소를 timeout으로 바꾸지 않는다). 나머지3개 연결을 background view가 보유한 상태에서 후속 HTTP 요청이 반환된 예약 연결로 정상 조회한다. snapshot/늦은 callback도 race 검사에 포함된다.
- **오류·제한:** 없는 블록의 DB 조회는 SQL1 뒤 GraphQL 오류이며,표준 validation·깊이 제한 거절은 SQL/admission0이다.8개 중 의도한 transport_error1·graphql_error3은 전체 verdict를 실패로 만든다. handler 미완료는0이다. 실제 이 verdict에 PASS 문자열을 붙인 parser 기능 입력도 `api_errors`/`api_validation_failed`로 거절하며 원본 log와 `.failed.json`을 보존하는 것을 확인했다. 이는 합성 parser 입력 검증이며 성능 실행이 아니다.

기능 검사를 공통 경로로 실행하도록 test-only HTTP 진단 wrapper를 `mixed_pool_diagnostics_test.go`에 추출했고 기존 opt-in 측정 fixture도 그 helper를 호출한다. functional 전용 read barrier는 dispatch 전에 설치되며 기존 측정 fixture에서는 nil이다. `mixed_pool_measure_test.go`의 부하/수치 계산·RPC/fresh 정책은 바꾸거나 실행하지 않았다. 새 `mixed_pool_diagnostics_postgres_test.go`와 `query_reader_diagnostics_test.go`에 통합/취소 검사를 추가했다. production 코드에서 수정이 필요한 결함은 이번 범위에서 발견하지 못했다.

첫 실행의 저장소/API 검사는 통과했지만 새 app fixture의 첫 블록이1거래인데2거래 검증을 기대해 invalid_data로 실패했다. BuildLoad는 첫 블록에 별도 배포 거래를 만든다는 것을 확인하고 테스트를 명시적2거래 블록으로 고쳤다. validator/허용 기준을 낮추지 않았고 초기 실패 원본 `functional-race.jsonl`을 유지했다. 이후 app 대상 검사는 모두 통과했다. 최종 소스 기준 저장소/API/app 대상10개 top-level 기능 검사를 GOMAXPROCS1/race에서 통과했고 Python 오류 판정4검사와 실제 DB verdict parser 거절도 통과했다. 전체 repository 회귀/race·push 전200회·성능/SLO 검증을 실행한 것은 아니다.

검사 종료 뒤 소유 container/volume을 ID·라벨로 확인해 제거하고 stale DSN 파일도 삭제했다. 기존12개 container의 ID/status/StartedAt은 같았다.141개 기존 보호 파일 중 변경은 test helper2개뿐이고 production·설정/의존성·DB4/API 예약1·RPC5 HTTP/s/burst1·커밋 직전 fresh/post-RPC epoch/transaction fence·SQL 바인딩·raw/barrier·다른 프로젝트는 유지했다. 원본·소스 diff/해시·DB 준비/정리·대상 검사·parser 증거는 `/private/tmp/indexer-db-api-integration-20261018/final-audit.json`에 기록했다. 전용 build cache는 기존 작업 위치를 재사용·보존하고 공유 cache는 정리하지 않았다.1~3의 이 DB 통합 기능 공백은 검사 범위 내에서 완료했으며 API tail 인과/개선율은 여전히 미확정,추가 성능 측정과 반복 host screen은 보류다.

## API 변경1~3 및 실제 DB 검증 최종 정리 (2026-10-18)

3.85는 새 실행 없이 현재 코드/기존 검사·정리 원본을 대조한 최종 검토다.3.84 검증 시점의 보호 소스143개와 현재 해시가 모두 같았다. 구현과 대상 기능 검증 범위에서 추가 수정이 필요한 확정 결함은 발견하지 못했다. **1~3 API 변경·keepalive 검사 안정화·실제 DB 통합 기능 검증은 완료 상태로 정리한다.** 전체 repository 검증·성능 인과/SLO 완료를 뜻하지 않는다.

| 완료 항목 | 근거/범위 |
|---|---|
| 1 시간 구분·오류 판정 | client/handler/DB 진단,미완료/null 잔여/snapshot,모든 API 오류의 실패 및 원본/실패 JSON 보존. 실제 DB 오류 verdict도 PASS 문자열과 무관하게 거절. |
| 2 단일 파싱 | request별 options/AST1개,깊이·복잡도/전체 표준 검증/cycle guard/변수/operationName/ctx·취소 유지. |
| 3 compact JSON | 값/오류 의미·Playground 유지. 제한 on/off42개 호환 비교와 GraphQL/REST 기능 검사 통과. |
| keepalive | server deadline 갱신과 실제 만료/늦은 pong/비활성을 분리. 운영60s/54s와 테스트200ms 유지.10회 keepalive 및 GraphQL/REST package race 통과. 과거 실패 실행의 정확한 socket 원인은 기록 부족으로 미확정이며 이미 끝난 테스트 보완을 다시 미완료로 돌리지 않는다. |
| 실제 DB 통합 | pool4/예약1,유한 HTTP8개. pinned/borrowed,admission·SQL 취소/반납/재조회,SQL1/decode/phase,검증·깊이 SQL0,오류 verdict/보존. 저장소/API/app 대상10검사 GOMAXPROCS1/race·Python4·실제 DB verdict parser 검사 통과. |
| 검사용 자원 정리 | 소유 DB/container·volume/stale DSN 제거,기존12개 container 상태/StartedAt 보존. 후속 기능 검사용 전용 Go cache는 보존. |

DB 통합의 첫 app 실패는 새 fixture의 거래 수 불일치였다. 명시적2거래 데이터로 바로잡고 최종 app 검사를 통과했으며 validator를 완화하거나 실패 원본을 없애지 않았다. 저장소/API는 최초 실행부터 통과했고 해당 소스는 이후 바뀌지 않았다. 현재 합격 근거는 이 대상 검사 결과를 합친 것이며,저장소 전체를 한 번에 모두 검사했다고 표시하지 않는다. 단일 파싱 뒤 limits 시간 범위가 달라졌고 execution은 DB/decode를 포함하므로 과거 단계 p99를 그대로 차감/비교하지 않는다.

남은 API 관련 작업 우선순위는 다음과 같다.

1. **변경 확정/PR 전 전체 회귀 검증.** 저장소 규칙의 format/vet/lint·전체 기능/DB/example 검증과 repository 전체 race가 남는다. 새 동시성 검사의 push 전200회 검증도 해당 시점의 규칙이다. 현재의 대상10개 검사나 GraphQL/REST package race가 이를 대체하지 않는다. 이번 최종 검토에서는 실행하지 않았다. 부하/실제 node 옵션은 별도 요청 없이 켜지 않는다.
2. **API tail 인과·1~3 성능 효과 검증은 보류.** 기존 큰 지연의 단일 원인·3.73 회귀 해소·p99 개선율은 미확정이다. 새 진단이 포함된 부하 자료가 없어서 기존 log를 소급 분해할 수 없다. 사용자 별도 재개 요청 전 측정/host screen을 자동 반복하지 않는다.
3. **추가 최적화는 증거가 생긴 구간만 선택.** full-block decode/projection·SQL·입장 정책은 후순위 후보이며 지금 확정 수정안이 아니다. 기존에 악화해 제외한 wake 후보는 유지하고 DB4/RPC5/커밋 fresh/raw 내구성을 바꾸지 않는다.

현재 범위는 기능 구현·대상 검증 마감이며 성능 개선 확정/merge-ready/배포 완료로 표시하지 않는다. 이번 새 코드 변경/빌드/테스트/성능 측정/호스트 관찰/Docker/실제 DB/공개 RPC 조작은0이다. 문서의 현재 상태와 과거 이력을 구분했고 다른 프로젝트·DB4/API 예약1·RPC5 HTTP/s/burst1·커밋 직전 fresh/post-RPC epoch/transaction fence·SQL 바인딩/raw/설정/의존성은 유지했다. 최종 범위 근거는 `/private/tmp/indexer-api-final-review-20261018/final-audit.json`이다.

## PR 전 전체 회귀 검증 (2026-10-18)

3.86은 사용자 요청에 따라 추가 성능 측정 없이 저장소 전체 기능·정적·race·예제 검증을 실행했다. **요청한 비성능 검증 범위에서 미해결 기능 실패와 데이터 경쟁 보고는 없다.** PostgreSQL 최초 전체 명령은 검사 준비 오류1건으로 exit1이었으며, 해당1개 검사만 준비 보완 후 통과했다. 이 결과를 최초 명령 전체의 exit0으로 바꾸거나 표시하지 않는다.

| 검사 | 실제 실행 결과 |
|---|---|
| 전체 기능 | Go1.24.9, `go test -count=1 -timeout=20m -skip='^TestEngineLoad$' ./...`. 테스트 있는75개 package, 상위2279개/하위 포함4569개 통과. 상위81개/하위 포함162개 skip. 별도19개 package는 test file 없음. |
| 전체 race | 같은 Go/범위의 `go test -race -count=1` 실행. 동일한75개 package·상위2279개/하위 포함4569개 통과, 데이터 경쟁 보고0. 대상 package race로 대체하지 않았다. |
| 실제 PostgreSQL | 전용 PostgreSQL16, fsync=on, TZ=Asia/Seoul. DB 환경 분기가 있는9개 package 전체를 race로 실행. 최초8개 package 통과, app은 결과 파일 출력 준비1건 때문에 실패(상위368개 통과/30개 skip/1개 실패). 준비 보완 뒤 해당1개만 race 재검증 통과. 최종 기능 근거는 상위369개 통과이며 최초 실패 원본도 보존. |
| 정적·빌드 | CI와 같은 Go1.24.9의 전체 build/vet, 고정 toolchain 일치, Go1.24.9와1.26.8의 gofmt, diff 공백 검사 통과. Go1.26.8로 빌드된 golangci-lint2.14.0 전체 검사 통과. |
| 예제2개 | `chainprofile` 상위3개, `receipts` 상위4개/하위 포함14개 race 통과. 두 모듈 vet/lint도 통과. |
| 저장소 수명주기 | 별도 non-race `invariants` 태그의 `TestPebbleCacheLifecycle` 통과. race가 leak finalizer를 비활성화하는 공백을 별도로 확인. |
| 별도 도구 | `tools/astgraph` Go1.26.8 vet/lint 및 race 빌드 통과(test file 없음). 셸4개 문법 검사, Python 오류 판정 unit4개 통과. |
| Rust/IPC | Rust1.98.1에서 fmt, 잠긴 의존성의 offline unit2개, `--features reth` 타입 검사, fixture 빌드 통과. 실제 Rust→Go IPC/node-feed 상위11개/하위 포함18개 race 통과(측정 fixture1개 skip). Reth dependency의 `proc-macro-error2` 향후 Rust 호환 경고는 원본에 보존하고 의존성은 변경하지 않음. |

전체 Go 실행은 GOMAXPROCS2/package 병렬1을 사용했다. Go1.24.9를 작업 전용 경로에 준비했고, 처음 상속된 GOROOT1.26.8과 충돌한 실행은 컴파일 전에 실패했다. GOROOT를 해당 toolchain에 맞춘 뒤 위 최종 검사를 통과했으며 초기 로그도 보존했다. 이는 소스 컴파일 결함이나 실행된 기능 테스트 실패로 집계하지 않는다.

저장소 코드 수정은 test-only3개다.

- `pkg/app/testdb_test.go`, `pkg/app/postgres_test.go`: 공통 app/driver 비교 fixture의 PostgreSQL pool8→4. 실제 app의 pool4/API 예약 정책까지 전체 기능 검사에 포함한다. 작은 pool의 한계·무예약 동작을 확인하는 기존1/2개 계약 테스트는 원래 조건을 유지했다.
- `pkg/app/mixed_pool_diagnostics_unit_test.go`: staticcheck QF1011이 지적한 불필요한 `io.ReadCloser` 타입 선언을 추론 선언으로 정리했다. 타입/동작은 동일하며 수정 후 전체 lint·기능·race를 통과했다.

DB 최초 실패는 `TestAPIDiagnosticsPostgresIntegration` 마지막의 선택적 `db-api-verdict.json` 출력에서, 작업용 runner가 지정한 `db-functional/` 디렉터리를 만들지 않아 발생했다. 그 전의 예약·차용·취소/반납·복구·표준 validation/깊이·compact JSON·오류 verdict assertions는 모두 통과했다. runner에서 소유 결과 디렉터리를 준비하고 같은 소스/DB4/예약1의 이 테스트만 다시 실행해 통과했다. 기능 assertion·오류 기준·production 코드는 바꾸지 않았고 DB 전체를 다시 실행하지 않았다. 실제 출력은 요청8개, 의도한 graphql_error3/transport_error1, 미완료0, verdict=false를 보존한다. 최초9개 package 결과와 재검증1개의 합집합으로 확인했으며, 하나의 처음부터 깨끗한 전체 PG 실행이라고 주장하지 않는다.

이번 작업에서 production/API1~3 구현·설정·의존성·DB4/API 예약1·RPC5 HTTP/s/burst1·커밋 직전 fresh/post-RPC epoch/transaction fence·SQL 바인딩·raw fsync/barrier는 변경하지 않았다. 이전 보호143개 중 변경은 위 test 선언1개뿐이다. 나머지 두 fixture는 그 목록 밖의 기존 테스트다. 최초 전체 파일1178개의 기준 해시도 별도로 보존했다.

`TestEngineLoad`는 opt-in 없이1만 구독자의 p99를 측정하므로 사용자 지시에 따라 명시적으로 제외했다. 모든 INDEXER 측정·SLO·live opt-in도 비활성화했고 새 처리량/p99·호스트 부하 관찰·공개 RPC·실제 node/peer 실행은0이다. `integration` 태그의 실제 localhost 노드 요구 코드는 컴파일만 확인했다. 삭제된 외부 Arc checkout의 hook 설치/노드 빌드, 실제 peer·외장 mount·운영 SLO는 이 검증의 합격 주장에 포함하지 않는다.

전용 DB container·named volume·DSN을 ID/label로 확인해 제거했고 기존12개 container의 ID/status/StartedAt은 모두 같았다. 작업 전용 Rust target도 검사 후 제거하며, 후속 기능 검사용 Go cache와 고정 Go toolchain은 유지한다. 공유 cache·다른 프로젝트·Git branch/staging/commit/PR은 조작하지 않았다. 원본·단계별 결과·설정 오류 및 단독 재검증·범위 해시·정리는 `/private/tmp/indexer-pr-regression-20261018/`의 `regression-audit.json`, `resolution.json`, `cleanup.json`, `build-cleanup.json`을 따른다.

PR 전 비성능 전체 회귀 항목은 이 범위에서 마감한다. 같은 소스를 이유 없이 다시 검사하거나 성능 측정/host screen을 자동 재개하지 않는다. **push 전 새 동시성 검사200회/GOMAXPROCS1/race 규칙은 실제 push 시 별도 조건**이며 이번에는 push하지 않아 실행하지 않았다. API tail 인과·1~3 성능 효과·운영 SLO는 여전히 보류/미확정이다. 후속 최적화는 증거가 확보된 구간에서만 선택한다.

## Push 전 API 동시성 200회 검증 (2026-10-18)

3.87은 API1~3 및 관련 연결 수명주기의 남은 동시성 검사를 Go1.24.9·GOMAXPROCS=1·race·각 단일 `-count=200`으로 실행했다. **선정한18개 검사가 각각 연속200회, 상위3600회 통과했다. 최종 실패·skip·DATA RACE 보고는 모두0이다.** 저장소 전체 테스트를200회 실행했다는 뜻은 아니다. 이전 수집/RPC/epoch 관련200회 기록을 대상 선정에 참고했으며 그 과거 횟수는 이번3600회에 합산하지 않았다. 정확한18개 이름·명령·카운터는 증거 폴더의 `plan.json`, `verification.json`에 있다.

| 최종 실행 | 상위 검사 수 | 각 반복 | 상위 통과 | 하위 포함 통과 |
|---|---:|---:|---:|---:|
| GraphQL keepalive·취소·진단 | 3 | 200 | 600 | 2600 |
| 늦은 callback/snapshot | 1 | 200 | 200 | 200 |
| 수정한 SQL 취소 검사 | 1 | 200 | 200 | 1000 |
| DB 예약·차용·반납·SQL/transaction IO | 11 | 200 | 2200 | 3800 |
| 다른 작업이 pool을 채운 상태의 HTTP | 1 | 200 | 200 | 200 |
| 실제 DB API 진단·취소·오류 통합 | 1 | 200 | 200 | 200 |
| 합계 | 18 | 각200 | 3600 | 8000 |

모든 최종 명령은 `go test -race -count=200 -parallel=1 -failfast -timeout=45m -run='<고정 선택식>'`이며 package 병렬1·GOMAXPROCS1을 사용했다. Go의 pass/run 카운터가 검사별 각각200인지와 exit0·skip0을 대조했다. 실제 PostgreSQL16/fsync=on의 명시적 전용 container/volume을 사용했고 기본 Store/API fixture는 pool4/예약1이다. 기존1/2개 pool의 무예약 계약 검사도 조건을 유지했다. 실제 통합은 매회 유한 HTTP8개와 의도 오류4개의 실패 verdict를 확인하며 부하 도착 루프·처리량/p99는 집계하지 않는다.

### 발견한 실패와 수정

첫 DB 묶음은123번째 `TestReservedQueryIOCancellation/row`에서 실패했다. SQL 취소/오류 판정과 후속 `SELECT 7`은 성공했으나 직후 `AcquiredConns`가 기대1 대신2였다. DATA RACE 보고는 없었다. `pgxpool.Conn.Release`는 닫힌 연결을 `puddle.Resource.Destroy`에 넘기며, 이 함수는 별도 goroutine에서 연결 정리와 `CleanupDone` 대기를 끝낸 뒤 pool 자원을 제거한다. 따라서 새 예약 연결의 조회 성공이 이전 연결의 폐기/통계 반영 완료를 동기적으로 보장하지 않는다. 해당 실행의 두 backend PID 폐기 순서를 별도 추적한 것은 아니지만, 실패의 관측값과 비동기 폐기 계약은 확인했다.

`pkg/storage/postgres/query_io_test.go`의 즉시 `EqualValues(1)`을 같은 조건 `AcquiredConns()==1`의 최대1초/1ms 간격 `Eventually`로 보완했다. 기존 borrowed/transaction 취소 검사와 같은 상태 대기 방식이다. 허용 연결 수를2로 늘리거나 SQL100ms 취소·3초 hang guard·정상 재조회·pool 최대4 조건을 없애지 않았다. 운영 IO/예약 정책에는 대기를 추가하지 않았다. 수정한 전체 검사(exec/row/query/stream)를 최종 소스로 연속200회 통과했다.

실패 전 다른 검사의122/123회 기록을 합산하려던 초기 계획도 바로잡았다. 별도 프로세스의122+78은 단일 `-count=200`과 같은 반복 상태를 보장하지 않으므로, 시작된78회 실행의 소유 parent/Go process group을 명령·PID로 확인해 종료했다. 그 부분 실행과 최초 중단 묶음의 모든 횟수는 최종3600회에서 제외했다. 이미 온전히 끝난 GraphQL/snapshot/수정 SQL 취소 검사는 유지하고 남은11개를 단일200회로 다시 완료했다. 원본 실패와 중단/계획 수정 기록도 보존한다.

DB 준비 단계에서도 초기화용 Unix socket 서버가 ready를 반환하지만 실제 `indexer` DB가 아직 없는 경우를 확인했다(원본에 `FATAL: database "indexer" does not exist` 뒤 CREATE DATABASE/정상 서버 시작이 있다). `scripts/test-postgres.sh`를 실제 DB의 TCP `psql -h 127.0.0.1 -U indexer -d indexer -Atc 'SELECT 1'` 성공 뒤 검사하도록 바꿨다.60회 안에 준비되지 않으면 명시적 실패로 종료해 기존 소유 자원 cleanup을 수행한다. 실제 스크립트를 별도의 전용 DB와 기존 진단 검사1회/race로 확인했고 container/volume 제거도 통과했다. 이1회는200회 합격 수에 넣지 않는다. 기존 fsync/max_connections/volume cleanup 정책은 바꾸지 않았다.

### 범위와 보존

이번 코드 변경은 위 테스트1개와 DB 검사 스크립트1개뿐이다. 운영 Go 소스·설정/의존성·DB4/예약1·RPC5 HTTP/s/burst1·커밋 직전 fresh/post-RPC epoch/transaction fence·SQL 값 바인딩·raw 내구성은 기준 해시와 같다. 수정 package의 vet/Go1.24.9 gofmt·shell syntax·diff 공백 검사도 통과했다.3.86의 전체 회귀를 다시 반복하거나 그때 로그를 이번 테스트 수정 후 전체 실행이라고 표시하지 않는다.

새 성능 측정·host screen·공개 RPC·실제 node/peer 실행은0이며 다른 프로젝트와 공유 cache는 조작하지 않았다. 전용 DB/container·named volume·DSN을 ID/label로 확인해 제거했고 기존12개 container의 ID/status/StartedAt도 보존했다. 준비 스크립트의 별도 임시 DB/volume 역시 제거됐다. Go cache/toolchain은 후속 기능 검사용으로 유지한다. Git staging/commit/push/PR은 실행하지 않았다.

근거는 `/private/tmp/indexer-concurrency200-20261018/`의 `verification.json`, `final-audit.json`, `failure-analysis.json`, `single-invocation-correction.json`, `script-functional.json`, `cleanup.json`과 각 최종/최초 원본 로그다. 이 API 범위의 push 전 동시성200회 항목은 완료로 마감한다. 새 변경/실패 없이 동일 검사를 다시 미완료로 제안하지 않는다. API tail 인과·성능 효과·운영 SLO 및 실제 peer/mount는 기존대로 미확정/보류다.


## PR CI의 PostgreSQL 내구성 설정 불일치 (2026-10-19)

첫 PR CI [39139569852](https://github.com/0xmhha/indexer-go/actions/runs/39139569852)는 lint/test/examples PASS, postgres FAIL이었다. PostgreSQL의 상위31개 실패 중 직접 오류는 capture의 fsync 요구 거부이고, 네 체인 격리 검사는 Arc 진행 대기 timeout이었다. 로컬 전체 실제 DB 검증은 fsync=on이었으나 공식 `scripts/test-postgres.sh`가 fsync=off를 강제했다. 준비 단계에서 이 실행 조건 차이를 놓쳤다. 이 기록은 앞선 로컬 검증을 CI 전체 통과로 확대하지 않는다.

시험 스크립트의 임시 DB만 `fsync=on`으로 맞췄다. capture/projection의 내구성 guard, synchronous_commit on, SQL 바인딩, commit fresh와 공유 RPC5 HTTP/s를 완화하지 않았다. 같은 수정 스크립트에서 Go1.24.9/GOMAXPROCS1/race/각1회로 다음5개를 통과했다: TestCaptureBatchSnapshotBindingAndOwnership, TestCaptureBatchBoundsAndStickyFailure, TestProjectionDurability, TestManagedBlobCapabilityReadonlyAndLegacy, TestManagedFeedFourChainIsolation. 실패/skip0, 소요31.55초였다. 이는 추가 성능 측정이나 전체 회귀/200회 반복이 아니다. Go 소스/운영 설정/의존성 변경은 없으며 앞선 전체 race/동시성 근거는 유지한다. 임시 DB/volume이 정리됐고 기존13개 container 상태/StartedAt 변경은0이었다. 원본은 `/private/tmp/indexer-pr-submit-20261019/`에 보존한다. PR 병합에는 수정 커밋의 보호 CI 네 항목 통과가 별도로 필요하다.
