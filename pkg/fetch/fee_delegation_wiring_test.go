package fetch

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

type stubFeeDelegationClient struct {
	metas map[uint64][]*FeeDelegationMeta
	calls int
}

func (c *stubFeeDelegationClient) GetBlockWithFeeDelegationMeta(_ context.Context, number uint64) (*types.Block, []*FeeDelegationMeta, error) {
	c.calls++
	return nil, c.metas[number], nil
}

// TestFeeDelegationClientIsUsed checks the injected client is preferred over
// the main RPC client (which cannot extract fee delegation metadata) and that
// the metadata reaches storage.
func TestFeeDelegationClientIsUsed(t *testing.T) {
	store, err := storagepkg.NewPebbleStorage(storagepkg.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer store.Close()

	txHash := common.HexToHash("0xfd")
	payer := common.HexToAddress("0x00000000000000000000000000000000000000FD")
	stub := &stubFeeDelegationClient{metas: map[uint64][]*FeeDelegationMeta{
		5: {{TxHash: txHash, BlockNumber: 5, OriginalType: 0x16, FeePayer: payer,
			FeePayerV: big.NewInt(1), FeePayerR: big.NewInt(2), FeePayerS: big.NewInt(3)}},
	}}

	f := NewFetcher(newMockClient(), store, &Config{BatchSize: 1, MaxRetries: 1, NumWorkers: 1}, zap.NewNop(), nil)
	ctx := context.Background()
	fb := &fetchedBlock{block: &model.Block{Number: 5}}

	// Without an injected client nothing is extracted: mockClient lacks it.
	require.NoError(t, f.processFeeDelegationMetadata(ctx, fb))
	none, err := store.GetFeeDelegationTxMeta(ctx, txHash)
	require.NoError(t, err)
	require.Nil(t, none, "nil, nil means not a fee delegation tx")

	f.SetFeeDelegationClient(stub)
	require.NoError(t, f.processFeeDelegationMetadata(ctx, fb))
	require.Equal(t, 1, stub.calls)

	got, err := store.GetFeeDelegationTxMeta(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, payer, got.FeePayer)
	require.Equal(t, uint8(0x16), got.OriginalType)
}
