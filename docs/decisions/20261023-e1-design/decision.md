# E1 실행 수집 결정 (2026-10-23)

이 문서는 E1 상세 설계 전에 사용자와 대화로 정한 결정을 모은 것이다. 질문과 답의 원문은 같은 폴더의 `rounds.jsonl`에 있다. 상위 문서는 [실행 수집 설계](../../analysis/execution-ingest-design.md)와 [discovery](../../codemine/runs/20261023-e1-execution/01-discovery.md)다.

인터뷰는 46회차까지 진행했다. 44회차에 설계 1판의 반증 검토 결과를 기록했고, 그 결과로 D19와 D20을 더 정했다. 모호성 점수는 42회차 직전에 0.144로 기준(0.2) 아래를 두 번 연속 기록했다. 그 뒤 사용자가 데이터 관점의 의도를 다시 설명하고(43회차) "설명을 기반하여 작업을 진행한다"고 지시해 인터뷰를 닫았다. 43회차의 설명은 아래 1장과 2장에 반영했다.

---

## 1. 목표

StableNet 블록을 별도 실행부 프로세스에서 go-stablenet 규칙으로 실행하고, 그 결과를 indexer가 PostgreSQL에 archive 수준으로 쌓는다. 쌓은 데이터로 어느 날짜, 어느 블록 높이, 어느 계정 주소의 정보든, 블록 구간 사이의 자금 이동이든 쿼리로 찾을 수 있어야 한다. 정상 행위와 인위적인 행위를 구분하는 분석도 이 데이터 위에서 한다. 가장 최근 상태만 필요하면 RPC로 묻는 것으로 충분하므로, PostgreSQL에 쌓는 이유는 이력이다(43회차).

같은 testnet 블록에서 기존 RPC 수집보다 블록 생성부터 API 노출까지의 p50과 p99가 모두 작아야 한다(D15).

---

## 2. 데이터 관점 (43회차)

- genesis(0번 블록)가 최초 상태를 만든다.
- 그 뒤 상태는 transaction이 바꾼다. 계정 자산이 늘고 줄고, 계약이 생기고, 계약 함수가 계약 내부 상태를 바꾼다.
- transaction이 아닌 변화는 블록 보상이다. StableNet에서는 WBFT `Finalize`의 base fee 분배와 system contract 전이가 여기에 해당한다(E0 확인).
- 이 변화들이 누적되어 각 높이의 상태가 된다.
- PostgreSQL에는 컬럼마다 변화를 누적하는 이력 표와, 최신 값을 유지하는 표를 따로 둔다.

**설계에 주는 요구.** D19의 재실행 recorder로 거래별 전후 값을 남기고, Finalize 변화는 마지막 ordinal의 별도 원인으로 기록한다. 블록 단위 양방향 trie 비교는 기록 누락을 검증하는 독립 검사다.

---

## 3. 결정

| 번호 | 주제 | 결정 | 회차 |
|---|---|---|---|
| D1 | 바뀐 상태를 꺼내는 방법 | 노드 DB의 부모 trie와 새 trie를 비교한다. D19 뒤로는 제품 경로가 아니라 시험에서 D19 결과를 대조하는 데만 쓴다 | 22, 45 |
| D2 | header를 얻는 곳 | 실행부의 노드 DB(`BlockChain`). IPC header 메시지는 두지 않는다 | 26 |
| D3 | trie 노드와 code | trie 노드는 노드 DB에만, code는 PostgreSQL에도 둔다 | 27 |
| D4 | 높이가 어긋날 때 | indexer의 PostgreSQL 커밋 높이가 정본이다. 연결할 때와 커밋마다 실행부에 알리고, 실행부는 `SetHead`로 되감거나 기다린다. reorg는 fork 지점부터 다시 보낸다. 내보내기는 (높이, hash) 단위로 여러 번 해도 결과가 같다 | 28, 36 |
| D5 | wire | 길이, 종류, JSON 머리, 원본 바이트 구간. 연결 하나를 유지하고 8MiB씩 나눈다. 표준 라이브러리만 쓴다 | 6, 36 |
| D6 | PostgreSQL 표와 쓰기 | `blocks`, `transactions`, `receipts`, `logs`, `accounts`, `balance_history`, `contract_code`, `storage_history`, `storage_current`. 표마다 queue와 담당 writer를 두고, 모든 표가 높이 h를 쓰면 `visible_height`를 올린다. 이력 표는 높이 범위 partition. 지금 값 표는 이력에서 복원해 되돌린다 | 31 |
| D7 | `token.metadata` 계약 호출 | E1에서는 RPC를 유지한다. `balance.native`는 실행 결과를 쓴다 | 8 |
| D8 | 시험 기준 블록 | 녹화한 testnet 구간은 CI 기본, 로컬 go-stablenet 체인은 opt-in | 9 |
| D9 | 시험 시작 상태 | 녹화할 때 구간이 건드리는 노드만 받는다. 로컬 체인은 genesis부터 | 10 |
| D10 | 조립 | go-stablenet `eth.New`와 `node.New`를 쓰고 필요 없는 기능은 설정으로 끈다 | 21 |
| D11 | 노드 DB | 유지한다. PostgreSQL 저장은 따로 구현한다 | 19 |
| D12 | PostgreSQL을 쓰는 주체 | indexer만 쓴다. 실행부는 PostgreSQL에 접근하지 않는다. 기존 기능 표는 E1에서 기존 단일 writer가 쓴다 | 31, 36 |
| D13 | 결과를 넘기는 방법 | 실행부 안에서 블록 이벤트를 받아 메모리에서 꺼내 IPC로 보낸다 | 36 |
| D14 | 노드 DB 크기 | E1은 path 방식으로 만들고 증가량을 잰다. 블록 정리는 E3 | 25 |
| D15 | 완료 기준과 속도 | 기능 기준 1~5, 속도는 기존 RPC 수집과의 상대 비교(B) | 29 |
| D16 | 완료 기준 추가 | 기준 6(archive 쿼리 대조), 7(반쯤 쓰인 블록이 보이지 않음) | 39 |
| D17 | 증명 tool | E2에서 bootstrap 검증 도구로 먼저 만든다. 증명 API는 나중에 정한다 | 40 |
| D18 | D9를 지금 구조에 맞춤 | 녹화에 건드린 계정·슬롯의 시작 직전 값을 더한다. 노드와 code는 실행부 노드 DB에, 시작 직전 값은 indexer 상태 표에 넣는다 | 41 |
| D19 | 거래별 상태 변화 | 실행부가 블록을 다시 실행할 때 노드 DB를 감싼 `state.Database`를 쓴다. 거래마다 `IntermediateRoot`로 바뀐 계정·슬롯을 전 값과 함께 기록하고, `Finalize` 뒤 블록 단위 변화를 기록한다. 마지막 root가 header와 같아야 한다. prestate tracer는 fee delegation 지불자와 EVM 밖 변화를 놓쳐 쓰지 않는다 | 45 |
| D20 | 표 이름과 writer | `state_` 접두. writer 7개: blocks, transactions, receipts, logs, accounts(`state_account_changes`+`state_accounts`), storage(`state_storage_changes`+`state_storage`), contracts(`state_contracts`+`state_code`) | 46 |

**흐름.** 실행부(`x/stablenet/executor`, 자기 go.mod)는 정상 peer로 블록을 받아 WBFT로 검증하고 EVM으로 실행해 노드 DB에 저장한다. 블록이 저장될 때마다 같은 프로세스에서 결과를 꺼낸다. 블록 원문, receipt 원문, 상태 변화를 IPC로 indexer에 보낸다. indexer는 수신 goroutine, 표별 queue와 writer, 조회 goroutine, 외부 요청 goroutine(WebSocket, RPC, GraphQL)으로 나눠 동작한다.

**IPC로 보내는 것의 단위.** 상태 변화는 D19대로 transaction별로, Finalize 변화는 블록 단위로 보낸다. 두 종류의 순서는 ordinal로 보존한다.

---

## 4. 제약

- go-stablenet은 고치지 않는다. 실행부는 indexer 코드를 import하지 않는다(work log 3.109).
- 실행부는 PostgreSQL에 접근하지 않는다.
- 실행 수집의 Pebble 지원은 계획하지 않는다.
- 기존 RPC 수집 경로는 바꾸거나 없애지 않는다.

---

## 5. 완료 기준

1. 녹화 구간을 RPC 수집과 실행 수집으로 돌려 블록, 거래, receipt, 로그, native 잔액의 읽기 결과 차이가 0건이다.
2. 실행부가 모든 블록을 받아들이고, header와 다른 블록은 거부한다.
3. trie 비교로 구한 상태 변화가 블록마다 E0 방식(reflect, 시험 코드 전용) 결과와 같다.
4. 로컬 체인에서 내보내기 재시작, 노드 비정상 종료, reorg 뒤 표 내용이 중단 없이 돌린 결과와 같다.
5. 블록당 노드 DB와 PostgreSQL 증가량을 기록한다(판정 없음).
6. 구간 안의 모든 높이와 건드린 계정·슬롯에 대해 PostgreSQL 이력 값이 노드 상태와 같다. 128블록 밖은 E0 결과와 대조한다.
7. 표 writer 하나를 늦추거나 죽여도 `visible_height` 위가 보이지 않고, 재시작 뒤 결과가 같다.

속도: 같은 testnet 블록에서 실행 수집의 블록 생성→API 노출 p50, p99가 기존 RPC 수집보다 작다. 실제 testnet peer 접속은 측정 전에 승인을 받는다.

---

## 6. 가정

- E1은 StableNet과 PostgreSQL만 다룬다.
- go-stablenet 노드 DB에 바깥에서 받은 trie 노드를 넣고 부모 블록에서 바로 시작할 수 있다(D18). 시험하지 않았다.
- pathdb에서 `SetHead`가 D4의 용도로 동작한다. 시험하지 않았다.
- 블록 정리(E3)를 해도 `BlockChain`이 동작한다. 시험하지 않았다.
- preimage 기록의 디스크 비용은 감당할 만하다. 재지 않았다.

---

## 7. 미룬 것

- E2: 두 시작 모드, 전체 상태 bootstrap, 증명 tool(검증용)
- E3: peer 블록 정리, 원본 삭제
- E4: 기존 기능 표의 writer 분리
- E5: 다른 체인. E6: RPC 없는 canonical 판정
- E1 뒤: `token.metadata` 계약 호출을 실행부로 옮기기, 증명 API
- 거래별 귀속은 D19, 표별 writer는 D20과 구현 계획으로 정했다. 미결 항목으로 되돌리지 않는다.

---


## 8. 구현 위임과 보완 계약

2026-10-24 사용자는 E1 완료까지 추천 결정을 에이전트에 위임했다. 2회 실행을 유지한다. 연결4개·미공개 저장과 원자적 공개·공개 시점 상태 조회·rewind fence·최종 공개 직전 fresh의 구체 계약은 [구현 계획](../../codemine/runs/20261023-e1-execution/03-plan.md)을 따른다. 이는 사용자가 새 계획을 직접 검토했다는 뜻이 아니다. 각 기능은 RED→GREEN·문서·체크리스트·필수 CI를 거쳐 squash merge한다. 과거 결정 대화는 Git history에서 확인한다.
