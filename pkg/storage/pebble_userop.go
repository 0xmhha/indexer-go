package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/userop"
)

// Compile-time check to ensure PebbleStorage implements UserOp interfaces
var _ port.UserOpIndexReader = (*PebbleStorage)(nil)
var _ port.UserOpIndexWriter = (*PebbleStorage)(nil)

// ========== UserOp Read Operations ==========

// GetUserOp retrieves a specific UserOperation by its hash.
func (s *PebbleStorage) GetUserOp(ctx context.Context, opHash common.Hash) (*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	key := UserOpKey(opHash)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
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
		return nil, port.ErrClosed
	}

	prefix := UserOpTxIndexKeyPrefix(txHash)
	return s.getUserOpsByIndex(ctx, prefix)
}

// GetUserOpsBySender returns one page of the UserOperations sent by a specific address,
// newest first.
func (s *PebbleStorage) GetUserOpsBySender(ctx context.Context, sender common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	return s.getUserOpsPage(ctx, UserOpSenderIndexKeyPrefix(sender), page)
}

// GetUserOpsByBundler returns one page of the UserOperations bundled by a specific address,
// newest first.
func (s *PebbleStorage) GetUserOpsByBundler(ctx context.Context, bundler common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	return s.getUserOpsPage(ctx, UserOpBundlerIndexKeyPrefix(bundler), page)
}

// GetUserOpsByBlock retrieves all UserOperations in a specific block.
func (s *PebbleStorage) GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*userop.UserOperation, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	prefix := UserOpBlockIndexKeyPrefix(blockNumber)
	return s.getUserOpsByIndex(ctx, prefix)
}

// GetUserOpsByPaymaster returns one page of the UserOperations sponsored by a specific paymaster,
// newest first.
func (s *PebbleStorage) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	return s.getUserOpsPage(ctx, UserOpPaymasterIndexKeyPrefix(paymaster), page)
}

// GetUserOpsByFactory returns one page of the UserOperations that deployed accounts via a specific factory,
// newest first.
func (s *PebbleStorage) GetUserOpsByFactory(ctx context.Context, factory common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	return s.getUserOpsPage(ctx, UserOpFactoryIndexKeyPrefix(factory), page)
}

// GetBundlerStats retrieves statistics for a bundler address.
func (s *PebbleStorage) GetBundlerStats(ctx context.Context, bundler common.Address) (*userop.BundlerStats, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	key := BundlerStatsKey(bundler)
	value, closer, err := s.kv(ctx).Get(key)
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
		return nil, port.ErrClosed
	}

	key := FactoryStatsKey(factory)
	value, closer, err := s.kv(ctx).Get(key)
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
		return nil, port.ErrClosed
	}

	key := PaymasterStatsKey(paymaster)
	value, closer, err := s.kv(ctx).Get(key)
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
		return nil, port.ErrClosed
	}

	key := SmartAccountKey(address)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
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
		return nil, port.ErrClosed
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}

	prefix := UserOpBlockIndexAllPrefix()

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
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
		return 0, port.ErrClosed
	}

	prefix := UserOpKeyPrefix()

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
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

// ListBundlers returns one page of bundler stats, in key order (by address).
func (s *PebbleStorage) ListBundlers(ctx context.Context, page port.Page) ([]*userop.BundlerStats, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	return pageRecords[userop.BundlerStats](ctx, s, BundlerStatsKeyPrefix(), page, "bundler stats")
}

// ListFactories returns one page of factory stats, in key order (by address).
func (s *PebbleStorage) ListFactories(ctx context.Context, page port.Page) ([]*userop.FactoryStats, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	return pageRecords[userop.FactoryStats](ctx, s, FactoryStatsKeyPrefix(), page, "factory stats")
}

// ListPaymasters returns one page of paymaster stats, in key order (by address).
func (s *PebbleStorage) ListPaymasters(ctx context.Context, page port.Page) ([]*userop.PaymasterStats, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	return pageRecords[userop.PaymasterStats](ctx, s, PaymasterStatsKeyPrefix(), page, "paymaster stats")
}

// ListSmartAccounts returns one page of smart account, in key order (by address).
func (s *PebbleStorage) ListSmartAccounts(ctx context.Context, page port.Page) ([]*userop.SmartAccount, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	return pageRecords[userop.SmartAccount](ctx, s, SmartAccountKeyPrefix(), page, "smart account")
}

// ========== UserOp Write Operations ==========

// SaveUserOp saves a UserOperation record.
func (s *PebbleStorage) SaveUserOp(ctx context.Context, op *userop.UserOperation) error {
	if s.closed.Load() {
		return port.ErrClosed
	}

	data, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal userop: %w", err)
	}

	batch := s.newBatch(ctx)
	defer batch.Close()

	// 1. Save the primary record
	key := UserOpKey(op.Hash)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set userop: %w", err)
	}

	// Index value: opHash (32 bytes)
	indexValue := op.Hash.Bytes()

	// 2. Create sender index
	senderKey := UserOpSenderIndexKey(op.Sender, op.BlockNumber, op.TransactionHash, op.BundleIndex)
	if err := batch.Set(senderKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set sender index: %w", err)
	}

	// 3. Create bundler index
	bundlerKey := UserOpBundlerIndexKey(op.Bundler, op.BlockNumber, op.TransactionHash, op.BundleIndex)
	if err := batch.Set(bundlerKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set bundler index: %w", err)
	}

	// 4. Create block index
	blockKey := UserOpBlockIndexKey(op.BlockNumber, op.TransactionHash, op.BundleIndex)
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
		pmKey := UserOpPaymasterIndexKey(*op.Paymaster, op.BlockNumber, op.TransactionHash, op.BundleIndex)
		if err := batch.Set(pmKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set paymaster index: %w", err)
		}
	}

	// 7. Create factory index (if factory exists)
	if op.Factory != nil && *op.Factory != (common.Address{}) {
		factoryKey := UserOpFactoryIndexKey(*op.Factory, op.BlockNumber, op.TransactionHash, op.BundleIndex)
		if err := batch.Set(factoryKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set factory index: %w", err)
		}
	}

	// Commit batch
	if err := s.commitBatch(ctx, batch, pebble.Sync); err != nil {
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
		return port.ErrClosed
	}

	if len(ops) == 0 {
		return nil
	}

	batch := s.newBatch(ctx)
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
		senderKey := UserOpSenderIndexKey(op.Sender, op.BlockNumber, op.TransactionHash, op.BundleIndex)
		if err := batch.Set(senderKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set sender index: %w", err)
		}

		// 3. Create bundler index
		bundlerKey := UserOpBundlerIndexKey(op.Bundler, op.BlockNumber, op.TransactionHash, op.BundleIndex)
		if err := batch.Set(bundlerKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set bundler index: %w", err)
		}

		// 4. Create block index
		blockKey := UserOpBlockIndexKey(op.BlockNumber, op.TransactionHash, op.BundleIndex)
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
			pmKey := UserOpPaymasterIndexKey(*op.Paymaster, op.BlockNumber, op.TransactionHash, op.BundleIndex)
			if err := batch.Set(pmKey, indexValue, pebble.Sync); err != nil {
				return fmt.Errorf("failed to set paymaster index: %w", err)
			}
		}

		// 7. Create factory index
		if op.Factory != nil && *op.Factory != (common.Address{}) {
			factoryKey := UserOpFactoryIndexKey(*op.Factory, op.BlockNumber, op.TransactionHash, op.BundleIndex)
			if err := batch.Set(factoryKey, indexValue, pebble.Sync); err != nil {
				return fmt.Errorf("failed to set factory index: %w", err)
			}
		}
	}

	// Commit batch
	if err := s.commitBatch(ctx, batch, pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit userop batch: %w", err)
	}

	s.logger.Debug("saved userop batch",
		zap.Int("count", len(ops)))

	return nil
}

// UpdateBundlerStats updates statistics for a bundler address.
func (s *PebbleStorage) UpdateBundlerStats(ctx context.Context, stats *userop.BundlerStats) error {
	if s.closed.Load() {
		return port.ErrClosed
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal bundler stats: %w", err)
	}

	key := BundlerStatsKey(stats.Address)
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
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
		return port.ErrClosed
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal factory stats: %w", err)
	}

	key := FactoryStatsKey(stats.Address)
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
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
		return port.ErrClosed
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal paymaster stats: %w", err)
	}

	key := PaymasterStatsKey(stats.Address)
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
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
		return port.ErrClosed
	}

	data, err := json.Marshal(account)
	if err != nil {
		return fmt.Errorf("failed to marshal smart account: %w", err)
	}

	key := SmartAccountKey(account.Address)
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set smart account: %w", err)
	}

	s.logger.Debug("saved smart account",
		zap.String("address", account.Address.Hex()),
		zap.Uint64("totalOps", account.TotalOps))

	return nil
}

// ========== Internal Helpers ==========

// getUserOpsByIndex retrieves all UserOps referenced by an index prefix (no pagination)
func (s *PebbleStorage) getUserOpsByIndex(ctx context.Context, prefix []byte) ([]*userop.UserOperation, error) {
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
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

// userOpPageLimit returns the number of items of a UserOperation list page:
// the default limit when page.Limit is not positive, at most the maximum.
func userOpPageLimit(page port.Page) int {
	return min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
}

// getUserOpsPage reads one page of a UserOperation index (sender, bundler,
// paymaster or factory) newest first. The index keys end in
// {blockNumber}/{txHash}/{bundleIndex}, so the reverse key order is the list
// order and the page cursor is the last entry's index key.
func (s *PebbleStorage) getUserOpsPage(ctx context.Context, prefix []byte, page port.Page) ([]*userop.UserOperation, string, error) {
	hasHash := func(_, value []byte) bool { return len(value) >= 32 }
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), true, page, userOpPageLimit(page), hasHash)
	if err != nil {
		return nil, "", err
	}

	ops := make([]*userop.UserOperation, 0, len(entries))
	for _, e := range entries {
		opHash := common.BytesToHash(e.Value[:32])
		op, err := s.GetUserOp(ctx, opHash)
		if err != nil {
			s.logger.Warn("failed to get userop",
				zap.String("opHash", opHash.Hex()),
				zap.Error(err))
			continue
		}
		ops = append(ops, op)
	}

	return ops, next, nil
}

// pageRecords reads one page of the JSON records stored under prefix, in key
// order; the page cursor is the last record's key. A record that does not
// decode is logged and left out.
func pageRecords[T any](ctx context.Context, s *PebbleStorage, prefix []byte, page port.Page, what string) ([]*T, string, error) {
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), false, page, userOpPageLimit(page), nil)
	if err != nil {
		return nil, "", err
	}

	out := make([]*T, 0, len(entries))
	for _, e := range entries {
		var record T
		if err := json.Unmarshal(e.Value, &record); err != nil {
			s.logger.Warn("failed to unmarshal "+what,
				zap.String("key", string(e.Key)),
				zap.Error(err))
			continue
		}
		out = append(out, &record)
	}

	return out, next, nil
}
