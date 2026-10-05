package consensus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Backend is the storage the WBFT store needs: key-value access (bound to
// the block transaction through ctx while a block is indexed) and the
// stored blocks.
type Backend interface {
	storage.KV
	storage.ModelReader
}

// Store reads and writes WBFT consensus data.
type Store struct {
	db     Backend
	logger *zap.Logger
}

var (
	_ WBFTReader = (*Store)(nil)
	_ WBFTWriter = (*Store)(nil)
)

// NewStore returns a WBFT store over db.
func NewStore(db Backend, logger *zap.Logger) *Store {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Store{db: db, logger: logger}
}

// Open returns a WBFT store over s, which must provide key-value access and
// stored blocks (the Pebble storage does).
func Open(s any, logger *zap.Logger) (*Store, error) {
	db, ok := s.(Backend)
	if !ok {
		return nil, fmt.Errorf("storage %T does not support WBFT data", s)
	}
	return NewStore(db, logger), nil
}

// getJSON decodes the value of key into v; a missing key is
// storage.ErrNotFound.
func (s *Store) getJSON(ctx context.Context, key []byte, what string, v any) error {
	raw, err := s.db.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return storage.ErrNotFound
		}
		return fmt.Errorf("failed to get %s: %w", what, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("failed to decode %s: %w", what, err)
	}
	return nil
}

func (s *Store) putJSON(ctx context.Context, key []byte, what string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to encode %s: %w", what, err)
	}
	if err := s.db.Put(ctx, key, raw); err != nil {
		return fmt.Errorf("failed to save %s: %w", what, err)
	}
	return nil
}

// scanPrefix visits the keys under prefix in key order (reverse order when
// reverse is set) until fn returns false.
func (s *Store) scanPrefix(ctx context.Context, prefix []byte, reverse bool, fn func(key, value []byte) bool) error {
	return s.db.Scan(ctx, prefix, storage.PrefixEnd(prefix), reverse, fn)
}

// GetWBFTBlockExtra returns WBFT consensus metadata for a block
func (s *Store) GetWBFTBlockExtra(ctx context.Context, blockNumber uint64) (*WBFTBlockExtra, error) {
	var extra WBFTBlockExtra
	if err := s.getJSON(ctx, WBFTBlockExtraKey(blockNumber), "WBFT block extra", &extra); err != nil {
		return nil, err
	}
	return &extra, nil
}

// GetWBFTBlockExtraByHash returns WBFT consensus metadata for a block by hash
func (s *Store) GetWBFTBlockExtraByHash(ctx context.Context, blockHash common.Hash) (*WBFTBlockExtra, error) {
	raw, err := s.db.Get(ctx, storage.BlockHashIndexKey(blockHash))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get block number: %w", err)
	}
	blockNumber, err := storage.DecodeUint64(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block number: %w", err)
	}
	return s.GetWBFTBlockExtra(ctx, blockNumber)
}

// GetEpochInfo returns epoch information for a specific epoch
func (s *Store) GetEpochInfo(ctx context.Context, epochNumber uint64) (*EpochInfo, error) {
	var info EpochInfo
	if err := s.getJSON(ctx, WBFTEpochKey(epochNumber), "epoch info", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetLatestEpochInfo returns the most recent epoch information
func (s *Store) GetLatestEpochInfo(ctx context.Context) (*EpochInfo, error) {
	raw, err := s.db.Get(ctx, LatestEpochKey())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get latest epoch: %w", err)
	}
	epochNumber, err := storage.DecodeUint64(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode epoch number: %w", err)
	}
	return s.GetEpochInfo(ctx, epochNumber)
}

// proposerCounts counts the stored blocks in [from, to] by proposer
// (coinbase).
func (s *Store) proposerCounts(ctx context.Context, from, to uint64) map[common.Address]uint64 {
	counts := make(map[common.Address]uint64)
	for n := from; n <= to; n++ {
		if b, err := s.db.GetModelBlock(ctx, n); err == nil && b != nil {
			counts[b.Miner]++
		}
		if n == to { // to may be the largest uint64
			break
		}
	}
	return counts
}

// GetValidatorSigningStats returns signing statistics for a validator
func (s *Store) GetValidatorSigningStats(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64) (*ValidatorSigningStats, error) {
	totalBlocksInRange := toBlock - fromBlock + 1

	var stats ValidatorSigningStats
	err := s.getJSON(ctx, WBFTValidatorStatsKey(validatorAddress, fromBlock, toBlock), "validator signing stats", &stats)
	if errors.Is(err, storage.ErrNotFound) {
		// Return empty stats if not found
		return &ValidatorSigningStats{
			ValidatorAddress: validatorAddress,
			FromBlock:        fromBlock,
			ToBlock:          toBlock,
			TotalBlocks:      totalBlocksInRange,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	// Compute proposer stats from block headers
	stats.TotalBlocks = totalBlocksInRange
	stats.BlocksProposed = s.proposerCounts(ctx, fromBlock, toBlock)[validatorAddress]
	if totalBlocksInRange > 0 {
		stats.ProposalRate = float64(stats.BlocksProposed) / float64(totalBlocksInRange) * constants.PercentageMultiplier
	}
	return &stats, nil
}

// clampLimit applies the default and maximum page size.
func clampLimit(limit int) int {
	if limit <= 0 {
		return constants.DefaultMaxPaginationLimit
	}
	if limit > constants.MaxPaginationLimitExtended {
		return constants.MaxPaginationLimitExtended
	}
	return limit
}

// GetAllValidatorsSigningStats returns signing statistics for all validators in a block range
func (s *Store) GetAllValidatorsSigningStats(ctx context.Context, fromBlock, toBlock uint64, limit, offset int) ([]*ValidatorSigningStats, error) {
	limit = clampLimit(limit)

	// Scan all validator activity records to aggregate stats
	statsMap := make(map[common.Address]*ValidatorSigningStats)
	err := s.scanPrefix(ctx, WBFTValidatorActivityAllKeyPrefix(), false, func(_, value []byte) bool {
		var activity ValidatorSigningActivity
		if err := json.Unmarshal(value, &activity); err != nil {
			s.logger.Warn("failed to decode validator activity", zap.Error(err))
			return true
		}
		if activity.BlockNumber < fromBlock || activity.BlockNumber > toBlock {
			return true
		}
		stats, ok := statsMap[activity.ValidatorAddress]
		if !ok {
			stats = &ValidatorSigningStats{
				ValidatorAddress: activity.ValidatorAddress,
				ValidatorIndex:   activity.ValidatorIndex,
				FromBlock:        fromBlock,
				ToBlock:          toBlock,
			}
			statsMap[activity.ValidatorAddress] = stats
		}
		if activity.SignedPrepare {
			stats.PrepareSignCount++
		} else {
			stats.PrepareMissCount++
		}
		if activity.SignedCommit {
			stats.CommitSignCount++
		} else {
			stats.CommitMissCount++
		}
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate validator activities: %w", err)
	}

	// Calculate signing rates, proposal stats, and convert to slice
	totalBlocksInRange := toBlock - fromBlock + 1
	proposerCounts := s.proposerCounts(ctx, fromBlock, toBlock)
	result := make([]*ValidatorSigningStats, 0, len(statsMap))
	for _, stats := range statsMap {
		activityCount := stats.PrepareSignCount + stats.PrepareMissCount
		if activityCount > 0 {
			stats.SigningRate = float64(stats.PrepareSignCount) / float64(activityCount) * constants.PercentageMultiplier
		}
		stats.TotalBlocks = totalBlocksInRange
		stats.BlocksProposed = proposerCounts[stats.ValidatorAddress]
		if totalBlocksInRange > 0 {
			stats.ProposalRate = float64(stats.BlocksProposed) / float64(totalBlocksInRange) * constants.PercentageMultiplier
		}
		result = append(result, stats)
	}

	// Apply pagination
	if offset >= len(result) {
		return []*ValidatorSigningStats{}, nil
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}
	return result[offset:end], nil
}

// GetValidatorSigningActivity returns detailed signing activity for a validator
func (s *Store) GetValidatorSigningActivity(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64, limit, offset int) ([]*ValidatorSigningActivity, error) {
	limit = clampLimit(limit)

	result := make([]*ValidatorSigningActivity, 0, 32)
	count := 0
	err := s.scanPrefix(ctx, WBFTValidatorActivityKeyPrefix(validatorAddress), false, func(_, value []byte) bool {
		var activity ValidatorSigningActivity
		if err := json.Unmarshal(value, &activity); err != nil {
			s.logger.Warn("failed to decode validator activity", zap.Error(err))
			return true
		}
		if activity.BlockNumber < fromBlock || activity.BlockNumber > toBlock {
			return true
		}
		if count < offset {
			count++
			return true
		}
		result = append(result, &activity)
		return len(result) < limit
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate validator activities: %w", err)
	}
	return result, nil
}

// signersAt returns the validators indexed under prefix; the address is
// the last element of each key.
func (s *Store) signersAt(ctx context.Context, prefix []byte) ([]common.Address, error) {
	out := make([]common.Address, 0, 32)
	err := s.scanPrefix(ctx, prefix, false, func(key, _ []byte) bool {
		k := string(key)
		if i := strings.LastIndexByte(k, '/'); i >= 0 && i+1 < len(k) {
			out = append(out, common.HexToAddress(k[i+1:]))
		}
		return true
	})
	return out, err
}

// GetBlockSigners returns list of validators who signed a specific block
func (s *Store) GetBlockSigners(ctx context.Context, blockNumber uint64) (preparers []common.Address, committers []common.Address, err error) {
	preparers, err = s.signersAt(ctx, WBFTSignerPrepareIndexKeyPrefix(blockNumber))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to iterate prepare signers: %w", err)
	}
	committers, err = s.signersAt(ctx, WBFTSignerCommitIndexKeyPrefix(blockNumber))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to iterate commit signers: %w", err)
	}
	return preparers, committers, nil
}

// GetEpochsList returns a paginated list of epochs, ordered by epoch number descending
func (s *Store) GetEpochsList(ctx context.Context, limit, offset int) ([]*EpochInfo, int, error) {
	// The latest epoch key lives under /meta/wbft/, outside this prefix.
	var allEpochs []*EpochInfo
	err := s.scanPrefix(ctx, WBFTEpochKeyPrefix(), true, func(_, value []byte) bool {
		var info EpochInfo
		if err := json.Unmarshal(value, &info); err != nil {
			s.logger.Warn("failed to decode epoch info", zap.Error(err))
			return true
		}
		allEpochs = append(allEpochs, &info)
		return true
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to iterate epochs: %w", err)
	}

	totalCount := len(allEpochs)
	if offset >= totalCount {
		return []*EpochInfo{}, totalCount, nil
	}
	end := offset + limit
	if end > totalCount {
		end = totalCount
	}
	return allEpochs[offset:end], totalCount, nil
}

// SaveWBFTBlockExtra saves WBFT consensus metadata for a block
func (s *Store) SaveWBFTBlockExtra(ctx context.Context, extra *WBFTBlockExtra) error {
	return s.putJSON(ctx, WBFTBlockExtraKey(extra.BlockNumber), "WBFT block extra", extra)
}

// SaveEpochInfo saves epoch information and makes it the latest epoch
func (s *Store) SaveEpochInfo(ctx context.Context, epochInfo *EpochInfo) error {
	if err := s.putJSON(ctx, WBFTEpochKey(epochInfo.EpochNumber), "epoch info", epochInfo); err != nil {
		return err
	}
	if err := s.db.Put(ctx, LatestEpochKey(), storage.EncodeUint64(epochInfo.EpochNumber)); err != nil {
		return fmt.Errorf("failed to update latest epoch: %w", err)
	}
	return nil
}

// UpdateValidatorSigningStats records the signing activity of validators
// for a block, with signer indexes. While a block is indexed the writes
// belong to the block transaction.
func (s *Store) UpdateValidatorSigningStats(ctx context.Context, blockNumber uint64, signingActivities []*ValidatorSigningActivity) error {
	for _, activity := range signingActivities {
		if err := s.putJSON(ctx, WBFTValidatorActivityKey(activity.ValidatorAddress, activity.BlockNumber), "validator activity", activity); err != nil {
			return err
		}
		if activity.SignedPrepare {
			if err := s.db.Put(ctx, WBFTSignerPrepareIndexKey(activity.BlockNumber, activity.ValidatorAddress), []byte{1}); err != nil {
				return fmt.Errorf("failed to save prepare signer index: %w", err)
			}
		}
		if activity.SignedCommit {
			if err := s.db.Put(ctx, WBFTSignerCommitIndexKey(activity.BlockNumber, activity.ValidatorAddress), []byte{1}); err != nil {
				return fmt.Errorf("failed to save commit signer index: %w", err)
			}
		}
	}
	return nil
}

// GetModelBlock returns a stored block (the proposer of WBFT data).
func (s *Store) GetModelBlock(ctx context.Context, height uint64) (*model.Block, error) {
	return s.db.GetModelBlock(ctx, height)
}
