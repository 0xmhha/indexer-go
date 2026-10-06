package porttest

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Addresses of the module fixtures.
var (
	moduleAccount  = common.HexToAddress("0x0000000000000000000000000000000000005a01")
	moduleAccount2 = common.HexToAddress("0x0000000000000000000000000000000000005a02")
)

// moduleAddr returns the address of fixture module n.
func moduleAddr(n uint64) common.Address {
	return common.BigToAddress(new(big.Int).SetUint64(0x5b00 + n))
}

// testModuleIndex checks ModuleIndexReader and ModuleIndexWriter: an
// ERC-7579 module installed on an account is found by account and module,
// listed by account and by type newest first with pagination and grouped by
// type for the account; uninstalling keeps the record but marks it inactive
// with the block and transaction of the removal; module statistics are
// stored as given.
func testModuleIndex(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("NotFoundOnEmptyStore", func(t *testing.T) {
		s := open[moduleStore](t, newStore)
		_, err := s.GetInstalledModule(ctx, moduleAccount, moduleAddr(1))
		assert.ErrorIs(t, err, port.ErrNotFound)
		err = s.RemoveModule(ctx, moduleAccount, moduleAddr(1), 3, fixtureHash("module-tx", 3))
		assert.ErrorIs(t, err, port.ErrNotFound, "removing a module that was never installed")

		byAccount, err := s.GetModulesByAccount(ctx, moduleAccount, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, byAccount)
		byType, err := s.GetModulesByType(ctx, port.ModuleTypeValidator, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, byType)
		recent, err := s.GetRecentModuleEvents(ctx, 10)
		require.NoError(t, err)
		assert.Empty(t, recent)
		n, err := s.GetModuleEventCount(ctx)
		require.NoError(t, err)
		assert.Zero(t, n)
		list, err := s.ListModuleStats(ctx, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, list)

		stats, err := s.GetModuleStats(ctx, moduleAddr(1))
		require.NoError(t, err, "no activity is zero-value stats, not an error")
		require.NotNil(t, stats)
		assert.Equal(t, moduleAddr(1), stats.Module)
		assert.Zero(t, stats.TotalInstalls)
		assert.Zero(t, stats.ActiveInstalls)

		grouped, err := s.GetAccountModules(ctx, moduleAccount)
		require.NoError(t, err, "an account without modules has empty groups")
		require.NotNil(t, grouped)
		assert.Equal(t, moduleAccount, grouped.Account)
		assert.Empty(t, grouped.Validators)
		assert.Empty(t, grouped.Executors)
		assert.Empty(t, grouped.Fallbacks)
		assert.Empty(t, grouped.Hooks)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		s := open[moduleStore](t, newStore)
		want := moduleRecord(moduleAccount, 1, port.ModuleTypeExecutor, 4)
		require.NoError(t, s.SaveInstalledModule(ctx, want))

		got, err := s.GetInstalledModule(ctx, moduleAccount, moduleAddr(1))
		require.NoError(t, err)
		moduleAssertRecord(t, want, got)

		for name, list := range map[string]func() ([]*port.InstalledModule, error){
			"ByAccount": func() ([]*port.InstalledModule, error) { return s.GetModulesByAccount(ctx, moduleAccount, 10, 0) },
			"ByType": func() ([]*port.InstalledModule, error) {
				return s.GetModulesByType(ctx, port.ModuleTypeExecutor, 10, 0)
			},
			"Recent": func() ([]*port.InstalledModule, error) { return s.GetRecentModuleEvents(ctx, 10) },
		} {
			got, err := list()
			require.NoError(t, err, name)
			require.Len(t, got, 1, name)
			moduleAssertRecord(t, want, got[0])
		}
		n, err := s.GetModuleEventCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)

		_, err = s.GetInstalledModule(ctx, moduleAccount2, moduleAddr(1))
		assert.ErrorIs(t, err, port.ErrNotFound, "the same module on another account")
		other, err := s.GetModulesByAccount(ctx, moduleAccount2, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, other, "another account")
		other, err = s.GetModulesByType(ctx, port.ModuleTypeValidator, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, other, "another type")
	})

	t.Run("GroupedByType", func(t *testing.T) {
		s := open[moduleStore](t, newStore)
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 1, port.ModuleTypeValidator, 1)))
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 2, port.ModuleTypeExecutor, 2)))
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 3, port.ModuleTypeFallback, 3)))
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 4, port.ModuleTypeHook, 4)))
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 5, port.ModuleTypeValidator, 5)))
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount2, 6, port.ModuleTypeValidator, 6)))

		got, err := s.GetAccountModules(ctx, moduleAccount)
		require.NoError(t, err)
		assert.Equal(t, moduleAccount, got.Account)
		assert.ElementsMatch(t, []common.Address{moduleAddr(1), moduleAddr(5)}, moduleAddrs(got.Validators))
		assert.Equal(t, []common.Address{moduleAddr(2)}, moduleAddrs(got.Executors))
		assert.Equal(t, []common.Address{moduleAddr(3)}, moduleAddrs(got.Fallbacks))
		assert.Equal(t, []common.Address{moduleAddr(4)}, moduleAddrs(got.Hooks))
		for _, m := range got.Validators {
			assert.Equal(t, moduleAccount, m.Account, "only the account's own modules")
			assert.Equal(t, port.ModuleTypeValidator, m.ModuleType)
		}

		got, err = s.GetAccountModules(ctx, moduleAccount2)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{moduleAddr(6)}, moduleAddrs(got.Validators))
		assert.Empty(t, got.Executors)
	})

	t.Run("NewestFirstAndPagination", func(t *testing.T) {
		s := open[moduleStore](t, newStore)
		for b := uint64(1); b <= 5; b++ {
			require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, b, port.ModuleTypeValidator, b)))
		}
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount2, 9, port.ModuleTypeHook, 6)))

		pages := map[string]func(limit, offset int) ([]*port.InstalledModule, error){
			"ByAccount": func(limit, offset int) ([]*port.InstalledModule, error) {
				return s.GetModulesByAccount(ctx, moduleAccount, limit, offset)
			},
			"ByType": func(limit, offset int) ([]*port.InstalledModule, error) {
				return s.GetModulesByType(ctx, port.ModuleTypeValidator, limit, offset)
			},
		}
		for name, page := range pages {
			got, err := page(2, 0)
			require.NoError(t, err, name)
			assert.Equal(t, []uint64{5, 4}, moduleBlocks(got), "%s: newest first", name)
			got, err = page(2, 2)
			require.NoError(t, err, name)
			assert.Equal(t, []uint64{3, 2}, moduleBlocks(got), "%s: second page", name)
			got, err = page(2, 4)
			require.NoError(t, err, name)
			assert.Equal(t, []uint64{1}, moduleBlocks(got), "%s: last page is short", name)
			got, err = page(2, 5)
			require.NoError(t, err, name)
			assert.Empty(t, got, "%s: offset at the end", name)
			got, err = page(2, 50)
			require.NoError(t, err, name)
			assert.Empty(t, got, "%s: offset past the end", name)
		}

		recent, err := s.GetRecentModuleEvents(ctx, 3)
		require.NoError(t, err)
		assert.Equal(t, []uint64{6, 5, 4}, moduleBlocks(recent), "recent: newest first across accounts, bounded by limit")
		recent, err = s.GetRecentModuleEvents(ctx, 50)
		require.NoError(t, err)
		assert.Equal(t, []uint64{6, 5, 4, 3, 2, 1}, moduleBlocks(recent))
		n, err := s.GetModuleEventCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 6, n)
	})

	t.Run("InstallThenUninstall", func(t *testing.T) {
		s := open[moduleStore](t, newStore)
		installed := moduleRecord(moduleAccount, 1, port.ModuleTypeValidator, 2)
		require.NoError(t, s.SaveInstalledModule(ctx, installed))
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 2, port.ModuleTypeValidator, 3)))

		removedTx := fixtureHash("module-remove-tx", 7)
		require.NoError(t, s.RemoveModule(ctx, moduleAccount, moduleAddr(1), 7, removedTx))

		got, err := s.GetInstalledModule(ctx, moduleAccount, moduleAddr(1))
		require.NoError(t, err, "an uninstalled module keeps its record")
		assert.False(t, got.Active)
		require.NotNil(t, got.RemovedAt)
		assert.Equal(t, uint64(7), *got.RemovedAt)
		require.NotNil(t, got.RemovedTx)
		assert.Equal(t, removedTx, *got.RemovedTx)
		assert.Equal(t, installed.InstalledAt, got.InstalledAt, "the install is kept")
		assert.Equal(t, installed.InstalledTx, got.InstalledTx)
		assert.Equal(t, installed.ModuleType, got.ModuleType)

		still, err := s.GetInstalledModule(ctx, moduleAccount, moduleAddr(2))
		require.NoError(t, err)
		assert.True(t, still.Active, "another module of the account is unaffected")
		assert.Nil(t, still.RemovedAt)

		// The account's lists show the module with its current state.
		byAccount, err := s.GetModulesByAccount(ctx, moduleAccount, 10, 0)
		require.NoError(t, err)
		require.Len(t, byAccount, 2)
		for _, m := range byAccount {
			assert.Equal(t, m.Module != moduleAddr(1), m.Active, "module %s", m.Module.Hex())
		}
		grouped, err := s.GetAccountModules(ctx, moduleAccount)
		require.NoError(t, err)
		require.Len(t, grouped.Validators, 2)
		for _, m := range grouped.Validators {
			assert.Equal(t, m.Module != moduleAddr(1), m.Active, "module %s", m.Module.Hex())
		}
	})

	t.Run("ResaveIsIdempotent", func(t *testing.T) {
		s := open[moduleStore](t, newStore)
		r := moduleRecord(moduleAccount, 1, port.ModuleTypeHook, 4)
		require.NoError(t, s.SaveInstalledModule(ctx, r))
		require.NoError(t, s.SaveInstalledModule(ctx, r))

		n, err := s.GetModuleEventCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		for name, list := range map[string]func() ([]*port.InstalledModule, error){
			"ByAccount": func() ([]*port.InstalledModule, error) { return s.GetModulesByAccount(ctx, moduleAccount, 10, 0) },
			"ByType":    func() ([]*port.InstalledModule, error) { return s.GetModulesByType(ctx, port.ModuleTypeHook, 10, 0) },
			"Recent":    func() ([]*port.InstalledModule, error) { return s.GetRecentModuleEvents(ctx, 10) },
		} {
			got, err := list()
			require.NoError(t, err, name)
			assert.Len(t, got, 1, "%s: writing the same install again does not duplicate it", name)
		}
	})

	t.Run("ReinstallListedOnce", func(t *testing.T) {
		knownDefect(t, "re-installing a module leaves the earlier install's account/type/block index entries, so the module is listed twice")
		s := open[moduleStore](t, newStore)
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 1, port.ModuleTypeValidator, 1)))
		require.NoError(t, s.RemoveModule(ctx, moduleAccount, moduleAddr(1), 2, fixtureHash("module-remove-tx", 2)))
		again := moduleRecord(moduleAccount, 1, port.ModuleTypeValidator, 3)
		require.NoError(t, s.SaveInstalledModule(ctx, again))

		got, err := s.GetInstalledModule(ctx, moduleAccount, moduleAddr(1))
		require.NoError(t, err)
		moduleAssertRecord(t, again, got)

		byAccount, err := s.GetModulesByAccount(ctx, moduleAccount, 10, 0)
		require.NoError(t, err)
		assert.Len(t, byAccount, 1, "ByAccount")
		byType, err := s.GetModulesByType(ctx, port.ModuleTypeValidator, 10, 0)
		require.NoError(t, err)
		assert.Len(t, byType, 1, "ByType")
	})

	t.Run("UninstallIsAnEvent", func(t *testing.T) {
		knownDefect(t, "RemoveModule records no module event: the event count and recent events ignore uninstalls")
		s := open[moduleStore](t, newStore)
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 1, port.ModuleTypeValidator, 1)))
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 2, port.ModuleTypeValidator, 5)))
		require.NoError(t, s.RemoveModule(ctx, moduleAccount, moduleAddr(1), 10, fixtureHash("module-remove-tx", 10)))

		n, err := s.GetModuleEventCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, n, "two installs and one uninstall")
		recent, err := s.GetRecentModuleEvents(ctx, 1)
		require.NoError(t, err)
		require.Len(t, recent, 1)
		assert.Equal(t, moduleAddr(1), recent[0].Module, "the uninstall at block 10 is the newest event")
		assert.False(t, recent[0].Active)
	})

	t.Run("StatsReplaceAndList", func(t *testing.T) {
		s := open[moduleStore](t, newStore)
		want := &port.ModuleStats{Module: moduleAddr(1), ModuleType: port.ModuleTypeValidator, TotalInstalls: 3, ActiveInstalls: 2}
		require.NoError(t, s.UpdateModuleStats(ctx, want))
		got, err := s.GetModuleStats(ctx, moduleAddr(1))
		require.NoError(t, err)
		assert.Equal(t, *want, *got)

		// An update stores the given totals; it does not add to them.
		replaced := &port.ModuleStats{Module: moduleAddr(1), ModuleType: port.ModuleTypeValidator, TotalInstalls: 4, ActiveInstalls: 1}
		require.NoError(t, s.UpdateModuleStats(ctx, replaced))
		got, err = s.GetModuleStats(ctx, moduleAddr(1))
		require.NoError(t, err)
		assert.Equal(t, *replaced, *got)

		require.NoError(t, s.UpdateModuleStats(ctx, &port.ModuleStats{Module: moduleAddr(2), ModuleType: port.ModuleTypeHook, TotalInstalls: 1, ActiveInstalls: 1}))
		require.NoError(t, s.UpdateModuleStats(ctx, &port.ModuleStats{Module: moduleAddr(3), ModuleType: port.ModuleTypeExecutor, TotalInstalls: 2}))

		all, err := s.ListModuleStats(ctx, 10, 0)
		require.NoError(t, err)
		assert.ElementsMatch(t, []common.Address{moduleAddr(1), moduleAddr(2), moduleAddr(3)}, moduleStatsAddrs(all))
		for _, st := range all {
			if st.Module == moduleAddr(1) {
				assert.Equal(t, *replaced, *st, "the list holds the latest stats")
			}
		}
		first, err := s.ListModuleStats(ctx, 2, 0)
		require.NoError(t, err)
		assert.Equal(t, moduleStatsAddrs(all)[:2], moduleStatsAddrs(first), "pages follow the list order")
		second, err := s.ListModuleStats(ctx, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, moduleStatsAddrs(all)[2:], moduleStatsAddrs(second))
		past, err := s.ListModuleStats(ctx, 2, 3)
		require.NoError(t, err)
		assert.Empty(t, past, "offset at the end")

		// Installs alone do not create stats: the writer maintains them.
		require.NoError(t, s.SaveInstalledModule(ctx, moduleRecord(moduleAccount, 4, port.ModuleTypeHook, 1)))
		untouched, err := s.GetModuleStats(ctx, moduleAddr(4))
		require.NoError(t, err)
		assert.Zero(t, untouched.TotalInstalls)
	})
}

// moduleRecord returns an active install of fixture module n on account at
// block.
func moduleRecord(account common.Address, n uint64, typ port.ModuleType, block uint64) *port.InstalledModule {
	return &port.InstalledModule{
		Account:     account,
		Module:      moduleAddr(n),
		ModuleType:  typ,
		InstalledAt: block,
		InstalledTx: fixtureHash("module-tx", block*1000+n),
		Active:      true,
		Timestamp:   time.Unix(int64(baseTime+12*block), 0).UTC(),
	}
}

// moduleAssertRecord compares install records field by field through their
// JSON form, which compares times by value.
func moduleAssertRecord(t *testing.T, want, got *port.InstalledModule) {
	t.Helper()
	require.NotNil(t, got)
	w, err := json.Marshal(want)
	require.NoError(t, err)
	g, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, string(w), string(g))
}

// moduleBlocks returns the install blocks of records, in order.
func moduleBlocks(records []*port.InstalledModule) []uint64 {
	out := make([]uint64, 0, len(records))
	for _, r := range records {
		out = append(out, r.InstalledAt)
	}
	return out
}

// moduleAddrs returns the module addresses of records, in order.
func moduleAddrs(records []port.InstalledModule) []common.Address {
	out := make([]common.Address, 0, len(records))
	for _, r := range records {
		out = append(out, r.Module)
	}
	return out
}

// moduleStatsAddrs returns the module addresses of stats, in order.
func moduleStatsAddrs(stats []*port.ModuleStats) []common.Address {
	out := make([]common.Address, 0, len(stats))
	for _, s := range stats {
		out = append(out, s.Module)
	}
	return out
}
