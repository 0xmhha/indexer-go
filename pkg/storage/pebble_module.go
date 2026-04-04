package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
)

// Compile-time check to ensure PebbleStorage implements Module interfaces
var _ ModuleIndexReader = (*PebbleStorage)(nil)
var _ ModuleIndexWriter = (*PebbleStorage)(nil)

// ========== Module Read Operations ==========

// GetInstalledModule retrieves a specific installed module by account and module address.
func (s *PebbleStorage) GetInstalledModule(ctx context.Context, account, module common.Address) (*InstalledModule, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := ModuleKey(account, module)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get installed module: %w", err)
	}
	defer closer.Close()

	var record InstalledModule
	if err := json.Unmarshal(value, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal installed module: %w", err)
	}

	return &record, nil
}

// GetModulesByAccount retrieves all modules installed on a specific account.
// Results are ordered by block number descending (newest first).
func (s *PebbleStorage) GetModulesByAccount(ctx context.Context, account common.Address, limit, offset int) ([]*InstalledModule, error) {
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

	prefix := ModuleAccountIndexKeyPrefix(account)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Collect all module references (reverse order for newest first)
	type moduleRef struct {
		account common.Address
		module  common.Address
	}
	var refs []moduleRef
	for iter.Last(); iter.Valid(); iter.Prev() {
		value := iter.Value()
		if len(value) >= 40 {
			var ref moduleRef
			ref.account = common.BytesToAddress(value[:20])
			ref.module = common.BytesToAddress(value[20:40])
			refs = append(refs, ref)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	// Apply pagination
	start := offset
	if start >= len(refs) {
		return []*InstalledModule{}, nil
	}
	end := start + limit
	if end > len(refs) {
		end = len(refs)
	}

	// Fetch full records
	records := make([]*InstalledModule, 0, end-start)
	for _, ref := range refs[start:end] {
		record, err := s.GetInstalledModule(ctx, ref.account, ref.module)
		if err != nil {
			s.logger.Warn("failed to get installed module",
				zap.String("account", ref.account.Hex()),
				zap.String("module", ref.module.Hex()),
				zap.Error(err))
			continue
		}
		records = append(records, record)
	}

	return records, nil
}

// GetModulesByType retrieves modules by their type across all accounts.
// Results are ordered by block number descending (newest first).
func (s *PebbleStorage) GetModulesByType(ctx context.Context, moduleType ModuleType, limit, offset int) ([]*InstalledModule, error) {
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

	prefix := ModuleTypeIndexKeyPrefix(moduleType)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Collect all module references (reverse order for newest first)
	type moduleRef struct {
		account common.Address
		module  common.Address
	}
	var refs []moduleRef
	for iter.Last(); iter.Valid(); iter.Prev() {
		value := iter.Value()
		if len(value) >= 40 {
			var ref moduleRef
			ref.account = common.BytesToAddress(value[:20])
			ref.module = common.BytesToAddress(value[20:40])
			refs = append(refs, ref)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	// Apply pagination
	start := offset
	if start >= len(refs) {
		return []*InstalledModule{}, nil
	}
	end := start + limit
	if end > len(refs) {
		end = len(refs)
	}

	// Fetch full records
	records := make([]*InstalledModule, 0, end-start)
	for _, ref := range refs[start:end] {
		record, err := s.GetInstalledModule(ctx, ref.account, ref.module)
		if err != nil {
			s.logger.Warn("failed to get installed module",
				zap.String("account", ref.account.Hex()),
				zap.String("module", ref.module.Hex()),
				zap.Error(err))
			continue
		}
		records = append(records, record)
	}

	return records, nil
}

// GetModuleStats retrieves aggregate statistics for a module contract.
func (s *PebbleStorage) GetModuleStats(ctx context.Context, module common.Address) (*ModuleStats, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := ModuleStatsKey(module)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			// Return zero-value stats
			return &ModuleStats{
				Module: module,
			}, nil
		}
		return nil, fmt.Errorf("failed to get module stats: %w", err)
	}
	defer closer.Close()

	var stats ModuleStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal module stats: %w", err)
	}

	return &stats, nil
}

// GetAccountModules retrieves all modules for an account, grouped by type.
func (s *PebbleStorage) GetAccountModules(ctx context.Context, account common.Address) (*AccountModules, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	result := &AccountModules{
		Account:    account,
		Validators: []InstalledModule{},
		Executors:  []InstalledModule{},
		Fallbacks:  []InstalledModule{},
		Hooks:      []InstalledModule{},
	}

	// Get all modules for this account from primary storage
	prefix := ModuleKeyPrefix(account)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var record InstalledModule
		if err := json.Unmarshal(iter.Value(), &record); err != nil {
			s.logger.Warn("failed to unmarshal installed module",
				zap.String("key", string(iter.Key())),
				zap.Error(err))
			continue
		}

		switch record.ModuleType {
		case ModuleTypeValidator:
			result.Validators = append(result.Validators, record)
		case ModuleTypeExecutor:
			result.Executors = append(result.Executors, record)
		case ModuleTypeFallback:
			result.Fallbacks = append(result.Fallbacks, record)
		case ModuleTypeHook:
			result.Hooks = append(result.Hooks, record)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return result, nil
}

// GetRecentModuleEvents retrieves the most recent module install/uninstall events.
func (s *PebbleStorage) GetRecentModuleEvents(ctx context.Context, limit int) ([]*InstalledModule, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}

	prefix := ModuleBlockIndexAllPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var records []*InstalledModule
	count := 0

	// Iterate in reverse order (newest first)
	for iter.Last(); iter.Valid() && count < limit; iter.Prev() {
		value := iter.Value()
		if len(value) >= 40 {
			account := common.BytesToAddress(value[:20])
			module := common.BytesToAddress(value[20:40])

			record, err := s.GetInstalledModule(ctx, account, module)
			if err != nil {
				s.logger.Warn("failed to get installed module",
					zap.String("account", account.Hex()),
					zap.String("module", module.Hex()),
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

// GetModuleEventCount returns the total count of module events indexed.
func (s *PebbleStorage) GetModuleEventCount(ctx context.Context) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := ModuleBlockIndexAllPrefix()

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

// ListModuleStats retrieves module stats with pagination.
func (s *PebbleStorage) ListModuleStats(ctx context.Context, limit, offset int) ([]*ModuleStats, error) {
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

	prefix := ModuleStatsKeyPrefix()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var allStats []*ModuleStats
	for iter.First(); iter.Valid(); iter.Next() {
		var stats ModuleStats
		if err := json.Unmarshal(iter.Value(), &stats); err != nil {
			s.logger.Warn("failed to unmarshal module stats",
				zap.String("key", string(iter.Key())),
				zap.Error(err))
			continue
		}
		allStats = append(allStats, &stats)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	// Apply pagination
	start := offset
	if start >= len(allStats) {
		return []*ModuleStats{}, nil
	}
	end := start + limit
	if end > len(allStats) {
		end = len(allStats)
	}

	return allStats[start:end], nil
}

// ========== Module Write Operations ==========

// SaveInstalledModule saves a module installation record.
func (s *PebbleStorage) SaveInstalledModule(ctx context.Context, record *InstalledModule) error {
	if s.closed.Load() {
		return ErrClosed
	}

	// Marshal record
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal installed module: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// 1. Save the primary record
	key := ModuleKey(record.Account, record.Module)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set installed module: %w", err)
	}

	// Create index value: account (20 bytes) + module (20 bytes)
	indexValue := make([]byte, 40)
	copy(indexValue[:20], record.Account.Bytes())
	copy(indexValue[20:40], record.Module.Bytes())

	// 2. Create account index
	accountKey := ModuleAccountIndexKey(record.Account, record.InstalledAt, record.Module)
	if err := batch.Set(accountKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set account index: %w", err)
	}

	// 3. Create type index
	typeKey := ModuleTypeIndexKey(record.ModuleType, record.InstalledAt, record.Account, record.Module)
	if err := batch.Set(typeKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set type index: %w", err)
	}

	// 4. Create block index
	blockKey := ModuleBlockIndexKey(record.InstalledAt, record.Account, record.Module)
	if err := batch.Set(blockKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set block index: %w", err)
	}

	// Commit batch
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit installed module: %w", err)
	}

	s.logger.Debug("saved installed module",
		zap.String("account", record.Account.Hex()),
		zap.String("module", record.Module.Hex()),
		zap.String("moduleType", record.ModuleType.String()),
		zap.Uint64("installedAt", record.InstalledAt),
		zap.Bool("active", record.Active))

	return nil
}

// RemoveModule marks a module as uninstalled.
func (s *PebbleStorage) RemoveModule(ctx context.Context, account, module common.Address, blockNumber uint64, txHash common.Hash) error {
	if s.closed.Load() {
		return ErrClosed
	}

	// Load existing module record
	record, err := s.GetInstalledModule(ctx, account, module)
	if err != nil {
		return fmt.Errorf("failed to get installed module for removal: %w", err)
	}

	// Update record
	record.Active = false
	record.RemovedAt = &blockNumber
	record.RemovedTx = &txHash

	// Marshal updated record
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal updated module: %w", err)
	}

	// Save updated record
	key := ModuleKey(account, module)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to update installed module: %w", err)
	}

	s.logger.Debug("removed module",
		zap.String("account", account.Hex()),
		zap.String("module", module.Hex()),
		zap.Uint64("removedAt", blockNumber),
		zap.String("removedTx", txHash.Hex()))

	return nil
}

// UpdateModuleStats updates the aggregate stats for a module.
func (s *PebbleStorage) UpdateModuleStats(ctx context.Context, stats *ModuleStats) error {
	if s.closed.Load() {
		return ErrClosed
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal module stats: %w", err)
	}

	key := ModuleStatsKey(stats.Module)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set module stats: %w", err)
	}

	s.logger.Debug("updated module stats",
		zap.String("module", stats.Module.Hex()),
		zap.String("moduleType", stats.ModuleType.String()),
		zap.Uint64("totalInstalls", stats.TotalInstalls),
		zap.Uint64("activeInstalls", stats.ActiveInstalls))

	return nil
}
