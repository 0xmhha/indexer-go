package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
)

// Compile-time check to ensure PebbleStorage implements UserOp interfaces
var _ UserOpIndexReader = (*PebbleStorage)(nil)
var _ UserOpIndexWriter = (*PebbleStorage)(nil)

// ========== UserOp Read Operations ==========

// GetUserOp retrieves a UserOperation by its hash.
func (s *PebbleStorage) GetUserOp(ctx context.Context, userOpHash common.Hash) (*UserOperationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := AAUserOpKey(userOpHash)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get userop: %w", err)
	}
	defer closer.Close()

	var record UserOperationRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal userop: %w", err)
	}

	return &record, nil
}

// GetUserOpsByTx retrieves all UserOperations in a transaction.
func (s *PebbleStorage) GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*UserOperationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	prefix := AATxIndexPrefix(txHash)
	return s.getUserOpsByIndex(prefix)
}

// GetUserOpsBySender retrieves UserOperations by sender address.
func (s *PebbleStorage) GetUserOpsBySender(ctx context.Context, sender common.Address, limit, offset int) ([]*UserOperationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := AASenderIndexPrefix(sender)
	return s.getUserOpsByIndexPaginated(prefix, limit, offset)
}

// GetUserOpsByBundler retrieves UserOperations by bundler address.
func (s *PebbleStorage) GetUserOpsByBundler(ctx context.Context, bundler common.Address, limit, offset int) ([]*UserOperationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := AABundlerIndexPrefix(bundler)
	return s.getUserOpsByIndexPaginated(prefix, limit, offset)
}

// GetUserOpsByPaymaster retrieves UserOperations by paymaster address.
func (s *PebbleStorage) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, limit, offset int) ([]*UserOperationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := AAPaymasterIndexPrefix(paymaster)
	return s.getUserOpsByIndexPaginated(prefix, limit, offset)
}

// GetUserOpsByBlock retrieves all UserOperations in a specific block.
func (s *PebbleStorage) GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*UserOperationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	prefix := AABlockIndexPrefix(blockNumber)
	return s.getUserOpsByIndex(prefix)
}

// GetUserOpsByEntryPoint retrieves UserOperations by EntryPoint address.
func (s *PebbleStorage) GetUserOpsByEntryPoint(ctx context.Context, entryPoint common.Address, limit, offset int) ([]*UserOperationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := AAEntryPointIndexPrefix(entryPoint)
	return s.getUserOpsByIndexPaginated(prefix, limit, offset)
}

// GetAccountDeployment retrieves an account deployment record by userOpHash.
func (s *PebbleStorage) GetAccountDeployment(ctx context.Context, userOpHash common.Hash) (*AccountDeployedRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := AADeployKey(userOpHash)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get account deployment: %w", err)
	}
	defer closer.Close()

	var record AccountDeployedRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account deployment: %w", err)
	}

	return &record, nil
}

// GetAccountDeploymentsByFactory retrieves deployments by factory address.
func (s *PebbleStorage) GetAccountDeploymentsByFactory(ctx context.Context, factory common.Address, limit, offset int) ([]*AccountDeployedRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, offset = normalizePagination(limit, offset)
	prefix := AAFactoryIndexPrefix(factory)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var records []*AccountDeployedRecord
	skipped := 0
	collected := 0

	for iter.First(); iter.Valid() && collected < limit; iter.Next() {
		if skipped < offset {
			skipped++
			continue
		}

		userOpHash := common.BytesToHash(iter.Value())
		record, err := s.GetAccountDeployment(ctx, userOpHash)
		if err != nil {
			s.logger.Warn("failed to get account deployment from index",
				zap.String("userOpHash", userOpHash.Hex()),
				zap.Error(err))
			continue
		}
		records = append(records, record)
		collected++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return records, nil
}

// GetUserOpRevert retrieves a revert reason by userOpHash.
func (s *PebbleStorage) GetUserOpRevert(ctx context.Context, userOpHash common.Hash) (*UserOpRevertRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := AARevertKey(userOpHash)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get userop revert: %w", err)
	}
	defer closer.Close()

	var record UserOpRevertRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal userop revert: %w", err)
	}

	return &record, nil
}

// GetBundlerStats retrieves aggregated statistics for a bundler.
func (s *PebbleStorage) GetBundlerStats(ctx context.Context, bundler common.Address) (*BundlerStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := AABundlerStatsKey(bundler)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return &BundlerStats{
				Address:           bundler,
				TotalGasSponsored: new(big.Int),
			}, nil
		}
		return nil, fmt.Errorf("failed to get bundler stats: %w", err)
	}
	defer closer.Close()

	var stats BundlerStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bundler stats: %w", err)
	}

	return &stats, nil
}

// GetPaymasterStats retrieves aggregated statistics for a paymaster.
func (s *PebbleStorage) GetPaymasterStats(ctx context.Context, paymaster common.Address) (*PaymasterStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := AAPaymasterStatsKey(paymaster)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return &PaymasterStats{
				Address:           paymaster,
				TotalGasSponsored: new(big.Int),
			}, nil
		}
		return nil, fmt.Errorf("failed to get paymaster stats: %w", err)
	}
	defer closer.Close()

	var stats PaymasterStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal paymaster stats: %w", err)
	}

	return &stats, nil
}

// GetUserOpCount returns the total count of UserOperations indexed.
func (s *PebbleStorage) GetUserOpCount(ctx context.Context) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := AAUserOpAllPrefix()
	return s.countByPrefix(prefix)
}

// GetUserOpsCountBySender returns the count of UserOperations for a sender.
func (s *PebbleStorage) GetUserOpsCountBySender(ctx context.Context, sender common.Address) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := AASenderIndexPrefix(sender)
	return s.countByPrefix(prefix)
}

// GetUserOpsCountByBundler returns the count of UserOperations for a bundler.
func (s *PebbleStorage) GetUserOpsCountByBundler(ctx context.Context, bundler common.Address) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := AABundlerIndexPrefix(bundler)
	return s.countByPrefix(prefix)
}

// GetUserOpsCountByPaymaster returns the count of UserOperations for a paymaster.
func (s *PebbleStorage) GetUserOpsCountByPaymaster(ctx context.Context, paymaster common.Address) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := AAPaymasterIndexPrefix(paymaster)
	return s.countByPrefix(prefix)
}

// GetRecentUserOps retrieves the most recent UserOperations.
func (s *PebbleStorage) GetRecentUserOps(ctx context.Context, limit int) ([]*UserOperationRecord, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	limit, _ = normalizePagination(limit, 0)
	prefix := AARecentIndexAllPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var records []*UserOperationRecord
	collected := 0

	// Keys are already sorted newest-first (inverted block number)
	for iter.First(); iter.Valid() && collected < limit; iter.Next() {
		userOpHash := common.BytesToHash(iter.Value())
		record, err := s.GetUserOp(ctx, userOpHash)
		if err != nil {
			s.logger.Warn("failed to get userop from recent index",
				zap.String("userOpHash", userOpHash.Hex()),
				zap.Error(err))
			continue
		}
		records = append(records, record)
		collected++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return records, nil
}

// ========== UserOp Write Operations ==========

// SaveUserOp saves a UserOperation record with all necessary indexes.
func (s *PebbleStorage) SaveUserOp(ctx context.Context, record *UserOperationRecord) error {
	if s.closed.Load() {
		return ErrClosed
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal userop: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	if err := s.writeUserOpToBatch(batch, record, data); err != nil {
		return err
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit userop: %w", err)
	}

	s.logger.Debug("saved userop",
		zap.String("userOpHash", record.UserOpHash.Hex()),
		zap.String("sender", record.Sender.Hex()),
		zap.Bool("success", record.Success))

	return nil
}

// SaveUserOps saves multiple UserOperation records in a batch.
func (s *PebbleStorage) SaveUserOps(ctx context.Context, records []*UserOperationRecord) error {
	if s.closed.Load() {
		return ErrClosed
	}

	if len(records) == 0 {
		return nil
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("failed to marshal userop: %w", err)
		}

		if err := s.writeUserOpToBatch(batch, record, data); err != nil {
			return err
		}
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit userops batch: %w", err)
	}

	s.logger.Debug("saved userops batch",
		zap.Int("count", len(records)))

	return nil
}

// SaveAccountDeployed saves an account deployment record.
func (s *PebbleStorage) SaveAccountDeployed(ctx context.Context, record *AccountDeployedRecord) error {
	if s.closed.Load() {
		return ErrClosed
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal account deployment: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// 1. Save the primary record keyed by userOpHash
	key := AADeployKey(record.UserOpHash)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set account deployment: %w", err)
	}

	// 2. Create factory index
	indexValue := record.UserOpHash.Bytes()
	factoryKey := AAFactoryIndexKey(record.Factory, record.BlockNumber, record.LogIndex)
	if err := batch.Set(factoryKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set factory index: %w", err)
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit account deployment: %w", err)
	}

	s.logger.Debug("saved account deployment",
		zap.String("userOpHash", record.UserOpHash.Hex()),
		zap.String("sender", record.Sender.Hex()),
		zap.String("factory", record.Factory.Hex()))

	return nil
}

// SaveUserOpRevert saves a UserOperation revert reason record.
func (s *PebbleStorage) SaveUserOpRevert(ctx context.Context, record *UserOpRevertRecord) error {
	if s.closed.Load() {
		return ErrClosed
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal userop revert: %w", err)
	}

	key := AARevertKey(record.UserOpHash)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set userop revert: %w", err)
	}

	s.logger.Debug("saved userop revert",
		zap.String("userOpHash", record.UserOpHash.Hex()),
		zap.String("sender", record.Sender.Hex()),
		zap.String("revertType", record.RevertType))

	return nil
}

// IncrementBundlerStats increments bundler statistics.
func (s *PebbleStorage) IncrementBundlerStats(ctx context.Context, bundler common.Address, success bool, gasCost *big.Int, blockNumber uint64) error {
	if s.closed.Load() {
		return ErrClosed
	}

	stats, err := s.GetBundlerStats(ctx, bundler)
	if err != nil {
		return fmt.Errorf("failed to get bundler stats: %w", err)
	}

	stats.TotalOps++
	if success {
		stats.SuccessfulOps++
	} else {
		stats.FailedOps++
	}
	if gasCost != nil && stats.TotalGasSponsored != nil {
		stats.TotalGasSponsored = new(big.Int).Add(stats.TotalGasSponsored, gasCost)
	}
	stats.LastActivityBlock = blockNumber
	stats.LastActivityTime = time.Now()

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal bundler stats: %w", err)
	}

	key := AABundlerStatsKey(bundler)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set bundler stats: %w", err)
	}

	return nil
}

// IncrementPaymasterStats increments paymaster statistics.
func (s *PebbleStorage) IncrementPaymasterStats(ctx context.Context, paymaster common.Address, success bool, gasCost *big.Int, blockNumber uint64) error {
	if s.closed.Load() {
		return ErrClosed
	}

	stats, err := s.GetPaymasterStats(ctx, paymaster)
	if err != nil {
		return fmt.Errorf("failed to get paymaster stats: %w", err)
	}

	stats.TotalOps++
	if success {
		stats.SuccessfulOps++
	} else {
		stats.FailedOps++
	}
	if gasCost != nil && stats.TotalGasSponsored != nil {
		stats.TotalGasSponsored = new(big.Int).Add(stats.TotalGasSponsored, gasCost)
	}
	stats.LastActivityBlock = blockNumber
	stats.LastActivityTime = time.Now()

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal paymaster stats: %w", err)
	}

	key := AAPaymasterStatsKey(paymaster)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set paymaster stats: %w", err)
	}

	return nil
}

// ========== Internal Helper Methods ==========

// writeUserOpToBatch writes a UserOp record and all its indexes to a batch.
func (s *PebbleStorage) writeUserOpToBatch(batch *pebble.Batch, record *UserOperationRecord, data []byte) error {
	indexValue := record.UserOpHash.Bytes()

	// 1. Primary record
	key := AAUserOpKey(record.UserOpHash)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set userop: %w", err)
	}

	// 2. Sender index
	senderKey := AASenderIndexKey(record.Sender, record.BlockNumber, record.LogIndex)
	if err := batch.Set(senderKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set sender index: %w", err)
	}

	// 3. Bundler index
	bundlerKey := AABundlerIndexKey(record.Bundler, record.BlockNumber, record.LogIndex)
	if err := batch.Set(bundlerKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set bundler index: %w", err)
	}

	// 4. Paymaster index (only if paymaster is set)
	if record.Paymaster != (common.Address{}) {
		paymasterKey := AAPaymasterIndexKey(record.Paymaster, record.BlockNumber, record.LogIndex)
		if err := batch.Set(paymasterKey, indexValue, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set paymaster index: %w", err)
		}
	}

	// 5. Block index
	blockKey := AABlockIndexKey(record.BlockNumber, record.LogIndex)
	if err := batch.Set(blockKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set block index: %w", err)
	}

	// 6. EntryPoint index
	entryPointKey := AAEntryPointIndexKey(record.EntryPoint, record.BlockNumber, record.LogIndex)
	if err := batch.Set(entryPointKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set entrypoint index: %w", err)
	}

	// 7. Tx index
	txKey := AATxIndexKey(record.TxHash, record.LogIndex)
	if err := batch.Set(txKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set tx index: %w", err)
	}

	// 8. Recent index (newest first via inverted block number)
	recentKey := AARecentIndexKey(record.BlockNumber, record.LogIndex)
	if err := batch.Set(recentKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set recent index: %w", err)
	}

	return nil
}

// getUserOpsByIndex retrieves all UserOps pointed to by an index prefix (no pagination).
func (s *PebbleStorage) getUserOpsByIndex(prefix []byte) ([]*UserOperationRecord, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var records []*UserOperationRecord
	for iter.First(); iter.Valid(); iter.Next() {
		userOpHash := common.BytesToHash(iter.Value())
		record, err := s.GetUserOp(context.Background(), userOpHash)
		if err != nil {
			s.logger.Warn("failed to get userop from index",
				zap.String("userOpHash", userOpHash.Hex()),
				zap.Error(err))
			continue
		}
		records = append(records, record)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return records, nil
}

// getUserOpsByIndexPaginated retrieves UserOps with pagination from an index.
// Index keys are already sorted (newest first via inverted block numbers).
func (s *PebbleStorage) getUserOpsByIndexPaginated(prefix []byte, limit, offset int) ([]*UserOperationRecord, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var records []*UserOperationRecord
	skipped := 0
	collected := 0

	for iter.First(); iter.Valid() && collected < limit; iter.Next() {
		if skipped < offset {
			skipped++
			continue
		}

		userOpHash := common.BytesToHash(iter.Value())
		record, err := s.GetUserOp(context.Background(), userOpHash)
		if err != nil {
			s.logger.Warn("failed to get userop from index",
				zap.String("userOpHash", userOpHash.Hex()),
				zap.Error(err))
			continue
		}
		records = append(records, record)
		collected++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return records, nil
}

// countByPrefix counts entries under a prefix.
func (s *PebbleStorage) countByPrefix(prefix []byte) (int, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
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

// normalizePagination applies default limits and bounds.
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
