package feedelegation

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

func newMetaStore(t *testing.T) (*MetaStore, *storage.PebbleStorage) {
	t.Helper()
	db, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	m, err := OpenMetaStore(db)
	require.NoError(t, err)
	return m, db
}

func TestMetaStore(t *testing.T) {
	m, _ := newMetaStore(t)
	ctx := context.Background()

	feePayer := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	txHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	meta := &TxMeta{
		TxHash: txHash, BlockNumber: 100, OriginalType: 0x16, FeePayer: feePayer,
		FeePayerV: big.NewInt(28), FeePayerR: big.NewInt(12345), FeePayerS: big.NewInt(67890),
	}

	t.Run("SetAndGet", func(t *testing.T) {
		require.NoError(t, m.SetTxMeta(ctx, meta))
		got, err := m.TxMeta(ctx, txHash)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, txHash, got.TxHash)
		assert.Equal(t, feePayer, got.FeePayer)
		assert.Equal(t, uint64(100), got.BlockNumber)
		assert.Equal(t, uint8(0x16), got.OriginalType)
		assert.Zero(t, got.FeePayerV.Cmp(big.NewInt(28)))
		assert.Zero(t, got.FeePayerR.Cmp(big.NewInt(12345)))
		assert.Zero(t, got.FeePayerS.Cmp(big.NewInt(67890)))
	})

	t.Run("NotFeeDelegated", func(t *testing.T) {
		got, err := m.TxMeta(ctx, common.HexToHash("0x01"))
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("NilMeta", func(t *testing.T) {
		require.Error(t, m.SetTxMeta(ctx, nil))
	})

	t.Run("ByFeePayer", func(t *testing.T) {
		second := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
		require.NoError(t, m.SetTxMeta(ctx, &TxMeta{
			TxHash: second, BlockNumber: 200, OriginalType: 0x16, FeePayer: feePayer,
			FeePayerV: big.NewInt(27), FeePayerR: big.NewInt(11111), FeePayerS: big.NewInt(22222),
		}))
		third := common.HexToHash("0x03")
		require.NoError(t, m.SetTxMeta(ctx, &TxMeta{TxHash: third, BlockNumber: 0x1000, OriginalType: 0x16, FeePayer: feePayer}))
		all := []PayerTx{{txHash, 100}, {second, 200}, {third, 0x1000}}

		got, next, err := m.FeePayerTxs(ctx, feePayer, port.Page{Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, all, got, "oldest first, block numbers in numeric order")
		assert.Empty(t, next, "no cursor after the last page")

		var paged []PayerTx
		page := port.Page{Limit: 1}
		for {
			got, next, err := m.FeePayerTxs(ctx, feePayer, page)
			require.NoError(t, err)
			paged = append(paged, got...)
			if next == "" {
				break
			}
			page.After = next
		}
		assert.Equal(t, all, paged, "cursor pages cover every transaction once")

		got, _, err = m.FeePayerTxs(ctx, feePayer, port.Page{Limit: 1, Offset: 1})
		require.NoError(t, err)
		assert.Equal(t, all[1:2], got, "limit and offset")

		got, _, err = m.FeePayerTxs(ctx, common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"), port.Page{})
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

// TestMetaStoreFollowsBlockTransaction: metadata written inside a block
// transaction is discarded with it.
func TestMetaStoreFollowsBlockTransaction(t *testing.T) {
	m, db := newMetaStore(t)
	txCtx, tx, err := db.BeginBlock(context.Background())
	require.NoError(t, err)
	h := common.HexToHash("0x02")
	require.NoError(t, m.SetTxMeta(txCtx, &TxMeta{TxHash: h, FeePayer: common.HexToAddress("0x03")}))
	got, err := m.TxMeta(txCtx, h)
	require.NoError(t, err)
	require.NotNil(t, got, "visible inside the transaction")
	tx.Rollback()

	got, err = m.TxMeta(context.Background(), h)
	require.NoError(t, err)
	assert.Nil(t, got, "gone after rollback")
}
