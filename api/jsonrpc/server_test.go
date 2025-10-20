package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/wemix-blockchain/indexer-go/storage"
	"go.uber.org/zap"
)

// mockStorage is a mock implementation of storage.Storage for testing
type mockStorage struct {
	storage.Storage
	latestHeight uint64
	blocks       map[uint64]*types.Block
	blocksByHash map[common.Hash]*types.Block
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
	return nil, nil, storage.ErrNotFound
}

func (m *mockStorage) GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return nil, storage.ErrNotFound
}

// mockStorageWithData extends mockStorage with transaction and receipt data
type mockStorageWithData struct {
	*mockStorage
	transactions map[common.Hash]*types.Transaction
	receipts     map[common.Hash]*types.Receipt
}

func (m *mockStorageWithData) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *storage.TxLocation, error) {
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

func (m *mockStorageWithData) GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	if receipt, ok := m.receipts[hash]; ok {
		return receipt, nil
	}
	return nil, storage.ErrNotFound
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
	testBlock := types.NewBlock(header, nil, nil, nil)

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
		mockStorage: store,
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
			t.Error("expected error for missing block number")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams error, got %v", err.Code)
		}
	})

	t.Run("GetBlock_InvalidParams_WrongType", func(t *testing.T) {
		params := json.RawMessage(`{"number": true}`)
		_, err := server.HandleMethodDirect(ctx, "getBlock", params)
		if err == nil {
			t.Error("expected error for invalid number type")
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
			t.Error("expected error for missing hash")
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
			t.Error("expected error for missing hash")
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
			t.Error("expected error for missing hash")
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
			t.Error("expected error when storage fails")
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
			t.Error("expected error for invalid hash format")
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
	storage.Storage
}

func (m *mockStorageWithErrors) GetLatestHeight(ctx context.Context) (uint64, error) {
	return 0, storage.ErrNotFound
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

// mockStorageWithNonNotFoundErrors returns non-ErrNotFound errors to test logging paths
type mockStorageWithNonNotFoundErrors struct {
	storage.Storage
}

func (m *mockStorageWithNonNotFoundErrors) GetLatestHeight(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *storage.TxLocation, error) {
	return nil, nil, fmt.Errorf("database connection failed")
}

func (m *mockStorageWithNonNotFoundErrors) GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return nil, fmt.Errorf("database connection failed")
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
			t.Error("expected error")
		}
		if err.Code != InternalError {
			t.Errorf("expected InternalError, got %v", err.Code)
		}
	})

	t.Run("GetBlockByHash_DatabaseError", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "0x123"}`)
		_, err := server.HandleMethodDirect(ctx, "getBlockByHash", params)
		if err == nil {
			t.Error("expected error")
		}
		if err.Code != InternalError {
			t.Errorf("expected InternalError, got %v", err.Code)
		}
	})

	t.Run("GetTxResult_DatabaseError", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "0x123"}`)
		_, err := server.HandleMethodDirect(ctx, "getTxResult", params)
		if err == nil {
			t.Error("expected error")
		}
		if err.Code != InternalError {
			t.Errorf("expected InternalError, got %v", err.Code)
		}
	})

	t.Run("GetTxReceipt_DatabaseError", func(t *testing.T) {
		params := json.RawMessage(`{"hash": "0x123"}`)
		_, err := server.HandleMethodDirect(ctx, "getTxReceipt", params)
		if err == nil {
			t.Error("expected error")
		}
		if err.Code != InternalError {
			t.Errorf("expected InternalError, got %v", err.Code)
		}
	})
}
