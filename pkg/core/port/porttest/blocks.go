package porttest

import (
	"bytes"
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// testBlocks checks BlockReader and BlockWriter: blocks, transactions and
// receipts come back as they were stored, under the hashes the chain
// reports, and missing data is port.ErrNotFound.
func testBlocks(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	c := newChain(5)

	t.Run("NotFoundOnEmptyStore", func(t *testing.T) {
		s := open[blockStore](t, newStore)
		_, err := s.GetBlock(ctx, 0)
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetBlockByHash(ctx, c.Blocks[0].Hash)
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, _, err = s.GetTransaction(ctx, c.Blocks[1].Transactions[0].Hash)
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetReceipt(ctx, c.Blocks[1].Transactions[0].Hash)
		assert.ErrorIs(t, err, port.ErrNotFound)
		blocks, err := s.GetBlocks(ctx, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, blocks)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		s := open[blockStore](t, newStore)
		c.write(t, s)
		for _, want := range c.Blocks {
			got, err := s.GetBlock(ctx, want.Number)
			require.NoError(t, err, "block %d", want.Number)
			assertBlock(t, want, got)

			got, err = s.GetBlockByHash(ctx, want.Hash)
			require.NoError(t, err, "block %s", want.Hash.Hex())
			assertBlock(t, want, got)

			for i, tx := range want.Transactions {
				gotTx, loc, err := s.GetTransaction(ctx, tx.Hash)
				require.NoError(t, err, "tx %s", tx.Hash.Hex())
				assertTx(t, tx, gotTx)
				assert.Equal(t, port.TxLocation{BlockHeight: want.Number, TxIndex: uint64(i), BlockHash: want.Hash}, *loc)

				gotR, err := s.GetReceipt(ctx, tx.Hash)
				require.NoError(t, err, "receipt %s", tx.Hash.Hex())
				assertReceipt(t, c.receipt(tx.Hash), gotR)
			}
		}
	})

	t.Run("GetBlocksRangeSkipsMissing", func(t *testing.T) {
		s := open[blockStore](t, newStore)
		for _, b := range []*model.Block{c.Blocks[1], c.Blocks[3], c.Blocks[4]} {
			require.NoError(t, s.SetBlock(ctx, b))
		}
		got, err := s.GetBlocks(ctx, 0, 3)
		require.NoError(t, err)
		require.Len(t, got, 2, "heights 1 and 3; 0 and 2 are missing")
		assert.Equal(t, uint64(1), got[0].Number)
		assert.Equal(t, uint64(3), got[1].Number)

		got, err = s.GetBlocks(ctx, 4, 4)
		require.NoError(t, err)
		require.Len(t, got, 1, "the range is inclusive")
		assert.Equal(t, c.Blocks[4].Hash, got[0].Hash)
	})

	t.Run("SetBlockReplaces", func(t *testing.T) {
		s := open[blockStore](t, newStore)
		require.NoError(t, s.SetBlock(ctx, c.Blocks[2]))
		other := *c.Blocks[2]
		other.Extra = []byte("replacement")
		require.NoError(t, s.SetBlock(ctx, &other))
		got, err := s.GetBlock(ctx, 2)
		require.NoError(t, err)
		assert.Equal(t, []byte("replacement"), got.Extra)
	})

	t.Run("RejectsNil", func(t *testing.T) {
		s := open[blockStore](t, newStore)
		assert.Error(t, s.SetBlock(ctx, nil))
		assert.Error(t, s.SetReceipt(ctx, nil))
	})
}

// testReader checks the Reader methods beyond BlockReader.
func testReader(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	c := newChain(5)

	t.Run("LatestHeight", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		_, err := s.GetLatestHeight(ctx)
		assert.ErrorIs(t, err, port.ErrNotFound, "nothing indexed yet")
		c.write(t, s)
		h, err := s.GetLatestHeight(ctx)
		require.NoError(t, err)
		assert.Equal(t, c.head(), h)
	})

	t.Run("BatchReads", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		c.write(t, s)
		a, b := c.Blocks[1].Transactions[0], c.Blocks[4].Transactions[1]
		txs, locs, err := s.GetTransactions(ctx, []common.Hash{a.Hash, b.Hash})
		require.NoError(t, err)
		require.Len(t, txs, 2)
		require.Len(t, locs, 2)
		assertTx(t, a, txs[0])
		assertTx(t, b, txs[1])
		assert.Equal(t, uint64(4), locs[1].BlockHeight)
		assert.Equal(t, uint64(1), locs[1].TxIndex)

		rs, err := s.GetReceipts(ctx, []common.Hash{b.Hash, a.Hash})
		require.NoError(t, err)
		require.Len(t, rs, 2)
		assert.Equal(t, b.Hash, rs[0].TxHash, "receipts follow the order of the hashes")
		assert.Equal(t, a.Hash, rs[1].TxHash)
	})

	t.Run("BatchReadsReportMissing", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		c.write(t, s)
		a := c.Blocks[1].Transactions[0]
		missing := fixtureHash("missing", 0)
		txs, locs, err := s.GetTransactions(ctx, []common.Hash{missing, a.Hash})
		assert.ErrorIs(t, err, port.ErrNotFound, "the first error is returned")
		require.Len(t, txs, 2)
		assert.Nil(t, txs[0], "a missing hash leaves a nil entry")
		assert.Nil(t, locs[0])
		assertTx(t, a, txs[1])

		rs, err := s.GetReceipts(ctx, []common.Hash{a.Hash, missing})
		assert.ErrorIs(t, err, port.ErrNotFound)
		require.Len(t, rs, 2)
		assert.Equal(t, a.Hash, rs[0].TxHash)
		assert.Nil(t, rs[1])
	})

	t.Run("ReceiptsByBlock", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		c.write(t, s)
		b := c.Blocks[3]
		byNumber, err := s.GetReceiptsByBlockNumber(ctx, b.Number)
		require.NoError(t, err)
		require.Len(t, byNumber, len(b.Transactions))
		for i, tx := range b.Transactions {
			assert.Equal(t, tx.Hash, byNumber[i].TxHash, "transaction order")
		}
		byHash, err := s.GetReceiptsByBlockHash(ctx, b.Hash)
		require.NoError(t, err)
		require.Len(t, byHash, len(b.Transactions))
		assert.Equal(t, byNumber[2].TxHash, byHash[2].TxHash)

		empty, err := s.GetReceiptsByBlockNumber(ctx, 0)
		require.NoError(t, err)
		assert.Empty(t, empty, "block 0 has no transactions")

		_, err = s.GetReceiptsByBlockHash(ctx, fixtureHash("missing", 1))
		assert.Error(t, err, "unknown block hash")
	})

	t.Run("ReceiptsByBlockSkipMissing", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		b := c.Blocks[3]
		require.NoError(t, s.SetBlock(ctx, b))
		require.NoError(t, s.SetReceipt(ctx, c.receipt(b.Transactions[1].Hash)))

		got, err := s.GetReceiptsByBlockNumber(ctx, b.Number)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, b.Transactions[1].Hash, got[0].TxHash)

		missing, err := s.GetMissingReceipts(ctx, b.Number)
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{b.Transactions[0].Hash, b.Transactions[2].Hash}, missing)
	})

	t.Run("Has", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		tx := c.Blocks[2].Transactions[0]
		for _, f := range []func() (bool, error){
			func() (bool, error) { return s.HasBlock(ctx, 2) },
			func() (bool, error) { return s.HasTransaction(ctx, tx.Hash) },
			func() (bool, error) { return s.HasReceipt(ctx, tx.Hash) },
		} {
			ok, err := f()
			require.NoError(t, err)
			assert.False(t, ok)
		}
		c.write(t, s)
		for _, f := range []func() (bool, error){
			func() (bool, error) { return s.HasBlock(ctx, 2) },
			func() (bool, error) { return s.HasTransaction(ctx, tx.Hash) },
			func() (bool, error) { return s.HasReceipt(ctx, tx.Hash) },
		} {
			ok, err := f()
			require.NoError(t, err)
			assert.True(t, ok)
		}
	})

	t.Run("TransactionsByAddress", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		var want []common.Hash
		for _, b := range c.Blocks[1:] {
			tx := b.Transactions[0]
			require.NoError(t, s.AddTransactionToAddressIndex(ctx, addrA, tx.Hash))
			require.NoError(t, s.AddTransactionToAddressIndex(ctx, addrB, b.Transactions[1].Hash))
			want = append(want, tx.Hash)
		}
		byAddr := func(addr common.Address) listPage[common.Hash] {
			return func(page port.Page) ([]common.Hash, string, error) {
				return s.GetTransactionsByAddress(ctx, addr, page)
			}
		}
		checkPaging(t, want, func(h common.Hash) common.Hash { return h }, byAddr(addrA))
		checkCursorFromOtherList(t, byAddr(addrA), byAddr(addrB))

		got, next, err := s.GetTransactionsByAddress(ctx, unknown, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, got)
		assert.Empty(t, next)
	})

	t.Run("TransactionsByAddressResumeAfterAppend", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		first, second := c.Blocks[1].Transactions[0].Hash, c.Blocks[2].Transactions[0].Hash
		require.NoError(t, s.AddTransactionToAddressIndex(ctx, addrA, first))
		require.NoError(t, s.AddTransactionToAddressIndex(ctx, addrA, second))
		_, next, err := s.GetTransactionsByAddress(ctx, addrA, port.FirstPage(1))
		require.NoError(t, err)
		require.NotEmpty(t, next)
		third := c.Blocks[3].Transactions[0].Hash
		require.NoError(t, s.AddTransactionToAddressIndex(ctx, addrA, third))
		got, _, err := s.GetTransactionsByAddress(ctx, addrA, port.Page{After: next, Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, []common.Hash{second, third}, got, "a cursor stays valid while the list grows")
	})
}

// testWriter checks the Writer methods beyond BlockWriter.
func testWriter(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	c := newChain(4)

	t.Run("SetLatestHeightOverwrites", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		require.NoError(t, s.SetLatestHeight(ctx, 7))
		require.NoError(t, s.SetLatestHeight(ctx, 3))
		h, err := s.GetLatestHeight(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(3), h, "the cursor may move back (rollback)")
	})

	t.Run("DeleteBlock", func(t *testing.T) {
		s := open[readerStore](t, newStore)
		c.write(t, s)
		require.NoError(t, s.DeleteBlock(ctx, 2))
		_, err := s.GetBlock(ctx, 2)
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetBlockByHash(ctx, c.Blocks[2].Hash)
		assert.ErrorIs(t, err, port.ErrNotFound, "the hash index goes with the block")
		ok, err := s.HasBlock(ctx, 2)
		require.NoError(t, err)
		assert.False(t, ok)
		_, err = s.GetBlock(ctx, 3)
		assert.NoError(t, err, "other blocks stay")
		assert.NoError(t, s.DeleteBlock(ctx, 2), "deleting a missing block is not an error")
	})
}

// testBlockTransactor checks block transactions: writes made with the
// transaction's context are visible to it, invisible to others until
// Commit, and discarded by Rollback.
func testBlockTransactor(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	c := newChain(3)
	b := c.Blocks[1]

	t.Run("CommitPublishes", func(t *testing.T) {
		s := open[txStore](t, newStore)
		txCtx, tx, err := s.BeginBlock(ctx)
		require.NoError(t, err)
		require.NoError(t, s.SetBlock(txCtx, b))
		require.NoError(t, s.SetLatestHeight(txCtx, b.Number))

		got, err := s.GetBlock(txCtx, b.Number)
		require.NoError(t, err, "the transaction reads its own writes")
		assert.Equal(t, b.Hash, got.Hash)
		_, err = s.GetBlock(ctx, b.Number)
		assert.ErrorIs(t, err, port.ErrNotFound, "invisible outside before Commit")

		require.NoError(t, tx.Commit())
		got, err = s.GetBlock(ctx, b.Number)
		require.NoError(t, err)
		assert.Equal(t, b.Hash, got.Hash)
		h, err := s.GetLatestHeight(ctx)
		require.NoError(t, err)
		assert.Equal(t, b.Number, h)

		assert.ErrorIs(t, tx.Commit(), port.ErrBlockTxDone, "a finished transaction cannot commit again")
		tx.Rollback() // no-op after Commit
		_, err = s.GetBlock(ctx, b.Number)
		assert.NoError(t, err, "Rollback after Commit changes nothing")
	})

	t.Run("RollbackDiscards", func(t *testing.T) {
		s := open[txStore](t, newStore)
		txCtx, tx, err := s.BeginBlock(ctx)
		require.NoError(t, err)
		require.NoError(t, s.SetBlock(txCtx, b))
		for _, tx := range b.Transactions {
			require.NoError(t, s.SetReceipt(txCtx, c.receipt(tx.Hash)))
		}
		tx.Rollback()
		_, err = s.GetBlock(ctx, b.Number)
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetReceipt(ctx, b.Transactions[0].Hash)
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, _, err = s.GetTransaction(ctx, b.Transactions[0].Hash)
		assert.ErrorIs(t, err, port.ErrNotFound)
		assert.ErrorIs(t, tx.Commit(), port.ErrBlockTxDone)
	})

	t.Run("Sequential", func(t *testing.T) {
		s := open[txStore](t, newStore)
		for _, blk := range c.Blocks {
			txCtx, tx, err := s.BeginBlock(ctx)
			require.NoError(t, err)
			tx.SetHeight(blk.Number)
			require.NoError(t, s.SetBlock(txCtx, blk))
			require.NoError(t, s.SetLatestHeight(txCtx, blk.Number))
			require.NoError(t, tx.Commit())
		}
		got, err := s.GetBlocks(ctx, 0, c.head())
		require.NoError(t, err)
		assert.Len(t, got, len(c.Blocks))
	})
}

// assertBlock compares the fields a block keeps through storage.
func assertBlock(t *testing.T, want, got *model.Block) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.Hash, got.Hash)
	assert.Equal(t, want.ParentHash, got.ParentHash)
	assert.Equal(t, want.Number, got.Number)
	assert.Equal(t, want.Time, got.Time)
	assert.Equal(t, want.Miner, got.Miner)
	assert.Equal(t, want.GasLimit, got.GasLimit)
	assert.Equal(t, want.GasUsed, got.GasUsed)
	assert.Zero(t, want.BaseFee.Cmp(got.BaseFee))
	require.Len(t, got.Transactions, len(want.Transactions))
	for i := range want.Transactions {
		assertTx(t, want.Transactions[i], got.Transactions[i])
	}
}

// assertTx compares the fields a transaction keeps through storage.
func assertTx(t *testing.T, want, got *model.Transaction) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.Hash, got.Hash)
	assert.Equal(t, want.Type, got.Type)
	assert.Equal(t, want.From, got.From)
	assert.Equal(t, want.To, got.To)
	assert.Equal(t, want.Nonce, got.Nonce)
	assert.Zero(t, want.Value.Cmp(got.Value))
	assert.True(t, bytes.Equal(want.Input, got.Input), "input %x != %x", want.Input, got.Input)
	assert.Equal(t, want.Gas, got.Gas)
}

// assertReceipt compares the fields a receipt keeps through storage.
func assertReceipt(t *testing.T, want, got *model.Receipt) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.TxHash, got.TxHash)
	assert.Equal(t, want.Status, got.Status)
	assert.Equal(t, want.GasUsed, got.GasUsed)
	assert.Equal(t, want.BlockNumber, got.BlockNumber)
	assert.Equal(t, want.BlockHash, got.BlockHash)
	assert.Equal(t, want.TxIndex, got.TxIndex)
	assert.Equal(t, want.ContractAddress, got.ContractAddress)
	require.Len(t, got.Logs, len(want.Logs))
	for i := range want.Logs {
		assertLog(t, want.Logs[i], got.Logs[i])
	}
}

// assertLog compares the fields a log keeps through storage.
func assertLog(t *testing.T, want, got *model.Log) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.Address, got.Address)
	assert.Equal(t, want.Topics, got.Topics)
	assert.True(t, bytes.Equal(want.Data, got.Data), "data %x != %x", want.Data, got.Data)
	assert.Equal(t, want.BlockNumber, got.BlockNumber)
	assert.Equal(t, want.BlockHash, got.BlockHash)
	assert.Equal(t, want.TxHash, got.TxHash)
	assert.Equal(t, want.TxIndex, got.TxIndex)
	assert.Equal(t, want.Index, got.Index)
}
