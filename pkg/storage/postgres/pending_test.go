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
