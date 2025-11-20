# Stable-One 체인 기술 분석

Stable-One은 go-ethereum을 기반으로 WBFT를 확장한 **Anzeon 엔진**을 사용하는 퍼블릭 체인으로, 가스비를 스테이블 토큰으로 지불하도록 설계되었습니다. Gno 체인(Tendermint2)과 전혀 다른 합의·데이터 구조를 채택하기 때문에 indexer-go는 Gno 시절과 다른 전략이 필요합니다.

**Last Updated**: 2025-11-20

---

## 문서 목적

1. Stable-One과 Gno 아키텍처 차이를 빠르게 파악한다.
2. indexer-go 코드 관점에서 이미 구현된 부분과 남은 작업을 명확히 한다.
3. Stable-One 체인 코드(`/Users/wm-it-22-00661/Work/github/stable-net/test/go-stablenet`)의 최신 변경 내용을 반영한다.

---

## 1. Gno vs Stable-One 핵심 비교

| 항목 | Gno (TM2) | Stable-One (Anzeon/WBFT) |
|------|-----------|---------------------------|
| 기반 프레임워크 | Cosmos SDK + Tendermint2 | go-ethereum 포크 (`go-stablenet`) |
| 합의 | Tendermint BFT, 5s | Anzeon WBFT(`consensus/wbft`), 1s, epoch 기반 validator 교체 |
| 베이스 코인 | genesis 고정 발행 | `NativeCoinAdapter` 시스템 컨트랙트(0x1000)가 스테이블코인 발행/소각 |
| 거버넌스 | 체인 코드 | GovValidator/GovMinter/GovMasterMinter/GovCouncil 시스템 컨트랙트 |
| 트랜잭션 타입 | MsgCall/MsgSend | Ethereum typed tx + fee delegation 0x16 |
| 인코딩/해시 | Amino + SHA256 | RLP + Keccak256 |
| RPC | TM2 RPC | Ethereum Execution JSON-RPC, `eth_getBlockReceipts` 지원 |
| 주소 포맷 | bech32 | 0x-prefixed EOA/contract |
| 블록 가스 한도 | 40M 수준 | 105,000,000 (`miner/miner.go`), priority fee 고정 |

---

## 2. Stable-One 체인 세부 구조

### 2.1 Anzeon WBFT 합의
- `params/config_wbft.go`에서 Genesis validator/BLS 키/시스템 컨트랙트를 선언하며 GovValidator 파라미터와 일치해야 합니다.
- `consensus/wbft/engine/engine.go`는 WPoA·Staking·Block reward 로직을 제거하고 GovContract 기반으로 validator 메타데이터를 관리합니다.
- 블록 헤더 `Extra` 필드에 BLS signatures, round, committed seal이 포함되며 WBFT 특화 해시 계산(`WBFTFilteredHeader`)이 적용됩니다.

### 2.2 경제·가스 모델
- 베이스 코인(스테이블 토큰) 잔고는 `systemcontracts/coin_adapter.go`의 NativeCoinAdapter 상태와 이벤트로 추적합니다.
- GovValidator(0x1001)/GovMasterMinter(0x1002)/GovMinter(0x1003)/GovCouncil(0x1004)이 genesis에 배포되며, validator·minter 권한 변경은 해당 컨트랙트 이벤트로 확인합니다.
- `miner/miner.go` 기본 설정: GasCeil=105,000,000, GasPrice=100 gwei, `SetGasTip`/`SetGasPrice`는 Anzeon에서 무시됩니다(`miner/miner.go:215`, `eth/api_miner.go:62-75`).

### 2.3 트랜잭션 타입 및 인코딩
- `core/types/transaction.go`는 Legacy/AccessList/DynamicFee/Blob에 `FeeDelegateDynamicFeeTxType(0x16)`을 추가했습니다.
- `TxData.effectiveGasPrice(baseFee *big.Int, gasTip *big.Int)` 시그니처가 최신 구조입니다. 기존 문서에 있던 `effectiveGasPrice(dst, baseFee)`는 삭제되었습니다.
- Fee delegation용 `feePayer` 캐시와 서명 필드가 Transaction에 포함되며 GraphQL/JSON-RPC에서 노출해야 합니다.
- 인코딩은 전부 RLP이며 typed transaction envelope(EIP-2718)을 사용합니다.

### 2.4 시스템 컨트랙트
- Genesis 샘플(`README.md`)에서 `0x...1000~1004` 주소 공간에 필수 시스템 컨트랙트를 배치합니다.
- NativeCoinAdapter 파라미터: `masterMinter`, `minters`, `minterAllowed`, `name`, `symbol`, `decimals`, `currency`. 슬롯 계산은 `systemcontracts/coin_adapter.go` 참고.
- Gov 시리즈 컨트랙트는 validator/mint 권한, 만료, 멤버 버전을 상태로 저장하므로 인덱서가 이벤트를 별도로 처리해야 합니다 (향후 작업).

---

## 3. indexer-go 구현 현황

### 3.1 구현 완료

#### Analytics API (완료 ✅)
- **Top Miners Query**: `topMiners(limit: Int): [MinerStats!]!`
  - 블록 채굴 횟수 기준 마이너 순위 반환
  - MinerStats: { address, blockCount, lastBlockNumber }
- **Token Balance API**: `tokenBalances(address: Address!): [TokenBalance!]!`
  - ERC-20 Transfer 이벤트 스캔 방식으로 토큰 잔액 조회
  - TokenBalance: { contractAddress, tokenType, balance, tokenId }
  - 주의: 대용량 데이터에서 성능 최적화 필요 (향후 Pre-indexed balances 구현)

#### 기타 완료 기능
- Rate Limiting Middleware
- GraphQL Subscription
- Data Folder Management (--clear-data 옵션)

### 3.2 미구현 API/스키마
- GraphQL/JSON-RPC 레이어는 아직 Gno 구조를 유지하고 있으므로, EIP-1559 필드, fee delegation, NativeCoinAdapter 상태/이벤트 노출은 향후 해야 할 작업입니다.

### 3.3 NativeCoinAdapter & Gov 이벤트 추적
- 현재 indexer-go는 NativeCoinAdapter/Gov 컨트랙트 로그를 별도로 파싱하지 않습니다. base coin 잔액, 활성 minter, validator 변경 히스토리를 API로 제공하려면 로그 인덱싱 파이프라인을 설계해야 합니다.

### 3.4 WBFT 메타데이터 & 모니터링
- Extra 필드 파서, validator 서명 통계, priority fee 변경 감지는 아직 구현되지 않았습니다. Prometheus에 노출하는 메트릭을 향후 설계해야 합니다.

---

## 4. 구현 로드맵 (현황 반영)

| Phase | 목표 | 상태 |
|-------|------|------|
| Phase 2 | 워커 풀 튜닝, Gap 감지, 배치 요청 고도화, Receipt 병렬화 | ⏳ 최적화 필요 |
| Phase 3 | GraphQL/JSON-RPC/WebSocket에서 EVM 필드/fee delegation/NativeCoinAdapter 노출 | ⏳ 미구현 |
| Phase 4 | 주소 인덱싱 확장, 이벤트 필터, ABI 디코딩, rate limiting/caching | ⏳ 미구현 |

---

## 5. 성능 고려사항

- Receipt 조회는 `eth_getBlockReceipts`로 한 번에 처리되지만, RPC rate limit을 고려해 워커 수와 재시도 정책을 조정해야 합니다.
- 105M 가스 블록 때문에 RLP payload가 크므로 Pebble compaction, 디스크 IOPS, 백업 전략을 재검토해야 합니다.
- 현재 Fetcher는 80~150 blocks/s 정도를 목표로 설계되어 있고 병목은 RPC 대역폭과 Receipt 디코딩입니다.

---

## 6. 참고 코드 위치

- 블록/트랜잭션/Receipt: `../go-stablenet/core/types/*.go`
- WBFT 엔진: `../go-stablenet/consensus/wbft/engine`
- 시스템 컨트랙트: `../go-stablenet/systemcontracts`
- Genesis/Anzeon config: `../go-stablenet/params/config_wbft.go`, `../go-stablenet/README.md`
- Miner 설정: `../go-stablenet/miner/miner.go`, `../go-stablenet/eth/api_miner.go`
- indexer-go 클라이언트: `client/client.go`
- indexer-go 스토리지: `storage/`
- indexer-go Fetcher: `fetch/fetcher.go`

---

## 7. 추가 고려사항

- WBFT Extra 필드 파서를 작성해 validator 서명 실패/지연을 감지하면 운영 대시보드에서 활용할 수 있습니다.
- NativeCoinAdapter는 ERC20 이벤트를 발생시키므로, 로그 파서를 추가해 base 코인 총발행량·잔액·minter 정보를 API로 제공할 수 있습니다.
- Fee delegation(0x16) 처리 시 fee payer 잔액을 확인하고 GraphQL에서 `feePayer`, `feePayerSignature` 등 필드를 노출해야 합니다.

---

## 8. 다음 단계

1. GraphQL/JSON-RPC/WebSocket에서 EVM 헤더 필드, fee delegation, NativeCoinAdapter/Gov 이벤트를 노출한다.
2. NativeCoinAdapter 및 Gov 컨트랙트 로그 파이프라인을 설계한다.
3. WBFT Extra 필드 파서를 추가해 validator 별 메트릭을 수집한다.
4. 워커 풀/배치/Gap 감지 등 Fetcher 최적화를 진행한다.
