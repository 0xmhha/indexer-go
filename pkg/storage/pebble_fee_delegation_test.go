package storage

import (
	"context"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPebbleStorage_FeeDelegationTxMeta(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_feedelegation_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()

	feePayer := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	txHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	meta := &FeeDelegationTxMeta{
		TxHash:       txHash,
		BlockNumber:  100,
		OriginalType: 22,
		FeePayer:     feePayer,
		FeePayerV:    big.NewInt(28),
		FeePayerR:    big.NewInt(12345),
		FeePayerS:    big.NewInt(67890),
	}

	t.Run("SetAndGet", func(t *testing.T) {
		err := storage.SetFeeDelegationTxMeta(ctx, meta)
		require.NoError(t, err)

		got, err := storage.GetFeeDelegationTxMeta(ctx, txHash)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, txHash, got.TxHash)
		assert.Equal(t, feePayer, got.FeePayer)
		assert.Equal(t, uint64(100), got.BlockNumber)
		assert.Equal(t, uint8(22), got.OriginalType)
		assert.Equal(t, 0, got.FeePayerV.Cmp(big.NewInt(28)))
		assert.Equal(t, 0, got.FeePayerR.Cmp(big.NewInt(12345)))
		assert.Equal(t, 0, got.FeePayerS.Cmp(big.NewInt(67890)))
	})

	t.Run("GetNonExistent", func(t *testing.T) {
		nonExistent := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
		got, err := storage.GetFeeDelegationTxMeta(ctx, nonExistent)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("SetNilMeta", func(t *testing.T) {
		err := storage.SetFeeDelegationTxMeta(ctx, nil)
		require.Error(t, err)
	})

	t.Run("GetByFeePayer", func(t *testing.T) {
		// Store another tx from the same fee payer
		meta2 := &FeeDelegationTxMeta{
			TxHash:       common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
			BlockNumber:  200,
			OriginalType: 22,
			FeePayer:     feePayer,
			FeePayerV:    big.NewInt(27),
			FeePayerR:    big.NewInt(11111),
			FeePayerS:    big.NewInt(22222),
		}
		require.NoError(t, storage.SetFeeDelegationTxMeta(ctx, meta2))

		txHashes, err := storage.GetFeeDelegationTxsByFeePayer(ctx, feePayer, 10, 0)
		require.NoError(t, err)
		assert.Len(t, txHashes, 2)
	})

	t.Run("GetByFeePayerEmpty", func(t *testing.T) {
		unknown := common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
		txHashes, err := storage.GetFeeDelegationTxsByFeePayer(ctx, unknown, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, txHashes)
	})
}
