# Consensus Enhancement Plan: WBFT Integration & Frontend Support

## Executive Summary

이 문서는 indexer-go 프로젝트에 WBFT(Weemix Byzantine Fault Tolerant) 합의 메커니즘의 완전한 지원을 추가하고, 프론트엔드에서 노드와 체인의 상태를 실시간으로 파악할 수 있는 구조를 만들기 위한 종합 계획입니다.

### Goals
- SOLID 원칙과 Clean Code 준수
- 확장 가능한 아키텍처 설계
- 상용 프로덕트 수준의 코드 품질
- 간결한 depth의 성능 최적화된 구조

---

## 1. Architecture Overview

### 1.1 Current State Analysis

```
┌─────────────────────────────────────────────────────────────────┐
│                     Current Architecture                        │
├─────────────────────────────────────────────────────────────────┤
│  go-stablenet (78+ RPC Methods)                                 │
│  ├── Block/Transaction APIs      ✅ Supported                   │
│  ├── istanbul_* APIs             ⚠️  Partially Supported        │
│  └── WBFT Extra Data             ❌ Not Parsed                   │
├─────────────────────────────────────────────────────────────────┤
│  indexer-go                                                      │
│  ├── Block Indexing              ✅ Working                      │
│  ├── Transaction Indexing        ✅ Working                      │
│  ├── Log Indexing                ✅ Working                      │
│  ├── Validator Tracking          ⚠️  Basic (18 TODOs)            │
│  └── Consensus Metadata          ❌ Missing                       │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 Target Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Enhanced Architecture                             │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌──────────────┐    ┌──────────────────┐    ┌───────────────────┐     │
│  │  go-stablenet │    │   indexer-go     │    │    Frontend       │     │
│  │     Node      │───▶│   (Enhanced)     │───▶│   Dashboard       │     │
│  └──────────────┘    └──────────────────┘    └───────────────────┘     │
│         │                     │                       │                  │
│         │                     ▼                       ▼                  │
│         │            ┌────────────────┐      ┌────────────────┐         │
│         │            │  Consensus     │      │  Real-time     │         │
│         │            │  Analytics     │      │  Monitoring    │         │
│         │            └────────────────┘      └────────────────┘         │
│         │                                                                │
│         ▼                                                                │
│  ┌──────────────────────────────────────────────────────────────┐       │
│  │                    Data Flow                                  │       │
│  │  Block Extra Data → Parse → Store → Query → GraphQL → UI     │       │
│  │  Validator Set    → Track → Index → Analyze → Subscribe      │       │
│  │  Round Changes    → Detect → Store → Alert → Dashboard       │       │
│  └──────────────────────────────────────────────────────────────┘       │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 2. WBFT Data Structures

### 2.1 go-stablenet Block Extra Data Structure

```go
// From go-stablenet/core/types/istanbul.go
type WBFTExtra struct {
    VanityData        []byte              // 32 bytes vanity
    RandaoReveal      []byte              // BLS signature
    PrevRound         uint32              // Previous block's round
    PrevPreparedSeal  *WBFTAggregatedSeal // Previous prepare seal
    PrevCommittedSeal *WBFTAggregatedSeal // Previous commit seal
    Round             uint32              // Current round (0 = success on first try)
    PreparedSeal      *WBFTAggregatedSeal // Current prepare seal
    CommittedSeal     *WBFTAggregatedSeal // Current commit seal
    GasTip            *big.Int            // Governance gas tip
    EpochInfo         *EpochInfo          // Validator changes (epoch boundary only)
}

type WBFTAggregatedSeal struct {
    Sealers   SealerSet  // Bitmap of participating validators
    Signature []byte     // BLS aggregated signature
}

type EpochInfo struct {
    Candidates    []*Candidate  // All candidate validators
    Validators    []uint32      // Active validator indices
    BLSPublicKeys [][]byte      // BLS public keys
}
```

### 2.2 Available RPC Methods for Consensus

| Method | Description | Return Type |
|--------|-------------|-------------|
| `istanbul_getValidators` | Get validators at block | `[]Address` |
| `istanbul_getValidatorsAtHash` | Get validators by hash | `[]Address` |
| `istanbul_getCommitSignersFromBlock` | Get block signers | `{Author, Committers[]}` |
| `istanbul_status` | Validator activity stats | `{SealerActivity, AuthorCounts, RoundStats}` |
| `istanbul_nodeAddress` | Current node address | `Address` |

---

## 3. Implementation Phases

### Phase 1: Core Data Structures & Interfaces (Week 1)

**Objective**: Clean, extensible interfaces following SOLID principles

#### 3.1.1 New Types Package Structure

```
types/
├── consensus/
│   ├── wbft.go          # WBFT-specific types
│   ├── validator.go     # Validator types
│   ├── round.go         # Round change types
│   └── seal.go          # Seal types
├── block_extended.go    # Extended block with consensus data
└── interfaces.go        # Core interfaces
```

#### 3.1.2 Key Interfaces (Interface Segregation Principle)

```go
// types/consensus/interfaces.go

// ConsensusDataProvider - Single responsibility for consensus data extraction
type ConsensusDataProvider interface {
    ExtractConsensusData(header *types.Header) (*ConsensusData, error)
}

// ValidatorTracker - Validator participation tracking
type ValidatorTracker interface {
    GetValidatorsAtBlock(blockNum uint64) ([]common.Address, error)
    GetValidatorParticipation(startBlock, endBlock uint64) (*ValidatorStats, error)
}

// RoundAnalyzer - Round change analysis
type RoundAnalyzer interface {
    GetRoundInfo(blockNum uint64) (*RoundInfo, error)
    AnalyzeRoundChanges(startBlock, endBlock uint64) (*RoundAnalysis, error)
}

// SealVerifier - Seal verification (optional, for security)
type SealVerifier interface {
    VerifySeal(header *types.Header, seal *AggregatedSeal) error
}
```

#### 3.1.3 Consensus Data Models

```go
// types/consensus/wbft.go

// ConsensusData - Parsed consensus information from block
type ConsensusData struct {
    BlockNumber     uint64            `json:"blockNumber"`
    BlockHash       common.Hash       `json:"blockHash"`

    // Round Information
    Round           uint32            `json:"round"`
    PrevRound       uint32            `json:"prevRound"`
    RoundChanged    bool              `json:"roundChanged"` // Round > 0 means round change occurred

    // Validator Participation
    Proposer        common.Address    `json:"proposer"`
    Validators      []common.Address  `json:"validators"`
    PrepareSigners  []common.Address  `json:"prepareSigners"`
    CommitSigners   []common.Address  `json:"commitSigners"`

    // Participation Metrics
    PrepareCount    int               `json:"prepareCount"`
    CommitCount     int               `json:"commitCount"`
    MissedPrepare   []common.Address  `json:"missedPrepare,omitempty"`
    MissedCommit    []common.Address  `json:"missedCommit,omitempty"`

    // Raw Data
    VanityData      []byte            `json:"vanityData,omitempty"`
    RandaoReveal    []byte            `json:"randaoReveal,omitempty"`
    GasTip          *big.Int          `json:"gasTip,omitempty"`

    // Epoch Information (only on epoch boundary)
    EpochInfo       *EpochData        `json:"epochInfo,omitempty"`
    IsEpochBoundary bool              `json:"isEpochBoundary"`

    // Timestamps
    Timestamp       uint64            `json:"timestamp"`
    ParsedAt        time.Time         `json:"parsedAt"`
}

// RoundInfo - Detailed round information
type RoundInfo struct {
    BlockNumber       uint64   `json:"blockNumber"`
    FinalRound        uint32   `json:"finalRound"`
    TotalRoundChanges uint32   `json:"totalRoundChanges"`
    SuccessOnFirstTry bool     `json:"successOnFirstTry"`
    ConsensusTime     uint64   `json:"consensusTimeMs,omitempty"` // If measurable
}

// ValidatorStats - Aggregated validator statistics
type ValidatorStats struct {
    Address           common.Address `json:"address"`
    TotalBlocks       uint64         `json:"totalBlocks"`
    BlocksProposed    uint64         `json:"blocksProposed"`
    PreparesSigned    uint64         `json:"preparesSigned"`
    CommitsSigned     uint64         `json:"commitsSigned"`
    PreparessMissed   uint64         `json:"preparesMissed"`
    CommitsMissed     uint64         `json:"commitsMissed"`
    ParticipationRate float64        `json:"participationRate"` // 0-100%
}

// EpochData - Epoch boundary information
type EpochData struct {
    EpochNumber       uint64            `json:"epochNumber"`
    ValidatorCount    int               `json:"validatorCount"`
    Validators        []ValidatorInfo   `json:"validators"`
    CandidateCount    int               `json:"candidateCount"`
    Candidates        []CandidateInfo   `json:"candidates,omitempty"`
}

type ValidatorInfo struct {
    Address   common.Address `json:"address"`
    Index     uint32         `json:"index"`
    BLSPubKey []byte         `json:"blsPubKey,omitempty"`
}

type CandidateInfo struct {
    Address   common.Address `json:"address"`
    Diligence uint64         `json:"diligence"` // 0 - 2,000,000
}
```

---

### Phase 2: RPC Client Extensions (Week 1-2)

**Objective**: Add missing consensus RPC methods

#### 3.2.1 Client Interface Extension

```go
// client/consensus.go

// ConsensusClient - Consensus-specific RPC methods
type ConsensusClient interface {
    // Validator queries
    GetValidators(ctx context.Context, blockNum uint64) ([]common.Address, error)
    GetValidatorsAtHash(ctx context.Context, hash common.Hash) ([]common.Address, error)

    // Signer queries
    GetCommitSigners(ctx context.Context, blockNum uint64) (*SignerInfo, error)
    GetCommitSignersByHash(ctx context.Context, hash common.Hash) (*SignerInfo, error)

    // Status queries
    GetValidatorStatus(ctx context.Context, startBlock, endBlock uint64) (*ValidatorStatusResponse, error)
    GetNodeAddress(ctx context.Context) (common.Address, error)
}

// SignerInfo - Block signer information
type SignerInfo struct {
    Number     uint64           `json:"number"`
    Hash       common.Hash      `json:"hash"`
    Author     common.Address   `json:"author"`
    Committers []common.Address `json:"committers"`
}

// ValidatorStatusResponse - istanbul_status response
type ValidatorStatusResponse struct {
    SealerActivity map[string]SealerActivity `json:"sealerActivity"`
    AuthorCounts   map[string]uint64         `json:"authorCounts"`
    BlockRange     BlockRange                `json:"blockRange"`
    RoundStats     RoundStats                `json:"roundStats"`
}

type SealerActivity struct {
    Total         uint64 `json:"total"`
    Prepared      uint64 `json:"prepared"`
    Committed     uint64 `json:"committed"`
    PrevPrepared  uint64 `json:"prevPrepared"`
    PrevCommitted uint64 `json:"prevCommitted"`
}
```

#### 3.2.2 Implementation

```go
// client/consensus_impl.go

func (c *Client) GetValidators(ctx context.Context, blockNum uint64) ([]common.Address, error) {
    var result []common.Address
    err := c.rpc.CallContext(ctx, &result, "istanbul_getValidators", hexutil.Uint64(blockNum))
    if err != nil {
        return nil, fmt.Errorf("failed to get validators at block %d: %w", blockNum, err)
    }
    return result, nil
}

func (c *Client) GetCommitSigners(ctx context.Context, blockNum uint64) (*SignerInfo, error) {
    var result SignerInfo
    err := c.rpc.CallContext(ctx, &result, "istanbul_getCommitSignersFromBlock", hexutil.Uint64(blockNum))
    if err != nil {
        return nil, fmt.Errorf("failed to get commit signers at block %d: %w", blockNum, err)
    }
    return &result, nil
}

func (c *Client) GetValidatorStatus(ctx context.Context, startBlock, endBlock uint64) (*ValidatorStatusResponse, error) {
    var result ValidatorStatusResponse
    err := c.rpc.CallContext(ctx, &result, "istanbul_status",
        hexutil.Uint64(startBlock), hexutil.Uint64(endBlock))
    if err != nil {
        return nil, fmt.Errorf("failed to get validator status: %w", err)
    }
    return &result, nil
}
```

---

### Phase 3: Extra Data Parser (Week 2)

**Objective**: Parse WBFT extra data from block headers

#### 3.3.1 Parser Implementation

```go
// consensus/parser/wbft_parser.go

package parser

import (
    "github.com/ethereum/go-ethereum/rlp"
    "github.com/ethereum/go-ethereum/core/types"
)

// WBFTParser - Parses WBFT consensus data from block headers
type WBFTParser struct {
    logger *zap.Logger
}

// NewWBFTParser creates a new WBFT parser
func NewWBFTParser(logger *zap.Logger) *WBFTParser {
    return &WBFTParser{logger: logger}
}

// ParseExtraData extracts WBFT consensus data from block header
func (p *WBFTParser) ParseExtraData(header *types.Header) (*consensus.ConsensusData, error) {
    if len(header.Extra) < IstanbulExtraVanity {
        return nil, ErrInvalidExtraDataLength
    }

    extra, err := p.decodeExtra(header.Extra)
    if err != nil {
        return nil, fmt.Errorf("failed to decode extra data: %w", err)
    }

    return p.buildConsensusData(header, extra)
}

// decodeExtra decodes RLP-encoded WBFT extra data
func (p *WBFTParser) decodeExtra(data []byte) (*WBFTExtraRaw, error) {
    var extra WBFTExtraRaw

    // Skip 32-byte vanity
    if len(data) <= IstanbulExtraVanity {
        return nil, ErrInvalidExtraDataLength
    }

    err := rlp.DecodeBytes(data[IstanbulExtraVanity:], &extra)
    if err != nil {
        return nil, fmt.Errorf("RLP decode failed: %w", err)
    }

    return &extra, nil
}

// buildConsensusData constructs ConsensusData from parsed extra
func (p *WBFTParser) buildConsensusData(header *types.Header, extra *WBFTExtraRaw) (*consensus.ConsensusData, error) {
    data := &consensus.ConsensusData{
        BlockNumber:     header.Number.Uint64(),
        BlockHash:       header.Hash(),
        Round:           extra.Round,
        PrevRound:       extra.PrevRound,
        RoundChanged:    extra.Round > 0,
        Proposer:        header.Coinbase,
        VanityData:      header.Extra[:IstanbulExtraVanity],
        RandaoReveal:    extra.RandaoReveal,
        GasTip:          extra.GasTip,
        Timestamp:       header.Time,
        ParsedAt:        time.Now(),
    }

    // Extract commit signers from seal bitmap
    if extra.CommittedSeal != nil {
        data.CommitSigners = p.extractSignersFromBitmap(extra.CommittedSeal.Sealers)
        data.CommitCount = len(data.CommitSigners)
    }

    // Extract prepare signers
    if extra.PreparedSeal != nil {
        data.PrepareSigners = p.extractSignersFromBitmap(extra.PreparedSeal.Sealers)
        data.PrepareCount = len(data.PrepareSigners)
    }

    // Parse epoch info if present
    if extra.EpochInfo != nil {
        data.IsEpochBoundary = true
        data.EpochInfo = p.parseEpochInfo(extra.EpochInfo)
    }

    return data, nil
}

// extractSignersFromBitmap converts bitmap to list of signer indices
func (p *WBFTParser) extractSignersFromBitmap(bitmap []byte) []uint32 {
    var signers []uint32
    for byteIdx, b := range bitmap {
        for bitIdx := 0; bitIdx < 8; bitIdx++ {
            if b&(1<<bitIdx) != 0 {
                signers = append(signers, uint32(byteIdx*8+bitIdx))
            }
        }
    }
    return signers
}
```

---

### Phase 4: Storage Layer Enhancement (Week 2-3)

**Objective**: Store and index consensus data efficiently

#### 3.4.1 Storage Schema Extension

```
New Key Patterns:
────────────────────────────────────────────────────────────────────
/consensus/block/{height}           → RLP-encoded ConsensusData
/consensus/round/{height}           → RoundInfo
/consensus/epoch/{epochNum}         → EpochData
/index/validator/{address}/{height} → Participation record
/index/proposer/{address}/{height}  → Block hash (blocks proposed)
/index/round-change/{height}        → Round change indicator
/stats/validator/{address}          → Aggregated ValidatorStats
────────────────────────────────────────────────────────────────────
```

#### 3.4.2 Storage Interface Extension

```go
// storage/consensus.go

// ConsensusReader - Read consensus data (Interface Segregation)
type ConsensusReader interface {
    // Block consensus data
    GetConsensusData(blockNum uint64) (*consensus.ConsensusData, error)
    GetConsensusDataByHash(hash common.Hash) (*consensus.ConsensusData, error)
    GetConsensusDataRange(start, end uint64) ([]*consensus.ConsensusData, error)

    // Round information
    GetRoundInfo(blockNum uint64) (*consensus.RoundInfo, error)
    GetRoundChanges(start, end uint64) ([]*consensus.RoundInfo, error)

    // Epoch information
    GetEpochInfo(epochNum uint64) (*consensus.EpochData, error)
    GetCurrentEpoch() (*consensus.EpochData, error)

    // Validator queries
    GetValidatorStats(address common.Address) (*consensus.ValidatorStats, error)
    GetValidatorStatsRange(address common.Address, start, end uint64) (*consensus.ValidatorStats, error)
    GetAllValidatorStats(start, end uint64) ([]*consensus.ValidatorStats, error)

    // Participation queries
    GetBlocksProposedBy(address common.Address, limit, offset uint64) ([]uint64, error)
    GetMissedBlocks(address common.Address, start, end uint64) ([]uint64, error)
}

// ConsensusWriter - Write consensus data
type ConsensusWriter interface {
    StoreConsensusData(data *consensus.ConsensusData) error
    StoreConsensusDataBatch(data []*consensus.ConsensusData) error
    StoreEpochInfo(epoch *consensus.EpochData) error
    UpdateValidatorStats(stats *consensus.ValidatorStats) error
}
```

#### 3.4.3 Implementation with Batch Operations

```go
// storage/pebble_consensus.go

func (p *PebbleStorage) StoreConsensusData(data *consensus.ConsensusData) error {
    batch := p.db.NewBatch()
    defer batch.Close()

    // Store main consensus data
    key := p.consensusBlockKey(data.BlockNumber)
    encoded, err := rlp.EncodeToBytes(data)
    if err != nil {
        return fmt.Errorf("failed to encode consensus data: %w", err)
    }
    batch.Set(key, encoded, pebble.Sync)

    // Store round info if round change occurred
    if data.RoundChanged {
        roundKey := p.roundChangeKey(data.BlockNumber)
        roundInfo := &consensus.RoundInfo{
            BlockNumber:       data.BlockNumber,
            FinalRound:        data.Round,
            TotalRoundChanges: data.Round,
            SuccessOnFirstTry: false,
        }
        roundEncoded, _ := rlp.EncodeToBytes(roundInfo)
        batch.Set(roundKey, roundEncoded, pebble.Sync)
    }

    // Index validator participation
    for _, signer := range data.CommitSigners {
        indexKey := p.validatorParticipationKey(signer, data.BlockNumber)
        batch.Set(indexKey, []byte{1}, pebble.Sync) // 1 = committed
    }

    // Index proposer
    proposerKey := p.proposerIndexKey(data.Proposer, data.BlockNumber)
    batch.Set(proposerKey, data.BlockHash.Bytes(), pebble.Sync)

    return batch.Commit(pebble.Sync)
}
```

---

### Phase 5: GraphQL API Extension (Week 3-4)

**Objective**: Expose consensus data via GraphQL

#### 3.5.1 Schema Extension

```graphql
# schema/consensus.graphql

extend type Query {
    # Consensus data queries
    consensusData(blockNumber: Long!): ConsensusData
    consensusDataByHash(hash: Hash!): ConsensusData
    consensusDataRange(start: Long!, end: Long!): [ConsensusData!]!

    # Validator queries
    validators(blockNumber: Long): [Validator!]!
    validator(address: Address!): ValidatorStats
    validatorParticipation(
        address: Address!
        startBlock: Long
        endBlock: Long
    ): ValidatorParticipation!

    # Round queries
    roundInfo(blockNumber: Long!): RoundInfo
    roundChanges(startBlock: Long!, endBlock: Long!): [RoundChange!]!
    roundChangeStats(startBlock: Long!, endBlock: Long!): RoundChangeStats!

    # Epoch queries
    currentEpoch: EpochInfo
    epoch(number: Long!): EpochInfo
    epochHistory(limit: Int, offset: Int): [EpochInfo!]!
}

extend type Subscription {
    # Real-time consensus events
    newConsensusData: ConsensusData!
    validatorParticipationUpdate: ValidatorParticipationEvent!
    roundChangeOccurred: RoundChangeEvent!
    epochChanged: EpochChangeEvent!
}

# Types
type ConsensusData {
    blockNumber: Long!
    blockHash: Hash!

    # Round information
    round: Int!
    prevRound: Int!
    roundChanged: Boolean!

    # Validator information
    proposer: Address!
    validators: [Address!]!
    prepareSigners: [Address!]!
    commitSigners: [Address!]!

    # Participation metrics
    prepareCount: Int!
    commitCount: Int!
    missedPrepare: [Address!]
    missedCommit: [Address!]
    participationRate: Float!

    # Extra data
    vanityData: Bytes
    gasTip: BigInt

    # Epoch info (if epoch boundary)
    isEpochBoundary: Boolean!
    epochInfo: EpochInfo

    timestamp: Long!
}

type ValidatorStats {
    address: Address!
    totalBlocks: Long!
    blocksProposed: Long!
    preparesSigned: Long!
    commitsSigned: Long!
    preparesMissed: Long!
    commitsMissed: Long!
    participationRate: Float!

    # Recent activity
    recentBlocks: [ConsensusData!]!
    lastProposedBlock: Long
    lastCommittedBlock: Long
}

type ValidatorParticipation {
    address: Address!
    startBlock: Long!
    endBlock: Long!

    # Aggregated stats
    totalBlocks: Long!
    blocksProposed: Long!
    blocksCommitted: Long!
    blocksMissed: Long!
    participationRate: Float!

    # Per-block breakdown
    blocks: [BlockParticipation!]!
}

type BlockParticipation {
    blockNumber: Long!
    wasProposer: Boolean!
    signedPrepare: Boolean!
    signedCommit: Boolean!
    round: Int!
}

type RoundInfo {
    blockNumber: Long!
    finalRound: Int!
    totalRoundChanges: Int!
    successOnFirstTry: Boolean!
}

type RoundChangeStats {
    totalBlocks: Long!
    blocksWithRoundChange: Long!
    roundChangeRate: Float!
    averageRound: Float!
    maxRound: Int!
    roundDistribution: [RoundDistribution!]!
}

type RoundDistribution {
    round: Int!
    count: Long!
    percentage: Float!
}

type EpochInfo {
    epochNumber: Long!
    startBlock: Long!
    endBlock: Long!
    validatorCount: Int!
    validators: [ValidatorInfo!]!
    candidateCount: Int!
    candidates: [CandidateInfo!]
}

type ValidatorInfo {
    address: Address!
    index: Int!
    blsPubKey: Bytes
}

type CandidateInfo {
    address: Address!
    diligence: Long!
    diligencePercentage: Float! # 0-100%
}

# Subscription Events
type ValidatorParticipationEvent {
    blockNumber: Long!
    validator: Address!
    wasProposer: Boolean!
    signedPrepare: Boolean!
    signedCommit: Boolean!
    participationRate: Float!
}

type RoundChangeEvent {
    blockNumber: Long!
    round: Int!
    previousRound: Int!
    proposer: Address!
}

type EpochChangeEvent {
    epochNumber: Long!
    blockNumber: Long!
    previousValidators: [Address!]!
    newValidators: [Address!]!
    addedValidators: [Address!]!
    removedValidators: [Address!]!
}
```

#### 3.5.2 Resolver Implementation

```go
// api/graphql/resolvers/consensus.go

type ConsensusResolver struct {
    storage storage.ConsensusReader
    client  client.ConsensusClient
    logger  *zap.Logger
}

func (r *ConsensusResolver) ConsensusData(ctx context.Context, args struct{ BlockNumber uint64 }) (*ConsensusDataResolver, error) {
    data, err := r.storage.GetConsensusData(args.BlockNumber)
    if err != nil {
        return nil, err
    }
    return &ConsensusDataResolver{data: data}, nil
}

func (r *ConsensusResolver) ValidatorParticipation(
    ctx context.Context,
    args struct {
        Address    common.Address
        StartBlock *uint64
        EndBlock   *uint64
    },
) (*ValidatorParticipationResolver, error) {
    start := uint64(0)
    end := uint64(0)

    if args.StartBlock != nil {
        start = *args.StartBlock
    }
    if args.EndBlock != nil {
        end = *args.EndBlock
    } else {
        // Default to latest 1000 blocks
        latest, _ := r.storage.GetLatestBlockNumber()
        end = latest
        if start == 0 && latest > 1000 {
            start = latest - 1000
        }
    }

    stats, err := r.storage.GetValidatorStatsRange(args.Address, start, end)
    if err != nil {
        return nil, err
    }

    return &ValidatorParticipationResolver{
        address: args.Address,
        start:   start,
        end:     end,
        stats:   stats,
    }, nil
}

func (r *ConsensusResolver) RoundChangeStats(
    ctx context.Context,
    args struct{ StartBlock, EndBlock uint64 },
) (*RoundChangeStatsResolver, error) {
    changes, err := r.storage.GetRoundChanges(args.StartBlock, args.EndBlock)
    if err != nil {
        return nil, err
    }

    // Calculate statistics
    stats := calculateRoundStats(changes, args.StartBlock, args.EndBlock)
    return &RoundChangeStatsResolver{stats: stats}, nil
}
```

---

### Phase 6: Event System Integration (Week 4)

**Objective**: Real-time consensus event notifications

#### 3.6.1 New Event Types

```go
// events/consensus_events.go

const (
    EventTypeConsensusData      EventType = "consensus_data"
    EventTypeValidatorUpdate    EventType = "validator_update"
    EventTypeRoundChange        EventType = "round_change"
    EventTypeEpochChange        EventType = "epoch_change"
)

// ConsensusDataEvent - Emitted for each new block with consensus data
type ConsensusDataEvent struct {
    BaseEvent
    Data *consensus.ConsensusData
}

// RoundChangeEvent - Emitted when round > 0
type RoundChangeEvent struct {
    BaseEvent
    BlockNumber   uint64
    Round         uint32
    PreviousRound uint32
    Proposer      common.Address
}

// EpochChangeEvent - Emitted at epoch boundaries
type EpochChangeEvent struct {
    BaseEvent
    EpochNumber        uint64
    BlockNumber        uint64
    PreviousValidators []common.Address
    NewValidators      []common.Address
    AddedValidators    []common.Address
    RemovedValidators  []common.Address
}
```

---

## 4. Frontend Integration Guide

### 4.1 GraphQL Client Setup

```typescript
// frontend/lib/graphql/client.ts
import { createClient, subscriptionExchange } from '@urql/core';
import { createClient as createWSClient } from 'graphql-ws';

const wsClient = createWSClient({
  url: 'ws://localhost:8080/graphql/ws',
});

export const client = createClient({
  url: 'http://localhost:8080/graphql',
  exchanges: [
    subscriptionExchange({
      forwardSubscription: (operation) => ({
        subscribe: (sink) => ({
          unsubscribe: wsClient.subscribe(operation, sink),
        }),
      }),
    }),
  ],
});
```

### 4.2 Key Queries

```graphql
# Get validator participation overview
query ValidatorOverview($startBlock: Long!, $endBlock: Long!) {
  validators(blockNumber: $endBlock) {
    address
    index
  }

  consensusDataRange(start: $startBlock, end: $endBlock) {
    blockNumber
    round
    roundChanged
    proposer
    commitSigners
    participationRate
  }
}

# Get specific validator stats
query ValidatorDetails($address: Address!, $start: Long!, $end: Long!) {
  validator(address: $address) {
    totalBlocks
    blocksProposed
    participationRate
    recentBlocks {
      blockNumber
      round
      isProposer
    }
  }

  validatorParticipation(address: $address, startBlock: $start, endBlock: $end) {
    blocks {
      blockNumber
      wasProposer
      signedPrepare
      signedCommit
      round
    }
  }
}

# Round change analysis
query RoundChangeAnalysis($start: Long!, $end: Long!) {
  roundChangeStats(startBlock: $start, endBlock: $end) {
    totalBlocks
    blocksWithRoundChange
    roundChangeRate
    averageRound
    maxRound
    roundDistribution {
      round
      count
      percentage
    }
  }

  roundChanges(startBlock: $start, endBlock: $end) {
    blockNumber
    finalRound
    totalRoundChanges
  }
}
```

### 4.3 Real-time Subscriptions

```graphql
# Subscribe to new consensus data
subscription OnNewConsensusData {
  newConsensusData {
    blockNumber
    round
    roundChanged
    proposer
    commitSigners
    missedCommit
    participationRate
    timestamp
  }
}

# Subscribe to round changes
subscription OnRoundChange {
  roundChangeOccurred {
    blockNumber
    round
    previousRound
    proposer
  }
}

# Subscribe to epoch changes
subscription OnEpochChange {
  epochChanged {
    epochNumber
    blockNumber
    newValidators
    addedValidators
    removedValidators
  }
}
```

---

## 5. Performance Considerations

### 5.1 Query Optimization

```go
// Use batch queries for range operations
func (p *PebbleStorage) GetConsensusDataRange(start, end uint64) ([]*consensus.ConsensusData, error) {
    // Pre-allocate result slice
    result := make([]*consensus.ConsensusData, 0, end-start+1)

    // Use iterator for efficient range scan
    iter, err := p.db.NewIter(&pebble.IterOptions{
        LowerBound: p.consensusBlockKey(start),
        UpperBound: p.consensusBlockKey(end + 1),
    })
    if err != nil {
        return nil, err
    }
    defer iter.Close()

    for iter.First(); iter.Valid(); iter.Next() {
        var data consensus.ConsensusData
        if err := rlp.DecodeBytes(iter.Value(), &data); err != nil {
            continue
        }
        result = append(result, &data)
    }

    return result, nil
}
```

### 5.2 Caching Strategy

```go
// Cache validator stats for frequently accessed data
type ValidatorStatsCache struct {
    cache   *lru.Cache[common.Address, *consensus.ValidatorStats]
    storage ConsensusReader
    ttl     time.Duration
}

func (c *ValidatorStatsCache) Get(address common.Address) (*consensus.ValidatorStats, error) {
    if stats, ok := c.cache.Get(address); ok {
        return stats, nil
    }

    stats, err := c.storage.GetValidatorStats(address)
    if err != nil {
        return nil, err
    }

    c.cache.Add(address, stats)
    return stats, nil
}
```

---

## 6. Testing Strategy

### 6.1 Unit Tests

```go
// consensus/parser/wbft_parser_test.go

func TestParseExtraData(t *testing.T) {
    tests := []struct {
        name     string
        extra    []byte
        expected *consensus.ConsensusData
        wantErr  bool
    }{
        {
            name:  "valid extra data with round 0",
            extra: validExtraRound0,
            expected: &consensus.ConsensusData{
                Round:        0,
                RoundChanged: false,
            },
        },
        {
            name:  "valid extra data with round change",
            extra: validExtraRound2,
            expected: &consensus.ConsensusData{
                Round:        2,
                RoundChanged: true,
            },
        },
        {
            name:    "invalid extra data length",
            extra:   make([]byte, 10),
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            parser := NewWBFTParser(zap.NewNop())
            header := &types.Header{Extra: tt.extra}

            got, err := parser.ParseExtraData(header)

            if tt.wantErr {
                assert.Error(t, err)
                return
            }

            assert.NoError(t, err)
            assert.Equal(t, tt.expected.Round, got.Round)
            assert.Equal(t, tt.expected.RoundChanged, got.RoundChanged)
        })
    }
}
```

### 6.2 Integration Tests

```go
// storage/consensus_integration_test.go

func TestConsensusDataRoundTrip(t *testing.T) {
    storage := setupTestStorage(t)
    defer storage.Close()

    // Create test data
    data := &consensus.ConsensusData{
        BlockNumber:  100,
        Round:        2,
        RoundChanged: true,
        Proposer:     common.HexToAddress("0x1234"),
        CommitSigners: []common.Address{
            common.HexToAddress("0x1234"),
            common.HexToAddress("0x5678"),
        },
    }

    // Store
    err := storage.StoreConsensusData(data)
    require.NoError(t, err)

    // Retrieve
    retrieved, err := storage.GetConsensusData(100)
    require.NoError(t, err)

    // Verify
    assert.Equal(t, data.BlockNumber, retrieved.BlockNumber)
    assert.Equal(t, data.Round, retrieved.Round)
    assert.Equal(t, data.RoundChanged, retrieved.RoundChanged)
    assert.Equal(t, len(data.CommitSigners), len(retrieved.CommitSigners))
}
```

---

## 7. Migration Plan

### 7.1 Database Migration

```go
// storage/migrations/001_add_consensus_data.go

func MigrateAddConsensusData(db *pebble.DB, client client.ConsensusClient) error {
    // Get current indexed range
    latestHeight, err := getLatestHeight(db)
    if err != nil {
        return err
    }

    // Process in batches
    batchSize := uint64(1000)
    for start := uint64(0); start <= latestHeight; start += batchSize {
        end := start + batchSize
        if end > latestHeight {
            end = latestHeight
        }

        if err := migrateBlockRange(db, client, start, end); err != nil {
            return fmt.Errorf("failed to migrate blocks %d-%d: %w", start, end, err)
        }

        log.Printf("Migrated blocks %d-%d", start, end)
    }

    return nil
}
```

### 7.2 Backward Compatibility

- 기존 API 엔드포인트 유지
- 새로운 consensus 엔드포인트 추가
- Feature flag로 새 기능 활성화 제어

---

## 8. Deliverables Checklist

### Phase 1: Core Infrastructure
- [ ] types/consensus/wbft.go - WBFT data structures
- [ ] types/consensus/validator.go - Validator types
- [ ] types/consensus/interfaces.go - Core interfaces
- [ ] Unit tests for all types

### Phase 2: RPC Client
- [ ] client/consensus.go - Consensus client interface
- [ ] client/consensus_impl.go - Implementation
- [ ] Integration tests

### Phase 3: Parser
- [ ] consensus/parser/wbft_parser.go - Extra data parser
- [ ] consensus/parser/wbft_parser_test.go - Parser tests
- [ ] Benchmark tests

### Phase 4: Storage
- [ ] storage/consensus.go - Consensus storage interface
- [ ] storage/pebble_consensus.go - PebbleDB implementation
- [ ] storage/schema.go updates - New key patterns
- [ ] Migration script

### Phase 5: GraphQL
- [ ] api/graphql/schema/consensus.graphql - Schema
- [ ] api/graphql/resolvers/consensus.go - Resolvers
- [ ] api/graphql/resolvers/consensus_test.go - Tests

### Phase 6: Events & Subscriptions
- [ ] events/consensus_events.go - Event types
- [ ] WebSocket subscription handlers
- [ ] Integration tests

### Documentation
- [ ] API Documentation
- [ ] Frontend Integration Guide
- [ ] UI/UX Design Guide (separate document)

---

## 9. Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Extra data format changes | High | Version detection, fallback parsing |
| Performance degradation | Medium | Batch operations, caching, indexes |
| Storage size increase | Medium | Data pruning, compression |
| Breaking changes | High | Feature flags, gradual rollout |
| RPC method unavailability | Medium | Graceful degradation, fallback |

---

## 10. Success Metrics

- **Coverage**: 100% of WBFT extra data fields parsed
- **Performance**: <50ms for single block consensus query
- **Performance**: <500ms for 1000 block range query
- **Reliability**: 99.9% successful parse rate
- **Test Coverage**: >80% for new code

---

*Document Version: 1.0*
*Created: 2025-11-25*
*Author: Claude Code Assistant*
