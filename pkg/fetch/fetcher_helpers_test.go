package fetch

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
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

func TestFetcher_SetTokenIndexer(t *testing.T) {
	f := newTestFetcherForHelpers(t)
	mock := &mockTokenIndexer{}
	f.SetTokenIndexer(mock)

	if f.tokenIndexer == nil {
		t.Error("expected tokenIndexer to be set")
	}
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

// recordingTokenIndexer records the contracts it is asked to index.
type recordingTokenIndexer struct {
	calls []string
}

func (r *recordingTokenIndexer) IndexToken(_ context.Context, address common.Address, blockHeight uint64) error {
	r.calls = append(r.calls, fmt.Sprintf("%s@%d", address.Hex(), blockHeight))
	return nil
}

// TestInitializeGenesisTokenMetadata indexes the chain's known token
// contracts at height 0.
func TestInitializeGenesisTokenMetadata(t *testing.T) {
	addr := common.HexToAddress("0x000000000000000000000000000000000000F00d")
	storagepkg.RegisterKnownToken(addr, storagepkg.KnownToken{Name: "Test", Symbol: "TST", Decimals: 18})

	f := newTestFetcherForHelpers(t)
	require.NoError(t, f.initializeGenesisTokenMetadata(context.Background()), "no token indexer")

	idx := &recordingTokenIndexer{}
	f.SetTokenIndexer(idx)
	require.NoError(t, f.initializeGenesisTokenMetadata(context.Background()))
	require.Contains(t, idx.calls, addr.Hex()+"@0")
}
