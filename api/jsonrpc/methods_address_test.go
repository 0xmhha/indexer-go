package jsonrpc

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// mockAddressIndexStorage extends mockStorage with address indexing support
type mockAddressIndexStorage struct {
	*mockStorage
	contractCreation         *storage.ContractCreation
	contractsByCreator       []common.Address
	internalTxs              []*storage.InternalTransaction
	internalTxsByAddress     []*storage.InternalTransaction
	erc20Transfer            *storage.ERC20Transfer
	erc20TransfersByToken    []*storage.ERC20Transfer
	erc20TransfersByAddress  []*storage.ERC20Transfer
	erc721Transfer           *storage.ERC721Transfer
	erc721TransfersByToken   []*storage.ERC721Transfer
	erc721TransfersByAddress []*storage.ERC721Transfer
	erc721Owner              common.Address
}

func (m *mockAddressIndexStorage) GetContractCreation(ctx context.Context, contractAddress common.Address) (*storage.ContractCreation, error) {
	if m.contractCreation != nil {
		return m.contractCreation, nil
	}
	// For GetContractsByCreator tests, return a valid ContractCreation for any address
	// Only if contractsByCreator is set (indicating we're testing the list functionality)
	if m.contractsByCreator != nil {
		return &storage.ContractCreation{
			ContractAddress: contractAddress,
			Creator:         common.HexToAddress("0xcreator123"),
			TransactionHash: common.HexToHash("0xtx123"),
			BlockNumber:     100,
			Timestamp:       1234567890,
		}, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockAddressIndexStorage) GetContractsByCreator(ctx context.Context, creator common.Address, limit, offset int) ([]common.Address, error) {
	if m.contractsByCreator != nil {
		start := offset
		end := offset + limit
		if start >= len(m.contractsByCreator) {
			return []common.Address{}, nil
		}
		if end > len(m.contractsByCreator) {
			end = len(m.contractsByCreator)
		}
		return m.contractsByCreator[start:end], nil
	}
	return []common.Address{}, nil
}

func (m *mockAddressIndexStorage) GetInternalTransactions(ctx context.Context, txHash common.Hash) ([]*storage.InternalTransaction, error) {
	if m.internalTxs != nil {
		return m.internalTxs, nil
	}
	return []*storage.InternalTransaction{}, nil
}

func (m *mockAddressIndexStorage) GetInternalTransactionsByAddress(ctx context.Context, address common.Address, isFrom bool, limit, offset int) ([]*storage.InternalTransaction, error) {
	if m.internalTxsByAddress != nil {
		start := offset
		end := offset + limit
		if start >= len(m.internalTxsByAddress) {
			return []*storage.InternalTransaction{}, nil
		}
		if end > len(m.internalTxsByAddress) {
			end = len(m.internalTxsByAddress)
		}
		return m.internalTxsByAddress[start:end], nil
	}
	return []*storage.InternalTransaction{}, nil
}

func (m *mockAddressIndexStorage) GetERC20Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*storage.ERC20Transfer, error) {
	if m.erc20Transfer != nil {
		return m.erc20Transfer, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockAddressIndexStorage) GetERC20TransfersByToken(ctx context.Context, tokenAddress common.Address, limit, offset int) ([]*storage.ERC20Transfer, error) {
	if m.erc20TransfersByToken != nil {
		start := offset
		end := offset + limit
		if start >= len(m.erc20TransfersByToken) {
			return []*storage.ERC20Transfer{}, nil
		}
		if end > len(m.erc20TransfersByToken) {
			end = len(m.erc20TransfersByToken)
		}
		return m.erc20TransfersByToken[start:end], nil
	}
	return []*storage.ERC20Transfer{}, nil
}

func (m *mockAddressIndexStorage) GetERC20TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, limit, offset int) ([]*storage.ERC20Transfer, error) {
	if m.erc20TransfersByAddress != nil {
		start := offset
		end := offset + limit
		if start >= len(m.erc20TransfersByAddress) {
			return []*storage.ERC20Transfer{}, nil
		}
		if end > len(m.erc20TransfersByAddress) {
			end = len(m.erc20TransfersByAddress)
		}
		return m.erc20TransfersByAddress[start:end], nil
	}
	return []*storage.ERC20Transfer{}, nil
}

func (m *mockAddressIndexStorage) GetERC721Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*storage.ERC721Transfer, error) {
	if m.erc721Transfer != nil {
		return m.erc721Transfer, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockAddressIndexStorage) GetERC721TransfersByToken(ctx context.Context, tokenAddress common.Address, limit, offset int) ([]*storage.ERC721Transfer, error) {
	if m.erc721TransfersByToken != nil {
		start := offset
		end := offset + limit
		if start >= len(m.erc721TransfersByToken) {
			return []*storage.ERC721Transfer{}, nil
		}
		if end > len(m.erc721TransfersByToken) {
			end = len(m.erc721TransfersByToken)
		}
		return m.erc721TransfersByToken[start:end], nil
	}
	return []*storage.ERC721Transfer{}, nil
}

func (m *mockAddressIndexStorage) GetERC721TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, limit, offset int) ([]*storage.ERC721Transfer, error) {
	if m.erc721TransfersByAddress != nil {
		start := offset
		end := offset + limit
		if start >= len(m.erc721TransfersByAddress) {
			return []*storage.ERC721Transfer{}, nil
		}
		if end > len(m.erc721TransfersByAddress) {
			end = len(m.erc721TransfersByAddress)
		}
		return m.erc721TransfersByAddress[start:end], nil
	}
	return []*storage.ERC721Transfer{}, nil
}

func (m *mockAddressIndexStorage) GetERC721Owner(ctx context.Context, tokenAddress common.Address, tokenId *big.Int) (common.Address, error) {
	if m.erc721Owner != (common.Address{}) {
		return m.erc721Owner, nil
	}
	return common.Address{}, storage.ErrNotFound
}

func TestAddressIndexingJSONRPCMethods(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	t.Run("GetContractCreation_Success", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			contractCreation: &storage.ContractCreation{
				ContractAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
				Creator:         common.HexToAddress("0x0987654321098765432109876543210987654321"),
				TransactionHash: common.HexToHash("0xabcdef"),
				BlockNumber:     100,
				Timestamp:       1234567890,
				BytecodeSize:    1024,
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0x1234567890123456789012345678901234567890"}`)
		result, err := server.HandleMethodDirect(ctx, "getContractCreation", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		contractAddr, ok := resultMap["contractAddress"].(string)
		if !ok {
			t.Fatal("expected contractAddress to be string")
		}
		if contractAddr != "0x1234567890123456789012345678901234567890" {
			t.Errorf("expected address 0x1234567890123456789012345678901234567890, got %s", contractAddr)
		}
	})

	t.Run("GetContractCreation_NotFound", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage:      &mockStorage{},
			contractCreation: nil,
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0xnonexistent"}`)
		result, err := server.HandleMethodDirect(ctx, "getContractCreation", params)
		if err != nil {
			t.Errorf("expected no error for not found, got %v", err)
		}
		if result != nil {
			t.Error("expected nil result for not found")
		}
	})

	t.Run("GetContractCreation_InvalidParams", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{}`)
		_, err := server.HandleMethodDirect(ctx, "getContractCreation", params)
		if err == nil {
			t.Error("expected error for missing address")
		}
		if err.Code != InvalidParams {
			t.Errorf("expected InvalidParams, got %v", err.Code)
		}
	})

	t.Run("GetContractsByCreator_Success", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			contractsByCreator: []common.Address{
				common.HexToAddress("0xcontract1"),
				common.HexToAddress("0xcontract2"),
				common.HexToAddress("0xcontract3"),
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"creator": "0xcreator123"}`)
		result, err := server.HandleMethodDirect(ctx, "getContractsByCreator", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		contracts, ok := resultMap["contracts"].([]interface{})
		if !ok {
			t.Fatal("expected contracts to be array")
		}
		if len(contracts) != 3 {
			t.Errorf("expected 3 contracts, got %d", len(contracts))
		}
	})

	t.Run("GetContractsByCreator_WithPagination", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			contractsByCreator: []common.Address{
				common.HexToAddress("0xcontract1"),
				common.HexToAddress("0xcontract2"),
				common.HexToAddress("0xcontract3"),
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"creator": "0xcreator123", "limit": 2, "offset": 1}`)
		result, err := server.HandleMethodDirect(ctx, "getContractsByCreator", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		contracts, ok := resultMap["contracts"].([]interface{})
		if !ok {
			t.Fatal("expected contracts to be array")
		}
		if len(contracts) != 2 {
			t.Errorf("expected 2 contracts with limit=2, got %d", len(contracts))
		}
	})

	t.Run("GetERC20Transfer_Success", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			erc20Transfer: &storage.ERC20Transfer{
				ContractAddress: common.HexToAddress("0xtoken"),
				From:            common.HexToAddress("0xfrom"),
				To:              common.HexToAddress("0xto"),
				Value:           big.NewInt(1000000),
				TransactionHash: common.HexToHash("0xtx123"),
				BlockNumber:     100,
				LogIndex:        0,
				Timestamp:       1234567890,
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"txHash": "0xtx123", "logIndex": 0}`)
		result, err := server.HandleMethodDirect(ctx, "getERC20Transfer", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		value, ok := resultMap["value"].(string)
		if !ok {
			t.Fatal("expected value to be string")
		}
		// Value should be hex encoded
		if value[:2] != "0x" {
			t.Errorf("expected hex value, got %s", value)
		}
	})

	t.Run("GetERC20TransfersByToken_Success", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			erc20TransfersByToken: []*storage.ERC20Transfer{
				{
					ContractAddress: common.HexToAddress("0xtoken"),
					From:            common.HexToAddress("0xfrom1"),
					To:              common.HexToAddress("0xto1"),
					Value:           big.NewInt(1000),
					TransactionHash: common.HexToHash("0xtx1"),
					BlockNumber:     100,
					LogIndex:        0,
					Timestamp:       1234567890,
				},
				{
					ContractAddress: common.HexToAddress("0xtoken"),
					From:            common.HexToAddress("0xfrom2"),
					To:              common.HexToAddress("0xto2"),
					Value:           big.NewInt(2000),
					TransactionHash: common.HexToHash("0xtx2"),
					BlockNumber:     101,
					LogIndex:        0,
					Timestamp:       1234567891,
				},
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"token": "0xtoken"}`)
		result, err := server.HandleMethodDirect(ctx, "getERC20TransfersByToken", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		transfers, ok := resultMap["transfers"].([]interface{})
		if !ok {
			t.Fatal("expected transfers to be array")
		}
		if len(transfers) != 2 {
			t.Errorf("expected 2 transfers, got %d", len(transfers))
		}
	})

	t.Run("GetERC20TransfersByAddress_From", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			erc20TransfersByAddress: []*storage.ERC20Transfer{
				{
					ContractAddress: common.HexToAddress("0xtoken"),
					From:            common.HexToAddress("0xfrom"),
					To:              common.HexToAddress("0xto"),
					Value:           big.NewInt(1000),
					TransactionHash: common.HexToHash("0xtx1"),
					BlockNumber:     100,
					LogIndex:        0,
					Timestamp:       1234567890,
				},
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0xfrom", "isFrom": true}`)
		result, err := server.HandleMethodDirect(ctx, "getERC20TransfersByAddress", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		transfers, ok := resultMap["transfers"].([]interface{})
		if !ok {
			t.Fatal("expected transfers to be array")
		}
		if len(transfers) != 1 {
			t.Errorf("expected 1 transfer, got %d", len(transfers))
		}
	})

	t.Run("GetERC721Transfer_Success", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			erc721Transfer: &storage.ERC721Transfer{
				ContractAddress: common.HexToAddress("0xnft"),
				From:            common.HexToAddress("0xfrom"),
				To:              common.HexToAddress("0xto"),
				TokenId:         big.NewInt(42),
				TransactionHash: common.HexToHash("0xtx123"),
				BlockNumber:     100,
				LogIndex:        0,
				Timestamp:       1234567890,
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"txHash": "0xtx123", "logIndex": 0}`)
		result, err := server.HandleMethodDirect(ctx, "getERC721Transfer", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		tokenId, ok := resultMap["tokenId"].(string)
		if !ok {
			t.Fatal("expected tokenId to be string")
		}
		// tokenId should be hex encoded (42 decimal = 0x2a hex)
		if tokenId != "0x2a" {
			t.Errorf("expected tokenId 0x2a, got %s", tokenId)
		}
	})

	t.Run("GetERC721Owner_Success", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			erc721Owner: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"token": "0xnft", "tokenId": "42"}`)
		result, err := server.HandleMethodDirect(ctx, "getERC721Owner", params)
		if err != nil {
			t.Fatalf("expected no error, got %v (code: %d, message: %s, data: %v)", err, err.Code, err.Message, err.Data)
		}

		if result == nil {
			t.Fatal("expected non-nil result, but got nil - this means ERC721Owner was not found or method returned nil")
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected result to be map, got %T: %v", result, result)
		}

		owner, ok := resultMap["owner"].(string)
		if !ok {
			t.Fatal("expected owner to be string")
		}
		if owner != "0x1234567890123456789012345678901234567890" {
			t.Errorf("expected owner 0x1234567890123456789012345678901234567890, got %s", owner)
		}
	})

	t.Run("GetInternalTransactions_Success", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			internalTxs: []*storage.InternalTransaction{
				{
					TransactionHash: common.HexToHash("0xtx123"),
					From:            common.HexToAddress("0xfrom"),
					To:              common.HexToAddress("0xto"),
					Value:           big.NewInt(1000),
					Gas:             21000,
					GasUsed:         21000,
					Input:           []byte{},
					Output:          []byte{},
					Type:            storage.InternalTxTypeCall,
					Index:           0,
					BlockNumber:     100,
					Depth:           0,
				},
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"txHash": "0xtx123"}`)
		result, err := server.HandleMethodDirect(ctx, "getInternalTransactions", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		internals, ok := resultMap["internals"].([]interface{})
		if !ok {
			t.Fatal("expected internals to be array")
		}
		if len(internals) != 1 {
			t.Errorf("expected 1 internal transaction, got %d", len(internals))
		}
	})

	t.Run("GetInternalTransactionsByAddress_Success", func(t *testing.T) {
		store := &mockAddressIndexStorage{
			mockStorage: &mockStorage{},
			internalTxsByAddress: []*storage.InternalTransaction{
				{
					TransactionHash: common.HexToHash("0xtx123"),
					From:            common.HexToAddress("0xaddress"),
					To:              common.HexToAddress("0xto"),
					Value:           big.NewInt(1000),
					Gas:             21000,
					GasUsed:         21000,
					Input:           []byte{},
					Output:          []byte{},
					Type:            storage.InternalTxTypeCall,
					Index:           0,
					BlockNumber:     100,
					Depth:           0,
				},
			},
		}

		server := NewServer(store, logger)
		params := json.RawMessage(`{"address": "0xaddress"}`)
		result, err := server.HandleMethodDirect(ctx, "getInternalTransactionsByAddress", params)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected result to be map")
		}

		internals, ok := resultMap["internals"].([]interface{})
		if !ok {
			t.Fatal("expected internals to be array")
		}
		if len(internals) != 1 {
			t.Errorf("expected 1 internal transaction, got %d", len(internals))
		}
	})

	// Test storage that doesn't support address indexing
	t.Run("AddressIndexing_NotSupported", func(t *testing.T) {
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
			{"GetContractCreation", "getContractCreation", json.RawMessage(`{"address": "0x1234"}`)},
			{"GetContractsByCreator", "getContractsByCreator", json.RawMessage(`{"creator": "0x1234"}`)},
			{"GetInternalTransactions", "getInternalTransactions", json.RawMessage(`{"txHash": "0x1234"}`)},
			{"GetInternalTransactionsByAddress", "getInternalTransactionsByAddress", json.RawMessage(`{"address": "0x1234"}`)},
			{"GetERC20Transfer", "getERC20Transfer", json.RawMessage(`{"txHash": "0x1234", "logIndex": 0}`)},
			{"GetERC20TransfersByToken", "getERC20TransfersByToken", json.RawMessage(`{"token": "0x1234"}`)},
			{"GetERC20TransfersByAddress", "getERC20TransfersByAddress", json.RawMessage(`{"address": "0x1234", "isFrom": true}`)},
			{"GetERC721Transfer", "getERC721Transfer", json.RawMessage(`{"txHash": "0x1234", "logIndex": 0}`)},
			{"GetERC721TransfersByToken", "getERC721TransfersByToken", json.RawMessage(`{"token": "0x1234"}`)},
			{"GetERC721TransfersByAddress", "getERC721TransfersByAddress", json.RawMessage(`{"address": "0x1234", "isFrom": true}`)},
			{"GetERC721Owner", "getERC721Owner", json.RawMessage(`{"token": "0x1234", "tokenId": "42"}`)},
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
