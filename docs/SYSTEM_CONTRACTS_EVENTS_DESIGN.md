# Stable-One System Contracts Events Tracking Design

> 설계 문서: Stable-One 체인 시스템 컨트랙트 이벤트 추적 및 인덱싱

**Last Updated**: 2025-11-20
**Status**: Design Phase
**Priority**: High (Phase 4)

---

## 1. 개요

### 1.1 목적

Stable-One 체인의 핵심 거버넌스 및 경제 모델을 구현하는 시스템 컨트랙트들의 이벤트를 추적하고 인덱싱하여, 다음 정보를 API로 제공합니다:

- 베이스 코인 (스테이블코인) 발행/소각 히스토리
- Minter 권한 변경 히스토리
- Validator 설정 변경 히스토리
- 거버넌스 제안 및 투표 히스토리
- 블랙리스트 관리 히스토리

### 1.2 시스템 컨트랙트 주소

| 컨트랙트 | 주소 | 역할 |
|---------|------|------|
| NativeCoinAdapter | 0x1000 | 베이스 코인 발행/소각/전송 관리 |
| GovValidator | 0x1001 | Validator 관리 및 WBFT 파라미터 |
| GovMasterMinter | 0x1002 | Minter 권한 관리 |
| GovMinter | 0x1003 | 실제 mint/burn 실행 |
| GovCouncil | 0x1004 | 블랙리스트 및 권한 관리 |

---

## 2. 이벤트 분류 및 구조

### 2.1 NativeCoinAdapter (0x1000) 이벤트

#### 2.1.1 ERC-20 표준 이벤트

```solidity
event Transfer(address indexed from, address indexed to, uint256 value);
event Approval(address indexed owner, address indexed spender, uint256 value);
```

**용도**: 베이스 코인 전송 및 승인 추적
**인덱싱 전략**: 기존 ERC-20 이벤트 처리와 동일

#### 2.1.2 Minting/Burning 이벤트

```solidity
event Mint(address indexed minter, address indexed to, uint256 amount);
event Burn(address indexed burner, uint256 amount);
```

**추적 정보**:
- 총 발행량 변화
- Minter별 발행량 통계
- 시간대별 발행/소각 추이

#### 2.1.3 Minter 관리 이벤트

```solidity
event MinterConfigured(address indexed minter, uint256 minterAllowedAmount);
event MinterRemoved(address indexed oldMinter);
event MasterMinterChanged(address indexed newMasterMinter);
```

**추적 정보**:
- 활성 Minter 목록
- Minter별 허용량 히스토리
- MasterMinter 변경 히스토리

### 2.2 GovBase 공통 이벤트 (모든 Gov 컨트랙트)

#### 2.2.1 제안 관리 이벤트

```solidity
event ProposalCreated(
    uint256 indexed proposalId,
    address indexed proposer,
    bytes32 indexed actionType,
    bytes callData,
    uint256 memberVersion,
    uint256 requiredApprovals,
    uint256 createdAt
);
event ProposalVoted(uint256 indexed proposalId, address indexed voter, bool approval, uint256 approved, uint256 rejected);
event ProposalApproved(uint256 indexed proposalId, address indexed approver, uint256 approved, uint256 rejected);
event ProposalRejected(uint256 indexed proposalId, address indexed rejector, uint256 approved, uint256 rejected);
event ProposalExecuted(uint256 indexed proposalId, address indexed executor, bool success);
event ProposalFailed(uint256 indexed proposalId, address indexed executor, bytes reason);
event ProposalExpired(uint256 indexed proposalId, address indexed executor);
event ProposalCancelled(uint256 indexed proposalId, address indexed canceller);
```

**추적 정보**:
- 제안 생애주기 (생성 → 투표 → 승인/거부 → 실행/실패/만료/취소)
- 컨트랙트별 제안 통계
- 투표 패턴 분석

#### 2.2.2 멤버 관리 이벤트

```solidity
event MemberAdded(address indexed member, uint256 totalMembers, uint32 newQuorum);
event MemberRemoved(address indexed member, uint256 totalMembers, uint32 newQuorum);
event MemberChanged(address indexed oldMember, address indexed newMember);
event QuorumUpdated(uint32 oldQuorum, uint32 newQuorum);
event MaxProposalsPerMemberUpdated(uint256 oldMax, uint256 newMax);
```

**추적 정보**:
- 멤버 변경 히스토리
- Quorum 변경 히스토리
- 컨트랙트별 활성 멤버 목록

### 2.3 GovValidator (0x1001) 특화 이벤트

```solidity
event GasTipUpdated(uint256 oldTip, uint256 newTip, address indexed updater);
```

**추적 정보**:
- Gas tip 변경 히스토리
- Validator 설정 변경 시점

**참고**: Validator 추가/제거는 GovBase의 멤버 관리 이벤트로 추적

### 2.4 GovMasterMinter (0x1002) 특화 이벤트

```solidity
event MinterConfigured(address indexed minter, uint256 allowance);
event MinterRemoved(address indexed minter);
event MaxMinterAllowanceUpdated(uint256 oldLimit, uint256 newLimit);
event EmergencyPaused(uint256 indexed proposalId);
event EmergencyUnpaused(uint256 indexed proposalId);
```

**추적 정보**:
- Minter 권한 설정/제거 히스토리
- Minter 허용량 변경 히스토리
- 긴급 중지/재개 히스토리

### 2.5 GovMinter (0x1003) 특화 이벤트

```solidity
event DepositMintProposed(
    uint256 indexed proposalId,
    address indexed to,
    uint256 indexed amount,
    string depositId
);
event BurnPrepaid(address indexed user, uint256 amount);
event BurnExecuted(address indexed from, uint256 indexed amount, string withdrawalId);
event EmergencyPaused(uint256 indexed proposalId);
event EmergencyUnpaused(uint256 indexed proposalId);
```

**추적 정보**:
- Deposit mint 제안 히스토리
- Burn 실행 히스토리 (예치금 차감 및 실제 소각)
- 긴급 중지/재개 히스토리

### 2.6 GovCouncil (0x1004) 특화 이벤트

```solidity
event AddressBlacklisted(address indexed account, uint256 indexed proposalId);
event AddressUnblacklisted(address indexed account, uint256 indexed proposalId);
event AuthorizedAccountAdded(address indexed account, uint256 indexed proposalId);
event AuthorizedAccountRemoved(address indexed account, uint256 indexed proposalId);
event ProposalExecutionSkipped(address indexed account, uint256 indexed proposalId, string reason);
```

**추적 정보**:
- 블랙리스트 변경 히스토리
- 권한 계정 변경 히스토리
- 제안 실행 스킵 히스토리

---

## 3. Storage Layer 설계

### 3.1 새로운 인터페이스

```go
// SystemContractReader provides read-only access to system contract events
type SystemContractReader interface {
    // NativeCoinAdapter queries
    GetTotalSupply(ctx context.Context) (*big.Int, error)
    GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*MintEvent, error)
    GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*BurnEvent, error)
    GetActiveMinters(ctx context.Context) ([]common.Address, error)
    GetMinterAllowance(ctx context.Context, minter common.Address) (*big.Int, error)
    GetMinterHistory(ctx context.Context, minter common.Address) ([]*MinterConfigEvent, error)

    // GovValidator queries
    GetActiveValidators(ctx context.Context) ([]common.Address, error)
    GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*GasTipUpdateEvent, error)
    GetValidatorHistory(ctx context.Context, validator common.Address) ([]*ValidatorChangeEvent, error)

    // GovMasterMinter queries
    GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*MinterConfigEvent, error)
    GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*EmergencyPauseEvent, error)

    // GovMinter queries
    GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status ProposalStatus) ([]*DepositMintProposal, error)
    GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*BurnEvent, error)

    // GovCouncil queries
    GetBlacklistedAddresses(ctx context.Context) ([]common.Address, error)
    GetBlacklistHistory(ctx context.Context, address common.Address) ([]*BlacklistEvent, error)
    GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error)

    // Generic governance queries
    GetProposals(ctx context.Context, contract common.Address, status ProposalStatus, limit, offset int) ([]*Proposal, error)
    GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*Proposal, error)
    GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*ProposalVote, error)
    GetMemberHistory(ctx context.Context, contract common.Address) ([]*MemberChangeEvent, error)
}

// SystemContractWriter provides write access for system contract event indexing
type SystemContractWriter interface {
    // Index events from logs
    IndexSystemContractEvent(ctx context.Context, log *types.Log) error
    IndexSystemContractEvents(ctx context.Context, logs []*types.Log) error
}
```

### 3.2 데이터 구조

```go
// MintEvent represents a Mint event
type MintEvent struct {
    BlockNumber uint64
    TxHash      common.Hash
    Minter      common.Address
    To          common.Address
    Amount      *big.Int
    Timestamp   uint64
}

// BurnEvent represents a Burn event
type BurnEvent struct {
    BlockNumber uint64
    TxHash      common.Hash
    Burner      common.Address
    Amount      *big.Int
    Timestamp   uint64
    // For GovMinter burn
    WithdrawalID string
}

// MinterConfigEvent represents Minter configuration changes
type MinterConfigEvent struct {
    BlockNumber uint64
    TxHash      common.Hash
    Minter      common.Address
    Allowance   *big.Int
    Action      string // "configured" or "removed"
    Timestamp   uint64
}

// Proposal represents a governance proposal
type Proposal struct {
    Contract       common.Address
    ProposalID     *big.Int
    Proposer       common.Address
    ActionType     [32]byte
    CallData       []byte
    MemberVersion  *big.Int
    RequiredApprovals uint32
    Approved       uint32
    Rejected       uint32
    Status         ProposalStatus
    CreatedAt      uint64
    ExecutedAt     *uint64
    BlockNumber    uint64
    TxHash         common.Hash
}

// ProposalStatus enum
type ProposalStatus uint8
const (
    ProposalStatusNone ProposalStatus = iota
    ProposalStatusVoting
    ProposalStatusApproved
    ProposalStatusExecuted
    ProposalStatusCancelled
    ProposalStatusExpired
    ProposalStatusFailed
    ProposalStatusRejected
)

// ProposalVote represents a vote on a proposal
type ProposalVote struct {
    ProposalID  *big.Int
    Voter       common.Address
    Approval    bool
    BlockNumber uint64
    TxHash      common.Hash
    Timestamp   uint64
}

// GasTipUpdateEvent represents a gas tip update
type GasTipUpdateEvent struct {
    BlockNumber uint64
    TxHash      common.Hash
    OldTip      *big.Int
    NewTip      *big.Int
    Updater     common.Address
    Timestamp   uint64
}

// BlacklistEvent represents blacklist changes
type BlacklistEvent struct {
    BlockNumber uint64
    TxHash      common.Hash
    Account     common.Address
    Action      string // "blacklisted" or "unblacklisted"
    ProposalID  *big.Int
    Timestamp   uint64
}

// ValidatorChangeEvent represents validator changes
type ValidatorChangeEvent struct {
    BlockNumber uint64
    TxHash      common.Hash
    Validator   common.Address
    Action      string // "added", "removed", "changed"
    OldValidator *common.Address // for "changed" action
    Timestamp   uint64
}

// MemberChangeEvent represents member changes in Gov contracts
type MemberChangeEvent struct {
    Contract     common.Address
    BlockNumber  uint64
    TxHash       common.Hash
    Member       common.Address
    Action       string // "added", "removed", "changed"
    OldMember    *common.Address
    TotalMembers uint64
    NewQuorum    uint32
    Timestamp    uint64
}

// EmergencyPauseEvent represents emergency pause/unpause
type EmergencyPauseEvent struct {
    Contract    common.Address
    BlockNumber uint64
    TxHash      common.Hash
    ProposalID  *big.Int
    Action      string // "paused" or "unpaused"
    Timestamp   uint64
}

// DepositMintProposal represents a deposit mint proposal
type DepositMintProposal struct {
    ProposalID  *big.Int
    To          common.Address
    Amount      *big.Int
    DepositID   string
    Status      ProposalStatus
    BlockNumber uint64
    TxHash      common.Hash
    Timestamp   uint64
}
```

### 3.3 Storage 키 설계 (PebbleDB)

```
Prefix:
- sys_mint:        NativeCoinAdapter Mint events
- sys_burn:        NativeCoinAdapter Burn events
- sys_minter:      Minter configuration events
- sys_validator:   Validator events
- sys_proposal:    Governance proposals
- sys_vote:        Proposal votes
- sys_blacklist:   Blacklist events
- sys_member:      Member change events
- sys_gastip:      Gas tip update events
- sys_emergency:   Emergency pause events

Key formats:
- sys_mint:{blockNumber}:{txIndex}:{logIndex} → MintEvent
- sys_burn:{blockNumber}:{txIndex}:{logIndex} → BurnEvent
- sys_minter:{minter}:{blockNumber} → MinterConfigEvent
- sys_validator:{validator}:{blockNumber} → ValidatorChangeEvent
- sys_proposal:{contract}:{proposalId} → Proposal
- sys_vote:{contract}:{proposalId}:{voter} → ProposalVote
- sys_blacklist:{address}:{blockNumber} → BlacklistEvent
- sys_member:{contract}:{blockNumber}:{txIndex} → MemberChangeEvent
- sys_gastip:{blockNumber}:{txIndex} → GasTipUpdateEvent
- sys_emergency:{contract}:{blockNumber}:{txIndex} → EmergencyPauseEvent

Indexes:
- idx_mint_minter:{minter}:{blockNumber} → TxHash
- idx_burn_burner:{burner}:{blockNumber} → TxHash
- idx_proposal_status:{contract}:{status}:{proposalId} → empty
- idx_blacklist_active:{address} → empty (exists = blacklisted)
- idx_minter_active:{address} → Allowance
- idx_validator_active:{address} → empty (exists = active)
```

---

## 4. Event Parsing 구현

### 4.1 Event Signature 계산

```go
var (
    // NativeCoinAdapter events
    EventSigMint                = crypto.Keccak256Hash([]byte("Mint(address,address,uint256)"))
    EventSigBurn                = crypto.Keccak256Hash([]byte("Burn(address,uint256)"))
    EventSigMinterConfigured    = crypto.Keccak256Hash([]byte("MinterConfigured(address,uint256)"))
    EventSigMinterRemoved       = crypto.Keccak256Hash([]byte("MinterRemoved(address)"))
    EventSigMasterMinterChanged = crypto.Keccak256Hash([]byte("MasterMinterChanged(address)"))

    // GovBase events
    EventSigProposalCreated   = crypto.Keccak256Hash([]byte("ProposalCreated(uint256,address,bytes32,bytes,uint256,uint256,uint256)"))
    EventSigProposalVoted     = crypto.Keccak256Hash([]byte("ProposalVoted(uint256,address,bool,uint256,uint256)"))
    EventSigProposalApproved  = crypto.Keccak256Hash([]byte("ProposalApproved(uint256,address,uint256,uint256)"))
    EventSigProposalRejected  = crypto.Keccak256Hash([]byte("ProposalRejected(uint256,address,uint256,uint256)"))
    EventSigProposalExecuted  = crypto.Keccak256Hash([]byte("ProposalExecuted(uint256,address,bool)"))
    EventSigProposalFailed    = crypto.Keccak256Hash([]byte("ProposalFailed(uint256,address,bytes)"))
    EventSigProposalExpired   = crypto.Keccak256Hash([]byte("ProposalExpired(uint256,address)"))
    EventSigProposalCancelled = crypto.Keccak256Hash([]byte("ProposalCancelled(uint256,address)"))
    EventSigMemberAdded       = crypto.Keccak256Hash([]byte("MemberAdded(address,uint256,uint32)"))
    EventSigMemberRemoved     = crypto.Keccak256Hash([]byte("MemberRemoved(address,uint256,uint32)"))
    EventSigMemberChanged     = crypto.Keccak256Hash([]byte("MemberChanged(address,address)"))
    EventSigQuorumUpdated     = crypto.Keccak256Hash([]byte("QuorumUpdated(uint32,uint32)"))

    // GovValidator events
    EventSigGasTipUpdated = crypto.Keccak256Hash([]byte("GasTipUpdated(uint256,uint256,address)"))

    // GovMasterMinter events
    EventSigMaxMinterAllowanceUpdated = crypto.Keccak256Hash([]byte("MaxMinterAllowanceUpdated(uint256,uint256)"))
    EventSigEmergencyPaused           = crypto.Keccak256Hash([]byte("EmergencyPaused(uint256)"))
    EventSigEmergencyUnpaused         = crypto.Keccak256Hash([]byte("EmergencyUnpaused(uint256)"))

    // GovMinter events
    EventSigDepositMintProposed = crypto.Keccak256Hash([]byte("DepositMintProposed(uint256,address,uint256,string)"))
    EventSigBurnPrepaid         = crypto.Keccak256Hash([]byte("BurnPrepaid(address,uint256)"))
    EventSigBurnExecuted        = crypto.Keccak256Hash([]byte("BurnExecuted(address,uint256,string)"))

    // GovCouncil events
    EventSigAddressBlacklisted      = crypto.Keccak256Hash([]byte("AddressBlacklisted(address,uint256)"))
    EventSigAddressUnblacklisted    = crypto.Keccak256Hash([]byte("AddressUnblacklisted(address,uint256)"))
    EventSigAuthorizedAccountAdded  = crypto.Keccak256Hash([]byte("AuthorizedAccountAdded(address,uint256)"))
    EventSigAuthorizedAccountRemoved = crypto.Keccak256Hash([]byte("AuthorizedAccountRemoved(address,uint256)"))
)

// System contract addresses
var (
    NativeCoinAdapterAddress = common.HexToAddress("0x1000")
    GovValidatorAddress      = common.HexToAddress("0x1001")
    GovMasterMinterAddress   = common.HexToAddress("0x1002")
    GovMinterAddress         = common.HexToAddress("0x1003")
    GovCouncilAddress        = common.HexToAddress("0x1004")
)
```

### 4.2 Event Parser 구조

```go
// events/system_contracts.go

type SystemContractEventParser struct {
    storage storage.SystemContractWriter
    logger  *zap.Logger
}

func NewSystemContractEventParser(storage storage.SystemContractWriter, logger *zap.Logger) *SystemContractEventParser {
    return &SystemContractEventParser{
        storage: storage,
        logger:  logger,
    }
}

func (p *SystemContractEventParser) ParseAndIndexLogs(ctx context.Context, logs []*types.Log) error {
    for _, log := range logs {
        if err := p.parseAndIndexLog(ctx, log); err != nil {
            p.logger.Error("failed to parse system contract log",
                zap.String("address", log.Address.Hex()),
                zap.String("txHash", log.TxHash.Hex()),
                zap.Error(err))
            // Continue processing other logs
            continue
        }
    }
    return nil
}

func (p *SystemContractEventParser) parseAndIndexLog(ctx context.Context, log *types.Log) error {
    // Check if log is from system contract
    if !isSystemContract(log.Address) {
        return nil
    }

    // Route to appropriate parser based on event signature
    if len(log.Topics) == 0 {
        return nil
    }

    eventSig := log.Topics[0]

    switch eventSig {
    case EventSigMint:
        return p.parseMintEvent(ctx, log)
    case EventSigBurn:
        return p.parseBurnEvent(ctx, log)
    case EventSigMinterConfigured:
        return p.parseMinterConfiguredEvent(ctx, log)
    case EventSigProposalCreated:
        return p.parseProposalCreatedEvent(ctx, log)
    // ... more event cases
    default:
        // Unknown event, skip
        return nil
    }
}

func isSystemContract(addr common.Address) bool {
    return addr == NativeCoinAdapterAddress ||
           addr == GovValidatorAddress ||
           addr == GovMasterMinterAddress ||
           addr == GovMinterAddress ||
           addr == GovCouncilAddress
}
```

---

## 5. API 설계

### 5.1 GraphQL Schema Extensions

```graphql
# System Contracts Queries
extend type Query {
    # NativeCoinAdapter
    totalSupply: String!
    mintEvents(fromBlock: Long, toBlock: Long, minter: Address, limit: Int, offset: Int): [MintEvent!]!
    burnEvents(fromBlock: Long, toBlock: Long, burner: Address, limit: Int, offset: Int): [BurnEvent!]!
    activeMinters: [Address!]!
    minterAllowance(minter: Address!): String!
    minterHistory(minter: Address!): [MinterConfigEvent!]!

    # GovValidator
    activeValidators: [Address!]!
    gasTipHistory(fromBlock: Long, toBlock: Long): [GasTipUpdateEvent!]!
    validatorHistory(validator: Address!): [ValidatorChangeEvent!]!

    # Governance
    proposals(contract: Address!, status: ProposalStatus, limit: Int, offset: Int): [Proposal!]!
    proposal(contract: Address!, proposalId: String!): Proposal
    proposalVotes(contract: Address!, proposalId: String!): [ProposalVote!]!

    # GovCouncil
    blacklistedAddresses: [Address!]!
    blacklistHistory(address: Address!): [BlacklistEvent!]!
    authorizedAccounts: [Address!]!
}

# Types
type MintEvent {
    blockNumber: Long!
    txHash: Hash!
    minter: Address!
    to: Address!
    amount: String!
    timestamp: Long!
}

type BurnEvent {
    blockNumber: Long!
    txHash: Hash!
    burner: Address!
    amount: String!
    withdrawalId: String
    timestamp: Long!
}

type MinterConfigEvent {
    blockNumber: Long!
    txHash: Hash!
    minter: Address!
    allowance: String!
    action: String!
    timestamp: Long!
}

type Proposal {
    contract: Address!
    proposalId: String!
    proposer: Address!
    actionType: String!
    callData: String!
    memberVersion: String!
    requiredApprovals: Int!
    approved: Int!
    rejected: Int!
    status: ProposalStatus!
    createdAt: Long!
    executedAt: Long
    blockNumber: Long!
    txHash: Hash!
}

enum ProposalStatus {
    NONE
    VOTING
    APPROVED
    EXECUTED
    CANCELLED
    EXPIRED
    FAILED
    REJECTED
}

type ProposalVote {
    proposalId: String!
    voter: Address!
    approval: Boolean!
    blockNumber: Long!
    txHash: Hash!
    timestamp: Long!
}

type GasTipUpdateEvent {
    blockNumber: Long!
    txHash: Hash!
    oldTip: String!
    newTip: String!
    updater: Address!
    timestamp: Long!
}

type BlacklistEvent {
    blockNumber: Long!
    txHash: Hash!
    account: Address!
    action: String!
    proposalId: String!
    timestamp: Long!
}

type ValidatorChangeEvent {
    blockNumber: Long!
    txHash: Hash!
    validator: Address!
    action: String!
    oldValidator: Address
    timestamp: Long!
}
```

### 5.2 JSON-RPC Methods

```go
// NativeCoinAdapter methods
stable_getTotalSupply() -> string
stable_getMintEvents(fromBlock, toBlock, minter, limit, offset) -> []MintEvent
stable_getBurnEvents(fromBlock, toBlock, burner, limit, offset) -> []BurnEvent
stable_getActiveMinters() -> []address
stable_getMinterAllowance(minter) -> string
stable_getMinterHistory(minter) -> []MinterConfigEvent

// GovValidator methods
stable_getActiveValidators() -> []address
stable_getGasTipHistory(fromBlock, toBlock) -> []GasTipUpdateEvent
stable_getValidatorHistory(validator) -> []ValidatorChangeEvent

// Governance methods
stable_getProposals(contract, status, limit, offset) -> []Proposal
stable_getProposal(contract, proposalId) -> Proposal
stable_getProposalVotes(contract, proposalId) -> []ProposalVote

// GovCouncil methods
stable_getBlacklistedAddresses() -> []address
stable_getBlacklistHistory(address) -> []BlacklistEvent
stable_getAuthorizedAccounts() -> []address
```

---

## 6. 구현 단계

### Phase 1: Storage Layer (3-4일)
1. Storage 인터페이스 정의 (SystemContractReader/Writer)
2. 데이터 구조 정의 (MintEvent, Proposal, etc.)
3. PebbleDB 키 스키마 구현
4. Storage 메서드 구현
5. 단위 테스트

### Phase 2: Event Parsing (3-4일)
1. Event signature 정의
2. SystemContractEventParser 구현
3. 각 이벤트별 파서 구현
4. Fetcher 통합 (블록 처리 시 자동 파싱)
5. 통합 테스트

### Phase 3: API Implementation (2-3일)
1. GraphQL schema 확장
2. GraphQL resolver 구현
3. JSON-RPC methods 구현
4. API 테스트

### Phase 4: Testing & Documentation (1-2일)
1. End-to-end 테스트
2. 성능 테스트
3. 문서 업데이트
4. 사용 예제 작성

**총 예상 기간**: 9-13일

---

## 7. 성능 고려사항

### 7.1 인덱싱 성능
- 블록당 평균 10-20개의 시스템 컨트랙트 이벤트 예상
- 배치 인덱싱 지원 (IndexSystemContractEvents)
- 병렬 처리 가능 (이벤트간 의존성 없음)

### 7.2 쿼리 성능
- 활성 상태 쿼리 (activeMinters, activeValidators, blacklistedAddresses)는 인덱스로 O(1) 조회
- 히스토리 쿼리는 블록 범위 스캔 필요, 페이지네이션 필수
- 자주 사용되는 쿼리는 캐싱 고려

### 7.3 스토리지 최적화
- 압축 가능한 필드 (ProposalStatus enum → uint8)
- 중복 데이터 최소화 (txHash, blockNumber는 키로 분리 가능)
- 만료된 제안은 아카이브 고려

---

## 8. 보안 고려사항

- 시스템 컨트랙트 주소 하드코딩 (변경 불가)
- Event signature 검증 (잘못된 이벤트 무시)
- 권한 확인 없이 Read-only 제공 (모든 이벤트는 public)
- Rate limiting 적용 (API 레벨)

---

## 9. 향후 확장

### 9.1 고급 분석
- Minter별 발행량 통계
- Validator 활동 통계
- 제안 승인율 분석
- 블랙리스트 변경 빈도 분석

### 9.2 알림 시스템
- 긴급 중지 이벤트 알림
- 큰 금액 Mint/Burn 알림
- 제안 실행 실패 알림

### 9.3 대시보드
- Grafana 대시보드 (Minter 활동, Validator 상태)
- 실시간 모니터링 (EventBus 통합)

---

## 10. 참고 자료

- `go-stablenet/systemcontracts/solidity/v1/*.sol` - Solidity 컨트랙트 소스
- `go-stablenet/systemcontracts/*.go` - Go 인터페이스 및 초기화 코드
- `docs/STABLE_ONE_TECHNICAL_ANALYSIS.md` - Stable-One 체인 분석
- `docs/TODO.md` - 프로젝트 작업 계획
