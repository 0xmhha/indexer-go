# 체인 프로필 구조 상세 설계 (10/4)

indexer가 여러 체인을 지원하면서 체인마다 고유한 트랜잭션 타입, 합의 데이터, 시스템 기능을 그대로 다룰 수 있게 하는 구조를 설계한다. 직접적인 계기는 StableNet의 fee delegation 트랜잭션(type 0x16)이다. 지금 indexer는 이 트랜잭션을 안쪽 송신자 트랜잭션으로 바꿔 저장해서, 실제 hash로 조회할 수 없고 receipt도 읽지 못한다(refactoring-plan.md D13). 결정 사항은 두 가지다. 우회하지 않고 type 0x16을 그대로 지원한다. 그리고 go-ethereum을 fork로 통째로 바꾸지 않고 체인별로 커스터마이즈하는 구조로 간다.

이 문서는 refactoring-plan.md의 R1-3(키·값 형식), R2-1(소스 SPI), R2-6(기능 모듈 이전)을 앞당겨 하나로 묶는다.

## 1. 결론

1. **core는 체인 중립 모델만 안다.** 블록, 트랜잭션, receipt, 로그를 indexer 자신의 타입(`core/model`)으로 표현한다. 체인 고유 필드(예: fee payer)는 타입이 정해진 확장(`Ext`)으로 담는다. go-ethereum 타입은 core에서 쓰지 않는다.
2. **체인 프로필이 체인 고유 규칙을 맡는다.** 노드 감지, RPC 응답 decode, 트랜잭션 hash와 송신자 복원, 합의 정보 해석, 체인 기본 기능을 프로필이 구현하고 레지스트리에 등록한다.
   - EVM 프로필은 표준 타입을 다룬다. 프로필 안에서는 upstream go-ethereum을 그대로 써도 된다.
   - StableNet 프로필은 EVM 프로필을 확장해 type 0x16, WBFT, 시스템 컨트랙트를 다룬다.
3. **type 0x16은 명세대로 직접 구현한다(4절).** go-stablenet 코드를 복사하지 않는다. go-stablenet의 `core/types`는 LGPL-3.0이고 이 저장소는 Apache-2.0이라, 복사하면 라이선스 의무가 생긴다(법무 확인 대상).
4. **저장 형식을 중립 모델의 버전 있는 형식으로 바꾼다.** 이 작업이 R1-3과 겹치므로 함께 해서 재색인을 한 번으로 줄인다. 기존 형식과는 새 DB 디렉터리(schema v2)로 나란히 두고, 검증한 뒤 바꿔 끼운다(점진 교체).
5. **검증 기준.** 로컬 StableNet 네트워크의 실제 type 0x16 트랜잭션으로 다음을 확인한다. 노드가 보고하는 hash로 트랜잭션과 receipt를 조회할 수 있어야 한다. 송신자·fee payer가 노드 값과 같아야 한다. 그 트랜잭션의 Transfer 로그가 색인되어야 한다.

---

## 2. 배경

### 2.1 지금의 결합

| 항목 | 현황 |
|---|---|
| core가 쓰는 go-ethereum 타입 | `*types.Transaction` 22개 파일·117곳, `*types.Receipt` 25개 파일·136곳, `*types.Block` 36개 파일·295곳, `*types.Log` 29개 파일·280곳(시험 제외). 주로 `pkg/fetch`(12), `pkg/storage`(9), `pkg/api/graphql`(5)에 있다 |
| 저장 값 형식 | 블록·트랜잭션·receipt·로그를 go-ethereum RLP로 저장한다(`storage/encoder.go`). go-ethereum이 모르는 타입은 저장하거나 읽을 수 없다(type 0x16 receipt의 decode 실패가 이 때문이다) |
| RPC decode | `ethclient`가 JSON을 go-ethereum 타입으로 decode한다. 실패하면 `factory.EVMClient`가 원시 JSON으로 다시 받아 type 0x16을 안쪽 tx로 바꾼다(D13의 원인) |
| 송신자 복원 | `types.Sender(types.LatestSignerForChainID(...))`. type 0x16의 송신자와 fee payer를 구분할 수 없다 |
| 체인 고유 코드 | 시스템 컨트랙트 주소, WKRC, WBFT 해석, fee delegation 타입 번호(22)가 core 여러 곳에 박혀 있다(refactoring-plan.md 5.3절) |

### 2.2 검토했다가 버린 방식

| 방식 | 버린 이유 |
|---|---|
| go-ethereum을 go-stablenet으로 전역 replace | v1.1.0으로 시도하니 indexer 빌드가 실패했다(`Header.RequestsHash` 없음, `Block.WithBody` 시그니처, pebble·go-verkle 불일치). go-stablenet은 upstream보다 오래된 go-ethereum을 기반으로 한다. 바꾸면 모든 체인이 이 fork에 묶인다(G1과 충돌) |
| type 0x16을 type 2로 바꿔 저장(지금 방식) | 우회다. hash가 틀리고 receipt를 읽을 수 없다 |
| 체인별 바이너리를 빌드 태그로 따로 빌드 | 두 go-ethereum API를 모두 컴파일하는 호환 계층이 필요하고, 빌드·배포 경로가 체인 수만큼 는다 |

---

## 3. 목표 구조

### 3.1 core 중립 모델 (`pkg/core/model`)

```go
// Block is a chain-neutral block. Hash and ParentHash are the values the
// chain reports; profiles may verify them.
type Block struct {
	Number       uint64
	Hash         common.Hash
	ParentHash   common.Hash
	Time         uint64
	Miner        common.Address
	GasLimit     uint64
	GasUsed      uint64
	BaseFee      *big.Int
	Extra        []byte          // raw header extra (consensus data)
	Transactions []*Transaction
	Ext          Extensions      // chain-specific header fields
}

// Transaction is a chain-neutral transaction.
type Transaction struct {
	Hash      common.Hash     // canonical hash on its chain
	Type      uint8           // chain's type number, not remapped
	ChainID   *big.Int
	Nonce     uint64
	From      common.Address  // recovered sender
	To        *common.Address // nil for contract creation
	Value     *big.Int
	Gas       uint64
	GasPrice  *big.Int        // legacy/2930; for 1559-style the fee cap
	GasTipCap *big.Int
	GasFeeCap *big.Int
	Input     []byte
	AccessList []AccessTuple
	AuthList   []SetCodeAuthorization // EIP-7702
	Signature  Signature              // v, r, s of the sender
	Raw        []byte                 // canonical encoding (type byte || payload)
	Index      uint                   // position in block
	Ext        Extensions             // e.g. stablenet.FeeDelegation
}

// Receipt and Log follow the same pattern (Type kept as reported).
```

- `common.Hash`, `common.Address` 같은 기본 타입은 go-ethereum `common`을 그대로 쓴다. 이 패키지는 체인 규칙이 없는 값 타입이다.
- `Raw`에 체인의 정식 인코딩을 남긴다. 그러면 모델에 아직 없는 필드도 잃지 않고, 나중에 다시 해석할 수 있다.
- `Ext`는 문자열 키 맵이 아니다. 프로필이 정의한 구조체를 담는다. 기능 모듈은 `stablenet.FeeDelegationOf(tx)` 같은 접근자로 꺼낸다.

### 3.2 체인 프로필 SPI (`pkg/chains`)

```go
type Profile interface {
	ID() string // "evm", "stablenet", "anvil"
	// Detect reports whether the node behind info belongs to this profile.
	Detect(info NodeInfo) bool
	// DecodeBlock turns an eth_getBlockByNumber(full=true) result into the
	// neutral model, recovering senders and verifying transaction hashes.
	DecodeBlock(raw json.RawMessage) (*model.Block, error)
	// DecodeReceipts turns an eth_getBlockReceipts result into the model.
	DecodeReceipts(raw json.RawMessage) ([]*model.Receipt, error)
	// Consensus returns the consensus data parser, or nil.
	Consensus() ConsensusParser
	// Features lists features this chain enables by default (refactoring
	// plan 5.3: e.g. stablenet.system_contracts, stablenet.fee_delegation).
	Features() []string
}
```

- 프로필은 `init()`에서 레지스트리에 등록한다. 감지 순서는 구체적인 프로필(StableNet) → 일반 EVM이다. `--adapter`(나중에는 `chain.profile` 설정)로 강제할 수 있다.
- 소스 계층(R2-1)은 `rpc.Client`로 원시 JSON만 가져오고, decode는 프로필에 맡긴다. `ethclient`는 core에서 쓰지 않는다.
- 지금의 `pkg/adapters`(감지, 블록 fetcher, 합의 파서, 시스템 컨트랙트)는 프로필로 흡수한다.

### 3.3 EVM 프로필

- 표준 타입(0, 1, 2, 3, 4)은 프로필 안에서 upstream go-ethereum의 `types.Transaction` JSON decode, `Hash()`, `types.Sender`를 써서 처리하고 모델로 옮긴다. 검증된 구현을 그대로 쓰므로 정확하다.
- **모르는 타입 정책**(결정 필요, 7절): 권장은 블록을 실패시키지 않고 `Raw`와 JSON에서 읽을 수 있는 공통 필드만 담아 저장하는 것이다. 이때 노드가 준 hash를 그대로 쓰고 경고 지표를 올린다.

### 3.4 StableNet 프로필

EVM 프로필을 포함하고 다음을 더한다.
- type 0x16 codec(4절)
- 노드 감지(`Gstable/`, `go-stablenet`, chain id)
- WBFT 합의 해석(현재 `storage/wbft_parser.go`를 이동)
- 시스템 컨트랙트(0x1000~0x1004)와 WKRC
- 기본 기능: `stablenet.system_contracts`, `stablenet.fee_delegation`, `stablenet.wbft`

---

## 4. type 0x16 명세

go-stablenet v1.1.0의 동작에서 확인한 명세다. 구현은 이 명세로 독립적으로 한다.

| 항목 | 명세 |
|---|---|
| 타입 번호 | `0x16` (22) |
| 정식 인코딩 | `0x16 ‖ rlp([sender, feePayer, fv, fr, fs])`. `sender` = `[chainId, nonce, maxPriorityFeePerGas, maxFeePerGas, gas, to, value, data, accessList, v, r, s]`(EIP-1559 tx와 같은 필드 순서, 서명 포함). `to`가 없으면 빈 문자열 |
| 트랜잭션 hash | `keccak256(정식 인코딩)` |
| 송신자 | `sender`의 서명 `(v, r, s)`로 복원한다. 서명 대상은 EIP-1559 sighash `keccak256(0x02 ‖ rlp([chainId, nonce, tip, feeCap, gas, to, value, data, accessList]))`. 복원에는 `v + 27`을 쓴다 |
| fee payer | `(fv, fr, fs)`로 복원한다. 서명 대상은 `keccak256(0x16 ‖ rlp([sender(서명 포함 12개), feePayer]))`. 복원한 주소가 `feePayer` 필드와 같아야 한다 |
| JSON(`eth_getBlockByNumber` full) | 일반 dynamic fee 필드(`chainId`, `nonce`, `maxPriorityFeePerGas`, `maxFeePerGas`, `gas`, `to`, `value`, `input`, `accessList`, `v`, `r`, `s`)에 `feePayer`, `fv`, `fr`, `fs`가 더해진다. `type`은 `0x16`, `hash`는 위 hash |
| 유효 가스 가격 | EIP-1559와 같다: `min(feeCap, baseFee + tip)` |
| 수수료 부담자 | 가스비는 fee payer가, value는 송신자가 낸다. go-stablenet의 상태 전이(`core/state_transition.go`)가 fee delegation일 때 가스 구매와 환불을 fee payer 계정으로 처리한다. 잔액 추적(`processBalanceTracking`)에서 차감 대상을 나눠야 한다 |
| receipt | `type: 0x16`, 나머지는 EIP-1559 receipt와 같다. receipt의 `from`은 송신자다 |

**구현 위치.** `pkg/chains/stablenet/feedelegation.go`. RLP는 go-ethereum `rlp` 패키지를 쓴다. 이 패키지는 체인 규칙이 없는 범용 인코더라 core에 둬도 문제없다.

**시험 벡터.** 로컬 StableNet 네트워크에서 다음을 수집해 `testdata`로 고정한다.
- type 0x16 트랜잭션 몇 건의 JSON
- `eth_getRawTransactionByHash` 결과(정식 인코딩)
- 노드가 보고한 hash, receipt의 `from`, `feePayer`

벡터에 대해 hash, 송신자, fee payer, 재인코딩 바이트가 모두 일치해야 한다.

**잔액 처리 주의.** 지금의 잔액 추적은 `value + gasUsed × gasPrice`를 송신자에게서 뺀다. type 0x16에서는 가스비를 fee payer가 내므로 이 규칙이 틀린다. 프로필이 "수수료 부담자"를 알려 주는 연결점(`FeePayerOf(tx)`)이 필요하다.

---

## 5. 저장 형식 (R1-3과 묶음)

- 값은 중립 모델의 버전 있는 이진 형식으로 저장한다. 권장은 모델 구조체에 대한 RLP이고, 버전 바이트를 앞에 붙인다. 트랜잭션은 `Raw`를 함께 저장하므로 원본이 보존된다.
- 키는 R1-3의 고정 길이 이진 키로 바꾼다.
- 스키마 버전을 `/meta/schema`에 기록한다. 시작할 때 버전이 다르면 멈추고 재색인을 안내한다.
- **점진 교체.** 새 형식(schema v2)은 새 DB 디렉터리에 만든다. 운영에서는 기존 인스턴스를 그대로 둔 채 v2 인스턴스로 재색인을 끝낸다. 그다음 API 결과를 비교하고 바꿔 끼운다. 기존 형식의 코드는 그 뒤 한 릴리스가 지나면 지운다.

---

## 6. 단계와 검증

| 단계 | 내용 | 검증 기준 |
|---|---|---|
| CP-1 | `core/model`, 프로필 SPI·레지스트리, EVM 프로필(표준 타입 → 모델) | 가짜 체인 시나리오의 모든 블록에서, 모델의 hash·송신자·필드가 go-ethereum으로 decode한 값과 같다 |
| CP-2 | StableNet 프로필: type 0x16 codec, 감지, WBFT 이동 | 실제 네트워크 벡터로 hash·송신자·fee payer·재인코딩이 일치한다 |
| CP-3 | 소스 계층이 원시 JSON → 프로필 decode → 모델을 돌려준다. `ethclient`와 `factory.EVMClient`의 변환 경로를 없앤다 | 블록 fetch 경로에서 go-ethereum 타입 decode가 사라졌다(그래프 도구로 확인) |
| CP-4 | 저장 계층을 모델 기반 schema v2로(R1-3 포함). 수집 처리기와 기능 모듈이 모델을 쓰도록 이전 | 가짜 체인 golden을 v2로 다시 만든다. v1과 v2의 API 결과(GraphQL golden)가 type 0x16 관련 항목 외에는 같다 |
| CP-5 | API 계층이 모델을 읽는다(GraphQL, JSON-RPC, Etherscan). type 0x16의 fee payer 필드를 응답에 넣는다 | indexer-frontend 회귀, JSON-RPC 응답의 `type: "0x16"`과 `feePayer` |
| CP-6 | 실제 StableNet 검증 | `TestLiveStableNet`에 D13 항목을 추가한다. 실제 hash로 트랜잭션과 receipt를 조회할 수 있고, 송신자·fee payer가 노드 값과 같고, Transfer 로그가 색인되고, fee payer의 잔액 이력에 가스비가 반영되어야 한다 |

CP-1과 CP-2는 기존 코드에 영향 없이 새 패키지로 만들 수 있다. CP-3부터 기존 경로를 교체한다.

---

## 7. 결정이 필요한 것

| 결정 | 선택지 | 권장 | 권장안의 단점 |
|---|---|---|---|
| 모르는 tx 타입 | 블록 실패 / 공통 필드 + `Raw`로 저장 | 저장 + 경고 지표 | 그 타입 고유 정보는 프로필이 생길 때까지 해석되지 않는다 |
| 노드가 준 hash | 그대로 신뢰 / 프로필이 다시 계산해 검증 | 다시 계산해 검증(불일치하면 블록 실패) | 트랜잭션마다 keccak 비용이 든다(작다) |
| 저장 값 인코딩 | 모델 RLP / protobuf / JSON | 모델 RLP + 버전 바이트 | 필드를 추가할 때 RLP 순서 규칙을 지켜야 한다 |
| 라이선스 | 명세로 직접 구현 / go-stablenet 코드 복사 | 직접 구현 | 구현과 시험 벡터 작성 비용이 든다 |

---

**적용(10/4).** 따로 지시가 없어 위 표의 권장안 네 가지로 진행한다. 바꾸면 CP-1~CP-2 범위 안에서 반영할 수 있다.

## 8. 단점과 한계

- **범위가 크다.** core 타입 교체가 약 40개 파일, 860곳에 걸친다. CP-3~CP-5 동안 기능 개발이 멈추거나 느려진다.
- **재색인이 필요하다.** schema v2는 기존 DB와 호환되지 않는다. 새 DB를 옆에서 만드는 동안 디스크가 두 배 필요하다.
- **명세를 직접 구현하므로** go-stablenet이 type 0x16을 바꾸면 따라가야 한다. 시험 벡터를 노드 버전마다 다시 수집하는 절차가 필요하다.
- 이 문서의 type 0x16 명세는 go-stablenet v1.1.0 코드를 읽어 정리한 것이다. 노드 dev 브랜치(`740526d`)와의 차이는 확인하지 않았다. CP-2의 벡터 시험이 실제 노드와의 일치를 판정한다.
- 라이선스 판단(LGPL 코드를 읽고 독립 구현하는 것)은 법무 확인 대상이다.

---

## 9. 진행 기록

### CP-1: 중립 모델, 프로필 SPI, EVM 프로필 (10/4)

| 산출물 | 내용 |
|---|---|
| `pkg/core/model` | `Block`, `Transaction`, `Receipt`, `Log`, `Signature`, `AccessTuple`, `SetCodeAuthorization`. 체인 고유 필드는 `Extensions`(`*ExtKey`로 키를 정해 식별자 단위로 비교하므로 이름이 겹쳐도 충돌하지 않는다)에 담는다. 트랜잭션의 `Type`은 체인 값 그대로이고, `Raw`에 정식 인코딩을 남긴다. 어느 프로필도 모르는 타입은 `Opaque`로 표시한다 |
| `pkg/chains` | `Profile` SPI(`ID`, `Detect`, `DecodeBlock`, `DecodeReceipts`, `Features`), 우선순위가 있는 레지스트리, `Detect`, `Lookup`. 같은 id를 두 번 등록하면 panic(배선 오류)이다 |
| `pkg/chains/evm` | 범용 EVM 프로필(id `evm`, 우선순위 0, 모든 노드를 받는 fallback). 표준 타입은 upstream go-ethereum을 codec으로만 써서 decode한다. 트랜잭션 hash와 송신자를 다시 계산해 노드 값과 비교하고, 다르면 `ErrHashMismatch`/`ErrSenderMismatch`로 블록을 실패시킨다. 블록 hash도 header로 다시 계산해 비교하며, 끌 수 있다(`WithHeaderHashCheck`). 체인 고유 타입은 `WithTxDecoder`로 등록한 decoder가 우선한다. 아무도 모르는 타입은 노드가 보고한 hash와 송신자로 opaque 저장한다 |

검증 결과(`pkg/chains/evm/evm_test.go`, `pkg/chains/profile_test.go`, race 검사 포함)는 다음과 같다.
- `TestDecodeMatchesGoEthereum`: 기준 시나리오(블록 21개, legacy·dynamic fee·SetCode 포함)와 부하 시나리오(블록 12개)의 모든 블록에서, 모델의 블록 hash, 트랜잭션 hash·타입·송신자·필드·정식 인코딩, receipt·로그가 `ethclient`가 decode한 값과 같다.
- 모르는 타입 opaque 저장, hash·송신자·블록 hash 위조 거부, 체인 고유 decoder 우선 적용, 감지 우선순위와 fallback을 확인했다.

기존 코드는 아직 이 패키지들을 쓰지 않는다. 연결은 CP-3에서 한다.

**CP-2 전에 확인할 것.** 실제 StableNet header로 계산한 hash가 노드 hash와 같은지 확인해야 한다. 같지 않으면 StableNet 프로필에서 header hash 검사를 끄거나 StableNet 규칙으로 계산해야 한다.
