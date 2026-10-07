package porttest

import (
	"context"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// testHistorical checks HistoricalReader and HistoricalWriter. Time queries
// find the blocks stored with SetBlock or indexed with SetBlockTimestamp,
// address queries the address index written by
// Writer.AddTransactionToAddressIndex, balances the snapshots written by
// UpdateBalance and SetBalance; the statistics are derived from the stored
// blocks and receipts.
func testHistorical(t *testing.T, newStore NewStore) {
	t.Run("TransactionFilter", testHistTransactionFilter)
	t.Run("TimeIndex", func(t *testing.T) { testHistTimeIndex(t, newStore) })
	t.Run("AddressTransactions", func(t *testing.T) { testHistAddressTransactions(t, newStore) })
	t.Run("Balances", func(t *testing.T) { testHistBalances(t, newStore) })
	t.Run("Counts", func(t *testing.T) { testHistCounts(t, newStore) })
	t.Run("Miners", func(t *testing.T) { testHistMiners(t, newStore) })
	t.Run("TokenBalances", func(t *testing.T) { testHistTokenBalances(t, newStore) })
	t.Run("GasStats", func(t *testing.T) { testHistGasStats(t, newStore) })
	t.Run("AddressStats", func(t *testing.T) { testHistAddressStats(t, newStore) })
}

// testHistTransactionFilter checks the filter helpers of the port, which
// every store applies the same way.
func testHistTransactionFilter(t *testing.T) {
	c := newChain(4)
	transfer := c.Blocks[2].Transactions[0] // A -> B, value 2
	call := c.Blocks[2].Transactions[1]     // B -> C, failed
	create := c.Blocks[3].Transactions[2]   // A creates `created`
	loc2 := &port.TxLocation{BlockHeight: 2, TxIndex: 0, BlockHash: c.Blocks[2].Hash}

	t.Run("DefaultHasNoRestrictions", func(t *testing.T) {
		f := port.DefaultTransactionFilter()
		assert.Equal(t, uint64(0), f.FromBlock)
		assert.Equal(t, uint64(math.MaxUint64), f.ToBlock)
		assert.Equal(t, port.TxTypeAll, f.TxType)
		assert.Nil(t, f.MinValue)
		assert.Nil(t, f.MaxValue)
		assert.False(t, f.SuccessOnly)
		assert.Nil(t, f.IsFeeDelegated)
		assert.Empty(t, f.MethodID)
		assert.Nil(t, f.MinGasUsed)
		assert.Nil(t, f.MaxGasUsed)
		assert.NoError(t, f.Validate())
	})

	t.Run("Validate", func(t *testing.T) {
		valid := func(mod func(f *port.TransactionFilter)) error {
			f := port.DefaultTransactionFilter()
			mod(f)
			return f.Validate()
		}
		assert.NoError(t, valid(func(f *port.TransactionFilter) { f.FromBlock, f.ToBlock = 5, 5 }), "a one-block range")
		assert.NoError(t, valid(func(f *port.TransactionFilter) { f.MinValue, f.MaxValue = big.NewInt(3), big.NewInt(3) }))
		assert.NoError(t, valid(func(f *port.TransactionFilter) { f.MinGasUsed, f.MaxGasUsed = histU64(7), histU64(7) }))
		assert.Error(t, valid(func(f *port.TransactionFilter) { f.FromBlock, f.ToBlock = 6, 5 }))
		assert.Error(t, valid(func(f *port.TransactionFilter) { f.MinValue, f.MaxValue = big.NewInt(4), big.NewInt(3) }))
		assert.Error(t, valid(func(f *port.TransactionFilter) { f.MinValue = big.NewInt(-1) }))
		assert.Error(t, valid(func(f *port.TransactionFilter) { f.MaxValue = big.NewInt(-1) }))
		assert.Error(t, valid(func(f *port.TransactionFilter) { f.MinGasUsed, f.MaxGasUsed = histU64(8), histU64(7) }))
	})

	t.Run("MatchTransaction", func(t *testing.T) {
		r := c.receipt(transfer.Hash)
		match := func(mod func(f *port.TransactionFilter), tx *model.Transaction, r *model.Receipt, addr common.Address) bool {
			f := port.DefaultTransactionFilter()
			mod(f)
			return f.MatchTransaction(tx, r, loc2, addr)
		}
		none := func(*port.TransactionFilter) {}
		assert.True(t, match(none, transfer, r, addrA), "the sender")
		assert.True(t, match(none, transfer, r, addrB), "the recipient")
		assert.False(t, match(none, transfer, r, addrC), "neither sender nor recipient")

		assert.True(t, match(func(f *port.TransactionFilter) { f.FromBlock, f.ToBlock = 2, 2 }, transfer, r, addrA), "the range is inclusive")
		assert.False(t, match(func(f *port.TransactionFilter) { f.FromBlock = 3 }, transfer, r, addrA))
		assert.False(t, match(func(f *port.TransactionFilter) { f.ToBlock = 1 }, transfer, r, addrA))

		assert.True(t, match(func(f *port.TransactionFilter) { f.TxType = port.TxTypeSent }, transfer, r, addrA))
		assert.False(t, match(func(f *port.TransactionFilter) { f.TxType = port.TxTypeSent }, transfer, r, addrB))
		assert.True(t, match(func(f *port.TransactionFilter) { f.TxType = port.TxTypeReceived }, transfer, r, addrB))
		assert.False(t, match(func(f *port.TransactionFilter) { f.TxType = port.TxTypeReceived }, transfer, r, addrA))
		assert.False(t, match(func(f *port.TransactionFilter) { f.TxType = port.TxTypeReceived }, create, c.receipt(create.Hash), created),
			"a contract creation has no recipient")

		assert.True(t, match(func(f *port.TransactionFilter) { f.MinValue, f.MaxValue = big.NewInt(2), big.NewInt(2) }, transfer, r, addrA), "value bounds are inclusive")
		assert.False(t, match(func(f *port.TransactionFilter) { f.MinValue = big.NewInt(3) }, transfer, r, addrA))
		assert.False(t, match(func(f *port.TransactionFilter) { f.MaxValue = big.NewInt(1) }, transfer, r, addrA))
		noValue := *transfer
		noValue.Value = nil
		assert.True(t, match(func(f *port.TransactionFilter) { f.MaxValue = big.NewInt(0) }, &noValue, r, addrA), "a missing value is zero")
		assert.False(t, match(func(f *port.TransactionFilter) { f.MinValue = big.NewInt(1) }, &noValue, r, addrA))

		successOnly := func(f *port.TransactionFilter) { f.SuccessOnly = true }
		assert.True(t, match(successOnly, transfer, r, addrA))
		assert.False(t, match(successOnly, call, c.receipt(call.Hash), addrB), "failed")
		assert.False(t, match(successOnly, transfer, nil, addrA), "no receipt")

		assert.True(t, match(func(f *port.TransactionFilter) { f.MethodID = "0xa9059cbb" }, call, c.receipt(call.Hash), addrB))
		assert.True(t, match(func(f *port.TransactionFilter) { f.MethodID = "0xA9059CBB" }, call, c.receipt(call.Hash), addrB), "case-insensitive")
		assert.False(t, match(func(f *port.TransactionFilter) { f.MethodID = "0x12345678" }, call, c.receipt(call.Hash), addrB))
		assert.False(t, match(func(f *port.TransactionFilter) { f.MethodID = "0xa9059cbb" }, transfer, r, addrA), "no input")
		assert.False(t, match(func(f *port.TransactionFilter) { f.MethodID = "0x6080" }, create, c.receipt(create.Hash), addrA), "input shorter than a selector")

		assert.True(t, match(func(f *port.TransactionFilter) { f.MinGasUsed, f.MaxGasUsed = histU64(21000), histU64(21000) }, transfer, r, addrA), "gas bounds are inclusive")
		assert.False(t, match(func(f *port.TransactionFilter) { f.MinGasUsed = histU64(21001) }, transfer, r, addrA))
		assert.False(t, match(func(f *port.TransactionFilter) { f.MaxGasUsed = histU64(20999) }, transfer, r, addrA))
		assert.False(t, match(func(f *port.TransactionFilter) { f.MinGasUsed = histU64(0) }, transfer, nil, addrA), "a gas filter needs the receipt")
	})
}

// testHistTimeIndex checks GetBlocksByTimeRange, GetBlockByTimestamp and
// GetNetworkMetrics: they find the blocks stored with SetBlock and those
// indexed with SetBlockTimestamp.
func testHistTimeIndex(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	c := newChain(5)
	at := func(h uint64) uint64 { return baseTime + 12*h }

	t.Run("BlocksByTimeRange", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		histWrite(t, s, c)

		got, _, err := s.GetBlocksByTimeRange(ctx, at(1), at(3), port.FirstPage(10))
		require.NoError(t, err)
		assert.Equal(t, []uint64{1, 2, 3}, histNumbers(got), "both ends are inclusive, in time order")
		assertBlock(t, c.Blocks[2], got[1])

		got, _, err = s.GetBlocksByTimeRange(ctx, at(1)-1, at(3)+1, port.FirstPage(10))
		require.NoError(t, err)
		assert.Equal(t, []uint64{1, 2, 3}, histNumbers(got))

		byTime := func(from, to uint64) listPage[*model.Block] {
			return func(page port.Page) ([]*model.Block, string, error) {
				return s.GetBlocksByTimeRange(ctx, from, to, page)
			}
		}
		number := func(b *model.Block) uint64 { return b.Number }
		checkPaging(t, c.Blocks, number, byTime(at(0), at(4)))
		checkCursorFromOtherList(t, byTime(at(0), at(4)), byTime(at(3), at(4)))

		got, _, err = s.GetBlocksByTimeRange(ctx, at(1)+1, at(2)-1, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, got, "no block in the window")

		_, _, err = s.GetBlocksByTimeRange(ctx, at(3), at(1), port.FirstPage(10))
		assert.Error(t, err, "fromTime after toTime")
	})

	t.Run("BlocksByTimeRangeSkipsMissingBlocks", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		histWrite(t, s, c)
		require.NoError(t, s.SetBlockTimestamp(ctx, at(5), 5)) // block 5 is not stored
		got, _, err := s.GetBlocksByTimeRange(ctx, at(3), at(5), port.FirstPage(10))
		require.NoError(t, err)
		assert.Equal(t, []uint64{3, 4}, histNumbers(got))
	})

	t.Run("BlocksByTimeRangeUnboundedEnd", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		histWrite(t, s, c)
		got, _, err := s.GetBlocksByTimeRange(ctx, 0, math.MaxUint64, port.FirstPage(10))
		require.NoError(t, err)
		assert.Equal(t, []uint64{0, 1, 2, 3, 4}, histNumbers(got))
	})

	t.Run("BlockByTimestamp", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		_, err := s.GetBlockByTimestamp(ctx, at(2))
		assert.ErrorIs(t, err, port.ErrNotFound, "nothing indexed")

		histWrite(t, s, c)
		cases := []struct {
			name string
			ts   uint64
			want uint64
		}{
			{"exact", at(2), 2},
			{"just before a block", at(2) - 1, 2},
			{"before the first block", at(0) - 100, 0},
			{"after the last block", at(4) + 100, 4},
		}
		for _, tc := range cases {
			got, err := s.GetBlockByTimestamp(ctx, tc.ts)
			require.NoError(t, err, tc.name)
			assert.Equal(t, tc.want, got.Number, tc.name)
		}
		got, err := s.GetBlockByTimestamp(ctx, at(2))
		require.NoError(t, err)
		assertBlock(t, c.Blocks[2], got)
	})

	t.Run("BlockByTimestampIsFirstAtOrAfter", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		histWrite(t, s, c)
		got, err := s.GetBlockByTimestamp(ctx, at(2)+1) // 1s after block 2, 11s before block 3
		require.NoError(t, err)
		assert.Equal(t, uint64(3), got.Number, "never a block before the time while a later one exists")
		got, err = s.GetBlockByTimestamp(ctx, at(2))
		require.NoError(t, err)
		assert.Equal(t, uint64(2), got.Number)
	})

	t.Run("BlocksWrittenWithBlockWriter", func(t *testing.T) {
		// The time queries find blocks stored with SetBlock alone, without
		// SetBlockTimestamp.
		s := open[historicalStore](t, newStore)
		c.write(t, s)

		got, _, err := s.GetBlocksByTimeRange(ctx, at(1), at(3), port.FirstPage(10))
		require.NoError(t, err)
		assert.Equal(t, []uint64{1, 2, 3}, histNumbers(got))
		b, err := s.GetBlockByTimestamp(ctx, at(2))
		require.NoError(t, err)
		assertBlock(t, c.Blocks[2], b)
		m, err := s.GetNetworkMetrics(ctx, at(1), at(4))
		require.NoError(t, err)
		assert.Equal(t, uint64(4), m.TotalBlocks)
		assert.Equal(t, uint64(9), m.TotalTransactions)
	})

	t.Run("NetworkMetrics", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		histWrite(t, s, c)

		m, err := s.GetNetworkMetrics(ctx, at(1), at(4))
		require.NoError(t, err)
		assert.Equal(t, uint64(4), m.TotalBlocks)
		assert.Equal(t, uint64(9), m.TotalTransactions)
		assert.Equal(t, uint64(106_000), m.AverageBlockSize, "(81000*3 + 181000) / 4")
		assert.InDelta(t, 12.0, m.BlockTime, 1e-9, "36s over 3 intervals")
		assert.InDelta(t, 0.25, m.TPS, 1e-9, "9 transactions in 36s")
		assert.Equal(t, uint64(36), m.TimePeriod)

		m, err = s.GetNetworkMetrics(ctx, at(1)+1, at(2)-1)
		require.NoError(t, err)
		assert.Equal(t, port.NetworkMetrics{TimePeriod: 10}, *m, "an empty window")

		_, err = s.GetNetworkMetrics(ctx, at(2), at(1))
		assert.Error(t, err)
	})
}

// testHistAddressTransactions checks GetTransactionsByAddressFiltered: it
// lists the address index in the order it was written, applies every
// filter field and pages over the matches (port.Page).
func testHistAddressTransactions(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	c := newChain(5)
	tr := func(h uint64) common.Hash { return fixtureHash("transfer", h) }
	call := func(h uint64) common.Hash { return fixtureHash("call", h) }
	create := fixtureHash("create", 3)
	// The address index of B in ingest order.
	allB := []common.Hash{tr(1), call(1), tr(2), call(2), tr(3), call(3), tr(4), call(4)}

	s := open[historicalStore](t, newStore)
	histWrite(t, s, c)
	list := func(t *testing.T, addr common.Address, mod func(f *port.TransactionFilter), limit, offset int) []common.Hash {
		t.Helper()
		f := port.DefaultTransactionFilter()
		if mod != nil {
			mod(f)
		}
		got, _, err := s.GetTransactionsByAddressFiltered(ctx, addr, f, port.Page{Limit: limit, Offset: offset})
		require.NoError(t, err)
		return histTxHashes(got)
	}

	t.Run("NilFilterListsAll", func(t *testing.T) {
		got, _, err := s.GetTransactionsByAddressFiltered(ctx, addrB, nil, port.FirstPage(100))
		require.NoError(t, err)
		assert.Equal(t, allB, histTxHashes(got))
		assert.Equal(t, []common.Hash{tr(1), tr(2), tr(3), create, tr(4)}, list(t, addrA, nil, 100, 0))
		assert.Empty(t, list(t, unknown, nil, 100, 0))
	})

	t.Run("ResultsCarryReceiptAndLocation", func(t *testing.T) {
		got, _, err := s.GetTransactionsByAddressFiltered(ctx, addrB, nil, port.FirstPage(100))
		require.NoError(t, err)
		require.Len(t, got, len(allB))
		failed := got[3] // the failed call in block 2
		assertTx(t, c.Blocks[2].Transactions[1], failed.Transaction)
		assertReceipt(t, c.receipt(call(2)), failed.Receipt)
		assert.Equal(t, port.TxLocation{BlockHeight: 2, TxIndex: 1, BlockHash: c.Blocks[2].Hash}, *failed.Location)
	})

	t.Run("BlockRange", func(t *testing.T) {
		assert.Equal(t, []common.Hash{tr(2), tr(3), create}, list(t, addrA, func(f *port.TransactionFilter) { f.FromBlock, f.ToBlock = 2, 3 }, 100, 0))
	})

	t.Run("Direction", func(t *testing.T) {
		assert.Equal(t, []common.Hash{call(1), call(2), call(3), call(4)}, list(t, addrB, func(f *port.TransactionFilter) { f.TxType = port.TxTypeSent }, 100, 0))
		assert.Equal(t, []common.Hash{tr(1), tr(2), tr(3), tr(4)}, list(t, addrB, func(f *port.TransactionFilter) { f.TxType = port.TxTypeReceived }, 100, 0))
		assert.Empty(t, list(t, addrA, func(f *port.TransactionFilter) { f.TxType = port.TxTypeReceived }, 100, 0))
	})

	t.Run("Value", func(t *testing.T) {
		assert.Equal(t, []common.Hash{tr(2), tr(3)}, list(t, addrA, func(f *port.TransactionFilter) { f.MinValue, f.MaxValue = big.NewInt(2), big.NewInt(3) }, 100, 0))
		assert.Equal(t, []common.Hash{tr(4)}, list(t, addrA, func(f *port.TransactionFilter) { f.MinValue = big.NewInt(4) }, 100, 0))
		assert.Equal(t, []common.Hash{create}, list(t, addrA, func(f *port.TransactionFilter) { f.MaxValue = big.NewInt(0) }, 100, 0))
	})

	t.Run("SuccessOnly", func(t *testing.T) {
		assert.Equal(t, []common.Hash{tr(1), call(1), tr(2), tr(3), call(3), tr(4), call(4)},
			list(t, addrB, func(f *port.TransactionFilter) { f.SuccessOnly = true }, 100, 0))
	})

	t.Run("MethodID", func(t *testing.T) {
		calls := []common.Hash{call(1), call(2), call(3), call(4)}
		assert.Equal(t, calls, list(t, addrB, func(f *port.TransactionFilter) { f.MethodID = "0xa9059cbb" }, 100, 0))
		assert.Equal(t, calls, list(t, addrB, func(f *port.TransactionFilter) { f.MethodID = "0xA9059CBB" }, 100, 0))
		assert.Empty(t, list(t, addrB, func(f *port.TransactionFilter) { f.MethodID = "0x12345678" }, 100, 0))
	})

	t.Run("GasUsed", func(t *testing.T) {
		assert.Equal(t, []common.Hash{call(1), call(2), call(3), call(4)}, list(t, addrB, func(f *port.TransactionFilter) { f.MinGasUsed = histU64(60000) }, 100, 0))
		assert.Equal(t, []common.Hash{tr(1), tr(2), tr(3), tr(4)}, list(t, addrB, func(f *port.TransactionFilter) { f.MaxGasUsed = histU64(21000) }, 100, 0))
		assert.Equal(t, []common.Hash{create}, list(t, addrA, func(f *port.TransactionFilter) { f.MinGasUsed = histU64(50000) }, 100, 0))
	})

	t.Run("FeeDelegation", func(t *testing.T) {
		// No fixture transaction is fee delegated.
		assert.Empty(t, list(t, addrB, func(f *port.TransactionFilter) { f.IsFeeDelegated = histBool(true) }, 100, 0))
		assert.Equal(t, allB, list(t, addrB, func(f *port.TransactionFilter) { f.IsFeeDelegated = histBool(false) }, 100, 0))
	})

	t.Run("PagingAfterFilter", func(t *testing.T) {
		filtered := func(addr common.Address, mod func(f *port.TransactionFilter)) listPage[common.Hash] {
			return func(page port.Page) ([]common.Hash, string, error) {
				f := port.DefaultTransactionFilter()
				if mod != nil {
					mod(f)
				}
				got, next, err := s.GetTransactionsByAddressFiltered(ctx, addr, f, page)
				return histTxHashes(got), next, err
			}
		}
		received := func(f *port.TransactionFilter) { f.TxType = port.TxTypeReceived }
		hash := func(h common.Hash) common.Hash { return h }
		checkPaging(t, allB, hash, filtered(addrB, nil))
		checkPaging(t, []common.Hash{tr(1), tr(2), tr(3), tr(4)}, hash, filtered(addrB, received))
		checkCursorFromOtherList(t, filtered(addrB, nil), filtered(addrA, nil))

		assert.Equal(t, []common.Hash{tr(2), call(2), tr(3)}, list(t, addrB, nil, 3, 2))
		assert.Equal(t, []common.Hash{tr(3), tr(4)}, list(t, addrB, received, 2, 2),
			"the offset counts matching transactions only")
		assert.Empty(t, list(t, addrB, nil, 3, 8))

		// A cursor continues after the last match: the entries it skipped
		// are not read again.
		_, next, err := filtered(addrB, received)(port.FirstPage(1))
		require.NoError(t, err)
		got, _, err := filtered(addrB, received)(port.Page{After: next, Limit: 2})
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{tr(2), tr(3)}, got)
	})

	t.Run("RejectsInvalidFilter", func(t *testing.T) {
		f := port.DefaultTransactionFilter()
		f.FromBlock, f.ToBlock = 3, 2
		_, _, err := s.GetTransactionsByAddressFiltered(ctx, addrB, f, port.FirstPage(100))
		assert.Error(t, err)
	})
}

// testHistBalances checks UpdateBalance, SetBalance, GetAddressBalance and
// GetBalanceHistory: every write records a snapshot in write order, and the
// balance at a block is the last snapshot at or below it. The store under
// test has no node, so an account never recorded reads zero.
func testHistBalances(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	h1, h3 := fixtureHash("bal", 1), fixtureHash("bal", 3)
	write := func(t *testing.T, s historicalStore) {
		t.Helper()
		require.NoError(t, s.UpdateBalance(ctx, addrA, 1, big.NewInt(100), h1))
		require.NoError(t, s.UpdateBalance(ctx, addrA, 3, big.NewInt(-30), h3))
		require.NoError(t, s.SetBalance(ctx, addrA, 5, big.NewInt(500)))
	}
	want := []port.BalanceSnapshot{
		{BlockNumber: 1, Balance: big.NewInt(100), Delta: big.NewInt(100), TxHash: h1},
		{BlockNumber: 3, Balance: big.NewInt(70), Delta: big.NewInt(-30), TxHash: h3},
		{BlockNumber: 5, Balance: big.NewInt(500), Delta: big.NewInt(430), TxHash: common.Hash{}},
	}

	t.Run("BalanceAtBlock", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		write(t, s)
		for block, bal := range map[uint64]int64{0: 500, 1: 100, 2: 100, 3: 70, 4: 70, 5: 500, 2000: 500} {
			got, err := s.GetAddressBalance(ctx, addrA, block)
			require.NoError(t, err)
			histAssertBig(t, big.NewInt(bal), got, "balance at block %d (0 is the latest)", block)
		}
		got, err := s.GetAddressBalance(ctx, unknown, 0)
		require.NoError(t, err)
		histAssertBig(t, new(big.Int), got, "never recorded")
	})

	t.Run("History", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		write(t, s)
		got, _, err := s.GetBalanceHistory(ctx, addrA, 0, math.MaxUint64, port.FirstPage(10))
		require.NoError(t, err)
		histAssertSnapshots(t, want, got)

		got, _, err = s.GetBalanceHistory(ctx, addrA, 3, 5, port.FirstPage(10))
		require.NoError(t, err)
		histAssertSnapshots(t, want[1:], got, "the block range is inclusive")

		got, _, err = s.GetBalanceHistory(ctx, addrA, 2, 4, port.FirstPage(10))
		require.NoError(t, err)
		histAssertSnapshots(t, want[1:2], got)

		history := func(addr common.Address) listPage[port.BalanceSnapshot] {
			return func(page port.Page) ([]port.BalanceSnapshot, string, error) {
				return s.GetBalanceHistory(ctx, addr, 0, math.MaxUint64, page)
			}
		}
		block := func(b port.BalanceSnapshot) uint64 { return b.BlockNumber }
		checkPaging(t, want, block, history(addrA))
		checkCursorFromOtherList(t, history(addrA), history(addrB))

		got, _, err = s.GetBalanceHistory(ctx, unknown, 0, math.MaxUint64, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, got)

		_, _, err = s.GetBalanceHistory(ctx, addrA, 5, 3, port.FirstPage(10))
		assert.Error(t, err, "fromBlock after toBlock")
	})

	t.Run("NegativeBalanceIsRejected", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		write(t, s)
		err := s.UpdateBalance(ctx, addrA, 6, big.NewInt(-501), fixtureHash("bal", 6))
		assert.ErrorIs(t, err, port.ErrNegativeBalance)
		got, err := s.GetAddressBalance(ctx, addrA, 0)
		require.NoError(t, err)
		histAssertBig(t, big.NewInt(500), got, "a rejected update changes nothing")
		hist, _, err := s.GetBalanceHistory(ctx, addrA, 0, math.MaxUint64, port.FirstPage(10))
		require.NoError(t, err)
		assert.Len(t, hist, 3, "a rejected update records no snapshot")

		require.NoError(t, s.UpdateBalance(ctx, addrA, 6, big.NewInt(-500), fixtureHash("bal", 6)), "reaching zero is allowed")
		assert.ErrorIs(t, s.UpdateBalance(ctx, addrB, 1, big.NewInt(-1), common.Hash{}), port.ErrNegativeBalance, "a new account starts at zero")
	})
}

// testHistCounts checks GetBlockCount and GetTransactionCount.
func testHistCounts(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	s := open[historicalStore](t, newStore)
	n, err := s.GetBlockCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), n)
	n, err = s.GetTransactionCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), n)

	newChain(5).write(t, s)
	n, err = s.GetBlockCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), n, "heights 0..4")
	n, err = s.GetTransactionCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(9), n, "two per block 1..4 and the creation")
}

// testHistMiners checks GetTopMiners: blocks per miner, share of the range,
// last block, and fees (gas used * gas price) earned, most blocks first.
func testHistMiners(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	c := newChain(5)
	// Fees per block: transfer 21000*2 gwei + call 60000*3 gwei; block 3
	// adds the creation 100000*2 gwei.
	x := port.MinerStats{Address: minerX, BlockCount: 3, LastBlockNumber: 4, LastBlockTime: baseTime + 48, Percentage: 60, TotalRewards: histGwei(444_000)}
	y := port.MinerStats{Address: minerY, BlockCount: 2, LastBlockNumber: 3, LastBlockTime: baseTime + 36, Percentage: 40, TotalRewards: histGwei(644_000)}

	t.Run("EmptyStore", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		got, err := s.GetTopMiners(ctx, 10, 0, 0)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("AllTime", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		c.write(t, s)
		got, err := s.GetTopMiners(ctx, 10, 0, 0)
		require.NoError(t, err)
		histAssertMiners(t, []port.MinerStats{x, y}, got)

		got, err = s.GetTopMiners(ctx, 1, 0, 0)
		require.NoError(t, err)
		histAssertMiners(t, []port.MinerStats{x}, got, "limit")
	})

	t.Run("Range", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		c.write(t, s)
		got, err := s.GetTopMiners(ctx, 10, 2, 3)
		require.NoError(t, err)
		require.Len(t, got, 2)
		byAddr := map[common.Address]port.MinerStats{got[0].Address: got[0], got[1].Address: got[1]}
		histAssertMiners(t, []port.MinerStats{
			{Address: minerX, BlockCount: 1, LastBlockNumber: 2, LastBlockTime: baseTime + 24, Percentage: 50, TotalRewards: histGwei(222_000)},
			{Address: minerY, BlockCount: 1, LastBlockNumber: 3, LastBlockTime: baseTime + 36, Percentage: 50, TotalRewards: histGwei(422_000)},
		}, []port.MinerStats{byAddr[minerX], byAddr[minerY]}, "the range is inclusive")

		got, err = s.GetTopMiners(ctx, 10, 3, 100)
		require.NoError(t, err)
		require.Len(t, got, 2, "a range past the head stops at the head")
		for _, m := range got {
			assert.Equal(t, uint64(1), m.BlockCount, "blocks 3 and 4")
			assert.InDelta(t, 50.0, m.Percentage, 1e-9)
		}
	})
}

// testHistTokenBalances checks GetTokenBalances: the net amount of the
// Transfer logs to and from the address per contract, positive balances
// only, filtered by token type.
func testHistTokenBalances(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	s := open[historicalStore](t, newStore)
	got, err := s.GetTokenBalances(ctx, addrA, "")
	require.NoError(t, err)
	assert.Empty(t, got, "empty store")

	newChain(5).write(t, s)
	got, err = s.GetTokenBalances(ctx, addrA, "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, addrC, got[0].ContractAddress)
	assert.Equal(t, string(port.TokenStandardERC20), got[0].TokenType)
	histAssertBig(t, big.NewInt(8), got[0].Balance, "1 + 3 + 4: the call in block 2 failed")
	assert.Empty(t, got[0].TokenID)

	got, err = s.GetTokenBalances(ctx, addrA, "ERC20")
	require.NoError(t, err)
	assert.Len(t, got, 1)
	got, err = s.GetTokenBalances(ctx, addrA, "ERC721")
	require.NoError(t, err)
	assert.Empty(t, got, "filtered by token type")

	got, err = s.GetTokenBalances(ctx, addrB, "")
	require.NoError(t, err)
	assert.Empty(t, got, "B only sent tokens")
	got, err = s.GetTokenBalances(ctx, unknown, "")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// testHistGasStats checks GetGasStatsByBlockRange, GetGasStatsByAddress,
// GetTopAddressesByGasUsed and GetTopAddressesByTxCount over the stored
// blocks and receipts; senders are counted, missing blocks skipped.
func testHistGasStats(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	c := newChain(5)
	// Sent in blocks 1..4: A four transfers (21000 gas, 2 gwei) and the
	// creation (100000 gas, 2 gwei); B four calls (60000 gas, 3 gwei).
	statsA := port.AddressGasStats{Address: addrA, TotalGasUsed: 184_000, TransactionCount: 5, AverageGasPerTx: 36_800, TotalFeesPaid: histGwei(368_000)}
	statsB := port.AddressGasStats{Address: addrB, TotalGasUsed: 240_000, TransactionCount: 4, AverageGasPerTx: 60_000, TotalFeesPaid: histGwei(720_000)}

	t.Run("ByBlockRange", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		c.write(t, s)
		g, err := s.GetGasStatsByBlockRange(ctx, 1, 4)
		require.NoError(t, err)
		assert.Equal(t, uint64(4), g.BlockCount)
		assert.Equal(t, uint64(9), g.TransactionCount)
		assert.Equal(t, uint64(424_000), g.TotalGasUsed)
		assert.Equal(t, uint64(120_000_000), g.TotalGasLimit)
		assert.Equal(t, uint64(106_000), g.AverageGasUsed)
		histAssertBig(t, big.NewInt(2_444_444_444), g.AverageGasPrice, "(5*2 + 4*3) gwei / 9")

		g, err = s.GetGasStatsByBlockRange(ctx, 0, 100)
		require.NoError(t, err)
		assert.Equal(t, uint64(5), g.BlockCount, "missing blocks are skipped")
		assert.Equal(t, uint64(150_000_000), g.TotalGasLimit)
		assert.Equal(t, uint64(84_800), g.AverageGasUsed)

		g, err = s.GetGasStatsByBlockRange(ctx, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), g.BlockCount)
		assert.Equal(t, uint64(0), g.TransactionCount)
		histAssertBig(t, new(big.Int), g.AverageGasPrice, "no transactions")

		_, err = s.GetGasStatsByBlockRange(ctx, 3, 2)
		assert.Error(t, err)
	})

	t.Run("ByAddress", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		c.write(t, s)
		g, err := s.GetGasStatsByAddress(ctx, addrA, 1, 4)
		require.NoError(t, err)
		histAssertGasStats(t, statsA, *g)
		g, err = s.GetGasStatsByAddress(ctx, addrB, 0, 100)
		require.NoError(t, err)
		histAssertGasStats(t, statsB, *g)
		g, err = s.GetGasStatsByAddress(ctx, addrA, 3, 3)
		require.NoError(t, err)
		histAssertGasStats(t, port.AddressGasStats{Address: addrA, TotalGasUsed: 121_000, TransactionCount: 2, AverageGasPerTx: 60_500, TotalFeesPaid: histGwei(242_000)}, *g)
		g, err = s.GetGasStatsByAddress(ctx, addrC, 1, 4)
		require.NoError(t, err)
		histAssertGasStats(t, port.AddressGasStats{Address: addrC, TotalFeesPaid: new(big.Int)}, *g, "C never sends")
		_, err = s.GetGasStatsByAddress(ctx, addrA, 3, 2)
		assert.Error(t, err)
	})

	t.Run("TopAddressesByGasUsed", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		c.write(t, s)
		got, err := s.GetTopAddressesByGasUsed(ctx, 10, 1, 4)
		require.NoError(t, err)
		require.Len(t, got, 2)
		histAssertGasStats(t, statsB, got[0], "most gas first")
		histAssertGasStats(t, statsA, got[1])

		got, err = s.GetTopAddressesByGasUsed(ctx, 1, 1, 4)
		require.NoError(t, err)
		require.Len(t, got, 1)
		histAssertGasStats(t, statsB, got[0])

		got, err = s.GetTopAddressesByGasUsed(ctx, 10, 0, 0)
		require.NoError(t, err)
		assert.Empty(t, got, "block 0 has no transactions")
		_, err = s.GetTopAddressesByGasUsed(ctx, 10, 3, 2)
		assert.Error(t, err)
	})

	t.Run("TopAddressesByTxCount", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		c.write(t, s)
		got, err := s.GetTopAddressesByTxCount(ctx, 10, 1, 4)
		require.NoError(t, err)
		assert.Equal(t, []port.AddressActivityStats{
			{Address: addrA, TransactionCount: 5, TotalGasUsed: 184_000, FirstActivityBlock: 1, LastActivityBlock: 4},
			{Address: addrB, TransactionCount: 4, TotalGasUsed: 240_000, FirstActivityBlock: 1, LastActivityBlock: 4},
		}, got, "most transactions first")

		got, err = s.GetTopAddressesByTxCount(ctx, 1, 0, 100)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, addrA, got[0].Address)

		got, err = s.GetTopAddressesByTxCount(ctx, 10, 3, 3)
		require.NoError(t, err)
		assert.Equal(t, []port.AddressActivityStats{
			{Address: addrA, TransactionCount: 2, TotalGasUsed: 121_000, FirstActivityBlock: 3, LastActivityBlock: 3},
			{Address: addrB, TransactionCount: 1, TotalGasUsed: 60_000, FirstActivityBlock: 3, LastActivityBlock: 3},
		}, got)
		_, err = s.GetTopAddressesByTxCount(ctx, 10, 3, 2)
		assert.Error(t, err)
	})

	t.Run("FeesUseEffectiveGasPrice", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		histWithEffectivePrice(newChain(2), histGwei(2)).write(t, s)
		// B's call in block 1: 60000 gas at an effective 2 gwei (fee cap 3 gwei).
		g, err := s.GetGasStatsByAddress(ctx, addrB, 1, 1)
		require.NoError(t, err)
		histAssertBig(t, histGwei(120_000), g.TotalFeesPaid)
		top, err := s.GetTopAddressesByGasUsed(ctx, 10, 1, 1)
		require.NoError(t, err)
		require.Len(t, top, 2)
		histAssertGasStats(t, port.AddressGasStats{Address: addrB, TotalGasUsed: 60_000, TransactionCount: 1, AverageGasPerTx: 60_000, TotalFeesPaid: histGwei(120_000)}, top[0])
	})
}

// testHistAddressStats checks GetAddressStats over the address index: the
// counts by direction and status, value moved by successful transactions,
// gas and its cost at the effective gas price for the account that paid,
// contract calls, counterparties and the first and last block times.
func testHistAddressStats(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	first, last := baseTime+12, baseTime+48

	t.Run("Fixture", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		histWrite(t, s, newChain(5))
		cases := []port.AddressStats{
			{
				Address: addrA, TotalTransactions: 5, SentCount: 5, SuccessCount: 5,
				TotalGasUsed: 184_000, TotalGasCost: histGwei(368_000),
				TotalValueSent: big.NewInt(10), TotalValueReceived: new(big.Int),
				UniqueAddressCount: 1, FirstTransactionTimestamp: first, LastTransactionTimestamp: last,
			},
			{
				Address: addrB, TotalTransactions: 8, SentCount: 4, ReceivedCount: 4, SuccessCount: 7, FailedCount: 1,
				TotalGasUsed: 240_000, TotalGasCost: histGwei(720_000),
				TotalValueSent: new(big.Int), TotalValueReceived: big.NewInt(10),
				ContractInteractionCount: 4, UniqueAddressCount: 2, FirstTransactionTimestamp: first, LastTransactionTimestamp: last,
			},
			{
				Address: addrC, TotalTransactions: 4, ReceivedCount: 4, SuccessCount: 3, FailedCount: 1,
				TotalGasCost: new(big.Int), TotalValueSent: new(big.Int), TotalValueReceived: new(big.Int),
				ContractInteractionCount: 4, UniqueAddressCount: 1, FirstTransactionTimestamp: first, LastTransactionTimestamp: last,
			},
			{
				Address: unknown, TotalGasCost: new(big.Int), TotalValueSent: new(big.Int), TotalValueReceived: new(big.Int),
			},
		}
		for _, want := range cases {
			got, err := s.GetAddressStats(ctx, want.Address)
			require.NoError(t, err)
			histAssertAddressStats(t, want, *got)
		}
	})

	t.Run("FailedTransactionMovesNoValue", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		c := newChain(3)
		c.receipt(fixtureHash("transfer", 2)).Status = model.ReceiptStatusFailed
		histWrite(t, s, c)
		got, err := s.GetAddressStats(ctx, addrA)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), got.FailedCount)
		histAssertBig(t, big.NewInt(1), got.TotalValueSent, "only block 1's transfer succeeded")
		got, err = s.GetAddressStats(ctx, addrB)
		require.NoError(t, err)
		histAssertBig(t, big.NewInt(1), got.TotalValueReceived)
	})

	t.Run("GasCostUsesEffectiveGasPrice", func(t *testing.T) {
		s := open[historicalStore](t, newStore)
		histWrite(t, s, histWithEffectivePrice(newChain(2), histGwei(2)))
		got, err := s.GetAddressStats(ctx, addrB)
		require.NoError(t, err)
		histAssertBig(t, histGwei(120_000), got.TotalGasCost, "60000 gas at 2 gwei")
	})
}

// testBalanceRecordChecker checks that HasBalanceRecord reports exactly the
// accounts with a stored balance, including a recorded zero, and that a
// rejected update records nothing.
func testBalanceRecordChecker(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("RecordedByWrites", func(t *testing.T) {
		s := open[balanceRecordStore](t, newStore)
		has, err := s.HasBalanceRecord(ctx, addrA)
		require.NoError(t, err)
		assert.False(t, has, "empty store")

		require.NoError(t, s.UpdateBalance(ctx, addrA, 1, big.NewInt(5), fixtureHash("bal", 1)))
		require.NoError(t, s.SetBalance(ctx, addrB, 1, new(big.Int)))
		for addr, want := range map[common.Address]bool{addrA: true, addrB: true, addrC: false} {
			has, err := s.HasBalanceRecord(ctx, addr)
			require.NoError(t, err)
			assert.Equal(t, want, has, "%s", addr.Hex())
		}
	})

	t.Run("RejectedUpdateRecordsNothing", func(t *testing.T) {
		s := open[balanceRecordStore](t, newStore)
		assert.ErrorIs(t, s.UpdateBalance(ctx, addrA, 1, big.NewInt(-1), common.Hash{}), port.ErrNegativeBalance)
		has, err := s.HasBalanceRecord(ctx, addrA)
		require.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("ZeroAfterSpendingIsRecorded", func(t *testing.T) {
		s := open[balanceRecordStore](t, newStore)
		require.NoError(t, s.UpdateBalance(ctx, addrA, 1, big.NewInt(5), common.Hash{}))
		require.NoError(t, s.UpdateBalance(ctx, addrA, 2, big.NewInt(-5), common.Hash{}))
		has, err := s.HasBalanceRecord(ctx, addrA)
		require.NoError(t, err)
		assert.True(t, has)
	})
}

// histWrite stores c and the indexes ingest writes beside it: the
// timestamp index and the address index (sender and recipient of every
// transaction, in block order).
func histWrite(t *testing.T, s historicalStore, c *chain) {
	t.Helper()
	ctx := context.Background()
	c.write(t, s)
	for _, b := range c.Blocks {
		require.NoError(t, s.SetBlockTimestamp(ctx, b.Time, b.Number))
		for _, tx := range b.Transactions {
			require.NoError(t, s.AddTransactionToAddressIndex(ctx, tx.From, tx.Hash))
			if tx.To != nil {
				require.NoError(t, s.AddTransactionToAddressIndex(ctx, *tx.To, tx.Hash))
			}
		}
	}
}

// histWithEffectivePrice sets the effective gas price of the fee-market
// calls' receipts below their fee cap, as a chain with a base fee does.
func histWithEffectivePrice(c *chain, price *big.Int) *chain {
	for _, r := range c.Receipts {
		if r.Type == 2 {
			r.EffectiveGasPrice = price
		}
	}
	return c
}

func histU64(v uint64) *uint64 { return &v }

func histBool(v bool) *bool { return &v }

func histGwei(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000)) }

func histNumbers(blocks []*model.Block) []uint64 {
	out := []uint64{}
	for _, b := range blocks {
		out = append(out, b.Number)
	}
	return out
}

func histTxHashes(txs []*port.TransactionWithReceipt) []common.Hash {
	out := []common.Hash{}
	for _, tx := range txs {
		out = append(out, tx.Transaction.Hash)
	}
	return out
}

func histAssertBig(t *testing.T, want, got *big.Int, msgAndArgs ...any) {
	t.Helper()
	require.NotNil(t, got, msgAndArgs...)
	assert.Equal(t, want.String(), got.String(), msgAndArgs...)
}

func histAssertSnapshots(t *testing.T, want, got []port.BalanceSnapshot, msgAndArgs ...any) {
	t.Helper()
	require.Len(t, got, len(want), msgAndArgs...)
	for i := range want {
		assert.Equal(t, want[i].BlockNumber, got[i].BlockNumber, msgAndArgs...)
		assert.Equal(t, want[i].TxHash, got[i].TxHash, msgAndArgs...)
		histAssertBig(t, want[i].Balance, got[i].Balance, msgAndArgs...)
		histAssertBig(t, want[i].Delta, got[i].Delta, msgAndArgs...)
	}
}

func histAssertMiners(t *testing.T, want, got []port.MinerStats, msgAndArgs ...any) {
	t.Helper()
	require.Len(t, got, len(want), msgAndArgs...)
	for i := range want {
		assert.Equal(t, want[i].Address, got[i].Address, msgAndArgs...)
		assert.Equal(t, want[i].BlockCount, got[i].BlockCount, msgAndArgs...)
		assert.Equal(t, want[i].LastBlockNumber, got[i].LastBlockNumber, msgAndArgs...)
		assert.Equal(t, want[i].LastBlockTime, got[i].LastBlockTime, msgAndArgs...)
		assert.InDelta(t, want[i].Percentage, got[i].Percentage, 1e-9, msgAndArgs...)
		histAssertBig(t, want[i].TotalRewards, got[i].TotalRewards, msgAndArgs...)
	}
}

func histAssertGasStats(t *testing.T, want, got port.AddressGasStats, msgAndArgs ...any) {
	t.Helper()
	assert.Equal(t, want.Address, got.Address, msgAndArgs...)
	assert.Equal(t, want.TotalGasUsed, got.TotalGasUsed, msgAndArgs...)
	assert.Equal(t, want.TransactionCount, got.TransactionCount, msgAndArgs...)
	assert.Equal(t, want.AverageGasPerTx, got.AverageGasPerTx, msgAndArgs...)
	histAssertBig(t, want.TotalFeesPaid, got.TotalFeesPaid, msgAndArgs...)
}

func histAssertAddressStats(t *testing.T, want, got port.AddressStats) {
	t.Helper()
	msg := want.Address.Hex()
	histAssertBig(t, want.TotalGasCost, got.TotalGasCost, msg)
	histAssertBig(t, want.TotalValueSent, got.TotalValueSent, msg)
	histAssertBig(t, want.TotalValueReceived, got.TotalValueReceived, msg)
	want.TotalGasCost, want.TotalValueSent, want.TotalValueReceived = nil, nil, nil
	got.TotalGasCost, got.TotalValueSent, got.TotalValueReceived = nil, nil, nil
	assert.Equal(t, want, got, msg)
}
