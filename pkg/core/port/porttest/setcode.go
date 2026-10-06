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

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Addresses of the SetCode fixtures.
var (
	setCodeAuthority = common.HexToAddress("0x0000000000000000000000000000000000007a01")
	setCodeOther     = common.HexToAddress("0x0000000000000000000000000000000000007a02")
	setCodeTarget    = common.HexToAddress("0x0000000000000000000000000000000000007b01")
	setCodeTarget2   = common.HexToAddress("0x0000000000000000000000000000000000007b02")
)

// testSetCodeIndex checks SetCodeIndexReader and SetCodeIndexWriter: an
// EIP-7702 authorization is found by its transaction and index and through
// the target, authority and block indexes, address lists are newest first
// and paginated, counts agree with the stored authorizations, and the
// delegation state and per-address statistics follow what was written.
func testSetCodeIndex(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("NotFoundOnEmptyStore", func(t *testing.T) {
		s := open[setCodeStore](t, newStore)
		tx := setCodeTxHash(1, 0)

		_, err := s.GetSetCodeAuthorization(ctx, tx, 0)
		assert.ErrorIs(t, err, port.ErrNotFound)

		byTx, err := s.GetSetCodeAuthorizationsByTx(ctx, tx)
		require.NoError(t, err)
		assert.Empty(t, byTx)
		byBlock, err := s.GetSetCodeAuthorizationsByBlock(ctx, 1)
		require.NoError(t, err)
		assert.Empty(t, byBlock)
		byTarget, err := s.GetSetCodeAuthorizationsByTarget(ctx, setCodeTarget, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, byTarget)
		byAuthority, err := s.GetSetCodeAuthorizationsByAuthority(ctx, setCodeAuthority, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, byAuthority)
		recent, err := s.GetRecentSetCodeAuthorizations(ctx, 10)
		require.NoError(t, err)
		assert.Empty(t, recent)

		n, err := s.GetSetCodeAuthorizationsCountByTarget(ctx, setCodeTarget)
		require.NoError(t, err)
		assert.Zero(t, n)
		n, err = s.GetSetCodeAuthorizationsCountByAuthority(ctx, setCodeAuthority)
		require.NoError(t, err)
		assert.Zero(t, n)
		n, err = s.GetSetCodeTransactionCount(ctx)
		require.NoError(t, err)
		assert.Zero(t, n)

		stats, err := s.GetAddressSetCodeStats(ctx, setCodeAuthority)
		require.NoError(t, err, "no activity is zero-value stats, not an error")
		require.NotNil(t, stats)
		assert.Equal(t, setCodeAuthority, stats.Address)
		assert.Zero(t, stats.AsTargetCount)
		assert.Zero(t, stats.AsAuthorityCount)
		assert.Nil(t, stats.CurrentDelegation)

		state, err := s.GetAddressDelegationState(ctx, setCodeAuthority)
		require.NoError(t, err, "no delegation is a state, not an error")
		require.NotNil(t, state)
		assert.Equal(t, setCodeAuthority, state.Address)
		assert.False(t, state.HasDelegation)
		assert.Nil(t, state.DelegationTarget)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		s := open[setCodeStore](t, newStore)
		want := setCodeRecord(7, 2, 0, setCodeAuthority, setCodeTarget)
		want.Applied = false
		want.Error = port.SetCodeErrNonceMismatch
		require.NoError(t, s.SaveSetCodeAuthorization(ctx, want))

		got, err := s.GetSetCodeAuthorization(ctx, want.TxHash, want.AuthIndex)
		require.NoError(t, err)
		setCodeAssertRecord(t, want, got)

		_, err = s.GetSetCodeAuthorization(ctx, want.TxHash, 1)
		assert.ErrorIs(t, err, port.ErrNotFound, "another index of the same transaction")

		for name, list := range map[string]func() ([]*port.SetCodeAuthorizationRecord, error){
			"ByTx": func() ([]*port.SetCodeAuthorizationRecord, error) {
				return s.GetSetCodeAuthorizationsByTx(ctx, want.TxHash)
			},
			"ByBlock": func() ([]*port.SetCodeAuthorizationRecord, error) { return s.GetSetCodeAuthorizationsByBlock(ctx, 7) },
			"ByTarget": func() ([]*port.SetCodeAuthorizationRecord, error) {
				return s.GetSetCodeAuthorizationsByTarget(ctx, setCodeTarget, 10, 0)
			},
			"ByAuthority": func() ([]*port.SetCodeAuthorizationRecord, error) {
				return s.GetSetCodeAuthorizationsByAuthority(ctx, setCodeAuthority, 10, 0)
			},
			"Recent": func() ([]*port.SetCodeAuthorizationRecord, error) { return s.GetRecentSetCodeAuthorizations(ctx, 10) },
		} {
			got, err := list()
			require.NoError(t, err, name)
			require.Len(t, got, 1, name)
			setCodeAssertRecord(t, want, got[0])
		}

		other, err := s.GetSetCodeAuthorizationsByBlock(ctx, 8)
		require.NoError(t, err)
		assert.Empty(t, other, "another block")
		other, err = s.GetSetCodeAuthorizationsByTarget(ctx, setCodeAuthority, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, other, "the authority is not a target")
		other, err = s.GetSetCodeAuthorizationsByAuthority(ctx, setCodeTarget, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, other, "the target is not an authority")
	})

	t.Run("BatchSaveIndexesEveryRecord", func(t *testing.T) {
		s := open[setCodeStore](t, newStore)
		require.NoError(t, s.SaveSetCodeAuthorizations(ctx, nil), "an empty batch is a no-op")

		// Block 4: tx 0 carries three authorizations, tx 1 one. Block 5: one.
		records := []*port.SetCodeAuthorizationRecord{
			setCodeRecord(4, 0, 0, setCodeAuthority, setCodeTarget),
			setCodeRecord(4, 0, 1, setCodeOther, setCodeTarget2),
			setCodeRecord(4, 0, 2, setCodeAuthority, setCodeTarget2),
			setCodeRecord(4, 1, 0, setCodeOther, setCodeTarget),
			setCodeRecord(5, 0, 0, setCodeAuthority, setCodeTarget),
		}
		require.NoError(t, s.SaveSetCodeAuthorizations(ctx, records))

		for _, want := range records {
			got, err := s.GetSetCodeAuthorization(ctx, want.TxHash, want.AuthIndex)
			require.NoError(t, err)
			setCodeAssertRecord(t, want, got)
		}

		byTx, err := s.GetSetCodeAuthorizationsByTx(ctx, records[0].TxHash)
		require.NoError(t, err)
		require.Len(t, byTx, 3)
		for i, got := range byTx {
			assert.Equal(t, i, got.AuthIndex, "authorizations of a transaction are in authorization list order")
		}

		byBlock, err := s.GetSetCodeAuthorizationsByBlock(ctx, 4)
		require.NoError(t, err)
		require.Len(t, byBlock, 4)
		for i, got := range byBlock {
			assert.Equal(t, records[i].TxHash, got.TxHash, "block order: transaction, then authorization index")
			assert.Equal(t, records[i].AuthIndex, got.AuthIndex)
		}
		byBlock, err = s.GetSetCodeAuthorizationsByBlock(ctx, 5)
		require.NoError(t, err)
		require.Len(t, byBlock, 1)
		assert.Equal(t, records[4].TxHash, byBlock[0].TxHash)
	})

	t.Run("NewestFirstAndPagination", func(t *testing.T) {
		s := open[setCodeStore](t, newStore)
		for b := uint64(1); b <= 5; b++ {
			require.NoError(t, s.SaveSetCodeAuthorization(ctx, setCodeRecord(b, 0, 0, setCodeAuthority, setCodeTarget)))
		}

		pages := map[string]func(limit, offset int) ([]*port.SetCodeAuthorizationRecord, error){
			"ByTarget": func(limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
				return s.GetSetCodeAuthorizationsByTarget(ctx, setCodeTarget, limit, offset)
			},
			"ByAuthority": func(limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
				return s.GetSetCodeAuthorizationsByAuthority(ctx, setCodeAuthority, limit, offset)
			},
		}
		for name, page := range pages {
			got, err := page(2, 0)
			require.NoError(t, err, name)
			assert.Equal(t, []uint64{5, 4}, setCodeBlocks(got), "%s: newest first", name)
			got, err = page(2, 2)
			require.NoError(t, err, name)
			assert.Equal(t, []uint64{3, 2}, setCodeBlocks(got), "%s: second page", name)
			got, err = page(2, 4)
			require.NoError(t, err, name)
			assert.Equal(t, []uint64{1}, setCodeBlocks(got), "%s: last page is short", name)
			got, err = page(2, 5)
			require.NoError(t, err, name)
			assert.Empty(t, got, "%s: offset at the end", name)
			got, err = page(2, 50)
			require.NoError(t, err, name)
			assert.Empty(t, got, "%s: offset past the end", name)
		}

		recent, err := s.GetRecentSetCodeAuthorizations(ctx, 3)
		require.NoError(t, err)
		assert.Equal(t, []uint64{5, 4, 3}, setCodeBlocks(recent), "recent: newest first, bounded by limit")
		recent, err = s.GetRecentSetCodeAuthorizations(ctx, 50)
		require.NoError(t, err)
		assert.Equal(t, []uint64{5, 4, 3, 2, 1}, setCodeBlocks(recent))
	})

	t.Run("CountsMatchRecords", func(t *testing.T) {
		s := open[setCodeStore](t, newStore)
		require.NoError(t, s.SaveSetCodeAuthorizations(ctx, []*port.SetCodeAuthorizationRecord{
			setCodeRecord(1, 0, 0, setCodeAuthority, setCodeTarget),
			setCodeRecord(2, 0, 0, setCodeAuthority, setCodeTarget2),
			setCodeRecord(3, 0, 0, setCodeOther, setCodeTarget),
			setCodeRecord(4, 0, 0, setCodeAuthority, setCodeTarget),
		}))
		for _, c := range []struct {
			target bool
			addr   common.Address
			want   int
		}{
			{true, setCodeTarget, 3},
			{true, setCodeTarget2, 1},
			{true, setCodeAuthority, 0},
			{false, setCodeAuthority, 3},
			{false, setCodeOther, 1},
			{false, setCodeTarget, 0},
		} {
			var n int
			var list []*port.SetCodeAuthorizationRecord
			var err error
			if c.target {
				n, err = s.GetSetCodeAuthorizationsCountByTarget(ctx, c.addr)
				require.NoError(t, err)
				list, err = s.GetSetCodeAuthorizationsByTarget(ctx, c.addr, 100, 0)
			} else {
				n, err = s.GetSetCodeAuthorizationsCountByAuthority(ctx, c.addr)
				require.NoError(t, err)
				list, err = s.GetSetCodeAuthorizationsByAuthority(ctx, c.addr, 100, 0)
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, n, "count of %s (target=%v)", c.addr.Hex(), c.target)
			assert.Len(t, list, n, "count agrees with the list of %s", c.addr.Hex())
		}
		n, err := s.GetSetCodeTransactionCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 4, n, "four transactions with one authorization each")
	})

	t.Run("ResaveIsIdempotent", func(t *testing.T) {
		s := open[setCodeStore](t, newStore)
		r := setCodeRecord(3, 1, 0, setCodeAuthority, setCodeTarget)
		require.NoError(t, s.SaveSetCodeAuthorization(ctx, r))
		require.NoError(t, s.SaveSetCodeAuthorization(ctx, r))
		require.NoError(t, s.SaveSetCodeAuthorizations(ctx, []*port.SetCodeAuthorizationRecord{r}))

		n, err := s.GetSetCodeAuthorizationsCountByTarget(ctx, setCodeTarget)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		n, err = s.GetSetCodeAuthorizationsCountByAuthority(ctx, setCodeAuthority)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		n, err = s.GetSetCodeTransactionCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		for _, list := range [][]*port.SetCodeAuthorizationRecord{
			setCodeMust(s.GetSetCodeAuthorizationsByTx(ctx, r.TxHash)),
			setCodeMust(s.GetSetCodeAuthorizationsByBlock(ctx, 3)),
			setCodeMust(s.GetRecentSetCodeAuthorizations(ctx, 10)),
		} {
			assert.Len(t, list, 1, "writing the same authorization again does not duplicate it")
		}
	})

	t.Run("TransactionCountCountsTransactions", func(t *testing.T) {
		knownDefect(t, "GetSetCodeTransactionCount counts authorizations, not transactions")
		s := open[setCodeStore](t, newStore)
		require.NoError(t, s.SaveSetCodeAuthorizations(ctx, []*port.SetCodeAuthorizationRecord{
			setCodeRecord(1, 0, 0, setCodeAuthority, setCodeTarget),
			setCodeRecord(1, 0, 1, setCodeOther, setCodeTarget),
			setCodeRecord(2, 0, 0, setCodeAuthority, setCodeTarget2),
		}))
		n, err := s.GetSetCodeTransactionCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 2, n, "two SetCode transactions, one of them with two authorizations")
	})

	t.Run("LargeAuthIndex", func(t *testing.T) {
		knownDefect(t, "authorization index above 255 is truncated in the target/authority/block indexes")
		s := open[setCodeStore](t, newStore)
		r := setCodeRecord(9, 0, 256, setCodeAuthority, setCodeTarget)
		require.NoError(t, s.SaveSetCodeAuthorization(ctx, r))
		for name, list := range map[string][]*port.SetCodeAuthorizationRecord{
			"ByTarget":    setCodeMust(s.GetSetCodeAuthorizationsByTarget(ctx, setCodeTarget, 10, 0)),
			"ByAuthority": setCodeMust(s.GetSetCodeAuthorizationsByAuthority(ctx, setCodeAuthority, 10, 0)),
			"ByBlock":     setCodeMust(s.GetSetCodeAuthorizationsByBlock(ctx, 9)),
			"Recent":      setCodeMust(s.GetRecentSetCodeAuthorizations(ctx, 10)),
		} {
			require.Len(t, list, 1, name)
			assert.Equal(t, 256, list[0].AuthIndex, name)
		}
	})

	t.Run("DelegationSetThenCleared", func(t *testing.T) {
		s := open[setCodeStore](t, newStore)
		target := setCodeTarget
		updated := time.Unix(int64(baseTime), 0).UTC()
		require.NoError(t, s.UpdateAddressDelegationState(ctx, &port.AddressDelegationState{
			Address: setCodeAuthority, HasDelegation: true, DelegationTarget: &target,
			LastUpdatedBlock: 3, LastUpdatedTxHash: setCodeTxHash(3, 0), UpdatedAt: updated,
		}))

		got, err := s.GetAddressDelegationState(ctx, setCodeAuthority)
		require.NoError(t, err)
		assert.Equal(t, setCodeAuthority, got.Address)
		assert.True(t, got.HasDelegation)
		require.NotNil(t, got.DelegationTarget)
		assert.Equal(t, setCodeTarget, *got.DelegationTarget)
		assert.Equal(t, uint64(3), got.LastUpdatedBlock)
		assert.Equal(t, setCodeTxHash(3, 0), got.LastUpdatedTxHash)
		assert.True(t, updated.Equal(got.UpdatedAt), "UpdatedAt %v, want %v", got.UpdatedAt, updated)

		other, err := s.GetAddressDelegationState(ctx, setCodeOther)
		require.NoError(t, err)
		assert.False(t, other.HasDelegation, "another address is unaffected")

		require.NoError(t, s.UpdateAddressDelegationState(ctx, &port.AddressDelegationState{
			Address: setCodeAuthority, HasDelegation: false,
			LastUpdatedBlock: 6, LastUpdatedTxHash: setCodeTxHash(6, 0), UpdatedAt: updated.Add(time.Minute),
		}))
		got, err = s.GetAddressDelegationState(ctx, setCodeAuthority)
		require.NoError(t, err)
		assert.False(t, got.HasDelegation, "the later update replaces the state")
		assert.Nil(t, got.DelegationTarget)
		assert.Equal(t, uint64(6), got.LastUpdatedBlock)
		assert.Equal(t, setCodeTxHash(6, 0), got.LastUpdatedTxHash)
	})

	t.Run("StatsIncrement", func(t *testing.T) {
		s := open[setCodeStore](t, newStore)
		require.NoError(t, s.IncrementSetCodeStats(ctx, setCodeAuthority, true, false, 1))
		require.NoError(t, s.IncrementSetCodeStats(ctx, setCodeAuthority, false, true, 2))
		require.NoError(t, s.IncrementSetCodeStats(ctx, setCodeAuthority, true, true, 3))

		stats, err := s.GetAddressSetCodeStats(ctx, setCodeAuthority)
		require.NoError(t, err)
		assert.Equal(t, setCodeAuthority, stats.Address)
		assert.Equal(t, 2, stats.AsTargetCount)
		assert.Equal(t, 2, stats.AsAuthorityCount)
		assert.Equal(t, uint64(3), stats.LastActivityBlock, "the block of the latest activity")

		other, err := s.GetAddressSetCodeStats(ctx, setCodeOther)
		require.NoError(t, err)
		assert.Zero(t, other.AsTargetCount, "another address is unaffected")
		assert.Zero(t, other.AsAuthorityCount)
	})

	t.Run("StatsActivityTimeIsBlockTime", func(t *testing.T) {
		probe := newStore(t)
		if _, ok := probe.(port.BlockWriter); !ok {
			t.Skip("the store does not store blocks")
		}
		s := probe.(setCodeStore)
		blk := &model.Block{Hash: fixtureHash("setcode-block", 3), Number: 3, Time: baseTime + 36, GasLimit: 30_000_000}
		require.NoError(t, probe.(port.BlockWriter).SetBlock(ctx, blk))
		require.NoError(t, s.IncrementSetCodeStats(ctx, setCodeAuthority, true, false, 3))

		stats, err := s.GetAddressSetCodeStats(ctx, setCodeAuthority)
		require.NoError(t, err)
		want := time.Unix(int64(blk.Time), 0)
		assert.True(t, want.Equal(stats.LastActivityTime), "LastActivityTime %v, want the block time %v (reindexing is deterministic)", stats.LastActivityTime, want)
	})

	t.Run("StatsFollowDelegation", func(t *testing.T) {
		knownDefect(t, "AddressSetCodeStats.CurrentDelegation is never set from the delegation state")
		s := open[setCodeStore](t, newStore)
		target := setCodeTarget
		require.NoError(t, s.UpdateAddressDelegationState(ctx, &port.AddressDelegationState{
			Address: setCodeAuthority, HasDelegation: true, DelegationTarget: &target, LastUpdatedBlock: 3,
		}))
		require.NoError(t, s.IncrementSetCodeStats(ctx, setCodeAuthority, false, true, 3))
		stats, err := s.GetAddressSetCodeStats(ctx, setCodeAuthority)
		require.NoError(t, err)
		require.NotNil(t, stats.CurrentDelegation)
		assert.Equal(t, setCodeTarget, *stats.CurrentDelegation)
	})
}

// setCodeTxHash returns the hash of the fixture SetCode transaction at a
// block and transaction index.
func setCodeTxHash(block, txIndex uint64) common.Hash {
	return fixtureHash("setcode-tx", block*1000+txIndex)
}

// setCodeRecord returns an applied authorization of authority delegating to
// target, in the fixture SetCode transaction at block and txIndex.
func setCodeRecord(block, txIndex uint64, authIndex int, authority, target common.Address) *port.SetCodeAuthorizationRecord {
	return &port.SetCodeAuthorizationRecord{
		TxHash:           setCodeTxHash(block, txIndex),
		BlockNumber:      block,
		BlockHash:        fixtureHash("setcode-block", block),
		TxIndex:          txIndex,
		AuthIndex:        authIndex,
		TargetAddress:    target,
		AuthorityAddress: authority,
		ChainID:          big.NewInt(1),
		Nonce:            uint64(authIndex) + 7,
		YParity:          1,
		R:                big.NewInt(int64(block) + 11),
		S:                big.NewInt(int64(authIndex) + 13),
		Applied:          true,
		Error:            port.SetCodeErrNone,
		Timestamp:        time.Unix(int64(baseTime+12*block), 0).UTC(),
	}
}

// setCodeAssertRecord compares authorizations field by field through their
// JSON form, which compares big integers and times by value.
func setCodeAssertRecord(t *testing.T, want, got *port.SetCodeAuthorizationRecord) {
	t.Helper()
	require.NotNil(t, got)
	w, err := json.Marshal(want)
	require.NoError(t, err)
	g, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, string(w), string(g))
}

// setCodeBlocks returns the block numbers of records, in order.
func setCodeBlocks(records []*port.SetCodeAuthorizationRecord) []uint64 {
	out := make([]uint64, 0, len(records))
	for _, r := range records {
		out = append(out, r.BlockNumber)
	}
	return out
}

// setCodeMust returns the records of a list call, or nil on error (the
// following length check then fails).
func setCodeMust(records []*port.SetCodeAuthorizationRecord, err error) []*port.SetCodeAuthorizationRecord {
	if err != nil {
		return nil
	}
	return records
}
