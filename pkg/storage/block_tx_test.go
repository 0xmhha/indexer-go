package storage

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	txAddrA = common.HexToAddress("0x00000000000000000000000000000000000000A1")
	txHash1 = common.HexToHash("0x01")
	txHash2 = common.HexToHash("0x02")
)

func addrSeqOf(s *PebbleStorage, a common.Address) uint64 {
	s.addrSeqMu.RLock()
	defer s.addrSeqMu.RUnlock()
	return s.addrSeq[seqKey{seqAddrTx, a}]
}

// writeBlock performs the writes of a small "block" through ctx.
func writeBlock(t *testing.T, s *PebbleStorage, ctx context.Context, height uint64) {
	t.Helper()
	require.NoError(t, s.AddTransactionToAddressIndex(ctx, txAddrA, txHash1))
	require.NoError(t, s.AddTransactionToAddressIndex(ctx, txAddrA, txHash2))
	_ = s.addTxCount(ctx, 2)
	require.NoError(t, s.SetLatestHeight(ctx, height))
}

func TestBlockTxCommitPublishesWritesAndState(t *testing.T) {
	s := newTestPebble(t)
	ctx, tx, err := s.BeginBlock(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()

	writeBlock(t, s, ctx, 9)

	// Staged, not published.
	require.Zero(t, addrSeqOf(s, txAddrA))
	require.Zero(t, s.txCount.Load())
	require.Zero(t, committedKeys(t, s, "/index/addr/"))
	_, err = s.GetLatestHeight(context.Background())
	require.Error(t, err)
	h, err := s.GetLatestHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(9), h, "reads in the block see its writes")

	require.NoError(t, tx.Commit())

	require.Equal(t, uint64(2), addrSeqOf(s, txAddrA))
	require.Equal(t, uint64(2), s.txCount.Load())
	require.Equal(t, int64(2), committedKeys(t, s, "/index/addr/"))
	h, err = s.GetLatestHeight(context.Background())
	require.NoError(t, err)
	require.Equal(t, uint64(9), h)

	tx.Rollback() // no-op after Commit
	require.ErrorIs(t, tx.Commit(), port.ErrBlockTxDone)
}

func TestBlockTxRollbackLeavesNoTrace(t *testing.T) {
	s := newTestPebble(t)

	ctx, tx, err := s.BeginBlock(context.Background())
	require.NoError(t, err)
	writeBlock(t, s, ctx, 9)
	tx.Rollback()

	require.Zero(t, addrSeqOf(s, txAddrA))
	require.Zero(t, s.txCount.Load())
	require.Zero(t, committedKeys(t, s, "/index/addr/"))
	_, err = s.GetLatestHeight(context.Background())
	require.Error(t, err)

	// Replaying the same block after a rollback yields the same keys as a
	// first attempt would (sequence numbers were not consumed).
	ctx, tx, err = s.BeginBlock(context.Background())
	require.NoError(t, err)
	writeBlock(t, s, ctx, 9)
	require.NoError(t, tx.Commit())

	for i, h := range []common.Hash{txHash1, txHash2} {
		v, closer, err := s.db.Get(AddressTransactionKey(txAddrA, uint64(i)))
		require.NoError(t, err)
		require.Equal(t, h.Bytes(), v)
		closer.Close()
	}
}

func TestBlockTxSingleWriter(t *testing.T) {
	s := newTestPebble(t)
	_, first, err := s.BeginBlock(context.Background())
	require.NoError(t, err)

	acquired := make(chan port.BlockTx)
	go func() {
		_, second, err := s.BeginBlock(context.Background())
		if err != nil {
			close(acquired)
			return
		}
		acquired <- second
	}()

	select {
	case <-acquired:
		t.Fatal("second BeginBlock must wait while a block transaction is open")
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, first.Commit())
	select {
	case second := <-acquired:
		require.NotNil(t, second)
		second.Rollback()
	case <-time.After(5 * time.Second):
		t.Fatal("second BeginBlock did not proceed after Commit")
	}
}

func TestBlockTxRejectsNesting(t *testing.T) {
	s := newTestPebble(t)
	ctx, tx, err := s.BeginBlock(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()

	_, nested, err := s.BeginBlock(ctx)
	require.Error(t, err)
	require.Nil(t, nested)
}

func TestWritesOutsideBlockTxStillPublishImmediately(t *testing.T) {
	s := newTestPebble(t)
	require.NoError(t, s.AddTransactionToAddressIndex(context.Background(), txAddrA, txHash1))
	require.Equal(t, uint64(1), addrSeqOf(s, txAddrA))
	require.Equal(t, int64(1), committedKeys(t, s, "/index/addr/"))
}

func reopen(t *testing.T, s *PebbleStorage, dir string) *PebbleStorage {
	t.Helper()
	require.NoError(t, s.Close())
	r, err := NewPebbleStorage(DefaultConfig(dir))
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// TestAddrSeqRestoredAfterReopen covers D1: after a restart the per-address
// counter continues after the highest stored sequence instead of restarting
// at 0 and overwriting entries.
func TestAddrSeqRestoredAfterReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := NewPebbleStorage(DefaultConfig(dir))
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, s.AddTransactionToAddressIndex(ctx, txAddrA, txHash1)) // seq 0
	require.NoError(t, s.AddTransactionToAddressIndex(ctx, txAddrA, txHash2)) // seq 1

	s = reopen(t, s, dir)
	require.NoError(t, s.AddTransactionToAddressIndex(ctx, txAddrA, common.HexToHash("0x03")))

	for i, want := range []common.Hash{txHash1, txHash2, common.HexToHash("0x03")} {
		v, closer, err := s.db.Get(AddressTransactionKey(txAddrA, uint64(i)))
		require.NoError(t, err, "seq %d", i)
		require.Equal(t, want.Bytes(), v, "seq %d was overwritten", i)
		closer.Close()
	}
}

// TestAddrSeqFamiliesAreIndependent checks that the address transaction
// index and the balance history number their keys separately (D20), and
// that each counter resumes after its own highest stored sequence.
func TestAddrSeqFamiliesAreIndependent(t *testing.T) {
	s := newTestPebble(t)
	require.NoError(t, s.db.Set(AddressTransactionKey(txAddrA, 3), txHash1.Bytes(), nil))
	require.NoError(t, s.db.Set(AddressBalanceKey(txAddrA, 7), []byte("x"), nil))

	seq, err := s.nextAddrSeq(context.Background(), seqAddrTx, txAddrA)
	require.NoError(t, err)
	require.Equal(t, uint64(4), seq, "the balance history does not advance the transaction index")
	seq, err = s.nextAddrSeq(context.Background(), seqBalance, txAddrA)
	require.NoError(t, err)
	require.Equal(t, uint64(8), seq)
	seq, err = s.nextAddrSeq(context.Background(), seqAddrTx, txAddrA)
	require.NoError(t, err)
	require.Equal(t, uint64(5), seq)

	ctx, tx, err := s.BeginBlock(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()
	other := common.HexToAddress("0x00000000000000000000000000000000000000B2")
	require.NoError(t, s.db.Set(AddressBalanceKey(other, 4), []byte("x"), nil))
	seq, err = s.nextAddrSeq(ctx, seqBalance, other)
	require.NoError(t, err)
	require.Equal(t, uint64(5), seq, "restore also works inside a block transaction")
	seq, err = s.nextAddrSeq(ctx, seqAddrTx, other)
	require.NoError(t, err)
	require.Equal(t, uint64(0), seq)
}
