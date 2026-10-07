package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/userop"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// mockStorage is a mock implementation of storage.Storage for testing
type mockStorage struct {
	latestHeight uint64
	blocks       map[uint64]*types.Block
	blocksByHash map[common.Hash]*types.Block
}

func (m *mockStorage) GetLatestHeight(ctx context.Context) (uint64, error) {
	return m.latestHeight, nil
}

func (m *mockStorage) gethGetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	if block, ok := m.blocks[height]; ok {
		return block, nil
	}
	return nil, port.ErrNotFound
}

func (m *mockStorage) gethGetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if block, ok := m.blocksByHash[hash]; ok {
		return block, nil
	}
	return nil, port.ErrNotFound
}

func (m *mockStorage) gethGetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *port.TxLocation, error) {
	return nil, nil, port.ErrNotFound
}
func (m *mockStorage) gethGetTransactions(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, []*port.TxLocation, error) {
	return nil, nil, nil
}

func (m *mockStorage) gethGetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) offsetGetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return []common.Hash{}, nil
}

func (m *mockStorage) gethGetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) gethGetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) gethGetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) gethGetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	blocks := make([]*types.Block, 0, endHeight-startHeight+1)
	for i := startHeight; i <= endHeight; i++ {
		if block, ok := m.blocks[i]; ok {
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

func (m *mockStorage) HasBlock(ctx context.Context, height uint64) (bool, error) {
	_, ok := m.blocks[height]
	return ok, nil
}

func (m *mockStorage) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	return false, nil
}

func (m *mockStorage) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	return false, nil
}

func (m *mockStorage) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	return nil, nil
}

func (m *mockStorage) SetLatestHeight(ctx context.Context, height uint64) error {
	m.latestHeight = height
	return nil
}

func (m *mockStorage) gethSetBlock(ctx context.Context, block *types.Block) error {
	m.blocks[block.NumberU64()] = block
	m.blocksByHash[block.Hash()] = block
	return nil
}

func (m *mockStorage) SetTransaction(ctx context.Context, tx *types.Transaction, location *port.TxLocation) error {
	return nil
}

func (m *mockStorage) gethSetReceipt(ctx context.Context, receipt *types.Receipt) error {
	return nil
}

func (m *mockStorage) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
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

// KVStore interface methods
func (m *mockStorage) Put(ctx context.Context, key, value []byte) error {
	return nil
}

func (m *mockStorage) Get(ctx context.Context, key []byte) ([]byte, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) Delete(ctx context.Context, key []byte) error {
	return nil
}

func (m *mockStorage) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	return nil
}

func (m *mockStorage) Has(ctx context.Context, key []byte) (bool, error) {
	return false, nil
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

func (m *mockStorage) gethGetLogs(ctx context.Context, filter *port.LogFilter) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) gethGetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) gethGetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) gethGetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
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

func (m *mockStorage) gethIndexLogs(ctx context.Context, logs []*types.Log) error {
	return nil
}

func (m *mockStorage) gethIndexLog(ctx context.Context, log *types.Log) error {
	return nil
}

func (m *mockStorage) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]port.SearchResult, error) {
	return []port.SearchResult{}, nil
}

// Contract verification methods
func (m *mockStorage) GetContractVerification(ctx context.Context, address common.Address) (*port.ContractVerification, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	return false, nil
}

func (m *mockStorage) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error) {
	return []common.Address{}, nil
}

func (m *mockStorage) CountVerifiedContracts(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockStorage) SetContractVerification(ctx context.Context, verification *port.ContractVerification) error {
	return nil
}

func (m *mockStorage) DeleteContractVerification(ctx context.Context, address common.Address) error {
	return nil
}

func (m *mockStorage) GetAddressStats(ctx context.Context, addr common.Address) (*port.AddressStats, error) {
	return nil, nil
}

// HistoricalReader methods
func (m *mockStorage) gethGetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	return []*types.Block{}, nil
}

func (m *mockStorage) gethGetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, limit, offset int) ([]*port.TransactionWithReceipt, error) {
	return []*port.TransactionWithReceipt{}, nil
}

func (m *mockStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (m *mockStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]port.BalanceSnapshot, error) {
	return []port.BalanceSnapshot{}, nil
}

func (m *mockStorage) GetBlockCount(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockStorage) GetTransactionCount(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockStorage) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.MinerStats, error) {
	return []port.MinerStats{}, nil
}

func (m *mockStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]port.TokenBalance, error) {
	return []port.TokenBalance{}, nil
}

func (m *mockStorage) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*port.GasStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*port.AddressGasStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressGasStats, error) {
	return []port.AddressGasStats{}, nil
}

func (m *mockStorage) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressActivityStats, error) {
	return []port.AddressActivityStats{}, nil
}

func (m *mockStorage) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*port.NetworkMetrics, error) {
	return nil, port.ErrNotFound
}

// HistoricalWriter methods
func (m *mockStorage) SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error {
	return nil
}

func (m *mockStorage) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	return nil
}

func (m *mockStorage) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	return nil
}

// TokenMetadataReader methods
func (m *mockStorage) GetTokenMetadata(ctx context.Context, address common.Address) (*port.TokenMetadata, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) ListTokensByStandard(ctx context.Context, standard port.TokenStandard, limit, offset int) ([]*port.TokenMetadata, error) {
	return []*port.TokenMetadata{}, nil
}

func (m *mockStorage) GetTokensCount(ctx context.Context, standard port.TokenStandard) (int, error) {
	return 0, nil
}

func (m *mockStorage) SearchTokens(ctx context.Context, query string, limit int) ([]*port.TokenMetadata, error) {
	return []*port.TokenMetadata{}, nil
}

// TokenMetadataWriter methods
func (m *mockStorage) SaveTokenMetadata(ctx context.Context, metadata *port.TokenMetadata) error {
	return nil
}

func (m *mockStorage) DeleteTokenMetadata(ctx context.Context, address common.Address) error {
	return nil
}

func (m *mockStorage) SetTokenMetadataFetcher(fetcher port.TokenMetadataFetcher) {
}

// SetCodeIndexReader methods
func (m *mockStorage) GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*port.SetCodeAuthorizationRecord, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, nil
}
func (m *mockStorage) offsetGetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, nil
}
func (m *mockStorage) offsetGetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsByBlock(ctx context.Context, blockNumber uint64) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, nil
}
func (m *mockStorage) GetAddressSetCodeStats(ctx context.Context, address common.Address) (*port.AddressSetCodeStats, error) {
	return &port.AddressSetCodeStats{Address: address}, nil
}
func (m *mockStorage) GetAddressDelegationState(ctx context.Context, address common.Address) (*port.AddressDelegationState, error) {
	return &port.AddressDelegationState{Address: address}, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsCountByAuthority(ctx context.Context, authority common.Address) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetSetCodeTransactionCount(ctx context.Context) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetRecentSetCodeAuthorizations(ctx context.Context, limit int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, nil
}

// SetCodeIndexWriter methods
func (m *mockStorage) SaveSetCodeAuthorization(ctx context.Context, record *port.SetCodeAuthorizationRecord) error {
	return nil
}
func (m *mockStorage) SaveSetCodeAuthorizations(ctx context.Context, records []*port.SetCodeAuthorizationRecord) error {
	return nil
}
func (m *mockStorage) UpdateAddressDelegationState(ctx context.Context, state *port.AddressDelegationState) error {
	return nil
}
func (m *mockStorage) IncrementSetCodeStats(ctx context.Context, address common.Address, asTarget, asAuthority bool, blockNumber uint64) error {
	return nil
}

// UserOpIndexReader methods
func (m *mockStorage) GetUserOp(ctx context.Context, opHash common.Hash) (*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsBySender(ctx context.Context, sender common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsByBundler(ctx context.Context, bundler common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsByFactory(ctx context.Context, factory common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetBundlerStats(ctx context.Context, bundler common.Address) (*userop.BundlerStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetFactoryStats(ctx context.Context, factory common.Address) (*userop.FactoryStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetPaymasterStats(ctx context.Context, paymaster common.Address) (*userop.PaymasterStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetSmartAccount(ctx context.Context, address common.Address) (*userop.SmartAccount, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetRecentUserOps(ctx context.Context, limit int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpCount(ctx context.Context) (int, error) {
	return 0, nil
}
func (m *mockStorage) ListBundlers(ctx context.Context, limit, offset int) ([]*userop.BundlerStats, error) {
	return nil, nil
}
func (m *mockStorage) ListFactories(ctx context.Context, limit, offset int) ([]*userop.FactoryStats, error) {
	return nil, nil
}
func (m *mockStorage) ListPaymasters(ctx context.Context, limit, offset int) ([]*userop.PaymasterStats, error) {
	return nil, nil
}
func (m *mockStorage) ListSmartAccounts(ctx context.Context, limit, offset int) ([]*userop.SmartAccount, error) {
	return nil, nil
}

// UserOpIndexWriter methods
func (m *mockStorage) SaveUserOp(ctx context.Context, op *userop.UserOperation) error {
	return nil
}
func (m *mockStorage) SaveUserOps(ctx context.Context, ops []*userop.UserOperation) error {
	return nil
}
func (m *mockStorage) UpdateBundlerStats(ctx context.Context, stats *userop.BundlerStats) error {
	return nil
}
func (m *mockStorage) UpdateFactoryStats(ctx context.Context, stats *userop.FactoryStats) error {
	return nil
}
func (m *mockStorage) UpdatePaymasterStats(ctx context.Context, stats *userop.PaymasterStats) error {
	return nil
}
func (m *mockStorage) SaveSmartAccount(ctx context.Context, account *userop.SmartAccount) error {
	return nil
}

// ModuleIndexReader methods
func (m *mockStorage) GetInstalledModule(ctx context.Context, account, module common.Address) (*port.InstalledModule, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) offsetGetModulesByAccount(ctx context.Context, account common.Address, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, nil
}
func (m *mockStorage) offsetGetModulesByType(ctx context.Context, moduleType port.ModuleType, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, nil
}
func (m *mockStorage) GetModuleStats(ctx context.Context, module common.Address) (*port.ModuleStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetAccountModules(ctx context.Context, account common.Address) (*port.AccountModules, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetRecentModuleEvents(ctx context.Context, limit int) ([]*port.InstalledModule, error) {
	return nil, nil
}
func (m *mockStorage) GetModuleEventCount(ctx context.Context) (int, error) {
	return 0, nil
}
func (m *mockStorage) offsetListModuleStats(ctx context.Context, limit, offset int) ([]*port.ModuleStats, error) {
	return nil, nil
}

// ModuleIndexWriter methods
func (m *mockStorage) SaveInstalledModule(ctx context.Context, record *port.InstalledModule) error {
	return nil
}
func (m *mockStorage) RemoveModule(ctx context.Context, account, module common.Address, blockNumber uint64, txHash common.Hash) error {
	return nil
}
func (m *mockStorage) UpdateModuleStats(ctx context.Context, stats *port.ModuleStats) error {
	return nil
}

// mockStorageWithData extends mockStorage with transaction and receipt data
type mockStorageWithData struct {
	*mockStorage
	transactions map[common.Hash]*types.Transaction
	receipts     map[common.Hash]*types.Receipt
}

func (m *mockStorageWithData) gethGetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *port.TxLocation, error) {
	if tx, ok := m.transactions[hash]; ok {
		location := &port.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		return tx, location, nil
	}
	return nil, nil, port.ErrNotFound
}
func (m *mockStorageWithData) gethGetTransactions(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, []*port.TxLocation, error) {
	txs := make([]*types.Transaction, len(hashes))
	locs := make([]*port.TxLocation, len(hashes))
	for i, h := range hashes {
		txs[i], locs[i], _ = m.gethGetTransaction(ctx, h)
	}
	return txs, locs, nil
}

func (m *mockStorageWithData) gethGetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	if receipt, ok := m.receipts[hash]; ok {
		return receipt, nil
	}
	return nil, port.ErrNotFound
}

func TestJSONRPCServer(t *testing.T) {
	logger := zap.NewNop()
	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	server := NewServer(store, logger)

	t.Run("GetLatestHeight", func(t *testing.T) {
		reqBody := `{"jsonrpc":"2.0","method":"getLatestHeight","params":{},"id":1}`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %v", w.Code)
		}

		var resp Response
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Error != nil {
			t.Errorf("expected no error, got %v", resp.Error)
		}

		result, ok := resp.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected result to be a map")
		}

		height, ok := result["height"].(float64)
		if !ok || uint64(height) != 100 {
			t.Errorf("expected height 100, got %v", height)
		}
	})

	t.Run("InvalidMethod", func(t *testing.T) {
		reqBody := `{"jsonrpc":"2.0","method":"invalidMethod","params":{},"id":1}`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Error == nil {
			t.Error("expected error for invalid method")
		}

		if resp.Error.Code != MethodNotFound {
			t.Errorf("expected MethodNotFound error, got %v", resp.Error.Code)
		}
	})

	t.Run("InvalidJSONRPCVersion", func(t *testing.T) {
		reqBody := `{"jsonrpc":"1.0","method":"getLatestHeight","params":{},"id":1}`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Error == nil {
			t.Error("expected error for invalid jsonrpc version")
		}

		if resp.Error.Code != InvalidRequest {
			t.Errorf("expected InvalidRequest error, got %v", resp.Error.Code)
		}
	})

	t.Run("BatchRequest", func(t *testing.T) {
		reqBody := `[
			{"jsonrpc":"2.0","method":"getLatestHeight","params":{},"id":1},
			{"jsonrpc":"2.0","method":"invalidMethod","params":{},"id":2}
		]`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var batch BatchResponse
		if err := json.NewDecoder(w.Body).Decode(&batch); err != nil {
			t.Fatalf("failed to decode batch response: %v", err)
		}

		if len(batch) != 2 {
			t.Errorf("expected 2 responses, got %v", len(batch))
		}

		// First request should succeed
		if batch[0].Error != nil {
			t.Errorf("first request should succeed, got error: %v", batch[0].Error)
		}

		// Second request should fail
		if batch[1].Error == nil {
			t.Error("second request should fail")
		}
	})

	t.Run("MethodNotAllowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rpc", nil)
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected MethodNotAllowed, got %v", w.Code)
		}
	})
}

func TestJSONRPCTypes(t *testing.T) {
	t.Run("NewError", func(t *testing.T) {
		err := NewError(InvalidParams, "test error", "test data")
		if err.Code != InvalidParams {
			t.Errorf("expected code %v, got %v", InvalidParams, err.Code)
		}
		if err.Message != "test error" {
			t.Errorf("expected message 'test error', got %v", err.Message)
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "test error") {
			t.Errorf("error string should contain message: %s", errStr)
		}
	})

	t.Run("ErrorWithoutData", func(t *testing.T) {
		err := NewError(InvalidRequest, "test error", nil)
		errStr := err.Error()
		if !strings.Contains(errStr, "test error") {
			t.Errorf("error string should contain message: %s", errStr)
		}
		if strings.Contains(errStr, "data:") {
			t.Errorf("error string should not contain data: %s", errStr)
		}
	})

	t.Run("NewResponse", func(t *testing.T) {
		resp := NewResponse(1, "test result")
		if resp.JSONRPC != "2.0" {
			t.Errorf("expected jsonrpc 2.0, got %v", resp.JSONRPC)
		}
		if resp.ID != 1 {
			t.Errorf("expected id 1, got %v", resp.ID)
		}
		if resp.Result != "test result" {
			t.Errorf("expected result 'test result', got %v", resp.Result)
		}
	})

	t.Run("NewErrorResponse", func(t *testing.T) {
		err := NewError(InternalError, "internal error", nil)
		resp := NewErrorResponse(1, err)
		if resp.Error == nil {
			t.Error("expected error to be set")
		}
		if resp.Error.Code != InternalError {
			t.Errorf("expected error code %v, got %v", InternalError, resp.Error.Code)
		}
	})
}

func TestJSONRPCMethods(t *testing.T) {
	logger := zap.NewNop()

	// Create test block
	header := &types.Header{
		Number:     common.Big1,
		ParentHash: common.HexToHash("0x123"),
		Time:       123456,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	testBlock := types.NewBlockWithHeader(header)

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
	}

	// Extend mockStorage for successful tests
	storeWithData := &mockStorageWithData{
		mockStorage:  store,
		transactions: map[common.Hash]*types.Transaction{testTx.Hash(): testTx},
		receipts:     map[common.Hash]*types.Receipt{testTx.Hash(): testReceipt},
	}

	server := NewServer(store, logger)
	serverWithData := NewServer(storeWithData, logger)
	ctx := context.Background()

	t.Run("GetBlock_InvalidParams_MissingNumber", func(t *testing.T) {
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getBlock", params)
		if err == nil {
			t.Fatal("expected error for missing block number")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams error, got %v", err.Code)
		}
	})

	t.Run("GetBlock_InvalidParams_WrongType", func(t *testing.T) {
		params := json.RawMessage(`{"number": true}`)
		_, err := server.HandleMethodDirect(ctx, "getBlock", params)
		if err == nil {
			t.Fatal("expected error for invalid number type")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams error, got %v", err.Code)
		}
	})

	t.Run("GetBlock_NumberFormat", func(t *testing.T) {
		params := json.RawMessage(`{"number": 100}`)
		_, err := server.HandleMethodDirect(ctx, "getBlock", params)
		if err == nil {
			t.Error("expected error when block not found")
		}
	})

	t.Run("GetBlock_StringNumber", func(t *testing.T) {
		params := json.RawMessage(`{"number": "100"}`)
		_, err := server.HandleMethodDirect(ctx, "getBlock", params)
		if err == nil {
			t.Error("expected error when block not found")
		}
	})

	t.Run("GetBlockByHash_InvalidParams", func(t *testing.T) {
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getBlockByHash", params)
		if err == nil {
			t.Fatal("expected error for missing hash")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams error, got %v", err.Code)
		}
	})

	t.Run("GetBlockByHash_NotFound", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "0x1234567890abcdef"}`)
		_, err := server.HandleMethodDirect(ctx, "getBlockByHash", params)
		if err == nil {
			t.Error("expected error when block not found")
		}
	})

	t.Run("GetTxResult_InvalidParams", func(t *testing.T) {
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getTxResult", params)
		if err == nil {
			t.Fatal("expected error for missing hash")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams error, got %v", err.Code)
		}
	})

	t.Run("GetTxResult_NotFound", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "0x1234567890abcdef"}`)
		_, err := server.HandleMethodDirect(ctx, "getTxResult", params)
		if err == nil {
			t.Error("expected error when transaction not found")
		}
	})

	t.Run("GetTxReceipt_InvalidParams", func(t *testing.T) {
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getTxReceipt", params)
		if err == nil {
			t.Fatal("expected error for missing hash")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams error, got %v", err.Code)
		}
	})

	t.Run("GetTxReceipt_NotFound", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "0x1234567890abcdef"}`)
		_, err := server.HandleMethodDirect(ctx, "getTxReceipt", params)
		if err == nil {
			t.Error("expected error when receipt not found")
		}
	})

	t.Run("ParseError", func(t *testing.T) {
		params := json.RawMessage(`invalid json`)
		_, err := server.HandleMethodDirect(ctx, "getBlock", params)
		if err == nil {
			t.Error("expected parse error")
		}
	})

	// Success cases
	t.Run("GetBlock_Success", func(t *testing.T) {
		params := json.RawMessage(`{"number": 1}`)
		result, err := server.HandleMethodDirect(ctx, "getBlock", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}
	})

	t.Run("GetBlockByHash_Success", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "` + testBlock.Hash().Hex() + `"}`)
		result, err := server.HandleMethodDirect(ctx, "getBlockByHash", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}
	})

	t.Run("GetTxResult_Success", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "` + testTx.Hash().Hex() + `"}`)
		result, err := serverWithData.HandleMethodDirect(ctx, "getTxResult", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}
	})

	t.Run("GetTxReceipt_Success", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "` + testTx.Hash().Hex() + `"}`)
		result, err := serverWithData.HandleMethodDirect(ctx, "getTxReceipt", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}
	})

	t.Run("GetLatestHeight_Error", func(t *testing.T) {
		errorStore := &mockStorageWithErrors{}
		errorServer := NewServer(errorStore, logger)
		params := json.RawMessage(`{}`)
		_, err := errorServer.HandleMethodDirect(ctx, "getLatestHeight", params)
		if err == nil {
			t.Fatal("expected error when storage fails")
		}
		if err.Code != InternalError {
			t.Errorf("expected InternalError, got %v", err.Code)
		}
	})

	t.Run("GetBlock_StorageError", func(t *testing.T) {
		errorStore := &mockStorageWithErrors{}
		errorServer := NewServer(errorStore, logger)
		params := json.RawMessage(`{"number": 1}`)
		_, err := errorServer.HandleMethodDirect(ctx, "getBlock", params)
		if err == nil {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("GetBlockByHash_StorageError", func(t *testing.T) {
		errorStore := &mockStorageWithErrors{}
		errorServer := NewServer(errorStore, logger)
		params := json.RawMessage(`{"hash": "0x123"}`)
		_, err := errorServer.HandleMethodDirect(ctx, "getBlockByHash", params)
		if err == nil {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("GetTxResult_StorageError", func(t *testing.T) {
		errorStore := &mockStorageWithErrors{}
		errorServer := NewServer(errorStore, logger)
		params := json.RawMessage(`{"hash": "0x123"}`)
		_, err := errorServer.HandleMethodDirect(ctx, "getTxResult", params)
		if err == nil {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("GetTxReceipt_StorageError", func(t *testing.T) {
		errorStore := &mockStorageWithErrors{}
		errorServer := NewServer(errorStore, logger)
		params := json.RawMessage(`{"hash": "0x123"}`)
		_, err := errorServer.HandleMethodDirect(ctx, "getTxReceipt", params)
		if err == nil {
			t.Error("expected error when storage fails")
		}
	})

	// Test hash validation in getBlock
	t.Run("GetBlock_InvalidHashFormat", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "invalid-hash"}`)
		_, err := server.HandleMethodDirect(ctx, "getBlock", params)
		if err == nil {
			t.Fatal("expected error for invalid hash format")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	// Test JSON conversion with different transaction types
	t.Run("TransactionToJSON_DynamicFeeTx", func(t *testing.T) {
		// Create EIP-1559 transaction
		toAddr := common.HexToAddress("0x456")
		dynamicTx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   common.Big1,
			Nonce:     0,
			GasTipCap: common.Big1,
			GasFeeCap: common.Big2,
			Gas:       21000,
			To:        &toAddr,
			Value:     common.Big1,
			Data:      []byte{},
		})

		storeWithDynamicTx := &mockStorageWithData{
			mockStorage:  store,
			transactions: map[common.Hash]*types.Transaction{dynamicTx.Hash(): dynamicTx},
			receipts:     map[common.Hash]*types.Receipt{},
		}

		serverWithDynamicTx := NewServer(storeWithDynamicTx, logger)
		params := json.RawMessage(`{"hash": "` + dynamicTx.Hash().Hex() + `"}`)
		result, err := serverWithDynamicTx.HandleMethodDirect(ctx, "getTxResult", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}

		// Verify type field
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}
		txType, ok := resultMap["type"].(string)
		if !ok {
			t.Error("expected type field in result")
		}
		if txType != "0x2" {
			t.Errorf("expected type 0x2 for EIP-1559, got %s", txType)
		}
	})

	t.Run("TransactionToJSON_AccessListTx", func(t *testing.T) {
		// Create EIP-2930 transaction with access list
		toAddr := common.HexToAddress("0x456")
		accessList := types.AccessList{
			types.AccessTuple{
				Address: common.HexToAddress("0xabc"),
				StorageKeys: []common.Hash{
					common.HexToHash("0x123"),
					common.HexToHash("0x456"),
				},
			},
		}
		accessListTx := types.NewTx(&types.AccessListTx{
			ChainID:    common.Big1,
			Nonce:      0,
			GasPrice:   common.Big1,
			Gas:        21000,
			To:         &toAddr,
			Value:      common.Big1,
			Data:       []byte{},
			AccessList: accessList,
		})

		storeWithAccessListTx := &mockStorageWithData{
			mockStorage:  store,
			transactions: map[common.Hash]*types.Transaction{accessListTx.Hash(): accessListTx},
			receipts:     map[common.Hash]*types.Receipt{},
		}

		serverWithAccessListTx := NewServer(storeWithAccessListTx, logger)
		params := json.RawMessage(`{"hash": "` + accessListTx.Hash().Hex() + `"}`)
		result, err := serverWithAccessListTx.HandleMethodDirect(ctx, "getTxResult", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}

		// Verify access list serialization
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}
		accessListField, ok := resultMap["accessList"]
		if !ok {
			t.Error("expected accessList field in result")
		}
		if accessListField == nil {
			t.Error("expected non-nil access list")
		}
	})

	// Test receipt with logs
	t.Run("ReceiptToJSON_WithLogs", func(t *testing.T) {
		receiptWithLogs := &types.Receipt{
			TxHash:            testTx.Hash(),
			Status:            1,
			CumulativeGasUsed: 21000,
			GasUsed:           21000,
			Logs: []*types.Log{
				{
					Address: common.HexToAddress("0xabc"),
					Topics: []common.Hash{
						common.HexToHash("0x123"),
					},
					Data:        []byte{1, 2, 3},
					BlockNumber: 1,
				},
			},
			BlockNumber:       common.Big1,
			BlockHash:         testBlock.Hash(),
			EffectiveGasPrice: common.Big1,
		}

		storeWithLogs := &mockStorageWithData{
			mockStorage:  store,
			transactions: map[common.Hash]*types.Transaction{testTx.Hash(): testTx},
			receipts:     map[common.Hash]*types.Receipt{testTx.Hash(): receiptWithLogs},
		}

		serverWithLogs := NewServer(storeWithLogs, logger)
		params := json.RawMessage(`{"hash": "` + testTx.Hash().Hex() + `"}`)
		result, err := serverWithLogs.HandleMethodDirect(ctx, "getTxReceipt", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Verify logs are included
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}
		logs, ok := resultMap["logs"]
		if !ok {
			t.Error("expected logs field in result")
		}
		if logs == nil {
			t.Error("expected non-nil logs")
		}
	})
}

// mockStorageWithErrors returns errors for testing error paths
type mockStorageWithErrors struct {
}

func (m *mockStorageWithErrors) GetLatestHeight(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) gethGetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *port.TxLocation, error) {
	return nil, nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) gethGetTransactions(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, []*port.TxLocation, error) {
	return nil, nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) offsetGetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) HasBlock(ctx context.Context, height uint64) (bool, error) {
	return false, port.ErrNotFound
}

func (m *mockStorageWithErrors) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	return false, port.ErrNotFound
}

func (m *mockStorageWithErrors) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	return false, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) SetLatestHeight(ctx context.Context, height uint64) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) gethSetBlock(ctx context.Context, block *types.Block) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetTransaction(ctx context.Context, tx *types.Transaction, location *port.TxLocation) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) gethSetReceipt(ctx context.Context, receipt *types.Receipt) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetBlocks(ctx context.Context, blocks []*types.Block) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteBlock(ctx context.Context, height uint64) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) Close() error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) Compact(ctx context.Context, start, end []byte) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetABI(ctx context.Context, address common.Address, abiJSON []byte) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteABI(ctx context.Context, address common.Address) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) ListABIs(ctx context.Context) ([]common.Address, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) HasABI(ctx context.Context, address common.Address) (bool, error) {
	return false, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetLogs(ctx context.Context, filter *port.LogFilter) ([]*types.Log, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) SaveLog(ctx context.Context, log *types.Log) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SaveLogs(ctx context.Context, logs []*types.Log) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteLogsByBlock(ctx context.Context, blockNumber uint64) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) gethIndexLogs(ctx context.Context, logs []*types.Log) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) gethIndexLog(ctx context.Context, log *types.Log) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]port.SearchResult, error) {
	return nil, port.ErrNotFound
}

// Contract verification methods
func (m *mockStorageWithErrors) GetContractVerification(ctx context.Context, address common.Address) (*port.ContractVerification, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	return false, port.ErrNotFound
}

func (m *mockStorageWithErrors) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) CountVerifiedContracts(ctx context.Context) (int, error) {
	return 0, port.ErrNotFound
}

func (m *mockStorageWithErrors) SetContractVerification(ctx context.Context, verification *port.ContractVerification) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteContractVerification(ctx context.Context, address common.Address) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) GetAddressStats(ctx context.Context, addr common.Address) (*port.AddressStats, error) {
	return nil, port.ErrNotFound
}

// HistoricalReader methods for mockStorageWithErrors
func (m *mockStorageWithErrors) gethGetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, limit, offset int) ([]*port.TransactionWithReceipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]port.BalanceSnapshot, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetBlockCount(ctx context.Context) (uint64, error) {
	return 0, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetTransactionCount(ctx context.Context) (uint64, error) {
	return 0, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.MinerStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]port.TokenBalance, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*port.GasStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*port.AddressGasStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressGasStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressActivityStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*port.NetworkMetrics, error) {
	return nil, port.ErrNotFound
}

// HistoricalWriter methods for mockStorageWithErrors
func (m *mockStorageWithErrors) SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	return port.ErrNotFound
}

// KVStore interface methods for mockStorageWithErrors
func (m *mockStorageWithErrors) Put(ctx context.Context, key, value []byte) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) Get(ctx context.Context, key []byte) ([]byte, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) Delete(ctx context.Context, key []byte) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) Has(ctx context.Context, key []byte) (bool, error) {
	return false, port.ErrNotFound
}

// TokenMetadataReader interface methods for mockStorageWithErrors
func (m *mockStorageWithErrors) GetTokenMetadata(ctx context.Context, address common.Address) (*port.TokenMetadata, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) ListTokensByStandard(ctx context.Context, standard port.TokenStandard, limit, offset int) ([]*port.TokenMetadata, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetTokensCount(ctx context.Context, standard port.TokenStandard) (int, error) {
	return 0, port.ErrNotFound
}

func (m *mockStorageWithErrors) SearchTokens(ctx context.Context, query string, limit int) ([]*port.TokenMetadata, error) {
	return nil, port.ErrNotFound
}

// TokenMetadataWriter interface methods for mockStorageWithErrors
func (m *mockStorageWithErrors) SaveTokenMetadata(ctx context.Context, metadata *port.TokenMetadata) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteTokenMetadata(ctx context.Context, address common.Address) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetTokenMetadataFetcher(fetcher port.TokenMetadataFetcher) {
}

// SetCodeIndexReader methods for mockStorageWithErrors
func (m *mockStorageWithErrors) GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*port.SetCodeAuthorizationRecord, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) offsetGetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) offsetGetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByBlock(ctx context.Context, blockNumber uint64) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetAddressSetCodeStats(ctx context.Context, address common.Address) (*port.AddressSetCodeStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetAddressDelegationState(ctx context.Context, address common.Address) (*port.AddressDelegationState, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error) {
	return 0, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetSetCodeAuthorizationsCountByAuthority(ctx context.Context, authority common.Address) (int, error) {
	return 0, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetSetCodeTransactionCount(ctx context.Context) (int, error) {
	return 0, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetRecentSetCodeAuthorizations(ctx context.Context, limit int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, port.ErrNotFound
}

// SetCodeIndexWriter methods for mockStorageWithErrors
func (m *mockStorageWithErrors) SaveSetCodeAuthorization(ctx context.Context, record *port.SetCodeAuthorizationRecord) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SaveSetCodeAuthorizations(ctx context.Context, records []*port.SetCodeAuthorizationRecord) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) UpdateAddressDelegationState(ctx context.Context, state *port.AddressDelegationState) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) IncrementSetCodeStats(ctx context.Context, address common.Address, asTarget, asAuthority bool, blockNumber uint64) error {
	return port.ErrNotFound
}

// UserOpIndexReader methods for mockStorageWithErrors
func (m *mockStorageWithErrors) GetUserOp(ctx context.Context, opHash common.Hash) (*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetUserOpsBySender(ctx context.Context, sender common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetUserOpsByBundler(ctx context.Context, bundler common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetUserOpsByFactory(ctx context.Context, factory common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetBundlerStats(ctx context.Context, bundler common.Address) (*userop.BundlerStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetFactoryStats(ctx context.Context, factory common.Address) (*userop.FactoryStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetPaymasterStats(ctx context.Context, paymaster common.Address) (*userop.PaymasterStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetSmartAccount(ctx context.Context, address common.Address) (*userop.SmartAccount, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetRecentUserOps(ctx context.Context, limit int) ([]*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetUserOpCount(ctx context.Context) (int, error) {
	return 0, port.ErrNotFound
}
func (m *mockStorageWithErrors) ListBundlers(ctx context.Context, limit, offset int) ([]*userop.BundlerStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) ListFactories(ctx context.Context, limit, offset int) ([]*userop.FactoryStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) ListPaymasters(ctx context.Context, limit, offset int) ([]*userop.PaymasterStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) ListSmartAccounts(ctx context.Context, limit, offset int) ([]*userop.SmartAccount, error) {
	return nil, port.ErrNotFound
}

// UserOpIndexWriter methods for mockStorageWithErrors
func (m *mockStorageWithErrors) SaveUserOp(ctx context.Context, op *userop.UserOperation) error {
	return port.ErrNotFound
}
func (m *mockStorageWithErrors) SaveUserOps(ctx context.Context, ops []*userop.UserOperation) error {
	return port.ErrNotFound
}
func (m *mockStorageWithErrors) UpdateBundlerStats(ctx context.Context, stats *userop.BundlerStats) error {
	return port.ErrNotFound
}
func (m *mockStorageWithErrors) UpdateFactoryStats(ctx context.Context, stats *userop.FactoryStats) error {
	return port.ErrNotFound
}
func (m *mockStorageWithErrors) UpdatePaymasterStats(ctx context.Context, stats *userop.PaymasterStats) error {
	return port.ErrNotFound
}
func (m *mockStorageWithErrors) SaveSmartAccount(ctx context.Context, account *userop.SmartAccount) error {
	return port.ErrNotFound
}

// ModuleIndexReader methods for mockStorageWithErrors
func (m *mockStorageWithErrors) GetInstalledModule(ctx context.Context, account, module common.Address) (*port.InstalledModule, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) offsetGetModulesByAccount(ctx context.Context, account common.Address, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) offsetGetModulesByType(ctx context.Context, moduleType port.ModuleType, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetModuleStats(ctx context.Context, module common.Address) (*port.ModuleStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetAccountModules(ctx context.Context, account common.Address) (*port.AccountModules, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetRecentModuleEvents(ctx context.Context, limit int) ([]*port.InstalledModule, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) GetModuleEventCount(ctx context.Context) (int, error) {
	return 0, port.ErrNotFound
}
func (m *mockStorageWithErrors) offsetListModuleStats(ctx context.Context, limit, offset int) ([]*port.ModuleStats, error) {
	return nil, port.ErrNotFound
}

// ModuleIndexWriter methods for mockStorageWithErrors
func (m *mockStorageWithErrors) SaveInstalledModule(ctx context.Context, record *port.InstalledModule) error {
	return port.ErrNotFound
}
func (m *mockStorageWithErrors) RemoveModule(ctx context.Context, account, module common.Address, blockNumber uint64, txHash common.Hash) error {
	return port.ErrNotFound
}
func (m *mockStorageWithErrors) UpdateModuleStats(ctx context.Context, stats *port.ModuleStats) error {
	return port.ErrNotFound
}

// mockStorageWithNonNotFoundErrors returns non-ErrNotFound errors to test logging paths
type mockStorageWithNonNotFoundErrors struct {
}

func (m *mockStorageWithNonNotFoundErrors) GetLatestHeight(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *port.TxLocation, error) {
	return nil, nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) gethGetTransactions(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, []*port.TxLocation, error) {
	return nil, nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) offsetGetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) HasBlock(ctx context.Context, height uint64) (bool, error) {
	return false, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	return false, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	return false, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SetLatestHeight(ctx context.Context, height uint64) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethSetBlock(ctx context.Context, block *types.Block) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SetTransaction(ctx context.Context, tx *types.Transaction, location *port.TxLocation) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethSetReceipt(ctx context.Context, receipt *types.Receipt) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SetBlocks(ctx context.Context, blocks []*types.Block) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) DeleteBlock(ctx context.Context, height uint64) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) Close() error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) Compact(ctx context.Context, start, end []byte) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SetABI(ctx context.Context, address common.Address, abiJSON []byte) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) DeleteABI(ctx context.Context, address common.Address) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) ListABIs(ctx context.Context) ([]common.Address, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) HasABI(ctx context.Context, address common.Address) (bool, error) {
	return false, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetLogs(ctx context.Context, filter *port.LogFilter) ([]*types.Log, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SaveLog(ctx context.Context, log *types.Log) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SaveLogs(ctx context.Context, logs []*types.Log) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) DeleteLogsByBlock(ctx context.Context, blockNumber uint64) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethIndexLogs(ctx context.Context, logs []*types.Log) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethIndexLog(ctx context.Context, log *types.Log) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]port.SearchResult, error) {
	return nil, fmt.Errorf("database connection failed")
}

// Contract verification methods
func (m *mockStorageWithNonNotFoundErrors) GetContractVerification(ctx context.Context, address common.Address) (*port.ContractVerification, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	return false, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) CountVerifiedContracts(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SetContractVerification(ctx context.Context, verification *port.ContractVerification) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) DeleteContractVerification(ctx context.Context, address common.Address) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetAddressStats(ctx context.Context, addr common.Address) (*port.AddressStats, error) {
	return nil, fmt.Errorf("database connection failed")
}

// HistoricalReader methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) gethGetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) gethGetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, limit, offset int) ([]*port.TransactionWithReceipt, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]port.BalanceSnapshot, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetBlockCount(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetTransactionCount(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.MinerStats, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]port.TokenBalance, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*port.GasStats, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*port.AddressGasStats, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressGasStats, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressActivityStats, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*port.NetworkMetrics, error) {
	return nil, fmt.Errorf("database connection failed")
}

// HistoricalWriter methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	return fmt.Errorf("database connection failed")
}

// KVStore interface methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) Put(ctx context.Context, key, value []byte) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) Get(ctx context.Context, key []byte) ([]byte, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) Delete(ctx context.Context, key []byte) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) Has(ctx context.Context, key []byte) (bool, error) {
	return false, fmt.Errorf("database connection failed")
}

// TokenMetadataReader interface methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) GetTokenMetadata(ctx context.Context, address common.Address) (*port.TokenMetadata, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) ListTokensByStandard(ctx context.Context, standard port.TokenStandard, limit, offset int) ([]*port.TokenMetadata, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetTokensCount(ctx context.Context, standard port.TokenStandard) (int, error) {
	return 0, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SearchTokens(ctx context.Context, query string, limit int) ([]*port.TokenMetadata, error) {
	return nil, fmt.Errorf("database connection failed")
}

// TokenMetadataWriter interface methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) SaveTokenMetadata(ctx context.Context, metadata *port.TokenMetadata) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) DeleteTokenMetadata(ctx context.Context, address common.Address) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SetTokenMetadataFetcher(fetcher port.TokenMetadataFetcher) {
}

// SetCodeIndexReader methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) offsetGetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) offsetGetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetSetCodeAuthorizationsByBlock(ctx context.Context, blockNumber uint64) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetAddressSetCodeStats(ctx context.Context, address common.Address) (*port.AddressSetCodeStats, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetAddressDelegationState(ctx context.Context, address common.Address) (*port.AddressDelegationState, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error) {
	return 0, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetSetCodeAuthorizationsCountByAuthority(ctx context.Context, authority common.Address) (int, error) {
	return 0, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetSetCodeTransactionCount(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetRecentSetCodeAuthorizations(ctx context.Context, limit int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("database connection failed")
}

// SetCodeIndexWriter methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) SaveSetCodeAuthorization(ctx context.Context, record *port.SetCodeAuthorizationRecord) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) SaveSetCodeAuthorizations(ctx context.Context, records []*port.SetCodeAuthorizationRecord) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) UpdateAddressDelegationState(ctx context.Context, state *port.AddressDelegationState) error {
	return fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) IncrementSetCodeStats(ctx context.Context, address common.Address, asTarget, asAuthority bool, blockNumber uint64) error {
	return fmt.Errorf("database connection failed")
}

// UserOpIndexReader methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) GetUserOp(ctx context.Context, opHash common.Hash) (*userop.UserOperation, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetUserOpsBySender(ctx context.Context, sender common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetUserOpsByBundler(ctx context.Context, bundler common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetUserOpsByFactory(ctx context.Context, factory common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetBundlerStats(ctx context.Context, bundler common.Address) (*userop.BundlerStats, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetFactoryStats(ctx context.Context, factory common.Address) (*userop.FactoryStats, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetPaymasterStats(ctx context.Context, paymaster common.Address) (*userop.PaymasterStats, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetSmartAccount(ctx context.Context, address common.Address) (*userop.SmartAccount, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetRecentUserOps(ctx context.Context, limit int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetUserOpCount(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) ListBundlers(ctx context.Context, limit, offset int) ([]*userop.BundlerStats, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) ListFactories(ctx context.Context, limit, offset int) ([]*userop.FactoryStats, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) ListPaymasters(ctx context.Context, limit, offset int) ([]*userop.PaymasterStats, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) ListSmartAccounts(ctx context.Context, limit, offset int) ([]*userop.SmartAccount, error) {
	return nil, fmt.Errorf("database connection failed")
}

// UserOpIndexWriter methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) SaveUserOp(ctx context.Context, op *userop.UserOperation) error {
	return fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) SaveUserOps(ctx context.Context, ops []*userop.UserOperation) error {
	return fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) UpdateBundlerStats(ctx context.Context, stats *userop.BundlerStats) error {
	return fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) UpdateFactoryStats(ctx context.Context, stats *userop.FactoryStats) error {
	return fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) UpdatePaymasterStats(ctx context.Context, stats *userop.PaymasterStats) error {
	return fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) SaveSmartAccount(ctx context.Context, account *userop.SmartAccount) error {
	return fmt.Errorf("database connection failed")
}

// ModuleIndexReader methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) GetInstalledModule(ctx context.Context, account, module common.Address) (*port.InstalledModule, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) offsetGetModulesByAccount(ctx context.Context, account common.Address, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) offsetGetModulesByType(ctx context.Context, moduleType port.ModuleType, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetModuleStats(ctx context.Context, module common.Address) (*port.ModuleStats, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetAccountModules(ctx context.Context, account common.Address) (*port.AccountModules, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetRecentModuleEvents(ctx context.Context, limit int) ([]*port.InstalledModule, error) {
	return nil, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) GetModuleEventCount(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) offsetListModuleStats(ctx context.Context, limit, offset int) ([]*port.ModuleStats, error) {
	return nil, fmt.Errorf("database connection failed")
}

// ModuleIndexWriter methods for mockStorageWithNonNotFoundErrors
func (m *mockStorageWithNonNotFoundErrors) SaveInstalledModule(ctx context.Context, record *port.InstalledModule) error {
	return fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) RemoveModule(ctx context.Context, account, module common.Address, blockNumber uint64, txHash common.Hash) error {
	return fmt.Errorf("database connection failed")
}
func (m *mockStorageWithNonNotFoundErrors) UpdateModuleStats(ctx context.Context, stats *port.ModuleStats) error {
	return fmt.Errorf("database connection failed")
}

func TestJSONRPCServerEdgeCases(t *testing.T) {
	logger := zap.NewNop()
	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	server := NewServer(store, logger)

	t.Run("InvalidJSONRequest", func(t *testing.T) {
		reqBody := `invalid json`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %v", w.Code)
		}

		var resp Response
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Error == nil {
			t.Error("expected error for invalid JSON")
		}
		if resp.Error.Code != ParseError {
			t.Errorf("expected ParseError, got %v", resp.Error.Code)
		}
	})

	t.Run("ErrorResponseLogging", func(t *testing.T) {
		// Test error response with logging
		reqBody := `{"jsonrpc":"2.0","method":"invalidMethod","params":{},"id":1}`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Error == nil {
			t.Error("expected error response")
		}
	})
}

func TestJSONRPCBatchEdgeCases(t *testing.T) {
	logger := zap.NewNop()
	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	server := NewServer(store, logger)

	t.Run("EmptyBatch", func(t *testing.T) {
		reqBody := `[]`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Error == nil {
			t.Error("expected error for empty batch")
		}
		if resp.Error.Code != InvalidRequest {
			t.Errorf("expected InvalidRequest error, got %v", resp.Error.Code)
		}
	})

	t.Run("MixedValidInvalid", func(t *testing.T) {
		reqBody := `[
			{"jsonrpc":"2.0","method":"getLatestHeight","params":{},"id":1},
			{"jsonrpc":"1.0","method":"getLatestHeight","params":{},"id":2},
			{"jsonrpc":"2.0","params":{},"id":3}
		]`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var batch BatchResponse
		if err := json.NewDecoder(w.Body).Decode(&batch); err != nil {
			t.Fatalf("failed to decode batch response: %v", err)
		}

		if len(batch) != 3 {
			t.Errorf("expected 3 responses, got %v", len(batch))
		}

		// First should succeed
		if batch[0].Error != nil {
			t.Errorf("first request should succeed, got error: %v", batch[0].Error)
		}

		// Second should fail (invalid jsonrpc version)
		if batch[1].Error == nil {
			t.Error("second request should fail")
		}

		// Third should fail (missing method)
		if batch[2].Error == nil {
			t.Error("third request should fail")
		}
	})

	t.Run("ParseError", func(t *testing.T) {
		reqBody := `not valid json`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Error == nil {
			t.Error("expected parse error")
		}
		if resp.Error.Code != ParseError {
			t.Errorf("expected ParseError, got %v", resp.Error.Code)
		}
	})

	t.Run("InvalidBatchJSON", func(t *testing.T) {
		reqBody := `[invalid json]`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Error == nil {
			t.Error("expected parse error for invalid batch")
		}
		if resp.Error.Code != ParseError {
			t.Errorf("expected ParseError, got %v", resp.Error.Code)
		}
	})
}

func TestTransactionJSONConversion(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	t.Run("ContractCreation_NilTo", func(t *testing.T) {
		// Contract creation transaction (To is nil)
		contractTx := types.NewContractCreation(
			0,
			common.Big1,
			21000,
			common.Big1,
			[]byte{0x60, 0x60, 0x60}, // contract bytecode
		)

		store := &mockStorageWithData{
			mockStorage: &mockStorage{
				latestHeight: 1,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
			transactions: map[common.Hash]*types.Transaction{contractTx.Hash(): contractTx},
			receipts:     map[common.Hash]*types.Receipt{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"hash": "` + contractTx.Hash().Hex() + `"}`)
		result, err := server.HandleMethodDirect(ctx, "getTxResult", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Verify To field is null for contract creation
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}
		// To field should exist and be nil for contract creation
		if _, exists := resultMap["to"]; !exists {
			t.Error("expected 'to' field in result")
		}
	})

	t.Run("TransactionWithZeroValue", func(t *testing.T) {
		toAddr := common.HexToAddress("0x456")
		zeroValueTx := types.NewTransaction(
			0,
			toAddr,
			common.Big0, // zero value
			21000,
			common.Big1,
			nil,
		)

		store := &mockStorageWithData{
			mockStorage: &mockStorage{
				latestHeight: 1,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
			transactions: map[common.Hash]*types.Transaction{zeroValueTx.Hash(): zeroValueTx},
			receipts:     map[common.Hash]*types.Receipt{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"hash": "` + zeroValueTx.Hash().Hex() + `"}`)
		result, err := server.HandleMethodDirect(ctx, "getTxResult", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}
	})

	// Test receipt with contract address
	t.Run("ReceiptWithContractAddress", func(t *testing.T) {
		contractTx := types.NewContractCreation(0, common.Big1, 21000, common.Big1, []byte{0x60})
		contractAddress := common.HexToAddress("0xcontract123")

		contractReceipt := &types.Receipt{
			TxHash:            contractTx.Hash(),
			Status:            1,
			CumulativeGasUsed: 21000,
			GasUsed:           21000,
			Logs:              []*types.Log{},
			BlockNumber:       common.Big1,
			BlockHash:         common.HexToHash("0xblock123"),
			EffectiveGasPrice: common.Big1,
			ContractAddress:   contractAddress,
		}

		store := &mockStorageWithData{
			mockStorage: &mockStorage{
				latestHeight: 1,
				blocks:       make(map[uint64]*types.Block),
				blocksByHash: make(map[common.Hash]*types.Block),
			},
			transactions: map[common.Hash]*types.Transaction{},
			receipts:     map[common.Hash]*types.Receipt{contractTx.Hash(): contractReceipt},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"hash": "` + contractTx.Hash().Hex() + `"}`)
		result, err := server.HandleMethodDirect(ctx, "getTxReceipt", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Verify contract address is included
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}
		contractAddr, ok := resultMap["contractAddress"]
		if !ok {
			t.Error("expected contractAddress field")
		}
		if contractAddr == nil {
			t.Error("expected non-nil contract address")
		}
	})
}

func TestJSONRPCErrorLogging(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	// Use non-NotFound errors to trigger logging paths
	errorStore := &mockStorageWithNonNotFoundErrors{}
	server := NewServer(errorStore, logger)

	t.Run("GetBlock_DatabaseError", func(t *testing.T) {
		params := json.RawMessage(`{"number": 1}`)
		_, err := server.HandleMethodDirect(ctx, "getBlock", params)
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Code != InternalError {
			t.Errorf("expected InternalError, got %v", err.Code)
		}
	})

	t.Run("GetBlockByHash_DatabaseError", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "0x123"}`)
		_, err := server.HandleMethodDirect(ctx, "getBlockByHash", params)
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Code != InternalError {
			t.Errorf("expected InternalError, got %v", err.Code)
		}
	})

	t.Run("GetTxResult_DatabaseError", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "0x123"}`)
		_, err := server.HandleMethodDirect(ctx, "getTxResult", params)
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Code != InternalError {
			t.Errorf("expected InternalError, got %v", err.Code)
		}
	})

	t.Run("GetTxReceipt_DatabaseError", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "0x123"}`)
		_, err := server.HandleMethodDirect(ctx, "getTxReceipt", params)
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Code != InternalError {
			t.Errorf("expected InternalError, got %v", err.Code)
		}
	})
}

func (m *mockStorage) GetBlock(ctx context.Context, height uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlock(ctx, height))
}

func (m *mockStorage) GetBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByHash(ctx, hash))
}

func (m *mockStorage) GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *port.TxLocation, error) {
	return modelTxOf(m.gethGetTransaction(ctx, hash))
}

func (m *mockStorage) GetTransactions(ctx context.Context, hashes []common.Hash) ([]*model.Transaction, []*port.TxLocation, error) {
	return modelTxsOf(m.gethGetTransactions(ctx, hashes))
}

func (m *mockStorage) GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error) {
	return modelReceiptOf(m.gethGetReceipt(ctx, hash))
}

func (m *mockStorage) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceipts(ctx, hashes))
}

func (m *mockStorage) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockHash(ctx, blockHash))
}

func (m *mockStorage) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockNumber(ctx, blockNumber))
}

func (m *mockStorage) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocks(ctx, startHeight, endHeight))
}

func (m *mockStorage) SetBlock(ctx context.Context, b *model.Block) error {
	gb, err := gethconv.BlockToGeth(b)
	if err != nil {
		return err
	}
	return m.gethSetBlock(ctx, gb)
}

func (m *mockStorage) SetReceipt(ctx context.Context, r *model.Receipt) error {
	return m.gethSetReceipt(ctx, gethconv.ReceiptToGeth(r))
}

func (m *mockStorage) GetLogs(ctx context.Context, filter *port.LogFilter) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogs(ctx, filter))
}

func (m *mockStorage) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByBlock(ctx, blockNumber))
}

func (m *mockStorage) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByAddress(ctx, address, fromBlock, toBlock))
}

func (m *mockStorage) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByTopic(ctx, topic, topicIndex, fromBlock, toBlock))
}

func (m *mockStorage) IndexLogs(ctx context.Context, logs []*model.Log) error {
	return m.gethIndexLogs(ctx, gethconv.LogsToGeth(logs))
}

func (m *mockStorage) IndexLog(ctx context.Context, log *model.Log) error {
	return m.gethIndexLog(ctx, gethconv.LogToGeth(log))
}

func (m *mockStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocksByTimeRange(ctx, fromTime, toTime, limit, offset))
}

func (m *mockStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByTimestamp(ctx, timestamp))
}

func (m *mockStorageWithData) GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *port.TxLocation, error) {
	return modelTxOf(m.gethGetTransaction(ctx, hash))
}

func (m *mockStorageWithData) GetTransactions(ctx context.Context, hashes []common.Hash) ([]*model.Transaction, []*port.TxLocation, error) {
	return modelTxsOf(m.gethGetTransactions(ctx, hashes))
}

func (m *mockStorageWithData) GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error) {
	return modelReceiptOf(m.gethGetReceipt(ctx, hash))
}

func (m *mockStorageWithErrors) GetBlock(ctx context.Context, height uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlock(ctx, height))
}

func (m *mockStorageWithErrors) GetBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByHash(ctx, hash))
}

func (m *mockStorageWithErrors) GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *port.TxLocation, error) {
	return modelTxOf(m.gethGetTransaction(ctx, hash))
}

func (m *mockStorageWithErrors) GetTransactions(ctx context.Context, hashes []common.Hash) ([]*model.Transaction, []*port.TxLocation, error) {
	return modelTxsOf(m.gethGetTransactions(ctx, hashes))
}

func (m *mockStorageWithErrors) GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error) {
	return modelReceiptOf(m.gethGetReceipt(ctx, hash))
}

func (m *mockStorageWithErrors) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceipts(ctx, hashes))
}

func (m *mockStorageWithErrors) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockHash(ctx, blockHash))
}

func (m *mockStorageWithErrors) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockNumber(ctx, blockNumber))
}

func (m *mockStorageWithErrors) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocks(ctx, startHeight, endHeight))
}

func (m *mockStorageWithErrors) SetBlock(ctx context.Context, b *model.Block) error {
	gb, err := gethconv.BlockToGeth(b)
	if err != nil {
		return err
	}
	return m.gethSetBlock(ctx, gb)
}

func (m *mockStorageWithErrors) SetReceipt(ctx context.Context, r *model.Receipt) error {
	return m.gethSetReceipt(ctx, gethconv.ReceiptToGeth(r))
}

func (m *mockStorageWithErrors) GetLogs(ctx context.Context, filter *port.LogFilter) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogs(ctx, filter))
}

func (m *mockStorageWithErrors) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByBlock(ctx, blockNumber))
}

func (m *mockStorageWithErrors) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByAddress(ctx, address, fromBlock, toBlock))
}

func (m *mockStorageWithErrors) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByTopic(ctx, topic, topicIndex, fromBlock, toBlock))
}

func (m *mockStorageWithErrors) IndexLogs(ctx context.Context, logs []*model.Log) error {
	return m.gethIndexLogs(ctx, gethconv.LogsToGeth(logs))
}

func (m *mockStorageWithErrors) IndexLog(ctx context.Context, log *model.Log) error {
	return m.gethIndexLog(ctx, gethconv.LogToGeth(log))
}

func (m *mockStorageWithErrors) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocksByTimeRange(ctx, fromTime, toTime, limit, offset))
}

func (m *mockStorageWithErrors) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByTimestamp(ctx, timestamp))
}

func (m *mockStorageWithNonNotFoundErrors) GetBlock(ctx context.Context, height uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlock(ctx, height))
}

func (m *mockStorageWithNonNotFoundErrors) GetBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByHash(ctx, hash))
}

func (m *mockStorageWithNonNotFoundErrors) GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *port.TxLocation, error) {
	return modelTxOf(m.gethGetTransaction(ctx, hash))
}

func (m *mockStorageWithNonNotFoundErrors) GetTransactions(ctx context.Context, hashes []common.Hash) ([]*model.Transaction, []*port.TxLocation, error) {
	return modelTxsOf(m.gethGetTransactions(ctx, hashes))
}

func (m *mockStorageWithNonNotFoundErrors) GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error) {
	return modelReceiptOf(m.gethGetReceipt(ctx, hash))
}

func (m *mockStorageWithNonNotFoundErrors) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceipts(ctx, hashes))
}

func (m *mockStorageWithNonNotFoundErrors) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockHash(ctx, blockHash))
}

func (m *mockStorageWithNonNotFoundErrors) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockNumber(ctx, blockNumber))
}

func (m *mockStorageWithNonNotFoundErrors) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocks(ctx, startHeight, endHeight))
}

func (m *mockStorageWithNonNotFoundErrors) SetBlock(ctx context.Context, b *model.Block) error {
	gb, err := gethconv.BlockToGeth(b)
	if err != nil {
		return err
	}
	return m.gethSetBlock(ctx, gb)
}

func (m *mockStorageWithNonNotFoundErrors) SetReceipt(ctx context.Context, r *model.Receipt) error {
	return m.gethSetReceipt(ctx, gethconv.ReceiptToGeth(r))
}

func (m *mockStorageWithNonNotFoundErrors) GetLogs(ctx context.Context, filter *port.LogFilter) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogs(ctx, filter))
}

func (m *mockStorageWithNonNotFoundErrors) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByBlock(ctx, blockNumber))
}

func (m *mockStorageWithNonNotFoundErrors) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByAddress(ctx, address, fromBlock, toBlock))
}

func (m *mockStorageWithNonNotFoundErrors) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByTopic(ctx, topic, topicIndex, fromBlock, toBlock))
}

func (m *mockStorageWithNonNotFoundErrors) IndexLogs(ctx context.Context, logs []*model.Log) error {
	return m.gethIndexLogs(ctx, gethconv.LogsToGeth(logs))
}

func (m *mockStorageWithNonNotFoundErrors) IndexLog(ctx context.Context, log *model.Log) error {
	return m.gethIndexLog(ctx, gethconv.LogToGeth(log))
}

func (m *mockStorageWithNonNotFoundErrors) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocksByTimeRange(ctx, fromTime, toTime, limit, offset))
}

func (m *mockStorageWithNonNotFoundErrors) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByTimestamp(ctx, timestamp))
}

func (m *mockStorageWithNonNotFoundErrors) GetTransactionsByAddress(ctx context.Context, addr common.Address, page port.Page) ([]common.Hash, string, error) {
	items, err := m.offsetGetTransactionsByAddress(ctx, addr, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetTransactionsByAddress(ctx context.Context, addr common.Address, page port.Page) ([]common.Hash, string, error) {
	items, err := m.offsetGetTransactionsByAddress(ctx, addr, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetTransactionsByAddress(ctx context.Context, addr common.Address, page port.Page) ([]common.Hash, string, error) {
	items, err := m.offsetGetTransactionsByAddress(ctx, addr, page.Limit, page.Offset)
	return items, "", err
}
