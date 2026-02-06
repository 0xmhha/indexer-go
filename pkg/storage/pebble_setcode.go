package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
)

// Compile-time check to ensure PebbleStorage implements SetCode interfaces
var _ SetCodeIndexReader = (*PebbleStorage)(nil)
var _ SetCodeIndexWriter = (*PebbleStorage)(nil)

// ========== SetCode Authorization Read Operations ==========

// GetSetCodeAuthorization retrieves a specific authorization by transaction hash and index.
func (s *PebbleStorage) GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := SetCodeAuthorizationKey(txHash, authIndex)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get setcode authorization: %w", err)
	}
	defer closer.Close()

	var record SetCodeAuthorizationRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal setcode authorization: %w", err)
	}

	return &record, nil
}

// GetSetCodeAuthorizationsByTx retrieves all authorizations in a transaction.
func (s *PebbleStorage) GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	prefix := SetCodeAuthorizationKeyPrefix(txHash)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var records []*SetCodeAuthorizationRecord
	for iter.First(); iter.Valid(); iter.Next() {
		var record SetCodeAuthorizationRecord
		if err := json.Unmarshal(iter.Value(), &record); err != nil {
			s.logger.Warn("failed to unmarshal setcode authorization",
				zap.String("key", string(iter.Key())),
				zap.Error(err))
			continue
		}
		records = append(records, &record)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return records, nil
}

// GetSetCodeAuthorizationsByTarget retrieves authorizations where address is the target.
// Results are ordered by block number descending (newest first).
func (s *PebbleStorage) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, limit, offset int) ([]*SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	prefix := SetCodeTargetIndexKeyPrefix(target)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Collect all tx hashes first (reverse order for newest first)
	var txRefs []struct {
		txHash    common.Hash
		authIndex int
	}
	for iter.Last(); iter.Valid(); iter.Prev() {
		value := iter.Value()
		if len(value) >= 32 {
			var ref struct {
				txHash    common.Hash
				authIndex int
			}
			ref.txHash = common.BytesToHash(value[:32])
			if len(value) > 32 {
				ref.authIndex = int(value[32])
			}
			txRefs = append(txRefs, ref)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	// Apply pagination
	start := offset
	if start >= len(txRefs) {
		return []*SetCodeAuthorizationRecord{}, nil
	}
	end := start + limit
	if end > len(txRefs) {
		end = len(txRefs)
	}

	// Fetch full records
	records := make([]*SetCodeAuthorizationRecord, 0, end-start)
	for _, ref := range txRefs[start:end] {
		record, err := s.GetSetCodeAuthorization(ctx, ref.txHash, ref.authIndex)
		if err != nil {
			s.logger.Warn("failed to get setcode authorization",
				zap.String("txHash", ref.txHash.Hex()),
				zap.Int("authIndex", ref.authIndex),
				zap.Error(err))
			continue
		}
		records = append(records, record)
	}

	return records, nil
}

// GetSetCodeAuthorizationsByAuthority retrieves authorizations where address is the authority.
func (s *PebbleStorage) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, limit, offset int) ([]*SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	prefix := SetCodeAuthorityIndexKeyPrefix(authority)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Collect all tx hashes first (reverse order for newest first)
	var txRefs []struct {
		txHash    common.Hash
		authIndex int
	}
	for iter.Last(); iter.Valid(); iter.Prev() {
		value := iter.Value()
		if len(value) >= 32 {
			var ref struct {
				txHash    common.Hash
				authIndex int
			}
			ref.txHash = common.BytesToHash(value[:32])
			if len(value) > 32 {
				ref.authIndex = int(value[32])
			}
			txRefs = append(txRefs, ref)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	// Apply pagination
	start := offset
	if start >= len(txRefs) {
		return []*SetCodeAuthorizationRecord{}, nil
	}
	end := start + limit
	if end > len(txRefs) {
		end = len(txRefs)
	}

	// Fetch full records
	records := make([]*SetCodeAuthorizationRecord, 0, end-start)
	for _, ref := range txRefs[start:end] {
		record, err := s.GetSetCodeAuthorization(ctx, ref.txHash, ref.authIndex)
		if err != nil {
			s.logger.Warn("failed to get setcode authorization",
				zap.String("txHash", ref.txHash.Hex()),
				zap.Int("authIndex", ref.authIndex),
				zap.Error(err))
			continue
		}
		records = append(records, record)
	}

	return records, nil
}

// GetSetCodeAuthorizationsByBlock retrieves all authorizations in a specific block.
func (s *PebbleStorage) GetSetCodeAuthorizationsByBlock(ctx context.Context, blockNumber uint64) ([]*SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	prefix := SetCodeBlockIndexKeyPrefix(blockNumber)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var records []*SetCodeAuthorizationRecord
	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if len(value) >= 32 {
			txHash := common.BytesToHash(value[:32])
			authIndex := 0
			if len(value) > 32 {
				authIndex = int(value[32])
			}

			record, err := s.GetSetCodeAuthorization(ctx, txHash, authIndex)
			if err != nil {
				s.logger.Warn("failed to get setcode authorization",
					zap.String("txHash", txHash.Hex()),
					zap.Int("authIndex", authIndex),
					zap.Error(err))
				continue
			}
			records = append(records, record)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return records, nil
}

// GetAddressSetCodeStats retrieves SetCode statistics for an address.
func (s *PebbleStorage) GetAddressSetCodeStats(ctx context.Context, address common.Address) (*AddressSetCodeStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := SetCodeStatsKey(address)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			// Return zero-value stats
			return &AddressSetCodeStats{
				Address: address,
			}, nil
		}
		return nil, fmt.Errorf("failed to get setcode stats: %w", err)
	}
	defer closer.Close()

	var stats AddressSetCodeStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal setcode stats: %w", err)
	}

	return &stats, nil
}

// GetAddressDelegationState retrieves the current delegation state for an address.
func (s *PebbleStorage) GetAddressDelegationState(ctx context.Context, address common.Address) (*AddressDelegationState, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := SetCodeDelegationStateKey(address)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			// Return state with no delegation
			return &AddressDelegationState{
				Address:       address,
				HasDelegation: false,
			}, nil
		}
		return nil, fmt.Errorf("failed to get delegation state: %w", err)
	}
	defer closer.Close()

	var state AddressDelegationState
	if err := json.Unmarshal(value, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal delegation state: %w", err)
	}

	return &state, nil
}

// GetSetCodeAuthorizationsCountByTarget returns the count of authorizations for a target address.
func (s *PebbleStorage) GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := SetCodeTargetIndexKeyPrefix(target)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, fmt.Errorf("iterator error: %w", err)
	}

	return count, nil
}

// GetSetCodeAuthorizationsCountByAuthority returns the count of authorizations by an authority address.
func (s *PebbleStorage) GetSetCodeAuthorizationsCountByAuthority(ctx context.Context, authority common.Address) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := SetCodeAuthorityIndexKeyPrefix(authority)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, fmt.Errorf("iterator error: %w", err)
	}

	return count, nil
}

// GetSetCodeTransactionCount returns the total count of SetCode authorizations indexed.
func (s *PebbleStorage) GetSetCodeTransactionCount(ctx context.Context) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := SetCodeAuthKeyPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, fmt.Errorf("iterator error: %w", err)
	}

	return count, nil
}

// GetRecentSetCodeAuthorizations retrieves the most recent SetCode authorizations.
func (s *PebbleStorage) GetRecentSetCodeAuthorizations(ctx context.Context, limit int) ([]*SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}

	prefix := SetCodeBlockIndexAllPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var records []*SetCodeAuthorizationRecord
	count := 0

	// Iterate in reverse order (newest first)
	for iter.Last(); iter.Valid() && count < limit; iter.Prev() {
		value := iter.Value()
		if len(value) >= 32 {
			txHash := common.BytesToHash(value[:32])
			authIndex := 0
			if len(value) > 32 {
				authIndex = int(value[32])
			}

			record, err := s.GetSetCodeAuthorization(ctx, txHash, authIndex)
			if err != nil {
				s.logger.Warn("failed to get setcode authorization",
					zap.String("txHash", txHash.Hex()),
					zap.Int("authIndex", authIndex),
					zap.Error(err))
				continue
			}
			records = append(records, record)
			count++
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return records, nil
}

// ========== SetCode Authorization Write Operations ==========

// SaveSetCodeAuthorization saves a SetCode authorization record.
func (s *PebbleStorage) SaveSetCodeAuthorization(ctx context.Context, record *SetCodeAuthorizationRecord) error {
	if s.closed.Load() {
		return ErrClosed
	}

	// Marshal record
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal setcode authorization: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// 1. Save the primary record
	key := SetCodeAuthorizationKey(record.TxHash, record.AuthIndex)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set setcode authorization: %w", err)
	}

	// Create index value: txHash + authIndex
	indexValue := make([]byte, 33)
	copy(indexValue[:32], record.TxHash.Bytes())
	indexValue[32] = byte(record.AuthIndex)

	// 2. Create target index
	targetKey := SetCodeTargetIndexKey(record.TargetAddress, record.BlockNumber, record.TxIndex, record.AuthIndex)
	if err := batch.Set(targetKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set target index: %w", err)
	}

	// 3. Create authority index
	authorityKey := SetCodeAuthorityIndexKey(record.AuthorityAddress, record.BlockNumber, record.TxIndex, record.AuthIndex)
	if err := batch.Set(authorityKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set authority index: %w", err)
	}

	// 4. Create block index
	blockKey := SetCodeBlockIndexKey(record.BlockNumber, record.TxIndex, record.AuthIndex)
	if err := batch.Set(blockKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set block index: %w", err)
	}

	// 5. Create tx index
	txKey := SetCodeTxIndexKey(record.TxHash, record.AuthIndex)
	if err := batch.Set(txKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set tx index: %w", err)
	}

	// Commit batch
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit setcode authorization: %w", err)
	}

	s.logger.Debug("saved setcode authorization",
		zap.String("txHash", record.TxHash.Hex()),
		zap.Int("authIndex", record.AuthIndex),
		zap.String("target", record.TargetAddress.Hex()),
		zap.String("authority", record.AuthorityAddress.Hex()),
		zap.Bool("applied", record.Applied))

	return nil
}

// SaveSetCodeAuthorizations saves multiple authorization records in a batch.
func (s *PebbleStorage) SaveSetCodeAuthorizations(ctx context.Context, records []*SetCodeAuthorizationRecord) error {
	if s.closed.Load() {
		return ErrClosed
	}

	if len(records) == 0 {
		return nil
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	for _, record := range records {
		// Marshal record
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("failed to marshal setcode authorization: %w", err)
		}

		// 1. Save the primary record
		key := SetCodeAuthorizationKey(record.TxHash, record.AuthIndex)
		if err := batch.Set(key, data, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set setcode authorization: %w", err)
		}

		// Create index value: txHash + authIndex
		indexValue := make([]byte, 33)
		copy(indexValue[:32], record.TxHash.Bytes())
		indexValue[32] = byte(record.AuthIndex)

		// 2. Create target index
		targetKey := SetCodeTargetIndexKey(record.TargetAddress, record.BlockNumber, record.TxIndex, record.AuthIndex)
		if err := batch.Set(targetKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set target index: %w", err)
		}

		// 3. Create authority index
		authorityKey := SetCodeAuthorityIndexKey(record.AuthorityAddress, record.BlockNumber, record.TxIndex, record.AuthIndex)
		if err := batch.Set(authorityKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set authority index: %w", err)
		}

		// 4. Create block index
		blockKey := SetCodeBlockIndexKey(record.BlockNumber, record.TxIndex, record.AuthIndex)
		if err := batch.Set(blockKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set block index: %w", err)
		}

		// 5. Create tx index
		txKey := SetCodeTxIndexKey(record.TxHash, record.AuthIndex)
		if err := batch.Set(txKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set tx index: %w", err)
		}
	}

	// Commit batch
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit setcode authorizations batch: %w", err)
	}

	s.logger.Debug("saved setcode authorizations batch",
		zap.Int("count", len(records)))

	return nil
}

// UpdateAddressDelegationState updates the delegation state for an address.
func (s *PebbleStorage) UpdateAddressDelegationState(ctx context.Context, state *AddressDelegationState) error {
	if s.closed.Load() {
		return ErrClosed
	}

	state.UpdatedAt = time.Now()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal delegation state: %w", err)
	}

	key := SetCodeDelegationStateKey(state.Address)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set delegation state: %w", err)
	}

	s.logger.Debug("updated delegation state",
		zap.String("address", state.Address.Hex()),
		zap.Bool("hasDelegation", state.HasDelegation))

	return nil
}

// IncrementSetCodeStats increments SetCode statistics for an address.
func (s *PebbleStorage) IncrementSetCodeStats(ctx context.Context, address common.Address, asTarget, asAuthority bool, blockNumber uint64) error {
	if s.closed.Load() {
		return ErrClosed
	}

	// Get current stats
	stats, err := s.GetAddressSetCodeStats(ctx, address)
	if err != nil {
		return fmt.Errorf("failed to get current stats: %w", err)
	}

	// Update counts
	if asTarget {
		stats.AsTargetCount++
	}
	if asAuthority {
		stats.AsAuthorityCount++
	}
	stats.LastActivityBlock = blockNumber
	stats.LastActivityTime = time.Now()

	// Save updated stats
	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal setcode stats: %w", err)
	}

	key := SetCodeStatsKey(address)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set setcode stats: %w", err)
	}

	s.logger.Debug("incremented setcode stats",
		zap.String("address", address.Hex()),
		zap.Bool("asTarget", asTarget),
		zap.Bool("asAuthority", asAuthority),
		zap.Int("targetCount", stats.AsTargetCount),
		zap.Int("authorityCount", stats.AsAuthorityCount))

	return nil
}
