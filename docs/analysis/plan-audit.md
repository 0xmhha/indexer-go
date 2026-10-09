# 리팩토링 계획 완료 검토 (10/9)

`refactoring-plan.md`에서 "완료"로 적은 항목이 정말 완료인지, 코드 전체의 AST 그래프와 실제 코드, 시험 실행으로 대조한 기록이다. 완료 표기가 사실과 다른 항목을 찾는 것이 목적이었다.

## 1. 결론

- 계획 항목 42개(R0-1~R6-3)와 4.2절 결함, 설계 문서의 주장을 검토했다. 완료 표기가 사실과 크게 다른 것은 둘이었고, 둘 다 같은 날 고쳤다.
  - D5(기능 처리기가 저장 실패를 경고로만 남기고 블록을 commit한다)는 4.2절 표에 상태가 없었고 계정 추상화(AA) 기능에 그대로 남아 있었다. #95에서 고쳤다(`balance.native`는 #91에서 먼저 고쳤다).
  - R2-8의 "쓰지 않는 `Chain*Key` 삭제"는 사실이 아니었다(21개 함수가 남아 있었다). #96에서 지웠다.
- 나머지 PARTIAL 판정은 대부분 문서가 코드보다 늦은 것이다(옮겨진 경로, 이름이 바뀐 시험, 이미 해소된 "남은 것"). 이 문서의 4장과 함께 계획 문서를 고쳤다.
- 확인하지 못한 것: 실제 go-stablenet 노드가 필요한 시험(`INDEXER_LIVE_RPC`)은 돌리지 않았다. D16의 live 부분이 여기에 해당한다.

---

## 2. 방법

1. `tools/astgraph`로 운영 코드(시험 제외) 전체를 그래프로 만들었다: 패키지 72개, 노드 5,804개, 간선 25,591개(호출, interface 호출, 참조, 구현, 포함, import).
2. `tools/astgraph/query.py`로 운영 경로에서 닿는 코드를 계산했다. 출발점은 `cmd/indexer.main`과 모든 패키지의 `init`(등록)이다. 메서드는 직접 호출되거나, 그 메서드가 구현하는 interface 메서드가 닿는 경로에서 호출될 때 닿은 것으로 본다. reflection(graphql-go resolver)이나 표준 라이브러리 interface(`http.Handler` 등)로만 불리는 메서드는 닿지 않은 것으로 나오므로 코드를 읽어 따로 확인했다.
3. 계획 단계별로 읽기 전용 검토 agent 6개가 항목마다 주장을 하나씩 확인했다: 인용한 함수가 있는가(그래프 find), 운영 경로에 연결됐는가(그래프 path), 설정 키를 읽는가, 인용한 시험이 있고 통과하는가(직접 실행), 동작이 글과 같은가. 판정은 CONFIRMED, PARTIAL(완료지만 주장 하나가 사실과 다름), NOT_DONE, CANNOT_VERIFY(외부 환경 필요)이다.
4. PostgreSQL 시험은 일회용 컨테이너로 일부 돌렸다. 전체는 이 문서와 같은 날 `scripts/test-postgres.sh`로 통과했다.

재현:

```bash
(cd tools/astgraph && go run . -dir ../.. -out out)
python3 tools/astgraph/query.py path "pkg/fetch.(Fetcher).indexSparse"
python3 tools/astgraph/query.py dead pkg/chains/stablenet/feedelegation
```

---

## 3. 판정

| 판정 | 항목 |
|---|---|
| CONFIRMED | R0-1, R0-3, R0-4, R0-5, R0-6, R0-7, R0-8, R0-9, R0-10, R1-1, R1-2, R1-4, R1-5, R1-6, R2-2~R2-7, R3-1~R3-5, R4-1~R4-5, R5-1~R5-5, R6-1, R6-3, D1~D4, D6~D8, D10~D12, D14, D15, D18, D20~D22, F1, F2, C5, C6, 8장 결정성 항목 |
| PARTIAL | R0-2, R1-3, R2-1, R2-8, R6-2, D13, 11절, `reorg-design.md`, `feature-registry-design.md`, `chain-profile-design.md`, `p07-reuse.md` |
| NOT_DONE | D5 |
| CANNOT_VERIFY | D16(live 노드 부분) |

PARTIAL과 NOT_DONE의 내용과 처리:

| 항목 | 사실과 다른 점 | 심각도 | 처리 |
|---|---|---|---|
| D5 | AA 기능 3개가 처리기 오류를 모두 버리고, 처리기 안의 통계·위임 상태·스마트 계정 쓰기 실패도 경고뿐이었다. 통계 읽기가 실패하면 0으로 덮어썼다 | [중요] | #95에서 고침. 4.2절에 상태 기록 |
| R2-8 | "쓰지 않는 `Chain*Key` 삭제"가 사실이 아님 | [권장] | #96에서 삭제 |
| R6-2 | `docs/SDK.md`가 `OnPart`를 안내하지만 `PartRegistrar`가 `pkg/sdk`에 없었다. ingest 역할에서도 경로가 mount되지 않는 점, go-ethereum 타입 필드, CI가 없다는 점이 문서에 없었다 | [권장] | #96에서 `sdk.PartRegistrar` 추가, 문서 보완 |
| R1-3 | 과제에 있던 "단일 값 형식"은 하지 않았고 보류로도 적지 않았다. 11절이 약속한 K2(이진 키) 재측정은 R4-2 뒤에 기록이 없다 | [권장] | 계획에 미완으로 적음, 미결 사항에 넣음 |
| R2-1 | 노드 failover는 단일 체인 모드에서만 된다. 멀티체인은 `fallback_endpoints`를 비운다 | [권장] | 계획에 적음, 미결 사항에 넣음 |
| R0-2 | 완료 조건의 "옛 코드에서 D1~D3 재현"은 D1·D3·D10만 했다(D2는 옛 경로가 지워져 이제 할 수 없다). kill 시험에 결함을 주입해 잡는지 확인하지 않았다. 한 실행에서 마지막 kill 4번이 같은 높이(137)에서 멈춰 있었다 | [권장] | 미결 사항에 넣음 |
| D13 | 행의 "`profile_source: false`면 결함이 남는다"는 이제 그 설정을 시작할 때 거부하므로 낡은 설명이다 | [권장] | 계획 수정 |
| 설계 문서 3개, `p07-reuse.md` | 지워진 시험·패키지 이름(`TestFinalityFinalizedUnsupported`, `TestSystemContractsFeatureOff`, `TestClientSourceMatchesGolden`, `pkg/resilience`, `pkg/watchlist`)과 이미 해소된 "남은 것"이 남아 있다. `p07-reuse.md`의 결론(P07을 따로 유지)은 R6-3이 대체했다 | [권장] | 각 문서 앞에 갱신 표기 |

CONFIRMED지만 검토가 덧붙인 사실:

- `stablenet.fee_delegation`이 쓰는 메타데이터와 fee payer 색인은 운영 코드에서 읽는 곳이 없다(그래프: `feedelegation.(MetaStore).TxMeta`, `TxsByFeePayer`가 닿지 않음).
- R4-1: API 전용 프로세스에는 알림 서비스가 없어 GraphQL 스키마에서 알림 모듈이 빠진다. 계획의 "쓰기는 read-only 오류"와 다르게, 알림 mutation은 스키마 오류가 된다.
- R4-2, R4-4, R0-9의 경로·시험 이름이 바뀌었다(`cmd/indexer/store.go` → `pkg/app/store.go` 등).
- R5-1, R5-2의 DEX 시험은 실제 컨트랙트를 실행하지 않고 실제 이벤트 시그니처로 만든 로그를 쓴다. testnet 8283 컨트랙트로 확인한 시험은 없다.
- PostgreSQL에서 R5와 R6-1 인수 시험의 절반은 검토 agent가 돌리지 않았다. 같은 날 전체 PostgreSQL 시험이 통과했다.

---

## 4. 검토 중 함께 찾은 것

| 발견 | 근거 | 심각도 | 처리 |
|---|---|---|---|
| gap 채우기가 `ErrGapBelowIndexed` 말고 다른 이유로 실패하면 로그만 남고, live loop가 다시 시도하지 않는다. 다음 재시작까지 빈칸이 남는다 | `pkg/fetch/fetcher_gaps.go:219` | [중요] | 미결 사항 |
| 해석할 수 없는 outbox 항목(다른 build가 쓴 이벤트 타입)을 relay가 건너뛰면, 구독 엔진이 빈 sequence를 유실로 보고 구독자를 끊는다. 재개가 같은 항목을 다시 읽어 반복될 수 있다 | `pkg/stream/relay.go:66`, `engine.go:177` | [권장] [Mid] | 미결 사항 |
| outbox 정리(prune)는 같은 프로세스에서 소비 중인 group만 보호한다 | `pkg/stream/outboxbus.go:210` | [권장] | 미결 사항 |
| relay가 시작할 때 stream에 들어가지 못하면 로그만 남기고 health에 드러나지 않는다 | `pkg/fetch/outbox.go:51` | [권장] [Low] | 미결 사항 |
| `Fetcher.FetchBlock`, `orderbook.Service.Sync`, `agg.Recompute*`는 시험만 쓰는 운영 패키지 코드다 | 그래프 dead | [권장] | 유지(시험 도우미) |
| 저장소에 CI가 없어 `TestSDKSurface`, 예제, PostgreSQL, SLO 시험은 사람이 돌릴 때만 돈다 | `.github/workflows` 없음 | [중요] | 미결 사항 |
