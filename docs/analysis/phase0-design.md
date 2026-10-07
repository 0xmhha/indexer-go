# Phase 0 상세 설계: 정합성 수정과 안전망 (10/3)

[리팩토링 작업 정리](refactoring-plan.md)의 Phase 0(R0-1 ~ R0-10)를 구현할 수 있는 수준으로 설계한다. Phase 0는 구조 개편이 아니다. 지금 `main`에서 데이터가 손실·오염되거나 기능이 꺼져 있는 결함을 고치는 단계다. 이후 Phase들이 회귀 기준으로 쓸 시험도 이 단계에서 만든다.

전제로 삼은 결정(refactoring-plan.md 9.1절)은 셋이다.
- `main`에 바로 적용한다.
- 연결되지 않은 기능(SetCode, UserOp, Module, fee delegation)은 연결한다.
- 점진 교체 방식으로 진행한다.

대상 커밋은 `5baff64`이다. 이 문서의 코드 위치는 모두 그 커밋 기준이다.

## 1. 결론

1. **시험부터 만든다.** RPC를 흉내 내는 가짜 체인 서버와 시나리오 생성기를 만든다. 색인을 끝까지 한 번에 돌린 DB와, 중간에 쓰기 오류를 넣었다 다시 시작한 DB의 전체 키 공간을 비교한다. 지금 코드에서는 이 시험이 실패해서 D1·D2·D3·D10을 재현해야 한다.
2. **블록 하나를 트랜잭션 하나로 commit한다.**
   - `PebbleStorage`의 모든 읽기·쓰기를 `s.db` 대신 `s.kv(ctx)`로 보낸다.
   - context에 블록 트랜잭션(Pebble indexed batch)이 있으면 그 batch로 가고, 없으면 DB로 간다.
   - 처리기는 고치지 않는다. 같은 context를 받으므로 그들의 쓰기도 저절로 batch에 들어간다.
   - 커서는 같은 batch에 넣고 Sync로 commit한다.
   - 이벤트는 commit한 뒤에 발행한다.
3. **라이브 수집과 gap 복구가 같은 함수 `indexBlock`을 쓴다.**
   - gap 복구 때문에 커서가 되돌아가지 않게 한다. 커서는 앞으로만 간다(D10).
   - 큰 블록의 병렬 쓰기 경로는 없앤다. batch는 동시에 쓸 수 없고, 같은 데이터를 두 번 색인하는 결함(D7)도 함께 사라진다.
4. **주소 sequence는 키 형식을 유지하고, 주소를 처음 볼 때 DB에서 최대값을 복원한다**(D1). 키 형식 변경은 Phase 1에서 한 번만 한다.
5. **genesis wrapper를 없앤다.** 그 동작은 `PebbleStorage`의 선택적 hook으로 옮긴다. 그러면 가려졌던 인터페이스 8개가 다시 보이고, `*PebbleStorage` 타입 단언도 없앨 수 있다(F1).
6. **SetCode, UserOp, Module, fee delegation을 연결한다**(F2). UserOp과 Module은 지금 무시되는 `account_abstraction.enabled` 설정으로 켜고 끈다.
7. **나머지 결함을 고친다.** 동시성 결함(C1~C4), 범위 없는 조회(P1), 멀티체인 차단(D4), 설정 정리(F4)다.
8. **배포 후 재색인이 필요하다.** 기존 DB는 D1·D3·D10으로 이미 오염되었을 수 있다. 수정 코드로 고칠 수 없으므로 배포 뒤 `--reindex`로 다시 색인해야 한다.

---

## 2. 현재 쓰기 경로

설계의 근거가 되는 사실을 정리한다.

### 2.1 블록 하나의 쓰기 순서

`FetchBlock`(`pkg/fetch/fetcher.go:354-424`)은 다음 순서로 쓴다. 각 단계가 DB에 바로 쓰고, Sync와 NoSync가 섞여 있다.

1. `SetBlock`: 블록, hash 색인, 트랜잭션마다 `SetTransaction`, 트랜잭션 수(NoSync)
2. `processBlockMetadata`
   - WBFT 메타(Sync batch)
   - 주소 색인과 토큰 transfer(`processAddressIndexing`)
   - 잔액(`UpdateBalance`, 읽고 delta를 더해 씀, Sync)
   - 블록 0이면 genesis 잔액
3. fee delegation 메타
4. **블록 이벤트 발행**
5. receipt 저장, 로그 색인, 시스템 컨트랙트 파싱. 큰 블록이면 고루틴 여러 개가 동시에 쓴다
6. 트랜잭션·로그 이벤트 발행, 블록 처리기(토큰 메타데이터)
7. `SetLatestHeight`(NoSync)

gap 복구(`FetchRangeConcurrent`, `fetcher.go:476-690`)는 이 순서를 따로 다시 짜 두었다. 그래서 시스템 컨트랙트 파싱, 로그 이벤트, 블록 처리기가 빠져 있다(D6).

### 2.2 저장소가 DB에 접근하는 방식

| 항목 | 수 | 설계에 주는 뜻 |
|---|---|---|
| `s.db.Set/Delete` 직접 호출 | 64곳(파일 15개) | `s.kv(ctx)`로 기계적으로 바꿀 수 있다 |
| `s.db.NewBatch()` + `Commit` | 16곳 + 19곳 | 블록 트랜잭션 안에서는 바깥 batch에 합쳐야 한다(`Batch.Apply`) |
| `s.db.Get` / `s.db.NewIter` | 54 / 79곳 | 블록 안에서 앞서 쓴 값을 읽어야 하므로(잔액, holder 수) 읽기도 옮긴다 |
| ctx가 없는 쓰기 메서드 | `DeleteByPrefix`, `NewBatch` 둘뿐 | 둘 다 수집 경로 밖이다. context로 경로를 고르는 방식이 성립한다 |
| ctx 없이 DB를 읽는 내부 함수 | `updateHolderCountInBatch`(`pebble_token_holder.go:469`) | batch를 받아 놓고 DB에서 읽는다. 함께 고친다 |
| 메모리 상태 | `addrSeq`(주소별 sequence), `txCount`(atomic) | 트랜잭션이 실패하면 되돌려야 한다 |

Pebble v1.1.5의 `*pebble.DB`와 `*pebble.Batch`는 둘 다 `pebble.Reader`(`Get`, `NewIter`)와 `pebble.Writer`(`Set`, `Delete`, `Apply` …)를 구현한다. indexed batch(`DB.NewIndexedBatch`)는 자기가 쓴 값을 DB 내용과 합쳐 읽게 해 준다. 다른 batch를 자기 안에 합치는 `Batch.Apply`도 있다.

### 2.3 이번에 새로 확인한 결함 D10

gap 복구는 블록을 채울 때마다 `SetLatestHeight(높이)`를 무조건 기록한다. 그래서 gap [100, 120]을 채우면, 이미 1,000까지 색인되어 있어도 커서가 120으로 돌아간다. 이어서 `Run`이 `GetNextHeight`(= 커서 + 1)부터 다시 시작하므로 121~1,000을 모두 재처리한다. 재처리는 멱등이 아니어서 잔액이 두 번 더해지고 주소 색인이 중복된다. `--gap-recovery`로 켰을 때 gap이 하나라도 있으면 생기는 일이다.

---

## 3. 설계

### 3.1 R0-1·R0-2: 시험 기반

#### 가짜 체인 서버 `internal/testchain`

`httptest.Server` 위에서 JSON-RPC를 흉내 낸다. 실제 `client.Client`와 adapter 코드를 그대로 거치게 하려는 것이다. 응답할 메서드는 다음과 같다.

| 메서드 | 쓰는 곳 |
|---|---|
| `eth_chainId`, `net_version`, `web3_clientVersion` | 시작 확인, 노드 종류 감지 |
| `eth_blockNumber`, `eth_getBlockByNumber`(full tx), `eth_getBlockByHash` | 수집 |
| `eth_getBlockReceipts`, `eth_getTransactionReceipt` | receipt |
| `eth_getBalance` | genesis 잔액, 잔액 추적 |
| `eth_call` | 토큰 메타데이터(name, symbol, decimals) |

블록 응답은 RLP 객체가 아니라 JSON으로 직접 만든다. 그래야 go-ethereum이 만들 수 없는 fee delegation 트랜잭션(type 0x16)도 넣을 수 있다.

#### 시나리오 생성기 `internal/testchain/scenario`

결정적인 블록 열을 만든다. 같은 seed를 주면 같은 체인이 나온다. 한 시나리오에 다음을 모두 넣는다.

- native 전송, 컨트랙트 생성
- ERC-20·ERC-721 Transfer 로그
- 시스템 컨트랙트(0x1000~0x1004)의 mint·burn·governance 이벤트
- WBFT extra data
- EIP-7702 SetCode 트랜잭션(type 4)
- EntryPoint의 `UserOperationEvent`
- ERC-7579 `ModuleInstalled`/`ModuleUninstalled`
- fee delegation 트랜잭션
- `LargeBlockThreshold`를 넘는 큰 블록 하나

#### 키 공간 digest `storage.DumpKeyspace(db) → []Entry`

DB의 모든 키·값을 정렬된 순서로 꺼낸다. 비교에서 뺄 키는 명시 목록으로 둔다(예: 시각을 담는 notification 키). 두 DB의 결과를 비교하면 처음 다른 키를 보여 준다.

#### 시험 셋

| 시험 | 방법 | Phase 0 전 | Phase 0 후 |
|---|---|---|---|
| T-golden | 시나리오를 처음부터 끝까지 색인한다. 주요 GraphQL 조회 결과를 JSON 스냅샷(`testdata/golden/*.json`)과 비교한다 | 통과(현재 동작을 고정) | R0-5·R0-6으로 바뀐 부분만 스냅샷을 갱신한다 |
| T-crash | 쓰기 k번째에서 오류를 내는 fault injection(`faultKV`). k를 바꿔 가며 중단 → 재시작 → 끝까지 색인한 뒤, 한 번에 색인한 DB와 키 공간을 비교한다 | **실패**(D2, D3 재현) | 통과 |
| T-restart | 블록 경계에서 정상 종료 → 재시작을 반복한다. 주소 색인과 잔액 이력 키가 덮어써지지 않는지 본다 | **실패**(D1 재현) | 통과 |
| T-gap | 중간 블록 몇 개를 지운 DB에서 `RunWithGapRecovery`를 돌린다. 한 번에 색인한 DB와 비교한다 | **실패**(D10, D6 재현) | 통과 |

`faultKV`는 3.2절의 `kv` 인터페이스를 감싸서 k번째 `Set`/`Apply`/`Commit`에서 오류를 낸다. 지금 코드에는 `kv` 인터페이스가 없다. 그래서 Phase 0 전에는 Pebble의 `pebble.Options.FS`에 오류 주입 파일 시스템(`vfs.NewStrictMem` + 오류 주입)을 끼워 같은 효과를 낸다. 프로세스를 죽이는 것과 같은 효과를 내려고, 오류가 나면 저장소를 닫고 같은 디렉터리로 다시 연다.

시험은 `make test`에 넣는다. 가짜 서버를 쓰므로 anvil이나 네트워크가 필요 없다.

### 3.2 R0-4: 블록 트랜잭션

#### 경로 선택

```go
// pkg/storage/kv.go

// kv is the subset of pebble.DB / pebble.Batch used by PebbleStorage.
type kv interface {
	pebble.Reader
	pebble.Writer
}

type blockTxKey struct{}

// kv returns the block transaction bound to ctx, or the DB.
func (s *PebbleStorage) kv(ctx context.Context) kv {
	if tx, ok := ctx.Value(blockTxKey{}).(*BlockTx); ok && tx.owner == s {
		return tx.batch
	}
	return s.db
}
```

- `s.db.Get/Set/Delete/NewIter(` 호출 64 + 54 + 79곳을 `s.kv(ctx).…(`로 바꾼다. ctx가 없는 내부 함수는 ctx를 받도록 signature를 바꾼다.
- 메서드 안에서 만드는 batch(16곳)는 `s.newBatch(ctx)`와 `s.commitBatch(ctx, b, opts)`로 바꾼다. 블록 트랜잭션 안이면 `commitBatch`가 `tx.batch.Apply(b, nil)`로 바깥 batch에 합친다. 밖이면 지금처럼 `b.Commit(opts)`를 호출한다.
- `tx.owner == s` 검사는 다른 저장소 인스턴스(시험용 두 번째 DB 등)가 같은 context를 받았을 때 잘못된 batch로 쓰는 것을 막는다.

#### BlockTx

```go
// pkg/storage/block_tx.go

type BlockTx struct {
	owner    *PebbleStorage
	batch    *pebble.Batch                  // indexed: reads see own writes
	seqDelta map[common.Address]uint64      // staged address sequence increments
	txDelta  uint64                         // staged transaction count increment
	done     bool
}

// BeginBlock binds a new block transaction to ctx.
// Only one block transaction may be open at a time (single writer).
func (s *PebbleStorage) BeginBlock(ctx context.Context) (context.Context, *BlockTx, error)

// Commit writes all staged operations atomically with pebble.Sync,
// then publishes staged in-memory state (addrSeq, txCount).
func (tx *BlockTx) Commit() error

// Rollback discards the batch and staged in-memory state.
func (tx *BlockTx) Rollback()
```

- **메모리 상태.** 블록 안에서 `addrSeq`와 `txCount`를 바꾸는 코드는 `tx.seqDelta`와 `tx.txDelta`에 적는다. `Commit`이 성공했을 때만 공유 상태에 반영한다. 실패하거나 `Rollback`하면 버린다. 이렇게 해야 실패한 블록이 sequence 번호를 소비하지 않고, 재처리한 결과가 한 번에 처리한 결과와 같아진다.
- **단일 writer.** `BeginBlock`은 `s.writeMu`를 잡고, `Commit`/`Rollback`이 놓는다. 수집 경로 밖에서 수집 소유 키를 쓰는 코드도 같은 잠금을 잡는다. genesis 잔액 hook(3.4절)과 on-demand 토큰 메타데이터 저장이 그렇다. 그래야 블록이 읽고-수정하고-쓰는 사이에 다른 쓰기가 끼어들지 않는다.
- **`Storage` 인터페이스.** `Storage`에 `BeginBlock`을 추가하지 않는다. 대신 좁은 인터페이스 `BlockTransactor`를 새로 정의한다. 수집기는 이 인터페이스를 쓴다. Phase 1의 포트 분리와 방향이 같다.

#### indexBlock: 두 경로가 함께 쓰는 처리 함수

```go
// pkg/fetch/index_block.go

// indexBlock indexes one fetched block atomically.
// Both the live loop and gap recovery call this function.
func (f *Fetcher) indexBlock(ctx context.Context, block *types.Block, receipts types.Receipts, mode cursorMode) error {
	ctx, tx, err := f.txr.BeginBlock(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit

	pending := newEventBuffer() // events are buffered, not published

	if err := f.applyBlock(ctx, block, receipts, pending); err != nil {
		return err // whole block is discarded; caller retries
	}
	if err := f.advanceCursor(ctx, block.NumberU64(), mode); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	pending.publish(f.eventBus) // after durable commit
	return nil
}
```

- `applyBlock`은 2.1절의 1~6단계를 지금 순서대로 하나로 모은 함수다. 단, 이벤트 발행은 `pending`에 쌓는다.
- **오류 정책.** 저장소 오류는 모두 블록 전체를 실패시킨다. 블록을 재시도하면 같은 결과가 나오므로 안전하다. 외부 조회 실패(토큰 메타데이터 `eth_call`)만 경고 로그를 남기고 계속한다. 이 데이터는 나중에 on-demand로 채워지기 때문이다. 이렇게 해서 "실패를 경고로만 남기고 커서를 전진"(D5)을 없앤다.
- **커서.**
  - 라이브 경로(`cursorAdvance`)는 `max(현재 커서, 높이)`를 쓴다.
  - gap 복구 경로(`cursorKeep`)는 커서를 쓰지 않는다. 블록 존재 여부는 블록 키로 판단하므로 gap 복구에 커서가 필요 없다. 이것으로 D10이 해결된다.
- **큰 블록.** `LargeBlockProcessor.ProcessReceiptsParallel`을 수집 경로에서 뺀다. `*pebble.Batch`는 동시 쓰기에 안전하지 않고, 이 경로는 주소 색인과 SetCode·UserOp을 두 번 처리한다(D7). 병렬화는 RPC fetch에서만 한다. receipt 처리는 CPU 비용이 작아서 순차로 처리해도 처리량의 병목이 되지 않는다고 본다([Mid], 3.10절 벤치로 확인).
- **gap 복구.** `FetchRangeConcurrent`의 worker는 fetch만 한다. 높이 순서로 결과를 꺼내는 쪽이 `indexBlock(…, cursorKeep)`을 부른다. 같은 함수를 쓰므로 D6가 해결된다.

#### 점진 교체 적용

1. `kv(ctx)` 경로 선택을 먼저 넣는다. 블록 트랜잭션을 열지 않으면 `kv(ctx)`는 언제나 `s.db`를 돌려주므로 동작이 바뀌지 않는다. T-golden이 그대로 통과해야 한다.
2. `indexBlock`을 새 경로로 만든다. 내부 스위치 `indexer.atomic_block`(기본값 false)으로 옛 `FetchBlock` 경로와 고를 수 있게 한다.
3. 시나리오로 두 경로를 각각 돌려 키 공간을 비교한다. 다르게 나와야 하는 키는 의도한 수정(D1, D3, D10)에 해당하는 키뿐이어야 한다. 그 목록을 시험에 명시한다. T-crash, T-restart, T-gap은 새 경로에서만 통과해야 한다.
4. 기본값을 true로 바꾼다. 다음 릴리스에서 옛 경로, 스위치, `LargeBlockProcessor`의 쓰기 부분을 지운다.

### 3.3 R0-3: 주소 sequence 복원

키 형식(`/index/addr/{addr}/{seq:%020d}`, 잔액 이력 `AddressBalanceKey(addr, seq)`)은 그대로 둔다. 두 기능이 카운터 하나를 나눠 쓰는 구조도 Phase 0에서는 유지한다.

```go
// nextAddrSeq returns the next sequence for addr, restoring it from disk
// the first time addr is seen in this process.
func (s *PebbleStorage) nextAddrSeq(ctx context.Context, addr common.Address) (uint64, error)
```

- 메모리 map에 없는 주소를 처음 보면, 두 prefix(`AddressTransactionKeyPrefix(addr)`, `AddressBalanceKeyPrefix(addr)`)에서 각각 마지막 키를 찾는다(`SeekLT(prefix의 상한)`). 두 값 중 큰 것 + 1을 쓴다. 주소마다 프로세스 수명 동안 한 번, iterator seek 두 번이 든다. DB 전체를 훑을 필요는 없다.
- 블록 트랜잭션 안에서는 indexed batch를 읽으므로, 같은 블록에서 앞서 쓴 항목도 반영된다.
- `loadAddressSequences`(빈 함수)는 지운다.

### 3.4 R0-5: genesis wrapper 제거

- `GenesisInitializingStorage`를 지운다. `main.go:398-400`의 감싸기도 지운다.
- 그 동작은 `PebbleStorage.SetGenesisBalanceResolver(r GenesisBalanceResolver)`로 옮긴다. 기존 `SetTokenMetadataFetcher`와 같은 방식이다. 공개 `GetAddressBalance`가 기존 조건(잔액 0, 블록 1,000 미만, 이력 없음)일 때만 resolver를 부른다. `UpdateBalance`가 내부에서 읽는 경로(`getAddressBalance`)는 resolver를 부르지 않는다. 지금도 수집 경로는 wrapper를 거치지 않기 때문이다.
- resolver가 결과를 저장할 때는 `s.writeMu`를 잡는다(3.2절).
- wrapper가 따로 구현하던 나머지 32개 메서드는 단순 위임이므로 함께 사라진다.
- `NewConsensusStorage(*PebbleStorage)`는 필요한 메서드만 담은 인터페이스를 받게 바꾼다(`WBFTReader`, `WBFTWriter`, `GetBlock`). 그러면 GraphQL의 `*storage.PebbleStorage` 단언 5곳과 `main.go:398`의 단언이 없어진다.

검증: T-golden에서 다음을 확인한다. 이전 스냅샷에서는 비어 있거나 오류였던 항목이다.
- 시스템 컨트랙트 이벤트 조회
- 합의 통계 조회
- token holder 조회
- module 조회

### 3.5 R0-6: 미연결 기능 연결

| 기능 | 지금 | 연결 방법 | 켜고 끄기 |
|---|---|---|---|
| EIP-7702 SetCode | `SetSetCodeProcessor` 호출자 없음 | `initFetcher`에서 저장소가 `SetCodeIndexer`를 구현하면 `NewSetCodeProcessor`를 만들어 등록한다 | 항상 켬. tx type 4가 있을 때만 동작한다 |
| ERC-4337 UserOp | `SetUserOpProcessor` 호출자 없음 | 같은 방식. `account_abstraction.entry_point_addresses`가 있으면 그 주소만, 없으면 지금처럼 이벤트 signature로 찾는다 | `account_abstraction.enabled`(지금 무시되는 키, 기본값 true로 정한다) |
| ERC-7579 Module | 호출 위치도 없음 | `applyBlock`에서 UserOp 처리 뒤에 `ProcessModuleEventsFromBlock`을 부른다 | `account_abstraction.enabled` |
| fee delegation | `*client.Client`가 `FeeDelegationClient`를 구현하지 않는다. `factory.EVMClient`는 메타 타입이 달라 역시 맞지 않는다 | `FeeDelegationMeta`를 공용 패키지(`pkg/types/chain`)로 옮겨 `fetch`와 `factory`가 같은 타입을 쓰게 한다. adapter가 stableone이면 그 클라이언트를 fetcher에 넘긴다 | adapter가 stableone일 때만 |

- 멀티체인 경로(`multichain/instance.go`)에도 같은 연결을 넣는다. 다만 멀티체인은 R0-9로 막혀 있으므로, 시험은 단일 체인으로만 한다.
- 처리기 등록 코드는 `initFetcher` 한곳에 모은다. Phase 2의 기능 레지스트리로 옮길 때 이곳 하나만 바꾸면 되게 하려는 것이다.

**확인할 사항 [Mid].** fee delegation 메타 처리(`processFeeDelegationMetadata`)는 블록마다 원시 RPC로 블록을 한 번 더 가져온다. 또 upstream go-ethereum의 `ethclient`는 type 0x16 트랜잭션을 decode하지 못한다. 그래서 StableNet에서 fee delegation 트랜잭션이 든 블록을 지금 어떤 클라이언트가 읽고 있는지(adapter의 `BlockFetcher`인지, `client.Client`인지) 실제 노드에서 확인해야 한다. 확인 결과에 따라 "블록을 두 번 읽기"를 "한 번 읽고 메타를 함께 추출"로 바꿀지 정한다. 이 확인은 R0-6 착수 전의 spike로 둔다.

### 3.6 R0-7: 동시성·안정성

| 결함 | 수정 |
|---|---|
| C1 `FetchRangeConcurrent` 고루틴 누수 | `errgroup.WithContext`로 바꾼다. 오류가 나면 context를 취소하고, 생산자와 worker는 `select { case results <- r: case <-ctx.Done(): return }`로 보낸다. 반환 전에 `g.Wait()`를 한다 |
| C2-1 subscription ID 충돌 | 버스 ID를 `connID + "/" + clientID`로 만든다. `connID`는 연결마다 만드는 난수다. 클라이언트에게는 계속 `clientID`로 응답한다 |
| C2-2 닫힌 채널에 send | `close(c.send)`를 없앤다. `c.done` 채널을 닫고, 보내는 쪽은 `select`로 `c.done`을 확인한다. 쓰기 고루틴은 `c.done`이 닫히면 끝낸다 |
| C2-3 keepalive | `api.enable_websocket_keepalive`를 `api.Config`로 넘긴다(`main.go:667-684`). 기본값을 true로 바꾼다. 서버 ping 주기는 읽기 deadline(60초)보다 짧게(예: 25초) 둔다 |
| C3 재귀 RLock | `GetAllSubscriberInfo` 안에서 잠금 없이 동작하는 `subscriberInfoLocked(id)`를 부르게 한다 |
| C4 context를 무시하는 sleep | `time.Sleep(d)`를 `sleepCtx(ctx, d) error`(`select` + `time.NewTimer`)로 바꾼다. 수집 경로 5곳과 재시도 경로가 대상이다 |
| C4 RPC timeout | 블록·receipt 조회마다 `context.WithTimeout(ctx, rpc.timeout)`을 쓴다 |
| C5 hub·rate limiter race | hub는 삭제할 때 쓰기 잠금을 잡는다. rate limiter는 `lastAccess`를 잠금 안에서 쓰거나 atomic 값으로 바꾼다 |

검증: 해당 패키지 시험을 `go test -race`로 돌린다. C1은 `go.uber.org/goleak`으로 확인한다. C2는 같은 `id`를 쓰는 두 클라이언트 시험과, 연결을 끊는 도중에 이벤트를 보내는 시험으로 확인한다.

### 3.7 R0-8: 범위 없는 조회

GraphQL `transactions`/`logs`는 범위를 주지 않으면 0..latest 전체 블록을 메모리에 올린다(`resolvers.go:432-441`). indexer-frontend가 범위 없이 최근 목록을 부를 수 있으므로, 이 조회를 거부하지 않고 의미를 유지하는 쪽으로 고친다.

- 범위가 없으면 latest부터 **거꾸로** 블록을 읽는다. `offset + limit`개를 모으면 멈춘다. 비용이 체인 길이가 아니라 `offset + limit`에 비례하게 된다.
- `offset`에 상한(예: 10,000)을 두고, 넘으면 범위를 지정하라는 오류를 낸다.
- `totalCount`처럼 전체 수를 요구하는 필드는 이미 있는 카운터(트랜잭션 수)를 쓴다. 필터가 있으면 근사값이라고 표시하거나 null을 돌려준다. 어느 쪽으로 할지는 indexer-frontend가 이 필드를 쓰는지 확인하고 정한다.

검증: 10만 블록 시나리오에서 범위 없는 첫 페이지 조회의 시간과 할당이 블록 수와 무관해야 한다(벤치).

### 3.8 R0-9: 멀티체인 차단

`multichain.enabled: true`이면 시작 단계에서 오류를 내고 멈춘다. 오류 메시지는 "chain-scoped storage is not implemented yet (see refactoring-plan R2-8)"이다. 지금 배포 설정(`configs/*.yaml`)에는 멀티체인을 켠 것이 없으므로, 이 조치로 운영 중인 배포가 멈추지는 않는다.

### 3.9 R0-10: 설정 정리

| 항목 | 수정 |
|---|---|
| `database.readonly` | ingest 프로세스에서 true면 오류를 내고 멈춘다. api 전용 실행은 R4-1에서 다룬다 |
| 무시되는 키 | `eventbus.*`, `node.*`, `watchlist.*`, `resilience.*`, 체인별 `workers`/`batch_size`/`rpc_timeout`. 값이 설정되어 있으면 시작할 때 "아직 지원하지 않는 설정" 경고를 한 번 남긴다. 키를 지우지 않는 이유는 기존 설정 파일이 parse 오류로 깨지지 않게 하려는 것이다 |
| `account_abstraction.*` | R0-6에서 반영한다 |
| CLI 기본값이 설정 파일을 덮는 문제 | `flag.Visit`으로 사용자가 직접 준 플래그만 적용한다. `--workers`의 기본값 100이 `indexer.workers`를 덮지 않게 한다 |
| bool 플래그로 끌 수 없는 문제 | 같은 `flag.Visit` 방식으로 `--api=false`처럼 명시한 값도 적용한다 |
| 검증 순서 | CLI 플래그를 적용한 다음 `Validate`를 부른다. 그래야 `--rpc`만으로 필수값을 줄 수 있다 |

검증: 설정 우선순위 표 시험(기본값 < 파일 < 환경 변수 < CLI, CLI는 명시한 것만 적용).

### 3.10 성능 확인

블록 트랜잭션은 블록마다 fsync를 한 번 한다. 지금은 블록마다 Sync 쓰기가 여러 번이므로(로그 색인, 잔액, transfer 각각) fsync 횟수는 줄어든다고 본다([Mid]). 큰 블록의 병렬 처리를 없앤 영향과 함께 벤치로 확인한다.

- **벤치.** 시나리오 1만 블록(보통 블록 99%, 큰 블록 1%)을 색인하는 시간을 옛 경로와 새 경로에서 잰다.
- **통과 기준.** 새 경로의 처리 시간이 옛 경로의 1.2배 이내여야 한다. 넘으면 블록 N개를 트랜잭션 하나로 묶는 옵션을 검토한다(대신 커서 단위가 N블록이 된다).

---

## 4. 작업 순서와 PR 분할

| 순서 | PR | 내용 | 선행 | 통과 조건 |
|---|---|---|---|---|
| 1 | test: testchain | 가짜 체인 서버, 시나리오 생성기, 키 공간 digest, T-golden | — | T-golden 통과 |
| 2 | test: fault injection | 오류 주입 FS, T-crash·T-restart·T-gap(지금은 실패로 표시, `t.Skip` 대신 known-failure 목록) | 1 | 지금 코드에서 D1·D2·D3·D10이 재현된다 |
| 3 | fix(fetch): goroutine lifecycle | C1, C4(sleep, timeout) | — | `-race`, goleak |
| 4 | fix(api): websocket and event bus | C2, C3, C5 | — | `-race`, 시험 |
| 5 | refactor(storage): route db access through kv(ctx) | 3.2절의 경로 선택만 넣는다(동작 변화 없음) | 1 | T-golden 통과 |
| 6 | feat(storage): block transaction | `BeginBlock`/`Commit`/`Rollback`, 메모리 상태 staging, `writeMu`, `updateHolderCountInBatch` 수정 | 5 | 저장소 단위 시험 |
| 7 | fix(storage): restore address sequence | R0-3 | 5 | T-restart 통과 |
| 8 | feat(fetch): atomic indexBlock behind switch | `indexBlock`, 이벤트 버퍼, 커서 단조 증가, gap 경로 통합, 큰 블록 병렬 쓰기 제외 | 6, 7 | 새 경로에서 T-crash·T-gap 통과, 두 경로 키 공간 차이가 의도 목록과 같다, 벤치 기준 통과 |
| 9 | fix(storage): remove genesis wrapper | R0-5 | 1 | T-golden 스냅샷의 의도한 변경만 생긴다 |
| 10 | feat(fetch): wire SetCode, UserOp, Module, fee delegation | R0-6(fee delegation은 spike 뒤) | 8, 9 | 시나리오 조회 결과가 기대값과 같다 |
| 11 | fix(api): bounded queries | R0-8 | 1 | 벤치 |
| 12 | fix(config): precedence and unsupported keys, block multichain | R0-9, R0-10 | — | 설정 시험 |
| 13 | chore: flip atomic_block default | 기본값 true | 8~10 | 전체 시험 |
| 14 | chore: remove legacy path (완료 10/6, v0.1.0 이후) | 옛 `FetchBlock` 경로, 스위치, `LargeBlockProcessor` 쓰기 부분, `SetBlockWithReceipts`(호출자 없음) 삭제 | 13 이후 한 릴리스 | 그래프 도구로 옛 경로 호출자가 0곳 |

3, 4, 11, 12는 다른 작업과 독립이라 병렬로 진행할 수 있다.

---

## 5. 배포와 데이터

- **기존 DB 재색인.** D1(재시작 때 덮어쓰기), D3(재처리 중복), D10(gap 복구 뒤 재처리)은 이미 운영 DB에 흔적을 남겼을 수 있다. 오염된 항목은 수정 코드가 고칠 수 없다. Phase 0를 배포한 뒤 `--reindex`(또는 빈 DB에서 처음부터)로 다시 색인해야 한다. 재색인하는 동안 API가 불완전한 데이터를 보여 주므로, 새 DB를 옆에서 만든 뒤 바꿔 끼우는 방식을 권장한다.
- **키 형식.** Phase 0는 키 형식을 바꾸지 않는다. 그래서 재색인을 하지 않아도 새 코드는 기존 DB를 읽을 수 있다. 다만 기존 오염은 그대로 남는다.
- **API 동작 변화.** 범위 없는 목록 조회의 offset 상한(R0-8), 켜지는 기능의 조회 결과(R0-5, R0-6), keepalive 기본값(R0-7)이 바뀐다. 릴리스 노트에 적고, indexer-frontend에서 회귀 확인을 한다.

---

## 6. 위험과 단점

| 위험·단점 | 영향 | 대응 |
|---|---|---|
| `s.db` → `s.kv(ctx)` 치환이 200곳에 가깝다 | 하나라도 놓치면 그 쓰기는 batch 밖으로 나가 원자성이 깨진다 | 치환 뒤 `s.db.` 직접 호출이 `kv.go`, `BeginBlock`, `Compact`, `DeleteByPrefix` 외에는 없는지 시험(소스 검사 또는 그래프 도구)으로 강제한다 |
| context에 숨은 상태(batch)를 싣는다 | 코드를 읽는 사람에게 경로가 보이지 않는다. 블록 context를 고루틴에 넘기면 batch를 동시에 쓰게 된다 | Phase 1에서 포트에 트랜잭션을 명시적으로 넘기는 방식으로 바꾼다. Phase 0에서는 블록 트랜잭션 안에서 고루틴을 띄우지 않는다는 규칙을 시험(race)으로 지킨다 |
| indexed batch의 메모리 | 아주 큰 블록에서 batch가 커진다 | 벤치에서 최대 batch 크기를 잰다. 필요하면 상한을 두고, 넘으면 오류로 알린다 |
| `writeMu`가 API 쓰기를 블록 처리 시간만큼 기다리게 한다 | genesis 잔액 저장, on-demand 메타데이터 저장의 지연이 늘어난다 | 이 쓰기는 드물다. 지연은 블록 하나 처리 시간(수십 ms 수준으로 예상, [Low])으로 제한된다 |
| 큰 블록 병렬 처리 제거 | 큰 블록의 처리 시간이 늘어날 수 있다 | 3.10절 벤치로 확인한다 |
| 재색인 필요 | 운영 중단이나 이중 운영 비용 | 옆에서 새 DB를 만들어 바꿔 끼운다 |
| 시나리오가 실제 체인을 다 담지 못한다 | 가짜 서버로 통과해도 실제 노드에서 다를 수 있다 | 배포 전에 testnet에서 일정 범위를 두 번(한 번은 중간에 강제 종료) 색인해 키 공간을 비교한다 |

---

## 7. 착수 전에 확인할 것

| 항목 | 확인 방법 | 결과에 따라 바뀌는 것 |
|---|---|---|
| `Batch.Apply`가 indexed batch에 합칠 때 색인까지 반영하는지 | **확인 완료(10/3).** Pebble v1.1.5 spike 시험 결과는 다음과 같다. 안쪽 batch의 `Set`·`Delete`를 `Apply`로 합치면 indexed batch의 `Get`과 iterator에 보인다. commit 전에는 DB에 보이지 않는다. `Commit(Sync)` 뒤에는 한꺼번에 반영된다 | 3.2절 설계를 그대로 쓴다 |
| fee delegation 블록을 어떤 클라이언트가 decode하는지 | StableNet 노드에서 type 0x16 트랜잭션이 든 블록으로 확인 | R0-6 fee delegation 연결 방식 |
| indexer-frontend가 범위 없는 목록, 깊은 offset, `totalCount`를 쓰는지 | indexer-frontend의 GraphQL 질의 검색 | R0-8 상한값과 `totalCount` 처리 |
| 운영 DB 크기와 재색인 시간 | 운영 지표 | 5절 배포 방식 |

---

## 8. 진행 기록

### PR 1: testchain과 T-golden (10/3, 브랜치 `phase0/testchain`)

| 산출물 | 내용 |
|---|---|
| `internal/testchain/chain.go` | 서명된 실제 트랜잭션과 receipt로 블록을 만드는 결정적 체인. 블록마다 잔액 상태를 보관한다(indexer와 같은 규칙: value + gasUsed × gasPrice) |
| `internal/testchain/server.go` | JSON-RPC 서버. 구현하지 않은 메서드가 호출되면 기록한다(`UnknownMethods`). `debug_*`, `anvil_*`는 일반 노드처럼 "지원 안 함"으로 답한다 |
| `internal/testchain/scenario.go` | 기준 시나리오(블록 21개). native 전송(legacy, dynamic fee), 실패 트랜잭션, ERC-20·721 생성과 전송, 시스템 컨트랙트 Mint·Burn, EIP-7702, ERC-4337, ERC-7579 설치·해제 |
| `internal/testchain/keyspace.go` | 키 공간 덤프, 정규화 filter, 비교, 형식화 |
| `cmd/indexer/golden_test.go` | `NewApp`(운영 배선)으로 시나리오를 색인한다. `TestGoldenKeyspace`는 키 공간 전체를 고정하고, `TestIndexIsDeterministic`은 두 번 색인한 결과가 같은지 확인한다 |
| `cmd/indexer/testdata/golden/keyspace.txt` | 현재 동작의 golden(키 348개) |

구현하면서 확인한 사실은 다음과 같다.
- **F1·F2가 저장 결과로 확인되었다.** 시나리오에 시스템 컨트랙트 Mint·Burn, SetCode, UserOp, Module 이벤트를 넣었지만, golden에는 해당 키가 하나도 없다. R0-5·R0-6을 적용하면 golden에서 이 키들이 늘어나야 한다.
- **결정성 결함이 하나 있다.** 토큰 메타데이터가 `createdAt`/`updatedAt`에 처리 시각(`time.Now()`, `pkg/token/block_processor.go:149`)을 저장한다. 같은 체인을 다시 색인하면 값이 달라진다. golden에서는 두 필드를 지우고 비교한다. 이 결함은 처리기 결정성 규약(refactoring-plan.md 8절)에서 블록 시각으로 바꾸는 대상으로 남긴다.
- **`.gitignore` 결함이 있었다.** 루트 실행 파일을 무시하려던 `indexer` 패턴이 `cmd/indexer/` 아래 새 파일까지 무시했다. `/indexer`로 고쳤다.
- **GraphQL 조회 golden은 이 PR에 넣지 않았다.** API 동작이 바뀌는 R0-5(PR 9)에서 함께 추가한다.

### PR 2: 결함 재현 시험 (10/3, 같은 브랜치)

`cmd/indexer/defects_test.go`에 재현 시험 셋을 넣었다. 셋 다 운영 배선(`NewApp`)을 쓴다. 이 시험들은 `knownDefects` 목록 방식으로 동작한다. 결함 ID가 목록에 있는 동안에는 시험이 "결함이 아직 재현된다"를 확인한다. 고치는 PR에서 ID를 지우면 같은 시험이 정상 동작을 요구한다.

| 시험 | 방법 | 재현 결과 |
|---|---|---|
| `TestRestartPreservesIndex`(D1) | 세 세션(0~3, 4~12, 13~20)으로 나눠 색인하고, 한 번에 색인한 결과와 비교한다 | 주소 색인: 빠짐 22, 추가 12, 덮어씀 15. 잔액 이력: 빠짐 21, 추가 12, 바뀜 9 |
| `TestReprocessingIsIdempotent`(D3) | 같은 세션에서 전체를 색인한 뒤 15~20을 다시 처리한다. crash 때문에 커서가 기록되지 않았을 때와 같은 상황이다 | 주소 색인 중복 16, 잔액 이력 중복 12, 최신 잔액 2개 오류, 트랜잭션 수 오류 |
| `TestGapRecoveryDoesNotReprocess`(D10) | 6~9를 비워 두고 `RunWithGapRecovery`의 시작 단계(감지 → 채움 → 커서부터 재개)를 실행한다. 블록별 RPC 조회 횟수를 센다 | 블록 10~20이 각각 두 번 조회·처리된다 |

**순서 변경.** 중간 crash 시험(T-crash, D2)은 PR 5(`kv(ctx)` 경로) 뒤로 옮긴다. 설계에서는 그 전까지 파일 시스템 수준 오류 주입을 쓰기로 했다. 그런데 Pebble의 NoSync 쓰기는 WAL 버퍼를 백그라운드에서 flush한다. 그래서 파일 시스템에서 오류를 주입하면 어느 쓰기까지 남는지가 실행마다 달라져 재현이 결정적이지 않다. `kv(ctx)`가 생기면 그 경로를 감싸 "k번째 쓰기부터 모두 실패"를 결정적으로 주입할 수 있다. 그 사이 D3 재현은 위의 재처리 시험이 맡는다. crash가 결과적으로 일으키는 피해가 "커서가 기록되지 않은 블록의 재처리"이기 때문이다.

### P0-5: 저장소 접근을 kv(ctx)로 모으기 (10/3)

작업 단위 이름을 GitHub PR 번호와 구분하려고 이 절부터 "PR n" 대신 "P0-n"으로 적는다. 4절 표의 순서 번호와 같다.

| 산출물 | 내용 |
|---|---|
| `pkg/storage/kv.go` | `kv(ctx)`, `newBatch(ctx)`, `commitBatch(ctx, …)`, `newBatchCtx(ctx)`. ctx에 이 저장소 인스턴스의 batch가 묶여 있으면 그 batch로, 아니면 DB로 보낸다. 묶인 batch가 있으면 메서드 안의 batch는 `Apply`로 합친다. 닫는 일은 호출자의 `defer Close()`가 맡는다 |
| 저장소 메서드 17개 파일 | ctx가 있는 함수 안의 `s.db.Get/Set/Delete/NewIter/NewBatch`와 그 batch의 `Commit`을 바꿨다(227곳). `Batch` wrapper(`IndexLogs` 등 4곳)도 ctx를 받아 같은 경로로 commit한다. `updateHolderCountInBatch`에 ctx를 추가했다 |
| `pkg/storage/kv_routing_test.go` | 소스 검사 시험. 허용 목록(라우팅 자체, 시작할 때 카운터 로드, 재색인용 prefix 삭제·집계, 공개 `NewBatch`) 밖에서 `s.db`를 직접 읽고 쓰면 실패한다. 위반 코드를 임시로 넣어 실패하는지 확인했다 |
| `pkg/storage/kv_test.go` | 동작 시험. 직접 쓰기(`SetBlock`), wrapper batch(`IndexLogs`), 메서드 내부 batch(`SaveTokenMetadata`)가 묶인 batch에 들어가는지, commit 전에는 DB에 보이지 않는지, 같은 ctx로 읽으면 보이는지, commit 뒤 반영되는지, 다른 인스턴스의 batch는 무시되는지 확인한다 |

동작 변화가 없다는 것은 `TestGoldenKeyspace`(키 공간 전체가 그대로)로 확인했다. 결함 재현 시험 셋도 그대로 D1·D3·D10을 재현한다. `kv(ctx)`에 batch를 묶는 코드는 아직 없다. 묶는 일은 P0-6(`BeginBlock`)에서 한다.

**race 검사.** `go test -race`로 돌리면 `pkg/fetch`의 `TestFetchRangeConcurrentWithRetry`, `TestFetchRangeConcurrentMaxRetries`가 실패한다. 변경 전 커밋(`5baff64`)에서도 똑같이 실패하므로 원래 있던 문제다. 원인은 시험용 `mockClient.GetBlockByNumber`가 호출 카운터를 잠금 없이 동시에 쓰는 것이다. 고루틴 수명을 다루는 P0-3에서 함께 고친다.

### P0-6: 블록 트랜잭션 (10/3)

| 산출물 | 내용 |
|---|---|
| `pkg/storage/block_tx.go` | `BlockTransactor`, `BeginBlock`/`Commit`/`Rollback`. indexed batch를 ctx에 묶고, `writeMu`로 writer를 하나로 제한한다. `Commit`은 `pebble.Sync`로 한 번에 쓴 뒤 staging한 메모리 상태를 반영한다. `Rollback`은 `Commit` 뒤에 불러도 아무 일도 하지 않는다 |
| 메모리 상태 staging | 주소 sequence(`nextAddrSeq`)와 트랜잭션 수(`addTxCount`/`subTxCount`)를 바꾸던 5곳을 helper로 바꿨다. 블록 트랜잭션 안에서는 바뀐 값을 `BlockTx`에 모아 두었다가 commit할 때만 반영한다 |
| `pkg/storage/block_tx_test.go` | commit 반영, rollback 뒤 흔적 없음, rollback한 블록을 다시 처리하면 같은 키, writer 하나 제한, 중첩 거부, 트랜잭션 밖 쓰기는 지금처럼 즉시 반영 |

수집 경로는 아직 `BeginBlock`을 쓰지 않는다. 연결은 P0-8에서 한다. 소스 검사 시험의 허용 목록에 `BeginBlock`을 추가했다(batch를 만드는 자리라 DB를 직접 써야 한다).

### P0-7: 주소 sequence 복원 (10/3, D1 수정)

| 산출물 | 내용 |
|---|---|
| `nextAddrSeq` / `restoreAddrSeq` | 프로세스에서 주소를 처음 보면, 주소 색인과 잔액 이력 두 prefix의 마지막 sequence 키를 역방향 seek로 찾아 최대값 + 1부터 이어간다. 20자리 숫자가 아닌 키는 건너뛴다. 디스크 읽기가 실패하면 0부터 시작하지 않고 오류를 돌려준다. 블록 트랜잭션 안에서는 batch를 읽으므로 같은 블록의 앞선 쓰기도 반영된다 |
| `loadAddressSequences` | 빈 함수를 지웠다 |
| 시험 | `TestAddrSeqRestoredAfterReopen`, `TestAddrSeqRestoreUsesHighestOfBothPrefixes`. `knownDefects`에서 D1을 지웠다. 이제 `TestRestartPreservesIndex`는 세 세션으로 나눈 결과가 한 번에 색인한 결과와 완전히 같아야 통과하고, 실제로 통과한다 |

golden 키 공간은 바뀌지 않았다. 한 세션으로 색인할 때는 처음부터 sequence가 0이므로 복원 결과가 같다.

### P0-8: 원자적 indexBlock (10/3, D2·D3·D10 수정, 스위치 뒤)

| 산출물 | 내용 |
|---|---|
| `pkg/fetch/index_block.go` | `indexBlock`. 블록 하나의 모든 쓰기와 커서를 `BeginBlock` 트랜잭션 하나로 commit한다. 이벤트는 버퍼에 모았다가 commit 뒤에 발행한다(`f.publish`). 라이브 수집(`FetchBlock`)과 gap 복구(`FetchRangeConcurrent`의 순서대로 commit하는 부분)가 같은 함수를 쓴다 |
| 커서 | `advanceCursor`가 저장된 값보다 클 때만 올린다. gap을 채워도 커서가 되돌아가지 않는다(D10) |
| 이미 저장된 블록 | 같은 높이에 같은 hash가 있으면 건너뛴다. hash가 다르면 `ErrBlockConflict`로 멈춘다(reorg 처리는 R2-4). **설계와 달라진 점**: 설계에서는 원자성만으로 재처리가 멱등해진다고 보았다. 그런데 이미 commit된 블록을 다시 처리하면(재색인, 수동 범위 지정 등) 잔액 delta와 주소 색인이 여전히 중복된다. 그래서 이 규칙을 넣었다 |
| 오류 처리 | 원자적 경로(`strictStorageErrors`)에서는 저장 쓰기 실패 8곳이 블록 실패가 된다. 대상은 주소 색인 3곳, 컨트랙트 생성, ERC-20·721 transfer, 로그 색인, fee delegation 저장이다. 체인 데이터 해석이나 RPC 보강 실패(WBFT 해석, 시스템 컨트랙트 파서, 잔액 초기화 RPC, 블록 처리기, SetCode·UserOp 처리기, 잔액 갱신)는 여전히 경고로 남긴다. 잘못된 데이터 하나로 수집 전체가 멈추지 않게 하려는 것이다. 이 부분은 D5의 남은 과제다 |
| 큰 블록 | 원자적 경로는 receipt를 항상 순차로 처리한다(`storeReceiptsSequential`). batch는 동시 쓰기에 안전하지 않고, 병렬 경로는 같은 데이터를 두 번 색인했다(D7) |
| 스위치 | `indexer.atomic_block`(환경 변수 `INDEXER_ATOMIC_BLOCK`), 기본값 false. genesis wrapper가 `BeginBlock`을 위임하도록 했다(그러지 않으면 wrapper가 이 기능을 가린다) |
| 시험용 지점 | `Fetcher.SetBeforeCommitHook`. commit 직전에 오류를 내서 crash를 흉내 낸다 |

검증 결과는 다음과 같다(`cmd/indexer`).
- `TestAtomicPathMatchesGolden`: 원자적 경로로 깨끗하게 색인한 키 공간이 기존 golden과 같다.
- 결함 재현 시험을 두 경로로 돌린다. `knownDefects`는 경로별로 관리한다. 기존 경로는 D3·D10을 계속 재현하고, 원자적 경로는 D1·D3·D10 모두 정상이다.
- `TestCrashBeforeCommitRecovers`: 블록 0, 2, 6, 8, 13, 20의 commit 직전에 crash를 넣고 재시작했다. 여섯 경우 모두 최종 키 공간이 golden과 같다. 원자적 경로에서는 블록 안 어느 지점의 crash든 "commit 전 crash"와 같다. commit 전에는 아무것도 DB에 반영되지 않기 때문이다(`TestBoundBatchCapturesWrites`, `TestBlockTxRollbackLeavesNoTrace`).
- gap 복구 뒤 키 공간은 주소 색인·잔액 이력을 빼고 golden과 같다. 이 두 prefix의 sequence는 처리 순서를 따르므로, 나중에 채운 블록은 번호가 다르게 매겨지는 것이 정상이다.

남은 일은 다음과 같다.
- P0-13에서 기본값을 true로 바꾸고, P0-14에서 옛 경로를 지운다.
- `FetchRangeConcurrent`의 고루틴 누수(C1)는 P0-3에서 고친다.

### P0-9: genesis wrapper 제거 (10/3, F1 수정, 새로 찾은 D11 수정)

| 산출물 | 내용 |
|---|---|
| `pkg/storage/genesis_balance.go` | wrapper의 유일한 동작(잔액이 0이고 이력이 없는 초기 블록 주소에 대해 RPC로 genesis 잔액을 조회해 저장)을 `PebbleStorage.SetGenesisBalanceResolver`와 공개 `GetAddressBalance`로 옮겼다. 쓰기 함수(`UpdateBalance`, `SetBalance`)는 조회 없는 내부 `getAddressBalance`를 쓴다(wrapper 시절과 같다). 블록 트랜잭션 안에서 불리면 같은 batch에 쓰고, 밖에서 불리면 `writeMu`를 잡는다. "이미 조회한 주소" 기억은 블록 트랜잭션 안에서는 staging했다가 commit할 때 반영한다. wrapper는 rollback해도 기억이 남아, 같은 프로세스에서 재시도하면 초기화를 건너뛰었다 |
| wrapper 삭제 | `genesis_initializer.go`를 지웠다. `main.go`는 `GenesisBalanceConfigurer` 인터페이스로 resolver를 설정한다(`*PebbleStorage` 단언 제거) |
| `ConsensusStorage` | 구체 타입 대신 `ConsensusBackend`(`Reader` + `WBFTReader` + `WBFTWriter`)를 받는다. GraphQL의 `*storage.PebbleStorage` 단언 5곳을 없앴다. 이제 운영 코드에 `*PebbleStorage` 타입 단언이 없다 |
| D11 수정 | 시스템 컨트랙트 조회 10개가 쓸 때와 같은 decoder(`Decode…`)를 쓰도록 고쳤다. `TestSystemContractEventsRoundTrip`이 9종을 저장·조회한다. 수정 전 코드로 돌리면 9개 모두 실패하는 것을 확인했다 |
| `cmd/indexer/graphql_golden_test.go` | 운영 GraphQL 스키마로 `mintEvents`, `burnEvents`, `allValidatorsSigningStats`를 조회해 `testdata/golden/graphql.json`과 비교한다 |

golden 키 공간은 의도한 세 키만 늘었다. `/data/syscontracts/mint/…`, `/data/syscontracts/burn/…`, `/index/syscontracts/total_supply/…`다. 시스템 컨트랙트 파서가 다시 동작하기 때문이다. genesis 처리를 옮긴 뒤에도 다른 키는 바뀌지 않았다.

**기존 불안정 시험.** `pkg/resilience`의 `TestConnectionManager_GetActiveSessionCount`가 가끔 실패한다(이번 브랜치 10회 중 4회, 변경 전 커밋 10회 중 2회). 메모리 저장소를 쓰는 시험이라 이번 변경과는 무관하다. 이 패키지는 운영 배선에 연결되어 있지 않으므로, R1-2에서 연결하거나 지울 때 함께 정리한다.

### P0-10: 미연결 기능 연결 (10/3, F2 수정)

| 기능 | 연결 방법 | 켜고 끄기 |
|---|---|---|
| EIP-7702 SetCode | `main.go`의 `registerFeatureProcessors`가 저장소가 `SetCodeIndexer`를 구현하면 처리기를 등록한다 | 항상 켬(type 4 트랜잭션이 있을 때만 동작) |
| ERC-4337 UserOp | 같은 함수에서 `UserOpIndexer`일 때 등록한다 | `account_abstraction.enabled`. 기본값은 true다. `NewConfig`에서 넣으므로 설정 파일에서 키를 생략하면 켜지고, `false`를 명시하면 꺼진다(`TestAccountAbstractionDefault`). `entry_point_addresses`는 아직 처리기가 지원하지 않아 경고만 남긴다 |
| ERC-7579 Module | `Fetcher.SetModuleProcessor`를 추가하고, 주소 색인 처리의 UserOp 다음에 호출한다 | `account_abstraction.enabled` |
| fee delegation | `FeeDelegationMeta`를 `pkg/types/chain`으로 옮기고 `fetch`·`factory`에서는 type alias로 가리킨다. 그래서 `factory.EVMClient`가 `fetch.FeeDelegationClient`를 만족한다(`main.go`에 컴파일 시점 확인). `Fetcher.SetFeeDelegationClient`로 주입한다 | 노드 감지 결과가 StableOne일 때만 |

검증 결과는 다음과 같다.
- golden 키 공간에 SetCode·UserOp·Module(번들러 통계, 스마트 계정 포함) 키 20개가 추가되었다. 기존 키 변화는 없다.
- GraphQL golden에 `setCodeTransactionCount`, `userOperationCount`, `moduleEventCount`(각 1)와 `installedModules`(블록 9 설치, 블록 11 해제, 비활성)를 추가했다. 해제는 새 이벤트가 아니라 설치 기록을 비활성으로 바꾸는 설계라 개수는 1이 맞다.
- fee delegation은 시나리오로 검증하지 못했다. StableNet 노드 감지와 type 0x16 JSON이 필요하기 때문이다. 대신 `TestFeeDelegationClientIsUsed`로 주입한 클라이언트를 쓰고 메타가 저장되는지 확인했다. 실제 노드에서 어느 클라이언트가 0x16 블록을 decode하는지는 여전히 7절의 확인 항목이다.

연결하면서 결함 둘을 함께 찾았다.
- **결정성 결함.** SetCode 상태·통계의 `updatedAt`, `lastActivityTime`이 처리 시각(`time.Now()`)이었다. 같은 체인을 다시 색인하면 값이 달라져 결정성·crash 시험이 실패했다. 호출하는 쪽에서 블록 시각을 넣게 했고, 통계는 같은 트랜잭션에 이미 저장된 블록 헤더의 시각을 읽는다(`blockTimeOrNow`). 비용은 SetCode 기록마다 블록 읽기 한 번이다. 토큰 메타데이터의 같은 결함은 golden에서 정규화로 피하고 있으며 아직 고치지 않았다.
- **D12(새 결함).** gap을 뒤의 블록보다 나중에 채우면, 처리 순서에 따라 결과가 달라지는 상태가 틀어진다. 시험에서는 블록 11의 모듈 해제가 블록 9의 설치보다 먼저 처리되어 최종 상태가 "활성"으로 남았다. 원자적 경로는 새 gap을 만들지 않지만, 기존 DB의 gap을 채울 때는 남는다. gap 시험의 비교에서는 순서 의존 prefix(`/index/addr/`, `/index/balance/`, `/data/module/`)를 이유와 함께 뺐다.

### P0-13: 원자적 경로를 기본값으로 (10/3)

**성능 확인(3.10절 기준: 새 경로가 옛 경로의 1.2배 이내).** `cmd/indexer/ingest_bench_test.go`의 `BenchmarkIngest`로 측정했다. 운영 배선(`NewApp`)으로 가짜 체인을 색인하고, `FetchRange` 시간만 잰다. 부하 시나리오는 `testchain.BuildLoad`로 만들었다.

| 경우 (블록 202개) | 기존 경로 | 원자적 경로 |
|---|---|---|
| regular: 블록당 트랜잭션 50개, 절반은 ERC-20 transfer (총 10,001개) | 179.1초, 180.3초 | 2.7초, 3.2초, 4.1초 |
| large_block: 위에 트랜잭션 1,200개 블록 하나 추가 (총 11,151개). 기존 경로는 이 블록을 병렬 worker로 처리한다 | 190.9초, 226.2초 | 2.6초, 5.5초 |

원자적 경로가 약 40~70배 빠르다. 기준을 충분히 통과한다. 큰 블록의 병렬 처리를 없앤 영향도 보이지 않는다. 기존 경로가 느린 원인은 블록마다 동기 쓰기(fsync)를 여러 번 하는 것으로 추정한다(transfer, 잔액 갱신, 로그 색인이 각각 Sync). 원자적 경로는 블록마다 fsync를 한 번 한다. 이 추정은 프로파일로 확인하지 않았다. 측정 환경은 macOS(arm64) 로컬 디스크이고, 가짜 체인의 RPC 비용은 두 경로에 똑같이 들어간다.

**전환.**
- `NewConfig`가 `indexer.atomic_block`을 true로 둔다. 설정 파일의 `atomic_block: false`나 `INDEXER_ATOMIC_BLOCK=false`로 기존 경로를 고를 수 있다(`TestAtomicBlockDefault`).
- 시험 harness의 기본 경로를 원자적 경로로 바꿨다. `TestLegacyPathMatchesGolden`은 되돌리기 경로인 기존 경로가 같은 키 공간을 내는지 계속 확인한다.
- `docs/CONFIG.md`에 키와 환경 변수, `account_abstraction.enabled` 기본값을 적었다.
- golden 키 공간과 GraphQL golden은 바뀌지 않았다.

**남은 것.**
- 멀티체인 경로(`multichain/instance.go`)는 fetch 설정을 따로 만들므로 아직 기존 경로를 쓴다. 멀티체인은 P0-12에서 시작을 막을 예정이라 이번에는 바꾸지 않았다.
- P0-14에서 한 릴리스 뒤 기존 경로, 스위치, `LargeBlockProcessor`의 쓰기 부분을 지운다.

### P0-3: 고루틴 수명과 RPC timeout (10/3, C1·C4 수정)

| 결함 | 수정 | 시험 |
|---|---|---|
| C1 `FetchRangeConcurrent` 고루틴 누수 | 함수 안에서 취소 가능한 context를 만든다. 반환할 때 취소한 뒤 결과 채널을 끝까지 비운다. worker는 결과를 보낼 때도 취소를 확인한다 | `TestFetchRangeConcurrentDoesNotLeakOnError`. 수정 전 코드에서는 worker 32개로 고루틴 34개가 남아 실패했다 |
| C4 context를 무시하는 sleep | `time.Sleep` 5곳(수집 루프 3곳, 재시도 backoff 2곳)을 `sleepCtx`로 바꿨다. 취소되면 `ctx.Err()`를 돌려준다 | `TestSleepCtxStopsOnCancel`, `TestRunStopsPromptlyWhileWaiting`. 수정 전 코드에서는 대기 1시간 설정에서 취소한 뒤에도 멈추지 않았다 |
| C4 RPC timeout 없음 | `Config.RPCTimeout`(`rpc.timeout`에서 가져옴)으로 블록·receipt·최신 높이·잔액 조회를 호출마다 제한한다. 조회 코드의 중복도 `getBlock`, `getReceipts`로 합쳤다 | `TestRPCTimeoutBoundsCalls` |
| 기존 race(시험용 mock) | `mockClient`의 실패 횟수 카운터를 잠금으로 보호했다 | `go test -race ./pkg/fetch`가 처음으로 통과했다 |

### P0-4: WebSocket·이벤트 버스 (10/3, C2·C3·C5 수정)

| 결함 | 수정 | 수정 전 코드에서의 시험 결과 |
|---|---|---|
| C2-1 구독 ID 충돌 | 연결마다 무작위 `connID`를 만들고 버스에는 `connID/클라이언트ID`로 등록·해제한다. 클라이언트에게는 계속 원래 ID로 응답한다 | `TestSubscriptionIDsAreScopedPerConnection` 실패: B가 같은 ID로 구독하자 A가 이벤트를 받지 못했다 |
| C2-2 닫힌 채널에 send | `close(c.send)`를 없앴다. 쓰기 고루틴은 연결 context가 끝나면 close frame을 보내고 끝낸다. 보내는 쪽은 context 종료를 먼저 확인한다 | `TestSendAfterCleanupDoesNotPanic` 실패: panic |
| C2-3 keepalive | `api.enable_websocket_keepalive`를 API 설정까지 전달하고, 기본값을 true로 했다(`NewConfig`). keepalive를 끄면 읽기 deadline도 걸지 않는다. 그래서 ping 없이 60초 뒤 끊기는 문제가 없다 | 시험 없음. 60초 대기가 필요해 코드 확인으로만 판단했다 |
| C3 재귀 RLock | `GetSubscriberInfo`를 잠금을 잡는 겉 함수와 잠금 없는 `subscriberInfoLocked`로 나눴다. `GetAllSubscriberInfo`는 후자를 쓴다 | `TestGetAllSubscriberInfoUnderWriters` 실패: deadlock(40초 timeout) |
| C5 `/ws` hub | broadcast에서 느린 클라이언트를 map에서 지울 때 쓰기 잠금을 잡는다 | 시험 없음 |
| C5 rate limiter | `lastAccess`를 atomic 값(UnixNano)으로 바꿨다 | 시험 없음. 기존 시험을 새 타입에 맞췄다 |

**기존 시험 race.** `pkg/events`의 `TestMetrics_FilteredEvents`가 race 검사에서 가끔 실패했다(변경 전 커밋 5회 중 1회). 원인은 시험 코드의 카운터를 잠금 없이 쓰는 것이었고, atomic으로 바꿨다. 별도 프로세스로 10회 돌려 실패 0회를 확인했다. 한 프로세스에서 `-count`를 2 이상 주면 Prometheus metric 중복 등록으로 panic이 나는데, 이것은 원래 시험 구조의 문제라 이번에는 고치지 않았다.

**Etherscan 검증 상태 race(새로 찾음).** race 검사에서 `pkg/api/etherscan`이 실패했다(변경 전 커밋 3회 중 3회). 원인은 운영 코드에 있었다. 검증 상태 조회가 잠금 안에서 job 포인터만 꺼내고, 잠금을 푼 뒤 상태 필드를 읽었다. 그 사이 백그라운드 검증 고루틴이 같은 필드를 쓴다. 상태와 메시지를 잠금 안에서 복사하도록 고쳤고, 시험도 같은 방식으로 읽게 했다. race 검사 5회 모두 통과했다. 그 시험은 job이 아직 "Pending"이라고 가정한다. 검증 고루틴이 아주 빨리 끝나면 불안정해질 수 있다(5회 중 실패 없음).

### P0-12: 설정 정리와 멀티체인 차단 (10/3, F4·D4 임시 조치)

| 항목 | 수정 | 시험 |
|---|---|---|
| CLI 기본값이 설정 파일을 덮음 | 플래그 파싱을 `parseFlagsFrom`(FlagSet)으로 바꾸고 `fs.Visit`으로 명시한 플래그만 기록한다. `applyFlags`와 `applyAPIFlags`는 명시한 플래그만 적용한다 | `TestFlagsOnlyOverrideWhenGiven`(파일의 workers 7 유지, `--workers 3` 적용) |
| bool 플래그로 끌 수 없음 | 명시한 값을 그대로 적용한다 | 같은 시험(`--api=false`) |
| 검증이 플래그 적용 전 | `config.LoadUnvalidated`를 추가했다. `main`은 플래그를 적용한 뒤 `Validate`를 부른다. `config.Load`는 그대로 검증까지 한다 | `TestFlagsCanSupplyRequiredValues` |
| 기본 설정 파일이 없으면 오류 | `--config`를 주지 않았고 `config.yaml`이 없으면 파일 없이 시작한다. 명시한 파일이 없으면 지금처럼 오류다 | `TestDefaultConfigFileIsOptional` |
| D4 멀티체인 | 체인이 설정된 채로 켜면 시작을 거부한다. (R2-8에서 체인별 DB로 해소하고 거부를 지웠다. 지금 이 시험은 DB 경로를 벗어나는 체인 id를 거부하는지 본다) | `TestStartupRejectsUnsafeModes/multichain` |
| `database.readonly` 무시 | 켜면 시작을 거부한다 | `TestStartupRejectsUnsafeModes/readonly` |
| 무시되는 설정 | `Config.UnsupportedSettings`가 목록을 만들고, 시작 로그에 경고로 남긴다 | `TestUnsupportedSettings` |

이 시험들은 새 함수(`parseFlagsFrom`, `LoadUnvalidated`)를 쓰므로 수정 전 코드에서는 돌릴 수 없다. 수정 전 동작은 코드를 읽어 판단했다.

`docs/CONFIG.md`의 우선순위, CLI, 멀티체인 절에 바뀐 동작을 적었다.

P0-4에서 고친 Etherscan 시험이 한 번 실패했다. 검증 고루틴이 먼저 끝나 상태가 "Pending"이 아니었던 경우다. 이 시험은 job 생성을 확인하는 것이 목적이므로, 상태가 알려진 세 값 중 하나인지만 확인하도록 바꿨다(race 검사와 함께 10회 통과).

### P0-11: 범위 없는 목록 조회 (10/3, P1 수정)

**indexer-frontend 사용 확인(설계 7절 항목).** indexer-frontend(`e0f8100`)는 범위 없는 조회를 실제로 쓴다. 거래 목록(`useTransactions`)은 `limit/offset`, 선택적 블록 범위·from·to·type 필터, 페이지 계산용 `totalCount`를 쓴다. 토큰 transfer와 주소 로그는 `logs(filter: {address, topics})`를 `offset`, `totalCount`와 함께 쓴다. 가스 추정은 `transactions(pagination: {limit})`를 쓴다. 그래서 범위 없는 조회를 거부하거나 `totalCount`를 없애면 화면이 깨진다.

**수정.** 범위가 없을 때만 `unbounded_queries.go`의 경로를 탄다. 범위를 준 조회는 바뀌지 않았다.
- 거래는 최신 블록부터 거꾸로, 로그는 0번 블록부터 순서대로 한 블록씩 읽는다. `offset + limit + 1`건을 모으면 멈춘다. 반환 순서는 범위를 준 경로와 같다(거래는 최신 순, 로그는 오래된 순).
- `totalCount`: 필터 없는 거래는 지금처럼 전체 거래 수다(정확). 주소 필터가 있는 거래와 로그는 끝까지 훑었으면 정확하고, 중간에 멈췄으면 하한값이다(더 있으면 `offset + limit + 1` 이상).
- offset이 10,000을 넘으면 블록 범위를 지정하라는 오류를 낸다.

**검증**(`cmd/indexer/graphql_paging_test.go`, 블록 61개 · 거래 1,201개).
- 범위 없는 조회와 "0~최신" 범위를 명시한 조회의 결과(node 목록, `hasNextPage`)가 같다. 필터 없는 거래는 `totalCount`도 같다. offset 0, 25, 500과 주소 필터, 로그(offset 0, 30)에서 확인했다.
- 블록 읽기 횟수: 수정 전 코드는 첫 페이지(10건)에 블록 62개를 읽었고, 수정 후에는 2개 이하다.
- 깊은 offset 거부를 확인했다. 수정 전 코드에서는 거부하지 않았다.

**단점.** 필터가 있는 목록에서는 `totalCount`가 하한값이 되므로, 화면의 전체 페이지 수가 처음에는 작게 보이다가 뒤로 넘길수록 늘어난다. 드문 필터(일치 건수가 적은 주소)는 일치 건을 찾을 때까지 블록을 계속 읽으므로, 최악의 경우 시간은 지금과 같다. 다만 메모리는 한 페이지 분량으로 제한된다. 주소 색인을 쓰는 조회로 바꾸는 일은 Phase 1의 keyset 페이지(R1-5)에서 한다.

### 실제 StableNet 노드 검증 (10/3)

설계 6절의 위험("가짜 서버로 통과해도 실제 노드에서 다를 수 있다")과 7절의 확인 항목(fee delegation decode 경로)을 확인하려고 실제 노드로 시험했다. 로컬에서 go-stablenet 네트워크를 띄웠다(`go-stablenet` `740526d`, `Gstable v1.1.0`, 검증자 3, chainbench).
- macOS의 유닉스 소켓 경로 제한 때문에 노드 설정에서 IPC를 껐다.
- fee delegation을 받으려면 Applepie가 필요해서 genesis에 `applepieBlock: 0`을 넣었다.
- 체인에는 일반 전송과 fee delegation(type 0x16) 전송을 각 6건 보냈다. 보낸 도구는 scratchpad의 `txgen`이고, go-stablenet의 `FeeDelegateDynamicFeeTx`와 `NewFeeDelegateSigner`를 쓴다.

시험은 `cmd/indexer/live_stablenet_test.go`(`TestLiveStableNet`)이다. `INDEXER_LIVE_RPC`가 있을 때만 돌고, 평소의 `go test`에서는 건너뛴다. 이 시험은 노드 감지, WBFT 데이터 저장, 그리고 "끝까지 한 번 색인"과 "세 지점에서 commit 전 crash 뒤 재시작"의 키 공간 비교를 확인한다. `INDEXER_LIVE_FD_TXS`를 주면 fee delegation 메타도 확인한다.

결과(블록 0~788, 키 2,593개):
- **원자적 경로의 crash 복구**: 두 방식의 키 공간이 같다(WBFT 데이터 포함).
- **D14 노드 감지 실패를 찾아 고쳤다.** 실제 client version `Gstable/…`을 Unknown으로 분류해 StableOne adapter가 선택되지 않았다. 감지 규칙에 `gstable`을 추가하고 시험 케이스를 더했다. 기존 시험은 `go-stablenet/v1.0.0`이라는 문자열을 가정하고 있었다.
- **D15 WBFT 해석 실패를 찾아 고쳤다.** 모든 header가 RLP decode에 실패해 WBFT 데이터가 저장되지 않았다. `WBFTExtraRLP.DecodeRLP`의 지역 구조체에 go-stablenet과 같은 `rlp:"nil"` 태그를 붙였다. 수정 후 블록 0~788의 WBFT 키가 810개 저장된다. 회귀 시험 `TestParseWBFTExtraLiveHeaders`는 실제 header 두 개(genesis, 블록 5)를 벡터로 쓴다. 수정 전 코드에서는 실패한다. 원인을 찾는 동안 바깥 구조체의 태그와 seal codec을 고치는 방향으로 잘못 짚었는데, 그 변경은 모두 되돌리고 최소 수정만 남겼다.
- **D13 fee delegation hash 불일치를 찾았고, 아직 고치지 않았다.** 노드가 보고하는 hash(`0xd63d…`, type 0x16)로는 트랜잭션을 찾을 수 없고, receipt는 decode에 실패한다. indexer가 만든 안쪽 hash(`0x271f…`)로는 트랜잭션이 있지만 receipt가 없다. upstream go-ethereum 타입에 type 0x16이 없기 때문에 생기는 문제라, 표현 방식을 정하는 설계 결정이 필요하다(아래).

**D13 수정 방향(결정 필요).**
1. 바깥 hash를 정식 hash로 쓴다. 트랜잭션 위치 색인과 메타를 바깥 hash로 저장하고, receipt는 type을 2로 바꿔 저장하되 원래 type은 메타에 남긴다. 기존 API 형태를 유지할 수 있다. 단점은 저장된 트랜잭션 본문(안쪽 tx)의 hash와 색인 hash가 달라서, 본문으로 hash를 다시 계산하는 코드가 있으면 틀린다는 것이다.
2. go-stablenet의 types 패키지를 의존성으로 바꿔(replace) type 0x16을 그대로 저장한다. 표현은 정확해진다. 단점은 go-ethereum fork에 묶여 다른 EVM 체인 지원(G1)과 충돌한다는 것이다.
