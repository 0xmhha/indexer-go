package fetch

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/types/chain"
)

// ============================================================================
// Fetcher getter/setter Tests
// ============================================================================

func newTestFetcherForHelpers(t *testing.T) *Fetcher {
	t.Helper()
	client := newMockClient()
	storage := newMockStorage()
	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Second,
	}
	return NewFetcher(client, storage, config, zap.NewNop(), nil)
}

func TestFetcher_SetGetChainID(t *testing.T) {
	f := newTestFetcherForHelpers(t)

	if f.GetChainID() != "" {
		t.Error("expected empty chain ID initially")
	}

	f.SetChainID("stable-mainnet")
	if f.GetChainID() != "stable-mainnet" {
		t.Errorf("expected stable-mainnet, got %s", f.GetChainID())
	}
}

func TestFetcher_SetGetChainAdapter(t *testing.T) {
	f := newTestFetcherForHelpers(t)

	if f.GetChainAdapter() != nil {
		t.Error("expected nil chain adapter initially")
	}

	adapter := &mockChainAdapter{}
	f.SetChainAdapter(adapter)
	if f.GetChainAdapter() == nil {
		t.Error("expected chain adapter to be set")
	}
}

func TestNewFetcherWithAdapter(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Second,
	}
	adapter := &mockChainAdapter{}
	f := NewFetcherWithAdapter(client, storage, config, zap.NewNop(), nil, adapter)
	if f == nil {
		t.Fatal("expected non-nil fetcher")
	}
	if f.GetChainAdapter() == nil {
		t.Error("expected chain adapter to be set")
	}
}

func TestNewFetcherWithAdapter_NilAdapter(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Second,
	}
	f := NewFetcherWithAdapter(client, storage, config, zap.NewNop(), nil, nil)
	if f == nil {
		t.Fatal("expected non-nil fetcher")
	}
	if f.GetChainAdapter() != nil {
		t.Error("expected nil chain adapter")
	}
}

func TestFetcher_SetTokenIndexer(t *testing.T) {
	f := newTestFetcherForHelpers(t)
	mock := &mockTokenIndexer{}
	f.SetTokenIndexer(mock)

	if f.tokenIndexer == nil {
		t.Error("expected tokenIndexer to be set")
	}
}

func TestFetcher_SetSetCodeProcessor(t *testing.T) {
	f := newTestFetcherForHelpers(t)
	f.SetSetCodeProcessor(nil)
	// Just verify no panic
}

func TestFetcher_AddRemoveBlockProcessor(t *testing.T) {
	f := newTestFetcherForHelpers(t)
	p := &mockBlockProcessor{}

	f.AddBlockProcessor(p)
	if len(f.blockProcessors) != 1 {
		t.Errorf("expected 1 processor, got %d", len(f.blockProcessors))
	}

	f.RemoveBlockProcessor(p)
	if len(f.blockProcessors) != 0 {
		t.Errorf("expected 0 processors after remove, got %d", len(f.blockProcessors))
	}
}

func TestFetcher_RemoveBlockProcessor_NotFound(t *testing.T) {
	f := newTestFetcherForHelpers(t)
	p1 := &mockBlockProcessor{}
	p2 := &mockBlockProcessor{}

	f.AddBlockProcessor(p1)
	f.RemoveBlockProcessor(p2) // p2 was never added

	if len(f.blockProcessors) != 1 {
		t.Errorf("expected 1 processor after removing non-existent, got %d", len(f.blockProcessors))
	}
}

// ============================================================================
// buildReceiptMap Tests
// ============================================================================

func TestBuildReceiptMap_Empty(t *testing.T) {
	result := buildReceiptMap(nil)
	if len(result) != 0 {
		t.Error("expected empty map for nil receipts")
	}

	result = buildReceiptMap(types.Receipts{})
	if len(result) != 0 {
		t.Error("expected empty map for empty receipts")
	}
}

func TestBuildReceiptMap_WithReceipts(t *testing.T) {
	txHash1 := common.HexToHash("0xaaa")
	txHash2 := common.HexToHash("0xbbb")

	receipts := types.Receipts{
		{TxHash: txHash1, Status: 1},
		{TxHash: txHash2, Status: 0},
	}

	result := buildReceiptMap(receipts)
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
	if result[txHash1].Status != 1 {
		t.Error("expected receipt for txHash1 with status 1")
	}
	if result[txHash2].Status != 0 {
		t.Error("expected receipt for txHash2 with status 0")
	}
}

func TestBuildReceiptMap_SkipsNil(t *testing.T) {
	receipts := types.Receipts{
		nil,
		{TxHash: common.HexToHash("0xaaa"), Status: 1},
		nil,
	}

	result := buildReceiptMap(receipts)
	if len(result) != 1 {
		t.Errorf("expected 1 entry (nil skipped), got %d", len(result))
	}
}

// ============================================================================
// getTransactionSender Tests
// ============================================================================

func TestGetTransactionSender_Valid(t *testing.T) {
	key, _ := crypto.GenerateKey()
	signer := types.LatestSignerForChainID(big.NewInt(1))

	tx := types.MustSignNewTx(key, signer, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{},
		Value:    big.NewInt(0),
	})

	sender := getTransactionSender(tx)
	expected := crypto.PubkeyToAddress(key.PublicKey)

	if sender != expected {
		t.Errorf("expected sender %s, got %s", expected.Hex(), sender.Hex())
	}
}

func TestGetTransactionSender_UnsignedReturnsZero(t *testing.T) {
	// Create an unsigned transaction - sender cannot be recovered
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{},
		Value:    big.NewInt(0),
	})

	sender := getTransactionSender(tx)
	if sender != (common.Address{}) {
		t.Errorf("expected zero address for unsigned tx, got %s", sender.Hex())
	}
}

// ============================================================================
// Mock implementations
// ============================================================================

type mockTokenIndexer struct{}

func (m *mockTokenIndexer) IndexToken(ctx context.Context, address common.Address, blockHeight uint64) error {
	return nil
}

type mockBlockProcessor struct {
	processedBlocks int
}

func (m *mockBlockProcessor) ProcessBlock(ctx context.Context, chainID string, block *types.Block, receipts []*types.Receipt) error {
	m.processedBlocks++
	return nil
}

// mockChainAdapter implements chain.Adapter for testing
type mockChainAdapter struct{}

func (m *mockChainAdapter) Info() *chain.ChainInfo {
	return &chain.ChainInfo{
		ChainType:     chain.ChainTypeEVM,
		ConsensusType: chain.ConsensusTypeWBFT,
		Name:          "test",
	}
}
func (m *mockChainAdapter) BlockFetcher() chain.BlockFetcher               { return nil }
func (m *mockChainAdapter) TransactionParser() chain.TransactionParser     { return nil }
func (m *mockChainAdapter) ConsensusParser() chain.ConsensusParser         { return nil }
func (m *mockChainAdapter) SystemContracts() chain.SystemContractsHandler  { return nil }
func (m *mockChainAdapter) Close() error                                   { return nil }
