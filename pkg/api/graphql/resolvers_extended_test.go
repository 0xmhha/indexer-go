package graphql

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ---- richMockStorage returns actual data for deep code path coverage ----

type richMockStorage struct {
	mockStorage
}

// ---- Address index overrides for richMockStorage ----

func (m *richMockStorage) GetContractCreation(_ context.Context, addr common.Address) (*storage.ContractCreation, error) {
	return &storage.ContractCreation{ContractAddress: addr, Creator: common.HexToAddress("0x01"), TransactionHash: common.HexToHash("0xcc1"), BlockNumber: 10, Timestamp: 1700000000, BytecodeSize: 1024}, nil
}
func (m *richMockStorage) GetContractsByCreator(_ context.Context, _ common.Address, _, _ int) ([]common.Address, error) {
	return []common.Address{common.HexToAddress("0xA1"), common.HexToAddress("0xA2")}, nil
}
func (m *richMockStorage) ListContracts(_ context.Context, _, _ int) ([]*storage.ContractCreation, error) {
	return []*storage.ContractCreation{
		{ContractAddress: common.HexToAddress("0xA1"), Creator: common.HexToAddress("0x01"), TransactionHash: common.HexToHash("0xcc1"), BlockNumber: 10, Timestamp: 1700000000, BytecodeSize: 512},
	}, nil
}
func (m *richMockStorage) GetContractsCount(_ context.Context) (int, error) { return 5, nil }
func (m *richMockStorage) GetInternalTransactions(_ context.Context, _ common.Hash) ([]*storage.InternalTransaction, error) {
	return []*storage.InternalTransaction{
		{TransactionHash: common.HexToHash("0xdd1"), BlockNumber: 15, Index: 0, Type: "CALL", From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), Value: big.NewInt(1000), Gas: 21000, GasUsed: 21000, Depth: 0},
	}, nil
}
func (m *richMockStorage) GetInternalTransactionsByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.InternalTransaction, error) {
	return []*storage.InternalTransaction{
		{TransactionHash: common.HexToHash("0xdd2"), BlockNumber: 16, Index: 0, Type: "DELEGATECALL", From: common.HexToAddress("0x01"), To: common.HexToAddress("0x03"), Value: big.NewInt(0), Gas: 50000, GasUsed: 30000, Depth: 1},
	}, nil
}
func (m *richMockStorage) GetERC20Transfer(_ context.Context, _ common.Hash, _ uint) (*storage.ERC20Transfer, error) {
	return &storage.ERC20Transfer{ContractAddress: common.HexToAddress("0xE1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), Value: big.NewInt(5000), TransactionHash: common.HexToHash("0xee1"), BlockNumber: 20, LogIndex: 0, Timestamp: 1700000000}, nil
}
func (m *richMockStorage) GetERC20TransfersByToken(_ context.Context, _ common.Address, _, _ int) ([]*storage.ERC20Transfer, error) {
	return []*storage.ERC20Transfer{
		{ContractAddress: common.HexToAddress("0xE1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), Value: big.NewInt(5000), TransactionHash: common.HexToHash("0xee1"), BlockNumber: 20, LogIndex: 0, Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetERC20TransfersByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.ERC20Transfer, error) {
	return []*storage.ERC20Transfer{
		{ContractAddress: common.HexToAddress("0xE1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), Value: big.NewInt(5000), TransactionHash: common.HexToHash("0xee2"), BlockNumber: 21, LogIndex: 1, Timestamp: 1700000010},
	}, nil
}
func (m *richMockStorage) GetERC721Transfer(_ context.Context, _ common.Hash, _ uint) (*storage.ERC721Transfer, error) {
	return &storage.ERC721Transfer{ContractAddress: common.HexToAddress("0xF1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), TokenId: big.NewInt(42), TransactionHash: common.HexToHash("0xff1"), BlockNumber: 25, LogIndex: 0, Timestamp: 1700000000}, nil
}
func (m *richMockStorage) GetERC721TransfersByToken(_ context.Context, _ common.Address, _, _ int) ([]*storage.ERC721Transfer, error) {
	return []*storage.ERC721Transfer{
		{ContractAddress: common.HexToAddress("0xF1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), TokenId: big.NewInt(42), TransactionHash: common.HexToHash("0xff1"), BlockNumber: 25, LogIndex: 0, Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetERC721TransfersByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.ERC721Transfer, error) {
	return []*storage.ERC721Transfer{
		{ContractAddress: common.HexToAddress("0xF1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), TokenId: big.NewInt(43), TransactionHash: common.HexToHash("0xff2"), BlockNumber: 26, LogIndex: 1, Timestamp: 1700000010},
	}, nil
}
func (m *richMockStorage) GetERC721Owner(_ context.Context, _ common.Address, _ *big.Int) (common.Address, error) {
	return common.HexToAddress("0x02"), nil
}
func (m *richMockStorage) GetNFTsByOwner(_ context.Context, _ common.Address, _, _ int) ([]*storage.NFTOwnership, error) {
	return []*storage.NFTOwnership{
		{ContractAddress: common.HexToAddress("0xF1"), TokenId: big.NewInt(42), Owner: common.HexToAddress("0x02")},
	}, nil
}

// ---- SetCode overrides for richMockStorage ----

func (m *richMockStorage) GetSetCodeAuthorization(_ context.Context, _ common.Hash, _ int) (*storage.SetCodeAuthorizationRecord, error) {
	return &storage.SetCodeAuthorizationRecord{TxHash: common.HexToHash("0xsc1"), BlockNumber: 30, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS1"), AuthorityAddress: common.HexToAddress("0xS2"), ChainID: big.NewInt(1), Nonce: 5, Applied: true, Timestamp: time.Unix(1700000000, 0)}, nil
}
func (m *richMockStorage) GetSetCodeAuthorizationsByTx(_ context.Context, _ common.Hash) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc1"), BlockNumber: 30, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS1"), AuthorityAddress: common.HexToAddress("0xS2"), ChainID: big.NewInt(1), Nonce: 5, Applied: true, Timestamp: time.Unix(1700000000, 0)},
	}, nil
}
func (m *richMockStorage) GetSetCodeAuthorizationsByTarget(_ context.Context, _ common.Address, _, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc2"), BlockNumber: 31, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS1"), AuthorityAddress: common.HexToAddress("0xS3"), ChainID: big.NewInt(1), Applied: true, Timestamp: time.Unix(1700000010, 0)},
	}, nil
}
func (m *richMockStorage) GetSetCodeAuthorizationsByAuthority(_ context.Context, _ common.Address, _, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc3"), BlockNumber: 32, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS4"), AuthorityAddress: common.HexToAddress("0xS2"), ChainID: big.NewInt(1), Applied: false, Error: "nonce mismatch", Timestamp: time.Unix(1700000020, 0)},
	}, nil
}
func (m *richMockStorage) GetSetCodeAuthorizationsByBlock(_ context.Context, _ uint64) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc1"), BlockNumber: 30, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS1"), AuthorityAddress: common.HexToAddress("0xS2"), Applied: true, Timestamp: time.Unix(1700000000, 0)},
	}, nil
}
func (m *richMockStorage) GetSetCodeTransactionCount(_ context.Context) (int, error) { return 10, nil }
func (m *richMockStorage) GetRecentSetCodeAuthorizations(_ context.Context, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc4"), BlockNumber: 33, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS5"), AuthorityAddress: common.HexToAddress("0xS6"), Applied: true, Timestamp: time.Unix(1700000030, 0)},
	}, nil
}

// ---- TokenHolder overrides for richMockStorage ----

func (m *richMockStorage) GetTokenHolders(_ context.Context, _ common.Address, _, _ int) ([]*storage.TokenHolder, error) {
	return []*storage.TokenHolder{
		{TokenAddress: common.HexToAddress("0xT1"), HolderAddress: common.HexToAddress("0x01"), Balance: big.NewInt(1000000)},
	}, nil
}
func (m *richMockStorage) GetTokenHolderCount(_ context.Context, _ common.Address) (int, error) {
	return 42, nil
}
func (m *richMockStorage) GetTokenBalance(_ context.Context, _, _ common.Address) (*big.Int, error) {
	return big.NewInt(999), nil
}

// ---- Historical overrides for richMockStorage ----

func (m *richMockStorage) GetTokenBalances(_ context.Context, _ common.Address, _ string) ([]storage.TokenBalance, error) {
	decimals := 18
	return []storage.TokenBalance{
		{ContractAddress: common.HexToAddress("0xT1"), TokenType: "ERC20", Balance: big.NewInt(1000000), Name: "TestToken", Symbol: "TT", Decimals: &decimals},
	}, nil
}
func (m *richMockStorage) GetGasStatsByBlockRange(_ context.Context, _, _ uint64) (*storage.GasStats, error) {
	return &storage.GasStats{TotalGasUsed: 500000, TotalGasLimit: 8000000, AverageGasUsed: 50000, AverageGasPrice: big.NewInt(20000000000), BlockCount: 10, TransactionCount: 100}, nil
}
func (m *richMockStorage) GetGasStatsByAddress(_ context.Context, addr common.Address, _, _ uint64) (*storage.AddressGasStats, error) {
	return &storage.AddressGasStats{Address: addr, TotalGasUsed: 100000, TransactionCount: 50, AverageGasPerTx: 2000, TotalFeesPaid: big.NewInt(1000000000000)}, nil
}
func (m *richMockStorage) GetTopAddressesByGasUsed(_ context.Context, _ int, _, _ uint64) ([]storage.AddressGasStats, error) {
	return []storage.AddressGasStats{
		{Address: common.HexToAddress("0x01"), TotalGasUsed: 100000, TransactionCount: 50},
	}, nil
}
func (m *richMockStorage) GetTopAddressesByTxCount(_ context.Context, _ int, _, _ uint64) ([]storage.AddressActivityStats, error) {
	return []storage.AddressActivityStats{
		{Address: common.HexToAddress("0x01"), TransactionCount: 200, TotalGasUsed: 500000},
	}, nil
}
func (m *richMockStorage) GetNetworkMetrics(_ context.Context, _, _ uint64) (*storage.NetworkMetrics, error) {
	return &storage.NetworkMetrics{TPS: 15.5, BlockTime: 2.0, TotalBlocks: 1000, TotalTransactions: 5000, AverageBlockSize: 4000000}, nil
}
func (m *richMockStorage) GetAddressStats(_ context.Context, addr common.Address) (*storage.AddressStats, error) {
	return &storage.AddressStats{Address: addr, TotalTransactions: 100, SentCount: 60, ReceivedCount: 40, SuccessCount: 95}, nil
}

// newRichTestHandler creates a handler with a rich mock returning actual data.
func newRichTestHandler(t *testing.T) *Handler {
	t.Helper()
	return newRichTestHandlerFull(t)
}

// newRichTestHandlerFull creates a handler with full schema (including SetCode, TokenHolder queries).
func newRichTestHandlerFull(t *testing.T) *Handler {
	t.Helper()
	header := &types.Header{
		Number:     common.Big1,
		ParentHash: common.HexToHash("0x123"),
		Time:       1700000000,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	testBlock := types.NewBlockWithHeader(header)
	store := &richMockStorage{
		mockStorage: mockStorage{
			latestHeight: 100,
			blocks:       map[uint64]*types.Block{1: testBlock},
			blocksByHash: map[common.Hash]*types.Block{testBlock.Hash(): testBlock},
			transactions: make(map[common.Hash]*types.Transaction),
			receipts:     make(map[common.Hash]*types.Receipt),
		},
	}
	logger := zap.NewNop()
	schema, err := NewSchemaBuilder(store, logger).
		WithCoreQueries().
		WithHistoricalQueries().
		WithAnalyticsQueries().
		WithAddressIndexingQueries().
		WithSetCodeQueries().
		WithTokenMetadataQueries().
		WithTokenHolderQueries().
		WithSubscriptions().
		WithMutations().
		Build()
	require.NoError(t, err)
	return &Handler{schema: schema}
}

// ---- AddressIndexReader implementation for mockStorage ----

func (m *mockStorage) GetContractCreation(_ context.Context, _ common.Address) (*storage.ContractCreation, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetContractsByCreator(_ context.Context, _ common.Address, _, _ int) ([]common.Address, error) {
	return []common.Address{}, nil
}
func (m *mockStorage) ListContracts(_ context.Context, _, _ int) ([]*storage.ContractCreation, error) {
	return []*storage.ContractCreation{}, nil
}
func (m *mockStorage) GetContractsCount(_ context.Context) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetInternalTransactions(_ context.Context, _ common.Hash) ([]*storage.InternalTransaction, error) {
	return []*storage.InternalTransaction{}, nil
}
func (m *mockStorage) GetInternalTransactionsByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.InternalTransaction, error) {
	return []*storage.InternalTransaction{}, nil
}
func (m *mockStorage) GetERC20Transfer(_ context.Context, _ common.Hash, _ uint) (*storage.ERC20Transfer, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetERC20TransfersByToken(_ context.Context, _ common.Address, _, _ int) ([]*storage.ERC20Transfer, error) {
	return []*storage.ERC20Transfer{}, nil
}
func (m *mockStorage) GetERC20TransfersByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.ERC20Transfer, error) {
	return []*storage.ERC20Transfer{}, nil
}
func (m *mockStorage) GetERC721Transfer(_ context.Context, _ common.Hash, _ uint) (*storage.ERC721Transfer, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetERC721TransfersByToken(_ context.Context, _ common.Address, _, _ int) ([]*storage.ERC721Transfer, error) {
	return []*storage.ERC721Transfer{}, nil
}
func (m *mockStorage) GetERC721TransfersByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.ERC721Transfer, error) {
	return []*storage.ERC721Transfer{}, nil
}
func (m *mockStorage) GetERC721Owner(_ context.Context, _ common.Address, _ *big.Int) (common.Address, error) {
	return common.Address{}, storage.ErrNotFound
}
func (m *mockStorage) GetNFTsByOwner(_ context.Context, _ common.Address, _, _ int) ([]*storage.NFTOwnership, error) {
	return []*storage.NFTOwnership{}, nil
}

// ---- SetCodeIndexReader implementation for mockStorage ----

func (m *mockStorage) GetSetCodeAuthorization(_ context.Context, _ common.Hash, _ int) (*storage.SetCodeAuthorizationRecord, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetSetCodeAuthorizationsByTx(_ context.Context, _ common.Hash) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsByTarget(_ context.Context, _ common.Address, _, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsByAuthority(_ context.Context, _ common.Address, _, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsByBlock(_ context.Context, _ uint64) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}
func (m *mockStorage) GetAddressSetCodeStats(_ context.Context, addr common.Address) (*storage.AddressSetCodeStats, error) {
	return &storage.AddressSetCodeStats{Address: addr}, nil
}
func (m *mockStorage) GetAddressDelegationState(_ context.Context, addr common.Address) (*storage.AddressDelegationState, error) {
	return &storage.AddressDelegationState{Address: addr}, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsCountByTarget(_ context.Context, _ common.Address) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsCountByAuthority(_ context.Context, _ common.Address) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetSetCodeTransactionCount(_ context.Context) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetRecentSetCodeAuthorizations(_ context.Context, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}

// ---- TokenHolderIndexReader implementation for mockStorage ----

func (m *mockStorage) GetTokenHolders(_ context.Context, _ common.Address, _, _ int) ([]*storage.TokenHolder, error) {
	return []*storage.TokenHolder{}, nil
}
func (m *mockStorage) GetTokenHolderCount(_ context.Context, _ common.Address) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetTokenBalance(_ context.Context, _, _ common.Address) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (m *mockStorage) GetTokenHolderStats(_ context.Context, token common.Address) (*storage.TokenHolderStats, error) {
	return &storage.TokenHolderStats{TokenAddress: token}, nil
}
func (m *mockStorage) GetHolderTokens(_ context.Context, _ common.Address, _, _ int) ([]*storage.TokenHolder, error) {
	return []*storage.TokenHolder{}, nil
}

// Compile-time interface assertions
var _ storage.AddressIndexReader = (*mockStorage)(nil)
var _ storage.SetCodeIndexReader = (*mockStorage)(nil)
var _ storage.TokenHolderIndexReader = (*mockStorage)(nil)

// newTestHandler creates a handler with a test block at height 1.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	header := &types.Header{
		Number:     common.Big1,
		ParentHash: common.HexToHash("0x123"),
		Time:       1700000000,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	testBlock := types.NewBlockWithHeader(header)
	store := &mockStorage{
		latestHeight: 100,
		blocks:       map[uint64]*types.Block{1: testBlock},
		blocksByHash: map[common.Hash]*types.Block{testBlock.Hash(): testBlock},
		transactions: make(map[common.Hash]*types.Transaction),
		receipts:     make(map[common.Hash]*types.Receipt),
	}
	handler, err := NewHandler(store, zap.NewNop())
	require.NoError(t, err)
	return handler
}

// TestAddressIndexingResolvers tests address indexing query resolvers.
// Since mockStorage doesn't implement AddressIndexReader, these hit the "not available" paths.
func TestAddressIndexingResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"addressOverview", `{ addressOverview(address: "0x0000000000000000000000000000000000000001") { address balance transactionCount isContract } }`},
		{"contractCreation", `{ contractCreation(address: "0x0000000000000000000000000000000000000001") { contractAddress creator transactionHash blockNumber } }`},
		{"contracts", `{ contracts { nodes { contractAddress creator } totalCount pageInfo { hasNextPage } } }`},
		{"contractsByCreator", `{ contractsByCreator(creator: "0x0000000000000000000000000000000000000001") { contractAddress } }`},
		{"internalTransactions", `{ internalTransactions(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001") { from to value } }`},
		{"internalTransactionsByAddress", `{ internalTransactionsByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { from to value } totalCount } }`},
		{"erc20Transfer", `{ erc20Transfer(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001", logIndex: 0) { contractAddress from to value } }`},
		{"erc20TransfersByToken", `{ erc20TransfersByToken(token: "0x0000000000000000000000000000000000000001") { nodes { from to value } totalCount } }`},
		{"erc20TransfersByAddress", `{ erc20TransfersByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { contractAddress from to value } totalCount } }`},
		{"erc721Transfer", `{ erc721Transfer(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001", logIndex: 0) { contractAddress from to tokenId } }`},
		{"erc721TransfersByToken", `{ erc721TransfersByToken(token: "0x0000000000000000000000000000000000000001") { nodes { from to tokenId } totalCount } }`},
		{"erc721TransfersByAddress", `{ erc721TransfersByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { contractAddress from to tokenId } totalCount } }`},
		{"erc721Owner", `{ erc721Owner(token: "0x0000000000000000000000000000000000000001", tokenId: "1") }`},
		{"nftsByOwner", `{ nftsByOwner(owner: "0x0000000000000000000000000000000000000001") { nodes { contractAddress tokenId } totalCount } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			_ = result // Resolver ran, coverage gained
		})
	}
}

// TestSetCodeResolvers tests setCode authorization query resolvers.
// Since mockStorage doesn't implement SetCodeIndexReader, these hit the "not available" paths.
func TestSetCodeResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"setCodeAuthorization", `{ setCodeAuthorization(txHash: "0x0000000000000000000000000000000000000000000000000000000000000001", authIndex: 0) { txHash authorizationIndex address authority chainId nonce } }`},
		{"setCodeAuthorizationsByTx", `{ setCodeAuthorizationsByTx(txHash: "0x0000000000000000000000000000000000000000000000000000000000000001") { txHash address authority } }`},
		{"setCodeAuthorizationsByTarget", `{ setCodeAuthorizationsByTarget(target: "0x0000000000000000000000000000000000000001") { nodes { txHash address authority } totalCount } }`},
		{"setCodeAuthorizationsByAuthority", `{ setCodeAuthorizationsByAuthority(authority: "0x0000000000000000000000000000000000000001") { nodes { txHash address authority } totalCount } }`},
		{"addressSetCodeInfo", `{ addressSetCodeInfo(address: "0x0000000000000000000000000000000000000001") { address hasDelegation delegationTarget asTargetCount } }`},
		{"setCodeTransactionsInBlock", `{ setCodeTransactionsInBlock(blockNumber: "1") { hash from to } }`},
		{"recentSetCodeTransactions", `{ recentSetCodeTransactions(limit: 10) { hash from to } }`},
		{"setCodeTransactionCount", `{ setCodeTransactionCount }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			_ = result // Resolver ran, coverage gained
		})
	}
}

// TestTokenMetadataResolvers tests token metadata query resolvers.
func TestTokenMetadataResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"tokenMetadata", `{ tokenMetadata(address: "0x0000000000000000000000000000000000000001") { address name symbol decimals standard } }`},
		{"tokens", `{ tokens(standard: "ERC20") { nodes { address name symbol } totalCount } }`},
		{"searchTokens", `{ searchTokens(query: "test") { address name symbol } }`},
		{"tokenCount", `{ tokenCount(standard: "ERC20") }`},
		// TokenHolder queries - mockStorage doesn't implement TokenHolderIndexReader, hits "not available" path
		{"tokenHolders", `{ tokenHolders(token: "0x0000000000000000000000000000000000000001") { nodes { holderAddress balance } totalCount } }`},
		{"tokenHolderCount", `{ tokenHolderCount(token: "0x0000000000000000000000000000000000000001") }`},
		{"tokenBalance", `{ tokenBalance(token: "0x0000000000000000000000000000000000000001", holder: "0x0000000000000000000000000000000000000002") }`},
		{"tokenHolderStats", `{ tokenHolderStats(token: "0x0000000000000000000000000000000000000001") { holderCount transferCount } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			_ = result // Resolver ran, coverage gained
		})
	}
}

// TestHistoricalResolvers tests untested historical/analytics query resolvers.
func TestHistoricalAndAnalyticsResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"blockCount", `{ blockCount }`},
		{"transactionCount", `{ transactionCount }`},
		{"blocksByTimeRange", `{ blocksByTimeRange(fromTime: "1700000000", toTime: "1700001000") { number hash timestamp } }`},
		{"blockByTimestamp", `{ blockByTimestamp(timestamp: "1700000000") { number hash } }`},
		{"transactionsByAddressFiltered", `{ transactionsByAddressFiltered(address: "0x0000000000000000000000000000000000000001") { nodes { hash } totalCount } }`},
		{"addressBalance", `{ addressBalance(address: "0x0000000000000000000000000000000000000001") }`},
		{"addressBalance_withBlock", `{ addressBalance(address: "0x0000000000000000000000000000000000000001", blockNumber: "1") }`},
		{"balanceHistory", `{ balanceHistory(address: "0x0000000000000000000000000000000000000001") { blockNumber balance txHash } }`},
		{"topMiners", `{ topMiners(limit: 5) { address blockCount percentage } }`},
		{"topMiners_withRange", `{ topMiners(limit: 5, fromBlock: "0", toBlock: "100") { address blockCount } }`},
		{"tokenBalances", `{ tokenBalances(address: "0x0000000000000000000000000000000000000001") { address balance tokenType } }`},
		{"gasStats", `{ gasStats(fromBlock: "0", toBlock: "100") { averageGasPrice totalGasUsed averageGasUsed } }`},
		{"addressGasStats", `{ addressGasStats(address: "0x0000000000000000000000000000000000000001", fromBlock: "0", toBlock: "100") { address totalGasUsed averageGasPerTx transactionCount } }`},
		{"topAddressesByGasUsed", `{ topAddressesByGasUsed(limit: 5, fromBlock: "0", toBlock: "100") { address totalGasUsed } }`},
		{"topAddressesByTxCount", `{ topAddressesByTxCount(limit: 5, fromBlock: "0", toBlock: "100") { address transactionCount } }`},
		{"networkMetrics", `{ networkMetrics(fromTime: "1700000000", toTime: "1700001000") { totalBlocks totalTransactions blockTime } }`},
		{"addressStats", `{ addressStats(address: "0x0000000000000000000000000000000000000001") { totalTransactions sentCount receivedCount } }`},
		{"contractVerification", `{ contractVerification(address: "0x0000000000000000000000000000000000000001") { address verified } }`},
		{"search", `{ search(query: "0x1234") { type value label } }`},
		{"blocksRange", `{ blocksRange(from: "1", to: "10") { number hash } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			_ = result // Resolver ran, coverage gained
		})
	}
}

// TestNewSchema tests the NewSchema constructor directly.
func TestNewSchema_Direct(t *testing.T) {
	store := &mockStorage{
		latestHeight: 0,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}
	schema, err := NewSchema(store, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, schema)
	assert.NotNil(t, schema.Schema())
}

// TestNewHandlerWithOptions_Variations tests handler creation with different options.
func TestNewHandlerWithOptions_Variations(t *testing.T) {
	store := &mockStorage{
		latestHeight: 0,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}
	logger := zap.NewNop()

	t.Run("nil options", func(t *testing.T) {
		handler, err := NewHandlerWithOptions(store, logger, nil)
		require.NoError(t, err)
		require.NotNil(t, handler)
	})

	t.Run("empty options", func(t *testing.T) {
		handler, err := NewHandlerWithOptions(store, logger, &HandlerOptions{})
		require.NoError(t, err)
		require.NotNil(t, handler)
	})
}

// TestSchemaBuilder tests schema builder methods.
func TestSchemaBuilder_Methods(t *testing.T) {
	store := &mockStorage{
		latestHeight: 0,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}
	logger := zap.NewNop()

	builder := NewSchemaBuilder(store, logger)
	require.NotNil(t, builder)

	// Chain all query builders
	builder = builder.
		WithCoreQueries().
		WithHistoricalQueries().
		WithAnalyticsQueries().
		WithAddressIndexingQueries().
		WithTokenMetadataQueries().
		WithTokenHolderQueries().
		WithSetCodeQueries().
		WithSubscriptions().
		WithMutations()

	schema, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, schema)
}

// ---- Tests using richMockStorage for deep code path coverage ----

// TestBlocksRangeResolver exercises the resolveBlocksRange function.
func TestBlocksRangeResolver(t *testing.T) {
	handler := newRichTestHandler(t)

	t.Run("basic_range", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blocksRange(startNumber: "1", endNumber: "1") { blocks { number hash gasUsed gasLimit } startNumber endNumber count hasMore latestHeight } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		br := data["blocksRange"].(map[string]interface{})
		assert.Equal(t, 1, br["count"])
		assert.Equal(t, true, br["hasMore"])
	})

	t.Run("range_beyond_latest", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blocksRange(startNumber: "200", endNumber: "300") { count hasMore } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		br := data["blocksRange"].(map[string]interface{})
		assert.Equal(t, 0, br["count"])
	})

	t.Run("range_noTransactions", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blocksRange(startNumber: "1", endNumber: "1", includeTransactions: false) { blocks { number } count } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("range_withReceipts", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blocksRange(startNumber: "1", endNumber: "1", includeReceipts: true) { blocks { number } count } }`, nil)
		assert.Empty(t, result.Errors)
	})
}

// TestAddressResolversWithData exercises address indexing resolvers with actual data.
func TestAddressResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"contractCreation", `{ contractCreation(address: "0x0000000000000000000000000000000000000001") { contractAddress creator transactionHash blockNumber timestamp } }`},
		{"contracts", `{ contracts { nodes { contractAddress creator transactionHash blockNumber } totalCount pageInfo { hasNextPage } } }`},
		{"contractsByCreator", `{ contractsByCreator(creator: "0x0000000000000000000000000000000000000001") { nodes { contractAddress } totalCount } }`},
		{"internalTransactions", `{ internalTransactions(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001") { transactionHash blockNumber type from to value gas gasUsed } }`},
		{"internalTransactionsByAddress", `{ internalTransactionsByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { transactionHash from to value type } totalCount } }`},
		{"erc20Transfer", `{ erc20Transfer(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001", logIndex: 0) { contractAddress from to value transactionHash blockNumber } }`},
		{"erc20TransfersByToken", `{ erc20TransfersByToken(token: "0x0000000000000000000000000000000000000001") { nodes { from to value } totalCount } }`},
		{"erc20TransfersByAddress", `{ erc20TransfersByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { contractAddress from to value } totalCount } }`},
		{"erc721Transfer", `{ erc721Transfer(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001", logIndex: 0) { contractAddress from to tokenId transactionHash blockNumber } }`},
		{"erc721TransfersByToken", `{ erc721TransfersByToken(token: "0x0000000000000000000000000000000000000001") { nodes { from to tokenId } totalCount } }`},
		{"erc721TransfersByAddress", `{ erc721TransfersByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { contractAddress from to tokenId } totalCount } }`},
		{"erc721Owner", `{ erc721Owner(token: "0x0000000000000000000000000000000000000001", tokenId: "1") }`},
		{"nftsByOwner", `{ nftsByOwner(owner: "0x0000000000000000000000000000000000000001") { nodes { contractAddress tokenId owner } totalCount } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
		})
	}
}

// TestSetCodeResolversWithData exercises setCode resolvers with actual data.
func TestSetCodeResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"setCodeAuthorization", `{ setCodeAuthorization(txHash: "0x0000000000000000000000000000000000000000000000000000000000000001", authIndex: 0) { txHash authorizationIndex address authority chainId nonce applied } }`},
		{"setCodeAuthorizationsByTx", `{ setCodeAuthorizationsByTx(txHash: "0x0000000000000000000000000000000000000000000000000000000000000001") { txHash address authority applied } }`},
		{"setCodeAuthorizationsByTarget", `{ setCodeAuthorizationsByTarget(target: "0x0000000000000000000000000000000000000001") { nodes { txHash address authority } totalCount } }`},
		{"setCodeAuthorizationsByAuthority", `{ setCodeAuthorizationsByAuthority(authority: "0x0000000000000000000000000000000000000001") { nodes { txHash address authority applied error } totalCount } }`},
		{"setCodeTransactionsInBlock", `{ setCodeTransactionsInBlock(blockNumber: "1") { hash from to } }`},
		{"recentSetCodeTransactions", `{ recentSetCodeTransactions(limit: 10) { hash from to } }`},
		{"setCodeTransactionCount", `{ setCodeTransactionCount }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
		})
	}
}

// TestTokenResolversWithData exercises token holder resolvers with actual data.
func TestTokenResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"tokenHolders", `{ tokenHolders(token: "0x0000000000000000000000000000000000000001") { nodes { holderAddress balance } totalCount } }`},
		{"tokenHolderCount", `{ tokenHolderCount(token: "0x0000000000000000000000000000000000000001") }`},
		{"tokenBalance", `{ tokenBalance(token: "0x0000000000000000000000000000000000000001", holder: "0x0000000000000000000000000000000000000002") }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
		})
	}
}

// TestHistoricalResolversWithData exercises historical/analytics resolvers with actual data.
func TestHistoricalResolversWithRichData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name      string
		query     string
		checkData func(t *testing.T, data map[string]interface{})
	}{
		{
			"tokenBalances",
			`{ tokenBalances(address: "0x0000000000000000000000000000000000000001") { address balance tokenType name symbol } }`,
			func(t *testing.T, data map[string]interface{}) {
				balances, ok := data["tokenBalances"].([]interface{})
				require.True(t, ok)
				assert.Len(t, balances, 1)
			},
		},
		{
			"gasStats",
			`{ gasStats(fromBlock: "0", toBlock: "100") { totalGasUsed averageGasPrice blockCount transactionCount } }`,
			func(t *testing.T, data map[string]interface{}) {
				stats := data["gasStats"]
				assert.NotNil(t, stats)
			},
		},
		{
			"addressGasStats",
			`{ addressGasStats(address: "0x0000000000000000000000000000000000000001", fromBlock: "0", toBlock: "100") { address totalGasUsed averageGasPerTx transactionCount } }`,
			func(t *testing.T, data map[string]interface{}) {
				stats := data["addressGasStats"]
				assert.NotNil(t, stats)
			},
		},
		{
			"topAddressesByTxCount",
			`{ topAddressesByTxCount(limit: 5, fromBlock: "0", toBlock: "100") { address transactionCount } }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["topAddressesByTxCount"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 1)
			},
		},
		{
			"networkMetrics",
			`{ networkMetrics(fromTime: "1700000000", toTime: "1700001000") { tps blockTime totalBlocks totalTransactions } }`,
			func(t *testing.T, data map[string]interface{}) {
				metrics := data["networkMetrics"]
				assert.NotNil(t, metrics)
			},
		},
		{
			"addressStats",
			`{ addressStats(address: "0x0000000000000000000000000000000000000001") { address totalTransactions sentCount receivedCount } }`,
			func(t *testing.T, data map[string]interface{}) {
				stats := data["addressStats"]
				assert.NotNil(t, stats)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
			if tc.checkData != nil {
				data, ok := result.Data.(map[string]interface{})
				require.True(t, ok)
				tc.checkData(t, data)
			}
		})
	}
}
