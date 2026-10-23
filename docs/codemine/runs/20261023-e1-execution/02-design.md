# E1 design: StableNet 실행부와 indexer 표별 writer (2판)

입력:
- [01-discovery](01-discovery.md): 10/23 승인
- [결정 기록](../../../decisions/20261023-e1-design/decision.md): D1~D20과 데이터 관점

D19의 2회 실행과 D20의 writer 7개를 유지한다. 1차 실행은 정상 import, 2차 실행은 상태 변화 기록이며 노드 상태를 다시 commit하지 않는다.

이 문서는 승인된 E1 설계다. 2026-10-24에 사용자가 구현·검증·추천 결정을 위임했다. [구현 계획](03-plan.md)이 연결 4개, 공개·reorg·fresh 경계와 기록 검증을 구체화한다. 현재 진행은 [체크리스트](checklist.md)를 따른다. 과거 판본과 검토 이력은 Git history로 확인한다.

표기:
- **확인**: 코드를 읽었거나 돌려 본 것
- **추정**: 해석
- **미확인**: 확인하지 못한 것

숫자 가운데 잰 것은 출처를 적었다. 출처가 없는 숫자는 추정이다.

---

## 1. 배경과 범위

E1은 StableNet 블록을 별도 실행부에서 go-stablenet 규칙으로 실행하고, 그 결과를 indexer가 PostgreSQL에 archive 수준으로 쌓는 단계다. PostgreSQL에 쌓는 이유는 이력이다. 날짜, 높이, 주소, 블록 구간으로 자금 이동과 상태 변화를 쿼리하고 행위를 분석하려는 것이다(43회차). 최신 상태만 필요하면 RPC로 충분하다.

문서는 데이터에서 출발한다. 2장~4장은 데이터가 어디서 생기고, 어디에 얼마나 머물고, 어디로 가는지 정한다. 5장~6장은 그 흐름에 맞는 구조와 알고리즘을, 7장은 자원 상한을, 9장은 기능별 검증을 정한다.

**E1에서 시작하는 조건.**
- E1 구현은 녹화 fixture와 genesis 기반 EVM 라이브러리 검사로 검증한다(D8, D9, D18). 실제 노드 서비스·peer·성능 검사는 보류하며 별도 증거로 관리한다.
- 운영 노드를 처음부터 띄우는 bootstrap은 E2다. 그래서 E1의 실행부는 아직 testnet head에서 바로 운영을 시작하지 못한다.
- 속도 측정(D15)은 현재 보류다. 추후 승인된 측정에서는 실행부 노드를 snap sync로 head 근처까지 맞춘다. 그 경우 처음 보는 계정은 PostgreSQL에 이전 이력이 없으므로, 그 계정의 첫 행에 `baseline=true`를 단다(4.2).

하지 않는 것은 decision.md 4장과 7장 그대로다. E2~E6, 계약 호출을 실행부로 옮기는 일, Pebble 지원, 기존 RPC 경로 변경이 여기에 들어간다.

---

## 2. 데이터 목록

| 데이터 | 생기는 곳 | 크기 (StableNet) | 성질 |
|---|---|---|---|
| header, body | peer 전파 → 노드 검증 | 블록당 1~수 KB (추정) | 변하지 않는다. reorg로 버려질 수 있다 |
| receipt, 로그 | 노드 실행 | 거래당 수백 B (추정) | 변하지 않는다 |
| 거래별 상태 변화 | 실행부가 다시 실행하며 기록(5.2) | 거래 블록당 계정 약 9개, 슬롯 1~7개 (E0 측정) | 전후 값 쌍 |
| 블록 단위 변화 | WBFT `Finalize` | 블록당 계정 수 개 (E0) | 거래에 속하지 않는다 |
| contract와 code | 계약 생성 | 계약당 수 KB~24KB | code는 hash가 key이고 변하지 않는다 |
| trie 노드 | 노드 DB | 거래 블록당 9~11KB (E0) | 노드 DB에만 둔다(D3) |

높이 h의 상태는 genesis 상태에 높이 1부터 h까지의 변화를 차례로 적용한 것이다(43회차). 변화는 두 종류다. 거래가 만든 변화와, 블록 단위 변화(WBFT `Finalize`: base fee 분배, system contract 전이, epoch)다. PostgreSQL은 이 변화를 컬럼마다 전후 값으로 쌓고, 지금 값은 따로 둔다.

---

## 3. 데이터가 머무는 곳과 움직이는 길

```
StableNet peers ─ eth/68 NewBlock (D11, 확인)
   ▼
[실행부  x/stablenet/executor, go-stablenet go.mod, PostgreSQL 접근 없음]
   node(eth.New) ─ WBFT 검증 ─ 실행 ─ 노드 DB(path)          header·body·receipt·trie가 머문다
      │ ChainHeadEvent → 깨우기만 함 (5.3)
   exporter: canonical 높이를 차례로 걸으며
      다시 실행(감싼 state.Database, D19) → 거래별 변화, Finalize 변화, root = header 확인
      │ 결과를 실행부 spool에 append (7장, 노드를 막지 않음)
      ▼ BlockResult 프레임 (execwire, D5)
[indexer]
   receiver → assembler(블록 단위 조립)
      ├ queue blocks       → writer → blocks
      ├ queue transactions → writer → transactions
      ├ queue receipts     → writer → receipts
      ├ queue logs         → writer → logs
      ├ queue accounts     → writer → state_account_changes + state_accounts
      ├ queue storage      → writer → state_storage_changes + state_storage
      ├ queue contracts    → writer → state_contracts + state_code
      └ 기존 기능 writer(주소 색인, 토큰, AA …)  ← 기존 단일 writer, 핵심 표는 쓰지 않음
   watermark: visible_height = min(모든 writer cursor) → meta, outbox relay 문 열기, Committed(h)
   조회(port, visible_height 필터) ─ GraphQL, JSON-RPC, WebSocket goroutine
```

**머무는 시간.**
- **노드 DB.** E1에서는 지우지 않는다. 블록 정리는 E3다(D14). 상태는 path 방식이라 디스크에는 최신 하나만 남고, 메모리 계층은 128개다(확인, `triedb/pathdb/database.go:38`).
- **실행부 spool.** indexer가 Committed(h)를 보낼 때까지 h의 결과를 남긴다. 크기에 상한을 둔다(7장).
- **indexer 메모리.** assembler와 queue에 상한을 두고, 모든 writer가 쓰면 버린다.
- **PostgreSQL.** 영구히 남긴다. 이력 표는 높이 범위로 partition을 나눈다.

---

## 4. PostgreSQL 표 (D6, D20)

### 4.1 핵심 표

`blocks`, `transactions`, `receipts`, `logs`는 지금 schema와 `pkg/core/model` codec을 그대로 쓴다. API가 읽는 모양이 RPC 수집과 같아진다.

실행 수집 모드에서는 표마다 writer가 쓴다. 기존 기능 writer는 이 표들을 쓰지 않는다. 표별 writer는 `BeginBlock`을 쓰지 않는다. 대신 `indexer.undo`를 끈 트랜잭션에서 `undo_height`를 행의 높이로 직접 넣는다.
- `undo_height_guard`는 `indexer.undo=on`일 때 기존 행의 수정과 삭제만 막는다(확인, migration 0012:21-28). 그래서 이렇게 넣는 것이 허용된다.
- 같은 높이의 `undo_log`가 지워지는 문제(반증 CR-06)도 생기지 않는다. 표별 writer가 `undo_log`를 쓰지 않기 때문이다.

### 4.2 상태 표 (migration 0014)

**`state_account_changes`**: 계정 필드의 변화를 컬럼마다 전후로 남긴다.

| 컬럼 | 타입 | 뜻 |
|---|---|---|
| height | bigint | |
| block_time | timestamptz | 날짜 쿼리용 |
| ordinal | bigint | baseline→거래 순서→Finalize의 실행 순서. tx_index=-1를 정렬에 사용하지 않는다 |
| tx_index | integer | 블록 단위 변화는 -1 |
| tx_hash | bytea null | |
| cause | smallint | 0 거래, 1 블록 단위(`Finalize`), 2 genesis, 3 시작 상태(D18) |
| address | bytea(20) | |
| nonce_before, nonce_after | numeric | |
| balance_before, balance_after | numeric | |
| code_hash_before, code_hash_after | bytea | |
| storage_root_before, storage_root_after | bytea | |
| extra_before, extra_after | numeric | StableNet `Extra`. blacklist와 authorized에 쓴다(확인, `core/types/state_account.go:37`) |
| existed_before, exists_after | boolean | 계정이 없던 상태와 있는 상태를 가린다. 처음 돈을 받은 EOA도 여기서 잡힌다 |
| storage_cleared | boolean | 초기 hard fork의 SELFDESTRUCT가 storage를 통째로 지웠다 |
| baseline | boolean | 이전 이력이 없는 계정의 첫 행(1장) |

- 기본 key는 (height, tx_index, cause, address)다.
- 색인은 (address, height desc)와 BRIN(block_time)이다.
- 같은 거래 안의 여러 변화는 첫 전 값과 마지막 후 값 한 행으로 합친다.
- base fee 분배와 system contract 전이는 둘 다 `Finalize` 안에서 일어난다. 그래서 cause 1 하나로 묶는다. 둘을 나누려면 `Finalize`의 내부 단계를 다시 구현해야 하는데, D11을 고른 이유와 맞지 않는다.

**`state_storage_changes`**: 컬럼은 (height, ordinal, block_time, tx_index, tx_hash, cause, address, slot, value_before, value_after, baseline)이다. 기본 key는 (height, tx_index, cause, address, slot)이고, 색인은 (address, slot, height desc)다. 계정의 `storage_cleared`가 참이면, indexer는 clear tombstone을 기록하고 공개 시점 조회와 rollback에서 clear 이전 슬롯을 되살리지 않는다.

**`state_accounts`**(지금 값): 기본 key는 address다. 컬럼은 nonce, balance, code_hash, storage_root, extra, first_height, last_height, last_tx_hash다.

**`state_storage`**(지금 값): 기본 key는 (address, slot)이다. 컬럼은 value, last_height다.

**`state_contracts`**: 주소별 contract다. 기본 key는 address이고, 컬럼은 code_hash, created_height, created_tx_hash, creator, destroyed_height다.

**`state_code`**: 기본 key는 code_hash이고, 컬럼은 code, first_height다. 삽입만 한다.

**`exec_cursors`**: writer마다 (name, generation, height, hash, digest)를 둔다. writer는 자기 표를 쓰는 트랜잭션에서 이 값을 함께 갱신한다. `exec_parts`의 블록별 완료 기록으로 연속성·동일 입력을 확인하며 최고 높이만으로 추정하지 않는다.

**`exec_rewind`**: 되돌리기가 진행 중일 때만 (target)을 둔다(5.6).

**공개 control**: visible height/hash, generation, rewind 상태를 유지한다. 공개 transaction이 정확히 일치하는7개 결과와 기능 처리를 확인하고 새 fresh 검사를 통과한 뒤 갱신한다. API와 source ACK의 유일한 완료 경계다.

**partition.** `*_changes` 표는 `execution.partition_blocks`(기본 1,000,000블록, 추정값)마다 partition을 만든다. writer는 다음 partition이 필요해지기 전에 미리 만든다.

### 4.3 키로 잇기

- `transactions.block_number` → `blocks`
- `receipts.tx_hash` → `transactions`
- `logs`(block_number, log_index) → `receipts`
- `state_*_changes`(height, tx_index, tx_hash) → `transactions`
- `state_accounts.code_hash` → `state_code`
- `state_contracts.address` → `state_accounts`

외래 key 제약은 걸지 않는다. writer마다 따로 커밋하기 때문이다. 참조가 맞는 것은 `visible_height` 아래에서만 보장한다.

### 4.4 undo 등록

상태 표와 `exec_*`는 `TestEveryTableIsUndoTracked`의 `untracked`에 이유를 달아 넣는다. 이 표들은 자기 writer가 높이로 되돌린다(5.6). `undo_by_height`에 넣지 않는 이유가 있다. 기능 writer의 `RollbackTo`가 `undo_block`으로 다른 writer의 표까지 지우지 않게 하려는 것이다(반증 CR-06). 핵심 표 네 개는 기존 등록(`undo_by_height`)을 그대로 둔다. 실행 수집 모드에서는 5.6의 순서로 지운다.

---

## 5. 구조와 알고리즘

### 5.1 모듈

| 모듈 | 위치 | 책임 | 새로 만드는 이유 |
|---|---|---|---|
| execwire | `execwire/` (별도 go.mod, 표준 라이브러리만) | 프레임, 메시지, version | 실행부와 indexer가 함께 import한다. 어느 쪽 모듈에 두어도 다른 쪽의 go-ethereum 버전이 끌려온다(work log 3.109) |
| 실행부 | `x/stablenet/executor/` (별도 go.mod) | 노드 조립, canonical 높이 걷기, 다시 실행과 기록, spool, IPC 서버, 시작할 때 되감기 | indexer 모듈에 둘 수 없다(같은 이유) |
| 수신과 분배 | `pkg/execution/` | IPC client, assembler, 표별 queue, watermark, 되돌리기 조정 | 기존 `pkg/fetch`는 블록 하나를 writer 하나에 넘기는 구조라, 표별 writer를 담지 못한다 |
| 상태 표 writer | `pkg/storage/postgres/execstate.go` + migration 0014 | 상태 표 COPY, cursor, 높이 단위 되돌리기 | 표 schema는 postgres 패키지가 소유한다 |
| 핵심 표 writer | `pkg/storage/postgres`의 기존 block, receipt 쓰기 함수 재사용 | 높이를 직접 넣는 쓰기 경로 | 함수는 재사용하고, 트랜잭션 방식만 4.1처럼 바꾼다 |
| 조회 필터 | `pkg/storage/postgres` 읽기 | 실행 수집 모드에서 모든 핵심 표와 상태 표 읽기에 `높이 ≤ visible_height` 조건을 붙인다(5.4) | 읽기 경로의 소유자가 postgres 패키지다 |
| 연결 | `pkg/app` | `indexer.mode: execution`, socket 경로, 연결 수 검사 | 구성 소유자 |

### 5.2 실행부의 다시 실행과 기록 (D19)

블록 h 하나에 대해 이렇게 한다.

1. canonical hash `bc.GetCanonicalHash(h)`와 블록을 읽는다(5.3).
2. 부모 상태를 연다. 감싼 `state.Database`를 `bc.StateCache()` 위에 두고 `state.New(parent.Root, wrapped, nil)`로 만든다. 감싼 쪽의 `OpenTrie`와 `OpenStorageTrie`는 원래 trie를 감싼 trie를 돌려준다. `CopyTrie`도 감싼 쪽이 처리한다. 이 상태 객체는 commit하지 않으므로 노드 DB에 쓰지 않는다.
3. 거래마다 `core.ApplyTransaction`을 부르고, 이어서 `statedb.IntermediateRoot(true)`를 부른다.
   - 그러면 바뀐 객체마다 감싼 trie의 `UpdateAccount`, `DeleteAccount`, `UpdateStorage`, `DeleteStorage`가 불린다(확인, `core/state/statedb.go:572`, `state_object.go:307,316`).
   - 감싼 쪽은 바꾸기 직전 값을 원래 trie에서 읽어 전 값으로 삼는다.
   - fee payer, NativeManager, precompile의 쓰기도 모두 상태 객체를 거치므로 여기서 잡힌다.
4. 마지막 거래 뒤 엔진의 `Finalize`를 적용하고 `IntermediateRoot`를 한 번 더 부른다. 이때 잡힌 것이 블록 단위 변화(cause 1)다.
5. **실행 결과 확인.** 4에서 나온 root가 header의 state root와 같아야 한다. 다르면 내보내지 않는다. 이 확인만으로 기록 목록의 무누락을 입증하지 않는다. 실제 trie 갱신은 전달하면서 기록만 버리는 fault와 독립 양방향 trie 비교를 검사한다([구현 계획 2.6](03-plan.md#26-실행-기록과-검증)).
6. receipt는 다시 실행한 결과가 아니라 노드가 저장한 것(`bc.GetReceiptsByHash`)을 consensus RLP로 보낸다. 다시 실행한 receipt와 노드 receipt의 root가 같은지도 확인한다.
7. 계정은 주소순, 슬롯은 (주소, 슬롯)순으로 정렬한다. 그래서 같은 블록이면 같은 바이트가 나온다.

**비용(미확인).** 거래마다 root를 계산하므로 hash 계산이 거래 수만큼 늘어난다. E0에서 블록 하나 실행은 1ms 안팎이었다. 거래가 많은 블록의 비용은 로컬 체인 시험에서 잰다.

**D1의 trie 비교**는 시험에서만 쓴다. 같은 블록의 블록 단위 합(거래별 + cause 1)이 노드 DB의 두 root 사이 trie 비교와 같은지 대조한다. 이 대조에서는 반증 CR-03이 지적한 두 가지를 반영한다. 하나는 양방향으로 비교해 삭제를 찾는 것이고, 다른 하나는 storage trie까지 내려가 비교하는 것이다.

### 5.3 높이를 걷는 방식 (반증 CR-07, CR-08)

- 노드가 이벤트를 보낼 때 막히지 않게 한다.
  - `event.Feed.Send`는 받는 쪽이 받을 때까지 막힌다. 노드는 chainmu를 잡은 채 보낸다(확인, `core/blockchain.go:1552`).
  - 그래서 `ChainHeadEvent` 구독은 항상 받기만 하는 goroutine 하나가 맡는다. 이 goroutine은 받은 이벤트를 크기 1인 채널에 넣되, 채널이 차 있으면 그냥 넘어간다. 신호를 하나로 합치는 셈이다.
- 이벤트는 깨우는 신호로만 쓴다. reorg 때 새 가지의 중간 블록에는 `ChainEvent`가 없기 때문이다(확인, `blockchain.go:2230-2238`). exporter는 깨어날 때마다 "마지막으로 내보낸 높이 e + 1"부터 노드 head까지 canonical hash를 차례로 읽는다.
- reorg는 이렇게 찾는다. 높이 e의 canonical hash가 내보낸 hash와 다르면, 내보낸 hash 기록을 거꾸로 따라가 fork 지점 f를 찾는다. 이 기록은 최근 128블록을 남긴다. 그다음 Rewind(f)를 보내고 f부터 다시 걷는다.
- 128블록보다 깊은 reorg는 다루지 않고 멈춘다. 기능 표 undo의 한도와 같다(반증 CR-12). WBFT에서는 일어나지 않을 것으로 본다(추정).

### 5.4 표별 writer와 visible_height

- **assembler.** 블록을 완성하면 각 queue에 넣는다.
- **writer.** 자기 cursor보다 높은 연속 높이만 꺼낸다. 이미 쓴 generation·height·hash·digest가 모두 같은 결과만 재사용한다. 높이만 같다고 건너뛰지 않는다.
  - 묶음 크기: 따라잡을 때는 queue 상한 이하, head를 따라갈 때는 1블록이다.
  - 자기 표에 COPY로 쓰고, 같은 트랜잭션에서 cursor를 갱신하고 커밋한다.
  - 상태 writer는 COPY 전에 같은 트랜잭션에서 그 블록이 건드린 key의 지금 값을 한 번에 읽는다(`= ANY`). 그리고 실행부가 보낸 전 값과 비교한다. 다르면 그 블록을 쓰지 않고 멈춘다. 이전 행이 없는 key는 `baseline=true`일 때만 받아들인다.
- **watermark.** 표별 저장은 미공개 durable 자료다. 같은 generation·height·hash의 7개 결과가 모인 뒤 기능 writer의 transaction에서 기능 표·outbox·공개 높이를 원자적으로 commit한다. 바로 직전의 새 fresh 검사와 응답 후 epoch fence를 통과해야 공개·ACK한다([구현 계획 2.2](03-plan.md#22-미공개-저장과-공개-commit)).
- **조회.** 실행 수집 모드의 불변 행은 `높이 ≤ visible_height`로 제한한다. hash·주소·로그·통계에도 적용한다. 작업용 최신 상태의 last_height만 필터링하지 않고 이력에서 공개 시점의 값을 읽는다. 삭제·clear·baseline을 구분한다. 읽기 transaction과 rewind의 advisory fence는 [구현 계획 2.3](03-plan.md#23-공개-높이와-최신-값-조회)을 따른다.
- **이벤트(반증 CR-11).** 기능 writer는 지금처럼 자기 트랜잭션에서 outbox에 쓴다. outbox relay는 높이가 visible_height 이하인 항목까지만 내보낸다. 그래서 이벤트가 API보다 먼저 나가지 않고, 같은 이벤트를 두 번 내보내지도 않는다.

### 5.5 기존 기능 writer와의 분리

실행 수집 모드에서 기존 `pkg/fetch` writer는 기능 표만 쓴다.
- `applyBlock`은 `SetBlock`과 `storeReceiptsSequential`을 건너뛰는 option을 받는다. 기본값은 꺼져 있다.
- 블록 데이터는 assembler가 만든 `model.Block`과 receipt를 받는다.
- `balance.native`는 실행 결과의 계정 balance 후 값을 받는다. 연결은 `facts.Source.BindFeatures` 방식으로 한다(확인, `pkg/source/facts/state.go:61-72`). 같은 잔액이 `balances`와 `state_accounts`에 모두 있게 되는 중복은 E1 뒤에 정리한다(D20).

**확인할 것.** 기능 핸들러가 같은 트랜잭션 안에서 핵심 표 행을 다시 읽는지 확인해야 한다. 계획의 첫 단위에서 코드 그래프로 찾는다. 읽는다면 그 port를 assembler 데이터로 바꾼다.

### 5.6 되돌리기

[구현 계획 2.4](03-plan.md#24-reorg와-재시작)를 따른다. 첫 단계에서 새 generation과 durable rewind 상태로 공개·새 조회를 막고, 진행 중 작업을 취소·join한 뒤 표와 기존 기능 undo를 되돌린다. 완료를 확인한 뒤 공개 높이/hash와 조회를 재개한다. 중간 종료 뒤에는 이 절차부터 재개한다. 일부 표가 바뀐 상태에서 옛 공개 높이로 조회를 허용하지 않는다. 128블록보다 깊거나 복구 자료가 부족하면 멈춘다.

---

## 6. 성능

### 6.1 지연 예산 (head를 따라갈 때, 블록 하나)

| 구간 | 예상 | 근거 |
|---|---|---|
| 블록 생성 → 실행부 수신 | 망 지연 | 전파로 받는다(D11) |
| 노드 import | 1ms 안팎 + 디스크 | E0 실행 측정. 디스크는 재지 않았다 |
| 다시 실행과 기록 | 1~수 ms | E0 측정에 거래별 root 계산이 더해진다. 재지 않았다 |
| spool append + IPC | 1ms 미만 | 추정 |
| 표별 writer 커밋 | writer 7개가 작업하고 DB transaction은 최대2개 동시 실행 | 재지 않았다. PostgreSQL group commit에 기댄다 |
| watermark → API, 이벤트 | 1ms 미만 | 추정 |

- 지금 RPC 경로는 polling(50ms)과 RPC 왕복을 거치고, 5 HTTP/s 상한을 받는다.
- 다만 `token.metadata`는 E1에서 RPC를 유지한다(D7). 그래서 계약을 만드는 블록은 그 RPC가 끝나야 `visible_height`가 오른다(반증 CR-14). 속도 비교에서는 이런 블록을 따로 집계해 보고한다.

### 6.2 pipeline

- 노드 import, exporter, IPC 전송이 각각 따로 돈다.
- indexer에서는 writer 7개가 처리한다. 표별 DB transaction 동시성은2이고 기능 writer의 최종 공개가 별도로 원자성을 보장한다.
- 따라잡을 때는 writer가 여러 블록을 COPY 한 번으로 묶어 커밋 수를 줄인다.

---

## 7. 자원 상한

| 자원 | 상한 | 넘으면 |
|---|---|---|
| IPC 프레임 | 8MiB | part로 나눈다 |
| 실행부 spool (확인 전 결과, 실행부 로컬 디스크) | `execution.spool_bytes` (기본 4GiB, 추정값) | 실행부가 노드 p2p를 멈춘다(`Server.Stop`). 그러면 import가 멈추고 상태 계층이 밀려나지 않는다. indexer가 따라오면 다시 연다 |
| exporter의 부모 상태 | pathdb 계층 128개 | spool 덕분에 exporter는 indexer를 기다리지 않는다. exporter 자체가 128블록 넘게 밀리면 멈추고, 다시 시작할 때 되감는다 |
| indexer assembler | 64블록, 256MiB | IPC 읽기를 멈춘다 |
| 표별 queue | 64블록, 64MiB | assembler가 기다린다 |
| writer 묶음 | queue 상한 이하 | 나눠 커밋한다 |
| PostgreSQL 연결 | 기존 pool 4개. API 예약1, 동시에 part transaction 최대2, 나머지는 공개·관리 | writer 7개는 연결을 상시 점유하지 않는다. permit·취소·반환과 공정성은 구현 계획 2.1을 따른다 |
| 노드 DB | E1은 상한 없음, 증가량만 잰다 | E3에서 정리한다(D14) |

- 모든 goroutine은 ctx로 멈추고, 소유자가 join한다. 모든 대기에는 ctx와 timeout이 있다.
- **socket(반증 CR-24).** 실행부는 시작할 때 이미 있는 socket 파일에 연결해 본다. 듣는 프로세스가 없으면, 자기 소유이고 권한이 0600인 경우에만 지우고 다시 만든다. 듣는 프로세스가 있으면 시작을 거부한다.

---

## 8. 시나리오

### 8.1 정상

1. 블록 h가 전파되고 노드가 import한다.
2. head 이벤트가 exporter를 깨운다.
3. exporter가 h를 다시 실행하며 상태 변화를 기록하고, root를 확인한 뒤 spool에 쓴다.
4. BlockResult로 indexer에 보낸다.
5. 표별 writer와 기능 writer가 각각 커밋한다.
6. `visible_height`가 h로 오른다.
7. API와 이벤트에 h가 보인다.
8. indexer가 Committed(h)를 보내고, 실행부는 spool에서 h를 지운다.

### 8.2 시작

indexer가 Hello(committed = visible_height k)를 보낸다. 실행부는 노드 p2p를 시작하기 전에 다음을 판단한다. p2p를 켜기 전이라 노드 import가 돌지 않으므로, `SetHead`가 chainmu를 잡지 못해 실패하는 경우를 피할 수 있다(반증 CR-08).

- spool에 k+1부터의 결과가 있으면 그것부터 보낸다.
- spool에 없고 k의 상태를 열 수 있으면 k+1부터 다시 실행한다.
- k의 상태를 열 수 없으면 `bc.SetHead(k)`로 되감은 뒤 p2p를 연다.
- 노드 head가 k보다 낮으면 따라올 때까지 기다린다. 같은 hash인 높이는 건너뛴다.

**미확인.** pathdb에서 `SetHead`를 얼마나 깊게 할 수 있는지 아직 모른다. `StateHistory`는 되감기 상한(128)보다 크게 둔다.

### 8.3 실패와 경계

| 경우 | 동작 |
|---|---|
| 실행부가 죽음 | indexer가 다시 연결하고 Hello부터 시작한다 |
| indexer가 죽음 | 실행부는 spool에 계속 쌓는다. 상한에 닿으면 p2p를 멈춘다 |
| writer 하나가 실패 | 그 writer만 다시 시도한다. visible_height는 멈춘다. 계속 실패하면 opsguard 분류에 따라 멈춘다 |
| root 불일치(5.2의 5) | 실행부가 멈추고 원인을 남긴다 |
| 전 값 불일치(5.4) | indexer가 멈춘다 |
| reorg | 5.3으로 fork 지점을 찾고 5.6으로 되돌린다 |
| 128블록보다 깊은 reorg | 멈춘다 |
| 빈 블록 | Finalize 변화가 있는지 실행해 확인한다. 빈 결과인 writer도 연속 완료를 기록한다 |
| 계약 생성 또는 소멸 | `state_contracts`, `state_code`. 초기 hard fork의 SELFDESTRUCT는 `storage_cleared`로 남긴다 |
| epoch 블록 | 블록 단위 변화가 크다. part로 나눠 보낸다 |
| 디스크 부족 | 기존 opsguard가 처리한다. 실행부 디스크(노드 DB, spool)도 감시 대상에 넣는다 |

---

## 9. 검증

완료 기준 번호는 decision.md 5장을 따른다. 시험마다 "일부러 틀리게 만들면 실패하는지"를 함께 확인한다.

| 기능 | 검증 | 일부러 틀리게 하는 방법 | 기준 |
|---|---|---|---|
| execwire | 단위 시험, 파서 fuzz, part 왕복 | 길이 조작, 잘린 프레임 | |
| 거래별 기록(D19) | 녹화 구간과 로컬 체인의 모든 블록에서 root = header 및 독립 기록 대조 | 실제 trie 갱신을 유지하면서 journal 기록만 버림 | 3 |
| 대조(D1) | 블록 단위 합 = 양방향 trie 비교(storage 포함) | 슬롯을 0으로 만드는 블록, 슬롯만 바뀌는 블록, fee delegation 블록, NativeManager 거래 | 3 |
| 실행부 수락 | 모든 블록 수락 | header를 조작한 블록 | 2 |
| RPC 수집과 같음 | `TestRPCArchiveStorageParity` 틀로 두 경로의 port 읽기 비교 | 한 receipt 필드를 바꿈 | 1 |
| archive 쿼리 | 모든 높이와 건드린 key의 이력 값 = 노드 상태(128 안), E0 결과(밖) | 이력 행 하나를 지움 | 6 |
| 반쯤 쓰인 블록 | transactions writer만 h+1을 커밋한 상태에서 다음을 조회해 h+1이 보이지 않아야 함: `transaction(hash)`, 주소 색인, 로그 조회, 구독 이벤트, `LatestHeight` | 조회 필터를 끔 | 7 |
| 복구 | 로컬 체인: 내보내기 kill, 노드 비정상 종료, 되돌리기 도중 kill, reorg, 새 EOA가 생기는 블록의 reorg | | 4 |
| 재연결 중복 | writer 하나만 앞선 상태에서 indexer kill 후 재연결 | | 4 |
| 이벤트 순서 | blocks writer를 늦춘 상태에서 구독자가 h 이벤트를 받은 시각 ≥ visible_height가 h가 된 시각 | relay 문을 엶 | 7 |
| 증가량 | 블록당 노드 DB, spool, PostgreSQL 바이트. 전 값 컬럼이 차지하는 비율 포함 | | 5 |
| 속도 | testnet에서 두 경로를 동시에 돌려 p50, p99 비교. 계약 생성 블록은 따로 집계. 승인을 받은 뒤 실행 | | 속도 |
| 동시성 | 새 goroutine 시험은 `-race`, `-count=200`, `GOMAXPROCS=1`, goleak | | |

**기준 1의 비교 범위(반증 CR-15).** RPC JSON 경로와 binary 경로가 다를 수 있는 필드가 있다. BlobGasPrice, ContractAddress, Ext, EffectiveGasPrice다. `TestBinaryMatchesLiveJSON`(`x/stablenet/binary_test.go:46`)이 StableNet에서 이 차이를 고정한다. 계획의 첫 단위에서 그 시험이 허용하는 차이를 확인한다. 기존 fixture는 model codec 바이트를 그대로 비교하며 필드 차이 예외를 두지 않는다. 새 차이를 자동 허용하지 않고 원인을 고친다. 사용자에게 위임받은 구현 결정은 계획에 기록한다.

실행부 모듈은 자기 go.mod에서 시험하므로, CI에 실행부 job을 하나 더한다.

---

## 10. 계약과 호환성

- 새 설정: `indexer.mode: execution`, `execution.socket`, `execution.partition_blocks`, `execution.spool_bytes`. 기존 설정의 뜻은 바꾸지 않는다.
- migration 0014는 표를 더하기만 한다.
- 실행 수집 모드에서 Pebble을 쓰거나 정한 pool 4개 계약을 충족하지 않으면 시작을 거부한다.
- 기존 RPC 수집 경로는 바꾸지 않는다. 5.5의 option과 5.4의 조회 필터는 실행 수집 모드에서만 켜진다.
- execwire version은 1부터 시작한다. Hello에서 version이 다르면 거부한다.

---

## 11. 위험

| 위험 | 언제 알게 되나 | 대응 |
|---|---|---|
| 감싼 `state.Database`가 go-stablenet 내부 가정(타입 단언 등)에 걸림 | 첫 단위(녹화 구간 root = header) | 걸리는 곳을 찾아 감싸는 범위를 조정한다. 안 되면 원인을 기록하고 위임된 범위에서 호환 구현을 선택한다 |
| 거래별 root 계산 비용이 큼 | 로컬 체인의 거래 많은 블록 | 추가 측정은 보류하고 기능 정확성을 먼저 검증한다 |
| 외부 trie 노드로 노드 DB를 채워 부모에서 시작하지 못함(D18) | 녹화 시험 준비 | 녹화 구간 실행은 E0 방식 실행기로 하고, 노드 서비스는 실행하지 않고 라이브러리 fixture로 시작 상태를 검증한다 |
| pathdb `SetHead` 깊이 | 복구 시험 | 되감기 상한을 정한다 |
| 기능 핸들러가 핵심 표를 다시 읽음 | 첫 단위의 그래프 확인 | 그 port를 assembler 데이터로 바꾼다 |
| 기준 1의 필드 차이 | 첫 단위 | 차이의 원인을 분석하고 정확한 model codec parity를 유지한다 |
| p2p를 멈췄다 다시 열 때 peer 점수가 깎임 | 운영 | static peer를 쓰고, 멈춤이 얼마나 자주 일어나는지 잰다 |
| 실행부 프로세스 감시가 없음 | 운영 | E1은 Docker 감시를 쓴다 |

---

## 12. 현재 구현 상태

U01의 [wire 모듈](../../../../execwire/README.md)은 version1 framing, raw block/receipt segment, ordered journal, bounded assembly와 취소를 구현했다. 실행부·DB·앱 통합은 별도 단계다. 기능별 상태·RED/GREEN·PR은 [체크리스트](checklist.md)에 기록한다. 위험과 단위별 롤백은 [구현 계획](03-plan.md)을 따른다. 실제 노드 운영과 속도 측정은 보류다.
