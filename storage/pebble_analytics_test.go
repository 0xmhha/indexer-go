package storage

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
)

// Helper to create a block with a specific miner
func createTestBlockWithMiner(height uint64, miner common.Address, gasUsed uint64, timestamp uint64) *types.Block {
	header := &types.Header{
		ParentHash:  common.Hash{},
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    miner,
		Root:        common.Hash{},
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(int64(height)),
		GasLimit:    5000000,
		GasUsed:     gasUsed,
		Time:        timestamp,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
	}
	return types.NewBlockWithHeader(header)
}

// Helper to create a signed transaction
func createSignedTransaction(nonce uint64, to common.Address, value *big.Int, gasPrice *big.Int, privateKey *ecdsa.PrivateKey) (*types.Transaction, error) {
	tx := types.NewTransaction(
		nonce,
		to,
		value,
		21000,
		gasPrice,
		[]byte{},
	)

	signer := types.NewEIP155Signer(big.NewInt(1))
	signedTx, err := types.SignTx(tx, signer, privateKey)
	return signedTx, err
}

func TestPebbleStorage_GetTopMiners(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	miner1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	miner2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	miner3 := common.HexToAddress("0x3333333333333333333333333333333333333333")

	// Create blocks with different miners
	// Miner1: 5 blocks, Miner2: 3 blocks, Miner3: 2 blocks
	blocks := []*types.Block{
		createTestBlockWithMiner(100, miner1, 100000, 1000),
		createTestBlockWithMiner(101, miner1, 100000, 1001),
		createTestBlockWithMiner(102, miner2, 100000, 1002),
		createTestBlockWithMiner(103, miner1, 100000, 1003),
		createTestBlockWithMiner(104, miner2, 100000, 1004),
		createTestBlockWithMiner(105, miner3, 100000, 1005),
		createTestBlockWithMiner(106, miner1, 100000, 1006),
		createTestBlockWithMiner(107, miner2, 100000, 1007),
		createTestBlockWithMiner(108, miner1, 100000, 1008),
		createTestBlockWithMiner(109, miner3, 100000, 1009),
	}

	// Index blocks
	for _, block := range blocks {
		storage.SetBlock(ctx, block)
	}
	storage.SetLatestHeight(ctx, 109)

	// Get top miners
	miners, err := storage.GetTopMiners(ctx, 10, 100, 109)
	if err != nil {
		t.Fatalf("GetTopMiners() error = %v", err)
	}

	if len(miners) != 3 {
		t.Fatalf("GetTopMiners() returned %d miners, want 3", len(miners))
	}

	// Verify ordering (by block count)
	if miners[0].Address != miner1 {
		t.Errorf("Top miner = %s, want %s", miners[0].Address.Hex(), miner1.Hex())
	}
	if miners[0].BlockCount != 5 {
		t.Errorf("Top miner block count = %d, want 5", miners[0].BlockCount)
	}

	if miners[1].Address != miner2 {
		t.Errorf("2nd miner = %s, want %s", miners[1].Address.Hex(), miner2.Hex())
	}
	if miners[1].BlockCount != 3 {
		t.Errorf("2nd miner block count = %d, want 3", miners[1].BlockCount)
	}

	if miners[2].Address != miner3 {
		t.Errorf("3rd miner = %s, want %s", miners[2].Address.Hex(), miner3.Hex())
	}
	if miners[2].BlockCount != 2 {
		t.Errorf("3rd miner block count = %d, want 2", miners[2].BlockCount)
	}

	// Verify percentage calculations
	expectedPercentage1 := float64(5) / float64(10) * 100.0
	if miners[0].Percentage != expectedPercentage1 {
		t.Errorf("Top miner percentage = %f, want %f", miners[0].Percentage, expectedPercentage1)
	}
}

func TestPebbleStorage_GetTopMiners_WithLimit(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Create 5 different miners
	miners := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		common.HexToAddress("0x4444444444444444444444444444444444444444"),
		common.HexToAddress("0x5555555555555555555555555555555555555555"),
	}

	// Create blocks for each miner
	for i, miner := range miners {
		storage.SetBlock(ctx, createTestBlockWithMiner(uint64(100+i), miner, 100000, uint64(1000+i)))
	}
	storage.SetLatestHeight(ctx, 104)

	// Get top 3 miners
	topMiners, err := storage.GetTopMiners(ctx, 3, 100, 104)
	if err != nil {
		t.Fatalf("GetTopMiners() error = %v", err)
	}

	if len(topMiners) != 3 {
		t.Errorf("GetTopMiners(limit=3) returned %d miners, want 3", len(topMiners))
	}
}

func TestPebbleStorage_GetTopMiners_EmptyRange(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Set some blocks
	miner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	storage.SetBlock(ctx, createTestBlockWithMiner(100, miner, 100000, 1000))
	storage.SetLatestHeight(ctx, 100)

	// Query range with no blocks
	miners, err := storage.GetTopMiners(ctx, 10, 200, 300)
	if err != nil {
		t.Fatalf("GetTopMiners() error = %v", err)
	}

	if len(miners) != 0 {
		t.Errorf("GetTopMiners(empty range) returned %d miners, want 0", len(miners))
	}
}

func TestPebbleStorage_GetTokenBalances(t *testing.T) {
	t.Skip("TODO: Fix this test - requires block with actual transactions")
	// This test needs to be rewritten to include transactions in the block
	// GetReceiptsByBlockNumber requires transactions in the block to find receipts
}

func TestPebbleStorage_GetGasStatsByBlockRange(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Create test blocks with varying gas usage
	privateKey, _ := crypto.GenerateKey()
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")

	for i := uint64(0); i < 5; i++ {
		block := createTestBlockWithMiner(100+i, common.Address{}, 100000+i*10000, 1000+i)

		// Add transaction
		tx, err := createSignedTransaction(i, to, big.NewInt(1000), big.NewInt(1000000000), privateKey)
		if err != nil {
			t.Fatalf("Failed to create transaction: %v", err)
		}

		// Create block with transaction
		header := block.Header()
		header.TxHash = types.DeriveSha(types.Transactions{tx}, trie.NewStackTrie(nil))
		blockWithTx := types.NewBlockWithHeader(header).WithBody([]*types.Transaction{tx}, nil)

		storage.SetBlock(ctx, blockWithTx)

		// Create receipt
		receipt := &types.Receipt{
			Type:              types.LegacyTxType,
			PostState:         []byte{},
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
			Bloom:             types.Bloom{},
			Logs:              []*types.Log{},
			TxHash:            tx.Hash(),
			ContractAddress:   common.Address{},
			GasUsed:           21000,
			BlockHash:         blockWithTx.Hash(),
			BlockNumber:       big.NewInt(int64(100 + i)),
			TransactionIndex:  0,
		}
		storage.SetReceipt(ctx, receipt)
	}

	storage.SetLatestHeight(ctx, 104)

	// Get gas stats
	stats, err := storage.GetGasStatsByBlockRange(ctx, 100, 104)
	if err != nil {
		t.Fatalf("GetGasStatsByBlockRange() error = %v", err)
	}

	if stats.BlockCount != 5 {
		t.Errorf("BlockCount = %d, want 5", stats.BlockCount)
	}

	if stats.TransactionCount != 5 {
		t.Errorf("TransactionCount = %d, want 5", stats.TransactionCount)
	}

	if stats.TotalGasUsed == 0 {
		t.Error("TotalGasUsed should not be 0")
	}

	if stats.AverageGasUsed == 0 {
		t.Error("AverageGasUsed should not be 0")
	}
}

func TestPebbleStorage_GetGasStatsByBlockRange_InvalidRange(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// fromBlock > toBlock should return error
	_, err := storage.GetGasStatsByBlockRange(ctx, 200, 100)
	if err == nil {
		t.Error("GetGasStatsByBlockRange() with fromBlock > toBlock should return error")
	}
}

func TestPebbleStorage_GetNetworkMetrics(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Create blocks with timestamps
	miner := common.HexToAddress("0x1111111111111111111111111111111111111111")

	// Create 10 blocks over 10 seconds (1 block per second)
	for i := uint64(0); i < 10; i++ {
		block := createTestBlockWithMiner(100+i, miner, 100000, 1000+i)
		storage.SetBlock(ctx, block)
		storage.SetBlockTimestamp(ctx, 1000+i, 100+i)
	}

	storage.SetLatestHeight(ctx, 109)

	// Get network metrics
	metrics, err := storage.GetNetworkMetrics(ctx, 1000, 1009)
	if err != nil {
		t.Fatalf("GetNetworkMetrics() error = %v", err)
	}

	if metrics.TotalBlocks != 10 {
		t.Errorf("TotalBlocks = %d, want 10", metrics.TotalBlocks)
	}

	if metrics.TimePeriod != 9 {
		t.Errorf("TimePeriod = %d, want 9", metrics.TimePeriod)
	}

	// Block time should be approximately 1 second
	if metrics.BlockTime < 0.9 || metrics.BlockTime > 1.1 {
		t.Errorf("BlockTime = %f, want ~1.0", metrics.BlockTime)
	}
}

func TestPebbleStorage_GetNetworkMetrics_InvalidRange(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// fromTime > toTime should return error
	_, err := storage.GetNetworkMetrics(ctx, 2000, 1000)
	if err == nil {
		t.Error("GetNetworkMetrics() with fromTime > toTime should return error")
	}
}

func TestPebbleStorage_GetNetworkMetrics_EmptyRange(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Query empty time range
	metrics, err := storage.GetNetworkMetrics(ctx, 5000, 6000)
	if err != nil {
		t.Fatalf("GetNetworkMetrics() error = %v", err)
	}

	if metrics.TotalBlocks != 0 {
		t.Errorf("TotalBlocks = %d, want 0", metrics.TotalBlocks)
	}

	if metrics.TPS != 0 {
		t.Errorf("TPS = %f, want 0", metrics.TPS)
	}
}

func TestPebbleStorage_Analytics_ClosedStorage(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	// Close storage
	storage.Close()

	// All analytics operations should return ErrClosed
	_, err := storage.GetTopMiners(ctx, 10, 0, 100)
	if err != ErrClosed {
		t.Errorf("GetTopMiners() on closed storage error = %v, want ErrClosed", err)
	}

	_, err = storage.GetTokenBalances(ctx, addr, "")
	if err != ErrClosed {
		t.Errorf("GetTokenBalances() on closed storage error = %v, want ErrClosed", err)
	}

	_, err = storage.GetGasStatsByBlockRange(ctx, 0, 100)
	if err != ErrClosed {
		t.Errorf("GetGasStatsByBlockRange() on closed storage error = %v, want ErrClosed", err)
	}

	_, err = storage.GetNetworkMetrics(ctx, 1000, 2000)
	if err != ErrClosed {
		t.Errorf("GetNetworkMetrics() on closed storage error = %v, want ErrClosed", err)
	}
}

func TestPebbleStorage_GetTopMiners_NoBlocks(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Query without any blocks
	miners, err := storage.GetTopMiners(ctx, 10, 0, 100)
	if err != nil {
		t.Fatalf("GetTopMiners() error = %v", err)
	}

	if len(miners) != 0 {
		t.Errorf("GetTopMiners() with no blocks returned %d miners, want 0", len(miners))
	}
}

func TestPebbleStorage_GetTokenBalances_NoBalances(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	storage.SetLatestHeight(ctx, 100)

	// Query without any token transfers
	balances, err := storage.GetTokenBalances(ctx, addr, "")
	if err != nil {
		t.Fatalf("GetTokenBalances() error = %v", err)
	}

	if len(balances) != 0 {
		t.Errorf("GetTokenBalances() with no transfers returned %d balances, want 0", len(balances))
	}
}

func TestPebbleStorage_GetGasStatsByBlockRange_EmptyRange(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Add a block
	miner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	storage.SetBlock(ctx, createTestBlockWithMiner(100, miner, 100000, 1000))
	storage.SetLatestHeight(ctx, 100)

	// Query range with no blocks
	stats, err := storage.GetGasStatsByBlockRange(ctx, 200, 300)
	if err != nil {
		t.Fatalf("GetGasStatsByBlockRange() error = %v", err)
	}

	if stats.BlockCount != 0 {
		t.Errorf("BlockCount = %d, want 0", stats.BlockCount)
	}

	if stats.TotalGasUsed != 0 {
		t.Errorf("TotalGasUsed = %d, want 0", stats.TotalGasUsed)
	}
}
