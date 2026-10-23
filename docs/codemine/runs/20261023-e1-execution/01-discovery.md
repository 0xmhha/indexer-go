# E1 discovery: 실행 수집의 데이터와 현재 구조

출발점: [실행 수집 설계](../../../analysis/execution-ingest-design.md) 10장의 E1, 10/22까지의 결정(같은 문서 11장, work log 3.108, 3.109). 승인된 intent 문서는 없다.

이 문서는 E1 상세 설계 전의 조사 결과다. 무엇이 있는지, 데이터가 지금 어디에 있고 어떻게 움직이는지, 설계가 지켜야 할 원칙과 먼저 정해야 할 것을 적는다. 설계안(02-design)은 이 문서가 승인된 뒤 쓴다.

표기: **확인**은 코드를 읽어 확인한 것, **추정**은 코드에서 해석한 것, **미확인**은 확인하지 못한 것이다. 근거의 `file:line`은 조사 시점(main `40a91ff`, go-stablenet `4d17eea01`)의 위치다.

---

## 1. 준비: 코드 그래프

`tools/astgraph`로 두 저장소 전체를 AST 기반 그래프로 만들었다. 시험 코드는 포함하지 않는다.

| 대상 | 패키지 | 노드 | 간선 |
|---|---|---|---|
| indexer-go (`40a91ff`) | 102 | 7,416 | 35,753 |
| go-stablenet (`4d17eea01`) | 197 | 12,955 | 78,815 |

- indexer 노드: 함수 1,742, 메서드 2,921, 타입 1,038, interface 209. 간선: 호출 7,087, interface 호출 1,461, 참조 18,774, 구현 537.
- go-stablenet은 개발 도구가 쓰는 `golang.org/x/tools` 옛 버전 한 패키지가 Go 1.26에서 타입 검사에 실패했다. 실행 경로(`core`, `trie`, `triedb`, `consensus/wbft`)와는 관계없는 패키지다.
- 그래프는 저장소에 넣지 않는다. 같은 명령으로 다시 만든다: `(cd tools/astgraph && go run . -dir <module root> -out <dir>)`, 질의는 `ASTGRAPH=<dir>/graph.json python3 tools/astgraph/query.py ...`.

그래프로 호출 경로를 따라간 뒤 코드를 읽어 확인했다. 세 갈래(indexer 수집·저장, 수신·IPC, go-stablenet 실행·상태)로 나눠 읽었다.

---

## 2. 요구사항 분해

**목표.** StableNet 블록을 별도 프로세스(실행부)에서 go-stablenet 규칙으로 실행하고, receipt와 상태 변화를 indexer의 단일 writer가 PostgreSQL에 쓴다. 같은 블록 범위에서 RPC 수집과 실행 수집의 블록, 거래, receipt, 로그, 잔액이 같다(설계 10장).

**비목표 (E1에서 하지 않는 것).**
- peer에서 블록 받기, 원본 즉시 삭제(E3), 두 시작 모드와 전체 상태 bootstrap(E2), 테이블별 writer(E4), 다른 체인(E5), RPC 없는 canonical 판정(E6).
- 기존 RPC 수집 경로 대체. 유지하기로 했다(10/22).

**행위자.** indexer 프로세스(블록 공급, 검증, 저장), 실행부 프로세스(실행, 상태 보유), PostgreSQL, 운영자(두 프로세스 시작·감시), StableNet 노드(E1에서는 블록과 canonical hash 공급).

**기능 요구사항.**
1. indexer가 검증된 블록을 순서대로 실행부에 보낸다.
2. 실행부가 부모 상태 위에서 실행하고 state root, receipt root, gas, bloom을 header와 비교한다. 다르면 그 블록에서 멈춘다.
3. 실행부가 receipt, 바뀐 계정·슬롯·code, 새 trie 노드를 돌려준다.
4. indexer가 이 결과를 한 블록 트랜잭션 안에 쓴다. 상태 표도 undo 대상이다.
5. 잔액 기능이 RPC 대신 로컬 상태를 쓴다.
6. reorg 때 상태도 되돌린다(설계 6장).

**품질 속성.** 블록마다 결과가 같아야 한다(결정성). 결과가 header와 다르면 쓰지 않는다(fail closed). 메모리, 대기열, 프레임 크기에 상한이 있다. 실행부가 죽거나 늦어도 indexer가 무한히 기다리지 않는다. 지금의 durable ACK와 undo 규칙을 깨지 않는다.

**예외.** 실행부 없음·중단·버전 불일치, 부모 상태 없음, 결과 불일치, 프레임 초과, reorg, 실행부와 DB의 상태 높이 불일치(한쪽만 커밋된 경우).

---

## 3. 데이터: 지금 어디에 있고 어떻게 움직이나

### 3.1 블록이 커밋되기까지 (RPC 경로)

| 단계 | 데이터 (Go 타입) | 소유와 상한 | 근거 |
|---|---|---|---|
| source | `*model.Block`, `[]*model.Receipt` | `BlockWithReceipts`. RPC 한 묶음(`eth_getBlockByNumber`, `eth_getBlockReceipts`) | `pkg/source/source.go:45-54`, `pkg/source/rpc/rpc.go:142-210` |
| worker | `*fetchBlock{prepared, block, receipts, geth, gethReceipts}` | 같은 블록을 model과 go-ethereum 두 벌로 든다 | `pkg/fetch/fetched.go:31-50` |
| 순서 정렬 | `map[uint64]*jobResult` | 동시 처리 `2×workers`, `results` 채널도 같은 크기 | `pkg/fetch/scheduler.go:38-45,104` |
| writer | `writerCmd` | 채널 64개. 블록은 사실상 한 번에 하나 | `pkg/fetch/writer.go:32-39` |
| 블록 트랜잭션 | features가 받는 `*feature.Block{Model, Receipts, Geth, GethReceipts}` | 이벤트는 커밋까지 메모리에 모은다 | `pkg/feature/feature.go:44-52`, `pkg/fetch/index_block.go:40-89` |
| 커밋 | outbox 행, cursor, undo | `tx.Commit` 뒤 relay를 깨운다. ACK는 그 뒤 | `pkg/fetch/writer.go:154-245`, `outbox.go:88-103` |

`indexBlock`이 끝나면 reorder buffer에서 지워지고 slot이 풀리며 블록 두 벌이 garbage가 된다(확인, `scheduler.go:127-133`).

**설계 문서 2.3과 다른 점 (확인).**
- RPC 경로는 커밋 직전에 `HashAt`을 일반적으로 부르지 않는다. fresh 확인은 `state_prefetch`가 상태를 읽었을 때 `BeginBlock` 전에(`pkg/fetch/prepare.go:53-70`), 그리고 feed 경로의 `facts.ValidateCommit`에서만 한다.
- RPC 경로는 receipt root를 확인하지 않는다. 개수와 hash 연결만 본다(`rpc.go:239-249`). receipt root 확인은 binary 경로(`evmdata`, nodefeed)에만 있다.

설계 2.3은 이 두 점에 맞게 고쳐야 한다.

### 3.2 노드 상태를 읽는 곳

- `feature.StateReader`는 `ReadsState() bool` 표시뿐이다. 실제 읽기는 `feature.Deps`의 함수 필드다: `BalanceAt`, `BalanceAtHash`, `BalanceBatchAtHash`, `BlockAt`, `Contracts{CallContract, CodeAt}`와 hash 고정 변형(확인, `pkg/feature/feature.go:85-129`).
- 로컬 상태로 바꾸는 선례가 있다. `facts.Source.BindFeatures`가 이 함수들을 통째로 갈아 끼우고 genesis 잔액 resolver를 넣는다(확인, `pkg/source/facts/state.go:61-72`).
- `balance.native`는 처음 보는 계정의 부모 블록 잔액(`internal/features/balance/balance.go:230-257`)과 resync 때 잔액을 노드에 묻는다. 그 밖의 잔액 변화는 체인 profile의 native accounting 규칙으로 계산한다(StableNet: `x/stablenet/profile.go:39`).
- `token.metadata`는 계약을 만든 블록 기준으로 `CodeAt`, `CallContract`를 부른다(`internal/token/block_processor.go:241-280`).
- `checkNodeState`는 시작할 때 노드가 시작 높이의 상태를 갖는지 본다(`pkg/app/node_state.go:24-98`).

**해석 (추정).** 실행 결과에 블록 뒤 계정 잔액이 들어오면, 잔액 기능은 노드에 묻지도 계산하지도 않고 실행 결과를 그대로 쓸 수 있다. `token.metadata`의 `eth_call`은 로컬 상태 위에서 다시 실행해야 하므로 실행부에 call 요청이 필요하다. E1에 넣을지는 정해야 한다(5장 D7).

### 3.3 PostgreSQL

- 표: migration 0001~0013(블록, 거래, receipt, 로그, 주소 색인, 토큰, AA, DEX, 집계, records, undo, outbox, capture). **계정, nonce, code, storage, trie 노드를 담는 표는 없다**(확인). 상태 비슷한 것은 `balances`, `balance_history`와 `contract_creations.bytecode_size`뿐이다.
- 블록 트랜잭션: 결과를 돌려받지 않는 쓰기는 `pgx.Batch`에 모았다가 다음 문장 전이나 커밋 때 보낸다. 이 대기열에는 크기 상한이 없다(확인 `pkg/storage/postgres/pending.go:16-80`; 상한 없음은 추정).
- undo 두 방식(확인):
  - 삽입만 하는 표는 `undo_by_height_track`: `undo_height` 열, 다른 높이 행을 못 바꾸게 막는 trigger, 되돌릴 때 그 높이 행 삭제(migration 0012).
  - 나머지는 `undo_track`: AFTER trigger가 바뀌기 전 행을 jsonb로 `undo_log`에 남기고, 되돌릴 때 새것부터 다시 쓴다(0006). 최근 128블록만 남긴다(`store.go:298-310`).
- 새 표는 둘 중 하나로 등록하거나 `untracked`에 넣어야 한다. `TestEveryTableIsUndoTracked`가 막는다(`undo_test.go:17-64`).
- PostgreSQL만 구현하는 선택 port가 이미 있다: `port.LatestBlockWindowReader`, `port.BalancePrefetcher`(확인). 실행 상태도 같은 형태로 둘 수 있다.

**해석 (추정).** 상태 표에 행 단위 `undo_log` trigger를 붙이면 블록마다 바뀐 슬롯 수만큼 jsonb 행이 더 생긴다. 이력 표(높이별 값)를 따로 두면 undo는 "그 높이의 이력 행 삭제 + 현재 값 복원"이 되어 `undo_by_height`와 같은 방식으로 풀 수 있다. 설계 6장의 방향과 맞는다.

### 3.4 원본 블록과 header

- `pkg/chainfeed/ethpeer`: 높이마다 header, body, receipt를 ETH/68로 받아 envelope 세 개로 낸다. 새 블록 알림은 버린다(`peer.go:276-281,405-458`).
- `pkg/window`: 검증 전 원본. 24시간 미만 보존은 거부한다. segment 단위로 지우고, DB의 DeletionGuard가 허락할 때만 지운다(`window/options.go:92-101`, `gc.go:16-104`).
- `pkg/capture` + `pkg/source/facts`: 검증된 원본을 DB에 영구 저장하고 순서대로 읽는다. 지금은 receipt가 없는 계획을 받지 않는다(`facts/source.go:56-61`).
- indexer `blocks` 표: header의 모든 필드를 담는다(`pkg/core/model/model.go:20-48`). `gethconv`로 header를 다시 만들 수 있다.

**실행부가 header를 얼마나 알아야 하나 (확인, go-stablenet).**
- `BLOCKHASH`: 직전 256개(`core/vm/instructions.go:441-449`, `core/evm.go:94-130`).
- WBFT `Finalize`의 base fee 분배: 현재 epoch 블록 header(`consensus/wbft/engine/engine.go:1419-1448`).
- epoch 블록에서는 `buildEpochInfo`가 그 epoch의 **모든 header**와 이전 epoch 블록의 부모까지 거슬러 읽는다(`engine.go:660-780`). StableNet epoch 길이는 140이다.
- `verifyGasTip`이 부모 상태를 `StateAt(parent.Root)`로 연다(`engine.go:634-657`).

즉 실행부는 최근 max(256, epoch 길이 + 2)개 header와 부모 상태를 가져야 한다.

### 3.5 프로세스 사이 통신의 선례

`pkg/nodefeed`(Go)와 `x/arc/nodefeed`(Rust)가 Unix socket 위의 framed JSON을 쓴다(확인).
- 프레임: big-endian u32 길이 + JSON. 응답 상한 32MiB. socket mode 0600.
- 요청마다 연결을 새로 열고 닫는다. version, chain ID, session을 모든 메시지에 싣고 `hello`에서 genesis를 맞춘다. 오류 코드가 정해져 있다.
- ACK는 durable 커밋 뒤에만 보낸다(`pkg/fetch/writer.go:154-172`).
- wire 구조체가 go-ethereum `common.Hash`, `hexutil.Big`을 쓴다(`pkg/nodefeed/protocol.go:5-6`). 그래서 이 패키지를 실행부가 그대로 import할 수 없다.

**해석 (추정).** 프레임 형식, 버전·session·genesis 확인, 오류 코드, timeout 처리는 가져다 쓸 수 있다. 블록을 줄지어 보내는 실행부에는 요청마다 연결을 여는 방식보다 연결 하나를 유지하는 편이 맞다. JSON+base64는 RLP를 약 33% 부풀린다. 상태 변화와 trie 노드가 큰 블록은 32MiB를 넘을 수 있다.

**프로세스 감시 선례 (확인).** 운영 코드에서 오래 도는 자식 프로세스를 띄우고 감시하는 코드는 없다. 시험용 `e2e/anvil/anvil.go`(시작, 준비 대기, 조기 종료 감지, 종료)와 `internal/multichain`의 재시작 지연·`FailurePolicy`가 가장 가까운 형태다. 운영은 지금 Docker가 프로세스를 감시한다.

### 3.6 go-stablenet의 실행과 상태 표면 (확인)

- **상태 저장.** hash scheme trie는 key = 노드 hash 32바이트, value = 노드 RLP다. code는 `"c"+codeHash`다(`rawdb/accessors_trie.go:166`, `rawdb/accessors_state.go:45-85`). 실행에는 이 두 종류만 필요하다(추정, preimage는 기본으로 꺼져 있다).
- **우리 저장소로 받치기.** `rawdb.NewDatabase(kv)`로 감싸면 `triedb`(hashdb)가 우리 key-value 위에서 돈다. 실제 일을 하는 메서드는 `Get`, `Has`, `NewBatch`이고, batch는 `Replay`까지 구현해야 한다(`triedb/hashdb/database.go:433,481`).
- **commit이 내놓는 것.** `StateDB.Commit`은 root만 돌려준다. 안에서 owner별 `trienode.NodeSet`을 모아 `TrieDB().Update`로 넘긴다(`core/state/statedb.go:1221-1332`). 바뀐 상태를 알려 주는 공개 hook은 없다.
- **바뀐 상태를 꺼내는 길 세 가지.**
  1. `trie.NewDifferenceIterator`로 부모 root와 새 root를 비교한다(`trie/iterator.go:584`). 10/22에 고른 "trie 비교"다. 두 trie를 함께 걷는다.
  2. `state.Database`를 우리가 구현해 `*trie.StateTrie`를 감싼다. `UpdateAccount`, `UpdateStorage`, `DeleteAccount`, `DeleteStorage`가 블록이 끝날 때 바뀐 객체마다 한 번 불리고, `Commit`이 새 노드 집합을 돌려준다(`statedb.go:572`, `state_object.go:307,316`). fork는 고치지 않는다. **10/22 결정 뒤에 찾은 방법이다.**
  3. 새 노드만 필요하면 `tdb.Commit(root)`이 우리 batch에 쓰는 `Put`을 기록한다. 계정·슬롯 변화는 얻지 못한다.
- **메모리.** hashdb dirty 캐시는 `Commit(root)` 때 비워진다. clean 캐시(fastcache)는 크기를 정해 켠다(기본 0). snapshot은 `snaps=nil`로 끈다. 그러면 계정·슬롯 읽기마다 trie를 6~9단계 걷는다(추정). code 캐시는 64MiB LRU다(`core/state/database.go:38-41`).
- **실행 진입.** `StateProcessor`는 진짜 `*BlockChain`이 필요해서 쓸 수 없다. E0처럼 `core.ApplyTransaction`을 쓰거나, `Process`의 구조를 따라 EVM 하나를 블록 동안 재사용한다(`applyTransaction`은 비공개라 옮겨 써야 한다).
- **header 검증.** `wbftengine.Engine.VerifyHeader`가 시간, 부모, gas, 서명자, BLS seal, randao를 확인한다. `checkSig`를 넘겨야 하며 E0는 nil을 넘겼다(`engine.go:196-338`, `backend/backend.go:292-303`).
- **receipt.** 실행이 status, 누적 gas, 로그, bloom, 계약 주소, Anzeon effective gas price를 채운다(`core/state_processor.go:121-162`). receipt에는 fee payer 필드가 없다.
- **동시성.** trie prefetcher는 켜야 돈다. 켜면 우리 key-value가 동시 읽기를 견뎌야 한다. 변경이 100개 이상이면 hash 계산이 자동으로 16 goroutine을 쓴다.

### 3.7 receipt를 RPC 경로와 같게 만들 수 있나

실행부가 consensus receipt(EIP-2718 바이트)를 돌려주면, indexer는 nodefeed처럼 receipt root를 확인한 뒤 `profile.DeriveReceipts`로 model receipt를 만들 수 있다(확인, `pkg/chains/evm/binary.go:205-300`). fee delegation은 body에서, Anzeon tip과 epoch는 header에서 오므로 StableNet 고유 필드를 wire에 더할 필요가 없다(추정).

RPC JSON 경로와 다를 수 있는 필드(추정): `BlobGasPrice`(binary 경로는 nil), 계약 생성 거래의 `ContractAddress`, receipt `Ext`, profile 규칙과 노드가 다를 때의 `EffectiveGasPrice`. `TestBinaryMatchesLiveJSON`(`x/stablenet/binary_test.go:46`)이 StableNet에서 이 차이를 이미 고정하고 있으므로, E1의 비교 기준으로 쓸 수 있다.

### 3.8 같은지 비교하는 시험의 선례

- `TestNodeFeedStorageParityAndDurableACK`(`pkg/app/node_feed_test.go:26-83`): RPC와 IPC feed로 같은 시나리오를 색인하고 keyspace를 비교한다. `eth_getBalance`가 불리지 않았는지, ACK가 커밋 뒤에만 오는지도 본다. E1 시험의 틀로 가장 가깝다.
- `TestPostgresMatchesPebble`: 모든 읽기 port를 비교한다. RPC-on-PostgreSQL과 실행-on-PostgreSQL 비교로 바꿀 수 있다.
- `TestLiveRPCStorageParity`, `TestRPCArchiveStorageParity`: 실제 범위를 한 번 녹화하고 다시 돌린다. 실제 StableNet 블록에 맞는다.
- **주의.** `pkg/testchain`은 EVM을 돌리지 않고 receipt와 잔액을 대본처럼 만든다. header의 state root도 0이다(`pkg/testchain/chain.go:300-346`). 실행 결과의 기준으로 쓸 수 없다. 실제로 실행된 블록(녹화한 testnet 범위, 또는 로컬 go-stablenet 체인)이 필요하다.

---

## 4. 설계 원칙 (제안)

상세 설계는 다음 원칙으로 판단한다. 저장소 규칙(`CLAUDE.md`, Go 품질 지침)에 이미 있는 것은 다시 적지 않고, E1에서 판단이 갈릴 곳만 적는다.

1. **데이터마다 쓰는 쪽은 하나다.** PostgreSQL 쓰기는 indexer의 단일 writer만 한다. 실행부는 상태를 메모리에 들고 있어도 DB에 쓰지 않는다. 같은 데이터를 두 곳이 쓰면 한쪽만 커밋된 상태를 다뤄야 한다.
2. **경계에서 검증하고 안쪽은 믿는다.** 실행 결과는 state root, receipt root, gas, bloom이 header와 같을 때만 writer에 들어간다. 검증된 뒤로는 다시 검사하지 않는다.
3. **변하지 않는 데이터는 내용 주소로 둔다.** trie 노드와 code는 hash가 key이고 한 번 쓰면 바뀌지 않는다. 그래서 undo, 중복 쓰기, 캐시 무효화가 필요 없다. 바뀌는 데이터(현재 상태, 이력)만 높이와 묶는다.
4. **import로 되면 import하고, 고칠 곳만 새로 쓴다.** go-stablenet은 고치지 않는다. 우리가 쓰는 코드는 경계(상태 저장소 어댑터, chain reader, wire)뿐이다.
5. **wire는 표준 라이브러리만 쓴다.** `execwire`는 hash·주소를 고정 길이 배열로, 블록·receipt를 RLP 바이트로 나른다. 양쪽 모두 자기 go-ethereum 타입으로 바꾼다(work log 3.109).
6. **모든 것에 상한이 있다.** 대기열 길이, 프레임 크기, 캐시 크기, 기다리는 시간. 상한에 닿으면 기다리거나 멈추고, 버리지 않는다.
7. **결과가 같아야 한다.** 같은 입력이면 같은 출력이 나온다. map 순회 결과는 정렬해서 내보내고, 시간과 난수는 주입한다.
8. **다시 시작할 수 있어야 한다.** 어느 순간에 죽어도, 다시 시작하면 DB의 커밋된 높이에서 이어 간다. 실행부의 메모리 상태는 DB에서 다시 만들 수 있어야 한다.
9. **PostgreSQL 전용 기능은 선택 port로 둔다.** Pebble에서 실행 수집을 켜면 시작할 때 거부한다. 기존 Pebble 경로는 바꾸지 않는다.
10. **측정한 뒤 최적화한다.** 테이블별 writer, 병렬 실행 같은 최적화는 병목을 잰 뒤에 한다(E4).

**활용하는 도구와 스킬.**
- `tools/astgraph`: 두 저장소의 호출 그래프(1장). 설계 단계에서 변경 영향과 호출 경로를 질의한다.
- `codemine:design-first`: discovery, design, plan, 단위 구현을 단계마다 승인받는 절차. 이 문서가 그 첫 단계이고, 기록은 이 디렉터리의 `workflow.json`이다.
- 설계안을 쓴 뒤 `groundwork:critic` 에이전트로 반증 검토를 한 번 받는다. 근거 없는 주장과 계약을 지킨 척한 곳을 찾게 한다.
- 구현 단계의 시험은 `core-dev:reproduce-first`의 순서(먼저 실패하는 시험, 그다음 구현)를 따른다.
- 로컬 다중 노드 StableNet 체인이 필요하면 chainbench MCP 도구를 쓸 수 있다. 쓰기 전에 승인을 받는다.

---

## 5. 설계 전에 정해야 할 것

구조를 바꾸는 항목이다. 설계안에서 각각 제안과 단점을 쓴다. 여기서는 무엇을 골라야 하는지만 적는다.

| 번호 | 정할 것 | 선택지 | 지금 보는 방향 |
|---|---|---|---|
| D1 | 바뀐 상태를 꺼내는 방법 | (a) `NewDifferenceIterator`로 두 root 비교 (10/22 결정) / (b) `state.Database`를 감싸 trie 갱신을 기록 / (c) 둘 다: (b)로 꺼내고 (a)는 시험에서 대조 | (b)를 제품 경로로, (a)를 시험 대조로 쓰는 (c). **10/22 결정을 바꾸는 것이라 승인이 필요하다.** (b)도 fork를 고치지 않는다. (a)는 두 trie를 함께 걸어 블록마다 더 읽는다. (b)의 단점은 go-stablenet `state.Database` 인터페이스(7개 메서드)를 우리가 구현하고, fork가 바뀌면 따라가야 한다는 점이다 |
| D2 | 실행부가 header를 얻는 곳 | indexer가 요청마다 필요한 header를 함께 보냄 / 실행부가 DB `blocks` 표를 읽음 / window·capture | indexer가 보냄. 시작할 때 최근 max(256, epoch+2)개를 한 번 보내고, 그 뒤는 블록마다 그 header 하나다. DB 접속을 실행부에 주지 않아도 된다 |
| D3 | 실행부가 trie 노드·code를 얻는 곳 | 실행부가 PostgreSQL을 읽기 전용으로 직접 읽음 / indexer에 IPC로 물음 / 실행부 자기 로컬 저장소 | 미정. 직접 읽기는 빠르지만 실행부가 DB 접속 정보와 schema를 알아야 한다. IPC는 왕복이 늘어난다. 로컬 저장소는 DB와 두 벌이 되어 원칙 1과 부딪친다 |
| D4 | 실행부와 DB의 높이가 어긋날 때 | 실행부는 커밋 확인 전까지 결과를 메모리에 두고, DB 높이를 기준으로 되돌림 | DB를 정본으로 둔다. 실행부는 "높이 h의 결과를 냈다"와 "h가 커밋됐다"를 구분해 들고, 재시작하면 DB의 커밋 높이부터 다시 실행한다 |
| D5 | wire 인코딩과 연결 | nodefeed식 JSON+base64, 요청마다 연결 / 길이 접두 binary, 연결 유지 | binary, 연결 유지. 단점은 nodefeed와 형식이 달라져 사람이 읽기 어렵다는 점이다 |
| D6 | 상태 표 | 현재 값 표 + 높이별 이력 표 + `trie_nodes` + `code` / 현재 값에 `undo_track` | 이력 표 방식. undo를 높이 단위로 지울 수 있고 설계 6장과 맞는다 |
| D7 | `token.metadata`의 `eth_call` | E1에 실행부 call 요청을 넣음 / E1에서는 RPC 유지 | E1에서는 RPC를 유지하고 잔액만 로컬 상태로 바꾼다. 범위를 줄이는 대신 E1이 끝나도 RPC 상태 읽기가 남는다 |
| D8 | E1 시험의 기준 블록 | 녹화한 StableNet testnet 범위 / 로컬 go-stablenet 체인 / go-stablenet으로 만든 고정 fixture | 녹화한 testnet 범위(E0와 같은 구간)와 로컬 체인 둘 다. 녹화본은 실제 규칙을, 로컬 체인은 거래가 많은 블록과 reorg를 시험한다 |
| D9 | E1의 시작 상태 | E0처럼 공개 RPC `debug_dbGet`으로 필요한 노드만 받기 / 로컬 체인의 genesis부터 | 시험에서는 둘 다. 제품의 bootstrap은 E2에서 정한다 |

---

## 6. 사실, 해석, 가정, 미확인

**사실 (확인).**
- 상태 표는 없다. PostgreSQL만 구현하는 선택 port 선례가 있다.
- RPC 경로는 receipt root를 확인하지 않고, 커밋 직전 일반 `HashAt`도 없다.
- go-stablenet은 hash scheme이고, 실행에 필요한 저장 key는 trie 노드 hash와 `"c"+codeHash`다.
- `state.Database`를 감싸면 fork를 고치지 않고 바뀐 계정·슬롯과 새 노드를 얻을 수 있다.
- epoch 블록의 `Finalize`는 epoch 전체 header를 읽는다.
- `pkg/testchain`은 실행 결과의 기준이 될 수 없다.

**해석 (추정).**
- 실행 결과가 계정 잔액을 담으면 `balance.native`의 노드 잔액 읽기와 native accounting 계산을 둘 다 대체할 수 있다.
- consensus receipt + `DeriveReceipts`로 RPC 경로와 같은 model receipt를 만들 수 있다. 다를 수 있는 필드는 3.7에 적었다.
- 상태 변화와 trie 노드가 큰 블록은 32MiB 프레임을 넘을 수 있어 나눠 보내야 할 수 있다.

**가정.**
- E1은 StableNet만, PostgreSQL만 다룬다.
- E1 동안 블록은 기존 RPC source에서 받는다. peer 수신은 E3 이후다.

**미확인.**
- `state.Database` 감싸기가 실제로 컴파일되고 E0와 같은 root를 내는지. 코드를 읽은 결과이고 돌려 보지 않았다.
- trie 비교와 감싸기 방식의 블록당 시간 차이.
- 실행부가 PostgreSQL에서 노드를 읽을 때의 지연과 필요한 캐시 크기.
- 거래가 많은 블록에서 결과 크기.
- `pending.go`의 batch에 크기 상한이 정말 없는지. 블록 하나의 상태 쓰기가 클 때 메모리가 얼마나 되는지.
