package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectQueryType(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{"block number", "12345", "blockNumber"},
		{"block number zero", "0", "blockNumber"},
		{"block hash with 0x", "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", "hash"},
		{"block hash without 0x", "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", "hash"},
		{"address with 0x", "0x1234567890123456789012345678901234567890", "address"},
		{"address without 0x", "1234567890123456789012345678901234567890", "address"},
		{"short query", "abc", "address"},
		{"empty after trim", "", "address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectQueryType(tt.query)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPebbleStorage_Search(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	t.Run("EmptyQuery", func(t *testing.T) {
		results, err := storage.Search(ctx, "", nil, 10)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("SearchByBlockNumber", func(t *testing.T) {
		// Store a block
		block := createTestBlock(42)
		require.NoError(t, storage.SetBlock(ctx, block))

		results, err := storage.Search(ctx, "42", nil, 10)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "block", results[0].Type)
		assert.Equal(t, "42", results[0].Value)
		assert.Contains(t, results[0].Label, "42")
	})

	t.Run("SearchByBlockNumber_NotFound", func(t *testing.T) {
		results, err := storage.Search(ctx, "99999", nil, 10)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("SearchByBlockNumber_TypeFilter", func(t *testing.T) {
		// Block 42 exists from previous subtest
		results, err := storage.Search(ctx, "42", []string{"transaction"}, 10)
		require.NoError(t, err)
		assert.Empty(t, results) // block type not in filter
	})

	t.Run("SearchByAddress", func(t *testing.T) {
		addr := "0x1234567890123456789012345678901234567890"
		results, err := storage.Search(ctx, addr, nil, 10)
		require.NoError(t, err)
		// Should return address type result
		found := false
		for _, r := range results {
			if r.Type == "address" {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("SearchByAddress_TypeFilter", func(t *testing.T) {
		addr := "0x1234567890123456789012345678901234567890"
		results, err := storage.Search(ctx, addr, []string{"address"}, 10)
		require.NoError(t, err)
		for _, r := range results {
			assert.Equal(t, "address", r.Type)
		}
	})

	t.Run("DefaultLimit", func(t *testing.T) {
		// Negative limit should use default
		results, err := storage.Search(ctx, "42", nil, -1)
		require.NoError(t, err)
		assert.NotNil(t, results)
	})

	t.Run("SearchByHash_BlockHash", func(t *testing.T) {
		// Store a block and search by its hash
		block := createTestBlock(50)
		require.NoError(t, storage.SetBlock(ctx, block))

		hash := block.Hash().Hex()
		results, err := storage.Search(ctx, hash, []string{"block"}, 10)
		require.NoError(t, err)
		if len(results) > 0 {
			assert.Equal(t, "block", results[0].Type)
		}
	})
}
