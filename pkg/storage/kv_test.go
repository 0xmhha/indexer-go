package storage

import (
	"context"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testutil"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// bindTestBatch binds an indexed batch to ctx the way a block transaction will.
func bindTestBatch(s *PebbleStorage) (context.Context, *pebble.Batch) {
	b := s.db.NewIndexedBatch()
	return context.WithValue(context.Background(), blockTxKey{}, &blockTxBinding{owner: s, batch: b}), b
}

func newTestPebble(t *testing.T) *PebbleStorage {
	t.Helper()
	s, err := NewPebbleStorage(DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func committedKeys(t *testing.T, s *PebbleStorage, prefix string) int64 {
	t.Helper()
	n, err := s.CountByPrefix([]byte(prefix))
	require.NoError(t, err)
	return n
}

// TestBoundBatchCapturesWrites checks the three write styles used by
// PebbleStorage: direct writes (SetBlock), the Batch wrapper (IndexLogs) and a
// method-local batch (SaveTokenMetadata). With a batch bound to ctx none of
// them may reach the DB before the batch commits, and reads with the same ctx
// must see them.
func TestBoundBatchCapturesWrites(t *testing.T) {
	s := newTestPebble(t)
	ctx, batch := bindTestBatch(s)
	defer batch.Close()

	block := testutil.NewTestBlockWithTransactions(7, 2)
	require.NoError(t, s.SetBlock(ctx, block))

	logAddr := common.HexToAddress("0x00000000000000000000000000000000000000AA")
	require.NoError(t, s.IndexLogs(ctx, []*types.Log{{
		Address:     logAddr,
		Topics:      []common.Hash{common.HexToHash("0x01")},
		BlockNumber: 7,
		TxHash:      common.HexToHash("0xbeef"), // test blocks carry no transactions
	}}))

	tokenAddr := common.HexToAddress("0x00000000000000000000000000000000000000BB")
	require.NoError(t, s.SaveTokenMetadata(ctx, &port.TokenMetadata{Address: tokenAddr, Standard: port.TokenStandardERC20, Name: "T"}))

	// Nothing is committed yet.
	for _, prefix := range []string{"/data/blocks/", "/data/logs/", "/data/token/metadata/"} {
		require.Zero(t, committedKeys(t, s, prefix), "%s leaked before commit", prefix)
	}
	_, err := s.GetBlock(context.Background(), 7)
	require.Error(t, err, "unbound reads must not see uncommitted writes")

	// Reads through the bound ctx see the pending writes.
	got, err := s.GetBlock(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, block.Hash(), got.Hash())
	meta, err := s.GetTokenMetadata(ctx, tokenAddr)
	require.NoError(t, err)
	require.Equal(t, "T", meta.Name)

	require.NoError(t, batch.Commit(pebble.Sync))

	for _, prefix := range []string{"/data/blocks/", "/data/logs/", "/data/token/metadata/"} {
		require.NotZero(t, committedKeys(t, s, prefix), "%s missing after commit", prefix)
	}
	got, err = s.GetBlock(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, block.Hash(), got.Hash())
}

// TestUnboundContextWritesDirectly checks the default path is unchanged.
func TestUnboundContextWritesDirectly(t *testing.T) {
	s := newTestPebble(t)
	block := testutil.NewTestBlockWithTransactions(3, 1)
	require.NoError(t, s.SetBlock(context.Background(), block))
	require.NotZero(t, committedKeys(t, s, "/data/blocks/"))
}

// TestBatchBoundToOtherStorageIsIgnored ensures a ctx carrying another
// instance's batch does not redirect writes.
func TestBatchBoundToOtherStorageIsIgnored(t *testing.T) {
	a := newTestPebble(t)
	b := newTestPebble(t)
	ctx, batch := bindTestBatch(a)
	defer batch.Close()

	require.NoError(t, b.SetBlock(ctx, testutil.NewTestBlockWithTransactions(5, 1)))
	require.NotZero(t, committedKeys(t, b, "/data/blocks/"))
	require.Zero(t, batch.Count())
}
