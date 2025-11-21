package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"go.uber.org/zap"
)

// Ensure PebbleStorage implements WBFTReader and WBFTWriter
var _ WBFTReader = (*PebbleStorage)(nil)
var _ WBFTWriter = (*PebbleStorage)(nil)

// GetWBFTBlockExtra returns WBFT consensus metadata for a block
func (s *PebbleStorage) GetWBFTBlockExtra(ctx context.Context, blockNumber uint64) (*WBFTBlockExtra, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := WBFTBlockExtraKey(blockNumber)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get WBFT block extra: %w", err)
	}
	defer closer.Close()

	var extra WBFTBlockExtra
	if err := json.Unmarshal(value, &extra); err != nil {
		return nil, fmt.Errorf("failed to decode WBFT block extra: %w", err)
	}

	return &extra, nil
}

// GetWBFTBlockExtraByHash returns WBFT consensus metadata for a block by hash
func (s *PebbleStorage) GetWBFTBlockExtraByHash(ctx context.Context, blockHash common.Hash) (*WBFTBlockExtra, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get block number from block hash index
	blockNumKey := BlockHashIndexKey(blockHash)
	blockNumValue, closer, err := s.db.Get(blockNumKey)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get block number: %w", err)
	}
	defer closer.Close()

	blockNumber, err := DecodeUint64(blockNumValue)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block number: %w", err)
	}

	return s.GetWBFTBlockExtra(ctx, blockNumber)
}

// GetEpochInfo returns epoch information for a specific epoch
func (s *PebbleStorage) GetEpochInfo(ctx context.Context, epochNumber uint64) (*EpochInfo, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := WBFTEpochKey(epochNumber)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get epoch info: %w", err)
	}
	defer closer.Close()

	var epochInfo EpochInfo
	if err := json.Unmarshal(value, &epochInfo); err != nil {
		return nil, fmt.Errorf("failed to decode epoch info: %w", err)
	}

	return &epochInfo, nil
}

// GetLatestEpochInfo returns the most recent epoch information
func (s *PebbleStorage) GetLatestEpochInfo(ctx context.Context) (*EpochInfo, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get latest epoch number
	value, closer, err := s.db.Get(LatestEpochKey())
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get latest epoch: %w", err)
	}
	defer closer.Close()

	epochNumber, err := DecodeUint64(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode epoch number: %w", err)
	}

	return s.GetEpochInfo(ctx, epochNumber)
}

// GetValidatorSigningStats returns signing statistics for a validator
func (s *PebbleStorage) GetValidatorSigningStats(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64) (*ValidatorSigningStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := WBFTValidatorStatsKey(validatorAddress, fromBlock, toBlock)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			// Return empty stats if not found
			return &ValidatorSigningStats{
				ValidatorAddress: validatorAddress,
				FromBlock:        fromBlock,
				ToBlock:          toBlock,
				PrepareSignCount: 0,
				PrepareMissCount: 0,
				CommitSignCount:  0,
				CommitMissCount:  0,
				SigningRate:      0,
			}, nil
		}
		return nil, fmt.Errorf("failed to get validator signing stats: %w", err)
	}
	defer closer.Close()

	var stats ValidatorSigningStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to decode validator signing stats: %w", err)
	}

	return &stats, nil
}

// GetAllValidatorsSigningStats returns signing statistics for all validators in a block range
func (s *PebbleStorage) GetAllValidatorsSigningStats(ctx context.Context, fromBlock, toBlock uint64, limit, offset int) ([]*ValidatorSigningStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Default limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	// Scan all validator activity records to aggregate stats
	prefix := WBFTValidatorActivityAllKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Map: validator address -> stats
	statsMap := make(map[common.Address]*ValidatorSigningStats)

	for iter.First(); iter.Valid(); iter.Next() {
		var activity ValidatorSigningActivity
		if err := json.Unmarshal(iter.Value(), &activity); err != nil {
			s.logger.Warn("failed to decode validator activity", zap.Error(err))
			continue
		}

		// Filter by block range
		if activity.BlockNumber < fromBlock || activity.BlockNumber > toBlock {
			continue
		}

		// Initialize stats if not exists
		if _, exists := statsMap[activity.ValidatorAddress]; !exists {
			statsMap[activity.ValidatorAddress] = &ValidatorSigningStats{
				ValidatorAddress: activity.ValidatorAddress,
				ValidatorIndex:   activity.ValidatorIndex,
				FromBlock:        fromBlock,
				ToBlock:          toBlock,
			}
		}

		stats := statsMap[activity.ValidatorAddress]

		// Update prepare stats
		if activity.SignedPrepare {
			stats.PrepareSignCount++
		} else {
			stats.PrepareMissCount++
		}

		// Update commit stats
		if activity.SignedCommit {
			stats.CommitSignCount++
		} else {
			stats.CommitMissCount++
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("failed to iterate validator activities: %w", err)
	}

	// Calculate signing rates and convert to slice
	result := make([]*ValidatorSigningStats, 0, len(statsMap))
	for _, stats := range statsMap {
		totalBlocks := stats.PrepareSignCount + stats.PrepareMissCount
		if totalBlocks > 0 {
			stats.SigningRate = float64(stats.PrepareSignCount) / float64(totalBlocks) * 100
		}
		result = append(result, stats)
	}

	// Apply pagination
	start := offset
	if start >= len(result) {
		return []*ValidatorSigningStats{}, nil
	}

	end := start + limit
	if end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// GetValidatorSigningActivity returns detailed signing activity for a validator
func (s *PebbleStorage) GetValidatorSigningActivity(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64, limit, offset int) ([]*ValidatorSigningActivity, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Default limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	prefix := WBFTValidatorActivityKeyPrefix(validatorAddress)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	result := make([]*ValidatorSigningActivity, 0)
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		var activity ValidatorSigningActivity
		if err := json.Unmarshal(iter.Value(), &activity); err != nil {
			s.logger.Warn("failed to decode validator activity", zap.Error(err))
			continue
		}

		// Filter by block range
		if activity.BlockNumber < fromBlock || activity.BlockNumber > toBlock {
			continue
		}

		// Apply pagination
		if count < offset {
			count++
			continue
		}

		result = append(result, &activity)

		if len(result) >= limit {
			break
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("failed to iterate validator activities: %w", err)
	}

	return result, nil
}

// GetBlockSigners returns list of validators who signed a specific block
func (s *PebbleStorage) GetBlockSigners(ctx context.Context, blockNumber uint64) (preparers []common.Address, committers []common.Address, err error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, nil, err
	}

	// Get prepare signers
	preparePrefix := WBFTSignerPrepareIndexKeyPrefix(blockNumber)
	prepareIter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: preparePrefix,
		UpperBound: append(preparePrefix, 0xff),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create prepare iterator: %w", err)
	}
	defer prepareIter.Close()

	preparers = make([]common.Address, 0)
	for prepareIter.First(); prepareIter.Valid(); prepareIter.Next() {
		// Key format: /index/wbft/signers/prepare/{blockNumber}/{validator}
		// Extract validator address from key
		keyStr := string(prepareIter.Key())
		// Find last '/' and extract address
		lastSlash := len(keyStr) - 1
		for lastSlash >= 0 && keyStr[lastSlash] != '/' {
			lastSlash--
		}
		if lastSlash >= 0 && lastSlash+1 < len(keyStr) {
			addrHex := keyStr[lastSlash+1:]
			preparers = append(preparers, common.HexToAddress(addrHex))
		}
	}

	if err := prepareIter.Error(); err != nil {
		return nil, nil, fmt.Errorf("failed to iterate prepare signers: %w", err)
	}

	// Get commit signers
	commitPrefix := WBFTSignerCommitIndexKeyPrefix(blockNumber)
	commitIter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: commitPrefix,
		UpperBound: append(commitPrefix, 0xff),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create commit iterator: %w", err)
	}
	defer commitIter.Close()

	committers = make([]common.Address, 0)
	for commitIter.First(); commitIter.Valid(); commitIter.Next() {
		// Key format: /index/wbft/signers/commit/{blockNumber}/{validator}
		// Extract validator address from key
		keyStr := string(commitIter.Key())
		// Find last '/' and extract address
		lastSlash := len(keyStr) - 1
		for lastSlash >= 0 && keyStr[lastSlash] != '/' {
			lastSlash--
		}
		if lastSlash >= 0 && lastSlash+1 < len(keyStr) {
			addrHex := keyStr[lastSlash+1:]
			committers = append(committers, common.HexToAddress(addrHex))
		}
	}

	if err := commitIter.Error(); err != nil {
		return nil, nil, fmt.Errorf("failed to iterate commit signers: %w", err)
	}

	return preparers, committers, nil
}

// SaveWBFTBlockExtra saves WBFT consensus metadata for a block
func (s *PebbleStorage) SaveWBFTBlockExtra(ctx context.Context, extra *WBFTBlockExtra) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}

	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Encode to JSON
	value, err := json.Marshal(extra)
	if err != nil {
		return fmt.Errorf("failed to encode WBFT block extra: %w", err)
	}

	key := WBFTBlockExtraKey(extra.BlockNumber)
	if err := s.db.Set(key, value, pebble.Sync); err != nil {
		return fmt.Errorf("failed to save WBFT block extra: %w", err)
	}

	return nil
}

// SaveEpochInfo saves epoch information
func (s *PebbleStorage) SaveEpochInfo(ctx context.Context, epochInfo *EpochInfo) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}

	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Encode to JSON
	value, err := json.Marshal(epochInfo)
	if err != nil {
		return fmt.Errorf("failed to encode epoch info: %w", err)
	}

	// Save epoch info
	key := WBFTEpochKey(epochInfo.EpochNumber)
	if err := s.db.Set(key, value, pebble.Sync); err != nil {
		return fmt.Errorf("failed to save epoch info: %w", err)
	}

	// Update latest epoch
	latestEpochValue := EncodeUint64(epochInfo.EpochNumber)
	if err := s.db.Set(LatestEpochKey(), latestEpochValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to update latest epoch: %w", err)
	}

	return nil
}

// UpdateValidatorSigningStats updates signing statistics for validators
func (s *PebbleStorage) UpdateValidatorSigningStats(ctx context.Context, blockNumber uint64, signingActivities []*ValidatorSigningActivity) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}

	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	for _, activity := range signingActivities {
		// Save activity
		activityKey := WBFTValidatorActivityKey(activity.ValidatorAddress, activity.BlockNumber)
		activityValue, err := json.Marshal(activity)
		if err != nil {
			return fmt.Errorf("failed to encode validator activity: %w", err)
		}
		if err := batch.Set(activityKey, activityValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to save validator activity: %w", err)
		}

		// Create signer indexes
		if activity.SignedPrepare {
			prepareIndexKey := WBFTSignerPrepareIndexKey(activity.BlockNumber, activity.ValidatorAddress)
			if err := batch.Set(prepareIndexKey, []byte{1}, pebble.Sync); err != nil {
				return fmt.Errorf("failed to save prepare signer index: %w", err)
			}
		}

		if activity.SignedCommit {
			commitIndexKey := WBFTSignerCommitIndexKey(activity.BlockNumber, activity.ValidatorAddress)
			if err := batch.Set(commitIndexKey, []byte{1}, pebble.Sync); err != nil {
				return fmt.Errorf("failed to save commit signer index: %w", err)
			}
		}
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit validator signing stats: %w", err)
	}

	return nil
}

// Helper functions for WBFT data encoding/decoding

// EncodeWBFTAggregatedSeal encodes WBFTAggregatedSeal to RLP bytes
func EncodeWBFTAggregatedSeal(seal *WBFTAggregatedSeal) ([]byte, error) {
	if seal == nil {
		return nil, nil
	}

	sealRLP := &WBFTAggregatedSealRLP{
		Sealers:   seal.Sealers,
		Signature: seal.Signature,
	}

	return rlp.EncodeToBytes(sealRLP)
}

// DecodeWBFTAggregatedSeal decodes RLP bytes to WBFTAggregatedSeal
func DecodeWBFTAggregatedSeal(data []byte) (*WBFTAggregatedSeal, error) {
	if data == nil || len(data) == 0 {
		return nil, nil
	}

	var sealRLP WBFTAggregatedSealRLP
	if err := rlp.DecodeBytes(data, &sealRLP); err != nil {
		return nil, err
	}

	return &WBFTAggregatedSeal{
		Sealers:   sealRLP.Sealers,
		Signature: sealRLP.Signature,
	}, nil
}

// EncodeEpochInfo encodes EpochInfo to RLP bytes
func EncodeEpochInfo(epochInfo *EpochInfo) ([]byte, error) {
	if epochInfo == nil {
		return nil, nil
	}

	candidates := make([]*CandidateRLP, len(epochInfo.Candidates))
	for i, c := range epochInfo.Candidates {
		candidates[i] = &CandidateRLP{
			Addr:      c.Address[:],
			Diligence: c.Diligence,
		}
	}

	epochInfoRLP := &EpochInfoRLP{
		Candidates:    candidates,
		Validators:    epochInfo.Validators,
		BLSPublicKeys: epochInfo.BLSPublicKeys,
	}

	return rlp.EncodeToBytes(epochInfoRLP)
}

// DecodeEpochInfo decodes RLP bytes to EpochInfo
func DecodeEpochInfo(data []byte) (*EpochInfo, error) {
	if data == nil || len(data) == 0 {
		return nil, nil
	}

	var epochInfoRLP EpochInfoRLP
	if err := rlp.DecodeBytes(data, &epochInfoRLP); err != nil {
		return nil, err
	}

	candidates := make([]Candidate, len(epochInfoRLP.Candidates))
	for i, c := range epochInfoRLP.Candidates {
		if len(c.Addr) != 20 {
			return nil, fmt.Errorf("invalid candidate address length: %d", len(c.Addr))
		}
		var addr common.Address
		copy(addr[:], c.Addr)
		candidates[i] = Candidate{
			Address:   addr,
			Diligence: c.Diligence,
		}
	}

	return &EpochInfo{
		Candidates:    candidates,
		Validators:    epochInfoRLP.Validators,
		BLSPublicKeys: epochInfoRLP.BLSPublicKeys,
	}, nil
}
