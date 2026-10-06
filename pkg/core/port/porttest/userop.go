package porttest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/userop"
)

// Addresses of the UserOperation fixtures.
var (
	userOpSender     = common.HexToAddress("0x0000000000000000000000000000000000004a01")
	userOpSender2    = common.HexToAddress("0x0000000000000000000000000000000000004a02")
	userOpBundler    = common.HexToAddress("0x0000000000000000000000000000000000004b01")
	userOpBundler2   = common.HexToAddress("0x0000000000000000000000000000000000004b02")
	userOpPaymaster  = common.HexToAddress("0x0000000000000000000000000000000000004c01")
	userOpFactory    = common.HexToAddress("0x0000000000000000000000000000000000004d01")
	userOpEntryPoint = common.HexToAddress("0x0000000071727De22E5E9d8BAf0edAc6f37da032")
)

// testUserOpIndex checks UserOpIndexReader and UserOpIndexWriter: an
// ERC-4337 UserOperation is found by its hash and through the transaction,
// block, sender, bundler, paymaster and factory indexes, address lists are
// newest first and paginated, the count agrees with the stored operations,
// and bundler, factory and paymaster statistics and smart accounts are
// stored as given (a later write replaces an earlier one).
func testUserOpIndex(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("NotFoundOnEmptyStore", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		_, err := s.GetUserOp(ctx, userOpHash(1, 0, 0))
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetSmartAccount(ctx, userOpSender)
		assert.ErrorIs(t, err, port.ErrNotFound)

		for name, list := range map[string]func() ([]*userop.UserOperation, error){
			"ByTx":        func() ([]*userop.UserOperation, error) { return s.GetUserOpsByTx(ctx, userOpTxHash(1, 0)) },
			"ByBlock":     func() ([]*userop.UserOperation, error) { return s.GetUserOpsByBlock(ctx, 1) },
			"BySender":    func() ([]*userop.UserOperation, error) { return s.GetUserOpsBySender(ctx, userOpSender, 10, 0) },
			"ByBundler":   func() ([]*userop.UserOperation, error) { return s.GetUserOpsByBundler(ctx, userOpBundler, 10, 0) },
			"ByPaymaster": func() ([]*userop.UserOperation, error) { return s.GetUserOpsByPaymaster(ctx, userOpPaymaster, 10, 0) },
			"ByFactory":   func() ([]*userop.UserOperation, error) { return s.GetUserOpsByFactory(ctx, userOpFactory, 10, 0) },
			"Recent":      func() ([]*userop.UserOperation, error) { return s.GetRecentUserOps(ctx, 10) },
		} {
			got, err := list()
			require.NoError(t, err, name)
			assert.Empty(t, got, name)
		}

		n, err := s.GetUserOpCount(ctx)
		require.NoError(t, err)
		assert.Zero(t, n)

		bundler, err := s.GetBundlerStats(ctx, userOpBundler)
		require.NoError(t, err, "no activity is zero-value stats, not an error")
		assert.Equal(t, userop.BundlerStats{Address: userOpBundler}, *bundler)
		factory, err := s.GetFactoryStats(ctx, userOpFactory)
		require.NoError(t, err)
		assert.Equal(t, userop.FactoryStats{Address: userOpFactory}, *factory)
		paymaster, err := s.GetPaymasterStats(ctx, userOpPaymaster)
		require.NoError(t, err)
		assert.Equal(t, userop.PaymasterStats{Address: userOpPaymaster}, *paymaster)

		bundlers, err := s.ListBundlers(ctx, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, bundlers)
		factories, err := s.ListFactories(ctx, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, factories)
		paymasters, err := s.ListPaymasters(ctx, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, paymasters)
		accounts, err := s.ListSmartAccounts(ctx, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, accounts)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		want := userOpOp(6, 1, 0, userOpSender, userOpBundler, &userOpPaymaster, &userOpFactory)
		want.Status = false
		want.RevertReason = []byte{0x08, 0xc3, 0x79, 0xa0}
		require.NoError(t, s.SaveUserOp(ctx, want))

		got, err := s.GetUserOp(ctx, want.Hash)
		require.NoError(t, err)
		userOpAssertOp(t, want, got)

		for name, list := range map[string]func() ([]*userop.UserOperation, error){
			"ByTx":        func() ([]*userop.UserOperation, error) { return s.GetUserOpsByTx(ctx, want.TransactionHash) },
			"ByBlock":     func() ([]*userop.UserOperation, error) { return s.GetUserOpsByBlock(ctx, 6) },
			"BySender":    func() ([]*userop.UserOperation, error) { return s.GetUserOpsBySender(ctx, userOpSender, 10, 0) },
			"ByBundler":   func() ([]*userop.UserOperation, error) { return s.GetUserOpsByBundler(ctx, userOpBundler, 10, 0) },
			"ByPaymaster": func() ([]*userop.UserOperation, error) { return s.GetUserOpsByPaymaster(ctx, userOpPaymaster, 10, 0) },
			"ByFactory":   func() ([]*userop.UserOperation, error) { return s.GetUserOpsByFactory(ctx, userOpFactory, 10, 0) },
			"Recent":      func() ([]*userop.UserOperation, error) { return s.GetRecentUserOps(ctx, 10) },
		} {
			got, err := list()
			require.NoError(t, err, name)
			require.Len(t, got, 1, name)
			userOpAssertOp(t, want, got[0])
		}

		n, err := s.GetUserOpCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)

		other, err := s.GetUserOpsByBlock(ctx, 7)
		require.NoError(t, err)
		assert.Empty(t, other, "another block")
		other, err = s.GetUserOpsBySender(ctx, userOpBundler, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, other, "the bundler is not a sender")
		other, err = s.GetUserOpsByBundler(ctx, userOpSender, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, other, "the sender is not a bundler")
	})

	t.Run("NoPaymasterOrFactoryIsNotIndexed", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		zero := common.Address{}
		require.NoError(t, s.SaveUserOps(ctx, []*userop.UserOperation{
			userOpOp(2, 0, 0, userOpSender, userOpBundler, nil, nil),
			userOpOp(3, 0, 0, userOpSender, userOpBundler, &zero, &zero),
		}))
		byPaymaster, err := s.GetUserOpsByPaymaster(ctx, zero, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, byPaymaster, "an operation without a paymaster is not listed under the zero address")
		byFactory, err := s.GetUserOpsByFactory(ctx, zero, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, byFactory, "an operation without a factory is not listed under the zero address")

		bySender, err := s.GetUserOpsBySender(ctx, userOpSender, 10, 0)
		require.NoError(t, err)
		assert.Len(t, bySender, 2, "the other indexes still list them")
	})

	t.Run("BatchSaveIndexesEveryOp", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		require.NoError(t, s.SaveUserOps(ctx, nil), "an empty batch is a no-op")

		// Block 4 has one bundle transaction with three operations.
		ops := []*userop.UserOperation{
			userOpOp(4, 0, 0, userOpSender, userOpBundler, &userOpPaymaster, nil),
			userOpOp(4, 0, 1, userOpSender2, userOpBundler, nil, &userOpFactory),
			userOpOp(4, 0, 2, userOpSender, userOpBundler, nil, nil),
		}
		require.NoError(t, s.SaveUserOps(ctx, ops))

		for _, want := range ops {
			got, err := s.GetUserOp(ctx, want.Hash)
			require.NoError(t, err)
			userOpAssertOp(t, want, got)
		}
		byTx, err := s.GetUserOpsByTx(ctx, ops[0].TransactionHash)
		require.NoError(t, err)
		assert.Equal(t, userOpHashes(ops), userOpHashes(byTx), "operations of a transaction are in bundle order")
		byBlock, err := s.GetUserOpsByBlock(ctx, 4)
		require.NoError(t, err)
		assert.Equal(t, userOpHashes(ops), userOpHashes(byBlock), "operations of a block are in bundle order")

		n, err := s.GetUserOpCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
		bySender, err := s.GetUserOpsBySender(ctx, userOpSender2, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{ops[1].Hash}, userOpHashes(bySender))
		byPaymaster, err := s.GetUserOpsByPaymaster(ctx, userOpPaymaster, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{ops[0].Hash}, userOpHashes(byPaymaster))
		byFactory, err := s.GetUserOpsByFactory(ctx, userOpFactory, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{ops[1].Hash}, userOpHashes(byFactory))
	})

	t.Run("NewestFirstAndPagination", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		for b := uint64(1); b <= 5; b++ {
			require.NoError(t, s.SaveUserOp(ctx, userOpOp(b, 0, 0, userOpSender, userOpBundler, &userOpPaymaster, &userOpFactory)))
		}
		pages := map[string]func(limit, offset int) ([]*userop.UserOperation, error){
			"BySender": func(limit, offset int) ([]*userop.UserOperation, error) {
				return s.GetUserOpsBySender(ctx, userOpSender, limit, offset)
			},
			"ByBundler": func(limit, offset int) ([]*userop.UserOperation, error) {
				return s.GetUserOpsByBundler(ctx, userOpBundler, limit, offset)
			},
			"ByPaymaster": func(limit, offset int) ([]*userop.UserOperation, error) {
				return s.GetUserOpsByPaymaster(ctx, userOpPaymaster, limit, offset)
			},
			"ByFactory": func(limit, offset int) ([]*userop.UserOperation, error) {
				return s.GetUserOpsByFactory(ctx, userOpFactory, limit, offset)
			},
		}
		for name, page := range pages {
			got, err := page(2, 0)
			require.NoError(t, err, name)
			assert.Equal(t, []uint64{5, 4}, userOpBlocks(got), "%s: newest first", name)
			got, err = page(2, 2)
			require.NoError(t, err, name)
			assert.Equal(t, []uint64{3, 2}, userOpBlocks(got), "%s: second page", name)
			got, err = page(2, 4)
			require.NoError(t, err, name)
			assert.Equal(t, []uint64{1}, userOpBlocks(got), "%s: last page is short", name)
			got, err = page(2, 5)
			require.NoError(t, err, name)
			assert.Empty(t, got, "%s: offset at the end", name)
			got, err = page(2, 50)
			require.NoError(t, err, name)
			assert.Empty(t, got, "%s: offset past the end", name)
		}

		recent, err := s.GetRecentUserOps(ctx, 3)
		require.NoError(t, err)
		assert.Equal(t, []uint64{5, 4, 3}, userOpBlocks(recent), "recent: newest first, bounded by limit")
		recent, err = s.GetRecentUserOps(ctx, 50)
		require.NoError(t, err)
		assert.Equal(t, []uint64{5, 4, 3, 2, 1}, userOpBlocks(recent))
		n, err := s.GetUserOpCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
	})

	t.Run("ResaveIsIdempotent", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		op := userOpOp(8, 0, 0, userOpSender, userOpBundler, &userOpPaymaster, &userOpFactory)
		require.NoError(t, s.SaveUserOp(ctx, op))
		require.NoError(t, s.SaveUserOp(ctx, op))
		require.NoError(t, s.SaveUserOps(ctx, []*userop.UserOperation{op}))

		n, err := s.GetUserOpCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		for name, list := range map[string]func() ([]*userop.UserOperation, error){
			"ByTx":        func() ([]*userop.UserOperation, error) { return s.GetUserOpsByTx(ctx, op.TransactionHash) },
			"ByBlock":     func() ([]*userop.UserOperation, error) { return s.GetUserOpsByBlock(ctx, 8) },
			"BySender":    func() ([]*userop.UserOperation, error) { return s.GetUserOpsBySender(ctx, userOpSender, 10, 0) },
			"ByBundler":   func() ([]*userop.UserOperation, error) { return s.GetUserOpsByBundler(ctx, userOpBundler, 10, 0) },
			"ByPaymaster": func() ([]*userop.UserOperation, error) { return s.GetUserOpsByPaymaster(ctx, userOpPaymaster, 10, 0) },
			"ByFactory":   func() ([]*userop.UserOperation, error) { return s.GetUserOpsByFactory(ctx, userOpFactory, 10, 0) },
			"Recent":      func() ([]*userop.UserOperation, error) { return s.GetRecentUserOps(ctx, 10) },
		} {
			got, err := list()
			require.NoError(t, err, name)
			assert.Len(t, got, 1, "%s: writing the same operation again does not duplicate it", name)
		}
	})

	t.Run("BundlesInOneBlock", func(t *testing.T) {
		knownDefect(t, "two bundle transactions in one block overwrite each other's block/sender/bundler/paymaster index entries")
		s := open[userOpStore](t, newStore)
		// Two bundles of one bundler in block 9; each bundle's first
		// operation has bundle index 0.
		a := userOpOp(9, 0, 0, userOpSender, userOpBundler, &userOpPaymaster, nil)
		b := userOpOp(9, 1, 0, userOpSender2, userOpBundler, &userOpPaymaster, nil)
		require.NoError(t, s.SaveUserOps(ctx, []*userop.UserOperation{a, b}))

		n, err := s.GetUserOpCount(ctx)
		require.NoError(t, err)
		require.Equal(t, 2, n)
		byBlock, err := s.GetUserOpsByBlock(ctx, 9)
		require.NoError(t, err)
		assert.ElementsMatch(t, []common.Hash{a.Hash, b.Hash}, userOpHashes(byBlock), "ByBlock")
		byBundler, err := s.GetUserOpsByBundler(ctx, userOpBundler, 10, 0)
		require.NoError(t, err)
		assert.ElementsMatch(t, []common.Hash{a.Hash, b.Hash}, userOpHashes(byBundler), "ByBundler")
		byPaymaster, err := s.GetUserOpsByPaymaster(ctx, userOpPaymaster, 10, 0)
		require.NoError(t, err)
		assert.ElementsMatch(t, []common.Hash{a.Hash, b.Hash}, userOpHashes(byPaymaster), "ByPaymaster")
		recent, err := s.GetRecentUserOps(ctx, 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, []common.Hash{a.Hash, b.Hash}, userOpHashes(recent), "Recent")
	})

	t.Run("StatsReplace", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		require.NoError(t, s.UpdateBundlerStats(ctx, &userop.BundlerStats{Address: userOpBundler, TotalBundles: 2, TotalOps: 5}))
		require.NoError(t, s.UpdateFactoryStats(ctx, &userop.FactoryStats{Address: userOpFactory, TotalAccounts: 3}))
		require.NoError(t, s.UpdatePaymasterStats(ctx, &userop.PaymasterStats{Address: userOpPaymaster, TotalOps: 4}))

		bundler, err := s.GetBundlerStats(ctx, userOpBundler)
		require.NoError(t, err)
		assert.Equal(t, userop.BundlerStats{Address: userOpBundler, TotalBundles: 2, TotalOps: 5}, *bundler)
		factory, err := s.GetFactoryStats(ctx, userOpFactory)
		require.NoError(t, err)
		assert.Equal(t, userop.FactoryStats{Address: userOpFactory, TotalAccounts: 3}, *factory)
		paymaster, err := s.GetPaymasterStats(ctx, userOpPaymaster)
		require.NoError(t, err)
		assert.Equal(t, userop.PaymasterStats{Address: userOpPaymaster, TotalOps: 4}, *paymaster)

		// An update stores the given totals; it does not add to them.
		require.NoError(t, s.UpdateBundlerStats(ctx, &userop.BundlerStats{Address: userOpBundler, TotalBundles: 3, TotalOps: 7}))
		require.NoError(t, s.UpdateFactoryStats(ctx, &userop.FactoryStats{Address: userOpFactory, TotalAccounts: 4}))
		require.NoError(t, s.UpdatePaymasterStats(ctx, &userop.PaymasterStats{Address: userOpPaymaster, TotalOps: 6}))
		bundler, err = s.GetBundlerStats(ctx, userOpBundler)
		require.NoError(t, err)
		assert.Equal(t, userop.BundlerStats{Address: userOpBundler, TotalBundles: 3, TotalOps: 7}, *bundler)
		factory, err = s.GetFactoryStats(ctx, userOpFactory)
		require.NoError(t, err)
		assert.Equal(t, uint64(4), factory.TotalAccounts)
		paymaster, err = s.GetPaymasterStats(ctx, userOpPaymaster)
		require.NoError(t, err)
		assert.Equal(t, uint64(6), paymaster.TotalOps)

		other, err := s.GetBundlerStats(ctx, userOpBundler2)
		require.NoError(t, err)
		assert.Equal(t, userop.BundlerStats{Address: userOpBundler2}, *other, "another bundler is unaffected")
	})

	t.Run("ListStatsPaginated", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		addrs := []common.Address{userOpBundler, userOpBundler2, userOpPaymaster}
		for i, a := range addrs {
			n := uint64(i + 1)
			require.NoError(t, s.UpdateBundlerStats(ctx, &userop.BundlerStats{Address: a, TotalBundles: n, TotalOps: n}))
			require.NoError(t, s.UpdateFactoryStats(ctx, &userop.FactoryStats{Address: a, TotalAccounts: n}))
			require.NoError(t, s.UpdatePaymasterStats(ctx, &userop.PaymasterStats{Address: a, TotalOps: n}))
			require.NoError(t, s.SaveSmartAccount(ctx, &userop.SmartAccount{Address: a, TotalOps: n}))
		}
		// Every list is paginated over the same three addresses: pages are
		// disjoint and together hold every address once.
		lists := map[string]func(limit, offset int) ([]common.Address, error){
			"Bundlers": func(limit, offset int) ([]common.Address, error) {
				l, err := s.ListBundlers(ctx, limit, offset)
				out := make([]common.Address, 0, len(l))
				for _, x := range l {
					out = append(out, x.Address)
				}
				return out, err
			},
			"Factories": func(limit, offset int) ([]common.Address, error) {
				l, err := s.ListFactories(ctx, limit, offset)
				out := make([]common.Address, 0, len(l))
				for _, x := range l {
					out = append(out, x.Address)
				}
				return out, err
			},
			"Paymasters": func(limit, offset int) ([]common.Address, error) {
				l, err := s.ListPaymasters(ctx, limit, offset)
				out := make([]common.Address, 0, len(l))
				for _, x := range l {
					out = append(out, x.Address)
				}
				return out, err
			},
			"SmartAccounts": func(limit, offset int) ([]common.Address, error) {
				l, err := s.ListSmartAccounts(ctx, limit, offset)
				out := make([]common.Address, 0, len(l))
				for _, x := range l {
					out = append(out, x.Address)
				}
				return out, err
			},
		}
		for name, list := range lists {
			all, err := list(10, 0)
			require.NoError(t, err, name)
			assert.ElementsMatch(t, addrs, all, name)

			first, err := list(2, 0)
			require.NoError(t, err, name)
			require.Len(t, first, 2, name)
			assert.Equal(t, all[:2], first, "%s: pages follow the list order", name)
			second, err := list(2, 2)
			require.NoError(t, err, name)
			assert.Equal(t, all[2:], second, "%s: second page", name)
			past, err := list(2, 3)
			require.NoError(t, err, name)
			assert.Empty(t, past, "%s: offset at the end", name)
		}
	})

	t.Run("SmartAccountSaveOrUpdate", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		opHash := userOpHash(2, 0, 0)
		txHash := userOpTxHash(2, 0)
		created := time.Unix(int64(baseTime+24), 0).UTC()
		factory := userOpFactory
		want := &userop.SmartAccount{
			Address: userOpSender, CreationOpHash: &opHash, CreationTxHash: &txHash,
			CreationTimestamp: &created, Factory: &factory, TotalOps: 1,
		}
		require.NoError(t, s.SaveSmartAccount(ctx, want))
		got, err := s.GetSmartAccount(ctx, userOpSender)
		require.NoError(t, err)
		userOpAssertJSON(t, want, got)

		updated := *want
		updated.TotalOps = 5
		require.NoError(t, s.SaveSmartAccount(ctx, &updated))
		got, err = s.GetSmartAccount(ctx, userOpSender)
		require.NoError(t, err)
		userOpAssertJSON(t, &updated, got)

		accounts, err := s.ListSmartAccounts(ctx, 10, 0)
		require.NoError(t, err)
		assert.Len(t, accounts, 1, "saving an account again updates it")

		_, err = s.GetSmartAccount(ctx, userOpSender2)
		assert.ErrorIs(t, err, port.ErrNotFound, "another address")
	})
}

// userOpTxHash returns the hash of the fixture bundle transaction at a
// block and transaction index.
func userOpTxHash(block, txIndex uint64) common.Hash {
	return fixtureHash("userop-tx", block*1000+txIndex)
}

// userOpHash returns the hash of the fixture operation at a block,
// transaction index and bundle index.
func userOpHash(block, txIndex uint64, bundleIndex uint32) common.Hash {
	return fixtureHash("userop", block*1_000_000+txIndex*1000+uint64(bundleIndex))
}

// userOpOp returns a successful operation of sender in the fixture bundle
// transaction at block and txIndex, submitted by bundler.
func userOpOp(block, txIndex uint64, bundleIndex uint32, sender, bundler common.Address, paymaster, factory *common.Address) *userop.UserOperation {
	return &userop.UserOperation{
		Hash: userOpHash(block, txIndex, bundleIndex), Sender: sender, Nonce: "3",
		CallData: []byte{0xb6, 0x1d, 0x27, 0xf6}, CallGasLimit: "100000", VerificationGasLimit: "50000",
		PreVerificationGas: "21000", MaxFeePerGas: "2000000000", MaxPriorityFeePerGas: "1000000000",
		Signature:  []byte{0x01, 0x02},
		EntryPoint: userOpEntryPoint, EntryPointVersion: string(userop.EntryPointV07),
		TransactionHash: userOpTxHash(block, txIndex), BlockNumber: block,
		BlockHash: fixtureHash("userop-block", block), BundleIndex: bundleIndex,
		Bundler: bundler, Factory: userOpCopyAddr(factory), Paymaster: userOpCopyAddr(paymaster),
		Status: true, GasUsed: "90000", ActualGasCost: "180000000000000",
		SponsorType:        userop.DetermineSponsorType(paymaster),
		UserLogsStartIndex: bundleIndex * 2, UserLogsCount: 1,
		Timestamp: time.Unix(int64(baseTime+12*block), 0).UTC(),
	}
}

// userOpCopyAddr copies an optional address so fixtures do not share it.
func userOpCopyAddr(a *common.Address) *common.Address {
	if a == nil {
		return nil
	}
	c := *a
	return &c
}

// userOpAssertOp compares operations field by field through their JSON
// form, which compares times by value.
func userOpAssertOp(t *testing.T, want, got *userop.UserOperation) {
	t.Helper()
	require.NotNil(t, got)
	userOpAssertJSON(t, want, got)
}

func userOpAssertJSON(t *testing.T, want, got any) {
	t.Helper()
	w, err := json.Marshal(want)
	require.NoError(t, err)
	g, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, string(w), string(g))
}

// userOpHashes returns the hashes of ops, in order.
func userOpHashes(ops []*userop.UserOperation) []common.Hash {
	out := make([]common.Hash, 0, len(ops))
	for _, op := range ops {
		out = append(out, op.Hash)
	}
	return out
}

// userOpBlocks returns the block numbers of ops, in order.
func userOpBlocks(ops []*userop.UserOperation) []uint64 {
	out := make([]uint64, 0, len(ops))
	for _, op := range ops {
		out = append(out, op.BlockNumber)
	}
	return out
}
