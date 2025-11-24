package graphql

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"go.uber.org/zap"
)

// mockHistoricalStorage extends mockStorage with historical methods
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
	if m.blocksByTime == nil {
		return []*types.Block{}, nil
	}
	return m.blocksByTime, nil
}

func (m *mockHistoricalStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	if m.blockByTimestamp == nil {
		return nil, storage.ErrNotFound
	}
	return m.blockByTimestamp, nil
}

func (m *mockHistoricalStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *storage.TransactionFilter, limit, offset int) ([]*storage.TransactionWithReceipt, error) {
	if m.txsWithReceipts == nil {
		return []*storage.TransactionWithReceipt{}, nil
	}
	return m.txsWithReceipts, nil
}

func (m *mockHistoricalStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	if m.balance == nil {
		return big.NewInt(0), nil
	}
	return m.balance, nil
}

func (m *mockHistoricalStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]storage.BalanceSnapshot, error) {
	if m.balanceHistory == nil {
		return []storage.BalanceSnapshot{}, nil
	}
	return m.balanceHistory, nil
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

func TestHistoricalResolvers(t *testing.T) {
	logger := zap.NewNop()

	// Create test blocks
	header1 := &types.Header{
		Number:   big.NewInt(1),
		Time:     1000,
		GasLimit: 8000000,
		GasUsed:  5000000,
	}
	block1 := types.NewBlock(header1, nil, nil, nil, trie.NewStackTrie(nil))

	header2 := &types.Header{
		Number:   big.NewInt(2),
		Time:     2000,
		GasLimit: 8000000,
		GasUsed:  6000000,
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

	store := &mockHistoricalStorage{
		mockStorage: &mockStorage{
			latestHeight: 100,
			blocks:       map[uint64]*types.Block{1: block1, 2: block2},
			blocksByHash: map[common.Hash]*types.Block{block1.Hash(): block1, block2.Hash(): block2},
			transactions: make(map[common.Hash]*types.Transaction),
			receipts:     make(map[common.Hash]*types.Receipt),
		},
		blocksByTime:     []*types.Block{block1, block2},
		blockByTimestamp: block1,
		txsWithReceipts: []*storage.TransactionWithReceipt{
			{
				Transaction: testTx,
				Receipt:     testReceipt,
				Location: &storage.TxLocation{
					BlockHeight: 1,
					BlockHash:   block1.Hash(),
					TxIndex:     0,
				},
			},
		},
		balance: big.NewInt(1000000),
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
		blockCount: 100,
		txCount:    500,
	}

	handler, err := NewHandler(store, logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	t.Run("ResolveBlocksByTimeRange_Success", func(t *testing.T) {
		query := `{
			blocksByTimeRange(fromTime: "1000", toTime: "2000") {
				nodes {
					number
					timestamp
				}
				totalCount
				pageInfo {
					hasNextPage
					hasPreviousPage
				}
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ResolveBlocksByTimeRange_WithPagination", func(t *testing.T) {
		query := `{
			blocksByTimeRange(
				fromTime: "1000"
				toTime: "2000"
				pagination: {limit: 1, offset: 0}
			) {
				nodes {
					number
				}
				totalCount
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveBlockByTimestamp_Success", func(t *testing.T) {
		query := `{
			blockByTimestamp(timestamp: "1000") {
				number
				timestamp
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ResolveTransactionsByAddressFiltered_Success", func(t *testing.T) {
		query := `{
			transactionsByAddressFiltered(
				address: "0x456"
				filter: {
					fromBlock: "0"
					toBlock: "100"
					minValue: "0"
					txType: 0
					successOnly: true
				}
			) {
				nodes {
					hash
					value
					receipt {
						status
					}
				}
				totalCount
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ResolveTransactionsByAddressFiltered_WithAllFilters", func(t *testing.T) {
		query := `{
			transactionsByAddressFiltered(
				address: "0x456"
				filter: {
					fromBlock: "1"
					toBlock: "10"
					minValue: "100"
					maxValue: "10000"
					txType: 1
					successOnly: false
				}
				pagination: {limit: 10, offset: 0}
			) {
				nodes {
					hash
				}
				totalCount
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveAddressBalance_Latest", func(t *testing.T) {
		query := `{
			addressBalance(address: "0x456")
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ResolveAddressBalance_AtBlock", func(t *testing.T) {
		query := `{
			addressBalance(address: "0x456", blockNumber: "100")
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveBalanceHistory_Success", func(t *testing.T) {
		query := `{
			balanceHistory(
				address: "0x456"
				fromBlock: "0"
				toBlock: "100"
			) {
				nodes {
					blockNumber
					balance
					delta
					transactionHash
				}
				totalCount
				pageInfo {
					hasNextPage
				}
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ResolveBalanceHistory_WithPagination", func(t *testing.T) {
		query := `{
			balanceHistory(
				address: "0x456"
				fromBlock: "0"
				toBlock: "100"
				pagination: {limit: 5, offset: 1}
			) {
				nodes {
					blockNumber
					balance
				}
				totalCount
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveBlockCount_Success", func(t *testing.T) {
		query := `{ blockCount }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ResolveTransactionCount_Success", func(t *testing.T) {
		query := `{ transactionCount }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
		if result.Data == nil {
			t.Error("expected data in result")
		}
	})
}

func TestHistoricalResolversErrorPaths(t *testing.T) {
	logger := zap.NewNop()

	t.Run("BlocksByTimeRange_InvalidTime", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{
				latestHeight: 100,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
		}

		handler, err := NewHandler(store, logger)
		if err != nil {
			t.Fatalf("failed to create handler: %v", err)
		}

		query := `{
			blocksByTimeRange(fromTime: "invalid", toTime: "2000") {
				nodes { number }
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid fromTime")
		}
	})

	t.Run("BlockByTimestamp_InvalidTimestamp", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{
				latestHeight: 100,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
		}

		handler, err := NewHandler(store, logger)
		if err != nil {
			t.Fatalf("failed to create handler: %v", err)
		}

		query := `{ blockByTimestamp(timestamp: "invalid") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid timestamp")
		}
	})

	t.Run("TransactionsByAddressFiltered_InvalidFilter", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{
				latestHeight: 100,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
		}

		handler, err := NewHandler(store, logger)
		if err != nil {
			t.Fatalf("failed to create handler: %v", err)
		}

		query := `{
			transactionsByAddressFiltered(
				address: "0x456"
				filter: {
					fromBlock: "invalid"
					toBlock: "100"
				}
			) {
				nodes { hash }
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid fromBlock")
		}
	})

	t.Run("AddressBalance_InvalidBlockNumber", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{
				latestHeight: 100,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
		}

		handler, err := NewHandler(store, logger)
		if err != nil {
			t.Fatalf("failed to create handler: %v", err)
		}

		query := `{ addressBalance(address: "0x456", blockNumber: "invalid") }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid blockNumber")
		}
	})

	t.Run("BalanceHistory_InvalidBlock", func(t *testing.T) {
		store := &mockHistoricalStorage{
			mockStorage: &mockStorage{
				latestHeight: 100,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
		}

		handler, err := NewHandler(store, logger)
		if err != nil {
			t.Fatalf("failed to create handler: %v", err)
		}

		query := `{
			balanceHistory(
				address: "0x456"
				fromBlock: "invalid"
				toBlock: "100"
			) {
				nodes { blockNumber }
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid fromBlock")
		}
	})

	t.Run("NotHistoricalStorage", func(t *testing.T) {
		// Use regular mockStorage without historical methods
		regularStore := &mockStorage{
			latestHeight: 100,
			blocks:       make(map[uint64]*types.Block),
			blocksByHash: make(map[common.Hash]*types.Block),
		}

		handler, err := NewHandler(regularStore, logger)
		if err != nil {
			t.Fatalf("failed to create handler: %v", err)
		}

		query := `{ blockCount }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage does not support historical queries")
		}
	})
}

func TestHistoricalTypesInSchema(t *testing.T) {
	logger := zap.NewNop()
	store := &mockHistoricalStorage{
		mockStorage: &mockStorage{
			latestHeight: 100,
			blocks:       make(map[uint64]*types.Block),
			blocksByHash: make(map[common.Hash]*types.Block),
		},
	}

	schema, err := NewSchema(store, logger)
	if err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	s := schema.Schema()
	queryFields := s.QueryType().Fields()

	// Test that historical query fields exist
	historicalFields := []string{
		"blocksByTimeRange", "blockByTimestamp",
		"transactionsByAddressFiltered",
		"addressBalance", "balanceHistory",
		"blockCount", "transactionCount",
	}

	for _, field := range historicalFields {
		if _, exists := queryFields[field]; !exists {
			t.Errorf("expected historical query field %s to exist", field)
		}
	}

	// Test historical types initialization
	if balanceSnapshotType == nil {
		t.Error("balanceSnapshotType should be initialized")
	}
	if balanceHistoryConnectionType == nil {
		t.Error("balanceHistoryConnectionType should be initialized")
	}
	if historicalTransactionFilterType == nil {
		t.Error("historicalTransactionFilterType should be initialized")
	}
}

func TestParseHistoricalTransactionFilter(t *testing.T) {
	t.Run("ValidFilter", func(t *testing.T) {
		args := map[string]interface{}{
			"fromBlock":   "0",
			"toBlock":     "100",
			"minValue":    "1000",
			"maxValue":    "10000",
			"txType":      1,
			"successOnly": true,
		}

		filter, err := parseHistoricalTransactionFilter(args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if filter.FromBlock != 0 {
			t.Errorf("expected FromBlock 0, got %d", filter.FromBlock)
		}
		if filter.ToBlock != 100 {
			t.Errorf("expected ToBlock 100, got %d", filter.ToBlock)
		}
		if filter.MinValue.Cmp(big.NewInt(1000)) != 0 {
			t.Errorf("expected MinValue 1000, got %s", filter.MinValue.String())
		}
		if filter.MaxValue.Cmp(big.NewInt(10000)) != 0 {
			t.Errorf("expected MaxValue 10000, got %s", filter.MaxValue.String())
		}
		if filter.TxType != storage.TransactionType(1) {
			t.Errorf("expected TxType 1, got %d", filter.TxType)
		}
		if !filter.SuccessOnly {
			t.Error("expected SuccessOnly true")
		}
	})

	t.Run("MissingFromBlock", func(t *testing.T) {
		args := map[string]interface{}{
			"toBlock": "100",
		}

		_, err := parseHistoricalTransactionFilter(args)
		if err == nil {
			t.Error("expected error for missing fromBlock")
		}
	})

	t.Run("MissingToBlock", func(t *testing.T) {
		args := map[string]interface{}{
			"fromBlock": "0",
		}

		_, err := parseHistoricalTransactionFilter(args)
		if err == nil {
			t.Error("expected error for missing toBlock")
		}
	})

	t.Run("InvalidFromBlock", func(t *testing.T) {
		args := map[string]interface{}{
			"fromBlock": "invalid",
			"toBlock":   "100",
		}

		_, err := parseHistoricalTransactionFilter(args)
		if err == nil {
			t.Error("expected error for invalid fromBlock")
		}
	})

	t.Run("InvalidMinValue", func(t *testing.T) {
		args := map[string]interface{}{
			"fromBlock": "0",
			"toBlock":   "100",
			"minValue":  "invalid",
		}

		_, err := parseHistoricalTransactionFilter(args)
		if err == nil {
			t.Error("expected error for invalid minValue")
		}
	})

	t.Run("OptionalFields", func(t *testing.T) {
		args := map[string]interface{}{
			"fromBlock": "0",
			"toBlock":   "100",
		}

		filter, err := parseHistoricalTransactionFilter(args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if filter.MinValue != nil {
			t.Error("expected MinValue to be nil")
		}
		if filter.MaxValue != nil {
			t.Error("expected MaxValue to be nil")
		}
		if filter.TxType != storage.TxTypeAll {
			t.Errorf("expected TxType TxTypeAll, got %d", filter.TxType)
		}
		if filter.SuccessOnly {
			t.Error("expected SuccessOnly false")
		}
	})
}
