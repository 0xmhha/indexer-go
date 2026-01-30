package watchlist

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestWatchedAddressKey(t *testing.T) {
	key := WatchedAddressKey("addr-123")
	expected := "/wl/addr/addr-123"
	if string(key) != expected {
		t.Errorf("expected %s, got %s", expected, string(key))
	}
}

func TestWatchedAddressKeyPrefix(t *testing.T) {
	prefix := WatchedAddressKeyPrefix()
	expected := "/wl/addr/"
	if string(prefix) != expected {
		t.Errorf("expected %s, got %s", expected, string(prefix))
	}
}

func TestChainAddressesKey(t *testing.T) {
	key := ChainAddressesKey("chain-1", "addr-123")
	expected := "/wl/chain/chain-1/addr-123"
	if string(key) != expected {
		t.Errorf("expected %s, got %s", expected, string(key))
	}
}

func TestChainAddressesKeyPrefix(t *testing.T) {
	prefix := ChainAddressesKeyPrefix("chain-1")
	expected := "/wl/chain/chain-1/"
	if string(prefix) != expected {
		t.Errorf("expected %s, got %s", expected, string(prefix))
	}
}

func TestBloomFilterKey(t *testing.T) {
	key := BloomFilterKey("chain-1")
	expected := "/wl/bloom/chain-1"
	if string(key) != expected {
		t.Errorf("expected %s, got %s", expected, string(key))
	}
}

func TestSubscriberKey(t *testing.T) {
	key := SubscriberKey("sub-456")
	expected := "/wl/sub/sub-456"
	if string(key) != expected {
		t.Errorf("expected %s, got %s", expected, string(key))
	}
}

func TestSubscriberKeyPrefix(t *testing.T) {
	prefix := SubscriberKeyPrefix()
	expected := "/wl/sub/"
	if string(prefix) != expected {
		t.Errorf("expected %s, got %s", expected, string(prefix))
	}
}

func TestAddressSubscribersKey(t *testing.T) {
	key := AddressSubscribersKey("addr-123", "sub-456")
	expected := "/wl/addr_subs/addr-123/sub-456"
	if string(key) != expected {
		t.Errorf("expected %s, got %s", expected, string(key))
	}
}

func TestAddressSubscribersKeyPrefix(t *testing.T) {
	prefix := AddressSubscribersKeyPrefix("addr-123")
	expected := "/wl/addr_subs/addr-123/"
	if string(prefix) != expected {
		t.Errorf("expected %s, got %s", expected, string(prefix))
	}
}

func TestWatchEventKey(t *testing.T) {
	txHash := common.HexToHash("0xabcdef1234567890")
	key := WatchEventKey("chain-1", 12345, txHash, 5)
	// Expected format: /wl/event/{chainID}/{blockNumber:20d}/{txHash}/{logIndex:06d}
	expected := "/wl/event/chain-1/00000000000000012345/0x000000000000000000000000000000000000000000000000abcdef1234567890/000005"
	if string(key) != expected {
		t.Errorf("expected %s, got %s", expected, string(key))
	}
}

func TestWatchEventKeyPrefix(t *testing.T) {
	prefix := WatchEventKeyPrefix()
	expected := "/wl/event/"
	if string(prefix) != expected {
		t.Errorf("expected %s, got %s", expected, string(prefix))
	}
}

func TestWatchEventChainKeyPrefix(t *testing.T) {
	prefix := WatchEventChainKeyPrefix("chain-1")
	expected := "/wl/event/chain-1/"
	if string(prefix) != expected {
		t.Errorf("expected %s, got %s", expected, string(prefix))
	}
}

func TestEventIndexKey(t *testing.T) {
	key := EventIndexKey("addr-123", 1704067200, "evt-789")
	// Expected format: /wl/eventidx/{addressID}/{timestamp:20d}/{eventID}
	expected := "/wl/eventidx/addr-123/00000000001704067200/evt-789"
	if string(key) != expected {
		t.Errorf("expected %s, got %s", expected, string(key))
	}
}

func TestEventIndexKeyPrefix(t *testing.T) {
	prefix := EventIndexKeyPrefix("addr-123")
	expected := "/wl/eventidx/addr-123/"
	if string(prefix) != expected {
		t.Errorf("expected %s, got %s", expected, string(prefix))
	}
}

func TestAddressByEthAddressKey(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	key := AddressByEthAddressKey("chain-1", addr)
	expected := "/wl/idx/addr/chain-1/0x1234567890123456789012345678901234567890"
	if string(key) != expected {
		t.Errorf("expected %s, got %s", expected, string(key))
	}
}

func TestAddressByEthAddressKeyPrefix(t *testing.T) {
	prefix := AddressByEthAddressKeyPrefix("chain-1")
	expected := "/wl/idx/addr/chain-1/"
	if string(prefix) != expected {
		t.Errorf("expected %s, got %s", expected, string(prefix))
	}
}

func TestAddressStatsKey(t *testing.T) {
	key := AddressStatsKey("addr-123")
	expected := "/wl/stats/addr-123"
	if string(key) != expected {
		t.Errorf("expected %s, got %s", expected, string(key))
	}
}

func TestAddressStatsKeyPrefix(t *testing.T) {
	prefix := AddressStatsKeyPrefix()
	expected := "/wl/stats/"
	if string(prefix) != expected {
		t.Errorf("expected %s, got %s", expected, string(prefix))
	}
}

// Table-driven test for all key functions
func TestStorageKeys_Consistency(t *testing.T) {
	// Ensure keys use correct prefixes
	tests := []struct {
		name           string
		key            []byte
		expectedPrefix string
	}{
		{"WatchedAddressKey", WatchedAddressKey("test"), "/wl/addr/"},
		{"ChainAddressesKey", ChainAddressesKey("chain", "addr"), "/wl/chain/"},
		{"BloomFilterKey", BloomFilterKey("chain"), "/wl/bloom/"},
		{"SubscriberKey", SubscriberKey("sub"), "/wl/sub/"},
		{"AddressSubscribersKey", AddressSubscribersKey("addr", "sub"), "/wl/addr_subs/"},
		{"WatchEventKey", WatchEventKey("chain", 1, common.Hash{}, 0), "/wl/event/"},
		{"EventIndexKey", EventIndexKey("addr", 0, "evt"), "/wl/eventidx/"},
		{"AddressByEthAddressKey", AddressByEthAddressKey("chain", common.Address{}), "/wl/idx/addr/"},
		{"AddressStatsKey", AddressStatsKey("addr"), "/wl/stats/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyStr := string(tt.key)
			if len(keyStr) < len(tt.expectedPrefix) || keyStr[:len(tt.expectedPrefix)] != tt.expectedPrefix {
				t.Errorf("%s: key %s does not start with expected prefix %s", tt.name, keyStr, tt.expectedPrefix)
			}
		})
	}
}
