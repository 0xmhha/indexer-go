package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// TestTransactionFilter_Validate tests filter validation
func TestTransactionFilter_Validate(t *testing.T) {
	tests := []struct {
		name    string
		filter  *TransactionFilter
		wantErr bool
	}{
		{
			name: "valid filter",
			filter: &TransactionFilter{
				FromBlock:   0,
				ToBlock:     1000,
				MinValue:    big.NewInt(0),
				MaxValue:    big.NewInt(1000),
				TxType:      TxTypeAll,
				SuccessOnly: false,
			},
			wantErr: false,
		},
		{
			name: "invalid block range",
			filter: &TransactionFilter{
				FromBlock: 1000,
				ToBlock:   0,
			},
			wantErr: true,
		},
		{
			name: "invalid value range",
			filter: &TransactionFilter{
				FromBlock: 0,
				ToBlock:   1000,
				MinValue:  big.NewInt(1000),
				MaxValue:  big.NewInt(0),
			},
			wantErr: true,
		},
		{
			name: "negative min value",
			filter: &TransactionFilter{
				FromBlock: 0,
				ToBlock:   1000,
				MinValue:  big.NewInt(-1),
			},
			wantErr: true,
		},
		{
			name: "negative max value",
			filter: &TransactionFilter{
				FromBlock: 0,
				ToBlock:   1000,
				MaxValue:  big.NewInt(-1),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDefaultTransactionFilter tests default filter
func TestDefaultTransactionFilter(t *testing.T) {
	filter := DefaultTransactionFilter()

	if filter.FromBlock != 0 {
		t.Errorf("FromBlock = %d, want 0", filter.FromBlock)
	}

	if filter.ToBlock != ^uint64(0) {
		t.Errorf("ToBlock = %d, want max uint64", filter.ToBlock)
	}

	if filter.TxType != TxTypeAll {
		t.Errorf("TxType = %v, want TxTypeAll", filter.TxType)
	}

	if filter.SuccessOnly {
		t.Errorf("SuccessOnly = true, want false")
	}

	if err := filter.Validate(); err != nil {
		t.Errorf("Default filter validation failed: %v", err)
	}
}

// TestBalanceSnapshot_EncodeDecode tests BalanceSnapshot encoding and decoding
func TestBalanceSnapshot_EncodeDecode(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *BalanceSnapshot
	}{
		{
			name: "positive balance and delta",
			snapshot: &BalanceSnapshot{
				BlockNumber: 1000,
				Balance:     big.NewInt(123456789),
				Delta:       big.NewInt(1000),
				TxHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			},
		},
		{
			name: "negative delta",
			snapshot: &BalanceSnapshot{
				BlockNumber: 2000,
				Balance:     big.NewInt(999999),
				Delta:       big.NewInt(-5000),
				TxHash:      common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
			},
		},
		{
			name: "zero balance",
			snapshot: &BalanceSnapshot{
				BlockNumber: 3000,
				Balance:     big.NewInt(0),
				Delta:       big.NewInt(0),
				TxHash:      common.Hash{},
			},
		},
		{
			name: "large balance",
			snapshot: &BalanceSnapshot{
				BlockNumber: 4000,
				Balance:     new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), // 1 ETH in wei
				Delta:       new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil), // 0.1 ETH
				TxHash:      common.HexToHash("0x9999999999999999999999999999999999999999999999999999999999999999"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			encoded, err := EncodeBalanceSnapshot(tt.snapshot)
			if err != nil {
				t.Fatalf("EncodeBalanceSnapshot() error = %v", err)
			}

			// Decode
			decoded, err := DecodeBalanceSnapshot(encoded)
			if err != nil {
				t.Fatalf("DecodeBalanceSnapshot() error = %v", err)
			}

			// Verify
			if decoded.BlockNumber != tt.snapshot.BlockNumber {
				t.Errorf("BlockNumber = %d, want %d", decoded.BlockNumber, tt.snapshot.BlockNumber)
			}

			if decoded.Balance.Cmp(tt.snapshot.Balance) != 0 {
				t.Errorf("Balance = %v, want %v", decoded.Balance, tt.snapshot.Balance)
			}

			if decoded.Delta.Cmp(tt.snapshot.Delta) != 0 {
				t.Errorf("Delta = %v, want %v", decoded.Delta, tt.snapshot.Delta)
			}

			if decoded.TxHash != tt.snapshot.TxHash {
				t.Errorf("TxHash = %v, want %v", decoded.TxHash, tt.snapshot.TxHash)
			}
		})
	}
}

// TestBalanceSnapshot_EncodeDecodeError tests error cases
func TestBalanceSnapshot_EncodeDecodeError(t *testing.T) {
	t.Run("encode nil snapshot", func(t *testing.T) {
		_, err := EncodeBalanceSnapshot(nil)
		if err == nil {
			t.Error("Expected error encoding nil snapshot")
		}
	})

	t.Run("decode empty data", func(t *testing.T) {
		_, err := DecodeBalanceSnapshot([]byte{})
		if err == nil {
			t.Error("Expected error decoding empty data")
		}
	})

	t.Run("decode invalid data", func(t *testing.T) {
		_, err := DecodeBalanceSnapshot([]byte{1, 2, 3})
		if err == nil {
			t.Error("Expected error decoding invalid data")
		}
	})
}

// TestEncodeDecode_BigInt tests big.Int encoding and decoding
func TestEncodeDecode_BigInt(t *testing.T) {
	tests := []struct {
		name string
		n    *big.Int
	}{
		{"zero", big.NewInt(0)},
		{"small positive", big.NewInt(123)},
		{"large positive", new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)},
		{"nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeBigInt(tt.n)
			decoded := DecodeBigInt(encoded)

			expected := tt.n
			if expected == nil {
				expected = big.NewInt(0)
			}

			if decoded.Cmp(expected) != 0 {
				t.Errorf("DecodeBigInt(EncodeBigInt(%v)) = %v, want %v", tt.n, decoded, expected)
			}
		})
	}
}

// TestGetBlocksByTimeRange tests time-based block queries
func TestGetBlocksByTimeRange(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Create test blocks with timestamps
	blocks := []*types.Block{
		createTestBlockWithTimestamp(t, 1, 1000),
		createTestBlockWithTimestamp(t, 2, 2000),
		createTestBlockWithTimestamp(t, 3, 3000),
		createTestBlockWithTimestamp(t, 4, 4000),
		createTestBlockWithTimestamp(t, 5, 5000),
	}

	// Store blocks and index timestamps
	for _, block := range blocks {
		if err := storage.SetBlock(ctx, block); err != nil {
			t.Fatalf("SetBlock() error = %v", err)
		}
		if err := storage.SetBlockTimestamp(ctx, block.Time(), block.Number().Uint64()); err != nil {
			t.Fatalf("SetBlockTimestamp() error = %v", err)
		}
	}

	tests := []struct {
		name      string
		fromTime  uint64
		toTime    uint64
		limit     int
		offset    int
		wantCount int
	}{
		{"all blocks", 1000, 5000, 10, 0, 5},
		{"middle range", 2000, 4000, 10, 0, 3},
		{"with limit", 1000, 5000, 3, 0, 3},
		{"with offset", 1000, 5000, 10, 2, 3},
		{"limit and offset", 1000, 5000, 2, 1, 2},
		{"no results", 6000, 7000, 10, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := storage.GetBlocksByTimeRange(ctx, tt.fromTime, tt.toTime, tt.limit, tt.offset)
			if err != nil {
				t.Fatalf("GetBlocksByTimeRange() error = %v", err)
			}

			if len(results) != tt.wantCount {
				t.Errorf("GetBlocksByTimeRange() got %d blocks, want %d", len(results), tt.wantCount)
			}
		})
	}
}

// TestGetBlockByTimestamp tests closest block lookup
func TestGetBlockByTimestamp(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Create test blocks with timestamps
	blocks := []*types.Block{
		createTestBlockWithTimestamp(t, 1, 1000),
		createTestBlockWithTimestamp(t, 2, 2000),
		createTestBlockWithTimestamp(t, 3, 3000),
	}

	// Store blocks and index timestamps
	for _, block := range blocks {
		if err := storage.SetBlock(ctx, block); err != nil {
			t.Fatalf("SetBlock() error = %v", err)
		}
		if err := storage.SetBlockTimestamp(ctx, block.Time(), block.Number().Uint64()); err != nil {
			t.Fatalf("SetBlockTimestamp() error = %v", err)
		}
	}

	tests := []struct {
		name           string
		timestamp      uint64
		wantHeight     uint64
		expectNotFound bool
	}{
		{"exact match", 2000, 2, false},
		{"before first - returns first block", 500, 1, false},
		{"between blocks - returns next block", 2500, 3, false},
		{"after last - returns last block", 4000, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block, err := storage.GetBlockByTimestamp(ctx, tt.timestamp)
			if tt.expectNotFound {
				if err != ErrNotFound {
					t.Errorf("GetBlockByTimestamp() error = %v, want ErrNotFound", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("GetBlockByTimestamp() error = %v", err)
			}

			if block.Number().Uint64() != tt.wantHeight {
				t.Errorf("GetBlockByTimestamp() height = %d, want %d", block.Number().Uint64(), tt.wantHeight)
			}
		})
	}
}

// TestUpdateBalance tests balance tracking
func TestUpdateBalance(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Test initial balance (should be 0)
	balance, err := storage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("Initial balance = %v, want 0", balance)
	}

	// Update balance with positive delta
	delta1 := big.NewInt(1000)
	txHash1 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	if err := storage.UpdateBalance(ctx, addr, 1, delta1, txHash1); err != nil {
		t.Fatalf("UpdateBalance() error = %v", err)
	}

	// Check updated balance
	balance, err = storage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("Balance after first update = %v, want 1000", balance)
	}

	// Update balance with negative delta
	delta2 := big.NewInt(-300)
	txHash2 := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	if err := storage.UpdateBalance(ctx, addr, 2, delta2, txHash2); err != nil {
		t.Fatalf("UpdateBalance() error = %v", err)
	}

	// Check final balance
	balance, err = storage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(700)) != 0 {
		t.Errorf("Balance after second update = %v, want 700", balance)
	}

	// Test historical balance (at block 1)
	balance, err = storage.GetAddressBalance(ctx, addr, 1)
	if err != nil {
		t.Fatalf("GetAddressBalance() error = %v", err)
	}
	if balance.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("Historical balance at block 1 = %v, want 1000", balance)
	}
}

// TestGetBalanceHistory tests balance history retrieval
func TestGetBalanceHistory(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Create balance history
	deltas := []struct {
		block uint64
		delta *big.Int
		hash  common.Hash
	}{
		{1, big.NewInt(1000), common.HexToHash("0x1111")},
		{2, big.NewInt(500), common.HexToHash("0x2222")},
		{3, big.NewInt(-300), common.HexToHash("0x3333")},
		{4, big.NewInt(200), common.HexToHash("0x4444")},
	}

	for _, d := range deltas {
		if err := storage.UpdateBalance(ctx, addr, d.block, d.delta, d.hash); err != nil {
			t.Fatalf("UpdateBalance() error = %v", err)
		}
	}

	tests := []struct {
		name      string
		fromBlock uint64
		toBlock   uint64
		limit     int
		offset    int
		wantCount int
	}{
		{"all history", 0, 10, 10, 0, 4},
		{"block range", 2, 3, 10, 0, 2},
		{"with limit", 0, 10, 2, 0, 2},
		{"with offset", 0, 10, 10, 2, 2},
		{"no results", 5, 10, 10, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history, err := storage.GetBalanceHistory(ctx, addr, tt.fromBlock, tt.toBlock, tt.limit, tt.offset)
			if err != nil {
				t.Fatalf("GetBalanceHistory() error = %v", err)
			}

			if len(history) != tt.wantCount {
				t.Errorf("GetBalanceHistory() got %d snapshots, want %d", len(history), tt.wantCount)
			}
		})
	}
}

// Helper functions

func createTestBlockWithTimestamp(t *testing.T, height uint64, timestamp uint64) *types.Block {
	t.Helper()

	header := &types.Header{
		Number:     big.NewInt(int64(height)),
		Time:       timestamp,
		Difficulty: big.NewInt(1),
		GasLimit:   1000000,
	}

	return types.NewBlock(header, nil, nil, nil)
}
