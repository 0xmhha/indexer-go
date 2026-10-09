package storage

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Compile-time check to ensure PebbleStorage implements SetCode interfaces
var _ port.SetCodeIndexReader = (*PebbleStorage)(nil)
var _ port.SetCodeIndexWriter = (*PebbleStorage)(nil)

// ========== SetCode Authorization Read Operations ==========

// GetSetCodeAuthorization retrieves a specific authorization by transaction hash and index.
func (s *PebbleStorage) GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*port.SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	key := SetCodeAuthorizationKey(txHash, authIndex)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get setcode authorization: %w", err)
	}
	defer func() { _ = closer.Close() }()

	var record port.SetCodeAuthorizationRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal setcode authorization: %w", err)
	}

	return &record, nil
}

// GetSetCodeAuthorizationsByTx retrieves all authorizations in a transaction.
func (s *PebbleStorage) GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*port.SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	prefix := SetCodeAuthorizationKeyPrefix(txHash)

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	var records []*port.SetCodeAuthorizationRecord
	for iter.First(); iter.Valid(); iter.Next() {
		var record port.SetCodeAuthorizationRecord
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

// GetSetCodeAuthorizationsByTarget returns one page of the authorizations
// where address is the target, newest first.
func (s *PebbleStorage) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	return s.setCodeAuthorizationPage(ctx, SetCodeTargetIndexKeyPrefix(target), page)
}

// GetSetCodeAuthorizationsByAuthority returns one page of the authorizations
// where address is the authority, newest first.
func (s *PebbleStorage) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	return s.setCodeAuthorizationPage(ctx, SetCodeAuthorityIndexKeyPrefix(authority), page)
}

// setCodeAuthorizationPage reads one page of a SetCode address index (keys
// ordered by block, transaction index and authorization index) in reverse,
// so the newest authorization comes first, and loads the records it refers
// to. The cursor is the index key of the last entry.
func (s *PebbleStorage) setCodeAuthorizationPage(ctx context.Context, prefix []byte, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	limit := min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
	isRef := func(_, value []byte) bool { return len(value) >= common.HashLength }
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), true, page, limit, isRef)
	if err != nil {
		return nil, "", err
	}

	records := make([]*port.SetCodeAuthorizationRecord, 0, len(entries))
	for _, e := range entries {
		txHash, authIndex := decodeSetCodeIndexValue(e.Value)
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
	return records, next, nil
}

// GetSetCodeAuthorizationsByBlock retrieves all authorizations in a specific block.
func (s *PebbleStorage) GetSetCodeAuthorizationsByBlock(ctx context.Context, blockNumber uint64) ([]*port.SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	prefix := SetCodeBlockIndexKeyPrefix(blockNumber)

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	var records []*port.SetCodeAuthorizationRecord
	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if len(value) >= 32 {
			txHash, authIndex := decodeSetCodeIndexValue(value)

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
// CurrentDelegation is taken from the address's delegation state.
func (s *PebbleStorage) GetAddressSetCodeStats(ctx context.Context, address common.Address) (*port.AddressSetCodeStats, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	stats, err := s.getStoredSetCodeStats(ctx, address)
	if err != nil {
		return nil, err
	}
	state, err := s.GetAddressDelegationState(ctx, address)
	if err != nil {
		return nil, err
	}
	stats.CurrentDelegation = nil
	if state.HasDelegation && state.DelegationTarget != nil {
		target := *state.DelegationTarget
		stats.CurrentDelegation = &target
	}
	return stats, nil
}

// getStoredSetCodeStats reads the stored SetCode counters for an address.
func (s *PebbleStorage) getStoredSetCodeStats(ctx context.Context, address common.Address) (*port.AddressSetCodeStats, error) {
	key := SetCodeStatsKey(address)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			// Return zero-value stats
			return &port.AddressSetCodeStats{
				Address: address,
			}, nil
		}
		return nil, fmt.Errorf("failed to get setcode stats: %w", err)
	}
	defer func() { _ = closer.Close() }()

	var stats port.AddressSetCodeStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal setcode stats: %w", err)
	}

	return &stats, nil
}

// GetAddressDelegationState retrieves the current delegation state for an address.
func (s *PebbleStorage) GetAddressDelegationState(ctx context.Context, address common.Address) (*port.AddressDelegationState, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	key := SetCodeDelegationStateKey(address)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			// Return state with no delegation
			return &port.AddressDelegationState{
				Address:       address,
				HasDelegation: false,
			}, nil
		}
		return nil, fmt.Errorf("failed to get delegation state: %w", err)
	}
	defer func() { _ = closer.Close() }()

	var state port.AddressDelegationState
	if err := json.Unmarshal(value, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal delegation state: %w", err)
	}

	return &state, nil
}

// GetSetCodeAuthorizationsCountByTarget returns the count of authorizations for a target address.
func (s *PebbleStorage) GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error) {
	if s.closed.Load() {
		return 0, port.ErrClosed
	}

	prefix := SetCodeTargetIndexKeyPrefix(target)

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

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
		return 0, port.ErrClosed
	}

	prefix := SetCodeAuthorityIndexKeyPrefix(authority)

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, fmt.Errorf("iterator error: %w", err)
	}

	return count, nil
}

// GetSetCodeTransactionCount returns the total count of SetCode transactions indexed.
// Authorization keys are grouped by transaction hash, so each transaction is
// counted once however many authorizations it carries.
func (s *PebbleStorage) GetSetCodeTransactionCount(ctx context.Context) (int, error) {
	if s.closed.Load() {
		return 0, port.ErrClosed
	}

	prefix := SetCodeAuthKeyPrefix()

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	count := 0
	var lastTx []byte
	for iter.First(); iter.Valid(); iter.Next() {
		// Key: prefix + txHash hex + "/" + authIndex
		key := iter.Key()
		if len(key) < len(prefix)+common.HashLength*2+2 {
			continue
		}
		tx := key[len(prefix) : len(prefix)+common.HashLength*2+2]
		if string(tx) != string(lastTx) {
			count++
			lastTx = append(lastTx[:0], tx...)
		}
	}

	if err := iter.Error(); err != nil {
		return 0, fmt.Errorf("iterator error: %w", err)
	}

	return count, nil
}

// GetRecentSetCodeAuthorizations retrieves the most recent SetCode authorizations.
func (s *PebbleStorage) GetRecentSetCodeAuthorizations(ctx context.Context, limit int) ([]*port.SetCodeAuthorizationRecord, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}

	prefix := SetCodeBlockIndexAllPrefix()

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	var records []*port.SetCodeAuthorizationRecord
	count := 0

	// Iterate in reverse order (newest first)
	for iter.Last(); iter.Valid() && count < limit; iter.Prev() {
		value := iter.Value()
		if len(value) >= 32 {
			txHash, authIndex := decodeSetCodeIndexValue(value)

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
func (s *PebbleStorage) SaveSetCodeAuthorization(ctx context.Context, record *port.SetCodeAuthorizationRecord) error {
	if s.closed.Load() {
		return port.ErrClosed
	}

	// Marshal record
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal setcode authorization: %w", err)
	}

	batch := s.newBatch(ctx)
	defer func() { _ = batch.Close() }()

	// 1. Save the primary record
	key := SetCodeAuthorizationKey(record.TxHash, record.AuthIndex)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set setcode authorization: %w", err)
	}

	indexValue := encodeSetCodeIndexValue(record)

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
	if err := s.commitBatch(ctx, batch, pebble.Sync); err != nil {
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
func (s *PebbleStorage) SaveSetCodeAuthorizations(ctx context.Context, records []*port.SetCodeAuthorizationRecord) error {
	if s.closed.Load() {
		return port.ErrClosed
	}

	if len(records) == 0 {
		return nil
	}

	batch := s.newBatch(ctx)
	defer func() { _ = batch.Close() }()

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

		indexValue := encodeSetCodeIndexValue(record)

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
	if err := s.commitBatch(ctx, batch, pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit setcode authorizations batch: %w", err)
	}

	s.logger.Debug("saved setcode authorizations batch",
		zap.Int("count", len(records)))

	return nil
}

// UpdateAddressDelegationState updates the delegation state for an address.
func (s *PebbleStorage) UpdateAddressDelegationState(ctx context.Context, state *port.AddressDelegationState) error {
	if s.closed.Load() {
		return port.ErrClosed
	}

	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now()
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal delegation state: %w", err)
	}

	key := SetCodeDelegationStateKey(state.Address)
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
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
		return port.ErrClosed
	}

	// Get current stats (CurrentDelegation is not stored; it follows the delegation state)
	stats, err := s.getStoredSetCodeStats(ctx, address)
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
	stats.LastActivityTime = s.blockTimeOrNow(ctx, blockNumber)

	// Save updated stats
	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal setcode stats: %w", err)
	}

	key := SetCodeStatsKey(address)
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
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

// blockTimeOrNow returns the timestamp of a stored block, so values derived
// while indexing a block do not depend on when indexing ran. Within a block
// transaction the block written earlier in the same transaction is visible.
// It falls back to the wall clock only when the block is not stored.
func (s *PebbleStorage) blockTimeOrNow(ctx context.Context, height uint64) time.Time {
	if blk, err := s.GetBlock(ctx, height); err == nil {
		return time.Unix(int64(blk.Time), 0).UTC()
	}
	return time.Now()
}

// encodeSetCodeIndexValue returns the value of a SetCode secondary index
// entry: the transaction hash followed by the authorization index as a
// 4-byte big-endian integer.
func encodeSetCodeIndexValue(record *port.SetCodeAuthorizationRecord) []byte {
	value := make([]byte, common.HashLength+4)
	copy(value[:common.HashLength], record.TxHash.Bytes())
	binary.BigEndian.PutUint32(value[common.HashLength:], uint32(record.AuthIndex))
	return value
}

// decodeSetCodeIndexValue reads a SetCode secondary index value. Values
// written before the authorization index was widened hold it in one byte.
func decodeSetCodeIndexValue(value []byte) (common.Hash, int) {
	txHash := common.BytesToHash(value[:common.HashLength])
	rest := value[common.HashLength:]
	if len(rest) >= 4 {
		return txHash, int(binary.BigEndian.Uint32(rest[:4]))
	}
	if len(rest) > 0 {
		return txHash, int(rest[0])
	}
	return txHash, 0
}
