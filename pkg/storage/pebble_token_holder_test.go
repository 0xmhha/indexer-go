package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPebbleStorage_UpdateAndGetTokenHolder(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	token := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	holder1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	holder2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	t.Run("AddHolders", func(t *testing.T) {
		err := storage.UpdateTokenHolder(ctx, &TokenHolder{
			TokenAddress:  token,
			HolderAddress: holder1,
			Balance:       big.NewInt(1000),
			LastUpdatedAt: 100,
		})
		require.NoError(t, err)

		err = storage.UpdateTokenHolder(ctx, &TokenHolder{
			TokenAddress:  token,
			HolderAddress: holder2,
			Balance:       big.NewInt(500),
			LastUpdatedAt: 100,
		})
		require.NoError(t, err)
	})

	t.Run("GetTokenBalance", func(t *testing.T) {
		balance, err := storage.GetTokenBalance(ctx, token, holder1)
		require.NoError(t, err)
		assert.Equal(t, 0, balance.Cmp(big.NewInt(1000)))
	})

	t.Run("GetTokenHolders", func(t *testing.T) {
		holders, err := storage.GetTokenHolders(ctx, token, 10, 0)
		require.NoError(t, err)
		assert.Len(t, holders, 2)
	})

	t.Run("GetTokenHolderCount", func(t *testing.T) {
		count, err := storage.GetTokenHolderCount(ctx, token)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("GetHolderTokens", func(t *testing.T) {
		tokens, err := storage.GetHolderTokens(ctx, holder1, 10, 0)
		require.NoError(t, err)
		assert.Len(t, tokens, 1)
		assert.Equal(t, token, tokens[0].TokenAddress)
	})

	t.Run("UpdateBalance", func(t *testing.T) {
		err := storage.UpdateTokenHolder(ctx, &TokenHolder{
			TokenAddress:  token,
			HolderAddress: holder1,
			Balance:       big.NewInt(2000),
			LastUpdatedAt: 200,
		})
		require.NoError(t, err)

		balance, err := storage.GetTokenBalance(ctx, token, holder1)
		require.NoError(t, err)
		assert.Equal(t, 0, balance.Cmp(big.NewInt(2000)))
	})

	t.Run("RemoveHolderWithZeroBalance", func(t *testing.T) {
		err := storage.UpdateTokenHolder(ctx, &TokenHolder{
			TokenAddress:  token,
			HolderAddress: holder2,
			Balance:       big.NewInt(0),
			LastUpdatedAt: 200,
		})
		require.NoError(t, err)

		// Holder should be removed
		_, err = storage.GetTokenBalance(ctx, token, holder2)
		assert.Equal(t, ErrNotFound, err)

		count, err := storage.GetTokenHolderCount(ctx, token)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})
}

func TestPebbleStorage_UpdateTokenHolderStats(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	token := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")

	stats := &TokenHolderStats{
		TokenAddress:   token,
		HolderCount:    10,
		TransferCount:  50,
		LastActivityAt: 200,
	}

	err := storage.UpdateTokenHolderStats(ctx, stats)
	require.NoError(t, err)

	got, err := storage.GetTokenHolderStats(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, token, got.TokenAddress)
	assert.Equal(t, 10, got.HolderCount)
	assert.Equal(t, 50, got.TransferCount)
	assert.Equal(t, uint64(200), got.LastActivityAt)
}

func TestPebbleStorage_ProcessERC20TransferForHolders(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	token := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// First give 'from' some tokens (mint)
	err := storage.ProcessERC20TransferForHolders(ctx, &ERC20Transfer{
		ContractAddress: token,
		From:            common.Address{}, // mint
		To:              from,
		Value:           big.NewInt(1000),
		BlockNumber:     100,
	})
	require.NoError(t, err)

	// Verify from has 1000
	balance, err := storage.GetTokenBalance(ctx, token, from)
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(1000)))

	// Transfer 300 from -> to
	err = storage.ProcessERC20TransferForHolders(ctx, &ERC20Transfer{
		ContractAddress: token,
		From:            from,
		To:              to,
		Value:           big.NewInt(300),
		BlockNumber:     200,
	})
	require.NoError(t, err)

	// Verify balances
	fromBalance, err := storage.GetTokenBalance(ctx, token, from)
	require.NoError(t, err)
	assert.Equal(t, 0, fromBalance.Cmp(big.NewInt(700)))

	toBalance, err := storage.GetTokenBalance(ctx, token, to)
	require.NoError(t, err)
	assert.Equal(t, 0, toBalance.Cmp(big.NewInt(300)))

	// Check stats
	stats, err := storage.GetTokenHolderStats(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.TransferCount) // mint + transfer
}
