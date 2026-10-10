# 남은 미결 사항 (10/9)

리팩토링 계획(R0~R6)을 마치고 계획 검토(`plan-audit.md`)까지 한 뒤 남은 일이다. 정해야 할 것, 고쳐야 할 것, 운영·측정 순으로 적었다. 항목마다 왜 남았는지와 고르는 방법의 단점을 함께 적었다.

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
| 노드 URL 노출 (해소 10/9) | 노출 경로는 GraphQL이 아니라 `GET /chains`였다(GraphQL 멀티체인 모듈은 서빙되지 않는 코드였다). 이제 노드 URL은 scheme과 host만 내보낸다. 인증 없는 체인 등록 mutation이 든 서빙되지 않던 GraphQL 모듈은 지웠다 | — | — |
| HTTP 경로와 멀티체인 (해소 10/9) | 멀티체인 서버가 체인마다 `/chains/{id}/<pattern>`에 mount한다(`{id}` 인자가 있는 패턴은 제외). ingest 역할은 의도대로 mount하지 않는다 | — | — |
| records 표 정의 변경 (일부 해소 10/10) | 표에 컨트랙트 주소를 더하면 그 표를 처음부터 다시 채운다(`feature.PartEvolver`). 이벤트·키 변경과 주소 삭제는 여전히 시작을 거부한다 | 그 경우 이름을 바꿔 새로 색인하거나 재색인해야 한다 | [권장] |
| declared 모드 범위 | 멀티체인, `source.era_dir`를 지원하지 않는다. finalized 범위 모드는 로그 없는 블록을 저장하지 않는다 | 해당 구성은 시작하지 않는다 | [권장] |
| archive가 아닌 노드 | `balance.native`는 노드가 옛 블록 상태가 없다고 답하면 0에서 시작하고, `token.metadata`는 latest로 읽는다(둘 다 경고) | 옛 높이부터 색인하면 잔액과 토큰 총공급이 틀릴 수 있다. 정확한 값에는 archive 노드가 필요하다 | [권장] |

## 3. 측정과 확인이 남은 것

| 항목 | 남은 일 |
|---|---|
| K2 이진 키 | 11절이 R4-2 뒤 실제 DB로 다시 재기로 했으나 기록이 없다 |
| PostgreSQL 쓰기 성능 | ingest가 Pebble보다 약 4배 느리다(R4-2). 다시 재지 않았다 |
| 넓은 범위 조회 | 10,000블록에서 블록당 비용이 늘어난다(R0-8). 드문 조건의 필터를 범위 없이 주면 체인 끝까지 읽는다 |
| kill 시험 (10/10 확인) | 결함 두 가지를 주입해 둘 다 실패하는 것을 확인했다. 블록 commit을 두 batch로 나누면 데이터 120건이 빠지고, 재시작 뒤 주소 sequence를 복원하지 않으면 `/index/addr/`·`/index/balance/` 키가 덮어써진다. 같은 높이가 이어진 원인: 재시작 뒤 API가 답한 때부터 첫 블록 색인까지 30~45ms가 걸리는데(부하 없을 때 측정), kill 전 대기가 20~420ms에서 무작위라 짧은 대기가 이어지면 진행 없이 kill된다. 시작 경로가 멈추는 것은 아니다 |
| SLO | 한 호스트에서만 쟀다(R5-5). 시험의 구독 queue(1024)가 운영 기본값(16384)과 다르고, 부하 체인은 V2 시장만 쓴다 |
| DEX 실제 컨트랙트 | 시험은 실제 이벤트 시그니처로 만든 로그를 쓴다. testnet 8283 컨트랙트로 확인하지 않았다 |
| live 노드 시험 (10/10 확인) | 로컬 go-stablenet(Gstable v1.1.0, chainbench로 validator 4 + endpoint 1)에서 `TestLiveBalances`, `TestLiveStableNet`, `TestLiveStableNetIdentity`(fee delegation 0x16 트랜잭션 3건 포함), `TestLiveFailover`, `TestLiveRecordReplay`, `TestLiveHeadLatency`(newHeads p95 4ms), `TestLiveLoopRollsBackReorg`가 통과했다. `TestLiveEraSource`는 era1 파일이 없어 돌리지 않았다. chainbench가 만든 genesis에는 `applepieBlock`이 없어 fee delegation을 쓰려면 genesis에 `applepieBlock`, `bohoBlock`을 0으로 넣어야 했다(chainbench 쪽 문제) |
| indexer-frontend | GraphQL 문서 26개가 지금 스키마에서 동작하지 않는다(`known-invalid.txt`). REST로 옮기는 일도 frontend 쪽에 남아 있다(`docs/FRONTEND_MIGRATION.md`) |

## 4. 저장소 관리

- `tools/astgraph` 바이너리(7 MB)가 #92에서 잘못 commit됐다가 #93에서 지워졌다. git history에는 남는다. 지우려면 main의 history를 다시 써서 force push해야 하므로 하지 않았다.
- 시험 전용 도우미가 운영 패키지에 있다(`Fetcher.FetchBlock`, `orderbook.Service.Sync`, `agg.RecomputeCandles/Series`). 시험이 쓰므로 남겨 두었다.
