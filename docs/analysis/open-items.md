# 남은 미결 사항 (10/9)

리팩토링 계획(R0~R6)을 마치고 계획 검토(`plan-audit.md`)까지 한 뒤 남은 일이다. 정해야 할 것, 고쳐야 할 것, 운영·측정 순으로 적었다. 항목마다 왜 남았는지와 고르는 방법의 단점을 함께 적었다.

## 1. 정해야 할 것

### 1.1 fee delegation 메타데이터를 쓸지 지울지

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
| gap 채우기 재시도 | `--gap-recovery`에서 gap 채우기가 `ErrGapBelowIndexed`가 아닌 이유로 실패하면 로그만 남고 다시 시도하지 않는다(`fetcher_gaps.go:219`) | 다음 재시작까지 빈칸이 남는다 | [중요] |
| 멀티체인 failover | 멀티체인 모드는 체인마다 `rpc.fallback_endpoints`를 비운다(`app.go:780`), 설정 필드도 없다 | 멀티체인에서 노드 하나가 죽으면 그 체인이 멈춘다 | [권장] |
| 해석할 수 없는 outbox 항목 | relay가 건너뛴 sequence를 구독 엔진이 유실로 보고 구독자를 끊는다. 재개가 같은 항목을 다시 읽는다 | 서로 다른 build가 같은 DB를 쓸 때만 생긴다. 구독자가 재접속을 반복할 수 있다 | [권장] |
| outbox 정리 보호 | prune이 같은 프로세스에서 소비 중인 group만 본다 | 멈춘 알림 group이나 API 프로세스가 보존 범위(기본 10만)보다 뒤처지면 `SEQUENCE_TOO_OLD` | [권장] |
| relay 시작 실패 | stream에 들어가지 못하면 로그만 남는다(`fetch/outbox.go:52`) | 이벤트 전달이 멈춰도 health에 드러나지 않는다 | [권장] |
| API 프로세스의 알림 | `node.role: api`에는 알림 서비스가 없어 스키마에서 알림 모듈이 빠진다 | ingest 프로세스와 스키마가 다르다. 쓰기를 ingest로 넘기는 일(R4-1의 남은 것)은 그대로 남아 있다 | [권장] |
| 값 형식 통일 | 값이 JSON, RLP, 고유 binary로 섞여 있다. R1-3의 "단일 값 형식"은 하지 않았다 | 새 저장 코드가 형식을 고를 기준이 없다. 바꾸려면 재색인이 필요하다 | [권장] |
| HTTP 경로와 멀티체인 | `sdk.RegisterRoute` 경로는 멀티체인 서버와 ingest 역할에 mount되지 않는다 | 멀티체인으로 운영하는 프로젝트는 경로를 못 쓴다 | [권장] |
| records 표 정의 변경 | 이미 색인한 표의 정의(주소 추가 포함)를 바꾸면 시작을 거부한다 | 이름을 바꿔 새로 색인하거나 재색인해야 한다. 추가된 주소만 채우는 방식은 없다 | [권장] |
| declared 모드 범위 | 멀티체인, `source.era_dir`를 지원하지 않는다. finalized 범위 모드는 로그 없는 블록을 저장하지 않는다 | 해당 구성은 시작하지 않는다 | [권장] |
| archive가 아닌 노드 | `balance.native`는 노드가 옛 블록 상태가 없다고 답하면 0에서 시작하고, `token.metadata`는 latest로 읽는다(둘 다 경고) | 옛 높이부터 색인하면 잔액과 토큰 총공급이 틀릴 수 있다. 정확한 값에는 archive 노드가 필요하다 | [권장] |

## 3. 측정과 확인이 남은 것

| 항목 | 남은 일 |
|---|---|
| K2 이진 키 | 11절이 R4-2 뒤 실제 DB로 다시 재기로 했으나 기록이 없다 |
| PostgreSQL 쓰기 성능 | ingest가 Pebble보다 약 4배 느리다(R4-2). 다시 재지 않았다 |
| 넓은 범위 조회 | 10,000블록에서 블록당 비용이 늘어난다(R0-8). 드문 조건의 필터를 범위 없이 주면 체인 끝까지 읽는다 |
| kill 시험 | 결함을 주입해 잡는지(mutation) 확인하지 않았다. 한 실행에서 재시작 4번이 같은 높이에 머문 원인을 보지 않았다 |
| SLO | 한 호스트에서만 쟀다(R5-5). 시험의 구독 queue(1024)가 운영 기본값(16384)과 다르고, 부하 체인은 V2 시장만 쓴다 |
| DEX 실제 컨트랙트 | 시험은 실제 이벤트 시그니처로 만든 로그를 쓴다. testnet 8283 컨트랙트로 확인하지 않았다 |
| live 노드 시험 | `TestLiveStableNet*`, `TestLiveBalances`, `TestLiveFailover`, `TestLiveEraSource`는 go-stablenet 노드가 있어야 돈다. 이번 정리 기간에는 돌리지 않았다 |
| indexer-frontend | GraphQL 문서 26개가 지금 스키마에서 동작하지 않는다(`known-invalid.txt`). REST로 옮기는 일도 frontend 쪽에 남아 있다(`docs/FRONTEND_MIGRATION.md`) |

## 4. 저장소 관리

- `tools/astgraph` 바이너리(7 MB)가 #92에서 잘못 commit됐다가 #93에서 지워졌다. git history에는 남는다. 지우려면 main의 history를 다시 써서 force push해야 하므로 하지 않았다.
- 시험 전용 도우미가 운영 패키지에 있다(`Fetcher.FetchBlock`, `orderbook.Service.Sync`, `agg.RecomputeCandles/Series`). 시험이 쓰므로 남겨 두었다.
