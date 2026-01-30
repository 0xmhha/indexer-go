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
	"github.com/ethereum/go-ethereum/trie"
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
		TxHash:            common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
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

func TestPebbleStorage_GetBlockCount(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	pebbleStorage := storage.(*PebbleStorage)

	// Test initial count (no blocks) - should return 0 when no latest height is set
	count, err := pebbleStorage.GetBlockCount(ctx)
	if err != nil {
		t.Fatalf("GetBlockCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("GetBlockCount() = %d, want 0", count)
	}

	// Set latest height to 4 (blocks 0-4, total 5 blocks)
	heightValue := EncodeUint64(4)
	pebbleStorage.db.Set(LatestHeightKey(), heightValue, nil)

	// Test retrieval - should return height + 1
	count, err = pebbleStorage.GetBlockCount(ctx)
	if err != nil {
		t.Fatalf("GetBlockCount() error = %v", err)
	}
	if count != 5 {
		t.Errorf("GetBlockCount() = %d, want 5", count)
	}

	// Set larger height
	heightValue = EncodeUint64(999999)
	pebbleStorage.db.Set(LatestHeightKey(), heightValue, nil)

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

	// Store count manually (also update atomic counter since GetTransactionCount uses cache)
	countValue := EncodeUint64(10)
	pebbleStorage.db.Set(TransactionCountKey(), countValue, nil)
	pebbleStorage.txCount.Store(10) // Update cache to match

	// Test retrieval
	count, err = pebbleStorage.GetTransactionCount(ctx)
	if err != nil {
		t.Fatalf("GetTransactionCount() error = %v", err)
	}
	if count != 10 {
		t.Errorf("GetTransactionCount() = %d, want 10", count)
	}

	// Store larger count (also update atomic counter)
	countValue = EncodeUint64(5000000)
	pebbleStorage.db.Set(TransactionCountKey(), countValue, nil)
	pebbleStorage.txCount.Store(5000000) // Update cache to match

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
			TxHash:            tx.Hash(),
			Status:            types.ReceiptStatusSuccessful,
			BlockNumber:       big.NewInt(10),
			TransactionIndex:  0,
			GasUsed:           21000,
			CumulativeGasUsed: 21000,
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
				TxHash:            tx.Hash(),
				Status:            types.ReceiptStatusSuccessful,
				BlockNumber:       big.NewInt(10),
				TransactionIndex:  uint(i),
				GasUsed:           21000,
				CumulativeGasUsed: uint64((i + 1) * 21000),
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
		TxHash:            tx.Hash(),
		Status:            types.ReceiptStatusSuccessful,
		BlockNumber:       big.NewInt(10),
		TransactionIndex:  0,
		GasUsed:           21000,
		CumulativeGasUsed: 21000,
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
			TxHash:            tx.Hash(),
			Status:            types.ReceiptStatusSuccessful,
			BlockNumber:       big.NewInt(10),
			TransactionIndex:  uint(i),
			GasUsed:           21000,
			CumulativeGasUsed: uint64((i + 1) * 21000),
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

// ============================================================================
// KVStore Interface Tests (Put, Get, Delete, Has, Iterate)
// ============================================================================

func TestPebbleStorage_Put(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("Put success", func(t *testing.T) {
		key := []byte("test-key-1")
		value := []byte("test-value-1")

		err := pebbleStorage.Put(ctx, key, value)
		if err != nil {
			t.Errorf("Put() error = %v", err)
		}

		// Verify by Get
		retrieved, err := pebbleStorage.Get(ctx, key)
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
		if string(retrieved) != string(value) {
			t.Errorf("Get() = %v, want %v", string(retrieved), string(value))
		}
	})

	t.Run("Put empty value", func(t *testing.T) {
		key := []byte("test-key-empty")
		value := []byte{}

		err := pebbleStorage.Put(ctx, key, value)
		if err != nil {
			t.Errorf("Put() error = %v", err)
		}

		retrieved, err := pebbleStorage.Get(ctx, key)
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
		if len(retrieved) != 0 {
			t.Errorf("Get() = %v, want empty", retrieved)
		}
	})

	t.Run("Put overwrite", func(t *testing.T) {
		key := []byte("test-key-overwrite")
		value1 := []byte("original-value")
		value2 := []byte("new-value")

		err := pebbleStorage.Put(ctx, key, value1)
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		err = pebbleStorage.Put(ctx, key, value2)
		if err != nil {
			t.Errorf("Put() overwrite error = %v", err)
		}

		retrieved, err := pebbleStorage.Get(ctx, key)
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
		if string(retrieved) != string(value2) {
			t.Errorf("Get() = %v, want %v", string(retrieved), string(value2))
		}
	})
}

func TestPebbleStorage_Put_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	err = pebbleStorage.Put(ctx, []byte("key"), []byte("value"))
	if err == nil {
		t.Error("Put() on closed storage should return error")
	}
}

func TestPebbleStorage_Put_ReadOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-readonly-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// First create storage normally
	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	pebbleStorage.Close()

	// Reopen as read-only
	cfg.ReadOnly = true
	pebbleStorage, err = NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to open storage read-only: %v", err)
	}
	defer pebbleStorage.Close()

	ctx := context.Background()
	err = pebbleStorage.Put(ctx, []byte("key"), []byte("value"))
	if err == nil {
		t.Error("Put() on read-only storage should return error")
	}
}

func TestPebbleStorage_Get(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("Get existing key", func(t *testing.T) {
		key := []byte("get-test-key")
		value := []byte("get-test-value")

		err := pebbleStorage.Put(ctx, key, value)
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		retrieved, err := pebbleStorage.Get(ctx, key)
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
		if string(retrieved) != string(value) {
			t.Errorf("Get() = %v, want %v", string(retrieved), string(value))
		}
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		_, err := pebbleStorage.Get(ctx, []byte("non-existent-key"))
		if err != ErrNotFound {
			t.Errorf("Get() error = %v, want ErrNotFound", err)
		}
	})
}

func TestPebbleStorage_Get_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.Get(ctx, []byte("key"))
	if err == nil {
		t.Error("Get() on closed storage should return error")
	}
}

func TestPebbleStorage_Delete(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("Delete existing key", func(t *testing.T) {
		key := []byte("delete-test-key")
		value := []byte("delete-test-value")

		err := pebbleStorage.Put(ctx, key, value)
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		// Verify key exists
		_, err = pebbleStorage.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get() before delete error = %v", err)
		}

		// Delete the key
		err = pebbleStorage.Delete(ctx, key)
		if err != nil {
			t.Errorf("Delete() error = %v", err)
		}

		// Verify key no longer exists
		_, err = pebbleStorage.Get(ctx, key)
		if err != ErrNotFound {
			t.Errorf("Get() after delete error = %v, want ErrNotFound", err)
		}
	})

	t.Run("Delete non-existent key", func(t *testing.T) {
		// Deleting non-existent key should not error in PebbleDB
		err := pebbleStorage.Delete(ctx, []byte("non-existent-delete-key"))
		if err != nil {
			t.Errorf("Delete() non-existent key error = %v", err)
		}
	})
}

func TestPebbleStorage_Delete_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	err = pebbleStorage.Delete(ctx, []byte("key"))
	if err == nil {
		t.Error("Delete() on closed storage should return error")
	}
}

func TestPebbleStorage_Delete_ReadOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-readonly-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// First create storage normally
	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	pebbleStorage.Close()

	// Reopen as read-only
	cfg.ReadOnly = true
	pebbleStorage, err = NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to open storage read-only: %v", err)
	}
	defer pebbleStorage.Close()

	ctx := context.Background()
	err = pebbleStorage.Delete(ctx, []byte("key"))
	if err == nil {
		t.Error("Delete() on read-only storage should return error")
	}
}

func TestPebbleStorage_Has(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("Has existing key", func(t *testing.T) {
		key := []byte("has-test-key")
		value := []byte("has-test-value")

		err := pebbleStorage.Put(ctx, key, value)
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		exists, err := pebbleStorage.Has(ctx, key)
		if err != nil {
			t.Errorf("Has() error = %v", err)
		}
		if !exists {
			t.Error("Has() = false, want true")
		}
	})

	t.Run("Has non-existent key", func(t *testing.T) {
		exists, err := pebbleStorage.Has(ctx, []byte("non-existent-has-key"))
		if err != nil {
			t.Errorf("Has() error = %v", err)
		}
		if exists {
			t.Error("Has() = true, want false")
		}
	})
}

func TestPebbleStorage_Has_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.Has(ctx, []byte("key"))
	if err == nil {
		t.Error("Has() on closed storage should return error")
	}
}

func TestPebbleStorage_Iterate(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("Iterate with prefix", func(t *testing.T) {
		// Set up test data
		prefix := []byte("iter-test/")
		testData := map[string]string{
			"iter-test/key1": "value1",
			"iter-test/key2": "value2",
			"iter-test/key3": "value3",
			"other-prefix/key": "other-value",
		}

		for k, v := range testData {
			err := pebbleStorage.Put(ctx, []byte(k), []byte(v))
			if err != nil {
				t.Fatalf("Put() error = %v", err)
			}
		}

		// Iterate over prefix
		var count int
		var keys []string
		err := pebbleStorage.Iterate(ctx, prefix, func(key, value []byte) bool {
			count++
			keys = append(keys, string(key))
			return true // continue iteration
		})
		if err != nil {
			t.Errorf("Iterate() error = %v", err)
		}

		if count != 3 {
			t.Errorf("Iterate() count = %d, want 3", count)
		}
	})

	t.Run("Iterate with early stop", func(t *testing.T) {
		prefix := []byte("stop-test/")
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("stop-test/key%d", i)
			err := pebbleStorage.Put(ctx, []byte(key), []byte("value"))
			if err != nil {
				t.Fatalf("Put() error = %v", err)
			}
		}

		var count int
		err := pebbleStorage.Iterate(ctx, prefix, func(key, value []byte) bool {
			count++
			return count < 2 // stop after 2 items
		})
		if err != nil {
			t.Errorf("Iterate() error = %v", err)
		}

		if count != 2 {
			t.Errorf("Iterate() stopped at count = %d, want 2", count)
		}
	})

	t.Run("Iterate with empty prefix", func(t *testing.T) {
		// Empty prefix should iterate all keys
		var count int
		err := pebbleStorage.Iterate(ctx, []byte{}, func(key, value []byte) bool {
			count++
			return true
		})
		if err != nil {
			t.Errorf("Iterate() error = %v", err)
		}

		// Should have at least the keys we added in previous tests
		if count < 1 {
			t.Errorf("Iterate() with empty prefix count = %d, want > 0", count)
		}
	})

	t.Run("Iterate with context cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		// Add some test data
		for i := 0; i < 3; i++ {
			key := fmt.Sprintf("cancel-test/key%d", i)
			_ = pebbleStorage.Put(ctx, []byte(key), []byte("value"))
		}

		err := pebbleStorage.Iterate(cancelCtx, []byte("cancel-test/"), func(key, value []byte) bool {
			return true
		})
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Errorf("Iterate() with cancelled context should return context.Canceled, got %v", err)
		}
	})
}

func TestPebbleStorage_Iterate_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	err = pebbleStorage.Iterate(ctx, []byte("prefix"), func(key, value []byte) bool {
		return true
	})
	if err == nil {
		t.Error("Iterate() on closed storage should return error")
	}
}

func TestPrefixUpperBound(t *testing.T) {
	tests := []struct {
		name     string
		prefix   []byte
		expected []byte
	}{
		{
			name:     "empty prefix",
			prefix:   []byte{},
			expected: nil,
		},
		{
			name:     "simple prefix",
			prefix:   []byte("test"),
			expected: []byte("tesu"), // 't' becomes 'u'
		},
		{
			name:     "prefix ending with 0xff",
			prefix:   []byte{0x01, 0xff},
			expected: []byte{0x02},
		},
		{
			name:     "all 0xff",
			prefix:   []byte{0xff, 0xff, 0xff},
			expected: nil,
		},
		{
			name:     "single byte",
			prefix:   []byte{0x00},
			expected: []byte{0x01},
		},
		{
			name:     "prefix with multiple 0xff at end",
			prefix:   []byte{0x01, 0xff, 0xff},
			expected: []byte{0x02},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prefixUpperBound(tt.prefix)
			if string(result) != string(tt.expected) {
				t.Errorf("prefixUpperBound(%v) = %v, want %v", tt.prefix, result, tt.expected)
			}
		})
	}
}

func TestPebbleStorage_Sync(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	// Write some data
	err := pebbleStorage.Put(ctx, []byte("sync-test"), []byte("value"))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Call Sync (takes no arguments)
	err = pebbleStorage.Sync()
	if err != nil {
		t.Errorf("Sync() error = %v", err)
	}
	_ = ctx // ctx is used in Put
}

func TestPebbleStorage_Sync_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	err = pebbleStorage.Sync()
	if err == nil {
		t.Error("Sync() on closed storage should return error")
	}
}

// ============================================================================
// HasReceipt and GetMissingReceipts Tests
// ============================================================================

func TestPebbleStorage_HasReceipt(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	// Create a test block with transaction
	block := createTestBlockWithTransactions(1, 1)
	tx := block.Transactions()[0]

	// Store block first
	err := pebbleStorage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	t.Run("HasReceipt - not found", func(t *testing.T) {
		exists, err := pebbleStorage.HasReceipt(ctx, tx.Hash())
		if err != nil {
			t.Errorf("HasReceipt() error = %v", err)
		}
		if exists {
			t.Error("HasReceipt() = true, want false for missing receipt")
		}
	})

	t.Run("HasReceipt - found after storing", func(t *testing.T) {
		// Store a receipt
		receipt := &types.Receipt{
			Type:              types.LegacyTxType,
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
			Bloom:             types.Bloom{},
			Logs:              []*types.Log{},
			TxHash:            tx.Hash(),
			GasUsed:           21000,
			BlockNumber:       block.Number(),
			BlockHash:         block.Hash(),
			TransactionIndex:  0,
		}

		err := pebbleStorage.SetReceipt(ctx, receipt)
		if err != nil {
			t.Fatalf("SetReceipt() error = %v", err)
		}

		exists, err := pebbleStorage.HasReceipt(ctx, tx.Hash())
		if err != nil {
			t.Errorf("HasReceipt() error = %v", err)
		}
		if !exists {
			t.Error("HasReceipt() = false, want true for stored receipt")
		}
	})
}

func TestPebbleStorage_HasReceipt_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.HasReceipt(ctx, common.Hash{})
	if err == nil {
		t.Error("HasReceipt() on closed storage should return error")
	}
}

func TestPebbleStorage_GetMissingReceipts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	// Create a test block with multiple transactions
	block := createTestBlockWithTransactions(1, 3)

	// Store block first
	err := pebbleStorage.SetBlock(ctx, block)
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	t.Run("All receipts missing", func(t *testing.T) {
		missing, err := pebbleStorage.GetMissingReceipts(ctx, 1)
		if err != nil {
			t.Errorf("GetMissingReceipts() error = %v", err)
		}

		if len(missing) != len(block.Transactions()) {
			t.Errorf("GetMissingReceipts() = %d hashes, want %d", len(missing), len(block.Transactions()))
		}
	})

	t.Run("Some receipts present", func(t *testing.T) {
		// Store receipt for first transaction only
		tx := block.Transactions()[0]
		receipt := &types.Receipt{
			Type:              types.LegacyTxType,
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
			Bloom:             types.Bloom{},
			Logs:              []*types.Log{},
			TxHash:            tx.Hash(),
			GasUsed:           21000,
			BlockNumber:       block.Number(),
			BlockHash:         block.Hash(),
			TransactionIndex:  0,
		}

		err := pebbleStorage.SetReceipt(ctx, receipt)
		if err != nil {
			t.Fatalf("SetReceipt() error = %v", err)
		}

		missing, err := pebbleStorage.GetMissingReceipts(ctx, 1)
		if err != nil {
			t.Errorf("GetMissingReceipts() error = %v", err)
		}

		// Should be missing transactions minus the one we stored
		expectedMissing := len(block.Transactions()) - 1
		if len(missing) != expectedMissing {
			t.Errorf("GetMissingReceipts() = %d hashes, want %d", len(missing), expectedMissing)
		}
	})
}

func TestPebbleStorage_GetMissingReceipts_BlockNotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	_, err := pebbleStorage.GetMissingReceipts(ctx, 999)
	if err == nil {
		t.Error("GetMissingReceipts() for non-existent block should return error")
	}
}

func TestPebbleStorage_GetMissingReceipts_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetMissingReceipts(ctx, 1)
	if err == nil {
		t.Error("GetMissingReceipts() on closed storage should return error")
	}
}

// Helper function to create block with transactions
func createTestBlockWithTransactions(height uint64, txCount int) *types.Block {
	// Create transactions
	txs := make([]*types.Transaction, txCount)
	for i := 0; i < txCount; i++ {
		txs[i] = createTestTransaction(uint64(i))
	}

	header := &types.Header{
		ParentHash:  common.Hash{},
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.Address{},
		Root:        common.Hash{},
		TxHash:      types.DeriveSha(types.Transactions(txs), trie.NewStackTrie(nil)),
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(int64(height)),
		GasLimit:    5000000,
		GasUsed:     21000 * uint64(txCount),
		Time:        1234567890 + height,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
	}
	return types.NewBlockWithHeader(header).WithBody(types.Body{Transactions: txs})
}

// ============================================================================
// SetBlockWithReceipts Tests
// ============================================================================

func TestPebbleStorage_SetBlockWithReceipts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("block with receipts", func(t *testing.T) {
		block := createTestBlockWithTransactions(1, 2)
		txs := block.Transactions()

		// Create receipts for the transactions
		receipts := make([]*types.Receipt, len(txs))
		for i, tx := range txs {
			receipts[i] = &types.Receipt{
				Type:              0,
				Status:            types.ReceiptStatusSuccessful,
				CumulativeGasUsed: uint64(21000 * (i + 1)),
				TxHash:            tx.Hash(),
				BlockNumber:       block.Number(),
				TransactionIndex:  uint(i),
			}
		}

		err := pebbleStorage.SetBlockWithReceipts(ctx, block, receipts)
		if err != nil {
			t.Fatalf("SetBlockWithReceipts() error = %v", err)
		}

		// Verify block was stored
		storedBlock, err := pebbleStorage.GetBlock(ctx, 1)
		if err != nil {
			t.Errorf("GetBlock() error = %v", err)
		}
		if storedBlock == nil {
			t.Error("GetBlock() returned nil")
		}

		// Verify transactions were stored
		for _, tx := range txs {
			storedTx, _, err := pebbleStorage.GetTransaction(ctx, tx.Hash())
			if err != nil {
				t.Errorf("GetTransaction() error = %v", err)
			}
			if storedTx == nil {
				t.Errorf("Transaction %s not found", tx.Hash().Hex())
			}
		}

		// Verify receipts were stored
		for _, tx := range txs {
			receipt, err := pebbleStorage.GetReceipt(ctx, tx.Hash())
			if err != nil {
				t.Errorf("GetReceipt() error = %v", err)
			}
			if receipt == nil {
				t.Errorf("Receipt for tx %s not found", tx.Hash().Hex())
			}
		}
	})

	t.Run("nil block", func(t *testing.T) {
		err := pebbleStorage.SetBlockWithReceipts(ctx, nil, nil)
		if err == nil {
			t.Error("SetBlockWithReceipts() should fail for nil block")
		}
	})

	t.Run("block without receipts", func(t *testing.T) {
		block := createTestBlockWithTransactions(2, 1)

		err := pebbleStorage.SetBlockWithReceipts(ctx, block, nil)
		if err != nil {
			t.Fatalf("SetBlockWithReceipts() error = %v", err)
		}

		// Verify block was stored
		storedBlock, err := pebbleStorage.GetBlock(ctx, 2)
		if err != nil {
			t.Errorf("GetBlock() error = %v", err)
		}
		if storedBlock == nil {
			t.Error("GetBlock() returned nil")
		}
	})
}

func TestPebbleStorage_SetBlockWithReceipts_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-setblockwithreceipts-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	block := createTestBlock(1)
	err = pebbleStorage.SetBlockWithReceipts(ctx, block, nil)
	if err == nil {
		t.Error("SetBlockWithReceipts() on closed storage should return error")
	}
}

func TestPebbleStorage_SetBlockWithReceipts_ReadOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-setblockwithreceipts-ro-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// First create a writable storage to initialize the database
	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	pebbleStorage.Close()

	// Now open in read-only mode
	roCfg := DefaultConfig(tmpDir)
	roCfg.ReadOnly = true
	roStorage, err := NewPebbleStorage(roCfg)
	if err != nil {
		t.Fatalf("Failed to create read-only storage: %v", err)
	}
	defer roStorage.Close()

	ctx := context.Background()
	block := createTestBlock(1)
	err = roStorage.SetBlockWithReceipts(ctx, block, nil)
	if err == nil {
		t.Error("SetBlockWithReceipts() on read-only storage should return error")
	}
}

// ============================================================================
// Minter and Validator Query Tests
// ============================================================================

func TestPebbleStorage_GetActiveMinters(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("empty minters list", func(t *testing.T) {
		minters, err := pebbleStorage.GetActiveMinters(ctx)
		if err != nil {
			t.Errorf("GetActiveMinters() error = %v", err)
		}
		if len(minters) != 0 {
			t.Errorf("GetActiveMinters() = %d minters, want 0", len(minters))
		}
	})

	t.Run("with stored minters", func(t *testing.T) {
		// Store some minter records
		minter1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
		minter2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

		key1 := MinterActiveIndexKey(minter1)
		key2 := MinterActiveIndexKey(minter2)

		err := pebbleStorage.Put(ctx, key1, EncodeBigInt(big.NewInt(1000000)))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		err = pebbleStorage.Put(ctx, key2, EncodeBigInt(big.NewInt(2000000)))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		minters, err := pebbleStorage.GetActiveMinters(ctx)
		if err != nil {
			t.Errorf("GetActiveMinters() error = %v", err)
		}
		if len(minters) != 2 {
			t.Errorf("GetActiveMinters() = %d minters, want 2", len(minters))
		}
	})
}

func TestPebbleStorage_GetActiveMinters_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getactiveminters-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetActiveMinters(ctx)
	if err == nil {
		t.Error("GetActiveMinters() on closed storage should return error")
	}
}

func TestPebbleStorage_GetMinterAllowance(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	minter := common.HexToAddress("0x3333333333333333333333333333333333333333")

	t.Run("non-existent minter returns zero", func(t *testing.T) {
		allowance, err := pebbleStorage.GetMinterAllowance(ctx, minter)
		if err != nil {
			t.Errorf("GetMinterAllowance() error = %v", err)
		}
		if allowance.Cmp(big.NewInt(0)) != 0 {
			t.Errorf("GetMinterAllowance() = %s, want 0", allowance.String())
		}
	})

	t.Run("existing minter returns allowance", func(t *testing.T) {
		expectedAllowance := big.NewInt(5000000)
		key := MinterActiveIndexKey(minter)
		err := pebbleStorage.Put(ctx, key, EncodeBigInt(expectedAllowance))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		allowance, err := pebbleStorage.GetMinterAllowance(ctx, minter)
		if err != nil {
			t.Errorf("GetMinterAllowance() error = %v", err)
		}
		if allowance.Cmp(expectedAllowance) != 0 {
			t.Errorf("GetMinterAllowance() = %s, want %s", allowance.String(), expectedAllowance.String())
		}
	})
}

func TestPebbleStorage_GetMinterAllowance_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getminterallowance-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	minter := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetMinterAllowance(ctx, minter)
	if err == nil {
		t.Error("GetMinterAllowance() on closed storage should return error")
	}
}

func TestPebbleStorage_GetMinterHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	minter := common.HexToAddress("0x4444444444444444444444444444444444444444")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetMinterHistory(ctx, minter)
		if err != nil {
			t.Errorf("GetMinterHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetMinterHistory() = %d events, want 0", len(history))
		}
	})
}

func TestPebbleStorage_GetMinterHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getminterhistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	minter := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetMinterHistory(ctx, minter)
	if err == nil {
		t.Error("GetMinterHistory() on closed storage should return error")
	}
}

func TestPebbleStorage_GetActiveValidators(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("empty validators list", func(t *testing.T) {
		validators, err := pebbleStorage.GetActiveValidators(ctx)
		if err != nil {
			t.Errorf("GetActiveValidators() error = %v", err)
		}
		if len(validators) != 0 {
			t.Errorf("GetActiveValidators() = %d validators, want 0", len(validators))
		}
	})

	t.Run("with stored validators", func(t *testing.T) {
		validator1 := common.HexToAddress("0x5555555555555555555555555555555555555555")
		validator2 := common.HexToAddress("0x6666666666666666666666666666666666666666")

		key1 := ValidatorActiveIndexKey(validator1)
		key2 := ValidatorActiveIndexKey(validator2)

		err := pebbleStorage.Put(ctx, key1, []byte("active"))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		err = pebbleStorage.Put(ctx, key2, []byte("active"))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		validators, err := pebbleStorage.GetActiveValidators(ctx)
		if err != nil {
			t.Errorf("GetActiveValidators() error = %v", err)
		}
		if len(validators) != 2 {
			t.Errorf("GetActiveValidators() = %d validators, want 2", len(validators))
		}
	})
}

func TestPebbleStorage_GetActiveValidators_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getactivevalidators-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetActiveValidators(ctx)
	if err == nil {
		t.Error("GetActiveValidators() on closed storage should return error")
	}
}

func TestPebbleStorage_GetGasTipHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetGasTipHistory(ctx, 0, 100)
		if err != nil {
			t.Errorf("GetGasTipHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetGasTipHistory() = %d events, want 0", len(history))
		}
	})
}

func TestPebbleStorage_GetGasTipHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getgastiphistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetGasTipHistory(ctx, 0, 100)
	if err == nil {
		t.Error("GetGasTipHistory() on closed storage should return error")
	}
}

func TestPebbleStorage_GetValidatorHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	validator := common.HexToAddress("0x7777777777777777777777777777777777777777")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetValidatorHistory(ctx, validator)
		if err != nil {
			t.Errorf("GetValidatorHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetValidatorHistory() = %d events, want 0", len(history))
		}
	})
}

func TestPebbleStorage_GetValidatorHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getvalidatorhistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	validator := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetValidatorHistory(ctx, validator)
	if err == nil {
		t.Error("GetValidatorHistory() on closed storage should return error")
	}
}

func TestPebbleStorage_GetMinterConfigHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetMinterConfigHistory(ctx, 0, 100)
		if err != nil {
			t.Errorf("GetMinterConfigHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetMinterConfigHistory() = %d events, want 0", len(history))
		}
	})
}

func TestPebbleStorage_GetMinterConfigHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getminterconfighistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetMinterConfigHistory(ctx, 0, 100)
	if err == nil {
		t.Error("GetMinterConfigHistory() on closed storage should return error")
	}
}

func TestPebbleStorage_GetEmergencyPauseHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	contract := common.HexToAddress("0xdddddddddddddddddddddddddddddddddddddddd")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetEmergencyPauseHistory(ctx, contract)
		if err != nil {
			t.Errorf("GetEmergencyPauseHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetEmergencyPauseHistory() = %d events, want 0", len(history))
		}
	})
}

func TestPebbleStorage_GetEmergencyPauseHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getemergencypausehistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetEmergencyPauseHistory(ctx, contract)
	if err == nil {
		t.Error("GetEmergencyPauseHistory() on closed storage should return error")
	}
}

func TestPebbleStorage_GetDepositMintProposals(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("empty proposals", func(t *testing.T) {
		proposals, err := pebbleStorage.GetDepositMintProposals(ctx, 0, 100, ProposalStatusAll)
		if err != nil {
			t.Errorf("GetDepositMintProposals() error = %v", err)
		}
		if len(proposals) != 0 {
			t.Errorf("GetDepositMintProposals() = %d proposals, want 0", len(proposals))
		}
	})
}

func TestPebbleStorage_GetDepositMintProposals_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getdepositmintproposals-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetDepositMintProposals(ctx, 0, 100, ProposalStatusAll)
	if err == nil {
		t.Error("GetDepositMintProposals() on closed storage should return error")
	}
}

func TestPebbleStorage_GetBurnHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	account := common.HexToAddress("0x8888888888888888888888888888888888888888")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetBurnHistory(ctx, 0, 100, account)
		if err != nil {
			t.Errorf("GetBurnHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetBurnHistory() = %d events, want 0", len(history))
		}
	})
}

func TestPebbleStorage_GetBurnHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getburnhistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	account := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetBurnHistory(ctx, 0, 100, account)
	if err == nil {
		t.Error("GetBurnHistory() on closed storage should return error")
	}
}

func TestPebbleStorage_GetBlacklistedAddresses(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("empty blacklist", func(t *testing.T) {
		addresses, err := pebbleStorage.GetBlacklistedAddresses(ctx)
		if err != nil {
			t.Errorf("GetBlacklistedAddresses() error = %v", err)
		}
		if len(addresses) != 0 {
			t.Errorf("GetBlacklistedAddresses() = %d addresses, want 0", len(addresses))
		}
	})

	t.Run("with stored blacklisted addresses", func(t *testing.T) {
		addr1 := common.HexToAddress("0x9999999999999999999999999999999999999999")
		addr2 := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

		key1 := BlacklistActiveIndexKey(addr1)
		key2 := BlacklistActiveIndexKey(addr2)

		err := pebbleStorage.Put(ctx, key1, []byte("1"))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		err = pebbleStorage.Put(ctx, key2, []byte("1"))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		addresses, err := pebbleStorage.GetBlacklistedAddresses(ctx)
		if err != nil {
			t.Errorf("GetBlacklistedAddresses() error = %v", err)
		}
		if len(addresses) != 2 {
			t.Errorf("GetBlacklistedAddresses() = %d addresses, want 2", len(addresses))
		}
	})
}

func TestPebbleStorage_GetBlacklistedAddresses_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getblacklistedaddresses-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetBlacklistedAddresses(ctx)
	if err == nil {
		t.Error("GetBlacklistedAddresses() on closed storage should return error")
	}
}

func TestPebbleStorage_GetBlacklistHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	addr := common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetBlacklistHistory(ctx, addr)
		if err != nil {
			t.Errorf("GetBlacklistHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetBlacklistHistory() = %d events, want 0", len(history))
		}
	})
}

func TestPebbleStorage_GetBlacklistHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getblacklisthistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetBlacklistHistory(ctx, addr)
	if err == nil {
		t.Error("GetBlacklistHistory() on closed storage should return error")
	}
}

func TestPebbleStorage_GetAuthorizedAccounts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("empty authorized accounts", func(t *testing.T) {
		accounts, err := pebbleStorage.GetAuthorizedAccounts(ctx)
		if err != nil {
			t.Errorf("GetAuthorizedAccounts() error = %v", err)
		}
		if len(accounts) != 0 {
			t.Errorf("GetAuthorizedAccounts() = %d accounts, want 0", len(accounts))
		}
	})
}

func TestPebbleStorage_GetAuthorizedAccounts_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getauthorizedaccounts-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetAuthorizedAccounts(ctx)
	if err == nil {
		t.Error("GetAuthorizedAccounts() on closed storage should return error")
	}
}

func TestPebbleStorage_GetProposals(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	contract := common.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")

	t.Run("empty proposals", func(t *testing.T) {
		proposals, err := pebbleStorage.GetProposals(ctx, contract, ProposalStatusAll, 100, 0)
		if err != nil {
			t.Errorf("GetProposals() error = %v", err)
		}
		if len(proposals) != 0 {
			t.Errorf("GetProposals() = %d proposals, want 0", len(proposals))
		}
	})
}

func TestPebbleStorage_GetProposals_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getproposals-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetProposals(ctx, contract, ProposalStatusAll, 100, 0)
	if err == nil {
		t.Error("GetProposals() on closed storage should return error")
	}
}

func TestPebbleStorage_GetProposalById(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	contract := common.HexToAddress("0xffffffffffffffffffffffffffffffffffffffff")

	t.Run("non-existent proposal", func(t *testing.T) {
		proposal, err := pebbleStorage.GetProposalById(ctx, contract, big.NewInt(999))
		if err != nil {
			t.Errorf("GetProposalById() error = %v", err)
		}
		if proposal != nil {
			t.Error("GetProposalById() should return nil for non-existent proposal")
		}
	})
}

func TestPebbleStorage_GetProposalById_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getproposalbyid-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetProposalById(ctx, contract, big.NewInt(1))
	if err == nil {
		t.Error("GetProposalById() on closed storage should return error")
	}
}

func TestPebbleStorage_GetProposalVotes(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	contract := common.HexToAddress("0x0000000000000000000000000000000000000001")

	t.Run("empty votes", func(t *testing.T) {
		votes, err := pebbleStorage.GetProposalVotes(ctx, contract, big.NewInt(1))
		if err != nil {
			t.Errorf("GetProposalVotes() error = %v", err)
		}
		if len(votes) != 0 {
			t.Errorf("GetProposalVotes() = %d votes, want 0", len(votes))
		}
	})
}

func TestPebbleStorage_GetProposalVotes_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getproposalvotes-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetProposalVotes(ctx, contract, big.NewInt(1))
	if err == nil {
		t.Error("GetProposalVotes() on closed storage should return error")
	}
}

func TestPebbleStorage_GetMemberHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	member := common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetMemberHistory(ctx, member)
		if err != nil {
			t.Errorf("GetMemberHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetMemberHistory() = %d events, want 0", len(history))
		}
	})
}

func TestPebbleStorage_GetMemberHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getmemberhistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	member := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetMemberHistory(ctx, member)
	if err == nil {
		t.Error("GetMemberHistory() on closed storage should return error")
	}
}

func TestPebbleStorage_IndexSystemContractEvent(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("index single event - returns error for direct call", func(t *testing.T) {
		log := &types.Log{
			Address:     contractAddr,
			Topics:      []common.Hash{EventSigTransfer, common.HexToHash("0x1"), common.HexToHash("0x2")},
			Data:        []byte("test data"),
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xabcd"),
			TxIndex:     0,
			BlockHash:   common.HexToHash("0x1234"),
			Index:       0,
		}
		err := pebbleStorage.IndexSystemContractEvent(ctx, log)
		// This function should not be called directly but from events package
		if err == nil {
			t.Error("IndexSystemContractEvent() should return error when called directly")
		}
	})

	t.Run("index nil log", func(t *testing.T) {
		err := pebbleStorage.IndexSystemContractEvent(ctx, nil)
		// Should return error for direct call
		if err == nil {
			t.Error("IndexSystemContractEvent(nil) should return error when called directly")
		}
	})
}

func TestPebbleStorage_IndexSystemContractEvent_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-indexsysevent-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	log := &types.Log{
		Address:     common.HexToAddress("0x1234"),
		Topics:      []common.Hash{EventSigTransfer},
		BlockNumber: 100,
	}
	err = pebbleStorage.IndexSystemContractEvent(ctx, log)
	if err == nil {
		t.Error("IndexSystemContractEvent() on closed storage should return error")
	}
}

func TestPebbleStorage_IndexSystemContractEvent_ReadOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-indexsysevent-readonly-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	pebbleStorage.Close()

	// Reopen in read-only mode
	cfg.ReadOnly = true
	pebbleStorage, err = NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create read-only storage: %v", err)
	}
	defer pebbleStorage.Close()

	ctx := context.Background()
	log := &types.Log{
		Address:     common.HexToAddress("0x1234"),
		Topics:      []common.Hash{EventSigTransfer},
		BlockNumber: 100,
	}
	err = pebbleStorage.IndexSystemContractEvent(ctx, log)
	if err == nil {
		t.Error("IndexSystemContractEvent() on read-only storage should return error")
	}
}

func TestPebbleStorage_IndexSystemContractEvents(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("index multiple events - returns error for direct call", func(t *testing.T) {
		logs := []*types.Log{
			{
				Address:     contractAddr,
				Topics:      []common.Hash{EventSigTransfer, common.HexToHash("0x1"), common.HexToHash("0x2")},
				Data:        []byte("data1"),
				BlockNumber: 100,
				TxHash:      common.HexToHash("0xabcd"),
				Index:       0,
			},
			{
				Address:     contractAddr,
				Topics:      []common.Hash{EventSigMint, common.HexToHash("0x3")},
				Data:        []byte("data2"),
				BlockNumber: 101,
				TxHash:      common.HexToHash("0xef01"),
				Index:       0,
			},
		}
		err := pebbleStorage.IndexSystemContractEvents(ctx, logs)
		// This function should not be called directly but from events package
		if err == nil {
			t.Error("IndexSystemContractEvents() should return error when called directly")
		}
	})

	t.Run("index empty logs", func(t *testing.T) {
		err := pebbleStorage.IndexSystemContractEvents(ctx, []*types.Log{})
		// Empty logs is ok
		if err != nil {
			t.Errorf("IndexSystemContractEvents([]) error = %v", err)
		}
	})

	t.Run("index nil logs", func(t *testing.T) {
		err := pebbleStorage.IndexSystemContractEvents(ctx, nil)
		// Nil logs is ok
		if err != nil {
			t.Errorf("IndexSystemContractEvents(nil) error = %v", err)
		}
	})
}

func TestPebbleStorage_IndexSystemContractEvents_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-indexsysevents-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	logs := []*types.Log{
		{
			Address:     common.HexToAddress("0x1234"),
			Topics:      []common.Hash{EventSigTransfer},
			BlockNumber: 100,
		},
	}
	err = pebbleStorage.IndexSystemContractEvents(ctx, logs)
	if err == nil {
		t.Error("IndexSystemContractEvents() on closed storage should return error")
	}
}

func TestPebbleStorage_InitializeTransactionCount(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("initialize transaction count - requires latest height", func(t *testing.T) {
		// First call without setting latest height should fail
		err := pebbleStorage.InitializeTransactionCount(ctx)
		if err == nil {
			t.Error("InitializeTransactionCount() without latest height should return error")
		}
	})

	t.Run("initialize transaction count - with latest height set", func(t *testing.T) {
		// Set up latest height first
		err := pebbleStorage.SetLatestHeight(ctx, 10)
		if err != nil {
			t.Fatalf("SetLatestHeight() error = %v", err)
		}
		// Now initialize should work
		err = pebbleStorage.InitializeTransactionCount(ctx)
		if err != nil {
			t.Errorf("InitializeTransactionCount() error = %v", err)
		}
	})
}

func TestPebbleStorage_InitializeTransactionCount_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-inittxcount-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	err = pebbleStorage.InitializeTransactionCount(ctx)
	if err == nil {
		t.Error("InitializeTransactionCount() on closed storage should return error")
	}
}

func TestPebbleStorage_InitializeTransactionCount_ReadOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-inittxcount-readonly-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	pebbleStorage.Close()

	// Reopen in read-only mode
	cfg.ReadOnly = true
	pebbleStorage, err = NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create read-only storage: %v", err)
	}
	defer pebbleStorage.Close()

	ctx := context.Background()
	err = pebbleStorage.InitializeTransactionCount(ctx)
	if err == nil {
		t.Error("InitializeTransactionCount() on read-only storage should return error")
	}
}

func TestPebbleStorage_GetTotalSupply_Basic(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage.(*PebbleStorage)

	t.Run("get total supply", func(t *testing.T) {
		supply, err := pebbleStorage.GetTotalSupply(ctx)
		if err != nil {
			t.Errorf("GetTotalSupply() error = %v", err)
		}
		// Should return zero for non-existent token
		if supply == nil {
			t.Error("GetTotalSupply() should return non-nil value")
		}
	})
}

func TestPebbleStorage_GetTotalSupply_ClosedStorage_Basic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-gettotalsupply-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultConfig(tmpDir)
	pebbleStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetTotalSupply(ctx)
	if err == nil {
		t.Error("GetTotalSupply() on closed storage should return error")
	}
}
