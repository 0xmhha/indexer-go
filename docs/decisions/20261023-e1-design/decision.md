# E1 실행 수집 결정 (2026-10-23)

이 문서는 E1 상세 설계 전에 사용자와 대화로 정한 결정을 모은 것이다. 질문과 답의 원문은 같은 폴더의 `rounds.jsonl`에 있고, 맨 아래에 사용자 답을 주제별로 붙였다. 상위 문서는 [실행 수집 설계](../../analysis/execution-ingest-design.md)와 [discovery](../../codemine/runs/20261023-e1-execution/01-discovery.md)다.

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

**설계에 주는 요구.** D19로 정했다. 아래 문단은 D19 이전의 해석이다.

**(이전 해석)** 상태 변화가 어느 transaction에서 생겼는지 기록에 남아야 자금 이동과 행위 분석을 쿼리할 수 있다. 블록 보상처럼 transaction이 아닌 변화는 블록 단위 변화로 따로 표시한다. 블록 단위 trie 비교(D1)만으로는 변화를 transaction별로 나눌 수 없으므로, 02-design에서 transaction별로 귀속하는 방법을 정한다. 후보는 실행부 안에서 블록을 다시 실행하며 go-stablenet의 `prestateTracer`(diffMode, `eth/tracers/native/prestate.go:76`)로 transaction별 전후 값을 얻고, 블록 단위 trie 비교 결과와 맞춰 보는 방법이다. 아직 시험하지 않았다.

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

**IPC로 보내는 것의 단위.** 43회차 설명에 따라 상태 변화는 transaction별로, 블록 보상은 블록 단위로 보내는 방향으로 02-design에서 다시 정한다. 42회차에 보인 블록 단위 예시는 확정이 아니다.

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
- 02-design: 상태 변화를 transaction별로 귀속하는 방법, 기존 단일 writer와 표별 writer의 분리 방식

---

## 결정 기록 원문: E1 실행 수집 상세 설계

### D1. 바뀐 상태를 꺼내는 방법 (decided)
- 질문 2: 바뀐 상태를 꺼내는 방법: A trie 비교 / B state.Database 감싸기 / C B로 꺼내고 시험에서 A로 대조
  - 답: 추천 C로 진행해
- 질문 20: reopen
  - 답: D11에서 노드 DB 유지(A)를 고름. BlockChain이 state.Database를 스스로 만들어 감싸기 방식을 그대로 쓸 수 없음
- 질문 22: 바뀐 상태를 꺼내는 방법(노드 DB 유지 후): A trie 비교 + preimage 기록 / B 처리기 교체 hook / C snapshot 갱신 내용
  - 답: 추천 A로 진행

### D2. 실행부가 header를 얻는 곳 (decided)
- 질문 3: 실행부가 header를 얻는 곳: A indexer가 IPC로 보냄 / B 실행부가 PostgreSQL blocks 표 직접 읽기 / C 원본 저장소
  - 답: A로 진행.
- 질문 14: reopen
  - 답: 사용자 교정(13회차): 실행부가 header가 필요한 만큼의 블록을 스스로 유지하면 IPC로 받을 필요가 없고, 노드 클라이언트의 DB 기능 유지 여부를 분석해야 한다
- 질문 26: header를 얻는 곳(노드 DB 유지 후): A 실행부 노드 DB(BlockChain)에서 읽고 IPC header 메시지 없앰 / B indexer가 IPC로 보냄
  - 답: A

### D3. 실행부가 trie 노드와 code를 얻는 곳 (decided)
- 질문 4: 실행부가 trie 노드와 code를 얻는 곳: A PostgreSQL 읽기 전용 직접 읽기 / B indexer에 IPC로 묻기 / C 실행부 로컬 저장소
  - 답: 추천 A로 진행
- 질문 15: reopen
  - 답: 사용자 교정(13회차): 실행부가 header가 필요한 만큼의 블록을 스스로 유지하면 IPC로 받을 필요가 없고, 노드 클라이언트의 DB 기능 유지 여부를 분석해야 한다
- 질문 27: trie 노드와 code를 어디에 두나(노드 DB 유지 후): A trie 노드는 노드 DB에만, code는 PostgreSQL에도 / B 둘 다 PostgreSQL에도 / C 둘 다 노드 DB에만
  - 답: A 로 진행.

### D4. 실행부와 DB의 높이가 어긋날 때 (decided)
- 질문 5: 실행부와 DB의 높이가 어긋날 때: A DB 정본, 실행부는 미확정 결과를 메모리에 두고 커밋됨/되돌려라 메시지로 맞춤 / B 실행부 상태 없음 / C 멈추고 운영자 처리
  - 답: 추천 A로 진행해
- 질문 16: reopen
  - 답: 사용자 교정(13회차): 실행부가 header가 필요한 만큼의 블록을 스스로 유지하면 IPC로 받을 필요가 없고, 노드 클라이언트의 DB 기능 유지 여부를 분석해야 한다
- 질문 28: 노드 DB와 PostgreSQL 높이 어긋남: A PostgreSQL cursor가 내보낸 높이의 정본, 시작 시 비교해 SetHead로 되감거나 기다림, reorg는 fork 지점까지 원천 표 되돌림, 내보내기는 (높이, hash) 멱등 / B 노드 저장 전에 내보내기를 묶음 / C 멈추고 운영자 처리
  - 답: A

### D5. wire 인코딩과 연결 (decided)
- 질문 6: wire 인코딩과 연결: A nodefeed식 JSON+base64, 요청마다 연결 / B 길이+종류+JSON 머리+원본 바이트 구간, 연결 유지, 큰 결과는 8MiB 프레임으로 나눔 / C gRPC
  - 답: 추천 B진행
- 질문 34: reopen
  - 답: D6 답(31회차): 실행부는 PostgreSQL 접근 권한이 필요 없고, PostgreSQL 쓰기는 indexer 내부 기능이 해야 한다. PostgreSQL에는 archive 수준으로 데이터가 쌓여 어떤 상황이든 쿼리로 추출할 수 있어야 한다
- 질문 36: 확인 3 최종본(4번 수정 포함): 실행부는 PostgreSQL 접근 없음, 블록 이벤트로 메모리에서 꺼내 IPC(D5 프레임, 연결 유지, 8MiB 분할)로 indexer에 보냄, indexer가 수신 goroutine과 표별 queue·writer로 PostgreSQL에 씀, visible_height, 기존 기능 표는 기존 단일 writer, indexer가 커밋 높이 정본이고 실행부는 SetHead로 맞춤, archive 수준 저장, 증명은 별도 tool이 상태 값으로 trie 재계산(E2 bootstrap이 전체 상태 적재), tool 구현은 E1 밖
  - 답: A

### D6. 상태 표 구조 (decided)
- 질문 7: 상태 표 구조(trie 노드·code는 hash key 삽입 전용으로 고정): A 현재 값 표 + 높이별 이력 표, 되돌릴 때 이력에서 복원 / B 이력 표만 / C 현재 값에 undo_track + 이력 표
  - 답: 추천 A로 진행
- 질문 30: reopen
  - 답: 확인 2에서 B. PostgreSQL에 모을 데이터는 이전 의도 그대로: 블록, 트랜잭션, receipt, state 4가지. state는 account list, account별 balance 변화 누적, contract address별 code, contract state 변화 누적으로 별도 테이블. 테이블끼리 서로 참조하는 키. 테이블마다 queue와 담당 goroutine writer. 조회 goroutine 별도, write/read liveness. 외부 요청은 websocket, rpc, graphql 담당 goroutine 분리
- 질문 31: PostgreSQL 표와 쓰는 방식: blocks, transactions, receipts, logs, accounts, balance_history, contract_code, storage_history(+storage_current), 표마다 queue와 담당 writer, visible_height. A E1부터 표마다 writer / B E1은 한 트랜잭션
  - 답: 추천 A로 진행. 단, 실행부는 postgreSQL 에 접근 권한이 필요 없다. 제안한 내용은 목적에 맞지 않는다. indexer 의 내부 기능에 의해서 처리되어야 하며, postgreSQL 에 쌓이는 DB 가 en node 기준으로 archive db 에 해당할 정도로, 데이터가 쌓여야 한다. 그로 인하여, 어떤 상황에 대해서든 쿼리를 통해 정보를 추출할수 있어야 한다.

### D7. token.metadata의 eth_call (decided)
- 질문 8: token.metadata의 eth_call: A E1에서는 balance.native만 실행 결과로, token.metadata는 RPC 유지 / B 실행부 호출 요청 추가 / C 둘 다 RPC 유지
  - 답: 추천 A

### D8. E1 시험의 기준 블록 (decided)
- 질문 9: E1 시험의 기준 블록: A testnet 구간 녹화·재생 / B 로컬 go-stablenet 체인 / C 둘 다, A는 CI 기본, B는 거래 많은 블록·경계 사례 opt-in
  - 답: 추천 C

### D9. E1 시험의 시작 상태 (decided)
- 질문 10: E1 시험의 시작 상태: A 녹화 때 구간이 건드리는 노드만 받아 둠, 로컬 체인은 genesis부터 / B 시작 블록 전체 상태 / C testnet 녹화는 RPC 비교만
  - 답: 추천으로 진행

### D10. 실행부가 가져오는 go-stablenet 범위 (peer, 검증, 실행) (decided)
- 질문 21: go-stablenet 조립 방식: A eth.New·node.New + 설정으로 끔(~30MB) / B 필요한 부분만 직접 조립, handler 복사(~23MB) / C A로 시작해 필요시 B
  - 답: 추천 A로 진행

### D11. 노드 클라이언트 DB 유지 여부 (decided)
- 질문 19: 노드 클라이언트 DB 유지 여부: A 노드 DB 유지 + PostgreSQL 저장 별도 구현 / B 노드 DB 자리에 PostgreSQL KV 어댑터 / C 노드 DB 없이 필요한 부분 재구현
  - 답: 나도 추천 A 가 맞는것 같아. 그렇지 않으면, 프로젝트 추가마다 어려움이 발생할것 같아.

### D12. PostgreSQL 테이블을 쓰는 주체 (decided)
- 질문 23: PostgreSQL 테이블 쓰는 주체: A 실행부가 원천 표, indexer가 기능 표(테이블별 writer까지 E1) / B IPC로 넘기고 indexer가 모두 / C A와 같은 경계, E1 기능 표는 기존 단일 writer
  - 답: C의 방향으로 갈수밖에 없는데, 이렇게되면 문제가 외부 RPC 를 통해서 읽는것과 다르지 않다는 점. 즉, Block 을 전파 받는것은 endpoint node 로 되어있어야 바로 전달 받을수 있기 때문에 delay가 줄어드는 장점이 있고, 받은 블록을 처리하는 코드를 수행함으로써, db 가 생성되는데, 결과적으로 이렇게되면, 또다시 발생하는 문제는 다음과 같다. 1. en node 에 의해 쌓이는 db 와 postgresql 에 의해 쌓이는 db 로 스토리지가 훨씬 많이 사용된다. -> en node 에 쌓이는 db 는 필요한 수만큼만 유지하고, 계속 지워야 한다. 이것을 indexer 의 프로젝트에서 파생된 스레드에 의해 관리되어야 한다. 2. indexer 프로젝트가 다른 indexer 보다 훨씬 빠른 처리를 위해 알고리즘을 생각하는것인데, en node 를 통해서 전달받은 블록과 그것을 처리하는 결과, 그리고 evm 에 의해 처리된 값등은 모두 db 에 write 되기전에 메모리에 남게되는데, 메모리에 남은 파일을 전달 받든지, 아니면, en node 로 동작하는 노드에의해 전파 받은 블록이 저장되는 db 파일이 존재할텐데, 그 파일을 바로바로 읽어서 파싱하여 처리하든지 고민이 필요해보인다. 어떤방식으로 해야 빠른 처리가 가능할까??
- 질문 32: reopen
  - 답: D6 답(31회차): 실행부는 PostgreSQL 접근 권한이 필요 없고, PostgreSQL 쓰기는 indexer 내부 기능이 해야 한다. PostgreSQL에는 archive 수준으로 데이터가 쌓여 어떤 상황이든 쿼리로 추출할 수 있어야 한다
- 질문 37: 확인 3 최종본(4번 수정 포함): 실행부는 PostgreSQL 접근 없음, 블록 이벤트로 메모리에서 꺼내 IPC(D5 프레임, 연결 유지, 8MiB 분할)로 indexer에 보냄, indexer가 수신 goroutine과 표별 queue·writer로 PostgreSQL에 씀, visible_height, 기존 기능 표는 기존 단일 writer, indexer가 커밋 높이 정본이고 실행부는 SetHead로 맞춤, archive 수준 저장, 증명은 별도 tool이 상태 값으로 trie 재계산(E2 bootstrap이 전체 상태 적재), tool 구현은 E1 밖 (D12: PostgreSQL 쓰기는 모두 indexer, 실행부는 IPC로 넘김)
  - 답: A

### D13. 실행 결과를 빠르게 넘기는 방법 (메모리 전달 vs 노드 DB 파일 읽기) (decided)
- 질문 24: 실행 결과를 빠르게 넘기는 방법: A 실행부 안에서 이벤트 구독으로 메모리에서 꺼내 원천 표에 COPY로 쓰고 NOTIFY, indexer는 알림 받아 읽음, pipeline / B A + IPC로 직접 흐름 / C 노드 DB 파일 파싱
  - 답: 추천 A
- 질문 33: reopen
  - 답: D6 답(31회차): 실행부는 PostgreSQL 접근 권한이 필요 없고, PostgreSQL 쓰기는 indexer 내부 기능이 해야 한다. PostgreSQL에는 archive 수준으로 데이터가 쌓여 어떤 상황이든 쿼리로 추출할 수 있어야 한다
- 질문 38: 확인 3 최종본(4번 수정 포함): 실행부는 PostgreSQL 접근 없음, 블록 이벤트로 메모리에서 꺼내 IPC(D5 프레임, 연결 유지, 8MiB 분할)로 indexer에 보냄, indexer가 수신 goroutine과 표별 queue·writer로 PostgreSQL에 씀, visible_height, 기존 기능 표는 기존 단일 writer, indexer가 커밋 높이 정본이고 실행부는 SetHead로 맞춤, archive 수준 저장, 증명은 별도 tool이 상태 값으로 trie 재계산(E2 bootstrap이 전체 상태 적재), tool 구현은 E1 밖 (D13: 실행부 안에서 블록 이벤트로 메모리에서 꺼내 IPC로 흘려보냄)
  - 답: A

### D14. 노드 DB를 필요한 만큼만 유지하는 방법 (decided)
- 질문 25: 노드 DB를 필요한 만큼만 유지: A E1에 path 방식 + 정리 goroutine / B E1은 path 방식만, 블록 정리는 E3, E1에서 증가량 측정 / C hash 방식 + 주기적 정지 정리
  - 답: 주천 B로 진행

### D15. E1 완료 기준과 속도 목표 (decided)
- 질문 29: E1 완료 기준: 기능 기준 1~5(녹화 구간에서 블록·거래·receipt·로그·native 잔액 읽기 결과 0건 차이, 노드가 모든 블록 수락, trie 비교와 E0 reflect 결과 블록마다 일치, 로컬 체인에서 내보내기 재시작·노드 비정상 종료·reorg 뒤 원천 표 일치, 증가량 기록) + 속도 목표: A 절대값 / B 같은 testnet 블록에서 기존 RPC 수집보다 블록 생성→API 노출 p50·p99가 작음 / C 측정만
  - 답: 기능 기준 1~5 맞음, 속도 목표 추천 B로 진행

### D16. archive 쿼리와 표별 writer의 완료 기준 추가 (decided)
- 질문 39: 완료 기준 추가: 6 archive 쿼리(구간 모든 높이·건드린 계정·슬롯의 PostgreSQL 이력 값 = 노드 상태, 128블록 밖은 E0 결과와 대조), 7 표 writer 하나를 늦추거나 죽여도 visible_height 위 데이터가 보이지 않고 재시작 후 결과 일치. A 추가 / B 수정
  - 답: A로 진행.

### D17. 증명 tool을 만드는 시점 (decided)
- 질문 40: 증명 tool 시점: A E2에서 bootstrap 검증 도구로 먼저(root 재계산 비교), 증명 API는 나중 / B E2에서 증명 API까지 / C E5 이후
  - 답: A 진행.

### D18. D9 시작 상태를 지금 구조에 맞추기 (decided)
- 질문 41: D9 시작 상태 맞추기: 녹화 때 건드린 trie 노드·code + 건드린 계정·슬롯의 구간 시작 직전 값 기록, 실행부 노드 DB에 노드·code 적재 후 부모 상태에서 시작, indexer 상태 표에 시작 직전 값을 초기 행으로, 로컬 체인은 genesis를 양쪽에. A / B
  - 답: A 진행.

### D19. 거래별 상태 변화를 얻는 방법 (tracer 대신) (decided)
- 질문 45: 거래별 상태 변화를 얻는 방법: A 다시 실행 때 state.Database 감싸기, 거래마다 IntermediateRoot로 UpdateAccount/UpdateStorage/Delete* 기록, Finalize 뒤 블록 단위 변화, 마지막 root = header root로 빠짐없음 확인, D1 trie 비교는 시험 대조용 / B tracer 수정 / C 거래마다 trie 비교
  - 답: 추천 A로 진행

### D20. 표 이름과 writer 단위 (decided)
- 질문 46: 표 이름과 writer 단위: A state_ 접두, writer 7개(blocks, transactions, receipts, logs, accounts[이력+지금 값], storage[이력+지금 값], contracts[주소별 + hash별 code]) / B 표마다 writer 10개 / C 사용자 이름 그대로, balance.native 표 미사용
  - 답: 추천 A

### 주제 밖 답변
- 질문 11: 확인 1. E1 목표와 하지 않을 것 (E2~E6 단계, token.metadata 호출 이전, Pebble 지원, RPC 경로 변경을 하지 않을 것으로 나열)
  - 답: 해야하는것이 왜 하지 얺을것으로 들어갔어?
- 질문 12: 확인 1 재질문: E1 다음 단계에서 할 일 6개 / 계획하지 않은 일 2개로 나눠 맞는지
  - 답: 최종본으로 다시 정리해줘. 지금 나눠서 질문해서, 오해가 생길수 있어.
- 질문 13: 확인 1 최종본: 목표, E1에서 만드는 것(D1~D9), 다음 단계 할 일, 계획하지 않은 일이 맞는지
  - 답: 어느정도 맞는데, 일부 의도와 달라. 1. go-stablenet을 고치지 않고 import 한 별도 프로세스에서, go-stablenet 의 모든 코드를 import 하면 빌드를 위해서 사이즈가 너무 커진다. go-stablenet 의 모든 코드가 필요한것이 아니라, peer 를 통해, 블록을 전파 받는부분과 관련된 부분이 필요하다. 즉, 다른 peer client 에는 우리가 정상적인 peer로 보이고, 그에 따라 블록을 전파 받을수 있어야 하며, 전파 받은 블록을 검증하고 evm 통해 실행하여 db 에 저장하는 부분에 있어서도, 기존의 db 에 저장하는 방식이 아니라, postgresql 에 저장하되, 각각의 테이블 담당자가 스스로 관리하는 테이블과 관련된 부분에 대해서만 저장하는 방식이기 때문에, import 로 그대로 사용하는 코드와 변경해서 다시 구현해야하는 코드가 섞인다고 볼수 있다. 2. 이미 header 가 indexer 의 ipc 로 받아야 하는 정도의 block 은 유지하고 있으면, 문제가 되지 않는다. 그러면 ipc 를 통해 db 를 전달받지 않을수 있다. 단, 이렇게 구현함에 있어서 검토할 사항이 하나 있는데, 이렇게하는 경우, 기존의 db 에 저장하는 코드까지 필요하고 그것은 위에서 설명한 1번의 내용과 충돌이 발생한다. 따라서, indexer 가 갖고 있는 db 기준으로 정보를 제공하는것보다, 유지보수 및 관리, 그리고 확장등의 관점에서 훨씬 효율적이고 편하게 동작하기 위해서는 node client 에서 지원하는 db를 저장 하고 유지하는 원래 기능을 유지하고, indexer 의 db 에 저장하는 기능을 별도로 구현해야 할수 있다. 이부분에 대한 분석 검토가 필요하다. 나머지는 맞다.
- 질문 35: 확인 3 전체 데이터 흐름 최종본
  - 답: 4번에서 Merkle 증명의 원리를 활용하는 별도의 tool 이 존재하면, 그 tool을 통해, db의 내용으로 증명을 만들어볼수 있지 않아?
- 질문 42: 마지막 확인: 목표 한 문장
  - 답: B. 상태 변화만 ipc 로 보낸다는 의미를 명확하게 예시를 들어서 설명해줘.
- 질문 43: IPC로 보내는 것의 예시(블록 원문, receipt 원문, 블록 단위 상태 변화 목록)와 바뀌기 전 값 포함 여부
  - 답: data 관점으로 접근하고, 실제 상태변화는 transaction 에 의해서 변경되는것이 잖아? (블록 리워드에 대한 balance 변화를 제외하고.) 결국 transaction 에 대한 기록이 모든 상태를 생성하고 만들수 있잖아? genesis 블록의 내용(즉, 0번블록)이 최초의 상태를 만들고, 그 다음부터 transaction 으로 어떤 상태 변화를 요청하느냐에 따라서, 계정에 자산이 증가/감소도 하고, 컨트랙트도 새로 생기고, 컨트랙트의 함수를 실행하여 컨트랙트 내부의 상태도 업데이트 되고,.. 모든 것을 이렇게 처리가 되고, 그것들이 누적되어서 최신 블록으로 부터 가장 최근의 현재 상태를 알수 있는것인데, 가장 최근의 상태만 알고 싶으면 rpc 통해서 쿼리를 하면되지, postgreSQL 을 통해, 데이터를 뭣하로 누적하겠어? 운영 레벨에서 보면 어느 시점의 날짜, 혹은 어떤 블록 높이, 혹은 특정 계정 주소에 대한 정보 조회 혹은 블록 구간 사이에 자금의 이동 파악 및 인위적인 행위와 정상행위의 구분, 이런것들을 조회하고 찾아보려고 할때가 많기 때문에 postgreSQL 을 통해 정보를 컬럼마다 누적하고, 최신의 값을 유지하는 테이블도 따로 두어, 최대한 데이터를 잘 활용할 수 있도록 준비하려고 하는 요구사항이 많고, 강한다. 지금 예시로 작성해준것은 비슷한것 같으면서도 이질감이 들고, 너의 이해가 나와 정확하게 일치하는지도 확인이 되지 않는다. 따라서, 설명을 기반하여 작업을 진행한다.
