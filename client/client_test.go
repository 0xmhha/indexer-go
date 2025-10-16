package client

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "empty endpoint",
			config: &Config{
				Endpoint: "",
			},
			wantErr: true,
		},
		{
			name: "invalid endpoint",
			config: &Config{
				Endpoint: "invalid://endpoint",
				Timeout:  5 * time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if client != nil {
				client.Close()
			}
		})
	}
}

// TestClientIntegration contains integration tests - require running Ethereum node
// Skip by default, run with: go test -tags=integration
func TestClientIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Set up test client pointing to local or testnet node
	endpoint := "http://localhost:8545" // Change to your test node
	logger, _ := zap.NewDevelopment()

	cfg := &Config{
		Endpoint: endpoint,
		Timeout:  30 * time.Second,
		Logger:   logger,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	t.Run("GetChainID", func(t *testing.T) {
		chainID, err := client.GetChainID(ctx)
		if err != nil {
			t.Errorf("GetChainID() error = %v", err)
			return
		}
		if chainID.Cmp(big.NewInt(0)) <= 0 {
			t.Errorf("GetChainID() returned invalid chain ID: %v", chainID)
		}
		t.Logf("Chain ID: %s", chainID.String())
	})

	t.Run("GetLatestBlockNumber", func(t *testing.T) {
		blockNumber, err := client.GetLatestBlockNumber(ctx)
		if err != nil {
			t.Errorf("GetLatestBlockNumber() error = %v", err)
			return
		}
		if blockNumber == 0 {
			t.Errorf("GetLatestBlockNumber() returned 0")
		}
		t.Logf("Latest block number: %d", blockNumber)
	})

	t.Run("GetBlockByNumber", func(t *testing.T) {
		// Get genesis block
		block, err := client.GetBlockByNumber(ctx, 0)
		if err != nil {
			t.Errorf("GetBlockByNumber() error = %v", err)
			return
		}
		if block == nil {
			t.Errorf("GetBlockByNumber() returned nil block")
			return
		}
		if block.Number().Uint64() != 0 {
			t.Errorf("GetBlockByNumber(0) returned block %d", block.Number().Uint64())
		}
		t.Logf("Genesis block hash: %s", block.Hash().Hex())
	})

	t.Run("GetBlockByHash", func(t *testing.T) {
		// First get a block by number
		blockByNum, err := client.GetBlockByNumber(ctx, 0)
		if err != nil {
			t.Errorf("GetBlockByNumber() error = %v", err)
			return
		}

		// Then get the same block by hash
		blockByHash, err := client.GetBlockByHash(ctx, blockByNum.Hash())
		if err != nil {
			t.Errorf("GetBlockByHash() error = %v", err)
			return
		}

		if blockByHash.Hash() != blockByNum.Hash() {
			t.Errorf("Block hashes don't match: %s != %s",
				blockByHash.Hash().Hex(), blockByNum.Hash().Hex())
		}
	})

	t.Run("GetBlockReceipts", func(t *testing.T) {
		// Get latest block number
		latestNum, err := client.GetLatestBlockNumber(ctx)
		if err != nil {
			t.Errorf("GetLatestBlockNumber() error = %v", err)
			return
		}

		// Get a recent block (not genesis)
		if latestNum > 100 {
			receipts, err := client.GetBlockReceipts(ctx, latestNum-10)
			if err != nil {
				t.Errorf("GetBlockReceipts() error = %v", err)
				return
			}
			t.Logf("Block %d has %d receipts", latestNum-10, len(receipts))
		}
	})

	t.Run("GetTransactionByHash", func(t *testing.T) {
		// Get a block with transactions
		latestNum, err := client.GetLatestBlockNumber(ctx)
		if err != nil {
			t.Errorf("GetLatestBlockNumber() error = %v", err)
			return
		}

		// Try to find a block with transactions
		for i := latestNum; i > latestNum-100 && i > 0; i-- {
			block, err := client.GetBlockByNumber(ctx, i)
			if err != nil {
				continue
			}

			if block.Transactions().Len() > 0 {
				txHash := block.Transactions()[0].Hash()
				tx, isPending, err := client.GetTransactionByHash(ctx, txHash)
				if err != nil {
					t.Errorf("GetTransactionByHash() error = %v", err)
					return
				}
				if isPending {
					t.Errorf("Transaction %s should not be pending", txHash.Hex())
				}
				if tx.Hash() != txHash {
					t.Errorf("Transaction hash mismatch: %s != %s",
						tx.Hash().Hex(), txHash.Hex())
				}
				t.Logf("Found transaction: %s", txHash.Hex())
				break
			}
		}
	})

	t.Run("GetTransactionReceipt", func(t *testing.T) {
		// Get a block with transactions
		latestNum, err := client.GetLatestBlockNumber(ctx)
		if err != nil {
			t.Errorf("GetLatestBlockNumber() error = %v", err)
			return
		}

		// Try to find a block with transactions
		for i := latestNum; i > latestNum-100 && i > 0; i-- {
			block, err := client.GetBlockByNumber(ctx, i)
			if err != nil {
				continue
			}

			if block.Transactions().Len() > 0 {
				txHash := block.Transactions()[0].Hash()
				receipt, err := client.GetTransactionReceipt(ctx, txHash)
				if err != nil {
					t.Errorf("GetTransactionReceipt() error = %v", err)
					return
				}
				if receipt.TxHash != txHash {
					t.Errorf("Receipt hash mismatch: %s != %s",
						receipt.TxHash.Hex(), txHash.Hex())
				}
				t.Logf("Receipt status: %d, gas used: %d",
					receipt.Status, receipt.GasUsed)
				break
			}
		}
	})

	t.Run("BatchGetBlocks", func(t *testing.T) {
		numbers := []uint64{0, 1, 2, 3, 4}
		blocks, err := client.BatchGetBlocks(ctx, numbers)
		if err != nil {
			t.Errorf("BatchGetBlocks() error = %v", err)
			return
		}
		if len(blocks) != len(numbers) {
			t.Errorf("BatchGetBlocks() returned %d blocks, expected %d",
				len(blocks), len(numbers))
		}
		for i, block := range blocks {
			if block != nil && block.Number().Uint64() != numbers[i] {
				t.Errorf("Block %d has wrong number: %d",
					i, block.Number().Uint64())
			}
		}
		t.Logf("Batch fetched %d blocks", len(blocks))
	})

	t.Run("BatchGetReceipts", func(t *testing.T) {
		// Get a block with transactions
		latestNum, err := client.GetLatestBlockNumber(ctx)
		if err != nil {
			t.Errorf("GetLatestBlockNumber() error = %v", err)
			return
		}

		// Collect transaction hashes
		var txHashes []common.Hash
		for i := latestNum; i > latestNum-100 && i > 0; i-- {
			block, err := client.GetBlockByNumber(ctx, i)
			if err != nil {
				continue
			}

			for _, tx := range block.Transactions() {
				txHashes = append(txHashes, tx.Hash())
				if len(txHashes) >= 5 {
					break
				}
			}
			if len(txHashes) >= 5 {
				break
			}
		}

		if len(txHashes) > 0 {
			receipts, err := client.BatchGetReceipts(ctx, txHashes)
			if err != nil {
				t.Errorf("BatchGetReceipts() error = %v", err)
				return
			}
			if len(receipts) != len(txHashes) {
				t.Errorf("BatchGetReceipts() returned %d receipts, expected %d",
					len(receipts), len(txHashes))
			}
			t.Logf("Batch fetched %d receipts", len(receipts))
		}
	})

	t.Run("Ping", func(t *testing.T) {
		err := client.Ping(ctx)
		if err != nil {
			t.Errorf("Ping() error = %v", err)
		}
	})
}
