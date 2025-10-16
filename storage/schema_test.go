package storage

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestLatestHeightKey(t *testing.T) {
	key := LatestHeightKey()

	if len(key) == 0 {
		t.Error("LatestHeightKey() returned empty key")
	}

	// Should be consistent
	key2 := LatestHeightKey()
	if !bytes.Equal(key, key2) {
		t.Error("LatestHeightKey() is not consistent")
	}
}

func TestBlockKey(t *testing.T) {
	tests := []struct {
		name   string
		height uint64
	}{
		{"genesis", 0},
		{"block 1", 1},
		{"block 100", 100},
		{"large block", 1000000},
		{"max uint32", 4294967295},
		{"large uint64", 18446744073709551615},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := BlockKey(tt.height)

			if len(key) == 0 {
				t.Error("BlockKey() returned empty key")
			}

			// Should be unique per height
			if tt.height > 0 {
				prevKey := BlockKey(tt.height - 1)
				if bytes.Equal(key, prevKey) {
					t.Error("BlockKey() generated same key for different heights")
				}
			}

			// Should parse back correctly
			parsed, err := ParseBlockKey(key)
			if err != nil {
				t.Errorf("ParseBlockKey() error = %v", err)
			}
			if parsed != tt.height {
				t.Errorf("ParseBlockKey() = %d, want %d", parsed, tt.height)
			}
		})
	}
}

func TestTransactionKey(t *testing.T) {
	tests := []struct {
		name    string
		height  uint64
		txIndex uint64
	}{
		{"genesis tx 0", 0, 0},
		{"block 1 tx 0", 1, 0},
		{"block 1 tx 5", 1, 5},
		{"block 100 tx 0", 100, 0},
		{"large block large tx", 1000000, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := TransactionKey(tt.height, tt.txIndex)

			if len(key) == 0 {
				t.Error("TransactionKey() returned empty key")
			}

			// Should be unique
			if tt.txIndex > 0 {
				prevKey := TransactionKey(tt.height, tt.txIndex-1)
				if bytes.Equal(key, prevKey) {
					t.Error("TransactionKey() generated same key for different indices")
				}
			}

			// Should parse back correctly
			height, txIndex, err := ParseTransactionKey(key)
			if err != nil {
				t.Errorf("ParseTransactionKey() error = %v", err)
			}
			if height != tt.height {
				t.Errorf("ParseTransactionKey() height = %d, want %d", height, tt.height)
			}
			if txIndex != tt.txIndex {
				t.Errorf("ParseTransactionKey() txIndex = %d, want %d", txIndex, tt.txIndex)
			}
		})
	}
}

func TestReceiptKey(t *testing.T) {
	tests := []struct {
		name   string
		txHash common.Hash
	}{
		{
			"zero hash",
			common.Hash{},
		},
		{
			"sample hash 1",
			common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		},
		{
			"sample hash 2",
			common.HexToHash("0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := ReceiptKey(tt.txHash)

			if len(key) == 0 {
				t.Error("ReceiptKey() returned empty key")
			}

			// Different hashes should produce different keys
			if tt.txHash != (common.Hash{}) {
				differentHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
				differentKey := ReceiptKey(differentHash)
				if bytes.Equal(key, differentKey) {
					t.Error("ReceiptKey() generated same key for different hashes")
				}
			}
		})
	}
}

func TestTransactionHashIndexKey(t *testing.T) {
	tests := []struct {
		name   string
		txHash common.Hash
	}{
		{
			"zero hash",
			common.Hash{},
		},
		{
			"sample hash",
			common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := TransactionHashIndexKey(tt.txHash)

			if len(key) == 0 {
				t.Error("TransactionHashIndexKey() returned empty key")
			}
		})
	}
}

func TestAddressTransactionKey(t *testing.T) {
	tests := []struct {
		name string
		addr common.Address
		seq  uint64
	}{
		{
			"zero address seq 0",
			common.Address{},
			0,
		},
		{
			"sample address seq 0",
			common.HexToAddress("0x1234567890123456789012345678901234567890"),
			0,
		},
		{
			"sample address seq 1",
			common.HexToAddress("0x1234567890123456789012345678901234567890"),
			1,
		},
		{
			"sample address seq 100",
			common.HexToAddress("0x1234567890123456789012345678901234567890"),
			100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := AddressTransactionKey(tt.addr, tt.seq)

			if len(key) == 0 {
				t.Error("AddressTransactionKey() returned empty key")
			}

			// Different sequences should produce different keys
			if tt.seq > 0 {
				prevKey := AddressTransactionKey(tt.addr, tt.seq-1)
				if bytes.Equal(key, prevKey) {
					t.Error("AddressTransactionKey() generated same key for different sequences")
				}
			}

			// Different addresses should produce different keys
			if tt.addr != (common.Address{}) {
				differentAddr := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
				differentKey := AddressTransactionKey(differentAddr, tt.seq)
				if bytes.Equal(key, differentKey) {
					t.Error("AddressTransactionKey() generated same key for different addresses")
				}
			}
		})
	}
}

func TestParseBlockKey_InvalidKey(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{"nil key", nil},
		{"empty key", []byte{}},
		{"wrong prefix", []byte("wrong/prefix")},
		{"incomplete key", []byte("/data/blocks/")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseBlockKey(tt.key)
			if err == nil {
				t.Error("ParseBlockKey() should return error for invalid key")
			}
		})
	}
}

func TestParseTransactionKey_InvalidKey(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{"nil key", nil},
		{"empty key", []byte{}},
		{"wrong prefix", []byte("wrong/prefix")},
		{"incomplete key", []byte("/data/txs/100/")},
		{"missing index", []byte("/data/txs/100")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseTransactionKey(tt.key)
			if err == nil {
				t.Error("ParseTransactionKey() should return error for invalid key")
			}
		})
	}
}

func TestKeyPrefixes(t *testing.T) {
	// Test that keys have correct prefixes and don't overlap
	latestHeight := LatestHeightKey()
	block := BlockKey(100)
	tx := TransactionKey(100, 5)
	receipt := ReceiptKey(common.HexToHash("0x1234"))
	txhIndex := TransactionHashIndexKey(common.HexToHash("0x1234"))
	addrTx := AddressTransactionKey(common.HexToAddress("0x1234"), 0)

	// All keys should be different
	keys := [][]byte{latestHeight, block, tx, receipt, txhIndex, addrTx}
	for i, key1 := range keys {
		for j, key2 := range keys {
			if i != j && bytes.Equal(key1, key2) {
				t.Errorf("Key %d and %d are equal", i, j)
			}
		}
	}
}

func BenchmarkBlockKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		BlockKey(uint64(i))
	}
}

func BenchmarkParseBlockKey(b *testing.B) {
	key := BlockKey(123456)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseBlockKey(key)
	}
}

func BenchmarkTransactionKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		TransactionKey(uint64(i), uint64(i%100))
	}
}

func BenchmarkAddressTransactionKey(b *testing.B) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AddressTransactionKey(addr, uint64(i))
	}
}

func TestBlockKeyRange(t *testing.T) {
	start, end := BlockKeyRange(100, 200)

	if len(start) == 0 {
		t.Error("BlockKeyRange() start key is empty")
	}
	if len(end) == 0 {
		t.Error("BlockKeyRange() end key is empty")
	}

	// Verify start key
	height, err := ParseBlockKey(start)
	if err != nil {
		t.Errorf("ParseBlockKey(start) error = %v", err)
	}
	if height != 100 {
		t.Errorf("start height = %d, want 100", height)
	}

	// Verify end key
	height, err = ParseBlockKey(end)
	if err != nil {
		t.Errorf("ParseBlockKey(end) error = %v", err)
	}
	if height != 201 {
		t.Errorf("end height = %d, want 201", height)
	}
}

func TestAddressTransactionKeyPrefix(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	prefix := AddressTransactionKeyPrefix(addr)

	if len(prefix) == 0 {
		t.Error("AddressTransactionKeyPrefix() returned empty prefix")
	}

	// Verify that keys with this address have this prefix
	key1 := AddressTransactionKey(addr, 0)
	key2 := AddressTransactionKey(addr, 1)

	if !HasPrefix(key1, prefix) {
		t.Error("key1 doesn't have the expected prefix")
	}
	if !HasPrefix(key2, prefix) {
		t.Error("key2 doesn't have the expected prefix")
	}

	// Keys with different address should not have this prefix
	differentAddr := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	differentKey := AddressTransactionKey(differentAddr, 0)
	if HasPrefix(differentKey, prefix) {
		t.Error("differentKey should not have the same prefix")
	}
}

func TestHasPrefix(t *testing.T) {
	tests := []struct {
		name       string
		key        []byte
		prefix     []byte
		wantResult bool
	}{
		{"exact match", []byte("hello"), []byte("hello"), true},
		{"has prefix", []byte("hello world"), []byte("hello"), true},
		{"no prefix", []byte("hello world"), []byte("world"), false},
		{"empty prefix", []byte("hello"), []byte(""), true},
		{"empty key", []byte(""), []byte("hello"), false},
		{"both empty", []byte(""), []byte(""), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasPrefix(tt.key, tt.prefix)
			if result != tt.wantResult {
				t.Errorf("HasPrefix() = %v, want %v", result, tt.wantResult)
			}
		})
	}
}

func TestIsMetadataKey(t *testing.T) {
	tests := []struct {
		name       string
		key        []byte
		wantResult bool
	}{
		{"latest height", LatestHeightKey(), true},
		{"block key", BlockKey(100), false},
		{"transaction key", TransactionKey(100, 5), false},
		{"empty", []byte(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsMetadataKey(tt.key)
			if result != tt.wantResult {
				t.Errorf("IsMetadataKey() = %v, want %v", result, tt.wantResult)
			}
		})
	}
}

func TestIsDataKey(t *testing.T) {
	tests := []struct {
		name       string
		key        []byte
		wantResult bool
	}{
		{"block key", BlockKey(100), true},
		{"transaction key", TransactionKey(100, 5), true},
		{"receipt key", ReceiptKey(common.Hash{}), true},
		{"latest height", LatestHeightKey(), false},
		{"tx hash index", TransactionHashIndexKey(common.Hash{}), false},
		{"empty", []byte(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDataKey(tt.key)
			if result != tt.wantResult {
				t.Errorf("IsDataKey() = %v, want %v", result, tt.wantResult)
			}
		})
	}
}

func TestIsIndexKey(t *testing.T) {
	tests := []struct {
		name       string
		key        []byte
		wantResult bool
	}{
		{"tx hash index", TransactionHashIndexKey(common.Hash{}), true},
		{"address tx key", AddressTransactionKey(common.Address{}, 0), true},
		{"block key", BlockKey(100), false},
		{"latest height", LatestHeightKey(), false},
		{"empty", []byte(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsIndexKey(tt.key)
			if result != tt.wantResult {
				t.Errorf("IsIndexKey() = %v, want %v", result, tt.wantResult)
			}
		})
	}
}
