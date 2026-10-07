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
			"ByTx":    func() ([]*userop.UserOperation, error) { return s.GetUserOpsByTx(ctx, userOpTxHash(1, 0)) },
			"ByBlock": func() ([]*userop.UserOperation, error) { return s.GetUserOpsByBlock(ctx, 1) },
			"BySender": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsBySender(ctx, userOpSender, port.FirstPage(10)))
			},
			"ByBundler": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsByBundler(ctx, userOpBundler, port.FirstPage(10)))
			},
			"ByPaymaster": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsByPaymaster(ctx, userOpPaymaster, port.FirstPage(10)))
			},
			"ByFactory": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsByFactory(ctx, userOpFactory, port.FirstPage(10)))
			},
			"Recent": func() ([]*userop.UserOperation, error) { return s.GetRecentUserOps(ctx, 10) },
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

		bundlers, err := userOpItems(s.ListBundlers(ctx, port.FirstPage(10)))
		require.NoError(t, err)
		assert.Empty(t, bundlers)
		factories, err := userOpItems(s.ListFactories(ctx, port.FirstPage(10)))
		require.NoError(t, err)
		assert.Empty(t, factories)
		paymasters, err := userOpItems(s.ListPaymasters(ctx, port.FirstPage(10)))
		require.NoError(t, err)
		assert.Empty(t, paymasters)
		accounts, err := userOpItems(s.ListSmartAccounts(ctx, port.FirstPage(10)))
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
			"ByTx":    func() ([]*userop.UserOperation, error) { return s.GetUserOpsByTx(ctx, want.TransactionHash) },
			"ByBlock": func() ([]*userop.UserOperation, error) { return s.GetUserOpsByBlock(ctx, 6) },
			"BySender": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsBySender(ctx, userOpSender, port.FirstPage(10)))
			},
			"ByBundler": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsByBundler(ctx, userOpBundler, port.FirstPage(10)))
			},
			"ByPaymaster": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsByPaymaster(ctx, userOpPaymaster, port.FirstPage(10)))
			},
			"ByFactory": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsByFactory(ctx, userOpFactory, port.FirstPage(10)))
			},
			"Recent": func() ([]*userop.UserOperation, error) { return s.GetRecentUserOps(ctx, 10) },
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
		other, err = userOpItems(s.GetUserOpsBySender(ctx, userOpBundler, port.FirstPage(10)))
		require.NoError(t, err)
		assert.Empty(t, other, "the bundler is not a sender")
		other, err = userOpItems(s.GetUserOpsByBundler(ctx, userOpSender, port.FirstPage(10)))
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
		byPaymaster, err := userOpItems(s.GetUserOpsByPaymaster(ctx, zero, port.FirstPage(10)))
		require.NoError(t, err)
		assert.Empty(t, byPaymaster, "an operation without a paymaster is not listed under the zero address")
		byFactory, err := userOpItems(s.GetUserOpsByFactory(ctx, zero, port.FirstPage(10)))
		require.NoError(t, err)
		assert.Empty(t, byFactory, "an operation without a factory is not listed under the zero address")

		bySender, err := userOpItems(s.GetUserOpsBySender(ctx, userOpSender, port.FirstPage(10)))
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
		bySender, err := userOpItems(s.GetUserOpsBySender(ctx, userOpSender2, port.FirstPage(10)))
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{ops[1].Hash}, userOpHashes(bySender))
		byPaymaster, err := userOpItems(s.GetUserOpsByPaymaster(ctx, userOpPaymaster, port.FirstPage(10)))
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{ops[0].Hash}, userOpHashes(byPaymaster))
		byFactory, err := userOpItems(s.GetUserOpsByFactory(ctx, userOpFactory, port.FirstPage(10)))
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{ops[1].Hash}, userOpHashes(byFactory))
	})

	t.Run("NewestFirstAndPagination", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		paymaster2 := common.HexToAddress("0x0000000000000000000000000000000000004c02")
		factory2 := common.HexToAddress("0x0000000000000000000000000000000000004d02")
		var want []*userop.UserOperation
		for b := uint64(1); b <= 5; b++ {
			op := userOpOp(b, 0, 0, userOpSender, userOpBundler, &userOpPaymaster, &userOpFactory)
			require.NoError(t, s.SaveUserOp(ctx, op))
			want = append([]*userop.UserOperation{op}, want...)
			// Operations of other addresses in the same blocks, so each
			// list has a neighbour of the same kind.
			if b <= 2 {
				require.NoError(t, s.SaveUserOp(ctx, userOpOp(b, 1, 0, userOpSender2, userOpBundler2, &paymaster2, &factory2)))
			}
		}
		type byAddr func(addr common.Address) listPage[*userop.UserOperation]
		lists := map[string]struct {
			list        byAddr
			addr, other common.Address
		}{
			"BySender": {func(a common.Address) listPage[*userop.UserOperation] {
				return func(page port.Page) ([]*userop.UserOperation, string, error) {
					return s.GetUserOpsBySender(ctx, a, page)
				}
			}, userOpSender, userOpSender2},
			"ByBundler": {func(a common.Address) listPage[*userop.UserOperation] {
				return func(page port.Page) ([]*userop.UserOperation, string, error) {
					return s.GetUserOpsByBundler(ctx, a, page)
				}
			}, userOpBundler, userOpBundler2},
			"ByPaymaster": {func(a common.Address) listPage[*userop.UserOperation] {
				return func(page port.Page) ([]*userop.UserOperation, string, error) {
					return s.GetUserOpsByPaymaster(ctx, a, page)
				}
			}, userOpPaymaster, paymaster2},
			"ByFactory": {func(a common.Address) listPage[*userop.UserOperation] {
				return func(page port.Page) ([]*userop.UserOperation, string, error) {
					return s.GetUserOpsByFactory(ctx, a, page)
				}
			}, userOpFactory, factory2},
		}
		opHash := func(op *userop.UserOperation) common.Hash { return op.Hash }
		for name, l := range lists {
			t.Run(name, func(t *testing.T) {
				got, _, err := l.list(l.addr)(port.FirstPage(10))
				require.NoError(t, err)
				assert.Equal(t, []uint64{5, 4, 3, 2, 1}, userOpBlocks(got), "newest first")
				checkPaging(t, want, opHash, l.list(l.addr))
				checkCursorFromOtherList(t, l.list(l.addr), l.list(l.other))
			})
		}

		recent, err := s.GetRecentUserOps(ctx, 3)
		require.NoError(t, err)
		assert.Equal(t, []uint64{5, 4, 3}, userOpBlocks(recent), "recent: newest first, bounded by limit")
		recent, err = s.GetRecentUserOps(ctx, 50)
		require.NoError(t, err)
		assert.Equal(t, []uint64{5, 4, 3, 2, 2, 1, 1}, userOpBlocks(recent), "recent: every address")
		n, err := s.GetUserOpCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 7, n)
	})

	t.Run("BySenderResumeAfterAppend", func(t *testing.T) {
		s := open[userOpStore](t, newStore)
		first := userOpOp(1, 0, 0, userOpSender, userOpBundler, nil, nil)
		second := userOpOp(2, 0, 0, userOpSender, userOpBundler, nil, nil)
		require.NoError(t, s.SaveUserOps(ctx, []*userop.UserOperation{first, second}))
		page, next, err := s.GetUserOpsBySender(ctx, userOpSender, port.FirstPage(1))
		require.NoError(t, err)
		require.Equal(t, []common.Hash{second.Hash}, userOpHashes(page))
		require.NotEmpty(t, next)
		require.NoError(t, s.SaveUserOp(ctx, userOpOp(3, 0, 0, userOpSender, userOpBundler, nil, nil)))
		got, _, err := s.GetUserOpsBySender(ctx, userOpSender, port.Page{After: next, Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{first.Hash}, userOpHashes(got), "a cursor stays valid while newer operations arrive")
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
			"ByTx":    func() ([]*userop.UserOperation, error) { return s.GetUserOpsByTx(ctx, op.TransactionHash) },
			"ByBlock": func() ([]*userop.UserOperation, error) { return s.GetUserOpsByBlock(ctx, 8) },
			"BySender": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsBySender(ctx, userOpSender, port.FirstPage(10)))
			},
			"ByBundler": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsByBundler(ctx, userOpBundler, port.FirstPage(10)))
			},
			"ByPaymaster": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsByPaymaster(ctx, userOpPaymaster, port.FirstPage(10)))
			},
			"ByFactory": func() ([]*userop.UserOperation, error) {
				return userOpItems(s.GetUserOpsByFactory(ctx, userOpFactory, port.FirstPage(10)))
			},
			"Recent": func() ([]*userop.UserOperation, error) { return s.GetRecentUserOps(ctx, 10) },
		} {
			got, err := list()
			require.NoError(t, err, name)
			assert.Len(t, got, 1, "%s: writing the same operation again does not duplicate it", name)
		}
	})

	t.Run("BundlesInOneBlock", func(t *testing.T) {
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
		byBundler, err := userOpItems(s.GetUserOpsByBundler(ctx, userOpBundler, port.FirstPage(10)))
		require.NoError(t, err)
		assert.ElementsMatch(t, []common.Hash{a.Hash, b.Hash}, userOpHashes(byBundler), "ByBundler")
		byPaymaster, err := userOpItems(s.GetUserOpsByPaymaster(ctx, userOpPaymaster, port.FirstPage(10)))
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
		// Every list holds the same three addresses, one entry each, in a
		// fixed order.
		lists := map[string]listPage[common.Address]{
			"Bundlers": func(page port.Page) ([]common.Address, string, error) {
				l, next, err := s.ListBundlers(ctx, page)
				out := make([]common.Address, 0, len(l))
				for _, x := range l {
					out = append(out, x.Address)
				}
				return out, next, err
			},
			"Factories": func(page port.Page) ([]common.Address, string, error) {
				l, next, err := s.ListFactories(ctx, page)
				out := make([]common.Address, 0, len(l))
				for _, x := range l {
					out = append(out, x.Address)
				}
				return out, next, err
			},
			"Paymasters": func(page port.Page) ([]common.Address, string, error) {
				l, next, err := s.ListPaymasters(ctx, page)
				out := make([]common.Address, 0, len(l))
				for _, x := range l {
					out = append(out, x.Address)
				}
				return out, next, err
			},
			"SmartAccounts": func(page port.Page) ([]common.Address, string, error) {
				l, next, err := s.ListSmartAccounts(ctx, page)
				out := make([]common.Address, 0, len(l))
				for _, x := range l {
					out = append(out, x.Address)
				}
				return out, next, err
			},
		}
		for name, list := range lists {
			t.Run(name, func(t *testing.T) {
				all, _, err := list(port.FirstPage(10))
				require.NoError(t, err)
				assert.ElementsMatch(t, addrs, all)
				checkPaging(t, all, func(a common.Address) common.Address { return a }, list)
			})
		}
		checkCursorFromOtherList(t, lists["Bundlers"], lists["Factories"])
		checkCursorFromOtherList(t, lists["Paymasters"], lists["SmartAccounts"])
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

		accounts, err := userOpItems(s.ListSmartAccounts(ctx, port.FirstPage(10)))
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

// userOpItems returns the items of a list page, dropping its cursor.
func userOpItems[T any](items []T, _ string, err error) ([]T, error) {
	return items, err
}
