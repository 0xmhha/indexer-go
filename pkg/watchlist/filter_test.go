package watchlist

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestNewEventMatcher(t *testing.T) {
	matcher := NewEventMatcher()

	if matcher == nil {
		t.Fatal("expected matcher to not be nil")
	}

	if matcher.HasWatchedAddresses("any-chain") {
		t.Error("new matcher should have no watched addresses")
	}
}

func TestEventMatcherAddAddress(t *testing.T) {
	matcher := NewEventMatcher()

	watched := &WatchedAddress{
		ID:      "addr-1",
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
		Filter:  DefaultWatchFilter(),
	}

	matcher.AddAddress(watched)

	if !matcher.HasWatchedAddresses("chain-1") {
		t.Error("matcher should have watched addresses for chain-1")
	}

	count := matcher.GetWatchedAddressCount("chain-1")
	if count != 1 {
		t.Errorf("expected 1 watched address, got %d", count)
	}
}

func TestEventMatcherAddMultipleAddresses(t *testing.T) {
	matcher := NewEventMatcher()

	// Add addresses for chain-1
	for i := 0; i < 3; i++ {
		watched := &WatchedAddress{
			ID:      "addr-" + string(rune('a'+i)),
			Address: common.BigToAddress(big.NewInt(int64(i + 1))),
			ChainID: "chain-1",
			Filter:  DefaultWatchFilter(),
		}
		matcher.AddAddress(watched)
	}

	// Add address for chain-2
	watched := &WatchedAddress{
		ID:      "addr-other",
		Address: common.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		ChainID: "chain-2",
		Filter:  DefaultWatchFilter(),
	}
	matcher.AddAddress(watched)

	if matcher.GetWatchedAddressCount("chain-1") != 3 {
		t.Errorf("expected 3 addresses for chain-1, got %d", matcher.GetWatchedAddressCount("chain-1"))
	}

	if matcher.GetWatchedAddressCount("chain-2") != 1 {
		t.Errorf("expected 1 address for chain-2, got %d", matcher.GetWatchedAddressCount("chain-2"))
	}
}

func TestEventMatcherRemoveAddress(t *testing.T) {
	matcher := NewEventMatcher()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	watched := &WatchedAddress{
		ID:      "addr-1",
		Address: addr,
		ChainID: "chain-1",
		Filter:  DefaultWatchFilter(),
	}

	matcher.AddAddress(watched)

	if matcher.GetWatchedAddressCount("chain-1") != 1 {
		t.Fatal("address should be added")
	}

	matcher.RemoveAddress("chain-1", addr)

	if matcher.GetWatchedAddressCount("chain-1") != 0 {
		t.Error("address should be removed")
	}
}

func TestEventMatcherSetAndGetBloomFilter(t *testing.T) {
	matcher := NewEventMatcher()

	// No bloom filter initially
	if matcher.GetBloomFilter("chain-1") != nil {
		t.Error("expected no bloom filter initially")
	}

	// Set a bloom filter
	bf := NewBloomFilter(nil)
	matcher.SetBloomFilter("chain-1", bf)

	retrieved := matcher.GetBloomFilter("chain-1")
	if retrieved == nil {
		t.Fatal("expected bloom filter to be set")
	}
}

func TestEventMatcherBloomFilterCreatedOnAdd(t *testing.T) {
	matcher := NewEventMatcher()

	watched := &WatchedAddress{
		ID:      "addr-1",
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
		Filter:  DefaultWatchFilter(),
	}

	matcher.AddAddress(watched)

	bf := matcher.GetBloomFilter("chain-1")
	if bf == nil {
		t.Fatal("bloom filter should be created on add")
	}

	if !bf.MightContain(watched.Address) {
		t.Error("bloom filter should contain the added address")
	}
}

func TestEventMatcherHasWatchedAddresses(t *testing.T) {
	matcher := NewEventMatcher()

	// Empty
	if matcher.HasWatchedAddresses("chain-1") {
		t.Error("empty matcher should return false")
	}

	// Add address
	watched := &WatchedAddress{
		ID:      "addr-1",
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
		Filter:  DefaultWatchFilter(),
	}
	matcher.AddAddress(watched)

	if !matcher.HasWatchedAddresses("chain-1") {
		t.Error("should have watched addresses for chain-1")
	}

	if matcher.HasWatchedAddresses("chain-2") {
		t.Error("should not have watched addresses for chain-2")
	}
}

func TestMatchesValueFilter(t *testing.T) {
	matcher := NewEventMatcher()

	tests := []struct {
		name     string
		value    *big.Int
		minValue string
		expected bool
	}{
		{
			name:     "empty filter matches all",
			value:    big.NewInt(100),
			minValue: "",
			expected: true,
		},
		{
			name:     "value equals min",
			value:    big.NewInt(1000),
			minValue: "1000",
			expected: true,
		},
		{
			name:     "value above min",
			value:    big.NewInt(2000),
			minValue: "1000",
			expected: true,
		},
		{
			name:     "value below min",
			value:    big.NewInt(500),
			minValue: "1000",
			expected: false,
		},
		{
			name:     "zero value",
			value:    big.NewInt(0),
			minValue: "1000",
			expected: false,
		},
		{
			name:     "large value",
			value:    new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), // 1e18
			minValue: "1000000000000000000",                                  // 1e18
			expected: true,
		},
		{
			name:     "invalid filter passes through",
			value:    big.NewInt(100),
			minValue: "not-a-number",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.matchesValueFilter(tt.value, tt.minValue)
			if result != tt.expected {
				t.Errorf("matchesValueFilter(%v, %s) = %v, expected %v",
					tt.value, tt.minValue, result, tt.expected)
			}
		})
	}
}

func TestDefaultWatchFilter(t *testing.T) {
	filter := DefaultWatchFilter()

	if filter == nil {
		t.Fatal("expected filter to not be nil")
	}

	// Check all default values are true
	if !filter.TxFrom {
		t.Error("TxFrom should be true by default")
	}
	if !filter.TxTo {
		t.Error("TxTo should be true by default")
	}
	if !filter.ERC20 {
		t.Error("ERC20 should be true by default")
	}
	if !filter.ERC721 {
		t.Error("ERC721 should be true by default")
	}
	if filter.Logs {
		t.Error("Logs should be false by default (disabled to avoid noise)")
	}
	if filter.MinValue != "" {
		t.Error("MinValue should be empty by default")
	}
}

func TestEventMatcherGetWatchedAddressCount(t *testing.T) {
	matcher := NewEventMatcher()

	// Empty
	if matcher.GetWatchedAddressCount("chain-1") != 0 {
		t.Error("empty matcher should return 0")
	}

	// Add addresses
	for i := 0; i < 5; i++ {
		watched := &WatchedAddress{
			ID:      "addr-" + string(rune('a'+i)),
			Address: common.BigToAddress(big.NewInt(int64(i + 1))),
			ChainID: "chain-1",
			Filter:  DefaultWatchFilter(),
		}
		matcher.AddAddress(watched)
	}

	if matcher.GetWatchedAddressCount("chain-1") != 5 {
		t.Errorf("expected 5, got %d", matcher.GetWatchedAddressCount("chain-1"))
	}

	// Different chain
	if matcher.GetWatchedAddressCount("chain-2") != 0 {
		t.Error("should have 0 for chain-2")
	}
}

func TestEventTopicSignatures(t *testing.T) {
	// ERC20 Transfer event signature
	expectedSig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	if ERC20TransferTopic != expectedSig {
		t.Errorf("ERC20TransferTopic mismatch: got %s, expected %s", ERC20TransferTopic.Hex(), expectedSig.Hex())
	}

	if ERC721TransferTopic != expectedSig {
		t.Errorf("ERC721TransferTopic mismatch: got %s, expected %s", ERC721TransferTopic.Hex(), expectedSig.Hex())
	}
}
