package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Compile-time check to ensure PebbleStorage implements Module interfaces
var _ port.ModuleIndexReader = (*PebbleStorage)(nil)
var _ port.ModuleIndexWriter = (*PebbleStorage)(nil)

// ========== Module Read Operations ==========

// GetInstalledModule retrieves a specific installed module by account and module address.
func (s *PebbleStorage) GetInstalledModule(ctx context.Context, account, module common.Address) (*port.InstalledModule, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	key := ModuleKey(account, module)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get installed module: %w", err)
	}
	defer closer.Close()

	var record port.InstalledModule
	if err := json.Unmarshal(value, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal installed module: %w", err)
	}

	return &record, nil
}

// GetModulesByAccount returns one page of the modules installed on an
// account, newest install first.
func (s *PebbleStorage) GetModulesByAccount(ctx context.Context, account common.Address, page port.Page) ([]*port.InstalledModule, string, error) {
	return s.modulePage(ctx, ModuleAccountIndexKeyPrefix(account), page)
}

// GetModulesByType returns one page of the modules of a type across all
// accounts, newest install first.
func (s *PebbleStorage) GetModulesByType(ctx context.Context, moduleType port.ModuleType, page port.Page) ([]*port.InstalledModule, string, error) {
	return s.modulePage(ctx, ModuleTypeIndexKeyPrefix(moduleType), page)
}

// modulePage reads one page of a module index (keys ordered by install
// block) in reverse, so the newest install comes first, and loads the
// records it refers to. The cursor is the index key of the last entry.
func (s *PebbleStorage) modulePage(ctx context.Context, prefix []byte, page port.Page) ([]*port.InstalledModule, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	limit := min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
	isRef := func(_, value []byte) bool { return len(value) >= 2*common.AddressLength }
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), true, page, limit, isRef)
	if err != nil {
		return nil, "", err
	}

	records := make([]*port.InstalledModule, 0, len(entries))
	for _, e := range entries {
		account := common.BytesToAddress(e.Value[:common.AddressLength])
		module := common.BytesToAddress(e.Value[common.AddressLength : 2*common.AddressLength])
		record, err := s.GetInstalledModule(ctx, account, module)
		if err != nil {
			s.logger.Warn("failed to get installed module",
				zap.String("account", account.Hex()),
				zap.String("module", module.Hex()),
				zap.Error(err))
			continue
		}
		records = append(records, record)
	}
	return records, next, nil
}

// GetModuleStats retrieves aggregate statistics for a module contract.
func (s *PebbleStorage) GetModuleStats(ctx context.Context, module common.Address) (*port.ModuleStats, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	key := ModuleStatsKey(module)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			// Return zero-value stats
			return &port.ModuleStats{
				Module: module,
			}, nil
		}
		return nil, fmt.Errorf("failed to get module stats: %w", err)
	}
	defer closer.Close()

	var stats port.ModuleStats
	if err := json.Unmarshal(value, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal module stats: %w", err)
	}

	return &stats, nil
}

// GetAccountModules retrieves all modules for an account, grouped by type.
func (s *PebbleStorage) GetAccountModules(ctx context.Context, account common.Address) (*port.AccountModules, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	result := &port.AccountModules{
		Account:    account,
		Validators: []port.InstalledModule{},
		Executors:  []port.InstalledModule{},
		Fallbacks:  []port.InstalledModule{},
		Hooks:      []port.InstalledModule{},
	}

	// Get all modules for this account from primary storage
	prefix := ModuleKeyPrefix(account)

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var record port.InstalledModule
		if err := json.Unmarshal(iter.Value(), &record); err != nil {
			s.logger.Warn("failed to unmarshal installed module",
				zap.String("key", string(iter.Key())),
				zap.Error(err))
			continue
		}

		switch record.ModuleType {
		case port.ModuleTypeValidator:
			result.Validators = append(result.Validators, record)
		case port.ModuleTypeExecutor:
			result.Executors = append(result.Executors, record)
		case port.ModuleTypeFallback:
			result.Fallbacks = append(result.Fallbacks, record)
		case port.ModuleTypeHook:
			result.Hooks = append(result.Hooks, record)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return result, nil
}

// GetRecentModuleEvents retrieves the most recently installed modules (one
// install record per account and module; uninstalls add no entry).
func (s *PebbleStorage) GetRecentModuleEvents(ctx context.Context, limit int) ([]*port.InstalledModule, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}

	prefix := ModuleBlockIndexAllPrefix()

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var records []*port.InstalledModule
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

// GetModuleEventCount returns the total count of module install records indexed.
func (s *PebbleStorage) GetModuleEventCount(ctx context.Context) (int, error) {
	if s.closed.Load() {
		return 0, port.ErrClosed
	}

	prefix := ModuleBlockIndexAllPrefix()

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

// ListModuleStats returns one page of the module stats in key order.
// Entries that do not decode are skipped and are not items of the list.
func (s *PebbleStorage) ListModuleStats(ctx context.Context, page port.Page) ([]*port.ModuleStats, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	limit := min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
	prefix := ModuleStatsKeyPrefix()
	decodes := func(key, value []byte) bool {
		var stats port.ModuleStats
		if err := json.Unmarshal(value, &stats); err != nil {
			s.logger.Warn("failed to unmarshal module stats",
				zap.String("key", string(key)),
				zap.Error(err))
			return false
		}
		return true
	}
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), false, page, limit, decodes)
	if err != nil {
		return nil, "", err
	}

	statsList := make([]*port.ModuleStats, 0, len(entries))
	for _, e := range entries {
		var stats port.ModuleStats
		if err := json.Unmarshal(e.Value, &stats); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal module stats: %w", err)
		}
		statsList = append(statsList, &stats)
	}
	return statsList, next, nil
}

// ========== Module Write Operations ==========

// SaveInstalledModule saves a module installation record.
func (s *PebbleStorage) SaveInstalledModule(ctx context.Context, record *port.InstalledModule) error {
	if s.closed.Load() {
		return port.ErrClosed
	}

	// Marshal record
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal installed module: %w", err)
	}

	// A reinstall replaces the primary record; its index keys carry the
	// install block and module type, so drop the replaced record's entries.
	previous, err := s.GetInstalledModule(ctx, record.Account, record.Module)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return fmt.Errorf("failed to get replaced module: %w", err)
	}

	batch := s.newBatch(ctx)
	defer batch.Close()

	if previous != nil && (previous.InstalledAt != record.InstalledAt || previous.ModuleType != record.ModuleType) {
		for _, staleKey := range [][]byte{
			ModuleAccountIndexKey(previous.Account, previous.InstalledAt, previous.Module),
			ModuleTypeIndexKey(previous.ModuleType, previous.InstalledAt, previous.Account, previous.Module),
			ModuleBlockIndexKey(previous.InstalledAt, previous.Account, previous.Module),
		} {
			if err := batch.Delete(staleKey, pebble.Sync); err != nil {
				return fmt.Errorf("failed to delete replaced module index: %w", err)
			}
		}
	}

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
	if err := s.commitBatch(ctx, batch, pebble.Sync); err != nil {
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
		return port.ErrClosed
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
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
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
func (s *PebbleStorage) UpdateModuleStats(ctx context.Context, stats *port.ModuleStats) error {
	if s.closed.Load() {
		return port.ErrClosed
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal module stats: %w", err)
	}

	key := ModuleStatsKey(stats.Module)
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set module stats: %w", err)
	}

	s.logger.Debug("updated module stats",
		zap.String("module", stats.Module.Hex()),
		zap.String("moduleType", stats.ModuleType.String()),
		zap.Uint64("totalInstalls", stats.TotalInstalls),
		zap.Uint64("activeInstalls", stats.ActiveInstalls))

	return nil
}
