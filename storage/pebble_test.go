package storage

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
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

// TestPebbleStorage_GetTransaction_NotFound tests transaction not found error
func TestPebbleStorage_GetTransaction_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	_, _, err := storage.GetTransaction(ctx, common.HexToHash("0xnonexistent"))
	if err != ErrNotFound {
		t.Errorf("GetTransaction() error = %v, want ErrNotFound", err)
	}
}

// TestPebbleStorage_Transaction_DynamicFee tests EIP-1559 dynamic fee transactions
func TestPebbleStorage_Transaction_DynamicFee(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create EIP-1559 transaction (type 0x02)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     1,
		GasTipCap: big.NewInt(2000000000),
		GasFeeCap: big.NewInt(3000000000),
		Gas:       21000,
		To:        &common.Address{0x01},
		Value:     big.NewInt(1000000000),
		Data:      []byte{},
	})

	location := &TxLocation{
		BlockHeight: 200,
		TxIndex:     0,
		BlockHash:   common.HexToHash("0xblock200"),
	}

	// Store transaction
	err := storage.SetTransaction(ctx, tx, location)
	if err != nil {
		t.Fatalf("SetTransaction() error = %v", err)
	}

	// Retrieve and verify
	retrieved, loc, err := storage.GetTransaction(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("GetTransaction() error = %v", err)
	}

	if retrieved.Hash() != tx.Hash() {
		t.Errorf("Transaction hash mismatch")
	}
	if retrieved.Type() != types.DynamicFeeTxType {
		t.Errorf("Transaction type = %d, want %d", retrieved.Type(), types.DynamicFeeTxType)
	}
	if loc.BlockHeight != 200 {
		t.Errorf("BlockHeight = %d, want 200", loc.BlockHeight)
	}
}

// TestPebbleStorage_Transaction_AccessList tests EIP-2930 access list transactions
func TestPebbleStorage_Transaction_AccessList(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create EIP-2930 transaction (type 0x01)
	accessList := types.AccessList{
		{
			Address: common.HexToAddress("0x1234"),
			StorageKeys: []common.Hash{
				common.HexToHash("0xabcd"),
			},
		},
	}

	tx := types.NewTx(&types.AccessListTx{
		ChainID:    big.NewInt(1),
		Nonce:      2,
		GasPrice:   big.NewInt(1000000000),
		Gas:        25000,
		To:         &common.Address{0x02},
		Value:      big.NewInt(2000000000),
		Data:       []byte{},
		AccessList: accessList,
	})

	location := &TxLocation{
		BlockHeight: 300,
		TxIndex:     1,
		BlockHash:   common.HexToHash("0xblock300"),
	}

	// Store transaction
	err := storage.SetTransaction(ctx, tx, location)
	if err != nil {
		t.Fatalf("SetTransaction() error = %v", err)
	}

	// Retrieve and verify
	retrieved, _, err := storage.GetTransaction(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("GetTransaction() error = %v", err)
	}

	if retrieved.Hash() != tx.Hash() {
		t.Errorf("Transaction hash mismatch")
	}
	if retrieved.Type() != types.AccessListTxType {
		t.Errorf("Transaction type = %d, want %d", retrieved.Type(), types.AccessListTxType)
	}
}

// TestPebbleStorage_Transaction_WithData tests transactions with data payload
func TestPebbleStorage_Transaction_WithData(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create transaction with data (contract call or deployment)
	data := []byte{0x60, 0x60, 0x60, 0x40} // Sample contract bytecode
	tx := types.NewTransaction(
		3,
		common.HexToAddress("0xcontract"),
		big.NewInt(0),
		100000,
		big.NewInt(1000000000),
		data,
	)

	location := &TxLocation{
		BlockHeight: 400,
		TxIndex:     2,
		BlockHash:   common.HexToHash("0xblock400"),
	}

	// Store transaction
	err := storage.SetTransaction(ctx, tx, location)
	if err != nil {
		t.Fatalf("SetTransaction() error = %v", err)
	}

	// Retrieve and verify
	retrieved, _, err := storage.GetTransaction(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("GetTransaction() error = %v", err)
	}

	if retrieved.Hash() != tx.Hash() {
		t.Errorf("Transaction hash mismatch")
	}

	retrievedData := retrieved.Data()
	if len(retrievedData) != len(data) {
		t.Errorf("Data length = %d, want %d", len(retrievedData), len(data))
	}
}

// TestPebbleStorage_AddressIndex_Pagination tests address index with pagination
func TestPebbleStorage_AddressIndex_Pagination(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	addr := common.HexToAddress("0xuser1")

	// Add 10 transactions
	txHashes := make([]common.Hash, 10)
	for i := 0; i < 10; i++ {
		txHashes[i] = common.HexToHash(fmt.Sprintf("0x%02d", i))
		err := storage.AddTransactionToAddressIndex(ctx, addr, txHashes[i])
		if err != nil {
			t.Fatalf("AddTransactionToAddressIndex() error = %v", err)
		}
	}

	// Test pagination - first page (limit 5, offset 0)
	page1, err := storage.GetTransactionsByAddress(ctx, addr, 5, 0)
	if err != nil {
		t.Fatalf("GetTransactionsByAddress() error = %v", err)
	}
	if len(page1) != 5 {
		t.Errorf("Page 1 length = %d, want 5", len(page1))
	}

	// Test pagination - second page (limit 5, offset 5)
	page2, err := storage.GetTransactionsByAddress(ctx, addr, 5, 5)
	if err != nil {
		t.Fatalf("GetTransactionsByAddress() error = %v", err)
	}
	if len(page2) != 5 {
		t.Errorf("Page 2 length = %d, want 5", len(page2))
	}

	// Verify no overlap
	for _, h1 := range page1 {
		for _, h2 := range page2 {
			if h1 == h2 {
				t.Error("Pages should not overlap")
			}
		}
	}
}

// TestPebbleStorage_AddressIndex_MultipleAddresses tests multiple addresses
func TestPebbleStorage_AddressIndex_MultipleAddresses(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	tx1 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	tx2 := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	tx3 := common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")

	// Add transactions to addr1
	err := storage.AddTransactionToAddressIndex(ctx, addr1, tx1)
	if err != nil {
		t.Fatalf("AddTransactionToAddressIndex(addr1, tx1) error = %v", err)
	}
	err = storage.AddTransactionToAddressIndex(ctx, addr1, tx2)
	if err != nil {
		t.Fatalf("AddTransactionToAddressIndex(addr1, tx2) error = %v", err)
	}

	// Add transaction to addr2
	err = storage.AddTransactionToAddressIndex(ctx, addr2, tx3)
	if err != nil {
		t.Fatalf("AddTransactionToAddressIndex(addr2, tx3) error = %v", err)
	}

	// Query addr1 - should have 2 transactions
	txs1, err := storage.GetTransactionsByAddress(ctx, addr1, 10, 0)
	if err != nil {
		t.Fatalf("GetTransactionsByAddress(addr1) error = %v", err)
	}
	if len(txs1) != 2 {
		t.Errorf("addr1 has %d txs, want 2", len(txs1))
	}

	// Query addr2 - should have 1 transaction
	txs2, err := storage.GetTransactionsByAddress(ctx, addr2, 10, 0)
	if err != nil {
		t.Fatalf("GetTransactionsByAddress(addr2) error = %v", err)
	}
	if len(txs2) != 1 {
		t.Errorf("addr2 has %d txs, want 1", len(txs2))
	}
}

// TestPebbleStorage_AddressIndex_EmptyAddress tests querying empty address
func TestPebbleStorage_AddressIndex_EmptyAddress(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	addr := common.HexToAddress("0xempty")

	// Query empty address - should return empty list
	txs, err := storage.GetTransactionsByAddress(ctx, addr, 10, 0)
	if err != nil {
		t.Fatalf("GetTransactionsByAddress() error = %v", err)
	}
	if len(txs) != 0 {
		t.Errorf("Empty address should have 0 txs, got %d", len(txs))
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

// TestPebbleStorage_GetReceipt_NotFound tests receipt not found error
func TestPebbleStorage_GetReceipt_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	_, err := storage.GetReceipt(ctx, common.HexToHash("0xnonexistent"))
	if err != ErrNotFound {
		t.Errorf("GetReceipt() error = %v, want ErrNotFound", err)
	}
}

// TestPebbleStorage_Receipt_FailedTransaction tests receipts for failed transactions
func TestPebbleStorage_Receipt_FailedTransaction(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create receipt for failed transaction (status = 0)
	receipt := &types.Receipt{
		Type:              types.LegacyTxType,
		Status:            types.ReceiptStatusFailed, // 0 = failed
		CumulativeGasUsed: 21000,
		Bloom:             types.Bloom{},
		Logs:              []*types.Log{},
		TxHash:            common.HexToHash("0xfailed"),
		GasUsed:           21000,
	}

	// Store receipt
	err := storage.SetReceipt(ctx, receipt)
	if err != nil {
		t.Fatalf("SetReceipt() error = %v", err)
	}

	// Retrieve and verify
	retrieved, err := storage.GetReceipt(ctx, receipt.TxHash)
	if err != nil {
		t.Fatalf("GetReceipt() error = %v", err)
	}

	if retrieved.Status != types.ReceiptStatusFailed {
		t.Errorf("Status = %d, want %d (failed)", retrieved.Status, types.ReceiptStatusFailed)
	}
}

// TestPebbleStorage_Receipt_WithLogs tests receipts with log events
func TestPebbleStorage_Receipt_WithLogs(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create receipt with logs
	log1 := &types.Log{
		Address: common.HexToAddress("0xcontract"),
		Topics: []common.Hash{
			common.HexToHash("0xevent1"),
			common.HexToHash("0xparam1"),
		},
		Data:        []byte("test data"),
		BlockNumber: 100,
		TxIndex:     1,
		Index:       0,
	}

	receipt := &types.Receipt{
		Type:              types.LegacyTxType,
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 50000,
		Bloom:             types.Bloom{},
		Logs:              []*types.Log{log1},
		TxHash:            common.HexToHash("0xwithlogs"),
		GasUsed:           30000,
	}

	// Store receipt
	err := storage.SetReceipt(ctx, receipt)
	if err != nil {
		t.Fatalf("SetReceipt() error = %v", err)
	}

	// Retrieve and verify logs
	retrieved, err := storage.GetReceipt(ctx, receipt.TxHash)
	if err != nil {
		t.Fatalf("GetReceipt() error = %v", err)
	}

	if len(retrieved.Logs) != 1 {
		t.Fatalf("Logs count = %d, want 1", len(retrieved.Logs))
	}

	if retrieved.Logs[0].Address != log1.Address {
		t.Errorf("Log address mismatch")
	}

	if len(retrieved.Logs[0].Topics) != 2 {
		t.Errorf("Log topics count = %d, want 2", len(retrieved.Logs[0].Topics))
	}
}

// TestPebbleStorage_GetReceipts tests batch receipt retrieval
func TestPebbleStorage_GetReceipts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create and store multiple receipts
	hashes := []common.Hash{
		common.HexToHash("0xaaa"),
		common.HexToHash("0xbbb"),
		common.HexToHash("0xccc"),
	}

	for i, hash := range hashes {
		receipt := &types.Receipt{
			Type:              types.LegacyTxType,
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: uint64(21000 * (i + 1)),
			TxHash:            hash,
			GasUsed:           21000,
		}
		err := storage.SetReceipt(ctx, receipt)
		if err != nil {
			t.Fatalf("SetReceipt() error = %v", err)
		}
	}

	// Batch retrieve receipts
	receipts, err := storage.GetReceipts(ctx, hashes)
	if err != nil {
		t.Fatalf("GetReceipts() error = %v", err)
	}

	if len(receipts) != 3 {
		t.Errorf("GetReceipts() returned %d receipts, want 3", len(receipts))
	}

	// Verify order and content
	for i, receipt := range receipts {
		if receipt.TxHash != hashes[i] {
			t.Errorf("Receipt %d hash mismatch", i)
		}
		expectedGas := uint64(21000 * (i + 1))
		if receipt.CumulativeGasUsed != expectedGas {
			t.Errorf("Receipt %d gas = %d, want %d", i, receipt.CumulativeGasUsed, expectedGas)
		}
	}
}

// TestPebbleStorage_GetReceipts_PartialNotFound tests batch retrieval with some missing
func TestPebbleStorage_GetReceipts_PartialNotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Store only one receipt
	hash1 := common.HexToHash("0xaaa")
	receipt1 := &types.Receipt{
		Type:              types.LegacyTxType,
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		TxHash:            hash1,
		GasUsed:           21000,
	}
	storage.SetReceipt(ctx, receipt1)

	// Try to get multiple receipts including non-existent ones
	hashes := []common.Hash{
		hash1,
		common.HexToHash("0xnonexistent1"),
		common.HexToHash("0xnonexistent2"),
	}

	receipts, err := storage.GetReceipts(ctx, hashes)

	// Should return error for partial failure
	if err == nil {
		t.Error("GetReceipts() should return error when some receipts not found")
	}

	// But receipts slice should contain what was found (nil for missing)
	if receipts != nil && len(receipts) > 0 {
		if receipts[0] == nil {
			t.Error("First receipt should not be nil")
		}
	}
}

// TestPebbleStorage_SetReceipts tests batch receipt storage
func TestPebbleStorage_SetReceipts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple receipts
	receipts := []*types.Receipt{
		{
			Type:              types.LegacyTxType,
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
			TxHash:            common.HexToHash("0x111"),
			GasUsed:           21000,
		},
		{
			Type:              types.LegacyTxType,
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 42000,
			TxHash:            common.HexToHash("0x222"),
			GasUsed:           21000,
		},
		{
			Type:              types.LegacyTxType,
			Status:            types.ReceiptStatusFailed,
			CumulativeGasUsed: 63000,
			TxHash:            common.HexToHash("0x333"),
			GasUsed:           21000,
		},
	}

	// Batch store receipts
	err := storage.SetReceipts(ctx, receipts)
	if err != nil {
		t.Fatalf("SetReceipts() error = %v", err)
	}

	// Verify all receipts were stored
	for _, receipt := range receipts {
		retrieved, err := storage.GetReceipt(ctx, receipt.TxHash)
		if err != nil {
			t.Errorf("GetReceipt(%s) error = %v", receipt.TxHash.Hex(), err)
		}
		if retrieved.Status != receipt.Status {
			t.Errorf("Receipt status mismatch for %s", receipt.TxHash.Hex())
		}
	}
}

// createTestReceipt creates a test receipt
func createTestReceipt(txHash common.Hash, gasUsed uint64) *types.Receipt {
	return &types.Receipt{
		Type:              types.LegacyTxType,
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: gasUsed,
		Bloom:             types.Bloom{},
		Logs:              []*types.Log{},
		TxHash:            txHash,
		GasUsed:           gasUsed,
	}
}

// createTestBlockWithTxs creates a test block with transactions
func createTestBlockWithTxs(t *testing.T, height uint64, numTxs int) *types.Block {
	t.Helper()

	txs := make([]*types.Transaction, numTxs)
	for i := 0; i < numTxs; i++ {
		txs[i] = createTestTransaction(uint64(i))
	}

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
		GasLimit:    5000000,
		GasUsed:     0,
		Time:        1234567890 + height,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
	}

	if numTxs > 0 {
		return types.NewBlockWithHeader(header).WithBody(types.Body{Transactions: txs})
	}
	return types.NewBlockWithHeader(header)
}

// TestPebbleStorage_SetLogger tests logger setting
func TestPebbleStorage_SetLogger(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pebble-test-*")
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("NewPebbleStorage() error = %v", err)
	}
	defer storage.Close()

	// Create a new logger and set it
	logger := zap.NewExample()
	storage.SetLogger(logger)

	// Verify no panic occurred - test passes if we get here
}

// TestPebbleStorage_Compact tests manual compaction
func TestPebbleStorage_Compact(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create some test data
	block := createTestBlock(1)
	err := storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Test compaction with specific range
	// Note: PebbleDB requires start < end, so we use actual key ranges
	startKey := []byte{0x00}
	endKey := []byte{0xff}
	err = storage.Compact(ctx, startKey, endKey)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	// Verify data is still accessible after compaction
	retrievedBlock, err := storage.GetBlock(ctx, 1)
	if err != nil {
		t.Fatalf("GetBlock() after compaction error = %v", err)
	}
	if retrievedBlock.Hash() != block.Hash() {
		t.Error("Block hash mismatch after compaction")
	}
}

// TestPebbleStorage_Compact_Closed tests compaction on closed storage
func TestPebbleStorage_Compact_Closed(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	cleanup() // Close immediately

	ctx := context.Background()
	err := storage.Compact(ctx, nil, nil)
	if err != ErrClosed {
		t.Errorf("Compact() on closed storage should return ErrClosed, got %v", err)
	}
}

// TestPebbleStorage_GetReceiptsByBlockNumber tests receipt retrieval by block number
func TestPebbleStorage_GetReceiptsByBlockNumber(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create test block with transactions
	block := createTestBlockWithTxs(t, 100, 3)
	err := storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Create and store receipts for each transaction
	expectedReceipts := make([]*types.Receipt, 0, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		receipt := createTestReceipt(tx.Hash(), uint64(21000*(i+1)))
		err := storage.SetReceipt(ctx, receipt)
		if err != nil {
			t.Fatalf("SetReceipt() error = %v", err)
		}
		expectedReceipts = append(expectedReceipts, receipt)
	}

	// Retrieve receipts by block number
	receipts, err := storage.GetReceiptsByBlockNumber(ctx, 100)
	if err != nil {
		t.Fatalf("GetReceiptsByBlockNumber() error = %v", err)
	}
	if len(receipts) != 3 {
		t.Errorf("GetReceiptsByBlockNumber() returned %d receipts, want 3", len(receipts))
	}

	// Verify all receipts are correct
	for i, receipt := range receipts {
		if receipt.TxHash != expectedReceipts[i].TxHash {
			t.Errorf("Receipt %d hash mismatch", i)
		}
		if receipt.Status != expectedReceipts[i].Status {
			t.Errorf("Receipt %d status mismatch", i)
		}
		if receipt.CumulativeGasUsed != expectedReceipts[i].CumulativeGasUsed {
			t.Errorf("Receipt %d cumulative gas used = %d, want %d", i, receipt.CumulativeGasUsed, expectedReceipts[i].CumulativeGasUsed)
		}
	}
}

// TestPebbleStorage_GetReceiptsByBlockNumber_EmptyBlock tests with block that has no transactions
func TestPebbleStorage_GetReceiptsByBlockNumber_EmptyBlock(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create block with no transactions
	block := createTestBlockWithTxs(t, 100, 0)
	err := storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Retrieve receipts - should return empty slice
	receipts, err := storage.GetReceiptsByBlockNumber(ctx, 100)
	if err != nil {
		t.Fatalf("GetReceiptsByBlockNumber() error = %v", err)
	}
	if len(receipts) != 0 {
		t.Errorf("Expected 0 receipts for empty block, got %d", len(receipts))
	}
}

// TestPebbleStorage_GetReceiptsByBlockNumber_NotFound tests with non-existent block
func TestPebbleStorage_GetReceiptsByBlockNumber_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get receipts for non-existent block
	_, err := storage.GetReceiptsByBlockNumber(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetReceiptsByBlockNumber() for non-existent block should return ErrNotFound, got %v", err)
	}
}

// TestPebbleStorage_GetReceiptsByBlockNumber_MissingReceipts tests with missing receipts
func TestPebbleStorage_GetReceiptsByBlockNumber_MissingReceipts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create block with transactions but don't store receipts
	block := createTestBlockWithTxs(t, 100, 3)
	err := storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Retrieve receipts - should return empty slice (missing receipts are skipped)
	receipts, err := storage.GetReceiptsByBlockNumber(ctx, 100)
	if err != nil {
		t.Fatalf("GetReceiptsByBlockNumber() error = %v", err)
	}
	if len(receipts) != 0 {
		t.Errorf("Expected 0 receipts when none stored, got %d", len(receipts))
	}
}

// TestPebbleStorage_GetReceiptsByBlockNumber_Closed tests on closed storage
func TestPebbleStorage_GetReceiptsByBlockNumber_Closed(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	cleanup() // Close immediately

	ctx := context.Background()
	_, err := storage.GetReceiptsByBlockNumber(ctx, 100)
	if err != ErrClosed {
		t.Errorf("GetReceiptsByBlockNumber() on closed storage should return ErrClosed, got %v", err)
	}
}

// TestPebbleStorage_GetReceiptsByBlockHash tests receipt retrieval by block hash
func TestPebbleStorage_GetReceiptsByBlockHash(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create test block with transactions
	block := createTestBlockWithTxs(t, 200, 2)
	blockHash := block.Hash()
	err := storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Create and store receipts
	expectedReceipts := make([]*types.Receipt, 0, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		receipt := createTestReceipt(tx.Hash(), uint64(25000*(i+1)))
		err := storage.SetReceipt(ctx, receipt)
		if err != nil {
			t.Fatalf("SetReceipt() error = %v", err)
		}
		expectedReceipts = append(expectedReceipts, receipt)
	}

	// Retrieve receipts by block hash
	receipts, err := storage.GetReceiptsByBlockHash(ctx, blockHash)
	if err != nil {
		t.Fatalf("GetReceiptsByBlockHash() error = %v", err)
	}
	if len(receipts) != 2 {
		t.Errorf("GetReceiptsByBlockHash() returned %d receipts, want 2", len(receipts))
	}

	// Verify receipts
	for i, receipt := range receipts {
		if receipt.TxHash != expectedReceipts[i].TxHash {
			t.Errorf("Receipt %d hash mismatch", i)
		}
	}
}

// TestPebbleStorage_GetReceiptsByBlockHash_NotFound tests with non-existent block hash
func TestPebbleStorage_GetReceiptsByBlockHash_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get receipts for non-existent block hash
	nonExistentHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	_, err := storage.GetReceiptsByBlockHash(ctx, nonExistentHash)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetReceiptsByBlockHash() for non-existent hash should return ErrNotFound, got %v", err)
	}
}

// TestPebbleStorage_GetReceiptsByBlockHash_Closed tests on closed storage
func TestPebbleStorage_GetReceiptsByBlockHash_Closed(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	cleanup() // Close immediately

	ctx := context.Background()
	blockHash := common.HexToHash("0x1234")
	_, err := storage.GetReceiptsByBlockHash(ctx, blockHash)
	if err != ErrClosed {
		t.Errorf("GetReceiptsByBlockHash() on closed storage should return ErrClosed, got %v", err)
	}
}

// TestPebbleStorage_Close_AlreadyClosed tests closing storage twice
func TestPebbleStorage_Close_AlreadyClosed(t *testing.T) {
	storage, cleanup := setupTestStorage(t)

	// First close should succeed
	err := storage.Close()
	if err != nil {
		t.Errorf("First Close() error = %v", err)
	}

	// Second close should be no-op (no error)
	err = storage.Close()
	if err != nil {
		t.Errorf("Second Close() error = %v, should be no-op", err)
	}

	// Clean up temp directory
	cleanup()
}

// TestPebbleStorage_DeleteBlock_NotFound tests deleting non-existent block
func TestPebbleStorage_DeleteBlock_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Delete non-existent block should succeed (idempotent)
	err := storage.DeleteBlock(ctx, 999)
	if err != nil {
		t.Errorf("DeleteBlock() for non-existent block should succeed, got %v", err)
	}
}

// TestPebbleStorage_DeleteBlock_Success tests successful block deletion
func TestPebbleStorage_DeleteBlock_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create and store block
	block := createTestBlock(100)
	err := storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Verify block exists
	exists, err := storage.HasBlock(ctx, 100)
	if err != nil {
		t.Fatalf("HasBlock() error = %v", err)
	}
	if !exists {
		t.Fatal("Block should exist before deletion")
	}

	// Delete block
	err = storage.DeleteBlock(ctx, 100)
	if err != nil {
		t.Fatalf("DeleteBlock() error = %v", err)
	}

	// Verify block no longer exists
	exists, err = storage.HasBlock(ctx, 100)
	if err != nil {
		t.Fatalf("HasBlock() after deletion error = %v", err)
	}
	if exists {
		t.Error("Block should not exist after deletion")
	}
}

// TestPebbleStorage_DeleteBlock_Closed tests deletion on closed storage
func TestPebbleStorage_DeleteBlock_Closed(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	cleanup() // Close immediately

	ctx := context.Background()
	err := storage.DeleteBlock(ctx, 100)
	if err != ErrClosed {
		t.Errorf("DeleteBlock() on closed storage should return ErrClosed, got %v", err)
	}
}

// TestPebbleStorage_DeleteBlock_ReadOnly tests deletion on read-only storage
func TestPebbleStorage_DeleteBlock_ReadOnly(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pebble-test-readonly-*")
	defer os.RemoveAll(tmpDir)

	// Create storage with test data
	cfg := DefaultConfig(tmpDir)
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("NewPebbleStorage() error = %v", err)
	}

	ctx := context.Background()
	block := createTestBlock(100)
	err = storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}
	storage.Close()

	// Reopen as read-only
	cfg.ReadOnly = true
	roStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("NewPebbleStorage() read-only error = %v", err)
	}
	defer roStorage.Close()

	// Try to delete - should fail with ErrReadOnly
	err = roStorage.DeleteBlock(ctx, 100)
	if err != ErrReadOnly {
		t.Errorf("DeleteBlock() on read-only storage should return ErrReadOnly, got %v", err)
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

// TestPebbleStorage_Batch_SetReceipts tests batch SetReceipts method
func TestPebbleStorage_Batch_SetReceipts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create batch
	batch := storage.NewBatch()
	defer batch.Close()

	// Create multiple receipts
	receipts := []*types.Receipt{
		{
			Type:              types.LegacyTxType,
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
			TxHash:            common.HexToHash("0x111"),
		},
		{
			Type:              types.LegacyTxType,
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 42000,
			TxHash:            common.HexToHash("0x222"),
		},
	}

	// Use batch SetReceipts method
	err := batch.SetReceipts(ctx, receipts)
	if err != nil {
		t.Fatalf("batch.SetReceipts() error = %v", err)
	}

	// Commit batch
	err = batch.Commit()
	if err != nil {
		t.Fatalf("batch.Commit() error = %v", err)
	}

	// Verify all receipts were stored
	for _, receipt := range receipts {
		retrieved, err := storage.GetReceipt(ctx, receipt.TxHash)
		if err != nil {
			t.Errorf("GetReceipt(%s) error = %v", receipt.TxHash.Hex(), err)
		}
		if retrieved.CumulativeGasUsed != receipt.CumulativeGasUsed {
			t.Errorf("Receipt gas used mismatch for %s", receipt.TxHash.Hex())
		}
	}
}

// TestPebbleStorage_Batch_SetBlocks tests batch SetBlocks method
func TestPebbleStorage_Batch_SetBlocks(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create batch
	batch := storage.NewBatch()
	defer batch.Close()

	// Create multiple blocks
	blocks := []*types.Block{
		createTestBlock(100),
		createTestBlock(101),
		createTestBlock(102),
	}

	// Use batch SetBlocks method
	err := batch.SetBlocks(ctx, blocks)
	if err != nil {
		t.Fatalf("batch.SetBlocks() error = %v", err)
	}

	// Commit batch
	err = batch.Commit()
	if err != nil {
		t.Fatalf("batch.Commit() error = %v", err)
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

// TestPebbleStorage_Batch_DeleteBlock tests batch DeleteBlock method
func TestPebbleStorage_Batch_DeleteBlock(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// First store a block
	block := createTestBlock(100)
	err := storage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Verify block exists
	exists, err := storage.HasBlock(ctx, 100)
	if err != nil || !exists {
		t.Fatal("Block should exist before deletion")
	}

	// Create batch and delete block
	batch := storage.NewBatch()
	defer batch.Close()

	err = batch.DeleteBlock(ctx, 100)
	if err != nil {
		t.Fatalf("batch.DeleteBlock() error = %v", err)
	}

	// Commit batch
	err = batch.Commit()
	if err != nil {
		t.Fatalf("batch.Commit() error = %v", err)
	}

	// Verify block no longer exists
	exists, err = storage.HasBlock(ctx, 100)
	if err != nil {
		t.Fatalf("HasBlock() after deletion error = %v", err)
	}
	if exists {
		t.Error("Block should not exist after batch deletion")
	}
}
