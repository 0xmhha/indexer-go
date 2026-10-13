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

## 4. 저장소 관리

- `tools/astgraph` 바이너리(7 MB)가 #92에서 잘못 commit됐다가 #93에서 지워졌다. git history에는 남는다. 지우려면 main의 history를 다시 써서 force push해야 하므로 하지 않았다.
- 시험 전용 도우미가 운영 패키지에 있다(`Fetcher.FetchBlock`, `orderbook.Service.Sync`, `agg.RecomputeCandles/Series`). 시험이 쓰므로 남겨 두었다.
