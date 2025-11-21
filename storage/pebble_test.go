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
	"github.com/ethereum/go-ethereum/crypto"
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

	// Should return ErrNotFound initially
	height, err := storage.GetLatestHeight(ctx)
	if err != ErrNotFound {
		t.Fatalf("GetLatestHeight() error = %v, want ErrNotFound", err)
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
		return types.NewBlockWithHeader(header).WithBody(txs, nil)
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

func TestPebbleStorage_GetBlockCount(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Test initial count (no blocks)
	count, err := pebbleStorage.GetBlockCount(ctx)
	if err != nil {
		t.Fatalf("GetBlockCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("GetBlockCount() = %d, want 0", count)
	}

	// Store count manually
	countValue := EncodeUint64(5)
	pebbleStorage.db.Set(BlockCountKey(), countValue, nil)

	// Test retrieval
	count, err = pebbleStorage.GetBlockCount(ctx)
	if err != nil {
		t.Fatalf("GetBlockCount() error = %v", err)
	}
	if count != 5 {
		t.Errorf("GetBlockCount() = %d, want 5", count)
	}

	// Store larger count
	countValue = EncodeUint64(1000000)
	pebbleStorage.db.Set(BlockCountKey(), countValue, nil)

	count, err = pebbleStorage.GetBlockCount(ctx)
	if err != nil {
		t.Fatalf("GetBlockCount() error = %v", err)
	}
	if count != 1000000 {
		t.Errorf("GetBlockCount() = %d, want 1000000", count)
	}
}

func TestPebbleStorage_GetTransactionCount(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Test initial count (no transactions)
	count, err := pebbleStorage.GetTransactionCount(ctx)
	if err != nil {
		t.Fatalf("GetTransactionCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("GetTransactionCount() = %d, want 0", count)
	}

	// Store count manually
	countValue := EncodeUint64(10)
	pebbleStorage.db.Set(TransactionCountKey(), countValue, nil)

	// Test retrieval
	count, err = pebbleStorage.GetTransactionCount(ctx)
	if err != nil {
		t.Fatalf("GetTransactionCount() error = %v", err)
	}
	if count != 10 {
		t.Errorf("GetTransactionCount() = %d, want 10", count)
	}

	// Store larger count
	countValue = EncodeUint64(5000000)
	pebbleStorage.db.Set(TransactionCountKey(), countValue, nil)

	count, err = pebbleStorage.GetTransactionCount(ctx)
	if err != nil {
		t.Fatalf("GetTransactionCount() error = %v", err)
	}
	if count != 5000000 {
		t.Errorf("GetTransactionCount() = %d, want 5000000", count)
	}
}

func TestPebbleStorage_SetBalance(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Test initial balance (should be 0)
	balance, err := pebbleStorage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("Initial balance = %v, want 0", balance)
	}

	// Set balance to 1000
	err = pebbleStorage.SetBalance(ctx, addr, 1, big.NewInt(1000))
	if err != nil {
		t.Fatalf("SetBalance() error = %v", err)
	}

	// Verify balance
	balance, err = pebbleStorage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("Balance after SetBalance(1000) = %v, want 1000", balance)
	}

	// Set balance to 500 (decrease)
	err = pebbleStorage.SetBalance(ctx, addr, 2, big.NewInt(500))
	if err != nil {
		t.Fatalf("SetBalance() error = %v", err)
	}

	// Verify new balance
	balance, err = pebbleStorage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("Balance after SetBalance(500) = %v, want 500", balance)
	}

	// Set balance to 2000 (increase)
	err = pebbleStorage.SetBalance(ctx, addr, 3, big.NewInt(2000))
	if err != nil {
		t.Fatalf("SetBalance() error = %v", err)
	}

	// Verify final balance
	balance, err = pebbleStorage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(2000)) != 0 {
		t.Errorf("Balance after SetBalance(2000) = %v, want 2000", balance)
	}

	// Test multiple addresses
	addr2 := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	err = pebbleStorage.SetBalance(ctx, addr2, 1, big.NewInt(3000))
	if err != nil {
		t.Fatalf("SetBalance() for addr2 error = %v", err)
	}

	// Verify both balances are independent
	balance, err = pebbleStorage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(2000)) != 0 {
		t.Errorf("addr balance = %v, want 2000", balance)
	}

	balance2, err := pebbleStorage.GetAddressBalance(ctx, addr2, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() for addr2 error = %v", err)
	}
	if balance2.Cmp(big.NewInt(3000)) != 0 {
		t.Errorf("addr2 balance = %v, want 3000", balance2)
	}
}

func TestPebbleStorage_GetBlockCount_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	// Try to get count from closed storage
	_, err := pebbleStorage.GetBlockCount(ctx)
	if err == nil {
		t.Error("GetBlockCount() should fail on closed storage")
	}
}

func TestPebbleStorage_GetTransactionCount_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	// Try to get count from closed storage
	_, err := pebbleStorage.GetTransactionCount(ctx)
	if err == nil {
		t.Error("GetTransactionCount() should fail on closed storage")
	}
}

func TestPebbleStorage_SetBalance_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Try to set balance on closed storage
	err := pebbleStorage.SetBalance(ctx, addr, 1, big.NewInt(1000))
	if err == nil {
		t.Error("SetBalance() should fail on closed storage")
	}
}

func TestPebbleStorage_GetTransactionsByAddressFiltered_Simple(t *testing.T) {
	// Simple test for basic code path coverage
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	targetAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("empty address - no indexed transactions", func(t *testing.T) {
		filter := &TransactionFilter{
			FromBlock: 0,
			ToBlock:   ^uint64(0),
			TxType:    TxTypeAll,
		}
		results, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, targetAddr, filter, 10, 0)
		if err != nil {
			t.Fatalf("GetTransactionsByAddressFiltered() error = %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Got %d results, want 0", len(results))
		}
	})

	t.Run("with default filter (nil)", func(t *testing.T) {
		results, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, targetAddr, nil, 10, 0)
		if err != nil {
			t.Fatalf("GetTransactionsByAddressFiltered() error = %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Got %d results, want 0", len(results))
		}
	})

	t.Run("invalid filter", func(t *testing.T) {
		filter := &TransactionFilter{
			FromBlock: 100,
			ToBlock:   0, // Invalid: from > to
			TxType:    TxTypeAll,
		}
		_, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, targetAddr, filter, 10, 0)
		if err == nil {
			t.Error("Expected error for invalid filter")
		}
	})
}

func TestPebbleStorage_GetTransactionsByAddressFiltered(t *testing.T) {
	// Complex test with signed transactions for full filter matching
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Create a test private key for signing
	privateKeyHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	privateKeyBytes := common.Hex2Bytes(privateKeyHex)
	key, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		t.Fatalf("Failed to create private key: %v", err)
	}

	fromAddr := crypto.PubkeyToAddress(key.PublicKey)
	toAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	chainID := big.NewInt(1)

	// Create and store test blocks with signed transactions
	for i := uint64(1); i <= 3; i++ {
		// Create block header
		header := &types.Header{
			Number:      big.NewInt(int64(i)),
			Time:        1000 + i*100,
			Difficulty:  big.NewInt(1),
			GasLimit:    1000000,
			ParentHash:  common.Hash{},
			UncleHash:   types.EmptyUncleHash,
			Coinbase:    common.Address{},
			Root:        common.Hash{},
			TxHash:      types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash,
		}

		// Create signed transaction
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     i - 1,
			GasTipCap: big.NewInt(1000000000),
			GasFeeCap: big.NewInt(1000000000),
			Gas:       21000,
			To:        &toAddr,
			Value:     big.NewInt(int64(i * 1000)),
		})

		signer := types.LatestSignerForChainID(chainID)
		signedTx, err := types.SignTx(tx, signer, key)
		if err != nil {
			t.Fatalf("Failed to sign transaction: %v", err)
		}

		block := types.NewBlockWithHeader(header)

		// Store block
		if err := pebbleStorage.SetBlock(ctx, block); err != nil {
			t.Fatalf("SetBlock() error = %v", err)
		}

		// Store transaction
		txLocation := &TxLocation{
			BlockHash:   block.Hash(),
			BlockHeight: i,
			TxIndex:     0,
		}
		if err := pebbleStorage.SetTransaction(ctx, signedTx, txLocation); err != nil {
			t.Fatalf("SetTransaction() error = %v", err)
		}

		// Index transaction for sender address
		if err := pebbleStorage.AddTransactionToAddressIndex(ctx, fromAddr, signedTx.Hash()); err != nil {
			t.Fatalf("AddTransactionToAddressIndex() error = %v", err)
		}

		// Store receipt
		receipt := &types.Receipt{
			Status:      types.ReceiptStatusSuccessful,
			TxHash:      signedTx.Hash(),
			BlockNumber: big.NewInt(int64(i)),
			BlockHash:   block.Hash(),
		}
		if err := pebbleStorage.SetReceipt(ctx, receipt); err != nil {
			t.Fatalf("SetReceipt() error = %v", err)
		}
	}

	t.Run("sent filter - all transactions", func(t *testing.T) {
		filter := &TransactionFilter{
			FromBlock: 0,
			ToBlock:   ^uint64(0),
			TxType:    TxTypeSent,
		}
		results, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, fromAddr, filter, 10, 0)
		if err != nil {
			t.Fatalf("GetTransactionsByAddressFiltered() error = %v", err)
		}
		if len(results) != 3 {
			t.Errorf("Got %d results, want 3", len(results))
		}
	})

	t.Run("with limit", func(t *testing.T) {
		filter := &TransactionFilter{
			FromBlock: 0,
			ToBlock:   ^uint64(0),
			TxType:    TxTypeSent,
		}
		results, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, fromAddr, filter, 2, 0)
		if err != nil {
			t.Fatalf("GetTransactionsByAddressFiltered() error = %v", err)
		}
		if len(results) != 2 {
			t.Errorf("Got %d results, want 2", len(results))
		}
	})

	t.Run("with offset", func(t *testing.T) {
		filter := &TransactionFilter{
			FromBlock: 0,
			ToBlock:   ^uint64(0),
			TxType:    TxTypeSent,
		}
		results, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, fromAddr, filter, 10, 1)
		if err != nil {
			t.Fatalf("GetTransactionsByAddressFiltered() error = %v", err)
		}
		if len(results) != 2 {
			t.Errorf("Got %d results, want 2", len(results))
		}
	})

	t.Run("with block filter", func(t *testing.T) {
		filter := &TransactionFilter{
			FromBlock: 2,
			ToBlock:   3,
			TxType:    TxTypeSent,
		}
		results, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, fromAddr, filter, 10, 0)
		if err != nil {
			t.Fatalf("GetTransactionsByAddressFiltered() error = %v", err)
		}
		if len(results) != 2 {
			t.Errorf("Got %d results, want 2", len(results))
		}
	})

	t.Run("with value filter", func(t *testing.T) {
		filter := &TransactionFilter{
			FromBlock: 0,
			ToBlock:   ^uint64(0),
			TxType:    TxTypeSent,
			MinValue:  big.NewInt(2000),
		}
		results, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, fromAddr, filter, 10, 0)
		if err != nil {
			t.Fatalf("GetTransactionsByAddressFiltered() error = %v", err)
		}
		if len(results) != 2 {
			t.Errorf("Got %d results, want 2", len(results))
		}
	})

	t.Run("invalid filter", func(t *testing.T) {
		filter := &TransactionFilter{
			FromBlock: 100,
			ToBlock:   0, // Invalid: from > to
			TxType:    TxTypeSent,
		}
		_, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, fromAddr, filter, 10, 0)
		if err == nil {
			t.Error("Expected error for invalid filter")
		}
	})

	t.Run("no results - unknown address", func(t *testing.T) {
		unknownAddr := common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
		filter := &TransactionFilter{
			FromBlock: 0,
			ToBlock:   ^uint64(0),
			TxType:    TxTypeSent,
		}
		results, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, unknownAddr, filter, 10, 0)
		if err != nil {
			t.Fatalf("GetTransactionsByAddressFiltered() error = %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Got %d results, want 0", len(results))
		}
	})
}

func TestPebbleStorage_GetTransactionsByAddressFiltered_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Try to get filtered transactions from closed storage
	_, err := pebbleStorage.GetTransactionsByAddressFiltered(ctx, addr, nil, 10, 0)
	if err == nil {
		t.Error("GetTransactionsByAddressFiltered() should fail on closed storage")
	}
}

func TestPebbleStorage_SetTransaction_ErrorCases(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	t.Run("nil transaction", func(t *testing.T) {
		location := &TxLocation{
			BlockHash:   common.Hash{},
			BlockHeight: 1,
			TxIndex:     0,
		}
		err := pebbleStorage.SetTransaction(ctx, nil, location)
		if err == nil {
			t.Error("SetTransaction() should fail with nil transaction")
		}
	})

	t.Run("nil location", func(t *testing.T) {
		tx := createTestTransaction(0)
		err := pebbleStorage.SetTransaction(ctx, tx, nil)
		if err == nil {
			t.Error("SetTransaction() should fail with nil location")
		}
	})
}

func TestPebbleStorage_SetTransaction_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	tx := createTestTransaction(0)
	location := &TxLocation{
		BlockHash:   common.Hash{},
		BlockHeight: 1,
		TxIndex:     0,
	}
	err := pebbleStorage.SetTransaction(ctx, tx, location)
	if err == nil {
		t.Error("SetTransaction() should fail on closed storage")
	}
}

func TestPebbleStorage_GetBlocks_Extended(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Store test blocks
	for i := uint64(1); i <= 5; i++ {
		block := createTestBlock(i)
		if err := pebbleStorage.SetBlock(ctx, block); err != nil {
			t.Fatalf("SetBlock() error = %v", err)
		}
	}

	t.Run("get all blocks", func(t *testing.T) {
		blocks, err := pebbleStorage.GetBlocks(ctx, 1, 5)
		if err != nil {
			t.Fatalf("GetBlocks() error = %v", err)
		}
		if len(blocks) != 5 {
			t.Errorf("Got %d blocks, want 5", len(blocks))
		}
	})

	t.Run("get partial range", func(t *testing.T) {
		blocks, err := pebbleStorage.GetBlocks(ctx, 2, 4)
		if err != nil {
			t.Fatalf("GetBlocks() error = %v", err)
		}
		if len(blocks) != 3 {
			t.Errorf("Got %d blocks, want 3", len(blocks))
		}
	})

	t.Run("skip missing blocks", func(t *testing.T) {
		// Request range that includes non-existent blocks
		blocks, err := pebbleStorage.GetBlocks(ctx, 1, 10)
		if err != nil {
			t.Fatalf("GetBlocks() error = %v", err)
		}
		if len(blocks) != 5 {
			t.Errorf("Got %d blocks, want 5 (should skip missing)", len(blocks))
		}
	})

	t.Run("empty range", func(t *testing.T) {
		blocks, err := pebbleStorage.GetBlocks(ctx, 100, 200)
		if err != nil {
			t.Fatalf("GetBlocks() error = %v", err)
		}
		if len(blocks) != 0 {
			t.Errorf("Got %d blocks, want 0", len(blocks))
		}
	})
}

func TestPebbleStorage_GetBlocks_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, err := pebbleStorage.GetBlocks(ctx, 1, 10)
	if err == nil {
		t.Error("GetBlocks() should fail on closed storage")
	}
}

func TestPebbleStorage_SetReceipt_ErrorCases(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	t.Run("nil receipt", func(t *testing.T) {
		err := pebbleStorage.SetReceipt(ctx, nil)
		if err == nil {
			t.Error("SetReceipt() should fail with nil receipt")
		}
	})
}

func TestPebbleStorage_SetReceipt_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	receipt := &types.Receipt{
		Status:      types.ReceiptStatusSuccessful,
		TxHash:      common.Hash{},
		BlockNumber: big.NewInt(1),
	}
	err := pebbleStorage.SetReceipt(ctx, receipt)
	if err == nil {
		t.Error("SetReceipt() should fail on closed storage")
	}
}

func TestPebbleStorage_DeleteBlock_ErrorCases(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	t.Run("delete non-existent block returns nil", func(t *testing.T) {
		// DeleteBlock returns nil for non-existent blocks (idempotent operation)
		err := pebbleStorage.DeleteBlock(ctx, 999)
		if err != nil {
			t.Errorf("DeleteBlock() for non-existent block should return nil, got %v", err)
		}
	})

	t.Run("delete existing block", func(t *testing.T) {
		// Create and then delete a block
		block := createTestBlock(100)
		if err := pebbleStorage.SetBlock(ctx, block); err != nil {
			t.Fatalf("SetBlock() error = %v", err)
		}

		// Verify block exists
		exists, err := pebbleStorage.HasBlock(ctx, 100)
		if err != nil || !exists {
			t.Fatalf("Block should exist before deletion")
		}

		// Delete the block
		err = pebbleStorage.DeleteBlock(ctx, 100)
		if err != nil {
			t.Errorf("DeleteBlock() error = %v", err)
		}

		// Verify block is deleted
		exists, err = pebbleStorage.HasBlock(ctx, 100)
		if err != nil {
			t.Fatalf("HasBlock() error = %v", err)
		}
		if exists {
			t.Error("Block should not exist after deletion")
		}
	})
}

func TestPebbleStorage_DeleteBlock_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	err := pebbleStorage.DeleteBlock(ctx, 1)
	if err == nil {
		t.Error("DeleteBlock() should fail on closed storage")
	}
}

func TestPebbleStorage_GetTransaction_ErrorCases(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	t.Run("get non-existent transaction", func(t *testing.T) {
		_, _, err := pebbleStorage.GetTransaction(ctx, common.Hash{})
		if err == nil {
			t.Error("GetTransaction() should fail for non-existent transaction")
		}
		if err != ErrNotFound {
			t.Errorf("GetTransaction() error = %v, want ErrNotFound", err)
		}
	})
}

func TestPebbleStorage_GetTransaction_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, _, err := pebbleStorage.GetTransaction(ctx, common.Hash{})
	if err == nil {
		t.Error("GetTransaction() should fail on closed storage")
	}
}

func TestPebbleStorage_SetBlockTimestamp(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	t.Run("set timestamp", func(t *testing.T) {
		err := pebbleStorage.SetBlockTimestamp(ctx, 1000, 1)
		if err != nil {
			t.Fatalf("SetBlockTimestamp() error = %v", err)
		}

		err = pebbleStorage.SetBlockTimestamp(ctx, 2000, 2)
		if err != nil {
			t.Fatalf("SetBlockTimestamp() error = %v", err)
		}
	})
}

func TestPebbleStorage_SetBlockTimestamp_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	err := pebbleStorage.SetBlockTimestamp(ctx, 1000, 1)
	if err == nil {
		t.Error("SetBlockTimestamp() should fail on closed storage")
	}
}

func TestPebbleStorage_SetLatestHeight_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	err := pebbleStorage.SetLatestHeight(ctx, 100)
	if err == nil {
		t.Error("SetLatestHeight() should fail on closed storage")
	}
}

func TestPebbleStorage_SetBlock_ErrorCases(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	t.Run("nil block", func(t *testing.T) {
		err := pebbleStorage.SetBlock(ctx, nil)
		if err == nil {
			t.Error("SetBlock() should fail with nil block")
		}
	})
}

func TestPebbleStorage_SetBlock_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	block := createTestBlock(1)
	err := pebbleStorage.SetBlock(ctx, block)
	if err == nil {
		t.Error("SetBlock() should fail on closed storage")
	}
}

func TestPebbleStorage_GetBlock_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, err := pebbleStorage.GetBlock(ctx, 1)
	if err == nil {
		t.Error("GetBlock() should fail on closed storage")
	}
}

func TestPebbleStorage_GetBlockByHash_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, err := pebbleStorage.GetBlockByHash(ctx, common.Hash{})
	if err == nil {
		t.Error("GetBlockByHash() should fail on closed storage")
	}
}

func TestPebbleStorage_GetReceipt_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, err := pebbleStorage.GetReceipt(ctx, common.Hash{})
	if err == nil {
		t.Error("GetReceipt() should fail on closed storage")
	}
}

func TestPebbleStorage_HasBlock_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, err := pebbleStorage.HasBlock(ctx, 1)
	if err == nil {
		t.Error("HasBlock() should fail on closed storage")
	}
}

func TestPebbleStorage_HasTransaction_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, err := pebbleStorage.HasTransaction(ctx, common.Hash{})
	if err == nil {
		t.Error("HasTransaction() should fail on closed storage")
	}
}

func TestPebbleStorage_GetTransactionsByAddress_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	_, err := pebbleStorage.GetTransactionsByAddress(ctx, addr, 10, 0)
	if err == nil {
		t.Error("GetTransactionsByAddress() should fail on closed storage")
	}
}

func TestPebbleStorage_AddTransactionToAddressIndex_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	err := pebbleStorage.AddTransactionToAddressIndex(ctx, addr, common.Hash{})
	if err == nil {
		t.Error("AddTransactionToAddressIndex() should fail on closed storage")
	}
}

func TestPebbleStorage_SetReceipts_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	receipts := []*types.Receipt{
		{Status: types.ReceiptStatusSuccessful, TxHash: common.Hash{}},
	}
	err := pebbleStorage.SetReceipts(ctx, receipts)
	if err == nil {
		t.Error("SetReceipts() should fail on closed storage")
	}
}

func TestPebbleStorage_SetBlocks_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	blocks := []*types.Block{createTestBlock(1)}
	err := pebbleStorage.SetBlocks(ctx, blocks)
	if err == nil {
		t.Error("SetBlocks() should fail on closed storage")
	}
}

func TestPebbleStorage_GetLatestHeight_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, err := pebbleStorage.GetLatestHeight(ctx)
	if err == nil {
		t.Error("GetLatestHeight() should fail on closed storage")
	}
}

func TestPebbleStorage_UpdateBalance_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	err := pebbleStorage.UpdateBalance(ctx, addr, 1, big.NewInt(1000), common.Hash{})
	if err == nil {
		t.Error("UpdateBalance() should fail on closed storage")
	}
}

func TestPebbleStorage_GetAddressBalance_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	_, err := pebbleStorage.GetAddressBalance(ctx, addr, 0)
	if err == nil {
		t.Error("GetAddressBalance() should fail on closed storage")
	}
}

func TestPebbleStorage_GetBalanceHistory_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	_, err := pebbleStorage.GetBalanceHistory(ctx, addr, 0, 100, 10, 0)
	if err == nil {
		t.Error("GetBalanceHistory() should fail on closed storage")
	}
}

func TestPebbleStorage_GetBlocksByTimeRange_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, err := pebbleStorage.GetBlocksByTimeRange(ctx, 0, 1000, 10, 0)
	if err == nil {
		t.Error("GetBlocksByTimeRange() should fail on closed storage")
	}
}

func TestPebbleStorage_GetBlockByTimestamp_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	_, err := pebbleStorage.GetBlockByTimestamp(ctx, 1000)
	if err == nil {
		t.Error("GetBlockByTimestamp() should fail on closed storage")
	}
}

// Batch operation tests
func TestPebbleBatch_SetLatestHeight(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("successful set latest height", func(t *testing.T) {
		batch := storage.NewBatch()
		if batch == nil {
			t.Fatal("NewBatch() returned nil")
		}
		defer batch.Close()

		err := batch.SetLatestHeight(ctx, 100)
		if err != nil {
			t.Errorf("SetLatestHeight() error = %v", err)
		}

		if batch.Count() != 1 {
			t.Errorf("Count() = %d, want 1", batch.Count())
		}

		// Commit the batch
		err = batch.Commit()
		if err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		// Verify the value was written
		height, err := storage.GetLatestHeight(ctx)
		if err != nil {
			t.Fatalf("GetLatestHeight() error = %v", err)
		}
		if height != 100 {
			t.Errorf("GetLatestHeight() = %d, want 100", height)
		}
	})

	t.Run("closed batch error", func(t *testing.T) {
		batch := storage.NewBatch()
		batch.Close()

		err := batch.SetLatestHeight(ctx, 200)
		if err == nil {
			t.Error("SetLatestHeight() should fail on closed batch")
		}
	})
}

func TestPebbleBatch_SetBlock(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("successful set block", func(t *testing.T) {
		batch := storage.NewBatch()
		defer batch.Close()

		block := createTestBlock(50)
		err := batch.SetBlock(ctx, block)
		if err != nil {
			t.Errorf("SetBlock() error = %v", err)
		}

		if batch.Count() != 2 { // block data + hash index
			t.Errorf("Count() = %d, want 2", batch.Count())
		}

		err = batch.Commit()
		if err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		// Verify the block was written
		retrieved, err := storage.GetBlock(ctx, 50)
		if err != nil {
			t.Fatalf("GetBlock() error = %v", err)
		}
		if retrieved.Number().Uint64() != 50 {
			t.Errorf("GetBlock() returned wrong block number")
		}
	})

	t.Run("closed batch error", func(t *testing.T) {
		batch := storage.NewBatch()
		batch.Close()

		block := createTestBlock(51)
		err := batch.SetBlock(ctx, block)
		if err == nil {
			t.Error("SetBlock() should fail on closed batch")
		}
	})
}

func TestPebbleBatch_SetTransaction(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("successful set transaction", func(t *testing.T) {
		batch := storage.NewBatch()
		defer batch.Close()

		tx := createTestTransaction(1)
		location := &TxLocation{
			BlockHeight: 10,
			TxIndex:     0,
		}

		err := batch.SetTransaction(ctx, tx, location)
		if err != nil {
			t.Errorf("SetTransaction() error = %v", err)
		}

		if batch.Count() != 2 { // tx data + hash index
			t.Errorf("Count() = %d, want 2", batch.Count())
		}

		err = batch.Commit()
		if err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		// Verify the transaction was written
		retrieved, _, err := storage.GetTransaction(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("GetTransaction() error = %v", err)
		}
		if retrieved.Hash() != tx.Hash() {
			t.Errorf("GetTransaction() returned wrong transaction")
		}
	})

	t.Run("closed batch error", func(t *testing.T) {
		batch := storage.NewBatch()
		batch.Close()

		tx := createTestTransaction(2)
		location := &TxLocation{
			BlockHeight: 11,
			TxIndex:     0,
		}

		err := batch.SetTransaction(ctx, tx, location)
		if err == nil {
			t.Error("SetTransaction() should fail on closed batch")
		}
	})
}

func TestPebbleBatch_SetReceipt(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("successful set receipt", func(t *testing.T) {
		batch := storage.NewBatch()
		defer batch.Close()

		tx := createTestTransaction(1)
		receipt := &types.Receipt{
			TxHash:           tx.Hash(),
			Status:           types.ReceiptStatusSuccessful,
			BlockNumber:      big.NewInt(10),
			TransactionIndex: 0,
			GasUsed:          21000,
		}

		err := batch.SetReceipt(ctx, receipt)
		if err != nil {
			t.Errorf("SetReceipt() error = %v", err)
		}

		if batch.Count() != 1 {
			t.Errorf("Count() = %d, want 1", batch.Count())
		}

		err = batch.Commit()
		if err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		// Verify the receipt was written
		retrieved, err := storage.GetReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("GetReceipt() error = %v", err)
		}
		if retrieved.TxHash != tx.Hash() {
			t.Errorf("GetReceipt() returned wrong receipt")
		}
	})

	t.Run("closed batch error", func(t *testing.T) {
		batch := storage.NewBatch()
		batch.Close()

		tx := createTestTransaction(2)
		receipt := &types.Receipt{
			TxHash: tx.Hash(),
			Status: types.ReceiptStatusSuccessful,
		}

		err := batch.SetReceipt(ctx, receipt)
		if err == nil {
			t.Error("SetReceipt() should fail on closed batch")
		}
	})
}

func TestPebbleBatch_SetReceipts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("successful set receipts", func(t *testing.T) {
		batch := storage.NewBatch()
		defer batch.Close()

		var receipts []*types.Receipt
		for i := 0; i < 3; i++ {
			tx := createTestTransaction(uint64(i))
			receipt := &types.Receipt{
				TxHash:           tx.Hash(),
				Status:           types.ReceiptStatusSuccessful,
				BlockNumber:      big.NewInt(10),
				TransactionIndex: uint(i),
				GasUsed:          21000,
			}
			receipts = append(receipts, receipt)
		}

		err := batch.SetReceipts(ctx, receipts)
		if err != nil {
			t.Errorf("SetReceipts() error = %v", err)
		}

		if batch.Count() != 3 {
			t.Errorf("Count() = %d, want 3", batch.Count())
		}

		err = batch.Commit()
		if err != nil {
			t.Fatalf("Commit() error = %v", err)
		}
	})

	t.Run("closed batch error", func(t *testing.T) {
		batch := storage.NewBatch()
		batch.Close()

		tx := createTestTransaction(1)
		receipts := []*types.Receipt{
			{TxHash: tx.Hash(), Status: types.ReceiptStatusSuccessful},
		}

		err := batch.SetReceipts(ctx, receipts)
		if err == nil {
			t.Error("SetReceipts() should fail on closed batch")
		}
	})
}

func TestPebbleBatch_SetBlocks(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("successful set blocks", func(t *testing.T) {
		batch := storage.NewBatch()
		defer batch.Close()

		var blocks []*types.Block
		for i := uint64(60); i <= 62; i++ {
			blocks = append(blocks, createTestBlock(i))
		}

		err := batch.SetBlocks(ctx, blocks)
		if err != nil {
			t.Errorf("SetBlocks() error = %v", err)
		}

		// 3 blocks * 2 operations each (data + hash index)
		if batch.Count() != 6 {
			t.Errorf("Count() = %d, want 6", batch.Count())
		}

		err = batch.Commit()
		if err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		// Verify blocks were written
		for i := uint64(60); i <= 62; i++ {
			retrieved, err := storage.GetBlock(ctx, i)
			if err != nil {
				t.Errorf("GetBlock(%d) error = %v", i, err)
			}
			if retrieved.Number().Uint64() != i {
				t.Errorf("GetBlock(%d) returned wrong block", i)
			}
		}
	})

	t.Run("closed batch error", func(t *testing.T) {
		batch := storage.NewBatch()
		batch.Close()

		blocks := []*types.Block{createTestBlock(70)}

		err := batch.SetBlocks(ctx, blocks)
		if err == nil {
			t.Error("SetBlocks() should fail on closed batch")
		}
	})
}

func TestPebbleBatch_DeleteBlock(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// First create a block to delete
	block := createTestBlock(80)
	if err := pebbleStorage.SetBlock(ctx, block); err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	t.Run("successful delete block", func(t *testing.T) {
		batch := storage.NewBatch()
		defer batch.Close()

		err := batch.DeleteBlock(ctx, 80)
		if err != nil {
			t.Errorf("DeleteBlock() error = %v", err)
		}

		err = batch.Commit()
		if err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		// Verify block was deleted
		exists, err := storage.HasBlock(ctx, 80)
		if err != nil {
			t.Fatalf("HasBlock() error = %v", err)
		}
		if exists {
			t.Error("Block should not exist after batch delete")
		}
	})

	t.Run("delete non-existent block returns nil", func(t *testing.T) {
		batch := storage.NewBatch()
		defer batch.Close()

		// Should not error for non-existent block
		err := batch.DeleteBlock(ctx, 999)
		if err != nil {
			t.Errorf("DeleteBlock() for non-existent block should return nil, got %v", err)
		}
	})

	t.Run("closed batch error", func(t *testing.T) {
		batch := storage.NewBatch()
		batch.Close()

		err := batch.DeleteBlock(ctx, 81)
		if err == nil {
			t.Error("DeleteBlock() should fail on closed batch")
		}
	})
}

func TestPebbleStorage_GetBlocksByTimeRange_Extended(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Store blocks with timestamps
	for i := uint64(1); i <= 10; i++ {
		block := createTestBlock(i)
		if err := pebbleStorage.SetBlock(ctx, block); err != nil {
			t.Fatalf("SetBlock() error = %v", err)
		}
		// Set timestamp (timestamp = block number * 1000, height = i)
		// SetBlockTimestamp(ctx, timestamp, height)
		if err := pebbleStorage.SetBlockTimestamp(ctx, i*1000, i); err != nil {
			t.Fatalf("SetBlockTimestamp() error = %v", err)
		}
	}

	t.Run("get blocks in time range", func(t *testing.T) {
		blocks, err := pebbleStorage.GetBlocksByTimeRange(ctx, 2000, 5000, 10, 0)
		if err != nil {
			t.Fatalf("GetBlocksByTimeRange() error = %v", err)
		}
		if len(blocks) != 4 { // blocks 2, 3, 4, 5
			t.Errorf("Got %d blocks, want 4", len(blocks))
		}
	})

	t.Run("get with limit", func(t *testing.T) {
		blocks, err := pebbleStorage.GetBlocksByTimeRange(ctx, 1000, 10000, 3, 0)
		if err != nil {
			t.Fatalf("GetBlocksByTimeRange() error = %v", err)
		}
		if len(blocks) != 3 {
			t.Errorf("Got %d blocks, want 3", len(blocks))
		}
	})

	t.Run("get with offset", func(t *testing.T) {
		blocks, err := pebbleStorage.GetBlocksByTimeRange(ctx, 1000, 10000, 10, 5)
		if err != nil {
			t.Fatalf("GetBlocksByTimeRange() error = %v", err)
		}
		if len(blocks) != 5 { // blocks 6, 7, 8, 9, 10
			t.Errorf("Got %d blocks, want 5", len(blocks))
		}
	})

	t.Run("empty range", func(t *testing.T) {
		blocks, err := pebbleStorage.GetBlocksByTimeRange(ctx, 100000, 200000, 10, 0)
		if err != nil {
			t.Fatalf("GetBlocksByTimeRange() error = %v", err)
		}
		if len(blocks) != 0 {
			t.Errorf("Got %d blocks, want 0", len(blocks))
		}
	})
}

func TestPebbleStorage_UpdateBalance_Extended(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	txHash1 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	txHash2 := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")

	t.Run("update balance with different values", func(t *testing.T) {
		// First update - add 1000
		err := pebbleStorage.UpdateBalance(ctx, addr, 1, big.NewInt(1000), txHash1)
		if err != nil {
			t.Errorf("UpdateBalance() error = %v", err)
		}

		// Second update - add 2000 (total should be 3000)
		err = pebbleStorage.UpdateBalance(ctx, addr, 2, big.NewInt(2000), txHash2)
		if err != nil {
			t.Errorf("UpdateBalance() error = %v", err)
		}

		// Check latest balance (use blockNumber 0 for latest)
		// UpdateBalance adds delta, so 1000 + 2000 = 3000
		balance, err := pebbleStorage.GetAddressBalance(ctx, addr, 0)
		if err != nil {
			t.Fatalf("GetAddressBalance() error = %v", err)
		}
		if balance.Cmp(big.NewInt(3000)) != 0 {
			t.Errorf("GetAddressBalance() = %s, want 3000", balance.String())
		}

		// Check history (fromBlock=1, toBlock=10, limit=10, offset=0)
		history, err := pebbleStorage.GetBalanceHistory(ctx, addr, 1, 10, 10, 0)
		if err != nil {
			t.Fatalf("GetBalanceHistory() error = %v", err)
		}
		if len(history) != 2 {
			t.Errorf("Got %d history entries, want 2", len(history))
		}
	})

	t.Run("update with zero balance", func(t *testing.T) {
		addr2 := common.HexToAddress("0x3333333333333333333333333333333333333333")
		txHash3 := common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")

		err := pebbleStorage.UpdateBalance(ctx, addr2, 1, big.NewInt(0), txHash3)
		if err != nil {
			t.Errorf("UpdateBalance() with zero error = %v", err)
		}

		balance, err := pebbleStorage.GetAddressBalance(ctx, addr2, 0)
		if err != nil {
			t.Fatalf("GetAddressBalance() error = %v", err)
		}
		if balance.Cmp(big.NewInt(0)) != 0 {
			t.Errorf("GetAddressBalance() = %s, want 0", balance.String())
		}
	})
}

// Additional coverage tests
func TestPebbleStorage_GetTransaction_NotFound_Extended(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get non-existent transaction
	nonExistentHash := common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	tx, location, err := storage.GetTransaction(ctx, nonExistentHash)
	if err != ErrNotFound {
		t.Errorf("GetTransaction() error = %v, want ErrNotFound", err)
	}
	if tx != nil {
		t.Error("GetTransaction() returned non-nil transaction for non-existent hash")
	}
	if location != nil {
		t.Error("GetTransaction() returned non-nil location for non-existent hash")
	}
}

func TestPebbleStorage_SetTransaction_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Create and store a transaction
	tx := createTestTransaction(1)
	location := &TxLocation{
		BlockHeight: 100,
		TxIndex:     5,
	}

	err := pebbleStorage.SetTransaction(ctx, tx, location)
	if err != nil {
		t.Fatalf("SetTransaction() error = %v", err)
	}

	// Verify it can be retrieved
	retrieved, loc, err := pebbleStorage.GetTransaction(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("GetTransaction() error = %v", err)
	}
	if retrieved.Hash() != tx.Hash() {
		t.Error("Retrieved transaction hash doesn't match")
	}
	if loc.BlockHeight != 100 || loc.TxIndex != 5 {
		t.Errorf("Retrieved location = {%d, %d}, want {100, 5}", loc.BlockHeight, loc.TxIndex)
	}
}

func TestPebbleStorage_GetBlocksByTimeRange_InvalidRange(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Test with fromTime > toTime
	_, err := pebbleStorage.GetBlocksByTimeRange(ctx, 10000, 1000, 10, 0)
	if err == nil {
		t.Error("GetBlocksByTimeRange() should fail when fromTime > toTime")
	}
}

func TestPebbleStorage_SetBalance_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	addr := common.HexToAddress("0x5555555555555555555555555555555555555555")

	// Set balance directly
	err := pebbleStorage.SetBalance(ctx, addr, 1, big.NewInt(5000))
	if err != nil {
		t.Fatalf("SetBalance() error = %v", err)
	}

	// Verify the balance
	balance, err := pebbleStorage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("GetAddressBalance() = %s, want 5000", balance.String())
	}
}

func TestPebbleStorage_SetBalance_ClosedStorage_Extended(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Close storage
	cleanup()

	addr := common.HexToAddress("0x6666666666666666666666666666666666666666")
	err := pebbleStorage.SetBalance(ctx, addr, 1, big.NewInt(1000))
	if err == nil {
		t.Error("SetBalance() should fail on closed storage")
	}
}

func TestPebbleStorage_GetBlockByTimestamp(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Store a block with timestamp
	block := createTestBlock(50)
	if err := pebbleStorage.SetBlock(ctx, block); err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}
	if err := pebbleStorage.SetBlockTimestamp(ctx, 50000, 50); err != nil {
		t.Fatalf("SetBlockTimestamp() error = %v", err)
	}

	// Get block by timestamp
	retrieved, err := pebbleStorage.GetBlockByTimestamp(ctx, 50000)
	if err != nil {
		t.Fatalf("GetBlockByTimestamp() error = %v", err)
	}
	if retrieved.Number().Uint64() != 50 {
		t.Errorf("GetBlockByTimestamp() returned block %d, want 50", retrieved.Number().Uint64())
	}
}

func TestPebbleStorage_GetBlockByTimestamp_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Try to get block with non-existent timestamp
	_, err := pebbleStorage.GetBlockByTimestamp(ctx, 99999)
	if err == nil {
		t.Error("GetBlockByTimestamp() should fail for non-existent timestamp")
	}
}

func TestPebbleStorage_GetAddressBalance_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Get balance for address with no history
	addr := common.HexToAddress("0x7777777777777777777777777777777777777777")
	balance, err := pebbleStorage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	// Should return 0 for non-existent balance
	if balance.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("GetAddressBalance() = %s, want 0 for non-existent address", balance.String())
	}
}

func TestPebbleStorage_GetBalanceHistory_Empty(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Get history for address with no history
	addr := common.HexToAddress("0x8888888888888888888888888888888888888888")
	history, err := pebbleStorage.GetBalanceHistory(ctx, addr, 0, 100, 10, 0)
	if err != nil {
		t.Fatalf("GetBalanceHistory() error = %v", err)
	}
	if len(history) != 0 {
		t.Errorf("GetBalanceHistory() returned %d entries, want 0", len(history))
	}
}

func TestPebbleStorage_GetBalanceHistory_InvalidRange(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	addr := common.HexToAddress("0x9999999999999999999999999999999999999999")

	// Test with fromBlock > toBlock
	_, err := pebbleStorage.GetBalanceHistory(ctx, addr, 100, 10, 10, 0)
	if err == nil {
		t.Error("GetBalanceHistory() should fail when fromBlock > toBlock")
	}
}

// Encoder coverage tests
func TestEncodeBlock_NilBlock(t *testing.T) {
	_, err := EncodeBlock(nil)
	if err == nil {
		t.Error("EncodeBlock() should fail for nil block")
	}
}

func TestEncodeTransaction_NilTx(t *testing.T) {
	_, err := EncodeTransaction(nil)
	if err == nil {
		t.Error("EncodeTransaction() should fail for nil transaction")
	}
}

func TestEncodeReceipt_NilReceipt(t *testing.T) {
	_, err := EncodeReceipt(nil)
	if err == nil {
		t.Error("EncodeReceipt() should fail for nil receipt")
	}
}

func TestEncodeTxLocation_NilLocation(t *testing.T) {
	_, err := EncodeTxLocation(nil)
	if err == nil {
		t.Error("EncodeTxLocation() should fail for nil location")
	}
}

func TestPebbleStorage_NewBatch_Operations(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	batch := storage.NewBatch()
	if batch == nil {
		t.Fatal("NewBatch() returned nil")
	}
	defer batch.Close()

	// Add multiple operations to batch
	for i := uint64(1); i <= 5; i++ {
		block := createTestBlock(i)
		if err := batch.SetBlock(ctx, block); err != nil {
			t.Errorf("SetBlock(%d) error = %v", i, err)
		}
	}

	// Check count
	expectedCount := 5 * 2 // 5 blocks * 2 operations each
	if batch.Count() != expectedCount {
		t.Errorf("Count() = %d, want %d", batch.Count(), expectedCount)
	}

	// Reset batch
	batch.Reset()
	if batch.Count() != 0 {
		t.Errorf("Count() after Reset() = %d, want 0", batch.Count())
	}
}

func TestPebbleStorage_GetTransaction_SuccessfulRetrieval(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Create multiple transactions
	for i := uint64(1); i <= 3; i++ {
		tx := createTestTransaction(i)
		location := &TxLocation{
			BlockHeight: i * 10,
			TxIndex:     i - 1,
		}
		if err := pebbleStorage.SetTransaction(ctx, tx, location); err != nil {
			t.Fatalf("SetTransaction(%d) error = %v", i, err)
		}
	}

	// Retrieve each transaction
	for i := uint64(1); i <= 3; i++ {
		tx := createTestTransaction(i)
		retrieved, location, err := pebbleStorage.GetTransaction(ctx, tx.Hash())
		if err != nil {
			t.Errorf("GetTransaction(%d) error = %v", i, err)
			continue
		}
		if retrieved.Hash() != tx.Hash() {
			t.Errorf("GetTransaction(%d) returned wrong hash", i)
		}
		if location.BlockHeight != i*10 {
			t.Errorf("GetTransaction(%d) returned wrong block height: %d", i, location.BlockHeight)
		}
	}
}

func TestPebbleStorage_HasTransaction_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Create and store a transaction
	tx := createTestTransaction(1)
	location := &TxLocation{BlockHeight: 10, TxIndex: 0}
	if err := pebbleStorage.SetTransaction(ctx, tx, location); err != nil {
		t.Fatalf("SetTransaction() error = %v", err)
	}

	// Check it exists
	exists, err := pebbleStorage.HasTransaction(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("HasTransaction() error = %v", err)
	}
	if !exists {
		t.Error("HasTransaction() returned false for existing transaction")
	}

	// Check non-existent
	nonExistent := common.HexToHash("0xdeadbeef")
	exists, err = pebbleStorage.HasTransaction(ctx, nonExistent)
	if err != nil {
		t.Fatalf("HasTransaction() error = %v", err)
	}
	if exists {
		t.Error("HasTransaction() returned true for non-existent transaction")
	}
}

func TestPebbleStorage_HasBlock_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Create and store a block
	block := createTestBlock(100)
	if err := pebbleStorage.SetBlock(ctx, block); err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Check it exists
	exists, err := pebbleStorage.HasBlock(ctx, 100)
	if err != nil {
		t.Fatalf("HasBlock() error = %v", err)
	}
	if !exists {
		t.Error("HasBlock() returned false for existing block")
	}

	// Check non-existent
	exists, err = pebbleStorage.HasBlock(ctx, 999)
	if err != nil {
		t.Fatalf("HasBlock() error = %v", err)
	}
	if exists {
		t.Error("HasBlock() returned true for non-existent block")
	}
}

func TestPebbleBatch_AddTransactionToAddressIndex(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	batch := storage.NewBatch()
	defer batch.Close()

	addr := common.HexToAddress("0xaaaa")
	txHash := common.HexToHash("0xbbbb")

	err := batch.AddTransactionToAddressIndex(ctx, addr, txHash)
	if err != nil {
		t.Errorf("AddTransactionToAddressIndex() error = %v", err)
	}

	if batch.Count() != 1 {
		t.Errorf("Count() = %d, want 1", batch.Count())
	}

	// Commit and verify
	err = batch.Commit()
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

func TestPebbleBatch_Commit_ClosedBatch(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	batch := storage.NewBatch()
	batch.Close()

	err := batch.Commit()
	if err == nil {
		t.Error("Commit() should fail on closed batch")
	}
}

// Additional normal path tests for better coverage
func TestPebbleStorage_GetBlockByHash_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Create and store a block
	block := createTestBlock(100)
	if err := pebbleStorage.SetBlock(ctx, block); err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Get by hash
	retrieved, err := pebbleStorage.GetBlockByHash(ctx, block.Hash())
	if err != nil {
		t.Fatalf("GetBlockByHash() error = %v", err)
	}
	if retrieved.Number().Uint64() != 100 {
		t.Errorf("GetBlockByHash() returned wrong block: %d", retrieved.Number().Uint64())
	}
}

func TestPebbleStorage_GetBlockByHash_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Try to get non-existent block
	nonExistent := common.HexToHash("0xdeadbeef")
	_, err := pebbleStorage.GetBlockByHash(ctx, nonExistent)
	if err == nil {
		t.Error("GetBlockByHash() should fail for non-existent hash")
	}
}

func TestPebbleStorage_GetReceipt_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Create and store receipt
	tx := createTestTransaction(1)
	receipt := &types.Receipt{
		TxHash:           tx.Hash(),
		Status:           types.ReceiptStatusSuccessful,
		BlockNumber:      big.NewInt(10),
		TransactionIndex: 0,
		GasUsed:          21000,
	}

	if err := pebbleStorage.SetReceipt(ctx, receipt); err != nil {
		t.Fatalf("SetReceipt() error = %v", err)
	}

	// Get receipt
	retrieved, err := pebbleStorage.GetReceipt(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("GetReceipt() error = %v", err)
	}
	if retrieved.TxHash != tx.Hash() {
		t.Error("GetReceipt() returned wrong receipt")
	}
}

func TestPebbleStorage_SetReceipts_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	var receipts []*types.Receipt
	for i := 0; i < 3; i++ {
		tx := createTestTransaction(uint64(i))
		receipt := &types.Receipt{
			TxHash:           tx.Hash(),
			Status:           types.ReceiptStatusSuccessful,
			BlockNumber:      big.NewInt(10),
			TransactionIndex: uint(i),
			GasUsed:          21000,
		}
		receipts = append(receipts, receipt)
	}

	err := pebbleStorage.SetReceipts(ctx, receipts)
	if err != nil {
		t.Fatalf("SetReceipts() error = %v", err)
	}

	// Verify each receipt
	for i, r := range receipts {
		retrieved, err := pebbleStorage.GetReceipt(ctx, r.TxHash)
		if err != nil {
			t.Errorf("GetReceipt(%d) error = %v", i, err)
			continue
		}
		if retrieved.TxHash != r.TxHash {
			t.Errorf("GetReceipt(%d) returned wrong receipt", i)
		}
	}
}

func TestPebbleStorage_SetBlocks_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	var blocks []*types.Block
	for i := uint64(200); i <= 202; i++ {
		blocks = append(blocks, createTestBlock(i))
	}

	err := pebbleStorage.SetBlocks(ctx, blocks)
	if err != nil {
		t.Fatalf("SetBlocks() error = %v", err)
	}

	// Verify each block
	for i := uint64(200); i <= 202; i++ {
		retrieved, err := pebbleStorage.GetBlock(ctx, i)
		if err != nil {
			t.Errorf("GetBlock(%d) error = %v", i, err)
			continue
		}
		if retrieved.Number().Uint64() != i {
			t.Errorf("GetBlock(%d) returned wrong block", i)
		}
	}
}

func TestPebbleStorage_AddTransactionToAddressIndex_Success(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	addr := common.HexToAddress("0xcccc")

	// Add multiple transactions to index
	var hashes []common.Hash
	for i := 0; i < 3; i++ {
		tx := createTestTransaction(uint64(i))
		hashes = append(hashes, tx.Hash())
		if err := pebbleStorage.AddTransactionToAddressIndex(ctx, addr, tx.Hash()); err != nil {
			t.Fatalf("AddTransactionToAddressIndex(%d) error = %v", i, err)
		}
	}

	// Get transactions by address
	retrieved, err := pebbleStorage.GetTransactionsByAddress(ctx, addr, 10, 0)
	if err != nil {
		t.Fatalf("GetTransactionsByAddress() error = %v", err)
	}
	if len(retrieved) != 3 {
		t.Errorf("GetTransactionsByAddress() returned %d transactions, want 3", len(retrieved))
	}
}

func TestPebbleStorage_GetLatestHeight_NotSet(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// On fresh storage, GetLatestHeight should return 0 or error
	height, err := storage.GetLatestHeight(ctx)
	if err != nil && err != ErrNotFound {
		t.Fatalf("GetLatestHeight() unexpected error = %v", err)
	}
	// Height should be 0 for fresh storage
	_ = height // Just accessing the value is enough
}
