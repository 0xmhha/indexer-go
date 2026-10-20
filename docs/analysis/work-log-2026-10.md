# 작업 결정·검증 기록 (2026-10-09~14)

이 문서는 당시의 문제·선택지·결정·측정 결과를 보존한 기록이다. 결정 전 상태와 결정 후 결과가 함께 들어 있다. 현재 범위와 남은 작업은 [open-items.md](open-items.md), 현재 구조는 [ARCHITECTURE.md](../ARCHITECTURE.md)를 따른다. Stable 관련 구현 기록은 과거 사실이며 현재 지원 범위가 아니다.

## 1. 정해야 할 것

### 1.1 fee delegation 메타데이터를 쓸지 지울지 (결정 10/9: 쓴다)

결정: fee payer 색인을 GraphQL `feePayerTransactions(feePayer, pagination)`이 읽는다(cursor 페이지, 오래된 것부터). 쓰기를 지우면 나중에 다시 필요할 때 재색인해야 하고, 조회를 더하는 쪽은 되돌릴 수 있어서 이쪽을 골랐다. 트랜잭션별 메타데이터(`TxMeta`)는 트랜잭션 모델의 fee delegation 필드와 내용이 같아 여전히 읽는 곳이 없다.


`stablenet.fee_delegation` 기능은 fee delegation 트랜잭션마다 메타데이터와 fee payer 색인을 저장한다. 그런데 이것을 읽는 GraphQL·JSON-RPC 조회가 없다(그래프에서 `MetaStore.TxMeta`, `TxsByFeePayer`가 운영 경로에 닿지 않는다). 지금 조회는 트랜잭션 모델의 fee delegation 필드에서 답한다.

- 조회를 더한다(`transactionsByFeePayer` 등): 저장한 색인을 쓰게 된다. 단점은 API 면이 늘고 indexer-frontend가 써야 의미가 있다는 것이다.
- 기능의 쓰기를 지운다: 저장 공간과 블록마다의 쓰기가 줄어든다. 단점은 나중에 fee payer 조회가 필요하면 재색인해야 한다는 것이다.

### 1.2 CI를 둘지 (결정 10/9: 둔다)

결정: GitHub Actions(`.github/workflows/ci.yml`)가 PR과 기본 branch의 commit마다 gofmt, vet, lint(두 모듈), `go test ./...`, PostgreSQL 시험, 예제를 돌린다. SLO와 넓은 범위 측정처럼 무거운 시험은 넣지 않았다.


저장소에 `.github/workflows`가 없다. 그래서 `go test ./...` 밖의 시험(예제 모듈, PostgreSQL, SLO, 넓은 범위 측정)과 호환성 약속의 강제(`TestSDKSurface`)는 사람이 돌릴 때만 돈다.

- GitHub Actions로 `go test ./...`, lint, `make test-examples`, PostgreSQL(서비스 컨테이너)을 PR마다 돌린다: 약속과 회귀가 자동으로 지켜진다. 단점은 PR마다 시간이 들고(전체 시험과 PostgreSQL 시험만 합쳐 지금 로컬에서 약 6분), SLO처럼 무거운 시험은 따로 일정으로 돌려야 한다는 것이다.
- 지금처럼 release 전에 직접 돌린다: 비용이 없다. 단점은 빠뜨리기 쉽다는 것이다(이번 검토에서 lint 338건이 쌓여 있었다).

### 1.3 SDK 버전 규칙 (결정 10/9: 지금 규칙 유지)

`docs/SDK.md` 7장의 규칙(v1 전에는 깨는 변경을 minor 버전에서만, 변경 기록과 함께)은 기본값으로 정했다. v0.2.0을 발행하면 이 규칙이 처음 적용된다.

## 2. 고쳐야 할 것

| 항목 | 지금 동작 | 영향 | 심각도 |
|---|---|---|---|
| gap 채우기 재시도 (해소 10/9) | 남은 gap을 다시 찾아 채우는 round를 3번까지 하고, 그래도 실패하면 오류로 시작을 멈춘다(`Fetcher.recoverGaps`). 전에는 로그만 남기고 빈칸을 둔 채 live loop로 넘어갔다 | — | — |
| 멀티체인 failover (해소 10/9) | 체인 항목의 `fallback_endpoints`로 체인마다 failover한다(API로는 노출하지 않음) | — | — |
| 해석할 수 없는 outbox 항목 (해소 10/9) | relay가 그 위치를 `events.SkippedEvent`로 발행해 구독 엔진이 빈칸으로 보지 않는다. 그 이벤트를 받는 구독은 없다 | — | — |
| outbox 정리 보호 (검토 10/9: 설계상 의도, 바꾸지 않음) | prune은 같은 프로세스에서 소비 중인 group만 본다(`OutboxBus` 주석에 명시). 알림 group은 relay와 같은 ingest/all 프로세스에서만 돌아 실행 중에는 보호되고, ingest가 멈추면 새 entry가 없어 prune도 없다. API 프로세스는 위치를 메모리에만 두므로 저장소가 보호할 수 없고, 재개는 `SEQUENCE_TOO_OLD`로 답한다. 기록된 모든 group을 보호하면 버려진 group 하나가 outbox를 끝없이 키운다 | — | — |
| relay 시작 실패 (해소 10/9) | `Fetcher.Recover`가 join을 다시 시도하고, 실패하면 시작을 멈춘다 | — | — |
| API 프로세스의 알림 (해소 10/10, 결정: api가 쓰고 ingest가 다시 읽기) | api 프로세스가 알림 API를 서빙하고 알림 키만 DB에 쓴다. ingest의 알림 서비스가 설정을 5초마다 다시 읽는다. 재시도는 pending으로 저장해 ingest가 보낸다(전에는 재시도가 저장되지 않아 queue가 차 있으면 사라졌다) | — | — |
| 값 형식 통일 (결정 10/10: 보류) | 값이 JSON, RLP, 고유 binary로 섞여 있다. 기존 값은 그대로 둔다(바꾸면 모든 DB를 재색인해야 한다). 새 저장 코드의 규칙: 체인 데이터(블록, 트랜잭션, 영수증, 로그)는 `pkg/core/model`의 인코딩을, 기능의 레코드는 JSON을 쓴다(지금 기능들이 쓰는 방식) | 형식이 섞인 상태는 남는다 | — |
| 토큰 메타데이터 on-demand 조회 (발견·해소 10/10) | 이전에는 `setTokenMetadataFetcher`의 type assertion이 `ethclient.Client`와 맞지 않아 fetcher가 설정되지 않았다. 이제 RPC proxy를 만들 때 fetcher를 설정하고, 노드 호출은 proxy의 cache(`eth_getCode` 24시간, latest의 `eth_call` 30초, revert 포함), 노드 rate limit(100/s, burst 200), circuit breaker를 거친다. 호출 하나라도 답을 받지 못하면(rate limit 등) 메타데이터를 버려 빈 칸이 영구 저장되지 않게 한다 | 색인되지 않은 토큰 하나에 노드 호출 약 6~10번(첫 요청). all/ingest는 저장해 다음부터 노드를 부르지 않고, api 역할은 저장할 수 없어 cache 만료 뒤 다시 부른다 | — |
| 알림 API 인증과 webhook SSRF (발견·해소 10/10) | 알림 설정 API에 인증이 없었고, webhook·Slack 주소가 내부망·cloud metadata여도 등록·전송했다(알림을 켠 배포에서 누구나 서버가 내부 주소로 요청하게 만들 수 있었다). 이제 알림 API는 `api.keys`의 키가 필요하고, 내부 주소는 등록과 연결 시점에 거절하며 redirect를 따르지 않는다 | 키를 나눠 줘야 알림 API를 쓸 수 있다. 키별 소유는 10/10 해소: 설정은 만든 키의 것이고 다른 키에는 없는 것처럼 보이며, 운영자 라벨만 모두 본다(소유자 없는 예전 설정은 운영자만) | — |
| 노드 URL 노출 (해소 10/9) | 노출 경로는 GraphQL이 아니라 `GET /chains`였다(GraphQL 멀티체인 모듈은 서빙되지 않는 코드였다). 이제 노드 URL은 scheme과 host만 내보낸다. 인증 없는 체인 등록 mutation이 든 서빙되지 않던 GraphQL 모듈은 지웠다 | — | — |
| HTTP 경로와 멀티체인 (해소 10/9) | 멀티체인 서버가 체인마다 `/chains/{id}/<pattern>`에 mount한다(`{id}` 인자가 있는 패턴은 제외). ingest 역할은 의도대로 mount하지 않는다 | — | — |
| records 표 정의 변경 (해소 10/10, 결정: 명시적 허용) | 표에 컨트랙트 주소를 더하면 그 표를 처음부터 다시 채운다(`feature.PartEvolver`). 이벤트·키 변경과 주소 삭제는 표를 `features.records.rebuild`에 적었을 때만 그 표의 레코드를 지우고(새 정의 기록과 같은 트랜잭션) 처음부터 다시 채우고(`feature.PartRebuilder`, `port.RecordWriter.DeleteRecords`), 적지 않으면 지금처럼 시작을 거부한다 | 다시 채우는 동안 그 표의 조회는 일부만 돌려준다 | — |
| declared 모드 범위 (결정 10/10: 지원한다, 해소 10/10) | 멀티체인(체인별 `features`)과 `source.era_dir`(era1 파일에서 선언한 로그를 걸러 읽음)를 지원한다. 실제 era1 파일로 노드 `eth_getLogs`와 속도 비교는 하지 않았다. finalized 범위 모드는 로그 없는 블록을 저장하지 않는다. 실시간 구독의 전제로 둘 다 지원하기로 했다(`subscriptions-design.md` 3장, 7장 1~2단계) | 구현 전까지 해당 구성은 시작하지 않는다 | [권장] |
| ERC-4337 EntryPoint 설정 (발견·해소 10/10) | UserOp는 알려진 EntryPoint(v0.6, v0.7)의 로그만 읽었고 `account_abstraction.entry_point_addresses`는 경고만 냈다. 그래서 poc-contract의 EntryPoint v0.9를 거친 UserOp는 하나도 색인되지 않았다. 이제 `features.aa.erc4337.entry_points`(주소와 버전, v0.6~v0.9)로 더한다. 기능 설정이므로 멀티체인 모드에서는 체인마다 준다(`chains[].features`). 주소를 코드나 chain profile에 넣지 않은 이유: poc-contract의 EntryPoint 주소는 `Create2Deployer` 주소에 따라 달라지고, 그 주소는 배포 계정의 nonce에 따라 달라진다. 로컬 anvil에 poc-contract EntryPoint v0.9를 배포하고 `handleOps` 한 건을 보내 확인했다: 설정이 없으면 0건, 있으면 1건이 hash·nonce·gasUsed까지 체인의 이벤트와 같게 색인됐다 | 이미 색인한 DB에 주소를 더하면 그 뒤 블록부터 색인된다. 앞선 블록은 재색인해야 한다(통계가 누적 값이라 다시 채우면 두 번 센다) | — |
| perp 마켓 이름 (검토 10/10: 하지 않음) | perp 마켓은 기초 자산 토큰이 없어 `MarketCreated`의 base가 `address(0)`이다. poc-contract #2가 `PerpetualEngine.getMarketSymbols(marketId)`를 더했다(로컬 anvil에서 "BTC", "USDT"를 돌려주는 것을 확인). 이 이름을 저장하지 않기로 했다. 이유: 이름을 요구하는 소비자(indexer-frontend, 캔들 이름표)가 아직 없다. 저장하려면 `DexMarket`에 필드를 더하고 PostgreSQL migration과 golden을 바꿔야 한다. 생성 블록에서 `eth_call`로 읽으면 옛 마켓의 backfill에 archive 노드가 필요하다. 필요해지면 두 방법이 있다. 첫째, 생성 블록에서 읽어 저장한다. 결정적이지만 위 비용이 든다. 둘째, 조회할 때 RPC proxy로 읽어 cache한다(토큰 메타데이터와 같은 방식). 저장 변경이 없지만 노드가 응답하지 않으면 이름이 비고, 응답이 블록에 묶이지 않는다. 이름은 생성 뒤 바뀌지 않아 둘째도 값은 같다 | API의 perp 마켓에 이름이 없다. 클라이언트가 engine에서 직접 읽어야 한다 | [권장] |
| archive가 아닌 노드 (해소 10/12, 결정: 시작 시 거부) | 노드 상태를 읽는 기능(`balance.native`, `token.metadata`, `feature.StateReader`)이 처리할 첫 블록(다음 색인 블록, 또는 색인된 DB에서 새로 켰을 때 backfill 시작) 직전의 상태를 시작할 때 노드에 묻고, 노드가 그 상태가 없다고 답하면 시작을 멈춘다(`pkg/app/node_state.go`). 기능마다 `features.<name>.allow_pruned_state: true`면 예전처럼 계속한다(`balance.native`는 0에서 시작, `token.metadata`는 latest로 읽음, 경고) | — | — |

## 3. 측정과 확인이 남은 것

3.6절은 당시 범위 결정 기록이다. 현재 작업 목록은 [open-items.md](open-items.md)를 따른다. 3.4~3.5절은 당시 검증 기록이며, 10/13 결정으로 Stable은 현재 지원 대상에서 제외했다.

| 항목 | 남은 일 |
|---|---|
| K2 이진 키 (10/12 재측정, 보류 유지) | StableNet testnet 블록 0~700,000(키 1,761만 개)에서 키는 60% 짧아지지만 디스크는 6.7%만 준다. cache가 찬 뒤 단건 조회 약 30%, 20건 범위 조회 약 11% 빠르다(`TestBinaryKeysOnRealData`, refactoring-plan 11절). 저장 형식을 다시 바꿀 때 함께 검토한다 |
| PostgreSQL 쓰기 성능 (10/11 재측정, 원인 확인, 일부 해소) | `BenchmarkIngest/regular`(블록 202개, 거래 10,001건, 3회씩 3번)에서 Pebble 1.6~2.7초, PostgreSQL 32~48초(fsync 켬 32~36초, 끔 34~48초)로 약 15~20배 느리다(R4-2의 4배보다 크다). macOS Docker의 PostgreSQL 16이다. 원인은 디스크가 아니라 왕복이다. fsync를 꺼도 같고, 서버의 실행 시간 합은 수 초이며, 클라이언트 CPU는 실행 시간의 17%이고 그 대부분이 네트워크 대기(`kevent`)다. 이 환경에서 문장 하나의 왕복은 p50 292µs, 같은 문장 100개를 한 batch로 보내면 합계 0.97ms다. 블록마다 따로 보내는 문장이 많다. 잔액 갱신 하나가 3번(`SELECT ... FOR UPDATE`, `balance_history` INSERT, `balances` upsert, 15,007회), receipt가 거래마다 1번(10,001회), ERC-20 이동이 1번(5,000회)이다. 로그와 주소 색인은 이미 batch다. 고치는 방향: 블록 안의 쓰기를 batch로 모은다. 잔액은 SQL 안에서 계산하게 바꿔 읽기 왕복을 없앤다. 같은 호스트의 PostgreSQL(Docker NAT 없이)은 왕복 자체가 짧다. 고침(10/11): 블록 트랜잭션 안에서 결과를 읽지 않는 쓰기(주소 색인, receipt, ERC-20 이동, 잔액 이력·upsert, 기능의 KV·레코드 등 26곳과 기존 batch)는 대기열에 쌓았다가, 같은 트랜잭션의 다른 문장이나 commit 직전에 한 번에 보낸다(`pkg/storage/postgres/pending.go`). 그래서 트랜잭션은 자기 쓰기를 그대로 읽는다. 잔액은 트랜잭션 안에서 계정마다 한 번만 읽는다. 같은 부하에서 번갈아 쟀다(기계 load average 30~45, 다른 작업이 돎). main 44~68초가 batch 8.1~9.1초로 약 5배 빨라졌다. Pebble은 2.0~4.3초라 차이가 약 2~4배로 줄었다. 남은 비용은 서버 쪽 undo trigger(행마다 `undo_log`와 `jsonb_object_agg`)와, 블록마다 계정을 처음 읽을 때의 왕복이다 이어서(10/11, migration 0012): 블록이 행을 넣기만 하는 표 8개는 trigger 대신 그 행을 쓴 트랜잭션의 높이로 되돌린다. 잔액은 블록마다 계정당 한 번 쓴다(`postgres-undo-design.md`). trigger 호출이 97% 줄었다. 같은 부하에서 main 7.0~7.6초가 5.2~5.9초가 되었고, Pebble은 1.6~2.0초다. 남은 것은 블록마다 계정을 처음 읽는 왕복이다 이어서(10/12): 잔액 기능이 블록에서 바뀌는 계정을 한 번에 읽는다(`port.BalancePrefetcher`, PostgreSQL은 `= ANY` 한 질의로 읽고 잠근다). 블록 트랜잭션이 잔액과 기록 여부를 갖고 있으므로, 계정마다 하던 `EXISTS`(1,202번)와 `FOR UPDATE`(1,201번)가 블록당 한 번(201번)으로 줄었다. 같은 부하에서 main 4.1~4.3초가 3.1~3.6초가 되었고, Pebble은 1.5~1.7초다 |
| 넓은 범위 조회 (해소 10/10, 측정 10/12) | 블록 범위 없는 조회에서 트랜잭션 `from`/`to`는 주소 색인(새 port `AddressTransactionsNewestFirst`, `address.index`가 gap 없이 켜져 있을 때)으로, 로그 `address`/`topics`는 로그 색인으로 답한다. 색인이 없는 조건은 블록을 최대 10,000개 읽고 멈추며 `scannedThrough`로 멈춘 위치를 알린다(결정 10/10: 색인 사용 + scan 상한). 범위를 10,000블록보다 길게 준 조회는 트랜잭션은 최신 쪽, 로그는 오래된 쪽만 읽고 `scannedThrough`로 알린다(전에는 트랜잭션도 오래된 쪽을 말없이 읽었다). 10,000블록 범위의 블록당 비용 증가(R0-8)는 10/10 다시 재고 원인을 확인했다: 1,000블록 13.5µs, 10,000블록 22.6µs. CPU의 약 60%가 Pebble이 sstable을 파일에서 읽는 시간이었고, block cache(128MB, `storage.DefaultConfig` 고정, 설정으로 바꿀 수 없음)를 1GB로 올리면 10,000블록이 15.6µs로 1,000블록(14.5µs)과 거의 같아진다. 조회 하나가 블록을 한꺼번에 올려 블록당 약 17KB, 10,000블록에 170MB를 할당하고 GC를 한 번 부른다. 고침(결정 10/10: 둘 다): `database.cache_mb`(`INDEXER_DB_CACHE_MB`, 기본 128)로 cache를 정한다(1024에서 10,000블록 조회 약 220ms → 160ms). 범위 조회는 블록을 하나씩 읽고 일치하는 트랜잭션만 남겨 조회 중 최대 heap이 약 290MB에서 210MB로 줄었다(총 할당과 시간은 같다). 10/12 main `7a919e9`에서 다시 쟀다(`INDEXER_BENCH_LARGE=1`, 24,000블록 × 트랜잭션 4, 기본 cache 128MB): 100블록 1.5ms(14.7µs/블록), 1,000블록 14.5ms(14.5µs), 10,000블록 218ms(21.8µs). 10배 범위마다 시간은 9.9배, 15.0배로 시험의 상한(30배) 안이다. 범위 없는 첫 페이지는 24,001블록에서 0.29ms, 2,001블록에서 0.26ms로 DB 크기에 따라 늘지 않는다. 10,000블록의 블록당 비용 증가는 10/10에 확인한 cache 크기 때문이며, 필요하면 `database.cache_mb`로 줄인다 |
| kill 시험 (10/10 확인) | 결함 두 가지를 주입해 둘 다 실패하는 것을 확인했다. 블록 commit을 두 batch로 나누면 데이터 120건이 빠지고, 재시작 뒤 주소 sequence를 복원하지 않으면 `/index/addr/`·`/index/balance/` 키가 덮어써진다. 같은 높이가 이어진 원인: 재시작 뒤 API가 답한 때부터 첫 블록 색인까지 30~45ms가 걸리는데(부하 없을 때 측정), kill 전 대기가 20~420ms에서 무작위라 짧은 대기가 이어지면 진행 없이 kill된다. 시작 경로가 멈추는 것은 아니다 |
| SLO | 한 호스트에서만 쟀다(R5-5). 부하 체인은 10/12부터 V2 pair, V3 pool, 선물 주문장 시장(시장가 주문, 같은 트랜잭션에서 만든 지정가 주문 두 개의 체결)에 거래를 번갈아 보낸다(`testchain.BuildDEXLoadMixed`). V3 거래는 시작 가격에 머물러 tick을 넘지 않는다. indexer는 swap 이벤트의 가격·유동성·tick을 그대로 저장하므로 tick을 넘어도 하는 일이 같다. end-to-end p99는 V2만 쓸 때와 같은 범위다(약 100~120ms). 읽지 않는 구독자의 영향(20% 이내)은 10/12부터 두 조건을 번갈아 두 번씩 재고 p99 중앙값끼리 비교한다. 한 번의 p99는 사실상 60블록 중 가장 느린 블록 하나로 정해져, 아무것도 바꾸지 않아도 두 실행이 15% 정도 차이 난다. 그래서 한 번씩만 재 비교하면 main에서도 7번 중 1번 실패했다(같은 날 이 branch는 7번 중 2번). 가끔 한 블록이 170~240ms 걸리는 일이 V2만 쓰는 main에서도 있었다(원인 미확인). 시험의 구독 queue는 10/12부터 운영 기본값(16384)을 쓴다. 바꾸기 전에 두 값으로 쟀다. end-to-end p99는 1024일 때 87ms(읽지 않는 구독자 있을 때 96ms), 16384일 때 89ms(94ms)로 같았다. 이 부하(60블록 × 10건)에서는 1024로도 queue가 차지 않아, 읽지 않는 구독자가 끊긴 적이 없다(0/10). 그래서 SLO 시험은 queue overflow 경로를 시험하지 않는다(그 경로는 `pkg/api/graphql/subscription_engine_test.go`가 시험한다) |
| DEX 실제 컨트랙트 (10/10 해소, 선물 주문장) | 시험은 실제 이벤트 시그니처로 만든 로그를 쓴다. 시그니처, indexed 인자 수, 읽는 data word 수, reconcile이 부르는 view와 `getOrder` word 위치를 `pkg/features/dex/testdata/venues`에 고정한 컨트랙트 ABI와 대조하는 시험을 더했다(`TestVenueEventsMatchContractABIs`, `TestReconcileCallsMatchContractABIs`, `TestGetOrderLayoutMatchesContractABI`). poc-contract의 perpetual을 로컬 anvil에 배포해 낸 로그의 topic과 `getOrder` word 위치가 디코더와 같은 것을 확인했다. 10/10에는 indexer 프로세스로도 확인했다. poc-contract `5623967`의 perpetual을 로컬 anvil에 배포했다(`DeployPerpetual`과 같은 순서). 그다음 시장 하나, 지정가 주문 두 개(매수 10, 매도 4)를 만들고 `matchOrders` 한 번을 실행했다. indexer는 `dex.pools`·`dex.trades`·`dex.orderbook`을 켜고 RPC로 색인했다. 결과: 시장 1개, 체결 1건(가격 50000e18, base 4e18, quote 200000e18, taker 매도), 주문 상태(filled, partially_filled), 호가창 매수 6e18, `getOrder` 대조 `inSync`. chainbench StableNet이 아니라 anvil에서 한 이유: 디코딩과 대조는 체인 규칙(WBFT hash, 수수료)에 기대지 않는다. RPC로 로그를 받는 경로는 live 노드 시험이 이미 StableNet에서 확인했다. Uniswap V2/V3 venue와 testnet 8283 배포본으로는 확인하지 않았다 |
| live 노드 시험 (10/10 확인) | 로컬 go-stablenet(Gstable v1.1.0, chainbench로 validator 4 + endpoint 1)에서 `TestLiveBalances`, `TestLiveStableNet`, `TestLiveStableNetIdentity`(fee delegation 0x16 트랜잭션 3건 포함), `TestLiveFailover`, `TestLiveRecordReplay`, `TestLiveHeadLatency`(newHeads p95 4ms), `TestLiveLoopRollsBackReorg`가 통과했다. 10/11에 `TestLiveNotificationLatency`를 더해 돌렸다: newHeads를 켜면 노드가 블록을 보인 때부터 fast 스트림 메시지까지 p99 6~6.9ms(30건, 60건), polling만이면 p99 51.5ms. `TestLiveEraSource`는 era1 파일이 없어 돌리지 않았다. chainbench가 만든 genesis에는 `applepieBlock`이 없어 fee delegation을 쓰려면 genesis에 `applepieBlock`, `bohoBlock`을 0으로 넣어야 했다(chainbench 쪽 문제) |
| indexer-frontend | GraphQL 문서 26개가 지금 스키마에서 동작하지 않는다(`known-invalid.txt`). REST로 옮기는 일도 frontend 쪽에 남아 있다(`docs/FRONTEND_MIGRATION.md`) |

### 3.1 10/12 후속 SLO 조사

같은 main `47b5363`에서 기본 `make test-slo`를 두 번 실행했다. 부하는 기존과 같다(구독자 1,000명, 블록당 10건, 60블록, 조건별 두 번씩 번갈아 측정).

| 실행 | 읽는 구독자만 있을 때 p99 중앙값 | 읽지 않는 구독자 1% 포함 p99 중앙값 | 결과 |
|---|---|---|---|
| 첫 실행 | 104.5ms | 125.4ms | 모든 구간의 p99는 250ms 이내. 영향 검사만 실패: 실제 125.396ms가 허용치 125.361ms를 0.035ms 초과 |
| 재실행 | 117.3ms | 90.0ms | 전체 통과(영향 -23%) |

두 기본 실행의 최대 지연은 각각 139.6ms, 156.0ms로 170~240ms 지연은 재현되지 않았다. `TestNotificationLatency`는 둘 다 통과했다(fast stream 노드 기준 p99 10.2ms, 10.9ms). 첫 실행 때 호스트 load average는 약 3, 재실행 시작 때는 약 13이었지만, 이 부하 변화가 지연 차이의 원인인지는 확인하지 않았다.

별도 임시 Go overlay로 블록별 최대 지연과 runtime trace를 수집했다(조건별 한 번, 총 120블록; 저장소의 시험·운영 코드는 변경하지 않음). 최대 지연은 116.4ms, 134.6ms였고, 단일 비교의 영향 검사는 +26%로 실패했다. 계측과 반복 횟수가 기본 시험과 다르므로 기본 SLO 통과 결과로 취급하지 않는다. trace의 GC stop-the-world 최대는 0.88ms였다. 가장 느린 134.6ms 블록에는 GC가 겹치지 않았고, 그 시간 구간의 고루틴 Runnable 대기 최대는 49.1ms였다. 해당 고루틴이 최종 수신 지연을 결정했는지는 확인하지 않았다. 재현되지 않은 170~240ms 지연의 원인은 계속 미확인으로 둔다. SLO 상한이나 20% 검사 기준은 바꾸지 않았다.

### 3.2 10/12 후속 DEX reorg 확인

main `47b5363`의 `TestDEXReorg`, `TestDEXReorgWithoutOutbox`를 `go test -race -count=200 -run '^TestDEXReorg' ./pkg/app`로 반복했고 통과했다(156.7초). 같은 시험을 `-cpu=1`(GOMAXPROCS=1)에서도 각각 200회 반복해 통과했다(196.6초). 두 조건에서 flake는 재현되지 않았으며, 재현되면 수정한다는 기존 방침은 유지한다.

### 3.3 10/12 후속 PostgreSQL ingest 측정

같은 main `47b5363`, macOS/Apple M2 Max, Go 1.26.8에서 `BenchmarkIngest/regular/atomic`(202블록, 거래 10,001건)을 Pebble, PostgreSQL 순서로 세 번씩 번갈아 쟀다(`-benchtime=3x -count=1`). 각 값은 한 실행에서 세 번 색인한 평균이다. PostgreSQL은 이 측정 전용 Docker 컨테이너의 `postgres:16-alpine`, `fsync=on`을 썼고 끝난 뒤 해당 컨테이너만 제거했다.

| 비교 | Pebble | PostgreSQL | PostgreSQL/Pebble |
|---|---|---|---|
| 1 | 1.534초 | 2.749초 | 1.79배 |
| 2 | 1.492초 | 3.005초 | 2.01배 |
| 3 | 1.506초 | 2.728초 | 1.81배 |

세 실행 평균의 중앙값은 Pebble 1.506초, PostgreSQL 2.749초(1.83배)다. 앞선 3.1~3.6초 기록과 측정 환경의 부하가 같다고 확인하지 않았으므로 코드 개선 효과로 해석하지 않는다. 같은 호스트의 이 부하에서 얻은 수치이며 운영 장비의 성능을 보장하지 않는다. PostgreSQL 목표 배수와 추가 구조 변경 여부는 아직 사용자 답을 기다린다.

### 3.4 10/13 공개 테스트넷 RPC와 full node 검증

사용자가 운영 노드는 full node이고 archive 노드는 없다고 답했고, 여러 테스트넷 RPC로 프레임워크의 체인 확장성을 검증하도록 승인했다. 아래 공개 RPC를 production `NewApp` 기본 기능, 기본 Pebble 저장소와 최근 시작 높이로 검증했다. 공개 RPC의 PostgreSQL 색인은 확인하지 않았다. 과거 상태 조회의 성공·실패를 관찰한 것이며, 공개 서비스의 실제 노드 구성이나 archive 여부를 확인한 것은 아니다.

| 체인 / 자동 선택 profile | RPC | 검증 블록 | 트랜잭션 / 로그 | 결과 |
|---|---|---|---|---|
| StableNet testnet 8283 / stablenet | `https://api.test.stablenet.network` | 22,168,449~22,168,451 (3개) | 0 / 0 | 시작·색인·API·replay 통과. genesis 상태 조회도 응답 |
| BSC testnet 97 / evm | `https://bsc-testnet-dataseed.bnbchain.org` | 136,431,487~136,431,494 (8개) | 18 / 74 | genesis 상태는 missing trie 오류, genesis 시작 거부. 최근 시작·색인·API·replay 통과 |
| Sepolia 11155111 / evm | `https://ethereum-sepolia-rpc.publicnode.com` | 11,901,639 (1개) | 157 / 287 | genesis 상태는 historical state not available 오류, genesis 시작 거부. 최근 시작·색인·API·replay 통과 |

검증 범위는 노드의 chain id와 profile 자동 선택, 상태 검사, 블록 해시·부모·높이·거래 수, 거래의 유형·송수신자·금액·위치, receipt의 gasUsed·status·해시, 로그의 주소·topics·data·거래 해시·index, GraphQL/JSON-RPC의 블록 해시, 기록을 오프라인 replay했을 때 전체 저장 키 공간의 일치다. `TestLiveRPCFramework`는 체인 목록을 JSON으로 받아 체인별 코드를 추가하지 않고 반복할 수 있다(`CLAUDE.md` Testing). 기본 시험·CI에서는 외부 RPC를 호출하지 않는다. 기존 `TestLiveBalances`도 상태 검사에 실제 최근 시작 높이를 전달하도록 고쳤고, genesis 상태가 없는 로컬 시험 노드로 회귀를 확인한다. 보존 기간이 항상 128블록이라는 가정은 제거했다.

**[중요][High] Sepolia에서 실제 호환성 결함을 발견했다.** 블록 11,901,474의 노드 해시가 기존 계산 해시와 달랐다. 이 헤더에는 현재 의존하는 geth v1.16.5의 Header에 없는 `blockAccessListHash`, `slotNumber`가 있었다. [Glamsterdam 테스트넷 공지](https://blog.ethereum.org/2026/09/17/glamsterdam-testnet-announcement)의 Sepolia 활성화 일정, [EIP-7928](https://eips.ethereum.org/EIPS/eip-7928), [EIP-7843](https://eips.ethereum.org/EIPS/eip-7843)의 헤더 필드와 일치한다. 두 필드를 chain profile에서 보존하고 canonical RLP 해시에 포함시켰다. 해시 검사나 시작 시 상태 검사를 끄지 않았다. 실제 헤더와 노드가 보고한 해시를 fixture로 고정해 JSON·RLP 입력 및 저장 codec round trip을 검증하고, 어느 필드를 제거해도 같은 해시로 받아들이지 않는 것을 확인한다. Go·geth 의존성과 SDK 모델은 그대로이며, 추가 필드는 typed model extension `evm.header`에 저장한다. 이 codec이 없는 이전 빌드는 해당 새 블록을 읽을 수 없다. 추가 필드를 가진 custom hash 체인은 자신의 profile에서 해시 규칙을 구현할 때까지 거부한다.

**확인하지 않은 것:** 모든 EVM fork의 실행·수수료·내부 전송을 포함한 native balance 정확성, BAL 자체의 실행 검증, 이번 StableNet 표본에서 fee delegation(거래가 없었음), 여러 체인을 한 프로세스에서 동시에 돌린 운영 구성, 공개 RPC의 전체 상태 보존 범위, 사용자 운영 설정·시작 높이·활성 기능. Python 요청에서 Sepolia HTTP 403이 관찰됐지만 같은 URL의 Go RPC와 curl은 응답했고 실제 검증도 통과했다. 따라서 403을 서비스 전체의 접근 불가로 해석하지 않는다.

변경 후 `gofmt -l`, `make build`, `make lint`, `make test-race`, `make test-postgres`, `make test-examples`, `make test-slo`가 모두 통과했다. 최근 시작 높이 회귀 시험은 `-race -count=200`과 `-race -cpu=1 -count=200` 각각 통과했다. SLO는 읽는 구독자만 있는 조건의 p99 중앙값 105.9ms, 읽지 않는 구독자 1%를 포함한 조건 106.8ms(+1%)로 기준 이내였고, fast stream 알림 p99는 노드 기준 14.2ms, fast path 기준 6.9ms였다. 시험 전용 PostgreSQL 컨테이너의 정리와 기존 컨테이너의 보존도 확인했다.

### 3.5 10/13 Arc·Stable 추가와 공개 RPC PostgreSQL 검증

사용자가 Arc·Stable 추가와 다음 작업 진행을 요청했다. Arc는 Circle의 USDC 네이티브 체인, Stable은 USDT0 네이티브 체인으로 해석해 테스트넷을 추가했다. 공식 [Arc 연결 문서](https://docs.arc.io/arc/references/connect-to-arc)와 [Stable 연결 문서](https://docs.stable.xyz/en/reference/connect/)의 RPC와 chain id를 사용했다. 기존 3.4절의 StableNet(8283)과 Stable(2201)은 각각 별도의 profile이다.

**[중요][High] Stable 합의 해시와 시스템 거래의 차이를 확인했다.** 블록 72,432,091의 해시는 Ethereum RLP 계산과 달랐다. Stable의 CometBFT 합의 구조 및 [Cosmos EVM의 RPCMarshalHeader/RPCMarshalBlock](https://github.com/cosmos/evm/blob/main/rpc/types/utils.go)은 합의 블록 해시를 Ethereum 형태 RPC에 싣는다. `stable` profile에서 이 RPC 합의 해시를 보존하며 거래의 blockHash가 그 해시와 같은지도 확인한다. 이 profile은 Ethereum RLP 블록 해시를 재계산하지 않으며 CometBFT 합의 증명을 검증하는 구현은 아니다. RLP에서 합의 해시를 복원할 수 없으므로 BinaryProfile을 구현하지 않는다. 다른 체인의 Ethereum 헤더 검사는 유지한다.

같은 블록에는 서명 v/r/s가 0인 type 2 시스템 거래가 있었다. [StableSystem 공식 문서](https://docs.stable.xyz/en/reference/system-transactions-api/)의 `notifySystemTxLogs()` selector `0xe8e983b7`, 대상 `0x9999`와 일치한다. 문서는 예약된 발신자를 address(1)로 설명하지만 이번 testnet 응답은 `0x8888…8888`을 보고했다. 배포 버전이 왜 다른지는 확인하지 않았다. 두 예약 발신자를 지원하되 시스템 호출의 유형·대상·selector·chain id·zero signature·zero fee/value·블록 높이와 nonce를 검사하고 Ethereum 거래 해시도 검증한다. 서명으로 sender를 복원할 수 없는 시스템 거래는 `Opaque`로 표시한다. 정상 사용자 거래는 sender 복원과 hash 검사를 유지하고 변조하면 실패한다. 실제 Stable 블록 fixture로 재현·저장 round trip·잘못된 시스템 거래 거부를 검증했다.

**[중요][High] Arc에서 기본 회계의 잔액 불일치를 발견해 수정했다.** 기존 Ethereum 회계로 66,904,957~66,904,959를 색인하면 16개 계정의 17개 블록 잔액 중 8개 계정에서 노드와 달랐다. [Arc USDC system events](https://docs.arc.io/arc/references/usdc-system-events)에 따라 18자리 네이티브 Transfer와 6자리 ERC-20 Transfer를 구분해야 하며, tx.value도 다시 더하면 중복이다. 또 표본에서 블록 생성자의 차이는 baseFee×gasUsed와 정확히 같았다(블록 66,904,957: 11,390,580,000,000,000). `arc` profile은 시스템 이벤트(Zero5 전 legacy authority 이벤트 포함)로 네이티브 이동을 읽고 execution gas fee 전액을 block.miner에 지급한다. 네이티브 emitter는 ERC-20 색인·토큰 알림에서 제외한다. 수정 후 66,906,252~66,906,254의 21개 계정, 23개 블록 잔액 대조가 통과했다. 최초 실패 블록의 원본과 별도로 조회한 12개 주소의 전후 잔액을 fixture로 고정해 회계 회귀를 검증했고, 18/6자리 중복 및 legacy mint/burn도 시험한다. 이것은 관찰한 testnet 표본의 검증이며 mainnet이나 모든 업그레이드의 증명은 아니다.

| 저장소 / 체인 | 블록 | 거래 / 로그 | 결과 |
|---|---|---|---|
| Pebble / Arc (전용 회계 추가 전 core 검증) | 66,904,261~66,904,263 | 15 / 52 | 색인·API·replay 통과 |
| Pebble / Stable | 72,436,516~72,436,518 | 3 / 0 | genesis 시작 거부, 최근 시작·색인·API·replay 통과. 거래는 시스템 호출 |
| PostgreSQL / StableNet | 22,179,019~22,179,021 | 0 / 0 | 색인·API·replay 통과 |
| PostgreSQL / BSC | 136,454,979~136,454,986 | 15 / 33 | genesis 시작 거부, 최근 시작·색인·API·replay 통과 |
| PostgreSQL / Sepolia | 11,902,519 | 172 / 442 | genesis 시작 거부, 최근 시작·색인·API·replay 통과 |
| PostgreSQL / Arc (전용 회계 추가 전 core 검증) | 66,904,542~66,904,544 | 16 / 44 | 색인·API·replay 통과 |
| PostgreSQL / Stable | 72,436,708~72,436,710 | 3 / 0 | genesis 시작 거부, 최근 시작·색인·API·replay 통과 |
| PostgreSQL / Arc (최종 arc profile) | 66,909,300~66,909,302 | 25 / 47 | 전용 회계 포함 색인·API·replay 통과 |
| PostgreSQL / Stable (최종 stable profile) | 72,440,074~72,440,076 | 3 / 0 | genesis 시작 거부, 최근 시작·색인·API·replay 통과 |

PostgreSQL로도 실제 공개 RPC를 색인하고 기록을 offline replay한 저장 내용이 일치하는지 확인했다. 각 저장소 안에서 기록/재생을 비교했으며, 서로 다른 시간에 고른 Pebble·PostgreSQL 범위끼리 같은 데이터라고 비교한 것은 아니다. 검증은 전용 임시 컨테이너만 사용했고 정리와 기존 컨테이너 보존을 확인했다. 재현 명령은 `scripts/test-postgres.sh ./pkg/app -v -timeout 15m -run '^TestLiveRPCFramework$' -args -live-rpc-targets=<absolute path to pkg/app/testdata/live-rpc-targets.json>`이다.

기존 generic EVM profile로 Arc를 색인한 DB에는 잘못된 native balance와 ERC-20으로 분류한 system transfer가 남을 수 있다. 이번 변경은 이미 쓴 데이터를 자동 수정하지 않는다. 해당 데이터가 있는 DB는 새 arc profile로 재색인해야 한다. 실제 운영 DB는 읽거나 변경하지 않았다.

최종 코드의 gofmt, build, lint, full race, PostgreSQL 전체 시험, 예제, SLO 검사가 모두 통과했다. 최종 race의 pkg/app은 318.2초, PostgreSQL pkg/app은 150.0초였다. SLO p99 중앙값은 readers only 105.7ms, non-readers 포함 101.6ms(-4%), fast stream p99는 node 기준 4.5ms, fast path 기준 0.8ms였다. 새 동시성 시험은 추가하지 않았다.

**확인하지 않은 것:** Arc·Stable mainnet, Arc의 Zero5 이전 전체 역사 구간의 실제 잔액(legacy 이벤트 디코딩은 로컬 시험으로 확인), Stable의 일반 사용자 거래가 포함된 실체인 표본(서명 검증은 로컬 시험으로 확인), Stable의 Cosmos bank·분배·보상·USDT0 fractional reconciliation을 포함한 전체 잔액 회계, CometBFT 합의 증명, 운영 설정·시작 높이, 여러 체인의 동시 운영. Stable 회계는 현재 generic Ethereum fallback이라 이 영역의 정확성을 보장하지 않는다. 새 profile을 쓴다고 이 미검증 영역까지 지원된다고 해석하지 않는다.

### 3.6 10/13 EVM 범위 결정과 다음 작업

사용자 결정: Stable은 Tendermint/CometBFT 기반이므로 제외하고 현재는 EVM 전용 프레임워크로 진행한다. StableNet과 Stable은 별개이며 StableNet 지원은 유지한다. `pkg/chains/stable`과 앱의 등록 import, 공개 RPC target을 제거한다. Stable의 전용 회계, 시스템 거래 확장, CometBFT 증명, Stable mainnet 검증은 현재 작업 목록에서 제외한다. 3.5절의 검증 기록은 과거 사실로 남긴다.

공통 코어는 체인 중립으로 유지한다. 체인별 디코딩·회계·native coin 규칙은 `pkg/chains/<chain>` profile 및 optional interface로, 체인별 기능·API는 feature/SDK/registry 확장으로 제공한다. 새로운 체인을 위해 공통 fetch/storage/API에 chain-id 조건문을 더하지 않는다.

| 추천 순서 | 작업 | 완료 기준 / 필요한 것 |
|---|---|---|
| 1 (완료 10/13) | 커스터마이징 계약과 예제 정리 | `docs/CHAIN_CUSTOMIZATION.md`, `examples/chainprofile` 및 기존 receipts SDK 예제. 새 공통 인터페이스를 추가하지 않음. 범위는 3.7절 |
| 2 | Arc 회계 표본 확대 및 저장소 대조 | 내부 전송·mint/burn·실패 거래·수수료가 포함된 실제 표본에서 노드 잔액과 비교. 같은 RPC 기록을 Pebble/PostgreSQL에 재생해 결과 대조 |
| 3 | EVM 멀티체인 동시 실행 검증 | StableNet·BSC·Sepolia·Arc의 DB·feature·API·알림 분리, RPC 장애와 재시작 검증. 순차 RPC 시험만으로 동시 운영이 검증됐다고 보지 않음 |
| 4 | full node 운영 설정 확정 | 운영 설정 경로·시작 높이·활성 기능·상태 보존 범위 필요. archive는 없으며, 최근 높이 시작/기능 비활성화/fallback 방침을 정해야 함 |
| 5 | 운영 성능 측정 | PostgreSQL 목표 배수 결정 및 여러 호스트 환경 필요. 현재 단일 호스트 중앙값 1.83배는 운영 목표가 아님 |

별도 자원이나 승인이 필요한 항목은 유지한다: 실제 export era1/노드 속도 비교, Arc mainnet 검증, frontend 이전(이 저장소에서는 수정 금지), 태그 발행·원격 branch 삭제·외부 배포. 간헐 SLO 지연은 원인 미확인이고 DEX reorg flake는 재현 시 수정한다. K2·값 형식 통일·perp 이름 등 기존 보류/하지 않음 결정은 다시 열지 않는다.

### 3.7 10/13 체인 커스터마이징 계약·예제

사용자가 다음 작업 진행을 요청해 3.6절의 첫 항목을 수행했다. `docs/CHAIN_CUSTOMIZATION.md`에 profile·회계·native coin·typed extension·binary·feature/SDK·API 등록의 책임과 검증 범위를 정리했다. profile wrapper의 선택 인터페이스 손실, 중복 id/priority, 등록되지 않은 adapter 이름의 자동 감지, full node 상태 조건을 설명한다. `pkg/chains`는 SDK 호환성 약속 밖이며 새 SDK 면이나 공통 코어 변경을 추가하지 않았다.

별도 모듈 `examples/chainprofile`의 `pkg/chains/projectarc`는 `arc.New()`의 구체적 EVM profile을 embed해 `project-arc` id/alias와 testnet 감지를 등록한다. priority 200으로 testnet만 커스터마이징하고 Arc mainnet/generic EVM 선택은 유지한다. native accounting/native coin/BinaryProfile 선택 인터페이스를 보존한다. 로컬 시험은 선택/alias, native 수수료와 value 규칙의 위임, 변조된 블록/거래 해시·sender 거부를 확인한다. SDK feature와 API 구현 예제는 기존 `examples/receipts`를 연결한다.

최종 gofmt/build/root lint/전체 race/PostgreSQL/예제 검증은 모두 통과했다(전체 race pkg/app 423.5초, PostgreSQL app 150.2초). 별도 예제 모듈의 race/lint와 실제 바이너리 빌드도 통과했다. 첫 전체 실행의 구조 검사 실패는 예제 내부에서 해결했고 검사 코드나 허용 예산을 바꾸지 않았다. 새 동시성 시험이나 SLO/알림/부하 경로 변경은 없다.

이번 예제는 공개 RPC를 다시 색인하거나 새 회계 알고리즘·고유 거래 타입·운영 기능을 추가하지 않는다. 실제 바이너리의 빌드는 확인하지만 실노드 실행, JSON/binary 동일 데이터 대조, mainnet, 전체 회계·동시 운영까지 새로 검증한 것으로 해석하지 않는다. 다음 작업은 3.6절의 Arc 회계 표본 확대와 같은 RPC 기록의 Pebble/PostgreSQL 대조다.

### 3.8 10/14 Arc 회계 표본 확대·동일 RPC 저장소 대조

R01을 수행했다. 운영 회계·저장소·API·SDK 코드는 변경하지 않았다. Arc 전용 표본과 회계 회귀는 `pkg/chains/arc/testdata`와 `accounting_test.go`에, profile과 무관한 기록/재생 비교 도구는 `pkg/app/rpc_parity_test.go`에 둔다. 공통 코어에 체인 조건문이나 새 인터페이스를 더하지 않는다.

Arc testnet RPC `https://rpc.testnet.arc.io`(5042002)에서 66,917,992 / 66,918,549 / 66,918,552 / 66,918,564의 block·receipt와 각 블록 전후 `eth_getBalance` 응답을 독립적으로 수집했다. 각각 19 / 13 / 10 / 13개 주소, 총 55개 계정/블록 델타가 회계 출력과 일치했다. mint 2건, burn 1건, 실패 거래의 gas 지불, miner의 base fee 포함 전체 execution fee, 최상위 거래의 from/to와 다른 native 이동이 포함된다. balance feature가 잘못된 잔액을 노드 값으로 reset하기 **전**의 델타를 검증한다. [표본 설명](../../pkg/chains/arc/testdata/README.md)에 범위와 수집 방법이 있다.

별도의 연속 구간 66,919,625~66,919,724(100블록)을 production `NewApp` 기본 기능으로 색인했다. 600개 거래(실패 101개), 1,436개 로그, native transfer 430건과 burn 1건을 포함한다. 블록·거래·receipt·로그·GraphQL/JSON-RPC 블록 해시를 확인했다. 461개 계정의 980개 블록별 마지막 잔액이 같은 높이의 노드 상태와 일치했다. 이 구간에는 mint가 없으며 위 고정 표본에서 별도로 검증했다.

한 번 확보한 RPC 응답을 새 Pebble과 PostgreSQL DB에 각각 재생해 25,118개 port 조회가 일치했고, 어느 재생도 미기록 RPC 호출을 요구하지 않았다. PostgreSQL은 이 작업만의 임시 컨테이너(`fsync=on`)를 사용했다. 비교는 model codec의 블록·거래·receipt·로그, native balance/history, 주소·transfer·holder·AA·조회 통계 등 helper의 조회 결과다. 서로 다른 물리적 keyspace나 outbox/undo 내부 형식을 동일하다고 비교하지 않는다. 범위 없는 `GetTokenBalances`는 genesis부터 순회하므로 최근 높이 비교에서 제외한다. Transfer 목록은 저장소별 최대 페이지 크기 차이(Pebble 100)에 기대지 않고 커서로 끝까지 읽는다. 다른 list 조회는 helper의 지정된 한 페이지/최근 목록 범위다.

처음에는 공개 RPC의 일시적인 응답 오류와 429 때문에 실행이 중단됐다. 색인뿐 아니라 블록 식별자·잔액 확인도 recorder의 5 requests/s 제한을 공유하게 했다. 첫 재생 비교는 상위 100개의 동률 경계와 최대 transfer 페이지 크기 차이로 실패했다. 통계의 전체 순위와 커서로 읽은 transfer 내용을 대조하도록 **시험 도구**를 수정한 뒤, 확보한 동일 RPC 기록으로 오프라인 재실행해 통과했다. 운영 API의 정렬·페이지 크기는 변경하지 않았다.

재실행은 `INDEXER_TEST_POSTGRES`가 지정된 테스트 DB에서 수행한다. live 표본: `go test -count=1 -v -timeout 15m -run '^TestLiveRPCStorageParity$' ./pkg/app -args -live-rpc-targets=testdata/live-rpc-accounting-targets.json -rpc-parity-balances`. 보유한 연속 기록: `TestRPCArchiveStorageParity`에 `-rpc-parity-archive`와 필요하면 `-rpc-parity-balances`를 준다. archive manifest는 100블록 이내의 연속 색인 구간이어야 하며, 다른 높이의 context 블록이 섞인 기록은 이 입력으로 사용하지 않는다. 일반 실행·CI는 live/보유 archive 플래그가 없으면 외부 호출 없이 건너뛴다. 로컬 `TestRecordedRPCStorageParity`는 외부 노드 없이 같은 비교 경로를 검사한다.

**확인하지 않은 것:** 전체 역사·Zero5 이전 실노드 회계, SELFDESTRUCT와 모든 fork 규칙, mainnet, 여러 체인의 동시 운영, 실제 운영 설정과 기존 Arc DB, 공개 RPC 노드의 물리적 full/archive 구성과 전체 상태 보존 범위. 표본의 endpoint 차이를 실행 trace로 입증한 것은 아니다. 체인 트랜잭션 전송·외부 배포·운영 DB 변경을 하지 않았다.

최종 gofmt/build/lint/전체 race/기본 PostgreSQL 전체 시험/예제가 모두 통과했다. 새 로컬 기록·재생 경로의 별도 race 시험도 통과했다. 동시 실행 중에는 PostgreSQL 전체 시험의 10분 제한과 기존 1,500블록 backfill 시험의 30초 대기 실패, 전체 race의 앱 시험 실패가 관찰됐다. 해당 race 실패의 세부 원인은 확인하지 못했다. 최종 필수 시험은 각각 단독으로 재실행해 통과했으며 시험 판정 기준이나 운영 구현은 바꾸지 않았다. 새 동시성 시험은 추가하지 않았고 SLO·알림·부하 경로 변경이 없어 추가 SLO 실행은 하지 않았다.

### 3.9 10/14 공개 RPC 멀티체인 동시 실행

R02의 검증이다. 기존 `testdata/live-rpc-targets.json`의 네 공개 RPC를 production `NewApp` 한 프로세스에 연결했다. 체인 데이터·합의 형식·회계는 기존 profile을 자동 선택했으며 공통 코어에 chain-id 분기를 더하지 않았다. 로컬 제어 proxy는 광고하는 head만 단계적으로 공개하고 한 체인에 HTTP 503을 주입했다. 블록·receipt·상태는 실제 공개 노드에서 읽었고, proxy마다 5 requests/s를 적용했다. 체인에 거래를 보내거나 외부 배포·운영 DB 변경을 하지 않았다.

| 테스트넷 / profile | 최근 구간 | 거래 | 로그 |
|---|---|---:|---:|
| StableNet 8283 / `stablenet` | 22,253,852~22,253,854 | 0 | 0 |
| BSC 97 / `evm` | 136,621,269~136,621,276 | 11 | 28 |
| Sepolia 11155111 / `evm` | 11,908,739~11,908,741 | 346 | 596 |
| Arc 5042002 / `arc` | 67,051,866~67,051,868 | 15 | 30 |

총 17블록·372거래·654로그를 원본 RPC의 hash·위치·receipt·로그와 대조했다. 각 DB에 다른 체인의 블록 hash가 없고, `/chains/<id>/`의 GraphQL/JSON-RPC/REST가 그 체인의 블록을 반환함을 확인했다. production 기능 기본값에서 첫 체인의 `token.transfers`만 끄는 시험용 override를 적용했고 다른 체인의 기능은 유지됐다. 체인별로 같은 API key의 독립된 STREAM 설정을 만들고 durable BLOCK 알림의 설정 ID·chain ID·번호·hash를 대조했다. 거래가 없는 StableNet 구간도 블록 알림으로 검사했다.

첫 블록을 색인한 뒤 StableNet proxy를 차단한 동안 BSC·Sepolia·Arc가 마지막에서 하나 전 블록까지 진행했다. StableNet의 health는 실패를 보고하고 저장 높이는 그대로였으며, 복구 후 다른 체인과 함께 따라잡았다. 전체 프로세스를 종료하고 같은 DB·알림 설정으로 다시 시작해 stream을 재연결한 뒤 마지막 블록을 공개했다. 저장 설정·profile·기능 override를 유지했고 마지막 알림은 자기 체인의 다음 블록이었다. 이는 한 프로세스의 동시 파이프라인 검증이며 이전 순차 시험을 재사용한 결과가 아니다.

별도로 자동 재시작 코드가 성공한 `Start`에 사용한 컨텍스트를 즉시 취소하는 오류를 로컬 회귀 시험으로 재현했다. 새 파이프라인의 수명을 manager의 컨텍스트에 연결하고, restart delay 계산의 오류 시각을 lock 아래에서 읽게 했다. RPC 호출의 설정된 timeout은 유지한다. 공개 RPC의 일시적인 503은 기존 fetch loop의 재시도로 복구했고, 이 fatal pipeline 자동 재시작 회귀와 구별한다.

초기 시험은 fast BLOCK 설정으로 알림을 기다려 실패했다. 기존 fast 경로는 거래·로그를 지원하므로 durable BLOCK 경로를 사용했다. 첫 공개 실행은 다른 체인의 색인을 기다리며 stream을 읽지 않아 ping/pong이 멈추고 연결이 끊겼다. 네 stream을 계속 읽는 클라이언트로 보완했다. 다음 실행은 BSC의 프로세스 재시작 준비 대기에서 시간 초과했다. 당시 실제 오류의 원인은 확정하지 못했고, 진단을 추가한 후 다른 최근 구간으로 재실행해 모든 검사에 통과했다. 노드의 상태 보존 문제로 단정하거나 자동 재시작 수정이 그 시간 초과를 해결했다고 주장하지 않는다.

재실행: `go test -count=1 -v -timeout 12m -run '^TestLiveRPCMultiChainIsolation$' ./pkg/app -args -live-rpc-targets=testdata/live-rpc-targets.json`. 최소 두 target, 체인당 최소 3·최대 100 최근 블록이다. `TestControlledRPCMultiChainIsolation`은 서로 다른 ID/hash의 로컬 EVM 노드 네 개로 같은 분리·장애·재시작 시험을 수행하며 일반 시험과 PostgreSQL 시험에 포함된다. 공개 RPC 검증의 저장소는 Pebble이다.

최종 gofmt/build/lint/전체 race/PostgreSQL 전체 시험/예제/SLO가 통과했다. 새 자동 재시작·네 체인 동시성 시험은 각각 race 200회와 단일 CPU 200회에 통과했다. PostgreSQL의 앱 통합 시험에서도 네 로컬 체인의 같은 분리·장애·재시작 검사가 통과했다. 이 작업의 임시 PostgreSQL 컨테이너는 정리했다. 단일 호스트 SLO는 fast stream p99 노드 기준 12.8ms·fast path 기준 4.2ms, 1,000 구독자 부하의 반복별 p99 중앙값은 읽는 구독자만 145.8ms·1% 비독자 포함 96.8ms였다. 이를 운영 환경의 목표나 보장으로 바꾸지 않는다.

**확인하지 않은 것:** 장기간의 움직이는 head 동시 운영, 네 공개 체인의 PostgreSQL 동시 실행, 운영 설정·실제 보존 범위·기존 Arc DB, 여러 호스트 SLO, mainnet·전체 역사·모든 fork 규칙과 native 잔액. 공개 노드의 물리적 full/archive 구성은 확인하지 않았다. R03의 full node 운영 설정 결정과 R04의 운영 측정은 남는다.

### 3.10 10/14 로컬 full node 설정 복구·명시적 profile 선택

R03의 로컬 설정을 점검했다. 실제 `config.yaml`이 없고 예제 설정만 남아 있어, 사용자 선택에 따라 StableNet·BSC·Sepolia·Arc 테스트넷을 한 App에서 실행하는 로컬 전용 `config.yaml`을 재구성했다. 이 파일은 저장소의 기존 ignore 규칙을 따른다. 원격 운영 설정을 복원했다는 뜻은 아니다.

새 Pebble DB 경로는 `./data/multichain-testnets`, API는 `127.0.0.1:8088`이다. 기존 `data-devnet`과 이전 generic EVM으로 색인한 Arc DB는 재사용하지 않았다. 각 공개 노드의 chain id와 최근 시작 높이 직전의 zero-address `eth_getBalance` 응답을 확인하고, balance.native/token.metadata는 켠 채 allow_pruned_state=false를 유지했다. 체인당 5 requests/s·workers=2·poll_interval=2s는 공개 RPC를 위한 보수적인 로컬 설정이며 운영 처리량 목표가 아니다. 알림 대상과 API 키는 구성하지 않았고 notifications/verifier는 껐다.

07:41:48 KST에 StableNet 22,258,605, BSC 136,631,835, Sepolia 11,909,133, Arc 67,061,244를 시작 높이로 선택했다. 네 노드에서 그 직전 상태 조회가 성공했다. BSC/Sepolia의 genesis 상태 조회는 실패했고 StableNet/Arc에서는 성공했다. 이 표본만으로 물리적 full/archive 구성, 전체 상태 보존 범위나 모든 계정의 상태 가용성을 확정하지 않는다. 별도 임시 DB와 임시 localhost 포트에서 production App을 실행해 네 시작 블록의 색인·체인별 GraphQL 높이/해시 조회와 /chains 목록을 확인한 뒤 앱과 임시 DB를 정리했다. 실제 로컬 DB 경로에 상시 운영을 시작하거나 외부 배포하지 않았다.

설정 재구성 중 `adapter_type: stablenet`이 시작 전에 거부됐다. 멀티체인 validator가 auto/evm/stableone/anvil만 허용하는 별도 목록을 갖고 있어, 문서에 안내한 arc/stablenet와 외부 profile의 명시적 선택이 막혀 있었다. 첫 로컬 검증은 auto로 통과했다. 후속 수정에서는 `chains.Lookup`으로 등록된 id/alias를 검증하며 빈 값·auto·anvil의 기존 선택과 미등록 이름 거부를 유지한다. production 멀티체인 패키지의 체인 고유 문자열 두 개를 없애고 구조 검사 예산도 그만큼 줄였다. 체인 profile import나 chain-id 분기를 공통 코어에 추가하지 않았다.

회귀 시험은 수정 전 arc/stablenet/외부 profile/alias 거부를 재현하고, 수정 후 등록된 이름 수용과 기존 미등록 이름 거부를 확인한다. 기존 두 체인 통합 시험은 명시적 evm/stablenet 선택으로 색인·DB 분리·GraphQL/JSON-RPC/REST 조회와 단일 체인 색인 결과 일치를 검사한다. profile 선택 이후의 회계·feature·알림·SLO 경로는 변경하지 않았다.

후속 공개 검증은 수정한 실제 실행 파일과 명시적 profile 설정으로 수행했다. 08:09:13 KST에 새 시작 직전 상태를 확인하고 StableNet 22,260,252 / BSC 136,635,495 / Sepolia 11,909,271 / Arc 67,064,495에서 시작했다. 별도 임시 DB·포트에서 네 체인의 시작 블록 hash를 GraphQL로 읽고 종료한 뒤, 같은 DB로 재시작해 시작 블록 hash가 유지되고 재시작 후 처음 관측한 저장 높이보다 새 색인이 진행하는 것을 확인했다. 재시작 때의 관측 높이는 각각 22,260,265 / 136,635,594 / 11,909,274 / 67,064,532다. 이는 시작·저장·재시작 진행 검사이며 전체 잔액 대조나 운영 SLO 측정이 아니다. 시험 앱과 임시 DB는 종료·정리했다. 로컬 config.yaml의 최근 높이와 명시적 profile도 갱신했다.

**실제 운영 문제:** 재시작 실행의 BSC 로그에 새 계정의 이전 상태 소실로 0에서 시작하는 경고 66건과 잔액 보정 상태 소실 경고 73건이 나타났다(총 66개 주소). `balance.native.allow_pruned_state: false`였으나 현재 설정은 시작 검사에만 적용된다. 움직이는 head 처리 중 필요한 상태가 사라져도 이 경로는 경고 후 계속 진행한다. 원인을 고정된 보존 블록 수나 단일 부하 요인으로 단정하지 않는다. 네 체인의 블록/재시작 검사가 통과했다는 사실을 잔액 정확성 보장으로 해석하지 않는다. 처리 중 블록 실패·재시도 또는 상태 읽기 기능 비활성화의 사용자 운영 방침은 아직 정하지 않았다. 이 동작을 명시적 profile 수정에서 바꾸지 않았다.

필수 로컬 검증은 gofmt/build/lint(0 issues)/전체 race(pkg/app 421.053초)/PostgreSQL(adapter 44.826초, app 184.418초)/예제 모두 통과했다. 새 동시성 시험은 추가하지 않았고 SLO·알림·부하 실행 경로 변경도 없어 추가 SLO 시험은 실행하지 않았다.

첫 실행 전에는 시작 직전 상태를 다시 확인해야 한다. 기존 DB 재시작에서는 최신 저장 높이와 새 기능 backfill이 요구하는 상태를 확인하고, 시작 높이를 임의로 올려 데이터 누락을 만들지 않는다. 오래 중단했거나 기능을 추가해 필요한 상태가 사라졌다면 기존 startup refusal을 유지한다. 재색인/기능 비활성화 등 운영 결정을 정하지 않고 fallback을 켜지 않는다. 최근 시작은 이전 블록의 거래·계정·토큰 색인 완전성을 제공하지 않는다.

**확인하지 않은 것:** 실제 운영 호스트의 보존 범위·허용 중단 시간, 움직이는 head의 장기 동시 운영, 기존 Arc 운영 DB, 공개 네 체인의 PostgreSQL 동시 운영, 여러 호스트 SLO와 PostgreSQL 운영 성능 목표. R03 전체 운영 검증과 R04는 남는다.

### 3.11 10/14 처리 중 상태 소실 거부·full node 성능 과제

사용자는 3.10에서 관측한 상태 소실에 대해 **해당 블록 실패·같은 높이 재시도**를 선택했다. `allow_pruned_state: false`를 시작 검사뿐 아니라 처리 중에도 적용한다. `balance.native`의 genesis/새 계정 초기 잔액과 음수 델타 보정에 필요한 조회가 실패하면 error를 반환한다. `token.metadata`는 필요한 과거 상태가 없으면 latest로 대체하지 않고 error를 반환하며, code 조회 오류로 토큰을 조용히 누락하지 않는다. 계약의 정상적인 호출 revert는 토큰 판별 응답으로 유지한다. 기존 근사 처리는 `true`를 명시한 경우에만 허용한다.

writer의 기존 블록 트랜잭션을 이용하므로 실패한 블록·거래·영수증·기능 데이터·cursor·outbox는 commit되지 않는다. live loop는 성공하기 전 다음 높이로 이동하지 않는다. 동작 검증은 startup 성공 뒤 상태를 소실시키는 로컬 체인/실제 RPC proxy로 수행했다. genesis 초기화·새 계정·음수 델타 보정·토큰 생성 높이에서 오류와 원자적 취소를 확인하고, 상태 응답을 복구한 뒤 **같은 App·같은 높이**를 다시 처리해 정확한 잔액/메타데이터와 cursor 진행을 확인한다. 토큰 판별/메타데이터 조회 도중 상태가 사라지는 경우와 명시적 fallback도 검사한다. 새 시험은 수정 전 누락된 거부 동작을 재현했다.

필수 로컬 검증은 gofmt/build/lint(0 issues)/전체 race(pkg/app 414.710초)/PostgreSQL(adapter 43.284초, app 170.099초)/예제 모두 통과했다. 새 runtime 회귀 시험은 Pebble에서 `-race -count=200`(165.598초)과 `GOMAXPROCS=1 -race -count=200`(188.067초) 모두 통과했다. PostgreSQL에서도 전체 App 시험에 포함해 취소·동일 높이 재시도를 확인했다. SLO·알림·부하 실행 경로를 바꾸지 않아 추가 SLO 시험은 실행하지 않았다. 임시 PostgreSQL 컨테이너는 정리됐고 기존 네 컨테이너는 유지됐다.

이는 영구히 지워진 상태를 되살리는 수정이 아니다. 해당 RPC가 다시 필요한 상태를 제공하지 못하면 진행은 계속 멈춘다. 실제 노드의 보존 범위·장기 동시 운영·운영 잔액 정확성 전체 대조는 확인하지 않았다. 3.10의 BSC 경고 기록은 당시 구현의 증거로 남기며, 최근 시작이나 제한된 lifecycle 검사를 잔액 정확성 보장으로 해석하지 않는다.

사용자가 제안한 **처리 속도 향상·수집/처리 고루틴 분리·채널 통신**을 R04의 구체적인 과제로 기록했다. 현재 `scheduler.go`에도 병렬 블록/영수증 수집, bounded results channel, 순서 복원과 하나의 writer가 있다. 처리 단계 안의 feature 상태 RPC 대기는 추가 병렬 준비의 후보이며 아직 병목 측정 결과는 아니다. 체인별 단계 지연·RPC 제한 대기·queue 메모리·head 지연·실제 상태 가용성을 먼저 측정한다. 준비 결과를 chain id/height/hash에 묶고 소유권을 채널로 넘기며 reorg 폐기·backpressure를 유지한다. 잔액 의존성과 원자적 commit을 지키기 위해 체인별 writer는 높이 순서를 유지한다. 이 수정에서 전체 파이프라인 변경이나 처리량 향상을 구현·측정했다고 주장하지 않는다.

### 3.12 10/14 상태 준비 고루틴·채널 파이프라인

사용자가 권장 구조의 실행을 승인했다. `state_prefetch`는 수집 worker에서 초기 잔액/토큰 메타데이터를 준비하고, 기존 크기 제한 채널·순서 복원·체인별 writer로 전달한다. 기본은 opt-in이며 체인별로 덮어쓸 수 있다. `state_workers`의 permit은 한 pipeline의 준비 작업이 공유한다. 부모/생성 block hash로 상태를 읽으며 writer에서 canonicality·DB 기록을 재확인한다. 음수 잔액 보정·backfill·미지원 custom handler는 기존 순차 경로를 사용한다. 기존 SDK 인터페이스는 유지하고 선택적 준비/hash reader API와 snapshot을 추가했다. StableNet epoch 조회는 준비 block의 조상 hash 연결을 검증해 다른 fork의 헤더로 cache를 채우지 않는다.

준비 값은 전달 후 수정하지 않는다. 범위 취소/reorg/실패 시 작업 고루틴과 channel drainer를 모두 기다리고 결과를 폐기한다. 최대 `2 × workers` 블록이 진행 중이며 `state_max_bytes`는 블록별 전달 payload 계산값만 제한한다. 기본 블록/영수증과 임시 할당을 포함한 heap 상한은 아니다. 상태가 없거나 hash RPC가 미지원이면 잘못된 0/latest 대체를 저장하지 않는다(strict 기본값). 노드가 큐 대기 중 이미 조회한 상태를 pruning한 뒤에도 준비 값으로 같은 블록을 정확하게 저장하는 시험을 추가했다.

통제된 비교는 Apple M2 Max/darwin arm64/Go 1.26.8/Pebble에서 `BenchmarkStatePreparation -benchtime=3x -count=1`로 수행했다. 동일한 25블록(24블록은 각 8개 새 수신자를 건드리는 native 거래), 수집 workers=2·state_workers=4·양쪽 pipeline_metrics=true이며 DB/노드 생성과 App 시작/종료는 시간에서 제외했다. 아래 값은 3회 평균이며 RPC 수는 논리 요청 수, writer p95는 각 반복의 최근 window p95 평균이다. peak payload는 반복 중 최대 계산값이다. B/op는 **누적 할당량**이며 peak heap이 아니다.

| 입력 | 기존/준비 시간 | 기존/준비 RPC | 기존/준비 writer p95 | 기존/준비 할당량 |
|---|---|---|---|---|
| localhost, 추가 지연 없음 | 0.284 / 0.164초 | 245 / 272 | 26.33 / 13.27ms | 28.94 / 34.80 MB/op |
| 각 RPC 왕복 5ms 추가, quota 없음 | 1.435 / 0.322초 | 245 / 272 | 69.57 / 19.96ms | 27.38 / 35.60 MB/op |
| 왕복 5ms, 50 requests/s 제한 | 4.469 / 4.942초 | 245 / 272 | 202.8 / 149.4ms | 26.88 / 30.78 MB/op |

준비 payload 최고값은 각각 10,206 / 7,416 / 5,050 bytes다. RTT 입력은 약 4.45배 개선했지만 요청 제한 입력은 약 10.6% 느렸다. 추가 canonical 검증·speculative 초기 잔액 조회와 메모리 할당 비용이 있다. 초기 구현의 준비 직후 header 검증은 hash 고정 조회와 중복돼 제거했으며 writer에서 재확인한다. 실제 체인·RPC·PostgreSQL·여러 호스트 SLO에 이 비율을 적용하지 않는다. 로컬 공개 RPC의 초당 5회 cap을 임의로 올리지 않았다.

공개 RPC의 hash 지원은 production Go client(5 requests/s)에서 확인했다. StableNet 22,268,836 / BSC 136,654,564 / Sepolia 11,909,989 / Arc 67,081,451의 부모 hash 잔액 및 head hash 코드/호출 조회가 모두 성공했다. 조회 시간에는 limiter 대기도 포함한다. zero-address 표본이므로 일반 계정의 상태 보존 범위·장기 운영 정확성을 보장하지 않는다. Python 기본 요청은 Sepolia/Arc에서 403이었으나 production client 조회는 성공했다.

실제 바이너리와 `state_prefetch: true`/state_workers=4/수집 workers=2/5 requests/s/strict 기능 설정으로 네 공개 체인을 임시 DB·localhost 포트에서 동시에 실행했다. 시작 높이는 StableNet 22,269,858 / BSC 136,656,840 / Sepolia 11,910,071 / Arc 67,083,465였다. 같은 DB로 재시작해 저장된 시작 hash 유지와 새 진행을 확인했다. 재시작에서 관측한 높이는 각각 22,269,869 / 136,656,902 / 11,910,073 / 67,083,486이었다. 초기 0 대체·latest 메타데이터 대체·보정 상태 소실 경고는 두 실행 합계 모두 0건이었다. 이 검사는 lifecycle·블록 hash 검사이며 일반 계정의 잔액 전체 대조나 장기 SLO가 아니다. 임시 앱/DB는 정리했고 실제 새 DB 경로는 생성하지 않았다. 로컬 config.yaml의 최근 시작 높이는 재확인 후 갱신했으며 실제 운영 시작 전에는 다시 확인해야 한다.

새 준비 회귀 시험은 일반 race 200회(feature 1.622초, fetch 78.587초, app 615.625초)와 단일 CPU race 200회(feature 1.684초, fetch 50.539초, app 675.676초)를 통과했다. 기존 네 체인 장애/stream/같은 DB 재시작 통합 시험은 실제 native 거래와 준비 옵션을 사용하도록 확장했으며 focused race도 통과했다. 알림/SLO 시험은 `INDEXER_LOAD_STATE_PREFETCH=1`로 준비 경로의 영향을 추가로 측정할 수 있다.

최종 필수 검증은 gofmt/build/lint(0 issues)/전체 race(app 420.418초)/PostgreSQL(adapter 44.743초, app 190.377초)/예제 모두 통과했다. 계측 분리 뒤의 채널/준비 시험도 일반 race 200회(feature 1.611초, fetch 56.190초) 및 단일 CPU race 200회(feature 1.497초, fetch 55.380초)를 통과했다.

**SLO 실패와 비교:** 초기 기본 설정 두 실행에서 non-reader 상대 지연은 110.9/138.8ms(+25%), 93.9/123.9ms(+32%)로 20% 기준을 초과했다. 절대 p99는 기준 안이었다. 변경 전 main e80ce1b를 별도 임시 archive checkout에서 같은 입력으로 실행한 결과는 111.0/104.5ms(-6%)였다. 기존 경로의 비용을 보존하기 위해 새 계측은 state_prefetch 또는 명시적 pipeline_metrics에서만 실행하도록 분리했다. 그 뒤 같은 기준의 기본 SLO는 172.3/108.0ms(-37%), 옵션 true SLO는 107.0/101.8ms(-5%)로 통과했다. 기준·입력·반복 횟수는 완화하지 않았다. 실행 간 변동이 커서 최초 실패의 유일한 원인을 확정하거나 간헐 SLO 문제 전체를 해소했다고 주장하지 않는다. R07에는 이 비율 변동도 남긴다.

두 최종 알림 검사의 fast stream p99는 기본 10.6ms, 준비 true 12.2ms였다. 준비는 fast block tap 앞에 있으므로 cold 상태 RPC 대기가 provisional 알림에도 더해질 수 있다. localhost 시험 결과를 원격 RPC 알림 지연 보장으로 해석하지 않는다. queue/outbox 원자성과 데이터 정확성 검증, 제한된 네 공개 체인 lifecycle 확인과 실제 운영 SLO를 구분한다.


### 3.13 10/14 네 공개 테스트넷 5분 운영 창 관측

R04의 실제 요청량·head 지연을 후속 관측했다. main `ebc7e7a`의 production 바이너리(`v0.2.15-12-gebc7e7a`)와 로컬 네 테스트넷 설정을 사용했다. 12:44:51 KST부터 300.004초, Pebble 임시 DB에서 한 App으로 동시 실행했다. state_prefetch=true, workers=2, state_workers=4, chunk_size=10, poll_interval=2s, finality=head, 두 상태 기능의 allow_pruned_state=false를 유지했다. 저장소의 실제 DB/config와 기능 기본값은 바꾸지 않았다. 시작 직전 zero-address 상태를 확인하고 당시 head-2를 시작 높이로 선택했다.

HTTP 관측 proxy는 body/응답을 변경하지 않고 upstream HTTPS 연결을 재사용했다. App의 체인별 5 HTTP requests/s 한도를 유지하면서, 관측용 head 조회까지 proxy의 같은 5회/초 limiter를 공유했다. batch는 그대로 전달하며 HTTP 요청 수와 batch member의 논리 요청 수를 따로 센다. 같은 JSON-RPC batch라도 제공자가 멤버별 quota를 적용할 수 있으므로 HTTP cap을 제공자의 실제 계약 한도로 해석하지 않는다. head/색인 API/RSS는 약 10초마다 관측했다. 관측 자체가 약 0.093 requests/s를 쓰고 proxy의 추가 대기·전달 비용도 있으므로 무계측 직접 실행의 처리량 수치가 아니다. 마지막 표본은 시작 후 294.718초이며 head 조회와 DB/API 조회가 완전히 동시인 것은 아니다.

| 체인 | 시작 / 마지막 관측 저장 높이 | 마지막 head 차이 | 첫 유효 저장 표본~마지막의 색인/생성률 |
|---|---|---:|---:|
| StableNet 8283 | 22,276,792 / 22,277,088 (297블록) | 1블록 | 1.525 / 1.001 blocks/s |
| BSC 97 | 136,672,247 / 136,672,351 (105블록) | 555블록 | 0.348 / 2.225 blocks/s |
| Sepolia 11155111 | 11,910,643 / 11,910,646 (4블록) | 24블록 | 0.023 / 0.090 blocks/s |
| Arc 5042002 | 67,097,158 / 67,097,305 (148블록) | 439블록 | 0.529 / 1.976 blocks/s |

위 비율의 관측 구간은 각각 186.884/284.519/88.781/274.240초다. 처음 유효 저장 높이를 관측한 시각은 107.834/10.199/205.936/20.478초다. StableNet은 초기 epoch/상태 준비 이후 따라잡는 구간을 포함해 비율이 생성률보다 높았고, 마지막에는 head 근처였다. Sepolia는 아래 transport 실패·재시도 영향을 포함한다. BSC의 낮은 비율에는 멈춘 시간이 포함되므로 성공 경로의 지속 처리량으로 해석하지 않는다. Arc의 격차는 첫 유효 표본 42블록에서 마지막 439블록으로 증가했다.

| 체인 | App HTTP / 논리 요청 | head / 잔액 요청 | proxy 대기 p95 / upstream 응답 p95 |
|---|---:|---:|---:|
| StableNet | 1,252 / 1,956 | 544 / 1 | 566.7 / 37.5ms |
| BSC | 1,027 / 1,333 | 440 / 202 | 560.6 / 176.3ms |
| Sepolia | 1,445 / 1,454 | 303 / 1,125 | 1,020.8 / 115.4ms |
| Arc | 1,455 / 1,607 | 440 / 681 | 678.2 / 76.7ms |

각 체인의 관측 head 요청은 추가 28회다. proxy 대기는 App 내부 limiter 대기를 포함하지 않는다. HTTP 429는 없었다. BSC 잔액 hash 조회 74회는 missing trie node였다. Arc의 계약 호출 오류 17회는 정상적인 token 판별 VM revert이며 상태 소실과 구분했다. Sepolia는 upstream 연결이 응답 없이 닫혀 proxy가 502를 반환한 transport 실패 1회와 첫 블록 전체 재시도가 있었다. proxy/연결 재사용의 영향을 배제하지 못했으므로 이를 노드 자체 장애율이나 무계측 성능으로 주장하지 않는다. 네 체인 프로세스 RSS는 표본 33,584~112,688 KiB였다. 누수 판정이나 chain별 heap 수치가 아니다.

**실제 상태 소실:** BSC는 `136672352`의 필수 초기 잔액을 부모 hash로 읽지 못해 25회 batch 실패·같은 높이 재시도를 기록했다. 마지막까지 저장 높이 `136672351`을 유지했다. 실패한 snapshot을 0으로 저장하거나 시작 높이를 올려 건너뛰지 않았다. 최초 오류는 12:46:36 KST였다. 이후 다른 세 체인의 색인은 진행했다. 전체 실행의 0 초기화/latest 메타데이터 대체/보정 상태 소실 fallback 경고는 모두 0건이다. 이는 잔액 전체 oracle 검증이 아니다.

앱 종료 후 실제로 성공했던 첫 nonzero-address 잔액 요청을 다시 조회했다. BSC의 `0x0000000000000000000000000000000000001000`은 원래 hash에서 성공했던 상태가 재조회 때 missing trie node였고, 당시 head 및 head-32의 number 조회는 성공, head-128/head-512는 실패했다. Sepolia의 표본 계정은 원래 hash 및 head/head-32/head-128 성공, head-512 실패였다. Arc 표본은 원래 hash와 네 number 조회 모두 성공했다. StableNet에는 준비 중 nonzero-address 잔액 요청이 없어 이 계정 검사를 수행하지 않았다. 이 소수 계정·시점의 결과로 고정 보존 블록 수, 제공자의 물리적 archive 구성이나 허용 중단 시간을 확정하지 않는다.

기존 단계 계측의 마지막 **완료 chunk 로그**에서 fetch/prepare/write p95는 StableNet 870.1/1.0/30.1ms, BSC 1,204.7/1,995.8/1,417.6ms, Arc 1,403.2/6,002.3/1,633.3ms였다. 순서대로 sample count는 301/100/150이며 최근 256개 window와 전체 누적 count를 구분한다. BSC의 실패 뒤에는 완료 chunk 로그가 갱신되지 않아 위 값은 최종 실패들을 포함하지 않는다. Sepolia는 chunk를 끝내지 못해 이 snapshot이 없었다. write 시간은 canonical RPC와 전체 transaction을 포함하며 순수 DB commit latency가 아니다. 전달 payload peak는 0/5,706/9,375 bytes, in-flight peak는 모두 4였다. 이 값은 준비 중 임시 할당이나 전체 heap 상한이 아니다.

**다음 개선 우선순위:** head 조회가 App 논리 요청의 27.8/33.0/20.8/27.4%, HTTP 요청의 43.5/42.8/21.0/30.2%다. 코드에서 `fetch.followTarget`은 설정 poll_interval과 별도로 1초마다 RPC를 읽고, `multichain.trackMetrics`는 syncing 중 commit 블록마다 health 조회로 head RPC를 더한다. 따라서 우선 head/동기화 상태 관측의 중복을 줄이되 API의 freshness·실패 시 progress와 체인 health 계약을 유지하는 회귀 검증이 필요하다. 다음은 Sepolia/Arc의 초기 잔액 요청량과 speculative 중복·hash 고정 batch 가능성 측정이다. workers만 늘리거나 quota를 임의로 올리는 것으로 해결됐다고 간주하지 않는다. BSC/Arc의 현재 설정을 정확한 잔액의 지속 운영 가능한 구성으로 확정하지 않았다.

처음 관측 도구는 batch 배열을 단일 JSON-RPC object로 읽어 색인이 진행되지 않았다. 그 실행은 중단·정리하고 성능 표본에서 제외했다. 수정 후 단일/batch payload의 동일 전달, HTTP/논리 집계, 오류 ID 매핑과 표본 계정 선택을 별도로 확인한 뒤 위 측정을 수행했다. 응답 내용이나 운영 코드를 이 문제 때문에 바꾸지 않았다. 측정 후 App은 정상 종료(code 0)하고 임시 DB를 제거했다. 다른 프로세스/컨테이너를 중지하지 않았고 실제 `data/multichain-testnets`를 만들지 않았다. 도구·원본 JSON/log는 로컬 `/private/tmp/indexer-recovery-20261014/observe-live.py`, `operating-window.json/log`에 있다. 영구 사실·방법·한계는 이 기록에 보존한다.

**남은 범위:** 장기 실행, 동일 입력 off/on 비교, 네 공개 체인의 PostgreSQL 동시 관측, 전체 잔액 정확성, 개별 DB commit latency/전체 heap, 여러 호스트 SLO와 상태 보존 안전 여유. 이번 5분 실행은 운영 SLO나 R03/R04 전체 완료가 아니다. 소스/부하 시험/알림 경로를 변경하지 않았으므로 추가 SLO 실행은 하지 않는다. 문서 PR 전 gofmt/build/lint(0 issues)/전체 race(app 466.575초)/PostgreSQL(adapter 44.341초, app 176.484초)/예제를 통과했다. 링크·코드 fence·측정 표의 원본 대조와 임시 DB 정리도 확인했다. 새 동시성 시험은 추가하지 않았다.

### 3.14 10/14 중복 head 관측 감소

사용자는 후속 작업 1~6을 순서대로 진행하도록 승인했다. 첫 항목은 3.13에서 확인한 head 요청 중복이다. `Fetcher`마다 raw head/finalized target 관측을 별도로 두고, background progress와 syncing 중 블록 metric 판정이 최근 1초 이내 성공 또는 진행 중 조회를 공유한다. live target poll/head 알림과 명시적/API/주기적 health는 **독립적인 fresh RPC**를 시작한다. 관측 timer는 마지막 성공의 만료 시각을 따라 실패·긴 batch 대기에서도 progress를 갱신한다. confirmations target을 raw health head로 사용하지 않는다.

실패는 이전 성공을 무효화하고, 요청 generation으로 늦게 끝난 이전 조회가 새 관측을 덮지 않게 한다. 원래 RPC latency를 재사용해 cache hit 실행 시간을 노드 지연으로 보고하지 않는다. 조회를 요청한 고루틴이 RPC를 소유하며 새 background job을 만들지 않는다. 관측 대기자는 자기 context로 취소하고, 먼저 조회한 호출자가 취소되면 살아 있는 대기자는 재조회한다. canonical hash·계정/토큰 상태 RPC는 이 cache와 분리돼 그대로 검증한다. chain/pipeline 재시작의 새 Fetcher는 기존 관측을 재사용하지 않는다.

기존 `multichain.Indexer`는 유지하고 선택적 `NodeHeightObserver`만 추가했다. 지원하지 않는 외부 구현은 기존 fresh NodeHeight 경로다. syncing→active 판정은 성공한 health와 살아 있는 context를 요구하고 상태 lock 안에서 syncing인지 재확인해 오류/중지를 active로 덮지 않는다.

초기 구현은 명시적 health까지 성공 관측을 재사용해 기존 네 체인 장애 시험이 실패했다. 그 결과를 보고 fresh health 계약을 유지하도록 범위를 좁혔다. 새 head 알림도 이전 요청에 합류하지 않도록 검사했다. 시험 기준·오류 주입·API 응답은 바꾸지 않았다.

최종 새 회귀는 race200(fetch 55.451초, multichain 3.052초, app 50.653초)와 CPU1 race200(fetch 44.748초, multichain 2.509초, app 33.282초)을 통과했다. 조회 공유/만료/fresh 우회/오류 무효화/대기자·소유자 취소/낮아진 head/이전 요청의 늦은 완료/원래 latency/명시적 health 장애 감지/실패 시 active 승격 거부를 확인한다. 기존 progress 재시도·finality·네 체인 장애/재시작 시험의 focused race도 통과했다.

같은 `BenchmarkHeadObservationQuota` 파일을 변경 전 main `0d9d8f5` git archive checkout과 최종 구현에 적용했다. Apple M2 Max/darwin arm64/Go1.26.8/Pebble, 결정적인 41개 empty block(40+genesis), 5 HTTPrequests/s, workers2/state_workers4/chunk10/poll2s/state_prefetch=true의 같은 NewApp 멀티체인 wiring이다. fixture와 parent App 생성 및 최종 Shutdown은 제외하고 child startup·색인·metrics subscriber 전달·live cancel까지 잰 3회 평균이다. block/receipt batch의 member 수와 HTTP 요청 수는 다르다.

| 구현 | 완료 시간 | HTTP / head 요청 | 누적 할당량 / 횟수 |
|---|---:|---:|---:|
| main 0d9d8f5 | 17.660초 | 89 / 41 | 9.31 MB/op / 61,702 |
| head 관측 공유 | 11.908초 | 60 / 12 | 7.50 MB/op / 48,834 |

시간 약32.6%, HTTP약32.6%, head약70.7% 감소다. 초기 fixture 시간을 포함한 smoke/기준 실행은 이 최종 표에 사용하지 않았다. 빈 블록으로 관측 비용을 분리했으며 거래·상태 조회가 많은 실노드의 개선률·장기 SLO·PostgreSQL 운영 목표로 일반화하지 않는다. 누적 할당량은 peak heap이 아니다. RPC quota와 기존 ignored config/DB를 변경하지 않았다. 실제 체인 운영의 동시 관측과 초기 잔액/토큰 조회량 감소는 다음 항목이다.

최종 gofmt/build/lint(0 issues)/전체 race(app475.451초)/PostgreSQL(adapter41.865초, app171.661초)/예제(14.438초)가 통과했다. 격리한 기본 SLO는 readers125.3/non-readers108.5ms(-13%), 준비 true SLO는109.0/111.8ms(+3%)로 동일 기준을 통과했다. fast stream p99는 각각10.5/8.0ms였다. 부하·round 수·판정 임계값은 완화하지 않았다. 기존 R07 변동의 원인을 해소했다고 주장하지 않는다. 문서 링크/코드 fence와 비교 표를 확인했다.


### 3.15 10/14 hash 상태 batch 준비

초기 잔액의 개별 HTTP 왕복과 토큰 속성의 순차 왕복을 줄이기 위해 `indexer.state_batch_size`를 추가했다. 기본값 1, 0은 1로 정규화, 최대 64이며 체인별 override가 가능하다. `state_prefetch`가 켜진 full ingest에서 같은 부모 hash의 초기 잔액과 같은 생성 hash의 독립 메타데이터 속성을 묶는다. canonical 검증·블록별 atomic writer·같은 높이 재시도·strict 상태 정책은 유지한다. URI fallback과 번호 기반 backfill은 기존 경로다.

batch envelope가 거부되거나 전체 응답의 ID를 대응시킬 수 없으면 client별로 개별 조회를 사용한다. 그 fallback은 이미 보유한 공유 permit 안에서 실행해 permit 재진입 교착을 피한다. 부분 오류·null 잔액·상태 소실·hash selector 오류·429/5xx는 성공이나 batch 미지원으로 바꾸지 않는다. 정상 계약 revert는 선택 속성의 기존 규칙을 따른다. SDK의 기존 인터페이스는 유지하고 선택 batch reader와 Deps 필드만 추가했다.

HTTP 왕복 감소는 논리 RPC/제공자 quota 감소를 뜻하지 않는다. 앞선 블록의 commit 전에 같은 계정이 준비되는 speculative 중복은 남는다. 공개 RPC의 멤버별 quota가 확정되기 전 운영 config는 개별 조회를 유지한다.

검증 결과: EVM/최근 시작/StableNet의 개별·배치 keyspace와 실제 잔액 oracle 일치, native/token 멤버 오류 시 블록/cursor/outbox rollback과 같은 높이 재시도, 80개 신규 계정·4 permit의 미지원 fallback, 취소 후 pipeline 배출, 역순 응답/부분 오류/null 응답을 확인했다. 새 App 동시성 시험의 일반 race 200회는 806.202초, CPU1 race 200회는 1200.760초로 통과했다.


동일 25블록(각 8개 신규 수신자), workers2/state_workers4, 양쪽 pipeline 계측 활성화, 3회 평균으로 Pebble에서 측정했다. fixture/App 생성과 Shutdown은 제외한다. 같은 구현에서 옵션만 바꾼 비교이며 hash/canonical의 논리 요청과 batch HTTP를 별도로 센다. Apple M2 Max/darwin arm64/Go1.26.8/64GiB/12CPU, 자체 heavy 검증은 종료한 상태였으나 다른 사용자 작업의 호스트 부하는 통제하지 않았다(측정 시작 load average 9.27/6.85/6.46).

| 조건 | ordered | prepared 개별 | prepared batch16 | HTTP 개별→batch / 논리 RPC 개별→batch |
|---|---:|---:|---:|---:|
| 로컬 무지연 | 0.222초 | 0.154초 | 0.215초 | 246.3→76 / 271.3→272 |
| RPC 5ms | 1.469초 | 0.462초 | 0.376초 | 246.7→76 / 271.7→272 |
| RPC 5ms·50HTTP/s | 4.477초 | 4.953초 | 1.534초 | 247→76 / 272→273 |

50HTTP/s에서 prepared 개별 대비 batch의 완료 시간은 약69.0%, HTTP 왕복은 약69.2% 줄었다. 논리 요청은 감소하지 않았고 로컬 무지연에서는 batch가 약40.2% 느렸다. 누적 할당은 cap50 조건 31.21→21.34MB/op, writer p95 평균은149.5→86.27ms였다. prepared batch의 최고 payload 계산값은9,888bytes였다. 누적 할당은 peak heap이 아니며 writer 시간에는 queue/canonical/전체 트랜잭션이 포함된다. 이 native workload에는 새 토큰 생성이 없고, 토큰 batch는 정확성 시험으로 검증했다. 공개 5HTTP/s·PostgreSQL·장기 운영의 개선률로 적용하지 않는다. 다음 순차 항목에서 동일 입력의 두 저장소 전후 비교를 수행한다.

최종 gofmt/build/lint(0 issues)/예제(17.813초)/PostgreSQL(adapter46.940초, app204.780초)가 통과했다. 첫 전체 race는 다른 자체 반복 검증과 겹쳐 실행해 package 전체 10분 제한으로 실패했다(TestRollbackLargeBlocks 시작 시점의 Pebble Open/fsync). 통과로 간주하지 않았으며 반복 검증 종료 후 같은 전체 race를 단독으로 재실행해 app448.922초로 통과했다. 두 번째 실행은 package 실행 제한만20분으로 지정했으며 테스트 항목과 assertion을 완화하지 않았다.

SLO 기본 경로의 첫 실행은 round2 비읽기 구독자의 end-to-end p99가337.2ms로 절대250ms 기준을 초과했다(ingest105.4/push217.8ms, readers137.5ms). round1은116.4/100.5ms였고 fast stream11.8ms/path1.6ms는 통과했다. 당시 호스트 load average7.66/7.32/6.88과 OS mediaanalysisd/앱 renderer/VM의 CPU 부하를 읽었으나 원인으로 확정하지 않았고 다른 작업을 중지하지 않았다. 실패 표본을 통과로 간주하지 않는다.

변경 전 main34619b0 git archive를 같은 호스트·1000구독자·60블록·10trades/s·2 alternating rounds·같은250ms/상대20% 기준으로 실행했다. 기준 실행은252.876초, readers116.4/non-readers119.3ms(+3%), fast stream8.2ms로 통과했다. 비교 시점의 호스트 부하는 달랐으며 causal regression/특정 외부 부하 원인을 확정하지 않았다. 준비가 꺼진 기본 경로에서 이 batch 옵션은 사용되지 않는다. R07의 변동 조사와 여러 호스트 검증은 계속 남는다.

현재 기본 SLO의 동일 기준 재확인은252.885초로 상대 기준 실패(readers109.3/non-readers158.2ms,+45%)였다. 이 표본의 비읽기 라운드는 ingest110.9/113.2ms, push55.3/47.2ms로 첫 실패의 push 급증과 다른 양상이다. CPU/GC 계측을 추가한 동일 기준 진단 실행은readers153.8/non109.9ms(-29%)로 통과했다. 185 GC에서 최대 STW 합9.1ms·최대 concurrent GC13ms였고, CPU 표본56.57초의73.6%는raw syscall, 누적TCP read41.4%/write31.8%였다. 이 프로파일은 통과한 계측 표본이며 실패 시점의 원인을 대신 설명하지 않는다.

검토 중 load fixture의 수명 결함을 확인했다. 각 SLO 라운드와 알림 부하 시험이 `fetcher.Run(t.Context())`를 시작하고 App/DB를 닫기 전에 loop를 취소·join하지 않았다. `Fetcher.Close`는 writer/relay/background를 닫지만 외부 Run context를 끝내지 않으므로 이전 라운드의 loop가 부모 시험 종료까지 남았다. 시험별 child context와 종료 대기를 추가했다. 작은 두 라운드에서 부모 context가 살아 있어도 live fetcher가 남지 않는 회귀 시험을 추가했고, 원래 시작 방식에서기대0/실제1로 실패를 재현했다. 저장한 수정본을 복원한 뒤 일반 race200(74.684초)·CPU1 race200(84.315초)가 통과했다. 이는 시험 격리의 수정이며 실제 SLO 변동의 유일한 원인으로 확정하지 않았다. 부하·round 수·절대/상대 기준을 변경하지 않았다.

수정한 fixture의 기본 SLO는252.927초로 통과(readers121.3/non116.1ms,-4%; fast stream8.6ms/path1.2ms), state_prefetch=true/batch16 SLO도252.829초로 통과(readers125.6/non119.9ms,-4%; fast stream5.0ms/path1.1ms)했다. 1000구독자·60블록·10trades/s·2 alternating rounds·기존 절대/상대 기준을 그대로 사용했다. 이전 실패와 계측 표본을 삭제하지 않았으며 여러 호스트와 장기 반복의 변동 원인까지 해결했다고 주장하지 않는다.

수명 수정까지 포함한 최종 소스의 gofmt/build/lint(0 issues)/전체 race(app418.368초)/PostgreSQL(adapter42.217초, app177.008초)/예제(정상 캐시 통과)를 다시 확인했다. 새 batch 동시성 시험의 일반/CPU1 200회와 load 수명 시험의 일반/CPU1 200회, 기본/배치16의 최종 정규 SLO가 모두 통과했다. GPG/noreply와 PR/main 네 CI의 기존 저장소 절차를 따른다.

### 3.16 10/14 동일 입력 Pebble/PostgreSQL 전후 비교

main `34619b0`과 `44fd99c`를 동일한 25블록(24×8 신규 수신자+genesis)·workers 2/state_workers 4·동일 HTTP/member counter와 pipeline 계측·3회 평균으로 비교했다. 기준 커밋의 archive checkout에 같은 benchmark를 복사하고 미지원 batch 16 옵션만 제외했다. 입력·fixture/App 생성 제외·Shutdown 제외 기준은 3.15와 같다. 이 native workload에 새 토큰 생성은 없다. 각 DB는 새 임시 DB/schema다. 이 작업의 heavy 검증은 끝낸 상태지만 다른 사용자 작업의 호스트 부하를 통제하지 않았다.

| 저장소/조건 | 이전 ordered | 이전 prepared | 현재 ordered | 현재 prepared | 현재 batch16 |
|---|---:|---:|---:|---:|---:|
| Pebble local | 0.216초 | 0.233초 | 0.231초 | 0.154초 | 0.221초 |
| Pebble RPC5ms | 1.416초 | 0.342초 | 1.403초 | 0.404초 | 0.418초 |
| Pebble RPC5ms/50HTTPs | 4.455초 | 4.939초 | 4.453초 | 4.932초 | 1.534초 |
| PostgreSQL local | 0.205초 | 0.219초 | 0.218초 | 0.212초 | 0.262초 |
| PostgreSQL RPC5ms | 1.494초 | 0.382초 | 1.580초 | 0.379초 | 0.365초 |
| PostgreSQL RPC5ms/50HTTPs | 4.420초 | 4.993초 | 4.423초 | 4.942초 | 1.519초 |

PostgreSQL 16.15(aarch64/alpine/Docker)는 `fsync/synchronous_commit/full_page_writes=on`을 실제로 조회했다. 기능 시험의 fsync=off와 구분한다. Pebble도 기존 synced commit을 유지했다. 두 저장소의 실행 환경은 macOS native와 같은 호스트의 Docker VM으로 다르므로 DB 고유 속도나 운영 목표 배수로 일반화하지 않는다. 현재 batch의 cap 50 완료 시간 차이는 약 1%였다. 이 조건에서는 RPC 제한의 영향이 크다.

제한 조건에서 HTTP 요청은 247→76, 논리 RPC는 약 272→273이었다. 이전 prepared 대비 시간은 Pebble 약 68.9%/PG 약 69.6% 감소했다. 현재 prepared와 batch 비교도 약 68.9%/69.3%다. 현재 무지연 batch는 개별 준비보다 느렸다(Pebble0.154→0.221/PG0.212→0.262). RPC 5ms에서는 Pebble batch가 소폭 느려서 왕복 지연만으로 항상 개선을 보장할 수 없다. 전후 개별 control의 local/무제한 변동을 batch 효과로 해석하지 않는다. cap 50 현재 writer p95 평균은 Pebble 141.7→80.0ms/PG165.5→69.7ms, 누적 할당 31.05→21.10MB/30.65→21.00MB였다. peak payload는 9,888/10,312 bytes였다. heap 전체 크기나 DB commit 단독 시간은 아니다.

현재 PG 도구의 첫 시도는 초기화 중 DB 생성 전에 `pg_isready`가 성공하여 metadata 쿼리에서 실패했다. benchmark는 실행하지 않았다. 실제 TCP로 테스트 DB의 `SELECT 1` 성공을 기다리도록 고친 뒤 위 최종 표를 측정했다. 실패 시도는 성능 표본에 포함하지 않았고, 해당 임시 컨테이너는 정리했다. 공개 5 HTTP/s 운영 성능·전체 회계·장기 SLO는 다음 실노드 관측과 별개다.

### 3.17 10/14 네 공개 테스트넷 30분 동시 관측

main `44fd99c` 실행 파일을 17:13:10 KST부터 1800.010초 실행했다. 새 임시 DB에서 최근 head-2와 시작 상태를 재확인했다. 설정은 `allow_pruned_state: false`, workers 2, state_workers 4, chunk 10, state_prefetch true, state_batch_size 16이었다. 원래 ignored config의 batch 기본값은 1이며, 16은 임시 검증 설정에만 적용했다. App과 투명 proxy의 체인별 5 HTTP/s 제한을 유지했고, head 관측 174회/체인도 같은 proxy 제한을 공유했다. 배치 전달·HTTP/member별 논리 요청 분리·HTTPS 연결 재사용 조건은 3.13과 같다. 다른 사용자 작업의 호스트 부하는 통제하지 않았고 이 작업의 heavy 시험은 겹치지 않았다.

| 체인 | 시작 | 마지막색인/head | gap | App HTTP/논리 | 마지막약10분 생성/색인 blk/s |
|---|---:|---:|---:|---:|---:|
| StableNet | 22292889 | 22294690/22294691 | 1 | 4822/7396 | 1.001/1.002 |
| BSC | 136708015 | 136712015/136712017 | 2 | 8283/12768 | 2.222/2.236 |
| Sepolia | 11911983 | 11912135/11912135 | 0 | 2859/8179 | 0.083/0.083 |
| Arc | 67128941 | 67130550/67132499 | 1949 | 8770/15224 | 1.976/0.682 |

마지막 표본은 1799.550초, 마지막 속도 창은 592.645초다. 첫 색인 확인은 StableNet 166.839/BSC 10.206/Sepolia 20.519/Arc 10.206초였다. StableNet 초기 epoch tracker가 앞선 블록을 순차 조회하는 경로를 확인했지만 초기 지연의 모든 원인을 확정하지 않았다. 헤더/영수증 조회 각 2,574개와 색인 1,802블록의 차이도 profile 추가 비용으로 분리할 필요가 있다. BSC/Sepolia는 head 근처를 유지했다. 이전 3.13의 BSC 정지 표본과 이번 입력·노드 라우팅·상태는 달라, 실노드의 인과적 개선률을 계산하지 않는다.

Arc의 격차는 약 5분 184/10분 383/20분 1,196/종료 1,949블록으로 늘었다. 끝무렵 native balance 4건과 receipt 1건에서 JSON-RPC -32005 `rate limit exceeded`를 받았다(HTTP 200). 필수 조회 실패는 블록 전체 실패와 동일 높이 재시도로 이어졌다. Arc의 기능 RPC는 balance 5,168/code 552/call 3,506건으로, native/token 상태 획득 비용이 남는다. 정상 VM revert 546/invalid jump 116건은 토큰 판별 응답으로 quota 실패와 구분했다. 배치는 물리 왕복을 줄이지만 member quota를 없애지 않는다. 이 설정으로 Arc의 상시 운영 조건을 충족했다고 볼 수 없다.

StableNet의 proxy HTTP 502는 38건 모두 upstream 이름 해석 실패(`[Errno 8] nodename...`)였다. App의 target/block 재시도 후 head 근처로 복구했고, source block not found 6회도 관찰했다. BSC/Sepolia/Arc의 proxy HTTP status는 모두 200이었다. proxy transport p95는 StableNet 35.8/BSC 166.1/Sepolia 136.9/Arc 75.7ms, proxy wait p95는 350.5/206.5/74.6/344.0ms였다. App limiter 대기와 proxy 대기를 합쳐 노드 CPU 비용으로 해석하지 않는다.

최근 256개 샘플의 단계별 p95(수집/준비/순차 쓰기)는 StableNet 416.9/399.8/33.1ms, BSC 520.9/399.0/337.8ms, Sepolia 449.3/2759.9/686.0ms, Arc 800.8/4992.9/960.1ms였다. 쓰기는 DB commit 단독 시간이 아니다. 최종 pipeline의 in_flight/state bytes는 모두 0, peak in_flight는 4였다. peak payload는 StableNet 0/BSC 4,459/Sepolia 65,590/Arc 19,285 bytes였다. RSS는 32.8~509.5MiB였으며 DB cache·전체 heap·기본 블록을 포함하므로 payload 상한과 구분한다.

종료 후 실제 초기 조회 계정과 원래 hash로 상태를 재조회했다. BSC/Sepolia에서 성공했던 원래 hash는 상태 소실 오류를 반환했다. 같은 계정의 head-32는 성공했고 head-128/-512는 실패했다. Arc는 원래 hash와 head-512도 성공했다. StableNet은 이 창에서 zero 주소 외 초기 잔액 표본이 없어 계정 보존 probe를 수행하지 않았다. 계정·시점·라우팅의 제한이 있으므로 고정 보존 폭·허용 중단 시간·전체 native 회계를 확정하지 않는다.

프로세스는 exit 0으로 정상 종료했고 자체 임시 DB·proxy를 정리했다. 근사 fallback 경고는 0건이었다. 이 30분 관측은 며칠 단위의 운영 SLO, 재시작, 전체 회계, 메인넷 검증을 대신하지 않는다. 시작·재시작 증거는 이전의 제한된 3.9 검증과 구분한다.

### 3.18 10/14 full node 실행 후보와 운영 미확정 조건

현재 `indexer-go/config.yaml`은 네 공개 RPC, 명시적 profile, strict 상태 정책, workers 2/state_workers 4, payload 16MiB, chunk 10, 물리 5 HTTP/s로 구성됐다. `database.path`의 실제 DB가 없음을 확인했다. 네 chain ID 및 최근 시작 직전의 실제 계정(StableNet miner/BSC 0x1000/기존 Sepolia·Arc 표본)의 parent hash 잔액 조회가 모두 성공한 뒤 시작 높이를 갱신했다. 이후 실행 전에 다시 조회해야 하는 순간 표본이다. 기존 DB가 있으면 시작 높이를 건너뛰지 않고 next height/backfill 상태를 검사해야 한다. 글로벌 `state_batch_size`는 1이며, 검증에 쓴 16을 상시 운영 설정에 자동 적용하지 않았다.

초기 Python 기본 User-Agent 프리플라이트는 Sepolia/Arc에서 HTTP 403으로 실패했다. 실제 Go User-Agent를 사용해 재확인한 최근 state 표본은 네 곳 모두 성공했다. 초기 403을 실제 노드 장애로 일반화하지 않는다. 관측 전 config의 mtime(10:49:20 KST)과 내용을 확인했으며, 3.17의 Arc endpoint는 `https://rpc.testnet.arc.io`였다. 이전 인수인계의 `.network` 표기는 현재 파일과 달랐다. 관측 중 endpoint를 바꾸지 않았다.

[BNB 공식 문서](https://docs.bnbchain.org/bnb-smart-chain/developers/json_rpc/json-rpc-endpoint/)는 testnet/mainnet 10K/5min을 명시하지만 batch member 계산과 현재 사용자의 전체 IP 예산은 확정하지 못했다. [StableNet node 설정 문서](https://docs.stablenet.network/en/getting-started/2.2-node-configuration)는 full node가 최근 state만 보관하고 batch request 기본값이 1,000/response 25MB라고 설명한다. 현재 공개 RPC 운영자의 실제 설정과 보존 기간을 검증한 것은 아니다. [Arc 공식 연결 문서](https://docs.arc.io/arc/references/connect-to-arc)는 5042002/primary rpc.testnet.arc.io를 명시하지만 quota/retention 보장은 확인하지 못했다.

운영 확정에는 실제 full node의 설정 경로/CLI와 state pruning 정책, member/HTTP/메서드별 quota, 허용 중단 시간, 기존 DB의 next height/backfill 정보가 필요하다. archive가 없다는 결정은 유지한다. 실측 상태 보존 여유는 현재 lag+상태 획득 시간+중단+복구 시간보다 충분히 커야 하며, 지속 색인 속도가 생성 속도를 넘어야 한다. 이번 Arc는 속도 조건을 충족하지 못했고 quota 오류도 있었다. BSC/Sepolia의 head-32 성공/head-128 실패를 고정 보존 폭이나 허용 중단 시간 보장으로 바꾸지 않는다. 로컬 후보 설정 점검·재구성은 완료했으나, 실제 운영 보존 정책과 허용 중단 시간의 확정은 미완료다.

### 3.19 10/14 SLO 변동과 추가 호스트 조건

3.15에 기본 실패 2회, 변경 전 main 기준 통과 1회, CPU/GC 계측 통과 1회, load loop 수명 누수 재현·수정, race 200회/CPU1 race 200회, 수정 후 기본·batch 16 SLO의 동일 기준 통과를 기록했다. 프로파일은 다른 시점의 집계 표본이므로 실패 원인을 단정하지 않는다. 기존 170~240ms 및 상대 비율 변동의 단일 원인과 여러 호스트 운영 SLO는 미확정이다.

추가 호스트의 SSH 주소·계정·테스트 경로를 요청했다. 연결된 MacBookAir의 현재 SSH 해석은 kevin/22였으며, BatchMode/StrictHostKeyChecking/5초 timeout의 읽기 전용 uname·Go 버전 probe는 port 22 `Connection refused`로 실행하지 못했다. SSH 설정·인증·known_hosts·원격 파일은 변경하지 않았다. 접속 가능한 host/port/account/path가 필요하므로 추가 호스트 실측은 미완료다. 같은 호스트의 Docker PostgreSQL은 다른 물리 호스트의 SLO 증거가 아니다. 가능한 로컬 진단·수명 수정·표본 보존은 완료했으며 R07에는 여러 호스트와 실패 시점의 분리 관측을 남긴다.

문서 커밋 전 gofmt/build/lint(0 issues)/예제/전체 race(app 437.472초)/PostgreSQL(adapter 43.339초, app 185.490초)가 통과했다. 생산 코드는 #173 병합 이후 동일하며, 기본·batch 16의 정규 SLO 통과 증거는 3.15와 같다.

### 3.20 10/14 상태 조회 슬롯 대기 중 완료된 잔액 초기화 재확인

3.17의 Arc 관측에서 native balance RPC 5,168건과 상태 준비 p95 4,992.9ms를 확인했다. 기존 준비 경로는 최초 DB 검사 뒤 공유 permit을 기다렸으므로, 대기 중 이전 블록이 계정을 초기화해도 처음 만든 목록대로 RPC를 보냈다. 이는 가능한 중복 경로이며, 이 경로가 해당 실노드 지연의 유일한 원인이라고 단정하지 않는다.

초기 DB 검사로 배치를 구성하는 기존 동작을 유지하고, 공유 permit 확보 뒤 실제 RPC 직전에 committed balance record를 재확인한다. 이미 초기화된 계정은 단건·배치·unsupported fallback에서 읽지 않는다. 남은 배치 멤버는 원래 주소 인덱스로 결과/오류를 되돌리고 빈 배치는 보내지 않는다. 조회를 생략한 계정은 준비 결과에 넣지 않으며, writer에서 rollback 후 초기화가 다시 필요하면 기존 순차 조회를 사용한다. 모든 조회가 생략된 블록은 불필요한 prepared-state canonical RPC를 요구하지 않는다. 다른 블록이나 fork의 speculative 잔액을 공유하지 않는다. 추가 DB 재확인의 비용이 있어 환경별 효과를 따로 측정한다. 준비 옵션을 끈 경로는 동일하다.

새 회귀 시험은 기다리는 4개 계정 중 필요한 단건 조회 1개/배치 멤버 2개만 남기는 통제 사례, unsupported fallback, 기록 소실 뒤 writer 재조회, 압축 배치의 주소/멤버 오류 매핑, 모두 기록된 계정의 RPC 생략, DB 오류 전파를 확인한다. 이전 구현을 사본으로 바꿨을 때 대기 계정 회귀가 실패했고 최신 소스를 즉시 복원했다. 최종 시험은 일반 race 200회 1.792초, CPU1 race 200회 1.597초로 통과했다. 이 통제 사례의 요청 감소를 Arc 공개 RPC의 개선률로 확대하지 않는다.

전후 측정은 `b207243` archive checkout과 현재 소스의 동일 25블록·매 블록 새 수신자 8개·workers 2/state_workers 4·3회 평균으로 실행했다. Pebble 표는 아래와 같다. 이 입력은 매번 새 계정을 읽어야 하므로 대기 중 중복 제거의 이득이 작다. 기존 테스트넷 30분 표본을 새 코드의 개선률로 사용하지 않는다.

| 조건 | 이전 prepared | 현재 prepared | 이전 batch16 | 현재 batch16 |
|---|---:|---:|---:|---:|
| local | 0.236초 | 0.197초 | 0.249초 | 0.172초 |
| RPC 5ms | 0.434초 | 0.385초 | 0.418초 | 0.394초 |
| RPC 5ms/50 HTTP/s | 4.936초 | 4.912초 | 1.558초 | 1.559초 |

50 HTTP/s 조건의 prepared HTTP/논리 요청은 247/272→245.7/270.7, batch16은 76/272.7→76/272였다(3회 평균). 현재 대비 이전 writer p95는 prepared 160.8/145.0ms, batch 89.66/85.43ms로 개선되지 않았다. 변경하지 않은 ordered control도 local 0.276→0.218초, 제한 조건 4.514→4.748초로 변동했다. 짧은 호스트 표본의 시간 차이를 인과적 속도 개선으로 단정하지 않는다. 추가 DB 확인이 모든 환경을 더 빠르게 만든다는 보장은 없다.

새 PostgreSQL 비교의 첫 시도는 Docker 파일시스템 부족(`No space left on device`)으로 실패했다. 기존 이미지·볼륨은 삭제하지 않았다. 호스트 임시 bind mount에서도 데이터 디렉터리 소유권 검사로 DB 시작이 실패했다. 전용 컨테이너 UID를 임시 디렉터리 소유자에 맞춘 기준 측정 1회는 fsync/synchronous_commit/full_page_writes=on으로 완료됐지만, 다음 변경 후 DB는 다시 소유권 오류로 시작하지 못했다. 이 불완전한 쌍으로 PostgreSQL 개선률을 계산하지 않는다. 실패 준비 시도는 성능 표본에서 제외하고 생성한 컨테이너·임시 데이터만 정리했다. 필수 PostgreSQL 기능 시험은 전용 tmpfs 데이터 디렉터리와 기존 기능 시험의 fsync=off 설정으로 수행했으며 adapter 40.324초/app 175.737초로 통과했다. 이 기능 환경을 디스크 성능이나 내구성 검증으로 해석하지 않는다. 이전 3.16의 완결된 디스크 비교 결과는 이번 변경 후 측정이 아니다.

전체 race는 app 415.677초로 통과했고 build/lint(0 issues)/예제도 통과했다. 배치16 정규 SLO는 읽기 중앙 p99 94.6ms/비읽기 포함 106ms(+12%)로 통과했다(app 253.001초). 단건/배치 fast stream p99는 각각 node→message 9.1/6.1ms, fast path→message 1.2/0.6ms로 통과했다. 단건 준비 옵션의 정규 SLO는 읽기 중앙 p99 112.2ms/비읽기 포함 105.3ms(-6%)로 통과했다(app 252.763초). 구독자 1,000명·10 trades/s·2회 교대·절대 250ms/상대 20%의 기존 기준을 유지했다.

### 3.21 10/14 현재 소스의 네 테스트넷 10분 관측·Arc RPC 비용 분해

#175 병합 소스 `99a6666` 실행 파일(v0.2.15-17-g99a6666)을 21:35:38 KST부터 600.002초 관측했다. 새 임시 DB·head-2 시작 재조회·체인 ID/시작 상태 검사·strict 정책·workers 2/state_workers 4/chunk 10/state_prefetch true/batch16, App과 proxy의 체인별 5 HTTP/s 제한을 유지했다. 관측 head 요청도 같은 proxy 예산을 공유했다. profile은 실행 로그에서 StableNet/evm/evm/arc를 확인했다. 성공 요청에도 params·물리 요청 그룹·응답 바이트를 기록하여 주소와 상태 참조의 중복을 분리했다. 기존 설정 파일·DB와 이전 관측 자료는 수정하지 않았다. 이 작업의 heavy 검증은 관측 후에 실행했고, 다른 사용자 작업의 호스트 부하는 통제하지 않았다.

| 체인 | 시작 | 마지막 색인/head | 격차 | App HTTP/논리 RPC | 마지막 약 5분 생성/색인 blk/s |
|---|---:|---:|---:|---:|---:|
| StableNet | 22308638 | 22309231/22309233 | 2 | 2016/3357 | 1.001/0.997 |
| BSC | 136743014 | 136744318/136744332 | 14 | 2929/4703 | 2.222/2.201 |
| Sepolia | 11913286 | 11913337/11913337 | 0 | 1060/3420 | 0.083/0.083 |
| Arc | 67160059 | 67160864/67161232 | 368 | 2908/5342 | 1.974/1.479 |

마지막 API 표본은 591.804초, 속도 계산 창은 290.746초다. 전체 요청 수는 600초 창이므로 마지막 색인 표본으로 나눈 요청/블록 값에는 시작 및 끝의 진행 중 작업이 포함된다. 첫 색인 확인은 StableNet 51.652초, 나머지 세 체인 10.183초였다(관측 간격 약 10초). Arc 격차는 약 4분 178/6분 267/9분 355/마지막 368블록으로 계속 늘었다. 이 입력에서 #175 이후 최신 상태를 지속 유지하는 조건은 충족하지 못했다. 이전 3.17의 30분 표본과 같은 입력이 아니므로 0.682→1.479 blk/s를 코드 개선률로 계산하지 않는다. 이번 Arc contract code 조회는 69/806 색인 블록이며 이전은 552/1609로, 토큰 생성 작업량도 다르다.

Arc의 물리 요청은 블록+영수증 batch 830, 잔액 704, canonical hash 확인용 block(false) 708, head 441, code 69, contract call 153, chain ID 3건이었다. 논리 요청은 balance 2,172/code 69/call 289/head 441/block 1,538/receipt 830/chain ID 3건이다. zero 주소 검사 1건을 뺀 초기 잔액 2,171건은 2,161개 주소/상태 참조 조합, 2,132개 주소였다. 동일 조합의 추가 조회는 10건(약 0.46%)이며 같은 주소의 다른 참조는 29개다. 10개 조합은 모두 동일한 부모 hash에 묶인 재시도였고, 최초 실패한 2개 멤버와 성공했던 8개 멤버를 포함한다. 실패한 2개 멤버는 반드시 다시 읽어야 한다. 다른 block hash의 잔액을 같다고 가정해 공유하지 않는다. 중복 10건을 모두 제거해도 이 표본의 약 25% 처리량 부족을 해결한다고 볼 수 없다. 성공했던 추측 입력의 range 취소·재시도와 같은 참조의 반복은 별도로 조사할 수 있지만, 현재 대다수 초기 조회는 별개 상태다.

Arc contract call의 논리 비용은 ERC165 확인 89, name/symbol/decimals/totalSupply 186, tokenURI fallback 14건이었다. 실제 물리 그룹 수로 합산하며 배치 멤버 수를 HTTP 수로 대체하지 않는다. 최근 300초 App HTTP는 약 4.903/s로 proxy의 남은 예산에 근접했다. 같은 입력 비율·효율이 유지된다는 단순 추정에서는 생성률을 맞추려면 App 약 6.55 HTTP/s 또는 요청/블록 비용 약 25.1% 감소가 필요하다. 이는 공급자의 허용 quota나 운영 권장 한도가 아니며 서로 약간 다른 요청/속도 창을 사용한 근사 용량 계산이다. 한도를 임의로 높이지 않았다. 준비 상태의 canonical 확인이나 필요한 초기 잔액을 생략해 정확성을 낮추지 않는다.

Arc native balance 멤버 2건이 HTTP200/JSON-RPC -32005 `rate limit exceeded`로 실패했고, 블록 67160128 전체 실패·동일 높이 재시도 후 진행했다. Arc의 정상 토큰 판별 응답은 execution revert 75/invalid jump 80건으로 quota 오류와 구분한다. StableNet의 HTTP502 1건은 upstream transport timeout(`[Errno 60] Operation timed out`)으로 재시도 후 복구했다. 이 표본을 이전 관측의 DNS 실패와 혼동하지 않는다. BSC/Sepolia/Arc HTTP status는 모두 200이었다. 종료 코드는 0, 근사 fallback 경고는 0, 자체 프로세스·임시 DB·proxy는 정리됐다.

최근 proxy transport p95는 StableNet 34.3/BSC 165.2/Sepolia 168.4/Arc 75.8ms, proxy wait p95는 160.7/230.7/147.6/327.7ms였다. 최근 pipeline 샘플의 수집/상태 준비/쓰기 p95는 StableNet 421.5/399.8/25.9ms, BSC 633.3/767.9/799.9ms, Sepolia 638.3/5054.5/608.6ms, Arc 809.8/1393.5/826.8ms였다. writer 단계는 canonical RPC·대기·DB 작업을 포함하므로 DB commit 단독 지연이 아니다. App limiter 대기는 proxy 대기에 포함되지 않는다. 최종 in_flight/state bytes는 모두 0, peak in_flight는 4 이하였고 RSS는 33.6~385.3MiB였다.

StableNet의 시작 높이 이전 full block 요청은 696건/서로 다른 58개 높이였다. `EpochTracker`는 적용 epoch를 찾을 때 부모 hash를 검증하면서 이전 블록을 읽고, 현재 `Fetcher.BlockAt`은 블록 모델만 반환하면서 `fetchOnce`로 영수증도 획득한다. 이 조상 조회의 반복과 불필요한 receipt 비용은 R04의 별도 개선 후보다. hash/fork 검증을 유지하는 방법으로 비교해야 하며, 이 기록에서 새 캐시나 조회 경로를 구현하지 않았다.

종료 후 최초 성공한 실제 계정의 원래 상태 참조를 재조회했다. BSC는 원래 참조와 head-128/-512가 상태 소실 오류였고 head/head-32는 성공했다. Sepolia는 이번 10분 원래 참조 및 head-128까지 성공했고 head-512는 실패했다. 이는 이전 30분 표본과 다르다. Arc는 원래 참조와 head-512까지 모두 성공했다. StableNet은 이 창에 실제 초기 잔액 계정 표본이 없었다. 고정 보존 폭·허용 중단 시간·전체 native 회계를 확정하지 않는다. 운영 완료에는 지속 생성률 초과 처리량과 실제 보존 정책/quota 확인이 여전히 필요하다.

관측 종료 후 gofmt/build/lint(0 issues)/예제/전체 race(app 419.154초)/격리 PostgreSQL 기능 시험(adapter 42.423초, app 177.228초)이 통과했다. PostgreSQL은 이전 디스크·bind 소유권 문제를 피하는 전용 tmpfs와 기능 시험의 fsync=off 설정이며 디스크 성능 비교가 아니다. 생산 코드는 #175와 같고 정규 SLO 증거는 3.20의 단건/배치16 통과 결과를 유지한다.


### 3.22 10/14~15 macOS SLO 재현·캐시 수명 수정·WAL 대기 분리

사용자는 공개 RPC 네 개·체인별 5 HTTP/s 유지, 우선 로컬 Docker 비교를 선택했다. `00f99af`에서 동일 Go1.26.8/arm64/CGO off/GOMAXPROCS4·상태 준비 true/batch1·구독자1000명·10trades/s·60블록·2회 교대·250ms/상대20% 기준을 유지했다. macOS는 비읽기 round2 p99 318.2ms(ingest 최대294.7ms)로 실패했고, Docker Linux는 일반104.8/비읽기112.5ms(+7%)로 통과했다. 빠른 stream p99는8.8/10.1ms였다. Linux만 CPU quota4/메모리2GiB 제한을 갖고, DB는 같은 호스트 APFS의 새 경로이며 Linux는 bind mount를 사용했다. 이는 여러 물리 호스트나 공개 5 HTTP/s 색인 처리량 비교가 아니다. Docker CPU throttle은0이었다. localhost 전용 임시 SSH 계정의 공개 키 로그인으로 시험을 실행한 뒤 계정·키·컨테이너를 제거했고, 운영 설정 해시가 동일함을 확인했다. 원본은 `/private/tmp/indexer-docker-ssh-slo-20261014/`에 있다.

후속 macOS 진단은 같은4CPU/상태 준비 조건의 임시 Go overlay와 Go1.26 FlightRecorder를 사용했다. 첫 시도는 시작 이전 height0 이벤트가 trace를 선점해 중단했으며 유효 성능 표본으로 사용하지 않는다. 측정 높이부터 캡처한 실행에서 일반 구독자 round2도 p99 317.1ms/ingest305.5ms로 실패했다. 따라서 비읽기 구독자만의 문제가 아니다. 이 실행의 첫 수신150ms trace는 해당 블록의 write 약9ms/Commit약8ms였고, 뒤의305ms 블록을 포함하지 않았다. 원래318ms 또는 이317ms 실패 전체의 유일한 원인을 이 trace로 확정하지 않는다.

ingest와 수신 캡처를 나눈 다음 실행은 절대 기준을 통과했으나 일반107.2/비읽기183.4ms(+71%)로 상대 기준에 실패했다. 비읽기 round2 height45의 ingest197.032ms를 캡처했다. 같은 단조 trace 시간축에서 write174.776ms/Commit174.009ms/Pebble batch Commit173.411ms였고, WAL의 실제 파일 write syscall은26.447/41.750ms였다. 직전 height43/44의 Commit도67.310/100.388ms였다. 해당 window의 GC stop-the-world 최대0.210ms여서 이 ingest197ms 표본의 지배적 대기는 WAL 쓰기·동기화를 포함하는 커밋 구간이다. 디스크의 지연을 특정 외부 프로세스나 하나의 syscall로 더 좁히지는 않았다. macOS의 Go `os.File.Sync`가 `F_FULLFSYNC`를 사용하는 코드는 로컬 toolchain에서 확인했다. 내구성 경계를 약화하거나 동기화를 끄지 않는다. trace export/상시 tracing 비용이 있는 진단 실행을 비계측 성능 개선률로 사용하지 않는다. 원본과 분석은 `/private/tmp/indexer-macos-slo-cause-20261014/`에 있다.

진단 과정에서 별개로 확인한 계산 오류를 수정했다. 기존 `floor(q*(n-1))`은60블록의 p99에서 가장 느린 블록을 제외했다. 한 블록은 각 구독자의 메시지1/60을 차지하므로 ingest와 수신의 p99 설명이 어긋났다. pipeline metrics와 같은 nearest-rank로 통일하고, 느린 ingest·수신 block height와 지연을 라운드마다 출력한다. `INDEXER_LOAD_DIAGNOSTICS=1`은 pipeline snapshot도 출력한다. `database_commit`은 정상 색인의 `BlockTx.Commit` 호출(undo·내구성 대기·staged state 반영)을 독립적으로 계측하며 RPC·writer queue·feature 처리를 제외한다. fsync syscall 단독 시간은 아니다. 기존 state_prefetch/pipeline_metrics 계측 활성화 조건과 disabled fast path를 유지한다. 저장 순서·동기화·RPC 제한·SLO 부하 및 합격 기준은 동일하다.

회귀 시험은59개10ms와1개300ms의60개 표본에서 p99=300ms를 요구한다. 수정 전 소스 overlay에서 실제10ms로 실패했고 수정본은 통과했다. 지연 후 Commit 실패 시 cursor/outbox가 전진하지 않으며 같은 높이 재시도는 성공하고, Commit 관측에 실패·대기 시간이 남는 것을 검증했다. disabled 경로의 계측은0이다. 캐시 수정 전 계측 변경만 적용한4CPU/CGO off/상태 준비 true/batch1의 비trace SLO는 일반150.8/비읽기106.7ms(-29%)로 통과했다. 한 표본의 통과를 이전 실패의 해소나 코드 속도 개선률로 해석하지 않는다.

추가 검토에서 `NewPebbleStorage`가 직접 만든 `pebble.NewCache`의 생성자 참조를 해제하지 않는 결함을 확인했다. Pebble Open/Close는 DB 자신의 참조만 관리하므로 생성자의1개 참조가 남았다. 캐시 생성 직후 defer Unref를 추가해 성공·Open 실패·초기화 실패에서 모두 소유권을 해제한다. DB가 실행되는 동안은 DB 자신의 참조가 유지된다. 캐시 크기·저장 형식·sync 방식은 바꾸지 않는다.

`TestPebbleCacheLifecycle`은 Pebble의 `invariants` 최종 참조 검사를 켜고 정상 종료, 잘못된 경로의 Open 실패, 손상된 transaction counter로 초기화가 실패하는 경로를 GC로 회수한다. 수정 전 `cache ... non-zero reference count: 1`로 프로세스가 실패했고 수정 후1회(1.126초)/20회(10.903초)가 통과했다. 내부 참조 필드를 reflection으로 읽거나 noisy heap 크기 기준을 사용하지 않는다. Pebble은 race 빌드에서 이 finalizer를 끄므로 시험은 `invariants && !race`이며 `make test-cache-lifecycle`과 기존 CI test job의 별도 step으로 실행한다. 새 CI job은 추가하지 않는다. 캐시 소유권 누락은 확인·수정된 독립 결함이며, 원래318ms I/O 지연의 유일한 원인 또는 메모리 증가의 양적 원인으로 단정하지 않는다.

계측·통계 수정 시점의 전체 race는App442.139초/fetch13.286초, PostgreSQL adapter49.901초/App199.755초로 통과했다. 이후 캐시 수명 수정이 추가되어 최종 전체 검증을 다시 실행한다. 최종 캐시 수정 소스의 빌드/lint(0 issues)/예제/전체 race(App475.010초, storage304.742초, fetch16.662초)/PostgreSQL(adapter46.090초, App185.512초)이 통과했다. PostgreSQL은 자체 tmpfs·fsync off의 기존 기능 시험 환경이며 디스크 성능·내구성 비교는 아니다.

기본 `make test-slo`의 첫 최종 실행은 전체 구독자 부하가 일반156.4/비읽기117.1ms(-25%)로 통과했지만 빠른 stream p99130.026ms/fast-path 이후44ms로 실패했다. 이10초 실패 구간에는 trace가 없다. 커밋·통계·캐시 수정으로 모든 OS 지연을 해소했다고 주장하지 않는다. 같은 빠른 알림 경로에 임시 writer/commit trace를 붙인5회 실행은 node→message p999.1/12.7/8.3/17.0/9.2ms, fast-path 이후3.5/7.6/1.1/2.6/1.6ms로 통과해 새130ms 실패를 재현하지 못했다. 통과한 trace로 실패 순간의 유일한 원인을 대신 설명하지 않는다.

단일CPU의64회 별도 동기화 probe는 동일한 F_FULLFSYNC 내구성 요청을 `os.File.Sync` 및 RawConn.Control 안의 x/sys/unix.FcntlInt로 각각 수행했다. 실행 가능한 CPU worker의 동기화 중 진행이0인 표본은61/64 대57/64, 중앙 진행량은 양쪽0이었다. maxSync17.416/17.567ms, heartbeat 최대간격21.396/23.544ms였다. 이 표본으로 명확한 스케줄러 개선을 입증하지 못했으며 별도Darwin FS wrapper나 sync 방식 변경을 채택하지 않았다. 합격 기준·부하·반복 횟수는 완화하지 않고 기본SLO를 재확인한다. 실패와 통과 표본을 모두 보존한다. 기본 SLO 재확인은App252.877초로 통과했다. 빠른stream node→message p9910.1ms/fast-path→message1.4ms, 일반 구독자 중앙p99156.1/비읽기161.6ms(+4%)였다. 첫130ms 실패를 지우지 않으며 반복 측정의 통과를 상시 안정성 보장으로 바꾸지 않는다.

최종4CPU/CGO off/상태 준비 true/batch1/진단 true 실행은 빠른알림은 통과했지만 일반 구독자 round1 p99377.993ms로 실패했다. 가장 느린 block27의 ingest362.918ms/최대수신400.511ms, 해당 라운드의 writer p99310.949ms와 독립Commit p99309.910ms를 기록했다. fetch p9914.655ms/준비0.792ms, 커밋 오류0·최종inflight0·state bytes0이었다. Commit 대기만250ms 예산을 넘는 저장 경로의 tail을 비trace 실행에서도 확인했다. 이 측정은 syscall 단독 원인을 더 분리하지 않지만 RPC나 상태 준비만의 병목으로 설명할 수 없다. 커밋을 기다리기 전 durable 이벤트를 보내거나 sync를 끄는 방식으로 기준을 맞추지 않는다. 캐시 누수/통계·진단 결함의 수정과 간헐OS I/O 지연의 해소는 구분하며 R07을 완료로 표시하지 않는다.

필수 기본 SLO/빌드/lint(0 issues)/전체race/PostgreSQL/예제와 캐시 수명20회, 새Commit 회귀race200(52.465초)/CPU1race200(45.657초)이 통과했다. 위4CPU 실패는 추가 운영 조건의 미충족 증거로 보존한다. 공개RPC 네 개와5HTTP/s, 운영config.yaml은 유지했고 다른 사용자의 프로세스·컨테이너는 변경하지 않았다.



### 3.23 10/15 macOS 블록별 undo·WAL 쓰기·동기화·교체 분리

main `5ab0b9b`의 남은 R07 저장 대기를 조사했다. 저장소 런타임 소스는 바꾸지 않고 Go overlay를 사용했다. 첫 실행은 FlightRecorder와 undo/Pebble Commit/WAL Write·SyncData region을 추가했고, 두 번째는 trace 없이 블록별 시각·`Batch.CommitStats()`·같은 DB의 raw WAL Write/Sync/SyncData/SyncTo 구간을 메모리에 모아 라운드 종료 후 저장했다. 파일 래퍼는 기존 파일의 모든 연산과 인자를 그대로 전달했다. 두 실행 모두 macOS/GOMAXPROCS4/CGO off, state prefetch true/batch1/diagnostics true, 1,000 구독자·10 trades/s·60블록×4라운드, 절대 p99 250ms·non-reader 상대 median +20% 기준을 유지했다. 실제 공개 RPC나 로컬 config.yaml은 바꾸지 않았다.

| 실행 | readers median p99 | non-readers median p99 | 상대 변화 | 결과 |
|---|---:|---:|---:|---|
| FlightRecorder | 100.6ms | 114.4ms | +14% | TestSLOLoad 통과, 242.21s |
| 블록별 메모리 기록 | 100.3ms | 106.2ms | +6% | TestSLOLoad 통과, 242.26s |

메모리 기록의 240개 부하 블록에서 undo 최대1.242ms, 커밋 최대30.237ms였다. 가장 느린 round2 block35는 undo0.715ms/Pebble29.518ms/공유 상태 게시0.004ms였다. Pebble 통계는 WAL 교체17.603ms/CommitWait11.246ms, 같은 커밋 구간에서 raw WAL 쓰기 합계0.266ms/SyncData 합계15.679ms(2회)를 기록했다. WAL 교체는 이전 WAL의 동기화·종료 및 새 WAL 생성 등을 포함하므로 raw sync 시간과 중첩한다. 두 수치를 더해 독립 비용처럼 계산하지 않는다. Memtable/L0 write stall, WAL queue wait, 커밋 오류는 모두0이었다. 현재 표본에서는 undo CPU 비용보다 저장 대기가 크며, 작은 파일 쓰기만이 아니라 WAL 교체와 내구성 대기를 함께 봐야 한다.

첫 실행의 ingest250ms/receipt300ms trace trigger는 발생하지 않았다. 이전 378ms 실패를 이번 8라운드에서 재현하지 못했으므로 그 실패의 fsync 단독 원인이나 해결을 새로 입증하지 않았다. 계측 부하·공유 호스트 조건이 있는 통과 표본을 성능 개선으로 비교하지 않는다. `TestNotificationLatency`는 이번 두 실행에 포함하지 않았으며 여러 물리 호스트나 공개 RPC의 운영 SLO를 검증한 것도 아니다. 추가 런타임 최적화는 이번 표본만으로 채택하지 않았고 sync/커밋 후 알림 계약은 유지했다. R07은 계속 미완료다.

원본·overlay·분해 결과: `/private/tmp/indexer-macos-commit-stages-20261015/{diagnostic.log,overlay.json,summary.json}` 및 `memory/{diagnostic.log,overlay.json,round-1-events.json,round-2-events.json,round-3-events.json,round-4-events.json,analysis.json,blocks.json,analyze.py}`. 임시 래퍼는 운영 빌드에 포함하지 않는다.


### 3.24 10/15 StableNet 조상 영수증 제거·해시별 epoch 재사용

R04의 확인된 낭비부터 줄였다. 기존 `Fetcher.BlockAt`은 모델 블록만 반환하면서 `fetchOnce`로 영수증과 불필요한 geth 뷰까지 읽었다. 선택적 `source.BlockReader`/`ReadBlock`을 추가해 RPC의 기존 profile 검증 Block 경로를 사용하고, `Chained`도 선택한 source의 블록 전용 읽기를 유지한다. 지원하지 않는 source는 기존 `BlockWithReceipts`로 돌아간다. 정상 ingest의 영수증 획득·일대일/순서·해시 검증은 바꾸지 않았다.

StableNet `EpochTracker`는 마지막 블록 하나 대신 최대256개의 **블록 hash+height**에 묶인 epoch 해석을 기억한다. 부모 hash를 잇는 전체 탐색이 성공해야 결과를 게시하고, epoch 블록의 본인 적용 집합과 다음 블록 집합을 구분한다. 탐색 중에도 가까운 조상의 가벼운 정보만256개까지 보관하며 전체 모델/거래를 붙잡지 않는다. 같은 높이의 다른 fork는 다른 해시 키이고, 이미 hash로 증명된 header metadata만 재사용한다. StableNet hash는 epochInfo를 포함하며 현재 round/seals만 걸러낸다. 계정 state 캐시나 writer의 fresh canonical 검증 생략은 추가하지 않았다.

WBFT의 시작 시 부모 epoch(`EpochOf`)와 현재 epoch(`For`)가 각각 같은 조상을 탐색하는 비용도 제거했다. 먼저 부모 모델의 높이·hash가 현재 블록과 연결되는지 검증해 같은 tracker의 `For(parent)`를 호출하고, 현재 블록이 그 부모의 next 집합을 재사용한다. 부모 모델은 signing timestamp에도 재사용한다. 부모의 epoch 집합이 자식부터 적용된다는 경계 계약은 회귀 시험으로 확인했다.

12개의 반복·순서 역전 블록을 같은 입력으로 비교한 회귀에서 조상 읽기는589→59회(약90.0% 감소)였다. 기존 main overlay는 영수증 요청0건과59회 기대값 두 시험을 실제로 실패했다. 3회 CPU 벤치마크 중앙값은74,421→16,430ns/op,41,746→29,203B/op,1,485→226allocs/op이었다. 이는 가짜 header reader의 요청 수·CPU 측정이며 실노드 전체 처리량 개선률이 아니다. 서로 다른 fork·실패 후 재시도·취소·epoch 경계·캐시 축출·1,024조상 탐색·잘못된 부모 모델·Source fallback/Chained 경계를 검증했다. 동시 fork 시험은 race200/CPU1 race200 각각 통과했다.

네 공개 테스트넷을 기존5 HTTP/s·state prefetch true/batch16·workers2/state_workers4·strict 정책·head-2·새 임시DB로 300초씩 두 번 실행했다. 첫 부분 변경의 StableNet 조상은333회/111높이/영수증0건, 최종 WBFT 통합 뒤는14회/7높이/영수증0건이었다. 이는 독립 Accounting/WBFT tracker 각각 한 번의 탐색이며 마지막 단계에서 높이당3→2회가 확인됐다. 두 표본의 시작 epoch까지 거리가111/7로 달라 시작 지연84초→10초나 절대 요청333→14를 코드 개선률로 계산하지 않는다. 최종 창은300.008초, 마지막 API 표본290.339초, 속도 창134.662초였다.

| 체인 | 시작 | 마지막 색인/head | 격차 | App HTTP/논리 RPC | 최근 생성/색인 blk/s |
|---|---:|---:|---:|---:|---:|
| stablenet-testnet | 22342467 | 22342758/22342760 | 2 | 730/1033 | 1.003/1.003 |
| bsc-testnet | 136818182 | 136818825/136818830 | 5 | 1415/2297 | 2.220/2.235 |
| sepolia | 11916101 | 11916127/11916127 | 0 | 496/1920 | 0.082/0.082 |
| arc-testnet | 67226868 | 67227249/67227445 | 196 | 1447/2718 | 1.968/1.352 |

최종 StableNet의 아직 보이지 않은 블록 한 조회는 `source: block not found` 뒤 같은 높이 재시도로 복구했다. 첫 창 Arc receipt quota(-32005)1건도 재시도로 진행했다. 최종 창의 Arc44건/Sepolia2건 `eth_call` revert는 정상 토큰 판별 응답이며 상태 소실/quota와 구분했다. 두 창의 근사 fallback 경고는0·종료코드0이고 자체 프로세스·proxy·임시DB는 정리했다. 실제 config.yaml/기존DB/5회 한도는 바꾸지 않았다. 최종 Arc 생성1.968/색인1.352blk/s·격차196은 계속 처리량 부족을 보여준다. StableNet 변경을 Arc 처리량 개선으로 주장하지 않으며 물리 batch/head/canonical/필수 잔액 비용을 R04에 남긴다. 전체 회계 oracle·days-long/다중 물리 호스트 SLO 증거로 확대하지 않는다.

검증: gofmt/build/lint0/예제/새 회귀 및 concurrency200회, 전체 race(app456.438초/storage291.364초), 격리 PostgreSQL(adapter106.708초/app306.126초)이 통과했다. PostgreSQL은 기존 기능 시험의 tmpfs/fsync=off이며 디스크 성능 증거가 아니다. 첫 기본 SLO는 fast19.4/3.9ms와 모든 라운드의 절대250ms를 통과했으나 readers/non-reader median139.3/169.9ms(+22%)로 상대20% 기준을 실패했다. 원본 `slo.log`를 보존했다. 기존 main 네 소스 파일 overlay의 같은 조건 비교는 fast166.7/2.4ms와 reader round2 p99895.695ms로 실패했고, 예정한 수정본 한 차례 재확인도 fast171.5/19.3ms·첫 pair의 readers857.4ms/non-readers2813.7ms로 실패했다. 기준·부하·표본 수는 바꾸지 않았고 추가로 통과할 때까지 반복하지 않았다. 비교 실패는 기존 소스에도 큰 지연이 있음을 보이지만 각각의 실패 원인을 동일하게 확정하지 않는다. 재확인 종료 직후 읽기 전용 호스트 표본은12코어/load average49.43·32.17·22.62, memory free31%, disk0 다음1초9526tps/47.67MBs였다. 공유 호스트의 CPU/I/O 경합 증거이며 정확한 실패 블록의 trace나 특정 프로세스의 인과 증거는 아니다. 다른 프로세스·설정은 변경하지 않았다. SLO는 미해결로 기록하고 변경은 draft PR로 정리해 병합하지 않는다. 원본/비교/관측 자료는 `/private/tmp/indexer-epoch-reads-20261015/`의 `regression-before.log`, `regression-final.log`, `benchmark-{before,after}.log`, `four-testnets{,-final}/{operating-window.json,summary.json,operating-window.log}`, `summary.json`에 보존했다.

## 3.25 Arc 연속 수집과 처리 큐 (10/15)

사용자는 Arc의 약2blk/s 생성률에 맞춰 첫 worker의 수집 목표를450ms로 두고, full node에서 소실될 입력을 먼저 확보한 뒤 큰 공유 메모리 큐로 다음 worker에 넘기도록 요청했다. 공개 RPC의 기존 체인별5 HTTP/s 제한과 strict 정책을 유지했다. 선택적 `acquisition_queue`를 추가해 작은 수집/재정렬 창(2*workers)과 기본256블록/256MiB의 순차 처리 큐를 분리했다. 슬롯은 커밋 후가 아닌 큐 handoff 후 반환하고 live 수집은 chunk_size 경계 없이 새 target까지 계속한다. 초기 잔액/생성 계약 메타데이터는 해시로 준비하고 writer의 fresh canonical 검사·원자적 commit·cursor/undo/outbox를 유지한다. 상한에는 처리 중인 블록도 포함하며 byte admission은 높이 순서로 수행해 교착을 막는다. 오류/취소/fork에서는 worker를 join하고 미커밋 큐를 버린다. reorg는 설정 시작 높이 아래라도 반환된 fork+1부터 재개한다. sparse range는 기존 전용 경로를 유지하며, 명시적 head 알림은 이전 관측 cache에 머물지 않고 새 조회를 시작한다. byte 상한은 직렬화 모델+준비 payload이고 별도 수집 창과 전체 heap/RSS의 상한은 아니다. 큐는 volatile이며 긴 중단 후 pruned state를 복구하지 못한다. 보정/legacy custom handler의 후속 상태 RPC는 남아 있고 소실되면 블록 전체 실패한다. 자세한 계약은 [설계](arc-acquisition-queue.md)에 기록했다.

검증: 커밋을 막아도 새 head와 chunk_size=1 경계를 넘어 수집하는 시험, count/byte backpressure·취소·낮은 높이 오류·fork/재시도·큐에 남은 old fork 롤백을 검증했다. 순차/준비/큐 경로의 EVM·recent start·StableNet 전체 저장 keyspace를 비교했고, 상태가 삭제된 후에도 미리 확보한 잔액을 사용했다. Arc의 독립 before/after 잔액5개 표본도 준비 handler에서 late state 조회 없이 일치했다. 수집 큐 race200(250.017초), 실제 pruning/queued fork race200(203.104초), 단일CPU200회(fetch243.900/app418.247/balance14.869초), 최종 head 알림 race200/CPU1 race200(39.772/50.519초)이 통과했다. 전체 race는 app563.644/storage380.718/fetch19.484초로 통과했고, 마지막 sparse guard는 별도 race200/CPU1 race200(20.903/18.054초) 및 최종 fetch 전체 race9.288초로 확인했다. 마지막 guard 뒤 build/lint0이 성공했고 SDK 예제 검증도 통과했다. 자체 Docker PostgreSQL(tmpfs/fsync=off)에서 큐 관련 App 회귀7.616초가 통과하고 컨테이너를 정리했다. 이는 PostgreSQL 전체 suite나 디스크 SLO 증거가 아니다.

네 공개 테스트넷을 새 임시 DB/head-2, workers2/state_workers4/state_batch16, 체인별5 HTTP/s로300.004초 동시에 실행했다. Arc에만 큐를 켰다. 처음 관측 도구의 출력 디렉터리가 없어서 서비스 시작 전에 실패한 로그를 보존하고 경로를 보완했다. 유효 관측은1회이며 종료코드0, 근사 fallback0이고 자체 process/proxy/임시DB를 정리했다. 마지막 API 표본은294.861초다.

| 체인 | 마지막 색인/head | 격차 | App HTTP/논리 RPC |
|---|---:|---:|---:|
| StableNet | 22349554/22349555 | 1 | 843/1145 |
| BSC | 136833927/136833929 | 2 | 1349/2067 |
| Sepolia | 11916693/11916693 | 0 | 590/4018 |
| Arc | 67240688/67240868 | 180 | 1454/2902 |

Arc의 동일 heartbeat210.107초 구간은 생성1.980/준비 완료1.423/커밋1.433blk/s였다. 마지막 heartbeat의 target/수집/커밋은67240864/67240688/67240687로 수집 격차176, 후속 처리 격차1이다. 이 표본을 약4블록 뒤의 API head와 섞지 않는다. 최근256개 수집 완료 간격p50/p95/p99는603.400/2130.822/2599.302ms, 병렬 job 하나의 acquisition p50은1395.286ms였다. DB commit p50/p99는5.982/45.922ms, 순차 write p50은611.858ms로 canonical RPC·limiter·writer 대기를 포함한다. 큐 최고4블록/126933 payload bytes, 프로세스 최고RSS175568KiB였다. 큰 큐 상한에 막힌 창이 아니며, 먼저 수집이 head에서 뒤처진다. 획득 높이는 연속 준비 완료이며 불필요해질 speculative 읽기 오류가 남을 수 있어 정확한 commit과 구분한다.

Arc의 물리 요청은 블록+영수증418, 초기잔액377, canonical374, head147, code64, call71, chainId3이었다. 필수 실패/HTTP 오류/quota 오류는0이고 정상 토큰 판별 eth_call revert(code3)65건을 구분했다. 신규 계정이 있는 블록은 통상 수집1+상태1+canonical1 요청이므로2blk/s에 기본6 HTTP/s가 필요하고 head/메타데이터도 더해진다. 상태 배치를 이미 사용해도 현재 요청 분리로는5 HTTP/s 안에서450ms·지속 추종을 달성하지 못했다. 큐만 늘리거나 DB commit만 최적화하는 것으로 이 창의 주된 병목을 해결하지 못한다. 다음 후보는 동시에 대기 중인 독립 읽기 RPC를 같은 envelope로 합치는 것이다. fresh canonical 검사 생략·hash 상태 공유·최신/0 대체·한도 증가는 선택하지 않았다. 이전 창과 live input이 달라 처리량의 인과적 개선률로 비교하지 않는다.

로컬 ignored config.yaml에는 Arc만 acquisition_queue=true/256블록/256MiB/state_batch_size16을 적용했다. 기존 DB/시작 높이, 다른 체인 설정, global state_batch_size1, RPC5는 유지했다. 첫 운영 시작 전 상태/DB cursor 재확인 조건은 그대로이며 전체 회계·days-long 운영·remote host/450ms SLO 완료로 간주하지 않는다. 관측 도구·원본·source/binary manifest·회귀 로그는 `/private/tmp/indexer-arc-queue-20261015/`에 보존했다.

관측 뒤 별도로 queue/state prefetch/batch16·GOMAXPROCS4의 native `make test-slo`를1회 실행했다. 부하·표본 수·기준을 낮추지 않았고 반복해 green 결과를 고르지 않았다. fast stream node→message p99210.4ms(기준20), fast-path→message p9942.3ms(기준10)를 초과해 알림 시험이 실패했다. load의 reader/non-reader p99는 round1 91.1/210.8ms, round2 556.5/105.4ms였고 reader round2가250ms를 넘어 실패했다(app253.688초). 상대20% 판정이 수행됐다고 주장하지 않는다. 기존 R07의 native SLO는 여전히 미해결이며, 이번 결과만으로 실패 원인을 queue나 특정 host I/O로 확정하지 않는다. 원본 `slo-queue.log`를 보존했고 source/config는 review 가능한 working tree에 남겼다. 새 commit/PR/merge는 하지 않았다.

로그의 초기 API server error1건, StableNet 아직 보이지 않는 블록22349408 조회1건(재시도 후 진행), 종료 시 BSC head query context canceled1건을 분리했다. Arc의 필수 실패는 없었다. 종료 뒤 단일 touched 계정의 Arc 원래 hash 및 head-0/32/128/512 조회가 성공했으나 전체 상태 보존 보장은 아니다. BSC의 원래 hash와 head-128/512는 실패하고 head-0/32는 성공했으며, Sepolia의 head-512는 실패했다. 이 표본은 full node 상태의 처리 중 확보가 필요한 이유와 한계를 보여준다.


## 3.26 독립 RPC HTTP 배치 통합과 Arc 재측정 (10/15)

`rpc.coalesce_reads`(기본false)와 체인별 override를 추가했다. 로컬 config.yaml은 Arc만 true이고 RPC5·기존 DB/시작 높이·다른 체인 설정은 보존했다. `pkg/rpcpool`의 읽기 allowlist에 해당하는 동시 대기 envelope를 10ms 창과 limiter 대기 중 모아 최대64멤버로 전송한다. 기존 single/batch를 펼치고 고유 ID를 부여한 뒤 원래 ID·응답 모양으로 복원한다. 인증/header가 다른 요청, 쓰기, notification, 알 수 없는 메서드는 합치지 않는다. 128-envelope 대기열·동시 그룹4개, 원본 통합 대상1MiB 상한과 cancellation/Close join을 구현했다. 멤버 오류는 보존하며 missing/duplicate/unknown 응답을 성공으로 만들지 않는다. 명시적인 envelope-level -32600/null-ID만 원본 재시도로 돌리고 이후 통합을 끈다. recorder/replay·비HTTP에는 적용하지 않는다.

기존 limiter가 endpoint failover loop 밖에서 한 번만 대기하던 부분도 수정했다. 이제 failover·미지원 배치의 원본 재시도를 포함해 **모든 실제 HTTP 시도**가 같은 limiter를 거친다. canonical 결과를 cache/share하지 않는다. writer가 필요할 때 새 조회 멤버를 보내고 준비된 hash와 비교한다. canonical·EIP-1898·필수 상태 실패 시 원자적 abort/재시도 정책은 그대로다.

새 물리 관측은 `/private/tmp/indexer-arc-coalesce-20261015/four-testnets/`에 원본을 보존했다. 4개 공개 테스트넷 동시300.005초, 체인별5HTTP/s(app+observer 공유), workers2/state_workers4/state_batch_size16, Arc queue256블록/256MiB, 최근 head-2의 새 임시 DB, 종료exit0 조건이다. 전역 state_batch_size16은 이전 관측과 비교하기 위한 임시 조건이며 실제 config의 global1/Arc override16은 보존했다. 필수 상태 approximate fallback은0이었다. 물리 입력이 시간에 따라 달라 동일-input 인과 비교나 장기 운영/SLO 검증은 아니다.

| Arc 지표 | 이전 queue 창 | 배치 통합 창 |
| --- | ---: | ---: |
| 최초60초 이후 heartbeat 구간 | 64.613–294.720s | 62.973–286.998s |
| 생성률 (blk/s) | 1.982 | 1.982 |
| 수집률 (blk/s, 재시도 시간 포함) | 1.425 | 1.977 |
| 커밋률 (blk/s, 재시도 시간 포함) | 1.421 | 1.982 |
| 마지막 heartbeat 수집 / 처리 격차 | 176 / 1 | 40 / 3 |
| App HTTP / 논리 멤버 | 1454 / 2902 | 794 / 4289 |
| active 수집 간격 p50 / p95 / p99 | 603 / 2131 / 2599ms | 201 / 605 / 1194ms |

비교 구간은 양쪽 모두 **process 시작 wall clock +60초 이후의 첫 heartbeat**부터 마지막 heartbeat까지다. 이전 helper처럼 최초 heartbeat 자체를 기준으로60초를 추가하면 오류/재시작으로 heartbeat가 건너뛴 구간을 더 제외해 수집/커밋률을 높게 보일 수 있어, 이번 요약 도구는 고정된 시작 시각 기준으로 수정했다. 생성률과 수집/커밋률은 같은 heartbeat 높이와 시간으로 계산한다. 마지막 동일 heartbeat의 target/acquired/committed는67244229/67244189/67244186이다. 별도의 마지막 API 표본은 node67244235/indexed67244187로48이다. API 첫60초 이후 구간(이전62.651–294.861s, 새62.196–289.678s)의 생성률은1.977/1.974, 커밋률1.421/1.965blk/s였다. API와 heartbeat 시간을 섞어 gap을 계산하지 않는다. **active acquisition_interval은 pipeline 재시작 시 기준 시간이 초기화되어 실패 후 retry 대기 전체를 포함하지 않는다.** 201ms 중앙값을 안정적인450ms 처리/SLO 통과로 해석하지 않는다.

배치 통합 창은 HTTP 모두200이지만35개 envelope 안의115멤버가 quota(-32005)를 받았다: blockReceipts33, balance75, code6, call1. 정상 token 판별 revert(code3)110건과 구분한다. 작은3–5멤버 envelope에서도 quota가 발생해 단순한 최대 batch 크기 초과만으로 설명할 수 없다. HTTP5 안의 멤버/메서드 조회량·burst 또는 backend 추가 quota를 시사하지만 실제 정책/임계값은 확인되지 않았다. 오류 없는 구간의 catch-up은 빨라졌으나 필수 실패 때문에 블록 전체 retry가 남고 안정적인450ms·지속적인 head 근접 보장을 달성하지 못했다. 더 큰 큐나 fresh canonical 생략으로 숨기지 않는다.

Arc mixed-method HTTP666개, canonical 헤더 멤버536개(독립 envelope15개 포함), 최대 envelope27멤버를 기록했다. 즉 canonical 실제 조회를 유지하며 다른 독립 요청과 같은 HTTP에 보냈다. writer p50/p95/p99203/400/575ms, DB commit6.385/40.287/63.592ms, queue 최고9블록/160272payload bytes, 4체인 RSS 최고182656KiB였다. 종료 뒤 Arc 단일 touched 계정의 원래 hash/head-0/32/128/512는 모두 조회됐지만 전체 상태 보존 보장은 아니다. BSC 원래 hash/head-128/512, Sepolia head-512는 실패했다.

동시성·ID/shape·멤버 오류·취소/종료·batch 미지원·멤버 상한/인증 분리·failover 제한 시험은 race200(35.488초)과 GOMAXPROCS1/race200(37.757초)를 통과했다. 실제 앱의 ordered/prepared/queued/coalesced 저장 keyspace·잔액 일치, 준비 뒤 pruning, 큐 fork 폐기/재처리·체인 설정 회귀는 race1(12.064초) 통과했다. build와 lint(0 issues)를 확인했다. 전체 `go test -race -count=1 ./...`도 통과했다(app440.223초/storage276.089초/fetch15.200초/rpcpool3.999초). 그 뒤 runtime 수정 없이 작은 envelope의 서로 다른 인증 헤더를 분리하는 추가 회귀를 race200(6.422초)/CPU1race200(6.000초)로 확인했다. 예제는 receipts12.307초/chainprofile통과, PostgreSQL 관련 acquisition/prepared-state 앱 회귀8.784초 통과였다. PostgreSQL은 임시 tmpfs/fsync=off로 기능을 검사한 것이며 durable 운영 성능/SLO 근거가 아니다. 컨테이너는 helper의 trap으로 제거했다. 최종 lint0/gofmt 차이0을 확인했고 observed runtime Go source·binary hash는 그대로다. R07의 이전 native SLO 실패는 해소로 표시하지 않으며 이번 변경에서 SLO를 반복해 green 결과를 고르지 않았다.

EN 방향은 [node-backed acquisition options](node-feed-options.md)에 정리했다. Arc 공식 노드는 Malachite block 검증/동기화와 Reth 실행/state/RPC가 함께 필요하다. 독립 node-feed adapter와 local RPC baseline 후 execution-extension/IPC stream을 권장한다. RPC server 모듈만 떼어서는 동기화/receipt/state가 생기지 않는다. generic Reth ExEx를 Arc fork에서 바로 쓸 수 있다고 가정하지 않으며 CPU/disk/초기 sync·상태 retention/ACK·canonical/reorg를 검증해야 한다. 자체 노드는 아직 구성하지 않았다.

## 3.27 Arc ExEx node-feed 모듈 구현 (10/15)

사용자는 공개 RPC5·fresh canonical 검사를 유지하면서 Arc ExEx 지원 확인과 독립 node-feed 모듈 구현을 요청했다. Git은 branch 이름의 `:`를 허용하지 않아 `refactor:arc-node-feed` 대신 `refactor/arc-node-feed`를 생성했다. `codex` prefix는 사용하지 않았다. 원래 `perf/arc-acquisition-queue`의 HEAD `a543046ef4eeb00c257c01b2b3061bbd6ea2787e`와 기존 미커밋 queue/coalescer 소스를 먼저 `/private/tmp/indexer-node-feed-20261015/baseline/` 및 SHA256 manifest에 보존했다. 기존 변경과 이번 변경이 working tree에 함께 있으므로 별도 node-feed-only patch도 보존하며, baseline commit이나 사용자 handoff 문서 변경은 하지 않는다.

공식 Arc `6e764023ee6515fe70573e123ed2db912a7207b4`를 확인했다. 별도 Reth fork가 아니라 upstream Reth v2.2.0 SDK(`88505c7fcbfdebfd3b56d88c86b62e950043c6c4`)를 사용하며 `ArcNode::Primitives = EthPrimitives`다. stock 바이너리는 ExEx를 등록하지 않지만 NodeBuilder의 `install_exex_if`를 사용할 수 있다. `modules/arc-node-feed` 독립 Rust crate와 검증 revision 전용 idempotent 설치 도우미를 추가했다. Arc와 같은 git/tag Cargo source identity를 사용하고 lock으로 revision을 고정했다. 별도 임시 Arc checkout에 실제 hook을 적용한 **Arc 실행 바이너리 cargo check**가 통과했다. Arc CL/EL을 실행하거나 체인을 동기화한 시험은 아니다.

ExEx 실행/reorg notification의 block/receipt를 bounded hot cache에 공유하고 Unix IPC로 Go Source에 전달한다. hot cache miss는 같은 실행 노드의 native provider에서 읽는다. native 잔액·보정·lazy genesis는 hash-bound StateProvider를 사용하고, token metadata 등 남은 외부 조회와 fresh canonical은 기존 공개 RPC·5 HTTP/s를 유지한다. startup은 native/local과 metadata/public provider를 각각 검사한다. Go는 RLP/height/hash/parent/transaction trie root/receipt trie root를 확인하고 기존 PrepareBlock→bounded queue→순차 writer에 연결한다. source를 전역으로 다른 체인에 상속하지 않는다.

producer는 수집/준비만으로 ACK하지 않는다. block/cursor/outbox의 원자적 DB commit 뒤 ACK하며, local canonical hash도 확인한 뒤 부모 높이만 FinishedHeight로 보내 다음 블록의 부모 상태를 보존한다. ACK 실패는 durable commit을 되돌리지 않으며 재시작·재연결·동일 DB consumer identity·유실 ACK 재검증을 지원한다. 최근 cache8블록/32MiB, 미ACK256블록/256MiB payload, frame/worker 상한과 backpressure를 둔다. native I/O/응답 인코딩과 notification 인코딩은 async node executor 밖의 bounded blocking 작업이다. 큐의 전체 RSS 상한이나 무제한 과거 상태 보존을 보장하지 않는다. 상세 설치·Docker kernel/UID 조건·복구 계약은 [NODE_FEED](../NODE_FEED.md)에 기록했다. 실제 config.yaml은 사용 가능한 producer가 없으므로 활성화하지 않았고 기존 RPC/DB/시작 높이를 보존했다.

검증 결과:

- 최종 전체 `go test -race -count=1 -timeout=20m ./...` 통과(App461.184초/storage313.522초/fetch17.028초). 초기 전체 실행의 config 시험은 먼저 발동하는 기존 state_prefetch guard의 오류 문구와 새 assertion이 달라 실패했으며, 실제 guard를 약화하지 않고 assertion을 고친 뒤 전체를 다시 통과했다. 초기 로그도 보존한다.
- 최종 Source 취소/재연결·frame/identity/trie 검사 race200은90.600초, GOMAXPROCS1/race200은61.940초로 통과했다. App의 state 실패·queued fork 동시성은 race200/CPU1race200 각각99.376/97.567초 통과했다.
- Rust ACK/pruning/fork/consumer 계약2개 통과. 실제 Rust fixture→Go App 저장·잔액 interoperability2.924초 통과. fixture는 frozen oracle이며 실제 Arc node가 아니다.
- 독립 Rust crate의 reth feature 검사와 실제 Arc 실행 바이너리 hook 검사 통과. 최종 Arc check3.91초. installer 재실행은 대상 두 파일 hash를 바꾸지 않았다.
- 격리 PostgreSQL의 관련 NodeFeed App 시험3.379초 통과. 임시 tmpfs/fsync=off의 기능 시험이며 운영 내구성·디스크 성능 비교가 아니다. 자체 컨테이너는 trap으로 제거했다.
- build/lint0/예제(chainprofile 및 receipts14.423초)/cargo fmt/gofmt/diff whitespace 검사 통과.

원본 로그·고정 revision/hook 자료·소스 hash·baseline 대비 patch는 `/private/tmp/indexer-node-feed-20261015/`에 보존한다. 이번에는 공개 테스트넷 처리량을 재측정하지 않았고 실제 Arc node sync·450ms 수집 목표·장기 retention/디스크 증가·live reorg/SLO는 아직 검증하지 않았다. R04/R07을 완료로 표시하거나 이전 RPC 표본의 수집률을 node-feed 성능으로 바꾸지 않는다. 기존 draft PR·다른 사용자의 프로세스·컨테이너는 변경하지 않았다.

## 3.28 Arc node-feed 기록 기반 처리량 비교 (10/15)

사용자는 구현 후 성능 측정을 요청했다. 현재 host는12CPU/64GiB RAM, 내부 disk 여유약152GiB이며 실행 중인 Arc node/Arc Docker container/설치 binary는 확인되지 않았다. 공식 요구사항과 최신 snapshot API/manifest를 읽었다. snapshot67243540은 state 추출116.0GiB+headers33.4GiB만149.4GiB이고 CL·압축 파일·WAL 여유가 추가로 필요하므로 내부 disk에 bootstrap 다운로드를 시작하지 않았다. 이미 동기화된 노드/별도 저장 경로를 질문하고, 답변에 의존하지 않는 동일 입력 비교를 먼저 수행했다. 다른 사용자의 프로세스·컨테이너·외장 disk는 변경하지 않았다.

보존한 실제 Arc RPC archive에서67019625..67019656의32블록/174거래를 사용했다. HTTP baseline은 재생 source+production5HTTP/s+coalescing, candidate는 release Rust fixture IPC이며 native state는 hash로 고정했다. workers2/state_workers4/batch16/queue256·256MiB와 production feature, 새 durable Pebble DB는 동일하다. App/DB 시작·RLP 변환·종료·정확성 대조는 제외했다. RPC→IPC/IPC→RPC/RPC→IPC 세 쌍을 순차로 실행하고 모두 보존했다. 실제 Arc CL/EL·ExEx 실행 중 native DB 부하·live head 추종을 측정한 것은 아니다.

초기 시험은 기존 archive의 높이 기반 상태/EIP-1898 인코딩 차이와 미기록 speculative parent-state를 발견했다. RPC의 멤버별 실패와 fixture의 batch 실패 차이로 incomplete oracle을 성능 자료로 사용할 수 없었다. Arc NativeDeltas의32블록 잠재적 parent-state 목록을 검사하고 빠진77개 hash/계정 값을 실제 공개 RPC에서 추가 확보했다. context3회·상태3회·최대4멤버의 bulk19회로25 HTTP/80논리 요청을 사용했다. bulk는1 HTTP/초, 오류·최신/0 대체0건이다. 이 비용은 타이밍에서 제외하고 입력을 동결했다. 초기 실패·요청 원문·snapshot metadata도 보존한다.

| 중앙값 / 조건 | RPC 재생+배치 | Rust IPC fixture |
| --- | ---: | ---: |
| 32블록 시간 | 7.583s | 6.802s |
| 커밋 blk/s | 4.220 | 4.704 |
| acquisition p95 | 799.8ms | 395.4ms |
| HTTP / 논리 멤버 | 38 / 309 | 34 / 44 |
| queue 최고 블록 | 4 | 28 |

처리량+11.5%/시간-10.3%/논리 요청-85.8%다. IPC가 제거한 full block/receipt/native balance 조회와 달리32 canonical header/4 code/8 call은34 HTTP에 남고5 HTTP/s 예산을 계속 쓴다. 기존 batching baseline의 HTTP 감소가4개뿐이라 전체 이득은 수집 지연 감소보다 작다. Rust IPC의 logical native balance RPC는0, fresh canonical 실제 HTTP 멤버는32였다. canonical 응답 자체는 동결된 replay이므로 live fork 검증 자료가 아니다.6회 모두 저장 keyspace와 독립 end-state 잔액 일치, 미기록 RPC0건이다. p95 표본이450ms 이내인 것을 운영 SLO로 표시하지 않는다. 이전4체인 공개 RPC 수집1.977blk/s와 현재 수치의 직접 비교도 하지 않는다.

원본은 `/private/tmp/indexer-node-feed-measure-20261015/{results.json,summary.json,measurement-final-complete.log,requests-round*-*.json,measurement-input.json,fixture.json,public-native-freeze.jsonl,snapshot-manifest.json,snapshot-sizes.json}` 및 입력 준비 script에 보존했다. [측정 계약·재실행](../NODE_FEED.md#동일-arc-기록의-전송-경로-비교-1015)을 참조한다. production Go 소스는3.27 전체 race 검증 source SHA와 동일하며 이번에는 opt-in 측정 코드와 fixture의 strict oracle 옵션만 추가했다. 별도 디렉터리의 race 측정 도구·Rust interoperability 검증57.928초, Rust 계약 시험2개, lint0/cargo fmt/gofmt/whitespace 검사가 통과했다. race 실행의 시간은 성능 표에 포함하지 않았다. measurement-only.patch와 source/input/binary SHA manifest를 같은 자료 디렉터리에 보존한다. 실제 Arc node 운영 성능·장기 retention·R04/R07은 계속 미완료다.

## 3.29 Arc 검증용 노드·빌드 산출물 정리 (10/15)

사용자가 노드 종료·제거와 용량 확보를 요청했다. 실행 중인 Arc node/fixture/cargo/rustc 및 관련 Docker container는 없었고 실제 chain snapshot을 다운로드한 적도 없다. 이번 작업이 만든 `modules/arc-node-feed/target`과 `/private/tmp/indexer-node-feed-20261015/{arc-node,reth}`만 삭제했다. 삭제 대상의 할당 용량 합계5.769GiB, 같은 filesystem의 관측 여유는150.913→156.469GiB(+5.556GiB)였다. 공유 host의 파일 변동이 있으므로 삭제 합계와 free 변화량을 같다고 기록하지 않는다.

구현 소스/Cargo.lock·기존 config.yaml/DB·측정 결과/원본 입력/로그/manifest/patch와 다른 컨테이너·공유 Cargo/Go cache는 보존했다. 삭제 직후 Git source status가 같음을 확인했다. 정리 기록은 `/private/tmp/indexer-node-feed-measure-20261015/cleanup.json`이다. Rust fixture를 다시 실행하려면 `cargo build`가 필요하고, installer 검증용 Arc checkout도 고정 revision에서 다시 확보해야 한다. 실제 노드 구축·동기화·추가 운영 측정은 사용자가 다시 요청할 때까지 진행하지 않는다.

## 3.30 24시간 윈도우와 원천 DB 설계 (10/15)

사용자는 노드 전체 실행이 아닌 프로젝트별 네트워크 기술을 재사용한 수신·동기화와 sliding window를 원한다고 명확히 했다. 원천 정보를 DB에 저장해 account/contract/storage/transaction/receipt/log/trace/blob를 조회·종합하도록 하고,1블록/초 기준86,400블록의24시간 보관을 설계 기준으로 승인했다. [windowed-feed-design](windowed-feed-design.md)에 현재 설계를 작성했다. Arc 약2블록/초에는172,800블록을 시간 기준으로 보관하며, 현재 메모리 queue의65,536 상한을 늘리는 방식은 사용하지 않는다.

기본 흐름은 체인별 peer/relay→durable disk window→원천 DB completeness/원자적 commit→DB 기반 후처리→조회 API다. 데이터별 identity·원본 encoding·계정/slot 이력·trace path·blob 참조와 영구 객체·provider/proof/scope를 정의했다. 원격 상태가 사라지기 전에 필수 입력을 확보하고, 미확보/unsupported/pruned와 검증된 빈 결과를 구분한다. 원천 DB 저장 ACK와 projection ACK를 분리하며24시간/완전성/finality/generation/읽기 lease를 모두 만족해야 삭제한다. 처리율 부족·바이트 한도에서는 미처리 원본을 버리지 않고 수집을 제한한다.

재시작·중복·ACK 유실·fork·undo 범위 밖 재구축과 API별 완료 높이, 독립 consumer의 cursor/transaction 전환을 정의했다. 체인별 module/composition 실행 프로젝트와 기존 Source/Feature/port 연결, W0..W7 구현 및 수용 기준을 정리했다. 공개 RPC5와 fresh canonical·strict 상태·기존 undo/outbox를 유지한다. source 자료를 완전히 저장한 뒤에는 ABI/집계 후처리 지연이 원본 삭제를 막지 않는다. DB만으로 수행할 수 없는 legacy worker는 별도의 raw-only ACK로 차단한다.

저장 바이트 예산은 실측 후 배정한다. 예를 들어 평균 payload256KiB에서24시간 원본은1블록/초21.09GiB,2블록/초42.19GiB이며 DB/객체/compaction 비용은 추가다. 설계 YAML의32/64GiB는 예시이며 실제 config에 적용하지 않았다. Arc 공개 연결 방식과 Ethereum의 consensus/ETH 역할, trace/BAL/blob 공급은 원문 명세에 연결했고 BSC/StableNet의 구체적인 protocol 지원은 W0에서 입증하도록 구분했다.

이전 ExEx 문서에는 과거 비교 경로라는 표시와 새 설계 링크를 넣고 README/ARCHITECTURE/CLAUDE/open-items를 갱신했다. 설계 문서·링크·수량/용량 계산·whitespace를 검사했다. 런타임 소스·config.yaml·DB는 수정하지 않았고 노드 설치/실행·대용량 다운로드·Rust 재빌드·성능 시험은 수행하지 않았다. 이전11.5%의 fixture 결과를 새 P2P/window 구조의 성능으로 사용하지 않는다.

## 3.31 외장 디스크 저장 경로 설계 (10/15)

사용자의 외장 디스크 활용 요청을 반영해 [윈도우 설계](windowed-feed-design.md)에 체인별 `window.path`, 외장 `volume_root`와 저장소 identity, 별도 영구 객체 경로를 추가했다. 원본/임시 파일/compaction이 지정 저장소 안에서 동작하고, 외장 볼륨이 없거나 분리됐을 때 내부 디스크로 자동 저장하지 않도록 정의했다. 재연결 시 identity와 segment/manifest를 확인해 재개한다. Docker bind mount와 native 경로 예시를 구분하고 W1 완료 기준에 mount/분리/권한/다른 볼륨 검증을 포함했다. 실제 외장 경로 지정·데이터 이동·런타임 구현은 수행하지 않았다.

## 3.32 남은 코드 수정과 윈도우 설계 통합 (10/15)

사용자는 설계를 코드에 반영하기 전에 남은 목록의 코드 수정을 새 설계와 함께 리팩토링하도록 요청했다. 현재 source/메모리 큐/기존 처리 commit ACK/app 조립을 대조하고 open-items에 통합 순서를 정리했다. R03/R04/R07을 W0 계약·공급 가능성→W1 외장 경로/디스크 윈도우→W2 원천 저장→W3/W4 adapter/기존 writer 연결→W5/W6 포괄 데이터/독립 후처리→W7 장기 검증으로 연결했다. 계측·회귀 시험은 각 단계에서 함께 반영하고 실제 SLO 원인이 미확인된 항목은 남긴다. 기존 RPC5/fresh canonical/strict 상태/undo/outbox를 유지하며 R05/R06의 외부 조건, R08의 재현 조건, R09/R10과 보류 범위는 별도로 두었다. 이번 변경은 통합 구현 순서에 대한 문서 변경이며 런타임 코드·설정·DB는 수정하지 않았다.

## 3.33 수집 공통 계약과 SQL 파라미터 바인딩 (10/15)

통합 리팩토링의 첫 변경으로 `pkg/core/model/capture.go`에 chain ID/genesis/block hash identity, plan version/digest, dataset·범위·완료 상태·출처/검증 종류와 manifest 검사 코드를 추가했다. plan digest는 요구 순서와 무관하며 호출자 slice를 수정하지 않는다. tracked 주소/slot 집합은 selection hash로 구분하고 필수 입력 누락·pruned·unsupported·미저장·검증 없는 빈 결과는 완료로 인정하지 않는다. 검증 성공 자체는 durable commit이나 삭제 ACK가 아니며 DB의 현재 generation fence·객체 저장·canonical 검사·원자적 commit은 후속 단계다.

`pkg/chainfeed`에 모듈용 adapter/stream/capability·bounded 입력 계약을 추가하고 지원 범위를 낮춰 요청을 통과시키지 않도록 했다. 기존 Source의 method set을 유지하면서 `CanonicalReader`를 분리하고 ExEx 비교 adapter의 canonical 의존성을 좁혔다. 공개 RPC 5 HTTP/s·fresh canonical·기존 처리 commit ACK의 실행 의미는 유지했다. 실제 네트워크 adapter의 공급 가능성 감사, capture storage port/schema, 외장 디스크 윈도우와 런타임 연결은 아직 남아 있다.

SQL 인젝션 방어는 [OWASP](https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html)와 설치된 pgx v5.7.5의 실행 코드를 확인했다. 기존 PostgreSQL의 검색/저장 값은 이미 대부분 바인딩했다. LIMIT/OFFSET·outbox/token 검색 limit·KV 배치 limit도 바인딩으로 통일했고, 연결 설정을 분리해 기본 prepared cache는 유지하고 DSN simple_protocol은 서버 바인딩 exec로 전환했다. schema는 허용 규칙과 드라이버 식별자 처리, 정렬식/방향은 코드에 정의한 값만 사용한다. 새 dataset 이름도 고정 enum이고 향후 원천 SQL에는 같은 규칙을 적용한다.

재색인의 catalog table 이름 수동 따옴표 처리는 schema-qualified `pgx.Identifier`로 교체했다. 시험 전용 DB에서 `x"; TRUNCATE abis; --` 이름의 실제 테이블을 생성하고 이전 처리만 되돌린 overlay로 실행하자 보존해야 할 ABI가 삭제돼 새 회귀 시험이 실패했다. 수정본은 통과했다. catalog 객체 생성 권한이 필요한 재색인 경로의 재현이며 공개 API 입력만으로 동일하게 공격할 수 있다는 주장은 하지 않는다. SQL 형태의 record table/key/fields·KV·token 검색어·outbox consumer·위조 cursor도 실제 PostgreSQL에서 확인했고, 기본 cache와 simple→exec 경로 모두 race 시험을 통과했다. 보안 시험은 fsync 기본 설정의 임시 PostgreSQL에서 수행했다.

수정 패키지 단위 시험, core/model·chainfeed·source 전체 race와 go vet를 통과했다. 전체 일반 시험의 app은563.009초, storage는467.062초에 통과했으나 GraphQL keep-alive가 실패해 한 번의 전체 실행 성공으로 기록하지 않는다. 이후 GraphQL 패키지 race3회는13.263초에 통과했다. fsync 기본값의 PostgreSQL 전체 race는 누적10분 제한에서 KV 시험의 초기 migration 중 종료됐고, Pebble 기반 app 전체 race도10분 제한으로 미완료다. timeout stack에 migration/파일 sync 대기를 확인했으나 모든 지연의 유일한 원인을 확정하지 않는다.

기존 `scripts/test-postgres.sh`와 같은 fsync=off 기능 시험 설정을 시험 전용 DB에만 적용한 재실행에서 PostgreSQL 전체 port/undo/SQL race는43.333초에 통과했다. 이 결과를 durability나 운영 SLO 증거로 사용하지 않는다. 별도 fsync 기본값의 PostgreSQL app 회귀에서 schema/API method·멀티체인 declared·strict balance/token 상태 소실이39.180초에 통과했고 Pebble 전용 golden 두 항목은 명시적으로 skip됐다. 실제 config.yaml·기존 DB·공개 RPC 한도·운영 fsync는 변경하지 않았고 full node 설치/실행도 하지 않았다. 시험 기록은 `/private/tmp/indexer-capture-sql-20261015/`에 보존하며 시험 전용 컨테이너는 제거한다.

## 3.34 원본 디스크 윈도우 저장소 코어 (10/15)

통합 계획 W1의 공통 저장소를 `pkg/window`에 구현했다. 명시적 volume 초기화/Create와 기존 저장소 Open, 지정 경로·mount/marker/디렉터리 identity·단일 writer 잠금, 원본 frame append/fsync와 닫힌 segment의 정렬 디스크 색인, 마지막 미완성 frame 복구를 제공한다. 기존 record의 재수신은 최초 수신 시각을 바꾸지 않고 bytes/codec 충돌은 거부한다. header 길이와 metadata/payload를 checksum으로 검증해 손상을 미완성 쓰기나 빈 결과로 바꾸지 않는다. 닫힌 record 전체를 RAM에 올리지 않고 활성 참조·색인 읽기·payload/reader 수를 제한한다. x/sys는 이미 사용하던 v0.37.0을 직접 의존성으로 이동했고 새 버전/노드 의존성은 추가하지 않았다.

읽기 lease는 payload I/O 전에 예약하고 append mutex 밖에서 읽는다. segment seal 뒤에도 lease를 보존한다. byte/segment/읽기 한도와 최소 free space에서 역압력을 반환하며 처리 전 자료를 evict하지 않는다. 24시간 경과만으로 GC하지 않고 DB coordinator가 generation fence 안에서 필수 plan/영구 객체/finality/raw-only ACK를 승인해야 한다. 새 lease·숨긴 오류·늦은 callback을 다시 검사하며 승인된 GC의 durable intent를 먼저 쓴다. GC 도중 중단돼도 삭제를 마치고 segment 번호를 재사용하지 않는다. `Page`는 원본 append 순서를 bounded 참조로 재개하며 canonical 연속 높이와 구분한다. 수거된 cursor는 DB 복구가 필요하다는 오류를 반환한다.

macOS 전체 저장소 race5.487초, CPU1의 append/read/GC와 lease 경합50회31.547초를 통과했다. fsync 반환 뒤 참조 게시 전 자식 프로세스 종료, partial header/payload·checksum 손상·영구/메모리 한도·경로/marker 교체·중복/충돌·GC/읽기 경합·cursor 만료를 검증했다. 공통 model/chainfeed/chain coupling 시험과 go vet, 전체 Go 패키지 컴파일도 통과했다. native의 실제 mount 시험은 환경변수 미지정으로 skip하고 Linux/arm64 정적 시험 바이너리를 기존 postgres:16-alpine 이미지에서 DB 실행 없이 검증했다. 실제 bind mount 등록/읽기/쓰기와 읽기 전용 bind의 writer 거부가 통과했다. 네트워크 없는 임시 컨테이너를 사용하고 자동 제거했다. 검증 기록은 `/private/tmp/indexer-window-store-20261015/`다.

현재는 [저장소 패키지 API](../WINDOW_STORAGE.md)까지다. 기존 cmd/indexer 수집·설정/lifecycle·W2 capture DB/실제 삭제 guard는 연결하지 않았으며, 공유 volume 전체 바이트 예약/운영 경고·부분 segment compaction·실제 외장 장치 분리/전원 손실·86,400/172,800블록과24시간 실운영은 남아 있다. 테스트용 승인 oracle을 운영 guard로 쓰지 않는다. 공개 RPC5/fresh canonical·기존 DB/설정·SQL 바인딩·writer undo/outbox는 유지했다. 노드 설치/실행·snapshot 다운로드·Rust 재빌드·Arc 처리량/SLO 재측정은 하지 않았다.

## 3.35 원천 DB transaction과 durable 삭제 fence (10/15)

통합 W2의 공통 저장 계층으로 `port.CaptureDatabase`와 `pkg/capture`를 구현했다. 필수 encoded dataset 바이트·manifest·canonical pointer·bootstrap checkpoint 이후의 연속 cursor·별도 facts_ready 이벤트를 한 transaction으로 저장한다. plan/coverage/status·정확한 payload digest·다음 높이/parent·현재 generation을 확인하고 transaction 안에서 fresh canonical을 조회한다. 조회 실패/불일치·pruned·누락은 완료로 바꾸지 않는다. 명시적 verified_empty와 fork_rules 근거가 있는 not_applicable을 구분한다. 재전달은 저장 바이트까지 대조하며 응답 유실 뒤 재시도도 이벤트를 중복 생성하지 않는다.

Pebble은 별도 source namespace/indexed batch를 기존 writer mutex로 직렬화하고 WAL+Sync를 요구한다. PostgreSQL migration0013은 capture_locks/capture_records를 추가하고 chain/key/value를 모두 바인딩한다. 체인 row lock으로 여러 pool/process를 직렬화하고 서버 fsync가 꺼졌으면 거부하며 각 capture transaction은 synchronous_commit을 on으로 고정한다. source facts는 기존 projection undo와 일반 재색인에서 보존한다. 현재 저장 형태는 dataset당8MiB/블록 합계64MiB의 DB 내 encoded 바이트이며 큰 영구 객체 lifecycle·account/storage/log별 정규화 schema/API가 완료된 것은 아니다.

Reset은 공통 조상에서 generation을 올리고 과거 fork facts를 보존하며 이전 worker의 새 commit을 거부한다. 현재 cursor 위에 남은 과거 pointer는 완료로 보지 않는다. Finalize는 체인 정책이 이미 인증한 checkpoint를 받는 내부 API이며 consensus 검증 구현은 별도다. raw-only ACK는 정확한 canonical capture identity와 등록 consumer를 확인한다. 공통 조상의 이전 manifest/ACK는 유지한다.

`capture.Store`의 window guard는 필수 plan 전체의 실제 DB 바이트/digest·finality·원본 codec/digest/길이·raw-only ACK를 확인한다. 삭제 전에 generation fence와 정확한 raw store/segment/참조 intent를 durable commit하므로 PostgreSQL 연결 유실만으로 fence가 풀리지 않는다. 동기 callback이 성공한 뒤에만 fence를 해제한다. 처음에는 callback 오류 뒤 해제를 고려했으나, 이미 `.gc`가 쓰인 오류라면 재시작 삭제가 generation 변경을 가로지를 수 있어 오류 때도 fence를 보존하도록 구현·시험했다. panic/프로세스 종료/불확실한 commit도 변경·추가 삭제를 차단하고 State/PendingDeletion으로 확인할 수 있다. 임의 clear/TTL 해제는 제공하지 않는다. DB intent와 window Open의 GC 복구/단일 writer를 결합한 안전한 재개가 다음 작업이다.

동일 suite를 실제 Pebble과 시험 전용 PostgreSQL16.15 fsync=on/full_page_writes=on에 적용해 capture race9.621초를 통과했다. 서버 기본 synchronous_commit=off에서도 capture 내부 on을 확인했고 별도 fsync-off 서버의 저장 거부도 통과했다. 단계별 rollback·재시작·중복/응답 유실·순서/parent/fresh mismatch·generation/포크·빈 결과·SQL 형태의 바이트 보존·snapshot·갱신 경합·실제 window 삭제/lease 차단·필수 receipt 바이트 손상 거부·삭제 실패/불확실한 승인 fence를 확인했다. 실제 Pebble 자식 프로세스를 facts commit과 삭제 fence commit 직후 종료하고 데이터를 재확인했다. rollback 시험의 초기 실패는 fixture에 rollback 대상 block이 없어서 발생했고 block을 저장한 재실행에서 통과했다. no-WAL 시험도 기존 schema가 있는 DB를 WAL 없이 다시 여는 fixture로 보완했다.

최종 generation 경합200회 CPU1/race25.810초, 갱신 경합200회29.427초를 통과했다. Pebble source 격리/WAL/읽기 전용과 PostgreSQL의 다른 pool 잠금/읽기 전용/undo/재색인 보존, 기존 migration/undo/SQL 공격 회귀를 확인했다. window 전체 race6.682초와 공통 model/port/chainfeed/source/체인 경계 race, go vet·변경 관련 패키지 lint0건·전체 Go 패키지 컴파일을 통과했다. 앞서 작성한 window의 미처리 Close 반환값도 정리하고 directory sync/read의 close 오류를 반영했다. Linux/arm64에서는 네트워크 없는 기존 이미지로 window 전체+실제 bind mount·읽기 전용 bind writer 거부를 다시 통과했다. 검증은 `/private/tmp/indexer-capture-db-20261015/`에 보존하며 시험 전용 DB 두 개와 mount 시험 container는 제거했다. 기존 사용자 container는 유지했다.

[현재 API/한계](../CAPTURE_STORAGE.md)와 저장소/설계/open-items/CLAUDE를 갱신했다. 기존 cmd/indexer/config.yaml·RPC5/fresh canonical·writer undo/outbox 실행 경로는 변경하지 않았고 full node·snapshot·Rust build도 실행하지 않았다. 자동 삭제 fence 복구·공유 volume 예약·큰 영구 객체·실제 adapter/finality·DB-only projection·수집 lifecycle과 장기 성능 검증은 남아 있다. 이번 시험은 전체 app race 성공, 물리 전원 손실 내구성, 실제 Arc throughput/SLO 또는 모든 EVM 상태 수집 완료를 뜻하지 않는다.

## 3.36 삭제 잠금 복구와 수집 세션 lifecycle (10/15)

3.35에서 남긴 DB 삭제 intent와 윈도우를 결합하는 복구를 구현했다. `.gc`에 정확한 전체 참조 집합의 해시를 기록하고, 파일 unlink·directory sync 뒤 bounded 단일 `GC.completed.json`을 durable하게 남긴다. 파일 부재만으로 완료를 추정하지 않는다. 남은 원본은 seal 색인의 전체 참조·24시간 보관·lease를 확인해 삭제를 끝내고, 이미 삭제된 원본은 정확한 store/segment/참조 해시와 완료 기록을 확인한다. DB token/generation·필수 바이트 전체·raw-only ACK를 다시 확인한 뒤 동일 token만 해제한다. GC/복구/Close는 GC mutex로 직렬화하고 process writer lock은 유지한다. 다른 coordinator가 실행 중 GC의 오래된 intent를 읽어도 잠금을 기다린 뒤 다시 token을 검사하므로 새 상태를 해제하지 않는다.

복구 순서도 보강했다. `window.OpenChecked`와 `capture.Store.OpenWindow`는 process writer 잠금을 얻은 뒤 DB 승인을 확인해야 부분 `.gc`의 남은 파일을 삭제한다. 단독 `window.Open`의 과거 preapproved-marker 복구는 유지하지만 capture 세션은 DB 조정 경로를 사용한다. 실제 시험에서 `.gc`와 raw unlink 뒤 idx 삭제에 파일시스템 오류를 발생시키고, DB receipt 바이트 검증을 실패시키자 남은 seal/gc가 유지됐다. DB가 정상일 때만 재개·완료 기록·잠금 해제가 통과했다. 예전 참조 해시 없는 marker나 정확한 완료 증거가 없는 과거 DB 잠금은 자동 해제하지 않는다.

`RunSession`은 plan/checkpoint 확인·지정 원본 저장소 열기·삭제 잠금 복구를 마쳐야 수신/처리 Task를 별도 goroutine으로 실행한다. 한 worker가 끝나면 상대를 취소하고 두 Task/lease가 정리된 뒤 윈도우를 닫으며 DB는 빌려 쓴다. 직접 반환한 peer 취소 외의 오류와 여러 오류를 합친 값은 보존한다. restart의 누락 경로를 Create로 대체하지 않고, DB 잠금이 남은 상태에서 새 빈 window Create도 거부한다. 자동 GC·자동 재시도·실제 peer/assembler는 시작하지 않는다. 기존 cmd/config/app 수집 경로는 활성화하지 않았다.

삭제 승인 검사의 중복 I/O도 줄였다. 같은 블록의 여러 raw 참조는 한 DB transaction 안에서 manifest·필수 dataset 바이트/digest·ACK를 한 번만 검증하고 metadata로 비교한다. payload를 cache에 보관하거나 다음 transaction까지 검증 결과를 재사용하지 않는다. 두 실제 저장소에서 receipt 읽기 횟수와 다음 transaction의 재검증을 확인했다. 이는 실제 Arc 처리량 측정이 아니다.

최종 Pebble/PostgreSQL fsync-on 공통 capture/session race40.559초, window 전체 race13.394초와 port/체인 경계 회귀가 통과했다. 새 worker 취소/join/lease 경합200회 CPU1/race79.694초, GC와 별도 coordinator 복구 경합200회93.144초가 통과했다. 임시 PostgreSQL16.15는 fsync/full_page_writes=on, 기본 synchronous_commit=off였으며 기존 capture transaction의 on 강제와 SQL 공격/undo 회귀도 통과했다. 자식 프로세스를 승인 직후·`.gc` fsync 직후·일부 unlink 직후·완료 기록 fsync 직후·파일 삭제 직후에 종료해 복구했다. DB 완료 쓰기 실패·다른 store·누락된 intent·손상·lease·취소·누락 경로와 파일 부재만으로 해제 금지를 확인했다. go vet·관련 패키지 lint0건·전체 Go 패키지 컴파일, Linux/arm64 네트워크 없는 container의 전체 window/실제 bind mount·읽기 전용 bind 거부도 통과했다. 시험 기록은 `/private/tmp/indexer-capture-recovery-20261015/`다. 시험 전용 DB와 mount container는 제거하고 기존 사용자 container는 유지했다.

[capture API](../CAPTURE_STORAGE.md)·window 문서·설계/open-items/CLAUDE를 갱신했다. 실제 데이터 공급 감사·completeness assembler·공유 volume 예약/경고·외장 경로 설정 조립·큰 영구 객체·정규화 schema·DB-only projection과 장기 검증은 남아 있다. 다음은 공유 volume 예산과 설정 lifecycle을 연결한 뒤 검증된 adapter/worker를 적용하는 단계다. 공개 RPC5·fresh canonical·SQL 바인딩·기존 writer의 undo/outbox와 실제 config.yaml/DB는 유지했다. full node·snapshot·Rust 재빌드·실제 Arc throughput/SLO 측정은 수행하지 않았다.

## 3.37 공유 volume 예약·압력과 설정 세션 연결 (10/15)

W1/세션 후속으로 managed volume의 durable 예약 장부와 설정 조립을 구현했다. v2 marker에 총 예산/여유 floor를 고정하고 checksum 장부에 이름·상대 경로·chain/genesis·한도·store ID를 저장한다. 장부/잠금 identity를 검사하며 여러 프로세스의 예약·bounded 디스크 쓰기를 flock으로 직렬화한다. 예약 합계는 관리 파일1MiB를 제외한 예산 이내다. 새 예약은 디렉터리 생성 전에 durable commit하고, Open은 새 예약을 만들지 않는다. Close/crash/GC는 고정 예약을 반환하지 않으며 PID/TTL 회수·장부 재구성·v1 자동 전환·온라인 resize 기능을 제공하지 않는다. 장부 소실/손상·잠금 파일 교체·다른 store로 재생성은 중단한다.

물리 preallocation은 하지 않는다. 최초 전체 예산+여유 검사와 각 쓰기의 chain/volume 중 큰 여유 floor를 적용한다. `Usage`는 실제 파일/색인 예약·고정 예약 합계·물리 여유를 분리하고 체인80/95%, volume floor 위 margin20/5%를 보고한다. 가득 찬 raw를 임의로 삭제하지 않는다. `AppendWait`는 동일 입력을 보유하고 취소 가능한100ms 대기로 재시도하며 그동안 잠금을 보유하지 않는다. 세션은 수신을 대기시켜도 처리 Task는 실행하고, 기본10초 압력 감시는 severity 변화만 로그로 남긴다. volume/장부 장애면 monitor와 모든 Task/lease를 join한 뒤 Close한다. 종료 시 전체 다음 append 예약을 요구하지 않고 현재 seal에 필요한 여유만 사용하도록 했다.

`window.Layout`/`ReadLayout`은 단일 문서1MiB·strict nested YAML, 절대 경로·겹침·예산 합계·최소24시간을 검증하며 파일을 만들지 않는다. `window_storage`에서 `RunConfiguredSession`으로 instance와 검증된 chain/genesis를 연결했다. 실제 feed/assembler 없는 기존 App은 이 설정이 있으면 DB/RPC 시작 전에 명시적으로 거부한다. 설정을 읽고 무시한 채 기존 ingest로 실행하지 않는다. 네 체인 외장 경로/예산 예시는 `docs/examples/window-storage.yaml`에만 추가했고 실제 config.yaml/운영 DB는 수정하지 않았다.

최종 신규 window/capture/config/app race, window 전체와 port/chain 경계 race, 실제 fsync-on PostgreSQL/Pebble capture 공통 suite와 구성 세션 roundtrip을 통과했다. 별도 PostgreSQL SQL 바인딩·공격 문자열·식별자·undo 회귀도 통과했다. 세션 용량 대기/processor 지속·volume 분리 감지·lease join의 CPU1/race200회는207.163초에 통과했다. 예약 경쟁·압력/GC 재개·공간 대기 취소·DB 승인 시 잠금 비보유의 window CPU1/race200회도255.055초에 통과했다. Linux/arm64 전체 window(문서 파일 검사는 macOS에서 실행)·실제 managed bind·읽기 전용 bind 거부, 관련 lint0건·vet·전체 Go 패키지 컴파일도 통과했다. 시험 전용 컨테이너/볼륨은 제거했고 기존 사용자 컨테이너는 유지했다.

실패도 보존했다. 처음 설정 시험은 fixture의 필수 DB 경로 누락을 수정했다. 초기 장부 시험에서 교체된 잠금 파일을 재시작이 허용하던 경우를 검출해 durable 잠금 identity 검증을 추가했다. 전체 PostgreSQL package 확대 시험은 많은 schema를 생성하던 중 Docker 파일시스템의 No space left on device로 실패했다(capture package 자체는76.129초에 통과). 시험 DB/볼륨을 제거한 뒤 관련 SQL/undo22.181초·구성 세션4.551초를 새 DB에서 분리 통과했으며 전체 PostgreSQL package 통과로 표시하지 않는다. Linux의 기존 fixture 재생성 거부는 새 빈 bind 경로로 시험 조건을 수정해 통과했다. 큰 압력 fixture의 CPU1/race200은 반복 실행 누적으로600초 제한에 걸렸다(종료 스택은 seal 파일 생성 중). 같은 80/95% 경계를 적은 frame으로 검증하도록 fixture를 축소해 최종 재실행했다. 운영 durability나 fsync를 낮추지 않았다.

기록은 `/private/tmp/indexer-volume-budget-20261015/`다. 공개 RPC5·fresh canonical·SQL 바인딩·기존 undo/outbox는 유지했다. full node/snapshot/Rust 재빌드·실제 Arc 처리량·24시간 운영·실제 외장 장치 전원/분리·전체 app race/SLO는 수행하지 않았다. 다음은 체인별 입력 공급/검증 capability 감사와 completeness assembler 연결이며, 큰 영구 객체·정규화 원천 schema·DB-only projection·운영 앱 활성화도 남아 있다.

## 3.38 Docker 공간 확보와 PostgreSQL 전체 회귀 재검증 (10/15)

사용자 요청에 따라 Docker 공간을 조사했다. 연결되지 않은 익명 volume47개가 있었으며 읽기 전용 container로 PG16·indexer DB·시험용 namespace 또는 fsync=off 실행 옵션만 확인했다. 자격 증명이나 사용자 데이터는 출력하지 않았다. 이 조건으로 시험 산출물로 확인한40개를 목록으로 고정하고, 삭제 직전 dangling 여부와 anonymous label을 다시 확인해 개별 삭제했다. Docker가 보고한 기존 논리 크기 합계 기준 약28.85GB를 회수했다. 용도가 불확실한7개와 기존 운영/중지 container·연결된 volume·이미지는 보존했다. 전체 volume prune은 사용하지 않았다.

새로 이름/label을 붙인 전용 volume의 PostgreSQL16.15에서 `fsync=on`, `full_page_writes=on`, 기본 `synchronous_commit=off`를 확인한 뒤, 이전 실패와 동일한 `go test -race`로 `pkg/storage/postgres` 전체 및 `pkg/capture` 전체를 재실행했다. PostgreSQL85.678초(413개 test/subtest), capture17.770초(45개 test/subtest)가 모두 통과했고 실패/skip은 없었다. SQL 공격 문자열/서버 파라미터 바인딩·catalog 식별자·모든 port contract·undo·capture의 강제 synchronous_commit=on·세션/삭제 복구를 포함한다. 3.37의 Docker No space left on device로 미완료였던 전체 PostgreSQL 회귀는 이번 실행으로 재검증을 완료했다. Go/SQL 런타임 코드를 변경해 통과시킨 것은 아니다.

`scripts/test-postgres.sh`도 고유 named volume과 프로젝트/용도 label을 사용하도록 보강했다. volume 생성 직후 EXIT 정리를 등록하고 container 시작 실패·Go 시험 실패에도 container/volume을 명시적으로 제거하며 원래 실패 코드를 보존한다. 시험 성공 뒤 volume 제거가 실패하면 실행을 실패로 표시한다. 외부 DSN은 기존처럼 Docker 자원을 만들거나 삭제하지 않는다. 기존 기본 기능 시험의 fsync=off 설정은 바꾸지 않았고 위 전체 재검증은 별도로 fsync-on 서버에서 수행했다. bash 문법·성공/시험 실패/시작 실패/정리 실패/외부 DSN의5가지 stub 경로가 통과했다. 수정 스크립트로 실제 PostgreSQL 생성→서버 바인딩 시험→container/named volume 제거도 통과했다.

전체 재검증용 DB/전용 named volume과 읽기 전용 감사/probe는 제거했다. 기존 container 상태·보존 volume 목록을 대조하고 script label의 잔여 volume이 없음을 확인했다. 기록과 삭제 감사 목록·race JSON은 `/private/tmp/indexer-docker-recheck-20261015/`에 있다. 전후 공간과 hash는 해당 validation.json에 기록한다. RPC5·fresh canonical·운영 config/DB는 유지하며, 전체 app race/SLO·실제 Arc feed·24시간 운영 검증은 이번 결과에 포함하지 않는다.

## 3.39 다음 작업과 전체 잔여 목록 정리 (10/15)

사용자 요청에 따라 open-items를 실제 코드/검증 결과와 대조해 다시 정리했다. 완료한 공통 계약·window·encoded 원천 DB·삭제 복구·공유 예산/설정·SQL 방어와 Docker 공간/전체 PostgreSQL 재검증을 완료 범위로 분리했다. 바로 다음은 Arc 공급/검증 가능성 감사→체인별 peer/relay adapter→필수 입력 검증/capture worker 연결이다. 이어서 finality/reorg/GC, 큰 객체/정규화 저장, DB facts→기존 writer, 실제 앱/네 테스트넷 조립, 독립 후처리/포괄 데이터/API, 장기 성능/장애 검증의9단계 순서를 정의했다. 각 단계의 선행 조건과 W0~W7/R 작업 ID를 연결했다.

R03 운영 외장 경로/checkpoint·상태 보존, R05 실제 era1 비교, R06 mainnet, R07 macOS/여러 호스트 SLO, R08 앱 전체 race/간헐 회귀, R09 frontend와 R10 외부 작업의 조건을 별도 목록에 유지했다. R01/R02와 기존 RPC/ExEx 표본의 완료 범위·한계는 작업 기록 링크로 옮겼으며 과거 실패/측정 기록은 삭제하지 않았다. Stable·K2·값 형식 변환·AA 목록·Git history 재작성 등의 보류/범위 밖 결정도 유지했다. README 안내를 갱신하고 문서 링크/상태를 확인했다. 이번에는 목록/문서만 변경했으며 런타임 구현·RPC/DB/컨테이너 실행이나 시험을 추가하지 않았다.

## 3.40 Arc ETH68 peer와 두 worker 연결 (10/15)

사용자가 다음 목록1~3번의 실행을 요청했다. 공식 Arc `6e764023ee6515fe70573e123ed2db912a7207b4`와 Reth v2.2.0 `88505c7fcbfdebfd3b56d88c86b62e950043c6c4`의 wire 지원을 확인했다. Arc CL Malachite/libp2p와 EL devp2p를 구분하고, 첫 입력을 ETH/68 header/body/receipt RLP3개로 고정했다. [공급 감사](arc-peer-capabilities.md)에 자료별 형식·검증·보존 한계·unsupported state/trace/blob와 RPC 예산을 정리했다. body의 거래/receipt의 로그를 별도 complete dataset이나 전체 계정 상태로 표시하지 않는다. 실제 EL peer는 온보딩 enode/network/fork ID 확인이 필요하며 아직 제공받지 않았다.

`pkg/chainfeed/ethpeer`는 기존 geth RLPx의 static identity peer로 연결한다. network/genesis/명시적 fork ID, request ID·응답 종류·header 높이·message 크기를 검사하고 transport 단절은 같은 높이로 재연결한다. 최초 advertised head header를 hash로 확인하고 알려진 head 아래 자료 누락은 명시적 `ErrHistoryGap`으로 중단한다. `pkg/chainfeed/evmdata`는 제한된 RLP cardinality를 profile decoder 앞에서 검사하고 블록 hash/거래, transaction·uncle·withdrawal·receipt root, receipt 수·유형·gas/bloom을 검증한다. 이는 commitment 검증이며 EVM 실행/CL finality 증명이 아니다. 별도 Go import 패키지 `modules/feeds/arc`가 Arc testnet5042002 profile·bootstrap fresh 확인·지원 plan·configured session을 연결한다.

`capture.PipelineTasks`의 수신은 P2P encoded dataset을 지정 raw window에 `AppendWait`/fsync한 뒤 bounded 참조 channel에 알린다. 후처리는 다른 goroutine에서 raw Page의 필수 참조를 모아 검증하고 기존 facts transaction으로 저장한다. channel이 가득 차면 알림만 합치고 작업은 디스크에 보존하므로 처리 정지 중에도 quota 범위에서 수신을 계속한다. partial raw restart는 중복 append 없이 재요청·복구하고 gap/fork/손상/누락은 완료 cursor를 전진시키지 않는다. DB commit을 마친 참조만 peer 응답에 게시하며 기본1024개 블록의 metadata만 메모리에 둔다. 원본 응답 byte는 짧은 lease로 읽는다. raw fsync/channel 전달을 capture/기존 처리 완료 ACK로 바꾸지 않았다. 세션 종료는 stream/peer까지 join한 뒤 window를 닫는다.

공개 RPC는 caller의 기존5 HTTP/s 공유 pool을 계속 사용한다. 별도 HTTP client/limiter를 모듈에 만들지 않았고 매 capture commit 안의 fresh canonical 검사도 유지했다. local HTTP oracle와 실제 공유 limiter/coalescer를 연결한 시험에서 각 수집 블록의 canonical 조회가 pool을 통과하고 body/receipt/state용 RPC를 추가하지 않는 것을 확인했다. 계정/code/storage/trace/blob 등 unsupported required는 peer/디스크 시작 전에 거부했다. SQL 작성 경로는 기존 바인딩 source DB를 그대로 사용했다. 기존 CLI/window 활성화는 finality·DB-only projection·앱 조립 전이므로 계속 거부한다. 실제 config.yaml/운영 DB는 변경하지 않았다.

독립 TCP/RLPx oracle→window→검증→Pebble 및 fsync-on PostgreSQL이 통과했다. 처리 worker 정지·channel1개에서3블록 수신 지속, partial restart, 틀린 body/receipt root·request ID·fresh hash, missing receipt의 명시적 gap, peer에 미검증 raw 제공 금지를 확인했다. 이전 공개 기록에서 Arc67,019,625의 header/body/receipt 한 블록만 고정 fixture로 추출해 실제 optional header·root·5거래/receipt와 정확한 byte 보존을 검증했다. 실제 네트워크 peer 녹화 자료로 표현하지 않는다.

최초 Arc module race6.981초(실제 PostgreSQL 포함), capture/chainfeed/port/체인 경계 race와 source/era/replay/nodefeed/RPC/pool/SDK 회귀가 통과했다. 마지막 검토에서 withdrawals 개수를 profile allocation 전에 제한하고 typed 거래에 legacy receipt를 붙이는 경우를 명시적으로 거부했다. 새 음수 시험의 profile 생성자/초기 잔액과 PostgreSQL harness의 임시 socket 서버 준비 판정을 수정한 뒤, 보강된 verifier/peer/Arc/capture race2.502/2.627/15.315/19.044초와 별도 fsync-on PostgreSQL race5.673초가 통과했다. 초기 시험 설정 실패 로그도 보존한다. 새 수신/처리 지연 및 partial restart 경합2개를 `GOMAXPROCS=1 -race -count=200`으로 최초176.781초, 보강된 검증기에서 최종311.490초에 통과했다. go vet·관련 package lint0건·전체 Go package 컴파일도 통과했다. Linux/arm64 정적 시험 바이너리를 네트워크 없는 기존 postgres:16-alpine 이미지에서 실행해 loopback RLPx·window·제한 HTTP·기록 Arc 시험이 통과했다. 그 container에는 DB가 없으므로 PostgreSQL1개는 명시적으로 skip했으며, 실제 DB 검증은 위 macOS race 실행에서 수행했다. geth/Go/toolchain의 선언 버전은 유지하고 p2p 사용에 필요한 기존 geth의 indirect 의존성만 추가했다.

검증 기록·입력/source SHA와 audit metadata는 `/private/tmp/indexer-arc-peer-20261015/`에 보존한다. 시험 전용 PostgreSQL container/named volume, 자동 제거 Linux container와 시험 바이너리를 정리했고 기존7개 container 상태를 유지했다. full node/Rust build/snapshot은 실행하지 않았다. 실제 Arc peer 공급·history 보존·실제 네트워크 재연결·450ms 목표/2블록초 추종·24시간 운영, CL finality/reorg/GC·큰 객체/정규화 원천·DB-only writer·앱 활성화는 남아 있다. 1~3번의 기본 코드/로컬 검증과 실제 네트워크 미검증을 구분해 open-items를 갱신했고 다음 코드 단계는4번이다. 전체 app race/R04/R07 완료로 표시하지 않는다.

## 3.41 Arc finality·reorg·raw ACK·GC runtime 연결 (10/15)

사용자의 다음 작업 요청으로4번의 Arc 기본 경로를 구현했다. 공식 Arc `6e764023ee6515fe70573e123ed2db912a7207b4`의 결정 블록 확정→Engine forkchoice와 RPC/IPC의 head/safe/finalized 동일 hash 설정을 확인했다. `source.FinalizedReader`/RPC의 optional `FinalizedCheckpoint`는 기존 client/pool에 finalized 태그만 요청하며 null·오류·malformed를 latest/safe/depth로 대체하지 않는다. Arc `TrustedRPCFinality`는 이 node assertion을 명시적으로 신뢰하는 모드이며 독립 Malachite validator-set/certificate 검증은 아니다. mode/revision/authority ID를 DB에 고정하고, 기존 정책 불일치는 peer 생성과 pending GC를 복구하는 window Open보다 먼저 거부한다. 근거와 실행 API는 [Feed 런타임](../FEED_RUNTIME.md)에 정리했다.

`capture.RunFeedSession`/`RunConfiguredFeed`와 `arcfeed.RunManaged`는 수신·검증·제어·raw 소비자를 한 epoch로 관리한다. reorg는 모든 worker/peer를 취소·join하고 window를 닫은 뒤 공통 조상을 다시 fresh 조회하고 DB transaction에서 generation을 바꾼다. 기본128높이(최대4096)·finalized/bootstrap floor와 한 실행당8회 재개 한도를 둔다. RPC head가 잠시 낮아진 경우에도 더 낮은 일치 prefix를 확인하며, 일반 통신/I/O 오류를 hash 불일치로 바꾸지 않는다. deep reorg·finalized 위반·한도 초과는 명시적 재구축/충돌 오류로 중단하고 DB/원본을 지우거나 latest로 점프하지 않는다.

managed 처리는 next identity를 fresh로 resolve하고 raw Lookup으로 required를 모아 retained fork의 append 순서와 분리한다. Commit 안의 fresh 검사는 별도로 유지한다. raw 소비자는 자기 durable 작업 성공·하위 작업 join·lease 반환 뒤 자기 그룹만 ACK하며 실패·취소는 ACK하지 않는다. 기본10초의 제어 tick은 정책 검증 후에만 최소24시간·전체 bytes/digest/codec·finality·raw ACK·lease·DB fence 조건의 GC를 수행한다. active는 최신 record까지 retention을 지난 경우만 seal한다. 미완료/ACK 지연/이전 fork segment는 정확한 `ErrRetain`으로 남기고 다른 eligible segment를 검사한다. 이전 fork 원본의 자동 폐기 권한은 만들지 않았다. 삭제 fence 동안 capture/ACK는 입력을 유지하며 대기하고, 미완료 삭제는 기존 exact intent/완료 기록으로만 복구한다.

시험 중 reorg에 따른 wrapped cancellation을 독립 장애로 오인하던 경우를 수정했다. reorg 원인과 순수 worker cancellation만 재개하며 함께 온 I/O/손상/finality 오류는 보존한다. 정책 ID는 최초 한 번만 durable하게 쓰고 steady 일치 확인은 읽기로 처리한다. 공개 RPC5 HTTP/s의 동일 pool과 기존 SQL 값 바인딩을 유지했고 별도 production HTTP client/limiter·schema 변경은 추가하지 않았다. 독립 finalized 조회가 HTTP batch를 공유할 수 있어 물리 HTTP와 논리 method 횟수를 시험에서 구분했다.

독립 loopback RLPx의3블록 수신→2블록 fork 교체→join/reset→새 fork 저장과 이전 facts/raw 보존을 Pebble/실제 PostgreSQL에서 검증했다. 실제 제한 HTTP pool과 finalized 정책, 두 DB의 ACK 지연·25시간 논리 시간·lease·정책 교체/역행·deep/shorter-head reorg, 소비자 실패 무ACK와 pending deletion 전에 정책 불일치 거부가 통과했다. fsync/full_page_writes=on·기본 synchronous_commit=off의 전용 PostgreSQL에서 전체 저장소413개·capture53개·Arc16개 test/subtest가176.325/40.426/22.439초에 통과했다. 이후 정책 preflight/읽기 최적화 보강의 두 DB 공통 회귀와 P2P reorg를5.983/4.987초에 추가 통과했다. capture transaction의 synchronous_commit=on 강제와 기존 SQL 공격/undo/삭제 중단 복구도 유지했다.

새 ACK/lease/GC 경합의 CPU1/race200회는57.614초에 통과했다. 큰 reorg를 포함한 합산/단독200회는720/600초의 누적 제한에 걸렸으며 마지막 개별 시험은 약1~2초 경과 중이었다. 이를 통과로 기록하지 않는다.3블록/2블록 재분기 시나리오는 일반 race·20회·실제 DB에서 유지하고, 반복 fixture는 명시적 최근 bootstrap 다음 한 블록 교체로 최소화했다. 같은 실제 RLPx·managed window·durable generation/이전 fork 보존 경로의 CPU1/race200회가88.473초에 통과했다. fsync나 운영 한도는 낮추지 않았다. 초기 물리/논리 RPC count 기대값과 Linux YAML fixture mount 누락, lint 조건식 수정 및 timeout 로그도 보존한다.

관련 native race·Source/era/replay/nodefeed/pool/SDK/port/체인 중립 회귀, vet·관련 lint0건·전체 Go package 컴파일이 통과했다. Linux/arm64의 네트워크 없는 기존 이미지에서 Arc·capture·window 시험이 통과했고 DB/실제 mount 전용 조건은 skip으로 구분한다. 시험 PostgreSQL/이름 붙인 volume·임시 Linux container와 바이너리를 정리했고 기존 사용자 container/volume과 실제 config.yaml/운영 DB/handoff는 보존한다. full node/Rust build/snapshot은 실행하지 않았다. 자료·source hash·성공/실패·정리는 `/private/tmp/indexer-feed-policy-20261015/`에 기록한다.

4번은 명시적 RPC-finalized trust를 쓰는 라이브러리 기본 연결까지다. 실제 Arc peer/운영 정책·독립 CL certificate·orphan raw의 완료/취소 입증과 삭제 정책·450ms/24시간 운영·기존 앱 활성화는 남아 있다. 다음 코드 단계는5번 큰 객체/정규화 원천 저장이며 기존 writer의 DB-only 입력은6번이다. 전체 app race/R04/R07 완료로 표시하지 않는다.


### 3.42 영구 객체·정규화 원천 행과 JSON 바인딩 보강 (10/15)

다음5번의 공통 저장 기반을 구현했다. `window.Objects`는 명시적 별도 경로/volume/reservation에서 대용량 입력을32KiB 버퍼로 저장한다. raw와 mount/marker/pinned directory/writer lock/공유 quota 코드를 재사용하되 저장 형식·수명·GC는 구분한다. 길이/digest 확인→file fsync→rename→directory fsync 뒤 난수 ID 참조를 반환한다. `NewWithObjects`의 transaction은 실제 파일을 검증하고 binding·행·manifest·canonical pointer·연속 cursor·내부 이벤트를 함께 확정한다. Commit 전 fresh canonical과 기존 shared5 HTTP/s 정책은 유지한다. 객체 경로 장애를 감시하며 세션 종료 전에 모든 worker를 join한다. 객체 handle은 호출자가 소유한다.

참조되지 않은 객체는 serialized DB transaction의 영구 retirement 표식 뒤에만 지운다. 실패/불확실한 DB commit은 unlink하지 않는다. 늦은 Commit은 retired ID를 참조할 수 없고 동일 내용의 새 업로드 ID로 재시도한다. 표식 뒤 삭제 중단은 재개할 수 있다. 연결된 객체는 이전 fork도 보존하고, TTL이나 raw retention으로 삭제하지 않는다. 실제 객체 손상은 capture Read/재시도/원본 GC를 차단한다. 임시 파일 복구·프로세스 강제 종료 뒤 durable upload 재개·공유 quota·path replacement도 검사했다.

Arc 기본3종의 모든 기존 검증 뒤 header/body/transaction/receipt/log를 kind/ordinal 행으로 만든다. model 전용 codec으로 체인 확장을 보존하고 정렬된 row digest/count·개별 digest·chain/block/plan/generation owner를 기록한다. `Store.Rows`는 최대256행/8MiB로 불변 fork 데이터를 조회한다. PostgreSQL 전용 관계형 테이블/전역 주소·topic 검색은 추가하지 않았으며 두 DB의 기존 원천 namespace를 사용한다. header/transaction/receipt/log와 body 순서의 기록된 Arc sample 대조, 확장 Sepolia header codec 보존, 잘못된 블록 행 치환 거부와 row 손상 시 raw GC 차단을 검증했다. encoded-only 과거 입력의 정규화 조회는 coverage gap을 반환한다. 계정/계약/storage/trace/blob 입력 공급·검증·검색과 DB-only writer/앱 활성화는 아직 남았다. [API·한도·복구 계약](../PERMANENT_SOURCE_STORAGE.md)을 따른다.

실제 PostgreSQL 검사 중 `simple_protocol`을 서버 바인딩 `exec`로 올린 설정에서 기존 `feature_states` JSON이 []byte/bytea로 인코딩돼 실패하는 결함을 재현했다. 해당 값만 JSON text로 바인딩해 고쳤고 queued write·SQL처럼 생긴 name/definition·조회/undo를 함께 재검증했다. 파라미터 바인딩을 우회하거나 SimpleProtocol로 되돌리지 않았다. 첫 실패 로그와 수정 뒤 결과를 모두 보존했다.

검증 결과:

- macOS 관련11개 패키지 race 통과: window/capture/chainfeed·ethpeer·evmdata/Arc/source RPC/shared pool/SDK·sdktest/model. 최종 행 owner 보강 뒤 capture/evmdata/Arc race 재검사 통과. Go 전체 패키지 compile 통과. 변경 패키지 vet 통과·lint0 issues.
- 전용 fsync-on PostgreSQL16에서 exec 모드 전체 저장소 race413 tests/subtests 통과(150.115s). capture/Arc 전체 race87 tests/subtests 통과(45.712s/20.155s). 최종 owner 보강 뒤 두 DB 객체/정규화와 실제 Postgres Arc P2P/reorg를 선택 재검사16 tests/subtests 통과(6.500s/4.261s). 실제 운영 DB를 사용하지 않았다.
- `GOMAXPROCS=1 -race -count=200`: 객체 Commit/Collect 경합200회 통과(50.237s), 객체 경로 장애의 세션 취소/join/lock 해제200회 통과(35.100s). 객체 참조와15개 DB write 지점 실패의 원자성·커밋 응답 유실·retirement 응답 유실·복구를 두 DB에서 검증했다.
- Linux/arm64 Docker에서 window/capture/evmdata/Arc 통과. 최종 owner/세션 장애 변경 뒤 capture/evmdata/Arc 재검사 통과. read-only repo bind·network none·임시 디렉터리에서 실행했다. 실제 외장 mount/읽기 전용 mount와 SQL 환경이 필요한 일부 OS 검사는 조건 미충족으로 skip됐으며 실제 외장 장치 운영을 검증했다고 표시하지 않는다. SQL 검사는 별도 native 실행에서 위와 같이 통과했다.
- 전용 테스트 컨테이너/이름 있는 볼륨과 임시 Linux 바이너리는 종료 후 제거했다. 기존7개 컨테이너/다른 볼륨은 보존했고 broad prune은 하지 않았다. config.yaml·Go/geth 의존성·이전 handoff 문서는 변경하지 않았다. 새 full node/스냅샷/공개 RPC 요청은 없으며450ms/실시간 처리량/24시간 운영을 이번 검사로 주장하지 않는다.

증거는 `/private/tmp/indexer-source-objects-20261015/`의 `validation.json`, `before-sha.json`, race/CPU1/SQL/Linux 로그와 자원 cleanup 기록이다. 다음6번은 원천 DB facts를 기존 feature writer에 연결하고 결과·잔액·undo/outbox·처리 ACK를 대조하는 작업이다.


### 3.43 DB facts와 기존 writer 연결 (10/15)

다음6번의 full-ingest 라이브러리를 연결했다. `pkg/source/facts`는 complete header/body/receipt3종을 DB/영구 객체에서 재생하며, `evmdata.Decode`로 수집 verifier와 같은 bounded profile/commitment 검증을 다시 수행한다. `Head`는 captured Through이고 `HashAt`만 별도의 fresh canonical reader를 사용한다. 새 HTTP client나 노드를 만들지 않으며 기존5 HTTP/s pool을 그대로 빌린다. `arcfeed.NewFactsSource`가 Arc testnet의 profile/native 회계를 고정한다. raw 디렉터리를 제거한 뒤에도 block/transaction/receipt/log를 읽고 기존 writer로 순차 처리했다.

선택적 `source.CommitValidator`를 추가해 state prefetch 유무와 관계없이 정상/중복 재시도/feature backfill의 커밋 직전에 source identity·generation·fresh hash를 검사한다. `port.BlockDurability`가 Pebble WAL을 요구하고 PostgreSQL fsync=on/synchronous_commit=on을 확인/강제한다. 같은 PostgreSQL Store handle이면 bound projection transaction에서 source view를 읽고 capture chain row의 SHARE lock을 commit까지 유지한다. MaxConns1에서도 중첩 pool transaction 없이 동작하고 CaptureUpdate/reset을 배제한다. 별도 handle/DB의 commit은 분산 원자적 transaction이 아니므로 관측/검사 뒤의 fork 가능성을 숨기지 않는다.

기존 durable commit 뒤 ACK 위치를 유지하며 source DB에 consumer/epoch/input별 `ProjectionReceipt`를 기록한다. 이것은 raw ACK·원본 GC·연속 완료 cursor가 아니다. ACK 유실 재시도는 fresh를 다시 확인하고 중복 feature/undo/outbox를 만들지 않는다. Source의 epoch는 고정이고 Reset 뒤 stale handle은 거부한다. 새 Source 생성 뒤 기존 HandleReorg/undo/outbox로 공통 조상까지 복구하고 새 branch를 재생하는 경로를 두 DB에서 확인했다. 실제 앱 supervisor의 자동 epoch 교체/CLI 활성화는7번에 남겼다.

`BindFeatures`가 알려진 balance/contract/code/ancestor/legacy genesis read 경계를 DB-only facts로 바꾼다. 계정/code/call 상태가 없는 현재 plan은 일반 coverage 오류를 내며 allow_pruned_state를 켜도0/latest로 대체하지 않는다. 검증된 초기 account 기록이 이미 있는 projection은 native delta를 처리하지만 새 account/resync/contract metadata에 상태가 필요하면 중단한다. 이번 잔액 비교는 fixture의 명시적인 알려진 초기값을 넣었으며 운영 snapshot/증명 공급을 구현한 것은 아니다. bootstrap anchor 자체의 header/body와 전역 transaction hash 색인도 현재 Source의 coverage에 포함하지 않는다. 자세한 조립/재시작/제한은 [facts Source](../SOURCE_FACTS.md)를 따른다.

`source_validate`와 `source_ack`의 bounded 단계 진단을 추가했다. 전자는 내구성/queued SQL flush/source DB fence/fresh RPC 대기를, 후자는 commit 뒤 receipt 저장을 측정한다. 기존 fetch_decode/ordered_write/database_commit도 유지하고 SQL이 다른 단계에서 먼저 flush된 경우 실제 Commit 호출 시간에 포함되지 않음을 명시했다.

검증 결과:

- macOS에서 facts writer·기존 fetch·source RPC/era/replay/nodefeed·capture/chainfeed·Arc·SDK/sdktest·체인 회계의 race 회귀 통과. Go 전체 패키지 compile 통과, 변경 패키지 vet 통과·lint0 issues. 마지막 영구 객체/잘못된 commitment 검사도 native race와 Linux에서 통과했다. 전체 app race/SLO 완료로 표시하지 않는다.
- 전용 fsync-on PostgreSQL16의 simple_protocol→extended exec/synchronous_commit=off DSN 조건에서 전체 SQL race415 tests/subtests 통과(123.741s). facts/capture/Arc105 tests/subtests 통과: facts30.969s, capture50.026s, Arc23.297s. 한 연결 pool의 read fence·내구성 강제·두 DB/일반 EVM/Arc의 모델/초기화된 잔액/log/outbox 의미 값 대조(수집 시각·RPC보고 Size 제외)·실제 branch 교체와 undo를 포함한다. 운영 DB는 사용하지 않았다.
- 실제 로컬 RLPx→window→capture→facts→기존 writer→처리 receipt의 Arc 전체 경로도 확인했다. Source read와 Reset 경합을 GOMAXPROCS=1/race/count200으로 통과(98.032s). read가 완료됐더라도 reset 뒤 commit/ACK는 stale로 거부한다. Source 입력 오류·commit 주입 실패·fresh 불일치·ACK 유실 재시도·재시작·없는 계정/계약 상태·영구 객체·DB digest는 맞지만 header commitment는 틀린 공급을 검증했다.
- Linux/arm64 Docker의 facts/fetch/capture/evmdata/Arc5개 패키지 통과. repo read-only bind/network none/임시 디렉터리에서 실행했고 SQL 환경이 필요한 Linux 검사는 skip했다. 실제 SQL 결과는 위 native 연결 검사다. 실제 Arc peer/외장 mount/24시간 운영·450ms 처리량을 검증했다고 주장하지 않는다.
- 시험용 컨테이너/명시적 볼륨과 Linux 바이너리217,666,556bytes를 제거했다. 기존7개 컨테이너 상태와 다른 볼륨을 보존했다. config.yaml·go.mod/go.sum·기존 handoff 문서는 변경하지 않았다. full node/스냅샷/공개 RPC/운영 활성화/commit/push는 수행하지 않았다.

증거는 `/private/tmp/indexer-facts-writer-20261015/validation.json`, 변경 전 SHA/파일, native/SQL/race/CPU1/Linux 로그와 cleanup 기록이다. 첫 WAL-disabled fixture 초기화 오류와 잘못된 reorg outbox 비교, lint 변수명 오류는 수정했고 실패 로그도 보존했다. 다음7번은 실제 설정/앱/multichain lifecycle·epoch supervisor·지원 chain/plan·운영 저장 경로/정책을 조립하는 단계다.

### 3.44 앱 feed 활성화·epoch supervisor·혼합 멀티체인 (10/15)

7번의 config/app/multichain lifecycle을 구현했다. `pkg/feed` registry와 strict `source.feed`/per-chain `feed`를 추가하고 Arc 모듈이 `arc.eth68`을 등록한다. 공통 앱에는 chain ID 분기를 넣지 않는다. 모든 enabled plan/window/feature를 IO 전에 검사하며 실제 RPC chain ID·genesis·anchor를 별도로 확인한다. 기본 RPC mode/config.yaml은 그대로다. source.feed에서는 5 HTTP/s 공유 pool과 source/projection commit의 fresh canonical 검사를 유지한다.

같은 DB handle의 capture와 DB facts projection을 별도 task로 실행한다. source generation 변경을 관찰하면 기존 live 작업·online backfill·writer·relay를 취소/join/close한 뒤 새 Source/fetcher를 만들고 undo/outbox로 복구한다. 미수집 bootstrap까지 undo할 때는 명시적인 RecoveryAnchor와 fresh hash 일치를 요구한다. anchor 이전 backfill은 동기/online 모두 시작 전에 coverage 오류로 거부한다. 재시작 때 마지막 commit을 멱등 재검증해 유실된 projection receipt를 복구한다. 한 task의 오류/종료나 Shutdown은 양쪽 join 뒤 DB/client를 닫는다.

외장 volume/root/path/reservation의 성공한 연결을 source DB에 동기 기록한다. 최초 initialize_volume/create_window는 명시적 선택이고, 기록된 window는 first-start flag가 남아 있어도 Open만 수행한다. 원본이 없어지면 생성하지 않는다. 기존 marker/quota/pending deletion/policy fence를 우회하지 않는다. module key는 명시적 private 파일에서만 읽고 enode/fork/listener 입력을 검증한다.

현재 앱 plan은 Arc 테스트넷 header/body/receipt뿐이며 StateReader와 dex.orderbook를 사전 거부한다. default balance.native/token.metadata를 꺼야 한다. 상태/영구 객체 앱 YAML·독립 worker 소유권·coverage API·다른 체인 peer 공급은8번 이후다. Arc만 feed, StableNet/BSC/Sepolia는 기존 RPC인 혼합4체인 구성을 검증했다. [실행/제한/초기화](../FEED_APP.md)에 설정 조각과 재시작 계약을 기록했다.

검증 결과:

- macOS app race 통합/회귀42 tests/subtests 통과(69.368s): 새 lifecycle·bootstrap reorg·누락 원본과 기존 RPC/era/replay/declared/multichain/undo. 별도 최종 backfill coverage·bootstrap·YAML round-trip 검사도 통과했다. fetch/source/facts/multichain/config/chain/SDK/Arc 경계 race 회귀와 전체 패키지 compile 통과, 최종 vet/lint0 issues. 전체 app race/SLO 완료 주장은 하지 않는다.
- fsync-on PostgreSQL16에서 simple_protocol→extended exec·synchronous_commit=off·앱 pool MaxConns=1 조건의 최종 앱17 tests/subtests 통과(43.334s). 마지막 coverage/미수집 bootstrap cursor2 tests도 통과(13.113s). 운영 DB를 사용하지 않았다.
- worker cancel/join·Stop 순서를 GOMAXPROCS=1/race/count200으로 통과(1.959s). 네 체인 fixture는 Arc RPC가503을 반환하는 동안 다른3체인의 추가 블록 commit을 확인했다. DB commit→receipt 사이 정상 간격은 완료 receipt를 기다려 검증한다.
- Linux/arm64 Docker에서 최종 앱18 tests/subtests 통과. repo read-only, network none, tmpfs에서 로컬 RLPx/HTTP oracle로 실행했다. 공개 Arc peer/실제 외장 mount/450ms·24시간·공개4체인 feed 측정은 아니다.
- 전용 PostgreSQL 컨테이너/명시적 볼륨과 Linux 시험 바이너리를 정리했다. 기존7개 컨테이너와 다른 데이터는 보존했다. config.yaml·go.mod/go.sum·10/12 handoff를 유지했다. full node/스냅샷/공개 RPC 운영 활성화/commit/push는 수행하지 않았다.

증거는 `/private/tmp/indexer-app-feed-20261015/validation.json`과 native/SQL/Linux/CPU1 로그·cleanup 기록이다. 첫 native 검사에서는 receipt의 post-commit 간격과 무자금 reorg fixture를 수정했다. 마지막 backfill fixture는 비활성 Through=0(실제 From=1) 대신 From=0의 명시적 Gap으로 고쳐 검사했다. 실패 로그도 남겼다. 다음은8번의 기능별 worker/cursor/coverage/API와 부족한 데이터 공급이며 운영 peer/mount 입력 확보 후9번의 처리량·장기 시험으로 이어진다.

### 3.45 기능별 완료·독립 불변 결과 worker·coverage HTTP API (10/15)

8번의 첫 단계인 cursor/transaction/epoch/조회 경계를 코드에 반영했다. facts Source의 ProjectionReporter가 exact input/anchor/current epoch를 제공하고 기존 writer가 실제 처리한 feature/part의 definition·chain·plan digest·구간/end hash를 동일 block transaction/undo에 기록한다. 중간 누락을 최대128구간으로 유지하며 최고 처리 높이를 연속 완료로 추정하지 않는다. 중복 block은 새 feature를 처리했다고 기록하지 않는다. 기존 backfill/Gap·정의 변경·stale generation도 확인한다. RPC/era/replay 경로에 새 완료 추정을 넣지 않았다.

선택 `feed.projections: [execution.summary.v1]`을 추가했다. 별도 goroutine이 source DB의 검증된 block/receipt를 재검증해 block별 거래·receipt·성공/실패/log/생성 계약 수와 gas used를 계산한다. 출력 최대64KiB·자체 연속 cursor·sequence/output event가 capture namespace의 별도 durable transaction에 들어간다. 기존 feature table/global undo/outbox를 공유하는 새 writer를 만들지 않는다. 모든 commit은 현재 generation/canonical/parent와 같은5 HTTP/s pool의 fresh hash를 확인한다. 원본 GC의 ACK로 사용하지 않는다.

reorg에서는 old epoch 계산을 버리고 exact current canonical input에 대한 과거 불변 결과 prefix를 최대128높이 뒤쪽 탐색으로 재사용한다. 없으면 bootstrap부터 DB 입력을 재생한다. own reset event 뒤 새 Source로 이어가며 이전 fork 결과는 명시적인 historical library 조회에만 남긴다. 앱의 수집/projection/선택 worker는 하나의 취소/join 집합으로 종료한 뒤 DB/client를 닫는다. definition 변경/일반 stateful 집계의 독립 undo·event/consumer 계약은 후속 작업이다.

`GET /indexing/status` 및 `GET /indexing/workers/{worker}/blocks/{height}`를 앱과 per-chain 경로에 연결했다. 첫 API는 최대100개의 feature/part에 대해 source plan/범위/epoch·projectionHead·기능별 nullable 완료/hash/gap/definition·worker cursor/sequence를 제공한다. 두 번째는 현재 canonical worker 완료 prefix의 불변 결과만 반환한다. projectionHead 자체는 gap-free completion 증거가 아니다. source coverage에서 account/code/storage/trace/blob/state input은 unavailable이다. 과거 epoch/미완료/미설정·손상 자료를 현재 데이터나0으로 대체하지 않는다.

projection의 일관된 bounded snapshot을 닫은 뒤 capture snapshot에서 현재 세대와 identity를 대조한다. PostgreSQL 한 연결과 별도 read-only API store를 지원하고 API 자체는 RPC를 호출하지 않는다. SQL 값/페이지/namespace 키는 모두 바인딩한다. no-store 응답이며 DB 오류·경로/키/URL·삭제 token을 노출하지 않는다. feature_progress 계측은 staged metadata와 앞선 queued SQL flush의 비용이며 durable 완료/ACK와 구분한다. 상세 계약과 설정은 [진행/독립 worker](../PROJECTION_PROGRESS.md)에 있다.

검증 결과:

- native app/race20 tests/subtests 통과(73.283s): 실제 로컬 RLPx→source→기능별 완료/독립 worker, 재시작·기존 branch 재조정·실패 rollback·status/출력 HTTP·혼합4체인 route/격리. source/fetch/capture/progress/API/chain/SDK/multichain/config race 경계 회귀도 통과했다. 실제 공개 Arc/외장 mount/450ms/24시간·전체 app race 완료 주장은 하지 않는다.
- fsync-on PostgreSQL16의 simple_protocol→extended exec/synchronous_commit=off·pool1 조건에서 앱20개 통과(91.696s), capture/facts94개 통과(capture30.101s/facts18.404s). 마지막 prefix 재사용/API 코드로 앱14개(37.265s)와 worker/status6개(3.05s)를 재검증했다. 별도 read-only store·API 조회의 RPC0회·quote/SQL 공격 형태 페이지·손상 metadata500·underlying read error 보존을 포함한다.
- final GOMAXPROCS=1/race/count200에서 worker commit/reset200개(23.752s), 세 task 취소/join200개(3.755s)를 통과했다. uncertain commit 재시도는 event를 중복하지 않고 다른 결과는 거부하며, stale worker는 현재 출력 API에 제공되지 않는다.
- Linux/arm64 Docker에서 앱22개·worker/status5개·progress2개 tests/subtests 통과. repo read-only/network none/tmpfs·로컬 oracle로 검사했고 SQL 의존 worker subtest는 skip했다. 실제 SQL은 위 native 연결 결과다. 시험 바이너리119,433,899bytes와 owned 컨테이너/명시적 DB 볼륨을 정리했다.
- 최초 import 이름/uint32/pointer 타입 컴파일 오류를 수정했고 로그를 보존했다. 실행된 race 검사에서 실패/데이터 race는 없었다. 최종 vet/lint·전체 compile과 protected 파일/기존7개 컨테이너 보존은 validation 기록에 남긴다. 운영 config.yaml/DB·go.mod/go.sum·10/12 handoff, full node/스냅샷·원격 배포/commit/push는 변경하지 않았다.

최종 자원 점검에서 기존 `postgres-postgres-1`과 `peace-endpoint-623-pg`가23:10 KST경 exited/ExitCode0/OOMKilled=false로 바뀐 것을 관찰했다. 이번 작업의 stop/rm 대상은 UUID 이름의 owned 시험 자원뿐이며 이 두 컨테이너를 재시작하거나 삭제하지 않았다. 기존7개 이름/데이터는 남아 있고 상태 변화는 `existing-state-changes.json`에 기록했다.

증거는 `/private/tmp/indexer-progress-workers-20261015/validation.json`과 각 native/SQL/Linux/CPU1/race 로그·cleanup 기록이다. 8번 전체 완료는 아니다. 다음은 hash/proof와 보존/예산을 정한 account/code/storage 공급, trace/blob·source 검색, 추가/stateful worker 소유권 계약이다. 운영 peer/mount 입력과9번의 동일 입력 처리량·장기 SLO 검증도 남아 있다.

### 3.46 지정 계정·code·storage proof 공급 library와 DB-only snapshot (10/15)

실제 Arc enode가 없다는 답변을 반영해 공개 peer 연결 없이 다음8번의 상태 공급 기반을 구현했다. `pkg/chainfeed/evmstate`와 Arc factory는 명시적 selection(최대8주소·총128슬롯)의 `eth_getProof`/선택 code 조회를 기존 shared5 HTTP/s client의 HTTP batch 한 번으로 요청한다. blockHash+requireCanonical을 고정하고 모든 member를 요구한다. profile-verified header의 state root→account MPT→storage MPT와 code keccak을 대조해 RPC 보고 값까지 검증한다. 없는 계정/슬롯/빈 code도 증명이 있어야 하며 pruned/누락을0/latest로 바꾸지 않는다. 증명된 account balance를 Arc native USDC 회계와 자동 동일시하지 않는다.

proof/code/selection 원본을 exact capture identity·codec·selection digest에 묶인 optional permanent supplement로 저장한다. commit은 기존 capture namespace의 동기 transaction에서 epoch/current canonical/fresh hash를 확인한다. 동일 재시도는 DB 원본을 재검증하고 fresh만 다시 조회하며 다른 바이트는 immutable conflict다. old epoch/fork는 현재 조회에서 거부하고, 새 epoch의 retained canonical ancestor는 재사용한다. read는 digest뿐 아니라 proof/code commitment를 다시 검증하며 RPC0회다. `facts.Source.StateSnapshot`을 연결했고 Arc factory/source wrapper의 지원·미공급 오류를 확인했다. 기존 값 파라미터 바인딩과 DB 내구성 경로를 재사용하며 새 SQL/table은 없다.

proof JSON2MiB·node65개/1024bytes·code256KiB·bundle8MiB를 제한한다. 물리 RPC 응답도 shared pool에서 JSON decode 전에64MiB로 제한하며 작은 override만 허용한다. 제한 초과 failover도 기존 limiter를 통과한다. Supplier Metrics는 RPCBatch/Verify/Commit의 횟수·실패·누적 wall time을 제공하며 limiter/fresh/fsync 대기를 포함하는 범위를 문서화했다. 실제 throughput percentile/전체 Acquire 소요나 coverage 완료를 대신하지 않는다.

이 단계는 호출 단위 library다. 기본3종 capture plan/digest·Through/FactsReady·feature writer/undo/outbox·ACK/raw GC 권한을 늘리지 않는다. 현재 coverage HTTP의 account_changes/storage_changes는 계속 unavailable이며 tracked state point와 전체 change dataset을 구분한다. 앱의 선택 YAML/acquisition scheduler·cursor/우선순위·부모 상태/checkpoint·필수 상태 plan/GC·coverage HTTP와 state feature 활성화는 다음 단계다. trace/blob/다른 peer module·실제 enode/mount/450ms·24시간 운영도 미완료다. [사용·한도·복구/예산](../STATE_PROOFS.md)을 따른다.

검증 결과:

- 독립 Geth trie builder/prover와 별도 JSON-RPC HTTP oracle로 account/slot 존재·부재, code, block hash/canonical selector·slot 순서·reverse batch response IDs를 확인했다. 잘못된 balance/nonce/address/root/path/code/slot, 누락 멤버/pruned/형식 오류·과대 node/cardinality·digest만 맞는 조작 원본을 거부했다. 두 주소의 proof/code4개가 HTTP1회로 묶이고 fresh는 별도1회였으며 같은 pool의6요청 간격으로5 HTTP/s를 확인했다. DB-only read는 추가 RPC0회다. 미요청 code는 proven-empty와 구분했고 selection의 순서는 digest를 바꾸지 않지만 범위/code intent 변경은 다른 selection이다.
- native boundary race11개 패키지 통과: chainfeed/ethpeer/evmdata/evmstate·capture·facts·rpcpool·Arc·chain coupling·SDK/sdktest. 최종 계측/factory/source 연결 뒤 native proof/Arc/pool 재검증과 선택 범위 추가 검사도 통과했다. Go 전체 패키지 compile·최종 vet·lint0 issues·diff whitespace 검사를 통과했다. 전체 app/SLO 완료로 표시하지 않는다.
- fsync-on 전용 PostgreSQL16, simple_protocol→extended exec·synchronous_commit=off 요청·proof pool1에서 proof/capture/facts 전체 race127 tests/subtests 통과: evmstate28개10.409s, capture81개42.493s, facts18개27.738s. 최종 계측/Arc factory 코드로33개를 추가 확인했다(evmstate28개5.999s/capture4개2.805s/Arc1개3.802s). 실제 운영 DB는 사용하지 않았다. uncertain commit·실패 rollback·재시작·generation/retained ancestor·fresh mismatch를 두 DB에서 확인했다.
- 새 supplement Commit/Reset 경합을 GOMAXPROCS=1/race/count200으로 통과(30.055s). proof read/reset의 반복 경계는 source/store의 epoch 검사를 따른다. 새 source/proof worker가 자동 스케줄된 운영 시험은 아니다.
- Linux/arm64 Docker에서 evmstate27개·supplement3개 tests/subtests 통과. read-only repo/network none/tmpfs에서 실행했고 SQL 조건의2개 subtest는 skip했다. 실제 SQL 결과는 위 native 연결 검사다. owned Linux 컨테이너2개와 임시 바이너리70,986,067bytes, 전용 SQL 컨테이너/명시적 볼륨은 정리했다. 기존7개 이름/데이터/이번 baseline 상태는 최종 자원 기록으로 대조한다.
- 최초 sandbox 캐시 접근 실패, test-only trie DB import의 추가 fastcache module 요구, lint의4개 unchecked Close를 수정/회피했고 로그를 보존했다. oracle은 기존 Geth in-memory trie builder/prover를 사용하므로 의존성 추가가 없다. config.yaml·go.mod/go.sum·10/12 handoff SHA를 유지했다. full node/스냅샷·공개 RPC·운영 활성화·commit/push를 수행하지 않았다.

증거는 `/private/tmp/indexer-state-proof-20261015/validation.json`, native/SQL/Linux/CPU1/race 로그와 cleanup 기록이다. 실제 Arc public RPC의 eth_getProof/hash selector 지원과 실제 P2P 속도는 미검증이며, 새 library의 배치가5 HTTP/s 안에서2블록/s를 보장한다고 주장하지 않는다.

### 3.47 자동 tracked 상태 수집·설정·atomic cursor와 coverage (10/16)

다음8번의 자동 공급 조립을 연결했다. opt-in `feed.state_proofs`와 per-chain 설정에 name/start_height/accounts(code·slots), 선택 poll_interval을 받는다. 정확한20/32바이트 hex·최대8주소/128슬롯·중복·오타·명시적 시작 범위와 모듈 capability를 IO 전에 검사한다. 현재 Arc testnet 모듈만 MPT 공급 capability를 선언한다. 기존 operating config.yaml은 그대로이며 상태 feature 시작 제한도 유지한다.

별도 goroutine이 canonical captured DB prefix를 durable 작업 큐로 읽는다. 메모리에는 한 블록만 활성화하고 수신·원천 처리·기존 projection·선택 요약과 독립적으로 동작한다. 각 proof batch 뒤 `CommitStreamSupplement`가 원본 bundle, exact input receipt, 연속 완료 cursor, 자체 input event를 같은 source namespace transaction에서 한 fresh 검사로 기록한다. 새 proof stream이 추가 fresh를 두 번 호출하지 않으며 동일5 HTTP/s client/limiter를 공유한다. 부분 공급/pruned/오류는 완료 높이를 건너뛰지 않고 전체 앱 task를 취소/join한다. 기존 source Through/FactsReady·feature undo/outbox·raw ACK/GC 권한은 변경하지 않는다.

name/codec/selection/start를 영구 고정한다. RawMessage가 JSON 저장 과정에서 compact/escape되어 digest를 바꾸는 정의는 거부하고 저장 후 동일 digest를 확인한다. restart는 마지막 완료 proof를 DB에서 재검증하고 다음 높이로 진행하며, 이미 저장한 proof는 provider가 pruned돼도 재사용한다. scope를 같은 이름으로 바꾸면 충돌한다. canonical mismatch는 기존 capture supervisor의 reset을 기다리고, 새 epoch에서 exact canonical receipt prefix만 최대128높이 뒤쪽 탐색으로 재사용한다. 없으면 명시한 시작 높이부터 재생한다. old fork는 현재 완료로 표시하지 않는다. 최대64회 epoch 재개 뒤에는 재구성이 필요하다.

`GET /indexing/status`와 chain mount에 선택 `stateProofs`를 추가했다. exact descriptor/selection/fromHeight·현재 epoch·nullable 완료 height/hash·backlog·sequence·waiting/not_started/lagging/caught_up/stale를 반환한다. 비교 대상은 captured DB prefix이고 네트워크 tip은 아니다. status는 작은 atomic receipt를 확인하며 최대8MiB proof를 매 poll마다 읽지 않는다. 실제 소비/read와 restart는 bytes/commitment를 재검증한다. read-only/no-store/RPC0회·SQL pool1 계약과 파라미터 바인딩을 유지한다. 전체 account/storage changes coverage는 계속 unavailable이며 새 상태값 HTTP endpoint를 추가한 것은 아니다. [설정·구조·남은 경계](../STATE_PROOFS.md)를 따른다.

검증 결과:

- native 초기 앱4개 scenario/race 통과(29.978s). 최종 실제 수집/projection/요약/proof4개 task 공존·실패 취소, 기존 source.feed 비활성 경로·선택 요약·혼합4체인까지 앱7개 scenario/race 통과(59.321s). 선택 시작 이전은 coverage 밖이며 pruned 이전 proof를 재시작 후 다시 요청하지 않았다. reorg 후 새 fork 완료 hash와 기존 projection undo 재개를 확인했다.
- chainfeed/ethpeer/evmdata/evmstate·capture/facts/progress/model/pool·Arc/일반 EVM/StableNet profiles·SDK·설정/multichain의 native race25개 패키지 통과. 전체 Go compile, 최종 vet/lint0 issues, gofmt/diff 검사를 확인한다. 전체 app/SLO 완료로 표시하지 않는다.
- fsync-on 전용 PostgreSQL16, simple_protocol→extended exec/synchronous_commit=off 요청 조건에서38 tests/subtests 통과(capture4개4.347s/evmstate30개7.904s/app4개30.706s). 최종 metadata/selection 코드와 실제4 task 공존으로11개를 재검사했다(capture4개3.257s/evmstate3개2.777s/app4개32.547s). 앱/proof pool1과 별도 read-only store에서 status의 RPC0회, partial write4지점·uncertain commit/event 중복 방지·scope 충돌·재시작·reorg를 확인했다. 실제 운영 DB는 사용하지 않았다.
- 새 atomic stream Commit/Reset을 GOMAXPROCS=1/race/count200으로 통과(21.935s). 수집/projection/선택 worker2개의 취소/join/Stop 순서는 같은 조건200회 통과(1.749s). metadata status가 proof payload를 읽지 않고 receipt I/O 오류는 숨기지 않는 것도 확인했다.
- Linux/arm64 Docker에서 앱4개·capture stream3개·rich proof runtime2개 tests/subtests 통과. repo read-only/network none/tmpfs·로컬 RLPx/HTTP oracle로 실행했고 SQL이 필요한2개 subtest는 skip했다. 실제 SQL은 위 native 결과다. Linux 임시 바이너리132,314,335bytes·owned 컨테이너3개와 전용 SQL 컨테이너/명시적 볼륨을 정리했다. 실제 외장 mount/Arc peer·450ms·24시간 운영 검증은 아니다.
- 최초 lint의 De Morgan 스타일 경고1개를 수정했고 로그를 보존했다. 실행된 race에서 실패/데이터 race는 없었다. config.yaml·go.mod/go.sum·10/12 handoff SHA와 기존 Docker baseline/owned cleanup은 최종 validation 기록에서 대조한다. full node/스냅샷·공개 RPC·운영 활성화·commit/push는 하지 않았다.

증거는 `/private/tmp/indexer-state-scheduler-20261016/validation.json`과 native/SQL/Linux/CPU1 로그·자원 기록이다. 다음은 부모 상태/checkpoint·provider 보존/지연 운영 기준·필수 상태 plan/GC 및 stateful feature 연결이다. trace/blob·검색·추가 worker 소유권과 실제 Arc enode/mount·장기 처리량/SLO도 남아 있다.

### 3.48 부모 상태 checkpoint·필수 완료/GC 계약 (10/16)

다음8번의 부모 상태와 필수 완료/삭제 경계를 연결했다. `state_proofs.parent_checkpoint: true`는 exact selection의 start_height-1 상태를 먼저 검증·영구 보존한다. `required: true`는 parent도 자동으로 켜며 source State에 descriptor를 고정한다. 추가는 첫 intake 이전(Through=anchor·event0·finalized/pending deletion 없음)에만 허용하고, 기존 수집 DB를 자동 전환하지 않는다. 앱/session은 raw Open/과거 삭제 복구 전에 계약을 확인한다. required 설정의 제거·이름/범위/모드 변경으로 우회할 수 없다. operating config.yaml은 변경하지 않았다.

부모가 captured block이면 DB header를 사용한다. 부모가 bootstrap anchor이면 해당 hash의 header JSON을 EIP-1898 proof/code와 같은 HTTP batch로 받으며, 새 optional profile HeaderCodec이 header hash를 반드시 재계산한다. 기존 hash 규칙·알려진 suffix를 유지하고 header/number/hash/state root·MPT·code를 검증한다. parent checkpoint bytes·cursor parent·자체 checkpoint event는 한 transaction/한 fresh로 기록한다. captured 부모/anchor 모두 새 공급의 물리 batch1+fresh1이고 저장된 복구는 RPC0이다. bootstrap에 body/receipt/가짜 capture manifest가 있다고 표시하지 않는다.

parent가 없으면 첫 input 완료를 거부하며, source reorg 후 새 canonical parent만 인정한다. `StateTransition`은 DB-only before/after의 block/epoch/selection/accounts를 반환한다. `RequiredSupplementReady`는 명시한 필수 범위의 실제 proof bytes를 검증한다. base3종 plan/Through와 상태가 필요 없는 projection/summary 완료점은 독립적으로 유지한다. 전체 account/storage changes·native USDC 회계·eth_call/stateful feature를 이 tracked snapshot으로 자동 활성화하지 않는다.

raw GC는 기존24h·finality/canonical·base bytes·모든 raw ACK·lease 조건에 필수 checkpoint/연속 완료/current·previous proof 검증을 추가한다. 실제 digest/header hash/MPT/code 재검증을 deletion fence와 같은 source transaction에서 수행한다. proof SHA와 input receipt를 함께 조작해도 MPT 불일치를 거부한다. segment 안 같은 높이의 검증은 metadata로 중복 제거한다. verifier 없는 다른 handle과 pending 삭제 복구도 fail closed다. 정상 checkpoint/proof lag는 raw를 보관하며 수집을 계속하고, 완료를 주장한 입력의 손상/I/O는 숨기지 않는다. 새 SQL/table/의존성 없이 기존 bound-value 저장 계약을 사용한다.

검증 결과:

- 새 native race: capture/checkpoint/header/rich MPT/app15 tests/subtests 통과(실제 SQL이 필요한4개 skip). app의 captured-parent·bootstrap-header 재시작/pruned 재사용·필수 설정 삭제/변경 거부·부모 실패 전체 task join·부모 reorg를 확인했다(46.622s). 관련22개 패키지 race 회귀758개 통과,41개 환경 skip; 기존 chain/profile/수집/GC/facts/status/pool 경계를 확인했다. 전체 SLO 완료를 뜻하지 않는다.
- 전용 fsync-on PostgreSQL16, simple_protocol 요청→extended exec·synchronous_commit=off 요청→capture 강제 on, 앱/proof pool1에서 최종27 tests/subtests 통과·skip0(capture8.244s/evmstate6.585s/evm2.280s/app51.672s). checkpoint 부분3 write·uncertain commit·계약 고정·정상 GC 대기/복구·실제 MPT before/after와 proof/receipt 동시 조작을 검사했다. 실제 운영 DB는 사용하지 않았다.
- 최초 SQL 회귀에서 취소 중 pgx transport I/O timeout이 context.Canceled를 빠뜨리는 경합을 발견했다. Run의 최종 반환에 실제 I/O 오류와 owner cancellation을 함께 보존하도록 수정했다. 결정적 fault oracle로 둘 다 반환하는지 검사하고 native/SQL/Linux 및 CPU1/race/count200으로 확인했다. 기존 오류를 cancellation으로 숨기지 않는다. 최초 fixture의 fault wrapper 적용 경로·불완전 header suffix·복구 후 이미 삭제된 segment의 기대값·nonce pointer 문법을 수정한 실행 로그도 남겼다.
- checkpoint/reset 경합 CPU1/race/count200 통과(46.456s). 네 task join/Stop200회(6.292s), cancellation/driver-error 보존200회(19.974s) 통과했다. 데이터 race는 발견하지 않았다.
- Linux/arm64 read-only repo/network none/tmpfs에서 새 테스트15개 통과(실제 SQL4개 skip). owned 컨테이너3개·임시 바이너리132,584,053bytes, 두 차례 SQL 시험의 UUID 전용 컨테이너/명시적 볼륨을 모두 정리했다. 기존 Docker7개 전체 State가 baseline과 같고3개 running/4개 exited다.
- 전체 Go compile·변경 영역 vet·lint0 issues·gofmt/diff를 확인했다. config.yaml·go.mod/go.sum·10/12 handoff SHA와 브랜치를 유지했다. full node/스냅샷·공개 RPC 시험·운영 활성화·commit/push는 수행하지 않았다.

증거는 `/private/tmp/indexer-state-required-20261016/validation.json`과 각 native/SQL/Linux/CPU1 로그·자원 기록이다. 다음은 상태 공급 지연/보존 기준 및 실제 tracked 상태를 소비하는 독립 worker의 저장/undo 연결이다. trace/blob·검색·추가 worker 소유권과 실제 Arc peer/외장 mount·장기 처리량 검증도 남아 있다.

### 3.49 지정 상태 후처리·독립 head/undo/event와 조회 API (10/16)

다음8번의 첫 전용 상태 후처리를 연결했다. opt-in `feed.state_proofs.project_changes: true`는 parent checkpoint 모드도 켜고 exact proof stream/버전1 정의를 고정한 `tracked.<name>` worker를 실행한다. 선택한 주소·슬롯의 consensus balance/nonce/Exists/codeHash/storageRoot/storage 값을 현재값으로 보관하고 블록별 순변경을 기록한다. code bytes는 영구 proof에 남기며 중복하지 않는다. 전체 변경 열거·native USDC 회계·임의 eth_call·기존 balance/token 기능 활성화로 확대하지 않는다.

worker는 captured/proof DB prefix를 durable 큐로 소비하며 한 블록만 활성화한다. source transaction 안의 순수 reducer가 부모/현재 header hash/MPT/code와 exact selection을 검증하고, 실제 저장한 before 값과 CAS 대조한다. 불변 output(전체 선택 after 값/변경/digest)·자체 undo·가변 head·연속 cursor·apply event를 한 source transaction에 저장한다. 실제 적용/retry의 fresh1회는 기존5 HTTP/s pool을 사용한다. 상태/원본 RPC나 window fallback은 없으며 기존 feature의 전역 undo/outbox·raw ACK를 변경하지 않는다. 현재값128KiB/변경256KiB, 기존 selection8주소/128슬롯 상한과 bound-value SQL/WAL+sync/fsync 계약을 유지한다.

source epoch 변경 시 자기 head의 undo만 역순으로 적용해 exact canonical input prefix 또는 부모까지 복원하고, 부모도 달라지면 active head를 비운다. revert들·head·새 cursor/epoch·reset event는 한 transaction이며128 undo를 넘으면 일부 변경 없이 ErrRebuildRequired다. fork 결과와 undo/event는 역사 자료로 보존한다. retry/uncertain commit은 event를 중복하지 않는다. 일반 proof 지연만 기다리며 완료를 주장한 bytes 누락/손상/I/O는 중단한다. runtime은 epoch 경합을 재개하고 fresh canonical mismatch는 기존 capture supervisor를 최대2분 기다린다. pgx 취소 I/O와 owner cancellation은 함께 보존하며 다섯 task를 모두 join한 뒤 DB/client를 닫는다.

`/indexing/status`의 stateChanges와 `/indexing/state/current`, `/blocks/{height}`, `/events?after=&limit=`, `/inputs/{height}/{hash}/{generation}`을 추가했다(뒤3개도 `/indexing/state` 하위). chain mount/read-only/no-store/RPC0·pool1·bounded page를 유지한다. current/blocks는 현재 epoch의 완료 범위만, inputs는 명시한 역사 input을 반환한다. 원천 plan/anchor와 descriptor를 대조하고, 완료 뒤 사라진 head/output/event는 정상404로 숨기지 않는다. 오류 body는 내부 DB/경로 세부 사항을 노출하지 않는다. 외부 소비자는 descriptor/sequence로 재전달을 중복 제거하고 자기 sink와 소비 sequence를 같은 transaction에 기록해야 한다. 서버가 외부 ACK/transaction을 대체하지 않는다. [설정·소유권·API·한계](../TRACKED_STATE_PROJECTION.md)를 따른다.

검증 결과:

- 실제 Geth trie fixture의 balance123456→999999, nonce7→8, codeHash와 slot42→99, 계정 삭제/생성과 fork 복원을 두 DB에 대조했다. state worker가 추가하는 RPC는 적용 fresh1회이며 공급 RPC는0이다. 잘못된/누락된 proof 뒤 완료를 올리지 않고, 이미 완료를 주장한 누락 bytes도 정상 대기로 처리하지 않았다.
- native 앱 새3 scenario/실제5 task·DB-only API·pruned 재시작·reorg·실패 취소 통과(앱23.836s). 기존 feed 재시작/혼합4체인 격리/summary/required proof6 tests/subtests race 통과(60.162s). 관련11개 패키지 race206개/환경42skip, SDK/config/multichain/모듈 분리5개 패키지188개/skip0 통과했다. 전체 app/SLO 완료를 뜻하지 않는다.
- 전용 fsync-on PostgreSQL16의 simple_protocol 요청→extended exec·synchronous_commit=off 요청→capture on·앱/proof pool1에서17 tests/subtests/skip0 통과(capture18.237s/evmstate4.081s/app43.602s). apply 부분5 write·reset 부분5 write·uncertain commit·fresh 불일치·자체 undo/event·128 한도·역사 출력·read-only pool1·실제5 task를 확인했다. 최종 API 원천 범위/완료 뒤 누락 자료 처리를4개로 재확인했다(capture3.000s/app20.284s). native 최종 API scope19.703s/reader2.328s도 통과했다.
- 상태 Apply/Reset 경합 CPU1/race/count200 통과(48.963s), 다섯 task join/Stop200회(6.511s), state worker 취소 중 driver I/O 보존200회(17.630s) 통과했다. 실행한 테스트의 실패·데이터 race는 없었다.
- Linux/arm64 network none/read-only repo/tmpfs에서12개 통과·실제 SQL3skip. 최종 reader/API scope3개 통과·SQL1skip. owned 컨테이너5개와 임시 바이너리228,120,948bytes를 정리했다. 두 SQL 시험의 UUID 전용 컨테이너/명시적 볼륨도 제거했다. 기존 Docker7개의 State와 protected 파일 SHA는 최종 validation 기록으로 대조한다.
- 전체 Go compile·변경 영역 vet·최종 lint0 issues·gofmt/diff 확인. test-only if/switch 형식 경고1개를 정리했고 로그를 보존했다. 운영 config.yaml·go.mod/go.sum·10/12 handoff·브랜치/기존 사용자 변경을 유지했다. full node/스냅샷·공개 RPC/실제 Arc enode·운영 활성화·commit/push는 수행하지 않았다.

증거는 `/private/tmp/indexer-state-projection-20261016/validation.json`과 native/SQL/Linux/CPU1 로그·자원 기록이다. 다음은 공급/후처리 지연·보존의 운영 기준과 각 단계 계측이다. trace/internal call·blob/검색·다른 상태 기능의 소유권, 실제 peer/외장 mount·장기 처리량/450ms 검증은 남아 있다.

### 3.50 수신·proof·상태 후처리 지연과 적체 계측 (10/16)

opt-in `feed.telemetry: {receive_interval_target: 450ms}`를 구현했다. 메모리에만 단계별 count/failures/total·최근256개 nearest-rank p50/p95/p99·inFlight/연속 busy 시간, 각 완료 단계의 exact input 시각 최대1024개를 보관한다. raw 작업 큐 크기와 별개다. recorder는 프로세스 최대64개, random runId와 chain/genesis/anchor/plan으로 분리한다. runId/목표를 기존 projection descriptor에 포함하며 hot-path 진단용 DB transaction·SQL/table·fsync나 추가 RPC를 만들지 않는다.

수신 Next/연속 header 간격·append/quota/fsync·raw 대기/assemble·capture commit/간격·proof acquire/batch/header-MPT 검증/commit·상태 queue/apply·기존 pool token 대기/물리 HTTP 시도를 연결했다. coalescer의 첫 token 예약과 failover의 추가 예약도 그대로 계측하며5 HTTP/s·burst1·fresh 경로를 변경하지 않는다. operation들은 중첩되므로 duration 합계를 end-to-end로 표시하지 않는다. 정상 proof 미완료 polling은 stateApply 성공/실패 표본에서 제외한다. proof 부모 checkpoint의 batch/검증/commit도 계측한다.

`/indexing/status.pipeline`은 동일 capture snapshot의 proof/state backlog와 ready-state backlog, 현재 실행에서 알 수 있는 첫 대기 입력의 나이, 별도 runtime snapshot을 반환한다. 완료점은 exact input/generation으로 구분하며 reset 전 progress를 현재값으로 반환하지 않는다. metrics는 전체 실행의 표본이다. 네트워크 head는 null이며 최근 수신 header를 실제 latest로 대신하지 않는다. 측정 전/재시작/다른 API 프로세스/시각 ledger 범위 밖에서는 null/unavailable이며0 지연으로 보완하지 않는다. API는 read-only/no-store/RPC0이다. managed task join 후 stopped, Shutdown에서 pool 관측 해제/registry 정리를 한다.

[계측 설정·stage 정의·운영 기준](../FEED_TELEMETRY.md)을 추가했다. 450ms는 최소2개 최근 header 간격의 p95를 비교하는 관측 경고이며 chain 대기까지 포함하므로 SLO 성공으로 취급하지 않는다. proof pending age는 capture 후 관측 시간부터라 remote state 나이/남은 retention의 보장이 아니다. provider 보존 높이/시간·중단 여유가 확인되기 전 임의 pruning 안전선을 적용하지 않는다. 디스크24시간은 RPC의 원격 trie24시간을 보장하지 않으며 required GC/기존 완료/finality/raw ACK/lease와 strict 오류는 유지한다.

검증 결과:

- native 관련9패키지 race86 tests/subtests 통과·환경17skip(앱57.312s), 최종 설정/SDK/다중 체인/모듈·RPC/계측10패키지 race262개 통과·환경2skip. 기존 state/reorg/required/capture 경계와 새 계측/재시작/누락 표본/API RPC0를 확인했다.
- 전용 fsync-on PostgreSQL16에서 telemetry 앱 재시작·read-only pool1·기존 state worker/reorg/join을 포함한5 tests/subtests 통과·skip0(앱34.521s). simple_protocol 요청은 기존 extended exec로, synchronous_commit=off 요청은 capture 강제 on으로 처리했다. SQL 값 바인딩·기존 저장 계약을 변경하지 않았다.
- collector 동시 접근·세대별 lag/unknown 처리를 CPU1/race/count200으로 최종400개 통과했다. coalesced batch 두 logical 요청→첫503+failover의 물리2회, token2회·같은5 HTTP/s 대기를 확인했다.
- Linux/arm64 read-only repo/network none/tmpfs의 새 계측·RPC·API/재시작7 tests 통과·skip0. UUID 전용 컨테이너4개와 임시 바이너리88,071,620bytes를 정리했다. SQL 전용 UUID 컨테이너/볼륨도 제거했고 기존 Docker7개(3 running/4 exited) 상태가 시작과 같았다.
- 초기 테스트의 ‘task join 직후 모든 pool stage가 idle’ 기대는 이미 예약된 shared dispatcher token이 drain될 수 있어 틀렸다. 실제 inFlight를 노출하고 테스트가 drain 완료를 기다리도록 수정했다. Go 최소1.24에 없는 테스트 전용 WaitGroup.Go도 기존 Add/go/Done으로 바꿨다. 수정 뒤 race 반복·vet·lint0 issues·전체 compile·gofmt/diff를 확인했다. 초기 실패 로그는 보존했다.
- 작은 로컬3블록 fixture의 두 DB 결과는 계측 지점 구분을 확인하는 근거이며 실제 Arc 처리량/450ms·장기 운영의 증거가 아니다. 운영 config.yaml·go.mod/go.sum·10/12 handoff SHA·기존 브랜치를 유지했다. 실제 peer/full node/스냅샷·공개 RPC/운영 활성화·commit/push는 수행하지 않았다.

증거는 `/private/tmp/indexer-feed-telemetry-20261016/validation.json`, 각 native/SQL/Linux/CPU1 로그와 추출한 local fixture pipeline snapshot이다. 추가 stateful 기능의 소유권/undo 연결·trace/internal call/blob 공급/검증·전용 검색, 실제 peer/외장 mount/원격 상태 보존 기준·장기/450ms/SLO 비교는 남아 있다.

### 3.51 선택 호출 trace 수집과 DB 전용 fork 고정 조회 (10/16)

남은8번의 첫 trace 공급을 연결했다. `feed.call_traces`의 명시적 name/provider/start_height와 module profile로 Geth callTracer 형식을 고정하며 기본 비활성이다. 같은 chain-wide5 HTTP/s client에 exact hash의 debug_traceBlockByHash batch1회를 요청하고 onlyTopCall=false/withLog=false/reexec0·tracer timeout5s·전체 요청15s를 고정했다. 임의 tracer/override/latest fallback은 없다. 실제 Arc debug 노출/형식·부모 상태 보존은 아직 확인하지 않았다.

profile-verified header/body/receipt를 재검증하고 trace의 거래 수/순서/hash와 root의 sender/대상/value/input/gas/receipt gas·성공/생성 주소를 대조한다. post-state receipt/opaque tx/누락·과대·알 수 없는 형식·중복 JSON key는 전체 거부한다. 원문 result4MiB·4096tx·16384frames·깊이64·input/output각64KiB·error각4096bytes와 supplement8MiB 한도를 적용한다. recursive frame allocation 전 JSON 구조/개수/depth를 제한한다. path와 부모 revert를 보존하고 absent value는 null이다. 하위 실행/누락은 provider 보고이며 자체 EVM/상태 증명이 아니다. verification=provider_reported_root_consistency, coverage=reported_call_tree로 표시한다. native USDC 회계나 legacy InternalTransaction/잔액 table로 승격하지 않는다.

ordered 독립 worker가 captured DB prefix를 큐로 사용하고 한 블록만 활성화한다. 정확한 응답 bytes와 identity/provider/format/profile의 immutable bundle·input receipt·자체 연속 cursor·input event를 기존 source transaction/fresh1회로 저장한다. retry는 DB bytes/의미를 재검증하고 fresh1회만 실행한다. 재시작 마지막 완료 입력과 retained canonical prefix를 확인하고 source reset에 재조정한다. unavailable/pruned/실제 손상은 건너뛰지 않고 전체 task를 취소/join한다. 선택 proof·상태·summary와 함께 최대6개 task 구성과 종료를 검증했다. base Through/plan·feature/global undo/outbox·raw ACK/GC/required proof 계약은 변경하지 않는다. 새 SQL/table/의존성을 추가하지 않았다.

`capture.NewReader`는 RPC/write/ACK/삭제 메서드 없는 DB/명시적 영구 객체 읽기 facade다. `facts.Source.CallTraces`와 `arcfeed.NewCallTraceSupplier`를 연결했다. `/indexing/status.callTraces`는 current epoch descriptor/완료/backlog/자체 sequence를, `/indexing/traces/blocks/{height}`는 DB-only frame 페이지를 제공한다. limit1..256·route 동시2개·초과429·read-only/no-store/RPC0이고 다음 페이지는 epoch/hash/selection을 모두 고정한다. 누락 고정값/잘못된 offset은400, fork/정의 변경409, 미완료404, 기능 없음503, 완료 뒤 누락/손상과 잘못된 DB metadata는500이다. source anchor/plan도 대조한다. 기존 base traces coverage는 unavailable이다. telemetry에 traceAcquire/Batch/Verify/Commit을 추가했다. [설정·검증·소유권·운영 제한](../CALL_TRACES.md)을 따른다.

검증 결과:

- 고정 Geth1.16.5 native tracer hooks로 생성한 독립 wire fixture를 체크인했다. CALL→revert하는 부모 아래 성공한 자식·DELEGATECALL·STATICCALL·SELFDESTRUCT·CREATE 성공/실패와 signed body/receipt를 대조한다. 실제 EVM 실행/Arc 입력의 증거는 아니다. tracer 전체 test import가 새 indirect 모듈을 요구해 별도 임시 modfile에서 생성했고 운영 go.mod/go.sum은 유지했다.
- native 새 trace/app28 tests/subtests 통과·SQL2skip(앱24.808s), 관련12패키지 전체 race228개 통과·환경42skip, 기존 feed/proof/state/telemetry/혼합 체인/SDK·설정49개 통과·skip0(앱137.307s). 초기 fixture의 잔액 부족·빈 trie 전용 proof와 일반 tx fixture 혼용·위조 대상이 root 대신 delegate child였던 시험을 수정했다. 실패 로그도 보존했다.
- fsync-on PostgreSQL16·simple_protocol 요청→extended exec·synchronous_commit off 요청→capture on·app/proof pool1에서34 tests/subtests/skip0 통과(앱27.411s). raw/정의/단계 검증·실제 재시작·provider disabled 중 DB 조회·reorg·read-only pool1·선택6 task와 기존4-write partial/uncertain-commit stream 계약을 확인했다. 최종 malformed metadata/API scope를 별도 SQL1개/skip0(앱15.536s)와 native2개/skip0(앱14.322s)로 재확인했다.
- CPU1/race/count200으로 decoder 동시 읽기·driver I/O와 cancellation 보존·6 task join600개, API admission/실패 후 slot 반환200개를 통과했다. trace HTTP 실패 전환의 물리3회(trace 첫503+fallback+fresh)도 동일5 HTTP/s 간격을 확인했다. 완료 digest를 다시 계산한 root 위조를 DB-only read에서 거부했다.
- Linux/arm64 read-only repo/network none/tmpfs에서29 tests/subtests 통과·실제 SQL2skip. UUID owned 컨테이너2개와 임시 바이너리100,714,056bytes를 제거했다. 두 차례 SQL용 컨테이너/명시적 볼륨도 제거했다. 시작의 기존7개 컨테이너 상태는 유지하며 동시에 다른 작업이 만든 wbft-own-input-dump-20261016에는 손대지 않았다.
- 전체 compile·변경 영역 vet·최종 lint0 issues·gofmt/diff를 확인했다. linter의 전역 실행 잠금은 다른 프로세스를 중단하지 않고 지원되는 parallel 옵션으로 실행했다. 조건식 권고1개는 동등한 논리식으로 정리했다. 운영 config.yaml·go.mod/go.sum·10/12 handoff·브랜치의 보호 SHA/이름이 시작과 같았다. full node/스냅샷·실제 peer/공개 RPC·운영 활성화·commit/push는 수행하지 않았다.

증거는 `/private/tmp/indexer-call-traces-20261016/validation.json`과 native/SQL/Linux/CPU1 로그, native fixture 생성 소스/표본과 자원 기록이다. 실제 provider/보존/Arc 성능·450ms/24시간 검증, blob sidecar 공급/검증·전역 검색/관계형 색인·포괄 실행 증명과 추가 stateful worker는 남아 있다.

### 3.52 전체 blob 검증·공급 worker와 영구 DB 조회 (10/16)

다음8b 항목으로 `pkg/chainfeed/evmblob`의 전체 EIP-4844 blob 검증/공급 library를 구현했다. profile로 재검증한 영구 header/body/receipts에서 서명된 type-3 거래의 versioned hash를 위치별로 추출하고, 전체 blob·commitment·단일 KZG proof를 chain/genesis/높이/hash/ordinal에 결합한다. 같은 해시의 반복 참조는 허용하고 누락·추가·순서/버전/크기 오류·비정규 field/잘못된 곡선점을 거부한다. header gas를 참조 수와 대조한다. zero/constant-one 다항식의 독립 algebraic 벡터와 별도 서명 거래를 사용했으며 production commitment/proof 생성 함수로 기대값을 만들지 않았다.

고정 relay RLP `eth.full-blobs.rlp.v1`은 Ethereum beacon SSZ/pooled-transaction transport 자체가 아니다. 전체32 blobs/응답 약4.01MiB/선택 supplement8MiB/페이지4개를 운영 상한으로 둔다. 할당 전 RLP cardinality/고정 길이와 정규 encoding을 검사한다. PeerDAS cells/sample·다중 cell proof/미래 버전은 이 형식으로 인정하지 않는다. KZG/거래 commitment 검증은 EVM execution·독립 consensus finality·포괄 data availability 증명이 아니다. [계약과 근거](../BLOB_INPUTS.md)를 따른다.

명시적 Provider는 context와 concurrent Close/Read 계약을 따른다.15초 bounded 수집과 취소 시 reader close/callback join을 구현했다. 실행 transport/default endpoint/가짜 Arc RPC는 추가하지 않았다. 실제 chain별 공급 adapter는 기존 canonical client와 같은5 HTTP/s pool을 빌려야 한다. 시험 전용 relay RPC를 통해 공급/fresh/failover가 동일 물리 제한을 공유하는 것을 확인했다. 실제 Arc blob 적용·공급/보존과 앱 config/HTTP 등록은 남는다. 빈 결과는 검증된 본문에 type-3 참조가 없을 때만 만들며, 누락/만료 자료를0/latest나 빈 결과로 대체하지 않는다.

독립 순차 Run은 captured prefix를 durable queue로 사용한다. 검증한 원응답과 input receipt/cursor/자체 event의4 write를 기존 source transaction/fresh1회로 commit한다. 입력 재시도는 bytes/KZG를 다시 검증하고 fresh도 재실행한다. 재시작/reorg·이전 epoch 거부·마지막 완료 재검증·취소/driver I/O 오류 보존을 따른다. `capture.NewReader`와 `facts.Source.Blobs`는 RPC 없는 exact epoch/identity/selection 페이지를 제공하며 digest를 재계산한 DB 변조도 의미 검증에서 거부한다. blob JSON은 값/포인터 전달에 관계없이 hex로 고정했다. `blobAcquire/Supply/Verify/Commit`의 bounded context 계측을 추가했다. 기본3종 plan/Through·raw ACK/GC·기존 feature/global undo/outbox·필수 상태 gate는 확장하지 않았다. 새 SQL/table/의존성은 없으며 기존 bound-value 저장을 사용한다.

검증 결과:

- 최종 macOS blob race29 tests/subtests 통과, 실제 SQL이 필요한3개 skip(15.411s). 관련 facts/trace/telemetry race43개 통과·환경 skip6개, 기존 앱 feed telemetry/trace/여섯 task join 회귀7개 통과(33.547s). 전체 앱/SLO 완료를 뜻하지 않는다.
- PostgreSQL16 fsync-on·pool1·DSN simple_protocol/synchronous_commit=off 요청에서도 adapter의 binding/강제 내구성 경로로 최종38개 통과·skip0(모듈20.029s). 공급 실패·검증 실패·4 write별 rollback·fresh mismatch·재시작/재시도·reset·DB 의미 변조·facts DB-only/epoch·기존 generic stream을 확인했다.
- 새 검증 동시 실행·공급 취소/close/join·worker 취소/I/O 보존·완료 후 join4개 시험을 각각 CPU1/race/count200, 총800회 통과(50.720s). 이후 JSON 직렬화/경계 시험을 추가하고 모듈 native/SQL/Linux를 다시 확인했다. 데이터 race나 goroutine 잔류 실패는 없었다.
- 최종 Linux arm64/CGO0·network-none/read-only 컨테이너에서31개 통과·SQL 환경 skip4개. 전체 컴파일·변경 경로 vet·전체 lint0 issues·gofmt·diff 검사를 통과했다.
- 이 단계의 두 차례 SQL 임시 컨테이너/명시적 named volume과 네 Linux 컨테이너/시험 binary156,609,192바이트를 정리했다. 최초/최종 개별 inventory의 기존 Docker8개는 보존했다. 중간 system df의 일시적 개수는 정리 대상으로 삼지 않았다. full node/운영 DB/실제 Arc peer·snapshot은 사용하지 않았다. go.mod/go.sum/config.yaml/기존 handoff SHA256과 refactor/arc-node-feed branch를 유지했다.

로컬 근거는 `/private/tmp/indexer-blobs-20261016/validation.json`과 각 race/Linux/정적 검사 로그다. 실제 sidecar retention 전에 durable 수신하는 transport/window 연결, 공급 capability·장기 운영과 source 검색은 남는다. 로컬24시간 raw window가 원격 blob 보존을 보장하지 않는다.

### 3.53 Arc blob 정책 감사와 공급 차단·DB 상태 연결 (10/16)

이전 단계의 실제 Arc blob 공급 여부를 확인했다. 고정 공식 Arc `6e764023ee6515fe70573e123ed2db912a7207b4`의 execution txpool은 `.no_eip4844()`와 blob cache0을 사용하며 같은 파일의 시험도 EIP-4844 거래 거부를 확인한다. [공식 근거](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/execution-txpool/src/pool.rs). execution/validation/payload 경로도 읽었으나 이 결론의 범위는 **고정 노드 구성의 transaction admission 정책**이다. 이를 모든 과거 블록의 blob 부재·독립 consensus/fork 증명·실제 public endpoint 실행 버전 확인으로 확대하지 않았다. 작은 공개 소스5개와 tree/audit SHA만 보관했고 노드/스냅샷/Rust 실행은 하지 않았다.

정책은 `pkg/chains/arc`에 두고 선택 `BlobCapabilityProfile`로 전달한다. generic/optional interface가 없는 profile은 unknown/not_audited이고, 사용자 정의 embedding profile은 원래 audit를 상속하면서 외부 ID에 결합한다. 새 정책 metadata 때문에 기존 custom profile ID를 lower-case supplement label로 제한하지 않는다. code-owned audit의 version/reason/basis·pinned source/revision을 검증하며 값 복사본을 사용한다. generic 코어에 chain-ID 분기를 넣지 않았다.

`evmblob.New`와 profile 기반 DB-only snapshot/page는 unsupported를 provider/DB/fresh RPC 전에 거부한다. 검증된 빈 결과로 대체하지 않고, 표준 Decode/Verify 및 명시적 provider를 받는 unknown profile library는 유지한다. Arc module audit는 순수 config 사전 검사에서 검증하고 실제 Open 결과의 profile과 대조한 뒤 Program에 한 번 결합한다. `blobs` YAML은 실행 가능한 transport가 없으므로 여전히 strict unknown-field 오류다.

앱은 audit를 기존 durable projection descriptor와 같은 transaction에 기록한다. `/indexing/status`의 `source.blobCapability` 의미는 [Blob 입력 계약](../BLOB_INPUTS.md#arc-정책과-상태-조회)을 따른다. DB-only 상태 조회의 필드이며 dataset coverage/완료 cursor/Through·기존 plan/raw ACK/GC를 바꾸지 않는다. 과거 descriptor에 정보가 없으면 null로 두고 현재 registry/chain ID/header0에서 소급 추정하지 않는다. 잘못되거나 unpinned인 audit와 unsupported+required-blobs 모순은 거부한다. 손상된 DB metadata는 HTTP500이며 client400이나 빈 결과로 숨기지 않는다. 운영 config.yaml·본체 go.mod/go.sum·이전 handoff·브랜치와 공유5 HTTP/s/fresh/SQL binding을 유지했다.

검증 결과:

- 최종 native race: 새 정책·profile·module·공급 차단·기존 blob/공유 limiter·앱 status51 tests/subtests 통과, 실제 SQL 필요3개 skip(모듈16.620s/app13.496s). 관련17개 패키지의 chain/codec/capture/progress/status race 회귀720개 통과·환경 skip34개. 최종 policy 프로그램 결합/metadata 호환성은 별도 native/SQL/Linux로 재검증했다. 전체 앱/SLO 완료를 의미하지 않는다.
- PostgreSQL16 fsync-on·pool1·DSN simple_protocol/synchronous_commit=off 요청에서도 최종55개 통과·skip0(app26.544s). 같은 DB의 read-only handle, descriptor 저장/legacy 누락/손상 거부, 재시작·reorg와 기존 call trace profile 일치 경계를 확인했다. 실제 운영 DB는 사용하지 않았다.
- immutable policy 동시 읽기를 CPU1/race/count200으로 검증(1.357s). 최종 Linux arm64/CGO0·외부 network-none/read-only 시험32개 통과·SQL 필요3개 skip. 전체 컴파일·변경 경로 vet·전체 lint0 issues·gofmt/diff를 통과했다.
- 처음 fixture 컴파일에서 import/Shutdown 서명을 맞춰 수정한 뒤 최종 검증을 통과했다. 예제 검사는 기존 feed/P2P 경로의 누락된 전이 의존성 목록으로 실패했다. 두 examples의 go.mod/go.sum을 동기화했다(각8개 indirect는 본체와 같은 버전, 기존 버전 upgrade 없음). 사용자 정의 Arc의 새 optional 정책 상속 assertion을 추가했다. 두 예제 race 및 기본 `make test-examples`를 통과했다. 본체 의존성 파일은 변경하지 않았다.
- 두 차례의 이번 UUID-owned PostgreSQL 컨테이너/named volume과 네 Linux 컨테이너·시험 binary200,026,941바이트를 정리했다. 최초 개별 inventory의 기존 Docker9개를 보존했다. 실제 peer/mount/장기 처리량·원격 debug/state 보존은 이번 증거에 포함하지 않는다.

로컬 근거: `/private/tmp/indexer-blob-capabilities-20261016/validation.json`, `audit.json`, race/SQL/Linux/예제/정적 검사 로그. 다음은 원천 source 검색/색인이다. Arc용 blob 공급 활성화를 반복할 필수 항목으로 잡지 않는다. 전체 blob transport/보존/앱 연결은 실제 지원 체인과 공급 경로를 정한 뒤 별도로 진행한다.

### 3.54 원천 블록/거래 해시 색인과 DB 전용 행 페이지 (10/16)

남은 source 검색의 첫 단계로 선택 `feed.projections: [source.lookup.v1]`를 구현했다. 독립 순차 worker가 영구 header/body/receipts를 chain profile/commitment로 재검증한 뒤 블록 해시와 ordered 거래 해시를 색인한다. immutable content-addressed trie node·입력 digest/root·자체 cursor·event를 같은 source transaction에 저장한다. 모든 commit/불확실한 commit 재시도는 uncached fresh 검사를 수행하고 기존5 HTTP/s/burst1 예산을 공유한다. root/node 기록은 `CommitLookup`만 허용하며 generic worker commit으로 우회하지 않는다. reorg는 기존 최대128높이 retained-prefix root를 선택하고, 없으면 anchor부터 영구 DB를 재생한다. 이전 fork node/output은 보존하며 global undo/outbox·base plan/Through·raw ACK/24시간 GC 조건을 변경하지 않았다.

`/indexing/source/blocks/{height}/{kind}`, `/blocks/by-hash/{hash}/{kind}`, `/transactions/{hash}/{kind}`는 읽기 전용 DB에서 versioned header/body/transaction/receipt/log 행을 반환한다. block/transaction 해시 결과는 다시 검증한 source bytes의 hash/ordinal과 대조한다. 검색 부재는 anchor 다음부터 indexedThrough까지만 적용하며 capturedThrough와 구분한다. 알려진 trie node의 누락/손상은500이고 부재로 숨기지 않는다. 페이지1..256·원래 행8MiB·모든 source 경로의 합산 동시2개/초과429·no-store를 적용했다. epoch/hash/definition을 고정하고 같은 epoch의 root 전진은 기존 블록 페이지를 유지한다. model RLP/base64와 body/log JSON codec을 명시해 chain extension을 보존한다. per-chain route와 PostgreSQL read-only/pool1을 지원한다. 새 SQL/table/의존성 없이 기존 값 바인딩 저장을 사용한다. `lookupVerify`와 `lookupCommit`의 bounded 단계 계측을 추가했다. [범위·설정·운영 비용](../SOURCE_QUERIES.md)을 따른다.

추가 검증에서 기존 두 문제를 확인하고 수정했다. Pebble AST routing 검사는 독립 CaptureUpdate의 허용 목록이 누락돼 있었고, 실제 bound block context 거부·내구성/undo 격리 테스트를 유지한 채 명시적 예외를 기록했다. PostgreSQL에서는 source/index가3→2로 낮아지는 reorg를 복구한 뒤 기존 projection이 사라진3번 tip을 먼저 fresh 조회해 종료했다. 기록된 capture reset의 epoch>1이고 old projection head가 현재 capture prefix보다 높을 때 기존 HandleReorg의 fresh hash·bootstrap/undo 범위·writer/outbox transaction으로 먼저 복구하도록 수정했다. initial epoch의 DB/과거 범위 검사는 유지한다. runtime 및 reset 직후 process restart를 재현/검증했다.

검증 결과:

- 관련28패키지의 마지막 패키지별 결과를 합치면 native race1,863 tests/subtests 통과·환경37skip이다. 처음 발생한 AST guard 실패 로그는 보존했고 Pebble 전체916개를 다시 통과했다. 최종 source/core/계측/API9패키지는136개 통과·환경32skip이다. 기존 앱 feed/혼합 체인/trace/상태18개도 통과했으며, 복구 수정 후 source·bootstrap·초기화·종료·과거 범위 거부9개를 별도로 통과했다(68.349s). 전체 app/SLO 완료를 뜻하지 않는다.
- fsync-on PostgreSQL16·simple_protocol 요청→기존 extended binding·synchronous_commit off 요청→capture on·pool1에서 최종18 tests/subtests 통과·skip0(앱54.146s). 실제6개 node/root/cursor/event write 각각의 rollback·fresh mismatch/uncertain retry·512개 trie entry·재시작/reorg·미완료 범위와 read-only API를 확인했다. 원본 윈도우 디렉터리를 잠시 옮기고 RPC 실패를 설정한 상태에서도 DB API는 추가 RPC0으로 동작했다. 두 번 재현된 lower-tip 실패 로그도 보존했다.
- 새 epoch 경쟁과 API admission/slot 반환·per-chain route를 CPU1/race/count200으로600회 통과했다. Linux/arm64 read-only repo/network none/tmpfs에서는9 tests/subtests 통과·실제 SQL2skip이며, 최종 UUID 컨테이너2개와 임시 바이너리96,365,056bytes를 제거했다. 매 SQL 실행도 전용 UUID 컨테이너/볼륨만 제거했다.
- 전체 compile·변경 영역 vet·lint0 issues·예제2모듈 테스트·gofmt/diff를 확인했다. 운영 config.yaml·go.mod/go.sum·10/12 handoff SHA와 기존 브랜치를 유지했다. full node/스냅샷·실제 peer/공개 RPC·운영 활성화·commit/push는 수행하지 않았다.

증거는 `/private/tmp/indexer-source-lookup-20261016/validation.json`과 native/SQL/Linux/CPU1·실패 재현 로그 및 자원 기록이다. 다음은 주소/topic/시간 범위 검색·전용 관계형 source 테이블/용량과 동일 입력 성능 비교, 추가 stateful worker 소유권이다. 실제 peer·외장 mount/보존·450ms/장기 SLO 측정은 계속 남아 있다.

### 3.55 주소·topic·고정 블록 범위의 원천 검색 (10/16)

선택 `source.search.v1` worker를 추가했다. 영구 encoded source를 profile/commitment로 다시 검증하고 거래 from/to 합집합·log 발행 주소/위치별 topic·높이의 ordered posting을 immutable trie에 저장한다. empty block marker·입력 digest/root·자체 연속 cursor/event·fresh를 기존 source transaction에 결합했다. generic CommitWorker로 원자성 계약을 우회하지 못하며 이전 fork root는 보존한다. 재시작/epoch reset은 기존 retained-prefix 계약을 따른다. source.lookup.v1의 저장 형식은 유지하며 공통 content-addressed node 읽기/쓰기를 private helper로 분리했다. 해시/필터 worker의 lifecycle·영구 source 검증도 공통화했다. 새 SQL/table/migration/외부 의존성은 추가하지 않았고 값 바인딩·동일5 HTTP/s/burst1·raw ACK/GC·기존 writer undo/outbox 경계를 유지했다. 선택 worker별 fresh 비용은 추가된다.

`/indexing/source/logs`와 `/transactions`는 필수 inclusive from/to 범위와 선택 log 주소/topic0..3 AND·거래 from/to 역할을 받는다. 지정하지 않은 topic 위치는 wildcard이며 zero address/topic은 실제 값이다. 거래 sender가 검증되지 않은 opaque 자료는 거부한다. 내부 호출/계약 storage/생성 주소를 주소 관련성으로 추론하지 않는다. 하나의 primary posting을 seek하고 원천 byte의 hash/ordinal/primary/secondary predicate를 다시 검증한 행만 반환한다. capture.Reader.ScanSearch의 SearchCandidatePage는 full-filter 결과가 아닌 후보임을 명시했다.

페이지는 후보256개·재검증 블록16개·행 값8MiB·행256개로 제한한다. 빈 중간 페이지도 마지막 검사 위치로 전진하고, 한도 때문에 미처리한 후보는 다음 페이지에서 잃지 않는다. source의 기존 해시 경로와 합산 동시2개/초과429를 공유한다. opaque cursor는 epoch·범위 끝 hash/root·filter digest·definition·keyset 위치를 고정한다. 같은 epoch의 높이 전진은 고정 범위를 유지하고 reorg/필터 변경은409다. 상위 범위를 미완료점으로 조용히 자르지 않고404로 반환한다. 필요한 trie node/자료 누락과 잘못된 primary binding은500이다. DB-only/read-only pool1·no-store·RPC/raw/legacy fallback 없는 계약을 따른다. searchVerify/searchCommit 단계와 요청별 scannedCandidates/decodedBlocks를 추가했다. [조회·범위·용량/예산](../SOURCE_QUERIES.md)을 따른다.

검증 결과:

- native 관련27패키지 race956 tests/subtests 통과·환경38skip. 최종 후보/커서 경계가 추가된 source/core/계측/API9패키지도 전체145개 통과·환경33skip이다. 앱의 기존 해시·mixed4·summary·trace·지정 상태·계측 회귀17개 통과(142.465s), 최종 검색/해시/cursor 기능19개 통과·실제 SQL2skip(앱37.478s)이다. 전체 app/SLO 실행 완료를 의미하지 않는다.
- fsync-on PostgreSQL16·simple_protocol 요청→기존 extended binding·synchronous_commit off 요청→capture on·pool1에서 최종28 tests/subtests 통과·skip0(앱59.795s). 실제42개 write 각각의 rollback·fresh mismatch/uncertain retry·self transfer 중복 방지·topic 위치/zero 값·empty block root·재시작/reorg와 root 전진 중 cursor를 확인했다. 원본 디렉터리가 없고 RPC가 실패하도록 설정한 동안에도 읽기 전용 DB/HTTP는 추가 RPC0으로 동작했다.
- 270개 후보의 AND 필터 fixture에서 빈256-candidate 페이지의 전진을,18개 블록에서16-block 한도를, 두3MiB log data에서8MiB row 한도가 미처리 후보를 보존하는 것을 실제 profile/저장 경로로 검증했다. 첫 test는 마지막 페이지 뒤에도 next가 있다고 잘못 기대해400이 발생했으며 올바른 done/빈 next 계약으로 수정했다. 실패 로그는 보존했다. lint의 동등한 struct conversion 권고1건도 수정했다.
- epoch 경쟁·hash/filter admission 공유·per-chain routing을 CPU1/race/count200으로600회 통과했다. Linux/arm64 read-only repo/network none/tmpfs에서17 tests/subtests 통과·실제 SQL3skip. 최종 UUID 컨테이너2개와 바이너리96,744,920bytes를 정리했고, 모든 SQL 실행도 고유 이름/라벨의 컨테이너/볼륨만 제거했다.
- 전체 compile·변경 영역 vet·lint0 issues·예제2모듈·gofmt/diff를 확인했다. 운영 config.yaml·go.mod/go.sum·10/12 handoff와 refactor/arc-node-feed 브랜치를 유지했다. 실제 Arc peer/공개 RPC·노드/스냅샷·운영 활성화·commit/push는 수행하지 않았다.

증거는 `/private/tmp/indexer-source-search-20261016/validation.json`과 native/SQL/Linux/CPU1·초기 실패·자원 로그다. 다음은 timestamp/OR·다중 주소 조건, 전용 관계형 source 테이블과 현재 trie의 용량/동일 입력 조회 성능 비교다. 실제 peer/외장 mount/보존·450ms/24시간 SLO와 추가 stateful worker 소유권도 남아 있다.

### 3.56 시간 범위·OR 검색과 같은 입력의 DB 조회/용량 비교 (10/16)

기존 선택 `source.search.v1` 저장 형식/definition을 유지하면서 inclusive `from_time`/`to_time`(uint64 Unix 초)과 주소/위치별 topic OR를 구현했다. 시간은 원천 profile/commitment로 검증한 header.Time을 판정하며 RPC나 timestamp 기반 높이 추측을 추가하지 않는다. API의 address/topicN은 쉼표 목록을 받으며 각 차원16개·합계64개를 중복 제거 전 제한하고 query는8KiB로 제한한다. 차원 사이 AND, 차원 내부 OR이고 지정 topic 위치가 없으면 불일치다. zero 주소/topic/시간과 최대 uint64를 지원한다.

조회는 선택한 차원의 최대16개 posting iterator를 높이/ordinal로 병합하고 sender/recipient가 동시에 일치하는 거래를 중복 제거한다. 전체 이력 정렬이나 Cartesian product를 만들지 않는다. 후보256·source 검증16블록·행8MiB·공유 admission2는 유지했다. 모든 primary/secondary 조건을 원천 자료와 대조하며 시간 불일치도 source/hash/ordinal 검증을 우회하지 않는다. OR 순서/중복은 새 slice에 정규화하므로 기존 단일 필터 digest/cursor가 유지되고, 실제 시간/조건 변경이나 reorg는409다. 운영 config·기존 source plan/Through·raw ACK/GC·writer undo/outbox·같은5 HTTP/s와 commit/retry fresh는 변경하지 않았다. 새 운영 SQL/table/migration은 없고 기존 바인딩을 사용한다.

검증:

- native7개 테스트 보유 패키지 race148 tests/subtests 통과·SQL 등 환경33skip, evmlookup/projection2패키지는 자체 테스트 파일 없음. 앱의 검색/재시작/reorg/후보·블록·8MiB 예산4 tests도 통과(38.490s)했다.
- fsync-on PostgreSQL16·pool1·simple_protocol 입력에서도 기존 extended binding·capture synchronous_commit 강제 계약으로31 tests/subtests 통과·test skip0(앱76.795s). 마지막 읽기 전용 pool1 OR/시간·API 경계2개를 별도로 추가 확인했다(앱15.557s). 원본 윈도우가 없고 RPC 실패가 설정된 동안 정확한 OR/time 페이지를 DB만으로 반환했다.
- epoch 경쟁·공유 admission·per-chain routing·새 OR/시간 경계를 GOMAXPROCS=1/race/count200으로800회 통과했다. Linux arm64/read-only repo/network none/tmpfs에서19 tests/subtests 통과·실제 SQL3skip. 소유 컨테이너2개와 바이너리96,900,682bytes를 정리했다. SQL 검증/측정의 UUID 컨테이너·명시적 볼륨도 모두 제거했고 초기10개 컨테이너의 이름/실행 상태를 보존했다.
- 전체 compile·변경 패키지 vet·lint0 issues·예제2모듈·gofmt/diff를 확인했다. go.mod/go.sum·운영 config.yaml·10/12 handoff SHA와 refactor/arc-node-feed를 유지했다. 전체 app/SLO·실제 Arc peer/공개 RPC·노드/스냅샷·운영 활성화·commit/push 완료를 의미하지 않는다.

선택 `TestMeasureSourceSearch`에 같은32블록/64거래/512로그·주소 OR/topic OR/시간 조건을 넣었다. 전체 검증 스캔과 색인+원천 재검증의 결과16행이 byte 단위로 같았다. 색인은64후보/4블록, 전체 스캔은512후보/32블록을 검사했고 조회 RPC는0회였다. 다른 검증 실행과 분리한 최종 warm20회·race 없는 측정의 p50/p95/p99(ms)는 Pebble 전체10.904/11.577/11.639→색인1.447/1.553/1.855, PostgreSQL 전체516.917/580.427/593.003→색인80.448/93.261/94.682였다.20표본의 p99는 최대 표본이며 Arc ingest/SLO 근거가 아니다. 초기 경합이 있던 측정은 별도로 보존했다.

같은64개 후보의 높이/ordinal/hash를 대조한 후보 발견 단계 p50은 Pebble trie0.309ms·PostgreSQL KV trie37.634ms·임시 SQL B-tree0.618ms였다. SQL 비교는 세션 임시 테이블+주소 index/PK만 사용하며 epoch/fresh/원자적 cursor/event·reorg 복구·원천 재검증을 구현한 대체 저장소가 아니다. 두 DB trie는 과거 root 포함1,974nodes·논리 key/value357,648bytes였고 임시 SQL table+index의 물리 크기는204,800bytes다. 기능/보존/overhead가 달라 동등한 디스크 효율 비교나 하루 용량 외삽으로 쓰지 않는다. [측정 방법과 한계](../SOURCE_QUERIES.md#고정-입력-비교-2026-10-16)를 따른다.

증거는 `/private/tmp/indexer-source-search-or-20261016/validation.json`·`measurements.json`과 native/SQL/Linux/CPU1·자원 로그다. 다음은 PostgreSQL의 node별 CaptureReader.Get 왕복을 bounded batch/요청 단위 hash 검증 캐시로 줄이고 같은 입력으로 재측정하는 것이다. 전용 관계형 source 색인은 필요하면 현재 epoch/커서/내구성 계약부터 갖춘다. worker 소유권, 실제 peer/mount·보존·450ms/24시간 SLO는 남아 있다.

### 3.57 PostgreSQL trie 스냅샷 캐시·bounded child batch 읽기 (10/16~17)

읽기 전용 ScanSearch의 CaptureView 내부에서 immutable node cache를 공유했다. code-owned prefix/hash key·실제 Keccak 대조·1MiB node 한도·호출자 소유 byte 복사를 유지한다. cache의 계산된 key/value/entry 비용4MiB·4,096항목을 제한하고 한도 도달 시 검증된 단건 읽기를 계속한다. 조회 종료 후 cache를 보관하지 않고 metadata/canonical cursor/epoch/definition/fresh를 cache하지 않는다. lookup/snapshot point 조회는 cache만 적용하고 형제 경로 prefetch를 피한다. writer CommitSearch/CommitLookup·fresh 및 자체 root/cursor/event의 원자성은 바꾸지 않았다.

선택 port.CaptureBatchReader.GetMany를 PostgreSQL capture reader에 추가했다. 같은 chain/snapshot/transaction, bound block transaction SHARE fence, read-only/pool1 및 바인딩을 유지한다. code-owned SELECT/CASE와 chain=$1·key=ANY($2::bytea[])·value limit=$3를 사용하며 새 table/migration/설정/의존성은 없다. 최대32입력 key(중복 제거 전)·value1MiB·결과 key/value4MiB를 강제하고, 서버 CASE가 과대 value 전송을 막는다. 없는 key는 생략하고 빈 값은 보존한다. 과대/오류는 partial success 없이 callback을 실패시키며 후속 접근도 거부한다. range trie는 검증한 branch/extension의 직접 child hash만 prefetch한다. leaf/branch terminal 값이32바이트여도 child로 해석하지 않는다. 필요하지 않은 누락/손상 sibling을 결과에 섞지 않고 필요한 경로는 반드시 conflict로 실패한다. batch I/O 오류를 단건/RPC fallback으로 숨기지 않는다. Get-only/Pebble도 같은 검증/cache로 동작한다.

검증:

- native8패키지 전체 race1,100 tests/subtests 통과·환경33skip. 기존 의도적 broken-store 포트 검출까지 포함해 storage225.567s에 통과했다. 앱 검색·해시·재시작/reorg·raw 없는 DB 조회·후보/블록/byte budget6개도 통과(57.167s)했다.
- 실제 PostgreSQL16 fsync-on, simple_protocol 요청→기존 extended binding, pool1/read-only/bound transaction, key SQL punctuation/NUL·genesis 격리·같은 snapshot 중 외부 update와 새 snapshot 재조회·escaped reader 거부·빈 값/중복·개수/길이/value/합계/cancel·sticky rollback 및 기존 검색을51 tests/subtests로 통과·test skip0(앱63.114s)했다.
- 독립 cache view의 동시 읽기·epoch 경쟁·공유 admission을 GOMAXPROCS=1/race/count200으로600회 통과했다. Linux arm64/read-only repo/network none/tmpfs에서23 tests/subtests 통과·SQL3skip, 소유 컨테이너2개/바이너리96,931,296bytes를 정리했다.
- 전체 compile·변경 vet·최종 lint0 issues·예제2모듈·gofmt/diff를 확인했다. 첫 lint의 임베딩 선택자 QF1008 한 건은 동등한 Get 호출로 수정했다. 운영 config.yaml·go.mod/go.sum·10/12 handoff SHA·refactor/arc-node-feed를 유지했다. 공개 RPC5 HTTP/s·fresh·원천/undo/outbox·raw ACK/GC 경계는 동일하다.

변경 전 app test 바이너리를 소스 수정 전에 보관하고, 최종 바이너리와 같은32블록/64거래/512로그 fixture를 race 없이 순차 측정했다. SQL은 같은 임시 서버의 개별 schema, 각 경로 warm20표본을 사용했다. 두 버전 모두 전체 검증 스캔과 결과16행이 byte 단위로 일치했고 후보64/검증4블록·과거 trie1,974nodes/357,648bytes·조회 RPC0이 같았다. PostgreSQL 검증 검색 p50/p95/p99(ms)는88.159/226.101/527.756→58.996/70.853/95.621, 후보 발견43.621/54.459/143.850→15.680/19.783/23.739였다. Pebble 검증 검색은1.490/1.879/2.093→1.344/1.776/2.174였다.20표본 p99는 최대값이며 운영 tail/SLO 추정치가 아니다.

최종 코드에서 cache를 유지하고 batch만 숨긴 진단 control은 PostgreSQL node 단건88회, batch 사용 시 단건5+배치19=24회였다. 전체 CaptureReader 읽기는175단건→92단건+19배치=111회, batch102keys/16,285bytes였다. 이는 전체 SQL 문장 수가 아니며 counter wrapper는 latency 표본 밖에만 적용했다. [방법·한도·수치](../SOURCE_QUERIES.md#postgresql-trie-읽기-최적화)를 따른다. 비교용 SQL 후보 table은 세션 전용 프로토타입이고 운영 source 계약을 대체하지 않는다.

검증/비교의 UUID PostgreSQL 컨테이너·명시적 볼륨을 제거했고 변경 전후 바이너리133,525,364bytes도 정리했다. 이전10개 Docker 컨테이너는 이름/실행 상태를 보존했다. 증거는 `/private/tmp/indexer-trie-batch-20261017/validation.json`·`measurements.json`과 회귀/SQL/Linux/CPU1/비교/자원 기록이다. 다음은 남은 metadata/영구 원천 입력의 반복 CaptureView/SQL을 일관된 읽기 범위로 줄이는 작업이다. 큰 payload/외부 객체·epoch/pool1·손상/누락 계약과 source node용1MiB 한도를 구분한다. 실제 Arc peer/mount·보존·450ms/24시간 SLO, worker 소유권은 남아 있으며 전체 app/SLO·운영 활성화·commit/push를 수행한 결과가 아니다.

### 3.58 canonical 영구 입력의 단일 snapshot·작은 metadata 배치 (10/17)

`capture.Store`/`Reader.ReadProjectionInput`을 추가했다. 한 CaptureView에서 state/epoch·정확한 canonical pointer/identity·plan/manifest를 확인한 뒤 inline payload와 영구 객체 binding/bytes를 읽는다. 선택 GetMany가 있으면 작은 metadata3개만 같은 snapshot의 바인딩된 query로 읽는다. Get-only backend에도 metadata1MiB를 적용하며 wrapped missing과 joined I/O/cancel 원인을 구분한다. 큰 body/receipt를 node용1MiB batch에 넣거나 한도를 늘리지 않았다. public ValidateProjection도 작은 metadata batch를 사용하고 writer 내부 checkProjection·fresh/commit 쓰기 경계는 유지했다.

materialization은 dataset별8MiB·합계64MiB·기존11종 한도를 둔다. 외부 객체는 정확한 area/store ID·binding/retirement를 읽고 descriptor/length/digest·volume guard를 확인하는 Copy를 한 번 수행한다. 중첩 Store.Read/CopyPayload 반복 없이 호출자 소유 Payloads로 반환하며 오류면 partial Input을 노출하지 않는다. 기존 Read의 역사 fork/ref와 더 큰 객체 CopyPayload 스트리밍은 그대로다. 객체 binding 검사를 private helper로 분리했으며 기존 commit/GC/retirement 계약은 바꾸지 않았다.

`evmdata.ReadCaptured`는 새 optional ProjectionInputReader를 쓰되 기존 필수 CapturedReader interface/레거시 경로를 유지한다. 선택 경로 실패 시 fallback하지 않으며 profile hash/sender/root/gas/bloom/parent 검증 뒤 독립적인 최신 epoch/canonical 검사를 유지한다. snapshot 중 reset이 일어나도 내부 bytes는 한 view로 읽되 후속 epoch 검사는 실패한다. retained canonical input의 원래 generation도 보존한다. 적용 범위는 source hash/search API와 해당 helper를 쓰는 선택 worker이며 facts Source.BlockWithReceipts의 직접 materialization은 아직 별도다. 그 writer의 public ValidateProjection은 metadata 배치를 이미 사용한다.

검증:

- native7패키지 race194 tests/subtests 통과·환경36skip, 앱의 검색/해시/managed feed·혼합/재시작/reorg·소유권/부모·크기 예산30개도 통과(143.246s)했다. 전체 app/SLO 완료를 뜻하지 않는다.
- 실제 PostgreSQL16 fsync-on·simple_protocol 요청→기존 extended binding·synchronous_commit off 요청→capture on·pool1/read-only/bound transaction 계약으로77 tests/subtests 통과·test skip0(앱75.559s)했다. 2MiB inline payload는 작은 batch 밖으로 읽고, 원천 reads가 한 snapshot인지·읽는 중 reset 뒤 사후 검사·retained generation·외부 객체의 실제 손상/누락·binding/retired/area 없음·8MiB 초과 및64MiB 합계·metadata1MiB·byte 소유권·joined 오류와 레거시 대형 스트리밍을 확인했다.
- 입력 읽기/검색 epoch 경쟁과 독립 node cache view를 GOMAXPROCS=1/race/count200으로600회 통과했다. Linux arm64/read-only repo/network none/tmpfs에서32 tests/subtests 통과·SQL6skip. 소유 컨테이너2개와 바이너리97,089,444bytes를 정리했다.
- 전체 compile·변경 vet·lint0 issues·예제2모듈·gofmt/diff를 확인했다. 운영 config.yaml·go.mod/go.sum·10/12 handoff SHA와 refactor/arc-node-feed를 유지했다. 공개 RPC5 HTTP/s·fresh·undo/outbox·raw ACK/24시간 GC·table/migration·설정은 변경하지 않았다.

변경 전후 바이너리와 같은32블록/64거래/512로그 입력을 다른 검증 종료 뒤 순차 비교했다. SQL은 같은 임시 서버의 개별 schema를 사용했다. 양쪽 모두 전체 검증 스캔과 같은16행·64후보/4검증 블록·과거 trie1,974nodes/357,648bytes·조회 RPC0을 유지했다.20 warm 표본의 PostgreSQL 검증 검색 p50/p95/p99(ms)는74.502/96.927/152.403→46.108/85.307/94.592, 검증 전체 스캔426.999/604.736/694.196→278.567/440.869/573.135였다. Pebble 검증 검색은1.431/1.846/1.999→1.469/2.191/3.035로 이번 작은 표본에서 소폭 느렸다. 운영 tail/SLO나 Arc ingest 개선을 주장하지 않는다.

시간 표본 밖의 counter로 optimized query는92단건+19배치=111회→57단건+28배치=85회 CaptureReader 읽기를 확인했다. 새28배치는 node19+metadata9개이며 node 자체는 기존5단건+19배치=24회와 같다. 최종 View16회·129batch keys/44,906bytes다. 변경 전 바이너리에는 Views counter가 없으며 모든 SQL 문장/transaction/ReadIndexingPage 수로 해석하지 않는다. [방법·범위](../SOURCE_QUERIES.md#canonical-원천-입력의-단일-스냅샷-읽기)를 따른다.

검증/비교 UUID PostgreSQL 컨테이너·명시적 볼륨·변경 전후 바이너리133,595,652bytes도 정리하고 기존 Docker10개를 보존했다. 증거는 `/private/tmp/indexer-input-read-20261017/validation.json`·`measurements.json`과 DB/객체/동시성/Linux/비교/자원 로그다. 다음은 facts Source의 직접 materialization을 연결하고 coverage 오류·큰 객체·부모·era/replay·commit fresh/처리 ACK 경계를 검증하는 작업이다. 실제 peer/mount·보존·450ms/24시간 SLO와 추가 worker 소유권은 남는다. 실제 공개 Arc·노드/스냅샷·운영 활성화·commit/push는 수행하지 않았다.

### 3.59 실제 facts writer의 단일 snapshot 소비·전후 측정 (10/17)

`facts.Source.BlockWithReceipts`의 직접 `Read`/객체별 중첩 `CopyPayload`를 `ReadProjectionInput(epoch, identity)`으로 연결했다. canonical state/pointer/manifest와 inline/정확한 영구 객체 bytes를 한 snapshot에서 읽는다. 기존 codec/commitment evidence·profile hash/sender/transaction/receipt root/gas/bloom/parent 검증·독립적인 디코딩 뒤 epoch 검사·writer 커밋 직전 uncached fresh·durable commit 뒤 ACK를 유지했다. 일반 streaming/history API는 그대로다. 큰 영구 descriptor의 decoder 한도는 `ErrProjectionInputLimit`와 `port.ErrInvalidData`로 구분하고 facts는 기존 `ErrCoverage`를 감싼다. 객체 손상/누락/바인딩 오류나 다른 invalid data를 일괄 coverage로 바꾸지 않는다. 잘못된 descriptor는 크기 오류와 구분한다. RPC/window fallback은 없다.

변경 전 source SHA를 확인한 Go overlay와 같은 opt-in 측정 코드로 전후 바이너리를 만들었다. Arc profile의 고정32블록·64거래·512로그, inline/영구 객체3종을 각각 사용했다. warm 뒤64회의 DB-only 읽기와 실제 fetch writer32블록을 분리했다. address.index/token.transfers·동일 source/projection DB·2 fetch workers·기존 prepared pipeline/undo/outbox·fresh/ACK를 실행했다. PostgreSQL16 fsync-on·pool1, simple_protocol/sync_commit off 요청에 대한 기존 extended binding/동기 커밋 강제도 유지했다. 다른 테스트를 끝낸 뒤 Pebble 전후→같은 SQL 서버의 개별 schema 전후를 순차 측정했다. 첫 비교 실행기는 모델의 일반 JSON 직렬화를 지원하지 않아 중단했고, 정식 EncodeBlock/EncodeReceipt codec으로 수정한 뒤 전후를 모두 다시 측정했다.

DB-only p50/p95/p99(ms)는 PostgreSQL inline6.993/15.418/19.568→5.363/6.090/7.280, 객체19.658/24.977/53.609→6.521/7.129/8.336이었다. Pebble inline0.183/0.564/5.482→0.212/1.892/7.171, 객체3.382/4.935/5.472→1.247/5.810/21.729였다. 객체 p50은 SQL약66.8%/Pebble약63.1% 줄었으나 Pebble inline과 객체 tail 악화도 관측했다.64표본 p99는 최대 표본이고 장기 tail/SLO를 뜻하지 않는다.

실제 writer의 source 읽기 p50(ms)은 SQL inline192.294→190.009, 객체204.636→189.795, Pebble inline0.313→0.354, 객체6.087→1.683이었다. SQL pool1에서는 fresh 검사 동안의 block transaction/connection 대기가 병렬 읽기에 포함된다. sourceValidate p50도 약162~170ms다. 최종 durable ACK 처리량(blocks/s)은 SQL inline5.097→5.111, 객체5.085→5.108, Pebble inline5.014→5.070, 객체5.134→5.125로 비슷했다. byte 소비 개선만으로 전체 처리량이 빨라졌다고 주장하지 않는다. fresh 검사는 transaction 안에 그대로 둔다.

SQL 객체 DB-only CaptureReader 호출은 블록당23 Get+1 batch=24→6 Get+2 batch=8, CaptureView7→4다. 실제 writer 전체 source 읽기는37→21회/블록, View17→14다. Pebble 객체는26→12 Get, inline은11→12 Get으로 늘었다. 카운터는 CaptureView Get/GetMany 진단이며 모든 SQL/쓰기/ACK 내부 호출의 합계가 아니다. 모든8개 비교 실행은 입력/저장 block·receipt/생성 시각을 뺀608 outbox event digest가 같고32개 ACK identity를 확인했다. fresh canonical 물리 HTTP32회·validator32회·ACK32회·오류0을 유지했다. transport 시각으로5 HTTP/s/burst1 envelope를 확인했다(20ms scheduler 오차 허용). 초기 token1개를 포함한 짧은32블록 평균이5를 조금 넘는 것은 지속 한도를 바꾼 것이 아니다. DB-only 읽기 RPC0이다. [측정 범위·전체 표](../SOURCE_FACTS.md#실제-facts-writer-전후-측정-2026-10-17)를 따른다.

검증:

- native facts/capture/evmdata/era/replay race121 tests/subtests 통과·환경42skip, 추가 실제 객체/fresh/한도/손상·누락/디코딩 중 reset 경계8통과·SQL6skip. fetch/rpcpool race185통과·opt-in1skip. 앱 managed feed/search/hash/재시작/reorg30통과(140.661s)로 전체 app/SLO 완료를 뜻하지 않는다.
- 실제 fsync-on PostgreSQL9패키지112 tests/subtests 통과·test skip0(앱68.603s). facts writer의3종 영구 객체·fresh 불일치 롤백/ACK/outbox0·큰 객체 coverage 구분·손상/누락 오류·후속 epoch barrier·기존 실패/재시도/reorg/readonly/bound pool1/SQL injection 값 바인딩을 확인했다.
- 단일 CPU/race에서 facts 읽기-reset과 projection input epoch 경쟁을 각각200회, 합계400회 통과했다. Linux arm64/network none/read-only repo/tmpfs에서51통과·SQL16skip. 소유한 컨테이너2개·바이너리75,235,805bytes를 정리했다.
- 전체 compile/vet·측정 codec 수정 뒤 facts vet/전체 lint0 issues·예제2모듈·gofmt/diff를 확인했다. config.yaml·go.mod/go.sum·10/12 handoff SHA와 refactor/arc-node-feed를 유지했다. RPC5 HTTP/s·fresh·raw ACK/24시간 GC·table/migration·운영 설정은 변경하지 않았다.

검증/비교용 UUID PostgreSQL 컨테이너·명시적 볼륨과 최종 비교 바이너리86,958,852bytes를 정리하고 기존 Docker10개 이름/상태를 보존했다. 비교용 PostgreSQL 시작이 지연됐지만 실행/측정/정리는 완료했다. 증거는 `/private/tmp/indexer-facts-writer-20261017/validation.json`·`measurements.json`·비교/소유권/검증 로그다. 실제 Arc enode/P2P 수신·공개 RPC 지연·외장 mount·보존/450ms/24시간 SLO, 추가 worker 소유권·필요시 durable 관계형 source 색인은 남는다. 실제 공개 Arc/노드/스냅샷/운영 활성화/commit/push는 수행하지 않았다.

### 3.60 PostgreSQL 연결 예산으로 facts 읽기/writer 경합 완화 (10/17)

앞 측정의 단일 연결은 facts fixture가 `MaxConns:1`로 강제한 값이었다. 운영 adapter/앱은 이미 기존 `database.postgres.max_conns`와 `INDEXER_DB_POSTGRES_MAX_CONNS`를 전달하므로 그 bounded pool을 활용했다. 사실상 필요한 것은 여유 연결을 가진 구성과 동일 입력의 확인이었다. 테스트 opener에 명시적 예산1/2/4를 전달하도록 확장하고 기본1은 유지했다. 측정에 `INDEXER_MEASURE_FACTS_MAX_CONNS`와 실제 예산을 기록한다. Source/SDK/store interface·driver 기본 동작·table/migration·RPC 경계는 바꾸지 않았다. config.example.yaml의 PostgreSQL 예제는 측정 뒤2개로 정했다.0은 DSN/pgxpool 기본값이고 명시적1을 자동 확장하지 않는다. 운영 config.yaml은 유지했다.

같은 Store handle/같은 pool 안에서 writer는 자기 연결과 bound transaction/SHARE capture fence를 유지하고 acquisition은 여유 연결의 read-only snapshot으로 진행한다. 커밋 검증을 별도 Store/읽기 transaction으로 옮기거나 fresh를 앞당기지 않았다. 새 gated `TestFactsReadsDuringFresh`는 실제 writer를 커밋 직전 HashAt 안에 정지시켜1개 연결의 읽기 대기/취소,2·4개의 독립 facts 읽기, 미커밋 projection 행 비노출, reset 대기/epoch 유지,4개에서 fresh 불일치 시 block/outbox/ACK0을 확인했다. 실패 경로도 gate 해제/취소/join 뒤 DB를 닫는다.

같은 바이너리·원천 저장이 끝난 Arc profile32블록/64거래/512로그·inline/객체3종·fsync-on PostgreSQL16 한 서버에서 pool별3회씩 순차 비교했다. 순서는 `[1,4,2]→[4,2,1]→[2,1,4]`다. 각 실행은 별도 schema,64 warm DB-only 읽기 표본과 실제 fetch writer32블록·workers2·address.index/token.transfers·원자적 commit/undo/outbox·기존 shared5 HTTP/s/burst1/fresh/ACK를 유지했다. 테스트와 반복 검사가 끝난 뒤 측정했다. 각 실행의 percentile/rate를 구한 뒤3개 값의 중앙값을 비교하며 표본을 합친 percentile은 아니다.

| 입력 | 연결 상한 | writer 읽기 p50/p95/p99 ms 중앙값 | durable ACK 블록/s 중앙값 |
|---|---:|---:|---:|
| inline | 1 | 191.633 / 200.316 / 218.321 | 5.105 |
| inline | 2 | 7.330 / 11.140 / 12.321 | 5.116 |
| inline | 4 | 6.790 / 22.248 / 89.711 | 4.920 |
| 객체 | 1 | 192.529 / 203.953 / 211.889 | 5.104 |
| 객체 | 2 | 9.193 / 14.927 / 15.387 | 5.105 |
| 객체 | 4 | 8.244 / 17.124 / 27.834 | 5.110 |

연결1→2의 객체 writer 읽기 p50은약95.2%, inline은약96.2% 감소했다. 객체 DB-only p50은6.862→6.450ms로 비슷하고 입력/정규 결과·CaptureView/Get/GetMany 수는 동일했다. 줄어든 값은 주로 writer의 connection 대기다.4개도 개선했지만2개 대비 처리량 증가는 거의 없고 inline tail/처리량 편차가 더 커 현재 후처리의 시작 예산은2로 정했다.4개가 일반적으로 더 빠르거나 느리다는 결론은 아니다. 실제 수집/API/선택 worker 동시 작업량에 따라 필요한 예산은 별도 검증한다. sourceValidate p50은 객체 약172~178ms이며 최종 객체 처리량은약5.1블록/s로 비슷했다. 작은32블록/64읽기·3회 값은 실제 Arc 수신/장기 tail/450ms/24시간 운영 SLO를 뜻하지 않는다.

모든18개 layout 측정에서 입력/저장 block·receipt/생성 시각을 제외한608 outbox event digest·32 ACK identity가 같았다. writer 구간별 fresh 물리 HTTP32회·sourceValidate32회·sourceACK32회·오류0·5 HTTP/s/burst1 envelope를 확인했고 DB-only 읽기 RPC0이다. Source 생성의 genesis/anchor fresh2회는 writer 구간 밖이다. [전체 방법/표](../SOURCE_FACTS.md#postgresql-연결-경합-비교-2026-10-17)를 따른다.

검증은 native facts/config race66 tests/subtests 통과·SQL14skip, fetch/rpcpool185통과·opt-in1skip, 실제 PostgreSQL9패키지119통과·test skip0(앱72.385s)이었다. 실제4연결 fresh gate/읽기/reset 경합은 단일 Go CPU/race/count200으로200회(부모/자식400 test 기록) 통과했다. 기존 bound transaction pool1·fsync-on/synchronous commit·extended parameter binding/SQL injection 값·객체/coverage/epoch/reorg/ACK·검색 앱 회귀도 유지했다. 전체 compile/vet/lint0 issues·예제2모듈·gofmt/diff를 확인했다. 전체 app/SLO나 이번 단계의 별도 Linux client suite를 실행한 것으로 표시하지 않는다. SQL 서버는 Docker Linux PostgreSQL16이다.

같은 UUID 소유 컨테이너/명시적 볼륨으로 검증과 측정을 마친 뒤 정리했고, 측정 바이너리43,498,642bytes도 제거했다. 기존 Docker10개 이름/상태·go.mod/go.sum/config.yaml/10월12일 handoff SHA와 refactor/arc-node-feed를 보존했다. 증거는 `/private/tmp/indexer-facts-pool-20261017/validation.json`·`measurements.json`·SQL/CPU1/회귀/소유권 로그다. 실제 enode/mount·연속 수집/외장 저장·운영 RPC 지연/450ms/24시간 보존·추가 worker 소유권·필요시 durable 관계형 source 색인은 남는다. 실제 공개 Arc/노드/스냅샷/운영 활성화/commit/push는 수행하지 않았다.

### 3.61 실제 수집·writer·HTTP API 혼합 부하의 연결2/4 비교 (10/17)

`TestMeasureManagedFeedPoolLoad` opt-in fixture를 추가해 기존 managed App의 ETH68/RLPx 수신·raw fsync·capture DB transaction과 facts writer, 같은 Store를 조회하는 production GraphQL HTTP router를 동시에 실행했다. 별도 reader pool/Store/API RPC proxy·lookup/search worker를 추가하지 않았다. 기본2 warm 뒤32블록/64거래/512로그를500ms마다 공개한다(전체34블록/68거래/544로그). workers2/chunk1/acquisition_queue off·poll50ms/control1s·base plan·state feature off를 양쪽에서 고정했다. 공개는 메모리 peer oracle/RPC의 가시성을 함께 올리며 실제 Arc 노드/합의 실행이 아니다.

API 두 client는 단일 블록과 최근8블록 목록을 번갈아100ms ticker로 조회한다. 최대20 RPS의 bounded closed-loop이고 요청이 길면 다음 launch 기회가 합쳐진다. 실제 완료율과 오류/timeout 포함 지연을 함께 보고하며 무한 open-loop20 RPS로 표시하지 않는다. 공개16초와 최종 capture/ACK까지 drain에서 API 부하를 유지하고 양쪽 cohort를 구분했다. API 응답의 높이/hash/거래 수를 검사한다. 진행 관찰은20ms 메모리 snapshot만 쓰며 추가 DB/RPC를 보내지 않는다. source 완료는 기존 ledger 시각이고 ACK는 polling 상한 관측이다. API 시작 시점과 완료 시점을 구분하도록 지표를 보완하고 최종 코드의4블록 race smoke를2/4연결 모두 다시 확인했다. 초기 lint의 response.Body.Close 오류 처리 누락은 수정 후 최종 lint0을 확인했다.

같은 바이너리/입력/소유 PostgreSQL16 fsync-on 서버의 별도 schema에서 `[2,4]→[4,2]→[2,4]` 순서로3회씩 측정했다. 다른 테스트는 끝낸 뒤 실행했다. 다음은 각 실행 percentile/rate를 먼저 구한 뒤3개 값의 중앙값이며 pooled percentile이 아니다.

| 지표 | 연결2 | 연결4 |
|---|---:|---:|
| 최근8블록 API p50/p95/p99 ms | 3.449 / 191.973 / 210.169 | 2.314 / 9.457 / 21.038 |
| 단일 블록 API p95 ms | 187.042 | 4.453 |
| 공개 구간 실제 API 완료/초 | 19.062 | 19.875 |
| 32블록 ACK 완료까지 초 | 17.200 | 17.621 |
| 공개 종료 뒤 drain 초 | 1.199 | 1.620 |
| 공개 속도 포함 평균 블록/s | 1.860 | 1.816 |
| 공개→capture p50/p95 ms | 715.860 / 1185.569 | 808.842 / 1411.778 |
| 공개→ACK 관측 p50/p95 ms | 1119.622 / 1619.802 | 1219.944 / 1820.002 |
| 최대 전체 적체 높이 중앙값 | 4 | 4 |

4개는 API 최근8블록 p95를약95.1% 줄였으나 수집/writer 처리량 개선은 관측하지 못했고 이번 실행의 전체 완료는 소폭 느렸다. 최대 수신→capture3블록/capture→writer2블록 적체는 같았다. API가 실제로 더 많은 요청을 완료한 상태이며 이3회 비교만으로 수집 지연 차이의 일반 원인을 확정하지 않는다. 원천 완료 뒤 후처리만 하는 경우2개 시작 권장은 유지하고, 동일 DB의 실제 수집/writer/API에서 API tail이 중요하면4개를 시작점으로 권장한다. backlog-only5.1블록/s와 직접 비교하거나4개가 수집을 가속했다고 표시하지 않는다. [전체 조건/표](../SOURCE_FACTS.md#수집writerhttp-api-동시-부하-비교-2026-10-17)를 따른다.

6회 모두 API 오류0(연결2 총988/연결4 총1040 측정 cohort), 최종 block/receipt·생성 시각을 제외한646 outbox event digest가 같고34 canonical input/처리 ACK identity를 확인했다. 진행 시점에 따라 API 최신 목록이 달라지므로 response bytes 전체가 같은 것은 아니다. captureCommit34회·writer sourceValidate34회·sourceACK34회·오류0이며 기존 shared5 HTTP/s/burst1·커밋 직전 fresh·SHARE fence·durable commit 뒤 ACK를 유지했다. timed HTTP 도착 수는 양쪽 중앙값73회이고 기존 envelope 검사도 통과했다. 초기 RPC/Source 확인은 timed 구간 밖이며, 중단 뒤 같은 API 두 조회는 RPC0임을 확인했다. 수신·capture·ACK 관측은 원자적 snapshot이 아니며 API 완료율/latency cohort 구간도 구분했다.

검증은 native feedtelemetry/fetch/rpcpool/GraphQL race439 tests/subtests 통과·opt-in2skip, 실제 PostgreSQL9패키지119통과·test skip0(앱62.606s), 현재 mixed fixture의4블록 race2/4 연결 각각 통과(9.406/9.316s)였다. 전체 compile/vet·최종 app vet/전체 lint0 issues·예제2모듈·gofmt/diff를 확인했다. 전체 app/SLO, 신규 helper200회, 이번 단계의 별도 Linux client suite를 실행한 것으로 표시하지 않는다. 같은 fixture의 race smoke 이전 계측 수정본 로그도 보존했다.

검증/비교용 UUID 컨테이너·명시적 볼륨과 바이너리66,884,866bytes를 제거하고 기존 Docker10개 이름/상태를 유지했다. go.mod/go.sum/config.yaml/config.example.yaml/10월12일 handoff SHA와 refactor/arc-node-feed를 유지했다. production driver/Source/SDK/writer/RPC interface를 바꾸지 않았다. 증거는 `/private/tmp/indexer-mixed-pool-20261017/validation.json`·`measurements.json`·SQL/race/비교/소유권 로그다. 실제 Arc enode·외장 mount·공개 RPC/450ms/24시간 보존·추가 worker 소유권/관계형 source 색인은 남는다. 실제 공개 RPC/노드/스냅샷/운영 활성화/commit/push는 수행하지 않았다.

### 3.62 연결4 수집·writer canonical 후보 병목 개선 (10/17)

사용자가 요청한 pool4 기준 수집/writer 병목을 기존 ETH68/window/capture·writer·GraphQL 부하로 분석했다. rawQueue p50 482.958ms, writer ACK 212.874ms 대비 DB commit 1.926ms/RPC HTTP 0.483ms로, 선행 canonical 조회·미래 not-found1초 candidate·shared limiter와 capture/writer/ACK row fence의 대기를 확인했다. 각 lock/limiter의 기여 비율을 완전히 분해한 측정은 아니다.

`window.Candidate`로 완성된 단일 raw hash만 참조로 반환하며 범위 요약/한 bounded segment index씩 읽는다. 전체 retained 맵/payload cache를 만들지 않는다. hint를 기존 verifier에 전달하고 기존 capture generation transaction 내부 fresh로 최종 판단한다. 미완성/미래 입력에는 미리 RPC를 보내지 않는다. partial fork/거절된 hint는 canonical RPC/Lookup으로 전환하고, 알림 없이 durable metadata를 재확인한다. callback/peer 공개는 성공 후에만 수행한다. 첫 SQL/App reorg 시험에서 낮아진 head보다 높은 retained orphan의 source.ErrNotFound가 중단을 일으켜, 해당 transaction rollback 후 canonical 선택 대기로 보완했다. wrapped not-found만 구분하며 joined I/O/rollback/취소 오류를 숨기지 않는다. 초기 실패 시 assertion cleanup에서 발생한 App panic도 로그에 남겼고 최종 App/SQL 같은 경로는 통과했다. SQL 바인딩·writer fresh/SHARE/epoch/ACK·GC/finality/undo/outbox·5 HTTP/s/burst1은 그대로다.

동일34블록/68거래/544로그와 pool4/동시 API2 client(목표 최대20/s)를 고정하고 기존500ms streaming 및 새1ms backlog fixture를 각각3쌍씩 비교했다. 새 환경 변수는 opt-in 측정 전용이며 운영 생성률을 뜻하지 않는다. 검증/정적 검사/최종CPU1/race200을 끝낸 뒤 비교했다. 기준 바이너리는 최초 SQL 실패 때 cleanup으로 정리돼, 변경 전 resolved.go만 Go overlay로 복원하고 동일 최종 측정 코드로 재빌드했다. 원본 소스/바이너리 SHA와 이 경위를 보존했다.

각 실행 percentile/rate의3회 중앙값으로 streaming 공개→capture p50/p95 807.135/1414.645→178.940/346.977ms, 공개→ACK p50 1119.945→380.953ms, 최종 완료 17.420→16.380s였다. rawQueue p50 482.958→9.434ms, sourceACK 212.874→8.584ms, 최대 total gap 중앙값4→2블록이었다. 누적 부하 완료 14.021→13.020s, 처리량 2.282→2.458블록/s(+7.7%)였다. 논리 RPC streaming 118→81/backlog 112→77, 물리 HTTP 72→66/67→65로 구분한다.

지연이 모두 개선된 것은 아니다. backlog capture→writer 최대 gap 중앙값2→16, 공개→ACK p50 7603.946→9643.989ms였고 API tail도 일부 증가했다. streaming 최근8블록 API p95/p99 3.797/7.887→7.255/65.466ms, backlog API p95 4.024→9.071ms였다. 최종 완료·원천 확보와 중간 후처리 지연의 차이를 보고하고 다음 병목은 필수 fresh/세대 fence를 유지한 lock 점유·공정성으로 남긴다. [전체 표/조건](../SOURCE_FACTS.md#연결4-수집writer-병목-개선-전후-2026-10-17)을 따른다.

12회 모두 final34block/68receipt·646 normalized outbox digest·34 canonical input/ACK가 같고 captureCommit/sourceValidate/sourceACK 각34회·오류0이었다. API 전체 측정 cohort 3641건 오류0, DB-only API 추가 RPC0과5 HTTP/s envelope를 확인했다. ACK는20ms nominal polling 상한, 단계 계측34/warm 포함과 공개→완료32를 구분했다. native 관련8패키지 race 및 최종3패키지 재검증, App60통과/선택2skip, PostgreSQL10패키지129통과/skip0, 최종CPU1/race200회1000 tests/subtests, streaming/backlog4블록 race, 전체 compile/vet·최종 관련 vet/전체 lint0·예제2모듈을 확인했다. 전체 app/SLO suite를 실행한 것으로 표시하지 않는다.

UUID 소유 PostgreSQL 컨테이너·명시적 볼륨과 두 측정 바이너리를 정리하고 기존 Docker10개 이름/상태를 유지했다. 운영/예제 YAML·go.mod/go.sum·handoff·writer/facts/SQL/RPC core SHA와 refactor/arc-node-feed를 유지했다. 증거는 `/private/tmp/indexer-capture-latency-20261017/measurements.json`·`validation.json`·초기/최종 검증/비교/소유권 로그다. 실제 Arc peer/공개 RPC 지연·외장 mount·450ms/24시간 SLO, 추가 worker 예산·관계형 source 색인은 남는다. 운영 활성화·공개 RPC/노드/스냅샷·commit/push는 수행하지 않았다.

### 3.63 연결4 잠금 분리와 ACK 공정성 (10/17)

사용자 요청에 따라 연결4·shared5 HTTP/s/burst1·capture/writer 커밋 직전 fresh를 유지하고 PostgreSQL chain row lock 범위를 나눴다. optional CaptureAppender는 NO KEY UPDATE로 append끼리 직렬화하고 공개 prefix 검증/immutable CaptureReceipter는 KEY SHARE를 사용한다. Reset/삭제/finality/기타 CaptureUpdate는 FOR UPDATE이며, arbitrary callback의 전체 snapshot 계약을 유지하려고 일반 bound CaptureView도 FOR UPDATE로 보호한다. unbound 조회는 기존 RepeatableRead/read-only, 같은 Store/transaction 재사용과 pool1 지원도 유지한다. 별도 pool/migration/queue sleep을 추가하지 않았다.

State 한 건·공개 prefix CanonicalAt·state/pointer/manifest ValidateProjection만 optional prefix view로 읽는다. Through는 전진할 수 있지만 generation/기존 inputs는 writer commit까지 바뀌지 않는다. Commit의 epoch/plan/next-height/parent/fresh 검사는 그대로다. 첫 SQL 회귀에서 Reset 위에 남은 이전 fork canonical pointer의 교체까지 막은 오류를 발견해, 다음 높이 pointer/state 전진을 허용하고 facts/events/object bindings/ACK의 불변 쓰기 제한은 유지했다. 동일 key의 서로 다른 bytes와 잘못된 쓰기 namespace는 callback이 오류를 무시해도 rollback한다. 기존 Source/fetch writer/RPC/후속 epoch barrier/undo/outbox/raw ACK/GC 경계는 바꾸지 않았다. 지원하지 않는 driver/wrapper만 기존 경로를 사용하며 오류 시 fallback하지 않는다.

실제 writer2와 capture4의 독립 fresh를 동시에 gate로 멈춰 ACK retry/원천/API DB 읽기 진행·양쪽 미커밋 데이터 비노출·Reset 차단을 검사했다. capture fresh 거절/취소 후 facts/Through/event 무변경, writer 성공 뒤 ACK와 Reset/stale 거절도 확인했다. 실제 row lock과 같은/다른 bytes의 동시 ACK를 CPU1/race/count200으로 반복해800 tests/subtests를 통과했다. 일반 full view가 append/ACK를 모두 차단하고 fsync/synchronous_commit·read-only/nested/callback rollback/parameter-binding도 검증했다.

단계 시작의 실제 코드를 before 바이너리/소스로 보존하고 동일 fixture·소유 PostgreSQL16 fsync-on 서버의 별도 schema로 비교했다.2 warm+32 timed Arc profile34블록/68거래/544로그, 실제 ETH68/window/capture·writer·production GraphQL2 client(목표20/s closed loop), pool4/workers2/chunk1/acquisition_queue off/control1s/base plan을 고정했다.500ms streaming/1ms backlog 각각3쌍씩 총12회, CPU/기타 검증을 끝낸 뒤 `[before,after]→[after,before]→[before,after]` 순서로 실행했다. fixture와 operating/example YAML·의존성·Source/writer/RPC 코드는 바꾸지 않았다.

실행별 percentile/rate의3회 중앙값으로 backlog ACK p50/p95 9004.156/14328.976→4620.655/7824.005ms, API blocks8 p95/p99 33.828/89.179→17.023/41.285ms였다. 최종 완료 14.540→8.041초, 처리량 2.201→3.980블록/s(+80.8%), peak capture→writer gap 중앙값14→3블록이었다. 실제 API 완료율은19.876→19.745/s였다. 이전 단계와 환경 시점이 달라 그 수치를 baseline으로 재사용하지 않았다.

backlog API p50은4.942→5.709ms, DB commit p99는15.103→32.889ms로 늘었다. API p99는 전68.705~860.750/후18.143~200.769ms로 편차가 크고 두 번째 쌍에서는 증가했다. 모든 실행/지표의 개선이나 strict FIFO/장기 SLO를 주장하지 않는다. streaming 최종 완료는16.361→16.420초로 비슷하며 ACK p95 840.019→520.568ms, API p99 62.833→20.298ms였다. 첫 after streaming API p99 353.043ms도 그대로 남겼다. [전체 표/범위](../SOURCE_FACTS.md#연결4-잠금-분리와-ack-공정성-2026-10-17)를 따른다.

12회 모두 최종34블록/68receipt·646 normalized outbox digest·34 canonical input/ACK가 같고 captureCommit/sourceValidate/sourceACK 각34회·오류0이었다. API 전체3248건 오류0, DB-only API 추가 RPC0,5 HTTP/s envelope를 확인했다. 필수 fresh 수는 그대로이며 독립 fresh가 기존 coalescer에서 묶여 backlog 물리 HTTP66→39회가 됐다. 논리 RPC79→72회는 짧아진 실행의 control tick 감소를 포함한다. streaming 물리 HTTP는 배치 타이밍 차이로70→78회 늘었고 제한은 유지했다. ACK는20ms nominal polling 상한이며34블록 단계 계측과32블록 공개 지연을 구분했다.

PostgreSQL10패키지169통과/skip0, App60통과/선택2skip, native capture/facts/port140통과/SQL 등54skip, CPU1/race200회800통과, 같은 fixture streaming/backlog4블록 race, 전체 compile/vet·최종 lint0·예제2모듈을 확인했다. 마지막 fsync-off 전환은 서버 command-line on이 우선해 적용되지 않았다. 명시적 off의 별도 UUID 서버에서 Update/Append/Receipt3경로의 callback 진입 전 거절을 통과하고 정리했다. 최초 SQL/시험 스크립트 실패·재실행 로그도 보존했다. 전체 app/SLO를 실행한 것으로 표시하지 않는다.

양/음성 UUID 시험 컨테이너·명시적 볼륨과 두 바이너리를 제거하고 기존 Docker10개 이름/상태를 유지했다. branch는 refactor/arc-node-feed다. 증거는 `/private/tmp/indexer-lock-fairness-20261017/measurements.json`·`validation.json`·코드/바이너리 SHA·검증/소유권 로그다. 실제 Arc peer/외장 mount/450ms/24시간 SLO, 추가 worker/무거운 API 예산·필요시 관계형 source 색인은 남는다. 실제 공개 RPC/노드 활성화·운영 설정 변경·commit/push는 수행하지 않았다.

### 3.64 API tail 계측·지연 계산·읽기 슬롯 (10/17)

DB 연결4·공유5 HTTP/s/burst1·capture와 writer의 커밋 직전 fresh를 유지했다. 요청별 pool/입장 대기·SQL 왕복/결과 소비·server/client 전체 시간을 분리했다. SQL/인자/행 데이터는 노출하지 않고 진단 SQL/RPC는 추가하지 않는다. 과거 느린 실행에는 세부 span이 없어 사후 원인을 확정하지 않으며, 새 재현 부하의 단계 시간을 근거로 한다.

GraphQL `blocks`는 요청하지 않은 count 계산으로 최신 높이를 다시 읽어 SQL3회를 실행했다. `totalCount`와 block의 거래 상세 변환을 선택한 필드일 때만 한 번 계산한다. 거래 수는 raw model 길이로 유지하고 aliases/fragments/계약 생성 주소 응답을 검사했다. 메타데이터 요청은 계약 생성 receipt도 조회하지 않는다. 조회 필터·페이지·오류·count fallback·wire schema를 유지했다.

단일 SQL head/page 후보는20회 비교에서 streaming과 backlog 결과가 달라 제외하고 관련 port/SQL/테스트를 제거했다. 지연 계산만 적용한32/128블록 비교도 누적 tail을 안정시키지 못했다. 고정 API1개 정책은 일반 작업과 격리되지만 API끼리 직렬화해 추가 대기를 만들었다. 이 중간 결과를 최종 개선 수치로 재사용하지 않고 모든 결과를 보존했다.

최종 정책은 같은 pool4에서 API 최소1개를 예약하고 사용 가능한 일반 연결도 짧게 빌린다. 일반 연결 입장은1ms씩 시도하고 취소/종료를 관찰하며, 연결 생성 오류에는50ms backoff를 둔다. SQL 실행은 재시도하지 않는다. 별도 pool/Store/연결·migration·캐시는 없다. `q(ctx)`는 같은 Store의 bound transaction을 먼저 선택한다. source/facts/writer/RPC/epoch barrier/undo/outbox/raw ACK/GC 코드는 유지한다. source의 fresh 대기가 API 최소 슬롯을 점유하지 않는다.

`node.role: all`·명시적 max_conns4·단일 체인 root API에서 앱이 활성화한다. API/ingest 전용 역할,1/2/default0·다른 예산은 자동 변경하지 않는다. read-only Store와4 미만 pool은 예약하지 않으며 SDK API 생성은 opt-in이다. source snapshot API/멀티체인 route/무거운 API와 추가 worker 예산은 별도 검증 범위다. rows/Scan/Exec가 차용/예약 연결을 반환하고 lease 참조·API Stop·Store.Close가 자원을 회수한다.

실제 pool3개 점유/4번째 일반 작업 취소 중 HTTP API 읽기, 미커밋 비노출, lease+bound context의 원래 transaction 우선 사용, 다른 Store 격리, 취소/재연결/공격 문자열 파라미터 바인딩/4개 상한/참조와 Close 회수를 검증했다. 다른 backend PID의 두 rows가 동시에 열려 병렬 진행하고, 종료는 양쪽 rows를 기다리는 것도 검사했다. 최종 PostgreSQL/race·프런트엔드 앱·128블록 race·compile/vet/lint와 포트/계층 검사를 통과했다. 초기 전체 App 검사는8분 공통 한도에 도달해 완료한 결과를 보존하고 미완료44개만 재개,202개 최상위 항목을 완료했다(선택 측정/도구 항목 skip 포함).

최종 비교는 같은128 timed+2 warm Arc profile·pool4·workers2/chunk1·실제 ETH68/window fsync/capture/writer·production HTTP GraphQL2 client(목표 최대20/s closed loop)로 진행한다. 각 mode3쌍/교차 순서이며 최적화 전은 보존한 원본 GraphQL2파일+예약 비활성 method의 Go overlay, 후는 실제 코드다. 공통 fixture는128블록과 전체 outbox 페이지 읽기를 지원한다. 운영/예제 YAML·의존성·10/12 handoff는 바꾸지 않았다.

실행 중 턴 중단으로 runner/child가 종료된 사례는 테스트 assertion 실패로 분류하지 않는다. 완료4회를 보존하고 미완료 로그를 별도 보관했으며, 바이너리 SHA·소유 label·fsync를 확인해 같은 서버/바이너리로 남은8회만 이어 실행했다. 최종 집계와 자원 감사는 `/private/tmp/indexer-api-tail-20261017`에 기록한다.

최종12회에서 backlog 최근8블록 API p95/p99는4.675/12.173→3.348/5.901ms, streaming은6.615/31.347→3.348/8.239ms였다(실행별3회 중앙값). 각각p99 약51.5%/73.7% 감소다. 처리량은 backlog4.915→4.915블록/s, streaming 완료64.281→64.340초로 유지됐고 실제 API 완료율도약19.9/s였다. backlog 단일 블록 p99는3.640→4.735ms, ACK p50은약60ms 늘어 모든 지표 개선으로 해석하지 않는다. [조건·모든 실행 범위·한계](../SOURCE_FACTS.md#api-tail-계측과-불필요-작업-지연-2026-10-17)를 따른다.

최종130블록/260receipt/2,470 outbox digest와canonical ACK가12회 모두 같고 captureCommit/sourceValidate/sourceACK 각130회·오류0, 전체API10,826건 오류0이었다. 필수 fresh/5 HTTP/s와 SQL값 바인딩을 유지했다. 최종 SQL/race65통과·skip0, 프런트엔드 앱12통과, native API/GraphQL286통과·선택1skip, 포트/계층17통과,130블록 race·compile/vet·lint0·SDK 예제2모듈을 확인했다. 모든 소유 컨테이너/볼륨을 제거하고 기존10개 status/StartedAt을 보존했다. 비교 바이너리9개/약602MB를 정리했다.


### 3.65 단일 블록 p99 분리·후보 제외·실행 위치 비교 (10/17)

DB 연결4·공유5 HTTP/s/burst1·same Store/bound transaction과 커밋 직전 fresh를 유지했다.3.64의 단일 p99 중앙값3.640→4.735ms 증가는 각 실행 분포가 크게 겹쳤다. opt-in fixture의 기존 요청별 관측을 모두 남겨 p99를 만든 실제 요청의 admission/SQL/server/HTTP 시간을 대조하도록 했다. 추가 직렬화는 timed 종료 뒤 수행하며 SQL/RPC/요청 중 계측은 늘리지 않는다.

반환된 예약 연결을1ms 일반 pool 대기가 끝날 때까지 보지 못하는 코너를 재현했다. 무기한 pool probe와 기존1ms 예산 내 wakeup의 두 후보를 각각128블록/3쌍·두 모드로 비교했다. 후속 후보의 제어된 admission-only p99는1.213→0.020ms였지만 실제 API p99는 backlog4.527→5.971ms, streaming18.655→22.450ms였다. **두 정책 모두 제외하고 제품 query_reader와 기존 tests를 작업 시작 상태로 복원했다.** 새 opt-in wake 측정 helper와 전체 요청 기록만 남겼다. root `measurements.json`은 제외된 후보이며 최종 제품 성능 결과가 아니다.

별도 DB duration 진단에서 point execute p99/max0.322/0.566ms, parse+bind+execute 합 p99/max1.375/18.203ms였고 HTTP p99는32.933ms였다. 그 실제 요청은 admission30.080ms·SQL1.558ms였다. client SQL span을 순수 DB 실행으로 해석하지 않는다.12 CPU 호스트의 높은 부하와 서로 다른 구간의 spike를 관측했지만 개별 과거 표본의 인과는 확정하지 않았다. diagnostic logging은 복원했다.

32블록 placement 탐색 뒤 동일 Go1.26.8/CGO0·128블록·같은 PostgreSQL named volume에서 native→Docker DB와 별도 app/DB 컨테이너→전용 bridge를3쌍/모드 비교했다. point p99 중앙값은 backlog **3.059→1.855ms**, streaming **10.379→4.780ms**였다. streaming 첫 쌍2.508→3.592ms는 악화했다. 처리량은4.885→4.961블록/s 또는 입력-bound1.988→1.989블록/s였다. OS/runtime·network·raw temp 저장 위치의 묶음 비교이며 pure network/코드 인과/운영 SLO 증거는 아니다. 일반 API를 위해 새 연결 정책을 채택하지 않는다.

최종12회 모두130 capture/validate/ACK·260receipt·2080로그·2470 outbox digest와 API10,839건 오류0을 확인했다. SQL1/2회·물리5/s·fresh·post-decode epoch·undo/outbox·서버 파라미터 바인딩을 보존했다. 운영/예제 YAML·Go 의존성·handoff는 그대로다. [자세한 결과/제약](../SOURCE_FACTS.md#단일-블록-p99와-예약-연결-반환-대기-2026-10-17)을 따른다.

`scripts/measure-api-placement.py`는 고유 label의 DB/app/network/named volume과 동일 CGO0 빌드로 재현한다. 로컬 Unix Docker만 허용하고 성공/실패/중단 정리·원본 로그 보존을 구현했다.4블록 smoke와 build 실패 cleanup·원격 endpoint 사전 거부를 확인한다. 실험 원본은 `/private/tmp/indexer-point-tail-20261017`, 최종 배치 비교는 `placement128/measurements.json`에 보존한다. 실제 peer/외장 mount/장기 부하·무거운 API/추가 worker 예산은 남는다.

최종 관련 race tests/subtests는 반복 포함367회 통과·선택2skip이었다(Reader/binding16, facts/fresh49, CPU1 wake3,128블록 혼합1, API/GraphQL286, frontend12). compile/vet/lint도 통과했다. 재현 도구의4블록 정상 실행, 의도한 build 실패 뒤 자원 정리, 원격 endpoint 사전 거부를 검증했다. 모든 테스트 소유 DB/app 컨테이너·network·volume(이미지가 만든 빈 volume 포함)을 제거했고, 기존10개 컨테이너의 이름/status/StartedAt을 보존했다. 비교 바이너리7개·367,070,381bytes도 정리했다.

### 3.66 Docker streaming p99의 쿼리 종료 대기 개선 (10/17)

3.65 회귀 실행의 요청별 기록에서 입장 대기는 작고 SQL wire·나머지 서버/HTTP 시간이 분산돼 있었다. 별도32블록 Docker 실행 추적으로 pgx5.7.5의 정상 쿼리 종료도 취소 감시 고루틴과 동기 rendezvous하는 비용을 확인했다. 예약 API의 QueryRow.Scan 하위 누적 스케줄 대기는22.066760→0.008256ms였다. 전체 앱 scheduling delay는 증가했으므로 이 값을 전체 성능으로 해석하지 않는다.

`query_io.go`를 추가해 leased API IO만 AfterFunc/socket deadline으로 감시한다. 행 Close까지 취소 감시, callback join/reset 뒤 연결 반환, context/driver timeout 오류 보존, 취소 확인/SQL once를 유지했다. bound transaction/source/writer/epoch/commit fresh·SQL 바인딩·pool4/예약1+일반 차용·5 HTTP/s/burst1은 그대로다. Go/deps·운영/예제 YAML·GraphQL·측정 fixture/재현 도구·handoff는 변경하지 않았다.

동일 Linux/CGO0/PG16 Docker app/DB로128 timed+2 warm을 각 모드3교대쌍씩 실행했다. 프로파일러와 테스트/빌드를 껐다. 실행별 percentile 중앙값은 streaming 단일 p99 **4.059→3.518ms**, 목록8 **5.343→4.466ms**, ACK480.975→479.996ms·처리량1.989→1.989blocks/s였다. 누적 부하는 단일4.904→1.893ms·목록9.985→3.674ms·처리량4.961→4.961blocks/s였다. streaming 세 번째 목록5.969→6.794ms·ACK499.760→1519.851ms 증가와 동반 raw/수신 지연, 누적 첫 목록9.985→11.146ms 증가, streaming 단일 SQL wire p991.864→2.290ms 증가도 남긴다. 모든 실행/지표의 개선이나 실제 운영 SLO를 주장하지 않는다.

12회 모두130 capture/validate/ACK,260 receipts·2080logs·2470 normalized outbox digest가 일치하고 API10,820건 오류0, SQL1/2회·물리5/s/fresh를 확인했다. CPU1/race45·facts/fresh17·실제 HTTP1·4블록 혼합 race1 및 전체 API suite/vet/lint가 통과했다. 초기 helper 실패와 수정도 보존한다. 테스트 소유 app/DB/network/volume·비교 바이너리125,120,439bytes를 정리했고 기존10개 컨테이너의 이름/status/StartedAt·보호 hash가 유지됐다. 최종 원본은 `/private/tmp/indexer-docker-stream-tail-20261017/measurements.json`, 상세와 한계는 `docs/SOURCE_FACTS.md`의 Docker streaming 절을 따른다. 실제 peer/외장 mount/장기·무거운 API/추가 worker 검증은 남는다.

## 4. 저장소 관리

- `tools/astgraph` 바이너리(7 MB)가 #92에서 잘못 commit됐다가 #93에서 지워졌다. git history에는 남는다. 지우려면 main의 history를 다시 써서 force push해야 하므로 하지 않았다.
- 시험 전용 도우미가 운영 패키지에 있다(`Fetcher.FetchBlock`, `orderbook.Service.Sync`, `agg.RecomputeCandles/Series`). 시험이 쓰므로 남겨 두었다.


### 3.67 원본 저장·수신·ACK 분리와 같은 블록 fsync 공유 (10/17)

DB 연결4·공유5 HTTP/s/burst1·capture/writer 커밋 직전 uncached fresh·same Store/bound transaction·epoch/삭제 fence·SQL 바인딩을 유지했다. 별도 trace에서 느린 블록의 수신1.720ms 뒤 raw durable395.076ms, ACK782.674ms를 확인했다. 해당 raw append393.322ms는 data sync161.881·seal207.754·create22.936ms였고 mutex 대기는0.093ms였다. 저장과 다음 수신이 같은 worker의 임계 경로에 있었다. 첫 수신 선행 후보는128블록·3쌍의 ACK 중앙값800→980ms로 증가해 제외했다. 원복 코드/결과는 `prefetch-probe/`에 보존했다.

최종 후보는 선택적 `BatchStream`과 `AppendBatch/Wait`로 이미 fetch한 같은 블록의 header/body/receipt 최대3개를 함께 기록한다. 원본 동기화 뒤에만 참조/callback/알림을 반환하며, segment 회전 전 fsync·완성 prefix 복구·capacity/cancel·fault·기존 Next/Append·파일 형식/GC 경계를 유지한다. 추가 수신 goroutine·cross-block payload queue는 남기지 않았다. 별도32 timed+2 warm 진단의102개 같은 자료에서 fsync102→42회, raw 누적1037→780ms였다. frame별/배치별 작업 p99의 분모를 혼동하지 않는다.

같은 Go1.26.8/CGO0 Linux arm64·Docker app/PG16·fsync-on·2 warm+128 timed·3 alternating pairs/mode를 재측정했다. streaming500ms 입력에서 capture p99 중앙값305.143→240.024ms, ACK500.385→500.701ms, receive interval p99583.913→579.171ms였다. backlog1ms 입력에서 receive interval p99108.258→53.474ms, ACK25458.085→25460.214ms, 최종4.961blocks/s로 ACK/처리량은 사실상 같았다. streaming raw 누적 중앙값1984→2404ms·API 단일/목록8 p996.630/6.357→9.690/16.084ms는 증가했다. backlog 단일 API5.940→3.295ms는 낮았으나 목록5.172→6.413ms는 증가했다. 첫 streaming ACK도481→586ms로 증가했다. 공유 호스트 부하/실행별 편차를 보존하며 전체 tail 개선·통계적 유의성·운영 SLO를 주장하지 않는다.

최종12회 모두130 capture/validate/ACK·260 receipt·2080로그·2470 normalized outbox digest·공유 물리 RPC envelope를 통과했고 API10807건 오류0이었다. 관련 race tests/subtests466회·선택/helper9skip, vet/lint0 issues가 통과했다. fsync 직후 프로세스 종료·batch 회전/중복/충돌/완성 prefix·취소/guard fail closed·legacy stream·128블록 writer/API·restart/reorg/shutdown을 포함한다. profiler/DB timing은 본 실행과 분리했고 설정은 off로 복원했다. 기존 Go/의존성/YAML/handoff/이전 query IO/reader/writer hash는 유지했다.

소유 테스트 컨테이너2개·network·named volume과 바이너리3개187703550bytes를 정리했고 기존10개 컨테이너 name/status/StartedAt이 같았다. 최종 근거는 `/private/tmp/indexer-raw-latency-20261017/measurements.json`, 별도 진단은 `trace-batch-*`, 제외 후보는 `prefetch-probe/`다. [상세 수치/한계](../SOURCE_FACTS.md#원본-저장수신ack-분리와-같은-블록-fsync-공유-2026-10-17)를 따른다. 다음은 metadata/index 회전 I/O와5 HTTP/s 안의 fresh admission 공정성을 API 회귀와 함께 점검하는 것이다. 실제 Arc peer·외장 mount·24시간·추가 worker 운영은 남는다.


### 3.68 Seal과 다음 번호의 directory fsync 공유 (10/17)

3.67의 같은 블록 묶음 저장을 양쪽에서 유지하고 자동 교체의 metadata I/O를 개선했다. index를 먼저 fsync·rename·directory sync한 뒤 seal/다음 STORE 번호를 각각 file sync·rename하고 부모 directory fsync를 공유한다. 이 barrier 뒤에만 새 raw를 생성하므로 index→seal·번호→raw 선행 관계와 기존 파일/복구/GC/quota/ACK 계약을 유지한다. explicit Seal/Close·segment/번호 한도는 기존 경로다. 각 단계 프로세스 종료·공유 sync 전 metadata 생존 네 조합·생성 실패 fail closed와 prefix 재생을 검사했다.

별도32 timed+2 warm trace는 directory sync61→49회·raw data sync42회 동일이었다. seal+create 누적337→82ms였지만 수정하지 않은 data sync 시간도 변해 시간 감소 전체를 코드 단독 효과로 돌리지 않는다. 느린22번 블록의 capture148ms→ACK455ms 사이 writer fresh237ms·DB commit43ms가 확인됐다. limiter 대기 p99~189ms가 남아 검사 시점/캐시/별도 제한기를 바꾸지 않았다.

동일 Go1.26.8/CGO0 Docker app/PG16·128 timed+2 warm·모드별3교대쌍·pool4/최소API예약1/차용·5 HTTP/s·커밋 직전 fresh를 유지했다. 원본 append p99 중앙값은 streaming117.574→101.277ms, 누적99.247→28.885ms; 누적 시간은2616→2197/570→349ms였다. streaming capture247.272→237.260ms, ACK459.994→458.814ms·누적 ACK25478→25462ms로 ACK/최종1.990·4.961blocks/s는 사실상 같다. streaming 수신 간격562.516→580.149ms와 API 단일/목록8 p993.450/4.608→4.521/5.688ms는 증가했다. 마지막 streaming 쌍의 원본/capture/ACK·첫/세 번째 API 증가도 보존한다. 전체 tail/통계적 유의성/운영 SLO를 주장하지 않는다.

최종12회 모두130 capture/validate/ACK·260receipt·2080로그·2470 outbox digest·RPC envelope를 통과했고 API10812건 오류0이었다. 최종 관련 race348 tests/subtests·선택/helper7skip, vet/lint0 issues가 통과했다. 모든 빌드/검사/별도 profiler 뒤 본 비교를 실행했고 PG timing은 off로 복원했다. 테스트 소유 컨테이너2개·network·named volume과 바이너리2개125142322bytes를 정리했다. 기존10개 컨테이너 상태와 보호한 config/deps/handoff/RPC/source/writer/API/fixture hash는 같다. 근거는 `/private/tmp/indexer-rotation-20261017/measurements.json`과 [상세 기록](../SOURCE_FACTS.md#seal과-다음-번호의-directory-fsync-공유-2026-10-17)이다. 다음은 fresh 내부 DB/RPC 대기와 API tail의 IO 부하 재현이다. 실제 peer/외장 mount/24시간·무거운 API/추가 worker는 남는다.

### 3.69 Fresh DB/RPC 대기와 API tail 분리 (10/17)

연결4·같은 pool의 API 최소1예약/차용·shared5 HTTP/s/burst1·capture/writer 커밋 직전 fresh를 유지했다. writer fresh의 scope/canonical pointer/manifest를 `ValidateProjectionBlock`의 한 published-prefix view로 묶었다. 같은 Store/transaction의 generation fence와 기존 검증을 사용하고, 매번 새 RPC 응답 뒤 독립 epoch 검사도 유지한다. 원본/rotation/writer/API SQL·연결 정책은 바꾸지 않았다. 공통 opt-in runtime trace로 SQL/acquire task·RPC read/dispatch ID·API 요청을 나눴다. GraphQL resolver가 다른 goroutine인 점을 반영해 API SQL은 요청 context 진단으로 연결했다.

별도32 timed+2 warm trace에서 bound fresh34건 모두 SQL pre9→3/post2→2, 총11→5회였으며 pool acquire는0회였다. pre DB p50/p99는1.731/3.924→0.735/2.570ms였다. RPC 큐 p50은162.174→153.244ms, 실제 local HTTP p50은0.399→0.466ms로 공유 queue/limiter 대기가 대부분 남았다. API tail은 admission뿐 아니라 wire/행 소비·나머지 handler·HTTP 바깥 구간에도 있었다. 서버 blocks SELECT 집계와 요청별 API를 같은 표본으로 차감하지 않는다. 진단 API p99가 증가한 점과 pgx watcher의 누적 scheduling delay도 보존했다. PG IO/WAL timing·SQL duration logging은 원래 값으로 복원했다.

Go1.26.8/CGO0 Linux arm64·같은 격리 Docker app/PG16·fsync-on·raw 배치/metadata barrier·2 warm+128 timed·workers2/chunk1/base plan/API2 clients를 고정했다. streaming500ms/backlog1ms 각각3 alternating pairs로 총12회이며 본 측정 중 빌드/검사/프로파일러는 없었다. 실행 percentile의3회 중앙값으로 streaming API 단일/목록8 p99는5.292/6.660→3.233/4.456ms였지만 누적은1.609/2.200→1.813/2.732ms로 증가했다. streaming 두 번째·누적 두 번째와 세 번째 쌍의 API가 증가했다. streaming capture233.728→269.861ms·ACK459.421→479.407ms·raw p9979.061→157.230ms도 증가했다. fresh p99는 streaming185.766→185.544/누적185.097→184.877ms, 누적 ACK25458.680→25461.021ms·처리량4.957블록/s로 사실상 같았다. host1분 load13.86~57.11의 소수3쌍이므로 시간 차이 전체의 코드 인과/통계적 유의성을 주장하지 않는다. 채택 범위는 같은 snapshot의 안전한 SQL11→5 감소이며 전체 API/수집 tail 해결은 아니다.

12회 모두130 capture/validate/ACK·동일2470 outbox digest·API10812건 오류0·SQL1/2회·물리5/s envelope·offline API RPC0을 통과했다. 관련 race337 tests/subtests(반복 포함)·선택1skip, vet/lint0 issues였다. 새 두 backend preflight·fresh 재호출·RPC 중 Reset과 기존 publication/fence/재시작/reorg/128블록 혼합·SQL 보안을 검사했다. 테스트 소유 app/DB/network/named volume·바이너리125152749bytes를 정리했고 기존10개 컨테이너 상태와 보호 config/deps/handoff/raw/pipeline/writer/query IO hash가 같다. 최종 원본은 `/private/tmp/indexer-fresh-tail-20261017/measurements.json`, 진단·표·한계는 [상세 기록](../SOURCE_FACTS.md#fresh-dbrpc-대기와-api-tail-분리-2026-10-17)을 따른다. 다음은 같은 예산의 capture/writer RPC admission과 CPU/IO를 고정한 HTTP/행 소비 tail 재현이다. 실제 peer/mount/24시간 운영은 남는다.

### 3.70 Capture와 writer SQL 종료 대기 감소 (10/17)

이전 raw 시간 증가와 API wire/handler/HTTP 지연을 구분해 재현했다. pgx5.7.5가 정상 SQL 종료에도 context watcher goroutine과 rendezvous하는 비용을 확인하고, 이미 소유한 capture/bound writer transaction의 Exec/Query/QueryRow에 기존 API의 AfterFunc/socket deadline 감시를 확장했다. rows 소비와 취소 callback 완료 뒤 deadline을 복원한다. 원래 scope/fresh context·BEGIN/COMMIT/ROLLBACK·SendBatch·admission·동일 Store/transaction/generation·fsync/undo/outbox/ACK는 유지한다. pool4/예약1·차용·shared5 HTTP/s/burst1·커밋 직전 uncached fresh/응답 뒤 epoch·SQL 바인딩도 그대로다. RPC·API SQL·window/rotation·writer 파일과 설정/의존성은 바꾸지 않았다.

별도32 timed+2 warm trace에서 watcher/captureWrite/captureView를 거친 누적 scheduling delay473→278ms, channel send362→157ms, capture Commit 경유61.494→4.480ms였다. 이는 블록 p99가 아니다. capture34건의 SQL36회·fresh pre3/post2/acquire0이 같았다. capture SQL 합산 p50은9.479→8.811ms였으나 p99는20.685→47.820ms, 진단 API p99도 증가했다. raw fsync42·directory sync49회와 SQL 개수는 유지됐다. 원본/WAL/공유 RPC/HTTP scheduling 대기가 남아 모든 지연 감소를 주장하지 않는다. IO/SQL logging 설정은 원래 값으로 복원했다.

본 비교는 모든 빌드/회귀/별도 trace를 끝내고 같은 Go1.26.8/CGO0 Linux arm64·별도 Docker app/PG16·fsync-on·raw 배치/barrier·2 warm+128 timed·workers2/chunk1/base plan/API2 clients에서 streaming500ms/backlog1ms 각각3 alternating pairs로 총12회 실행했다. 실행별 percentile의3회 중앙값으로 streaming API 단일/목록8 p99는3.948/7.688→2.491/3.746ms(약36.9%/51.3% 감소), 누적은2.767/3.856→1.777/3.637ms(약35.8%/5.7% 감소)였다. streaming API는 세 쌍 모두 낮았지만 누적 첫 쌍과 각 API p50 중앙값은 증가했다. 목록 SQL-wire 누적 p99도1.955→2.598ms로 늘었다.

streaming capture p99는246.489→253.436ms로약2.8% 증가했고 한 쌍 감소·두 쌍 증가였다. capture commit p99~219ms·ACK440.739→440.736ms는 비슷했다. 원본 p99/누적/수신 간격 중앙값도 늘었으나 해당 파일 hash는 초기값과 같다. 누적 ACK25649→25737ms·처리량4.919→4.908블록/s도 개선으로 표시하지 않는다. host load6.03~16.69·소수3쌍의 시간 차이를 모두 코드 단독 인과나 통계적 유의성으로 확정하지 않는다. 채택 범위는 정상 SQL 종료 비용 감소와 관측한 API tail 개선이며 capture tail/SLO 해결은 남는다.

12회 모두130 capture/validate/ACK·2470 outbox digest·API10836건 오류0·SQL1/2회·물리5/s envelope·offline API RPC0을 통과했다. 전체 PostgreSQL/capture/facts/RPC/telemetry와128블록 혼합·managed features/독립 worker/reorg/재시작/종료 등 관련 race729 tests/subtests(반복 포함)·선택2skip, vet와 변경 패키지 lint0 issues였다. 새 취소/스트리밍/EOF·완료 뒤 같은 tx의 다음 statement·이미 취소 시 SQL0·context 진단 보존을 검증했다. 테스트 소유 app/DB/network/named volume·바이너리125158887bytes를 정리했고 기존10개 컨테이너 상태를 보존했다. 원본은 `/private/tmp/indexer-capture-api-tail-20261017/measurements.json`, [분석·표·회귀와 한계](../SOURCE_FACTS.md#capture와-writer-sql-종료-대기-감소-2026-10-17)를 따른다. 다음은 head/capture/writer RPC 예약과 원본 fsync를 동일 블록으로 연결한 streaming tail 개선이다. 실제 peer/외장 mount/24시간 운영은 미검증이다.


### 3.71 유지보수 RPC의 fresh 배치 합류 (10/17)

같은 블록의 raw fsync·capture fresh와 정기 canonical 검사의 HTTP 예약을 연결했다. 유지보수 전송 직후20~65ms에 도착한 capture가 다음200ms 슬롯을 기다리는 사례를 확인했다. ControlStep의 transaction 밖 조상/checkpoint 사전 검사에만 context 힌트를 붙이고 rate≤10/burst1/read coalescer에서 다음 foreground와 최대500ms 모으도록 했다. foreground 도착 시 즉시 모으기를 끝내고 그 뒤 기존 limiter로 전송한다. 새 큐/worker/limiter·결과 cache/선행 fresh는 없다. 데이터/writer/finality commit의 원래 context·각 fresh/응답 뒤 epoch·동일 Store/transaction·SQL 바인딩·삭제/ACK는 유지한다. 유지보수 응답은 더 늦을 수 있고 caller deadline은 연장하지 않는다. unlimited/high rate/다른 burst/알려진 batch 미지원은 기존10ms다.

최초75ms 후보는 경계 직후 fresh를 놓쳐 probe로 보존하고 본 비교에서 제외했다. 최종32 timed+2 warm 진단의 전체 논리99건/역할82+17·timed RPC80건은 양쪽 같고 혼합 dispatch1→16·timed HTTP79→64였다. timed capture RPC 큐 p50/p99는63.875/179.661→0.031/96.778ms, 유지보수 직후 슬롯 누락 사례5→0이었다. raw p99가32.213→122.448ms로 늘어 종단 p99221.096→232.075ms도 증가했다. 후보25번의 raw122.448ms는 seal81.389/create16.687/data sync24.061ms였고 RPC queue0.023ms·capture11.048ms였다. RPC와 disk tail을 구분한다. 진단 API 증가도 보존했고 profiler/PG 설정은 원래 값으로 복원했다.

같은 Go1.26.8/CGO0 Linux arm64·격리 Docker app/PG16·fsync-on·raw 배치/barrier·2 warm+128 timed·workers2/chunk1/base plan/API2 clients·pool4/예약1·shared5 HTTP/s/burst1/commit fresh를 고정했다. streaming500ms/backlog1ms 각각3 alternating pairs로 총12회이며 모든 빌드/회귀/별도 trace 뒤 순차 실행했다. 실행 percentile의3회 중앙값으로 streaming capture p99는259.427→213.765ms(약17.6% 감소), ACK480.103→419.706ms(약12.6% 감소)였다. 각 쌍의 capture/ACK·두 API p99가 모두 낮았다. 동일320 논리조회에서 physical HTTP319/302/305→256/256/256회였다. API 단일/목록8 p99 중앙값은3.051/5.162→2.692/3.940ms였다. raw 시간도 감소해 모든 시간 차이를 코드 단독 효과로 확정하지 않는다.

누적은 physical HTTP129회씩·ACK25460.508→25444.814ms·처리량4.961블록/s로 사실상 같았다. 반면 API 단일/목록8 p99는1.607/2.456→1.875/3.157ms로약16.7%/28.5% 증가했고 세 쌍 모두 증가했다. admission은 낮아졌으나 SQL-wire tail은 늘어 연결 개수 하나로 설명하지 않는다. host load3.22~9.22·소수3쌍의 편차·유지보수 지연·누적 API 회귀를 명시하고 실제 Arc/450ms/장기 SLO 완료로 표시하지 않는다.

12회 모두130 capture/validate/ACK·2470 outbox digest·API10808건 오류0·SQL1/2회·물리5/s envelope·offline API RPC0을 통과했다. 최종 관련 race1728 tests/subtests(단일 CPU200회1400 포함)·선택2skip, vet/lint0 issues였다. 취소/deadline/FIFO/인증/member 한도/새 응답/finality commit 역할과 capture/facts/Arc/128블록 writer/API·reorg/재시작/종료를 검사했다.75ms 후보 검사는 최종 수에서 제외했다. 테스트 소유 app/DB/network/named volume·바이너리3개187742684bytes를 정리했고 기존10개 컨테이너 상태와 보호 source/config/deps/handoff hash를 보존했다. 원본은 `/private/tmp/indexer-rpc-admission-20261017/measurements.json`, [정책·블록별 분석·전체 표](../SOURCE_FACTS.md#유지보수-rpc의-fresh-배치-합류-2026-10-17)를 따른다. 다음은 누적 API wire/행 소비 tail과 token 부족 시 모으기 정책·API/commit 시점의 겹침을 분리하는 작업, 이후 raw seal/create sync 비용이다. 실제 peer/외장 mount/24시간 운영은 남는다.

### 3.72 누적 API의 최신 높이·블록 윈도우 통합 조회 (10/17)

누적 API의 연결 admission·SQL-wire/행 소비·서버 parse/bind/execute·나머지 handler를 같은 요청으로 분리했다. 진단 전용 PID/시각 observer overlay와 PG 로그를 연결했고 모호한 대응은 제외했다(기준520 API 중489, 최종516 중516 대응). 길어진 wire를 연결 부족·SELECT 실행·순수 네트워크 중 하나로 단정할 수 없었다. 기준35.340ms 단일 API의 admission0.00146ms·wire3.109ms·서버 단계 합0.284ms처럼 handler/프로토콜 대기도 있었고, 제외한 후보의 다른 요청에서는 서버 parse 자체가63.950ms였다. 서로 다른 percentile을 차감하지 않았다. 최종 진단의 목록 residual p50은0.301→0.143ms, 서버 단계 p50은0.238→0.308ms여서 줄인 왕복과 늘어난 계산도 구분했다.

최종 구현은 선택적 LatestBlockWindowReader다. tip AS MATERIALIZED로 indexed 높이를 한 번 계산하고 그 높이 윈도우를 같은 SQL statement snapshot에서 읽는다. 기본 목록 SQL2→1회이며 단일은1회 그대로다. 행 수가 아니라 높이 기준 offset/limit·빈칸·미공개 높이 차단·기존 순서/필터/페이지·지연 totalCount·명시적 범위/미지원 저장소 fallback을 유지한다. q(ctx)가 같은 Store의 bound transaction을 먼저 쓰며 API 예약1/유휴 차용·pool4·취소/row 종료·SQL 바인딩을 유지한다. 메타/본문 손상을 빈 결과로 바꾸지 않고 특수 uint64 경계는 기존 범위 조회로 보존했다. schema/config/Go/의존성은 그대로다.

busy token이면 유지보수 grace를 줄이는 후보는 제외했다. 다음 foreground가 슬롯 전에 도착한다는 보장이 없었고 Streaming 같은320논리조회에서 physical HTTP256→308/305회로 늘었다. 완료된3개 실행과 중단 실행·소스/추적은 busy-window-probe에 보존했으며 최종3쌍에 섞지 않았다. 기존3.71의500ms 합류 정책·RPC/capture/writer/fresh/raw 파일은 원래 해시로 복원했다. 새 pool/큐/worker/limiter·cache/선행 fresh는 없다.

같은 Go1.26.8/CGO0 Linux arm64·격리 Docker app/PG16·fsync-on/동일 synchronous_commit=off 세션·raw64KiB/8records/같은 블록3자료/metadata barrier·2 warm+128 timed·workers2/chunk1/base plan·API2 clients/목표20rps·pool4/예약1·shared5 HTTP/s/burst1/커밋 직전 fresh로 Streaming500ms/누적1ms 각각3 alternating pairs, 총12회를 실행했다. 모든 빌드/회귀/별도 진단과 PG 설정 복원 뒤 본 비교를 했고 중간 profiler/테스트/SQL timing은 없었다. 실행별 percentile/rate의3회 중앙값이다.

누적 단일/최근8블록 API p99는2.812/3.682→1.945/2.648ms로약30.8%/28.1% 감소했다. 단일은 세 쌍 모두 낮았지만 목록 마지막 쌍은7.380→9.162ms로 늘었다. 목록 SQL-wire p99도1.638→1.657ms로 소폭 증가했다. 누적 capture25.245→25.259초·ACK25.454→25.459초·처리량4.961블록/s는 사실상 같다. physical HTTP는129/129/130→129/129/129회이며 정기 검사 한 번의 차이를 data fresh 감소로 해석하지 않는다.

Streaming은 같은320논리조회에서6회 모두HTTP256회로 배치 이점을 유지했다. 하지만 capture p99190.575→206.360ms(약8.3% 증가), ACK380.905→420.129ms, raw p9977.412→135.747ms로 늘었다. API 단일3.605→4.045ms는 악화했고 목록5.424→4.713ms는 낮았다. capture는 세 쌍 모두 높았으므로 수치상 Streaming 지연 개선 유지까지 완료라고 하지 않는다. 원본 코드가 같고 I/O 편차도 컸지만 모든 차이의 원인을 디스크나 API 하나로 확정하지 않는다. host load5.81~17.18·소수3쌍의 공유 환경이며 실제 Arc/450ms/24시간 SLO 완료도 아니다.

12회 모두130 capture/validate/ACK·390원본자료·260거래/receipt·2080로그·2470 outbox digest가 같았다. API10804건 오류0, 기준SQL1/2·후보1/1, 물리5/s envelope·offline API RPC0을 확인했다. 전체 RPC/capture/facts/Arc·PostgreSQL/API/GraphQL 회귀 후 최종 경로 race와 실제128블록 앱 race·vet/lint0 issues를 다시 통과했다. 같은 transaction/미커밋 차단·gap/offset/경계·손상/취소·필터/페이지/fallback을 검사했다. 제외한 busy 후보의 반복 검사는 최종 코드의 새 concurrency 테스트 수로 세지 않았다.

근거는 /private/tmp/indexer-backlog-api-20261017의 measurements/supplemental-summary/sql-match/api-schedule/validation JSON, 원본 trace·전후 소스/해시·overlay·재현 스크립트와 [상세 분석](../SOURCE_FACTS.md#누적-api의-최신-높이블록-윈도우-통합-조회-2026-10-17)이다. 소유 app/DB/network/named volume과 임시 바이너리7개438077110bytes를 정리하고 기존10개 컨테이너 상태를 보존했다. 다음은 raw seal/create sync와 API handler/서버 대기의 동시 변화·누적 목록 마지막 쌍의 악화를 같은 블록/요청으로 연결한다. 실제 peer·외장 mount·장기 운영은 별도다.

### 3.73 Streaming capture와 회전 메타데이터 준비 (10/17)

3.72의 API SQL1회 구현과3.71의 RPC 합류 정책을 유지하고 Streaming capture의 raw 저장·seal/create·RPC 큐·SQL을 같은 블록으로 분리했다. 최초 SQL logging probe의22번은 raw208.185ms 중 seal163.543ms였다. PG 설정 복원 뒤 최종32블록 trace에서 기준20번의 raw는8.197ms였지만 capture273.080ms 중 SQL span252.303ms였다. 후보4번은 raw30.703ms·RPC 큐189.675ms·SQL96.454ms로 원인이 달랐다. SQL span은 서버 SELECT 실행 시간과 같지 않다. 최종 진단의 raw/capture/RPC 큐 tail은 오히려 증가했으며 진단 수치를 본 비교의 개선 증거로 사용하지 않는다.

최종 코드는 seal/STORE 임시 파일을 먼저 준비하고 각각 기존 full Sync/Close/rename 순서를 유지한다. 파일시스템이 독립 쓰기를 함께 처리할 기회를 주지만 fsync 수를 줄이거나 병합을 보장하지 않는다. index barrier→seal/STORE rename→공유 directory fsync→다음 raw 순서·동일 파일 형식·quota·Root/nofollow·prefix 복구·poison·GC/삭제 fence는 그대로다. metadataPrepared crash 경계·authoritative STORE 불변·다음 raw 미생성과 준비/rename 실패 후 재시작 prefix를 검사했다. 새 worker/큐·cache·DB 연결·RPC limiter는 없다.

같은 Go1.26.8/CGO0 Linux arm64·Docker app/PG16·fsync-on·raw64KiB/8records/동일 블록3자료/barrier·2 warm+128 timed·workers2/chunk1/base plan·API2 clients/목표20rps·DB4/예약1·5 HTTP/s/burst1·커밋 직전 새 fresh를 고정했다. 양쪽 DSN의 synchronous_commit=off와 capture SET LOCAL synchronous_commit=on도 같다. Streaming500ms/누적1ms 각각3 alternating pairs, 총12회이며 본 비교 중 작업 소유 trace/PG timing/빌드/테스트는 없었다. 실행별 percentile/rate의3회 중앙값이다.

Streaming capture p99는477.593→239.330ms(약49.9% 감소), ACK686.458→533.842ms(약22.2% 감소), raw207.834→109.630ms였다. 그러나 capture 첫 쌍은137.759→239.330ms로 악화했다. 나머지 두 쌍911.114/477.593→247.682/230.243ms였다. raw 누적 중앙값3334.537→3394.926ms와 API 단일/목록 p9910.807/10.272→13.499/21.682ms는 증가했고 최종 처리량1.991블록/s는 같다.

누적 API 단일/목록8 p99는14.832/13.569→16.148/58.024ms였고 목록은 세 쌍 모두 악화했다. admission·SQL-wire·나머지 handler tail이 함께 달라 연결 부족 하나로 설명하지 않는다. 후보 마지막 쌍의105.625ms 목록 요청은 admission0.0024ms·wire92.750ms였다. 기준 마지막 쌍에는 SQL-wire919.858ms 요청도 별도로 있었다. API SQL1회 코드/결과는 유지했지만 **수치상 이전 API 개선 유지와 성능 수용은 미완료**다. 누적 ACK25.654→25.898초·최종4.923→4.878블록/s도 개선되지 않았다.

이번 host1분 load는164.67~243.08로 높았다.32블록 진단 악화·첫 Streaming 쌍/누적 API 회귀·공유 호스트 I/O/스케줄 편차를 함께 남기며,50%의 코드 단독 효과나 실제 Arc/450ms/24시간 SLO를 주장하지 않는다. 다음은 낮고 일정한 호스트 부하에서 같은3쌍으로 API 회귀를 재현하고 capture36 SQL 왕복의 immutable row 저장 묶음을 검토한다. 충돌/바인딩/동일 transaction/fresh/ACK 계약을 유지해야 한다.

12회 모두130 raw append/capture/validate/ACK·390자료·260거래/receipt·2080로그·2470 outbox digest가 같았다. API10834건 오류0·단일/목록SQL1회·물리5/s envelope·offline API RPC0을 확인했다. Streaming 물리HTTP256/252/254→256/256/256, 논리320/320/321→320/320/320이며 정기 검사/합류 시점 차이와 data fresh130회를 구분한다. API/DB/RPC/fresh/config/deps의 초기 hash는 같고 보호58파일 중 window3개만 변경했다.

최종 관련 race407 tests/subtests 통과·선택7skip, Linux window98통과·선택6skip, 실제128블록 writer/API·capture/facts/RPC/Arc·회전/복구·vet/lint0 issues를 확인했다. 최초 병렬 RPC telemetry 대기시간 assertion과 Arc reorg5초 DB I/O timeout은 같은 코드의 패키지 전체를 순차 race로 재실행해 모두 통과했다. assertion/timeout을 바꾸지 않았고 실패 로그를 보존했다. Linux 최초 상대 fixture 누락도 fixture/cwd 복원 후 같은 바이너리 전체를 통과했다. 선택200회 stress는 반복 I/O를 중단했으며 성공 수에 넣지 않았다.

근거는 `/private/tmp/indexer-stream-capture-20261017/measurements.json`, supplemental-summary/api-tail-check/block-stages/trace-analysis/validation/final-audit JSON과 전후 소스/해시·원본 trace·재현 스크립트다. [상세 표·계약·한계](../SOURCE_FACTS.md#streaming-capture와-회전-메타데이터-준비-2026-10-17)를 따른다. 소유 app/DB/network/named volume·바이너리3개133632465bytes를 정리했고 시작 시점 기존12개 컨테이너 상태를 보존했다. 실제 peer·외장 mount·장기 운영은 별도다.

### 3.74 누적 API 재검증과 호스트 부하 확인 (10/17)

3.73의 회귀를 재검증하려 했으나 **낮은 부하128블록·3쌍 검증은 미완료**다. 시작 시12 CPU 호스트의1분 load가약305였다. 같은 Go/CGO0 Linux arm64·Docker app/PG16/fsync-on·동일 raw 배치/barrier·2 warm+128 timed·workers2/base plan/API2 clients20rps·DB4/예약1·5 HTTP/s/burst1/commit fresh를 유지했다. 변경 전은3.73 이전 window3파일 overlay이며 작업 파일을 바꾸지 않았다. 두 바이너리 SHA는3.73 본 비교와 각각 같다.

누적 변경 전후3쌍과 동일 candidate 바이너리 대조3쌍을 교차 배치하고 이후 Streaming3쌍을 계획했다(총18회).1초 host load와 모든 원본을 보존하며 사전 기준 load≤24/쌍 max-min ratio≤1.25를 정했다. 장비 격리·유의성 보장으로 사용하지 않는다.6회 시도/5회 통과 후 기준 코드의 API2초 timeout1건으로 외부 검증이 실패해 중단했고 나머지12회는 실행하지 않았다. 측정 load137.56~220.67·저부하 통과0회로3쌍 중앙값을 만들지 않는다.

개별 API 단일/목록 p99는 동일 바이너리 대조1이5.400/6.845→5.319/7.758ms였다. 변경 전후1은13.368/13.815→4.461/11.338ms였으나 다음 후보는268.229/192.761ms로 증가했다. 이후 기준은674.423/713.624ms와 timeout1건으로 유효한 쌍이 아니다. 이를 변경의 무해함이나 코드 단독 회귀로 확정하지 않는다.

후보2의268.229ms 단일 요청은 같은 요청의 서버7.058ms·획득6.280ms·SQL0.180ms여서 서버 밖 대기도 컸다. 실패 기준2의 목록은2074.453ms·서버1866.161ms·획득55.333ms·SQL170.805ms로 나머지 서버 구간도약1640ms였다. SQL span에는 프로토콜/행 소비/스케줄도 있어 서버 실행 시간과 같지 않다. 별도 trace/서버 timing이 없어 순수 CPU/network/메모리/디스크로 더 분리하지 않는다. 경로별약260표본의 p99는 대략 세 번째로 느린 요청에 해당했으며 최대값만으로 p99 방향을 설명하지 않는다.

시도6회는130 capture/validate/ACK·260거래/receipt·2080로그·2470 digest와 SQL1/1·물리5/s envelope를 유지했다. API3178건 중1건 timeout으로 오류0 조건은 실패다. Go 측정 함수 PASS와 외부 API 검증 성공을 구분한다. 이전407 race 검사 수를 이번 성공 검사 수에 넣지 않았고 Go 실행 코드/config/deps·fresh/회전/API/DB/RPC는 그대로다.

이번 코드 변경은 placement 측정 도구의 선택적 `--max-host-load`다. startup의 Docker 접근/자원 생성 전과 각 실행 전후에 부하를 확인한다. host-load.json·부분 runs.json·원본 log를 남기며 기존 소유 label cleanup을 유지한다. 생략 시 제한 없이 부하만 기록한다. AST/help·잘못된 인자4종·실제 과부하 startup 중단을 확인했고 마지막 검사는 host-load.json만 만들었다. 전후 확인은 순간별 부하나 장비 격리를 보장하지 않는다. 별도 gate가 있는 저부하 재시도 계획은 준비했지만 기준 미달로 시작하지 않았다.

근거는 `/private/tmp/indexer-api-recheck-20261017`의 attempted-runs/tail-requests/incomplete-audit/protocol/host-load/guard-validation JSON·원본 log·script diff와 [상세 분석](../SOURCE_FACTS.md#누적-api-재검증과-호스트-부하-확인-2026-10-17)이다. 미시작 계획은 `/private/tmp/indexer-api-recheck-lowload-20261017/not-started.json`이다. 소유 app/DB/network/named volume과 바이너리2개125168526bytes를 정리했고 기존12개 컨테이너 상태를 유지했다. 미시작 바이너리2개도125168526bytes 정리했다. 다음 조건은 다른 CPU/메모리 집중 작업 종료 뒤 안정된 호스트이며 같은3쌍으로 API 회귀 재검증 후 capture SQL 묶음을 평가한다. timeout/RPC/fresh 기준을 완화하지 않았고3.73 성능 수용·실제 peer/mount/장기 SLO는 미완료다.

22:49~22:54 KST 사용자 요청으로 호스트를 재확인했다. 첫1분 load23.27·별도 CPU idle47.56% 표본 뒤 빌드를 마치고22:51:16~22:54:16의180초 동안5초 간격37표본을 기록했다. load27.96~119.23·마지막117.29였고 연속60초≤24/비율≤1.25 기준은 충족하지 않았다. 누적128블록·3쌍의 동일 바이너리는 준비했지만 측정0회·Docker 자원 생성0회로 종료하고2개125168526bytes를 제거했다. 보호58 source/config/deps 파일 해시와 DB4/RPC5/commit fresh는 그대로다. 새 API 성능 수치나 추가 회귀 검사 성공 수를 만들지 않는다. 근거는 `/private/tmp/indexer-backlog-recheck-20261017/preflight.json`, `host-summary-preflight.txt`, `final-audit.json`과 [안정성 재확인](../SOURCE_FACTS.md#호스트-안정성-재확인-2026-10-17-22492254-kst)이다. 저부하3쌍 검증은 계속 미완료다.

### 3.75 호스트 부하 원인과 고아 CI 조회 정리 (10/17)

사용자 요청으로 프로세스 CPU/메모리·부모·실행 파일·cwd/입출력과 Docker 소유 기록을 대조했다. 시스템 fseventsd는약96~99% CPU/메모리 비중9~11%, Finder약95%, WindowServer45~66% 표본이었다. 별도 digital-tennis Unity editor와 하위 프로세스는합계약210%, consensus의 WBFT native 동기화는39~73%였고 별도 WBFT Go 링크 작업도 있었다. 약66.7초에 메모리 압축5.40GiB/해제7.37GiB·압축기24.83~25.89GiB를 관측했으며 swap-out 증가는0이었다. 프로세스별 CPU 표본을 호스트 load 전체의 정량적 원인 배분이나 파일시스템/메모리 누수의 확정으로 쓰지 않는다.

indexer cwd의 고아 쉘76116은 과거 Claude job으로5일 넘게 커밋7a919e9 CI를20초마다 조회하고 있었다. 짧은 SHA 조회는 빈 결과였고 전체 SHA에서는10/12 KST 성공 완료였다. 현재 수집/writer 누수로 기록하지 않는다. PID/시작 시각/명령/자식 종류를 재확인하고 쉘76116과 sleep39614에 SIGTERM을 보내 둘 다 종료했다. CPU가 거의0%여서 큰 부하 감소 효과는 기대하지 않는다. 다른 프로젝트/시스템 프로세스는 유지했다.

추가 indexer 실행 파일은 발견되지 않았고 과거 소유9기록과 전체 Docker 대조에서도 소유 컨테이너0이었다. 기존12개 name/state/StartedAt은 같았다. Go 실행 코드/측정 도구/config/deps 보호58파일도 같은 해시다. DB4/예약1·5 HTTP/s/burst1·commit fresh·API SQL1회·raw/barrier를 유지한 동일128+2warm/3 alternating pairs를 준비했으나23:05:02~23:08:02의37표본 load73.75~196.89·마지막188.58로 안정 기준을 충족하지 못했다. 측정0회·Docker 자원 생성0회로 끝냈고 임시 바이너리2개125168526bytes를 제거했다.

근거는 `/private/tmp/indexer-host-audit-20261017`의 final-audit/process-cleanup/external-ownership/memory-summary JSON·소유/프로세스 표본과 measurement/preflight·final-audit JSON, [상세 분석](../SOURCE_FACTS.md#호스트-부하-원인과-고아-ci-조회-정리-2026-10-17)이다. API3쌍/3.73 성능 수용은 미완료이며 다음 조건은 별도 CPU/메모리 집중 작업 종료 후 안정된 호스트다. timeout/RPC/fresh 조건을 완화하지 않았고 새 API 수치나 Go 회귀 검사 성공 수를 만들지 않았다.

23:12~23:15 KST 후속 요청에 따라 다른 프로젝트를 유지하고 부하만 재확인했다. 약181초/37표본의1분 load184.75~293.78·마지막293.78로 기존 안정 조건은 충족하지 않았다. 빌드0·누적 API 측정0·Docker 자원 생성0회이며 다른 프로젝트에 종료/수정 명령을 내리지 않았다. 보호58파일과 DB4/RPC5/commit fresh는 그대로다. `/private/tmp/indexer-backlog-stability-20261017/preflight.json`, `final-audit.json`을 보존했고 API3쌍 검증은 계속 미완료다.

### 3.76 누적 API 비교 준비와 최종 호스트 안정성 확인 (10/18)

다른 프로젝트를 유지하고 다시 확인했다. 시작 load24.74가 낮아져 비교 바이너리 준비 후00:09:43 KST에 마지막60초14.04~16.96·비율1.208로 기존 기준을 잠시 통과했다. 빌드 중 관찰은 별도 보존했고 두 바이너리 SHA는3.73 변경 전후와 같다. 소유 app/PG16/fsync-on·network/named volume·진단 off를 준비했지만 첫 실행 전 guard에서 load35로 중단됐다. 아직 benchmark 블록은0개였고 해당 프로토콜/로그를 보존했다.

모든 준비 후00:11:36~00:14:36의180초/37표본은load30.15~82.39·마지막42.67·마지막 구간비율1.93으로 다시 안정 기준에 미달했다. 본128+2warm/3쌍 측정은0회이며 API percentile/개선율을 만들지 않는다. 준비의 잔여 OS 작업과 외부 작업의 영향은 분리하지 않았다. 보호58파일·DB4/예약1·5 HTTP/s/burst1·commit fresh·API SQL1·raw/barrier는 그대로다. 새 Go 회귀 검사 성공 수도 없다.

소유 app/DB/network/named volume과 바이너리2개125168526bytes를 정리했고 기존12개 컨테이너 상태를 보존했다. 다른 프로젝트에 종료/수정 명령은 없었다. 근거는 `/private/tmp/indexer-backlog-stability-20261018`의 final-audit/screen/comparison-builds/cleanup JSON·before-final-screen protocol/guard log와 [상세 기록](../SOURCE_FACTS.md#누적-api-비교-준비와-최종-호스트-안정성-확인-2026-10-18)이다.5분 관찰 연장 보조 스크립트는 미실행이다. API3쌍/3.73 성능 수용은 계속 미완료이며 최종 준비 후 안정 상태에서 재검증해야 한다.

### 3.77 모든 측정 준비 후 호스트 부하 재확인 (10/18)

사용자 요청대로 빌드·3.73 변경 전후 SHA 일치·소유 Docker 환경·바이너리 복사·PG fsync-on/진단 off 확인을00:21:11 KST에 모두 마치고 부하를 관찰했다.00:21:22~00:24:22의180.26초/37표본에서1분 load35.97~145.08·마지막132.45였다. 마지막60초 비율1.142는 충족했지만 부하 상한24를 계속 넘었으므로128+2warm/3 alternating pairs는0회다. 관찰 중 추가 빌드·DB 준비·benchmark는 없었고 새 API 수치/Go 회귀 성공 수도 없다.

다른 프로젝트에 종료/수정 명령을 내리지 않았고 기존12개 컨테이너 name/state/StartedAt을 보존했다. 보호58파일·branch·DB4/예약1·5 HTTP/s/burst1·commit fresh·API SQL1·raw/barrier는 그대로다. 소유 app/DB/network/named volume과 바이너리2개125168526bytes를 정리했다. 근거는 `/private/tmp/indexer-prepared-recheck-20261018/final-audit.json` 및 [준비 완료 후 관찰 기록](../SOURCE_FACTS.md#모든-측정-준비-후-호스트-부하-재확인-2026-10-18)이다. API3쌍/3.73 성능 수용은 계속 미완료다.

후속 재확인은00:31:19 KST에 빌드/SHA·소유 Docker·복사·PG 설정까지 모두 끝내고00:31:23~00:34:23에180.24초/37표본을 관찰했다. load257.92~351.68·마지막257.92·마지막60초 비율1.276으로 안정 기준을 충족하지 않아128블록·3쌍은0회다. 새 API/Go 회귀 성공 수는 없다. 다른 프로젝트와 기존12개 컨테이너 상태·보호58파일·DB4/예약1·RPC5/commit fresh/API SQL1/raw barrier를 유지했다. 소유4종 Docker 자원과 바이너리2개125168526bytes를 정리했고 `/private/tmp/indexer-prepared-recheck-20261018-0030/final-audit.json` 및 준비/screen/정리 원본을 보존했다.

### 3.78 기존 API 로그와 코드의 원인 검토·수정 제안 (10/18)

사용자가 추가 측정을 금지하고 원인·수정안을 먼저 요청했다. 빌드/테스트/벤치마크/호스트 관찰/Docker·DB·RPC 조작 없이3.73의12회10,834 API와3.74의6회3,178 API를 원본18개 log와 대조했다. 새18회를 실행한 것이 아니다. SQL은 모두1회/요청이며3.73 오류0·3.74 timeout1이다. 목록 API가 세 쌍 모두 증가한 기존 결과를 유지하며 코드 인과는 미확정이다.

같은 p99 요청을 나누면3.74 후보2 단일268.229ms는 handler7.058/밖 잔여261.170ms, 목록192.761ms는 admission130.959/SQL span27.490/나머지 handler33.698ms였다. timeout2074.453ms의 handler DB 외 잔여는1640.023ms다. `AllMS`에는 클라이언트 JSON/fixture 검증이 포함되고 API client/server는 같은 Go process의 loopback이다. `QueryMS`도 순수 DB 실행이 아니므로 잔여를 외부 네트워크·DB·CPU 하나로 단정하지 않는다.

코드에서 기본 제한 검사와 실제 GraphQL 실행이 요청/AST를 중복 파싱하고 `Pretty: true`로 응답을 꾸미는 추가 작업은 확인했다. 이들이 큰 tail의 주원인인지는 단계별 자료가 없어 미확정이다.1ms 예약 연결 wake 후보는3.65에서 실제 API 악화로 제외된 안이므로 재적용하지 않는다. 원본 회전의 간접 영향·호스트/GC/VM 대기 기여도도 분리할 수 없다.

먼저 공유할 안은①HTTP/클라이언트 검증·handler 단계·admission 분기 구분과 API 오류 후 PASS 오해 방지②제한/표준 validation·취소·요청/오류/extension 계약을 보존한 요청 내 단일 파싱③별도 작은 compact JSON 변경이다. **아직 구현하지 않았다.** 새 pool/연결·cache·fresh/RPC/timeout/barrier 완화나 근거 없는 DB 요약 schema 확장은 제외한다. DB4/예약1·5 HTTP/s/burst1·커밋 직전 새 fresh와 post-RPC epoch/transaction·다른 프로젝트를 유지했다.

[사실·가설·수정 범위](../SOURCE_FACTS.md#기존-api-로그와-코드의-원인-검토수정-제안-2026-10-18)와 `/private/tmp/indexer-api-static-review-20261018/request-review.json`에 원본 SHA·요청별 수치를 기록했다. 문서 외 실행 소스는 수정하지 않았고 보호58파일과 읽은 코드15파일 해시를 대조했다. 사용자 요청 없이 추가 측정을 실행하지 않는다.

### 3.79 API 시간 구분과 오류 판정 구현 (10/18)

사용자 요청으로3.78의1번만 구현했다. 클라이언트 준비/HTTP/JSON·fixture 검증, opt-in GraphQL 제한/준비/parse/validation/execution/response, DB block decode와 예약/차용 경로·입장 시도 수를 구분한다. 기존 전체시간과 acquire/query 의미를 유지하고 중첩 단계임을 표시했다. 일반 API는 기존 handler/schema로 동작하며 diagnostic context가 없는 요청에는 추가 extension callback/clock이 없다. 미완료는 flag와 제외 표본 수로, 겹친/음수 잔여는 null/별도 수로 구분한다.

측정 요청의 모든 API 오류·handler 미완료와 늦게 시작한 제외 요청의 오류를 verdict에 포함한다. 원본 JSON을 먼저 출력하고 Go fixture를 실패시킨다. runner는 실패한 subprocess의 JSON도 `.failed.json`에 보존하고 PASS가 있어도 API 오류/미완료면 거부한다. 파싱 불가/누락은 실패 사유와 raw log로 남긴다. 늦은 callback 뒤 snapshot map의 경쟁을 방지했다.

실제 DB/노드/HTTP server 없이 mock/메모리 transport/취소·종료 경로의 대상3 Go 패키지 기능 검사·최종 GOMAXPROCS1/race와 Python 결과 판정4검사를 통과했다. 범위 감사는 `/private/tmp/indexer-api-diagnostics-20261018/final-audit.json`에 기록했다. DB 예약/차용 성공 assertion은 추가했지만 DB 통합 실행은 하지 않았다. 추가 부하 측정·호스트 관찰·Docker/DB/RPC 조작은0이며 다른 프로젝트를 건드리지 않았다. DB4/예약1·RPC5/burst1·commit fresh·raw/barrier·SQL 바인딩·설정/의존성은 유지했다. 중복 파싱/Pretty 변경은 아직 미구현이고 성능 효과는 미확인이다. [변경 내용·시간 의미·검증 범위](../SOURCE_FACTS.md#api-시간-구분과-오류-판정-구현-2026-10-18)를 따른다.

### 3.80 GraphQL 요청의 단일 파싱 구현 (10/18)

사용자 요청으로2번을 구현했다. HTTP 옵션을 한 번 해석하고 request별 AST를 제한 검사/전체 표준 validation/기존 cancellable executor에 재사용한다. QueryCost/오류 code·operationName/변수·fragment/alias와 원래 context를 유지하며 AST/result cache·의존성 변경은 없다.3번 Pretty 변경과 REST 고정 문서 Execute/Validate helper는 그대로다. HTML 렌더링이 재실행 경로를 만들지 않도록 pinned adapter renderer를 MIT 고지와 함께 분리하고 template/협상/escaping을 유지했다.

제한 on/off 각20종의 기존 adapter와 status/header/body 호환 비교40개, depth/complexity/표준 오류의 resolver 실행 차단, 실행 전/중 취소와 context 값·본문 오류를 기능 검사했다. 순환 fragment 검사가 기존 검증기 내부의 무한 재귀/stack overflow를 드러내 같은 AST의 공식 NoFragmentCyclesRule을 먼저 적용하고 정상 fragment 그래프에는 전체 규칙을 유지했다. 자기/상호 순환과 제한 off도 오류로 거절한다.

공유 Go cache 누락/std 조회 오류·분리 과정의 import 누락·검증기 stack overflow 로그를 보존했다. 다른 프로젝트 환경/cache를 삭제하지 않고 전용 GOCACHE로 검사했으며 최종 GraphQL·REST 기능 검사는 통과했다. 전체 race는 기존 WebSocket `TestKeepAlive/answering_client_stays`의200ms 시간 조건에서 실패했고 원인은 미확정이다. 이 별도 경로와 조건을 바꾸지 않았으며 전체 race 통과로 표기하지 않는다. 데이터 경쟁 경고는 없었고, 변경한 HTTP 경로의 별도 race·REST race는 통과했다. 실패/성공 원본·보호 소스·작업 전용 cache 정리 결과는 `/private/tmp/indexer-graphql-single-parse-20261018/final-audit.json`에 기록했다. 추가 성능 측정·호스트 부하 관찰·Docker/실제 DB/공개 RPC 조작은0이다. DB4/예약1·RPC5/burst1·커밋 직전 fresh/post-RPC epoch/transaction·raw/SQL 바인딩·설정/의존성과 다른 프로젝트를 유지했다. [변경·호환·실패와 보완](../SOURCE_FACTS.md#graphql-요청의-단일-파싱-구현-2026-10-18)을 따른다. 처리 속도 개선/SLO 검증은 미완료이며 새 수치를 만들지 않는다.

### 3.81 GraphQL JSON 꾸밈 출력 제거 (10/18)

사용자 요청으로3번을 구현했다. GraphQL HTTP의 MarshalIndent를 Marshal로 바꾸고, 기존 Pretty adapter에서 꾸밈 공백만 제거한 JSON과 응답을 비교한다. 정상 데이터+resolver 오류가 함께 있는 경우를 추가해 제한 on/off42개 호환 비교와 GraphQL·REST 기능 검사를1회 실행, 모두 통과했다. HTTP status/header·JSON 값과 오류 의미·Playground HTML·단일 파싱/제한/표준 검증/취소는 유지했다. 추가 성능 측정·호스트 관찰·Docker/실제 DB/공개 RPC 조작은0이고 DB4/예약1·RPC5/burst1·commit fresh/postepoch/raw/SQL/설정/의존성과 다른 프로젝트는 그대로다.135개 보호 파일 중 request.go/request_test.go만 변경됐으며 production 변경은 인코딩1줄이다. 이번 race는 미실행이며 이전 keepalive race 실패 원인은 미확정이다. 작업 전용 cache만 정리하고 성공 로그·diff/범위 감사는 `/private/tmp/indexer-graphql-compact-json-20261018/final-audit.json`에 보존했다. [변경·검증 범위](../SOURCE_FACTS.md#graphql-json-꾸밈-출력-제거-2026-10-18)를 따른다.1~3번은 구현했지만 처리 속도/p99/SLO 효과를 새로 측정하거나 확정하지 않았다.

### 3.82 API 변경1~3 종합 검토 (10/18)

사용자 요청으로 현재 코드/기존3.78 분석·3.79~3.81 검사 원본을 종합 검토했다.1~3 구현/대상 기능 검사는 완료이며 서로 연결돼 있다. 최종42개 호환/GraphQL·REST 기능은 통과했으나3.80 전체 race의 별도 keepalive 실패와 실제 DB 진단 통합 검증은 미완료다. 큰 API tail은 기존 요청별로 handler 밖/입장/SQL 왕복/미분리 handler에 분산되고 단일 원인·호스트/metadata 변경의 인과·성능 개선율은 미확정이다. 다음 순서는 keepalive 실패 원인 분석→실제 DB 통합 기능 검증→사용자 재개 요청 전 성능 검증 보류→근거 있는 구간만 최적화다. 이번 빌드/검사/성능 측정/호스트 관찰/Docker/실제 DB/RPC 조작은0이며 실행 코드/DB4/예약1/RPC5/burst1/commit fresh/다른 프로젝트를 유지했다. 문서만 갱신했고 [종합 근거와 우선순위](../SOURCE_FACTS.md#api-변경13-종합-검토와-남은-우선순위-2026-10-18) 및 `/private/tmp/indexer-api-integrated-review-20261018/final-audit.json`에 기록했다.

### 3.83 WebSocket keepalive 검사 안정화 (10/18)

기존 실패는 DATA RACE 경고 없는 연결 생존 assertion이었다.200ms 만료·180ms ping은 pong 수락에20ms만 남기며 client callback 계수는 server deadline 갱신을 증명하지 못한다. 당시 오류가 bool/Nop logger로 사라져 정확한 소켓 종료/스케줄링 원인은 미확정이다. 테스트만 수정해 실제 제어프레임과 server deadline 갱신은 OS 타이머를 분리해 관찰하고,무응답 실제200ms 만료·명시적 read 만료 뒤 늦은 pong·비활성 동작/reader join을 따로 검사했다. production60s/54s와 TTL·DB4/예약1/RPC5/burst1/commit fresh/raw/SQL/설정/의존성/다른 프로젝트는 그대로다. GOMAXPROCS1/race keepalive10회·40하위경우 및 GOMAXPROCS2/race GraphQL/REST 패키지1회 모두 통과했고 goleak/경쟁 경고는 없다. 전체 repository race/과거 정확 원인 규명/성능 개선 증거로 표기하지 않는다. 추가 성능 측정/호스트 관찰/Docker/실제 DB/RPC 조작은0이다. 원본·diff/141파일 해시/audit와 후속 검사 재사용용 private Go cache를 `/private/tmp/indexer-keepalive-functional-20261018`에 보존했다. [변경과 한계](../SOURCE_FACTS.md#websocket-keepalive-검사-안정화-2026-10-18)를 따른다.

### 3.84 실제 DB에서 API 변경1~3 통합 기능 검증 (10/18)

라벨 있는 임시 PostgreSQL/volume과 pool4/예약1로 finite HTTP8개 및 실제 adapter 취소/반납을 검사했다. pinned/borrowed,background3개 점유 중 취소 후 pin 재사용,SQL lock 대기 확인 뒤 취소/오류 정규화/복구,단일/목록 SQL1/decode/GraphQL 단계와 compact JSON,검증·깊이 제한 SQL0·실제 오류 verdict 실패/원본 보존을 통과했다. 첫 app fixture의 초기 배포1거래/기대2거래 오류는 명시적2거래 데이터로 수정했으며 validator는 유지했다. 초기 저장소/API PASS+app fixture FAIL·최종 app PASS 원본을 모두 보존했다. 최종 소스의 대상10 Go 검사 GOMAXPROCS1/race·Python4·실제 DB verdict 거절 검사를 통과했다. 기존 측정 fixture와 새 functional이 공통 test-only HTTP 진단 wrapper를 쓰도록 helper2파일을 보완하고 새 test2파일을 추가했으며 production 수정은 없다. 실제 부하/처리량/p99/host screen/공개 RPC는0,DB4/RPC5/commit fresh/raw/SQL/설정/의존성·다른 프로젝트 유지다. 소유 DB와volume만 제거했고 기존12개 상태/StartedAt은 같았다. `/private/tmp/indexer-db-api-integration-20261018/final-audit.json`과 [상세 범위/한계](../SOURCE_FACTS.md#실제-db에서-api-변경13-통합-기능-검증-2026-10-18)를 따른다. 전체 repository/200회/성능 인과 검증은 미실행이며 API tail 효과는 미확정이다.

### 3.85 API 변경1~3 및 실제 DB 검증 최종 정리 (10/18)

새 실행 없이 현재143개 소스와3.79~3.84 기능/race/DB/parser/정리 원본을 대조했다. DB 검증 후 해시는 모두 같고 추가 확정 결함은 찾지 못했다.1~3·keepalive·실제 DB 통합은 완료로 마감하고 open-items의 과거 현재 표기를 이력으로 바꿔 완료 항목을 반복 제안하지 않도록 했다. 남은 순서는 변경 확정/PR 전 전체 회귀/정적/race/example 및 push 시 새 동시성200회 규칙,사용자 재개 요청 전 성능 인과/효과 검증 보류,증거 있는 구간만 후속 최적화다. 대상10검사·42호환·package race는 repository 전체/성능/SLO 완료와 다르다. 이번 코드/빌드/검사/측정/host/Docker/DB/RPC 조작은0이고 다른 프로젝트·DB4/예약1/RPC5/commit fresh/raw/SQL/설정/의존성을 유지했다. [최종 범위와 우선순위](../SOURCE_FACTS.md#api-변경13-및-실제-db-검증-최종-정리-2026-10-18), `/private/tmp/indexer-api-final-review-20261018/final-audit.json`에 기록했다.

### 3.86 PR 전 전체 회귀 검증 (10/18)

추가 성능 측정 없이 전체 기능·정적·race·DB·예제와 Rust/IPC를 검증했다. Go1.24.9 전체 기능/race는 각각75개 package·상위2279개(하위 포함4569개) 통과, 상위81개 skip. 실제1만 구독자 p99 측정인 TestEngineLoad만 명시적 제외하고 측정/SLO/live opt-in은 모두 껐다. 전체 build/vet/gofmt/lint, 예제2개 race/vet/lint, astgraph vet/lint/race 빌드, Pebble invariants lifecycle, Python4/셸4, Rust unit2/reth 타입/fixture 빌드와 Go IPC11개 race도 통과했다. 실제 node 요구 integration 태그는 컴파일만 확인했다.

코드는 test-only3개: app DB 공통/driver 비교 fixture8→4, QF1011 중복 타입 선언1개 정리. Production·DB4/예약1·RPC5/burst1·커밋 직전 fresh/postepoch/transaction fence·SQL binding/raw/설정/의존성은 유지했다. 초기 Go1.24/GOROOT1.26 혼재는 실행 환경을 바로잡아 해결했고 검사 원본을 남겼다.

PostgreSQL16/fsync=on/Asia-Seoul의 DB 분기9개 package race 원본은8개 package PASS, app의 선택적 결과 파일 출력 디렉터리를 runner가 준비하지 않아1건 FAIL(exit1)이다. 다른 상위368개는 통과했다. 경로 준비 후 같은 소스의 해당1개만 race 재검사해 통과했으며 전체 PG를 반복하지 않았다. 최종 기능 근거는369개 통과의 합집합이고 최초 전체 명령이 exit0이었다고 표시하지 않는다. 실제 verdict는8요청/의도 오류4개/미완료0/Passed=false. 운영 코드/판정 기준 수정은 없었다.

소유 DB/container·volume/DSN을 정리하고 기존12개 container 상태/StartedAt 보존을 확인했다. 전용 Rust target은 정리하고 Go cache/toolchain은 보존한다. 추가 성능 측정·host screen·공개 RPC0, 다른 프로젝트 조작0. [전체 결과·제외·실패 해결 근거](../SOURCE_FACTS.md#pr-전-전체-회귀-검증-2026-10-18), `/private/tmp/indexer-pr-regression-20261018/{regression-audit,resolution,cleanup,build-cleanup}.json`을 따른다. PR 전 비성능 전체 회귀는 마감하며 새 변경/실패 없이 반복 제안하지 않는다. push 시 새 동시성200회 규칙·실제 peer/mount는 별도이고 API tail 효과/SLO는 사용자 재개 전 보류한다.

### 3.87 Push 전 API 동시성 200회 검증 (10/18)

성능 측정 없이 남은 API/연결 수명주기18개를 Go1.24.9/GOMAXPROCS1/race/각 단일-count200으로 완료했다. 상위3600회/하위 포함8000회 PASS, 최종 실패·skip·DATA RACE0. GraphQL3/snapshot1/수정 SQL 취소1/나머지DB11/HTTP1/통합1이며 실제 DB는 PostgreSQL16/fsync=on/pool4/예약1이다. 이전 수집/RPC200 기록을 이번 횟수에 합산하지 않았다.

최초 DB 묶음123번째 SQL row 취소 검사에서 재조회 성공 뒤 AcquiredConns2/기대1의 즉시 통계 assertion이 실패했다. pgx/puddle은 닫힌 pin 폐기를 비동기로 끝낸 뒤 통계에서 제거한다. test-only로 동일1개 조건의 최대1초 Eventually를 사용해 실제 반납 완료를 확인하고 수정본 전체 exec/row/query/stream200회를 통과했다. SQL 취소/오류/재조회/최대4 조건과 운영 IO는 유지했다. 최초 중단 기록과78회 합산용 부분 실행은 모두 최종 수에서 제외했고, 나머지11개도 단일200회 실행으로 완료했다. 온전히 완료한5개 검사는 반복하지 않았다.

초기화용 PostgreSQL 임시 서버가 실제 DB 생성 전에 ready를 반환한 것도 원본에서 확인했다. test-postgres.sh는 최종 TCP/test DB의 SELECT1 성공을 확인하고 bounded startup 실패를 명시한다. 실제 스크립트/진단/race1회 및 소유 자원 정리를 확인했으며 그1회는3600에 미산입했다. 코드 변경은 테스트1개/검사 스크립트1개뿐, package vet/gofmt·shell/diff 검사를 통과했다.

운영 소스/설정/의존성·DB4/예약1/RPC5/burst1/커밋 fresh/postepoch/fences/raw/SQL binding·다른 프로젝트를 보존했다. 새 성능/host screen/공개 RPC0, Git 변경 명령0. 소유 DB/volume/DSN 제거·기존12개 container 상태/StartedAt 보존을 확인했다. [범위·실패·수정·단일200회 근거](../SOURCE_FACTS.md#push-전-api-동시성-200회-검증-2026-10-18), `/private/tmp/indexer-concurrency200-20261018/{verification,final-audit,failure-analysis,single-invocation-correction,script-functional,cleanup}.json`을 따른다. 이 API push 전200회 항목은 마감하며 같은 소스를 이유 없이 반복 제안하지 않는다. 성능 인과/SLO·실제 peer/mount는 기존 범위대로 보류/미확정이다.


### 3.88 누적 변경 PR 제출 준비 (10/19)

사용자가 커밋·PR 진행과 squash merge를 요청했다. `refactor/arc-node-feed`를 유지하고 누적 window/feed·원천 저장·독립 후처리·API/DB 읽기 변경을 제출 범위로 정리했다. origin/main과의 차이에는 기존 StableNet ancestor RPC 개선 커밋도 포함한다. PR 설명은 기능/안전 경계와 실제 운영 미검증 범위를 구분한다. 기존3.86/3.87 원본 해시와 현재 파일을 대조했으며 차이는 해당 검증에서 기록한 test-only/시험 스크립트 수정과 문서다. 이번 준비에서 운영 Go 코드·설정·의존성을 수정하지 않았고 추가 성능 측정이나 로컬 전체 회귀/200회 반복도 실행하지 않았다.

open-items의 날짜·제출 범위와 R08을 최신 완료 결과에 맞췄다. 다른 프로젝트·DB4/예약1·RPC5 HTTP/s·커밋 직전 fresh/post-RPC epoch/transaction fence·raw 내구성·SQL 바인딩은 유지한다. GitHub main 보호 규칙은 lint/test/postgres/examples 네 CI를 요구하며 관리자에게도 적용된다. 필수 CI를 통과한 뒤 squash로 병합하고 우회 옵션이나 force push를 사용하지 않는다. 실제 Arc peer/외장 mount/24시간 운전·독립 CL certificate·API tail 인과와 SLO는 계속 미검증/보류다.


### 3.89 PR CI의 PostgreSQL 내구성 설정 불일치 수정 (10/19)

PR [#179](https://github.com/0xmhha/indexer-go/pull/179)의 첫 [CI](https://github.com/0xmhha/indexer-go/actions/runs/39139569852)는 lint/test/examples를 통과했지만 PostgreSQL에서 상위31개 검사가 실패했다. 직접 오류는 `capture: PostgreSQL fsync must be on`이며 네 체인 격리 검사는 Arc 진행 대기로 실패했다. 기존3.86/3.87 실제 DB 검증은 fsync=on으로 실행했지만 공식 시험 스크립트에는 과거 `-c fsync=off`가 남아 있었다. 제출 준비에서 이 실행 조건 불일치를 놓쳤다. 새 원천 저장의 내구성 거부는 정상이며 운영 guard를 완화하지 않는다.

수정은 `scripts/test-postgres.sh`의 임시 DB 시작 옵션을 `fsync=on`으로 바꾸고 이유를 주석/규칙에 기록한 것이다. 운영 Go 코드·설정·의존성·RPC5/fresh/DB4·예약1·raw/SQL 경계는 그대로다. 수정 스크립트 자체로 CaptureBatch2개·ProjectionDurability·ManagedBlobCapabilityReadonlyAndLegacy·ManagedFeedFourChainIsolation의5개를 Go1.24.9/GOMAXPROCS1/race/각1회 검사하여 모두 통과했다. 이는 성능 측정이나200회 재실행이 아니다. 소유 임시 자원은 정리됐고 시작 시 존재한 다른13개 container의 상태/StartedAt은 같았다. 원본과 결과는 `/private/tmp/indexer-pr-submit-20261019/{ci-failed.log,initial-ci-failure.json,fsync-functional.json}`에 보존한다. 수정 커밋의 필수 CI 네 항목을 통과한 뒤 squash merge한다.


### 3.90 개발자 대상 공개 문서 재구성 (10/19)

README를 기존의 세션/작업 상태 순서에서 개발자의 평가·실행·확장 흐름으로 새로 작성했다. 영문 개요·용도·CI/toolchain/license·Anvil 로컬 실행과 GraphQL 조회·SDK/체인 profile/예제 선택·지원/실험 범위·기여/도움·라이선스를 노출한다. 시간별 측정·내부 운영 결정은 첫 안내에 복제하지 않고 상세 문서로 연결한다. README 구성과 기여 문서의 역할은 GitHub의 공식 안내를 참고했다.

문서 목차를 시작/조회/확장/운영/선택 Arc feed로 재편하고 QUICKSTART를 로컬 시작→자기 RPC→저장/역할→문제 해결의 영문 가이드로 다시 썼다. CONTRIBUTING에는 기존 코드/내구성/검증/PR 규칙과 squash merge를 모았다. analysis 목차를 추가해 현재 계약·남은 일·제안·과거 증거를 구분한다. 상세 한국어 문서의 언어를 안내했고 기존 설계/실험 원본은 보존했다. 아키텍처 그림은 RPC와 disk-window/capture/DB facts/독립 projection의 현재 연결을 포함한다. 이미 확인한 제한 구간 멀티체인 검증을 미완료로 적은 부분과 strict 상태 방침/개인 운영 구성이 섞인 설명을 바로잡았다.

문서 링크/앵커·Markdown 렌더·셸 구문·요청 JSON/API snapshot 대조를 검사했다. QUICKSTART의 새 YAML과 기존 Anvil/config.example의3개를 pinned Go1.24.9 실제 config.Load로 읽어 검증했다. 검증 도구는 `/private/tmp/indexer-docs-refresh-20261019/`에만 두며 runtime/RPC/DB를 시작하지 않는다. 추가 성능 측정·로컬 전체 회귀·200회 반복·Docker/노드 실행은 없었다. 변경은 Markdown 문서뿐이며 다른 프로젝트·DB4/예약1·RPC5 HTTP/s·커밋 직전 fresh·운영 설정·의존성은 그대로다.

### 3.91 네 테스트넷 운영 설정 초안 (10/19)

사용자가 권장안을 운영 초안으로 작성하되 포맷/실행/추가 측정은 보류하도록 요청했다. 별도 `docs/OPERATIONS_DRAFT.md`, `docs/examples/config-testnets-windowed.draft.yaml`, `docs/examples/postgresql-testnets.draft.conf`를 작성하고 문서 목차·CONFIG·R03에서 연결했다. 현재 무시 파일 `config.yaml`의 Pebble 설정은 변경하지 않았다.

전용 APFS 후보X31·raw 전체160GiB/Arc128GiB·raw 쓰기 여유100GiB·DB계획160GiB·최소24h와 PostgreSQL16/체인별pool4개(합계16)/서버32개를 구분했다. DB160GiB는 자동 quota가 아니고 raw 여유 검사는 DB 쓰기 전체를 제한하지 않는다. 실제 APFS 확보/기존 자료 보존·DB 접속·enode·genesis/network/fork·finalized anchor와 나머지3체인 시작 높이·허용 중단/복구 시간은 미확정으로 남겼다. Docker 호스트/컨테이너 경로와 아직 없는 기동 전 mount 검사/조립을 표시했다. Arc만 execution-only로 상태 기능을 끄고, 다른3체인은 strict RPC를 유지한다. 초기화·GC는false이며 RPC-finalized authority와 후속 기능 확인/GC 전환 순서를 기록했다. 모든5 HTTP/s·commit fresh/epoch·내구성·ACK/삭제 fence는 그대로다. 별도 영구 객체 YAML이나 추가 proof/trace/worker는 켜지 않는다.

중복 YAML key·11개 placeholder·보호값·용량/경로·PostgreSQL 조각·링크 대상을 점검했다. 별도 임시 검사기의 Go1.24.9 실제 config.Load가 원본 초안을 typed decode 단계에서 거부했고, 가상 높이/해시/문서용IP enode를 넣은 임시본은 strict known-fields·설정/Arc module/window 옵션 순수 검증을 통과했다. 가상 값을 실제 값으로 제시하지 않았고 App/feed.Open·RPC·DB·mount 초기화는 호출하지 않았다. 공유 Go 캐시 접근 제한은 소유 임시 캐시로 해결했으며 저장소 코드는 수정하지 않았다. 검토/가상 fixture 기록은 `/private/tmp/indexer-operations-draft-20261019/`에 남기고 검사기 소유 캐시는 정리했다. config.yaml·runtime/config/deps 등1094개 보호 파일 해시가 일치하며 다른 프로젝트·Docker 자원·디스크 포맷·추가 성능/전체 회귀/200회 반복·commit/push/PR 작업은 없다.

### 3.92 Arc genesis·network ID 확인과 fork 후보 분리 (10/19)

사용자 요청으로 미확정 네트워크 값을 읽기 전용 확인했다. 기존 공개 Arc endpoint에 `eth_chainId/net_version/block0/latest`의1배치와 `web3_clientVersion/arc_getVersion`의1배치, 총HTTP2회·method6개를 보냈다. HTTP5회/s를 넘지 않았고 failover/재시도·처리량/지연 측정은 없다. chain/network5042002, genesis hash `0xe20e653af4441e8c6088e172b129d56420139824400477287b46e7101ae2bb1f`를 확인했다. genesis hash는 공식 감사 commit6e764023의 testnet chainspec assertion과, timestamp는 genesis JSON과 각각 일치했다. genesis/network placeholder2개만 초안에 반영해 미확정 표시는9개가 남는다. 최신 관측 head67,881,454는 fork 계산에만 사용했으며 finalized/운영 checkpoint를 선택하지 않았다.

공식 main은 기존 감사 commit6e764023과 같았다. built-in testnet은 genesis JSON 이후 별도 Arc hardfork 일정을 사용한다. Zero3/4의 블록11172019/26148086, Osaka·Zero5/6·Zero7·Zero8의 timestamp1779890400/1779894517/1781791200/1788447600을 사용하고 동시 Zero5/6은1회만 CRC32에 넣었다. Reth의 EIP-2124/6122 계산 규칙과 표준 벡터·독립 bitwise CRC를 대조해 공개 소스 기준 기대값`910609e1/next0`을 계산했다.

RPC는 `v0.8.1-hotfix.rc1`, 별도 commit`6ede3f18a211315078909e9d3b5be1f6608fe9e8`을 보고했다. 이 commit의 공식 genesis/chainspec/hardfork raw 경로3개가404여서 배포 소스 일정이 감사 소스와 같다고 확인할 수 없었다. 실제 enode/peer status도 없으므로 fork YAML placeholder는 유지했다. [identity 근거](arc-testnet-identity.md)에 직접 확인/계산 후보/미확인을 나누고 운영 문서·R03을 갱신했다. 원문·source SHA256·계산기는 `/private/tmp/indexer-arc-identity-20261019/`에 보존한다. runtime/config/deps 보호1094개는 그대로이며 디스크 포맷·노드/indexer/DB/Docker 실행·추가 성능/전체 회귀/200회 반복·다른 프로젝트 변경·commit/push는 없다.

### 3.93 Docker 운영 구성·APFS 검사·비공개 DSN 초안 (10/19)

사용자 요청으로 [전용 Docker 초안](../../deployments/testnets/README.md)을 작성했다. 별도 Compose/Dockerfile·미확정 host manifest·호스트 검사기·DSN entrypoint·새 DB용 비관리자 role 초기 SQL과 오프라인 검사를 추가했다. 기존 root Compose/Dockerfile·무시된 config.yaml과 Go 실행 코드는 그대로다. 서비스는 PostgreSQL16과 멀티체인 indexer만이며 전용 project/profile·restart=no·내부 DB network·loopback API를 사용한다. 체인별 pool4개(합계16)·공유 RPC5 HTTP/s와 기존 커밋 직전 fresh/epoch·내구성·ACK/GC 경계를 유지한다.

호스트 검사는 실제 macOS mount/APFS·VolumeUUID·외장 physical-store partition UUID·ownership·여유100GiB·같은 파일시스템의 기존 경로와 비공개 파일 권한을 확인한다. 모든 bind는 create_host_path=false이며 누락 경로를 만들지 않는다. 기본 lint/--check-host는 기동하지 않고, 향후 별도 --start만 전체 검사 후 고정 project/context에 no-build/pull=never로 넘긴다. 실행 보류 중 이 경로를 호출하지 않았다. private directory0700과 소유 regular/single-link 파일0600을 요구하며 DSN은 Compose 환경 정의나 argv에 값으로 넣지 않고 secret 파일에서 기존 프로세스 환경 변수로만 전달한다. 직접 DSN override·불일치 password·미확정 필드를 거부한다. 앱 role은 superuser/createdb/createrole이 없고 전용 DB의 schema 생성 권한만 갖도록 제안했다.

최종 오프라인15개 검사를 통과했다. mount/UUID/물리 디스크/여유 공간·secret 권한/심볼릭 링크·DSN 정책·미확정 값 거부, 검사 후 identity 재확인, 실패 시 기동 차단과 성공 시 고정 dispatch를 fixture/mock으로 확인했다. entrypoint는 임시 가짜 프로그램으로 데이터 그대로 전달·정확한 인자·누락/override 거부를 확인했다. 별도 존재하지 않는 Docker 소켓과 가상 경로로 Compose config --quiet를 통과했으며 셸 구문·diff·문서 링크를 검사했다. 이는 실제 diskutil/APFS/컨테이너 UID/DB 초기 SQL/role grant/peer/24h/450ms 검증이 아니다. raw 초기화·GC는false이며 최초 provisioning은 별도 보류다. 기동 전 UUID 검사는 기동 후 장치 분리 시 PostgreSQL을 멈추는 watchdog이 아니며 실제 디스크 내구성과 Docker Desktop sharing은 미검증이다.

실제 APFS UUID·private 경로/자격 증명·검토한 이미지·enode/peer fork·checkpoint는 미확정으로 남겼다. 포맷·초기화·이미지 build/pull·컨테이너/노드/indexer/DB 실행·공개 RPC·추가 성능/전체 회귀/200회 반복·다른 프로젝트 조작·commit/push는 없다. runtime/config/deps 보호1094개 해시와 현재 파일의 일치를 확인했다. 검사 범위와 한계는 `/private/tmp/indexer-docker-operations-draft-20261019/final-audit.json`에 기록한다.

### 3.94 용량·backlog·복구 정책 초안 (10/19)

사용자 요청으로 [정책 초안](../OPERATIONS_CAPACITY_DRAFT.md)을 작성하고 운영/Docker 안내·문서 목차·R03에서 연결했다. 기존 코드의 raw80/95%·filesystem floor100GiB와 free132/108GiB 압력·수신 capacity 대기/후처리 지속·severity 변경 로그를 확인했다. PostgreSQL160GiB는 강제 quota가 아니며 GC=false에서는 처리 완료만으로 raw가 회수되지 않는다. 내부 manager의 오류 체인 자동 재시작은 최초 Docker APFS 호스트 검사를 다시 거치지 않으므로 capacity/identity 사고 때 이 프로젝트 indexer 전체 중단과 재개 interlock이 필요한 공백을 기록했다.

filesystem free150GiB 경고/120GiB indexer 중단/110GiB 긴급 DB 종료 판단·재개150GiB, PGDATA128/144/160GiB와 raw80/95/재개70%를 최초 제안으로 분리했다. worker 미처리 관측5분/30분·알려진 미처리 중 cursor5분 정지를 경보로 두되 지연만으로 자동 종료하지 않는다. source capture/기존 writer/필수 feature 완료·원격 latest를 구분하고 null/미관측을0으로 만들지 않는다. 현재 base raw 연속 intake/oldest pending/gross append 노출은 없고 선택 telemetry도 운영 YAML에 없음을 확인했다. 증가 예측·관측 누락·재개 hysteresis·초기화/GC/retention 불변과 영구 cursor 복구 조건을 정리했다. RPO/RTO·실제 peer 보존·백업/증설은 미확정이다.

감시 주기·PGDATA/DB/schema/WAL 중복 합산 방지·서버32개 중 관리 단기 연결 최대1개·query 취소/반납·비밀 비노출·SQL 값 바인딩을 제안했다. 앱 pool은 체인별4개 그대로다. PostgreSQL16 공식 크기/디렉터리 함수·disk full·VACUUM 문서를 대조했으며 실제 쿼리는 실행하지 않았다. 감시·원인별 재시작 억제·중단 제어·제안 종료 유예(indexer120초/DB60초)는 미구현으로 표시하고 Compose에는 반영하지 않았다. 다음 문서 작업은 checkpoint/재시작/GC·백업/이전의 상세 runbook이다.

문서 상대 파일 링크147개와 임계값 산식을 정적으로 검사했고 오류가 없었다. 기존 runtime/config/deps1094개와 추가 배포/설정11개 해시가 그대로이며 기존 deployment README만 의도한 문서 링크를 추가했다. 디스크 관찰/포맷·실제 DB/RPC/컨테이너/서비스·추가 성능/기능/전체 회귀/200회 실행·삭제/중단/재개·다른 프로젝트 조작·commit/push는 없다. DB4·RPC5·커밋 직전 fresh/epoch·내구성/ACK/GC를 유지했다. 정책 문서·정적 검토 범위/한계·보호 해시 기록은 `/private/tmp/indexer-capacity-policy-draft-20261019/{static-review,final-audit}.json`에 보존한다. 이 결과는 자동 운영 제어 구현이나 실제 장애 복구 검증의 완료가 아니다.

### 3.95 Checkpoint·재시작·GC 운영 절차 초안 (10/19)

사용자 요청으로 [운영 절차 초안](../OPERATIONS_RUNBOOK_DRAFT.md)을 작성하고 운영 설정·용량 정책·Docker 안내·문서 목차·R03에서 연결했다. 최초 anchor H0/hash와 source 연속 완료 C/세대 G·finalized F·writer P·필수 feature 완료/gap·raw identity·삭제 intent/완료 기록을 분리해 기록한다. 정상 종료는 이 프로젝트 indexer 전체 SIGTERM→task 취소/join/lease 반환·window/DB close 결과 확인→필요할 때 전용 PostgreSQL 정상 종료 순서이며 backlog0이나 stopped 로그 하나를 종료 성공으로 삼지 않는다. 종료 유예120/60초는 제안일 뿐 현재 Compose에는 반영하지 않았다.

재개 전 APFS/용량 정책·같은 DB/schema/plan/anchor/authority/window binding·초기화false를 확인하고 기존 durable cursor로 이어가는 절차와 오류별 중단/재구성 분기를 정리했다. manager 내부 자동 재시작은 host guard 재검사/재개 승인이 아니며, 실제 host guard의100GiB 최소선은 용량 정책의150GiB 재개선과 다르다. 원본 누락/손상·필수 입력 pruned·finalized/깊은 reorg 충돌을 latest/0·새 anchor·수동 cursor 수정으로 우회하지 않는다. projection receipt·raw ACK·연속 완료를 구분하며 writer commit-ACK 사이 중단은 기존 복구 검증 경로를 따른다.

새 GC 전에는 최소24h/정상 시계/sealed·같은 identity·필수 encoded bytes/정규화/영구 객체·canonical/finalized·기존 required supplement·모든 raw-only ACK/lease·durable generation fence·정확한 완료 기록을 확인한다. 코드상 collect=false는 새 Collect를 막지만 과거 승인된 삭제는 앱 생성 중 OpenWindow/OpenChecked/RecoverDeletion에서 완료될 수 있다. 따라서 startup은 읽기 전용 점검이 아니며 이 차이를 기존 운영/용량/Docker 문서에도 반영했다. 현재 preflight는 collect=true를 거부하므로 새 GC 적용은 별도 명시적 변경·검증 대상으로 남겼다. 파일 부재만으로 완료 추정/TTL fence 해제·이전 fork 원본 수동 삭제는 허용하지 않는다.

DB/window 삭제 복구 코드를 대조해 intent만 남음·부분 unlink·완료 기록만 남고 DB fence 미해제·기록 불일치의 네 분기를 정리했다. 전체 checkpoint/PendingDelete/raw-only ACK/lease를 추출하는 읽기 전용 CLI/GC dry-run과 원인별 재시작 억제·중단 controller는 아직 없음을 표시했다. 관련 기존 검사 소스는 근거로 읽었으나 재실행하지 않았다. 백업/복원은 일관된 DB cluster/WAL·raw/삭제 기록·identity/binding 보존 원칙을 제안하고 PostgreSQL16 공식 종료/파일 백업 문서를 대조했다. 실제 보관/이전 방식·RPO/RTO·복원 검증은 미확정/미검증이다.

상대 파일 링크175개·참조 anchor4개·diff를 정적으로 검사했고 오류가 없었다. 보호한 비Markdown1098개와 기존 runtime/config/deps1094개 해시가 모두 일치하며 앱 YAML의 DB4/RPC5/head·초기화/GC false·미확정 checkpoint 값을 유지했다. 포맷·디스크/host 관측·실제 DB/RPC·Docker/service·종료/복구/삭제·추가 성능/기능/전체 회귀/200회 실행·다른 프로젝트 조작·commit/push는0이다. 근거/해시/검토 범위는 `/private/tmp/indexer-checkpoint-runbook-draft-20261019/{static-review,final-audit}.json`에 보존한다. 절차 초안 작성은 실제 정상 종료/장애 복구/GC 운영 검증 완료나 실행 승인으로 표시하지 않는다.


### 3.96 운영 문서·설정의 오프라인 일관성 점검 (10/19)

사용자 요청으로 Docker·용량·checkpoint/정상 종료/재시작/GC 초안을 실제 설정·코드에 대조하고 [점검 결과](operations-offline-review.md)를 작성했다. 기존 host policy가 놓치던 RPC chain별 strict-state override·schema/window/floor·API listener를 보완하고, 공개 Compose/app/PostgreSQL 파일의 DB4/RPC5·secret/entrypoint·image·mount/network·내구성/로그 연결을 검사하는 check_artifacts를 모든 진입 전 적용했다. 두 파일을 같이 잘못 바꿔도 예산을 올리지 못하도록 고정 policy를 먼저 확인한다. 실제 config.yaml과 Go 실행 코드는 수정하지 않았다.

manager의 max_unhealthy_duration은 기본값/전달만 있고 중단 timer로 사용되지 않아 운영 YAML에서 제외하고 CONFIG와 각 운영 문서에 표시했다. Docker 두 서비스에만 local log driver·10m/최대3파일 회전을 추가했으며 Docker Desktop 저장 영역 감시는 외장 APFS free와 별개라고 정리했다. warning과 resume이 같은 값에서 겹치던 경계는 free>150GiB/PGDATA<128GiB로 통일했다.420GiB는 raw160+DB160+floor100의 산술이고 재개 여유까지 계획하면470GiB 초과+관리 여유가 필요함을 보완했다. writer RAM 큐·raw 알림·디스크 backlog, Python3.9+/private manifest0600 조건도 구분했다.

단일 live App/체인일 때의 연결 예산16개와 체인별 pool4/RPC5를 전역 hard cap으로 확대하지 않도록 했다. Stop timeout 뒤 Close 지연은 코드에서 확인했지만 실제 중복 App/예산 초과는 관측·재현하지 않았다. 종료/자원 반납 확인 전 재기동 금지와 미구현 interlock을 표시했다. 기존 GC=false와 pending 허가 복구의 차이·source/feature/ACK/finality/lease/삭제 fence·미확정 checkpoint·실행 보류는 유지했다. capacity controller·DB checkpoint/GC 진단·종료 유예·백업/RPO/RTO는 이 점검에서 구현/검증 완료로 취급하지 않는다.

변경한 검사기의 최종 fixture21개가 실패/skip0으로 통과했다. 검사기 보완 전 실행과 최종 결과를 합산하지 않는다. 기본 lint는 artifacts consistent·host4/app9 미확정·service0을 반환했다. 존재하지 않는 daemon 소켓과 합성 경로/파일로 Compose config --quiet와 셸 구문도 통과했으며 이미지 build/pull·daemon 자원 조작은 없다. 상대 파일 링크186개·anchor4개·용량 산식/경계·diff를 검사했다. 이는 full Go schema/module 재검증·실제 APFS/UID/SQL bootstrap/log rotation/장애 복구·450ms/24h 운영의 증거가 아니다.

runtime/config/deps1094개 해시가 같고 비Markdown 보호1098개 중 의도한 draft Compose/preflight/test/app YAML4개만 달라졌다. 공개 YAML에서는 미사용 timer를 제외하고 주석을 보완했으며 DB4·RPC5/head·초기화/GC false·미확정 값은 그대로다. 디스크/host 관찰·포맷·실제 DB/RPC·container/indexer/node 실행·종료/복구/삭제·추가 성능/전체 회귀/race200·다른 프로젝트 조작·commit/push는0이다. 합성 Compose 파일만 정리하고 근거/해시/범위는 /private/tmp/indexer-operations-offline-review-20261019/{offline-tests-final.log,offline-lint-final.json,compose-config.log,static-review.json,final-audit.json}에 보존한다.

### 3.97 운영 초안 커밋·PR 제출 전 검증 (10/19)

사용자 요청으로 누적된 운영 설정·Arc identity·Docker·용량·checkpoint/재시작/GC·오프라인 일관성 변경을 `chore/testnet-operations-drafts` 브랜치에서 함께 제출한다. 분석 목차에 새 근거 문서를 연결하고 PyYAML6.0.3을 전용 requirements로 고정했다. 기존 필수 CI의 test job에 Python3.12/21개 오프라인 fixture·draft lint·셸 구문 검사를 추가했다. 기존 lint/test/postgres/examples job 이름과 Go 회귀 명령은 그대로다.

커밋 전 로컬 필수 race 회귀는 Go1.24.9·GOMAXPROCS2·전용 cache·네트워크 의존성 다운로드 차단으로1회 실행했다. `make test-race`의 GOTEST에 `test -json -timeout=20m -skip=^TestEngineLoad`를 지정하여 기존10,000 subscriber 성능 부하 검사만 로컬에서 제외했다. 추가 성능 측정 보류와 저장소 race 규칙을 함께 적용한 범위이며, 완전 무제외 실행으로 표시하지 않는다. 결과는75개 패키지 통과/19개 테스트 없음, 최상위2279개 통과/81개 opt-in 등 건너뜀·실패0·DATA RACE 경고0이다. PostgreSQL/live RPC/선택 Rust fixture 환경은 주입하지 않았다. 새 Go 동시성 검사가 없어 기존 완료한200회 검사는 반복하지 않았다.

고정 PyYAML을 작업 전용 임시 경로에 설치해 오프라인21개를 실패/skip0으로 통과했고 lint·셸 구문·문서 파일 링크309개·diff/CI 구조를 검사했다. 기존 runtime/config/deps1094개 중 의도한 CI workflow 외 해시가 같으며 비공개 config.yaml의 ignore를 유지했다. Go 실행 코드/의존성·체인별 DB4·RPC5·커밋 직전 fresh/epoch·초기화/GC false와 다른 프로젝트는 그대로다. 실제 운영 디스크 검사/포맷·이미지 build/pull·노드/indexer/DB 운영 실행·추가 성능 비교는 하지 않았다. 원본과 범위 감사는 `/private/tmp/indexer-operations-pr-20261019/{race.log,race.result.json,offline.log,final-audit.json}`에 보존한다.

제출 이후 GitHub의 기존 필수4개 CI 전체를 확인하고 보호 규칙을 우회하지 않는 squash merge 뒤 main을 fast-forward 동기화한다. CI PostgreSQL은 기존 격리 기능 회귀이며 운영용 DB 실행 승인이 아니다. 이 항목은 제출 전 로컬 결과이며 PR/CI/merge 완료 자체의 증거로 사용하지 않는다.

### 3.98 읽기 전용 checkpoint·GC metadata 진단 도구 (10/19)

사용자 요청으로 `feat/checkpoint-inspection` 브랜치에 [별도 진단 명령](../OPERATIONS_INSPECTION.md)을 구현했다. `cmd/indexer-inspect`는 앱/source/window writer를 시작하지 않고 PostgreSQL의 기존 schema를 ReadOnly/no writable prefixes·관리용 최대1개 연결로 열어 source 세대/plan/anchor/Through/finality·writer/feature/worker의 기존 DB 상태·window binding·pending token/segment/reference digest와 raw ACK 표본을 읽는다. 앱의 체인별 pool4개를 바꾸거나 별도 앱 pool을 추가하지 않는다. RPC/peer 통신은0이다. 이번 CLI는 captured-feed/window schema 대상이며 RPC-only DB의 없는 capture 상태를 만들어내지 않는다.

raw reader는 기존 mount/논리 marker·관리 ledger/store 연결·STORE.json·durable .gc/완료 기록과 pending 파일 존재를 O_RDONLY로 조회한다. quota claim/LOCK 생성·window Open·복구·seal/truncate/fsync/rename/unlink·DB update를 호출하지 않는다. missing 파일만으로 완료를 추정하지 않으며 matching receipt도 삭제 승인이 아니다. live lease는 unknown, payload/proof integrity는 not_checked, gcDecision은 항상 not_authorized다. ACK는 pending 첫16개 고유 블록 또는 Through만 관측하고 truncation/범위를 표시한다. feature 페이지의 unitsPageComplete는 열거 범위이며 처리 완료와 구분한다. checkpoint/ACK·feature 페이지·선택 raw metadata를 다시 읽어 관측 중 변화를 거부하되 분산 atomic snapshot이나 완전한 GC 승인 dry-run이라고 표시하지 않는다.

첫 기능 검사에서 새 raw reader가 같은 root 내부 STORE.json 심볼릭 링크를 허용한 하위 검사1개가 실패했다. 읽기 플래그만 의존한 helper 재사용을 보완해 진단 경로에서 Lstat·열린 handle·재조회 identity/regular-file 여부를 확인한다. 기존 앱/GC 실행 소스는 수정하지 않았다. 초기 실패 원본은 functional-initial.log/result.json에 보존했다. 보완 후 기능 검사와 최종 소스 대상 race 각1회는3개 패키지·최상위14개/하위39개 통과, 실패/skip/DATA RACE0이었다. fake DB의 update는 panic이며 임시 파일의 내용/권한/수정 시각이 조회 전후 같음을 검사했다. pending/부분 삭제/완료 receipt/파일만 없음·identity/digest 오류·raw ACK 누락/손상/상한·페이지/세대 변화·취소·DSN0600/소유자/single-link/링크 거부·연결 반납·오류 비밀값 비노출을 포함한다.

Go1.24.9 대상 vet, macOS 별도 명령 build와 Linux arm64/CGO0 교차 build도 통과했다. Docker 초안에는 진단 binary build/copy recipe만 추가했으며 entrypoint/기동 policy는 그대로다. DB binding의 컨테이너 경로와 호스트 alias를 임의 교환하지 않고 같은 namespace에서 조회하도록 문서화했다. 이미지 build/pull·Docker/서비스·실제 DB/APFS/노드/RPC 실행은 하지 않았다. 기존 배포 offline lint도 artifacts consistent·servicesStarted0/host4/app9 미확정을 유지했다.

보호13개 config/의존성/수집·writer·fresh/GC/lifecycle·운영 YAML/Compose 해시와 기존 tracked Go 파일이 그대로임을 확인했다. 체인별 DB4·공유 RPC5 HTTP/s·커밋 직전 fresh/epoch·초기화/GC false·다른 프로젝트를 유지했다. 문서/남은 목록에는 metadata 조회 완료와 live lease/bytes·실제 DB/APFS·용량 controller·restart interlock·백업/RPO/RTO 미완료를 구분했다. 포맷·실제 운영 실행·추가 성능/전체 회귀/동시성200·commit/push/PR은 하지 않았다. 로그/도구 빌드/보호 해시/범위 감사는 `/private/tmp/indexer-checkpoint-inspection-20261019`에 보존한다.


### 3.99 checkpoint-inspection 커밋 전 검증 (10/20)

사용자 요청으로 `feat/checkpoint-inspection`의 진단 구현·기능 검사·Docker build recipe·문서18개 파일을 커밋한다. 요청한 읽기 전용 metadata 조회 범위는 구현됐으며 운영 전 실제 PostgreSQL read-only/no migration/write·취소/반납과 Docker/APFS identity/path 검증은 실행 보류로 남는다. 전체 raw/index/source bytes·required proof·ACK pagination·실행 중 writer의 읽기 전용 lease interface와 안전한 Pebble/RPC-only 조회는 선택 확장으로 구분했다. 자동 복구·fence 해제·기동·삭제는 이 진단 도구 범위가 아니다.

첫 전체 race 실행은 제한된 환경의 loopback bind 거부로 기존 모의 서버 검사들이 실패해 중단했다. 이 실행을 성공에 합산하지 않는다. 로컬 테스트 소켓을 허용한 환경에서 bind 사전 확인 후 pinned Go1.24.9·GOMAXPROCS2·전용 cache·의존성 다운로드 차단으로 `make test-race`를1회 완료했다. GOTEST의 `test -json -timeout=20m -skip=^TestEngineLoad`로 기존 성능 부하 검사만 제외한 범위이며 완전 무제외 실행으로 표시하지 않는다. 최종 결과는77개 패키지 통과/19개 테스트 없음, 최상위2293개 통과/81개 opt-in 등 건너뜀·실패0·DATA RACE0이다. 외부 PostgreSQL/live RPC/선택 fixture 환경은 주입하지 않았다. 새 동시성 검사가 없어 기존200회 검사는 반복하지 않았다.

진단 Go 소스9개는3.98의 최종 검증 후 동일하므로 기존 대상 기능/race14개·하위39개, vet, macOS/Linux arm64 build 결과도 유지된다. 보호13개 설정·수집/writer/fresh·의존성·운영 YAML/Compose 해시, gofmt·상대 문서 링크·staged diff를 최종 확인한다. 앱의 체인별 DB4·공유 RPC5 HTTP/s·커밋 직전 fresh/epoch·초기화/GC false와 다른 프로젝트는 그대로다. 디스크 포맷·이미지 build/pull·실제 운영 서비스/DB/노드·공개 RPC·추가 성능 측정은 없다. 이번 범위는 로컬 커밋이며 push/PR은 하지 않는다. 제한 환경 실패 원본과 최종 성공 로그·명령·해시는 `/private/tmp/indexer-inspection-commit-20261020/`에 보존한다.


### 3.100 PR #182 PostgreSQL CI 실패 분리와 알림 생성 복구 보완 (10/20)

PR [#182](https://github.com/0xmhha/indexer-go/pull/182)의 첫 [CI](https://github.com/0xmhha/indexer-go/actions/runs/39340912454)는 lint/test/examples를 통과했지만 postgres가 실패했다. adapter 패키지는251.632초에 통과했고 앱에서 TestNotificationsFollowTheChangeStream의 webhook 대기60초가 실패했다. 이후 진행하던 별도 source lookup 검사가16초인 상태에서 앱 패키지 전체가 Go 기본10분 제한으로 중단됐다. 개별 기능 실패와 suite 제한을 구분하며 제한 증가만으로 알림 실패를 숨기지 않는다.

원본 알림 테스트의 독립 PostgreSQL1회는 통과해 CI의 취소 시점을 직접 재현했다고 표시하지 않는다. 대신 기존 저장 순서의 각 쓰기에 취소를 주입하는 동기식 회귀 검사에서6개 하위 실패로 유실 경로를 확인했다. 본문만 저장된 뒤 인덱스 쓰기가 실패하면 replay가 이미 있는 ID로 판단해 생성을 건너뛰고 pending index/queue가 비는 경로다. 인덱스가 모두 기록된 뒤 본문을 공개하도록 바꾸고, 조회는 미공개/과거 시각/상태의 인덱스를 제외한다. 같은 ID의 재시도에서 바뀐 CreatedAt 때문에 중복 목록이 생기거나 잘못된 offset을 소비하지 않도록 했다. 기존 키/값 형식·SQL 바인딩은 유지하고 과거 부분 레코드의 자동 삭제·수리나 migration은 추가하지 않는다. PostgreSQL 검사 스크립트의 외부 DSN/소유 Docker 두 경로에는 일반 CI와 같은 package timeout20분을 지정했다.

보완 후 notifications 패키지 기능105개/하위175개와 임시 PostgreSQL의 알림/API/채널/재시작9개가 통과했고 성능 opt-in1개는 건너뛰었다. 임시 DB 두 실행은 fsync=on·앱 pool4이며 각 소유 container/volume을 정리했고 기존15개 container의 상태/시작 시각이 전후 같았다. 새 회귀3개는 worker Start 없이 동기식 쓰기 실패를 주입하므로 새 동시성200회 대상이 아니다. 운영 DB/서비스/노드·공개 RPC·APFS/디스크 포맷·추가 성능 비교·다른 프로젝트 변경은 없다. 보호13개 설정·수집/writer/fresh/GC·의존성과 체인별 DB4·RPC5·커밋 직전 fresh/epoch는 유지한다. 원래 CI 실패 로그, 실패 주입 red, 보완 후 기능 결과와 커밋 전 전체 race/정적 검사 결과는 `/private/tmp/indexer-inspection-pr-20261020/`와 `fix/`에 보존한다.


보완 소스의 커밋 전 전체 race는 Go1.24.9·GOMAXPROCS2·각1회·package timeout20분·기존 성능 부하 TestEngineLoad 제외로 완료했다.77개 패키지/최상위2296개 통과·테스트 없는19개 패키지/opt-in 등81개 건너뜀·실패0·DATA RACE0이다. 새3개 동기식 회귀와 최종 pagination assertion을 포함한다. 전체 build/vet/root lint·Go1040개 fmt·셸 구문도 통과했다. 변경 없는 astgraph vet/lint·예제·cache lifecycle·진단14/39 기능 결과는 앞선 성공 범위를 유지한다. 보호13개와 진단 Go9개 해시·문서 상대 링크245개·diff를 재확인했다. 다음 제출은 보완4개 파일을 별도 fix 커밋으로 push하고 PR의 필수4개 CI를 새 head에서 확인하는 범위다. 이 기록은 새 head CI 완료를 대신하지 않는다.
