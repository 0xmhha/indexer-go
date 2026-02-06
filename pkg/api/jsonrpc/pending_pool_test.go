package jsonrpc

import (
	"math/big"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPendingPool(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		pool := NewPendingPool(0, 0)
		defer pool.Close()

		assert.Equal(t, DefaultPendingPoolSize, pool.maxSize)
		assert.Equal(t, DefaultPendingTxTTL, pool.ttl)
		assert.Equal(t, uint64(1), pool.nextIndex)
	})

	t.Run("custom values", func(t *testing.T) {
		pool := NewPendingPool(100, 10*time.Second)
		defer pool.Close()

		assert.Equal(t, 100, pool.maxSize)
		assert.Equal(t, 10*time.Second, pool.ttl)
	})
}

func TestPendingPoolAddTransaction(t *testing.T) {
	pool := NewPendingPool(10, 5*time.Minute)
	defer pool.Close()

	t.Run("add single transaction", func(t *testing.T) {
		tx := createTestTx(1)
		txEvent := createTestTxEvent(tx, common.HexToAddress("0x1111"))

		pool.AddTransaction(txEvent)

		assert.Equal(t, 1, pool.Size())
		assert.Equal(t, uint64(1), pool.CurrentIndex())

		pendingTx, exists := pool.GetTransaction(tx.Hash())
		require.True(t, exists)
		assert.Equal(t, tx.Hash(), pendingTx.Hash)
		assert.Equal(t, common.HexToAddress("0x1111"), pendingTx.From)
	})

	t.Run("add duplicate transaction", func(t *testing.T) {
		// Create a new pool for this test
		dupPool := NewPendingPool(10, 5*time.Minute)
		defer dupPool.Close()

		tx := createTestTx(100)
		txEvent := createTestTxEvent(tx, common.HexToAddress("0x1111"))

		dupPool.AddTransaction(txEvent)
		assert.Equal(t, 1, dupPool.Size())

		dupPool.AddTransaction(txEvent) // Duplicate - same hash
		assert.Equal(t, 1, dupPool.Size()) // Should still be 1
	})

	t.Run("pool overflow evicts oldest", func(t *testing.T) {
		smallPool := NewPendingPool(3, 5*time.Minute)
		defer smallPool.Close()

		// Add 4 transactions to a pool of size 3
		var hashes []common.Hash
		for i := 0; i < 4; i++ {
			tx := createTestTx(uint64(i + 100))
			hashes = append(hashes, tx.Hash())
			txEvent := createTestTxEvent(tx, common.HexToAddress("0x2222"))
			smallPool.AddTransaction(txEvent)
		}

		assert.Equal(t, 3, smallPool.Size())

		// First transaction should be evicted
		_, exists := smallPool.GetTransaction(hashes[0])
		assert.False(t, exists)

		// Last three should exist
		for i := 1; i < 4; i++ {
			_, exists := smallPool.GetTransaction(hashes[i])
			assert.True(t, exists)
		}
	})
}

func TestPendingPoolRemoveTransaction(t *testing.T) {
	pool := NewPendingPool(10, 5*time.Minute)
	defer pool.Close()

	tx1 := createTestTx(1)
	tx2 := createTestTx(2)
	pool.AddTransaction(createTestTxEvent(tx1, common.HexToAddress("0x1111")))
	pool.AddTransaction(createTestTxEvent(tx2, common.HexToAddress("0x1111")))

	assert.Equal(t, 2, pool.Size())

	t.Run("remove existing", func(t *testing.T) {
		pool.RemoveTransaction(tx1.Hash())
		assert.Equal(t, 1, pool.Size())

		_, exists := pool.GetTransaction(tx1.Hash())
		assert.False(t, exists)

		_, exists = pool.GetTransaction(tx2.Hash())
		assert.True(t, exists)
	})

	t.Run("remove non-existing", func(t *testing.T) {
		initialSize := pool.Size()
		pool.RemoveTransaction(common.HexToHash("0x9999"))
		assert.Equal(t, initialSize, pool.Size())
	})
}

func TestPendingPoolGetTransactionsSince(t *testing.T) {
	pool := NewPendingPool(100, 5*time.Minute)
	defer pool.Close()

	// Add 5 transactions
	for i := 0; i < 5; i++ {
		tx := createTestTx(uint64(i))
		pool.AddTransaction(createTestTxEvent(tx, common.HexToAddress("0x1111")))
	}

	t.Run("get all transactions", func(t *testing.T) {
		hashes, maxIndex := pool.GetTransactionsSince(0)
		assert.Len(t, hashes, 5)
		assert.Equal(t, uint64(5), maxIndex)
	})

	t.Run("get transactions since index 3", func(t *testing.T) {
		hashes, maxIndex := pool.GetTransactionsSince(3)
		assert.Len(t, hashes, 2) // Transactions 4 and 5
		assert.Equal(t, uint64(5), maxIndex)
	})

	t.Run("get transactions since current index", func(t *testing.T) {
		hashes, maxIndex := pool.GetTransactionsSince(5)
		assert.Len(t, hashes, 0)
		assert.Equal(t, uint64(5), maxIndex)
	})
}

func TestPendingPoolGetAllTransactions(t *testing.T) {
	pool := NewPendingPool(100, 5*time.Minute)
	defer pool.Close()

	var expectedHashes []common.Hash
	for i := 0; i < 3; i++ {
		tx := createTestTx(uint64(i))
		expectedHashes = append(expectedHashes, tx.Hash())
		pool.AddTransaction(createTestTxEvent(tx, common.HexToAddress("0x1111")))
	}

	hashes := pool.GetAllTransactions()
	assert.Len(t, hashes, 3)
	for i, h := range hashes {
		assert.Equal(t, expectedHashes[i], h)
	}
}

func TestPendingPoolCleanup(t *testing.T) {
	// Create pool with very short TTL
	pool := NewPendingPool(100, 50*time.Millisecond)
	defer pool.Close()

	tx := createTestTx(1)
	pool.AddTransaction(createTestTxEvent(tx, common.HexToAddress("0x1111")))
	assert.Equal(t, 1, pool.Size())

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)
	pool.cleanup() // Force cleanup

	assert.Equal(t, 0, pool.Size())
}

func TestFilterManagerWithPendingPool(t *testing.T) {
	pool := NewPendingPool(100, 5*time.Minute)
	defer pool.Close()

	fm := NewFilterManagerWithPendingPool(5*time.Minute, pool)
	defer fm.Close()

	// Add some pending transactions
	for i := 0; i < 5; i++ {
		tx := createTestTx(uint64(i))
		pool.AddTransaction(createTestTxEvent(tx, common.HexToAddress("0x1111")))
	}

	t.Run("create pending tx filter", func(t *testing.T) {
		filterID := fm.NewFilter(PendingTxFilterType, nil, 0, false)
		assert.NotEmpty(t, filterID)

		filter, exists := fm.GetFilter(filterID)
		require.True(t, exists)
		assert.Equal(t, PendingTxFilterType, filter.Type)
		assert.Equal(t, uint64(5), filter.LastPendingTxIndex) // Should be current index
	})

	t.Run("get pending transactions since last poll", func(t *testing.T) {
		filterID := fm.NewFilter(PendingTxFilterType, nil, 0, false)

		// First poll should return empty (filter created with current index)
		hashes, err := fm.GetPendingTransactionsSinceLastPoll(nil, nil, filterID)
		require.NoError(t, err)
		assert.Empty(t, hashes)

		// Add more transactions
		for i := 10; i < 13; i++ {
			tx := createTestTx(uint64(i))
			pool.AddTransaction(createTestTxEvent(tx, common.HexToAddress("0x2222")))
		}

		// Second poll should return new transactions
		hashes, err = fm.GetPendingTransactionsSinceLastPoll(nil, nil, filterID)
		require.NoError(t, err)
		assert.Len(t, hashes, 3)

		// Third poll should return empty (already seen)
		hashes, err = fm.GetPendingTransactionsSinceLastPoll(nil, nil, filterID)
		require.NoError(t, err)
		assert.Empty(t, hashes)
	})

	t.Run("non-pending filter returns nil", func(t *testing.T) {
		filterID := fm.NewFilter(BlockFilterType, nil, 0, false)
		hashes, err := fm.GetPendingTransactionsSinceLastPoll(nil, nil, filterID)
		require.NoError(t, err)
		assert.Nil(t, hashes)
	})
}

func TestFilterManagerWithoutPendingPool(t *testing.T) {
	fm := NewFilterManager(5 * time.Minute)
	defer fm.Close()

	filterID := fm.NewFilter(PendingTxFilterType, nil, 0, false)

	hashes, err := fm.GetPendingTransactionsSinceLastPoll(nil, nil, filterID)
	require.NoError(t, err)
	assert.Empty(t, hashes)
}

// Helper functions

func createTestTx(nonce uint64) *types.Transaction {
	return types.NewTransaction(
		nonce,
		common.HexToAddress("0xdead"),
		big.NewInt(1000),
		21000,
		big.NewInt(1000000000),
		nil,
	)
}

func createTestTxEvent(tx *types.Transaction, from common.Address) *events.TransactionEvent {
	to := tx.To()
	return &events.TransactionEvent{
		Tx:          tx,
		Hash:        tx.Hash(),
		BlockNumber: 0, // Pending
		BlockHash:   common.Hash{},
		Index:       0,
		From:        from,
		To:          to,
		Value:       tx.Value().String(),
		Receipt:     nil,
		CreatedAt:   time.Now(),
	}
}
