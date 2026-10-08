package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.ModuleIndexReader = (*Store)(nil)
	_ port.ModuleIndexWriter = (*Store)(nil)
)

// GetInstalledModule implements port.ModuleIndexReader.
func (s *Store) GetInstalledModule(ctx context.Context, account, module common.Address) (*port.InstalledModule, error) {
	return getJSON[port.InstalledModule](ctx, s.q(ctx),
		"SELECT data FROM installed_modules WHERE account = $1 AND module = $2", account.Bytes(), module.Bytes())
}

// GetModulesByAccount implements port.ModuleIndexReader: installs of the
// same block by module address, descending.
func (s *Store) GetModulesByAccount(ctx context.Context, account common.Address, page port.Page) ([]*port.InstalledModule, string, error) {
	return listQuery[*port.InstalledModule]{
		list: "modules-account:" + account.Hex(),
		sql:  "SELECT data FROM installed_modules WHERE account = $1",
		args: []any{account.Bytes()},
		keys: []keyCol{{"installed_at", kindInt, true}, {"module", kindBytes, true}},
		scan: scanJSON[port.InstalledModule],
		keyOf: func(m *port.InstalledModule) []string {
			return []string{u64s(m.InstalledAt), hexOf(m.Module.Bytes())}
		},
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// GetModulesByType implements port.ModuleIndexReader: installs of the same
// block by account, then module address, descending.
func (s *Store) GetModulesByType(ctx context.Context, moduleType port.ModuleType, page port.Page) ([]*port.InstalledModule, string, error) {
	return listQuery[*port.InstalledModule]{
		list: "modules-type:" + moduleType.String(),
		sql:  "SELECT data FROM installed_modules WHERE module_type = $1",
		args: []any{int16(moduleType)},
		keys: []keyCol{{"installed_at", kindInt, true}, {"account", kindBytes, true}, {"module", kindBytes, true}},
		scan: scanJSON[port.InstalledModule],
		keyOf: func(m *port.InstalledModule) []string {
			return []string{u64s(m.InstalledAt), hexOf(m.Account.Bytes()), hexOf(m.Module.Bytes())}
		},
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// GetModuleStats implements port.ModuleIndexReader.
func (s *Store) GetModuleStats(ctx context.Context, module common.Address) (*port.ModuleStats, error) {
	st, err := getJSON[port.ModuleStats](ctx, s.q(ctx), "SELECT data FROM module_stats WHERE module = $1", module.Bytes())
	if errors.Is(err, port.ErrNotFound) {
		return &port.ModuleStats{Module: module}, nil
	}
	return st, err
}

// GetAccountModules implements port.ModuleIndexReader: each group in module
// address order.
func (s *Store) GetAccountModules(ctx context.Context, account common.Address) (*port.AccountModules, error) {
	records, err := queryJSON[port.InstalledModule](ctx, s.q(ctx),
		"SELECT data FROM installed_modules WHERE account = $1 ORDER BY module", account.Bytes())
	if err != nil {
		return nil, err
	}
	out := &port.AccountModules{
		Account:    account,
		Validators: []port.InstalledModule{},
		Executors:  []port.InstalledModule{},
		Fallbacks:  []port.InstalledModule{},
		Hooks:      []port.InstalledModule{},
	}
	for _, r := range records {
		switch r.ModuleType {
		case port.ModuleTypeValidator:
			out.Validators = append(out.Validators, *r)
		case port.ModuleTypeExecutor:
			out.Executors = append(out.Executors, *r)
		case port.ModuleTypeFallback:
			out.Fallbacks = append(out.Fallbacks, *r)
		case port.ModuleTypeHook:
			out.Hooks = append(out.Hooks, *r)
		}
	}
	return out, nil
}

// GetRecentModuleEvents implements port.ModuleIndexReader.
func (s *Store) GetRecentModuleEvents(ctx context.Context, limit int) ([]*port.InstalledModule, error) {
	return queryJSON[port.InstalledModule](ctx, s.q(ctx),
		"SELECT data FROM installed_modules ORDER BY installed_at DESC, account DESC, module DESC LIMIT $1", recentLimit(limit))
}

// GetModuleEventCount implements port.ModuleIndexReader.
func (s *Store) GetModuleEventCount(ctx context.Context) (int, error) {
	return s.count(ctx, "SELECT count(*) FROM installed_modules")
}

// ListModuleStats implements port.ModuleIndexReader: in module address
// order.
func (s *Store) ListModuleStats(ctx context.Context, page port.Page) ([]*port.ModuleStats, string, error) {
	return listQuery[*port.ModuleStats]{
		list: "module-stats",
		sql:  "SELECT data FROM module_stats WHERE true",
		keys: []keyCol{{"module", kindBytes, false}},
		scan: scanJSON[port.ModuleStats],
		keyOf: func(st *port.ModuleStats) []string {
			return []string{hexOf(st.Module.Bytes())}
		},
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// SaveInstalledModule implements port.ModuleIndexWriter: a reinstall
// replaces the account's record of the module.
func (s *Store) SaveInstalledModule(ctx context.Context, record *port.InstalledModule) error {
	if err := s.write(); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal installed module: %w", err)
	}
	_, err = s.q(ctx).Exec(ctx, `INSERT INTO installed_modules (account, module, module_type, installed_at, data)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (account, module) DO UPDATE SET module_type = EXCLUDED.module_type,
			installed_at = EXCLUDED.installed_at, data = EXCLUDED.data`,
		record.Account.Bytes(), record.Module.Bytes(), int16(record.ModuleType), i64(record.InstalledAt), data)
	return err
}

// RemoveModule implements port.ModuleIndexWriter: the record stays, marked
// inactive with the block and transaction of the removal.
func (s *Store) RemoveModule(ctx context.Context, account, module common.Address, blockNumber uint64, txHash common.Hash) error {
	return s.inTx(ctx, func(q querier) error {
		record, err := getJSON[port.InstalledModule](ctx, q,
			"SELECT data FROM installed_modules WHERE account = $1 AND module = $2 FOR UPDATE", account.Bytes(), module.Bytes())
		if err != nil {
			return fmt.Errorf("get installed module for removal: %w", err)
		}
		record.Active = false
		record.RemovedAt = &blockNumber
		record.RemovedTx = &txHash
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal installed module: %w", err)
		}
		_, err = q.Exec(ctx, "UPDATE installed_modules SET data = $3 WHERE account = $1 AND module = $2",
			account.Bytes(), module.Bytes(), data)
		return err
	})
}

// UpdateModuleStats implements port.ModuleIndexWriter: the given totals
// replace the stored ones.
func (s *Store) UpdateModuleStats(ctx context.Context, stats *port.ModuleStats) error {
	if err := s.write(); err != nil {
		return err
	}
	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("marshal module stats: %w", err)
	}
	_, err = s.q(ctx).Exec(ctx, `INSERT INTO module_stats (module, data) VALUES ($1, $2)
		ON CONFLICT (module) DO UPDATE SET data = EXCLUDED.data`, stats.Module.Bytes(), data)
	return err
}
