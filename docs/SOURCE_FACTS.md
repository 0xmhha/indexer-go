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

현재 실행 plan에는 계정 상태·code·contract call 결과가 없다. 해당 메서드는 일반 `ErrCoverage`를 반환하며 provider의 pruned-state 응답처럼 취급하지 않는다. `allow_pruned_state`가 켜져 있어도0/latest fallback을 허용하지 않는다. 상태가 필요 없는 feature는 재생할 수 있다. native balance는 projection DB에 이미 검증하여 초기화한 모든 해당 계정 기록이 있으면 delta로 계속 진행할 수 있고, 처음 만난 계정이나 resync에 필요한 상태가 없으면 블록을 중단한다. 초기 계정 기록과 필요한 상태 coverage는 호출자가 검증해 준비해야 한다. 이 source가 운영 snapshot import나 전체 상태 공급을 자동 수행하지 않는다.

## Fresh 검사와 커밋 내구성

새 Source는 선택적 `source.CommitValidator`를 구현한다. writer는 일반 block·이미 저장된 block 재시도·feature backfill의 커밋 직전에 이를 호출한다. capture generation/plan/정확한 canonical input identity를 확인하고 기존 live canonical reader로 현재 hash를 다시 조회한다. state preparation을 하지 않은 block도 검사한다. 공개 RPC의5 HTTP/s 제한을 그대로 공유하며 hash를 입력/DB cache로 대체하지 않는다.

이 경로의 projection 저장소와 그 wrapper는 `port.BlockDurability`를 구현/전달해야 한다. Pebble은 WAL 비활성화를 거부하고 기존 Sync commit을 사용한다. PostgreSQL은 fsync=on을 요구하며 transaction의 synchronous_commit을 on으로 강제한다. 일반 RPC/era/replay의 기존 경로는 이 opt-in 검사를 새로 요구하지 않는다.

source와 projection이 **같은 Store handle**을 쓸 때 PostgreSQL은 bound block transaction을 재사용한다. 공개 prefix 검증은 선택 `CaptureProjectionView`의 KEY SHARE로 세대를 고정하며, 일반 full `CaptureView`는 FOR UPDATE로 namespace 전체를 고정한다. 한 연결 pool도 중첩 transaction 대기 없이 동작하며 capture generation 변경은 projection commit/rollback까지 기다린다. queued SQL은 같은 transaction 안에서 먼저 flush한다. Pebble은 원래 block write lock이 같은 DB의 capture 변경을 배제한다.

bound transaction 재사용과 독립 facts 읽기의 연결 예산은 구분한다. 운영 초안과
native execution은 체인/프로세스 Store pool4를 사용하며 별도 write/read pool을
만들지 않는다. 일반 facts 경로는 명시적1/2/기본0 예산을 자동 증액하지 않는다.
0은 DSN/pgxpool 기본값이다. 같은 Store의 사전 읽기는 일반 연결을 빌리므로
writer transaction이 차지한 연결과 경합할 수 있다. API 예약 조건은 아래를 따른다.

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

## DB 연결과 API 예약

단일 체인 `node.role: all`에서 `database.postgres.max_conns: 4`를 명시하면
root API의 짧은 projection SQL용으로 같은 pool 안에서1개를 예약한다. 사용 가능한
일반 연결도 짧게 차용하며 총 pool 상한4를 늘리지 않는다. ingest/API 전용 역할,
read-only Store와 다른 명시적 pool 예산을 이 정책으로 자동 변경하지 않는다.
직접 API를 조립할 때는 `api.Config.ReserveQueryReader`로 명시한다.

예약 reader/차용 연결/transaction의 수명은 실제 query·row/stream 소비·취소
callback의 반환과 함께 관리한다. 취소가 완료되기 전에 같은 연결을 다음 SQL로
돌리거나 호출 종료만으로 모든 pool 통계가 즉시 반영됐다고 가정하지 않는다.
source의 bound commit fence는 API 예약 reader로 이동하지 않는다. background
통계는 API 예약 context를 빌리지 않으며 source generation·현재 읽기 fence도 유지한다.

capture의 공개 prefix 검사·append·immutable ACK와 mutable full view/Reset/삭제는
서로 다른 잠금 계약이다. 동시성은 기존 snapshot·generation·writer commit 경계를
보존하는 범위에서만 허용한다. [capture 계약](CAPTURE_STORAGE.md)을 따른다.

## GraphQL 요청 처리

HTTP 요청별 AST 하나를 깊이/복잡도 검사·표준 문서 검증·실행에 공유한다.
operationName/변수 coercion/fragment/alias와 원래 request context·취소 처리를
유지한다. AST/result/latest cache를 추가한 의미가 아니며 독립 helper와 REST의
고정 문서 실행 계약은 별개다.

HTTP GraphQL 결과는 compact JSON이다. 데이터·문자열/숫자/null·배열 순서와
오류 message/locations/path/extensions, status/Content-Type의 의미를 유지한다.
Playground와 HTML renderer는 이 JSON 직렬화 계약과 분리된다.

## API 진단과 오류 판정

진단은 trusted request context의 opt-in observer로 수집하며 HTTP header만으로
활성화하거나 API 응답에 query/variables/result·진단 payload를 노출하지 않는다.

| 구간 | 해석 |
| --- | --- |
| Client `PrepareMS` | 요청 준비 |
| Client `HTTPMS` | Client.Do부터 본문 읽기까지 |
| Client `ValidationMS` | JSON/fixture 검증 |
| `ServerMS` | handler의 시작~완료 |
| GraphQL stages | request_prepare·parse·limits·validation·execution·response |
| `AcquireMS` / `QueryMS` | 연결 admission / 드라이버 응답·행 소비 |

시작하지 않은 구간과 실제0ms를 구분한다. HTTP 전체와 server/DB 구간은 서로
다른 관측이며 중첩/병렬 시간을 무조건 합하거나 나머지를 순수 CPU 시간으로
단정하지 않는다. handler/resolver는 다른 goroutine일 수 있으므로 같은 goroutine의
SQL 수만으로 요청 전체 SQL0/RPC0을 주장하지 않는다.

본문 읽기 timeout, GraphQL errors, transport/JSON 실패와 미완료 요청을 별도로
판정한다. HTTP200이나 fail counter0만으로 성공으로 만들지 않는다. 의도된 오류
fixture도 그 오류 결과를 보존하며 실제 완료/시간 구간과 대응하는 요청만 진단한다.
API 지연의 원인·개선율·운영 SLO는 이 기능의 존재만으로 확정되지 않는다.

## 검증 범위

관련 검사는 facts materialization, coverage 오류, fresh 불일치, 취소·연결 반납,
원본 제거 후 읽기, ACK 유실·reorg·epoch 교체 및 GraphQL 의미/제한을 다룬다.
[CI 범위](CI.md)와 [현재 수용 기능](SESSION_STATE.md)을 따른다.
새로 합의한 범위 없이 성능·호스트/SLO 검사를 반복하지 않는다. 이전 측정과
실행 이력은 Git history로 확인하며 현재 사용 가이드에 누적하지 않는다.
