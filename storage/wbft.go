package storage

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// WBFTAggregatedSeal represents an aggregated BLS signature from validators
type WBFTAggregatedSeal struct {
	Sealers   []byte // Bitmap of validators who signed
	Signature []byte // Aggregated BLS signature (96 bytes)
}

// WBFTBlockExtra represents WBFT consensus metadata for a block
type WBFTBlockExtra struct {
	BlockNumber       uint64
	BlockHash         common.Hash
	RandaoReveal      []byte              // BLS signature of block number
	PrevRound         uint32              // Previous block's round number
	PrevPreparedSeal  *WBFTAggregatedSeal // Previous block's prepare seal
	PrevCommittedSeal *WBFTAggregatedSeal // Previous block's commit seal
	Round             uint32              // Current round number
	PreparedSeal      *WBFTAggregatedSeal // Prepare phase aggregated seal
	CommittedSeal     *WBFTAggregatedSeal // Commit phase aggregated seal
	GasTip            *big.Int            // Gas tip value agreed through governance
	EpochInfo         *EpochInfo          // Epoch info (only for last block of epoch)
	Timestamp         uint64              // Block timestamp
}

// EpochInfo represents validator set information for an epoch
type EpochInfo struct {
	EpochNumber   uint64      // Epoch number
	BlockNumber   uint64      // Block number where epoch info is stored
	Candidates    []Candidate // Candidate list for next epoch
	Validators    []uint32    // Validator indices for next epoch
	BLSPublicKeys [][]byte    // BLS public keys for next epoch
}

// Candidate represents a validator candidate
type Candidate struct {
	Address   common.Address
	Diligence uint64 // Diligence score (unit: 10^-6)
}

// ValidatorSigningStats represents signing statistics for a validator
type ValidatorSigningStats struct {
	ValidatorAddress common.Address
	ValidatorIndex   uint32
	// Prepare phase statistics
	PrepareSignCount uint64 // Total number of prepare signatures
	PrepareMissCount uint64 // Total number of missed prepare signatures
	// Commit phase statistics
	CommitSignCount uint64 // Total number of commit signatures
	CommitMissCount uint64 // Total number of missed commit signatures
	// Block range
	FromBlock uint64
	ToBlock   uint64
	// Performance metrics
	SigningRate float64 // (SignCount / TotalBlocks) * 100
}

// ValidatorSigningActivity represents a validator's signing activity for a specific block
type ValidatorSigningActivity struct {
	BlockNumber      uint64
	BlockHash        common.Hash
	ValidatorAddress common.Address
	ValidatorIndex   uint32
	SignedPrepare    bool // Whether validator signed in prepare phase
	SignedCommit     bool // Whether validator signed in commit phase
	Round            uint32
	Timestamp        uint64
}

// WBFTReader defines read operations for WBFT metadata
type WBFTReader interface {
	// GetWBFTBlockExtra returns WBFT consensus metadata for a block
	GetWBFTBlockExtra(ctx context.Context, blockNumber uint64) (*WBFTBlockExtra, error)

	// GetWBFTBlockExtraByHash returns WBFT consensus metadata for a block by hash
	GetWBFTBlockExtraByHash(ctx context.Context, blockHash common.Hash) (*WBFTBlockExtra, error)

	// GetEpochInfo returns epoch information for a specific epoch
	GetEpochInfo(ctx context.Context, epochNumber uint64) (*EpochInfo, error)

	// GetLatestEpochInfo returns the most recent epoch information
	GetLatestEpochInfo(ctx context.Context) (*EpochInfo, error)

	// GetValidatorSigningStats returns signing statistics for a validator
	GetValidatorSigningStats(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64) (*ValidatorSigningStats, error)

	// GetAllValidatorsSigningStats returns signing statistics for all validators in a block range
	GetAllValidatorsSigningStats(ctx context.Context, fromBlock, toBlock uint64, limit, offset int) ([]*ValidatorSigningStats, error)

	// GetValidatorSigningActivity returns detailed signing activity for a validator
	GetValidatorSigningActivity(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64, limit, offset int) ([]*ValidatorSigningActivity, error)

	// GetBlockSigners returns list of validators who signed a specific block
	GetBlockSigners(ctx context.Context, blockNumber uint64) (preparers []common.Address, committers []common.Address, err error)
}

// WBFTWriter defines write operations for WBFT metadata
type WBFTWriter interface {
	// SaveWBFTBlockExtra saves WBFT consensus metadata for a block
	SaveWBFTBlockExtra(ctx context.Context, extra *WBFTBlockExtra) error

	// SaveEpochInfo saves epoch information
	SaveEpochInfo(ctx context.Context, epochInfo *EpochInfo) error

	// UpdateValidatorSigningStats updates signing statistics for validators
	UpdateValidatorSigningStats(ctx context.Context, blockNumber uint64, signingActivities []*ValidatorSigningActivity) error
}
