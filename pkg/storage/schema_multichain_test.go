package storage

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestChainPrefix(t *testing.T) {
	tests := []struct {
		chainID  string
		expected string
	}{
		{"mainnet", "/chain/mainnet"},
		{"testnet", "/chain/testnet"},
		{"chain-1", "/chain/chain-1"},
	}

	for _, tt := range tests {
		result := ChainPrefix(tt.chainID)
		if result != tt.expected {
			t.Errorf("ChainPrefix(%q) = %q, want %q", tt.chainID, result, tt.expected)
		}
	}
}

func TestChainKeyPrefix(t *testing.T) {
	tests := []struct {
		chainID  string
		expected string
	}{
		{"mainnet", "/chain/mainnet/"},
		{"testnet", "/chain/testnet/"},
	}

	for _, tt := range tests {
		result := ChainKeyPrefix(tt.chainID)
		if string(result) != tt.expected {
			t.Errorf("ChainKeyPrefix(%q) = %q, want %q", tt.chainID, string(result), tt.expected)
		}
	}
}

func TestChainLatestHeightKey(t *testing.T) {
	result := ChainLatestHeightKey("mainnet")
	expected := "/chain/mainnet/meta/lh"
	if string(result) != expected {
		t.Errorf("ChainLatestHeightKey() = %q, want %q", string(result), expected)
	}
}

func TestChainBlockKey(t *testing.T) {
	result := ChainBlockKey("mainnet", 12345)
	expected := "/chain/mainnet/data/blocks/12345"
	if string(result) != expected {
		t.Errorf("ChainBlockKey() = %q, want %q", string(result), expected)
	}
}

func TestChainTransactionKey(t *testing.T) {
	result := ChainTransactionKey("mainnet", 12345, 3)
	expected := "/chain/mainnet/data/txs/12345/3"
	if string(result) != expected {
		t.Errorf("ChainTransactionKey() = %q, want %q", string(result), expected)
	}
}

func TestChainReceiptKey(t *testing.T) {
	txHash := common.HexToHash("0x1234567890abcdef")
	result := ChainReceiptKey("mainnet", txHash)
	expected := "/chain/mainnet/data/receipts/" + txHash.Hex()
	if string(result) != expected {
		t.Errorf("ChainReceiptKey() = %q, want %q", string(result), expected)
	}
}

func TestChainLogKey(t *testing.T) {
	result := ChainLogKey("mainnet", 12345, 2, 5)
	expected := "/chain/mainnet/data/logs/00000000000000012345/000002/000005"
	if string(result) != expected {
		t.Errorf("ChainLogKey() = %q, want %q", string(result), expected)
	}
}

func TestChainTxHashIndexKey(t *testing.T) {
	txHash := common.HexToHash("0xabcdef")
	result := ChainTxHashIndexKey("mainnet", txHash)
	expected := "/chain/mainnet/index/txh/" + txHash.Hex()
	if string(result) != expected {
		t.Errorf("ChainTxHashIndexKey() = %q, want %q", string(result), expected)
	}
}

func TestChainBlockHashIndexKey(t *testing.T) {
	blockHash := common.HexToHash("0xfedcba")
	result := ChainBlockHashIndexKey("mainnet", blockHash)
	expected := "/chain/mainnet/index/blockh/" + blockHash.Hex()
	if string(result) != expected {
		t.Errorf("ChainBlockHashIndexKey() = %q, want %q", string(result), expected)
	}
}

func TestChainAddressTransactionKey(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	result := ChainAddressTransactionKey("mainnet", addr, 100)
	expected := "/chain/mainnet/index/addr/" + addr.Hex() + "/00000000000000000100"
	if string(result) != expected {
		t.Errorf("ChainAddressTransactionKey() = %q, want %q", string(result), expected)
	}
}

func TestChainBlockCountKey(t *testing.T) {
	result := ChainBlockCountKey("mainnet")
	expected := "/chain/mainnet/meta/bc"
	if string(result) != expected {
		t.Errorf("ChainBlockCountKey() = %q, want %q", string(result), expected)
	}
}

func TestChainTransactionCountKey(t *testing.T) {
	result := ChainTransactionCountKey("mainnet")
	expected := "/chain/mainnet/meta/tc"
	if string(result) != expected {
		t.Errorf("ChainTransactionCountKey() = %q, want %q", string(result), expected)
	}
}

func TestChainBlockKeyPrefix(t *testing.T) {
	result := ChainBlockKeyPrefix("mainnet")
	expected := "/chain/mainnet/data/blocks/"
	if string(result) != expected {
		t.Errorf("ChainBlockKeyPrefix() = %q, want %q", string(result), expected)
	}
}

func TestChainLogKeyPrefix(t *testing.T) {
	result := ChainLogKeyPrefix("mainnet")
	expected := "/chain/mainnet/data/logs/"
	if string(result) != expected {
		t.Errorf("ChainLogKeyPrefix() = %q, want %q", string(result), expected)
	}
}

func TestChainAddressIndexKeyPrefix(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	result := ChainAddressIndexKeyPrefix("mainnet", addr)
	expected := "/chain/mainnet/index/addr/" + addr.Hex() + "/"
	if string(result) != expected {
		t.Errorf("ChainAddressIndexKeyPrefix() = %q, want %q", string(result), expected)
	}
}

func TestChainLogAddressIndexKey(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	result := ChainLogAddressIndexKey("mainnet", addr, 12345, 2, 5)
	expected := "/chain/mainnet/index/logs/addr/" + addr.Hex() + "/00000000000000012345/000002/000005"
	if string(result) != expected {
		t.Errorf("ChainLogAddressIndexKey() = %q, want %q", string(result), expected)
	}
}

func TestChainLogAddressIndexKeyPrefix(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	result := ChainLogAddressIndexKeyPrefix("mainnet", addr)
	expected := "/chain/mainnet/index/logs/addr/" + addr.Hex() + "/"
	if string(result) != expected {
		t.Errorf("ChainLogAddressIndexKeyPrefix() = %q, want %q", string(result), expected)
	}
}

func TestChainLogTopic0IndexKey(t *testing.T) {
	topic := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	result := ChainLogTopic0IndexKey("mainnet", topic, 12345, 2, 5)
	expected := "/chain/mainnet/index/logs/topic0/" + topic.Hex() + "/00000000000000012345/000002/000005"
	if string(result) != expected {
		t.Errorf("ChainLogTopic0IndexKey() = %q, want %q", string(result), expected)
	}
}

func TestChainLogTopic0IndexKeyPrefix(t *testing.T) {
	topic := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	result := ChainLogTopic0IndexKeyPrefix("mainnet", topic)
	expected := "/chain/mainnet/index/logs/topic0/" + topic.Hex() + "/"
	if string(result) != expected {
		t.Errorf("ChainLogTopic0IndexKeyPrefix() = %q, want %q", string(result), expected)
	}
}

func TestChainERC20TransferKey(t *testing.T) {
	txHash := common.HexToHash("0xabcdef1234567890")
	result := ChainERC20TransferKey("mainnet", txHash, 3)
	expected := "/chain/mainnet/data/erc20/transfer/" + txHash.Hex() + "/000003"
	if string(result) != expected {
		t.Errorf("ChainERC20TransferKey() = %q, want %q", string(result), expected)
	}
}

func TestChainERC721TransferKey(t *testing.T) {
	txHash := common.HexToHash("0xabcdef1234567890")
	result := ChainERC721TransferKey("mainnet", txHash, 5)
	expected := "/chain/mainnet/data/erc721/transfer/" + txHash.Hex() + "/000005"
	if string(result) != expected {
		t.Errorf("ChainERC721TransferKey() = %q, want %q", string(result), expected)
	}
}

func TestParseChainKey(t *testing.T) {
	tests := []struct {
		name          string
		key           []byte
		wantChainID   string
		wantRest      string
		wantErr       bool
	}{
		{
			name:        "valid chain key",
			key:         []byte("/chain/mainnet/data/blocks/12345"),
			wantChainID: "mainnet",
			wantRest:    "/data/blocks/12345",
			wantErr:     false,
		},
		{
			name:        "valid chain key with different chain",
			key:         []byte("/chain/testnet/meta/lh"),
			wantChainID: "testnet",
			wantRest:    "/meta/lh",
			wantErr:     false,
		},
		{
			name:        "not a chain key",
			key:         []byte("/data/blocks/12345"),
			wantChainID: "",
			wantRest:    "",
			wantErr:     true,
		},
		{
			name:        "invalid format - missing slash after chainID",
			key:         []byte("/chain/mainnet"),
			wantChainID: "",
			wantRest:    "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chainID, rest, err := ParseChainKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseChainKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if chainID != tt.wantChainID {
				t.Errorf("ParseChainKey() chainID = %q, want %q", chainID, tt.wantChainID)
			}
			if rest != tt.wantRest {
				t.Errorf("ParseChainKey() rest = %q, want %q", rest, tt.wantRest)
			}
		})
	}
}

func TestIsChainKey(t *testing.T) {
	tests := []struct {
		key      []byte
		expected bool
	}{
		{[]byte("/chain/mainnet/data/blocks/12345"), true},
		{[]byte("/chain/testnet/meta/lh"), true},
		{[]byte("/data/blocks/12345"), false},
		{[]byte("/meta/lh"), false},
		{[]byte(""), false},
	}

	for _, tt := range tests {
		result := IsChainKey(tt.key)
		if result != tt.expected {
			t.Errorf("IsChainKey(%q) = %v, want %v", string(tt.key), result, tt.expected)
		}
	}
}
