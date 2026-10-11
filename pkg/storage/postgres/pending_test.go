package postgres

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// TestQueuedWritesReadTheirOwn: writes a block transaction queues are seen
// by its later reads and by the next transaction after the commit; an
// account's balance is read once per transaction and then kept.
func TestQueuedWritesReadTheirOwn(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	addr := common.HexToAddress("0xa1")

	bctx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, s.UpdateBalance(bctx, addr, 1, big.NewInt(100), common.HexToHash("0x01")))
	require.NoError(t, s.UpdateBalance(bctx, addr, 1, big.NewInt(-30), common.HexToHash("0x02")))
	require.NoError(t, s.AddTransactionToAddressIndex(bctx, addr, common.HexToHash("0x01")))
	bt := s.boundTx(bctx)
	require.NotNil(t, bt.pending, "writes are queued")
	got, _, err := s.GetTransactionsByAddress(bctx, addr, port.FirstPage(10))
	require.NoError(t, err)
	assert.Equal(t, []common.Hash{common.HexToHash("0x01")}, got, "a read in the transaction sees its queued writes")
	assert.Nil(t, bt.pending, "the read sent them")
	require.ErrorIs(t, s.UpdateBalance(bctx, addr, 1, big.NewInt(-71), common.Hash{}), port.ErrNegativeBalance, "the kept balance is 70")
	tx.SetHeight(1)
	require.NoError(t, tx.Commit())

	history, _, err := s.GetBalanceHistory(ctx, addr, 0, 10, port.FirstPage(10))
	require.NoError(t, err)
	require.Len(t, history, 2)
	var balances []string
	for _, h := range history {
		balances = append(balances, h.Balance.String())
	}
	assert.ElementsMatch(t, []string{"100", "70"}, balances)
}

// TestQueuedWriteErrorsFailTheCommit: a queued write that fails makes the
// commit fail and nothing of the transaction is stored; a rolled back
// transaction's queued writes are never sent.
func TestQueuedWriteErrorsFailTheCommit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	addr := common.HexToAddress("0xa2")

	bctx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, s.AddTransactionToAddressIndex(bctx, addr, common.HexToHash("0x01")))
	require.NoError(t, s.execLater(bctx, "INSERT INTO no_such_table VALUES (1)"))
	require.Error(t, tx.Commit())
	got, _, err := s.GetTransactionsByAddress(ctx, addr, port.FirstPage(10))
	require.NoError(t, err)
	assert.Empty(t, got, "nothing of the failed transaction is stored")

	bctx, tx, err = s.BeginBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, s.AddTransactionToAddressIndex(bctx, addr, common.HexToHash("0x02")))
	tx.Rollback()
	got, _, err = s.GetTransactionsByAddress(ctx, addr, port.FirstPage(10))
	require.NoError(t, err)
	assert.Empty(t, got, "a rolled back transaction's queued writes are dropped")
}

// TestPrefetchBalances: one prefetch reads the accounts a block changes;
// the block transaction then answers their balances and whether each was
// recorded from what it read and wrote, and the results equal reading each
// account.
func TestPrefetchBalances(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	old, fresh := common.HexToAddress("0xb1"), common.HexToAddress("0xb2")

	bctx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	tx.SetHeight(1)
	require.NoError(t, s.SetBalance(bctx, old, 1, big.NewInt(100)))
	require.NoError(t, tx.Commit())

	bctx, tx, err = s.BeginBlock(ctx)
	require.NoError(t, err)
	tx.SetHeight(2)
	require.NoError(t, s.PrefetchBalances(bctx, []common.Address{fresh, old}))
	bt := s.boundTx(bctx)
	assert.Equal(t, map[common.Address]bool{old: true, fresh: false}, bt.recorded)
	assert.Equal(t, "100", bt.balances[old].String())
	assert.Equal(t, "0", bt.balances[fresh].String())

	has, err := s.HasBalanceRecord(bctx, fresh)
	require.NoError(t, err)
	assert.False(t, has)
	require.NoError(t, s.SetBalance(bctx, fresh, 2, big.NewInt(7)))
	has, err = s.HasBalanceRecord(bctx, fresh)
	require.NoError(t, err)
	assert.True(t, has, "recorded once the transaction writes it")
	require.ErrorIs(t, s.UpdateBalance(bctx, old, 2, big.NewInt(-101), common.Hash{}), port.ErrNegativeBalance, "the prefetched balance is 100")
	require.NoError(t, s.UpdateBalance(bctx, old, 2, big.NewInt(-40), common.Hash{}))
	require.NoError(t, tx.Commit())

	for addr, want := range map[common.Address]string{old: "60", fresh: "7"} {
		has, err := s.HasBalanceRecord(ctx, addr)
		require.NoError(t, err)
		assert.True(t, has)
		history, _, err := s.GetBalanceHistory(ctx, addr, 0, 10, port.FirstPage(10))
		require.NoError(t, err)
		require.NotEmpty(t, history)
		assert.Equal(t, want, history[len(history)-1].Balance.String(), "%s", addr.Hex())
	}
}
