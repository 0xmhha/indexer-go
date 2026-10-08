package postgres

import (
	"context"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// fakeNode answers BalanceAt with a fixed genesis allocation and counts the
// calls.
type fakeNode struct {
	balance *big.Int
	calls   atomic.Int32
}

func (n *fakeNode) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	n.calls.Add(1)
	return new(big.Int).Set(n.balance), nil
}

// TestGenesisLookup: with a resolver, an early zero balance of an account
// without a recorded balance is its genesis allocation, recorded once; a
// lookup inside a rolled back block transaction happens again.
func TestGenesisLookup(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	addr, other, spent := common.HexToAddress("0x0a"), common.HexToAddress("0x0b"), common.HexToAddress("0x0c")

	got, err := s.GetAddressBalance(ctx, addr, 5)
	require.NoError(t, err)
	assert.Zero(t, got.Sign(), "no resolver, no lookup")

	node := &fakeNode{balance: big.NewInt(1000)}
	s.SetGenesisBalanceResolver(node)

	txCtx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	got, err = s.GetAddressBalance(txCtx, addr, 5)
	require.NoError(t, err)
	assert.Equal(t, "1000", got.String())
	tx.Rollback()
	ok, err := s.HasBalanceRecord(ctx, addr)
	require.NoError(t, err)
	assert.False(t, ok, "the rolled back block's record is gone")

	got, err = s.GetAddressBalance(ctx, addr, 5)
	require.NoError(t, err)
	assert.Equal(t, "1000", got.String(), "looked up again after the rollback")
	got, err = s.GetAddressBalance(ctx, addr, 0)
	require.NoError(t, err)
	assert.Equal(t, "1000", got.String(), "recorded")
	assert.Equal(t, int32(2), node.calls.Load())

	got, err = s.GetAddressBalance(ctx, other, port.GenesisLookupMaxBlock)
	require.NoError(t, err)
	assert.Zero(t, got.Sign(), "no lookup for a later block")
	require.NoError(t, s.UpdateBalance(ctx, spent, 1, big.NewInt(5), common.Hash{}))
	require.NoError(t, s.UpdateBalance(ctx, spent, 2, big.NewInt(-5), common.Hash{}))
	got, err = s.GetAddressBalance(ctx, spent, 3)
	require.NoError(t, err)
	assert.Zero(t, got.Sign(), "an account with a recorded balance is not looked up")
	assert.Equal(t, int32(2), node.calls.Load())
}

// TestTransactionCount: the count SetBlock keeps counts each transaction
// once and follows a rollback.
func TestTransactionCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	index := func(height uint64, n int) {
		t.Helper()
		b := &model.Block{Number: height, Hash: common.Hash{byte(height)}}
		for i := 0; i < n; i++ {
			b.Transactions = append(b.Transactions, &model.Transaction{Hash: common.Hash{0xee, byte(height), byte(i)}})
		}
		txCtx, tx, err := s.BeginBlock(ctx)
		require.NoError(t, err)
		tx.SetHeight(height)
		require.NoError(t, s.SetBlock(txCtx, b))
		require.NoError(t, s.SetBlock(txCtx, b), "saving a block again adds nothing")
		for _, t2 := range b.Transactions {
			require.NoError(t, s.SetReceipt(txCtx, &model.Receipt{TxHash: t2.Hash, BlockNumber: height, BlockHash: b.Hash, Logs: []*model.Log{}}))
		}
		require.NoError(t, s.SetLatestHeight(txCtx, height))
		require.NoError(t, tx.Commit())
	}
	count := func() uint64 {
		t.Helper()
		n, err := s.GetTransactionCount(ctx)
		require.NoError(t, err)
		return n
	}
	assert.Zero(t, count())
	index(1, 3)
	index(2, 2)
	assert.Equal(t, uint64(5), count())
	_, err := s.RollbackTo(ctx, 1, nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), count(), "the rolled back block's transactions are not counted")
}

// TestClearChainData: a reindex clears the chain data and keeps the user
// data and the outbox numbering; clearing all removes the user data too.
func TestClearChainData(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	contract := common.HexToAddress("0x0c")
	require.NoError(t, s.SetBlock(ctx, &model.Block{Number: 1, Hash: common.Hash{1}}))
	require.NoError(t, s.SetLatestHeight(ctx, 1))
	require.NoError(t, s.SetABI(ctx, contract, []byte("[]")))
	require.NoError(t, s.Put(ctx, []byte("/data/chain/x"), []byte("1")))
	require.NoError(t, s.Put(ctx, []byte("/user/x"), []byte("1")))
	require.NoError(t, s.SetOutboxCursor(ctx, "relay", 4))

	_, err := s.ClearChainData(ctx, []string{"/data/chain/"}, false)
	require.NoError(t, err)
	_, err = s.GetBlock(ctx, 1)
	assert.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.GetLatestHeight(ctx)
	assert.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.Get(ctx, []byte("/data/chain/x"))
	assert.ErrorIs(t, err, port.ErrNotFound, "kv chain data cleared")
	_, err = s.Get(ctx, []byte("/user/x"))
	assert.NoError(t, err, "kv rows under other prefixes kept")
	has, err := s.HasABI(ctx, contract)
	require.NoError(t, err)
	assert.True(t, has, "verification data kept")
	seq, ok, err := s.OutboxCursor(ctx, "relay")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, uint64(4), seq, "outbox cursors kept")

	_, err = s.ClearChainData(ctx, nil, true)
	require.NoError(t, err)
	has, err = s.HasABI(ctx, contract)
	require.NoError(t, err)
	assert.False(t, has)
	_, err = s.Get(ctx, []byte("/user/x"))
	assert.ErrorIs(t, err, port.ErrNotFound)
	require.NoError(t, s.checkVersion(ctx), "the schema version is kept")
}

// fakeTokenFetcher answers with fixed metadata and counts the calls.
type fakeTokenFetcher struct{ calls atomic.Int32 }

func (f *fakeTokenFetcher) FetchTokenMetadata(_ context.Context, addr common.Address) (*port.TokenMetadata, error) {
	f.calls.Add(1)
	return &port.TokenMetadata{Address: addr, Standard: port.TokenStandardERC20, Name: "Fetched", Symbol: "FT", Decimals: 6}, nil
}

// TestTokenMetadataFetched: a token without stored metadata is described by
// the node, and what the node said is stored.
func TestTokenMetadataFetched(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	f := &fakeTokenFetcher{}
	s.SetTokenMetadataFetcher(f)
	token := common.HexToAddress("0x70")
	for i := 0; i < 2; i++ {
		tb := &port.TokenBalance{ContractAddress: token}
		s.describeToken(ctx, tb)
		assert.Equal(t, "Fetched", tb.Name)
		require.NotNil(t, tb.Decimals)
		assert.Equal(t, 6, *tb.Decimals)
	}
	assert.Equal(t, int32(1), f.calls.Load(), "the second description reads the stored metadata")
}
