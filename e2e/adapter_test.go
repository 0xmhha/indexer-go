// Package e2e provides end-to-end tests for the indexer against real blockchain nodes.
// These tests require Anvil to be installed and available in the PATH.
package e2e

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/adapters/detector"
	"github.com/0xmhha/indexer-go/adapters/factory"
	"github.com/0xmhha/indexer-go/e2e/anvil"
	"github.com/0xmhha/indexer-go/types/chain"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// TestAdapterDetection tests that the adapter factory correctly detects Anvil
func TestAdapterDetection(t *testing.T) {
	anvil.SkipIfNoAnvil(t.Skip)

	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start Anvil
	instance := anvil.NewTestInstance(nil, logger)
	if err := instance.Start(ctx); err != nil {
		t.Fatalf("Failed to start anvil: %v", err)
	}
	defer instance.Stop()

	// Create adapter using factory
	result, err := instance.CreateAdapter(ctx)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}

	// Verify adapter type
	if result.AdapterType != "anvil" {
		t.Errorf("Expected adapter type 'anvil', got '%s'", result.AdapterType)
	}

	// Verify node info
	if result.NodeInfo == nil {
		t.Fatal("Expected non-nil node info")
	}

	if result.NodeInfo.Type != detector.NodeTypeAnvil {
		t.Errorf("Expected node type %s, got %s", detector.NodeTypeAnvil, result.NodeInfo.Type)
	}

	// Verify chain info
	info := result.Adapter.Info()
	if info == nil {
		t.Fatal("Expected non-nil chain info")
	}

	if info.ChainType != chain.ChainTypeEVM {
		t.Errorf("Expected chain type %s, got %s", chain.ChainTypeEVM, info.ChainType)
	}

	if info.ConsensusType != chain.ConsensusTypePoA {
		t.Errorf("Expected consensus type %s, got %s", chain.ConsensusTypePoA, info.ConsensusType)
	}

	if info.Name != "Anvil" {
		t.Errorf("Expected name 'Anvil', got '%s'", info.Name)
	}

	t.Logf("Successfully detected Anvil adapter with chain ID: %s", info.ChainID.String())
}

// TestAdapterBlockFetching tests that the adapter can fetch blocks
func TestAdapterBlockFetching(t *testing.T) {
	anvil.SkipIfNoAnvil(t.Skip)

	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start Anvil
	instance := anvil.NewTestInstance(nil, logger)
	if err := instance.Start(ctx); err != nil {
		t.Fatalf("Failed to start anvil: %v", err)
	}
	defer instance.Stop()

	// Mine some blocks
	if err := instance.MineBlocks(ctx, 5); err != nil {
		t.Fatalf("Failed to mine blocks: %v", err)
	}

	// Create adapter
	result, err := instance.CreateAdapter(ctx)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}

	// Get block fetcher
	fetcher := result.Adapter.BlockFetcher()
	if fetcher == nil {
		t.Fatal("Expected non-nil block fetcher")
	}

	// Get latest block number
	latestBlock, err := fetcher.GetLatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("Failed to get latest block number: %v", err)
	}

	if latestBlock < 5 {
		t.Errorf("Expected at least 5 blocks, got %d", latestBlock)
	}

	// Fetch block by number
	block, err := fetcher.GetBlockByNumber(ctx, latestBlock)
	if err != nil {
		t.Fatalf("Failed to get block %d: %v", latestBlock, err)
	}

	if block == nil {
		t.Fatal("Expected non-nil block")
	}

	if block.NumberU64() != latestBlock {
		t.Errorf("Expected block number %d, got %d", latestBlock, block.NumberU64())
	}

	// Fetch block by hash - get the original hash from Anvil directly via raw RPC
	// (avoids EIP-4844 parsing issues with go-ethereum's ethclient)
	originalHash, err := instance.GetBlockHashByNumber(ctx, big.NewInt(int64(latestBlock)))
	if err != nil {
		t.Fatalf("Failed to get original block hash: %v", err)
	}

	blockByHash, err := fetcher.GetBlockByHash(ctx, originalHash)
	if err != nil {
		t.Fatalf("Failed to get block by hash: %v", err)
	}

	if blockByHash.NumberU64() != latestBlock {
		t.Errorf("Expected same block number, got %d vs %d", blockByHash.NumberU64(), latestBlock)
	}

	t.Logf("Successfully fetched block %d with hash %s", latestBlock, originalHash.Hex())
}

// TestAdapterConsensusParsing tests that the adapter can parse consensus data
func TestAdapterConsensusParsing(t *testing.T) {
	anvil.SkipIfNoAnvil(t.Skip)

	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start Anvil
	instance := anvil.NewTestInstance(nil, logger)
	if err := instance.Start(ctx); err != nil {
		t.Fatalf("Failed to start anvil: %v", err)
	}
	defer instance.Stop()

	// Mine some blocks
	if err := instance.MineBlocks(ctx, 3); err != nil {
		t.Fatalf("Failed to mine blocks: %v", err)
	}

	// Create adapter
	result, err := instance.CreateAdapter(ctx)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}

	// Get consensus parser
	parser := result.Adapter.ConsensusParser()
	if parser == nil {
		t.Fatal("Expected non-nil consensus parser")
	}

	// Verify consensus type
	if parser.ConsensusType() != chain.ConsensusTypePoA {
		t.Errorf("Expected consensus type %s, got %s", chain.ConsensusTypePoA, parser.ConsensusType())
	}

	// Fetch a block and parse consensus data
	fetcher := result.Adapter.BlockFetcher()
	block, err := fetcher.GetBlockByNumber(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to get block 1: %v", err)
	}

	consensusData, err := parser.ParseConsensusData(block)
	if err != nil {
		t.Fatalf("Failed to parse consensus data: %v", err)
	}

	if consensusData == nil {
		t.Fatal("Expected non-nil consensus data")
	}

	if consensusData.ConsensusType != chain.ConsensusTypePoA {
		t.Errorf("Expected consensus type %s, got %s", chain.ConsensusTypePoA, consensusData.ConsensusType)
	}

	if consensusData.BlockNumber != 1 {
		t.Errorf("Expected block number 1, got %d", consensusData.BlockNumber)
	}

	t.Logf("Successfully parsed consensus data for block %d, proposer: %s",
		consensusData.BlockNumber, consensusData.ProposerAddress.Hex())
}

// TestForcedAdapterType tests forcing a specific adapter type
func TestForcedAdapterType(t *testing.T) {
	anvil.SkipIfNoAnvil(t.Skip)

	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start Anvil
	instance := anvil.NewTestInstance(nil, logger)
	if err := instance.Start(ctx); err != nil {
		t.Fatalf("Failed to start anvil: %v", err)
	}
	defer instance.Stop()

	// Create adapter with forced EVM type
	config := factory.DefaultConfig(instance.RPCURL())
	config.ForceAdapterType = "evm"

	result, err := factory.CreateAdapterWithConfig(ctx, config, logger)
	if err != nil {
		t.Fatalf("Failed to create forced adapter: %v", err)
	}

	// Should be EVM adapter, not Anvil
	if result.AdapterType != "evm" {
		t.Errorf("Expected adapter type 'evm', got '%s'", result.AdapterType)
	}

	// Verify it still works
	info := result.Adapter.Info()
	if info.ChainType != chain.ChainTypeEVM {
		t.Errorf("Expected chain type EVM, got %s", info.ChainType)
	}

	t.Logf("Successfully created forced EVM adapter")
}

// TestAnvilSpecificFeatures tests Anvil-specific RPC methods
func TestAnvilSpecificFeatures(t *testing.T) {
	anvil.SkipIfNoAnvil(t.Skip)

	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start Anvil
	instance := anvil.NewTestInstance(nil, logger)
	if err := instance.Start(ctx); err != nil {
		t.Fatalf("Failed to start anvil: %v", err)
	}
	defer instance.Stop()

	// Test snapshot/revert
	snapshotID, err := instance.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Mine some blocks
	if err := instance.MineBlocks(ctx, 5); err != nil {
		t.Fatalf("Failed to mine blocks: %v", err)
	}

	blockAfterMine, err := instance.GetLatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("Failed to get block number: %v", err)
	}

	// Revert to snapshot
	if err := instance.Revert(ctx, snapshotID); err != nil {
		t.Fatalf("Failed to revert: %v", err)
	}

	blockAfterRevert, err := instance.GetLatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("Failed to get block number after revert: %v", err)
	}

	if blockAfterRevert >= blockAfterMine {
		t.Errorf("Expected block number to decrease after revert, got %d >= %d",
			blockAfterRevert, blockAfterMine)
	}

	t.Logf("Successfully tested snapshot/revert: %d -> %d", blockAfterMine, blockAfterRevert)
}

// TestMultipleBlocks tests fetching multiple blocks
func TestMultipleBlocks(t *testing.T) {
	anvil.SkipIfNoAnvil(t.Skip)

	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start Anvil
	instance := anvil.NewTestInstance(nil, logger)
	if err := instance.Start(ctx); err != nil {
		t.Fatalf("Failed to start anvil: %v", err)
	}
	defer instance.Stop()

	// Create adapter
	result, err := instance.CreateAdapter(ctx)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}

	fetcher := result.Adapter.BlockFetcher()

	// Mine blocks
	blockCount := uint64(20)
	if err := instance.MineBlocks(ctx, blockCount); err != nil {
		t.Fatalf("Failed to mine blocks: %v", err)
	}

	// Fetch all blocks
	for i := uint64(1); i <= blockCount; i++ {
		block, err := fetcher.GetBlockByNumber(ctx, i)
		if err != nil {
			t.Fatalf("Failed to fetch block %d: %v", i, err)
		}

		if block.NumberU64() != i {
			t.Errorf("Block number mismatch: expected %d, got %d", i, block.NumberU64())
		}

		// Verify block hash is not zero
		if block.Hash() == (common.Hash{}) {
			t.Errorf("Block %d has zero hash", i)
		}
	}

	t.Logf("Successfully fetched %d blocks", blockCount)
}
