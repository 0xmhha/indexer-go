package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/userop"
)

// Compile-time check to ensure PebbleStorage implements UserOp interfaces
var _ UserOpIndexReader = (*PebbleStorage)(nil)
var _ UserOpIndexWriter = (*PebbleStorage)(nil)

// ========== UserOp Read Operations ==========

// GetUserOp retrieves a specific UserOperation by its hash.
func (s *PebbleStorage) GetUserOp(ctx context.Context, opHash common.Hash) (*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := UserOpKey(opHash)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get userop: %w", err)
	}
	defer closer.Close()

	var op userop.UserOperation
	if err := json.Unmarshal(value, &op); err != nil {
		return nil, fmt.Errorf("failed to unmarshal userop: %w", err)
	}

	return &op, nil
}

// GetUserOpsByTx retrieves all UserOperations in a transaction.
func (s *PebbleStorage) GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	prefix := UserOpTxIndexKeyPrefix(txHash)
	return s.getUserOpsByIndex(ctx, prefix)
}

// GetUserOpsBySender retrieves UserOperations sent by a specific address.
func (s *PebbleStorage) GetUserOpsBySender(ctx context.Context, sender common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := UserOpSenderIndexKeyPrefix(sender)
	return s.getUserOpsByIndexPaginated(ctx, prefix, limit, offset)
}

// GetUserOpsByBundler retrieves UserOperations bundled by a specific address.
func (s *PebbleStorage) GetUserOpsByBundler(ctx context.Context, bundler common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := UserOpBundlerIndexKeyPrefix(bundler)
	return s.getUserOpsByIndexPaginated(ctx, prefix, limit, offset)
}

// GetUserOpsByBlock retrieves all UserOperations in a specific block.
func (s *PebbleStorage) GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	prefix := UserOpBlockIndexKeyPrefix(blockNumber)
	return s.getUserOpsByIndex(ctx, prefix)
}

// GetUserOpsByPaymaster retrieves UserOperations sponsored by a specific paymaster.
func (s *PebbleStorage) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := UserOpPaymasterIndexKeyPrefix(paymaster)
	return s.getUserOpsByIndexPaginated(ctx, prefix, limit, offset)
}

// GetUserOpsByFactory retrieves UserOperations that deployed accounts via a specific factory.
func (s *PebbleStorage) GetUserOpsByFactory(ctx context.Context, factory common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := UserOpFactoryIndexKeyPrefix(factory)
	return s.getUserOpsByIndexPaginated(ctx, prefix, limit, offset)
}

// GetBundlerStats retrieves statistics for a bundler address.
func (s *PebbleStorage) GetBundlerStats(ctx context.Context, bundler common.Address) (*userop.BundlerStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := BundlerStatsKey(bundler)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return &userop.BundlerStats{Address: bundler}, nil
		}
		return nil, fmt.Errorf("failed to get bundler stats: %w", err)
	}
	defer closer.Close()

	var stats userop.BundlerStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bundler stats: %w", err)
	}

	return &stats, nil
}

// GetFactoryStats retrieves statistics for a factory address.
func (s *PebbleStorage) GetFactoryStats(ctx context.Context, factory common.Address) (*userop.FactoryStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := FactoryStatsKey(factory)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return &userop.FactoryStats{Address: factory}, nil
		}
		return nil, fmt.Errorf("failed to get factory stats: %w", err)
	}
	defer closer.Close()

	var stats userop.FactoryStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal factory stats: %w", err)
	}

	return &stats, nil
}

// GetPaymasterStats retrieves statistics for a paymaster address.
func (s *PebbleStorage) GetPaymasterStats(ctx context.Context, paymaster common.Address) (*userop.PaymasterStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := PaymasterStatsKey(paymaster)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return &userop.PaymasterStats{Address: paymaster}, nil
		}
		return nil, fmt.Errorf("failed to get paymaster stats: %w", err)
	}
	defer closer.Close()

	var stats userop.PaymasterStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal paymaster stats: %w", err)
	}

	return &stats, nil
}

// GetSmartAccount retrieves a smart account by address.
func (s *PebbleStorage) GetSmartAccount(ctx context.Context, address common.Address) (*userop.SmartAccount, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := SmartAccountKey(address)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get smart account: %w", err)
	}
	defer closer.Close()

	var account userop.SmartAccount
	if err := json.Unmarshal(value, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal smart account: %w", err)
	}

	return &account, nil
}

// GetRecentUserOps retrieves the most recent UserOperations.
func (s *PebbleStorage) GetRecentUserOps(ctx context.Context, limit int) ([]*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}

	prefix := UserOpBlockIndexAllPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var ops []*userop.UserOperation
	count := 0

	// Iterate in reverse order (newest first)
	for iter.Last(); iter.Valid() && count < limit; iter.Prev() {
		value := iter.Value()
		if len(value) >= 32 {
			opHash := common.BytesToHash(value[:32])
			op, err := s.GetUserOp(ctx, opHash)
			if err != nil {
				s.logger.Warn("failed to get userop from index",
					zap.String("opHash", opHash.Hex()),
					zap.Error(err))
				continue
			}
			ops = append(ops, op)
			count++
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return ops, nil
}

// GetUserOpCount returns the total count of UserOperations indexed.
func (s *PebbleStorage) GetUserOpCount(ctx context.Context) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := UserOpKeyPrefix()

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

// ListBundlers retrieves bundler stats with pagination.
func (s *PebbleStorage) ListBundlers(ctx context.Context, limit, offset int) ([]*userop.BundlerStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := BundlerStatsKeyPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var all []*userop.BundlerStats
	for iter.First(); iter.Valid(); iter.Next() {
		var stats userop.BundlerStats
		if err := json.Unmarshal(iter.Value(), &stats); err != nil {
			s.logger.Warn("failed to unmarshal bundler stats",
				zap.String("key", string(iter.Key())),
				zap.Error(err))
			continue
		}
		all = append(all, &stats)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	// Apply pagination
	start := offset
	if start >= len(all) {
		return []*userop.BundlerStats{}, nil
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}

	return all[start:end], nil
}

// ListFactories retrieves factory stats with pagination.
func (s *PebbleStorage) ListFactories(ctx context.Context, limit, offset int) ([]*userop.FactoryStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := FactoryStatsKeyPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var all []*userop.FactoryStats
	for iter.First(); iter.Valid(); iter.Next() {
		var stats userop.FactoryStats
		if err := json.Unmarshal(iter.Value(), &stats); err != nil {
			s.logger.Warn("failed to unmarshal factory stats",
				zap.String("key", string(iter.Key())),
				zap.Error(err))
			continue
		}
		all = append(all, &stats)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	start := offset
	if start >= len(all) {
		return []*userop.FactoryStats{}, nil
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}

	return all[start:end], nil
}

// ListPaymasters retrieves paymaster stats with pagination.
func (s *PebbleStorage) ListPaymasters(ctx context.Context, limit, offset int) ([]*userop.PaymasterStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := PaymasterStatsKeyPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var all []*userop.PaymasterStats
	for iter.First(); iter.Valid(); iter.Next() {
		var stats userop.PaymasterStats
		if err := json.Unmarshal(iter.Value(), &stats); err != nil {
			s.logger.Warn("failed to unmarshal paymaster stats",
				zap.String("key", string(iter.Key())),
				zap.Error(err))
			continue
		}
		all = append(all, &stats)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	start := offset
	if start >= len(all) {
		return []*userop.PaymasterStats{}, nil
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}

	return all[start:end], nil
}

// ListSmartAccounts retrieves smart accounts with pagination.
func (s *PebbleStorage) ListSmartAccounts(ctx context.Context, limit, offset int) ([]*userop.SmartAccount, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := SmartAccountKeyPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var all []*userop.SmartAccount
	for iter.First(); iter.Valid(); iter.Next() {
		var account userop.SmartAccount
		if err := json.Unmarshal(iter.Value(), &account); err != nil {
			s.logger.Warn("failed to unmarshal smart account",
				zap.String("key", string(iter.Key())),
				zap.Error(err))
			continue
		}
		all = append(all, &account)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	start := offset
	if start >= len(all) {
		return []*userop.SmartAccount{}, nil
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}

	return all[start:end], nil
}

// ========== UserOp Write Operations ==========

// SaveUserOp saves a UserOperation record.
func (s *PebbleStorage) SaveUserOp(ctx context.Context, op *userop.UserOperation) error {
	if s.closed.Load() {
		return ErrClosed
	}

	data, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal userop: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// 1. Save the primary record
	key := UserOpKey(op.Hash)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set userop: %w", err)
	}

	// Index value: opHash (32 bytes)
	indexValue := op.Hash.Bytes()

	// 2. Create sender index
	senderKey := UserOpSenderIndexKey(op.Sender, op.BlockNumber, op.BundleIndex)
	if err := batch.Set(senderKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set sender index: %w", err)
	}

	// 3. Create bundler index
	bundlerKey := UserOpBundlerIndexKey(op.Bundler, op.BlockNumber, op.BundleIndex)
	if err := batch.Set(bundlerKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set bundler index: %w", err)
	}

	// 4. Create block index
	blockKey := UserOpBlockIndexKey(op.BlockNumber, op.BundleIndex)
	if err := batch.Set(blockKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set block index: %w", err)
	}

	// 5. Create tx index
	txKey := UserOpTxIndexKey(op.TransactionHash, op.BundleIndex)
	if err := batch.Set(txKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set tx index: %w", err)
	}

	// 6. Create paymaster index (if paymaster exists)
	if op.Paymaster != nil && *op.Paymaster != (common.Address{}) {
		pmKey := UserOpPaymasterIndexKey(*op.Paymaster, op.BlockNumber, op.BundleIndex)
		if err := batch.Set(pmKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set paymaster index: %w", err)
		}
	}

	// 7. Create factory index (if factory exists)
	if op.Factory != nil && *op.Factory != (common.Address{}) {
		factoryKey := UserOpFactoryIndexKey(*op.Factory, op.BlockNumber, op.BundleIndex)
		if err := batch.Set(factoryKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set factory index: %w", err)
		}
	}

	// Commit batch
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit userop: %w", err)
	}

	s.logger.Debug("saved userop",
		zap.String("opHash", op.Hash.Hex()),
		zap.String("sender", op.Sender.Hex()),
		zap.String("bundler", op.Bundler.Hex()),
		zap.Bool("status", op.Status))

	return nil
}

// SaveUserOps saves multiple UserOperation records in a batch.
func (s *PebbleStorage) SaveUserOps(ctx context.Context, ops []*userop.UserOperation) error {
	if s.closed.Load() {
		return ErrClosed
	}

	if len(ops) == 0 {
		return nil
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	for _, op := range ops {
		data, err := json.Marshal(op)
		if err != nil {
			return fmt.Errorf("failed to marshal userop: %w", err)
		}

		// 1. Save the primary record
		key := UserOpKey(op.Hash)
		if err := batch.Set(key, data, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set userop: %w", err)
		}

		indexValue := op.Hash.Bytes()

		// 2. Create sender index
		senderKey := UserOpSenderIndexKey(op.Sender, op.BlockNumber, op.BundleIndex)
		if err := batch.Set(senderKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set sender index: %w", err)
		}

		// 3. Create bundler index
		bundlerKey := UserOpBundlerIndexKey(op.Bundler, op.BlockNumber, op.BundleIndex)
		if err := batch.Set(bundlerKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set bundler index: %w", err)
		}

		// 4. Create block index
		blockKey := UserOpBlockIndexKey(op.BlockNumber, op.BundleIndex)
		if err := batch.Set(blockKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set block index: %w", err)
		}

		// 5. Create tx index
		txKey := UserOpTxIndexKey(op.TransactionHash, op.BundleIndex)
		if err := batch.Set(txKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set tx index: %w", err)
		}

		// 6. Create paymaster index
		if op.Paymaster != nil && *op.Paymaster != (common.Address{}) {
			pmKey := UserOpPaymasterIndexKey(*op.Paymaster, op.BlockNumber, op.BundleIndex)
			if err := batch.Set(pmKey, indexValue, pebble.Sync); err != nil {
				return fmt.Errorf("failed to set paymaster index: %w", err)
			}
		}

		// 7. Create factory index
		if op.Factory != nil && *op.Factory != (common.Address{}) {
			factoryKey := UserOpFactoryIndexKey(*op.Factory, op.BlockNumber, op.BundleIndex)
			if err := batch.Set(factoryKey, indexValue, pebble.Sync); err != nil {
				return fmt.Errorf("failed to set factory index: %w", err)
			}
		}
	}

	// Commit batch
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit userop batch: %w", err)
	}

	s.logger.Debug("saved userop batch",
		zap.Int("count", len(ops)))

	return nil
}

// UpdateBundlerStats updates statistics for a bundler address.
func (s *PebbleStorage) UpdateBundlerStats(ctx context.Context, stats *userop.BundlerStats) error {
	if s.closed.Load() {
		return ErrClosed
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal bundler stats: %w", err)
	}

	key := BundlerStatsKey(stats.Address)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set bundler stats: %w", err)
	}

	s.logger.Debug("updated bundler stats",
		zap.String("address", stats.Address.Hex()),
		zap.Uint64("totalBundles", stats.TotalBundles),
		zap.Uint64("totalOps", stats.TotalOps))

	return nil
}

// UpdateFactoryStats updates statistics for a factory address.
func (s *PebbleStorage) UpdateFactoryStats(ctx context.Context, stats *userop.FactoryStats) error {
	if s.closed.Load() {
		return ErrClosed
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal factory stats: %w", err)
	}

	key := FactoryStatsKey(stats.Address)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set factory stats: %w", err)
	}

	s.logger.Debug("updated factory stats",
		zap.String("address", stats.Address.Hex()),
		zap.Uint64("totalAccounts", stats.TotalAccounts))

	return nil
}

// UpdatePaymasterStats updates statistics for a paymaster address.
func (s *PebbleStorage) UpdatePaymasterStats(ctx context.Context, stats *userop.PaymasterStats) error {
	if s.closed.Load() {
		return ErrClosed
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal paymaster stats: %w", err)
	}

	key := PaymasterStatsKey(stats.Address)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set paymaster stats: %w", err)
	}

	s.logger.Debug("updated paymaster stats",
		zap.String("address", stats.Address.Hex()),
		zap.Uint64("totalOps", stats.TotalOps))

	return nil
}

// SaveSmartAccount saves or updates a smart account record.
func (s *PebbleStorage) SaveSmartAccount(ctx context.Context, account *userop.SmartAccount) error {
	if s.closed.Load() {
		return ErrClosed
	}

	data, err := json.Marshal(account)
	if err != nil {
		return fmt.Errorf("failed to marshal smart account: %w", err)
	}

	key := SmartAccountKey(account.Address)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set smart account: %w", err)
	}

	s.logger.Debug("saved smart account",
		zap.String("address", account.Address.Hex()),
		zap.Uint64("totalOps", account.TotalOps))

	return nil
}

// ========== Internal Helpers ==========

// normalizePagination applies default limits and bounds to pagination parameters
func normalizePagination(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// getUserOpsByIndex retrieves all UserOps referenced by an index prefix (no pagination)
func (s *PebbleStorage) getUserOpsByIndex(ctx context.Context, prefix []byte) ([]*userop.UserOperation, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var ops []*userop.UserOperation
	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if len(value) >= 32 {
			opHash := common.BytesToHash(value[:32])
			op, err := s.GetUserOp(ctx, opHash)
			if err != nil {
				s.logger.Warn("failed to get userop from index",
					zap.String("opHash", opHash.Hex()),
					zap.Error(err))
				continue
			}
			ops = append(ops, op)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return ops, nil
}

// getUserOpsByIndexPaginated retrieves UserOps from an index with reverse iteration and pagination
func (s *PebbleStorage) getUserOpsByIndexPaginated(ctx context.Context, prefix []byte, limit, offset int) ([]*userop.UserOperation, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Collect all op hashes (reverse order for newest first)
	var opHashes []common.Hash
	for iter.Last(); iter.Valid(); iter.Prev() {
		value := iter.Value()
		if len(value) >= 32 {
			opHashes = append(opHashes, common.BytesToHash(value[:32]))
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	// Apply pagination
	start := offset
	if start >= len(opHashes) {
		return []*userop.UserOperation{}, nil
	}
	end := start + limit
	if end > len(opHashes) {
		end = len(opHashes)
	}

	// Fetch full records
	ops := make([]*userop.UserOperation, 0, end-start)
	for _, opHash := range opHashes[start:end] {
		op, err := s.GetUserOp(ctx, opHash)
		if err != nil {
			s.logger.Warn("failed to get userop",
				zap.String("opHash", opHash.Hex()),
				zap.Error(err))
			continue
		}
		ops = append(ops, op)
	}

	return ops, nil
}
