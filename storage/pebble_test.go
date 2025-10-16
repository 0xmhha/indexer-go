package storage

import (
	"context"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// setupTestStorage creates a temporary PebbleDB storage for testing
func setupTestStorage(t *testing.T) (Storage, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cfg := DefaultConfig(tmpDir)
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create storage: %v", err)
	}

	cleanup := func() {
		storage.Close()
		os.RemoveAll(tmpDir)
	}

	return storage, cleanup
}

// Test helper to create a test block
func createTestBlock(height uint64) *types.Block {
	header := &types.Header{
		ParentHash:  common.Hash{},
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.Address{},
		Root:        common.Hash{},
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(int64(height)),
		GasLimit:    5000,
		GasUsed:     0,
		Time:        1234567890 + height,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
	}
	return types.NewBlockWithHeader(header)
}

// Test helper to create a test transaction
func createTestTransaction(nonce uint64) *types.Transaction {
	return types.NewTransaction(
		nonce,
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		big.NewInt(1000000000),
		21000,
		big.NewInt(1000000000),
		[]byte{},
	)
}

func TestNewPebbleStorage(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		storage, cleanup := setupTestStorage(t)
		defer cleanup()

		if storage == nil {
			t.Fatal("NewPebbleStorage() returned nil")
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		cfg := DefaultConfig("")
		_, err := NewPebbleStorage(cfg)
		if err == nil {
			t.Error("NewPebbleStorage() should fail with empty path")
		}
	})

	t.Run("nil config", func(t *testing.T) {
		_, err := NewPebbleStorage(nil)
		if err == nil {
			t.Error("NewPebbleStorage() should fail with nil config")
		}
	})
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			"valid config",
			DefaultConfig("/tmp/test"),
			false,
		},
		{
			"empty path",
			&Config{Path: ""},
			true,
		},
		{
			"negative cache",
			&Config{Path: "/tmp", Cache: -1},
			true,
		},
		{
			"negative max open files",
			&Config{Path: "/tmp", MaxOpenFiles: -1},
			true,
		},
		{
			"zero compaction concurrency",
			&Config{Path: "/tmp", CompactionConcurrency: 0},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPebbleStorage_LatestHeight(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Should return 0 initially
	height, err := storage.GetLatestHeight(ctx)
	if err != nil {
		t.Fatalf("GetLatestHeight() error = %v", err)
	}
	if height != 0 {
		t.Errorf("Initial height = %d, want 0", height)
	}

	// Set height
	err = storage.SetLatestHeight(ctx, 100)
	if err != nil {
		t.Fatalf("SetLatestHeight() error = %v", err)
	}

	// Get height
	height, err = storage.GetLatestHeight(ctx)
	if err != nil {
		t.Fatalf("GetLatestHeight() error = %v", err)
	}
	if height != 100 {
		t.Errorf("GetLatestHeight() = %d, want 100", height)
	}

	// Update height
	err = storage.SetLatestHeight(ctx, 200)
	if err != nil {
		t.Fatalf("SetLatestHeight() error = %v", err)
	}

	height, err = storage.GetLatestHeight(ctx)
	if err != nil {
		t.Fatalf("GetLatestHeight() error = %v", err)
	}
	if height != 200 {
		t.Errorf("GetLatestHeight() = %d, want 200", height)
	}
}

func TestPebbleStorage_Block(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create test block
	block := createTestBlock(100)

	// Block should not exist initially
	exists, err := storage.HasBlock(ctx, 100)
	if err != nil {
		t.Fatalf("HasBlock() error = %v", err)
	}
	if exists {
		t.Error("Block should not exist initially")
	}

	// Store block
	err = storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Block should exist now
	exists, err = storage.HasBlock(ctx, 100)
	if err != nil {
		t.Fatalf("HasBlock() error = %v", err)
	}
	if !exists {
		t.Error("Block should exist after SetBlock")
	}

	// Retrieve block
	retrieved, err := storage.GetBlock(ctx, 100)
	if err != nil {
		t.Fatalf("GetBlock() error = %v", err)
	}

	// Verify block
	if retrieved.Hash() != block.Hash() {
		t.Errorf("Block hash mismatch: got %s, want %s",
			retrieved.Hash().Hex(), block.Hash().Hex())
	}
	if retrieved.Number().Uint64() != 100 {
		t.Errorf("Block number = %d, want 100", retrieved.Number().Uint64())
	}
}

func TestPebbleStorage_GetBlock_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	_, err := storage.GetBlock(ctx, 999)
	if err != ErrNotFound {
		t.Errorf("GetBlock() error = %v, want ErrNotFound", err)
	}
}

func TestPebbleStorage_GetBlockByHash(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create and store block
	block := createTestBlock(100)
	err := storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Retrieve by hash
	retrieved, err := storage.GetBlockByHash(ctx, block.Hash())
	if err != nil {
		t.Fatalf("GetBlockByHash() error = %v", err)
	}

	if retrieved.Hash() != block.Hash() {
		t.Errorf("Block hash mismatch")
	}
}

func TestPebbleStorage_Transaction(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create test transaction
	tx := createTestTransaction(0)
	location := &TxLocation{
		BlockHeight: 100,
		TxIndex:     5,
		BlockHash:   common.HexToHash("0xabcd"),
	}

	// Transaction should not exist initially
	exists, err := storage.HasTransaction(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("HasTransaction() error = %v", err)
	}
	if exists {
		t.Error("Transaction should not exist initially")
	}

	// Store transaction
	err = storage.SetTransaction(ctx, tx, location)
	if err != nil {
		t.Fatalf("SetTransaction() error = %v", err)
	}

	// Transaction should exist now
	exists, err = storage.HasTransaction(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("HasTransaction() error = %v", err)
	}
	if !exists {
		t.Error("Transaction should exist after SetTransaction")
	}

	// Retrieve transaction
	retrieved, loc, err := storage.GetTransaction(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("GetTransaction() error = %v", err)
	}

	// Verify transaction
	if retrieved.Hash() != tx.Hash() {
		t.Errorf("Transaction hash mismatch")
	}
	if loc.BlockHeight != location.BlockHeight {
		t.Errorf("BlockHeight = %d, want %d", loc.BlockHeight, location.BlockHeight)
	}
	if loc.TxIndex != location.TxIndex {
		t.Errorf("TxIndex = %d, want %d", loc.TxIndex, location.TxIndex)
	}
}

func TestPebbleStorage_Receipt(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create test receipt
	receipt := &types.Receipt{
		Type:              types.LegacyTxType,
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		Bloom:             types.Bloom{},
		Logs:              []*types.Log{},
		TxHash:            common.HexToHash("0x1234"),
		GasUsed:           21000,
	}

	// Store receipt
	err := storage.SetReceipt(ctx, receipt)
	if err != nil {
		t.Fatalf("SetReceipt() error = %v", err)
	}

	// Retrieve receipt by TxHash
	retrieved, err := storage.GetReceipt(ctx, receipt.TxHash)
	if err != nil {
		t.Fatalf("GetReceipt() error = %v", err)
	}

	// Verify receipt
	if retrieved.Status != receipt.Status {
		t.Errorf("Status = %d, want %d", retrieved.Status, receipt.Status)
	}
}

func TestPebbleStorage_AddressIndex(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	tx1 := common.HexToHash("0xaaa")
	tx2 := common.HexToHash("0xbbb")
	tx3 := common.HexToHash("0xccc")

	// Add transactions to address index
	err := storage.AddTransactionToAddressIndex(ctx, addr, tx1)
	if err != nil {
		t.Fatalf("AddTransactionToAddressIndex() error = %v", err)
	}

	err = storage.AddTransactionToAddressIndex(ctx, addr, tx2)
	if err != nil {
		t.Fatalf("AddTransactionToAddressIndex() error = %v", err)
	}

	err = storage.AddTransactionToAddressIndex(ctx, addr, tx3)
	if err != nil {
		t.Fatalf("AddTransactionToAddressIndex() error = %v", err)
	}

	// Query transactions for address
	txHashes, err := storage.GetTransactionsByAddress(ctx, addr, 10, 0)
	if err != nil {
		t.Fatalf("GetTransactionsByAddress() error = %v", err)
	}

	if len(txHashes) != 3 {
		t.Errorf("GetTransactionsByAddress() returned %d txs, want 3", len(txHashes))
	}
}

func TestPebbleStorage_Batch(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create batch
	batch := storage.NewBatch()
	if batch == nil {
		t.Fatal("NewBatch() returned nil")
	}
	defer batch.Close()

	// Add operations to batch
	block1 := createTestBlock(100)
	block2 := createTestBlock(101)
	block3 := createTestBlock(102)

	err := batch.SetBlock(ctx, block1)
	if err != nil {
		t.Fatalf("batch.SetBlock() error = %v", err)
	}

	err = batch.SetBlock(ctx, block2)
	if err != nil {
		t.Fatalf("batch.SetBlock() error = %v", err)
	}

	err = batch.SetBlock(ctx, block3)
	if err != nil {
		t.Fatalf("batch.SetBlock() error = %v", err)
	}

	// Blocks should not be visible before commit
	_, err = storage.GetBlock(ctx, 100)
	if err != ErrNotFound {
		t.Error("Blocks should not be visible before batch commit")
	}

	// Commit batch
	err = batch.Commit()
	if err != nil {
		t.Fatalf("batch.Commit() error = %v", err)
	}

	// Blocks should be visible after commit
	retrieved, err := storage.GetBlock(ctx, 100)
	if err != nil {
		t.Fatalf("GetBlock() after commit error = %v", err)
	}
	if retrieved.Hash() != block1.Hash() {
		t.Error("Block mismatch after batch commit")
	}
}

func TestPebbleStorage_BatchComprehensive(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create batch
	batch := storage.NewBatch()
	if batch == nil {
		t.Fatal("NewBatch() returned nil")
	}
	defer batch.Close()

	// Test batch operations
	// 1. SetLatestHeight
	err := batch.SetLatestHeight(ctx, 500)
	if err != nil {
		t.Fatalf("batch.SetLatestHeight() error = %v", err)
	}

	// 2. SetBlock
	block := createTestBlock(100)
	err = batch.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("batch.SetBlock() error = %v", err)
	}

	// 3. SetTransaction
	tx := createTestTransaction(0)
	location := &TxLocation{
		BlockHeight: 100,
		TxIndex:     0,
		BlockHash:   block.Hash(),
	}
	err = batch.SetTransaction(ctx, tx, location)
	if err != nil {
		t.Fatalf("batch.SetTransaction() error = %v", err)
	}

	// 4. SetReceipt
	receipt := &types.Receipt{
		Type:              types.LegacyTxType,
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		TxHash:            tx.Hash(),
	}
	err = batch.SetReceipt(ctx, receipt)
	if err != nil {
		t.Fatalf("batch.SetReceipt() error = %v", err)
	}

	// 5. AddTransactionToAddressIndex
	addr := common.HexToAddress("0x1111")
	err = batch.AddTransactionToAddressIndex(ctx, addr, tx.Hash())
	if err != nil {
		t.Fatalf("batch.AddTransactionToAddressIndex() error = %v", err)
	}

	// 6. Check Count()
	if batch.Count() == 0 {
		t.Error("batch.Count() should be > 0")
	}

	// Commit batch
	err = batch.Commit()
	if err != nil {
		t.Fatalf("batch.Commit() error = %v", err)
	}

	// Verify all data was written
	height, err := storage.GetLatestHeight(ctx)
	if err != nil || height != 500 {
		t.Errorf("GetLatestHeight() = %d, want 500", height)
	}

	retrievedBlock, err := storage.GetBlock(ctx, 100)
	if err != nil {
		t.Errorf("GetBlock() error = %v", err)
	}
	if retrievedBlock.Hash() != block.Hash() {
		t.Error("Block hash mismatch")
	}

	retrievedTx, _, err := storage.GetTransaction(ctx, tx.Hash())
	if err != nil {
		t.Errorf("GetTransaction() error = %v", err)
	}
	if retrievedTx.Hash() != tx.Hash() {
		t.Error("Transaction hash mismatch")
	}

	retrievedReceipt, err := storage.GetReceipt(ctx, tx.Hash())
	if err != nil {
		t.Errorf("GetReceipt() error = %v", err)
	}
	if retrievedReceipt.Status != receipt.Status {
		t.Error("Receipt status mismatch")
	}
}

func TestPebbleStorage_BatchReset(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	batch := storage.NewBatch()
	defer batch.Close()

	// Add some operations
	batch.SetLatestHeight(ctx, 100)
	batch.SetBlock(ctx, createTestBlock(1))

	initialCount := batch.Count()
	if initialCount == 0 {
		t.Error("Batch should have operations")
	}

	// Reset batch
	batch.Reset()

	if batch.Count() != 0 {
		t.Errorf("After Reset(), Count() = %d, want 0", batch.Count())
	}
}

func TestPebbleStorage_GetBlocks(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Store multiple blocks
	for i := uint64(100); i <= 110; i++ {
		block := createTestBlock(i)
		err := storage.SetBlock(ctx, block)
		if err != nil {
			t.Fatalf("SetBlock(%d) error = %v", i, err)
		}
	}

	// Get block range
	blocks, err := storage.GetBlocks(ctx, 100, 105)
	if err != nil {
		t.Fatalf("GetBlocks() error = %v", err)
	}

	if len(blocks) != 6 { // 100, 101, 102, 103, 104, 105
		t.Errorf("GetBlocks() returned %d blocks, want 6", len(blocks))
	}

	// Verify order
	for i, block := range blocks {
		expected := uint64(100 + i)
		if block.Number().Uint64() != expected {
			t.Errorf("Block %d has number %d, want %d",
				i, block.Number().Uint64(), expected)
		}
	}
}

func TestPebbleStorage_SetBlocks(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple blocks
	blocks := []*types.Block{
		createTestBlock(100),
		createTestBlock(101),
		createTestBlock(102),
	}

	// Store all blocks atomically
	err := storage.SetBlocks(ctx, blocks)
	if err != nil {
		t.Fatalf("SetBlocks() error = %v", err)
	}

	// Verify all blocks were stored
	for _, block := range blocks {
		retrieved, err := storage.GetBlock(ctx, block.Number().Uint64())
		if err != nil {
			t.Errorf("GetBlock(%d) error = %v", block.Number().Uint64(), err)
		}
		if retrieved.Hash() != block.Hash() {
			t.Errorf("Block hash mismatch for height %d", block.Number().Uint64())
		}
	}
}

func TestPebbleStorage_DeleteBlock(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Store block
	block := createTestBlock(100)
	err := storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Verify block exists
	exists, err := storage.HasBlock(ctx, 100)
	if err != nil || !exists {
		t.Fatal("Block should exist")
	}

	// Delete block
	err = storage.DeleteBlock(ctx, 100)
	if err != nil {
		t.Fatalf("DeleteBlock() error = %v", err)
	}

	// Verify block is deleted
	exists, err = storage.HasBlock(ctx, 100)
	if err != nil {
		t.Fatalf("HasBlock() error = %v", err)
	}
	if exists {
		t.Error("Block should not exist after deletion")
	}
}

func TestPebbleStorage_Close(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pebble-test-*")
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("NewPebbleStorage() error = %v", err)
	}

	// Close storage
	err = storage.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Operations after close should fail
	ctx := context.Background()
	_, err = storage.GetLatestHeight(ctx)
	if err != ErrClosed {
		t.Errorf("Operations after Close() should return ErrClosed, got %v", err)
	}
}

func TestPebbleStorage_ReadOnly(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pebble-test-*")
	defer os.RemoveAll(tmpDir)

	// Create storage and write some data
	cfg := DefaultConfig(tmpDir)
	storage, _ := NewPebbleStorage(cfg)
	ctx := context.Background()

	block := createTestBlock(100)
	storage.SetBlock(ctx, block)
	storage.Close()

	// Open in read-only mode
	cfg.ReadOnly = true
	roStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("NewPebbleStorage() read-only error = %v", err)
	}
	defer roStorage.Close()

	// Read should work
	retrieved, err := roStorage.GetBlock(ctx, 100)
	if err != nil {
		t.Errorf("GetBlock() in read-only mode error = %v", err)
	}
	if retrieved.Hash() != block.Hash() {
		t.Error("Block mismatch in read-only mode")
	}

	// Write should fail
	err = roStorage.SetBlock(ctx, createTestBlock(101))
	if err != ErrReadOnly {
		t.Errorf("SetBlock() in read-only mode should return ErrReadOnly, got %v", err)
	}
}

func TestPebbleStorage_Concurrent(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			block := createTestBlock(uint64(n))
			err := storage.SetBlock(ctx, block)
			if err != nil {
				t.Errorf("Concurrent SetBlock(%d) error = %v", n, err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all blocks were stored
	for i := 0; i < 10; i++ {
		exists, err := storage.HasBlock(ctx, uint64(i))
		if err != nil {
			t.Errorf("HasBlock(%d) error = %v", i, err)
		}
		if !exists {
			t.Errorf("Block %d should exist", i)
		}
	}
}

func BenchmarkPebbleStorage_SetBlock(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "pebble-bench-*")
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	storage, _ := NewPebbleStorage(cfg)
	defer storage.Close()

	ctx := context.Background()
	block := createTestBlock(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.SetBlock(ctx, block)
	}
}

func BenchmarkPebbleStorage_GetBlock(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "pebble-bench-*")
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	storage, _ := NewPebbleStorage(cfg)
	defer storage.Close()

	ctx := context.Background()
	block := createTestBlock(100)
	storage.SetBlock(ctx, block)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.GetBlock(ctx, 100)
	}
}
