package jsonrpc

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"go.uber.org/zap"
)

// mockHistoricalStorage extends mockStorage with historical query support
type mockHistoricalStorage struct {
	*mockStorage
	blocksByTime     []*types.Block
	blockByTimestamp *types.Block
	txsWithReceipts  []*storage.TransactionWithReceipt
	balance          *big.Int
	balanceHistory   []storage.BalanceSnapshot
	blockCount       uint64
	txCount          uint64
}

func (m *mockHistoricalStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	if m.blocksByTime != nil {
		start := offset
		end := offset + limit
		if start >= len(m.blocksByTime) {
			return []*types.Block{}, nil
		}
		if end > len(m.blocksByTime) {
			end = len(m.blocksByTime)
		}
		return m.blocksByTime[start:end], nil
	}
	return []*types.Block{}, nil
}

func (m *mockHistoricalStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	if m.blockByTimestamp != nil {
		return m.blockByTimestamp, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockHistoricalStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *storage.TransactionFilter, limit, offset int) ([]*storage.TransactionWithReceipt, error) {
	if m.txsWithReceipts != nil {
		start := offset
		end := offset + limit
		if start >= len(m.txsWithReceipts) {
			return []*storage.TransactionWithReceipt{}, nil
		}
		if end > len(m.txsWithReceipts) {
			end = len(m.txsWithReceipts)
		}
		return m.txsWithReceipts[start:end], nil
	}
	return []*storage.TransactionWithReceipt{}, nil
}

func (m *mockHistoricalStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	if m.balance != nil {
		return m.balance, nil
	}
	return big.NewInt(0), nil
}

func (m *mockHistoricalStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]storage.BalanceSnapshot, error) {
	if m.balanceHistory != nil {
		start := offset
		end := offset + limit
		if start >= len(m.balanceHistory) {
			return []storage.BalanceSnapshot{}, nil
		}
		if end > len(m.balanceHistory) {
			end = len(m.balanceHistory)
		}
		return m.balanceHistory[start:end], nil
	}
	return []storage.BalanceSnapshot{}, nil
}

func (m *mockHistoricalStorage) GetBlockCount(ctx context.Context) (uint64, error) {
	return m.blockCount, nil
}

func (m *mockHistoricalStorage) GetTransactionCount(ctx context.Context) (uint64, error) {
	return m.txCount, nil
}

func (m *mockHistoricalStorage) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.MinerStats, error) {
	return []storage.MinerStats{}, nil
}

func (m *mockHistoricalStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]storage.TokenBalance, error) {
	return []storage.TokenBalance{}, nil
}

func (m *mockHistoricalStorage) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*storage.GasStats, error) {
	return &storage.GasStats{}, nil
}

func (m *mockHistoricalStorage) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*storage.AddressGasStats, error) {
	return &storage.AddressGasStats{}, nil
}

func (m *mockHistoricalStorage) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.AddressGasStats, error) {
	return []storage.AddressGasStats{}, nil
}

func (m *mockHistoricalStorage) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.AddressActivityStats, error) {
	return []storage.AddressActivityStats{}, nil
}

func (m *mockHistoricalStorage) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*storage.NetworkMetrics, error) {
	return &storage.NetworkMetrics{}, nil
}

// Contract verification methods
func (m *mockHistoricalStorage) GetContractVerification(ctx context.Context, address common.Address) (*storage.ContractVerification, error) {
	return nil, storage.ErrNotFound
}

func (m *mockHistoricalStorage) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	return false, nil
}

func (m *mockHistoricalStorage) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error) {
	return []common.Address{}, nil
}

func (m *mockHistoricalStorage) CountVerifiedContracts(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockHistoricalStorage) SetContractVerification(ctx context.Context, verification *storage.ContractVerification) error {
	return nil
}

func (m *mockHistoricalStorage) DeleteContractVerification(ctx context.Context, address common.Address) error {
	return nil
}

func TestHistoricalJSONRPCMethods(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	// Create test blocks
	header1 := &types.Header{
		Number:     big.NewInt(1),
		ParentHash: common.HexToHash("0x123"),
		Time:       1000,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	block1 := types.NewBlock(header1, nil, nil, nil, trie.NewStackTrie(nil))

	header2 := &types.Header{
		Number:     big.NewInt(2),
		ParentHash: block1.Hash(),
		Time:       2000,
		GasLimit:   8000000,
		GasUsed:    6000000,
	}
	block2 := types.NewBlock(header2, nil, nil, nil, trie.NewStackTrie(nil))

	// Create test transaction
	testTx := types.NewTransaction(
		0,
		common.HexToAddress("0x456"),
		big.NewInt(1000),
		21000,
		big.NewInt(1),
		nil,
	)

	testReceipt := &types.Receipt{
		TxHash:            testTx.Hash(),
		Status:            1,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		Logs:              []*types.Log{},
		BlockNumber:       big.NewInt(1),
		BlockHash:         block1.Hash(),
		EffectiveGasPrice: big.NewInt(1),
	}

	t.Run("GetBlocksByTimeRange_Success", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{
				latestHeight: 100,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
			blocksByTime: []*types.Block{block1, block2},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"fromTime": 1000, "toTime": 2000}`)
		result, err := server.HandleMethodDirect(ctx, "getBlocksByTimeRange", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		nodes, ok := resultMap["nodes"].([]interface{})
		if !ok {
			t.Fatal("expected nodes to be array")
		}
		if len(nodes) != 2 {
			t.Errorf("expected 2 blocks, got %d", len(nodes))
		}

		totalCount, ok := resultMap["totalCount"].(int)
		if !ok || totalCount != 2 {
			t.Errorf("expected totalCount 2, got %v", resultMap["totalCount"])
		}
	})

	t.Run("GetBlocksByTimeRange_WithPagination", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{
				latestHeight: 100,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
			blocksByTime: []*types.Block{block1, block2},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"fromTime": "0x3e8", "toTime": "0x7d0", "limit": 1, "offset": 1}`)
		result, err := server.HandleMethodDirect(ctx, "getBlocksByTimeRange", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		nodes, ok := resultMap["nodes"].([]interface{})
		if !ok {
			t.Fatal("expected nodes to be array")
		}
		if len(nodes) != 1 {
			t.Errorf("expected 1 block with limit=1, got %d", len(nodes))
		}
	})

	t.Run("GetBlocksByTimeRange_InvalidParams", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getBlocksByTimeRange", params)
		if err == nil {
			t.Error("expected error for missing parameters")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetBlocksByTimeRange_InvalidTimeFormat", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"fromTime": "invalid", "toTime": 2000}`)
		_, err := server.HandleMethodDirect(ctx, "getBlocksByTimeRange", params)
		if err == nil {
			t.Error("expected error for invalid time format")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetBlockByTimestamp_Success", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{
				latestHeight: 100,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
			blockByTimestamp: block1,
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"timestamp": 1000}`)
		result, err := server.HandleMethodDirect(ctx, "getBlockByTimestamp", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}
	})

	t.Run("GetBlockByTimestamp_StringFormat", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage:      &mockStorage{},
			blockByTimestamp: block1,
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"timestamp": "0x3e8"}`)
		result, err := server.HandleMethodDirect(ctx, "getBlockByTimestamp", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}
	})

	t.Run("GetBlockByTimestamp_InvalidParams", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getBlockByTimestamp", params)
		if err == nil {
			t.Error("expected error for missing timestamp")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetBlockByTimestamp_NotFound", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage:      &mockStorage{},
			blockByTimestamp: nil,
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"timestamp": 9999}`)
		_, err := server.HandleMethodDirect(ctx, "getBlockByTimestamp", params)
		if err == nil {
			t.Error("expected error when block not found")
		}
	})

	t.Run("GetTransactionsByAddressFiltered_Success", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
			txsWithReceipts: []*storage.TransactionWithReceipt{
				{
					Transaction: testTx,
					Location: &storage.TxLocation{
						BlockHeight: 1,
						BlockHash:   block1.Hash(),
						TxIndex:     0,
					},
					Receipt: testReceipt,
				},
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{
			"address": "0x456",
			"filter": {"fromBlock": 0, "toBlock": 100}
		}`)
		result, err := server.HandleMethodDirect(ctx, "getTransactionsByAddressFiltered", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		nodes, ok := resultMap["nodes"].([]interface{})
		if !ok {
			t.Fatal("expected nodes to be array")
		}
		if len(nodes) != 1 {
			t.Errorf("expected 1 transaction, got %d", len(nodes))
		}
	})

	t.Run("GetTransactionsByAddressFiltered_WithComplexFilter", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage:     &mockStorage{},
			txsWithReceipts: []*storage.TransactionWithReceipt{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{
			"address": "0x456",
			"filter": {
				"fromBlock": "0x0",
				"toBlock": "0x64",
				"minValue": "0x64",
				"maxValue": "0x3e8",
				"txType": 1,
				"successOnly": true
			}
		}`)
		result, err := server.HandleMethodDirect(ctx, "getTransactionsByAddressFiltered", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}
	})

	t.Run("GetTransactionsByAddressFiltered_InvalidParams", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getTransactionsByAddressFiltered", params)
		if err == nil {
			t.Error("expected error for missing parameters")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetTransactionsByAddressFiltered_MissingFilter", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0x456"}`)
		_, err := server.HandleMethodDirect(ctx, "getTransactionsByAddressFiltered", params)
		if err == nil {
			t.Error("expected error for missing filter")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetAddressBalance_Success", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
			balance:     big.NewInt(123456789),
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0x456"}`)
		result, err := server.HandleMethodDirect(ctx, "getAddressBalance", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		balance, ok := resultMap["balance"].(string)
		if !ok {
			t.Fatal("expected balance to be string")
		}
		if balance != "0x75bcd15" {
			t.Errorf("expected balance 0x75bcd15, got %s", balance)
		}
	})

	t.Run("GetAddressBalance_WithBlockNumber", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
			balance:     big.NewInt(1000),
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0x456", "blockNumber": 100}`)
		result, err := server.HandleMethodDirect(ctx, "getAddressBalance", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}
	})

	t.Run("GetAddressBalance_InvalidParams", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getAddressBalance", params)
		if err == nil {
			t.Error("expected error for missing address")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetAddressBalance_InvalidBlockNumber", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0x456", "blockNumber": "invalid"}`)
		_, err := server.HandleMethodDirect(ctx, "getAddressBalance", params)
		if err == nil {
			t.Error("expected error for invalid block number")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetBalanceHistory_Success", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
			balanceHistory: []storage.BalanceSnapshot{
				{
					BlockNumber: 1,
					Balance:     big.NewInt(1000),
					Delta:       big.NewInt(1000),
					TxHash:      testTx.Hash(),
				},
				{
					BlockNumber: 2,
					Balance:     big.NewInt(2000),
					Delta:       big.NewInt(1000),
					TxHash:      testTx.Hash(),
				},
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0x456", "fromBlock": 0, "toBlock": 100}`)
		result, err := server.HandleMethodDirect(ctx, "getBalanceHistory", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		nodes, ok := resultMap["nodes"].([]interface{})
		if !ok {
			t.Fatal("expected nodes to be array")
		}
		if len(nodes) != 2 {
			t.Errorf("expected 2 snapshots, got %d", len(nodes))
		}
	})

	t.Run("GetBalanceHistory_NegativeDelta", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
			balanceHistory: []storage.BalanceSnapshot{
				{
					BlockNumber: 1,
					Balance:     big.NewInt(500),
					Delta:       big.NewInt(-500),
					TxHash:      testTx.Hash(),
				},
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0x456", "fromBlock": 0, "toBlock": 100}`)
		result, err := server.HandleMethodDirect(ctx, "getBalanceHistory", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		nodes, ok := resultMap["nodes"].([]interface{})
		if !ok || len(nodes) == 0 {
			t.Fatal("expected at least one node")
		}

		firstNode := nodes[0].(map[string]interface{})
		delta, ok := firstNode["delta"].(string)
		if !ok {
			t.Fatal("expected delta to be string")
		}
		// Should be negative delta
		if delta[0] != '-' {
			t.Errorf("expected negative delta, got %s", delta)
		}
	})

	t.Run("GetBalanceHistory_NilTxHash", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
			balanceHistory: []storage.BalanceSnapshot{
				{
					BlockNumber: 1,
					Balance:     big.NewInt(1000),
					Delta:       big.NewInt(1000),
					TxHash:      common.Hash{}, // zero hash
				},
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0x456", "fromBlock": 0, "toBlock": 100}`)
		result, err := server.HandleMethodDirect(ctx, "getBalanceHistory", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		nodes, ok := resultMap["nodes"].([]interface{})
		if !ok || len(nodes) == 0 {
			t.Fatal("expected at least one node")
		}

		firstNode := nodes[0].(map[string]interface{})
		txHash := firstNode["transactionHash"]
		if txHash != nil {
			t.Errorf("expected nil transactionHash for zero hash, got %v", txHash)
		}
	})

	t.Run("GetBalanceHistory_InvalidParams", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getBalanceHistory", params)
		if err == nil {
			t.Error("expected error for missing parameters")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetBalanceHistory_MissingBlocks", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0x456"}`)
		_, err := server.HandleMethodDirect(ctx, "getBalanceHistory", params)
		if err == nil {
			t.Error("expected error for missing block parameters")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetBlockCount_Success", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
			blockCount:  12345,
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{}`)
		result, err := server.HandleMethodDirect(ctx, "getBlockCount", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		count, ok := resultMap["count"].(string)
		if !ok {
			t.Fatal("expected count to be string")
		}
		if count != "0x3039" {
			t.Errorf("expected count 0x3039, got %s", count)
		}
	})

	t.Run("GetTransactionCount_Success", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{},
			txCount:     67890,
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{}`)
		result, err := server.HandleMethodDirect(ctx, "getTransactionCount", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		count, ok := resultMap["count"].(string)
		if !ok {
			t.Fatal("expected count to be string")
		}
		if count != "0x10932" {
			t.Errorf("expected count 0x10932, got %s", count)
		}
	})

	// Test storage that doesn't support historical queries
	t.Run("HistoricalQueries_NotSupported", func(t *testing.T) {
		store := &mockStorage{
			latestHeight: 100,
			blocks:       make(map[uint64]*types.Block),
			blocksByHash: make(map[common.Hash]*types.Block),
		}

		server := NewServer(store, logger)

		testCases := []struct {
			name   string
			method string
			params json.RawMessage
		}{
			{"GetBlocksByTimeRange", "getBlocksByTimeRange", json.RawMessage(`{"fromTime": 1000, "toTime": 2000}`)},
			{"GetBlockByTimestamp", "getBlockByTimestamp", json.RawMessage(`{"timestamp": 1000}`)},
			{"GetTransactionsByAddressFiltered", "getTransactionsByAddressFiltered", json.RawMessage(`{"address": "0x456", "filter": {"fromBlock": 0, "toBlock": 100}}`)},
			{"GetAddressBalance", "getAddressBalance", json.RawMessage(`{"address": "0x456"}`)},
			{"GetBalanceHistory", "getBalanceHistory", json.RawMessage(`{"address": "0x456", "fromBlock": 0, "toBlock": 100}`)},
			{"GetBlockCount", "getBlockCount", json.RawMessage(`{}`)},
			{"GetTransactionCount", "getTransactionCount", json.RawMessage(`{}`)},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := server.HandleMethodDirect(ctx, tc.method, tc.params)
				if err == nil {
					t.Error("expected error for unsupported storage")
				}
				if err.Code != InternalError {
					t.Errorf("expected InternalError, got %v", err.Code)
				}
			})
		}
	})
}

func TestParseTransactionFilter(t *testing.T) {
	t.Run("ValidFilter_AllFields", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock":   float64(0),
			"toBlock":     float64(100),
			"minValue":    "0x64",
			"maxValue":    "0x3e8",
			"txType":      float64(1),
			"successOnly": true,
		}

		result, err := parseTransactionFilter(filter)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result.FromBlock != 0 {
			t.Errorf("expected FromBlock 0, got %d", result.FromBlock)
		}
		if result.ToBlock != 100 {
			t.Errorf("expected ToBlock 100, got %d", result.ToBlock)
		}
		if result.MinValue.Cmp(big.NewInt(100)) != 0 {
			t.Errorf("expected MinValue 100, got %v", result.MinValue)
		}
		if result.MaxValue.Cmp(big.NewInt(1000)) != 0 {
			t.Errorf("expected MaxValue 1000, got %v", result.MaxValue)
		}
		if result.TxType != storage.TransactionType(1) {
			t.Errorf("expected TxType 1, got %v", result.TxType)
		}
		if !result.SuccessOnly {
			t.Error("expected SuccessOnly true")
		}
	})

	t.Run("ValidFilter_RequiredOnly", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": "0x0",
			"toBlock":   "0x64",
		}

		result, err := parseTransactionFilter(filter)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result.FromBlock != 0 {
			t.Errorf("expected FromBlock 0, got %d", result.FromBlock)
		}
		if result.ToBlock != 100 {
			t.Errorf("expected ToBlock 100, got %d", result.ToBlock)
		}
		if result.TxType != storage.TxTypeAll {
			t.Errorf("expected default TxType, got %v", result.TxType)
		}
		if result.SuccessOnly {
			t.Error("expected SuccessOnly false")
		}
	})

	t.Run("MissingFromBlock", func(t *testing.T) {
		filter := map[string]interface{}{
			"toBlock": float64(100),
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for missing fromBlock")
		}
	})

	t.Run("MissingToBlock", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": float64(0),
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for missing toBlock")
		}
	})

	t.Run("InvalidFromBlockType", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": true,
			"toBlock":   float64(100),
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for invalid fromBlock type")
		}
	})

	t.Run("InvalidToBlockType", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": float64(0),
			"toBlock":   true,
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for invalid toBlock type")
		}
	})

	t.Run("InvalidMinValueFormat", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": float64(0),
			"toBlock":   float64(100),
			"minValue":  "invalid",
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for invalid minValue format")
		}
	})

	t.Run("InvalidMaxValueFormat", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": float64(0),
			"toBlock":   float64(100),
			"maxValue":  "invalid",
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for invalid maxValue format")
		}
	})

	t.Run("InvalidMinValueType", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": float64(0),
			"toBlock":   float64(100),
			"minValue":  float64(100),
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for invalid minValue type")
		}
	})

	t.Run("InvalidMaxValueType", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": float64(0),
			"toBlock":   float64(100),
			"maxValue":  float64(1000),
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for invalid maxValue type")
		}
	})

	t.Run("InvalidTxTypeType", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": float64(0),
			"toBlock":   float64(100),
			"txType":    "invalid",
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for invalid txType type")
		}
	})

	t.Run("InvalidSuccessOnlyType", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock":   float64(0),
			"toBlock":     float64(100),
			"successOnly": "invalid",
		}

		_, err := parseTransactionFilter(filter)
		if err == nil {
			t.Error("expected error for invalid successOnly type")
		}
	})

	t.Run("StringNumberFormats", func(t *testing.T) {
		filter := map[string]interface{}{
			"fromBlock": "100",
			"toBlock":   "200",
		}

		result, err := parseTransactionFilter(filter)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result.FromBlock != 100 {
			t.Errorf("expected FromBlock 100, got %d", result.FromBlock)
		}
		if result.ToBlock != 200 {
			t.Errorf("expected ToBlock 200, got %d", result.ToBlock)
		}
	})
}
