package multichain

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestStorageKeyPrefix(t *testing.T) {
	tests := []struct {
		name     string
		chainID  string
		expected string
	}{
		{
			name:     "simple chain ID",
			chainID:  "ethereum",
			expected: "chain:ethereum:",
		},
		{
			name:     "chain ID with hyphen",
			chainID:  "eth-mainnet",
			expected: "chain:eth-mainnet:",
		},
		{
			name:     "numeric chain ID",
			chainID:  "1",
			expected: "chain:1:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StorageKeyPrefix(tt.chainID)
			if result != tt.expected {
				t.Errorf("StorageKeyPrefix(%q) = %q, want %q", tt.chainID, result, tt.expected)
			}
		})
	}
}

func TestBlockKey(t *testing.T) {
	tests := []struct {
		name     string
		chainID  string
		height   uint64
		expected string
	}{
		{
			name:     "block zero",
			chainID:  "ethereum",
			height:   0,
			expected: "chain:ethereum:block:00000000000000000000",
		},
		{
			name:     "block with height",
			chainID:  "ethereum",
			height:   12345,
			expected: "chain:ethereum:block:00000000000000012345",
		},
		{
			name:     "large block height",
			chainID:  "eth",
			height:   18000000,
			expected: "chain:eth:block:00000000000018000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BlockKey(tt.chainID, tt.height)
			if result != tt.expected {
				t.Errorf("BlockKey(%q, %d) = %q, want %q", tt.chainID, tt.height, result, tt.expected)
			}
		})
	}
}

func TestTxKey(t *testing.T) {
	hash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	result := TxKey("ethereum", hash)
	expected := "chain:ethereum:tx:" + hash.Hex()
	if result != expected {
		t.Errorf("TxKey() = %q, want %q", result, expected)
	}
}

func TestReceiptKey(t *testing.T) {
	hash := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	result := ReceiptKey("polygon", hash)
	expected := "chain:polygon:receipt:" + hash.Hex()
	if result != expected {
		t.Errorf("ReceiptKey() = %q, want %q", result, expected)
	}
}

func TestLogKey(t *testing.T) {
	tests := []struct {
		name     string
		chainID  string
		blockNum uint64
		logIndex uint
		expected string
	}{
		{
			name:     "first log in first block",
			chainID:  "eth",
			blockNum: 0,
			logIndex: 0,
			expected: "chain:eth:log:00000000000000000000:00000000000000000000",
		},
		{
			name:     "log with indices",
			chainID:  "eth",
			blockNum: 12345,
			logIndex: 42,
			expected: "chain:eth:log:00000000000000012345:00000000000000000042",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LogKey(tt.chainID, tt.blockNum, tt.logIndex)
			if result != tt.expected {
				t.Errorf("LogKey(%q, %d, %d) = %q, want %q", tt.chainID, tt.blockNum, tt.logIndex, result, tt.expected)
			}
		})
	}
}

func TestLatestHeightKey(t *testing.T) {
	tests := []struct {
		name     string
		chainID  string
		expected string
	}{
		{
			name:     "simple chain ID",
			chainID:  "ethereum",
			expected: "chain:ethereum:latest",
		},
		{
			name:     "chain ID with hyphen",
			chainID:  "eth-sepolia",
			expected: "chain:eth-sepolia:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LatestHeightKey(tt.chainID)
			if result != tt.expected {
				t.Errorf("LatestHeightKey(%q) = %q, want %q", tt.chainID, result, tt.expected)
			}
		})
	}
}

func TestUintToString(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected string
	}{
		{
			name:     "zero",
			input:    0,
			expected: "00000000000000000000",
		},
		{
			name:     "small number",
			input:    123,
			expected: "00000000000000000123",
		},
		{
			name:     "large number",
			input:    18446744073709551615, // max uint64
			expected: "18446744073709551615",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := uintToString(tt.input)
			if result != tt.expected {
				t.Errorf("uintToString(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
