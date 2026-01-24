package consensus

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ConsensusData represents parsed consensus information from a WBFT block
// This is the primary data structure that aggregates all consensus-related information
type ConsensusData struct {
	// Block identification
	BlockNumber uint64      `json:"blockNumber"`
	BlockHash   common.Hash `json:"blockHash"`

	// Round information
	// Round = 0 means consensus succeeded on first try (optimal)
	// Round > 0 means round changes occurred (consensus delay)
	Round        uint32 `json:"round"`
	PrevRound    uint32 `json:"prevRound"`
	RoundChanged bool   `json:"roundChanged"` // Derived: Round > 0

	// Validator participation
	Proposer       common.Address   `json:"proposer"`       // Block proposer (from header.Coinbase)
	Validators     []common.Address `json:"validators"`     // Active validator set for this block
	PrepareSigners []common.Address `json:"prepareSigners"` // Validators who signed Prepare
	CommitSigners  []common.Address `json:"commitSigners"`  // Validators who signed Commit

	// Participation metrics
	PrepareCount  int              `json:"prepareCount"`
	CommitCount   int              `json:"commitCount"`
	MissedPrepare []common.Address `json:"missedPrepare,omitempty"` // Validators - PrepareSigners
	MissedCommit  []common.Address `json:"missedCommit,omitempty"`  // Validators - CommitSigners

	// Raw consensus data
	VanityData   []byte   `json:"vanityData,omitempty"`   // 32 bytes vanity data
	RandaoReveal []byte   `json:"randaoReveal,omitempty"` // BLS signature for randomness
	GasTip       *big.Int `json:"gasTip,omitempty"`       // Governance gas tip

	// Epoch information (only present at epoch boundaries)
	EpochInfo       *EpochData `json:"epochInfo,omitempty"`
	IsEpochBoundary bool       `json:"isEpochBoundary"`

	// Metadata
	Timestamp uint64    `json:"timestamp"` // Block timestamp
	ParsedAt  time.Time `json:"parsedAt"`  // When this data was parsed
}

// RoundInfo provides detailed information about consensus rounds
type RoundInfo struct {
	BlockNumber       uint64 `json:"blockNumber"`
	FinalRound        uint32 `json:"finalRound"`                // The round that achieved consensus
	TotalRoundChanges uint32 `json:"totalRoundChanges"`         // Number of round changes (FinalRound)
	SuccessOnFirstTry bool   `json:"successOnFirstTry"`         // True if FinalRound == 0
	ConsensusTimeMs   uint64 `json:"consensusTimeMs,omitempty"` // Time to consensus (if measurable)
}

// EpochData contains validator set information at epoch boundaries
type EpochData struct {
	EpochNumber    uint64          `json:"epochNumber"`
	ValidatorCount int             `json:"validatorCount"`
	Validators     []ValidatorInfo `json:"validators"`
	CandidateCount int             `json:"candidateCount"`
	Candidates     []CandidateInfo `json:"candidates,omitempty"`
}

// ValidatorInfo represents a single validator in the epoch
type ValidatorInfo struct {
	Address   common.Address `json:"address"`
	Index     uint32         `json:"index"`               // Validator index in the set
	BLSPubKey []byte         `json:"blsPubKey,omitempty"` // BLS public key
}

// CandidateInfo represents a candidate validator
type CandidateInfo struct {
	Address   common.Address `json:"address"`
	Diligence uint64         `json:"diligence"` // Diligence score (0 - 2,000,000)
}

// WBFTAggregatedSeal represents an aggregated BLS signature seal
// This corresponds to go-stablenet's WBFTAggregatedSeal structure
type WBFTAggregatedSeal struct {
	Sealers   []byte `json:"sealers"`   // Bitmap of participating validators
	Signature []byte `json:"signature"` // BLS aggregated signature
}

// WBFTExtra represents the complete WBFT extra data structure
// This corresponds to go-stablenet's WBFTExtra structure
type WBFTExtra struct {
	VanityData        []byte              `json:"vanityData"`        // 32 bytes vanity
	RandaoReveal      []byte              `json:"randaoReveal"`      // BLS signature
	PrevRound         uint32              `json:"prevRound"`         // Previous block's round
	PrevPreparedSeal  *WBFTAggregatedSeal `json:"prevPreparedSeal"`  // Previous prepare seal
	PrevCommittedSeal *WBFTAggregatedSeal `json:"prevCommittedSeal"` // Previous commit seal
	Round             uint32              `json:"round"`             // Current round
	PreparedSeal      *WBFTAggregatedSeal `json:"preparedSeal"`      // Current prepare seal
	CommittedSeal     *WBFTAggregatedSeal `json:"committedSeal"`     // Current commit seal
	GasTip            *big.Int            `json:"gasTip"`            // Governance gas tip
	EpochInfo         *EpochInfoRaw       `json:"epochInfo"`         // Epoch boundary info
}

// EpochInfoRaw represents raw epoch information from block extra data
type EpochInfoRaw struct {
	Candidates    []*CandidateRaw `json:"candidates"`    // All candidate validators
	Validators    []uint32        `json:"validators"`    // Active validator indices
	BLSPublicKeys [][]byte        `json:"blsPublicKeys"` // BLS public keys
}

// CandidateRaw represents a raw candidate from epoch info
type CandidateRaw struct {
	Address   common.Address `json:"address"`
	Diligence uint64         `json:"diligence"`
}

// RoundAnalysis provides statistical analysis of round changes over a range
type RoundAnalysis struct {
	StartBlock            uint64              `json:"startBlock"`
	EndBlock              uint64              `json:"endBlock"`
	TotalBlocks           uint64              `json:"totalBlocks"`
	BlocksWithRoundChange uint64              `json:"blocksWithRoundChange"`
	RoundChangeRate       float64             `json:"roundChangeRate"` // Percentage
	AverageRound          float64             `json:"averageRound"`
	MaxRound              uint32              `json:"maxRound"`
	RoundDistribution     []RoundDistribution `json:"roundDistribution"`
}

// RoundDistribution shows the distribution of blocks by round number
type RoundDistribution struct {
	Round      uint32  `json:"round"`
	Count      uint64  `json:"count"`
	Percentage float64 `json:"percentage"`
}

// BlockParticipation represents a validator's participation in a specific block
type BlockParticipation struct {
	BlockNumber   uint64 `json:"blockNumber"`
	WasProposer   bool   `json:"wasProposer"`
	SignedPrepare bool   `json:"signedPrepare"`
	SignedCommit  bool   `json:"signedCommit"`
	Round         uint32 `json:"round"`
}

// ParticipationRate calculates the participation rate percentage
func (cd *ConsensusData) ParticipationRate() float64 {
	if len(cd.Validators) == 0 {
		return 0.0
	}
	return float64(cd.CommitCount) / float64(len(cd.Validators)) * 100.0
}

// IsHealthy returns true if consensus is operating normally
// Healthy means: Round 0 and full participation (>= 2/3 validators)
func (cd *ConsensusData) IsHealthy() bool {
	if cd.Round > 0 {
		return false // Round change indicates delay
	}

	// Check if we have quorum (>= 2/3 of validators)
	requiredSigners := (len(cd.Validators) * 2 / 3) + 1
	return cd.CommitCount >= requiredSigners
}

// CalculateMissedValidators computes which validators missed prepare/commit
func (cd *ConsensusData) CalculateMissedValidators() {
	if len(cd.Validators) == 0 {
		return
	}

	// Create sets for efficient lookup
	prepareSet := make(map[common.Address]bool)
	for _, addr := range cd.PrepareSigners {
		prepareSet[addr] = true
	}

	commitSet := make(map[common.Address]bool)
	for _, addr := range cd.CommitSigners {
		commitSet[addr] = true
	}

	// Find missed validators
	cd.MissedPrepare = make([]common.Address, 0)
	cd.MissedCommit = make([]common.Address, 0)

	for _, validator := range cd.Validators {
		if !prepareSet[validator] {
			cd.MissedPrepare = append(cd.MissedPrepare, validator)
		}
		if !commitSet[validator] {
			cd.MissedCommit = append(cd.MissedCommit, validator)
		}
	}
}
