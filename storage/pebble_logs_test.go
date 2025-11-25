package storage

import (
	"context"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Test helper to create a test log
func createTestLog(blockNumber uint64, txIndex uint, logIndex uint, address common.Address, topics []common.Hash, data []byte) *types.Log {
	return &types.Log{
		Address:     address,
		Topics:      topics,
		Data:        data,
		BlockNumber: blockNumber,
		TxHash:      common.Hash{byte(txIndex)},
		TxIndex:     txIndex,
		BlockHash:   common.Hash{byte(blockNumber)},
		Index:       logIndex,
		Removed:     false,
	}
}

func TestPebbleStorage_IndexLog(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	topic0 := common.HexToHash("0xabcd")
	topic1 := common.HexToHash("0xef12")
	topics := []common.Hash{topic0, topic1}
	data := []byte{1, 2, 3, 4}

	log := createTestLog(100, 0, 0, addr, topics, data)

	// Index the log
	err := storage.IndexLog(ctx, log)
	if err != nil {
		t.Fatalf("IndexLog() error = %v", err)
	}

	// Verify log was indexed by retrieving it
	logs, err := storage.GetLogsByBlock(ctx, 100)
	if err != nil {
		t.Fatalf("GetLogsByBlock() error = %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("GetLogsByBlock() returned %d logs, want 1", len(logs))
	}

	if logs[0].Address != addr {
		t.Errorf("Log address = %v, want %v", logs[0].Address, addr)
	}
	if len(logs[0].Topics) != 2 {
		t.Errorf("Log topics count = %d, want 2", len(logs[0].Topics))
	}
	if logs[0].Topics[0] != topic0 {
		t.Errorf("Log topic0 = %v, want %v", logs[0].Topics[0], topic0)
	}
}

func TestPebbleStorage_IndexLogs(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	topic0 := common.HexToHash("0xaaaa")

	logs := []*types.Log{
		createTestLog(100, 0, 0, addr1, []common.Hash{topic0}, []byte{1}),
		createTestLog(100, 0, 1, addr1, []common.Hash{topic0}, []byte{2}),
		createTestLog(100, 1, 0, addr2, []common.Hash{topic0}, []byte{3}),
	}

	// Index multiple logs
	err := storage.IndexLogs(ctx, logs)
	if err != nil {
		t.Fatalf("IndexLogs() error = %v", err)
	}

	// Verify all logs were indexed
	retrievedLogs, err := storage.GetLogsByBlock(ctx, 100)
	if err != nil {
		t.Fatalf("GetLogsByBlock() error = %v", err)
	}

	if len(retrievedLogs) != 3 {
		t.Fatalf("GetLogsByBlock() returned %d logs, want 3", len(retrievedLogs))
	}
}

func TestPebbleStorage_IndexLog_Errors(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("nil log", func(t *testing.T) {
		err := storage.IndexLog(ctx, nil)
		if err == nil {
			t.Error("IndexLog(nil) should return error")
		}
	})

	t.Run("closed storage", func(t *testing.T) {
		storage.Close()
		log := createTestLog(100, 0, 0, common.Address{}, []common.Hash{}, []byte{})
		err := storage.IndexLog(ctx, log)
		if err != ErrClosed {
			t.Errorf("IndexLog() on closed storage error = %v, want ErrClosed", err)
		}
	})
}

func TestPebbleStorage_GetLogsByBlock(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	topic := common.HexToHash("0xabcd")

	// Index logs in different blocks
	log1 := createTestLog(100, 0, 0, addr, []common.Hash{topic}, []byte{1})
	log2 := createTestLog(100, 0, 1, addr, []common.Hash{topic}, []byte{2})
	log3 := createTestLog(101, 0, 0, addr, []common.Hash{topic}, []byte{3})

	storage.IndexLog(ctx, log1)
	storage.IndexLog(ctx, log2)
	storage.IndexLog(ctx, log3)

	// Get logs from block 100
	logs, err := storage.GetLogsByBlock(ctx, 100)
	if err != nil {
		t.Fatalf("GetLogsByBlock() error = %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("GetLogsByBlock(100) returned %d logs, want 2", len(logs))
	}

	// Get logs from block 101
	logs, err = storage.GetLogsByBlock(ctx, 101)
	if err != nil {
		t.Fatalf("GetLogsByBlock() error = %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("GetLogsByBlock(101) returned %d logs, want 1", len(logs))
	}

	// Get logs from non-existent block
	logs, err = storage.GetLogsByBlock(ctx, 999)
	if err != nil {
		t.Fatalf("GetLogsByBlock(999) error = %v", err)
	}

	if len(logs) != 0 {
		t.Fatalf("GetLogsByBlock(999) returned %d logs, want 0", len(logs))
	}
}

func TestPebbleStorage_GetLogsByAddress(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	topic := common.HexToHash("0xabcd")

	// Index logs from different addresses
	log1 := createTestLog(100, 0, 0, addr1, []common.Hash{topic}, []byte{1})
	log2 := createTestLog(101, 0, 0, addr1, []common.Hash{topic}, []byte{2})
	log3 := createTestLog(102, 0, 0, addr2, []common.Hash{topic}, []byte{3})

	storage.IndexLog(ctx, log1)
	storage.IndexLog(ctx, log2)
	storage.IndexLog(ctx, log3)

	// Get logs from addr1
	logs, err := storage.GetLogsByAddress(ctx, addr1, 100, 102)
	if err != nil {
		t.Fatalf("GetLogsByAddress() error = %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("GetLogsByAddress(addr1) returned %d logs, want 2", len(logs))
	}

	// Get logs from addr2
	logs, err = storage.GetLogsByAddress(ctx, addr2, 100, 102)
	if err != nil {
		t.Fatalf("GetLogsByAddress() error = %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("GetLogsByAddress(addr2) returned %d logs, want 1", len(logs))
	}

	// Test block range filtering
	logs, err = storage.GetLogsByAddress(ctx, addr1, 100, 100)
	if err != nil {
		t.Fatalf("GetLogsByAddress() error = %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("GetLogsByAddress(addr1, 100-100) returned %d logs, want 1", len(logs))
	}
}

func TestPebbleStorage_GetLogsByTopic(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	topic0 := common.HexToHash("0xaaaa")
	topic1 := common.HexToHash("0xbbbb")
	topic2 := common.HexToHash("0xcccc")

	// Index logs with different topics
	log1 := createTestLog(100, 0, 0, addr, []common.Hash{topic0, topic1}, []byte{1})
	log2 := createTestLog(101, 0, 0, addr, []common.Hash{topic0, topic2}, []byte{2})
	log3 := createTestLog(102, 0, 0, addr, []common.Hash{topic1, topic2}, []byte{3})

	storage.IndexLog(ctx, log1)
	storage.IndexLog(ctx, log2)
	storage.IndexLog(ctx, log3)

	// Get logs by topic0 at position 0
	logs, err := storage.GetLogsByTopic(ctx, topic0, 0, 100, 102)
	if err != nil {
		t.Fatalf("GetLogsByTopic() error = %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("GetLogsByTopic(topic0, pos0) returned %d logs, want 2", len(logs))
	}

	// Get logs by topic2 at position 1
	logs, err = storage.GetLogsByTopic(ctx, topic2, 1, 100, 102)
	if err != nil {
		t.Fatalf("GetLogsByTopic() error = %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("GetLogsByTopic(topic2, pos1) returned %d logs, want 2", len(logs))
	}

	// Test invalid topic index
	_, err = storage.GetLogsByTopic(ctx, topic0, 4, 100, 102)
	if err == nil {
		t.Error("GetLogsByTopic() with invalid topic index should return error")
	}

	_, err = storage.GetLogsByTopic(ctx, topic0, -1, 100, 102)
	if err == nil {
		t.Error("GetLogsByTopic() with negative topic index should return error")
	}
}

func TestPebbleStorage_GetLogs(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	topic0 := common.HexToHash("0xaaaa")
	topic1 := common.HexToHash("0xbbbb")
	topic2 := common.HexToHash("0xcccc")

	// Index diverse logs
	log1 := createTestLog(100, 0, 0, addr1, []common.Hash{topic0, topic1}, []byte{1})
	log2 := createTestLog(101, 0, 0, addr1, []common.Hash{topic0, topic2}, []byte{2})
	log3 := createTestLog(102, 0, 0, addr2, []common.Hash{topic1, topic2}, []byte{3})
	log4 := createTestLog(103, 0, 0, addr2, []common.Hash{topic0, topic1}, []byte{4})

	storage.IndexLog(ctx, log1)
	storage.IndexLog(ctx, log2)
	storage.IndexLog(ctx, log3)
	storage.IndexLog(ctx, log4)

	// Set latest height for filter
	storage.SetLatestHeight(ctx, 103)

	t.Run("filter by address", func(t *testing.T) {
		filter := &LogFilter{
			Addresses: []common.Address{addr1},
			FromBlock: 100,
			ToBlock:   103,
		}

		logs, err := storage.GetLogs(ctx, filter)
		if err != nil {
			t.Fatalf("GetLogs() error = %v", err)
		}

		if len(logs) != 2 {
			t.Fatalf("GetLogs(addr1) returned %d logs, want 2", len(logs))
		}
	})

	t.Run("filter by topic0", func(t *testing.T) {
		filter := &LogFilter{
			Topics:    [][]common.Hash{{topic0}},
			FromBlock: 100,
			ToBlock:   103,
		}

		logs, err := storage.GetLogs(ctx, filter)
		if err != nil {
			t.Fatalf("GetLogs() error = %v", err)
		}

		if len(logs) != 3 {
			t.Fatalf("GetLogs(topic0) returned %d logs, want 3", len(logs))
		}
	})

	t.Run("filter by address and topics", func(t *testing.T) {
		filter := &LogFilter{
			Addresses: []common.Address{addr1},
			Topics:    [][]common.Hash{{topic0}},
			FromBlock: 100,
			ToBlock:   103,
		}

		logs, err := storage.GetLogs(ctx, filter)
		if err != nil {
			t.Fatalf("GetLogs() error = %v", err)
		}

		if len(logs) != 2 {
			t.Fatalf("GetLogs(addr1, topic0) returned %d logs, want 2", len(logs))
		}
	})

	t.Run("filter by block range", func(t *testing.T) {
		filter := &LogFilter{
			FromBlock: 100,
			ToBlock:   101,
		}

		logs, err := storage.GetLogs(ctx, filter)
		if err != nil {
			t.Fatalf("GetLogs() error = %v", err)
		}

		if len(logs) != 2 {
			t.Fatalf("GetLogs(100-101) returned %d logs, want 2", len(logs))
		}
	})

	t.Run("filter with multiple topic options", func(t *testing.T) {
		filter := &LogFilter{
			Topics:    [][]common.Hash{{topic0, topic1}}, // Either topic0 OR topic1
			FromBlock: 100,
			ToBlock:   103,
		}

		logs, err := storage.GetLogs(ctx, filter)
		if err != nil {
			t.Fatalf("GetLogs() error = %v", err)
		}

		// All logs should match (they all have either topic0 or topic1)
		if len(logs) != 4 {
			t.Fatalf("GetLogs(topic0 OR topic1) returned %d logs, want 4", len(logs))
		}
	})

	t.Run("nil filter", func(t *testing.T) {
		_, err := storage.GetLogs(ctx, nil)
		if err == nil {
			t.Error("GetLogs(nil) should return error")
		}
	})

	t.Run("invalid block range", func(t *testing.T) {
		filter := &LogFilter{
			FromBlock: 200,
			ToBlock:   100,
		}

		_, err := storage.GetLogs(ctx, filter)
		if err == nil {
			t.Error("GetLogs() with fromBlock > toBlock should return error")
		}
	})
}

func TestPebbleStorage_FilterLogsByTopics(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ps := storage.(*PebbleStorage)

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	topic0 := common.HexToHash("0xaaaa")
	topic1 := common.HexToHash("0xbbbb")
	topic2 := common.HexToHash("0xcccc")

	logs := []*types.Log{
		createTestLog(100, 0, 0, addr, []common.Hash{topic0, topic1}, []byte{1}),
		createTestLog(100, 0, 1, addr, []common.Hash{topic0, topic2}, []byte{2}),
		createTestLog(100, 0, 2, addr, []common.Hash{topic1, topic2}, []byte{3}),
	}

	t.Run("empty topic filter", func(t *testing.T) {
		filtered := ps.filterLogsByTopics(logs, [][]common.Hash{})
		if len(filtered) != 3 {
			t.Errorf("filterLogsByTopics(empty) returned %d logs, want 3", len(filtered))
		}
	})

	t.Run("single topic filter", func(t *testing.T) {
		topicFilter := [][]common.Hash{{topic0}}
		filtered := ps.filterLogsByTopics(logs, topicFilter)
		if len(filtered) != 2 {
			t.Errorf("filterLogsByTopics(topic0) returned %d logs, want 2", len(filtered))
		}
	})

	t.Run("multiple topic positions", func(t *testing.T) {
		// Match topic0 at position 0 AND topic2 at position 1
		topicFilter := [][]common.Hash{{topic0}, {topic2}}
		filtered := ps.filterLogsByTopics(logs, topicFilter)
		if len(filtered) != 1 {
			t.Errorf("filterLogsByTopics(topic0, topic2) returned %d logs, want 1", len(filtered))
		}
	})

	t.Run("wildcard topic position", func(t *testing.T) {
		// Match topic2 at position 1, any value at position 0
		topicFilter := [][]common.Hash{{}, {topic2}}
		filtered := ps.filterLogsByTopics(logs, topicFilter)
		if len(filtered) != 2 {
			t.Errorf("filterLogsByTopics(*, topic2) returned %d logs, want 2", len(filtered))
		}
	})
}

func TestPebbleStorage_MatchesTopicFilter(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ps := storage.(*PebbleStorage)

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	topic0 := common.HexToHash("0xaaaa")
	topic1 := common.HexToHash("0xbbbb")
	topic2 := common.HexToHash("0xcccc")

	log := createTestLog(100, 0, 0, addr, []common.Hash{topic0, topic1}, []byte{1})

	tests := []struct {
		name        string
		topicFilter [][]common.Hash
		want        bool
	}{
		{
			"empty filter matches",
			[][]common.Hash{},
			true,
		},
		{
			"exact match topic0",
			[][]common.Hash{{topic0}},
			true,
		},
		{
			"exact match topic0 and topic1",
			[][]common.Hash{{topic0}, {topic1}},
			true,
		},
		{
			"wildcard at position 0",
			[][]common.Hash{{}, {topic1}},
			true,
		},
		{
			"no match topic2",
			[][]common.Hash{{topic2}},
			false,
		},
		{
			"no match - wrong position",
			[][]common.Hash{{}, {}, {topic0}},
			false,
		},
		{
			"topic filter longer than log topics",
			[][]common.Hash{{topic0}, {topic1}, {topic2}},
			false,
		},
		{
			"multiple options - one matches",
			[][]common.Hash{{topic0, topic2}},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ps.matchesTopicFilter(log, tt.topicFilter)
			if got != tt.want {
				t.Errorf("matchesTopicFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPebbleStorage_LogIndexing_MultipleTopics(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	topic0 := common.HexToHash("0xaaaa")
	topic1 := common.HexToHash("0xbbbb")
	topic2 := common.HexToHash("0xcccc")
	topic3 := common.HexToHash("0xdddd")

	// Index log with all 4 topics
	log := createTestLog(100, 0, 0, addr, []common.Hash{topic0, topic1, topic2, topic3}, []byte{1, 2, 3, 4})

	err := storage.IndexLog(ctx, log)
	if err != nil {
		t.Fatalf("IndexLog() error = %v", err)
	}

	// Verify log can be retrieved by each topic at each position
	for i, topic := range []common.Hash{topic0, topic1, topic2, topic3} {
		logs, err := storage.GetLogsByTopic(ctx, topic, i, 100, 100)
		if err != nil {
			t.Fatalf("GetLogsByTopic(topic%d, pos%d) error = %v", i, i, err)
		}
		if len(logs) != 1 {
			t.Errorf("GetLogsByTopic(topic%d, pos%d) returned %d logs, want 1", i, i, len(logs))
		}
	}
}

func TestPebbleStorage_Logs_ReadOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-readonly-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create storage and add a log
	cfg := DefaultConfig(tmpDir)
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	ctx := context.Background()
	log := createTestLog(100, 0, 0, common.Address{}, []common.Hash{}, []byte{1})
	storage.IndexLog(ctx, log)
	storage.Close()

	// Reopen as read-only
	cfg.ReadOnly = true
	roStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create read-only storage: %v", err)
	}
	defer roStorage.Close()

	// Should be able to read
	logs, err := roStorage.GetLogsByBlock(ctx, 100)
	if err != nil {
		t.Fatalf("GetLogsByBlock() on read-only storage error = %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("GetLogsByBlock() returned %d logs, want 1", len(logs))
	}

	// Should not be able to write
	newLog := createTestLog(101, 0, 0, common.Address{}, []common.Hash{}, []byte{2})
	err = roStorage.IndexLog(ctx, newLog)
	if err != ErrReadOnly {
		t.Errorf("IndexLog() on read-only storage error = %v, want ErrReadOnly", err)
	}
}
