package watchlist

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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

func TestMatchTransactionNoBloomFilter(t *testing.T) {
	matcher := NewEventMatcher()

	// Create a simple transaction
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       ptrTo(common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")),
		Value:    big.NewInt(1000000),
		Data:     nil,
	})

	receipt := &types.Receipt{
		Status:           types.ReceiptStatusSuccessful,
		TransactionIndex: 0,
		GasUsed:          21000,
	}

	// No addresses registered - should return empty
	events := matcher.MatchTransaction(
		"chain-1",
		tx, receipt,
		100, common.Hash{}, 1234567890,
	)

	if len(events) != 0 {
		t.Errorf("expected 0 events without bloom filter, got %d", len(events))
	}
}

// Helper function to create address pointer
func ptrTo(addr common.Address) *common.Address {
	return &addr
}

func TestMatchTransactionTxFrom(t *testing.T) {
	matcher := NewEventMatcher()

	// Generate a key and address for the sender
	senderAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	// Register the sender address to watch
	watched := &WatchedAddress{
		ID:      "sender-1",
		Address: senderAddr,
		ChainID: "chain-1",
		Filter: &WatchFilter{
			TxFrom: true,
			TxTo:   false,
			ERC20:  false,
			ERC721: false,
			Logs:   false,
		},
	}
	matcher.AddAddress(watched)

	// Note: We can't fully test MatchTransaction without signing a tx
	// because go-ethereum requires a valid signature to recover sender.
	// The bloom filter check and address map lookup are tested here.

	// Verify the bloom filter contains the sender
	bf := matcher.GetBloomFilter("chain-1")
	if bf == nil {
		t.Fatal("bloom filter should be created")
	}

	if !bf.MightContain(senderAddr) {
		t.Error("bloom filter should contain sender address")
	}
}

func TestMatchTransactionTxTo(t *testing.T) {
	matcher := NewEventMatcher()

	// Register a recipient address
	recipientAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	watched := &WatchedAddress{
		ID:      "recipient-1",
		Address: recipientAddr,
		ChainID: "chain-1",
		Filter: &WatchFilter{
			TxFrom: false,
			TxTo:   true,
			ERC20:  false,
			ERC721: false,
			Logs:   false,
		},
	}
	matcher.AddAddress(watched)

	// Verify the bloom filter contains the recipient
	bf := matcher.GetBloomFilter("chain-1")
	if bf == nil {
		t.Fatal("bloom filter should be created")
	}

	if !bf.MightContain(recipientAddr) {
		t.Error("bloom filter should contain recipient address")
	}
}

func TestMatchLogsNoBloomFilter(t *testing.T) {
	matcher := NewEventMatcher()

	// No addresses registered
	events := matcher.MatchLogs(
		"chain-1",
		nil,
		100, common.Hash{}, 1234567890,
	)

	if len(events) != 0 {
		t.Errorf("expected 0 events without bloom filter, got %d", len(events))
	}
}

func TestMatchLogsEmptyLogs(t *testing.T) {
	matcher := NewEventMatcher()

	// Register an address
	watchedAddr := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	watched := &WatchedAddress{
		ID:      "addr-1",
		Address: watchedAddr,
		ChainID: "chain-1",
		Filter:  DefaultWatchFilter(),
	}
	matcher.AddAddress(watched)

	// Empty logs
	events := matcher.MatchLogs(
		"chain-1",
		[]*types.Log{},
		100, common.Hash{}, 1234567890,
	)

	if len(events) != 0 {
		t.Errorf("expected 0 events with empty logs, got %d", len(events))
	}
}

func TestMatchLogsContractLogs(t *testing.T) {
	matcher := NewEventMatcher()

	// Register a contract address with Logs filter enabled
	contractAddr := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	watched := &WatchedAddress{
		ID:      "contract-1",
		Address: contractAddr,
		ChainID: "chain-1",
		Filter: &WatchFilter{
			TxFrom: false,
			TxTo:   false,
			ERC20:  false,
			ERC721: false,
			Logs:   true, // Only watching contract logs
		},
	}
	matcher.AddAddress(watched)

	// Create a log from the watched contract
	logs := []*types.Log{
		{
			Address:     contractAddr,
			Topics:      []common.Hash{common.HexToHash("0x1234")},
			Data:        []byte("test data"),
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xabcd"),
			TxIndex:     0,
			Index:       0,
		},
	}

	events := matcher.MatchLogs(
		"chain-1",
		logs,
		100, common.HexToHash("0xblock"), 1234567890,
	)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].EventType != WatchEventTypeLog {
		t.Errorf("expected event type %s, got %s", WatchEventTypeLog, events[0].EventType)
	}

	if events[0].AddressID != "contract-1" {
		t.Errorf("expected address ID 'contract-1', got '%s'", events[0].AddressID)
	}
}

func TestMatchLogsERC20Transfer(t *testing.T) {
	matcher := NewEventMatcher()

	// Register sender and receiver addresses
	senderAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	receiverAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Watch the sender with ERC20 filter
	watchedSender := &WatchedAddress{
		ID:      "sender-1",
		Address: senderAddr,
		ChainID: "chain-1",
		Filter: &WatchFilter{
			TxFrom: false,
			TxTo:   false,
			ERC20:  true,
			ERC721: false,
			Logs:   false,
		},
	}
	matcher.AddAddress(watchedSender)

	// Watch the receiver with ERC20 filter
	watchedReceiver := &WatchedAddress{
		ID:      "receiver-1",
		Address: receiverAddr,
		ChainID: "chain-1",
		Filter: &WatchFilter{
			TxFrom: false,
			TxTo:   false,
			ERC20:  true,
			ERC721: false,
			Logs:   false,
		},
	}
	matcher.AddAddress(watchedReceiver)

	// Create an ERC20 Transfer log (3 topics: signature, from, to + data for amount)
	tokenAddr := common.HexToAddress("0xTokenTokenTokenTokenTokenTokenTokenToken")
	amount := big.NewInt(1000000)
	logs := []*types.Log{
		{
			Address: tokenAddr,
			Topics: []common.Hash{
				ERC20TransferTopic,
				common.BytesToHash(senderAddr.Bytes()),
				common.BytesToHash(receiverAddr.Bytes()),
			},
			Data:        common.LeftPadBytes(amount.Bytes(), 32),
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xabcd"),
			TxIndex:     0,
			Index:       0,
		},
	}

	events := matcher.MatchLogs(
		"chain-1",
		logs,
		100, common.HexToHash("0xblock"), 1234567890,
	)

	// Should match both sender and receiver
	if len(events) != 2 {
		t.Fatalf("expected 2 events (sender and receiver), got %d", len(events))
	}

	// Both should be ERC20 transfer events
	for _, event := range events {
		if event.EventType != WatchEventTypeERC20Transfer {
			t.Errorf("expected event type %s, got %s", WatchEventTypeERC20Transfer, event.EventType)
		}
	}
}

func TestMatchLogsERC721Transfer(t *testing.T) {
	matcher := NewEventMatcher()

	// Register receiver address with ERC721 filter
	receiverAddr := common.HexToAddress("0x3333333333333333333333333333333333333333")

	watchedReceiver := &WatchedAddress{
		ID:      "nft-receiver",
		Address: receiverAddr,
		ChainID: "chain-1",
		Filter: &WatchFilter{
			TxFrom: false,
			TxTo:   false,
			ERC20:  false,
			ERC721: true,
			Logs:   false,
		},
	}
	matcher.AddAddress(watchedReceiver)

	// Create an ERC721 Transfer log (4 topics: signature, from, to, tokenId)
	senderAddr := common.HexToAddress("0x4444444444444444444444444444444444444444")
	nftAddr := common.HexToAddress("0xNFTNFTNFTNFTNFTNFTNFTNFTNFTNFTNFTNFT")
	tokenID := big.NewInt(42)

	logs := []*types.Log{
		{
			Address: nftAddr,
			Topics: []common.Hash{
				ERC721TransferTopic,
				common.BytesToHash(senderAddr.Bytes()),
				common.BytesToHash(receiverAddr.Bytes()),
				common.BytesToHash(common.LeftPadBytes(tokenID.Bytes(), 32)),
			},
			Data:        []byte{},
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xabcd"),
			TxIndex:     0,
			Index:       0,
		},
	}

	events := matcher.MatchLogs(
		"chain-1",
		logs,
		100, common.HexToHash("0xblock"), 1234567890,
	)

	if len(events) != 1 {
		t.Fatalf("expected 1 event for NFT receiver, got %d", len(events))
	}

	event := events[0]
	if event.EventType != WatchEventTypeERC721Transfer {
		t.Errorf("expected event type %s, got %s", WatchEventTypeERC721Transfer, event.EventType)
	}

	if event.AddressID != "nft-receiver" {
		t.Errorf("expected address ID 'nft-receiver', got '%s'", event.AddressID)
	}

	if event.TokenID != "42" {
		t.Errorf("expected token ID '42', got '%s'", event.TokenID)
	}
}

func TestMatchLogsFilterDisabled(t *testing.T) {
	matcher := NewEventMatcher()

	// Register address but disable ERC20 filter
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	watched := &WatchedAddress{
		ID:      "addr-1",
		Address: addr,
		ChainID: "chain-1",
		Filter: &WatchFilter{
			TxFrom: false,
			TxTo:   false,
			ERC20:  false, // Disabled
			ERC721: false, // Disabled
			Logs:   false, // Disabled
		},
	}
	matcher.AddAddress(watched)

	// Create an ERC20 Transfer log with the watched address
	logs := []*types.Log{
		{
			Address: common.HexToAddress("0xToken"),
			Topics: []common.Hash{
				ERC20TransferTopic,
				common.BytesToHash(addr.Bytes()),
				common.BytesToHash(common.HexToAddress("0xReceiver").Bytes()),
			},
			Data:        common.LeftPadBytes(big.NewInt(1000).Bytes(), 32),
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xabcd"),
			TxIndex:     0,
			Index:       0,
		},
	}

	events := matcher.MatchLogs(
		"chain-1",
		logs,
		100, common.HexToHash("0xblock"), 1234567890,
	)

	// Should not match because ERC20 filter is disabled
	if len(events) != 0 {
		t.Errorf("expected 0 events with ERC20 filter disabled, got %d", len(events))
	}
}

func TestMatchLogsWrongChain(t *testing.T) {
	matcher := NewEventMatcher()

	// Register address on chain-1
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	watched := &WatchedAddress{
		ID:      "addr-1",
		Address: addr,
		ChainID: "chain-1",
		Filter:  DefaultWatchFilter(),
	}
	matcher.AddAddress(watched)

	// Try to match on chain-2
	logs := []*types.Log{
		{
			Address: addr,
			Topics:  []common.Hash{common.HexToHash("0x1234")},
			Data:    []byte("test"),
		},
	}

	events := matcher.MatchLogs(
		"chain-2", // Different chain
		logs,
		100, common.HexToHash("0xblock"), 1234567890,
	)

	if len(events) != 0 {
		t.Errorf("expected 0 events for wrong chain, got %d", len(events))
	}
}

func TestMatchLogsNonTransferTopics(t *testing.T) {
	matcher := NewEventMatcher()

	// Register address
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	watched := &WatchedAddress{
		ID:      "addr-1",
		Address: addr,
		ChainID: "chain-1",
		Filter: &WatchFilter{
			TxFrom: false,
			TxTo:   false,
			ERC20:  true,
			ERC721: true,
			Logs:   false,
		},
	}
	matcher.AddAddress(watched)

	// Create a log with a different event signature (not ERC20/721 Transfer)
	logs := []*types.Log{
		{
			Address: common.HexToAddress("0xToken"),
			Topics: []common.Hash{
				common.HexToHash("0xOtherEventSignature"),
				common.BytesToHash(addr.Bytes()),
			},
			Data:        []byte{},
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xabcd"),
			TxIndex:     0,
			Index:       0,
		},
	}

	events := matcher.MatchLogs(
		"chain-1",
		logs,
		100, common.HexToHash("0xblock"), 1234567890,
	)

	// Should not match (not a Transfer event and Logs filter is disabled)
	if len(events) != 0 {
		t.Errorf("expected 0 events for non-transfer log, got %d", len(events))
	}
}

func TestMatchLogsInsufficientTopics(t *testing.T) {
	matcher := NewEventMatcher()

	// Register address
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	watched := &WatchedAddress{
		ID:      "addr-1",
		Address: addr,
		ChainID: "chain-1",
		Filter:  DefaultWatchFilter(),
	}
	matcher.AddAddress(watched)

	// Create a log with Transfer topic but insufficient topic count
	logs := []*types.Log{
		{
			Address: common.HexToAddress("0xToken"),
			Topics: []common.Hash{
				ERC20TransferTopic,
				// Missing from and to topics
			},
			Data:        []byte{},
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xabcd"),
			TxIndex:     0,
			Index:       0,
		},
	}

	events := matcher.MatchLogs(
		"chain-1",
		logs,
		100, common.HexToHash("0xblock"), 1234567890,
	)

	// Should not match (insufficient topics for Transfer event)
	if len(events) != 0 {
		t.Errorf("expected 0 events for insufficient topics, got %d", len(events))
	}
}
