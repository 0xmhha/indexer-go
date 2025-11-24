package graphql

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"go.uber.org/zap"
)

// mockStorage is a mock implementation of storage.Storage for testing
type mockStorage struct {
	latestHeight uint64
	blocks       map[uint64]*types.Block
	blocksByHash map[common.Hash]*types.Block
	transactions map[common.Hash]*types.Transaction
	receipts     map[common.Hash]*types.Receipt
}

func (m *mockStorage) GetLatestHeight(ctx context.Context) (uint64, error) {
	return m.latestHeight, nil
}

func (m *mockStorage) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	if block, ok := m.blocks[height]; ok {
		return block, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if block, ok := m.blocksByHash[hash]; ok {
		return block, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *storage.TxLocation, error) {
	if tx, ok := m.transactions[hash]; ok {
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		return tx, location, nil
	}
	return nil, nil, storage.ErrNotFound
}

func (m *mockStorage) GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	if receipt, ok := m.receipts[hash]; ok {
		return receipt, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return []common.Hash{}, nil
}

func (m *mockStorage) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	blocks := make([]*types.Block, 0, endHeight-startHeight+1)
	for i := startHeight; i <= endHeight; i++ {
		if block, ok := m.blocks[i]; ok {
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

func (m *mockStorage) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) HasBlock(ctx context.Context, height uint64) (bool, error) {
	_, ok := m.blocks[height]
	return ok, nil
}

func (m *mockStorage) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	_, ok := m.transactions[hash]
	return ok, nil
}

func (m *mockStorage) SetLatestHeight(ctx context.Context, height uint64) error {
	m.latestHeight = height
	return nil
}

func (m *mockStorage) SetBlock(ctx context.Context, block *types.Block) error {
	m.blocks[block.NumberU64()] = block
	m.blocksByHash[block.Hash()] = block
	return nil
}

func (m *mockStorage) SetTransaction(ctx context.Context, tx *types.Transaction, location *storage.TxLocation) error {
	m.transactions[tx.Hash()] = tx
	return nil
}

func (m *mockStorage) SetReceipt(ctx context.Context, receipt *types.Receipt) error {
	m.receipts[receipt.TxHash] = receipt
	return nil
}

func (m *mockStorage) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	for _, receipt := range receipts {
		m.receipts[receipt.TxHash] = receipt
	}
	return nil
}

func (m *mockStorage) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	return nil
}

func (m *mockStorage) SetBlocks(ctx context.Context, blocks []*types.Block) error {
	for _, block := range blocks {
		m.blocks[block.NumberU64()] = block
		m.blocksByHash[block.Hash()] = block
	}
	return nil
}

func (m *mockStorage) DeleteBlock(ctx context.Context, height uint64) error {
	if block, ok := m.blocks[height]; ok {
		delete(m.blocksByHash, block.Hash())
		delete(m.blocks, height)
	}
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

func (m *mockStorage) NewBatch() storage.Batch {
	return nil
}

func (m *mockStorage) Compact(ctx context.Context, start, end []byte) error {
	return nil
}

func (m *mockStorage) SetABI(ctx context.Context, address common.Address, abiJSON []byte) error {
	return nil
}

func (m *mockStorage) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	return nil, nil
}

func (m *mockStorage) DeleteABI(ctx context.Context, address common.Address) error {
	return nil
}

func (m *mockStorage) ListABIs(ctx context.Context) ([]common.Address, error) {
	return []common.Address{}, nil
}

func (m *mockStorage) HasABI(ctx context.Context, address common.Address) (bool, error) {
	return false, nil
}

func (m *mockStorage) GetLogs(ctx context.Context, filter *storage.LogFilter) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) SaveLog(ctx context.Context, log *types.Log) error {
	return nil
}

func (m *mockStorage) SaveLogs(ctx context.Context, logs []*types.Log) error {
	return nil
}

func (m *mockStorage) DeleteLogsByBlock(ctx context.Context, blockNumber uint64) error {
	return nil
}

func (m *mockStorage) IndexLogs(ctx context.Context, logs []*types.Log) error {
	return nil
}

func (m *mockStorage) IndexLog(ctx context.Context, log *types.Log) error {
	return nil
}

// mockStorageWithErrors returns errors for testing error paths
type mockStorageWithErrors struct {
}

func (m *mockStorageWithErrors) GetLatestHeight(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *storage.TxLocation, error) {
	return nil, nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) HasBlock(ctx context.Context, height uint64) (bool, error) {
	return false, storage.ErrNotFound
}

func (m *mockStorageWithErrors) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	return false, storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetLatestHeight(ctx context.Context, height uint64) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetBlock(ctx context.Context, block *types.Block) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetTransaction(ctx context.Context, tx *types.Transaction, location *storage.TxLocation) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetReceipt(ctx context.Context, receipt *types.Receipt) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetBlocks(ctx context.Context, blocks []*types.Block) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteBlock(ctx context.Context, height uint64) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) Close() error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) NewBatch() storage.Batch {
	return nil
}

func (m *mockStorageWithErrors) Compact(ctx context.Context, start, end []byte) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetABI(ctx context.Context, address common.Address, abiJSON []byte) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteABI(ctx context.Context, address common.Address) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) ListABIs(ctx context.Context) ([]common.Address, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) HasABI(ctx context.Context, address common.Address) (bool, error) {
	return false, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetLogs(ctx context.Context, filter *storage.LogFilter) ([]*types.Log, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) SaveLog(ctx context.Context, log *types.Log) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SaveLogs(ctx context.Context, logs []*types.Log) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteLogsByBlock(ctx context.Context, blockNumber uint64) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) IndexLogs(ctx context.Context, logs []*types.Log) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) IndexLog(ctx context.Context, log *types.Log) error {
	return storage.ErrNotFound
}

func TestGraphQLHandler(t *testing.T) {
	logger := zap.NewNop()

	// Create test block
	header := &types.Header{
		Number:     common.Big1,
		ParentHash: common.HexToHash("0x123"),
		Time:       123456,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	testBlock := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))

	store := &mockStorage{
		latestHeight: 100,
		blocks:       map[uint64]*types.Block{1: testBlock},
		blocksByHash: map[common.Hash]*types.Block{testBlock.Hash(): testBlock},
		transactions: make(map[common.Hash]*types.Transaction),
		receipts:     make(map[common.Hash]*types.Receipt),
	}

	handler, err := NewHandler(store, logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	t.Run("GraphQLEndpoint", func(t *testing.T) {
		query := `{"query":"{ latestHeight }"}`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(query))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %v", w.Code)
		}
	})

	t.Run("GraphQLEndpoint_InvalidJSON", func(t *testing.T) {
		query := `invalid json`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(query))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK even with invalid JSON, got %v", w.Code)
		}
	})

	t.Run("GraphQLEndpoint_GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// GraphQL handler should handle GET requests too
		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %v", w.Code)
		}
	})

	t.Run("PlaygroundEndpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/playground", nil)
		w := httptest.NewRecorder()

		playgroundHandler := handler.PlaygroundHandler()
		playgroundHandler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %v", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "GraphQL Playground") {
			t.Error("expected GraphQL Playground HTML")
		}
	})

	t.Run("ExecuteQuery_LatestHeight", func(t *testing.T) {
		query := `{ latestHeight }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}

		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ExecuteQueryJSON", func(t *testing.T) {
		query := `{ latestHeight }`
		jsonBytes, err := handler.ExecuteQueryJSON(query, nil)
		if err != nil {
			t.Fatalf("failed to execute query JSON: %v", err)
		}

		if len(jsonBytes) == 0 {
			t.Error("expected JSON response")
		}
	})
}

func TestGraphQLSchema(t *testing.T) {
	logger := zap.NewNop()
	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	schema, err := NewSchema(store, logger)
	if err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	s := schema.Schema()
	if s.QueryType() == nil {
		t.Error("expected query type in schema")
	}

	// Test that schema has all expected query fields
	queryFields := s.QueryType().Fields()
	expectedFields := []string{
		"latestHeight", "block", "blockByHash", "blocks",
		"transaction", "transactions", "transactionsByAddress",
		"receipt", "receiptsByBlock", "logs",
	}
	for _, field := range expectedFields {
		if _, exists := queryFields[field]; !exists {
			t.Errorf("expected query field %s to exist", field)
		}
	}
}

func TestGraphQLTypes(t *testing.T) {
	// Test type initialization
	if blockType == nil {
		t.Error("blockType should be initialized")
	}
	if transactionType == nil {
		t.Error("transactionType should be initialized")
	}
	if receiptType == nil {
		t.Error("receiptType should be initialized")
	}
	if logType == nil {
		t.Error("logType should be initialized")
	}
	if pageInfoType == nil {
		t.Error("pageInfoType should be initialized")
	}
	if blockConnectionType == nil {
		t.Error("blockConnectionType should be initialized")
	}
	if transactionConnectionType == nil {
		t.Error("transactionConnectionType should be initialized")
	}
	if logConnectionType == nil {
		t.Error("logConnectionType should be initialized")
	}
	if bigIntType == nil {
		t.Error("bigIntType should be initialized")
	}
	if hashType == nil {
		t.Error("hashType should be initialized")
	}
	if addressType == nil {
		t.Error("addressType should be initialized")
	}
	if bytesType == nil {
		t.Error("bytesType should be initialized")
	}
}

func TestGraphQLResolvers(t *testing.T) {
	logger := zap.NewNop()

	// Create test block
	header := &types.Header{
		Number:     common.Big1,
		ParentHash: common.HexToHash("0x123"),
		Time:       123456,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	testBlock := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))

	// Create test transaction
	testTx := types.NewTransaction(
		0,
		common.HexToAddress("0x456"),
		common.Big1,
		21000,
		common.Big1,
		nil,
	)

	// Create test receipt
	testReceipt := &types.Receipt{
		TxHash:            testTx.Hash(),
		Status:            1,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		Logs:              []*types.Log{},
		BlockNumber:       common.Big1,
		BlockHash:         testBlock.Hash(),
		EffectiveGasPrice: common.Big1,
	}

	store := &mockStorage{
		latestHeight: 100,
		blocks:       map[uint64]*types.Block{1: testBlock},
		blocksByHash: map[common.Hash]*types.Block{testBlock.Hash(): testBlock},
		transactions: map[common.Hash]*types.Transaction{testTx.Hash(): testTx},
		receipts:     map[common.Hash]*types.Receipt{testTx.Hash(): testReceipt},
	}

	handler, err := NewHandler(store, logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	t.Run("ResolveBlock_Success", func(t *testing.T) {
		query := `{ block(number: "1") { number hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ResolveBlock_NotFound", func(t *testing.T) {
		query := `{ block(number: "999") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for non-existent block")
		}
	})

	t.Run("ResolveBlockByHash_Success", func(t *testing.T) {
		query := `{ blockByHash(hash: "` + testBlock.Hash().Hex() + `") { number hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveBlockByHash_NotFound", func(t *testing.T) {
		query := `{ blockByHash(hash: "0x0000000000000000000000000000000000000000000000000000000000000000") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for non-existent block")
		}
	})

	t.Run("ResolveTransaction_Success", func(t *testing.T) {
		query := `{ transaction(hash: "` + testTx.Hash().Hex() + `") { hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveTransaction_NotFound", func(t *testing.T) {
		query := `{ transaction(hash: "0x0000000000000000000000000000000000000000000000000000000000000000") { hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for non-existent transaction")
		}
	})

	t.Run("ResolveReceipt_Success", func(t *testing.T) {
		query := `{ receipt(transactionHash: "` + testReceipt.TxHash.Hex() + `") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveReceiptsByBlock", func(t *testing.T) {
		query := `{ receiptsByBlock(blockNumber: "1") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveTransactionsByAddress", func(t *testing.T) {
		query := `{ transactionsByAddress(address: "0x456") { nodes { hash } pageInfo { hasNextPage } } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("InvalidQuery", func(t *testing.T) {
		query := `{ invalid }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid query")
		}
	})

	t.Run("ComplexQuery_BlockWithTransactions", func(t *testing.T) {
		query := `{
			block(number: "1") {
				number
				hash
				transactions {
					hash
					from
				}
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveBlocks", func(t *testing.T) {
		query := `{ blocks { nodes { number hash } totalCount pageInfo { hasNextPage } } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveTransactions", func(t *testing.T) {
		query := `{ transactions { nodes { hash } totalCount pageInfo { hasNextPage } } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveLogs", func(t *testing.T) {
		query := `{ logs(filter: {}) { nodes { address } totalCount pageInfo { hasNextPage } } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveBlock_InvalidNumber", func(t *testing.T) {
		query := `{ block(number: "invalid") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid block number")
		}
	})

	t.Run("ResolveBlockByHash_InvalidHash", func(t *testing.T) {
		query := `{ blockByHash(hash: "invalid") { number } }`
		result := handler.ExecuteQuery(query, nil)

		// Should handle invalid hash gracefully
		if len(result.Errors) > 0 {
			// Error is acceptable for invalid hash
		}
	})

	t.Run("ResolveTransaction_InvalidHash", func(t *testing.T) {
		query := `{ transaction(hash: "invalid") { hash } }`
		result := handler.ExecuteQuery(query, nil)

		// Should handle invalid hash gracefully
		if len(result.Errors) > 0 {
			// Error is acceptable for invalid hash
		}
	})

	t.Run("ResolveReceipt_InvalidHash", func(t *testing.T) {
		query := `{ receipt(transactionHash: "invalid") { status } }`
		result := handler.ExecuteQuery(query, nil)

		// Should handle invalid hash gracefully
		if len(result.Errors) > 0 {
			// Error is acceptable for invalid hash
		}
	})

	t.Run("ResolveReceiptsByBlock_InvalidNumber", func(t *testing.T) {
		query := `{ receiptsByBlock(blockNumber: "invalid") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid block number")
		}
	})

	t.Run("ResolveTransactionsByAddress_WithPagination", func(t *testing.T) {
		query := `{ transactionsByAddress(address: "0x456", pagination: {limit: 5, offset: 0}) { nodes { hash } totalCount } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})
}

func TestGraphQLErrorPaths(t *testing.T) {
	logger := zap.NewNop()
	errorStore := &mockStorageWithErrors{}

	handler, err := NewHandler(errorStore, logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	t.Run("ResolveLatestHeight_Error", func(t *testing.T) {
		query := `{ latestHeight }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveBlock_Error", func(t *testing.T) {
		query := `{ block(number: "1") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveBlockByHash_Error", func(t *testing.T) {
		query := `{ blockByHash(hash: "0x123") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveTransaction_Error", func(t *testing.T) {
		query := `{ transaction(hash: "0x123") { hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveReceipt_Error", func(t *testing.T) {
		query := `{ receipt(transactionHash: "0x123") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveReceiptsByBlock_Error", func(t *testing.T) {
		query := `{ receiptsByBlock(blockNumber: "1") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveTransactionsByAddress_Error", func(t *testing.T) {
		query := `{ transactionsByAddress(address: "0x456") { nodes { hash } totalCount } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})
}

func TestGraphQLMappers(t *testing.T) {
	logger := zap.NewNop()
	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	schema, err := NewSchema(store, logger)
	if err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	t.Run("BlockToMap", func(t *testing.T) {
		header := &types.Header{
			Number:     common.Big1,
			ParentHash: common.HexToHash("0x123"),
			Time:       123456,
			GasLimit:   8000000,
			GasUsed:    5000000,
		}
		block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
		blockMap := schema.blockToMap(block)

		if blockMap == nil {
			t.Error("expected blockMap to be non-nil")
		}
		if blockMap["number"] == nil {
			t.Error("expected number field")
		}
		if blockMap["hash"] == nil {
			t.Error("expected hash field")
		}
	})

	t.Run("TransactionToMap", func(t *testing.T) {
		tx := types.NewTransaction(
			0,
			common.HexToAddress("0x456"),
			common.Big1,
			21000,
			common.Big1,
			nil,
		)
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(tx, location)

		if txMap == nil {
			t.Error("expected txMap to be non-nil")
		}
		if txMap["hash"] == nil {
			t.Error("expected hash field")
		}
		if txMap["blockNumber"] == nil {
			t.Error("expected blockNumber field")
		}
	})

	t.Run("ReceiptToMap", func(t *testing.T) {
		receipt := &types.Receipt{
			TxHash:            common.HexToHash("0xabc"),
			Status:            1,
			CumulativeGasUsed: 21000,
			GasUsed:           21000,
			Logs:              []*types.Log{},
			BlockNumber:       common.Big1,
			BlockHash:         common.HexToHash("0x123"),
			EffectiveGasPrice: common.Big1,
		}
		receiptMap := schema.receiptToMap(receipt)

		if receiptMap == nil {
			t.Error("expected receiptMap to be non-nil")
		}
		if receiptMap["status"] == nil {
			t.Error("expected status field")
		}
		if receiptMap["gasUsed"] == nil {
			t.Error("expected gasUsed field")
		}
	})

	t.Run("LogToMap", func(t *testing.T) {
		log := &types.Log{
			Address: common.HexToAddress("0x789"),
			Topics:  []common.Hash{common.HexToHash("0xabc")},
			Data:    []byte{1, 2, 3},
		}
		logMap := schema.logToMap(log)

		if logMap == nil {
			t.Error("expected logMap to be non-nil")
		}
		if logMap["address"] == nil {
			t.Error("expected address field")
		}
		if logMap["topics"] == nil {
			t.Error("expected topics field")
		}
	})

	t.Run("TransactionToMap_WithToAddress", func(t *testing.T) {
		to := common.HexToAddress("0x789")
		tx := types.NewTx(&types.LegacyTx{
			Nonce:    0,
			GasPrice: common.Big1,
			Gas:      21000,
			To:       &to,
			Value:    common.Big1,
			Data:     []byte{1, 2, 3},
		})
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(tx, location)

		if txMap == nil {
			t.Error("expected txMap to be non-nil")
		}
		if txMap["to"] == nil {
			t.Error("expected to field for contract call")
		}
		if txMap["gasPrice"] == nil {
			t.Error("expected gasPrice for legacy tx")
		}
	})

	t.Run("TransactionToMap_DynamicFeeTx", func(t *testing.T) {
		to := common.HexToAddress("0x789")
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   common.Big1,
			Nonce:     0,
			GasTipCap: common.Big1,
			GasFeeCap: common.Big2,
			Gas:       21000,
			To:        &to,
			Value:     common.Big1,
			Data:      []byte{},
		})
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(tx, location)

		if txMap == nil {
			t.Error("expected txMap to be non-nil")
		}
		if txMap["maxFeePerGas"] == nil {
			t.Error("expected maxFeePerGas for EIP-1559 tx")
		}
		if txMap["maxPriorityFeePerGas"] == nil {
			t.Error("expected maxPriorityFeePerGas for EIP-1559 tx")
		}
		if txMap["chainId"] == nil {
			t.Error("expected chainId")
		}
	})

	t.Run("TransactionToMap_AccessListTx", func(t *testing.T) {
		to := common.HexToAddress("0x789")
		accessList := types.AccessList{
			types.AccessTuple{
				Address: common.HexToAddress("0xabc"),
				StorageKeys: []common.Hash{
					common.HexToHash("0x123"),
					common.HexToHash("0x456"),
				},
			},
		}
		tx := types.NewTx(&types.AccessListTx{
			ChainID:    common.Big1,
			Nonce:      0,
			GasPrice:   common.Big1,
			Gas:        21000,
			To:         &to,
			Value:      common.Big1,
			Data:       []byte{},
			AccessList: accessList,
		})
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(tx, location)

		if txMap == nil {
			t.Error("expected txMap to be non-nil")
		}
		if txMap["accessList"] == nil {
			t.Error("expected accessList for EIP-2930 tx")
		}
		accessListResult, ok := txMap["accessList"].([]interface{})
		if !ok {
			t.Error("expected accessList to be an array")
		}
		if len(accessListResult) != 1 {
			t.Errorf("expected 1 access list entry, got %d", len(accessListResult))
		}
	})

	t.Run("BlockToMap_Nil", func(t *testing.T) {
		blockMap := schema.blockToMap(nil)
		if blockMap != nil {
			t.Error("expected nil for nil block")
		}
	})

	t.Run("TransactionToMap_Nil", func(t *testing.T) {
		txMap := schema.transactionToMap(nil, &storage.TxLocation{})
		if txMap != nil {
			t.Error("expected nil for nil transaction")
		}
	})

	t.Run("ReceiptToMap_Nil", func(t *testing.T) {
		receiptMap := schema.receiptToMap(nil)
		if receiptMap != nil {
			t.Error("expected nil for nil receipt")
		}
	})

	t.Run("LogToMap_Nil", func(t *testing.T) {
		logMap := schema.logToMap(nil)
		if logMap != nil {
			t.Error("expected nil for nil log")
		}
	})

	t.Run("ReceiptToMap_WithLogs", func(t *testing.T) {
		log := &types.Log{
			Address: common.HexToAddress("0x789"),
			Topics:  []common.Hash{common.HexToHash("0xabc")},
			Data:    []byte{1, 2, 3},
		}
		receipt := &types.Receipt{
			TxHash:            common.HexToHash("0xabc"),
			Status:            1,
			CumulativeGasUsed: 21000,
			GasUsed:           21000,
			Logs:              []*types.Log{log},
			BlockNumber:       common.Big1,
			BlockHash:         common.HexToHash("0x123"),
			EffectiveGasPrice: common.Big1,
		}
		receiptMap := schema.receiptToMap(receipt)

		if receiptMap == nil {
			t.Error("expected receiptMap to be non-nil")
		}
		logs, ok := receiptMap["logs"].([]interface{})
		if !ok {
			t.Error("expected logs to be an array")
		}
		if len(logs) != 1 {
			t.Errorf("expected 1 log, got %d", len(logs))
		}
	})

	t.Run("ReceiptToMap_WithContractAddress", func(t *testing.T) {
		receipt := &types.Receipt{
			TxHash:            common.HexToHash("0xabc"),
			Status:            1,
			CumulativeGasUsed: 21000,
			GasUsed:           21000,
			Logs:              []*types.Log{},
			BlockNumber:       common.Big1,
			BlockHash:         common.HexToHash("0x123"),
			EffectiveGasPrice: common.Big1,
			ContractAddress:   common.HexToAddress("0x999"),
		}
		receiptMap := schema.receiptToMap(receipt)

		if receiptMap == nil {
			t.Error("expected receiptMap to be non-nil")
		}
		if receiptMap["contractAddress"] == nil {
			t.Error("expected contractAddress field")
		}
	})
}
