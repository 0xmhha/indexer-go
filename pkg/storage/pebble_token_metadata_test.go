package storage

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestTokenMetadata(addr common.Address, name, symbol string, standard TokenStandard) *TokenMetadata {
	return &TokenMetadata{
		Address:          addr,
		Standard:         standard,
		Name:             name,
		Symbol:           symbol,
		Decimals:         18,
		TotalSupply:      big.NewInt(1000000),
		DetectedAt:       100,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		SupportsERC165:   true,
		SupportsMetadata: true,
	}
}

func TestPebbleStorage_SaveAndGetTokenMetadata(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	addr := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	meta := createTestTokenMetadata(addr, "TestToken", "TT", TokenStandardERC20)

	t.Run("SaveAndGet", func(t *testing.T) {
		err := storage.SaveTokenMetadata(ctx, meta)
		require.NoError(t, err)

		got, err := storage.GetTokenMetadata(ctx, addr)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, addr, got.Address)
		assert.Equal(t, "TestToken", got.Name)
		assert.Equal(t, "TT", got.Symbol)
		assert.Equal(t, uint8(18), got.Decimals)
		assert.Equal(t, TokenStandardERC20, got.Standard)
		assert.Equal(t, 0, got.TotalSupply.Cmp(big.NewInt(1000000)))
	})

	t.Run("GetNonExistent", func(t *testing.T) {
		unknown := common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
		_, err := storage.GetTokenMetadata(ctx, unknown)
		assert.Equal(t, ErrNotFound, err)
	})

	t.Run("Update", func(t *testing.T) {
		updated := createTestTokenMetadata(addr, "UpdatedToken", "UT", TokenStandardERC20)
		err := storage.SaveTokenMetadata(ctx, updated)
		require.NoError(t, err)

		got, err := storage.GetTokenMetadata(ctx, addr)
		require.NoError(t, err)
		assert.Equal(t, "UpdatedToken", got.Name)
		assert.Equal(t, "UT", got.Symbol)
	})
}

func TestPebbleStorage_DeleteTokenMetadata(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	addr := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	meta := createTestTokenMetadata(addr, "TestToken", "TT", TokenStandardERC20)

	require.NoError(t, storage.SaveTokenMetadata(ctx, meta))

	err := storage.DeleteTokenMetadata(ctx, addr)
	require.NoError(t, err)

	_, err = storage.GetTokenMetadata(ctx, addr)
	assert.Equal(t, ErrNotFound, err)

	// Delete non-existent should not error
	err = storage.DeleteTokenMetadata(ctx, addr)
	require.NoError(t, err)
}

func TestPebbleStorage_ListTokensByStandard(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Create tokens of different standards
	erc20_1 := createTestTokenMetadata(
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		"Token1", "T1", TokenStandardERC20,
	)
	erc20_2 := createTestTokenMetadata(
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		"Token2", "T2", TokenStandardERC20,
	)
	erc721 := createTestTokenMetadata(
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		"NFT1", "N1", TokenStandardERC721,
	)

	require.NoError(t, storage.SaveTokenMetadata(ctx, erc20_1))
	require.NoError(t, storage.SaveTokenMetadata(ctx, erc20_2))
	require.NoError(t, storage.SaveTokenMetadata(ctx, erc721))

	t.Run("FilterByERC20", func(t *testing.T) {
		tokens, err := storage.ListTokensByStandard(ctx, TokenStandardERC20, 10, 0)
		require.NoError(t, err)
		assert.Len(t, tokens, 2)
	})

	t.Run("FilterByERC721", func(t *testing.T) {
		tokens, err := storage.ListTokensByStandard(ctx, TokenStandardERC721, 10, 0)
		require.NoError(t, err)
		assert.Len(t, tokens, 1)
		assert.Equal(t, "NFT1", tokens[0].Name)
	})

	t.Run("AllTokens", func(t *testing.T) {
		tokens, err := storage.ListTokensByStandard(ctx, "", 10, 0)
		require.NoError(t, err)
		assert.Len(t, tokens, 3)
	})

	t.Run("WithPagination", func(t *testing.T) {
		tokens, err := storage.ListTokensByStandard(ctx, TokenStandardERC20, 1, 0)
		require.NoError(t, err)
		assert.Len(t, tokens, 1)
	})
}

func TestPebbleStorage_GetTokensCount(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Empty
	count, err := storage.GetTokensCount(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Add tokens
	erc20 := createTestTokenMetadata(
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		"Token1", "T1", TokenStandardERC20,
	)
	erc721 := createTestTokenMetadata(
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		"NFT1", "N1", TokenStandardERC721,
	)
	require.NoError(t, storage.SaveTokenMetadata(ctx, erc20))
	require.NoError(t, storage.SaveTokenMetadata(ctx, erc721))

	count, err = storage.GetTokensCount(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = storage.GetTokensCount(ctx, TokenStandardERC20)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestPebbleStorage_SearchTokens(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	token1 := createTestTokenMetadata(
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		"stablecoin", "USDT", TokenStandardERC20,
	)
	token2 := createTestTokenMetadata(
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		"wrapped-eth", "WETH", TokenStandardERC20,
	)
	require.NoError(t, storage.SaveTokenMetadata(ctx, token1))
	require.NoError(t, storage.SaveTokenMetadata(ctx, token2))

	t.Run("SearchByName", func(t *testing.T) {
		results, err := storage.SearchTokens(ctx, "stablecoin", 10)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "stablecoin", results[0].Name)
	})

	t.Run("SearchBySymbol", func(t *testing.T) {
		results, err := storage.SearchTokens(ctx, "weth", 10)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "WETH", results[0].Symbol)
	})

	t.Run("EmptyQuery", func(t *testing.T) {
		results, err := storage.SearchTokens(ctx, "", 10)
		require.NoError(t, err)
		assert.Nil(t, results)
	})

	t.Run("NoMatch", func(t *testing.T) {
		results, err := storage.SearchTokens(ctx, "zzzzz", 10)
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}
