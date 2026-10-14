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

## 4. 저장소 관리

- `tools/astgraph` 바이너리(7 MB)가 #92에서 잘못 commit됐다가 #93에서 지워졌다. git history에는 남는다. 지우려면 main의 history를 다시 써서 force push해야 하므로 하지 않았다.
- 시험 전용 도우미가 운영 패키지에 있다(`Fetcher.FetchBlock`, `orderbook.Service.Sync`, `agg.RecomputeCandles/Series`). 시험이 쓰므로 남겨 두었다.
